// Ver 2026-08-07, by Opus 5
package report

import (
	"testing"

	"vmr/internal/pricing"
)

func rf(v float64) *float64 { return &v }

// TestCostFor_IncludesCacheRead pins the cost fix documented in costFor's
// own comment: an earlier formula silently excluded cache_read, which
// only looked correct because the repo's own sample pricing.yaml happened
// to price it at 0 for all four providers. A nonzero cache_read rate must
// now produce a strictly higher $ figure than the same usage priced with
// cache_read excluded.
func TestCostFor_IncludesCacheRead(t *testing.T) {
	rc := &rec2{usageInOK: true, usageOutOK: true}
	rc.usage.In = 1_000_000
	rc.usage.CacheRead = 500_000
	rc.usage.Out = 100_000

	withCacheRead := pricing.Rate{InFresh: rf(1.0), CacheRead: rf(0.5), CacheWrite: rf(1.0), Out: rf(4.0)}
	withoutCacheRead := pricing.Rate{InFresh: rf(1.0), CacheRead: nil, CacheWrite: rf(1.0), Out: rf(4.0)}

	got, estCost := costFor(withCacheRead, rc)
	gotExcluded, _ := costFor(withoutCacheRead, rc)
	if got <= gotExcluded {
		t.Fatalf("costFor with a priced cache_read (%v) should exceed costFor with cache_read excluded (%v)", got, gotExcluded)
	}
	if estCost != 0 {
		t.Errorf("costFor with both sides sniffed must report estCost=0, got %v", estCost)
	}

	// fresh = In - CacheRead - CacheWrite = 1,000,000 - 500,000 - 0 = 500,000.
	want := (500_000.0/1e6)*1.0 + (500_000.0/1e6)*0.5 + (100_000.0/1e6)*4.0
	if got != want {
		t.Fatalf("costFor = %v, want %v", got, want)
	}
}

func TestCostFor_MissingRateComponent_TreatedAsZero(t *testing.T) {
	rc := &rec2{usageInOK: true, usageOutOK: true}
	rc.usage.In = 1_000_000
	rate := pricing.Rate{InFresh: rf(2.0)} // everything else nil
	got, _ := costFor(rate, rc)
	if got != 2.0 {
		t.Fatalf("costFor = %v, want 2.0 (nil components contribute 0)", got)
	}
}

// TestCostFor_NoUsage_PricesDegradedEstimate pins the false-zero fix: before
// it, a record whose usage was never sniffed contributed a hardcoded 0 to
// every CostEstimate bucket regardless of how much traffic it actually
// represented — a window entirely made of such records rendered a
// misleadingly precise $0.0000 instead of either a real number or "-". Now
// it prices the same degraded byte-count estimate (rc.estInFresh/rc.estOut)
// internal/router/quota.go's tokenCharge degraded branch charges.
func TestCostFor_NoUsage_PricesDegradedEstimate(t *testing.T) {
	rc := &rec2{estInFresh: 1_000_000, estOut: 500_000}
	rate := pricing.Rate{InFresh: rf(2.0), CacheRead: rf(0.5), CacheWrite: rf(1.0), Out: rf(4.0)}
	got, estCost := costFor(rate, rc)
	// No cache components: the degraded estimate can't tell cache hits
	// apart from a fresh token, same as the router's own degraded branch.
	want := (1_000_000.0/1e6)*2.0 + (500_000.0/1e6)*4.0
	if got != want {
		t.Fatalf("costFor = %v, want %v (Fresh/Out only, no cache components)", got, want)
	}
	if estCost != got {
		t.Errorf("both sides degraded: estCost (%v) must equal the whole cost (%v)", estCost, got)
	}
}

// TestCostFor_SplitSide pins VD1: a record with real input usage but a
// degraded output side prices its cost almost entirely from the exact input,
// so estCost must be only the small output-side portion — not the whole
// figure, which would inflate WindowEstimatedPct toward 100% for an account
// that is in fact ~99% exactly billed.
func TestCostFor_SplitSide(t *testing.T) {
	rc := &rec2{usageInOK: true, usageOutOK: false, estOut: 10}
	rc.usage.In = 200_000
	rate := pricing.Rate{InFresh: rf(3.0), Out: rf(15.0)}
	got, estCost := costFor(rate, rc)
	near := func(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }
	wantTotal := (200_000.0/1e6)*3.0 + (10.0/1e6)*15.0
	wantEst := (10.0 / 1e6) * 15.0
	if !near(got, wantTotal) {
		t.Fatalf("costFor total = %v, want %v", got, wantTotal)
	}
	if !near(estCost, wantEst) {
		t.Fatalf("costFor estCost = %v, want %v (output side only)", estCost, wantEst)
	}
	if estCost >= got {
		t.Errorf("estCost (%v) must be a small fraction of the total (%v)", estCost, got)
	}
}

func TestCostFor_NoUsageNoEstimate_ReturnsZero(t *testing.T) {
	rc := &rec2{} // estInFresh/estOut both zero: e.g. a replay record with no response at all
	rate := pricing.Rate{InFresh: rf(999)}
	got, estCost := costFor(rate, rc)
	if got != 0 || estCost != 0 {
		t.Fatalf("costFor = (%v, %v), want (0, 0) — no usage, no estimate, nothing to attribute", got, estCost)
	}
}

func TestSplitEndpointProviderModel(t *testing.T) {
	for _, tc := range []struct {
		endpoint                string
		wantProvider, wantModel string
	}{
		{"openai-completions:volcengine:doubao-seed-2.0-lite", "volcengine", "doubao-seed-2.0-lite"},
		{"openai-completions:openrouter:z-ai/glm-5.2", "openrouter", "z-ai/glm-5.2"},
		{"openai-completions:openrouter:vendor:sub-model", "openrouter", "vendor:sub-model"},
		{"malformed", "", ""},
	} {
		p, m := splitEndpointProviderModel(tc.endpoint)
		if p != tc.wantProvider || m != tc.wantModel {
			t.Errorf("splitEndpointProviderModel(%q) = (%q, %q), want (%q, %q)", tc.endpoint, p, m, tc.wantProvider, tc.wantModel)
		}
	}
}
