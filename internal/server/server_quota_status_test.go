// Ver 2026-08-07, by Opus 5

// /admin/status's "quota" section — see internal/router/quota.go's
// QuotaStatus and docs/TokenPlan_Quota_Routing_Design_opus-5.md's
// observability section.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/quota"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

const quotaStatusYAML = `
listen: 127.0.0.1:18801
providers:
  - name: p1
    base_url: {openai: http://127.0.0.1:1}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 500}]
models:
  vm:
    endpoints: [{protocol: openai, provider: p1, models: [m1]}]
`

const noQuotaYAML = `
listen: 127.0.0.1:18802
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints: [{protocol: openai, provider: p1, models: [m1]}]
`

type quotaStatusRow struct {
	Provider     string  `json:"provider"`
	Metric       string  `json:"metric"`
	Every        string  `json:"every"`
	Amount       float64 `json:"amount"`
	Used         float64 `json:"used"`
	Pct          float64 `json:"pct"`
	Headroom     float64 `json:"headroom"`
	EstimatedPct float64 `json:"estimated_pct"`
}

func fetchAdminStatusRaw(t *testing.T, rt *router.Router) map[string]json.RawMessage {
	t.Helper()
	s := New(rt, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAdminStatus_QuotaSection_Present(t *testing.T) {
	cfg, err := config.Parse([]byte(quotaStatusYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)

	l := snap.Models["openai"]["vm"].Endpoints[0].Quota.Limits[0]
	ps := quota.PeriodStart(l, time.Now())
	rt.Quota.Charge("p1", "requests/1mo", ps, quota.Counters{Requests: 5}, 0)

	out := fetchAdminStatusRaw(t, rt)
	raw, ok := out["quota"]
	if !ok {
		t.Fatalf("response missing \"quota\" key: %v", out)
	}
	var rows []quotaStatusRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Provider != "p1" || r.Metric != "requests" || r.Every != "1mo" || r.Amount != 500 || r.Used != 5 {
		t.Fatalf("row = %+v", r)
	}
}

func TestAdminStatus_QuotaSection_AbsentWhenUnconfigured(t *testing.T) {
	cfg, err := config.Parse([]byte(noQuotaYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)

	out := fetchAdminStatusRaw(t, rt)
	if _, ok := out["quota"]; ok {
		t.Fatalf("response has a \"quota\" key with no provider configured: %v", out)
	}
}

func TestAdminStatus_QuotaSection_AbsentWithoutRegistry(t *testing.T) {
	// Same config as the "present" test, but Router.Quota is never wired up
	// (the common case for tests, `vmr diagnose`, etc.) — must degrade to
	// "no quota key", not panic or emit an empty/zeroed row.
	cfg, err := config.Parse([]byte(quotaStatusYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)

	out := fetchAdminStatusRaw(t, rt)
	if _, ok := out["quota"]; ok {
		t.Fatalf("response has a \"quota\" key with no Registry wired up: %v", out)
	}
}
