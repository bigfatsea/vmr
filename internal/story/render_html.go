// Ver 2026-08-28, by Sonnet 5

// RenderHTML: the single-file HTML journey dashboard (`vmr analyze -journey
// <id> -html`, optionally `-redact`). A self-contained document — inline
// CSS, one small inline script, zero external requests — built for sharing
// and fast comprehension rather than as a transcript: a verdict line, the
// Task/Step structure as a compact visual timeline, the behavior metrics
// (with an inline SVG sparkline), and the Findings — with per-Step links
// out to details/*.md for the full record rather than inlining
// conversation bodies. Pure view over the same *story.Journey / Metrics /
// []Finding the Markdown renderer walks (no re-parsing, no new judgment);
// redact mode swaps every conversation body for a length placeholder,
// drops the detail links, and hides finding text — keeping structure,
// roles, token counts and tool names.
package story

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RenderHTML renders j as one self-contained HTML dashboard in lang.
func RenderHTML(j *Journey, m Metrics, findings []Finding, lang i18n.Lang, redact bool) string {
	t := i18n.StoryHTML(lang)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	allSteps := flattenSteps(j)

	w("<!doctype html>\n<html lang=%q>\n<head>\n<meta charset=\"utf-8\">\n", htmlLangAttr(lang))
	w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	w("<title>%s</title>\n<style>\n%s</style>\n</head>\n<body>\n<div class=\"wrap\">\n", he(t.DocTitle(j.ID)), htmlStyle)

	htmlHeader(w, j, allSteps, t, redact)

	w("<div class=\"layout\">\n<div class=\"railwrap\">\n")
	htmlRail(w, j, t)
	w("</div>\n<main class=\"body\">\n")

	w("<section class=\"block\" id=\"structure\">\n<h2>%s</h2>\n", he(t.SectionStructure))
	htmlStructure(w, j, allSteps, findings, t, redact)
	w("</section>\n")

	w("<section class=\"block\" id=\"metrics\">\n<h2>%s</h2>\n", he(t.SectionMetrics))
	htmlMetrics(w, m, t)
	w("</section>\n")

	w("<section class=\"block\" id=\"findings\">\n<h2>%s</h2>\n", he(t.SectionFindings))
	htmlFindings(w, findings, t, redact)
	w("</section>\n")

	w("</main>\n</div>\n")
	w("<footer class=\"gennote\">%s</footer>\n</div>\n", he(t.GeneratedNote))
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

// htmlHeader renders the verdict screen: identity, title, banners, and a
// one-line outcome (the final deliverable if deliverableStats found one,
// else the termination finish reason).
func htmlHeader(w func(string, ...any), j *Journey, allSteps []*Step, t i18n.StoryHTMLText, redact bool) {
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

	w("<div class=\"outcome\"><span class=\"lbl\">%s</span> ", he(t.OutcomeLabel))
	if d := deliverableStats(j); d.Found {
		w("%s <code>%s</code>", he(t.OutcomeDeliverable(d.StepSeq)), he(d.ToolName))
	} else if fin := htmlLastFinish(allSteps); fin != "" {
		w("%s", he(t.OutcomeTermination(fin)))
	} else {
		w("%s", he(t.OutcomeUnknown))
	}
	w("</div>\n</header>\n")
}

func htmlLastFinish(steps []*Step) string {
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Finish
}

// htmlRail renders the sticky left navigation: the four section anchors,
// with every Step anchor nested under "Structure".
func htmlRail(w func(string, ...any), j *Journey, t i18n.StoryHTMLText) {
	w("<nav class=\"rail\">\n<ol>\n")
	w("<li><a href=\"#structure\">%s</a>\n<ol class=\"steps\">\n", he(t.SectionStructure))
	step := 0
	for ti, task := range j.Tasks {
		w("<li><a href=\"#task-%d\">%s</a></li>\n", ti+1, he(t.TaskLabel(ti+1)))
		for range task.Steps {
			step++
			w("<li><a href=\"#step-%d\">%s</a></li>\n", step, he(t.StepLabel(step)))
		}
	}
	w("</ol>\n</li>\n")
	w("<li><a href=\"#metrics\">%s</a></li>\n", he(t.SectionMetrics))
	w("<li><a href=\"#findings\">%s</a></li>\n", he(t.SectionFindings))
	w("</ol>\n</nav>\n")
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
	return "<pre>" + he(s) + "</pre>"
}

func he(s string) string { return html.EscapeString(s) }
