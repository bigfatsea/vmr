// Ver 2026-08-01, by Sonnet 5

// The report document skeleton: the section running order, the summary and
// highlights that open it, the closing pointers, and the one table
// primitive every section shares. Each numbered section's own body lives in
// its own section_*.go file — adding a section means adding a file and one
// line to Markdown below, not editing a renderer that keeps growing.
package report

import (
	"fmt"
	"strconv"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// mdTable collapses the "write a header row + separator row, then one row
// per data item" pattern repeated ~20 times across the section files into one
// declaration + one row() call per item, instead of each call site hand-
// writing its own "| h1 | h2 |...|\n|---|---|...|\n" header and %s-joined
// row format string. Column *formatting* stays the caller's job: the tables
// differ too much in per-cell logic (conditional flags, composite
// "a / b / c" cells, dynamic header text, low-n markers) for a template
// layer to express without becoming Go code again — untyped, and failing at
// run time instead of compile time.
type mdTable struct {
	w    func(string, ...any)
	cols int
}

// newTable writes the header and separator rows immediately (every call
// site already writes the header right before its data rows) and returns a
// handle for the data rows.
func newTable(w func(string, ...any), headers ...string) *mdTable {
	w("%s", "| "+strings.Join(headers, " | ")+" |\n")
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	w("%s", "|"+strings.Join(seps, "|")+"|\n")
	return &mdTable{w: w, cols: len(headers)}
}

// row writes one data row. len(cells) must equal the header's column count
// — a mismatch is a programmer error in a section renderer, not malformed input, so
// it panics immediately rather than silently emitting a ragged table.
//
// Cells are passed to w via a literal "%s" verb, never interpolated
// straight into the format string: many cells legitimately contain a raw
// "%" (percentages, cache-efficiency figures), which fmt.Fprintf would
// otherwise try to parse as a verb and corrupt into "%!s(MISSING)" — caught
// by diffing real report output against the pre-2.6 renderer, not by the
// unit tests (whose fixtures happened not to exercise this).
func (t *mdTable) row(cells ...string) {
	if len(cells) != t.cols {
		panic(fmt.Sprintf("mdTable: row has %d cells, header has %d", len(cells), t.cols))
	}
	// Every cell is escaped for table-cell safety (a raw "|" splits the row
	// into extra columns, a literal newline breaks the one-row-per-line
	// structure) — user-derived cells like session/task titles reach here
	// verbatim (B4, the same bug class story fixed in P12.2/P12.3). No
	// current caller passes intentional table markup in a cell. Cells that
	// also carry free-form user/model text HTML-escape at the call site
	// (see section_sessions.go) so an unclosed "<!--" can't swallow the
	// rest of the document.
	esc := make([]string, len(cells))
	for i, c := range cells {
		esc[i] = reqdetail.EscapeCell(c)
	}
	t.w("%s", "| "+strings.Join(esc, " | ")+" |\n")
}

// StoriesLinkInfo carries the "vmr-report.md → stories/vmr-stories.md"
// navigation edge (P6.2a, architecture doc §7.5) — this points at ANOTHER
// COMMAND's aggregate product (vmr story's index), not at anything report
// itself can compute, so the two-classes-of-edges rule (§7.5) applies:
// the caller stats the target and passes nil when absent, rather than
// report reaching into stories/ itself (report never generates story's
// index — see the architecture doc's explicit "report doesn't generate
// vmr-stories.*" ruling, kept to avoid widening report's scope into
// story's candidate computation).
type StoriesLinkInfo struct {
	// Path is relative to vmr-report.md itself, e.g. "stories/vmr-stories.md".
	Path                   string
	JourneyCount           int
	FromDisplay, ToDisplay string // pre-formatted, same convention as MetaLine's from/to
}

// Markdown renders the full vmr-report.md document in lang — see
// docs/VirtualModelRouter_Design_v4_Analytics.md's output-language section:
// every narrative string comes from internal/i18n, looked up once per
// section via that section's own Xxx(lang) bundle; nothing here or in any
// section_*.go file hardcodes a language. stories is nil when this run's
// output root has no vmr-stories.md to link to (see StoriesLinkInfo).
func Markdown(rep *Report2, lang i18n.Lang, stories *StoriesLinkInfo) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	o := rep.Overall
	t := i18n.Doc(lang)

	// ---- header ----
	w("# %s\n\n", t.Title)
	w("%s\n", t.MetaLine(t.MetaInputSummary(len(rep.Meta.Inputs)), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors,
		fmtDisplayFull(rep.Meta.From), fmtDisplayFull(rep.Meta.To)))
	w("\n<details><summary>%s</summary>\n\n%s\n</details>\n\n", t.MetaInputListLabel, strings.Join(rep.Meta.Inputs, ", "))
	w("%s", t.DetailLinkLine)
	withSibling := clientsWithSiblingFile(rep)
	for _, c := range rep.ByClient {
		if withSibling[c.ClientKey] {
			w(" · [-%s](./vmr-requests-%s.md)", c.ClientKey, sanitize(c.ClientKey))
		}
	}
	w("\n\n")
	if stories != nil {
		w("%s", t.StoriesLinkLine(stories.Path, stories.JourneyCount, stories.FromDisplay, stories.ToDisplay))
	}

	renderSummary(w, rep, o, lang)
	renderCostTokens(w, rep, o, lang)
	renderCostEstimate(w, rep, lang)
	renderProviders(w, rep, lang)
	renderReliability(w, rep, o, lang)
	renderLatency(w, rep, o, lang)
	renderWorkload(w, rep, o, lang)
	renderClientEndpoint(w, rep, lang)
	renderSessions(w, rep, lang)
	renderStickyEffect(w, rep, lang)
	renderEndpointValue(w, rep, lang)
	renderCompactions(w, rep, lang)
	renderEfficiency(w, rep, o, lang)
	renderRequestIndexLink(w, rep, lang)
	renderAppendix(w, rep, lang)
	return b.String()
}

