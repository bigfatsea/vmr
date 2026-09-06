// Ver 2026-09-15, by Opus 5

// §7 效率与浪费 view model: the findings list and the per-tool-shape
// detail behind the declared-but-never-called tool waste figure. Pairs
// with internal/i18n/report_efficiency.go (plus report_toolwaste.go's
// stat labels for the top-line totals).
//
// This builder does NOT read rep.Efficiency even when it already holds
// localized copy: Markdown's language correctness must not depend on
// WriteJSON having run first with this exact lang (see
// section_efficiency.go for the full reasoning, mirrored here).
package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

func vmEfficiencySection(rep *Report2, o Row, lang i18n.Lang) SectionVM {
	_ = o
	t := i18n.Efficiency(lang)
	sec := SectionVM{ID: "efficiency", Title: t.Title}
	vmToolWasteTotals(&sec, rep, lang)
	findings := buildFindings(rep, lang)
	if len(findings) > 0 {
		tbl := &TableVM{Headers: t.TableHeaders[:]}
		for _, f := range findings {
			tbl.row(f.Finding, f.Metric, f.Value, f.Implicated, f.Action)
		}
		sec.Blocks = append(sec.Blocks, tbl)
	}
	// tool waste Top-5: compact table + per-shape used/never-called detail
	if len(rep.Tools) > 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ToolWasteTitle + "\n\n"})
		top := rep.Tools
		if len(top) > 5 {
			top = top[:5]
		}
		toolTbl := &TableVM{Headers: t.ToolWasteHeaders[:]}
		for _, tl := range top {
			toolTbl.row(tl.Shape, strconv.Itoa(tl.Requests), strconv.Itoa(len(tl.Declared)), strconv.Itoa(tl.DistinctCalled),
				pctStr(tl.DeclareUtilization), fmtBytesGB(tl.SchemaWasteBytes))
		}
		for _, tl := range top {
			toolTbl.note(vmToolShapeDetail(tl, t))
		}
		toolTbl.note(t.WindowNote)
		sec.Blocks = append(sec.Blocks, toolTbl)
	}
	return sec
}

// vmToolWasteTotals is §7's top-line: the four window totals leading the
// tool-waste block (bytes shipped, dead-weight bytes, wasted tokens,
// tool-set shape count) — the report's headline efficiency figures.
// Reuses i18n.ToolWaste's own labels so the JSON slice and this block
// can't disagree.
func vmToolWasteTotals(sec *SectionVM, rep *Report2, lang i18n.Lang) {
	if len(rep.Tools) == 0 {
		return
	}
	var shipped, waste int64
	for _, tl := range rep.Tools {
		shipped += tl.SchemaBytesShipped
		waste += tl.SchemaWasteBytes
	}
	pct := 0.0
	if shipped > 0 {
		pct = float64(waste) / float64(shipped) * 100
	}
	tw := i18n.ToolWaste(lang)
	sec.Blocks = append(sec.Blocks, ParaVM{Text: fmt.Sprintf("> **%s** %s · **%s** %s (%.0f%%) · **%s** %s · **%s** %d\n\n",
		tw.StatShipped, fmtBytesGB(shipped),
		tw.StatDead, fmtBytesGB(waste), pct,
		tw.StatTokens, vmTwTokens(waste),
		tw.StatShapes, len(rep.Tools))})
}

// vmToolWasteBytesPerToken is the rough JSON→token divisor for the
// "≈ tokens wasted" figure. Tool-schema JSON is dense ASCII (keys, braces,
// quotes), so ~4 bytes/token holds close; the label carries the "≈".
const vmToolWasteBytesPerToken = 4

// vmTwTokens renders a byte count as its rough wasted-token equivalent for
// the §7 tool-waste block.
func vmTwTokens(bytes int64) string {
	tok := bytes / vmToolWasteBytesPerToken
	switch {
	case tok >= 1_000_000:
		return strconv.FormatFloat(float64(tok)/1e6, 'f', 1, 64) + "M"
	case tok >= 1_000:
		return strconv.FormatFloat(float64(tok)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(tok, 10)
	}
}

// vmToolShapeDetail lists, for one declared-tool-set shape, which tools
// were actually called (call count, descending) and which were declared
// but never invoked (alphabetical) — the data behind the summary table's
// "利用率" number, collapsed into <details> so a 60+-tool schema doesn't
// blow out the document while still keeping full detail one click away.
// Rendered as one pre-composed note under the tool table.
func vmToolShapeDetail(tl ToolShapeRow, tx i18n.EfficiencyText) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<details><summary>%s</summary>\n\n", tx.DetailSummary(tl.Shape, tl.Requests, len(tl.Declared), tl.DistinctCalled))
	if len(tl.Calls) > 0 {
		type callCount struct {
			name string
			n    int
		}
		calls := make([]callCount, 0, len(tl.Calls))
		for name, n := range tl.Calls {
			calls = append(calls, callCount{name, n})
		}
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].n != calls[j].n {
				return calls[i].n > calls[j].n
			}
			return calls[i].name < calls[j].name
		})
		fmt.Fprintf(&b, "%s\n\n", tx.CalledToolsTitle(len(calls)))
		for i, c := range calls {
			fmt.Fprintf(&b, "%s\n", tx.CalledToolLine(i+1, c.name, c.n))
		}
		fmt.Fprintf(&b, "\n")
	}
	if len(tl.NeverCalled) > 0 {
		names := append([]string(nil), tl.NeverCalled...)
		sort.Strings(names)
		fmt.Fprintf(&b, "%s\n\n", tx.NeverCalledTitle(len(names)))
		for i, n := range names {
			fmt.Fprintf(&b, "%s\n", tx.NeverCalledLine(i+1, n))
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "</details>\n\n")
	return b.String()
}
