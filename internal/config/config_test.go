// Ver 2026-08-02, by Sonnet 5
package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
	_ "vmr/internal/adapter/openairesponses"
)

const validYAML = `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1}
    api_key: ${VMR_TEST_KEY}
models:
  m1:
    endpoints:
      - protocol: openai
        provider: p1
        models: [real-model]
        priority: 1
`

func TestParseDefaultsAndEnvExpansion(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.ProviderByName("p1")
	if !ok || p.APIKey != "sk-test-123" {
		t.Errorf("env expansion: got %q", p.APIKey)
	}
	if cfg.MaxAttempts != 0 || cfg.MaxRequestBodyMB != 8 {
		t.Errorf("defaults: attempts=%d (want 0 = unlimited) body=%d", cfg.MaxAttempts, cfg.MaxRequestBodyMB)
	}
	if cfg.ImageDownscaleMaxPx != 0 {
		t.Errorf("default image_downscale: got %d, want 0 (disabled)", cfg.ImageDownscaleMaxPx)
	}
	if cfg.Timeouts.Connect.D() != 10*time.Second {
		t.Errorf("default connect timeout: %v", cfg.Timeouts.Connect.D())
	}
	if got := cfg.Models["m1"].Strategy; len(got) != 1 || got[0] != "priority" {
		t.Errorf("default strategy: %v", got)
	}
	if cfg.ProbeTimeout.D() != DefaultProbeTimeout {
		t.Errorf("default probe_timeout: got %v, want %v", cfg.ProbeTimeout.D(), DefaultProbeTimeout)
	}
}

func TestProbeTimeoutConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nprobe_timeout: 5s", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProbeTimeout.D() != 5*time.Second {
		t.Errorf("probe_timeout: got %v, want 5s", cfg.ProbeTimeout.D())
	}
}

// TestProbeModeFieldRejected locks in the removal of probe_mode (passive
// mode no longer exists — recovery probing is always the active,
// backgrounded kind): a config still setting it must fail to load as an
// unknown field, same as any other typo, with no dedicated migration
// message needed.
func TestProbeModeFieldRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nprobe_mode: active", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "probe_mode") {
		t.Errorf("want load error naming probe_mode, got %v", err)
	}
}

