// Ver 2026-08-21, by Sonnet 5

// P14.2's detector-coverage disclosure (KNOWN_ISSUES §1.43): split out of
// corpus.go once this pushed that file over archtest's file-line budget —
// same package, no new import boundary. See RenderCorpusMarkdown's call to
// anthropicCoverageNote for where this actually surfaces.
package story

import (
	"strings"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// anthropicOnlyCoverage names the reader-facing signals gated on
// isErrorMarker/chatmsg.ToolResult.IsError — text/fields only ever
// populated from an Anthropic tool_result's is_error field (see
// toolresults.go's doc comment: "always false for OpenAI-shaped results",
// and metrics.go's isErrorMarker doc comment for the text-marker path
// several of these route through instead of reading IsError directly).
// KNOWN_ISSUES §2.4 already forecloses content-sniffing OpenAI's tool
// results to work around this (495,672 records scanned, the target JSON
// shape appeared 0 times) — the only remaining gap was disclosure, not
// detection, which this list exists to drive (see anthropicCoverageNote).
//
// Findings/Metrics are exhaustive as of this writing (grep -n
// "isErrorMarker\|\.IsError" internal/story/*.go, excluding _test.go and
// llm_findings.go's evidence-pack construction — an LLM-judged Finding
// degrades gracefully on weaker evidence rather than going structurally
// silent, a different failure mode this list doesn't claim to cover).
// CorpusSections/JourneySections name views that read the same signal but
// aren't FindingCode/MetricCode-keyed — free text since there's no code to
// key them by. CorpusSections only exist in -corpus output
// (ContextRotBucket.ErrorRate/ErrorStepCount, ToolSequencePattern.ErrorRate);
// JourneySections only exist in a single journey's own report/JSON (the
// decision spine's ❌/↩️ tool-result badge, structure.json's
// ToolCalls[].ResultError).
var anthropicOnlyCoverage = struct {
	Findings        []FindingCode
	Metrics         []MetricCode
	CorpusSections  []string
	JourneySections []string
}{
	Findings:        []FindingCode{FindingUnadaptedRetry, FindingUnverifiedSuccess},
	Metrics:         []MetricCode{MetricErrorRecoveryCount},
	CorpusSections:  []string{"Context Rot error rate", "Tool Sequence error rate"},
	JourneySections: []string{"decision spine's tool-result ❌ badge", "structure.json's ToolCalls[].ResultError"},
}

// anthropicCoverageNote renders P14.2's disclosure line (KNOWN_ISSUES
// §1.43), or "" when protocolShare is empty (never computed, e.g. a
// hand-built CorpusStats, or protocolShare's own zero-Steps edge case:
// asserting "Anthropic traffic is scarce" from data we don't actually have
// would be exactly the kind of unearned claim §5.6's discipline rules out)
// or this corpus is (up to floating-point noise) 100% anthropic-messages —
// the only case where anthropicOnlyCoverage's signals are NOT structurally
// blind on some slice of the data. No intermediate threshold: any non-
// Anthropic share, however small, means a 0% hit rate or all-zero metric
// on that slice reads as "checked, no issue found" when it's really
// "couldn't be checked" — Package E's original 1%-cliff design (a corpus
// at 1.2% Anthropic printed nothing, silently reintroducing the exact
// ambiguity this note exists to close) was cut for exactly this reason.
func anthropicCoverageNote(protocolShare map[string]float64, t i18n.CorpusText) string {
	if len(protocolShare) == 0 || protocolShare[core.ProtocolAnthropicMessages] > 1-1e-9 {
		return ""
	}
	names := anthropicOnlyCoverageNames(anthropicOnlyCoverage.CorpusSections)
	return t.AnthropicOnlyCoverageNote(fmtutil.FmtPercent(protocolShare[core.ProtocolAnthropicMessages], 1), strings.Join(names, ", "))
}

// journeyAnthropicCoverageNote is anthropicCoverageNote's per-journey
// counterpart (P14.2 follow-up — an independent review, 2026-08-21, found
// scoping this disclosure to -corpus only left it unreachable from the
// default suite, the path most readers actually use). Fires only when j
// has NO anthropic-messages Steps at all — unlike the corpus note's "not
// literally 100%" rule, a single journey's traffic is normally not
// mixed-protocol, so "any Anthropic Steps present" is the meaningful
// binary here, not a percentage.
func journeyAnthropicCoverageNote(j *Journey, t i18n.SpineText) string {
	share := protocolShare([]*Journey{j})
	if len(share) == 0 || share[core.ProtocolAnthropicMessages] > 0 {
		return ""
	}
	names := anthropicOnlyCoverageNames(anthropicOnlyCoverage.JourneySections)
	return t.AnthropicOnlyCoverageNote(strings.Join(names, ", "))
}

// anthropicOnlyCoverageNames flattens anthropicOnlyCoverage's Finding/Metric
// codes plus the given free-text section names into one ordered list —
// shared by anthropicCoverageNote and journeyAnthropicCoverageNote so the
// two can't silently drift on which codes they each remember to include.
func anthropicOnlyCoverageNames(sections []string) []string {
	names := make([]string, 0, len(anthropicOnlyCoverage.Findings)+len(anthropicOnlyCoverage.Metrics)+len(sections))
	for _, c := range anthropicOnlyCoverage.Findings {
		names = append(names, string(c))
	}
	for _, c := range anthropicOnlyCoverage.Metrics {
		names = append(names, string(c))
	}
	names = append(names, sections...)
	return names
}

// protocolShare tallies each Step's ctxgraph.Manifest.Protocol across every
// journey and returns each value's fraction of the total — the denominator
// is Steps, not journeys or requests, matching how Metrics/Findings are
// themselves computed per-Step within a Journey. A Step with an empty
// Protocol (shouldn't happen on real audit records, but cheaper to count
// than to special-case) is tallied under "" like any other value; callers
// that only care about "is this corpus Anthropic-only" read
// share["anthropic-messages"].
func protocolShare(journeys []*Journey) map[string]float64 {
	counts := map[string]int{}
	total := 0
	for _, j := range journeys {
		for _, task := range j.Tasks {
			for _, s := range task.Steps {
				if s.Manifest == nil {
					continue
				}
				counts[s.Manifest.Protocol]++
				total++
			}
		}
	}
	if total == 0 {
		return map[string]float64{}
	}
	share := make(map[string]float64, len(counts))
	for proto, n := range counts {
		share[proto] = float64(n) / float64(total)
	}
	return share
}
