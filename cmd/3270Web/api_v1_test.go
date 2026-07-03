package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/session"
)

// TestAPICreateSessionRejectsInvalidHostnameWith400 guards a status-code fix:
// an invalid hostname is a client error, but APICreateSession used to map
// every startHostSession failure — including this one — to 502, indistinguishable
// from a real connection failure to a validly-formed host.
func TestAPICreateSessionRejectsInvalidHostnameWith400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}
	r := gin.New()
	r.POST("/api/v1/sessions", app.APICreateSession)

	body, _ := json.Marshal(map[string]string{"host": "169.254.169.254:80"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestAPIDeleteSessionIsIdempotent guards a status-code fix: deleting a
// session id that doesn't exist (never existed, or a repeat of an earlier
// delete) used to 404. DELETE is supposed to be idempotent — a client
// retrying a dropped response shouldn't see an error for a delete that
// already took effect.
func TestAPIDeleteSessionIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}
	r := gin.New()
	r.DELETE("/api/v1/sessions/:id", app.APIDeleteSession)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	// A second delete of the same (never-existent) id must also succeed.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/does-not-exist", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second delete status = %d, want %d, body=%s", w2.Code, http.StatusOK, w2.Body.String())
	}
}
