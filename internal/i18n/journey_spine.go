// Ver 2026-09-22 03:10, by Sonnet 5

// Pairs with internal/journey/render_spine.go and its viewmodel counterpart
// internal/journey/viewmodel_spine.go — the decision-spine layer
// (a 3-second overview card, a compact per-Task action
// list, per-Step role tags, an optional tool-call timeline) added on top
// of render_md.go's existing fact-layer renderer. All of it is pure
// formatting over data render_md.go's renderStep already has; only the
// Findings section's text comes from journey_findings.go.
package i18n

import "fmt"

// SpineText is render_spine.go's text, in one language.
type SpineText struct {
	OverviewTitle       string
	OverviewStart       func(ts string) string
	OverviewFirstError  func(seq int, ts string) string
	OverviewTransition  func(seq int, kind, ts string) string
	OverviewEnd         func(seq int, finish, ts string) string
	TagToolIntensive    string
	TagRetryHeavy       string
	TagContextCompacted string
	TagsLine            func(tags string) string
	// OverviewCostLine is the overview card's estimated-spend bullet, rendered
	// only when a price book resolved (same "$ only when priced, and labelled
	// an estimate" discipline the HTML damage line and the macro report use).
	OverviewCostLine func(money string) string

	// OverviewFailedStepsLine is rendered in the overview card when any
	// requests in this Journey failed at the HTTP/upstream layer (问题 8).
	OverviewFailedStepsLine func(failed, total int) string

	SpineTitle                string
	SpineTaskLine             func(idx int, title string) string
	SpineFindingTag           string                      // appended to a spine Step header that hit a Finding
	SpineValueTruncated       func(more int) string       // appended to a tool-call payload block capped at spineFullCap
	SpineResultValueTruncated func(more int) string       // same, for a paired tool RESULT — the full text lives in the NEXT Step's record
	SpineDetailLink           func(relPath string) string // "→ detail" link to this Step's own record
	// SpineDetailCoord is the coordinate form of the same "→ detail"
	// pointer, used by the default batch suite where detail pages are not
	// materialized — an inline `file:line` coordinate instead of a link
	// that would 404.
	SpineDetailCoord func(coord string) string
	// SpineCoordNote is the one-time explainer printed at the top of a
	// coordinate-mode decision spine, so a reader knows why the Steps show
	// coordinates rather than links and how to get the linked form.
	SpineCoordNote string

	SpineInstructionLine func(text string) string // a mid-task Step whose opening carries a new user instruction
	SpineReportLine      func(text string) string // a non-tool-calling Step's plain report/reasoning one-liner
	SpinePositionalMatch string                   // appended to a tool result paired by position, not id (level 3)
	CacheBreakBadge      func(kind string, from, to string) string

	ArtifactsTitle       string
	ArtifactsSummary     func(count int) string
	ArtifactsTableHeader string
	ArtifactsHeuristic   string
	ArtifactsStructured  string

	SpineFinalDeliverableTitle        string
	SpineFinalDeliverableFound        func(stepSeq int, toolName string) string
	SpineFinalDeliverableExcerptLabel string

	StepTagPlan       string
	StepTagAction     string
	StepTagObserve    string
	StepTagRetry      string
	StepTagError      string
	StepTagCompaction string
	StepTagReport     string

	TimelineTitle  string
	TimelineLegend string
	TimelineNoData string

	FindingsTitle string
	FindingsNone  string
	// FindingGroupTitle is the per-detector group header rendered above a
	// set of findings sharing one Code — "*N* hits of this detector", so a
	// 162-item flat list stops reading as 162 distinct problems (问题 15).
	FindingGroupTitle func(code string, count int, firstStep int) string
	FindingHeader     func(idx int, code string, stepSeq int) string
	FindingRelated    func(seqs string) string
	FindingEvidence   func(text string) string
	FindingAction     func(text string) string
	// AnthropicOnlyCoverageNote is the per-journey detector-coverage
	// disclosure line — same wording/role as journey_benchmarks.go's field
	// of the same name, printed when THIS journey has no Anthropic Messages
	// Steps at all, naming the signals (Findings, the decision spine's own
	// ❌/↩️ tool-result badge, structure.json's ToolCalls[].ResultError)
	// that read as clean/absent for structural reasons on this journey, not
	// because nothing was found.
	AnthropicOnlyCoverageNote func(codes string) string

	BadgeLLMInferred    func(confidence string) string
	BadgeRuleDetected   string
	LabelEvidenceAnchor string
}

