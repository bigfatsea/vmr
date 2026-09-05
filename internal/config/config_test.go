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
    base_url: {openai-completions: https://api.example.com/v1}
    api_key: ${VMR_TEST_KEY}
models:
  m1:
    endpoints:
      openai-completions:
        - providers: [p1]
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
	if cfg.Timeouts.Probe.D() != DefaultProbeTimeout {
		t.Errorf("default timeouts.probe: got %v, want %v", cfg.Timeouts.Probe.D(), DefaultProbeTimeout)
	}
}

// TestParseTracksEmptyEnvRefs: a ${VAR} the config references that is unset
// (or set to "") lands in Config.EmptyEnvRefs so cmd_start's banner can name
// it — the most common "loads fine, 401s on the first request" cause.
func TestParseTracksEmptyEnvRefs(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "") // referenced by validYAML, explicitly empty
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EmptyEnvRefs) != 1 || cfg.EmptyEnvRefs[0] != "VMR_TEST_KEY" {
		t.Fatalf("EmptyEnvRefs = %v, want [VMR_TEST_KEY]", cfg.EmptyEnvRefs)
	}

	t.Setenv("VMR_TEST_KEY", "sk-real")
	cfg, err = Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EmptyEnvRefs) != 0 {
		t.Fatalf("EmptyEnvRefs = %v, want empty once the var is set", cfg.EmptyEnvRefs)
	}
}

// TestParseRejectsEnvValueWithNewline pins the fix for a finding from the
// 2026-08-12 review (VMR_项目全面Review报告 B5): expandEnv substitutes
// ${VAR} into the raw YAML TEXT before parsing, so a value containing a
// newline doesn't just fill in the scalar it was written into — it can
// inject a new top-level key (here, a second listen: line) that changes
// the document's structure. Must be a hard load error, not a config that
// silently parses into something the author never wrote.
func TestParseRejectsEnvValueWithNewline(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-test-123\nlisten: 0.0.0.0:1")
	_, err := Parse([]byte(validYAML))
	if err == nil || !strings.Contains(err.Error(), "VMR_TEST_KEY") || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("want a rejection naming VMR_TEST_KEY and \"newline\", got %v", err)
	}
}

// TestParseRejectsEnvValueWithColonSpace is TestParseRejectsEnvValueWithNewline's
// other trigger: a ": " inside an expanded value can turn what was meant to
// be a scalar into a new key: value pair on the same line.
func TestParseRejectsEnvValueWithColonSpace(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-test-123, extra: stuff")
	_, err := Parse([]byte(validYAML))
	if err == nil || !strings.Contains(err.Error(), "VMR_TEST_KEY") {
		t.Fatalf("want a rejection naming VMR_TEST_KEY, got %v", err)
	}
}

// TestParseRejectsEnvValueWithHashComment covers a gap an independent
// review found in the first version of this guard: a " #" inside an
// expanded value doesn't restructure the document, it starts a YAML
// comment mid-scalar — silently truncating the value with no parse error
// at all, which is arguably worse than the newline/colon cases (those at
// least tend to produce a load error downstream; this one doesn't).
func TestParseRejectsEnvValueWithHashComment(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-test-123 #rotated 2026-08")
	_, err := Parse([]byte(validYAML))
	if err == nil || !strings.Contains(err.Error(), "VMR_TEST_KEY") {
		t.Fatalf("want a rejection naming VMR_TEST_KEY, got %v", err)
	}
}

// TestParseRejectsEnvValueWithLeadingHash covers an expanded value starting
// with "#": in YAML, a line starting with "#" becomes a comment, silently
// turning the key's value into an empty string without any parse error.
func TestParseRejectsEnvValueWithLeadingHash(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "#secret-api-key")
	_, err := Parse([]byte(validYAML))
	if err == nil || !strings.Contains(err.Error(), "VMR_TEST_KEY") {
		t.Fatalf("want a rejection naming VMR_TEST_KEY, got %v", err)
	}

	t.Setenv("VMR_TEST_KEY", "  #secret-with-leading-spaces")
	_, err = Parse([]byte(validYAML))
	if err == nil || !strings.Contains(err.Error(), "VMR_TEST_KEY") {
		t.Fatalf("want a rejection naming VMR_TEST_KEY for leading-whitespace comment, got %v", err)
	}
}

