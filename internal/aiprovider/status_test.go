// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestChatRepeatsTheUpstreamRetryAfter checks that a 429 reaches the browser
// with the wait the provider asked for, so the panel can say "try again in
// about 20 seconds" instead of guessing. Both wire formats are covered: the
// Anthropic client builds its own APIError.
func TestChatRepeatsTheUpstreamRetryAfter(t *testing.T) {
	tooMany := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit"}}`)
	}
	for _, tc := range []struct{ name, save string }{
		{"openai-compatible", `{"provider":"openai-compatible","baseUrl":"%s","model":"m"}`},
		{"anthropic", `{"provider":"anthropic","baseUrl":"%s","apiKey":"sk-ant-key","model":"m"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(tooMany))
			defer upstream.Close()

			r, _ := newRouterForTest(t)
			if code, _ := doJSON(t, r, "POST", "/api/ai/config", strings.Replace(tc.save, "%s", upstream.URL, 1)); code != http.StatusOK {
				t.Fatalf("save failed with %d", code)
			}

			req := httptest.NewRequest("POST", "/api/ai/chat",
				bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
			req.AddCookie(&http.Cookie{Name: identityCookieName, Value: testIdentityID})
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want the upstream 429 passed through", w.Code)
			}
			if got := w.Header().Get("Retry-After"); got != "20" {
				t.Errorf("Retry-After = %q, want %q", got, "20")
			}
		})
	}
}

// TestStatusNamesTheSettingThatIsMissing pins the cause the panel puts in its
// "needs ..." line. The OpenAI-compatible entry's key is optional and its
// endpoint is not, so an empty endpoint used to be reported as a missing API
// key, which sent people looking for the wrong field.
func TestStatusNamesTheSettingThatIsMissing(t *testing.T) {
	cases := []struct {
		name    string
		save    string
		missing string
	}{
		{"endpoint missing", `{"provider":"openai-compatible"}`, "baseUrl"},
		{"endpoint missing even with a key", `{"provider":"openai-compatible","apiKey":"k"}`, "baseUrl"},
		{"key missing", `{"provider":"anthropic"}`, "apiKey"},
		{"key missing on a keyed provider with an endpoint", `{"provider":"openai"}`, "apiKey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newRouterForTest(t)
			if code, body := doJSON(t, r, "POST", "/api/ai/config", tc.save); code != http.StatusOK {
				t.Fatalf("save failed with %d: %v", code, body)
			}
			_, body := doJSON(t, r, "GET", "/api/ai/status", "")
			if ready, _ := body["ready"].(bool); ready {
				t.Fatalf("ready = true, want false: %v", body)
			}
			if body["needs"] != "config" {
				t.Errorf("needs = %v, want config", body["needs"])
			}
			if body["missing"] != tc.missing {
				t.Errorf("missing = %v, want %q", body["missing"], tc.missing)
			}
		})
	}

	t.Run("nothing missing once the endpoint is set", func(t *testing.T) {
		r, _ := newRouterForTest(t)
		save := `{"provider":"openai-compatible","baseUrl":"http://localhost:11434/v1"}`
		if code, body := doJSON(t, r, "POST", "/api/ai/config", save); code != http.StatusOK {
			t.Fatalf("save failed with %d: %v", code, body)
		}
		_, body := doJSON(t, r, "GET", "/api/ai/status", "")
		if ready, _ := body["ready"].(bool); !ready {
			t.Fatalf("ready = false with an endpoint and no key; the key is optional: %v", body)
		}
		if m, _ := body["missing"].(string); m != "" {
			t.Errorf("missing = %q on a ready provider", m)
		}
	})
}

// TestChatNamesTheMissingSetting covers the other place the cause is spoken:
// the 401 a chat request gets when the provider is not usable yet.
func TestChatNamesTheMissingSetting(t *testing.T) {
	r, _ := newRouterForTest(t)
	if code, _ := doJSON(t, r, "POST", "/api/ai/config", `{"provider":"openai-compatible"}`); code != http.StatusOK {
		t.Fatal("save failed")
	}
	code, body := doJSON(t, r, "POST", "/api/ai/chat", `{"messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "endpoint URL") {
		t.Errorf("error = %q, want it to name the endpoint URL (the form's own label)", msg)
	}
	if strings.Contains(msg, "API key") {
		t.Errorf("error = %q blames the API key, which is optional for this provider", msg)
	}
}
