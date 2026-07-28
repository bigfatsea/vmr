// Ver 2026-07-28 19:20, by Opus 5

// §3 可靠性: outcome mix, per-endpoint health (attempt-level availability
// vs request-level success rate — deliberately distinct), and the error
// class distribution, bucketed by protocol.
package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

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

func topErrorClassShort(e EndpointRow) string {
	if len(e.ErrorClasses) == 0 {
		return "-"
	}
	cls, n := topErrorClassCount(e.ErrorClasses)
	return cls + " ×" + strconv.Itoa(n)
}
