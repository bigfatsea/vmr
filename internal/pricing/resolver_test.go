// Ver 2026-08-10, by Sonnet 5
package pricing

import (
	"testing"
)

func TestResolver_RateFor_TableOnly(t *testing.T) {
	r := NewResolver(testTable(), nil, 1, "")
	rate, ok := r.RateFor("anthropic", "claude-3-5-sonnet")
	if !ok || *rate.InFresh != 3 {
		t.Fatalf("RateFor = %+v ok=%v, want InFresh=3", rate, ok)
	}
}

func TestResolver_RateFor_UnknownProvider_NoPolicy_StillTriesTable(t *testing.T) {
	r := NewResolver(testTable(), nil, 1, "")
	// "anthropic" as the vmr provider name lets step ② of the 4-step
	// resolution succeed even with zero policy configured for it.
	rate, ok := r.RateFor("anthropic", "claude-3-5-sonnet")
	if !ok || rate.InFresh == nil {
		t.Fatalf("RateFor should resolve via the table alone, got ok=%v rate=%+v", ok, rate)
	}
}

func TestResolver_RateFor_PerProviderPolicy(t *testing.T) {
	r := NewResolver(testTable(), map[string]ProviderPolicy{
		"my-plan": {Overrides: []OverrideRule{{Model: "*", Discount: f(0.5)}}},
	}, 1, "")
	// "totally-unknown-model" has no table entry under any resolution step
	// (unlike claude-3-5-sonnet, which testTable() would resolve via the
	// unique-suffix step) — a wildcard discount override still counts as
	// "something to go on" (ok=true, matching Resolve's own documented
	// contract), but scaling an empty Base produces an empty (all-nil)
	// Rate — report's costFor treats every nil component as 0, so this
	// degrades to "no cost estimate", not an error.
	rate, ok := r.RateFor("my-plan", "totally-unknown-model")
	if !ok {
		t.Fatal("a matching override, even with nothing to discount, still counts as ok=true")
	}
	if rate.InFresh != nil {
		t.Fatalf("rate = %+v, want all-nil (discount scaling an empty Base stays empty)", rate)
	}
}

func TestResolver_RateFor_NoMatch(t *testing.T) {
	r := NewResolver(testTable(), nil, 1, "")
	if _, ok := r.RateFor("nope", "nope"); ok {
		t.Fatal("want ok=false for a totally unknown provider+model")
	}
}

func TestResolver_MemoizesResolution(t *testing.T) {
	r := NewResolver(testTable(), nil, 1, "")
	// Call twice; the second call should hit the cache path (resolve()'s
	// map lookup) rather than re-running resolveCanonicalKey — this test
	// can't observe the internal call count directly, but it does prove
	// repeated calls stay consistent and don't error out on a second pass
	// through the cache-hit branch.
	r1, ok1 := r.RateFor("anthropic", "claude-3-5-sonnet")
	r2, ok2 := r.RateFor("anthropic", "claude-3-5-sonnet")
	if !ok1 || !ok2 || *r1.InFresh != *r2.InFresh {
		t.Fatalf("repeated RateFor calls disagree: %+v/%v vs %+v/%v", r1, ok1, r2, ok2)
	}
}

func TestResolver_CachesMisses(t *testing.T) {
	r := NewResolver(testTable(), nil, 1, "")
	_, ok1 := r.RateFor("nope", "nope")
	_, ok2 := r.RateFor("nope", "nope")
	if ok1 || ok2 {
		t.Fatal("want ok=false consistently for a cached miss")
	}
}

func TestResolver_WithDisplayFactor_ScalesRateFor(t *testing.T) {
	r := NewResolver(testTable(), nil, 1, "").WithDisplayFactor(7.1)
	rate, ok := r.RateFor("anthropic", "claude-3-5-sonnet")
	if !ok {
		t.Fatal("RateFor: ok = false")
	}
	if got, want := *rate.InFresh, 3*7.1; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("InFresh = %v, want %v (3 x display factor 7.1)", got, want)
	}
}

func TestResolver_WithDisplayFactor_OneOrZeroIsNoOp(t *testing.T) {
	base := NewResolver(testTable(), nil, 1, "")
	for _, factor := range []float64{0, 1} {
		r := base.WithDisplayFactor(factor)
		rate, ok := r.RateFor("anthropic", "claude-3-5-sonnet")
		if !ok || *rate.InFresh != 3 {
			t.Fatalf("WithDisplayFactor(%v): rate = %+v ok=%v, want InFresh=3 unchanged", factor, rate, ok)
		}
	}
}

func TestResolver_WithDisplayFactor_OriginalResolverUnaffected(t *testing.T) {
	base := NewResolver(testTable(), nil, 1, "")
	_ = base.WithDisplayFactor(7.1)
	rate, ok := base.RateFor("anthropic", "claude-3-5-sonnet")
	if !ok || *rate.InFresh != 3 {
		t.Fatalf("base Resolver was mutated by WithDisplayFactor: rate = %+v ok=%v, want InFresh=3", rate, ok)
	}
}
