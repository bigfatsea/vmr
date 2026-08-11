// Ver 2026-08-07, by Opus 5

// Quota-Aware Routing's YAML-shape config types and their validation — see
// docs/TokenPlan_Quota_Routing_Design_opus-5.md for the full design and its
// "现状与后续计划" section for what's actually shipped. Split from
// config.go per its own archtest line-count budget.
package config

import (
	"fmt"
	"math"
	"regexp"
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/quota"
)

// positiveFinite reports whether v is a usable positive magnitude — i.e.
// NOT NaN and NOT ±Inf, on top of being > 0. The explicit IsNaN/IsInf test
// is the whole point: `v <= 0` is FALSE for NaN, so a plain sign check
// silently admits `.nan` (valid YAML) into every quota/pricing knob, and a
// NaN multiplier poisons everything downstream of it — int64(math.Ceil(NaN))
// is platform-defined garbage, a NaN amount makes every headroom comparison
// false and the candidate sort order arbitrary. Shared by every numeric
// field in this file and pricing.go; see nonNegativeFinite for the price
// components, where an explicit 0.0 is legitimate ("genuinely free").
func positiveFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

// nonNegativeFinite is positiveFinite's variant for price components, where
// 0.0 is a meaningful value (explicitly free) but a negative or non-finite
// one never is — a negative rate would drive Counters.Cost DOWN on every
// charge, making an account look progressively more (not less) unused.
func nonNegativeFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// QuotaConfig is one provider's quota declaration — the YAML-shape
// counterpart of core.QuotaSpec (see config.EndpointGroup -> core.Endpoint
// for the same "YAML shape / runtime shape are separate types" precedent).
// Still exactly one entry in Limits; multiple windows, rolling windows, and
// per-model scope remain documented, explicit load-time errors (see
// LimitConfig.validate) — never a silent no-op; that's P3. TokenWeights and
// ModelMultipliers are P2.1: account-level, apply to every Limit on this
// provider (see core.QuotaSpec's doc comment for why folding them per-Limit
// would just be the same fact copied N times). metric: cost is P2.2 — see
// pricing.go for the providers[].pricing block a cost-metric account needs.
type QuotaConfig struct {
	Limits           []LimitConfig       `yaml:"limits"`
	TokenWeights     *TokenWeightsConfig `yaml:"token_weights"`
	ModelMultipliers map[string]float64  `yaml:"model_multipliers"`

	// ResolvedTokenWeights is TokenWeights' parsed, always-fully-defaulted
	// runtime form, filled in by validateQuota — see TokenWeightsConfig's
	// doc comment for the resolution rule. Not a yaml field.
	ResolvedTokenWeights core.TokenWeights `yaml:"-"`
}

// TokenWeightsConfig is TokenWeights as written in YAML: each component is a
// pointer so "omitted" (nil, resolves to core.DefaultTokenWeight) and
// "explicitly set to 0.0" are distinguishable — the same distinction
// PricingRate's components will need in P2.2 for the same reason (an
// omitted weight isn't "this component doesn't count", it's "I didn't say,
// use the default").
//
// Design note: unlike PricingRate's per-model rates, an *explicit* 0.0
// weight is rejected by validate() below (must be > 0, per the design doc's
// §9.1 validation checklist) — a zero token_weight would silently make a
// whole component invisible to quota accounting, which is a materially
// different (and far more dangerous) failure than a zero *price*, so the
// two are not treated the same way even though both are "0.0 the number".
type TokenWeightsConfig struct {
	InFresh    *float64 `yaml:"in_fresh"`
	CacheRead  *float64 `yaml:"cache_read"`
	CacheWrite *float64 `yaml:"cache_write"`
	Out        *float64 `yaml:"out"`
}

// resolve fills every unset component with core.DefaultTokenWeight and
// validates every set one is > 0 — see TokenWeightsConfig's doc comment for
// why 0.0 is rejected rather than silently accepted. tw==nil (token_weights:
// omitted entirely) resolves to all-default, same as every component being
// individually unset.
func (tw *TokenWeightsConfig) resolve(providerName string) (core.TokenWeights, error) {
	r := core.NewTokenWeights()
	if tw == nil {
		return r, nil
	}
	fields := []struct {
		name string
		val  *float64
		dst  *float64
	}{
		{"in_fresh", tw.InFresh, &r.InFresh},
		{"cache_read", tw.CacheRead, &r.CacheRead},
		{"cache_write", tw.CacheWrite, &r.CacheWrite},
		{"out", tw.Out, &r.Out},
	}
	for _, f := range fields {
		if f.val == nil {
			continue
		}
		if !positiveFinite(*f.val) {
			return core.TokenWeights{}, fmt.Errorf("provider %q: quota.token_weights.%s: must be a finite number > 0 (got %v)", providerName, f.name, *f.val)
		}
		*f.dst = *f.val
	}
	return r, nil
}

