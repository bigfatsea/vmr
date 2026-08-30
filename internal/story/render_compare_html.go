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
	"strconv"
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

	w("<div class=\"recbar\"><span class=\"rl\">%s</span></div>\n", che(t.RecorderBar))
	w("<header class=\"jhead\">\n")
	if redact {
		w("<div class=\"banner redact\">%s</div>\n", che(t.RedactedBanner))
	}
	chtmlTaleOfTheTape(w, cmp, t, redact)
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
		chtmlDivergence(w, cmp.Extras.Divergence, t, redact)
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
		// -redact: the sibling journey-<id>.md carries full, un-redacted
		// conversation bodies (0600, not for sharing) — same reason the
		// journey dashboard drops its details/*.md links in redact mode.
		// Keep the filename as a plain-text pointer, drop the anchor.
		if redact {
			w("<div class=\"absub\"><code>%s</code></div>\n", che(ref.ReportFile))
		} else {
			w("<div class=\"absub\"><a href=\"%s\">%s</a></div>\n", che(ref.ReportFile), che(ref.ReportFile))
		}
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

func chtmlDivergence(w func(string, ...any), d DivergencePoint, t i18n.CompareHTMLText, redact bool) {
	w("<div class=\"divergence\">\n")
	if !d.Found {
		w("<div class=\"dv-none\">%s</div>\n</div>\n", che(t.DivergenceNone))
		return
	}
	// The task title is user-authored instruction text (taskseg.TaskTitle) —
	// redact it like every other body, or -redact leaks it in the headline.
	task := oneLineTruncate(cbodyPlain(d.TaskTitle, t), 70)
	if redact && strings.TrimSpace(d.TaskTitle) != "" {
		task = t.Redacted(len([]rune(d.TaskTitle)))
	}
	w("<div class=\"dv-h\">%s</div>\n", che(t.DivergenceHeadline(d.AStepSeq, d.BStepSeq, task)))
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
	chtmlCostFact(w, ex.Cost, t)
	w("</div>\n")
}

func chtmlCostFact(w func(string, ...any), cp CostPair, t i18n.CompareHTMLText) {
	if !cp.A.Resolved && !cp.B.Resolved {
		chtmlFact(w, t.CostLabel, t.CostUnresolvedFact)
		return
	}
	chtmlFact(w, t.CostLabel, t.CostPairFact(totMoney(cp.A), totMoney(cp.B)))
	// Exactly one side priced: the other renders as "—", which reads as
	// "free" without this note (F-3).
	if cp.A.Resolved != cp.B.Resolved {
		side := "A"
		if cp.A.Resolved {
			side = "B"
		}
		w("<div class=\"fact sub\">%s</div>\n", che(t.CostOneSideNote(side)))
	}
}

// --- tale of the tape --------------------------------------------------------

// chtmlTaleOfTheTape renders the versus scorecard: the metric name centered,
// each side's value flanking it — the iconic pre-fight format. No winner is
// declared (see i18n.CompareHTMLText.TotNoWinner). Redact-safe: every value
// here is a count, a duration, a model name or a $ figure, never body text.
func chtmlTaleOfTheTape(w func(string, ...any), cmp Comparison, t i18n.CompareHTMLText, redact bool) {
	w("<div class=\"tot\">\n<div class=\"tot-h\">%s</div>\n<table class=\"tot-tbl\"><tbody>\n", che(t.TaleOfTheTape))

	aModel, bModel := "—", "—"
	var dur DurationFact
	var cost CostPair
	div := t.TotAligned
	if cmp.Extras != nil {
		aModel = totModelLabel(cmp.Extras.Endpoints.A)
		bModel = totModelLabel(cmp.Extras.Endpoints.B)
		dur, cost = cmp.Extras.Duration, cmp.Extras.Cost
		if cmp.Extras.Divergence.Found {
			div = t.TotDivergeAt(cmp.Extras.Divergence.Index + 1)
		}
	}
	totRow(w, aModel, t.TotModel, bModel)
	totRow(w, strconv.Itoa(cmp.A.Steps), t.TotSteps, strconv.Itoa(cmp.B.Steps))
	totRow(w, strconv.Itoa(cmp.A.ToolCalls), t.TotToolCalls, strconv.Itoa(cmp.B.ToolCalls))
	if cmp.Extras != nil {
		totRow(w, fmtSpan(dur.AWall), t.TotWallTime, fmtSpan(dur.BWall))
		if cost.A.Resolved || cost.B.Resolved {
			totRow(w, totMoney(cost.A), t.TotCost, totMoney(cost.B))
		}
	}
	w("</tbody></table>\n")
	w("<div class=\"tot-split\">%s</div>\n", che(div))
	w("<div class=\"tot-note\">%s</div>\n</div>\n", che(t.TotNoWinner))
}

func totRow(w func(string, ...any), a, label, b string) {
	w("<tr><td class=\"ta\">%s</td><td class=\"tm\">%s</td><td class=\"tb\">%s</td></tr>\n",
		che(a), che(label), che(b))
}

// totModelLabel is the short "provider:model" for the scorecard — the
// audit label's protocol prefix dropped, and a second distinct endpoint
// collapsed to "(+N)" so a mid-run failover doesn't blow out the cell.
func totModelLabel(endpoints []string) string {
	if len(endpoints) == 0 {
		return "—"
	}
	short := endpoints[0]
	if parts := strings.SplitN(short, ":", 3); len(parts) == 3 {
		short = parts[1] + ":" + parts[2]
	}
	if n := len(endpoints) - 1; n > 0 {
		short += fmt.Sprintf(" (+%d)", n)
	}
	return short
}

