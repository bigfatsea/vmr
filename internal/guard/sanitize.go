// Ver 2026-09-23 03:33, by Doubao Seed 2.0

// Unicode steganography online sanitizer. The
// classification decision (which code point falls in which tier) is
// ClassifyRune's (runes.go) -- this file only adds the DELETION action on
// top of it: a
// generic JSON string-value walk (parallel to walk.go's, but a rewrite
// pass instead of a scan -- see below for why it's a separate walker
// rather than a reuse of Engine's) that splices A-tier (and, depending on
// config, B-tier) code points out of every string value in one SSE
// event's JSON body, leaves C-tier untouched always, and returns
// the original bytes unchanged (zero allocation) whenever nothing needed
// touching -- the common case for the overwhelming majority of real
// traffic per the corpus baseline.
//
// A separate walker rather than reusing walk.go's Engine-coupled
// walkValue/walkObject/walkArray: those exist to populate a Scratch's
// Finding buffer for credential SCANNING (Tier/rule/path bookkeeping baked
// in throughout); this is a byte-splice REWRITE with a completely
// different accumulation shape (an edit list reconstructed into a new
// buffer) and no Engine/Rule/Tier concept at all. Forcing one traversal to
// serve both would mean threading a "scan vs rewrite" mode through
// walk.go's already-tested scanning code for no benefit -- the traversal
// skeleton itself is genuinely small (SkipJSONString/SkipJSONWS/SkipJSONValue
// composition), so duplicating just that skeleton here is cheaper and
// safer than coupling two unrelated operations to one shared walker.
//
// Both wire forms are handled per string value (escape-aware): literal
// UTF-8 bytes (JSON permits raw non-ASCII inside a
// string) and \uXXXX / surrogate-pair \uXXXX\uXXXX escapes -- an attacker
// can equally well spell a zero-width character as six literal ASCII
// bytes, which a byte-level scanner never looking for \u would miss
// entirely.
package guard

import (
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"vmr/internal/jsonscan"
)

// shouldStrip decides one classified rune's fate. A-tier (Tags/Control) is
// always stripped once sanitization is on at all -- there is no legitimate
// use of either in API text. C-tier (varsel) is NEVER
// stripped (required by real scripts and every ZWJ emoji sequence).
// Everything else classified is B-tier, always stripped too: the former
// strip/flag_only middle rung was removed after the calibration corpus
// showed B-tier's residual harm is visual only (a handful of occurrences
// in months of real traffic), so keeping it as a knob was methodology
// symmetry without a use case.
func shouldStrip(cat string) bool {
	switch cat {
	case "":
		return false
	case RuneCatVarSel:
		return false
	default:
		return true
	}
}

// runeEdit is one string value's replacement, keyed by its byte range
// (excluding the surrounding quotes) in the original event bytes.
type runeEdit struct {
	start, end int
	repl       []byte
}

type sanitizeState struct {
	dst   map[string]int
	edits []runeEdit
}

// SanitizeEventJSON walks every string value in raw (one SSE event's JSON
// data payload) and strips A-tier (always) and B-tier (always -- see
// shouldStrip) code points, counting each STRIPPED occurrence into dst --
// dst is the "what this call actually removed" tally that becomes
// Record.Guard.SanitizedRunes, so a C-tier code point (classified but
// never stripped) is never counted here even though ClassifyRune
// recognizes it. ClassifyRunes (runes.go) is the separate offline function
// that counts every classified occurrence regardless of tier, for the
// different question "what survived in the final response" -- conflating
// the two would make SanitizedRunes claim credit for characters (e.g. a
// legitimate emoji's variation selector) the online sanitizer never
// touched. Malformed JSON stops the walk at the point it can no longer
// make progress (sanitizeWalkValue's return value, deliberately unchecked
// here) -- same failure mode as walk.go's walkStrings -- but any edits
// already recorded from the well-formed prefix scanned before that point
// are still applied: a genuinely truncated event (the connection cut
// mid-response, not a malformed one a real provider would ever send) is
// exactly the case Fail-Open exists for, and discarding runes this call
// already found and safely identified, just because a later, unrelated
// part of the same event turned out malformed, would leave strictly MORE
// invisible content in front of the client for no safety benefit -- the
// opposite of what this function exists to do.
//
// Fast path: when raw contains no byte >= 0x80 and no literal "\u", no
// A/B/C-tier code point can possibly be present in either wire form (JSON
// requires escaping every control byte below 0x20, and \t/\n/\r -- the only
// ones exempt from that requirement -- are explicitly excluded from every
// tier by ClassifyRune), so this returns immediately with changed=false and
// zero allocation -- the common case for the overwhelming majority of real
// (ASCII) traffic.
func SanitizeEventJSON(raw []byte, dst map[string]int) (out []byte, changed bool, dstOut map[string]int) {
	if !needsRuneScan(raw) {
		return raw, false, dst
	}
	st := &sanitizeState{dst: dst}
	i := jsonscan.SkipJSONWS(raw, 0)
	sanitizeWalkValue(raw, i, 0, st)
	if len(st.edits) == 0 {
		return raw, false, st.dst
	}
	out = make([]byte, 0, len(raw))
	cursor := 0
	for _, e := range st.edits {
		out = append(out, raw[cursor:e.start]...)
		out = append(out, e.repl...)
		cursor = e.end
	}
	out = append(out, raw[cursor:]...)
	return out, true, st.dst
}

