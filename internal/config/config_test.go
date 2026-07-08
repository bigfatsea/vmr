// Ver 2026-07-08 20:15, by Sonnet 5
package config

import (
	"strings"
	"testing"
	"time"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

const validYAML = `
listen: 127.0.0.1:9900
providers:
  openai:
    p1:
      base_url: https://api.example.com/v1
      api_key: ${VMR_TEST_KEY}
models:
  openai:
    m1:
      endpoints:
        - provider: p1
          model: real-model
          priority: 1
`

func TestParseDefaultsAndEnvExpansion(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers["openai"]["p1"].APIKey; got != "sk-test-123" {
		t.Errorf("env expansion: got %q", got)
	}
	if cfg.MaxAttempts != 0 || cfg.MaxBodyMB != 8 {
		t.Errorf("defaults: attempts=%d (want 0 = unlimited) body=%d", cfg.MaxAttempts, cfg.MaxBodyMB)
	}
	if cfg.ImageDownscaleMaxPx != 0 {
		t.Errorf("default image_downscale: got %d, want 0 (disabled)", cfg.ImageDownscaleMaxPx)
	}
	if cfg.Timeouts.Connect.D() != 10*time.Second {
		t.Errorf("default connect timeout: %v", cfg.Timeouts.Connect.D())
	}
	if got := cfg.Models["openai"]["m1"].Strategy; len(got) != 1 || got[0] != "priority" {
		t.Errorf("default strategy: %v", got)
	}
}

func TestUnsetEnvExpandsEmpty(t *testing.T) {
	cfg, err := Parse([]byte(strings.Replace(validYAML, "${VMR_TEST_KEY}", "${VMR_DEFINITELY_UNSET_VAR}", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["openai"]["p1"].APIKey != "" {
		t.Errorf("unset env should expand to empty, got %q", cfg.Providers["openai"]["p1"].APIKey)
	}
}

func TestCustomTimeouts(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\ntimeouts:\n  connect: 3s\n  response_header: 30s", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeouts.Connect.D() != 3*time.Second || cfg.Timeouts.ResponseHeader.D() != 30*time.Second {
		t.Errorf("timeouts: %+v", cfg.Timeouts)
	}
	if cfg.Timeouts.StreamIdle.D() != 120*time.Second {
		t.Errorf("stream_idle default: %v", cfg.Timeouts.StreamIdle.D())
	}
}

func TestImageDownscaleConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nimage_downscale: 512", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageDownscaleMaxPx != 512 {
		t.Errorf("image_downscale: got %d, want 512", cfg.ImageDownscaleMaxPx)
	}
}

func TestImageDownscaleNegativeClampsToDisabled(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nimage_downscale: -1", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageDownscaleMaxPx != 0 {
		t.Errorf("negative image_downscale must clamp to 0 (disabled), got %d", cfg.ImageDownscaleMaxPx)
	}
}

func TestMaxConcurrencyNegativeClampsToUnlimited(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nmax_concurrency: -3", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConcurrency != 0 {
		t.Errorf("negative max_concurrency must clamp to 0 (unlimited), got %d", cfg.MaxConcurrency)
	}
}

func TestAuditRetentionDaysDefaultsToDisabled(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditRetentionDays != 0 {
		t.Errorf("audit_retention_days: default must be 0 (never delete), got %d", cfg.AuditRetentionDays)
	}
}

func TestAuditRetentionDaysConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\naudit_retention_days: 30", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditRetentionDays != 30 {
		t.Errorf("audit_retention_days: got %d, want 30", cfg.AuditRetentionDays)
	}
}

func TestAuditRetentionDaysNegativeClampsToDisabled(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\naudit_retention_days: -5", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditRetentionDays != 0 {
		t.Errorf("negative audit_retention_days must clamp to 0 (disabled), got %d", cfg.AuditRetentionDays)
	}
}

// TestPriorityOmittedUsesFileOrder documents and locks in the idiom this
// schema is built around: skip "priority" entirely and just list endpoints
// in the order they should be tried. Sort is stable, so endpoints tying at
// the zero-value default keep exactly that order — this is not a fallback
// behavior, it's the intended everyday usage.
func TestPriorityOmittedUsesFileOrder(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9901
providers:
  openai:
    p:
      base_url: https://api.example.com/v1
      api_key: k
models:
  openai:
    m:
      endpoints:
        - {provider: p, model: third}
        - {provider: p, model: first}
        - {provider: p, model: second}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	eps := cfg.Models["openai"]["m"].Endpoints
	if len(eps) != 3 || eps[0].Model != "third" || eps[1].Model != "first" || eps[2].Model != "second" {
		t.Errorf("endpoints must keep file order when priority is omitted: %+v", eps)
	}
	for _, ep := range eps {
		if ep.Priority != 0 {
			t.Errorf("omitted priority must default to 0, got %d for %s", ep.Priority, ep.Model)
		}
	}
}

// TestSameProviderNameAcrossProtocols is the whole point of nesting
// providers/models by protocol: the same short name can be reused once per
// protocol group (e.g. the same OpenRouter account's OpenAI-compatible and
// Anthropic-compatible surfaces), no "_a" suffix hack required.
func TestSameProviderNameAcrossProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9902
providers:
  openai:
    openrouter: {base_url: https://openrouter.ai/api/v1, api_key: k1}
  anthropic:
    openrouter: {base_url: https://openrouter.ai/api/v1, api_key: k1}
models:
  openai:
    coding: {endpoints: [{provider: openrouter, model: z-ai/glm-5.2}]}
  anthropic:
    coding: {endpoints: [{provider: openrouter, model: minimax/minimax-m3}]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["openai"]["openrouter"].BaseURL == "" || cfg.Providers["anthropic"]["openrouter"].BaseURL == "" {
		t.Fatal("both protocol-scoped providers must parse")
	}
	if cfg.Models["openai"]["coding"].Endpoints[0].Model != "z-ai/glm-5.2" {
		t.Error("openai-face coding model mismatch")
	}
	if cfg.Models["anthropic"]["coding"].Endpoints[0].Model != "minimax/minimax-m3" {
		t.Error("anthropic-face coding model mismatch")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, mutate, wantErr string
	}{
		{"bad base_url", "base_url: https://api.example.com/v1", "invalid base_url"},
		{"unknown provider ref", "provider: p1", "unknown provider"},
		{"missing model", "model: real-model", "missing model"},
	}
	replacements := map[string]string{
		"bad base_url":         "base_url: not-a-url",
		"unknown provider ref": "provider: ghost",
		"missing model":        `model: ""`,
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml := strings.Replace(validYAML, c.mutate, replacements[c.name], 1)
			_, err := Parse([]byte(yaml))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestUnknownProtocolKeyRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "providers:\n  openai:", "providers:\n  nosuch:", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown adapter type") {
		t.Errorf("want unknown adapter type error, got %v", err)
	}
}

func TestEmptySections(t *testing.T) {
	if _, err := Parse([]byte("listen: 127.0.0.1:1\nmodels: {openai: {m: {endpoints: [{provider: x, model: y}]}}}")); err == nil {
		t.Error("want error for no providers")
	}
	if _, err := Parse([]byte("providers: {openai: {p: {base_url: https://x.com}}}")); err == nil {
		t.Error("want error for no models")
	}
}
