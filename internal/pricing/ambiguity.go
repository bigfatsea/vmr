// Ver 2026-08-31, by Opus 5

// Vendor-precedence transparency: which bare model names are carried by
// more than one vendor, and how each one currently resolves.
//
// This exists because precedence's failure mode is SILENCE. An alias whose
// target disappears is a load-time error; precedence flipping — a refresh
// adds a second first-party row and a name that used to resolve stops
// resolving — produces no error at all, just a price that quietly leaves
// the report. The 2026-08-31 refresh did exactly that to four models this
// repo routes (dashscope started reselling DeepSeek/Zhipu/Moonshot), which
// is what motivated both this report and the much longer alias list in
// standard_price_curated.yaml.
//
// tools/gen_standard_pricing prints this after every refresh, so a flip is
// visible at the moment it is introduced rather than months later in a
// report nobody cross-checks. It shares Table's own data and
// LookupPreferredSuffix's own rule — a report that re-derived either could
// disagree with what actually resolves, which is the whole bug class this
// package keeps fighting.
package pricing

import (
	"sort"
	"strings"
)

// SuffixAmbiguity is one bare model name carried by more than one vendor.
type SuffixAmbiguity struct {
	// Model is the bare (vendor-prefix-free) name.
	Model string
	// Vendors is every vendor prefix carrying it, sorted.
	Vendors []string
	// Alias is the canonical key an alias pins this name to, or "" when
	// nothing pins it and resolution is left to vendor precedence.
	Alias string
	// Winner is the canonical key vendor precedence picks, or "" when it
	// declines to (several candidates at the top rank). Reported even when
	// Alias is set, so a mismatch between what is pinned and what precedence
	// would have chosen is visible.
	Winner string
}

// Pinned reports whether an alias decides this name, making it immune to a
// snapshot refresh adding another vendor.
func (s SuffixAmbiguity) Pinned() bool { return s.Alias != "" }

// Ambiguities lists every bare model name in t carried by more than one
// vendor, sorted by name. Reports both what pins it and what precedence
// would pick, so a refresh diff shows exactly which names changed hands and
// which stopped resolving.
func (t *Table) Ambiguities() []SuffixAmbiguity {
	if t == nil {
		return nil
	}
	byModel := map[string][]string{}
	for _, k := range t.order {
		if !strings.Contains(k, "/") {
			continue // a bare key has no vendor to be ambiguous against
		}
		byModel[ModelBasename(k)] = append(byModel[ModelBasename(k)], k)
	}
	var out []SuffixAmbiguity
	for model, keys := range byModel {
		if len(keys) < 2 {
			continue
		}
		a := SuffixAmbiguity{Model: model, Alias: t.aliases[model]}
		var firstParty []string
		for _, k := range keys {
			a.Vendors = append(a.Vendors, vendorOf(k))
			if !aggregatorVendors[vendorOf(k)] {
				firstParty = append(firstParty, k)
			}
		}
		// len(keys) >= 2 is guaranteed here (see the continue above), so the
		// only resolution worth reporting is the single-first-party case
		// precedence actually decides; the "len(keys) == 1" branch
		// LookupPreferredSuffix needs (its "all" may be a single match) is
		// unreachable in this enumeration and was dropped.
		if len(firstParty) == 1 {
			a.Winner = firstParty[0]
		}
		sort.Strings(a.Vendors)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}
