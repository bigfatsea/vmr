// Ver 2026-09-02 07:35, by pi-agent

package respnorm

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestUsageSides_TruncatedAnthropicStream pins R46: a stream truncated after
// Anthropic's message_start carries the input side of the usage ledger but
// only the ~1 placeholder output count. UsageSides must report (true, false)
// — billing that output as exact would write out≈1 into the quota ledger
// with estimated=0, poisoning estimated_pct, the operator's only trust
// signal. The fixture is the same one the absolute magnitude baseline
// (baseline_test.go) uses.
func TestUsageSides_TruncatedAnthropicStream(t *testing.T) {
	raw, err := os.ReadFile("testdata/anthropic_truncated_stream.sse")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rs := Wrap(strings.NewReader(string(raw)), Options{
		ClientModel: "agent", UpstreamModel: "claude-x", IsSSE: true, Protocol: "anthropic-messages",
	})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	u, ok := rs.Usage()
	if !ok || u.In != 1200+300 || u.CacheRead != 300 {
		t.Errorf("Usage = %+v ok = %v, want In=1500 CacheRead=300 ok=true (input side must stay exact)", u, ok)
	}
	inSeen, outSeen := rs.UsageSides()
	if !inSeen || outSeen {
		t.Errorf("UsageSides = (%v, %v), want (true, false) — a truncated-after-message_start stream has no real output usage", inSeen, outSeen)
	}
}

// TestUsageSides_CompleteAnthropicStream is the regression half: a stream
// that reaches message_delta has BOTH sides, so usage must bill exact.
func TestUsageSides_CompleteAnthropicStream(t *testing.T) {
	sse := `data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":100,"cache_read_input_tokens":20,"output_tokens":1}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":25}}

data: {"type":"message_stop"}

`
	rs := Wrap(strings.NewReader(sse), Options{
		ClientModel: "agent", UpstreamModel: "claude-x", IsSSE: true, Protocol: "anthropic-messages",
	})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	inSeen, outSeen := rs.UsageSides()
	if !inSeen || !outSeen {
		t.Errorf("UsageSides = (%v, %v), want (true, true) for a complete two-stage stream", inSeen, outSeen)
	}
	u, ok := rs.Usage()
	if !ok || u.In != 120 || u.Out != 25 || u.CacheRead != 20 {
		t.Errorf("Usage = %+v ok = %v, want In=120 Out=25 CacheRead=20", u, ok)
	}
}

// TestUsageSides_OpenAIUsageChunk: the OpenAI family carries both sides in
// one final usage chunk — classified from the parsed values.
func TestUsageSides_OpenAIUsageChunk(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n" +
		"data: [DONE]\n\n"
	rs := Wrap(strings.NewReader(sse), Options{
		ClientModel: "agent", UpstreamModel: "gpt-x", IsSSE: true, Protocol: "openai-completions",
	})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if inSeen, outSeen := rs.UsageSides(); !inSeen || !outSeen {
		t.Errorf("UsageSides = (%v, %v), want (true, true)", inSeen, outSeen)
	}
}

// TestUsageSides_NoUsage: nothing parsed, nothing seen.
func TestUsageSides_NoUsage(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"plain reply\"}}]}\n\ndata: [DONE]\n\n"
	rs := Wrap(strings.NewReader(sse), Options{
		ClientModel: "agent", UpstreamModel: "gpt-x", IsSSE: true, Protocol: "openai-completions",
	})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if inSeen, outSeen := rs.UsageSides(); inSeen || outSeen {
		t.Errorf("UsageSides = (%v, %v), want (false, false)", inSeen, outSeen)
	}
	if _, ok := rs.Usage(); ok {
		t.Error("Usage() ok = true, want false for a usage-less stream")
	}
}
