// Ver 2026-07-25, by Sonnet 5

// Markdown rendering of Report2, organized around nine numbered sections
// (§0-§8: summary, token economics, cost estimate, reliability, latency,
// workload, sessions, efficiency/waste, request-index pointer — the §2 cost
// section is new since the original "eight operator questions" design in
// REPORT_REDESIGN_V2.zh.md §5, added when per-endpoint/per-client cost
// estimates got their own section instead of a subsection of §1). Every
// table stays ≤7 columns; percentiles carry their basis n with a
// ⚠️low-n flag when n<20; ratio metrics whose denominator falls below 90% of
// total requests get a footnote; hourly series render as mermaid
// xychart-beta charts.

package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// mdTable collapses the "write a header row + separator row, then one row
// per data item" pattern repeated ~20 times across this file into one
// declaration + one row() call per item, instead of each call site hand-
// writing its own "| h1 | h2 |...|\n|---|---|...|\n" header and %s-joined
// row format string. Column *formatting* stays the caller's job — the ~20
// tables here differ too much in per-cell logic (conditional flags,
// composite "a / b / c" cells, dynamic header text) to templatize further,
// and a heavier templating layer would be over-abstraction for a Markdown
// renderer with this few consumers.
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
// — a mismatch is a programmer error in this file, not malformed input, so
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

// ---- §1 成本与 Token 经济 ----
func renderCostTokens(w func(string, ...any), rep *Report2, o Row) {
	w("## §1 成本与 Token 经济\n\n")
	// token class breakdown
	w("**Token 类别分解**（basis: %d 条带 usage 的记录）\n\n", o.TokensKnown)
	tokTbl := newTable(w, "类别", "数量", "占比")
	tokTbl.row("输入-缓存命中", fmtTokens(o.TokensInCached), pctStr(o.CacheHitRate)+" of in")
	freshShare := 0.0
	if o.TokensInCached+o.TokensInFresh > 0 {
		freshShare = float64(o.TokensInFresh) / float64(o.TokensInCached+o.TokensInFresh)
	}
	tokTbl.row("输入-fresh ⭐", fmtTokens(o.TokensInFresh), pctFloat(freshShare)+" of (fresh+cached)")
	cw := ""
	if o.TokensInCacheWrite > 0 {
		cw = "（Anthropic 缓存创建，溢价计费）"
	}
	tokTbl.row("输入-cache_write", fmtTokens(o.TokensInCacheWrite), orDash(cw))
	tokTbl.row("输出", fmtTokens(o.TokensOut), "-")
	if o.TokensReasoning > 0 {
		tokTbl.row("└ 其中 reasoning", fmtTokens(o.TokensReasoning), pctStr(o.ReasoningShare)+" of out")
	}
	w("\n> 计费口径：fresh + cache_write(×溢价) + out。缓存命中按各厂免费/极低价计。\n")
	if rep.Pricing == nil {
		w("> 未配置定价 -> 不显示 $ 估算；配置后见 §2 成本估算。\n")
	} else {
		w("> %s 详见 §2 成本估算。\n", rep.Pricing.Disclaimer())
	}
	w("\n")

	// by-model cache efficiency (7 cols)
	w("**按模型缓存效率** ⭐\n\n")
	modelTbl := newTable(w, "模型", "协议", "请求", "缓存效率⭐", "fresh", "cached", "out")
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests),
			cacheEffCell(m.CacheEfficiency, m.TokensKnown, m.Requests),
			fmtTokens(m.TokensInFresh), fmtTokens(m.TokensInCached), fmtTokens(m.TokensOut))
	}
	w("\n")

	// role chars + estimated tokens (D-family)
	if len(o.RoleChars) > 0 {
		w("**请求消息字符、预估Token及占比**\n\n")
		roleTbl := newTable(w, "角色", "字符", "预估Token⭐", "占比⭐")
		totalTok := sumRoleChars(o.RoleTokens)
		for _, role := range sortedRoles(o.RoleChars) {
			c := o.RoleChars[role]
			t := o.RoleTokens[role]
			share := 0.0
			if totalTok > 0 {
				share = float64(t) / float64(totalTok)
			}
			roleTbl.row(role, fmtTokens(c), fmtTokens(t), pctStr(share))
		}
		w("\n> 预估Token⭐：上游 usage 不按角色拆分，无法拿到真实值，这里用粗估口径（ASCII ~4B/token，多字节 UTF-8 ~2B/token，同 §1 计费口径）；占比按预估Token 计算。\n")
		w("> takeaway: tool 结果占比最大时，上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。\n\n")
	}
}