// spineRow holds this file's literal templates, one row per Lang (Table's
// own doc comment). CacheBreakBadge's switch over kind is the one real
// branching logic — same set of kind values dispatch to the same case in
// both languages, so the switch is written once in Spine below and each
// case's OUTPUT (mostly plain, one interpolated) lives here.
type spineRow struct {
	overviewTitle          string
	overviewStartFmt       string
	overviewFirstErrorFmt  string
	overviewTransitionFmt  string
	overviewEndFmt         string
	tagToolIntensive       string
	tagRetryHeavy          string
	tagContextCompacted    string
	tagsLineFmt            string
	overviewCostLineFmt    string
	overviewFailedStepsFmt string

	spineTitle                   string
	spineTaskLineFmt             string
	spineFindingTag              string
	spineValueTruncatedFmt       string
	spineResultValueTruncatedFmt string
	spineDetailLinkFmt           string
	spineDetailCoordFmt          string
	spineCoordNote               string

	spineInstructionLineFmt string
	spineReportLineFmt      string
	spinePositionalMatch    string

	cacheBreakUnexplainedFmt  string
	cacheBreakProviderSwitch  string
	cacheBreakSystem          string
	cacheBreakTools           string
	cacheBreakHistoryStitch   string
	cacheBreakHistoryContract string
	cacheBreakHistoryFork     string

	artifactsTitle       string
	artifactsSummaryFmt  string
	artifactsTableHeader string
	artifactsHeuristic   string
	artifactsStructured  string

	spineFinalDeliverableTitle        string
	spineFinalDeliverableFoundFmt     string
	spineFinalDeliverableExcerptLabel string

	stepTagPlan       string
	stepTagAction     string
	stepTagObserve    string
	stepTagRetry      string
	stepTagError      string
	stepTagCompaction string
	stepTagReport     string

	timelineTitle  string
	timelineLegend string
	timelineNoData string

	findingsTitle            string
	findingsNone             string
	findingGroupTitleFmt     string
	findingHeaderFmt         string
	findingRelatedFmt        string
	findingEvidenceFmt       string
	findingActionFmt         string
	anthropicOnlyCoverageFmt string

	badgeLLMInferredFmt string
	badgeRuleDetected   string
	labelEvidenceAnchor string
}

