// Ver 2026-09-15, by pi

// Deprecated: legacy RenderMarkdown eating *Journey was deleted in Phase 3
// in favor of viewmodel.go's RenderMarkdownFromSummary (D11).
// Pure formatting helpers (codeFence, escapeHTML, escapeCell, pctStr) retained here.
package journey

import (
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/reqdetail"
)

// codeFence wraps s in a fenced code block whose fence is longer than any
// backtick run inside s, so message content can never break out of its block.
func codeFence(s string) string {
	n := 3
	run := 0
	for _, r := range s {
		if r == '`' {
			run++
			if run >= n {
				n = run + 1
			}
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", n)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return f + "\n" + s + f + "\n"
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
