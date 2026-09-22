// Ver 2026-07-25, by Sonnet 5

package report

import (
	"math"
	"os"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/taskseg"
)

var realLogPath = "../../logs/vmr-audit-2026-07-24.jsonl"

// TestBuild_InvariantsOnSyntheticCorpus verifies core report invariants against
// a hermetic, in-memory corpus on every clone and in CI without requiring
// external uncompressed audit logs.
func TestBuild_InvariantsOnSyntheticCorpus(t *testing.T) {
	at := func(sec int) time.Time { return time.Date(2026, 7, 24, 10, 0, sec, 0, time.UTC) }

	mkRec := func(sec int, model, clientTag string, dur, ttft int64, in, out, cached, write int64) audit.Record {
		body := map[string]any{
			"model": model,
			"messages": []any{
				map[string]any{"role": "user", "content": "hello world"},
			},
		}
		respBody := map[string]any{
			"model": model,
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "response"}},
			},
			"usage": map[string]any{
				"prompt_tokens":     in,
				"completion_tokens": out,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": cached,
				},
				"cache_creation_input_tokens": write,
			},
		}
		return audit.Record{
			TS:           at(sec),
			Model:        model,
			Protocol:     "openai-completions",
			Outcome:      "ok",
			Stream:       true,
			DurMS:        dur,
			TTFTMS:       ttft,
			ClientKeyTag: clientTag,
			Client: audit.Exchange{
				Request:  audit.Message{Body: body},
				Response: &audit.Message{Status: 200, Body: respBody},
			},
			Attempts: []audit.Attempt{
				{
					Endpoint: "openai-completions:provider1:" + model,
					DurMS:    dur,
					Response: &audit.Message{Status: 200},
				},
			},
		}
	}

	recs := []audit.Record{
		mkRec(0, "model-a", "client-1", 500, 100, 1000, 200, 800, 0),
		mkRec(1, "model-a", "client-1", 800, 150, 1200, 300, 500, 100),
		mkRec(2, "model-b", "client-2", 400, 80, 500, 100, 0, 0),
		mkRec(3, "model-b", "client-2", 1200, 200, 2000, 500, 1500, 200),
	}

	path := writeJSONL(t, recs)
	rep, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// ---- internal invariants ----
	// fresh = in - cached - cache_write
	if rep.Overall.TokensInFresh != rep.Overall.TokensIn-rep.Overall.TokensInCached-rep.Overall.TokensInCacheWrite {
		t.Errorf("fresh invariant broken: %d != %d - %d - %d",
			rep.Overall.TokensInFresh, rep.Overall.TokensIn, rep.Overall.TokensInCached, rep.Overall.TokensInCacheWrite)
	}
	// cache_eff formula
	if rep.Overall.TokensKnown > 0 {
		want := cacheEff(rep.Overall.TokensInCached, rep.Overall.TokensInFresh)
		if math.Abs(float64(rep.Overall.CacheEfficiency)-float64(want)) > 1e-6 {
			t.Errorf("cache_eff mismatch: want %.4f got %.4f", want, rep.Overall.CacheEfficiency)
		}
	}
	// sum(by_model) == overall
	var sumReq, sumIn int
	for _, m := range rep.ByModel {
		sumReq += m.Requests
		sumIn += int(m.TokensIn)
	}
	if sumReq != rep.Overall.Requests {
		t.Errorf("sum(by_model.requests)=%d != overall=%d", sumReq, rep.Overall.Requests)
	}
	if sumIn != int(rep.Overall.TokensIn) {
		t.Errorf("sum(by_model.tokens_in)=%d != overall=%d", sumIn, rep.Overall.TokensIn)
	}
	// sum(by_client) == overall
	var clientReq int
	for _, c := range rep.ByClient {
		clientReq += c.Requests
	}
	if clientReq != rep.Overall.Requests {
		t.Errorf("sum(by_client.requests)=%d != overall=%d", clientReq, rep.Overall.Requests)
	}
	// stream_ms p95 <= dur p95
	if rep.Overall.StreamMSP95 > rep.Overall.DurMSP95 {
		t.Errorf("stream_ms_p95(%d) > dur_p95(%d)", rep.Overall.StreamMSP95, rep.Overall.DurMSP95)
	}
	// dur p50 <= dur p95
	if rep.Overall.DurMSP50 > rep.Overall.DurMSP95 {
		t.Errorf("dur p50(%d) > dur p95(%d)", rep.Overall.DurMSP50, rep.Overall.DurMSP95)
	}
	// each workload cache_eff in [0,1]
	for _, wl := range rep.Workloads {
		if wl.CacheEfficiency < 0 || wl.CacheEfficiency > 1 {
			t.Errorf("workload %s cache_eff out of range: %.4f", wl.Class, wl.CacheEfficiency)
		}
	}
}

