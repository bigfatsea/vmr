// Ver 2026-08-07, by Opus 5

package quota

import (
	"math"
	"testing"
	"time"

	"vmr/internal/core"
)

func almostEqual(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestHeadroom_LinearConsumption(t *testing.T) {
	if got := Headroom(0.5, 0.5); !almostEqual(got, 1.0, 1e-9) {
		t.Errorf("on-pace consumption: raw = %v, want 1.0", got)
	}
}

func TestHeadroom_ClampsToZero(t *testing.T) {
	if got := Headroom(1.0, 0.5); got != 0 {
		t.Errorf("fully used: headroom = %v, want 0", got)
	}
}

func TestHeadroom_ClampsToHeadroomCap(t *testing.T) {
	// Barely used, almost no time left: raw would be enormous without the cap.
	if got := Headroom(0.01, 0.001); got != HeadroomCap {
		t.Errorf("near-zero time left: headroom = %v, want cap %v", got, HeadroomCap)
	}
}

func TestHeadroom_EpsilonGuardsDivByZero(t *testing.T) {
	got := Headroom(0.5, 0) // window's exact closing instant
	if math.IsInf(got, 0) || math.IsNaN(got) {
		t.Fatalf("Headroom(0.5, 0) = %v, want a finite clamped value", got)
	}
	if got != HeadroomCap {
		t.Errorf("Headroom(0.5, 0) = %v, want %v (clamped)", got, HeadroomCap)
	}
}

// TestHeadroom_MisalignedResetDays reproduces the design doc's §1.1 worked
// example — the whole reason headroom (ratio of remaining shares) beats the
// naive "remaining/total" signal: three monthly plans whose reset days
// don't line up. A (50% through its cycle) and B (freshly reset, 3%
// through) both track their own progress closely (raw ~= 1.0); C is 87%
// through its cycle — only a few days from expiring — but has only used
// 60% of its quota, so it MUST win priority over A and B despite A/B having
// more total quota remaining in absolute terms. This is the scenario that
// makes remaining/total wrong and headroom right (see the design doc's
// Problem Definition section): under remaining/total, B — which has the
// most quota left — would wrongly win, while C — the one about to forfeit
// unused quota — would wrongly lose.
func TestHeadroom_MisalignedResetDays(t *testing.T) {
	a := Headroom(0.50, 0.50)   // 50% used, 50% time left: on pace
	b := Headroom(0.03, 0.97)   // just reset: 3% used, 97% time left
	c := Headroom(0.60, 0.1333) // 60% used, only 13.3% time left (design doc's 0.4/0.133 ~= 3.0 worked example)

	if !almostEqual(a, 1.0, 0.05) {
		t.Errorf("A (on-pace) headroom = %v, want ~1.0", a)
	}
	if !almostEqual(b, 1.0, 0.1) {
		t.Errorf("B (just reset) headroom = %v, want ~1.0", b)
	}
	if !almostEqual(c, 3.0, 0.05) {
		t.Errorf("C (about to expire, underused) headroom = %v, want ~3.0 (matches the design doc's 0.4/0.133 worked example)", c)
	}
	if !(c > a && c > b) {
		t.Fatalf("C must win priority over both A and B: a=%v b=%v c=%v", a, b, c)
	}
}

func TestUsedFrac_ClampsAndGuards(t *testing.T) {
	if got := UsedFrac(150, 100); got != 1 {
		t.Errorf("over-consumption: UsedFrac = %v, want 1 (clamped)", got)
	}
	if got := UsedFrac(-5, 100); got != 0 {
		t.Errorf("negative used: UsedFrac = %v, want 0", got)
	}
	if got := UsedFrac(10, 0); got != 1 {
		t.Errorf("amount<=0: UsedFrac = %v, want 1 (defensive)", got)
	}
}

func TestTimeLeftFrac_ClampsAndDegenerateWindow(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	mid := start.Add(15 * 24 * time.Hour)
	if got := TimeLeftFrac(mid, start, end); !almostEqual(got, 0.5, 0.02) {
		t.Errorf("mid-window TimeLeftFrac = %v, want ~0.5", got)
	}
	if got := TimeLeftFrac(end.Add(time.Hour), start, end); got != 0 {
		t.Errorf("past-end TimeLeftFrac = %v, want 0", got)
	}
	if got := TimeLeftFrac(start, start, start); got != 0 {
		t.Errorf("degenerate zero-length window TimeLeftFrac = %v, want 0", got)
	}
}

func TestScoreForLimit_EarlyUnderConsumption(t *testing.T) {
	l := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 1000}
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	got := ScoreForLimit(l, 0, now) // nothing used yet, ~50% through the month
	if got <= 1.0 {
		t.Errorf("under-consumed early account should score above 1.0, got %v", got)
	}
}

