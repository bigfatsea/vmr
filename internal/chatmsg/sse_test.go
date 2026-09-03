// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import (
	"strings"
	"testing"
)

func sseToolCall(tool string) string {
	return strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}],"model":"agent"}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"` + tool + `","arguments":"{}"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":4},"prompt_tokens_details":{"cached_tokens":80}}}`,
		``,
		`data: [DONE]`,
	}, "\n")
}

func sseText(text string) string {
	return strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}`,
		``,
		`data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}`,
		``,
		`data: [DONE]`,
	}, "\n")
}

func TestReassembleSSE_OpenAIToolCall(t *testing.T) {
	t.Parallel()
	s := ReassembleSSE(sseToolCall("exec"))
	if s == nil {
		t.Fatal("ReassembleSSE returned nil")
	}
	if s.Finish != "tool_calls" {
		t.Errorf("Finish = %q, want tool_calls", s.Finish)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "exec" {
		t.Errorf("ToolCalls = %+v", s.ToolCalls)
	}
}

func TestReassembleSSE_OpenAIText(t *testing.T) {
	t.Parallel()
	s := ReassembleSSE(sseText("hello world"))
	if s == nil || s.Content != "hello world" || s.Finish != "stop" {
		t.Errorf("ReassembleSSE text = %+v", s)
	}
}

func TestReassembleSSE_AnthropicEvents(t *testing.T) {
	t.Parallel()
	raw := strings.Join([]string{
		`data: {"type":"message_start","message":{"model":"claude"}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"read"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
	}, "\n")
	s := ReassembleSSE(raw)
	if s == nil {
		t.Fatal("nil summary")
	}
	if s.Model != "claude" || s.Finish != "tool_use" {
		t.Errorf("summary = %+v", s)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "read" || s.ToolCalls[0].ID != "t1" {
		t.Errorf("ToolCalls = %+v", s.ToolCalls)
	}
}

func TestReassembleSSE_NoParsableLines(t *testing.T) {
	t.Parallel()
	if s := ReassembleSSE("not sse at all"); s != nil {
		t.Errorf("expected nil, got %+v", s)
	}
}

func TestFinalMessage_OpenAI(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"choices": []any{
			map[string]any{"finish_reason": "stop", "message": map[string]any{"content": "hi"}},
		},
	}
	s, ok := FinalMessage(body)
	if !ok || s.Content != "hi" || s.Finish != "stop" {
		t.Errorf("FinalMessage openai-completions = %+v ok=%v", s, ok)
	}
}

func TestFinalMessage_Anthropic(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"stop_reason": "end_turn",
		"content": []any{
			map[string]any{"type": "text", "text": "hi"},
			map[string]any{"type": "tool_use", "id": "t1", "name": "read", "input": map[string]any{}},
		},
	}
	s, ok := FinalMessage(body)
	if !ok || s.Content != "hi" || s.Finish != "end_turn" || len(s.ToolCalls) != 1 {
		t.Errorf("FinalMessage anthropic = %+v ok=%v", s, ok)
	}
}

func TestFinalMessage_UnrecognizedShape(t *testing.T) {
	t.Parallel()
	if _, ok := FinalMessage(map[string]any{"foo": "bar"}); ok {
		t.Error("expected ok=false for unrecognized body shape")
	}
	if _, ok := FinalMessage("a string"); ok {
		t.Error("expected ok=false for non-map body")
	}
}

// TestFinalMessage_ResponsesOutputArray proves the gap this fix closes: a
// non-streaming openai-responses response body's "output" typed-Item array
// (message + reasoning + function_call mixed together, the way a real tool-
// calling turn looks) is fully reconstructed into a StreamSummary — content,
// reasoning summary, and tool calls alike — not silently dropped as an
// "unrecognized shape". Before this fix FinalMessage only knew "choices"
// (openai) and "content" (anthropic), so every Responses-protocol response
// vmr report/vmr story tried to summarize came back ok=false.
func TestFinalMessage_ResponsesOutputArray(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"model":  "gpt-5.6",
		"status": "completed",
		"output": []any{
			map[string]any{
				"type":    "reasoning",
				"summary": []any{map[string]any{"type": "summary_text", "text": "thinking it through"}},
			},
			map[string]any{
				"type": "function_call", "call_id": "c1", "name": "exec", "arguments": `{"cmd":"ls"}`,
			},
			map[string]any{
				"type": "message", "role": "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "done"}},
			},
		},
	}
	s, ok := FinalMessage(body)
	if !ok {
		t.Fatal("expected ok=true for a Responses-shaped output array")
	}
	if s.Model != "gpt-5.6" || s.Finish != "completed" {
		t.Errorf("Model/Finish = %q/%q, want gpt-5.6/completed", s.Model, s.Finish)
	}
	if s.Content != "done" {
		t.Errorf("Content = %q, want %q", s.Content, "done")
	}
	if !strings.Contains(s.Reasoning, "thinking it through") {
		t.Errorf("Reasoning = %q, want it to contain the reasoning summary", s.Reasoning)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "exec" || s.ToolCalls[0].ID != "c1" {
		t.Errorf("ToolCalls = %+v", s.ToolCalls)
	}
}

// TestFinalMessage_ResponsesEmptyOutputStillRecognized mirrors the
// anthropic branch's "content: []" behavior: an empty (but present) output
// array is still a recognized Responses shape (ok=true, zero-value
// content) — not the same as "no output key at all" (ok=false).
func TestFinalMessage_ResponsesEmptyOutputStillRecognized(t *testing.T) {
	t.Parallel()
	s, ok := FinalMessage(map[string]any{"model": "m", "output": []any{}})
	if !ok || s.Content != "" {
		t.Errorf("FinalMessage with empty output = %+v ok=%v, want ok=true empty content", s, ok)
	}
}

