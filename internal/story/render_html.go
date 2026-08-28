// Ver 2026-08-28, by Sonnet 5

// RenderHTML: the single-file HTML journey view (`vmr analyze -journey <id>
// -html`, optionally `-redact`). A self-contained document — inline CSS, one
// small inline script, zero external requests — with a sticky left timeline
// and a right-hand waterfall of Step cards. Pure view over the same
// *story.Journey the Markdown renderer walks (no re-parsing, no new
// judgment); redact mode swaps every conversation body for a length
// placeholder while keeping structure, roles, token counts and tool names.
package story

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RenderHTML renders j as one self-contained HTML document in lang. m and
// findings are accepted for parity with RenderMarkdown's signature and to
// leave room for a findings panel later; the MVP view uses j alone.
func RenderHTML(j *Journey, m Metrics, findings []Finding, lang i18n.Lang, redact bool) string {
	t := i18n.StoryHTML(lang)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	allSteps := flattenSteps(j)

	w("<!doctype html>\n<html lang=%q>\n<head>\n<meta charset=\"utf-8\">\n", htmlLangAttr(lang))
	w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	w("<title>%s</title>\n<style>\n%s</style>\n</head>\n<body>\n", he(t.DocTitle(j.ID)), htmlStyle)

	w("<header class=\"jhead\">\n<h1>%s</h1>\n", he(t.DocTitle(j.ID)))
	w("<p class=\"subtitle\">%s</p>\n", he(t.Subtitle(len(j.Tasks), len(allSteps),
		j.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04"),
		j.To.In(fmtutil.DisplayZone).Format("2006-01-02 15:04"))))
	w("<blockquote>%s</blockquote>\n", bodyText(j.Title, redact, t))
	if redact {
		w("<div class=\"banner redact\">%s</div>\n", he(t.RedactedBanner))
	}
	if j.Partial {
		w("<div class=\"banner warn\">%s</div>\n", he(t.PartialBanner))
	}
	if j.Break != nil {
		w("<div class=\"banner warn\">%s</div>\n", he(t.BreakBanner(j.Break.Edit.Kind.String())))
	}
	w("</header>\n")

	w("<div class=\"layout\">\n")
	htmlTimeline(w, j, t)
	w("<main class=\"cards\">\n")
	step := 0
	for ti, task := range j.Tasks {
		w("<section class=\"task\" id=\"task-%d\">\n<h2>%s <span class=\"tasktitle\">%s</span></h2>\n",
			ti+1, he(t.TaskLabel(ti+1)), bodyText(task.Title, redact, t))
		for range task.Steps {
			htmlStepCard(w, allSteps, step, t, redact)
			step++
		}
		w("</section>\n")
	}
	w("</main>\n</div>\n")

	w("<footer class=\"gennote\">%s</footer>\n", he(t.GeneratedNote))
	w("<script>\n%s</script>\n</body>\n</html>\n", htmlScript)
	return b.String()
}

func flattenSteps(j *Journey) []*Step {
	var out []*Step
	for _, task := range j.Tasks {
		out = append(out, task.Steps...)
	}
	return out
}

func htmlLangAttr(lang i18n.Lang) string {
	if lang == i18n.ZH {
		return "zh"
	}
	return "en"
}

func htmlTimeline(w func(string, ...any), j *Journey, t i18n.StoryHTMLText) {
	w("<nav class=\"timeline\">\n<h3>%s</h3>\n<ol>\n", he(t.TimelineTitle))
	step := 0
	for ti, task := range j.Tasks {
		w("<li class=\"tl-task\"><a href=\"#task-%d\">%s</a><ol>\n", ti+1, he(t.TaskLabel(ti+1)))
		for range task.Steps {
			step++
			w("<li><a href=\"#step-%d\">%s</a></li>\n", step, he(t.StepLabel(step)))
		}
		w("</ol></li>\n")
	}
	w("</ol>\n</nav>\n")
}

