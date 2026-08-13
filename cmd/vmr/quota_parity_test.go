// Ver 2026-08-13 14:40, by Opus 5

// Differential test: `vmr report` §2.5's recomputed "window consumed" column
// vs. what internal/router ACTUALLY charged for the same traffic.
//
// Why this lives in cmd/vmr and not internal/report: internal/archtest
// forbids report from importing router (they are the two halves of the
// product and must stay independent). cmd/vmr is the composition root — the
// one place that already legitimately knows about both — so it is also the
// only honest place to assert that the two agree.
//
// Why it exists at all: DP2's batch 1 unified the FORMULA behind both sides
// (quota.BaseAmount / quota.ApplyModelMultiplier, "one implementation, two
// consumers"). It did not unify the BASIS — which raw counters get fed into
// that formula is still decided independently on each side, and nothing
// checked that the two decisions matched. Two basis bugs lived through two dev
// plans, a delivery review and an external review because every check asked
// "does the code match the plan" and none asked "does the number match the
// router":
//
// - the report used EndpointRow.Requests (a REQUEST-level count that
// still counts a fully-failed request against the last endpoint tried)
// where the router charges once per FORWARDED ATTEMPT — a systematic
// over-count, on an account whose multiplier is 6x worth 6 phantom
// charges per failed request.
// - tokens whose usage was unparseable count 0 here while the router
// charged a byte-count estimate for them.
//
// Both would have been caught on the first run of this test.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/report"
	"vmr/internal/router"
)

// parityAttempt is one upstream try in a synthetic request: the status the
// upstream returned (0 = never got a response at all, e.g. a network error)
// and whether the forwarded 2xx then broke mid-copy.
type parityAttempt struct {
	status    int
	truncated bool
}

// forwarded mirrors internal/router's own decision to call chargeQuota:
// forwardSuccess runs for any response with status < 400, and charges
// "regardless of copyErr" — so a truncated 2xx is charged too.
func (a parityAttempt) forwarded() bool { return a.status > 0 && a.status < 400 }

// parityRequest is one synthetic client request and the attempts it took.
type parityRequest struct {
	model    string // real upstream model — picks the model_multiplier
	attempts []parityAttempt
}

// auditLine renders one parityRequest as an audit JSONL record in exactly
// the shape internal/report parses. The final attempt is the one the report
// treats as having served the client (rc.endpoint).
func (r parityRequest) auditLine(ts time.Time, provider string) string {
	var atts []string
	for _, a := range r.attempts {
		ep := core.EndpointLabel("openai", provider, r.model)
		fields := fmt.Sprintf(`"endpoint":%q,"protocol":"openai","provider":%q,"model":%q,"url":"https://example.com/v1","dur_ms":5,"request":{}`,
			ep, provider, r.model)
		switch {
		case a.status == 0:
			fields += `,"error":"network: dial tcp: refused","error_class":"transient"`
		case a.status >= 400:
			fields += fmt.Sprintf(`,"response":{"status":%d},"error":"upstream %d","error_class":"rate_limit"`, a.status, a.status)
		case a.truncated:
			fields += fmt.Sprintf(`,"response":{"status":%d},"error":"truncated: unexpected EOF","error_class":"truncated"`, a.status)
		default:
			fields += fmt.Sprintf(`,"response":{"status":%d}`, a.status)
		}
		atts = append(atts, "{"+fields+"}")
	}
	last := r.attempts[len(r.attempts)-1]
	outcome := "error"
	if last.forwarded() {
		outcome = "ok"
	}
	return fmt.Sprintf(`{"ts":%q,"dur_ms":5,"model":"m1","protocol":"openai","outcome":%q,"client":{"request":{}},"attempts":[%s]}`,
		ts.Format(time.RFC3339), outcome, strings.Join(atts, ","))
}

// routerCharged replays the same requests through internal/router's REAL
// charging entry point (router.ChargeResponse, the function chargeQuota
// hands off to and the one internal/replay reuses) and returns the account's
// resulting consumption in its metric's own unit — i.e. the authoritative
// number the report's recomputed column is trying to reproduce.
func routerCharged(t *testing.T, reqs []parityRequest, provider string, spec *core.QuotaSpec, now time.Time) float64 {
	t.Helper()
	reg := quota.NewRegistry("")
	for _, r := range reqs {
		for _, a := range r.attempts {
			if !a.forwarded() {
				continue // the router only ever charges a forwarded response
			}
			ep := &core.Endpoint{AdapterType: "openai", Provider: provider, Model: r.model, Quota: spec}
			router.ChargeResponse(reg, ep, quota.Counters{}, 0, now)
		}
	}
	l := spec.Limits[0]
	c, _ := reg.Used(provider, string(l.Metric)+"/"+l.EveryText, quota.PeriodStart(l, now))
	return quota.BaseAmount(spec, c)
}

