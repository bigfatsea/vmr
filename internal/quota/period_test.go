// Ver 2026-08-07, by Opus 5

package quota

import (
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata for %s not available: %v", name, err)
	}
	return loc
}

func withDisplayZone(t *testing.T, loc *time.Location) {
	t.Helper()
	old := fmtutil.DisplayZone
	fmtutil.DisplayZone = loc
	t.Cleanup(func() { fmtutil.DisplayZone = old })
}

func TestAddMonthsClamped_MonthEndTruncation(t *testing.T) {
	loc := time.UTC
	jan31 := time.Date(2026, 1, 31, 9, 0, 0, 0, loc)
	got := addMonthsClamped(jan31, 1)
	want := time.Date(2026, 2, 28, 9, 0, 0, 0, loc) // 2026 is not a leap year
	if !got.Equal(want) {
		t.Fatalf("Jan 31 + 1mo = %v, want %v (clamped to Feb end, not overflowed to Mar)", got, want)
	}
}

func TestAddMonthsClamped_LeapYear(t *testing.T) {
	loc := time.UTC
	jan31 := time.Date(2028, 1, 31, 0, 0, 0, 0, loc) // 2028 is a leap year
	got := addMonthsClamped(jan31, 1)
	want := time.Date(2028, 2, 29, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("Jan 31 2028 + 1mo = %v, want %v (leap Feb 29)", got, want)
	}
}

func TestAddMonthsClamped_CrossYear(t *testing.T) {
	loc := time.UTC
	dec31 := time.Date(2026, 12, 31, 0, 0, 0, 0, loc)
	got := addMonthsClamped(dec31, 1)
	want := time.Date(2027, 1, 31, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("Dec 31 + 1mo = %v, want %v", got, want)
	}
}

func TestAddMonthsClamped_GoAddDateWouldOverflow(t *testing.T) {
	// Sanity check documenting exactly the bug this function exists to
	// avoid: Go's own AddDate normalizes Jan 31 + 1 month into Mar 3.
	loc := time.UTC
	jan31 := time.Date(2026, 1, 31, 0, 0, 0, 0, loc)
	naive := jan31.AddDate(0, 1, 0)
	want := time.Date(2026, 3, 3, 0, 0, 0, 0, loc)
	if !naive.Equal(want) {
		t.Fatalf("expected Go's AddDate to overflow to %v, got %v — if this changed, addMonthsClamped's rationale needs revisiting", want, naive)
	}
}

func TestPeriodStartEnd_MonthlyInvariant(t *testing.T) {
	loc := time.UTC
	l := core.Limit{Metric: core.MetricTokens, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 1, 31, 0, 0, 0, 0, loc), Amount: 100}
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, loc)
	start := PeriodStart(l, now)
	end := PeriodEnd(l, now)
	// Anchor day 31: period 0 is [Jan31, Feb28) (Feb has no 31st, clamped);
	// period 1 is [Feb28, Mar31) (March has a 31st, so it lands exactly on
	// the anchor day again) — now=Mar15 falls inside period 1.
	wantStart := time.Date(2026, 2, 28, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) {
		t.Fatalf("PeriodStart = %v, want %v", start, wantStart)
	}
	if !(start.Before(now) || start.Equal(now)) || !now.Before(end) {
		t.Fatalf("PeriodStart<=now<PeriodEnd violated: start=%v now=%v end=%v", start, now, end)
	}
	wantEnd := time.Date(2026, 3, 31, 0, 0, 0, 0, loc)
	if !end.Equal(wantEnd) {
		t.Fatalf("PeriodEnd = %v, want %v", end, wantEnd)
	}
}

func TestPeriodStartEnd_CrossYearMonthly(t *testing.T) {
	loc := time.UTC
	l := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "mo",
		Since: time.Date(2026, 1, 1, 0, 0, 0, 0, loc), Amount: 100}
	now := time.Date(2027, 1, 5, 0, 0, 0, 0, loc)
	start := PeriodStart(l, now)
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
	if !start.Equal(want) {
		t.Fatalf("cross-year PeriodStart = %v, want %v", start, want)
	}
}

func TestPeriodStartEnd_MultiWeekMultiDay(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name        string
		unit        string
		n           int
		wantSpanDay int
	}{
		{"2w", "w", 2, 14},
		{"3d", "d", 3, 3},
	}
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := core.Limit{Metric: core.MetricRequests, EveryN: c.n, EveryUnit: c.unit, Since: since, Amount: 100}
			now := since.AddDate(0, 0, c.wantSpanDay+1) // one period further in
			start := PeriodStart(l, now)
			end := PeriodEnd(l, now)
			gotSpan := end.Sub(start)
			wantSpan := time.Duration(c.wantSpanDay) * 24 * time.Hour
			if gotSpan != wantSpan {
				t.Fatalf("%s: span = %v, want %v (start=%v end=%v)", c.name, gotSpan, wantSpan, start, end)
			}
		})
	}
}

