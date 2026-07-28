// Ver 2026-07-28 20:40, by Opus 5

// §6.6 端点性价比: not "what did this endpoint cost" (§2 already answers
// that) but "what did it cost per unit of work delivered, and what did its
// failures cost in time". A cheap endpoint that fails a third of the time
// is not cheap — but nothing in a per-endpoint spend column shows that,
// because the spend lands on whichever endpoint eventually succeeded.
//
// Every figure here is derived at render time from fields §2 and §3 already
// carry; the only thing this feature had to collect is EndpointRow.WastedMS.
// That is deliberate — a derived metric that disagrees with the totals it
// was derived from is a reporting bug waiting to happen.
package report

import (
	"fmt"
	"sort"
	"strconv"
)

// valueRow is the rendered form of one endpoint's efficiency, computed
// once so the table body stays a formatting exercise.
type valueRow struct {
	endpoint     string
	costPer1MOut float64
	costPerReq   float64
	hasCost      bool
	tokensOut    int64
	requestsOK   int
	failed       int
	availability float64 // 0..1 fraction, NOT 0..100 — EndpointRow.ErrorRate is the other convention
	wastedMS     int64
}

func renderEndpointValue(w func(string, ...any), rep *Report2) {
	rows := endpointValueRows(rep)
	if len(rows) == 0 {
		return
	}
	w("## §6.6 端点性价比 ⭐\n\n")

	cur := ""
	priced := rep.Pricing != nil
	if priced {
		cur = " (" + rep.Pricing.Currency + ")"
	}
	w("单位产出的代价，而不只是总花费——%s\n\n",
		orDash2(priced,
			"一个单价便宜但经常失败的端点，把请求推给下一家之后的真实代价可能更高。",
			"未配置定价，本节只显示时间维度；配置 `-pricing` 后会补上单位成本列。"))

	headers := []string{"端点", "成功请求", "out tokens"}
	if priced {
		headers = append(headers, "成本/1M out"+cur, "成本/成功请求"+cur)
	}
	headers = append(headers, "失败尝试", "可用率", "失败耗时⭐")
	tbl := newTable(w, headers...)

	for _, r := range rows {
		cells := []string{r.endpoint, strconv.Itoa(r.requestsOK), fmtTokens(r.tokensOut)}
		if priced {
			if r.hasCost {
				cells = append(cells, fmt.Sprintf("%.4f", r.costPer1MOut), fmt.Sprintf("%.4f", r.costPerReq))
			} else {
				cells = append(cells, "-", "-")
			}
		}
		cells = append(cells, strconv.Itoa(r.failed), pctStr(r.availability), fmtDurMS(r.wastedMS))
		tbl.row(cells...)
	}
	w("\n")
	w("> 失败耗时⭐ = 该端点**失败尝试**累计墙钟时间：请求最终由别处完成，这段时间是纯粹的延迟损耗。\n")
	w("> **只记时间、不折算成钱**：失败尝试拿不到 usage（vmr 只从客户端真正收到的那份响应里提取），厂商通常也不对失败请求计费——\n")
	w("> 给它标一个金额会是编造。这里的口径是「它让你多等了多久」，不是「它花了你多少钱」。\n")
	if priced {
		w("> 成本/1M out 用于横向比价（同样产出 100 万 token 谁更便宜）；成本/成功请求受各端点承接的请求形态影响，跨端点比较前先看 §5 的负载画像。\n")
	}
	w("\n")
}

// endpointValueRows builds the sorted body. Sort key: cheapest per unit of
// output first when pricing is available (that is the question the section
// exists to answer), else most wasted time first.
func endpointValueRows(rep *Report2) []valueRow {
	var out []valueRow
	for _, e := range rep.EndpointsAll {
		// An endpoint that never served a request has no unit of work to
		// divide by; its failures still show up in §3 端点健康.
		if e.RequestsOK == 0 && e.WastedMS == 0 {
			continue
		}
		r := valueRow{
			endpoint: e.Endpoint, tokensOut: e.TokensOut, requestsOK: e.RequestsOK,
			failed: e.Failed, availability: e.Availability, wastedMS: e.WastedMS,
		}
		if e.CostEstimate != nil && *e.CostEstimate > 0 {
			if e.TokensOut > 0 {
				r.costPer1MOut = *e.CostEstimate / float64(e.TokensOut) * 1e6
				r.hasCost = true
			}
			if e.RequestsOK > 0 {
				r.costPerReq = *e.CostEstimate / float64(e.RequestsOK)
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.hasCost != b.hasCost {
			return a.hasCost // priced rows first: they carry the comparison
		}
		if a.hasCost && a.costPer1MOut != b.costPer1MOut {
			return a.costPer1MOut < b.costPer1MOut
		}
		if a.wastedMS != b.wastedMS {
			return a.wastedMS > b.wastedMS
		}
		return a.endpoint < b.endpoint
	})
	return out
}
