// Ver 2026-08-13, by Opus 5

// §2.5's "额度与消耗对照" sub-table: for every config.yaml account that
// declares a quota:, places this report run's own recomputed window
// consumption next to the router's real-time counter — see
// docs/future-strategy/vmr_quota_visibility_devplan_opus-5.md's batch 2 for
// the full rationale and ProviderQuotaRow's doc comment (rows.go) for why
// the two numbers are never combined into one. Mirrors provider.go/
// buildProviders: a pure function over already-finished EndpointsAll rows,
// called once after the aggregation loop completes.
package report

import (
	"sort"
	"time"

	"vmr/internal/core"
	"vmr/internal/quota"
)

// buildProviderQuotaRows produces one ProviderQuotaRow per entry in quotas
// (cmd_report.go's buildProviderQuotas — already carrying each account's
// resolved Limit/Spec/Live). now MUST be the same instant buildProviderQuotas
// used to resolve Live's period match, so PeriodStart/PeriodEndsAt computed
// here agree with Live's exactly. windowFrom/windowTo are this report run's
// own audit-log coverage (the same instants formatted into Meta.From/To) —
// zero-valued windowFrom (no records at all) skips the overlap check
// entirely (there's no meaningful window to compare a billing period
// against). Returns nil when quotas is empty — the common "no account
// declares quota:" case, so the caller (and the renderer) can treat "no
// sub-table" and "nil" the same way.
func buildProviderQuotaRows(rep *Report2, quotas map[string]ProviderQuotaRef, now, windowFrom, windowTo time.Time) []ProviderQuotaRow {
	if len(quotas) == 0 {
		return nil
	}

	windowSums := map[string]quota.Counters{}
	windowCost := map[string]float64{}
	// {cost,tokens}SawTraffic/AnyKnown implement the same "missing data is
	// not a zero" rule for the two metrics that can have traffic yet no
	// computable amount:
	//   - cost:   traffic happened but pricing never resolved for any of it
	//   - tokens: traffic happened but not one request's usage was parseable
	//             (TokensKnown == 0 everywhere), so this window contributes
	//             zero tokens while the ROUTER charged a byte-count estimate
	//             for those same requests (see internal/router/quota.go's
	//             tokenCharge degraded path)
	// Both render nil → "-" rather than a fabricated 0, which would be
	// indistinguishable from "genuinely zero traffic this window" (a real 0,
	// which the requests metric and a no-traffic account still produce).
	costSawTraffic := map[string]bool{}
	costAnyPriced := map[string]bool{}
	tokensSawTraffic := map[string]bool{}
	tokensAnyKnown := map[string]bool{}
	for _, e := range rep.EndpointsAll {
		provider, model := splitEndpointProviderModelAny(e.Endpoint)
		ref, ok := quotas[provider]
		if !ok || ref.Limit == nil || ref.Spec == nil {
			continue
		}
		switch ref.Limit.Metric {
		case core.MetricRequests:
			// Exact, not an estimate — two things have to line up, and both are
			// deliberate. (1) THE MULTIPLIER: every charge is Requests:1 scaled
			// by the same constant ceil(mult), so applying it to a unit request
			// and multiplying is the identity, unlike an aggregate-then-ceil
			// (ceil(n*mult)), which drifts for a fractional multiplier.
			// (2) THE BASIS: e.Forwarded, not e.Requests — chargeQuota fires per
			// FORWARDED ATTEMPT (router.go's forwardSuccess), while e.Requests is
			// request-level and still counts a request whose every attempt failed
			// against the last endpoint tried, which the router never charged.
			// e.OK isn't it either: a truncated 2xx leaves OK but was charged.
			// cmd/vmr/quota_parity_test.go pins both halves against the router.
			unit, _ := quota.ApplyModelMultiplier(ref.Spec, model, quota.Counters{Requests: 1}, 0)
			d := quota.Counters{Requests: unit.Requests * int64(e.Forwarded)}
			windowSums[provider] = windowSums[provider].Add(d)
		case core.MetricTokens:
			// Requests, not TokensKnown: "did any traffic reach this
			// account this window" is a question about requests, so an
			// endpoint whose every request had unparseable usage still
			// counts as traffic (and lands in the nil/"-" case below).
			if e.Requests > 0 {
				tokensSawTraffic[provider] = true
			}
			if e.TokensKnown > 0 {
				tokensAnyKnown[provider] = true
			}
			c := quota.Counters{Fresh: e.TokensInFresh, CacheRead: e.TokensInCached, CacheWrite: e.TokensInCacheWrite, Out: e.TokensOut}
			d, _ := quota.ApplyModelMultiplier(ref.Spec, model, c, 0)
			windowSums[provider] = windowSums[provider].Add(d)
		case core.MetricCost:
			// model_multipliers never applies to a cost account (config.validate
			// rejects that combination — see ChargeResponse's own comment), so
			// this branch deliberately skips ApplyModelMultiplier.
			costSawTraffic[provider] = true
			if e.CostEstimate != nil {
				windowCost[provider] += *e.CostEstimate
				costAnyPriced[provider] = true
			}
		}
	}

	rows := make([]ProviderQuotaRow, 0, len(quotas))
	for provider, ref := range quotas {
		if ref.Limit == nil || ref.Spec == nil {
			continue // this account's quota didn't resolve — no row to show
		}
		var windowConsumed *float64
		switch {
		case ref.Limit.Metric == core.MetricCost && costSawTraffic[provider] && !costAnyPriced[provider]:
			windowConsumed = nil // traffic existed, none of it priced — unknown, not zero
		case ref.Limit.Metric == core.MetricTokens && tokensSawTraffic[provider] && !tokensAnyKnown[provider]:
			windowConsumed = nil // traffic existed, no usage parseable for any of it — unknown, not zero
		case ref.Limit.Metric == core.MetricCost:
			v := windowCost[provider]
			windowConsumed = &v
		default:
			v := quota.BaseAmount(ref.Spec, windowSums[provider])
			windowConsumed = &v
		}
		periodStart := quota.PeriodStart(*ref.Limit, now)
		periodEnd := quota.PeriodEnd(*ref.Limit, now)
		// [windowFrom, windowTo] and [periodStart, periodEnd] overlap
		// iff windowFrom <= periodEnd && periodStart <= windowTo — the
		// standard interval-intersection test. Skipped (never flagged)
		// when windowFrom is zero: an empty audit-log window has no
		// interval to compare against in the first place.
		windowNoOverlap := !windowFrom.IsZero() &&
			(windowFrom.After(periodEnd) || periodStart.After(windowTo))
		rows = append(rows, ProviderQuotaRow{
			Provider:          provider,
			Metric:            string(ref.Limit.Metric),
			Every:             ref.Limit.EveryText,
			Amount:            ref.Limit.Amount,
			WindowConsumed:    windowConsumed,
			WindowNoOverlap:   windowNoOverlap,
			Live:              ref.Live,
			LiveConfigChanged: ref.LiveConfigChanged,
			PeriodStart:       periodStart,
			PeriodEndsAt:      periodEnd,
			PeriodElapsedPct:  (1 - quota.TimeLeftFrac(now, periodStart, periodEnd)) * 100,
		})
	}

	// Rows with live data sort by used% descending (the account closest to
	// exhaustion first — the whole point of this table); rows with no live
	// data sink to the bottom rather than sorting as if their missing Pct
	// were 0 (same "hasCost" treatment endpointValueRows already uses).
	// Provider name is the final tie-break either way, for a deterministic
	// order across runs.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		ha, hb := a.Live != nil, b.Live != nil
		if ha != hb {
			return ha
		}
		if ha && hb && a.Live.Pct != b.Live.Pct {
			return a.Live.Pct > b.Live.Pct
		}
		return a.Provider < b.Provider
	})
	return rows
}
