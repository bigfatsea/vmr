// Ver 2026-07-28 19:20, by Opus 5

// §5 负载画像: request shape by workload class, hourly series, and the
// per-client / per-endpoint breakdowns.
package report

import (
	"fmt"
	"sort"
	"strconv"
)

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
