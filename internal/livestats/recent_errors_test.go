package livestats

import (
	"testing"
	"time"
)

// TestRecentErrors_NewestFirstAndAdmission pins the ring's shape (design
// §8.1 / contracts §1.4): ok samples are never collected, error and canceled
// both are, output is newest first, and fields (including error_class and a
// non-HTTP status 0) pass through verbatim.
func TestRecentErrors_NewestFirstAndAdmission(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 14, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	ok := Sample{TS: now, VModel: "coding", Outcome: OutcomeOK, Provider: "p1", Model: "m1"}
	e1 := Sample{
		TS: now.Add(time.Second), VModel: "agent", Protocol: "anthropic-messages", Stream: true,
		ClientKeyTag: "openclaw", Provider: "p1-main", KeyLabel: "main", Model: "claude-opus-4.6",
		Attempt: 2, Outcome: OutcomeError, ErrorClass: "upstream_5xx", Status: 502, DurMS: 4100,
	}
	c1 := Sample{
		TS: now.Add(2 * time.Second), VModel: "agent",
		ClientKeyTag: "openclaw", Attempt: 1, Outcome: OutcomeCanceled, ErrorClass: "canceled",
	}
	ok2 := Sample{TS: now.Add(3 * time.Second), VModel: "coding", Outcome: OutcomeOK, Provider: "p1", Model: "m1"}

	agg.Record(ok)
	agg.Record(e1)
	agg.Record(c1)
	agg.Record(ok2)

	rows := agg.Snapshot(HourlyTailDefault).RecentErrors
	if len(rows) != 2 {
		t.Fatalf("recent_errors = %d rows, want 2 (ok samples excluded)", len(rows))
	}
	if rows[0].Outcome != OutcomeCanceled || !rows[0].TS.Equal(c1.TS) {
		t.Errorf("first row must be the newest (canceled): %+v", rows[0])
	}
	if rows[1].Outcome != OutcomeError || !rows[1].TS.Equal(e1.TS) {
		t.Errorf("second row must be the older error: %+v", rows[1])
	}
	// Wire-shape spot check on the error row.
	if rows[1].ErrorClass != "upstream_5xx" || rows[1].Status != 502 || rows[1].Attempt != 2 ||
		rows[1].Provider != "p1-main" || rows[1].KeyLabel != "main" || rows[1].Model != "claude-opus-4.6" ||
		rows[1].ClientKeyTag != "openclaw" || rows[1].DurMS != 4100 || !rows[1].Stream {
		t.Errorf("error row fields drifted: %+v", rows[1])
	}
	// Canceled-before-forward: no service identity, no HTTP status.
	if rows[0].Provider != "" || rows[0].Status != 0 {
		t.Errorf("canceled row must carry empty provider/status: %+v", rows[0])
	}
}

// TestRecentErrors_CapHundred pins the capacity: the ring keeps the newest
// recentErrCap entries; older ones fall off the old end.
func TestRecentErrors_CapHundred(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 14, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	const total = recentErrCap + 20
	for i := 0; i < total; i++ {
		agg.Record(Sample{
			TS:     now.Add(time.Duration(i) * time.Second),
			VModel: "coding", Outcome: OutcomeError,
			ErrorClass: "upstream_5xx", Attempt: 1,
		})
	}

	rows := agg.Snapshot(HourlyTailDefault).RecentErrors
	if len(rows) != recentErrCap {
		t.Fatalf("recent_errors = %d rows, want cap %d", len(rows), recentErrCap)
	}
	// Newest first: the head is sample total-1, the tail is the first
	// survivor, total-recentErrCap.
	if rows[0].TS != now.Add(time.Duration(total-1)*time.Second) {
		t.Errorf("head = %v, want the newest sample", rows[0].TS)
	}
	if rows[len(rows)-1].TS != now.Add(time.Duration(total-recentErrCap)*time.Second) {
		t.Errorf("tail = %v, want the oldest survivor", rows[len(rows)-1].TS)
	}
}

// TestRecentErrors_EvictOlderThan24Hours verifies that samples older than 24 hours
// are dropped even when the total is well below 100.
func TestRecentErrors_EvictOlderThan24Hours(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// 1 error 25 hours ago, 1 error 23 hours ago, 1 error 1 hour ago
	agg.Record(Sample{
		TS:         now.Add(-25 * time.Hour),
		VModel:     "coding",
		Outcome:    OutcomeError,
		ErrorClass: "upstream_5xx",
	})
	agg.Record(Sample{
		TS:         now.Add(-23 * time.Hour),
		VModel:     "coding",
		Outcome:    OutcomeError,
		ErrorClass: "upstream_5xx",
	})
	agg.Record(Sample{
		TS:         now.Add(-1 * time.Hour),
		VModel:     "coding",
		Outcome:    OutcomeError,
		ErrorClass: "upstream_5xx",
	})

	rows := agg.Snapshot(HourlyTailDefault).RecentErrors
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the 25h-old error must be evicted)", len(rows))
	}
	if rows[0].TS != now.Add(-1*time.Hour) || rows[1].TS != now.Add(-23*time.Hour) {
		t.Errorf("unexpected rows TS: %+v", rows)
	}
}

// TestRecentErrors_TransientRingNotPersisted pins the memory-only contract:
// the ring survives across snapshots within one process, but a fresh
// aggregator on the same log_dir starts empty — nothing ever lands in the
// slim or rollup files (§8.1).
func TestRecentErrors_TransientRingNotPersisted(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 14, 0, 0, 0, time.Local)

	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	agg.Record(Sample{TS: now, VModel: "coding", Outcome: OutcomeError, ErrorClass: "network"})
	agg.Close()

	// A restart must come up with an empty ring.
	agg2, err := NewAt(dir, func() time.Time { return now.Add(time.Minute) })
	if err != nil {
		t.Fatalf("NewAt after restart: %v", err)
	}
	defer agg2.Close()
	if rows := agg2.Snapshot(HourlyTailDefault).RecentErrors; len(rows) != 0 {
		t.Errorf("recent_errors survived a restart: %d rows", len(rows))
	}
}

// TestRecentErrors_PastHourBookedToo covers the late-arrival path
// (bookPastSampleLocked): a failure whose arrival hour was already rolled
// must still reach the recent_errors ring.
func TestRecentErrors_PastHourBookedToo(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	agg.Record(Sample{TS: clock, VModel: "coding", Outcome: OutcomeError, ErrorClass: "auth", Status: 401})
	clock = clock.Add(2 * time.Hour)
	agg.Record(Sample{TS: clock.Add(-3 * time.Hour), VModel: "coding", Outcome: OutcomeError, ErrorClass: "upstream_5xx", Status: 503})

	rows := agg.Snapshot(HourlyTailDefault).RecentErrors
	if len(rows) != 2 {
		t.Fatalf("recent_errors = %d rows, want 2 (past-hour failure included)", len(rows))
	}
	if rows[0].ErrorClass != "upstream_5xx" || rows[1].ErrorClass != "auth" {
		t.Errorf("newest-first order broken across booking paths: %+v", rows)
	}
}
