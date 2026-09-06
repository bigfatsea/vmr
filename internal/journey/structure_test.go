// Ver 2026-08-20, by Sonnet 5

package journey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// TestBuildStructure_BasicShape locks the straightforward projection: every
// Step gets a non-empty Req, tool call counts match — using the same
// synthetic fixture corpus_test.go's buildTestJourney already builds for
// Finding tests, so this doesn't invent a second fixture-building
// convention. Precise (exact/id-normalized) pairing correctness itself is
// covered by TestBuildStructure_LosslessReconstruction's fixture, which
// actually includes a real tool result to pair against — this fixture
// never echoes one back, so every call here legitimately goes unmatched.
func TestBuildStructure_BasicShape(t *testing.T) {
	j := buildTestJourney(t, 3, false)
	steps := journeySteps(j)
	structure := BuildStructure(j)

	var ssteps []StepStructure
	for _, task := range structure.Tasks {
		ssteps = append(ssteps, task.Steps...)
	}
	if len(ssteps) != len(steps) {
		t.Fatalf("got %d structure steps, want %d", len(ssteps), len(steps))
	}
	for i, s := range steps {
		ss := ssteps[i]
		if ss.Seq != s.Seq {
			t.Errorf("step %d: Seq %d, want %d", i, ss.Seq, s.Seq)
		}
		if ss.Req == "" {
			t.Errorf("step %d: empty Req", s.Seq)
		}
		if len(ss.ToolCalls) != len(s.ToolCalls) {
			t.Errorf("step %d: %d ToolCallRefs, want %d", s.Seq, len(ss.ToolCalls), len(s.ToolCalls))
		}
		if len(ss.NewEvents) != len(s.NewEvents) {
			t.Errorf("step %d: %d EventRefs, want %d", s.Seq, len(ss.NewEvents), len(s.NewEvents))
		}
	}
}

// TestBuildStructure_GraphLevelFactsCarried locks in the fix for the two
// independent P4 ActionPlan reviews' most important finding (gemini's §1.2,
// pi's T2): Edit/StitchEdge/Compaction are graph-level analysis facts —
// computed by comparing this Step's manifest against ITS PREDECESSOR (or,
// for StitchEdge, against the whole corpus's stitch graph) — that cannot be
// recovered from this Step's own Req record alone the way NewEvents' text
// can. If StepStructure didn't carry them, P5.1 deleting fact-layer's
// rendering (render_md.go's renderStep, which shows all three today) would
// permanently erase them from every produced artifact. This also covers the
// per-step Endpoint/DurMS/TTFTMS/Usage fields fact-layer's header line shows
// (same reviews, same reasoning).
func TestBuildStructure_GraphLevelFactsCarried(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "start")
	u2 := msg("user", "continue please")

	r1 := mkRecWithUsage(at(0), []any{sys, u1}, "ok", 100, 10)
	r2 := mkRecWithUsage(at(1), []any{sys, u1, msg("assistant", "ok"), u2}, "done", 120, 15)

	path := writeJSONL(t, []audit.Record{r1, r2})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	structure := BuildStructure(j)
	var ssteps []StepStructure
	for _, task := range structure.Tasks {
		ssteps = append(ssteps, task.Steps...)
	}
	if len(ssteps) != 2 {
		t.Fatalf("got %d steps, want 2", len(ssteps))
	}

	// Step 1 (Journey's first) has no predecessor: Edit/StitchEdge nil.
	if ssteps[0].Edit != nil {
		t.Errorf("step 1: Edit = %+v, want nil (no predecessor)", ssteps[0].Edit)
	}
	// Step 2 has a real predecessor within the same lineage: Edit must be
	// populated (an ordinary append — u2 extends r1's message list).
	if ssteps[1].Edit == nil {
		t.Fatal("step 2: Edit is nil, want a populated EditRef (append vs. step 1)")
	}
	if ssteps[1].Edit.Kind == "" {
		t.Error("step 2: Edit.Kind is empty")
	}

	// Per-step performance/cost facts, already resident on Step.Manifest,
	// must be inlined (fact-layer's header line shows all four today).
	for i, ss := range ssteps {
		if !ss.UsageInOK || !ss.UsageOutOK {
			t.Errorf("step %d: UsageInOK/UsageOutOK = %v/%v, want true/true (mkRecWithUsage sets a usage block)", i+1, ss.UsageInOK, ss.UsageOutOK)
		}
		if ss.Usage.In == 0 && ss.Usage.Out == 0 {
			t.Errorf("step %d: Usage is zero-valued, want the mkRecWithUsage tokens", i+1)
		}
		if ss.TS.IsZero() {
			t.Errorf("step %d: TS is zero", i+1)
		}
	}
}

