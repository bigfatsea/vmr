// Ver 2026-07-23 11:00, by Sonnet 5
//
// End-to-end tests for Sticky Model (see
// docs/vmr_condition_routing_and_sticky_model_sonnet-5.md §2): once a
// conversation successfully lands on an endpoint, follow-up turns of the
// SAME conversation should keep going there even if plain priority would
// otherwise prefer a different endpoint — but only for the same
// conversation, identified by system prompt + first message, not by
// coincidence of an identical opening line.
//
// Every test forces the initial p1→p2 failover with a content-policy flag
// (403 + "flagged" body): internal/adapter/classify.go classifies that as
// core.ErrContent, which carries zero cooldown (see classify.go's
// contentHint). That isolates the sticky mechanism from health/cooldown
// side effects — without it, p1 would still be excluded by its own
// cooldown on the second call, and these tests couldn't tell "sticky kept
// it on p2" apart from "p1 just hadn't recovered yet".
package server

import (
	"fmt"
	"testing"
	"time"
)

// stickyYAML is stickyYAML(u1, u2, extraModelLines): unlike twoEndpointYAML,
// sticky defaults to true here (these tests exist to exercise it).
func stickyYAML(u1, u2, extraModelLines string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: %s, api_key: k1}
    p2: {base_url: %s, api_key: k2}
models:
  openai:
    vm:
      %s
      endpoints:
        - {provider: p1, model: model-one, priority: 1}
        - {provider: p2, model: model-two, priority: 2}
`, u1, u2, extraModelLines)
}

func flagP1(u1 *upstream) {
	u1.status.Store(403)
	u1.errBody.Store(`{"error":{"message":"flagged"}}`)
}

func TestSticky_PinsToLastSuccessfulEndpoint(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	flagP1(u1)
	ts := newRouterServer(t, stickyYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("setup: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}

	u1.status.Store(200) // priority alone would now prefer p1 again
	resp, _ = chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Errorf("endpoint=%s, want p2 (sticky should keep the same conversation on its last successful endpoint)", got)
	}
	if u1.hits.Load() != 1 {
		t.Errorf("p1 should not have been tried again once p2 is sticky, hits=%d", u1.hits.Load())
	}
}

func TestSticky_DifferentConversationNotPinned(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	flagP1(u1)
	ts := newRouterServer(t, stickyYAML(u1.srv.URL, u2.srv.URL, ""))

	chat(t, ts, simpleReq, nil) // establishes a sticky entry for this conversation's fingerprint

	u1.status.Store(200)
	otherReq := `{"model":"vm","messages":[{"role":"user","content":"totally different first message"}]}`
	resp, _ := chat(t, ts, otherReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("endpoint=%s, want p1 (a different conversation has no sticky entry; priority should win)", got)
	}
}

// TestSticky_DifferentSystemPromptSameFirstMessageNotPinned is the
// regression test for the whole reason the anchor includes the system
// prompt (design doc §2.1): two different agents whose conversations
// happen to open with the identical user message must NOT share a sticky
// entry, because their prompt-cache prefixes diverge at the system prompt,
// before the shared opening line is ever reached.
func TestSticky_DifferentSystemPromptSameFirstMessageNotPinned(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	flagP1(u1)
	ts := newRouterServer(t, stickyYAML(u1.srv.URL, u2.srv.URL, ""))

	agentA := `{"model":"vm","messages":[{"role":"system","content":"you are Agent A"},{"role":"user","content":"hi"}]}`
	resp, _ := chat(t, ts, agentA, nil)
	if resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("setup (Agent A) did not land on p2 as expected")
	}

	u1.status.Store(200)
	agentB := `{"model":"vm","messages":[{"role":"system","content":"you are Agent B"},{"role":"user","content":"hi"}]}`
	resp, _ = chat(t, ts, agentB, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("endpoint=%s, want p1 (Agent B has a different system prompt; must not inherit Agent A's sticky pointer just because the opening user line matches)", got)
	}
}

func TestSticky_TTLExpiry(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	flagP1(u1)
	// sticky_ttl is per-endpoint (design doc §2.3 — cache lifetime is a
	// property of the upstream provider, not of the virtual model), so
	// it's declared on p2 specifically: that's the endpoint the sticky
	// entry this test is about will point at.
	ts := newRouterServer(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: %s, api_key: k1}
    p2: {base_url: %s, api_key: k2}
models:
  openai:
    vm:
      endpoints:
        - provider: p1
          model: model-one
          priority: 1
        - provider: p2
          model: model-two
          priority: 2
          sticky_ttl: 200ms
`, u1.srv.URL, u2.srv.URL))

	chat(t, ts, simpleReq, nil) // establishes p2 stickiness

	u1.status.Store(200)
	time.Sleep(350 * time.Millisecond) // past sticky_ttl

	resp, _ := chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("endpoint=%s, want p1 (the sticky entry should have expired)", got)
	}
}

func TestSticky_FailoverMovesThePointer(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	flagP1(u1)
	ts := newRouterServer(t, stickyYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, _ := chat(t, ts, simpleReq, nil) // p1 flagged -> p2 succeeds, sticky now points at p2
	if resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("setup did not land on p2 as expected")
	}

	// Flip which endpoint is flagged: now p2 is flagged, p1 has recovered.
	u1.status.Store(200)
	u2.status.Store(403)
	u2.errBody.Store(`{"error":{"message":"flagged"}}`)
	resp, _ = chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p1/model-one" {
		t.Fatalf("expected failover from sticky p2 (now flagged) to p1: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}

	// The sticky pointer should have moved to p1 — p2 recovering shouldn't matter now.
	u2.status.Store(200)
	resp, _ = chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("endpoint=%s, want p1 (sticky pointer should have followed the failover success)", got)
	}
}

func TestSticky_DisabledMeansNoAffinity(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	flagP1(u1)
	ts := newRouterServer(t, stickyYAML(u1.srv.URL, u2.srv.URL, "sticky: false"))

	chat(t, ts, simpleReq, nil) // p1 flagged -> p2 succeeds; sticky is off, so no entry should be recorded

	u1.status.Store(200)
	resp, _ := chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("endpoint=%s, want p1 (sticky: false — priority should decide, unaffected by the earlier p2 success)", got)
	}
}
