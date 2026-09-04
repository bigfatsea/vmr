// Ver 2026-08-07, by Opus 5

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/pricing"
)

const quotaConfigYAML = `
listen: 127.0.0.1:0
providers:
  - name: plan-a
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
    quota:
      limits:
        - {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [plan-a]
        models: [real-model]
`

func TestCmdCheck_PrintsQuotaConfig(t *testing.T) {
	path := writeTempFile(t, "config.yaml", quotaConfigYAML)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, "quota:") {
		t.Fatalf("output missing quota: section:\n%s", out)
	}
	if !strings.Contains(out, "requests:") || !strings.Contains(out, "every=1mo") || !strings.Contains(out, "amount=90000") {
		t.Fatalf("output missing resolved limit detail:\n%s", out)
	}
}

// TestCmdCheck_PrintsBucketGateRoles pins the role annotation on vmr check's
// quota section: the single Limit is the bucket ("role=bucket"), and adding
// a shorter window marks it "role=gate" — a resolved role an operator can
// see instead of one that only emerges from scoring at runtime.
func TestCmdCheck_PrintsBucketGateRoles(t *testing.T) {
	path := writeTempFile(t, "config.yaml", quotaConfigYAML)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, "role=bucket") {
		t.Fatalf("output missing role=bucket for the single-Limit provider:\n%s", out)
	}

	twoLimits := strings.Replace(quotaConfigYAML,
		"limits:", "limits:\n        - {metric: requests, every: 1min, amount: 60}", 1)
	path = writeTempFile(t, "config.yaml", twoLimits)
	out = captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "every=1min") && !strings.Contains(line, "role=gate") {
			t.Fatalf("the 1min Limit should be role=gate:\n%s", out)
		}
		if strings.Contains(line, "every=1mo") && !strings.Contains(line, "role=bucket") {
			t.Fatalf("the 1mo Limit should be role=bucket:\n%s", out)
		}
	}
}

// TestCmdCheck_EqualPeriods_RoleNotWrittenOrder pins the §2.90 tie-break at
// the check layer: a shared 1mo pool and a per-model 1mo pool must resolve
// the shared pool to the bucket regardless of the order the two limits:
// entries appear in the YAML.
func TestCmdCheck_EqualPeriods_RoleNotWrittenOrder(t *testing.T) {
	yaml := strings.Replace(quotaConfigYAML,
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}",
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}\n        - {metric: tokens, every: 1mo, amount: 5000000, models: [real-model]}", 1)
	path := writeTempFile(t, "config.yaml", yaml)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "models=real-model") && !strings.Contains(line, "role=gate") {
			t.Fatalf("the per-model equal-period Limit should be role=gate (shared pool wins the tie):\n%s", out)
		}
	}
	// A single-named-model Limit is resolved exactly (see
	// TestCmdCheck_SingleNamedModelListIsExact) — no note here, unlike the
	// wildcard/multi-model case in TestCmdCheck_ApproximatedRoleNote.
	if strings.Contains(out, "role can differ per model") {
		t.Fatalf("a single-named-model Limit's role is exact — it should not produce the approximation note:\n%s", out)
	}
}

// TestCmdCheck_SharedRoleExcludesRestrictedCompetitor pins the shared-row
// fix at the check layer: a shared pool alongside a STRICTLY LONGER
// restricted-list Limit must still print the shared line as role=bucket —
// the restricted Limit only ever competes for the model it names, never
// for the shared row (see quota.Role's doc comment). Contrast
// TestCmdCheck_EqualPeriods_RoleNotWrittenOrder, where the two periods tie
// and the pool already won under the pre-fix full-set derivation too.
func TestCmdCheck_SharedRoleExcludesRestrictedCompetitor(t *testing.T) {
	yaml := strings.Replace(quotaConfigYAML,
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}",
		"- {metric: requests, every: 1d, amount: 500}\n        - {metric: tokens, every: 1mo, amount: 250000, models: [real-model]}", 1)
	path := writeTempFile(t, "config.yaml", yaml)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "every=1d") && !strings.Contains(line, "role=bucket") {
			t.Fatalf("the shared 1d pool should stay role=bucket despite the longer restricted-list competitor:\n%s", out)
		}
		if strings.Contains(line, "models=real-model") && !strings.Contains(line, "role=bucket") {
			t.Fatalf("real-model's own applicable set has the 1mo Limit as its bucket:\n%s", out)
		}
	}
}