// TestBucketIndex_LongestTumblingWins pins §5.2's zero-config bucket rule:
// among a provider's Limits, the one with the longest period is the bucket
// — everything else is a gate.
func TestBucketIndex_LongestTumblingWins(t *testing.T) {
	limits := []core.Limit{
		{EveryN: 5, EveryUnit: "h"},  // gate
		{EveryN: 1, EveryUnit: "w"},  // gate
		{EveryN: 1, EveryUnit: "mo"}, // bucket — longest
	}
	if got := BucketIndex(limits); got != 2 {
		t.Errorf("BucketIndex = %v, want 2 (the 1mo Limit)", got)
	}
}

// TestScoreForLimits_TypeA is the design doc's §5.2 worked example, pinned
// as a numeric assertion: a 5h gate at 5500/6000 used with 1h left has
// raw = 0.417 — under the flat gate cap (`min(1, raw)`) that's also its
// headroom directly (already < 1, so the cap never engages) — alongside a
// monthly bucket exactly on pace (raw = headroom = 1.0). The provider's
// score is the min of the two: 0.417 — the gate's near-saturation
// suppresses the score even though the bucket itself is perfectly healthy.
func TestScoreForLimits_TypeA(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Since: now.Add(-4 * time.Hour), Amount: 6000} // 4h of 5h elapsed -> 1h (20%) left
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Amount: 1000} // April has 30 days; now is exactly the midpoint
	limits := []core.Limit{gate, bucket}
	used := []float64{5500, 500} // gate: 5500/6000 used; bucket: exactly on pace (50% used, 50% elapsed)

	got := ScoreForLimits(limits, used, now)
	if !almostEqual(got, 0.417, 0.01) {
		t.Fatalf("ScoreForLimits (type A) = %v, want ~0.417 (design doc §5.2's worked example, gate cap flattened to min(1,raw))", got)
	}
}

// TestScoreForLimits_GateNeverBoosts pins the invariant a flat min(raw)
// simplification would violate (see the design doc's §14.3 P3 open
// question 1): an underused GATE must never push the provider's score
// above what the bucket alone would give. Here the gate is barely touched
// (raw = 3.0, which would win a naive min() if gates weren't capped), but
// the bucket is exactly on pace (raw = 1.0) — the merged score must stay
// at 1.0, not jump to whatever the gate's own raw signal is.
func TestScoreForLimits_GateNeverBoosts(t *testing.T) {
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Amount: 1000} // computed used/now below give raw=3.0 (HeadroomCap territory, but not clamped)
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo", Amount: 1000}
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	gate.Since = now.Add(-1 * time.Hour)                       // 1h of 5h elapsed -> 80% time left
	bucket.Since = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) // half the month elapsed

	limits := []core.Limit{gate, bucket}
	used := []float64{40, 500} // gate: 40/1000 used (raw = 0.96/0.8 = 1.2, still under HeadroomCap); bucket: on pace (raw=1.0)

	got := ScoreForLimits(limits, used, now)
	if got > 1.0+1e-9 {
		t.Fatalf("ScoreForLimits = %v, want <= 1.0 — an underused gate must never boost the score above the bucket's own headroom", got)
	}
}

// TestScoreForLimits_DegeneratesToScoreForLimit pins zero regression for
// existing single-Limit configs (P1/P2's only shape): with one Limit, it is
// trivially both the longest and the only one, so it always gets bucket
// treatment (no gate cap) — byte-identical to calling ScoreForLimit
// directly.
func TestScoreForLimits_DegeneratesToScoreForLimit(t *testing.T) {
	l := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 1000}
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	want := ScoreForLimit(l, 0, now)
	got := ScoreForLimits([]core.Limit{l}, []float64{0}, now)
	if got != want {
		t.Fatalf("ScoreForLimits (single Limit) = %v, want %v (== ScoreForLimit)", got, want)
	}
}
