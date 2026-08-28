// Ver 2026-08-28, by Sonnet 5

// Pairs with internal/story/render_html.go + render_html_dashboard.go — the
// single-page HTML journey dashboard (`vmr analyze -journey <id> -html`).
// Fixed UI strings only; every number and every piece of conversation
// content comes from the Journey itself (or, under -redact, a length
// placeholder or a bare count).
package i18n

import "fmt"

// StoryHTMLText is the journey dashboard's chrome, in one language.
type StoryHTMLText struct {
	DocTitle       func(id string) string
	Subtitle       func(tasks, steps int, from, to string) string
	PartialBanner  string
	BreakBanner    func(kind string) string
	RedactedBanner string
	GeneratedNote  string
	Empty          string
	RedactedText   func(runes int) string

	// Verdict (first screen). OutcomeDeliverable is a prefix — the renderer
	// appends the tool name as <code> so it reads as code, not backticks.
	OutcomeLabel       string
	OutcomeDeliverable func(step int) string
	OutcomeTermination func(finish string) string
	OutcomeUnknown     string

	// Section headings + left-rail labels
	SectionStructure string
	SectionMetrics   string
	SectionFindings  string
	TaskLabel        func(n int) string
	StepLabel        func(n int) string

	// Structure timeline
	StructNoSteps   string
	MoreTools       func(n int) string // "+3" overflow chip
	Attempts        func(n int) string
	FailoverBadge   string
	SysChanged      string
	EditLabel       func(kind string) string
	StitchLabel     func(kind string, score, confidence string) string
	CompactionShort func(before, after int64) string
	EntityCounts    func(swallowed, survived int) string // redact-mode compaction summary
	Swallowed       string
	Survived        string
	Instruction     string
	StepSaid        string
	NoReply         string

	// Metrics
	MetricNetTime       string
	MetricModelTime     string
	MetricAgentTime     string
	MetricTokensIn      string
	MetricTokensCached  string
	MetricTokensOut     string
	MetricToolCalls     string
	MetricCompactions   string
	MetricDupActionRate string
	MetricOutputRepeat  string
	MetricErrorRecovery string
	MetricPlanExecRatio string
	MetricModelSwitches string
	MetricContextUtil   string
	SparkContextTitle   string
	SparkContextCaption func(firstTok, lastTok int64) string

	// Findings
	FindingSourceRule    string
	FindingSourceLLM     string
	FindingConfidence    func(level string) string
	FindingAtStep        func(seq int) string
	FindingRelatedSteps  func(seqs string) string
	FindingEvidenceLabel string
	FindingActionLabel   string
	NoFindings           string
	RedactedFindingsNote string
}

