// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jnnngs/3270Web/internal/aipolicy"
	"io"
	"strings"
	"testing"
	"time"
)

// decodeOpenAIPayload parses an OpenAI-shaped request body the way the chat
// panel builds it.
func decodeOpenAIPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	return payload
}

func TestToAnthropicRequestMovesSystemOutOfMessages(t *testing.T) {
	payload := decodeOpenAIPayload(t, `{
		"model": "claude-opus-5",
		"messages": [
			{"role": "system", "content": "You drive a 3270 terminal."},
			{"role": "user", "content": "Read the screen"}
		]
	}`)
	req, err := toAnthropicRequest(payload, "fallback-model")
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	if len(req.System) != 1 || req.System[0].Type != "text" || req.System[0].Text != "You drive a 3270 terminal." {
		t.Errorf("System = %+v, want the system turn hoisted out as one text block", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("Messages = %+v, want a single user turn", req.Messages)
	}
	if !req.Stream {
		t.Error("Stream = false, want streaming forced on")
	}
}

func TestToAnthropicRequestFloorsMaxTokens(t *testing.T) {
	// The panel sends 4096, which is sized for a non-thinking completion and
	// would truncate a thinking Claude turn.
	low := decodeOpenAIPayload(t, `{"messages":[{"role":"user","content":"hi"}],"max_tokens":4096}`)
	req, err := toAnthropicRequest(low, "claude-opus-5")
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	if req.MaxTokens != anthropicMinMaxTokens {
		t.Errorf("MaxTokens = %d, want the %d floor", req.MaxTokens, anthropicMinMaxTokens)
	}

	high := decodeOpenAIPayload(t, `{"messages":[{"role":"user","content":"hi"}],"max_tokens":64000}`)
	req, err = toAnthropicRequest(high, "claude-opus-5")
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	if req.MaxTokens != 64000 {
		t.Errorf("MaxTokens = %d, want a larger caller value respected", req.MaxTokens)
	}
}

func TestToAnthropicRequestDropsUnsupportedSamplingParams(t *testing.T) {
	// temperature/top_p are accepted-and-ignored by OpenAI but rejected
	// outright by current Claude models, so they must not be forwarded.
	payload := decodeOpenAIPayload(t, `{
		"messages": [{"role": "user", "content": "hi"}],
		"temperature": 0.7,
		"top_p": 0.9,
		"frequency_penalty": 1
	}`)
	req, err := toAnthropicRequest(payload, "claude-opus-5")
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{"temperature", "top_p", "frequency_penalty"} {
		if strings.Contains(string(body), field) {
			t.Errorf("request body forwarded %q: %s", field, body)
		}
	}
}

func TestToAnthropicRequestConvertsToolCallsAndResults(t *testing.T) {
	payload := decodeOpenAIPayload(t, `{
		"messages": [
			{"role": "user", "content": "look at the screen"},
			{"role": "assistant", "content": "Checking.", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_screen", "arguments": "{\"verbose\":true}"}},
				{"id": "call_2", "type": "function", "function": {"name": "chaos_status", "arguments": ""}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "SCREEN TEXT"},
			{"role": "tool", "tool_call_id": "call_2", "content": ""}
		],
		"tools": [
			{"type": "function", "function": {"name": "get_screen", "description": "Read it", "parameters": {"type": "object", "properties": {}}}}
		],
		"tool_choice": "auto"
	}`)
	req, err := toAnthropicRequest(payload, "claude-opus-5")
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}

	if len(req.Messages) != 3 {
		t.Fatalf("got %d messages, want user/assistant/user: %+v", len(req.Messages), req.Messages)
	}
	assistant := req.Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 3 {
		t.Fatalf("assistant turn = %+v, want text + two tool_use blocks", assistant)
	}
	if assistant.Content[1].Type != "tool_use" || assistant.Content[1].Name != "get_screen" {
		t.Errorf("first tool block = %+v, want tool_use get_screen", assistant.Content[1])
	}
	if string(assistant.Content[1].Input) != `{"verbose":true}` {
		t.Errorf("tool input = %s, want the arguments string decoded to an object", assistant.Content[1].Input)
	}
	// Empty arguments must still produce a well-formed object, or Anthropic
	// rejects the whole turn.
	if string(assistant.Content[2].Input) != `{}` {
		t.Errorf("empty tool input = %s, want {}", assistant.Content[2].Input)
	}

	// Both tool results belong to ONE user turn — Anthropic requires
	// alternating roles, so two consecutive user messages would 400.
	results := req.Messages[2]
	if results.Role != "user" || len(results.Content) != 2 {
		t.Fatalf("tool results = %+v, want both merged into one user turn", results)
	}
	if results.Content[0].Type != "tool_result" || results.Content[0].ToolUseID != "call_1" {
		t.Errorf("first result = %+v, want tool_result for call_1", results.Content[0])
	}
	if results.Content[1].Content == "" {
		t.Error("empty tool output produced an empty tool_result body, which Anthropic rejects")
	}

	if len(req.Tools) != 1 || req.Tools[0].Name != "get_screen" || req.Tools[0].InputSchema == nil {
		t.Errorf("Tools = %+v, want the function schema mapped to input_schema", req.Tools)
	}
	if req.ToolChoice["type"] != "auto" {
		t.Errorf("ToolChoice = %+v, want {type: auto}", req.ToolChoice)
	}
}

