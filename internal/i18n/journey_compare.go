// Ver 2026-09-22 03:00, by Sonnet 5

// Pairs with internal/journey/render_compare.go (compare-*.md) and the 14
// behavior-profile metric labels internal/journey/compare.go's Compare
// produces. MetricLabel takes the metric code as a plain string (not
// journey.MetricCode) — internal/i18n must not import internal/journey (it
// would invert the dependency direction the design doc and archtest
// require: journey depends on i18n, never the reverse); MetricCode's
// underlying type is already a plain string, so callers pass
// string(diff.Metric).
package i18n

import (
	"fmt"
	"strconv"
	"strings"
)

// CompareText is render_compare.go's text, in one language.
type CompareText struct {
	Title string
	// PartialBanner is the ⚠️ line shown when either side is head-truncated
	// (the "-partial" filename suffix is retired; partiality lives as
	// the Comparison's Partial field and this banner).
	PartialBanner                  string
	SummaryCard                    func(items []string) string
	SummaryNotableTop              func(items string) string
	SummaryDivergence              func(index, aSeq, bSeq int) string
	SummaryEndpointsSame           string
	SummaryEndpointsDiff           string
	SummaryTermination             func(aTerm, bTerm string) string
	SideBlock                      func(label, id, title, from, to, file string) string
	InitialInstructionTitle        string
	InitialInstructionExcerptLabel func(side string) string
	InitialInstructionIdentical    string
	ProfileTitle                   string
	ProfileTableHeader             string
	NotableFootnote                func(thresholdPct float64) string
	// DeltaNew is the "变化" column value when a metric went from 0 to a
	// positive value (no proportional change is meaningful there).
	DeltaNew         string
	ToolsTitle       string
	ToolsTableHeader string

	SourcesTitle string
	SourcesIntro string

	CostTitle       string
	CostLine        func(a, b string) string
	CostUnresolved  string
	CostOneSideNote func(side string) string // footnote when exactly one side priced (F-3)

	WallClockLine         func(aWall, bWall string) string
	TerminationLine       func(aTerm, bTerm string) string
	FinalContextTitle     string
	FinalContextHeader    func(aSeq, bSeq int) string
	FinalContextRowLabels [4]string // system, user, assistant, tool
	EmptyDash             string

	EndpointsTitle string
	EndpointSide   func(label, list string) string
	EndpointsSame  string
	EndpointsDiff  string
	NoEndpoints    string

	CacheTitle             string
	CacheNoData            string
	CacheTableHeader       string
	CacheBreaksTableHeader string
	CacheCurveSummary      string
	CacheCurveNoData       string

	SysPromptTitle        string
	SysPromptTableHeader  string
	SysPromptExcerptLabel func(side string) string
	SysPromptIdentical    func(tokens string) string
	SysPromptDiffSummary  func(diffCount int) string

	DeliverableTitle        string
	DeliverableNotFound     func(side string) string
	DeliverableFound        func(side string, stepSeq int, toolName string) string
	DeliverableExcerptLabel func(side string) string
	ExcerptTruncatedNote    string

	DivergenceTitle    string
	DivergenceNone     string
	DivergenceHeavy    func(index int, taskTitle string, aSeq, bSeq int, aTools, bTools string) string
	DivergenceLight    func(index int, taskTitle string, aSeq, bSeq int, tools string) string
	DivergenceFootnote string

	MetricLabels map[string]string
}

