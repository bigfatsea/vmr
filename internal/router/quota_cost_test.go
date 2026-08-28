// Ver 2026-08-07, by Opus 5

// Tests for the metric: cost charging path (ChargeResponse's MetricCost
// branch/componentCost, baseAmount's MetricCost case, QuotaStatus's
// cost-denominated EstimatedPct) — see quota_multiplier_test.go for the
// token_weights/model_multipliers tests these sit alongside.
package router

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/pricing"
	"vmr/internal/quota"
	"vmr/internal/respnorm"
)

func f(v float64) *float64 { return &v }

func costLimit(amount float64) core.Limit {
	return core.Limit{
		Metric: core.MetricCost, EveryN: 1, EveryUnit: "mo", EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: amount,
	}
}

// fullRate builds a core.PricingSpec whose Base is fully priced — attached
// to a hand-built core.Endpoint.PricingRate in these unit tests (bypassing
// config.validate()'s resolution pipeline, which is exercised end-to-end by
// TestServe_EndToEnd_MetricCost_ChargesRealCost below).
func fullRate() *core.PricingSpec {
	return &core.PricingSpec{
		Base:     core.Rate{InFresh: f(1.0), CacheRead: f(0.1), CacheWrite: f(1.25), Out: f(4.0)},
		Currency: "USD",
	}
}

// --- componentCost: the base(cost) formula itself ---

func TestComponentCost_AllComponentsPriced(t *testing.T) {
	d := quota.Counters{Fresh: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000, Out: 1_000_000}
	rate := pricing.Rate{InFresh: f(1.0), CacheRead: f(0.1), CacheWrite: f(1.25), Out: f(4.0)}
	got := componentCost(d, rate)
	want := 1.0 + 0.1 + 1.25 + 4.0
	if got != want {
		t.Fatalf("componentCost = %v, want %v", got, want)
	}
}

func TestComponentCost_MissingComponent_TreatsAsZero(t *testing.T) {
	d := quota.Counters{Fresh: 1_000_000, CacheRead: 1_000_000}
	rate := pricing.Rate{InFresh: f(2.0)} // CacheRead deliberately nil
	got := componentCost(d, rate)
	if got != 2.0 {
		t.Fatalf("componentCost = %v, want 2.0 (missing cache_read price contributes 0, defensive floor)", got)
	}
}

// --- ChargeResponse (metric: cost): exact (sniffed) usage ---

func TestChargeCost_SniffedUsage_ComputesExactCost(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := costLimit(1000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}, PricingRate: fullRate()}
	creq := &core.CanonicalRequest{}

	body := `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1000000,"completion_tokens":1000000,"prompt_tokens_details":{"cached_tokens":500000}}}`
	rs := respnorm.Wrap(strings.NewReader(body), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai-completions", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)

	used, estTokens := rt.Quota.Used("p1", "cost/1mo", quota.PeriodStart(l, chargeNow))
	// fresh = 1,000,000 - 500,000 = 500,000; cache_read = 500,000; out = 1,000,000.
	wantCost := (500_000.0/1_000_000)*1.0 + (500_000.0/1_000_000)*0.1 + (1_000_000.0/1_000_000)*4.0
	if used.Cost != wantCost {
		t.Fatalf("Counters.Cost = %v, want %v", used.Cost, wantCost)
	}
	if estTokens != 0 {
		t.Fatalf("estimated tokens = %v, want 0 (usage was sniffed, exact)", estTokens)
	}
	if estCost := rt.Quota.EstimatedCostFor("p1", "cost/1mo", quota.PeriodStart(l, chargeNow)); estCost != 0 {
		t.Fatalf("EstimatedCost = %v, want 0 (exact charge)", estCost)
	}
}

