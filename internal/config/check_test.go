// Ver 2026-07-30, by Sonnet 5
package config

import "testing"

func mustParse(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cfg
}

// TestCheckCleanConfigHasNoIssues locks in that a config with nothing
// operationally wrong reports zero Issues — Check must not false-positive
// on the common case validate() already accepts.
func TestCheckCleanConfigHasNoIssues(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("clean config: Check() = %v, want empty", issues)
	}
}

// TestCheckFlagsMissingAPIKey ensures an empty provider api_key is
// reported — validate() accepts it (syntactically valid YAML), so this is
// exactly the gap Check exists to cover.
func TestCheckFlagsMissingAPIKey(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: ""}
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Provider != "p1" || issues[0].Field != "api_key" {
		t.Errorf("Check() = %+v, want exactly one api_key issue for provider p1", issues)
	}
}

// TestCheckFlagsInheritedProxySchemeMismatch covers the gap validate()
// leaves open: a provider inheriting the global proxy: true default (no
// explicit per-provider switch — that case IS caught by validate()) whose
// base_url scheme has no matching proxy URL configured, so ProxySpecFor
// silently resolves to direct instead of proxying like the config intends.
func TestCheckFlagsInheritedProxySchemeMismatch(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
proxy: true
https_proxy: http://127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai: http://10.0.0.1:8000}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Provider != "p1" || issues[0].Field != "proxy" {
		t.Errorf("Check() = %+v, want exactly one proxy issue for provider p1 (https_proxy set but base_url is http)", issues)
	}

	// The matching-scheme case must NOT be flagged.
	cfg2 := mustParse(t, `
listen: 127.0.0.1:0
proxy: true
https_proxy: http://127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`)
	if issues := cfg2.Check(); len(issues) != 0 {
		t.Errorf("matching-scheme proxy: Check() = %v, want empty", issues)
	}
}

// TestCheckFlagsProbeTimeoutNotUnderResponseHeader locks in the
// active-probe budget invariant from DefaultProbeTimeout's doc comment: a
// probe_timeout at or above response_header defeats the "never make real
// traffic wait on a probe" guarantee. Passive mode never uses
// ProbeTimeout, so the same values must not be flagged there.
func TestCheckFlagsProbeTimeoutNotUnderResponseHeader(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
probe_timeout: 130s
timeouts: {response_header: 120s}
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Field != "probe_timeout" {
		t.Errorf("Check() = %+v, want exactly one probe_timeout issue", issues)
	}

	cfg2 := mustParse(t, `
listen: 127.0.0.1:0
probe_mode: passive
probe_timeout: 130s
timeouts: {response_header: 120s}
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`)
	if issues := cfg2.Check(); len(issues) != 0 {
		t.Errorf("passive mode: Check() = %v, want empty (probe_timeout unused)", issues)
	}
}

// TestCheckFlagsDuplicateEndpoint covers a copy-paste mistake validate()
// has no reason to reject (each individual field is valid) but that's dead
// weight at best and a config author's mistake at worst.
func TestCheckFlagsDuplicateEndpoint(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  m:
    endpoints:
      - {protocol: openai, provider: p1, models: [x, x]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Model != "m" || issues[0].Field != "endpoint" || issues[0].Endpoint != "openai/p1/x" {
		t.Errorf("Check() = %+v, want exactly one duplicate-endpoint issue for openai/p1/x", issues)
	}
}
