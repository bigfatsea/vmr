// Ver 2026-08-07, by Opus 5

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const quotaConfigYAML = `
listen: 127.0.0.1:0
providers:
  - name: plan-a
    base_url: {openai: https://example.com/v1}
    api_key: test-key
    quota:
      limits:
        - {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}
models:
  m1:
    endpoints:
      - protocol: openai
        provider: plan-a
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

// TestCmdStatus_RendersQuotaLine mirrors TestCmdStatus_WithMockServer but
// adds a "quota" array to the mocked /admin/status payload — pinning that
// server/admin.go's new section actually reaches a human-readable line in
// `vmr status`, not just the JSON struct (see cmd_status.go's statusResponse.Quota).
func TestCmdStatus_RendersQuotaLine(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": map[string]any{},
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
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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
			"models":      map[string]any{},
			"concurrency": map[string]any{"limit": 0, "in_flight": 0, "waiting": 0},
			"time":        "2026-08-07T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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