// compareRow holds this file's literal templates, one row per Lang (Table's
// own doc comment). notableFootnoteFmt escapes its two literal "%" example
// figures ("+40%", "-100%") as "%%" — easy to miss since they read as plain
// text in the original concatenation but become format verbs once this
// string is a Sprintf template.
type compareRow struct {
	title                             string
	partialBanner                     string
	summaryCardHeader                 string
	summaryNotableTopFmt              string
	summaryDivergenceFmt              string
	summaryEndpointsSame              string
	summaryEndpointsDiff              string
	summaryTerminationFmt             string
	initialInstructionTitle           string
	initialInstructionExcerptLabelFmt string
	initialInstructionIdentical       string
	profileTitle                      string
	profileTableHeader                string
	notableFootnoteFmt                string
	deltaNew                          string
	toolsTitle                        string
	toolsTableHeader                  string

	sourcesTitle string
	sourcesIntro string

	costTitle          string
	costLineFmt        string
	costUnresolved     string
	costOneSideNoteFmt string

	wallClockLineFmt      string
	terminationLineFmt    string
	finalContextTitle     string
	finalContextHeaderFmt string
	finalContextRowLabels [4]string
	emptyDash             string

	endpointsTitle  string
	endpointSideFmt string
	endpointsSame   string
	endpointsDiff   string
	noEndpoints     string

	cacheTitle             string
	cacheNoData            string
	cacheTableHeader       string
	cacheBreaksTableHeader string
	cacheCurveSummary      string
	cacheCurveNoData       string

	sysPromptTitle           string
	sysPromptTableHeader     string
	sysPromptExcerptLabelFmt string
	sysPromptIdenticalFmt    string
	sysPromptDiffSummaryFmt  string

	deliverableTitle           string
	deliverableNotFoundFmt     string
	deliverableFoundFmt        string
	deliverableExcerptLabelFmt string
	excerptTruncatedNote       string

	divergenceTitle    string
	divergenceNone     string
	divergenceHeavyFmt string
	divergenceLightFmt string
	divergenceFootnote string

	metricLabels map[string]string
}

