// Ver 2026-08-14, by Sonnet 5

// Package taskseg isolates the one part of vmr's offline analytics that is
// genuinely agent-specific: telling a real user instruction apart from
// transport scaffolding (routing envelopes, tool-result-only messages,
// timestamp pings), recognizing an agent's "I deliberately said nothing this
// turn" convention, and extracting a framework-specific session identifier
// (chat_id) when the transport carries one. This file holds that seam
// (Profile) plus its two implementations (openclaw.go, generic.go);
// segment.go holds the session/task-segmentation algorithm itself
// (real-instruction indexing, new-task/new-instruction detection, task
// titling, response reassembly, preview truncation) — generic over Profile,
// not agent-specific on its own, but converged here in a shared package
// because report/session.go and story/journey.go used to
// each carry an independent copy of it.
//
// internal/report's session.go and internal/story's journey.go both call
// through Profile instead of each hardcoding one agent's conventions —
// report used to carry its own byte-identical copy of these heuristics
// (previously duplicated across report and story) — a name change from that package's own to reflect that it's no
// longer story-exclusive: this is the leaf both consumers depend on, never
// the reverse.
//
// Currently provides one implementation (OpenClawAware) plus a template-free fallback (Generic) — no registry yet (deliberate YAGNI call, not an oversight — build the registry when
// a second real profile (Pi/Hermes) actually needs one, driven by what real
// corpus differences turn out to matter, not by guessing now.
package taskseg

import "vmr/internal/chatmsg"

// Profile is the seam internal/report and internal/story both call through
// instead of hardcoding one agent's conventions.
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
