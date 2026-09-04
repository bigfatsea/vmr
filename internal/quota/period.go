// Ver 2026-08-07, by Opus 5

// Package quota implements Quota-Aware Routing's accounting half: counting
// what a provider account has consumed against its configured Limit(s) and
// computing the headroom score the router reorders candidates by. See
// docs/VirtualModelRouter_Design_v4_Quota.md for the full design and its
// "现状与后续计划" section for what's actually shipped (currently: one or
// more tumbling Limits per provider, P3's bucket-vs-gate multi-window
// merge; rolling windows remain undelivered).
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
	case "min":
		return func(since time.Time, k int) time.Time {
			return since.Add(time.Duration(everyN*k) * time.Minute)
		}
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
	case "min":
		return float64(everyN) / 60
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

// PeriodBounds returns [start, end) of the tumbling period containing now
// for Limit l — one findK, so the two boundaries are always same-k
// consistent. Computed in fmtutil.DisplayZone — period boundaries are a
// human-facing concept (CLAUDE.md's timezone invariant: everything
// human-facing renders through DisplayZone), not raw request data. N4: a
// zero l.Since (never valid from config, but guard the pure function
// anyway) is anchored to DefaultSince(now, l.EveryUnit) rather than fed to
// findK, whose elapsed-since-year-1 arithmetic overflows.
func PeriodBounds(l core.Limit, now time.Time) (start, end time.Time) {
	if l.Since.IsZero() {
		l.Since = DefaultSince(now, l.EveryUnit)
	}
	since := l.Since.In(fmtutil.DisplayZone)
	now = now.In(fmtutil.DisplayZone)
	step := stepFor(l.EveryUnit, l.EveryN)
	k := findK(step, since, now, l.EveryUnit, l.EveryN)
	return step(since, k), step(since, k+1)
}

// PeriodStart returns the start of the tumbling period containing now, for
// Limit l. now at or before l.Since returns l.Since itself: the first
// period starts at the anchor even when now is earlier (a Limit staged
// ahead of its account's actual start date).
func PeriodStart(l core.Limit, now time.Time) time.Time {
	start, _ := PeriodBounds(l, now)
	return start
}

// PeriodEnd returns the (exclusive) end of the tumbling period containing
// now — always PeriodStart's next boundary, derived from the same k so the
// two can never disagree about where a period ends.
func PeriodEnd(l core.Limit, now time.Time) time.Time {
	_, end := PeriodBounds(l, now)
	return end
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
// `since` field was left empty (see config.LimitConfig.Since's doc comment):
// now, in fmtutil.DisplayZone, truncated back to a fixed calendar boundary —
// midnight for min/h/d, Monday 00:00 for w, the 1st for mo.
//
// The alignment is what keeps a bucket alive across a config reload. The
// bucket key (quota.LimitKey) deliberately omits `since`, so it is stable;
// but PeriodStart is recomputed from the anchor on every load, and
// resetIfStaleLocked zeroes a bucket whose stored period start no longer
// matches. With a raw `now` anchor, every reload moved the anchor to the
// reload instant and silently wiped the account's accumulated usage.
// Anchoring to a fixed boundary makes PeriodStart identical across all
// reloads within the same period, so the count survives — matching the
// Quota design doc's "计数跨重载存活" promise for the common (no explicit
// `since`) case too.
//
// Midnight (not top-of-hour) for min/h so the period grid stays fixed to the
// day: two reloads on the same calendar day always resolve to the same
// anchor regardless of `every` N, so any same-day hot reload is safe. The
// grid can still shift on a reload that crosses midnight, and only for an N
// that doesn't divide its unit's span of a day (min: N∤1440, h: N∤24) — e.g.
// `every: 5h` or `every: 7min`. That residual is narrow (needs a cross-day
// restart/reload AND an odd N) and self-corrects to at most one reset;
// omitting `since` is still the declaration "I don't care about exact
// phase", and a Limit that needs its window pinned writes an explicit
// `since`. Evaluated once at config-load time, not on the request hot path.
func DefaultSince(now time.Time, unit string) time.Time {
	n := now.In(fmtutil.DisplayZone)
	y, mo, d := n.Date()
	loc := n.Location()
	switch unit {
	case "min", "h", "d":
		return time.Date(y, mo, d, 0, 0, 0, 0, loc)
	case "w":
		// ISO week start: Monday. time.Weekday has Sunday==0.
		back := (int(n.Weekday()) + 6) % 7
		return time.Date(y, mo, d, 0, 0, 0, 0, loc).AddDate(0, 0, -back)
	case "mo":
		return time.Date(y, mo, 1, 0, 0, 0, 0, loc)
	default:
		return n
	}
}
