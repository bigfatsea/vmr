package livestats

import (
	"math"
	"slices"
	"time"
)

type ringKey struct {
	provider, keyLabel, model string
}

// ring is a fixed-capacity circular buffer of raw per-request tuples
// (design §3.4): percentiles and rates are computed at read time, so a
// formula fix never requires data migration. Entries keep the four-way
// token tally, not just the generated count — the window's usage total
// needs all four components even though the toks rate only needs out.
type ring struct {
	buf  [ringCap]ringEntry
	next int
	n    int
}

type ringEntry struct {
	ts     time.Time
	durMS  int64
	ttftMS int64
	stream bool
	tokens TokenCounts
}

func (r *ring) add(e ringEntry) {
	r.buf[r.next] = e
	r.next = (r.next + 1) % ringCap
	if r.n < ringCap {
		r.n++
	}
}

// last returns the most recent k entries, oldest first; k beyond the fill
// level returns everything there is.
func (r *ring) last(k int) []ringEntry {
	if k > r.n {
		k = r.n
	}
	out := make([]ringEntry, k)
	start := (r.next - k + ringCap) % ringCap
	for i := range out {
		out[i] = r.buf[(start+i)%ringCap]
	}
	return out
}

// windowBlock is the WindowBlock computation over a raw-entry window
// (contracts §1.2): n is the window's actual entry count, tokens are the
// four-way sums, ttft/toks percentiles are nearest-rank. ttft_ms==0 is
// "unmeasured" and stays out of the ttft pools (design §4.2); a sample
// with zero output tokens or a non-positive generation span stays out of
// the toks pool (design §8: one throughput caliber, tokens.out over the
// generation-only span).
func windowBlock(entries []ringEntry) *WindowBlock {
	wb := &WindowBlock{N: int64(len(entries))}
	if len(entries) == 0 {
		return wb
	}
	ttfts := make([]int64, 0, len(entries))
	toks := make([]float64, 0, len(entries))
	for _, e := range entries {
		wb.Tokens.add(e.tokens)
		if e.ttftMS != 0 {
			ttfts = append(ttfts, e.ttftMS)
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

// toksOf applies the single toks rate (design §8): output-token generation
// throughput. For a streamed sample the span excludes the prefill/wait
// phase (dur_ms - ttft_ms) — the client-visible TTFT already answers "was
// the wait slow", so folding it into the rate too would just dilute the
// generation signal. A non-streamed sample delivers its whole body in one
// write, so ttft_ms there marks "response ready" rather than a distinct
// prefill phase — the full dur_ms is the honest span. Zero-output,
// non-positive-span, or sub-minToksSpanMS samples yield 0 and drop out of
// the percentile population.
func toksOf(e ringEntry) float64 {
	if e.tokens.Out <= 0 || e.durMS <= 0 {
		return 0
	}
	spanMS := e.durMS
	if e.stream && e.ttftMS > 0 && e.durMS > e.ttftMS {
		spanMS = e.durMS - e.ttftMS
	}
	if spanMS < minToksSpanMS {
		return 0
	}
	return float64(e.tokens.Out) / (float64(spanMS) / 1000)
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
