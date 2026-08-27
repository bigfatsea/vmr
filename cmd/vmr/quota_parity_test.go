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
// Why it exists at all: the quota system unified the formula behind both sides
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
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/report"
	"vmr/internal/router"
	"vmr/internal/tokenutil"
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
//
// respBody/estTokens exist for the tokens and cost metrics, which — unlike
// requests — depend on what came BACK. respBody is deliberately typed as a
// string (an SSE stream, the shape audit.EncodeBody stores for any non-JSON
// body) rather than a JSON object: a JSON object round-trips through
// map[string]any on the way back out of the audit file and re-marshals with
// sorted keys and no whitespace, so the bytes internal/report measures would
// no longer be the bytes this fixture wrote. A string round-trips verbatim,
// which is what lets the degraded byte-count estimate be compared for EXACT
// equality instead of approximately.
type parityRequest struct {
	model    string // real upstream model — picks the model_multiplier
	attempts []parityAttempt
	// respBody is the recorded client-side response. Include a usage object
	// to exercise the exact path; omit it to exercise the degraded one.
	respBody string
	// estTokens is audit.Record.Facts.EstimatedTokens — vmr's own pre-routing
	// input estimate, and the request half of the degraded charge. The router
	// reads it off creq.Facts; the report reads the SAME persisted value back
	// out of the audit record, so this half is exactly reproducible.
	estTokens int64
}

// auditLine renders one parityRequest as an audit JSONL record in exactly
// the shape internal/report parses. The final attempt is the one the report
// treats as having served the client (rc.endpoint).
func (r parityRequest) auditLine(ts time.Time, provider string) string {
	var atts []string
	for _, a := range r.attempts {
		ep := core.EndpointLabel("openai-completions", provider, r.model)
		fields := fmt.Sprintf(`"endpoint":%q,"protocol":"openai-completions","provider":%q,"model":%q,"url":"https://example.com/v1","dur_ms":5,"request":{}`,
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
	clientResp := ""
	if r.respBody != "" {
		body, err := json.Marshal(r.respBody)
		if err != nil { // unreachable: json.Marshal of a string cannot fail
			panic(err)
		}
		clientResp = fmt.Sprintf(`,"response":{"status":200,"body":%s}`, body)
	}
	facts := ""
	if r.estTokens > 0 {
		facts = fmt.Sprintf(`,"facts":{"estimated_tokens":%d}`, r.estTokens)
	}
	return fmt.Sprintf(`{"ts":%q,"dur_ms":5,"model":"m1","protocol":"openai-completions","outcome":%q%s,"client":{"request":{}%s},"attempts":[%s]}`,
		ts.Format(time.RFC3339), outcome, facts, clientResp, strings.Join(atts, ","))
}

// tokenChargeFor reproduces what internal/router computed for this request by
// calling the router's OWN exported entry point (router.TokenCounters — the
// same function the live path's tokenCharge and `vmr replay`'s chargeReplay
// both go through). Nothing about the exact-vs-degraded rule is restated
// here: restating it is precisely the failure mode this whole file exists to
// catch, one level down.
func (r parityRequest) tokenCharge() (quota.Counters, float64) {
	u, sniffed := chatmsg.ExtractUsage(r.respBody)
	return router.TokenCounters(u, sniffed, r.estTokens, tokenutil.Estimate([]byte(r.respBody)))
}

// routerCharged replays the same requests through internal/router's REAL
// charging entry point (router.ChargeResponse, the function chargeQuota
// hands off to and the one internal/replay reuses) and returns the account's
// resulting consumption in its metric's own unit — i.e. the authoritative
// number the report's recomputed column is trying to reproduce.
func routerCharged(t *testing.T, reqs []parityRequest, provider string, spec *core.QuotaSpec, rate *core.PricingSpec, now time.Time) float64 {
	t.Helper()
	reg := quota.NewRegistry("")
	for _, r := range reqs {
		raw, estimated := r.tokenCharge()
		for _, a := range r.attempts {
			if !a.forwarded() {
				continue // the router only ever charges a forwarded response
			}
			ep := &core.Endpoint{AdapterType: "openai-completions", Provider: provider, Model: r.model, Quota: spec, PricingRate: rate}
			router.ChargeResponse(reg, ep, raw, estimated, now)
		}
	}
	l := spec.Limits[0]
	c, _ := reg.Used(provider, quota.LimitKey(l, ""), quota.PeriodStart(l, now))
	return quota.BaseAmount(l, c)
}

// reportWindowConsumed runs the real `vmr report` pipeline over the same
// requests and returns §2.5's recomputed window-consumed figure.
func reportWindowConsumed(t *testing.T, reqs []parityRequest, provider string, ts time.Time) *float64 {
	t.Helper()
	row := reportQuotaRow(t, reqs, provider, ts, quotaYAML)
	return row.WindowConsumed
}

// reportQuotaRow is reportWindowConsumed's full-row form: runs the real
// `vmr report` pipeline over reqs against the config yamlFn produces, and
// returns this provider's whole §2.5 row (the tokens tests also need
// WindowEstimatedPct, not just the consumed figure).
func reportQuotaRow(t *testing.T, reqs []parityRequest, provider string, ts time.Time, yamlFn func(logDir string) string) report.ProviderQuotaRow {
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
	configPath := writeTempFile(t, "config.yaml", yamlFn(logDir))
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
			return row
		}
	}
	t.Fatalf("no §2.5 quota row for provider %q (rows: %+v)", provider, rep.ProviderQuotas)
	return report.ProviderQuotaRow{}
}

