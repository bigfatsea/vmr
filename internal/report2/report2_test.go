// Ver 2026-07-25, report2

package report2

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil)
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil)
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil)
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
  - endpoint: "openai:volcengine:doubao-seed-2.0-lite"
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
	// Build with pricing
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, p)
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
		byEndpoint: map[string]PricingRate{
			"openai:volcengine:doubao-seed-2.0-lite": {
				Endpoint:     "openai:volcengine:doubao-seed-2.0-lite",
				InFreshPer1M: 0.28, CacheReadPer1M: 0.028, CacheWritePer1M: 0, OutPer1M: 1.10,
			},
		},
	}
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, pricing)
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil)
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
// end to end — no prior test exercised WriteRequestsIndex/renderIndex at
// all, only the aggregate Report2 (via smallAuditRecords, whose records all
// share identical message content and so fold into a single session,
// useless for testing grouping). Two distinct-content records under
// client "alice" become two separate one-turn sessions (so "alice"'s Chat
// User header can be checked for the "N 会话 N 任务 N 轮" count); one
// heartbeat-tagged record under "bob" is a single-shot scheduled session
// and must collapse into the top-level 定时任务 rollup instead of getting
// its own Chat User section.
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
	rep, sess, err := Build([]string{path}, time.Now(), nil, nil)
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
	if !strings.Contains(s, "# Chat User: alice · 2 会话 2 任务 2 轮") {
		t.Errorf("missing alice Chat User header with counts:\n%s", s)
	}
	if !strings.Contains(s, "# 定时任务 · heartbeat 单发会话 × 1") {
		t.Errorf("missing collapsed heartbeat rollup section:\n%s", s)
	}
	if strings.Contains(s, "# Chat User: bob") {
		t.Errorf("bob's only record is a single-shot heartbeat and must not get its own Chat User section:\n%s", s)
	}
	// 00:00 UTC on the first record must render as 08:00 local (UTC+8),
	// not the source record's own (UTC) offset.
	if !strings.Contains(s, "2026-07-24 08:00:00") {
		t.Errorf("timestamps should be converted to UTC+8:\n%s", s)
	}

	for _, name := range []string{"vmr-requests-alice.md", "vmr-requests-bob.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing per-tag sibling %s: %v", name, err)
		}
	}
	for _, legacy := range []string{"vmr-requests-index.md", "vmr-requests-index-alice.md"} {
		if _, err := os.Stat(filepath.Join(dir, legacy)); !os.IsNotExist(err) {
			t.Errorf("legacy-named file %s should not be written", legacy)
		}
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
		_, _, _ = Build([]string{path}, time.Now(), nil, nil)
	}
}
