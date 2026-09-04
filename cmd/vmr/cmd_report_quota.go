// Ver 2026-08-22, by Sonnet 5

// `vmr report`'s §2.5 quota-vs-consumption sub-table: resolving each
// config.yaml provider's declared quota.limits[] against live state read
// from vmr-quota.json into the report.ProviderQuotaRef shape
// buildProviderQuotaRows consumes. Split out of cmd_report.go per
// internal/archtest's per-file line budget.
package main

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/report"
)

// buildProviderQuotas loads declared quota limits from config and live quota
// state from vmr-quota.json — one report.ProviderQuotaRef per Limit (P3: a
// provider can carry more than one window; see
// docs/VirtualModelRouter_Design_v4_Quota.md's §5.2). Returns nil if config
// or live quota is unavailable without failing report generation.
func buildProviderQuotas(cfg *config.Config, loadErr error, configPath string, tw io.Writer, now time.Time) (map[string][]report.ProviderQuotaRef, string) {
	if loadErr != nil {
		// cmdReport already printed one unified warning for cfgErr — a
		// second, near-identical one here would just repeat it.
		// configPath is kept in the signature purely so this function's
		// doc comment / callers stay symmetric with buildPricing's.
		return nil, ""
	}
	quotaJSONPath := filepath.Join(cfg.LogDir, "vmr-quota.json")
	live, err := quota.LoadFile(quotaJSONPath)
	if err != nil {
		fmt.Fprintf(tw, "provider quotas: %s not usable (%v) — §2.5's real-time columns render as \"-\"\n", quotaJSONPath, err)
	}
	quotas := map[string][]report.ProviderQuotaRef{}
	for _, p := range cfg.Providers {
		if p.Quota == nil || len(p.Quota.Limits) == 0 {
			continue
		}
		var refs []report.ProviderQuotaRef
		for i := range p.Quota.Limits {
			lim := p.Quota.Limits[i].Resolved
			refs = append(refs, quotaRefsForLimit(lim, live[p.Name], now)...)
		}
		if len(refs) > 0 {
			quotas[p.Name] = refs
		}
	}
	return quotas, quotaJSONPath
}

// quotaRefsForLimit builds the report.ProviderQuotaRef(s) for one Limit
// against providerLive (this provider's slice of the loaded vmr-quota.json,
// possibly nil). A shared Limit always produces exactly one ref, Model "".
// A per-model Limit produces one ref PER MODEL THAT ACTUALLY HAS A LIVE
// BUCKET FOR IT — enumerated from providerLive's own keys via
// quota.ExtractModel, the same way router.QuotaStatus enumerates the live
// Registry (a per-model Limit's real membership, especially the wildcard
// shape, isn't derivable from config alone) — see ProviderQuotaRef's doc
// comment for why a per-model Limit with no live buckets yet produces zero
// refs rather than a placeholder.
func quotaRefsForLimit(lim core.Limit, providerLive map[string]quota.Bucket, now time.Time) []report.ProviderQuotaRef {
	if !quota.PerModel(lim) {
		return []report.ProviderQuotaRef{quotaRefFor(lim, "", providerLive, now)}
	}
	var refs []report.ProviderQuotaRef
	for key := range providerLive {
		if model, ok := quota.ExtractModel(lim, key); ok {
			refs = append(refs, quotaRefFor(lim, model, providerLive, now))
		}
	}
	return refs
}

// quotaRefFor builds one report.ProviderQuotaRef for lim and model ("" for
// a shared Limit) — the single-ref body quotaRefsForLimit factors out so
// the shared and per-model paths build a ref identically once they've each
// decided which model(s) apply.
func quotaRefFor(lim core.Limit, model string, providerLive map[string]quota.Bucket, now time.Time) report.ProviderQuotaRef {
	models := lim.Models
	if model != "" {
		models = []string{model}
	}
	ref := report.ProviderQuotaRef{
		Metric: string(lim.Metric),
		Every:  lim.EveryText,
		Amount: lim.Amount,
		Models: models,
		Model:  model,
		Limit:  &lim,
	}
	// §5.2's stale-period trap: quota.Registry resets lazily, so a bucket
	// still on disk from a period the process wasn't running through must
	// NOT be rendered as "this period's usage" — only a bucket whose stored
	// PeriodStart matches what PeriodBounds(lim, now) computes for right
	// now qualifies as Live. One PeriodBounds: start and end are same-k
	// consistent, so the rendered PeriodEndsAt is that same period's end.
	limitKey := quota.LimitKey(lim, model)
	periodStart, periodEnd := quota.PeriodBounds(lim, now)
	if b, ok := providerLive[limitKey]; ok && b.PeriodStartTime().Equal(periodStart) {
		used := quota.BaseAmount(lim, b.C)
		var pct float64
		if lim.Amount > 0 {
			pct = used / lim.Amount * 100
		}
		ref.Live = &report.LiveQuota{
			Used: used, Pct: pct,
			PeriodStart: periodStart, PeriodEndsAt: periodEnd,
			EstimatedPct: quota.EstimatedPct(lim.Metric, b.C, b.Estimated, b.EstimatedCost),
		}
	} else if _, exists := providerLive[limitKey]; !exists && len(providerLive) > 0 {
		// Distinguishes two different-looking "Live is nil" causes that the
		// generic stale-period footnote alone conflates:
		// - limitKey absent, but this provider DOES have other keys on disk
		// → its quota:'s metric/every/models scope changed since those
		// were last written (Registry never deletes an old key — it's
		// lazy-reset, not lazy-cleaned), so the OLD bucket is simply keyed
		// differently now. The process is healthy and running; the config
		// just moved out from under it. Unreachable for a per-model ref
		// (quotaRefsForLimit only ever builds one FROM an existing key), so
		// this branch only ever fires for a shared ref.
		// - limitKey present but period mismatch (the `if` branch's
		// negative), or no data for this provider at all → the existing
		// "process wasn't running through this period" or "never charged
		// yet" story, unchanged.
		ref.LiveConfigChanged = true
	}
	return ref
}
