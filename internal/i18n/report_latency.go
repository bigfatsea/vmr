// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_latency.go (§4 Latency & Throughput).
package i18n

import "fmt"

// LatencyText is viewmodel_latency.go's text, in one language.
type LatencyText struct {
	Title           string
	Headers         func(slowSec int) [6]string // model, protocol, ttft p50/p95(n), dur p50/p95/max(n), slow>Ns, tok/s
	EndpointHeaders func(slowSec int) [5]string // endpoint, ttft p50/p95(n), dur p50/p95/max(n), slow>Ns, tok/s
	SummaryNote     func(p95, max string) string
	StreamNote      string
	ByEndpointTitle string
	LowSampleOpen   func(n int) string
}

// latencyRow holds report_latency.go's literal templates, one row per Lang
// (Table's own doc comment). The 6/5-header arrays hold every column
// except the "slow>Ns" one, which is built separately since it's the only
// column carrying an interpolated value.
type latencyRow struct {
	title            string
	headersStatic    [5]string // model, protocol, ttft, dur, tok/s (slow column inserted at index 4)
	endpointHeaders  [4]string // endpoint, ttft, dur, tok/s (slow column inserted at index 3)
	slowHeaderFmt    string    // "slow>%ds⭐"
	summaryNoteFmt   string
	streamNote       string
	byEndpointTitle  string
	lowSampleOpenFmt string
}

var latencyRows = Table[latencyRow]{
	EN: {
		title:            "§4 Latency & Throughput",
		headersStatic:    [5]string{"Model", "Protocol", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "tok/s"},
		endpointHeaders:  [4]string{"Endpoint", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "tok/s"},
		slowHeaderFmt:    "slow>%ds⭐",
		summaryNoteFmt:   "\n> Global p95 dur %s, max %s. Sorted by tok/s descending.\n",
		streamNote:       "> If coding's slowness mostly comes from a long stream rather than time-to-first-token, check the ttft vs dur gap per model.\n\n",
		byEndpointTitle:  "**By Endpoint** (merged across dates)",
		lowSampleOpenFmt: "<details><summary>+ %d more low-sample endpoints (n < 20)</summary>\n\n",
	},
	ZH: {
		title:            "§4 延迟与吞吐",
		headersStatic:    [5]string{"模型", "协议", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "tok/s"},
		endpointHeaders:  [4]string{"端点", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "tok/s"},
		slowHeaderFmt:    "slow>%ds⭐",
		summaryNoteFmt:   "\n> 全局 p95 dur %s，max %s。按 tok/s 降序排列。\n",
		streamNote:       "> 若 coding 的慢主要来自长流式输出，而非首字延迟，参见每模型的 ttft vs dur 差值。\n\n",
		byEndpointTitle:  "**按端点**（跨日合并）",
		lowSampleOpenFmt: "<details><summary>+ 另有 %d 个低样本端点（样本 < 20）</summary>\n\n",
	},
}

func Latency(lang Lang) LatencyText {
	r := latencyRows.Row(lang)
	return LatencyText{
		Title: r.title,
		Headers: func(slowSec int) [6]string {
			return [6]string{r.headersStatic[0], r.headersStatic[1], r.headersStatic[2], r.headersStatic[3], fmt.Sprintf(r.slowHeaderFmt, slowSec), r.headersStatic[4]}
		},
		EndpointHeaders: func(slowSec int) [5]string {
			return [5]string{r.endpointHeaders[0], r.endpointHeaders[1], r.endpointHeaders[2], fmt.Sprintf(r.slowHeaderFmt, slowSec), r.endpointHeaders[3]}
		},
		SummaryNote:     func(p95, max string) string { return fmt.Sprintf(r.summaryNoteFmt, p95, max) },
		StreamNote:      r.streamNote,
		ByEndpointTitle: r.byEndpointTitle,
		LowSampleOpen:   func(n int) string { return fmt.Sprintf(r.lowSampleOpenFmt, n) },
	}
}