// ---- §2 成本估算 ----
func renderCostEstimate(w func(string, ...any), rep *Report2) {
	w("## §2 成本估算\n\n")
	if rep.Pricing == nil {
		w("未配置定价（`-pricing pricing.yaml`），本章节不显示 $ 估算。\n\n")
		return
	}
	w("> %s\n\n", rep.Pricing.Disclaimer())
	cur := rep.Pricing.Currency

	hasModel := false
	for _, m := range rep.ByModel {
		if m.CostEstimate != nil {
			hasModel = true
			break
		}
	}
	if hasModel {
		w("**按模型估算成本**（%s）\n\n", cur)
		tbl := newTable(w, "模型", "协议", "fresh", "out", "估算成本")
		for _, m := range rep.ByModel {
			if m.CostEstimate != nil {
				tbl.row(m.Model, m.Protocol, fmtTokens(m.TokensInFresh), fmtTokens(m.TokensOut),
					fmt.Sprintf("%.4f %s", *m.CostEstimate, cur))
			}
		}
		w("\n")
	}

	hasEndpoint := false
	for _, e := range rep.EndpointsAll {
		if e.CostEstimate != nil {
			hasEndpoint = true
			break
		}
	}
	if hasEndpoint {
		w("**按端点估算成本**（%s，跨日合并）\n\n", cur)
		tbl := newTable(w, "端点", "fresh", "out", "估算成本")
		for _, e := range rep.EndpointsAll {
			if e.CostEstimate != nil {
				tbl.row(e.Endpoint, fmtTokens(e.TokensInFresh), fmtTokens(e.TokensOut),
					fmt.Sprintf("%.4f %s", *e.CostEstimate, cur))
			}
		}
		w("\n")
	}

	hasClient := false
	for _, c := range rep.ByClient {
		if c.CostEstimate != nil {
			hasClient = true
			break
		}
	}
	if hasClient {
		w("**按客户端估算成本**（%s）\n\n", cur)
		tbl := newTable(w, "client_key", "fresh", "out", "估算成本")
		for _, c := range rep.ByClient {
			if c.CostEstimate != nil {
				tbl.row(c.ClientKey, fmtTokens(c.TokensInFresh), fmtTokens(c.TokensOut),
					fmt.Sprintf("%.4f %s", *c.CostEstimate, cur))
			}
		}
		w("\n")
	}

	if !hasModel && !hasEndpoint && !hasClient {
		w("配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n")
	}

	// Freeze the exact pricing.yaml used for this report, collapsed by
	// default — pricing.yaml can keep changing after the fact; embedding it
	// verbatim means a later reader of this report is never left guessing
	// which price snapshot the $ figures above actually came from.
	if len(rep.Pricing.Raw) > 0 {
		w("%s\n", details("本次使用的定价配置（冻结快照，点击展开）", codeFence(string(rep.Pricing.Raw))))
	}
}

