// Ver 2026-07-26, by Sonnet 5
package fmtutil

import (
	"testing"
	"time"
)

// TestFmtBytes locks the B/KB/MB thresholds and rounding — these three
// functions had no direct test coverage before this package existed (only
// exercised indirectly through report/router output), so this is new
// coverage, not a relocation of an existing test.
func TestFmtBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1 << 20, "1.0MB"},
		{3*(1<<20) + (1 << 19), "3.5MB"},
	}
	for _, c := range cases {
		if got := FmtBytes(c.n); got != c.want {
			t.Errorf("FmtBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestFmtSeconds locks the fixed-decimal rendering and the decimals
// parameter (2 for the live router log, 3 for `vmr diagnose`).
func TestFmtSeconds(t *testing.T) {
	t.Parallel()
	d := 6*time.Second + 320*time.Millisecond
	if got, want := FmtSeconds(d, 2), "6.32s"; got != want {
		t.Errorf("FmtSeconds(%s, 2) = %q, want %q", d, got, want)
	}
	if got, want := FmtSeconds(d, 3), "6.320s"; got != want {
		t.Errorf("FmtSeconds(%s, 3) = %q, want %q", d, got, want)
	}
	if got, want := FmtSeconds(0, 2), "0.00s"; got != want {
		t.Errorf("FmtSeconds(0, 2) = %q, want %q", got, want)
	}
}

// TestFmtPercent locks the decimals parameter both internal/report's and
// internal/story's own pctStr now alias — 1 decimal for report's dense
// tables, 0 for story's narrative text — so a future edit to one of those
// aliases can't silently re-diverge them from this shared definition.
func TestFmtPercent(t *testing.T) {
	t.Parallel()
	if got, want := FmtPercent(0.4234, 1), "42.3%"; got != want {
		t.Errorf("FmtPercent(0.4234, 1) = %q, want %q", got, want)
	}
	if got, want := FmtPercent(0.4234, 0), "42%"; got != want {
		t.Errorf("FmtPercent(0.4234, 0) = %q, want %q", got, want)
	}
	if got, want := FmtPercent(0, 1), "0.0%"; got != want {
		t.Errorf("FmtPercent(0, 1) = %q, want %q", got, want)
	}
}
