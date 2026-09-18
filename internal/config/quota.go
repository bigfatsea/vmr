// Ver 2026-08-22, by Sonnet 5

// Quota-Aware Routing's YAML-shape config types and their validation — see
// docs/VirtualModelRouter_Design_v4_Quota.md for the full design and its
// "现状与后续计划" section for what's actually shipped. Split from
// config.go per its own archtest line-count budget.
package config

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/quota"
)

// positiveFinite reports whether v is a usable positive magnitude — i.e.
// NOT NaN and NOT ±Inf, on top of being > 0. The explicit IsNaN/IsInf test
// is the whole point: `v <= 0` is FALSE for NaN, so a plain sign check
// silently admits `.nan` (valid YAML) into every quota/pricing knob, and a
// NaN multiplier poisons everything downstream of it — it propagates through
// quota.ApplyModelMultiplier's multiplication and quota.Counters.Add's
// accumulation forever (NaN + x is always NaN), a NaN amount makes every
// headroom comparison false and the candidate sort order arbitrary. Shared
// by every numeric field in this file and pricing.go; see nonNegativeFinite
// for the price components, where an explicit 0.0 is legitimate ("genuinely
// free").
func positiveFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

// nonNegativeFinite is positiveFinite's variant for price components, where
// 0.0 is a meaningful value (explicitly free) but a negative or non-finite
// one never is — a negative rate would understate spend in vmr analyze's $
// estimates, making an account with a data-entry typo look progressively
// cheaper.
func nonNegativeFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// QuotaConfig is one provider's quota declaration — the YAML-shape
// counterpart of core.QuotaSpec (see config.EndpointGroup -> core.Endpoint
// for the same "YAML shape / runtime shape are separate types" precedent).
// One or more Limits, each carrying its own metric/window/Scope. Only
// two metrics: requests and tokens — see LimitConfig.validate's "cost" case
// for why a metric: cost Limit is a load-time error, not a supported value.
//
// TokenWeights and ModelMultipliers sit at the provider quota: level (peer to
// limits:) so that multi-window configurations (e.g. short-term rate gate +
// monthly budget bucket) share the exact same scaling rules without repetition.
// TokenWeights applies to every metric: tokens Limit under this provider (and
// is safely ignored by metric: requests Limits); ModelMultipliers applies to
// every charge under this provider across all metrics.
type QuotaConfig struct {
	Limits           []LimitConfig       `yaml:"limits"`
	TokenWeights     *TokenWeightsConfig `yaml:"token_weights"`
	ModelMultipliers map[string]float64  `yaml:"model_multipliers"`
}

// TokenWeightsConfig is TokenWeights as written in YAML: each component is a
// pointer so "omitted" (nil, resolves to core.DefaultTokenWeight) and
// "explicitly set to 0.0" are distinguishable — an omitted weight isn't
// "this component doesn't count", it's "I didn't say, use the default".
//
// Design note: unlike a providers[].pricing.rates row (where an explicit
// 0.0 legitimately means "free"), an *explicit* 0.0 weight is rejected by
// validate() below (must be > 0, per the design doc's §9.1 validation
// checklist) — a zero token_weight would silently make a whole component
// invisible to quota accounting, which is a materially different (and far
// more dangerous) failure than a zero *price*.
type TokenWeightsConfig struct {
	InFresh    *float64 `yaml:"in_fresh"`
	CacheRead  *float64 `yaml:"cache_read"`
	CacheWrite *float64 `yaml:"cache_write"`
	Out        *float64 `yaml:"out"`
}

// Resolve fills every unset component with core.DefaultTokenWeight and
// validates every set one is > 0 — see TokenWeightsConfig's doc comment for
// why 0.0 is rejected rather than silently accepted. tw==nil (token_weights:
// omitted entirely) resolves to all-default, same as every component being
// individually unset. fieldPath names this occurrence in error messages
// (e.g. "quota.token_weights").
func (tw *TokenWeightsConfig) Resolve(providerName, fieldPath string) (core.TokenWeights, error) {
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
			return core.TokenWeights{}, fmt.Errorf("provider %q: %s.%s: must be a finite number > 0 (got %v)", providerName, fieldPath, f.name, *f.val)
		}
		*f.dst = *f.val
	}
	return r, nil
}

