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

// TestBucketIndex_EqualPeriods_SharedBeatsPerModel pins the §2.90 tie-break's
// first rule: two Limits with EQUAL nominal periods resolve by class, not by
// YAML written order — the shared pool is the bucket in BOTH orders, because
// only as the bucket does the shared pool's drain produce the smooth
// declining-score signal reordering needs (as a gate it stays silent until
// it blows).
func TestBucketIndex_EqualPeriods_SharedBeatsPerModel(t *testing.T) {
	shared := core.Limit{Metric: core.MetricTokens, EveryN: 1, EveryUnit: "mo", EveryText: "1mo", Amount: 90_000_000}
	perModel := core.Limit{Metric: core.MetricTokens, EveryN: 1, EveryUnit: "mo", EveryText: "1mo", Amount: 5_000_000, Models: []string{"claude-x"}}
	if got := BucketIndex([]core.Limit{shared, perModel}); got != 0 {
		t.Errorf("BucketIndex([shared, perModel]) = %v, want 0 (shared pool is the bucket)", got)
	}
	if got := BucketIndex([]core.Limit{perModel, shared}); got != 1 {
		t.Errorf("BucketIndex([perModel, shared]) = %v, want 1 (shared pool is the bucket regardless of written order)", got)
	}
}

// TestBucketIndex_EqualPeriodsSameClass_LargerAmountWins pins the §2.90
// tie-break's second rule: two same-class equal-period Limits resolve by
// Amount — the tighter constraint is the better fuse (it trips first, all a
// gate is for), the looser pool the better capacity gauge.
func TestBucketIndex_EqualPeriodsSameClass_LargerAmountWins(t *testing.T) {
	small := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "w", EveryText: "1w", Amount: 100}
	large := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "w", EveryText: "1w", Amount: 90_000}
	if got := BucketIndex([]core.Limit{small, large}); got != 1 {
		t.Errorf("BucketIndex([small, large]) = %v, want 1 (larger Amount is the bucket)", got)
	}
	if got := BucketIndex([]core.Limit{large, small}); got != 0 {
		t.Errorf("BucketIndex([large, small]) = %v, want 0 (larger Amount is the bucket regardless of written order)", got)
	}
}

// TestBucketIndex_FullTie_KeepsConfigOrder pins the tie-break's documented
// final fallback: identical class AND Amount keeps the earlier-configured
// Limit — config order is only ever consulted when nothing else
// distinguishes the candidates.
func TestBucketIndex_FullTie_KeepsConfigOrder(t *testing.T) {
	a := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo", EveryText: "1mo", Amount: 1000}
	b := a
	if got := BucketIndex([]core.Limit{a, b}); got != 0 {
		t.Errorf("BucketIndex([a, a]) = %v, want 0 (full tie keeps config order)", got)
	}
}

// TestScoreForLimits_TypeA is the design doc's §5.2 worked example, pinned
// as a numeric assertion: a 5h gate at 5500/6000 used with 1h left is LIVE
// (used < amount), and a live gate doesn't touch the score at all — the
// monthly bucket alone decides, and it's exactly on pace (raw = 1.0). The
// gate's own raw (0.417, near saturation) is deliberately NOT consumed: it
// reflects the safety margin's tightness, not a capacity signal. Contrast
// the blown variant in TestScoreForLimits_BlownGateZeroesScore.
func TestScoreForLimits_TypeA(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Since: now.Add(-4 * time.Hour), Amount: 6000} // 4h of 5h elapsed -> 1h (20%) left
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Amount: 1000} // April has 30 days; now is exactly the midpoint
	limits := []core.Limit{gate, bucket}
	used := []float64{5500, 500} // gate: alive (5500 < 6000); bucket: exactly on pace (50% used, 50% elapsed)

	got := ScoreForLimits(limits, used, now)
	if !almostEqual(got, 1.0, 1e-9) {
		t.Fatalf("ScoreForLimits (type A) = %v, want exactly 1.0 (design doc §5.2's worked example: live gate doesn't participate, the bucket alone decides)", got)
	}
}

