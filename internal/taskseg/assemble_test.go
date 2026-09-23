// Ver 2026-09-24 15:25, by dev

package taskseg

import (
	"crypto/md5"
	"testing"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

func hashStr(s string) ctxgraph.Hash {
	return md5.Sum([]byte(s))
}

func TestIsCompaction(t *testing.T) {
	// 1. Summarization in system prompt
	body1 := map[string]any{"messages": []any{}}
	msgs1 := []chatmsg.Message{{Role: "system", Text: "You are a context summarization assistant."}}
	if !IsCompaction(body1, 1, msgs1, "trace1") {
		t.Error("summarization system prompt should be compaction regardless of tools/trace")
	}

	// 2. Three-signal: no tools + max_completion_tokens + empty traceID
	body2 := map[string]any{"max_completion_tokens": 1000}
	msgs2 := []chatmsg.Message{{Role: "user", Text: "Summarize"}}
	if !IsCompaction(body2, 0, msgs2, "") {
		t.Error("no-tools + max_completion_tokens + empty trace should be compaction")
	}

	// 3. Negative: has tools declared
	body3 := map[string]any{
		"max_completion_tokens": 1000,
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "fetch"}},
		},
	}
	if IsCompaction(body3, 0, msgs2, "") {
		t.Error("tools declared should not match three-signal compaction")
	}

	// 4. Negative: has non-empty trace ID
	if IsCompaction(body2, 0, msgs2, "trace-123") {
		t.Error("non-empty traceID should not match three-signal compaction")
	}
}

// TestSegment_CompactionSkippedAsPredecessor pins the core canonical rule: a
// compaction record sitting between two ordinary records is never the later
// record's predecessor — the later record classifies against the most recent
// non-compaction record, the same way report's session grouping (which
// excludes compaction records) always computed it.
func TestSegment_CompactionSkippedAsPredecessor(t *testing.T) {
	// r1: root turn [u1, a1]
	m1 := &ctxgraph.Manifest{
		LeadSys: 1,
		HasSys:  true,
		SysHash: hashStr("sys"),
		Keys:    []ctxgraph.Hash{hashStr("u1"), hashStr("a1")},
	}
	ru1 := RealUsers{1: "First instruction"}

	// comp: compaction record [u1, a1, feed] — shares more with r2 than r1
	// does, which is exactly what used to make journey's positional
	// predecessor produce a different delta/instruction.
	mComp := &ctxgraph.Manifest{
		LeadSys: 1,
		HasSys:  true,
		SysHash: hashStr("comp_sys"),
		Keys:    []ctxgraph.Hash{hashStr("u1"), hashStr("a1"), hashStr("feed")},
	}

	// r2: continuation [u1, a1, feed, tool]
	m2 := &ctxgraph.Manifest{
		LeadSys: 1,
		HasSys:  true,
		SysHash: hashStr("sys"),
		Keys:    []ctxgraph.Hash{hashStr("u1"), hashStr("a1"), hashStr("feed"), hashStr("tool")},
	}
	ru2 := RealUsers{4: "New task instruction"}

	boundaries := Segment([]StepInput{
		{Manifest: m1, RealUsers: ru1, TotalMsgs: 3},
		{Manifest: mComp, RealUsers: RealUsers{}, TotalMsgs: 4, Compaction: true},
		{Manifest: m2, RealUsers: ru2, TotalMsgs: 5},
	})
	if len(boundaries) != 3 {
		t.Fatalf("len(boundaries) = %d, want 3", len(boundaries))
	}

	// r1: root
	if boundaries[0].Parent != -1 || !boundaries[0].NewTask || boundaries[0].DeltaStart != 0 {
		t.Errorf("r1 boundary: %+v", boundaries[0])
	}
	if boundaries[0].Instruction != "First instruction" {
		t.Errorf("r1 Instruction = %q, want %q", boundaries[0].Instruction, "First instruction")
	}

	// comp: predecessor is r1 (idx 0), and it does NOT update the tracking
	// predecessor state.
	if boundaries[1].Parent != 0 {
		t.Errorf("comp Parent = %d, want 0", boundaries[1].Parent)
	}

	// r2: predecessor must be r1 (idx 0), NOT comp (idx 1) — LCP 2 against
	// r1, so DeltaStart = LeadSys(1) + 2 = 3 even though a positional
	// comparison against comp would give LCP 3 / DeltaStart 4.
	if boundaries[2].Parent != 0 {
		t.Fatalf("r2 Parent = %d, want 0 (r1, skipping compaction)", boundaries[2].Parent)
	}
	if boundaries[2].DeltaStart != 3 {
		t.Errorf("r2 DeltaStart = %d, want 3 (relative to r1, not to comp)", boundaries[2].DeltaStart)
	}
	if !boundaries[2].NewTask {
		t.Errorf("r2 NewTask = false, want true")
	}
	if boundaries[2].Instruction != "New task instruction" {
		t.Errorf("r2 Instruction = %q, want %q", boundaries[2].Instruction, "New task instruction")
	}
	if boundaries[2].SysChanged {
		t.Errorf("r2 SysChanged should be false against r1 (same SysHash)")
	}
}

