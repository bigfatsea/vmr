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

	snap := agg.Snapshot(HourlyTailDefault)

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

func TestSnapshot_StreamAndNonStreamMergedProviderRow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// 10 streaming + 10 non-streaming samples with identical tuples, but
	// toks differs by stream: streamed = 50/((4000-1000)/1000) = 16.667,
	// non-streamed = 50/(4000/1000) = 12.5. Stream and non-stream still
	// share the same ring and merge into 1 row.
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
			Tokens:   TokenCounts{In: 100, Out: 50},
		})
		agg.Record(Sample{
			TS:       now.Add(time.Duration(20+i) * time.Second),
			VModel:   "coding",
			Stream:   false,
			Outcome:  OutcomeOK,
			Provider: "p1",
			Model:    "m1",
			DurMS:    4000,
			TTFTMS:   1000,
			Tokens:   TokenCounts{In: 100, Out: 50},
		})
	}

	snap := agg.Snapshot(HourlyTailDefault)
	if len(snap.ByProviderModel) != 1 {
		t.Fatalf("expected 1 merged provider row (stream + non-stream), got %d", len(snap.ByProviderModel))
	}

	r := snap.ByProviderModel[0]
	if r.Provider != "p1" || r.Model != "m1" {
		t.Errorf("unexpected provider/model: %s:%s", r.Provider, r.Model)
	}
	if r.OK != 20 {
		t.Errorf("OK count = %d, want 20", r.OK)
	}
	if r.Last10 == nil || r.Last100 == nil {
		t.Fatalf("Last10 or Last100 nil")
	}
	if r.Last10.N != 10 {
		t.Errorf("Last10 n = %d, want 10", r.Last10.N)
	}
	if r.Last100.N != 20 {
		t.Errorf("Last100 n = %d, want 20 (both modes shared ring)", r.Last100.N)
	}
	// Last10 = the 5 most recent stream + 5 most recent non-stream samples,
	// interleaved by insertion order: pool = {12.5×5, 16.667×5} sorted,
	// p50 = ceil(0.5×10) = 5th element = 12.5 (still inside the low block).
	if math.Abs(r.Last10.ToksP50-12.5) > 1e-4 {
		t.Errorf("toks p50 = %f, want 12.5", r.Last10.ToksP50)
	}
	if r.Last10.TTFTP50 != 1000 {
		t.Errorf("ttft p50 = %d, want 1000", r.Last10.TTFTP50)
	}
	if r.Last100.Tokens.In != 2000 || r.Last100.Tokens.Out != 1000 {
		t.Errorf("window token sums wrong: %+v", r.Last100.Tokens)
	}
}

// TestSnapshot_OverallMatchesSingleRingLast100 pins the contracts §1.3
// invariant: with exactly one ring key, overall must agree numerically with
// that key's last_100 — the union is then just that ring's window.
func TestSnapshot_OverallMatchesSingleRingLast100(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.Local)
	agg, err := NewAt(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// 12 samples on one key: ttft/tokens/dur all vary, so every field of
	// the block is non-trivial.
	for i := 0; i < 12; i++ {
		agg.Record(Sample{
			TS:       now.Add(time.Duration(i) * time.Second),
			VModel:   "coding",
			Stream:   true,
			Outcome:  OutcomeOK,
			Provider: "p1",
			KeyLabel: "main",
			Model:    "m1",
			DurMS:    int64(1000 + i*100),
			TTFTMS:   int64(100 + i*10),
			Tokens:   TokenCounts{In: int64(100 + i), Out: int64(i)},
		})
	}

	snap := agg.Snapshot(HourlyTailDefault)
	if len(snap.ByProviderModel) != 1 {
		t.Fatalf("expected 1 provider row, got %d", len(snap.ByProviderModel))
	}
	if snap.Overall == nil {
		t.Fatal("overall block missing with ring data present")
	}
	last100 := snap.ByProviderModel[0].Last100
	if last100 == nil {
		t.Fatal("last_100 block missing")
	}
	if snap.Overall.N != last100.N {
		t.Errorf("overall n = %d, want last_100 n %d", snap.Overall.N, last100.N)
	}
	if snap.Overall.Tokens != last100.Tokens {
		t.Errorf("overall tokens %+v, want %+v", snap.Overall.Tokens, last100.Tokens)
	}
	if snap.Overall.TTFTP50 != last100.TTFTP50 || snap.Overall.TTFTP90 != last100.TTFTP90 {
		t.Errorf("overall ttft p50/p90 = %d/%d, want %d/%d",
			snap.Overall.TTFTP50, snap.Overall.TTFTP90, last100.TTFTP50, last100.TTFTP90)
	}
	if snap.Overall.ToksP50 != last100.ToksP50 || snap.Overall.ToksP90 != last100.ToksP90 {
		t.Errorf("overall toks p50/p90 = %f/%f, want %f/%f",
			snap.Overall.ToksP50, snap.Overall.ToksP90, last100.ToksP50, last100.ToksP90)
	}
}

// TestSnapshot_DailyTailBoundsHistory: a deployment running well past the
// retention window keeps neither the in-memory rollup nor daily[] growing —
// each hour roll evicts anything older than rollupRetentionDays (§8), so the
// fold behind every /stats call stays a bounded constant.
func TestSnapshot_DailyTailBoundsHistory(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	nDays := dailyTail + 15
	clock := base
	agg, err := NewAt(dir, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// One sample per day, clock advancing with it — the realistic shape, so
	// each day's roll runs evictOldRollup.
	for d := 0; d < nDays; d++ {
		clock = base.AddDate(0, 0, d).Add(time.Hour)
		agg.Record(Sample{
			TS:     clock,
			VModel: "coding", Outcome: OutcomeOK,
			Provider: "p1", Model: "m1", Tokens: TokenCounts{In: 1, Out: 1},
		})
	}

	// In-memory rollup stays bounded by the window, not by nDays of operation.
	// This test writes one hour-key per day, so the exact bound here is
	// rollupRetentionDays (+1 for the current partial day); real traffic fills
	// up to ~24 hour-keys per retained day.
	agg.mu.Lock()
	rollupHours := len(agg.rollup)
	agg.mu.Unlock()
	if rollupHours > rollupRetentionDays+1 {
		t.Errorf("in-memory rollup holds %d hour-keys after %d days of operation, want <= %d", rollupHours, nDays, rollupRetentionDays+1)
	}

	snap := agg.Snapshot(HourlyTailDefault)
	if len(snap.Daily) != dailyTail {
		t.Fatalf("daily rows = %d, want dailyTail=%d", len(snap.Daily), dailyTail)
	}
	// The oldest kept day sits within the retention window of the last sample.
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

	snap := agg.Snapshot(HourlyTailDefault)
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
