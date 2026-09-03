// Ver 2026-09-03, by pi-agent

package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/quota"
)

// probeRequestsUsed reads p1's requests-metric consumption, the only counter
// a probe can move (its ChargeResponse call passes zero token counters).
func probeRequestsUsed(t *testing.T, rt *Router) float64 {
	t.Helper()
	used, _ := rt.Quota.Used("p1", "requests/1mo", quota.PeriodStart(requestsLimit(100), time.Now()))
	return used.Requests
}

// TestRunProbe_ChargesRequestsOnlyOn2xx pins the probe's quota basis: charge
// the request-metered quota ONLY when the upstream answered 2xx — the same
// basis as live traffic, where only forwardSuccess ever reaches chargeQuota.
// An error response (429/5xx) must leave the counter untouched.
func TestRunProbe_ChargesRequestsOnlyOn2xx(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, status int) (*Router, *core.Endpoint) {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"upstream says no"}}`)
		}))
		t.Cleanup(srv.Close)

		cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai-completions: `+srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`)
		snap := mustSnapshot(t, cfg)
		rt := New(nil)
		rt.Quota = quota.NewRegistry("")
		rt.Install(snap)
		snap = rt.Snapshot()
		ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
		ep.Quota = &core.QuotaSpec{Limits: []core.Limit{requestsLimit(100)}}
		return rt, ep
	}

	t.Run("429 charges nothing", func(t *testing.T) {
		t.Parallel()
		rt, ep := setup(t, http.StatusTooManyRequests)
		rt.runProbe(ep, rt.Snapshot())
		if got := probeRequestsUsed(t, rt); got != 0 {
			t.Fatalf("requests charged on a 429 probe: %v, want 0", got)
		}
	})

	t.Run("500 charges nothing", func(t *testing.T) {
		t.Parallel()
		rt, ep := setup(t, http.StatusInternalServerError)
		rt.runProbe(ep, rt.Snapshot())
		if got := probeRequestsUsed(t, rt); got != 0 {
			t.Fatalf("requests charged on a 500 probe: %v, want 0", got)
		}
	})

	t.Run("200 charges one request", func(t *testing.T) {
		t.Parallel()
		rt, ep := setup(t, http.StatusOK)
		rt.runProbe(ep, rt.Snapshot())
		if got := probeRequestsUsed(t, rt); got != 1 {
			t.Fatalf("requests charged on a 200 probe: %v, want 1", got)
		}
	})
}
