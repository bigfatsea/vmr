// Ver 2026-07-30 12:30, by Sonnet 5

package story

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// TestIDMatchesDeriveID locks in that the public ID entry point (used by
// cmd_story.go's listing/-journey/-compare resolution, all outside this
// package) returns exactly what deriveID computes — a
// same-package test that would catch a future divergence directly, instead
// of only through cmd/vmr's own end-to-end coverage.
func TestIDMatchesDeriveID(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "调研一下")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("开工"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "done")}, sseText("完成"))
	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	chain := []*ctxgraph.Lineage{l}

	if got, want := ID(chain), deriveID(chain); got != want {
		t.Errorf("ID(chain) = %q, want deriveID(chain) = %q", got, want)
	}
}

// TestPreviewTitles_NilProfileErrors pins the fail-fast guard for a nil
// taskseg.Profile — see the same-named guards in journey_test.go for why
// this matters more than a nicer error message (titleFromRecord would
// otherwise panic on prof.RealUserText).
func TestPreviewTitles_NilProfileErrors(t *testing.T) {
	if _, err := PreviewTitles([][]*ctxgraph.Lineage{{{}}}, nil, i18n.EN); err == nil {
		t.Error("PreviewTitles with a nil Profile should return an error, not panic")
	}
}

// TestPreviewTitles_ReturnsRealOpeningInstruction covers the batched path
// (the one cmd_story.go's listing actually uses) against a fixture whose
// real opening instruction is known — the result must be that instruction,
// keyed by the chain's tail lineage.
func TestPreviewTitles_ReturnsRealOpeningInstruction(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "调研一下 A 股新股打新收益")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("开工"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "done")}, sseText("完成"))
	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	chain := []*ctxgraph.Lineage{l}

	batched, err := PreviewTitles([][]*ctxgraph.Lineage{chain}, taskseg.OpenClawAware, i18n.EN)
	if err != nil {
		t.Fatalf("PreviewTitles: %v", err)
	}
	if got := batched[l]; got != "调研一下 A 股新股打新收益" {
		t.Errorf("PreviewTitles[tail] = %q, want the fixture's opening instruction", got)
	}
}

// TestTitleFromRecordFallbacks covers titleFromRecord's two non-happy
// paths: a nil record (FetchRecords couldn't resolve the location) and a
// record whose body has no real user instruction at all.
func TestTitleFromRecordFallbacks(t *testing.T) {
	if got := titleFromRecord(nil, taskseg.Generic, i18n.EN); got != "(unreadable)" {
		t.Errorf("titleFromRecord(nil) = %q, want (unreadable)", got)
	}
	rec := mkRec(time.Now(), "", []any{msg("system", "sys only")}, sseText("ok"))
	if got := titleFromRecord(&rec, taskseg.Generic, i18n.EN); got != "(untitled)" {
		t.Errorf("titleFromRecord(no real user msg) = %q, want (untitled)", got)
	}
}
