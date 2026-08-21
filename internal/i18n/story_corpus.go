// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

// Pairs with internal/story/render_corpus.go (the corpus layer's vmr-story-corpus.md).
package i18n

import "strconv"

// CorpusText is render_corpus.go's text, in one language.
type CorpusText struct {
	Title            string
	JourneyCount     func(n int) string
	NoJourneys       string
	MetricDistTitle  string
	MetricDistHeader string

	FindingRateTitle  string
	FindingRateHeader string
	NoFindings        string
	// AnthropicOnlyCoverageNote is P14.2's disclosure line (KNOWN_ISSUES
	// §1.43): printed only when this corpus has ~0% Anthropic-protocol
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

	ToolSeqTitle    string
	ToolSeqHeader   string
	NoToolSeq       string
	ToolSeqFootnote string
}

func Corpus(lang Lang) CorpusText {
	if lang == ZH {
		return CorpusText{
			Title:        "# Journey 语料统计报告\n\n",
			JourneyCount: func(n int) string { return "> 分析了 " + strconv.Itoa(n) + " 个 Journey\n\n" },
			NoJourneys:   "没有可分析的 Journey。\n",

			MetricDistTitle:  "## 指标分布\n\n",
			MetricDistHeader: "| 指标 | 样本数 | 均值 | 中位数 | 最小值 | 最大值 | P90 |\n|---|---|---|---|---|---|---|\n",

			FindingRateTitle:  "## Finding 命中率\n\n",
			FindingRateHeader: "| Code | 命中率（至少一次） |\n|---|---|\n",
			NoFindings:        "本批语料未检测到任何规则 Finding。\n\n",
			AnthropicOnlyCoverageNote: func(anthropicPct, codes string) string {
				return "> ⚠️ 本批语料仅 " + anthropicPct + " 为 Anthropic 协议请求。以下信号依赖仅 Anthropic 协议才会填充的字段（`chatmsg.ToolResult.IsError`），在非 Anthropic 请求上结构性无法触发——命中率为 0 或指标全为 0 代表\"测不出来\"，不代表\"检查过没问题\"：" + codes + "\n\n"
			},

			CorrelationTitle:  "## 指标相关性（|rho| ≥ 0.3，Spearman 秩相关，按 |rho| 降序取前 15）\n\n",
			CorrelationHeader: "| 指标 A | 指标 B | rho | 样本数 |\n|---|---|---|---|\n",
			NoCorrelations:    "未发现达到阈值的相关性——这本身也是一个诚实的结果，不代表指标之间一定没有关系，只是在本批样本规模下没有观测到足够强的秩相关。\n\n",
			CorrelationMore: func(n int) string {
				return "> 另有 " + strconv.Itoa(n) + " 组达到阈值但未在表中列出的相关性（含不少是同一时间类指标之间的机械关联，如\"净工作时长 = 模型时间 + Agent 侧执行时间\"）——完整列表见 vmr-story-corpus.json 的 correlations 字段。\n\n"
			},
			CorrelationFootnote: "> 仅报告效应量（rho），不报告 p 值/显著性——当前语料规模不足以支撑严格的显著性检验，报告 p 值只会制造虚假的确定性。相关性不代表因果关系。\n\n",

			GroupCompTitle:     "## Finding 分组对比（净工作时长）\n\n",
			GroupCompHeader:    "| Code | 命中数 | 未命中数 | 命中组中位数 | 未命中组中位数 | 相对变化 |\n|---|---|---|---|---|---|\n",
			NoGroupComparisons: "没有样本量足够（双侧均 ≥ 3）的 Finding 分组可比较。\n\n",
			SkippedGroupComparisons: func(codes string) string {
				return "> 以下 Finding 因命中组或未命中组样本数不足 3 个而跳过分组对比（不是没有差异，是数据不够）：" + codes + "\n\n"
			},
			GroupCompFootnote: "> ⚠️ = 相对变化 ≥ 30% 且绝对差值超过噪声阈值——一个规则性的\"值得看一眼\"标记，不是\"这个 Finding 导致了更长的耗时\"的确定性结论；VMR 没有任务是否成功的标签，这里比较的是耗时这一个代理指标，不是效果。\n\n",

			ContextRotTitle:    "## Context 增长与质量拐点（Context Rot）\n\n",
			ContextRotHeader:   "| 上下文区间 | Step 样本数 | Finding 总数 | Finding 密度 | 错误 Step 数 | 错误率 |\n|---|---|---|---|---|---|\n",
			ContextRotFootnote: "> 按照 Step 输入 Token 大小分桶统计。高上下文区间下的 Finding 密度突增或错误率上升反映了注意力衰减（Context Rot）趋势。错误率基于协议原生错误标记统计。\n\n",

			ToolSeqTitle:    "## 高频工具调用序列模式（N-gram）\n\n",
			ToolSeqHeader:   "| 工具调用序列 | 出现频次 | 尾步错误率 |\n|---|---|---|\n",
			NoToolSeq:       "未提取到满足频次阈值的工具调用序列。\n\n",
			ToolSeqFootnote: "> 基于任务内连续 2-gram 与 3-gram 工具调用统计，展示最高频出现的行为定势与异常关联。\n\n",
		}
	}
	return CorpusText{
		Title:        "# Journey Corpus Report\n\n",
		JourneyCount: func(n int) string { return "> Analyzed " + strconv.Itoa(n) + " journeys\n\n" },
		NoJourneys:   "No journeys to analyze.\n",

		MetricDistTitle:  "## Metric Distributions\n\n",
		MetricDistHeader: "| Metric | N | Mean | Median | Min | Max | P90 |\n|---|---|---|---|---|---|---|\n",

		FindingRateTitle:  "## Finding Hit Rates\n\n",
		FindingRateHeader: "| Code | Hit Rate (≥1 occurrence) |\n|---|---|\n",
		NoFindings:        "No rule Findings were detected in this corpus.\n\n",
		AnthropicOnlyCoverageNote: func(anthropicPct, codes string) string {
			return "> ⚠️ Only " + anthropicPct + " of this corpus is Anthropic-protocol traffic. The following signals depend on a field only ever populated for Anthropic-protocol tool results (`chatmsg.ToolResult.IsError`) and structurally cannot fire on non-Anthropic requests — a 0% hit rate or all-zero metric here means \"couldn't be checked\", not \"checked, no issue found\": " + codes + "\n\n"
		},

		CorrelationTitle:  "## Metric Correlations (|rho| ≥ 0.3, Spearman rank, top 15 by |rho|)\n\n",
		CorrelationHeader: "| Metric A | Metric B | rho | N |\n|---|---|---|---|\n",
		NoCorrelations:    "No correlation cleared the threshold — this is itself an honest result, not proof the metrics are unrelated, just that no sufficiently strong rank correlation was observed at this sample size.\n\n",
		CorrelationMore: func(n int) string {
			return "> " + strconv.Itoa(n) + " more correlation(s) cleared the threshold but aren't listed here (several are mechanical relationships between time-based metrics, e.g. \"Net Working Time = Model Time + Agent-Side Execution Time\") — the full list is in vmr-story-corpus.json's correlations field.\n\n"
		},
		CorrelationFootnote: "> Effect size (rho) only — no p-values/significance claims: this corpus size can't support a rigorous significance test, and reporting one would manufacture false confidence. Correlation is not causation.\n\n",

		GroupCompTitle:     "## Finding-Grouped Comparison (Net Working Time)\n\n",
		GroupCompHeader:    "| Code | Hit N | No-Hit N | Hit Median | No-Hit Median | Relative Change |\n|---|---|---|---|---|---|\n",
		NoGroupComparisons: "No Finding had enough samples (≥3 on both sides) for a group comparison.\n\n",
		SkippedGroupComparisons: func(codes string) string {
			return "> The following Findings were skipped (fewer than 3 journeys on the hit or no-hit side — not evidence of no difference, just not enough data): " + codes + "\n\n"
		},
		GroupCompFootnote: "> ⚠️ = relative change ≥ 30% and the absolute difference clears the noise floor — a rule-based \"worth a look\" flag, not a determined \"this Finding caused longer duration\" conclusion; VMR has no task-success label, so this compares duration as a proxy, not outcome.\n\n",

		ContextRotTitle:    "## Context Window Scaling & Quality Inflection (Context Rot)\n\n",
		ContextRotHeader:   "| Context Range | Step N | Finding N | Finding Density | Error Step N | Error Rate |\n|---|---|---|---|---|---|\n",
		ContextRotFootnote: "> Bucket statistics by step input tokens. Increases in Finding density or error rate at larger context windows indicate context rot trends. Error rate is computed from protocol-level error markers.\n\n",

		ToolSeqTitle:    "## Frequent Tool Call Sequences (N-gram)\n\n",
		ToolSeqHeader:   "| Tool Sequence | Occurrences | Tail Step Error Rate |\n|---|---|---|\n",
		NoToolSeq:       "No tool call sequences met the frequency threshold.\n\n",
		ToolSeqFootnote: "> Continuous 2-gram and 3-gram tool sequences within task boundaries, showing dominant behavioral patterns and associated error rates.\n\n",
	}
}
