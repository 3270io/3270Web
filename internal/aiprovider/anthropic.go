// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jnnngs/3270Web/internal/aipolicy"
)

// anthropicVersion is the required API version header value.
const anthropicVersion = "2023-06-01"

// anthropicMinMaxTokens is the floor applied to max_tokens.
//
// On the Messages API max_tokens caps thinking *plus* response text, and
// current Claude models think by default. The chat panel sends 4096, which is
// sized for a Copilot-style non-thinking completion and would truncate a
// tool-heavy Claude turn mid-answer, so it is raised here rather than in the
// browser (where it would also affect every other provider).
const anthropicMinMaxTokens = 16000

// anthropicClient translates between the OpenAI chat/completions protocol the
// browser speaks and Anthropic's Messages protocol, in both directions.
type anthropicClient struct {
	provider Provider
	cfg      ProviderConfig
	http     *http.Client
}

func newAnthropicClient(p Provider, cfg ProviderConfig) *anthropicClient {
	return &anthropicClient{provider: p, cfg: cfg, http: outboundClient()}
}

func (c *anthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

// StreamChat converts payload to a Messages request, then converts the
// Anthropic SSE response back into OpenAI-shaped chunks written to out.
func (c *anthropicClient) StreamChat(ctx context.Context, payload map[string]any, out io.Writer) error {
	req, err := toAnthropicRequest(payload, c.cfg.Model)
	if err != nil {
		return err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode chat request: %w", err)
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	httpReq, err := http.NewRequestWithContext(streamCtx, "POST", c.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s chat request: %w", c.provider.Label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return &APIError{Provider: c.provider.Label, Status: resp.StatusCode, Body: string(b)}
	}
	if err := aipolicy.CheckContentType(resp, aipolicy.ContentEventStream); err != nil {
		return fmt.Errorf("%s chat response: %w", c.provider.Label, err)
	}
	if err := translateAnthropicStream(streamCtx, resp.Body, out, cancelStream); err != nil {
		return fmt.Errorf("%s stream: %w", c.provider.Label, err)
	}
	return nil
}

// ListModels asks Anthropic's /v1/models endpoint what this key can serve.
func (c *anthropicClient) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/v1/models?limit=100", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s models request: %w", c.provider.Label, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Provider: c.provider.Label, Status: resp.StatusCode, Body: string(b)}
	}
	if err := aipolicy.CheckContentType(resp, aipolicy.ContentJSON); err != nil {
		return nil, fmt.Errorf("%s models response: %w", c.provider.Label, err)
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
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("provider advertised no models")
	}
	return ids, nil
}

// -- request translation ---------------------------------------------------

type anthropicRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	// System is sent as text blocks rather than one string, because only a
	// block can carry a cache breakpoint.
	System     []anthropicBlock   `json:"system,omitempty"`
	Messages   []anthropicMessage `json:"messages"`
	Tools      []anthropicTool    `json:"tools,omitempty"`
	ToolChoice map[string]any     `json:"tool_choice,omitempty"`
	Stream     bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`

	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type anthropicTool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"input_schema"`
	CacheControl *cacheControl  `json:"cache_control,omitempty"`
}

// cacheControl marks a prompt-cache breakpoint: everything up to and including
// the block that carries it is cached, and a later request with the same
// prefix reads it back at a fraction of the input price.
type cacheControl struct {
	Type string `json:"type"`
}

func ephemeral() *cacheControl { return &cacheControl{Type: "ephemeral"} }

