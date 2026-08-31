// Ver 2026-08-29, by Sonnet 5

// Per-journey cost: the one economics number the behavior profile
// (metrics.go) deliberately leaves out because it needs an external price
// book, not just the Journey. Same "rule-derived fact that also needs
// Step/Manifest access" class as compare.go's ComparisonExtras — plus a
// *pricing.Resolver. A nil resolver (no config.yaml reachable, or the
// embedded standard table failed to load) yields Resolved:false and a zero
// total: unknown, never a fake $0 — the distinction internal/pricing is
// built around. Formula and token basis match internal/report/cost.go's
// costFor (both go through pricing.Rate.Cost), so a journey's $ line and the
// macro report's $ column can't drift.
package story

import (
	"sort"

	"vmr/internal/pricing"
)

// ModelCost is one upstream endpoint's priced share of a Journey's spend.
// Endpoint is the full "protocol:provider:model" label; Amount is in
// CostFact.Currency, which is NOT necessarily USD (the field was called
// "usd" and carried CNY under -currency CNY).
type ModelCost struct {
	Endpoint string  `json:"endpoint"`
	Amount   float64 `json:"amount"`
}

// CostFact is a Journey's estimated cost — every step whose endpoint
// resolved a rate, summed. Currency is whatever the resolver resolved in
// (or was rescaled to via -currency). Resolved is false when the resolver
// was nil or not one step priced; PricedSteps < TotalSteps means a partial
// estimate (some steps' endpoints had no resolvable price).
//
// Total is a POINTER, and the field is not named "_usd": both were machine-
// readable lies. `total_usd: 0` on an unresolved journey read as "this was
// free" to any consumer that didn't first check `resolved` — the exact
// unknown-vs-zero confusion internal/pricing is built to prevent — and the
// `_usd` suffix contradicted the `currency` field sitting beside it. Now:
// null (omitted) when nothing priced, and the unit is stated once, in
// Currency.
type CostFact struct {
	Currency    string   `json:"currency,omitempty"`
	Total       *float64 `json:"total,omitempty"`
	Resolved    bool     `json:"resolved"`
	PricedSteps int      `json:"priced_steps"`
	TotalSteps  int      `json:"total_steps"`
	// EstimatedSteps is how many of PricedSteps were priced from a degraded
	// token estimate (the upstream reported no usage) rather than real
	// reported usage — the per-journey counterpart of the macro report's §2
	// degraded-share note. Those steps are IN Total, as they always have
	// been on the report side.
	EstimatedSteps int `json:"estimated_steps,omitempty"`
	// IncompleteSteps is how many of PricedSteps were priced through a rate
	// missing at least one component (pricing.Rate.Complete is false) — nil
	// components price as 0, so those steps' cost is a low bound (the
	// per-journey counterpart of EndpointRow.CostRateIncomplete).
	IncompleteSteps int         `json:"incomplete_steps,omitempty"`
	ByModel         []ModelCost `json:"by_model,omitempty"`
}

// TotalAmount is Total dereferenced, 0 when unresolved — for renderers that
// have already checked Resolved and just want the number.
func (c CostFact) TotalAmount() float64 {
	if c.Total == nil {
		return 0
	}
	return *c.Total
}

// Partial reports whether the estimate covers only some of the Journey's
// steps — a renderer showing "$X" should signal "at least $X" instead.
func (c CostFact) Partial() bool { return c.Resolved && c.PricedSteps < c.TotalSteps }

// ComputeJourneyCost prices every step of j through res. currency only
// labels the result (the resolver carries no currency of its own). res ==
// nil is the documented "no pricing available" path — CostFact{Resolved:false}.
func ComputeJourneyCost(j *Journey, res *pricing.Resolver, currency string) CostFact {
	steps := journeySteps(j)
	fact := CostFact{Currency: currency, TotalSteps: len(steps)}
	if res == nil {
		return fact
	}
	byEndpoint := map[string]float64{}
	var total float64
	for _, s := range steps {
		if s.Manifest == nil {
			continue
		}
		// Same attribution rule as internal/report's endpointInfo, carried on
		// the manifest itself as ServedEndpoint (built by ctxgraph from the
		// same attempts loop report runs): cost belongs to an endpoint that
		// actually SERVED the client — one that committed a < 400 response
		// header. A request canceled before any 2xx (or failed outright
		// with network/4xx/5xx errors) is unpriced here AND on the report
		// side (whose endpointInfo returns "" for the same record), so an
		// early cancel can't make a journey's total exceed the macro
		// report's; a canceled or truncated stream that DID commit a 2xx
		// stays priced — the tokens were genuinely consumed. Note the
		// attribution keys off the served endpoint, never the Outcome: an
		// outcome:"error" record whose attempt still committed a 2xx (a
		// soft-block failover) is priced exactly like report prices it.
		ep := s.Manifest.ServedEndpoint
		if ep == "" {
			continue
		}
		rate, ok := res.RateForEndpoint(ep)
		if !ok {
			continue
		}
		// Same basis as internal/report/cost.go's costFor: real usage when
		// the upstream reported it, the degraded estimate otherwise. Skipping
		// the unsniffed steps (what this did until 2026-08-31) made a
		// journey's total quietly lower than the macro report's for the very
		// same records, with nothing in either product saying so.
		u := s.Manifest.Usage
		c := rate.Cost(u.Fresh(), u.CacheRead, u.CacheWrite, u.Out)
		if !s.Manifest.UsageOK {
			c = rate.Cost(s.Manifest.EstIn, 0, 0, s.Manifest.EstOut)
			fact.EstimatedSteps++
		}
		if !rate.Complete() {
			fact.IncompleteSteps++
		}
		total += c
		byEndpoint[ep] += c
		fact.PricedSteps++
	}
	if fact.PricedSteps == 0 {
		return fact
	}
	fact.Resolved = true
	fact.Total = &total
	for ep, amount := range byEndpoint {
		fact.ByModel = append(fact.ByModel, ModelCost{Endpoint: ep, Amount: amount})
	}
	sort.Slice(fact.ByModel, func(a, b int) bool {
		if fact.ByModel[a].Amount != fact.ByModel[b].Amount {
			return fact.ByModel[a].Amount > fact.ByModel[b].Amount
		}
		return fact.ByModel[a].Endpoint < fact.ByModel[b].Endpoint
	})
	return fact
}
