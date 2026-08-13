// Ver 2026-08-12 23:40, by Opus 5

// §2.5 账户（Provider）消耗与额度: cross-model roll-up per upstream account.
// The main table itself carries no quota column — a declared quota (if any)
// only ever appears in the "额度与消耗对照" sub-table below it
// (renderProviderQuotaTable), never duplicated up here with a second
// formatter (see rows.go's ProviderQuotaRow doc comment for why the two
// numbers must stay separate). ProviderRow.Quota still round-trips into
// vmr-report.json for programmatic consumers; it's simply not rendered
// as Markdown in this table. See rows.go's ProviderRow/ProviderQuotaRef
// doc comments for what this deliberately does and doesn't claim.
package report

import (
	"strconv"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func renderProviders(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	// The sub-table (renderProviderQuotaTable) is gated independently below:
	// an account can declare quota: and have Live data worth showing even
	// with zero traffic in THIS report's window (buildProviderQuotaRows
	// reads off the quotas map directly, not off rep.Providers) — e.g. an
	// account failover currently isn't routing to. Only skip the whole
	// section when BOTH the main table and the sub-table have nothing.
	if len(rep.Providers) == 0 && len(rep.ProviderQuotas) == 0 {
		return
	}
	t := i18n.Provider(lang)
	w("## %s\n\n", t.Title)
	w("%s", t.Intro)

	if len(rep.Providers) > 0 {
		priced := rep.Pricing != nil

		headers := append([]string(nil), t.Headers...)
		if priced {
			cur := ""
			if rep.Pricing.Currency != "" {
				cur = " (" + rep.Pricing.Currency + ")"
			}
			headers = append(headers, t.CostHdr(cur))
		}
		tbl := newTable(w, headers...)

		for _, p := range rep.Providers {
			cells := []string{
				p.Provider,
				strconv.Itoa(len(p.Models)),
				strconv.Itoa(p.Requests),
				pctStr2(p.RequestsOK, p.Requests),
				fmtTokens(p.TokensInFresh) + " / " + fmtTokens(p.TokensInCached) + " / " + fmtTokens(p.TokensOut),
				cacheEffCell(p.CacheEfficiency, p.TokensKnown, p.Requests),
				fmtDurMS(p.DurMSMean),
				pctFloat(p.ErrorRate / 100),
				topErrorClassProviderCell(p),
			}
			if priced {
				if p.CostEstimate != nil {
					cells = append(cells, strconv.FormatFloat(*p.CostEstimate, 'f', 4, 64))
				} else {
					cells = append(cells, "-")
				}
			}
			tbl.row(cells...)
		}
		w("\n")
	}

	renderProviderQuotaTable(w, rep, lang)
}

// renderProviderQuotaTable (batch 2) is §2.5's "额度与消耗对照" sub-table —
// see rows.go's ProviderQuotaRow doc comment for why WindowConsumed and
// Live are two independently-windowed numbers that must stay visually
// separate, never combined into one. Absent entirely when no config.yaml
// account both declares quota: and resolved successfully (buildProviderQuotaRows
// returns nil in that case) — same "no section" degrade every other §2.5
// sub-piece already uses.
func renderProviderQuotaTable(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	if len(rep.ProviderQuotas) == 0 {
		return
	}
	t := i18n.ProviderQuota(lang)
	w("### %s\n\n", t.Title)
	w("%s", t.Intro)
	if rep.Meta.QuotaJSONPath != "" {
		w("%s", t.SourcePathLine(rep.Meta.QuotaJSONPath))
		if rep.Meta.QuotaInputOutsideLogDir {
			w("%s", t.CrossInstanceWarning)
		}
		w("\n")
	}
	tbl := newTable(w, t.Headers...)
	anyNoOverlap, anyConfigChanged, anyOverQuota := false, false, false
	for _, r := range rep.ProviderQuotas {
		liveUsed, pct := "-", "-"
		if r.Live != nil {
			liveUsed = t.FormatLiveUsed(numStr(r.Live.Used), r.Live.EstimatedPct)
			pct = pctHundred(r.Live.Pct)
			if r.Live.Pct >= 100 {
				// Pct is deliberately not clamped (see LiveQuota's doc
				// comment) — an over-quota account needs a visual flag,
				// not just a plain "138.9%" that reads the same as any
				// other number at a glance. Reuses the
				// existing ⭐ marker convention rather than inventing a new
				// English string that would need its own i18n entry.
				pct += "⭐"
				anyOverQuota = true
			}
		} else if r.LiveConfigChanged {
			// A plain "-" here would read as "process wasn't running
			// this period", which is wrong when the real cause is a
			// config.yaml quota: edit leaving the old on-disk bucket keyed
			// under a metric/every this row no longer uses — the process is
			// healthy, the key just changed. ‡ keeps this visually distinct
			// from the ⭐ (over-quota) and † (no window overlap) markers
			// already used in this same table.
			liveUsed, pct = "-‡", "-‡"
			anyConfigChanged = true
		}
		windowConsumed := windowConsumedCell(r.WindowConsumed)
		if r.WindowNoOverlap {
			// The report's own audit-log window and this account's
			// billing period share no time at all — the more extreme,
			// more easily misread cousin of the routine "windows don't
			// align" case the footnotes already explain. † rather than ⭐
			// (already used for over-quota above) to keep the two
			// meanings visually distinct in the same table.
			windowConsumed += "†"
			anyNoOverlap = true
		}
		tbl.row(
			r.Provider,
			r.Metric,
			windowConsumed,
			liveUsed,
			numStr(r.Amount),
			pct,
			pctHundred(r.PeriodElapsedPct),
			periodRangeCell(r.PeriodStart, r.PeriodEndsAt),
		)
	}
	// WindowFootnote/StalePeriodFootnote explain the two CONSUMPTION COLUMNS
	// themselves (what each number is and why the two can't be subtracted),
	// so they are unconditional. The other three explain MARKERS, and are
	// each gated on that marker actually appearing — a report where every
	// account is healthy must not carry an explanation of a ⭐ it doesn't
	// contain (⭐ used to print unconditionally while ‡ and † didn't).
	w("\n%s", t.WindowFootnote)
	w("%s", t.StalePeriodFootnote)
	if anyConfigChanged {
		w("%s", t.ConfigChangedFootnote)
	}
	if anyOverQuota {
		w("%s", t.OverQuotaFootnote)
	}
	if anyNoOverlap {
		w("%s", t.NoOverlapFootnote)
	}
	w("\n")
}

// topErrorClassProviderCell renders a provider's dominant error class as
// "rate_limit 12(63%)" — the data (ProviderRow.ErrorClasses)
// was already aggregated and serialized into vmr-report.json before this
// fix, just never rendered into the main table's Markdown; this is the
// cheap way to answer "is this account hard-quota-exhausted or just being
// rate-limited" the existing "错误率" column alone can't. The percentage is
// this class's share of FAILED attempts (not of all attempts — "错误率"
// already answers that), mirroring section_reliability.go's
// topErrorClassShort in spirit but against ProviderRow rather than
// EndpointRow (no EndpointRow.Attempts equivalent exists at the
// account roll-up level, so it isn't reused directly).
func topErrorClassProviderCell(p ProviderRow) string {
	if len(p.ErrorClasses) == 0 {
		return "-"
	}
	cls, n := topErrorClassCount(p.ErrorClasses)
	if p.Failed <= 0 {
		return cls + " " + strconv.Itoa(n)
	}
	return cls + " " + strconv.Itoa(n) + "(" + pctStr2(n, p.Failed) + ")"
}

// windowConsumedCell renders ProviderQuotaRow.WindowConsumed — nil (a
// cost-metric account with traffic this window but no resolvable price
// for any of it) renders "-", the same "missing data, not a real zero"
// convention the main table's $ Estimate column already uses, never a
// fabricated 0 that would read as "genuinely spent nothing."
func windowConsumedCell(v *float64) string {
	if v == nil {
		return "-"
	}
	return numStr(*v)
}

// periodRangeCell formats a Limit's current period as "MM-DD ~ MM-DD" in
// fmtutil.DisplayZone — the timezone invariant every human-facing timestamp
// in this package goes through (see CLAUDE.md).
func periodRangeCell(start, end time.Time) string {
	const layout = "01-02"
	return start.In(fmtutil.DisplayZone).Format(layout) + " ~ " + end.In(fmtutil.DisplayZone).Format(layout)
}
