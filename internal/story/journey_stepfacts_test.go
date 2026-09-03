// Ver 2026-08-30 22:00, by Sonnet 5

package story

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// TestFillStepFacts_ExtractsWhatConsumersNeed pins the four Step facts and
// the two Journey facts fillStepFacts produces against a small lineage that
// exercises each: a system prompt, a tool call answered in the next step's
// delta, and a per-step Attempts list.
func TestFillStepFacts_ExtractsWhatConsumersNeed(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "You are a careful assistant. Keep answers short.")
	// >previewLen (80) runes, with internal double spaces — so this text
	// only survives intact if InitialInstruction skips segment.Preview
	// (which caps length and collapses whitespace).
	longInstr := "investigate the flaky  test in the parser package and report which  assertion is racing with the writer goroutine"
	u1 := msg("user", longInstr)
	a1 := map[string]any{"role": "assistant", "content": "looking", "tool_calls": []any{
		map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "grep", "arguments": `{"q":"flaky"}`}},
	}}
	tr := map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "3 matches"}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("looking"))
	r1.Attempts = []audit.Attempt{{Provider: "prov-a", Model: "m-1"}}
	r2 := mkRec(at(1), "", []any{sys, u1, a1, tr}, sseText("done"))
	r2.Attempts = []audit.Attempt{
		{Provider: "prov-a", Model: "m-1", ErrorClass: "transient"},
		{Provider: "prov-b", Model: "m-2"},
	}

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	steps := journeySteps(j)
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}

	// Attempts: trimmed to (Provider, Model), one entry per audit attempt.
	if len(steps[0].Attempts) != 1 || steps[0].Attempts[0] != (AttemptFact{Provider: "prov-a", Model: "m-1"}) {
		t.Errorf("step 0 Attempts = %+v", steps[0].Attempts)
	}
	if len(steps[1].Attempts) != 2 || steps[1].Attempts[1] != (AttemptFact{Provider: "prov-b", Model: "m-2"}) {
		t.Errorf("step 1 Attempts = %+v", steps[1].Attempts)
	}

	// Context: system tokens on both steps (system message is resent every turn).
	if steps[0].Context.SystemTokens == 0 || steps[1].Context.SystemTokens == 0 {
		t.Errorf("Context.SystemTokens should be non-zero on both steps: %+v / %+v", steps[0].Context, steps[1].Context)
	}
	if steps[0].Context.Seq != steps[0].Seq {
		t.Errorf("Context.Seq = %d, want %d", steps[0].Context.Seq, steps[0].Seq)
	}

	// NewToolResults: the call_1 result lands in step 1's delta, not step 0's.
	if len(steps[0].NewToolResults) != 0 {
		t.Errorf("step 0 NewToolResults = %+v, want none", steps[0].NewToolResults)
	}
	if len(steps[1].NewToolResults) != 1 || steps[1].NewToolResults[0].CallID != "call_1" {
		t.Errorf("step 1 NewToolResults = %+v, want one call_1", steps[1].NewToolResults)
	}

	// SysChars: rune length of the leading system block, same on both steps.
	wantSysChars := len([]rune("You are a careful assistant. Keep answers short."))
	if steps[0].SysChars != wantSysChars || steps[1].SysChars != wantSysChars {
		t.Errorf("SysChars = %d / %d, want %d", steps[0].SysChars, steps[1].SysChars, wantSysChars)
	}

	// Journey.SysText: deduped by SysHash — one entry, one part.
	if len(j.SysText) != 1 {
		t.Fatalf("j.SysText has %d entries, want 1", len(j.SysText))
	}
	for _, parts := range j.SysText {
		if len(parts) != 1 || parts[0] != "You are a careful assistant. Keep answers short." {
			t.Errorf("j.SysText parts = %+v", parts)
		}
	}

	// Journey.InitialInstruction: raw text, NOT run through segment.Preview
	// (full length kept, internal double space preserved).
	if j.InitialInstruction != longInstr {
		t.Errorf("j.InitialInstruction = %q, want the full raw instruction", j.InitialInstruction)
	}
	if got := taskseg.FirstInstruction(taskseg.RealUsers{0: longInstr}); got == j.InitialInstruction {
		t.Errorf("InitialInstruction should differ from the Preview'd form; both were %q", got)
	}
}

