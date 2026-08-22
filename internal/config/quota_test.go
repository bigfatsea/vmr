// Ver 2026-08-07, by Opus 5

package config

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

const quotaBaseYAML = `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
    quota:
%s
models:
  m1:
    endpoints:
      - protocol: openai
        providers: [p1]
        models: [real-model]
`

func withQuotaBlock(block string) string {
	// Every line of block gets indented under quota: — callers pass raw
	// YAML starting at "limits:".
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		b.WriteString("      " + line + "\n")
	}
	return strings.Replace(quotaBaseYAML, "%s\n", b.String(), 1)
}

func TestQuota_HappyPath_Requests(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := cfg.ProviderByName("p1")
	if !ok || p.Quota == nil || len(p.Quota.Limits) != 1 {
		t.Fatalf("quota not parsed: %+v", p.Quota)
	}
	l := p.Quota.Limits[0].Resolved
	if l.Metric != core.MetricRequests || l.EveryN != 1 || l.EveryUnit != "mo" || l.Amount != 90000 {
		t.Fatalf("resolved limit = %+v, want requests/1mo/90000", l)
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, l.Since.Location())
	if !l.Since.Equal(want) {
		t.Fatalf("since = %v, want %v", l.Since, want)
	}
}

func TestQuota_HappyPath_Tokens_DefaultSince(t *testing.T) {
	before := time.Now()
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1w, amount: 65000000}`)
	cfg, err := Parse([]byte(yaml))
	after := time.Now()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	l := p.Quota.Limits[0].Resolved
	if l.Metric != core.MetricTokens || l.EveryUnit != "w" {
		t.Fatalf("resolved limit = %+v", l)
	}
	// Omitting since anchors the window at load time itself — no calendar
	// alignment — so the resolved Since must fall within [before, after].
	if l.Since.Before(before) || l.Since.After(after) {
		t.Fatalf("default since = %v, want within [%v, %v] (load-time instant)", l.Since, before, after)
	}
}

func TestQuota_HappyPath_MultipleLimits(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 5h, amount: 6000}
  - {metric: requests, every: 1w, amount: 45000}
  - {metric: requests, every: 1mo, amount: 90000}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := cfg.ProviderByName("p1")
	if !ok || p.Quota == nil || len(p.Quota.Limits) != 3 {
		t.Fatalf("quota not parsed: %+v", p.Quota)
	}
}

func TestQuota_HappyPath_MinUnit(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1min, amount: 60}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	l := p.Quota.Limits[0].Resolved
	if l.EveryUnit != "min" || l.EveryN != 1 {
		t.Fatalf("resolved limit = %+v, want every=1min", l)
	}
}

func TestQuota_HappyPath_PerLimitTokenWeightsAndScope(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1mo, amount: 100000000, token_weights: {cache_read: 0.1, out: 4.0}, models: [heavy-model]}
  - {metric: requests, every: 1d, amount: 10000, model_multipliers: {"deepseek-r1": 2.0}}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	l0 := p.Quota.Limits[0].Resolved
	if l0.TokenWeights.CacheRead != 0.1 || l0.TokenWeights.Out != 4.0 {
		t.Fatalf("limit[0].TokenWeights = %+v, want cache_read=0.1 out=4.0", l0.TokenWeights)
	}
	if len(l0.Models) != 1 || l0.Models[0] != "heavy-model" {
		t.Fatalf("limit[0].Models = %v, want [heavy-model]", l0.Models)
	}
	l1 := p.Quota.Limits[1].Resolved
	if l1.ModelMultipliers["deepseek-r1"] != 2.0 {
		t.Fatalf("limit[1].ModelMultipliers = %v, want deepseek-r1=2.0", l1.ModelMultipliers)
	}
}

func TestQuota_Reject_AccountLevelTokenWeights(t *testing.T) {
	yaml := withQuotaBlock(`token_weights: {cache_read: 0.1}
limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "no longer account-level") {
		t.Errorf("want account-level token_weights migration error, got %v", err)
	}
}

func TestQuota_Reject_AccountLevelModelMultipliers(t *testing.T) {
	yaml := withQuotaBlock(`model_multipliers: {"*": 2}
limits:
  - {metric: requests, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "no longer account-level") {
		t.Errorf("want account-level model_multipliers migration error, got %v", err)
	}
}

func TestQuota_Reject_DuplicateLimitKey_SameScope(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1d, amount: 100, models: [m1]}
  - {metric: requests, every: 1d, amount: 200, models: [m1]}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "duplicate limit key") {
		t.Errorf("want duplicate limit key rejection, got %v", err)
	}
}

func TestQuota_Accept_SameMetricEveryDifferentScope(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1d, amount: 200, models: [premium-model]}
  - {metric: requests, every: 1d, amount: 50000}`)
	_, err := Parse([]byte(yaml))
	if err != nil {
		t.Errorf("want two same-window Limits distinguished by Scope to be accepted, got %v", err)
	}
}

// --- models: "*" wildcard (per-model, unrestricted) ---

