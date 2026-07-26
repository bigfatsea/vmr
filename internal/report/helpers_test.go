// Ver 2026-07-25, report

// Tests for small shared helpers that used to live in report.go/usage.go's
// test file before the old aggregate-report code (report.go/markdown.go)
// was removed; attemptErrorClass now lives in render.go, ExtractUsage in
// usage.go — both still load-bearing via session.go/detail.go.
package report

import (
	"testing"

	"vmr/internal/audit"
)

// TestAttemptErrorClassFallback covers backward compatibility with audit
// logs that lack Attempt.ErrorClass: the class must still be recoverable
// from the free-text Error field alone.
func TestAttemptErrorClassFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    audit.Attempt
		want string
	}{
		{"new log: ErrorClass wins even if Error looks parseable",
			audit.Attempt{Error: "network: dial tcp refused", ErrorClass: "network"}, "network"},
		{"old log: HTTP-classified error has no colon, used verbatim",
			audit.Attempt{Error: "rate_limit"}, "rate_limit"},
		{"old log: non-HTTP failure uses the class:detail prefix",
			audit.Attempt{Error: "network: dial tcp: connection refused"}, "network"},
		{"old log: canceled has no colon at all, used verbatim",
			audit.Attempt{Error: "canceled by client"}, "canceled by client"},
		{"old log: truncated uses the prefix",
			audit.Attempt{Error: "truncated: stream idle timeout"}, "truncated"},
		{"success attempt: no error, no class", audit.Attempt{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := attemptErrorClass(tc.a); got != tc.want {
				t.Errorf("attemptErrorClass(%+v) = %q, want %q", tc.a, got, tc.want)
			}
		})
	}
}

func TestMs(t *testing.T) {
	for _, tc := range []struct {
		v    int64
		want string
	}{
		{0, "0ms"},
		{999, "999ms"},
		{1000, "1.0s"}, // exactly 1000ms must already switch to seconds
		{1001, "1.0s"},
		{2500, "2.5s"},
	} {
		if got := ms(tc.v); got != tc.want {
			t.Errorf("ms(%d) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestExtractUsage(t *testing.T) {
	cases := []struct {
		name                           string
		body                           any
		in, out, cacheRead, cacheWrite int64
		ok                             bool
	}{
		{"openai json", map[string]any{"usage": map[string]any{"prompt_tokens": 10.0, "completion_tokens": 20.0}}, 10, 20, 0, 0, true},
		{"anthropic json", map[string]any{"usage": map[string]any{"input_tokens": 5.0, "output_tokens": 7.0}}, 5, 7, 0, 0, true},
		{"no usage", map[string]any{"choices": []any{}}, 0, 0, 0, 0, false},
		{"anthropic sse", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":41,\"output_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\ndata: [DONE]\n", 41, 9, 0, 0, true},
		{"openai sse with usage chunk", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":3}}\n\ndata: [DONE]\n", 6, 3, 0, 0, true},
		{"sse without usage", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n", 0, 0, 0, 0, false},
		// Anthropic: input_tokens excludes cache counters — total In sums all three.
		{"anthropic cache json", map[string]any{"usage": map[string]any{
			"input_tokens": 5.0, "output_tokens": 7.0,
			"cache_read_input_tokens": 100.0, "cache_creation_input_tokens": 20.0,
		}}, 125, 7, 100, 20, true},
		{"anthropic cache sse", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":1,\"cache_read_input_tokens\":100,\"cache_creation_input_tokens\":20}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n", 125, 9, 100, 20, true},
		// OpenAI: prompt_tokens_details.cached_tokens is a subset already inside prompt_tokens.
		{"openai cache json", map[string]any{"usage": map[string]any{
			"prompt_tokens": 110.0, "completion_tokens": 20.0,
			"prompt_tokens_details": map[string]any{"cached_tokens": 90.0},
		}}, 110, 20, 90, 0, true},
		// DeepSeek: prompt_cache_hit_tokens is likewise a subset of prompt_tokens.
		{"deepseek cache json", map[string]any{"usage": map[string]any{
			"prompt_tokens": 110.0, "completion_tokens": 20.0,
			"prompt_cache_hit_tokens": 80.0, "prompt_cache_miss_tokens": 30.0,
		}}, 110, 20, 80, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, ok := ExtractUsage(c.body)
			if u.In != c.in || u.Out != c.out || u.CacheRead != c.cacheRead || u.CacheWrite != c.cacheWrite || ok != c.ok {
				t.Errorf("got %+v ok=%v, want in=%d out=%d cacheRead=%d cacheWrite=%d ok=%v",
					u, ok, c.in, c.out, c.cacheRead, c.cacheWrite, c.ok)
			}
		})
	}
}