// TestCmdCheck_SingleNamedModelListIsExact pins that a Limit restricted to
// exactly ONE named model is resolved exactly, not approximated: two
// disjoint single-model Limits on the same provider (legal — Scopes don't
// overlap) each have applicableLimits({that one model}) = {itself} alone,
// so each is trivially its own bucket regardless of which one has the
// larger Amount. The old full-Limit-set approximation would pick a single
// winner across BOTH lines (the larger Amount, via preferBucket) and print
// the loser as role=gate even though it's the sole, hence winning, Limit in
// its own model's routing view.
func TestCmdCheck_SingleNamedModelListIsExact(t *testing.T) {
	yaml := strings.Replace(quotaConfigYAML,
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}",
		"- {metric: requests, every: 1mo, amount: 100, models: [lite]}\n        - {metric: requests, every: 1mo, amount: 200, models: [flash]}", 1)
	path := writeTempFile(t, "config.yaml", yaml)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "models=lite") && !strings.Contains(line, "role=bucket") {
			t.Fatalf("lite's applicable set is {itself} alone, so it's trivially its own bucket, not gated by flash's larger Amount:\n%s", out)
		}
		if strings.Contains(line, "models=flash") && !strings.Contains(line, "role=bucket") {
			t.Fatalf("flash's applicable set is {itself} alone too:\n%s", out)
		}
	}
	if strings.Contains(out, "role can differ per model") {
		t.Fatalf("every Limit here is a single-named-model list, resolved exactly — no approximation note expected:\n%s", out)
	}
}

// TestCmdCheck_ApproximatedRoleNote pins the narrowed note trigger: it
// fires for a wildcard or a multi-model list (the two Scope shapes a
// static line genuinely cannot resolve to one correct role), but not
// merely because a per-model Limit exists (see
// TestCmdCheck_SingleNamedModelListIsExact, which has none of the note).
func TestCmdCheck_ApproximatedRoleNote(t *testing.T) {
	const note = "role can differ per model"

	wildcard := strings.Replace(quotaConfigYAML,
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}",
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}\n        - {metric: tokens, every: 1min, amount: 1000, models: [\"*\"]}", 1)
	path := writeTempFile(t, "config.yaml", wildcard)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, note) {
		t.Fatalf("a wildcard Limit should produce the approximation note:\n%s", out)
	}

	multiModel := strings.Replace(quotaConfigYAML,
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}",
		"- {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}\n        - {metric: tokens, every: 1min, amount: 1000, models: [lite, flash]}", 1)
	path = writeTempFile(t, "config.yaml", multiModel)
	out = captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, note) {
		t.Fatalf("a multi-model-list Limit should produce the approximation note:\n%s", out)
	}
}

func TestCmdCheck_PrintsEffectiveTimezone(t *testing.T) {
	path := writeTempFile(t, "config.yaml", minimalConfigYAML)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, "timezone:") {
		t.Fatalf("output missing timezone: line:\n%s", out)
	}
}

func TestCmdCheck_NoQuotaBlock_SectionAbsent(t *testing.T) {
	path := writeTempFile(t, "config.yaml", minimalConfigYAML)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if strings.Contains(out, "quota:") {
		t.Fatalf("output has a quota: section for a config with none configured:\n%s", out)
	}
}

