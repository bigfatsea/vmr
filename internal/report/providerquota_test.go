// Ver 2026-08-22, by Sonnet 5
package report

import (
	"testing"
	"time"

	"vmr/internal/core"
)

func TestBuildProviderQuotaRows_Empty(t *testing.T) {
	if got := buildProviderQuotaRows(&Report2{}, nil, time.Now(), time.Time{}, time.Time{}); got != nil {
		t.Fatalf("empty quotas must return nil, got %+v", got)
	}
}

func requestsLimit(amount float64) core.Limit {
	return core.Limit{Metric: core.MetricRequests, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: amount}
}

// tokensLimit resolves TokenWeights to the all-default (1.0) shape
// config.LimitConfig.validate() always produces — core.Limit's own zero
// value is {0,0,0,0}, NOT that default (see core.Limit.TokenWeights' doc
// comment on the trap), so a hand-built Limit that skips this would exercise
// a degenerate always-zero-weighted Limit no real config ever produces.
func tokensLimit(amount float64) core.Limit {
	return core.Limit{Metric: core.MetricTokens, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: amount, TokenWeights: core.NewTokenWeights()}
}

func costLimit(amount float64) core.Limit {
	return core.Limit{Metric: core.MetricCost, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: amount}
}

// oneRef wraps a single core.Limit into the map[string][]ProviderQuotaRef
// shape buildProviderQuotaRows/accumulateQuotaWindow now take (P3: one
// entry per Limit) — nearly every test in this file only ever exercises one
// Limit per provider, so this keeps each fixture down to one line.
func oneRef(provider string, l *core.Limit) map[string][]ProviderQuotaRef {
	return map[string][]ProviderQuotaRef{provider: {{Limit: l}}}
}

