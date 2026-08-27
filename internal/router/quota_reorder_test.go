// Ver 2026-08-07, by Opus 5

package router

import (
	"encoding/hex"
	"testing"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/strategy"
)

func priorityDims(t *testing.T) []strategy.Dimension {
	t.Helper()
	dims, err := strategy.Build([]string{"priority"})
	if err != nil {
		t.Fatal(err)
	}
	return dims
}

func epWithLimit(t *testing.T, provider string, priority int, l *core.Limit) *core.Endpoint {
	t.Helper()
	ep := &core.Endpoint{Provider: provider, AdapterType: "openai-completions", Model: provider + "-model", Priority: priority}
	if l != nil {
		ep.Quota = &core.QuotaSpec{Limits: []core.Limit{*l}}
	}
	return ep
}

// --- nil-safety / no-op cases ---

func TestReorderByQuota_NilRegistry_NoOp(t *testing.T) {
	l := requestsLimit(100)
	cands := []*core.Endpoint{epWithLimit(t, "a", 0, &l), epWithLimit(t, "b", 0, &l)}
	orig := append([]*core.Endpoint(nil), cands...)
	changed := reorderByQuota(cands, priorityDims(t), nil, chargeNow)
	if changed {
		t.Error("changed=true with nil Registry, want false")
	}
	for i := range cands {
		if cands[i] != orig[i] {
			t.Fatalf("cands mutated with nil Registry: %v", cands)
		}
	}
}

func TestReorderByQuota_EmptyCands_NoOp(t *testing.T) {
	reg := quota.NewRegistry("")
	if changed := reorderByQuota(nil, priorityDims(t), reg, chargeNow); changed {
		t.Error("changed=true for empty candidate list, want false")
	}
}

// --- tier boundaries never crossed ---

func TestReorderByQuota_NeverCrossesPriorityTiers(t *testing.T) {
	reg := quota.NewRegistry("")
	lHigh := requestsLimit(100) // priority 0, exhausted — score should be 0
	lLow := requestsLimit(100)  // priority 1, fresh — score should be high
	ps := quota.PeriodStart(lHigh, chargeNow)
	reg.Charge("high-tier", "requests/1mo", ps, quota.Counters{Requests: 100}, 0) // fully used

	cands := []*core.Endpoint{
		epWithLimit(t, "high-tier", 0, &lHigh), // priority 0: must stay first regardless of its bad score
		epWithLimit(t, "low-tier", 1, &lLow),   // priority 1: must stay second regardless of its good score
	}
	reorderByQuota(cands, priorityDims(t), reg, chargeNow)
	if cands[0].Provider != "high-tier" || cands[1].Provider != "low-tier" {
		t.Fatalf("quota reordering crossed a priority tier: %v/%v", cands[0].Provider, cands[1].Provider)
	}
}

// --- placeholder members (no quota configured) never move ---

func TestReorderByQuota_PlaceholderMembersUnchanged(t *testing.T) {
	reg := quota.NewRegistry("")
	lGood := requestsLimit(1000) // untouched: high headroom
	cands := []*core.Endpoint{
		epWithLimit(t, "no-quota-1", 0, nil),
		epWithLimit(t, "has-quota", 0, &lGood),
		epWithLimit(t, "no-quota-2", 0, nil),
	}
	reorderByQuota(cands, priorityDims(t), reg, chargeNow)
	// Only one quota-bearing member in this tier: nothing to reorder against
	// (reorderTier requires >=2), so positions must be untouched entirely.
	if cands[0].Provider != "no-quota-1" || cands[1].Provider != "has-quota" || cands[2].Provider != "no-quota-2" {
		t.Fatalf("single quota member's presence moved placeholders: %v/%v/%v", cands[0].Provider, cands[1].Provider, cands[2].Provider)
	}
}

func TestReorderByQuota_PlaceholderSlotsUntouchedAmongMultipleQuotaMembers(t *testing.T) {
	reg := quota.NewRegistry("")
	lBad := requestsLimit(100)
	lGood := requestsLimit(1000)
	ps := quota.PeriodStart(lBad, chargeNow)
	reg.Charge("worse", "requests/1mo", ps, quota.Counters{Requests: 99}, 0) // nearly exhausted -> low score

	// Slot layout: [no-quota, worse(quota), no-quota, better(quota)] — the
	// two placeholder slots (index 0 and 2) must never receive a quota
	// endpoint's value, and vice versa.
	cands := []*core.Endpoint{
		epWithLimit(t, "placeholder-1", 0, nil),
		epWithLimit(t, "worse", 0, &lBad),
		epWithLimit(t, "placeholder-2", 0, nil),
		epWithLimit(t, "better", 0, &lGood),
	}
	reorderByQuota(cands, priorityDims(t), reg, chargeNow)

	if cands[0].Provider != "placeholder-1" || cands[2].Provider != "placeholder-2" {
		t.Fatalf("placeholder slots were touched: %v", providerNames(cands))
	}
	// "better" (fresh, high headroom) must now sort ahead of "worse"
	// (nearly exhausted) among the quota-bearing slots (index 1 and 3).
	if cands[1].Provider != "better" || cands[3].Provider != "worse" {
		t.Fatalf("quota-bearing slots not reordered by score: %v", providerNames(cands))
	}
}