// ---- §3 可靠性 ----
func renderReliability(w func(string, ...any), rep *Report2, o Row) {
	w("## §3 可靠性\n\n")
	w("**结果分布**\n\n")
	outcomeTbl := newTable(w, "ok", "error", "canceled", "truncated", "fallback(恢复/失败)⭐")
	outcomeTbl.row(strconv.Itoa(o.OK), strconv.Itoa(o.Errors), strconv.Itoa(o.Canceled), strconv.Itoa(o.Truncated),
		fmt.Sprintf("%d (%d/%d)", o.Fallbacks, o.FallbackRecovered, o.FallbackFailed))
	w("\n")

	// endpoint health (6 cols) - use EndpointsAll for cross-date view, split by protocol
	if len(rep.EndpointsAll) > 0 {
		w("**端点健康**（跨日合并）\n\n")
		protocols, byProto := protocolBuckets(rep.EndpointsAll)
		for _, p := range protocols {
			w("*%s*\n\n", p)
			tbl := newTable(w, "端点", "尝试", "成功", "可用度", "错误率⭐", "首要错误")
			for _, e := range byProto[p] {
				marker := ""
				if e.ErrorRate > 10 {
					marker = " ⚠️"
				}
				tbl.row(e.Endpoint, strconv.Itoa(e.Attempts), strconv.Itoa(e.OK),
					pctStr(e.Availability), pctHundred(e.ErrorRate)+marker,
					topErrorClassShort(e))
			}
			w("\n")
		}
	}

	// error class × endpoint (only non-zero), split by protocol
	nonzero := false
	for _, e := range rep.EndpointsAll {
		if len(e.ErrorClasses) > 0 {
			nonzero = true
			break
		}
	}
	if nonzero {
		w("**错误类别 × 端点**（仅非零）\n\n")
		protocols, byProto := protocolBuckets(rep.EndpointsAll)
		for _, p := range protocols {
			rows := byProto[p]
			hasAny := false
			for _, e := range rows {
				if len(e.ErrorClasses) > 0 {
					hasAny = true
					break
				}
			}
			if !hasAny {
				continue
			}
			w("*%s*\n\n", p)
			tbl := newTable(w, "端点", "类别", "计数")
			for _, e := range rows {
				for _, cls := range sortedKeysInt(e.ErrorClasses) {
					n := e.ErrorClasses[cls]
					rate := 0.0
					if e.Attempts > 0 {
						rate = float64(n) / float64(e.Attempts) * 100
					}
					tbl.row(e.Endpoint, cls, fmt.Sprintf("%d(%s)", n, pctHundred(rate)))
				}
			}
			w("\n")
		}
	}

	// error timeline sparkline (per hour error counts from HoursOfDay)
	if len(rep.HoursOfDay) > 0 {
		errs := make([]int64, 24)
		for _, h := range rep.HoursOfDay {
			if h.Hour >= 0 && h.Hour < 24 {
				errs[h.Hour] += int64(h.Errors)
			}
		}
		w("**错误时间线**（错误数 / 小时）\n\n%s", mermaidHourBar("错误数 / 小时", "错误数", errs))
		// callout the peak hour
		peakH, peakN := 0, int64(0)
		for i, n := range errs {
			if n > peakN {
				peakH, peakN = i, n
			}
		}
		if peakN > 0 {
			w("> 错误集中在 %02d:00（共 %d 条）。\n\n", peakH, peakN)
		}
	}
}

// ---- §4 延迟与吞吐 ----
func renderLatency(w func(string, ...any), rep *Report2, o Row) {
	w("## §4 延迟与吞吐\n\n")
	tbl := newTable(w, "模型", "协议", "ttft p50/p95 (n)", "dur p50/p95/max (n)",
		fmt.Sprintf("slow>%ds⭐", SlowThresholdMS/1000), "tok/s")
	byModelSpeed := append([]Row(nil), rep.ByModel...)
	sort.SliceStable(byModelSpeed, func(i, j int) bool { return byModelSpeed[i].TokOutPerSec > byModelSpeed[j].TokOutPerSec })
	for _, m := range byModelSpeed {
		tbl.row(m.Model, m.Protocol,
			ppCell(m.TTFTMSP50, m.TTFTMSP95, 0, m.TTFTKnown),
			ppCell(m.DurMSP50, m.DurMSP95, m.DurMSMax, m.RequestsWithDur),
			strconv.Itoa(m.SlowRequests),
			tokPerSec(m.TokOutPerSec))
	}
	w("\n> 全局 p95 dur %s，max %s。按 tok/s 降序排列。\n",
		fmtDurMS(o.DurMSP95), fmtDurMS(o.DurMSMax))
	w("> 若 coding 的慢主要来自长流式输出，而非首字延迟，参见每模型的 ttft vs dur 差值。\n\n")

	// by endpoint, split by protocol (跨日合并, same basis as §3 端点健康), each
	// group sorted by tok/s descending
	if len(rep.EndpointsAll) > 0 {
		w("**按端点**（跨日合并）\n\n")
		protocols, byProto := protocolBuckets(rep.EndpointsAll)
		for _, p := range protocols {
			rows := append([]EndpointRow(nil), byProto[p]...)
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].TokOutPerSec > rows[j].TokOutPerSec })
			w("*%s*\n\n", p)
			epTbl := newTable(w, "端点", "ttft p50/p95 (n)", "dur p50/p95/max (n)",
				fmt.Sprintf("slow>%ds⭐", SlowThresholdMS/1000), "tok/s")
			for _, e := range rows {
				epTbl.row(e.Endpoint,
					ppCell(e.TTFTMSP50, e.TTFTMSP95, 0, e.TTFTKnown),
					ppCell(e.DurMSP50, e.DurMSP95, e.DurMSMax, e.RequestsWithDur),
					strconv.Itoa(e.SlowRequests),
					tokPerSec(e.TokOutPerSec))
			}
			w("\n")
		}
	}
}