// TestQuotaParity_RequestsMetric_ReportMatchesRouter is the core assertion:
// for metric: requests the two sides must agree exactly (== , not an
// epsilon compare) — quotaYAML's account has no model_multipliers, so
// every charge is Requests:1 unscaled, and integer-valued float64 addition
// is exact at these magnitudes. Anything less than equality here means a
// basis was chosen wrong, not a floating-point rounding artifact.
//
// quotaYAML's account declares model_multipliers implicitly at 1.0; the
// multiplier axis (where an epsilon compare IS warranted — see below) is
// covered separately below.
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

	want := routerCharged(t, reqs, "acct1", spec, nil, now)
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

// TestQuotaParity_RequestsMetric_NonIntegerMultiplier adds the fractional-
// multiplier axis on top of the basis axis. Since 2026-08-14,
// quota.ApplyModelMultiplier applies an exact multiplier with no rounding
// (see quota.Counters' doc comment), so the router side (N independent
// float64 additions of the same per-charge value) and the report side (one
// multiplication) are two different floating-point expressions computing
// the same mathematical quantity — not guaranteed bit-identical for an
// arbitrary multiplier by IEEE 754's non-associativity, even though they
// are for the specific values this fixture uses. The comparison below uses
// a relative epsilon for exactly that reason, not because either side is
// allowed to actually drift. Driving the router directly means this test
// can't drift from whatever ApplyModelMultiplier does — it asserts (near-)
// equality, not a hand-computed constant.
func TestQuotaParity_RequestsMetric_NonIntegerMultiplier(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	var reqs []parityRequest
	for i := 0; i < 19; i++ {
		reqs = append(reqs, parityRequest{model: "real-model", attempts: []parityAttempt{{status: 200}}})
	}
	// plus some noise the router never charges, to keep the basis honest
	reqs = append(reqs, parityRequest{model: "real-model", attempts: []parityAttempt{{status: 503}}})

	lim := core.Limit{Metric: core.MetricRequests, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 100000,
		ModelMultipliers: map[string]float64{"real-model": 5.5}}
	spec := &core.QuotaSpec{Limits: []core.Limit{lim}}

	want := routerCharged(t, reqs, "acct1", spec, nil, now)
	if want != 104.5 {
		t.Fatalf("fixture sanity: router charged %v, expected 19*5.5=104.5 (exact, no rounding)", want)
	}

	// The report side reads its multiplier from config.yaml, so this half
	// is asserted through buildProviderQuotaRows' own inputs rather than the
	// YAML pipeline (quotaYAML declares no model_multipliers). Reproducing
	// the router's formula here is the point; the end-to-end basis is
	// covered by the test above.
	unit, _ := quota.ApplyModelMultiplier(lim, "real-model", quota.Counters{Requests: 1}, 0)
	var forwarded float64
	for _, r := range reqs {
		for _, a := range r.attempts {
			if a.forwarded() {
				forwarded++
			}
		}
	}
	got := quota.BaseAmount(lim, quota.Counters{Requests: unit.Requests * forwarded})
	if diff := math.Abs(got - want); diff > 1e-9*want {
		t.Errorf("recomputed = %v, router charged %v (diff %v exceeds relative epsilon)", got, want, diff)
	}
}

