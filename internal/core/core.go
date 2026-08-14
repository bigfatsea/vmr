// Ver 2026-07-25, by Sonnet 5
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// WriteJSON writes v as the JSON response body with the given status.
// Shared by router and server so every JSON response (success or error)
// goes through one encoding path.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteError emits an error body that both OpenAI clients (error.message)
// and Anthropic clients (type:"error" envelope) can parse. Shared by router
// and server so a format change only has to be made once.
func WriteError(w http.ResponseWriter, status int, errType, msg string) {
	WriteJSON(w, status, map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": msg},
	})
}

// CanonicalRequest is the routing view of a chat request. Both supported
// ingress protocols (OpenAI chat completions, Anthropic messages) carry
// top-level "model" and "stream" fields; only those are parsed. Raw keeps
// every other byte untouched so unknown upstream parameters pass through
// unchanged — VMR never converts between protocols.
type CanonicalRequest struct {
	Model  string
	Stream bool
	Raw    json.RawMessage
	Header http.Header // client headers after the server's blocklist filter (credentials removed)
	Facts  RequestFacts
}

// RequestFacts are cheap, request-derived signals computed once per request
// (never per candidate endpoint) and consulted by strategy.Condition
// implementations — see docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing section.
// WantsThinking/HasAudio/HasVideo are typed placeholders only: no
// detection logic populates them yet (protocol shapes for audio/video input
// and MiniMax's thinking parameter aren't confirmed), so they are always
// false until a later change adds the corresponding detection.
//
// JSON tags exist so this same value (computed once in server.go) can be
// persisted verbatim into audit.Record.Facts — vmr's own pre-routing
// analysis, sitting alongside the raw request rather than recomputed later
// from it (see audit.Record.Facts's doc comment).
type RequestFacts struct {
	HasImage        bool  `json:"has_image,omitempty"`
	HasAudio        bool  `json:"has_audio,omitempty"`
	HasVideo        bool  `json:"has_video,omitempty"`
	HasTools        bool  `json:"has_tools,omitempty"`
	WantsThinking   bool  `json:"wants_thinking,omitempty"`
	EstimatedTokens int64 `json:"estimated_tokens"`
}

// ErrorClass drives failover and cooldown decisions.
type ErrorClass int

const (
	ErrClient    ErrorClass = iota // request itself is bad: return to client, no failover
	ErrAuth                        // 401/403: long cooldown, switch
	ErrRateLimit                   // 429: honor Retry-After, switch
	ErrEndpoint                    // endpoint persistently unusable (quota/402, unknown model/404): long cooldown, switch
	ErrTransient                   // 5xx/408/timeouts/network: short cooldown, switch
	ErrContent                     // content policy/moderation flag: request-specific, switch WITHOUT health penalty
	// ErrContextLimit is a conversation-history-exceeds-context-window
	// rejection: a static property of THIS endpoint's model, not evidence
	// the endpoint is unhealthy — another candidate may have a larger
	// window. Switch WITHOUT health penalty, same treatment as ErrContent
	// (see internal/adapter/classify.go's contextLimitHint for the word-list
	// sniffing, and its own doc comment for why this is deliberately NOT
	// used for a request's own max_tokens/output-length parameter being
	// too large — switching endpoints can't fix a client-supplied number).
	ErrContextLimit

	// The four below never reach Health.ReportFailure/ReportNeutral — they
	// occur before a response classification is possible (build/network) or
	// after the health outcome for this attempt was already reported
	// (canceled, truncated). They exist purely so audit.Attempt.ErrorClass
	// has a single typed category for every Attempt.Error shape router.go
	// produces, instead of the report package inferring one by string-prefix
	// parsing. Router health/cooldown logic keeps classifying these as
	// ErrTransient (build/network) or skipping ReportFailure entirely
	// (canceled/truncated) exactly as before — adding these values changes
	// no failover behavior.
	ErrBuild     // adapter failed to build the outbound request
	ErrNetwork   // dial/write/read failure before any response arrived
	ErrCanceled  // client disconnected while this attempt was in flight
	ErrTruncated // response was already committed to the client when the upstream connection died mid-stream
)

