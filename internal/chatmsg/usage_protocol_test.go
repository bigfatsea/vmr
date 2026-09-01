// Ver 2026-09-02 07:50, by pi-agent

package chatmsg

import (
	"testing"
)

// --- R75: the protocol, not field presence, selects the In-additivity rule ---

// TestUsageFromObj_ProtocolSelectsInRule pins the core R75 fix: the same
// usage object under different ingress protocols yields different In
// semantics — anthropic adds the cache counters, openai-responses does not
// (its In already includes them). The router's quota metering runs on this
// parse, so a wrong rule here is mis-billed quota, not just a report column.
func TestUsageFromObj_ProtocolSelectsInRule(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"usage": map[string]any{
			"input_tokens":            float64(100),
			"cache_read_input_tokens": float64(50),
		},
	}
	u, _ := ExtractUsageWithProtocol(obj, "anthropic-messages")
	if u.In != 150 || u.CacheRead != 50 || u.Fresh() != 100 {
		t.Errorf("anthropic: usage = %+v, want In=150 (100+50) Fresh=100", u)
	}
	u, _ = ExtractUsageWithProtocol(obj, "openai-responses")
	if u.In != 100 || u.CacheRead != 50 || u.Fresh() != 50 {
		t.Errorf("openai-responses: usage = %+v, want In=100 (already inclusive) Fresh=50", u)
	}
}

// TestUsageFromObj_UnknownProtocolFallsBackToFieldPresence pins the fallback:
// with no protocol, the historical field-presence tell still decides.
func TestUsageFromObj_UnknownProtocolFallsBackToFieldPresence(t *testing.T) {
	t.Parallel()
	responsesShape := map[string]any{
		"usage": map[string]any{
			"input_tokens":         float64(1000),
			"output_tokens":        float64(200),
			"input_tokens_details": map[string]any{"cached_tokens": float64(800)},
		},
	}
	u, _ := ExtractUsageWithProtocol(responsesShape, "")
	if u.In != 1000 {
		t.Errorf("unknown protocol, responses shape: In = %d, want 1000 (details presence selects the inclusive rule)", u.In)
	}
	anthropicShape := map[string]any{
		"usage": map[string]any{
			"input_tokens":                float64(500),
			"cache_read_input_tokens":     float64(300),
			"cache_creation_input_tokens": float64(20),
		},
	}
	u, _ = ExtractUsageWithProtocol(anthropicShape, "")
	if u.In != 820 {
		t.Errorf("unknown protocol, anthropic shape: In = %d, want 820 (500+300+20)", u.In)
	}
}

// TestUsageFromObj_ProtocolBeatsFieldPresence is the aggregated-gateway case
// that motivated R75: an anthropic-protocol response that ALSO carries an
// OpenAI-style details object. The details object must not flip the rule —
// the protocol wins.
func TestUsageFromObj_ProtocolBeatsFieldPresence(t *testing.T) {
	t.Parallel()
	gateway := map[string]any{
		"usage": map[string]any{
			"input_tokens":                float64(100),
			"cache_read_input_tokens":     float64(50),
			"cache_creation_input_tokens": float64(10),
			"input_tokens_details":        map[string]any{"cached_tokens": float64(30)},
		},
	}
	u, _ := ExtractUsageWithProtocol(gateway, "anthropic-messages")
	if u.In != 160 || u.CacheRead != 50 || u.CacheWrite != 10 {
		t.Errorf("gateway mix: usage = %+v, want In=160 (100+50+10) — the anthropic rule must hold despite input_tokens_details", u)
	}
}

// TestMergeUsage_ProtocolThroughSSE checks the protocol survives the SSE
// byte-path (the form respnorm's stream sniffing actually takes).
func TestMergeUsage_ProtocolThroughSSE(t *testing.T) {
	t.Parallel()
	sse := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"cache_read_input_tokens\":50}}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":25}}\n\n"
	u := MergeUsageWithProtocol([]byte(sse), Usage{}, "anthropic-messages")
	if u.In != 150 || u.Out != 25 || u.CacheRead != 50 {
		t.Errorf("SSE anthropic: usage = %+v, want In=150 Out=25 CacheRead=50", u)
	}
	// Same bytes as openai-responses: In must NOT get the cache counters added.
	u = MergeUsageWithProtocol([]byte(sse), Usage{}, "openai-responses")
	if u.In != 100 {
		t.Errorf("SSE openai-responses: In = %d, want 100 (already inclusive)", u.In)
	}
}

// --- R76: per-side object-level max, never per-field stitching ---

// TestMergeUsage_PerSideAnthropicStream guards both failure directions:
// per-FIELD max would stitch In and CacheRead from different objects, while
// whole-OBJECT max would drop the In side entirely (Anthropic never puts
// both sides in one event).
func TestMergeUsage_PerSideAnthropicStream(t *testing.T) {
	t.Parallel()
	sse := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1005,\"cache_read_input_tokens\":1000,\"output_tokens\":1}}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":25}}\n\n"
	u := MergeUsageWithProtocol([]byte(sse), Usage{}, "anthropic-messages")
	if u.In != 2005 || u.CacheRead != 1000 || u.Out != 25 {
		t.Errorf("usage = %+v, want In=2005 (1005+1000) CacheRead=1000 Out=25 — In side from message_start, Out side from message_delta", u)
	}
}

// TestMergeUsage_InSideMovesAsGroup pins the group constraint: of two In-side
// objects, the one with the larger In must contribute ALL THREE In-side
// fields together — mixing In from one with CacheRead from the other would
// fabricate a Fresh() no upstream reported. Fixture (the R76 dossier's
// shape, in anthropic totals): A reports total In=1005 (5 fresh + 1000
// cached), B reports total In=1200 (no cache fields). B wins the In side as
// a group; the old per-field max would have stitched In=1200 with
// CacheRead=1000, inventing Fresh=200.
func TestMergeUsage_InSideMovesAsGroup(t *testing.T) {
	t.Parallel()
	acc := Usage{}
	acc = mergeUsage(map[string]any{"usage": map[string]any{
		"input_tokens":            float64(5),
		"cache_read_input_tokens": float64(1000),
	}}, acc, "anthropic-messages")
	acc = mergeUsage(map[string]any{"usage": map[string]any{
		"input_tokens": float64(1200),
	}}, acc, "anthropic-messages")
	if acc.In != 1200 || acc.CacheRead != 0 || acc.CacheWrite != 0 {
		t.Errorf("usage = %+v, want In=1200 CacheRead=0 CacheWrite=0 — the winning object's In-side group, not a per-field stitch", acc)
	}
	if acc.Fresh() != 1200 {
		t.Errorf("Fresh() = %d, want 1200", acc.Fresh())
	}
}

// TestMergeUsage_OutSideMovesAsGroup is the Out-side twin: Out and Reasoning
// come from the same winning object.
func TestMergeUsage_OutSideMovesAsGroup(t *testing.T) {
	t.Parallel()
	acc := mergeUsage(map[string]any{"usage": map[string]any{
		"output_tokens":             float64(10),
		"completion_tokens_details": map[string]any{"reasoning_tokens": float64(8)},
	}}, Usage{}, "")
	acc = mergeUsage(map[string]any{"usage": map[string]any{
		"output_tokens": float64(50),
	}}, acc, "")
	if acc.Out != 50 || acc.Reasoning != 0 {
		t.Errorf("usage = %+v, want Out=50 Reasoning=0 — Out-side group from the winning object", acc)
	}
}
