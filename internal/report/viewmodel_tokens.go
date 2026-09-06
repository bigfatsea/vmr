// Ver 2026-09-15, by Opus 5

// §1 成本与 Token 经济 view model: the token-class breakdown, per-model
// cache efficiency, and the role-level character and estimated-token
// split. Pairs with internal/i18n/report_tokens.go.
package report

import (
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func vmTokensSection(rep *Report2, o Row, lang i18n.Lang) SectionVM {
	t := i18n.Tokens(lang)
	sec := SectionVM{ID: "tokens", Title: t.Title}

	// token class breakdown
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ClassBreakdownFmt(o.TokensKnown) + "\n\n"})
	tokTbl := &TableVM{Headers: t.ClassHeaders[:]}
	tokTbl.row(t.RowInputCached, fmtutil.FmtTokens(o.TokensInCached), t.OfInSuffix(pctStr(o.CacheHitRate)))
	// Both this row and the cached row above are shares of the same
	// denominator (o.TokensIn = fresh + cached + cache_write), so cached %,
	// fresh % and the cache_write remainder add up to 100% — "of in" means
	// the same thing on every input row.
	freshShare := 0.0
	if o.TokensIn > 0 {
		freshShare = float64(o.TokensInFresh) / float64(o.TokensIn)
	}
	tokTbl.row(t.RowInputFresh, fmtutil.FmtTokens(o.TokensInFresh), t.OfInSuffix(pctStr(freshShare)))
	cw := ""
	if o.TokensInCacheWrite > 0 {
		cw = t.CacheWriteNote
	}
	tokTbl.row(t.RowInputCacheWrite, fmtutil.FmtTokens(o.TokensInCacheWrite), orDash(cw))
	tokTbl.row(t.RowOutput, fmtutil.FmtTokens(o.TokensOut), "-")
	if o.TokensReasoning > 0 {
		tokTbl.row(t.RowReasoning, fmtutil.FmtTokens(o.TokensReasoning), t.OfOutSuffix(pctStr(o.ReasoningShare)))
	}
	if rep.Pricing == nil {
		tokTbl.Notes = vmNotes(t.BillingNote, t.NoPricingNote, "\n")
	} else {
		tokTbl.Notes = vmNotes(t.BillingNote, t.PricingNote(rep.Pricing.Disclaimer(lang)), "\n")
	}
	sec.Blocks = append(sec.Blocks, tokTbl)

	// by-model cache efficiency (7 cols)
	modelTbl := &TableVM{Title: t.ByModelCacheTitle, Headers: t.ByModelHeaders[:]}
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests),
			cacheEffCell(m.CacheEfficiency, m.TokensKnown, m.Requests),
			fmtutil.FmtTokens(m.TokensInFresh), fmtutil.FmtTokens(m.TokensInCached), fmtutil.FmtTokens(m.TokensOut))
	}
	sec.Blocks = append(sec.Blocks, modelTbl)

	// role chars + estimated tokens (D-family)
	if len(o.RoleChars) > 0 {
		roleTbl := &TableVM{Title: t.RoleCharsTitle, Headers: t.RoleHeaders[:]}
		totalTok := sumRoleChars(o.RoleTokens)
		for _, role := range sortedRoles(o.RoleChars) {
			c := o.RoleChars[role]
			tk := o.RoleTokens[role]
			share := 0.0
			if totalTok > 0 {
				share = float64(tk) / float64(totalTok)
			}
			roleTbl.row(role, fmtutil.FmtTokens(c), fmtutil.FmtTokens(tk), pctStr(share))
		}
		roleTbl.Notes = vmNotes(t.EstimatedTokensNote, t.TakeawayNote)
		sec.Blocks = append(sec.Blocks, roleTbl)
	}
	return sec
}
