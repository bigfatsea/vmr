// Ver 2026-09-23 08:10, by Claude Opus 5.5

package guard

import (
	"fmt"
	"math"
	"strings"
)

// binaryBlobMinLen and binaryBlobMinB64Frac implement binary-blob
// skipping: a JSON
// string value shaped like a base64-encoded binary blob (an inline image,
// typically) is skipped whole rather than scanned rule-by-rule. Scanning
// it both wastes time on multi-MB payloads and risks an accidental
// credential-shaped match inside random-looking image bytes corrupting
// that image if a future mode ever rewrites in place (a false-positive
// rate of roughly 8% per 4MB image is what this guards against).
const (
	binaryBlobMinLen     = 8 << 10 // 8 KiB
	binaryBlobMinB64Frac = 0.98
)

// Engine holds a compiled rule set and its Aho-Corasick literal prefilter.
// Immutable after NewEngine returns — every field is read-only
// for the Engine's lifetime — so one Engine can be shared across
// goroutines (a package-level singleton reused
// by every request goroutine).
// All per-scan mutable state (the in-progress Finding buffer, the
// prefilter's hit set) lives in Scratch instead, one per calling
// goroutine.
type Engine struct {
	rules []Rule
	ver   int
	ac    *acAutomaton
}

// Scratch is one scan's mutable workspace: the in-progress Finding buffer,
// and the Aho-Corasick hit set scanValue's prefilter step reuses across
// rules. Not safe for concurrent use — each goroutine that calls
// Engine.Scan/ScanText needs its own Scratch (NewScratch is cheap: a few
// small slice allocations, done once per goroutine, not per call).
type Scratch struct {
	dst    []Finding
	acHits []bool
}

// NewScratch returns a ready Scratch. acHits is sized against nRules so
// the first Scan call never needs to grow it — nRules is normally
// len(engine.Rules()) for whichever Engine this Scratch will be used with.
func NewScratch(nRules int) *Scratch {
	return &Scratch{acHits: make([]bool, nRules)}
}

// NewEngine validates rules and returns a ready Engine. The one check that
// matters most: every Tier1 rule's compiled pattern must
// carry the exact leftBoundary anchor — the missing-anchor mistake that
// produced ~8,000 false hits in this repo's own logs. ver labels every
// Finding's provenance (RulesVersion
// for the built-in set; callers of a custom rule set choose their own).
func NewEngine(rules []Rule, ver int) (*Engine, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("guard: NewEngine: at least one rule required")
	}
	seen := make(map[string]bool, len(rules))
	lits := make([][]byte, len(rules))
	for i, r := range rules {
		if r.Name == "" {
			return nil, fmt.Errorf("guard: NewEngine: rule with empty name")
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("guard: NewEngine: duplicate rule name %q", r.Name)
		}
		seen[r.Name] = true
		if len(r.Literal) == 0 {
			return nil, fmt.Errorf("guard: NewEngine: rule %q has no literal prefix", r.Name)
		}
		if r.Re == nil {
			return nil, fmt.Errorf("guard: NewEngine: rule %q has no compiled pattern", r.Name)
		}
		if r.Tier == Tier1 && !strings.HasPrefix(r.Re.String(), leftBoundary) {
			return nil, fmt.Errorf("guard: NewEngine: Tier1 rule %q is missing the left-boundary anchor "+
				"(construct rules via NewRule, never a hand-built regexp.Regexp)", r.Name)
		}
		lits[i] = r.Literal
	}
	return &Engine{rules: rules, ver: ver, ac: newACAutomaton(lits)}, nil
}

// Version reports the rule-set version this Engine was constructed with
// (Record.Guard.Ver's source of truth).
func (e *Engine) Version() int { return e.ver }

// Rules returns the engine's rule set (read-only use: calibration tooling
// wants to report Literal/Tier/MinEntropy per rule without duplicating
// DefaultRules' table).
func (e *Engine) Rules() []Rule { return e.rules }

