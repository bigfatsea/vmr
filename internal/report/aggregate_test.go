// Ver 2026-07-25, by Sonnet 5

package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// smallAuditRecords returns a few synthetic records covering ok/error,
// cache hit/miss, stream dur/ttft, fallback, and tool usage.
func smallAuditRecords() []map[string]any {
	mkRecord := func(ts time.Time, model, protocol, outcome string, dur, ttft int64, in, out, cached int64) map[string]any {
		body := map[string]any{
			"usage": map[string]any{
				"prompt_tokens":     in,
				"completion_tokens": out,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": cached,
				},
			},
		}
		rec := map[string]any{
			"ts":             ts.Format(time.RFC3339Nano),
			"dur_ms":         dur,
			"ttft_ms":        ttft,
			"model":          model,
			"protocol":       protocol,
			"outcome":        outcome,
			"stream":         true,
			"client_key_tag": "pimini",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{
					"model": model, "messages": []any{map[string]any{"role": "user", "content": "hi"}},
				}},
				"response": map[string]any{"status": 200, "body": body},
			},
			"attempts": []map[string]any{
				{"endpoint": "openai:volcengine:doubao-seed-2.0-lite", "dur_ms": dur, "error": "", "response": map[string]any{"status": 200}},
			},
		}
		if outcome == "error" {
			rec["client"].(map[string]any)["response"] = map[string]any{"status": 500, "body": map[string]any{"error": "boom"}}
			rec["attempts"] = []map[string]any{
				{"endpoint": "openai:volcengine:doubao-seed-2.0-lite", "dur_ms": dur, "error": "transient: boom", "error_class": "transient"},
			}
		}
		return rec
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	records := []map[string]any{
		mkRecord(t0, "coding", "openai", "ok", 3000, 1000, 1000, 100, 800),
		mkRecord(t0.Add(time.Minute), "coding", "openai", "ok", 3500, 1200, 1200, 150, 900),
		mkRecord(t0.Add(2*time.Minute), "coding", "openai", "ok", 3200, 1100, 1100, 120, 950),
		mkRecord(t0.Add(3*time.Minute), "agent", "openai", "ok", 4000, 1500, 500, 50, 0),
		mkRecord(t0.Add(4*time.Minute), "agent", "openai", "error", 100, 80, 100, 10, 0),
	}
	return records
}

func writeTempJSONL(t *testing.T, dir string, records []map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, rec := range records {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(b)
		f.Write([]byte{'\n'})
	}
	return path
}

func TestFreshAndCacheEfficiency(t *testing.T) {
	// fresh = in - cached - cachewrite; cacheEff = cached / (cached + fresh)
	if freshTokens(1000, 800, 0) != 200 {
		t.Fatalf("fresh tokens mismatch")
	}
	if got := cacheEff(800, 200); got != 0.8 {
		t.Fatalf("cache_eff want 0.80, got %.4f", got)
	}
	if got := cacheEff(800, 0); got != 1.0 {
		t.Fatalf("cache_eff when fresh=0 should be 1.0, got %.4f", got)
	}
	if got := cacheEff(0, 0); got != 0 {
		t.Fatalf("cache_eff when both 0 should be 0, got %.4f", got)
	}
}

func TestPercentilesAndStream(t *testing.T) {
	p50, p95 := percentiles([]int64{10, 20, 30, 40, 50})
	if p50 != 30 || p95 != 50 {
		t.Fatalf("percentiles want p50=30 p95=50, got p50=%d p95=%d", p50, p95)
	}
	// stream_ms true percentile != two percentiles subtracted (the F1 invariant)
	durs := []int64{100, 200, 300, 400, 500}
	ttfts := []int64{10, 20, 30, 40, 50}
	var stream []int64
	for i := range durs {
		stream = append(stream, durs[i]-ttfts[i])
	}
	_, dur95 := percentiles(durs)
	_, ttft95 := percentiles(ttfts)
	_, stream95 := percentiles(stream)
	if stream95 != dur95-ttft95 {
		// This should be true for this monotonic data; the point is it needn't be.
		t.Logf("note: P95(dur)-P95(ttft)=%d, P95(stream)=%d", dur95-ttft95, stream95)
	}
}

