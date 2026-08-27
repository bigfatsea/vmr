// Ver 2026-08-19, by Sonnet 5
package config

import "testing"

// TestCheckFlagsDuplicateEndpoint_ViaProvidersList pins that checkModels'
// duplicate detection walks EndpointGroup.Providers — a config listing
// several providers on one entry must get the same duplicate protection a
// single-provider entry already had.
func TestCheckFlagsDuplicateEndpoint_ViaProvidersList(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://example.com}, api_key: k2}
models:
  m:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [x]}
      - {protocol: openai-completions, providers: [p1, p2], models: [x]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Model != "m" || issues[0].Field != "endpoint" || issues[0].Endpoint != "openai-completions/p1/x" {
		t.Errorf("Check() = %+v, want exactly one duplicate-endpoint issue for openai-completions/p1/x (p2/x should be untouched)", issues)
	}
}

// TestCheckFlagsFallbackDuplicatingOwnEndpoint pins the new check: a
// fallback_endpoints: entry that would silently duplicate an endpoint the
// model already declares for itself must be flagged, using the same
// protocol/provider/model key format as an ordinary duplicate.
func TestCheckFlagsFallbackDuplicatingOwnEndpoint(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
fallback_endpoints:
  - {protocol: openai-completions, providers: [p1], models: [x], priority: 90}
models:
  m:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [x]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Model != "m" || issues[0].Field != "endpoint" || issues[0].Endpoint != "openai-completions/p1/x" {
		t.Errorf("Check() = %+v, want exactly one fallback-duplicate issue for openai-completions/p1/x", issues)
	}
}

// TestCheckIgnoresFallbackDuplicate_WrongProtocol mirrors BuildSnapshot's
// own injection rule: a fallback never attaches to a protocol the model
// doesn't already have an entry point on, so it can never actually
// duplicate anything there — Check must not false-positive on this case.
func TestCheckIgnoresFallbackDuplicate_WrongProtocol(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com, anthropic-messages: https://example.com}, api_key: k1}
fallback_endpoints:
  - {protocol: anthropic-messages, providers: [p1], models: [x], priority: 90}
models:
  m:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [x]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("Check() = %+v, want no issues — model m has no anthropic entry point for the fallback to attach to", issues)
	}
}

// TestCheckIgnoresFallbackDuplicate_ModelOptedOut mirrors BuildSnapshot's
// fallback: false opt-out — a model that never receives the injection
// cannot have it flagged as a duplicate either.
func TestCheckIgnoresFallbackDuplicate_ModelOptedOut(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
fallback_endpoints:
  - {protocol: openai-completions, providers: [p1], models: [x], priority: 90}
models:
  m:
    fallback: false
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [x]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("Check() = %+v, want no issues — model m opted out of fallbacks", issues)
	}
}

// TestCheckCleanConfig_ProvidersAndFallbacks_NoIssues is the non-duplicate
// happy path for both new shapes together, mirroring
// TestCheckCleanConfigHasNoIssues for the pre-existing single-provider case.
func TestCheckCleanConfig_ProvidersAndFallbacks_NoIssues(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://example.com}, api_key: k2}
fallback_endpoints:
  - {protocol: openai-completions, providers: [p2], models: [y], priority: 90}
models:
  m:
    endpoints:
      - {protocol: openai-completions, providers: [p1, p2], models: [x]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("Check() = %+v, want empty", issues)
	}
}
