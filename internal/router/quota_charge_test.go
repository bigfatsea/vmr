// Ver 2026-08-07, by Opus 5

package router

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/respnorm"
)

func tokensLimit(amount float64) core.Limit {
	return core.Limit{
		Metric: core.MetricTokens, EveryN: 1, EveryUnit: "mo", EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: amount,
	}
}

func requestsLimit(amount float64) core.Limit {
	return core.Limit{
		Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo", EveryText: "1mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: amount,
	}
}

var chargeNow = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

// --- nil-safety (dev plan §5.4) ---

func TestChargeQuota_NilSafe_NoRegistry(t *testing.T) {
	rt := &Router{} // Quota left nil, exactly like router.New's real construction
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{requestsLimit(100)}}}
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	rt.chargeQuota(ep, rbody, &core.CanonicalRequest{}, chargeNow) // must not panic
}

func TestChargeQuota_NilSafe_NoEndpointQuota(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	ep := &core.Endpoint{Provider: "p1"} // Quota nil: no quota: configured
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	rt.chargeQuota(ep, rbody, &core.CanonicalRequest{}, chargeNow)
	used, _ := rt.Quota.Used("p1", "requests/1mo", chargeNow)
	if used.Requests != 0 {
		t.Fatalf("charged an endpoint with no quota: %+v", used)
	}
}

func TestChargeQuota_NilSafe_EmptyLimits(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{}} // Limits: nil
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	rt.chargeQuota(ep, rbody, &core.CanonicalRequest{}, chargeNow) // must not panic
}

// --- metric: requests ---

func TestChargeQuota_Requests_OnePerCall(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := requestsLimit(100)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(ep, rbody, creq, chargeNow)
	rt.chargeQuota(ep, rbody, creq, chargeNow)
	rt.chargeQuota(ep, rbody, creq, chargeNow)

	used, est := rt.Quota.Used("p1", "requests/1mo", quota.PeriodStart(l, chargeNow))
	if used.Requests != 3 {
		t.Fatalf("Requests = %v, want 3", used.Requests)
	}
	if est != 0 {
		t.Fatalf("estimated = %v, want 0 (requests metric is always exact)", est)
	}
}

// --- metric: tokens, sniffed usage (buffered / non-SSE JSON body) ---

func TestChargeQuota_Tokens_SniffedUsage_Buffered(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	creq := &core.CanonicalRequest{}

	body := `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":20}}}`
	rs := respnorm.Wrap(strings.NewReader(body), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if u, ok := rs.Usage(); !ok || u.In != 100 || u.Out != 50 || u.CacheRead != 20 {
		t.Fatalf("sniffed usage = %+v ok=%v, want In=100 Out=50 CacheRead=20", u, ok)
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)

	used, est := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))
	// OpenAI semantics: prompt_tokens (100) already includes the 20 cached
	// tokens, so fresh = 100 - 20 - 0 = 80.
	if used.Fresh != 80 || used.CacheRead != 20 || used.CacheWrite != 0 || used.Out != 50 {
		t.Fatalf("charged counters = %+v, want Fresh=80 CacheRead=20 Out=50", used)
	}
	if est != 0 {
		t.Fatalf("estimated = %v, want 0 (usage was sniffed, not estimated)", est)
	}
}

// --- metric: tokens, sniffed usage over SSE (streaming, exercises emitBlock) ---

func TestChargeQuota_Tokens_SniffedUsage_SSE(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	creq := &core.CanonicalRequest{}

	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n" +
		"data: [DONE]\n\n"
	rs := respnorm.Wrap(strings.NewReader(sse), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: true, Protocol: "openai", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if u, ok := rs.Usage(); !ok || u.In != 10 || u.Out != 5 {
		t.Fatalf("sniffed SSE usage = %+v ok=%v, want In=10 Out=5", u, ok)
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)
	used, est := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))
	if used.Fresh != 10 || used.Out != 5 {
		t.Fatalf("charged counters = %+v, want Fresh=10 Out=5", used)
	}
	if est != 0 {
		t.Fatalf("estimated = %v, want 0", est)
	}
}

// --- metric: tokens, sniffed usage over Anthropic's two-stage SSE form
// (message_start carries input+cache tokens nested under "message", a later
// message_delta carries cumulative output tokens at the top level) ---

