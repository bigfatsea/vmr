// Ver 2026-08-19, by Sonnet 5

// renderDecisionSpine/renderSpineStep: split out of render_spine.go purely
// to stay under its archtest line budget (see file_sizes_test.go) — no
// behavior split, this is the decision spine's per-Step rendering, plus the
// two "fold instead of truncate" helpers it needs: foldWhyLine (a Step's
// own stated reason, previously a same-looking truncated one-liner —
// mirrors payloadBlock's fold convention in render_spine_args.go) and
// toolResultLine (a tool call's paired result, found via
// findings_toolresult.go's toolResultsFor — the same tool_call_id pairing
// the Finding detectors already trust, protocol-guaranteed by F9, see
// chatmsg/pairing.go's doc comment, extended here with an id-normalization
// fallback — see toolResultsFor's own doc comment — and, render-layer only,
// a positional fallback (positionalToolResults) for the residual case where
// even normalization doesn't recover the id).
//
// Every Step in a Journey renders here now, not just tool-calling ones
// (P1.2, story_report_architecture_opus-5.md §7.4a/A6): a Step with no
// ToolCalls still gets a header line plus, when it has something to show,
// a one-line summary (renderSpineBriefStep) — a mid-task instruction, a
// plain report, or nothing at all when the record genuinely has neither
// (never invented). No Journey and no Task is skipped wholesale anymore
// either, including a pure Q&A Journey with zero tool calls.
package story

import (
	"strconv"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// spineWhyRespCap/spineWhyReasoningCap gate foldWhyLine's inline-vs-fold
// decision — generous for RespText (in practice always a short one-sentence
// plan/decision, e.g. "akshare 1.18.79 可用！直接用 Python 获取数据。" — this only
// guards against an unusually verbose reply), tighter for a Reasoning
// excerpt (that field can run to paragraphs). Neither caps content anymore:
// past this length the text folds (collapsed, one click away, complete),
// it never gets cut.
//
// spineBriefLineCap bounds the one-line summaries renderSpineBriefStep
// renders for a non-tool-calling Step — deliberately NOT run through
// foldWhyLine's fold convention (that's for a Step's own primary why-line;
// these are secondary interstitial markers in the spine, truncated, not
// folded, so a Journey with many such Steps doesn't turn the spine itself
// into a wall of expandable blocks).
const (
	spineWhyRespCap      = 400
	spineWhyReasoningCap = 200
	spineBriefLineCap    = 120
)

// spineWhyLine renders the "why" behind this Step's tool calls, when the
// model said anything: its own stated reply/plan (RespText) if present, or
// — when RespText is empty but Reasoning isn't — that Reasoning, prefixed
// 🤔 to mark it as the model's inferred internal reasoning rather than a
// stated decision (matching render_md.go's own 🤔 convention for the same
// field). "" when the Step said nothing at all this turn — not every
// tool-calling Step does, and the spine shouldn't invent a reason where the
// record has none.
func spineWhyLine(s *Step) string {
	if s.RespText != "" {
		return foldWhyLine("> ", s.RespText, spineWhyRespCap)
	}
	if s.Reasoning != "" {
		return foldWhyLine("> 🤔 ", s.Reasoning, spineWhyReasoningCap)
	}
	return ""
}

// foldWhyLine renders text inline — flattened to one line — when it fits
// within capLen runes, else as a folded <details> block: a flattened
// preview (oneLineTruncate) so a reader scanning the spine without
// expanding anything still sees roughly what the Step said, and the full,
// unflattened text (fenced, complete — no spineFullCap-style cap: a Step's
// own stated reason is never large enough to need one) one click away.
func foldWhyLine(prefix, text string, capLen int) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len([]rune(flat)) <= capLen {
		return prefix + flat + "\n\n"
	}
	return prefix + "<details><summary>" + oneLineTruncate(text, capLen) + "</summary>\n\n" +
		codeFence(text) + "\n</details>\n\n"
}

// toolResultLine renders one tool call's paired result (chatmsg.ToolResult),
// folded the same way payloadBlock folds a call's arguments. "" for a
// result that came back empty — toolResultsFor already returns nil for a
// call with no visible answer (the Journey's last Step; see its own doc
// comment), so a missing entry in results silently renders nothing rather
// than claiming "no result" where none was ever recorded. positional marks
// a level-3 (positionally inferred, ID unmatched) pairing — see
// positionalToolResults — with an explicit badge; false for a level-1/2
// (exact or normalized ID) match, which needs no caveat.
func toolResultLine(name string, r chatmsg.ToolResult, positional bool, t i18n.SpineText) string {
	if r.Text == "" {
		return ""
	}
	mark := "↩️"
	if r.IsError {
		mark = "❌"
	}
	badge := ""
	if positional {
		badge = t.SpinePositionalMatch
	}
	return mark + " `" + name + "`" + badge + ": <details><summary>" + oneLineTruncate(r.Text, spinePreviewLen) + "</summary>\n\n" +
		codeFence(capFullWith(r.Text, t.SpineResultValueTruncated)) + "\n</details>\n\n"
}

