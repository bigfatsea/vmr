// Ver 2026-08-30, by Sonnet 5

package story

import (
	"encoding/json"
	"strings"
	"testing"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/pricing"
)

// costResolver builds a Resolver over a tiny two-model table. Keys are
// "provider/model" so RateForEndpoint("proto:provider:model") resolves via
// resolveCanonicalKey's provider+"/"+model step, no Map needed.
func costResolver(t *testing.T) *pricing.Resolver {
	t.Helper()
	tbl, err := pricing.ParseTable([]byte(`currency: USD
generated_at: "2026-08-30"
rates:
  - key: acme/big
    in_fresh: 10.0
    cache_read: 1.0
    cache_write: 12.5
    out: 40.0
  - key: acme/small
    in_fresh: 1.0
    cache_read: 0.1
    cache_write: 1.25
    out: 4.0
`))
	if err != nil {
		t.Fatalf("ParseTable: %v", err)
	}
	return pricing.NewResolver(tbl, nil, 1, "")
}

func costStep(seq int, endpoint string, usageOK bool, u chatmsg.Usage) *Step {
	return &Step{Seq: seq, Manifest: &ctxgraph.Manifest{Endpoint: endpoint, ServedEndpoint: endpoint, UsageInOK: usageOK, UsageOutOK: usageOK, Usage: u}}
}

// estStep is a step whose upstream reported NO usage — ctxgraph.BuildManifest
// fills EstIn/EstOut instead, and pricing must fall back to those (the basis
// internal/report has always used).
func estStep(seq int, endpoint string, estIn, estOut int64) *Step {
	return &Step{Seq: seq, Manifest: &ctxgraph.Manifest{Endpoint: endpoint, ServedEndpoint: endpoint, EstIn: estIn, EstOut: estOut}}
}

// unservedStep is a step whose record never committed a < 400 response to
// the client (early client cancel, all attempts failing) — Endpoint still
// names the last attempted endpoint, but ServedEndpoint (the cost-
// attribution field) is empty, matching what ctxgraph.BuildManifest builds
// for such a record.
func unservedStep(seq int, endpoint string, estIn, estOut int64) *Step {
	return &Step{Seq: seq, Manifest: &ctxgraph.Manifest{Endpoint: endpoint, EstIn: estIn, EstOut: estOut}}
}

func TestComputeJourneyCost_NilResolver(t *testing.T) {
	j := journeyOf(costStep(1, "openai-completions:acme:big", true, chatmsg.Usage{In: 1000, Out: 100}))
	got := ComputeJourneyCost(j, nil, "USD")
	if got.Resolved || got.Total != nil || len(got.ByModel) != 0 {
		t.Fatalf("nil resolver must yield an unresolved zero fact, got %+v", got)
	}
	if got.TotalSteps != 1 {
		t.Fatalf("TotalSteps = %d, want 1", got.TotalSteps)
	}
	if got.Partial() {
		t.Fatal("an unresolved fact is not Partial() (it's simply unknown)")
	}
}

func TestComputeJourneyCost_SumsPricedStepsAndSortsByModel(t *testing.T) {
	res := costResolver(t)
	j := journeyOf(
		// small: fresh = In-CacheRead-CacheWrite = 1_000_000 - 0 - 0
		costStep(1, "openai-completions:acme:small", true, chatmsg.Usage{In: 1_000_000, Out: 0}),
		// big: fresh 500k + cacheRead 500k + out 200k
		costStep(2, "openai-completions:acme:big", true, chatmsg.Usage{In: 1_000_000, CacheRead: 500_000, Out: 200_000}),
	)
	got := ComputeJourneyCost(j, res, "USD")

	if !got.Resolved || got.PricedSteps != 2 || got.TotalSteps != 2 {
		t.Fatalf("want resolved 2/2, got %+v", got)
	}
	if got.Partial() {
		t.Fatal("every step priced — not Partial()")
	}
	// small: 1.0 * 1.0 (M tokens) = 1.0
	// big: 10*0.5 + 1*0.5 + 40*0.2 = 5 + 0.5 + 8 = 13.5
	if d := got.TotalAmount() - 14.5; d > 1e-9 || d < -1e-9 {
		t.Fatalf("TotalUSD = %v, want 14.5", got.TotalAmount())
	}
	// ByModel is spend-descending: big (13.5) before small (1.0).
	if len(got.ByModel) != 2 ||
		got.ByModel[0].Endpoint != "openai-completions:acme:big" ||
		got.ByModel[1].Endpoint != "openai-completions:acme:small" {
		t.Fatalf("ByModel not spend-descending: %+v", got.ByModel)
	}
	if got.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD (the resolver carries none of its own)", got.Currency)
	}
}