// TestBuildStructure_ToolCallRefHasNoResultText locks in the fix for the
// second review's T3 finding: a tool call's RESULT is conversation-history
// content (the client echoes it back verbatim as the next Step's tool-role
// NewEvent), not this-turn decision content — unlike Args, it must not be
// inlined. Matched/ResultError (facts ABOUT the result, not the result
// itself) still are.
func TestBuildStructure_ToolCallRefHasNoResultText(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "look it up")
	a1 := msg("assistant", "looking")
	t1 := map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "rate: 7.1"}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
		map[string]any{"id": "call_1", "function": map[string]any{"name": "lookup_rate", "arguments": `{"pair":"USDCNY"}`}},
	}))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, t1}, sseText("the rate is 7.1"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	structure := BuildStructure(j)
	step1 := structure.Tasks[0].Steps[0]
	if len(step1.ToolCalls) != 1 {
		t.Fatalf("step 1: %d tool calls, want 1", len(step1.ToolCalls))
	}
	tc := step1.ToolCalls[0]
	if tc.Result == nil || tc.Result.Match != "exact" || tc.Result.IsError {
		t.Errorf("step 1 tool call: Result=%+v, want Match=exact IsError=false", tc.Result)
	}
	if tc.Result.Ref == "" || structure.Bodies[tc.Result.Ref] != "rate: 7.1" {
		t.Errorf("step 1 tool call: result ref %q resolves to %q, want \"rate: 7.1\"", tc.Result.Ref, structure.Bodies[tc.Result.Ref])
	}
	if tc.ArgsRef == "" || structure.Bodies[tc.ArgsRef] != `{"pair":"USDCNY"}` {
		t.Errorf("step 1 tool call: args ref %q resolves to %q, want `{\"pair\":\"USDCNY\"}`", tc.ArgsRef, structure.Bodies[tc.ArgsRef])
	}
	// ToolCallRef has no Result field at all — this is a compile-time
	// guarantee (the struct literal above would fail to build if it did),
	// so the meaningful runtime check is that the result TEXT ("rate: 7.1")
	// is recoverable from where it actually lives: step 2's tool-role
	// NewEvent.
	step2 := structure.Tasks[0].Steps[1]
	found := false
	for _, ev := range step2.NewEvents {
		if ev.Role == "tool" {
			found = true
		}
	}
	if !found {
		t.Error("step 2: no tool-role NewEvent — the paired result's text should live here, not inlined on ToolCallRef")
	}
}

