// Ver 2026-08-10, by Sonnet 5
package pricing

import (
	"testing"
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

func TestResolveCanonicalKey_MapPointsToAlias(t *testing.T) {
	tbl := testTable()
	tbl.putAlias("claude-3-5-sonnet", "anthropic/claude-3-5-sonnet")
	// Target is the bare alias name instead of the canonical key
	r, ok := resolveCanonicalKey("my-plan", "my-custom-alias", tbl, map[string]string{"my-custom-alias": "claude-3-5-sonnet"})
	if !ok || *r.InFresh != 3 {
		t.Fatalf("map targeting alias failed: ok=%v r=%+v", ok, r)
	}
}

func TestResolveCanonicalKey_MapCaseInsensitive(t *testing.T) {
	// Incoming request model is uppercase, mapping key is lowercase
	r, ok := resolveCanonicalKey("my-plan", "MY-MODEL-X", testTable(), map[string]string{"my-model-x": "anthropic/claude-3-5-sonnet"})
	if !ok || *r.InFresh != 3 {
		t.Fatalf("map-explicit case-insensitive resolution failed: ok=%v r=%+v", ok, r)
	}
	// Incoming request model is lowercase, mapping key is uppercase
	r, ok = resolveCanonicalKey("my-plan", "my-model-x", testTable(), map[string]string{"MY-MODEL-X": "anthropic/claude-3-5-sonnet"})
	if !ok || *r.InFresh != 3 {
		t.Fatalf("map-explicit case-insensitive resolution failed: ok=%v r=%+v", ok, r)
	}
}

func TestResolveCanonicalKey_WhitespaceTrimmed(t *testing.T) {
	tbl := testTable()
	tbl.putAlias("claude-3-5-sonnet", "anthropic/claude-3-5-sonnet")

	// 1. Map explicit with whitespace around model and canonical key
	r, ok := resolveCanonicalKey("my-plan", "  my-model-x  ", tbl, map[string]string{" my-model-x ": " anthropic/claude-3-5-sonnet "})
	if !ok || *r.InFresh != 3 {
		t.Fatalf("map-explicit whitespace trimming failed: ok=%v r=%+v", ok, r)
	}

	// 2. Map pointing to alias with whitespace
	r, ok = resolveCanonicalKey("my-plan", "  my-alias-model  ", tbl, map[string]string{"my-alias-model": " claude-3-5-sonnet "})
	if !ok || *r.InFresh != 3 {
		t.Fatalf("map targeting alias with whitespace failed: ok=%v r=%+v", ok, r)
	}

	// 3. Bare alias with whitespace
	r, ok = resolveCanonicalKey("my-plan", "  claude-3-5-sonnet  ", tbl, nil)
	if !ok || *r.InFresh != 3 {
		t.Fatalf("alias resolution with whitespace failed: ok=%v r=%+v", ok, r)
	}

	// 4. Suffix match with whitespace
	r, ok = resolveCanonicalKey("my-plan", "  claude-3-5-sonnet  ", testTable(), nil)
	if !ok || *r.InFresh != 3 {
		t.Fatalf("suffix match with whitespace failed: ok=%v r=%+v", ok, r)
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

// TestResolve_OverrideNotFoldedIntoBase pins the fix for a real
// double-application bug caught by TestResolve_DiscountAppliesOnceToBase
// below: Resolve must NOT fold a matching override into Base, because
// EffectiveRate (the only place Base and an Override are ever combined)
// applies the first matching Override on top of Base — folding it into
// Base too would apply it twice. Base stays the pure table lookup;
// EffectiveRate is where the composed value lives.
func TestResolve_OverrideNotFoldedIntoBase(t *testing.T) {
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

func TestResolve_DiscountOverride_ComposesViaEffectiveRate(t *testing.T) {
	spec, ok := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.6)}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := EffectiveRate(spec)
	if !almostEqual(*got.InFresh, 3*0.6) {
		t.Fatalf("EffectiveRate.InFresh = %v, want %v (table rate x 0.6 discount)", *got.InFresh, 3*0.6)
	}
}

func TestResolve_ExplicitOverride_ReplacesTable(t *testing.T) {
	explicit := Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "my-model-x", Explicit: explicit}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := EffectiveRate(spec)
	if *got.InFresh != 1.58 || *got.Out != 9.54 {
		t.Fatalf("EffectiveRate = %+v, want the explicit override rate, table not involved", got)
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
	if !EffectiveRate(spec).Complete() {
		t.Fatalf("EffectiveRate = %+v, want complete", EffectiveRate(spec))
	}
}

func TestResolve_NothingMatches_OkFalse(t *testing.T) {
	_, ok := Resolve("plan-x", "totally-unknown", ResolveOptions{Table: testTable(), Currency: "USD"})
	if ok {
		t.Fatal("want ok=false: no table entry, no override")
	}
}

// TestResolve_DanglingDiscountOnly_OkFalse pins the "dangling discount"
// gate: a matching discount with NO table hit and no Explicit override has
// nothing to scale — the chain would resolve all-nil, which downstream
// consumers would read as a priced $0.00 rather than "unpriced".
func TestResolve_DanglingDiscountOnly_OkFalse(t *testing.T) {
	_, ok := Resolve("plan-x", "totally-unknown", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.8)}},
	})
	if ok {
		t.Fatal("want ok=false: a discount over an empty Base resolves nothing, not a $0.00 rate")
	}
}

