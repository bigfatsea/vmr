// Ver 2026-08-07, by Opus 5
package pricing

import (
	"math"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestRate_Complete_RejectsNonFiniteOrNegative(t *testing.T) {
	nan := f(math.NaN())
	neg := f(-1)
	nanRate := Rate{InFresh: f(1), CacheRead: nan, CacheWrite: f(2), Out: f(4)}
	if nanRate.Complete() {
		t.Fatal("rate with a NaN component must not be Complete (R42)")
	}
	negRate := Rate{InFresh: f(1), CacheRead: f(0), CacheWrite: f(2), Out: neg}
	if negRate.Complete() {
		t.Fatal("rate with a negative component must not be Complete (R42)")
	}
	// 0.0 is an explicitly-free price, still usable — Complete must keep it.
	zero := Rate{InFresh: f(0), CacheRead: f(0), CacheWrite: f(0), Out: f(0)}
	if !zero.Complete() {
		t.Fatal("all-zero rate must be Complete (free model)")
	}
}

func TestRate_MissingComponents_IdentifiesNonFiniteOrNegative(t *testing.T) {
	nan := f(math.NaN())
	neg := f(-1.0)
	inf := f(math.Inf(1))
	r := Rate{InFresh: nan, CacheRead: neg, CacheWrite: inf, Out: nil}
	missing := r.MissingComponents()
	want := []string{"in_fresh", "cache_read", "cache_write", "out"}
	if len(missing) != len(want) {
		t.Fatalf("MissingComponents() = %v, want %v", missing, want)
	}
	for i, name := range want {
		if missing[i] != name {
			t.Errorf("missing[%d] = %s, want %s", i, missing[i], name)
		}
	}
}

// TestParseTable_RejectsNonFiniteOrNegativeRates pins R42: a hand-written
// supplement/standard file with a NaN, Inf, or negative rate is a load-time
// hard error naming the offending key, not a silently accepted poison that
// corrupts Counters.Cost and stalls quota persistence.
func TestParseTable_RejectsNonFiniteOrNegativeRates(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp string // one YAML component literal inside a rates row
	}{
		{"nan", "in_fresh: .nan"},
		{"inf", "cache_read: .inf"},
		{"neg-inf", "out: -.inf"},
		{"negative", "cache_write: -5.0"},
	} {
		data := []byte("currency: USD\nrates:\n  - {key: vendor/broken-model, " + tc.comp + "}\n")
		if _, err := ParseTable(data); err == nil {
			t.Errorf("%s: ParseTable accepted %s, want a load-time error", tc.name, tc.comp)
			continue
		} else if !strings.Contains(err.Error(), "vendor/broken-model") {
			t.Errorf("%s: error must name the offending key, got: %v", tc.name, err)
		}
	}
}

// TestParseTable_ZeroComponentsStillAllowed pins the boundary the R42 gate
// must NOT cross: a legitimately free model (all four components explicitly
// 0.0) and a partial row with a zero component must keep loading.
func TestParseTable_ZeroComponentsStillAllowed(t *testing.T) {
	data := []byte(`currency: USD
rates:
  - {key: free/model, in_fresh: 0, cache_read: 0, cache_write: 0, out: 0}
  - {key: partial/model, in_fresh: 0}
`)
	tbl, err := ParseTable(data)
	if err != nil {
		t.Fatalf("ParseTable rejected a zero-valued table: %v", err)
	}
	if r, ok := tbl.Lookup("free/model"); !ok || !r.Complete() {
		t.Fatalf("all-zero rate must load and stay Complete: ok=%v r=%+v", ok, r)
	}
}

func TestRate_Complete(t *testing.T) {
	full := Rate{InFresh: f(1), CacheRead: f(0), CacheWrite: f(2), Out: f(4)}
	if !full.Complete() {
		t.Fatalf("Complete() = false for a fully-set rate (0.0 counts as set), missing=%v", full.MissingComponents())
	}
	partial := Rate{InFresh: f(1), Out: f(4)}
	if partial.Complete() {
		t.Fatal("Complete() = true, want false (cache_read/cache_write unset)")
	}
	if got := partial.MissingComponents(); len(got) != 2 || got[0] != "cache_read" || got[1] != "cache_write" {
		t.Fatalf("MissingComponents() = %v, want [cache_read cache_write]", got)
	}
}

