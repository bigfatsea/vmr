// Ver 2026-07-07 01:55, by Fable 5
package adapter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"vmr/internal/core"
)

// Adapter carries requests to one provider protocol family and back.
// Adding a provider = one package implementing these methods + one blank import.
// VMR never converts between protocols: an adapter's Protocol() names the
// ingress protocol it serves, and routing stays within that protocol.
type Adapter interface {
	// Protocol names the ingress protocol this adapter speaks ("openai", "anthropic").
	// A virtual model's endpoints must all share one protocol.
	Protocol() string

	// BuildRequest turns the canonical request into the provider's HTTP request
	// (URL, headers, body rewrite). It must inject the provider's credentials.
	BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, error)

	// TransformBody converts the provider response body to the ingress format.
	// Compatible providers return the body unchanged (zero copy).
	TransformBody(body io.ReadCloser, stream bool) io.ReadCloser

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