// TestScoreForLimits_GateNeverBoosts pins the one invariant every gate
// merge has preserved across all three generations (GateReserve band,
// min(1, raw) cap, today's binary fuse): a gate can never push the
// provider's score above what the bucket alone would give. Here the gate
// is barely touched (raw = 1.2) while the bucket is exactly on pace
// (raw = 1.0) — under the binary fuse a live gate isn't consulted at all,
// so the merged score is the bucket's 1.0 exactly.
func TestScoreForLimits_GateNeverBoosts(t *testing.T) {
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Amount: 1000} // computed used/now below give raw=1.2
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo", Amount: 1000}
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	gate.Since = now.Add(-1 * time.Hour)                       // 1h of 5h elapsed -> 80% time left
	bucket.Since = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) // half the month elapsed

	limits := []core.Limit{gate, bucket}
	used := []float64{40, 500} // gate: 40/1000 used (raw = 0.96/0.8 = 1.2), alive; bucket: on pace (raw=1.0)

	got := ScoreForLimits(limits, used, now)
	if !almostEqual(got, 1.0, 1e-9) {
		t.Fatalf("ScoreForLimits = %v, want exactly 1.0 — a live gate must never boost the score above the bucket's own headroom", got)
	}
}

// TestScoreForLimits_EmptySetIsNeutral pins 备注B: no quota configured is
// neither exhausted (0.0 = the penalty score) nor a maximally-underused
// bucket (HeadroomCap = 5.0, what the pre-guard code returned while
// BucketIndex's comment claimed a guard existed) — it's the neutral 1.0.
func TestScoreForLimits_EmptySetIsNeutral(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	if got := ScoreForLimits(nil, nil, now); got != 1.0 {
		t.Errorf("ScoreForLimits(nil, nil) = %v, want 1.0 (neutral)", got)
	}
	if got := ScoreForLimits([]core.Limit{}, nil, now); got != 1.0 {
		t.Errorf("ScoreForLimits(empty, nil) = %v, want 1.0 (neutral)", got)
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

// TestScoreForLimits_BucketBoostWithIdleGate is the positive half of the
// KNOWN_ISSUES 2.88 adjudication (N3, closed 2026-09-04): an underused
// monthly bucket alone scores ~2.0 (use-it-or-lose-it boost), and adding
// one perfectly idle short gate must NOT collapse it to 1.0 — a live gate
// carries no routing information, so the bucket alone decides. The old
// min(1, raw) cap pinned this at exactly 1.0, which is what killed the
// boost for every gated account.
func TestScoreForLimits_BucketBoostWithIdleGate(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Amount: 1000} // April midpoint: 50% elapsed
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Since: now.Add(-1 * time.Hour), Amount: 1000} // 1h of 5h elapsed -> 80% time left

	bucketAlone := ScoreForLimits([]core.Limit{bucket}, []float64{0}, now)
	if !almostEqual(bucketAlone, 2.0, 0.02) {
		t.Fatalf("bucket alone (0 used, 50%% elapsed) = %v, want ~2.0 (underuse boost)", bucketAlone)
	}

	withIdleGate := ScoreForLimits([]core.Limit{gate, bucket}, []float64{0, 0}, now)
	if !almostEqual(withIdleGate, 2.0, 0.02) {
		t.Fatalf("bucket + idle gate = %v, want ~2.0 (2.88 adjudicated: a live gate doesn't participate, the boost survives)", withIdleGate)
	}
}

// TestScoreForLimits_BlownGateZeroesScore pins the negative half of the
// 2.88 adjudication: a gate's one guaranteed bit is blown-or-not, and blown
// must veto everything — the local bound tripped before the vendor's real
// rate limit could, so the provider stands down (score 0, sinks below every
// live alternative) until the short window resets, regardless of how rich
// its bucket is.
func TestScoreForLimits_BlownGateZeroesScore(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Amount: 1000}
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Since: now.Add(-1 * time.Hour), Amount: 1000} // fully consumed

	got := ScoreForLimits([]core.Limit{gate, bucket}, []float64{1000, 0}, now)
	if got != 0 {
		t.Fatalf("bucket (rich) + blown gate = %v, want exactly 0 (2.88 adjudicated: a blown gate vetoes the whole provider)", got)
	}
}

// TestScoreForLimits_NearBlownGateDoesNotThrottle pins that the gate's
// graded raw is genuinely unconsumed all the way down: a gate at 99.9%
// used still doesn't touch the score — there is no progressive throttle
// band, because the margin's tightness is a calibration artifact, not a
// capacity signal. The cutoff is the blow itself (see
// TestScoreForLimits_BlownGateZeroesScore).
func TestScoreForLimits_NearBlownGateDoesNotThrottle(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	bucket := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Amount: 1000}
	gate := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h",
		Since: now.Add(-1 * time.Hour), Amount: 1000} // 999/1000 = 99.9% used, still alive

	got := ScoreForLimits([]core.Limit{gate, bucket}, []float64{999, 0}, now)
	if !almostEqual(got, 2.0, 0.02) {
		t.Fatalf("bucket + 99.9%%-used gate = %v, want ~2.0 (2.88 adjudicated: no graded throttle — only the blow zeroes the score)", got)
	}
}
