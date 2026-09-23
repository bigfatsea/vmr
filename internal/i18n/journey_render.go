// Ver 2026-09-23 08:10, by Claude Opus 5.5

// Pairs with internal/journey/viewmodel_build.go + viewmodel_spine.go
// (j-<id>.md via the ViewModel path, the only render path since the legacy
// in-memory renderer was removed in Phase 3). Journey/Task titles are
// mostly a verbatim quote of the user's own message and so were never
// localized (see the design doc's "structured vs narrative field"
// boundary). The handful of *fallback* placeholders used when there's no
// real instruction to quote used to localize too, until a real-corpus
// -lang diff caught them leaking into journeys/index.json — a JSON data
// product that must stay language-neutral (KNOWN_ISSUES' "language never enters data products" carrier
// table). Frozen to English (JourneyNoTitle etc., below) instead of moved
// to the Code+Params pattern ComputeFindings uses: unlike findings text,
// nothing ever re-localizes these at render time — the same string sits in
// Journey/Task.Title all the way to both journeys/index.json and the
// rendered Markdown, so a Table[T] row would just be dead ZH text no code
// path reads.
package i18n

import "fmt"

// Journey/Task title fallback placeholders (internal/journey's journey.go
// deriveTitle/stitchTaskTitle/titleAtStitchBoundary, preview.go
// titleFromRecord, clusters.go's anchor title) — see this file's package
// doc for why these are frozen to English rather than following -lang.
const (
	JourneyToolLoopTitle   = "(tool loop continuation)"
	JourneyNoTitle         = "(untitled)"
	JourneyUnreadableTitle = "(unreadable)"
)

// JourneyStitchedTaskTitle is the fallback Task title at a stitch boundary
// with no genuine new user instruction (stitchTaskTitle's caller).
func JourneyStitchedTaskTitle(kind, scorePct string) string {
	return fmt.Sprintf("(stitched from an earlier fragment · %s, coverage %s)", kind, scorePct)
}

// JourneyText is render_md.go's (plus journey.go's fallback titles') text, in
// one language.
type JourneyText struct {
	ListSep     string // joins e.g. swallowed/survived entity lists ("a、b" vs "a, b")
	JourneyMeta func(tasks, turns int, from, to string) string
	// PartialBanner is the ⚠️ line shown when a Journey is partial (its tail
	// lineage's continuation was never recorded). It moved here from the
	// retired StoryHTMLText (the self-contained dashboard's chrome) — the
	// Markdown banner and the dashboard banner were the same sentence.
	PartialBanner string
	// BackLinkLine is the "journey report → return" edge:
	// journeys/index.md (always, as ../index.md from journeys/details/) and,
	// when reportLink != "", vmr-report.md.
	BackLinkLine        func(reportLink string) string
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
	// SysPromptEraLink is one era's line when it has a system prompt: the
	// effective Step range, a char-count summary, and a link to the shared
	// evidence blob (full text no longer inlines here).
	SysPromptEraLink func(fromSeq, toSeq, chars int, relPath string) string
	// SysPromptEraCoord is the coordinate form of the same line, used by the
	// default batch suite where the evidence blob is not materialized
	// — names the blob and points at `vmr analyze -journey <id>`
	// rather than emitting a link that would 404 (guarded by the dead-link
	// check in cmd/vmr/cmd_analyze_test.go).
	SysPromptEraCoord func(fromSeq, toSeq, chars int, blobName string) string
	// SysPromptEraNoSys is the line for an era with no leading system
	// block at all (HasSys == false) — rendered as-is, not silently
	// skipped, so the effective-range list stays complete.
	SysPromptEraNoSys func(fromSeq, toSeq int) string

	CompactionSummary func(before, after, ratio string, swallowed, survived int) string
	SwallowedEntities func(entities string) string
	SurvivedEntities  func(entities string) string
}

// journeyStepRangeLabel renders a Step range ("Step 12" or "Step 12–15") —
// shared by the three SysPromptEra* closures below. Language-independent:
// "Step" is never translated in either language's existing text, so this
// isn't a row field, just a plain helper.
func journeyStepRangeLabel(fromSeq, toSeq int) string {
	if toSeq > fromSeq {
		return fmt.Sprintf("Step %d–%d", fromSeq, toSeq)
	}
	return fmt.Sprintf("Step %d", fromSeq)
}

