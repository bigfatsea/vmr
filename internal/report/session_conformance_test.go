// Ver 2026-07-29 22:30, by Sonnet 5

// T2.5 (design doc Appendix C.4/§7.2): a one-shot conformance check between
// this package's own session grouping (AnalyzeSessions/group) and
// ctxgraph's independently-computed lineage grouping (Scan), on the same
// corpus. This was R8's only mitigation before design doc Appendix C.5 T3.1
// ("Phase A permanently forks from report, Phase B never happens") — every
// difference had to land on one of two explicit outcomes, expectedSame or a
// whitelisted knownImprovement (F6: anchor gluing two lineages together
// across a hidden Contract/Fork break).
//
// Post-T3.1, group() sources its grouping FROM this same Scan output, so
// every assertion below is now a same-source consistency check rather than
// a comparison between two independent implementations — but it still earns
// its keep as a regression guard on that wiring (a future change to group()
// that quietly stopped consuming ctxgraph correctly would show up here
// first) and as the one place F6's fix is verified end-to-end through the
// production AnalyzeSessions entry point, including the new
// linkStitchedLineages ContinuedFrom link (see
// TestConformance_F6AnchorGluedLineageSplitMatchesCtxgraph).
//
// File placement note: design doc Appendix C.4 named this file
// internal/story/conformance_test.go, but internal/story is forbidden from
// depending on internal/report (see internal/archtest's import boundary
// rule) — this comparison needs BOTH AnalyzeSessions (report) and Scan
// (ctxgraph), so it can only live here, in internal/report, as a _test.go
// file.
package report

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// recLoc is defined in session.go (production code) — group() now needs the
// same (path, line) coordinate to correlate a ReqInfo with its
// ctxgraph.Manifest, so it moved out of this file rather than duplicating.

// groupingComparison holds both systems' record-to-bucket assignment for
// one corpus, built once and reused across assertions.
type groupingComparison struct {
	// report side: only records AnalyzeSessions actually assigned to a
	// Session (compaction-tagged and ungrouped records have no entry here —
	// report deliberately excludes them from Sessions, see group()).
	reportSession map[recLoc]string
	reportSessKey map[recLoc]string

	// ctxgraph side: every record Scan produced a Manifest for lands in
	// exactly one Lineage (Ungrouped manifests have no entry).
	lineageOf      map[recLoc]int
	lineageSessKey map[int]string
	lineageBroke   map[int]*ctxgraph.BreakInfo
}

func compareGrouping(t *testing.T, paths []string) groupingComparison {
	t.Helper()
	a, err := AnalyzeSessions(paths)
	if err != nil {
		t.Fatalf("AnalyzeSessions: %v", err)
	}
	g, err := ctxgraph.Scan(paths)
	if err != nil {
		t.Fatalf("ctxgraph.Scan: %v", err)
	}

	cmp := groupingComparison{
		reportSession:  map[recLoc]string{},
		reportSessKey:  map[recLoc]string{},
		lineageOf:      map[recLoc]int{},
		lineageSessKey: map[int]string{},
		lineageBroke:   map[int]*ctxgraph.BreakInfo{},
	}
	for _, s := range a.Sessions {
		for _, r := range s.Recs {
			loc := recLoc{r.Path, r.Line}
			cmp.reportSession[loc] = s.ID
			cmp.reportSessKey[loc] = r.SessKey
		}
	}
	for _, l := range g.Lineages {
		cmp.lineageSessKey[l.Idx] = l.SessKey
		cmp.lineageBroke[l.Idx] = l.BrokeFrom
		for _, m := range l.Manifests {
			cmp.lineageOf[recLoc{m.Path, m.Line}] = l.Idx
		}
	}
	return cmp
}

// lineagesForSession returns the distinct ctxgraph lineage indices every
// record of report session sessionID maps to.
func (c groupingComparison) lineagesForSession(sessionID string) map[int]bool {
	out := map[int]bool{}
	for loc, sid := range c.reportSession {
		if sid == sessionID {
			if idx, ok := c.lineageOf[loc]; ok {
				out[idx] = true
			}
		}
	}
	return out
}

