// Ver 2026-09-15, by pi

// Journey ViewModel builders for the decision spine, the tool-call timeline,
// the findings section and the final deliverable — the viewmodel-side
// counterparts of render_spine.go / render_spine_step.go / render_spine_args.go.
// Same bytes as the old render path, but over the self-contained JourneySummary:
// tool-call arguments and paired results are resolved through the bodies blob
// table, pairing through the stamped three-level match (ToolResultRef.Match),
// and the cross-Step judgments the old path re-derived from in-memory Steps
// (repeat flags, error markers) read the facts BuildStructure stamps.
package journey

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// --- decision spine ---------------------------------------------------------

func buildVMSpine(s *JourneySummary, lang i18n.Lang, linkDetails bool) []VMBlock {
	t := i18n.Spine(lang)
	journeyT := i18n.Journey(lang)
	hit := map[int]bool{}
	for _, f := range s.Findings {
		hit[f.StepSeq] = true
		for _, r := range f.RelatedSeq {
			hit[r] = true
		}
	}
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.SpineTitle))
	if !linkDetails {
		blocks = append(blocks, vmTextBlock(t.SpineCoordNote))
	}
	for ti := range s.Structure.Tasks {
		task := &s.Structure.Tasks[ti]
		blocks = append(blocks, vmTextBlock(t.SpineTaskLine(ti+1, escapeHTML(task.Title))))
		for si := range task.Steps {
			blocks = append(blocks, vmStepBlocks(s, &task.Steps[si], si, hit, t, journeyT, linkDetails)...)
		}
	}
	blocks = append(blocks, vmFinalDeliverable(s, t)...)
	return blocks
}

// vmStepRepeated is the old path's repeat[s.Seq] (any of the Step's calls is
// an exact repeat) — read from the stamped per-call flag.
func vmStepRepeated(ss *StepStructure) bool {
	for i := range ss.ToolCalls {
		if ss.ToolCalls[i].Repeat {
			return true
		}
	}
	return false
}

// vmDetailFileName recomputes the Step's detail-page name the way
// reqdetail.FileNameForManifest does — FileName's five inputs are all
// StepStructure fields (the real-model segment is Endpoint's own label).
func vmDetailFileName(ss *StepStructure) string {
	_, _, real, _ := core.SplitEndpointLabel(ss.Endpoint)
	return reqdetail.FileName(ss.TS, ss.Model, real, ss.Outcome, ss.Req)
}

func vmStepHeader(ss *StepStructure, repeated, flagged bool, reply, reasoning string, t i18n.SpineText, linkDetails bool) string {
	header := "**" + vmStepRoleTag(ss, repeated, reply, reasoning, t) + " Step " + strconv.Itoa(ss.Seq) + " · " + vmStepTS(ss) + "**"
	if flagged {
		header += t.SpineFindingTag
	}
	header += "\n\n"
	if linkDetails {
		// journey .md lives at journeys/details/j-<id>.md; request detail
		// files live at requests/details/r-<...>.md (§4 topology) — two
		// levels up, then into the requests/ tree.
		header += t.SpineDetailLink("../../requests/details/" + vmDetailFileName(ss))
	} else {
		header += t.SpineDetailCoord(ss.Req)
	}
	return header
}

// vmStepRoleTag is stepRoleTag's viewmodel counterpart — same priority order,
// same already-computed signals; the reply/reasoning texts come from
// vmRespTexts (the bodies blob table), the error-marker scan from the stamped
// HasErrorMarker.
func vmStepRoleTag(ss *StepStructure, repeated bool, reply, reasoning string, t i18n.SpineText) string {
	if ss.StitchEdge != nil {
		return t.StepTagCompaction
	}
	if ss.Outcome == "error" {
		return t.StepTagError
	}
	if ss.HasErrorMarker {
		return t.StepTagError
	}
	if repeated {
		return t.StepTagRetry
	}
	if len(ss.ToolCalls) > 0 {
		return t.StepTagAction
	}
	if len(ExtractActionablePlan(reasoning)) >= minPlanItems || len(ExtractActionablePlan(reply)) >= minPlanItems {
		return t.StepTagPlan
	}
	if reply != "" {
		return t.StepTagReport
	}
	return t.StepTagObserve
}