func TestRate_Scale(t *testing.T) {
	r := Rate{InFresh: f(1), Out: f(4)} // CacheRead/CacheWrite deliberately unset
	scaled := r.Scale(2.5)
	if *scaled.InFresh != 2.5 || *scaled.Out != 10 {
		t.Fatalf("Scale = %+v, want InFresh=2.5 Out=10", scaled)
	}
	if scaled.CacheRead != nil || scaled.CacheWrite != nil {
		t.Fatalf("Scale must not manufacture a value for an unset component, got %+v", scaled)
	}
}

func TestTable_LookupCaseInsensitive(t *testing.T) {
	tbl := NewTable("USD")
	tbl.put("Anthropic/Claude-3-5-Sonnet", Rate{InFresh: f(3)})
	if _, ok := tbl.Lookup("anthropic/claude-3-5-sonnet"); !ok {
		t.Fatal("Lookup should be case-insensitive")
	}
}

func TestTable_LookupPreferredSuffix(t *testing.T) {
	tbl := NewTable("USD")
	tbl.put("anthropic/claude-3-5-sonnet", Rate{InFresh: f(3)})
	if r, ok := tbl.LookupPreferredSuffix("claude-3-5-sonnet"); !ok || *r.InFresh != 3 {
		t.Fatalf("unique suffix match failed: ok=%v r=%+v", ok, r)
	}
	if _, ok := tbl.LookupPreferredSuffix("nonexistent-model"); ok {
		t.Fatal("no row ends with this suffix, want ok=false")
	}
}

func TestTable_LookupPreferredSuffix_AmbiguousNeverGuesses(t *testing.T) {
	tbl := NewTable("USD")
	tbl.put("vendor-a/shared-model", Rate{InFresh: f(1)})
	tbl.put("vendor-b/shared-model", Rate{InFresh: f(2)})
	if _, ok := tbl.LookupPreferredSuffix("shared-model"); ok {
		t.Fatal("two rows share this suffix — must refuse to guess, want ok=false")
	}
}

func TestMerge_WholeRowOverlay_NotPerComponent(t *testing.T) {
	base := NewTable("USD")
	base.put("v/m", Rate{InFresh: f(1), CacheRead: f(0.1), CacheWrite: f(1.25), Out: f(4)})
	overlay := NewTable("USD")
	// overlay only sets in_fresh — under whole-row semantics this REPLACES
	// base's row entirely, it does not inherit base's other 3 components.
	overlay.put("v/m", Rate{InFresh: f(99)})

	merged := Merge(base, overlay)
	r, ok := merged.Lookup("v/m")
	if !ok {
		t.Fatal("merged table missing v/m")
	}
	if *r.InFresh != 99 {
		t.Fatalf("InFresh = %v, want 99 (overlay wins)", *r.InFresh)
	}
	if r.CacheRead != nil || r.CacheWrite != nil || r.Out != nil {
		t.Fatalf("whole-row overlay must not inherit base's other components, got %+v", r)
	}
}

func TestMerge_DisjointKeysBothSurvive(t *testing.T) {
	base := NewTable("USD")
	base.put("a/m", Rate{InFresh: f(1)})
	overlay := NewTable("USD")
	overlay.put("b/m", Rate{InFresh: f(2)})
	merged := Merge(base, overlay)
	if _, ok := merged.Lookup("a/m"); !ok {
		t.Error("base-only key a/m missing from merge")
	}
	if _, ok := merged.Lookup("b/m"); !ok {
		t.Error("overlay-only key b/m missing from merge")
	}
}

