// Ver 2026-08-01, by Sonnet 5

// §4 延迟: TTFT / total duration / stream duration percentiles, each
// carrying the n it was computed from.
package report

import (
	"sort"
	"strconv"

	"vmr/internal/i18n"
)

// ---- §4 延迟与吞吐 ----
func renderLatency(w func(string, ...any), rep *Report2, o Row, lang i18n.Lang) {
	t := i18n.Latency(lang)
	w("## %s\n\n", t.Title)
	h := t.Headers(SlowThresholdMS / 1000)
	tbl := newTable(w, h[0], h[1], h[2], h[3], h[4], h[5])
	byModelSpeed := append([]Row(nil), rep.ByModel...)
	sort.SliceStable(byModelSpeed, func(i, j int) bool { return byModelSpeed[i].TokOutPerSec > byModelSpeed[j].TokOutPerSec })
	for _, m := range byModelSpeed {
		tbl.row(m.Model, m.Protocol,
			ppCell(m.TTFTMSP50, m.TTFTMSP95, 0, m.TTFTKnown),
			ppCell(m.DurMSP50, m.DurMSP95, m.DurMSMax, m.RequestsWithDur),
			strconv.Itoa(m.SlowRequests),
			tokPerSec(m.TokOutPerSec))
	}
	w("%s", t.SummaryNote(fmtDurMS(o.DurMSP95), fmtDurMS(o.DurMSMax)))
	w("%s", t.StreamNote)

	// by endpoint, split by protocol (跨日合并, same basis as §3 端点健康), each
	// group sorted by tok/s descending
	if len(rep.EndpointsAll) > 0 {
		w("%s\n\n", t.ByEndpointTitle)
		protocols, byProto := protocolBuckets(rep.EndpointsAll)
		eh := t.EndpointHeaders(SlowThresholdMS / 1000)
		for _, p := range protocols {
			rows := append([]EndpointRow(nil), byProto[p]...)
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].TokOutPerSec > rows[j].TokOutPerSec })
			w("*%s*\n\n", p)
			epTbl := newTable(w, eh[0], eh[1], eh[2], eh[3], eh[4])
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
