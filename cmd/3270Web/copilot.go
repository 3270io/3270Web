// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/aiprovider"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// initCopilot wires the Copilot package's HTTP routes and adds the
// /screen.json route used by the get_screen tool.
//
// Copilot login/token state is scoped per-browser (see the copilotAuthStore
// field and the identity cookie in internal/copilot/handlers.go), separately
// from the 3270Web_session cookie that tool execution resolves the active
// host connection through.
func (app *App) initCopilot(r *gin.Engine) {
	authPath, err := copilot.DefaultAuthPath()
	if err != nil {
		log.Printf("[copilot] disabled: cannot resolve auth path: %v", err)
		return
	}
	app.copilotAuthStore = copilot.NewAuthStore(filepath.Dir(authPath))
	copilot.NewHandlers(app.copilotAuthStore).Register(r)

	// Provider selection (Claude, OpenAI, Ollama, Google AI, any
	// OpenAI-compatible endpoint, ...) lives one layer above Copilot: it
	// stores per-browser credentials and proxies chat to whichever backend is
	// selected, delegating back to internal/copilot when that backend is
	// Copilot itself. The /api/copilot/* routes above stay for the OAuth
	// device flow and for backwards compatibility.
	app.aiConfigStore = aiprovider.NewConfigStore(filepath.Dir(authPath))
	aiprovider.NewHandlers(app.aiConfigStore, app.copilotAuthStore).Register(r)

	// Tool-supporting endpoints for the get_screen / send_key / write_field
	// / submit_screen tools. These wrap host.* calls in JSON so the
	// frontend can drive them from the Copilot panel.
	r.GET("/screen.json", app.ScreenJSONHandler)
	r.POST("/screen/key", app.ScreenKeyHandler)
	r.POST("/screen/write", app.ScreenWriteHandler)
	r.POST("/screen/submit", app.ScreenSubmitHandler)
	r.POST("/screen/connect", app.ScreenConnectHandler)
	r.POST("/screen/cursor", app.ScreenCursorHandler)
	r.POST("/screen/wait", app.ScreenWaitHandler)

	// Per-turn orientation block injected by the chat panel into the system
	// prompt (current screen + learned application knowledge).
	r.GET("/copilot/context", app.CopilotContextHandler)

	// One discovered screen, by the hash every screen result now carries.
	r.GET("/copilot/known-screen", app.KnownScreenHandler)
}

// dangerousKeyRefusal answers a request for a key that needs a person, unless
// the request says a person agreed.
//
// The chat panel has always asked before pressing PF3 or Clear, but it was the
// panel that asked: the endpoint itself pressed whatever it was sent. Anything
// else holding the session cookie — a script, a second tab, a panel build with
// the check removed, a model that found a spelling the panel's classifier did
// not fold — reached the host unchallenged. The rule now lives where the key
// is pressed. "confirmed" is set by the panel only after a human click; the
// server cannot verify the click, but it can make sure that pressing a
// dangerous key is always a deliberate, separately-marked request rather than
// the default.
//
// It reports whether it wrote a response.
func dangerousKeyRefusal(c *gin.Context, key string, confirmed bool) bool {
	if confirmed || !copilot.IsDangerousKey(key) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"error": fmt.Sprintf(
			"%s needs a person's approval before it is sent: it commonly ends the session or discards what is on the screen. "+
				"Nothing was sent. Ask the user, and resend with \"confirmed\": true only once they have agreed.", key),
		"requiresApproval": true,
		"key":              key,
	})
	return true
}

// sendRecordedKey presses a key the way the operator's own submit does: the
// screen the host painted and the key are appended to an active recording
// first, then the key is sent. A recording made while an assistant drives the
// terminal used to have none of the assistant's keystrokes in it.
func (app *App) sendRecordedKey(s *session.Session, h host.Host, key string) error {
	recordScreenForStep(s, hostScreenSnapshot(h))
	recordActionKey(s, key)
	return h.SendKey(key)
}

// screenAfterAction reads the screen an action produced, with everything the
// model is shown about a screen attached.
func (app *App) screenAfterAction(s *session.Session, h host.Host) (gin.H, error) {
	if err := h.UpdateScreen(); err != nil {
		return nil, fmt.Errorf("update screen: %w", err)
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		return nil, fmt.Errorf("screen unavailable")
	}
	return app.sessionScreenJSON(s, screen), nil
}