func vmTransitionBlocks(ss *StepStructure, journeyT i18n.JourneyText) []VMBlock {
	var blocks []VMBlock
	if ss.Edit != nil && ss.Edit.Kind != ctxgraphAppend {
		blocks = append(blocks, vmTextBlock(journeyT.EditLine(ss.Edit.Kind, vmEditStatsHint(ss.Edit, journeyT))))
	}
	if ss.StitchEdge != nil {
		blocks = append(blocks, vmTextBlock(journeyT.StitchLine(ss.StitchEdge.Kind, pctStr(ss.StitchEdge.Score), pctStr(ss.StitchEdge.Confidence))))
	}
	if ss.SysChanged {
		blocks = append(blocks, vmTextBlock(journeyT.SysChangedLine))
	}
	if ss.Compaction != nil {
		blocks = append(blocks, vmCompactionBlock(ss.Compaction, journeyT))
	}
	return blocks
}

func vmCompactionBlock(c *CompactionRef, journeyT i18n.JourneyText) VMBlock {
	ratio := "—"
	if c.TokensBefore > 0 {
		ratio = fmtutil.FmtPercent(float64(c.TokensAfter)/float64(c.TokensBefore), 1)
	}
	summary := journeyT.CompactionSummary(fmtutil.FmtTokens(c.TokensBefore), fmtutil.FmtTokens(c.TokensAfter), ratio, len(c.SwallowedEntities), len(c.SurvivedEntities))
	var body string
	if len(c.SwallowedEntities) > 0 {
		body += journeyT.SwallowedEntities(strings.Join(c.SwallowedEntities, journeyT.ListSep))
	}
	if len(c.SurvivedEntities) > 0 {
		body += journeyT.SurvivedEntities(strings.Join(c.SurvivedEntities, journeyT.ListSep))
	}
	return vmDetailsBlock("", summary, body)
}

// vmStepBlocks renders one Step's spine entry — renderSpineStep and
// renderSpineBriefStep's shared shape: header, transitions, then either the
// full tool-calling block (instruction, why-line, every call + its paired
// result) or the single summary line, then the NoReply marker.
func vmStepBlocks(s *JourneySummary, ss *StepStructure, taskStepIdx int, hit map[int]bool, t i18n.SpineText, journeyT i18n.JourneyText, linkDetails bool) []VMBlock {
	repeated := vmStepRepeated(ss)
	reply, reasoning := vmRespTexts(s, ss)
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(vmStepHeader(ss, repeated, hit[ss.Seq], reply, reasoning, t, linkDetails)))
	blocks = append(blocks, vmTransitionBlocks(ss, journeyT)...)
	if len(ss.ToolCalls) > 0 {
		if taskStepIdx > 0 && ss.Instruction != "" {
			blocks = append(blocks, vmTextBlock(t.SpineInstructionLine(escapeHTML(ss.Instruction))))
		}
		blocks = append(blocks, vmWhyBlocks(reply, reasoning)...)
		for i := range ss.ToolCalls {
			blocks = append(blocks, vmToolCallBlocks(s, &ss.ToolCalls[i], t)...)
		}
	} else {
		switch {
		case taskStepIdx > 0 && ss.Instruction != "":
			blocks = append(blocks, vmTextBlock(t.SpineInstructionLine(escapeHTML(ss.Instruction))))
		case reply != "":
			blocks = append(blocks, vmTextBlock(t.SpineReportLine(escapeHTML(oneLineTruncate(reply, spineBriefLineCap)))))
		case reasoning != "":
			blocks = append(blocks, vmTextBlock(t.SpineReportLine(escapeHTML(oneLineTruncate(reasoning, spineBriefLineCap)))))
		}
	}
	if ss.NoReply {
		blocks = append(blocks, vmTextBlock(journeyT.NoReplyLine))
	}
	return blocks
}

// vmWhyBlocks is spineWhyLine/foldWhyLine's viewmodel counterpart.
func vmWhyBlocks(reply, reasoning string) []VMBlock {
	if reply != "" {
		return vmFoldWhy("> ", reply, spineWhyRespCap)
	}
	if reasoning != "" {
		return vmFoldWhy("> 🤔 ", reasoning, spineWhyReasoningCap)
	}
	return nil
}

