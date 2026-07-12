// Ver 2026-07-08 08:30, by Fable 5
package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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
	default:
		return "transient"
	}
}

// Endpoint is the smallest schedulable unit: provider × real model × scheduling attrs.
type Endpoint struct {
	Provider    string // provider name as written in config
	AdapterType string
	BaseURL     string
	APIKey      string
	Model       string
	Priority    int
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
