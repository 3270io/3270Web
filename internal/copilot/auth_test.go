package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *AuthManager {
	t.Helper()
	dir := t.TempDir()
	return NewAuthManager(filepath.Join(dir, "copilot-auth.json"))
}

func TestAuthManagerStartsLoggedOut(t *testing.T) {
	m := newTestManager(t)
	if m.LoggedIn() {
		t.Fatalf("fresh manager reports logged in")
	}
	if _, _, err := m.CopilotToken(context.Background()); err == nil {
		t.Fatalf("CopilotToken should fail when logged out, got nil error")
	}
}

func TestPollDeviceLoginPersistsToken(t *testing.T) {
	m := newTestManager(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if r.Form.Get("device_code") != "DEV" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "OAUTH_TOKEN",
			"token_type":   "bearer",
			"scope":        "read:user",
		})
	}))
	defer srv.Close()
	m.githubHost = srv.URL

	res, err := m.PollDeviceLogin(context.Background(), "DEV")
	if err != nil {
		t.Fatalf("poll error: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("expected success status, got %q (err=%q)", res.Status, res.Error)
	}
	if !m.LoggedIn() {
		t.Fatalf("manager did not record logged-in state")
	}

	// Reload from disk and confirm OAuth token was persisted.
	m2 := NewAuthManager(m.path)
	if !m2.LoggedIn() {
		t.Fatalf("reloaded manager did not see persisted token")
	}
}

func TestCopilotTokenRefresh(t *testing.T) {
	m := newTestManager(t)
	m.cache.OAuth = "OAUTH_TOKEN"
	m.loaded = true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/copilot_internal/v2/token" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer OAUTH_TOKEN" {
			http.Error(w, "bad auth: "+got, 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "COPILOT_TOKEN",
			"expires_at": time.Now().Add(30 * time.Minute).Unix(),
			"endpoints":  map[string]string{"api": "https://api.example.test"},
		})
	}))
	defer srv.Close()
	m.apiHost = srv.URL

	tok, endpoint, err := m.CopilotToken(context.Background())
	if err != nil {
		t.Fatalf("CopilotToken: %v", err)
	}
	if tok != "COPILOT_TOKEN" {
		t.Fatalf("token = %q, want COPILOT_TOKEN", tok)
	}
	if endpoint != "https://api.example.test" {
		t.Fatalf("endpoint = %q, want https://api.example.test", endpoint)
	}

	// Second call within the expiry window must not re-hit the server.
	m.apiHost = "http://127.0.0.1:1" // unreachable
	tok2, _, err := m.CopilotToken(context.Background())
	if err != nil {
		t.Fatalf("cached CopilotToken: %v", err)
	}
	if tok2 != tok {
		t.Fatalf("cached token mismatch")
	}
}

func TestCopilotTokenRefreshSurfacesError(t *testing.T) {
	m := newTestManager(t)
	m.cache.OAuth = "OAUTH_TOKEN"
	m.loaded = true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error": "bad token"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	m.apiHost = srv.URL

	_, _, err := m.CopilotToken(context.Background())
	if err == nil {
		t.Fatalf("expected error from 401 response, got nil")
	}
}

func TestLogoutClearsCache(t *testing.T) {
	m := newTestManager(t)
	m.cache.OAuth = "x"
	m.loaded = true
	if err := m.Logout(); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if m.LoggedIn() {
		t.Fatalf("manager still logged in after logout")
	}
}

func TestStartDeviceLogin(t *testing.T) {
	m := newTestManager(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/device/code" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if r.Form.Get("client_id") == "" {
			http.Error(w, "missing client_id", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "DEV",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	}))
	defer srv.Close()
	m.githubHost = srv.URL

	dc, err := m.StartDeviceLogin(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if dc.UserCode != "ABCD-1234" {
		t.Fatalf("user_code = %q", dc.UserCode)
	}
	if dc.Interval != 5 {
		t.Fatalf("interval = %d", dc.Interval)
	}
	u, err := url.Parse(dc.VerificationURI)
	if err != nil || u.Host == "" {
		t.Fatalf("verification_uri = %q (%v)", dc.VerificationURI, err)
	}
}

func TestPollDeviceLoginPending(t *testing.T) {
	m := newTestManager(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	defer srv.Close()
	m.githubHost = srv.URL

	res, err := m.PollDeviceLogin(context.Background(), "DEV")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if res.Status != "pending" {
		t.Fatalf("status = %q, want pending", res.Status)
	}
}

func TestSetEnterpriseInvalidatesToken(t *testing.T) {
	m := newTestManager(t)
	m.cache.OAuth = "x"
	m.cache.CopilotToken = "y"
	m.cache.CopilotExpires = time.Now().Add(time.Hour).UnixMilli()
	m.cache.APIEndpoint = "https://old"
	m.loaded = true

	if err := m.SetEnterpriseURL("ghe.example.com"); err != nil {
		t.Fatalf("SetEnterpriseURL: %v", err)
	}
	if m.cache.CopilotToken != "" || m.cache.APIEndpoint != "" {
		t.Fatalf("token cache not invalidated after enterprise change")
	}
	if got := m.EnterpriseURL(); got != "ghe.example.com" {
		t.Fatalf("EnterpriseURL = %q", got)
	}
}

func TestDefaultAuthPathHonorsEnv(t *testing.T) {
	t.Setenv("COPILOT_AUTH_PATH", "/tmp/copilot-foo.json")
	p, err := DefaultAuthPath()
	if err != nil {
		t.Fatalf("DefaultAuthPath: %v", err)
	}
	if !strings.HasSuffix(p, "copilot-foo.json") {
		t.Fatalf("path = %q", p)
	}
}