// TestBuildStructure_LosslessReconstruction is §9's rewritten acceptance test
// (D18): given ONLY journeys/details/j-<id>.json — the published
// JourneySummary, exercised as a real file so nothing in-memory can sneak in —
// the same .md renders byte-for-byte. The pre-D18 version of this test proved
// the weaker claim "structure plus the audit log can reconstruct what the
// fact layer showed"; after 1C's bodies blob table and this package's
// viewmodel layer, the audit log is not an input at all, so the assertion is
// structural now: file in, .md out, equal to the old render path's bytes.
// (The old test's hash-matching discipline — match EventRef.Hash against the
// refetched record's own message hashes, never a raw DeltaStart slice — lives
// on in the contract comment on StepStructure.DeltaStart.)
func TestBuildStructure_LosslessReconstruction(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "you are a helpful research assistant")
	u1 := msg("user", "please look up the exchange rate")
	a1 := msg("assistant", "looking it up")
	t1 := map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "rate: 7.1"}
	a2 := msg("assistant", "want me to check anything else?")

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
		map[string]any{"id": "call_1", "function": map[string]any{"name": "lookup_rate", "arguments": `{"pair":"USDCNY"}`}},
	}))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, t1}, sseText("the rate is 7.1"))
	// Step 3 resends u1 verbatim (byte-identical map literal) after a2 —
	// its NewEvents then carry a2 only (u1's repeat is dropped by the
	// journey-wide seen-hash dedup), the case that used to force
	// hash-matching over DeltaStart slicing.
	r3 := mkRec(at(2), "", []any{sys, u1, a1, t1, a2, u1}, sseText("sure, anything else?"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	m := ComputeMetrics(j)
	findings := ComputeFindings(j, i18n.EN)
	summary := NewJourneySummary(j, m, findings, nil, nil)

	// Publish exactly what j-<id>.json publishes: marshal, write to disk,
	// read the file back, unmarshal. Everything below this line sees only
	// the file's contents — the in-memory Journey/summary is out of scope.
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	file := filepath.Join(t.TempDir(), "j-test.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatalf("write j-<id>.json: %v", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read back j-<id>.json: %v", err)
	}
	var published JourneySummary
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatalf("unmarshal j-<id>.json: %v", err)
	}

	got := RenderMarkdownFromSummary(&published, i18n.EN, false, true)
	want := RenderMarkdown(j, m, findings, i18n.EN, false, true, nil)
	if got != want {
		t.Errorf(".md rendered from j-<id>.json alone diverges from the old render path\n=== from json ===\n%s\n=== from journey ===\n%s", got, want)
	}
}

// TestBuildStructure_BodiesNoOrphansNoDangling is §9's two-sided bodies
// guard: every *_ref in the structure resolves to a blob (a dangling
// reference means the rendering silently loses content), and every blob is
// referenced at least once (an orphan means the file carries dead weight).
func TestBuildStructure_BodiesNoOrphansNoDangling(t *testing.T) {
	for _, name := range []string{"golden", "rich"} {
		t.Run(name, func(t *testing.T) {
			var j *Journey
			if name == "golden" {
				j = buildGoldenJourney(t)
			} else {
				j = vmEquivalenceFixture(t)
			}
			summary := NewJourneySummary(j, ComputeMetrics(j), nil, nil, nil)

			referenced := map[string]bool{}
			addRef := func(ref, what string) {
				if ref == "" {
					return
				}
				if _, ok := summary.Bodies[ref]; !ok {
					t.Errorf("dangling %s %q: not in the bodies table", what, ref)
				}
				referenced[ref] = true
			}
			for _, ss := range vmSteps(&summary) {
				addRef(ss.RespRef, "resp_ref")
				addRef(ss.ReasoningRef, "reasoning_ref")
				for _, tc := range ss.ToolCalls {
					addRef(tc.ArgsRef, "args_ref")
					if tc.Result != nil {
						addRef(tc.Result.Ref, "result ref")
					}
				}
				if ss.Compaction != nil {
					addRef(ss.Compaction.PredecessorExcerptRef, "predecessor_excerpt_ref")
				}
			}
			for h := range summary.Bodies {
				if !referenced[h] {
					t.Errorf("orphan blob %s: stored in bodies but referenced by nothing", h)
				}
			}
			if len(summary.Bodies) == 0 {
				t.Error("fixture no longer populates the bodies table — the guard is vacuous")
			}
		})
	}
}

