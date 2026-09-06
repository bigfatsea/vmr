// Ver 2026-08-01, by Sonnet 5

// §7 效率与浪费: the findings list and the per-tool-shape detail behind
// the declared-but-never-called tool waste figure.
package report

import (
	"sort"
	"strconv"

	"vmr/internal/i18n"
)

// ---- §7 效率与浪费 ----
//
// This section does NOT render rep.Efficiency directly, even though by the
// time this runs it may already hold the correctly localized copy
// (cmd_report.go calls report.LocalizeEfficiency(rep, lang) before
// WriteJSON, ahead of Markdown — see docs/VirtualModelRouter_Design_v4_Analytics.md's
// "JSON 契约" subsection). Deliberately: reading it here would make
// Markdown's language correctness depend on WriteJSON having already run
// with this exact lang, an ordering contract with no compiler or runtime
// enforcement. Markdown computes its own independent localized copy
// instead, calling buildFindings again with the caller's real lang — a
// second, cheap, in-memory-only call over the same already-aggregated rep
// — so it's self-sufficient regardless of call order, and never writes
// back into rep.Efficiency.
func renderEfficiency(w func(string, ...any), rep *Report2, o Row, lang i18n.Lang) {
	t := i18n.Efficiency(lang)
	w("## %s\n\n", t.Title)
	renderToolWasteTotals(w, rep, lang)
	findings := buildFindings(rep, lang)
	if len(findings) > 0 {
		h := t.TableHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3], h[4])
		for _, f := range findings {
			tbl.row(f.Finding, f.Metric, f.Value, f.Implicated, f.Action)
		}
		w("\n")
	}
	// tool waste Top-5: compact table + per-shape used/never-called detail
	if len(rep.Tools) > 0 {
		w("%s\n\n", t.ToolWasteTitle)
		top := rep.Tools
		if len(top) > 5 {
			top = top[:5]
		}
		wh := t.ToolWasteHeaders
		toolTbl := newTable(w, wh[0], wh[1], wh[2], wh[3], wh[4], wh[5])
		for _, tl := range top {
			toolTbl.row(tl.Shape, strconv.Itoa(tl.Requests), strconv.Itoa(len(tl.Declared)), strconv.Itoa(tl.DistinctCalled),
				pctStr(tl.DeclareUtilization), fmtBytesGB(tl.SchemaWasteBytes))
		}
		w("\n")
		for _, tl := range top {
			renderToolShapeDetail(w, tl, t)
		}
		w("%s", t.WindowNote)
	}
}

// renderToolWasteTotals is §7's top-line: the four window totals leading
// the tool-waste block (bytes shipped, dead-weight bytes, wasted
// tokens, tool-set shape count) — the report's headline efficiency figures.
// Reuses i18n.ToolWaste's own labels so the JSON slice and this block
// can't disagree.
func renderToolWasteTotals(w func(string, ...any), rep *Report2, lang i18n.Lang) {
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
	w("> **%s** %s · **%s** %s (%.0f%%) · **%s** %s · **%s** %d\n\n",
		tw.StatShipped, fmtBytesGB(shipped),
		tw.StatDead, fmtBytesGB(waste), pct,
		tw.StatTokens, twTokens(waste),
		tw.StatShapes, len(rep.Tools))
}

// toolWasteBytesPerToken is the rough JSON→token divisor for the "≈ tokens
// wasted" figure. Tool-schema JSON is dense ASCII (keys, braces, quotes), so
// ~4 bytes/token holds close; the label carries the "≈". A precise count
// would mean threading the marshaled schema text through the report's
// aggregation just for this one display number — not worth it against a
// <10% error on dense ASCII.
const toolWasteBytesPerToken = 4

// twTokens renders a byte count as its rough wasted-token equivalent for
// the §7 tool-waste block.
func twTokens(bytes int64) string {
	tok := bytes / toolWasteBytesPerToken
	switch {
	case tok >= 1_000_000:
		return strconv.FormatFloat(float64(tok)/1e6, 'f', 1, 64) + "M"
	case tok >= 1_000:
		return strconv.FormatFloat(float64(tok)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(tok, 10)
	}
}

// renderToolShapeDetail lists, for one declared-tool-set shape, which tools
// were actually called (call count, descending) and which were declared but
// never invoked (alphabetical) — the data behind the summary table's
// "利用率" number, collapsed into <details> so a 60+-tool schema doesn't
// blow out the document while still keeping full detail one click away.
func renderToolShapeDetail(w func(string, ...any), t ToolShapeRow, tx i18n.EfficiencyText) {
	w("<details><summary>%s</summary>\n\n", tx.DetailSummary(t.Shape, t.Requests, len(t.Declared), t.DistinctCalled))
	if len(t.Calls) > 0 {
		type callCount struct {
			name string
			n    int
		}
		calls := make([]callCount, 0, len(t.Calls))
		for name, n := range t.Calls {
			calls = append(calls, callCount{name, n})
		}
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].n != calls[j].n {
				return calls[i].n > calls[j].n
			}
			return calls[i].name < calls[j].name
		})
		w("%s\n\n", tx.CalledToolsTitle(len(calls)))
		for i, c := range calls {
			w("%s\n", tx.CalledToolLine(i+1, c.name, c.n))
		}
		w("\n")
	}
	if len(t.NeverCalled) > 0 {
		names := append([]string(nil), t.NeverCalled...)
		sort.Strings(names)
		w("%s\n\n", tx.NeverCalledTitle(len(names)))
		for i, n := range names {
			w("%s\n", tx.NeverCalledLine(i+1, n))
		}
		w("\n")
	}
	w("</details>\n\n")
}
