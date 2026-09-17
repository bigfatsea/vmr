// Ver 2026-09-15, by pi

// Pure Markdown formatting helpers (codeFence, escapeHTML, escapeCell,
// pctStr). The *Journey-based RenderMarkdown that used to live here is now
// viewmodel.go's RenderMarkdownFromSummary (D11).
package journey

import (
	"vmr/internal/fmtutil"
	"vmr/internal/reqdetail"
)

// codeFence wraps s in a fenced code block whose fence is longer than any
// backtick run inside s, so message content can never break out of its block.
func codeFence(s string) string {
	return fmtutil.CodeFence(s)
}

// escapeHTML neutralizes user/model-derived text before it enters raw Markdown/HTML.
func escapeHTML(s string) string {
	return reqdetail.EscapeHTML(s)
}

// escapeCell neutralizes a value for use inside a Markdown table cell.
func escapeCell(s string) string {
	return reqdetail.EscapeCell(s)
}

// pctStr is journey's local 0-decimal alias for fmtutil.FmtPercent.
func pctStr(f float64) string {
	return fmtutil.FmtPercent(f, 0)
}