// ScreenKeyHandler sends one key to the host without first submitting any
// pending field writes, and returns the screen it produced. Used by the
// send_key tool.
//
// Body: {"key": "PF7", "confirmed": false}. A key on the dangerous list is
// refused with 409 unless confirmed is true — see dangerousKeyRefusal.
func (app *App) ScreenKeyHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		Key       string `json:"key"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	key, ok := normalizeKey(body.Key)
	if !ok {
		// A model asking for a key that doesn't exist (e.g. a typo like
		// "PF25") must fail loudly, not silently press Enter and submit
		// whatever is currently on screen.
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unrecognized key %q", body.Key)})
		return
	}
	if dangerousKeyRefusal(c, key, body.Confirmed) {
		return
	}
	h := app.sessionHost(s)
	if err := app.sendRecordedKey(s, h, key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := gin.H{"ok": true, "key": key}
	if screen, err := app.screenAfterAction(s, h); err == nil {
		out["screen"] = screen
	}
	c.JSON(http.StatusOK, out)
}

// fieldWriteRequest is one write, after the two ways of naming a position
// have been reduced to a 0-indexed row and column.
type fieldWriteRequest struct {
	Row, Col int
	Text     string
	Erase    bool
}

// writeFieldChecked writes into the field at a position with the checks a
// caller that cannot see the screen needs — see host.WriteField — and records
// the write when a recording is active.
//
// A host with no buffer to check against falls back to the bare write. Every
// host that reaches a real terminal has one.
func (app *App) writeFieldChecked(s *session.Session, h host.Host, req fieldWriteRequest) (*host.FieldWrite, error) {
	var (
		result *host.FieldWrite
		err    error
	)
	if fw, ok := h.(host.FieldWriter); ok {
		result, err = fw.WriteField(req.Row, req.Col, req.Text, req.Erase)
	} else {
		err = h.WriteStringAt(req.Row, req.Col, req.Text)
		result = &host.FieldWrite{Row: req.Row, Col: req.Col, Length: len([]rune(req.Text)), Value: req.Text}
	}
	if err != nil {
		return nil, err
	}
	recordFieldWrite(s, req.Row, req.Col, req.Text)
	return result, nil
}

// recordFieldWrite appends a write to an active recording through the same
// function the operator's own typing goes through, so a step an assistant
// produced is indistinguishable from one a person did.
func recordFieldWrite(s *session.Session, row, col int, text string) {
	typed := &host.Field{
		StartY: row, StartX: col,
		EndY: row, EndX: col + len([]rune(text)) - 1,
		Value:   text,
		Changed: true,
	}
	recordFieldUpdates(s, &host.Screen{Fields: []*host.Field{typed}})
}

// fieldWriteJSON describes the field after a write. A hidden field's value is
// masked: the caller learns that the field now holds something of that length,
// which it knew, and not the characters, which must not travel back into a
// model's context or a browser transcript.
func fieldWriteJSON(w *host.FieldWrite) gin.H {
	value := w.Value
	if w.Hidden {
		value = strings.Repeat("*", len([]rune(value)))
	}
	out := gin.H{
		"row":    w.Row,
		"col":    w.Col,
		"length": w.Length,
		"value":  value,
	}
	if w.Hidden {
		out["hidden"] = true
	}
	if w.Numeric {
		out["numeric"] = true
	}
	return out
}

// ScreenWriteHandler writes text into the input field at a position. Used by
// the write_field tool.
//
// Body: {"row": 4, "col": 20, "text": "LEE", "erase": true}, or a 1-indexed
// "field_key" (R<row>C<col>L<len>, from chaos_list_screens and business
// function steps) in place of row and col — never both conventions applied to
// one call, which is what once let a model land a row and a column off by
// passing a field key through as though it were 0-indexed.
//
// The position is required: a write with neither used to default to row 0,
// column 0 and type into whatever was there. The target must be an unprotected
// field with room for the text. By default the field is erased from the
// position to its end first, so the field holds what was written and not the
// new text followed by the tail of the old; "erase": false overtypes. Empty
// text clears the field. The answer is the field as it now stands.
func (app *App) ScreenWriteHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		FieldKey string `json:"field_key"`
		Row      *int   `json:"row"`
		Col      *int   `json:"col"`
		Text     string `json:"text"`
		Erase    *bool  `json:"erase"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req := fieldWriteRequest{Text: body.Text, Erase: body.Erase == nil || *body.Erase}
	if fieldKey := strings.TrimSpace(body.FieldKey); fieldKey != "" {
		fkRow, fkCol, _, ok := chaos.ParseMindMapFieldKey(fieldKey)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid field_key %q (expected R<row>C<col>L<len>)", fieldKey)})
			return
		}
		// field_key is 1-indexed; the terminal is 0-indexed.
		req.Row, req.Col = fkRow-1, fkCol-1
	} else {
		if body.Row == nil || body.Col == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "row and col are required (0-indexed, from get_screen), or a field_key"})
			return
		}
		req.Row, req.Col = *body.Row, *body.Col
	}
	if req.Row < 0 || req.Col < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "row/col must be >= 0"})
		return
	}
	// Reject control characters that could be interpreted as s3270 commands.
	if host.ContainsForbiddenFieldText(req.Text) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text must not contain CR/LF/TAB"})
		return
	}

	result, err := app.writeFieldChecked(s, app.sessionHost(s), req)
	if err != nil {
		if host.IsFieldWriteError(err) {
			// The request was understood and the screen does not permit it.
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "field": fieldWriteJSON(result)})
}

