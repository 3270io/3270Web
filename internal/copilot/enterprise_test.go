// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/aipolicy"
)

// Changing the enterprise host used to clear the Copilot token and keep the
// OAuth one — which the next token refresh then sent, as a bearer credential,
// to the new host. Anyone able to set the host could have a GitHub login
// delivered to a server of their choosing.
func TestChangingTheEnterpriseHostDropsTheOAuthToken(t *testing.T) {
	m := NewAuthManager(filepath.Join(t.TempDir(), "auth.json"))
	m.cache.OAuth = "gho_issued_by_the_old_host"
	m.cache.CopilotToken = "copilot"
	m.cache.APIEndpoint = "https://api.githubcopilot.com"
	m.loaded = true

	if err := m.SetEnterpriseURL("ghe.example.com"); err != nil {
		t.Fatalf("SetEnterpriseURL: %v", err)
	}
	if m.cache.OAuth != "" {
		t.Fatalf("the OAuth token survived a change of host: %q", m.cache.OAuth)
	}
	if m.LoggedIn() {
		t.Error("a change of host should leave the user signed out")
	}

	// And from one enterprise host to another, and back to none.
	m.cache.OAuth = "gho_issued_by_ghe"
	if err := m.SetEnterpriseURL("other.example.com"); err != nil {
		t.Fatal(err)
	}
	if m.cache.OAuth != "" {
		t.Error("the OAuth token survived a move between enterprise hosts")
	}
	m.cache.OAuth = "gho_issued_by_other"
	if err := m.SetEnterpriseURL(""); err != nil {
		t.Fatal(err)
	}
	if m.cache.OAuth != "" {
		t.Error("the OAuth token survived clearing the enterprise host")
	}
}

// Saving the host that is already configured is not a change, and must not
// sign anybody out.
func TestSavingTheSameEnterpriseHostKeepsTheLogin(t *testing.T) {
	m := NewAuthManager(filepath.Join(t.TempDir(), "auth.json"))
	if err := m.SetEnterpriseURL("ghe.example.com"); err != nil {
		t.Fatal(err)
	}
	m.cache.OAuth = "gho_token"
	for _, same := range []string{"ghe.example.com", " GHE.example.com "} {
		if err := m.SetEnterpriseURL(same); err != nil {
			t.Fatal(err)
		}
		if m.cache.OAuth != "gho_token" {
			t.Fatalf("re-saving %q signed the user out", same)
		}
	}
}

// The request that would have carried the token to the new host carries
// nothing, because there is no token to carry.
func TestNoCredentialReachesTheNewHost(t *testing.T) {
	m := NewAuthManager(filepath.Join(t.TempDir(), "auth.json"))
	m.cache.OAuth = "gho_issued_by_github"
	m.loaded = true
	if err := m.SetEnterpriseURL("attacker.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.CopilotToken(context.Background()); err == nil {
		t.Fatal("a token refresh went ahead with no login")
	}
}

func TestValidCopilotEndpoint(t *testing.T) {
	cases := []struct {
		endpoint, enterprise string
		want                 bool
	}{
		{"https://api.githubcopilot.com", "", true},
		{"https://api.individual.githubcopilot.com", "", true},
		{"https://api.business.githubcopilot.com/", "", true},
		{"https://githubcopilot.com", "", true},
		{"http://api.githubcopilot.com", "", false},               // not HTTPS
		{"https://api.githubcopilot.com.evil.example", "", false}, // suffix trick
		{"https://evilgithubcopilot.com", "", false},              // not a subdomain
		{"https://evil.example", "", false},                       // elsewhere
		{"https://user:pw@api.githubcopilot.com", "", false},      // credentials
		{"https://api.githubcopilot.com?x=1", "", false},          // query
		{"https://169.254.169.254", "", false},                    // metadata
		{"https://localhost:8080", "", false},                     // loopback
		{"not a url", "", false},
		{"", "", false},
		{"https://copilot-api.ghe.example.com", "ghe.example.com", true},
		{"https://ghe.example.com", "ghe.example.com", true},
		{"https://api.ghe.example.com", "ghe.example.com:8443", true},
		{"https://api.githubcopilot.com", "ghe.example.com", true},
		{"https://ghe.example.com.evil.example", "ghe.example.com", false},
		{"https://other.example.com", "ghe.example.com", false},
	}
	for _, c := range cases {
		if got := validCopilotEndpoint(c.endpoint, c.enterprise); got != c.want {
			t.Errorf("validCopilotEndpoint(%q, %q) = %v, want %v", c.endpoint, c.enterprise, got, c.want)
		}
	}
}

// A token response naming an API endpoint somewhere unexpected is refused, and
// nothing from it is kept.
func TestTokenRefreshRefusesAnUnexpectedEndpoint(t *testing.T) {
	for _, endpoint := range []string{"https://evil.example", "http://api.githubcopilot.com", "https://127.0.0.1:9"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "copilot-token",
				"expires_at": 4102444800,
				"endpoints":  map[string]string{"api": endpoint},
			})
		}))
		m := NewAuthManager(filepath.Join(t.TempDir(), "auth.json"))
		m.apiHost = srv.URL
		m.cache.OAuth = "gho_token"
		m.loaded = true

		_, got, err := m.CopilotToken(context.Background())
		srv.Close()
		if err == nil {
			t.Errorf("endpoint %q was accepted as %q", endpoint, got)
		}
		if m.cache.CopilotToken != "" || m.cache.APIEndpoint != "" {
			t.Errorf("endpoint %q: something from a refused response was kept", endpoint)
		}
		if err != nil && strings.Contains(err.Error(), endpoint) {
			t.Errorf("the error quotes the refused endpoint: %v", err)
		}
	}
}

func TestCopilotRoutesHonourTheOperatorsPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandlers(NewAuthStore(t.TempDir())).Register(r)

	get := func(method, path string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	if code, _ := get("GET", "/api/copilot/tools"); code != http.StatusOK {
		t.Fatalf("tools answered %d with nothing restricted", code)
	}

	for name, set := range map[string]func(){
		"disabled":               func() { t.Setenv(aipolicy.EnvDisabled, "1") },
		"provider not permitted": func() { t.Setenv(aipolicy.EnvDisabled, ""); t.Setenv(aipolicy.EnvAllowedProviders, "ollama") },
	} {
		set()
		for _, route := range [][2]string{
			{"GET", "/api/copilot/tools"}, {"GET", "/api/copilot/models"},
			{"POST", "/api/copilot/login/start"}, {"POST", "/api/copilot/login/poll"},
			{"POST", "/api/copilot/enterprise"}, {"POST", "/api/copilot/chat"},
		} {
			if code, _ := get(route[0], route[1]); code != http.StatusForbidden {
				t.Errorf("%s: %s %s answered %d, want 403", name, route[0], route[1], code)
			}
		}
		code, status := get("GET", "/api/copilot/status")
		if code != http.StatusOK || status["disabled"] != true {
			t.Errorf("%s: status answered %d %v, want 200 with disabled:true", name, code, status)
		}
		if code, _ := get("POST", "/api/copilot/logout"); code != http.StatusOK {
			t.Errorf("%s: sign-out answered %d; it must always be possible", name, code)
		}
	}
}
