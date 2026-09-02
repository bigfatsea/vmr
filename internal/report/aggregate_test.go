// Ver 2026-07-29 11:00, by Sonnet 5

package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/pricing"
	"vmr/internal/reqdetail"
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
				{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite", "dur_ms": dur, "error": "", "response": map[string]any{"status": 200}},
			},
		}
		if outcome == "error" {
			rec["client"].(map[string]any)["response"] = map[string]any{"status": 500, "body": map[string]any{"error": "boom"}}
			rec["attempts"] = []map[string]any{
				{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite", "dur_ms": dur, "error": "transient: boom", "error_class": "transient"},
			}
		}
		return rec
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	records := []map[string]any{
		mkRecord(t0, "coding", "openai-completions", "ok", 3000, 1000, 1000, 100, 800),
		mkRecord(t0.Add(time.Minute), "coding", "openai-completions", "ok", 3500, 1200, 1200, 150, 900),
		mkRecord(t0.Add(2*time.Minute), "coding", "openai-completions", "ok", 3200, 1100, 1100, 120, 950),
		mkRecord(t0.Add(3*time.Minute), "agent", "openai-completions", "ok", 4000, 1500, 500, 50, 0),
		mkRecord(t0.Add(4*time.Minute), "agent", "openai-completions", "error", 100, 80, 100, 10, 0),
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

// TestBuild_LegacyProtocolNamesNormalized is the analytics-side differential
// for the protocol rename: a pre-2026-08 audit line ("openai" / an
// "openai:acct:model" endpoint label) must aggregate under the current enum,
// same as a fresh line would. Covers the audit.Record.UnmarshalJSON path
// report relies on. TODO(2026-10): remove with core.CanonicalProtocol.
func TestBuild_LegacyProtocolNamesNormalized(t *testing.T) {
	dir := t.TempDir()
	rec := map[string]any{
		"ts": time.Now().Format(time.RFC3339Nano), "dur_ms": 100, "model": "vm",
		"protocol": "openai", "outcome": "ok", "stream": true,
		"client": map[string]any{
			"request":  map[string]any{"body": map[string]any{"model": "vm", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
			"response": map[string]any{"status": 200, "body": map[string]any{"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}}},
		},
		"attempts": []map[string]any{{"endpoint": "openai:acct:real-model", "protocol": "openai", "dur_ms": 100, "response": map[string]any{"status": 200}}},
	}
	path := writeTempJSONL(t, dir, []map[string]any{rec})
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range rep.EndpointsAll {
		if e.Endpoint == "openai-completions:acct:real-model" {
			found = true
		}
		if strings.HasPrefix(e.Endpoint, "openai:") {
			t.Errorf("endpoint row kept legacy protocol name: %q", e.Endpoint)
		}
	}
	if !found {
		t.Errorf("no endpoint row under normalized label openai-completions:acct:real-model; got %+v", rep.EndpointsAll)
	}
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
	// stream_ms true percentile != two percentiles subtracted (P95(dur)-P95(ttft) need not equal P95(stream))
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(rep, i18n.EN, nil, nil)
	if !containsAll(md, []string{"# VMR Usage Report", "## §0 Summary", "## §1 Cost & Token Economy", "## §2 Pay-As-You-Go Equivalent Cost", "## §8 Request Detail Index"}) {
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range rep.Tools {
		if len(tool.Declared) == 0 {
			continue
		}
		// distinct_called is declared-and-called (len(Declared) - NeverCalled),
		// never the raw distinct-invocation count — see buildTools.
		wantCalled := len(tool.Declared) - len(tool.NeverCalled)
		if tool.DistinctCalled != wantCalled {
			t.Fatalf("distinct_called want %d got %d", wantCalled, tool.DistinctCalled)
		}
		util := float64(wantCalled) / float64(len(tool.Declared))
		if tool.DeclareUtilization != round2(util) {
			t.Fatalf("declare_utilization want %.2f got %.2f", util, tool.DeclareUtilization)
		}
		if tool.SchemaBytesShipped != tool.DeclaredBytes*int64(tool.Requests) {
			t.Fatalf("schema_bytes_shipped mismatch")
		}
		if tool.SchemaWasteBytes > tool.SchemaBytesShipped || tool.SchemaWasteBytes < 0 {
			t.Fatalf("waste bytes out of range: %d (shipped %d)", tool.SchemaWasteBytes, tool.SchemaBytesShipped)
		}
	}
}

// TestPricing exercises pricing resolution: internal/pricing.Table/Resolver
// feeding Build's pricingSrc parameter, plus the report-local *Pricing
// summary (Build's pricingInfo parameter) that ends up on rep.Pricing for
// rendering (Currency/Disclaimer — see pricing.go's doc comment for why
// these are two separate values now instead of one report.Pricing type).
func TestPricing(t *testing.T) {
	dir := t.TempDir()
	table, err := pricing.ParseTable([]byte(`currency: USD
generated_at: "2026-07-20"
rates:
  - key: volcengine/doubao-seed-2.0-lite
    in_fresh: 0.28
    cache_read: 0.028
    cache_write: 0
    out: 1.10
`))
	if err != nil {
		t.Fatal(err)
	}
	rate, ok := table.Lookup("volcengine/doubao-seed-2.0-lite")
	if !ok || rate.InFresh == nil || *rate.InFresh != 0.28 || rate.Out == nil || *rate.Out != 1.10 {
		t.Fatalf("price values not parsed: ok=%v rate=%+v", ok, rate)
	}
	pricingInfo := &Pricing{Currency: "USD", StandardGeneratedAt: "2026-07-20"}
	if pricingInfo.Disclaimer(i18n.EN) == "" {
		t.Fatalf("disclaimer should not be empty")
	}

	resolver := pricing.NewResolver(table, nil, 1, "")
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, pricingInfo, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pricing == nil {
		t.Fatalf("report should carry pricing")
	}
	if rep.Overall.CostEstimate == nil || *rep.Overall.CostEstimate <= 0 {
		t.Fatalf("Overall.CostEstimate should be populated and positive, got %v", rep.Overall.CostEstimate)
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
	table, err := pricing.ParseTable([]byte(`currency: USD
rates:
  - key: volcengine/doubao-seed-2.0-lite
    in_fresh: 0.28
    cache_read: 0.028
    cache_write: 0
    out: 1.10
`))
	if err != nil {
		t.Fatal(err)
	}
	resolver := pricing.NewResolver(table, nil, 1, "")
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, &Pricing{Currency: "CNY"}, resolver, nil)
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

	// by-date: all 5 records land on the same day, so ByDate[0] == Overall —
	// pins the fix for a finding from the 2026-08-12 review
	// (VMR_项目全面Review报告 B3): the cost-accumulation block populated
	// Overall/ByModel/EndpointsAll/ByClient but never the ByDate bucket
	// (also a Row, also carrying CostEstimate), so §2 成本估算 had no
	// per-day cost breakdown at all.
	if len(rep.ByDate) != 1 {
		t.Fatalf("expected 1 date bucket, got %d", len(rep.ByDate))
	}
	if rep.ByDate[0].CostEstimate == nil {
		t.Fatalf("ByDate[0].CostEstimate should be populated")
	}
	if got := *rep.ByDate[0].CostEstimate; got != want {
		t.Errorf("date cost_estimate=%v != overall=%v", got, want)
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

// TestWriteRequestsJSONL covers WriteRequestsJSONL as a generic row writer.
// The only production caller today is vmr-requests-failed.jsonl (see
// cmd_report.go) — vmr-requests.json, the main per-request export, is
// WriteRequestsJSON (no "L"), a different function with its own
// "files" cache section. The filename below is deliberately generic
// (not vmr-requests-failed.jsonl) so this test doesn't imply the function
// is single-purpose.
func TestWriteRequestsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "vmr-requests-failed.jsonl")
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
// into the top-level 定时任务 rollup (its own vmr-requests-cron-heartbeat.md,
// linked from the main index) instead of getting its own Chat User section
// or per-tag sibling — "bob" never had any interactive traffic, so no
// vmr-requests-bob.md is written at all.
func TestWriteRequestsIndexGrouping(t *testing.T) {
	// Override DisplayZone to something other than UTC (this package's
	// TestMain default) and other than a plausible "real" timezone, so the
	// assertion below proves display genuinely follows fmtutil.DisplayZone
	// — not a hardcoded offset (the old behavior this test used to lock in)
	// and not a coincidence of the test fixture's own zone.
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.FixedZone("TEST+05:00", 5*3600)
	defer func() { fmtutil.DisplayZone = origZone }()

	dir := t.TempDir()
	at := func(h, m int) string {
		return time.Date(2026, 7, 24, h, m, 0, 0, time.UTC).Format(time.RFC3339)
	}
	mk := func(ts, clientKey, userMsg string) map[string]any {
		return map[string]any{
			"ts": ts, "dur_ms": 100, "model": "agent", "protocol": "openai-completions",
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
			"attempts": []map[string]any{{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite",
				"dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	records := []map[string]any{
		mk(at(0, 0), "alice", "alice session one hello"),
		mk(at(1, 0), "alice", "alice session two, a different topic entirely"),
		mk(at(2, 0), "bob", "heartbeat check [OpenClaw heartbeat poll]"),
	}
	path := writeTempJSONL(t, dir, records)
	rep, sess, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequestsIndex(rep, sess, dir, i18n.EN, nil, filepath.Join(dir, "details")); err != nil {
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
	if !strings.Contains(s, "## Chat User: alice · 2 sessions 2 tasks 2 turns") {
		t.Errorf("missing alice Chat User index entry with counts:\n%s", s)
	}
	if !strings.Contains(s, "[vmr-requests-alice.md](vmr-requests-alice.md)") {
		t.Errorf("missing link to alice's detail sibling:\n%s", s)
	}
	if !strings.Contains(s, "## Scheduled · heartbeat single-shot sessions × 1") {
		t.Errorf("missing collapsed heartbeat rollup index entry:\n%s", s)
	}
	if !strings.Contains(s, "[vmr-requests-cron-heartbeat.md](vmr-requests-cron-heartbeat.md)") {
		t.Errorf("missing link to heartbeat's cron detail sibling:\n%s", s)
	}
	if strings.Contains(s, "Chat User: bob") {
		t.Errorf("bob's only record is a single-shot heartbeat and must not get its own Chat User section:\n%s", s)
	}
	alice, err := os.ReadFile(filepath.Join(dir, "vmr-requests-alice.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(alice), "# Chat User: alice · 2 sessions 2 tasks 2 turns") {
		t.Errorf("alice's sibling should carry the full Chat User detail card:\n%s", alice)
	}
	// 00:00 UTC on the first record must render as 05:00 in fmtutil.DisplayZone
	// (TEST+05:00 above), not the source record's own (UTC) offset. The main
	// index carries no per-request timestamps now (the flat "all requests"
	// table is gone) — the session card in the per-tag sibling is where the
	// converted timestamp shows.
	if !strings.Contains(string(alice), "2026-07-24 05:00:00") {
		t.Errorf("timestamps should be converted to fmtutil.DisplayZone:\n%s", alice)
	}

	if _, err := os.Stat(filepath.Join(dir, "vmr-requests-bob.md")); !os.IsNotExist(err) {
		t.Errorf("bob has no interactive traffic and should get no per-tag sibling")
	}
	if _, err := os.Stat(filepath.Join(dir, "vmr-requests-cron-heartbeat.md")); err != nil {
		t.Errorf("missing scheduled-class sibling vmr-requests-cron-heartbeat.md: %v", err)
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
			"protocol": "openai-completions", "outcome": outcome,
			"client": map[string]any{
				"request":  map[string]any{"body": map[string]any{"model": "agent", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
				"response": map[string]any{"status": status, "body": map[string]any{}},
			},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	ok := base(t0, "ok", 200)
	ok["attempts"] = []map[string]any{{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "response": map[string]any{"status": 200}}}

	failed := base(t0.Add(time.Minute), "error", 500)
	failed["attempts"] = []map[string]any{{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "error": "transient: boom", "error_class": "transient"}}

	canceled := base(t0.Add(2*time.Minute), "canceled", 0)
	canceled["attempts"] = []map[string]any{{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "error": "canceled by client", "error_class": "canceled"}}

	truncated := base(t0.Add(3*time.Minute), "ok", 200)
	// Real shape: SetSuccessResponse commits the 2xx response first, then
	// SetTruncated (mid-stream death) only sets error/error_class — Response
	// stays as the already-committed 2xx.
	truncated["attempts"] = []map[string]any{{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite", "dur_ms": 100, "response": map[string]any{"status": 200}, "error": "truncated: EOF", "error_class": "truncated"}}

	return []map[string]any{ok, failed, canceled, truncated}
}

// TestFailedRequestRows checks the outcome filter: of failureSurfaceRecords'
// four records, exactly the error/canceled/ok-but-truncated three must
// survive — the plain "ok" one must not.
func TestFailedRequestRows(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, failureSurfaceRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
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
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	const wantEp = "openai-completions:volcengine:doubao-seed-2.0-lite"
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

// quirkNormRecords returns three successful requests against one endpoint
// (soft_block_detected once, think_strip once, and a clean response with
// only the routine model_rewrite step) plus one clean request against a
// second endpoint, all attempts also carrying model_rewrite (the ~100%
// baseline every successful response gets).
func quirkNormRecords() []map[string]any {
	base := func(ts time.Time, endpoint string, norm []string) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339Nano), "dur_ms": 100, "model": "agent",
			"protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request":  map[string]any{"body": map[string]any{"model": "agent", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
				"response": map[string]any{"status": 200, "body": map[string]any{}},
			},
			"attempts": []map[string]any{
				{"endpoint": endpoint, "dur_ms": 100, "response": map[string]any{"status": 200}, "norm": norm},
			},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	const minimax, openrouter = "openai-completions:minimax:m3", "openai-completions:openrouter:gpt"
	return []map[string]any{
		base(t0, minimax, []string{"model_rewrite", "soft_block_detected"}),
		base(t0.Add(time.Minute), minimax, []string{"model_rewrite", "think_strip"}),
		base(t0.Add(2*time.Minute), minimax, []string{"model_rewrite"}),
		base(t0.Add(3*time.Minute), openrouter, []string{"model_rewrite"}),
	}
}

// TestEndpointNormCounts verifies EndpointRow.NormCounts only tallies
// diagnosticNormMarker's curated "vendor quirk fix" subset of Attempt.Norm
// — model_rewrite (the routine, near-100%-hit-rate step every successful
// response carries) must never show up, or it would drown out the actually
// interesting soft-block/thinking-leak signal this field exists to surface
// — and attributes each marker to the specific endpoint the successful
// attempt that carried it actually hit, not globally.
func TestEndpointNormCounts(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, quirkNormRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var minimax, openrouter *EndpointRow
	for i, e := range rep.EndpointsAll {
		switch e.Endpoint {
		case "openai-completions:minimax:m3":
			minimax = &rep.EndpointsAll[i]
		case "openai-completions:openrouter:gpt":
			openrouter = &rep.EndpointsAll[i]
		}
	}
	if minimax == nil {
		t.Fatal("no EndpointsAll entry for openai-completions:minimax:m3")
	}
	if n := minimax.NormCounts["model_rewrite"]; n != 0 {
		t.Errorf("model_rewrite must be filtered out (routine, not a quirk), got count %d", n)
	}
	if n := minimax.NormCounts["soft_block_detected"]; n != 1 {
		t.Errorf("soft_block_detected = %d, want 1", n)
	}
	if n := minimax.NormCounts["think_strip"]; n != 1 {
		t.Errorf("think_strip = %d, want 1", n)
	}
	if len(minimax.NormCounts) != 2 {
		t.Errorf("minimax.NormCounts = %v, want exactly 2 keys (soft_block_detected, think_strip)", minimax.NormCounts)
	}

	if openrouter == nil {
		t.Fatal("no EndpointsAll entry for openai-completions:openrouter:gpt")
	}
	if len(openrouter.NormCounts) != 0 {
		t.Errorf("openrouter saw no quirk markers, want empty NormCounts, got %v", openrouter.NormCounts)
	}
}

// TestRenderReliabilityQuirkSection covers section_reliability.go's render
// side: the "Quirk Fix × Endpoint" table must appear (with the endpoint,
// marker, and a rate derived from EndpointRow.OK, not Attempts) when at
// least one EndpointRow.NormCounts entry is non-zero, and must be entirely
// absent (not an empty table) when the report has none.
func TestRenderReliabilityQuirkSection(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, quirkNormRecords())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(rep, i18n.EN, nil, nil)
	if !strings.Contains(md, "Quirk Fix × Endpoint") {
		t.Fatalf("markdown missing the quirk-by-endpoint section:\n%s", md)
	}
	if !containsAll(md, []string{"openai-completions:minimax:m3", "soft_block_detected", "think_strip"}) {
		t.Errorf("markdown missing expected endpoint/marker cells:\n%s", md)
	}
	if strings.Contains(md, "model_rewrite") {
		t.Error("model_rewrite must never appear in the quirk table — it's the filtered-out routine step")
	}
	// minimax.OK == 3 (all three requests succeeded), 1 soft_block_detected
	// hit -> 33%.
	if !strings.Contains(md, "1(33") {
		t.Errorf("expected a 1(33...%%) cell (1 hit / 3 OK attempts) for soft_block_detected, got:\n%s", md)
	}

	// Second report with no quirk markers at all: the section must not render.
	dir2 := t.TempDir()
	clean := []map[string]any{quirkNormRecords()[3]} // the openrouter record, only model_rewrite
	path2 := writeTempJSONL(t, dir2, clean)
	rep2, _, err := Build([]string{path2}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	md2 := Markdown(rep2, i18n.EN, nil, nil)
	if strings.Contains(md2, "Quirk Fix") {
		t.Errorf("quirk section must not render when no endpoint has a non-zero NormCounts:\n%s", md2)
	}
}

// TestWriteFailedIndex checks vmr-requests-failed.md/.jsonl: they must list
// exactly the 3 failed rows (not the 1 plain-ok row), link to detail files,
// and leave the normal per-group detail untouched (the per-tag sibling still
// renders every request, failed ones included) — the failed index is
// additive, not a move.
func TestWriteFailedIndex(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, failureSurfaceRecords())
	rep, sess, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := rep.RequestRows()

	if err := WriteFailedIndex(rows, dir, i18n.EN, filepath.Join(dir, "details")); err != nil {
		t.Fatal(err)
	}
	failedMD, err := os.ReadFile(filepath.Join(dir, "vmr-requests-failed.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(failedMD)
	if !strings.Contains(s, "3 total.") {
		t.Errorf("want exactly 3 failed rows reported:\n%s", s)
	}

	n, err := WriteRequestsJSONL(FailedRequestRows(rows), filepath.Join(dir, "vmr-requests-failed.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3 rows written to vmr-requests-failed.jsonl, got %d", n)
	}

	// The per-group detail is unaffected: the per-tag sibling still renders
	// every request, failed ones included (no client_key_tag on these
	// records, so they land in vmr-requests-unresolved.md).
	if err := WriteRequestsIndex(rep, sess, dir, i18n.EN, nil, filepath.Join(dir, "details")); err != nil {
		t.Fatal(err)
	}
	unresolvedMD, err := os.ReadFile(filepath.Join(dir, "vmr-requests-unresolved.md"))
	if err != nil {
		t.Fatal(err)
	}
	full := string(unresolvedMD)
	for _, want := range []string{"❌transient", "❌canceled", "⚠️trunc"} {
		if !strings.Contains(full, want) {
			t.Errorf("the per-group sibling should still carry %q inline, unmoved:\n%s", want, full)
		}
	}
}

func TestWriteFailedIndexEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFailedIndex(nil, dir, i18n.EN, filepath.Join(dir, "details")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "vmr-requests-failed.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "0 total.") {
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
			"ts": ts, "dur_ms": 100, "model": "agent", "protocol": "openai-completions",
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
		mk(t0.Format(time.RFC3339), "alice", "openai-completions:provider-a:model-a", "alice's first message"),
		mk(t0.Add(time.Minute).Format(time.RFC3339), "bob", "openai-completions:provider-b:model-b", "bob's first message"),
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
	// now is captured once, outside the loop, and reused for every run: this
	// test's whole point is map-iteration-order determinism (see doc
	// comment above), not wall-clock time — a fresh time.Now() per
	// iteration would make Meta.GeneratedAt legitimately differ (and the
	// byte-identical-JSON assertion below legitimately fail) if two calls
	// happened to straddle a second boundary, which is exactly the kind of
	// incidental flakiness this test exists to NOT have.
	now := time.Now()
	var want []byte
	for i := 0; i < runs; i++ {
		rep, _, err := Build([]string{path}, now, nil, nil, nil, nil)
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

// heartbeatDreamDiaryTiedRecords builds one heartbeat and one dream_diary
// record, each with zero cached tokens — an EXACT (not just rounded) tie on
// cache_efficiency (0/(0+fresh) = 0 for both) — but different TokensInFresh,
// so there is an unambiguous "worst offender" (heartbeat, with more fresh
// tokens) for buildFindings' "定时任务冗余" finding to pick.
func heartbeatDreamDiaryTiedRecords() []map[string]any {
	mk := func(ts, userMsg string, fresh int) map[string]any {
		return map[string]any{
			"ts": ts, "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": []any{
					map[string]any{"role": "user", "content": userMsg},
				}}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": fresh, "completion_tokens": 5,
						"prompt_tokens_details": map[string]any{"cached_tokens": 0}},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	return []map[string]any{
		mk(t0.Format(time.RFC3339), "[OpenClaw heartbeat poll]", 1000),
		mk(t0.Add(time.Minute).Format(time.RFC3339), "Write a dream diary entry", 100),
	}
}

// TestBuildFindingsIsDeterministic locks in a fix for a bug distinct from
// (but the same class as) TestBuildIsDeterministic above: buildFindings
// runs BEFORE Build's "---- sort all slices ----" pass, so its "pick the
// worst one, first-match-then-break" loops over rep.Tools/rep.ByModel/
// rep.Workloads read map-iteration order, not the deterministic sorted
// order those same slices end up in by the time Build returns. Confirmed
// via a real corpus (2026-07-16's audit log): the "定时任务冗余" finding
// alternated between "heartbeat" and "dream_diary" across repeated `vmr
// report` runs on byte-identical input, even though rep.Workloads itself
// (the JSON array) was proven identical every time — only buildFindings'
// PICK from it varied. This test reproduces that with a minimal fixture
// instead of depending on real log data being present.
func TestBuildFindingsIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, heartbeatDreamDiaryTiedRecords())

	const runs = 8
	for i := 0; i < runs; i++ {
		rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("run %d: Build: %v", i, err)
		}
		var found *Finding
		for j := range rep.Efficiency {
			if rep.Efficiency[j].Code == FindingCronRedundancy {
				found = &rep.Efficiency[j]
				break
			}
		}
		if found == nil {
			t.Fatalf("run %d: no 定时任务冗余 finding (both heartbeat and dream_diary should qualify: cache_efficiency=0)", i)
		}
		if found.Implicated != "heartbeat" {
			t.Fatalf("run %d: implicated = %q, want %q (heartbeat has more fresh tokens: 1000 vs 100 — the higher-fresh class must always win, not whichever the map happened to yield first)", i, found.Implicated, "heartbeat")
		}
	}
}

// toolShapeTieToolNames are two declared-tool sets used by
// TestBuildFindingsWorstToolTieIsDeterministic: five tools each, one name
// per set at each position, every name exactly 6 bytes long so their
// serialized "tools" arrays are byte-for-byte the same length. Only the
// ToolsSig hash (computed from these names) differs between the two shapes.
var (
	toolShapeTieNamesA = []string{"alpha1", "alpha2", "alpha3", "alpha4", "alpha5"}
	toolShapeTieNamesB = []string{"beta01", "beta02", "beta03", "beta04", "beta05"}
)

// toolShapeTieRecords returns two single-request records whose tool-waste
// finding ties exactly: each declares 5 same-size tools and calls none of
// them, so DeclareUtilization=0 and SchemaWasteBytes == SchemaBytesShipped
// == DeclaredBytes*1 for both shapes — identical by construction, with only
// the shape identity (ToolsSig) differing.
func toolShapeTieRecords() []map[string]any {
	toolsFor := func(names []string) []any {
		tools := make([]any, len(names))
		for i, n := range names {
			tools[i] = map[string]any{"name": n}
		}
		return tools
	}
	mk := func(ts time.Time, names []string) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "tools": toolsFor(names), "messages": []any{
					map[string]any{"role": "user", "content": "hi " + names[0]},
				}}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	return []map[string]any{
		mk(t0, toolShapeTieNamesA),
		mk(t0.Add(time.Minute), toolShapeTieNamesB),
	}
}

// TestBuildFindingsWorstToolTieIsDeterministic covers the "工具 schema 浪费"
// tie-break in buildFindings (metrics.go's worstTool loop), the first of the
// three tie-break sites TestBuildFindingsIsDeterministic above left
// unexercised (the fix's tie-break logic is identical in shape across all
// four findings, but only 定时任务冗余 had a regression test).
// toolShapeTieRecords ties SchemaWasteBytes exactly
// between two shapes; only their Shape string (ToolsSig) differs, so a
// deterministic pick must always prefer the alphabetically-smaller one —
// computed here via the package's own toolsSig, not hardcoded, since the
// hash is opaque from outside.
func TestBuildFindingsWorstToolTieIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, toolShapeTieRecords())

	wantShape := reqdetail.ToolsSig(toolShapeTieNamesA)
	if other := reqdetail.ToolsSig(toolShapeTieNamesB); other < wantShape {
		wantShape = other
	}
	wantImplicated := wantShape + "/1 requests"

	const runs = 8
	for i := 0; i < runs; i++ {
		rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("run %d: Build: %v", i, err)
		}
		var found *Finding
		for j := range rep.Efficiency {
			if rep.Efficiency[j].Code == FindingToolSchemaWaste {
				found = &rep.Efficiency[j]
				break
			}
		}
		if found == nil {
			t.Fatalf("run %d: no 工具 schema 浪费 finding (both shapes should qualify: 0%% utilization)", i)
		}
		if found.Implicated != wantImplicated {
			t.Fatalf("run %d: implicated = %q, want %q (tied SchemaWasteBytes must always resolve to the alphabetically-smaller Shape, not whichever the map happened to yield first)", i, found.Implicated, wantImplicated)
		}
	}
}

// modelFreshTieRecords returns two records on distinct models, each with the
// same fresh (uncached) input tokens — an exact tie on TokensInFresh for
// buildFindings' domModel loop ("缓存未命中输入" finding's implicated-model
// note), and together exactly half the global fresh total each, satisfying
// the ">= fresh/2" threshold that decides whether a dominant model gets
// named at all.
func modelFreshTieRecords() []map[string]any {
	mk := func(ts time.Time, model string) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": model, "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": model, "messages": []any{
					map[string]any{"role": "user", "content": "hi from " + model},
				}}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": model,
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": 500, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:" + model, "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	return []map[string]any{
		mk(t0, "model-a"),
		mk(t0.Add(time.Minute), "model-b"),
	}
}

// TestBuildFindingsDomModelTieIsDeterministic covers the second untested
// tie-break site: buildFindings' domModel loop for "缓存未命中输入". Both
// models in modelFreshTieRecords have TokensInFresh=500 (tied) against a
// global fresh total of 1000, so the tie-break (alphabetically-smaller
// Model name) is the only thing deciding which model gets named.
func TestBuildFindingsDomModelTieIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, modelFreshTieRecords())

	const runs = 8
	for i := 0; i < runs; i++ {
		rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("run %d: Build: %v", i, err)
		}
		var found *Finding
		for j := range rep.Efficiency {
			if rep.Efficiency[j].Code == FindingCacheMiss {
				found = &rep.Efficiency[j]
				break
			}
		}
		if found == nil {
			t.Fatalf("run %d: no 缓存未命中输入 finding", i)
		}
		if !strings.Contains(found.Implicated, "model-a") || strings.Contains(found.Implicated, "model-b") {
			t.Fatalf("run %d: implicated = %q, want it to name model-a (alphabetically smaller on an exact TokensInFresh tie), never model-b", i, found.Implicated)
		}
	}
}

// sessionGrowthTieRecords returns two 2-turn sessions whose ContextGrowth
// ties exactly (100 -> 1000 input tokens, x10 growth, both well above the
// >=5 finding threshold) but whose distinct opening messages give them
// different anchors/session IDs — session A opens first (gets "s01"),
// session B second ("s02"), so a deterministic pick must always prefer s01.
func sessionGrowthTieRecords() []map[string]any {
	mkTurn := func(ts time.Time, opening string, extra []any, promptTokens int) map[string]any {
		msgs := append([]any{map[string]any{"role": "user", "content": opening}}, extra...)
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": msgs}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": promptTokens, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	var recs []map[string]any
	recs = append(recs,
		mkTurn(t0, "session growth fixture A", nil, 100),
		mkTurn(t0.Add(time.Minute), "session growth fixture A", []any{
			map[string]any{"role": "assistant", "content": "ack"},
			map[string]any{"role": "user", "content": "continue A"},
		}, 1000),
	)
	recs = append(recs,
		mkTurn(t0.Add(2*time.Minute), "session growth fixture B", nil, 100),
		mkTurn(t0.Add(3*time.Minute), "session growth fixture B", []any{
			map[string]any{"role": "assistant", "content": "ack"},
			map[string]any{"role": "user", "content": "continue B"},
		}, 1000),
	)
	return recs
}

// TestBuildFindingsContextGrowthTieIsDeterministic covers the third and
// last untested tie-break site: buildFindings' worst-session loop for
// "上下文膨胀". Both sessions in sessionGrowthTieRecords tie exactly on
// ContextGrowth (x10). Session IDs are now content-addressed
// (Lineage.LineageID, P6.1) rather than positional s01/s02, so which of
// the two fixture sessions is lexicographically smaller is fixed by their
// content hashes, not knowable in advance — this test computes it from
// run 0 instead of hardcoding "s01", then asserts every subsequent run
// agrees, which is the actual property under test: the tie-break
// (metrics.go's "s.ID < worst.ID") always resolves to the SAME session
// across repeated runs on the same input, not whichever session
// rep.Sessions' as-yet-unsorted order happened to visit first.
func TestBuildFindingsContextGrowthTieIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, sessionGrowthTieRecords())

	const runs = 8
	var wantImplicated string
	for i := 0; i < runs; i++ {
		rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("run %d: Build: %v", i, err)
		}
		if len(rep.Sessions) != 2 {
			t.Fatalf("run %d: sessions = %d, want 2", i, len(rep.Sessions))
		}
		var found *Finding
		for j := range rep.Efficiency {
			if rep.Efficiency[j].Code == FindingContextGrowth {
				found = &rep.Efficiency[j]
				break
			}
		}
		if found == nil {
			t.Fatalf("run %d: no 上下文膨胀 finding (both sessions should qualify: growth x10 >= 5)", i)
		}
		if i == 0 {
			wantImplicated = found.Implicated
			smaller := rep.Sessions[0].ID
			if rep.Sessions[1].ID < smaller {
				smaller = rep.Sessions[1].ID
			}
			if !strings.HasPrefix(wantImplicated, smaller+" ") {
				t.Fatalf("run 0: implicated = %q, want it to start with the lexicographically smaller session id %q (tie-break rule is s.ID < worst.ID)", wantImplicated, smaller)
			}
			continue
		}
		if found.Implicated != wantImplicated {
			t.Fatalf("run %d: implicated = %q, want %q (tied ContextGrowth must always resolve to the same session across runs)", i, found.Implicated, wantImplicated)
		}
	}
}

// contextGrowthContractFixture reproduces the ContextGrowth dirty-value case:
// a session that grows normally for a few turns, then hits a Contract-style
// edit (anchor survives, everything else collapses), then keeps growing on
// the other side. Before the session-grouping fix, this whole thing was ONE
// report session (anchor never changed), so
// ContextGrowth (last/first tokens_in) compared a post-reset token count
// against a pre-reset one — numerically whatever it happened to be, but
// meaningless either way, since the two sides never shared a context.
func contextGrowthContractFixture() []map[string]any {
	mkTurn := func(ts time.Time, msgs []any, promptTokens int) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": msgs}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": promptTokens, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	sys := map[string]any{"role": "system", "content": "sys"}
	u1 := map[string]any{"role": "user", "content": "深入调研内存涨价 context growth fixture"}

	var recs []map[string]any
	msgs := []any{sys, u1}
	// 6 pre-contract turns with distinct assistant steps so the accumulated
	// key set reaches 7 distinct keys — enough for a Contract opening that
	// preserves 3 distinct keys (anchor + the two most recent steps) and
	// still strictly satisfies len(cur) < 0.6 * len(prev_last).
	for i, tok := range []int{100, 500, 2000, 4000, 6000, 8000} {
		recs = append(recs, mkTurn(t0.Add(time.Duration(i)*time.Minute), append([]any{}, msgs...), tok))
		msgs = append(msgs, map[string]any{"role": "assistant", "content": "step " + strconv.Itoa(i)})
	}
	// Contract: history collapses to [sys v2, u1, step3, step4] — the
	// opening instruction plus the two most recent replies survive
	// verbatim, clearing stitchMinAbsOverlap (3 shared distinct keys).
	recs = append(recs, mkTurn(t0.Add(10*time.Minute),
		[]any{map[string]any{"role": "system", "content": "sys v2"}, u1,
			map[string]any{"role": "assistant", "content": "step 3"},
			map[string]any{"role": "assistant", "content": "step 4"}}, 150))
	// Post-contract growth continues independently, to its own honest 6x peak.
	recs = append(recs, mkTurn(t0.Add(11*time.Minute),
		[]any{map[string]any{"role": "system", "content": "sys v2"}, u1,
			map[string]any{"role": "assistant", "content": "step 3"},
			map[string]any{"role": "assistant", "content": "step 4"},
			map[string]any{"role": "assistant", "content": "continuing"}}, 900))
	return recs
}

// TestContextGrowthDoesNotCrossContractBreak covers the case where group()
// now splits a session at every Contract/Fork edit, so ContextGrowth's
// last/first ratio can no longer straddle a hidden history reset — each of
// the two resulting sessions gets its own honest, independently-computed
// growth figure instead of one meaningless number spanning both. No separate
// "segment by lineage, take the longest segment" algorithm turned out to be
// needed: a SessionInfo IS already exactly one Lineage's worth of records
// post-grouping-fix, so there is nothing left to segment.
func TestContextGrowthDoesNotCrossContractBreak(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, contextGrowthContractFixture())
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (report must split at the Contract break)", len(rep.Sessions))
	}
	// Session ids are now content-addressed (Lineage.LineageID, P6.1), not
	// positional s01/s02 — DisplayAlias still carries the old positional
	// label (kept purely for human display, see SessionInfo.ID's doc
	// comment), so use that to pick out "the pre-contract session" and
	// "the post-contract session" instead of assuming a specific hash.
	byAlias := map[string]*SessionRow{}
	for i := range rep.Sessions {
		byAlias[rep.Sessions[i].Alias] = &rep.Sessions[i]
	}
	s1, s2 := byAlias["s01"], byAlias["s02"]
	if s1 == nil || s2 == nil {
		t.Fatalf("expected sessions aliased s01/s02, got %+v", rep.Sessions)
	}
	if s1.ContextGrowth != 80 {
		t.Errorf("session 1 ContextGrowth = %v, want 80 (100 -> 8000 within its own pre-contract lineage)", s1.ContextGrowth)
	}
	if s2.ContextGrowth != 6 {
		t.Errorf("session 2 ContextGrowth = %v, want 6 (150 -> 900 within its own post-contract lineage, independent of session 1) — a value computed across the Contract break would land somewhere else entirely", s2.ContextGrowth)
	}
	if s2.ContinuedFrom != s1.ID {
		t.Errorf("session 2 ContinuedFrom = %q, want %q (linkStitchedLineages should still connect the two for display, even though their ContextGrowth figures stay separate)", s2.ContinuedFrom, s1.ID)
	}
}

// TestBuildCompactionsEntitySplitAndTokens covers the §6.7 Compaction
// Reconstruction section: a standalone compaction call whose input
// mentions two file paths and whose summary output keeps only one of them —
// the surviving one must land in SurvivedEntities, the dropped one in
// SwallowedEntities, and tokens_in/tokens_out must come from the compaction
// call's OWN usage, not either neighboring session's.
func TestBuildCompactionsEntitySplitAndTokens(t *testing.T) {
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	rec := map[string]any{
		"ts": t0.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
		"client": map[string]any{
			"request": map[string]any{"body": map[string]any{
				"model": "agent",
				"messages": []any{
					map[string]any{"role": "system", "content": "You are a context summarization assistant."},
					map[string]any{"role": "user", "content": "worked on keep.go and drop.go together"},
				},
			}},
			"response": map[string]any{"status": 200, "body": map[string]any{
				"model": "agent",
				"choices": []any{map[string]any{"finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "Summary: continued work on keep.go"}}},
				"usage": map[string]any{"prompt_tokens": 5000, "completion_tokens": 300},
			}},
		},
		"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
	}

	dir := t.TempDir()
	path := writeTempJSONL(t, dir, []map[string]any{rec})
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Compactions) != 1 {
		t.Fatalf("compactions = %d, want 1: %+v", len(rep.Compactions), rep.Compactions)
	}
	c := rep.Compactions[0]
	if c.TokensIn != 5000 || c.TokensOut != 300 {
		t.Errorf("tokens = %d/%d, want 5000/300 (the compaction call's own usage)", c.TokensIn, c.TokensOut)
	}
	if len(c.SurvivedEntities) != 1 || c.SurvivedEntities[0] != "keep.go" {
		t.Errorf("survived entities = %v, want [keep.go]", c.SurvivedEntities)
	}
	if len(c.SwallowedEntities) != 1 || c.SwallowedEntities[0] != "drop.go" {
		t.Errorf("swallowed entities = %v, want [drop.go]", c.SwallowedEntities)
	}

	md := Markdown(rep, i18n.EN, nil, nil)
	if !strings.Contains(md, "§6.7 Compaction Reconstruction") {
		t.Error("rendered Markdown missing the §6.7 Compaction section header")
	}
	if !strings.Contains(md, "drop.go") {
		t.Errorf("rendered Markdown should surface the swallowed entity sample:\n%s", md)
	}
}

// TestRenderCompactionsTSConvertsToDisplayZone proves the §6.7 Compaction
// table's TS column goes through fmtutil.DisplayZone like every other
// human-facing timestamp in this package, rather than a raw cut() of the
// record's own embedded offset (regression: section_compaction.go's
// renderCompactions used to call cut(c.TS, 19), which showed the source
// offset verbatim and never converted at all).
func TestRenderCompactionsTSConvertsToDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.FixedZone("TEST+05:00", 5*3600)
	defer func() { fmtutil.DisplayZone = origZone }()

	rep := &Report2{Compactions: []CompactionRow{
		{TS: "2026-07-24T00:00:00Z", TokensIn: 100, TokensOut: 10},
	}}
	md := Markdown(rep, i18n.EN, nil, nil)
	if !strings.Contains(md, "2026-07-24 05:00:00") {
		t.Errorf("compaction TS should render as 05:00:00 in DisplayZone (TEST+05:00), not the source 00:00:00 UTC:\n%s", md)
	}
	if strings.Contains(md, "2026-07-24T00:00:00Z") {
		t.Errorf("compaction TS rendered the raw RFC3339 string verbatim instead of converting through DisplayZone:\n%s", md)
	}
}

// TestRenderCompactionsZeroUsageIsNotAMeasuredZero: a compaction row whose
// usage never parsed (TokensIn == TokensOut == 0) must render its size cell
// as "-", not a literal "0 → 0" that reads as a measured no-op (问题 27 /
// R1-7). The linkage and swallowed-entity columns still carry their info.
func TestRenderCompactionsZeroUsageIsNotAMeasuredZero(t *testing.T) {
	rep := &Report2{Compactions: []CompactionRow{
		{TS: "2026-07-24T00:00:00Z", Summarizes: "l-77c20384", SwallowedEntities: []string{"~/ENV.md"}},
	}}
	md := Markdown(rep, i18n.EN, nil, nil)
	if strings.Contains(md, "0 → 0") {
		t.Errorf("zero-usage compaction rendered a literal \"0 → 0\":\n%s", md)
	}
	if !strings.Contains(md, "l-77c20384") || !strings.Contains(md, "~/ENV.md") {
		t.Errorf("zero-usage row should still show its linkage and swallowed entities:\n%s", md)
	}
}

// TestMarkdownTableCellsWithPercentRenderVerbatim is a regression test ensuring cells
// containing a literal "%" (such as percentages) render verbatim without being reinterpreted
// as format verbs by fmt.Fprintf.
func TestMarkdownTableCellsWithPercentRenderVerbatim(t *testing.T) {
	rep := &Report2{
		Overall: Row{TrafficStats: TrafficStats{Requests: 10, OK: 9, TokensIn: 100, TokensInCached: 90, TokensKnown: 10, CacheEfficiency: 0.9, RequestsWithDur: 10, DurMSP95: 500}},
		EndpointsAll: []EndpointRow{
			{Endpoint: "openai-completions:p:m", Attempts: 10, OK: 9, Availability: 0.9, ErrorRate: 10,
				ErrorClasses: map[string]int{"transient": 1}},
		},
	}
	md := Markdown(rep, i18n.EN, nil, nil)
	if strings.Contains(md, "MISSING") {
		t.Fatalf("rendered Markdown contains a corrupted Printf verb (percent sign in a table cell mishandled):\n%s", md)
	}
	if !strings.Contains(md, "90.0%") {
		t.Errorf("expected a literal cache-efficiency percentage in the output; got:\n%s", md)
	}
}

// TestMarkdownEscapesUserDerivedTitles is the B4 regression: a session
// title carrying a shell pipe or an unclosed HTML comment must not corrupt
// the report — the pipe is escaped so it doesn't split the table row, the
// "<!--" is escaped so an HTML-aware renderer doesn't swallow the rest of
// the file.
func TestMarkdownEscapesUserDerivedTitles(t *testing.T) {
	rep := &Report2{
		Overall: Row{TrafficStats: TrafficStats{Requests: 1, OK: 1}},
		Sessions: []SessionRow{
			{ID: "l-deadbeef", Class: "interactive", ClientKey: "cli", Tasks: 1,
				TrafficStats: TrafficStats{Requests: 1},
				Title:        "run `ps aux | grep vmr` <!-- unclosed"},
		},
	}
	md := Markdown(rep, i18n.EN, nil, nil)
	if strings.Contains(md, "aux | grep") {
		t.Errorf("raw pipe in a session title leaked into the table (row corruption):\n%s", md)
	}
	if !strings.Contains(md, `aux \| grep`) {
		t.Errorf("session-title pipe was not escaped:\n%s", md)
	}
	if strings.Contains(md, "<!--") {
		t.Errorf("unclosed HTML comment in a session title left unescaped:\n%s", md)
	}
	if !strings.Contains(md, "&lt;!--") {
		t.Errorf("session-title '<!--' was not HTML-escaped:\n%s", md)
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

// TestBuildDateHourBucketsUseDisplayZone proves buildRec2 derives its
// byDate/hoursOfDay bucket keys through fmtutil.DisplayZone rather than the
// record's own embedded offset (the bug absorbed from an independent
// Gemini analysis of this same timezone problem: a naive
// arec.TS.Format("2006-01-02")/.Hour() reads whatever offset the record
// happened to carry, which can silently misfile a request into the wrong
// calendar day/hour of the report's byDate/hoursOfDay charts). The record
// below is 23:30 UTC — late on 2026-07-24 in its own zone, but 04:30 the
// next calendar day once converted to a +05:00 DisplayZone.
func TestBuildDateHourBucketsUseDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.FixedZone("TEST+05:00", 5*3600)
	defer func() { fmtutil.DisplayZone = origZone }()

	dir := t.TempDir()
	ts := time.Date(2026, 7, 24, 23, 30, 0, 0, time.UTC)
	record := map[string]any{
		"ts": ts.Format(time.RFC3339Nano), "dur_ms": 100, "model": "agent", "protocol": "openai-completions",
		"outcome": "ok", "stream": false,
		"client": map[string]any{
			"request": map[string]any{"body": map[string]any{"model": "agent", "messages": []any{
				map[string]any{"role": "user", "content": "hi"},
			}}},
			"response": map[string]any{"status": 200, "body": map[string]any{
				"model":   "agent",
				"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "ok"}}},
				"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
			}},
		},
		"attempts": []map[string]any{{"endpoint": "openai-completions:volcengine:doubao-seed-2.0-lite",
			"dur_ms": 100, "response": map[string]any{"status": 200}}},
	}
	path := writeTempJSONL(t, dir, []map[string]any{record})
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.ByDate) != 1 || rep.ByDate[0].Date != "2026-07-25" {
		t.Errorf("ByDate = %+v, want a single 2026-07-25 bucket (23:30 UTC converted to +05:00 crosses midnight)", rep.ByDate)
	}
	if len(rep.HoursOfDay) != 1 || rep.HoursOfDay[0].Hour != 4 {
		t.Errorf("HoursOfDay = %+v, want a single hour=4 bucket (23:30 UTC + 5h = 04:30)", rep.HoursOfDay)
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
		_, _, _ = Build([]string{path}, time.Now(), nil, nil, nil, nil)
	}
}

// TestAddAttempt_ForwardedCountsTruncated pins the one way EndpointRow.
// Forwarded deliberately differs from OK: a 2xx response that broke
// mid-copy carries Error = "truncated: …" (audit.Attempt.SetTruncated), so
// it leaves OK — but the router already charged quota for it, which is why
// Forwarded (not OK, not the request-level Requests) is the basis
// providerquota.go's requests metric reproduces. cmd/vmr/quota_parity_test.go
// asserts the end-to-end equality; this pins the counter itself.
func TestAddAttempt_ForwardedCountsTruncated(t *testing.T) {
	dir := t.TempDir()
	// status 0 means the attempt never got a response at all (transport
	// failure); such a record carries no "response" key, which is exactly what
	// makes it neither OK nor Forwarded.
	mk := func(ts time.Time, attemptErr string, status int) map[string]any {
		att := map[string]any{"endpoint": "openai-completions:acct1:m1", "dur_ms": 10, "error": attemptErr}
		if status > 0 {
			att["response"] = map[string]any{"status": status}
		}
		return map[string]any{
			"ts": ts.Format(time.RFC3339Nano), "dur_ms": 10, "model": "coding",
			"protocol": "openai-completions", "outcome": "ok",
			"client":   map[string]any{"request": map[string]any{"body": map[string]any{"model": "coding"}}},
			"attempts": []map[string]any{att},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	path := writeTempJSONL(t, dir, []map[string]any{
		mk(t0, "", 200), // clean success: OK + Forwarded
		mk(t0.Add(time.Minute), "truncated: EOF", 200),   // forwarded + charged, but NOT OK
		mk(t0.Add(2*time.Minute), "upstream 429", 429),   // neither
		mk(t0.Add(3*time.Minute), "network: refused", 0), // no response at all: neither
	})
	rep, _, err := Build([]string{path}, t0.Add(time.Hour), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var row *EndpointRow
	for i := range rep.EndpointsAll {
		if rep.EndpointsAll[i].Endpoint == "openai-completions:acct1:m1" {
			row = &rep.EndpointsAll[i]
		}
	}
	if row == nil {
		t.Fatalf("no endpoint row for openai-completions:acct1:m1: %+v", rep.EndpointsAll)
	}
	if row.Attempts != 4 {
		t.Errorf("Attempts = %d, want 4", row.Attempts)
	}
	if row.OK != 1 {
		t.Errorf("OK = %d, want 1 (the truncated 2xx carries an Error, so it must not count here)", row.OK)
	}
	if row.Forwarded != 2 {
		t.Errorf("Forwarded = %d, want 2 (clean 2xx + truncated 2xx — both were forwarded and both were charged)", row.Forwarded)
	}
}

func TestClientEndpointScale(t *testing.T) {
	clients, rows := clientEndpointScale([]ClientEndpointRow{
		{ClientKey: "a", Endpoint: "e1"},
		{ClientKey: "a", Endpoint: "e2"},
		{ClientKey: "b", Endpoint: "e1"},
	})
	if clients != 2 || rows != 3 {
		t.Errorf("got %d client(s) x %d row(s), want 2 x 3", clients, rows)
	}
	if clients, rows := clientEndpointScale(nil); clients != 0 || rows != 0 {
		t.Errorf("empty input: got %d x %d, want 0 x 0", clients, rows)
	}
}

// TestContextGrowthInFallback pins review P-06: finishSession's
// ContextGrowth ratio must fall back to the degraded estimate when the
// session's first turn's upstream reported no usage — otherwise a session
// that opens with an opaque/usage-less response reads "no growth" (0)
// forever even though later turns DO report usage. The fallback uses
// manifest.EstIn (ctxgraph's EstimateDegradedTokens on the same basis
// report's cost path uses) — see contextGrowthIn's doc comment.
func TestContextGrowthInFallback(t *testing.T) {
	cases := []struct {
		name string
		r    *ReqInfo
		want int64
	}{
		{
			name: "usage known wins",
			r: &ReqInfo{
				UsageOK:  true,
				Usage:    chatmsg.Usage{In: 1200},
				manifest: &ctxgraph.Manifest{EstIn: 999}, // must be ignored
			},
			want: 1200,
		},
		{
			name: "no usage falls back to manifest estimate",
			r: &ReqInfo{
				UsageOK:  false,
				manifest: &ctxgraph.Manifest{EstIn: 3400},
			},
			want: 3400,
		},
		{
			name: "no usage and no manifest is zero",
			r: &ReqInfo{
				UsageOK:  false,
				manifest: nil,
			},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextGrowthIn(tc.r); got != tc.want {
				t.Errorf("contextGrowthIn = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestContextGrowthFallsBackToEstimateEndToEnd drives the same fallback
// through a full Build: a two-turn session whose FIRST turn's response has
// no usage block (so UsageOK is false and the ratio must use the degraded
// estimate as its denominator) and whose second turn reports real usage.
// Before the fix this produced ContextGrowth 0; now it must produce
// last/est-first, proving the session-level metric no longer silently
// reports "no growth" for a session whose first turn just lacked usage
// (review P-06).
func TestContextGrowthFallsBackToEstimateEndToEnd(t *testing.T) {
	// mkNoUsageTurn builds a turn whose response body has no "usage" key.
	mkNoUsageTurn := func(ts time.Time, msgs []any, msg string) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": msgs}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": msg}}},
					// no "usage" key at all
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	// mkUsageTurn reports real usage.
	mkUsageTurn := func(ts time.Time, msgs []any, msg string, prompt int64) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": msgs}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": msg}}},
					"usage": map[string]any{"prompt_tokens": prompt, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	u1 := map[string]any{"role": "user", "content": "first instruction"}
	// Turn 1: response has NO usage — ratio denominator must come from the
	// degraded estimate. Turn 2: real usage (9000).
	recs := []map[string]any{
		mkNoUsageTurn(t0, []any{u1}, "step one"),
		mkUsageTurn(t0.Add(time.Minute), []any{u1, map[string]any{"role": "assistant", "content": "step one"}, map[string]any{"role": "user", "content": "continue"}}, "step two", 9000),
	}
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, recs)
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1 (a two-turn same-lineage session)", len(rep.Sessions))
	}
	if rep.Sessions[0].ContextGrowth == 0 {
		t.Fatal("ContextGrowth = 0: the fallback did not engage — the session's first turn has no usage, so the ratio must use the degraded estimate as its denominator (review P-06)")
	}
	// The estimate of the first turn's tiny request body is a small number
	// while the last turn reports 9000 real usage tokens, so the ratio is
	// large — the assertion that matters is that it is no longer silently
	// 0 and that it reflects genuine growth (last > first baseline).
	if rep.Sessions[0].ContextGrowth <= 1 {
		t.Errorf("ContextGrowth = %v, want > 1 (last turn's real usage / first turn's estimated baseline)", rep.Sessions[0].ContextGrowth)
	}
}
