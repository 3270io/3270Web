// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jnnngs/3270Web/internal/egress"
)

// streamIdleTimeout is the maximum time the proxied SSE stream may go without
// receiving any data before it is treated as stalled and aborted. This is far
// tighter than the handler's overall stream cap so a hung upstream fails fast
// (with an actionable error) instead of pinning the connection for minutes.
const streamIdleTimeout = 60 * time.Second

// errStreamStalled is returned when the upstream stops sending data for longer
// than streamIdleTimeout.
var errStreamStalled = errors.New("upstream stalled (no data received for 60s)")

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
		httpClient:          egress.Default().HTTPClient(0),
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
// The model field defaults to DefaultModel if the caller did not set
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

	// Derive a cancellable context so the idle watchdog can abort a stalled
	// stream (which unblocks the in-flight Body.Read) independently of the
	// caller's overall timeout.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	httpReq, err := http.NewRequestWithContext(streamCtx, "POST", apiEndpoint+"/chat/completions", bytes.NewReader(body))
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
		return &APIError{
			Status: resp.StatusCode,
			Body:   readErrorBody(resp),
		}
	}

	if err := copyWithIdleTimeout(out, resp.Body, streamIdleTimeout, cancelStream); err != nil {
		return fmt.Errorf("copilot stream: %w", err)
	}
	return nil
}

// copyWithIdleTimeout copies src to dst like io.Copy, but aborts (via onIdle,
// which cancels the request context to unblock a stuck Read) when no data has
// arrived for `idle`. The deadline resets on every chunk received, so a slow
// but steadily-streaming completion is never interrupted — only a genuinely
// stalled upstream is. Returns errStreamStalled when the idle watchdog fires.
func copyWithIdleTimeout(dst io.Writer, src io.Reader, idle time.Duration, onIdle func()) error {
	var stalled atomic.Bool
	timer := time.AfterFunc(idle, func() {
		stalled.Store(true)
		onIdle()
	})
	defer timer.Stop()

	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			timer.Reset(idle)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			if stalled.Load() {
				return errStreamStalled
			}
			return rerr
		}
	}
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
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Status: resp.StatusCode, Body: readErrorBody(resp)}
	}
	bodyBytes, err := readCapped(resp, maxCatalogueResponseBytes, "copilot models response")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	// Intersect the upstream list with SupportedModels (the curated allowlist).
	// Copilot's /models endpoint sometimes advertises preview Claude models
	// that 400 on /chat/completions with model_not_supported, and non-Claude
	// models don't speak the OpenAI tool_call_id format we use.
	advertised := make(map[string]bool, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			advertised[m.ID] = true
		}
	}
	ids := make([]string, 0, len(SupportedModels))
	for _, id := range SupportedModels {
		if advertised[id] {
			ids = append(ids, id)
		}
	}
	// If nothing in the allowlist is currently advertised (e.g. the user is
	// on an enterprise plan with a different catalog), fall back to the
	// allowlist itself so the dropdown still populates.
	if len(ids) == 0 {
		ids = append(ids, SupportedModels...)
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