// ---- §5 负载分布 ----
func renderWorkload(w func(string, ...any), rep *Report2, o Row) {
	w("## §5 负载分布\n\n")
	// by virtual model (6)
	w("**按虚拟模型**\n\n")
	modelTbl := newTable(w, "模型", "协议", "请求", "成功率", "fresh/cached/out", "dur p50/p95")
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests), pctStr2(m.OK, m.Requests),
			fmt.Sprintf("%s / %s / %s", fmtTokens(m.TokensInFresh), fmtTokens(m.TokensInCached), fmtTokens(m.TokensOut)),
			p5095Cell(m.DurMSP50, m.DurMSP95))
	}
	w("\n")
	// by workload class (6)
	w("**按工作负载类**\n\n")
	wlTbl := newTable(w, "类", "请求", "fresh", "缓存效率⭐", "tool_call_rate", "dur p50/p95")
	for _, wl := range rep.Workloads {
		flag := ""
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			flag = " ⚠️"
		}
		wlTbl.row(wl.Class, strconv.Itoa(wl.Requests), fmtTokens(wl.TokensInFresh),
			cacheEffCell(wl.CacheEfficiency, wl.TokensKnown, wl.Requests)+flag,
			pctStr(wl.ToolCallRate), p5095Cell(wl.DurMSP50, wl.DurMSP95))
	}
	w("\n")
	// by hour: mermaid only - request volume + input tokens (no dur chart, no table)
	if len(rep.HoursOfDay) > 0 {
		vol := make([]int64, 24)
		tokIn := make([]int64, 24)
		for _, h := range rep.HoursOfDay {
			if h.Hour >= 0 && h.Hour < 24 {
				vol[h.Hour] = int64(h.Requests)
				tokIn[h.Hour] = h.TokensIn
			}
		}
		w("**每小时活跃度**\n\n%s%s",
			mermaidHourBar("请求量 / 小时", "请求", vol),
			mermaidTokenHourBar("输入Token / 小时", tokIn))
	}
	// by date: mermaid only - request volume + input tokens
	if len(rep.ByDate) > 0 {
		labels := make([]string, len(rep.ByDate))
		vol := make([]int64, len(rep.ByDate))
		tokIn := make([]int64, len(rep.ByDate))
		for i, d := range rep.ByDate {
			labels[i] = shortDate(d.Date)
			vol[i] = int64(d.Requests)
			tokIn[i] = d.TokensIn
		}
		w("**按日期活跃度**\n\n%s%s",
			mermaidBarLabeled("请求量 / 天", "请求", labels, vol),
			mermaidTokenBarLabeled("输入Token / 天", labels, tokIn))
	}
	// by client (8)
	if len(rep.ByClient) > 0 {
		w("**按客户端** ⭐\n\n")
		clientTbl := newTable(w, "client_key", "请求", "成功率", "fresh/cached/out(reasoning)", "缓存效率", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)")
		for _, c := range rep.ByClient {
			clientTbl.row(c.ClientKey, strconv.Itoa(c.Requests), pctStr2(c.OK, c.Requests),
				fmt.Sprintf("%s / %s / %s (%s)", fmtTokens(c.TokensInFresh), fmtTokens(c.TokensInCached), fmtTokens(c.TokensOut), fmtTokens(c.TokensReasoning)),
				cacheEffCell(c.CacheEfficiency, c.TokensKnown, c.Requests),
				p5095Cell(c.DurMSP50, c.DurMSP95),
				tokP5095Cell(c.InTokP50, c.InTokP95),
				tokP5095Cell(c.OutTokP50, c.OutTokP95))
		}
		w("\n")
	}
	// by endpoint (8), format mirrors 按客户端 - cross-day merged like §3/§4
	if len(rep.EndpointsAll) > 0 {
		w("**按端点** ⭐（跨日合并）\n\n")
		epTbl := newTable(w, "端点", "请求", "成功率", "fresh/cached/out(reasoning)", "缓存效率", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)")
		byRequests := append([]EndpointRow(nil), rep.EndpointsAll...)
		sort.SliceStable(byRequests, func(i, j int) bool { return byRequests[i].Requests > byRequests[j].Requests })
		for _, e := range byRequests {
			epTbl.row(e.Endpoint, strconv.Itoa(e.Requests), pctStr2(e.RequestsOK, e.Requests),
				fmt.Sprintf("%s / %s / %s (%s)", fmtTokens(e.TokensInFresh), fmtTokens(e.TokensInCached), fmtTokens(e.TokensOut), fmtTokens(e.TokensReasoning)),
				cacheEffCell(e.CacheEfficiency, e.TokensKnown, e.Requests),
				p5095Cell(e.DurMSP50, e.DurMSP95),
				tokP5095Cell(e.InTokP50, e.InTokP95),
				tokP5095Cell(e.OutTokP50, e.OutTokP95))
		}
		w("\n")
	}
}

