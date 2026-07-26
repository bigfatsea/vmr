// Ver 2026-07-24 10:00, by Sonnet 5

// Package sticky implements the Sticky Model affinity registry: an
// in-memory map from a session fingerprint to the endpoint that most
// recently, successfully served it, used to keep a multi-turn agent
// conversation on the same upstream endpoint so the provider's prompt
// cache stays warm. See
// docs/VirtualModelRouter_System_Design_v3.md §6.5 for the full
// design — in particular why this package deliberately knows nothing about
// config, endpoints, or TTL values: validity is a per-endpoint property
// (internal/core.Endpoint.StickyTTL) the caller resolves, not something
// this registry can determine on its own.
package sticky

import (
	"sync"
	"time"

	"vmr/internal/core"
)

// BackstopTTL bounds memory growth independent of any per-endpoint
// validity TTL, which can range from minutes (Anthropic/OpenAI/MiniMax) to
// days (DeepSeek's disk cache) — see design doc §2.4. It only governs when
// a stale entry is dropped from the map; it has no bearing on whether a
// still-present entry is "valid" for a routing decision, which Peek leaves
// entirely to the caller.
//
// This is core.StickyBackstopTTL, kept here as an alias for callers that
// already spell it sticky.BackstopTTL: the canonical value lives in
// internal/core so internal/config can validate a configured sticky_ttl
// against it without importing this package just to read one constant (see
// docs/vmr_architecture_review_opus-5.md §3.2) — such a setting would look
// accepted but silently stop working the moment an entry goes quiet for
// longer than BackstopTTL, since Set's sweep would have already dropped it
// from the map — a routing decision that quietly degrades from "sticky"
// to "not sticky" with no error is exactly the kind of surprise vmr's
// fail-fast config philosophy exists to catch before it ships.
const BackstopTTL = core.StickyBackstopTTL

// sweepInterval throttles the opportunistic sweep so Set doesn't walk the
// whole map on every call — same event-triggered-and-throttled shape as
// the image downscale cache housekeeping (design doc §7.1), not an
// independent ticker goroutine.
const sweepInterval = time.Hour

type entry struct {
	endpointKey string
	lastUsed    time.Time
}

// Registry is safe for concurrent use.
type Registry struct {
	mu        sync.Mutex
	entries   map[string]entry
	lastSweep time.Time
}

func New() *Registry {
	return &Registry{entries: make(map[string]entry)}
}

// Peek returns the endpoint key and last-used time recorded for key, if
// any. It does not apply a TTL itself — the caller knows which endpoint
// the returned key refers to and resolves validity against that
// endpoint's own StickyTTL (see router.Serve's Sticky Model integration).
func (r *Registry) Peek(key string) (endpointKey string, lastUsed time.Time, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, found := r.entries[key]
	if !found {
		return "", time.Time{}, false
	}
	return e.endpointKey, e.lastUsed, true
}

// Set records that key most recently, successfully resolved to
// endpointKey, refreshing its last-used time. Call this after every
// successful completion — including a failover success, not only the
// first time a session is seen — so the pointer follows wherever the
// conversation's cache is actually warm (design doc §2.5, point 3).
func (r *Registry) Set(key, endpointKey string) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[key] = entry{endpointKey: endpointKey, lastUsed: now}
	if now.Sub(r.lastSweep) < sweepInterval {
		return
	}
	r.lastSweep = now
	for k, e := range r.entries {
		if now.Sub(e.lastUsed) > BackstopTTL {
			delete(r.entries, k)
		}
	}
}

// Len reports the current entry count — for /admin/status or tests, not on
// any request path.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
