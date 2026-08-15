// Ver 2026-07-22 21:00, by Sonnet 5
package adapter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"vmr/internal/core"
)

// Adapter carries requests to one provider protocol family and back.
// Adding a provider = one package implementing these methods + one blank import.
// VMR never converts between protocols: an adapter's Protocol() names the
// ingress protocol it serves, and routing stays within that protocol —
// response bodies flow back untouched (the router's normalizer handles the
// few guarded quirk repairs; see internal/respnorm).
type Adapter interface {
	// Protocol names the ingress protocol this adapter speaks ("openai",
	// "anthropic", "openai-responses"). Each endpoint-group under a virtual
	// model belongs to exactly one protocol; a name can mix several groups
	// of different protocols to be reachable from more than one ingress.
	Protocol() string

	// ResolveURL returns the complete upstream URL for a given base_url.
	// Convention: base_url always carries the provider's own full API
	// version (/v1, /v3, whatever the provider calls it) — this adapter's
	// path suffix is only the bare protocol path, never a version. Called
	// once at initialization (BuildSnapshot, diagnose, replay) — not per
	// request — so BuildRequest just uses the pre-computed ep.FullURL.
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

// registry is read on every failover attempt (tryOne, probe.go) via a
// lock-free atomic load instead of an RWMutex.RLock/RUnlock pair for a map
// that, in practice, never changes after the first few milliseconds of
// process startup (each provider subpackage's init() calls Register exactly
// once). registerMu serializes the copy-on-write writes themselves:
// Register is only ever called from sequential init() in production, so this
// never actually contends, but a plain copy-on-write without it would lose
// an update under two genuinely concurrent Register calls (last Store wins,
// silently dropping the other writer's entry) — caught by
// TestGetConcurrentWithRegister under -race.
var (
	registerMu sync.Mutex
	registry   atomic.Pointer[map[string]Adapter]
)

// Register makes an adapter available under the given config `type` name.
// It follows the database/sql driver pattern and is called from init().
func Register(name string, a Adapter) {
	registerMu.Lock()
	defer registerMu.Unlock()
	next := map[string]Adapter{}
	if cur := registry.Load(); cur != nil {
		for k, v := range *cur {
			next[k] = v
		}
	}
	if _, dup := next[name]; dup {
		panic(fmt.Sprintf("adapter: Register called twice for %q", name))
	}
	next[name] = a
	registry.Store(&next)
}

func Get(name string) (Adapter, bool) {
	cur := registry.Load()
	if cur == nil {
		return nil, false
	}
	a, ok := (*cur)[name]
	return a, ok
}

func Names() []string {
	cur := registry.Load()
	if cur == nil {
		return nil
	}
	return core.SortedKeys(*cur)
}

// ResolveURL joins baseURL and suffix into the complete upstream URL.
// The provider's own API version is expected to already be part of
// baseURL (e.g. "https://api.example.com/v1", ".../api/coding/v3") —
// suffix is only the bare protocol path ("/chat/completions",
// "/messages") and never carries a version, so the two never need
// de-duplicating against each other. A trailing slash on baseURL is
// trimmed first. This is the shared implementation every adapter's
// ResolveURL method delegates to. Called once at initialization — never
// per request.
//
// Examples (suffix = "/chat/completions"):
//
//	"https://api.example.com/v1"		→ "https://api.example.com/v1/chat/completions"
//	"https://api.example.com/v1/"		→ "https://api.example.com/v1/chat/completions"  (trailing / trimmed)
//	"https://ark.example.com/coding/v3"	→ "https://ark.example.com/coding/v3/chat/completions"
func ResolveURL(baseURL, suffix string) string {
	return strings.TrimRight(baseURL, "/") + suffix
}