// TestReassembleSSE_ResponsesCompletedEvent proves the streaming
// counterpart of TestFinalMessage_ResponsesOutputArray: a Responses SSE
// stream's typed events (none of which ReassembleSSE tries to parse
// individually — see responsesFinalMessage's doc comment on why only the
// terminal event is trusted) still yields a full StreamSummary once
// "response.completed" arrives, reusing the exact same output-Item parsing
// FinalMessage uses for the non-streaming case.
func TestReassembleSSE_ResponsesCompletedEvent(t *testing.T) {
	t.Parallel()
	raw := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.6","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","item_id":"msg_1","delta":"ignored, not parsed per-delta"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6","status":"completed","output":[` +
			`{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}]},` +
			`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final answer"}]}` +
			`]}}`,
	}, "\n")
	s := ReassembleSSE(raw)
	if s == nil {
		t.Fatal("nil summary")
	}
	if s.Model != "gpt-5.6" || s.Finish != "completed" {
		t.Errorf("Model/Finish = %q/%q, want gpt-5.6/completed", s.Model, s.Finish)
	}
	if s.Content != "final answer" {
		t.Errorf("Content = %q, want %q", s.Content, "final answer")
	}
	if !strings.Contains(s.Reasoning, "thinking") {
		t.Errorf("Reasoning = %q, want it to contain the reasoning summary", s.Reasoning)
	}
	if strings.Contains(s.Content, "ignored") {
		t.Errorf("a response.output_text.delta event must not be parsed for content: %q", s.Content)
	}
}

func TestReassembleSSE_CursorScanLargeStream(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("data: {\"model\":\"agent-large\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
	const chunks = 500
	for i := 0; i < chunks; i++ {
		b.WriteString("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chunk\"}}]}\n\n")
	}
	b.WriteString("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}]}\n\n")
	b.WriteString("data: [DONE]\n")

	s := ReassembleSSE(b.String())
	if s == nil {
		t.Fatal("ReassembleSSE returned nil for large stream")
	}
	if s.Model != "agent-large" || s.Finish != "stop" {
		t.Errorf("Model/Finish = %q/%q, want agent-large/stop", s.Model, s.Finish)
	}
	if s.Content != strings.Repeat("chunk", chunks) {
		t.Errorf("Content length = %d, want %d", len(s.Content), chunks*len("chunk"))
	}
}

// TestReassembleSSE_OpenAIToolCallManyDeltas pins the §2.66 fix: a single
// tool call arriving as many small `arguments` chunks must reassemble into
// exactly the concatenated JSON string (the public ToolCall.Args shape
// downstream pairing/render code reads). The string-builder migration in
// sse.go only changed the ACCUMULATION path — output shape is unchanged —
// so this test asserts the equality that downstream code depends on.
// Chunks use only letters/digits/braces/colons so each SSE event stays
// valid JSON, and the concatenated result is also valid JSON.
func TestReassembleSSE_OpenAIToolCallManyDeltas(t *testing.T) {
	t.Parallel()
	chunks := []string{
		`{abc`, `:2,`, `de`, `f:3,`, `ghi`, `:4,`, `jkl`, `mn:5`, `}`,
	}
	var b strings.Builder
	b.WriteString(`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}],"model":"agent"}` + "\n\n")
	b.WriteString(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search","arguments":""}}]}}]}` + "\n\n")
	for _, c := range chunks {
		b.WriteString(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"` + c + `"}}]}}]}` + "\n\n")
	}
	b.WriteString(`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}` + "\n\n")
	b.WriteString("data: [DONE]\n")

	s := ReassembleSSE(b.String())
	if s == nil {
		t.Fatal("nil")
	}
	if len(s.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", s.ToolCalls)
	}
	want := `{abc:2,def:3,ghi:4,jklmn:5}`
	if s.ToolCalls[0].Args != want {
		t.Errorf("Args = %q, want %q", s.ToolCalls[0].Args, want)
	}
	if s.ToolCalls[0].Name != "search" || s.ToolCalls[0].ID != "c1" {
		t.Errorf("Name/ID = %q/%q", s.ToolCalls[0].Name, s.ToolCalls[0].ID)
	}
}

// TestReassembleSSE_AnthropicToolCallManyDeltas is the Anthropic-shaped
// counterpart: many `input_json_delta` chunks across `content_block_delta`
// events must also reassemble into the same concatenated JSON string. The
// builders map is indexed by content_block index, not by anything visible in
// the event itself.
func TestReassembleSSE_AnthropicToolCallManyDeltas(t *testing.T) {
	t.Parallel()
	chunks := []string{
		`{a`, `rg1`, `:v1,`, `arg`, `2:v`, `2,fi`, `nal:`, `true}`,
	}
	var b strings.Builder
	b.WriteString(`data: {"type":"message_start","message":{"model":"claude"}}` + "\n\n")
	b.WriteString(`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"do"}}` + "\n\n")
	for _, c := range chunks {
		b.WriteString(`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"` + c + `"}}` + "\n\n")
	}
	b.WriteString(`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}` + "\n\n")

	s := ReassembleSSE(b.String())
	if s == nil {
		t.Fatal("nil")
	}
	if len(s.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", s.ToolCalls)
	}
	want := `{arg1:v1,arg2:v2,final:true}`
	if s.ToolCalls[0].Args != want {
		t.Errorf("Args = %q, want %q", s.ToolCalls[0].Args, want)
	}
}
