// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_compaction.go (§6.7 Compaction Reconstruction).
package i18n

// CompactionText is section_compaction.go's text, in one language.
type CompactionText struct {
	Title    string
	None     string
	Headers  [6]string // time, compacted session, continues to, tokens_in -> tokens_out, retention, swallowed entities (sample)
	Footnote string
}

func Compaction(lang Lang) CompactionText {
	if lang == ZH {
		return CompactionText{
			Title:    "§6.7 Compaction 还原 ⭐",
			None:     "（本期无 compaction 调用）\n\n",
			Headers:  [6]string{"时间", "压缩会话", "续接会话", "tokens_in → tokens_out", "保留比", "吞掉的实体（样例）"},
			Footnote: "> 保留比 = tokens_out / tokens_in；保留比 ≥ 100% 表示输出未缩小（可能为规则识别假阳性或结构化扩展）。\n\n",
		}
	}
	return CompactionText{
		Title:    "§6.7 Compaction Reconstruction ⭐",
		None:     "(no compaction calls this period)\n\n",
		Headers:  [6]string{"Time", "Summarized Session", "Continues To", "tokens_in → tokens_out", "Retention", "Swallowed Entities (sample)"},
		Footnote: "> Retention = tokens_out / tokens_in; retention ≥ 100% means output did not shrink (may indicate detector false-positive or structured expansion).\n\n",
	}
}