// TestSegment_LineageBoundaryResetsPredecessor pins that a stitched boundary
// never carries the structural comparison across the lineage break: the
// first record of the new lineage gets opening semantics, and the next
// intra-lineage record compares against it.
func TestSegment_LineageBoundaryResetsPredecessor(t *testing.T) {
	mA2 := &ctxgraph.Manifest{LeadSys: 1, HasSys: true, SysHash: hashStr("sys"),
		Keys: []ctxgraph.Hash{hashStr("a1"), hashStr("a2")}}
	mB1 := &ctxgraph.Manifest{LeadSys: 1, HasSys: true, SysHash: hashStr("sys"),
		Keys: []ctxgraph.Hash{hashStr("b1")}}
	mB2 := &ctxgraph.Manifest{LeadSys: 1, HasSys: true, SysHash: hashStr("sys"),
		Keys: []ctxgraph.Hash{hashStr("b1"), hashStr("b2")}}

	boundaries := Segment([]StepInput{
		{Manifest: mA2, RealUsers: RealUsers{1: "task A"}, TotalMsgs: 3},
		{Manifest: mB1, RealUsers: RealUsers{1: "task B"}, TotalMsgs: 2, StitchBoundary: true, StitchNewTask: false},
		{Manifest: mB2, RealUsers: RealUsers{}, TotalMsgs: 3},
	})

	// B1: opening of the stitched-in lineage — no structural predecessor,
	// not a new task (nothing genuinely new bridged the stitch).
	if boundaries[1].Parent != -1 || boundaries[1].DeltaStart != 0 {
		t.Errorf("B1 boundary: %+v", boundaries[1])
	}
	if boundaries[1].NewTask || boundaries[1].HumanInitiated {
		t.Errorf("B1 NewTask/HumanInitiated = %v/%v, want false/false (stitch with nothing new)",
			boundaries[1].NewTask, boundaries[1].HumanInitiated)
	}
	if boundaries[1].Instruction != "" {
		t.Errorf("B1 Instruction = %q, want empty at a stitch boundary", boundaries[1].Instruction)
	}

	// B2: predecessor is B1 (within the new lineage), not anything from A.
	if boundaries[2].Parent != 1 {
		t.Errorf("B2 Parent = %d, want 1 (B1)", boundaries[2].Parent)
	}
	if boundaries[2].DeltaStart != 2 { // LeadSys 1 + LCP 1
		t.Errorf("B2 DeltaStart = %d, want 2", boundaries[2].DeltaStart)
	}
}

// TestSegment_PredMatchesCommit pins the two-phase contract: Pred's
// predecessor facts must equal what Commit later derives, so a caller that
// builds its real-user index from Pred's deltaStart before Commit gets a
// boundary consistent with that index.
func TestSegment_PredMatchesCommit(t *testing.T) {
	m1 := &ctxgraph.Manifest{LeadSys: 1, HasSys: true, SysHash: hashStr("s"),
		Keys: []ctxgraph.Hash{hashStr("u1")}}
	m2 := &ctxgraph.Manifest{LeadSys: 1, HasSys: true, SysHash: hashStr("s"),
		Keys: []ctxgraph.Hash{hashStr("u1"), hashStr("u2")}}

	seg := NewSegmenter()
	seg.Commit(StepInput{Manifest: m1, RealUsers: RealUsers{1: "first"}, TotalMsgs: 2})

	prev, deltaStart, sysChanged := seg.Pred(m2, false)
	if prev != m1 || deltaStart != 2 || sysChanged {
		t.Fatalf("Pred = (%v, %d, %v), want (m1, 2, false)", prev != nil, deltaStart, sysChanged)
	}
	b := seg.Commit(StepInput{Manifest: m2, RealUsers: RealUsers{2: "second"}, TotalMsgs: 3})
	if b.DeltaStart != deltaStart || b.Parent != 0 || !b.NewTask {
		t.Errorf("Commit boundary %+v inconsistent with Pred (deltaStart=%d)", b, deltaStart)
	}
}
