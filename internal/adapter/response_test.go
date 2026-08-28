// Ver 2026-08-28, by Sonnet 5

package adapter

import "testing"

func TestResponseAssistantText(t *testing.T) {
	cases := []struct {
		name      string
		protocol  string
		body      string
		wantRunes int
		wantTool  bool
		wantOK    bool
	}{
		{
			name:     "openai empty content (soft block shape)",
			protocol: "openai-completions",
			body:     `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"input_sensitive":true}`,
			wantOK:   true,
		},
		{
			name:      "openai real answer",
			protocol:  "openai-completions",
			body:      `{"choices":[{"message":{"role":"assistant","content":"the capital of France is Paris"}}]}`,
			wantRunes: len("the capital of France is Paris"),
			wantOK:    true,
		},
		{
			name:     "openai tool call, null content",
			protocol: "openai-completions",
			body:     `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","function":{"name":"x"}}]}}]}`,
			wantTool: true,
			wantOK:   true,
		},
		{
			name:     "anthropic empty text",
			protocol: "anthropic-messages",
			body:     `{"type":"message","content":[{"type":"text","text":""}]}`,
			wantOK:   true,
		},
		{
			name:      "anthropic real answer",
			protocol:  "anthropic-messages",
			body:      `{"type":"message","content":[{"type":"text","text":"hello there"}]}`,
			wantRunes: len("hello there"),
			wantOK:    true,
		},
		{
			name:     "anthropic tool use",
			protocol: "anthropic-messages",
			body:     `{"type":"message","content":[{"type":"tool_use","id":"t1","name":"x","input":{}}]}`,
			wantTool: true,
			wantOK:   true,
		},
		{
			name:      "responses output text",
			protocol:  "openai-responses",
			body:      `{"output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}`,
			wantRunes: len("answer"),
			wantOK:    true,
		},
		{
			name:     "unknown protocol",
			protocol: "gemini",
			body:     `{}`,
			wantOK:   false,
		},
		{
			name:     "unparseable",
			protocol: "openai-completions",
			body:     `not json`,
			wantOK:   false,
		},
		{
			name:     "wrong shape (no choices)",
			protocol: "openai-completions",
			body:     `{"error":{"message":"nope"}}`,
			wantOK:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, tool, ok := ResponseAssistantText(tc.protocol, []byte(tc.body))
			if ok != tc.wantOK || n != tc.wantRunes || tool != tc.wantTool {
				t.Errorf("ResponseAssistantText(%q) = (%d, %v, %v), want (%d, %v, %v)",
					tc.protocol, n, tool, ok, tc.wantRunes, tc.wantTool, tc.wantOK)
			}
		})
	}
}
