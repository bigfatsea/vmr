// Ver 2026-08-28, by Sonnet 5

// The Structure / Metrics / Findings sections of the journey dashboard
// (render_html.go). Split out to keep each file under archtest's line
// budget. Pure view over *story.Journey / Metrics / []Finding — no I/O, no
// judgment.
package story

import (
	"strconv"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

const dashMaxToolChips = 4

// htmlStructure renders the Task -> Step skeleton as a compact row-per-Step
// timeline: seq / time / model / tool chips / failover + finding badges /
// transition markers, each row anchored (#step-N) and — outside redact
// mode — linking to that Step's details/*.md.
func htmlStructure(w func(string, ...any), j *Journey, allSteps []*Step, findings []Finding, t i18n.StoryHTMLText, redact bool) {
	if len(allSteps) == 0 {
		w("<p class=\"empty\">%s</p>\n", he(t.StructNoSteps))
		return
	}
	flagged := map[int]bool{}
	for _, f := range findings {
		flagged[f.StepSeq] = true
		for _, r := range f.RelatedSeq {
			flagged[r] = true
		}
	}
	step := 0
	for ti, task := range j.Tasks {
		w("<div class=\"task\" id=\"task-%d\">\n<h3>%s · <span class=\"tt\">%s</span></h3>\n",
			ti+1, he(t.TaskLabel(ti+1)), bodyOrPlaceholder(task.Title, redact, t))
		for range task.Steps {
			htmlStepRow(w, allSteps[step], flagged[allSteps[step].Seq], t, redact)
			step++
		}
		w("</div>\n")
	}
}

func htmlStepRow(w func(string, ...any), s *Step, flagged bool, t i18n.StoryHTMLText, redact bool) {
	cls := "srow"
	if flagged {
		cls += " flagged"
	}
	if s.Outcome == "error" {
		cls += " error"
	}
	w("<article class=\"%s\" id=\"step-%d\">\n<div class=\"top\">\n", cls, s.Seq)
	w("<span class=\"seq\">%s</span>\n", he(t.StepLabel(s.Seq)))
	if s.Manifest != nil {
		w("<span class=\"ts\">%s</span>\n", he(s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")))
		if s.Manifest.Model != "" {
			w("<span class=\"model\">%s</span>\n", he(s.Manifest.Model))
		}
		if s.Outcome == "error" {
			errText := "error"
			if s.ErrorClass != "" {
				errText = s.ErrorClass
			}
			w("<span class=\"chip badge error\">❌ %s</span>\n", he(errText))
		} else if n := len(s.Attempts); n > 1 {
			w("<span class=\"chip badge\">%s · %s</span>\n", he(t.FailoverBadge), he(t.Attempts(n)))
		}
	}
	tools := dedupToolNames(s)
	shown := tools
	if len(shown) > dashMaxToolChips {
		shown = shown[:dashMaxToolChips]
	}
	for _, name := range shown {
		w("<span class=\"chip tool\">%s</span>\n", he(name))
	}
	if extra := len(tools) - len(shown); extra > 0 {
		w("<span class=\"chip\">%s</span>\n", he(t.MoreTools(extra)))
	}
	if flagged {
		w("<span class=\"chip flag\">⚠</span>\n")
	}
	w("</div>\n")

	htmlStepMarkers(w, s, t, redact)

	if s.HumanInitiated && s.Instruction != "" {
		w("<details>\n<summary>%s</summary>\n<div class=\"said\">%s</div>\n</details>\n",
			he(t.Instruction), bodyText(s.Instruction, redact, t))
	}
	if said := dashStepSaid(s); said != "" {
		w("<details>\n<summary>%s</summary>\n<div class=\"said\">%s</div>\n</details>\n",
			he(t.StepSaid), bodyText(said, redact, t))
	}
	if s.NoReply {
		w("<div class=\"markers\"><span class=\"marker\">%s</span></div>\n", he(t.NoReply))
	}

	if s.Manifest != nil {
		if redact {
			w("<div class=\"coord\">%s</div>\n", he(spineCoord(s.Manifest)))
		} else {
			w("<div><a class=\"coord\" href=\"../details/%s\">%s →</a></div>\n",
				he(reqdetail.FileNameForManifest(s.Manifest)), he(spineCoord(s.Manifest)))
		}
	}
	w("</article>\n")
}

func htmlStepMarkers(w func(string, ...any), s *Step, t i18n.StoryHTMLText, redact bool) {
	var ms []string
	if s.Edge != nil && s.Edge.Kind.String() != "append" {
		ms = append(ms, he(t.EditLabel(s.Edge.Kind.String())))
	}
	if s.StitchEdge != nil {
		ms = append(ms, he(t.StitchLabel(s.StitchEdge.Kind.String(), pctStr(s.StitchEdge.Score), pctStr(s.StitchEdge.Confidence))))
	}
	if s.SysChanged {
		ms = append(ms, he(t.SysChanged))
	}
	if c := s.Compaction; c != nil {
		ms = append(ms, he(t.CompactionShort(c.TokensBefore, c.TokensAfter)))
		if redact {
			ms = append(ms, he(t.EntityCounts(len(c.SwallowedEntities), len(c.SurvivedEntities))))
		} else {
			if len(c.SwallowedEntities) > 0 {
				ms = append(ms, he(t.Swallowed)+": "+entityCodes(c.SwallowedEntities))
			}
			if len(c.SurvivedEntities) > 0 {
				ms = append(ms, he(t.Survived)+": "+entityCodes(c.SurvivedEntities))
			}
		}
	}
	if len(ms) == 0 {
		return
	}
	w("<div class=\"markers\">")
	for _, m := range ms {
		w("<span class=\"marker\">%s</span>", m)
	}
	w("</div>\n")
}

func entityCodes(ents []string) string {
	parts := make([]string, len(ents))
	for i, e := range ents {
		parts[i] = "<code>" + he(e) + "</code>"
	}
	return strings.Join(parts, " ")
}

func dedupToolNames(s *Step) []string {
	seen := map[string]bool{}
	var out []string
	for _, tc := range s.ToolCalls {
		if tc.Name == "" || seen[tc.Name] {
			continue
		}
		seen[tc.Name] = true
		out = append(out, tc.Name)
	}
	return out
}

// dashStepSaid is the model's own stated reply or reasoning for this Step
// — a short "why", not the full turn. Mirrors spineWhyLine's field
// preference (RespText first, then Reasoning).
func dashStepSaid(s *Step) string {
	if s.RespText != "" {
		return s.RespText
	}
	return s.Reasoning
}

func bodyOrPlaceholder(title string, redact bool, t i18n.StoryHTMLText) string {
	if strings.TrimSpace(title) == "" {
		return "<span class=\"empty\">" + he(t.Empty) + "</span>"
	}
	if redact {
		return "<span class=\"placeholder\">" + he(t.RedactedText(len([]rune(title)))) + "</span>"
	}
	return he(title)
}

// --- Metrics ---

// htmlMetrics renders the metrics block of the journey dashboard. The row
// set comes from journeyMetrics (compare_metrics.go) — the same slice
// renderBehaviorIndicators (Markdown) iterates — so the two formats can
// never drift apart about which metrics a journey view shows; this
// function only decides how one HTML stat cell is rendered (§2.81).
func htmlMetrics(w func(string, ...any), m Metrics, isNonAnthropic bool, lang i18n.Lang, t i18n.StoryHTMLText) {
	stat := func(k, v string) {
		w("<div class=\"stat\"><div class=\"k\">%s</div><div class=\"v\">%s</div></div>\n", he(k), he(v))
	}

	w("<div class=\"stats\">\n")
	for _, jm := range journeyMetrics {
		v := jm.Value(m)
		stat(i18n.MetricLabel(lang, string(jm.Code)), jm.Format(m, v, isNonAnthropic))
	}
	w("</div>\n")

	htmlContextSparkline(w, m.ContextCurve, t)
}

// htmlContextSparkline draws total context tokens per Step as an inline
// SVG polyline — no axes, no library, just the shape of the growth.
func htmlContextSparkline(w func(string, ...any), curve []ContextPoint, t i18n.StoryHTMLText) {
	if len(curve) < 2 {
		return
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
		return
	}
	const vw, vh = 600.0, 64.0
	var pts strings.Builder
	for i, v := range totals {
		x := vw * float64(i) / float64(len(totals)-1)
		y := vh - vh*float64(v)/float64(maxT)
		if i > 0 {
			pts.WriteByte(' ')
		}
		pts.WriteString(strconv.FormatFloat(x, 'f', 1, 64))
		pts.WriteByte(',')
		pts.WriteString(strconv.FormatFloat(y, 'f', 1, 64))
	}
	w("<div class=\"spark\">\n<h4>%s</h4>\n", he(t.SparkContextTitle))
	w("<svg viewBox=\"0 0 %.0f %.0f\" preserveAspectRatio=\"none\" role=\"img\">\n", vw, vh)
	w("<polyline fill=\"none\" stroke=\"currentColor\" stroke-width=\"2\" points=\"%s\"/>\n</svg>\n", pts.String())
	w("<div class=\"cap\">%s</div>\n</div>\n", he(t.SparkContextCaption(totals[0], totals[len(totals)-1])))
}

// --- Findings ---

func htmlFindings(w func(string, ...any), findings []Finding, t i18n.StoryHTMLText, redact bool) {
	if len(findings) == 0 {
		w("<p class=\"empty\">%s</p>\n", he(t.NoFindings))
		return
	}
	if redact {
		w("<p class=\"empty\">%s</p>\n", he(t.RedactedFindingsNote))
	}
	for _, f := range findings {
		w("<div class=\"finding\">\n<div class=\"fh\">\n")
		w("<span class=\"code\">%s</span>\n", he(string(f.Code)))
		w("<span class=\"src\">%s</span>\n", he(findingSourceLabel(f, t)))
		if f.Source == SourceLLMInferred && f.Confidence != "" {
			w("<span class=\"src\">%s</span>\n", he(t.FindingConfidence(string(f.Confidence))))
		}
		w("<a href=\"#step-%d\">%s</a>\n", f.StepSeq, he(t.FindingAtStep(f.StepSeq)))
		w("</div>\n")
		if len(f.RelatedSeq) > 0 {
			w("<div class=\"sub\">%s</div>\n", he(t.FindingRelatedSteps(joinInts(f.RelatedSeq))))
		}
		if !redact {
			if f.Finding != "" {
				w("<div class=\"txt\">%s</div>\n", he(f.Finding))
			}
			if f.Evidence != "" {
				w("<div class=\"sub\"><span class=\"l\">%s</span>%s</div>\n", he(t.FindingEvidenceLabel), he(f.Evidence))
			}
			if f.Action != "" {
				w("<div class=\"sub\"><span class=\"l\">%s</span>%s</div>\n", he(t.FindingActionLabel), he(f.Action))
			}
		}
		w("</div>\n")
	}
}

func findingSourceLabel(f Finding, t i18n.StoryHTMLText) string {
	if f.Source == SourceLLMInferred {
		return t.FindingSourceLLM
	}
	return t.FindingSourceRule
}
