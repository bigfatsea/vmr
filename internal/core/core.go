// Ver 2026-07-25, by Sonnet 5
package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// MarshalNoEscape is json.Marshal without HTML escaping and without the
// trailing newline json.Encoder adds. Every place VMR re-serializes client
// JSON (model rewrite, image downscaling) uses this: the default marshal
// would rewrite < > & in message content to \uXXXX — semantically identical,
// but a gratuitous byte-level deviation from what a direct call would send.
func MarshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

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
// implementations — see docs/VirtualModelRouter_Design_v4_Core.md §6.4.
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

// Name is the human-readable endpoint label used in logs and status output.
func (e *Endpoint) Name() string {
	if e.name != "" {
		return e.name
	}
	return e.computeName()
}

func (e *Endpoint) computeName() string {
	return e.AdapterType + "/" + e.Provider + "/" + e.Model
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
	return ascii/asciiBytesPerToken + wide/wideBytesPerToken
}
