// Ver 2026-08-07, by Opus 5
package report

import (
	"testing"

	"vmr/internal/pricing"
)

func rf(v float64) *float64 { return &v }

// TestCostFor_IncludesCacheRead pins the P2.2 fix documented in costFor's
// own comment: the pre-P2.2 formula silently excluded cache_read, which
// only looked correct because the repo's own sample pricing.yaml happened
// to price it at 0 for all four providers. A nonzero cache_read rate must
// now produce a strictly higher $ figure than the same usage priced with
// cache_read excluded.
func TestCostFor_IncludesCacheRead(t *testing.T) {
	rc := &rec2{usageOK: true}
	rc.usage.In = 1_000_000
	rc.usage.CacheRead = 500_000
	rc.usage.Out = 100_000

	withCacheRead := pricing.Rate{InFresh: rf(1.0), CacheRead: rf(0.5), CacheWrite: rf(1.0), Out: rf(4.0)}
	withoutCacheRead := pricing.Rate{InFresh: rf(1.0), CacheRead: nil, CacheWrite: rf(1.0), Out: rf(4.0)}

	got := costFor(withCacheRead, rc)
	gotExcluded := costFor(withoutCacheRead, rc)
	if got <= gotExcluded {
		t.Fatalf("costFor with a priced cache_read (%v) should exceed costFor with cache_read excluded (%v)", got, gotExcluded)
	}

	// fresh = In - CacheRead - CacheWrite = 1,000,000 - 500,000 - 0 = 500,000.
	want := (500_000.0/1e6)*1.0 + (500_000.0/1e6)*0.5 + (100_000.0/1e6)*4.0
	if got != want {
		t.Fatalf("costFor = %v, want %v", got, want)
	}
}

func TestCostFor_MissingRateComponent_TreatedAsZero(t *testing.T) {
	rc := &rec2{usageOK: true}
	rc.usage.In = 1_000_000
	rate := pricing.Rate{InFresh: rf(2.0)} // everything else nil
	got := costFor(rate, rc)
	if got != 2.0 {
		t.Fatalf("costFor = %v, want 2.0 (nil components contribute 0)", got)
	}
}

func TestCostFor_NoUsage_ReturnsZero(t *testing.T) {
	rc := &rec2{usageOK: false}
	rate := pricing.Rate{InFresh: rf(999)}
	if got := costFor(rate, rc); got != 0 {
		t.Fatalf("costFor = %v, want 0 (no usage extracted)", got)
	}
}

func TestSplitEndpointProviderModel(t *testing.T) {
	for _, tc := range []struct {
		endpoint                string
		wantProvider, wantModel string
	}{
		{"openai:volcengine:doubao-seed-2.0-lite", "volcengine", "doubao-seed-2.0-lite"},
		{"openai:openrouter:z-ai/glm-5.2", "openrouter", "z-ai/glm-5.2"},
		{"openai:openrouter:vendor:sub-model", "openrouter", "vendor:sub-model"},
		{"malformed", "", ""},
	} {
		p, m := splitEndpointProviderModel(tc.endpoint)
		if p != tc.wantProvider || m != tc.wantModel {
			t.Errorf("splitEndpointProviderModel(%q) = (%q, %q), want (%q, %q)", tc.endpoint, p, m, tc.wantProvider, tc.wantModel)
		}
	}
}
