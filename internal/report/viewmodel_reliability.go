// Ver 2026-09-15, by Opus 5

// §3 可靠性 view model: outcome mix, per-endpoint health (attempt-level
// availability vs request-level success rate — deliberately distinct), the
// error class distribution, and the quirk-repair marker distribution,
// bucketed by protocol. Pairs with internal/i18n/report_reliability.go.
package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/core"
	"vmr/internal/i18n"
)

func vmReliabilitySection(rep *Report2, o Row, lang i18n.Lang) SectionVM {
	t := i18n.Reliability(lang)
	sec := SectionVM{ID: "reliability", Title: t.Title}

	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.OutcomeTitle + "\n\n"})
	outcomeTbl := &TableVM{Headers: t.OutcomeHeaders[:]}
	outcomeTbl.row(strconv.Itoa(o.OK), strconv.Itoa(o.Errors), strconv.Itoa(o.Canceled), strconv.Itoa(o.Truncated),
		fmt.Sprintf("%d (%d/%d)", o.Fallbacks, o.FallbackRecovered, o.FallbackFailed))
	sec.Blocks = append(sec.Blocks, outcomeTbl)

	// endpoint health (6 cols) - use EndpointsAll for cross-date view,
	// split by protocol
	if len(rep.EndpointsAll) > 0 {
		vmEndpointHealth(&sec, rep, t)
	}

	// error class × endpoint (only non-zero), split by protocol
	nonzero := false
	for _, e := range rep.EndpointsAll {
		if len(e.ErrorClasses) > 0 {
			nonzero = true
			break
		}
	}
	if nonzero {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ErrorByEndpointTitle + "\n\n"})
		protocols, byProto := protocolBuckets(rep.EndpointsAll)
		for _, p := range protocols {
			rows := byProto[p]
			hasAny := false
			for _, e := range rows {
				if len(e.ErrorClasses) > 0 {
					hasAny = true
					break
				}
			}
			if !hasAny {
				continue
			}
			sec.Blocks = append(sec.Blocks, ParaVM{Text: "*" + p + "*\n\n"})
			tbl := &TableVM{Headers: t.ErrorByEndpointHeaders[:]}
			for _, e := range rows {
				for _, cls := range sortedKeysInt(e.ErrorClasses) {
					n := e.ErrorClasses[cls]
					rate := 0.0
					if e.Attempts > 0 {
						rate = float64(n) / float64(e.Attempts) * 100
					}
					tbl.row(e.Endpoint, cls, fmt.Sprintf("%d(%s)", n, pctHundred(rate)))
				}
			}
			sec.Blocks = append(sec.Blocks, tbl)
		}
	}

	// quirk marker × endpoint (only non-zero), split by protocol — the
	// cross-request frequency view detail pages can't provide on their own
	// (see EndpointRow.NormCounts).
	quirkNonzero := false
	for _, e := range rep.EndpointsAll {
		if len(e.NormCounts) > 0 {
			quirkNonzero = true
			break
		}
	}
	if quirkNonzero {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.QuirkByEndpointTitle + "\n\n"})
		protocols, byProto := protocolBuckets(rep.EndpointsAll)
		for _, p := range protocols {
			rows := byProto[p]
			hasAny := false
			for _, e := range rows {
				if len(e.NormCounts) > 0 {
					hasAny = true
					break
				}
			}
			if !hasAny {
				continue
			}
			sec.Blocks = append(sec.Blocks, ParaVM{Text: "*" + p + "*\n\n"})
			tbl := &TableVM{Headers: t.QuirkByEndpointHeaders[:]}
			for _, e := range rows {
				for _, marker := range sortedKeysInt(e.NormCounts) {
					n := e.NormCounts[marker]
					rate := 0.0
					if e.OK > 0 {
						rate = float64(n) / float64(e.OK) * 100
					}
					tbl.row(e.Endpoint, marker, fmt.Sprintf("%d(%s)", n, pctHundred(rate)))
				}
			}
			sec.Blocks = append(sec.Blocks, tbl)
		}
	}

	// error timeline sparkline (per hour error counts from HoursOfDay)
	if len(rep.HoursOfDay) > 0 {
		errs := make([]int64, 24)
		for _, h := range rep.HoursOfDay {
			if h.Hour >= 0 && h.Hour < 24 {
				errs[h.Hour] += int64(h.Errors)
			}
		}
		chartTitle, chartAxis := t.ErrorTimelineChart()
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ErrorTimelineTitle + "\n\n" + mermaidHourBar(chartTitle, chartAxis, errs)})
		// callout the peak hour
		peakH, peakN := 0, int64(0)
		for i, n := range errs {
			if n > peakN {
				peakH, peakN = i, n
			}
		}
		if peakN > 0 {
			sec.Blocks = append(sec.Blocks, ParaVM{Text: t.PeakHourNote(peakH, peakN)})
		}
	}
	return sec
}