func (c ErrorClass) String() string {
	switch c {
	case ErrClient:
		return "client"
	case ErrAuth:
		return "auth"
	case ErrRateLimit:
		return "rate_limit"
	case ErrEndpoint:
		return "endpoint"
	case ErrContent:
		return "content"
	case ErrContextLimit:
		return "context_limit"
	case ErrBuild:
		return "build"
	case ErrNetwork:
		return "network"
	case ErrCanceled:
		return "canceled"
	case ErrTruncated:
		return "truncated"
	case ErrTransient:
		return "transient"
	default:
		// Every declared ErrorClass has an explicit case above; this is only
		// reached by a value outside the declared set, which never happens
		// in practice (ErrorClass is only ever constructed from the consts
		// above). Falls back to "transient" rather than a distinct label so
		// report's error_classes bucketing degrades safely instead of
		// growing an unbounded set of unknown keys.
		return "transient"
	}
}

// Endpoint is the smallest schedulable unit: provider × real model × scheduling attrs.
// FullURL is the complete upstream URL (base_url + protocol path, with
// overlap eliminated) pre-computed once at initialization so the adapter's
// BuildRequest never needs to construct or normalize a URL per request.
type Endpoint struct {
	Provider    string // provider name as written in config
	AdapterType string
	BaseURL     string // as written in config (for diagnostics: DNS/TLS checks, display)
	FullURL     string // complete upstream URL, pre-computed at init via adapter.ResolveURL
	APIKey      string
	Model       string
	Priority    int
	RoleMap     map[string]string // per-provider role remapping (e.g. {"developer":"system"}); nil = no remapping

	// Capabilities is a free-form allowlist (e.g. "image", "tools",
	// "thinking") — the *effective* set actually used for condition
	// routing, already resolved at BuildSnapshot time as the union of the
	// endpoint's virtual model's base config.VirtualModel.Capabilities and
	// this endpoint's own config.EndpointGroup.Capabilities. Empty/nil means
	// unconstrained — every capability is assumed supported — so existing
	// configs that don't set either field see no behavior change. Once
	// non-empty it is exhaustive: a capability the endpoint actually
	// supports but omits here is treated as unsupported.
	Capabilities []string
	// ExtraCapabilities is this endpoint's own declared
	// config.EndpointGroup.Capabilities, *before* merging with the model's
	// base — display-only (vmr check), so a human can see exactly what this
	// endpoint adds on top of its group's shared floor instead of the
	// already-merged set in Capabilities above.
	ExtraCapabilities []string
	// MaxContextTokens is the effective, already-resolved context-window
	// ceiling in tokens (this endpoint's own override if set, else its
	// virtual model's base); 0 means unconstrained.
	MaxContextTokens int64
	// OwnMaxContextTokens is this endpoint's own declared
	// config.EndpointGroup.MaxContextTokens override, 0 if it inherits its
	// virtual model's base value as-is — display-only (vmr check);
	// MaxContextTokens above always holds the resolved value routing uses.
	OwnMaxContextTokens int64
	// StickyTTL is how long a sticky preference for this endpoint stays
	// valid, resolved at BuildSnapshot time from the endpoint's own
	// config.EndpointConfig.StickyTTL override or, absent that, the global
	// config.Config.StickyTTL default.
	StickyTTL time.Duration

	// Quota is this endpoint's provider-level quota spec, resolved at
	// BuildSnapshot time — the SAME pointer is shared by every
	// core.Endpoint expanded from the same config.Provider (quota is an
	// account property, not a per-model one), so reading it costs one field
	// dereference instead of a linear scan of Cfg.Providers. nil = the
	// provider has no quota configured (unmetered account).
	Quota *QuotaSpec

	// PricingRate is this specific provider+model's resolved pricing
	// (P2.2), resolved at BuildSnapshot time — unlike Quota, NOT shared
	// across every Endpoint expanded from the same provider, because price
	// is inherently model-scoped (see PricingSpec's doc comment). nil = no
	// pricing resolved for this provider+model (no providers[].pricing
	// configured and no standard/supplement table match) — a metric: cost
	// Limit on such a provider is rejected at config-validate time before
	// this is ever read on the request path.
	PricingRate *PricingSpec

	// healthKey/name cache HealthKey()/Name()'s result. Both are pure
	// functions of the exported fields above and every Endpoint is
	// immutable once constructed, so BuildSnapshot computes them exactly
	// once (Freeze) instead of every one of the ~7 hot-path call sites
	// (health filtering, Acquire, sticky lookups, logging) recomputing —
	// HealthKey in particular re-hashes APIKey with SHA-256 every call.
	// Endpoints built directly (outside BuildSnapshot, as plenty of tests do
	// via `&core.Endpoint{...}` literals) simply never call Freeze and fall
	// back to computing on demand every time: slower, but correct, and that
	// fallback path is never on the request hot path.
	healthKey string
	name      string
}