func providerNames(eps []*core.Endpoint) []string {
	names := make([]string, len(eps))
	for i, ep := range eps {
		names[i] = ep.Provider
	}
	return names
}

// --- descending score order within a tier ---

func TestReorderByQuota_DescendingScoreOrder(t *testing.T) {
	reg := quota.NewRegistry("")
	l := requestsLimit(1000)
	ps := quota.PeriodStart(l, chargeNow)
	reg.Charge("mostly-used", "requests/1mo", ps, quota.Counters{Requests: 900}, 0) // low headroom
	reg.Charge("half-used", "requests/1mo", ps, quota.Counters{Requests: 500}, 0)   // mid headroom
	// "fresh" gets no charge: highest headroom

	cands := []*core.Endpoint{
		epWithLimit(t, "mostly-used", 0, &l),
		epWithLimit(t, "fresh", 0, &l),
		epWithLimit(t, "half-used", 0, &l),
	}
	reorderByQuota(cands, priorityDims(t), reg, chargeNow)
	got := providerNames(cands)
	want := []string{"fresh", "half-used", "mostly-used"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// --- empty dims: whole candidate set is one tier (baseline fact #4) ---

func TestReorderByQuota_EmptyDims_WholeSetIsOneTier(t *testing.T) {
	reg := quota.NewRegistry("")
	l := requestsLimit(1000)
	ps := quota.PeriodStart(l, chargeNow)
	reg.Charge("used", "requests/1mo", ps, quota.Counters{Requests: 999}, 0)

	cands := []*core.Endpoint{
		epWithLimit(t, "used", 5, &l), // different Priority values, but dims=nil means priority is never consulted
		epWithLimit(t, "fresh", 1, &l),
	}
	reorderByQuota(cands, nil, reg, chargeNow)
	if cands[0].Provider != "fresh" {
		t.Fatalf("with empty dims chain, quota should freely reorder regardless of Priority field: %v", providerNames(cands))
	}
}

// --- score==0 (exhausted) endpoints are demoted, never evicted ---

func TestReorderByQuota_ExhaustedEndpointDemotedNotEvicted(t *testing.T) {
	reg := quota.NewRegistry("")
	l := requestsLimit(100)
	ps := quota.PeriodStart(l, chargeNow)
	reg.Charge("exhausted", "requests/1mo", ps, quota.Counters{Requests: 100}, 0)

	cands := []*core.Endpoint{
		epWithLimit(t, "exhausted", 0, &l),
		epWithLimit(t, "fresh", 0, &l),
	}
	before := len(cands)
	reorderByQuota(cands, priorityDims(t), reg, chargeNow)
	if len(cands) != before {
		t.Fatalf("candidate set size changed: %d -> %d (quota must only reorder, never evict)", before, len(cands))
	}
	if cands[1].Provider != "exhausted" {
		t.Fatalf("exhausted endpoint = %v, want it demoted to last, still present", providerNames(cands))
	}
}

// --- the §1.1 misaligned-reset-day scenario, end to end through Serve ---

func TestServe_QuotaReordering_MisalignedResetDays(t *testing.T) {
	uA := newMockUpstream(t, 200, `{"id":"a"}`)
	uB := newMockUpstream(t, 200, `{"id":"b"}`)
	uC := newMockUpstream(t, 200, `{"id":"c"}`)

	// Plans A/B/C each get one monthly Limit; "now" for the test is real
	// time.Now() (Serve calls it directly), so the anchors below are
	// expressed relative to today rather than a fixed calendar date —
	// what matters is each account's CONSUMPTION relative to its own
	// period progress, exactly like the design doc's worked example.
	cfg := mustConfig(t, fmtQuotaCfg(uA.srv.URL, uB.srv.URL, uC.srv.URL))
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)

	now := time.Now()
	route := snap.Models["openai-completions"]["vm"]
	lA := route.Endpoints[0].Quota.Limits[0]
	lB := route.Endpoints[1].Quota.Limits[0]
	lC := route.Endpoints[2].Quota.Limits[0]

	// A: 50% used, roughly on pace (its own since anchor puts it near
	// mid-cycle) — charge to ~50% of amount.
	rt.Quota.Charge("plan-a", "requests/1mo", quota.PeriodStart(lA, now), quota.Counters{Requests: 500}, 0)
	// B: just reset, charge almost nothing — looks "most remaining" under a
	// naive remaining/total heuristic.
	rt.Quota.Charge("plan-b", "requests/1mo", quota.PeriodStart(lB, now), quota.Counters{Requests: 10}, 0)
	// C: charge 60% of a much smaller remaining-time window (its since
	// anchor is set close to now via config, so time_left_frac is small) —
	// headroom must still rank it above A and B despite lower absolute
	// remaining quota, because it's about to forfeit unused quota.
	rt.Quota.Charge("plan-c", "requests/1mo", quota.PeriodStart(lC, now), quota.Counters{Requests: 600}, 0)

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	endpoint := w.Header().Get("X-VMR-Endpoint")
	if endpoint != "openai-completions/plan-c/mc" {
		t.Fatalf("winning endpoint = %s, want openai-completions/plan-c/mc (plan-c has the least time left relative to its usage)", endpoint)
	}
	if got := w.Header().Get("X-VMR-Route-Reason"); got == "" || !containsSubstr(got, "pick=quota") {
		t.Errorf("X-VMR-Route-Reason = %q, want it to show pick=quota", got)
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// fmtQuotaCfg builds a 3-provider config where each provider's monthly
// Limit anchor (`since`) is deliberately staggered so each account's
// time_left_frac differs sharply at "now" — reproducing the design doc's
// §1.1 scenario (reset days 1/15/20, evaluated on the 16th) without
// depending on which calendar month the test actually runs in.
func fmtQuotaCfg(urlA, urlB, urlC string) string {
	now := time.Now()
	// A: anchored a full period back plus a few days -> mid-cycle.
	sinceA := now.AddDate(0, 0, -15).Format("2006-01-02")
	// B: anchored essentially now -> just reset, almost all time left.
	sinceB := now.Format("2006-01-02")
	// C: anchored so only ~2 days of a 30-day cycle remain -> almost all
	// time elapsed, matching the design doc's "about to forfeit" case.
	sinceC := now.AddDate(0, 0, -28).Format("2006-01-02")
	return `
listen: 127.0.0.1:0
providers:
  - name: plan-a
    base_url: {openai-completions: ` + urlA + `}
    api_key: ka
    quota:
      limits: [{metric: requests, every: 1mo, since: ` + sinceA + `, amount: 1000}]
  - name: plan-b
    base_url: {openai-completions: ` + urlB + `}
    api_key: kb
    quota:
      limits: [{metric: requests, every: 1mo, since: ` + sinceB + `, amount: 1000}]
  - name: plan-c
    base_url: {openai-completions: ` + urlC + `}
    api_key: kc
    quota:
      limits: [{metric: requests, every: 1mo, since: ` + sinceC + `, amount: 1000}]
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [plan-a], models: [ma]}
      - {protocol: openai-completions, providers: [plan-b], models: [mb]}
      - {protocol: openai-completions, providers: [plan-c], models: [mc]}
`
}

// --- Sticky overrides quota reordering ---

func TestServe_StickyOverridesQuotaReordering(t *testing.T) {
	u1 := newMockUpstream(t, 200, `{"id":"first"}`)
	u2 := newMockUpstream(t, 200, `{"id":"second"}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: `+u1.srv.URL+`}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1mo, since: 2020-01-01, amount: 1000}]
  - name: p2
    base_url: {openai-completions: `+u2.srv.URL+`}
    api_key: k2
    quota:
      limits: [{metric: requests, every: 1mo, since: 2020-01-01, amount: 1000}]
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
      - {protocol: openai-completions, providers: [p2], models: [m2]}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)

	now := time.Now()
	l1 := snap.Models["openai-completions"]["vm"].Endpoints[0].Quota.Limits[0]
	// Exhaust p1 so quota reordering alone would push p2 to the front.
	rt.Quota.Charge("p1", "requests/1mo", quota.PeriodStart(l1, now), quota.Counters{Requests: 1000}, 0)

	// Prime the sticky pointer to p1 directly, bypassing a first real
	// request (whose fingerprinting depends on request shape details this
	// test doesn't need to reproduce).
	ep1 := snap.Models["openai-completions"]["vm"].Endpoints[0]
	req := []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`)
	// First call establishes the sticky pointer (goes to whichever quota
	// ranks first — p2, since p1 is exhausted).
	w1 := serveReq(rt, "vm", req)
	if w1.Code != 200 {
		t.Fatalf("first call status=%d body=%s", w1.Code, w1.Body)
	}
	// Force the sticky registry to point at p1 (the quota-disfavored one)
	// to prove sticky, not quota, decides the next call. Replicates Serve's
	// own stickyKey computation (clientKeyTag "" since serveReq passes a
	// nil audit.Record) so the lookup actually hits.
	sysHash, firstMsgHash, ok := adapter.SessionFingerprint(req, "openai-completions")
	if !ok {
		t.Fatal("SessionFingerprint failed on test request body")
	}
	stickyKey := ":" + hex.EncodeToString(sysHash[:]) + ":" + hex.EncodeToString(firstMsgHash[:])
	rt.Sticky.Set(stickyKey, ep1.HealthKey())

	w2 := serveReq(rt, "vm", req)
	if w2.Code != 200 {
		t.Fatalf("second call status=%d body=%s", w2.Code, w2.Body)
	}
	if got := w2.Header().Get("X-VMR-Endpoint"); got != "openai-completions/p1/m1" {
		t.Fatalf("second call endpoint = %s, want openai-completions/p1/m1 (sticky must override quota's demotion)", got)
	}
}
