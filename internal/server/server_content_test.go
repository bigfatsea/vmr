// Ver 2026-07-07, by Fable 5

// Content-policy flags are request-specific: they must trigger failover but
// leave the flagged endpoint's health untouched.
package server

import (
	"testing"
)

func TestContentFlagFailsOverWithoutCooldown(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(403)
	u1.errBody.Store(`{"error":{"message":"Your input was flagged","metadata":{"reasons":["x"]}}}`)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	// Flagged on p1 → served by p2.
	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "p2/model-two" {
		t.Fatalf("failover: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "2" {
		t.Errorf("attempts: %s", got)
	}

	// p1 must NOT be in cooldown: the very next request tries it first again.
	u1.status.Store(200)
	resp, _ = chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "p1/model-one" {
		t.Errorf("p1 was penalized for a content flag: served by %s", got)
	}
}

func TestAllEndpointsFlaggedReturnsContentError(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	for _, u := range []*upstream{u1, u2} {
		u.status.Store(403)
		u.errBody.Store(`{"error":{"message":"input was flagged by moderation"}}`)
	}
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 403 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-VMR-Attempts") != "2" {
		t.Errorf("both endpoints must have been tried: attempts=%s", resp.Header.Get("X-VMR-Attempts"))
	}
	// Neither endpoint cooled down: both reachable again immediately.
	u1.status.Store(200)
	resp, _ = chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "p1/model-one" {
		t.Errorf("p1 should be available immediately: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}