// TestCmdCheck_PrintsPricingTableLine verifies pricing table summary output
// (pricingTableLine in cmd_check.go): a config that touches pricing at all
// — here, a global `pricing:` block — must grow a "pricing_table:" line
// naming the embedded standard table's generation date, so an operator can
// spot a stale reference price without opening internal/pricing's source.
// This had zero test coverage before (verified by grep across the repo
// during a 2026-08-09 review) — a regression that silently dropped this
// line, or broke the "no pricing touched" absence case below, would not
// have failed any existing test.
func TestCmdCheck_PrintsPricingTableLine(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
pricing:
  currency: USD
providers:
  - name: plan-a
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [plan-a]
        models: [real-model]
`
	path := writeTempFile(t, "config.yaml", yaml)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, "pricing_table:") || !strings.Contains(out, "built-in standard table generated") {
		t.Fatalf("output missing pricing_table: line for a config with a pricing: block:\n%s", out)
	}
}

// TestCmdCheck_NoPricingTouched_PricingTableLineAbsent is
// TestCmdCheck_PrintsPricingTableLine's negative case: a config that never
// mentions pricing (no global pricing: block, no providers[].pricing, no
// metric: cost limit) must not grow a pricing_table: line at all — see
// pricingTableLine's own doc comment ("ok=false when nothing in this
// config touches pricing at all").
func TestCmdCheck_NoPricingTouched_PricingTableLineAbsent(t *testing.T) {
	path := writeTempFile(t, "config.yaml", minimalConfigYAML)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if strings.Contains(out, "pricing_table:") {
		t.Fatalf("output has a pricing_table: line for a config that never touches pricing:\n%s", out)
	}
}

// TestCmdCheck_PricingLineShowsEffectiveRateNotBase pins the fix for a
// finding from the 2026-08-12 review (VMR_项目全面Review报告 A2):
// printProviderPricing used to print spec.Base, the standard table's list
// price, even for an account with a discount override — an operator reading
// `vmr check` for a metric: cost account had no way to see what it would
// actually be charged. This config gives claude-3-7-sonnet-20250219 (present
// in the embedded standard table) a 50% discount override; the printed line
// must reflect the discounted (effective) rate, not the undiscounted base.
func TestCmdCheck_PricingLineShowsEffectiveRateNotBase(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
pricing:
  currency: USD
providers:
  - name: anthropic
    base_url: {anthropic-messages: https://example.com/v1}
    api_key: test-key
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 100}
    pricing:
      overrides:
        - {model: "*", discount: 0.5}
models:
  m1:
    endpoints:
      - protocol: anthropic-messages
        providers: [anthropic]
        models: [claude-3-7-sonnet-20250219]
`
	path := writeTempFile(t, "config.yaml", yaml)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	spec := cfg.ResolvedPricing["anthropic\x00claude-3-7-sonnet-20250219"]
	if spec == nil {
		t.Fatal("no resolved pricing spec for anthropic/claude-3-7-sonnet-20250219")
	}
	base := spec.Base
	effective := pricing.EffectiveRate(spec)
	if base.InFresh == nil || effective.InFresh == nil || *base.InFresh == *effective.InFresh {
		t.Fatalf("test setup didn't produce a base/effective gap — base=%v effective=%v", ratePart(base.InFresh), ratePart(effective.InFresh))
	}

	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, fmt.Sprintf("in_fresh=%s", ratePart(effective.InFresh))) {
		t.Fatalf("pricing line does not show the discounted effective rate (%s):\n%s", ratePart(effective.InFresh), out)
	}
	if strings.Contains(out, fmt.Sprintf("in_fresh=%s", ratePart(base.InFresh))) {
		t.Fatalf("pricing line shows the undiscounted base rate (%s) instead of the effective one:\n%s", ratePart(base.InFresh), out)
	}
}

