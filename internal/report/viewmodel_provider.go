// Ver 2026-09-15, by Opus 5

// §2.5 账户（Provider）消耗与额度 view model: cross-model roll-up per
// upstream account, plus the "额度与消耗对照" sub-table. Pairs with
// internal/i18n/report_provider.go. The main table carries no quota
// column — a declared quota only ever appears in the sub-table (see
// rows.go's ProviderQuotaRow doc comment for why the two numbers must
// stay separate).
package report

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func vmProvidersSection(rep *Report2, lang i18n.Lang) SectionVM {
	// The sub-table is gated independently: an account can declare quota:
	// and have Live data worth showing even with zero traffic in THIS
	// report's window. Only skip the whole section when BOTH the main
	// table and the sub-table have nothing.
	if len(rep.Providers) == 0 && len(rep.ProviderQuotas) == 0 {
		return SectionVM{}
	}
	t := i18n.Provider(lang)
	sec := SectionVM{ID: "providers", Title: t.Title}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.Intro})

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
		tbl := &TableVM{Headers: headers}
		for _, p := range rep.Providers {
			cells := []string{
				p.Provider,
				strconv.Itoa(len(p.Models)),
				strconv.Itoa(p.Requests),
				pctStr2(p.RequestsOK, p.Requests),
				fmtutil.FmtTokens(p.TokensInFresh) + " / " + fmtutil.FmtTokens(p.TokensInCached) + " / " + fmtutil.FmtTokens(p.TokensOut),
				cacheEffCell(p.CacheEfficiency, p.TokensKnown, p.Requests),
				fmtDurMS(p.DurMSMean),
				pctStr(p.ErrorRate / 100),
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
		sec.Blocks = append(sec.Blocks, tbl)
	}

	vmProviderQuotaTable(&sec, rep, lang)
	return sec
}

// vmProviderQuotaTable is §2.5's "额度与消耗对照" sub-table. WindowConsumed
// and Live are two independently-windowed numbers that must stay visually
// separate, never combined into one. Absent entirely when no config.yaml
// account both declares quota: and resolved successfully.
func vmProviderQuotaTable(sec *SectionVM, rep *Report2, lang i18n.Lang) {
	if len(rep.ProviderQuotas) == 0 {
		return
	}
	t := i18n.ProviderQuota(lang)
	sec.Blocks = append(sec.Blocks, ParaVM{Text: "### " + t.Title + "\n\n"})
	intro := t.Intro
	if rep.Meta.QuotaJSONPath != "" {
		intro += t.SourcePathLine(rep.Meta.QuotaJSONPath)
		if rep.Meta.QuotaInputOutsideLogDir {
			intro += t.CrossInstanceWarning
		}
		intro += "\n"
	}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: intro})

	tbl := &TableVM{Headers: t.Headers}
	anyNoOverlap, anyConfigChanged, anyOverQuota := false, false, false
	anyHighEstimate := false
	for _, r := range rep.ProviderQuotas {
		if r.Metric == "tokens" {
			if (r.Live != nil && r.Live.EstimatedPct >= 95) || r.WindowEstimatedPct >= 95 {
				anyHighEstimate = true
			}
		}
		liveUsed, pct := "-", "-"
		if r.Live != nil {
			liveUsed = t.FormatEstimatedShare(numStr(r.Live.Used), r.Live.EstimatedPct)
			pct = pctHundred(r.Live.Pct)
			if r.Live.Pct >= 100 {
				// Pct is deliberately not clamped — an over-quota account
				// needs a visual flag. Reuses the existing ⭐ marker
				// convention rather than inventing a new i18n entry.
				pct += "⭐"
				anyOverQuota = true
			}
		} else if r.LiveConfigChanged {
			// A plain "-" would read as "process wasn't running this
			// period"; ‡ keeps this visually distinct from the ⭐
			// (over-quota) and † (no window overlap) markers.
			liveUsed, pct = "-‡", "-‡"
			anyConfigChanged = true
		}
		windowConsumed := t.FormatEstimatedShare(numStr(r.WindowConsumed), r.WindowEstimatedPct)
		if r.WindowNoOverlap {
			// † rather than ⭐ to keep the two meanings visually distinct
			// in the same table.
			windowConsumed += "†"
			anyNoOverlap = true
		}
		tbl.row(
			quotaRowProviderCell(r.Provider, r.Models),
			r.Metric,
			windowConsumed,
			liveUsed,
			numStr(r.Amount),
			pct,
			pctHundred(r.PeriodElapsedPct),
			periodRangeCell(r.PeriodStart, r.PeriodEndsAt),
		)
	}
	// WindowFootnote/StalePeriodFootnote explain the two CONSUMPTION
	// COLUMNS themselves, so they are unconditional. The other four
	// explain MARKERS, and are each gated on that marker actually
	// appearing — a report where every account is healthy must not carry
	// an explanation of a ⭐ it doesn't contain.
	tbl.note(t.WindowFootnote)
	tbl.note(t.StalePeriodFootnote)
	if anyConfigChanged {
		tbl.note(t.ConfigChangedFootnote)
	}
	if anyOverQuota {
		tbl.note(t.OverQuotaFootnote)
	}
	if anyNoOverlap {
		tbl.note(t.NoOverlapFootnote)
	}
	if anyHighEstimate {
		tbl.note(t.IncludeUsageFootnote)
	}
	// The skipped-attempts note belongs to this sub-table (it describes
	// what the window recomputation ignored), so it lives and dies with it.
	if s := skippedAttemptsNote(rep, lang); s != "" {
		tbl.note(s)
	}
	// The legacy path closed the block with one more blank line.
	tbl.note("\n")
	sec.Blocks = append(sec.Blocks, tbl)
}

// skippedAttemptsNote renders the P-5-2 line under §2.5 when some
// EndpointsAll rows carried a provider name not found in the quotas map.
// Directly calls providerquota.go's renderSkippedAttemptsNote.
func skippedAttemptsNote(rep *Report2, lang i18n.Lang) string {
	var buf strings.Builder
	renderSkippedAttemptsNote(func(format string, args ...any) { fmt.Fprintf(&buf, format, args...) }, rep, lang)
	return buf.String()
}

// quotaRowProviderCell renders the quota sub-table's first column: the
// provider name, suffixed with the row's model scope when it carries one.
func quotaRowProviderCell(provider string, models []string) string {
	if len(models) == 0 {
		return provider
	}
	return provider + " (" + strings.Join(models, ", ") + ")"
}

// topErrorClassProviderCell renders a provider's dominant error class as
// "rate_limit 12(63%)" — the share of FAILED attempts, mirroring
// section_reliability's topErrorClassShort in spirit but against
// ProviderRow.
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

// periodRangeCell formats a Limit's current period as "MM-DD ~ MM-DD" in
// fmtutil.DisplayZone — the timezone invariant every human-facing
// timestamp in this package goes through.
func periodRangeCell(start, end time.Time) string {
	const layout = "01-02"
	return start.In(fmtutil.DisplayZone).Format(layout) + " ~ " + end.In(fmtutil.DisplayZone).Format(layout)
}
