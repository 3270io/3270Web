// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

const (
	// screenStreamPollInterval is how often the handler checks the host for
	// changes to push. Matches workflow.js's playbackFastMs so an idle
	// screen refreshes about as promptly as an active chaos/playback run.
	screenStreamPollInterval = 700 * time.Millisecond
	// screenStreamHeartbeatInterval keeps the connection alive through
	// proxies/load balancers that time out an idle stream.
	screenStreamHeartbeatInterval = 25 * time.Second
	// screenStreamMaxDuration caps one SSE connection so a browser tab left
	// open indefinitely doesn't pin a server goroutine (and its polling load
	// on the s3270 subprocess) forever. EventSource reconnects automatically
	// once the connection ends, so this is invisible to the user.
	screenStreamMaxDuration = 30 * time.Minute
)

// ScreenStreamHandler pushes screen updates to the browser over
// Server-Sent Events. Without this, a plain interactive session only ever
// saw updates in response to that browser's own request (a keystroke or
// submit) — any other change to the screen (e.g. a Copilot tool call
// acting on the same session) stayed invisible until the next local
// interaction. It defers entirely to the existing chaos/playback polling
// (workflow.js) while either is active for this session, since that
// already refreshes the screen at its own tighter, purpose-tuned cadence.
func (app *App) ScreenStreamHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	// A host that has already hung up is answered with an error status rather
	// than an empty stream. A browser treats any answer but 200 to an
	// EventSource as the end of it and says so, which is what hands the page
	// to its reconnect handling (session-expiry.js). A stream that opened
	// happily and then said nothing was how a dead host went unnoticed for as
	// long as the tab was left open.
	if h := app.sessionHost(s); h != nil {
		if err := h.UpdateScreen(); err != nil && host.IsDisconnected(err) {
			respondHostLostJSON(c)
			return
		}
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), screenStreamMaxDuration)
	defer cancel()

	poll := time.NewTicker(screenStreamPollInterval)
	defer poll.Stop()
	heartbeat := time.NewTicker(screenStreamHeartbeatInterval)
	defer heartbeat.Stop()

	lastHTML := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(c.Writer, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
			data, changed, lost := app.renderScreenStreamState(s, &lastHTML)
			if lost {
				// The host hung up while this stream was open. End it, asking
				// the browser to come back quickly: the request it makes is
				// answered with the 409 above, and that is how the page finds
				// out. Carrying on would poll a dead host every poll interval
				// for ever, saying nothing.
				_, _ = io.WriteString(c.Writer, "retry: 500\n\n")
				flusher.Flush()
				return
			}
			if !changed {
				continue
			}
			if _, err := io.WriteString(c.Writer, "data: "+string(data)+"\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// renderScreenStreamUpdate renders the current screen and reports whether
// it changed since the last push. It yields (returns unchanged) whenever a
// chaos run or workflow playback is active for this session, since those
// already drive their own polling loop at a tighter cadence — pushing here
// too would just make the same DOM patch race two sources.
func (app *App) renderScreenStreamUpdate(s *session.Session, lastHTML *string) ([]byte, bool) {
	data, changed, _ := app.renderScreenStreamState(s, lastHTML)
	return data, changed
}

// renderScreenStreamState is renderScreenStreamUpdate that also says whether
// the host has hung up, which the handler answers by ending the stream.
func (app *App) renderScreenStreamState(s *session.Session, lastHTML *string) (data []byte, changed bool, lost bool) {
	var playbackActive bool
	withSessionLock(s, func() {
		playbackActive = s.Playback != nil && s.Playback.Active
	})
	if playbackActive {
		return nil, false, false
	}
	if app.chaosEngines != nil {
		if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil && eng.Active() {
			return nil, false, false
		}
	}

	h := app.sessionHost(s)
	if h == nil {
		return nil, false, false
	}
	if err := h.UpdateScreen(); err != nil {
		return nil, false, host.IsDisconnected(err)
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		return nil, false, false
	}
	if rows, cols, ok := app.modelDimensions(); ok {
		screen = limitScreenForDisplay(screen, rows, cols)
	}
	recordScreenHistory(s, screen)
	rendered := app.Renderer.Render(screen, "/submit", s.ID)
	oia := screenOIA(screen)
	// Dedup on the OIA as well as the HTML: a host that locks the keyboard or
	// drops the connection without repainting the screen produces byte-identical
	// HTML, and suppressing that update would leave the operator looking at a
	// stale "ready" indicator during exactly the wait it exists to report.
	fingerprint := rendered + "\x00" + fmt.Sprint(oia["oiaIndicator"], oia["oiaOnline"])
	if fingerprint == *lastHTML {
		return nil, false, false
	}
	*lastHTML = fingerprint

	keyboardLabel, modelLabel, dimensionLabel, cursorLabel := screenStatusLabels(screen)
	payload := mergeOIA(gin.H{
		"html":             rendered,
		"statusKeyboard":   keyboardLabel,
		"statusModel":      modelLabel,
		"statusDimensions": dimensionLabel,
		"statusCursor":     cursorLabel,
	}, screen)
	if cursorRow, cursorCol, ok := screen.StatusCursor(); ok {
		payload["cursorRow"] = cursorRow
		payload["cursorCol"] = cursorCol
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, false
	}
	return encoded, true, false
}
