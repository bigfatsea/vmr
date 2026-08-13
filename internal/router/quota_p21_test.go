// Ver 2026-08-07, by Opus 5

// Tests for P2.1 (see docs/VirtualModelRouter_Design_v4_Quota.md's
// "P2 — 计量准确" batch description): account-level model_multipliers
// (applied at charge time) and token_weights
// (applied at read time via baseAmount). See quota_charge_test.go/
// quota_status_test.go for the P1 tests these extend.
package router

import (
	"bytes"
	"io"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/quota"
)

// --- model_multipliers: charge-time application ---

func TestChargeQuota_ModelMultiplier_ExactMatch(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := requestsLimit(1000)
	spec := &core.QuotaSpec{Limits: []core.Limit{l}, ModelMultipliers: map[string]float64{"heavy-model": 9}}
	ep := &core.Endpoint{Provider: "p1", Model: "heavy-model", Quota: spec}
	rbody := newRespStream(bytes.NewReader(nil), "m", "m", false, "openai", false)
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, _ := rt.Quota.Used("p1", "requests/1mo", quota.PeriodStart(l, chargeNow))
	if used.Requests != 9 {
		t.Fatalf("Requests = %d, want 9 (1 call x 9x multiplier)", used.Requests)
	}
}

func TestChargeQuota_ModelMultiplier_WildcardFallback(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := requestsLimit(1000)
	spec := &core.QuotaSpec{Limits: []core.Limit{l}, ModelMultipliers: map[string]float64{"*": 3, "named-model": 9}}
	ep := &core.Endpoint{Provider: "p1", Model: "unnamed-model", Quota: spec}
	rbody := newRespStream(bytes.NewReader(nil), "m", "m", false, "openai", false)
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, _ := rt.Quota.Used("p1", "requests/1mo", quota.PeriodStart(l, chargeNow))
	if used.Requests != 3 {
		t.Fatalf("Requests = %d, want 3 (wildcard multiplier, not the named-model entry)", used.Requests)
	}
}

func TestChargeQuota_ModelMultiplier_NoMatchNoWildcard_DefaultsToOne(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := requestsLimit(1000)
	spec := &core.QuotaSpec{Limits: []core.Limit{l}, ModelMultipliers: map[string]float64{"named-model": 9}}
	ep := &core.Endpoint{Provider: "p1", Model: "other-model", Quota: spec}
	rbody := newRespStream(bytes.NewReader(nil), "m", "m", false, "openai", false)
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, _ := rt.Quota.Used("p1", "requests/1mo", quota.PeriodStart(l, chargeNow))
	if used.Requests != 1 {
		t.Fatalf("Requests = %d, want 1 (no match, no wildcard -> 1.0, unscaled)", used.Requests)
	}
}

func TestChargeQuota_ModelMultiplier_NotConfigured_NoOp(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := requestsLimit(1000)
	// ModelMultipliers left nil entirely (the common case: no account
	// configures it at all) — must behave identically to P1.
	spec := &core.QuotaSpec{Limits: []core.Limit{l}}
	ep := &core.Endpoint{Provider: "p1", Model: "any-model", Quota: spec}
	rbody := newRespStream(bytes.NewReader(nil), "m", "m", false, "openai", false)
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, _ := rt.Quota.Used("p1", "requests/1mo", quota.PeriodStart(l, chargeNow))
	if used.Requests != 1 {
		t.Fatalf("Requests = %d, want 1 (unconfigured model_multipliers must be a no-op)", used.Requests)
	}
}

