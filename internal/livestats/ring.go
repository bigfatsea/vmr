package livestats

import (
	"math"
	"slices"
	"time"
)

type ringKey struct {
	provider, keyLabel, model string
	stream                    bool
}

// ring is a fixed-capacity circular buffer of raw per-request tuples
// (design §3.4): percentiles and rates are computed at read time, so a
// formula fix never requires data migration. Entries keep the four-way
// token tally, not just the generated count — both the window's usage
// total and the single-request toks rate need all four components.
type ring struct {
	buf  [ringCap]ringEntry
	next int
	n    int
}

type ringEntry struct {
	ts     time.Time
	durMS  int64
	ttftMS int64
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
// with zero total tokens or a non-positive dur_ms stays out of the toks
// pool. The toks denominator is dur_ms for both stream and non-stream
// (design §8: tps revoked, one rate).
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
	wb.ToksP90 = nearestRankFloat(toks, 0.9)
	return wb
}

// toksOf applies the single toks rate (design §8): the four-way token sum
// over the whole request span. Zero-token or non-positive-span samples
// yield 0 and drop out of the percentile population.
func toksOf(e ringEntry) float64 {
	total := e.tokens.In + e.tokens.Out + e.tokens.CacheRead + e.tokens.CacheWrite
	if total <= 0 || e.durMS <= 0 {
		return 0
	}
	return float64(total) / (float64(e.durMS) / 1000)
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
