// Ver 2026-08-31, by Opus 5

// The report-half / story-half cost-basis differential test. CLAUDE.md's
// rule is that an analytics number reproducing another must be pinned by a
// test, not a comment — "sharing the formula is only half of it; the BASIS
// is chosen independently on each side, and a wrong basis reads exactly
// like a right one." That is exactly what had happened: both halves priced
// through pricing.Rate.Cost, but internal/report priced records whose
// upstream reported no usage (from a byte-count estimate) while
// internal/story skipped them, so the same traffic produced two different
// totals with nothing in either product saying so.
//
// This lives in cmd/vmr because it is the only package allowed to drive
// both halves' production entry points at once.
package main

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/pricing"
	"vmr/internal/report"
	"vmr/internal/story"
	"vmr/internal/taskseg"
)

const costParityEndpoint = "openai-completions:acme:m"

func costParityResolver(t *testing.T) *pricing.Resolver {
	t.Helper()
	tbl, err := pricing.ParseTable([]byte(`currency: USD
generated_at: "2026-08-31"
rates:
  - key: acme/m
    in_fresh: 10.0
    cache_read: 1.0
    cache_write: 12.5
    out: 40.0
`))
	if err != nil {
		t.Fatalf("ParseTable: %v", err)
	}
	return pricing.NewResolver(tbl, nil, 1, "")
}

// costParitySSE is storySSE plus an optional usage block — the one axis
// this test varies, since "upstream reported usage" vs "upstream reported
// nothing" is the only case the two halves ever disagreed on.
func costParitySSE(text string, withUsage bool) string {
	body := storySSE(text)
	if !withUsage {
		return body
	}
	return `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":4000,"completion_tokens":1500,"total_tokens":5500}}
data: [DONE]`
}

// TestCostBasis_ReportAndStoryAgreeCanceledAndError extends the parity
// differential to the outcome shapes where the two halves' attribution
// rules could (and once did) drift: a request canceled before any 2xx (the
// early-cancel case that made a journey's total exceed the macro report's
// — report unprices it via endpointInfo's empty result while story priced
// the request-side estimate), a canceled mid-stream (2xx committed, no
// usage — priced degraded on BOTH sides), an outcome:"error" record whose
// attempt still committed a 2xx (soft-block failover — priced on both
// sides: Outcome is never the skip reason), and an error with no committed
// response at all (unpriced on both sides). Every shape here pins the same
// rule from both ends: cost belongs to the endpoint that SERVED the client,
// and the two halves must agree on which records those are.
func TestCostBasis_ReportAndStoryAgreeCanceledAndError(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 9, 1, 9, m, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "cancel parity fixture opening instruction")
	doneMsg := storyMsg("assistant", "step reply")

	mk := func(i int, respBody any) audit.Record {
		r := storyRec(at(i), []any{sys, u1, doneMsg}, respBody)
		r.Attempts = []audit.Attempt{{Endpoint: costParityEndpoint, Protocol: "openai-completions", Provider: "acme", Model: "m", Response: &audit.Message{Status: 200, Headers: map[string][]string{}}}}
		return r
	}
	var recs []audit.Record
	// 1. ok with real usage — the only record with an exact price.
	recs = append(recs, mk(0, costParitySSE("served reply with usage", true)))
	// 2. ok, upstream reported nothing — degraded estimate, priced by both.
	recs = append(recs, mk(1, costParitySSE("served reply without usage", false)))
	// 3. canceled BEFORE any 2xx: no client response, attempt with no
	// response — unpriced by both halves.
	canceledEarly := mk(2, nil)
	canceledEarly.Outcome = "canceled"
	canceledEarly.Client.Response = nil
	canceledEarly.Attempts[0].Response = nil
	recs = append(recs, canceledEarly)
	// 4. canceled MID-STREAM: 2xx committed, no usage tail — degraded, priced by both.
	canceledMid := mk(3, costParitySSE("mid-stream reply cut short", false))
	canceledMid.Outcome = "canceled"
	canceledMid.Attempts[0].Error = "canceled"
	recs = append(recs, canceledMid)
	// 5. outcome "error" but the attempt committed a 2xx (soft-block
	// failover shape): served — priced by both, outcome notwithstanding.
	softBlocked := mk(4, costParitySSE("soft blocked then failed over", false))
	softBlocked.Outcome = "error"
	softBlocked.Client.Response = &audit.Message{Status: 500, Headers: map[string][]string{}, Body: `data: {"error":{"message":"upstream down"}}`}
	softBlocked.Attempts[0].Error = "content:soft_block"
	recs = append(recs, softBlocked)
	// 6. outcome "error", no committed response anywhere — unpriced by both.
	failedOutright := mk(5, nil)
	failedOutright.Outcome = "error"
	failedOutright.Client.Response = &audit.Message{Status: 502, Headers: map[string][]string{}, Body: `data: {"error":{"message":"upstream down"}}`}
	failedOutright.Attempts[0].Response = nil
	failedOutright.Attempts[0].Error = "network:connection refused"
	recs = append(recs, failedOutright)

	path := writeStoryJSONL(t, recs)

	res := costParityResolver(t)
	rep, _, err := report.Build([]string{path}, time.Now(), nil, &report.Pricing{Currency: "USD"}, res, nil)
	if err != nil {
		t.Fatalf("report.Build: %v", err)
	}
	if rep.Overall.CostEstimate == nil {
		t.Fatal("report side priced nothing — fixture no longer exercises the pricing path")
	}

	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("ctxgraph.Scan: %v", err)
	}
	byIdx := ctxgraph.LineageIndex(g)
	tails := ctxgraph.StitchedSuccessorSet(g)
	var storyTotal float64
	var pricedSteps, estimatedSteps int
	for _, l := range g.Lineages {
		if tails[l.Idx] {
			continue
		}
		j, err := story.BuildChain(ctxgraph.ChainFrom(l, byIdx), taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("story.BuildChain: %v", err)
		}
		c := story.ComputeJourneyCost(j, res, "USD")
		storyTotal += c.TotalAmount()
		pricedSteps += c.PricedSteps
		estimatedSteps += c.EstimatedSteps
	}
	// Records 1/2/4/5 are served and priced; 3/6 never committed a < 400
	// response and are unpriced on BOTH sides — story pricing them (or
	// report pricing them) would break the equality below.
	if pricedSteps != 4 {
		t.Fatalf("story priced %d/6 steps, want exactly 4 (the served ones)", pricedSteps)
	}
	if estimatedSteps != 3 {
		t.Fatalf("estimatedSteps = %d, want 3 (records 2/4/5 priced from the degraded estimate)", estimatedSteps)
	}
	want := *rep.Overall.CostEstimate
	if d := storyTotal - want; d > 1e-9*(1+want) || d < -1e-9*(1+want) {
		t.Errorf("story total %v != report total %v — the two halves are pricing different records, or the same records on different bases", storyTotal, want)
	}
	// The exact-usage record alone is 10*4000/1e6 + 40*1500/1e6 = 0.10; the
	// total must exceed that to prove the degraded records contributed too.
	if storyTotal < 0.10 {
		t.Errorf("story total %v < 0.10 — the degraded records stopped contributing", storyTotal)
	}
}