// A step goes unpriced for exactly one reason now — its ENDPOINT resolves no
// rate. "The upstream reported no usage" is no longer a second reason: those
// steps are priced from the degraded estimate, the same way
// internal/report's §2 total has always priced them.
func TestComputeJourneyCost_PartialWhenSomeStepsUnpriced(t *testing.T) {
	res := costResolver(t)
	j := journeyOf(
		costStep(1, "openai-completions:acme:small", true, chatmsg.Usage{In: 1_000_000}),
		costStep(2, "openai-completions:unknown:model", true, chatmsg.Usage{In: 1_000_000}), // no table entry
		estStep(3, "openai-completions:acme:small", 1_000_000, 0),                           // no usage reported -> estimate
		&Step{Seq: 4}, // no manifest at all
	)
	got := ComputeJourneyCost(j, res, "USD")
	if !got.Resolved {
		t.Fatal("at least one step priced — Resolved must be true")
	}
	if got.PricedSteps != 2 || got.TotalSteps != 4 {
		t.Fatalf("want 2/4 priced (the estimated step counts), got %d/%d", got.PricedSteps, got.TotalSteps)
	}
	if got.EstimatedSteps != 1 {
		t.Errorf("EstimatedSteps = %d, want 1 — the degraded share must be disclosed, not hidden inside the total", got.EstimatedSteps)
	}
	// 1M fresh in at 1.0/1M, twice: once from real usage, once from the estimate.
	if want := 2.0; got.TotalAmount() < want-1e-9 || got.TotalAmount() > want+1e-9 {
		t.Errorf("Total = %v, want %v", got.TotalAmount(), want)
	}
	if !got.Partial() {
		t.Fatal("Partial() must be true when PricedSteps < TotalSteps")
	}
}

func TestComputeJourneyCost_NoStepPricedStaysUnresolved(t *testing.T) {
	res := costResolver(t)
	j := journeyOf(
		costStep(1, "openai-completions:unknown:model", true, chatmsg.Usage{In: 1_000_000}),
		costStep(2, "openai-completions:also-unknown:model", false, chatmsg.Usage{In: 1_000_000}),
	)
	got := ComputeJourneyCost(j, res, "USD")
	if got.Resolved || got.Total != nil || got.PricedSteps != 0 {
		t.Fatalf("nothing priced — want unresolved with a nil Total, got %+v", got)
	}
	if got.Partial() {
		t.Fatal("unresolved is not Partial()")
	}
}

// TestComputeJourneyCost_UnservedStepsUnpriced pins the early-cancel fix:
// a record whose attempts never committed a < 400 response (canceled before
// any 2xx, or every attempt failing) carries an empty ServedEndpoint, and
// pricing it would make a journey's total exceed the macro report's — which
// unprices the same record (endpointInfo returns "" there). The attempted
// endpoint is still recorded on Manifest.Endpoint, but only ServedEndpoint
// is the cost-attribution basis.
func TestComputeJourneyCost_UnservedStepsUnpriced(t *testing.T) {
	res := costResolver(t)
	j := journeyOf(
		costStep(1, "openai-completions:acme:small", true, chatmsg.Usage{In: 1_000_000}),
		unservedStep(2, "openai-completions:acme:big", 5_000_000, 0), // canceled before any 2xx
	)
	got := ComputeJourneyCost(j, res, "USD")
	if !got.Resolved || got.PricedSteps != 1 || got.TotalSteps != 2 {
		t.Fatalf("want resolved 1/2, got %+v", got)
	}
	if got.EstimatedSteps != 0 {
		t.Errorf("EstimatedSteps = %d, want 0 (the unserved step never reached pricing)", got.EstimatedSteps)
	}
	if d := got.TotalAmount() - 1.0; d > 1e-9 || d < -1e-9 {
		t.Fatalf("Total = %v, want 1.0 (only the served step)", got.TotalAmount())
	}
}

