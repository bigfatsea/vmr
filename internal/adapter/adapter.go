// Ver 2026-07-17 08:00, by Sonnet 5
package adapter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"vmr/internal/core"
)

// Adapter carries requests to one provider protocol family and back.
// Adding a provider = one package implementing these methods + one blank import.
// VMR never converts between protocols: an adapter's Protocol() names the
// ingress protocol it serves, and routing stays within that protocol —
// response bodies flow back untouched (the router's normalizer handles the
// few guarded quirk repairs; see internal/router/response.go).
type Adapter interface {
	// Protocol names the ingress protocol this adapter speaks ("openai", "anthropic").
	// A virtual model's endpoints must all share one protocol.
	Protocol() string

	// ResolveURL returns the complete upstream URL for a given base_url,
	// with any overlap between the base_url's tail and this adapter's path
	// suffix eliminated. Called once at initialization (BuildSnapshot,
	// diagnose, replay) — not per request — so BuildRequest just uses the
	// pre-computed ep.FullURL.
	ResolveURL(baseURL string) string

	// BuildRequest turns the canonical request into the provider's HTTP request
	// (URL, headers, body rewrite). It must inject the provider's credentials.
	// The outbound body bytes are returned alongside the request so the caller
	// (the router's audit trail) can reference them directly instead of
	// re-reading req.GetBody into a second copy; the returned slice must not
	// be mutated after BuildRequest returns.
	BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error)

	// ClassifyError maps a provider error response to a unified class that
	// drives failover and cooldown.
	ClassifyError(status int, body []byte) core.ErrorClass
}

var (
	mu       sync.RWMutex
	registry = map[string]Adapter{}
)

// Register makes an adapter available under the given config `type` name.
// It follows the database/sql driver pattern and is called from init().
func Register(name string, a Adapter) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("adapter: Register called twice for %q", name))
	}
	registry[name] = a
}

func Get(name string) (Adapter, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := registry[name]
	return a, ok
}

func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	return core.SortedKeys(registry)
}

// ResolveURL joins baseURL and suffix into a single URL, eliminating any
// overlap between the tail of baseURL and the head of suffix so path
// segments are never duplicated. This is the shared implementation every
// adapter's ResolveURL method delegates to.
//
// Examples (suffix = "/v1/chat/completions"):
//
//	"https://api.example.com"		→ "https://api.example.com/v1/chat/completions"
//	"https://api.example.com/v1"	→ "https://api.example.com/v1/chat/completions"  (overlap /v1)
//	"https://api.example.com/v1/"	→ "https://api.example.com/v1/chat/completions"  (trailing / trimmed)
//	"https://a.co/v1/chat/completions"	→ "https://a.co/v1/chat/completions"  (full overlap, no dup)
//	"https://a.co/anthropic/v1"		→ "https://a.co/anthropic/v1/chat/completions"  (suffix = /v1/messages)
//	"https://a.co/anthropic"		→ "https://a.co/anthropic/v1/messages"
//
// The algorithm finds the longest suffix of (trimmed) baseURL that is also
// a prefix of suffix, then concatenates base + suffix[overlap:], so the
// overlapping segment appears exactly once. A trailing slash on baseURL is
// trimmed first. Called once at initialization — never per request.
func ResolveURL(baseURL, suffix string) string {
	s := strings.TrimRight(baseURL, "/")
	max := len(s)
	if len(suffix) < max {
		max = len(suffix)
	}
	for i := max; i > 0; i-- {
		if strings.HasSuffix(s, suffix[:i]) {
			return s + suffix[i:]
		}
	}
	return s + suffix
}