func isControlOrDEL(b byte) bool {
	return (b <= 0x08) || b == 0x0B || b == 0x0C || (b >= 0x0E && b <= 0x1F) || b == 0x7F
}

func needsRuneScan(raw []byte) bool {
	for i, b := range raw {
		if b >= 0x80 || isControlOrDEL(b) {
			return true
		}
		if b == '\\' && i+1 < len(raw) && raw[i+1] == 'u' {
			return true
		}
	}
	return false
}

func sanitizeWalkValue(raw []byte, i, depth int, st *sanitizeState) int {
	if depth > maxWalkDepth || i < 0 || i >= len(raw) {
		return -1
	}
	switch raw[i] {
	case '"':
		end, ok := jsonscan.SkipJSONString(raw, i)
		if !ok {
			return -1
		}
		contentStart, contentEnd := i+1, end-1
		if repl, changed, dst := sanitizeStringContent(raw[contentStart:contentEnd], st.dst); changed {
			st.edits = append(st.edits, runeEdit{start: contentStart, end: contentEnd, repl: repl})
			st.dst = dst
		} else {
			st.dst = dst
		}
		return end
	case '{':
		return sanitizeWalkObject(raw, i, depth+1, st)
	case '[':
		return sanitizeWalkArray(raw, i, depth+1, st)
	default:
		end, ok := jsonscan.SkipJSONValue(raw, i)
		if !ok {
			return -1
		}
		return end
	}
}

func sanitizeWalkObject(raw []byte, i, depth int, st *sanitizeState) int {
	i++ // past '{'
	for {
		i = jsonscan.SkipJSONWS(raw, i)
		if i >= len(raw) {
			return -1
		}
		switch raw[i] {
		case '}':
			return i + 1
		case ',':
			i++
			continue
		case '"':
			// key follows
		default:
			return -1
		}
		var ok bool
		i, ok = jsonscan.SkipJSONString(raw, i) // the key itself is never sanitized
		if !ok {
			return -1
		}
		i = jsonscan.SkipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return -1
		}
		i = jsonscan.SkipJSONWS(raw, i+1)
		i = sanitizeWalkValue(raw, i, depth, st)
		if i < 0 {
			return -1
		}
	}
}

func sanitizeWalkArray(raw []byte, i, depth int, st *sanitizeState) int {
	i++ // past '['
	for {
		i = jsonscan.SkipJSONWS(raw, i)
		if i >= len(raw) {
			return -1
		}
		switch raw[i] {
		case ']':
			return i + 1
		case ',':
			i++
			continue
		default:
			i = sanitizeWalkValue(raw, i, depth, st)
			if i < 0 {
				return -1
			}
		}
	}
}