// errUnrecognizedAID marks a submit that named a key which does not exist.
type errUnrecognizedAID struct{ aid string }

func (e errUnrecognizedAID) Error() string { return fmt.Sprintf("unrecognized aid %q", e.aid) }

// resolveSubmitAID reads the key a submit should end with: Enter when none is
// named.
func resolveSubmitAID(aid string) (string, error) {
	aid = strings.TrimSpace(aid)
	if aid == "" {
		return "Enter", nil
	}
	normalized, ok := normalizeKey(aid)
	if !ok {
		return "", errUnrecognizedAID{aid: aid}
	}
	return normalized, nil
}

// submitScreen is the whole of "submit": write any fields still marked
// changed, press the key, and wait for the host to answer.
//
// There is one of these on purpose. The REST API's submit did all three; the
// chat panel's did only the first, and since write_field types straight into
// the terminal there was never anything marked changed to write — so
// submit_screen reported success and pressed nothing. Both routes now come
// here. SendKey waits for the keyboard to unlock after an AID key, so the
// screen read next is the one the key produced.
func (app *App) submitScreen(s *session.Session, h host.Host, aid string) error {
	if err := h.SubmitScreen(); err != nil {
		return err
	}
	return app.sendRecordedKey(s, h, aid)
}

// ScreenSubmitHandler presses Enter — or the AID key named in the body — and
// returns the screen that comes back. Used by the submit_screen tool.
//
// Body (optional): {"aid": "PF(5)", "confirmed": false}.
func (app *App) ScreenSubmitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		AID       string `json:"aid"`
		Confirmed bool   `json:"confirmed"`
	}
	_ = c.ShouldBindJSON(&body) // body is optional
	aid, err := resolveSubmitAID(body.AID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Submitting with PF3 is pressing PF3.
	if dangerousKeyRefusal(c, aid, body.Confirmed) {
		return
	}
	h := app.sessionHost(s)
	if err := app.submitScreen(s, h, aid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	screen, err := app.screenAfterAction(s, h)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "aid": aid, "screen": screen})
}

// ScreenConnectHandler establishes (or replaces) the current session's host
// connection. Used by the Copilot connect_session tool so a chat request
// like "log in to the payments app" doesn't dead-end when nothing is
// connected yet, or when the user wants to switch targets mid-conversation.
// Reuses resetSessionHost (already used by the workflow-driven reconnect
// path), which validates the hostname and stops any existing host first.
func (app *App) ScreenConnectHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hostname := strings.TrimSpace(body.Hostname)
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hostname is required"})
		return
	}
	if err := app.resetSessionHost(c, s, hostname); err != nil {
		// A policy refusal is not a retryable upstream failure; the REST
		// session-creation path already answers it with 403 the same way.
		c.JSON(connectFailureStatus(err), gin.H{"error": err.Error()})
		return
	}
	snap := app.snapshotSession(s)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"targetHost": snap.TargetHost,
		"targetPort": snap.TargetPort,
	})
}

