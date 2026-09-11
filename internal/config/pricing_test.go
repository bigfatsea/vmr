// Ver 2026-09-06, by Sonnet 5
package config

import (
	"fmt"
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
      openai-completions:
        - providers: [%s]
          models: [%s]
`

// pricingCfg builds a config with provider name "p1" — used by tests that
// resolve pricing via an explicit rates rule or aliases, where the
// auto-resolution steps (which DO consult the vmr provider name) are
// irrelevant. globalBlock is the top-level YAML (e.g. "exchange_rate:
// {CNY: 7.1}\n"), providerBlock is indented under the one declared provider.
func pricingCfg(globalBlock, providerBlock, model string) string {
	return pricingCfgNamed(globalBlock, "p1", providerBlock, model)
}

// pricingCfgNamed is pricingCfg with an explicit provider name — used by
// tests relying on the design doc's step ② auto-resolution
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

// TestPricing_NoPricingBlock_ResolvesViaStandardTableAlone pins that a
// provider with no pricing: block at all still resolves standard-table
// prices through a pricing.Resolver — Go's zero-value map lookup already
// gives an absent entry the same empty ProviderPolicy an explicit-but-empty
// one would, so resolvePricing doesn't need to populate one for every
// provider (only for those that actually declare pricing:).
func TestPricing_NoPricingBlock_ResolvesViaStandardTableAlone(t *testing.T) {
	yaml := pricingCfgNamed("", "anthropic", "", "claude-3-7-sonnet-20250219")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := cfg.ProviderPricingPolicies["anthropic"]; ok {
		t.Fatal("a provider with no pricing: block should not get a ProviderPricingPolicies entry")
	}
	table, err := cfg.PricingTable()
	if err != nil {
		t.Fatalf("PricingTable: %v", err)
	}
	resolver := pricing.NewResolver(table, cfg.ProviderPricingPolicies)
	if _, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219"); !ok {
		t.Fatal("RateFor should still resolve via the standard table alone for an unconfigured provider")
	}
}

func TestPricing_Aliases_ResolvesToStandardTable(t *testing.T) {
	yaml := pricingCfg("", "pricing:\n  aliases: {my-custom-name: anthropic/claude-3-7-sonnet-20250219}\n", "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	policy := cfg.ProviderPricingPolicies["p1"]
	if policy.Aliases["my-custom-name"] != "anthropic/claude-3-7-sonnet-20250219" {
		t.Fatalf("policy.Aliases = %+v, want my-custom-name -> anthropic/claude-3-7-sonnet-20250219", policy.Aliases)
	}
}

func TestPricing_Aliases_UnknownCanonicalKey_Rejected(t *testing.T) {
	// A typo'd canonical key used to fall through to the automatic steps
	// and could land on some other model's price — silently. It's a
	// load-time error instead.
	yaml := pricingCfg("", "pricing:\n  aliases: {my-custom-name: anthropic/claude-3-7-sonnet-TYPO}\n", "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "pricing.aliases") {
		t.Errorf("want a pricing.aliases unknown-key rejection, got %v", err)
	}
}

func TestPricing_Aliases_PointsToTableAlias_Accepted(t *testing.T) {
	// pricing.aliases targeting a standard-table alias (e.g.
	// "deepseek-v4-flash" instead of "deepseek/deepseek-v4-flash") is
	// accepted.
	yaml := pricingCfg("", "pricing:\n  aliases: {my-custom-deepseek: deepseek-v4-flash}\n", "gpt-4o")
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestPricing_Rates_ExplicitReplacesTable(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*", in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	policy := cfg.ProviderPricingPolicies["p1"]
	if len(policy.Overrides) != 1 || policy.Overrides[0].Model != "*" {
		t.Fatalf("policy.Overrides = %+v, want one wildcard rule", policy.Overrides)
	}
	if policy.Overrides[0].Explicit.InFresh == nil || *policy.Overrides[0].Explicit.InFresh != 1.58 {
		t.Fatalf("Explicit.InFresh = %v, want 1.58", policy.Overrides[0].Explicit.InFresh)
	}
}

func TestPricing_Rates_Discount(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*", discount: 0.9}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	policy := cfg.ProviderPricingPolicies["p1"]
	if len(policy.Overrides) != 1 || policy.Overrides[0].Discount == nil || *policy.Overrides[0].Discount != 0.9 {
		t.Fatalf("policy.Overrides = %+v, want one 0.9 discount rule", policy.Overrides)
	}
}

func TestPricing_Rates_DiscountAndExplicit_MutuallyExclusive(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*", discount: 0.9, in_fresh: 1.0, cache_read: 0.1, cache_write: 1.0, out: 4.0}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("want a discount/explicit mutual-exclusion rejection, got %v", err)
	}
}

func TestPricing_Rates_PartialExplicit_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*", in_fresh: 1.0, out: 4.0}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "all four components") {
		t.Errorf("want a partial-explicit-rate rejection, got %v", err)
	}
}

func TestPricing_Rates_NeitherDiscountNorExplicit_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*"}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "either discount or all four") {
		t.Errorf("want a discount-or-explicit-required rejection, got %v", err)
	}
}

func TestPricing_Rates_MissingModel_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {discount: 0.9}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Errorf("want a missing-model rejection, got %v", err)
	}
}

func TestPricing_Rates_NonFiniteOrNegativeComponent_Rejected(t *testing.T) {
	for _, tc := range []string{
		"in_fresh: .nan, cache_read: 0.1, cache_write: 1.0, out: 4.0",
		"in_fresh: 1.0, cache_read: -0.1, cache_write: 1.0, out: 4.0",
	} {
		yaml := pricingCfg("", fmt.Sprintf("pricing:\n  rates:\n    - {model: \"*\", %s}\n", tc), "gpt-4o")
		if _, err := Parse([]byte(yaml)); err == nil {
			t.Errorf("%s: want a rejection, got none", tc)
		}
	}
}

func TestPricing_Rates_DuplicateExplicitModel_DeadOverride_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: my-model, in_fresh: 1.0, cache_read: 0.1, cache_write: 1.0, out: 4.0}
    - {model: my-model, discount: 0.5}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "can never activate") {
		t.Errorf("want a dead-override rejection, got %v", err)
	}
}

func TestPricing_Rates_ExplicitWildcardBeforeSpecific_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*", in_fresh: 1.0, cache_read: 0.1, cache_write: 1.0, out: 4.0}
    - {model: my-model, discount: 0.5}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "can never activate") {
		t.Errorf("want a dead-override rejection (wildcard shadows everything after it), got %v", err)
	}
}

func TestPricing_Rates_DiscountBeforeExplicit_Resolves(t *testing.T) {
	// A discount-form rule shadows nothing (it composes down the chain) —
	// this ordering must be accepted.
	yaml := pricingCfg("", `pricing:
  rates:
    - {model: "*", discount: 0.5}
    - {model: my-model, in_fresh: 1.0, cache_read: 0.1, cache_write: 1.0, out: 4.0}
`, "gpt-4o")
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestPricing_Currency_ConvertsRatesToUSD(t *testing.T) {
	yaml := pricingCfg("exchange_rate: {CNY: 7.1}\n", `pricing:
  currency: CNY
  rates:
    - {model: "*", in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	policy := cfg.ProviderPricingPolicies["p1"]
	rate := policy.Overrides[0].Explicit
	if rate.InFresh == nil || *rate.InFresh < 0.999 || *rate.InFresh > 1.001 {
		t.Fatalf("InFresh = %v, want ~1.0 (7.1 CNY / 7.1)", rate.InFresh)
	}
	if rate.Out == nil || *rate.Out < 3.999 || *rate.Out > 4.001 {
		t.Fatalf("Out = %v, want ~4.0 (28.4 CNY / 7.1)", rate.Out)
	}
}

func TestPricing_Currency_DiscountNotConverted(t *testing.T) {
	// A discount is a dimensionless multiplier — currency conversion must
	// never touch it.
	yaml := pricingCfg("exchange_rate: {CNY: 7.1}\n", `pricing:
  currency: CNY
  rates:
    - {model: "*", discount: 0.9}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	policy := cfg.ProviderPricingPolicies["p1"]
	if *policy.Overrides[0].Discount != 0.9 {
		t.Fatalf("Discount = %v, want unchanged 0.9", *policy.Overrides[0].Discount)
	}
}

func TestPricing_Currency_MissingExchangeRate_FallsBackToBuiltinDefault(t *testing.T) {
	// CNY is covered by the built-in default exchange-rate table (see
	// internal/pricing/standard_exchange_rate.yaml) — no top-level
	// exchange_rate: block is needed for it to resolve.
	yaml := pricingCfg("", `pricing:
  currency: CNY
  rates:
    - {model: "*", in_fresh: 7.1, cache_read: 0.1, cache_write: 1.0, out: 4.0}
`, "gpt-4o")
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Fatalf("Parse: %v (CNY should resolve via the built-in default table)", err)
	}
}

func TestPricing_Currency_UnknownCurrency_Rejected(t *testing.T) {
	// A currency neither the user's exchange_rate: block nor the built-in
	// default table covers is a load-time error, never a silent 1:1 guess.
	yaml := pricingCfg("", `pricing:
  currency: ZZZ
  rates:
    - {model: "*", in_fresh: 1.0, cache_read: 0.1, cache_write: 1.0, out: 4.0}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "exchange_rate") {
		t.Errorf("want an exchange-rate-not-found rejection, got %v", err)
	}
}

func TestPricing_LegacyTopLevelPricingBlock_Rejected(t *testing.T) {
	yaml := pricingCfg("pricing:\n  currency: USD\n", "", "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "top-level pricing: block is no longer supported") {
		t.Errorf("want a legacy top-level pricing: block rejection, got %v", err)
	}
}

// TestPricing_ProviderLegacyMapField_RenamedToAliases pins the
// pricing-architecture simplification plan §7.2 promise: a pre-2026-09
// config that still says `pricing.map:` decodes cleanly into the new
// `aliases` field, so migrating users do not have to hand-edit field
// names just to load the file. Resolved pricing must be identical to the
// post-rename `aliases:` form.
func TestPricing_ProviderLegacyMapField_RenamedToAliases(t *testing.T) {
	yaml := pricingCfg("", `pricing:
      map:
        my-alias: gpt-4o
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse with legacy pricing.map: %v", err)
	}
	pol, ok := cfg.ProviderPricingPolicies["p1"]
	if !ok {
		t.Fatalf("provider policy not built: %+v", cfg.ProviderPricingPolicies)
	}
	got, ok := pol.Aliases["my-alias"]
	if !ok || got != "gpt-4o" {
		t.Errorf("Aliases[my-alias] = (%q, %v), want (gpt-4o, true)", got, ok)
	}
	if len(pol.Overrides) != 0 {
		t.Errorf("Overrides = %d rules, want 0 — legacy `map:` must NOT populate rates", len(pol.Overrides))
	}
}

// TestPricing_ProviderLegacyOverridesField_RenamedToRates pins the
// pricing-architecture simplification plan §7.2 promise for `overrides`
// → `rates`. A pre-2026-09 config that still says `pricing.overrides:`
// decodes into the new `rates` field with full resolution parity.
func TestPricing_ProviderLegacyOverridesField_RenamedToRates(t *testing.T) {
	yaml := pricingCfg("", `pricing:
      overrides:
        - {model: gpt-4o, in_fresh: 100, cache_read: 10, cache_write: 100, out: 300}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse with legacy pricing.overrides: %v", err)
	}
	pol, ok := cfg.ProviderPricingPolicies["p1"]
	if !ok {
		t.Fatalf("provider policy not built: %+v", cfg.ProviderPricingPolicies)
	}
	if len(pol.Overrides) != 1 {
		t.Fatalf("Overrides = %d rules, want 1 — legacy `overrides:` must populate rates", len(pol.Overrides))
	}
	if pol.Overrides[0].Model != "gpt-4o" {
		t.Errorf("Overrides[0].Model = %q, want gpt-4o", pol.Overrides[0].Model)
	}
}

// TestPricing_ProviderLegacyDualWriteMapAndAliases_Rejected pins the
// dual-write guard: writing BOTH `map` (legacy) and `aliases` (current)
// for the same meaning is ambiguous, and must be a load-time error rather
// than a silent winner-take-all. Same shape the plan's normalize() sketch
// specified.
func TestPricing_ProviderLegacyDualWriteMapAndAliases_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
      map: {a: gpt-4o}
      aliases: {b: gpt-4o-mini}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "both `map`") || !strings.Contains(err.Error(), "`aliases`") {
		t.Errorf("want dual-write rejection naming both `map` and `aliases`, got %v", err)
	}
}

