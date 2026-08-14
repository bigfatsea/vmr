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
	// windowEstimated accumulates the share of windowSums that came from the
	// degraded byte-count estimate rather than sniffed usage — same numerator
	// quota.Registry tracks for the live counter, so WindowEstimatedPct below
	// and Live.EstimatedPct mean the same thing on both sides of the table.
	windowEstimated := map[string]float64{}
	// cost{SawTraffic,AnyPriced} implement the "missing data is not a zero"
	// rule for the one metric that can still have traffic yet no computable
	// amount: pricing never resolved for any of it. Renders nil → "-" rather
	// than a fabricated 0, which would be indistinguishable from "genuinely
	// zero spend this window".
	//
	// The tokens metric no longer needs an equivalent: an unparseable-usage
	// record now contributes its degraded estimate (EndpointRow's
	// TokensInFreshEst/TokensOutEst) instead of contributing nothing, so
	// "traffic happened but nothing is computable" is no longer reachable
	// there. What replaces that all-or-nothing bail-out is WindowEstimatedPct
	// — which also covers the case the old check silently missed: a window
	// where only SOME records had unparseable usage rendered a precise,
	// systematically-low number with no signal at all.
	costSawTraffic := map[string]bool{}
	costAnyPriced := map[string]bool{}
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
			// by the same exact multiplier (quota.ApplyModelMultiplier no longer
			// rounds — see quota.Counters' doc comment), so applying it to a
			// unit request and multiplying by the forwarded count is the
			// identity: no aggregate-vs-per-charge drift is possible once
			// neither side rounds. (2) THE BASIS: e.Forwarded, not e.Requests —
			// chargeQuota fires per FORWARDED ATTEMPT (router.go's
			// forwardSuccess), while e.Requests is request-level and still
			// counts a request whose every attempt failed against the last
			// endpoint tried, which the router never charged. e.OK isn't it
			// either: a truncated 2xx leaves OK but was charged.
			// cmd/vmr/quota_parity_test.go pins both halves against the router.
			unit, _ := quota.ApplyModelMultiplier(ref.Spec, model, quota.Counters{Requests: 1}, 0)
			d := quota.Counters{Requests: unit.Requests * float64(e.Forwarded)}
			windowSums[provider] = windowSums[provider].Add(d)
		case core.MetricTokens:
			// Sniffed usage and the degraded byte-count estimate are added
			// together here — and ONLY here. The router charges both (see
			// internal/router/quota.go's tokenCharge: exact when respStream
			// sniffed a usage object, a byte-count estimate when it didn't),
			// so a window mixing the two must count both or it reports a
			// number the router never charged. Counting only the sniffed half
			// was a systematic UNDER-count that rendered as a precise figure,
			// indistinguishable from a genuinely smaller consumption.
			//
			// `estimated` is threaded through ApplyModelMultiplier rather than
			// scaled separately: model_multipliers scales the estimate exactly
			// as it scales the real counters (see quota.ApplyModelMultiplier),
			// and EstimatedPct's denominator below has to be in the same
			// post-multiplier unit as its numerator.
			c := quota.Counters{
				Fresh:      float64(e.TokensInFresh + e.TokensInFreshEst),
				CacheRead:  float64(e.TokensInCached),
				CacheWrite: float64(e.TokensInCacheWrite),
				Out:        float64(e.TokensOut + e.TokensOutEst),
			}
			d, est := quota.ApplyModelMultiplier(ref.Spec, model, c, float64(e.TokensInFreshEst+e.TokensOutEst))
			windowSums[provider] = windowSums[provider].Add(d)
			windowEstimated[provider] += est
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
		case ref.Limit.Metric == core.MetricCost:
			v := windowCost[provider]
			windowConsumed = &v
		default:
			v := quota.BaseAmount(ref.Spec, windowSums[provider])
			windowConsumed = &v
		}
		// Same unit-matching discipline quota.EstimatedPct documents: the
		// estimate is a raw (unweighted) token count, so its denominator is
		// the raw four-component total, never BaseAmount's weighted sum.
		windowEstPct := quota.EstimatedPct(ref.Limit.Metric, windowSums[provider], windowEstimated[provider], 0)
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
			Provider:           provider,
			Metric:             string(ref.Limit.Metric),
			Every:              ref.Limit.EveryText,
			Amount:             ref.Limit.Amount,
			WindowConsumed:     windowConsumed,
			WindowEstimatedPct: windowEstPct,
			WindowNoOverlap:    windowNoOverlap,
			Live:               ref.Live,
			LiveConfigChanged:  ref.LiveConfigChanged,
			PeriodStart:        periodStart,
			PeriodEndsAt:       periodEnd,
			PeriodElapsedPct:   (1 - quota.TimeLeftFrac(now, periodStart, periodEnd)) * 100,
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
