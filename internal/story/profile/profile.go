// Ver 2026-07-28 23:05, by Sonnet 5

// Package profile isolates the one part of internal/story that is
// genuinely agent-specific: telling a real user instruction apart from
// transport scaffolding (routing envelopes, tool-result-only messages,
// timestamp pings), and recognizing an agent's "I deliberately said
// nothing this turn" convention.
//
// Step 1 ships exactly one implementation (OpenClawAware) plus a
// template-free fallback (Generic) — no Detect-based dispatch/registry yet.
// Design doc §11 D5: that's a deliberate YAGNI call, not an oversight —
// build the registry when a second real profile (Pi/Hermes) actually needs
// one, driven by what real corpus differences turn out to matter, not by
// guessing now.
package profile

import "vmr/internal/chatmsg"

// Profile is the seam internal/story calls through instead of hardcoding
// one agent's conventions.
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
}
