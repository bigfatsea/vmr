// Ver 2026-07-24 12:00, by Sonnet 5

// Package strategy implements scheduling as filter + stable multi-key sort.
// Every scheduling behavior (priority, weight, round_robin, …) is just a
// Dimension; combining them is list concatenation in config, and the router's
// main loop never changes.
package strategy

import (
	"cmp"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"vmr/internal/core"
)

// Dimension is one comparable sort key. Stateful dimensions (round_robin)
// manage their own state inside the instance returned by their factory.
type Dimension interface {
	Name() string
	Compare(a, b *core.Endpoint) int // <0: a first; 0: tie, defer to next dimension
}

var (
	mu        sync.RWMutex
	factories = map[string]func() Dimension{}
)

func Register(name string, f func() Dimension) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[name]; dup {
		panic(fmt.Sprintf("strategy: Register called twice for %q", name))
	}
	factories[name] = f
}

// Build instantiates the dimension chain for one virtual model.
func Build(names []string) ([]Dimension, error) {
	mu.RLock()
	defer mu.RUnlock()
	dims := make([]Dimension, 0, len(names))
	for _, n := range names {
		f, ok := factories[n]
		if !ok {
			return nil, fmt.Errorf("unknown strategy dimension %q", n)
		}
		dims = append(dims, f())
	}
	return dims, nil
}

// Sort orders endpoints by the dimension chain. The sort is stable, so full
// ties keep config-file order.
func Sort(eps []*core.Endpoint, dims []Dimension) {
	sort.SliceStable(eps, func(i, j int) bool {
		for _, d := range dims {
			if c := d.Compare(eps[i], eps[j]); c != 0 {
				return c < 0
			}
		}
		return false
	})
}

func init() {
	Register("priority", func() Dimension { return priority{} })
}

// priority: lower number wins; ties fall through to the next dimension.
type priority struct{}

func (priority) Name() string { return "priority" }
func (priority) Compare(a, b *core.Endpoint) int {
	return cmp.Compare(a.Priority, b.Priority)
}

// Condition tests whether one endpoint may serve a request at all, based on
// facts derived from the request and static properties the endpoint
// declares in config (core.Endpoint.Capabilities). Unlike Dimension
// (endpoint-vs-endpoint ordering, no request access), a Condition is
// request-aware and elimination-only — it never reorders candidates, it
// only says yes or no. See
// docs/VirtualModelRouter_System_Design_v3.md §6.4 for the
// architectural rationale (Dimension.Compare structurally can't see the
// request; this is a parallel, differently-shaped interface, not an
// extension of Dimension).
//
// Registered Conditions all participate unconditionally — unlike Dimension,
// there is no per-model opt-in list, because Condition composition is
// always plain AND with no meaningful ordering between conditions, and an
// endpoint that hasn't declared the relevant capability is unconstrained by
// definition (see core.Endpoint.HasCapability).
type Condition interface {
	Name() string
	Eligible(ep *core.Endpoint, facts core.RequestFacts) bool
}

// conditions is read once per endpoint per request by Eligible — the
// actual per-request hot path — via a lock-free atomic load instead of an
// RWMutex.RLock/RUnlock pair per endpoint (conditions.go's init() calls
// RegisterCondition twice, never dynamically; see
// docs/vmr_architecture_review_opus-5.md §3.5/§4.1). conditionsMu
// serializes the copy-on-write writes themselves — see adapter.registerMu's
// doc comment for why a plain copy-on-write without it would silently lose
// an update under genuinely concurrent writers. factories (the Dimension
// registry above) deliberately keeps its plain mutex on both paths: Build
// is only called once per model at config-load/reload time, not per
// request, so there's no read-side win to chase there.
var (
	conditionsMu sync.Mutex
	conditions   atomic.Pointer[[]Condition]
)

// RegisterCondition adds c to the set consulted by Eligible/RejectedBy.
// Called from init() in the file that defines each condition (see
// conditions.go), the same compile-time registration pattern as Register.
func RegisterCondition(c Condition) {
	conditionsMu.Lock()
	defer conditionsMu.Unlock()
	var next []Condition
	if cur := conditions.Load(); cur != nil {
		next = append(next, *cur...)
	}
	next = append(next, c)
	conditions.Store(&next)
}

// Eligible reports whether ep passes every registered hard Condition for
// this request. Context length is deliberately NOT one of these — it's
// applied separately by WithinContext with its own fallback rule (§1.5),
// because it rests on an estimate rather than a certainty the way
// capability conditions do.
func Eligible(ep *core.Endpoint, facts core.RequestFacts) bool {
	cur := conditions.Load()
	if cur == nil {
		return true
	}
	for _, c := range *cur {
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
	cur := conditions.Load()
	if cur == nil {
		return nil
	}
	var names []string
	for _, c := range *cur {
		if !c.Eligible(ep, facts) {
			names = append(names, c.Name())
		}
	}
	return names
}

// WithinContext reports whether ep's declared context window can plausibly
// fit this request's estimated size. Unset MaxContextTokens (0) is
// unconstrained. This is intentionally not a Condition: §1.5 requires a
// fallback (never let this estimate alone empty a non-empty candidate set)
// that only the caller — which already knows the pre-context-filter
// candidate set — can apply correctly.
func WithinContext(ep *core.Endpoint, facts core.RequestFacts) bool {
	return ep.MaxContextTokens == 0 || facts.EstimatedTokens <= ep.MaxContextTokens
}