// ---- §0 摘要 ----
func renderSummary(w func(string, ...any), rep *Report2, o Row, lang i18n.Lang) {
	t := i18n.Doc(lang)
	w("## %s\n\n", t.SummaryTitle)
	h := t.SummaryHeaders
	tbl := newTable(w, h[0], h[1], h[2], h[3], h[4])
	p95n := o.RequestsWithDur
	tbl.row(t.SummaryRequests(o.Requests, o.Fallbacks, o.Truncated),
		pctStr2(o.OK, o.Requests),
		fmtutil.FmtTokens(o.TokensInFresh),
		cacheEffCell(o.CacheEfficiency, o.TokensKnown, o.Requests),
		durCell(o.DurMSP95, p95n))
	w("\n")
	w("%s", t.SummaryStarNote)
	w("%s\n", t.HighlightsAuto)
	for _, h := range highlights(rep, lang) {
		w("- %s\n", h)
	}
	w("\n")
}

// highlights generates ≤3 auto highlights from the finished buckets.
func highlights(rep *Report2, lang i18n.Lang) []string {
	t := i18n.Doc(lang)
	var out []string
	// 1. workload with low cache-eff
	for _, wl := range rep.Workloads {
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			out = append(out, t.CacheWarn(wl.Class, pctStr(wl.CacheEfficiency), fmtutil.FmtTokens(wl.TokensInFresh)))
			break
		}
	}
	// 2. tool shape with low utilization
	for _, tl := range rep.Tools {
		if tl.Requests > 0 && tl.DeclareUtilization < 0.20 && tl.SchemaBytesShipped > 0 {
			out = append(out, t.ToolWarn(tl.Shape, tl.Requests, fmtBytesGB(tl.SchemaBytesShipped),
				pctStr(tl.DeclareUtilization), len(tl.NeverCalled)))
			break
		}
	}
	// 3. worst endpoint error rate
	var worst *EndpointRow
	for i := range rep.EndpointsAll {
		e := &rep.EndpointsAll[i]
		if worst == nil || e.ErrorRate > worst.ErrorRate {
			worst = e
		}
	}
	if worst != nil && worst.Attempts >= 4 && worst.ErrorRate > 5 {
		top := topErrorClass(worst, lang)
		out = append(out, t.EndpointWarn(worst.Endpoint, strconv.FormatFloat(float64(worst.ErrorRate), 'f', 1, 64), top))
	}
	if len(out) == 0 {
		out = append(out, t.NoAnomalies)
	}
	return out
}