// ---- §6 会话与任务 ----
// Only interactive-class sessions are listed here (scheduled single-shots -
// heartbeat/dream_diary/… - live in vmr-requests.md's own 定时任务 rollups,
// see requests.go); grouped by client (Chat User), matching vmr-requests.md's
// grouping, so a "类" column would be redundant (every row is interactive).
func renderSessions(w func(string, ...any), rep *Report2) {
	w("## §6 会话与任务\n\n")
	var interactive []SessionRow
	for _, s := range rep.Sessions {
		if s.Class == "interactive" {
			interactive = append(interactive, s)
		}
	}
	if len(interactive) == 0 {
		w("（无 interactive 会话）\n\n")
		renderCompactionChains(w, rep)
		return
	}

	byClient := map[string][]SessionRow{}
	var seenOrder []string
	for _, s := range interactive {
		key := s.ClientKey
		if key == "" {
			key = "(unresolved)"
		}
		if _, ok := byClient[key]; !ok {
			seenOrder = append(seenOrder, key)
		}
		byClient[key] = append(byClient[key], s)
	}
	// rep.ByClient order (by request volume) first, then any extra key with
	// no ByClient entry - "(unresolved)" never carries a client_key_tag.
	clientOrder := make([]string, 0, len(rep.ByClient)+1)
	for _, c := range rep.ByClient {
		clientOrder = append(clientOrder, c.ClientKey)
	}
	for _, k := range seenOrder {
		found := false
		for _, o := range clientOrder {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			clientOrder = append(clientOrder, k)
		}
	}

	for _, ck := range clientOrder {
		rows := byClient[ck]
		if len(rows) == 0 {
			continue
		}
		w("**%s**\n\n", ck)
		tbl := newTable(w, "会话", "标题", "轮", "任务", "fresh/cached/out", "结果")
		for _, s := range rows {
			renderSessionRow(tbl, s)
		}
		w("\n")
	}
	// compaction chains: mermaid for chains ≥3 nodes
	renderCompactionChains(w, rep)
}

func renderSessionRow(tbl *mdTable, s SessionRow) {
	outcome := "ok"
	if s.Errors > 0 {
		outcome = fmt.Sprintf("ok (%d error)", s.Errors)
	}
	if s.Fallbacks > 0 {
		outcome += fmt.Sprintf(" · %d fallback", s.Fallbacks)
	}
	tbl.row(s.ID, truncateTitle(s.Title, 28), strconv.Itoa(s.Requests), strconv.Itoa(s.Tasks),
		fmt.Sprintf("%s / %s / %s", fmtTokens(s.TokensInFresh), fmtTokens(s.TokensInCached), fmtTokens(s.TokensOut)),
		outcome)
}

