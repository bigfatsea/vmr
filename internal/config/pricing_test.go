// Ver 2026-08-07, by Opus 5
package config

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"vmr/internal/core"
	"vmr/internal/pricing"

	_ "vmr/internal/adapter/openai"
)

const pricingBaseYAML = `
listen: 127.0.0.1:9900
%s
providers:
  - name: %s
    base_url: {openai-completions: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
%s
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [%s]
        models: [%s]
`

// pricingCfg builds a config with provider name "p1" — used by tests that
// resolve pricing via an explicit override or pricing.map, where the
// auto-resolution steps (which DO consult the vmr provider name) are
// irrelevant.
func pricingCfg(globalBlock, providerBlock, model string) string {
	return pricingCfgNamed(globalBlock, "p1", providerBlock, model)
}

// pricingCfgNamed is pricingCfg with an explicit provider name — used by
// tests relying on the design doc's §9.1 step ② auto-resolution
// ("<provider>/<model>"), which only succeeds when the vmr provider name
// happens to match the standard table's vendor prefix (e.g. "anthropic").
func pricingCfgNamed(globalBlock, providerName, providerBlock, model string) string {
	indent := func(s, pad string) string {
		if s == "" {
			return ""
		}
		var b strings.Builder
		for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			b.WriteString(pad + line + "\n")
		}
		return b.String()
	}
	return fmt.Sprintf(pricingBaseYAML, globalBlock, providerName, indent(providerBlock, "    "), providerName, model)
}

func TestPricing_MetricRequests_PricingBlockUnused_NoResolution(t *testing.T) {
	yaml := pricingCfg("", "", "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ResolvedPricing != nil {
		t.Fatalf("ResolvedPricing = %v, want nil (no metric: cost provider, no pricing: block)", cfg.ResolvedPricing)
	}
}

func TestPricing_MetricCost_StandardTableOnly_HappyPath(t *testing.T) {
	yaml := pricingCfgNamed("pricing:\n  currency: USD\n", "anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "claude-3-7-sonnet-20250219")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec, ok := cfg.ResolvedPricing["anthropic\x00claude-3-7-sonnet-20250219"]
	if !ok {
		t.Fatal("claude-3-7-sonnet-20250219 not resolved — standard table should have covered it (<provider>/<model> canonical key)")
	}
	if spec.Base.InFresh == nil {
		t.Fatal("Base.InFresh unexpectedly nil")
	}
	if spec.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", spec.Currency)
	}
}

func TestPricing_MetricCost_NoRateFound_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "totally-unknown-model-xyz")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "no price found") {
		t.Errorf("want a no-price-found rejection, got %v", err)
	}
}

func TestPricing_MetricCost_IncompleteRate_Rejected(t *testing.T) {
	// deepseek-chat's real data has no cache_write component — the standard
	// table entry is genuinely incomplete, and this account has no override
	// to fill the gap. Provider named "deepseek" so step ② of the 4-step
	// resolution (<provider>/<model>) finds the table row directly.
	yaml := pricingCfgNamed("pricing:\n  currency: USD\n", "deepseek", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "deepseek-chat")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "incomplete rate") || !strings.Contains(err.Error(), "cache_write") {
		t.Errorf("want an incomplete-rate rejection naming cache_write, got %v", err)
	}
}

func TestPricing_MetricCost_OverrideFillsGap(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: deepseek-chat, in_fresh: 0.28, cache_read: 0.028, cache_write: 0, out: 0.42}`, "deepseek-chat")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v (override should have filled the missing cache_write component)", err)
	}
	spec := cfg.ResolvedPricing["p1\x00deepseek-chat"]
	if spec == nil {
		t.Fatal("no resolved spec")
	}
}

func TestPricing_MetricCost_DiscountOverride(t *testing.T) {
	yaml := pricingCfgNamed("pricing:\n  currency: USD\n", "anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.6}`, "claude-3-7-sonnet-20250219")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec := cfg.ResolvedPricing["anthropic\x00claude-3-7-sonnet-20250219"]
	if spec == nil || len(spec.Overrides) != 1 {
		t.Fatalf("spec = %+v, want exactly 1 override", spec)
	}
}

