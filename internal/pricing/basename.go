// Ver 2026-09-01, by Opus 5

package pricing

import "strings"

// ModelBasename returns the canonical bare name of an upstream model name:
// the segment after the LAST "/", unchanged otherwise ("google/gemma-4-31b-it"
// -> "gemma-4-31b-it", "accounts/fireworks/models/deepseek-v4-flash" ->
// "deepseek-v4-flash", "gemma-4-31b-it" -> "gemma-4-31b-it").
//
// Single source of truth for what a "bare model name" is: the table builder
// (tools/gen_standard_pricing) strips it from canonical keys — org/path
// prefixes are aggregator API naming noise, not model identity, and stripping
// them is what makes dedup, refresh stability, and vendor-precedence
// comparison work at all — and the resolver (resolveCanonicalKey) reduces
// request-side names with it after the raw four steps miss, so an
// org-prefixed request (openrouter's "meta-llama/...", together's
// "google/gemma-...") lands on the same rate the bare name resolves to. One
// definition, both sides, no drift — a second private copy is the bug class
// this function exists to close.
//
// Does NOT case-fold (Table lookups are case-insensitive already); case is
// the generator's concern when it assembles a lowercase canonical key.
func ModelBasename(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		return model[i+1:]
	}
	return model
}
