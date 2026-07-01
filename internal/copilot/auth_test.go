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
	"unicode/utf8"
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

func TestCopilotTokenMissingExpiryIsNotInstantlyStale(t *testing.T) {
	m := newTestManager(t)
	m.cache.OAuth = "OAUTH_TOKEN"
	m.loaded = true

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		// Omit expires_at entirely (decodes as 0). The old code computed a
		// negative expiry, so the freshly-fetched token was treated as expired
		// and re-fetched on every call.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":     "COPILOT_TOKEN",
			"endpoints": map[string]string{"api": "https://api.example.test"},
		})
	}))
	defer srv.Close()
	m.apiHost = srv.URL

	if _, _, err := m.CopilotToken(context.Background()); err != nil {
		t.Fatalf("CopilotToken: %v", err)
	}
	if m.cache.CopilotExpires <= time.Now().UnixMilli() {
		t.Fatalf("CopilotExpires = %d, want a future timestamp", m.cache.CopilotExpires)
	}
	// A second call must be served from cache, not trigger another refresh.
	if _, _, err := m.CopilotToken(context.Background()); err != nil {
		t.Fatalf("cached CopilotToken: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server hit %d times, want 1 (token should be cached)", hits)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	// Truncating between bytes of a multibyte rune must not corrupt the string.
	s := "héllo wörld" // contains multibyte runes
	got := truncate(s, 4)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	if r := []rune(got); len(r) != 5 { // 4 runes + ellipsis
		t.Fatalf("truncate(%q, 4) = %q (%d runes), want 4 runes + ellipsis", s, got, len(r))
	}
	if short := truncate("ab", 5); short != "ab" {
		t.Fatalf("truncate of short string changed it: %q", short)
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

func TestSetEnterpriseURLRejectsNonHostValues(t *testing.T) {
	m := newTestManager(t)
	cases := []string{
		"https://evil.example.com",     // scheme
		"ghe.example.com/steal",        // path
		"ghe.example.com\r\nX-Evil: 1", // header injection via CRLF
		"attacker.com@ghe.example.com", // userinfo-style smuggling
		"169.254.169.254",              // cloud metadata address
		"[fe80::1]",                    // link-local IPv6
	}
	for _, host := range cases {
		if err := m.SetEnterpriseURL(host); err == nil {
			t.Errorf("SetEnterpriseURL(%q) = nil error, want rejection", host)
		}
	}
	// The invalid attempts must not have overwritten any prior valid value.
	if got := m.EnterpriseURL(); got != "" {
		t.Fatalf("EnterpriseURL = %q after rejected inputs, want empty", got)
	}
}

func TestSetEnterpriseURLAcceptsBareHostnames(t *testing.T) {
	m := newTestManager(t)
	for _, host := range []string{"ghe.example.com", "ghe.internal:8443", "localhost"} {
		if err := m.SetEnterpriseURL(host); err != nil {
			t.Errorf("SetEnterpriseURL(%q) = %v, want success", host, err)
		}
	}
}

func TestPollDeviceLoginNonOKStatusSurfacesError(t *testing.T) {
	m := newTestManager(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	m.githubHost = srv.URL

	res, err := m.PollDeviceLogin(context.Background(), "DEV")
	if err == nil {
		t.Fatalf("expected error from 502 response, got nil")
	}
	if res.Status != "error" {
		t.Fatalf("status = %q, want error", res.Status)
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
