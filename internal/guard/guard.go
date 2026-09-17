// Ver 2026-09-17, by Sonnet 5

// Package guard is Agent Guard's bidirectional credential/steganography
// detection core (docs/design/agent-guard-technical-spec-final-2.0.md). It
// is a dependency-white-listed leaf ({vmr/internal/jsonscan} only — see
// internal/archtest's allowedDepPackages, ADR-1) so both the offline
// calibration tool (tools/guard_corpus_scan) and the routing half's
// outbound/inbound hooks share one scanning engine without either side
// reimplementing JSON string-value traversal or credential pattern
// matching.
//
// Scope note: this package implements the full detection layer M2's
// offline bidirectional forensic audit needs — Rule/Engine/Scan/ScanText
// (outbound + inbound text scanning), ClassifyRunes (Unicode steganography
// classification), InspectToolCall (tool-call risk judgment, offline-only
// consumer per ADR-15), and Fingerprint — plus the online intervention
// layer actually wired: Outbound (audit_only/block aggregation, M3) and
// Inbound (Unicode-steganography sanitization only, M4 as narrowed by
// ADR-15). The spec's former `mode: replace` (outbound pseudonymization +
// response-side restore) was removed after a first-principles review:
// block is strictly safer against every threat replace covers (ADR-6
// already conceded replace does not defend against an active upstream),
// and every real-corpus Tier1 hit was an accidental credential paste where
// a visible request failure is the wanted behavior. The online Tool Call
// double gate, circuit-breaker frames, and non-streaming block path
// (originally M4.1–M4.5) were removed in the same spirit (ADR-15): a
// client's own approval gate and sandbox judge whether to run a command
// with strictly more context than this gateway ever has, and two
// independent reviews found most of their findings concentrated in that
// machinery. See the spec's Appendix B and docs/KNOWN_ISSUES.md for both.
//
// The Aho-Corasick literal prefilter (ADR-13's Level 1) shipped with M3.0
// (prefilter.go), shared by the outbound scan and (offline) the inbound
// tool-call inspection library; the immutable-Engine / caller-held-Scratch
// split (engine.go) is what makes one shared Engine safe across
// per-request goroutines.
package guard

import (
	"fmt"
	"regexp"
	"strings"
)

// Tier is a rule's admission class (ADR-5). Tier1 rules are anchored tightly
// enough that a hit is trustworthy evidence of a real credential; Tier2
// rules are audit-only forever (K-G5) — never a basis for any online
// block decision.
type Tier uint8

const (
	Tier1 Tier = 1 + iota
	Tier2
)

func (t Tier) String() string {
	switch t {
	case Tier1:
		return "tier1"
	case Tier2:
		return "tier2"
	default:
		return "unknown"
	}
}

// leftBoundary is the left-edge anchor every rule's compiled pattern
// carries (ADR-5's fifth anchor, added in this spec after real-corpus
// analysis showed its absence causes ~8,000 false hits in this repo's own
// logs — see docs/design/agent-guard-technical-spec-final-2.0.md §2.3). Go's
// RE2 engine has no lookbehind, so the boundary alternative is written as
// a non-capturing group ((?:...)) and matched inline; the credential body
// that follows is the pattern's only capturing group (group 1) — callers
// read that span, never group 0 (which would include the boundary byte
// itself when the match isn't at the very start of the buffer). A
// position at the very start of the scanned string value satisfies "^"
// and needs no preceding character at all.
const leftBoundary = `(?:^|[^A-Za-z0-9_+/=-])`

// Rule is one credential-recognition rule. Re is always compiled by
// NewRule (or validated by NewEngine to have leftBoundary's exact prefix)
// so a rule can never silently ship without the left-boundary anchor.
type Rule struct {
	Name string
	Tier Tier
	// Literal is the mandatory literal prefix of the credential body,
	// used for the cheap pre-check before running Re. Tier1 rules are
	// expected (not compiler-enforced) to keep this at least 4 bytes per
	// ADR-5, with sk-/hf_/eyJ (openai-legacy-key/huggingface-token/jwt) as
	// the three documented, deliberate exceptions — each compensates with a
	// fixed exact body length (or, for jwt, the three-dot-segment
	// structure) and, for openai-legacy-key, the highest MinEntropy of any
	// rule in DefaultRules (3.8), buying the confidence a longer literal
	// would otherwise supply.
	Literal []byte
	// Re is the fully anchored pattern: leftBoundary + "(" + body + ")".
	// Group 1 is the credential body (Literal at its start) — the only
	// capturing group, since the boundary alternation is non-capturing.
	Re *regexp.Regexp
	// MinEntropy is the Shannon-entropy floor (bits/char) a match's body
	// (the part after Literal) must clear. 0 disables the entropy gate —
	// used only for private-key-block, whose PEM armor is structural text,
	// not a rendered-random secret.
	MinEntropy float64
}

// NewRule compiles body into an anchored Rule. body is the full credential
// pattern INCLUDING its literal prefix (e.g. "sk-ant-api03-[A-Za-z0-9_-]{93}");
// NewRule wraps it with leftBoundary so every rule constructed this way
// carries the anchor by construction rather than by author discipline.
func NewRule(name string, tier Tier, literal, body string, minEntropy float64) (Rule, error) {
	if name == "" {
		return Rule{}, fmt.Errorf("guard: rule needs a non-empty name")
	}
	if literal == "" {
		return Rule{}, fmt.Errorf("guard: rule %q needs a non-empty literal prefix", name)
	}
	if !strings.HasPrefix(body, literal) {
		return Rule{}, fmt.Errorf("guard: rule %q: body %q does not start with literal prefix %q", name, body, literal)
	}
	re, err := regexp.Compile(leftBoundary + "(" + body + ")")
	if err != nil {
		return Rule{}, fmt.Errorf("guard: rule %q: compile pattern: %w", name, err)
	}
	return Rule{Name: name, Tier: tier, Literal: []byte(literal), MinEntropy: minEntropy, Re: re}, nil
}

// Finding is one credential-shaped match. Start/End are byte offsets into
// the raw buffer Scan was given, spanning the credential body only (never
// the boundary character).
type Finding struct {
	Rule  string
	Tier  Tier
	Start int
	End   int
}

// Body returns the matched credential bytes within raw.
func (f Finding) Body(raw []byte) []byte { return raw[f.Start:f.End] }