// TestStepContextPoint_MatchesLegacyContextCurve confirms the extracted
// per-step composition equals what metrics.go's contextCurve computes for
// the same Journey.
func TestStepContextPoint_MatchesLegacyContextCurve(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys prompt here")
	u1 := msg("user", "first ask with several words in it")
	a1 := msg("assistant", "an assistant reply also with words")
	u2 := msg("user", "a follow-up question")

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("reply one"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, u2}, sseText("reply two"))
	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	steps := journeySteps(j)
	curve := contextCurve(steps)
	if len(curve) != len(steps) {
		t.Fatalf("curve len %d, steps %d", len(curve), len(steps))
	}
	for i, s := range steps {
		if s.Context != curve[i] {
			t.Errorf("step %d: Context %+v != contextCurve %+v", i, s.Context, curve[i])
		}
	}
}

func TestDeltaRawMsgs(t *testing.T) {
	raw := []any{"a", "b", "c", "d"}
	// off=1 (a synthetic leading system message excluded from RawArray).
	if got := deltaRawMsgs(raw, 1, 3); len(got) != 2 || got[0] != "c" {
		t.Errorf("deltaStart=3,off=1 -> %v, want [c d]", got)
	}
	if got := deltaRawMsgs(raw, 1, 1); len(got) != 4 { // deltaIdx 0 -> whole array
		t.Errorf("deltaStart=1,off=1 -> %v, want the whole array", got)
	}
	if got := deltaRawMsgs(raw, 0, 0); len(got) != 4 { // stitch boundary
		t.Errorf("deltaStart=0 -> %v, want the whole array", got)
	}
	if got := deltaRawMsgs(raw, 1, 99); got != nil { // past the end
		t.Errorf("deltaStart past end -> %v, want nil", got)
	}
}

