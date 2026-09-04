// Ver 2026-08-22, by Sonnet 5

// §2.5's "额度与消耗对照" sub-table: for every config.yaml account that
// declares a quota:, places this report run's own recomputed window
// consumption next to the router's real-time counter — see
// the quota design specification for
// the full rationale and ProviderQuotaRow's doc comment (rows.go) for why
// the two numbers are never combined into one. Mirrors provider.go/
// buildProviders: a pure function over already-finished EndpointsAll rows,
// called once after the aggregation loop completes.
package report

import (
	"sort"
	"strings"
	"time"

	"vmr/internal/core"
	"vmr/internal/i18n"
	"vmr/internal/quota"
)

// refKey identifies one ProviderQuotaRef within accumulateQuotaWindow's
// per-ref maps — provider name + that ref's own quota.LimitKey (ref.Model
// disambiguates a per-model ref from its siblings the same way it
// disambiguates their live bucket keys; a shared ref's Model is always ""
// and LimitKey ignores it).
func refKey(provider string, ref ProviderQuotaRef) string {
	return provider + "\x00" + quota.LimitKey(*ref.Limit, ref.Model)
}

// refMatchesModel reports whether ref should accumulate a traffic record
// for model — a shared ref (quota.PerModel false) matches every model, a
// per-model ref matches only the one exact model it was fanned out for
// (see ProviderQuotaRef's doc comment on why a per-model ref is always
// already model-specific, never a broader Scope to re-check against).
func refMatchesModel(ref ProviderQuotaRef, model string) bool {
	if !quota.PerModel(*ref.Limit) {
		return true
	}
	return ref.Model == model
}

// buildProviderQuotaRows produces one ProviderQuotaRow per (provider, Limit)
// pair in quotas (cmd_report.go's buildProviderQuotas — already carrying
// each Limit's resolved core.Limit and Live). now MUST be the same instant
// buildProviderQuotas used to resolve Live's period match, so
// PeriodStart/PeriodEndsAt computed here agree with Live's exactly.
// windowFrom/windowTo are this report run's own audit-log coverage (the
// same instants formatted into Meta.From/To) — zero-valued windowFrom (no
// records at all) skips the overlap check entirely (there's no meaningful
// window to compare a billing period against). Returns nil when quotas is
// empty — the common "no account declares quota:" case, so the caller (and
// the renderer) can treat "no sub-table" and "nil" the same way.
func buildProviderQuotaRows(rep *Report2, quotas map[string][]ProviderQuotaRef, now, windowFrom, windowTo time.Time) []ProviderQuotaRow {
	if len(quotas) == 0 {
		return nil
	}
	acc := accumulateQuotaWindow(rep, quotas)

	var rows []ProviderQuotaRow
	for provider, refs := range quotas {
		for _, ref := range refs {
			if ref.Limit == nil {
				continue // this Limit didn't resolve — no row to show
			}
			key := refKey(provider, ref)
			var windowConsumed *float64
			switch {
			case ref.Limit.Metric == core.MetricCost && acc.costSawTraffic[key] && !acc.costAnyPriced[key]:
				windowConsumed = nil // traffic existed, none of it priced — unknown, not zero
			case ref.Limit.Metric == core.MetricCost:
				v := acc.windowSums[key].Cost
				windowConsumed = &v
			default:
				v := quota.BaseAmount(*ref.Limit, acc.windowSums[key])
				windowConsumed = &v
			}
			// Same unit-matching discipline quota.EstimatedPct documents: the
			// tokens estimate is a raw (unweighted) token count, so its
			// denominator is the raw four-component total, never BaseAmount's
			// weighted sum; the cost estimate's denominator is
			// acc.windowSums[key].Cost itself (EstimatedPct's own MetricCost
			// branch), already the same $ unit as windowCostEstimated.
			windowEstPct := quota.EstimatedPct(ref.Limit.Metric, acc.windowSums[key], acc.windowEstimated[key], acc.windowCostEstimated[key])
			// Only meaningful next to a number that exists: the all-unpriced case
			// already renders "-", and "100% missing" beside a "-" is noise.
			var windowUnpricedPct float64
			if windowConsumed != nil && acc.costReqs[key] > 0 {
				windowUnpricedPct = float64(acc.costUnpricedReqs[key]) / float64(acc.costReqs[key]) * 100
			}
			// One PeriodBounds (one findK, same-k boundaries) — F9, the form
			// router.quotaStatusRow and cmd_report_quota already use.
			periodStart, periodEnd := quota.PeriodBounds(*ref.Limit, now)
			// [windowFrom, windowTo] and [periodStart, periodEnd] overlap
			// iff windowFrom <= periodEnd && periodStart <= windowTo — the
			// standard interval-intersection test. Skipped (never flagged)
			// when windowFrom is zero: an empty audit-log window has no
			// interval to compare against in the first place.
			windowNoOverlap := !windowFrom.IsZero() &&
				(windowFrom.After(periodEnd) || periodStart.After(windowTo))
			// Models here is the DISPLAY scope, mirroring
			// router.QuotaStatus's same choice: the one specific model for
			// a per-model ref (ref.Model), or the Limit's own declared
			// Scope (always empty) for a shared ref.
			models := ref.Limit.Models
			if ref.Model != "" {
				models = []string{ref.Model}
			}
			rows = append(rows, ProviderQuotaRow{
				Provider:           provider,
				Metric:             string(ref.Limit.Metric),
				Every:              ref.Limit.EveryText,
				Amount:             ref.Limit.Amount,
				Models:             models,
				WindowConsumed:     windowConsumed,
				WindowEstimatedPct: windowEstPct,
				WindowUnpricedPct:  windowUnpricedPct,
				WindowNoOverlap:    windowNoOverlap,
				Live:               ref.Live,
				LiveConfigChanged:  ref.LiveConfigChanged,
				PeriodStart:        periodStart,
				PeriodEndsAt:       periodEnd,
				PeriodElapsedPct:   (1 - quota.TimeLeftFrac(now, periodStart, periodEnd)) * 100,
			})
		}
	}

	// Rows with live data sort by used% descending (the account closest to
	// exhaustion first — the whole point of this table); rows with no live
	// data sink to the bottom rather than sorting as if their missing Pct
	// were 0 (same "hasCost" treatment endpointValueRows already uses).
	// Provider name, then window text, then the specific model (several
	// rows can now share Provider+Every — every per-model Limit's live
	// buckets do) is the final tie-break, for a deterministic order across
	// runs.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		ha, hb := a.Live != nil, b.Live != nil
		if ha != hb {
			return ha
		}
		if ha && hb && a.Live.Pct != b.Live.Pct {
			return a.Live.Pct > b.Live.Pct
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Every != b.Every {
			return a.Every < b.Every
		}
		return strings.Join(a.Models, ",") < strings.Join(b.Models, ",")
	})
	// Skip info lands on rep (part of the JSON contract, read by
	// renderSkippedAttemptsNote) rather than a package-level global —
	// buildProviderQuotaRows already received rep for exactly this kind of
	// cross-stage carry.
	rep.ProviderQuotaSkippedAttempts = acc.skippedAttempts
	rep.ProviderQuotaSkippedProviders = sortedSkippedProviders(acc.unknownProviders)
	return rows
}

