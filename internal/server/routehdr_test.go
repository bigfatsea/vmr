// Ver 2026-07-28 16:00, by Opus 5

// End-to-end proof that the routing-feedback headers reach a real client.
// The router package tests the formatting; what can only be verified here
// is the ordering constraint they depend on: X-VMR-Failover is written on
// the ResponseWriter *before* the attempt that ends up succeeding, because
// that attempt writes the response itself and never hands control back.
package server

import (
	"strings"
	"testing"
)

func TestRouteHeadersOnSuccessfulFailover(t *testing.T) {
	ups := make([]*upstream, 4)
	for i := range ups {
		ups[i] = newUpstream(t)
	}
	ups[0].status.Store(500)
	ups[1].status.Store(429)
	ups[2].status.Store(503)
	ts := newRouterServer(t, fourEndpointYAML(ups[0].srv.URL, ups[1].srv.URL, ups[2].srv.URL, ups[3].srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	// The request succeeded, yet the response still explains what it cost
	// to get there — that's the whole point of setting it before the call.
	want := "p1/m1:500, p2/m2:429, p3/m3:503"
	if got := resp.Header.Get("X-VMR-Failover"); got != want {
		t.Errorf("X-VMR-Failover = %q, want %q", got, want)
	}
	if got := resp.Header.Get("X-VMR-Route-Reason"); got != "pick=order eligible=4/4" {
		t.Errorf("X-VMR-Route-Reason = %q", got)
	}
}

// The no-failover case must not carry a failover header at all: an empty
// one reads like "a failover happened and had nothing to say".
func TestRouteHeadersOnFirstTrySuccess(t *testing.T) {
	ups := make([]*upstream, 4)
	for i := range ups {
		ups[i] = newUpstream(t)
	}
	ts := newRouterServer(t, fourEndpointYAML(ups[0].srv.URL, ups[1].srv.URL, ups[2].srv.URL, ups[3].srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Failover"); got != "" {
		t.Errorf("X-VMR-Failover = %q, want it absent", got)
	}
	if got := resp.Header.Get("X-VMR-Route-Reason"); got != "pick=order eligible=4/4" {
		t.Errorf("X-VMR-Route-Reason = %q", got)
	}
}

// When everything fails the client gets the last upstream error verbatim —
// the diagnostic headers must survive that path too, since it's the one
// where "what did each endpoint say" matters most.
func TestRouteHeadersWhenEveryEndpointFails(t *testing.T) {
	ups := make([]*upstream, 4)
	for i := range ups {
		ups[i] = newUpstream(t)
		ups[i].status.Store(500)
	}
	ts := newRouterServer(t, fourEndpointYAML(ups[0].srv.URL, ups[1].srv.URL, ups[2].srv.URL, ups[3].srv.URL, ""))

	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 500 {
		t.Fatalf("status=%d, want the last upstream error verbatim", resp.StatusCode)
	}
	got := resp.Header.Get("X-VMR-Failover")
	if strings.Count(got, ",") != 3 {
		t.Errorf("X-VMR-Failover = %q, want all four attempts listed", got)
	}
	if !strings.HasSuffix(got, "p4/m4:500") {
		t.Errorf("X-VMR-Failover = %q, want it to end with the last attempt", got)
	}
}
