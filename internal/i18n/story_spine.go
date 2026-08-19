// Ver 2026-08-05, by Sonnet 5

// Pairs with internal/story/render_spine.go — the decision-spine layer
// (a 3-second overview card, a compact per-Task action
// list, per-Step role tags, an optional tool-call timeline) added on top
// of render_md.go's existing fact-layer renderer. All of it is pure
// formatting over data render_md.go's renderStep already has; only the
// Findings section's text comes from story_findings.go.
package i18n

import "strconv"

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

	SpineTitle          string
	SpineTaskLine       func(idx int, title string) string
	SpineFindingTag     string                // appended to a spine Step header that hit a Finding
	SpineValueTruncated func(more int) string // appended to a tool-call payload block capped at spineFullCap

	SpineInstructionLine func(text string) string // a mid-task Step whose opening carries a new user instruction
	SpineReportLine      func(text string) string // a non-tool-calling Step's plain report/reasoning one-liner
	SpinePositionalMatch string                   // appended to a tool result paired by position, not id (level 3)

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

	FindingsTitle   string
	FindingsNone    string
	FindingHeader   func(idx int, code string, stepSeq int) string
	FindingRelated  func(seqs string) string
	FindingEvidence func(text string) string
	FindingAction   func(text string) string

	BadgeLLMInferred    func(confidence string) string
	BadgeRuleDetected   string
	LabelEvidenceAnchor string
}

