// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import "testing"

func TestExtractUsage_OpenAIJSON(t *testing.T) {
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

func TestExtractUsage_SSEStream(t *testing.T) {
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
	if _, ok := ExtractUsage(map[string]any{"foo": "bar"}); ok {
		t.Error("expected ok=false when no usage present")
	}
}

func TestExtractFinish(t *testing.T) {
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