// LimitConfig is one window-level quota constraint, as written in YAML —
// see core.Limit's doc comment for the resolved runtime shape this becomes.
// Rolling is declared here (rather than left undeclared and relying on
// strict-YAML's KnownFields to reject it) specifically so a user who writes
// it gets "this capability is planned for a later batch" instead of a
// confusing "unknown field" error — see validate() below. A provider's
// `pricing:` block is deliberately NOT declared anywhere in this file —
// its YAML shape and validation (ProviderPricingConfig, resolvePricing)
// live in pricing.go instead, a distinct config surface with no runtime
// coupling to quota limits at all (see core.PricingSpec's doc comment).
//
// Models (Scope) is per-Limit. TokenWeights and ModelMultipliers are declared
// here purely as a migration trap: they now live at the provider quota: level
// (peer to limits:) so multiple windows share one set of modifiers. Writing
// them inside a limits[] entry produces a load-time error naming the new
// location — see LimitConfig.validate below.
type LimitConfig struct {
	Metric           string              `yaml:"metric"`
	Every            string              `yaml:"every"`
	Since            string              `yaml:"since"`
	Amount           float64             `yaml:"amount"`
	Models           []string            `yaml:"models"`
	TokenWeights     *TokenWeightsConfig `yaml:"token_weights"`
	ModelMultipliers map[string]float64  `yaml:"model_multipliers"`
	// Rolling is a P1-era rejection-only field — see this type's doc
	// comment. It never reaches core.Limit.
	Rolling bool `yaml:"rolling"`

	// Resolved is this Limit's parsed, runtime-ready form, filled in by
	// validate() the moment this entry passes every check — internal/
	// router/snapshot.go reads it directly to build core.QuotaSpec instead
	// of re-parsing Every/Since a second time. Not a yaml field: `yaml:"-"`
	// keeps strict decoding from ever seeing it as user input.
	Resolved core.Limit `yaml:"-"`
}

// everyPattern matches the (every, since) encoding's "every" half: a
// positive integer plus one of the five unit letters — see the design
// doc's Window Implementation section for why (every, since) replaces a
// reset_period+reset_day field pair.
var everyPattern = regexp.MustCompile(`^([0-9]+)(min|h|d|w|mo)$`)

// parseEvery splits "every" text like "5h"/"2w"/"1mo"/"1min" into its count
// and unit. config.Duration (time.ParseDuration) can't be reused here — Go's
// duration parser has no concept of days/weeks/months at all, let alone the
// calendar-aware month math internal/quota/period.go implements.
func parseEvery(s string) (n int, unit string, err error) {
	m := everyPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, "", fmt.Errorf("invalid every %q (want N followed by min/h/d/w/mo, e.g. \"1min\", \"5h\", \"1w\")", s)
	}
	var count int
	if _, err := fmt.Sscanf(m[1], "%d", &count); err != nil || count <= 0 {
		return 0, "", fmt.Errorf("invalid every %q: count must be a positive integer", s)
	}
	return count, m[2], nil
}

// pureTimePattern matches a bare clock time with no date component
// ("15:04" or "15:04:05") — see parseSince's doc comment for why this form
// exists and is restricted to "min"/"h" Limits.
var pureTimePattern = regexp.MustCompile(`^([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?$`)

