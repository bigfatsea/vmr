// Ver 2026-09-23 04:16, by Claude Opus 5.5

// `vmr analyze`'s provider quota-vs-consumption sub-table: resolving each
// config.yaml provider's declared quota.limits[] against live state read
// from vmr-quota.json into the report.ProviderQuotaRef shape
// buildProviderQuotaRows consumes.
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
// state from vmr-quota.json — one report.ProviderQuotaRef per Limit (a
// provider can carry more than one window; see
// the Quota design doc's multi-limit section). Returns nil if config
// or live quota is unavailable without failing report generation.
func buildProviderQuotas(cfg *config.Config, loadErr error, configPath string, tw io.Writer, now time.Time) (map[string][]report.ProviderQuotaRef, string) {
	if loadErr != nil {
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
// possibly nil).
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
// a shared Limit).
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
	limitKey := quota.LimitKey(lim, model)
	periodStart, periodEnd := quota.PeriodBounds(lim, now)
	if b, ok := providerLive[limitKey]; ok && b.PeriodStartTime().Equal(periodStart) {
		used := quota.BaseAmount(lim, b.C)
		var pct float64
		if lim.Amount > 0 {
			pct = used / lim.Amount * 100
		}
		ref.Live = &report.LiveQuota{
			Used:         used,
			Pct:          pct,
			PeriodStart:  periodStart,
			PeriodEndsAt: periodEnd,
			EstimatedPct: quota.EstimatedPct(lim.Metric, b.C, b.Estimated),
		}
	} else if _, exists := providerLive[limitKey]; !exists && len(providerLive) > 0 {
		ref.LiveConfigChanged = true
	}
	return ref
}
