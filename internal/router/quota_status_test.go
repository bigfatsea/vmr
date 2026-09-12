// Ver 2026-08-07, by Opus 5

package router

import (
	"testing"
	"time"

	"vmr/internal/quota"

	_ "vmr/internal/adapter/openai"
)

func TestQuotaStatus_NilRegistry(t *testing.T) {
	rt := New(nil)
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  m1:
    endpoints: {openai-completions: [{providers: [p1], models: [m]}]}
`)
	rt.Install(mustSnapshot(t, cfg))
	if got := rt.QuotaStatus(); got != nil {
		t.Fatalf("QuotaStatus with no Registry = %+v, want nil", got)
	}
}

func TestQuotaStatus_NoSnapshot(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	if got := rt.QuotaStatus(); got != nil {
		t.Fatalf("QuotaStatus before Install = %+v, want nil", got)
	}
}

func TestQuotaStatus_ReportsConfiguredProvidersOnly(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: https://example.com}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 1000}]
  - {name: p2, base_url: {openai-completions: https://example2.com}, api_key: k2}
models:
  m1:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1a, m1b]}
        - {providers: [p2], models: [m2]}
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)

	// Charge 3 requests to p1 before checking status.
	l := snap.Models["openai-completions"]["m1"].Endpoints[0].Quota.Limits[0]
	ps := quota.PeriodStart(l, time.Now())
	rt.Quota.Charge("p1", "requests/1mo", ps, quota.Counters{Requests: 3}, 0)

	st := rt.QuotaStatus()
	if len(st) != 1 {
		t.Fatalf("QuotaStatus returned %d entries, want 1 (only p1 has quota:)", len(st))
	}
	q := st[0]
	if q.Provider != "p1" || q.Metric != "requests" || q.Every != "1mo" || q.Amount != 1000 {
		t.Fatalf("status = %+v", q)
	}
	if q.Used != 3 || q.Requests != 3 {
		t.Fatalf("Used/Requests = %v/%v, want 3/3", q.Used, q.Requests)
	}
	if q.Pct < 0.29 || q.Pct > 0.31 {
		t.Fatalf("Pct = %v, want ~0.3 (3/1000*100)", q.Pct)
	}
	if !q.PeriodStart.Before(q.PeriodEndsAt) {
		t.Fatalf("PeriodStart=%v not before PeriodEndsAt=%v", q.PeriodStart, q.PeriodEndsAt)
	}
	// De-duplicated: p1 has two endpoints (m1a, m1b) sharing one provider
	// quota — len(st)==1 above already proves it collapsed to one entry
	// rather than one row per endpoint.
}

func TestQuotaStatus_EstimatedPct(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: https://example.com}
    api_key: k1
    quota:
      limits: [{metric: tokens, every: 1mo, since: 2026-01-01, amount: 100000}]
models:
  m1:
    endpoints: {openai-completions: [{providers: [p1], models: [m]}]}
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)
	l := snap.Models["openai-completions"]["m1"].Endpoints[0].Quota.Limits[0]
	ps := quota.PeriodStart(l, time.Now())
	// 100 exact + 100 estimated = 200 total, 100 of it estimated -> 50%.
	rt.Quota.Charge("p1", "tokens/1mo", ps, quota.Counters{Fresh: 100}, 0)
	rt.Quota.Charge("p1", "tokens/1mo", ps, quota.Counters{Fresh: 100}, 100)

	st := rt.QuotaStatus()
	if len(st) != 1 {
		t.Fatalf("got %d entries, want 1", len(st))
	}
	if st[0].Used != 200 {
		t.Fatalf("Used = %v, want 200", st[0].Used)
	}
	if st[0].EstimatedPct < 49 || st[0].EstimatedPct > 51 {
		t.Fatalf("EstimatedPct = %v, want ~50", st[0].EstimatedPct)
	}
}

// TestQuotaStatus_UsedIsRoundedForDisplay pins the P1-4 fix: repeated
// fractional charges (the same class of drift model_multipliers produces
// over many real requests) leave IEEE754 summation noise in the raw
// quota.Counters float64 — that's fine and expected internally (see
// quota.Counters' doc comment: it must stay unrounded to avoid a systematic
// charge-time bias) — but /status's QuotaProviderStatus is a display/API
// boundary, and 0.1+0.1+0.1 must read back as a clean 0.3, not
// 0.30000000000000004.
func TestQuotaStatus_UsedIsRoundedForDisplay(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: https://example.com}
    api_key: k1
    quota:
      limits: [{metric: tokens, every: 1mo, since: 2026-01-01, amount: 100000}]
models:
  m1:
    endpoints: {openai-completions: [{providers: [p1], models: [m]}]}
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)
	l := snap.Models["openai-completions"]["m1"].Endpoints[0].Quota.Limits[0]
	ps := quota.PeriodStart(l, time.Now())
	for i := 0; i < 3; i++ {
		rt.Quota.Charge("p1", "tokens/1mo", ps, quota.Counters{Fresh: 0.1}, 0)
	}

	st := rt.QuotaStatus()
	if len(st) != 1 {
		t.Fatalf("got %d entries, want 1", len(st))
	}
	if st[0].Used != 0.3 {
		t.Fatalf("Used = %v (%.20f), want a clean 0.3 — float summation noise leaked into the display value", st[0].Used, st[0].Used)
	}
	if st[0].Fresh != 0.3 {
		t.Fatalf("Fresh = %v, want a clean 0.3", st[0].Fresh)
	}
}
