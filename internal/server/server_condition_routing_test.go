// Ver 2026-07-23 11:00, by Sonnet 5
//
// End-to-end tests for condition-based routing (see
// docs/vmr_condition_routing_and_sticky_model_sonnet-5.md §1): a request's
// content (image/tools present, estimated size) determines which endpoints
// in a virtual model's candidate list are even eligible, independent of
// health and priority.
package server

import (
	"fmt"
	"strings"
	"testing"
)

// capabilityYAML builds a two-endpoint virtual model where p1 declares only
// declP1 capabilities and p2 declares only declP2 — sticky disabled (these
// tests are about condition filtering, not session affinity, and repeated
// identical requests would otherwise get pinned by it).
func capabilityYAML(u1, u2, declP1, declP2 string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: %s, api_key: k1}
    p2: {base_url: %s, api_key: k2}
models:
  openai:
    vm:
      sticky: false
      endpoints:
        - provider: p1
          model: model-one
          priority: 1%s
        - provider: p2
          model: model-two
          priority: 2%s
`, u1, u2, declP1, declP2)
}

const imageReq = `{"model":"vm","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`
const toolsReq = `{"model":"vm","tools":[{"name":"x"}],"messages":[{"role":"user","content":"hi"}]}`

func TestCondition_ImageRoutesAwayFromNonCapableHigherPriority(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	// p1 (priority 1, would normally win) declares no image support; p2
	// declares it. An image request must skip p1 despite its priority.
	ts := newRouterServer(t, capabilityYAML(u1.srv.URL, u2.srv.URL,
		"\n          capabilities: [text, tools]",
		"\n          capabilities: [text, tools, image]"))

	resp, _ := chat(t, ts, imageReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("image request: status=%d ep=%s, want p2 (only image-capable endpoint)", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	if u1.hits.Load() != 0 {
		t.Errorf("p1 (not image-capable) should never have been tried, hits=%d", u1.hits.Load())
	}
}

func TestCondition_NonImageRequestUsesNormalPriority(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, capabilityYAML(u1.srv.URL, u2.srv.URL,
		"\n          capabilities: [text, tools]",
		"\n          capabilities: [text, tools, image]"))

	resp, _ := chat(t, ts, simpleReq, nil) // no image
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p1/model-one" {
		t.Fatalf("plain text request: status=%d ep=%s, want p1 (priority 1, condition doesn't apply)", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}

func TestCondition_UndeclaredCapabilitiesIsUnconstrained(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	// Neither endpoint declares "capabilities" at all — an image request
	// must still route by priority alone (zero-config-migration guarantee,
	// design doc §1.1).
	ts := newRouterServer(t, capabilityYAML(u1.srv.URL, u2.srv.URL, "", ""))

	resp, _ := chat(t, ts, imageReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p1/model-one" {
		t.Fatalf("status=%d ep=%s, want p1 (undeclared capabilities = unconstrained)", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}

func TestCondition_ToolsRoutesAwayFromNonCapable(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, capabilityYAML(u1.srv.URL, u2.srv.URL,
		"\n          capabilities: [text]",
		"\n          capabilities: [text, tools]"))

	resp, _ := chat(t, ts, toolsReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("tools request: status=%d ep=%s, want p2", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}

func TestCondition_AllRejectedGivesDiagnosticMessage(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	// Neither endpoint supports image — an image request must fail fast
	// (no upstream attempt at all) with a message naming the condition.
	ts := newRouterServer(t, capabilityYAML(u1.srv.URL, u2.srv.URL,
		"\n          capabilities: [text]",
		"\n          capabilities: [text]"))

	resp, body := chat(t, ts, imageReq, nil)
	if resp.StatusCode != 503 {
		t.Fatalf("status=%d, want 503", resp.StatusCode)
	}
	if !strings.Contains(body, "image") {
		t.Errorf("expected the rejection message to name the failing condition, body=%s", body)
	}
	if u1.hits.Load() != 0 || u2.hits.Load() != 0 {
		t.Errorf("neither endpoint should have been tried: p1 hits=%d p2 hits=%d", u1.hits.Load(), u2.hits.Load())
	}
}

// contextLenYAML gives p1 a small declared context window and p2 a large
// one (or none — unconstrained), both otherwise identical.
func contextLenYAML(u1, u2 string, p1Max, p2Max string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: %s, api_key: k1}
    p2: {base_url: %s, api_key: k2}
models:
  openai:
    vm:
      sticky: false
      endpoints:
        - provider: p1
          model: model-one
          priority: 1%s
        - provider: p2
          model: model-two
          priority: 2%s
`, u1, u2, p1Max, p2Max)
}

// bigReq is long enough that its estimated token count comfortably clears
// a max_context_tokens: 50 ceiling (asciiBytes/4 — a few thousand ASCII
// bytes estimates well past 50 tokens) but stays under a generous ceiling.
var bigReq = `{"model":"vm","messages":[{"role":"user","content":"` + strings.Repeat("a", 4000) + `"}]}`

func TestCondition_ContextLengthSkipsTooSmallEndpoint(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, contextLenYAML(u1.srv.URL, u2.srv.URL,
		"\n          max_context_tokens: 50",
		"\n          max_context_tokens: 1000000"))

	resp, _ := chat(t, ts, bigReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("status=%d ep=%s, want p2 (p1's declared context is too small for this request)", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	if u1.hits.Load() != 0 {
		t.Errorf("p1 (too small) should never have been tried, hits=%d", u1.hits.Load())
	}
}

func TestCondition_ContextLengthUnconstrainedByDefault(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, contextLenYAML(u1.srv.URL, u2.srv.URL, "", ""))

	resp, _ := chat(t, ts, bigReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p1/model-one" {
		t.Fatalf("status=%d ep=%s, want p1 (undeclared max_context_tokens = unconstrained, priority wins)", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}

func TestCondition_ContextLengthFallbackNeverEmptiesCandidates(t *testing.T) {
	// §1.5: both endpoints declare a ceiling the estimate exceeds. This
	// must NOT produce "no candidates" — it must fall back to trying the
	// (health/capability-eligible) set anyway, letting a real attempt and
	// #3's reactive failover make the call instead of refusing on a guess.
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, contextLenYAML(u1.srv.URL, u2.srv.URL,
		"\n          max_context_tokens: 10",
		"\n          max_context_tokens: 10"))

	resp, _ := chat(t, ts, bigReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200 — an overestimate must never empty an otherwise-eligible candidate set (§1.5)", resp.StatusCode)
	}
	// Priority order still applies within the fallback set.
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("endpoint=%s, want p1 (priority still decides among the fallback candidates)", got)
	}
}
