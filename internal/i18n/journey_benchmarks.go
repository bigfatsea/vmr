// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/journey/render_benchmarks.go (journeys/benchmarks.md).
package i18n

import "fmt"

// BenchmarksText is render_benchmarks.go's text, in one language.
type BenchmarksText struct {
	Title              string
	JourneyCount       func(n int) string
	NoJourneys         string
	MetricDistTitle    string
	MetricDistHeader   string
	MetricDistFootnote string

	FindingRateTitle  string
	FindingRateHeader string
	NoFindings        string
	// AnthropicOnlyCoverageNote is the detector-coverage disclosure line:
	// printed only when this corpus has ~0% Anthropic Messages
	// traffic, naming the signals that read as absent/zero for structural
	// reasons, not because nothing was found. anthropicPct is a
	// pre-formatted percentage; codes is a comma-joined list.
	AnthropicOnlyCoverageNote func(anthropicPct, codes string) string

	CorrelationTitle    string
	CorrelationHeader   string
	NoCorrelations      string
	CorrelationMore     func(n int) string
	CorrelationFootnote string

	GroupCompTitle          string
	GroupCompHeader         string
	NoGroupComparisons      string
	SkippedGroupComparisons func(codes string) string
	GroupCompFootnote       string

	ContextRotTitle    string
	ContextRotHeader   string
	ContextRotFootnote string
	// ContextRotExcludedNote appears under the Context Rot table when some
	// steps were excluded from all buckets because their in-token usage is
	// unknown (UsageInOK=false — S-2). These would otherwise pile into the
	// smallest "0-32k" bucket and pollute its error rate.
	ContextRotExcludedNote func(n int) string
	// UnrecognizedShapeNote discloses the chatmsg counter snapshot when
	// chatmsg hit an unrecognized content part type or usage holder shape
	// during this run (S-2 "let the silence speak"); both 0 = line omitted
	// by the call site.
	UnrecognizedShapeNote func(parts, holders int) string

	ToolSeqTitle    string
	ToolSeqHeader   string
	NoToolSeq       string
	ToolSeqFootnote string
}

// benchmarksRow holds journey_benchmarks.go's literal templates, one row
// per Lang (Table's own doc comment).
type benchmarksRow struct {
	title              string
	journeyCountFmt    string
	noJourneys         string
	metricDistTitle    string
	metricDistHeader   string
	metricDistFootnote string

	findingRateTitle         string
	findingRateHeader        string
	noFindings               string
	anthropicOnlyCoverageFmt string // %s = pre-formatted pct, %s = codes

	correlationTitle    string
	correlationHeader   string
	noCorrelations      string
	correlationMoreFmt  string
	correlationFootnote string

	groupCompTitle             string
	groupCompHeader            string
	noGroupComparisons         string
	skippedGroupComparisonsFmt string
	groupCompFootnote          string

	contextRotTitle       string
	contextRotHeader      string
	contextRotFootnote    string
	contextRotExcludedFmt string
	unrecognizedShapeFmt  string // %s parts, %s holders

	toolSeqTitle    string
	toolSeqHeader   string
	noToolSeq       string
	toolSeqFootnote string
}