// TestMerge_PreservesBaseGeneratedAt pins a real bug caught during e2e
// testing: Merge used to build its output via NewTable(base.Currency)
// without ever copying base.GeneratedAt, so LoadStandard()'s merged table
// always reported an empty generation date regardless of what
// standard_price_generated.yaml actually declared — silently defeating the
// "is this table stale" signal design doc §4.2③ requires (vmr report's §2
// appendix, vmr check's staleness display).
func TestMerge_PreservesBaseGeneratedAt(t *testing.T) {
	base := NewTable("USD")
	base.GeneratedAt = "2026-08-07"
	overlay := NewTable("USD")
	merged := Merge(base, overlay)
	if merged.GeneratedAt != "2026-08-07" {
		t.Fatalf("GeneratedAt = %q, want %q (base's generation date must survive the merge)", merged.GeneratedAt, "2026-08-07")
	}
}

func TestMerge_NilBase_NoPanic(t *testing.T) {
	overlay := NewTable("USD")
	overlay.put("a/m", Rate{InFresh: f(1)})
	merged := Merge(nil, overlay) // must not panic dereferencing a nil base
	if _, ok := merged.Lookup("a/m"); !ok {
		t.Error("overlay key missing when base is nil")
	}
}

func TestParseTable_MissingVsExplicitZero(t *testing.T) {
	data := []byte(`currency: USD
generated_at: "2026-08-07"
rates:
  - key: v/m
    in_fresh: 1.5
    cache_read: 0
    out: 4
`)
	tbl, err := ParseTable(data)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := tbl.Lookup("v/m")
	if !ok {
		t.Fatal("v/m not found")
	}
	if r.CacheRead == nil || *r.CacheRead != 0 {
		t.Fatalf("cache_read: explicit 0 must decode to a non-nil pointer at 0.0, got %v", r.CacheRead)
	}
	if r.CacheWrite != nil {
		t.Fatalf("cache_write: omitted field must decode to nil, got %v", *r.CacheWrite)
	}
}

func TestParseTable_EmptyOrCommentOnly_IsValidEmptyTable(t *testing.T) {
	for _, data := range [][]byte{nil, []byte(""), []byte("  \n"), []byte("# just a comment\n")} {
		tbl, err := ParseTable(data)
		if err != nil {
			t.Fatalf("ParseTable(%q): unexpected error: %v", data, err)
		}
		if len(tbl.order) != 0 {
			t.Fatalf("ParseTable(%q): want an empty table, got %d rows", data, len(tbl.order))
		}
	}
}

func TestParseTable_RejectsNonUSDCurrency(t *testing.T) {
	_, err := ParseTable([]byte("currency: CNY\nrates: []\n"))
	if err == nil {
		t.Fatal("want an error for a non-USD standard/curated/supplement table")
	}
}

func TestParseTable_DuplicateKeyRejected(t *testing.T) {
	data := []byte(`currency: USD
rates:
  - {key: v/m, in_fresh: 1}
  - {key: V/M, in_fresh: 2}
`)
	if _, err := ParseTable(data); err == nil {
		t.Fatal("want an error for a duplicate key (case-insensitive)")
	}
}

func TestParseTable_UnknownFieldRejected(t *testing.T) {
	data := []byte("currency: USD\nrates: []\nbogus_field: 1\n")
	if _, err := ParseTable(data); err == nil {
		t.Fatal("want a strict-YAML unknown-field rejection")
	}
}

func TestLoadStandard_EmbeddedTablesParseAndMerge(t *testing.T) {
	tbl, err := LoadStandard()
	if err != nil {
		t.Fatalf("LoadStandard: %v", err)
	}
	if len(tbl.order) == 0 {
		t.Fatal("LoadStandard produced an empty table — generator output missing?")
	}
	// A handful of well-known entries the generator should always produce
	// from the primary-vendor allowlist.
	for _, key := range []string{"openai/gpt-4o", "deepseek/deepseek-chat"} {
		if r, ok := tbl.Lookup(key); !ok {
			t.Errorf("standard table missing expected entry %q", key)
		} else if r.InFresh == nil {
			t.Errorf("%q: in_fresh unexpectedly nil", key)
		}
	}
}

// --- FactorBetween ---