func vmFoldWhy(prefix, text string, capLen int) []VMBlock {
	flat := strings.Join(strings.Fields(text), " ")
	if len([]rune(flat)) <= capLen {
		return []VMBlock{vmTextBlock(prefix + escapeHTML(flat) + "\n\n")}
	}
	return []VMBlock{vmDetailsBlock(prefix, escapeHTML(oneLineTruncate(text, capLen)), codeFence(text)+"\n")}
}

// vmToolCallBlocks renders one tool call — toolCallLine's argument shape-picking
// (payload field selection, inline vs folded) over the body blob's (possibly
// capped) argument text, immediately followed by its paired result block when
// the stamped match carries one.
func vmToolCallBlocks(s *JourneySummary, tc *ToolCallRef, t i18n.SpineText) []VMBlock {
	args := s.Bodies[tc.ArgsRef]
	head := "🔧 `" + tc.Name + "`"

	var v map[string]any
	if err := json.Unmarshal([]byte(args), &v); err != nil || len(v) == 0 {
		return append(vmPayloadBlocks(head, "", args, t), vmToolResultBlocks(s, tc, t)...)
	}

	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order — map iteration isn't

	type field struct{ key, val string }
	fields := make([]field, len(keys))
	allShort := true
	longest := 0
	for i, k := range keys {
		val, big := scalarSummary(v[k])
		fields[i] = field{k, val}
		if big {
			allShort = false
		}
		if len([]rune(val)) > len([]rune(fields[longest].val)) {
			longest = i
		}
	}

	if allShort {
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = f.key + "=" + escapeHTML(f.val)
		}
		return append([]VMBlock{vmTextBlock(head + "(" + strings.Join(parts, ", ") + ")\n\n")}, vmToolResultBlocks(s, tc, t)...)
	}
	f := fields[longest]
	return append(vmPayloadBlocks(head, f.key, f.val, t), vmToolResultBlocks(s, tc, t)...)
}

func vmPayloadBlocks(head, key, val string, t i18n.SpineText) []VMBlock {
	label := head
	if key != "" {
		label += " `" + key + "`"
	}
	if !strings.Contains(val, "\n") && len([]rune(val)) <= spineInlineLen {
		return []VMBlock{vmTextBlock(label + ": " + escapeHTML(val) + "\n\n")}
	}
	preview := oneLineTruncate(val, spinePreviewLen)
	return []VMBlock{vmDetailsBlock(label+": ", escapeHTML(preview), codeFence(capFull(val, t))+"\n")}
}

// vmToolResultBlocks is toolResultLine's viewmodel counterpart: the result
// text comes from the bodies blob table via the stamped ToolResultRef, and
// the positional badge is the third-level match itself — the JSON's pairing
// claim and the spine's rendered pairing are one and the same fact.
func vmToolResultBlocks(s *JourneySummary, tc *ToolCallRef, t i18n.SpineText) []VMBlock {
	if tc.Result == nil {
		return nil
	}
	text := s.Bodies[tc.Result.Ref]
	if text == "" {
		return nil
	}
	mark := "↩️"
	if tc.Result.IsError {
		mark = "❌"
	}
	badge := ""
	if tc.Result.Match == "positional" {
		badge = t.SpinePositionalMatch
	}
	return []VMBlock{vmDetailsBlock(mark+" `"+tc.Name+"`"+badge+": ",
		escSummary(text), codeFence(capFullWith(text, t.SpineResultValueTruncated))+"\n")}
}

func escSummary(text string) string {
	return escapeHTML(oneLineTruncate(text, spinePreviewLen))
}

// --- final deliverable ---------------------------------------------------------

func vmFinalDeliverable(s *JourneySummary, t i18n.SpineText) []VMBlock {
	d := s.Deliverable
	if d == nil {
		return nil
	}
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.SpineFinalDeliverableTitle))
	blocks = append(blocks, vmTextBlock(t.SpineFinalDeliverableFound(d.StepSeq, d.ToolName)))
	excerpt := d.Excerpt
	if d.Truncated {
		excerpt += "\n…"
	}
	blocks = append(blocks, vmDetailsBlock("", t.SpineFinalDeliverableExcerptLabel, codeFence(excerpt)+"\n"))
	return blocks
}

// --- tool-call timeline ---------------------------------------------------------

