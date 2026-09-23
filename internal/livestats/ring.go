package livestats

import (
	"math"
	"slices"
)

type provKey struct {
	provider, keyLabel, model string
}

// windowBlock is the WindowBlock computation over a slice of RecentRequestEntry
// (the console's /stats contract): n is the window's actual
// entry count, tokens are the
// four-way sums, ttft/toks percentiles are nearest-rank. ttft_ms==0 is
// "unmeasured" and stays out of the ttft pools (the design doc); a sample
// with zero output tokens or a non-positive generation span stays out of
// the toks pool (one throughput caliber, tokens.out over the
// generation-only span).
func windowBlock(entries []RecentRequestEntry) *WindowBlock {
	wb := &WindowBlock{N: int64(len(entries))}
	if len(entries) == 0 {
		return wb
	}
	ttfts := make([]int64, 0, len(entries))
	toks := make([]float64, 0, len(entries))
	for _, e := range entries {
		wb.Tokens.add(e.Tokens)
		if e.TTFTMS != 0 {
			ttfts = append(ttfts, e.TTFTMS)
		}
		if v := toksOf(e); v > 0 {
			toks = append(toks, v)
		}
	}
	slices.Sort(ttfts)
	slices.Sort(toks)
	wb.TTFTP50 = nearestRankInt(ttfts, 0.5)
	wb.TTFTP90 = nearestRankInt(ttfts, 0.9)
	wb.ToksP50 = nearestRankFloat(toks, 0.5)
	// toks is a yield metric (bigger is better), the opposite of TTFT (a cost
	// metric) — its worst-case tail is the BOTTOM decile, not the top one.
	// Reusing p90 here would report the fastest 10% of requests as if it were
	// a guaranteed floor. See WindowBlock's doc comment.
	wb.ToksP10 = nearestRankFloat(toks, 0.1)
	return wb
}

// minToksSpanMS floors the generation span toksOf will rate: a streamed
// response's post-TTFT span can be a couple of milliseconds when the
// upstream flushes its last chunks back-to-back (or a network hiccup
// delivers a burst after a stall), and dividing a handful of tokens by a
// single-digit millisecond span produces a rate in the thousands of
// tok/s — physically implausible, and it single-handedly dominates the
// window's percentiles. Below this floor the sample carries no reliable
// throughput signal and is dropped from the toks pool entirely (same
// treatment as zero-output/non-positive-span).
const minToksSpanMS = 50

// toksOf applies the single toks rate: output-token generation
// throughput. For a streamed sample the span excludes the prefill/wait
// phase (dur_ms - ttft_ms) — the client-visible TTFT already answers "was
// the wait slow", so folding it into the rate too would just dilute the
// generation signal. A non-streamed sample delivers its whole body in one
// write, so ttft_ms there marks "response ready" rather than a distinct
// prefill phase — the full dur_ms is the honest span. Zero-output,
// non-positive-span, or sub-minToksSpanMS samples yield 0 and drop out of
// the percentile population.
func toksOf(e RecentRequestEntry) float64 {
	if e.Tokens.Out <= 0 || e.DurMS <= 0 {
		return 0
	}
	spanMS := e.DurMS
	if e.Stream && e.TTFTMS > 0 && e.DurMS > e.TTFTMS {
		spanMS = e.DurMS - e.TTFTMS
	}
	if spanMS < minToksSpanMS {
		return 0
	}
	return float64(e.Tokens.Out) / (float64(spanMS) / 1000)
}

// overallBlock computes the WindowBlock across the global ring's retained samples.
func overallBlock(gr *globalRing) *WindowBlock {
	if gr == nil || gr.n == 0 {
		return nil
	}
	return windowBlock(gr.recent(globalRingCap))
}

// nearestRank{Int,Float} are the nearest-rank percentile over a pre-sorted
// slice: ceil(p·n), clamped to [1, n].
func nearestRankInt(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p * float64(len(sorted))))
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func nearestRankFloat(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p * float64(len(sorted))))
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

// globalRing is a fixed-capacity circular buffer of the last globalRingCap
// (300) completed ok+forwarded requests across all providers and models.
type globalRing struct {
	buf  [globalRingCap]RecentRequestEntry
	next int
	n    int
}

func (r *globalRing) add(e RecentRequestEntry) {
	if r == nil {
		return
	}
	r.buf[r.next] = e
	r.next = (r.next + 1) % globalRingCap
	if r.n < globalRingCap {
		r.n++
	}
}

// recent returns the most recent k entries, newest first; k <= 0 or beyond
// fill level returns all available entries.
func (r *globalRing) recent(k int) []RecentRequestEntry {
	if r == nil || r.n == 0 {
		return []RecentRequestEntry{}
	}
	if k <= 0 || k > r.n {
		k = r.n
	}
	out := make([]RecentRequestEntry, k)
	for i := 0; i < k; i++ {
		idx := (r.next - 1 - i + globalRingCap) % globalRingCap
		out[i] = r.buf[idx]
	}
	return out
}

func addGlobalRing(gr *globalRing, s Sample) {
	if gr == nil || s.Outcome != OutcomeOK || !s.Forwarded || s.TTFTMS == 0 {
		return
	}
	gr.add(RecentRequestEntry{
		TS:       s.TS,
		Provider: s.Provider,
		KeyLabel: s.KeyLabel,
		Model:    s.Model,
		Stream:   s.Stream,
		DurMS:    s.DurMS,
		TTFTMS:   s.TTFTMS,
		Tokens:   s.Tokens,
	})
}
