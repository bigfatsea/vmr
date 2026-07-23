// Ver 2026-07-23 10:00, by Sonnet 5

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

func init() {
	RegisterCondition(capabilityCondition{
		name: "image", required: "image",
		needed: func(f core.RequestFacts) bool { return f.HasImage },
	})
	RegisterCondition(capabilityCondition{
		name: "tools", required: "tools",
		needed: func(f core.RequestFacts) bool { return f.HasTools },
	})
	// "thinking" is deliberately not registered yet: the request-side
	// signal (WantsThinking) has no detection logic behind it until the
	// Anthropic/OpenAI/MiniMax protocol shapes are confirmed (see design
	// doc §1.3④) — registering the condition now would be a no-op that
	// looks implemented but never fires, which is worse than leaving it out.
}
