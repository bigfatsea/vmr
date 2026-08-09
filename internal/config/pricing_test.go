// Ver 2026-08-07, by Opus 5
package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/pricing"

	_ "vmr/internal/adapter/openai"
)

const pricingBaseYAML = `
listen: 127.0.0.1:9900
%s
providers:
  - name: %s
    base_url: {openai: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
%s
models:
  m1:
    endpoints:
      - protocol: openai
        provider: %s
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
	rate := pricing.RateAt(spec, time.Now())
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

func TestPricing_Override_BadDateFormat_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.5, date_from: "not-a-date"}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "date_from") {
		t.Errorf("want a bad-date-format rejection, got %v", err)
	}
}

func TestPricing_Override_BadHourFormat_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
pricing:
  overrides:
    - {model: "*", discount: 0.5, hour_from: "25:99"}`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "hour_from") {
		t.Errorf("want a bad-hour-format rejection, got %v", err)
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

func TestPricing_ProviderPolicy_CoversProviderWithoutPricingBlock(t *testing.T) {
	// The currency/exchange-rate factor must reach EVERY provider, not just
	// ones with their own pricing: block — vmr report resolves standard
	// table prices for all of them and labels the result with
	// pricing.currency.
	yaml := `
listen: 127.0.0.1:9900
pricing:
  currency: CNY
  exchange_rate: {CNY: 7.1}
providers:
  - name: plain
    base_url: {openai: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
models:
  m1:
    endpoints:
      - protocol: openai
        provider: plain
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
	if policy.ExchangeRateToTarget != 7.1 || policy.Currency != "CNY" {
		t.Errorf("policy = {factor %v, currency %q}, want {7.1, CNY}", policy.ExchangeRateToTarget, policy.Currency)
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
// This is the layer the P2 dev plan §12 #4 bug actually manifested at
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
    base_url: {openai: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
models:
  m1:
    endpoints:
      - protocol: openai
        provider: anthropic
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
	resolver := pricing.NewResolver(table, cfg.ProviderPricingPolicies)
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219", time.Now())
	if !ok {
		t.Fatal("RateFor: no rate resolved for a model the standard table covers")
	}
	if rate.InFresh == nil {
		t.Fatal("rate.InFresh unexpectedly nil")
	}
	// The standard table's USD list price for this model is 3.0/1M in_fresh
	// (internal/pricing/standard_price_generated.yaml) — converted at 7.1 that
	// must land on 21.3, not 3.0 (the bug's failure mode: computing in USD
	// while the report labels the column CNY).
	const wantUSD = 3.0
	if got, want := *rate.InFresh, wantUSD*7.1; got < want-0.001 || got > want+0.001 {
		t.Errorf("rate.InFresh = %v, want %v (= %v USD list price x 7.1 exchange rate) — got the unconverted USD figure instead?", got, want, wantUSD)
	}
}

// TestPricing_ThreeLayerResolution_PrecedenceOrder pins the design doc's
// §4.2① precedence chain end to end through internal/config's actual
// resolvePricing path (not internal/pricing.Merge tested in isolation,
// which the P2 dev plan's test coverage already exercises against
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
	rate := pricing.RateAt(spec2, time.Now())
	if got, want := *rate.InFresh, 4.5; got != want {
		t.Fatalf("RateAt.InFresh = %v, want %v (= supplement's 9 x 0.5 discount) — account override did not correctly layer on top of the supplement", got, want)
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
