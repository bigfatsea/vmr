// Ver 2026-08-15, by Sonnet 5

package taskseg

import (
	"strings"
	"testing"
	"unicode/utf8"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

func TestIndexRealUsers_OnlyRealUserMessages(t *testing.T) {
	msgs := []chatmsg.Message{
		{Role: "system", Text: "sys"},
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "hi"},
		{Role: "user", Text: "   "}, // whitespace-only: Generic rejects it
	}
	ru := IndexRealUsers(Generic, msgs, nil, 0)
	if len(ru) != 1 {
		t.Fatalf("len(ru) = %d, want 1", len(ru))
	}
	if ru[1] != "hello" {
		t.Errorf("ru[1] = %q, want %q", ru[1], "hello")
	}
	if _, ok := ru[3]; ok {
		t.Error("whitespace-only user message should not be indexed")
	}
}

// TestIndexRealUsers_StoresPreviewNotRaw pins the reversal of B3's original
// "store raw, truncate on read" decision: report keeps one index per record
// for the whole corpus and every record carries the session's full history,
// so storing raw retained each instruction once per later record in the same
// session (and, for an envelope-stripping dialect, pinned the entire
// original message through the returned subslice). No consumer ever wanted
// raw — see IndexRealUsers' doc comment.
func TestIndexRealUsers_StoresPreviewNotRaw(t *testing.T) {
	long := strings.Repeat("a ", 200) // far past previewLen once trimmed
	msgs := []chatmsg.Message{{Role: "user", Text: long}}
	ru := IndexRealUsers(Generic, msgs, nil, 0)
	if ru[0] != Preview(long) {
		t.Errorf("ru[0] = %q, want the Preview'd form %q", ru[0], Preview(long))
	}
	if utf8.RuneCountInString(ru[0]) > previewLen+1 { // +1: the ellipsis
		t.Errorf("stored value is %d runes, want at most %d — the index must not "+
			"retain the untruncated text", utf8.RuneCountInString(ru[0]), previewLen+1)
	}
}

// TestPreviewIsIdempotent is what makes storing the preview safe: every
// consumer of a RealUsers value still calls Preview on the way out, so a
// second application must be a no-op or the two commands' titles would
// differ by which side of the map truncated. Covers the boundary shapes —
// exactly at the cap, one past it, and multi-byte runes at the cut.
func TestPreviewIsIdempotent(t *testing.T) {
	cases := []string{
		"",
		"short",
		"  collapse   internal \n whitespace  ",
		strings.Repeat("a", previewLen),
		strings.Repeat("a", previewLen+1),
		strings.Repeat("a ", 200),
		strings.Repeat("中", previewLen+5),
		strings.Repeat("😀", previewLen+5),
	}
	for _, s := range cases {
		once := Preview(s)
		if twice := Preview(once); twice != once {
			t.Errorf("Preview not idempotent for %d-rune input: %q vs %q",
				utf8.RuneCountInString(s), once, twice)
		}
	}
}

func TestIndexRealUsers_OffsetAlignment(t *testing.T) {
	// off=1 mirrors the anthropic-messages/openai-responses shape: a synthesized system
	// message occupies msgs[0], so msgs[i] aligns to rawMsgs[i-1]. rawMsgs
	// carries only the real "messages" array elements (no synthesized
	// entry), so rawMsgs[0] here is the counterpart of msgs[1].
	msgs := []chatmsg.Message{
		{Role: "system", Text: "sys"},
		{Role: "user", Text: "hello"},
	}
	rawMsgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "tool_result", "content": "..."},
			},
		},
	}
	ru := IndexRealUsers(OpenClawAware, msgs, rawMsgs, 1)
	if _, ok := ru[1]; ok {
		t.Error("a user message whose raw counterpart (via the off=1 shift) is entirely tool_result parts must not be indexed as real")
	}
}

func TestManifestKeySet_NilManifest(t *testing.T) {
	if got := ManifestKeySet(nil); got != nil {
		t.Errorf("ManifestKeySet(nil) = %v, want nil", got)
	}
}

