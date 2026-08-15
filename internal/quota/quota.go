// Ver 2026-08-07, by Opus 5

package quota

import (
	"sync"
	"time"
)

// Counters is one Limit's accumulated consumption, stored by raw component —
// never pre-weighted or pre-priced. Charge/Used deal in these units directly;
// base(metric) is applied by the caller via weight.go's BaseAmount. Per the
// design doc's Storage Granularity decision: folding a weighting policy into
// the stored value would force a data migration every time that policy
// changes, while raw components let token_weights/model_multipliers/cost
// pricing all read the same history under a different formula.
//
// Cost is the one deliberate exception. A metric: cost charge is computed
// against a price table that changes over time, so the $ amount must be frozen
// at charge time — re-deriving it later from raw token counts at whatever
// price happens to be configured then would silently rewrite history on every
// pricing edit (see the design doc's "9.2 运行态"). Fresh/CacheRead/
// CacheWrite/Out are still recorded alongside Cost on a cost-metric account:
// unused for its routing decisions, but /admin/status's four-component
// breakdown is useful regardless of metric.
//
// These are float64, not int64, because an account with model_multipliers
// folds a possibly-fractional multiplier into them at charge time (see
// ApplyModelMultiplier) and that scaling must land exactly — rounding each
// charge to an integer produces a systematic overcharge bias of
// unpredictable magnitude, unrelated to what the provider actually bills.
// Without model_multipliers these always hold integer-valued floats.
type Counters struct {
	Fresh      float64 `json:"fresh"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Out        float64 `json:"out"`
	Requests   float64 `json:"requests"`
	Cost       float64 `json:"cost,omitempty"`
}

// Add returns the element-wise sum of c and d.
func (c Counters) Add(d Counters) Counters {
	return Counters{
		Fresh:      c.Fresh + d.Fresh,
		CacheRead:  c.CacheRead + d.CacheRead,
		CacheWrite: c.CacheWrite + d.CacheWrite,
		Out:        c.Out + d.Out,
		Requests:   c.Requests + d.Requests,
		Cost:       c.Cost + d.Cost,
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
	// Estimated is this period's total contributed by degraded (non-usage-
	// sniffed) TOKEN estimates — requests/tokens accounts only. JSON key
	// stays "estimated" (not "estimated_tokens") for on-disk compatibility
	// with a vmr-quota.json file written by a pre-P2.2 build. float64, not
	// int64: once an account has model_multipliers, this is scaled by the
	// same fractional multiplier as Counters (see ApplyModelMultiplier) —
	// see quota.Counters' doc comment for why that scaling must not be
	// rounded to an integer.
	Estimated float64 `json:"estimated"`
	// EstimatedCost (P2.2) is the $ equivalent for a metric: cost account:
	// this period's total Cost that came from a degraded token estimate
	// (via the resolved rate) rather than sniffed usage — same "how much to
	// trust this number" signal Estimated gives requests/tokens accounts,
	// just in money instead of tokens. Always 0 for a non-cost account.
	EstimatedCost float64 `json:"estimated_cost,omitempty"`
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
func (r *Registry) Charge(provider, limitKey string, periodStart time.Time, d Counters, estimated float64) {
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
func (r *Registry) Used(provider, limitKey string, periodStart time.Time) (Counters, float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.getLocked(provider, limitKey)
	resetIfStaleLocked(b, periodStart)
	return b.C, b.Estimated
}

// AddEstimatedCost (P2.2) bumps provider's limitKey bucket's running
// EstimatedCost — the metric: cost analogue of Charge's estimated float64
// parameter, kept as a separate method rather than overloading Charge's
// signature: Counters already has a Cost field (Charge/Add sum it exactly
// like every other component, so a cost charge's $ amount is recorded via
// an ordinary Charge call with d.Cost set), but the ESTIMATE signal for a
// cost-metric account is denominated in money, not tokens, and doesn't fit
// Charge's existing token-denominated estimated parameter. Call this
// alongside Charge, not instead of it, when a charge came from a degraded
// (non-usage-sniffed) token estimate priced through the resolved rate.
func (r *Registry) AddEstimatedCost(provider, limitKey string, periodStart time.Time, amount float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.getLocked(provider, limitKey)
	resetIfStaleLocked(b, periodStart)
	b.EstimatedCost += amount
	r.dirty = true
}

// EstimatedCostFor returns provider's limitKey bucket's running
// EstimatedCost as of periodStart (lazily resetting first, same as Used) —
// a small dedicated accessor rather than growing Used's return shape,
// since only /admin/status's cost-metric rendering needs it.
func (r *Registry) EstimatedCostFor(provider, limitKey string, periodStart time.Time) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.getLocked(provider, limitKey)
	resetIfStaleLocked(b, periodStart)
	return b.EstimatedCost
}
