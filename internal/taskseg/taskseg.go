// Ver 2026-08-14, by Sonnet 5

// Package taskseg isolates the one part of vmr's offline analytics that is
// genuinely agent-specific: telling a real user instruction apart from
// transport scaffolding (routing envelopes, tool-result-only messages,
// timestamp pings), recognizing an agent's "I deliberately said nothing this
// turn" convention, and extracting a framework-specific session identifier
// (chat_id) when the transport carries one.
//
// internal/report's session.go and internal/story's journey.go both call
// through Profile instead of each hardcoding one agent's conventions —
// report used to carry its own byte-identical copy of these heuristics
// (session.go originally, before the architecture review's B2 refactor
// batch moved it here alongside story's pre-existing internal/story/profile
// package) — a name change from that package's own to reflect that it's no
// longer story-exclusive: this is the leaf both consumers depend on, never
// the reverse.
//
// Step 1 ships exactly one implementation (OpenClawAware) plus a
// template-free fallback (Generic) — no Detect-based dispatch/registry yet.
// That's a deliberate YAGNI call, not an oversight — build the registry when
// a second real profile (Pi/Hermes) actually needs one, driven by what real
// corpus differences turn out to matter, not by guessing now.
package taskseg

import "vmr/internal/chatmsg"

// Profile is the seam internal/report and internal/story both call through
// instead of hardcoding one agent's conventions.
type Profile interface {
	Name() string
	// IsRealUser reports whether a user-role message is an actual
	// instruction rather than transport scaffolding. rawMsgs/rawIdx let an
	// implementation inspect the message's original (pre-render) shape —
	// e.g. to tell "pure tool_result parts" apart from real content — pass
	// rawIdx < 0 or out of range when unavailable; implementations must
	// treat that as "no raw shape to check", not panic.
	IsRealUser(m chatmsg.Message, rawMsgs []any, rawIdx int) bool
	// RealUserText is IsRealUser's sibling that also returns the
	// (possibly envelope-stripped) instruction text when true.
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