func TestToAnthropicRequestRejectsEmptyConversation(t *testing.T) {
	payload := decodeOpenAIPayload(t, `{"messages":[{"role":"system","content":"only a system prompt"}]}`)
	if _, err := toAnthropicRequest(payload, "claude-opus-5"); err == nil {
		t.Error("accepted a conversation with no user turn")
	}
}

func TestConvertToolChoiceForms(t *testing.T) {
	cases := []struct {
		in   any
		want map[string]any
	}{
		{"auto", map[string]any{"type": "auto"}},
		{"none", map[string]any{"type": "none"}},
		{"required", map[string]any{"type": "any"}},
		{map[string]any{"type": "function", "function": map[string]any{"name": "get_screen"}},
			map[string]any{"type": "tool", "name": "get_screen"}},
	}
	for _, tc := range cases {
		got, ok := convertToolChoice(tc.in)
		if !ok {
			t.Errorf("convertToolChoice(%v) reported no mapping", tc.in)
			continue
		}
		for k, want := range tc.want {
			if got[k] != want {
				t.Errorf("convertToolChoice(%v)[%q] = %v, want %v", tc.in, k, got[k], want)
			}
		}
	}
	if _, ok := convertToolChoice("gibberish"); ok {
		t.Error("convertToolChoice invented a mapping for an unknown value")
	}
}

// collectChunks parses the OpenAI-shaped SSE the translator produced.
func collectChunks(t *testing.T, sse string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, frame := range strings.Split(sse, "\n\n") {
		frame = strings.TrimSpace(frame)
		if !strings.HasPrefix(frame, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(frame, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("translator emitted invalid JSON %q: %v", data, err)
		}
		out = append(out, chunk)
	}
	return out
}

func firstDelta(chunk map[string]any) map[string]any {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	return delta
}

func TestTranslateAnthropicStreamText(t *testing.T) {
	upstream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_123"}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		``,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var out strings.Builder
	if err := translateAnthropicStream(context.Background(), strings.NewReader(upstream), &out, func() {}); err != nil {
		t.Fatalf("translateAnthropicStream: %v", err)
	}
	sse := out.String()
	if !strings.HasSuffix(strings.TrimSpace(sse), "[DONE]") {
		t.Errorf("stream did not terminate with [DONE]:\n%s", sse)
	}

	chunks := collectChunks(t, sse)
	var text string
	var finish string
	for _, chunk := range chunks {
		if chunk["id"] != "msg_123" {
			t.Errorf("chunk id = %v, want the upstream message id", chunk["id"])
		}
		if s, ok := firstDelta(chunk)["content"].(string); ok {
			text += s
		}
		choices, _ := chunk["choices"].([]any)
		choice, _ := choices[0].(map[string]any)
		if fr, ok := choice["finish_reason"].(string); ok {
			finish = fr
		}
	}
	if text != "Hello world" {
		t.Errorf("reassembled text = %q, want %q", text, "Hello world")
	}
	if finish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finish)
	}
}

