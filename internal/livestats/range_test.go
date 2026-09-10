package livestats

import (
	"sync"
	"testing"
	"time"
)

// TestSnapshot_HourlyTailParameterized pins the ?range= read-side behavior
// (contracts §1.5): the hourly tail keeps the N most recent distinct hours
// with data, daily[] stays fixed at dailyTail regardless of the tail, and
// values outside the server's vocabulary are the caller's problem — the
// aggregation honors whatever tail it is given.
func TestSnapshot_HourlyTailParameterized(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 10, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }
	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// 30 distinct observed hours; each Record crosses an hour boundary and
	// lazily rolls the previous slim (same as production hour transitions).
	for i := 0; i < 30; i++ {
		clock = clock.Add(time.Hour)
		agg.Record(Sample{TS: clock, VModel: "coding", Outcome: OutcomeOK})
	}

	cases := []struct {
		tail      int
		wantHours int
	}{
		{1, 1},
		{24, 24},
		{HourlyTailDefault, 30},
		{72, 30},
		{168, 30},
	}
	var dailyLen int
	for _, c := range cases {
		snap := agg.Snapshot(c.tail)
		if len(snap.Hourly) != c.wantHours {
			t.Errorf("tail %d: hourly rows = %d, want %d", c.tail, len(snap.Hourly), c.wantHours)
		}
		if dailyLen == 0 {
			dailyLen = len(snap.Daily)
			if dailyLen == 0 {
				t.Fatal("expected daily rows")
			}
		}
		if len(snap.Daily) != dailyLen {
			t.Errorf("tail %d: daily rows = %d, want %d (daily is tail-invariant)", c.tail, len(snap.Daily), dailyLen)
		}
	}
}

// TestCachedSnapshot_PerTailKeys pins the per-tail cache keying (contracts
// §1.5): two tails never serve each other's payloads, Record stays
// invisible inside the TTL, and concurrent mixed-tail polling is
// race-clean under -race.
func TestCachedSnapshot_PerTailKeys(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 10, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }
	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	rec := func() { agg.Record(Sample{TS: clock, VModel: "coding", Outcome: OutcomeOK}) }
	rec()
	if got := len(agg.CachedSnapshot(1).Hourly); got != 1 {
		t.Fatalf("tail 1 rows = %d, want 1", got)
	}

	// A second observed hour. Within the TTL the tail-1 entry must stay
	// stale; the tail-2 entry is a separate key and must fold fresh.
	clock = clock.Add(2 * time.Hour)
	rec()
	if got := len(agg.CachedSnapshot(1).Hourly); got != 1 {
		t.Errorf("tail 1 within TTL = %d rows, want stale 1", got)
	}
	if got := len(agg.CachedSnapshot(2).Hourly); got != 2 {
		t.Errorf("tail 2 = %d rows, want fresh 2 (per-tail key, not shared)", got)
	}

	// Past the TTL the stale entry recomputes. Tail 1 keeps only the
	// newest observed hour, so it must show exactly 1 row even after the
	// second hour was recorded.
	clock = clock.Add(snapCacheTTL + time.Millisecond)
	if got := len(agg.CachedSnapshot(1).Hourly); got != 1 {
		t.Errorf("tail 1 past TTL = %d rows, want refreshed 1 (newest hour only)", got)
	}
	if got := len(agg.CachedSnapshot(48).Hourly); got != 2 {
		t.Errorf("tail 48 past TTL = %d rows, want 2", got)
	}

	// Concurrent mixed-tail polling plus a writer: nothing may race and no
	// tail may observe another tail's cached payload.
	var wg sync.WaitGroup
	tails := []int{1, 2, 24, HourlyTailDefault}
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				tail := tails[(w+i)%len(tails)]
				got := len(agg.CachedSnapshot(tail).Hourly)
				if got < 1 || got > 2 {
					t.Errorf("tail %d: impossible hourly row count %d", tail, got)
					return
				}
				if i%5 == 0 {
					agg.Snapshot(tail)
					rec()
				}
			}
		}(w)
	}
	wg.Wait()
}