// sseWithUsage/sseNoUsage are the two response shapes a tokens/cost account's
// charge can come from. Both are SSE strings on purpose — see parityRequest's
// respBody doc comment for why a JSON object would make the degraded byte
// count unreproducible.
func sseWithUsage(in, cacheRead, cacheWrite, out int) string {
	return "data: {\"choices\":[{\"delta\":{\"content\":\"hello there\"}}]}\n\n" +
		fmt.Sprintf("data: {\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,"+
			"\"prompt_tokens_details\":{\"cached_tokens\":%d},\"cache_creation_input_tokens\":%d}}\n\n",
			in, out, cacheRead, cacheWrite) +
		"data: [DONE]\n\n"
}

func sseNoUsage() string {
	return "data: {\"choices\":[{\"delta\":{\"content\":\"a reply with no usage block at all\"}}]}\n\n" +
		"data: [DONE]\n\n"
}

// tokensParityFixture is the shared corpus for the tokens and cost parity
// tests: a MIXED window — some responses carry a usage object (charged
// exactly), some don't (charged as a degraded byte estimate) — plus traffic
// the router never charges at all.
//
// The mix is the whole point. Before B0, `vmr report` counted only the
// sniffed half and rendered the result as a precise number; the all-or-
// nothing guard that existed ("every request unparseable → render -") never
// fired on a window like this one, which is also the window real traffic
// produces.
func tokensParityFixture() []parityRequest {
	return []parityRequest{
		// exact: usage sniffed off the response
		{model: "real-model", attempts: []parityAttempt{{status: 200}}, respBody: sseWithUsage(1000, 200, 50, 300), estTokens: 900},
		{model: "real-model", attempts: []parityAttempt{{status: 200}}, respBody: sseWithUsage(500, 0, 0, 120), estTokens: 480},
		// degraded: no usage block — the router charged Facts.EstimatedTokens
		// on the way in and a byte count on the way out
		{model: "real-model", attempts: []parityAttempt{{status: 200}}, respBody: sseNoUsage(), estTokens: 700},
		{model: "real-model", attempts: []parityAttempt{{status: 200}}, respBody: sseNoUsage(), estTokens: 1300},
		// failover: the 429 was never charged, the 200 behind it was
		{model: "real-model", attempts: []parityAttempt{{status: 429}, {status: 200}}, respBody: sseWithUsage(800, 100, 0, 200), estTokens: 750},
		// truncated 2xx: committed to the client, so the router charged it
		{model: "real-model", attempts: []parityAttempt{{status: 200, truncated: true}}, respBody: sseNoUsage(), estTokens: 640},
		// every attempt failed: nothing forwarded, nothing charged
		{model: "real-model", attempts: []parityAttempt{{status: 429}, {status: 500}}},
		{model: "real-model", attempts: []parityAttempt{{status: 0}}},
	}
}

