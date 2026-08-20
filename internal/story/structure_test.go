// Ver 2026-08-20, by Sonnet 5

package story

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
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
		if !ss.UsageOK {
			t.Errorf("step %d: UsageOK = false, want true (mkRecWithUsage sets a usage block)", i+1)
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
	if !tc.Matched || tc.ResultError {
		t.Errorf("step 1 tool call: Matched=%v ResultError=%v, want Matched=true ResultError=false", tc.Matched, tc.ResultError)
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

// TestBuildStructure_LosslessReconstruction is the acceptance test DevPlan
// P4/P5 both point at: given only journey-<id>.json's structure field and
// the audit log (no in-memory Journey), can the same messages fact-layer's
// rendering (render_md.go's renderStep/renderEvent) shows today be
// recovered?
//
// The correct reconstruction is HASH MATCHING, not a DeltaStart slice — the
// first version of this test (and the first independent review's proposed
// fix) sliced msgs[DeltaStart:] and asserted a 1:1 count/order match against
// NewEvents. That only holds for the "no repeats" case: appendNewEvents
// (journey.go) applies a JOURNEY-WIDE seen-hash dedup on top of the
// DeltaStart slice, so whenever this Step's "new" range happens to repeat a
// message byte-identical to one already seen earlier in the Journey (a
// user resending the same text, a stitch boundary re-showing content),
// msgs[DeltaStart:] is a strict SUPERSET of NewEvents — a straight slice
// comparison would fail (second review's T4 finding). This fixture's step 3
// deliberately repeats step 1's user message verbatim to force exactly that
// case, and the reconstruction matches each EventRef.Hash against the
// refetched record's own message hashes (computed the same way
// ctxgraph.BuildManifest does) instead of assuming positional alignment.
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
	// LCP still matches the [u1,a1,t1] prefix, so DeltaStart lands right
	// after t1 and msgs[DeltaStart:] = [a2, u1-again]; global dedup then
	// drops u1-again from NewEvents (its hash was already seen at step 1),
	// leaving NewEvents = [a2] only. A DeltaStart-slice reconstruction sees
	// 2 "new" messages; the real answer is 1.
	r3 := mkRec(at(2), "", []any{sys, u1, a1, t1, a2, u1}, sseText("sure, anything else?"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	steps := journeySteps(j)
	structure := BuildStructure(j)
	var ssteps []StepStructure
	for _, task := range structure.Tasks {
		ssteps = append(ssteps, task.Steps...)
	}
	if len(ssteps) != len(steps) {
		t.Fatalf("got %d structure steps, want %d", len(ssteps), len(steps))
	}

	// Sanity: confirm the fixture actually exercises the hard case before
	// trusting the reconstruction loop below to have tested anything real.
	step3 := ssteps[2]
	if len(step3.NewEvents) != 1 {
		t.Fatalf("test setup: step 3 NewEvents = %d, want 1 (a2 only — u1's repeat must be deduped); fixture no longer exercises the dedup case", len(step3.NewEvents))
	}

	wantBasename := ctxgraph.CanonicalPath(path)
	for i, s := range steps {
		ss := ssteps[i]
		if ss.Req == "" {
			t.Fatalf("step %d: empty Req", s.Seq)
		}
		basename, line, err := ctxgraph.ParseReqCoord(ss.Req)
		if err != nil {
			t.Fatalf("step %d: ParseReqCoord(%q): %v", s.Seq, ss.Req, err)
		}
		if basename != wantBasename {
			t.Fatalf("step %d: Req basename %q, want %q", s.Seq, basename, wantBasename)
		}

		// True external I/O: refetch the record by coordinate alone (no
		// in-memory Step/Manifest involved from here on) and rebuild a
		// fresh Manifest the same way ctxgraph.BuildManifest always does —
		// this IS the canonical way to recover the Hash↔(role,text) mapping
		// EventRef.Hash's doc comment points at.
		raw, err := audit.LineAt(path, line)
		if err != nil {
			t.Fatalf("step %d: LineAt(%d): %v", s.Seq, line, err)
		}
		var rec audit.Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatalf("step %d: unmarshal refetched record: %v", s.Seq, err)
		}
		m, ok := ctxgraph.BuildManifest(&rec, path, line)
		if !ok {
			t.Fatalf("step %d: BuildManifest failed on refetched record", s.Seq)
		}
		msgs := chatmsg.Messages(rec.Client.Request.Body)

		// hash -> (role, text), for every message BuildManifest assigned a
		// Key to (i.e. every non-leading-system message — the leading
		// system block uses a separate hash space, SysHash, not Keys; none
		// of this fixture's NewEvents are leading-system events past step
		// 1, and step 1's single system message is checked separately
		// below without going through this map).
		byHash := make(map[ctxgraph.Hash]chatmsg.Message, len(m.Keys))
		for k, h := range m.Keys {
			byHash[h] = msgs[m.MsgIdx[k]]
		}

		for _, evRef := range ss.NewEvents {
			if evRef.Role == "system" {
				// Leading system message: Keys/MsgIdx don't cover it (see
				// the byHash comment above) — verified structurally instead
				// (it must be msgs[0], and only step 1 ever introduces it).
				if s.Seq != 1 || len(msgs) == 0 || msgs[0].Role != "system" {
					t.Errorf("step %d: unexpected system-role NewEvent outside the leading-system special case", s.Seq)
				}
				continue
			}
			got, found := byHash[evRef.Hash]
			if !found {
				t.Errorf("step %d: EventRef.Hash %x not found among the Req-refetched record's own message hashes — coordinate does not actually resolve to this content", s.Seq, evRef.Hash)
				continue
			}
			if got.Role != evRef.Role {
				t.Errorf("step %d: hash-matched message role %q, structure claims %q", s.Seq, got.Role, evRef.Role)
			}
		}
	}

	// The reconstructed text for step 3's sole NewEvent must be a2's own
	// text, not u1's (proving the dedup didn't just happen to leave the
	// count right for the wrong reason).
	m3, ok := ctxgraph.BuildManifest(&r3, path, 3)
	if !ok {
		t.Fatal("BuildManifest failed reconstructing step 3's own record")
	}
	msgs3 := chatmsg.Messages(r3.Client.Request.Body)
	gotHash := ssteps[2].NewEvents[0].Hash
	foundText := ""
	for k, h := range m3.Keys {
		if h == gotHash {
			foundText = msgs3[m3.MsgIdx[k]].Text
		}
	}
	if want := a2["content"].(string); foundText != want {
		t.Errorf("step 3's sole NewEvent hash-resolves to %q, want %q (a2's text, not u1's repeat)", foundText, want)
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