func TestPricing_Override_Currency_ConvertsToTarget(t *testing.T) {
	// Account's own negotiated rate entered straight from a USD invoice
	// while the deployment's accounting currency is CNY.
	yaml := pricingCfg("pricing:\n  currency: CNY\n  exchange_rate: {CNY: 7.1}\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", currency: USD, in_fresh: 1, cache_read: 0.1, cache_write: 1.25, out: 4}`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec := cfg.ResolvedPricing["p1\x00gpt-4o"]
	if spec == nil || len(spec.Overrides) != 1 {
		t.Fatalf("spec = %+v, want exactly 1 override", spec)
	}
	rate := pricing.EffectiveRate(spec)
	if got, want := *rate.InFresh, 7.1; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("InFresh = %v, want %v (1 USD x 7.1)", got, want)
	}
	if got, want := *rate.Out, 28.4; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("Out = %v, want %v (4 USD x 7.1)", got, want)
	}
}

func TestPricing_Override_CurrencyWithDiscount_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", currency: CNY, discount: 0.5}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "currency only applies to an explicit rate") {
		t.Errorf("want a currency+discount rejection, got %v", err)
	}
}

func TestPricing_Override_CurrencyMissingRate_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", currency: CNY, in_fresh: 1, cache_read: 0.1, cache_write: 1.25, out: 4}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "exchange_rate entry") {
		t.Errorf("want a missing-rate rejection, got %v", err)
	}
}

func TestPricing_Supplement_NonUSDRow_ConvertsViaExchangeRate(t *testing.T) {
	supplementPath := writeSupplementFile(t, `
currency: USD
generated_at: "2026-08-09"
rates:
  - key: domestic/model-a
    currency: CNY
    in_fresh: 7.1
    cache_read: 0.71
    cache_write: 8.875
    out: 28.4
`)
	yaml := pricingCfgNamed(fmt.Sprintf("pricing:\n  currency: USD\n  exchange_rate: {CNY: 7.1}\n  supplement: %s\n", supplementPath),
		"domestic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "model-a")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec := cfg.ResolvedPricing["domestic\x00model-a"]
	if spec == nil || spec.Base.InFresh == nil {
		t.Fatal("no resolved base rate")
	}
	if got := *spec.Base.InFresh; got < 1-1e-9 || got > 1+1e-9 {
		t.Fatalf("Base.InFresh = %v, want 1.0 (the supplement row's 7.1 CNY converted to USD via exchange_rate, then to the USD target currency unchanged)", got)
	}
}

func TestPricing_Supplement_SelfContainedExchangeRate_NoConfigRateNeeded(t *testing.T) {
	// config.yaml has NO pricing.exchange_rate at all — the supplement
	// file's own exchange_rate: block must be enough on its own, proving
	// pricing.yaml can be portable/self-contained rather than depending on
	// an unrelated config.yaml declaring a matching key.
	supplementPath := writeSupplementFile(t, `
currency: USD
generated_at: "2026-08-09"
exchange_rate: {CNY: 7.1}
rates:
  - key: domestic/model-a
    currency: CNY
    in_fresh: 7.1
    cache_read: 0.71
    cache_write: 8.875
    out: 28.4
`)
	yaml := pricingCfgNamed(fmt.Sprintf("pricing:\n  currency: USD\n  supplement: %s\n", supplementPath),
		"domestic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "model-a")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse (no pricing.exchange_rate in config.yaml at all): %v", err)
	}
	spec := cfg.ResolvedPricing["domestic\x00model-a"]
	if spec == nil || spec.Base.InFresh == nil {
		t.Fatal("no resolved base rate")
	}
	if got := *spec.Base.InFresh; got < 1-1e-9 || got > 1+1e-9 {
		t.Fatalf("Base.InFresh = %v, want 1.0 (converted via the supplement file's own exchange_rate, with no help from config.yaml)", got)
	}
}

func TestPricing_MetricCost_MissingCurrency_Rejected(t *testing.T) {
	yaml := pricingCfg("", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "pricing.currency") {
		t.Errorf("want a pricing.currency-not-set rejection, got %v", err)
	}
}

func TestPricing_MetricCost_NonUSD_MissingExchangeRate_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: CNY\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "exchange_rate") {
		t.Errorf("want an exchange_rate-missing rejection, got %v", err)
	}
}

func TestPricing_MetricCost_NonUSD_ExchangeRateApplied(t *testing.T) {
	yaml := pricingCfgNamed("pricing:\n  currency: CNY\n  exchange_rate: {CNY: 7.1}\n", "anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "claude-3-7-sonnet-20250219")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec := cfg.ResolvedPricing["anthropic\x00claude-3-7-sonnet-20250219"]
	if spec == nil || spec.Base.InFresh == nil {
		t.Fatal("no resolved base rate")
	}
	// claude-3-7-sonnet-20250219's standard in_fresh is 3 USD/1M -> 3*7.1 CNY.
	want := 3 * 7.1
	if diff := *spec.Base.InFresh - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Base.InFresh = %v, want %v (USD rate x exchange rate)", *spec.Base.InFresh, want)
	}
}

func TestPricing_ProviderPricingMap_ResolvesCustomModelName(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  map: {my-custom-name: anthropic/claude-3-7-sonnet-20250219}`, "my-custom-name")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ResolvedPricing["p1\x00my-custom-name"] == nil {
		t.Fatal("map-resolved model not found in ResolvedPricing")
	}
}