func vmEndpointHealth(sec *SectionVM, rep *Report2, t i18n.ReliabilityText) {
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.EndpointHealthTitle + "\n\n"})
	protocols, byProto := protocolBuckets(rep.EndpointsAll)
	for _, p := range protocols {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: "*" + p + "*\n\n"})
		var mainRows, lowNRows []EndpointRow
		for _, e := range byProto[p] {
			if e.Attempts >= 20 {
				mainRows = append(mainRows, e)
			} else {
				lowNRows = append(lowNRows, e)
			}
		}
		if len(mainRows) > 0 {
			tbl := &TableVM{Headers: t.EndpointHeaders[:]}
			for _, e := range mainRows {
				tbl.row(e.Endpoint, strconv.Itoa(e.Attempts), strconv.Itoa(e.OK),
					pctStr(e.Availability), pctHundred(e.ErrorRate)+errorRateMarker(e),
					topErrorClassShort(e))
			}
			sec.Blocks = append(sec.Blocks, tbl)
		}
		if len(lowNRows) > 0 {
			tbl := &TableVM{Headers: t.EndpointHeaders[:]}
			for _, e := range lowNRows {
				tbl.row(e.Endpoint, strconv.Itoa(e.Attempts), strconv.Itoa(e.OK),
					pctStr(e.Availability), pctHundred(e.ErrorRate)+errorRateMarker(e),
					topErrorClassShort(e))
			}
			if len(mainRows) > 0 {
				tbl.Fold = t.LowSampleOpen(len(lowNRows))
			}
			sec.Blocks = append(sec.Blocks, tbl)
		}
	}
}

// endpointProtocol extracts the leading "protocol:" segment from an
// EndpointRow.Endpoint label ("protocol:provider:model").
func endpointProtocol(endpoint string) string {
	if i := strings.IndexByte(endpoint, ':'); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}

// protocolBuckets splits endpoint rows by protocol, preserving each
// row's relative order within its bucket. "openai-completions" sorts
// first, "anthropic-messages" second, any other protocol follows
// alphabetically — the fixed group order every §3/§4 by-protocol table
// renders in.
func protocolBuckets(eps []EndpointRow) ([]string, map[string][]EndpointRow) {
	byProto := map[string][]EndpointRow{}
	var order []string
	for _, e := range eps {
		p := endpointProtocol(e.Endpoint)
		if _, ok := byProto[p]; !ok {
			order = append(order, p)
		}
		byProto[p] = append(byProto[p], e)
	}
	rank := func(p string) int {
		switch p {
		case core.ProtocolOpenAICompletions:
			return 0
		case core.ProtocolAnthropicMessages:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		ri, rj := rank(order[i]), rank(order[j])
		if ri != rj {
			return ri < rj
		}
		return order[i] < order[j]
	})
	return order, byProto
}

// errorRateMarker is the §3 endpoint-health error-rate cell suffix.
// Low-n rows (same n<20 cutoff as render_cells.go's ppCell) get §4's
// ⚠️low-n instead of the error-rate ⚠️: 50% off 2 attempts is not 50% off
// 300.
func errorRateMarker(e EndpointRow) string {
	switch {
	case e.Attempts < 20:
		return " ⚠️low-n"
	case e.ErrorRate > 10:
		return " ⚠️"
	default:
		return ""
	}
}

func topErrorClassShort(e EndpointRow) string {
	if len(e.ErrorClasses) == 0 {
		return "-"
	}
	cls, n := topErrorClassCount(e.ErrorClasses)
	return cls + " ×" + strconv.Itoa(n)
}
