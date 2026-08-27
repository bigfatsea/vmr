// Ver 2026-08-23 14:45, by Gemini

package router

import (
	"testing"
)

func TestTelemetry_Basic(t *testing.T) {
	var tel Telemetry

	// Initial zero state
	snap := tel.Snapshot()
	if snap.Requests.Total != 0 || snap.Tokens.Total.In != 0 {
		t.Fatalf("expected initial zero, got %+v", snap)
	}

	// Record requests
	tel.RecordRequest("openai-completions")
	tel.RecordRequest("anthropic-messages")
	tel.RecordRequest("openai-responses")
	tel.RecordOutcome(true, false)
	tel.RecordOutcome(false, true)
	tel.RecordOutcome(false, false)

	// Record tokens
	tel.RecordTokens(1000, 200, 300, 150, 400)

	snap = tel.Snapshot()
	if snap.Requests.Total != 3 {
		t.Errorf("expected total=3, got %d", snap.Requests.Total)
	}
	if snap.Requests.ByProtocol["openai-completions"] != 1 || snap.Requests.ByProtocol["anthropic-messages"] != 1 || snap.Requests.ByProtocol["openai-responses"] != 1 {
		t.Errorf("unexpected by_protocol: %+v", snap.Requests.ByProtocol)
	}
	if snap.Requests.ByStatus["ok"] != 1 || snap.Requests.ByStatus["canceled"] != 1 || snap.Requests.ByStatus["error"] != 1 {
		t.Errorf("unexpected by_status: %+v", snap.Requests.ByStatus)
	}
	if snap.Tokens.Total.In != 1000 || snap.Tokens.Total.CacheWrite != 200 || snap.Tokens.Total.CacheRead != 300 ||
		snap.Tokens.Total.Reasoning != 150 || snap.Tokens.Total.Out != 400 {
		t.Errorf("unexpected tokens: %+v", snap.Tokens.Total)
	}
}

func TestTelemetry_NilSafe(t *testing.T) {
	var tel *Telemetry
	tel.RecordRequest("openai-completions")
	tel.RecordOutcome(true, false)
	tel.RecordTokens(100, 10, 20, 5, 50)
	snap := tel.Snapshot()
	if snap.Requests.Total != 0 {
		t.Errorf("expected zero total for nil telemetry, got %d", snap.Requests.Total)
	}
}

// TestTelemetry_ServeOutcomeCoverage drives Serve through the terminal paths
// that must each record exactly one outcome, so requests.total never outruns
// by_status silently: model-not-found, wrong-protocol hint, uninitialized
// snapshot, and an all-attempts-failed walk. The client-canceled-mid-upstream
// path shares the same RecordOutcome call shape but needs a live upstream to
// cancel against; its integration coverage lives with the cancel tests.
func TestTelemetry_ServeOutcomeCoverage(t *testing.T) {
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k}
  - {name: p2, base_url: {anthropic-messages: https://example.com/v1}, api_key: k}
models:
  coding: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
  claude: {endpoints: [{protocol: anthropic-messages, providers: [p2], models: [m]}]}
`)
	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Unknown model -> one error outcome.
	serveReq(rt, "nope", []byte(`{"model":"nope"}`))
	// Wrong ingress protocol -> one error outcome (used to be missed).
	serveReq(rt, "claude", []byte(`{"model":"claude"}`))
	// Unroutable upstream (port 1, connection refused) -> all-failed -> one error outcome.
	serveReq(rt, "coding", []byte(`{"model":"coding"}`))

	snap := rt.Telemetry.Snapshot()
	if got := snap.Requests.ByStatus["error"]; got != 3 {
		t.Errorf("by_status.error = %d, want 3 (not-found, wrong-protocol, all-failed)", got)
	}
	if got := snap.Requests.ByStatus["ok"]; got != 0 {
		t.Errorf("by_status.ok = %d, want 0", got)
	}

	// Serve before any Install: the defensive 503 must also count its outcome.
	rt2 := New(nil)
	serveReq(rt2, "whatever", []byte(`{"model":"whatever"}`))
	if got := rt2.Telemetry.Snapshot().Requests.ByStatus["error"]; got != 1 {
		t.Errorf("uninitialized router: by_status.error = %d, want 1", got)
	}
}
