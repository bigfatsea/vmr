// Ver 2026-07-23 12:00, by Sonnet 5
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
	if cfg.MaxAttempts != 0 || cfg.MaxRequestBodyMB != 8 {
		t.Errorf("defaults: attempts=%d (want 0 = unlimited) body=%d", cfg.MaxAttempts, cfg.MaxRequestBodyMB)
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
	if cfg.ProbeMode != ProbeModeActive {
		t.Errorf("default probe_mode: got %q, want %q", cfg.ProbeMode, ProbeModeActive)
	}
	if cfg.ProbeTimeout.D() != DefaultProbeTimeout {
		t.Errorf("default probe_timeout: got %v, want %v", cfg.ProbeTimeout.D(), DefaultProbeTimeout)
	}
}

func TestProbeModeConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nprobe_mode: passive\nprobe_timeout: 5s", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProbeMode != ProbeModePassive {
		t.Errorf("probe_mode: got %q, want %q", cfg.ProbeMode, ProbeModePassive)
	}
	if cfg.ProbeTimeout.D() != 5*time.Second {
		t.Errorf("probe_timeout: got %v, want 5s", cfg.ProbeTimeout.D())
	}
}

func TestProbeModeInvalidValueRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nprobe_mode: sometimes", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "probe_mode") {
		t.Errorf("expected a probe_mode validation error, got %v", err)
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

func TestRoleMapConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n      role_map:\n        developer: system", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Providers["openai"]["p1"].RoleMap
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
	if cfg.Providers["openai"]["p1"].RoleMap != nil {
		t.Errorf("role_map should be nil when omitted, got %v", cfg.Providers["openai"]["p1"].RoleMap)
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
	if got := cfg.Models["openai"]["m1"].ImageDownscaleMaxPx; got != nil {
		t.Errorf("unset per-model image_downscale must parse to nil (inherit global), got %v", *got)
	}
}