// Scan walks every JSON string value in raw (one JSON document) and
// returns every rule hit, using sc as scratch workspace (its dst buffer's
// backing array is reused across calls with the same Scratch, so a hot
// caller allocates no Finding slice per scan — the returned slice is only valid until the next Scan/ScanText call on the
// same Scratch).
//
// Not safe for two goroutines to call concurrently with the same Scratch;
// the Engine itself (e) is immutable and safe to share — see the Engine
// and Scratch doc comments.
func (e *Engine) Scan(raw []byte, sc *Scratch) []Finding {
	sc.dst = sc.dst[:0]
	e.walkStrings(raw, sc)
	return sc.dst
}

// ScanText runs the same rule set against s as a single opaque text blob —
// no JSON structure assumed.
// For text the caller has already decoded out of a wire format the JSON
// walk in Scan doesn't apply to: an SSE-reassembled assistant message
// (chatmsg.StreamSummary.Content/Reasoning), a tool call's arguments once
// unescaped, or any other plain string a caller wants scanned for the same
// credential-shaped patterns Scan looks for inside JSON documents (the
// offline inbound-forensics path runs this over chatmsg's already-decoded
// output — guard itself never parses SSE or JSON message shapes, see the
// package doc comment).
func (e *Engine) ScanText(s []byte, sc *Scratch) []Finding {
	sc.dst = sc.dst[:0]
	e.scanValue(s, 0, len(s), sc)
	return sc.dst
}

// scanValue runs every rule against one string value's bytes
// (raw[start:end]), skipping binary-blob-shaped values outright, and
// appends any hit to sc.dst. The Aho-Corasick prefilter
// replaces the old per-rule bytes.Contains loop with one O(len(value))
// pass that marks every rule whose literal occurs at least once; only
// those rules then pay for FindAllSubmatchIndex + entropy — on the
// overwhelmingly common no-literal-anywhere path, no rule's regexp ever
// runs at all.
func (e *Engine) scanValue(raw []byte, start, end int, sc *Scratch) {
	value := raw[start:end]
	if looksLikeBinaryBlob(value) {
		return
	}
	for i := range sc.acHits {
		sc.acHits[i] = false
	}
	e.ac.match(value, sc.acHits)
	for i := range e.rules {
		if !sc.acHits[i] {
			continue
		}
		r := &e.rules[i]
		for _, m := range r.Re.FindAllSubmatchIndex(value, -1) {
			// FindAllSubmatchIndex indexes as [wholeStart,wholeEnd,
			// g1Start,g1End]; the boundary alternative is non-capturing
			// (leftBoundary's "(?:...)"), so group 1 (m[2],m[3]) is the
			// credential body — never m[0]/m[1], which would include the
			// boundary byte itself when the match isn't at position 0.
			if len(m) < 4 || m[2] < 0 {
				continue
			}
			bodyStart, bodyEnd := m[2], m[3]
			body := value[bodyStart:bodyEnd]
			if r.MinEntropy > 0 {
				tail := body
				if len(r.Literal) < len(body) {
					tail = body[len(r.Literal):]
				}
				if shannonEntropy(tail) < r.MinEntropy {
					continue
				}
			}
			sc.dst = append(sc.dst, Finding{
				Rule:  r.Name,
				Tier:  r.Tier,
				Start: start + bodyStart,
				End:   start + bodyEnd,
			})
		}
	}
}

// looksLikeBinaryBlob implements binary-blob skipping exactly: length over
// binaryBlobMinLen, at least binaryBlobMinB64Frac of characters in the
// base64 alphabet, and no whitespace/newline anywhere (source text —
// JSON, code, prose — always has both punctuation outside the base64
// alphabet and, past 8 KiB, at least one newline; a base64 blob has
// neither).
func looksLikeBinaryBlob(value []byte) bool {
	if len(value) < binaryBlobMinLen {
		return false
	}
	var b64Count int
	for _, c := range value {
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			return false
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '=':
			b64Count++
		}
	}
	return float64(b64Count)/float64(len(value)) >= binaryBlobMinB64Frac
}

// shannonEntropy returns b's Shannon entropy in bits/char. Used only on an
// already-matched (short, bounded-length) credential body — never on the
// full scanned buffer — so the O(256) frequency table is negligible cost.
func shannonEntropy(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	var freq [256]int
	for _, c := range b {
		freq[c]++
	}
	n := float64(len(b))
	var h float64
	for _, f := range freq {
		if f == 0 {
			continue
		}
		p := float64(f) / n
		h -= p * math.Log2(p)
	}
	return h
}
