// Ver 2026-08-05, by Sonnet 5

package story

import (
	"fmt"
	"sort"
	"strings"

	"vmr/internal/i18n"
)

// corpusCorrelationsShown caps how many correlation rows the Markdown
// table lists — see the call site's comment for why the full list would
// otherwise read as noise, not signal.
const corpusCorrelationsShown = 15

// RenderCorpusMarkdown renders stats as a self-contained Markdown report —
// same fact-layer-only convention as RenderMarkdown/RenderComparisonMarkdown:
// every number here is a value CorpusStats already computed, no judgment
// calls happen in this file.
func RenderCorpusMarkdown(stats CorpusStats, lang i18n.Lang) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	t := i18n.Corpus(lang)

	w("%s", t.Title)
	w("%s", t.JourneyCount(stats.JourneyCount))
	if stats.JourneyCount == 0 {
		w("%s", t.NoJourneys)
		return b.String()
	}

	w("%s", t.MetricDistTitle)
	w("%s", t.MetricDistHeader)
	for _, spec := range metricSpecs {
		d, ok := stats.MetricDist[spec.Code]
		if !ok {
			continue
		}
		w("| %s | %d | %s | %s | %s | %s | %s |\n",
			i18n.MetricLabel(lang, string(spec.Code)), d.Count,
			formatMetric(spec.Kind, d.Mean), formatMetric(spec.Kind, d.Median), formatMetric(spec.Kind, d.Min), formatMetric(spec.Kind, d.Max), formatMetric(spec.Kind, d.P90))
	}
	w("\n")

	if note := anthropicCoverageNote(stats.ProtocolShare, t); note != "" {
		w("%s", note)
	}

	w("%s", t.FindingRateTitle)
	if len(stats.FindingRate) == 0 {
		w("%s", t.NoFindings)
	} else {
		w("%s", t.FindingRateHeader)
		for _, code := range sortedFindingCodes(stats.FindingRate) {
			w("| %s | %s |\n", code, pctStr(stats.FindingRate[code]))
		}
		w("\n")
	}

	w("%s", t.CorrelationTitle)
	if len(stats.Correlations) == 0 {
		w("%s", t.NoCorrelations)
	} else {
		w("%s", t.CorrelationHeader)
		// Top-N by |rho| — the full list (routinely dozens of pairs once
		// weakly-correlated ones clear the 0.3 floor, many of them
		// mechanically implied by a metric's own formula, e.g.
		// NetWorkingMS = ModelMS + AgentExecMS) always lands in the JSON;
		// the Markdown table stays scannable the same way internal/report's
		// "Tool Shape Waste Top-5" table already does.
		shown := stats.Correlations
		if len(shown) > corpusCorrelationsShown {
			shown = shown[:corpusCorrelationsShown]
		}
		for _, c := range shown {
			w("| %s | %s | %.2f | %d |\n", i18n.MetricLabel(lang, string(c.MetricA)), i18n.MetricLabel(lang, string(c.MetricB)), c.Rho, c.N)
		}
		w("\n")
		if extra := len(stats.Correlations) - len(shown); extra > 0 {
			w("%s", t.CorrelationMore(extra))
		}
	}
	w("%s", t.CorrelationFootnote)

	w("%s", t.GroupCompTitle)
	if len(stats.GroupComparisons) == 0 {
		w("%s", t.NoGroupComparisons)
	} else {
		w("%s", t.GroupCompHeader)
		for _, g := range stats.GroupComparisons {
			mark := ""
			if g.Notable {
				mark = " ⚠️"
			}
			w("| %s%s | %d | %d | %s | %s | %s |\n",
				g.Code, mark, g.HitCount, g.NoHitCount,
				formatMetric(KindMillis, g.HitMedian), formatMetric(KindMillis, g.NoHitMedian), formatDeltaRel(g.DeltaRel))
		}
		w("\n")
	}
	if len(stats.SkippedGroupComparisons) > 0 {
		names := make([]string, len(stats.SkippedGroupComparisons))
		for i, c := range stats.SkippedGroupComparisons {
			names[i] = string(c)
		}
		w("%s", t.SkippedGroupComparisons(strings.Join(names, ", ")))
	}
	w("%s", t.GroupCompFootnote)

	renderContextRotSection(&b, stats.ContextRot, lang)
	renderToolSequenceSection(&b, stats.ToolSequences, lang)

	return b.String()
}

func sortedFindingCodes(m map[FindingCode]float64) []FindingCode {
	out := make([]FindingCode, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
