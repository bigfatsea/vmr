// Ver 2026-08-07, by Opus 5

package quota

import (
	"sync"
	"time"
)

// Counters is one Limit's accumulated consumption, stored by raw component
// — never pre-weighted or pre-priced. Charge/Used deal in these units
// directly; base(metric) (requests count, or an equal-weighted token sum in
// P1) is applied by the caller (internal/router/quota.go), not here. See
// the design doc's Storage Granularity decision: folding a weighting
// policy into the stored value would force a data migration every time that
// policy changes; storing raw components means P2's token_weights/
// model_multipliers/cost pricing read the exact same history under a
// different formula.
type Counters struct {
	Fresh      int64 `json:"fresh"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Out        int64 `json:"out"`
	Requests   int64 `json:"requests"`
}

// Add returns the element-wise sum of c and d.
func (c Counters) Add(d Counters) Counters {
	return Counters{
		Fresh:      c.Fresh + d.Fresh,
		CacheRead:  c.CacheRead + d.CacheRead,
		CacheWrite: c.CacheWrite + d.CacheWrite,
		Out:        c.Out + d.Out,
		Requests:   c.Requests + d.Requests,
	}
}

// bucket is one (provider, limitKey) account's live state: what period it
// currently believes it's in, and what's accumulated since that period
// started. Reset is lazy — see Registry.charge/used below — so there is no
// ticker, no scheduled job, and no "process wasn't running at the reset
// instant" gap: the next Charge or Used call after a period boundary simply
// notices periodStart has moved and zeroes the bucket in place.
type bucket struct {
	PeriodStart int64    `json:"period_start"` // Unix seconds
	C           Counters `json:"counters"`
	Estimated   int64    `json:"estimated"` // this period's total contributed by degraded (non-usage-sniffed) token estimates; same unit as C's token fields
}

// Registry holds every provider's live quota consumption. Shaped like
// health.Registry (see internal/health): it lives on the Router, not inside
// a Snapshot, so counts survive a hot config reload — only the *policy*
// (amount, window, metric) is re-read from the fresh Snapshot on every use;
// the *fact* of what's been consumed is Registry's alone to own.
//
// Keyed by provider name — deliberately NOT including the API key hash the
// way core.Endpoint.HealthKey() does. HealthKey hashes the key so that
// rotating it resets health (a fresh key deserves a fresh trust
// assessment — the failure history belonged to the old credential). Quota
// consumption is the opposite: it belongs to the account, and an account
// doesn't get a bigger budget just because its operator rotated the key
// (e.g. after a leak). Keying quota by API key hash would silently zero the
// current period's count on every rotation and let the account overspend.
// If a future change ever "harmonizes" this with HealthKey's shape, that
// would reintroduce exactly this bug — this comment is the guard against
// that.
type Registry struct {
	mu       sync.Mutex
	accounts map[string]map[string]*bucket // provider name -> limitKey -> bucket
	path     string
	dirty    bool
}

// NewRegistry creates a Registry that persists to path (see store.go).
// Empty path is valid (used by tests and any caller that never intends to
// call Load/Flush) — Charge/Used still work purely in memory.
func NewRegistry(path string) *Registry {
	return &Registry{accounts: map[string]map[string]*bucket{}, path: path}
}

func (r *Registry) getLocked(provider, limitKey string) *bucket {
	byLimit, ok := r.accounts[provider]
	if !ok {
		byLimit = map[string]*bucket{}
		r.accounts[provider] = byLimit
	}
	b, ok := byLimit[limitKey]
	if !ok {
		b = &bucket{}
		byLimit[limitKey] = b
	}
	return b
}

// resetIfStaleLocked zeroes b in place when periodStart has moved past what
// b currently believes — the lazy-reset mechanism: no goroutine, no missed-
// tick risk, and a process restart self-corrects the moment the first
// Charge/Used after the gap runs, simply by comparing timestamps instead of
// replaying missed ticks.
func resetIfStaleLocked(b *bucket, periodStart time.Time) {
	ps := periodStart.Unix()
	if b.PeriodStart != ps {
		*b = bucket{PeriodStart: ps}
	}
}

// Charge adds d (already the caller's raw per-request observation — see
// Counters' doc comment) to provider's limitKey bucket, lazily resetting
// first if periodStart has moved past what was previously recorded.
// estimated is added to the bucket's running Estimated total when this
// charge came from degraded (non-usage-sniffed) token estimation; 0 for an
// exact (usage-sniffed or requests-metric) charge.
func (r *Registry) Charge(provider, limitKey string, periodStart time.Time, d Counters, estimated int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.getLocked(provider, limitKey)
	resetIfStaleLocked(b, periodStart)
	b.C = b.C.Add(d)
	b.Estimated += estimated
	r.dirty = true
}

// Used returns provider's limitKey bucket as of periodStart, lazily
// resetting first if the stored period has gone stale — so a read-only
// caller (the router's per-request scoring path) sees a correctly-zeroed
// bucket immediately after a period boundary, without waiting for the next
// Charge to trigger the reset. This DOES mutate Registry state (the reset),
// which is why it takes the same lock Charge does rather than being a
// pure read.
func (r *Registry) Used(provider, limitKey string, periodStart time.Time) (Counters, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.getLocked(provider, limitKey)
	resetIfStaleLocked(b, periodStart)
	return b.C, b.Estimated
}
