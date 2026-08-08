package aiprovider

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
)

// streamIdleTimeout is how long a proxied SSE stream may go without any data
// before it is treated as stalled. Matches internal/copilot: far tighter than
// the handler's overall cap so a hung upstream fails fast with an actionable
// error instead of pinning the connection for minutes.
const streamIdleTimeout = 60 * time.Second

// errStreamStalled is returned when the upstream stops sending for longer
// than streamIdleTimeout.
var errStreamStalled = errors.New("upstream stalled (no data received for 60s)")

// APIError is returned when a provider answers with a non-2xx status.
type APIError struct {
	Provider string
	Status   int
	Body     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: status %d: %s", e.Provider, e.Status, truncate(e.Body, 300))
}

// openaiClient speaks the OpenAI /chat/completions protocol. Every provider
// except Anthropic and Copilot goes through here.
type openaiClient struct {
	provider Provider
	cfg      ProviderConfig
	http     *http.Client
}

func newOpenAIClient(p Provider, cfg ProviderConfig) *openaiClient {
	return &openaiClient{provider: p, cfg: cfg, http: &http.Client{}}
}

func (c *openaiClient) authorize(req *http.Request) {
	if key := c.cfg.APIKey; key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

// StreamChat forwards the browser's payload to the provider and copies the
// SSE response into out untouched — both sides already speak this protocol,
// so there is nothing to translate.
func (c *openaiClient) StreamChat(ctx context.Context, payload map[string]any, out io.Writer) error {
	if payload == nil {
		return errors.New("empty chat request")
	}
	if m, ok := payload["model"].(string); !ok || m == "" {
		payload["model"] = c.cfg.Model
	}
	payload["stream"] = true

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode chat request: %w", err)
	}

	// A cancellable child context lets the idle watchdog abort a stalled
	// stream (unblocking the in-flight Body.Read) independently of the
	// caller's overall timeout.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	req, err := http.NewRequestWithContext(streamCtx, "POST", c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s chat request: %w", c.provider.Label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return &APIError{Provider: c.provider.Label, Status: resp.StatusCode, Body: string(b)}
	}
	if err := copyWithIdleTimeout(out, resp.Body, streamIdleTimeout, cancelStream); err != nil {
		return fmt.Errorf("%s stream: %w", c.provider.Label, err)
	}
	return nil
}

// ListModels asks the provider's /models endpoint what it can serve.
func (c *openaiClient) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s models request: %w", c.provider.Label, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Provider: c.provider.Label, Status: resp.StatusCode, Body: string(b)}
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			ids = append(ids, normalizeModelID(m.ID))
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("provider advertised no models")
	}
	return ids, nil
}

// normalizeModelID strips the "models/" prefix Google's OpenAI-compatible
// /models endpoint puts on every entry. /chat/completions there accepts both
// forms, and the bare ID is what the user recognizes in the dropdown.
func normalizeModelID(id string) string {
	const prefix = "models/"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
}

// copyWithIdleTimeout copies src to dst like io.Copy, but calls onIdle (which
// cancels the request context, unblocking a stuck Read) when no data has
// arrived for `idle`. The deadline resets on every chunk, so a slow but
// steadily-streaming completion is never interrupted — only a genuinely
// stalled upstream is.
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

// truncate shortens s to at most n runes, appending an ellipsis. Counting by
// runes avoids slicing through a multibyte UTF-8 sequence.
func truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
