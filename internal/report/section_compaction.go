// Ver 2026-07-29 23:30, by Sonnet 5

// §6.7 Compaction 还原 (design doc §6.4 / CCR N-4, Appendix C.5 T3.3): every
// standalone compaction LLM call this period, with which sessions it links,
// how much it compressed, and a rule-based sample of what got swallowed.
// "不修复，只揭示" — no LLM, no attempt to judge whether the loss mattered,
// just the observable facts so a human can look.
package report

import (
	"strconv"
	"strings"
)

func renderCompactions(w func(string, ...any), rep *Report2) {
	w("## §6.7 Compaction 还原 ⭐\n\n")
	if len(rep.Compactions) == 0 {
		w("（本期无 compaction 调用）\n\n")
		return
	}
	tbl := newTable(w, "时间", "压缩会话", "续接会话", "tokens_in → tokens_out", "保留比", "吞掉的实体（样例）")
	for _, c := range rep.Compactions {
		tbl.row(cut(c.TS, 19), orDash(c.Summarizes), orDash(c.ContinuesTo),
			fmtTokens(c.TokensIn)+" → "+fmtTokens(c.TokensOut),
			retentionRatio(c.TokensIn, c.TokensOut),
			entitySample(c.SwallowedEntities))
	}
	w("\n")
}

// retentionRatio renders tokens_out/tokens_in as a percentage — how much of
// the original size the summary retained (a LOWER number is MORE
// compression; a number at or above 100% means this call didn't shrink
// anything, worth a second look at whether it's really a compaction rather
// than a heuristic false-positive — collect()'s Compaction detection is
// deliberately loose, see its own comment). "-" when tokens_in is unknown or
// zero (nothing to divide by).
func retentionRatio(in, out int64) string {
	if in <= 0 {
		return "-"
	}
	return pctStr(round2(float64(out) / float64(in)))
}

// entitySample renders up to 3 swallowed entities inline, with a "+N more"
// tail when there are more — a triage aid in a table cell, not the full
// list (which stays in vmr-report.json's swallowed_entities field).
func entitySample(entities []string) string {
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
