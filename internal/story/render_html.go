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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RenderHTML renders j as one self-contained HTML incident report in lang.
// cost is threaded in (not computed here) because it needs a
// *pricing.Resolver — a zero CostFact{Resolved:false} is the "no price book"
// case and simply drops the $ figure from the damage line.
func RenderHTML(j *Journey, m Metrics, findings []Finding, cost CostFact, lang i18n.Lang, redact bool) string {
	t := i18n.StoryHTML(lang)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	allSteps := journeySteps(j)
	ponr := ComputePointOfNoReturn(j, findings)
	sev, sevDriver, sevDriverLowConf := JourneySeverity(findings)

	w("<!doctype html>\n<html lang=%q>\n<head>\n<meta charset=\"utf-8\">\n", htmlLangAttr(lang))
	w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	w("<title>%s</title>\n<style>\n%s</style>\n</head>\n<body>\n<div class=\"wrap\">\n", he(t.DocTitle(j.ID)), htmlStyle)

	htmlHeader(w, j, allSteps, m, findings, cost, sev, sevDriver, sevDriverLowConf, ponr, t, redact)

	w("<div class=\"layout\">\n<div class=\"railwrap\">\n")
	htmlRail(w, j, t)
	w("</div>\n<main class=\"body\">\n")

	w("<section class=\"block\" id=\"structure\">\n<h2>%s</h2>\n", he(t.SectionStructure))
	htmlStructure(w, j, allSteps, findings, t, redact)
	w("</section>\n")

	w("<section class=\"block\" id=\"metrics\">\n<h2>%s</h2>\n", he(t.SectionMetrics))
	_, isNonAnthropic := journeyAnthropicCoverageCodes(j)
	htmlMetrics(w, m, isNonAnthropic, t)
	w("</section>\n")

	w("<section class=\"block\" id=\"findings\">\n<h2>%s</h2>\n", he(t.SectionFindings))
	if codes, ok := journeyAnthropicCoverageCodes(j); ok {
		w("<blockquote class=\"coverage-note\">%s</blockquote>\n", he(t.AnthropicCoverageNote(codes)))
	}
	htmlFindings(w, findings, t, redact)
	w("</section>\n")

	w("</main>\n</div>\n")
	w("<footer class=\"gennote\">%s</footer>\n</div>\n", he(t.GeneratedNote))
	w("<script>\n%s</script>\n</body>\n</html>\n", htmlScript)
	return b.String()
}

func htmlLangAttr(lang i18n.Lang) string {
	if lang == i18n.ZH {
		return "zh"
	}
	return "en"
}

// htmlHeader renders the incident-report front page: the recorder bar, the
// PROBABLE CAUSE verdict panel (severity stamp + the finding that drove it +
// a one-line damage tally), the title/subtitle, banners, the point-of-no-
// return strip, and the one-line outcome. Redact-safe: the verdict panel and
// PONR strip use structure/counts only; the driver finding's narrative text
// is shown under -redact as a bare code + step anchor, matching htmlFindings.
func htmlHeader(w func(string, ...any), j *Journey, allSteps []*Step, m Metrics, findings []Finding, cost CostFact, sev string, sevDriver FindingCode, sevDriverLowConf bool, ponr *PointOfNoReturn, t i18n.StoryHTMLText, redact bool) {
	w("<div class=\"recbar\"><span class=\"rl\">%s</span><span class=\"jid\">%s</span></div>\n",
		he(t.RecorderBar), he(j.ID))
	w("<header class=\"jhead\">\n")

	w("<div class=\"verdict v-%s\">\n", he(sev))
	w("<div class=\"vtop\"><span class=\"vlabel\">%s</span><span class=\"vstamp\">%s</span></div>\n",
		he(t.VerdictProbableCause), he(t.VerdictStamp(sev)))
	w("<div class=\"vcause\">%s</div>\n", verdictCause(findings, sevDriver, sevDriverLowConf, t, redact))
	w("<div class=\"damage\">%s%s</div>\n", he(t.DamageLine(len(allSteps),
		fmtSpan(time.Duration(m.NetWorkingMS)*time.Millisecond),
		fmtutil.FmtTokens(journeyTokenTotal(m)))),
		htmlDamageCost(cost, t))
	w("</div>\n")

	w("<h1>%s</h1>\n", htmlTitleText(j.Title, redact, t))
	w("<p class=\"subtitle\">%s</p>\n", he(t.Subtitle(len(j.Tasks), len(allSteps),
		j.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04"),
		j.To.In(fmtutil.DisplayZone).Format("2006-01-02 15:04"))))
	if redact {
		w("<div class=\"banner redact\">%s</div>\n", he(t.RedactedBanner))
	}
	if j.Partial {
		w("<div class=\"banner warn\">%s</div>\n", he(t.PartialBanner))
	}
	if j.Break != nil {
		w("<div class=\"banner warn\">%s</div>\n", he(t.BreakBanner(j.Break.Edit.Kind.String())))
	}

	htmlPointOfNoReturn(w, ponr, allSteps, sev, t)

	w("<div class=\"outcome\"><span class=\"lbl\">%s</span> ", he(t.OutcomeLabel))
	if d := deliverableStats(j); d.Found {
		w("%s <code>%s</code>", he(t.OutcomeDeliverable(d.StepSeq)), he(d.ToolName))
	} else if fin := htmlLastFinish(allSteps); fin != "" {
		w("%s", he(t.OutcomeTermination(fin)))
	} else if len(allSteps) > 0 && allSteps[len(allSteps)-1].Outcome == "error" {
		w("%s", he(t.OutcomeError))
	} else {
		w("%s", he(t.OutcomeUnknown))
	}
	w("</div>\n</header>\n")
}

