// Ver 2026-08-13, by Opus 5

// §7 Provider Quota Exhaustion finding (quota design specification). Split out of
// metrics.go on purpose (see that file's own note in the dev plan on why:
// buildFindings is already long and internal/report/metrics.go carries no
// archtest line budget of its own to grow into unnoticed).
package report

import (
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

// quotaExhaustionThresholdPct is the "about to run out" bar — a package
// constant, not a config.yaml knob, for the same reason internal/ctxgraph's
// thresholds are: a user can't calibrate a number they have no way to
// measure against (see the quota design specification).
const quotaExhaustionThresholdPct = 90.0

// quotaExhaustionFinding reports the single worst-off account among
// rep.ProviderQuotas whose LIVE (router-metered, never this report's own
// recomputed window estimate — see ProviderQuotaRow's doc comment) used%
// is at or above quotaExhaustionThresholdPct AND is burning faster than the
// period is elapsing (Live.Pct > PeriodElapsedPct — exactly the condition
// quota.Headroom < 1 reduces to, see PeriodElapsedPct's doc comment). The
// second half is what keeps a short-period account (e.g. every: 5h) that is
// simply, healthily full near the end of every cycle from alerting on every
// single run — the same "don't train users to ignore the section" reasoning
// that killed the tier-3 client-skew Finding candidate. Returns nil when no
// account qualifies, or when no account has Live data at all (an estimate
// must never be the basis of an alert an operator might act on).
//
// Picks the single highest Live.Pct among qualifying accounts, tie-broken
// by provider name, then by the row's model scope — the same "report only
// the worst one" convention buildFindings' other detectors already use (see
// e.g. its worst-tool-shape / worst-session selection).
func quotaExhaustionFinding(rep *Report2, lang i18n.Lang) *Finding {
	var worst *ProviderQuotaRow
	for i := range rep.ProviderQuotas {
		r := &rep.ProviderQuotas[i]
		if r.Live == nil || r.Live.Pct < quotaExhaustionThresholdPct || r.Live.Pct <= r.PeriodElapsedPct {
			continue
		}
		if worst == nil || r.Live.Pct > worst.Live.Pct ||
			(r.Live.Pct == worst.Live.Pct && (r.Provider < worst.Provider ||
				(r.Provider == worst.Provider && strings.Join(r.Models, ",") < strings.Join(worst.Models, ",")))) {
			worst = r
		}
	}
	if worst == nil {
		return nil
	}
	ft := i18n.Efficiency(lang).ProviderQuotaExhaustionFinding(
		worst.Provider, worst.Models, strconv.FormatFloat(worst.Live.Pct, 'f', 1, 64), worst.Metric, worst.Every)
	return &Finding{
		Code: FindingProviderQuotaExhaustion, Finding: ft.Title, Metric: "provider_quota_used_pct",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action,
	}
}