func TestManifestKeySet_BuildsSetFromKeys(t *testing.T) {
	h1, h2 := ctxgraph.Hash{0x01}, ctxgraph.Hash{0x02}
	m := &ctxgraph.Manifest{Keys: []ctxgraph.Hash{h1, h2}}
	set := ManifestKeySet(m)
	if !set[h1] || !set[h2] || len(set) != 2 {
		t.Errorf("ManifestKeySet = %v, want a 2-element set containing both keys", set)
	}
}

func TestFirstInstruction_PicksLowestIndex(t *testing.T) {
	ru := RealUsers{9: "latest", 1: "opening ask", 5: "middle"}
	if got := FirstInstruction(ru); got != "opening ask" {
		t.Errorf("FirstInstruction = %q, want %q", got, "opening ask")
	}
}

func TestFirstInstruction_EmptyWhenNoCandidates(t *testing.T) {
	if got := FirstInstruction(RealUsers{}); got != "" {
		t.Errorf("FirstInstruction(empty) = %q, want \"\"", got)
	}
}

func TestHasNewInstruction_WithinWindowAndNotInParent(t *testing.T) {
	ru := RealUsers{5: "new ask"}
	cur := &ctxgraph.Manifest{LeadSys: 0, Keys: make([]ctxgraph.Hash, 6)}
	if !HasNewInstruction(ru, nil, cur, 0, 6) {
		t.Error("a real instruction within the window and absent from a nil parent set should count as new")
	}
}

func TestHasNewInstruction_OutsideWindowIgnored(t *testing.T) {
	ru := RealUsers{0: "old ask"}
	total := 0 + chatmsg.NewUserWindow + 5
	cur := &ctxgraph.Manifest{LeadSys: 0, Keys: make([]ctxgraph.Hash, total)}
	if HasNewInstruction(ru, nil, cur, 0, total) {
		t.Error("an instruction far outside the tail window must not count as new")
	}
}

func TestHasNewInstruction_BeforeDeltaStartIgnored(t *testing.T) {
	ru := RealUsers{1: "in the parent's kept prefix"}
	cur := &ctxgraph.Manifest{LeadSys: 0, Keys: make([]ctxgraph.Hash, 3)}
	if HasNewInstruction(ru, nil, cur, 2, 3) {
		t.Error("an instruction before deltaStart is part of the retained prefix, not new")
	}
}

func TestHasNewInstruction_ShiftedButAlreadyInParentIsNotNew(t *testing.T) {
	h := ctxgraph.Hash{0x01}
	cur := &ctxgraph.Manifest{LeadSys: 0, Keys: []ctxgraph.Hash{h}}
	prevKeys := map[ctxgraph.Hash]bool{h: true}
	ru := RealUsers{0: "same content, shifted position"}
	if HasNewInstruction(ru, prevKeys, cur, 0, 1) {
		t.Error("content already present in the parent (by hash) must not count as new, even if its position shifted")
	}
}

func TestHasNewInstruction_NilPrevKeysNeverExcludes(t *testing.T) {
	h := ctxgraph.Hash{0x02}
	cur := &ctxgraph.Manifest{LeadSys: 0, Keys: []ctxgraph.Hash{h}}
	ru := RealUsers{0: "first ever message"}
	if !HasNewInstruction(ru, nil, cur, 0, 1) {
		t.Error("a nil prevKeys map (no parent) must never suppress an otherwise-new instruction")
	}
}

func TestLastInstruction_PicksHighestIndexAtOrAfterDeltaStart(t *testing.T) {
	ru := RealUsers{1: "first", 5: "middle", 9: "latest"}
	if got := LastInstruction(ru, 3); got != "latest" {
		t.Errorf("LastInstruction = %q, want %q", got, "latest")
	}
}

func TestLastInstruction_EmptyWhenNoneAtOrAfterDeltaStart(t *testing.T) {
	ru := RealUsers{1: "before"}
	if got := LastInstruction(ru, 3); got != "" {
		t.Errorf("LastInstruction = %q, want \"\"", got)
	}
}