// totMoney is fmtMoney (render_html.go) with an unresolved side rendered as
// a dash rather than "$0".
func totMoney(c CostFact) string {
	if !c.Resolved {
		return "—"
	}
	return fmtMoney(c)
}

func chtmlFact(w func(string, ...any), label, val string) {
	w("<div class=\"fact\"><span class=\"fk\">%s</span> %s</div>\n", che(label), che(val))
}

func chtmlDeliverable(w func(string, ...any), d DeliverableFact, t i18n.CompareHTMLText, redact bool) {
	// Neither side wrote a single-file deliverable: skip the row rather than
	// print "A none / B none" (F-6).
	if !d.A.Found && !d.B.Found {
		return
	}
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

// compareExtraStyle is appended after htmlStyle, so it inherits the same
// "VMR Forensics" tokens (--ink / --panel / --rule / --amber / --trace ...);
// it only adds the tale-of-the-tape scorecard and the A/B tables.
const compareExtraStyle = `
/* Tale of the tape */
.tot { border: 1px solid var(--rule); border-top: 4px solid var(--amber); border-radius: 4px;
  background: var(--panel); padding: 14px 16px 12px; margin-bottom: 22px; }
.tot-h { font-size: 11px; letter-spacing: .2em; text-transform: uppercase; color: var(--ink-dim);
  text-align: center; margin-bottom: 8px; }
table.tot-tbl { border-collapse: collapse; width: 100%; }
table.tot-tbl td { padding: 6px 10px; font-size: 14px; border-bottom: 1px solid var(--rule); }
table.tot-tbl tr:last-child td { border-bottom: none; }
.tot-tbl .ta { text-align: right; width: 40%; color: var(--trace); font-weight: 700; }
.tot-tbl .tb { text-align: left; width: 40%; color: var(--amber); font-weight: 700; }
.tot-tbl .tm { text-align: center; width: 20%; font-size: 10px; letter-spacing: .1em;
  text-transform: uppercase; color: var(--ink-dim); font-weight: 400; }
.tot-split { text-align: center; margin-top: 10px; font-size: 12px; letter-spacing: .04em;
  color: var(--ink); font-family: var(--sans); }
.tot-note { text-align: center; margin-top: 6px; font-size: 11px; color: var(--ink-dim); font-family: var(--sans); }

/* A / B side cards + divergence + tables */
.abside { border: 1px solid var(--rule); border-radius: 5px; padding: 12px 14px; margin: 8px 0; background: var(--panel); }
.abtag { font-weight: 700; font-size: 12px; color: var(--trace); letter-spacing: .1em; }
.abid { font-family: var(--mono); font-size: 11px; color: var(--ink-dim); word-break: break-all; }
.abtitle { margin: 4px 0; font-family: var(--sans); }
.absub { font-size: 11px; color: var(--ink-dim); }
.abinstr { margin: 10px 0; }
.abinstr-side { margin: 6px 0; font-family: var(--sans); font-size: 13px; }
.divergence { border: 1px solid var(--rule); border-left: 3px solid var(--amber); border-radius: 5px;
  padding: 12px 14px; margin: 10px 0; background: var(--panel); }
.dv-h { font-size: 14px; font-weight: 700; font-family: var(--sans); }
.dv-row { margin-top: 4px; font-size: 12.5px; }
.dv-l { display: inline-block; width: 18px; font-weight: 700; color: var(--ink-dim); }
.dv-none { color: var(--ink-dim); }
.dv-note { margin-top: 6px; font-size: 11px; color: var(--ink-dim); font-family: var(--sans); }
table.abtbl { border-collapse: collapse; width: 100%; margin: 12px 0; font-size: 12.5px; }
table.abtbl th, table.abtbl td { border: 1px solid var(--rule); padding: 5px 9px; text-align: left; }
table.abtbl th { background: var(--code); font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-dim); }
table.abtbl tr.notable td { color: var(--amber); }
table.abtbl tr.notable td:first-child { border-left: 2px solid var(--amber); }
.facts { margin-top: 14px; }
.fact { font-size: 12.5px; margin: 5px 0; font-family: var(--sans); }
.fact.sub { margin-left: 16px; color: var(--ink-dim); }
.fact .fk { font-size: 10px; text-transform: uppercase; letter-spacing: .08em; color: var(--ink-dim); margin-right: 6px; font-family: var(--mono); }
.llm-disclaimer { font-size: 11px; color: var(--ink-dim); border: 1px dashed var(--rule); border-radius: 4px; padding: 8px 10px; margin-bottom: 10px; font-family: var(--sans); }
.llm-part { margin: 10px 0; font-family: var(--sans); }
.llm-part h3 { font-size: 13px; margin: 0 0 4px; }
table.mdlite { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 12px; display: block; overflow-x: auto; }
table.mdlite th, table.mdlite td { border: 1px solid var(--rule); padding: 4px 7px; text-align: left; vertical-align: top; }
table.mdlite th { background: var(--code); }
@media (max-width: 820px) {
  .tot-tbl .ta, .tot-tbl .tb { width: 38%; }
  .tot-tbl td { font-size: 13px; }
}
`
