package livestats

import (
	"math"
	"testing"
	"time"
)

func TestRing_AddAndLast(t *testing.T) {
	r := &ring{}
	now := time.Now()

	// Fill with 150 entries: should retain only the last 100.
	for i := 1; i <= 150; i++ {
		r.add(ringEntry{
			ts:     now.Add(time.Duration(i) * time.Second),
			durMS:  int64(i * 10),
			ttftMS: int64(i),
			tokens: TokenCounts{Out: int64(i * 2)},
		})
	}

	if r.n != ringCap {
		t.Fatalf("expected ring size %d, got %d", ringCap, r.n)
	}

	// last(10) should return entries 141..150 in chronological order.
	last10 := r.last(10)
	if len(last10) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(last10))
	}
	for i, e := range last10 {
		expectedVal := int64(141 + i)
		if e.ttftMS != expectedVal {
			t.Errorf("last10[%d]: expected ttftMS %d, got %d", i, expectedVal, e.ttftMS)
		}
	}

	// last(100) should return entries 51..150.
	last100 := r.last(100)
	if len(last100) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(last100))
	}
	if last100[0].ttftMS != 51 || last100[99].ttftMS != 150 {
		t.Errorf("last100 boundary mismatch: first=%d, last=%d", last100[0].ttftMS, last100[99].ttftMS)
	}

	// Requesting more than ringCap clamped to capacity.
	lastMore := r.last(200)
	if len(lastMore) != 100 {
		t.Errorf("expected clamp to 100, got %d", len(lastMore))
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
	streamed := windowBlock([]ringEntry{
		{durMS: 5000, ttftMS: 1000, stream: true, tokens: TokenCounts{Out: 100}}, // toks = 100/4.0 = 25.0
	})
	if math.Abs(streamed.ToksP50-25.0) > 1e-6 {
		t.Errorf("streamed: toks p50 = %f, want 25.0", streamed.ToksP50)
	}

	nonStreamed := windowBlock([]ringEntry{
		{durMS: 5000, ttftMS: 1000, stream: false, tokens: TokenCounts{Out: 100}}, // toks = 100/5.0 = 20.0
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
	entries := []ringEntry{
		{durMS: 1000, ttftMS: 100, tokens: TokenCounts{In: 100, Out: 100}}, // toks = 100 tok-out / 1s = 100
		{durMS: 1000, ttftMS: 0, tokens: TokenCounts{Out: 400}},            // no ttft; toks = 400
		{durMS: 1000, ttftMS: 300, tokens: TokenCounts{}},                  // zero out: no toks
		{durMS: 0, ttftMS: 300, tokens: TokenCounts{Out: 500}},             // zero span: no toks
		{durMS: 2000, ttftMS: 200, tokens: TokenCounts{CacheRead: 300}},    // zero out: no toks
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
	empty := windowBlock([]ringEntry{
		{durMS: 1000, ttftMS: 0},
		{durMS: 1000, ttftMS: 0, tokens: TokenCounts{}},
	})
	if empty.TTFTP50 != 0 || empty.TTFTP90 != 0 || empty.ToksP50 != 0 || empty.ToksP10 != 0 {
		t.Errorf("expected zeroed percentiles, got %+v", empty)
	}
}

// TestWindowBlock_ToksP10IsWorstCaseNotFastest pins the P0-1 fix: toks is a
// yield metric (bigger is better), so its tail must be the bottom decile —
// the slow outlier — not the top decile the old p90 computation picked out.
// Ten requests, nine fast (100 tok/s) and one slow (10 tok/s): the old
// ascending-p90 read would have reported ~100 (the fast head of the
// distribution) as if it were a guaranteed floor; p10 must report the one
// genuinely slow request instead.
func TestWindowBlock_ToksP10IsWorstCaseNotFastest(t *testing.T) {
	entries := make([]ringEntry, 0, 10)
	for i := 0; i < 9; i++ {
		entries = append(entries, ringEntry{durMS: 1000, ttftMS: 100, tokens: TokenCounts{Out: 100}}) // 100 tok/s
	}
	entries = append(entries, ringEntry{durMS: 1000, ttftMS: 100, tokens: TokenCounts{Out: 10}}) // 10 tok/s, the slow tail
	wb := windowBlock(entries)
	if math.Abs(wb.ToksP50-100) > 1e-6 {
		t.Errorf("toks p50 = %f, want 100 (median is still in the fast block)", wb.ToksP50)
	}
	if math.Abs(wb.ToksP10-10) > 1e-6 {
		t.Errorf("toks p10 = %f, want 10 (the slow outlier, not the fast 90%%)", wb.ToksP10)
	}
}

// TestToksOf_MinSpanFloorDropsImplausibleBursts pins the P0-1 fix's second
// half: a sub-minToksSpanMS generation span (an upstream flushing its last
// chunks back-to-back, or a network burst after a stall) produces a
// physically implausible rate and must be excluded from the toks pool
// entirely, the same as a zero-output or non-positive-span sample.
func TestToksOf_MinSpanFloorDropsImplausibleBursts(t *testing.T) {
	tiny := ringEntry{durMS: 1002, ttftMS: 1000, stream: true, tokens: TokenCounts{Out: 50}} // span = 2ms → 25000 tok/s
	if v := toksOf(tiny); v != 0 {
		t.Errorf("toksOf(2ms span) = %f, want 0 (below minToksSpanMS)", v)
	}

	ok := ringEntry{durMS: 1050, ttftMS: 1000, stream: true, tokens: TokenCounts{Out: 50}} // span = 50ms, at the floor
	if v := toksOf(ok); v != 1000 {
		t.Errorf("toksOf(50ms span) = %f, want 1000 (50 tok / 0.05s)", v)
	}
}

// TestWindowBlock_CountsActualSamples pins that n reflects the window's
// real fill level, not the nominal capacity (contracts §1.2).
func TestWindowBlock_CountsActualSamples(t *testing.T) {
	r := &ring{}
	now := time.Now()
	for i := 0; i < 7; i++ {
		r.add(ringEntry{ts: now.Add(time.Duration(i) * time.Second), durMS: 1000, ttftMS: 50, tokens: TokenCounts{Out: 10}})
	}
	wb := windowBlock(r.last(10))
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
