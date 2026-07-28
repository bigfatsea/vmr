// Ver 2026-07-28 20:05, by Opus 5

// Sticky Model effectiveness: does sending a conversation back to the same
// endpoint actually keep its prompt cache warm? See StickyEffect (rows.go)
// for what is and isn't being claimed.
//
// Kept out of aggregate.go's Build because it is the one metric that cannot
// be accumulated as records stream past: "same endpoint as the previous
// request of this session" needs the session's requests in order, and
// records from different sessions arrive interleaved. So Build feeds
// entries in here as it goes and asks for the result at the end.
package report

import "sort"

// stickyEntry is the per-record slice of state this metric needs. Kept to
// plain scalars rather than a *rec2: every record in the input has to be
// buffered until the pass ends, and holding the full working struct alive
// would pin the whole log in memory on a large report.
type stickyEntry struct {
	seq             int // SessSeq: position within its session
	endpoint        string
	model, protocol string
	cached, fresh   int64
	cacheWrite      int64
	known           bool // usage was reported for this record
}

// stickyCollector buffers per-session entries during Build's pass.
type stickyCollector struct {
	bySession map[string][]stickyEntry
	ungrouped int
}

func newStickyCollector() *stickyCollector {
	return &stickyCollector{bySession: map[string][]stickyEntry{}}
}

func (sc *stickyCollector) add(rc *rec2) {
	// No endpoint means nothing ever served this request (every attempt
	// failed before a response) — there is no continuity to judge.
	if rc.endpoint == "" {
		return
	}
	if rc.sessionID == "" {
		sc.ungrouped++
		return
	}
	fresh := rc.usage.In - rc.usage.CacheRead - rc.usage.CacheWrite
	if fresh < 0 {
		fresh = 0
	}
	sc.bySession[rc.sessionID] = append(sc.bySession[rc.sessionID], stickyEntry{
		seq: rc.sessSeq, endpoint: rc.endpoint, model: rc.model, protocol: rc.protocol,
		cached: rc.usage.CacheRead, fresh: fresh, cacheWrite: rc.usage.CacheWrite,
		known: rc.usageOK,
	})
}

func (g *StickyGroup) add(e stickyEntry) {
	g.Requests++
	if !e.known {
		return
	}
	g.TokensKnown++
	g.TokensInCached += e.cached
	g.TokensInFresh += e.fresh
	g.TokensInCacheWrite += e.cacheWrite
}

func (g *StickyGroup) finish() {
	g.CacheEfficiency = cacheEff(g.TokensInCached, g.TokensInFresh)
}

// result walks each session in sequence order and classifies every request
// after the first. Returns nil when nothing could be classified at all —
// an empty comparison renders as an absent section rather than as two rows
// of zeroes that look like a finding.
func (sc *stickyCollector) result() *StickyEffect {
	eff := &StickyEffect{Ungrouped: sc.ungrouped}
	byModel := map[string]*StickyModelRow{}

	for _, entries := range sc.bySession {
		sort.Slice(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })
		for i, e := range entries {
			if i == 0 {
				eff.First++
				continue
			}
			key := e.model + "\x00" + e.protocol
			mr := byModel[key]
			if mr == nil {
				mr = &StickyModelRow{Model: e.model, Protocol: e.protocol}
				byModel[key] = mr
			}
			if e.endpoint == entries[i-1].endpoint {
				eff.Continued.add(e)
				mr.Continued.add(e)
			} else {
				eff.Switched.add(e)
				mr.Switched.add(e)
			}
		}
	}
	if eff.Continued.Requests == 0 && eff.Switched.Requests == 0 {
		return nil
	}
	eff.Continued.finish()
	eff.Switched.finish()
	for _, mr := range byModel {
		mr.Continued.finish()
		mr.Switched.finish()
		eff.ByModel = append(eff.ByModel, *mr)
	}
	// Most-compared model first; name as the tie-break so the output is
	// stable across runs (map iteration order is not).
	sort.Slice(eff.ByModel, func(i, j int) bool {
		a, b := eff.ByModel[i], eff.ByModel[j]
		an := a.Continued.Requests + a.Switched.Requests
		bn := b.Continued.Requests + b.Switched.Requests
		if an != bn {
			return an > bn
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Protocol < b.Protocol
	})
	return eff
}
