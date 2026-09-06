// Ver 2026-09-15, by pi

// Journey ViewModel builders for the document's front half — header, system
// prompt eras, overview card, behavior indicators, model usage — over the
// self-contained JourneySummary (viewmodel.go). Every function here is the
// viewmodel-side counterpart of its render_md.go / render_md_sysprompt.go /
// render_spine.go namesake: same bytes, different input (JourneySummary
// instead of *Journey), structured VM output instead of direct writes.
package journey

import (
	"fmt"
	"strings"

	"vmr/internal/core"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// vmSteps flattens s.Structure.Tasks into one Seq-ordered StepStructure slice —
// the viewmodel-side counterpart of journeySteps.
func vmSteps(s *JourneySummary) []*StepStructure {
	var out []*StepStructure
	for ti := range s.Structure.Tasks {
		t := &s.Structure.Tasks[ti]
		for si := range t.Steps {
			out = append(out, &t.Steps[si])
		}
	}
	return out
}

func vmStepTS(s *StepStructure) string {
	return s.TS.In(fmtutil.DisplayZone).Format("15:04:05")
}

// vmRespTexts reconstructs the Step's reply and reasoning texts from the
// bodies blob table, restoring the RespText/Reasoning distinction the
// RespRef/RespIsReasoning/ReasoningRef trio encodes (see StepStructure).
func vmRespTexts(s *JourneySummary, ss *StepStructure) (reply, reasoning string) {
	if ss.RespRef != "" {
		if ss.RespIsReasoning {
			reasoning = s.Bodies[ss.RespRef]
		} else {
			reply = s.Bodies[ss.RespRef]
		}
	}
	if ss.ReasoningRef != "" {
		reasoning = s.Bodies[ss.ReasoningRef]
	}
	return reply, reasoning
}

// --- header -----------------------------------------------------------------

// vmBreakReasonHint is breakReasonHint's string-kind counterpart: Break.Kind
// is EditRef's already-stringified kind.
func vmBreakReasonHint(kind string, t i18n.StoryText) string {
	switch kind {
	case ctxgraph.Contract.String():
		return t.BreakReasonContract
	case ctxgraph.Fork.String():
		return t.BreakReasonFork
	default:
		return t.BreakReasonDefault
	}
}

func vmEditStatsHint(e *EditRef, t i18n.StoryText) string {
	return t.EditStatsHint(e.LCP, e.Coverage*100)
}

func buildVMHeader(s *JourneySummary, lang i18n.Lang, reportMDExists bool) []VMBlock {
	t := i18n.Story(lang)
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(fmt.Sprintf("# Journey %s\n\n", s.ID)))
	blocks = append(blocks, vmTextBlock("> "+escapeHTML(s.Title)+"\n\n"))
	from := s.From.In(fmtutil.DisplayZone)
	to := s.To.In(fmtutil.DisplayZone)
	toLayout := "15:04:05"
	if from.Format("2006-01-02") != to.Format("2006-01-02") {
		toLayout = "2006-01-02 15:04:05"
	}
	blocks = append(blocks, vmTextBlock(t.JourneyMeta(len(s.Structure.Tasks), len(vmSteps(s)), from.Format("2006-01-02 15:04:05"), to.Format(toLayout))))
	reportLink := ""
	if reportMDExists {
		reportLink = "../vmr-report.md"
	}
	blocks = append(blocks, vmTextBlock(t.BackLinkLine(reportLink)))
	if s.Partial {
		blocks = append(blocks, vmTextBlock("> ⚠️ "+t.PartialBanner+"\n\n"))
	}
	if s.Break != nil {
		blocks = append(blocks, vmTextBlock(t.BreakWarning(s.Break.Kind, vmBreakReasonHint(s.Break.Kind, t), vmEditStatsHint(s.Break, t))))
	}
	return blocks
}

// --- system prompt eras -------------------------------------------------------

// vmSysPromptEra is systemPromptEra's viewmodel counterpart: the era grouping
// key is the same (HasSys, SysHash) pair, carried by StepStructure.SysHash
// (nil = no leading system block).
type vmSysPromptEra struct {
	sysHash        *ctxgraph.Hash
	fromSeq, toSeq int
	chars          int
}

