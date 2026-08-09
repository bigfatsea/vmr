// Ver 2026-08-07, by Opus 5

// TestMarketFixtures_EqualWeightedOverestimate3to8x pins the design doc's
// central empirical claim (docs/TokenPlan_Quota_Routing_Design_opus-5.md's
// §2.3/§14.2): treating a Credits-style account's quota as an equal-
// weighted token total (P1's metric: tokens behavior) overestimates real
// consumption by roughly 3-8x for a realistic agentic workload (99:1
// input:output, 90% cache hit rate — the design doc's own stated
// benchmark profile), because cache reads are typically priced far below
// fresh input. Unlike the design doc's own two hand-picked examples, this
// test draws its two fixtures directly from the EMBEDDED standard table
// (real market data, not synthetic numbers) — see
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md's §7 "真实费率夹具" test row.
package pricing

import "testing"

// marketFixtureRatio computes, for one provider+model's real Rate, the
// overestimate ratio a P1-style equal-weighted token count produces versus
// the true $-equivalent weighted count — under a 99:1 input:output,
// 90%-cache-hit-rate workload profile (the design doc's stated market
// benchmark methodology). Both sums are expressed in "fresh-input-token
// equivalents" (each Rate component's price normalized against InFresh's
// own price) so the ratio is dimensionless and comparable across models
// regardless of currency/scale.
func marketFixtureRatio(t *testing.T, key string) float64 {
	t.Helper()
	tbl, err := LoadStandard()
	if err != nil {
		t.Fatalf("LoadStandard: %v", err)
	}
	rate, ok := tbl.Lookup(key)
	if !ok {
		t.Fatalf("fixture %q not found in the embedded standard table", key)
	}
	if !rate.Complete() {
		t.Fatalf("fixture %q is not fully priced (missing %v) — pick a different fixture", key, rate.MissingComponents())
	}

	const totalInput = 1_000_000.0
	fresh := totalInput * 0.10  // 90% cache hit rate -> 10% fresh
	cached := totalInput * 0.90 // 90% cache hit rate -> 90% served from cache
	out := totalInput / 99      // 99:1 input:output ratio

	equalWeighted := fresh + cached + out
	priceWeighted := fresh*(*rate.InFresh / *rate.InFresh) +
		cached*(*rate.CacheRead / *rate.InFresh) +
		out*(*rate.Out / *rate.InFresh)

	return equalWeighted / priceWeighted
}

func TestMarketFixtures_EqualWeightedOverestimate3to8x(t *testing.T) {
	for _, key := range []string{
		"deepseek/deepseek-v4-pro",  // real market data: 120x cache_read discount vs fresh input
		"anthropic/claude-opus-4-1", // real market data: 10x cache_read discount, 5x output premium
	} {
		ratio := marketFixtureRatio(t, key)
		if ratio < 3 || ratio > 8 {
			t.Errorf("%s: equal-weighted overestimate ratio = %.2fx, want within [3, 8]x (design doc's §2.3 market-wide finding)", key, ratio)
		} else {
			t.Logf("%s: equal-weighted overestimate ratio = %.2fx", key, ratio)
		}
	}
}
