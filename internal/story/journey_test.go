// Ver 2026-07-28 23:20, by Sonnet 5

package story

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// writeJSONL writes recs to a fresh temp file and returns its path. The
// basename is derived from t.TempDir()'s own unique suffix (not a fixed
// "audit.jsonl") — real audit files always carry a date and never collide
// on basename (see ctxgraph's CheckPathCollisions), and a fixed name would
// make two calls within the same test collide on purpose.
func writeJSONL(t *testing.T, recs []audit.Record) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-"+filepath.Base(dir)+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mkRec(ts time.Time, trace string, msgs []any, respBody any) audit.Record {
	body := map[string]any{"model": "agent", "stream": true, "messages": msgs}
	h := map[string][]string{}
	if trace != "" {
		h["Traceparent"] = []string{"00-" + trace + "-abcdef0123456789-01"}
	}
	return audit.Record{
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai-completions", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: h, Body: body},
			Response: &audit.Message{Status: 200, Headers: map[string][]string{}, Body: respBody},
		},
	}
}

func sseText(text string) string {
	return `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}
data: [DONE]`
}

func msg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

// onlyLineage scans path and returns the single lineage found — fails the
// test if there isn't exactly one.
func onlyLineage(t *testing.T, path string) *ctxgraph.Lineage {
	t.Helper()
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 1 {
		t.Fatalf("got %d lineages, want 1", len(g.Lineages))
	}
	return g.Lineages[0]
}