// Freeze precomputes HealthKey()/Name() so every later call is a plain
// field read. Called once per Endpoint, right after construction, by
// router.BuildSnapshot — before the Endpoint is ever reachable from a
// concurrently-read Snapshot, so no additional synchronization is needed
// beyond the atomic pointer swap that already publishes the Snapshot.
// Idempotent; safe to call more than once (it never will be, but nothing
// breaks if it is).
func (e *Endpoint) Freeze() {
	e.healthKey = e.computeHealthKey()
	e.name = e.computeName()
}

// StickyBackstopTTL bounds internal/sticky's Registry memory growth,
// independent of any per-endpoint validity TTL (Endpoint.StickyTTL above),
// which can range from minutes to days — see the design doc's Sticky Model
// section. Lives here
// rather than in internal/sticky itself so internal/config can validate a
// configured sticky_ttl against it without importing the sticky package
// just to read one constant. internal/sticky.BackstopTTL is this same
// value, kept as an alias for callers that already spell it that way.
const StickyBackstopTTL = 24 * time.Hour

// HasCapability reports whether e declares support for name. An endpoint
// that declares no capabilities at all is unconstrained (see Capabilities).
func (e *Endpoint) HasCapability(name string) bool {
	if len(e.Capabilities) == 0 {
		return true
	}
	for _, c := range e.Capabilities {
		if c == name {
			return true
		}
	}
	return false
}

// HealthKey identifies this endpoint in the health registry. It is stable
// across config reloads so cooldown state survives a hot reload. AdapterType
// is part of the key because a provider name can now be reused across
// protocol groups (e.g. the same "openrouter" name under both providers.openai
// and providers.anthropic) — without it, two genuinely different endpoints
// sharing a name, API key, and upstream model string would collide.
func (e *Endpoint) HealthKey() string {
	if e.healthKey != "" {
		return e.healthKey
	}
	return e.computeHealthKey()
}

func (e *Endpoint) computeHealthKey() string {
	sum := sha256.Sum256([]byte(e.APIKey))
	return e.AdapterType + "/" + e.Provider + "/" + e.Model + "/" + hex.EncodeToString(sum[:4])
}

// Name is the human-readable, "/"-joined endpoint identity used by
// internal/server/admin.go's /admin/status and live log lines — a DIFFERENT
// format from endpointlabel.go's EndpointLabel (":"-joined), which is the
// audit-log's on-disk contract instead. The two coexist on purpose: this one
// is free to change shape without touching a single historical audit
// record; EndpointLabel's format cannot, since old records already contain
// it. Do not unify them.
func (e *Endpoint) Name() string {
	if e.name != "" {
		return e.name
	}
	return e.computeName()
}

func (e *Endpoint) computeName() string {
	return e.AdapterType + "/" + e.Provider + "/" + e.Model
}

// QuotaMetric is the counting unit a Limit is measured in — see
// docs/VirtualModelRouter_Design_v4_Quota.md's §3 for the full model.
type QuotaMetric string

const (
	MetricRequests QuotaMetric = "requests"
	MetricTokens   QuotaMetric = "tokens"
	// MetricCost is P2.2's Credits/money-denominated metric — see
	// docs/VirtualModelRouter_Design_v4_Quota.md's pricing/cost sections.
	// Charging it needs a resolved Endpoint.PricingRate; config.validate()
	// rejects a metric: cost Limit on any provider+model that doesn't
	// resolve one with every component present (see PricingSpec/Rate's doc
	// comments).
	MetricCost QuotaMetric = "cost"
)