// topErrorClassCount finds the error class with the highest count,
// iterating sortedKeysInt(classes) rather than ranging the map directly —
// found while verifying 2.6's table refactor against real report output: a
// tie (two classes with the same count) resolved to whichever class Go's
// randomized map order happened to visit last, so the same input could
// report a different "主因" class between two runs of the same binary.
// Same bug class, same fix, as the sort.Slice tie-break fix earlier applied
// to aggregate.go's Build — ties now always resolve to the
// alphabetically-first class name.
func topErrorClassCount(classes map[string]int) (cls string, n int) {
	for _, c := range sortedKeysInt(classes) {
		if m := classes[c]; m > n {
			cls, n = c, m
		}
	}
	return cls, n
}

func topErrorClass(e *EndpointRow, lang i18n.Lang) string {
	if len(e.ErrorClasses) == 0 {
		return ""
	}
	cls, n := topErrorClassCount(e.ErrorClasses)
	return i18n.Doc(lang).TopErrorSuffix(cls, n)
}

// ---- §8 请求详单 ----
func renderRequestIndexLink(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	t := i18n.Doc(lang)
	w("## %s\n\n", t.RequestIndexTitle)
	w("%s\n", t.RequestIndexBody)
	withSibling := clientsWithSiblingFile(rep)
	first := true
	for _, c := range rep.ByClient {
		if !withSibling[c.ClientKey] {
			continue
		}
		if first {
			w("%s", t.PerClientLabel)
			first = false
		} else {
			w(" · ")
		}
		w("[-%s](./vmr-requests-%s.md)", c.ClientKey, sanitize(c.ClientKey))
	}
	if !first {
		w("\n")
	}
	if rep.Meta.DetailsEnabled {
		w("%s", t.DetailsCaptureBody)
	} else {
		// Default run (-details=false, P3.3): details/*.md was never
		// materialized, so this section can't link into it — point at
		// the on-demand read primitive instead (P6.2b), with a real
		// coordinate from this run's own data so the example is
		// copy-pasteable, not a placeholder.
		example := ""
		if rows := rep.RequestRows(); len(rows) > 0 {
			example = rows[0].Req
		}
		w("%s", t.DetailsOnDemandBody(example))
	}
}

// ---- 附录 ----
func renderAppendix(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	t := i18n.Doc(lang)
	w("## %s\n\n", t.AppendixTitle)
	w("%s", t.AppendixInputLine(strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors))
	w("%s", t.AppendixPeriodLine(fmtDisplayFull(rep.Meta.From), fmtDisplayFull(rep.Meta.To)))
	w("%s", t.AppendixPercentile(rep.Meta.PercentileMethod))
	w("%s", t.AppendixNBase)
	w("%s", t.AppendixLowConf)
	w("%s", t.AppendixStarMark)
	w("%s", t.AppendixBillingLine(orDash2(rep.Pricing == nil, t.AppendixNoPricing, "")))
	w("%s", t.AppendixSlowThresh(rep.Meta.SlowThreshold/1000))
	if rep.Meta.SelfTrafficExcluded > 0 {
		w("%s", t.AppendixSelfTrafficExcluded(rep.Meta.SelfTrafficExcluded))
	} else {
		w("%s", t.AppendixSelfTrafficNotExcluded)
	}
}

// ---- cell/format helpers ----
