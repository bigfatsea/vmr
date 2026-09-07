// Ver 2026-09-06, by Sonnet 5

// Pricing display for `vmr check` — split out of cmd_check.go when the file
// crossed its archtest line budget: the pricing block (standard-table
// freshness, exchange-rate provenance, per-provider declared rates) is one
// cohesive concern that only printProviders and printGlobalSettings consume.
package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"vmr/internal/config"
	"vmr/internal/fmtutil"
	"vmr/internal/pricing"
)

// pricingStaleAfter is how old the built-in standard price table may get
// before `vmr check` says so out loud. Two months, not the six it used to
// be: the 2026-08-31 refresh showed a 20-day-old snapshot already missing
// list prices for four models this repo's own traffic was running on
// (gemini-3.7-flash, deepseek-v4-flash-vision-exp, kimi-k3, and a renamed
// glm row). A threshold that only fires after the table has been wrong for
// half a year is not a guardrail. Refresh is one command with no arguments
// (`go run ./tools/gen_standard_pricing -generated-at <today>` fetches
// upstream itself), so the reminder is cheap to act on.
const pricingStaleAfter = 60 * 24 * time.Hour

// pricingTableLine describes the standard price table backing this config's
// $ figures — its generation date, and whether that date is old enough to
// deserve a refresh. ok=false only when the embedded table itself somehow
// failed to load (PricingTable's error path) — otherwise the table is
// always present, since it no longer depends on anything the config
// declares (the two-layer model has no "config touches no pricing at
// all" case anymore).
func pricingTableLine(cfg *config.Config) (string, bool) {
	table, err := cfg.PricingTable()
	if err != nil || table == nil {
		return "", false
	}
	line := "built-in standard table (generation date unknown)"
	if table.GeneratedAt != "" {
		line = "built-in standard table generated " + table.GeneratedAt
		gen, perr := time.ParseInLocation("2006-01-02", table.GeneratedAt, fmtutil.DisplayZone)
		if perr == nil {
			if age := time.Since(gen); age > pricingStaleAfter {
				line += fmt.Sprintf(" — %d days old, list prices may have moved (regenerate: go run ./tools/gen_standard_pricing -generated-at $(date +%%F))", int(age.Hours()/24))
			}
		}
	}
	// Aliases silently redirect a bare model name to another vendor's row
	// (see internal/pricing.Table.aliases). That is exactly the kind of
	// resolution an operator should be able to see is in effect before
	// trusting a $ column, so their count is stated even though the
	// individual mappings are not.
	if n := len(table.Aliases()); n > 0 {
		line += fmt.Sprintf("; %d model alias(es)", n)
	}
	return line, true
}

// exchangeRateLine summarizes every currency actually in use across this
// config — the top-level exchange_rate: block's own keys, plus every
// provider's pricing.currency — and for each, whether its rate came from
// the user's own exchange_rate: block or the built-in default table (see
// pricing.EffectiveExchangeRate and the plan doc's §2.3: an operator must
// be able to tell the two apart, not just see a number). ok=false when
// nothing in this config names a non-USD currency at all — the common
// case, and the line would say nothing useful.
func exchangeRateLine(cfg *config.Config) (string, bool) {
	used := map[string]bool{}
	for ccy := range cfg.ExchangeRate {
		used[strings.ToUpper(strings.TrimSpace(ccy))] = true
	}
	for _, p := range cfg.Providers {
		if p.Pricing != nil && p.Pricing.Currency != "" {
			used[strings.ToUpper(strings.TrimSpace(p.Pricing.Currency))] = true
		}
	}
	if len(used) == 0 {
		return "", false
	}
	_, defaultGeneratedAt, _ := pricing.LoadDefaultExchangeRate()
	names := make([]string, 0, len(used))
	for ccy := range used {
		names = append(names, ccy)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, ccy := range names {
		if v, ok := cfg.ExchangeRate[ccy]; ok {
			parts = append(parts, fmt.Sprintf("%s=%g (user)", ccy, v))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (built-in default, generated %s)", ccy, defaultGeneratedAt))
	}
	return strings.Join(parts, ", "), true
}

// printProviderPricing renders p's declared providers[].pricing block, if
// any — currency annotation, aliases, and rates exactly as written (already
// validated at config-load time: aliases resolve, rates are well-formed —
// see resolvePricing). Absent entirely for a provider with no pricing:
// block, same as every other optional section here. Pricing never reaches
// the request path (see core.PricingSpec's doc comment), so there is no
// "resolved per-endpoint rate" to show the way an older build did — the
// full resolution (standard table + this account's rates/aliases) only
// happens offline, in `vmr analyze`.
func printProviderPricing(w io.Writer, p config.Provider) {
	if p.Pricing == nil {
		return
	}
	fmt.Fprintln(w, "  pricing:")
	if p.Pricing.Currency != "" {
		fmt.Fprintln(w, checkLine(4, "currency", p.Pricing.Currency))
	}
	for _, local := range fmtutil.SortedKeys(p.Pricing.Aliases) {
		fmt.Fprintln(w, checkLine(4, "aliases", local+" -> "+p.Pricing.Aliases[local]))
	}
	for i, oc := range p.Pricing.Rates {
		val := fmt.Sprintf("in_fresh=%s cache_read=%s cache_write=%s out=%s",
			ratePart(oc.InFresh), ratePart(oc.CacheRead), ratePart(oc.CacheWrite), ratePart(oc.Out))
		if oc.Discount != nil {
			val = fmt.Sprintf("discount=%g", *oc.Discount)
		}
		fmt.Fprintln(w, checkLine(4, fmt.Sprintf("rates[%d] model=%s", i, oc.Model), val))
	}
}

// ratePart renders one Rate component for display — "?" makes a missing
// (nil) component visually distinct from an explicit 0, the same
// distinction internal/pricing.Rate exists to preserve everywhere else.
func ratePart(v *float64) string {
	if v == nil {
		return "?"
	}
	// %.6g rather than %g: a rate that went through an exchange-rate
	// multiplication (e.g. 3.0 USD x 7.1) routinely lands on a float64 like
	// 21.299999999999997 — cosmetic noise for a display line, not a value
	// anything downstream computes from (vmr analyze's pricing.Resolver
	// reads the pricing.Rate directly, never this formatted string).
	return strconv.FormatFloat(*v, 'g', 6, 64)
}
