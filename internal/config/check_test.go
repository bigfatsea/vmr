// Ver 2026-08-02, by Sonnet 5
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

// TestCheckFlagsProbeTimeoutNotUnderResponseHeader locks in the
// background-probe budget invariant from DefaultProbeTimeout's doc comment:
// a probe_timeout at or above response_header defeats the "never make real
// traffic wait on a probe" guarantee.
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
