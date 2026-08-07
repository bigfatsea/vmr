// Ver 2026-08-07, by Opus 5

package quota

import (
	"time"

	"vmr/internal/core"
)

const (
	// HeadroomCap bounds Score's upper end: an account that has barely
	// touched its quota very early in its period shouldn't get an unbounded
	// multiplier just because the ratio is large — see the design doc's
	// Core Algorithm section (§5.2's clamp).
	HeadroomCap = 5.0
	// epsilon guards raw's denominator against an exact-zero time_left_frac
	// (the window's closing instant) — a floor, not a tuning knob; Score's
	// actual upper bound is HeadroomCap above, not this value (see the
	// design doc's Core Algorithm section).
	epsilon = 1e-9
)

// UsedFrac is used/amount, clamped to [0, 1]. amount<=0 returns 1
// (maximally "used") defensively — config validation already rejects
// amount<=0 before a Limit ever reaches this function, so this is a floor
// against division by zero, not a case expected to fire.
func UsedFrac(used, amount float64) float64 {
	if amount <= 0 {
		return 1
	}
	switch f := used / amount; {
	case f > 1:
		return 1
	case f < 0:
		return 0
	default:
		return f
	}
}

// TimeLeftFrac is the fraction of [start, end) still remaining at now,
// clamped to [0, 1]. A zero-or-negative window (start==end, a degenerate
// Limit) returns 0 — no time left, maximally urgent — rather than dividing
// by zero.
func TimeLeftFrac(now, start, end time.Time) float64 {
	total := end.Sub(start)
	if total <= 0 {
		return 0
	}
	left := end.Sub(now)
	switch {
	case left <= 0:
		return 0
	case left >= total:
		return 1
	default:
		return float64(left) / float64(total)
	}
}

// Headroom is the design doc's core ratio: raw = (1 - used_frac) /
// max(time_left_frac, epsilon), clamped to [0, HeadroomCap]. This is the
// whole algorithm — see the design doc's Core Algorithm section for why it
// is dimensionless (comparable across metrics) and self-correcting (no
// tuning parameter beyond the clamp).
//
// P1 has exactly one Limit per provider, and that Limit is always the
// account's bucket (see the design doc's Bucket vs Gate section) — the gate
// role and its GateReserve down-scaling only exist from P3 once a provider
// can carry more than one Limit, so this function has no "is this a bucket
// or a gate" parameter at all.
func Headroom(usedFrac, timeLeftFrac float64) float64 {
	tl := timeLeftFrac
	if tl < epsilon {
		tl = epsilon
	}
	raw := (1 - usedFrac) / tl
	switch {
	case raw < 0:
		return 0
	case raw > HeadroomCap:
		return HeadroomCap
	default:
		return raw
	}
}

// ScoreForLimit composes PeriodStart/PeriodEnd with Headroom for one Limit:
// used must already be in l.Metric's unit (the caller applies base(metric)
// — requests count or equal-weighted token sum, see the design doc's
// Metering section — before calling this; this function only knows ratios).
// P1 callers always pass a provider's single Limit; a future multi-Limit
// batch takes the min() of this across every Limit at the call site (see
// the design doc's Core Algorithm section on multi-window merging), not
// inside this function.
func ScoreForLimit(l core.Limit, used float64, now time.Time) float64 {
	start := PeriodStart(l, now)
	end := PeriodEnd(l, now)
	return Headroom(UsedFrac(used, l.Amount), TimeLeftFrac(now, start, end))
}
