// Ver 2026-07-30 12:30, by Sonnet 5

package story

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/story/profile"
)

// TestIDMatchesDeriveID locks in that the public ID entry point (used by
// cmd_story.go's listing/-journey/-compare-a/-compare-b resolution, all
// outside this package) returns exactly what deriveID computes — a
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

// TestPreviewTitleAndPreviewTitlesAgree covers both the single-chain
// PreviewTitle path and the batched PreviewTitles path (the one
// cmd_story.go's listing actually uses) against the same fixture — both
// must return the fixture's own real opening instruction, and must agree
// with each other.
func TestPreviewTitleAndPreviewTitlesAgree(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "调研一下 A 股新股打新收益")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("开工"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "done")}, sseText("完成"))
	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	chain := []*ctxgraph.Lineage{l}

	single, err := PreviewTitle(chain, profile.OpenClawAware)
	if err != nil {
		t.Fatalf("PreviewTitle: %v", err)
	}
	if single != "调研一下 A 股新股打新收益" {
		t.Errorf("PreviewTitle = %q, want the fixture's opening instruction", single)
	}

	batched, err := PreviewTitles([][]*ctxgraph.Lineage{chain}, profile.OpenClawAware)
	if err != nil {
		t.Fatalf("PreviewTitles: %v", err)
	}
	if got := batched[l]; got != single {
		t.Errorf("PreviewTitles[tail] = %q, want it to agree with PreviewTitle = %q", got, single)
	}
}

// TestTitleFromRecordFallbacks covers titleFromRecord's two non-happy
// paths: a nil record (FetchRecords couldn't resolve the location) and a
// record whose body has no real user instruction at all.
func TestTitleFromRecordFallbacks(t *testing.T) {
	if got := titleFromRecord(nil, profile.Generic); got != "(无法读取)" {
		t.Errorf("titleFromRecord(nil) = %q, want (无法读取)", got)
	}
	rec := mkRec(time.Now(), "", []any{msg("system", "sys only")}, sseText("ok"))
	if got := titleFromRecord(&rec, profile.Generic); got != "(无标题)" {
		t.Errorf("titleFromRecord(no real user msg) = %q, want (无标题)", got)
	}
}
