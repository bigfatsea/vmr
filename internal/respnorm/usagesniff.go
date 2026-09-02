// Ver 2026-09-02 07:20, by pi-agent

package respnorm

// Quota usage sniffing: the stream-riding extraction of the upstream's token
// usage for metering (see the package doc comment's note on this documented
// responsibility overlap with billing). Split out of respnorm.go by the
// archtest file budget on that file; this file owns everything "usage" on
// the stream type — the marker gate, the per-block side classification, and
// the inspection methods the billing side reads.

import (
	"bytes"

	"vmr/internal/chatmsg"
)

// usageFieldMarker is the cheap gate for Quota-Aware Routing's usage
// sniffing (see noteUsage below): almost every SSE token-delta event
// carries no "usage" key at all, so this bytes.Contains check skips a
// full JSON parse for the overwhelming majority of events — the same
// "cheap substring gate before an expensive parse" idiom modelFieldPattern
// and the other markers in this package already use.
var usageFieldMarker = []byte(`"usage"`)
var messageStartMarker = []byte(`"type":"message_start"`)

// noteUsage looks for a "usage" object in b and folds it into the running
// total, tracking WHICH SIDE of the usage ledger the block carried (see
// UsageSides). Called from both emitBlock (streaming) and finalizeBuffered
// (buffered) — the two paths never overlap for one response (see the
// package doc comment on transport modes), so this never double-counts.
// The bytes.Contains gate means the overwhelming majority of streamed
// events (plain content/tool-call deltas) cost one substring scan and
// nothing else; only an event that actually mentions "usage" pays for a
// JSON parse — here two, one block-local (for side classification) and one
// folded into the accumulator.
//
// The per-block side classification is delegated to chatmsg's
// ExtractUsageSides (the single source of truth for the "did this side
// actually get reported" rule, including the Anthropic message_start
// placeholder suppression) rather than re-deriving it here. The merged
// accumulator still uses MergeUsageWithProtocol — its per-side max rule
// (see mergeUsage's doc comment) is what keeps the two sides from leaking
// across events; the side booleans ride the same event-local parse
// ExtractUsageSides performed, so Usage() and UsageSides() can never
// disagree about whether a side was seen.
func (s *stream) noteUsage(b []byte) {
	if !bytes.Contains(b, usageFieldMarker) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ev := range splitEvents(b) {
		if !bytes.Contains(ev, usageFieldMarker) {
			continue
		}
		_, evInOK, evOutOK := chatmsg.ExtractUsageSides(ev, s.protocol)
		s.usage = chatmsg.MergeUsageWithProtocol(ev, s.usage, s.protocol)
		s.usageInSeen = s.usageInSeen || evInOK
		s.usageOutSeen = s.usageOutSeen || evOutOK
	}
}

// splitEvents splits an emitted block into individual SSE events. Blocks are
// delimited by eventSep, but one emitBlock block can carry several events at
// once (decide's emitComplete releases every complete withheld event as a
// single block), and side classification is per-event — see usageBlockSides.
// A body with no event separator (non-SSE JSON) is returned whole.
func splitEvents(b []byte) [][]byte {
	if !bytes.Contains(b, eventSep) {
		return [][]byte{b}
	}
	var out [][]byte
	for {
		i := bytes.Index(b, eventSep)
		if i < 0 {
			if len(b) > 0 {
				out = append(out, b)
			}
			return out
		}
		if i > 0 {
			out = append(out, b[:i])
		}
		b = b[i+len(eventSep):]
	}
}

// Usage returns the token usage extracted from this response so far; ok is
// true once at least ONE side of the usage ledger has actually been parsed.
// ok alone is deliberately NOT the exact-vs-degraded signal for billing: a
// stream truncated after Anthropic's message_start carries real input usage
// but only the placeholder output count, and treating that as authoritative
// usage would bill out≈1 as exact with estimated=0 — worse than no usage at
// all, because estimated_pct is the operator's only trust signal. Callers
// deciding how to charge must consult UsageSides (see quota.go's
// tokenCharge). Safe to call concurrently with an in-flight Read/ingest.
func (s *stream) Usage() (chatmsg.Usage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage, s.usageInSeen || s.usageOutSeen
}

// UsageSides reports which sides of the usage ledger have actually been
// parsed so far. The exact-vs-degraded billing decision needs this split,
// not the single Usage() bool: inSeen without outSeen is Anthropic's
// truncated-after-message_start shape — the input counts are real, while
// the output total is absent (message_start's output_tokens is a ~1
// placeholder, not a generation count). Same concurrency contract as
// Usage().
func (s *stream) UsageSides() (inSeen, outSeen bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usageInSeen, s.usageOutSeen
}