func TestChargeQuota_Tokens_SniffedUsage_AnthropicTwoStage(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	creq := &core.CanonicalRequest{}

	sse := `data: {"type":"message_start","message":{"id":"msg1","usage":{"input_tokens":100,"cache_read_input_tokens":20,"output_tokens":1}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":25}}

data: {"type":"message_stop"}

`
	rs := respnorm.Wrap(strings.NewReader(sse), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: true, Protocol: "anthropic", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	u, ok := rs.Usage()
	if !ok {
		t.Fatal("Usage() ok=false, want true")
	}
	// Anthropic semantics: input_tokens EXCLUDES cache — In is their sum
	// (see chatmsg.usageFromObj's doc comment) — so In=100+20=120, and the
	// final output_tokens (25, from message_delta) wins over message_start's
	// initial 1 via mergeUsage's per-field max.
	if u.In != 120 || u.Out != 25 || u.CacheRead != 20 {
		t.Fatalf("sniffed two-stage usage = %+v, want In=120 Out=25 CacheRead=20", u)
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)
	used, est := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))
	// fresh = In - CacheRead - CacheWrite = 120 - 20 - 0 = 100.
	if used.Fresh != 100 || used.CacheRead != 20 || used.Out != 25 {
		t.Fatalf("charged counters = %+v, want Fresh=100 CacheRead=20 Out=25", used)
	}
	if est != 0 {
		t.Fatalf("estimated = %v, want 0 (usage was sniffed)", est)
	}
}

// truncatingReader delivers data once, then a fixed non-EOF error on every
// later Read — simulating a real upstream connection that dies mid-response
// (io.ErrUnexpectedEOF, a stalled TLS session, …), as opposed to a clean
// io.EOF.
type truncatingReader struct {
	data []byte
	err  error
}

func (r *truncatingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// --- metric: tokens, genuinely truncated mid-stream: must still degrade to
// the byte-count estimate using whatever bytes actually arrived, never
// sniff a usage object that was never sent ---

func TestChargeQuota_Tokens_TruncatedMidStream_Degrades(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	creq := &core.CanonicalRequest{Facts: core.RequestFacts{EstimatedTokens: 15}}

	// The connection dies before the upstream ever got to send a usage
	// object — a real mid-stream drop, not a well-formed-but-usage-less body.
	partial := `{"choices":[{"delta":{"content":"partial response before the connection d`
	tr := &truncatingReader{data: []byte(partial), err: io.ErrUnexpectedEOF}
	rs := respnorm.Wrap(tr, respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})

	if _, copyErr := io.Copy(io.Discard, rs); copyErr == nil {
		t.Fatal("expected a non-nil error propagated from the truncated read")
	}
	if _, ok := rs.Usage(); ok {
		t.Fatal("a truncated stream must never report sniffed usage")
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)
	used, est := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))
	wantOutInt := core.EstimateTokensFromCounts(int64(len(partial)), 0) // pure ASCII partial body
	wantOut := float64(wantOutInt)
	if used.Fresh != 15 {
		t.Fatalf("Fresh = %v, want 15 (creq.Facts.EstimatedTokens)", used.Fresh)
	}
	if used.Out != wantOut {
		t.Fatalf("Out = %v, want %v (byte count of whatever partial bytes actually arrived)", used.Out, wantOut)
	}
	if est != 15+wantOut {
		t.Fatalf("estimated = %v, want %v", est, 15+wantOut)
	}
}

// --- metric: tokens, degraded estimate (no usage field) ---

func TestChargeQuota_Tokens_DegradedEstimate_NoUsageField(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	creq := &core.CanonicalRequest{Facts: core.RequestFacts{EstimatedTokens: 42}}

	body := `{"choices":[{"message":{"content":"hello, this response has no usage field at all"}}]}`
	rs := respnorm.Wrap(strings.NewReader(body), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if _, ok := rs.Usage(); ok {
		t.Fatal("Usage() ok=true, want false — body has no usage field")
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)
	used, est := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))

	wantOutInt := core.EstimateTokensFromCounts(int64(len(body)), 0) // pure ASCII body
	wantOut := float64(wantOutInt)
	if used.Fresh != 42 {
		t.Fatalf("Fresh = %v, want 42 (creq.Facts.EstimatedTokens)", used.Fresh)
	}
	if used.Out != wantOut {
		t.Fatalf("Out = %v, want %v (byte-count estimate of the response body)", used.Out, wantOut)
	}
	if est != 42+wantOut {
		t.Fatalf("estimated = %v, want %v (the whole degraded charge)", est, 42+wantOut)
	}
}

// --- metric: tokens, opaque (Content-Encoding) response: must degrade, never sniff ---