func TestQuota_HappyPath_WildcardModels(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1min, amount: 60, models: ["*"]}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	l := p.Quota.Limits[0].Resolved
	if len(l.Models) != 1 || l.Models[0] != "*" {
		t.Fatalf("Models = %v, want [\"*\"]", l.Models)
	}
}

func TestQuota_Reject_WildcardCombinedWithNamedModel(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1min, amount: 60, models: ["*", "deepseek-r1"]}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "reserved wildcard token") {
		t.Errorf("want wildcard-combined-with-named-model rejection, got %v", err)
	}
}

func TestQuota_Reject_DuplicateLimitKey_WildcardOverlapsEverything(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1d, amount: 100, models: ["*"]}
  - {metric: requests, every: 1d, amount: 200, models: [some-model]}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "duplicate limit key") {
		t.Errorf("want wildcard-overlaps-everything rejection, got %v", err)
	}
}

func TestQuota_Accept_DisjointModelLists(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1d, amount: 100, models: [a, b]}
  - {metric: requests, every: 1d, amount: 200, models: [c, d]}`)
	_, err := Parse([]byte(yaml))
	if err != nil {
		t.Errorf("want two per-model Limits with disjoint Scope to be accepted, got %v", err)
	}
}

func TestQuota_Reject_OverlappingModelLists(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1d, amount: 100, models: [a, b]}
  - {metric: requests, every: 1d, amount: 200, models: [b, c]}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "duplicate limit key") {
		t.Errorf("want overlapping-Scope-lists rejection (both would charge model b into the same bucket), got %v", err)
	}
}

func TestQuota_NoQuotaBlock_UnaffectedByDefault(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := cfg.ProviderByName("p1")
	if p.Quota != nil {
		t.Fatalf("Quota = %+v, want nil when not configured", p.Quota)
	}
}

// --- §2.2 reject cases: every one must be a load-time error, never silent. ---

// TestQuota_MetricCost_WithoutPricingCurrency_Rejected verifies that metric: cost requires a pricing currency:
// completeness gate at the point closest to "cost" alone: metric: cost now
// parses structurally, but an account with no pricing.currency configured
// at all can't be charged in anything — see resolvePricing.
func TestQuota_MetricCost_WithoutPricingCurrency_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: cost, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "pricing.currency") {
		t.Errorf("want a pricing.currency-not-set rejection, got %v", err)
	}
}

func TestQuota_Reject_Rolling(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 5h, rolling: true, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "rolling") {
		t.Errorf("want rolling-window rejection, got %v", err)
	}
}

// --- model_multipliers / token_weights are per-Limit fields (P3) ---

func TestQuota_ModelMultipliers_HappyPath(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, amount: 100, model_multipliers: {"*": 1, heavy-model: 9}}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	mm := p.Quota.Limits[0].Resolved.ModelMultipliers
	if got := mm["heavy-model"]; got != 9 {
		t.Fatalf("model_multipliers[heavy-model] = %v, want 9", got)
	}
	if got := mm["*"]; got != 1 {
		t.Fatalf(`model_multipliers["*"] = %v, want 1`, got)
	}
}

func TestQuota_ModelMultipliers_ZeroOrNegative_Rejected(t *testing.T) {
	for _, bad := range []string{"0", "-1"} {
		yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, amount: 100, model_multipliers: {heavy-model: ` + bad + `}}`)
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), "model_multipliers") {
			t.Errorf("multiplier=%s: want model_multipliers rejection, got %v", bad, err)
		}
	}
}

func TestQuota_ModelMultipliers_OnCostLimit_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: cost, every: 1mo, amount: 100, model_multipliers: {"*": 2}}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "model_multipliers is configured but this Limit's metric is cost") {
		t.Errorf("want model_multipliers-on-cost rejection, got %v", err)
	}
}

// TestQuota_TokenWeights_ZeroValueIsNotDefault pins a real footgun: a Limit
// with no token_weights: at all must resolve to core.DefaultTokenWeight
// (1.0) on every component, never Go's TokenWeights{} zero value (all 0),
// which would silently zero out that Limit's whole tokens accounting.
func TestQuota_TokenWeights_ZeroValueIsNotDefault(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	w := p.Quota.Limits[0].Resolved.TokenWeights
	want := core.TokenWeights{InFresh: 1, CacheRead: 1, CacheWrite: 1, Out: 1}
	if w != want {
		t.Fatalf("TokenWeights = %+v, want %+v (all-default, not the zero value)", w, want)
	}
}

func TestQuota_TokenWeights_PartialOverride_RestDefault(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1mo, amount: 100, token_weights: {cache_read: 0.1}}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	w := p.Quota.Limits[0].Resolved.TokenWeights
	want := core.TokenWeights{InFresh: 1, CacheRead: 0.1, CacheWrite: 1, Out: 1}
	if w != want {
		t.Fatalf("TokenWeights = %+v, want %+v (only cache_read overridden)", w, want)
	}
}

func TestQuota_TokenWeights_ExplicitZero_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1mo, amount: 100, token_weights: {cache_read: 0}}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "token_weights.cache_read") {
		t.Errorf("want explicit-zero token_weights rejection, got %v", err)
	}
}

