// Ver 2026-09-01, by Opus 5

package pricing

import (
	"strings"
	"testing"
)

// --- ModelBasename: the one definition of a "bare model name" ---

func TestModelBasename(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"gemma-4-31b-it", "gemma-4-31b-it"},                                 // bare stays bare
		{"google/gemma-4-31b-it", "gemma-4-31b-it"},                          // org prefix stripped
		{"accounts/fireworks/models/deepseek-v4-flash", "deepseek-v4-flash"}, // multi-segment path: keep the LAST segment
		{"@cf/zai-org/glm-5.2", "glm-5.2"},                                   // exotic org chars
		{"Gemma/4-31B-IT", "4-31B-IT"},                                       // case preserved (lookups fold case, the generator does)
		{"", ""},                                                             // empty in, empty out
		{"/", ""},
	} {
		if got := ModelBasename(tc.in); got != tc.want {
			t.Errorf("ModelBasename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- resolveCanonicalKey's org-prefix fallback ---

// A request name carrying the org prefix its aggregator API forces
// ("google/gemma-4-31b-it" on together) must resolve to exactly the rate the
// bare name resolves to — the fallback only lowers the name into the table's
// org-stripped key space; vendor precedence makes the same pick it always
// made for the bare name.
func TestResolveCanonicalKey_OrgPrefixMatchesBareName(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: gemini/gemma-4-31b-it
    in_fresh: 1
  - key: together_ai/gemma-4-31b-it
    in_fresh: 99
`)
	bare, ok := resolveCanonicalKey("together_ai", "gemma-4-31b-it", tbl, nil)
	if !ok {
		t.Fatal("bare-name resolution failed (fixture wrong?)")
	}
	for _, m := range []string{"google/gemma-4-31b-it", "pearl-ai/gemma-4-31b-it"} {
		r, ok := resolveCanonicalKey("together_ai", m, tbl, nil)
		if !ok {
			t.Errorf("%q: org-prefixed name must resolve via the basename fallback, got no rate", m)
			continue
		}
		if *r.InFresh != *bare.InFresh {
			t.Errorf("%q resolved to in_fresh=%v, want %v (the bare name's answer — same vendor-precedence decision)", m, *r.InFresh, *bare.InFresh)
		}
	}
}

// The fallback must not change any answer a raw step already gave: a name
// that IS a table key hits at step ③ before the fallback ever runs, and an
// account's pricing.map (step ①, matched on the RAW name) still wins.
func TestResolveCanonicalKey_OrgPrefixFallbackWidensOnly(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: gemini/gemma-4-31b-it
    in_fresh: 1
  - key: together_ai/gemma-4-31b-it
    in_fresh: 99
`)
	// "gemini/gemma-4-31b-it" is itself a key: step ③ answers directly.
	r, ok := resolveCanonicalKey("together_ai", "gemini/gemma-4-31b-it", tbl, nil)
	if !ok || *r.InFresh != 1 {
		t.Errorf("exact-key request must resolve at the raw steps, got ok=%v in_fresh=%v", ok, derefInFresh(r))
	}
	// A map entry keyed on the RAW org-prefixed name still wins over the fallback.
	mapped := map[string]string{"google/gemma-4-31b-it": "together_ai/gemma-4-31b-it"}
	r, ok = resolveCanonicalKey("together_ai", "google/gemma-4-31b-it", tbl, mapped)
	if !ok || *r.InFresh != 99 {
		t.Errorf("map entry on the raw name must win, got ok=%v in_fresh=%v", ok, derefInFresh(r))
	}
}

func derefInFresh(r Rate) float64 {
	if r.InFresh == nil {
		return -1
	}
	return *r.InFresh
}

// The fallback re-runs the alias hop on the basename: an alias pinning the
// bare name decides an org-prefixed request the same way it decides the bare
// name — aliases are the escape hatch when precedence can't, and the fallback
// must route org-prefixed traffic through it rather than bypassing it.
func TestResolveCanonicalKey_OrgPrefixGoesThroughAlias(t *testing.T) {
	tbl := rankTable(t, `aliases:
  glm-5.2: zai-org/glm-5.2
rates:
  - key: zai-org/glm-5.2
    in_fresh: 1
  - key: dashscope/glm-5.2
    in_fresh: 9
`)
	if err := tbl.ValidateAliases(); err != nil {
		t.Fatalf("ValidateAliases: %v", err)
	}
	r, ok := resolveCanonicalKey("some-plan", "cloudflare/@cf/zai-org/glm-5.2", tbl, nil)
	if !ok || *r.InFresh != 1 {
		t.Errorf("org-prefixed name must resolve through the alias on its basename, got ok=%v in_fresh=%v", ok, derefInFresh(r))
	}
}

// A name whose basename is also genuinely absent stays unresolved — the
// fallback widens coverage, it never invents a rate for a real data gap.
func TestResolveCanonicalKey_OrgPrefixStillUnpricedOnRealGap(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: together_ai/llama-3.3-70b-instruct-turbo
    in_fresh: 1
`)
	if _, ok := resolveCanonicalKey("openrouter", "meta-llama/llama-3.3-70b-instruct", tbl, nil); ok {
		t.Error("basename has no row — must stay unresolved, not guess at the -turbo variant")
	}
}

// --- the embedded standard table: parity on real market data ---

// TestResolveCanonicalKey_OrgPrefixParityOnStandardTable is the differential
// pin for the real bug this batch fixes: against the EMBEDDED table, an
// org-prefixed request name must now resolve to the byte-identical rate the
// bare name always resolved to — first-party vendor precedence included.
// Model fixtures are picked from the live snapshot shape (gemma is carried
// by its maker gemini plus resellers under several org spellings).
func TestResolveCanonicalKey_OrgPrefixParityOnStandardTable(t *testing.T) {
	tbl, err := LoadStandard()
	if err != nil {
		t.Fatalf("LoadStandard: %v", err)
	}
	if _, ok := tbl.Lookup("gemini/gemma-4-31b-it"); !ok {
		t.Skip("gemma-4-31b-it not in the current embedded table — pick a new fixture at the next refresh")
	}
	bare, ok := resolveCanonicalKey("together_ai", "gemma-4-31b-it", tbl, nil)
	if !ok {
		t.Fatal("bare gemma-4-31b-it must resolve in the embedded table (fixture wrong?)")
	}
	for _, m := range []string{"google/gemma-4-31b-it", "pearl-ai/gemma-4-31b-it"} {
		r, ok := resolveCanonicalKey("together_ai", m, tbl, nil)
		if !ok {
			t.Errorf("%q: still unpriced against the embedded table — the org-prefix fallback is not reaching it", m)
			continue
		}
		if (r.InFresh == nil) != (bare.InFresh == nil) || (r.InFresh != nil && *r.InFresh != *bare.InFresh) ||
			(r.Out != nil && bare.Out != nil && *r.Out != *bare.Out) {
			t.Errorf("%q resolved to %+v, want the bare name's %+v", m, r, bare)
		}
	}
}

// --- ParseTable's two-segment key invariant ---

func TestParseTable_RejectsDeepKey(t *testing.T) {
	for _, key := range []string{
		"openrouter/meta-llama/llama-3.3-70b-instruct",
		"fireworks_ai/accounts/fireworks/models/deepseek-v4-flash",
	} {
		data := []byte("currency: USD\nrates:\n  - key: " + key + "\n    in_fresh: 1\n")
		_, err := ParseTable(data)
		if err == nil || !strings.Contains(err.Error(), "vendor/basename") {
			t.Errorf("key %q: want a two-segment rejection naming the rule, got %v", key, err)
		}
	}
	// Both legal shapes still load: a plain vendor/basename key and a bare one.
	if _, err := ParseTable([]byte("currency: USD\nrates:\n  - key: gemini/m\n    in_fresh: 1\n  - key: m2\n    in_fresh: 2\n")); err != nil {
		t.Errorf("two-segment and bare keys must both still load, got %v", err)
	}
}
