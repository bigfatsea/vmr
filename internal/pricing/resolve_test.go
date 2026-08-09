// Ver 2026-08-07, by Opus 5
package pricing

import (
	"testing"
	"time"

	"vmr/internal/fmtutil"
)

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

func testTable() *Table {
	tbl := NewTable("USD")
	tbl.put("anthropic/claude-3-5-sonnet", Rate{InFresh: f(3), CacheRead: f(0.3), CacheWrite: f(3.75), Out: f(15)})
	tbl.put("deepseek/deepseek-chat", Rate{InFresh: f(0.28), CacheRead: f(0.028), Out: f(0.42)}) // CacheWrite deliberately missing, like the real deepseek data
	return tbl
}

// --- canonical key 4-step resolution ---

func TestResolveCanonicalKey_MapExplicit(t *testing.T) {
	r, ok := resolveCanonicalKey("my-plan", "my-model-x", testTable(), map[string]string{"my-model-x": "anthropic/claude-3-5-sonnet"})
	if !ok || *r.InFresh != 3 {
		t.Fatalf("map-explicit resolution failed: ok=%v r=%+v", ok, r)
	}
}

func TestResolveCanonicalKey_ProviderSlashModel(t *testing.T) {
	r, ok := resolveCanonicalKey("anthropic", "claude-3-5-sonnet", testTable(), nil)
	if !ok || *r.InFresh != 3 {
		t.Fatalf("<provider>/<model> resolution failed: ok=%v r=%+v", ok, r)
	}
}

func TestResolveCanonicalKey_BareModel(t *testing.T) {
	tbl := NewTable("USD")
	tbl.put("gpt-4o", Rate{InFresh: f(2.5)}) // some standard-table entries are bare, no vendor prefix
	r, ok := resolveCanonicalKey("my-openai-account", "gpt-4o", tbl, nil)
	if !ok || *r.InFresh != 2.5 {
		t.Fatalf("bare-model resolution failed: ok=%v r=%+v", ok, r)
	}
}

func TestResolveCanonicalKey_UniqueSuffixFallback(t *testing.T) {
	// provider name ("my-plan-a") doesn't match the vendor prefix, and there's
	// no bare "claude-3-5-sonnet" row — only step ④ (unique suffix) resolves.
	r, ok := resolveCanonicalKey("my-plan-a", "claude-3-5-sonnet", testTable(), nil)
	if !ok || *r.InFresh != 3 {
		t.Fatalf("unique-suffix fallback failed: ok=%v r=%+v", ok, r)
	}
}

func TestResolveCanonicalKey_NoMatch_NoGuess(t *testing.T) {
	_, ok := resolveCanonicalKey("my-plan-a", "totally-unknown-model", testTable(), nil)
	if ok {
		t.Fatal("no step should have matched, want ok=false")
	}
}

// --- Resolve(): table + override composition ---

func TestResolve_TableOnly_NoOverrides(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{Table: testTable(), Currency: "USD"})
	if !ok {
		t.Fatal("Resolve failed")
	}
	if spec.Base.InFresh == nil || *spec.Base.InFresh != 3 {
		t.Fatalf("Base.InFresh = %v, want 3", spec.Base.InFresh)
	}
	if len(spec.Overrides) != 0 {
		t.Fatalf("no overrides configured, want empty Overrides, got %v", spec.Overrides)
	}
}

func TestResolve_ExchangeRateAppliedToTableOnly(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{Table: testTable(), Currency: "CNY", ExchangeRateToTarget: 7.1})
	if !ok {
		t.Fatal("Resolve failed")
	}
	if !almostEqual(*spec.Base.InFresh, 3*7.1) {
		t.Fatalf("Base.InFresh = %v, want %v (table rate x exchange rate)", *spec.Base.InFresh, 3*7.1)
	}
}

// TestResolve_UnconditionalOverride_NotFoldedIntoBase pins the fix for a
// real double-application bug caught by TestResolve_DiscountAppliesOnceToBase
// below: Resolve must NOT fold an unconditional override into Base, because
// RateAt (the only place Base and an Override are ever combined) applies
// every matching Override on top of Base — an unconditional one matches
// every ts, so folding it into Base too would apply it twice. Base stays
// the pure table lookup; GuaranteedRate is where the composed value lives.
func TestResolve_UnconditionalOverride_NotFoldedIntoBase(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.6)}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	if !almostEqual(*spec.Base.InFresh, 3) {
		t.Fatalf("Base.InFresh = %v, want 3 (pure table value — the override must live in Overrides, not be pre-applied)", *spec.Base.InFresh)
	}
	if len(spec.Overrides) != 1 {
		t.Fatalf("want the discount override preserved in Overrides, got %v", spec.Overrides)
	}
}