// LimitConfig is one window-level quota constraint, as written in YAML —
// see core.Limit's doc comment for the resolved runtime shape this becomes.
// Rolling and Models are declared here (rather than left undeclared and
// relying on strict-YAML's KnownFields to reject them) specifically so a
// user who writes them gets "this capability is planned for a later batch"
// instead of a confusing "unknown field" error — see validate() below.
// A `pricing:` block (providers[].pricing/global pricing:) is deliberately
// NOT declared anywhere in this file — its YAML shape and validation
// (PricingConfig/ProviderPricingConfig, resolvePricing) live in pricing.go
// instead, since it's a distinct config surface from quota limits.
type LimitConfig struct {
	Metric string  `yaml:"metric"`
	Every  string  `yaml:"every"`
	Since  string  `yaml:"since"`
	Amount float64 `yaml:"amount"`
	// Rolling and Models are P1 rejection-only fields — see this type's doc
	// comment. Neither ever reaches core.Limit.
	Rolling bool     `yaml:"rolling"`
	Models  []string `yaml:"models"`

	// Resolved is this Limit's parsed, runtime-ready form, filled in by
	// validate() the moment this entry passes every P1 check — internal/
	// router/snapshot.go reads it directly to build core.QuotaSpec instead
	// of re-parsing Every/Since a second time. Not a yaml field: `yaml:"-"`
	// keeps strict decoding from ever seeing it as user input.
	Resolved core.Limit `yaml:"-"`
}

// everyPattern matches the (every, since) encoding's "every" half: a
// positive integer plus one of the four unit letters — see the design
// doc's Window Implementation section for why (every, since) replaces a
// reset_period+reset_day field pair.
var everyPattern = regexp.MustCompile(`^([0-9]+)(h|d|w|mo)$`)

// parseEvery splits "every" text like "5h"/"2w"/"1mo" into its count and
// unit. config.Duration (time.ParseDuration) can't be reused here — Go's
// duration parser has no concept of days/weeks/months at all, let alone the
// calendar-aware month math internal/quota/period.go implements.
func parseEvery(s string) (n int, unit string, err error) {
	m := everyPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, "", fmt.Errorf("invalid every %q (want N followed by h/d/w/mo, e.g. \"5h\", \"1w\", \"1mo\")", s)
	}
	var count int
	if _, err := fmt.Sscanf(m[1], "%d", &count); err != nil || count <= 0 {
		return 0, "", fmt.Errorf("invalid every %q: count must be a positive integer", s)
	}
	return count, m[2], nil
}

// parseSince parses an explicit `since` value. Two forms are accepted: a
// bare date ("2026-08-14", midnight in fmtutil.DisplayZone — the common
// case, matching how a subscription's start date is normally known) or a
// full RFC3339 timestamp (for the rare case an exact anchor hour matters).
// ok=false with a nil error means the field was empty — the caller applies
// the unit-specific default (quota.DefaultSince) in that case, not this
// function, which has no access to "now" or the unit.
func parseSince(s string) (t time.Time, ok bool, err error) {
	if s == "" {
		return time.Time{}, false, nil
	}
	if d, derr := time.Parse("2006-01-02", s); derr == nil {
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, fmtutil.DisplayZone), true, nil
	}
	if rt, rerr := time.Parse(time.RFC3339, s); rerr == nil {
		return rt, true, nil
	}
	return time.Time{}, false, fmt.Errorf("invalid since %q (want YYYY-MM-DD or RFC3339)", s)
}