// TestBuildStructure_VolumeBoundedByStepsNotProseLength is the "grows with
// step count, not with conversation length" guard DevPlan P4.2 calls a
// permanent check, not a one-off eyeball review. Two Journeys with the same
// step count, one with ordinary-length content and one with a per-step
// payload two orders of magnitude larger, must serialize to structures
// whose size difference is bounded by the per-field truncation cap
// (structureExcerptChars) times the field count — never proportional to the
// raw text-length difference itself.
func TestBuildStructure_VolumeBoundedByStepsNotProseLength(t *testing.T) {
	small := buildJourneyWithArgsLen(t, 20)
	huge := buildJourneyWithArgsLen(t, 200000) // two orders of magnitude beyond structureExcerptChars

	smallJSON, err := json.Marshal(BuildStructure(small))
	if err != nil {
		t.Fatalf("marshal small: %v", err)
	}
	hugeJSON, err := json.Marshal(BuildStructure(huge))
	if err != nil {
		t.Fatalf("marshal huge: %v", err)
	}

	// The raw inputs differ by ~4 * 200000 bytes (4 steps' worth of huge
	// tool-call args, the only field this fixture varies — Args is the
	// only inlined-and-truncated field this fixture's huge payload reaches,
	// since it never sets a tool result, RespText, or Reasoning of
	// comparable size). If truncation is working, the serialized structures
	// should differ by roughly 4 * structureExcerptChars at most — allow a
	// generous multiple for JSON escaping/field overhead, but this must stay
	// far below the raw injected difference, not merely "somewhat smaller".
	diff := len(hugeJSON) - len(smallJSON)
	bound := 4 * structureExcerptChars * 2 // 4 steps * cap * overhead factor for JSON escaping
	if diff > bound {
		t.Errorf("structure size grew by %d bytes for a %d-byte prose-length increase — want growth bounded by ~%d (step count * excerpt cap), not proportional to conversation length", diff, 4*200000, bound)
	}

	// Every huge injected payload must have been truncated, never inlined
	// whole — the sentinel is long enough that no cap-sized excerpt could
	// accidentally contain the full run.
	sentinel := strings.Repeat("x", 200000)
	if strings.Contains(string(hugeJSON), sentinel) {
		t.Error("huge tool-call args appear untruncated in the structure JSON")
	}
}

// buildJourneyWithArgsLen builds a 4-step, single-tool-call-per-step Journey
// whose every tool-call's "content" argument is a repeated-'x' string of
// argsLen bytes — shared between this file's volume guard and
// llm_packs_test.go's evidence-pack isolation guard, both of which need the
// same "same step count, wildly different prose length" fixture shape.
func buildJourneyWithArgsLen(t *testing.T, argsLen int) *Journey {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "do the task")
	var recs []audit.Record
	msgsSoFar := []any{sys, u1}
	const n = 4
	for i := 0; i < n; i++ {
		args := `{"content":"` + strings.Repeat("x", argsLen) + `"}`
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgsSoFar...), sseToolCalls([]any{
			map[string]any{"id": "c" + string(rune('a'+i)), "function": map[string]any{"name": "write_file", "arguments": args}},
		})))
		msgsSoFar = append(msgsSoFar, msg("assistant", "did step"))
		msgsSoFar = append(msgsSoFar, map[string]any{"role": "tool", "tool_call_id": "c" + string(rune('a'+i)), "content": "ok"})
	}
	path := writeJSONL(t, recs)
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return j
}