// TestChargeQuota_ModelMultiplier_NonIntegerRoundsUp pins the rounding
// direction the dev plan's S2 requires: ceil, never floor/round-to-nearest —
// under-counting a heavily-multiplied model's consumption is the dangerous
// direction (see applyModelMultiplier's doc comment).
func TestChargeQuota_ModelMultiplier_NonIntegerRoundsUp(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	spec := &core.QuotaSpec{
		Limits:           []core.Limit{l},
		ModelMultipliers: map[string]float64{"m": 1.5},
		TokenWeights:     core.TokenWeights{InFresh: 1, CacheRead: 1, CacheWrite: 1, Out: 1},
	}
	ep := &core.Endpoint{Provider: "p1", Model: "m", Quota: spec}
	creq := &core.CanonicalRequest{Facts: core.RequestFacts{EstimatedTokens: 10}}

	body := `{"choices":[{"message":{"content":"hi"}}]}` // no usage field -> degraded estimate path
	rbody := newRespStream(bytes.NewReader([]byte(body)), "m", "m", false, "openai", false)
	if _, err := io.Copy(io.Discard, rbody); err != nil {
		t.Fatalf("drain: %v", err)
	}
	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, _ := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))
	// Fresh: 10 tokens x 1.5 = 15 exactly, ceil(15)=15.
	if used.Fresh != 15 {
		t.Fatalf("Fresh = %d, want 15 (ceil(10 x 1.5))", used.Fresh)
	}
}

// TestChargeQuota_ModelMultiplier_IndependentProviders proves the multiplier
// is resolved per-endpoint (ep.Quota/ep.Model), never leaking across
// providers that happen to share a registry.
func TestChargeQuota_ModelMultiplier_IndependentProviders(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := requestsLimit(1000)
	specA := &core.QuotaSpec{Limits: []core.Limit{l}, ModelMultipliers: map[string]float64{"*": 5}}
	specB := &core.QuotaSpec{Limits: []core.Limit{l}} // no multiplier configured
	epA := &core.Endpoint{Provider: "plan-a", Model: "m", Quota: specA}
	epB := &core.Endpoint{Provider: "plan-b", Model: "m", Quota: specB}
	rbody := newRespStream(bytes.NewReader(nil), "m", "m", false, "openai", false)
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(epA, rbody, creq, chargeNow)
	rt.chargeQuota(epB, rbody, creq, chargeNow)

	usedA, _ := rt.Quota.Used("plan-a", "requests/1mo", quota.PeriodStart(l, chargeNow))
	usedB, _ := rt.Quota.Used("plan-b", "requests/1mo", quota.PeriodStart(l, chargeNow))
	if usedA.Requests != 5 {
		t.Fatalf("plan-a Requests = %d, want 5", usedA.Requests)
	}
	if usedB.Requests != 1 {
		t.Fatalf("plan-b Requests = %d, want 1 (must not inherit plan-a's multiplier)", usedB.Requests)
	}
}

// --- token_weights: read-time application via baseAmount ---

func TestBaseAmount_TokenWeights_AllDefault_MatchesP1EqualWeightedSum(t *testing.T) {
	spec := &core.QuotaSpec{
		Limits:       []core.Limit{tokensLimit(1_000_000)},
		TokenWeights: core.TokenWeights{InFresh: 1, CacheRead: 1, CacheWrite: 1, Out: 1},
	}
	c := quota.Counters{Fresh: 80, CacheRead: 20, CacheWrite: 5, Out: 50}
	got := quota.BaseAmount(spec, c)
	want := float64(80 + 20 + 5 + 50)
	if got != want {
		t.Fatalf("baseAmount = %v, want %v (equal-weighted sum, P1-identical)", got, want)
	}
}

func TestBaseAmount_TokenWeights_Applied(t *testing.T) {
	spec := &core.QuotaSpec{
		Limits:       []core.Limit{tokensLimit(1_000_000)},
		TokenWeights: core.TokenWeights{InFresh: 1.0, CacheRead: 0.1, CacheWrite: 1.25, Out: 4.0},
	}
	c := quota.Counters{Fresh: 100, CacheRead: 100, CacheWrite: 8, Out: 10}
	got := quota.BaseAmount(spec, c)
	want := 100*1.0 + 100*0.1 + 8*1.25 + 10*4.0 // = 100 + 10 + 10 + 40 = 160
	if got != want {
		t.Fatalf("baseAmount = %v, want %v", got, want)
	}
}