// verdictCause renders the one finding that set the severity — its narrative
// text when available, or (clean journey) the all-clear line, or (redact) a
// bare code + step anchor. A driver that is itself low-confidence (every
// finding at the worst level was — 问题 2 D3) degrades the verdict to a
// "secondary signal only" line instead of asserting a confident cause.
func verdictCause(findings []Finding, driver FindingCode, driverLowConf bool, t i18n.StoryHTMLText, redact bool) string {
	if driver == "" {
		return "<span class=\"vclean\">" + he(t.VerdictClean) + "</span>"
	}
	if driverLowConf {
		return "<span class=\"vlow\">" + he(t.VerdictLowConfidence(string(driver))) + "</span>"
	}
	var f *Finding
	for i := range findings {
		if findings[i].Code == driver {
			f = &findings[i]
			break
		}
	}
	if redact || f == nil || f.Finding == "" {
		step := 0
		if f != nil {
			step = f.StepSeq
		}
		return he(t.VerdictRedacted(string(driver), step))
	}
	return he(f.Finding)
}

// htmlPointOfNoReturn renders the turning-point strip. With no located turn:
// a "stayed on the rails" note normally, but a "degraded gradually" note when
// the verdict is critical — a behavioral failure (loop / drift / oscillation)
// leaves no one decisive step, and reassurance would contradict the stamp.
func htmlPointOfNoReturn(w func(string, ...any), p *PointOfNoReturn, allSteps []*Step, sev string, t i18n.StoryHTMLText) {
	if p == nil {
		if sev == SeverityCritical {
			w("<div class=\"ponr ponr-diffuse\">%s</div>\n", he(t.DiffusePointOfNoReturn))
		} else {
			w("<div class=\"ponr ponr-none\">%s</div>\n", he(t.NoPointOfNoReturn))
		}
		return
	}
	ts := ""
	for _, s := range allSteps {
		if s.Seq == p.StepSeq && s.Manifest != nil {
			ts = s.Manifest.TS.In(fmtutil.DisplayZone).Format("15:04:05")
			break
		}
	}
	var detail string
	switch p.Kind {
	case PONRCompaction:
		// Both sizes only: the compaction-reconstruction layer sometimes
		// can't measure one side (0), and "79.8K → 0 tokens" reads as broken.
		before, after := "", ""
		if p.TokensBefore > 0 && p.TokensAfter > 0 {
			before, after = fmtutil.FmtTokens(p.TokensBefore), fmtutil.FmtTokens(p.TokensAfter)
		}
		detail = t.PONRCompaction(before, after, len(p.EntitiesDropped))
	case PONRRetry:
		detail = t.PONRRetry
	default:
		detail = t.PONRContract
	}
	w("<div class=\"ponr\">\n<div class=\"ponr-h\">%s</div>\n<div class=\"ponr-d\">%s</div>\n</div>\n",
		he(t.PointOfNoReturnHead(p.StepSeq, ts)), he(detail))
}

// fmtSpan renders a duration in human units — "50m 55s" / "4m 12s" / "45s"
// — for the shareable headlines (the crash report's damage line, the
// tale-of-the-tape's wall-time row). fmtutil.FmtSeconds is fixed at bare
// seconds, which the tight metrics grid wants but a headline doesn't.
func fmtSpan(d time.Duration) string {
	s := int(d.Seconds() + 0.5)
	if s < 60 {
		return strconv.Itoa(s) + "s"
	}
	m := s / 60
	s %= 60
	if m < 60 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	h := m / 60
	m %= 60
	return fmt.Sprintf("%dh %02dm", h, m)
}

// journeyTokenTotal is the run's total token traffic — every step's reported
// input (cached included: still tokens that moved) plus output, summed from
// the per-model usage table.
func journeyTokenTotal(m Metrics) int64 {
	var n int64
	for _, u := range m.ModelUsage {
		n += u.TokensIn + u.TokensOut
	}
	return n
}

// htmlDamageCost is the trailing " · ≈ $X" on the damage line — empty when
// no price book resolved (never "$0"), a "+" suffix when only some steps
// priced.
func htmlDamageCost(c CostFact, t i18n.StoryHTMLText) string {
	if !c.Resolved {
		return ""
	}
	return he(t.DamageCost(fmtMoney(c)))
}

// fmtMoney formats c's total with its currency — "$4.80" for USD/blank,
// "CNY 34.20" otherwise, a trailing "+" when the estimate is partial.
func fmtMoney(c CostFact) string {
	amt := strconv.FormatFloat(c.TotalAmount(), 'f', moneyDecimals(c.TotalAmount()), 64)
	s := amt
	if c.Currency == "" || c.Currency == "USD" {
		s = "$" + amt
	} else {
		s = c.Currency + " " + amt
	}
	if c.Partial() {
		s += "+"
	}
	return s
}

// moneyDecimals shows cents for small amounts, whole units past $100 where
// the cents are noise on a shareable card.
func moneyDecimals(v float64) int {
	if v >= 100 {
		return 0
	}
	return 2
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

// htmlTitleText renders j.Title for the <h1> — a single-line instruction
// preview, not a body block, so it escapes inline rather than wrapping in
// <pre>. Redact swaps it for a length placeholder like every other body.
func htmlTitleText(s string, redact bool, t i18n.StoryHTMLText) string {
	if strings.TrimSpace(s) == "" {
		return "<span class=\"empty\">" + he(t.Empty) + "</span>"
	}
	if redact {
		return "<span class=\"placeholder\">" + he(t.RedactedText(utf8.RuneCountInString(s))) + "</span>"
	}
	return he(s)
}
