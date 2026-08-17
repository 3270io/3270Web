// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
)

func newScreenStreamTestApp(t *testing.T) (*App, *host.MockHost, *session.Session) {
	t.Helper()
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
		chaosEngines:   newChaosEngineStore(),
	}
	sess := app.SessionManager.CreateSession(mh)
	return app, mh, sess
}

// TestRenderScreenStreamUpdateDetectsChange guards the core diffing logic:
// the same screen content must not be reported as changed twice in a row,
// but an actual field write must be.
func TestRenderScreenStreamUpdateDetectsChange(t *testing.T) {
	app, mh, sess := newScreenStreamTestApp(t)
	// Unformatted so the rendered HTML reflects the raw buffer directly
	// (renderFormatted instead walks s.Fields, which is empty on a bare
	// MockHost screen, so a raw buffer edit wouldn't show up there).
	mh.Screen.IsFormatted = false
	lastHTML := ""

	_, changed := app.renderScreenStreamUpdate(sess, &lastHTML)
	if !changed {
		t.Fatalf("first render reported unchanged, want changed (nothing pushed yet)")
	}
	if lastHTML == "" {
		t.Fatalf("lastHTML was not populated after a changed render")
	}

	_, changedAgain := app.renderScreenStreamUpdate(sess, &lastHTML)
	if changedAgain {
		t.Fatalf("second render with no host mutation reported changed, want unchanged")
	}

	mh.Screen.Buffer[0][0] = 'X'
	_, changedAfterMutation := app.renderScreenStreamUpdate(sess, &lastHTML)
	if !changedAfterMutation {
		t.Fatalf("render after mutating the screen buffer reported unchanged, want changed")
	}
}

// TestRenderScreenStreamUpdateYieldsDuringPlayback guards the handoff to the
// existing chaos/playback polling: while a workflow playback is active for
// the session, the SSE poller must not also push updates (it would just
// race the same DOM patch against workflow.js's own, tighter-cadence poll).
func TestRenderScreenStreamUpdateYieldsDuringPlayback(t *testing.T) {
	app, _, sess := newScreenStreamTestApp(t)
	withSessionLock(sess, func() {
		sess.Playback = &session.WorkflowPlayback{Active: true}
	})

	lastHTML := ""
	_, changed := app.renderScreenStreamUpdate(sess, &lastHTML)
	if changed {
		t.Fatalf("renderScreenStreamUpdate pushed a change while playback is active, want it to yield")
	}
}

// TestScreenStreamHandlerRequiresSession guards the auth check: no session
// cookie must be rejected before any SSE headers are written.
func TestScreenStreamHandlerRequiresSession(t *testing.T) {
	app, _, _ := newScreenStreamTestApp(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/screen/stream", app.ScreenStreamHandler)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/screen/stream", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestScreenStreamHandlerPushesAndReconnects is a live end-to-end check: a
// real HTTP client connects, receives the SSE content-type and at least one
// event within a couple of poll intervals, then disconnects cleanly.
func TestScreenStreamHandlerPushesAndReconnects(t *testing.T) {
	app, _, sess := newScreenStreamTestApp(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/screen/stream", app.ScreenStreamHandler)

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/screen/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(3 * time.Second)
	sawDataLine := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			sawDataLine = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	if !sawDataLine {
		t.Fatalf("did not observe an SSE data line within the deadline")
	}
}