// parseSince parses an explicit `since` value. Three forms are accepted: a
// bare date ("2026-08-14", midnight in fmtutil.DisplayZone — the common
// case, matching how a subscription's start date is normally known), a full
// RFC3339 timestamp (for the rare case an exact anchor hour/minute
// matters), or a bare clock time ("15:04" / "15:04:05", today in
// fmtutil.DisplayZone) — the third form only makes sense for a "min"/"h"
// Limit, where "which calendar day" is irrelevant and RFC3339 would force
// spelling out an arbitrary date just to say "align to the top of the
// hour" (see docs/VirtualModelRouter_Design_v4_Quota.md §9.1 and §12.2).
// unit enforces that restriction; ok=false with a
// nil error means the field was empty — the caller applies the default
// (quota.DefaultSince) in that case, not this function, which has no
// access to "now".
func parseSince(s, unit string, now time.Time) (t time.Time, ok bool, err error) {
	if s == "" {
		return time.Time{}, false, nil
	}
	if d, derr := time.Parse("2006-01-02", s); derr == nil {
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, fmtutil.DisplayZone), true, nil
	}
	if rt, rerr := time.Parse(time.RFC3339, s); rerr == nil {
		return rt, true, nil
	}
	if m := pureTimePattern.FindStringSubmatch(s); m != nil {
		if unit != "min" && unit != "h" {
			return time.Time{}, false, fmt.Errorf("invalid since %q: a bare clock time is only allowed for every: min/h Limits (this Limit's every unit is %q) — use YYYY-MM-DD or RFC3339 instead", s, unit)
		}
		var hh, mm, ss int
		if _, serr := fmt.Sscanf(m[1], "%d", &hh); serr != nil || hh > 23 {
			return time.Time{}, false, fmt.Errorf("invalid since %q: hour out of range", s)
		}
		if _, serr := fmt.Sscanf(m[2], "%d", &mm); serr != nil || mm > 59 {
			return time.Time{}, false, fmt.Errorf("invalid since %q: minute out of range", s)
		}
		if m[3] != "" {
			if _, serr := fmt.Sscanf(m[3], "%d", &ss); serr != nil || ss > 59 {
				return time.Time{}, false, fmt.Errorf("invalid since %q: second out of range", s)
			}
		}
		nowInZone := now.In(fmtutil.DisplayZone)
		y, mo, d := nowInZone.Date()
		return time.Date(y, mo, d, hh, mm, ss, 0, fmtutil.DisplayZone), true, nil
	}
	return time.Time{}, false, fmt.Errorf("invalid since %q (want YYYY-MM-DD, RFC3339, or hh:mm[:ss] for min/h Limits)", s)
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
	tw, err := qc.TokenWeights.Resolve(providerName, "quota.token_weights")
	if err != nil {
		return err
	}
	for _, model := range fmtutil.SortedKeys(qc.ModelMultipliers) {
		if !positiveFinite(qc.ModelMultipliers[model]) {
			return fmt.Errorf("provider %q: quota.model_multipliers[%q]: must be a finite number > 0 (got %v)", providerName, model, qc.ModelMultipliers[model])
		}
	}
	for i := range qc.Limits {
		if err := (&qc.Limits[i]).validate(providerName, i, now, tw, qc.ModelMultipliers); err != nil {
			return err
		}
	}
	// Pairwise collision check: a per-model Limit's bucket key depends on
	// which real model gets charged, not on anything computable from the
	// Limit alone (see quota.LimitKey's doc comment — a "*" Scope's
	// membership is open-ended), so two Limits can no longer be deduped by
	// comparing one static key each. What actually matters is whether they
	// could ever produce the SAME live bucket key for some real model:
	// same metric+every, and (both shared, or both per-model with
	// overlapping Scope). A shared Limit and a per-model Limit on the same
	// metric+every never collide — their key shapes differ ("metric/every"
	// vs "metric/every#model=X") — and stacking exactly that combination is
	// a legitimate, intentional pattern (a provider-wide shared pool plus an
	// independent sub-allowance for one premium model), not an error.
	for i := range qc.Limits {
		for j := i + 1; j < len(qc.Limits); j++ {
			a, b := qc.Limits[i].Resolved, qc.Limits[j].Resolved
			if a.Metric != b.Metric || a.EveryText != b.EveryText {
				continue
			}
			collide := quota.PerModel(a) == quota.PerModel(b) &&
				(!quota.PerModel(a) || quota.ModelSetsOverlap(a.Models, b.Models))
			if collide {
				return fmt.Errorf("provider %q: quota.limits[%d] and quota.limits[%d]: duplicate limit key — same metric+every and overlapping (or both absent) models scope would collide on the same bucket", providerName, i, j)
			}
		}
	}
	return nil
}