func TestChargeQuota_Tokens_OpaqueAlwaysDegrades(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	l := tokensLimit(1_000_000)
	ep := &core.Endpoint{Provider: "p1", Quota: &core.QuotaSpec{Limits: []core.Limit{l}}}
	creq := &core.CanonicalRequest{Facts: core.RequestFacts{EstimatedTokens: 7}}

	// Even a body that WOULD contain a parseable usage field must not be
	// sniffed when opaque=true — running the usage/model regexes over
	// possibly-compressed bytes is exactly what opaque mode exists to avoid
	// (see response.go's package doc comment).
	body := `{"usage":{"prompt_tokens":999,"completion_tokens":999}}`
	rs := respnorm.Wrap(strings.NewReader(body), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: true})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if _, ok := rs.Usage(); ok {
		t.Fatal("opaque response yielded sniffed usage, want none")
	}

	rt.chargeQuota(ep, rs, creq, chargeNow)
	used, est := rt.Quota.Used("p1", "tokens/1mo", quota.PeriodStart(l, chargeNow))
	if used.Fresh != 7 {
		t.Fatalf("Fresh = %v, want 7 (degraded, not the 999 in the opaque body)", used.Fresh)
	}
	if est == 0 {
		t.Fatal("estimated = 0, want > 0 (opaque path is always a degraded charge)")
	}
}

// --- independent providers/limits never cross-contaminate ---

func TestChargeQuota_IndependentProviders(t *testing.T) {
	rt := &Router{Quota: quota.NewRegistry("")}
	lr := requestsLimit(100)
	epA := &core.Endpoint{Provider: "plan-a", Quota: &core.QuotaSpec{Limits: []core.Limit{lr}}}
	epB := &core.Endpoint{Provider: "plan-b", Quota: &core.QuotaSpec{Limits: []core.Limit{lr}}}
	rbody := respnorm.Wrap(bytes.NewReader(nil), respnorm.Options{ClientModel: "m", UpstreamModel: "m", IsSSE: false, Protocol: "openai", Opaque: false})
	creq := &core.CanonicalRequest{}

	rt.chargeQuota(epA, rbody, creq, chargeNow)
	rt.chargeQuota(epA, rbody, creq, chargeNow)
	rt.chargeQuota(epB, rbody, creq, chargeNow)

	usedA, _ := rt.Quota.Used("plan-a", "requests/1mo", quota.PeriodStart(lr, chargeNow))
	usedB, _ := rt.Quota.Used("plan-b", "requests/1mo", quota.PeriodStart(lr, chargeNow))
	if usedA.Requests != 2 || usedB.Requests != 1 {
		t.Fatalf("plan-a=%v plan-b=%v, want 2 and 1", usedA.Requests, usedB.Requests)
	}
}

// --- end-to-end through Serve: only the endpoint that actually succeeds
// gets charged, failed failover attempts never do ---

func TestServe_EndToEnd_OnlySuccessfulAttemptCharges(t *testing.T) {
	u1 := newMockUpstream(t, 500, `{"error":"e1"}`)
	u2 := newMockUpstream(t, 503, `{"error":"e2"}`)
	u3 := newMockUpstream(t, 200, `{"id":"ok","model":"m3"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: %s}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 1000}]
  - name: p2
    base_url: {openai: %s}
    api_key: k2
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 1000}]
  - name: p3
    base_url: {openai: %s}
    api_key: k3
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 1000}]
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [m1]}
      - {protocol: openai, provider: p2, models: [m2]}
      - {protocol: openai, provider: p3, models: [m3]}
`, u1.srv.URL, u2.srv.URL, u3.srv.URL))

	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}

	// The real charge landed under whatever "now" chargeQuota's own
	// time.Now() call captured, so query with a period boundary computed
	// the same way — not a fixed test date (a "since 2026-01-01" monthly
	// window resolved against today's real date is nowhere near January).
	l := requestsLimit(1000)
	l.Since = time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	ps := quota.PeriodStart(l, time.Now())
	u1Used, _ := rt.Quota.Used("p1", "requests/1mo", ps)
	u2Used, _ := rt.Quota.Used("p2", "requests/1mo", ps)
	u3Used, _ := rt.Quota.Used("p3", "requests/1mo", ps)
	if u1Used.Requests != 0 || u2Used.Requests != 0 {
		t.Fatalf("failed attempts charged: p1=%v p2=%v, want 0/0", u1Used.Requests, u2Used.Requests)
	}
	if u3Used.Requests != 1 {
		t.Fatalf("successful endpoint p3 charged %v, want 1", u3Used.Requests)
	}
}