// TestFillStepFacts_NewToolResultsScopedToDelta: a Step's NewToolResults
// must contain only the tool results ITS OWN delta introduced, not an
// earlier Step's already-in-history results that every resent body carries.
func TestFillStepFacts_NewToolResultsScopedToDelta(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "do the thing")
	a1 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
		map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "bash", "arguments": "{}"}},
	}}
	tr1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "first result"}
	a2 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
		map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "bash", "arguments": "{}"}},
	}}
	tr2 := map[string]any{"role": "tool", "tool_call_id": "c2", "content": "second result"}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("start"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, tr1}, sseText("more"))
	r3 := mkRec(at(2), "", []any{sys, u1, a1, tr1, a2, tr2}, sseText("done"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	steps := journeySteps(j)
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	// step 3's body carries both tr1 and tr2, but only tr2 is in its delta.
	if len(steps[2].NewToolResults) != 1 || steps[2].NewToolResults[0].CallID != "c2" {
		t.Errorf("step 3 NewToolResults = %+v, want only c2 (tr1 is older history)", steps[2].NewToolResults)
	}
	if len(steps[1].NewToolResults) != 1 || steps[1].NewToolResults[0].CallID != "c1" {
		t.Errorf("step 2 NewToolResults = %+v, want only c1", steps[1].NewToolResults)
	}
}

// TestComputeRU_ResetsOnSysChanged pins the sys-change reset: when a step's leading
// system block changes (sysChanged true OR curLeadSys diverges from the
// prior step's prevLeadSys), computeRU must NOT reuse the prior prefix —
// absolute message indices shift, and copying prevRu[k] into ru[k] for
// k < deltaStart silently mis-classifies real-user boundaries. The
// same-LeadSys/SysChanged=false path stays identical to before so
// unchanged journeys keep their existing behavior.
func TestComputeRU_ResetsOnSysChanged(t *testing.T) {
	t.Parallel()
	prof := taskseg.OpenClawAware
	// Step 1: single system + one user. LeadSys=1.
	msgs1 := []chatmsg.Message{{Role: "system", Text: "sysA"}, {Role: "user", Text: "u1"}}
	raw1 := []any{map[string]any{"role": "system", "content": "sysA"}, map[string]any{"role": "user", "content": "u1"}}
	var s stepFactState
	ru1 := s.computeRU(prof, msgs1, raw1, 0, 0, 1, false)
	if _, ok := ru1[1]; !ok {
		t.Fatalf("step 1 ru missing index 1: %+v", ru1)
	}
	// Step 2: SAME lead sys, just one new user message at idx 2. deltaStart=2, prevLeadSys==curLeadSys, sysChanged=false -> prefix reuse.
	msgs2 := []chatmsg.Message{{Role: "system", Text: "sysA"}, {Role: "user", Text: "u1"}, {Role: "user", Text: "u2"}}
	raw2 := []any{
		map[string]any{"role": "system", "content": "sysA"},
		map[string]any{"role": "user", "content": "u1"},
		map[string]any{"role": "user", "content": "u2"},
	}
	ru2 := s.computeRU(prof, msgs2, raw2, 0, 2, 1, false)
	if _, ok := ru2[1]; !ok {
		t.Errorf("step 2 (reuse path) lost prefix ru[1]: %+v", ru2)
	}
	if _, ok := ru2[2]; !ok {
		t.Errorf("step 2 missing ru[2] (the new u2 instruction): %+v", ru2)
	}
	// Step 3: system prompt GROWS by one block. curLeadSys=2 (now
	// has [sysA, sysA2]), sysChanged=true. Even if deltaStart still
	// targets the same absolute messages the prior step covered, we
	// must NOT reuse prevRu — the absolute-index alignment has shifted.
	msgs3 := []chatmsg.Message{
		{Role: "system", Text: "sysA"},
		{Role: "system", Text: "sysA2"},
		{Role: "user", Text: "u1"},
		{Role: "user", Text: "u2"},
		{Role: "user", Text: "u3"},
	}
	raw3 := []any{
		map[string]any{"role": "system", "content": "sysA"},
		map[string]any{"role": "system", "content": "sysA2"},
		map[string]any{"role": "user", "content": "u1"},
		map[string]any{"role": "user", "content": "u2"},
		map[string]any{"role": "user", "content": "u3"},
	}
	want := taskseg.IndexRealUsers(prof, msgs3, raw3, 0)
	ru3 := s.computeRU(prof, msgs3, raw3, 0, 3, 2, true)
	// Compare key-by-key to verify the SysChanged gate took the
	// "recompute everything" branch (no prefix reuse).
	if len(ru3) != len(want) {
		t.Errorf("step 3 ru len = %d, want %d (SysChanged gate): %+v vs %+v", len(ru3), len(want), ru3, want)
	}
	for k, v := range want {
		if got, ok := ru3[k]; !ok || got != v {
			t.Errorf("step 3 ru[%d] = %q (ok=%v), want %q", k, got, ok, v)
		}
	}
	// Step 4: same sys as step 3 (LeadSys=2), one new user at idx 5.
	// Reuse path should re-engage now that the gate aligns again.
	msgs4 := append([]chatmsg.Message{}, msgs3...)
	msgs4 = append(msgs4, chatmsg.Message{Role: "user", Text: "u4"})
	raw4 := append([]any{}, raw3...)
	raw4 = append(raw4, map[string]any{"role": "user", "content": "u4"})
	ru4 := s.computeRU(prof, msgs4, raw4, 0, 5, 2, false)
	if _, ok := ru4[4]; !ok {
		t.Errorf("step 4 missing ru[4] (new u3 after reuse resumed): %+v", ru4)
	}
	if _, ok := ru4[5]; !ok {
		t.Errorf("step 4 missing ru[5] (new u4): %+v", ru4)
	}
}
