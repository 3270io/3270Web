package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestScreenKeyHandlerRejectsUnrecognizedKey guards the Copilot send_key
// tool's backend: before this fix, an unrecognized key (a model typo like
// "PF25", or any string normalizeKey didn't recognize) silently pressed
// Enter and submitted whatever was on screen instead of failing loudly.
func TestScreenKeyHandlerRejectsUnrecognizedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.POST("/screen/key", app.ScreenKeyHandler)

	body, _ := json.Marshal(map[string]string{"key": "PF25"})
	req := httptest.NewRequest(http.MethodPost, "/screen/key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if len(mh.Commands) != 0 {
		t.Fatalf("expected no host commands for a rejected key, got %v", mh.Commands)
	}
}

// TestScreenKeyHandlerAcceptsRecognizedKey confirms the pre-existing valid
// path is unaffected by the rejection fix.
func TestScreenKeyHandlerAcceptsRecognizedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.POST("/screen/key", app.ScreenKeyHandler)

	body, _ := json.Marshal(map[string]string{"key": "pf3"})
	req := httptest.NewRequest(http.MethodPost, "/screen/key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Key != "PF(3)" {
		t.Fatalf("key = %q, want %q", resp.Key, "PF(3)")
	}
}

// TestAPISendKeyRejectsUnrecognizedKey mirrors the same fix on the REST
// API's /api/v1/sessions/:id/key endpoint.
func TestAPISendKeyRejectsUnrecognizedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.POST("/api/v1/sessions/:id/key", app.APISendKey)

	body, _ := json.Marshal(map[string]string{"key": "not-a-real-key"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}
