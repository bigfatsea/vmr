// Ver 2026-07-26, by Sonnet 5
//
// MiniMax-M3-specific response repairs: pattern knowledge for the two
// thinking-mode leaks respnorm.go's transport state machine buffers for
// (see respnorm.go's package doc), plus the soft-block detector. Everything
// here is vendor-specific pattern/marker knowledge; respnorm.go keeps generic
// transport mechanics and calls into this file for MiniMax quirk checks.
package respnorm

import (
	"bytes"
	"regexp"
)

// thinkPattern matches a complete <think>...</think> block plus the
// newline padding MiniMax appends after the closer (JSON-escaped `\n` or `\n`).
var thinkPattern = regexp.MustCompile(`(?s)<think>.*?</think>(?:\\n|\n)*`)

var thinkCloserWithPadding = regexp.MustCompile(`</think>(?:\\\\n|\\n)*`)

// thinkingProcessEndorsement matches the self-endorsement marker the
// model uses to signal "end of thinking, start of final response".
var thinkingProcessEndorsement = regexp.MustCompile(`Looks good\.[ \t]+Pro(?:ceed)?`)

// thinkingProcessNumberedMarker approximates the numbered-subsection shape
// of MiniMax's leaked thinking-mode outline ("1. **Analyze**", …) as it appears
// JSON-escaped inside content. Used only as an observation signal.
var thinkingProcessNumberedMarker = regexp.MustCompile(`\\n\s*[1-9]\d?\.`)

// thinkingProcessFullMatch captures the endorsement and the rest of its
// line; the final response starts on the same line right after the marker.
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

func extractJSONStringPrefix(v []byte) []byte {
	for j := 0; j < len(v); j++ {
		if v[j] == '"' && (j == 0 || v[j-1] != '\\') {
			return v[:j]
		}
	}
	return v
}

// stripFirstThink removes the first matching <think>...</think> block plus any
// trailing newline padding. Returns the stripped slice and true if found.
func stripFirstThink(b []byte) ([]byte, bool) {
	if loc := thinkPattern.FindIndex(b); loc != nil {
		res := make([]byte, 0, len(b)-(loc[1]-loc[0]))
		res = append(res, b[:loc[0]]...)
		res = append(res, b[loc[1]:]...)
		return res, true
	}
	if !bytes.Contains(b, thinkCloseMarker) || !thinkShapeGuard(b) {
		return b, false
	}
	lines := bytes.Split(b, eventSep)
	closerIdx := -1
	for i, line := range lines {
		if bytes.Contains(line, thinkCloseMarker) {
			closerIdx = i
			break
		}
	}
	if closerIdx < 0 {
		return b, false
	}
	keep := make([][]byte, 0, len(lines))
	for i := 0; i < closerIdx; i++ {
		if bytes.Index(lines[i], contentFieldMarker) < 0 && bytes.Index(lines[i], textFieldMarker) < 0 {
			keep = append(keep, lines[i])
		}
	}
	closerLine := lines[closerIdx]
	if loc := thinkCloserWithPadding.FindIndex(closerLine); loc != nil {
		cfIdx, mlen := bytes.Index(closerLine, contentFieldMarker), len(contentFieldMarker)
		if cfIdx < 0 {
			cfIdx, mlen = bytes.Index(closerLine, textFieldMarker), len(textFieldMarker)
		}
		if cfIdx >= 0 && cfIdx+mlen <= loc[0] {
			newLine := make([]byte, 0, len(closerLine))
			newLine = append(newLine, closerLine[:cfIdx+mlen]...)
			newLine = append(newLine, closerLine[loc[1]:]...)
			closerLine = newLine
		}
	}
	keep = append(keep, closerLine)
	keep = append(keep, lines[closerIdx+1:]...)
	return bytes.Join(keep, eventSep), true
}

func matchThinkingPrefix(acc []byte, isContent bool) verdict {
	if bytes.HasPrefix(acc, thinkOpenMarker) || (isContent && bytes.HasPrefix(acc, thinkingProcessPrefix)) {
		return verdictBuffered
	}
	if bytes.HasPrefix(thinkOpenMarker, acc) || (isContent && bytes.HasPrefix(thinkingProcessPrefix, acc)) {
		return verdictUndecided
	}
	return verdictPassthrough
}

// thinkShapeGuard reports whether the initial non-empty content/text in b
// starts with <think> (accumulated across chunks if split).
func thinkShapeGuard(b []byte) bool {
	var acc []byte
	for off := 0; off < len(b); {
		idx, mlen := -1, 0
		for _, m := range thinkGuardMarkers {
			if j := bytes.Index(b[off:], m); j >= 0 && (idx < 0 || off+j < idx) {
				idx, mlen = off+j, len(m)
			}
		}
		if idx < 0 {
			break
		}
		v := b[idx+mlen:]
		if len(acc) == 0 {
			v = trimEscapedWS(v)
		}
		strVal := extractJSONStringPrefix(v)
		if len(strVal) > 0 {
			acc = append(acc, strVal...)
			if bytes.HasPrefix(acc, thinkOpenMarker) {
				return true
			}
			if !bytes.HasPrefix(thinkOpenMarker, acc) {
				return false
			}
		}
		off = idx + mlen
	}
	return bytes.HasPrefix(acc, thinkOpenMarker)
}

// softBlockMarkers flag MiniMax's "soft" compliance blocks.
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

// ContainsSoftBlockMarker is the exported entry point for router soft-block failover.
func ContainsSoftBlockMarker(b []byte) bool { return containsSoftBlockMarker(b) }

// stripThinkingProcess removes MiniMax M3's "Thinking Process:" section
// from a fully buffered response. Returns b unchanged if not detected.
func stripThinkingProcess(b []byte) []byte {
	cf := bytes.Index(b, contentFieldMarker)
	if cf < 0 || !bytes.HasPrefix(trimEscapedWS(b[cf+len(contentFieldMarker):]), thinkingProcessPrefix) {
		return b
	}
	loc := thinkingProcessFullMatch.FindAllIndex(b, -1)
	if len(loc) == 0 {
		return b
	}
	last := loc[len(loc)-1]
	endorseLoc := thinkingProcessEndorsement.FindIndex(b[last[0]:last[1]])
	if endorseLoc == nil {
		return b
	}
	cut := last[0] + endorseLoc[1]
	lines := bytes.Split(b, eventSep)
	markerLineIdx := -1
	for i, line := range lines {
		if thinkingProcessFullMatch.FindIndex(line) != nil {
			markerLineIdx = i
		}
	}
	if markerLineIdx < 0 || markerLineIdx >= len(lines) {
		return b
	}

	keep := make([][]byte, 0, len(lines)-markerLineIdx+1)
	if markerLineIdx > 0 {
		keep = append(keep, lines[0])
	}
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
	keep = append(keep, lines[markerLineIdx+1:]...)
	return bytes.Join(keep, eventSep)
}
