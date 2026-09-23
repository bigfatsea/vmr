// Ver 2026-09-22 02:35, by Sonnet 5

// Pairs with internal/report/viewmodel_doc.go: the document title, the meta
// line, the summary + auto highlights, the request-index link line, and the appendix.
package i18n

import "fmt"

// DocText is viewmodel_doc.go's text, in one language.
type DocText struct {
	Title    string
	MetaLine func(inputs string, format, records, parseErrors int, from, to string) string
	// MetaInputSummary / MetaInputListLabel keep the header's data-source
	// line to one line: MetaLine gets "44 files" as its `inputs`, and the
	// full path list folds into a <details> under that label. On a full
	// corpus the raw list was ~1.7k chars twice (here and the appendix).
	MetaInputSummary   func(n int) string
	MetaInputListLabel string
	// MetaReportConfig names the report.yaml this run applied, or says
	// plainly that none was loaded — the "no file" case is the one worth
	// printing: a report.yaml the run never found looks exactly like a run
	// that was never meant to have one.
	MetaReportConfig func(path string) string
	DetailLinkLine   string
	// JourneyIndexLinkLine is the "vmr-report.md → journeys/index.md" edge
	// — path is relative to vmr-report.md itself.
	JourneyIndexLinkLine func(path string, journeyCount int, from, to string) string
	SummaryTitle         string
	SummaryRequests      func(requests, fallbacks, truncated int) string
	SummaryHeaders       [6]string // requests, success rate, billed input(fresh), cache efficiency, p95 duration, pay-as-you-go equivalent cost
	// SummaryCostUnknown fills the cost cell when nothing priced at all —
	// never "0", which reads as "this was free".
	SummaryCostUnknown string
	// SummaryInteractiveNote is a line below the summary table noting
	// what fraction of total requests belong to the "interactive" workload
	// class (as opposed to heartbeat/dream_diary/compaction). The total
	// requests figure includes everything; this note gives the reader a
	// quick sense of how much of the traffic is user-facing.
	SummaryInteractiveNote func(total, interactive int, pct string) string
	SummaryStarNote        string
	HighlightsAuto         string
	NoAnomalies            string
	CacheWarn              func(workload, cacheEffPct, freshTokens string) string
	ToolWarn               func(shape string, requests int, schemaBytes, wasteBytes, utilPct string, neverCalled int) string
	EndpointWarn           func(endpoint, errRatePct, topSuffix string) string
	TopErrorSuffix         func(cls string, n int) string
	RequestIndexTitle      string
	RequestIndexBody       string
	DetailsCaptureBody     string
	// DetailsOnDemandBody is DetailsCaptureBody's counterpart for the
	// default (-details=false) run, where details/*.md was never
	// materialized — example is a real "basename:line" coordinate
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
	// AppendixSelfTrafficExcluded is shown whenever exclusion was
	// configured and applied — n is how many records it skipped, and n == 0
	// ("configured, nothing in this window matched") still takes this line,
	// not AppendixSelfTrafficNotExcluded.
	AppendixSelfTrafficExcluded    func(n int) string
	AppendixSelfTrafficNotExcluded string
}

// docRow holds report_doc.go's literal templates, one row per Lang (Table's
// own doc comment). Two conditionals: MetaReportConfig ("path == """) and
// DetailsOnDemandBody ("example != """) — both written once in Doc below.
// toolWarnFmt uses explicit %[n] argument indices because the Chinese
// sentence places schemaBytes/requests in the opposite order from English —
// same six arguments, different word order, so the template picks the
// order instead of the call site.
type docRow struct {
	title                          string
	metaLineFmt                    string
	metaInputSummaryFmt            string
	metaInputListLabel             string
	metaReportConfigNone           string
	metaReportConfigFmt            string
	detailLinkLine                 string
	journeyIndexLinkFmt            string
	summaryTitle                   string
	summaryRequestsFmt             string
	summaryHeaders                 [6]string
	summaryCostUnknown             string
	summaryInteractiveFmt          string
	summaryStarNote                string
	highlightsAuto                 string
	noAnomalies                    string
	cacheWarnFmt                   string
	toolWarnFmt                    string
	endpointWarnFmt                string
	topErrorSuffixFmt              string
	requestIndexTitle              string
	requestIndexBody               string
	detailsCaptureBody             string
	detailsOnDemandBase            string
	detailsOnDemandExampleFmt      string
	detailsOnDemandSuffix          string
	appendixTitle                  string
	appendixInputLineFmt           string
	appendixPeriodLineFmt          string
	appendixPercentileFmt          string
	appendixNBase                  string
	appendixLowConf                string
	appendixStarMark               string
	appendixBillingLineFmt         string
	appendixNoPricing              string
	appendixSlowThreshFmt          string
	appendixSelfTrafficExcludedFmt string
	appendixSelfTrafficNotExcluded string
}

