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
		t.Errorf("FinalMessage openai = %+v ok=%v", s, ok)
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
