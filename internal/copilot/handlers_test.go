package copilot

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// testIdentityID is a fixed, well-formed (32 lowercase hex chars) identity
// used by every test request so each test's requests share one Copilot
// identity, letting the test manipulate a single *AuthManager (returned by
// newHandlersForTest) and see it reflected across every request it makes.
const testIdentityID = "0123456789abcdef0123456789abcdef"

func newHandlersForTest(t *testing.T) (*Handlers, *AuthManager) {
	t.Helper()
	dir := t.TempDir()
	store := NewAuthStore(dir)
	h := NewHandlers(store)
	ia := store.get(testIdentityID)
	return h, ia.auth
}

// newTestRequest builds a request carrying the fixed test identity cookie so
// it resolves to the same AuthManager newHandlersForTest returned.
func newTestRequest(method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.AddCookie(&http.Cookie{Name: identityCookieName, Value: testIdentityID})
	return req
}

func TestStatusReportsLoggedOut(t *testing.T) {
	h, _ := newHandlersForTest(t)
	r := gin.New()
	h.Register(r)

	w := httptest.NewRecorder()
	req := newTestRequest("GET", "/api/copilot/status", nil)
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
	r.ServeHTTP(w, newTestRequest("GET", "/api/copilot/tools", nil))
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
	r.ServeHTTP(w, newTestRequest("POST", "/api/copilot/chat", body))
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
	req := newTestRequest("POST", "/api/copilot/chat", body)
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
		r.ServeHTTP(w, newTestRequest("POST", "/api/copilot/login/start", nil))
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
		req := newTestRequest("POST", "/api/copilot/login/poll", bytes.NewBufferString(`{"login_id":"`+loginID+`"}`))
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

// TestStatusIssuesIdentityCookieWhenMissing guards the fix for one shared
// Copilot login for the whole server: a request with no identity cookie
// must come back with one, or every subsequent request from that browser
// would keep minting a fresh (never-logged-in) identity instead of reusing
// the one it was just assigned.
func TestStatusIssuesIdentityCookieWhenMissing(t *testing.T) {
	h, _ := newHandlersForTest(t)
	r := gin.New()
	h.Register(r)

	w := httptest.NewRecorder()
	// Deliberately bypass newTestRequest here: this test wants a request
	// with no cookie at all.
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/copilot/status", nil))

	var found *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == identityCookieName {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatalf("no %s cookie set on a request with none", identityCookieName)
	}
	if !identityIDPattern.MatchString(found.Value) {
		t.Fatalf("issued identity cookie value %q does not match the expected shape", found.Value)
	}
}

// TestInvalidIdentityCookieIsReplaced guards against trusting an
// attacker-controlled cookie value as an on-disk filename component
// (Store.get joins the identity straight into a path): anything not shaped
// like a value this package itself generates must be discarded and replaced,
// never passed through to the filesystem.
func TestInvalidIdentityCookieIsReplaced(t *testing.T) {
	h, _ := newHandlersForTest(t)
	r := gin.New()
	h.Register(r)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/copilot/status", nil)
	req.AddCookie(&http.Cookie{Name: identityCookieName, Value: "../../etc/passwd"})
	r.ServeHTTP(w, req)

	var found *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == identityCookieName {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatalf("no replacement %s cookie set for an invalid incoming value", identityCookieName)
	}
	if found.Value == "../../etc/passwd" || !identityIDPattern.MatchString(found.Value) {
		t.Fatalf("invalid identity cookie was not replaced, got %q", found.Value)
	}
}

// TestLoginStateIsScopedPerIdentity guards the actual behavioral fix: two
// different browsers (two different identity cookies) must never see each
// other's login state, unlike the old single-global-AuthManager design.
func TestLoginStateIsScopedPerIdentity(t *testing.T) {
	dir := t.TempDir()
	store := NewAuthStore(dir)
	// Seed "browser A" (testIdentityID, the identity newTestRequest's cookie
	// carries) as logged in.
	ia := store.get(testIdentityID)
	ia.auth.cache = cachedAuth{OAuth: "a-token"}
	ia.auth.loaded = true

	h := NewHandlers(store)
	r := gin.New()
	h.Register(r)

	// Browser A: logged in.
	wA := httptest.NewRecorder()
	r.ServeHTTP(wA, newTestRequest("GET", "/api/copilot/status", nil))
	var bodyA map[string]any
	_ = json.Unmarshal(wA.Body.Bytes(), &bodyA)
	if loggedIn, _ := bodyA["logged_in"].(bool); !loggedIn {
		t.Fatalf("browser A logged_in = %v, want true", bodyA["logged_in"])
	}

	// Browser B: no cookie at all, must NOT inherit browser A's login.
	wB := httptest.NewRecorder()
	r.ServeHTTP(wB, httptest.NewRequest("GET", "/api/copilot/status", nil))
	var bodyB map[string]any
	_ = json.Unmarshal(wB.Body.Bytes(), &bodyB)
	if loggedIn, _ := bodyB["logged_in"].(bool); loggedIn {
		t.Fatalf("browser B (no prior cookie) logged_in = true, want false — leaked browser A's login")
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
	r.ServeHTTP(w, newTestRequest("POST", "/api/copilot/chat", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
