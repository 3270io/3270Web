package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogAccessRestrictedByDefault(t *testing.T) {
	// Ensure env var is unset
	os.Unsetenv("ALLOW_LOG_ACCESS")

	gin.SetMode(gin.TestMode)
	app := &App{} // We don't need a session for this test as the check happens before session check

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		path    string
	}{
		{"LogsHandler", app.LogsHandler, "/logs"},
		{"LogsDownloadHandler", app.LogsDownloadHandler, "/logs/download"},
		{"LogsClearHandler", app.LogsClearHandler, "/logs/clear"},
		{"LogsToggleHandler", app.LogsToggleHandler, "/logs/toggle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.path, nil)

			tt.handler(c)

			if w.Code != http.StatusForbidden {
				t.Errorf("Expected status 403 Forbidden, got %d", w.Code)
			}
		})
	}
}

func TestLogAccessAllowedWithEnv(t *testing.T) {
	os.Setenv("ALLOW_LOG_ACCESS", "true")
	defer os.Unsetenv("ALLOW_LOG_ACCESS")

	gin.SetMode(gin.TestMode)
	app := &App{}
	// Note: these will fail with 401 or redirect because session is missing,
	// but that proves they passed the security check.

	tests := []struct {
		name       string
		handler    gin.HandlerFunc
		path       string
		wantStatus int // 401 or 302
	}{
		{"LogsHandler", app.LogsHandler, "/logs", http.StatusUnauthorized},
		// LogsDownloadHandler redirects to / if no session
		{"LogsDownloadHandler", app.LogsDownloadHandler, "/logs/download", http.StatusFound},
		{"LogsClearHandler", app.LogsClearHandler, "/logs/clear", http.StatusUnauthorized},
		{"LogsToggleHandler", app.LogsToggleHandler, "/logs/toggle", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.path, nil)

			tt.handler(c)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}
