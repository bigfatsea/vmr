// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import "testing"

func TestExtractUsage_OpenAIJSON(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(1000),
			"completion_tokens": float64(200),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(800),
			},
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 1000 || u.Out != 200 || u.CacheRead != 800 {
		t.Errorf("usage = %+v", u)
	}
}

func TestExtractUsage_AnthropicJSON(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":                float64(500),
			"output_tokens":               float64(50),
			"cache_read_input_tokens":     float64(300),
			"cache_creation_input_tokens": float64(20),
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Anthropic: In excludes cache by default, so total = input_tokens + cacheRead + cacheWrite.
	if u.In != 500+300+20 || u.Out != 50 || u.CacheRead != 300 || u.CacheWrite != 20 {
		t.Errorf("usage = %+v", u)
	}
}

// TestExtractUsage_ResponsesJSON proves openai-responses' usage object —
// Anthropic's field names (input_tokens/output_tokens) but Chat Completions'
// "cached tokens already included in the total" semantics — is read
// correctly rather than falling into either sibling protocol's rule
// verbatim: input_tokens_details.cached_tokens is the tell that selects the
// no-double-add path (see usageFromObj's doc comment).
func TestExtractUsage_ResponsesJSON(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":  float64(1000),
			"output_tokens": float64(200),
			"input_tokens_details": map[string]any{
				"cached_tokens": float64(800),
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": float64(50),
			},
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 1000 || u.Out != 200 || u.CacheRead != 800 || u.Reasoning != 50 {
		t.Errorf("usage = %+v, want In=1000 (not 1000+800 — already inclusive like Chat Completions)", u)
	}
}

// TestExtractUsage_ResponsesJSONNoCacheDetails covers a Responses usage
// object with no input_tokens_details at all (the shape my mockupstream
// loadtest scenario and a plain non-caching upstream both send) — must
// still take the "already inclusive" branch, not silently fall through to
// Anthropic's additive one just because input_tokens_details is absent.
func TestExtractUsage_ResponsesJSONNoCacheDetails(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":  float64(5),
			"output_tokens": float64(1),
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 5 || u.Out != 1 {
		t.Errorf("usage = %+v, want In=5 Out=1", u)
	}
}

func TestExtractUsage_SSEStream(t *testing.T) {
	t.Parallel()
	raw := sseToolCall("exec")
	u, ok := ExtractUsage(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 100 || u.Out != 10 || u.CacheRead != 80 || u.Reasoning != 4 {
		t.Errorf("usage = %+v", u)
	}
}

func TestExtractUsage_NoUsage(t *testing.T) {
	t.Parallel()
	if _, ok := ExtractUsage(map[string]any{"foo": "bar"}); ok {
		t.Error("expected ok=false when no usage present")
	}
}

func TestExtractFinish(t *testing.T) {
	t.Parallel()
	sse := sseText("hi") // finish_reason "stop"
	if got := ExtractFinish(sse); got != "stop" {
		t.Errorf("ExtractFinish(sse) = %q, want stop", got)
	}
	anth := map[string]any{"stop_reason": "end_turn"}
	if got := ExtractFinish(anth); got != "end_turn" {
		t.Errorf("ExtractFinish(anthropic) = %q, want end_turn", got)
	}
	jsonBody := map[string]any{"choices": []any{map[string]any{"finish_reason": "stop"}}}
	if got := ExtractFinish(jsonBody); got != "stop" {
		t.Errorf("ExtractFinish(openai json) = %q, want stop", got)
	}
	if got := ExtractFinish("no finish here"); got != "" {
		t.Errorf("ExtractFinish(no finish) = %q, want empty", got)
	}
}

// TestUsage_Fresh pins the one formula this type's own doc comment states
// ("In - CacheRead - CacheWrite is the fresh portion") — previously
// hand-written at four independent call sites (internal/router/quota.go,
// internal/report/{cost,sticky}.go, internal/story/render_md.go) with no
// single test covering any of them directly; each site's own behavior was
// only ever pinned indirectly through that package's higher-level tests.
func TestUsage_Fresh(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    Usage
		want int64
	}{
		{"no cache", Usage{In: 100, Out: 20}, 100},
		{"cache read only", Usage{In: 100, CacheRead: 40, Out: 20}, 60},
		{"cache read and write", Usage{In: 100, CacheRead: 30, CacheWrite: 10, Out: 20}, 60},
		{"pure cache hit, no fresh", Usage{In: 100, CacheRead: 100, Out: 20}, 0},
		{
			"defensive: reported cache exceeds reported total floors at 0, not negative",
			Usage{In: 100, CacheRead: 80, CacheWrite: 50, Out: 20}, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.u.Fresh(); got != tc.want {
				t.Errorf("Fresh() = %d, want %d", got, tc.want)
			}
		})
	}
}