// TestParseAllowsOrdinaryEnvValues is the negative case: a plain API key
// (no newline, no ": ") must keep loading exactly as before — the check
// above must not false-positive on ordinary secrets.
func TestParseAllowsOrdinaryEnvValues(t *testing.T) {
	t.Setenv("VMR_TEST_KEY", "sk-ordinary-1234567890abcdef")
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := cfg.ProviderByName("p1")
	if !ok || p.APIKey != "sk-ordinary-1234567890abcdef" {
		t.Errorf("env expansion: got %q", p.APIKey)
	}
}

func TestProbeTimeoutConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\ntimeouts:\n  probe: 5s", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeouts.Probe.D() != 5*time.Second {
		t.Errorf("timeouts.probe: got %v, want 5s", cfg.Timeouts.Probe.D())
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

// TestRoleMapConfig/TestRoleMapUnsetIsNil pin role_map's home: per
// provider (providers[].role_map) — the rejection of roles a provider's
// gateway doesn't recognize is a property of its API implementation, not
// of any one virtual model.
func TestRoleMapConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n    role_map:\n      developer: system", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Providers[0].RoleMap
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
	if got := cfg.Providers[0].RoleMap; got != nil {
		t.Errorf("role_map should be nil when omitted, got %v", got)
	}
}

// TestRoleMapEmptyNormalized pins the load-time normalization: a role_map
// written but left empty must not survive as a non-nil empty map.
func TestRoleMapEmptyNormalized(t *testing.T) {
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n    role_map: {}", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].RoleMap; got != nil {
		t.Errorf("empty role_map should be normalized to nil, got %v", got)
	}
}

// TestRoleMapInvalid pins the per-entry validation: blank names, blank
// targets, whitespace-padded names and self-mappings are all load errors.
// The padded cases are the dangerous half of the blank check: role matching
// is an exact string compare, so a padded key silently never matches (the
// 400 the user was fixing keeps happening) and a padded value rewrites to a
// role the gateway rejects — neither surfaces any hint at runtime.
func TestRoleMapInvalid(t *testing.T) {
	for name, frag := range map[string]string{
		"blank from":   "role_map:\n      \"\": system",
		"blank to":     "role_map:\n      developer: \"\"",
		"padded from":  "role_map:\n      \" developer\": system",
		"padded to":    "role_map:\n      developer: \" system\"",
		"padded NBSP":  "role_map:\n      \"developer\\u00a0\": system",
		"self-mapping": "role_map:\n      system: system",
	} {
		yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n    "+frag, 1)
		t.Setenv("VMR_TEST_KEY", "sk-test-123")
		if _, err := Parse([]byte(yaml)); err == nil {
			t.Errorf("%s: expected a load error, got none", name)
		}
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

func TestImageDownscaleNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nimage_downscale: -1", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "image_downscale must be >= 0") {
		t.Errorf("want image_downscale rejection error, got %v", err)
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

func TestModelImageDownscaleNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    image_downscale: -1\n    endpoints:", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "image_downscale must be >= 0") {
		t.Errorf("want per-model image_downscale rejection error, got %v", err)
	}
}

func TestImageCacheTTLDaysDefaultsToSevenDays(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTL.ImageCache.D() != time.Duration(DefaultImageCacheTTLDays)*24*time.Hour {
		t.Errorf("default ttl.image_cache: got %v, want %d days", cfg.TTL.ImageCache.D(), DefaultImageCacheTTLDays)
	}
}

func TestImageCacheTTLDaysConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nttl:\n  image_cache: 14d", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTL.ImageCache.D() != 14*24*time.Hour {
		t.Errorf("ttl.image_cache: got %v, want 14d", cfg.TTL.ImageCache.D())
	}
}