func TestModelImageDownscaleOverride(t *testing.T) {
	yaml := strings.Replace(validYAML, "endpoints:", "image_downscale: 256\n      endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["openai"]["m1"].ImageDownscaleMaxPx
	if got == nil || *got != 256 {
		t.Errorf("model image_downscale override: got %v, want 256", got)
	}
}

// TestModelImageDownscaleExplicitZeroDiffersFromUnset is the whole point of
// the pointer type: an explicit 0 must remain distinguishable from "not
// set" all the way through parsing, so it can force-disable the feature for
// this model even when the global default is on (§7).
func TestModelImageDownscaleExplicitZeroDiffersFromUnset(t *testing.T) {
	yaml := strings.Replace(validYAML, "endpoints:", "image_downscale: 0\n      endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["openai"]["m1"].ImageDownscaleMaxPx
	if got == nil {
		t.Fatal("explicit image_downscale: 0 must not parse to nil (that would mean 'unset')")
	}
	if *got != 0 {
		t.Errorf("explicit image_downscale: 0, got %d", *got)
	}
}

func TestModelImageDownscaleNegativeClampsToZero(t *testing.T) {
	yaml := strings.Replace(validYAML, "endpoints:", "image_downscale: -1\n      endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Models["openai"]["m1"].ImageDownscaleMaxPx
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
// pruning it (§10 decision table).
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

// TestLegacyAPIKeyRejectedWithMigrationMessage locks in the removal of the
// singular api_key: a config still carrying it must fail to load with a
// message that names api_keys as the replacement, not a generic yaml error.
func TestLegacyAPIKeyRejectedWithMigrationMessage(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\napi_key: sk-vmr-legacy-catchall", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "api_key has been removed") ||
		!strings.Contains(err.Error(), "api_keys") {
		t.Errorf("want migration error mentioning api_keys, got %v", err)
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

func TestEmptySections(t *testing.T) {
	if _, err := Parse([]byte("listen: 127.0.0.1:1\nmodels: {openai: {m: {endpoints: [{provider: x, model: y}]}}}")); err == nil {
		t.Error("want error for no providers")
	}
	if _, err := Parse([]byte("providers: {openai: {p: {base_url: https://x.com}}}")); err == nil {
		t.Error("want error for no models")
	}
}

// TestCountNested locks the shared helper directly: validate() only checks
// it against zero, but diagnose and cmd/vmr both print the actual count, so
// the arithmetic itself needs its own coverage, not just the boundary case.
func TestCountNested(t *testing.T) {
	m := map[string]map[string]int{
		"a": {"x": 1, "y": 2},
		"b": {"z": 3},
	}
	if got := CountNested(m); got != 3 {
		t.Errorf("CountNested = %d, want 3", got)
	}
	if got := CountNested(map[string]map[string]int{}); got != 0 {
		t.Errorf("CountNested(empty) = %d, want 0", got)
	}
}

// --- Condition routing / Sticky Model fields (see
// docs/vmr_condition_routing_and_sticky_model_sonnet-5.md §1.1/§2) ---

func TestCapabilitiesAndMaxContextTokensOptional(t *testing.T) {
	// The base fixture declares neither field on its one endpoint — this is
	// the zero-config-migration case every existing config.yaml is in.
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	ep := cfg.Models["openai"]["m1"].Endpoints[0]
	if len(ep.Capabilities) != 0 {
		t.Errorf("expected no declared capabilities, got %v", ep.Capabilities)
	}
	if ep.MaxContextTokens != 0 {
		t.Errorf("expected MaxContextTokens 0 (unconstrained), got %d", ep.MaxContextTokens)
	}
}

func TestCapabilitiesAndMaxContextTokensParsed(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1",
		"priority: 1\n          capabilities: [text, image, tools]\n          max_context_tokens: 200000", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	ep := cfg.Models["openai"]["m1"].Endpoints[0]
	want := []string{"text", "image", "tools"}
	if len(ep.Capabilities) != len(want) {
		t.Fatalf("Capabilities = %v, want %v", ep.Capabilities, want)
	}
	for i, c := range want {
		if ep.Capabilities[i] != c {
			t.Errorf("Capabilities[%d] = %q, want %q", i, ep.Capabilities[i], c)
		}
	}
	if ep.MaxContextTokens != 200000 {
		t.Errorf("MaxContextTokens = %d, want 200000", ep.MaxContextTokens)
	}
}

func TestMaxContextTokensNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n          max_context_tokens: -1", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("negative max_context_tokens must be rejected at load, not silently clamped")
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
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n          sticky_ttl: 2h", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	ep := cfg.Models["openai"]["m1"].Endpoints[0]
	if ep.StickyTTL == nil {
		t.Fatal("expected a non-nil per-endpoint StickyTTL override")
	}
	if ep.StickyTTL.D() != 2*time.Hour {
		t.Errorf("endpoint StickyTTL = %v, want 2h", ep.StickyTTL.D())
	}

	// The base fixture's endpoint doesn't set it — nil means "inherit the
	// global default", not "zero".
	base, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if base.Models["openai"]["m1"].Endpoints[0].StickyTTL != nil {
		t.Error("expected a nil per-endpoint StickyTTL when not set (inherit global)")
	}
}

func TestStickyTTLNonPositiveRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "priority: 1", "priority: 1\n          sticky_ttl: 0s", 1)
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("sticky_ttl: 0s must be rejected at load (a zero-duration affinity window is meaningless)")
	}
}

func TestModelStickyDefaultsToTrue(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Models["openai"]["m1"]
	if m.Sticky != nil {
		t.Errorf("expected Sticky to be nil (unset) when not declared, got %v", *m.Sticky)
	}
	// nil is the config-level representation; router.BuildSnapshot resolves
	// nil -> true (see docs/vmr_condition_routing_and_sticky_model_sonnet-5.md
	// §2.5) — that resolution is covered in internal/router's own tests.
}

func TestModelStickyExplicitFalse(t *testing.T) {
	yaml := strings.Replace(validYAML, "endpoints:", "sticky: false\n      endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Models["openai"]["m1"]
	if m.Sticky == nil || *m.Sticky != false {
		t.Errorf("expected Sticky to be an explicit false, got %v", m.Sticky)
	}
}
