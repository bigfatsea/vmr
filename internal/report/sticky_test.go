// Ver 2026-07-28 21:00, by Opus 5
package report

import "testing"

// entry is a terse constructor for the table below.
func stickyE(seq int, endpoint, model string, cached, fresh int64) stickyEntry {
	return stickyEntry{seq: seq, endpoint: endpoint, model: model, protocol: "openai",
		cached: cached, fresh: fresh, known: true}
}

func TestStickyClassifiesAgainstThePreviousRequestInSession(t *testing.T) {
	sc := newStickyCollector()
	sc.bySession["s1"] = []stickyEntry{
		// Deliberately out of order: the collector must sort by seq, not
		// trust arrival order (records from many sessions interleave).
		stickyE(3, "epB", "coding", 0, 1000),  // switched away from epA
		stickyE(1, "epA", "coding", 0, 1000),  // session's first: excluded
		stickyE(2, "epA", "coding", 900, 100), // continued on epA: warm cache
		stickyE(4, "epB", "coding", 800, 200), // continued on epB
	}
	got := sc.result()
	if got == nil {
		t.Fatal("result() = nil, want a comparison")
	}
	if got.First != 1 {
		t.Errorf("First = %d, want 1", got.First)
	}
	if got.Continued.Requests != 2 || got.Switched.Requests != 1 {
		t.Errorf("continued=%d switched=%d, want 2/1", got.Continued.Requests, got.Switched.Requests)
	}
	// continued: (900+800) cached vs (100+200) fresh = 85%
	if got.Continued.CacheEfficiency != 0.85 {
		t.Errorf("continued cache efficiency = %v, want 0.85", got.Continued.CacheEfficiency)
	}
	// switched: 0 cached vs 1000 fresh = 0%
	if got.Switched.CacheEfficiency != 0 {
		t.Errorf("switched cache efficiency = %v, want 0", got.Switched.CacheEfficiency)
	}
}

// The session's first request has no predecessor, so its cache state says
// nothing about continuity — counting it in either group would bias the
// comparison toward "switching is cold" for reasons that have nothing to
// do with switching.
func TestStickyFirstRequestOfEachSessionIsExcluded(t *testing.T) {
	sc := newStickyCollector()
	sc.bySession["s1"] = []stickyEntry{stickyE(1, "epA", "coding", 0, 500)}
	sc.bySession["s2"] = []stickyEntry{stickyE(1, "epA", "coding", 0, 500)}
	if got := sc.result(); got != nil {
		t.Fatalf("result() = %+v, want nil — nothing was classifiable", got)
	}
}

// Records that never got a session id are counted separately rather than
// guessed at: "no predecessor is knowable" is a different statement from
// "it switched".
func TestStickyUngroupedRecordsAreCountedNotClassified(t *testing.T) {
	sc := newStickyCollector()
	sc.add(&rec2{endpoint: "epA", model: "coding", usageOK: true})
	sc.bySession["s1"] = []stickyEntry{stickyE(1, "epA", "coding", 0, 1), stickyE(2, "epA", "coding", 5, 5)}
	got := sc.result()
	if got.Ungrouped != 1 {
		t.Errorf("Ungrouped = %d, want 1", got.Ungrouped)
	}
	if got.Continued.Requests != 1 || got.Switched.Requests != 0 {
		t.Errorf("ungrouped record leaked into a group: %+v", got)
	}
}

// A request no endpoint ever served has no continuity to judge — and it
// must not be miscounted as ungrouped either.
func TestStickySkipsRecordsWithNoServingEndpoint(t *testing.T) {
	sc := newStickyCollector()
	sc.add(&rec2{endpoint: "", sessionID: "", model: "coding"})
	sc.add(&rec2{endpoint: "", sessionID: "s1", model: "coding"})
	if sc.ungrouped != 0 || len(sc.bySession) != 0 {
		t.Errorf("ungrouped=%d sessions=%d, want 0/0", sc.ungrouped, len(sc.bySession))
	}
}

// Anthropic's usage counts input separately from the cache counters, so
// fresh must be derived as In-CacheRead-CacheWrite and never go negative
// on a provider that reports them inconsistently.
func TestStickyFreshNeverNegative(t *testing.T) {
	sc := newStickyCollector()
	sc.add(&rec2{
		endpoint: "epA", sessionID: "s1", model: "coding", usageOK: true,
		usage: Usage{In: 10, CacheRead: 100, CacheWrite: 50},
	})
	if got := sc.bySession["s1"][0].fresh; got != 0 {
		t.Errorf("fresh = %d, want 0 (clamped)", got)
	}
}

func TestStickyByModelSortedByComparedVolume(t *testing.T) {
	sc := newStickyCollector()
	sc.bySession["s1"] = []stickyEntry{
		stickyE(1, "epA", "agent", 0, 1), stickyE(2, "epA", "agent", 1, 1),
	}
	sc.bySession["s2"] = []stickyEntry{
		stickyE(1, "epA", "coding", 0, 1), stickyE(2, "epA", "coding", 1, 1),
		stickyE(3, "epB", "coding", 0, 1), stickyE(4, "epB", "coding", 1, 1),
	}
	got := sc.result()
	if len(got.ByModel) != 2 || got.ByModel[0].Model != "coding" {
		t.Fatalf("ByModel = %+v, want coding (3 compared) before agent (1)", got.ByModel)
	}
}
