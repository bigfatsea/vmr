// Ver 2026-08-07, by Opus 5

// Quota-Aware Routing's YAML-shape config types and their validation — see
// docs/TokenPlan_Quota_Routing_Design_opus-5.md for the full design and
// docs/TokenPlan_Quota_P1_DevPlan_opus-5.md for what P1 actually delivers.
// Split from config.go per that dev plan's file-size budget (config.go
// already has its own archtest line-count budget).
package config

import (
	"fmt"
	"regexp"
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/quota"
)

// QuotaConfig is one provider's quota declaration — the YAML-shape
// counterpart of core.QuotaSpec (see config.EndpointGroup -> core.Endpoint
// for the same "YAML shape / runtime shape are separate types" precedent).
// P1 accepts exactly one entry in Limits; everything else about the design
// doc's full model (multiple windows, cost metric, rolling windows,
// per-model scope, account-level token_weights/model_multipliers) is a
// documented, explicit load-time error below — never a silent no-op.
type QuotaConfig struct {
	Limits []LimitConfig `yaml:"limits"`
}

// LimitConfig is one window-level quota constraint, as written in YAML —
// see core.Limit's doc comment for the resolved runtime shape this becomes.
// Rolling and Models are declared here (rather than left undeclared and
// relying on strict-YAML's KnownFields to reject them) specifically so a
// user who writes them gets "this capability is planned for a later batch"
// instead of a confusing "unknown field" error — see validate() below.
// model_multipliers/token_weights (account-level, P2) and any `pricing:`
// block are deliberately NOT declared anywhere in this file — those DO rely
// on KnownFields' ordinary unknown-field rejection, since P1 has no
// specific guidance to offer about them beyond "not yet supported".
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
		return fmt.Errorf("provider %q: quota.limits: %d entries given, but only one is supported in this release — multi-window quota is planned for a later batch (see docs/TokenPlan_Quota_P1_DevPlan_opus-5.md)", providerName, len(qc.Limits))
	}
	seen := map[string]bool{}
	for i := range qc.Limits {
		if err := (&qc.Limits[i]).validate(providerName, i, now); err != nil {
			return err
		}
		key := string(qc.Limits[i].Resolved.Metric) + "/" + qc.Limits[i].Resolved.EveryText
		if seen[key] {
			return fmt.Errorf("provider %q: quota.limits: duplicate limit key %q (same metric + every)", providerName, key)
		}
		seen[key] = true
	}
	return nil
}

// validate checks one LimitConfig and, on success, fills in Resolved. Every
// P1-unsupported knob (cost metric, rolling windows, per-model scope) is
// rejected here with a message that names the capability and says it's
// planned, not "invalid" or "unsupported forever" — see
// docs/TokenPlan_Quota_P1_DevPlan_opus-5.md's §2.2, which treats a silently
// ignored quota field as the one failure mode this project cannot tolerate
// (the same fail-fast contract KnownFields already enforces everywhere
// else in this config).
func (lc *LimitConfig) validate(providerName string, idx int, now time.Time) error {
	switch lc.Metric {
	case "requests":
		lc.Resolved.Metric = core.MetricRequests
	case "tokens":
		lc.Resolved.Metric = core.MetricTokens
	case "cost":
		return fmt.Errorf("provider %q: quota.limits[%d]: metric \"cost\" is not supported in this release — cost/Credits-based quota accounting is planned for a later batch", providerName, idx)
	case "":
		return fmt.Errorf("provider %q: quota.limits[%d]: metric is required (requests|tokens)", providerName, idx)
	default:
		return fmt.Errorf("provider %q: quota.limits[%d]: unknown metric %q (want requests|tokens)", providerName, idx, lc.Metric)
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
	if lc.Amount <= 0 {
		return fmt.Errorf("provider %q: quota.limits[%d]: amount must be > 0", providerName, idx)
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