func TestResolve_UnconditionalDiscountOverride_ComposesViaGuaranteedRate(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.6)}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := GuaranteedRate(spec)
	if !almostEqual(*got.InFresh, 3*0.6) {
		t.Fatalf("GuaranteedRate.InFresh = %v, want %v (table rate x 0.6 discount)", *got.InFresh, 3*0.6)
	}
}

func TestResolve_UnconditionalExplicitOverride_ReplacesTable(t *testing.T) {
	explicit := Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "my-model-x", Explicit: explicit}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := GuaranteedRate(spec)
	if *got.InFresh != 1.58 || *got.Out != 9.54 {
		t.Fatalf("GuaranteedRate = %+v, want the explicit override rate, table not involved", got)
	}
}

func TestResolve_OverrideOnlyModel_NoTableEntry_StillResolves(t *testing.T) {
	// "my-model-x" has no canonical-table match at all — an explicit
	// override is the ONLY source, and that's sufficient.
	explicit := Rate{InFresh: f(1), CacheRead: f(0.1), CacheWrite: f(1), Out: f(4)}
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "my-model-x", Explicit: explicit}},
	})
	if !ok {
		t.Fatal("Resolve should succeed from the override alone")
	}
	if !GuaranteedRate(spec).Complete() {
		t.Fatalf("GuaranteedRate = %+v, want complete", GuaranteedRate(spec))
	}
}

func TestResolve_NothingMatches_OkFalse(t *testing.T) {
	_, ok := Resolve("plan-x", "totally-unknown", ResolveOptions{Table: testTable(), Currency: "USD"})
	if ok {
		t.Fatal("want ok=false: no table entry, no override")
	}
}

func TestResolve_TimeScopedOverrideOnly_GuaranteedRateStaysIncomplete(t *testing.T) {
	// The ONLY override is date-scoped (a temporary promo) — GuaranteedRate
	// (the "always available" rate config.validate() checks) must NOT be
	// considered complete just because a temporary window happens to
	// supply one; see GuaranteedRate's own doc comment.
	spec, ok := Resolve("plan-x", "totally-unknown", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.5), DateFrom: "2026-06-01", DateTo: "2026-06-30"}},
	})
	if !ok {
		t.Fatal("Resolve should still succeed (an override matched), just with an incomplete GuaranteedRate")
	}
	if GuaranteedRate(spec).Complete() {
		t.Fatal("GuaranteedRate must not be complete when only a time-scoped override supplies it")
	}
	if len(spec.Overrides) != 1 {
		t.Fatalf("want the time-scoped override preserved for RateAt, got %v", spec.Overrides)
	}
}

func TestResolve_OverrideModelPatternFiltering(t *testing.T) {
	spec, ok := Resolve("plan-b", "other-model", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "some-other-model", Discount: f(0.5)}},
	})
	// The only table entry that could match "other-model" via any of the 4
	// resolution steps doesn't exist, and the override's model pattern
	// doesn't match either -> nothing resolves.
	if ok {
		t.Fatalf("override for a different model must not apply here, got spec=%+v", spec)
	}
}

// --- RateAt(): charge-time window selection ---

func TestRateAt_NoOverrides_ReturnsBase(t *testing.T) {
	spec, _ := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{Table: testTable(), Currency: "USD"})
	r := RateAt(spec, time.Now())
	if *r.InFresh != 3 {
		t.Fatalf("RateAt = %+v, want Base (InFresh=3)", r)
	}
}

func TestRateAt_TimeWindowSelection(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: NewTable("USD"), Currency: "USD",
		Overrides: []OverrideRule{
			// Promo listed FIRST (first-match-wins): 25% off in a June window.
			{Model: "*", Discount: f(0.25), DateFrom: "2026-06-08", DateTo: "2026-08-08"},
			// Always-active explicit rate — this is what Base resolves to.
			{Model: "my-model-x", Explicit: Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}},
			// Catch-all 60% discount fallback (never reached: the explicit
			// rule above matches first for my-model-x at any other time).
			{Model: "*", Discount: f(0.6)},
		},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	if !GuaranteedRate(spec).Complete() {
		t.Fatalf("GuaranteedRate = %+v, want complete (the unconditional explicit override)", GuaranteedRate(spec))
	}

	// Inside the promo window: the FIRST override (the 25%-off promo) wins,
	// scaling Base (1.58) x 0.25.
	inPromo := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	r := RateAt(spec, inPromo)
	if !almostEqual(*r.InFresh, 1.58*0.25) {
		t.Fatalf("in-promo RateAt.InFresh = %v, want %v", *r.InFresh, 1.58*0.25)
	}

	// Outside the promo window: falls through to the explicit override.
	outsidePromo := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	r = RateAt(spec, outsidePromo)
	if *r.InFresh != 1.58 {
		t.Fatalf("outside-promo RateAt.InFresh = %v, want 1.58 (explicit override)", *r.InFresh)
	}
}

