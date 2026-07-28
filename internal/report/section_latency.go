// Ver 2026-07-28 19:20, by Opus 5

// §4 延迟: TTFT / total duration / stream duration percentiles, each
// carrying the n it was computed from.
package report

import (
	"fmt"
	"sort"
	"strconv"
)

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