func TestBuildProviderQuotaRows_RequestsMetric_RollsUpAndMultiplies(t *testing.T) {
	lim := requestsLimit(1000)
	lim.ModelMultipliers = map[string]float64{"heavy": 5}
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:heavy", Requests: 3, Forwarded: 3},
		{Endpoint: "openai-completions:acct1:light", Requests: 2, Forwarded: 2},
	}}
	quotas := map[string][]ProviderQuotaRef{"acct1": {{Metric: "requests", Every: "1mo", Amount: 1000, Limit: &lim}}}
	rows := buildProviderQuotaRows(rep, quotas, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// heavy: 3 requests * 5x multiplier = 15; light: 2 requests * 1x = 2; total 17.
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 17 {
		t.Fatalf("WindowConsumed = %v, want 17", rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_RequestsMetric_NonIntegerMultiplierExactlyMatchesRouter
// is the precision fix: the router charges PER REQUEST with an exact
// multiplier (no rounding — see quota.Counters' doc comment), so its real
// total for N requests at a non-integer multiplier is exactly N*mult. 19
// requests at 5.5x: 19*5.5=104.5 — not 114 (per-charge ceil(5.5)=6) and not
// 105 (ceil(19*5.5), aggregate-then-ceil). Both are rounding formulas this
// has to stay clear of.
func TestBuildProviderQuotaRows_RequestsMetric_NonIntegerMultiplierExactlyMatchesRouter(t *testing.T) {
	lim := requestsLimit(100000)
	lim.ModelMultipliers = map[string]float64{"deepseek-v4-pro": 5.5}
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:volcengine:deepseek-v4-pro", Requests: 19, Forwarded: 19},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("volcengine", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 104.5 {
		t.Fatalf("WindowConsumed = %v, want 104.5 (19*5.5, matching the router's per-request charge exactly)", derefOrNil(rows[0].WindowConsumed))
	}
}

// TestBuildProviderQuotaRows_RequestsMetric_UsesForwardedNotRequests pins the rule:
// the basis must be the FORWARDED-ATTEMPT count, not the request count. A
// request whose every attempt failed still contributes 1 to EndpointRow.
// Requests (it counts against the last endpoint tried) while the router
// charged nothing for it — using Requests over-counted by mult per fully-
// failed request, which on a 5.5x account is 5.5 phantom units each. Here:
// 20 requests reached the endpoint, only 12 attempts were ever forwarded,
// so the router's real total is 12*5.5=66, not 20*5.5=110.
func TestBuildProviderQuotaRows_RequestsMetric_UsesForwardedNotRequests(t *testing.T) {
	lim := requestsLimit(100000)
	lim.ModelMultipliers = map[string]float64{"m": 5.5}
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m", Requests: 20, Forwarded: 12},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 66 {
		t.Fatalf("WindowConsumed = %v, want 66 (12 forwarded * 5.5); 110 means it regressed to counting Requests", derefOrNil(rows[0].WindowConsumed))
	}
}

// derefOrNil dereferences a *float64 for a Fatalf argument without risking a
// nil-pointer panic inside the failure message itself.
func derefOrNil(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestBuildProviderQuotaRows_TokensMetric_UnsniffedUsageCountsItsEstimate
// pins the B0 fix. Before it, an unparseable-usage request contributed 0 here
// while the ROUTER charged a byte-count estimate for it — and the all-or-
// nothing bail-out that tried to cover for that ("every request unparseable →
// render nil") missed the case that actually matters: a window where only
// SOME requests were unparseable rendered a precise, systematically-low
// number with no signal at all.
//
// Now the estimate is counted (same formula the router charges with, see
// chatmsg.EstimateDegradedTokens) and its share is reported through
// WindowEstimatedPct instead of being papered over with a "-".
func TestBuildProviderQuotaRows_TokensMetric_UnsniffedUsageCountsItsEstimate(t *testing.T) {
	lim := tokensLimit(1_000_000)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m", Requests: 7, Forwarded: 7, TokensKnown: 0,
			TokensInFreshEst: 400, TokensOutEst: 100, TokensEstimated: 7},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 500 {
		t.Fatalf("WindowConsumed = %v, want 500 (the degraded estimate the router also charged); nil means it regressed to the old all-or-nothing bail-out",
			derefOrNil(rows[0].WindowConsumed))
	}
	if rows[0].WindowEstimatedPct != 100 {
		t.Errorf("WindowEstimatedPct = %v, want 100 (every token in this window is an estimate)", rows[0].WindowEstimatedPct)
	}
}

// TestBuildProviderQuotaRows_TokensMetric_MixedUsageIsFlagged is the case the
// old all-or-nothing check silently missed and the whole reason
// WindowEstimatedPct exists: sniffed and estimated tokens in the same window.
// The total must include both, and the row must say how much of it is a guess.
func TestBuildProviderQuotaRows_TokensMetric_MixedUsageIsFlagged(t *testing.T) {
	lim := tokensLimit(1_000_000)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m", Requests: 10, Forwarded: 10, TokensKnown: 6,
			TokensInFresh: 600, TokensOut: 150,
			TokensInFreshEst: 200, TokensOutEst: 50, TokensEstimated: 4},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 1000 {
		t.Fatalf("WindowConsumed = %v, want 1000 (750 sniffed + 250 estimated); 750 means the estimated half was dropped",
			derefOrNil(rows[0].WindowConsumed))
	}
	if rows[0].WindowEstimatedPct != 25 {
		t.Errorf("WindowEstimatedPct = %v, want 25 (250 of 1000 raw tokens are estimated)", rows[0].WindowEstimatedPct)
	}
}

// TestBuildProviderQuotaRows_TokensMetric_FullySniffedIsNotFlagged guards the
// other direction: a window with no degraded records must report a 0
// estimated share, so the "X% est." annotation never appears on an
// authoritative number.
func TestBuildProviderQuotaRows_TokensMetric_FullySniffedIsNotFlagged(t *testing.T) {
	lim := tokensLimit(1_000_000)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m", Requests: 5, Forwarded: 5, TokensKnown: 5, TokensInFresh: 800, TokensOut: 200},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowEstimatedPct != 0 {
		t.Errorf("WindowEstimatedPct = %v, want 0 (nothing in this window was estimated)", rows[0].WindowEstimatedPct)
	}
}

