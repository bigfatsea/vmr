// Ver 2026-07-26, by Sonnet 5
//
// MiniMax-M3-specific response repairs: pattern knowledge for the two
// thinking-mode leaks respnorm.go's transport state machine buffers for
// (see respnorm.go's package doc), plus the soft-block detector. Everything
// here is vendor-specific pattern/marker knowledge; respnorm.go keeps the
// generic transport mechanics (event splitting, model-field rewrite, [DONE]
// policy, the buffered/passthrough decision) and calls into this file only
// at the points where it needs to know "does this look like a MiniMax
// quirk". Originally split out of internal/router's response.go — pure
// move, no behavior change: respnorm.go and minimax.go together are
// byte-for-byte the same logic that lived in one file before that first
// split, and moved again (with router.go/responsefix.go renamed to
// respnorm.go/minimax.go) into this standalone package.
package respnorm

import (
	"bytes"
	"regexp"
)

// thinkPattern matches a complete <think>...</think> block plus the
// newline padding MiniMax appends after the closer. Content inside SSE
// data lines is JSON-escaped, so the padding is the two-character
// sequence `\n` (backslash-n, matched by `\\n`); real newlines (`\n`)
// are also accepted for non-escaped contexts. Without eating the
// padding, the assistant message starts with two blank lines every turn.
var thinkPattern = regexp.MustCompile(`(?s)<think>.*?</think>(?:\\n|\n)*`)

// thinkingProcessEndorsement matches the self-endorsement marker the
// model uses to signal "end of thinking, start of final response".
var thinkingProcessEndorsement = regexp.MustCompile(`Looks good\.[ \t]+Pro(?:ceed)?`)

// thinkingProcessNumberedMarker approximates the numbered-subsection shape
// of MiniMax's leaked thinking-mode outline ("1. **Analyze**", "2.
// **Draft**", …) as it appears JSON-escaped inside a content/text string
// value: an escaped newline immediately followed by a small integer and a
// period. Used only as an observation signal (see
// stream.noteThinkingPatternIfSuspected in respnorm.go) when
// stripThinkingProcess's own literal "Thinking Process:" prefix guard did
// NOT fire — a content-shape proxy for "this still looks like the leaked
// outline even though the literal marker guard missed it", not a strip
// trigger itself.
var thinkingProcessNumberedMarker = regexp.MustCompile(`\\n\s*[1-9]\d?\.`)

// thinkingProcessFullMatch captures the endorsement and the rest of its
// line; the final response starts on the same line right after the
// marker.
var thinkingProcessFullMatch = regexp.MustCompile(`[ \t]*Looks good\.[ \t]+Pro(?:ceed)?[^\n]*`)

var (
	thinkingProcessPrefix = []byte("Thinking Process:")
	thinkOpenMarker       = []byte("<think>")
	thinkCloseMarker      = []byte("</think>")
)

// thinkGuardMarkers are the payload fields whose value can carry MiniMax's
// inline-think pathology: openai-completions delta/message content and anthropic-messages text.
// Dedicated reasoning fields (reasoning_content, thinking) are well-behaved
// by construction and never guard-checked.
var thinkGuardMarkers = [][]byte{contentFieldMarker, textFieldMarker}

// thinkShapeGuard reports whether the FIRST non-empty content/text value in b
// starts with <think> — the only shape the think-strip repair targets
// (MiniMax-M3 thinking mode opens the reply with the tag). Without this
// guard, a reply that merely QUOTES a <think>…</think> block mid-text (a
// user asking about the tag format, a code sample echoing it) would have the
// quoted span silently deleted. Symmetric with stripThinkingProcess's own
// first-content-prefix guard; kept as cheap byte scans, same as the rest of
// this file.
func thinkShapeGuard(b []byte) bool {
	for off := 0; off < len(b); {
		idx, mlen := -1, 0
		for _, m := range thinkGuardMarkers {
			if j := bytes.Index(b[off:], m); j >= 0 && (idx < 0 || off+j < idx) {
				idx, mlen = off+j, len(m)
			}
		}
		if idx < 0 {
			return false
		}
		v := trimEscapedWS(b[idx+mlen:])
		if len(v) > 0 && v[0] != '"' { // first non-empty value decides
			return bytes.HasPrefix(v, thinkOpenMarker)
		}
		off = idx + mlen
	}
	return false
}

// softBlockMarkers flag MiniMax's "soft" content-policy block: a 2xx
// response that embeds a compliance flag instead of erroring. Detection is
// observation-only (see response.go's package doc) — it only adds
// "soft_block_detected" to the audit norm trail so these otherwise-invisible
// blocks become greppable instead of silently reaching the client as an
// empty or substituted response.
var softBlockMarkers = [][]byte{
	[]byte(`"input_sensitive":true`),
	[]byte(`"output_sensitive":true`),
}