var benchmarksRows = Table[benchmarksRow]{
	EN: {
		title:              "# Journey Benchmarks\n\n",
		journeyCountFmt:    "> Analyzed %d journeys\n\n",
		noJourneys:         "No journeys to analyze.\n",
		metricDistTitle:    "## Metric Distributions\n\n",
		metricDistHeader:   "| Metric | N | Mean | Median | Min | Max | P90 |\n|---|---|---|---|---|---|---|\n",
		metricDistFootnote: "> Mean on the time-based metrics is very sensitive to a few long-lived journeys — a single journey spanning days of wall-clock (idle gaps accumulating within one lineage) can pull it well above the typical case, even above P90. Read Median and P90 for the common case; treat Mean only as a total-load signal.\n\n",

		findingRateTitle:         "## Finding Hit Rates\n\n",
		findingRateHeader:        "| Code | Hit Rate (≥1 occurrence) |\n|---|---|\n",
		noFindings:               "No rule Findings were detected in this corpus.\n\n",
		anthropicOnlyCoverageFmt: "> ⚠️ Only %s of this corpus is Anthropic Messages traffic. The following signals depend on a field only ever populated for Anthropic Messages tool results (`chatmsg.ToolResult.IsError`) and structurally cannot fire on non-Anthropic Messages requests — a 0%% hit rate or all-zero metric here means \"couldn't be checked\", not \"checked, no issue found\": %s\n\n",

		correlationTitle:    "## Metric Correlations (|rho| ≥ 0.3, Spearman rank, top 15 by |rho|)\n\n",
		correlationHeader:   "| Metric A | Metric B | rho | N |\n|---|---|---|---|\n",
		noCorrelations:      "No correlation cleared the threshold — this is itself an honest result, not proof the metrics are unrelated, just that no sufficiently strong rank correlation was observed at this sample size.\n\n",
		correlationMoreFmt:  "> %d more correlation(s) cleared the threshold but aren't listed here (several are mechanical relationships between time-based metrics, e.g. \"Net Working Time = Model Time + Agent-Side Execution Time\") — the full list is in journeys/benchmarks.json's correlations field.\n\n",
		correlationFootnote: "> Effect size (rho) only — no p-values/significance claims: this corpus size can't support a rigorous significance test, and reporting one would manufacture false confidence. Correlation is not causation.\n\n",

		groupCompTitle:             "## Finding-Grouped Comparison (Net Working Time)\n\n",
		groupCompHeader:            "| Code | Hit N | No-Hit N | Hit Median | No-Hit Median | Relative Change |\n|---|---|---|---|---|---|\n",
		noGroupComparisons:         "No Finding had enough samples (≥3 on both sides) for a group comparison.\n\n",
		skippedGroupComparisonsFmt: "> The following Findings were skipped (fewer than 3 journeys on the hit or no-hit side — not evidence of no difference, just not enough data): %s\n\n",
		groupCompFootnote:          "> ⚠️ = relative change ≥ 30% and the absolute difference clears the noise floor — a rule-based \"worth a look\" flag, not a determined \"this Finding caused longer duration\" conclusion; VMR has no task-success label, so this compares duration as a proxy, not outcome.\n\n",

		contextRotTitle:       "## Context Window Scaling & Quality Inflection (Context Rot)\n\n",
		contextRotHeader:      "| Context Range | Step N | Finding N | Finding Density | Error Step N | Error Rate |\n|---|---|---|---|---|---|\n",
		contextRotFootnote:    "> Bucket statistics by step input tokens. Increases in Finding density or error rate at larger context windows indicate potential context rot trends; a flat distribution indicates no such trend was observed in this batch. Error rate is computed from protocol-level error markers.\n\n",
		contextRotExcludedFmt: "> %d step(s) excluded: no in-token usage data",
		unrecognizedShapeFmt:  "> %d unrecognized content part(s), %d unrecognized usage holder(s)",

		toolSeqTitle:    "## Frequent Tool Call Sequences (N-gram)\n\n",
		toolSeqHeader:   "| Tool Sequence | Occurrences | Tail Step Error Rate |\n|---|---|---|\n",
		noToolSeq:       "No tool call sequences met the frequency threshold.\n\n",
		toolSeqFootnote: "> Continuous 2-gram and 3-gram tool sequences within task boundaries, showing dominant behavioral patterns and associated error rates.\n\n",
	},
	ZH: {
		title:              "# Journey 基准统计报告\n\n",
		journeyCountFmt:    "> 分析了 %d 个 Journey\n\n",
		noJourneys:         "没有可分析的 Journey。\n",
		metricDistTitle:    "## 指标分布\n\n",
		metricDistHeader:   "| 指标 | 样本数 | 均值 | 中位数 | 最小值 | 最大值 | P90 |\n|---|---|---|---|---|---|---|\n",
		metricDistFootnote: "> 时间类指标的均值对少数超长 Journey 极其敏感——单个跨多日的 Journey（同一 lineage 内空闲间隔的累积）就能把均值抬到远高于典型值，甚至高于 P90。判断常见情况请优先看中位数与 P90，均值仅作总体负载的参考。\n\n",

		findingRateTitle:         "## Finding 命中率\n\n",
		findingRateHeader:        "| Code | 命中率（至少一次） |\n|---|---|\n",
		noFindings:               "本批语料未检测到任何规则 Finding。\n\n",
		anthropicOnlyCoverageFmt: "> ⚠️ 本批语料仅 %s 为 Anthropic Messages 协议请求。以下信号依赖仅 Anthropic Messages 协议才会填充的字段（`chatmsg.ToolResult.IsError`），在非 Anthropic Messages 请求上结构性无法触发——命中率为 0 或指标全为 0 代表\"测不出来\"，不代表\"检查过没问题\"：%s\n\n",

		correlationTitle:    "## 指标相关性（|rho| ≥ 0.3，Spearman 秩相关，按 |rho| 降序取前 15）\n\n",
		correlationHeader:   "| 指标 A | 指标 B | rho | 样本数 |\n|---|---|---|---|\n",
		noCorrelations:      "未发现达到阈值的相关性——这本身也是一个诚实的结果，不代表指标之间一定没有关系，只是在本批样本规模下没有观测到足够强的秩相关。\n\n",
		correlationMoreFmt:  "> 另有 %d 组达到阈值但未在表中列出的相关性（含不少是同一时间类指标之间的机械关联，如\"净工作时长 = 模型时间 + Agent 侧执行时间\"）——完整列表见 journeys/benchmarks.json 的 correlations 字段。\n\n",
		correlationFootnote: "> 仅报告效应量（rho），不报告 p 值/显著性——当前语料规模不足以支撑严格的显著性检验，报告 p 值只会制造虚假的确定性。相关性不代表因果关系。\n\n",

		groupCompTitle:             "## Finding 分组对比（净工作时长）\n\n",
		groupCompHeader:            "| Code | 命中数 | 未命中数 | 命中组中位数 | 未命中组中位数 | 相对变化 |\n|---|---|---|---|---|---|\n",
		noGroupComparisons:         "没有样本量足够（双侧均 ≥ 3）的 Finding 分组可比较。\n\n",
		skippedGroupComparisonsFmt: "> 以下 Finding 因命中组或未命中组样本数不足 3 个而跳过分组对比（不是没有差异，是数据不够）：%s\n\n",
		groupCompFootnote:          "> ⚠️ = 相对变化 ≥ 30% 且绝对差值超过噪声阈值——一个规则性的\"值得看一眼\"标记，不是\"这个 Finding 导致了更长的耗时\"的确定性结论；VMR 没有任务是否成功的标签，这里比较的是耗时这一个代理指标，不是效果。\n\n",

		contextRotTitle:       "## Context 增长与质量拐点（Context Rot）\n\n",
		contextRotHeader:      "| 上下文区间 | Step 样本数 | Finding 总数 | Finding 密度 | 错误 Step 数 | 错误率 |\n|---|---|---|---|---|---|\n",
		contextRotFootnote:    "> 按照 Step 输入 Token 大小分桶统计。若高上下文区间出现 Finding 密度突增或错误率上升，可作为注意力衰减（Context Rot）趋势的参考；若本批语料各区间分布平稳则未观测到该趋势。错误率基于协议原生错误标记统计。\n\n",
		contextRotExcludedFmt: "> %d 个 Step 被排除：缺少入 Token 用量数据",
		unrecognizedShapeFmt:  "> %d 处未识别的内容块、 %d 处未识别的 usage 载体",

		toolSeqTitle:    "## 高频工具调用序列模式（N-gram）\n\n",
		toolSeqHeader:   "| 工具调用序列 | 出现频次 | 尾步错误率 |\n|---|---|---|\n",
		noToolSeq:       "未提取到满足频次阈值的工具调用序列。\n\n",
		toolSeqFootnote: "> 基于任务内连续 2-gram 与 3-gram 工具调用统计，展示最高频出现的行为定势与异常关联。\n\n",
	},
}

