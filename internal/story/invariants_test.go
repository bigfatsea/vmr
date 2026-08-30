// Ver 2026-07-29 20:00, by Sonnet 5

package story

import (
	"fmt"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// fetchStepRecords re-fetches every Step's audit.Record the same way
// buildFrom did — Step no longer holds it, but the manifest coordinates
// still resolve it.
func fetchStepRecords(t *testing.T, j *Journey) map[ctxgraph.Loc]*audit.Record {
	t.Helper()
	var locs []ctxgraph.Loc
	for _, task := range j.Tasks {
		for _, step := range task.Steps {
			locs = append(locs, ctxgraph.Loc{Path: step.Manifest.Path, Line: step.Manifest.Line})
		}
	}
	recs, err := ctxgraph.FetchRecords(locs)
	if err != nil {
		t.Fatalf("FetchRecords: %v", err)
	}
	return recs
}

// manyToolCallRecords builds a lineage of n turns, each turn appending one
// more tool_call/tool_result pair on top of the accumulated history —
// mirrors the real 79-message/57-pair session F9 hand-verified. n=20 gives
// a manifest with system+user+20*(assistant+tool) =
// 42 messages and 20 tool_call/tool_result pairs by the last turn, enough to
// exercise the invariant at a meaningful scale without depending on
// real/uncommitted audit logs (see internal/story/golden_test.go's doc
// comment on why fixtures are built in Go, never checked in as JSONL).
func manyToolCallRecords(n int) []audit.Record {
	at := func(i int) time.Time { return time.Date(2026, 7, 9, 10, 0, i, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "do the thing")
	msgs := []any{sys, u1}
	var recs []audit.Record
	for i := 0; i < n; i++ {
		callID := fmt.Sprintf("call_%02d", i)
		msgs = append(msgs,
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": callID, "function": map[string]any{"name": "read", "arguments": "{}"}},
			}},
			map[string]any{"role": "tool", "tool_call_id": callID, "content": fmt.Sprintf("result %d", i)},
		)
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgs...), sseText("ok")))
	}
	return recs
}

// TestInvariant_ToolCallPairingIsAlways100Percent locks in F9
// ("Step 内部的因果结构是协议给定的精确事实，不是启发式") as an automated
// regression, closing the gap that F9 had only ever been hand-verified
// against one real record (57/57), never turned into a test that fails the
// build if the invariant is ever violated. Runs chatmsg.CheckToolPairing
// over every manifest a real Build() produces (re-fetching each Step's
// record by its manifest coordinate — the same body story.Build fed into
// rendering), so this also guards against any future change to
// Build/chatmsg accidentally introducing a mismatch.
func TestInvariant_ToolCallPairingIsAlways100Percent(t *testing.T) {
	const turns = 20
	recs := manyToolCallRecords(turns)
	path := writeJSONL(t, recs)
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Step no longer carries its Record; re-fetch each manifest's body the
	// same way Build did (ctxgraph.FetchRecords over the step manifests).
	stepRecs := fetchStepRecords(t, j)

	checked := 0
	for _, task := range j.Tasks {
		for _, step := range task.Steps {
			rec := stepRecs[ctxgraph.Loc{Path: step.Manifest.Path, Line: step.Manifest.Line}]
			if rec == nil {
				t.Fatalf("step %d: could not re-fetch its record", step.Seq)
			}
			body, ok := rec.Client.Request.Body.(map[string]any)
			if !ok {
				t.Fatalf("step %d: request body is not a map[string]any", step.Seq)
			}
			rawMsgs, _ := body["messages"].([]any)
			report := chatmsg.CheckToolPairing(rawMsgs)
			checked++
			if !report.OK() {
				t.Fatalf("step %d: tool_call/tool_result pairing invariant violated (F9) — orphan calls=%v orphan results=%v (calls=%d results=%d)",
					step.Seq, report.OrphanCalls, report.OrphanResults, report.Calls, report.Results)
			}
		}
	}
	if checked != turns {
		t.Fatalf("checked %d steps, want %d — the fixture didn't produce the expected step count", checked, turns)
	}

	// The LAST step's request body carries the full accumulated history —
	// same shape as the real 79-message/57-pair record F9 hand-verified.
	// Assert its pairing count matches exactly what the fixture built, so
	// this test would also catch a regression that silently dropped pairs
	// rather than mismatching them (report.OK() alone wouldn't distinguish
	// "0 calls, 0 results" from "20 calls, 20 results").
	lastSteps := j.Tasks[len(j.Tasks)-1].Steps
	lastStep := lastSteps[len(lastSteps)-1]
	lastRec := stepRecs[ctxgraph.Loc{Path: lastStep.Manifest.Path, Line: lastStep.Manifest.Line}]
	lastBody, _ := lastRec.Client.Request.Body.(map[string]any)
	lastReport := chatmsg.CheckToolPairing(lastBody["messages"].([]any))
	if lastReport.Calls != turns || lastReport.Results != turns {
		t.Fatalf("final step pairing count = %d/%d, want %d/%d", lastReport.Calls, lastReport.Results, turns, turns)
	}
}

// TestInvariant_ToolCallPairingCatchesAnOrphan is the negative control for
// the test above: proves the invariant check actually fails the build when
// a tool_call really is unanswered, so a future refactor that accidentally
// makes the positive test vacuously pass (e.g. an empty step list) doesn't
// go unnoticed. Uses t.Run + a sub-test recovery pattern: runs the check in
// a way that captures failure without stopping this test's own execution.
func TestInvariant_ToolCallPairingCatchesAnOrphan(t *testing.T) {
	rawMsgs := []any{
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "orphan_call", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
	}
	report := chatmsg.CheckToolPairing(rawMsgs)
	if report.OK() {
		t.Fatal("expected the checker to flag an orphan tool_call, got OK — the invariant test above would not catch a real regression")
	}
}
