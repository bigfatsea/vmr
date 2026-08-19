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
// chatmsg/pairing.go's doc comment).
package story

import (
	"strconv"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// spineWhyRespCap/spineWhyReasoningCap gate foldWhyLine's inline-vs-fold
// decision — generous for RespText (in practice always a short one-sentence
// plan/decision, e.g. "akshare 1.18.79 可用！直接用 Python 获取数据。" — this only
// guards against an unusually verbose reply), tighter for a Reasoning
// excerpt (that field can run to paragraphs). Neither caps content anymore:
// past this length the text folds (collapsed, one click away, complete),
// it never gets cut.
const (
	spineWhyRespCap      = 400
	spineWhyReasoningCap = 200
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
// than claiming "no result" where none was ever recorded.
func toolResultLine(name string, r chatmsg.ToolResult, t i18n.SpineText) string {
	if r.Text == "" {
		return ""
	}
	mark := "↩️"
	if r.IsError {
		mark = "❌"
	}
	return mark + " `" + name + "`: <details><summary>" + oneLineTruncate(r.Text, spinePreviewLen) + "</summary>\n\n" +
		codeFence(capFull(r.Text, t)) + "\n</details>\n\n"
}

// renderDecisionSpine renders the decision spine: one block per Task, one
// sub-block per tool-calling Step — Step number/time (role-tagged the same
// way the fact layer's own Step headers are just below, see stepRoleTag —
// 🔄 for a Step toolCallRepeats flagged as an exact repeat is that tag,
// not a second marker here), the model's own stated reason for what it's
// about to do when it said one (spineWhyLine), then every tool call that
// Step made — complete (toolCallLine) — each immediately followed by its
// own paired result when one was recorded (toolResultLine). A Step
// referenced (as StepSeq or RelatedSeq) by any Finding earns an extra ⚠️ on
// its header. Renders nothing for a Journey with no tool calls at all (a
// pure Q&A Journey has no "decisions" to spine) — and, within it, skips any
// Task whose Steps never called a tool either.
func renderDecisionSpine(w func(string, ...any), j *Journey, findings []Finding, lang i18n.Lang) {
	t := i18n.Spine(lang)
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

	anyCalls := false
	for _, s := range steps {
		if len(s.ToolCalls) > 0 {
			anyCalls = true
			break
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
			renderSpineStep(w, s, toolResultsFor(steps, idxOf[s]), repeat[s.Seq], hit[s.Seq], t)
		}
	}
}

// renderSpineStep renders one tool-calling Step's decision-spine block —
// see renderDecisionSpine. results pairs by ToolCall.ID (map built once per
// Step, small — a Step's own ToolCalls count, never the whole Journey's).
func renderSpineStep(w func(string, ...any), s *Step, results []chatmsg.ToolResult, repeated, flagged bool, t i18n.SpineText) {
	header := "**" + stepRoleTag(s, repeated, t) + " Step " + strconv.Itoa(s.Seq) + " · " +
		s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05") + "**"
	if flagged {
		header += t.SpineFindingTag
	}
	header += "\n\n"
	w("%s", header)
	w("%s", spineWhyLine(s))

	byID := make(map[string]chatmsg.ToolResult, len(results))
	for _, r := range results {
		byID[r.CallID] = r
	}
	for _, tc := range s.ToolCalls {
		w("%s", toolCallLine(tc, t))
		if r, ok := byID[tc.ID]; ok {
			w("%s", toolResultLine(tc.Name, r, t))
		}
	}
}
