// Ver 2026-08-01, by Sonnet 5

// §6.5 Sticky 有效性: the cache-efficiency gap between requests that stayed
// on their session's previous endpoint and those that switched. See
// StickyEffect (rows.go) for the measurement's definition and its limits.
package report

import (
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func renderStickyEffect(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	eff := rep.Sticky
	if eff == nil {
		return
	}
	t := i18n.Sticky(lang)
	w("## %s\n\n", t.Title)
	w("%s", t.Intro1)
	w("%s", t.Intro2)

	th := t.TableHeaders
	tbl := newTable(w, th[0], th[1], th[2], th[3], th[4], th[5])
	stickyRow(tbl, t.RowContinued, eff.Continued)
	stickyRow(tbl, t.RowSwitched, eff.Switched)
	w("\n")

	// The headline: one number, stated plainly, or an explicit "not enough
	// data" — never a percentage computed from a handful of requests.
	switch {
	case eff.Continued.TokensKnown < stickyMinBasis || eff.Switched.TokensKnown < stickyMinBasis:
		w("%s", t.InsufficientData(stickyMinBasis))
	case eff.Continued.CacheEfficiency > eff.Switched.CacheEfficiency:
		w("%s", t.Working(pctStr(eff.Continued.CacheEfficiency), pctStr(eff.Switched.CacheEfficiency),
			pctStr(eff.Continued.CacheEfficiency-eff.Switched.CacheEfficiency)))
	case eff.Continued.CacheEfficiency < eff.Switched.CacheEfficiency:
		w("%s", t.Reversed(pctStr(eff.Switched.CacheEfficiency), pctStr(eff.Continued.CacheEfficiency)))
		w("%s", t.ReversedNote2)
	default:
		w("%s", t.NoDifference)
	}
	ungroupedSuffix := ""
	if eff.Ungrouped != 0 {
		ungroupedSuffix = t.UngroupedSuffix(eff.Ungrouped)
	}
	w("%s", t.BasisNote(eff.First, ungroupedSuffix))
	w("%s", t.NoExplainNote)

	if len(eff.ByModel) > 0 {
		w("%s\n\n", t.ByModelTitle)
		mh := t.ByModelHeaders
		mt := newTable(w, mh[0], mh[1], mh[2], mh[3], mh[4], mh[5], mh[6])
		for _, m := range eff.ByModel {
			delta := "-"
			if m.Continued.TokensKnown >= stickyMinBasis && m.Switched.TokensKnown >= stickyMinBasis {
				delta = pctStr(m.Continued.CacheEfficiency - m.Switched.CacheEfficiency)
			}
			mt.row(m.Model, m.Protocol,
				strconv.Itoa(m.Continued.Requests), cacheEffCell(m.Continued.CacheEfficiency, m.Continued.TokensKnown, m.Continued.Requests),
				strconv.Itoa(m.Switched.Requests), cacheEffCell(m.Switched.CacheEfficiency, m.Switched.TokensKnown, m.Switched.Requests),
				delta)
		}
		w("%s", t.DeltaEmptyNote)
	}
}

// stickyMinBasis is the smallest per-group usage-bearing sample this
// section will draw a conclusion from. Below it the numbers still render
// (with the existing ⚠️low-n cell treatment) but the verdict line refuses
// to call it — a cache-efficiency gap computed from three requests is
// noise, and stating it as a finding is worse than saying nothing.
const stickyMinBasis = 20

func stickyRow(tbl *mdTable, label string, g StickyGroup) {
	tbl.row(label, strconv.Itoa(g.Requests), strconv.Itoa(g.TokensKnown),
		cacheEffCell(g.CacheEfficiency, g.TokensKnown, g.Requests),
		fmtutil.FmtTokens(g.TokensInCached), fmtutil.FmtTokens(g.TokensInFresh))
}