func TestPricing_Override_DiscountAndExplicit_MutuallyExclusive(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.5, in_fresh: 1, cache_read: 1, cache_write: 1, out: 1}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("want a discount+explicit rejection, got %v", err)
	}
}

func TestPricing_Override_PartialExplicit_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", in_fresh: 1, out: 4}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "all four components") {
		t.Errorf("want a partial-explicit rejection, got %v", err)
	}
}

func TestPricing_Override_NeitherDiscountNorExplicit_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*"}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "either discount or all four") {
		t.Errorf("want a neither-discount-nor-explicit rejection, got %v", err)
	}
}

// TestPricing_Override_TimeWindowFieldsUnknown_Rejected locks in P0-A's
// removal of the date_from/date_to/hour_from/hour_to promotional-window
// fields: they are no longer part of PricingOverrideConfig, so KnownFields'
// ordinary unknown-field rejection catches them exactly like any other typo
// — no dedicated migration message, same reasoning as
// TestLegacyAPIKeyRejected in config_test.go.
func TestPricing_Override_TimeWindowFieldsUnknown_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.5, date_from: "2026-06-08"}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "date_from") {
		t.Errorf("want an unknown-field rejection naming date_from, got %v", err)
	}
}

// TestPricing_Override_DuplicateModelPattern_Rejected covers
// firstDeadOverride: two EXPLICIT rules sharing the same model pattern, no
// time dimension left to differentiate them — first-match-wins never
// reaches the second, so it is dead config (see that function's doc
// comment). Two DISCOUNT rules on one model stay legal: a discount
// terminates nothing, so they compose multiplicatively
// (TestPricing_Override_SameModelDiscounts_Compose).
func TestPricing_Override_DuplicateModelPattern_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: gpt-4o, in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54}
    - {model: gpt-4o, in_fresh: 2.58, cache_read: 1.32, cache_write: 2.58, out: 19.54}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "can never activate") {
		t.Errorf("want a dead-override rejection, got %v", err)
	}
}

