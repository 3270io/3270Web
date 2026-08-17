// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/config"
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

// TestHealthHandlerReportsUnavailableWhenS3270Missing guards the readiness
// check added alongside this test: HealthHandler used to report "ok"
// unconditionally, so an orchestrator would keep routing traffic to a
// container that could never actually start a host session. To reliably
// make every fallback in resolveS3270Path fail (including the embedded
// binary, which otherwise always succeeds and makes this untestable), this
// points TMPDIR at a regular file instead of a directory: extraction's
// os.MkdirAll then fails structurally (ENOTDIR) regardless of privilege,
// the same trick used for the atomic-write failure tests.
func TestHealthHandlerReportsUnavailableWhenS3270Missing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR/PATH fallback semantics differ on windows")
	}
	gin.SetMode(gin.TestMode)

	fakeTmpDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(fakeTmpDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake TMPDIR file: %v", err)
	}
	t.Setenv("TMPDIR", fakeTmpDir)
	t.Setenv("PATH", t.TempDir())

	execDir := t.TempDir()
	app := &App{Config: &config.Config{ExecPath: execDir}}
	r := gin.New()
	r.GET("/healthz", app.HealthHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusServiceUnavailable, w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("could not decode body %q: %v", w.Body.String(), err)
	}
	if body["status"] != "unavailable" {
		t.Errorf("expected status \"unavailable\", got %q", body["status"])
	}
}