var spineRows = Table[spineRow]{
	EN: {
		overviewTitle:          "## Overview\n\n",
		overviewStartFmt:       "Started %s",
		overviewFirstErrorFmt:  "First error marker · Step %d · %s",
		overviewTransitionFmt:  "First non-routine transition (%s) · Step %d · %s",
		overviewEndFmt:         "Ended · Step %d · finish=%s · %s",
		tagToolIntensive:       "tool-intensive",
		tagRetryHeavy:          "retry-heavy",
		tagContextCompacted:    "context-compacted",
		tagsLineFmt:            "**Tags**: %s\n\n",
		overviewCostLineFmt:    "Estimated cost ≈ %s (list-price estimate, not your bill)",
		overviewFailedStepsFmt: "%d/%d steps failed (HTTP/upstream error)",

		spineTitle:                   "## Decision Spine\n\n",
		spineTaskLineFmt:             "**t%s · %s**\n\n",
		spineFindingTag:              " ⚠️",
		spineValueTruncatedFmt:       "\n… (+%d more chars — full value in this Step's detail page)",
		spineResultValueTruncatedFmt: "\n… (+%d more chars — full value in the next Step's detail page)",
		spineDetailLinkFmt:           "→ [detail](%s)\n\n",
		spineDetailCoordFmt:          "→ `%s`\n\n",
		spineCoordNote:               "> Batch suite output: each Step shows only its source coordinate (`file:line`). Run `vmr analyze -journey <id>` for a single-journey report with full detail links.\n\n",

		spineInstructionLineFmt: "💬 Instruction · %s\n\n",
		spineReportLineFmt:      "💬 Report · %s\n\n",
		spinePositionalMatch:    " (matched by position — ID unmatched)",

		cacheBreakUnexplainedFmt:  "Cache: unexplained drop (%s→%s)",
		cacheBreakProviderSwitch:  "Cache: provider switched",
		cacheBreakSystem:          "Cache: system prompt changed",
		cacheBreakTools:           "Cache: tool definitions changed",
		cacheBreakHistoryStitch:   "Cache: context stitched",
		cacheBreakHistoryContract: "Cache: context contracted",
		cacheBreakHistoryFork:     "Cache: context forked",

		artifactsTitle:       "## Touched Artifacts\n\n",
		artifactsSummaryFmt:  "Recorded %d target(s) touched during execution:\n\n",
		artifactsTableHeader: "| Target Path | Operation | First Step | Calls | Detection |\n| :--- | :--- | :--- | :--- | :--- |\n",
		artifactsHeuristic:   "Shell heuristic",
		artifactsStructured:  "Structured parameter",

		spineFinalDeliverableTitle:        "## Final Deliverable\n\n",
		spineFinalDeliverableFoundFmt:     "Step %d · `%s`\n\n",
		spineFinalDeliverableExcerptLabel: "Deliverable excerpt",

		stepTagPlan:       "🔷 📋",
		stepTagAction:     "🔷 🔧",
		stepTagObserve:    "🔷 👀",
		stepTagRetry:      "🔷 🔄",
		stepTagError:      "🔷 ⚠️",
		stepTagCompaction: "🔷 🧹",
		stepTagReport:     "🔷 💬",

		timelineTitle:  "## Tool Call Timeline\n\n",
		timelineLegend: "Legend: ● normal · 🔄 suspected repeat · ❌ step carries an error marker\n\n",
		timelineNoData: "(no tool calls in this Journey)\n\n",

		findingsTitle:            "## Suspected Issues (candidate list, not a verdict)\n\n",
		findingsNone:             "No rule-detectable suspected issues.\n\n",
		findingGroupTitleFmt:     "### %s · %d hits (earliest Step %d)\n\n",
		findingHeaderFmt:         "%d. **%s** · Step %d\n",
		findingRelatedFmt:        "   - related Steps: %s\n",
		findingEvidenceFmt:       "   - evidence: %s\n",
		findingActionFmt:         "   - action: %s\n",
		anthropicOnlyCoverageFmt: "> ⚠️ Every request in this journey is non-Anthropic Messages protocol. The following signals depend on a field only ever populated for Anthropic Messages protocol and structurally cannot fire on this journey — their absence doesn't mean \"checked, no issue found\": %s\n\n",

		badgeLLMInferredFmt: " [AI Inferred · %s]",
		badgeRuleDetected:   " [Rule-detected]",
		labelEvidenceAnchor: "Evidence Anchor: ",
	},
	ZH: {
		overviewTitle:          "## 概览\n\n",
		overviewStartFmt:       "起始 %s",
		overviewFirstErrorFmt:  "首个错误标记 · Step %d · %s",
		overviewTransitionFmt:  "首个非常规转折（%s）· Step %d · %s",
		overviewEndFmt:         "结束 · Step %d · finish=%s · %s",
		tagToolIntensive:       "工具密集型",
		tagRetryHeavy:          "重试多",
		tagContextCompacted:    "上下文压缩",
		tagsLineFmt:            "**标签**：%s\n\n",
		overviewCostLineFmt:    "估算成本 ≈ %s（按标价估算，非实际账单）",
		overviewFailedStepsFmt: "%d/%d 步请求失败（HTTP/上游错误）",

		spineTitle:                   "## 决策脊柱\n\n",
		spineTaskLineFmt:             "**t%s · %s**\n\n",
		spineFindingTag:              " ⚠️",
		spineValueTruncatedFmt:       "\n… (+%d 字符已截断 — 完整值见本 Step 的详单)",
		spineResultValueTruncatedFmt: "\n… (+%d 字符已截断 — 完整值见下一步的详单)",
		spineDetailLinkFmt:           "→ [详情](%s)\n\n",
		spineDetailCoordFmt:          "→ `%s`\n\n",
		spineCoordNote:               "> 批量套件产出：各 Step 只标源坐标（`文件:行`）。运行 `vmr analyze -journey <id>` 生成带完整详单链接的单条报告。\n\n",

		spineInstructionLineFmt: "💬 指令 · %s\n\n",
		spineReportLineFmt:      "💬 汇报 · %s\n\n",
		spinePositionalMatch:    "（按位置推测，ID 未匹配）",

		cacheBreakUnexplainedFmt:  "Cache: 异常骤降 (%s→%s)",
		cacheBreakProviderSwitch:  "Cache: 服务端点切换",
		cacheBreakSystem:          "Cache: 系统提示词变更",
		cacheBreakTools:           "Cache: 工具定义变更",
		cacheBreakHistoryStitch:   "Cache: 跨会话缝合",
		cacheBreakHistoryContract: "Cache: 上下文压缩截断",
		cacheBreakHistoryFork:     "Cache: 上下文分叉",

		artifactsTitle:       "## 触达文件与资产\n\n",
		artifactsSummaryFmt:  "任务过程共记录 %d 个触达目标：\n\n",
		artifactsTableHeader: "| 目标路径 | 操作 | 首次步骤 | 频次 | 判定依据 |\n| :--- | :--- | :--- | :--- | :--- |\n",
		artifactsHeuristic:   "Shell 启发式",
		artifactsStructured:  "结构化参数",

		spineFinalDeliverableTitle:        "## 最终交付物\n\n",
		spineFinalDeliverableFoundFmt:     "Step %d · `%s`\n\n",
		spineFinalDeliverableExcerptLabel: "交付物节选",

		stepTagPlan:       "🔷 📋",
		stepTagAction:     "🔷 🔧",
		stepTagObserve:    "🔷 👀",
		stepTagRetry:      "🔷 🔄",
		stepTagError:      "🔷 ⚠️",
		stepTagCompaction: "🔷 🧹",
		stepTagReport:     "🔷 💬",

		timelineTitle:  "## 工具调用时序图\n\n",
		timelineLegend: "图例：● 正常 · 🔄 疑似重复 · ❌ 本步含错误标记\n\n",
		timelineNoData: "（本 Journey 未出现工具调用）\n\n",

		findingsTitle:            "## 疑似问题（候选清单，不是判决）\n\n",
		findingsNone:             "未检测到规则可判定的疑似问题。\n\n",
		findingGroupTitleFmt:     "### %s · 命中 %d 条（最早 Step %d）\n\n",
		findingHeaderFmt:         "%d. **%s** · Step %d\n",
		findingRelatedFmt:        "   - 相关 Step：%s\n",
		findingEvidenceFmt:       "   - 证据：%s\n",
		findingActionFmt:         "   - 建议：%s\n",
		anthropicOnlyCoverageFmt: "> ⚠️ 本 journey 全部请求均为非 Anthropic Messages 协议。以下信号依赖仅 Anthropic Messages 协议才会填充的字段，在本 journey 上结构性无法触发——未出现不代表检查过没问题：%s\n\n",

		badgeLLMInferredFmt: " [AI推测 · 置信度: %s]",
		badgeRuleDetected:   " [规则检测]",
		labelEvidenceAnchor: "原文证据锚点：",
	},
}

