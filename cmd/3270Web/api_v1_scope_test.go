package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// scopedTestApp returns an App with one live mock session plus a router
// carrying the full /api/v1 surface, and the token callers must present.
func scopedTestApp(t *testing.T) (*App, *gin.Engine, *session.Session, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const token = "test-token"
	t.Setenv("API_TOKEN", token)

	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
		baseDir:        t.TempDir(),
	}

	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	sess := app.SessionManager.CreateSession(mock)

	r := gin.New()
	app.registerAPIv1(r)
	return app, r, sess, token
}

func doScoped(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestAPISessionScopeRequiresTokenAndSession covers the two ways a
// session-scoped call is rejected before it reaches a handler: no bearer
// token, and a session id that names nothing.
func TestAPISessionScopeRequiresTokenAndSession(t *testing.T) {
	_, r, sess, token := scopedTestApp(t)

	if w := doScoped(r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/screen.json", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401 (body %s)", w.Code, w.Body.String())
	}
	if w := doScoped(r, http.MethodGet, "/api/v1/sessions/does-not-exist/screen.json", token); w.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
}

// TestAPISessionScopeReachesCookieHandlers is the point of the shim: a
// handler that reads the session cookie is served correctly when the
// session is named in the path instead.
func TestAPISessionScopeReachesCookieHandlers(t *testing.T) {
	_, r, sess, token := scopedTestApp(t)

	w := doScoped(r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/screen.json", token)
	if w.Code != http.StatusOK {
		t.Fatalf("screen.json: status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	// The handler resolved a session rather than falling through to the
	// "no session" branch, which would have been a 401.
	if body := w.Body.String(); !strings.Contains(body, "width") {
		t.Errorf("screen.json body does not look like a screen: %s", body)
	}
}

// TestAPIv1DenyList pins what the token surface deliberately does not
// expose. Everything here is administrative or filesystem-facing and is
// not needed to drive an application, so it stays on the browser surface
// where an operator is present. A route added to the scoped group by
// accident shows up as a failure here.
func TestAPIv1DenyList(t *testing.T) {
	_, r, sess, token := scopedTestApp(t)

	denied := []struct{ method, path string }{
		{http.MethodGet, "/logs"},
		{http.MethodGet, "/logs/download"},
		{http.MethodPost, "/logs/clear"},
		{http.MethodPost, "/app/restart"},
		{http.MethodPost, "/transfer/send"},
		{http.MethodPost, "/transfer/receive"},
		{http.MethodGet, "/api/settings"},
		{http.MethodPost, "/api/themes/save"},
		{http.MethodPost, "/workflow/play"},
		{http.MethodPost, "/chaos/runs/delete"},
	}

	// Running a Guided Business Task is deliberately *on* the token surface —
	// see registerAPIv1 — so it is not in the list above. It is named here so
	// that removing it from the API reads as a decision rather than as a
	// route quietly going missing.
	if w := doScoped(r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/tasks/run", token); w.Code == http.StatusNotFound {
		t.Error("POST /api/v1/sessions/:id/tasks/run should be part of the token API")
	}

	for _, d := range denied {
		// Both the bare path under /api/v1 and the session-scoped form.
		for _, p := range []string{"/api/v1" + d.path, "/api/v1/sessions/" + sess.ID + d.path} {
			if w := doScoped(r, d.method, p, token); w.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want 404 — this route must not be on the token surface", d.method, p, w.Code)
			}
		}
	}
}

// TestAPICreateSessionSampleAppGate covers both sides of ALLOW_SAMPLE_APPS.
// Refusing outright meant the only way to exercise the headless API was
// against a real host; allowing it unconditionally would let any token
// holder start a listener.
func TestAPICreateSessionSampleAppGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("refused by default", func(t *testing.T) {
		t.Setenv("ALLOW_SAMPLE_APPS", "")
		app := &App{SessionManager: session.NewManager()}
		r := gin.New()
		r.POST("/api/v1/sessions", app.APICreateSession)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
			strings.NewReader(`{"host":"sampleapp:app1:3270"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "ALLOW_SAMPLE_APPS") {
			t.Errorf("error should name the flag that enables this, got %s", w.Body.String())
		}
	})

	t.Run("hostname recognition", func(t *testing.T) {
		for _, h := range []string{"sampleapp:app1", "sampleapp:app2:3270", "mock", "demo"} {
			if !isSampleAppHostname(h) {
				t.Errorf("isSampleAppHostname(%q) = false, want true", h)
			}
		}
		for _, h := range []string{"mvs.example.com:992", "10.1.2.3:23"} {
			if isSampleAppHostname(h) {
				t.Errorf("isSampleAppHostname(%q) = true, want false", h)
			}
		}
	})

	t.Run("flag parsing", func(t *testing.T) {
		for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
			t.Setenv("ALLOW_SAMPLE_APPS", v)
			if !sampleAppsAllowed() {
				t.Errorf("ALLOW_SAMPLE_APPS=%q should enable sample apps", v)
			}
		}
		for _, v := range []string{"", "0", "false", "no", "off", "maybe"} {
			t.Setenv("ALLOW_SAMPLE_APPS", v)
			if sampleAppsAllowed() {
				t.Errorf("ALLOW_SAMPLE_APPS=%q should not enable sample apps", v)
			}
		}
	})
}
