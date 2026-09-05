// Ver 2026-09-16, by Pkg-C
package router

import (
	"strings"
	"testing"

	"vmr/internal/config"

	_ "vmr/internal/adapter/openai"
)

const disabledSnapYAML = `
listen: 127.0.0.1:0
providers:
  - name: p_on
    base_url: {openai-completions: https://p-on.example.com}
    api_key: k-on
  - name: p_off
    base_url: {openai-completions: https://p-off.example.com}
    api_key: k-off
    disabled: true
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-08-01, amount: 1000}]
fallback_endpoints:
  openai-completions:
    - {providers: [p_off], models: [fb-model], priority: 90}
models:
  vm:
    endpoints:
      openai-completions:
        - providers: [p_on, p_off]
          models: [model-a]
`

// TestBuildSnapshot_DisabledAbsentFromRoutes pins the core semantics —
// "treated as nonexistent at every consumer": the disabled provider's
// (provider, model) expansion is skipped AND the fallback entry naming it
// injects nothing, while the enabled provider's endpoint is untouched.
func TestBuildSnapshot_DisabledAbsentFromRoutes(t *testing.T) {
	cfg, err := config.Parse([]byte(disabledSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["vm"].Endpoints
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1 (p_on only — p_off skipped from both the group and the fallback injection)", len(eps))
	}
	if eps[0].Provider != "p_on" || eps[0].Model != "model-a" {
		t.Errorf("endpoint = %s/%s, want p_on/model-a", eps[0].Provider, eps[0].Model)
	}
	if eps[0].FromFallback {
		t.Errorf("the surviving endpoint must be the model's own, not a fallback injection")
	}
}

// TestBuildSnapshot_DisabledNoQuotaSpec pins the quota half: a disabled
// provider gets no core.QuotaSpec, so a takedown doesn't leave a stranded
// counter bucket for an account carrying no traffic. Flipping disabled
// back to false restores it — the documented evolution path.
func TestBuildSnapshot_DisabledNoQuotaSpec(t *testing.T) {
	cfg, err := config.Parse([]byte(disabledSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	specs := BuildQuotaSpecsDisabled(cfg.Providers, enabledProviders(cfg.Providers))
	if _, ok := specs["p_off"]; ok {
		t.Errorf("disabled provider p_off has a QuotaSpec, want none")
	}

	reEnabled := strings.Replace(disabledSnapYAML, "disabled: true", "disabled: false", 1)
	cfg2, err := config.Parse([]byte(reEnabled))
	if err != nil {
		t.Fatal(err)
	}
	specs2 := BuildQuotaSpecsDisabled(cfg2.Providers, enabledProviders(cfg2.Providers))
	if _, ok := specs2["p_off"]; !ok {
		t.Errorf("re-enabled provider p_off has no QuotaSpec — flipping disabled back to false must restore quota accounting")
	}
}

// TestBuildQuotaSpecs_IgnoresDisabledFlag pins the exported
// BuildQuotaSpecs (internal/replay's entry point) as unfiltered: replay
// targets one specific (provider,model) pair and must resolve its quota
// spec regardless of the provider's live routing state. The disabled
// filtering lives only in the BuildSnapshot path
// (BuildQuotaSpecsDisabled) — two shapes for two consumers.
func TestBuildQuotaSpecs_IgnoresDisabledFlag(t *testing.T) {
	cfg, err := config.Parse([]byte(disabledSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := BuildQuotaSpecs(cfg.Providers)["p_off"]; !ok {
		t.Errorf("BuildQuotaSpecs dropped disabled provider p_off — replay needs the spec for its explicit target")
	}
}

// TestBuildSnapshot_AllDisabled_YieldsEmptyRoute pins the filter's shape:
// a fully-disabled group skips providers but keeps the (protocol, model)
// route present with zero endpoints — the ordinary no-candidate behavior,
// not an unknown-model error.
func TestBuildSnapshot_AllDisabled_YieldsEmptyRoute(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - name: p_off
    base_url: {openai-completions: https://p-off.example.com}
    api_key: k
    disabled: true
models:
  vm:
    endpoints:
      openai-completions:
        - providers: [p_off]
          models: [model-a]
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	route, ok := snap.Models["openai-completions"]["vm"]
	if !ok {
		t.Fatalf("model vm should still have a route (empty, not absent)")
	}
	if len(route.Endpoints) != 0 {
		t.Errorf("route carries %d endpoints, want 0", len(route.Endpoints))
	}
}

// TestBuildSnapshot_EnabledSurvives is the control: the same shape without
// the disabled line must expand exactly as before the field existed — both
// providers, plus the fallback injection.
func TestBuildSnapshot_EnabledSurvives(t *testing.T) {
	yaml := strings.Replace(disabledSnapYAML, "    disabled: true\n", "", 1)
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["vm"].Endpoints
	// p_on/model-a (own) + p_off/model-a (own) + p_off/fb-model (fallback)
	if len(eps) != 3 {
		t.Fatalf("got %d endpoints, want 3 (2 own + 1 fallback)", len(eps))
	}
	for _, ep := range eps {
		if ep.Provider == "p_off" && ep.Quota == nil {
			t.Errorf("p_off endpoint %s: Quota nil, want the configured spec", ep.Model)
		}
	}
}
