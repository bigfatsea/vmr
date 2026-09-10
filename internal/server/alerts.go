// Ver 2026-09-15

// /status alerts[] (G2) and the endpoint headroom join (G4) —
// contracts.md §2.1/§2.2, console-unification §4/§8.5. Kept out of admin.go
// on purpose (see internal/archtest's line budgets); the alert-content
// discipline (actionable state only — no rolling statistics) is pinned by
// tests in alerts_test.go.
package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vmr/internal/core"
	"vmr/internal/health"
	"vmr/internal/quota"
	"vmr/internal/router"
)

// Alert severity/kind vocabulary — contracts.md §2.1 fixes these values.
const (
	alertSeverityError   = "error"
	alertSeverityWarning = "warning"
	alertKindConfig      = "config"
	alertKindEndpoint    = "endpoint"
	alertKindQuota       = "quota"
)

// Quota alert thresholds. Both are the console-unification master's pinned
// values (D4), deliberately not tunables: a limit at ≥90% of its period is
// "act this week", a blown one is "act now" — the quota score itself carries
// exactly one boundary (used == amount), and 0.9 is the single agreed
// "nearly there" line ahead of it.
const (
	quotaAlertWarnFrac = 0.9
	quotaAlertErrFrac  = 1.0
)

// statusAlert is one /status alerts[] row (contracts.md §2.1).
type statusAlert struct {
	Severity string `json:"severity"` // "error" | "warning"
	Kind     string `json:"kind"`     // "config" | "endpoint" | "quota"
	Message  string `json:"message"`
	Ref      string `json:"ref"`
}

// statusAlerts builds /status's top-level alerts[] from three sources, in
// the shape contracts.md §2.1 fixes:
//
//   - config: one warning per snap.Cfg.Check() issue, ref = the config file
//     path the instance is running from ("" when the server was built
//     without one — tests/embedding);
//   - endpoint: one warning per endpoint the health state machine is holding
//     out of real traffic. The trigger is an ACTIVE cooldown specifically:
//     "consecutive_failures>0" with an expired cooldown IS half-open, the
//     normal recovery path, which the contract explicitly excludes from
//     alerts (alarming on it would pin the badge non-zero for the whole
//     backoff window);
//   - quota: any limit of an account at used_frac ≥ 1 (headroom 0) → error,
//     ≥ 0.9 → warning, all of the account's triggered limits merged into
//     one row.
//
// Ordering: errors first, everything else stable by kind+ref (kinds sort
// config < endpoint < quota; equal keys keep generation order). Returns nil
// when nothing is wrong — adminStatus omits the key entirely (an all-clear
// /status carries no alerts key, distinct from an empty array). Rolling
// statistics (24h error counts and the like) are deliberately not consulted:
// they are permanently true and would train operators to ignore the badge.
func (s *Server) statusAlerts(snap *router.Snapshot, now time.Time, qs []router.QuotaProviderStatus) []statusAlert {
	var out []statusAlert
	for _, iss := range snap.Cfg.Check() {
		out = append(out, statusAlert{
			Severity: alertSeverityWarning, Kind: alertKindConfig,
			Message: iss.Message, Ref: s.inst.configPath,
		})
	}
	out = append(out, endpointAlerts(snap, s.rt.Health, now)...)
	out = append(out, quotaAlerts(qs)...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Severity == alertSeverityError) != (b.Severity == alertSeverityError) {
			return a.Severity == alertSeverityError
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Ref < b.Ref
	})
	return out
}

// endpointAlerts reports every endpoint currently in cooldown. ref is the
// endpoint identity `provider:key_label:model` — the same triple the
// topology table renders, so an alert row and its Health hover share one
// key. The same identity can be listed under several virtual models (and
// each listing reports the same health state), so byte-identical rows are
// collapsed; a ref with genuinely different messages keeps every variant.
func endpointAlerts(snap *router.Snapshot, h *health.Registry, now time.Time) []statusAlert {
	var out []statusAlert
	seen := map[string]bool{}
	for _, byName := range snap.Models {
		for _, route := range byName {
			for _, ep := range route.Endpoints {
				st := h.Status(ep.HealthKey(), now)
				if st.Fails == 0 || !now.Before(st.CooldownUntil) {
					continue
				}
				ref := ep.Provider + ":" + ep.KeyLabel + ":" + ep.Model
				msg := endpointAlertCause(st.Fails, st.LastError)
				dedupe := ref + "\x00" + msg
				if seen[dedupe] {
					continue
				}
				seen[dedupe] = true
				out = append(out, statusAlert{
					Severity: alertSeverityWarning, Kind: alertKindEndpoint,
					Ref: ref, Message: msg,
				})
			}
		}
	}
	return out
}

// endpointAlertCause renders the causal pair the Health hover needs
// (consecutive failure count + most recent error class). The class string is
// the health registry's own classification stamp — never re-derived here.
func endpointAlertCause(fails int, lastErr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d consecutive failures", fails)
	if lastErr != "" {
		fmt.Fprintf(&b, ", last error %s", lastErr)
	}
	return b.String()
}

