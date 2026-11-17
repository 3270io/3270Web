// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"absent", "", 0},
		{"seconds", "20", 20 * time.Second},
		{"padded", "  7 ", 7 * time.Second},
		{"zero means nothing useful", "0", 0},
		{"negative", "-5", 0},
		{"garbage", "soon", 0},
		{"fractional is not a valid delay", "1.5", 0},
		{"http date", now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second},
		{"http date in the past", now.Add(-time.Minute).UTC().Format(http.TimeFormat), 0},
		// An upstream that asks for a day is not something a chat panel can
		// usefully count down; the ceiling keeps the message believable.
		{"capped", "999999", MaxRetryAfter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set("Retry-After", tc.value)
			}
			if got := ParseRetryAfter(h, now); got != tc.want {
				t.Errorf("ParseRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// The wait the upstream asked for has to survive the trip through APIError,
// because the handler that turns the error into a response is a different
// function from the one that read the headers.
func TestStreamChatCarriesRetryAfterOnTheAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "20")
		http.Error(w, `{"error":{"message":"slow down"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(newPrimedManager(t, srv.URL))
	err := c.StreamChat(context.Background(), ChatRequest{Raw: map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "x"}},
	}}, io.Discard)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %T (%v), want *APIError", err, err)
	}
	if apiErr.RetryAfter != 20*time.Second {
		t.Errorf("RetryAfter = %v, want 20s", apiErr.RetryAfter)
	}
}

// The browser can only wait as long as it is told to, so the handler has to
// repeat the upstream's Retry-After on the 429 it sends back.
func TestChatRepeatsRetryAfterToTheBrowser(t *testing.T) {
	h, m := newHandlersForTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "20")
		http.Error(w, `{"error":{"message":"slow down"}}`, http.StatusTooManyRequests)
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
	req := newTestRequest("POST", "/api/copilot/chat",
		bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want the upstream 429 passed through", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "20" {
		t.Errorf("Retry-After = %q, want %q", got, "20")
	}
}
