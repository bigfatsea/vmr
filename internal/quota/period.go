// Ver 2026-08-07, by Opus 5

// Package quota implements Quota-Aware Routing's accounting half: counting
// what a provider account has consumed against its configured Limit(s) and
// computing the headroom score the router reorders candidates by. See
// docs/VirtualModelRouter_Design_v4_Quota.md for the full design and its
// "现状与后续计划" section for what's actually shipped (currently: one
// Limit per provider, tumbling windows only; multi-window/rolling windows
// are P3, not yet delivered).
//
// Depends only on core + the standard library — no I/O beyond store.go's
// file persistence, and the period/score math here is pure functions, fully
// unit-testable without a Registry.
package quota

import (
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
)

// stepper advances since by k whole periods (k may be negative) — a pure
// function of (since, k), never of a running/previous result, so no
// per-step error ever compounds across many periods. h uses fixed
// wall-clock duration; d/w/mo advance calendar components via time.Date's
// own location-aware normalization, so DST transitions land on the correct
// wall-clock boundary instead of drifting by the transition's hour.
type stepper func(since time.Time, k int) time.Time

func stepFor(unit string, everyN int) stepper {
	switch unit {
	case "h":
		return func(since time.Time, k int) time.Time {
			return since.Add(time.Duration(everyN*k) * time.Hour)
		}
	case "d":
		return func(since time.Time, k int) time.Time {
			return since.AddDate(0, 0, everyN*k)
		}
	case "w":
		return func(since time.Time, k int) time.Time {
			return since.AddDate(0, 0, everyN*k*7)
		}
	case "mo":
		return func(since time.Time, k int) time.Time {
			return addMonthsClamped(since, everyN*k)
		}
	default:
		// config validation only ever admits h/d/w/mo; reachable only from a
		// hand-built core.Limit (e.g. a test) carrying something else.
		return func(since time.Time, _ int) time.Time { return since }
	}
}

// nominalUnitHours estimates one period's length for seeding findK's search
// below — it doesn't need to be exact ("mo" varies month to month; "d"/"w"
// can be off by an hour across a DST transition), findK corrects any error
// with a short walk to the real boundary.
func nominalUnitHours(unit string, everyN int) float64 {
	switch unit {
	case "h":
		return float64(everyN)
	case "d":
		return float64(everyN) * 24
	case "w":
		return float64(everyN) * 7 * 24
	case "mo":
		return float64(everyN) * 730 // ~30.4 days, average Gregorian month
	default:
		return float64(everyN) * 24
	}
}

// findK returns the k such that step(since,k) <= now < step(since,k+1) — the
// tumbling period index containing now. Seeded by dividing elapsed real time
// by a nominal period length, then walked to the exact boundary: the seed is
// never more than a period or so off (mo's own calendar variance, or a DST
// hour on d/w), so the walk below is O(1) in practice — never a scan from
// k=0, even when since is years in the past.
func findK(step stepper, since, now time.Time, unit string, everyN int) int {
	if !now.After(since) {
		return 0
	}
	nominal := nominalUnitHours(unit, everyN)
	k := int(now.Sub(since).Hours() / nominal)
	for {
		next := step(since, k+1)
		if next.After(now) {
			break
		}
		k++
	}
	for step(since, k).After(now) {
		k--
	}
	return k
}

// PeriodStart returns the start of the tumbling period containing now, for
// Limit l. Computed in fmtutil.DisplayZone — period boundaries are a
// human-facing concept (CLAUDE.md's timezone invariant: everything
// human-facing renders through DisplayZone), not raw request data. now at or
// before l.Since returns l.Since itself: the first period starts at the
// anchor even when now is earlier (a Limit staged ahead of its account's
// actual start date).
func PeriodStart(l core.Limit, now time.Time) time.Time {
	since := l.Since.In(fmtutil.DisplayZone)
	now = now.In(fmtutil.DisplayZone)
	step := stepFor(l.EveryUnit, l.EveryN)
	k := findK(step, since, now, l.EveryUnit, l.EveryN)
	return step(since, k)
}

// PeriodEnd returns the (exclusive) end of the tumbling period containing
// now — always PeriodStart's next boundary, derived from the same k so the
// two can never disagree about where a period ends.
func PeriodEnd(l core.Limit, now time.Time) time.Time {
	since := l.Since.In(fmtutil.DisplayZone)
	now = now.In(fmtutil.DisplayZone)
	step := stepFor(l.EveryUnit, l.EveryN)
	k := findK(step, since, now, l.EveryUnit, l.EveryN)
	return step(since, k+1)
}

// addMonthsClamped advances t by months calendar months, clamping the day
// component to the target month's actual length instead of letting it
// overflow into the following month the way Go's own t.AddDate(0,months,0)
// does: time.Date(2026,1,31,...).AddDate(0,1,0) normalizes to 2026-03-03,
// not "end of February". An account billed on the 31st of the month is a
// real case (see the design doc's Window Implementation section), so this
// must be handled explicitly rather than left to AddDate's overflow
// behavior — this is the single most error-prone piece of the period math,
// per the dev plan's own flag on it.
func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	ns := t.Nanosecond()
	loc := t.Location()
	totalMonths := int(m) - 1 + months
	y += totalMonths / 12
	mm := totalMonths % 12
	if mm < 0 {
		mm += 12
		y--
	}
	month := time.Month(mm + 1)
	if maxDay := daysInMonth(y, month); d > maxDay {
		d = maxDay
	}
	return time.Date(y, month, d, h, mi, s, ns, loc)
}

// daysInMonth returns the number of days in (y, m): day 0 of the following
// month is the last day of this one — a standard Go idiom for this, not an
// off-by-one guess.
func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// DefaultSince resolves the config-time default anchor for a Limit whose
// `since` field was left empty (see config.LimitConfig.Since's doc
// comment): "mo" -> the 1st of the current month, "w" -> this week's Monday,
// "h"/"d" -> today at midnight — all in fmtutil.DisplayZone. Evaluated once
// at config-load time, not on the request hot path.
func DefaultSince(unit string, now time.Time) time.Time {
	now = now.In(fmtutil.DisplayZone)
	y, m, d := now.Date()
	loc := now.Location()
	switch unit {
	case "mo":
		return time.Date(y, m, 1, 0, 0, 0, 0, loc)
	case "w":
		// ISO week: Monday is day 1. time.Weekday numbers Sunday as 0, so
		// (weekday+6)%7 gives "days since Monday" for every day including
		// Sunday itself (weekday=0 -> offset 6).
		offset := (int(now.Weekday()) + 6) % 7
		return time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, -offset)
	default: // "h", "d"
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	}
}