// applyCacheBreakpoints marks the three places a chat turn's prefix stops
// changing.
//
// Every request in a tool loop resends the whole conversation: some forty tool
// definitions, a system prompt of several thousand tokens, and every turn so
// far. Without breakpoints all of it is billed as fresh input each round — and
// a single user message can run to dozens of rounds. The prefix is rendered in
// the order tools, system, messages, and a breakpoint caches everything before
// it, so:
//
//   - the last tool definition: the tool list is the same for every request;
//   - the first system block: the static prompt. Anything after it in system
//     (the per-turn session context, when a client sends it there) changes
//     between turns and must sit after the breakpoint, or it would invalidate
//     the prompt it follows;
//   - the last block of the conversation so far: each round then re-reads the
//     rounds before it instead of paying for them again.
//
// Three of the four breakpoints a request may carry. A prefix too short to be
// cached is simply not cached; the markers cost nothing.
func applyCacheBreakpoints(req *anthropicRequest) {
	if n := len(req.Tools); n > 0 {
		req.Tools[n-1].CacheControl = ephemeral()
	}
	if len(req.System) > 0 {
		req.System[0].CacheControl = ephemeral()
	}
	if n := len(req.Messages); n > 0 {
		if blocks := req.Messages[n-1].Content; len(blocks) > 0 {
			blocks[len(blocks)-1].CacheControl = ephemeral()
		}
	}
}

// toAnthropicRequest maps an OpenAI chat/completions body onto the Messages
// API. Only the fields the chat panel actually sends are carried across —
// unknown extras are dropped rather than forwarded, because parameters that
// are merely ignored by OpenAI (temperature, top_p) are rejected outright by
// current Claude models.
func toAnthropicRequest(payload map[string]any, defaultModel string) (*anthropicRequest, error) {
	if payload == nil {
		return nil, errors.New("empty chat request")
	}
	out := &anthropicRequest{Stream: true}

	out.Model, _ = payload["model"].(string)
	if strings.TrimSpace(out.Model) == "" {
		out.Model = defaultModel
	}
	if strings.TrimSpace(out.Model) == "" {
		return nil, errors.New("no model selected")
	}

	out.MaxTokens = anthropicMinMaxTokens
	if n, ok := numberOf(payload["max_tokens"]); ok && int(n) > out.MaxTokens {
		out.MaxTokens = int(n)
	}

	rawMessages, _ := payload["messages"].([]any)
	system, messages, err := splitMessages(rawMessages)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("messages required")
	}
	for _, part := range system {
		out.System = append(out.System, anthropicBlock{Type: "text", Text: part})
	}
	out.Messages = messages

	if tools, ok := payload["tools"].([]any); ok && len(tools) > 0 {
		out.Tools = convertTools(tools)
	}
	if tc, ok := convertToolChoice(payload["tool_choice"]); ok && len(out.Tools) > 0 {
		out.ToolChoice = tc
	}
	applyCacheBreakpoints(out)
	return out, nil
}

// splitMessages pulls the system turns out into Anthropic's dedicated
// `system` field and converts the rest into content-block messages.
func splitMessages(raw []any) ([]string, []anthropicMessage, error) {
	var systemParts []string
	var msgs []anthropicMessage

	// appendUserBlocks merges into the previous message when it is already a
	// user turn. Anthropic requires alternating roles, and a run of OpenAI
	// "tool" messages (one per parallel tool call) all map to tool_result
	// blocks that belong in a single user turn.
	appendUserBlocks := func(blocks ...anthropicBlock) {
		if n := len(msgs); n > 0 && msgs[n-1].Role == "user" {
			msgs[n-1].Content = append(msgs[n-1].Content, blocks...)
			return
		}
		msgs = append(msgs, anthropicMessage{Role: "user", Content: blocks})
	}

	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch role {
		case "system", "developer":
			if s := contentText(m["content"]); s != "" {
				systemParts = append(systemParts, s)
			}
		case "user":
			if s := contentText(m["content"]); s != "" {
				appendUserBlocks(anthropicBlock{Type: "text", Text: s})
			}
		case "tool":
			id, _ := m["tool_call_id"].(string)
			if id == "" {
				continue
			}
			text := contentText(m["content"])
			if text == "" {
				// Anthropic rejects an empty tool_result body; a tool that
				// genuinely returned nothing still has to say so.
				text = "(no output)"
			}
			appendUserBlocks(anthropicBlock{Type: "tool_result", ToolUseID: id, Content: text})
		case "assistant":
			blocks := assistantBlocks(m)
			if len(blocks) == 0 {
				continue
			}
			if n := len(msgs); n > 0 && msgs[n-1].Role == "assistant" {
				msgs[n-1].Content = append(msgs[n-1].Content, blocks...)
				continue
			}
			msgs = append(msgs, anthropicMessage{Role: "assistant", Content: blocks})
		}
	}

	// The Messages API requires the conversation to open with a user turn.
	if len(msgs) > 0 && msgs[0].Role != "user" {
		return nil, nil, errors.New("conversation must start with a user message")
	}
	return systemParts, msgs, nil
}