// Limit is one window-level quota constraint: "in this long a period,
// starting from this anchor, at most this much." The runtime (yaml-tag-free)
// counterpart of config.LimitConfig, resolved once at BuildSnapshot time —
// see config.LimitConfig's doc comment for why the two are split into
// separate YAML-shape/runtime-shape types (the config.EndpointGroup ->
// core.Endpoint precedent).
//
// P1 supports exactly one Limit per provider, tumbling only (no Rolling
// field here at all — config.LimitConfig.Rolling exists solely to produce a
// clear "not yet supported" load error, never reaches this type).
type Limit struct {
	Metric QuotaMetric
	EveryN int
	// EveryUnit is one of "h"/"d"/"w"/"mo" — see period.go's PeriodStart/
	// PeriodEnd for the calendar arithmetic each implies.
	EveryUnit string
	// EveryText is the original "every" text (e.g. "1mo", "2w") — used
	// as-is for quota.Registry's limitKey and for display (vmr check,
	// /admin/status), so a human-readable value never needs reconstructing
	// from EveryN+EveryUnit.
	EveryText string
	// Since is the resolved period anchor — either the config's explicit
	// value or the unit-specific default (see config.LimitConfig.Since's
	// doc comment) — already parsed once here so period.go's hot-path
	// PeriodStart/PeriodEnd never reparses a string.
	Since  time.Time
	Amount float64
}

// TokenWeights is the account-level per-component scaling factor applied to
// a tokens-metric Limit's base(tokens) formula — see
// docs/VirtualModelRouter_Design_v4_Quota.md's §3 (charge = base(metric) ×
// ModelMultipliers[model]) and its "Simplification" section ⑧ for why this
// lives here rather than as a per-model price table: a Credits-style plan
// whose account discounts cache reads (or prices output higher) uniformly
// across all its models needs one shared ratio, not a per-model rate table
// (that's what metric: cost's pricing layer is for instead).
//
// The zero value is {0,0,0,0} — NOT the "1.0 across the board" default this
// type is documented to have. config.validate() (or router.BuildSnapshot,
// whichever resolves config.QuotaConfig into core.QuotaSpec) MUST explicitly
// fill every unset component with DefaultTokenWeight; nothing in this
// package does that for you. This is the same class of trap
// HeadroomCap/epsilon hit during P1 — recorded here so it isn't rediscovered.
type TokenWeights struct {
	InFresh    float64
	CacheRead  float64
	CacheWrite float64
	Out        float64
}

// DefaultTokenWeight is the value every one of TokenWeights' four
// components resolves to when the account's config leaves it unset — see
// TokenWeights' doc comment for why this can't just be relied on as Go's
// zero value.
const DefaultTokenWeight = 1.0

// NewTokenWeights returns the all-DefaultTokenWeight value TokenWeights'
// zero value is NOT — the one correct starting point for any caller
// resolving an account's token_weights, whether or not it configured one.
// Two production call sites (internal/config/quota.go's TokenWeightsConfig.
// resolve, cmd/vmr/cmd_check.go's "is this non-default" check) used to each
// spell out the same four-field literal by hand; a third that did the same
// thing slightly differently would have been a silent, uncompiler-caught
// drift risk (see this type's doc comment on the zero-value trap).
func NewTokenWeights() TokenWeights {
	return TokenWeights{
		InFresh: DefaultTokenWeight, CacheRead: DefaultTokenWeight,
		CacheWrite: DefaultTokenWeight, Out: DefaultTokenWeight,
	}
}

// Rate is a per-1,000,000-token four-component price snapshot — the
// runtime-shape counterpart of internal/pricing.Rate (this package cannot
// import internal/pricing: core is a zero-internal-dep package, see
// archtest's TestArchitecture_ZeroInternalDepPackages), in whatever
// currency the resolving account's pricing.currency names. A nil component
// means "unknown", never "free" — see PricingSpec's doc comment for why
// that distinction survives all the way to this type.
type Rate struct {
	InFresh    *float64
	CacheRead  *float64
	CacheWrite *float64
	Out        *float64
}

// PricingOverride is one model-scoped rule from an account's
// providers[].pricing.overrides, already filtered (at resolve time, in
// internal/pricing) to the ones whose `model` pattern matches this specific
// Endpoint's Model — see PricingSpec.EffectiveRate (internal/pricing.
// EffectiveRate, the function that actually walks this slice; core stays
// pure data, per this package's own "shared types, no internal deps"
// charter). No time dimension (date/hour window) — P0-A dropped that
// functionality; see internal/pricing.OverrideRule's doc comment for why.
type PricingOverride struct {
	// Discount, when non-nil, means "the rate that resolves BELOW this rule
	// in the chain, scaled by this factor" — NOT always PricingSpec.Base
	// directly: "below" can be another, more specific Override (see
	// internal/pricing.EffectiveRate/resolveChain's doc comments — folding a
	// discount straight onto Base instead of the chain below it double-
	// applies it whenever two discount rules are stacked). Explicit, when
	// Discount is nil, is used as-is. Discount and Explicit are mutually
	// exclusive by construction — internal/pricing's config-time resolution
	// never produces one with both set.
	Discount *float64
	Explicit Rate
}