// TestResolve_DanglingDiscountOverExplicitAnchor_OkTrue: the same shape
// with one Explicit rule further down the chain — now the discount has an
// anchor, Resolve succeeds, and EffectiveRate composes 0.8 x explicit.
func TestResolve_DanglingDiscountOverExplicitAnchor_OkTrue(t *testing.T) {
	explicit := Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}
	spec, ok := Resolve("plan-x", "totally-unknown", ResolveOptions{
		Table: testTable(), Currency: "USD",
		Overrides: []OverrideRule{
			{Model: "*", Discount: f(0.8)},
			{Model: "totally-unknown", Explicit: explicit},
		},
	})
	if !ok {
		t.Fatal("want ok=true: the Explicit rule anchors the chain")
	}
	got := EffectiveRate(spec)
	for name, tc := range map[string][2]*float64{
		"in_fresh": {got.InFresh, explicit.InFresh}, "cache_read": {got.CacheRead, explicit.CacheRead},
		"cache_write": {got.CacheWrite, explicit.CacheWrite}, "out": {got.Out, explicit.Out},
	} {
		if tc[0] == nil || tc[1] == nil || !almostEqual(*tc[0], *tc[1]*0.8) {
			t.Errorf("%s = %v, want %v (explicit x 0.8)", name, tc[0], tc[1])
		}
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

// --- EffectiveRate(): deterministic first-match-wins resolution ---

func TestEffectiveRate_NoOverrides_ReturnsBase(t *testing.T) {
	spec, _ := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{Table: testTable(), Currency: "USD"})
	r := EffectiveRate(spec)
	if *r.InFresh != 3 {
		t.Fatalf("EffectiveRate = %+v, want Base (InFresh=3)", r)
	}
}

// TestEffectiveRate_SpecificOverrideBeforeWildcardFallback exercises the
// still-supported composition pattern P0-A kept: a model-specific override
// listed BEFORE a wildcard catch-all resolves to the specific rate for that
// model, leaving the wildcard reachable only for every other model — see
// firstDeadOverride (internal/config/pricing.go) for the config-time guard
// against the reverse (unreachable) ordering.
func TestEffectiveRate_SpecificOverrideBeforeWildcardFallback(t *testing.T) {
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: NewTable("USD"), Currency: "USD",
		Overrides: []OverrideRule{
			{Model: "my-model-x", Explicit: Rate{InFresh: f(1.58), CacheRead: f(0.32), CacheWrite: f(1.58), Out: f(9.54)}},
			{Model: "*", Discount: f(0.6)},
		},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	got := EffectiveRate(spec)
	if *got.InFresh != 1.58 {
		t.Fatalf("EffectiveRate.InFresh = %v, want 1.58 (the specific override, not the wildcard fallback)", *got.InFresh)
	}
}

func TestEffectiveRate_NilSpec_ReturnsZeroRate(t *testing.T) {
	r := EffectiveRate(nil)
	if r.InFresh != nil || r.CacheRead != nil || r.CacheWrite != nil || r.Out != nil {
		t.Fatalf("EffectiveRate(nil) = %+v, want the zero Rate", r)
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
	got := EffectiveRate(spec)
	want := 3 * 0.6
	if !almostEqual(*got.InFresh, want) {
		t.Fatalf("InFresh = %v, want %v (table rate x 0.6, discount applied exactly once)", *got.InFresh, want)
	}
}

// TestResolve_StackedDiscounts_ComposeMultiplicatively directly exercises
// the "discount layered above another discount" case
// TestResolve_DiscountAppliesOnceToBase's comment describes but doesn't
// itself construct (that test has only one Discount-form rule; this one
// chains two). resolveChain's recursion means rule 0's Discount must scale
// "whatever rule 1 resolves to" (which is itself rule 1's Discount scaling
// Base), giving Base x 0.6 x 0.5 — NOT Base x 0.6 (rule 1 ignored) and NOT
// two independent applications straight to Base (which would also give the
// right number here by coincidence of both being simple scalars on the
// same Base, but for the wrong reason — see EffectiveRate's doc comment on
// why "the rate resolved below this rule" must be a genuine recursive
// descent, not always spec.Base directly).
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
	got := EffectiveRate(spec)
	want := 3 * 0.6 * 0.5
	if !almostEqual(*got.InFresh, want) {
		t.Fatalf("InFresh = %v, want %v (table rate x 0.6 x 0.5 — rule 0's discount composed against rule 1's resolution, not against Base directly or in isolation)", *got.InFresh, want)
	}
}