func TestLastInstruction_TruncatesLongText(t *testing.T) {
	long := strings.Repeat("x", previewLen+50)
	ru := RealUsers{0: long}
	got := LastInstruction(ru, 0)
	if got == long {
		t.Error("LastInstruction must truncate via Preview, not return the raw text verbatim")
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("LastInstruction of an over-length string should end in an ellipsis, got %q", got)
	}
}

func TestIsNewTask(t *testing.T) {
	cases := []struct {
		traceChanged, prevNoReply, hasNewInstr bool
		want                                   bool
	}{
		{false, false, false, false},
		{false, false, true, true},
		{false, true, true, false}, // retry of a skipped turn — not a new task
		{true, true, false, true},  // trace change always wins
		{true, false, false, true},
	}
	for _, c := range cases {
		if got := IsNewTask(c.traceChanged, c.prevNoReply, c.hasNewInstr); got != c.want {
			t.Errorf("IsNewTask(%v, %v, %v) = %v, want %v", c.traceChanged, c.prevNoReply, c.hasNewInstr, got, c.want)
		}
	}
}

func TestTaskTitle(t *testing.T) {
	if got := TaskTitle("do the thing", "(fallback)"); got != "do the thing" {
		t.Errorf("TaskTitle with a real instruction = %q, want it verbatim", got)
	}
	if got := TaskTitle("", "(fallback)"); got != "(fallback)" {
		t.Errorf("TaskTitle with no instruction = %q, want the fallback", got)
	}
}

func TestPreview_ShortTextUnchanged(t *testing.T) {
	if got := Preview("hello world"); got != "hello world" {
		t.Errorf("Preview(short) = %q, want unchanged", got)
	}
}

func TestPreview_CollapsesWhitespace(t *testing.T) {
	if got := Preview("hello   \n\t world"); got != "hello world" {
		t.Errorf("Preview must collapse internal whitespace, got %q", got)
	}
}

func TestPreview_TruncatesWithEllipsis(t *testing.T) {
	long := strings.Repeat("a", previewLen+10)
	got := Preview(long)
	r := []rune(got)
	if r[len(r)-1] != '…' {
		t.Errorf("Preview of an over-length string should end in an ellipsis, got %q", got)
	}
	if len(r) != previewLen+1 {
		t.Errorf("Preview length = %d, want %d (previewLen + ellipsis)", len(r), previewLen+1)
	}
}

func TestResponseSummary_StringBody(t *testing.T) {
	s := ResponseSummary("data: {\"choices\":[]}\n\n")
	if s == nil {
		t.Fatal("ResponseSummary(string) should dispatch to chatmsg.ReassembleSSE, not return nil")
	}
}

func TestResponseSummary_UnrecognizedShapeReturnsNil(t *testing.T) {
	if s := ResponseSummary(42); s != nil {
		t.Errorf("ResponseSummary(int) = %+v, want nil for an unrecognized body shape", s)
	}
}

func TestResponseSummary_MapBody(t *testing.T) {
	body := map[string]any{
		"choices": []any{
			map[string]any{
				"finish_reason": "stop",
				"message":       map[string]any{"content": "hi there"},
			},
		},
	}
	s := ResponseSummary(body)
	if s == nil {
		t.Fatal("ResponseSummary(map[string]any) should dispatch to chatmsg.FinalMessage, not return nil")
	}
	if s.Content != "hi there" || s.Finish != "stop" {
		t.Errorf("ResponseSummary = %+v, want Content %q Finish %q", s, "hi there", "stop")
	}
}

func TestPreview_TruncatesMultibyteRunesCleanly(t *testing.T) {
	long := strings.Repeat("你好😀", 30) // well past previewLen in runes, multi-byte throughout
	got := Preview(long)
	r := []rune(got)
	if r[len(r)-1] != '…' {
		t.Errorf("Preview of a multibyte over-length string should end in an ellipsis, got %q", got)
	}
	if len(r) != previewLen+1 {
		t.Errorf("Preview rune length = %d, want %d (previewLen + ellipsis)", len(r), previewLen+1)
	}
	if !utf8.ValidString(got) {
		t.Errorf("Preview produced invalid UTF-8: %q", got)
	}
}