// TestImageCacheTTLDaysNonPositiveClampsToDefault: the image cache is a pure
// performance optimization with no audit/compliance value, so its zero-value
// polarity is the same "use the default" as every other ttl.* field (the old
// int field's separate <=0 clamp survives as the same rule).
func TestImageCacheTTLDaysNonPositiveClampsToDefault(t *testing.T) {
	for _, v := range []string{"0d", "-5d"} {
		yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nttl:\n  image_cache: "+v, 1)
		cfg, err := Parse([]byte(yaml))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.TTL.ImageCache.D() != time.Duration(DefaultImageCacheTTLDays)*24*time.Hour {
			t.Errorf("ttl.image_cache: %s must clamp to default %d days, got %v", v, DefaultImageCacheTTLDays, cfg.TTL.ImageCache.D())
		}
	}
}

func TestMaxAttemptsNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nmax_attempts: -1", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "max_attempts must be >= 0") {
		t.Errorf("want max_attempts rejection error, got %v", err)
	}
}

func TestMaxConcurrencyNegativeRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nmax_concurrency: -3", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "max_concurrency must be >= 0") {
		t.Errorf("want max_concurrency rejection error, got %v", err)
	}
}

// TestAuditRetentionDefaultsTo90Days: the old "absent = keep forever" default
// is gone — retention now defaults to a finite 90d, because a default that
// silently reverts to never-deleting is a disk-full trap (see
// DefaultAuditRetentionDays).
func TestAuditRetentionDefaultsTo90Days(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTL.AuditRetention.D() != time.Duration(DefaultAuditRetentionDays)*24*time.Hour {
		t.Errorf("default ttl.audit_retention: got %v, want %d days", cfg.TTL.AuditRetention.D(), DefaultAuditRetentionDays)
	}
}

