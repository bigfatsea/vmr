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
