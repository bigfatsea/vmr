// Ver 2026-09-15, by pi

// JourneySummary — journey-<id>.json's shape and its one constructor. Split
// out of metrics.go (whose archtest budget it was crowding) so the summary's
// own growth — it must stay a rendering-complete, self-contained projection
// of a Journey (viewmodel.go renders j-<id>.md from it alone) — never again
// competes with the metrics table's line budget.
package journey

import (
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// JourneySummary is journey-<id>.json's shape (design doc: "输出同时落
// journey-<id>.json，供第 4 步的对比模块直接消费") — a Journey's identity
// plus its Metrics profile and rule-derived Findings, so Phase 4d's
// comparison module can diff two Journeys without re-parsing Markdown.
//
// It is also the SINGLE rendering input for j-<id>.md (D11/§5.0 via
// viewmodel.go): every fact the Markdown renders must be reachable from
// here — Structure+Bodies carry the per-Step content, Metrics the aggregates,
// Findings the detector output, and the stamped Break/Deliverable facts the
// two cross-Step judgments the old render path computed from the in-memory
// Journey. A rendering need that isn't on this shape is a data-layer gap,
// not a viewmodel problem.
type JourneySummary struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	// FromDisplay/ToDisplay are From/To in fmtutil.DisplayZone (§5.6: the
	// frontend shows these verbatim, no timezone math). From/To stay the
	// machine form.
	FromDisplay string    `json:"from_display,omitempty"`
	ToDisplay   string    `json:"to_display,omitempty"`
	Partial     bool      `json:"partial,omitempty"`
	Metrics     Metrics   `json:"metrics"`
	Findings    []Finding `json:"findings,omitempty"`
	LLMFindings []Finding `json:"llm_findings,omitempty"`
	// Structure is the complete Task/Step/Event/ToolCall skeleton — the
	// machine-readable counterpart to the human-readable fact-layer
	// (render_md.go's renderStep), P4 (see structure.go's doc comment).
	Structure JourneyStructure  `json:"structure"`
	Bodies    map[string]string `json:"bodies,omitempty"`
	// Cost is the estimated $ spend for this Journey (cost.go), nil when no
	// price book was available at render time — never a fake $0. Every other
	// field here is a pure function of the Journey; this one also needs a
	// *pricing.Resolver, so it's threaded in by the caller rather than
	// computed by Summarize.
	Cost *CostFact `json:"cost,omitempty"`
	// Break is j.Break's edit classification (the unresolved lineage break
	// the .md opens with a warning about, render_md.go's BreakWarning). nil
	// when the Journey's head lineage broke from nothing — the common case.
	Break *EditRef `json:"break,omitempty"`
	// Deliverable is the Journey's final write-shaped tool call
	// (deliverableStats — the same detection the .md's final-deliverable
	// section and -compare's tale-of-the-tape share), stamped at
	// construction because its file-write detection reads each call's FULL
	// arguments, which the truncated bodies blob table cannot reproduce.
	// nil when the Journey never wrote a deliverable.
	Deliverable *DeliverableStats `json:"deliverable,omitempty"`
}

// Summarize builds j's JourneySummary, computing Metrics and Findings
// in the specified target language. The finding Code fields remain stable
// canonical identifiers across languages, while human-readable Finding/Action
// texts follow lang, keeping .json and .md outputs fully aligned —
// compare-*.json's MetricDiff.Label and vmr-report.json's efficiency[]
// follow the same lang-follows-everywhere policy (P8,
// docs/future-strategy/analyze_architecture_redesign_opus-5.md §5.5).
//
// The -compare path (cmd/vmr/cmd_journey.go's compareJourneys) calls this on both
// sides purely to get Metrics for Compare(sA, sB, lang) — Compare/journeyRef
// only ever project ID/Title/From/To/Metrics out of the result, so the
// Structure this also computes (P4) is built and discarded on that path.
// Millisecond-scale waste, not worth a second entry point for; noted here
// so it reads as a known, accepted cost rather than an oversight.
func Summarize(j *Journey, lang i18n.Lang) JourneySummary {
	return NewJourneySummary(j, ComputeMetrics(j), ComputeFindings(j, lang), nil, nil)
}

// NewJourneySummary is JourneySummary's one constructor, shared by Summarize
// (which always computes its own Metrics/Findings and never sets
// llmFindings) and cmd/vmr's writeJourneyFile (which already has
// Metrics/Findings computed by its caller — see Summarize's doc comment on
// why re-deriving them here would cost double — and additionally has
// llmFindings to attach, which Summarize's own signature has no room for).
// Before this existed, writeJourneyFile built its own separate
// JourneySummary{} literal — the exact "same construction, two hand-written
// copies" pattern this project has already been bitten by once (P2's
// detailFileNameFromInfo mirroring detailFileName): P4 added Structure to
// Summarize's literal and, for one build, silently left writeJourneyFile's
// copy without it. One constructor is what keeps that from recurring the
// next time JourneySummary gains a field.
func NewJourneySummary(j *Journey, m Metrics, findings, llmFindings []Finding, cost *CostFact) JourneySummary {
	s := BuildStructure(j)
	sum := JourneySummary{
		ID: j.ID, Title: j.Title, From: j.From, To: j.To, Partial: j.Partial,
		Metrics: m, Findings: findings, LLMFindings: llmFindings,
		Structure: s,
		Bodies:    s.Bodies,
		Cost:      cost,
	}
	if !j.From.IsZero() {
		sum.FromDisplay = j.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
	}
	if !j.To.IsZero() {
		sum.ToDisplay = j.To.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
	}
	// The two cross-Step facts the renderer needs that BuildStructure's
	// per-Step projection can't see: the head lineage's unresolved break,
	// and the final deliverable (whose detection reads full, untruncated
	// tool-call arguments — see the Deliverable field's doc comment).
	if j.Break != nil {
		sum.Break = &EditRef{Kind: j.Break.Edit.Kind.String(), LCP: j.Break.Edit.LCP, Coverage: j.Break.Edit.Coverage}
	}
	if d := deliverableStats(j); d.Found {
		sum.Deliverable = &d
	}
	return sum
}