func TestQuota_TokenWeights_NegativeRejected(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1mo, amount: 100, token_weights: {out: -2}}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "token_weights.out") {
		t.Errorf("want negative token_weights rejection, got %v", err)
	}
}

func TestQuota_TokenWeights_OnNonTokensLimit_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, amount: 100, token_weights: {cache_read: 0.1}}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "token_weights is configured but this Limit's metric is") {
		t.Errorf("want token_weights-on-non-tokens-limit rejection, got %v", err)
	}
}

// TestQuota_PricingBlock_AcceptedWhenUnused verifies pricing block acceptance:
// a top-level pricing: block is now a real, known field — accepted (and
// simply unused) on a config with no metric: cost provider at all. See
// internal/config/pricing_test.go for the full pricing test matrix.
func TestQuota_PricingBlock_AcceptedWhenUnused(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\npricing:\n  currency: USD", 1)
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Errorf("pricing: block should now be a known, accepted field, got %v", err)
	}
}

func TestQuota_Reject_EmptyLimits(t *testing.T) {
	yaml := withQuotaBlock(`limits: []`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "at least one entry") {
		t.Errorf("want empty-limits rejection, got %v", err)
	}
}

func TestQuota_Reject_BadEverySyntax(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: monthly, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "invalid every") {
		t.Errorf("want bad every: syntax rejection, got %v", err)
	}
}

func TestQuota_Reject_ZeroAmount(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, amount: 0}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "amount must be a finite number > 0") {
		t.Errorf("want zero-amount rejection, got %v", err)
	}
}

func TestQuota_Reject_UnknownMetric(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: dollars, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown metric") {
		t.Errorf("want unknown-metric rejection, got %v", err)
	}
}

func TestQuota_Reject_MissingMetric(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "metric is required") {
		t.Errorf("want missing-metric rejection, got %v", err)
	}
}

func TestQuota_Reject_DuplicateLimitKey(t *testing.T) {
	qc := &QuotaConfig{Limits: []LimitConfig{
		{Metric: "requests", Every: "1mo", Amount: 100},
		{Metric: "requests", Every: "1mo", Amount: 200},
	}}
	err := validateQuota("p1", qc, time.Now())
	if err == nil || !strings.Contains(err.Error(), "duplicate limit key") {
		t.Fatalf("want duplicate limit key rejection, got %v", err)
	}
}

func TestQuota_Reject_InvalidSince(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, since: "not-a-date", amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "since") {
		t.Errorf("want invalid since rejection, got %v", err)
	}
}

func TestQuota_HappyPath_PureTimeSince_MinAndH(t *testing.T) {
	for _, tc := range []struct {
		since               string
		wantH, wantM, wantS int
	}{
		{"09:00", 9, 0, 0},
		{"23:15:30", 23, 15, 30},
	} {
		yaml := withQuotaBlock(fmt.Sprintf(`limits:
  - {metric: requests, every: 1h, since: %q, amount: 100}`, tc.since))
		cfg, err := Parse([]byte(yaml))
		if err != nil {
			t.Fatalf("since=%q: Parse: %v", tc.since, err)
		}
		p, _ := cfg.ProviderByName("p1")
		l := p.Quota.Limits[0].Resolved
		h, m, s := l.Since.Clock()
		if h != tc.wantH || m != tc.wantM || s != tc.wantS {
			t.Fatalf("since=%q: resolved clock = %02d:%02d:%02d, want %02d:%02d:%02d", tc.since, h, m, s, tc.wantH, tc.wantM, tc.wantS)
		}
	}
}

func TestQuota_Reject_PureTimeSince_OnNonMinH(t *testing.T) {
	for _, every := range []string{"1d", "1w", "1mo"} {
		yaml := withQuotaBlock(fmt.Sprintf(`limits:
  - {metric: requests, every: %q, since: "09:00", amount: 100}`, every))
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), "min/h") {
			t.Errorf("every=%q: want bare-clock-time rejection naming min/h, got %v", every, err)
		}
	}
}

// TestQuota_Reject_NonFiniteNumbers pins the NaN/Inf hole a plain `v <= 0`
// sign check leaves open: YAML's .nan/.inf are valid scalars, `NaN <= 0` is
// false, and a NaN that reaches ApplyModelMultiplier/headroom scoring
// poisons every accumulated quota.Counters value it touches instead of
// producing a config error.
func TestQuota_Reject_NonFiniteNumbers(t *testing.T) {
	cases := []struct{ name, block, want string }{
		{"amount NaN", `limits:
  - {metric: requests, every: 1mo, amount: .nan}`, "amount must be a finite number"},
		{"amount Inf", `limits:
  - {metric: requests, every: 1mo, amount: .inf}`, "amount must be a finite number"},
		{"model_multipliers NaN", `limits:
  - {metric: requests, every: 1mo, amount: 100, model_multipliers: {"*": .nan}}`, "model_multipliers"},
		{"token_weights Inf", `limits:
  - {metric: tokens, every: 1mo, amount: 100, token_weights: {in_fresh: .inf}}`, "token_weights.in_fresh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(withQuotaBlock(tc.block)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q rejection, got %v", tc.want, err)
			}
		})
	}
}
