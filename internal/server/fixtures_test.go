// Ver 2026-08-02, by Sonnet 5

// Shared YAML config fixtures for internal/server's integration tests.
// Consolidated here (docs/test_review_action_plan_sonnet-5.md Batch 3, T2-1)
// from eight different test files where each had grown its own — a new test
// needing a config shape should look here first instead of writing another
// near-duplicate builder.
package server

import "fmt"

// twoEndpointYAML is the shared fixture for failover/health/probe tests,
// which repeatedly send the byte-identical simpleReq and expect each call
// to be freshly routed by health/priority — sticky: false so Sticky
// Model's default-on affinity (see docs/VirtualModelRouter_Design_v4_Core.md
// §6.5) never pins repeated calls to whichever endpoint last succeeded and
// masks the health/priority behavior these tests exist to pin down.
func twoEndpointYAML(u1, u2 string, extra string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    sticky: false
    endpoints:
      - {protocol: openai, provider: p1, models: [model-one], priority: 1}
      - {protocol: openai, provider: p2, models: [model-two], priority: 2}
`, extra, u1, u2)
}

// oneProviderYAML is a minimal config: one virtual model, one provider.
func oneProviderYAML(u string) string {
	return `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: ` + u + `}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [upstream-model]}
`
}

// Priority is deliberately omitted below: this is the idiom the schema is
// designed around — file order alone decides try-order via stable sort.
func fourEndpointYAML(u1, u2, u3, u4, extra string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
  - {name: p3, base_url: {openai: %s}, api_key: k3}
  - {name: p4, base_url: {openai: %s}, api_key: k4}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [m1]}
      - {protocol: openai, provider: p2, models: [m2]}
      - {protocol: openai, provider: p3, models: [m3]}
      - {protocol: openai, provider: p4, models: [m4]}
`, extra, u1, u2, u3, u4)
}

// capabilityYAML builds a two-endpoint virtual model where p1 declares only
// declP1 capabilities and p2 declares only declP2 — sticky disabled (these
// tests are about condition filtering, not session affinity, and repeated
// identical requests would otherwise get pinned by it).
func capabilityYAML(u1, u2, declP1, declP2 string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    sticky: false
    endpoints:
      - protocol: openai
        provider: p1
        models: [model-one]
        priority: 1%s
      - protocol: openai
        provider: p2
        models: [model-two]
        priority: 2%s
`, u1, u2, declP1, declP2)
}

// contextLenYAML gives p1 a small declared context window and p2 a large
// one (or none — unconstrained), both otherwise identical.
func contextLenYAML(u1, u2 string, p1Max, p2Max string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    sticky: false
    endpoints:
      - protocol: openai
        provider: p1
        models: [model-one]
        priority: 1%s
      - protocol: openai
        provider: p2
        models: [model-two]
        priority: 2%s
`, u1, u2, p1Max, p2Max)
}

// stickyYAML is stickyYAML(u1, u2, extraModelLines): unlike twoEndpointYAML,
// sticky defaults to true here (these tests exist to exercise it).
func stickyYAML(u1, u2, extraModelLines string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    %s
    endpoints:
      - {protocol: openai, provider: p1, models: [model-one], priority: 1}
      - {protocol: openai, provider: p2, models: [model-two], priority: 2}
`, u1, u2, extraModelLines)
}

func dualProtocolYAML(oai, anth1, anth2 string, extra string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
providers:
  - {name: oai, base_url: {openai: %s}, api_key: k0}
  - {name: a1, base_url: {anthropic: %s}, api_key: ka1}
  - {name: a2, base_url: {anthropic: %s}, api_key: ka2}
models:
  vm-openai:
    endpoints:
      - {protocol: openai, provider: oai, models: [model-one], priority: 1}
  vm-anth:
    endpoints:
      - {protocol: anthropic, provider: a1, models: [real-a], priority: 1}
      - {protocol: anthropic, provider: a2, models: [real-b], priority: 2}
`, extra, oai, anth1, anth2)
}
