package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatRequest is a thin wrapper around the OpenAI-compatible request body
// the Copilot API expects. We let the frontend supply most of the JSON
// verbatim so we don't duplicate the schema; only the trusted fields below
// are inspected server-side.
type ChatRequest struct {
	// Raw is the verbatim JSON payload sent to /chat/completions. The
	// handler enforces `stream: true` and substitutes a default model if
	// none was supplied, but otherwise passes through unchanged.
	Raw map[string]any
}

// Client speaks the GitHub Copilot Chat completions protocol.
type Client struct {
	auth       *AuthManager
	httpClient *http.Client

	// Identification headers — exposed for tests.
	userAgent           string
	editorVersion       string
	editorPluginVersion string
	integrationID       string
}

// NewClient returns a Client backed by the given AuthManager.
func NewClient(auth *AuthManager) *Client {
	return &Client{
		auth:                auth,
		httpClient:          &http.Client{},
		userAgent:           defaultUserAgent,
		editorVersion:       defaultEditorVersion,
		editorPluginVersion: defaultEditorPluginVersion,
		integrationID:       defaultIntegrationID,
	}
}

// StreamChat forwards req to Copilot's chat/completions endpoint and copies
// the SSE response into out. The returned error is non-nil on transport
// failures or non-2xx responses; once the stream starts copying, partial
// writes to out are surfaced to the caller too.
//
// The model field defaults to "claude-opus-4.7" if the caller did not set
// one. `stream` is forced to true. Other fields (messages, tools,
// tool_choice, temperature, max_tokens, ...) are passed through.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest, out io.Writer) error {
	token, apiEndpoint, err := c.auth.CopilotToken(ctx)
	if err != nil {
		return err
	}
	payload := req.Raw
	if payload == nil {
		return errors.New("copilot: empty chat request")
	}

	if _, ok := payload["model"]; !ok {
		payload["model"] = DefaultModel
	}
	payload["stream"] = true

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiEndpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", c.userAgent)
	httpReq.Header.Set("Editor-Version", c.editorVersion)
	httpReq.Header.Set("Editor-Plugin-Version", c.editorPluginVersion)
	httpReq.Header.Set("Copilot-Integration-Id", c.integrationID)
	httpReq.Header.Set("Openai-Intent", "conversation-edits")
	httpReq.Header.Set("X-Initiator", initiatorFromMessages(payload))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("copilot chat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &APIError{
			Status: resp.StatusCode,
			Body:   string(bodyBytes),
		}
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("copilot stream: %w", err)
	}
	return nil
}

// ListModels fetches the available model IDs from the Copilot /models endpoint.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	token, apiEndpoint, err := c.auth.CopilotToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", apiEndpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Editor-Version", c.editorVersion)
	req.Header.Set("Editor-Plugin-Version", c.editorPluginVersion)
	req.Header.Set("Copilot-Integration-Id", c.integrationID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot models request: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Status: resp.StatusCode, Body: string(bodyBytes)}
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		// Only expose Claude models; non-Claude models (GPT, o-series, etc.)
		// don't support the Anthropic tool_call_id format and return 400 errors.
		if m.ID != "" && strings.HasPrefix(m.ID, "claude-") {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// APIError is returned when Copilot answers with a non-2xx status.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("copilot chat: status %d: %s", e.Status, truncate(e.Body, 200))
}

// initiatorFromMessages returns "user" if the trailing message is a user
// message, otherwise "agent". This matches the X-Initiator semantics used
// by the official Copilot Chat extension.
func initiatorFromMessages(payload map[string]any) string {
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return "user"
	}
	last, ok := msgs[len(msgs)-1].(map[string]any)
	if !ok {
		return "user"
	}
	role, _ := last["role"].(string)
	if role == "user" {
		return "user"
	}
	return "agent"
}
