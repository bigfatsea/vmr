// Ver 2026-08-05, by Sonnet 5
package fmtutil

import (
	"testing"
	"time"
)

// TestDisplayZoneDefault locks DisplayZone's zero-value behavior: it must
// start out as time.Local (the system default timezone), not UTC or some
// other implicit default — that's the whole point of the var.
func TestDisplayZoneDefault(t *testing.T) {
	if DisplayZone != time.Local {
		t.Errorf("DisplayZone = %v, want time.Local", DisplayZone)
	}
}

// TestDisplayZoneOverride confirms callers (and other tests) can swap
// DisplayZone deterministically and restore it — the override mechanism
// display/aggregation tests elsewhere rely on to prove a conversion
// actually happened rather than coincidentally matching the source offset.
// Deliberately not t.Parallel(): it mutates shared package state.
func TestDisplayZoneOverride(t *testing.T) {
	orig := DisplayZone
	defer func() { DisplayZone = orig }()

	fixed := time.FixedZone("TEST+05:00", 5*3600)
	DisplayZone = fixed
	ts := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	got := ts.In(DisplayZone).Format("15:04")
	if want := "15:00"; got != want {
		t.Errorf("ts.In(DisplayZone).Format = %q, want %q", got, want)
	}
}