func vmSystemPromptEras(s *JourneySummary) []vmSysPromptEra {
	var eras []vmSysPromptEra
	for _, ss := range vmSteps(s) {
		last := len(eras) - 1
		if last < 0 || !sameSysHash(ss.SysHash, eras[last].sysHash) {
			eras = append(eras, vmSysPromptEra{sysHash: ss.SysHash, fromSeq: ss.Seq, toSeq: ss.Seq, chars: ss.SysChars})
			continue
		}
		eras[last].toSeq = ss.Seq
	}
	return eras
}

func sameSysHash(a, b *ctxgraph.Hash) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func buildVMSysPrompt(s *JourneySummary, lang i18n.Lang, linkDetails bool) []VMBlock {
	t := i18n.Story(lang)
	eras := vmSystemPromptEras(s)
	if len(eras) == 0 {
		return nil
	}
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.SysPromptHeaderTitle))
	if len(eras) > 1 {
		blocks = append(blocks, vmTextBlock(t.SysPromptHeaderChanged(len(eras))))
	}
	for _, e := range eras {
		if e.sysHash == nil {
			blocks = append(blocks, vmTextBlock(t.SysPromptEraNoSys(e.fromSeq, e.toSeq)))
			continue
		}
		filename := reqdetail.SysPromptEvidenceFileName(*e.sysHash)
		if linkDetails {
			blocks = append(blocks, vmTextBlock(t.SysPromptEraLink(e.fromSeq, e.toSeq, e.chars, "../evidence/"+filename)))
		} else {
			blocks = append(blocks, vmTextBlock(t.SysPromptEraCoord(e.fromSeq, e.toSeq, e.chars, filename)))
		}
	}
	blocks = append(blocks, vmTextBlock("\n"))
	return blocks
}

// --- overview card ------------------------------------------------------------

func vmTimelineNodes(steps []*StepStructure, t i18n.SpineText) []string {
	if len(steps) == 0 {
		return nil
	}
	var out []string
	out = append(out, t.OverviewStart(vmStepTS(steps[0])))
	for _, s := range steps {
		if s.HasErrorMarker {
			out = append(out, t.OverviewFirstError(s.Seq, vmStepTS(s)))
			break
		}
	}
	for _, s := range steps {
		if s.StitchEdge != nil {
			out = append(out, t.OverviewTransition(s.Seq, s.StitchEdge.Kind, vmStepTS(s)))
			break
		}
		if s.Edit != nil && s.Edit.Kind != ctxgraphAppend {
			out = append(out, t.OverviewTransition(s.Seq, s.Edit.Kind, vmStepTS(s)))
			break
		}
	}
	last := steps[len(steps)-1]
	finish := last.Finish
	if finish == "" {
		finish = "-"
	}
	out = append(out, t.OverviewEnd(last.Seq, finish, vmStepTS(last)))
	return out
}

func buildVMOverview(s *JourneySummary, lang i18n.Lang) []VMBlock {
	t := i18n.Spine(lang)
	steps := vmSteps(s)
	nodes := vmTimelineNodes(steps, t)
	tags := structuralTags(s.Metrics, t)
	costLine := ""
	if s.Cost != nil && s.Cost.Resolved {
		costLine = t.OverviewCostLine(fmtMoney(*s.Cost))
	}
	failedSteps := 0
	for _, ss := range steps {
		if ss.Outcome == "error" {
			failedSteps++
		}
	}
	var failedLine string
	if failedSteps > 0 {
		failedLine = t.OverviewFailedStepsLine(failedSteps, len(steps))
	}
	if len(nodes) == 0 && len(tags) == 0 && costLine == "" && failedLine == "" {
		return nil
	}
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.OverviewTitle))
	for _, n := range nodes {
		blocks = append(blocks, vmTextBlock("- "+n+"\n"))
	}
	if failedLine != "" {
		blocks = append(blocks, vmTextBlock("- "+failedLine+"\n"))
	}
	if costLine != "" {
		blocks = append(blocks, vmTextBlock("- "+costLine+"\n"))
	}
	blocks = append(blocks, vmTextBlock("\n"))
	if len(tags) > 0 {
		blocks = append(blocks, vmTextBlock(t.TagsLine(strings.Join(tags, i18n.Story(lang).ListSep))))
	}
	return blocks
}

// --- detector-coverage disclosure ----------------------------------------------