// capFullWith is capFull's (render_spine_args.go) shared truncation core,
// parameterized on which localized "where's the rest" text to append —
// capFull uses it for a tool call's own arguments, where "this Step's
// detail link" is correct; toolResultLine above uses it for a paired
// result, whose full text actually lives in the NEXT Step's request
// record, not this one — hence a distinct SpineResultValueTruncated text
// rather than reusing SpineValueTruncated.
func capFullWith(s string, tail func(more int) string) string {
	r := []rune(s)
	if len(r) <= spineFullCap {
		return s
	}
	return string(r[:spineFullCap]) + tail(len(r)-spineFullCap)
}

// renderDecisionSpine renders the decision spine: one block per Task, one
// sub-block per Step — Step number/time (role-tagged the same way the fact
// layer's own Step headers are just below, see stepRoleTag — 🔄 for a Step
// toolCallRepeats flagged as an exact repeat is that tag, not a second
// marker here), the model's own stated reason for what it's about to do
// when it said one (spineWhyLine), then every tool call that Step made —
// complete (toolCallLine) — each immediately followed by its own paired
// result when one was recorded (toolResultLine). A Step referenced (as
// StepSeq or RelatedSeq) by any Finding earns an extra ⚠️ on its header.
// Every Step renders — a tool-calling one gets the full block
// (renderSpineStep), any other gets a header plus, when there's something
// to summarize, one line (renderSpineBriefStep) — so neither a pure Q&A
// Journey nor a Task with no tool calls disappears from the spine anymore
// (P1.2; see this file's package comment). Ends with the Journey's final
// deliverable section, when deliverableStats finds one.
func renderDecisionSpine(w func(string, ...any), j *Journey, findings []Finding, lang i18n.Lang) {
	t := i18n.Spine(lang)
	storyT := i18n.Story(lang)
	hit := map[int]bool{}
	for _, f := range findings {
		hit[f.StepSeq] = true
		for _, r := range f.RelatedSeq {
			hit[r] = true
		}
	}
	steps := journeySteps(j)
	repeat := map[int]bool{}
	for _, o := range toolCallRepeats(steps) {
		if o.IsRepeat {
			repeat[o.StepSeq] = true
		}
	}
	idxOf := make(map[*Step]int, len(steps))
	for i, s := range steps {
		idxOf[s] = i
	}

	w("%s", t.SpineTitle)
	for ti, task := range j.Tasks {
		w("%s", t.SpineTaskLine(ti+1, task.Title))
		for si, s := range task.Steps {
			if len(s.ToolCalls) > 0 {
				renderSpineStep(w, steps, idxOf[s], si, repeat[s.Seq], hit[s.Seq], t, storyT)
			} else {
				renderSpineBriefStep(w, s, si, repeat[s.Seq], hit[s.Seq], t, storyT)
			}
		}
	}
	renderFinalDeliverable(w, j, t)
}

// spineStepHeader renders one Step's header line — shared by renderSpineStep
// (tool-calling) and renderSpineBriefStep (everything else), so the two
// forms stay visually consistent (same tag, same Finding flag) and a reader
// scanning the spine sees one continuous Step sequence regardless of which
// kind of Step each one is. Includes a "→ detail" link to this Step's own
// record, computed purely from its Manifest (reqdetail.FileNameForManifest
// — no I/O, correct whether or not the target file has actually been
// written yet by EnsureJourneyDetails) — the link always renders (DevPlan
// P5.2: "名称可算"), so it's never absent just because materialization
// happened to fail for this one Step (see EnsureJourneyDetails' doc
// comment on graceful degradation).
func spineStepHeader(s *Step, repeated, flagged bool, t i18n.SpineText) string {
	// s.Manifest is non-nil on every production path (journey.go's buildStep
	// always sets it); the guard is for test fixtures that construct a Step
	// by hand. It has to cover the timestamp too — the old form dereferenced
	// s.Manifest.TS above the check, so the check could never have saved a
	// nil from panicking.
	ts := ""
	if s.Manifest != nil {
		ts = " · " + s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")
	}
	header := "**" + stepRoleTag(s, repeated, t) + " Step " + strconv.Itoa(s.Seq) + ts + "**"
	if flagged {
		header += t.SpineFindingTag
	}
	header += "\n\n"
	if s.Manifest != nil {
		header += t.SpineDetailLink("../details/" + reqdetail.FileNameForManifest(s.Manifest))
	}
	return header
}

