// Ver 2026-08-02, by Sonnet 5
package config

import (
	"encoding/json"
	"strings"
	"testing"
)

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
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("clean config: Check() = %v, want empty", issues)
	}
}

// TestCheckWarnsOnStreamingUsageInvisibility: a token/cost quota account on
// an openai-completions endpoint gets a SeverityWarning about the
// include_usage gap — the flag appears nowhere else in vmr, so this is the
// only place an operator learns their estimated_pct will climb.
func TestCheckWarnsOnStreamingUsageInvisibility(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: https://example.com}
    api_key: k1
    quota:
      limits:
        - {metric: tokens, every: 1d, amount: 1000000}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`)
	issues := cfg.Check()
	var got *Issue
	for i := range issues {
		if issues[i].Field == "quota" {
			got = &issues[i]
		}
	}
	if got == nil {
		t.Fatalf("Check() = %+v, want a quota usage-visibility warning", issues)
	}
	if got.Severity != SeverityWarning || got.Provider != "p1" || !strings.Contains(got.Message, "include_usage") {
		t.Errorf("issue = %+v, want a SeverityWarning for p1 mentioning include_usage", *got)
	}
	if HasErrors(issues) {
		t.Errorf("a usage-visibility warning must not make HasErrors true: %+v", issues)
	}
}

// TestCheckNoUsageWarningForAnthropic: anthropic-messages always reports
// usage, so a token quota on that protocol raises nothing.
func TestCheckNoUsageWarningForAnthropic(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {anthropic-messages: https://example.com}
    api_key: k1
    quota:
      limits:
        - {metric: tokens, every: 1d, amount: 1000000}
models:
  m: {endpoints: [{protocol: anthropic-messages, providers: [p1], models: [x]}]}
`)
	for _, is := range cfg.Check() {
		if is.Field == "quota" {
			t.Errorf("unexpected quota warning for an anthropic-messages endpoint: %+v", is)
		}
	}
}

// TestCheckFlagsMissingAPIKey ensures an empty provider api_key is
// reported — validate() accepts it (syntactically valid YAML), so this is
// exactly the gap Check exists to cover.
func TestCheckFlagsMissingAPIKey(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: ""}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
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
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Field != "probe_timeout" {
		t.Errorf("Check() = %+v, want exactly one probe_timeout issue", issues)
	}
}

// TestCheckFlagsNonLoopbackListenWithNoAPIKeys pins the fix for a finding
// from the 2026-08-12 review (VMR_项目全面Review报告 A3): validate() only
// checks that listen is a syntactically valid host:port, so a config that
// binds a non-loopback address with no api_keys configured loaded cleanly
// and ran as a silent open proxy holding every configured upstream
// credential — exactly the gap Check exists to cover. Severity must be
// SeverityWarning, not the default SeverityError: this is a risk worth
// surfacing, not a broken config — a first version of this check used the
// zero-value severity and ended up blocking `vmr check`/`vmr diagnose`
// entirely for a config that's fully intentional (see HasErrors' doc
// comment and TestCheckAllowsNonLoopbackListenWithAPIKeys below for the
// isn't-actually-broken half of that fix).
func TestCheckFlagsNonLoopbackListenWithNoAPIKeys(t *testing.T) {
	cfg := mustParse(t, `
listen: 0.0.0.0:8800
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Field != "listen" {
		t.Errorf("Check() = %+v, want exactly one listen issue", issues)
	}
	if issues[0].Severity != SeverityWarning {
		t.Errorf("listen exposure issue Severity = %v, want SeverityWarning — it must never fail vmr check or gate vmr diagnose", issues[0].Severity)
	}
	if HasErrors(issues) {
		t.Errorf("HasErrors(%+v) = true, want false — a SeverityWarning-only issue set must not read as errors", issues)
	}
}

// TestCheckAllowsNonLoopbackListenWithAPIKeys is the positive counterpart:
// a non-loopback listen address is fine once api_keys actually gates access
// to it — the risk checkListenExposure exists for is credentials-free
// exposure, not remote listening itself.
func TestCheckAllowsNonLoopbackListenWithAPIKeys(t *testing.T) {
	cfg := mustParse(t, `
listen: 0.0.0.0:8800
api_keys: [sixteen-plus-chars]
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("non-loopback listen with api_keys configured: Check() = %v, want empty", issues)
	}
}

// TestCheckAllowsLoopbackListenWithNoAPIKeys is the other positive
// counterpart: the default 127.0.0.1 bind with no api_keys is the
// documented, intentional "single local user, no auth" mode — must not
// trip checkListenExposure just because api_keys is empty.
func TestCheckAllowsLoopbackListenWithNoAPIKeys(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:8800
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`)
	if issues := cfg.Check(); len(issues) != 0 {
		t.Errorf("loopback listen with no api_keys: Check() = %v, want empty", issues)
	}
}

// TestCheckFlagsDuplicateEndpoint covers a copy-paste mistake validate()
// has no reason to reject (each individual field is valid) but that's dead
// weight at best and a config author's mistake at worst.
func TestCheckFlagsDuplicateEndpoint(t *testing.T) {
	cfg := mustParse(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [x, x]}
`)
	issues := cfg.Check()
	if len(issues) != 1 || issues[0].Model != "m" || issues[0].Field != "endpoint" || issues[0].Endpoint != "openai-completions/p1/x" {
		t.Errorf("Check() = %+v, want exactly one duplicate-endpoint issue for openai-completions/p1/x", issues)
	}
}

// TestCheckIssueJSON verifies that Issue and Severity serialize to and deserialize from JSON cleanly.
func TestCheckIssueJSON(t *testing.T) {
	issue := Issue{
		Field:    "listen",
		Message:  "listen on 0.0.0.0 is exposed",
		Severity: SeverityWarning,
	}
	data, err := json.Marshal(issue)
	if err != nil {
		t.Fatalf("Marshal Issue: %v", err)
	}
	var got Issue
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal Issue: %v", err)
	}
	if got.Severity != SeverityWarning || got.Field != "listen" || got.Message != issue.Message {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, issue)
	}
	if !strings.Contains(string(data), `"severity":"warning"`) {
		t.Errorf("json %s does not contain severity string 'warning'", string(data))
	}
}