// TestPricing_ProviderLegacyDualWriteOverridesAndRates_Rejected pins the
// dual-write guard for `overrides` vs `rates`.
func TestPricing_ProviderLegacyDualWriteOverridesAndRates_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
      overrides:
        - {model: gpt-4o, in_fresh: 100, cache_read: 10, cache_write: 100, out: 300}
      rates:
        - {model: gpt-4o-mini, in_fresh: 1, cache_read: 0.1, cache_write: 1, out: 3}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "both `overrides`") || !strings.Contains(err.Error(), "`rates`") {
		t.Errorf("want dual-write rejection naming both `overrides` and `rates`, got %v", err)
	}
}

// TestPricing_ProviderUnknownField_Rejected pins that the legacy-shim
// `UnmarshalYAML` still rejects misspelled keys — the allow-list names
// exactly `currency`/`aliases`/`rates` (current) and `map`/`overrides`
// (legacy); any other key, including typos like `alises:`, is a load-time
// error rather than silently ignored. The error names the offending key
// and the allowed set so the user can self-correct without guessing.
func TestPricing_ProviderUnknownField_Rejected(t *testing.T) {
	yaml := pricingCfg("", `pricing:
      alises: {a: gpt-4o}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `unknown field "alises"`) {
		t.Errorf("want unknown-field rejection naming alises, got %v", err)
	}
}

func TestPricing_MetricCost_Rejected(t *testing.T) {
	yaml := pricingCfg("", `quota:
  limits:
    - {metric: cost, every: 1mo, amount: 100}
`, "gpt-4o")
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "metric: cost is no longer supported") {
		t.Errorf("want a metric: cost rejection with migration guidance, got %v", err)
	}
}

func TestPricing_PricingTable_AlwaysAvailable(t *testing.T) {
	yaml := pricingCfg("", "", "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	table, err := cfg.PricingTable()
	if err != nil || table == nil {
		t.Fatalf("PricingTable() = (%v, %v), want a non-nil table with no error", table, err)
	}
}

// TestPricing_Resolver_EndToEnd_CurrencyConversion wires
// cfg.PricingTable()/cfg.ProviderPricingPolicies into a real
// pricing.Resolver — the exact construction cmd/vmr/cmd_report.go's
// buildPricing performs — and checks the NUMBER RateFor actually returns
// for a provider whose pricing.currency is non-USD is the converted
// figure, not the raw declared number. This is the layer a pricing
// resolution edge case actually manifested at historically ("label one
// currency, compute in another") — a regression here would silently ship
// a wrong number in every vmr analyze run.
func TestPricing_Resolver_EndToEnd_CurrencyConversion(t *testing.T) {
	yaml := pricingCfg("exchange_rate: {CNY: 7.1}\n", `pricing:
  currency: CNY
  rates:
    - {model: "*", in_fresh: 7.1, cache_read: 0.71, cache_write: 8.875, out: 28.4}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	table, err := cfg.PricingTable()
	if err != nil {
		t.Fatalf("PricingTable: %v", err)
	}
	resolver := pricing.NewResolver(table, cfg.ProviderPricingPolicies)
	rate, ok := resolver.RateFor("p1", "gpt-4o")
	if !ok {
		t.Fatal("RateFor: no rate resolved")
	}
	if rate.InFresh == nil || *rate.InFresh < 0.999 || *rate.InFresh > 1.001 {
		t.Fatalf("RateFor.InFresh = %v, want ~1.0 (already converted to USD at load time)", rate.InFresh)
	}
}

// TestQuota_ProviderBlock_SurvivesParse pins the config layer's actual quota
// round-trip contract: a provider-level quota: block survives Parse with every
// Limit's resolved form intact (not just a non-nil pointer). Quota is a
// provider-level concept in the YAML config — EndpointGroup carries no quota;
// the provider's *QuotaConfig becomes a *core.QuotaSpec and is attached to each
// core.Endpoint later, in router/snapshot.go's BuildSnapshot (covered by the
// internal/router quota snapshot tests).
func TestQuota_ProviderBlock_SurvivesParse(t *testing.T) {
	yaml := pricingCfg("", `quota:
  limits:
    - {metric: tokens, every: 1mo, amount: 1000000}
`, "gpt-4o")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(cfg.Providers))
	}
	q := cfg.Providers[0].Quota
	if q == nil || len(q.Limits) != 1 {
		t.Fatalf("provider quota block did not survive Parse: %+v", q)
	}
	l := q.Limits[0].Resolved
	if l.Metric != core.MetricTokens {
		t.Errorf("metric = %q, want tokens", l.Metric)
	}
	if l.EveryText != "1mo" || l.EveryN != 1 || l.EveryUnit != "mo" {
		t.Errorf("every = text %q n %d unit %q, want \"1mo\"/1/mo", l.EveryText, l.EveryN, l.EveryUnit)
	}
	if l.Amount != 1000000 {
		t.Errorf("amount = %v, want 1000000", l.Amount)
	}
	if l.Since.IsZero() {
		t.Error("since must be resolved to its default anchor at parse time, got zero")
	}
}
