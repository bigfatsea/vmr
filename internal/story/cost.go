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
// Endpoint is the full "protocol:provider:model" label.
type ModelCost struct {
	Endpoint string  `json:"endpoint"`
	USD      float64 `json:"usd"`
}

// CostFact is a Journey's estimated cost — every step whose endpoint priced
// and whose usage was reported, summed. Currency is whatever the resolver
// resolved in (or was rescaled to via -currency). Resolved is false when the
// resolver was nil or not one step priced; PricedSteps < TotalSteps means a
// partial estimate (some steps had no resolvable price or no reported usage).
type CostFact struct {
	Currency    string      `json:"currency,omitempty"`
	TotalUSD    float64     `json:"total_usd"`
	Resolved    bool        `json:"resolved"`
	PricedSteps int         `json:"priced_steps"`
	TotalSteps  int         `json:"total_steps"`
	ByModel     []ModelCost `json:"by_model,omitempty"`
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
	for _, s := range steps {
		if s.Manifest == nil || !s.Manifest.UsageOK {
			continue
		}
		rate, ok := res.RateForEndpoint(s.Manifest.Endpoint)
		if !ok {
			continue
		}
		u := s.Manifest.Usage
		c := rate.Cost(u.Fresh(), u.CacheRead, u.CacheWrite, u.Out)
		fact.TotalUSD += c
		byEndpoint[s.Manifest.Endpoint] += c
		fact.PricedSteps++
	}
	if fact.PricedSteps == 0 {
		return fact
	}
	fact.Resolved = true
	for ep, usd := range byEndpoint {
		fact.ByModel = append(fact.ByModel, ModelCost{Endpoint: ep, USD: usd})
	}
	sort.Slice(fact.ByModel, func(a, b int) bool {
		if fact.ByModel[a].USD != fact.ByModel[b].USD {
			return fact.ByModel[a].USD > fact.ByModel[b].USD
		}
		return fact.ByModel[a].Endpoint < fact.ByModel[b].Endpoint
	})
	return fact
}