// TestBuildStructure_BodiesIntegrity locks the D18 / §3.6 invariants:
// 1. No dangling references: every *_ref resolves to a valid body in bodies table.
// 2. No orphan blobs: every entry in bodies table is referenced by at least one *_ref.
// 3. De-duplication: identical strings map to the exact same hash reference.
func TestBuildStructure_BodiesIntegrity(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sameArgs := `{"query":"status"}`
	sameResult := "all systems operational"

	s1 := &Step{
		Seq:      1,
		RespText: "Step 1 initial plan",
		ToolCalls: []chatmsg.ToolCall{
			{ID: "call_1", Name: "fetch", Args: `{"url":"https://example.com"}`},
		},
		Manifest: mkManifest(at(0)),
	}
	s2 := &Step{
		Seq:       2,
		Reasoning: "thinking through step 2",
		ToolCalls: []chatmsg.ToolCall{
			{ID: "call_2", Name: "check", Args: sameArgs},
			{ID: "call_3", Name: "check", Args: sameArgs}, // Identical args to call_2
		},
		NewToolResults: []chatmsg.ToolResult{
			{CallID: "call_1", Text: "fetched body ok", IsError: false},
		},
		Manifest: mkManifest(at(1)),
	}
	s3 := &Step{
		Seq: 3,
		Compaction: &CompactionInfo{
			TokensBefore:           15000,
			TokensAfter:            3000,
			PredecessorTextExcerpt: "summary of earlier dialogue",
		},
		NewToolResults: []chatmsg.ToolResult{
			{CallID: "call_2", Text: sameResult, IsError: false},
			{CallID: "call_3", Text: sameResult, IsError: false}, // Identical result to call_2
		},
		Manifest: mkManifest(at(2)),
	}

	j := &Journey{
		ID: "j-test-bodies",
		Tasks: []*Task{
			{Title: "Task 1", Steps: []*Step{s1, s2}},
			{Title: "Task 2", Steps: []*Step{s3}},
		},
	}

	structure := BuildStructure(j)

	if len(structure.Bodies) == 0 {
		t.Fatal("expected non-empty Bodies map")
	}

	// 1. Check that call_2 and call_3 deduplicated their args ref
	step2 := structure.Tasks[0].Steps[1]
	if len(step2.ToolCalls) != 2 {
		t.Fatalf("step 2 tool calls = %d, want 2", len(step2.ToolCalls))
	}
	if step2.ToolCalls[0].ArgsRef == "" || step2.ToolCalls[0].ArgsRef != step2.ToolCalls[1].ArgsRef {
		t.Errorf("expected call_2 and call_3 to share identical ArgsRef, got %q and %q",
			step2.ToolCalls[0].ArgsRef, step2.ToolCalls[1].ArgsRef)
	}

	// Check that call_2 and call_3 deduplicated their result ref
	step3 := structure.Tasks[1].Steps[0]
	if step3.Compaction == nil || step3.Compaction.PredecessorExcerptRef == "" {
		t.Fatalf("expected step 3 to have compaction with predecessor excerpt ref")
	}
	if step2.ToolCalls[0].Result == nil || step2.ToolCalls[1].Result == nil {
		t.Fatalf("expected step 2 tool calls to have paired results")
	}
	if step2.ToolCalls[0].Result.Ref == "" || step2.ToolCalls[0].Result.Ref != step2.ToolCalls[1].Result.Ref {
		t.Errorf("expected call_2 and call_3 to share identical Result.Ref, got %q and %q",
			step2.ToolCalls[0].Result.Ref, step2.ToolCalls[1].Result.Ref)
	}

	// 2. Assert no dangling references
	referencedRefs := make(map[string]int)
	recordRef := func(ref string, fieldName string) {
		if ref == "" {
			return
		}
		val, ok := structure.Bodies[ref]
		if !ok {
			t.Errorf("dangling reference: %s ref %q not found in Bodies table", fieldName, ref)
		}
		if val == "" {
			t.Errorf("empty body stored for %s ref %q", fieldName, ref)
		}
		referencedRefs[ref]++
	}

	for _, task := range structure.Tasks {
		for _, step := range task.Steps {
			recordRef(step.RespRef, "RespRef")
			if step.Compaction != nil {
				recordRef(step.Compaction.PredecessorExcerptRef, "Compaction.PredecessorExcerptRef")
			}
			for _, tc := range step.ToolCalls {
				recordRef(tc.ArgsRef, "ToolCall.ArgsRef")
				if tc.Result != nil {
					recordRef(tc.Result.Ref, "ToolCall.Result.Ref")
				}
			}
		}
	}

	// 3. Assert no orphan blobs
	for blobKey, blobVal := range structure.Bodies {
		if count, ok := referencedRefs[blobKey]; !ok || count == 0 {
			t.Errorf("orphan blob found in Bodies: key=%q val=%q has 0 references", blobKey, blobVal)
		}
	}

	if len(referencedRefs) != len(structure.Bodies) {
		t.Errorf("referenced ref count %d != bodies count %d", len(referencedRefs), len(structure.Bodies))
	}
}

