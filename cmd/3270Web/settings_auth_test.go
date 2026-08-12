package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
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
	// Secrets are write-only. A session is not authorization to read the
	// mainframe key password back out, and includeSensitive=true no longer
	// means anything — the caller learns only that the value is set.
	if strings.Contains(w2.Body.String(), "super-secret") {
		t.Fatalf("secret returned to a session holder: %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), settingsSecretPlaceholder) {
		t.Fatalf("expected the secret to be masked, got: %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"masked":["S3270_KEY_PASSWORD"]`) {
		t.Fatalf("expected S3270_KEY_PASSWORD reported as masked, got: %s", w2.Body.String())
	}
}

// A masked secret is echoed back by the settings form on save. Persisting the
// placeholder would replace the real password with "********", so the write
// path has to recognise it and leave the stored value alone.
func TestUpdateSettings_PlaceholderDoesNotOverwriteSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("S3270_KEY_PASSWORD=super-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{SessionManager: session.NewManager(), envPath: envPath}

	r := gin.New()
	r.POST("/api/settings", app.SettingsHandler)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := post(`{"settings":{"S3270_KEY_PASSWORD":"` + settingsSecretPlaceholder + `"}}`); w.Code != http.StatusOK {
		t.Fatalf("placeholder save: status = %d, body=%s", w.Code, w.Body.String())
	}
	stored, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "S3270_KEY_PASSWORD=super-secret") {
		t.Fatalf("placeholder overwrote the stored secret: %s", stored)
	}

	// A genuinely new value must still be written.
	if w := post(`{"settings":{"S3270_KEY_PASSWORD":"rotated"}}`); w.Code != http.StatusOK {
		t.Fatalf("rotate save: status = %d, body=%s", w.Code, w.Body.String())
	}
	stored, err = os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "S3270_KEY_PASSWORD=rotated") {
		t.Fatalf("new secret was not written: %s", stored)
	}
}