// PricingSpec is one provider+model's fully resolved pricing (P2.2):
// Base — the rate reachable with no Override present (from the
// standard/supplement table, or an account override that fully replaces
// it) — plus zero or more Overrides layered on top, evaluated in written
// order (first-match-wins; a Discount composes against whatever the chain
// below it resolves to). Attached per-Endpoint (not per-account, unlike
// QuotaSpec) because price is inherently model-scoped — see
// docs/VirtualModelRouter_Design_v4_Quota.md's "9.2 运行态" section
// ("定价解析结果的挂点不一样") for why this couldn't be shared the way
// QuotaSpec is.
//
// Why Base can't just collapse into a single resolved core.Rate at
// config-load time: a wildcard catch-all Override (e.g. a blanket account
// discount) can be layered above a model-specific Explicit override, and
// EffectiveRate needs the full Base+Overrides chain to compose that
// correctly — see internal/pricing.EffectiveRate.
type PricingSpec struct {
	Base      Rate
	Overrides []PricingOverride
	// Currency is the account's pricing.currency this Spec's amounts are
	// denominated in — carried along so a charge doesn't need a second
	// lookup back into config just to label the number it just computed.
	Currency string
}

// QuotaSpec is a provider's full quota configuration: its Limit(s) (P1: just
// one; already shaped as a slice so P3's multi-window support is additive,
// not a type change) plus two account-level charge-time modifiers (P2.1):
//
//   - TokenWeights scales a tokens-metric Limit's four components when read
//     (applied in baseAmount, at read time — see
//     docs/VirtualModelRouter_Design_v4_Quota.md's "9.2 运行态" section
//     for the "store raw components, apply policy on read" principle, which
//     holds for this field).
//   - ModelMultipliers scales EVERY component (including Requests) of a
//     charge by the upstream model actually hit, keyed by that model name
//     with an optional "*" wildcard fallback; nil/absent-key means 1.0 (no
//     scaling). Unlike TokenWeights, this one is applied at CHARGE time, not
//     read time — see the same "9.2 运行态" section's model_multipliers
//     discussion for why: quota.Counters aggregates per-provider, not
//     per-model, so once a charge lands there is no way to recover which
//     slice of a later read came from which upstream model to multiply
//     retroactively.
type QuotaSpec struct {
	Limits           []Limit
	TokenWeights     TokenWeights
	ModelMultipliers map[string]float64
}

// SortedKeys returns m's keys in sorted order. A recurring need across
// packages that print or iterate a map deterministically (config summaries,
// adapter/model registries, header tables).
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// asciiBytesPerToken/wideBytesPerToken split EstimateTextTokens' byte count
// by script: ASCII (mostly English) tokenizes at roughly 4 bytes/token
// across mainstream BPE tokenizers; CJK and other multi-byte UTF-8 content
// is denser and deliberately overestimated at 2 bytes/token — higher than
// any tokenizer this was checked against actually costs, on purpose (see
// the design doc's token-estimation calibration).
const (
	asciiBytesPerToken = 4
	wideBytesPerToken  = 2
)

// EstimateTextTokens scans body byte-for-byte (JSON structure included — its
// overhead only pushes the estimate up, which is the safe direction)
// classifying by UTF-8 lead byte, no rune decoding. Originally the pre-
// routing estimator behind RequestFacts.EstimatedTokens (see
// server/facts.go); exported here so any caller needing a cheap, consistent
// token estimate from raw text/bytes — not just the routing path — shares
// the same formula instead of growing its own copy.
func EstimateTextTokens(body []byte) int64 {
	var ascii, wide int64
	for _, b := range body {
		if b < 0x80 {
			ascii++
		} else {
			wide++
		}
	}
	return EstimateTokensFromCounts(ascii, wide)
}

// EstimateTokensFromCounts applies EstimateTextTokens' formula to byte
// counts already tallied elsewhere (internal/router/response.go's respStream
// classifies bytes incrementally, as they arrive, rather than buffering a
// whole body to hand to EstimateTextTokens) — exported so both call sites
// share the exact same coefficients instead of one silently drifting from
// the other.
func EstimateTokensFromCounts(ascii, wide int64) int64 {
	return ascii/asciiBytesPerToken + wide/wideBytesPerToken
}
