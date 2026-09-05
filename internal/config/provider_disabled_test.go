// Ver 2026-09-16, by Pkg-C
package config

import (
	"strings"
	"testing"
)

const disabledYAML = `
listen: 127.0.0.1:0
providers:
  - name: p_on
    base_url: {openai-completions: https://p-on.example.com}
    api_key: k-on
  - name: p_off
    base_url: {openai-completions: https://p-off.example.com}
    api_key: k-off
    disabled: true
  - name: p_parked
    base_url: {openai-completions: https://p-parked.example.com}
    api_key: k-parked
    disabled: true
models:
  m: {endpoints: {openai-completions: [{providers: [p_on, p_off], models: [x]}]}}
`

// TestDisabled_ReferencedStillValidatesButWarns pins the headline
// acceptance: a disabled provider referenced by an endpoint group loads
// cleanly (the reference is intentional, not a typo — validate() must not
// turn the switch into a config error) but Check() says out loud that the
// reference currently carries no traffic.
func TestDisabled_ReferencedStillValidatesButWarns(t *testing.T) {
	cfg := mustParse(t, disabledYAML)
	issues := cfg.Check()
	var found []Issue
	for _, is := range issues {
		if is.Provider == "p_off" && is.Field == "disabled" {
			found = append(found, is)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Check() = %+v, want exactly one disabled-reference issue for p_off", issues)
	}
	if found[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want SeverityWarning — a disabled reference must never fail `vmr check`", found[0].Severity)
	}
	if !strings.Contains(found[0].Message, "carries no traffic until re-enabled") {
		t.Errorf("message = %q, want the carries-no-traffic wording", found[0].Message)
	}
	if HasErrors(issues) {
		t.Errorf("HasErrors must be false for a disabled-reference-only issue set")
	}
}

// TestDisabled_FallbackReferenceWarned covers the fallback_endpoints half
// of the reference scan — a fallback entry naming a disabled provider is
// just as silent as an endpoint-group one.
func TestDisabled_FallbackReferenceWarned(t *testing.T) {
	yaml := strings.Replace(disabledYAML, "providers: [p_on, p_off]", "providers: [p_on]", 1)
	yaml = strings.Replace(yaml,
		"models:\n  m:",
		"fallback_endpoints:\n  openai-completions:\n    - {providers: [p_off], models: [fb], priority: 90}\nmodels:\n  m:", 1)
	cfg := mustParse(t, yaml)
	issues := cfg.Check()
	for _, is := range issues {
		if is.Provider == "p_off" && is.Field == "disabled" {
			if is.Severity != SeverityWarning || !strings.Contains(is.Message, "fallback_endpoints") {
				t.Errorf("issue = %+v, want a warning naming fallback_endpoints", is)
			}
			return
		}
	}
	t.Errorf("Check() = %+v, want a disabled-reference issue for p_off from the fallback entry", issues)
}

// TestDisabled_SkipsAPIKeyCheck: an offline account missing its credential
// (or stripped of it on purpose) must not drown `vmr check` in api_key
// noise on top of a deliberate takedown.
func TestDisabled_SkipsAPIKeyCheck(t *testing.T) {
	yaml := strings.Replace(disabledYAML, "    api_key: k-off\n    disabled: true", "    disabled: true", 1)
	cfg := mustParse(t, yaml) // validate() still passes — api_key was never a structural requirement
	for _, is := range cfg.Check() {
		if is.Provider == "p_off" && is.Field == "api_key" {
			t.Errorf("unexpected api_key issue for disabled provider: %+v", is)
		}
	}
}

// TestDisabled_EnabledProviderStillChecked is the control half: the skip
// must key off the provider's own flag, not leak to its neighbors.
func TestDisabled_EnabledProviderStillChecked(t *testing.T) {
	yaml := strings.Replace(disabledYAML, "api_key: k-on", "api_key: \"\"", 1)
	cfg := mustParse(t, yaml)
	found := false
	for _, is := range cfg.Check() {
		if is.Provider == "p_on" && is.Field == "api_key" {
			found = true
		}
	}
	if !found {
		t.Errorf("enabled provider p_on missing api_key must still be reported")
	}
}

// TestDisabled_UnreferencedIsSilent pins the status quo: a disabled
// provider referenced nowhere is just an offline account parked in config
// — no new issue beyond what a disabled-but-referenced one already gets
// (here: none at all).
func TestDisabled_UnreferencedIsSilent(t *testing.T) {
	yaml := strings.Replace(disabledYAML, "providers: [p_on, p_off]", "providers: [p_on]", 1)
	cfg := mustParse(t, yaml)
	for _, is := range cfg.Check() {
		if is.Provider == "p_off" || is.Provider == "p_parked" {
			t.Errorf("unreferenced disabled provider %q got an issue: %+v", is.Provider, is)
		}
	}
}

// TestDisabled_APIKeysExpansionInherits pins that the disabled flag flows
// into every expanded sub-account: expandProviderAPIKeys copies the parent
// struct per label, so no per-child code is needed — but the inheritance
// is load-bearing for the whole feature (disabling "the provider" must
// disable all its keys, not just the parent entry), so it's pinned here.
func TestDisabled_APIKeysExpansionInherits(t *testing.T) {
	yaml := strings.Replace(disabledYAML,
		"  - name: p_off\n    base_url: {openai-completions: https://p-off.example.com}\n    api_key: k-off\n    disabled: true",
		"  - name: p_off\n    base_url: {openai-completions: https://p-off.example.com}\n    api_keys:\n      a: sk-aaaaaaaaaaaaaaaa\n      b: sk-bbbbbbbbbbbbbbbb\n    disabled: true", 1)
	cfg := mustParse(t, yaml)
	for _, name := range []string{"p_off-a", "p_off-b"} {
		p, ok := byName(cfg.Providers, name)
		if !ok {
			t.Fatalf("expanded sub-provider %q not found in %v", name, cfg.Providers)
		}
		if !p.Disabled {
			t.Errorf("expanded sub-provider %q has Disabled=false, want inherited true", name)
		}
	}
	// And the reference rewrite means the check sees the expanded names.
	for _, is := range cfg.Check() {
		if is.Field == "disabled" && is.Provider != "p_off-a" && is.Provider != "p_off-b" && is.Provider != "p_parked" {
			t.Errorf("unexpected disabled-reference issue for %q", is.Provider)
		}
	}
}