// TestComputeJourneyCost_ErrorOutcomeWithServedEndpointStillPriced pins the
// other half of the attribution rule: Outcome is never the skip reason — a
// record whose attempt still committed a 2xx (a soft-block failover whose
// later attempt failed, outcome "error") is priced exactly like
// internal/report prices it, via the same served endpoint.
func TestComputeJourneyCost_ErrorOutcomeWithServedEndpointStillPriced(t *testing.T) {
	res := costResolver(t)
	s := estStep(1, "openai-completions:acme:small", 2_000_000, 0)
	s.Manifest.Outcome = "error"
	j := journeyOf(s)
	got := ComputeJourneyCost(j, res, "USD")
	if !got.Resolved || got.PricedSteps != 1 {
		t.Fatalf("want resolved 1/1, got %+v", got)
	}
	if d := got.TotalAmount() - 2.0; d > 1e-9 || d < -1e-9 {
		t.Fatalf("Total = %v, want 2.0", got.TotalAmount())
	}
}

func TestComputeJourneyCost_IncompleteRate(t *testing.T) {
	tbl, err := pricing.ParseTable([]byte(`currency: USD
generated_at: "2026-08-30"
rates:
  - key: acme/incomplete
    in_fresh: 10.0
    out: 40.0
`))
	if err != nil {
		t.Fatalf("ParseTable: %v", err)
	}
	res := pricing.NewResolver(tbl, nil, 1, "")
	j := journeyOf(costStep(1, "openai-completions:acme:incomplete", true, chatmsg.Usage{In: 1000, CacheRead: 900, Out: 100}))
	got := ComputeJourneyCost(j, res, "USD")
	if !got.Resolved {
		t.Fatal("want Resolved=true")
	}
	if got.IncompleteSteps != 1 {
		t.Errorf("IncompleteSteps = %d, want 1", got.IncompleteSteps)
	}
}

// TestComputeJourneyCost_UnresolvedSerializesAsAbsent is problem 33's own
// regression: `total_usd: 0` on an unresolved journey read as "this traffic
// was free" to any consumer that didn't first check `resolved`, and the
// `_usd` suffix contradicted the `currency` field beside it.
func TestComputeJourneyCost_UnresolvedSerializesAsAbsent(t *testing.T) {
	j := journeyOf(costStep(1, "openai-completions:acme:big", true, chatmsg.Usage{In: 1000}))
	raw, err := json.Marshal(ComputeJourneyCost(j, nil, "CNY"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(raw); strings.Contains(s, "total_usd") || strings.Contains(s, `"total"`) {
		t.Errorf("unresolved cost serialized as %s — want no total field at all (absent, not 0)", s)
	}
}

func TestCostFact_Partial(t *testing.T) {
	cases := []struct {
		name string
		c    CostFact
		want bool
	}{
		{"unresolved", CostFact{Resolved: false, PricedSteps: 0, TotalSteps: 3}, false},
		{"full", CostFact{Resolved: true, PricedSteps: 3, TotalSteps: 3}, false},
		{"partial", CostFact{Resolved: true, PricedSteps: 1, TotalSteps: 3}, true},
	}
	for _, tc := range cases {
		if got := tc.c.Partial(); got != tc.want {
			t.Errorf("%s: Partial() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// fp is a float64 literal helper for CostFact.Total, which is a pointer so
// "unresolved" serializes as absent rather than as 0.
func fp(v float64) *float64 { return &v }