// journeyRow holds this file's literal templates, one row per Lang (Table's
// own doc comment). BackLinkLine's "reportLink != """ branch is the only
// conditional, identical both languages, written once in Journey below.
type journeyRow struct {
	listSep             string
	partialBanner       string
	journeyMetaFmt      string
	backLinkPrefix      string
	backLinkReportFmt   string
	breakWarningFmt     string
	breakReasonContract string
	breakReasonFork     string
	breakReasonDefault  string
	editStatsHintFmt    string
	editLineFmt         string
	stitchLineFmt       string
	sysChangedLine      string
	noReplyLine         string

	sysPromptHeaderTitle      string
	sysPromptHeaderChangedFmt string
	sysPromptEraLinkFmt       string
	sysPromptEraCoordFmt      string
	sysPromptEraNoSysFmt      string

	compactionSummaryFmt string
	swallowedEntitiesFmt string
	survivedEntitiesFmt  string
}

var journeyRows = Table[journeyRow]{
	EN: {
		listSep:             ", ",
		partialBanner:       "This journey's beginning is truncated by the loaded file range; only the visible part is shown.",
		journeyMetaFmt:      "> %d tasks · %d turns · %s → %s\n\n",
		backLinkPrefix:      "← Back to [index.md](../index.md)",
		backLinkReportFmt:   " · [vmr-report.md](%s)",
		breakWarningFmt:     "> ⚠️ **This journey's start is broken off from an earlier context** (%s: %s; %s) — automatic stitching to an earlier fragment was attempted but no predecessor with strong enough evidence was found (coverage/confidence too low, or confirmed none exists). The relationship between the two segments remains unresolved; this only marks the break honestly rather than forcing a connection (better to leave it broken than stitch it wrong).\n\n",
		breakReasonContract: "context was sharply contracted (truncated/rebuilt)",
		breakReasonFork:     "content barely overlaps with the previous segment (possibly another conversation under the same anchor)",
		breakReasonDefault:  "structural break",
		editStatsHintFmt:    "longest common prefix %d messages, content overlap %s%%",
		editLineFmt:         "> Edit: %s (%s)\n\n",
		stitchLineFmt:       "> 🧵 **Stitched from an earlier fragment** (%s, coverage %s, confidence %s) — a structural break occurred between this segment and the previous one; it has been automatically reconnected based on content-overlap evidence, kept here as-is for verification.\n\n",
		sysChangedLine:      "> ⚙️ **System prompt changed** (model switch / toolset switch / platform injection change, reason unknown, marked as observed)\n\n",
		noReplyLine:         "- ⏭️ **No actual LLM response this turn** (NO_REPLY or empty content) — the next turn may be a retry\n\n",

		sysPromptHeaderTitle:      "## System Prompt\n\n",
		sysPromptHeaderChangedFmt: "> ⚙️ %d distinct system prompt versions appeared over this Journey (listed below in order of first appearance, each shown once)\n\n",
		sysPromptEraLinkFmt:       "- %s · %d chars · → [detail](%s)\n",
		sysPromptEraCoordFmt:      "- %s · %d chars · `%s` (see `vmr analyze -journey <id>`)\n",
		sysPromptEraNoSysFmt:      "- %s · (no system prompt)\n",

		compactionSummaryFmt: "📉 Information loss: %s → %s tokens (%s) · %d entities disappeared / %d survived",
		swallowedEntitiesFmt: "**Disappeared entities** (mentioned in an earlier fragment, not mentioned again here — a rule-based rough scan, not proof they no longer matter): %s\n\n",
		survivedEntitiesFmt:  "**Still-surviving entities**: %s\n\n",
	},
	ZH: {
		listSep:             "、",
		partialBanner:       "此 Journey 的开头被所加载的文件范围截断，展示的是可见部分。",
		journeyMetaFmt:      "> %d 任务 · %d 轮 · %s → %s\n\n",
		backLinkPrefix:      "← 返回 [index.md](../index.md)",
		backLinkReportFmt:   " · [vmr-report.md](%s)",
		breakWarningFmt:     "> ⚠️ **本 journey 的开头是从上一段上下文断裂而来**（%s：%s；%s）——已尝试自动缝合到更早的片段，但没有找到证据充分的前驱（覆盖率/置信度不够，或确认没有前驱），两段之间的关系仍未确认，只如实标出断点，不强行缝合（宁可断开，不要错连）。\n\n",
		breakReasonContract: "上下文被大幅收缩（截断/重建）",
		breakReasonFork:     "内容与前段几乎不重叠（可能是同一 anchor 下的另一次对话）",
		breakReasonDefault:  "结构性断裂",
		editStatsHintFmt:    "最长相同前缀 %d 条消息，内容重合率 %s%%",
		editLineFmt:         "> 编辑: %s（%s）\n\n",
		stitchLineFmt:       "> 🧵 **缝合自更早片段**（%s，覆盖率 %s，置信度 %s）——这一段与上一段之间发生过一次结构性断裂，已根据内容重合证据自动重新接上；证据如实保留，供核实。\n\n",
		sysChangedLine:      "> ⚙️ **system prompt 变更**（换模型 / 换工具集 / 平台注入变化，原因未知，如实标出）\n\n",
		noReplyLine:         "- ⏭️ **本轮 LLM 未实际回复**（NO_REPLY 或空内容）——下一轮可能是重试\n\n",

		sysPromptHeaderTitle:      "## System Prompt\n\n",
		sysPromptHeaderChangedFmt: "> ⚙️ 全程共出现 %d 版不同的 system prompt（下方按出现顺序分别列出，每版只展示一次）\n\n",
		sysPromptEraLinkFmt:       "- %s · %d 字符 · → [详情](%s)\n",
		sysPromptEraCoordFmt:      "- %s · %d 字符 · `%s`（见 `vmr analyze -journey <id>`）\n",
		sysPromptEraNoSysFmt:      "- %s ·（无 system prompt）\n",

		compactionSummaryFmt: "📉 信息损失: %s → %s tokens（%s）· %d 个实体消失 / %d 个存活",
		swallowedEntitiesFmt: "**消失的实体**（在更早片段里提到过，这一步没再提到——规则粗筛，不代表真的不再相关）：%s\n\n",
		survivedEntitiesFmt:  "**仍然存活的实体**：%s\n\n",
	},
}

