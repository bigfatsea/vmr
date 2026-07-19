// Ver 2026-07-19, by Claude

package adapter

import "testing"

func TestResolveURL(t *testing.T) {
	cases := []struct {
		name, baseURL, suffix, want string
	}{
		// OpenAI suffix: /v1/chat/completions
		{"no /v1 → appended", "https://api.example.com", "/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"has /v1 → no dup", "https://api.example.com/v1", "/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"trailing slash trimmed", "https://api.example.com/v1/", "/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"deep path no /v1", "https://openrouter.ai/api", "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"deep path has /v1", "https://openrouter.ai/api/v1", "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"user wrote full path → full overlap", "https://a.co/v1/chat/completions", "/v1/chat/completions", "https://a.co/v1/chat/completions"},
		{"local http no /v1", "http://127.0.0.1:9900", "/v1/chat/completions", "http://127.0.0.1:9900/v1/chat/completions"},
		{"local http with path", "http://127.0.0.1:9900/fail1", "/v1/chat/completions", "http://127.0.0.1:9900/fail1/v1/chat/completions"},

		// Anthropic suffix: /v1/messages
		{"anthropic no /v1", "https://api.example.com/anthropic", "/v1/messages", "https://api.example.com/anthropic/v1/messages"},
		{"anthropic has /v1", "https://api.example.com/anthropic/v1", "/v1/messages", "https://api.example.com/anthropic/v1/messages"},
		{"anthropic trailing slash", "https://api.example.com/anthropic/v1/", "/v1/messages", "https://api.example.com/anthropic/v1/messages"},
		{"anthropic full path overlap", "https://a.co/anthropic/v1/messages", "/v1/messages", "https://a.co/anthropic/v1/messages"},

		// Edge cases
		{"bare host", "https://api.example.com", "/v1/messages", "https://api.example.com/v1/messages"},
		{"no overlap partial", "https://api.example.com/v11", "/v1/chat/completions", "https://api.example.com/v11/v1/chat/completions"},
		{"dashscope path", "https://dashscope.aliyuncs.com/compatible-mode/v1", "/v1/chat/completions", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveURL(c.baseURL, c.suffix); got != c.want {
				t.Errorf("ResolveURL(%q, %q) = %q, want %q", c.baseURL, c.suffix, got, c.want)
			}
		})
	}
}