// POST /api/settings used to accept any key at all, so a caller could write
// arbitrary variables into .env — which LoadDotEnv promotes into the process
// environment on the next start. API_TOKEN is the one that matters most: it is
// unset by default, so naming it here granted the caller the instance-wide API
// credential after a restart they could also trigger themselves.
func TestUpdateSettings_RejectsUnknownAndDeniedKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("APP_USE_KEYPAD=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{SessionManager: session.NewManager(), envPath: envPath}

	r := gin.New()
	r.POST("/api/settings", app.SettingsHandler)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	for _, key := range []string{
		"API_TOKEN",
		"PATH",
		"LD_PRELOAD",
		"COPILOT_AUTH_PATH",
		"WEBUI_BIND",
		"MCP_ALLOWED_HOSTS",
		"TOTALLY_MADE_UP_KEY",
	} {
		body := `{"settings":{"` + key + `":"attacker-value"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d (body=%s)", key, w.Code, http.StatusBadRequest, w.Body.String())
		}
		stored, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(stored), key+"=") {
			t.Errorf("%s was persisted to .env: %s", key, stored)
		}
	}
}

// The UI persists per-field option lists under an APP_SETTINGS_OPTIONS_ prefix,
// so the allowlist has to admit those without admitting arbitrary keys.
func TestUpdateSettings_AllowsKnownKeysAndUIExtras(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	app := &App{SessionManager: session.NewManager(), envPath: envPath}

	r := gin.New()
	r.POST("/api/settings", app.SettingsHandler)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	body := `{"settings":{"S3270_MODEL":"3279-4-E","APP_SETTINGS_OPTIONS_S3270_MODEL":"a,b"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	stored, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"S3270_MODEL=3279-4-E", "APP_SETTINGS_OPTIONS_S3270_MODEL=a,b"} {
		if !strings.Contains(string(stored), want) {
			t.Errorf("missing %q in .env: %s", want, stored)
		}
	}

	// The prefix must not become a way to smuggle a denied key through.
	body = `{"settings":{"APP_SETTINGS_OPTIONS_API_TOKEN":"nope"}}`
	req = httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("prefixed denied key: status = %d, want 400 (body=%s)", w.Code, w.Body.String())
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
// CSRF-style defense at all (safe method). A caller who is nobody — no
// principal resolved and no session either, which in production means the
// route was mounted without Authenticate — gets nothing.
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

// An administrator is entitled to instance settings without holding a terminal
// session. The two are unrelated: settings belong to the deployment, a session
// belongs to one connection, and requiring both meant Settings answered "no
// session" on the connect page — before there is anything to connect to, which
// is exactly when somebody opens it.
func TestSettingsHandler_AdminNeedsNoTerminalSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("S3270_MODEL=3279-2-E\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{
		SessionManager: session.NewManager(),
		envPath:        envPath,
		authMode:       authz.ModeLocal,
	}

	r := gin.New()
	r.GET("/api/settings", func(c *gin.Context) {
		c.Set(principalContextKey, authz.Principal{
			UserID: "u1", Role: authz.RoleAdmin, Kind: authz.KindWeb,
		})
		app.SettingsHandler(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("admin without a terminal session: status = %d, want %d, body=%s",
			w.Code, http.StatusOK, w.Body.String())
	}
}

// The same request from an ordinary account is still refused. RequireAdmin
// stops it in production; this pins the handler's own check so the two cannot
// drift apart.
func TestSettingsHandler_NonAdminStillNeedsASession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app := &App{
		SessionManager: session.NewManager(),
		envPath:        filepath.Join(t.TempDir(), ".env"),
		authMode:       authz.ModeLocal,
	}

	r := gin.New()
	r.GET("/api/settings", func(c *gin.Context) {
		c.Set(principalContextKey, authz.Principal{
			UserID: "u2", Role: authz.RoleUser, Kind: authz.KindWeb,
		})
		app.SettingsHandler(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ordinary account without a session: status = %d, want %d",
			w.Code, http.StatusUnauthorized)
	}
}

// The default deployment has no accounts, so the single operator is
// authz.Local() and the connect page is where they open Settings. This used to
// demand a terminal session there as well, on the grounds that the cookie was
// the only signal AUTH_MODE=none had — which made Settings unusable in the one
// deployment shape most instances run, and protected nothing: the same caller
// mints a session by posting to /connect, and already reaches /admin and the
// account pages without one.
func TestSettingsHandler_AuthModeNoneAnswersTheConnectPage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("S3270_MODEL=3279-2-E\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{
		SessionManager: session.NewManager(),
		envPath:        envPath,
		authMode:       authz.ModeNone,
	}

	r := gin.New()
	r.GET("/api/settings", func(c *gin.Context) {
		// Exactly what resolvePrincipal produces under AUTH_MODE=none.
		c.Set(principalContextKey, authz.Local())
		app.SettingsHandler(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AUTH_MODE=none from the connect page: status = %d, want %d, body=%s",
			w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "S3270_MODEL") {
		t.Fatalf("expected the settings snapshot, got: %s", w.Body.String())
	}
}

// The theme picker sits in the same dialog, on the same page, with no terminal
// session either. Gating its list on one left the connect page's copy of the
// picker offering the built-in themes only, with the operator's own saved
// themes missing from a list that offers to save more.
func TestThemeListHandler_AnswersTheConnectPage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app := &App{
		SessionManager: session.NewManager(),
		baseDir:        t.TempDir(),
		authMode:       authz.ModeNone,
	}

	r := gin.New()
	r.GET("/api/themes", func(c *gin.Context) {
		c.Set(principalContextKey, authz.Local())
		app.ThemeListHandler(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/themes", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("themes without a terminal session: status = %d, want %d, body=%s",
			w.Code, http.StatusOK, w.Body.String())
	}
}

// The templates gate Settings and the log viewer on Auth.CanAdminister, so what
// that field means is load-bearing: too narrow and the default single-operator
// deployment loses its own Settings button, too wide and every ordinary account
// is offered controls the server will refuse.
func TestAuthViewCanAdminister(t *testing.T) {
	gin.SetMode(gin.TestMode)

	view := func(mode authz.Mode, principal *authz.Principal) authView {
		app := &App{authMode: mode}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if principal != nil {
			c.Set(principalContextKey, *principal)
		}
		return app.authView(c)
	}

	admin := authz.Principal{UserID: "u1", Role: authz.RoleAdmin, Kind: authz.KindWeb}
	user := authz.Principal{UserID: "u2", Role: authz.RoleUser, Kind: authz.KindWeb}

	if got := view(authz.ModeNone, nil); !got.CanAdminister {
		t.Error("AUTH_MODE=none: the single operator must be offered the admin controls")
	} else if got.Enabled {
		t.Error("AUTH_MODE=none: there is no account to show a chip for")
	}
	if got := view(authz.ModeLocal, &admin); !got.CanAdminister {
		t.Error("a signed-in administrator must be offered the admin controls")
	}
	if got := view(authz.ModeLocal, &user); got.CanAdminister {
		t.Error("an ordinary account must not be offered controls the server refuses")
	}
	if got := view(authz.ModeLocal, nil); got.CanAdminister {
		t.Error("an anonymous caller must not be offered the admin controls")
	}
}