// TestRealLogE2E runs against an actual historical audit file when explicitly
// requested via RUN_REAL_LOG_E2E=1. Skipped by default.
func TestRealLogE2E(t *testing.T) {
	if os.Getenv("RUN_REAL_LOG_E2E") != "1" {
		t.Skip("skipping real audit log e2e test by default; set RUN_REAL_LOG_E2E=1 to run against historical files")
	}
	targetPath := realLogPath
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if _, errZst := os.Stat(targetPath + ".zst"); errZst == nil {
			targetPath = targetPath + ".zst"
		} else {
			t.Skip("real audit log not present; skipping e2e test on this clone")
		}
	}
	if os.Getenv("SKIP_SLOW_E2E") == "1" {
		t.Skip("SKIP_SLOW_E2E set")
	}

	new, _, _, err := BuildCached([]string{targetPath}, time.Now(), nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// ---- internal invariants ----
	// fresh = in - cached - cache_write
	if new.Overall.TokensInFresh != new.Overall.TokensIn-new.Overall.TokensInCached-new.Overall.TokensInCacheWrite {
		t.Errorf("fresh invariant broken: %d != %d - %d - %d",
			new.Overall.TokensInFresh, new.Overall.TokensIn, new.Overall.TokensInCached, new.Overall.TokensInCacheWrite)
	}
	// cache_eff formula
	if new.Overall.TokensKnown > 0 {
		want := cacheEff(new.Overall.TokensInCached, new.Overall.TokensInFresh)
		if math.Abs(float64(new.Overall.CacheEfficiency)-float64(want)) > 1e-6 {
			t.Errorf("cache_eff mismatch: want %.4f got %.4f", want, new.Overall.CacheEfficiency)
		}
	}
	// sum(by_model) == overall
	var sumReq, sumIn int
	for _, m := range new.ByModel {
		sumReq += m.Requests
		sumIn += int(m.TokensIn)
	}
	if sumReq != new.Overall.Requests {
		t.Errorf("sum(by_model.requests)=%d != overall=%d", sumReq, new.Overall.Requests)
	}
	if sumIn != int(new.Overall.TokensIn) {
		t.Errorf("sum(by_model.tokens_in)=%d != overall=%d", sumIn, new.Overall.TokensIn)
	}
	// sum(by_client) == overall
	var clientReq int
	for _, c := range new.ByClient {
		clientReq += c.Requests
	}
	if clientReq != new.Overall.Requests {
		t.Errorf("sum(by_client.requests)=%d != overall=%d", clientReq, new.Overall.Requests)
	}
	// stream_ms p95 <= dur p95
	if new.Overall.StreamMSP95 > new.Overall.DurMSP95 {
		t.Errorf("stream_ms_p95(%d) > dur_p95(%d)", new.Overall.StreamMSP95, new.Overall.DurMSP95)
	}
	// dur p50 <= dur p95
	if new.Overall.DurMSP50 > new.Overall.DurMSP95 {
		t.Errorf("dur p50(%d) > dur p95(%d)", new.Overall.DurMSP50, new.Overall.DurMSP95)
	}
	// each workload cache_eff in [0,1]
	for _, wl := range new.Workloads {
		if wl.CacheEfficiency < 0 || wl.CacheEfficiency > 1 {
			t.Errorf("workload %s cache_eff out of range: %.4f", wl.Class, wl.CacheEfficiency)
		}
	}

	// ---- design-doc anchors (qualitative sanity checks) ----
	// heartbeat should have low cache efficiency (<30%)
	var heartbeat *WorkloadRow
	for i := range new.Workloads {
		if new.Workloads[i].Class == "heartbeat" {
			heartbeat = &new.Workloads[i]
			break
		}
	}
	if heartbeat == nil {
		t.Errorf("missing heartbeat workload row")
	} else if heartbeat.CacheEfficiency >= 0.30 {
		t.Errorf("heartbeat cache_eff expected <30%%, got %.1f%%", heartbeat.CacheEfficiency*100)
	}
	// a tool shape should exist with utilization ≈6% (4/67 used)
	var bigShape *ToolShapeRow
	for i := range new.Tools {
		if len(new.Tools[i].Declared) >= 60 {
			bigShape = &new.Tools[i]
			break
		}
	}
	if bigShape == nil {
		t.Errorf("missing large tool shape (tools:67-ish)")
	} else if bigShape.DeclareUtilization > 0.10 || bigShape.SchemaWasteBytes == 0 {
		t.Errorf("tool shape utilization=%.2f, waste=%d (expected ~6%% with waste >0)", bigShape.DeclareUtilization, bigShape.SchemaWasteBytes)
	}
}