func TestUnsetEnvExpandsEmpty(t *testing.T) {
	cfg, err := Parse([]byte(strings.Replace(validYAML, "${VMR_TEST_KEY}", "${VMR_DEFINITELY_UNSET_VAR}", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := cfg.ProviderByName("p1"); p.APIKey != "" {
		t.Errorf("unset env should expand to empty, got %q", p.APIKey)
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

// TestRoleMapConfig/TestRoleMapUnsetIsNil pin role_map's new home: per
// endpoint-group (models.<name>.endpoints[].role_map), not per provider —
// the same account can back several endpoint-groups with different upstream
// model families, not all of which necessarily need the same role rewrite.
func TestRoleMapConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n        role_map:\n          developer: system", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["m1"].Endpoints[0].RoleMap
	if len(got) != 1 || got["developer"] != "system" {
		t.Errorf("role_map: got %v, want map[developer:system]", got)
	}
}

func TestRoleMapUnsetIsNil(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Models["m1"].Endpoints[0].RoleMap; got != nil {
		t.Errorf("role_map should be nil when omitted, got %v", got)
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

// TestModelImageDownscaleUnsetInheritsGlobal documents that a model with no
// image_downscale key parses to a nil pointer — the signal BuildSnapshot and
// ModelRoute.EffectiveImageDownscaleMaxPx use to fall back to the global
// setting, as opposed to an explicit 0 which force-disables the feature for
// that model regardless of the global value.
func TestModelImageDownscaleUnsetInheritsGlobal(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Models["m1"].ImageDownscaleMaxPx; got != nil {
		t.Errorf("unset per-model image_downscale must parse to nil (inherit global), got %v", *got)
	}
}

func TestModelImageDownscaleOverride(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    image_downscale: 256\n    endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["m1"].ImageDownscaleMaxPx
	if got == nil || *got != 256 {
		t.Errorf("model image_downscale override: got %v, want 256", got)
	}
}

// TestModelImageDownscaleExplicitZeroDiffersFromUnset is the whole point of
// the pointer type: an explicit 0 must remain distinguishable from "not
// set" all the way through parsing, so it can force-disable the feature for
// this model even when the global default is on.
func TestModelImageDownscaleExplicitZeroDiffersFromUnset(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    image_downscale: 0\n    endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["m1"].ImageDownscaleMaxPx
	if got == nil {
		t.Fatal("explicit image_downscale: 0 must not parse to nil (that would mean 'unset')")
	}
	if *got != 0 {
		t.Errorf("explicit image_downscale: 0, got %d", *got)
	}
}

func TestModelImageDownscaleNegativeClampsToZero(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    image_downscale: -1\n    endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["m1"].ImageDownscaleMaxPx
	if got == nil || *got != 0 {
		t.Errorf("negative per-model image_downscale must clamp to 0 (force-disabled), got %v", got)
	}
}

func TestImageCacheTTLDaysDefaultsToSevenDays(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageCacheTTLDays != DefaultImageCacheTTLDays {
		t.Errorf("default image_cache_ttl_days: got %d, want %d", cfg.ImageCacheTTLDays, DefaultImageCacheTTLDays)
	}
}

func TestImageCacheTTLDaysConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nimage_cache_ttl_days: 14", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageCacheTTLDays != 14 {
		t.Errorf("image_cache_ttl_days: got %d, want 14", cfg.ImageCacheTTLDays)
	}
}

// TestImageCacheTTLDaysNonPositiveClampsToDefault differs from
// audit_retention_days's "0 = keep forever" convention on purpose: the image
// cache is a pure performance optimization with no audit/compliance value,
// so silently growing it forever is not a safer default than actively
// pruning it (the design doc's decision table).
func TestImageCacheTTLDaysNonPositiveClampsToDefault(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nimage_cache_ttl_days: "+v, 1)
		cfg, err := Parse([]byte(yaml))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ImageCacheTTLDays != DefaultImageCacheTTLDays {
			t.Errorf("image_cache_ttl_days: %s must clamp to default %d, got %d", v, DefaultImageCacheTTLDays, cfg.ImageCacheTTLDays)
		}
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
  - name: p
    base_url: {openai: https://api.example.com/v1}
    api_key: k
models:
  m:
    endpoints:
      - {protocol: openai, provider: p, models: [third]}
      - {protocol: openai, provider: p, models: [first]}
      - {protocol: openai, provider: p, models: [second]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	eps := cfg.Models["m"].Endpoints
	if len(eps) != 3 || eps[0].Models[0] != "third" || eps[1].Models[0] != "first" || eps[2].Models[0] != "second" {
		t.Errorf("endpoints must keep file order when priority is omitted: %+v", eps)
	}
	for _, ep := range eps {
		if ep.Priority != 0 {
			t.Errorf("omitted priority must default to 0, got %d for %v", ep.Priority, ep.Models)
		}
	}
}

// TestModelsListExpandsToMultipleCandidates is the new format's headline
// feature: one endpoint-group's `models:` list stands in for that many
// EndpointGroup-level try-order entries sharing the same
// provider/protocol/capabilities — config-level this is just "the list
// parses with every name intact and in order"; BuildSnapshot's expansion
// into independent *core.Endpoints is covered in internal/router.
func TestModelsListExpandsToMultipleCandidates(t *testing.T) {
	yaml := strings.Replace(validYAML, "models: [real-model]", "models: [model-a, model-b, model-c]", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"model-a", "model-b", "model-c"}
	got := cfg.Models["m1"].Endpoints[0].Models
	if len(got) != len(want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestProviderServesBothProtocols pins the new Provider shape: one account
// entry declares base_url for both "openai" and "anthropic", sharing one
// api_key/proxy setting, instead of the old format's two separately-keyed
// provider entries under providers.openai/providers.anthropic.
func TestProviderServesBothProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9903
providers:
  - name: dual
    base_url: {openai: https://api.example.com/v1, anthropic: https://api.example.com/anthropic/v1}
    api_key: k1
models:
  m:
    endpoints:
      - {protocol: openai, provider: dual, models: [x]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.ProviderByName("dual")
	if !ok {
		t.Fatal("provider not found")
	}
	if p.BaseURL["openai"] != "https://api.example.com/v1" || p.BaseURL["anthropic"] != "https://api.example.com/anthropic/v1" {
		t.Errorf("base_url map: %v", p.BaseURL)
	}
}

// TestSameVirtualModelNameBothProtocols is the new format's version of "the
// same virtual model name is independently reachable from both ingress
// protocols": one models.<name> entry mixes an openai-protocol endpoint
// group and an anthropic-protocol one. Config-level this only needs to
// confirm both entries parse with their own protocol/provider/models intact
// — BuildSnapshot's split into two independent routes is covered in
// internal/router.
func TestSameVirtualModelNameBothProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9902
providers:
  - name: openrouter
    base_url: {openai: https://openrouter.ai/api/v1, anthropic: https://openrouter.ai/api/v1}
    api_key: k1
models:
  coding:
    endpoints:
      - {protocol: openai, provider: openrouter, models: [z-ai/glm-5.2]}
      - {protocol: anthropic, provider: openrouter, models: [minimax/minimax-m3]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	eps := cfg.Models["coding"].Endpoints
	if len(eps) != 2 {
		t.Fatalf("want 2 endpoint groups, got %d", len(eps))
	}
	if eps[0].Protocol != "openai" || eps[0].Models[0] != "z-ai/glm-5.2" {
		t.Errorf("openai-protocol entry mismatch: %+v", eps[0])
	}
	if eps[1].Protocol != "anthropic" || eps[1].Models[0] != "minimax/minimax-m3" {
		t.Errorf("anthropic-protocol entry mismatch: %+v", eps[1])
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, mutate, replacement, wantErr string
	}{
		{"bad base_url", "base_url: {openai: https://api.example.com/v1}", "base_url: {openai: not-a-url}", "invalid base_url"},
		{"unknown provider ref", "provider: p1", "provider: ghost", "unknown provider"},
		{"empty models list", "models: [real-model]", "models: []", "at least one required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml := strings.Replace(validYAML, c.mutate, c.replacement, 1)
			_, err := Parse([]byte(yaml))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestUnknownProtocolKeyRejected covers a provider's base_url declaring a
// protocol with no registered adapter (e.g. a typo).
func TestUnknownProtocolKeyRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "base_url: {openai: https://api.example.com/v1}", "base_url: {nosuch: https://api.example.com/v1}", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown adapter type") {
		t.Errorf("want unknown adapter type error, got %v", err)
	}
}

// TestUnknownEndpointProtocolRejected covers the new per-endpoint-group
// `protocol:` field itself naming an unregistered adapter — a validation
// path that didn't exist under the old nested-by-protocol schema, where
// protocol was implicit from map position rather than a value that could be
// wrong.
func TestUnknownEndpointProtocolRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "protocol: openai", "protocol: nosuch", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown protocol") {
		t.Errorf("want unknown protocol error, got %v", err)
	}
}

func TestAPIKeysParsed(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\napi_keys:\n  - sk-vmr-team-alice\n  - sk-vmr-team-bobby", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sk-vmr-team-alice", "sk-vmr-team-bobby"}; len(cfg.APIKeys) != len(want) ||
		cfg.APIKeys[0] != want[0] || cfg.APIKeys[1] != want[1] {
		t.Errorf("api_keys: got %v, want %v", cfg.APIKeys, want)
	}
}

// TestLegacyAPIKeyRejected locks in the removal of the singular api_key: a
// config still carrying it must fail to load. No dedicated migration
// message anymore — the field is simply unknown, same as any other typo,
// and KnownFields' own error already names it.
func TestLegacyAPIKeyRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\napi_key: sk-vmr-legacy-catchall", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Errorf("want load error naming api_key, got %v", err)
	}
}

// TestUnknownFieldRejected locks in strict decoding: a misspelled key must be
// a load error, not a silently ignored no-op the user believes is in effect.
func TestUnknownFieldRejected(t *testing.T) {
	cases := []string{
		"max_concurency: 8",       // misspelled top-level field
		"image_downscale_px: 512", // plausible-but-wrong field name
	}
	for _, extra := range cases {
		yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\n"+extra, 1)
		if _, err := Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("%s: want a field-not-found error, got %v", extra, err)
		}
	}
	// Nested typo inside a provider entry is caught too.
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_kye: x", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("nested provider typo accepted")
	}
}

func TestAPIKeysTooShortRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\napi_keys:\n  - sk-short", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("want a too-short api_keys error, got %v", err)
	}
}

func TestExtraRedactHeadersParsed(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nextra_redact_headers:\n  - X-Custom-Token\n  - X-Session-Secret", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"X-Custom-Token", "X-Session-Secret"}; !reflect.DeepEqual(cfg.ExtraRedactHeaders, want) {
		t.Errorf("ExtraRedactHeaders = %v, want %v", cfg.ExtraRedactHeaders, want)
	}
}

func TestExtraRedactHeadersEmptyEntryRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nextra_redact_headers:\n  - \"\"", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "extra_redact_headers[0]") {
		t.Errorf("want an empty-header-name error, got %v", err)
	}
}

