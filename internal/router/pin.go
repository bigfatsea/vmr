// Ver 2026-08-23, by Gemini

// Pinned routing: force one request onto a specific upstream provider and/or
// target model, within the requested virtual model's own endpoint set.
//
// Exists for `vmr smoke` (and manual curl debugging): probing a single
// backend through the live router — quota metering, audit recording, health
// state and failover semantics all still apply — instead of only through
// the priority/health/condition lens normal routing applies. It is the
// "smoke the exact backend you mean" escape hatch that /status's per-model
// quota rows are read from: per-model quota buckets only exist once traffic
// has actually charged them (see internal/quota's lazy bucket allocation),
// and pinning is how that traffic gets aimed without disturbing the routing
// config.
//
// The pin is deliberately a NARROWING lens, never an opening one: it only
// filters the endpoints already configured under the requested virtual
// model (post health/condition/context filtering). It cannot reach an
// endpoint the model doesn't declare, so it adds no routing surface a
// config author didn't already create. A pinned request that matches
// nothing fails exactly like any other no-available-endpoint request, with
// the pin named in the error so the operator sees why.
//
// Header names, set on the client request (consumed here, stripped from the
// upstream forward by FilterClientHeaders' blocklist):
//
//	X-VMR-Provider:     pin to one provider name (exact match)
//	X-VMR-Target-Model: pin to one upstream target model (exact match)
package router

import (
	"net/http"
	"strings"

	"vmr/internal/core"
)

// Pin headers. Blocklisted in clientheaders.go so they never reach an
// upstream; read here from the raw request header (r.Header), not
// CanonicalRequest.Header, precisely because the latter is the
// already-filtered copy that would have them stripped.
const (
	PinProviderHeader = "X-VMR-Provider"
	PinModelHeader    = "X-VMR-Target-Model"
)

// pin is the parsed form of the pin headers — empty field = no constraint
// on that axis.
type pin struct {
	provider string
	model    string
}

// parsePin reads the pin headers off the raw request header set. Both
// values are whitespace-trimmed; an empty value on one axis is the same as
// omitting it.
func parsePin(h http.Header) pin {
	if h == nil {
		return pin{}
	}
	return pin{
		provider: strings.TrimSpace(h.Get(PinProviderHeader)),
		model:    strings.TrimSpace(h.Get(PinModelHeader)),
	}
}

// active reports whether any axis is constrained.
func (p pin) active() bool { return p.provider != "" || p.model != "" }

// String renders the pin for X-VMR-Route-Reason and error messages.
func (p pin) String() string {
	var parts []string
	if p.provider != "" {
		parts = append(parts, "provider="+p.provider)
	}
	if p.model != "" {
		parts = append(parts, "model="+p.model)
	}
	return strings.Join(parts, ",")
}

// applyPin narrows candidates to the ones matching p. It returns the
// filtered slice (possibly empty) and whether the pin was active. A pin
// with no axis set returns (nil, false) — a pure no-op the caller can skip.
// Order is preserved so the caller's own Sort still decides among the
// survivors.
func applyPin(candidates []*core.Endpoint, p pin) ([]*core.Endpoint, bool) {
	if !p.active() {
		return nil, false
	}
	out := make([]*core.Endpoint, 0, len(candidates))
	for _, ep := range candidates {
		if p.provider != "" && ep.Provider != p.provider {
			continue
		}
		if p.model != "" && ep.Model != p.model {
			continue
		}
		out = append(out, ep)
	}
	return out, true
}

// applyPinToCandidates is the Serve hot-path wrapper: read the pin off the
// raw request header (the blocklist strips these from creq.Header), narrow
// the candidates, and return the narrowed slice plus the pin's String form
// for X-VMR-Route-Reason (empty when no pin was present).
func applyPinToCandidates(candidates []*core.Endpoint, r *http.Request) ([]*core.Endpoint, string) {
	p := parsePin(r.Header)
	if !p.active() {
		return candidates, ""
	}
	out, _ := applyPin(candidates, p)
	return out, p.String()
}
