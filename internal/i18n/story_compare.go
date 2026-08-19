// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/story/render_compare.go (compare-*.md) and the 12
// behavior-profile metric labels internal/story/compare.go's Compare
// produces. MetricLabel takes the metric code as a plain string (not
// story.MetricCode) — internal/i18n must not import internal/story (it
// would invert the dependency direction the design doc and archtest
// require: story depends on i18n, never the reverse); MetricCode's
// underlying type is already a plain string, so callers pass
// string(diff.Metric).
package i18n

import "strconv"

// CompareText is render_compare.go's text, in one language.
type CompareText struct {
	Title                          string
	SideBlock                      func(label, id, title, from, to, file string) string
	InitialInstructionTitle        string
	InitialInstructionExcerptLabel func(side string) string
	ProfileTitle                   string
	ProfileTableHeader             string
	NotableFootnote                func(thresholdPct float64) string
	ToolsTitle                     string
	ToolsTableHeader               string

	SourcesTitle string
	SourcesIntro string

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

	CacheTitle        string
	CacheNoData       string
	CacheTableHeader  string
	CacheCurveSummary string
	CacheCurveNoData  string

	SysPromptTitle        string
	SysPromptTableHeader  string
	SysPromptExcerptLabel func(side string) string

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

func Compare(lang Lang) CompareText {
	if lang == ZH {
		return CompareText{
			Title: "# Journey 对比：A vs B\n\n",
			SideBlock: func(label, id, title, from, to, file string) string {
				idPart := id
				if file != "" {
					idPart = "[" + id + "](" + file + ")"
				}
				return "**" + label + "** " + idPart + "\n> " + title + "\n> " + from + " → " + to + "\n\n"
			},
			InitialInstructionTitle:        "## 初始指令\n\n",
			InitialInstructionExcerptLabel: func(side string) string { return side + " 的初始指令" },
			ProfileTitle:                   "## 行为剖面对比\n\n",
			ProfileTableHeader:             "| 指标 | A | B | 相对变化 |\n|---|---|---|---|\n",
			NotableFootnote: func(thresholdPct float64) string {
				return "\n> ⚠️ = 相对变化 ≥ " + strconv.FormatFloat(thresholdPct, 'f', 0, 64) + "% 且绝对差值超过噪声阈值——一个规则性的\"值得看一眼\"标记，不代表已判断出原因。\n\n"
			},
			ToolsTitle:       "## 工具调用对比\n\n",
			ToolsTableHeader: "| 工具 | A 次数 | B 次数 |\n|---|---|---|\n",

			SourcesTitle: "## 证据溯源\n\n",
			SourcesIntro: "本报告所有数字均计算自以下源审计文件：\n\n",

			WallClockLine: func(aWall, bWall string) string {
				return "总耗时（墙钟）：A " + aWall + " · B " + bWall + " —— 含人类空闲时间，不是效率指标，效率请看上表的\"净工作时长\"（设计文档 F10）。\n\n"
			},
			TerminationLine: func(aTerm, bTerm string) string {
				return "终止方式：A `finish=" + aTerm + "` · B `finish=" + bTerm + "`——VMR 只能看到这一步的结果，看不到 Agent 自身是否配置了类似 loop detection 的机制。\n\n"
			},
			FinalContextTitle: "**末轮上下文构成**\n\n",
			FinalContextHeader: func(aSeq, bSeq int) string {
				return "| | A（第 " + strconv.Itoa(aSeq) + " 轮） | B（第 " + strconv.Itoa(bSeq) + " 轮） |\n|---|---|---|\n"
			},
			FinalContextRowLabels: [4]string{"system", "user", "assistant", "tool"},
			EmptyDash:             "(无)",

			EndpointsTitle: "## 模型与端点核查\n\n",
			EndpointSide:   func(label, list string) string { return "- " + label + ": " + list + "\n" },
			EndpointsSame:  "两侧模型/端点完全相同。\n\n",
			EndpointsDiff:  "两侧模型/端点**不同**——这本身可能是效果差异的一个直接原因，不要默认排除。\n\n",
			NoEndpoints:    "(未识别到任何端点)",

			CacheTitle:        "## Prompt 缓存命中率\n\n",
			CacheNoData:       "两侧均未取得可用的 usage/缓存数据。\n\n",
			CacheTableHeader:  "| | 首轮 | 稳态均值（除首轮） | 最小 | 最大 |\n|---|---|---|---|---|\n",
			CacheCurveSummary: "<details><summary>逐轮曲线</summary>\n\n",
			CacheCurveNoData:  "(无数据)",

			SysPromptTitle:        "## System Prompt 规模与稳定性\n\n",
			SysPromptTableHeader:  "| | tokens | 变更次数 |\n|---|---|---|\n",
			SysPromptExcerptLabel: func(side string) string { return side + " 的 system prompt 节选" },

			DeliverableTitle: "## 最终交付物对比\n\n",
			DeliverableNotFound: func(side string) string {
				return "**" + side + "**：未识别到可比较的最终交付物（没有找到参数形状像文件写入的工具调用）。\n\n"
			},
			DeliverableFound: func(side string, stepSeq int, toolName string) string {
				return "**" + side + "**：在第 " + strconv.Itoa(stepSeq) + " 轮通过 `" + toolName + "` 识别到疑似最终交付物。\n\n"
			},
			DeliverableExcerptLabel: func(side string) string { return side + " 的交付物节选" },
			ExcerptTruncatedNote:    "（已截断）",

			DivergenceTitle: "## 分叉点\n\n",
			DivergenceNone:  "在两侧共享的前缀范围内，未检测到工具使用结构上的分叉——如实陈述：这不代表两条轨迹完全相同，只代表本检测器能看到的这一层信号没有分叉。\n\n",
			DivergenceHeavy: func(index int, taskTitle string, aSeq, bSeq int, aTools, bTools string) string {
				return "🔴 **重度分叉**（对齐位置第 " + strconv.Itoa(index+1) + " 步，所属任务：" + taskTitle + "）：A 第 " + strconv.Itoa(aSeq) + " 轮调用了 [" + aTools + "]，B 第 " + strconv.Itoa(bSeq) + " 轮调用了 [" + bTools + "]——从这一步开始，两条轨迹选择了不同的工具（或一方有调用、另一方没有）。这只是一个结构事实，\"为什么分叉更差\"仍然是解读层的可选推测，不是确定性结论。\n\n"
			},
			DivergenceLight: func(index int, taskTitle string, aSeq, bSeq int, tools string) string {
				return "🟡 **轻度分叉**（对齐位置第 " + strconv.Itoa(index+1) + " 步，所属任务：" + taskTitle + "）：A 第 " + strconv.Itoa(aSeq) + " 轮与 B 第 " + strconv.Itoa(bSeq) + " 轮都调用了 [" + tools + "]，但调用参数不同——工具选择一致，目标不同。\n\n"
			},
			DivergenceFootnote: "> 分叉点定位 ≠ 根因判定：这里只陈述\"从哪一步开始两者不同了\"，不推断谁对谁错、也不解释为什么。\n\n",

			MetricLabels: map[string]string{
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
		}
	}
	return CompareText{
		Title: "# Journey Comparison: A vs B\n\n",
		SideBlock: func(label, id, title, from, to, file string) string {
			idPart := id
			if file != "" {
				idPart = "[" + id + "](" + file + ")"
			}
			return "**" + label + "** " + idPart + "\n> " + title + "\n> " + from + " → " + to + "\n\n"
		},
		InitialInstructionTitle:        "## Initial Instruction\n\n",
		InitialInstructionExcerptLabel: func(side string) string { return side + "'s initial instruction" },
		ProfileTitle:                   "## Behavior Profile Comparison\n\n",
		ProfileTableHeader:             "| Metric | A | B | Relative Change |\n|---|---|---|---|\n",
		NotableFootnote: func(thresholdPct float64) string {
			return "\n> ⚠️ = relative change ≥ " + strconv.FormatFloat(thresholdPct, 'f', 0, 64) + "% and the absolute difference clears the noise floor — a rule-based \"worth a look\" flag, not a determined cause.\n\n"
		},
		ToolsTitle:       "## Tool Call Comparison\n\n",
		ToolsTableHeader: "| Tool | A Count | B Count |\n|---|---|---|\n",

		SourcesTitle: "## Evidence Provenance\n\n",
		SourcesIntro: "Every number in this report was computed from the following source audit files:\n\n",

		WallClockLine: func(aWall, bWall string) string {
			return "Wall-clock total: A " + aWall + " · B " + bWall + " — includes human idle time, not an efficiency metric; for efficiency see \"Net Working Time\" in the table above (design doc F10).\n\n"
		},
		TerminationLine: func(aTerm, bTerm string) string {
			return "Termination: A `finish=" + aTerm + "` · B `finish=" + bTerm + "` — VMR can only see this step's result, not whether the Agent itself has something like loop detection configured.\n\n"
		},
		FinalContextTitle: "**Final-Turn Context Composition**\n\n",
		FinalContextHeader: func(aSeq, bSeq int) string {
			return "| | A (turn " + strconv.Itoa(aSeq) + ") | B (turn " + strconv.Itoa(bSeq) + ") |\n|---|---|---|\n"
		},
		FinalContextRowLabels: [4]string{"system", "user", "assistant", "tool"},
		EmptyDash:             "(none)",

		EndpointsTitle: "## Model & Endpoint Check\n\n",
		EndpointSide:   func(label, list string) string { return "- " + label + ": " + list + "\n" },
		EndpointsSame:  "Model/endpoint identical on both sides.\n\n",
		EndpointsDiff:  "Model/endpoint **differ** between the two sides — this alone may be a direct cause of any outcome difference; don't rule it out by default.\n\n",
		NoEndpoints:    "(no endpoint identified)",

		CacheTitle:        "## Prompt Cache Hit Rate\n\n",
		CacheNoData:       "Neither side has usable usage/cache data.\n\n",
		CacheTableHeader:  "| | First Turn | Steady-State Mean (excl. first) | Min | Max |\n|---|---|---|---|---|\n",
		CacheCurveSummary: "<details><summary>Per-turn curve</summary>\n\n",
		CacheCurveNoData:  "(no data)",

		SysPromptTitle:        "## System Prompt Size & Stability\n\n",
		SysPromptTableHeader:  "| | tokens | Changes |\n|---|---|---|\n",
		SysPromptExcerptLabel: func(side string) string { return side + "'s system prompt excerpt" },

		DeliverableTitle: "## Final Deliverable Comparison\n\n",
		DeliverableNotFound: func(side string) string {
			return "**" + side + "**: no comparable final deliverable identified (no tool call whose parameter shape looks like a file write was found).\n\n"
		},
		DeliverableFound: func(side string, stepSeq int, toolName string) string {
			return "**" + side + "**: a likely final deliverable was identified at turn " + strconv.Itoa(stepSeq) + " via `" + toolName + "`.\n\n"
		},
		DeliverableExcerptLabel: func(side string) string { return side + "'s deliverable excerpt" },
		ExcerptTruncatedNote:    " (truncated)",

		DivergenceTitle: "## Divergence Point\n\n",
		DivergenceNone:  "No structural divergence in tool usage was detected within the two sides' shared prefix — stated honestly: this does not mean the two runs were identical, only that this detector's own signal never diverged.\n\n",
		DivergenceHeavy: func(index int, taskTitle string, aSeq, bSeq int, aTools, bTools string) string {
			return "🔴 **Heavy divergence** (aligned position " + strconv.Itoa(index+1) + ", task: " + taskTitle + "): A's turn " + strconv.Itoa(aSeq) + " called [" + aTools + "], B's turn " + strconv.Itoa(bSeq) + " called [" + bTools + "] — from here on the two runs chose different tools (or one called and the other didn't). This is a structural fact only; \"why the divergence made things worse\" is still an optional, speculative interpretation-layer claim, never a determined conclusion.\n\n"
		},
		DivergenceLight: func(index int, taskTitle string, aSeq, bSeq int, tools string) string {
			return "🟡 **Light divergence** (aligned position " + strconv.Itoa(index+1) + ", task: " + taskTitle + "): A's turn " + strconv.Itoa(aSeq) + " and B's turn " + strconv.Itoa(bSeq) + " both called [" + tools + "], but with different arguments — same tool choice, different target.\n\n"
		},
		DivergenceFootnote: "> Divergence-point location ≠ root-cause determination: this only states \"the two runs differ starting here\", not who was right or why.\n\n",

		MetricLabels: map[string]string{
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
	}
}

// MetricLabel returns code's localized display label — the static lookup
// story.RenderComparisonMarkdown uses instead of recomputing anything (see
// this file's package comment): code is a MetricDiff.Metric value's
// underlying string, never a language-dependent value itself.
func MetricLabel(lang Lang, code string) string {
	if label, ok := Compare(lang).MetricLabels[code]; ok {
		return label
	}
	return code
}