var docRows = Table[docRow]{
	EN: {
		title:                          "VMR Usage Report",
		metaLineFmt:                    "Data source: %s · format %d · %d records (%d bad rows) · %s – %s",
		metaInputSummaryFmt:            "%d files",
		metaInputListLabel:             "File list",
		metaReportConfigNone:           "Config: no report.yaml loaded (flag/built-in defaults only; self-traffic exclusion off)",
		metaReportConfigFmt:            "Config: %s",
		detailLinkLine:                 "Request-level data is in `requests/index.json`; browse it interactively with `request-browser.html`",
		journeyIndexLinkFmt:            "Task narratives in [%s](%s) (%d task(s) indexed · covers %s – %s)\n\n",
		summaryTitle:                   "§0 Summary",
		summaryRequestsFmt:             "%d (fallback %d / trunc %d)",
		summaryHeaders:                 [6]string{"Requests", "Success Rate", "Billed Input (fresh)⭐", "Cache Efficiency⭐", "p95 Duration", "PAYG-Equivalent Cost⭐"},
		summaryCostUnknown:             "unpriced",
		summaryInteractiveFmt:          "of which the interactive workload accounts for %s (%d/%d)\n",
		summaryStarNote:                "> ⭐ = derived/estimated metric (not direct upstream value), see Appendix for basis.\n\n",
		highlightsAuto:                 "**Highlights (auto):**",
		noAnomalies:                    "(No notable anomalies: cache efficiency, tool utilization, and endpoint error rates are all in the normal range)",
		cacheWarnFmt:                   "⚠️ **%s workload cache efficiency %s** - %s fresh tokens (dominates this workload's input)",
		toolWarnFmt:                    "⚠️ **Tool declaration %[1]s wastes %[4]s** - %[3]s schema sent across %[2]d requests, utilization %[5]s (%[6]d never called)",
		endpointWarnFmt:                "⚠️ **Endpoint %s error rate %s/100** (worst)%s",
		topErrorSuffixFmt:              ", top cause %s ×%d",
		requestIndexTitle:              "§8 Request Detail Index",
		requestIndexBody:               "Machine-readable per-request detail is in `requests/index.json` (with the session/task title projection and journey cross-links); filter by client/model/endpoint/duration/tokens, sort, and locate one request with `request-browser.html`.\n",
		detailsCaptureBody:             "Full single-request capture (req/resp/SSE) is in `requests/details/*.md`.\n\n",
		detailsOnDemandBase:            "This run did not write `requests/details/*.md` (generated on demand by default). Fetch a single record any time by its coordinate (the `req` field of `requests/index.json`, `basename:line`): `vmr replay -print -req <coord>`",
		detailsOnDemandExampleFmt:      ", e.g. `vmr replay -print -req %s`",
		detailsOnDemandSuffix:          "; or pass `-details` to materialize all of them.\n\n",
		appendixTitle:                  "Appendix: Data Source & Methodology",
		appendixInputLineFmt:           "- Input: %s · format %d · %d records / %d bad rows\n",
		appendixPeriodLineFmt:          "- Period: %s – %s (local timezone)\n",
		appendixPercentileFmt:          "- Percentile method: %s\n",
		appendixNBase:                  "- n basis: each percentile is annotated with n (= ttft_known / requests_with_dur / stream_known); n<20 is marked ⚠️low-n.\n",
		appendixLowConf:                "- Ratio low confidence: cache_efficiency and similar ratio metrics get a ¹ footnote when their denominator / total requests < 90%.\n",
		appendixStarMark:               "- ⭐ marker: this column is a derived/estimated metric (not a value returned directly by the upstream) — read it together with its sample size and basis note.\n",
		appendixBillingLineFmt:         "- Billing basis: fresh + cache_write(premium) + out; cache hits are billed free/near-free by most providers. %s\n",
		appendixNoPricing:              "No $ figures shown when pricing isn't configured.",
		appendixSlowThreshFmt:          "- Slow-request threshold: %ds\n",
		appendixSelfTrafficExcludedFmt: "- Self-traffic: exclusion active; %d analysis request(s) from `vmr analyze -llm-addr` itself removed from every total (disable with `-include-self-traffic`).\n",
		appendixSelfTrafficNotExcluded: "- Self-traffic: exclusion not active (no `llm_key` or `self_traffic_client_tags` configured).\n",
	},
	ZH: {
		title:                          "VMR 用量报告",
		metaLineFmt:                    "数据源: %s · format %d · %d 条记录（%d 坏行）· %s – %s",
		metaInputSummaryFmt:            "%d 个文件",
		metaInputListLabel:             "文件清单",
		metaReportConfigNone:           "配置: 未加载 report.yaml（全部取命令行/内置默认值，自流量排除未启用）",
		metaReportConfigFmt:            "配置: %s",
		detailLinkLine:                 "请求明细数据见 `requests/index.json`；交互式浏览用 `request-browser.html`",
		journeyIndexLinkFmt:            "任务叙事见 [%s](%s)（%d 个任务索引 · 覆盖 %s – %s）\n\n",
		summaryTitle:                   "§0 摘要",
		summaryRequestsFmt:             "%d（fallback %d / trunc %d）",
		summaryHeaders:                 [6]string{"请求", "成功率", "计费输入(fresh)⭐", "缓存效率⭐", "p95 耗时", "按量计费等价成本⭐"},
		summaryCostUnknown:             "未定价",
		summaryInteractiveFmt:          "其中 interactive 工作负载占 %s（%d/%d）\n",
		summaryStarNote:                "> ⭐ = 衍生/预估指标（非上游直接返回值），完整口径见附录。\n\n",
		highlightsAuto:                 "**亮点 (auto):**",
		noAnomalies:                    "（无明显异常：缓存效率、工具利用率、端点错误率均在正常区间）",
		cacheWarnFmt:                   "⚠️ **%s 工作负载缓存效率 %s** - %s fresh tokens（占该负载输入大头）",
		toolWarnFmt:                    "⚠️ **工具声明 %[1]s 浪费 %[4]s** - 跨 %[2]d 请求发送 %[3]s schema，利用率 %[5]s（%[6]d 个从未调用）",
		endpointWarnFmt:                "⚠️ **端点 %s 错误率 %s/100**（最差）%s",
		topErrorSuffixFmt:              "，主因 %s ×%d",
		requestIndexTitle:              "§8 请求详单",
		requestIndexBody:               "每条请求的机读明细在 `requests/index.json`（含会话/任务标题投影与 journey 交叉链接）；按客户端/模型/端点/耗时/token 筛选、排序、定位单条请求用 `request-browser.html`。\n",
		detailsCaptureBody:             "单请求全量捕获（req/resp/SSE）见 `requests/details/*.md`。\n\n",
		detailsOnDemandBase:            "本次运行未生成 `requests/details/*.md`（默认按需生成）。用坐标（`requests/index.json` 的 `req` 字段，形如 `basename:line`）随时取出单条记录：`vmr replay -print -req <坐标>`",
		detailsOnDemandExampleFmt:      "，例如 `vmr replay -print -req %s`",
		detailsOnDemandSuffix:          "；或加 `-details` 全量生成。\n\n",
		appendixTitle:                  "附录 数据源与方法论",
		appendixInputLineFmt:           "- 输入: %s · format %d · %d 记录 / %d 坏行\n",
		appendixPeriodLineFmt:          "- 时段: %s – %s (本地时区)\n",
		appendixPercentileFmt:          "- 百分位: %s\n",
		appendixNBase:                  "- n 基准: 每个百分位标注 n（= ttft_known / requests_with_dur / stream_known）；n<20 标 ⚠️low-n。\n",
		appendixLowConf:                "- 比值低置信度: cache_efficiency 等比值指标的分母 / 总请求数 < 90% 时标注脚注 ¹。\n",
		appendixStarMark:               "- ⭐ 标记: 该列为衍生/预估指标（非上游直接返回值），解读时请结合样本量与口径说明。\n",
		appendixBillingLineFmt:         "- 计费口径: fresh + cache_write(溢价) + out；缓存命中按各厂免费/极低价。%s\n",
		appendixNoPricing:              "未配置定价时不显示 $。",
		appendixSlowThreshFmt:          "- 慢请求阈值: %ds\n",
		appendixSelfTrafficExcludedFmt: "- 自指流量: 排除已启用，本次从全部统计中排除 %d 条 `vmr analyze -llm-addr` 自身产生的分析请求（`-include-self-traffic` 可关闭）。\n",
		appendixSelfTrafficNotExcluded: "- 自指流量: 未启用排除（未配置排除标识 `llm_key` 或 `self_traffic_client_tags`）。\n",
	},
}