// assistantBlocks converts one OpenAI assistant message (text and/or
// tool_calls) into Anthropic content blocks.
func assistantBlocks(m map[string]any) []anthropicBlock {
	var blocks []anthropicBlock
	if s := contentText(m["content"]); s != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: s})
	}
	calls, _ := m["tool_calls"].([]any)
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := call["id"].(string)
		fn, _ := call["function"].(map[string]any)
		if id == "" || fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		// OpenAI carries arguments as a JSON *string*; Anthropic wants the
		// decoded object. A model that emitted malformed JSON still needs a
		// well-formed tool_use block or the whole turn is rejected, so fall
		// back to an empty object.
		args := json.RawMessage("{}")
		if s, _ := fn["arguments"].(string); strings.TrimSpace(s) != "" && json.Valid([]byte(s)) {
			args = json.RawMessage(s)
		}
		blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: id, Name: name, Input: args})
	}
	return blocks
}

// contentText flattens an OpenAI content value (a string, or an array of
// content parts) down to plain text.
func contentText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, item := range t {
			p, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := p["text"].(string); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func convertTools(raw []any) []anthropicTool {
	out := make([]anthropicTool, 0, len(raw))
	for _, item := range raw {
		t, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := t["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		desc, _ := fn["description"].(string)
		schema, _ := fn["parameters"].(map[string]any)
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, anthropicTool{Name: name, Description: desc, InputSchema: schema})
	}
	return out
}

func convertToolChoice(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case string:
		switch t {
		case "auto":
			return map[string]any{"type": "auto"}, true
		case "none":
			return map[string]any{"type": "none"}, true
		case "required", "any":
			return map[string]any{"type": "any"}, true
		}
	case map[string]any:
		fn, _ := t["function"].(map[string]any)
		if fn != nil {
			if name, _ := fn["name"].(string); name != "" {
				return map[string]any{"type": "tool", "name": name}, true
			}
		}
	}
	return nil, false
}