// ScreenCursorHandler moves the host cursor to (row, col) without sending a
// key or writing a field. Used by the Copilot move_cursor tool — MoveCursor
// already existed on host.Host but was never exposed as a tool, so the
// model had no way to position the cursor ahead of a key that acts relative
// to it (e.g. a bare Tab/BackTab, or a host action keyed off cursor
// position rather than a specific field).
func (app *App) ScreenCursorHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		Row int `json:"row"`
		Col int `json:"col"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Row < 0 || body.Col < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "row/col must be >= 0"})
		return
	}
	if err := app.sessionHost(s).MoveCursor(body.Row, body.Col); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

const (
	screenWaitDefaultTimeout = 5 * time.Second
	screenWaitMaxTimeout     = 15 * time.Second
	screenWaitPollInterval   = 150 * time.Millisecond
)

// screenWaitTimeout turns a caller's timeout_ms into the time to wait: the
// default when none is given, and the maximum when more than the maximum is
// asked for. A request for longer than the cap used to be treated as no
// request at all and given the five-second default — a third of what the
// caller was entitled to, for having asked for more.
func screenWaitTimeout(timeoutMs int) time.Duration {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	switch {
	case timeout <= 0:
		return screenWaitDefaultTimeout
	case timeout > screenWaitMaxTimeout:
		return screenWaitMaxTimeout
	}
	return timeout
}

// keyboardState names the keyboard's condition in words: "unlocked" when input
// is accepted, "locked" while the host is processing, "error" after an
// operator error, which stays until Reset is pressed. A screen with no status
// line to read is reported as unlocked, which is what its absence means on
// every host that has one.
func keyboardState(s *host.Screen) string {
	if s == nil {
		return "unlocked"
	}
	state, ok := s.StatusKeyboardState()
	if !ok {
		return "unlocked"
	}
	switch state {
	case "U":
		return "unlocked"
	case "E":
		return "error"
	default:
		return "locked"
	}
}

// ScreenWaitHandler polls the host status line until the keyboard unlocks
// (the transaction has finished processing) or timeout_ms elapses, then
// returns the screen at that point. Used by the wait_for_unlock tool.
//
// An operator error is not a wait. The keyboard is inhibited until Reset and
// no amount of waiting changes that, so it is reported at once as
// keyboard:"error" with unlocked:false — it used to be reported as unlocked,
// because the test was "not L".
func (app *App) ScreenWaitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		TimeoutMs int `json:"timeout_ms"`
	}
	_ = c.ShouldBindJSON(&body) // body is optional; zero value falls back to the default

	h := app.sessionHost(s)
	ctx := c.Request.Context()
	deadline := time.Now().Add(screenWaitTimeout(body.TimeoutMs))
	for {
		if err := h.UpdateScreen(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "update screen: " + err.Error()})
			return
		}
		screen := hostScreenSnapshot(h)
		if screen == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "screen unavailable"})
			return
		}
		keyboard := keyboardState(screen)
		unlocked := keyboard == "unlocked"
		timedOut := keyboard == "locked" && !time.Now().Before(deadline)
		if keyboard != "locked" || timedOut {
			out := gin.H{
				"unlocked": unlocked,
				"timedOut": timedOut,
				"keyboard": keyboard,
				"screen":   app.sessionScreenJSON(s, screen),
			}
			if keyboard == "error" {
				out["hint"] = "The keyboard is inhibited by an operator error. Send the Reset key to release it."
			}
			c.JSON(http.StatusOK, out)
			return
		}
		// A caller that has gone away is not waiting for anything; without
		// this the loop slept on for up to the full timeout holding a
		// goroutine and polling the host on behalf of nobody.
		select {
		case <-ctx.Done():
			return
		case <-time.After(screenWaitPollInterval):
		}
	}
}

// ScreenJSONHandler returns the current screen as a JSON object suitable
// for handing to a model via the get_screen tool. It mirrors the shape of
// (*host.Screen) but flattens the Buffer into a plain text field plus a
// per-field list so an LLM can reason about it without parsing internal
// rune arrays.
func (app *App) ScreenJSONHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	screen, err := app.screenAfterAction(s, app.sessionHost(s))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, screen)
}

// screenToJSON renders a screen for a model: text and fields with hidden
// values removed, the keyboard's state in a word, and a label for each field.
func screenToJSON(s *host.Screen) gin.H {
	if s == nil {
		return gin.H{}
	}
	labels := fieldLabels(s)
	fields := make([]gin.H, 0, len(s.Fields))
	for _, f := range s.Fields {
		if f == nil {
			continue
		}
		// Hidden (e.g. password) fields never leave the server: their typed
		// value must not reach the AI provider or be echoed back to the
		// browser, where it would land in the tool-card transcript and
		// persist in localStorage. See screen_redaction.go.
		entry := gin.H{
			"row":       f.StartY,
			"col":       f.StartX,
			"endRow":    f.EndY,
			"endCol":    f.EndX,
			"value":     visibleFieldValue(f),
			"protected": f.IsProtected(),
			"numeric":   f.IsNumeric(),
			"hidden":    f.IsHidden(),
			"length":    fieldLength(f),
		}
		if label := labels[f]; label != "" {
			entry["label"] = label
		}
		fields = append(fields, entry)
	}
	cursorRow, cursorCol, hasCursor := s.StatusCursor()
	out := gin.H{
		"width":     s.Width,
		"height":    s.Height,
		"text":      redactHiddenFieldText(s),
		"fields":    fields,
		"formatted": s.IsFormatted,
		"status":    strings.TrimSpace(s.Status),
		"keyboard":  keyboardState(s),
		"hasCursor": hasCursor,
	}
	if hasCursor {
		out["cursorRow"] = cursorRow
		out["cursorCol"] = cursorCol
	}
	return out
}

// redactHiddenFieldText and fieldLength live in screen_redaction.go, beside
// the single definition of what a hidden field may disclose.