func TestEmptySections(t *testing.T) {
	if _, err := Parse([]byte("listen: 127.0.0.1:1\nmodels: {m: {endpoints: [{protocol: openai, provider: x, models: [y]}]}}")); err == nil {
		t.Error("want error for no providers")
	}
	if _, err := Parse([]byte("providers:\n  - {name: p, base_url: {openai: https://x.com}}")); err == nil {
		t.Error("want error for no models")
	}
}

// --- Condition routing / Sticky Model fields (see
// docs/VirtualModelRouter_Design_v4_Core.md §6.4/§6.5) ---

func TestCapabilitiesAndMaxContextTokensOptional(t *testing.T) {
	// The base fixture declares neither field on its one endpoint-group —
	// this is the zero-config-migration case every existing config.yaml is in.
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	eg := cfg.Models["m1"].Endpoints[0]
	if len(eg.Capabilities) != 0 {
		t.Errorf("expected no declared capabilities, got %v", eg.Capabilities)
	}
	if eg.MaxContextTokens != 0 {
		t.Errorf("expected MaxContextTokens 0 (unconstrained), got %d", eg.MaxContextTokens)
	}
}

func TestCapabilitiesAndMaxContextTokensParsed(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1",
		"priority: 1\n        capabilities: [text, image, tools]\n        max_context_tokens: 200000", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	eg := cfg.Models["m1"].Endpoints[0]
	want := []string{"text", "image", "tools"}
	if len(eg.Capabilities) != len(want) {
		t.Fatalf("Capabilities = %v, want %v", eg.Capabilities, want)
	}
	for i, c := range want {
		if eg.Capabilities[i] != c {
			t.Errorf("Capabilities[%d] = %q, want %q", i, eg.Capabilities[i], c)
		}
	}
	if eg.MaxContextTokens != 200000 {
		t.Errorf("MaxContextTokens = %d, want 200000", eg.MaxContextTokens)
	}
}

func TestMaxContextTokensNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n        max_context_tokens: -1", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("negative max_context_tokens must be rejected at load, not silently clamped")
	}
}

// TestVirtualModelCapabilitiesAndMaxContextTokensParsed locks in the
// model-level base fields (VirtualModel.Capabilities/MaxContextTokens) —
// distinct from the endpoint-group-level fields above, which override/add
// to this base at BuildSnapshot time (see router.mergeCapabilities).
func TestVirtualModelCapabilitiesAndMaxContextTokensParsed(t *testing.T) {
	yaml := strings.Replace(validYAML, "  m1:\n    endpoints:",
		"  m1:\n    capabilities: [text, tools]\n    max_context_tokens: 128000\n    endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Models["m1"]
	want := []string{"text", "tools"}
	if len(m.Capabilities) != len(want) {
		t.Fatalf("Capabilities = %v, want %v", m.Capabilities, want)
	}
	for i, c := range want {
		if m.Capabilities[i] != c {
			t.Errorf("Capabilities[%d] = %q, want %q", i, m.Capabilities[i], c)
		}
	}
	if m.MaxContextTokens != 128000 {
		t.Errorf("MaxContextTokens = %d, want 128000", m.MaxContextTokens)
	}
}

func TestVirtualModelMaxContextTokensNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "  m1:\n    endpoints:", "  m1:\n    max_context_tokens: -1\n    endpoints:", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("negative model-level max_context_tokens must be rejected at load, not silently clamped")
	}
}

func TestStickyTTLGlobalDefault(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StickyTTL.D() != DefaultStickyTTL {
		t.Errorf("StickyTTL default = %v, want %v", cfg.StickyTTL.D(), DefaultStickyTTL)
	}
}

func TestStickyTTLGlobalOverride(t *testing.T) {
	yaml := "sticky_ttl: 30m\n" + validYAML
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StickyTTL.D() != 30*time.Minute {
		t.Errorf("StickyTTL = %v, want 30m", cfg.StickyTTL.D())
	}
}

func TestStickyTTLPerEndpointOverride(t *testing.T) {
	// nil (unset) vs. an explicit override must both be representable —
	// same *Duration pattern as ImageDownscaleMaxPx.
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n        sticky_ttl: 2h", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	eg := cfg.Models["m1"].Endpoints[0]
	if eg.StickyTTL == nil {
		t.Fatal("expected a non-nil per-endpoint StickyTTL override")
	}
	if eg.StickyTTL.D() != 2*time.Hour {
		t.Errorf("endpoint StickyTTL = %v, want 2h", eg.StickyTTL.D())
	}

	// The base fixture's endpoint doesn't set it — nil means "inherit the
	// global default", not "zero".
	base, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if base.Models["m1"].Endpoints[0].StickyTTL != nil {
		t.Error("expected a nil per-endpoint StickyTTL when not set (inherit global)")
	}
}

func TestStickyTTLNonPositiveRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n        sticky_ttl: 0s", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("sticky_ttl: 0s must be rejected at load (a zero-duration affinity window is meaningless)")
	}
}

// TestStickyTTLGlobalAboveBackstopRejected and
// TestStickyTTLPerEndpointAboveBackstopRejected lock the fix for the gap
// flagged in the previous review: internal/sticky.Registry evicts an idle
// entry from its map after sticky.BackstopTTL (24h) independent of any
// endpoint's own StickyTTL, so a configured sticky_ttl above that value
// would load successfully but silently stop taking effect once a
// conversation goes quiet for longer than the backstop — a "no error but
// the feature stops working" trap. validate() must catch it at load time.
func TestStickyTTLGlobalAboveBackstopRejected(t *testing.T) {
	yaml := "sticky_ttl: 25h\n" + validYAML
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("global sticky_ttl above sticky.BackstopTTL (24h) must be rejected at load")
	}
}

func TestStickyTTLPerEndpointAboveBackstopRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n        sticky_ttl: 48h", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("per-endpoint sticky_ttl above sticky.BackstopTTL (24h) must be rejected at load")
	}
}

func TestStickyTTLAtBackstopBoundaryAccepted(t *testing.T) {
	// Exactly the backstop value is still safe (the backstop only evicts
	// entries idle STRICTLY LONGER than itself — see internal/sticky.Set),
	// so this must not be rejected as an off-by-one.
	yaml := "sticky_ttl: 24h\n" + validYAML
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Errorf("sticky_ttl exactly at the backstop (24h) should be accepted, got %v", err)
	}
}

func TestModelStickyDefaultsToTrue(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Models["m1"]
	if m.Sticky != nil {
		t.Errorf("expected Sticky to be nil (unset) when not declared, got %v", *m.Sticky)
	}
	// nil is the config-level representation; router.BuildSnapshot resolves
	// nil -> true (see docs/VirtualModelRouter_Design_v4_Core.md
	// §6.5) — that resolution is covered in internal/router's own tests.
}

func TestModelStickyExplicitFalse(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    sticky: false\n    endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Models["m1"]
	if m.Sticky == nil || *m.Sticky != false {
		t.Errorf("expected Sticky to be an explicit false, got %v", m.Sticky)
	}
}

// TestOpenAIResponsesProtocolAccepted locks in that a third protocol needs
// zero config-package code changes to become valid config: base_url and
// EndpointGroup.Protocol are both validated purely against the adapter
// registry (adapter.Get), never a hardcoded "openai"/"anthropic" string
// list — see config.go's validate(). Registering the new adapter (this
// file's blank import above) is the only thing that made this YAML valid;
// nothing in this package itself was touched to allow it.
func TestOpenAIResponsesProtocolAccepted(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai-responses: https://api.example.com/v1}
    api_key: ${VMR_TEST_KEY}
models:
  m1:
    endpoints:
      - protocol: openai-responses
        provider: p1
        models: [real-model]
`
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("openai-responses protocol should validate: %v", err)
	}
	eg := cfg.Models["m1"].Endpoints[0]
	if eg.Protocol != "openai-responses" {
		t.Errorf("protocol: got %q", eg.Protocol)
	}
}

// TestOpenAIResponsesAndChatCompletionsCoexist locks in that one virtual
// model name can mix protocol: openai and protocol: openai-responses
// endpoint groups — the same "one name, several independently-reachable
// protocol faces" pattern already used for openai/anthropic (see
// VirtualModel's doc comment); BuildSnapshot splits them into separate
// per-protocol routes (see internal/router/router_test.go's
// TestBuildSnapshotSplitsVirtualModelByProtocol for the runtime-side
// assertion of that split).
func TestOpenAIResponsesAndChatCompletionsCoexist(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1, openai-responses: https://api.example.com/v1}
    api_key: ${VMR_TEST_KEY}
models:
  agent:
    endpoints:
      - protocol: openai
        provider: p1
        models: [real-model]
      - protocol: openai-responses
        provider: p1
        models: [real-model]
`
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("mixed protocol endpoints under one virtual model should validate: %v", err)
	}
	if len(cfg.Models["agent"].Endpoints) != 2 {
		t.Fatalf("expected 2 endpoint groups, got %d", len(cfg.Models["agent"].Endpoints))
	}
}