// TestPricing_Override_ExplicitWildcardBeforeSpecific_Rejected covers the
// other firstDeadOverride shape: an earlier EXPLICIT "*" wildcard
// terminates matching for every model, so a later rule — no matter its own
// model — is unreachable dead config. (A wildcard DISCOUNT does not
// shadow: it composes down the chain, see
// TestPricing_Override_WildcardDiscountOverSpecificExplicit_Resolves.)
func TestPricing_Override_ExplicitWildcardBeforeSpecific_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", in_fresh: 1.0, cache_read: 0.5, cache_write: 1.0, out: 5.0}
    - {model: gpt-4o, in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "can never activate") {
		t.Errorf("want a dead-override rejection, got %v", err)
	}
}

// TestPricing_Override_WildcardDiscountOverSpecificExplicit_Resolves pins
// the legal shape firstDeadOverride must NOT reject: an account-wide
// wildcard DISCOUNT first, a model-specific EXPLICIT rate after. A discount
// drills down the chain instead of terminating, so the gpt-4o rule stays
// reachable and the composed rate is 0.8 x the explicit four components.
func TestPricing_Override_WildcardDiscountOverSpecificExplicit_Resolves(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.8}
    - {model: gpt-4o, in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54}`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse rejected a wildcard-discount-over-specific-explicit list: %v", err)
	}
	spec := cfg.ResolvedPricing["p1\x00gpt-4o"]
	if spec == nil {
		t.Fatal("gpt-4o not resolved")
	}
	rate := pricing.EffectiveRate(spec)
	for _, tc := range []struct {
		name string
		have *float64
		want float64
	}{{"in_fresh", rate.InFresh, 1.58 * 0.8}, {"cache_read", rate.CacheRead, 0.32 * 0.8},
		{"cache_write", rate.CacheWrite, 1.58 * 0.8}, {"out", rate.Out, 9.54 * 0.8}} {
		// Tolerance, not ==: the composed value is a runtime float multiply,
		// the expected one a compile-time constant.
		if tc.have == nil || math.Abs(*tc.have-tc.want) > 1e-9 {
			t.Errorf("%s = %v, want %v (explicit x 0.8)", tc.name, tc.have, tc.want)
		}
	}
}

// TestPricing_Override_SameModelDiscounts_Compose pins the other shape the
// narrowed firstDeadOverride legalizes: two DISCOUNT rules (stacked here on
// one model; the same holds for a specific discount above a wildcard
// discount) terminate nothing and compose multiplicatively against the
// table's Base.
func TestPricing_Override_SameModelDiscounts_Compose(t *testing.T) {
	yaml := pricingCfgNamed("pricing:\n  currency: USD\n", "anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: claude-3-7-sonnet-20250219, discount: 0.5}
    - {model: "*", discount: 0.8}`, "claude-3-7-sonnet-20250219")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse rejected stacked discounts: %v", err)
	}
	spec := cfg.ResolvedPricing["anthropic\x00claude-3-7-sonnet-20250219"]
	if spec == nil {
		t.Fatal("claude-3-7-sonnet-20250219 not resolved")
	}
	rate := pricing.EffectiveRate(spec)
	base := []struct {
		name string
		have *float64
		want *float64
	}{{"in_fresh", rate.InFresh, spec.Base.InFresh}, {"cache_read", rate.CacheRead, spec.Base.CacheRead},
		{"cache_write", rate.CacheWrite, spec.Base.CacheWrite}, {"out", rate.Out, spec.Base.Out}}
	for _, tc := range base {
		if tc.have == nil || tc.want == nil || *tc.have != *tc.want*0.5*0.8 {
			t.Errorf("%s = %v, want %v (Base x 0.5 x 0.8)", tc.name, tc.have, tc.want)
		}
	}
}

func TestPricing_ModelMultipliers_OnCostOnlyAccount_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  model_multipliers: {"*": 2}
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.5}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "model_multipliers") {
		t.Errorf("want a model_multipliers-on-cost-only rejection, got %v", err)
	}
}

