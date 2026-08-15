// Ver 2026-08-15, by Sonnet 5

package taskseg

import (
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

// RealUsers indexes one request's real user instructions by absolute
// message index -> Preview of the instruction text. Built once per request
// via IndexRealUsers and consumed by HasNewInstruction/LastInstruction —
// the single point where a request's real-instruction regex work happens,
// no matter how many boundary/title decisions afterward read from it.
//
// The stored value is already Preview'd, and that placement is the whole
// point rather than an implementation detail (it reverses B3's original
// "store raw, truncate on read" decision — see IndexRealUsers). Preview is
// idempotent, so every consumer still calls it on the way out and nothing
// downstream depends on which side of the map the truncation happened on;
// exactly one constant (previewLen) still decides the length, so the two
// commands cannot drift apart on it either.
type RealUsers map[int]string

// IndexRealUsers scans msgs once, calling prof.RealUserText for every
// user-role message and storing Preview of what it returns. report's
// collect() and story's buildFrom() each call this exactly once per
// request, in their own main per-request scan, and pass the result down to
// HasNewInstruction/LastInstruction/a stitch-boundary title lookup instead
// of re-scanning for each — story used to rerun the same regex up to 2-3
// times per request this way.
//
// Why Preview here and not in the consumers (B3 stored the raw text and
// truncated on read; that is deliberately reversed): report keeps one of
// these indexes alive per RECORD for the whole corpus (SessionAnalysis
// holds every ReqInfo), and every record carries the session's entire
// history — so the same instruction's full text would be retained once per
// later record in that session, growing quadratically with session length.
// Two further multipliers made raw storage worse than it looks: an agent
// instruction can be many KB, and a dialect that strips a metadata envelope
// returns a SUBSLICE of the message (Go substrings share the backing
// array), which pins the whole original message even when the useful part
// is one line. No consumer ever wanted the raw text — all three of them
// (LastInstruction, FirstInstruction, story's stitch-boundary title) Preview
// it immediately, and HasNewInstruction reads only the keys.
func IndexRealUsers(prof Profile, msgs []chatmsg.Message, rawMsgs []any, off int) RealUsers {
	ru := RealUsers{}
	for i, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if text, ok := prof.RealUserText(m, rawMsgs, i-off); ok {
			ru[i] = Preview(text)
		}
	}
	return ru
}

// ManifestKeySet is m's Keys as a set — the prevKeys shape
// HasNewInstruction wants, built once by whichever caller has just
// classified this manifest against its parent (report's attach(), story's
// buildFrom()) rather than each repeating the same small loop. m may be
// nil (no parent manifest to compare against), matching HasNewInstruction's
// own documented nil-prevKeys contract.
func ManifestKeySet(m *ctxgraph.Manifest) map[ctxgraph.Hash]bool {
	if m == nil {
		return nil
	}
	set := make(map[ctxgraph.Hash]bool, len(m.Keys))
	for _, k := range m.Keys {
		set[k] = true
	}
	return set
}

// HasNewInstruction reports whether ru holds a real user instruction near
// the request's end (within chatmsg.NewUserWindow of total) whose content
// isn't already present somewhere in the parent manifest. prevKeys (the
// parent's message-hash set) makes this a set check rather than a position
// check: an in-place history edit (e.g. image pruning) can shift an old
// user message into the tail window without it being new. prevKeys may be
// nil (the request opens a session with no parent) — a nil map read
// returns false for every key, which is exactly "nothing to exclude", so
// no separate nil check is needed here.
func HasNewInstruction(ru RealUsers, prevKeys map[ctxgraph.Hash]bool, cur *ctxgraph.Manifest, deltaStart, total int) bool {
	for idx := range ru {
		if idx < deltaStart || idx < total-chatmsg.NewUserWindow {
			continue
		}
		if ki := idx - cur.LeadSys; ki >= 0 && ki < len(cur.Keys) && prevKeys[cur.Keys[ki]] {
			continue // identical content already existed in the parent — shifted, not new
		}
		return true
	}
	return false
}

// LastInstruction returns the preview of the newest real user instruction
// at or after deltaStart — "" when this turn is a pure tool-loop step.
// Unlike HasNewInstruction, this has no chatmsg.NewUserWindow bound: it
// picks the task's TITLE, which should reflect whatever the user actually
// asked even when that isn't near the request's very end.
func LastInstruction(ru RealUsers, deltaStart int) string {
	best := -1
	for idx := range ru {
		if idx >= deltaStart && idx > best {
			best = idx
		}
	}
	if best < 0 {
		return ""
	}
	return Preview(ru[best])
}

// FirstInstruction returns the preview of the EARLIEST real user instruction
// in ru — the conversation's opening ask, as opposed to LastInstruction's
// most-recent turn. "" when ru is empty. Reads an already-built RealUsers
// index rather than re-scanning a message list, the same single-index
// discipline IndexRealUsers exists to enforce: report's sessionTitle,
// story's Journey title, and story's cheap PreviewTitle path each used to
// hand-roll this same earliest-index search independently.
func FirstInstruction(ru RealUsers) string {
	best := -1
	for idx := range ru {
		if best < 0 || idx < best {
			best = idx
		}
	}
	if best < 0 {
		return ""
	}
	return Preview(ru[best])
}

// IsNewTask is the one task-boundary rule both report's session.go and
// story's journey.go apply: a trace-id change always opens a new task (a
// new client-side request chain, independent of message content);
// otherwise a genuinely new real user instruction does — UNLESS the
// previous turn ended in a deliberate no-reply (prevNoReply), in which case
// the "new" instruction is a retry of the skipped turn, not a fresh one.
func IsNewTask(traceChanged, prevNoReply, hasNewInstr bool) bool {
	return traceChanged || (!prevNoReply && hasNewInstr)
}

// TaskTitle returns newInstruction when non-empty, else fallback. Takes the
// fallback as a parameter instead of importing internal/i18n: report's one
// full-corpus analysis pass runs once, not once per language (its fallback
// is a fixed English placeholder — re-running the pass per language just
// for a rare placeholder string isn't worth it), while story passes its own
// localized i18n.Story(lang).ToolLoopTitle. taskseg stays a leaf that
// doesn't depend on the rendering layer either way.
func TaskTitle(newInstruction, fallback string) string {
	if newInstruction != "" {
		return newInstruction
	}
	return fallback
}

// ResponseSummary reassembles a recorded client response body (SSE string
// or JSON object) into the model's output.
func ResponseSummary(body any) *chatmsg.StreamSummary {
	switch b := body.(type) {
	case string:
		return chatmsg.ReassembleSSE(b)
	case map[string]any:
		if s, ok := chatmsg.FinalMessage(b); ok {
			return s
		}
	}
	return nil
}

// previewLen bounds Preview's single-line excerpt.
const previewLen = 80

// Preview returns a single-line, length-capped excerpt of s: internal
// whitespace collapsed to single spaces, then capped at previewLen runes
// with a trailing ellipsis when truncated.
//
// Idempotent — Preview(Preview(s)) == Preview(s) — which is what lets
// IndexRealUsers store an already-previewed value while every consumer
// keeps calling Preview on the way out (see RealUsers). A truncated result
// is previewLen runes plus the ellipsis, so re-previewing re-cuts at the
// same previewLen boundary and re-appends the same ellipsis. Pinned by
// TestPreviewIsIdempotent.
func Preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > previewLen {
		return string(r[:previewLen]) + "…"
	}
	return s
}