// TestChargeCost_TimeInvariance_HistoricalCostSurvivesLaterPriceChange pins
// docs/VirtualModelRouter_Design_v4_Quota.md's "9.2 运行态" argument
// directly: a promotional/time-scoped rate active at charge time produces a $ amount
// that must NOT change retroactively just because the account's pricing
// (an override, a promo window closing, a reconfigured discount) changes
// afterward — Counters.Cost is computed and frozen once, at charge time,
// never re-derived from raw tokens on a later read.
func TestChargeCost_TimeInvariance_HistoricalCostSurvivesLaterPriceChange(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := costLimit(1000)

	// "Now": a 50%-off promo is active on this endpoint's PricingRate.
	promoRate := &core.PricingSpec{Base: core.Rate{InFresh: f(2.0), CacheRead: f(0.2), CacheWrite: f(2.0), Out: f(8.0)}}
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}, PricingRate: promoRate}
	creq := &core.CanonicalRequest{}

	body := `{"usage":{"prompt_tokens":1000000,"completion_tokens":1000000}}`
	rs := respnorm.Wrap(strings.NewReader(body), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai-completions", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	rt.chargeQuota(ep, rs, creq, chargeNow)

	usedAtChargeTime, _ := rt.Quota.Used("p1", "cost/1mo", quota.PeriodStart(l, chargeNow))
	if usedAtChargeTime.Cost <= 0 {
		t.Fatalf("Counters.Cost = %v, want > 0 after a real charge", usedAtChargeTime.Cost)
	}

	// The promo ends: the account's pricing.overrides changes (e.g. the
	// operator edits config.yaml and hot-reloads). This is modeled here as
	// simply mutating ep.PricingRate — chargeQuota already ran, and
	// nothing about the STORED historical value should move.
	ep.PricingRate = &core.PricingSpec{Base: core.Rate{InFresh: f(4.0), CacheRead: f(0.4), CacheWrite: f(4.0), Out: f(16.0)}} // full price, no promo

	usedAfterPriceChange, _ := rt.Quota.Used("p1", "cost/1mo", quota.PeriodStart(l, chargeNow))
	if usedAfterPriceChange.Cost != usedAtChargeTime.Cost {
		t.Fatalf("stored Cost changed after the account's PricingRate changed: was %v, now %v — a historical charge must never be re-priced", usedAtChargeTime.Cost, usedAfterPriceChange.Cost)
	}
}

// --- ChargeResponse (metric: cost): degraded (estimated) usage tracks EstimatedCost ---

func TestChargeCost_DegradedEstimate_TracksEstimatedCost(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := costLimit(1000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}, PricingRate: fullRate()}
	creq := &core.CanonicalRequest{Facts: core.RequestFacts{EstimatedTokens: 1_000_000}}

	body := `{"choices":[{"message":{"content":"no usage field here"}}]}`
	rs := respnorm.Wrap(strings.NewReader(body), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai-completions", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)

	used, estTokens := rt.Quota.Used("p1", "cost/1mo", quota.PeriodStart(l, chargeNow))
	if estTokens != 0 {
		// bucket.Estimated is a requests/tokens-only accumulator; a cost
		// Limit's degraded-estimate signal is money, tracked via
		// EstimatedCostFor. The cost branch must not feed a token figure
		// into Charge's `estimated` param (B6).
		t.Fatalf("bucket.Estimated = %v, want 0 for a metric: cost charge", estTokens)
	}
	if used.Cost <= 0 {
		t.Fatalf("Counters.Cost = %v, want > 0 (degraded estimate still charges a nonzero cost)", used.Cost)
	}
	estCost := rt.Quota.EstimatedCostFor("p1", "cost/1mo", quota.PeriodStart(l, chargeNow))
	if estCost != used.Cost {
		t.Fatalf("EstimatedCost = %v, want it to equal the full charged Cost (%v) — the whole charge was degraded", estCost, used.Cost)
	}
}

// --- ChargeResponse's MetricCost branch never applies model_multipliers (config validation forbids configuring both, but pin the mechanism too) ---

func TestChargeCost_DoesNotConsultModelMultipliers(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := costLimit(1000)
	spec := &core.QuotaSpec{Limits: []core.Limit{l}} // no ModelMultipliers configured — the only config.validate()-legal shape for a cost account
	ep := &core.Endpoint{Provider: "p1", Model: "any-model", Quota: spec, PricingRate: fullRate()}
	creq := &core.CanonicalRequest{}
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai-completions", Opaque: false})

	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, _ := rt.Quota.Used("p1", "cost/1mo", quota.PeriodStart(l, chargeNow))
	if used.Cost != 0 {
		t.Fatalf("Cost = %v, want 0 (empty body, no tokens sniffed or estimated)", used.Cost)
	}
}