func TestPricing_Supplement_MissingFile_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n  supplement: /nonexistent/path/supplement.yaml\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "supplement") {
		t.Errorf("want a supplement-file-missing rejection, got %v", err)
	}
}

func TestPricing_NonCostProvider_PricingBlockStillValidatedStructurally(t *testing.T) {
	// No metric: cost anywhere — this pricing: block only sharpens vmr
	// report's $ estimates, but a malformed override is still an error at
	// load time (shape validation runs regardless of whether a cost Limit
	// consumes the result).
	yaml := pricingCfg("", `pricing:
  overrides:
    - {model: "*"}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "either discount or all four") {
		t.Errorf("want structural validation to still run on a non-cost provider's pricing block, got %v", err)
	}
}

func TestPricing_RoundTrip_QuotaSpecOnEndpoint(t *testing.T) {
	// Sanity: core.MetricCost survives Parse into the resolved Limit — this
	// is what router.BuildSnapshot / chargeQuota switch on.
	yaml := pricingCfgNamed("pricing:\n  currency: USD\n", "anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "claude-3-7-sonnet-20250219")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("anthropic")
	if p.Quota.Limits[0].Resolved.Metric != core.MetricCost {
		t.Fatalf("Resolved.Metric = %v, want core.MetricCost", p.Quota.Limits[0].Resolved.Metric)
	}
}

func TestPricing_NonUSD_MissingExchangeRate_RejectedEvenWithoutCostProvider(t *testing.T) {
	// No metric: cost account at all — the block exists only to sharpen vmr
	// report's $ column. The rejection is still unconditional: report prices
	// every provider an audit log names through the same USD standard table,
	// so a missing factor would print USD numbers under a "CNY" label.
	yaml := pricingCfg("pricing:\n  currency: CNY\n", "", "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "exchange_rate") {
		t.Errorf("want an exchange_rate-missing rejection with no cost provider, got %v", err)
	}
}

func TestPricing_ExchangeRate_NonPositive_Rejected(t *testing.T) {
	for _, rate := range []string{"-1.0", "0", ".nan", ".inf"} {
		yaml := pricingCfg("pricing:\n  currency: CNY\n  exchange_rate: {CNY: "+rate+"}\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "gpt-4o")
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), "exchange_rate") {
			t.Errorf("exchange_rate %s: want rejection, got %v", rate, err)
		}
	}
}

func TestPricing_Override_NonFiniteOrNegativeComponent_Rejected(t *testing.T) {
	for _, val := range []string{"-1.0", ".nan", ".inf"} {
		yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", in_fresh: `+val+`, cache_read: 0.1, cache_write: 1, out: 2}`, "gpt-4o")
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), "in_fresh must be a finite number") {
			t.Errorf("in_fresh %s: want rejection, got %v", val, err)
		}
	}
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: .nan}`, "gpt-4o")
	if _, err := Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "discount must be a finite number") {
		t.Errorf("discount .nan: want rejection, got %v", err)
	}
}

func TestPricing_Map_UnknownCanonicalKey_Rejected(t *testing.T) {
	// A typo'd canonical key used to fall through to the automatic steps
	// and could land on some other model's price — silently. It's a
	// load-time error instead.
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  map: {my-custom-name: anthropic/claude-3-7-sonnet-TYPO}`, "my-custom-name")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "pricing.map") {
		t.Errorf("want a pricing.map unknown-key rejection, got %v", err)
	}
}

