// Ver 2026-08-07, by Opus 5

package quota

import (
	"strings"
	"sync"
	"time"

	"vmr/internal/core"
)

// wildcardToken is core.Limit.Models' reserved "per-model, unrestricted"
// marker — never a literal upstream model name. See core.Limit.Models' doc
// comment for the three Scope shapes this and PerModel/AppliesToModel below
// all key off of.
const wildcardToken = "*"

// IsWildcardModels reports whether models is exactly the reserved
// []string{"*"} shape — "per-model, applies to every model" — as opposed to
// an explicit restricted list. config.validateQuota rejects "*" combined
// with any other entry, so this is the only shape that ever needs checking;
// a caller never has to also handle "*" mixed into a longer slice.
func IsWildcardModels(models []string) bool {
	return len(models) == 1 && models[0] == wildcardToken
}

// PerModel reports whether l uses independent per-model accounting — true
// whenever Models is set at all (either shape), false only when it's
// nil/empty ("shared", one pool for every model). See core.Limit.Models'
// doc comment: presence of Models, not which shape, is what decides this.
func PerModel(l core.Limit) bool {
	return len(l.Models) > 0
}

// AppliesToModel reports whether l's Scope covers model — shared (no
// Models) matches everything, the wildcard matches everything, an explicit
// list matches only its named entries. Shared by router (charge/score
// eligibility) and config (Scope validation), the same "one formula, every
// consumer" reason LimitKey/BaseAmount live here instead of being
// reimplemented at each call site.
func AppliesToModel(l core.Limit, model string) bool {
	if len(l.Models) == 0 || IsWildcardModels(l.Models) {
		return true
	}
	for _, m := range l.Models {
		if m == model {
			return true
		}
	}
	return false
}

// ModelSetsOverlap reports whether two non-empty Scope lists could ever
// match the same real model — the wildcard overlaps with anything (by
// definition it matches every model, including whatever the other list
// names); two explicit lists overlap iff they share at least one entry.
// config.validateQuota uses this to detect two per-model Limits that would
// collide on the same live bucket key for some real model — see
// PerModelPrefix/ExtractModel's doc comments for why a per-model Limit's
// bucket key can't be computed statically at config-validate time the way a
// shared Limit's can, which is exactly why this overlap check exists instead
// of just comparing two precomputed keys for equality.
func ModelSetsOverlap(a, b []string) bool {
	if IsWildcardModels(a) || IsWildcardModels(b) {
		return true
	}
	for _, m := range a {
		for _, n := range b {
			if m == n {
				return true
			}
		}
	}
	return false
}

// LimitKey returns l's Registry storage key for a charge/read against
// model. Two shapes, matching core.Limit.Models' two accounting modes:
//   - shared (Models unset): "metric/every" — model is ignored; every
//     matching endpoint's charges land in the same bucket, exactly P1/P2's
//     original single-bucket-per-Limit shape.
//   - per-model (Models set, wildcard or restricted list): "metric/every
//     #model=<model>" — keyed by the ACTUAL model this charge/read is for,
//     not by l's declared Scope list. This is why a per-model Limit's set
//     of live bucket keys can't be enumerated from config alone (a "*"
//     Limit's membership is open-ended until traffic actually arrives) —
//     see PerModelPrefix/ExtractModel for how a caller walks the Registry's
//     actual keys instead.
//
// Shared by router (charge/score) and any offline reader (vmr report's
// §2.5 table) — one formula, every consumer, the same reason
// BaseAmount/ApplyModelMultiplier live here instead of being reimplemented
// at each call site.
func LimitKey(l core.Limit, model string) string {
	base := string(l.Metric) + "/" + l.EveryText
	if !PerModel(l) {
		return base
	}
	return base + "#model=" + model
}

// PerModelPrefix returns the key prefix every live bucket LimitKey produces
// for l shares, when l is per-model — e.g. "requests/1d#model=". Used
// together with ExtractModel to enumerate which models have actually been
// charged against a per-model Limit (its Scope alone doesn't say — "*"
// covers an open-ended set, and even a restricted list only says which
// models COULD have a bucket, not which ones actually do yet). Callers:
// router.QuotaStatus (walks the live Registry) and vmr report's §2.5 table
// (walks the offline quota.LoadFile snapshot) — both need the same prefix,
// computed the same way, so this isn't reimplemented on either side.
func PerModelPrefix(l core.Limit) string {
	return string(l.Metric) + "/" + l.EveryText + "#model="
}

// ExtractModel reports whether limitKey is a live bucket key belonging to
// l (i.e. starts with l's PerModelPrefix) and, if so, the model it's keyed
// by. ok=false for any key that isn't one of l's own per-model buckets —
// including, deliberately, a shared Limit's plain "metric/every" key (no
// prefix to strip) and another Limit's differently-scoped per-model keys.
func ExtractModel(l core.Limit, limitKey string) (model string, ok bool) {
	prefix := PerModelPrefix(l)
	if !PerModel(l) || !strings.HasPrefix(limitKey, prefix) {
		return "", false
	}
	m := limitKey[len(prefix):]
	if !AppliesToModel(l, m) {
		return "", false
	}
	return m, true
}

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
// unused for its routing decisions, but /status's four-component
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
	// with a vmr-quota.json file written by a earlier build. float64, not
	// int64: once an account has model_multipliers, this is scaled by the
	// same fractional multiplier as Counters (see ApplyModelMultiplier) —
	// see quota.Counters' doc comment for why that scaling must not be
	// rounded to an integer.
	Estimated float64 `json:"estimated"`
	// EstimatedCost  is the $ equivalent for a metric: cost account:
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

// Keys returns every limitKey currently on record for provider — including
// ones from a since-changed config (§9.3's orphan-key caveat applies here
// too: this never cleans up, it just reports what's there). Needed to
// enumerate a per-model Limit's actual live buckets (see PerModelPrefix/
// ExtractModel): a "*" Scope's membership is open-ended, so which models
// have a bucket is a live-Registry fact, not something derivable from
// config alone. A shared Limit never needs this — its one key is already
// computable from the Limit itself. Order is unspecified; callers that need
// determinism (e.g. QuotaStatus's stable /status rendering) sort the
// result themselves.
func (r *Registry) Keys(provider string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	byLimit := r.accounts[provider]
	if len(byLimit) == 0 {
		return nil
	}
	out := make([]string, 0, len(byLimit))
	for k := range byLimit {
		out = append(out, k)
	}
	return out
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
// replaying missed ticks. Returns true when it actually reset, so a read
// path (Used/EstimatedCostFor) can mark the Registry dirty — a period roll
// observed only by a read still needs to reach vmr-quota.json (B8).
func resetIfStaleLocked(b *bucket, periodStart time.Time) bool {
	ps := periodStart.Unix()
	if b.PeriodStart != ps {
		*b = bucket{PeriodStart: ps}
		return true
	}
	return false
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
	if resetIfStaleLocked(b, periodStart) {
		r.dirty = true
	}
	return b.C, b.Estimated
}

// AddEstimatedCost  bumps provider's limitKey bucket's running
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
// since only /status's cost-metric rendering needs it.
func (r *Registry) EstimatedCostFor(provider, limitKey string, periodStart time.Time) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.getLocked(provider, limitKey)
	if resetIfStaleLocked(b, periodStart) {
		r.dirty = true
	}
	return b.EstimatedCost
}
