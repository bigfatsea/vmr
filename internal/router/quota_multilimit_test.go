// Ver 2026-08-22, by Sonnet 5

// Tests for P3's multi-Limit-per-provider support: charging every applicable
// Limit on a successful response (ChargeResponse/applicableLimits), Scope
// (`models:`) filtering which Limits an endpoint's charges/score interact
// with, and reorderByQuota's bucket-vs-gate merge across more than one
// Limit. See docs/VirtualModelRouter_Design_v4_Quota.md's §5.2/§9.1.
package router

import (
	"bytes"
	"testing"

	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/respnorm"
)

// TestChargeResponse_MultiLimit_ChargesEveryApplicableLimit pins the P3
// fan-out: one successful response charges EVERY Limit on the provider
// whose Scope covers the endpoint's model, not just the first one.
func TestChargeResponse_MultiLimit_ChargesEveryApplicableLimit(t *testing.T) {
	reg := quota.NewRegistry("")
	short := requestsLimit(100)
	short.EveryUnit, short.EveryText, short.Amount = "d", "1d", 100
	long := requestsLimit(90000)
	spec := &core.QuotaSpec{Limits: []core.Limit{short, long}}
	ep := &core.Endpoint{Provider: "p1", Model: "m", Quota: spec}

	ChargeResponse(reg, ep, quota.Counters{}, 0, chargeNow)

	usedShort, _ := reg.Used("p1", quota.LimitKey(short, ""), quota.PeriodStart(short, chargeNow))
	usedLong, _ := reg.Used("p1", quota.LimitKey(long, ""), quota.PeriodStart(long, chargeNow))
	if usedShort.Requests != 1 {
		t.Fatalf("short-window Requests = %v, want 1", usedShort.Requests)
	}
	if usedLong.Requests != 1 {
		t.Fatalf("long-window Requests = %v, want 1 — every applicable Limit must be charged, not just the first", usedLong.Requests)
	}
}

// TestChargeResponse_Scope_OnlyMatchingModelCharged pins Scope: a Limit
// scoped to specific models is charged only when the endpoint's model is
// one of them; an unscoped Limit on the same provider is charged
// regardless.
func TestChargeResponse_Scope_OnlyMatchingModelCharged(t *testing.T) {
	reg := quota.NewRegistry("")
	scoped := requestsLimit(200)
	scoped.EveryUnit, scoped.EveryText = "d", "1d"
	scoped.Models = []string{"premium-model"}
	unscoped := requestsLimit(90000)
	spec := &core.QuotaSpec{Limits: []core.Limit{scoped, unscoped}}

	epPremium := &core.Endpoint{Provider: "p1", Model: "premium-model", Quota: spec}
	epOther := &core.Endpoint{Provider: "p1", Model: "other-model", Quota: spec}

	ChargeResponse(reg, epPremium, quota.Counters{}, 0, chargeNow)
	ChargeResponse(reg, epOther, quota.Counters{}, 0, chargeNow)

	usedScoped, _ := reg.Used("p1", quota.LimitKey(scoped, "premium-model"), quota.PeriodStart(scoped, chargeNow))
	if usedScoped.Requests != 1 {
		t.Fatalf("scoped Limit Requests = %v, want 1 (only premium-model's charge applies)", usedScoped.Requests)
	}
	usedUnscoped, _ := reg.Used("p1", quota.LimitKey(unscoped, ""), quota.PeriodStart(unscoped, chargeNow))
	if usedUnscoped.Requests != 2 {
		t.Fatalf("unscoped Limit Requests = %v, want 2 (both endpoints' charges apply)", usedUnscoped.Requests)
	}
}

