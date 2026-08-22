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
// This is the raw per-Limit ratio ScoreForLimits below assigns a
// bucket-or-gate role to — call it directly only when a provider has
// exactly one Limit (P1/P2's shape) or when the role has already been
// decided elsewhere.
func ScoreForLimit(l core.Limit, used float64, now time.Time) float64 {
	start := PeriodStart(l, now)
	end := PeriodEnd(l, now)
	return Headroom(UsedFrac(used, l.Amount), TimeLeftFrac(now, start, end))
}

// BucketIndex returns the index of limits' bucket Limit — the longest
// tumbling period among them (see the design doc's §5.2 bucket-vs-gate
// rule: "周期最长的那条 tumbling Limit 是桶,其余全是闸"). This package
// carries no rolling windows yet (see core.Limit's doc comment), so every
// Limit is tumbling and this always resolves to a real index for a
// non-empty slice — the "all-rolling has no bucket" branch §5.2 also
// describes doesn't apply until rolling windows exist. len(limits)==0
// returns -1 (unreachable from ScoreForLimits, which guards it first).
func BucketIndex(limits []core.Limit) int {
	if len(limits) == 0 {
		return -1
	}
	bi := 0
	bh := nominalUnitHours(limits[0].EveryUnit, limits[0].EveryN)
	for i := 1; i < len(limits); i++ {
		h := nominalUnitHours(limits[i].EveryUnit, limits[i].EveryN)
		if h > bh {
			bi, bh = i, h
		}
	}
	return bi
}

// ScoreForLimits composes a whole provider's score across every one of its
// Limits (P3: multi-window) via the bucket-vs-gate rule: the longest-period
// Limit is scored as a bucket (its raw headroom passes through unchanged —
// underuse can push the score above 1.0, "use it or lose it"); every other
// Limit is scored as a gate, hard-capped at 1.0 (`min(1, raw)`) — underuse
// never boosts past neutral, only nearness-to-saturation (raw<1) suppresses.
// The provider's score is the min across all of them — "the tightest
// constraint decides".
//
// The gate cap is a flat ceiling, not a graduated reserve band — an earlier
// version eased a gate toward suppression starting at raw < 2× pace (a
// `GateReserve` constant), but that early-warning margin had no data behind
// it, just an arbitrary "how early should this start" choice. `min(1, raw)`
// keeps the one property that actually matters (a gate can never lift the
// score above what pure on-pace consumption would give) without the extra
// tunable.
//
// used[i] must already have base(limits[i].Metric) applied (see
// ScoreForLimit) and align index-for-index with limits — the caller (
// internal/router/quota.go's scoreForEndpoint) is the one walking each
// Limit's Scope-filtered Counters, this function only merges the results.
//
// Degenerates to plain ScoreForLimit when len(limits)==1: that one Limit is
// trivially both the longest and the only one, so it always gets bucket
// treatment (no gate cap) — byte-identical to P1/P2's single-Limit
// behavior, which is the "zero regression for existing configs" property.
func ScoreForLimits(limits []core.Limit, used []float64, now time.Time) float64 {
	bi := BucketIndex(limits)
	score := HeadroomCap
	for i, l := range limits {
		raw := ScoreForLimit(l, used[i], now)
		h := raw
		if i != bi && h > 1 {
			h = 1
		}
		if h < score {
			score = h
		}
	}
	return score
}