func TestPeriodStartEnd_DSTTransition(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	withDisplayZone(t, loc)
	// US DST spring-forward 2026-03-08. A daily window anchored before it
	// must still land on local midnight after the transition, not drift by
	// the missing hour.
	since := time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	l := core.Limit{Metric: core.MetricRequests, EveryN: 1, EveryUnit: "d", Since: since, Amount: 100}
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, loc)
	start := PeriodStart(l, now)
	want := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	if !start.Equal(want) {
		t.Fatalf("DST-crossing daily PeriodStart = %v, want %v", start, want)
	}
	end := PeriodEnd(l, now)
	wantEnd := time.Date(2026, 3, 10, 0, 0, 0, 0, loc)
	if !end.Equal(wantEnd) {
		t.Fatalf("DST-crossing daily PeriodEnd = %v, want %v", end, wantEnd)
	}
}

func TestPeriodStartEnd_HourlyFixedDuration(t *testing.T) {
	loc := time.UTC
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	l := core.Limit{Metric: core.MetricRequests, EveryN: 5, EveryUnit: "h", Since: since, Amount: 100}
	now := since.Add(11 * time.Hour) // 2 full 5h periods elapsed, into the 3rd
	start := PeriodStart(l, now)
	want := since.Add(10 * time.Hour)
	if !start.Equal(want) {
		t.Fatalf("hourly PeriodStart = %v, want %v", start, want)
	}
}

// Self-consistency sweep: for every unit, PeriodStart(now) <= now <
// PeriodEnd(now) must hold across a long span of "now" values, including
// across a DST transition.
func TestPeriodStartEnd_Invariant_Sweep(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	withDisplayZone(t, loc)
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	limits := []core.Limit{
		{EveryN: 5, EveryUnit: "h", Since: since, Amount: 1},
		{EveryN: 1, EveryUnit: "d", Since: since, Amount: 1},
		{EveryN: 1, EveryUnit: "w", Since: since, Amount: 1},
		{EveryN: 1, EveryUnit: "mo", Since: since, Amount: 1},
	}
	for _, l := range limits {
		for h := 0; h < 24*40; h += 3 { // 40 days, every 3 hours — crosses the March DST transition
			now := since.Add(time.Duration(h) * time.Hour)
			start := PeriodStart(l, now)
			end := PeriodEnd(l, now)
			if now.Before(start) || !now.Before(end) {
				t.Fatalf("unit=%s now=%v not in [start,end): start=%v end=%v", l.EveryUnit, now, start, end)
			}
		}
	}
}

// TestPeriodBounds_MatchesPeriodStartEnd pins F9: PeriodBounds is the
// single-findK source the two wrappers now delegate to, so its two returns
// must equal PeriodStart/PeriodEnd across the calendar shapes that matter
// (monthly clamping, cross-year, DST, week alignment).
func TestPeriodBounds_MatchesPeriodStartEnd(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	withDisplayZone(t, loc)
	cases := []struct {
		name  string
		limit core.Limit
		now   time.Time
	}{
		{"monthly", core.Limit{EveryUnit: "mo", EveryN: 1, Since: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)},
			time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)},
		{"cross-year", core.Limit{EveryUnit: "mo", EveryN: 1, Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
			time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)},
		{"dst", core.Limit{EveryUnit: "d", EveryN: 1, Since: time.Date(2026, 3, 1, 0, 0, 0, 0, loc)},
			time.Date(2026, 3, 9, 12, 0, 0, 0, loc)},
		// 2026-01-05 is a Monday; 2026-02-18 a Wednesday — the start must
		// land on the Monday-aligned boundary, not the anchor's raw phase.
		// Times in loc, not UTC: PeriodBounds converts into DisplayZone.
		{"weekly-monday", core.Limit{EveryUnit: "w", EveryN: 1, Since: time.Date(2026, 1, 5, 0, 0, 0, 0, loc)},
			time.Date(2026, 2, 18, 12, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := PeriodBounds(c.limit, c.now)
			wantStart, wantEnd := PeriodStart(c.limit, c.now), PeriodEnd(c.limit, c.now)
			if !start.Equal(wantStart) || !end.Equal(wantEnd) {
				t.Fatalf("PeriodBounds = (%v, %v), PeriodStart/End = (%v, %v) — the three must agree (same-k)",
					start, end, wantStart, wantEnd)
			}
			if (!start.Before(c.now) && !start.Equal(c.now)) || !c.now.Before(end) {
				t.Fatalf("now %v not within [start=%v, end=%v)", c.now, start, end)
			}
			if c.name == "weekly-monday" && start.Weekday() != time.Monday {
				t.Fatalf("weekly start = %v (%v), want a Monday", start, start.Weekday())
			}
		})
	}
}

