// Ver 2026-09-15, by Opus 5

// ViewModel builder for the document-level pieces the section files don't
// own: the H1 + meta header, §0's summary table and auto highlights, §8's
// link section, the appendix (as the report-level Disclaimers/Footnotes),
// and the assembly of the full MacroReportVM. Pairs with
// internal/i18n/report_doc.go. The legacy counterparts live in
// render_doc.go until the byte-equivalence transition window closes.
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// StoriesLinkInfo carries the "vmr-report.md → journeys/index.md"
// navigation edge (P6.2a, architecture doc §7.5).
type StoriesLinkInfo struct {
	// Path is relative to vmr-report.md itself, e.g. "journeys/index.md".
	Path                   string
	JourneyCount           int
	FromDisplay, ToDisplay string
}

// BuildMacroReportVM builds the whole vmr-report.md view model in lang.
// stories is nil when this run's output root has no journeys index to link
// to; journeyLink is the lineage-id → rendered-journey-filename map the §6
// session table links against (nil/empty when none).
func BuildMacroReportVM(rep *Report2, lang i18n.Lang, stories *StoriesLinkInfo, journeyLink map[string]string) *MacroReportVM {
	t := i18n.Doc(lang)
	vm := &MacroReportVM{Title: t.Title}
	vm.Meta = vmMetaHeader(rep, lang, stories)
	all := []SectionVM{
		vmSummarySection(rep, lang),
		vmTokensSection(rep, rep.Overall, lang),
		vmCostSection(rep, lang),
		vmProvidersSection(rep, lang),
		vmReliabilitySection(rep, rep.Overall, lang),
		vmLatencySection(rep, rep.Overall, lang),
		vmWorkloadSection(rep, rep.Overall, lang),
		vmClientEndpointSection(rep, lang),
		vmSessionsSection(rep, journeyLink, lang),
		vmStickySection(rep, lang),
		vmEndpointValueSection(rep, lang),
		vmCompactionsSection(rep, lang),
		vmEfficiencySection(rep, rep.Overall, lang),
		vmRequestIndexSection(rep, lang),
		vmAppendixSection(rep, lang),
	}
	// Sections whose data is absent return the zero SectionVM — the legacy
	// path skips them entirely, so no empty "## " heading may be emitted.
	for _, s := range all {
		if s.Title == "" {
			continue
		}
		vm.Sections = append(vm.Sections, s)
	}
	vm.Disclaimers, vm.Footnotes = vmAppendixClosing(rep, lang)
	return vm
}

// MacroMarkdown is the VM path's one-call entry: build the view model and
// serialize it.
func MacroMarkdown(rep *Report2, lang i18n.Lang, stories *StoriesLinkInfo, journeyLink map[string]string) string {
	return RenderMarkdown(BuildMacroReportVM(rep, lang, stories, journeyLink))
}

// Markdown renders rep via the single ViewModel path.
func Markdown(rep *Report2, lang i18n.Lang, stories *StoriesLinkInfo, journeyLink map[string]string) string {
	return MacroMarkdown(rep, lang, stories, journeyLink)
}

// LoadReport reads vmr-report.json from dir, and if requests/index.json is
// present, restores rep.requests from RequestsIndex.
func LoadReport(dir string) (*Report2, error) {
	data, err := os.ReadFile(filepath.Join(dir, "vmr-report.json"))
	if err != nil {
		return nil, err
	}
	var rep Report2
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, err
	}
	reqPath := filepath.Join(dir, "requests", "index.json")
	if reqData, err := os.ReadFile(reqPath); err == nil {
		var idx RequestsIndex
		if err := json.Unmarshal(reqData, &idx); err == nil {
			rep.requests = idx.Requests
		}
	}
	return &rep, nil
}

// vmMetaHeader builds the blocks between the H1 and §0: the data-source
// line (with the report window), the report-config disclosure, the
// collapsible input list, and the details/journeys link lines.
func vmMetaHeader(rep *Report2, lang i18n.Lang, stories *StoriesLinkInfo) []BlockVM {
	t := i18n.Doc(lang)
	var blocks []BlockVM
	blocks = append(blocks, ParaVM{Text: t.MetaLine(t.MetaInputSummary(len(rep.Meta.Inputs)), rep.Meta.Format,
		rep.Meta.Records, rep.Meta.ParseErrors, fmtDisplayFull(rep.Meta.From), fmtDisplayFull(rep.Meta.To)) + "\n\n"})
	blocks = append(blocks, ParaVM{Text: t.MetaReportConfig(rep.Meta.ReportConfigPath) + "\n\n"})
	blocks = append(blocks, DetailsVM{
		Summary: t.MetaInputListLabel,
		Body:    strings.Join(rep.Meta.Inputs, ", ") + "\n",
	})
	// clientsWithSiblingFile is empty since the per-client sibling files
	// were retired (D7), so the detail link line is never suffixed here.
	blocks = append(blocks, ParaVM{Text: t.DetailLinkLine + "\n\n"})
	if stories != nil {
		blocks = append(blocks, ParaVM{Text: t.StoriesLinkLine(stories.Path, stories.JourneyCount, stories.FromDisplay, stories.ToDisplay)})
	}
	return blocks
}

// ---- §0 摘要 ----

