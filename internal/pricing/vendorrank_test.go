// Ver 2026-08-31, by Opus 5

package pricing

import (
	"strings"
	"testing"
)

func rankTable(t *testing.T, body string) *Table {
	t.Helper()
	tbl, err := ParseTable([]byte("currency: USD\ngenerated_at: \"2026-08-31\"\n" + body))
	if err != nil {
		t.Fatalf("ParseTable: %v", err)
	}
	return tbl
}

// A bare model name carried by one first party and any number of resellers
// resolves to the first party — the case that made ~40 real models priceable
// through provider names that are not vendor names.
func TestLookupPreferredSuffix_FirstPartyBeatsResellers(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: openrouter/m
    in_fresh: 99
  - key: deepseek/m
    in_fresh: 1
  - key: fireworks_ai/m
    in_fresh: 50
`)
	r, ok := tbl.LookupPreferredSuffix("m")
	if !ok {
		t.Fatal("want the single first-party row to win, got no rate")
	}
	if *r.InFresh != 1 {
		t.Errorf("in_fresh = %v, want 1 (deepseek's, not a reseller's)", *r.InFresh)
	}
}

// Two resellers quoting someone else's model have no canonical answer
// between them — still "no rate", never a guess.
func TestLookupPreferredSuffix_ResellerTieStaysUnresolved(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: openrouter/m
    in_fresh: 99
  - key: groq/m
    in_fresh: 50
`)
	if _, ok := tbl.LookupPreferredSuffix("m"); ok {
		t.Error("two resellers disagreeing must not resolve — a wrong rate is worse than none")
	}
}

// Two NON-reseller vendors is the case vendor precedence deliberately cannot
// decide (a platform reselling another first party). That is what aliases
// are for; precedence itself must not pick one.
func TestLookupPreferredSuffix_TwoFirstPartiesStayUnresolved(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: dashscope/m
    in_fresh: 9
  - key: deepseek/m
    in_fresh: 1
`)
	if _, ok := tbl.LookupPreferredSuffix("m"); ok {
		t.Error("two first parties must not be silently ranked against each other")
	}
}

// A lone reseller row still wins — this rule only ever widens what resolves,
// it never changes what an unambiguous name resolved to before.
func TestLookupPreferredSuffix_LoneResellerStillWins(t *testing.T) {
	tbl := rankTable(t, `rates:
  - key: openrouter/m
    in_fresh: 7
`)
	r, ok := tbl.LookupPreferredSuffix("m")
	if !ok || *r.InFresh != 7 {
		t.Errorf("lone reseller row must still resolve, got ok=%v rate=%+v", ok, r)
	}
}

// The alias hop sits inside step ③, so it beats the suffix scan (step ④)
// but never a per-provider answer (steps ①/②).
func TestResolve_AliasBeatsSuffixScanButNotProviderPrefix(t *testing.T) {
	tbl := rankTable(t, `aliases:
  m: zhipu/m
rates:
  - key: zhipu/m
    in_fresh: 1
  - key: dashscope/m
    in_fresh: 9
  - key: myproxy/m
    in_fresh: 42
`)
	if err := tbl.ValidateAliases(); err != nil {
		t.Fatalf("ValidateAliases: %v", err)
	}
	// Bare name via a provider that is nothing in the table: the alias decides
	// what two first parties could not.
	r, ok := resolveCanonicalKey("some-plan", "m", tbl, nil)
	if !ok || *r.InFresh != 1 {
		t.Errorf("alias must resolve to zhipu/m, got ok=%v rate=%+v", ok, r)
	}
	// A provider that IS a table prefix has the more specific answer; the
	// global alias must not override it.
	r, ok = resolveCanonicalKey("myproxy", "m", tbl, nil)
	if !ok || *r.InFresh != 42 {
		t.Errorf("<provider>/<model> must win over a global alias, got ok=%v rate=%+v", ok, r)
	}
}

func TestValidateAliases_RejectsDanglingAndChained(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{
			name: "dangling",
			body: "aliases:\n  m: nowhere/m\nrates:\n  - key: zhipu/m\n    in_fresh: 1\n",
			want: "no such key",
		},
		{
			// A chain is the only way to build a cycle, so banning it
			// outright is what makes the one-hop rule safe by construction.
			name: "chained",
			body: "aliases:\n  a: b\n  b: zhipu/m\nrates:\n  - key: zhipu/m\n    in_fresh: 1\n",
			want: "itself an alias",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rankTable(t, tc.body).ValidateAliases()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// A supplement can retarget an alias the curated table shipped, the same way
// it can replace a rate row.
func TestMerge_OverlayAliasWins(t *testing.T) {
	base := rankTable(t, "aliases:\n  m: zhipu/m\nrates:\n  - key: zhipu/m\n    in_fresh: 1\n")
	overlay := rankTable(t, "aliases:\n  m: mine/m\nrates:\n  - key: mine/m\n    in_fresh: 5\n")
	merged := Merge(base, overlay)
	if err := merged.ValidateAliases(); err != nil {
		t.Fatalf("ValidateAliases: %v", err)
	}
	r, ok := resolveCanonicalKey("whatever", "m", merged, nil)
	if !ok || *r.InFresh != 5 {
		t.Errorf("overlay alias must win, got ok=%v rate=%+v", ok, r)
	}
}