func buildVMTimeline(s *JourneySummary, lang i18n.Lang) []VMBlock {
	t := i18n.Spine(lang)
	steps := vmSteps(s)
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.TimelineTitle))
	if len(steps) == 0 {
		blocks = append(blocks, vmTextBlock(t.TimelineNoData))
		return blocks
	}

	repeatAt := map[int]bool{}
	errAt := map[int]bool{}
	toolAt := map[string]map[int]bool{}
	var names []string
	for _, ss := range steps {
		if ss.HasErrorMarker {
			errAt[ss.Seq] = true
		}
		for i := range ss.ToolCalls {
			tc := &ss.ToolCalls[i]
			if tc.Repeat {
				repeatAt[ss.Seq] = true
			}
			if toolAt[tc.Name] == nil {
				toolAt[tc.Name] = map[int]bool{}
				names = append(names, tc.Name)
			}
			toolAt[tc.Name][ss.Seq] = true
		}
	}
	if len(names) == 0 {
		blocks = append(blocks, vmTextBlock(t.TimelineNoData))
		return blocks
	}
	sort.Strings(names)

	maxNameLen := 0
	for _, n := range names {
		if l := len([]rune(n)); l > maxNameLen {
			maxNameLen = l
		}
	}

	blocks = append(blocks, vmTextBlock(t.TimelineLegend))
	var b strings.Builder
	if len(steps) >= 20 {
		b.WriteString(padRight("Step", maxNameLen))
		b.WriteString(" ")
		for i := 1; i <= len(steps); i++ {
			if i%50 == 0 {
				b.WriteString("╎")
			} else if i%10 == 0 {
				b.WriteString(strconv.Itoa((i / 10) % 10))
			} else {
				b.WriteString("·")
			}
		}
		b.WriteByte('\n')
	}
	for _, n := range names {
		b.WriteString(padRight(n, maxNameLen))
		b.WriteString(" ")
		for _, ss := range steps {
			switch {
			case toolAt[n][ss.Seq] && errAt[ss.Seq]:
				b.WriteString("❌")
			case toolAt[n][ss.Seq] && repeatAt[ss.Seq]:
				b.WriteString("🔄")
			case toolAt[n][ss.Seq]:
				b.WriteString("●")
			default:
				b.WriteString("·")
			}
		}
		b.WriteByte('\n')
	}
	blocks = append(blocks, vmTextBlock(codeFence(b.String())))
	return blocks
}

// --- findings section --------------------------------------------------------------

func buildVMFindings(s *JourneySummary, lang i18n.Lang) []VMBlock {
	t := i18n.Spine(lang)
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.FindingsTitle))
	if codes, ok := vmAnthropicCoverageCodes(s); ok {
		blocks = append(blocks, vmTextBlock(t.AnthropicOnlyCoverageNote(codes)))
	}
	if len(s.Findings) == 0 {
		blocks = append(blocks, vmTextBlock(t.FindingsNone))
		return blocks
	}
	type group struct {
		code     FindingCode
		findings []Finding
	}
	byCode := map[FindingCode]*group{}
	var order []FindingCode
	for _, f := range s.Findings {
		if byCode[f.Code] == nil {
			byCode[f.Code] = &group{code: f.Code}
			order = append(order, f.Code)
		}
		byCode[f.Code].findings = append(byCode[f.Code].findings, f)
	}
	sort.SliceStable(order, func(i, j int) bool {
		ti, tj := findingTrustTier(order[i]), findingTrustTier(order[j])
		if ti != tj {
			return ti < tj
		}
		return byCode[order[i]].findings[0].StepSeq < byCode[order[j]].findings[0].StepSeq
	})
	for _, code := range order {
		g := byCode[code]
		blocks = append(blocks, vmTextBlock(t.FindingGroupTitle(string(code), len(g.findings), g.findings[0].StepSeq)))
		for i := range g.findings {
			f := &g.findings[i]
			var text string
			text += formatFindingHeader(i, *f, g.findings, t)
			if len(f.RelatedSeq) > 0 {
				text += t.FindingRelated(joinInts(f.RelatedSeq))
			}
			if evStr := formatFindingEvidence(*f, t); evStr != "" {
				text += evStr
			}
			if f.Action != "" {
				text += t.FindingAction(f.Action)
			}
			text += "\n"
			blocks = append(blocks, vmTextBlock(text))
		}
	}
	return blocks
}
