// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/render_doc.go: the document title, the meta
// line, §0 summary + auto highlights, §8's link line, and the appendix.
package i18n

import "strconv"

// DocText is render_doc.go's text, in one language.
type DocText struct {
	Title    string
	MetaLine func(inputs string, format, records, parseErrors int, from, to string) string
	// MetaInputSummary / MetaInputListLabel keep the header's data-source
	// line to one line: MetaLine gets "44 files" as its `inputs`, and the
	// full path list folds into a <details> under that label. On a full
	// corpus the raw list was ~1.7k chars twice (here and the appendix).
	MetaInputSummary   func(n int) string
	MetaInputListLabel string
	DetailLinkLine     string
	// StoriesLinkLine is the "vmr-report.md → stories/vmr-stories.md"
	// edge (P6.2a) — path is relative to vmr-report.md itself.
	StoriesLinkLine func(path string, journeyCount int, from, to string) string
	SummaryTitle    string
	SummaryRequests func(requests, fallbacks, truncated int) string
	SummaryHeaders  [6]string // requests, success rate, billed input(fresh), cache efficiency, p95 duration, pay-as-you-go equivalent cost
	// SummaryCostUnknown fills the cost cell when nothing priced at all —
	// never "0", which reads as "this was free".
	SummaryCostUnknown string
	SummaryStarNote    string
	HighlightsAuto     string
	NoAnomalies        string
	CacheWarn          func(workload, cacheEffPct, freshTokens string) string
	ToolWarn           func(shape string, requests int, schemaBytes, wasteBytes, utilPct string, neverCalled int) string
	EndpointWarn       func(endpoint, errRatePct, topSuffix string) string
	TopErrorSuffix     func(cls string, n int) string
	RequestIndexTitle  string
	RequestIndexBody   string
	PerClientLabel     string
	DetailsCaptureBody string
	// DetailsOnDemandBody is DetailsCaptureBody's counterpart for the
	// default (-details=false) run, where details/*.md was never
	// materialized (P6.2b) — example is a real "basename:line" coordinate
	// from this run's own data, "" when this run had no requests at all.
	DetailsOnDemandBody func(example string) string
	AppendixTitle       string
	AppendixInputLine   func(inputs string, format, records, parseErrors int) string
	AppendixPeriodLine  func(from, to string) string
	AppendixPercentile  func(method string) string
	AppendixNBase       string
	AppendixLowConf     string
	AppendixStarMark    string
	AppendixBillingLine func(suffix string) string
	AppendixNoPricing   string
	AppendixSlowThresh  func(sec int) string
	// AppendixSelfTrafficExcluded (P6.4) is shown whenever exclusion was
	// configured and applied — n is how many records it skipped, and n == 0
	// ("configured, nothing in this window matched") still takes this line,
	// not AppendixSelfTrafficNotExcluded.
	AppendixSelfTrafficExcluded    func(n int) string
	AppendixSelfTrafficNotExcluded string
	AppendixClientReconciliation   func(clients string) string
}