func TestTranslateAnthropicStreamToolUse(t *testing.T) {
	// Text block at index 0, two tool_use blocks at 1 and 2: the translator
	// must renumber them densely, because the panel keys tool calls by the
	// OpenAI tool_calls index.
	upstream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_9"}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Looking."}}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"get_screen"}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"verb"}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ose\":true}"}}`,
		``,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_b","name":"chaos_status"}}`,
		``,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var out strings.Builder
	if err := translateAnthropicStream(context.Background(), strings.NewReader(upstream), &out, func() {}); err != nil {
		t.Fatalf("translateAnthropicStream: %v", err)
	}

	// Reassemble exactly the way web/static/copilot-panel.js does.
	type call struct{ id, name, args string }
	calls := map[float64]*call{}
	var finish string
	for _, chunk := range collectChunks(t, out.String()) {
		choices, _ := chunk["choices"].([]any)
		choice, _ := choices[0].(map[string]any)
		if fr, ok := choice["finish_reason"].(string); ok {
			finish = fr
		}
		raw, _ := firstDelta(chunk)["tool_calls"].([]any)
		for _, item := range raw {
			tc, _ := item.(map[string]any)
			idx, _ := tc["index"].(float64)
			cur := calls[idx]
			if cur == nil {
				cur = &call{}
				calls[idx] = cur
			}
			if id, ok := tc["id"].(string); ok && id != "" {
				cur.id = id
			}
			fn, _ := tc["function"].(map[string]any)
			if name, ok := fn["name"].(string); ok && name != "" {
				cur.name += name
			}
			if args, ok := fn["arguments"].(string); ok {
				cur.args += args
			}
		}
	}

	if finish != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls (this is what drives the panel's tool loop)", finish)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2: %+v", len(calls), calls)
	}
	first, ok := calls[0]
	if !ok {
		t.Fatalf("no tool call at index 0 — indices were not renumbered densely: %+v", calls)
	}
	if first.id != "toolu_a" || first.name != "get_screen" || first.args != `{"verbose":true}` {
		t.Errorf("tool call 0 = %+v, want get_screen with reassembled arguments", *first)
	}
	second, ok := calls[1]
	if !ok {
		t.Fatalf("no tool call at index 1: %+v", calls)
	}
	if second.id != "toolu_b" || second.name != "chaos_status" || second.args != "{}" {
		t.Errorf("tool call 1 = %+v, want chaos_status with {}", *second)
	}
}

func TestTranslateAnthropicStreamSurfacesErrorFrames(t *testing.T) {
	upstream := `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` + "\n\n"
	var out strings.Builder
	err := translateAnthropicStream(context.Background(), strings.NewReader(upstream), &out, func() {})
	if err == nil || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("error = %v, want the upstream message surfaced", err)
	}
}