func TestFactorBetween_USDIdentity(t *testing.T) {
	if f, ok := FactorBetween("USD", "USD", nil); !ok || f != 1 {
		t.Fatalf("FactorBetween(USD, USD, nil) = (%v, %v), want (1, true)", f, ok)
	}
	if f, ok := FactorBetween("", "usd", nil); !ok || f != 1 {
		t.Fatalf("FactorBetween(\"\", usd, nil) = (%v, %v), want (1, true) — empty and lowercase both mean USD", f, ok)
	}
}

func TestFactorBetween_DirectPivot(t *testing.T) {
	rates := map[string]float64{"CNY": 7.1}
	f, ok := FactorBetween("USD", "CNY", rates)
	if !ok || f != 7.1 {
		t.Fatalf("FactorBetween(USD, CNY) = (%v, %v), want (7.1, true)", f, ok)
	}
	f, ok = FactorBetween("CNY", "USD", rates)
	if !ok {
		t.Fatalf("FactorBetween(CNY, USD) ok = false, want true")
	}
	if diff := f - 1/7.1; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("FactorBetween(CNY, USD) = %v, want %v (1/7.1, the inverse direction)", f, 1/7.1)
	}
}

func TestFactorBetween_CrossNonUSDPair_ViaUSDPivot(t *testing.T) {
	// CNY -> JPY with no direct rate between them — must resolve via the
	// implicit USD pivot (1 USD = 7.1 CNY = 155 JPY -> 1 CNY = 155/7.1 JPY).
	rates := map[string]float64{"CNY": 7.1, "JPY": 155}
	f, ok := FactorBetween("CNY", "JPY", rates)
	want := 155.0 / 7.1
	if !ok || f < want-1e-9 || f > want+1e-9 {
		t.Fatalf("FactorBetween(CNY, JPY) = (%v, %v), want (%v, true)", f, ok, want)
	}
}

func TestFactorBetween_MissingOrInvalidRate_Rejected(t *testing.T) {
	for _, rates := range []map[string]float64{
		nil,
		{},
		{"CNY": 0},
		{"CNY": -1},
		{"CNY": math.NaN()},
		{"CNY": math.Inf(1)},
	} {
		if _, ok := FactorBetween("USD", "CNY", rates); ok {
			t.Errorf("FactorBetween(USD, CNY, %v) = ok, want rejected", rates)
		}
	}
}

// --- ParseTableWithRates ---

func TestParseTableWithRates_RowLevelCurrency_ConvertsToUSD(t *testing.T) {
	data := []byte(`currency: USD
rates:
  - {key: domestic/model-a, currency: CNY, in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
`)
	tbl, err := ParseTableWithRates(data, map[string]float64{"CNY": 7.1})
	if err != nil {
		t.Fatalf("ParseTableWithRates: %v", err)
	}
	r, ok := tbl.Lookup("domestic/model-a")
	if !ok {
		t.Fatal("row not found")
	}
	if got := *r.InFresh; got < 1-1e-9 || got > 1+1e-9 {
		t.Fatalf("InFresh = %v, want 1.0 (7.1 CNY / 7.1)", got)
	}
	if got := *r.Out; got < 4-1e-9 || got > 4+1e-9 {
		t.Fatalf("Out = %v, want 4.0 (28.4 CNY / 7.1)", got)
	}
	if tbl.Currency != "USD" {
		t.Fatalf("Table.Currency = %q, want USD — the in-memory Table must always be USD regardless of source rows", tbl.Currency)
	}
}

func TestParseTableWithRates_TableLevelDefaultCurrency(t *testing.T) {
	data := []byte(`currency: CNY
rates:
  - {key: domestic/model-a, in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
  - {key: foreign/model-b, currency: USD, in_fresh: 1, cache_read: 0.1, cache_write: 1.25, out: 4}
`)
	tbl, err := ParseTableWithRates(data, map[string]float64{"CNY": 7.1})
	if err != nil {
		t.Fatalf("ParseTableWithRates: %v", err)
	}
	a, _ := tbl.Lookup("domestic/model-a")
	if got := *a.InFresh; got < 1-1e-9 || got > 1+1e-9 {
		t.Fatalf("model-a InFresh = %v, want 1.0 (inherits table-level currency: CNY)", got)
	}
	b, _ := tbl.Lookup("foreign/model-b")
	if got := *b.InFresh; got != 1 {
		t.Fatalf("model-b InFresh = %v, want 1.0 unchanged (row's own currency: USD overrides the table default)", got)
	}
}

