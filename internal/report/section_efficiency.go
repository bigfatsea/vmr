// Ver 2026-07-28 19:20, by Opus 5

// §7 效率与浪费: the findings list and the per-tool-shape detail behind
// the declared-but-never-called tool waste figure.
package report

import (
	"sort"
	"strconv"
)

// ---- §7 效率与浪费 ----
func renderEfficiency(w func(string, ...any), rep *Report2, o Row) {
	w("## §7 效率与浪费 ⭐\n\n")
	if len(rep.Efficiency) > 0 {
		tbl := newTable(w, "发现", "指标", "值", "涉及", "建议")
		for _, f := range rep.Efficiency {
			tbl.row(f.Finding, f.Metric, f.Value, f.Implicated, f.Action)
		}
		w("\n")
	}
	// tool waste Top-5: compact table + per-shape used/never-called detail
	if len(rep.Tools) > 0 {
		w("**工具形态浪费 Top-5**（按浪费字节降序；完整明细见 vmr-report.json -> tools[]）\n\n")
		top := rep.Tools
		if len(top) > 5 {
			top = top[:5]
		}
		toolTbl := newTable(w, "形态", "请求", "声明", "已用", "利用率", "浪费字节")
		for _, t := range top {
			toolTbl.row(t.Shape, strconv.Itoa(t.Requests), strconv.Itoa(len(t.Declared)), strconv.Itoa(t.DistinctCalled),
				pctStr(t.DeclareUtilization), fmtBytesGB(t.SchemaWasteBytes))
		}
		w("\n")
		for _, t := range top {
			renderToolShapeDetail(w, t)
		}
		w("> 统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内，裁剪决策建议基于 ≥1 周日志。\n\n")
	}
}

// renderToolShapeDetail lists, for one declared-tool-set shape, which tools
// were actually called (call count, descending) and which were declared but
// never invoked (alphabetical) — the data behind the summary table's
// "利用率" number, collapsed into <details> so a 60+-tool schema doesn't
// blow out the document while still keeping full detail one click away.
func renderToolShapeDetail(w func(string, ...any), t ToolShapeRow) {
	w("<details><summary>%s · %d 请求 · 声明 %d 个 · 实际调用 %d 个</summary>\n\n",
		t.Shape, t.Requests, len(t.Declared), t.DistinctCalled)
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
		w("**调用过的工具（%d 个，按调用次数降序）：**\n\n", len(calls))
		for i, c := range calls {
			w("%d. %s (%d 次)\n", i+1, c.name, c.n)
		}
		w("\n")
	}
	if len(t.NeverCalled) > 0 {
		names := append([]string(nil), t.NeverCalled...)
		sort.Strings(names)
		w("**声明但从未调用（%d 个，按字母序）：**\n\n", len(names))
		for i, n := range names {
			w("%d. %s\n", i+1, n)
		}
		w("\n")
	}
	w("</details>\n\n")
}
