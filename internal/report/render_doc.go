// Ver 2026-07-28 19:20, by Opus 5

// The report document skeleton: the section running order, the summary and
// highlights that open it, the closing pointers, and the one table
// primitive every section shares. Each numbered section's own body lives in
// its own section_*.go file — adding a section means adding a file and one
// line to Markdown below, not editing a renderer that keeps growing.
package report

import (
	"fmt"
	"strconv"
	"strings"
)

// mdTable collapses the "write a header row + separator row, then one row
// per data item" pattern repeated ~20 times across the section files into one
// declaration + one row() call per item, instead of each call site hand-
// writing its own "| h1 | h2 |...|\n|---|---|...|\n" header and %s-joined
// row format string. Column *formatting* stays the caller's job: the tables
// differ too much in per-cell logic (conditional flags, composite
// "a / b / c" cells, dynamic header text, low-n markers) for a template
// layer to express without becoming Go code again — untyped, and failing at
// run time instead of compile time.
type mdTable struct {
	w    func(string, ...any)
	cols int
}

// newTable writes the header and separator rows immediately (every call
// site already writes the header right before its data rows) and returns a
// handle for the data rows.
func newTable(w func(string, ...any), headers ...string) *mdTable {
	w("%s", "| "+strings.Join(headers, " | ")+" |\n")
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	w("%s", "|"+strings.Join(seps, "|")+"|\n")
	return &mdTable{w: w, cols: len(headers)}
}

// row writes one data row. len(cells) must equal the header's column count
// — a mismatch is a programmer error in a section renderer, not malformed input, so
// it panics immediately rather than silently emitting a ragged table.
//
// Cells are passed to w via a literal "%s" verb, never interpolated
// straight into the format string: many cells legitimately contain a raw
// "%" (percentages, cache-efficiency figures), which fmt.Fprintf would
// otherwise try to parse as a verb and corrupt into "%!s(MISSING)" — caught
// by diffing real report output against the pre-2.6 renderer, not by the
// unit tests (whose fixtures happened not to exercise this).
func (t *mdTable) row(cells ...string) {
	if len(cells) != t.cols {
		panic(fmt.Sprintf("mdTable: row has %d cells, header has %d", len(cells), t.cols))
	}
	t.w("%s", "| "+strings.Join(cells, " | ")+" |\n")
}

// Markdown renders the full vmr-report.md document.
func Markdown(rep *Report2) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	o := rep.Overall

	// ---- header ----
	w("# VMR 用量报告\n\n")
	w("数据源: %s · format %d · %d 条记录（%d 坏行）· %s – %s\n",
		strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors,
		cut(rep.Meta.From, 19), cut(rep.Meta.To, 19))
	w("详单见 [vmr-requests.md](./vmr-requests.md) · 同名 .json")
	withSibling := clientsWithSiblingFile(rep)
	for _, c := range rep.ByClient {
		if withSibling[c.ClientKey] {
			w(" · [-%s](./vmr-requests-%s.md)", c.ClientKey, sanitize(c.ClientKey))
		}
	}
	w("\n\n")

	renderSummary(w, rep, o)
	renderCostTokens(w, rep, o)
	renderCostEstimate(w, rep)
	renderReliability(w, rep, o)
	renderLatency(w, rep, o)
	renderWorkload(w, rep, o)
	renderSessions(w, rep)
	renderStickyEffect(w, rep)
	renderEndpointValue(w, rep)
	renderEfficiency(w, rep, o)
	renderRequestIndexLink(w, rep)
	renderAppendix(w, rep)
	return b.String()
}

// ---- §0 摘要 ----
func renderSummary(w func(string, ...any), rep *Report2, o Row) {
	w("## §0 摘要\n\n")
	tbl := newTable(w, "请求", "成功率", "计费输入(fresh)⭐", "缓存效率⭐", "p95 耗时")
	p95n := o.RequestsWithDur
	tbl.row(fmt.Sprintf("%d（fallback %d / trunc %d）", o.Requests, o.Fallbacks, o.Truncated),
		pctStr2(o.OK, o.Requests),
		fmtTokens(o.TokensInFresh),
		cacheEffCell(o.CacheEfficiency, o.TokensKnown, o.Requests),
		durCell(o.DurMSP95, p95n))
	w("\n")
	w("**亮点 (auto):**\n")
	for _, h := range highlights(rep) {
		w("- %s\n", h)
	}
	w("\n")
}

