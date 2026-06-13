package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app := &App{}
	r := gin.New()
	r.GET("/healthz", app.HealthHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("could not decode body %q: %v", w.Body.String(), err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status \"ok\", got %q", body["status"])
	}
	if body["version"] != appVersion {
		t.Errorf("expected version %q, got %q", appVersion, body["version"])
	}
}
