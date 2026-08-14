// Ver 2026-08-15, by Sonnet 5

package taskseg

import (
	"strings"
	"testing"

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

func TestIndexRealUsers_StoresRawNotPreview(t *testing.T) {
	long := strings.Repeat("a ", 200) // far past previewLen once trimmed
	msgs := []chatmsg.Message{{Role: "user", Text: long}}
	ru := IndexRealUsers(Generic, msgs, nil, 0)
	if ru[0] != long {
		t.Error("IndexRealUsers must store the raw, untruncated text — truncation happens on read")
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
