// Ver 2026-09-05, by Pkg-E
package config

import (
	"strings"
	"testing"

	_ "vmr/internal/adapter/openai"
)

func TestModelDefaults_EntireBlockOmittedIsUnconstrained(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [real-model]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.ModelDefaults) != 0 {
		t.Errorf("expected empty ModelDefaults, got %+v", cfg.ModelDefaults)
	}
}

func TestModelDefaults_Parsed(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
model_defaults:
  "*":
    capabilities: [text, tools]
  MiniMax-M3:
    capabilities: [text, tools, image, audio, video, thinking]
    max_context_tokens: 512000
    providers: [openrouter, minimax]
providers:
  - name: openrouter
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test1
  - name: minimax
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test2
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [openrouter]
          models: [MiniMax-M3]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.ModelDefaults) != 2 {
		t.Fatalf("expected 2 ModelDefaults entries, got %d", len(cfg.ModelDefaults))
	}
	star, ok := cfg.ModelDefaults["*"]
	if !ok {
		t.Fatal("missing '*' wildcard entry")
	}
	if len(star.Capabilities) != 2 || star.Capabilities[0] != "text" || star.Capabilities[1] != "tools" {
		t.Errorf("wildcard capabilities = %v, want [text tools]", star.Capabilities)
	}
	mm, ok := cfg.ModelDefaults["MiniMax-M3"]
	if !ok {
		t.Fatal("missing MiniMax-M3 entry")
	}
	if mm.MaxContextTokens != 512000 {
		t.Errorf("max_context_tokens = %d, want 512000", mm.MaxContextTokens)
	}
	if len(mm.Providers) != 2 || mm.Providers[0] != "openrouter" || mm.Providers[1] != "minimax" {
		t.Errorf("providers = %v, want [openrouter minimax]", mm.Providers)
	}
}

func TestModelDefaults_UnknownProviderRejected(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
model_defaults:
  MiniMax-M3:
    capabilities: [text]
    providers: [unknown-provider]
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [MiniMax-M3]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown provider in model_defaults.providers")
	}
	if !strings.Contains(err.Error(), "unknown provider \"unknown-provider\"") {
		t.Errorf("error should name the unknown provider, got: %v", err)
	}
}

func TestModelDefaults_EmptyProvidersListRejected(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
model_defaults:
  MiniMax-M3:
    capabilities: [text]
    providers: []
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [MiniMax-M3]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for empty providers list in model_defaults")
	}
	if !strings.Contains(err.Error(), "providers must not be empty when specified") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestModelDefaults_NegativeMaxContextTokensRejected(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
model_defaults:
  MiniMax-M3:
    max_context_tokens: -1
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [MiniMax-M3]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for negative max_context_tokens")
	}
	if !strings.Contains(err.Error(), "max_context_tokens must be >= 0") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestModelDefaults_APIKeysExpansionRewritesProviders(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
model_defaults:
  MiniMax-M3:
    capabilities: [text, image]
    max_context_tokens: 512000
    providers: [multi]
providers:
  - name: multi
    base_url: {openai-completions: https://example.com/v1}
    api_keys:
      k1: sk-1111111111111111
      k2: sk-2222222222222222
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [multi]
          models: [MiniMax-M3]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse with APIKeys expansion in model_defaults: %v", err)
	}
	mm := cfg.ModelDefaults["MiniMax-M3"]
	if len(mm.Providers) != 2 {
		t.Fatalf("expected 2 expanded providers in model_defaults, got %v", mm.Providers)
	}
	hasK1, hasK2 := false, false
	for _, p := range mm.Providers {
		if p == "multi-k1" {
			hasK1 = true
		}
		if p == "multi-k2" {
			hasK2 = true
		}
	}
	if !hasK1 || !hasK2 {
		t.Errorf("model_defaults providers should be expanded to multi-k1 and multi-k2, got %v", mm.Providers)
	}
}

func TestEndpointGroup_CapabilitiesOrMaxContextTokensRejected(t *testing.T) {
	// Endpoint-level overrides were removed; specifying them is now an unknown field error.
	capYAML := `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [real-model]
          capabilities: [text]
`
	if _, err := Parse([]byte(capYAML)); err == nil || !strings.Contains(err.Error(), "capabilities") {
		t.Errorf("endpoint-level capabilities should be rejected as unknown field: %v", err)
	}

	ctxYAML := `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: sk-test
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [real-model]
          max_context_tokens: 128000
`
	if _, err := Parse([]byte(ctxYAML)); err == nil || !strings.Contains(err.Error(), "max_context_tokens") {
		t.Errorf("endpoint-level max_context_tokens should be rejected as unknown field: %v", err)
	}
}
