// Ver 2026-09-15, by Opus 5

// §6.7 Compaction 还原 (CCR N-4) view model: every standalone compaction
// LLM call this period, with which sessions it links, how much it
// compressed, and a rule-based sample of what got swallowed. "不修复，只
// 揭示" — no LLM, no attempt to judge whether the loss mattered, just the
// observable facts so a human can look. Pairs with
// internal/i18n/report_compaction.go.
package report

import (
	"strconv"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func vmCompactionsSection(rep *Report2, lang i18n.Lang) SectionVM {
	t := i18n.Compaction(lang)
	sec := SectionVM{ID: "compactions", Title: t.Title}
	if len(rep.Compactions) == 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.None})
		return sec
	}
	tbl := &TableVM{Headers: t.Headers[:]}
	for _, c := range rep.Compactions {
		// TokensIn/TokensOut are only set when the call's usage parsed
		// (buildCompactions); a real compaction always consumes input, so
		// both-zero means "usage not captured", not a measured "0 → 0".
		sizeDelta := "-"
		if c.TokensIn != 0 || c.TokensOut != 0 {
			sizeDelta = fmtutil.FmtTokens(c.TokensIn) + " → " + fmtutil.FmtTokens(c.TokensOut)
		}
		tbl.row(fmtDisplayFull(c.TS), orDash(c.Summarizes), orDash(c.ContinuesTo),
			sizeDelta,
			vmRetentionRatio(c.TokensIn, c.TokensOut),
			vmEntitySample(c.SwallowedEntities))
	}
	tbl.note(t.Footnote)
	sec.Blocks = append(sec.Blocks, tbl)
	return sec
}

// vmRetentionRatio renders tokens_out/tokens_in as a percentage — how much
// of the original size the summary retained (a LOWER number is MORE
// compression; a number at or above 100% means this call didn't shrink
// anything, worth a second look at whether it's really a compaction rather
// than a heuristic false-positive). "-" when tokens_in is unknown or zero.
func vmRetentionRatio(in, out int64) string {
	if in <= 0 {
		return "-"
	}
	return pctStr(round2(float64(out) / float64(in)))
}

// vmEntitySample renders up to 3 swallowed entities inline, with a "+N
// more" tail when there are more — a triage aid in a table cell, not the
// full list (which stays in the JSON slice's swallowed_entities field).
func vmEntitySample(entities []string) string {
	if len(entities) == 0 {
		return "-"
	}
	const shown = 3
	n := len(entities)
	if n > shown {
		n = shown
	}
	sample := strings.Join(entities[:n], ", ")
	if len(entities) > shown {
		sample += " (+" + strconv.Itoa(len(entities)-shown) + " more)"
	}
	return sample
}
