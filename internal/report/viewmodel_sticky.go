// Ver 2026-09-15, by Opus 5

// §6.5 Sticky 有效性 view model: the cache-efficiency gap between requests
// that stayed on their session's previous endpoint and those that
// switched. See StickyEffect (rows.go) for the measurement's definition
// and its limits. Pairs with internal/i18n/report_sticky.go.
package report

import (
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func vmStickySection(rep *Report2, lang i18n.Lang) SectionVM {
	eff := rep.Sticky
	if eff == nil {
		return SectionVM{}
	}
	t := i18n.Sticky(lang)
	sec := SectionVM{ID: "sticky", Title: t.Title}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.Intro1 + t.Intro2})

	tbl := &TableVM{Headers: t.TableHeaders[:]}
	vmStickyRow(tbl, t.RowContinued, eff.Continued)
	vmStickyRow(tbl, t.RowSwitched, eff.Switched)
	// The headline: one number, stated plainly, or an explicit "not
	// enough data" — never a percentage computed from a handful of
	// requests.
	verdict := ""
	switch {
	case eff.Continued.TokensKnown < vmStickyMinBasis || eff.Switched.TokensKnown < vmStickyMinBasis:
		verdict = t.InsufficientData(vmStickyMinBasis)
	case eff.Continued.CacheEfficiency > eff.Switched.CacheEfficiency:
		verdict = t.Working(pctStr(eff.Continued.CacheEfficiency), pctStr(eff.Switched.CacheEfficiency),
			pctStr(eff.Continued.CacheEfficiency-eff.Switched.CacheEfficiency))
	case eff.Continued.CacheEfficiency < eff.Switched.CacheEfficiency:
		verdict = t.Reversed(pctStr(eff.Switched.CacheEfficiency), pctStr(eff.Continued.CacheEfficiency)) + t.ReversedNote2
	default:
		verdict = t.NoDifference
	}
	ungroupedSuffix := ""
	if eff.Ungrouped != 0 {
		ungroupedSuffix = t.UngroupedSuffix(eff.Ungrouped)
	}
	tbl.note(verdict + t.BasisNote(eff.First, ungroupedSuffix) + t.NoExplainNote)
	sec.Blocks = append(sec.Blocks, tbl)

	if len(eff.ByModel) > 0 {
		mt := &TableVM{Title: t.ByModelTitle, Headers: t.ByModelHeaders[:]}
		for _, m := range eff.ByModel {
			delta := "-"
			if m.Continued.TokensKnown >= vmStickyMinBasis && m.Switched.TokensKnown >= vmStickyMinBasis {
				delta = pctStr(m.Continued.CacheEfficiency - m.Switched.CacheEfficiency)
			}
			mt.row(m.Model, m.Protocol,
				strconv.Itoa(m.Continued.Requests), cacheEffCell(m.Continued.CacheEfficiency, m.Continued.TokensKnown, m.Continued.Requests),
				strconv.Itoa(m.Switched.Requests), cacheEffCell(m.Switched.CacheEfficiency, m.Switched.TokensKnown, m.Switched.Requests),
				delta)
		}
		mt.Notes = vmNotes(t.DeltaEmptyNote)
		sec.Blocks = append(sec.Blocks, mt)
	}
	return sec
}

// vmStickyMinBasis is the smallest per-group usage-bearing sample this
// section will draw a conclusion from. Below it the numbers still render
// (with the existing ⚠️low-n cell treatment) but the verdict line refuses
// to call it — a cache-efficiency gap computed from three requests is
// noise, and stating it as a finding is worse than saying nothing.
const vmStickyMinBasis = 20

func vmStickyRow(tbl *TableVM, label string, g StickyGroup) {
	tbl.row(label, strconv.Itoa(g.Requests), strconv.Itoa(g.TokensKnown),
		cacheEffCell(g.CacheEfficiency, g.TokensKnown, g.Requests),
		fmtutil.FmtTokens(g.TokensInCached), fmtutil.FmtTokens(g.TokensInFresh))
}
