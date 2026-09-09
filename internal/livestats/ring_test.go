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
			ts:        now.Add(time.Duration(i) * time.Second),
			durMS:     int64(i * 10),
			ttftMS:    int64(i),
			tokensOut: int64(i * 2),
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

func TestTPSCalculation_StreamVsNonStream(t *testing.T) {
	// Sample with 100 tokens out, dur 5000ms, ttft 1000ms.
	e := ringEntry{
		durMS:     5000,
		ttftMS:    1000,
		tokensOut: 100,
	}

	// Streaming TPS denominator: (durMS - ttftMS) / 1000 = (5000 - 1000) / 1000 = 4.0s
	// TPS = 100 / 4.0 = 25.0
	tpsStream := tpsOf(e, true)
	if math.Abs(tpsStream-25.0) > 1e-6 {
		t.Errorf("streaming TPS: expected 25.0, got %f", tpsStream)
	}

	// Non-streaming TPS denominator: durMS / 1000 = 5000 / 1000 = 5.0s
	// TPS = 100 / 5.0 = 20.0
	tpsNonStream := tpsOf(e, false)
	if math.Abs(tpsNonStream-20.0) > 1e-6 {
		t.Errorf("non-streaming TPS: expected 20.0, got %f", tpsNonStream)
	}

	// Zero tokens out: should drop out (0.0)
	eZeroTokens := ringEntry{durMS: 1000, ttftMS: 100, tokensOut: 0}
	if v := tpsOf(eZeroTokens, true); v != 0 {
		t.Errorf("zero tokens expected 0, got %f", v)
	}

	// Non-positive span: should drop out (0.0)
	eNoSpan := ringEntry{durMS: 100, ttftMS: 100, tokensOut: 10}
	if v := tpsOf(eNoSpan, true); v != 0 {
		t.Errorf("zero span streaming expected 0, got %f", v)
	}
}
