// Ver 2026-08-14, by Sonnet 5

// Package taskseg isolates agent-specific offline analytics heuristics: identifying
// genuine user instructions, transport scaffolding, no-reply conventions, and framework session IDs.
package taskseg

import "vmr/internal/chatmsg"

// Profile defines agent-specific segmentation conventions used by report and story.
type Profile interface {
	Name() string
	// RealUserText reports whether a user-role message is an actual
	// instruction rather than transport scaffolding, returning the
	// (possibly envelope-stripped) instruction text when true. rawMsgs/
	// rawIdx let an implementation inspect the message's original
	// (pre-render) shape — e.g. to tell "pure tool_result parts" apart from
	// real content — pass rawIdx < 0 or out of range when unavailable;
	// implementations must treat that as "no raw shape to check", not panic.
	RealUserText(m chatmsg.Message, rawMsgs []any, rawIdx int) (string, bool)
	// NoReply reports whether a response's reassembled finish reason and
	// content mean "the model deliberately skipped replying this turn"
	// (empty content, or an explicit skip marker) — the following record
	// is then a retry of THIS turn's instruction, not a new one.
	NoReply(finish, content string) bool
	// ChatID extracts a framework-specific session identifier from a
	// request's message list, when the transport carries one at all (e.g.
	// OpenClaw's "Conversation info (untrusted metadata)" wrapper) — "" when
	// absent or when the profile has no such convention.
	ChatID(msgs []chatmsg.Message) string
}
