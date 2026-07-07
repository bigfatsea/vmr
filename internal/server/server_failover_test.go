// Ver 2026-07-07, by Fable 5

// Exhaustive failover: by default the router walks every available endpoint
// until one succeeds; max_attempts (>0) is an optional cap.
package server

import (
	"fmt"
	"testing"
)

func fourEndpointYAML(u1, u2, u3, u4, extra string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
providers:
  p1: {type: openai, base_url: %s, api_key: k1}
  p2: {type: openai, base_url: %s, api_key: k2}
  p3: {type: openai, base_url: %s, api_key: k3}
  p4: {type: openai, base_url: %s, api_key: k4}
models:
  vm:
    endpoints:
      - {provider: p1, model: m1, priority: 1}
      - {provider: p2, model: m2, priority: 2}
      - {provider: p3, model: m3, priority: 3}
      - {provider: p4, model: m4, priority: 4}
`, extra, u1, u2, u3, u4)
}

func TestFailoverExhaustsAllEndpointsByDefault(t *testing.T) {
	ups := make([]*upstream, 4)
	for i := range ups {
		ups[i] = newUpstream(t)
	}
	for _, u := range ups[:3] {
		u.status.Store(500) // first three fail; only the last one works
	}
	ts := newRouterServer(t, fourEndpointYAML(ups[0].srv.URL, ups[1].srv.URL, ups[2].srv.URL, ups[3].srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "p4/m4" {
		t.Errorf("endpoint: %s (the 4th endpoint must be reached)", got)
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "4" {
		t.Errorf("attempts: %s", got)
	}
	for i, u := range ups {
		if u.hits.Load() != 1 {
			t.Errorf("upstream %d hits=%d, want 1", i+1, u.hits.Load())
		}
	}
}

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