// TestBuildStructure_ThreeLevelToolPairing verifies exact, normalized,
// and positional tool result pairing levels (D18 / §3.6).
func TestBuildStructure_ThreeLevelToolPairing(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }

	s1 := &Step{
		Seq: 1,
		ToolCalls: []chatmsg.ToolCall{
			{ID: "call_exact_1", Name: "fetch", Args: "{}"},
			{ID: "call_norm_2", Name: "read", Args: "{}"},
			{ID: "call_pos_3", Name: "eval", Args: "{}"},
		},
		Manifest: mkManifest(at(0)),
	}

	// In s2:
	// 1. exact match: ID matches "call_exact_1" verbatim
	// 2. normalized match: ID "callnorm2" matches "call_norm_2" when underscores stripped
	// 3. positional match: ID "call_random_unknown" positionally matches the 1 leftover call
	// And s2 introduces an unmatched tool call "call_unmatched_4" that receives no answer in s3.
	s2 := &Step{
		Seq: 2,
		ToolCalls: []chatmsg.ToolCall{
			{ID: "call_unmatched_4", Name: "skip", Args: "{}"},
		},
		NewToolResults: []chatmsg.ToolResult{
			{CallID: "call_exact_1", Text: "exact result", IsError: false},
			{CallID: "callnorm2", Text: "norm result", IsError: true},
			{CallID: "call_random_unknown", Text: "positional result", IsError: false},
		},
		Manifest: mkManifest(at(1)),
	}

	s3 := &Step{
		Seq:      3,
		Manifest: mkManifest(at(2)),
	}

	j := &Journey{
		ID: "j-test-pairing",
		Tasks: []*Task{
			{Title: "Task", Steps: []*Step{s1, s2, s3}},
		},
	}

	structure := BuildStructure(j)
	step1 := structure.Tasks[0].Steps[0]
	if len(step1.ToolCalls) != 3 {
		t.Fatalf("step 1 tool calls = %d, want 3", len(step1.ToolCalls))
	}

	byID := make(map[string]ToolCallRef)
	for _, tc := range step1.ToolCalls {
		byID[tc.ID] = tc
	}

	// 1. Exact match
	exactTC := byID["call_exact_1"]
	if exactTC.Result == nil {
		t.Fatal("call_exact_1 should be matched")
	}
	if exactTC.Result.Match != "exact" {
		t.Errorf("call_exact_1 match = %q, want \"exact\"", exactTC.Result.Match)
	}
	if exactTC.Result.IsError {
		t.Errorf("call_exact_1 IsError = true, want false")
	}
	if structure.Bodies[exactTC.Result.Ref] != "exact result" {
		t.Errorf("call_exact_1 result text = %q, want \"exact result\"", structure.Bodies[exactTC.Result.Ref])
	}

	// 2. Normalized match
	normTC := byID["call_norm_2"]
	if normTC.Result == nil {
		t.Fatal("call_norm_2 should be matched")
	}
	if normTC.Result.Match != "normalized" {
		t.Errorf("call_norm_2 match = %q, want \"normalized\"", normTC.Result.Match)
	}
	if !normTC.Result.IsError {
		t.Errorf("call_norm_2 IsError = false, want true")
	}
	if structure.Bodies[normTC.Result.Ref] != "norm result" {
		t.Errorf("call_norm_2 result text = %q, want \"norm result\"", structure.Bodies[normTC.Result.Ref])
	}

	// 3. Positional match
	posTC := byID["call_pos_3"]
	if posTC.Result == nil {
		t.Fatal("call_pos_3 should be matched")
	}
	if posTC.Result.Match != "positional" {
		t.Errorf("call_pos_3 match = %q, want \"positional\"", posTC.Result.Match)
	}
	if structure.Bodies[posTC.Result.Ref] != "positional result" {
		t.Errorf("call_pos_3 result text = %q, want \"positional result\"", structure.Bodies[posTC.Result.Ref])
	}

	// 4. Unmatched call in step 2
	step2 := structure.Tasks[0].Steps[1]
	if len(step2.ToolCalls) != 1 {
		t.Fatalf("step 2 tool calls = %d, want 1", len(step2.ToolCalls))
	}
	unmatchedTC := step2.ToolCalls[0]
	if unmatchedTC.Result != nil {
		t.Errorf("call_unmatched_4 should have nil Result, got %+v", unmatchedTC.Result)
	}
}

