// Ver 2026-08-02, by Sonnet 5

// Package health implements the failure-driven health state machine:
// cooldown with exponential backoff and single-flight half-open recovery
// (Acquire/ReportSuccess/ReportFailure/ReportNeutral). It knows nothing
// about *how* a half-open endpoint gets re-verified — that policy (a
// decoupled background probe, see internal/router/probe.go) lives entirely
// in internal/router.
package health

import (
	"sync"
	"time"

	"vmr/internal/core"
)

const (
	transientBase = 2 * time.Second
	transientCap  = 5 * time.Minute
	longBase      = 10 * time.Minute
	longCap       = time.Hour
)

type state struct {
	fails         int
	cooldownUntil time.Time
	lastClass     core.ErrorClass
	probing       bool
}

// Registry keeps health state keyed by Endpoint.HealthKey(). It lives outside
// the config snapshot, so cooldowns survive hot reloads.
type Registry struct {
	mu sync.Mutex
	m  map[string]*state
}

func New() *Registry { return &Registry{m: map[string]*state{}} }

func (r *Registry) get(key string) *state {
	s, ok := r.m[key]
	if !ok {
		s = &state{}
		r.m[key] = s
	}
	return s
}

// Available reports whether the endpoint would be tried now, without side
// effects — it never claims the half-open probe slot Acquire does. This is
// the only side-effect-free way to inspect routing eligibility, which is
// exactly why it has no caller in this package's own routing loop
// (that loop needs Acquire's claim) yet stays load-bearing: tests in this
// package and internal/router assert endpoint state via this method
// precisely because calling Acquire to check would itself change the
// state being asserted. Do not read "no production caller" as "delete" —
// see docs/KNOWN_ISSUES_sonnet-5.md §3 item 35.
func (r *Registry) Available(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[key]
	if !ok {
		return true
	}
	if now.Before(s.cooldownUntil) {
		return false
	}
	// Half-open with a probe already in flight: skip.
	return !(s.fails > 0 && s.probing)
}

// Acquire claims the right to send a real request. Healthy endpoints always
// pass. A half-open endpoint (cooldown expired after failures) admits exactly
// one caller — the probe — until that probe reports success or failure.
func (r *Registry) Acquire(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(key)
	if now.Before(s.cooldownUntil) {
		return false
	}
	if s.fails > 0 {
		if s.probing {
			return false
		}
		s.probing = true
	}
	return true
}

// ReportNeutral releases a half-open probe slot without touching failure
// counts or cooldown. Used for request-specific outcomes — content-policy
// flags, client cancellation, ErrClient responses — that say nothing about
// the endpoint's health: the probe neither confirms recovery nor deepens
// the backoff. Every acquired probe must end in exactly one of Success /
// Failure / Neutral, or the endpoint stays locked out forever.
func (r *Registry) ReportNeutral(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[key]; ok {
		s.probing = false
	}
}

func (r *Registry) ReportSuccess(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(key)
	s.fails = 0
	s.cooldownUntil = time.Time{}
	s.probing = false
}

// ReportFailure records a failure, deepens the backoff and returns the cooldown applied.
func (r *Registry) ReportFailure(key string, class core.ErrorClass, retryAfter time.Duration, now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(key)
	s.probing = false
	s.fails++
	s.lastClass = class

	var d time.Duration
	switch class {
	case core.ErrAuth, core.ErrEndpoint:
		d = backoff(longBase, longCap, s.fails)
	default:
		// Retry-After is honored beyond 429: OpenRouter sends it on 503 too.
		// Capped at longCap: it is upstream-controlled input, and a bogus
		// huge value must not lock an endpoint out until process restart —
		// the same "recovery promptness over precision" call as ErrEndpoint.
		if retryAfter > 0 {
			d = min(retryAfter, longCap)
		} else {
			d = backoff(transientBase, transientCap, s.fails)
		}
	}
	s.cooldownUntil = now.Add(d)
	return d
}

func backoff(base, cap time.Duration, fails int) time.Duration {
	d := base
	for i := 1; i < fails; i++ {
		d *= 2
		if d >= cap {
			return cap
		}
	}
	return d
}

// Classify answers the router's per-candidate health-filter question in one
// locked read instead of two: available reports whether the endpoint should
// be handed real traffic this round; needsProbe reports whether the caller
// just claimed the single-flight half-open probe slot and must launch a
// background probe (see internal/router/probe.go). Replaces the router's
// former Status(key,now).Fails>0 + Acquire(key,now) / Available(key,now)
// two-call sequence, which separately locked and unlocked the registry
// twice per endpoint per request — and, on the Fails>0 path, built a full
// Status struct (including a lastClass.String() call) just to read one
// field — plus left a window between the two locked reads where another
// goroutine's ReportSuccess/ReportFailure could change the state being
// classified out from under it. Status/Acquire/Available stay as their own
// methods: Status backs /status's full health view, and Acquire is
// still used standalone by the router's per-attempt loop once a candidate
// has already passed this filter.
func (r *Registry) Classify(key string, now time.Time) (available, needsProbe bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[key]
	if !ok || s.fails == 0 {
		return true, false
	}
	// cooldownUntil is only ever set alongside fails>0 (ReportSuccess always
	// clears both together), so a fails>0 state with cooldownUntil in the
	// future is still cooling down — not available, no probe to launch yet.
	if now.Before(s.cooldownUntil) {
		return false, false
	}
	// Half-open: cooldown expired, at least one failure on record. Exactly
	// one caller gets to claim the probe slot, same rule Acquire enforces.
	if s.probing {
		return false, false
	}
	s.probing = true
	return false, true
}

// Status is one endpoint's health as exposed by /status.
type Status struct {
	Fails         int       `json:"consecutive_failures"`
	CooldownUntil time.Time `json:"cooldown_until,omitzero"`
	LastError     string    `json:"last_error,omitempty"`
	// Available is narrower than its name suggests: it only reports
	// "cooldown has expired", the same test Registry.Available makes. A
	// half-open endpoint (cooldown expired, Fails>0, not yet re-verified)
	// reports Available==true here even though Classify — what the router
	// actually consults this request round — returns available=false for
	// it (only a background probe gets dispatched, never real traffic,
	// until that probe reports success). Kept as-is for backward
	// compatibility with existing consumers of this field; Serving below is
	// the field that answers "would real traffic route here right now".
	Available bool `json:"available"`
	// Probing is true while a single-flight probe holds this endpoint's
	// slot — a background probe (see internal/router/probe.go) is
	// currently deciding whether the endpoint has recovered. Purely observational:
	// nothing reads this field to make a routing decision, it exists so
	// `vmr status` and /status can show *why* a half-open endpoint
	// (Available==true, Fails>0) isn't being tried right this moment.
	Probing bool `json:"probing,omitempty"`
	// Serving mirrors Classify's available return value: true only when
	// the router's health filter would route real (non-probe) traffic to
	// this endpoint this round. Unlike Available, this is false for the
	// entire half-open window (Fails>0), whether or not a probe currently
	// holds the slot — the field to alert on, not Available.
	Serving bool `json:"serving"`
}

func (r *Registry) Status(key string, now time.Time) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[key]
	if !ok {
		return Status{Available: true, Serving: true}
	}
	st := Status{Fails: s.fails, Available: !now.Before(s.cooldownUntil), Probing: s.probing, Serving: s.fails == 0}
	if !s.cooldownUntil.IsZero() && now.Before(s.cooldownUntil) {
		st.CooldownUntil = s.cooldownUntil
	}
	if s.fails > 0 {
		st.LastError = s.lastClass.String()
	}
	return st
}