func TestBuild(t *testing.T) {
	dir := t.TempDir()
	records := smallAuditRecords()
	path := writeTempJSONL(t, dir, records)
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Meta.Records != len(records) {
		t.Fatalf("records want %d got %d", len(records), rep.Meta.Records)
	}
	// overall raw totals
	if rep.Overall.Requests != len(records) {
		t.Fatalf("overall.requests want %d got %d", len(records), rep.Overall.Requests)
	}
	if rep.Overall.OK != 4 || rep.Overall.Errors != 1 {
		t.Fatalf("outcome counts want ok=4/err=1, got ok=%d err=%d", rep.Overall.OK, rep.Overall.Errors)
	}
	// fresh = in - cached
	if rep.Overall.TokensInFresh != rep.Overall.TokensIn-rep.Overall.TokensInCached {
		t.Fatalf("fresh invariant broken: fresh=%d in=%d cached=%d", rep.Overall.TokensInFresh, rep.Overall.TokensIn, rep.Overall.TokensInCached)
	}
	// cache efficiency in [0,1]
	if rep.Overall.CacheEfficiency < 0 || rep.Overall.CacheEfficiency > 1 {
		t.Fatalf("cache_eff out of range: %.4f", rep.Overall.CacheEfficiency)
	}
	// by_model sums == overall
	var byModelReq, byModelIn int
	for _, m := range rep.ByModel {
		byModelReq += m.Requests
		byModelIn += int(m.TokensIn)
	}
	if byModelReq != rep.Overall.Requests {
		t.Fatalf("sum(by_model.requests)=%d != overall.requests=%d", byModelReq, rep.Overall.Requests)
	}
	if byModelIn != int(rep.Overall.TokensIn) {
		t.Fatalf("sum(by_model.tokens_in)=%d != overall.tokens_in=%d", byModelIn, rep.Overall.TokensIn)
	}
	// by_client sums == overall
	var byClientReq int
	for _, c := range rep.ByClient {
		byClientReq += c.Requests
	}
	if byClientReq != rep.Overall.Requests {
		t.Fatalf("sum(by_client.requests)=%d != overall.requests=%d", byClientReq, rep.Overall.Requests)
	}
	// stream_ms p95 <= dur p95
	if rep.Overall.StreamMSP95 > rep.Overall.DurMSP95 {
		t.Fatalf("stream_ms_p95=%d > dur_p95=%d", rep.Overall.StreamMSP95, rep.Overall.DurMSP95)
	}
	// requests export rows
	if len(rep.RequestRows()) != len(records) {
		t.Fatalf("request rows want %d got %d", len(records), len(rep.RequestRows()))
	}
}

func TestMarkdownAndJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(rep)
	if !containsAll(md, []string{"# VMR 用量报告", "## §0 摘要", "## §1 成本与 Token 经济", "## §2 成本估算", "## §8 请求详单"}) {
		t.Fatalf("Markdown missing expected sections")
	}
	// JSON roundtrip preserves fields but not the unexported requests slice.
	jsonPath := filepath.Join(dir, "vmr-report.json")
	if err := WriteJSON(rep, jsonPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed Report2
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Meta.Format != Format {
		t.Fatalf("format want %d got %d", Format, parsed.Meta.Format)
	}
	if parsed.Meta.Records != 5 {
		t.Fatalf("json records want 5 got %d", parsed.Meta.Records)
	}
	// verify unexported requests is NOT in JSON
	if len(parsed.RequestRows()) != 0 {
		t.Fatalf("requests should not be in aggregate JSON")
	}
}

func TestToolWaste(t *testing.T) {
	dir := t.TempDir()
	records := smallAuditRecords()
	// add a tools field to the coding records
	for _, rec := range records {
		if rec["model"] == "coding" {
			reqBody := rec["client"].(map[string]any)["request"].(map[string]any)["body"].(map[string]any)
			reqBody["messages"] = []any{}
			reqBody["tools"] = []any{
				map[string]any{"name": "tool_a"},
				map[string]any{"name": "tool_b"},
				map[string]any{"name": "tool_c"},
			}
		}
	}
	path := writeTempJSONL(t, dir, records)
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range rep.Tools {
		if len(tool.Declared) == 0 {
			continue
		}
		util := float64(len(tool.Calls)) / float64(len(tool.Declared))
		if tool.DeclareUtilization != round2(util) {
			t.Fatalf("declare_utilization want %.2f got %.2f", util, tool.DeclareUtilization)
		}
		if tool.SchemaBytesShipped != tool.DeclaredBytes*int64(tool.Requests) {
			t.Fatalf("schema_bytes_shipped mismatch")
		}
		if tool.SchemaWasteBytes > tool.SchemaBytesShipped {
			t.Fatalf("waste bytes cannot exceed shipped bytes")
		}
	}
}