// Doc returns viewmodel_doc.go's text for lang.
func Doc(lang Lang) DocText {
	r := docRows.Row(lang)
	return DocText{
		Title: r.title,
		MetaLine: func(inputs string, format, records, parseErrors int, from, to string) string {
			return fmt.Sprintf(r.metaLineFmt, inputs, format, records, parseErrors, from, to)
		},
		MetaInputSummary:   func(n int) string { return fmt.Sprintf(r.metaInputSummaryFmt, n) },
		MetaInputListLabel: r.metaInputListLabel,
		MetaReportConfig: func(path string) string {
			if path == "" {
				return r.metaReportConfigNone
			}
			return fmt.Sprintf(r.metaReportConfigFmt, path)
		},
		DetailLinkLine: r.detailLinkLine,
		JourneyIndexLinkLine: func(path string, journeyCount int, from, to string) string {
			return fmt.Sprintf(r.journeyIndexLinkFmt, path, path, journeyCount, from, to)
		},
		SummaryTitle: r.summaryTitle,
		SummaryRequests: func(requests, fallbacks, truncated int) string {
			return fmt.Sprintf(r.summaryRequestsFmt, requests, fallbacks, truncated)
		},
		SummaryHeaders:     r.summaryHeaders,
		SummaryCostUnknown: r.summaryCostUnknown,
		SummaryInteractiveNote: func(total, interactive int, pct string) string {
			return fmt.Sprintf(r.summaryInteractiveFmt, pct, interactive, total)
		},
		SummaryStarNote: r.summaryStarNote,
		HighlightsAuto:  r.highlightsAuto,
		NoAnomalies:     r.noAnomalies,
		CacheWarn: func(workload, cacheEffPct, freshTokens string) string {
			return fmt.Sprintf(r.cacheWarnFmt, workload, cacheEffPct, freshTokens)
		},
		ToolWarn: func(shape string, requests int, schemaBytes, wasteBytes, utilPct string, neverCalled int) string {
			return fmt.Sprintf(r.toolWarnFmt, shape, requests, schemaBytes, wasteBytes, utilPct, neverCalled)
		},
		EndpointWarn: func(endpoint, errRatePct, topSuffix string) string {
			return fmt.Sprintf(r.endpointWarnFmt, endpoint, errRatePct, topSuffix)
		},
		TopErrorSuffix:     func(cls string, n int) string { return fmt.Sprintf(r.topErrorSuffixFmt, cls, n) },
		RequestIndexTitle:  r.requestIndexTitle,
		RequestIndexBody:   r.requestIndexBody,
		DetailsCaptureBody: r.detailsCaptureBody,
		DetailsOnDemandBody: func(example string) string {
			s := r.detailsOnDemandBase
			if example != "" {
				s += fmt.Sprintf(r.detailsOnDemandExampleFmt, example)
			}
			return s + r.detailsOnDemandSuffix
		},
		AppendixTitle: r.appendixTitle,
		AppendixInputLine: func(inputs string, format, records, parseErrors int) string {
			return fmt.Sprintf(r.appendixInputLineFmt, inputs, format, records, parseErrors)
		},
		AppendixPeriodLine:  func(from, to string) string { return fmt.Sprintf(r.appendixPeriodLineFmt, from, to) },
		AppendixPercentile:  func(method string) string { return fmt.Sprintf(r.appendixPercentileFmt, method) },
		AppendixNBase:       r.appendixNBase,
		AppendixLowConf:     r.appendixLowConf,
		AppendixStarMark:    r.appendixStarMark,
		AppendixBillingLine: func(suffix string) string { return fmt.Sprintf(r.appendixBillingLineFmt, suffix) },
		AppendixNoPricing:   r.appendixNoPricing,
		AppendixSlowThresh:  func(sec int) string { return fmt.Sprintf(r.appendixSlowThreshFmt, sec) },
		AppendixSelfTrafficExcluded: func(n int) string {
			return fmt.Sprintf(r.appendixSelfTrafficExcludedFmt, n)
		},
		AppendixSelfTrafficNotExcluded: r.appendixSelfTrafficNotExcluded,
	}
}
