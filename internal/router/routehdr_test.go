// Ver 2026-07-28 15:45, by Opus 5
package router

import (
	"net/http"
	"testing"

	"vmr/internal/core"
)

// The common case must stay short: three healthy endpoints, nothing
// eliminated, order decided it. Anything longer here means the header is
// paying rent on every response for information nobody needed.
func TestRouteReasonOmitsWhatDidNotHappen(t *testing.T) {
	got := routeReason{total: 3, healthOK: 3, afterCond: 3}.String()
	if want := "pick=order eligible=3/3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRouteReasonReportsEliminations(t *testing.T) {
	cases := []struct {
		name string
		rr   routeReason
		want string
	}{
		{"sticky hit", routeReason{total: 2, healthOK: 2, afterCond: 2, sticky: true},
			"pick=sticky eligible=2/2"},
		{"one cooling down", routeReason{total: 3, healthOK: 2, afterCond: 2},
			"pick=order eligible=2/3 cooldown=1"},
		{"condition eliminated two", routeReason{total: 3, healthOK: 3, afterCond: 1},
			"pick=order eligible=1/3 conditions=2"},
		{"context fallback", routeReason{total: 2, healthOK: 2, afterCond: 2, ctxFallback: true},
			"pick=order eligible=2/2 ctx_fallback=1"},
		{"everything at once", routeReason{total: 5, healthOK: 4, afterCond: 2, ctxFallback: true, sticky: true},
			"pick=sticky eligible=2/5 cooldown=1 conditions=2 ctx_fallback=1"},
	}
	for _, c := range cases {
		if got := c.rr.String(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFailoverTrail(t *testing.T) {
	var ft failoverTrail
	h := http.Header{}

	// Nothing failed yet: no header at all, rather than an empty one.
	ft.apply(h)
	if _, ok := h["X-Vmr-Failover"]; ok {
		t.Error("empty trail must not set the header")
	}

	ft.add(&core.Endpoint{Provider: "deepseek", Model: "deepseek-v4"}, 429)
	ft.add(&core.Endpoint{Provider: "minimax", Model: "m2"}, 500)
	ft.add(&core.Endpoint{Provider: "local", Model: "x"}, 0) // build/network failure: no HTTP response
	ft.apply(h)

	want := "deepseek/deepseek-v4:429, minimax/m2:500, local/x:err"
	if got := h.Get("X-VMR-Failover"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Provider/model names come from config.yaml. A stray control character
// there must degrade this diagnostic header, never fail the whole response
// (Go's writer rejects invalid header values outright).
func TestFailoverTrailStripsHeaderBreakers(t *testing.T) {
	var ft failoverTrail
	ft.add(&core.Endpoint{Provider: "ev\r\nil", Model: "a:b,c"}, 500)
	h := http.Header{}
	ft.apply(h)
	if got := h.Get("X-VMR-Failover"); got != "evil/abc:500" {
		t.Errorf("got %q", got)
	}
}
