package livestats

import (
	"math"
	"testing"
	"time"
)

func TestSnapshot_HourlyDailyAndDimensions(t *testing.T) {
	dir := t.TempDir()
	// Two samples on day 1 (hour 10 and hour 11), one sample on day 2
	day1H10 := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	day1H11 := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	day2H08 := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)

	agg, err := NewAt(dir, func() time.Time { return day2H08 })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// Sample 1: day 1 hour 10, client_tag "user-A", key_label "key-X"
	s1 := Sample{
		TS:           day1H10.Add(5 * time.Minute),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       true,
		Outcome:      OutcomeOK,
		ClientKeyTag: "user-A",
		Provider:     "p1",
		Model:        "gpt-4o",
		KeyLabel:     "key-X",
		DurMS:        2000,
		TTFTMS:       500,
		Tokens:       TokenCounts{In: 100, Out: 60},
	}
	agg.Record(s1)

	// Sample 2: day 1 hour 11, client_tag "user-A", key_label "key-Y"
	s2 := Sample{
		TS:           day1H11.Add(10 * time.Minute),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       true,
		Outcome:      OutcomeOK,
		ClientKeyTag: "user-A",
		Provider:     "p1",
		Model:        "gpt-4o",
		KeyLabel:     "key-Y",
		DurMS:        1500,
		TTFTMS:       300,
		Tokens:       TokenCounts{In: 200, Out: 40},
	}
	agg.Record(s2)

	// Sample 3: day 2 hour 8, client_tag "user-B", key_label "key-X"
	s3 := Sample{
		TS:           day2H08.Add(2 * time.Minute),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       false,
		Outcome:      OutcomeError,
		ClientKeyTag: "user-B",
		Provider:     "p1",
		Model:        "gpt-4o",
		KeyLabel:     "key-X",
		DurMS:        1000,
		TTFTMS:       200,
		Tokens:       TokenCounts{In: 50, Out: 0},
	}
	agg.Record(s3)

	snap := agg.Snapshot()

	// 1. Hourly rows: must have 3 distinct entries across the 3 hours
	if len(snap.Hourly) != 3 {
		t.Errorf("expected 3 hourly rows, got %d", len(snap.Hourly))
	}

	// 2. Daily rows: day 1 has 2 distinct dims groups (key-X vs key-Y), day 2 has 1
	if len(snap.Daily) != 3 {
		t.Errorf("expected 3 daily rows, got %d", len(snap.Daily))
	}

	// 3. by_client_key_tag: 2 groups ("user-A" with 2 OKs, "user-B" with 1 Error)
	if len(snap.ByClientKeyTag) != 2 {
		t.Fatalf("expected 2 client key tag groups, got %d", len(snap.ByClientKeyTag))
	}
	tagA, tagB := snap.ByClientKeyTag[0], snap.ByClientKeyTag[1]
	if tagA.Value != "user-A" || tagA.OK != 2 || tagA.Tokens.Out != 100 {
		t.Errorf("user-A profile mismatch: %+v", tagA)
	}
	if tagB.Value != "user-B" || tagB.Error != 1 {
		t.Errorf("user-B profile mismatch: %+v", tagB)
	}

	// 4. by_key_label: 2 groups ("key-X" with 1 OK + 1 Error, "key-Y" with 1 OK)
	if len(snap.ByKeyLabel) != 2 {
		t.Fatalf("expected 2 key label groups, got %d", len(snap.ByKeyLabel))
	}
	lblX, lblY := snap.ByKeyLabel[0], snap.ByKeyLabel[1]
	if lblX.Value != "key-X" || lblX.OK != 1 || lblX.Error != 1 || lblX.Count != 2 {
		t.Errorf("key-X profile mismatch: %+v", lblX)
	}
	if lblY.Value != "key-Y" || lblY.OK != 1 || lblY.Count != 1 {
		t.Errorf("key-Y profile mismatch: %+v", lblY)
	}
}

