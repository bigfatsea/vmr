// Ver 2026-07-22, by Sonnet 5

package adapter

import "testing"

func TestResolveURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, baseURL, suffix, want string
	}{
		// OpenAI suffix: /chat/completions. base_url carries its own version.
		{"v1 base", "https://api.example.com/v1", "/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"trailing slash trimmed", "https://api.example.com/v1/", "/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"deep path with v1", "https://openrouter.ai/api/v1", "/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"non-v1 version marker", "https://ark.example.com/api/coding/v3", "/chat/completions", "https://ark.example.com/api/coding/v3/chat/completions"},
		{"local http with path", "http://127.0.0.1:9900/fail1/v1", "/chat/completions", "http://127.0.0.1:9900/fail1/v1/chat/completions"},
		{"dashscope path", "https://dashscope.aliyuncs.com/compatible-mode/v1", "/chat/completions", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},

		// Anthropic suffix: /messages. base_url carries its own version.
		{"anthropic v1", "https://api.example.com/anthropic/v1", "/messages", "https://api.example.com/anthropic/v1/messages"},
		{"anthropic trailing slash", "https://api.example.com/anthropic/v1/", "/messages", "https://api.example.com/anthropic/v1/messages"},
		{"anthropic non-v1 version marker", "https://ark.example.com/api/coding/v1", "/messages", "https://ark.example.com/api/coding/v1/messages"},

		// Edge case: base_url has no version at all — vmr does not infer
		// one; whatever is in base_url is used verbatim.
		{"bare host, no version", "https://api.example.com", "/chat/completions", "https://api.example.com/chat/completions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveURL(c.baseURL, c.suffix); got != c.want {
				t.Errorf("ResolveURL(%q, %q) = %q, want %q", c.baseURL, c.suffix, got, c.want)
			}
		})
	}
}