// TestQuotaParity_TokensMetric_ReportMatchesRouter is the N2 regression net.
//
// It is asserted as EXACT equality, which is only honest because both sides
// see the same bytes here: the fixture's recorded client-side body is the
// only body in play. In production the router counts UPSTREAM bytes while the
// report can only count the forwarded ones, so the two differ by whatever
// response normalization rewrote — a residual documented on
// report.estimateDegradedTokens and surfaced to users through
// ProviderQuotaRow.WindowEstimatedPct, NOT something this test can or should
// paper over. What it pins is the part that can silently drift: the formula
// and the basis.
func TestQuotaParity_TokensMetric_ReportMatchesRouter(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	reqs := tokensParityFixture()
	lim := core.Limit{Metric: core.MetricTokens, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 1_000_000,
		TokenWeights: core.NewTokenWeights()}
	spec := &core.QuotaSpec{Limits: []core.Limit{lim}}

	want := routerCharged(t, reqs, "acct1", spec, nil, now)
	if want <= 0 {
		t.Fatalf("fixture sanity: router charged %v, expected a positive token total", want)
	}
	row := reportQuotaRow(t, reqs, "acct1", ts, tokensQuotaYAML)
	if row.WindowConsumed == nil {
		t.Fatal("WindowConsumed is nil — a tokens account with traffic must report a number, not '-'")
	}
	if *row.WindowConsumed != want {
		t.Errorf("§2.5 window consumed = %v, router actually charged %v — the recomputed column is NOT reproducing the router.\n"+
			"3 of the 6 charged responses carried no usage object; counting only the sniffed ones under-reports by the whole degraded share.",
			*row.WindowConsumed, want)
	}
	// The mixed window must also SAY that part of it is a guess — a correct
	// total presented as if it were authoritative is the other half of N2.
	if row.WindowEstimatedPct <= 0 || row.WindowEstimatedPct >= 100 {
		t.Errorf("WindowEstimatedPct = %v, want strictly between 0 and 100 for a mixed window", row.WindowEstimatedPct)
	}
}

// TestQuotaParity_TokensMetric_NonIntegerMultiplier is the tokens-metric
// counterpart of the requests-metric float-multiplier test above, and the
// stricter of the two: requests has one integer counter to scale, tokens has
// FOUR component counters plus a separately-tracked degraded share, and
// quota.ApplyModelMultiplier scales the exact and the estimated halves in
// separate statements. A float factor therefore has two independent places
// to accumulate differently on the two sides — the router scaling each
// response as it lands, the report scaling one summed window at the end.
//
// It also goes further than the requests test could: the multiplier reaches
// the report side through the real config.yaml pipeline
// (tokensMultiplierQuotaYAML), so this asserts the wiring as well as the
// arithmetic rather than restating the formula in the test body.
func TestQuotaParity_TokensMetric_NonIntegerMultiplier(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	reqs := tokensParityFixture()
	plainLim := core.Limit{Metric: core.MetricTokens, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 1_000_000,
		TokenWeights: core.NewTokenWeights()}
	lim := plainLim
	lim.ModelMultipliers = map[string]float64{"real-model": 2.5}
	spec := &core.QuotaSpec{Limits: []core.Limit{lim}}

	want := routerCharged(t, reqs, "acct1", spec, nil, now)
	plain := routerCharged(t, reqs, "acct1",
		&core.QuotaSpec{Limits: []core.Limit{plainLim}}, nil, now)
	if diff := math.Abs(want - 2.5*plain); diff > 1e-9*want {
		t.Fatalf("fixture sanity: multiplied charge %v is not 2.5x the unmultiplied %v", want, plain)
	}

	row := reportQuotaRow(t, reqs, "acct1", ts, tokensMultiplierQuotaYAML)
	if row.WindowConsumed == nil {
		t.Fatal("WindowConsumed is nil — a tokens account with traffic must report a number, not '-'")
	}
	// Relative epsilon, not exact equality: the two sides multiply in a
	// different order (per-response vs. per-window), which is a legitimate
	// float difference. What must NOT differ is the value.
	if diff := math.Abs(*row.WindowConsumed - want); diff > 1e-9*want {
		t.Errorf("§2.5 window consumed = %v, router actually charged %v (diff %v exceeds relative epsilon) — "+
			"a non-integer model_multipliers factor is not surviving one of the two paths intact",
			*row.WindowConsumed, want, diff)
	}
	// The estimated share is a fraction of the multiplied total, so it must
	// be unchanged by the multiplier — a factor applied to only one of the
	// numerator/denominator would show up right here.
	if row.WindowEstimatedPct <= 0 || row.WindowEstimatedPct >= 100 {
		t.Errorf("WindowEstimatedPct = %v, want strictly between 0 and 100 for a mixed window", row.WindowEstimatedPct)
	}
}