func TestPricing_Map_PointsToAlias_Accepted(t *testing.T) {
	// pricing.map targeting an alias (e.g. "deepseek-v4-flash" instead of
	// "deepseek/deepseek-v4-flash") is accepted and resolves correctly.
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  map: {my-custom-deepseek: deepseek-v4-flash}`, "my-custom-deepseek")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec := cfg.ResolvedPricing["p1\x00my-custom-deepseek"]
	if spec == nil {
		t.Fatal("expected ResolvedPricing for p1/my-custom-deepseek")
	}
	rate := pricing.EffectiveRate(spec)
	if rate.InFresh == nil || *rate.InFresh != 0.44 {
		t.Errorf("InFresh = %v, want 0.44 (deepseek-v4-flash list price)", *rate.InFresh)
	}
}

func TestPricing_ProviderPolicy_CoversProviderWithoutPricingBlock(t *testing.T) {
	// Every provider gets a ProviderPolicy entry, even one with no pricing:
	// block — vmr report resolves standard-table prices for all of them.
	// The currency factor is deliberately NOT on the policy (it is global,
	// see Config.PricingAccounting); this only pins that the entry exists
	// and carries empty, not absent, map/overrides.
	yaml := `
listen: 127.0.0.1:9900
pricing:
  currency: CNY
  exchange_rate: {CNY: 7.1}
providers:
  - name: plain
    base_url: {openai-completions: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [plain]
        models: [gpt-4o]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	policy, ok := cfg.ProviderPricingPolicies["plain"]
	if !ok {
		t.Fatal("provider with no pricing: block got no ProviderPricingPolicies entry — its standard-table rates would stay unconverted while the report labels them CNY")
	}
	if len(policy.Map) != 0 || len(policy.Overrides) != 0 {
		t.Errorf("policy = %+v, want an entry with no map and no overrides", policy)
	}
	if factor, currency := cfg.PricingAccounting(); factor != 7.1 || currency != "CNY" {
		t.Errorf("PricingAccounting() = (%v, %q), want (7.1, \"CNY\")", factor, currency)
	}
}

// TestPricing_NonUSDProviderWithoutPricingBlock_ResolverActuallyConverts goes
// one layer past TestPricing_ProviderPolicy_CoversProviderWithoutPricingBlock:
// that test only asserts the config-layer ProviderPolicy carries the right
// factor; this one wires cfg.PricingTable()/cfg.ProviderPricingPolicies
// into a real pricing.Resolver — the exact construction
// cmd/vmr/cmd_report.go's buildPricing performs — and checks the NUMBER
// RateFor actually returns for a provider with no pricing: block of its
// own is the converted (CNY) figure, not the raw USD standard-table price.
// This is the layer a pricing resolution edge-case actually manifested at
// ("label CNY, compute USD") — a regression here would have shipped a ~7x
// silently wrong number in every vmr report run for exactly this common
// case (a plain provider with no per-account pricing override), and
// nothing before this test would have caught it: the ProviderPolicy-level
// test above only checks the factor is carried, never that RateFor applies
// it.
func TestPricing_NonUSDProviderWithoutPricingBlock_ResolverActuallyConverts(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
pricing:
  currency: CNY
  exchange_rate: {CNY: 7.1}
providers:
  - name: anthropic
    base_url: {openai-completions: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [anthropic]
        models: [claude-3-7-sonnet-20250219]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	table, err := cfg.PricingTable()
	if err != nil {
		t.Fatalf("PricingTable: %v", err)
	}
	factor, _ := cfg.PricingAccounting()
	resolver := pricing.NewResolver(table, cfg.ProviderPricingPolicies, factor, "")
	// The standard table's USD list price for this model is 3.0/1M in_fresh
	// (internal/pricing/standard_price_generated.yaml) — converted at 7.1 that
	// must land on 21.3, not 3.0 (the bug's failure mode: computing in USD
	// while the report labels the column CNY).
	const wantUSD = 3.0
	// "anthropic" is declared in config.yaml; "anthropic-old" is not — the
	// shape an audit log always has, since it names every provider that ever
	// ran, including ones since renamed, deleted, or split by api_keys. Both
	// MUST convert: holding the factor per provider meant the second one
	// silently resolved at 1.0, so one canonical row rendered as USD and as
	// CNY in adjacent lines of the same table.
	for _, provider := range []string{"anthropic", "anthropic-old"} {
		rate, ok := resolver.RateFor(provider, "claude-3-7-sonnet-20250219")
		if !ok {
			t.Fatalf("RateFor(%q): no rate resolved for a model the standard table covers", provider)
		}
		if rate.InFresh == nil {
			t.Fatalf("RateFor(%q): rate.InFresh unexpectedly nil", provider)
		}
		if got, want := *rate.InFresh, wantUSD*7.1; got < want-0.001 || got > want+0.001 {
			t.Errorf("RateFor(%q).InFresh = %v, want %v (= %v USD list price x 7.1 exchange rate) — got the unconverted USD figure instead?", provider, got, want, wantUSD)
		}
	}
}

// TestPricing_ThreeLayerResolution_PrecedenceOrder pins the design doc's
// §4.2① precedence chain end to end through internal/config's actual
// resolvePricing path (not internal/pricing.Merge tested in isolation,
// which standard test coverage already exercises against
// synthetic tables): account override > supplement ∪ standard > standard
// alone. Uses a REAL standard-table entry
// (anthropic/claude-3-haiku-20240307) so the "supplement wins over
// standard" step is genuinely exercised, not just "supplement fills a gap
// standard never had" (already covered elsewhere, e.g.
// TestPricing_MetricCost_OverrideFillsGap, which — being about an
// *account* override, not a supplement — doesn't touch this layer at all).
func TestPricing_ThreeLayerResolution_PrecedenceOrder(t *testing.T) {
	supplementPath := writeSupplementFile(t, `
currency: USD
generated_at: "2026-08-09"
rates:
  - key: anthropic/claude-3-haiku-20240307
    in_fresh: 9
    cache_read: 9
    cache_write: 9
    out: 9
`)

	// Layer check 1: no account override — resolved Base must be the
	// supplement's 9s, not the standard table's real (much cheaper) list
	// price for this model, proving supplement really does win over
	// standard on a matching canonical key rather than being silently
	// ignored or losing the merge.
	yaml := pricingCfgNamed(fmt.Sprintf("pricing:\n  currency: USD\n  supplement: %s\n", supplementPath),
		"anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}`, "claude-3-haiku-20240307")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	spec := cfg.ResolvedPricing["anthropic\x00claude-3-haiku-20240307"]
	if spec == nil {
		t.Fatal("no PricingSpec resolved")
	}
	if got := *spec.Base.InFresh; got != 9 {
		t.Fatalf("Base.InFresh = %v, want 9 (the supplement's value) — supplement did not win over the standard table's own %v", got, 0.25)
	}

	// Layer check 2: add an account-level override on top — it must win
	// over the supplement-merged value from check 1, not the standard
	// table's original price (proving the full three-layer precedence, not
	// just supplement-over-standard).
	yaml2 := pricingCfgNamed(fmt.Sprintf("pricing:\n  currency: USD\n  supplement: %s\n", supplementPath),
		"anthropic", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: claude-3-haiku-20240307, discount: 0.5}`, "claude-3-haiku-20240307")
	cfg2, err := Parse([]byte(yaml2))
	if err != nil {
		t.Fatalf("Parse (with account override): %v", err)
	}
	spec2 := cfg2.ResolvedPricing["anthropic\x00claude-3-haiku-20240307"]
	if spec2 == nil {
		t.Fatal("no PricingSpec resolved (with account override)")
	}
	rate := pricing.EffectiveRate(spec2)
	if got, want := *rate.InFresh, 4.5; got != want {
		t.Fatalf("EffectiveRate.InFresh = %v, want %v (= supplement's 9 x 0.5 discount) — account override did not correctly layer on top of the supplement", got, want)
	}
}

// writeSupplementFile writes content to a temp file and returns its path —
// internal/pricing.ParseTable (via config.buildPricingContext) reads
// pricing.supplement from disk via os.ReadFile, so an in-memory YAML string
// (the pattern every other test in this file uses via Parse) can't reach
// this code path; a real file is required.
func writeSupplementFile(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/supplement.yaml"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write supplement file: %v", err)
	}
	return path
}
