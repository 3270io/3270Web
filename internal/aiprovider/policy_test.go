// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/aipolicy"
	"github.com/jnnngs/3270Web/internal/copilot"
)

// What the operator has decided, as the routes enforce it.

const chatBody = `{"messages":[{"role":"user","content":"hi"}]}`

func sseUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDisabledTurnsTheFeatureOffAndStatusSaysSo(t *testing.T) {
	r, _ := newRouterForTest(t)
	t.Setenv(aipolicy.EnvDisabled, "true")

	code, status := doJSON(t, r, "GET", "/api/ai/status", "")
	if code != http.StatusOK {
		t.Fatalf("status endpoint answered %d; it must answer so the page can find out", code)
	}
	if status["disabled"] != true || status["ready"] != false || status["needs"] != "disabled" {
		t.Errorf("status = %v, want disabled:true ready:false needs:disabled", status)
	}
	if msg, _ := status["message"].(string); msg == "" {
		t.Error("status should say why in words")
	}

	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/ai/chat", chatBody},
		{"POST", "/api/ai/config", `{"provider":"ollama"}`},
		{"GET", "/api/ai/config", ""},
		{"GET", "/api/ai/models", ""},
		{"GET", "/api/ai/tools", ""},
	} {
		code, body := doJSON(t, r, route.method, route.path, route.body)
		if code != http.StatusForbidden || body["disabled"] != true {
			t.Errorf("%s %s answered %d %v, want 403 with disabled:true", route.method, route.path, code, body)
		}
	}

	// A stored credential can still be removed.
	if code, _ := doJSON(t, r, "POST", "/api/ai/forget", `{"provider":"ollama"}`); code != http.StatusOK {
		t.Errorf("forget answered %d while disabled; turning the feature off must not strand a key", code)
	}

	t.Setenv(aipolicy.EnvDisabled, "false")
	if _, status := doJSON(t, r, "GET", "/api/ai/status", ""); status["disabled"] != false {
		t.Errorf("disabled = %v with the switch off", status["disabled"])
	}
}

func TestAllowedProvidersLimitsWhatCanBeSelectedAndUsed(t *testing.T) {
	upstream := sseUpstream(t)
	r, _ := newRouterForTest(t)

	// Configured while everything was allowed.
	save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","model":"m"}`
	if code, body := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d: %v", code, body)
	}

	t.Setenv(aipolicy.EnvAllowedProviders, "anthropic, Ollama")

	_, list := doJSON(t, r, "GET", "/api/ai/providers", "")
	var ids []string
	for _, item := range list["providers"].([]any) {
		ids = append(ids, item.(map[string]any)["id"].(string))
	}
	if strings.Join(ids, ",") != "anthropic,ollama" && strings.Join(ids, ",") != "ollama,anthropic" {
		t.Errorf("the catalogue offers %v, want only anthropic and ollama", ids)
	}

	// The previously saved provider can no longer be used...
	code, body := doJSON(t, r, "POST", "/api/ai/chat", chatBody)
	if code != http.StatusForbidden || body["providerDisabled"] != true {
		t.Errorf("chat with a provider no longer permitted answered %d %v, want 403", code, body)
	}
	_, status := doJSON(t, r, "GET", "/api/ai/status", "")
	if status["ready"] != false || status["providerDisabled"] != true || status["needs"] != "provider" {
		t.Errorf("status = %v, want ready:false providerDisabled:true needs:provider", status)
	}

	// ...nor selected, nor configured as a target.
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"openai"}`); code != http.StatusForbidden {
		t.Errorf("selecting a provider that is not permitted answered %d, want 403", code)
	}
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"target":"openai","model":"x"}`); code != http.StatusForbidden {
		t.Errorf("configuring a provider that is not permitted answered %d, want 403", code)
	}
	// A permitted one still works.
	if code, body := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"ollama"}`); code != http.StatusOK {
		t.Errorf("selecting a permitted provider answered %d: %v", code, body)
	}
}

func TestEndpointHostAllowlistAppliesToCustomBaseURLs(t *testing.T) {
	upstream := sseUpstream(t)
	r, _ := newRouterForTest(t)
	t.Setenv(aipolicy.EnvAllowedEndpointHosts, "*.models.example.com")

	save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","model":"m"}`
	code, body := doJSON(t, r, "POST", "/api/ai/config", save)
	if code != http.StatusForbidden {
		t.Fatalf("saving an endpoint off the allowlist answered %d: %v", code, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, aipolicy.EnvAllowedEndpointHosts) {
		t.Errorf("the refusal should name the setting, got %q", msg)
	}

	// Saved before the allowlist existed, refused at use.
	t.Setenv(aipolicy.EnvAllowedEndpointHosts, "")
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save without an allowlist failed with %d", code)
	}
	t.Setenv(aipolicy.EnvAllowedEndpointHosts, "*.models.example.com")
	if code, _ := doJSON(t, r, "POST", "/api/ai/chat", chatBody); code != http.StatusForbidden {
		t.Errorf("chat to an endpoint off the allowlist answered %d, want 403", code)
	}

	t.Setenv(aipolicy.EnvAllowedEndpointHosts, "127.0.0.1")
	if code, body := doJSON(t, r, "POST", "/api/ai/chat", chatBody); code != http.StatusOK {
		t.Errorf("chat to an allowlisted endpoint answered %d: %v", code, body)
	}
}

