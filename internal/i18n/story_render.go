// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/story/render_md.go (journey-*.md) and the three
// fallback title strings in internal/story/journey.go (toolLoopTitle,
// stitchTaskTitle, deriveTitle's placeholder) — Journey/Task titles are
// mostly a verbatim quote of the user's own message and so aren't localized
// (see the design doc's "structured vs narrative field" boundary), but the
// handful of *fallback* placeholders used when there's no real instruction
// to quote are vmr's own generated text and do need it.
package i18n

import "strconv"

// StoryText is render_md.go's (plus journey.go's fallback titles') text, in
// one language.
type StoryText struct {
	ListSep             string // joins e.g. swallowed/survived entity lists ("a、b" vs "a, b")
	JourneyMeta         func(tasks, turns int, from, to string) string
	BreakWarning        func(kind, reasonHint, statsHint string) string
	BreakReasonContract string
	BreakReasonFork     string
	BreakReasonDefault  string
	EditStatsHint       func(lcp int, coveragePct float64) string
	EditLine            func(kind, statsHint string) string
	StitchLine          func(kind, scorePct, confPct string) string
	SysChangedLine      string
	NoReplyLine         string

	SysPromptHeaderTitle   string
	SysPromptHeaderChanged func(eras int) string
	SysPromptEraSummary    func(fromSeq, toSeq, chars int) string

	ReasoningSummary func(chars int) string
	ReplySummary     func(preview string) string
	EmptyEvent       func(head string) string
	RevisionMarker   func(hash string) string

	CompactionSummary func(before, after, ratio string, swallowed, survived int) string
	SwallowedEntities func(entities string) string
	SurvivedEntities  func(entities string) string

	// journey.go fallback titles
	ToolLoopTitle     string
	NoTitle           string
	StitchedTaskTitle func(kind, scorePct string) string

	// preview.go fallback title (record couldn't be refetched)
	UnreadableTitle string
}