// StoryHTML returns the dashboard chrome text for lang.
func StoryHTML(lang Lang) StoryHTMLText {
	if lang == ZH {
		return StoryHTMLText{
			DocTitle: func(id string) string { return "Journey " + id },
			Subtitle: func(t, s int, from, to string) string {
				return fmt.Sprintf("%d 个任务 · %d 个步骤 · %s – %s", t, s, from, to)
			},
			PartialBanner: "此 Journey 的开头被所加载的文件范围截断，展示的是可见部分。",
			BreakBanner: func(k string) string {
				return "此 Journey 的开头是一个未解析的断裂（" + k + "）——找不到更早的前驱。"
			},
			RedactedBanner: "脱敏模式：结构、指标、角色、token 数保留；对话正文替换为长度占位，逐步详单链接与 Findings 文本已隐去。",
			GeneratedNote:  "由 vmr analyze 生成 · 单文件自包含 · 零外部依赖",
			Empty:          "（空）",
			RedactedText:   func(n int) string { return fmt.Sprintf("‹text: %d chars›", n) },

			OutcomeLabel:       "结局",
			OutcomeDeliverable: func(step int) string { return fmt.Sprintf("Step %d 产出最终文件，工具", step) },
			OutcomeTermination: func(finish string) string { return "终止：finish=" + finish },
			OutcomeUnknown:     "终止方式未知",

			SectionStructure: "结构",
			SectionMetrics:   "指标",
			SectionFindings:  "疑似问题",
			TaskLabel:        func(n int) string { return fmt.Sprintf("任务 %d", n) },
			StepLabel:        func(n int) string { return fmt.Sprintf("步骤 %d", n) },

			StructNoSteps: "（无步骤）",
			MoreTools:     func(n int) string { return fmt.Sprintf("+%d", n) },
			Attempts:      func(n int) string { return fmt.Sprintf("%d 次尝试", n) },
			FailoverBadge: "failover",
			SysChanged:    "系统提示词变化",
			EditLabel:     func(k string) string { return "上下文编辑：" + k },
			StitchLabel: func(k, sc, cf string) string {
				return fmt.Sprintf("缝合 %s（覆盖率 %s，置信度 %s）", k, sc, cf)
			},
			CompactionShort: func(b, a int64) string { return fmt.Sprintf("压缩 %d→%d tok", b, a) },
			EntityCounts:    func(sw, su int) string { return fmt.Sprintf("压缩后：%d 个实体消失、%d 个保留", sw, su) },
			Swallowed:       "压缩后不再出现",
			Survived:        "压缩后仍保留",
			Instruction:     "指令",
			StepSaid:        "本步模型说了什么",
			NoReply:         "本轮模型按约定未回复",

			MetricNetTime:       "净工作时长",
			MetricModelTime:     "模型时长",
			MetricAgentTime:     "Agent 执行时长",
			MetricTokensIn:      "输入 token",
			MetricTokensCached:  "其中缓存命中",
			MetricTokensOut:     "输出 token",
			MetricToolCalls:     "工具调用",
			MetricCompactions:   "压缩次数",
			MetricDupActionRate: "重复动作率",
			MetricOutputRepeat:  "输出重复率",
			MetricErrorRecovery: "错误恢复次数",
			MetricPlanExecRatio: "纯规划轮占比",
			MetricModelSwitches: "模型切换次数",
			MetricContextUtil:   "上下文利用率",
			SparkContextTitle:   "每步上下文 token",
			SparkContextCaption: func(f, l int64) string { return fmt.Sprintf("%d → %d tok", f, l) },

			FindingSourceRule:    "规则",
			FindingSourceLLM:     "AI 推测",
			FindingConfidence:    func(l string) string { return "置信度 " + l },
			FindingAtStep:        func(seq int) string { return fmt.Sprintf("Step %d", seq) },
			FindingRelatedSteps:  func(seqs string) string { return "相关 Step：" + seqs },
			FindingEvidenceLabel: "证据",
			FindingActionLabel:   "建议",
			NoFindings:           "未命中任何行为指标。",
			RedactedFindingsNote: "脱敏模式：仅列出 Finding 代码与 Step 锚点，叙述文本与证据片段已隐去。",
		}
	}
	return StoryHTMLText{
		DocTitle: func(id string) string { return "Journey " + id },
		Subtitle: func(t, s int, from, to string) string {
			return fmt.Sprintf("%d tasks · %d steps · %s – %s", t, s, from, to)
		},
		PartialBanner: "This journey's beginning is truncated by the loaded file range; only the visible part is shown.",
		BreakBanner: func(k string) string {
			return "This journey opens on an unresolved break (" + k + ") — no earlier predecessor was found."
		},
		RedactedBanner: "Redacted: structure, metrics, roles and token counts kept; conversation bodies are length placeholders, per-step detail links and finding text are removed.",
		GeneratedNote:  "Generated by vmr analyze · single self-contained file · zero external dependencies",
		Empty:          "(empty)",
		RedactedText:   func(n int) string { return fmt.Sprintf("‹text: %d chars›", n) },

		OutcomeLabel:       "Outcome",
		OutcomeDeliverable: func(step int) string { return fmt.Sprintf("Final file written at Step %d via", step) },
		OutcomeTermination: func(finish string) string { return "Ended: finish=" + finish },
		OutcomeUnknown:     "Termination mode unknown",

		SectionStructure: "Structure",
		SectionMetrics:   "Metrics",
		SectionFindings:  "Suspected issues",
		TaskLabel:        func(n int) string { return fmt.Sprintf("Task %d", n) },
		StepLabel:        func(n int) string { return fmt.Sprintf("Step %d", n) },

		StructNoSteps:   "(no steps)",
		MoreTools:       func(n int) string { return fmt.Sprintf("+%d", n) },
		Attempts:        func(n int) string { return fmt.Sprintf("%d attempts", n) },
		FailoverBadge:   "failover",
		SysChanged:      "system prompt changed",
		EditLabel:       func(k string) string { return "context edit: " + k },
		StitchLabel:     func(k, sc, cf string) string { return fmt.Sprintf("stitch %s (coverage %s, confidence %s)", k, sc, cf) },
		CompactionShort: func(b, a int64) string { return fmt.Sprintf("compaction %d→%d tok", b, a) },
		EntityCounts: func(sw, su int) string {
			return fmt.Sprintf("after compaction: %d entities dropped, %d survived", sw, su)
		},
		Swallowed:   "dropped after compaction",
		Survived:    "survived compaction",
		Instruction: "Instruction",
		StepSaid:    "What the model said this step",
		NoReply:     "The model deliberately did not reply this turn",

		MetricNetTime:       "Net working time",
		MetricModelTime:     "Model time",
		MetricAgentTime:     "Agent exec time",
		MetricTokensIn:      "Input tokens",
		MetricTokensCached:  "  of which cached",
		MetricTokensOut:     "Output tokens",
		MetricToolCalls:     "Tool calls",
		MetricCompactions:   "Compactions",
		MetricDupActionRate: "Duplicate-action rate",
		MetricOutputRepeat:  "Output repetition",
		MetricErrorRecovery: "Error recoveries",
		MetricPlanExecRatio: "Plan-only turn share",
		MetricModelSwitches: "Model switches",
		MetricContextUtil:   "Context utilization",
		SparkContextTitle:   "Context tokens per step",
		SparkContextCaption: func(f, l int64) string { return fmt.Sprintf("%d → %d tok", f, l) },

		FindingSourceRule:    "rule",
		FindingSourceLLM:     "AI inferred",
		FindingConfidence:    func(l string) string { return "confidence " + l },
		FindingAtStep:        func(seq int) string { return fmt.Sprintf("Step %d", seq) },
		FindingRelatedSteps:  func(seqs string) string { return "related Steps: " + seqs },
		FindingEvidenceLabel: "evidence",
		FindingActionLabel:   "action",
		NoFindings:           "No behavior indicators fired.",
		RedactedFindingsNote: "Redacted: finding codes and Step anchors only; narrative text and evidence excerpts removed.",
	}
}