// spineTransitionLines renders the cross-record analysis facts the deleted
// fact-layer renderer (render_md.go's former renderStep, removed by P5.1)
// used to show — Edit/StitchEdge/SysChanged/Compaction — reusing the exact
// same i18n.StoryText functions and CompactionInfo renderer render_md.go
// still defines. These are graph-level facts a per-Step detail link
// (spineStepHeader, above) can never reach — reqdetail renders one record
// plus one prev Manifest, never a full Edit/StitchGraph/Compaction
// computation — so the decision spine, the one human-readable layer that
// survives P5.1, is their only remaining home. Called from both
// renderSpineStep and renderSpineBriefStep, right after the header line, so
// placement is independent of whether the Step happens to have tool calls.
//
// s.Edge's ordinary Append case is deliberately silent: within a single
// Lineage, Append ("cur starts with all of prev's messages") is the
// overwhelmingly common, structurally uninteresting default — showing it
// on every Step would bury the spine's "3-second scan" signal under noise
// for information a reader already assumes unless told otherwise (P4's
// structure.json still records every Step's EditRef, so nothing is lost by
// staying silent here, only the "nothing happened" case is elided).
func spineTransitionLines(w func(string, ...any), s *Step, storyT i18n.StoryText) {
	if s.Edge != nil && s.Edge.Kind != ctxgraph.Append {
		w("%s", storyT.EditLine(s.Edge.Kind.String(), editStatsHint(*s.Edge, storyT)))
	}
	if s.StitchEdge != nil {
		w("%s", storyT.StitchLine(s.StitchEdge.Kind.String(), pctStr(s.StitchEdge.Score), pctStr(s.StitchEdge.Confidence)))
	}
	if s.SysChanged {
		w("%s", storyT.SysChangedLine)
	}
	if s.Compaction != nil {
		renderCompactionInfo(w, s.Compaction, storyT)
	}
}

// renderSpineStep renders one tool-calling Step's decision-spine block —
// see renderDecisionSpine. steps/i (rather than a pre-fetched result slice)
// so it can compute both the id/normalized-id matches (toolResultsFor) and,
// for whatever's left over, the positional fallback (positionalToolResults)
// itself — the two levels share the same "which calls are still unresolved"
// bookkeeping and there's no benefit to splitting that across the caller.
// taskStepIdx (like renderSpineBriefStep's) gates the mid-task instruction
// line — a mid-task user instruction very often triggers an immediate tool
// call, so this path needs the same s.Instruction rendering
// renderSpineBriefStep already has, or that instruction is invisible
// whenever the Step that carries it also happens to call a tool.
func renderSpineStep(w func(string, ...any), steps []*Step, i, taskStepIdx int, repeated, flagged bool, t i18n.SpineText, storyT i18n.StoryText) {
	s := steps[i]
	w("%s", spineStepHeader(s, repeated, flagged, t))
	spineTransitionLines(w, s, storyT)
	if taskStepIdx > 0 && s.Instruction != "" {
		w("%s", t.SpineInstructionLine(s.Instruction))
	}
	w("%s", spineWhyLine(s))

	matched := toolResultsFor(steps, i)
	byID := make(map[string]chatmsg.ToolResult, len(matched))
	for _, r := range matched {
		byID[r.CallID] = r
	}
	posByID := positionalToolResults(steps, i, byID)

	for _, tc := range s.ToolCalls {
		w("%s", toolCallLine(tc, t))
		if r, ok := byID[tc.ID]; ok {
			w("%s", toolResultLine(tc.Name, r, false, t))
		} else if r, ok := posByID[tc.ID]; ok {
			w("%s", toolResultLine(tc.Name, r, true, t))
		}
	}
	if s.NoReply {
		w("%s", storyT.NoReplyLine)
	}
}