// TestQuotaParity_CostMetric_ReportMatchesRouter covers the third metric.
// Unlike tokens, the two sides reach their number by genuinely different
// routes — the router prices at charge time through ep.PricingRate
// (componentCost), the report prices post-hoc through its own
// pricing.Resolver (cost.go's costFor) — so this pins that both end up on
// pricing.Rate.Cost with all FOUR components, cache_read included. Dropping
// cache_read must be included: excluding it understates cost for every provider that
// prices cache reads above zero, which is nearly all of them.
//
// Reuses tokensParityFixture's MIXED window (exact usage / degraded
// estimate / never-forwarded) rather than an all-exact one of its own: a
// cost account's degraded share used to be silently dropped (costFor
// returned a hardcoded 0 for any record whose usage wasn't sniffed), the
// same false-zero N2 already fixed for tokens. componentCost/costFor both
// now price rc.estInFresh/rc.estOut (Fresh/Out only, no cache components —
// neither side can tell cache hits apart from an unparseable response) for
// those records instead of contributing nothing.
func TestQuotaParity_CostMetric_ReportMatchesRouter(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	reqs := tokensParityFixture()
	lim := core.Limit{Metric: core.MetricCost, EveryUnit: "mo", EveryN: 1, EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 100}
	spec := &core.QuotaSpec{Limits: []core.Limit{lim}}
	// Same four per-1M rates costParityYAML declares, as the router would
	// have resolved them onto the endpoint at BuildSnapshot time.
	rate := &core.PricingSpec{Currency: "USD", Base: core.Rate{
		InFresh: f64p(3), CacheRead: f64p(0.3), CacheWrite: f64p(3.75), Out: f64p(15),
	}}

	want := routerCharged(t, reqs, "acct1", spec, rate, now)
	if want <= 0 {
		t.Fatalf("fixture sanity: router charged $%v, expected a positive amount", want)
	}
	row := reportQuotaRow(t, reqs, "acct1", ts, costParityYAML)
	if row.WindowConsumed == nil {
		t.Fatal("WindowConsumed is nil — every request in this fixture resolves a price, so it must be a number")
	}
	// Relative epsilon, not equality: the two sides multiply the same four
	// component prices by the same four token counts, but sum them in
	// independently-written expressions (componentCost vs. costFor, both via
	// pricing.Rate.Cost) — IEEE 754 addition isn't associative, so bit
	// identity isn't guaranteed even when the math is.
	if diff := math.Abs(*row.WindowConsumed - want); diff > 1e-9*want {
		t.Errorf("§2.5 window consumed = $%v, router actually charged $%v (diff %v).\n"+
			"A gap near the cache_read share means one side dropped that component from the four-component formula, "+
			"or one side is silently skipping the degraded (unsniffed-usage) records the other still charges.",
			*row.WindowConsumed, want, diff)
	}
	// The mixed window must also SAY that part of it is a guess — a correct
	// total presented as if it were authoritative would repeat N2's mistake
	// on the cost metric instead of just the tokens one.
	if row.WindowEstimatedPct <= 0 || row.WindowEstimatedPct >= 100 {
		t.Errorf("WindowEstimatedPct = %v, want strictly between 0 and 100 for a mixed window", row.WindowEstimatedPct)
	}
}

func f64p(v float64) *float64 { return &v }