func TestAuditRetentionConfig(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900", "listen: 127.0.0.1:9900\nttl:\n  audit_retention: 30d", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTL.AuditRetention.D() != 30*24*time.Hour {
		t.Errorf("ttl.audit_retention: got %v, want 30d", cfg.TTL.AuditRetention.D())
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
    base_url: {openai-completions: https://api.example.com/v1}
    api_key: k
models:
  m:
    endpoints:
      openai-completions:
        - {providers: [p], models: [third]}
        - {providers: [p], models: [first]}
        - {providers: [p], models: [second]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	eps := cfg.Models["m"].Endpoints["openai-completions"]
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
	got := cfg.Models["m1"].Endpoints["openai-completions"][0].Models
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
// entry declares base_url for both openai-completions and anthropic-messages,
// sharing one api_key/proxy setting, instead of the old format's two
// separately protocol-keyed provider entries.
func TestProviderServesBothProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9903
providers:
  - name: dual
    base_url: {openai-completions: https://api.example.com/v1, anthropic-messages: https://api.example.com/anthropic/v1}
    api_key: k1
models:
  m:
    endpoints:
      openai-completions:
        - {providers: [dual], models: [x]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.ProviderByName("dual")
	if !ok {
		t.Fatal("provider not found")
	}
	if p.BaseURL["openai-completions"] != "https://api.example.com/v1" || p.BaseURL["anthropic-messages"] != "https://api.example.com/anthropic/v1" {
		t.Errorf("base_url map: %v", p.BaseURL)
	}
}

// TestSameVirtualModelNameBothProtocols is the new format's version of "the
// same virtual model name is independently reachable from both ingress
// protocols": one models.<name> entry mixes an openai-completions endpoint
// group and an anthropic-messages one. Config-level this only needs to
// confirm both entries parse with their own protocol/provider/models intact
// — BuildSnapshot's split into two independent routes is covered in
// internal/router.
func TestSameVirtualModelNameBothProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9902
providers:
  - name: openrouter
    base_url: {openai-completions: https://openrouter.ai/api/v1, anthropic-messages: https://openrouter.ai/api/v1}
    api_key: k1
models:
  coding:
    endpoints:
      openai-completions:
        - {providers: [openrouter], models: [z-ai/glm-5.2]}
      anthropic-messages:
        - {providers: [openrouter], models: [minimax/minimax-m3]}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	endpoints := cfg.Models["coding"].Endpoints
	if len(endpoints) != 2 || len(endpoints["openai-completions"]) != 1 || len(endpoints["anthropic-messages"]) != 1 {
		t.Fatalf("want one group per protocol bucket, got %v", endpoints)
	}
	if eg := endpoints["openai-completions"][0]; len(eg.Models) != 1 || eg.Models[0] != "z-ai/glm-5.2" {
		t.Errorf("openai-completions entry mismatch: %+v", eg)
	}
	if eg := endpoints["anthropic-messages"][0]; len(eg.Models) != 1 || eg.Models[0] != "minimax/minimax-m3" {
		t.Errorf("anthropic-messages entry mismatch: %+v", eg)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, mutate, replacement, wantErr string
	}{
		{"bad base_url", "base_url: {openai-completions: https://api.example.com/v1}", "base_url: {openai-completions: not-a-url}", "invalid base_url"},
		{"unknown provider ref", "providers: [p1]", "providers: [ghost]", "unknown provider"},
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
	yaml := strings.Replace(validYAML, "base_url: {openai-completions: https://api.example.com/v1}", "base_url: {nosuch: https://api.example.com/v1}", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown adapter type") {
		t.Errorf("want unknown adapter type error, got %v", err)
	}
}

// TestUnknownEndpointProtocolRejected covers the endpoints map key naming
// an unregistered adapter — the key IS the protocol, so the validation
// error names the key directly.
func TestUnknownEndpointProtocolRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "      openai-completions:", "      nosuch:", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown protocol") {
		t.Errorf("want unknown protocol error, got %v", err)
	}
}

// TestLegacyProtocolNameGivesRenameHint: the pre-2026-08 names ("openai",
// "anthropic") stay hard load errors (config is strict YAML), but the error
// must point at the exact rename rather than just listing valid names.
func TestLegacyProtocolNameGivesRenameHint(t *testing.T) {
	t.Run("base_url key", func(t *testing.T) {
		yaml := strings.Replace(validYAML, "base_url: {openai-completions: https://api.example.com/v1}", "base_url: {openai: https://api.example.com/v1}", 1)
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), `rename "openai" to "openai-completions"`) {
			t.Errorf("want a rename hint for the legacy base_url key, got %v", err)
		}
	})
	t.Run("endpoint protocol key", func(t *testing.T) {
		yaml := strings.Replace(validYAML, "      openai-completions:", "      anthropic:", 1)
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), `rename "anthropic" to "anthropic-messages"`) {
			t.Errorf("want a rename hint for the legacy endpoints key, got %v", err)
		}
	})
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
	if _, err := Parse([]byte("listen: 127.0.0.1:1\nmodels: {m: {endpoints: {openai-completions: [{providers: [x], models: [y]}]}}}")); err == nil {
		t.Error("want error for no providers")
	}
	if _, err := Parse([]byte("providers:\n  - {name: p, base_url: {openai-completions: https://x.com}}")); err == nil {
		t.Error("want error for no models")
	}
}

// --- Condition routing / Sticky Model fields (see
// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing and
// Sticky Model sections) ---

// TestVirtualModelCapabilitiesAndMaxContextTokensParsed locks in the
// model-level override fields (VirtualModel.Capabilities/MaxContextTokens) —
// which take precedence over model_defaults at BuildSnapshot time.
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
	if cfg.TTL.Sticky.D() != DefaultStickyTTL {
		t.Errorf("TTL.Sticky default = %v, want %v", cfg.TTL.Sticky.D(), DefaultStickyTTL)
	}
}

func TestStickyTTLGlobalOverride(t *testing.T) {
	yaml := "ttl:\n  sticky: 30m\n" + validYAML
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTL.Sticky.D() != 30*time.Minute {
		t.Errorf("TTL.Sticky = %v, want 30m", cfg.TTL.Sticky.D())
	}
}

