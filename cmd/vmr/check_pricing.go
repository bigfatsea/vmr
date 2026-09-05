// Ver 2026-09-06, by Claude

// Pricing display for `vmr check` — split out of cmd_check.go when the file
// crossed its archtest line budget: the pricing block (standard-table
// freshness, per-provider resolved rates, rate formatting) is one cohesive
// concern that only printProviders and printGlobalSettings consume.
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
// deserve a refresh. ok=false when nothing in this config touches pricing at
// all (no global pricing: block, no providers[].pricing, no metric: cost
// Limit), so the common quota-free config gains no new line.
func pricingTableLine(cfg *config.Config) (string, bool) {
	if len(cfg.ProviderPricingPolicies) == 0 && cfg.Pricing == nil {
		return "", false
	}
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
	// cfg.Pricing's currency/exchange_rate/supplement are otherwise
	// invisible in this output, yet directly determine what unit a
	// metric: cost quota's amount= (below) is denominated in.
	if cfg.Pricing != nil {
		currency := cfg.Pricing.Currency
		switch {
		case currency == "":
			line += "; currency=USD"
		case cfg.Pricing.ExchangeRate[currency] != 0:
			line += fmt.Sprintf("; currency=%s (1 USD = %g %s)", currency, cfg.Pricing.ExchangeRate[currency], currency)
		default:
			line += "; currency=" + currency
		}
		if len(cfg.Pricing.Rates) > 0 {
			line += fmt.Sprintf("; %d inline rate(s)", len(cfg.Pricing.Rates))
		}
		if cfg.Pricing.Supplement != "" {
			line += "; supplement=" + cfg.Pricing.Supplement
		}
		if cfg.Pricing.Standard != "" {
			line += "; standard=" + cfg.Pricing.Standard
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

// printProviderPricing renders p's resolved metric: cost pricing , if
// any — one line per upstream model this provider actually resolved a
// price for (see config.Config.ResolvedPricing), so an operator can see
// exactly what rate a cost account will be charged at without cross-
// referencing the standard table by hand. Absent entirely for a provider
// with no resolved pricing, same as every other optional section here.
func printProviderPricing(w io.Writer, cfg *config.Config, p config.Provider) {
	var models []string
	prefix := p.Name + "\x00"
	for key := range cfg.ResolvedPricing {
		if strings.HasPrefix(key, prefix) {
			models = append(models, strings.TrimPrefix(key, prefix))
		}
	}
	if len(models) > 0 {
		sort.Strings(models)
		fmt.Fprintln(w, "  pricing:")
		for _, model := range models {
			spec := cfg.ResolvedPricing[prefix+model]
			// EffectiveRate, not spec.Base: an account with an override (e.g.
			// discount:) is charged at the resolved rate, not the standard
			// table's list price — printing Base here would show an operator a
			// number that has nothing to do with what metric: cost will actually
			// charge.
			r := pricing.EffectiveRate(spec)
			fmt.Fprintln(w, checkLine(4, model, fmt.Sprintf("in_fresh=%s cache_read=%s cache_write=%s out=%s %s/1M (%d override rule(s))",
				ratePart(r.InFresh), ratePart(r.CacheRead), ratePart(r.CacheWrite), ratePart(r.Out), spec.Currency, len(spec.Overrides))))
		}
		return
	}
	if p.Pricing == nil {
		return
	}
	// A declared providers[].pricing block with no ResolvedPricing entry
	// means no virtual model's endpoint currently routes any model to this
	// provider (so resolvePricing had nothing to resolve against) — show
	// the raw declaration instead of silently dropping it, since it's
	// explicit config a human wrote and may expect to see confirmed.
	fmt.Fprintln(w, "  pricing: (declared; not resolved — no routed endpoint references this provider)")
	for _, local := range fmtutil.SortedKeys(p.Pricing.Map) {
		fmt.Fprintln(w, checkLine(4, "map", local+" -> "+p.Pricing.Map[local]))
	}
	for i, oc := range p.Pricing.Overrides {
		val := fmt.Sprintf("in_fresh=%s cache_read=%s cache_write=%s out=%s",
			ratePart(oc.InFresh), ratePart(oc.CacheRead), ratePart(oc.CacheWrite), ratePart(oc.Out))
		if oc.Discount != nil {
			val = fmt.Sprintf("discount=%g", *oc.Discount)
		}
		if oc.Currency != "" {
			val += " currency=" + oc.Currency
		}
		fmt.Fprintln(w, checkLine(4, fmt.Sprintf("overrides[%d] model=%s", i, oc.Model), val))
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
	// anything downstream computes from (router.ChargeResponse/componentCost
	// read the pricing.Rate directly, never this formatted string).
	return strconv.FormatFloat(*v, 'g', 6, 64)
}