func TestBuild_TaskSplittingOnNewInstruction(t *testing.T) {
	zone := time.UTC
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, zone) }
	sys := msg("system", "You are a personal assistant.")
	u1 := msg("user", "任务A：调研 X")
	a1 := map[string]any{"role": "assistant", "content": "开工"}
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "result"}
	u2 := msg("user", "任务B：现在帮我写个总结")

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("好的"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, t1}, sseText("继续"))
	r3 := mkRec(at(2), "", []any{sys, u1, a1, t1, u2}, sseText("好的开始总结"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(j.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2 (u2 opens a new task): titles=%v", len(j.Tasks), taskTitles(j))
	}
	if len(j.Tasks[0].Steps) != 2 {
		t.Errorf("task 1 steps = %d, want 2", len(j.Tasks[0].Steps))
	}
	if len(j.Tasks[1].Steps) != 1 {
		t.Errorf("task 2 steps = %d, want 1", len(j.Tasks[1].Steps))
	}
}

func taskTitles(j *Journey) []string {
	out := make([]string, len(j.Tasks))
	for i, t := range j.Tasks {
		out[i] = t.Title
	}
	return out
}

// TestBuild_NoReplyMergesRetryIntoSameTask ports
// internal/report/session_test.go's TestNoReplyMergesRetryIntoSameTask:
// OpenClaw's skip-on-memory-flush pattern (empty/NO_REPLY reply) must not
// let the next turn's genuinely-new user message open a fresh task — it's
// a retry of the instruction the parent skipped.
func TestBuild_NoReplyMergesRetryIntoSameTask(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "You are a personal assistant.")
	u1 := msg("user", "[Thu 2026-07-09 10:00 GMT+8] 任务开始：写日报")
	u2 := msg("user", "[Thu 2026-07-09 10:06 GMT+8] 继续，把日报写完")

	r1 := mkRec(at(0), "trace-a", []any{sys, u1}, sseText("NO_REPLY"))
	r2 := mkRec(at(6), "trace-a", []any{sys, u1, u2}, sseText("好的，日报如下"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.OpenClawAware, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(j.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1 (NO_REPLY parent should keep the retry in the same task): titles=%v", len(j.Tasks), taskTitles(j))
	}
	if len(j.Tasks[0].Steps) != 2 {
		t.Fatalf("task steps = %d, want 2", len(j.Tasks[0].Steps))
	}
	if !j.Tasks[0].Steps[0].NoReply {
		t.Error("first step should be flagged NoReply")
	}
	if j.Tasks[0].Steps[1].NoReply {
		t.Error("second step should not be flagged NoReply")
	}
}

func TestBuild_TraceChangeAlwaysOpensNewTask(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "first")
	a1 := map[string]any{"role": "assistant", "content": "ack"}

	r1 := mkRec(at(0), "aaaa1111aaaa1111aaaa1111aaaa1111", []any{sys, u1}, sseText("ok"))
	// Same content shape continuing (no new real user instruction), but a
	// different trace-id — a genuinely different client request chain,
	// which must open a new task even without new content. Note: real
	// trace-ids are hyphen-free hex (Traceparent = "version-traceid-spanid-
	// flags"); a fake id containing its own hyphen would corrupt the
	// split-by-hyphen parsing, which is exactly why this uses hex-looking
	// strings instead of "trace-a"/"trace-b".
	r2 := mkRec(at(1), "bbbb2222bbbb2222bbbb2222bbbb2222", []any{sys, u1, a1}, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(j.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2 (trace-id change forces a new task)", len(j.Tasks))
	}
}

func TestBuild_EventStreamDeduplicatesAcrossSteps(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "task")
	a1 := map[string]any{"role": "assistant", "content": "step one"}
	a2 := map[string]any{"role": "assistant", "content": "step two"}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1}, sseText("ok2"))
	r3 := mkRec(at(2), "", []any{sys, u1, a1, a2}, sseText("ok3"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Total distinct events: sys(system, folded but still 1 event) + u1 + a1 + a2 = 4.
	if len(j.Events) != 4 {
		t.Fatalf("Events = %d, want 4 (deduplicated across steps): %v", len(j.Events), eventTexts(j))
	}
	// Step 1 introduces sys+u1 (2 new events); step 2 introduces only a1;
	// step 3 introduces only a2 — u1 must NOT reappear as new in step 2/3.
	steps := allSteps(j)
	if len(steps[0].NewEvents) != 2 {
		t.Errorf("step 1 NewEvents = %d, want 2", len(steps[0].NewEvents))
	}
	if len(steps[1].NewEvents) != 1 {
		t.Errorf("step 2 NewEvents = %d, want 1", len(steps[1].NewEvents))
	}
	if len(steps[2].NewEvents) != 1 {
		t.Errorf("step 3 NewEvents = %d, want 1", len(steps[2].NewEvents))
	}
}

func eventTexts(j *Journey) []string {
	out := make([]string, len(j.Events))
	for i, e := range j.Events {
		out[i] = e.Msg.Text
	}
	return out
}

func allSteps(j *Journey) []*Step {
	var out []*Step
	for _, t := range j.Tasks {
		out = append(out, t.Steps...)
	}
	return out
}

func TestBuild_TitleFromEarliestInstruction(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "帮我调研一下 A 股新股打新收益")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("好的"))
	path := writeJSONL(t, []audit.Record{r1, mkRec(at(1), "", []any{sys, u1, msg("user", "继续")}, sseText("ok"))})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if j.Title != "帮我调研一下 A 股新股打新收益" {
		t.Errorf("Title = %q, want the opening instruction", j.Title)
	}
}

