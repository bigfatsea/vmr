// Ver 2026-08-29, by Sonnet 5

// Chrome strings for the §7 tool-waste totals line (vmToolWasteTotals in
// internal/report/viewmodel_efficiency.go). Fixed UI strings only; every
// number comes from the report's own rep.Tools rows.
package i18n

// ToolWasteText is the §7 tool-waste totals line's labels, in one language.
type ToolWasteText struct {
	StatShipped string
	StatDead    string
	StatTokens  string
	StatShapes  string
}

// ToolWaste returns the totals-line labels for lang.
func ToolWaste(lang Lang) ToolWasteText {
	if lang == ZH {
		return ToolWasteText{
			StatShipped: "累计发出",
			StatDead:    "其中死重",
			StatTokens:  "≈ 浪费 token",
			StatShapes:  "工具集形态",
		}
	}
	return ToolWasteText{
		StatShipped: "Total shipped",
		StatDead:    "Dead weight",
		StatTokens:  "≈ tokens wasted",
		StatShapes:  "Tool-set shapes",
	}
}