func TestPricing(t *testing.T) {
	dir := t.TempDir()
	pricingPath := filepath.Join(dir, "pricing.yaml")
	yaml := `currency: USD
updated_at: "2026-07-20"
rates:
  - provider: volcengine
    model: doubao-seed-2.0-lite
    in_fresh_per_1m: 0.28
    cache_read_per_1m: 0.028
    cache_write_per_1m: 0
    out_per_1m: 1.10
`
	if err := os.WriteFile(pricingPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPricing(pricingPath)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.Currency != "USD" {
		t.Fatalf("pricing load failed")
	}
	if len(p.Rates) != 1 {
		t.Fatalf("rates want 1 got %d", len(p.Rates))
	}
	if p.Rates[0].InFreshPer1M != 0.28 || p.Rates[0].OutPer1M != 1.10 {
		t.Fatalf("price values not parsed: %+v", p.Rates[0])
	}
	if p.Disclaimer() == "" {
		t.Fatalf("disclaimer should not be empty")
	}
	if len(p.Raw) == 0 {
		t.Fatalf("Raw should hold the file's exact bytes for §2's frozen snapshot")
	}
	// Build with pricing
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pricing == nil {
		t.Fatalf("report should carry pricing")
	}
}

// TestPricingAccumulatesToEndpointAndClient locks in that CostEstimate is
// populated on EndpointsAll/ByClient, not just Overall/ByModel — the four
// buckets the §2 成本估算 tables (by model/endpoint/client) each read from.
// smallAuditRecords' 5 records share one endpoint and one client_key_tag, so
// EndpointsAll[0] and ByClient[0] should both equal Overall's total; the
// error record (no successful attempt, so no serving endpoint) must
// contribute zero everywhere, not just get skipped from Overall.
func TestPricingAccumulatesToEndpointAndClient(t *testing.T) {
	dir := t.TempDir()
	pricing := &Pricing{
		Currency: "CNY",
		byKey: map[string][]PricingRate{
			rateKey("volcengine", "doubao-seed-2.0-lite"): {
				{Provider: "volcengine", Model: "doubao-seed-2.0-lite",
					InFreshPer1M: 0.28, CacheReadPer1M: 0.028, CacheWritePer1M: 0, OutPer1M: 1.10},
			},
		},
	}
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, pricing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Overall.CostEstimate == nil {
		t.Fatalf("Overall.CostEstimate should be populated")
	}
	want := *rep.Overall.CostEstimate
	if want <= 0 {
		t.Fatalf("Overall.CostEstimate should be > 0, got %v", want)
	}

	// by-model: coding (records 1-3) + agent (record 4 only; record 5 is the
	// error outcome with no successful attempt, contributing zero) should
	// sum back to Overall.
	var modelSum float64
	sawCoding, sawAgent := false, false
	for _, m := range rep.ByModel {
		if m.CostEstimate == nil {
			t.Fatalf("by_model %s missing cost_estimate", m.Model)
		}
		modelSum += *m.CostEstimate
		if m.Model == "coding" {
			sawCoding = true
		}
		if m.Model == "agent" {
			sawAgent = true
		}
	}
	if !sawCoding || !sawAgent {
		t.Fatalf("expected both coding and agent rows, got %+v", rep.ByModel)
	}
	if diff := modelSum - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("sum(by_model.cost_estimate)=%v != overall=%v", modelSum, want)
	}

	// by-endpoint: single shared endpoint, so EndpointsAll[0] == Overall.
	if len(rep.EndpointsAll) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(rep.EndpointsAll))
	}
	if rep.EndpointsAll[0].CostEstimate == nil {
		t.Fatalf("EndpointsAll[0].CostEstimate should be populated")
	}
	if got := *rep.EndpointsAll[0].CostEstimate; got != want {
		t.Errorf("endpoint cost_estimate=%v != overall=%v", got, want)
	}

	// by-client: single shared client_key_tag "pimini", so ByClient[0] == Overall.
	if len(rep.ByClient) != 1 || rep.ByClient[0].ClientKey != "pimini" {
		t.Fatalf("expected 1 client 'pimini', got %+v", rep.ByClient)
	}
	if rep.ByClient[0].CostEstimate == nil {
		t.Fatalf("ByClient[0].CostEstimate should be populated")
	}
	if got := *rep.ByClient[0].CostEstimate; got != want {
		t.Errorf("client cost_estimate=%v != overall=%v", got, want)
	}
}

func TestWriteRequestsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "vmr-requests.jsonl")
	n, err := WriteRequestsJSONL(rep.RequestRows(), jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(rep.RequestRows()) {
		t.Fatalf("jsonl rows want %d got %d", len(rep.RequestRows()), n)
	}
	b, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, by := range b {
		if by == '\n' {
			lines++
		}
	}
	if lines != n {
		t.Fatalf("jsonl file lines want %d got %d", n, lines)
	}
}

// TestWriteRequestsIndexGrouping covers vmr-requests.md's Chat User grouping
// end to end — no prior test exercised WriteRequestsIndex at all, only the
// aggregate Report2 (via smallAuditRecords, whose records all share
// identical message content and so fold into a single session, useless for
// testing grouping). Two distinct-content records under client "alice"
// become two separate one-turn sessions (so "alice"'s Chat User header can
// be checked for the "N 会话 N 任务 N 轮" count); one heartbeat-tagged
// record under "bob" is a single-shot scheduled session and must collapse
// into the top-level 定时任务 rollup (its own vmr-requests-cron-hartbeat.md,
// linked from the main index) instead of getting its own Chat User section
// or per-tag sibling — "bob" never had any interactive traffic, so no
// vmr-requests-bob.md is written at all.
func TestWriteRequestsIndexGrouping(t *testing.T) {
	dir := t.TempDir()
	at := func(h, m int) string {
		return time.Date(2026, 7, 24, h, m, 0, 0, time.UTC).Format(time.RFC3339)
	}
	mk := func(ts, clientKey, userMsg string) map[string]any {
		return map[string]any{
			"ts": ts, "dur_ms": 100, "model": "agent", "protocol": "openai",
			"outcome": "ok", "client_key_tag": clientKey,
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": []any{
					map[string]any{"role": "user", "content": userMsg},
				}}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai:volcengine:doubao-seed-2.0-lite",
				"dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	records := []map[string]any{
		mk(at(0, 0), "alice", "alice session one hello"),
		mk(at(1, 0), "alice", "alice session two, a different topic entirely"),
		mk(at(2, 0), "bob", "heartbeat check [OpenClaw heartbeat poll]"),
	}
	path := writeTempJSONL(t, dir, records)
	rep, sess, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequestsIndex(rep, sess, dir); err != nil {
		t.Fatal(err)
	}

	main, err := os.ReadFile(filepath.Join(dir, "vmr-requests.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(main)
	// The main index only carries a "## " group header + summary + link per
	// group now — the full "# Chat User: …" detail card moved to the
	// per-group sibling file.
	if !strings.Contains(s, "## Chat User: alice · 2 会话 2 任务 2 轮") {
		t.Errorf("missing alice Chat User index entry with counts:\n%s", s)
	}
	if !strings.Contains(s, "[vmr-requests-alice.md](vmr-requests-alice.md)") {
		t.Errorf("missing link to alice's detail sibling:\n%s", s)
	}
	if !strings.Contains(s, "## 定时任务 · heartbeat 单发会话 × 1") {
		t.Errorf("missing collapsed heartbeat rollup index entry:\n%s", s)
	}
	if !strings.Contains(s, "[vmr-requests-cron-hartbeat.md](vmr-requests-cron-hartbeat.md)") {
		t.Errorf("missing link to heartbeat's cron detail sibling:\n%s", s)
	}
	if strings.Contains(s, "Chat User: bob") {
		t.Errorf("bob's only record is a single-shot heartbeat and must not get its own Chat User section:\n%s", s)
	}
	// 00:00 UTC on the first record must render as 08:00 local (UTC+8),
	// not the source record's own (UTC) offset — footer table is still in
	// the main index.
	if !strings.Contains(s, "2026-07-24 08:00:00") {
		t.Errorf("timestamps should be converted to UTC+8:\n%s", s)
	}

	alice, err := os.ReadFile(filepath.Join(dir, "vmr-requests-alice.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(alice), "# Chat User: alice · 2 会话 2 任务 2 轮") {
		t.Errorf("alice's sibling should carry the full Chat User detail card:\n%s", alice)
	}

	if _, err := os.Stat(filepath.Join(dir, "vmr-requests-bob.md")); !os.IsNotExist(err) {
		t.Errorf("bob has no interactive traffic and should get no per-tag sibling")
	}
	if _, err := os.Stat(filepath.Join(dir, "vmr-requests-cron-hartbeat.md")); err != nil {
		t.Errorf("missing scheduled-class sibling vmr-requests-cron-hartbeat.md: %v", err)
	}
	for _, legacy := range []string{"vmr-requests-index.md", "vmr-requests-index-alice.md"} {
		if _, err := os.Stat(filepath.Join(dir, legacy)); !os.IsNotExist(err) {
			t.Errorf("legacy-named file %s should not be written", legacy)
		}
	}
}

// failureSurfaceRecords returns one record of each outcome shape relevant to
// FailedRequestRows: a clean "ok", an "error", a "canceled", and an
// ok-but-truncated (outcome "ok" with a "truncated"-classed attempt, the
// shape session.go's collect() reads to set ReqInfo.Truncated). Three of the
// four are the failure surface; the plain "ok" one is the control that must
// never appear in it.
func failureSurfaceRecords() []map[string]any {
	base := func(ts time.Time, outcome string, status int) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339Nano), "dur_ms": 100, "model": "agent",
			"protocol": "openai", "outcome": outcome,
			"client": map[string]any{
				"request":  map[string]any{"body": map[string]any{"model": "agent", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
				"response": map[string]any{"status": status, "body": map[string]any{}},
			},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	ok := base(t0, "ok", 200)
	ok["attempts"] = []map[string]any{{"endpoint": "openai:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "response": map[string]any{"status": 200}}}

	failed := base(t0.Add(time.Minute), "error", 500)
	failed["attempts"] = []map[string]any{{"endpoint": "openai:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "error": "transient: boom", "error_class": "transient"}}

	canceled := base(t0.Add(2*time.Minute), "canceled", 0)
	canceled["attempts"] = []map[string]any{{"endpoint": "openai:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "error": "canceled by client", "error_class": "canceled"}}

	truncated := base(t0.Add(3*time.Minute), "ok", 200)
	// Real shape: SetSuccessResponse commits the 2xx response first, then
	// SetTruncated (mid-stream death) only sets error/error_class — Response
	// stays as the already-committed 2xx.
	truncated["attempts"] = []map[string]any{{"endpoint": "openai:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "response": map[string]any{"status": 200}, "error": "truncated: EOF", "error_class": "truncated"}}

	return []map[string]any{ok, failed, canceled, truncated}
}

// TestFailedRequestRows checks the outcome filter: of failureSurfaceRecords'
// four records, exactly the error/canceled/ok-but-truncated three must
// survive — the plain "ok" one must not.
func TestFailedRequestRows(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, failureSurfaceRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := FailedRequestRows(rep.RequestRows())
	if len(failed) != 3 {
		t.Fatalf("want 3 failed rows (error, canceled, ok-but-truncated), got %d", len(failed))
	}
	got := map[string]bool{}
	for _, r := range failed {
		if r.Outcome == "ok" && !r.Truncated {
			t.Errorf("plain ok row leaked into FailedRequestRows: %+v", r)
		}
		got[r.Outcome+":"+strconv.FormatBool(r.Truncated)] = true
	}
	for _, want := range []string{"error:false", "canceled:false", "ok:true"} {
		if !got[want] {
			t.Errorf("missing expected failure shape %q among: %v", want, got)
		}
	}
}

// TestTruncatedRequestAttributesToServingEndpoint covers endpointInfo's
// fallback: a request whose only attempt got a 2xx response header before
// dying mid-stream (outcome "ok", attempt error_class "truncated") must
// still attribute its RequestRow.Endpoint and its endpoint-bucket
// request-level metrics to the endpoint that actually served those bytes —
// not leave them unattributed just because the attempt also carries an
// Error string.
func TestTruncatedRequestAttributesToServingEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, failureSurfaceRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	const wantEp = "openai:volcengine:doubao-seed-2.0-lite"
	rows := rep.RequestRows()
	var truncatedRow *RequestRow
	for i, r := range rows {
		if r.Outcome == "ok" && r.Truncated {
			truncatedRow = &rows[i]
		}
	}
	if truncatedRow == nil {
		t.Fatal("no truncated row found among request rows")
	}
	if truncatedRow.Endpoint != wantEp {
		t.Errorf("truncated request's RequestRow.Endpoint = %q, want %q", truncatedRow.Endpoint, wantEp)
	}

	var ep *EndpointRow
	for i, e := range rep.EndpointsAll {
		if e.Endpoint == wantEp {
			ep = &rep.EndpointsAll[i]
		}
	}
	if ep == nil {
		t.Fatalf("no EndpointsAll entry for %q", wantEp)
	}
	// ok + truncated both attribute to this endpoint at the request level;
	// error/canceled attempts in this fixture carry no committed response,
	// so they stay unattributed (unaffected by this fix).
	if ep.Requests != 2 {
		t.Errorf("EndpointsAll[%q].Requests = %d, want 2 (ok + truncated)", wantEp, ep.Requests)
	}
	// All 4 fixture records attempt this same endpoint once each, regardless
	// of request-level attribution.
	if ep.Attempts != 4 {
		t.Errorf("EndpointsAll[%q].Attempts = %d, want 4", wantEp, ep.Attempts)
	}
}

// TestWriteFailedIndex checks vmr-requests-failed.md/.jsonl: they must list
// exactly the 3 failed rows (not the 1 plain-ok row), link to detail files,
// and leave vmr-requests.md itself untouched (still carrying all 4 requests)
// — the failed index is additive, not a move.
func TestWriteFailedIndex(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, failureSurfaceRecords())
	rep, sess, err := Build([]string{path}, time.Now(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := rep.RequestRows()

	if err := WriteFailedIndex(rows, dir); err != nil {
		t.Fatal(err)
	}
	failedMD, err := os.ReadFile(filepath.Join(dir, "vmr-requests-failed.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(failedMD)
	if !strings.Contains(s, "共 3 条") {
		t.Errorf("want exactly 3 failed rows reported:\n%s", s)
	}

	n, err := WriteRequestsJSONL(FailedRequestRows(rows), filepath.Join(dir, "vmr-requests-failed.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3 rows written to vmr-requests-failed.jsonl, got %d", n)
	}

	// The full index is unaffected: still every request, the plain ok one included.
	if err := WriteRequestsIndex(rep, sess, dir); err != nil {
		t.Fatal(err)
	}
	fullMD, err := os.ReadFile(filepath.Join(dir, "vmr-requests.md"))
	if err != nil {
		t.Fatal(err)
	}
	full := string(fullMD)
	if strings.Count(full, "❌") != 1 {
		t.Errorf("vmr-requests.md should still carry the error row inline, unmoved:\n%s", full)
	}
	if !strings.Contains(full, "canceled") {
		t.Errorf("vmr-requests.md should still carry the canceled row inline, unmoved:\n%s", full)
	}
	if !strings.Contains(full, "⚠️trunc") {
		t.Errorf("vmr-requests.md should still carry the truncated ok row inline, unmoved:\n%s", full)
	}
}

func TestWriteFailedIndexEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFailedIndex(nil, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "vmr-requests-failed.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "共 0 条") {
		t.Errorf("want a 0-count header for no failed requests:\n%s", b)
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool { return jsonContains(s, sub) }

func jsonContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(haystack) > 0 && containsSub(haystack, needle))
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// tiedAuditRecords returns records deliberately constructed so several
// Build() buckets end up with genuine ties on their primary sort value:
// two client tags each with exactly 1 request (ByClient ties on Requests),
// two distinct endpoints each attempted exactly once (EndpointsAll ties on
// Attempts), and — as a side effect of two different first-user-messages —
// two single-request "interactive" sessions (Sessions ties on Requests).
// Without a deterministic tie-break, which endpoint/client/session of each
// tied pair sorts first depends on Go's (deliberately randomized) map
// iteration order — see TestBuildIsDeterministic below.
func tiedAuditRecords() []map[string]any {
	mk := func(ts, clientKey, endpoint, userMsg string) map[string]any {
		return map[string]any{
			"ts": ts, "dur_ms": 100, "model": "agent", "protocol": "openai",
			"outcome": "ok", "client_key_tag": clientKey,
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": []any{
					map[string]any{"role": "user", "content": userMsg},
				}}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": endpoint, "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	return []map[string]any{
		mk(t0.Format(time.RFC3339), "alice", "openai:provider-a:model-a", "alice's first message"),
		mk(t0.Add(time.Minute).Format(time.RFC3339), "bob", "openai:provider-b:model-b", "bob's first message"),
	}
}

// TestBuildIsDeterministic locks in the fix for a non-determinism bug:
// EndpointsAll/ByClient/Workloads/Sessions/Tools are appended from Go maps
// (whose iteration order the language spec deliberately leaves unspecified) and
// then sorted only by a count/byte-size value that can legitimately tie
// across distinct rows — without a secondary tie-break on the bucket's own
// identity field, two Build() calls against byte-identical input could
// (and, before the fix, empirically did — caught comparing
// loadtest-report.md across two runs of the same unmodified binary)
// disagree on which tied row comes first. Running Build() several times
// and requiring byte-identical JSON output is the direct test of that
// property — it doesn't matter whether any particular run's map iteration
// happened to "get lucky"; it must never be allowed to matter.
func TestBuildIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, tiedAuditRecords())

	const runs = 8
	var want []byte
	for i := 0; i < runs; i++ {
		rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil)
		if err != nil {
			t.Fatalf("run %d: Build: %v", i, err)
		}
		got, err := json.Marshal(rep)
		if err != nil {
			t.Fatalf("run %d: marshal: %v", i, err)
		}
		if i == 0 {
			want = got
			continue
		}
		if string(got) != string(want) {
			t.Fatalf("run %d produced different JSON than run 0 — Build() is not deterministic.\nrun 0: %s\nrun %d: %s", i, want, i, got)
		}
	}
}

// TestMarkdownTableCellsWithPercentRenderVerbatim locks in a bug 2.6's
// mdTable refactor introduced and the real-log verification caught: row()
// originally passed the joined cell string straight to w(format, ...) as
// the FORMAT string itself, so any cell containing a literal "%" (every
// percentage/cache-efficiency cell in this report) got reinterpreted by
// fmt.Fprintf as a verb and corrupted into "%!s(MISSING)" or similar. Fixed
// by always passing cells through a literal "%s" verb. Every row here has
// requests > 0, so pctStr2/cacheEffCell/pctHundred all produce "%" cells —
// if the bug reappears, "MISSING" shows up in the rendered document.
func TestMarkdownTableCellsWithPercentRenderVerbatim(t *testing.T) {
	rep := &Report2{
		Overall: Row{Requests: 10, OK: 9, TokensIn: 100, TokensInCached: 90, TokensKnown: 10, CacheEfficiency: 0.9, RequestsWithDur: 10, DurMSP95: 500},
		EndpointsAll: []EndpointRow{
			{Endpoint: "openai:p:m", Attempts: 10, OK: 9, Availability: 0.9, ErrorRate: 10,
				ErrorClasses: map[string]int{"transient": 1}},
		},
	}
	md := Markdown(rep)
	if strings.Contains(md, "MISSING") {
		t.Fatalf("rendered Markdown contains a corrupted Printf verb (percent sign in a table cell mishandled):\n%s", md)
	}
	if !strings.Contains(md, "90.0%") {
		t.Errorf("expected a literal cache-efficiency percentage in the output; got:\n%s", md)
	}
}

// TestTopErrorClassCountDeterministic locks in the fix for a second,
// previously-undiscovered instance of the same non-determinism class
// TestBuildIsDeterministic covers: topErrorClass/topErrorClassShort picked
// the error class with the highest count by ranging the map directly, so a
// TIE between two classes (both count 1, as here) resolved to whichever
// class Go's randomized map order happened to visit last — found while
// diffing 2.6's table-builder refactor against real production report
// output, where "主因" (top error class) flipped between two runs of the
// same unmodified binary. Fixed by iterating sortedKeysInt so a tie always
// resolves to the alphabetically-first class name.
func TestTopErrorClassCountDeterministic(t *testing.T) {
	classes := map[string]int{"transient": 1, "endpoint": 1, "auth": 1}
	for i := 0; i < 20; i++ {
		cls, n := topErrorClassCount(classes)
		if cls != "auth" || n != 1 {
			t.Fatalf("run %d: got (%q, %d), want (\"auth\", 1) — ties must always resolve alphabetically", i, cls, n)
		}
	}
}

func BenchmarkBuild(b *testing.B) {
	dir := b.TempDir()
	records := smallAuditRecords()
	path := filepath.Join(dir, "audit.jsonl")
	f, _ := os.Create(path)
	for _, rec := range records {
		by, _ := json.Marshal(rec)
		f.Write(by)
		f.Write([]byte{'\n'})
	}
	f.Close()
	for i := 0; i < b.N; i++ {
		_, _, _ = Build([]string{path}, time.Now(), nil, nil, nil)
	}
}
