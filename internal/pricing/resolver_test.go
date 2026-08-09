// Ver 2026-08-07, by Opus 5
package pricing

import (
	"testing"
	"time"
)

func TestResolver_RateFor_TableOnly(t *testing.T) {
	r := NewResolver(testTable(), nil)
	rate, ok := r.RateFor("anthropic", "claude-3-5-sonnet", time.Now())
	if !ok || *rate.InFresh != 3 {
		t.Fatalf("RateFor = %+v ok=%v, want InFresh=3", rate, ok)
	}
}

func TestResolver_RateFor_UnknownProvider_NoPolicy_StillTriesTable(t *testing.T) {
	r := NewResolver(testTable(), nil)
	// "anthropic" as the vmr provider name lets step ② of the 4-step
	// resolution succeed even with zero policy configured for it.
	rate, ok := r.RateFor("anthropic", "claude-3-5-sonnet", time.Now())
	if !ok || rate.InFresh == nil {
		t.Fatalf("RateFor should resolve via the table alone, got ok=%v rate=%+v", ok, rate)
	}
}

func TestResolver_RateFor_PerProviderPolicy(t *testing.T) {
	r := NewResolver(testTable(), map[string]ProviderPolicy{
		"my-plan": {Overrides: []OverrideRule{{Model: "*", Discount: f(0.5)}}},
	})
	// "totally-unknown-model" has no table entry under any resolution step
	// (unlike claude-3-5-sonnet, which testTable() would resolve via the
	// unique-suffix step) — a wildcard discount override still counts as
	// "something to go on" (ok=true, matching Resolve's own documented
	// contract), but scaling an empty Base produces an empty (all-nil)
	// Rate — report's costFor treats every nil component as 0, so this
	// degrades to "no cost estimate", not an error.
	rate, ok := r.RateFor("my-plan", "totally-unknown-model", time.Now())
	if !ok {
		t.Fatal("a matching override, even with nothing to discount, still counts as ok=true")
	}
	if rate.InFresh != nil {
		t.Fatalf("rate = %+v, want all-nil (discount scaling an empty Base stays empty)", rate)
	}
}

func TestResolver_RateFor_NoMatch(t *testing.T) {
	r := NewResolver(testTable(), nil)
	if _, ok := r.RateFor("nope", "nope", time.Now()); ok {
		t.Fatal("want ok=false for a totally unknown provider+model")
	}
}

func TestResolver_MemoizesResolution(t *testing.T) {
	r := NewResolver(testTable(), nil)
	// Call twice; the second call should hit the cache path (resolve()'s
	// map lookup) rather than re-running resolveCanonicalKey — this test
	// can't observe the internal call count directly, but it does prove
	// repeated calls stay consistent and don't error out on a second pass
	// through the cache-hit branch.
	r1, ok1 := r.RateFor("anthropic", "claude-3-5-sonnet", time.Now())
	r2, ok2 := r.RateFor("anthropic", "claude-3-5-sonnet", time.Now())
	if !ok1 || !ok2 || *r1.InFresh != *r2.InFresh {
		t.Fatalf("repeated RateFor calls disagree: %+v/%v vs %+v/%v", r1, ok1, r2, ok2)
	}
}

func TestResolver_CachesMisses(t *testing.T) {
	r := NewResolver(testTable(), nil)
	_, ok1 := r.RateFor("nope", "nope", time.Now())
	_, ok2 := r.RateFor("nope", "nope", time.Now())
	if ok1 || ok2 {
		t.Fatal("want ok=false consistently for a cached miss")
	}
}

func TestResolver_TimeWindowedOverride_PerCallRateChanges(t *testing.T) {
	r := NewResolver(NewTable("USD"), map[string]ProviderPolicy{
		"plan-e": {Overrides: []OverrideRule{
			{Model: "*", Discount: f(0.25), DateFrom: "2026-06-08", DateTo: "2026-08-08"},
			{Model: "my-model-x", Explicit: Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}},
		}},
	})
	inPromo := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	outsidePromo := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r1, ok1 := r.RateFor("plan-e", "my-model-x", inPromo)
	r2, ok2 := r.RateFor("plan-e", "my-model-x", outsidePromo)
	if !ok1 || !ok2 {
		t.Fatalf("both calls should resolve, got ok1=%v ok2=%v", ok1, ok2)
	}
	if *r1.InFresh == *r2.InFresh {
		t.Fatalf("promo and non-promo timestamps should resolve different rates, both got %v", *r1.InFresh)
	}
}
