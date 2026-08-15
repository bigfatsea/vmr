// Ver 2026-08-14, by Sonnet 5
package jsonscan

import "bytes"

// SkipJSONWS returns the index of the first non-whitespace byte at or after
// i. It is total: for ANY i outside [0, len(b)) the result is >= len(b), so
// the `i >= bound` check every caller already performs before dereferencing
// trips instead of the dereference panicking. That is the useful property to
// state, and it is why a negative i saturates forward to len(b) rather than
// clamping back to 0 — clamping would answer a question the caller did not
// ask (scan from the start of the buffer) and let a nonsense offset produce
// a plausible-looking result, which this package treats as worse than a
// refused one.
func SkipJSONWS(b []byte, i int) int {
	if i < 0 {
		return len(b)
	}
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// inRange reports whether [start,end) is a usable window into raw. The
// exported index-taking scanners below validate their window with it up
// front rather than trusting the caller: an end past len(raw) makes their
// `i >= end` loop guards stop guarding the buffer (i can reach len(raw)
// while still being < end, and the next raw[i] panics), and a negative
// start reaches the same place from the other side. Refusing an impossible
// window is also the honest answer — silently clamping end to len(raw)
// would report "scanned this array fine" for a buffer that was truncated.
func inRange(raw []byte, start, end int) bool {
	return start >= 0 && end <= len(raw) && start < end
}

// SkipJSONString advances past the string starting at b[i], returning the
// index just after the closing quote. A thin wrapper over
// IndexUnescapedQuote — the two only differ in what "start" means (b[i] is
// the opening quote here; IndexUnescapedQuote's b is everything after one).
//
// The entry guard is not redundant with the callers, which all reach here
// via a `case '"'` dispatch on an already-bounds-checked index. It is here
// because this is an exported function in a zero-internal-dependency leaf
// package: its callers are an open set, and the two ways it can be misused
// both fail badly. An out-of-range i panics on the b[i+1:] slice below
// (SkipJSONValue guards that for its own path, this one did not), and a b[i]
// that is not a quote returns a confidently wrong offset — the scan would
// find the NEXT quote in the buffer and report success. Failing the scan is
// the same choice SkipJSONValue's number branch already makes for malformed
// input: wrong answers are worse than refused ones.
func SkipJSONString(b []byte, i int) (int, bool) {
	if i < 0 || i >= len(b) || b[i] != '"' {
		return 0, false
	}
	j := IndexUnescapedQuote(b[i+1:])
	if j < 0 {
		return 0, false
	}
	return i + 1 + j + 1, true
}

// IndexUnescapedQuote returns the index of the first unescaped '"' in b, or
// -1 if there isn't one — b holds the bytes right after an opening quote,
// not including it. Escaped quotes are recognized by backslash parity (an
// even run of '\' before the '"' leaves it unescaped: each pair is one
// literal '\' in the string content; an odd run means the last '\' escapes
// the '"'), so the jump-to-next-quote stays at bytes.IndexByte (memchr)
// speed even through multi-MB content strings.
//
// Exported for internal/server/facts.go's estimateDocumentTokens, which
// needs the exact same "find where this JSON string value ends" scan to
// size a base64 document payload for token estimation.
func IndexUnescapedQuote(b []byte) int {
	i := 0
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return -1
		}
		k, n := i+j-1, 0
		for k >= 0 && b[k] == '\\' {
			n++
			k--
		}
		if n%2 == 0 {
			return i + j
		}
		i += j + 1
	}
}

// SkipJSONValue advances past one JSON value (string, object, array, number,
// or literal) starting at b[i]. For number/true/false/null it is a delimiter
// scanner, not a validator: it bounds the token at the next structural
// delimiter or at end of buffer, without checking the bytes in between spell
// a well-formed literal — see TopLevelValues' ok=false contract.
func SkipJSONValue(b []byte, i int) (int, bool) {
	if i < 0 || i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '"':
		return SkipJSONString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				var ok bool
				if i, ok = SkipJSONString(b, i); !ok {
					return 0, false
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, true
				}
			}
			i++
		}
		return 0, false
	default:
		// number / true / false / null: scan to the next structural
		// delimiter. start guards against zero-progress "success": if b[i]
		// is itself already a delimiter, there was no value token here at
		// all (e.g. a stray "," or unmatched "}" where a value was
		// expected) — returning (i, true) unchanged would let every caller
		// here, all of which loop as `i, ok = SkipJSONValue(...); continue`
		// on ok==true, spin forever re-scanning the same byte. Malformed
		// input must fail the scan (ok=false), not stall it.
		start := i
		for i < len(b) {
			switch b[i] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				if i == start {
					return 0, false
				}
				return i, true
			}
			i++
		}
		// EOF with the token still open: a bare literal filling the buffer
		// (SkipJSONValue([]byte("123"), 0)). Unreachable from in-repo callers
		// — all of them scan inside a container, where a delimiter always
		// arrives first — but this is exported from a leaf package with an
		// open caller set, and refusing it would contradict the contract
		// above. i > start is the same zero-progress rule as in the loop.
		if i > start {
			return i, true
		}
		return 0, false
	}
}
