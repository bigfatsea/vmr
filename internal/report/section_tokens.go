// Ver 2026-08-01, by Sonnet 5

// §1 成本与 Token 经济: the token-class breakdown (cached / fresh /
// cache_write / out), per-model cache efficiency, and the role-level
// character and estimated-token split.
package report

import (
	"strconv"

	"vmr/internal/i18n"
)

// ---- §1 成本与 Token 经济 ----
func renderCostTokens(w func(string, ...any), rep *Report2, o Row, lang i18n.Lang) {
	t := i18n.Tokens(lang)
	w("## %s\n\n", t.Title)
	// token class breakdown
	w("%s\n\n", t.ClassBreakdownFmt(o.TokensKnown))
	h := t.ClassHeaders
	tokTbl := newTable(w, h[0], h[1], h[2])
	tokTbl.row(t.RowInputCached, fmtTokens(o.TokensInCached), t.OfInSuffix(pctStr(o.CacheHitRate)))
	freshShare := 0.0
	if o.TokensInCached+o.TokensInFresh > 0 {
		freshShare = float64(o.TokensInFresh) / float64(o.TokensInCached+o.TokensInFresh)
	}
	tokTbl.row(t.RowInputFresh, fmtTokens(o.TokensInFresh), t.OfFreshCachedSuffix(pctFloat(freshShare)))
	cw := ""
	if o.TokensInCacheWrite > 0 {
		cw = t.CacheWriteNote
	}
	tokTbl.row(t.RowInputCacheWrite, fmtTokens(o.TokensInCacheWrite), orDash(cw))
	tokTbl.row(t.RowOutput, fmtTokens(o.TokensOut), "-")
	if o.TokensReasoning > 0 {
		tokTbl.row(t.RowReasoning, fmtTokens(o.TokensReasoning), t.OfOutSuffix(pctStr(o.ReasoningShare)))
	}
	w("%s", t.BillingNote)
	if rep.Pricing == nil {
		w("%s", t.NoPricingNote)
	} else {
		w("%s", t.PricingNote(rep.Pricing.Disclaimer(lang)))
	}
	w("\n")

	// by-model cache efficiency (7 cols)
	w("%s\n\n", t.ByModelCacheTitle)
	mh := t.ByModelHeaders
	modelTbl := newTable(w, mh[0], mh[1], mh[2], mh[3], mh[4], mh[5], mh[6])
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests),
			cacheEffCell(m.CacheEfficiency, m.TokensKnown, m.Requests),
			fmtTokens(m.TokensInFresh), fmtTokens(m.TokensInCached), fmtTokens(m.TokensOut))
	}
	w("\n")

	// role chars + estimated tokens (D-family)
	if len(o.RoleChars) > 0 {
		w("%s\n\n", t.RoleCharsTitle)
		rh := t.RoleHeaders
		roleTbl := newTable(w, rh[0], rh[1], rh[2], rh[3])
		totalTok := sumRoleChars(o.RoleTokens)
		for _, role := range sortedRoles(o.RoleChars) {
			c := o.RoleChars[role]
			tk := o.RoleTokens[role]
			share := 0.0
			if totalTok > 0 {
				share = float64(tk) / float64(totalTok)
			}
			roleTbl.row(role, fmtTokens(c), fmtTokens(tk), pctStr(share))
		}
		w("%s", t.EstimatedTokensNote)
		w("%s", t.TakeawayNote)
	}
}