func TestBaseAmount_Requests_UnaffectedByTokenWeights(t *testing.T) {
	// token_weights must never touch a requests-metric Limit — requests
	// counts by definition (base(requests) = 1 per call), and this spec sets
	// deliberately skewed weights to prove they're simply never read.
	spec := &core.QuotaSpec{
		Limits:       []core.Limit{requestsLimit(1000)},
		TokenWeights: core.TokenWeights{InFresh: 99, CacheRead: 99, CacheWrite: 99, Out: 99},
	}
	c := quota.Counters{Requests: 7, Fresh: 1000} // Fresh present but irrelevant to a requests Limit
	got := quota.BaseAmount(spec, c)
	if got != 7 {
		t.Fatalf("baseAmount = %v, want 7 (requests metric ignores TokenWeights entirely)", got)
	}
}

// --- end-to-end: model_multipliers + token_weights together through Serve ---

func TestServe_EndToEnd_ModelMultiplierAndTokenWeights(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok","model":"heavy","usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":50}}}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: `+u.srv.URL+`}
    api_key: k1
    quota:
      token_weights: {cache_read: 0.1}
      model_multipliers: {heavy: 2}
      limits: [{metric: tokens, every: 1mo, since: 2026-01-01, amount: 1000000}]
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [heavy]}
`)
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}

	spec := rt.Snapshot().Models["openai"]["vm"].Endpoints[0].Quota
	ps := quota.PeriodStart(spec.Limits[0], time.Now())
	used, _ := rt.Quota.Used("p1", "tokens/1mo", ps)
	// Raw usage: prompt_tokens=100 (fresh=100-50=50), cache_read=50, out=10.
	// model_multipliers doubles every component before it's stored:
	// Fresh=100, CacheRead=100, Out=20.
	if used.Fresh != 100 || used.CacheRead != 100 || used.Out != 20 {
		t.Fatalf("stored counters = %+v, want Fresh=100 CacheRead=100 Out=20 (2x model_multiplier applied at charge time)", used)
	}
	// Reading it back through baseAmount applies token_weights on top:
	// 100*1 (in_fresh default) + 100*0.1 (cache_read override) + 20*1 (out default) = 130.
	got := quota.BaseAmount(spec, used)
	if got != 130 {
		t.Fatalf("baseAmount = %v, want 130 (charge-time multiplier x read-time token_weights composed correctly)", got)
	}
}

// TestQuotaStatus_MetricTokens_EstimatedPctIgnoresTokenWeights pins the unit
// the estimate share is a ratio OF: `estimated` counts raw tokens charged by
// tokenCharge's degraded path, while Used has token_weights applied. Dividing
// one by the other reported a nonsense share for any account with a non-1.0
// weight — here, a fully-estimated period would have read as 20% estimated.
func TestQuotaStatus_MetricTokens_EstimatedPctIgnoresTokenWeights(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com}
    api_key: k1
    quota:
      token_weights: {out: 5.0}
      limits: [{metric: tokens, every: 1mo, since: 2026-01-01, amount: 1000000}]
models:
  m1:
    endpoints: [{protocol: openai, provider: p1, models: [real-model]}]
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)
	l := snap.Models["openai"]["m1"].Endpoints[0].Quota.Limits[0]
	ps := quota.PeriodStart(l, time.Now())

	// One fully-degraded charge: 100 raw tokens, all of them estimated.
	rt.Quota.Charge("p1", "tokens/1mo", ps, quota.Counters{Out: 100}, 100)

	st := rt.QuotaStatus()
	if len(st) != 1 {
		t.Fatalf("got %d entries, want 1", len(st))
	}
	if st[0].Used != 500 {
		t.Fatalf("Used = %v, want 500 (100 out tokens x weight 5.0)", st[0].Used)
	}
	if st[0].EstimatedPct < 99.9 || st[0].EstimatedPct > 100.1 {
		t.Fatalf("EstimatedPct = %v, want 100 (every token this period came from the degraded estimate)", st[0].EstimatedPct)
	}
}
