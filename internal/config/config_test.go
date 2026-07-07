// Ver 2026-07-07 02:20, by Fable 5
package config

import (
	"strings"
	"testing"
	"time"

	_ "vmr/internal/adapter/openai"
)

const validYAML = `
listen: 127.0.0.1:9900
providers:
  p1:
    type: openai
    base_url: https://api.example.com/v1
    api_key: ${VMR_TEST_KEY}
models:
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
	if got := cfg.Providers["p1"].APIKey; got != "sk-test-123" {
		t.Errorf("env expansion: got %q", got)
	}
	if cfg.MaxAttempts != 0 || cfg.MaxBodyMB != 8 {
		t.Errorf("defaults: attempts=%d (want 0 = unlimited) body=%d", cfg.MaxAttempts, cfg.MaxBodyMB)
	}
	if cfg.Timeouts.Connect.D() != 10*time.Second {
		t.Errorf("default connect timeout: %v", cfg.Timeouts.Connect.D())
	}
	if got := cfg.Models["m1"].Strategy; len(got) != 1 || got[0] != "priority" {
		t.Errorf("default strategy: %v", got)
	}
}

func TestUnsetEnvExpandsEmpty(t *testing.T) {
	cfg, err := Parse([]byte(strings.Replace(validYAML, "${VMR_TEST_KEY}", "${VMR_DEFINITELY_UNSET_VAR}", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["p1"].APIKey != "" {
		t.Errorf("unset env should expand to empty, got %q", cfg.Providers["p1"].APIKey)
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

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, mutate, wantErr string
	}{
		{"bad adapter type", "type: openai", "unknown adapter type"},
		{"bad base_url", "base_url: https://api.example.com/v1", "invalid base_url"},
		{"unknown provider ref", "provider: p1", "unknown provider"},
		{"missing model", "model: real-model", "missing model"},
	}
	replacements := map[string]string{
		"bad adapter type":     "type: nosuch",
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

func TestEmptySections(t *testing.T) {
	if _, err := Parse([]byte("listen: 127.0.0.1:1\nmodels: {m: {endpoints: [{provider: x, model: y}]}}")); err == nil {
		t.Error("want error for no providers")
	}
	if _, err := Parse([]byte("providers: {p: {type: openai, base_url: https://x.com}}")); err == nil {
		t.Error("want error for no models")
	}
}