// sortedSkippedProviders flattens the unknown-provider map into a
// deterministic name list, ordered by descending row count (ties broken by
// name) — the order renderSkippedAttemptsNote takes its first three from.
func sortedSkippedProviders(m map[string]int) []string {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if m[names[i]] != m[names[j]] {
			return m[names[i]] > m[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

// quotaWindow is buildProviderQuotaRows' per-ref fan-in, keyed by refKey
// (one entry per ProviderQuotaRef, not per Limit — a per-model Limit
// contributes several) — a struct rather than seven loose maps through a
// parameter list, since they are only ever read together.
type quotaWindow struct {
	windowSums          map[string]quota.Counters
	windowEstimated     map[string]float64
	windowCostEstimated map[string]float64
	costSawTraffic      map[string]bool
	costAnyPriced       map[string]bool
	costReqs            map[string]int
	costUnpricedReqs    map[string]int

	// skippedAttempts counts EndpointsAll rows whose provider name is not
	// found in the quotas map — traffic from accounts not tracked by any
	// quota window. These rows contribute nothing to the recomputed
	// consumption figures and were previously silently dropped (P-5-2).
	skippedAttempts int
	// unknownProviders maps each unknown provider name to the number of
	// EndpointsAll rows that carried it, collected during the same loop.
	unknownProviders map[string]int
}

// renderSkippedAttemptsNote writes a single line under the §2.5 quota table
// when some EndpointsAll rows carried a provider name not found in the
// quotas map — traffic that contributed nothing to the window recomputation
// (P-5-2). Reads the stats buildProviderQuotaRows wrote onto rep. The format
// lists the first 3 unknown provider names plus a remaining count when there
// are more.
func renderSkippedAttemptsNote(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	if rep == nil || rep.ProviderQuotaSkippedAttempts == 0 {
		return
	}
	names := rep.ProviderQuotaSkippedProviders
	var namesStr string
	more := 0
	if len(names) > 3 {
		namesStr = strings.Join(names[:3], ", ")
		more = len(names) - 3
	} else {
		namesStr = strings.Join(names, ", ")
	}
	w("%s\n", i18n.Provider(lang).SkippedAttemptsNote(rep.ProviderQuotaSkippedAttempts, namesStr, more))
}

// accumulateQuotaWindow folds rep.EndpointsAll into per-ref totals in each
// ref's own metric — an endpoint's traffic is folded into every ref that
// matches its model (see refMatchesModel), mirroring
// internal/router/quota.go's applicableLimits: a per-model ref only ever
// matches the one model it was fanned out for, and a shared ref matches
// every model, same as the router actually charged.
// Separate from the row construction because the two share no control
// flow, only this result.
func accumulateQuotaWindow(rep *Report2, quotas map[string][]ProviderQuotaRef) quotaWindow {
	acc := quotaWindow{
		windowSums:          map[string]quota.Counters{},
		windowEstimated:     map[string]float64{},
		windowCostEstimated: map[string]float64{},
		costSawTraffic:      map[string]bool{},
		costAnyPriced:       map[string]bool{},
		costReqs:            map[string]int{},
		costUnpricedReqs:    map[string]int{},
		unknownProviders:    map[string]int{},
	}
	// windowEstimated/windowCostEstimated: the share of windowSums that came
	// from the degraded estimate rather than sniffed usage — same numerator
	// quota.Registry tracks live, split in two because EstimatedPct's
	// tokens/cost branches read different denominators (raw token count vs.
	// acc.windowSums[key].Cost). See WindowEstimatedPct's doc comment.
	// cost{SawTraffic,AnyPriced}: "missing data is not a zero" for the one
	// gap degraded-estimate contribution doesn't cover — no rate resolved
	// AT ALL for this Limit's traffic. Renders nil → "-" rather than a
	// fabricated 0. Distinct from windowCostEstimated above: a rate that DID
	// resolve but priced a degraded estimate still counts as priced here.
	// cost{Reqs,UnpricedReqs}: the PARTIALLY-priced case costAnyPriced is too
	// coarse to see — an account mixing priced endpoints with unpriced ones
	// rendered a precise-looking, systematically-low figure. See
	// WindowUnpricedPct's doc comment (rows.go) for why this counts requests
	// rather than dollars.
	for _, e := range rep.EndpointsAll {
		provider, model := splitEndpointProviderModelAny(e.Endpoint)
		refs, ok := quotas[provider]
		if !ok {
			acc.skippedAttempts++
			acc.unknownProviders[provider]++
			continue
		}
		for _, ref := range refs {
			if ref.Limit == nil || !refMatchesModel(ref, model) {
				continue
			}
			key := refKey(provider, ref)
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
				unit, _ := quota.ApplyModelMultiplier(*ref.Limit, model, quota.Counters{Requests: 1}, 0)
				d := quota.Counters{Requests: unit.Requests * float64(e.Forwarded)}
				acc.windowSums[key] = acc.windowSums[key].Add(d)
			case core.MetricTokens:
				// Sniffed usage and the degraded byte-count estimate are added
				// together here — and ONLY here. The router charges both (see
				// internal/router/quota.go's tokenCharge: exact when respnorm
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
				d, est := quota.ApplyModelMultiplier(*ref.Limit, model, c, float64(e.TokensInFreshEst+e.TokensOutEst))
				acc.windowSums[key] = acc.windowSums[key].Add(d)
				acc.windowEstimated[key] += est
			case core.MetricCost:
				// model_multipliers never applies to a cost Limit (config.validate
				// rejects that combination — see LimitConfig.validate's own
				// comment), so this branch deliberately skips ApplyModelMultiplier.
				// Gated on e.Requests (SERVED — accumulateCost's own basis, NOT the
				// Forwarded basis MetricRequests needs above): EndpointsAll also
				// carries attempt-only rows for an endpoint whose every attempt
				// failed, and counting those made an all-failed window render "-"
				// when the router had charged exactly $0.00 — a false UNKNOWN
				// mirroring the false ZERO this guard exists to prevent.
				if e.Requests > 0 {
					acc.costSawTraffic[key] = true
					acc.costReqs[key] += e.Requests
					if e.CostEstimate == nil {
						// accumulateCost (cost.go) returns early with no rate, so
						// this row contributed nothing to windowSums and without
						// this counter left no trace that it existed.
						acc.costUnpricedReqs[key] += e.Requests
					}
				}
				if e.CostEstimate != nil {
					acc.windowSums[key] = acc.windowSums[key].Add(quota.Counters{Cost: *e.CostEstimate})
					acc.windowCostEstimated[key] += e.CostEstimateEst
					acc.costAnyPriced[key] = true
				}
			}
		}
	}
	return acc
}