// TestReorderByQuota_ScopedLimit_TreatsNonMatchingEndpointAsUnmetered pins
// the "占位重排" placeholder-reorder rule extended to Scope: an endpoint
// whose provider has quota: configured, but none of whose Limits cover its
// own model, must be left exactly where Sort put it — same as having no
// quota: configured at all.
func TestReorderByQuota_ScopedLimit_TreatsNonMatchingEndpointAsUnmetered(t *testing.T) {
	reg := quota.NewRegistry("")
	scoped := requestsLimit(100)
	scoped.Models = []string{"other-model"}
	spec := &core.QuotaSpec{Limits: []core.Limit{scoped}}

	// Charge the scoped Limit heavily (irrelevant, since epA's model never
	// matches it) to prove epA's score is untouched by it.
	reg.Charge("p1", quota.LimitKey(scoped, "other-model"), quota.PeriodStart(scoped, chargeNow), quota.Counters{Requests: 99}, 0)

	epA := &core.Endpoint{Provider: "p1", Model: "unrelated-model", Quota: spec}
	epB := &core.Endpoint{Provider: "p1", Model: "unrelated-model", Quota: nil}
	cands := []*core.Endpoint{epA, epB}

	changed := reorderByQuota(cands, nil, reg, chargeNow)
	if changed {
		t.Fatalf("reorderByQuota changed order, want no-op — neither candidate has an applicable Limit")
	}
	if cands[0] != epA {
		t.Fatalf("order = %v, want epA first (untouched — no applicable Limit for its model)", cands)
	}
}

// TestReorderByQuota_MultiLimit_GatePicksTighterConstraint pins the
// bucket-vs-gate merge (quota.ScoreForLimits) actually driving reorder
// decisions: two providers share the same healthy monthly bucket, but one
// also carries a nearly-exhausted daily gate — that provider must sort
// second despite its bucket being identical.
func TestReorderByQuota_MultiLimit_GatePicksTighterConstraint(t *testing.T) {
	reg := quota.NewRegistry("")
	bucket := requestsLimit(1000) // both providers: on-pace monthly bucket (nothing charged, mid-month)

	tightGate := requestsLimit(100)
	tightGate.EveryUnit, tightGate.EveryText = "d", "1d"

	specHealthy := &core.QuotaSpec{Limits: []core.Limit{bucket}}
	specGated := &core.QuotaSpec{Limits: []core.Limit{bucket, tightGate}}

	// Nearly exhaust the gate on the second provider only.
	reg.Charge("gated", quota.LimitKey(tightGate, ""), quota.PeriodStart(tightGate, chargeNow), quota.Counters{Requests: 99}, 0)

	epHealthy := &core.Endpoint{Provider: "healthy", Model: "m", Quota: specHealthy}
	epGated := &core.Endpoint{Provider: "gated", Model: "m", Quota: specGated}
	cands := []*core.Endpoint{epGated, epHealthy} // deliberately gated-first, so a no-op would leave it first

	reorderByQuota(cands, nil, reg, chargeNow)

	if cands[0] != epHealthy {
		t.Fatalf("order = %v, want epHealthy first (epGated's daily gate is nearly exhausted)", cands)
	}
}

// TestChargeResponse_PerModelWildcard_IndependentBuckets pins the core
// per-model behavior change: a Limit scoped to models: ["*"] gives EVERY
// model its own independent bucket, not one shared pool — charging two
// different models must not let one's consumption count against the
// other's.
func TestChargeResponse_PerModelWildcard_IndependentBuckets(t *testing.T) {
	reg := quota.NewRegistry("")
	l := requestsLimit(100)
	l.EveryUnit, l.EveryText = "min", "1min"
	l.Models = []string{"*"}
	spec := &core.QuotaSpec{Limits: []core.Limit{l}}

	epA := &core.Endpoint{Provider: "p1", Model: "model-a", Quota: spec}
	epB := &core.Endpoint{Provider: "p1", Model: "model-b", Quota: spec}
	ChargeResponse(reg, epA, quota.Counters{}, 0, chargeNow)
	ChargeResponse(reg, epA, quota.Counters{}, 0, chargeNow)
	ChargeResponse(reg, epB, quota.Counters{}, 0, chargeNow)

	usedA, _ := reg.Used("p1", quota.LimitKey(l, "model-a"), quota.PeriodStart(l, chargeNow))
	usedB, _ := reg.Used("p1", quota.LimitKey(l, "model-b"), quota.PeriodStart(l, chargeNow))
	if usedA.Requests != 2 {
		t.Fatalf("model-a Requests = %v, want 2", usedA.Requests)
	}
	if usedB.Requests != 1 {
		t.Fatalf("model-b Requests = %v, want 1 — must not have inherited model-a's charges from a shared bucket", usedB.Requests)
	}
}

