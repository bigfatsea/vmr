// Ver 2026-08-13, by Opus 5

package quota

import (
	"os"
	"testing"
	"time"

	"vmr/internal/core"
)

func TestBaseAmount_Requests(t *testing.T) {
	l := core.Limit{Metric: core.MetricRequests}
	got := BaseAmount(l, Counters{Requests: 7, Fresh: 999})
	if got != 7 {
		t.Fatalf("BaseAmount(requests) = %v, want 7", got)
	}
}

func TestBaseAmount_Tokens(t *testing.T) {
	l := core.Limit{
		Metric: core.MetricTokens,
		TokenWeights: core.TokenWeights{
			InFresh: 1.0, CacheRead: 0.1, CacheWrite: 1.25, Out: 4.0,
		},
	}
	c := Counters{Fresh: 100, CacheRead: 100, CacheWrite: 100, Out: 100}
	got := BaseAmount(l, c)
	want := 100*1.0 + 100*0.1 + 100*1.25 + 100*4.0
	if got != want {
		t.Fatalf("BaseAmount(tokens) = %v, want %v", got, want)
	}
}

func TestBaseAmount_Cost(t *testing.T) {
	l := core.Limit{Metric: core.MetricCost}
	got := BaseAmount(l, Counters{Cost: 12.34, Fresh: 999})
	if got != 12.34 {
		t.Fatalf("BaseAmount(cost) = %v, want 12.34", got)
	}
}

func TestModelMultiplier_ExactWildcardDefault(t *testing.T) {
	l := core.Limit{ModelMultipliers: map[string]float64{
		"glm-5.2": 4.5,
		"*":       2.0,
	}}
	if got := modelMultiplier(l, "glm-5.2"); got != 4.5 {
		t.Fatalf("exact match = %v, want 4.5", got)
	}
	if got := modelMultiplier(l, "other-model"); got != 2.0 {
		t.Fatalf("wildcard fallback = %v, want 2.0", got)
	}
	if got := modelMultiplier(core.Limit{}, "other-model"); got != 1.0 {
		t.Fatalf("no model_multipliers configured = %v, want 1.0", got)
	}
}

func TestApplyModelMultiplier_ExactMultiplyNoRounding(t *testing.T) {
	l := core.Limit{ModelMultipliers: map[string]float64{"m": 1.5}}
	d, est := ApplyModelMultiplier(l, "m", Counters{Fresh: 3, Out: 1, Requests: 1}, 5)
	// Exact multiplication, no rounding in either direction — see
	// quota.Counters' doc comment for why: 3*1.5=4.5, 1*1.5=1.5, 5*1.5=7.5.
	if d.Fresh != 4.5 || d.Out != 1.5 || d.Requests != 1.5 || est != 7.5 {
		t.Fatalf("ApplyModelMultiplier scaling = %+v est=%v, want Fresh=4.5 Out=1.5 Requests=1.5 est=7.5", d, est)
	}
}

func TestApplyModelMultiplier_NoScalingIsIdentity(t *testing.T) {
	l := core.Limit{}
	in := Counters{Fresh: 3, Out: 1, Requests: 1}
	d, est := ApplyModelMultiplier(l, "m", in, 5)
	if d != in || est != 5 {
		t.Fatalf("ApplyModelMultiplier with no multiplier configured = %+v est=%v, want identity", d, est)
	}
}

func TestEstimatedPct_RequestsAlwaysZero(t *testing.T) {
	got := EstimatedPct(core.MetricRequests, Counters{Requests: 100}, 100, 0)
	if got != 0 {
		t.Fatalf("EstimatedPct(requests) = %v, want 0 (always exact)", got)
	}
}

func TestEstimatedPct_Tokens_UsesRawUnweightedTotal(t *testing.T) {
	c := Counters{Fresh: 100, CacheRead: 100, CacheWrite: 100, Out: 100} // raw total 400
	got := EstimatedPct(core.MetricTokens, c, 40, 0)
	if got != 10 {
		t.Fatalf("EstimatedPct(tokens) = %v, want 10 (40/400*100)", got)
	}
}

func TestEstimatedPct_Cost_UsesCostRatio(t *testing.T) {
	c := Counters{Cost: 20.0}
	got := EstimatedPct(core.MetricCost, c, 0, 5.0)
	if got != 25 {
		t.Fatalf("EstimatedPct(cost) = %v, want 25 (5/20*100)", got)
	}
}

func TestEstimatedPct_ZeroDenominatorIsZeroNotNaN(t *testing.T) {
	if got := EstimatedPct(core.MetricTokens, Counters{}, 0, 0); got != 0 {
		t.Fatalf("EstimatedPct(tokens) with zero raw total = %v, want 0", got)
	}
	if got := EstimatedPct(core.MetricCost, Counters{}, 0, 0); got != 0 {
		t.Fatalf("EstimatedPct(cost) with zero cost = %v, want 0", got)
	}
}

func TestLoadFile_MissingIsNotAnError(t *testing.T) {
	m, err := LoadFile("/nonexistent/path/vmr-quota.json")
	if err != nil {
		t.Fatalf("LoadFile on missing file: %v", err)
	}
	if m != nil {
		t.Fatalf("LoadFile on missing file = %+v, want nil map", m)
	}
}

func TestLoadFile_RoundTripsRealShape(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/vmr-quota.json"
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	r := NewRegistry(path)
	r.Charge("volcengine", "requests/1mo", ps, Counters{Requests: 12240}, 0)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	m, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b, ok := m["volcengine"]["requests/1mo"]
	if !ok {
		t.Fatal("LoadFile did not return the volcengine/requests-1mo bucket")
	}
	if b.C.Requests != 12240 {
		t.Fatalf("bucket.C.Requests = %v, want 12240", b.C.Requests)
	}
	if !b.PeriodStartTime().Equal(ps) {
		t.Fatalf("PeriodStartTime() = %v, want %v", b.PeriodStartTime(), ps)
	}
}

func TestLoadFile_CorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/vmr-quota.json"
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile on corrupt file returned nil error, want non-nil")
	}
}