// sanitizeStringContent processes content -- one JSON string's bytes
// EXCLUDING the surrounding quotes -- and returns the same bytes with
// every code point shouldStrip approves of removed, in whichever wire form
// it was spelled (literal UTF-8 or a \uXXXX/surrogate-pair escape).
// Copy-on-write: out stays nil (and content is returned verbatim) until the
// first actual deletion, so a string with no A/B-tier content -- the
// overwhelming majority even once needsRuneScan's fast path is bypassed by
// an ordinary "\n" or a legitimate non-ASCII script -- allocates nothing.
func sanitizeStringContent(content []byte, dst map[string]int) (out []byte, changed bool, dstOut map[string]int) {
	var offset int
	ScanEscaped(content, func(r rune, rawSpan []byte, isEscape bool, ok bool) bool {
		spanLen := len(rawSpan)
		if !ok {
			if out != nil {
				out = append(out, rawSpan...)
			}
			offset += spanLen
			return true
		}
		var strip bool
		var cat string
		if !isEscape && spanLen == 1 && rawSpan[0] < 0x80 {
			if isControlOrDEL(rawSpan[0]) {
				strip = true
				cat = RuneCatControl
			}
		} else {
			cat = ClassifyRune(r)
			strip = shouldStrip(cat)
		}
		if strip {
			if dst == nil {
				dst = make(map[string]int, 4)
			}
			dst[cat]++
			if out == nil {
				out = append(out, content[:offset]...)
			}
			changed = true
			offset += spanLen
			return true
		}
		if out != nil {
			out = append(out, rawSpan...)
		}
		offset += spanLen
		return true
	})
	if !changed {
		return content, false, dst
	}
	return out, true, dst
}

// ScanEscaped walks raw bytes of JSON string content, resolving \uXXXX
// escapes (including UTF-16 surrogate pairs) and raw UTF-8 runes while
// correctly treating non-\u two-byte escapes (\\, \", \n, \r, \t, etc.) as
// indivisible units so that escaped backslashes (e.g. "\\u0041") are never
// misread as the start of a fresh \u escape on the next iteration.
//
// For each element, fn is invoked with:
//   - r: the decoded rune (valid when ok is true)
//   - rawSpan: the subslice of raw corresponding to this element
//   - isEscape: true if r was decoded from a \uXXXX escape / surrogate pair
//   - ok: true for valid runes (whether \u-escaped or literal); false for
//     non-\u escapes or malformed \u escapes
//
// If fn returns false, ScanEscaped terminates iteration early.
func ScanEscaped(raw []byte, fn func(r rune, rawSpan []byte, isEscape bool, ok bool) bool) {
	for i := 0; i < len(raw); {
		if raw[i] == '\\' && i+1 < len(raw) {
			if raw[i+1] != 'u' {
				if !fn(0, raw[i:i+2], false, false) {
					return
				}
				i += 2
				continue
			}
			r, escLen, ok := DecodeJSONUnicodeEscape(raw[i:])
			if !ok {
				if !fn(0, raw[i:i+2], false, false) {
					return
				}
				i += 2
				continue
			}
			if !fn(r, raw[i:i+escLen], true, true) {
				return
			}
			i += escLen
			continue
		}
		if raw[i] < 0x80 {
			if !fn(rune(raw[i]), raw[i:i+1], false, true) {
				return
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(raw[i:])
		if !fn(r, raw[i:i+size], false, true) {
			return
		}
		i += size
	}
}

// DecodeJSONUnicodeEscape decodes one \uXXXX escape at s[0:6] (s[0]=='\\',
// s[1]=='u'), combining it with an immediately-following \uXXXX low
// surrogate into one code point when s[0:6] decodes to a high surrogate --
// JSON (like JS) spells any code point above the BMP as a UTF-16 surrogate
// pair, which is exactly how the Tags block (U+E0000-E007F, A-tier)
// arrives on the wire. Returns escLen = 12 for a genuine pair, 6 otherwise
// (including an unpaired/stray surrogate, decoded as-is rather than
// rejected -- ClassifyRune's ranges don't overlap the surrogate range
// anyway, so a stray surrogate simply classifies as "").
//
// Exported so every \uXXXX-aware scanner (this package's own callers below,
// plus internal/probe's steganography probe) shares one implementation --
// a private reimplementation is how probe/guard.go's copy drifted from the
// escape-boundary handling its own callers already got right (see
// VerifyInvisibleRuneResponse's doc comment).
func DecodeJSONUnicodeEscape(s []byte) (r rune, escLen int, ok bool) {
	if len(s) < 6 {
		return 0, 0, false
	}
	v1, err := strconv.ParseUint(string(s[2:6]), 16, 32)
	if err != nil {
		return 0, 0, false
	}
	r1 := rune(v1)
	if utf16.IsSurrogate(r1) && len(s) >= 12 && s[6] == '\\' && s[7] == 'u' {
		if v2, err2 := strconv.ParseUint(string(s[8:12]), 16, 32); err2 == nil {
			if combined := utf16.DecodeRune(r1, rune(v2)); combined != utf8.RuneError {
				return combined, 12, true
			}
		}
	}
	return r1, 6, true
}