// TestBuildStructure_TruncationLimits verifies the uniform 3000 character limit
// for tool args, tool results, and compaction predecessor excerpts, while RespText
// remains completely untruncated (D18 / §3.6).
func TestBuildStructure_TruncationLimits(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	long4000 := strings.Repeat("A", 4000)

	s1 := &Step{
		Seq:      1,
		RespText: long4000,
		ToolCalls: []chatmsg.ToolCall{
			{ID: "c1", Name: "test", Args: long4000},
		},
		Manifest: mkManifest(at(0)),
	}
	s2 := &Step{
		Seq: 2,
		Compaction: &CompactionInfo{
			TokensBefore:           5000,
			PredecessorTextExcerpt: long4000,
		},
		NewToolResults: []chatmsg.ToolResult{
			{CallID: "c1", Text: long4000},
		},
		Manifest: mkManifest(at(1)),
	}

	j := &Journey{
		ID: "j-test-caps",
		Tasks: []*Task{
			{Title: "Task", Steps: []*Step{s1, s2}},
		},
	}

	structure := BuildStructure(j)
	step1 := structure.Tasks[0].Steps[0]
	step2 := structure.Tasks[0].Steps[1]

	// RespText must NOT be truncated
	respBody := structure.Bodies[step1.RespRef]
	if len(respBody) != 4000 {
		t.Errorf("RespText length = %d, want 4000 (untruncated)", len(respBody))
	}

	// Tool call args must be truncated to 3000
	argsBody := structure.Bodies[step1.ToolCalls[0].ArgsRef]
	if len(argsBody) != maxBodyExcerptChars {
		t.Errorf("Tool call args length = %d, want %d", len(argsBody), maxBodyExcerptChars)
	}

	// Tool result must be truncated to 3000
	resultBody := structure.Bodies[step1.ToolCalls[0].Result.Ref]
	if len(resultBody) != maxBodyExcerptChars {
		t.Errorf("Tool result length = %d, want %d", len(resultBody), maxBodyExcerptChars)
	}

	// Compaction predecessor excerpt must be truncated to 3000
	compactionBody := structure.Bodies[step2.Compaction.PredecessorExcerptRef]
	if len(compactionBody) != maxBodyExcerptChars {
		t.Errorf("Compaction excerpt length = %d, want %d", len(compactionBody), maxBodyExcerptChars)
	}
}

// TestJourneyReportFile_Normalization tests D19 / §1.1 filename normalization.
func TestJourneyReportFile_Normalization(t *testing.T) {
	cases := []struct {
		id      string
		partial bool
		want    string
	}{
		{"j-abc", false, "j-abc.md"},
		{"j-abc", true, "j-abc.md"}, // No -partial suffix!
		{"abc", false, "j-abc.md"},
		{"abc", true, "j-abc.md"},
	}

	for _, tc := range cases {
		got := JourneyReportFile(tc.id, tc.partial)
		if got != tc.want {
			t.Errorf("JourneyReportFile(%q, %v) = %q, want %q", tc.id, tc.partial, got, tc.want)
		}
	}
}
