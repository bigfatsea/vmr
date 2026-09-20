package livestats

import (
	"math"
	"testing"
	"time"
)

func TestGlobalRing_AddAndRecent(t *testing.T) {
	r := &globalRing{}
	now := time.Now()

	// Fill with 350 entries: should retain only the last 300.
	for i := 1; i <= 350; i++ {
		r.add(RecentRequestEntry{
			TS:       now.Add(time.Duration(i) * time.Second),
			DurMS:    int64(i * 10),
			TTFTMS:   int64(i),
			Tokens:   TokenCounts{Out: int64(i * 2)},
			Provider: "p1",
			Model:    "m1",
		})
	}

	if r.n != globalRingCap {
		t.Fatalf("expected ring size %d, got %d", globalRingCap, r.n)
	}

	// recent(10) should return newest first: entries 350..341.
	rec10 := r.recent(10)
	if len(rec10) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(rec10))
	}
	for i, e := range rec10 {
		expectedVal := int64(350 - i)
		if e.TTFTMS != expectedVal {
			t.Errorf("recent10[%d]: expected ttftMS %d, got %d", i, expectedVal, e.TTFTMS)
		}
	}

	// recent(300) should return entries 350..51.
	rec300 := r.recent(300)
	if len(rec300) != 300 {
		t.Fatalf("expected 300 entries, got %d", len(rec300))
	}
	if rec300[0].TTFTMS != 350 || rec300[299].TTFTMS != 51 {
		t.Errorf("rec300 boundary mismatch: newest=%d, oldest=%d", rec300[0].TTFTMS, rec300[299].TTFTMS)
	}

	// Requesting more than globalRingCap clamped to capacity.
	recMore := r.recent(400)
	if len(recMore) != 300 {
		t.Errorf("expected clamp to 300, got %d", len(recMore))
	}
}

func TestNearestRank_KnownDistribution(t *testing.T) {
	// Distribution: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10
	vals := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	// p50: ceil(0.50 * 10) = 5 -> vals[4] = 5
	if p50 := nearestRankInt(vals, 0.5); p50 != 5 {
		t.Errorf("p50: expected 5, got %d", p50)
	}
	// p90: ceil(0.90 * 10) = 9 -> vals[8] = 9
	if p90 := nearestRankInt(vals, 0.9); p90 != 9 {
		t.Errorf("p90: expected 9, got %d", p90)
	}

	// Single element
	single := []int64{42}
	if p50 := nearestRankInt(single, 0.5); p50 != 42 {
		t.Errorf("single p50: expected 42, got %d", p50)
	}
	if p90 := nearestRankInt(single, 0.9); p90 != 42 {
		t.Errorf("single p90: expected 42, got %d", p90)
	}

	// Empty
	if empty := nearestRankInt(nil, 0.5); empty != 0 {
		t.Errorf("empty p50: expected 0, got %d", empty)
	}
}

// TestWindowBlock_ToksStreamSubtractsTTFT pins the toks rate's per-stream
// span (design §8): a streamed sample's denominator drops the prefill/wait
// phase (dur_ms - ttft_ms), while a non-streamed sample — whose ttft_ms
// marks "response ready", not a distinct prefill phase — keeps the whole
// dur_ms. Same raw tuple, different stream flag, different rate.
func TestWindowBlock_ToksStreamSubtractsTTFT(t *testing.T) {
	streamed := windowBlock([]RecentRequestEntry{
		{DurMS: 5000, TTFTMS: 1000, Stream: true, Tokens: TokenCounts{Out: 100}}, // toks = 100/4.0 = 25.0
	})
	if math.Abs(streamed.ToksP50-25.0) > 1e-6 {
		t.Errorf("streamed: toks p50 = %f, want 25.0", streamed.ToksP50)
	}

	nonStreamed := windowBlock([]RecentRequestEntry{
		{DurMS: 5000, TTFTMS: 1000, Stream: false, Tokens: TokenCounts{Out: 100}}, // toks = 100/5.0 = 20.0
	})
	if math.Abs(nonStreamed.ToksP50-20.0) > 1e-6 {
		t.Errorf("non-streamed: toks p50 = %f, want 20.0", nonStreamed.ToksP50)
	}
}

