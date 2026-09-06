// Ver 2026-09-15, by Opus 5

// §5 负载分布 view model: request shape by workload class, hourly and
// daily series (mermaid charts, with the daily table folded), and the
// per-client / per-endpoint breakdowns. Pairs with
// internal/i18n/report_workload.go.
package report

import (
	"fmt"
	"sort"
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func vmWorkloadSection(rep *Report2, _ Row, lang i18n.Lang) SectionVM {
	t := i18n.Workload(lang)
	sec := SectionVM{ID: "workload", Title: t.Title}

	// by virtual model (6)
	modelTbl := &TableVM{Title: t.ByModelTitle, Headers: t.ByModelHeaders[:]}
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests), pctStr2(m.OK, m.Requests),
			fmt.Sprintf("%s / %s / %s", fmtutil.FmtTokens(m.TokensInFresh), fmtutil.FmtTokens(m.TokensInCached), fmtutil.FmtTokens(m.TokensOut)),
			p5095Cell(m.DurMSP50, m.DurMSP95))
	}
	sec.Blocks = append(sec.Blocks, modelTbl)

	// by workload class (6)
	wlTbl := &TableVM{Title: t.ByWorkloadTitle, Headers: t.ByWorkloadHeaders[:]}
	for _, wl := range rep.Workloads {
		flag := ""
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			flag = " ⚠️"
		}
		wlTbl.row(wl.Class, strconv.Itoa(wl.Requests), fmtutil.FmtTokens(wl.TokensInFresh),
			cacheEffCell(wl.CacheEfficiency, wl.TokensKnown, wl.Requests)+flag,
			pctStr(wl.ToolCallRate), p5095Cell(wl.DurMSP50, wl.DurMSP95))
	}
	sec.Blocks = append(sec.Blocks, wlTbl)

	// by hour: mermaid only - request volume + input tokens (no dur chart,
	// no table)
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
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.HourlyTitle + "\n\n" +
			mermaidHourBar(reqTitle, reqAxis, vol) +
			mermaidTokenHourBar(t.HourlyTokChart, tokIn)})
	}
	// by date: mermaid + folded table fallback
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
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.DailyTitle + "\n\n" +
			mermaidBarLabeled(dayTitle, dayAxis, labels, vol) +
			mermaidTokenBarLabeled(t.DailyTokChart, labels, tokIn)})
		dtbl := &TableVM{Fold: fmt.Sprintf(t.DailyTableOpen, len(rep.ByDate)), Headers: t.DailyTableHeaders[:]}
		for _, d := range rep.ByDate {
			dtbl.row(d.Date, strconv.Itoa(d.Requests), pctStr2(d.OK, d.Requests),
				fmt.Sprintf("%s / %s / %s", fmtutil.FmtTokens(d.TokensInFresh), fmtutil.FmtTokens(d.TokensInCached), fmtutil.FmtTokens(d.TokensOut)))
		}
		sec.Blocks = append(sec.Blocks, dtbl)
	}
	// by client (8)
	if len(rep.ByClient) > 0 {
		clientTbl := &TableVM{Title: t.ByClientTitle, Headers: t.ByClientHeaders[:]}
		for _, c := range rep.ByClient {
			clientTbl.row(c.ClientKey, strconv.Itoa(c.Requests), pctStr2(c.OK, c.Requests),
				fmt.Sprintf("%s / %s / %s (%s)", fmtutil.FmtTokens(c.TokensInFresh), fmtutil.FmtTokens(c.TokensInCached), fmtutil.FmtTokens(c.TokensOut), fmtutil.FmtTokens(c.TokensReasoning)),
				cacheEffCell(c.CacheEfficiency, c.TokensKnown, c.Requests),
				p5095Cell(c.DurMSP50, c.DurMSP95),
				tokP5095Cell(c.InTokP50, c.InTokP95),
				tokP5095Cell(c.OutTokP50, c.OutTokP95))
		}
		sec.Blocks = append(sec.Blocks, clientTbl)
	}
	// by endpoint (8), format mirrors 按客户端 - cross-day merged like §3/§4
	if len(rep.EndpointsAll) > 0 {
		epTbl := &TableVM{Title: t.ByEndpointTitle, Headers: t.ByEndpointHeaders[:]}
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
		sec.Blocks = append(sec.Blocks, epTbl)
	}
	return sec
}