// With accounts, the person choosing an endpoint is not the person who runs
// the network the server sits in. A loopback or private endpoint is then a
// request made inside that network for whoever asked.
func TestPrivateEndpointsAreRefusedWhenSignInIsOn(t *testing.T) {
	upstream := sseUpstream(t) // 127.0.0.1
	r, _ := newRouterForTest(t)
	save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","model":"m"}`

	// Single operator: a local model works with nothing configured.
	if code, body := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("single-operator save failed with %d: %v", code, body)
	}
	if code, body := doJSON(t, r, "POST", "/api/ai/chat", chatBody); code != http.StatusOK {
		t.Fatalf("single-operator chat to a local endpoint answered %d: %v", code, body)
	}

	for _, mode := range []string{"local", "oidc"} {
		t.Setenv("AUTH_MODE", mode)
		code, body := doJSON(t, r, "POST", "/api/ai/config", save)
		if code != http.StatusForbidden {
			t.Errorf("AUTH_MODE=%s: saving a loopback endpoint answered %d: %v", mode, code, body)
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, aipolicy.EnvAllowPrivateEndpoints) {
			t.Errorf("the refusal should name the setting that permits it, got %q", msg)
		}
		// The endpoint saved earlier is refused at use too.
		if code, _ := doJSON(t, r, "POST", "/api/ai/chat", chatBody); code != http.StatusForbidden {
			t.Errorf("AUTH_MODE=%s: chat to a loopback endpoint answered %d, want 403", mode, code)
		}
		for _, private := range []string{"http://10.1.2.3/v1", "http://192.168.0.9:8080/v1", "http://[::1]:11434/v1"} {
			if code, _ := doJSON(t, r, "POST", "/api/ai/config",
				`{"provider":"openai-compatible","baseUrl":"`+private+`"}`); code != http.StatusForbidden {
				t.Errorf("AUTH_MODE=%s: saving %s answered %d, want 403", mode, private, code)
			}
		}
	}

	// The operator can say otherwise.
	t.Setenv(aipolicy.EnvAllowPrivateEndpoints, "true")
	if code, body := doJSON(t, r, "POST", "/api/ai/chat", chatBody); code != http.StatusOK {
		t.Errorf("with private endpoints permitted, chat answered %d: %v", code, body)
	}
	// And can forbid them for a single operator.
	t.Setenv("AUTH_MODE", "none")
	t.Setenv(aipolicy.EnvAllowPrivateEndpoints, "false")
	if code, _ := doJSON(t, r, "POST", "/api/ai/chat", chatBody); code != http.StatusForbidden {
		t.Errorf("with private endpoints forbidden, chat answered %d, want 403", code)
	}
}

// A 2xx that is not an event stream is not a chat completion, and its body is
// not relayed.
func TestChatRefusesABodyThatIsNotAnEventStream(t *testing.T) {
	for _, contentType := range []string{"text/html", "application/json", "text/plain", ""} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			} else {
				w.Header()["Content-Type"] = nil
			}
			_, _ = io.WriteString(w, "root:x:0:0:internal-file-contents")
		}))
		r, _ := newRouterForTest(t)
		save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","model":"m"}`
		if code, _ := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
			t.Fatalf("save failed with %d", code)
		}

		req := httptest.NewRequest("POST", "/api/ai/chat", strings.NewReader(chatBody))
		req.AddCookie(&http.Cookie{Name: identityCookieName, Value: testIdentityID})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		upstream.Close()

		if w.Code != http.StatusBadGateway {
			t.Errorf("content type %q: status = %d, want 502", contentType, w.Code)
		}
		if strings.Contains(w.Body.String(), "internal-file-contents") {
			t.Errorf("content type %q: the body was relayed: %s", contentType, w.Body.String())
		}
	}
}

