// Ver 2026-07-12 16:30, by Fable 5
package adapter

import (
	"context"
	"fmt"
	"net/http"
	"sort"
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
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