// reportSessionsForLineage is lineagesForSession's mirror: every distinct
// report session index lineageIdx's records map to.
func (c groupingComparison) reportSessionsForLineage(lineageIdx int) map[string]bool {
	out := map[string]bool{}
	for loc, idx := range c.lineageOf {
		if idx == lineageIdx {
			if sid, ok := c.reportSession[loc]; ok {
				out[sid] = true
			}
		}
	}
	return out
}

// TestConformance_SessKeyAgreesEndToEnd checks SessKey agreement through the
// actual production entry points — AnalyzeSessions and Scan. Post design doc
// Appendix C.5 T3.1, group() sources ReqInfo.SessKey directly from the
// correlated ctxgraph.Manifest, so this is closer to a wiring check (did the
// (path,line) correlation in group() actually run) than an independent
// cross-implementation comparison — see Appendix F.7's note on this file's
// changed nature. Still the foundation every other assertion here depends
// on: if the correlation were ever broken, the higher-level session/lineage
// comparisons below would be comparing apples to oranges without any way to
// tell.
func TestConformance_SessKeyAgreesEndToEnd(t *testing.T) {
	path, _ := fixture(t)
	cmp := compareGrouping(t, []string{path})
	checked := 0
	for loc, reportKey := range cmp.reportSessKey {
		idx, ok := cmp.lineageOf[loc]
		if !ok {
			t.Errorf("%v: report assigned SessKey %q but ctxgraph produced no lineage for it", loc, reportKey)
			continue
		}
		if ctxKey := cmp.lineageSessKey[idx]; ctxKey != reportKey {
			t.Errorf("%v: SessKey mismatch — report=%q ctxgraph=%q", loc, reportKey, ctxKey)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no records compared — fixture() or compareGrouping() broke silently")
	}
}

// TestConformance_ExistingFixtureSessionsMapOneToOneWithLineages covers the
// expectedSame outcome: fixture()'s two sessions (A: r1-r3, its own anchor;
// A2: r4-r5, the compaction-summary anchor) each have a single, stable
// anchor throughout — no Contract/Fork edge inside either — so report's
// grouping and ctxgraph's lineage splitting must agree exactly: one report
// session <-> one ctxgraph lineage, both ways.
func TestConformance_ExistingFixtureSessionsMapOneToOneWithLineages(t *testing.T) {
	path, _ := fixture(t)
	cmp := compareGrouping(t, []string{path})

	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sessions) != 2 {
		t.Fatalf("fixture() sessions = %d, want 2 (has the fixture changed?)", len(a.Sessions))
	}
	for _, s := range a.Sessions {
		lineages := cmp.lineagesForSession(s.ID)
		if len(lineages) != 1 {
			t.Errorf("report session %s (%d recs) maps to %d ctxgraph lineages, want exactly 1 — expectedSame violated", s.ID, len(s.Recs), len(lineages))
		}
		for idx := range lineages {
			sessions := cmp.reportSessionsForLineage(idx)
			if len(sessions) != 1 {
				t.Errorf("ctxgraph lineage %d maps back to %d report sessions, want exactly 1 (reverse of expectedSame)", idx, len(sessions))
			}
		}
	}
}

// f6AnchorGluedFixture reproduces the real s231 pattern design doc §4 F6
// documented: N pure-append turns, then a Contract edit whose new manifest
// keeps the exact same opening user message (so the anchor — and therefore
// SessKey — never changes) while the rest of the history collapses. Before
// design doc Appendix C.5 T3.1, report had no Contract-detection mechanism
// and glued both halves into one session purely because the anchor
// survived; ctxgraph's edit classifier (Classify) always split it into two
// lineages, and T3.1 made report consume that same split instead of its own
// former per-SessKey bucketing — see
// TestConformance_F6AnchorGluedLineageSplitMatchesCtxgraph below.
func f6AnchorGluedFixture(t *testing.T) (string, int) {
	t.Helper()
	at := func(i int) time.Time { return time.Date(2026, 7, 16, 15, i, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "深入调研内存涨价")
	msgs := []any{sys, u1}
	var recs []audit.Record
	const preBreakTurns = 8
	for i := 0; i < preBreakTurns; i++ {
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgs...), nil, sseText("ok")))
		msgs = append(msgs, msg("assistant", "step"))
	}
	// Contract: history collapses to just [sys v2, u1] — same opening
	// instruction survives verbatim, everything else is gone.
	recs = append(recs, mkRec(at(30), "", []any{msg("system", "sys v2"), u1}, nil, sseText("continuing")))
	return writeJSONL(t, recs), preBreakTurns + 1
}

