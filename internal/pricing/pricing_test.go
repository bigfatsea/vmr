// Ver 2026-08-07, by Opus 5
package pricing

import "testing"

func f(v float64) *float64 { return &v }

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

func TestTable_LookupUniqueSuffix(t *testing.T) {
	tbl := NewTable("USD")
	tbl.put("anthropic/claude-3-5-sonnet", Rate{InFresh: f(3)})
	if r, ok := tbl.LookupUniqueSuffix("claude-3-5-sonnet"); !ok || *r.InFresh != 3 {
		t.Fatalf("unique suffix match failed: ok=%v r=%+v", ok, r)
	}
	if _, ok := tbl.LookupUniqueSuffix("nonexistent-model"); ok {
		t.Fatal("no row ends with this suffix, want ok=false")
	}
}

func TestTable_LookupUniqueSuffix_AmbiguousNeverGuesses(t *testing.T) {
	tbl := NewTable("USD")
	tbl.put("vendor-a/shared-model", Rate{InFresh: f(1)})
	tbl.put("vendor-b/shared-model", Rate{InFresh: f(2)})
	if _, ok := tbl.LookupUniqueSuffix("shared-model"); ok {
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
// standard.generated.yaml actually declared — silently defeating the
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
