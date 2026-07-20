// Ver 2026-07-17 08:00, by Sonnet 5
package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
}

// HealthKey identifies this endpoint in the health registry. It is stable
// across config reloads so cooldown state survives a hot reload. AdapterType
// is part of the key because a provider name can now be reused across
// protocol groups (e.g. the same "openrouter" name under both providers.openai
// and providers.anthropic) — without it, two genuinely different endpoints
// sharing a name, API key, and upstream model string would collide.
func (e *Endpoint) HealthKey() string {
	sum := sha256.Sum256([]byte(e.APIKey))
	return e.AdapterType + "/" + e.Provider + "/" + e.Model + "/" + hex.EncodeToString(sum[:4])
}

// Name is the human-readable endpoint label used in logs and status output.
func (e *Endpoint) Name() string {
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

// FmtBytes renders a byte count human-readably (B/KB/MB) — request/response
// bodies range from a few hundred bytes to several MB (inline images), so a
// fixed unit would be either unreadable or falsely precise at one end.
// Shared by every place that prints a body size (live router log, `vmr
// report` rendering) so they don't each carry their own copy of this
// threshold logic.
func FmtBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// FmtSeconds renders d as fixed-decimal seconds ("6.32s") instead of
// Duration.String()'s mixed units (ms/s/m) — a column where some rows read
// "141ms" and others "1m4s" doesn't scan as a column; one unit throughout
// does. decimals lets callers trade precision for width (2 for the live
// router log, 3 for `vmr diagnose`'s sub-10ms-sensitive latency columns).
func FmtSeconds(d time.Duration, decimals int) string {
	return fmt.Sprintf("%.*fs", decimals, d.Seconds())
}