// positionalToolResults is the third and final pairing level (architecture
// doc §5.5/§5.6): for whichever of steps[i]'s ToolCalls byID (toolResultsFor's
// exact+normalized-id matches) didn't resolve, try pairing them by position
// against the next step's still-unclaimed tool results — safe only when the
// unresolved-call count equals the unclaimed-result count (the one
// condition real-corpus verification confirmed is safe; a mismatch means
// don't guess, not "guess anyway"). Render-layer only: never called from a
// Finding detector, since a positional pairing is inference, not a fact.
func positionalToolResults(steps []*Step, i int, byID map[string]chatmsg.ToolResult) map[string]chatmsg.ToolResult {
	s := steps[i]
	if len(s.ToolCalls) == 0 || i+1 >= len(steps) || steps[i+1].Rec == nil {
		return nil
	}
	knownNorm := make(map[string]bool, len(s.ToolCalls))
	var unresolved []chatmsg.ToolCall
	for _, tc := range s.ToolCalls {
		knownNorm[normalizeToolCallID(tc.ID)] = true
		if _, ok := byID[tc.ID]; !ok {
			unresolved = append(unresolved, tc)
		}
	}
	if len(unresolved) == 0 {
		return nil
	}
	body, _ := steps[i+1].Rec.Client.Request.Body.(map[string]any)
	// steps[i+1]'s body carries the WHOLE conversation so far (every chat
	// API resends full history), so scanning chatmsg.RawArray(body)
	// unbounded would pull in every earlier Step's already-resolved tool
	// results too — inflating the leftover count against every OTHER
	// unresolved-call Step in the Journey, not just this one. DeltaStart
	// ("absolute message index where this step's new content begins",
	// journey.go) plus MsgOffset's index-base correction (RawArray excludes
	// the synthetic leading system message Messages() prepends) bounds the
	// scan to exactly the messages steps[i+1] introduced.
	rawArr := chatmsg.RawArray(body)
	deltaIdx := steps[i+1].DeltaStart - chatmsg.MsgOffset(body)
	if deltaIdx > 0 && deltaIdx < len(rawArr) {
		rawArr = rawArr[deltaIdx:]
	} else if deltaIdx >= len(rawArr) {
		rawArr = nil
	}
	var leftover []chatmsg.ToolResult
	for _, r := range chatmsg.ToolResultList(rawArr) {
		if !knownNorm[normalizeToolCallID(r.CallID)] {
			leftover = append(leftover, r)
		}
	}
	if len(leftover) != len(unresolved) {
		return nil
	}
	out := make(map[string]chatmsg.ToolResult, len(unresolved))
	for k, tc := range unresolved {
		out[tc.ID] = leftover[k]
	}
	return out
}

// renderSpineBriefStep renders a non-tool-calling Step's spine entry: always
// the header (so the Step stays visible in the sequence — P1.2's coverage
// fix), then at most one summary line, in priority order:
//  1. a mid-task instruction — taskStepIdx > 0 (the Task's own opening Step
//     already carries its instruction in the Task title, see
//     renderDecisionSpine's SpineTaskLine call, so it's skipped here to
//     avoid rendering the same instruction twice) and s.Instruction != ""
//     — via s.Instruction (buildFrom, taskseg.LastInstruction);
//  2. a plain report — s.RespText;
//  3. the model's reasoning, when it said nothing else — s.Reasoning.
//
// Renders no summary line at all when none of the three apply (a Step with
// no tool calls, no new instruction, no reply, and no reasoning — e.g. an
// NoReply Step) — "宁可粗糙也不猜语义": the header alone still keeps the Step
// countable, and inventing a line where the record has nothing would be
// worse than a gap.
func renderSpineBriefStep(w func(string, ...any), s *Step, taskStepIdx int, repeated, flagged bool, t i18n.SpineText, storyT i18n.StoryText) {
	w("%s", spineStepHeader(s, repeated, flagged, t))
	spineTransitionLines(w, s, storyT)
	switch {
	case taskStepIdx > 0 && s.Instruction != "":
		w("%s", t.SpineInstructionLine(s.Instruction))
	case s.RespText != "":
		w("%s", t.SpineReportLine(oneLineTruncate(s.RespText, spineBriefLineCap)))
	case s.Reasoning != "":
		w("%s", t.SpineReportLine(oneLineTruncate(s.Reasoning, spineBriefLineCap)))
	}
	if s.NoReply {
		w("%s", storyT.NoReplyLine)
	}
}

// renderFinalDeliverable renders the Journey's final deliverable section,
// when deliverableStats (compare.go — the same detection -compare already
// uses) finds a write-shaped tool call. Deliberately silent when it
// doesn't: most Journeys don't end on a file write, and
// renderSpineBriefStep's report line already surfaces "what did the last
// Step say" for the general case — this section is an enhancement for the
// specific case a Journey does write a deliverable, not a replacement.
func renderFinalDeliverable(w func(string, ...any), j *Journey, t i18n.SpineText) {
	d := deliverableStats(j)
	if !d.Found {
		return
	}
	w("%s", t.SpineFinalDeliverableTitle)
	w("%s", t.SpineFinalDeliverableFound(d.StepSeq, d.ToolName))
	excerpt := d.Excerpt
	if d.Truncated {
		excerpt += "\n…"
	}
	w("<details><summary>%s</summary>\n\n%s\n</details>\n\n", t.SpineFinalDeliverableExcerptLabel, codeFence(excerpt))
}