func Benchmarks(lang Lang) BenchmarksText {
	r := benchmarksRows.Row(lang)
	return BenchmarksText{
		Title:              r.title,
		JourneyCount:       func(n int) string { return fmt.Sprintf(r.journeyCountFmt, n) },
		NoJourneys:         r.noJourneys,
		MetricDistTitle:    r.metricDistTitle,
		MetricDistHeader:   r.metricDistHeader,
		MetricDistFootnote: r.metricDistFootnote,

		FindingRateTitle:  r.findingRateTitle,
		FindingRateHeader: r.findingRateHeader,
		NoFindings:        r.noFindings,
		AnthropicOnlyCoverageNote: func(anthropicPct, codes string) string {
			return fmt.Sprintf(r.anthropicOnlyCoverageFmt, anthropicPct, codes)
		},

		CorrelationTitle:    r.correlationTitle,
		CorrelationHeader:   r.correlationHeader,
		NoCorrelations:      r.noCorrelations,
		CorrelationMore:     func(n int) string { return fmt.Sprintf(r.correlationMoreFmt, n) },
		CorrelationFootnote: r.correlationFootnote,

		GroupCompTitle:     r.groupCompTitle,
		GroupCompHeader:    r.groupCompHeader,
		NoGroupComparisons: r.noGroupComparisons,
		SkippedGroupComparisons: func(codes string) string {
			return fmt.Sprintf(r.skippedGroupComparisonsFmt, codes)
		},
		GroupCompFootnote: r.groupCompFootnote,

		ContextRotTitle:        r.contextRotTitle,
		ContextRotHeader:       r.contextRotHeader,
		ContextRotFootnote:     r.contextRotFootnote,
		ContextRotExcludedNote: func(n int) string { return fmt.Sprintf(r.contextRotExcludedFmt, n) },
		UnrecognizedShapeNote: func(parts, holders int) string {
			return fmt.Sprintf(r.unrecognizedShapeFmt, parts, holders)
		},

		ToolSeqTitle:    r.toolSeqTitle,
		ToolSeqHeader:   r.toolSeqHeader,
		NoToolSeq:       r.noToolSeq,
		ToolSeqFootnote: r.toolSeqFootnote,
	}
}