func htmlStepCard(w func(string, ...any), steps []*Step, i int, t i18n.StoryHTMLText, redact bool) {
	s := steps[i]
	w("<article class=\"card\" id=\"step-%d\">\n", s.Seq)

	// Header: step number, time, model, attempt badge.
	w("<div class=\"cardhead\">\n<span class=\"seq\">%s</span>\n", he(t.StepLabel(s.Seq)))
	if s.Rec != nil {
		w("<span class=\"ts\">%s</span>\n", he(s.Rec.TS.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")))
		if s.Rec.Model != "" {
			w("<span class=\"model\">%s</span>\n", he(s.Rec.Model))
		}
		if n := len(s.Rec.Attempts); n > 1 {
			w("<span class=\"badge failover\">%s · %s</span>\n", he(t.FailoverBadge), he(t.Attempts(n)))
			if last := s.Rec.Attempts[n-1]; last.Endpoint != "" {
				w("<span class=\"endpoint\">→ %s</span>\n", he(last.Endpoint))
			}
		} else if n == 1 && s.Rec.Attempts[0].Endpoint != "" {
			w("<span class=\"endpoint\">%s</span>\n", he(s.Rec.Attempts[0].Endpoint))
		}
	}
	w("</div>\n")

	htmlTransitions(w, s, t)

	if s.HumanInitiated && s.Instruction != "" {
		w("<div class=\"instruction\"><span class=\"lbl\">%s</span>%s</div>\n",
			he(t.Instruction), bodyText(s.Instruction, redact, t))
	}

	htmlTurnMessages(w, s, t, redact)
	htmlResponse(w, steps, i, t, redact)

	w("</article>\n")
}

func htmlTransitions(w func(string, ...any), s *Step, t i18n.StoryHTMLText) {
	if s.Edge != nil && s.Edge.Kind.String() != "append" {
		w("<div class=\"marker edit\">%s</div>\n", he(t.EditLabel(s.Edge.Kind.String())))
	}
	if s.StitchEdge != nil {
		w("<div class=\"marker stitch\">%s</div>\n", he(t.StitchLabel(
			s.StitchEdge.Kind.String(), pctStr(s.StitchEdge.Score), pctStr(s.StitchEdge.Confidence))))
	}
	if s.SysChanged {
		w("<div class=\"marker sys\">%s</div>\n", he(t.SysChanged))
	}
	if c := s.Compaction; c != nil {
		w("<div class=\"marker compaction\">\n<div class=\"chdr\">%s</div>\n", he(t.CompactionHdr(c.TokensBefore, c.TokensAfter)))
		htmlEntityList(w, t.Swallowed, c.SwallowedEntities)
		htmlEntityList(w, t.Survived, c.SurvivedEntities)
		w("</div>\n")
	}
}

func htmlEntityList(w func(string, ...any), label string, ents []string) {
	if len(ents) == 0 {
		return
	}
	w("<div class=\"entities\"><span class=\"lbl\">%s</span> ", he(label))
	for i, e := range ents {
		if i > 0 {
			w(", ")
		}
		w("<code>%s</code>", he(e))
	}
	w("</div>\n")
}

func htmlTurnMessages(w func(string, ...any), s *Step, t i18n.StoryHTMLText, redact bool) {
	var shown []*Event
	for _, ev := range s.NewEvents {
		if ev.Msg.Role == "assistant" {
			continue // the model's own output is rendered in the Response block
		}
		shown = append(shown, ev)
	}
	if len(shown) == 0 {
		return
	}
	w("<details class=\"turn\" open>\n<summary>%s (%d)</summary>\n", he(t.InThisTurn), len(shown))
	for _, ev := range shown {
		w("<div class=\"msg role-%s\">\n<span class=\"role\">%s</span>\n<div class=\"body\">%s</div>\n</div>\n",
			he(sanitizeRole(ev.Msg.Role)), he(t.RoleLabel(ev.Msg.Role)), bodyText(ev.Msg.Text, redact, t))
	}
	w("</details>\n")
}

func htmlResponse(w func(string, ...any), steps []*Step, i int, t i18n.StoryHTMLText, redact bool) {
	s := steps[i]
	if s.Reasoning == "" && s.RespText == "" && len(s.ToolCalls) == 0 && !s.NoReply {
		return
	}
	w("<div class=\"response\">\n<div class=\"lbl\">%s</div>\n", he(t.Response))
	if s.Reasoning != "" {
		w("<details class=\"reasoning\">\n<summary>%s</summary>\n<div class=\"body\">%s</div>\n</details>\n",
			he(t.Reasoning), bodyText(s.Reasoning, redact, t))
	}
	if s.RespText != "" {
		w("<div class=\"resptext\">%s</div>\n", bodyText(s.RespText, redact, t))
	}
	if len(s.ToolCalls) > 0 {
		matched := toolResultsFor(steps, i)
		byID := make(map[string]string, len(matched))
		errByID := make(map[string]bool, len(matched))
		for _, r := range matched {
			byID[r.CallID] = r.Text
			errByID[r.CallID] = r.IsError
		}
		for _, tc := range s.ToolCalls {
			w("<div class=\"tool\">\n<div class=\"toolname\"><span class=\"lbl\">%s</span> <code>%s</code></div>\n",
				he(t.ToolCall), he(tc.Name))
			w("<details class=\"toolargs\">\n<summary>args</summary>\n<div class=\"body\">%s</div>\n</details>\n",
				jsonBody(tc.Args, redact, t))
			if res, ok := byID[tc.ID]; ok {
				cls := "toolresult"
				if errByID[tc.ID] {
					cls += " err"
				}
				w("<details class=\"%s\">\n<summary>%s</summary>\n<div class=\"body\">%s</div>\n</details>\n",
					cls, he(t.ToolResult), bodyText(res, redact, t))
			}
		}
	}
	if s.NoReply {
		w("<div class=\"noreply\">%s</div>\n", he(t.NoReply))
	}
	w("</div>\n")
}

// --- body rendering / redaction ---

// bodyText renders arbitrary conversation text as an escaped block, or — in
// redact mode — as a length placeholder that keeps "how much was here"
// without any of the content.
func bodyText(s string, redact bool, t i18n.StoryHTMLText) string {
	if strings.TrimSpace(s) == "" {
		return "<span class=\"empty\">" + he(t.Empty) + "</span>"
	}
	if redact {
		return "<span class=\"placeholder\">" + he(t.RedactedText(utf8.RuneCountInString(s))) + "</span>"
	}
	return "<pre class=\"text\">" + he(s) + "</pre>"
}

func jsonBody(s string, redact bool, t i18n.StoryHTMLText) string {
	if strings.TrimSpace(s) == "" {
		return "<span class=\"empty\">" + he(t.Empty) + "</span>"
	}
	if redact {
		return "<span class=\"placeholder\">" + he(t.RedactedJSON(utf8.RuneCountInString(s))) + "</span>"
	}
	return "<pre class=\"json\">" + he(s) + "</pre>"
}

func he(s string) string { return html.EscapeString(s) }

// sanitizeRole keeps a role usable as a CSS class suffix (role-user etc.)
// without trusting it — an upstream could put anything in "role".
func sanitizeRole(r string) string {
	var b strings.Builder
	for _, c := range r {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
