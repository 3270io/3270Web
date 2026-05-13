package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPrimedManager(t *testing.T, apiEndpoint string) *AuthManager {
	t.Helper()
	dir := t.TempDir()
	m := NewAuthManager(filepath.Join(dir, "auth.json"))
	m.cache = cachedAuth{
		OAuth:          "oauth",
		CopilotToken:   "copilot-tok",
		CopilotExpires: time.Now().Add(time.Hour).UnixMilli(),
		APIEndpoint:    apiEndpoint,
	}
	m.loaded = true
	return m
}

func TestStreamChatSendsRequiredHeaders(t *testing.T) {
	var (
		gotAuth         string
		gotEditor       string
		gotUserAgent    string
		gotIntegration  string
		gotInitiator    string
		gotIntent       string
		gotAccept       string
		gotContentType  string
		receivedPayload map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		gotEditor = r.Header.Get("Editor-Version")
		gotUserAgent = r.Header.Get("User-Agent")
		gotIntegration = r.Header.Get("Copilot-Integration-Id")
		gotInitiator = r.Header.Get("X-Initiator")
		gotIntent = r.Header.Get("Openai-Intent")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	m := newPrimedManager(t, srv.URL)
	c := NewClient(m)

	var out bytes.Buffer
	req := ChatRequest{Raw: map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}}
	if err := c.StreamChat(context.Background(), req, &out); err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	if gotAuth != "Bearer copilot-tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if !strings.HasPrefix(gotEditor, "vscode/") {
		t.Errorf("Editor-Version = %q", gotEditor)
	}
	if !strings.HasPrefix(gotUserAgent, "GitHubCopilotChat/") {
		t.Errorf("User-Agent = %q", gotUserAgent)
	}
	if gotIntegration != defaultIntegrationID {
		t.Errorf("Copilot-Integration-Id = %q", gotIntegration)
	}
	if gotInitiator != "user" {
		t.Errorf("X-Initiator = %q, want user", gotInitiator)
	}
	if gotIntent != "conversation-edits" {
		t.Errorf("Openai-Intent = %q", gotIntent)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}

	// Payload should have stream=true forced and model defaulted.
	if v, _ := receivedPayload["stream"].(bool); !v {
		t.Errorf("stream != true in payload: %v", receivedPayload["stream"])
	}
	if model, _ := receivedPayload["model"].(string); model != DefaultModel {
		t.Errorf("model = %q, want %s", model, DefaultModel)
	}

	// Stream content was copied through.
	if !strings.Contains(out.String(), "delta") {
		t.Errorf("stream body not copied: %q", out.String())
	}
}

func TestStreamChatInitiatorAgentForToolMessage(t *testing.T) {
	var initiator string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		initiator = r.Header.Get("X-Initiator")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	m := newPrimedManager(t, srv.URL)
	c := NewClient(m)
	req := ChatRequest{Raw: map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "ask"},
			map[string]any{"role": "assistant", "content": "calling tool"},
			map[string]any{"role": "tool", "content": "{}", "tool_call_id": "call_1"},
		},
	}}
	if err := c.StreamChat(context.Background(), req, io.Discard); err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if initiator != "agent" {
		t.Fatalf("X-Initiator = %q, want agent (last role is tool)", initiator)
	}
}

func TestStreamChatNon2xxReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	m := newPrimedManager(t, srv.URL)
	c := NewClient(m)
	err := c.StreamChat(context.Background(), ChatRequest{Raw: map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "x"}},
	}}, io.Discard)
	if err == nil {
		t.Fatalf("expected APIError, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %T (%v), want *APIError", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Fatalf("status = %d", apiErr.Status)
	}
}