// TestQuotaStatus_PerModelWildcard_OneRowPerActuallyChargedModel is the
// end-to-end version through Router.QuotaStatus: a wildcard per-model Limit
// produces one row per model that has ACTUALLY been charged — no rows for
// models that merely could match the wildcard but never sent traffic.
func TestQuotaStatus_PerModelWildcard_OneRowPerActuallyChargedModel(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1min, amount: 60, models: ["*"]}]
models:
  m1:
    endpoints: [{protocol: openai, providers: [p1], models: [model-a, model-b]}]
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)
	epA := snap.Models["openai"]["m1"].Endpoints[0]
	epB := snap.Models["openai"]["m1"].Endpoints[1]
	if epA.Model == "model-b" {
		epA, epB = epB, epA
	}

	ChargeResponse(rt.Quota, epA, quota.Counters{}, 0, chargeNow)
	ChargeResponse(rt.Quota, epA, quota.Counters{}, 0, chargeNow)
	ChargeResponse(rt.Quota, epB, quota.Counters{}, 0, chargeNow)

	st := rt.QuotaStatus()
	if len(st) != 2 {
		t.Fatalf("QuotaStatus rows = %d, want 2 (one per actually-charged model), got %+v", len(st), st)
	}
	byModel := map[string]QuotaProviderStatus{}
	for _, row := range st {
		if len(row.Models) != 1 {
			t.Fatalf("row.Models = %v, want exactly one specific model per row", row.Models)
		}
		byModel[row.Models[0]] = row
		if row.Role != "bucket" {
			t.Errorf("row.Role = %q, want %q (this provider's only Limit is trivially its own bucket)", row.Role, "bucket")
		}
	}
	if byModel["model-a"].Used != 2 {
		t.Errorf("model-a Used = %v, want 2", byModel["model-a"].Used)
	}
	if byModel["model-b"].Used != 1 {
		t.Errorf("model-b Used = %v, want 1", byModel["model-b"].Used)
	}
}

// TestQuotaStatus_PerModelRestrictedList_OnlyListedModelsCanProduceRows
// pins Scope membership still applies under per-model accounting: a
// restricted list only ever produces rows for its named models, even if
// other models on the provider also see traffic (those charges never reach
// this Limit at all — see applicableLimits).
func TestQuotaStatus_PerModelRestrictedList_OnlyListedModelsCanProduceRows(t *testing.T) {
	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1d, amount: 200, models: [premium-model]}]
models:
  m1:
    endpoints: [{protocol: openai, providers: [p1], models: [premium-model, other-model]}]
`)
	snap := mustSnapshot(t, cfg)
	rt.Install(snap)
	epPremium := snap.Models["openai"]["m1"].Endpoints[0]
	epOther := snap.Models["openai"]["m1"].Endpoints[1]
	if epPremium.Model != "premium-model" {
		epPremium, epOther = epOther, epPremium
	}

	ChargeResponse(rt.Quota, epPremium, quota.Counters{}, 0, chargeNow)
	ChargeResponse(rt.Quota, epOther, quota.Counters{}, 0, chargeNow) // must not produce a row — out of Scope

	st := rt.QuotaStatus()
	if len(st) != 1 {
		t.Fatalf("QuotaStatus rows = %d, want 1 (only premium-model is in Scope), got %+v", len(st), st)
	}
	if st[0].Models[0] != "premium-model" || st[0].Used != 1 {
		t.Fatalf("row = %+v, want premium-model with Used=1", st[0])
	}
}

func TestChargeQuota_MultiLimit_TokensOnlyFetchedWhenSomeLimitNeedsIt(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	req := requestsLimit(1000) // metric: requests — never needs usage extraction
	spec := &core.QuotaSpec{Limits: []core.Limit{req}}
	ep := &core.Endpoint{Provider: "p1", Model: "m", Quota: spec}
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})

	rt.chargeQuota(ep, rbody, &core.CanonicalRequest{}, chargeNow)

	used, _ := rt.Quota.Used("p1", quota.LimitKey(req, ""), quota.PeriodStart(req, chargeNow))
	if used.Requests != 1 {
		t.Fatalf("Requests = %v, want 1", used.Requests)
	}
}