// --- baseAmount: metric: cost returns Counters.Cost directly ---

func TestBaseAmount_MetricCost_ReturnsStoredCost(t *testing.T) {
	l := costLimit(1000)
	c := quota.Counters{Cost: 42.5, Fresh: 999999} // Fresh present but irrelevant to a cost Limit
	got := quota.BaseAmount(l, c)
	if got != 42.5 {
		t.Fatalf("baseAmount = %v, want 42.5 (Counters.Cost directly, not re-derived from Fresh)", got)
	}
}

// --- QuotaStatus: EstimatedPct for a cost account uses EstimatedCostFor, not the token-estimate counter ---

func TestQuotaStatus_MetricCost_EstimatedPctIsCostDenominated(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
pricing:
  currency: USD
providers:
  - name: anthropic
    base_url: {openai-completions: https://example.com}
    api_key: k1
    quota:
      limits: [{metric: cost, every: 1mo, since: 2026-01-01, amount: 100}]
models:
  m1:
    endpoints: [{protocol: openai-completions, providers: [anthropic], models: [claude-3-7-sonnet-20250219]}]
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)
	ep := snap.Models["openai-completions"]["m1"].Endpoints[0]
	if ep.PricingRate == nil {
		t.Fatal("PricingRate not resolved for a standard-table-covered model — config wiring broken")
	}
	l := ep.Quota.Limits[0]
	ps := quota.PeriodStart(l, time.Now())

	// 10 exact + 10 estimated cost = 20 total Cost, half of it estimated.
	rt.Quota.Charge("anthropic", "cost/1mo", ps, quota.Counters{Cost: 10}, 0)
	rt.Quota.Charge("anthropic", "cost/1mo", ps, quota.Counters{Cost: 10}, 0)
	rt.Quota.AddEstimatedCost("anthropic", "cost/1mo", ps, 10)

	st := rt.QuotaStatus()
	if len(st) != 1 {
		t.Fatalf("got %d entries, want 1", len(st))
	}
	if st[0].Used != 20 {
		t.Fatalf("Used = %v, want 20", st[0].Used)
	}
	if st[0].EstimatedPct < 49 || st[0].EstimatedPct > 51 {
		t.Fatalf("EstimatedPct = %v, want ~50 (10 of 20 Cost was estimated)", st[0].EstimatedPct)
	}
}

// --- end-to-end: a real request through Serve charges Counters.Cost ---

func TestServe_EndToEnd_MetricCost_ChargesRealCost(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok","model":"claude-3-7-sonnet-20250219","usage":{"prompt_tokens":1000000,"completion_tokens":1000000}}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
pricing:
  currency: USD
providers:
  - name: anthropic
    base_url: {openai-completions: `+u.srv.URL+`}
    api_key: k1
    quota:
      limits: [{metric: cost, every: 1mo, since: 2026-01-01, amount: 1000}]
models:
  vm:
    endpoints: [{protocol: openai-completions, providers: [anthropic], models: [claude-3-7-sonnet-20250219]}]
`)
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}

	spec := rt.Snapshot().Models["openai-completions"]["vm"].Endpoints[0].Quota
	ps := quota.PeriodStart(spec.Limits[0], time.Now())
	used, _ := rt.Quota.Used("anthropic", "cost/1mo", ps)
	if used.Cost <= 0 {
		t.Fatalf("Counters.Cost = %v, want > 0 after a real charged request", used.Cost)
	}
	// 1,000,000 fresh tokens @ in_fresh=3/1M + 1,000,000 out tokens @ out=15/1M = 3 + 15 = 18.
	want := 3.0 + 15.0
	if used.Cost != want {
		t.Fatalf("Counters.Cost = %v, want %v (claude-3-7-sonnet-20250219's standard rate)", used.Cost, want)
	}
}