func Spine(lang Lang) SpineText {
	r := spineRows.Row(lang)
	return SpineText{
		OverviewTitle:      r.overviewTitle,
		OverviewStart:      func(ts string) string { return fmt.Sprintf(r.overviewStartFmt, ts) },
		OverviewFirstError: func(seq int, ts string) string { return fmt.Sprintf(r.overviewFirstErrorFmt, seq, ts) },
		OverviewTransition: func(seq int, kind, ts string) string {
			return fmt.Sprintf(r.overviewTransitionFmt, kind, seq, ts)
		},
		OverviewEnd: func(seq int, finish, ts string) string {
			return fmt.Sprintf(r.overviewEndFmt, seq, finish, ts)
		},
		TagToolIntensive:    r.tagToolIntensive,
		TagRetryHeavy:       r.tagRetryHeavy,
		TagContextCompacted: r.tagContextCompacted,
		TagsLine:            func(tags string) string { return fmt.Sprintf(r.tagsLineFmt, tags) },
		OverviewCostLine:    func(money string) string { return fmt.Sprintf(r.overviewCostLineFmt, money) },
		OverviewFailedStepsLine: func(failed, total int) string {
			return fmt.Sprintf(r.overviewFailedStepsFmt, failed, total)
		},

		SpineTitle: r.spineTitle,
		SpineTaskLine: func(idx int, title string) string {
			return fmt.Sprintf(r.spineTaskLineFmt, pad2(idx), title)
		},
		SpineFindingTag: r.spineFindingTag,
		SpineValueTruncated: func(more int) string {
			return fmt.Sprintf(r.spineValueTruncatedFmt, more)
		},
		SpineResultValueTruncated: func(more int) string {
			return fmt.Sprintf(r.spineResultValueTruncatedFmt, more)
		},
		SpineDetailLink:  func(relPath string) string { return fmt.Sprintf(r.spineDetailLinkFmt, relPath) },
		SpineDetailCoord: func(coord string) string { return fmt.Sprintf(r.spineDetailCoordFmt, coord) },
		SpineCoordNote:   r.spineCoordNote,

		SpineInstructionLine: func(text string) string { return fmt.Sprintf(r.spineInstructionLineFmt, text) },
		SpineReportLine:      func(text string) string { return fmt.Sprintf(r.spineReportLineFmt, text) },
		SpinePositionalMatch: r.spinePositionalMatch,
		CacheBreakBadge: func(kind string, from, to string) string {
			switch kind {
			case "unexplained":
				return fmt.Sprintf(r.cacheBreakUnexplainedFmt, from, to)
			case "provider_switch":
				return r.cacheBreakProviderSwitch
			case "system":
				return r.cacheBreakSystem
			case "tools":
				return r.cacheBreakTools
			case "history:stitch":
				return r.cacheBreakHistoryStitch
			case "history:contract":
				return r.cacheBreakHistoryContract
			case "history:fork":
				return r.cacheBreakHistoryFork
			default:
				return ""
			}
		},

		ArtifactsTitle:       r.artifactsTitle,
		ArtifactsSummary:     func(count int) string { return fmt.Sprintf(r.artifactsSummaryFmt, count) },
		ArtifactsTableHeader: r.artifactsTableHeader,
		ArtifactsHeuristic:   r.artifactsHeuristic,
		ArtifactsStructured:  r.artifactsStructured,

		SpineFinalDeliverableTitle: r.spineFinalDeliverableTitle,
		SpineFinalDeliverableFound: func(stepSeq int, toolName string) string {
			return fmt.Sprintf(r.spineFinalDeliverableFoundFmt, stepSeq, toolName)
		},
		SpineFinalDeliverableExcerptLabel: r.spineFinalDeliverableExcerptLabel,

		StepTagPlan:       r.stepTagPlan,
		StepTagAction:     r.stepTagAction,
		StepTagObserve:    r.stepTagObserve,
		StepTagRetry:      r.stepTagRetry,
		StepTagError:      r.stepTagError,
		StepTagCompaction: r.stepTagCompaction,
		StepTagReport:     r.stepTagReport,

		TimelineTitle:  r.timelineTitle,
		TimelineLegend: r.timelineLegend,
		TimelineNoData: r.timelineNoData,

		FindingsTitle: r.findingsTitle,
		FindingsNone:  r.findingsNone,
		FindingGroupTitle: func(code string, count, firstStep int) string {
			return fmt.Sprintf(r.findingGroupTitleFmt, code, count, firstStep)
		},
		FindingHeader: func(idx int, code string, stepSeq int) string {
			return fmt.Sprintf(r.findingHeaderFmt, idx, code, stepSeq)
		},
		FindingRelated:  func(seqs string) string { return fmt.Sprintf(r.findingRelatedFmt, seqs) },
		FindingEvidence: func(text string) string { return fmt.Sprintf(r.findingEvidenceFmt, text) },
		FindingAction:   func(text string) string { return fmt.Sprintf(r.findingActionFmt, text) },
		AnthropicOnlyCoverageNote: func(codes string) string {
			return fmt.Sprintf(r.anthropicOnlyCoverageFmt, codes)
		},

		BadgeLLMInferred:    func(confidence string) string { return fmt.Sprintf(r.badgeLLMInferredFmt, confidence) },
		BadgeRuleDetected:   r.badgeRuleDetected,
		LabelEvidenceAnchor: r.labelEvidenceAnchor,
	}
}