func Spine(lang Lang) SpineText {
	if lang == ZH {
		return SpineText{
			OverviewTitle: "## 概览\n\n",
			OverviewStart: func(ts string) string { return "起始 " + ts },
			OverviewFirstError: func(seq int, ts string) string {
				return "首个错误标记 · Step " + strconv.Itoa(seq) + " · " + ts
			},
			OverviewTransition: func(seq int, kind, ts string) string {
				return "首个非常规转折（" + kind + "）· Step " + strconv.Itoa(seq) + " · " + ts
			},
			OverviewEnd: func(seq int, finish, ts string) string {
				return "结束 · Step " + strconv.Itoa(seq) + " · finish=" + finish + " · " + ts
			},
			TagToolIntensive:    "工具密集型",
			TagRetryHeavy:       "重试多",
			TagContextCompacted: "上下文压缩",
			TagsLine:            func(tags string) string { return "**标签**：" + tags + "\n\n" },

			SpineTitle:      "## 决策脊柱\n\n",
			SpineTaskLine:   func(idx int, title string) string { return "**t" + pad2(idx) + " · " + title + "**\n\n" },
			SpineFindingTag: " ⚠️",
			SpineValueTruncated: func(more int) string {
				return "\n… (+" + strconv.Itoa(more) + " 字符已截断 — 完整值见下方该 Step 的 tool_call 正文)"
			},

			SpineInstructionLine: func(text string) string { return "💬 指令 · " + text + "\n\n" },
			SpineReportLine:      func(text string) string { return "💬 汇报 · " + text + "\n\n" },
			SpinePositionalMatch: "（按位置推测，ID 未匹配）",

			SpineFinalDeliverableTitle: "## 最终交付物\n\n",
			SpineFinalDeliverableFound: func(stepSeq int, toolName string) string {
				return "Step " + strconv.Itoa(stepSeq) + " · `" + toolName + "`\n\n"
			},
			SpineFinalDeliverableExcerptLabel: "交付物节选",

			StepTagPlan:       "🔷 📋",
			StepTagAction:     "🔷 🔧",
			StepTagObserve:    "🔷 👀",
			StepTagRetry:      "🔷 🔄",
			StepTagError:      "🔷 ⚠️",
			StepTagCompaction: "🔷 🧹",
			StepTagReport:     "🔷 💬",

			TimelineTitle:  "## 工具调用时序图\n\n",
			TimelineLegend: "图例：● 正常 · 🔄 疑似重复 · ❌ 本步含错误标记\n\n",
			TimelineNoData: "（本 Journey 未出现工具调用）\n\n",

			FindingsTitle: "## 疑似问题（候选清单，不是判决）\n\n",
			FindingsNone:  "未检测到规则可判定的疑似问题。\n\n",
			FindingHeader: func(idx int, code string, stepSeq int) string {
				return strconv.Itoa(idx) + ". **" + code + "** · Step " + strconv.Itoa(stepSeq) + "\n"
			},
			FindingRelated:  func(seqs string) string { return "   - 相关 Step：" + seqs + "\n" },
			FindingEvidence: func(text string) string { return "   - 证据：" + text + "\n" },
			FindingAction:   func(text string) string { return "   - 建议：" + text + "\n" },

			BadgeLLMInferred: func(confidence string) string {
				return " [AI推测 · 置信度: " + confidence + "]"
			},
			BadgeRuleDetected:   " [规则检测]",
			LabelEvidenceAnchor: "原文证据锚点：",
		}
	}
	return SpineText{
		OverviewTitle: "## Overview\n\n",
		OverviewStart: func(ts string) string { return "Started " + ts },
		OverviewFirstError: func(seq int, ts string) string {
			return "First error marker · Step " + strconv.Itoa(seq) + " · " + ts
		},
		OverviewTransition: func(seq int, kind, ts string) string {
			return "First non-routine transition (" + kind + ") · Step " + strconv.Itoa(seq) + " · " + ts
		},
		OverviewEnd: func(seq int, finish, ts string) string {
			return "Ended · Step " + strconv.Itoa(seq) + " · finish=" + finish + " · " + ts
		},
		TagToolIntensive:    "tool-intensive",
		TagRetryHeavy:       "retry-heavy",
		TagContextCompacted: "context-compacted",
		TagsLine:            func(tags string) string { return "**Tags**: " + tags + "\n\n" },

		SpineTitle:      "## Decision Spine\n\n",
		SpineTaskLine:   func(idx int, title string) string { return "**t" + pad2(idx) + " · " + title + "**\n\n" },
		SpineFindingTag: " ⚠️",
		SpineValueTruncated: func(more int) string {
			return "\n… (+" + strconv.Itoa(more) + " more chars — full value in this Step's tool_call section below)"
		},

		SpineInstructionLine: func(text string) string { return "💬 Instruction · " + text + "\n\n" },
		SpineReportLine:      func(text string) string { return "💬 Report · " + text + "\n\n" },
		SpinePositionalMatch: " (matched by position — ID unmatched)",

		SpineFinalDeliverableTitle: "## Final Deliverable\n\n",
		SpineFinalDeliverableFound: func(stepSeq int, toolName string) string {
			return "Step " + strconv.Itoa(stepSeq) + " · `" + toolName + "`\n\n"
		},
		SpineFinalDeliverableExcerptLabel: "Deliverable excerpt",

		StepTagPlan:       "🔷 📋",
		StepTagAction:     "🔷 🔧",
		StepTagObserve:    "🔷 👀",
		StepTagRetry:      "🔷 🔄",
		StepTagError:      "🔷 ⚠️",
		StepTagCompaction: "🔷 🧹",
		StepTagReport:     "🔷 💬",

		TimelineTitle:  "## Tool Call Timeline\n\n",
		TimelineLegend: "Legend: ● normal · 🔄 suspected repeat · ❌ step carries an error marker\n\n",
		TimelineNoData: "(no tool calls in this Journey)\n\n",

		FindingsTitle: "## Suspected Issues (candidate list, not a verdict)\n\n",
		FindingsNone:  "No rule-detectable suspected issues.\n\n",
		FindingHeader: func(idx int, code string, stepSeq int) string {
			return strconv.Itoa(idx) + ". **" + code + "** · Step " + strconv.Itoa(stepSeq) + "\n"
		},
		FindingRelated:  func(seqs string) string { return "   - related Steps: " + seqs + "\n" },
		FindingEvidence: func(text string) string { return "   - evidence: " + text + "\n" },
		FindingAction:   func(text string) string { return "   - action: " + text + "\n" },

		BadgeLLMInferred: func(confidence string) string {
			return " [AI Inferred · " + confidence + "]"
		},
		BadgeRuleDetected:   " [Rule-detected]",
		LabelEvidenceAnchor: "Evidence Anchor: ",
	}
}