func containsSoftBlockMarker(b []byte) bool {
	for _, m := range softBlockMarkers {
		if bytes.Contains(b, m) {
			return true
		}
	}
	return false
}

// ContainsSoftBlockMarker is the exported entry point for
// containsSoftBlockMarker: internal/router calls it on a fully buffered 2xx
// body when an endpoint opted into soft_block_failover, to decide (together
// with an empty-content check) whether to fail over instead of forwarding.
func ContainsSoftBlockMarker(b []byte) bool { return containsSoftBlockMarker(b) }

// stripThinkingProcess removes MiniMax M3's "Thinking Process:"
// structured thinking section from a fully buffered response, preserving
// the SSE framing of every other data: line. Returns b unchanged if the
// pattern is not detected — better to pass through than to drop the
// actual response.
//
// The shape MiniMax emits under thinking=medium is a plain-text thinking
// section starting with "Thinking Process:", numbered subsections, a
// "Final Polish" draft area, and a "Looks good. Pro"/"Looks good.
// Proceed" self-endorsement marker; the final response starts on the
// marker's own line, right after the marker. Thinking and final response
// usually live in different data: lines, so the strip drops the lines
// between the role marker and the marker line, then trims the marker
// line's content up to the end of the endorsement.
func stripThinkingProcess(b []byte) []byte {
	// Trigger guard: only fire when the response's FIRST content value
	// begins with the literal "Thinking Process:". Without this, any
	// legitimate reply containing "Looks good. Pro…" (a code review
	// saying "Looks good. Proceed with the merge") would have the
	// chunks before the marker silently dropped.
	cf := bytes.Index(b, contentFieldMarker)
	if cf < 0 || !bytes.HasPrefix(trimEscapedWS(b[cf+len(contentFieldMarker):]), thinkingProcessPrefix) {
		return b
	}
	// Find the LAST self-endorsement marker.
	loc := thinkingProcessFullMatch.FindAllIndex(b, -1)
	if len(loc) == 0 {
		return b
	}
	last := loc[len(loc)-1]
	endorseLoc := thinkingProcessEndorsement.FindIndex(b[last[0]:last[1]])
	if endorseLoc == nil {
		return b
	}
	// cut is the byte offset immediately after "Pro" / "Proceed" — the
	// start of the actual final response text within the marker's line.
	cut := last[0] + endorseLoc[1]

	// Split into data: lines on the SSE separator (JSON-escaped content
	// contains no real "\n\n", so the split is safe).
	lines := bytes.Split(b, eventSep)
	// Locate the LAST line containing the marker — the model may emit
	// "Looks good. Pro" inside the thinking section too.
	markerLineIdx := -1
	for i, line := range lines {
		if thinkingProcessFullMatch.FindIndex(line) != nil {
			markerLineIdx = i
		}
	}
	if markerLineIdx < 0 || markerLineIdx >= len(lines) {
		return b
	}

	// Drop lines between the first line and the marker line (the
	// thinking-only content). The first line (role marker) is kept —
	// unless the marker IS the first line (non-streaming JSON is a
	// single "line"), where keeping it would duplicate the whole body.
	keep := make([][]byte, 0, len(lines)-markerLineIdx+1)
	if markerLineIdx > 0 {
		keep = append(keep, lines[0])
	}
	// Trim the marker line's content to start after "Pro"/"Proceed".
	lineStart := 0
	for i := 0; i < markerLineIdx; i++ {
		lineStart += len(lines[i]) + len(eventSep)
	}
	cutInLine := cut - lineStart
	markerLine := lines[markerLineIdx]
	if cutInLine > 0 && cutInLine < len(markerLine) {
		cfIdx := bytes.Index(markerLine, contentFieldMarker)
		if cfIdx >= 0 {
			contentValueStart := cfIdx + len(contentFieldMarker)
			if contentValueStart <= cutInLine {
				newLine := make([]byte, 0, len(markerLine))
				newLine = append(newLine, markerLine[:contentValueStart]...)
				newLine = append(newLine, markerLine[cutInLine:]...)
				markerLine = newLine
			}
		}
	}
	keep = append(keep, markerLine)
	// Keep all lines after the marker line verbatim — including a
	// trailing empty element from splitting a body that ends in "\n\n",
	// so the final SSE separator survives the re-join.
	keep = append(keep, lines[markerLineIdx+1:]...)
	return bytes.Join(keep, eventSep)
}