// Doc returns render_doc.go's text for lang.
func Doc(lang Lang) DocText {
	if lang == ZH {
		return DocText{
			Title: "VMR 用量报告",
			MetaLine: func(inputs string, format, records, parseErrors int, from, to string) string {
				return "数据源: " + inputs + " · format " + strconv.Itoa(format) + " · " + strconv.Itoa(records) +
					" 条记录（" + strconv.Itoa(parseErrors) + " 坏行）· " + from + " – " + to
			},
			MetaInputSummary:   func(n int) string { return strconv.Itoa(n) + " 个文件" },
			MetaInputListLabel: "文件清单",
			DetailLinkLine:     "详单见 [vmr-requests.md](./vmr-requests.md) · 同名 .json",
			StoriesLinkLine: func(path string, journeyCount int, from, to string) string {
				return "任务叙事见 [" + path + "](" + path + ")（" + strconv.Itoa(journeyCount) + " 个任务索引 · 覆盖 " + from + " – " + to + "）\n\n"
			},
			SummaryTitle: "§0 摘要",
			SummaryRequests: func(requests, fallbacks, truncated int) string {
				return strconv.Itoa(requests) + "（fallback " + strconv.Itoa(fallbacks) + " / trunc " + strconv.Itoa(truncated) + "）"
			},
			SummaryHeaders:     [6]string{"请求", "成功率", "计费输入(fresh)⭐", "缓存效率⭐", "p95 耗时", "按量计费等价成本⭐"},
			SummaryCostUnknown: "未定价",
			SummaryStarNote:    "> ⭐ = 衍生/预估指标（非上游直接返回值），完整口径见附录。\n\n",
			HighlightsAuto:     "**亮点 (auto):**",
			NoAnomalies:        "（无明显异常：缓存效率、工具利用率、端点错误率均在正常区间）",
			CacheWarn: func(workload, cacheEffPct, freshTokens string) string {
				return "⚠️ **" + workload + " 工作负载缓存效率 " + cacheEffPct + "** - " + freshTokens + " fresh tokens（占该负载输入大头）"
			},
			ToolWarn: func(shape string, requests int, schemaBytes, wasteBytes, utilPct string, neverCalled int) string {
				return "⚠️ **工具声明 " + shape + " 浪费 " + wasteBytes + "** - 跨 " + strconv.Itoa(requests) + " 请求发送 " + schemaBytes +
					" schema，利用率 " + utilPct + "（" + strconv.Itoa(neverCalled) + " 个从未调用）"
			},
			EndpointWarn: func(endpoint, errRatePct, topSuffix string) string {
				return "⚠️ **端点 " + endpoint + " 错误率 " + errRatePct + "/100**（最差）" + topSuffix
			},
			TopErrorSuffix: func(cls string, n int) string {
				return "，主因 " + cls + " ×" + strconv.Itoa(n)
			},
			RequestIndexTitle:  "§8 请求详单",
			RequestIndexBody:   "每条记录（Chat User -> Session -> Task -> Turn）见 [vmr-requests.md](./vmr-requests.md)。\n",
			PerClientLabel:     "per-client: ",
			DetailsCaptureBody: "单请求全量捕获（req/resp/SSE）见 `details/*.md`。\n\n",
			DetailsOnDemandBody: func(example string) string {
				s := "本次运行未生成 `details/*.md`（默认按需生成）。用坐标（`vmr-requests.md` 的『文件』列，未生成详单时显示为该坐标，形如 `basename:line`）随时取出单条记录：`vmr replay -print -req <坐标>`"
				if example != "" {
					s += "，例如 `vmr replay -print -req " + example + "`"
				}
				return s + "；或加 `-details` 全量生成。\n\n"
			},
			AppendixTitle: "附录 数据源与方法论",
			AppendixInputLine: func(inputs string, format, records, parseErrors int) string {
				return "- 输入: " + inputs + " · format " + strconv.Itoa(format) + " · " + strconv.Itoa(records) +
					" 记录 / " + strconv.Itoa(parseErrors) + " 坏行\n"
			},
			AppendixPeriodLine: func(from, to string) string {
				return "- 时段: " + from + " – " + to + " (本地时区)\n"
			},
			AppendixPercentile: func(method string) string { return "- 百分位: " + method + "\n" },
			AppendixNBase:      "- n 基准: 每个百分位标注 n（= ttft_known / requests_with_dur / stream_known）；n<20 标 ⚠️low-n。\n",
			AppendixLowConf:    "- 比值低置信度: cache_efficiency 等比值指标的分母 / 总请求数 < 90% 时标注脚注 ¹。\n",
			AppendixStarMark:   "- ⭐ 标记: 该列为衍生/预估指标（非上游直接返回值），解读时请结合样本量与口径说明。\n",
			AppendixBillingLine: func(suffix string) string {
				return "- 计费口径: fresh + cache_write(溢价) + out；缓存命中按各厂免费/极低价。" + suffix + "\n"
			},
			AppendixNoPricing:  "未配置定价时不显示 $。",
			AppendixSlowThresh: func(sec int) string { return "- 慢请求阈值: " + strconv.Itoa(sec) + "s\n" },
			AppendixSelfTrafficExcluded: func(n int) string {
				return "- 自指流量: 排除已启用，本次从全部统计中排除 " + strconv.Itoa(n) + " 条 `vmr story -llm-addr` 自身产生的分析请求（`-include-self-traffic` 可关闭）。\n"
			},
			AppendixSelfTrafficNotExcluded: "- 自指流量: 未启用排除（未配置排除标识 `llm_key` 或 `self_traffic_client_tags`）。\n",
			AppendixClientReconciliation: func(clients string) string {
				return "- 客户端对账: 成本/负载表中存在但未独立生成 sibling 文件的客户端（仅含单发定时任务，已并入 cron 汇总）：" + clients + "。\n"
			},
		}
	}
	return DocText{
		Title: "VMR Usage Report",
		MetaLine: func(inputs string, format, records, parseErrors int, from, to string) string {
			return "Data source: " + inputs + " · format " + strconv.Itoa(format) + " · " + strconv.Itoa(records) +
				" records (" + strconv.Itoa(parseErrors) + " bad rows) · " + from + " – " + to
		},
		MetaInputSummary:   func(n int) string { return strconv.Itoa(n) + " files" },
		MetaInputListLabel: "File list",
		DetailLinkLine:     "Details in [vmr-requests.md](./vmr-requests.md) · matching .json",
		StoriesLinkLine: func(path string, journeyCount int, from, to string) string {
			return "Task narratives in [" + path + "](" + path + ") (" + strconv.Itoa(journeyCount) + " task(s) indexed · covers " + from + " – " + to + ")\n\n"
		},
		SummaryTitle: "§0 Summary",
		SummaryRequests: func(requests, fallbacks, truncated int) string {
			return strconv.Itoa(requests) + " (fallback " + strconv.Itoa(fallbacks) + " / trunc " + strconv.Itoa(truncated) + ")"
		},
		SummaryHeaders:     [6]string{"Requests", "Success Rate", "Billed Input (fresh)⭐", "Cache Efficiency⭐", "p95 Duration", "PAYG-Equivalent Cost⭐"},
		SummaryCostUnknown: "unpriced",
		SummaryStarNote:    "> ⭐ = derived/estimated metric (not direct upstream value), see Appendix for basis.\n\n",
		HighlightsAuto:     "**Highlights (auto):**",
		NoAnomalies:        "(No notable anomalies: cache efficiency, tool utilization, and endpoint error rates are all in the normal range)",
		CacheWarn: func(workload, cacheEffPct, freshTokens string) string {
			return "⚠️ **" + workload + " workload cache efficiency " + cacheEffPct + "** - " + freshTokens + " fresh tokens (dominates this workload's input)"
		},
		ToolWarn: func(shape string, requests int, schemaBytes, wasteBytes, utilPct string, neverCalled int) string {
			return "⚠️ **Tool declaration " + shape + " wastes " + wasteBytes + "** - " + schemaBytes + " schema sent across " + strconv.Itoa(requests) +
				" requests, utilization " + utilPct + " (" + strconv.Itoa(neverCalled) + " never called)"
		},
		EndpointWarn: func(endpoint, errRatePct, topSuffix string) string {
			return "⚠️ **Endpoint " + endpoint + " error rate " + errRatePct + "/100** (worst)" + topSuffix
		},
		TopErrorSuffix: func(cls string, n int) string {
			return ", top cause " + cls + " ×" + strconv.Itoa(n)
		},
		RequestIndexTitle:  "§8 Request Detail Index",
		RequestIndexBody:   "Every record (Chat User -> Session -> Task -> Turn) is in [vmr-requests.md](./vmr-requests.md).\n",
		PerClientLabel:     "per-client: ",
		DetailsCaptureBody: "Full single-request capture (req/resp/SSE) is in `details/*.md`.\n\n",
		DetailsOnDemandBody: func(example string) string {
			s := "This run did not write `details/*.md` (generated on demand by default). Fetch a single record any time by its coordinate (the \"File\" column of `vmr-requests.md` shows it as this coordinate when no details were generated, `basename:line`): `vmr replay -print -req <coord>`"
			if example != "" {
				s += ", e.g. `vmr replay -print -req " + example + "`"
			}
			return s + "; or pass `-details` to materialize all of them.\n\n"
		},
		AppendixTitle: "Appendix: Data Source & Methodology",
		AppendixInputLine: func(inputs string, format, records, parseErrors int) string {
			return "- Input: " + inputs + " · format " + strconv.Itoa(format) + " · " + strconv.Itoa(records) +
				" records / " + strconv.Itoa(parseErrors) + " bad rows\n"
		},
		AppendixPeriodLine: func(from, to string) string {
			return "- Period: " + from + " – " + to + " (local timezone)\n"
		},
		AppendixPercentile: func(method string) string { return "- Percentile method: " + method + "\n" },
		AppendixNBase:      "- n basis: each percentile is annotated with n (= ttft_known / requests_with_dur / stream_known); n<20 is marked ⚠️low-n.\n",
		AppendixLowConf:    "- Ratio low confidence: cache_efficiency and similar ratio metrics get a ¹ footnote when their denominator / total requests < 90%.\n",
		AppendixStarMark:   "- ⭐ marker: this column is a derived/estimated metric (not a value returned directly by the upstream) — read it together with its sample size and basis note.\n",
		AppendixBillingLine: func(suffix string) string {
			return "- Billing basis: fresh + cache_write(premium) + out; cache hits are billed free/near-free by most providers. " + suffix + "\n"
		},
		AppendixNoPricing:  "No $ figures shown when pricing isn't configured.",
		AppendixSlowThresh: func(sec int) string { return "- Slow-request threshold: " + strconv.Itoa(sec) + "s\n" },
		AppendixSelfTrafficExcluded: func(n int) string {
			return "- Self-traffic: exclusion active; " + strconv.Itoa(n) + " analysis request(s) from `vmr story -llm-addr` itself removed from every total (disable with `-include-self-traffic`).\n"
		},
		AppendixSelfTrafficNotExcluded: "- Self-traffic: exclusion not active (no `llm_key` or `self_traffic_client_tags` configured).\n",
		AppendixClientReconciliation: func(clients string) string {
			return "- Client reconciliation: clients present in cost/workload tables without a standalone sibling file (single-shot scheduled traffic only, rolled into cron files): " + clients + ".\n"
		},
	}
}