func TestDeriveID_StableAcrossIndependentScans(t *testing.T) {
	at := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	sys := msg("system", "sys")
	u1 := msg("user", "same content")
	r1 := mkRec(at, "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at.Add(time.Minute), "", []any{sys, u1, msg("user", "more")}, sseText("ok2"))

	path1 := writeJSONL(t, []audit.Record{r1, r2})
	path2 := writeJSONL(t, []audit.Record{r1, r2}) // separate file, separate Scan call

	l1 := onlyLineage(t, path1)
	l2 := onlyLineage(t, path2)
	j1, err := Build(l1, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	j2, err := Build(l2, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	if j1.ID != j2.ID {
		t.Errorf("ID not stable: %q vs %q", j1.ID, j2.ID)
	}
}

func TestListCandidates_ExcludesSingleRequestLineages(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	// Single-shot "session" (e.g. a heartbeat poll): exactly one request, no
	// task narrative possible.
	single := mkRec(at(0), "", []any{msg("system", "sys"), msg("user", "[heartbeat]")}, sseText("ok"))
	// Real multi-turn session.
	multi1 := mkRec(at(1), "", []any{msg("system", "sys2"), msg("user", "real task")}, sseText("ok"))
	multi2 := mkRec(at(2), "", []any{msg("system", "sys2"), msg("user", "real task"), msg("assistant", "working")}, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{single, multi1, multi2})
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	cands := ListCandidates(g)
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1 (single-request lineage excluded)", len(cands))
	}
	if len(cands[0].Manifests) != 2 {
		t.Errorf("surviving candidate has %d manifests, want 2", len(cands[0].Manifests))
	}
}

func TestIsPartialHead_ColdStartNeverPartial(t *testing.T) {
	at := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	// Cold-start shape (<=2 keys): never partial, regardless of position.
	coldStart := mkRec(at, "", []any{msg("system", "sys"), msg("user", "hi")}, sseText("ok"))
	path := writeJSONL(t, []audit.Record{coldStart, mkRec(at.Add(time.Minute), "", []any{msg("system", "sys"), msg("user", "hi"), msg("assistant", "a")}, sseText("ok"))})
	l := onlyLineage(t, path)
	if IsPartialHead([]*ctxgraph.Lineage{l}, path) {
		t.Error("cold-start-shaped root should never be flagged partial")
	}
}

func TestIsPartialHead_TrueForMultiKeyRootEarlyInFirstFile(t *testing.T) {
	at := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	// A root manifest with >2 non-system keys (3 Keys for 4 total messages),
	// at line 1 of firstPath — a mid-conversation continuation whose true
	// beginning likely sits outside the loaded range.
	sys := msg("system", "sys")
	u1 := msg("user", "continue from previous")
	a1 := map[string]any{"role": "assistant", "content": "working"}
	u2 := msg("user", "also check this")
	r1 := mkRec(at, "", []any{sys, u1, a1, u2}, sseText("ok"))
	r2 := mkRec(at.Add(time.Minute), "", []any{sys, u1, a1, u2, msg("user", "more")}, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	if !IsPartialHead([]*ctxgraph.Lineage{l}, path) {
		t.Error("root with 3 Keys at line=1 of firstPath should be flagged partial")
	}
	// Same lineage with a different firstPath: not the earliest file, so
	// NOT flagged — the root really IS the start of what we can see.
	if IsPartialHead([]*ctxgraph.Lineage{l}, "/some/other/file.jsonl") {
		t.Error("root should not be flagged partial when its file is not firstPath")
	}
}

// TestBuildAll_MatchesIndividualBuild locks in that batching (BuildAll)
// produces byte-identical output to calling Build once per lineage — the
// whole point of BuildAll is an I/O optimization (one shared FetchRecords
// call across every lineage instead of one per lineage), it must not change
// what gets rendered. The two lineages live in separate source files so
// this actually exercises FetchRecords' cross-file grouping, not just
// cross-lineage-in-one-file.
func TestBuildAll_MatchesIndividualBuild(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")

	uA := msg("user", "lineage A opening")
	rA1 := mkRec(at(0), "", []any{sys, uA}, sseText("a1"))
	rA2 := mkRec(at(1), "", []any{sys, uA, msg("assistant", "reply A")}, sseText("a2"))

	uB := msg("user", "lineage B opening")
	rB1 := mkRec(at(10), "", []any{sys, uB}, sseText("b1"))
	rB2 := mkRec(at(11), "", []any{sys, uB, msg("assistant", "reply B")}, sseText("b2"))

	pathA := writeJSONL(t, []audit.Record{rA1, rA2})
	pathB := writeJSONL(t, []audit.Record{rB1, rB2})

	g, err := ctxgraph.Scan([]string{pathA, pathB})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}

	chains := [][]*ctxgraph.Lineage{{g.Lineages[0]}, {g.Lineages[1]}}
	got, err := BuildAll(chains, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("BuildAll returned %d journeys, want 2", len(got))
	}
	for i, l := range g.Lineages {
		want, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build[%d]: %v", i, err)
		}
		if gotMD, wantMD := RenderMarkdown(got[i], ComputeMetrics(got[i]), ComputeFindings(got[i], i18n.EN), i18n.EN, false, true, nil), RenderMarkdown(want, ComputeMetrics(want), ComputeFindings(want, i18n.EN), i18n.EN, false, true, nil); gotMD != wantMD {
			t.Errorf("BuildAll[%d] rendered differently than Build:\n=== BuildAll ===\n%s\n=== Build ===\n%s", i, gotMD, wantMD)
		}
	}
}

// TestBuildAll_EmptyLineageErrors covers BuildAll's defensive guard: same
// contract as Build itself (errEmptyLineage), just checked per-chain inside
// the batch instead of once up front.
func TestBuildAll_EmptyLineageErrors(t *testing.T) {
	if _, err := BuildAll([][]*ctxgraph.Lineage{{{}}}, taskseg.Generic, i18n.EN); err == nil {
		t.Error("BuildAll with an empty lineage should return an error")
	}
}

// TestBuildChain_NilProfileErrors and TestBuildAll_NilProfileErrors pin the
// fail-fast guard added for a nil taskseg.Profile: without it, a nil prof
// only surfaces once buildFrom calls prof.RealUserText, which for BuildAll
// happens inside a worker goroutine with no recover() — an unrecovered
// panic there kills the whole process instead of returning a clean error.
func TestBuildChain_NilProfileErrors(t *testing.T) {
	if _, err := BuildChain([]*ctxgraph.Lineage{{}}, nil, i18n.EN); err == nil {
		t.Error("BuildChain with a nil Profile should return an error, not panic")
	}
}

func TestBuildAll_NilProfileErrors(t *testing.T) {
	if _, err := BuildAll([][]*ctxgraph.Lineage{{{}}}, nil, i18n.EN); err == nil {
		t.Error("BuildAll with a nil Profile should return an error, not panic")
	}
}

// TestSortByRootThenTime_TieBreaksOnRootHash covers the tie-break path: two
// lineages whose root manifests share the exact same timestamp (should not
// happen in practice, but must still sort deterministically across runs
// rather than depending on input slice order) fall back to comparing
// RootHash strings.
func TestSortByRootThenTime_TieBreaksOnRootHash(t *testing.T) {
	tie := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	rA := mkRec(tie, "", []any{msg("system", "sys"), msg("user", "task A")}, sseText("ok"))
	rB := mkRec(tie, "", []any{msg("system", "sys"), msg("user", "task B")}, sseText("ok"))

	pathA := writeJSONL(t, []audit.Record{rA})
	pathB := writeJSONL(t, []audit.Record{rB})
	gA, err := ctxgraph.Scan([]string{pathA})
	if err != nil {
		t.Fatal(err)
	}
	gB, err := ctxgraph.Scan([]string{pathB})
	if err != nil {
		t.Fatal(err)
	}
	lA, lB := gA.Lineages[0], gB.Lineages[0]
	if !lA.Manifests[0].TS.Equal(lB.Manifests[0].TS) {
		t.Fatal("fixture bug: both lineages must share the exact same root timestamp")
	}

	want := []*ctxgraph.Lineage{lA, lB}
	if lB.RootHash().String() < lA.RootHash().String() {
		want = []*ctxgraph.Lineage{lB, lA}
	}

	for _, in := range [][]*ctxgraph.Lineage{{lA, lB}, {lB, lA}} {
		got := append([]*ctxgraph.Lineage(nil), in...)
		sortByRootThenTime(got)
		if got[0] != want[0] || got[1] != want[1] {
			t.Errorf("sortByRootThenTime(%v) tie-break not deterministic/RootHash-ordered: got %v, want %v", in, got, want)
		}
	}
}