func Journey(lang Lang) JourneyText {
	r := journeyRows.Row(lang)
	return JourneyText{
		ListSep:       r.listSep,
		PartialBanner: r.partialBanner,
		JourneyMeta: func(tasks, turns int, from, to string) string {
			return fmt.Sprintf(r.journeyMetaFmt, tasks, turns, from, to)
		},
		BackLinkLine: func(reportLink string) string {
			s := r.backLinkPrefix
			if reportLink != "" {
				s += fmt.Sprintf(r.backLinkReportFmt, reportLink)
			}
			return s + "\n\n"
		},
		BreakWarning: func(kind, reasonHint, statsHint string) string {
			return fmt.Sprintf(r.breakWarningFmt, kind, reasonHint, statsHint)
		},
		BreakReasonContract: r.breakReasonContract,
		BreakReasonFork:     r.breakReasonFork,
		BreakReasonDefault:  r.breakReasonDefault,
		EditStatsHint: func(lcp int, coveragePct float64) string {
			return fmt.Sprintf(r.editStatsHintFmt, lcp, fmtPct0(coveragePct))
		},
		EditLine: func(kind, statsHint string) string { return fmt.Sprintf(r.editLineFmt, kind, statsHint) },
		StitchLine: func(kind, scorePct, confPct string) string {
			return fmt.Sprintf(r.stitchLineFmt, kind, scorePct, confPct)
		},
		SysChangedLine: r.sysChangedLine,
		NoReplyLine:    r.noReplyLine,

		SysPromptHeaderTitle: r.sysPromptHeaderTitle,
		SysPromptHeaderChanged: func(eras int) string {
			return fmt.Sprintf(r.sysPromptHeaderChangedFmt, eras)
		},
		SysPromptEraLink: func(fromSeq, toSeq, chars int, relPath string) string {
			return fmt.Sprintf(r.sysPromptEraLinkFmt, journeyStepRangeLabel(fromSeq, toSeq), chars, relPath)
		},
		SysPromptEraCoord: func(fromSeq, toSeq, chars int, blobName string) string {
			return fmt.Sprintf(r.sysPromptEraCoordFmt, journeyStepRangeLabel(fromSeq, toSeq), chars, blobName)
		},
		SysPromptEraNoSys: func(fromSeq, toSeq int) string {
			return fmt.Sprintf(r.sysPromptEraNoSysFmt, journeyStepRangeLabel(fromSeq, toSeq))
		},

		CompactionSummary: func(before, after, ratio string, swallowed, survived int) string {
			return fmt.Sprintf(r.compactionSummaryFmt, before, after, ratio, swallowed, survived)
		},
		SwallowedEntities: func(entities string) string { return fmt.Sprintf(r.swallowedEntitiesFmt, entities) },
		SurvivedEntities:  func(entities string) string { return fmt.Sprintf(r.survivedEntitiesFmt, entities) },
	}
}
