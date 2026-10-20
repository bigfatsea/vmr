// Ver 2026-09-23 02:30, by GPT-5.2

package strategy

import "vmr/internal/core"

// capabilityCondition rejects an endpoint when the request needs a
// capability (per RequestFacts) that the endpoint doesn't declare in
// core.Endpoint.Capabilities. An endpoint that declares no capabilities at
// all is unconstrained (core.Endpoint.HasCapability), so existing configs
// see no behavior change until they opt in by listing capabilities.
type capabilityCondition struct {
	name     string
	required string
	needed   func(core.RequestFacts) bool
}

func (c capabilityCondition) Name() string { return c.name }

func (c capabilityCondition) Eligible(ep *core.Endpoint, facts core.RequestFacts) bool {
	if !c.needed(facts) {
		return true
	}
	return ep.HasCapability(c.required)
}

// conditions is the complete, static Condition set — a fixed compile-time
// slice, not a registry. Config may still accept capability words with no
// Condition behind them ("audio"/"video" — see config's validCapabilities
// and Check()'s warning) until some client sends such input.
//
// thinking is hard like the other two, by decision: a virtual model whose
// requests turn reasoning on must have a candidate that declares it, and
// refusing names the misconfiguration instead of silently stripping the
// client's parameters or sending them where they fail.
var conditions = []Condition{
	capabilityCondition{
		name: "image", required: "image",
		needed: func(f core.RequestFacts) bool { return f.HasImage },
	},
	capabilityCondition{
		name: "tools", required: "tools",
		needed: func(f core.RequestFacts) bool { return f.HasTools },
	},
	capabilityCondition{
		name: "thinking", required: "thinking",
		needed: func(f core.RequestFacts) bool { return f.WantsThinking },
	},
}
