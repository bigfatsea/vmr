// Ver 2026-07-30, by Sonnet 5

// Exhaustive failover: by default the router walks every available endpoint
// until one succeeds; max_attempts (>0) is an optional cap.
package server

import (
	"testing"
)

// TestServe_MultiEndpointFailoverSequence (internal/router/router_serve_test.go)
// is the authoritative coverage for exhausting all endpoints in order — same
// assertions (final endpoint header, attempt count, per-endpoint hit count),
// exercised at the Serve()-unit level instead of over real HTTP. This file
// used to duplicate that scenario end-to-end; removed as a genuine duplicate
// (docs/test_review_action_plan_sonnet-5.md Batch 2) rather than trimmed,
// since every assertion here had an exact router-level equivalent.

func TestMaxAttemptsStillCapsWhenSet(t *testing.T) {
	ups := make([]*upstream, 4)
	for i := range ups {
		ups[i] = newUpstream(t)
		ups[i].status.Store(500)
	}
	ts := newRouterServer(t, fourEndpointYAML(ups[0].srv.URL, ups[1].srv.URL, ups[2].srv.URL, ups[3].srv.URL, "max_attempts: 2"))

	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 500 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "2" {
		t.Errorf("attempts: %s", got)
	}
	if ups[2].hits.Load() != 0 || ups[3].hits.Load() != 0 {
		t.Error("endpoints beyond the cap must not be tried")
	}
}