// --- Complete: the completeness gate config.validate() actually uses ---

func TestComplete_NilSpec(t *testing.T) {
	if ok, _, _ := Complete(nil); ok {
		t.Fatal("nil spec must not be considered complete")
	}
}

func TestComplete_CompleteBaseNoOverrides(t *testing.T) {
	spec, _ := Resolve("anthropic", "claude-3-5-sonnet", ResolveOptions{Table: testTable(), Currency: "USD"})
	ok, _, _ := Complete(spec)
	if !ok {
		t.Fatal("a fully-priced table entry with no overrides should be complete")
	}
}

func TestComplete_IncompleteBaseNoOverrides_Fails(t *testing.T) {
	spec, _ := Resolve("deepseek", "deepseek-chat", ResolveOptions{Table: testTable(), Currency: "USD"})
	ok, bad, idx := Complete(spec)
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

// TestComplete_OverrideFullyCoversModel_BaseIrrelevant pins the exact gap
// this function's implementation has to get right: when an override fully
// covers a model, spec.Base (which may come from an incomplete or
// nonexistent table entry) is never reached by resolveChain and must NOT
// cause a false rejection — this is the design doc's plan-e shape (an
// account override supplies pricing for a model the standard table doesn't
// even list).
func TestComplete_OverrideFullyCoversModel_BaseIrrelevant(t *testing.T) {
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD", // no table entry for my-model-x at all -> Base is empty
		Overrides: []OverrideRule{{Model: "my-model-x", Explicit: Rate{InFresh: f(1), CacheRead: f(0.1), CacheWrite: f(1), Out: f(4)}}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := Complete(spec)
	if !complete {
		t.Fatalf("want complete (the override fully covers the only reachable resolution), got bad=%+v idx=%d", bad, idx)
	}
}

// TestComplete_DiscountOverIncompleteBase_Fails: a discount override
// composes against an incomplete Base and must surface as incomplete —
// the dangerous failure direction docs/VirtualModelRouter_Design_v4_Quota.md's
// validation checklist exists to rule out (a charge on the live request path
// silently under-priced with no load-time signal).
func TestComplete_DiscountOverIncompleteBase_Fails(t *testing.T) {
	spec, ok := Resolve("deepseek", "deepseek-chat", ResolveOptions{
		Table: testTable(), Currency: "USD", // deepseek-chat's Base is missing cache_write
		Overrides: []OverrideRule{{Model: "*", Discount: f(0.5)}},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := Complete(spec)
	if complete {
		t.Fatal("a discount scaling an incomplete Base must not be considered complete")
	}
	if idx != -1 {
		t.Errorf("badIndex = %d, want -1 (the discount recurses down to Base, which is where the incompleteness lives)", idx)
	}
	if len(bad.MissingComponents()) == 0 {
		t.Error("badRate should report at least one missing component")
	}
}

// TestComplete_RuleAfterFirstMatch_Unreachable_NotChecked: first-match-wins
// means an earlier fully-matching rule shadows every later one — a later
// rule that would resolve incomplete is dead config, not a live
// under-pricing risk. Checking it anyway would reject a config whose only
// reachable path is fully priced. (internal/config's firstDeadOverride
// rejects this exact shape at load time as unreachable config; this test
// exercises the lower-level pricing-package mechanism in isolation.)
func TestComplete_RuleAfterFirstMatch_Unreachable_NotChecked(t *testing.T) {
	spec, ok := Resolve("plan-e", "my-model-x", ResolveOptions{
		Table: testTable(), Currency: "USD", // no table entry -> Base is empty/incomplete
		Overrides: []OverrideRule{
			{Model: "*", Explicit: Rate{InFresh: f(1), CacheRead: f(0.1), CacheWrite: f(1), Out: f(4)}}, // matches first
			{Model: "*", Discount: f(0.5)}, // shadowed by the rule above: unreachable
		},
	})
	if !ok {
		t.Fatal("Resolve failed")
	}
	complete, bad, idx := Complete(spec)
	if !complete {
		t.Fatalf("want complete (the incomplete path is behind an earlier matching rule and can never be reached), got bad=%+v idx=%d", bad, idx)
	}
}
