// Ver 2026-09-14, by Sonnet 5

// BuildSnapshot coverage for Agent Guard's M3.5 trusted-providers
// precondition (the Agent Guard spec §4.3):
// ModelRoute.GuardAllTrusted, computed once here rather than per-request,
// since the entry point doesn't yet know which candidate Failover will
// land on.
package router

import (
	"testing"

	"vmr/internal/config"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

// snapshotFromYAML parses yaml and builds its Snapshot in one step --
// this file's own helper (distinct from router_serve_test.go's
// mustSnapshot, which takes an already-parsed *config.Config) since every
// test below only needs a one-shot YAML-to-Snapshot path.
func snapshotFromYAML(t *testing.T, yaml string) *Snapshot {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	return snap
}

func TestBuildSnapshot_GuardAllTrusted_AbsentGuardConfig(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`)
	if snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("GuardAllTrusted = true, want false when guard: is absent entirely")
	}
}

func TestBuildSnapshot_GuardAllTrusted_EmptyTrustedProvidersList(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
guard:
  outbound: {mode: audit_only}
`)
	if snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("GuardAllTrusted = true, want false when trusted_providers is empty (never a default-on exemption)")
	}
}

func TestBuildSnapshot_GuardAllTrusted_SingleTrustedCandidate(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
guard:
  trusted_providers: [p1]
`)
	if !snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("GuardAllTrusted = false, want true when the route's only candidate is trusted")
	}
}

func TestBuildSnapshot_GuardAllTrusted_OneUntrustedCandidateSpoilsIt(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://p2.example.com}, api_key: k2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
guard:
  trusted_providers: [p1]
`)
	if snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("GuardAllTrusted = true, want false -- p2 is not in trusted_providers (§4.3: any untrusted candidate spoils the exemption)")
	}
}

// TestBuildSnapshot_GuardAllTrusted_FallbackEndpointCounts confirms a
// virtual model's injected fallback_endpoints candidates count toward the
// "all candidates" set too -- an untrusted fallback provider must spoil
// the exemption exactly like an untrusted primary one.
func TestBuildSnapshot_GuardAllTrusted_FallbackEndpointCounts(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://p2.example.com}, api_key: k2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
fallback_endpoints:
  openai-completions:
    - {providers: [p2], models: [m2], priority: 90}
guard:
  trusted_providers: [p1]
`)
	if snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("GuardAllTrusted = true, want false -- the injected fallback candidate p2 is untrusted")
	}
}

func TestBuildSnapshot_GuardAllTrusted_AllCandidatesTrustedIncludingFallback(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://p2.example.com}, api_key: k2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
fallback_endpoints:
  openai-completions:
    - {providers: [p2], models: [m2], priority: 90}
guard:
  trusted_providers: [p1, p2]
`)
	if !snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("GuardAllTrusted = false, want true when every candidate, fallback included, is trusted")
	}
}

// TestBuildSnapshot_GuardAllTrusted_PerProtocolIndependent: a virtual
// model reachable from two protocols with different candidate sets is
// judged independently per (protocol, model) route -- ModelRoute is
// already split that way, so this pins that GuardAllTrusted doesn't
// accidentally get shared/aliased across a model's two routes.
func TestBuildSnapshot_GuardAllTrusted_PerProtocolIndependent(t *testing.T) {
	snap := snapshotFromYAML(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com, anthropic-messages: https://p1.example.com}, api_key: k1}
  - {name: p2, base_url: {anthropic-messages: https://p2.example.com}, api_key: k2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
      anthropic-messages:
        - {providers: [p1], models: [m1]}
        - {providers: [p2], models: [m2]}
guard:
  trusted_providers: [p1]
`)
	if !snap.Models["openai-completions"]["vm"].GuardAllTrusted {
		t.Error("openai-completions route: GuardAllTrusted = false, want true (its only candidate, p1, is trusted)")
	}
	if snap.Models["anthropic-messages"]["vm"].GuardAllTrusted {
		t.Error("anthropic-messages route: GuardAllTrusted = true, want false (p2 is an untrusted candidate on this route)")
	}
}
