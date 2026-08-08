package aiprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/copilot"
)

func init() { gin.SetMode(gin.TestMode) }

// identityCookieName mirrors the unexported constant in internal/copilot.
// Both packages key per-browser state on this one cookie, so the tests have
// to name it explicitly to pin every request to a single identity.
const identityCookieName = "3270Web_copilot_id"

// testIdentityID is a well-formed identity (32 lowercase hex characters);
// anything else is discarded and replaced by copilot.Identity.
const testIdentityID = "0123456789abcdef0123456789abcdef"

func newRouterForTest(t *testing.T) (*gin.Engine, *ConfigStore) {
	t.Helper()
	dir := t.TempDir()
	cfg := NewConfigStore(dir)
	r := gin.New()
	NewHandlers(cfg, copilot.NewAuthStore(dir)).Register(r)
	return r, cfg
}

func doJSON(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(&http.Cookie{Name: identityCookieName, Value: testIdentityID})
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestListProvidersReturnsTheCatalogue(t *testing.T) {
	r, _ := newRouterForTest(t)
	code, body := doJSON(t, r, "GET", "/api/ai/providers", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	list, _ := body["providers"].([]any)
	if len(list) != len(Providers()) {
		t.Fatalf("got %d providers, want %d", len(list), len(Providers()))
	}
	if _, ok := body["settings"].(map[string]any); !ok {
		t.Error("response carried no settings block")
	}
}

func TestStatusDefaultsToCopilotAndReportsLoginNeeded(t *testing.T) {
	r, _ := newRouterForTest(t)
	code, body := doJSON(t, r, "GET", "/api/ai/status", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body["provider"] != DefaultProviderID {
		t.Errorf("provider = %v, want %q", body["provider"], DefaultProviderID)
	}
	if ready, _ := body["ready"].(bool); ready {
		t.Error("ready = true with no Copilot login")
	}
	if body["needs"] != "login" {
		t.Errorf("needs = %v, want login", body["needs"])
	}
}

func TestSaveConfigRoundTripNeverEchoesTheKey(t *testing.T) {
	r, _ := newRouterForTest(t)

	code, body := doJSON(t, r, "POST", "/api/ai/config",
		`{"provider":"anthropic","apiKey":"sk-ant-supersecret","model":"claude-opus-5"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "supersecret") {
		t.Fatalf("save response leaked the API key: %s", raw)
	}

	code, body = doJSON(t, r, "GET", "/api/ai/config", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	raw, _ = json.Marshal(body)
	if strings.Contains(string(raw), "supersecret") {
		t.Fatalf("config response leaked the API key: %s", raw)
	}
	providers, _ := body["providers"].(map[string]any)
	entry, _ := providers["anthropic"].(map[string]any)
	if hasKey, _ := entry["hasKey"].(bool); !hasKey {
		t.Errorf("hasKey = false after saving a key: %v", entry)
	}

	// The provider now has everything it needs, so status must flip to ready.
	_, body = doJSON(t, r, "GET", "/api/ai/status", "")
	if ready, _ := body["ready"].(bool); !ready {
		t.Errorf("ready = false after saving a key: %v", body)
	}
}

func TestSaveConfigRejectsUnknownProvider(t *testing.T) {
	r, _ := newRouterForTest(t)
	code, body := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"skynet"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %v)", code, body)
	}
}

func TestForgetClearsCredentials(t *testing.T) {
	r, _ := newRouterForTest(t)
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"openai","apiKey":"sk-key"}`); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	if code, _ := doJSON(t, r, "POST", "/api/ai/forget", `{}`); code != http.StatusOK {
		t.Fatalf("forget failed with %d", code)
	}
	_, body := doJSON(t, r, "GET", "/api/ai/status", "")
	if ready, _ := body["ready"].(bool); ready {
		t.Error("ready = true after forgetting the credentials")
	}
}

func TestModelsFallsBackToTheCatalogue(t *testing.T) {
	r, _ := newRouterForTest(t)
	// Anthropic with no key: the live call can't be made, but the dropdown
	// must still populate so the user can pick before entering a key.
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"anthropic"}`); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	code, body := doJSON(t, r, "GET", "/api/ai/models", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	models, _ := body["models"].([]any)
	if len(models) == 0 {
		t.Fatal("models list was empty")
	}
}

func TestToolsExposesTheSchemaRegardlessOfProvider(t *testing.T) {
	r, _ := newRouterForTest(t)
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"ollama"}`); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	code, body := doJSON(t, r, "GET", "/api/ai/tools", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if tools, _ := body["tools"].([]any); len(tools) == 0 {
		t.Error("tools list was empty")
	}
	if prompt, _ := body["system_prompt"].(string); prompt == "" {
		t.Error("system_prompt was empty")
	}
}

func TestChatRefusesWhenTheProviderIsNotConfigured(t *testing.T) {
	r, _ := newRouterForTest(t)
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"anthropic"}`); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	code, body := doJSON(t, r, "POST", "/api/ai/chat",
		`{"messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %v)", code, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "API key") {
		t.Errorf("error = %q, want it to name the missing credential", msg)
	}
}

func TestChatRejectsAnEmptyConversation(t *testing.T) {
	r, _ := newRouterForTest(t)
	code, _ := doJSON(t, r, "POST", "/api/ai/chat", `{"messages":[]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
}

// TestChatProxiesAnOpenAICompatibleUpstream drives the whole path: settings
// save, provider resolution, and a streamed response copied back verbatim.
func TestChatProxiesAnOpenAICompatibleUpstream(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/chat/completions" {
			t.Errorf("upstream path = %q, want /chat/completions", req.URL.Path)
		}
		gotAuth = req.Header.Get("Authorization")
		body, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"x","choices":[{"delta":{"content":"hi"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	r, _ := newRouterForTest(t)
	save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","apiKey":"sk-test","model":"local-model"}`
	if code, body := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d: %v", code, body)
	}

	req := httptest.NewRequest("POST", "/api/ai/chat",
		bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.AddCookie(&http.Cookie{Name: identityCookieName, Value: testIdentityID})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want an SSE stream", ct)
	}
	if !strings.Contains(w.Body.String(), `"content":"hi"`) || !strings.Contains(w.Body.String(), "[DONE]") {
		t.Errorf("proxied body did not match the upstream stream:\n%s", w.Body.String())
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("upstream Authorization = %q, want the saved key", gotAuth)
	}
	if gotBody["model"] != "local-model" {
		t.Errorf("upstream model = %v, want the saved default filled in", gotBody["model"])
	}
	if stream, _ := gotBody["stream"].(bool); !stream {
		t.Error("upstream request did not force stream:true")
	}
}

// TestChatSurfacesUpstreamErrorsAsJSON checks that a failure before any bytes
// are streamed produces a readable JSON error rather than a half-open SSE.
func TestChatSurfacesUpstreamErrorsAsJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit"}}`)
	}))
	defer upstream.Close()

	r, _ := newRouterForTest(t)
	save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","model":"m"}`
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}

	code, body := doJSON(t, r, "POST", "/api/ai/chat", `{"messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want the upstream 429 passed through", code)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "rate limit") {
		t.Errorf("error = %q, want the upstream message", msg)
	}
}

// TestChatProxiesAnthropicThroughTheTranslator drives the Claude path end to
// end: an Anthropic-shaped upstream response must reach the browser as
// OpenAI-shaped SSE.
func TestChatProxiesAnthropicThroughTheTranslator(t *testing.T) {
	var gotKey, gotVersion string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/messages" {
			t.Errorf("upstream path = %q, want /v1/messages", req.URL.Path)
		}
		gotKey = req.Header.Get("x-api-key")
		gotVersion = req.Header.Get("anthropic-version")
		body, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_1"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer upstream.Close()

	r, _ := newRouterForTest(t)
	save := `{"provider":"anthropic","baseUrl":"` + upstream.URL + `","apiKey":"sk-ant-x","model":"claude-opus-5"}`
	if code, body := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d: %v", code, body)
	}

	req := httptest.NewRequest("POST", "/api/ai/chat", bytes.NewBufferString(
		`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}],"max_tokens":4096}`))
	req.AddCookie(&http.Cookie{Name: identityCookieName, Value: testIdentityID})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if gotKey != "sk-ant-x" {
		t.Errorf("x-api-key = %q, want the saved key", gotKey)
	}
	if gotVersion != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, anthropicVersion)
	}
	if gotBody["system"] != "sys" {
		t.Errorf("upstream system = %v, want the system turn hoisted out of messages", gotBody["system"])
	}
	out := w.Body.String()
	if !strings.Contains(out, `"chat.completion.chunk"`) || !strings.Contains(out, `"content":"hello"`) {
		t.Errorf("response was not translated to OpenAI chunks:\n%s", out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Errorf("translated stream did not terminate with [DONE]:\n%s", out)
	}
}
