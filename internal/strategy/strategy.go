// Ver 2026-09-23 03:00, by Claude Opus 5.5

// Package strategy implements candidate elimination and ordering as two
// decoupled functions: elimination (Eligible, which inspects request facts)
// and ordering (Sort, which inspects only static endpoint attributes).
// This preserves the invariant that elimination and ordering stay separate,
// structurally guaranteed by the function signatures themselves (Sort does
// not receive a request).
package strategy

import (
	"sort"

	"vmr/internal/core"
)

// Sort orders endpoints by Priority ascending (lower number wins).
// The sort is stable, so equal priorities keep config-file order.
func Sort(eps []*core.Endpoint) {
	sort.SliceStable(eps, func(i, j int) bool {
		return eps[i].Priority < eps[j].Priority
	})
}

// Condition tests whether one endpoint may serve a request at all, based on
// facts derived from the request and static properties the endpoint
// declares in config (core.Endpoint.Capabilities). Unlike Sort
// (endpoint-vs-endpoint ordering, no request access), a Condition is
// request-aware and elimination-only — it never reorders candidates, it
// only says yes or no. See
// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing
// section for the architectural rationale (Sort structurally cannot see the
// request; elimination and ordering are separate concerns).
//
// conditions (conditions.go) is the fixed compile-time Condition set — read
// once per endpoint per request by Eligible, the actual per-request hot path.
// All of them participate unconditionally — there is no per-model opt-in list,
// because Condition composition is always plain AND with no meaningful
// ordering between conditions, and an endpoint that hasn't declared the
// relevant capability is unconstrained by definition (see
// core.Endpoint.HasCapability).
type Condition interface {
	Name() string
	Eligible(ep *core.Endpoint, facts core.RequestFacts) bool
}

// Eligible reports whether ep passes every registered hard Condition for
// this request. Context length is deliberately NOT one of these — it's
// applied separately by WithinContext with its own fallback rule,
// because it rests on an estimate rather than a certainty the way
// capability conditions do.
func Eligible(ep *core.Endpoint, facts core.RequestFacts) bool {
	for _, c := range conditions {
		if !c.Eligible(ep, facts) {
			return false
		}
	}
	return true
}

// RejectedBy returns the names of every registered Condition that rejects
// ep for this request, for building a diagnostic message when a whole
// candidate set is eliminated. Not on the hot path — only called once
// Serve() already knows candidates ended up empty.
func RejectedBy(ep *core.Endpoint, facts core.RequestFacts) []string {
	var names []string
	for _, c := range conditions {
		if !c.Eligible(ep, facts) {
			names = append(names, c.Name())
		}
	}
	return names
}

// WithinContext reports whether ep's declared context window can plausibly
// fit this request's estimated size. Unset MaxContextTokens (0) is
// unconstrained. This is intentionally not a Condition: a fallback (never
// let this estimate alone empty a non-empty candidate set) is required
// that only the caller — which already knows the pre-context-filter
// candidate set — can apply correctly.
func WithinContext(ep *core.Endpoint, facts core.RequestFacts) bool {
	return ep.MaxContextTokens == 0 || facts.EstimatedTokens <= ep.MaxContextTokens
}
