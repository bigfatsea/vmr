// Ver 2026-09-09, by pi

// Attempt.Tokens / Attempt.KeyLabel stamping: the LiveStats design doc's
// §3.1 前置改动. Stamping happens at forwardSuccess (the same point that
// flips Forwarded), so the assertions here ride the full real pipeline —
// adapter → router → respnorm → recorder — via the shared server test
// scaffolding, not a unit-level fake.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/quota"
	"vmr/internal/router"
)

// TestAttemptKeyLabelTailAndExclusion walks a failover (p1 500s → p2 wins)
// and asserts: (a) the winning attempt of a plain api_key provider carries
// the key's last-6 tail as key_label; (b) the failed attempt carries no
// key_label and no Tokens (stamping is Forwarded-gated — same point as
// SetForwarded).
func TestAttemptKeyLabelTailAndExclusion(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(500)
	// twoEndpointYAML's p1/p2 use api_key k1/k2 (tail = "k1"/"k2" whole,
	// being shorter than 6); the failover asserts exclusion + tail at once.
	ts, al := newAuditedServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))
	if _, body := chat(t, ts, simpleReq, nil); body == "" {
		t.Fatal("empty response")
	}
	recs := readRecords(t, al)
	if len(recs) != 1 {
		t.Fatalf("records: %d", len(recs))
	}
	atts := recs[0].Attempts
	if len(atts) != 2 {
		t.Fatalf("attempts: %d", len(atts))
	}
	if atts[0].Forwarded || atts[0].Tokens != nil || atts[0].KeyLabel != "" {
		t.Errorf("failed attempt must be unstamped: forwarded=%v tokens=%v key_label=%q",
			atts[0].Forwarded, atts[0].Tokens, atts[0].KeyLabel)
	}
	if !atts[1].IsForwarded() {
		t.Fatalf("winning attempt not forwarded")
	}
	if got, want := atts[1].KeyLabel, "k2"; got != want {
		t.Errorf("key_label = %q, want %q (plain api_key shorter than 6 stays whole)", got, want)
	}
	if atts[1].Tokens == nil {
		t.Fatal("winning attempt has no Tokens stamp")
	}
}

// TestAttemptKeyLabelFromLabeledAPIKeys pins the label rule: a provider
// expanded from a labeled api_keys map stamps the label itself, not a tail,
// while Attempt.Provider keeps the expanded name.
func TestAttemptKeyLabelFromLabeledAPIKeys(t *testing.T) {
	u := newUpstream(t)
	yaml := `listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: ` + u.srv.URL + `}
    api_keys:
      main: sk-provider-main-key-000123
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
`
	ts, al := newAuditedServer(t, yaml)
	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	recs := readRecords(t, al)
	if len(recs) != 1 || len(recs[0].Attempts) != 1 {
		t.Fatalf("records/attempts: %d/%d", len(recs), len(recs[0].Attempts))
	}
	att := recs[0].Attempts[0]
	if got, want := att.KeyLabel, "main"; got != want {
		t.Errorf("key_label = %q, want %q (label verbatim from api_keys)", got, want)
	}
	if got, want := att.Provider, "p1-main"; got != want {
		t.Errorf("provider = %q, want expanded %q", got, want)
	}
	if att.Tokens == nil {
		t.Fatal("no Tokens stamp on forwarded attempt")
	}
}

// TestAttemptTokensParityWithQuotaCharge is the LiveStats §3.1 differential
// pin: the stamp written to the audit record must be the SAME raw counters
// the quota ledger was charged with — same source (the upstream-side
// normalizer's usage), same instant, read back from two independent
// consumers. The upstream body carries a real usage object, so the charge
// is exact (estimated=0) and every component is comparable component-wise.
// Quota weights stay at their all-1.0 default so the ledger's stored
// counters ARE the raw values (a weighted ledger would only be comparable
// through ApplyModelMultiplier, which is not the stamp's basis).
func TestAttemptTokensParityWithQuotaCharge(t *testing.T) {
	// Custom upstream: a success body WITH an OpenAI usage object
	// (prompt 100, of which 30 cached; completion 50). The router's
	// upstream-side fold must see exactly this.
	usageBody := `{"id":"x","object":"chat.completion","model":"upstream-model","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":30}}}`
	u := newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(usageBody))
	})
	yaml := oneProviderYAML(u.URL)
	// Attach a tokens quota so chargeQuota actually bills this provider.
	yaml = `listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: ` + u.URL + `}
    api_key: k1
    quota:
      limits: [{metric: tokens, every: 1mo, amount: 1000000}]
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Quota = quota.NewRegistry("") // no persistence; in-memory ledger only
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Close() })
	ts := httptest.NewServer(New(rt, al).Handler())
	t.Cleanup(ts.Close)

	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	recs := readRecords(t, al)
	if len(recs) != 1 || len(recs[0].Attempts) != 1 {
		t.Fatalf("records/attempts: %d/%d", len(recs), len(recs[0].Attempts))
	}
	stamp := recs[0].Attempts[0].Tokens
	if stamp == nil {
		t.Fatal("no Tokens stamp on forwarded attempt")
	}

	// Ledger side: read the raw counters the charge deposited (default
	// all-1.0 token weights ⇒ stored counters are the raw values).
	spec := router.BuildQuotaSpecs(cfg.Providers)["p1"]
	if spec == nil || len(spec.Limits) == 0 {
		t.Fatal("quota spec missing")
	}
	l := spec.Limits[0]
	used, _ := rt.Quota.Used("p1", quota.LimitKey(l, ""), quota.PeriodStart(l, time.Now()))
	ledger := audit.TokenCount{In: int64(used.Fresh), Out: int64(used.Out), CacheRead: int64(used.CacheRead), CacheWrite: int64(used.CacheWrite)}
	if *stamp != ledger {
		t.Errorf("stamp %+v != ledger %+v — the stamp and the charge must come from one source", *stamp, ledger)
	}
	// And the components must be the usage the upstream reported (the
	// exact path: no degraded estimate may leak into an exact charge).
	want := audit.TokenCount{In: 70, Out: 50, CacheRead: 30}
	if *stamp != want {
		t.Errorf("stamp = %+v, want %+v (exact fold of the upstream usage)", *stamp, want)
	}
}