// The model list is JSON or it is nothing; anything else falls back to the
// curated catalogue rather than being parsed.
func TestModelsIgnoresABodyThatIsNotJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `{"data":[{"id":"from-a-page-that-is-not-an-api"}]}`)
	}))
	defer upstream.Close()
	r, _ := newRouterForTest(t)
	save := `{"provider":"openai-compatible","baseUrl":"` + upstream.URL + `","model":"m"}`
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	_, body := doJSON(t, r, "GET", "/api/ai/models", "")
	models, _ := body["models"].([]any)
	for _, m := range models {
		if m == "from-a-page-that-is-not-an-api" {
			t.Error("a model list was read from a response that was not JSON")
		}
	}
}

// A redirect off the host the request was sent to is not followed: the request
// is carrying that host's API key.
func TestChatDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	var leakedAuth string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		leakedAuth = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer elsewhere.Close()
	// Same address, different port: a different host as far as a credential
	// is concerned.
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, elsewhere.URL+"/chat/completions", http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	r, _ := newRouterForTest(t)
	save := `{"provider":"openai-compatible","baseUrl":"` + redirecting.URL + `","apiKey":"sk-secret","model":"m"}`
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	code, body := doJSON(t, r, "POST", "/api/ai/chat", chatBody)
	if code == http.StatusOK {
		t.Errorf("a cross-host redirect was followed: %v", body)
	}
	if leakedAuth != "" {
		t.Errorf("the API key was delivered to the redirect target: %q", leakedAuth)
	}
}

// What went wrong goes to the log; the browser gets a sentence.
func TestTransportErrorsAreNotRelayed(t *testing.T) {
	r, _ := newRouterForTest(t)
	save := `{"provider":"openai-compatible","baseUrl":"http://127.0.0.1:1/v1","model":"m"}`
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
		t.Fatalf("save failed with %d", code)
	}
	code, body := doJSON(t, r, "POST", "/api/ai/chat", chatBody)
	if code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", code)
	}
	msg, _ := body["error"].(string)
	for _, leak := range []string{"127.0.0.1", "dial tcp", "connection refused", "connect:"} {
		if strings.Contains(msg, leak) {
			t.Errorf("the error relays transport detail (%q): %q", leak, msg)
		}
	}
	if msg == "" {
		t.Error("the browser should still be told something")
	}
}

// A provider's own endpoint is trusted to be describing an error, so its
// message is quoted — shortened, and only the message.
func TestPublicErrorQuotesOnlyATrustedProvidersMessage(t *testing.T) {
	err := &APIError{Provider: "Anthropic", Status: 400, Body: `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is too large"}}`}

	status, msg := aipolicy.PublicError(err, true)
	if status != 400 || !strings.Contains(msg, "max_tokens is too large") {
		t.Errorf("trusted: %d %q", status, msg)
	}
	status, msg = aipolicy.PublicError(err, false)
	if status != 400 || strings.Contains(msg, "max_tokens") {
		t.Errorf("untrusted: %d %q — the body must not be quoted", status, msg)
	}
	// A body that is not an error document is never quoted, trusted or not.
	_, msg = aipolicy.PublicError(&APIError{Provider: "X", Status: 500, Body: "<html>secret</html>"}, true)
	if strings.Contains(msg, "secret") {
		t.Errorf("a non-JSON body was quoted: %q", msg)
	}
}

func TestToolsReportsClassesAndTheRoundCeiling(t *testing.T) {
	r, _ := newRouterForTest(t)
	code, body := doJSON(t, r, "GET", "/api/ai/tools", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	classes, _ := body["toolClasses"].(map[string]any)
	if classes == nil {
		t.Fatalf("toolClasses missing: %v", body)
	}
	for _, tool := range copilot.DefaultTools() {
		switch classes[tool.Function.Name] {
		case "read", "write", "destructive":
		default:
			t.Errorf("tool %q has class %v", tool.Function.Name, classes[tool.Function.Name])
		}
	}
	if classes["chaos_start"] != "destructive" || classes["get_screen"] != "read" || classes["write_field"] != "write" {
		t.Errorf("classes look wrong: %v", classes)
	}
	if body["dangerous_send_keys"] == nil || body["dangerousSendKeys"] == nil {
		t.Error("the dangerous-key list must be reported under both spellings")
	}
	if body["maxToolRounds"] != float64(copilot.MaxToolRounds) {
		t.Errorf("maxToolRounds = %v, want %d", body["maxToolRounds"], copilot.MaxToolRounds)
	}
}