func TestStickyTTLPerProviderOverride(t *testing.T) {
	// nil (unset) vs. an explicit override must both be representable —
	// same *Duration pattern as ImageDownscaleMaxPx.
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n    sticky_ttl: 2h", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers[0].StickyTTL == nil {
		t.Fatal("expected a non-nil per-provider StickyTTL override")
	}
	if cfg.Providers[0].StickyTTL.D() != 2*time.Hour {
		t.Errorf("provider StickyTTL = %v, want 2h", cfg.Providers[0].StickyTTL.D())
	}

	// The base fixture's provider doesn't set it — nil means "inherit the
	// global default", not "zero".
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	base, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if base.Providers[0].StickyTTL != nil {
		t.Error("expected a nil per-provider StickyTTL when not set (inherit global)")
	}
}

func TestStickyTTLNonPositiveRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n    sticky_ttl: 0s", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("sticky_ttl: 0s must be rejected at load (a zero-duration affinity window is meaningless)")
	}
}

// TestStickyTTLGlobalAboveBackstopRejected and
// TestStickyTTLPerProviderAboveBackstopRejected lock the fix for the gap
// flagged in the previous review: internal/sticky.Registry evicts an idle
// entry from its map after sticky.BackstopTTL (24h) independent of any
// endpoint's own StickyTTL, so a configured ttl.sticky above that value
// would load successfully but silently stop taking effect once a
// conversation goes quiet for longer than the backstop — a "no error but
// the feature stops working" trap. validate() must catch it at load time.
func TestStickyTTLGlobalAboveBackstopRejected(t *testing.T) {
	yaml := "ttl:\n  sticky: 25h\n" + validYAML
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("global ttl.sticky above sticky.BackstopTTL (24h) must be rejected at load")
	}
}

func TestStickyTTLPerProviderAboveBackstopRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "api_key: ${VMR_TEST_KEY}", "api_key: ${VMR_TEST_KEY}\n    sticky_ttl: 48h", 1)
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Error("per-provider sticky_ttl above sticky.BackstopTTL (24h) must be rejected at load")
	}
}

func TestStickyTTLAtBackstopBoundaryAccepted(t *testing.T) {
	// Exactly the backstop value is still safe (the backstop only evicts
	// entries idle STRICTLY LONGER than itself — see internal/sticky.Set),
	// so this must not be rejected as an off-by-one.
	yaml := "ttl:\n  sticky: 24h\n" + validYAML
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Errorf("ttl.sticky exactly at the backstop (24h) should be accepted, got %v", err)
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
	// nil -> true (see docs/VirtualModelRouter_Design_v4_Core.md's Sticky
	// Model section) — that resolution is covered in internal/router's own tests.
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
// zero config-package code changes to become valid config: base_url's keys
// and the endpoints:/fallback_endpoints: protocol-map keys are both validated
// purely against the adapter registry (adapter.Get), never a hardcoded
// "openai-completions"/"anthropic-messages" string list — see config.go's
// validate(). Registering the new adapter (this file's blank import above)
// is the only thing that made this YAML valid; nothing in this package
// itself was touched to allow it.
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
      openai-responses:
        - providers: [p1]
          models: [real-model]
`
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("openai-responses protocol should validate: %v", err)
	}
	eg := cfg.Models["m1"].Endpoints["openai-responses"][0]
	if len(eg.Models) != 1 || eg.Models[0] != "real-model" {
		t.Errorf("openai-responses entry mismatch: %+v", eg)
	}
}

// TestOpenAIResponsesAndChatCompletionsCoexist locks in that one virtual
// model name can mix protocol: openai-completions and protocol: openai-responses
// endpoint groups — the same "one name, several independently-reachable
// protocol faces" pattern already used for openai-completions/anthropic-messages
// (see VirtualModel's doc comment); BuildSnapshot splits them into separate
// per-protocol routes (see internal/router/router_test.go's
// TestBuildSnapshotSplitsVirtualModelByProtocol for the runtime-side
// assertion of that split).
func TestOpenAIResponsesAndChatCompletionsCoexist(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai-completions: https://api.example.com/v1, openai-responses: https://api.example.com/v1}
    api_key: ${VMR_TEST_KEY}
models:
  agent:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [real-model]
      openai-responses:
        - providers: [p1]
          models: [real-model]
`
	t.Setenv("VMR_TEST_KEY", "sk-test-123")
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("mixed protocol endpoints under one virtual model should validate: %v", err)
	}
	endpoints := cfg.Models["agent"].Endpoints
	if len(endpoints) != 2 || len(endpoints["openai-completions"]) != 1 || len(endpoints["openai-responses"]) != 1 {
		t.Fatalf("expected one endpoint group per protocol bucket, got %v", endpoints)
	}
}