func TestSnapshot_StreamVsNonStreamProviderRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// Add 10 streaming samples:
	// tokensOut=150, dur=4000ms, ttft=1000ms -> tps = 150 / 3.0s = 50.0
	for i := 0; i < 10; i++ {
		agg.Record(Sample{
			TS:       now.Add(time.Duration(i) * time.Second),
			VModel:   "coding",
			Stream:   true,
			Outcome:  OutcomeOK,
			Provider: "p1",
			Model:    "m1",
			DurMS:    4000,
			TTFTMS:   1000,
			Tokens:   TokenCounts{Out: 150},
		})
	}

	// Add 10 non-streaming samples:
	// tokensOut=100, dur=5000ms, ttft=500ms -> non-stream tps = 100 / 5.0s = 20.0
	for i := 0; i < 10; i++ {
		agg.Record(Sample{
			TS:       now.Add(time.Duration(20+i) * time.Second),
			VModel:   "coding",
			Stream:   false,
			Outcome:  OutcomeOK,
			Provider: "p1",
			Model:    "m1",
			DurMS:    5000,
			TTFTMS:   500,
			Tokens:   TokenCounts{Out: 100},
		})
	}

	snap := agg.Snapshot()
	// Must have two separate ProviderRow entries (stream=false and stream=true)
	if len(snap.ByProviderModel) != 2 {
		t.Fatalf("expected 2 provider rows (stream vs non-stream), got %d", len(snap.ByProviderModel))
	}

	var rowNonStream, rowStream ProviderRow
	for _, r := range snap.ByProviderModel {
		if r.Stream {
			rowStream = r
		} else {
			rowNonStream = r
		}
	}

	// Check streaming row percentiles
	if rowStream.Last10 == nil {
		t.Fatalf("streaming Last10 nil")
	}
	if math.Abs(rowStream.Last10.TPSP50-50.0) > 1e-4 {
		t.Errorf("expected stream TPS p50=50.0, got %f", rowStream.Last10.TPSP50)
	}

	// Check non-streaming row percentiles
	if rowNonStream.Last10 == nil {
		t.Fatalf("non-streaming Last10 nil")
	}
	if math.Abs(rowNonStream.Last10.TPSP50-20.0) > 1e-4 {
		t.Errorf("expected non-stream TPS p50=20.0, got %f", rowNonStream.Last10.TPSP50)
	}
}

// TestSnapshot_DailyTailBoundsHistory: daily[] keeps only the most recent
// dailyTail distinct local-calendar-days, so a long-lived deployment's rollup
// (never auto-deleted) does not make /stats grow without bound.
func TestSnapshot_DailyTailBoundsHistory(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	nDays := dailyTail + 15
	clock := base.AddDate(0, 0, nDays).Add(12 * time.Hour)
	agg, err := NewAt(dir, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	for d := 0; d < nDays; d++ {
		agg.Record(Sample{
			TS:     base.AddDate(0, 0, d).Add(time.Hour),
			VModel: "coding", Outcome: OutcomeOK,
			Provider: "p1", Model: "m1", Tokens: TokenCounts{In: 1, Out: 1},
		})
	}

	snap := agg.Snapshot()
	if len(snap.Daily) != dailyTail {
		t.Fatalf("daily rows = %d, want dailyTail=%d", len(snap.Daily), dailyTail)
	}
	// Rows are day-sorted ascending; the oldest kept day is nDays-dailyTail.
	wantOldest := base.AddDate(0, 0, nDays-dailyTail)
	if got := snap.Daily[0].Hour; !sameLocalDay(got, wantOldest) {
		t.Errorf("oldest daily row = %s, want %s", got.Format("2006-01-02"), wantOldest.Format("2006-01-02"))
	}
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}

// TestSnapshot_DailyBucketsUseLocalCalendarDay pins foldDay to the operator's
// wall clock: a request in the early hours of a local day sits on the previous
// UTC day for any operator east of UTC, but must still fold into that local
// day's bucket, stamped at local midnight — not UTC midnight. A fixed-offset
// %86400 truncation (the earlier implementation) failed both checks in every
// non-UTC zone.
func TestSnapshot_DailyBucketsUseLocalCalendarDay(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 15, 1, 0, 0, 0, time.Local) // 01:00 local
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	agg.Record(Sample{
		TS: now.Add(5 * time.Minute), VModel: "coding", Outcome: OutcomeOK,
		Provider: "p1", Model: "m1", Tokens: TokenCounts{In: 1, Out: 1},
	})

	snap := agg.Snapshot()
	if len(snap.Daily) != 1 {
		t.Fatalf("expected 1 daily row, got %d", len(snap.Daily))
	}
	got := snap.Daily[0].Hour.In(time.Local)
	wy, wm, wd := now.Date()
	if gy, gm, gd := got.Date(); gy != wy || gm != wm || gd != wd {
		t.Errorf("daily bucket day = %04d-%02d-%02d, want local day %04d-%02d-%02d", gy, gm, gd, wy, wm, wd)
	}
	if h, m, s := got.Clock(); h != 0 || m != 0 || s != 0 {
		t.Errorf("daily bucket local clock = %02d:%02d:%02d, want 00:00:00 (local midnight, not UTC)", h, m, s)
	}
}
