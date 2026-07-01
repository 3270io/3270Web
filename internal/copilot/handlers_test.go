package copilot

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newHandlersForTest(t *testing.T) (*Handlers, *AuthManager) {
	t.Helper()
	dir := t.TempDir()
	m := NewAuthManager(filepath.Join(dir, "auth.json"))
	h := NewHandlers(m)
	return h, m
}

func TestStatusReportsLoggedOut(t *testing.T) {
	h, _ := newHandlersForTest(t)
	r := gin.New()
	h.Register(r)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/copilot/status", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if v, _ := body["logged_in"].(bool); v {
		t.Fatalf("logged_in true on fresh manager")
	}
	if body["model"] != DefaultModel {
		t.Fatalf("model = %v", body["model"])
	}
}

func TestToolsEndpointExposesSchema(t *testing.T) {
	h, _ := newHandlersForTest(t)
	r := gin.New()
	h.Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/copilot/tools", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Tools        []Tool `json:"tools"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tools) == 0 {
		t.Fatalf("no tools returned")
	}
	names := map[string]bool{}
	for _, tool := range body.Tools {
		names[tool.Function.Name] = true
	}
	want := []string{"get_screen", "send_key", "write_field", "chaos_start", "chaos_status"}
	for _, n := range want {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	if body.SystemPrompt == "" {
		t.Errorf("empty system prompt")
	}
}

func TestChatRequiresLogin(t *testing.T) {
	h, _ := newHandlersForTest(t)
	r := gin.New()
	h.Register(r)
	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`)
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/copilot/chat", body))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestChatProxiesUpstreamStream(t *testing.T) {
	h, m := newHandlersForTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	m.cache = cachedAuth{
		OAuth:          "x",
		CopilotToken:   "tok",
		CopilotExpires: time.Now().Add(time.Hour).UnixMilli(),
		APIEndpoint:    upstream.URL,
	}
	m.loaded = true

	r := gin.New()
	h.Register(r)
	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest("POST", "/api/copilot/chat", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("stream body not proxied: %q", w.Body.String())
	}
}

// TestLoginPollRejectsSupersededAttempt verifies the fix for the device-flow
// race: a second /login/start (e.g. another browser tab, or another user on
// a shared instance) must invalidate the first attempt's login_id so its
// poll can't silently ride along with whichever device code is now current.
func TestLoginPollRejectsSupersededAttempt(t *testing.T) {
	h, m := newHandlersForTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "DEV",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
				"interval":         5,
			})
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m.githubHost = srv.URL

	r := gin.New()
	h.Register(r)

	start := func() string {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/copilot/login/start", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("login/start status = %d, body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode login/start: %v", err)
		}
		id, _ := body["login_id"].(string)
		if id == "" {
			t.Fatalf("login/start returned empty login_id")
		}
		return id
	}

	poll := func(loginID string) map[string]any {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/copilot/login/poll", bytes.NewBufferString(`{"login_id":"`+loginID+`"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login/poll status = %d, body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode login/poll: %v", err)
		}
		return body
	}

	firstID := start()
	secondID := start() // supersedes the first attempt

	if got := poll(firstID)["status"]; got != "superseded" {
		t.Fatalf("poll(firstID) status = %v, want superseded", got)
	}
	if got := poll(secondID)["status"]; got != "pending" {
		t.Fatalf("poll(secondID) status = %v, want pending", got)
	}
}

func TestChatRejectsEmptyMessages(t *testing.T) {
	h, m := newHandlersForTest(t)
	m.cache = cachedAuth{
		OAuth:          "x",
		CopilotToken:   "tok",
		CopilotExpires: time.Now().Add(time.Hour).UnixMilli(),
		APIEndpoint:    "http://127.0.0.1:1",
	}
	m.loaded = true

	r := gin.New()
	h.Register(r)
	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"messages":[]}`)
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/copilot/chat", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
