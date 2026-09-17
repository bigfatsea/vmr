// Ver 2026-09-17, by Sonnet 5

// JSON string-VALUE traversal at arbitrary depth. jsonscan (this package's
// only whitelisted dependency, ADR-1) has structural byte-scanning
// primitives (SkipJSONWS/SkipJSONString/SkipJSONValue) but no walker that
// visits every string value regardless of nesting — TopLevelValues and
// WalkArrayElements are both single-level (see that package's doc
// comments). A credential can be anywhere ("messages[7].content[0].text",
// a tool_use.input field, a nested tool result), so Engine.Scan needs a
// walk that reuses jsonscan's byte-level primitives (quote/escape
// handling, delimiter skipping) but adds the "descend into every
// container" traversal jsonscan itself deliberately does not provide
// (K1 in the design spec — this really is new capability, not a rename of
// something already there).
package guard

import (
	"vmr/internal/jsonscan"
)

// maxWalkDepth guards against pathological/adversarial nesting (audit
// records are vmr's own historical output, not attacker-controlled input,
// but this package is also meant to be safe to point at arbitrary request
// bodies once M3/M4 exist) — refuse to recurse past a depth no legitimate
// LLM chat payload approaches, rather than risk unbounded stack growth.
const maxWalkDepth = 10000

// The traversal is implemented as methods on *Engine taking an explicit
// *Scratch (rather than a separate walker type holding a `visit
// func(...)` closure) specifically so Scan never constructs a closure: a
// closure capturing raw/dst afresh on every call is exactly the kind of
// allocation the no-hit-path zero-allocation goal (§4.8) rules out, and
// Go's escape analysis heap-allocates a closure stored through an
// interface-shaped `visit func(...)` field, the exact shape this design
// avoids. Threading sc explicitly (instead of a mutable Engine field) is what
// makes the Engine itself safe to share across goroutines (M3.0) — see
// engine.go's Engine/Scratch doc comments.

// walkStrings visits every JSON string VALUE (never a key) found anywhere
// in raw, at any nesting depth, calling e.scanValue on each. Malformed
// JSON simply stops the walk at the point it can no longer make progress —
// Scan's caller already knows raw came from vmr's own audit log or a real
// request body, so "give up cleanly on garbage" is the right failure mode,
// not a panic or an error nobody would act on differently.
func (e *Engine) walkStrings(raw []byte, sc *Scratch) {
	i := jsonscan.SkipJSONWS(raw, 0)
	e.walkValue(raw, i, 0, sc)
}

// walkValue descends into i's value if it is an object/array, or scans it
// directly if it is a string; other JSON types carry no credential and are
// skipped whole via jsonscan.SkipJSONValue. Returns the index just past
// the value, or -1 if the buffer ran out or was malformed at this point.
func (e *Engine) walkValue(raw []byte, i, depth int, sc *Scratch) int {
	if depth > maxWalkDepth || i < 0 || i >= len(raw) {
		return -1
	}
	switch raw[i] {
	case '"':
		end, ok := jsonscan.SkipJSONString(raw, i)
		if !ok {
			return -1
		}
		e.scanValue(raw, i+1, end-1, sc) // exclude the surrounding quotes
		return end
	case '{':
		return e.walkObject(raw, i, depth+1, sc)
	case '[':
		return e.walkArray(raw, i, depth+1, sc)
	default:
		end, ok := jsonscan.SkipJSONValue(raw, i)
		if !ok {
			return -1
		}
		return end
	}
}

// walkObject consumes raw[i:] as a JSON object (i points at '{'), visiting
// every value's strings while skipping every key.
func (e *Engine) walkObject(raw []byte, i, depth int, sc *Scratch) int {
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
		i, ok = jsonscan.SkipJSONString(raw, i)
		if !ok {
			return -1
		}
		i = jsonscan.SkipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return -1
		}
		i = jsonscan.SkipJSONWS(raw, i+1)
		i = e.walkValue(raw, i, depth, sc)
		if i < 0 {
			return -1
		}
	}
}

// walkArray consumes raw[i:] as a JSON array (i points at '['), visiting
// every element's strings recursively.
func (e *Engine) walkArray(raw []byte, i, depth int, sc *Scratch) int {
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
			i = e.walkValue(raw, i, depth, sc)
			if i < 0 {
				return -1
			}
		}
	}
}