var compareRows = Table[compareRow]{
	EN: {
		title:                             "# Journey Comparison: A vs B\n\n",
		partialBanner:                     "One or both journeys are head-truncated by the loaded file range; the affected side shows only its visible part.",
		summaryCardHeader:                 "> **Comparison Summary**:\n",
		summaryNotableTopFmt:              "Notable differences (Top): %s",
		summaryDivergenceFmt:              "Divergence point: aligned position %d (Step A%d / B%d)",
		summaryEndpointsSame:              "Endpoints: identical models/endpoints on both sides",
		summaryEndpointsDiff:              "Endpoints: models/endpoints differ between sides",
		summaryTerminationFmt:             "Termination: A finish=%s · B finish=%s",
		initialInstructionTitle:           "## Initial Instruction\n\n",
		initialInstructionExcerptLabelFmt: "%s's initial instruction",
		initialInstructionIdentical:       "> Both sides' initial instructions are verbatim identical (truncated prefix — not a claim that the full text matches).\n\n",
		profileTitle:                      "## Behavior Profile Comparison\n\n",
		profileTableHeader:                "| Metric | A | B | Change |\n|---|---|---|---|\n",
		notableFootnoteFmt:                "\n> The \"Change\" column gives direction and magnitude (`156×` / `0.02×` / `+40%%` / `new` / `-100%%`), not proportional precision. ⚠️ = relative difference ≥ %s%% and the absolute difference clears the noise floor — a rule-based \"worth a look\" flag, not a determined cause.\n\n",
		deltaNew:                          "new",
		toolsTitle:                        "## Tool Call Comparison\n\n",
		toolsTableHeader:                  "| Tool | A Count | B Count |\n|---|---|---|\n",

		sourcesTitle: "## Evidence Provenance\n\n",
		sourcesIntro: "Every number in this report was computed from the following source audit files:\n\n",

		costTitle:          "## Cost Estimate\n\n",
		costLineFmt:        "A %s · B %s (list-price estimate, not your bill)\n\n",
		costUnresolved:     "Neither side had resolvable pricing (common for Token-Plan / subscription accounts).\n\n",
		costOneSideNoteFmt: "> %s: pricing not on file (common for Token-Plan / subscription accounts) — a blank does not mean free.\n\n",

		wallClockLineFmt:      "Wall-clock total: A %s · B %s — includes human idle time, not an efficiency metric; for efficiency see \"Net Working Time\" in the table above (design doc F10).\n\n",
		terminationLineFmt:    "Termination: A `finish=%s` · B `finish=%s` — VMR can only see this step's result, not whether the Agent itself has something like loop detection configured.\n\n",
		finalContextTitle:     "**Final-Turn Context Composition**\n\n",
		finalContextHeaderFmt: "| | A (turn %d) | B (turn %d) |\n|---|---|---|\n",
		finalContextRowLabels: [4]string{"system", "user", "assistant", "tool"},
		emptyDash:             "(none)",

		endpointsTitle:  "## Model & Endpoint Check\n\n",
		endpointSideFmt: "- %s: %s\n",
		endpointsSame:   "Model/endpoint identical on both sides.\n\n",
		endpointsDiff:   "Model/endpoint **differ** between the two sides — this alone may be a direct cause of any outcome difference; don't rule it out by default.\n\n",
		noEndpoints:     "(no endpoint identified)",

		cacheTitle:             "## Prompt Cache Hit Rate\n\n",
		cacheNoData:            "Neither side has usable usage/cache data.\n\n",
		cacheTableHeader:       "| | First Turn | Steady-State Mean (excl. first) | Min | Max |\n|---|---|---|---|---|\n",
		cacheBreaksTableHeader: "| | Unexplained Drop | Provider Switch | System Prompt | Tools Churn | History Break |\n|---|---|---|---|---|---|\n",
		cacheCurveSummary:      "<details><summary>Per-turn curve</summary>\n\n> Turn numbers are shown only for turns with a computable hit rate; a gap means that turn returned no usage, not a numbering error.\n\n",
		cacheCurveNoData:       "(no data)",

		sysPromptTitle:           "## System Prompt Size & Stability\n\n",
		sysPromptTableHeader:     "| | tokens | Changes |\n|---|---|---|\n",
		sysPromptExcerptLabelFmt: "%s's system prompt excerpt",
		sysPromptIdenticalFmt:    "> Both sides' System Prompts are verbatim identical (%s tokens).\n\n",
		sysPromptDiffSummaryFmt:  "System Prompt Diff (approx. %d lines changed)",

		deliverableTitle:           "## Final Deliverable Comparison\n\n",
		deliverableNotFoundFmt:     "**%s**: no comparable final deliverable identified (no tool call whose parameter shape looks like a file write was found).\n\n",
		deliverableFoundFmt:        "**%s**: a likely final deliverable was identified at turn %d via `%s`.\n\n",
		deliverableExcerptLabelFmt: "%s's deliverable excerpt",
		excerptTruncatedNote:       " (truncated)",

		divergenceTitle:    "## Divergence Point\n\n",
		divergenceNone:     "No structural divergence in tool usage was detected within the two sides' shared prefix — stated honestly: this does not mean the two runs were identical, only that this detector's own signal never diverged.\n\n",
		divergenceHeavyFmt: "🔴 **Heavy divergence** (aligned position %d, task: %s): A's turn %d called [%s], B's turn %d called [%s] — from here on the two runs chose different tools (or one called and the other didn't). This is a structural fact only; \"why the divergence made things worse\" is still an optional, speculative interpretation-layer claim, never a determined conclusion.\n\n",
		divergenceLightFmt: "🟡 **Light divergence** (aligned position %d, task: %s): A's turn %d and B's turn %d both called [%s], but with different arguments — same tool choice, different target.\n\n",
		divergenceFootnote: "> Divergence-point location ≠ root-cause determination: this only states \"the two runs differ starting here\", not who was right or why.\n\n",

		metricLabels: map[string]string{
			"model_ms":               "Model Time",
			"agent_exec_ms":          "Agent-Side Execution Time",
			"human_idle_ms":          "Human Idle Time",
			"net_working_ms":         "Net Working Time",
			"model_tool_ratio":       "Model/Tool Time Ratio",
			"tool_call_count":        "Tool Call Count",
			"duplicate_action_rate":  "Duplicate Action Rate",
			"error_recovery_count":   "Error Recovery Count",
			"plan_exec_ratio":        "Plan/Execution Ratio",
			"context_utilization":    "Context Utilization",
			"compaction_count":       "Compaction Count",
			"compaction_loss_tokens": "Compaction Information Loss",
			"model_switch_count":     "Model Switch Count",
			"output_repetition_rate": "Output Repetition Rate",
		},
	},
	ZH: {
		title:                             "# Journey 对比：A vs B\n\n",
		partialBanner:                     "有一侧或两侧 Journey 的开头被所加载的文件范围截断，受影响一方展示的只是可见部分。",
		summaryCardHeader:                 "> **对比摘要**：\n",
		summaryNotableTopFmt:              "显著差异 Top: %s",
		summaryDivergenceFmt:              "分叉点: 对齐位置 %d (Step A%d / B%d)",
		summaryEndpointsSame:              "端点一致: 两侧模型/端点完全相同",
		summaryEndpointsDiff:              "端点不同: 两侧模型/端点存在差异",
		summaryTerminationFmt:             "终止状态: A finish=%s · B finish=%s",
		initialInstructionTitle:           "## 初始指令\n\n",
		initialInstructionExcerptLabelFmt: "%s 的初始指令",
		initialInstructionIdentical:       "> 两侧初始指令逐字完全一致（节选前缀，不代表完整文本逐字相同）。\n\n",
		profileTitle:                      "## 行为剖面对比\n\n",
		profileTableHeader:                "| 指标 | A | B | 变化 |\n|---|---|---|---|\n",
		notableFootnoteFmt:                "\n> 「变化」列给方向与量级（`156×` / `0.02×` / `+40%%` / `新增` / `-100%%`），非比例精度。⚠️ = 相对差 ≥ %s%% 且绝对差值超过噪声阈值——一个规则性的\"值得看一眼\"标记，不代表已判断出原因。\n\n",
		deltaNew:                          "新增",
		toolsTitle:                        "## 工具调用对比\n\n",
		toolsTableHeader:                  "| 工具 | A 次数 | B 次数 |\n|---|---|---|\n",

		sourcesTitle: "## 证据溯源\n\n",
		sourcesIntro: "本报告所有数字均计算自以下源审计文件：\n\n",

		costTitle:          "## 成本估算\n\n",
		costLineFmt:        "A %s · B %s（按标价估算，非实际账单）\n\n",
		costUnresolved:     "两侧均无可解析定价（Token-Plan / 订阅制账户常见）。\n\n",
		costOneSideNoteFmt: "> %s 侧定价未收录（Token-Plan / 订阅制账户常见）—— 空白不代表免费。\n\n",

		wallClockLineFmt:      "总耗时（墙钟）：A %s · B %s —— 含人类空闲时间，不是效率指标，效率请看上表的\"净工作时长\"（设计文档 F10）。\n\n",
		terminationLineFmt:    "终止方式：A `finish=%s` · B `finish=%s`——VMR 只能看到这一步的结果，看不到 Agent 自身是否配置了类似 loop detection 的机制。\n\n",
		finalContextTitle:     "**末轮上下文构成**\n\n",
		finalContextHeaderFmt: "| | A（第 %d 轮） | B（第 %d 轮） |\n|---|---|---|\n",
		finalContextRowLabels: [4]string{"system", "user", "assistant", "tool"},
		emptyDash:             "(无)",

		endpointsTitle:  "## 模型与端点核查\n\n",
		endpointSideFmt: "- %s: %s\n",
		endpointsSame:   "两侧模型/端点完全相同。\n\n",
		endpointsDiff:   "两侧模型/端点**不同**——这本身可能是效果差异的一个直接原因，不要默认排除。\n\n",
		noEndpoints:     "(未识别到任何端点)",

		cacheTitle:             "## Prompt 缓存命中率\n\n",
		cacheNoData:            "两侧均未取得可用的 usage/缓存数据。\n\n",
		cacheTableHeader:       "| | 首轮 | 稳态均值（除首轮） | 最小 | 最大 |\n|---|---|---|---|---|\n",
		cacheBreaksTableHeader: "| | 异常骤降 | 端点切换 | 系统提示词 | 工具定义 | 历史断裂 |\n|---|---|---|---|---|---|\n",
		cacheCurveSummary:      "<details><summary>逐轮曲线</summary>\n\n> 轮次编号只对能算出命中率的轮次连续标注；缺号表示该轮未返回 usage，不是编号错误。\n\n",
		cacheCurveNoData:       "(无数据)",

		sysPromptTitle:           "## System Prompt 规模与稳定性\n\n",
		sysPromptTableHeader:     "| | tokens | 变更次数 |\n|---|---|---|\n",
		sysPromptExcerptLabelFmt: "%s 的 system prompt 节选",
		sysPromptIdenticalFmt:    "> 两侧 System Prompt 逐字完全一致（%s token）。\n\n",
		sysPromptDiffSummaryFmt:  "两侧 System Prompt 差异（约 %d 行不同）",

		deliverableTitle:           "## 最终交付物对比\n\n",
		deliverableNotFoundFmt:     "**%s**：未识别到可比较的最终交付物（没有找到参数形状像文件写入的工具调用）。\n\n",
		deliverableFoundFmt:        "**%s**：在第 %d 轮通过 `%s` 识别到疑似最终交付物。\n\n",
		deliverableExcerptLabelFmt: "%s 的交付物节选",
		excerptTruncatedNote:       "（已截断）",

		divergenceTitle:    "## 分叉点\n\n",
		divergenceNone:     "在两侧共享的前缀范围内，未检测到工具使用结构上的分叉——如实陈述：这不代表两条轨迹完全相同，只代表本检测器能看到的这一层信号没有分叉。\n\n",
		divergenceHeavyFmt: "🔴 **重度分叉**（对齐位置第 %d 步，所属任务：%s）：A 第 %d 轮调用了 [%s]，B 第 %d 轮调用了 [%s]——从这一步开始，两条轨迹选择了不同的工具（或一方有调用、另一方没有）。这只是一个结构事实，\"为什么分叉更差\"仍然是解读层的可选推测，不是确定性结论。\n\n",
		divergenceLightFmt: "🟡 **轻度分叉**（对齐位置第 %d 步，所属任务：%s）：A 第 %d 轮与 B 第 %d 轮都调用了 [%s]，但调用参数不同——工具选择一致，目标不同。\n\n",
		divergenceFootnote: "> 分叉点定位 ≠ 根因判定：这里只陈述\"从哪一步开始两者不同了\"，不推断谁对谁错、也不解释为什么。\n\n",

		metricLabels: map[string]string{
			"model_ms":               "模型时间",
			"agent_exec_ms":          "Agent 侧执行时间",
			"human_idle_ms":          "人类空闲时间",
			"net_working_ms":         "净工作时长",
			"model_tool_ratio":       "模型/工具时间比",
			"tool_call_count":        "工具调用次数",
			"duplicate_action_rate":  "重复动作率",
			"error_recovery_count":   "错误恢复次数",
			"plan_exec_ratio":        "计划/执行比",
			"context_utilization":    "上下文有效利用率",
			"compaction_count":       "Compaction 次数",
			"compaction_loss_tokens": "Compaction 信息损失",
			"model_switch_count":     "模型切换次数",
			"output_repetition_rate": "输出重复率",
		},
	},
}

