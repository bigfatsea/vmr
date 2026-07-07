// Ver 2026-07-07 01:55, by Fable 5
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// CanonicalRequest is the routing view of a chat request. Both supported
// ingress protocols (OpenAI chat completions, Anthropic messages) carry
// top-level "model" and "stream" fields; only those are parsed. Raw keeps
// every other byte untouched so unknown upstream parameters pass through
// unchanged — VMR never converts between protocols.
type CanonicalRequest struct {
	Model  string
	Stream bool
	Raw    json.RawMessage
	Header http.Header // whitelisted protocol headers (anthropic-version, anthropic-beta)
}

// HeaderGet returns a whitelisted header value, tolerating a nil Header.
func (r *CanonicalRequest) HeaderGet(key string) string {
	if r.Header == nil {
		return ""
	}
	return r.Header.Get(key)
}

// ErrorClass drives failover and cooldown decisions.
type ErrorClass int

const (
	ErrClient    ErrorClass = iota // request itself is bad: return to client, no failover
	ErrAuth                        // 401/403: long cooldown, switch
	ErrRateLimit                   // 429: honor Retry-After, switch
	ErrEndpoint                    // endpoint persistently unusable (quota/402, unknown model/404): long cooldown, switch
	ErrTransient                   // 5xx/408/timeouts/network: short cooldown, switch
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
// across config reloads so cooldown state survives a hot reload.
func (e *Endpoint) HealthKey() string {
	sum := sha256.Sum256([]byte(e.APIKey))
	return e.Provider + "/" + e.Model + "/" + hex.EncodeToString(sum[:4])
}

// Name is the human-readable endpoint label used in logs and status output.
func (e *Endpoint) Name() string {
	return e.Provider + "/" + e.Model
}