// TestPeriodBounds_ZeroSince pins N4: a zero-value Since (never valid from
// config) must be anchored to DefaultSince rather than fed to findK, whose
// elapsed-since-year-1 arithmetic overflows — the pre-guard failure mode was
// a near-infinite boundary walk (CPU hang), not a clean answer. Also asserts
// the call returns promptly with sane bounds.
func TestPeriodBounds_ZeroSince(t *testing.T) {
	withDisplayZone(t, time.UTC)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	for _, unit := range []string{"min", "h", "d", "w", "mo"} {
		l := core.Limit{EveryUnit: unit, EveryN: 1} // Since left zero
		start, end := PeriodBounds(l, now)
		l.Since = DefaultSince(now, unit)
		wantStart, wantEnd := PeriodBounds(l, now)
		if !start.Equal(wantStart) || !end.Equal(wantEnd) {
			t.Errorf("unit %s: zero-Since bounds (%v, %v), want the DefaultSince-anchored (%v, %v)",
				unit, start, end, wantStart, wantEnd)
		}
		if now.Before(start) || !now.Before(end) {
			t.Errorf("unit %s: zero-Since bounds don't contain now: [%v, %v)", unit, start, end)
		}
	}
}

func TestDefaultSince(t *testing.T) {
	loc := time.UTC
	withDisplayZone(t, loc)
	now := time.Date(2026, 8, 7, 15, 30, 12, 500, loc) // a Friday, mid-second
	cases := []struct {
		unit string
		want time.Time
	}{
		{"min", time.Date(2026, 8, 7, 0, 0, 0, 0, loc)},
		{"h", time.Date(2026, 8, 7, 0, 0, 0, 0, loc)},
		{"d", time.Date(2026, 8, 7, 0, 0, 0, 0, loc)},
		{"w", time.Date(2026, 8, 3, 0, 0, 0, 0, loc)}, // Monday of that week
		{"mo", time.Date(2026, 8, 1, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := DefaultSince(now, c.unit); !got.Equal(c.want) {
			t.Errorf("DefaultSince(%v, %q) = %v, want %v", now, c.unit, got, c.want)
		}
	}
}

func TestDefaultSince_ConvertsToDisplayZone(t *testing.T) {
	withDisplayZone(t, time.UTC)
	other := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 7, 23, 30, 0, 0, other) // 15:30 UTC — same UTC calendar day
	got := DefaultSince(now, "h")
	want := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("DefaultSince(%v, h) = %v, want %v in DisplayZone (UTC)", now, got, want)
	}
}

// TestDefaultSince_SurvivesReload is the B2 regression: two config loads on
// the same calendar day must resolve to the same anchor for every unit and
// every N, so PeriodStart is identical and resetIfStaleLocked never fires on
// a same-day hot reload.
func TestDefaultSince_SurvivesReload(t *testing.T) {
	withDisplayZone(t, time.UTC)
	load1 := time.Date(2026, 8, 7, 2, 3, 0, 0, time.UTC)
	load2 := time.Date(2026, 8, 7, 21, 47, 30, 0, time.UTC) // same day, ~20h later, crosses many hour boundaries
	// charge and read at the same instant — the reload (anchor re-resolution)
	// is the only variable, so any PeriodStart difference is a pure phase shift
	at := time.Date(2026, 8, 7, 21, 40, 0, 0, time.UTC)
	charge, read := at, at
	for _, c := range []struct {
		unit string
		n    int
	}{{"min", 1}, {"min", 5}, {"min", 7}, {"min", 45}, {"h", 1}, {"h", 2}, {"h", 5}, {"d", 1}, {"w", 1}, {"mo", 1}} {
		l := core.Limit{EveryUnit: c.unit, EveryN: c.n}
		l.Since = DefaultSince(load1, c.unit)
		ps1 := PeriodStart(l, charge)
		l.Since = DefaultSince(load2, c.unit)
		ps2 := PeriodStart(l, read)
		if !ps1.Equal(ps2) {
			t.Errorf("every %d%s: PeriodStart moved across a same-day reload: %v -> %v", c.n, c.unit, ps1, ps2)
		}
	}
}
