// Ver 2026-09-15, by Opus 5

// §4 延迟 view model: TTFT / total duration / stream duration
// percentiles, each carrying the n it was computed from. Pairs with
// internal/i18n/report_latency.go.
package report

import (
	"sort"
	"strconv"

	"vmr/internal/i18n"
)

func vmLatencySection(rep *Report2, o Row, lang i18n.Lang) SectionVM {
	t := i18n.Latency(lang)
	sec := SectionVM{ID: "latency", Title: t.Title}
	h := t.Headers(SlowThresholdMS / 1000)
	tbl := &TableVM{Headers: h[:]}
	byModelSpeed := append([]Row(nil), rep.ByModel...)
	sort.SliceStable(byModelSpeed, func(i, j int) bool { return byModelSpeed[i].TokOutPerSec > byModelSpeed[j].TokOutPerSec })
	for _, m := range byModelSpeed {
		tbl.row(m.Model, m.Protocol,
			ppCell(m.TTFTMSP50, m.TTFTMSP95, 0, m.TTFTKnown),
			ppCell(m.DurMSP50, m.DurMSP95, m.DurMSMax, m.RequestsWithDur),
			strconv.Itoa(m.SlowRequests),
			tokPerSec(m.TokOutPerSec))
	}
	sec.Blocks = append(sec.Blocks, tbl)
	tbl.Notes = vmNotes(t.SummaryNote(fmtDurMS(o.DurMSP95), fmtDurMS(o.DurMSMax)) + t.StreamNote)

	// by endpoint, split by protocol (跨日合并, same basis as §3 端点健康),
	// each group sorted by tok/s descending
	if len(rep.EndpointsAll) > 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ByEndpointTitle + "\n\n"})
		protocols, byProto := vmProtocolBuckets(rep.EndpointsAll)
		eh := t.EndpointHeaders(SlowThresholdMS / 1000)
		for _, p := range protocols {
			rows := append([]EndpointRow(nil), byProto[p]...)
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].TokOutPerSec > rows[j].TokOutPerSec })
			sec.Blocks = append(sec.Blocks, ParaVM{Text: "*" + p + "*\n\n"})
			var mainRows, lowNRows []EndpointRow
			for _, e := range rows {
				if e.Requests >= 20 || e.Attempts >= 20 {
					mainRows = append(mainRows, e)
				} else {
					lowNRows = append(lowNRows, e)
				}
			}
			if len(mainRows) > 0 {
				epTbl := &TableVM{Headers: eh[:]}
				for _, e := range mainRows {
					vmLatencyEndpointRow(epTbl, e)
				}
				sec.Blocks = append(sec.Blocks, epTbl)
			}
			if len(lowNRows) > 0 {
				epTbl := &TableVM{Headers: eh[:]}
				for _, e := range lowNRows {
					vmLatencyEndpointRow(epTbl, e)
				}
				if len(mainRows) > 0 {
					epTbl.Fold = t.LowSampleOpen(len(lowNRows))
				}
				sec.Blocks = append(sec.Blocks, epTbl)
			}
		}
	}
	return sec
}

func vmLatencyEndpointRow(tbl *TableVM, e EndpointRow) {
	tbl.row(e.Endpoint,
		ppCell(e.TTFTMSP50, e.TTFTMSP95, 0, e.TTFTKnown),
		ppCell(e.DurMSP50, e.DurMSP95, e.DurMSMax, e.RequestsWithDur),
		strconv.Itoa(e.SlowRequests),
		tokPerSec(e.TokOutPerSec))
}
