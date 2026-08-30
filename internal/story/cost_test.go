// Ver 2026-08-30, by Sonnet 5

package story

import (
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
	return pricing.NewResolver(tbl, nil)
}

func costStep(seq int, endpoint string, usageOK bool, u chatmsg.Usage) *Step {
	return &Step{Seq: seq, Manifest: &ctxgraph.Manifest{Endpoint: endpoint, UsageOK: usageOK, Usage: u}}
}

func TestComputeJourneyCost_NilResolver(t *testing.T) {
	j := journeyOf(costStep(1, "openai-completions:acme:big", true, chatmsg.Usage{In: 1000, Out: 100}))
	got := ComputeJourneyCost(j, nil, "USD")
	if got.Resolved || got.TotalUSD != 0 || len(got.ByModel) != 0 {
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
	if d := got.TotalUSD - 14.5; d > 1e-9 || d < -1e-9 {
		t.Fatalf("TotalUSD = %v, want 14.5", got.TotalUSD)
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

func TestComputeJourneyCost_PartialWhenSomeStepsUnpriced(t *testing.T) {
	res := costResolver(t)
	j := journeyOf(
		costStep(1, "openai-completions:acme:small", true, chatmsg.Usage{In: 1_000_000}),
		costStep(2, "openai-completions:unknown:model", true, chatmsg.Usage{In: 1_000_000}), // no table entry
		costStep(3, "openai-completions:acme:small", false, chatmsg.Usage{In: 1_000_000}),   // usage not reported
		&Step{Seq: 4}, // no manifest at all
	)
	got := ComputeJourneyCost(j, res, "USD")
	if !got.Resolved {
		t.Fatal("at least one step priced — Resolved must be true")
	}
	if got.PricedSteps != 1 || got.TotalSteps != 4 {
		t.Fatalf("want 1/4 priced, got %d/%d", got.PricedSteps, got.TotalSteps)
	}
	if !got.Partial() {
		t.Fatal("Partial() must be true when PricedSteps < TotalSteps")
	}
}

func TestComputeJourneyCost_NoStepPricedStaysUnresolved(t *testing.T) {
	res := costResolver(t)
	j := journeyOf(
		costStep(1, "openai-completions:unknown:model", true, chatmsg.Usage{In: 1_000_000}),
		costStep(2, "openai-completions:acme:small", false, chatmsg.Usage{In: 1_000_000}),
	)
	got := ComputeJourneyCost(j, res, "USD")
	if got.Resolved || got.TotalUSD != 0 || got.PricedSteps != 0 {
		t.Fatalf("nothing priced — want unresolved zero, got %+v", got)
	}
	if got.Partial() {
		t.Fatal("unresolved is not Partial()")
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