// truncateTitle shortens s to at most maxRunes runes, appending an ellipsis
// when cut. Rune-based, unlike a byte slice - a truncated CJK title never
// splits a multi-byte UTF-8 sequence into mojibake (e.g. a trailing "�").
func truncateTitle(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// renderCompactionChains builds head->current chains from SessionRow.ContinuedFrom
// and renders a mermaid flowchart for any chain with ≥3 nodes (≥2 compaction
// hops). Shorter chains are noted inline as text. (V2 A3 / M5)
func renderCompactionChains(w func(string, ...any), rep *Report2) {
	byID := map[string]*SessionRow{}
	for i := range rep.Sessions {
		byID[rep.Sessions[i].ID] = &rep.Sessions[i]
	}
	// child -> parent (ContinuedFrom). A session is a "tip" if nobody continues from it.
	pointedTo := map[string]bool{}
	for _, s := range rep.Sessions {
		if s.ContinuedFrom != "" {
			pointedTo[s.ContinuedFrom] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range rep.Sessions {
		if pointedTo[s.ID] {
			continue // not a tip
		}
		// walk back to head via ContinuedFrom links (string-only, no pointer)
		chain := []string{s.ID}
		parent := s.ContinuedFrom
		for parent != "" && byID[parent] != nil && !seen[parent] {
			chain = append(chain, parent)
			seen[parent] = true
			parent = byID[parent].ContinuedFrom
		}
		// reverse: head -> tip
		for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
			chain[i], chain[j] = chain[j], chain[i]
		}
		if len(chain) >= 3 {
			w("```mermaid\nflowchart LR\n")
			for i := 0; i < len(chain)-1; i++ {
				w("    %s[\"%s\"] -->|compacted| %s[\"%s\"]\n", chain[i], chain[i], chain[i+1], chain[i+1])
			}
			w("```\n\n")
		} else if len(chain) == 2 {
			// text arrow, inline note
			w("> %s ← %s（单次 compaction）\n\n", chain[1], chain[0])
		}
	}
}

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

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// shortDate renders a Row.Date ("2026-07-14") as "MM-dd" ("07-14") for the
// §5 按日期活跃度 x-axis, where the year is implied by the report's own date
// range and would otherwise crowd 11+ daily labels.
func shortDate(s string) string {
	if len(s) == 10 {
		return s[5:]
	}
	return s
}

func fmtDurMS(v int64) string {
	if v <= 0 {
		return "-"
	}
	if v < 1000 {
		return strconv.FormatInt(v, 10) + "ms"
	}
	return strconv.FormatFloat(float64(v)/1000, 'f', 1, 64) + "s"
}

func pctFloat(f float64) string {
	return strconv.FormatFloat(f*100, 'f', 1, 64) + "%"
}

func pctStr2(num, den int) string {
	if den <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(num)/float64(den)*100, 'f', 1, 64) + "%"
}

// cacheEffCell renders a cache_efficiency value with a low-confidence footnote
// marker when its basis (TokensKnown) is < 90% of total requests.
func cacheEffCell(eff float64, basis, total int) string {
	s := pctStr(eff)
	if total > 0 && basis > 0 && float64(basis)/float64(total) < 0.90 {
		s += "¹"
	}
	return s
}

// ppCell renders "p50 / p95" or "p50 / p95 / max" with (n=…) and ⚠️low-n.
func ppCell(p50, p95, max int64, n int) string {
	if n == 0 {
		return "- (n=0)"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	if max > 0 {
		return fmt.Sprintf("%s / %s / %s (n=%d%s)", fmtDurMS(p50), fmtDurMS(p95), fmtDurMS(max), n, flag)
	}
	return fmt.Sprintf("%s / %s (n=%d%s)", fmtDurMS(p50), fmtDurMS(p95), n, flag)
}

func durCell(p95 int64, n int) string {
	if n == 0 {
		return "-"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	return fmt.Sprintf("%s (n=%d%s)", fmtDurMS(p95), n, flag)
}

func tokPerSec(f float64) string {
	if f <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(f), 'f', 1, 64)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orDash2(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func sumRoleChars(m map[string]int64) int64 {
	var t int64
	for _, v := range m {
		t += v
	}
	return t
}

func sortedRoles(m map[string]int64) []string {
	// order: tool, assistant, developer, system, user, then others
	order := []string{"tool", "assistant", "developer", "system", "user"}
	out := []string{}
	seen := map[string]bool{}
	for _, r := range order {
		if _, ok := m[r]; ok {
			out = append(out, r)
			seen[r] = true
		}
	}
	for r := range m {
		if !seen[r] {
			out = append(out, r)
		}
	}
	return out
}

// endpointProtocol extracts the leading "protocol:" segment from an
// EndpointRow.Endpoint label ("protocol:provider:model").
func endpointProtocol(endpoint string) string {
	if i := strings.IndexByte(endpoint, ':'); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}

// protocolBuckets splits endpoint rows by protocol, preserving each row's
// relative order within its bucket. "openai" sorts first, "anthropic"
// second, any other protocol follows alphabetically — the fixed group order
// every §3/§4 by-protocol table renders in.
func protocolBuckets(eps []EndpointRow) ([]string, map[string][]EndpointRow) {
	byProto := map[string][]EndpointRow{}
	var order []string
	for _, e := range eps {
		p := endpointProtocol(e.Endpoint)
		if _, ok := byProto[p]; !ok {
			order = append(order, p)
		}
		byProto[p] = append(byProto[p], e)
	}
	rank := func(p string) int {
		switch p {
		case "openai":
			return 0
		case "anthropic":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		ri, rj := rank(order[i]), rank(order[j])
		if ri != rj {
			return ri < rj
		}
		return order[i] < order[j]
	})
	return order, byProto
}

// pctHundred formats a value already on a 0-100 percentage scale (e.g.
// EndpointRow.ErrorRate) as "x.x%" — unlike pctStr/pctFloat, which expect a
// 0-1 fraction and would multiply by 100 again.
func pctHundred(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64) + "%"
}

// p5095Cell renders a "p50/p95" duration pair for the §5 workload tables.
func p5095Cell(p50, p95 int64) string {
	return fmtDurMS(p50) + "/" + fmtDurMS(p95)
}

// tokP5095Cell renders a "p50/p95" token-count pair (§5 按客户端 In/Out
// columns) - same shape as p5095Cell but token-scaled, not duration-scaled.
func tokP5095Cell(p50, p95 int64) string {
	return fmtTokens(p50) + "/" + fmtTokens(p95)
}

func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func topErrorClassShort(e EndpointRow) string {
	if len(e.ErrorClasses) == 0 {
		return "-"
	}
	cls, n := topErrorClassCount(e.ErrorClasses)
	return cls + " ×" + strconv.Itoa(n)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---- mermaid charts ----

// hourLabels returns the fixed 24-hour x-axis category list ("00".."23"),
// shared by every hourly chart.
func hourLabels() []string {
	labels := make([]string, 24)
	for i := 0; i < 24; i++ {
		labels[i] = fmt.Sprintf("%02d", i)
	}
	return labels
}

// mermaidHourBar renders 24 hourly integer buckets (requests, error counts)
// as a mermaid xychart-beta bar chart against the fixed 24-hour axis.
func mermaidHourBar(title, yLabel string, vals []int64) string {
	return mermaidBarLabeled(title, yLabel, hourLabels(), vals)
}

// mermaidBarLabeled renders an arbitrary-length integer series (hourly,
// daily, …) as a mermaid xychart-beta bar chart with the given x-axis
// category labels.
func mermaidBarLabeled(title, yLabel string, labels []string, vals []int64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return mermaidChart(title, yLabel, labels, parts)
}

// mermaidTokenHourBar is mermaidHourBar's token-count counterpart: raw
// token counts run into the tens/hundreds of millions, unreadable as bare
// integers against a chart axis, so values are scaled to millions (2
// decimals) and the axis is labeled accordingly.
func mermaidTokenHourBar(title string, vals []int64) string {
	return mermaidTokenBarLabeled(title, hourLabels(), vals)
}

// mermaidTokenBarLabeled is mermaidBarLabeled's token-count counterpart —
// see mermaidTokenHourBar.
func mermaidTokenBarLabeled(title string, labels []string, vals []int64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.FormatFloat(float64(v)/1e6, 'f', 2, 64)
	}
	return mermaidChart(title, "Token (M)", labels, parts)
}

// mermaidChart renders the shared xychart-beta scaffold; parts are already-
// formatted y-values (plain integers for counts, "M"-scaled decimals for
// token charts) — mermaid's bar/line data is a bare numeric list, so no
// thousands-separator can go in here without breaking the chart's own
// comma-delimited syntax.
func mermaidChart(title, yLabel string, labels []string, parts []string) string {
	qlabels := make([]string, len(labels))
	for i, l := range labels {
		qlabels[i] = fmt.Sprintf("%q", l)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "```mermaid\nxychart-beta\n    title %q\n    x-axis [%s]\n    y-axis %q\n    bar [%s]\n```\n\n",
		title, strings.Join(qlabels, ", "), yLabel, strings.Join(parts, ", "))
	return b.String()
}
