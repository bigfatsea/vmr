// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_latency.go (§4 Latency & Throughput).
package i18n

// LatencyText is section_latency.go's text, in one language.
type LatencyText struct {
	Title           string
	Headers         func(slowSec int) [6]string // model, protocol, ttft p50/p95(n), dur p50/p95/max(n), slow>Ns, tok/s
	EndpointHeaders func(slowSec int) [5]string // endpoint, ttft p50/p95(n), dur p50/p95/max(n), slow>Ns, tok/s
	SummaryNote     func(p95, max string) string
	StreamNote      string
	ByEndpointTitle string
}

func Latency(lang Lang) LatencyText {
	if lang == ZH {
		return LatencyText{
			Title: "§4 延迟与吞吐",
			Headers: func(slowSec int) [6]string {
				return [6]string{"模型", "协议", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "slow>" + itoa64(int64(slowSec)) + "s⭐", "tok/s"}
			},
			EndpointHeaders: func(slowSec int) [5]string {
				return [5]string{"端点", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "slow>" + itoa64(int64(slowSec)) + "s⭐", "tok/s"}
			},
			SummaryNote: func(p95, max string) string {
				return "\n> 全局 p95 dur " + p95 + "，max " + max + "。按 tok/s 降序排列。\n"
			},
			StreamNote:      "> 若 coding 的慢主要来自长流式输出，而非首字延迟，参见每模型的 ttft vs dur 差值。\n\n",
			ByEndpointTitle: "**按端点**（跨日合并）",
		}
	}
	return LatencyText{
		Title: "§4 Latency & Throughput",
		Headers: func(slowSec int) [6]string {
			return [6]string{"Model", "Protocol", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "slow>" + itoa64(int64(slowSec)) + "s⭐", "tok/s"}
		},
		EndpointHeaders: func(slowSec int) [5]string {
			return [5]string{"Endpoint", "ttft p50/p95 (n)", "dur p50/p95/max (n)", "slow>" + itoa64(int64(slowSec)) + "s⭐", "tok/s"}
		},
		SummaryNote: func(p95, max string) string {
			return "\n> Global p95 dur " + p95 + ", max " + max + ". Sorted by tok/s descending.\n"
		},
		StreamNote:      "> If coding's slowness mostly comes from a long stream rather than time-to-first-token, check the ttft vs dur gap per model.\n\n",
		ByEndpointTitle: "**By Endpoint** (merged across dates)",
	}
}
