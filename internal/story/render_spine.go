// Ver 2026-08-05, by Sonnet 5

// The decision-spine layer (see docs/future-strategy/
// vmr_story_journey_deepdive_sonnet-5.md's decision-spine section) on top
// of render_md.go's existing fact-layer renderer: a 3-second overview card,
// a compact per-Task action list, per-Step role tags, and an optional
// tool-call timeline. Every function here is pure formatting/aggregation over
// data render_md.go's renderStep already has (Task/Step/Event, Metrics,
// Finding) — no new fields on Journey/Step, no new computation beyond what
// toolCallRepeats/ComputeFindings already produce. render_md.go calls into
// this file, never the other way.
package story

import (
	"sort"
	"strconv"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// --- overview card ---------------------------------------------------------

// toolIntensiveThreshold/retryHeavyThreshold gate the overview card's
// structural tags — deliberately coarse triage bars (design doc: "宁可粗糙
// 也不猜语义"), not tuned against a calibration corpus the way the Finding
// detectors' thresholds are, since these only ever add a label next to
// numbers the reader can already see in the same document.
const (
	toolIntensiveThreshold = 10
	retryHeavyThreshold    = 0.2
)

func structuralTags(m Metrics, t i18n.SpineText) []string {
	var tags []string
	if m.ToolCallCount >= toolIntensiveThreshold {
		tags = append(tags, t.TagToolIntensive)
	}
	if m.DuplicateActionRate >= retryHeavyThreshold {
		tags = append(tags, t.TagRetryHeavy)
	}
	if m.CompactionCount > 0 {
		tags = append(tags, t.TagContextCompacted)
	}
	return tags
}

// timelineNodes picks the overview card's 3-5 headline moments: start,
// first error marker (if any), first non-Append/stitch transition (if any),
// end. Deliberately NOT every Break/Compaction/SysChanged event — those
// still render in full further down; this is the "3-second" layer, kept to
// a handful of nodes on purpose.
func timelineNodes(j *Journey, t i18n.SpineText) []string {
	steps := journeySteps(j)
	if len(steps) == 0 {
		return nil
	}
	var out []string
	first := steps[0]
	out = append(out, t.OverviewStart(first.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")))

errLoop:
	for _, s := range steps {
		for _, ev := range s.NewEvents {
			if strings.Contains(ev.Msg.Text, isErrorMarker) {
				out = append(out, t.OverviewFirstError(s.Seq, s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")))
				break errLoop
			}
		}
	}

	for _, s := range steps {
		if s.StitchEdge != nil {
			out = append(out, t.OverviewTransition(s.Seq, s.StitchEdge.Kind.String(), s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")))
			break
		}
		if s.Edge != nil && s.Edge.Kind != ctxgraph.Append {
			out = append(out, t.OverviewTransition(s.Seq, s.Edge.Kind.String(), s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")))
			break
		}
	}

	last := steps[len(steps)-1]
	finish := last.Finish
	if finish == "" {
		finish = "-"
	}
	out = append(out, t.OverviewEnd(last.Seq, finish, last.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")))
	return out
}

func renderOverviewCard(w func(string, ...any), j *Journey, m Metrics, lang i18n.Lang) {
	t := i18n.Spine(lang)
	nodes := timelineNodes(j, t)
	tags := structuralTags(m, t)
	if len(nodes) == 0 && len(tags) == 0 {
		return
	}
	w("%s", t.OverviewTitle)
	for _, n := range nodes {
		w("- %s\n", n)
	}
	w("\n")
	if len(tags) > 0 {
		w("%s", t.TagsLine(strings.Join(tags, i18n.Story(lang).ListSep)))
	}
}

// --- decision spine ---------------------------------------------------------
// toolCallLine and its helpers live in render_spine_args.go (split out to
// stay under this file's archtest line budget).

// spineWhyRespCap/spineWhyReasoningCap bound spineWhyLine's length —
// generous for RespText (in practice always a short one-sentence plan/
// decision, e.g. "akshare 1.18.79 可用！直接用 Python 获取数据。" — this cap only
// guards against an unusually verbose reply), tighter for a Reasoning
// excerpt (that field can run to paragraphs; the full text is one click
// away in this Step's own LLM Response section, folded).
const (
	spineWhyRespCap      = 400
	spineWhyReasoningCap = 200
)

// spineWhyLine renders the "why" behind this Step's tool calls, when the
// model said anything: its own stated reply/plan (RespText) if present, or
// — when RespText is empty but Reasoning isn't — a short excerpt of that,
// prefixed 🤔 to mark it as the model's inferred internal reasoning rather
// than a stated decision (matching render_md.go's own 🤔 convention for
// the same field). "" when the Step said nothing at all this turn — not
// every tool-calling Step does, and the spine shouldn't invent a reason
// where the record has none.
func spineWhyLine(s *Step) string {
	if s.RespText != "" {
		return "> " + oneLineTruncate(s.RespText, spineWhyRespCap) + "\n\n"
	}
	if s.Reasoning != "" {
		return "> 🤔 " + oneLineTruncate(s.Reasoning, spineWhyReasoningCap) + "\n\n"
	}
	return ""
}

// oneLineTruncate collapses s to one line (internal whitespace normalized
// to single spaces) and rune-truncates it to n.
func oneLineTruncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// renderDecisionSpine renders the decision spine: one block per Task, one
// sub-block per tool-calling Step — Step number/time (role-tagged the same
// way the fact layer's own Step headers are just below, see stepRoleTag —
// 🔄 for a Step toolCallRepeats flagged as an exact repeat is that tag,
// not a second marker here), the model's own stated reason for what it's
// about to do when it said one (spineWhyLine), then every tool call that
// Step made — complete (toolCallLine), not a same-looking truncated
// prefix. A Step referenced (as StepSeq or RelatedSeq) by any Finding
// earns an extra ⚠️ on its header. Renders nothing for a Journey with no
// tool calls at all (a pure Q&A Journey has no "decisions" to spine) —
// and, within it, skips any Task whose Steps never called a tool either.
func renderDecisionSpine(w func(string, ...any), j *Journey, findings []Finding, lang i18n.Lang) {
	t := i18n.Spine(lang)
	hit := map[int]bool{}
	for _, f := range findings {
		hit[f.StepSeq] = true
		for _, r := range f.RelatedSeq {
			hit[r] = true
		}
	}
	repeat := map[int]bool{}
	for _, o := range toolCallRepeats(journeySteps(j)) {
		if o.IsRepeat {
			repeat[o.StepSeq] = true
		}
	}

	anyCalls := false
	for _, task := range j.Tasks {
		for _, s := range task.Steps {
			if len(s.ToolCalls) > 0 {
				anyCalls = true
			}
		}
	}
	if !anyCalls {
		return
	}

	w("%s", t.SpineTitle)
	for ti, task := range j.Tasks {
		var acting []*Step
		for _, s := range task.Steps {
			if len(s.ToolCalls) > 0 {
				acting = append(acting, s)
			}
		}
		if len(acting) == 0 {
			continue
		}
		w("%s", t.SpineTaskLine(ti+1, task.Title))
		for _, s := range acting {
			renderSpineStep(w, s, repeat[s.Seq], hit[s.Seq], t)
		}
	}
}

// renderSpineStep renders one tool-calling Step's decision-spine block —
// see renderDecisionSpine.
func renderSpineStep(w func(string, ...any), s *Step, repeated, flagged bool, t i18n.SpineText) {
	header := "**" + stepRoleTag(s, repeated, t) + " Step " + strconv.Itoa(s.Seq) + " · " +
		s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05") + "**"
	if flagged {
		header += t.SpineFindingTag
	}
	header += "\n\n"
	w("%s", header)
	w("%s", spineWhyLine(s))
	for _, tc := range s.ToolCalls {
		w("%s", toolCallLine(tc, t))
	}
}

// --- Step role tags ----------------------------------------------------------

// stepRoleTag classifies s into one of 7 roles by already-computed
// structural signals only — no new judgment, just picking which existing
// fact to surface first. Priority order (first match wins) reflects how
// noteworthy each signal is: a compaction boundary or an error marker is
// worth flagging before a routine retry or plan/report distinction.
func stepRoleTag(s *Step, isRepeat bool, t i18n.SpineText) string {
	if s.StitchEdge != nil {
		return t.StepTagCompaction
	}
	for _, ev := range s.NewEvents {
		if strings.Contains(ev.Msg.Text, isErrorMarker) {
			return t.StepTagError
		}
	}
	if isRepeat {
		return t.StepTagRetry
	}
	if len(s.ToolCalls) > 0 {
		return t.StepTagAction
	}
	if numberedListRe.MatchString(s.Reasoning) || numberedListRe.MatchString(s.RespText) {
		return t.StepTagPlan
	}
	if s.RespText != "" {
		return t.StepTagReport
	}
	return t.StepTagObserve
}

// --- tool-call timeline -------------------------------------------------------

func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(r))
}

// renderToolTimeline renders the tool-call timeline: one row per distinct tool name, one column
// per Step, ● for a normal call, 🔄 when toolCallRepeats flagged that
// Step's call as a repeat, ❌ when the Step also carries an error marker —
// same underlying data the per-Step role tag uses, just laid out across the
// whole Journey at once so a reader can spot a burst pattern that a linear
// per-Step read doesn't surface as clearly.
func renderToolTimeline(w func(string, ...any), j *Journey, lang i18n.Lang) {
	t := i18n.Spine(lang)
	steps := journeySteps(j)
	w("%s", t.TimelineTitle)
	if len(steps) == 0 {
		w("%s", t.TimelineNoData)
		return
	}

	repeatAt := map[int]bool{}
	for _, o := range toolCallRepeats(steps) {
		if o.IsRepeat {
			repeatAt[o.StepSeq] = true
		}
	}
	errAt := map[int]bool{}
	for _, s := range steps {
		for _, ev := range s.NewEvents {
			if strings.Contains(ev.Msg.Text, isErrorMarker) {
				errAt[s.Seq] = true
				break
			}
		}
	}

	toolAt := map[string]map[int]bool{}
	var names []string
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if toolAt[tc.Name] == nil {
				toolAt[tc.Name] = map[int]bool{}
				names = append(names, tc.Name)
			}
			toolAt[tc.Name][s.Seq] = true
		}
	}
	if len(names) == 0 {
		w("%s", t.TimelineNoData)
		return
	}
	sort.Strings(names)

	maxNameLen := 0
	for _, n := range names {
		if l := len([]rune(n)); l > maxNameLen {
			maxNameLen = l
		}
	}

	w("%s", t.TimelineLegend)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(padRight(n, maxNameLen))
		b.WriteString(" ")
		for _, s := range steps {
			switch {
			case toolAt[n][s.Seq] && errAt[s.Seq]:
				b.WriteString("❌")
			case toolAt[n][s.Seq] && repeatAt[s.Seq]:
				b.WriteString("🔄")
			case toolAt[n][s.Seq]:
				b.WriteString("●")
			default:
				b.WriteString("·")
			}
		}
		b.WriteByte('\n')
	}
	w("%s", codeFence(b.String()))
}

// --- Findings section --------------------------------------------------

func joinInts(ns []int) string {
	ss := make([]string, len(ns))
	for i, n := range ns {
		ss[i] = strconv.Itoa(n)
	}
	return strings.Join(ss, ", ")
}

// renderFindingsSection renders findings.go's candidate list — the one
// place in the document every Finding's full text (not just the ⚠️ mark
// the decision spine adds) is shown.
func renderFindingsSection(w func(string, ...any), findings []Finding, lang i18n.Lang) {
	t := i18n.Spine(lang)
	w("%s", t.FindingsTitle)
	if len(findings) == 0 {
		w("%s", t.FindingsNone)
		return
	}
	for i, f := range findings {
		w("%s", t.FindingHeader(i+1, string(f.Code), f.StepSeq))
		if len(f.RelatedSeq) > 0 {
			w("%s", t.FindingRelated(joinInts(f.RelatedSeq)))
		}
		if f.Evidence != "" {
			w("%s", t.FindingEvidence(f.Evidence))
		}
		if f.Action != "" {
			w("%s", t.FindingAction(f.Action))
		}
		w("\n")
	}
}