// --- VE2: reject ':' and '/' in provider names and api_keys labels ---

// TestProviderNameWithColonRejected covers the headline VE2 case: a ':'
// in a provider name folds the trailing fields into SplitEndpointLabel's
// model segment, corrupting every audit-label consumer downstream (see
// core.EndpointLabel's contract). Must be a hard load error at validate().
// The spec uses "providers[0]" because provider-name validation runs
// inside validateProviders, which uses array indices, while api_keys label
// validation uses the provider name in the error message (see
// TestProviderAPIKeysLabelWithColonRejected).
func TestProviderNameWithColonRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "name: p1", `name: "a:b"`, 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "':'") || !strings.Contains(err.Error(), `providers[0]`) {
		t.Fatalf("want error naming providers[0] and ':', got %v", err)
	}
}

// TestProviderNameWithSlashRejected: the same invariant for '/'. Less
// acute than ':' (the audit label is colon-separated, not slash), but
// '/' collides with the path-ish separators used downstream by report
// and pricing lookups, so the rule stays the same.
func TestProviderNameWithSlashRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "name: p1", `name: "a/b"`, 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "'/'") {
		t.Fatalf("want error naming '/', got %v", err)
	}
}

// TestProviderNameLegalCharsAccepted is the negative counterpart: every
// provider name valid in current fixtures (with - _ .) still loads.
func TestProviderNameLegalCharsAccepted(t *testing.T) {
	for _, name := range []string{"my-provider", "vllm_local", "gpt.4", "a", "abc123"} {
		// Replace both the providers entry AND the matching endpoint
		// reference in the model, so the name stays self-consistent.
		yaml := strings.Replace(validYAML, "name: p1", "name: "+name, 1)
		yaml = strings.Replace(yaml, "providers: [p1]", "providers: ["+name+"]", 1)
		if _, err := Parse([]byte(yaml)); err != nil {
			t.Errorf("provider name %q should still validate: %v", name, err)
		}
	}
}

// --- VE4: validate model strategy names at load time ---

// TestModelStrategyUnknownDimensionRejected pins the load-time strategy
// check: a typo'd dimension name parses cleanly, but the snapshot-build
// path (which used to be the only thing catching it) is bypassed by
// `vmr check`'s no-network validate-only path, so a typo could silently
// load and only surface when the snapshot builder trips. Now caught here.
func TestModelStrategyUnknownDimensionRejected(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    strategy: [prioity]\n    endpoints:", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `model "m1"`) || !strings.Contains(err.Error(), `"prioity"`) {
		t.Fatalf("want error naming the model and the bad dimension, got %v", err)
	}
}

// TestModelStrategyValidDimensionAccepted: explicit ["priority"] still
// loads (it was the documented default, applied by applyDefaults when
// unset; explicit declaration must round-trip identically).
func TestModelStrategyValidDimensionAccepted(t *testing.T) {
	yaml := strings.Replace(validYAML, "    endpoints:", "    strategy: [priority]\n    endpoints:", 1)
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Fatalf("explicit strategy: [priority] should still validate: %v", err)
	}
}

// TestModelStrategyUnsetInheritsDefault covers the spec's third
// acceptance bullet — `m.strategy: []` (omitted) keeps the existing
// default behavior. The existing TestParseDefaultsAndEnvExpansion already
// pins this at the parsed-Struct level; this one is the
// load-doesn't-error reading of the same fact.
func TestModelStrategyUnsetInheritsDefault(t *testing.T) {
	if _, err := Parse([]byte(validYAML)); err != nil {
		t.Fatalf("default (omitted) strategy should keep validating: %v", err)
	}
}
