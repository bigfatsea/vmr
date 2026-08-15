// Ver 2026-08-01, by Sonnet 5

// §5 负载画像: request shape by workload class, hourly series, and the
// per-client / per-endpoint breakdowns.
package report

import (
	"fmt"
	"sort"
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// ---- §5 负载分布 ----
func renderWorkload(w func(string, ...any), rep *Report2, o Row, lang i18n.Lang) {
	t := i18n.Workload(lang)
	w("## %s\n\n", t.Title)
	// by virtual model (6)
	w("%s\n\n", t.ByModelTitle)
	mh := t.ByModelHeaders
	modelTbl := newTable(w, mh[0], mh[1], mh[2], mh[3], mh[4], mh[5])
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests), pctStr2(m.OK, m.Requests),
			fmt.Sprintf("%s / %s / %s", fmtutil.FmtTokens(m.TokensInFresh), fmtutil.FmtTokens(m.TokensInCached), fmtutil.FmtTokens(m.TokensOut)),
			p5095Cell(m.DurMSP50, m.DurMSP95))
	}
	w("\n")
	// by workload class (6)
	w("%s\n\n", t.ByWorkloadTitle)
	wh := t.ByWorkloadHeaders
	wlTbl := newTable(w, wh[0], wh[1], wh[2], wh[3], wh[4], wh[5])
	for _, wl := range rep.Workloads {
		flag := ""
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			flag = " ⚠️"
		}
		wlTbl.row(wl.Class, strconv.Itoa(wl.Requests), fmtutil.FmtTokens(wl.TokensInFresh),
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
		reqTitle, reqAxis := t.HourlyReqChart()
		w("%s\n\n%s%s",
			t.HourlyTitle,
			mermaidHourBar(reqTitle, reqAxis, vol),
			mermaidTokenHourBar(t.HourlyTokChart, tokIn))
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
		dayTitle, dayAxis := t.DailyReqChart()
		w("%s\n\n%s%s",
			t.DailyTitle,
			mermaidBarLabeled(dayTitle, dayAxis, labels, vol),
			mermaidTokenBarLabeled(t.DailyTokChart, labels, tokIn))
	}
	// by client (8)
	if len(rep.ByClient) > 0 {
		w("%s\n\n", t.ByClientTitle)
		ch := t.ByClientHeaders
		clientTbl := newTable(w, ch[0], ch[1], ch[2], ch[3], ch[4], ch[5], ch[6], ch[7])
		for _, c := range rep.ByClient {
			clientTbl.row(c.ClientKey, strconv.Itoa(c.Requests), pctStr2(c.OK, c.Requests),
				fmt.Sprintf("%s / %s / %s (%s)", fmtutil.FmtTokens(c.TokensInFresh), fmtutil.FmtTokens(c.TokensInCached), fmtutil.FmtTokens(c.TokensOut), fmtutil.FmtTokens(c.TokensReasoning)),
				cacheEffCell(c.CacheEfficiency, c.TokensKnown, c.Requests),
				p5095Cell(c.DurMSP50, c.DurMSP95),
				tokP5095Cell(c.InTokP50, c.InTokP95),
				tokP5095Cell(c.OutTokP50, c.OutTokP95))
		}
		w("\n")
	}
	// by endpoint (8), format mirrors 按客户端 - cross-day merged like §3/§4
	if len(rep.EndpointsAll) > 0 {
		w("%s\n\n", t.ByEndpointTitle)
		eh := t.ByEndpointHeaders
		epTbl := newTable(w, eh[0], eh[1], eh[2], eh[3], eh[4], eh[5], eh[6], eh[7])
		byRequests := append([]EndpointRow(nil), rep.EndpointsAll...)
		sort.SliceStable(byRequests, func(i, j int) bool { return byRequests[i].Requests > byRequests[j].Requests })
		for _, e := range byRequests {
			epTbl.row(e.Endpoint, strconv.Itoa(e.Requests), pctStr2(e.RequestsOK, e.Requests),
				fmt.Sprintf("%s / %s / %s (%s)", fmtutil.FmtTokens(e.TokensInFresh), fmtutil.FmtTokens(e.TokensInCached), fmtutil.FmtTokens(e.TokensOut), fmtutil.FmtTokens(e.TokensReasoning)),
				cacheEffCell(e.CacheEfficiency, e.TokensKnown, e.Requests),
				p5095Cell(e.DurMSP50, e.DurMSP95),
				tokP5095Cell(e.InTokP50, e.InTokP95),
				tokP5095Cell(e.OutTokP50, e.OutTokP95))
		}
		w("\n")
	}
}