// TestCmdCheck_DeclaredUnresolvedPricingOverrideWithoutDiscount pins a
// crash: printProviderPricing's "declared but not resolved" fallback (a
// providers[].pricing block whose provider has no endpoint currently routed
// to it, so config.Config.ResolvedPricing has nothing for it) used to
// unconditionally dereference oc.Discount before checking it for nil —
// exactly one of Discount or the four explicit rate components is set (see
// config.PricingOverrideConfig's doc comment), so an override using the
// explicit-components form panicked `vmr check` outright.
func TestCmdCheck_DeclaredUnresolvedPricingOverrideWithoutDiscount(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
pricing:
  currency: USD
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
    pricing:
      overrides:
        - {model: some-model, in_fresh: 1, cache_read: 0.1, cache_write: 1.25, out: 3}
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [p1]
        models: [other-model]
`
	path := writeTempFile(t, "config.yaml", yaml)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, "in_fresh=1 cache_read=0.1 cache_write=1.25 out=3") {
		t.Errorf("declared-but-unresolved override's explicit rate components not rendered:\n%s", out)
	}
}

// TestCmdCheck_ListenExposureWarningDoesNotFail pins the fix for a real
// Regression test: ensures checkListenExposure does not panic on a
// SeverityWarning-only Issue set must render under "=== Warnings ===", not
// "=== Failed ===", and cmdCheck must return nil (exit 0) — the check is
// meant to surface a risky-but-intentional setup, never to block `vmr
// check`/`vmr start`/`vmr diagnose`.
func TestCmdCheck_ListenExposureWarningDoesNotFail(t *testing.T) {
	yaml := `
listen: 0.0.0.0:8800
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: test-key}
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [p1]
        models: [real-model]
`
	path := writeTempFile(t, "config.yaml", yaml)
	var out string
	err := func() error {
		var e error
		out = captureStdout(t, func() { e = cmdCheck([]string{"-c", path}) })
		return e
	}()
	if err != nil {
		t.Fatalf("cmdCheck returned an error for a warning-only issue: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "=== Warnings ===") {
		t.Fatalf("output missing === Warnings === section:\n%s", out)
	}
	if strings.Contains(out, "=== Failed ===") {
		t.Fatalf("output has === Failed === for a warning-only issue set:\n%s", out)
	}
	if !strings.Contains(out, "=== OK ===") {
		t.Fatalf("output missing === OK === — a warning-only issue set must still report OK:\n%s", out)
	}
}

// TestCmdStatus_RendersQuotaLine mirrors TestCmdStatus_WithMockServer but
// adds a "quota" array to the mocked /status payload — pinning that
// server/admin.go's new section actually reaches a human-readable line in
// `vmr status`, not just the JSON struct (see cmd_status.go's statusResponse.Quota).
func TestCmdStatus_RendersQuotaLine(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []any{},
			"concurrency": map[string]any{
				"limit": 0, "in_flight": 0, "waiting": 0,
			},
			"quota": []map[string]any{
				{
					"provider": "plan-a", "metric": "requests", "every": "1mo",
					"amount": 90000, "used": 4500, "pct": 5.0, "headroom": 1.2,
					"period_start": "2026-08-01T00:00:00Z", "period_ends_at": "2026-09-01T00:00:00Z",
					"estimated_pct": 0,
				},
			},
			"time": "2026-08-07T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`, ts.Listener.Addr().String())

	path := writeTempFile(t, "config.yaml", yaml)
	got := captureStdout(t, func() {
		if err := cmdStatus([]string{"-c", path}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})
	if !strings.Contains(got, "plan-a") || !strings.Contains(got, "requests/1mo") {
		t.Errorf("output missing quota provider/metric line: %q", got)
	}
	if !strings.Contains(got, "4500") || !strings.Contains(got, "90000") {
		t.Errorf("output missing used/amount figures: %q", got)
	}
}

func TestCmdStatus_NoQuotaArray_NoQuotaLines(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models":      []any{},
			"concurrency": map[string]any{"limit": 0, "in_flight": 0, "waiting": 0},
			"time":        "2026-08-07T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`, ts.Listener.Addr().String())

	path := writeTempFile(t, "config.yaml", yaml)
	got := captureStdout(t, func() {
		if err := cmdStatus([]string{"-c", path}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})
	if strings.Contains(got, "quota ") {
		t.Errorf("output has a quota line with no quota array in the response: %q", got)
	}
}