// highlights generates ≤3 auto highlights from the finished buckets.
func highlights(rep *Report2) []string {
	var out []string
	// 1. workload with low cache-eff
	for _, wl := range rep.Workloads {
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			out = append(out, fmt.Sprintf("⚠️ **%s 工作负载缓存效率 %s** - %s fresh tokens（占该负载输入大头）",
				wl.Class, pctStr(wl.CacheEfficiency), fmtTokens(wl.TokensInFresh)))
			break
		}
	}
	// 2. tool shape with low utilization
	for _, t := range rep.Tools {
		if t.Requests > 0 && t.DeclareUtilization < 0.20 && t.SchemaBytesShipped > 0 {
			out = append(out, fmt.Sprintf("⚠️ **工具声明 %s** - 跨 %d 请求发送 %s schema，利用率 %s（%d 个从未调用）",
				t.Shape, t.Requests, fmtBytesGB(t.SchemaBytesShipped),
				pctStr(t.DeclareUtilization), len(t.NeverCalled)))
			break
		}
	}
	// 3. worst endpoint error rate
	var worst *EndpointRow
	for i := range rep.EndpointsAll {
		e := &rep.EndpointsAll[i]
		if worst == nil || e.ErrorRate > worst.ErrorRate {
			worst = e
		}
	}
	if worst != nil && worst.Attempts >= 4 && worst.ErrorRate > 5 {
		top := topErrorClass(worst)
		out = append(out, fmt.Sprintf("⚠️ **端点 %s 错误率 %s/100**（最差）%s",
			worst.Endpoint, strconv.FormatFloat(float64(worst.ErrorRate), 'f', 1, 64), top))
	}
	if len(out) == 0 {
		out = append(out, "（无明显异常：缓存效率、工具利用率、端点错误率均在正常区间）")
	}
	return out
}

// topErrorClassCount finds the error class with the highest count,
// iterating sortedKeysInt(classes) rather than ranging the map directly —
// found while verifying 2.6's table refactor against real report output: a
// tie (two classes with the same count) resolved to whichever class Go's
// randomized map order happened to visit last, so the same input could
// report a different "主因" class between two runs of the same binary.
// Same bug class, same fix, as the sort.Slice tie-break fix earlier applied
// to aggregate.go's Build — ties now always resolve to the
// alphabetically-first class name.
func topErrorClassCount(classes map[string]int) (cls string, n int) {
	for _, c := range sortedKeysInt(classes) {
		if m := classes[c]; m > n {
			cls, n = c, m
		}
	}
	return cls, n
}

func topErrorClass(e *EndpointRow) string {
	if len(e.ErrorClasses) == 0 {
		return ""
	}
	cls, n := topErrorClassCount(e.ErrorClasses)
	return "，主因 " + cls + " ×" + strconv.Itoa(n)
}

// ---- §8 请求详单 ----
func renderRequestIndexLink(w func(string, ...any), rep *Report2) {
	w("## §8 请求详单\n\n")
	w("每条记录（Chat User -> Session -> Task -> Turn）见 [vmr-requests.md](./vmr-requests.md)。\n")
	withSibling := clientsWithSiblingFile(rep)
	first := true
	for _, c := range rep.ByClient {
		if !withSibling[c.ClientKey] {
			continue
		}
		if first {
			w("per-client: ")
			first = false
		} else {
			w(" · ")
		}
		w("[-%s](./vmr-requests-%s.md)", c.ClientKey, sanitize(c.ClientKey))
	}
	if !first {
		w("\n")
	}
	w("单请求全量捕获（req/resp/SSE）见 `details/*.md`。\n\n")
}

// ---- 附录 ----
func renderAppendix(w func(string, ...any), rep *Report2) {
	w("## 附录 数据源与方法论\n\n")
	w("- 输入: %s · format %d · %d 记录 / %d 坏行\n", strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors)
	w("- 时段: %s – %s (本地时区)\n", cut(rep.Meta.From, 19), cut(rep.Meta.To, 19))
	w("- 百分位: %s\n", rep.Meta.PercentileMethod)
	w("- n 基准: 每个百分位标注 n（= ttft_known / requests_with_dur / stream_known）；n<20 标 ⚠️low-n。\n")
	w("- 比值低置信度: cache_efficiency 等比值指标的分母 / 总请求数 < 90%% 时标注脚注 ¹。\n")
	w("- ⭐ 标记: 该列为衍生/预估指标（非上游直接返回值），解读时请结合样本量与口径说明。\n")
	w("- 计费口径: fresh + cache_write(溢价) + out；缓存命中按各厂免费/极低价。%s\n", orDash2(rep.Pricing == nil, "未配置定价时不显示 $。", ""))
	w("- 慢请求阈值: %ds\n", rep.Meta.SlowThreshold/1000)
}

// ---- cell/format helpers ----
