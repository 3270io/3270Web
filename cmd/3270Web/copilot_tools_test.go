// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func newCopilotToolTestApp(t *testing.T) (*App, *host.MockHost, *session.Session) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = &host.Screen{Width: 80, Height: 24, IsFormatted: true, Buffer: make([][]rune, 24)}
	for i := range mh.Screen.Buffer {
		mh.Screen.Buffer[i] = make([]rune, 80)
	}
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)
	return app, mh, sess
}

func doJSON(r http.Handler, method, path string, sessID string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if sessID != "" {
		req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sessID})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The success path (a real host connection) needs a real s3270 subprocess —
// see TestResetSessionHostRejectsRestrictedHostname in main_test.go, which
// establishes the same precedent of only unit-testing the rejection path
// here and leaving the real-connection path to Playwright/manual E2E
// verification against a live server.

// TestScreenConnectHandlerRejectsRestrictedHostname confirms the tool goes
// through the same hostname validation as the workflow-driven reconnect
// path (resetSessionHost) — no separate, weaker validation path for
// Copilot-initiated connects.
func TestScreenConnectHandlerRejectsRestrictedHostname(t *testing.T) {
	app, _, sess := newCopilotToolTestApp(t)
	r := gin.New()
	r.POST("/screen/connect", app.ScreenConnectHandler)

	w := doJSON(r, http.MethodPost, "/screen/connect", sess.ID, map[string]string{"hostname": "169.254.169.254:80"})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadGateway, w.Body.String())
	}
}

// TestScreenCursorHandlerMovesCursor guards the Copilot move_cursor tool —
// MoveCursor already existed on host.Host but was never reachable from a
// tool call.
func TestScreenCursorHandlerMovesCursor(t *testing.T) {
	app, mh, sess := newCopilotToolTestApp(t)
	r := gin.New()
	r.POST("/screen/cursor", app.ScreenCursorHandler)

	w := doJSON(r, http.MethodPost, "/screen/cursor", sess.ID, map[string]int{"row": 5, "col": 12})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if mh.Screen.CursorY != 5 || mh.Screen.CursorX != 12 {
		t.Errorf("cursor = (%d,%d), want (5,12)", mh.Screen.CursorY, mh.Screen.CursorX)
	}
}

func TestScreenCursorHandlerRejectsNegativeCoordinates(t *testing.T) {
	app, _, sess := newCopilotToolTestApp(t)
	r := gin.New()
	r.POST("/screen/cursor", app.ScreenCursorHandler)

	w := doJSON(r, http.MethodPost, "/screen/cursor", sess.ID, map[string]int{"row": -1, "col": 0})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestScreenWaitHandlerReturnsImmediatelyWhenUnlocked guards the Copilot
// wait_for_unlock tool's common case: the keyboard is already unlocked, so
// it should return on the first poll without waiting out the timeout.
func TestScreenWaitHandlerReturnsImmediatelyWhenUnlocked(t *testing.T) {
	app, mh, sess := newCopilotToolTestApp(t)
	mh.Screen.Status = "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	r := gin.New()
	r.POST("/screen/wait", app.ScreenWaitHandler)

	start := time.Now()
	w := doJSON(r, http.MethodPost, "/screen/wait", sess.ID, map[string]int{"timeout_ms": 5000})
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Unlocked bool `json:"unlocked"`
		TimedOut bool `json:"timedOut"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Unlocked || resp.TimedOut {
		t.Errorf("expected unlocked=true timedOut=false, got %+v", resp)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected an immediate return for an already-unlocked keyboard, took %v", elapsed)
	}
}

// TestScreenWaitHandlerTimesOutWhenLocked guards the timeout path: a
// keyboard that never unlocks within timeout_ms must be reported as
// timedOut rather than hanging forever.
func TestScreenWaitHandlerTimesOutWhenLocked(t *testing.T) {
	app, mh, sess := newCopilotToolTestApp(t)
	mh.Screen.Status = "L F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	r := gin.New()
	r.POST("/screen/wait", app.ScreenWaitHandler)

	start := time.Now()
	w := doJSON(r, http.MethodPost, "/screen/wait", sess.ID, map[string]int{"timeout_ms": 300})
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Unlocked bool `json:"unlocked"`
		TimedOut bool `json:"timedOut"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Unlocked || !resp.TimedOut {
		t.Errorf("expected unlocked=false timedOut=true, got %+v", resp)
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("expected to actually wait out the timeout (~300ms), took %v", elapsed)
	}
}
