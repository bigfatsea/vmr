// Ver 2026-07-25, by Sonnet 5

package report

import (
	"math"
	"os"
	"testing"
	"time"
)

var realLogPath = "../../logs/vmr-audit-2026-07-24.jsonl"

// TestRealLogE2E runs against the actual audit file (if present) and verifies
// internal invariants and design-doc anchors. This used to also cross-
// validate raw totals against a since-deleted, independently-implemented
// legacy aggregator; the invariant and anchor checks below are what's left
// to catch a real regression.
func TestRealLogE2E(t *testing.T) {
	if _, err := os.Stat(realLogPath); os.IsNotExist(err) {
		t.Skip("real audit log not present; skipping e2e test on this clone")
	}
	if os.Getenv("SKIP_SLOW_E2E") == "1" {
		t.Skip("SKIP_SLOW_E2E set")
	}

	new, _, err := Build([]string{realLogPath}, time.Now(), nil, nil)
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