// TestConformance_F6AnchorGluedLineageSplitMatchesCtxgraph covers what used
// to be the one whitelisted knownImprovement outcome and is now, post T3.1,
// business as usual: report splits the anchor-glued fixture into two
// sessions, one per ctxgraph lineage — no more divergence to whitelist.
// Verifies the second lineage's BrokeFrom edit is structurally a Contract
// (not asserted by trust), the two sessions' records reconcile exactly
// against the fixture's total (nothing lost, nothing double-counted), AND
// (the new capability T3.1 adds — see linkStitchedLineages) the second
// session's ContinuedFrom points back at the first, since ctxgraph resolves
// this same-SessKey Contract break as a high-confidence Stitched
// StitchCompaction (the surviving opening instruction is a 100% overlap).
func TestConformance_F6AnchorGluedLineageSplitMatchesCtxgraph(t *testing.T) {
	path, total := f6AnchorGluedFixture(t)
	cmp := compareGrouping(t, []string{path})

	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sessions) != 2 {
		t.Fatalf("report sessions = %d, want 2 (T3.1: report must now split on the same Contract edge ctxgraph does)", len(a.Sessions))
	}
	s1, s2 := a.Sessions[0], a.Sessions[1]
	if len(s1.Recs)+len(s2.Recs) != total {
		t.Fatalf("report sessions have %d+%d=%d recs, want %d total", len(s1.Recs), len(s2.Recs), len(s1.Recs)+len(s2.Recs), total)
	}
	if len(s2.Recs) != 1 {
		t.Fatalf("second report session has %d recs, want 1 (just the post-Contract record)", len(s2.Recs))
	}

	lineages1 := cmp.lineagesForSession(s1.ID)
	lineages2 := cmp.lineagesForSession(s2.ID)
	if len(lineages1) != 1 || len(lineages2) != 1 {
		t.Fatalf("sessions map to %d/%d ctxgraph lineages, want exactly 1 each", len(lineages1), len(lineages2))
	}
	var idx2 int
	for idx := range lineages2 {
		idx2 = idx
	}
	bf := cmp.lineageBroke[idx2]
	if bf == nil {
		t.Fatal("second session's lineage should carry a BrokeFrom (F6's structural signature)")
	}
	if bf.Edit.Kind != ctxgraph.Contract {
		t.Errorf("BrokeFrom.Edit.Kind = %v, want Contract", bf.Edit.Kind)
	}

	if s2.ContinuedFrom != s1.ID {
		t.Errorf("second session's ContinuedFrom = %q, want %q (linkStitchedLineages should resolve the Contract break)", s2.ContinuedFrom, s1.ID)
	}
}

// TestConformance_NoLineageSpansMultipleReportSessions is the reverse
// invariant of the F6 case above, checked across every fixture this file
// knows about: a ctxgraph lineage must never span more than one distinct
// report session. That would mean ctxgraph MERGED content report kept
// separate — the opposite failure mode from F6, and not on either
// whitelist (ctxgraph is only ever expected to split further than report,
// never coarsen it) — a real bug if it ever happens.
func TestConformance_NoLineageSpansMultipleReportSessions(t *testing.T) {
	basePath, _ := fixture(t)
	f6Path, _ := f6AnchorGluedFixture(t)
	for _, path := range []string{basePath, f6Path} {
		cmp := compareGrouping(t, []string{path})
		for idx := range cmp.lineageSessKey {
			sessions := cmp.reportSessionsForLineage(idx)
			if len(sessions) > 1 {
				t.Errorf("%s: ctxgraph lineage %d spans %d distinct report sessions (%v) — ctxgraph must never merge what report kept separate", path, idx, len(sessions), sessions)
			}
		}
	}
}
