// Ver 2026-08-28, by Sonnet 5
package chatmsg

import (
	"strings"
	"testing"
)

func TestExtractReasoning(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want string
	}{
		{
			name: "nil map",
			in:   nil,
			want: "",
		},
		{
			name: "empty map",
			in:   map[string]any{},
			want: "",
		},
		{
			name: "reasoning_content only",
			in:   map[string]any{"reasoning_content": "think rc"},
			want: "think rc",
		},
		{
			name: "reasoning only (Ollama / OpenRouter)",
			in:   map[string]any{"reasoning": "think r"},
			want: "think r",
		},
		{
			name: "thought only (Qwen / Kimi / MiniMax)",
			in:   map[string]any{"thought": "think th"},
			want: "think th",
		},
		{
			name: "reasoning_content takes precedence over reasoning and thought",
			in: map[string]any{
				"reasoning_content": "rc wins",
				"reasoning":         "r second",
				"thought":           "th third",
			},
			want: "rc wins",
		},
		{
			name: "reasoning takes precedence over thought when reasoning_content is empty",
			in: map[string]any{
				"reasoning_content": "",
				"reasoning":         "r wins",
				"thought":           "th third",
			},
			want: "r wins",
		},
		{
			name: "thought selected when earlier keys are empty",
			in: map[string]any{
				"reasoning_content": "",
				"reasoning":         "",
				"thought":           "th fallback",
			},
			want: "th fallback",
		},
		{
			name: "non-string values ignored safely",
			in: map[string]any{
				"reasoning_content": 12345,
				"reasoning":         true,
				"thought":           "valid thought string",
			},
			want: "valid thought string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractReasoning(tc.in)
			if got != tc.want {
				t.Errorf("ExtractReasoning(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReassembleSSE_ReasoningVariants(t *testing.T) {
	// 1. Ollama / OpenRouter style: "reasoning" delta
	ollamaSSE := `data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"Thinking step 1. "}}]}
data: {"choices":[{"index":0,"delta":{"reasoning":"Thinking step 2."}}]}
data: {"choices":[{"index":0,"delta":{"content":"Final answer."}}]}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}
data: [DONE]`

	s := ReassembleSSE(ollamaSSE)
	if s == nil {
		t.Fatal("ReassembleSSE returned nil for Ollama SSE stream")
	}
	if s.Reasoning != "Thinking step 1. Thinking step 2." {
		t.Errorf("Reasoning = %q, want %q", s.Reasoning, "Thinking step 1. Thinking step 2.")
	}
	if s.Content != "Final answer." {
		t.Errorf("Content = %q, want %q", s.Content, "Final answer.")
	}

	// 2. Qwen / Kimi / MiniMax style: "thought" delta
	qwenSSE := `data: {"choices":[{"index":0,"delta":{"role":"assistant","thought":"Plan approach. "}}]}
data: {"choices":[{"index":0,"delta":{"thought":"Verify calculations."}}]}
data: {"choices":[{"index":0,"delta":{"content":"42"}}]}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}
data: [DONE]`

	s2 := ReassembleSSE(qwenSSE)
	if s2 == nil {
		t.Fatal("ReassembleSSE returned nil for Qwen SSE stream")
	}
	if s2.Reasoning != "Plan approach. Verify calculations." {
		t.Errorf("Reasoning = %q, want %q", s2.Reasoning, "Plan approach. Verify calculations.")
	}
	if s2.Content != "42" {
		t.Errorf("Content = %q, want %q", s2.Content, "42")
	}
}

func TestFinalMessage_ReasoningVariants(t *testing.T) {
	// 1. Ollama / OpenRouter style non-streaming body with "reasoning"
	ollamaBody := map[string]any{
		"model": "deepseek-r1",
		"choices": []any{
			map[string]any{
				"finish_reason": "stop",
				"message": map[string]any{
					"role":      "assistant",
					"reasoning": "Let's analyze the problem carefully.",
					"content":   "Here is the solution.",
				},
			},
		},
	}
	s, ok := FinalMessage(ollamaBody)
	if !ok || s == nil {
		t.Fatal("FinalMessage failed for Ollama body")
	}
	if s.Reasoning != "Let's analyze the problem carefully." {
		t.Errorf("Reasoning = %q, want %q", s.Reasoning, "Let's analyze the problem carefully.")
	}
	if s.Content != "Here is the solution." {
		t.Errorf("Content = %q, want %q", s.Content, "Here is the solution.")
	}

	// 2. Qwen / Kimi / MiniMax style non-streaming body with "thought"
	qwenBody := map[string]any{
		"model": "qwen-qwq",
		"choices": []any{
			map[string]any{
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"thought": "Deduce step by step.",
					"content": "Result is correct.",
				},
			},
		},
	}
	s2, ok := FinalMessage(qwenBody)
	if !ok || s2 == nil {
		t.Fatal("FinalMessage failed for Qwen body")
	}
	if s2.Reasoning != "Deduce step by step." {
		t.Errorf("Reasoning = %q, want %q", s2.Reasoning, "Deduce step by step.")
	}
	if s2.Content != "Result is correct." {
		t.Errorf("Content = %q, want %q", s2.Content, "Result is correct.")
	}
}

func TestMessages_ReasoningVariants(t *testing.T) {
	// Request containing messages with reasoning / thought fields
	reqBody := map[string]any{
		"messages": []any{
			map[string]any{
				"role":      "assistant",
				"reasoning": "Ollama reasoning history",
				"content":   "Assistant turn 1",
			},
			map[string]any{
				"role":    "assistant",
				"thought": "Qwen thought history",
				"content": "Assistant turn 2",
			},
			map[string]any{
				"role":              "assistant",
				"reasoning_content": "DeepSeek reasoning_content history",
				"content":           "Assistant turn 3",
			},
		},
	}

	msgs := Messages(reqBody)
	if len(msgs) != 3 {
		t.Fatalf("Messages returned %d items, want 3", len(msgs))
	}

	if !strings.Contains(msgs[0].Text, "Ollama reasoning history") || !strings.Contains(msgs[0].Text, "Assistant turn 1") {
		t.Errorf("msg[0] = %q, want Ollama reasoning and content", msgs[0].Text)
	}
	if !strings.Contains(msgs[1].Text, "Qwen thought history") || !strings.Contains(msgs[1].Text, "Assistant turn 2") {
		t.Errorf("msg[1] = %q, want Qwen thought and content", msgs[1].Text)
	}
	if !strings.Contains(msgs[2].Text, "DeepSeek reasoning_content history") || !strings.Contains(msgs[2].Text, "Assistant turn 3") {
		t.Errorf("msg[2] = %q, want DeepSeek reasoning_content and content", msgs[2].Text)
	}
}