// TestWindowBlock_ExclusionRules pins the percentile-pool admission rules
// (contracts §1.2): ttft==0 (unmeasured) stays out of the ttft pools; a
// zero-token-total or non-positive dur_ms sample stays out of the toks
// pools — while n still counts every sample in the window and tokens still
// sums the four components.
func TestWindowBlock_ExclusionRules(t *testing.T) {
	entries := []RecentRequestEntry{
		{DurMS: 1000, TTFTMS: 100, Tokens: TokenCounts{In: 100, Out: 100}}, // toks = 100 tok-out / 1s = 100
		{DurMS: 1000, TTFTMS: 0, Tokens: TokenCounts{Out: 400}},            // no ttft; toks = 400
		{DurMS: 1000, TTFTMS: 300, Tokens: TokenCounts{}},                  // zero out: no toks
		{DurMS: 0, TTFTMS: 300, Tokens: TokenCounts{Out: 500}},             // zero span: no toks
		{DurMS: 2000, TTFTMS: 200, Tokens: TokenCounts{CacheRead: 300}},    // zero out: no toks
	}
	wb := windowBlock(entries)
	if wb.N != 5 {
		t.Errorf("n = %d, want 5 (every sample counts)", wb.N)
	}
	if wb.Tokens.In != 100 || wb.Tokens.Out != 1000 || wb.Tokens.CacheRead != 300 {
		t.Errorf("tokens sums wrong: %+v", wb.Tokens)
	}
	// ttft pool = {100, 300, 200} sorted: p50=200, p90=300.
	if wb.TTFTP50 != 200 || wb.TTFTP90 != 300 {
		t.Errorf("ttft p50/p90 = %d/%d, want 200/300", wb.TTFTP50, wb.TTFTP90)
	}
	// toks pool = {100, 400} sorted ascending: p50 = ceil(0.5*2)=1st = 100,
	// p10 = ceil(0.1*2)=1st = 100 too (n=2 is too small to separate them —
	// TestWindowBlock_ToksP10IsWorstCaseNotFastest below uses a bigger pool).
	if math.Abs(wb.ToksP50-100) > 1e-6 || math.Abs(wb.ToksP10-100) > 1e-6 {
		t.Errorf("toks p50/p10 = %f/%f, want 100/100", wb.ToksP50, wb.ToksP10)
	}

	// All-unmeasured ttft and all-zero tokens leave the pools empty.
	empty := windowBlock([]RecentRequestEntry{
		{DurMS: 1000, TTFTMS: 0},
		{DurMS: 1000, TTFTMS: 0, Tokens: TokenCounts{}},
	})
	if empty.TTFTP50 != 0 || empty.TTFTP90 != 0 || empty.ToksP50 != 0 || empty.ToksP10 != 0 {
		t.Errorf("expected zeroed percentiles, got %+v", empty)
	}
}

func TestWindowBlock_ToksP10IsWorstCaseNotFastest(t *testing.T) {
	entries := make([]RecentRequestEntry, 0, 10)
	for i := 0; i < 9; i++ {
		entries = append(entries, RecentRequestEntry{DurMS: 1000, TTFTMS: 100, Tokens: TokenCounts{Out: 100}}) // 100 tok/s
	}
	entries = append(entries, RecentRequestEntry{DurMS: 1000, TTFTMS: 100, Tokens: TokenCounts{Out: 10}}) // 10 tok/s, the slow tail
	wb := windowBlock(entries)
	if math.Abs(wb.ToksP50-100) > 1e-6 {
		t.Errorf("toks p50 = %f, want 100 (median is still in the fast block)", wb.ToksP50)
	}
	if math.Abs(wb.ToksP10-10) > 1e-6 {
		t.Errorf("toks p10 = %f, want 10 (the slow outlier, not the fast 90%%)", wb.ToksP10)
	}
}

func TestToksOf_MinSpanFloorDropsImplausibleBursts(t *testing.T) {
	tiny := RecentRequestEntry{DurMS: 1002, TTFTMS: 1000, Stream: true, Tokens: TokenCounts{Out: 50}} // span = 2ms → 25000 tok/s
	if v := toksOf(tiny); v != 0 {
		t.Errorf("toksOf(2ms span) = %f, want 0 (below minToksSpanMS)", v)
	}

	ok := RecentRequestEntry{DurMS: 1050, TTFTMS: 1000, Stream: true, Tokens: TokenCounts{Out: 50}} // span = 50ms, at the floor
	if v := toksOf(ok); v != 1000 {
		t.Errorf("toksOf(50ms span) = %f, want 1000 (50 tok / 0.05s)", v)
	}
}

func TestWindowBlock_CountsActualSamples(t *testing.T) {
	r := &globalRing{}
	now := time.Now()
	for i := 0; i < 7; i++ {
		r.add(RecentRequestEntry{TS: now.Add(time.Duration(i) * time.Second), DurMS: 1000, TTFTMS: 50, Tokens: TokenCounts{Out: 10}})
	}
	wb := windowBlock(r.recent(10))
	if wb == nil || wb.N != 7 {
		t.Fatalf("n = %d, want 7 (ring not full)", r.n)
	}
	// tokens = 7 × {Out:10}
	if wb.Tokens.Out != 70 {
		t.Errorf("tokens.out sum = %d, want 70", wb.Tokens.Out)
	}
	if wb.TTFTP50 != 50 || math.Abs(wb.ToksP50-10) > 1e-6 {
		t.Errorf("percentiles wrong: %+v", wb)
	}
}