func vmSummarySection(rep *Report2, lang i18n.Lang) SectionVM {
	t := i18n.Doc(lang)
	o := rep.Overall
	sec := SectionVM{ID: "summary", Title: t.SummaryTitle}
	tbl := &TableVM{Headers: t.SummaryHeaders[:]}
	p95n := o.RequestsWithDur
	tbl.row(t.SummaryRequests(o.Requests, o.Fallbacks, o.Truncated),
		pctStr2(o.OK, o.Requests),
		fmtutil.FmtTokens(o.TokensInFresh),
		cacheEffCell(o.CacheEfficiency, o.TokensKnown, o.Requests),
		durCell(o.DurMSP95, p95n),
		summaryCostCell(rep, o, t.SummaryCostUnknown))
	sec.Blocks = append(sec.Blocks, tbl)
	// P-07: name the interactive share explicitly — the top-line request
	// figure includes every workload class.
	if n := summaryInteractiveShare(rep); n >= 0 && o.Requests > 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.SummaryInteractiveNote(o.Requests, n, pctStr(float64(n)/float64(o.Requests)))})
	}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.SummaryStarNote})
	hl := t.HighlightsAuto + "\n"
	for _, h := range highlights(rep, lang) {
		hl += "- " + h + "\n"
	}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: hl + "\n"})
	return sec
}

// summaryCostCell is §0's headline money cell: "Unpriced" rather than a
// number whenever nothing resolved a rate — never 0, which reads as "this
// traffic was free".
func summaryCostCell(rep *Report2, o Row, unknown string) string {
	if rep.Pricing == nil || o.CostEstimate == nil {
		return unknown
	}
	cur := rep.Pricing.Currency
	if cur == "" {
		cur = "USD"
	}
	return money(*o.CostEstimate, cur)
}

// summaryInteractiveShare returns how many of rep's total requests belong
// to the "interactive" workload class, or -1 when rep.Workloads is empty
// (a signal the caller should skip the note). (P-07)
func summaryInteractiveShare(rep *Report2) int {
	if rep == nil || len(rep.Workloads) == 0 {
		return -1
	}
	n := 0
	for i := range rep.Workloads {
		if rep.Workloads[i].Class == "interactive" {
			n += rep.Workloads[i].Requests
		}
	}
	return n
}

// highlightWasteFloorBytes is the minimum absolute tool-schema waste for
// the §0 auto-highlight — below ~8 MB across the whole window it isn't a
// headline, whatever the utilization ratio.
const highlightWasteFloorBytes = 8 << 20

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
	// 2. tool shape with the largest ABSOLUTE schema waste (rep.Tools is
	// sorted by SchemaWasteBytes desc), floored so a few MB of unavoidable
	// slack doesn't manufacture a highlight on a well-behaved corpus.
	if len(rep.Tools) > 0 && rep.Tools[0].SchemaWasteBytes >= highlightWasteFloorBytes {
		tl := rep.Tools[0]
		out = append(out, t.ToolWarn(tl.Shape, tl.Requests, fmtBytesGB(tl.SchemaBytesShipped),
			fmtBytesGB(tl.SchemaWasteBytes), pctStr(tl.DeclareUtilization), len(tl.NeverCalled)))
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

// topErrorClassCount finds the error class with the highest count over
// the sorted keys — ties always resolve to the alphabetically-first class
// name, never map order.
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

func vmRequestIndexSection(rep *Report2, lang i18n.Lang) SectionVM {
	t := i18n.Doc(lang)
	sec := SectionVM{ID: "request-index", Title: t.RequestIndexTitle}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.RequestIndexBody + "\n"})
	// No per-client sibling links: the whole sibling family was retired
	// with the human-readable request index (D7).
	if rep.Meta.DetailsEnabled {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.DetailsCaptureBody})
	} else {
		// Default run (-details=false): details/*.md was never
		// materialized, so point at the on-demand read primitive with a
		// real coordinate from this run's own data (P6.2b).
		example := ""
		if rows := rep.RequestRows(); len(rows) > 0 {
			example = rows[0].Req
		}
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.DetailsOnDemandBody(example)})
	}
	return sec
}

// ---- 附录 ----

// vmAppendixSection is the appendix's "##" heading; its lines are the
// report-level Disclaimers/Footnotes the serializer emits last.
func vmAppendixSection(rep *Report2, lang i18n.Lang) SectionVM {
	return SectionVM{ID: "appendix", Title: i18n.Doc(lang).AppendixTitle}
}

func vmAppendixClosing(rep *Report2, lang i18n.Lang) ([]string, []FootnoteVM) {
	t := i18n.Doc(lang)
	disclaimers := []string{
		t.AppendixInputLine(strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors),
		t.AppendixPeriodLine(fmtDisplayFull(rep.Meta.From), fmtDisplayFull(rep.Meta.To)),
		t.AppendixPercentile(rep.Meta.PercentileMethod),
		t.AppendixNBase,
		t.AppendixLowConf,
		t.AppendixStarMark,
		t.AppendixBillingLine(orDash2(rep.Pricing == nil, t.AppendixNoPricing, "")),
		t.AppendixSlowThresh(rep.Meta.SlowThreshold / 1000),
	}
	var footnotes []FootnoteVM
	if rep.Meta.SelfTrafficExclusionActive {
		footnotes = append(footnotes, FootnoteVM{ID: "self-traffic", Text: t.AppendixSelfTrafficExcluded(rep.Meta.SelfTrafficExcluded)})
	} else {
		footnotes = append(footnotes, FootnoteVM{ID: "self-traffic", Text: t.AppendixSelfTrafficNotExcluded})
	}
	// clientsWithSiblingFile is empty since D7, so every client with
	// traffic but no sibling file is reported.
	withSibling := clientsWithSiblingFile(rep)
	var missingSiblings []string
	for _, c := range rep.ByClient {
		if c.ClientKey != "" && !withSibling[c.ClientKey] {
			missingSiblings = append(missingSiblings, c.ClientKey)
		}
	}
	if len(missingSiblings) > 0 {
		footnotes = append(footnotes, FootnoteVM{ID: "client-reconciliation",
			Text: t.AppendixClientReconciliation(strings.Join(missingSiblings, ", "))})
	}
	return disclaimers, footnotes
}