// An upstream that closes without message_stop has been cut off. It used to
// be closed out as a clean stop, so whatever had arrived — including a tool
// call whose arguments stopped mid-JSON — was handed on as a complete answer.
func TestTranslateAnthropicStreamReportsATruncatedUpstream(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"send_key"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"key\": \"PF"}}`,
		``,
	}, "\n")
	var out strings.Builder
	err := translateAnthropicStream(context.Background(), strings.NewReader(upstream), &out, func() {})
	if !errors.Is(err, aipolicy.ErrStreamIncomplete) {
		t.Fatalf("err = %v, want ErrStreamIncomplete", err)
	}
	if strings.Contains(out.String(), "[DONE]") {
		t.Errorf("a truncated stream was terminated as though it were complete:\n%s", out.String())
	}
	if strings.Contains(out.String(), `"finish_reason"`) {
		t.Errorf("a truncated stream was given a finish reason:\n%s", out.String())
	}
}

// The idle watchdog stops a stalled stream by cancelling the request. What the
// user is told is that the stream stalled, not "context canceled".
func TestTranslateAnthropicStreamReportsAStallAsAStall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, `data: {"type":"message_start","message":{"id":"msg_1"}}`+"\n\n")
		// ...and then nothing. The reader is released when the watchdog
		// cancels the request, as closing the response body would.
		<-ctx.Done()
		_ = reader.CloseWithError(ctx.Err())
	}()

	var out strings.Builder
	err := translateAnthropicStreamIdle(ctx, reader, &out, cancel, 50*time.Millisecond)
	if !errors.Is(err, errStreamStalled) {
		t.Fatalf("err = %v, want the stalled-stream error", err)
	}
	if strings.Contains(err.Error(), "context canceled") {
		t.Errorf("the stall is reported as %q", err)
	}
}

// A cancellation that was not the watchdog's is still a cancellation.
func TestTranslateAnthropicStreamKeepsACallersCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	upstream := `data: {"type":"message_start","message":{"id":"msg_1"}}` + "\n\n"
	var out strings.Builder
	err := translateAnthropicStream(ctx, strings.NewReader(upstream), &out, func() {})
	if !errors.Is(err, context.Canceled) || errors.Is(err, errStreamStalled) {
		t.Errorf("err = %v, want context.Canceled and not a stall", err)
	}
}

// --- prompt caching ---------------------------------------------------------

// Every round of a tool loop resends the tools, the system prompt and the
// whole conversation. Without breakpoints all of it is billed as new input
// each time.
func TestToAnthropicRequestMarksCacheBreakpoints(t *testing.T) {
	payload := decodeOpenAIPayload(t, `{
		"model": "claude-opus-5",
		"messages": [
			{"role": "system", "content": "STATIC PROMPT"},
			{"role": "system", "content": "per-turn session context"},
			{"role": "user", "content": "first question"},
			{"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_screen", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "screen text"}
		],
		"tools": [
			{"type": "function", "function": {"name": "get_screen", "description": "read", "parameters": {"type": "object", "properties": {}}}},
			{"type": "function", "function": {"name": "send_key", "description": "press", "parameters": {"type": "object", "properties": {}}}}
		]
	}`)
	req, err := toAnthropicRequest(payload, "")
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}

	// System: one block per system turn, the breakpoint after the static one
	// so the part that changes every turn does not invalidate it.
	if len(req.System) != 2 {
		t.Fatalf("System = %+v, want two blocks", req.System)
	}
	if req.System[0].Text != "STATIC PROMPT" || req.System[0].CacheControl == nil || req.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("the static prompt should carry the breakpoint: %+v", req.System[0])
	}
	if req.System[1].CacheControl != nil {
		t.Errorf("the per-turn context must sit after the breakpoint, not carry one: %+v", req.System[1])
	}

	// Tools: the last definition, and only the last.
	if req.Tools[0].CacheControl != nil || req.Tools[1].CacheControl == nil {
		t.Errorf("only the last tool should carry a breakpoint: %+v", req.Tools)
	}

	// Conversation: the last block of the last message, and no other.
	marked := 0
	for mi, msg := range req.Messages {
		for bi, block := range msg.Content {
			if block.CacheControl == nil {
				continue
			}
			marked++
			if mi != len(req.Messages)-1 || bi != len(msg.Content)-1 {
				t.Errorf("a breakpoint is on message %d block %d, not on the end of the conversation", mi, bi)
			}
		}
	}
	if marked != 1 {
		t.Errorf("%d conversation breakpoints, want 1", marked)
	}

	// On the wire: system is an array of blocks and the markers are spelled
	// the way the API reads them. Four breakpoints at most are allowed.
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if !strings.Contains(wire, `"system":[{"type":"text","text":"STATIC PROMPT","cache_control":{"type":"ephemeral"}}`) {
		t.Errorf("system is not sent as blocks with a breakpoint:\n%s", wire)
	}
	if n := strings.Count(wire, `"cache_control"`); n != 3 {
		t.Errorf("%d breakpoints on the wire, want 3 (the API allows 4)", n)
	}
}

// A request with no tools and no system prompt still gets the conversation
// breakpoint and nothing malformed.
func TestCacheBreakpointsWithNothingToMark(t *testing.T) {
	payload := decodeOpenAIPayload(t, `{"model": "m", "messages": [{"role": "user", "content": "hi"}]}`)
	req, err := toAnthropicRequest(payload, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(req)
	if strings.Contains(string(raw), `"system"`) || strings.Contains(string(raw), `"tools"`) {
		t.Errorf("empty system/tools should be omitted: %s", raw)
	}
	if strings.Count(string(raw), `"cache_control"`) != 1 {
		t.Errorf("want the one conversation breakpoint: %s", raw)
	}
}

func TestTranslateAnthropicStreamDropsThinking(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"internal reasoning"}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	var out strings.Builder
	if err := translateAnthropicStream(context.Background(), strings.NewReader(upstream), &out, func() {}); err != nil {
		t.Fatalf("translateAnthropicStream: %v", err)
	}
	if strings.Contains(out.String(), "internal reasoning") {
		t.Errorf("thinking content leaked into the visible answer:\n%s", out.String())
	}
	var text string
	for _, chunk := range collectChunks(t, out.String()) {
		if s, ok := firstDelta(chunk)["content"].(string); ok {
			text += s
		}
	}
	if text != "answer" {
		t.Errorf("text = %q, want just the answer", text)
	}
}

func TestOpenAIFinishReasonMapping(t *testing.T) {
	cases := map[string]string{
		"tool_use":      "tool_calls",
		"max_tokens":    "length",
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"refusal":       "stop",
		"something_new": "stop",
	}
	for in, want := range cases {
		if got := openAIFinishReason(in); got != want {
			t.Errorf("openAIFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}