func TestCostBasis_ReportAndStoryAgree(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 31, 9, m, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "cost basis parity fixture opening instruction")

	var recs []audit.Record
	msgs := []any{sys, u1}
	for i := 0; i < 6; i++ {
		// Alternating: real reported usage, then none at all (the degraded
		// path both halves must price identically).
		r := storyRec(at(i), append([]any{}, msgs...), costParitySSE("reply body long enough to estimate a non-trivial token count from", i%2 == 0))
		r.Attempts = []audit.Attempt{{Endpoint: costParityEndpoint, Protocol: "openai-completions", Provider: "acme", Model: "m", Response: &audit.Message{Status: 200, Headers: map[string][]string{}}}}
		recs = append(recs, r)
		msgs = append(msgs, storyMsg("assistant", "step reply"))
	}
	path := writeStoryJSONL(t, recs)
	res := costParityResolver(t)
	rep, _, err := report.Build([]string{path}, time.Now(), nil, &report.Pricing{Currency: "USD"}, res, nil)
	if err != nil {
		t.Fatalf("report.Build: %v", err)
	}
	if rep.Overall.CostEstimate == nil {
		t.Fatal("report side priced nothing — fixture no longer exercises the pricing path")
	}

	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("ctxgraph.Scan: %v", err)
	}
	byIdx := ctxgraph.LineageIndex(g)
	// Only chain TAILS become Journeys — a lineage some other lineage is
	// stitched onto is already covered by that one's chain, and counting it
	// twice would make the story side spuriously exceed the report's.
	tails := ctxgraph.StitchedSuccessorSet(g)
	var storyTotal float64
	var pricedSteps, estimatedSteps int
	for _, l := range g.Lineages {
		if tails[l.Idx] {
			continue
		}
		j, err := story.BuildChain(ctxgraph.ChainFrom(l, byIdx), taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("story.BuildChain: %v", err)
		}
		c := story.ComputeJourneyCost(j, res, "USD")
		storyTotal += c.TotalAmount()
		pricedSteps += c.PricedSteps
		estimatedSteps += c.EstimatedSteps
	}
	if pricedSteps != len(recs) {
		t.Fatalf("story priced %d/%d steps — every record has a priceable endpoint", pricedSteps, len(recs))
	}
	if estimatedSteps == 0 {
		t.Fatal("no step took the degraded-estimate path — the fixture stopped covering the case this test exists for")
	}
	want := *rep.Overall.CostEstimate
	if d := storyTotal - want; d > 1e-9*(1+want) || d < -1e-9*(1+want) {
		t.Errorf("story total %v != report total %v — the two halves are pricing different records, or the same records on different bases", storyTotal, want)
	}
}