func Compare(lang Lang) CompareText {
	r := compareRows.Row(lang)
	return CompareText{
		Title:         r.title,
		PartialBanner: r.partialBanner,
		SummaryCard: func(items []string) string {
			var b strings.Builder
			b.WriteString(r.summaryCardHeader)
			for _, it := range items {
				b.WriteString("> - " + it + "\n")
			}
			b.WriteString("\n")
			return b.String()
		},
		SummaryNotableTop: func(items string) string { return fmt.Sprintf(r.summaryNotableTopFmt, items) },
		SummaryDivergence: func(index, aSeq, bSeq int) string {
			return fmt.Sprintf(r.summaryDivergenceFmt, index, aSeq, bSeq)
		},
		SummaryEndpointsSame: r.summaryEndpointsSame,
		SummaryEndpointsDiff: r.summaryEndpointsDiff,
		SummaryTermination: func(aTerm, bTerm string) string {
			return fmt.Sprintf(r.summaryTerminationFmt, aTerm, bTerm)
		},
		// SideBlock's markdown shape ("**label** id\n> title\n> from → to\n\n")
		// and its "wrap id in a link when file != """ branch don't vary by
		// language at all — no row field needed.
		SideBlock: func(label, id, title, from, to, file string) string {
			idPart := id
			if file != "" {
				idPart = "[" + id + "](" + file + ")"
			}
			return "**" + label + "** " + idPart + "\n> " + title + "\n> " + from + " → " + to + "\n\n"
		},
		InitialInstructionTitle: r.initialInstructionTitle,
		InitialInstructionExcerptLabel: func(side string) string {
			return fmt.Sprintf(r.initialInstructionExcerptLabelFmt, side)
		},
		InitialInstructionIdentical: r.initialInstructionIdentical,
		ProfileTitle:                r.profileTitle,
		ProfileTableHeader:          r.profileTableHeader,
		NotableFootnote: func(thresholdPct float64) string {
			return fmt.Sprintf(r.notableFootnoteFmt, strconv.FormatFloat(thresholdPct, 'f', 0, 64))
		},
		DeltaNew:         r.deltaNew,
		ToolsTitle:       r.toolsTitle,
		ToolsTableHeader: r.toolsTableHeader,

		SourcesTitle: r.sourcesTitle,
		SourcesIntro: r.sourcesIntro,

		CostTitle:      r.costTitle,
		CostLine:       func(a, b string) string { return fmt.Sprintf(r.costLineFmt, a, b) },
		CostUnresolved: r.costUnresolved,
		CostOneSideNote: func(side string) string {
			return fmt.Sprintf(r.costOneSideNoteFmt, side)
		},

		WallClockLine: func(aWall, bWall string) string {
			return fmt.Sprintf(r.wallClockLineFmt, aWall, bWall)
		},
		TerminationLine: func(aTerm, bTerm string) string {
			return fmt.Sprintf(r.terminationLineFmt, aTerm, bTerm)
		},
		FinalContextTitle: r.finalContextTitle,
		FinalContextHeader: func(aSeq, bSeq int) string {
			return fmt.Sprintf(r.finalContextHeaderFmt, aSeq, bSeq)
		},
		FinalContextRowLabels: r.finalContextRowLabels,
		EmptyDash:             r.emptyDash,

		EndpointsTitle: r.endpointsTitle,
		EndpointSide:   func(label, list string) string { return fmt.Sprintf(r.endpointSideFmt, label, list) },
		EndpointsSame:  r.endpointsSame,
		EndpointsDiff:  r.endpointsDiff,
		NoEndpoints:    r.noEndpoints,

		CacheTitle:             r.cacheTitle,
		CacheNoData:            r.cacheNoData,
		CacheTableHeader:       r.cacheTableHeader,
		CacheBreaksTableHeader: r.cacheBreaksTableHeader,
		CacheCurveSummary:      r.cacheCurveSummary,
		CacheCurveNoData:       r.cacheCurveNoData,

		SysPromptTitle:       r.sysPromptTitle,
		SysPromptTableHeader: r.sysPromptTableHeader,
		SysPromptExcerptLabel: func(side string) string {
			return fmt.Sprintf(r.sysPromptExcerptLabelFmt, side)
		},
		SysPromptIdentical: func(tokens string) string {
			return fmt.Sprintf(r.sysPromptIdenticalFmt, tokens)
		},
		SysPromptDiffSummary: func(diffCount int) string {
			return fmt.Sprintf(r.sysPromptDiffSummaryFmt, diffCount)
		},

		DeliverableTitle: r.deliverableTitle,
		DeliverableNotFound: func(side string) string {
			return fmt.Sprintf(r.deliverableNotFoundFmt, side)
		},
		DeliverableFound: func(side string, stepSeq int, toolName string) string {
			return fmt.Sprintf(r.deliverableFoundFmt, side, stepSeq, toolName)
		},
		DeliverableExcerptLabel: func(side string) string {
			return fmt.Sprintf(r.deliverableExcerptLabelFmt, side)
		},
		ExcerptTruncatedNote: r.excerptTruncatedNote,

		DivergenceTitle: r.divergenceTitle,
		DivergenceNone:  r.divergenceNone,
		DivergenceHeavy: func(index int, taskTitle string, aSeq, bSeq int, aTools, bTools string) string {
			return fmt.Sprintf(r.divergenceHeavyFmt, index+1, taskTitle, aSeq, aTools, bSeq, bTools)
		},
		DivergenceLight: func(index int, taskTitle string, aSeq, bSeq int, tools string) string {
			return fmt.Sprintf(r.divergenceLightFmt, index+1, taskTitle, aSeq, bSeq, tools)
		},
		DivergenceFootnote: r.divergenceFootnote,

		MetricLabels: r.metricLabels,
	}
}

// MetricLabel returns code's localized display label — the static lookup
// journey.RenderComparisonMarkdown uses instead of recomputing anything (see
// this file's package comment): code is a MetricDiff.Metric value's
// underlying string, never a language-dependent value itself.
func MetricLabel(lang Lang, code string) string {
	if label, ok := Compare(lang).MetricLabels[code]; ok {
		return label
	}
	return code
}