func Story(lang Lang) StoryText {
	if lang == ZH {
		return StoryText{
			ListSep: "、",
			JourneyMeta: func(tasks, turns int, from, to string) string {
				return "> " + strconv.Itoa(tasks) + " 任务 · " + strconv.Itoa(turns) + " 轮 · " + from + " → " + to + "\n\n"
			},
			BreakWarning: func(kind, reasonHint, statsHint string) string {
				return "> ⚠️ **本 journey 的开头是从上一段上下文断裂而来**（" + kind + "：" + reasonHint + "；" + statsHint +
					"）——已尝试自动缝合到更早的片段，但没有找到证据充分的前驱（覆盖率/置信度不够，或确认没有前驱），两段之间的关系仍未确认，只如实标出断点，不强行缝合（宁可断开，不要错连）。\n\n"
			},
			BreakReasonContract: "上下文被大幅收缩（截断/重建）",
			BreakReasonFork:     "内容与前段几乎不重叠（可能是同一 anchor 下的另一次对话）",
			BreakReasonDefault:  "结构性断裂",
			EditStatsHint: func(lcp int, coveragePct float64) string {
				return "最长相同前缀 " + strconv.Itoa(lcp) + " 条消息，内容重合率 " + fmtPct0(coveragePct) + "%"
			},
			EditLine: func(kind, statsHint string) string { return "> 编辑: " + kind + "（" + statsHint + "）\n\n" },
			StitchLine: func(kind, scorePct, confPct string) string {
				return "> 🧵 **缝合自更早片段**（" + kind + "，覆盖率 " + scorePct + "，置信度 " + confPct +
					"）——这一段与上一段之间发生过一次结构性断裂，已根据内容重合证据自动重新接上；证据如实保留，供核实。\n\n"
			},
			SysChangedLine: "> ⚙️ **system prompt 变更**（换模型 / 换工具集 / 平台注入变化，原因未知，如实标出）\n\n",
			NoReplyLine:    "- ⏭️ **本轮 LLM 未实际回复**（NO_REPLY 或空内容）——下一轮可能是重试\n\n",

			SysPromptHeaderTitle: "## System Prompt\n\n",
			SysPromptHeaderChanged: func(eras int) string {
				return "> ⚙️ 全程共出现 " + strconv.Itoa(eras) + " 版不同的 system prompt（下方按出现顺序分别列出，每版只展示一次）\n\n"
			},
			SysPromptEraSummary: func(fromSeq, toSeq, chars int) string {
				rangeLabel := "Step " + strconv.Itoa(fromSeq)
				if toSeq > fromSeq {
					rangeLabel += "–" + strconv.Itoa(toSeq)
				}
				return rangeLabel + " · " + strconv.Itoa(chars) + " 字符"
			},

			ReasoningSummary: func(chars int) string { return "🤔 reasoning · " + strconv.Itoa(chars) + " 字符" },
			ReplySummary:     func(preview string) string { return "💬 回复 · " + preview },
			EmptyEvent:       func(head string) string { return head + " (空)\n\n" },
			RevisionMarker:   func(hash string) string { return " 🔄[修订 " + hash + "]" },

			CompactionSummary: func(before, after, ratio string, swallowed, survived int) string {
				return "📉 信息损失: " + before + " → " + after + " tokens（" + ratio + "）· " + strconv.Itoa(swallowed) + " 个实体消失 / " + strconv.Itoa(survived) + " 个存活"
			},
			SwallowedEntities: func(entities string) string {
				return "**消失的实体**（在更早片段里提到过，这一步没再提到——规则粗筛，不代表真的不再相关）：" + entities + "\n\n"
			},
			SurvivedEntities: func(entities string) string { return "**仍然存活的实体**：" + entities + "\n\n" },

			ToolLoopTitle:   "(工具循环延续)",
			NoTitle:         "(无标题)",
			UnreadableTitle: "(无法读取)",
			StitchedTaskTitle: func(kind, scorePct string) string {
				return "(缝合自更早片段 · " + kind + "，覆盖率 " + scorePct + ")"
			},
		}
	}
	return StoryText{
		ListSep: ", ",
		JourneyMeta: func(tasks, turns int, from, to string) string {
			return "> " + strconv.Itoa(tasks) + " tasks · " + strconv.Itoa(turns) + " turns · " + from + " → " + to + "\n\n"
		},
		BreakWarning: func(kind, reasonHint, statsHint string) string {
			return "> ⚠️ **This journey's start is broken off from an earlier context** (" + kind + ": " + reasonHint + "; " + statsHint +
				") — automatic stitching to an earlier fragment was attempted but no predecessor with strong enough evidence was found (coverage/confidence too low, or confirmed none exists). The relationship between the two segments remains unresolved; this only marks the break honestly rather than forcing a connection (better to leave it broken than stitch it wrong).\n\n"
		},
		BreakReasonContract: "context was sharply contracted (truncated/rebuilt)",
		BreakReasonFork:     "content barely overlaps with the previous segment (possibly another conversation under the same anchor)",
		BreakReasonDefault:  "structural break",
		EditStatsHint: func(lcp int, coveragePct float64) string {
			return "longest common prefix " + strconv.Itoa(lcp) + " messages, content overlap " + fmtPct0(coveragePct) + "%"
		},
		EditLine: func(kind, statsHint string) string { return "> Edit: " + kind + " (" + statsHint + ")\n\n" },
		StitchLine: func(kind, scorePct, confPct string) string {
			return "> 🧵 **Stitched from an earlier fragment** (" + kind + ", coverage " + scorePct + ", confidence " + confPct +
				") — a structural break occurred between this segment and the previous one; it has been automatically reconnected based on content-overlap evidence, kept here as-is for verification.\n\n"
		},
		SysChangedLine: "> ⚙️ **System prompt changed** (model switch / toolset switch / platform injection change, reason unknown, marked as observed)\n\n",
		NoReplyLine:    "- ⏭️ **No actual LLM response this turn** (NO_REPLY or empty content) — the next turn may be a retry\n\n",

		SysPromptHeaderTitle: "## System Prompt\n\n",
		SysPromptHeaderChanged: func(eras int) string {
			return "> ⚙️ " + strconv.Itoa(eras) + " distinct system prompt versions appeared over this Journey (listed below in order of first appearance, each shown once)\n\n"
		},
		SysPromptEraSummary: func(fromSeq, toSeq, chars int) string {
			rangeLabel := "Step " + strconv.Itoa(fromSeq)
			if toSeq > fromSeq {
				rangeLabel += "–" + strconv.Itoa(toSeq)
			}
			return rangeLabel + " · " + strconv.Itoa(chars) + " chars"
		},

		ReasoningSummary: func(chars int) string { return "🤔 reasoning · " + strconv.Itoa(chars) + " chars" },
		ReplySummary:     func(preview string) string { return "💬 reply · " + preview },
		EmptyEvent:       func(head string) string { return head + " (empty)\n\n" },
		RevisionMarker:   func(hash string) string { return " 🔄[revises " + hash + "]" },

		CompactionSummary: func(before, after, ratio string, swallowed, survived int) string {
			return "📉 Information loss: " + before + " → " + after + " tokens (" + ratio + ") · " + strconv.Itoa(swallowed) + " entities disappeared / " + strconv.Itoa(survived) + " survived"
		},
		SwallowedEntities: func(entities string) string {
			return "**Disappeared entities** (mentioned in an earlier fragment, not mentioned again here — a rule-based rough scan, not proof they no longer matter): " + entities + "\n\n"
		},
		SurvivedEntities: func(entities string) string { return "**Still-surviving entities**: " + entities + "\n\n" },

		ToolLoopTitle:   "(tool loop continuation)",
		NoTitle:         "(untitled)",
		UnreadableTitle: "(unreadable)",
		StitchedTaskTitle: func(kind, scorePct string) string {
			return "(stitched from an earlier fragment · " + kind + ", coverage " + scorePct + ")"
		},
	}
}