func TestRateAt_HourWindow_WrapsMidnight(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	spec, _ := Resolve("volcengine", "ark-code", ResolveOptions{
		Table: NewTable("USD"), Currency: "USD",
		Overrides: []OverrideRule{
			{Model: "*", Explicit: Rate{InFresh: f(0.5), CacheRead: f(0), CacheWrite: f(0), Out: f(4)}, HourFrom: "22:00", HourTo: "06:00"},
			{Model: "*", Explicit: Rate{InFresh: f(1.2), CacheRead: f(0), CacheWrite: f(0), Out: f(8)}},
		},
	})
	at := func(h, mi int) time.Time { return time.Date(2026, 7, 26, h, mi, 0, 0, time.UTC) }

	if r := RateAt(spec, at(23, 0)); *r.InFresh != 0.5 {
		t.Errorf("23:00 should hit the off-peak rate (0.5), got %v", *r.InFresh)
	}
	if r := RateAt(spec, at(10, 0)); *r.InFresh != 1.2 {
		t.Errorf("10:00 should hit the catch-all rate (1.2), got %v", *r.InFresh)
	}
	if r := RateAt(spec, at(22, 0)); *r.InFresh != 0.5 {
		t.Errorf("22:00 boundary should hit the off-peak rate, got %v", *r.InFresh)
	}
	if r := RateAt(spec, at(6, 0)); *r.InFresh != 0.5 {
		t.Errorf("06:00 boundary should hit the off-peak rate, got %v", *r.InFresh)
	}
}

func TestRateAt_ConvertsToDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	spec, _ := Resolve("v", "m", ResolveOptions{
		Table: NewTable("USD"), Currency: "USD",
		Overrides: []OverrideRule{
			{Model: "*", Explicit: Rate{InFresh: f(0.5), CacheRead: f(0), CacheWrite: f(0), Out: f(0)}, HourFrom: "22:00", HourTo: "06:00"},
			{Model: "*", Explicit: Rate{InFresh: f(1.2), CacheRead: f(0), CacheWrite: f(0), Out: f(0)}},
		},
	})

	plusFive := time.FixedZone("+05:00", 5*3600)
	// 23:00+05:00 == 18:00 UTC — outside the 22:00..06:00 window when
	// correctly converted to DisplayZone (UTC here) before matching.
	ts := time.Date(2026, 7, 26, 23, 0, 0, 0, plusFive)
	if r := RateAt(spec, ts); *r.InFresh != 1.2 {
		t.Errorf("RateAt must convert to DisplayZone before matching — got %v, want the catch-all rate (1.2)", *r.InFresh)
	}
}

func TestRateAt_NilSpec_ReturnsZeroRate(t *testing.T) {
	r := RateAt(nil, time.Now())
	if r.InFresh != nil || r.CacheRead != nil || r.CacheWrite != nil || r.Out != nil {
		t.Fatalf("RateAt(nil, ...) = %+v, want the zero Rate", r)
	}
}

// --- discount composes against the resolved lower layer, never re-applies to itself ---

func TestResolve_DiscountAppliesOnceToBase(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.6)}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := RateAt(spec, time.Now())
	want := 3 * 0.6
	if !almostEqual(*got.InFresh, want) {
		t.Fatalf("InFresh = %v, want %v (table rate x 0.6, discount applied exactly once)", *got.InFresh, want)
	}
}

// TestResolve_StackedDiscounts_ComposeMultiplicatively directly exercises
// the "discount layered above another discount" case
// TestResolve_DiscountAppliesOnceToBase's comment describes but doesn't
// itself construct (that test has only one Discount-form rule; this one
// chains two, both unconditional so RateAt picks the same one every call).
// resolveChain's recursion means rule 0's Discount must scale "whatever
// rule 1 resolves to" (which is itself rule 1's Discount scaling Base),
// giving Base x 0.6 x 0.5 — NOT Base x 0.6 (rule 1 ignored) and NOT two
// independent applications straight to Base (which would also give the
// right number here by coincidence of both being simple scalars on the
// same Base, but for the wrong reason — see RateAt's doc comment on why
// "the rate resolved below this rule" must be a genuine recursive descent,
// not always spec.Base directly).
func TestResolve_StackedDiscounts_ComposeMultiplicatively(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{
			{Model: "*", Discount: f(0.6)},
			{Model: "*", Discount: f(0.5)},
		},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := RateAt(spec, time.Now())
	want := 3 * 0.6 * 0.5
	if !almostEqual(*got.InFresh, want) {
		t.Fatalf("InFresh = %v, want %v (table rate x 0.6 x 0.5 — rule 0's discount composed against rule 1's resolution, not against Base directly or in isolation)", *got.InFresh, want)
	}
}

// --- AllPathsComplete: the completeness gate config.validate() actually uses ---