// validate checks one LimitConfig and, on success, fills in Resolved. Every
// knob not yet supported (rolling windows — Scope IS supported, see the
// models case below) is rejected here with a message that names the
// capability and says it's planned, not "invalid" or "unsupported
// forever" — see docs/VirtualModelRouter_Design_v4_Quota.md's design
// specification, which treats a silently ignored quota field as the one
// failure mode this project cannot tolerate (the same fail-fast contract
// KnownFields already enforces everywhere else in this config).
func (lc *LimitConfig) validate(providerName string, idx int, now time.Time, tw core.TokenWeights, mm map[string]float64) error {
	fieldPrefix := fmt.Sprintf("quota.limits[%d]", idx)
	if lc.TokenWeights != nil || len(lc.ModelMultipliers) > 0 {
		return fmt.Errorf("provider %q: %s: token_weights and model_multipliers now belong at the provider quota level (peer to limits:) — move them up to quota.token_weights / quota.model_multipliers to share them across all limits (see docs/VirtualModelRouter_Design_v4_Quota.md)", providerName, fieldPrefix)
	}
	switch lc.Metric {
	case "requests":
		lc.Resolved.Metric = core.MetricRequests
	case "tokens":
		lc.Resolved.Metric = core.MetricTokens
	case "cost":
		return fmt.Errorf("provider %q: %s: metric: cost is no longer supported — express the same budget as a tokens limit (convert once: budget ÷ price), optionally with model_multipliers/token_weights to weight expensive models; $ cost estimates remain available in vmr analyze", providerName, fieldPrefix)
	case "":
		return fmt.Errorf("provider %q: %s: metric is required (requests|tokens)", providerName, fieldPrefix)
	default:
		return fmt.Errorf("provider %q: %s: unknown metric %q (want requests|tokens)", providerName, fieldPrefix, lc.Metric)
	}
	if lc.Rolling {
		return fmt.Errorf("provider %q: %s: rolling windows are not supported in this release — this capability is planned for a later batch; use a tumbling window (omit rolling, or set it to false)", providerName, fieldPrefix)
	}
	n, unit, err := parseEvery(lc.Every)
	if err != nil {
		return fmt.Errorf("provider %q: %s: every: %w", providerName, fieldPrefix, err)
	}
	if !positiveFinite(lc.Amount) {
		return fmt.Errorf("provider %q: %s: amount must be a finite number > 0 (got %v)", providerName, fieldPrefix, lc.Amount)
	}
	since, explicit, err := parseSince(lc.Since, unit, now)
	if err != nil {
		return fmt.Errorf("provider %q: %s: since: %w", providerName, fieldPrefix, err)
	}
	if !explicit {
		since = quota.DefaultSince(now, unit)
	}
	models, err := resolveModelsScope(providerName, fieldPrefix, lc.Models)
	if err != nil {
		return err
	}
	limitTW := core.NewTokenWeights()
	if lc.Resolved.Metric == core.MetricTokens {
		limitTW = tw
	}
	lc.Resolved = core.Limit{
		Metric:           lc.Resolved.Metric,
		EveryN:           n,
		EveryUnit:        unit,
		EveryText:        lc.Every,
		Since:            since,
		Amount:           lc.Amount,
		Models:           models,
		TokenWeights:     limitTW,
		ModelMultipliers: mm,
	}
	return nil
}

// resolveModelsScope validates a Limit's Scope (`models:`) — see
// core.Limit.Models' doc comment for the three shapes this produces.
// Nil/empty input (the common case) resolves to nil — "shared, applies to
// every model on this provider with one combined pool". A single `"*"`
// entry resolves to the reserved wildcard shape (`[]string{"*"}`) —
// "per-model, applies to every model, each with its own independent pool".
// Anything else is a restricted per-model list: every entry must be a
// non-empty, non-duplicate upstream model name, sorted so quota.LimitKey's
// callers (and `vmr check`'s display) see a deterministic order regardless
// of how the user wrote the list.
func resolveModelsScope(providerName, fieldPrefix string, models []string) ([]string, error) {
	if len(models) == 0 {
		return nil, nil
	}
	if quota.IsWildcardModels(models) {
		return models, nil
	}
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, m := range models {
		if m == "" {
			return nil, fmt.Errorf("provider %q: %s.models: entries must not be empty", providerName, fieldPrefix)
		}
		if m == "*" {
			return nil, fmt.Errorf("provider %q: %s.models: %q is a reserved wildcard token and must be the ONLY entry — combining it with a named model is redundant, the wildcard already covers it", providerName, fieldPrefix, m)
		}
		if seen[m] {
			return nil, fmt.Errorf("provider %q: %s.models: duplicate entry %q", providerName, fieldPrefix, m)
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}