func numberOf(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

// -- response translation --------------------------------------------------

// anthropicEvent is the subset of the Messages streaming event shape this
// translator needs.
type anthropicEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID string `json:"id"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// translateAnthropicStream reads Anthropic SSE frames from r and writes
// OpenAI-shaped SSE chunks to w, so web/static/copilot-panel.js can consume a
// Claude response with exactly the same parser it uses for every other
// provider.
func translateAnthropicStream(ctx context.Context, r io.Reader, w io.Writer, onIdle func()) error {
	return translateAnthropicStreamIdle(ctx, r, w, onIdle, streamIdleTimeout)
}

// translateAnthropicStreamIdle is translateAnthropicStream with the stall
// window given, so a test can make a stream stall without waiting a minute.
func translateAnthropicStreamIdle(ctx context.Context, r io.Reader, w io.Writer, onIdle func(), idleTimeout time.Duration) error {
	// Reuse the same stall watchdog as the pass-through path: reset it on
	// every frame, cancel the request if the upstream goes quiet.
	//
	// The watchdog stops a stalled stream by cancelling the request, and a
	// cancelled request reads as "context canceled" — which says nothing about
	// why, and is what the user was shown. It records that it fired, so the
	// failure can be reported as the stall it was.
	var stalled atomic.Bool
	idle := time.AfterFunc(idleTimeout, func() {
		stalled.Store(true)
		if onIdle != nil {
			onIdle()
		}
	})
	defer idle.Stop()
	streamErr := func(err error) error {
		if stalled.Load() {
			return errStreamStalled
		}
		return err
	}

	sc := bufio.NewScanner(r)
	// Anthropic tool_use input arrives as many small input_json_delta frames,
	// but a single text_delta can still be sizable; give the scanner room.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	msgID := "chatcmpl-anthropic"
	// toolIndex maps an Anthropic content-block index to a dense OpenAI
	// tool_calls index, so the browser sees 0,1,2… regardless of how many
	// text blocks were interleaved.
	toolIndex := map[int]int{}
	nextToolIndex := 0
	var finish string
	var writeErr error

	emit := func(delta map[string]any, finishReason any) {
		if writeErr != nil {
			return
		}
		choice := map[string]any{"index": 0, "delta": delta}
		if finishReason != nil {
			choice["finish_reason"] = finishReason
		}
		chunk := map[string]any{
			"id":      msgID,
			"object":  "chat.completion.chunk",
			"choices": []any{choice},
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		if _, err := w.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
			writeErr = err
		}
	}

	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return streamErr(err)
		}
		line := strings.TrimRight(sc.Text(), "\r")
		if !strings.HasPrefix(line, "data:") {
			// `event:` lines are redundant — every frame's payload carries its
			// own "type" — and blank separators carry nothing.
			continue
		}
		idle.Reset(idleTimeout)
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev anthropicEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.ID != "" {
				msgID = ev.Message.ID
			}
		case "content_block_start":
			if ev.ContentBlock == nil || ev.ContentBlock.Type != "tool_use" {
				continue
			}
			idx := nextToolIndex
			nextToolIndex++
			toolIndex[ev.Index] = idx
			emit(map[string]any{"tool_calls": []any{map[string]any{
				"index":    idx,
				"id":       ev.ContentBlock.ID,
				"type":     "function",
				"function": map[string]any{"name": ev.ContentBlock.Name, "arguments": ""},
			}}}, nil)
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					emit(map[string]any{"content": ev.Delta.Text}, nil)
				}
			case "input_json_delta":
				idx, ok := toolIndex[ev.Index]
				if !ok || ev.Delta.PartialJSON == "" {
					continue
				}
				emit(map[string]any{"tool_calls": []any{map[string]any{
					"index":    idx,
					"function": map[string]any{"arguments": ev.Delta.PartialJSON},
				}}}, nil)
			}
			// thinking_delta and signature_delta are intentionally dropped:
			// the panel has no place to render reasoning, and forwarding it
			// as assistant text would mix it into the visible answer.
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				finish = openAIFinishReason(ev.Delta.StopReason)
			}
		case "message_stop":
			emit(map[string]any{}, finishOrStop(finish))
			if writeErr == nil {
				_, writeErr = io.WriteString(w, "data: [DONE]\n\n")
			}
			return writeErr
		case "error":
			msg := "upstream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			return errors.New(msg)
		}
		if writeErr != nil {
			return writeErr
		}
	}
	if err := sc.Err(); err != nil {
		return streamErr(err)
	}
	if stalled.Load() || ctx.Err() != nil {
		return streamErr(ctx.Err())
	}
	// Upstream closed without message_stop. That is a response cut off part
	// way — a dropped connection, a proxy timeout — and it used to be closed
	// out as a clean stop. Whatever had arrived was then treated as the whole
	// answer: a tool call whose arguments stopped mid-JSON was handed to the
	// tool loop as something to run. It is an error, and the caller reports it
	// as one, so nothing partial is acted on.
	return aipolicy.ErrStreamIncomplete
}

func finishOrStop(finish string) string {
	if finish == "" {
		return "stop"
	}
	return finish
}

// openAIFinishReason maps an Anthropic stop_reason onto the finish_reason
// vocabulary the chat panel branches on ("tool_calls" drives the tool loop).
func openAIFinishReason(stop string) string {
	switch stop {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "end_turn", "stop_sequence", "refusal", "pause_turn":
		return "stop"
	default:
		return "stop"
	}
}