// validateQuota checks providerName's quota: block (nil = no quota
// configured, always valid) and resolves every surviving Limit's
// core.Limit form in place. now is the moment to resolve an unset `since`
// default against — passed in (rather than each LimitConfig calling
// time.Now() itself) so a single config.Parse call resolves every Limit's
// default anchor against the same instant.
func validateQuota(providerName string, qc *QuotaConfig, now time.Time) error {
	if qc == nil {
		return nil
	}
	if len(qc.Limits) == 0 {
		return fmt.Errorf("provider %q: quota.limits: at least one entry required when quota: is set", providerName)
	}
	if len(qc.Limits) > 1 {
		return fmt.Errorf("provider %q: quota.limits: %d entries given, but only one is supported in this release — multi-window quota is planned for a later batch (see docs/TokenPlan_Quota_Routing_Design_opus-5.md)", providerName, len(qc.Limits))
	}
	seen := map[string]bool{}
	hasTokensLimit := false
	for i := range qc.Limits {
		if err := (&qc.Limits[i]).validate(providerName, i, now); err != nil {
			return err
		}
		if qc.Limits[i].Resolved.Metric == core.MetricTokens {
			hasTokensLimit = true
		}
		key := string(qc.Limits[i].Resolved.Metric) + "/" + qc.Limits[i].Resolved.EveryText
		if seen[key] {
			return fmt.Errorf("provider %q: quota.limits: duplicate limit key %q (same metric + every)", providerName, key)
		}
		seen[key] = true
	}
	// token_weights only affects a tokens-metric Limit's base(tokens) sum
	// (see core.QuotaSpec's doc comment) — configuring it on an account with
	// no such Limit would be a field that silently never takes effect,
	// which the same fail-fast contract KnownFields enforces everywhere else
	// in this config does not allow.
	if qc.TokenWeights != nil && !hasTokensLimit {
		return fmt.Errorf("provider %q: quota.token_weights is configured but no quota.limits entry uses metric \"tokens\" — token_weights only affects tokens accounting", providerName)
	}
	resolved, err := qc.TokenWeights.resolve(providerName)
	if err != nil {
		return err
	}
	qc.ResolvedTokenWeights = resolved
	for _, model := range core.SortedKeys(qc.ModelMultipliers) {
		if !positiveFinite(qc.ModelMultipliers[model]) {
			return fmt.Errorf("provider %q: quota.model_multipliers[%q]: must be a finite number > 0 (got %v)", providerName, model, qc.ModelMultipliers[model])
		}
	}
	// model_multipliers is a pure accounting-unit concept (see the design
	// doc's "折扣与促销归入价格层" section) — it only ever multiplies a requests/tokens Limit's
	// base(metric). An account whose only Limit is metric: cost gets its
	// entire price differentiation from providers[].pricing instead
	// (per-model rates), so a model_multipliers block there would be
	// silently unused — the same fail-fast contract as token_weights
	// above, not a case P2.1's "requests/tokens don't need this
	// restriction" reasoning extends to.
	if len(qc.ModelMultipliers) > 0 && len(qc.Limits) == 1 && qc.Limits[0].Resolved.Metric == core.MetricCost {
		return fmt.Errorf("provider %q: quota.model_multipliers is configured but this account's only quota.limits entry is metric: cost — cost accounts express per-model price differences via providers[].pricing instead (model_multipliers would never take effect)", providerName)
	}
	return nil
}

// validate checks one LimitConfig and, on success, fills in Resolved. Every
// knob not yet supported (rolling windows, per-model scope — cost metric IS
// supported, see the "cost" case below) is rejected here with a message
// that names the capability and says it's planned, not "invalid" or
// "unsupported forever" — see docs/TokenPlan_Quota_Routing_Design_opus-5.md's
// P3 batch description, which treats a silently ignored quota field as the
// one failure mode this project cannot tolerate (the same fail-fast
// contract KnownFields already enforces everywhere else in this config).
func (lc *LimitConfig) validate(providerName string, idx int, now time.Time) error {
	switch lc.Metric {
	case "requests":
		lc.Resolved.Metric = core.MetricRequests
	case "tokens":
		lc.Resolved.Metric = core.MetricTokens
	case "cost":
		// Structurally accepted here — the actual pricing completeness
		// check (does this account resolve a full four-component rate for
		// every model it's configured to serve) happens later, in
		// Config.resolvePricing, once the models: block has been walked to
		// know which upstream models this provider must be priced for.
		lc.Resolved.Metric = core.MetricCost
	case "":
		return fmt.Errorf("provider %q: quota.limits[%d]: metric is required (requests|tokens|cost)", providerName, idx)
	default:
		return fmt.Errorf("provider %q: quota.limits[%d]: unknown metric %q (want requests|tokens|cost)", providerName, idx, lc.Metric)
	}
	if lc.Rolling {
		return fmt.Errorf("provider %q: quota.limits[%d]: rolling windows are not supported in this release — this capability is planned for a later batch; use a tumbling window (omit rolling, or set it to false)", providerName, idx)
	}
	if len(lc.Models) > 0 {
		return fmt.Errorf("provider %q: quota.limits[%d]: per-model quota scope (models:) is not supported in this release — this capability is planned for a later batch", providerName, idx)
	}
	n, unit, err := parseEvery(lc.Every)
	if err != nil {
		return fmt.Errorf("provider %q: quota.limits[%d]: every: %w", providerName, idx, err)
	}
	if !positiveFinite(lc.Amount) {
		return fmt.Errorf("provider %q: quota.limits[%d]: amount must be a finite number > 0 (got %v)", providerName, idx, lc.Amount)
	}
	since, explicit, err := parseSince(lc.Since)
	if err != nil {
		return fmt.Errorf("provider %q: quota.limits[%d]: since: %w", providerName, idx, err)
	}
	if !explicit {
		since = quota.DefaultSince(unit, now)
	}
	lc.Resolved = core.Limit{
		Metric:    lc.Resolved.Metric,
		EveryN:    n,
		EveryUnit: unit,
		EveryText: lc.Every,
		Since:     since,
		Amount:    lc.Amount,
	}
	return nil
}