// quotaAlerts folds QuotaProviderStatus rows into one alert per account
// (provider): the same provider can carry several limits and each triggered
// one is a fact about the same account, so they merge into a single row
// whose message lists each (contracts.md §2.1's merge rule). Severity is the
// worst across the account's triggered limits: any blown limit (used ≥
// amount, headroom 0) → error, else the ≥0.9 warning.
//
// Identity: rows are keyed by provider name — the quota.Registry's own
// account key. Counters are keyed by provider *name* by design (rotating a
// key must not reset the period), so api_keys-expanded sub-accounts like
// `p1-main`/`p1-backup` already alert separately.
func quotaAlerts(qs []router.QuotaProviderStatus) []statusAlert {
	type acc struct {
		err    bool
		limits []string
	}
	accs := map[string]*acc{}
	var order []string // first-seen account order; the final stable sort reorders by ref anyway
	for _, row := range qs {
		usedFrac := 0.0
		if row.Amount > 0 { // config validation rejects amount ≤ 0; floor against division by zero anyway
			usedFrac = row.Used / row.Amount
		}
		if usedFrac < quotaAlertWarnFrac {
			continue
		}
		a := accs[row.Provider]
		if a == nil {
			a = &acc{}
			accs[row.Provider] = a
			order = append(order, row.Provider)
		}
		if usedFrac >= quotaAlertErrFrac || row.Headroom <= 0 {
			a.err = true
		}
		a.limits = append(a.limits, quotaLimitDesc(row))
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]statusAlert, 0, len(order))
	for _, p := range order {
		a := accs[p]
		sev := alertSeverityWarning
		if a.err {
			sev = alertSeverityError
		}
		out = append(out, statusAlert{
			Severity: sev, Kind: alertKindQuota, Ref: p,
			Message: "account " + p + ": " + strings.Join(a.limits, "; "),
		})
	}
	return out
}

// quotaLimitDesc renders one triggered limit's line: metric window + usage +
// remaining amount — the "limit 描述 + 剩余量" the contract asks the message
// to carry (row.Pct is the quota row's own Used/Amount*100, unclamped, so an
// over-quota bucket reports >100% honestly).
func quotaLimitDesc(row router.QuotaProviderStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s %.2f%% used", row.Metric, row.Every, row.Pct)
	if left := row.Amount - row.Used; left <= 0 {
		b.WriteString(", exhausted")
	} else {
		fmt.Fprintf(&b, ", %.4g left", left)
	}
	if len(row.Models) > 0 {
		fmt.Fprintf(&b, " (models: %s)", strings.Join(row.Models, ","))
	}
	return b.String()
}

// endpointHeadroom joins ep's account headroom out of the QuotaProviderStatus
// rows adminStatus already renders (§8.5: a pure read-side join — the value
// comes from the same rows the quota section shows, never recomputed, which
// is also why the returned float is bit-identical to that row's headroom).
//
// Selection among the account's rows: a limit whose scope doesn't cover
// ep.Model doesn't constrain this endpoint, so candidates are the rows of
// ep's applicable limits. If any candidate is blown (row headroom 0 ⟺ that
// limit's used_frac hit 1), that 0 IS the endpoint's effective headroom —
// the same stand-down rule ScoreForLimits applies to gates. Otherwise the
// value is the bucket limit's own row headroom, with the bucket picked by
// quota.BucketIndex over the applicable core.Limits — the exported entry
// point that decides bucket-vs-gate; the ratio, its clamp and HeadroomCap
// all live in the quota package, never here.
//
// nil (field omitted) when the account is unmetered, no limit applies to
// this endpoint's model, or an applicable per-model limit has never been
// charged (its row only exists once traffic created the bucket — the field
// stays absent rather than fabricating a value outside the row source).
func endpointHeadroom(ep *core.Endpoint, qs []router.QuotaProviderStatus) *float64 {
	if ep.Quota == nil || len(ep.Quota.Limits) == 0 {
		return nil
	}
	// Scope-filter ep's limits the way the charge/score path does —
	// router.applicableLimits is unexported; quota.AppliesToModel is the
	// exported primitive it is built on.
	var applicable []core.Limit
	for _, l := range ep.Quota.Limits {
		if quota.AppliesToModel(l, ep.Model) {
			applicable = append(applicable, l)
		}
	}
	if len(applicable) == 0 {
		return nil
	}
	byKey := make(map[string]float64, len(qs))
	for _, row := range qs {
		if row.Provider == ep.Provider {
			byKey[limitRowKey(row)] = row.Headroom
		}
	}
	var bucketHR *float64
	bi := quota.BucketIndex(applicable)
	for i, l := range applicable {
		hr, ok := byKey[limitRowKeyOf(l, ep.Model)]
		if !ok {
			continue // per-model limit never charged → no live bucket yet
		}
		if hr <= 0 {
			zero := 0.0 // a blown limit has the account excluded from routing
			return &zero
		}
		if i == bi {
			bucketHR = &hr
		}
	}
	return bucketHR
}

// limitRowKey/limitRowKeyOf build the join key between a QuotaProviderStatus
// row and a core.Limit. QuotaProviderStatus deliberately has no limit-key
// field (its rows are identified by Models scope), so the join reconstructs
// the identity from the row's own fields: metric+every, plus the concrete
// model for a per-model row. quota.LimitKey itself is NOT reused because it
// encodes Registry storage keys ("tokens/1mo#model=m1"), a storage detail
// this display-side join must not depend on. Shared rows carry no Models;
// per-model rows always carry exactly the row's charged model.
func limitRowKey(row router.QuotaProviderStatus) string {
	k := row.Metric + "/" + row.Every
	if len(row.Models) == 1 {
		k += "#model=" + row.Models[0]
	}
	return k
}

func limitRowKeyOf(l core.Limit, model string) string {
	k := string(l.Metric) + "/" + l.EveryText
	if quota.PerModel(l) {
		k += "#model=" + model
	}
	return k
}