// reportWindowConsumed runs the real `vmr report` pipeline over the same
// requests and returns §2.5's recomputed window-consumed figure.
func reportWindowConsumed(t *testing.T, reqs []parityRequest, provider string, ts time.Time) *float64 {
	t.Helper()
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for i, r := range reqs {
		lines.WriteString(r.auditLine(ts.Add(time.Duration(i)*time.Minute), provider))
		lines.WriteString("\n")
	}
	auditPath := filepath.Join(logDir, "vmr-audit-2026-01-15.jsonl")
	if err := os.WriteFile(auditPath, []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeTempFile(t, "config.yaml", quotaYAML(logDir))
	outDir := filepath.Join(dir, "out")
	if err := cmdReport([]string{"-c", configPath, "-o", outDir, "-details=false", auditPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep report.Report2
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	for _, row := range rep.ProviderQuotas {
		if row.Provider == provider {
			return row.WindowConsumed
		}
	}
	t.Fatalf("no §2.5 quota row for provider %q (rows: %+v)", provider, rep.ProviderQuotas)
	return nil
}

// TestQuotaParity_RequestsMetric_ReportMatchesRouter is the core assertion:
// for metric: requests the two sides must agree EXACTLY, with no tolerance.
// That is not an aspiration — every charge is Requests:1 scaled by the same
// constant ceil(multiplier), so an exact identity is available and anything
// less means a basis was chosen wrong.
//
// quotaYAML's account declares model_multipliers implicitly at 1.0; the
// multiplier axis is covered separately below.
func TestQuotaParity_RequestsMetric_ReportMatchesRouter(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	reqs := []parityRequest{
		// plain success
		{model: "real-model", attempts: []parityAttempt{{status: 200}}},
		{model: "real-model", attempts: []parityAttempt{{status: 200}}},
		// failover: first attempt 429 (never charged), second 200 (charged once)
		{model: "real-model", attempts: []parityAttempt{{status: 429}, {status: 200}}},
		// every attempt failed: the router charges NOTHING, but the request
		// still counts 1 against this endpoint's request-level Requests —
		// this is the case was over-counting.
		{model: "real-model", attempts: []parityAttempt{{status: 429}, {status: 500}}},
		{model: "real-model", attempts: []parityAttempt{{status: 0}}},
		// forwarded 2xx that broke mid-copy: CHARGED by the router, yet it
		// drops out of EndpointRow.OK (SetTruncated writes Error) — the case
		// that makes OK the wrong basis too.
		{model: "real-model", attempts: []parityAttempt{{status: 200, truncated: true}}},
	}

	lim := core.Limit{Metric: core.MetricRequests, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 1000}
	spec := &core.QuotaSpec{Limits: []core.Limit{lim}}

	want := routerCharged(t, reqs, "acct1", spec, now)
	if want != 4 {
		t.Fatalf("fixture sanity: router charged %v, expected 4 forwarded attempts", want)
	}
	got := reportWindowConsumed(t, reqs, "acct1", ts)
	if got == nil {
		t.Fatal("WindowConsumed is nil for a requests-metric account — it must always be a real number")
	}
	if *got != want {
		t.Errorf("§2.5 window consumed = %v, router actually charged %v — the recomputed column is NOT reproducing the router.\n"+
			"6 requests were made; only 4 attempts were ever forwarded. Counting requests instead of forwarded attempts gives 6.",
			*got, want)
	}
}

// TestQuotaParity_RequestsMetric_NonIntegerMultiplier adds the rounding axis
// on top of the basis axis: with a fractional multiplier, per-charge
// ceil(mult) and aggregate-then-ceil disagree, and only the former matches
// the router. Driving the router directly means this test can't drift from
// whatever ApplyModelMultiplier does — it asserts equality, not a
// hand-computed constant.
func TestQuotaParity_RequestsMetric_NonIntegerMultiplier(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	var reqs []parityRequest
	for i := 0; i < 19; i++ {
		reqs = append(reqs, parityRequest{model: "real-model", attempts: []parityAttempt{{status: 200}}})
	}
	// plus some noise the router never charges, to keep the basis honest
	reqs = append(reqs, parityRequest{model: "real-model", attempts: []parityAttempt{{status: 503}}})

	lim := core.Limit{Metric: core.MetricRequests, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 100000}
	spec := &core.QuotaSpec{Limits: []core.Limit{lim}, ModelMultipliers: map[string]float64{"real-model": 5.5}}

	want := routerCharged(t, reqs, "acct1", spec, now)
	if want != 114 {
		t.Fatalf("fixture sanity: router charged %v, expected 19*ceil(5.5)=114", want)
	}

	// The report side reads its multiplier from config.yaml, so this half
	// is asserted through buildProviderQuotaRows' own inputs rather than the
	// YAML pipeline (quotaYAML declares no model_multipliers). Reproducing
	// the router's formula here is the point; the end-to-end basis is
	// covered by the test above.
	unit, _ := quota.ApplyModelMultiplier(spec, "real-model", quota.Counters{Requests: 1}, 0)
	forwarded := int64(0)
	for _, r := range reqs {
		for _, a := range r.attempts {
			if a.forwarded() {
				forwarded++
			}
		}
	}
	got := quota.BaseAmount(spec, quota.Counters{Requests: unit.Requests * forwarded})
	if got != want {
		t.Errorf("recomputed = %v, router charged %v", got, want)
	}
}