// vmAnthropicCoverageCodes is journeyAnthropicCoverageCodes's viewmodel
// counterpart: the same "no Anthropic-messages Steps at all" rule, tallied
// over StepStructure.Protocol.
func vmAnthropicCoverageCodes(s *JourneySummary) (codes string, ok bool) {
	steps := vmSteps(s)
	if len(steps) == 0 {
		return "", false
	}
	for _, ss := range steps {
		if ss.Protocol == core.ProtocolAnthropicMessages {
			return "", false
		}
	}
	return strings.Join(anthropicOnlyCoverageNames(anthropicOnlyCoverage.JourneySections), ", "), true
}

// --- behavior indicators + model usage -----------------------------------------

func buildVMIndicators(s *JourneySummary, lang i18n.Lang) []VMBlock {
	t := i18n.Indicators(lang)
	_, isNonAnthropic := vmAnthropicCoverageCodes(s)
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.Title))
	tbl := &VMTable{Header: t.TableHeader}
	for _, jm := range journeyMetrics {
		v := jm.Value(s.Metrics)
		tbl.Rows = append(tbl.Rows, fmt.Sprintf("| %s | %s |\n", i18n.MetricLabel(lang, string(jm.Code)), jm.Format(s.Metrics, v, isNonAnthropic)))
	}
	blocks = append(blocks, VMBlock{Kind: vmTable, Table: tbl})
	if blk, ok := vmContextSparkline(s.Metrics, t); ok {
		blocks = append(blocks, blk)
	}
	blocks = append(blocks, vmModelUsage(s.Metrics, lang)...)
	return blocks
}

func vmContextSparkline(m Metrics, t i18n.IndicatorsText) (VMBlock, bool) {
	curve := m.ContextCurve
	if len(curve) < 2 {
		return VMBlock{}, false
	}
	totals := make([]int64, len(curve))
	var maxT int64
	for i, p := range curve {
		totals[i] = p.SystemTokens + p.UserTokens + p.AssistantTokens + p.ToolTokens
		if totals[i] > maxT {
			maxT = totals[i]
		}
	}
	if maxT == 0 {
		return VMBlock{}, false
	}
	var b strings.Builder
	for _, v := range totals {
		idx := int(float64(v) / float64(maxT) * float64(len(asciiSparklineChars)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(asciiSparklineChars) {
			idx = len(asciiSparklineChars) - 1
		}
		b.WriteRune(asciiSparklineChars[idx])
	}
	return vmTextBlock(fmt.Sprintf("%s: `%s` %s", t.SparklineTitle, b.String(), t.SparklineCaption(totals[0], totals[len(totals)-1]))), true
}

func vmModelUsage(m Metrics, lang i18n.Lang) []VMBlock {
	if len(m.ModelUsage) == 0 {
		return nil
	}
	t := i18n.ModelUsage(lang)
	var blocks []VMBlock
	blocks = append(blocks, vmTextBlock(t.Title+"\n\n"))
	tbl := &VMTable{Header: t.UsageHeader}
	for _, u := range m.ModelUsage {
		tbl.Rows = append(tbl.Rows, fmt.Sprintf("| %s (%s) | %d | %s | %s | %s |\n",
			u.Model, u.Provider, u.Steps, fmtutil.FmtTokens(u.TokensIn), fmtutil.FmtTokens(u.TokensInCached), fmtutil.FmtTokens(u.TokensOut)))
	}
	blocks = append(blocks, VMBlock{Kind: vmTable, Table: tbl})
	if len(m.ModelSwitches) == 0 {
		blocks = append(blocks, vmTextBlock(t.NoSwitches))
		return blocks
	}
	blocks = append(blocks, vmTextBlock(t.SwitchTitle))
	for _, sw := range m.ModelSwitches {
		line := t.SwitchLine(sw.StepSeq, sw.From, sw.To)
		if sw.HasCacheData && t.CacheImpactNote != nil {
			line += t.CacheImpactNote(pctStr(sw.PrevCacheRatio), pctStr(sw.CurCacheRatio))
		}
		if sw.OnFailoverStep {
			line += t.OnFailoverNote
		}
		blocks = append(blocks, vmTextBlock(line+"\n"))
	}
	blocks = append(blocks, vmTextBlock("\n"))
	return blocks
}

// ctxgraphAppend is ctxgraph.Append's stringified form — EditRef.Kind carries
// the already-stringified kind, and spineTransitionLines' "ordinary Append is
// silent" rule compares against exactly this value.
var ctxgraphAppend = ctxgraph.Append.String()