func TestAllPathsComplete_NilSpec(t *testing.T) {
	if ok, _, _ := AllPathsComplete(nil); ok {
		t.Fatal("nil spec must not be considered complete")
	}
}

func TestAllPathsComplete_CompleteBaseNoOverrides(t *testing.T) {
	spec, _ := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{Table: testTable(), Currency: "USD"})
	ok, _, _ := AllPathsComplete(spec)
	if !ok {
		t.Fatal("a fully-priced table entry with no overrides should be complete")
	}
}

func TestAllPathsComplete_IncompleteBaseNoOverrides_Fails(t *testing.T) {
	spec, _ := Resolve("deepseek", "deepseek-chat", ResolveOptions{Table: testTable(), Currency: "USD"})
	ok, bad, idx := AllPathsComplete(spec)
	if ok {
		t.Fatal("deepseek-chat's table entry is missing cache_write — must not be complete")
	}
	if idx != -1 {
		t.Errorf("badIndex = %d, want -1 (the incompleteness is in Base, no override involved)", idx)
	}
	if len(bad.MissingComponents()) == 0 {
		t.Error("badRate should report at least one missing component")
	}
}

// TestAllPathsComplete_UnconditionalOverride_BaseUnreachable_NotChecked pins
// the exact gap this function's implementation had to get right: when an
// unconditional override fully covers a model, spec.Base (which may come
// from an incomplete or nonexistent table entry) is unreachable at every
// possible ts and must NOT cause a false rejection — this is the design
// doc's plan-e shape (an account override supplies pricing for a model the
// standard table doesn't even list).
func TestAllPathsComplete_UnconditionalOverride_BaseUnreachable_NotChecked(t *testing.T) {
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD", // no table entry for my-model-x at all -> Base is empty
		Overrides: []OverrideRule{{Model: "my-model-x", Explicit: Rate{InFresh: f(1), CacheRead: f(0.1), CacheWrite: f(1), Out: f(4)}}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := AllPathsComplete(spec)
	if !complete {
		t.Fatalf("want complete (unconditional override fully covers every reachable ts), got bad=%+v idx=%d", bad, idx)
	}
}

// TestAllPathsComplete_ConditionalDiscountOverIncompleteBase_Fails is the
// dangerous scenario GuaranteedRate alone would miss: a time-scoped
// (promotional) discount override composes against an incomplete Base —
// only wrong at charge time, during the promo window, with no load-time
// signal unless AllPathsComplete specifically checks this path.
func TestAllPathsComplete_ConditionalDiscountOverIncompleteBase_Fails(t *testing.T) {
	spec, ok := Resolve("deepseek", "deepseek-chat", ResolveOptions{
		Table: testTable(), Currency: "USD", // deepseek-chat's Base is missing cache_write
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.5), DateFrom: "2026-06-01", DateTo: "2026-06-30"}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := AllPathsComplete(spec)
	if complete {
		t.Fatal("a conditional discount scaling an incomplete Base must not be considered complete")
	}
	if idx != 0 {
		t.Errorf("badIndex = %d, want 0 (the discount override itself)", idx)
	}
	if len(bad.MissingComponents()) == 0 {
		t.Error("badRate should report at least one missing component")
	}
}

func TestAllPathsComplete_ConditionalOverride_ThenUnconditionalCoverage_OK(t *testing.T) {
	// A temporary promo (conditional) followed by an always-on explicit
	// override that fully covers the model — every reachable ts (promo
	// window or not) resolves complete.
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{
			{Model: "*", Discount: f(0.25), DateFrom: "2026-06-08", DateTo: "2026-08-08"},
			{Model: "my-model-x", Explicit: Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}},
		},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := AllPathsComplete(spec)
	if !complete {
		t.Fatalf("want complete, got bad=%+v idx=%d", bad, idx)
	}
}

// TestAllPathsComplete_RuleAfterUnconditional_Unreachable_NotChecked is the
// mirror image of the Base-unreachable case above: first-match-wins means an
// unconditional rule shadows every LATER rule at every ts, so a later rule
// that would resolve incomplete is dead config, not a live under-pricing
// risk. Checking it anyway would reject a config whose every reachable path
// is fully priced.
func TestAllPathsComplete_RuleAfterUnconditional_Unreachable_NotChecked(t *testing.T) {
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD", // no table entry -> Base is empty/incomplete
		Overrides: []OverrideRule{
			{Model: "*", Explicit: Rate{InFresh: f(1), CacheRead: f(0.1), CacheWrite: f(1), Out: f(4)}}, // always active
			{Model: "*", Discount: f(0.5)}, // shadowed by the rule above: unreachable at any ts
		},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := AllPathsComplete(spec)
	if !complete {
		t.Fatalf("want complete (the incomplete path is behind an unconditional rule and can never be reached), got bad=%+v idx=%d", bad, idx)
	}
}