func TestParseTableWithRates_MissingRate_Rejected(t *testing.T) {
	data := []byte(`currency: USD
rates:
  - {key: domestic/model-a, currency: CNY, in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
`)
	if _, err := ParseTableWithRates(data, nil); err == nil {
		t.Fatal("want an error: row declares currency CNY but no rates map was given to convert it")
	}
}

func TestParseTableWithRates_FileOwnExchangeRate_SelfContained(t *testing.T) {
	// No external rates passed at all — the file's own exchange_rate:
	// block must be enough on its own, proving a supplement/standard file
	// can be fully portable across deployments.
	data := []byte(`currency: USD
exchange_rate: {CNY: 7.1}
rates:
  - {key: domestic/model-a, currency: CNY, in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
`)
	tbl, err := ParseTableWithRates(data, nil)
	if err != nil {
		t.Fatalf("ParseTableWithRates (no external rates): %v", err)
	}
	r, ok := tbl.Lookup("domestic/model-a")
	if !ok {
		t.Fatal("row not found")
	}
	if got := *r.InFresh; got < 1-1e-9 || got > 1+1e-9 {
		t.Fatalf("InFresh = %v, want 1.0 (7.1 CNY / the file's own 7.1 rate)", got)
	}
}

func TestParseTableWithRates_FileOwnExchangeRate_WinsOverExternal(t *testing.T) {
	// The file's own exchange_rate: block must win over a conflicting
	// external (config.yaml) rate on the same currency code — a
	// self-declared rate is a deliberate pin, not a mere suggestion.
	data := []byte(`currency: USD
exchange_rate: {CNY: 7.1}
rates:
  - {key: domestic/model-a, currency: CNY, in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
`)
	tbl, err := ParseTableWithRates(data, map[string]float64{"CNY": 999}) // wildly different external rate
	if err != nil {
		t.Fatalf("ParseTableWithRates: %v", err)
	}
	r, _ := tbl.Lookup("domestic/model-a")
	if got := *r.InFresh; got < 1-1e-9 || got > 1+1e-9 {
		t.Fatalf("InFresh = %v, want 1.0 (the file's own 7.1 rate must win over the external 999)", got)
	}
}

func TestParseTableWithRates_FileExchangeRate_FallsBackToExternalForOtherCurrencies(t *testing.T) {
	// The file declares its own rate for CNY only; a JPY row must still
	// fall back to the externally-supplied rates map.
	data := []byte(`currency: USD
exchange_rate: {CNY: 7.1}
rates:
  - {key: domestic/model-a, currency: CNY, in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
  - {key: jp-vendor/model-b, currency: JPY, in_fresh: 155, cache_read: 15.5, cache_write: 193.75, out: 620}
`)
	tbl, err := ParseTableWithRates(data, map[string]float64{"JPY": 155})
	if err != nil {
		t.Fatalf("ParseTableWithRates: %v", err)
	}
	r, ok := tbl.Lookup("jp-vendor/model-b")
	if !ok {
		t.Fatal("row not found")
	}
	if got := *r.InFresh; got != 1 {
		t.Fatalf("InFresh = %v, want 1.0 (155 JPY / the external 155 rate)", got)
	}
}

func TestParseTableWithRates_NilRates_BehavesLikePlainParseTable(t *testing.T) {
	data := []byte("currency: USD\nrates: []\n")
	tbl, err := ParseTableWithRates(data, nil)
	if err != nil {
		t.Fatalf("ParseTableWithRates(nil rates, all-USD data): unexpected error: %v", err)
	}
	if tbl.Currency != "USD" {
		t.Fatalf("Table.Currency = %q, want USD", tbl.Currency)
	}
}