// TestBuildProviderQuotaRows_TokensMetric_NoTrafficRendersRealZero is the
// other side of the fix: an account with no traffic at all this window
// really did consume zero, and must NOT be suppressed to "-".
func TestBuildProviderQuotaRows_TokensMetric_NoTrafficRendersRealZero(t *testing.T) {
	lim := tokensLimit(1_000_000)
	rep := &Report2{} // no endpoints at all
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 0 {
		t.Fatalf("WindowConsumed = %v, want a real 0 (zero traffic is a known zero, not missing data)", rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_TokensMetric_PartialUsageStillSums guards the
// fix's boundary: as long as SOME request had parseable usage, the
// column is a number (this is the routine partial case), not "-".
func TestBuildProviderQuotaRows_TokensMetric_PartialUsageStillSums(t *testing.T) {
	lim := tokensLimit(1_000_000)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", Requests: 5, Forwarded: 5, TokensKnown: 0},
		{Endpoint: "openai-completions:acct1:m2", Requests: 5, Forwarded: 5, TokensKnown: 5, TokensInFresh: 100, TokensOut: 20},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 120 {
		t.Fatalf("WindowConsumed = %v, want 120 (100 fresh + 20 out from the one endpoint with usage)", rows[0].WindowConsumed)
	}
}

func TestBuildProviderQuotaRows_TokensMetric_AppliesWeightsAndMultiplier(t *testing.T) {
	lim := tokensLimit(1_000_000)
	lim.TokenWeights = core.TokenWeights{InFresh: 1, CacheRead: 0.1, CacheWrite: 1, Out: 4}
	lim.ModelMultipliers = map[string]float64{"*": 2}
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", TokensInFresh: 100, TokensInCached: 100, TokensOut: 10},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	// multiplier x2: Fresh=200, CacheRead=200, Out=20.
	// weighted: 200*1 + 200*0.1 + 20*4 = 200+20+80 = 300.
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 300 {
		t.Fatalf("WindowConsumed = %v, want 300", rows[0].WindowConsumed)
	}
}

func TestBuildProviderQuotaRows_CostMetric_SkipsModelMultiplier(t *testing.T) {
	lim := costLimit(100)
	c1, c2 := 1.5, 2.5
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", CostEstimate: &c1},
		{Endpoint: "openai-completions:acct1:m2", CostEstimate: &c2},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 4.0 {
		t.Fatalf("WindowConsumed = %v, want 4.0 (1.5+2.5, unweighted)", rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_CostMetric_NoPricingAnywhereRendersNil is
// traffic existed for this cost account, but not a single endpoint
// had a resolvable price — WindowConsumed must be nil (renders "-"), never
// a fabricated 0 indistinguishable from "genuinely spent nothing."
// Requests > 0 on both rows is load-bearing, not decoration: "-" means
// "this account SERVED traffic that nothing could price". A row with
// Requests == 0 is a different situation entirely (see
// _CostMetric_AllAttemptsFailedRendersRealZero below), and this fixture
// used to leave the field at its zero value — so it was quietly asserting
// the wrong shape's behavior.
func TestBuildProviderQuotaRows_CostMetric_NoPricingAnywhereRendersNil(t *testing.T) {
	lim := costLimit(100)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", Requests: 3, CostEstimate: nil},
		{Endpoint: "openai-completions:acct1:m2", Requests: 2, CostEstimate: nil},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed != nil {
		t.Fatalf("WindowConsumed = %v, want nil (served traffic, no endpoint priced)", *rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_CostMetric_AllAttemptsFailedRendersRealZero
// covers the third shape, between "no traffic at all" and "served but
// unpriced": every attempt against this account failed (upstream 5xx, a
// connection error), so EndpointsAll carries attempt-grade rows with
// Requests == 0 and no cost was ever attributed. The router charged exactly
// $0.00 for such a window (chargeQuota only ever runs from forwardSuccess),
// so "-" would be a false UNKNOWN — the mirror image of the false ZERO the
// nil branch exists to prevent, and out of step with what the requests and
// tokens metrics render for the identical window.
func TestBuildProviderQuotaRows_CostMetric_AllAttemptsFailedRendersRealZero(t *testing.T) {
	lim := costLimit(100)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", Attempts: 4, Failed: 4, Requests: 0, CostEstimate: nil},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 0 {
		t.Fatalf("WindowConsumed = %v, want a real 0 (nothing was ever forwarded, so nothing was charged)", rows[0].WindowConsumed)
	}
}

// A failed endpoint must not drag a sibling that DID serve unpriced traffic
// out of the "-" verdict either: the two conditions are independent, and
// the account-level answer is still "unknown".
func TestBuildProviderQuotaRows_CostMetric_FailedSiblingDoesNotMaskUnpriced(t *testing.T) {
	lim := costLimit(100)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:dead", Attempts: 4, Failed: 4, Requests: 0, CostEstimate: nil},
		{Endpoint: "openai-completions:acct1:unpriced", Attempts: 2, Requests: 2, CostEstimate: nil},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed != nil {
		t.Fatalf("WindowConsumed = %v, want nil (one endpoint served unpriced traffic)", *rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_CostMetric_NoTrafficRendersRealZero is the
// mirror case: no traffic at all this window is a genuine 0, not a missing-
// pricing "-" — the same distinction requests/tokens accounts already make.
func TestBuildProviderQuotaRows_CostMetric_NoTrafficRendersRealZero(t *testing.T) {
	lim := costLimit(100)
	rows := buildProviderQuotaRows(&Report2{}, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 0 {
		t.Fatalf("WindowConsumed = %v, want a real 0 (no traffic)", rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_CostMetric_PartiallyPricedSumsWhatItHas locks
// in the deliberate scope boundary: partial pricing (some endpoints priced,
// some not) still sums what's known rather than going nil. The nil case
// targets "zero endpoints priced" only; a partial undercount is already
// covered by the existing WindowFootnote's general drift-sources disclaimer.
func TestBuildProviderQuotaRows_CostMetric_PartiallyPricedSumsWhatItHas(t *testing.T) {
	lim := costLimit(100)
	c1 := 1.5
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:priced", CostEstimate: &c1},
		{Endpoint: "openai-completions:acct1:unpriced", CostEstimate: nil},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 1.5 {
		t.Fatalf("WindowConsumed = %v, want 1.5 (partial sum, not nil)", rows[0].WindowConsumed)
	}
}

// TestBuildProviderQuotaRows_CostMetric_UnsniffedUsageCountsItsEstimate is
// the cost-metric sibling of the tokens-metric fix above: before it, a
// record whose usage was never sniffed contributed a hardcoded 0 to
// CostEstimate (via costFor) — a window entirely made of such records
// rendered a misleadingly precise $0.0000 rather than either a real number
// or "-". Now it prices the same degraded byte-count estimate the router
// charges, and CostEstimateEst carries that whole amount as "estimated".
func TestBuildProviderQuotaRows_CostMetric_UnsniffedUsageCountsItsEstimate(t *testing.T) {
	lim := costLimit(100)
	c1 := 0.024
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m", CostEstimate: &c1, CostEstimateEst: c1},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 0.024 {
		t.Fatalf("WindowConsumed = %v, want 0.024 (the degraded estimate the router also charged); a false zero means it regressed",
			derefOrNil(rows[0].WindowConsumed))
	}
	if rows[0].WindowEstimatedPct != 100 {
		t.Errorf("WindowEstimatedPct = %v, want 100 (every dollar in this window is an estimate)", rows[0].WindowEstimatedPct)
	}
}

// TestBuildProviderQuotaRows_CostMetric_MixedUsageIsFlagged mirrors the
// tokens-metric mixed-window test: sniffed and degraded-estimate cost in the
// same window must both count toward WindowConsumed, and the row must say
// how much of it is a guess.
func TestBuildProviderQuotaRows_CostMetric_MixedUsageIsFlagged(t *testing.T) {
	lim := costLimit(100)
	exact, mixed := 3.0, 1.0
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", CostEstimate: &exact},                       // fully sniffed
		{Endpoint: "openai-completions:acct1:m2", CostEstimate: &mixed, CostEstimateEst: 0.4}, // partly degraded
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 4.0 {
		t.Fatalf("WindowConsumed = %v, want 4.0 (3.0 exact + 1.0 mixed)", derefOrNil(rows[0].WindowConsumed))
	}
	if rows[0].WindowEstimatedPct != 10 {
		t.Errorf("WindowEstimatedPct = %v, want 10 (0.4 of 4.0 total dollars are estimated)", rows[0].WindowEstimatedPct)
	}
}

// TestBuildProviderQuotaRows_CostMetric_FullySniffedIsNotFlagged guards the
// other direction: a window with no degraded records must report a 0
// estimated share, so the "X% est." annotation never appears on an
// authoritative cost figure.
func TestBuildProviderQuotaRows_CostMetric_FullySniffedIsNotFlagged(t *testing.T) {
	lim := costLimit(100)
	c1 := 2.0
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m", CostEstimate: &c1}, // CostEstimateEst left at its zero value
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowEstimatedPct != 0 {
		t.Errorf("WindowEstimatedPct = %v, want 0 (nothing in this window was estimated)", rows[0].WindowEstimatedPct)
	}
}

// TestBuildProviderQuotaRows_WindowNoOverlap_DisjointIntervalsFlagged is
// analyzing three-month-old archived logs (May) against a billing
// period computed for "now" (August) must flag WindowNoOverlap.
func TestBuildProviderQuotaRows_WindowNoOverlap_DisjointIntervalsFlagged(t *testing.T) {
	lim := requestsLimit(1000)
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	windowFrom := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	rows := buildProviderQuotaRows(&Report2{}, oneRef("acct1", &lim), now, windowFrom, windowTo)
	if !rows[0].WindowNoOverlap {
		t.Error("a May audit-log window against an August billing period must be flagged as non-overlapping")
	}
}

// TestBuildProviderQuotaRows_WindowOverlap_PartialOverlapNotFlagged is the no-overlap rule's
// negative case: the normal, expected "windows don't align but do share
// some time" situation must NOT be flagged — only the extreme disjoint case.
func TestBuildProviderQuotaRows_WindowOverlap_PartialOverlapNotFlagged(t *testing.T) {
	lim := requestsLimit(1000)
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC) // period: 08-01 ~ 09-01 (1mo since 2026-01-01)
	windowFrom := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	windowTo := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	rows := buildProviderQuotaRows(&Report2{}, oneRef("acct1", &lim), now, windowFrom, windowTo)
	if rows[0].WindowNoOverlap {
		t.Error("a window that partially overlaps the billing period must not be flagged")
	}
}

// TestBuildProviderQuotaRows_WindowNoOverlap_ZeroFromSkipsCheck covers the
// empty-report edge case (no records at all, windowFrom stays zero) — must
// never flag, since there's no meaningful window to compare.
func TestBuildProviderQuotaRows_WindowNoOverlap_ZeroFromSkipsCheck(t *testing.T) {
	lim := requestsLimit(1000)
	rows := buildProviderQuotaRows(&Report2{}, oneRef("acct1", &lim), time.Now(), time.Time{}, time.Time{})
	if rows[0].WindowNoOverlap {
		t.Error("a zero windowFrom (no records) must never flag WindowNoOverlap")
	}
}

// TestBuildProviderQuotaRows_LiveNil_StillRendersRow is the report-side half
// of §5.2's stale-period trap gate: cmd_report.go's buildProviderQuotas is
// responsible for leaving Live nil when the on-disk bucket's period doesn't
// match "now" (see that function's own test in cmd/vmr) — this test locks
// in that buildProviderQuotaRows, given a Live==nil ProviderQuotaRef, must
// still produce a row (with Live nil, not omit the account) rather than
// silently dropping the provider or fabricating a Live value.
func TestBuildProviderQuotaRows_LiveNil_StillRendersRow(t *testing.T) {
	lim := requestsLimit(1000)
	rep := &Report2{}
	quotas := map[string][]ProviderQuotaRef{"acct1": {{Limit: &lim, Live: nil}}}
	rows := buildProviderQuotaRows(rep, quotas, time.Now(), time.Time{}, time.Time{})
	if len(rows) != 1 || rows[0].Live != nil {
		t.Fatalf("rows = %+v, want exactly one row with Live == nil", rows)
	}
}

func TestBuildProviderQuotaRows_NilLimit_RowOmitted(t *testing.T) {
	quotas := map[string][]ProviderQuotaRef{
		"no-limit": {{}},
	}
	rows := buildProviderQuotaRows(&Report2{}, quotas, time.Now(), time.Time{}, time.Time{})
	if len(rows) != 0 {
		t.Fatalf("rows with nil Limit must be omitted, got %+v", rows)
	}
}

func TestBuildProviderQuotaRows_SortsLiveFirstByPctDesc_ThenNameTieBreak(t *testing.T) {
	limA, limB, limC := requestsLimit(100), requestsLimit(100), requestsLimit(100)
	quotas := map[string][]ProviderQuotaRef{
		"no-live":  {{Limit: &limA}},
		"low-pct":  {{Limit: &limB, Live: &LiveQuota{Used: 10, Pct: 10}}},
		"high-pct": {{Limit: &limC, Live: &LiveQuota{Used: 90, Pct: 90}}},
	}
	rows := buildProviderQuotaRows(&Report2{}, quotas, time.Now(), time.Time{}, time.Time{})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	got := []string{rows[0].Provider, rows[1].Provider, rows[2].Provider}
	want := []string{"high-pct", "low-pct", "no-live"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sort order = %v, want %v", got, want)
		}
	}
}

// TestBuildProviderQuotaRows_MultiLimit_OneRowPerLimit pins P3's core
// shape change: a provider with more than one Limit produces one row per
// Limit (a per-model Limit's ref already carries which specific model it's
// about — see ProviderQuotaRef's doc comment — mirroring what
// cmd_report.go's buildProviderQuotas would have fanned out from a live
// vmr-quota.json), each accumulating only the traffic that ref's own Scope
// covers (or all traffic, when shared) — not one row per provider.
func TestBuildProviderQuotaRows_MultiLimit_OneRowPerLimit(t *testing.T) {
	dailyScoped := requestsLimit(200)
	dailyScoped.EveryUnit, dailyScoped.EveryText = "d", "1d"
	dailyScoped.Models = []string{"premium-model"}
	monthlyUnscoped := requestsLimit(90000)
	quotas := map[string][]ProviderQuotaRef{"acct1": {
		{Limit: &dailyScoped, Model: "premium-model", Models: []string{"premium-model"}},
		{Limit: &monthlyUnscoped},
	}}
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:premium-model", Requests: 3, Forwarded: 3},
		{Endpoint: "openai-completions:acct1:other-model", Requests: 5, Forwarded: 5},
	}}
	rows := buildProviderQuotaRows(rep, quotas, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one per Limit)", len(rows))
	}
	var scoped, unscoped *ProviderQuotaRow
	for i := range rows {
		if rows[i].Every == "1d" {
			scoped = &rows[i]
		} else {
			unscoped = &rows[i]
		}
	}
	if scoped == nil || scoped.WindowConsumed == nil || *scoped.WindowConsumed != 3 {
		t.Fatalf("scoped row WindowConsumed = %v, want 3 (only premium-model's 3 requests)", scoped)
	}
	if unscoped == nil || unscoped.WindowConsumed == nil || *unscoped.WindowConsumed != 8 {
		t.Fatalf("unscoped row WindowConsumed = %v, want 8 (both endpoints' 3+5 requests)", unscoped)
	}
}

// TestBuildProviderQuotaRows_CostMetric_MixedPricedAndUnpricedIsFlagged is the
// two tests above's failure on the unresolved-rate axis instead of the
// degraded-usage one: WindowConsumed's "-" guard only fires when NOTHING
// priced, so this rendered a precise $3.00 with 40 requests invisible.
func TestBuildProviderQuotaRows_CostMetric_MixedPricedAndUnpricedIsFlagged(t *testing.T) {
	lim := costLimit(100)
	priced := 3.0
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", Requests: 60, CostEstimate: &priced},
		{Endpoint: "openai-completions:acct1:m2", Requests: 40}, // no rate resolved — CostEstimate nil
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed == nil || *rows[0].WindowConsumed != 3.0 {
		t.Fatalf("WindowConsumed = %v, want 3.0 (only the priced endpoint can be valued)", derefOrNil(rows[0].WindowConsumed))
	}
	if rows[0].WindowUnpricedPct != 40 {
		t.Errorf("WindowUnpricedPct = %v, want 40 (40 of 100 requests are absent from that 3.0)", rows[0].WindowUnpricedPct)
	}
}

// TestBuildProviderQuotaRows_CostMetric_FullyPricedIsNotFlagged guards the
// other direction: config.validate already forces every configured model to
// price, so this is the normal report — a false positive would stamp a "data
// is missing" warning on a healthy one.
func TestBuildProviderQuotaRows_CostMetric_FullyPricedIsNotFlagged(t *testing.T) {
	lim := costLimit(100)
	a, b := 1.0, 2.0
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", Requests: 10, CostEstimate: &a},
		{Endpoint: "openai-completions:acct1:m2", Requests: 10, CostEstimate: &b},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowUnpricedPct != 0 {
		t.Errorf("WindowUnpricedPct = %v, want 0 (every endpoint priced)", rows[0].WindowUnpricedPct)
	}
}

// TestBuildProviderQuotaRows_CostMetric_AllUnpricedStaysDashNotZeroPct pins the
// two guards' interaction: nothing priced already renders "-", and "100%
// missing" beside it is noise. ◇ is for a number that exists but is short.
func TestBuildProviderQuotaRows_CostMetric_AllUnpricedStaysDashNotZeroPct(t *testing.T) {
	lim := costLimit(100)
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:acct1:m1", Requests: 25},
	}}
	rows := buildProviderQuotaRows(rep, oneRef("acct1", &lim), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.Time{})
	if rows[0].WindowConsumed != nil {
		t.Fatalf("WindowConsumed = %v, want nil (traffic existed, none of it priced)", derefOrNil(rows[0].WindowConsumed))
	}
	if rows[0].WindowUnpricedPct != 0 {
		t.Errorf("WindowUnpricedPct = %v, want 0 — the nil WindowConsumed already says everything", rows[0].WindowUnpricedPct)
	}
}
