// Ver 2026-08-28, by Sonnet 5

// RenderComparisonHTML: the single-page HTML comparison dashboard (`vmr
// analyze -compare a,b -html`, optionally `-redact`). Same self-contained,
// zero-external-request shape as the journey dashboard (shares htmlStyle /
// htmlScript) — three sections: the two sides, the divergence point plus
// the metric-by-metric diff and the endpoint/cache/sysprompt facts, and
// (when -llm-addr was given) the LLM interpretation prose. Pure view over
// the already-computed Comparison + the two InterpretResults — no I/O, no
// judgment. Redact mode replaces excerpt bodies with length placeholders
// and drops the LLM section entirely (it paraphrases evidence verbatim).
package story

import (
	"fmt"
	"html"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// CompareLLMResult carries the structured LLM interpretation for the HTML
// dashboard — the .md path keeps using RenderLLMSection's string output, so
// this exists only so the renderer doesn't have to re-parse that Markdown.
type CompareLLMResult struct {
	Model          string
	Overall        InterpretResult
	Divergence     InterpretResult
	DivergenceUsed bool
}

func (r CompareLLMResult) empty() bool {
	return r.Overall.Text == "" && r.Divergence.Text == ""
}

func RenderComparisonHTML(cmp Comparison, llm CompareLLMResult, lang i18n.Lang, redact bool) string {
	t := i18n.CompareHTML(lang)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("<!doctype html>\n<html lang=%q>\n<head>\n<meta charset=\"utf-8\">\n", chtmlLangAttr(lang))
	w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	w("<title>%s</title>\n<style>\n%s%s</style>\n</head>\n<body>\n<div class=\"wrap\">\n",
		che(t.DocTitle), htmlStyle, compareExtraStyle)

	w("<header class=\"jhead\">\n<h1>%s</h1>\n", che(t.DocTitle))
	if redact {
		w("<div class=\"banner redact\">%s</div>\n", che(t.RedactedBanner))
	}
	w("</header>\n<div class=\"layout\">\n<div class=\"railwrap\">\n<nav class=\"rail\">\n<ol>\n")
	w("<li><a href=\"#sides\">%s</a></li>\n", che(t.SectionSides))
	w("<li><a href=\"#diff\">%s</a></li>\n", che(t.SectionDiff))
	if !redact && !llm.empty() {
		w("<li><a href=\"#llm\">%s</a></li>\n", che(t.SectionLLM))
	}
	w("</ol>\n</nav>\n</div>\n<main class=\"body\">\n")

	w("<section class=\"block\" id=\"sides\">\n<h2>%s</h2>\n", che(t.SectionSides))
	chtmlSide(w, "A", cmp.A, t, redact)
	chtmlSide(w, "B", cmp.B, t, redact)
	if cmp.Extras != nil {
		chtmlInitialInstruction(w, cmp.Extras.InitialInstruction, t, redact)
	}
	w("</section>\n")

	w("<section class=\"block\" id=\"diff\">\n<h2>%s</h2>\n", che(t.SectionDiff))
	if cmp.Extras != nil {
		chtmlDivergence(w, cmp.Extras.Divergence, t)
	}
	chtmlMetricTable(w, cmp.Rows, t)
	if len(cmp.Tools) > 0 {
		chtmlToolTable(w, cmp.Tools, t)
	}
	if cmp.Extras != nil {
		chtmlFacts(w, cmp.Extras, t, redact)
	}
	w("</section>\n")

	if !redact && !llm.empty() {
		chtmlLLM(w, llm, t)
	}

	w("</main>\n</div>\n<footer class=\"gennote\">%s</footer>\n</div>\n", che(t.GeneratedNote))
	w("<script>\n%s</script>\n</body>\n</html>\n", htmlScript)
	return b.String()
}

func chtmlLangAttr(lang i18n.Lang) string {
	if lang == i18n.ZH {
		return "zh"
	}
	return "en"
}

func che(s string) string { return html.EscapeString(s) }

func chtmlSide(w func(string, ...any), label string, ref JourneyRef, t i18n.CompareHTMLText, redact bool) {
	w("<div class=\"abside\">\n<div class=\"abtag\">%s</div>\n", che(label))
	w("<div class=\"abid\">%s</div>\n", che(ref.ID))
	w("<div class=\"abtitle\">%s</div>\n", cbodyText(ref.Title, redact, t))
	w("<div class=\"absub\">%s → %s</div>\n",
		che(ref.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")),
		che(ref.To.In(fmtutil.DisplayZone).Format("15:04:05")))
	if ref.ReportFile != "" {
		w("<div class=\"absub\"><a href=\"%s\">%s</a></div>\n", che(ref.ReportFile), che(ref.ReportFile))
	}
	w("</div>\n")
}

func chtmlInitialInstruction(w func(string, ...any), f InitialInstructionFact, t i18n.CompareHTMLText, redact bool) {
	if !f.A.Found && !f.B.Found {
		return
	}
	w("<details class=\"abinstr\">\n<summary>%s</summary>\n", che(t.InitialInstruction))
	w("<div class=\"abinstr-side\"><b>A</b> %s</div>\n", cexcerpt(f.A.Text, f.A.Truncated, redact, t))
	w("<div class=\"abinstr-side\"><b>B</b> %s</div>\n", cexcerpt(f.B.Text, f.B.Truncated, redact, t))
	w("</details>\n")
}

func chtmlDivergence(w func(string, ...any), d DivergencePoint, t i18n.CompareHTMLText) {
	w("<div class=\"divergence\">\n")
	if !d.Found {
		w("<div class=\"dv-none\">%s</div>\n</div>\n", che(t.DivergenceNone))
		return
	}
	w("<div class=\"dv-h\">%s</div>\n", che(t.DivergenceHeadline(d.AStepSeq, d.BStepSeq, oneLineTruncate(cbodyPlain(d.TaskTitle, t), 70))))
	w("<div class=\"dv-row\"><span class=\"dv-l\">A</span> %s</div>\n", che(strings.Join(nonEmpty(d.ATools, t.NoTools), ", ")))
	w("<div class=\"dv-row\"><span class=\"dv-l\">B</span> %s</div>\n", che(strings.Join(nonEmpty(d.BTools, t.NoTools), ", ")))
	w("<div class=\"dv-note\">%s</div>\n</div>\n", che(t.DivergenceNote))
}

func nonEmpty(xs []string, fallback string) []string {
	if len(xs) == 0 {
		return []string{fallback}
	}
	return xs
}

func chtmlMetricTable(w func(string, ...any), rows []MetricDiff, t i18n.CompareHTMLText) {
	w("<table class=\"abtbl\">\n<thead><tr><th>%s</th><th>A</th><th>B</th><th>Δ</th></tr></thead>\n<tbody>\n", che(t.MetricCol))
	for _, r := range rows {
		cls := ""
		mark := ""
		if r.Notable {
			cls = " class=\"notable\""
			mark = " ⚠️"
		}
		w("<tr%s><td>%s%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			cls, che(r.Label), mark,
			che(formatMetric(r.Kind, r.A)), che(formatMetric(r.Kind, r.B)), che(formatDeltaRel(r.DeltaRel)))
	}
	w("</tbody>\n</table>\n")
}

func chtmlToolTable(w func(string, ...any), tools []ToolShareDiff, t i18n.CompareHTMLText) {
	w("<table class=\"abtbl\">\n<thead><tr><th>%s</th><th>A</th><th>B</th></tr></thead>\n<tbody>\n", che(t.ToolCol))
	for _, tl := range tools {
		w("<tr><td><code>%s</code></td><td>%d</td><td>%d</td></tr>\n", che(tl.Name), tl.ACalls, tl.BCalls)
	}
	w("</tbody>\n</table>\n")
}

func chtmlFacts(w func(string, ...any), ex *ComparisonExtras, t i18n.CompareHTMLText, redact bool) {
	w("<div class=\"facts\">\n")
	sameTxt := t.EndpointsDiffer
	if ex.Endpoints.Same {
		sameTxt = t.EndpointsSame
	}
	chtmlFact(w, t.EndpointsLabel, sameTxt+" · A: "+strings.Join(nonEmpty(ex.Endpoints.A, "—"), ", ")+" · B: "+strings.Join(nonEmpty(ex.Endpoints.B, "—"), ", "))
	chtmlFact(w, t.CacheLabel, t.CachePair(pctStr(ex.Cache.A.FirstRatio), pctStr(ex.Cache.A.SteadyMean), pctStr(ex.Cache.B.FirstRatio), pctStr(ex.Cache.B.SteadyMean)))
	chtmlFact(w, t.SysPromptLabel, t.SysPromptPair(fmtutil.FmtTokens(ex.SysPrompt.A.Tokens), ex.SysPrompt.A.Changes, fmtutil.FmtTokens(ex.SysPrompt.B.Tokens), ex.SysPrompt.B.Changes))
	chtmlFact(w, t.DurationLabel, t.DurationPair(fmtutil.FmtSeconds(ex.Duration.AWall, 1), emptyOr(ex.Duration.ATermination, t.NoTermination), fmtutil.FmtSeconds(ex.Duration.BWall, 1), emptyOr(ex.Duration.BTermination, t.NoTermination)))
	chtmlDeliverable(w, ex.Deliverable, t, redact)
	w("</div>\n")
}

func chtmlFact(w func(string, ...any), label, val string) {
	w("<div class=\"fact\"><span class=\"fk\">%s</span> %s</div>\n", che(label), che(val))
}

func chtmlDeliverable(w func(string, ...any), d DeliverableFact, t i18n.CompareHTMLText, redact bool) {
	w("<div class=\"fact\"><span class=\"fk\">%s</span></div>\n", che(t.DeliverableLabel))
	for _, s := range []struct {
		label string
		st    DeliverableStats
	}{{"A", d.A}, {"B", d.B}} {
		if !s.st.Found {
			w("<div class=\"fact sub\"><b>%s</b> %s</div>\n", che(s.label), che(t.DeliverableNone))
			continue
		}
		w("<div class=\"fact sub\"><b>%s</b> %s <code>%s</code> — %s</div>\n",
			che(s.label), che(t.DeliverableAt(s.st.StepSeq)), che(s.st.ToolName),
			cexcerpt(s.st.Excerpt, s.st.Truncated, redact, t))
	}
}

func chtmlLLM(w func(string, ...any), llm CompareLLMResult, t i18n.CompareHTMLText) {
	w("<section class=\"block\" id=\"llm\">\n<h2>%s</h2>\n", che(t.SectionLLM))
	w("<div class=\"llm-disclaimer\">%s</div>\n", che(t.LLMDisclaimer(llm.Model)))
	chtmlLLMPart(w, t.LLMScopeOverall, llm.Overall, t)
	if llm.DivergenceUsed {
		chtmlLLMPart(w, t.LLMScopeDivergence, llm.Divergence, t)
	}
	w("</section>\n")
}

func chtmlLLMPart(w func(string, ...any), scope string, res InterpretResult, t i18n.CompareHTMLText) {
	if strings.TrimSpace(res.Text) == "" {
		return
	}
	w("<div class=\"llm-part\">\n<h3>%s%s</h3>\n", che(scope), cachedSuffix(res.Cached, t))
	w("%s\n</div>\n", mdToHTML(res.Text))
}

func cachedSuffix(cached bool, t i18n.CompareHTMLText) string {
	if cached {
		return " " + che(t.LLMCached)
	}
	return ""
}

// --- body / redaction helpers (compare-local, mirrors render_html.go's) ---

func cbodyText(s string, redact bool, t i18n.CompareHTMLText) string {
	if strings.TrimSpace(s) == "" {
		return "<span class=\"empty\">" + che(t.Empty) + "</span>"
	}
	if redact {
		return "<span class=\"placeholder\">" + che(t.Redacted(len([]rune(s)))) + "</span>"
	}
	return che(s)
}

func cbodyPlain(s string, t i18n.CompareHTMLText) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func cexcerpt(text string, truncated, redact bool, t i18n.CompareHTMLText) string {
	if strings.TrimSpace(text) == "" {
		return "<span class=\"empty\">" + che(t.Empty) + "</span>"
	}
	if redact {
		return "<span class=\"placeholder\">" + che(t.Redacted(len([]rune(text)))) + "</span>"
	}
	suffix := ""
	if truncated {
		suffix = " …"
	}
	return "<pre>" + che(text) + che(suffix) + "</pre>"
}

func emptyOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

const compareExtraStyle = `
.abside { border: 1px solid var(--line); border-radius: 8px; padding: 12px 14px; margin: 8px 0; background: var(--card); }
.abtag { font-weight: 700; font-size: 12px; color: var(--accent); }
.abid { font-family: ui-monospace, monospace; font-size: 12px; color: var(--muted); word-break: break-all; }
.abtitle { margin: 4px 0; }
.absub { font-size: 12px; color: var(--muted); }
.abinstr { margin: 10px 0; }
.abinstr-side { margin: 6px 0; }
.divergence { border: 1px solid var(--flag); border-radius: 8px; padding: 12px 14px; margin: 10px 0; }
.dv-h { font-size: 15px; font-weight: 600; }
.dv-row { margin-top: 4px; font-size: 13px; }
.dv-l { display: inline-block; width: 18px; font-weight: 700; color: var(--muted); }
.dv-none { color: var(--muted); }
.dv-note { margin-top: 6px; font-size: 12px; color: var(--muted); }
table.abtbl { border-collapse: collapse; width: 100%; margin: 12px 0; font-size: 13px; }
table.abtbl th, table.abtbl td { border: 1px solid var(--line); padding: 5px 9px; text-align: left; }
table.abtbl th { background: var(--card); font-size: 12px; }
table.abtbl tr.notable td { background: var(--warn-bg); color: var(--warn-fg); }
.facts { margin-top: 14px; }
.fact { font-size: 13px; margin: 5px 0; }
.fact.sub { margin-left: 16px; color: var(--muted); }
.fact .fk { font-size: 11px; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); margin-right: 6px; }
.llm-disclaimer { font-size: 12px; color: var(--muted); background: var(--card); border-radius: 6px; padding: 8px 10px; margin-bottom: 10px; }
.llm-part { margin: 10px 0; }
.llm-part h3 { font-size: 14px; margin: 0 0 4px; }
table.mdlite { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 12px; display: block; overflow-x: auto; }
table.mdlite th, table.mdlite td { border: 1px solid var(--line); padding: 4px 7px; text-align: left; vertical-align: top; }
table.mdlite th { background: var(--card); }
`
