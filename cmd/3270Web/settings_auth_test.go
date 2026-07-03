package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestSettingsHandler_RequiresSessionEvenFromLoopback guards against the
// isLoopbackClient bypass: the server only ever binds to 127.0.0.1, so
// every request it receives (including httptest's) originates from
// loopback, which used to be OR'd into the auth check and made it always
// pass regardless of session state. GET /api/settings?includeSensitive=true
// must now require a real session and must never leak S3270_KEY_PASSWORD
// to a caller without one.
func TestSettingsHandler_RequiresSessionEvenFromLoopback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("S3270_KEY_PASSWORD=super-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{
		SessionManager: session.NewManager(),
		envPath:        envPath,
	}

	r := gin.New()
	r.GET("/api/settings", app.SettingsHandler)

	// No session cookie: must be rejected, and the secret must not appear
	// anywhere in the response body. RemoteAddr is set explicitly to
	// loopback to match production reality (the server only ever binds to
	// 127.0.0.1, so every real request it receives has a loopback
	// RemoteAddr) — httptest.NewRequest's default placeholder RemoteAddr
	// is NOT loopback and would silently fail to exercise the bypass this
	// test guards against.
	req := httptest.NewRequest(http.MethodGet, "/api/settings?includeSensitive=true", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-session request: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if strings.Contains(w.Body.String(), "super-secret") {
		t.Fatalf("secret leaked to unauthenticated caller: %s", w.Body.String())
	}

	// With a valid session, the endpoint should work (session is now the
	// sole, meaningful gate).
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	req2 := httptest.NewRequest(http.MethodGet, "/api/settings?includeSensitive=true", nil)
	req2.RemoteAddr = "127.0.0.1:54321"
	req2.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("session request: status = %d, want %d, body=%s", w2.Code, http.StatusOK, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "super-secret") {
		t.Fatalf("expected includeSensitive with a valid session to return the real value, got: %s", w2.Body.String())
	}
}

// TestRestartHandler_RequiresSession guards the same bypass for
// POST /app/restart: without hasSession's fix this endpoint (a forced
// process restart / DoS) was reachable by any loopback caller with zero
// session, which is every caller since the server only binds to 127.0.0.1.
func TestRestartHandler_RequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}

	r := gin.New()
	r.POST("/app/restart", app.RestartHandler)

	req := httptest.NewRequest(http.MethodPost, "/app/restart", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-session restart request: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestThemeListHandler_RequiresSession guards GET /api/themes, which has no
// CSRF-style defense at all (safe method) — the session check is the only
// gate.
func TestThemeListHandler_RequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}

	r := gin.New()
	r.GET("/api/themes", app.ThemeListHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/themes", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-session themes request: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
