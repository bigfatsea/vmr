// Ver 2026-09-13, by Sonnet 5

// Agent Guard's audit contract (the Agent Guard spec
// §4.6, ADR-12). Every field is omitempty and Guard itself is a nilable
// pointer — a historical record with no Guard field at all decodes to
// Record.Guard == nil. Block/BlockInfo (the online Tool Call gate's
// circuit-break stamp) and the former mode: replace fields were removed
// before any tagged release, so no real audit record ever carried them.
package audit

// GuardRecord is one request's Agent Guard forensic metadata. It carries no
// raw secret material: a hit records only its rule name, tier, count,
// and a deterministic fingerprint (see Hit.FP's own doc comment).
type GuardRecord struct {
	// Ver is the rule-set version that produced this record's Hits
	// (guard.RulesVersion at scan time). A version bump changes the
	// rule/fingerprint universe — see docs/KNOWN_ISSUES.md K-G4.
	Ver int `json:"ver"`
	// OutMode is the outbound mode active when this record was produced
	// (guard.outbound.mode: off | audit_only | block).
	OutMode string `json:"out_mode,omitempty"`

	Hits []Hit `json:"hits,omitempty"`

	// SanitizedRunes counts invisible/steganographic Unicode code points
	// removed from the response by category ("tags"/"bidi"/"zwsp"/...) —
	// the one online inbound intervention ADR-15 left in place.
	SanitizedRunes map[string]int `json:"sanitized_runes,omitempty"`
}

// Hit is one rule match within a request.
type Hit struct {
	Rule string `json:"rule"`
	Tier int    `json:"tier"`
	// Count is how many times Rule matched within this single request —
	// the raw data behind the "context amplification factor" metric
	// (the Agent Guard spec §2.3 point 3): a
	// long session can resend the same credential dozens of times per
	// request as history accumulates, which is evidence of amplification,
	// not of "N distinct leaks."
	Count int `json:"count"`
	// FP is this credential's deterministic fingerprint:
	// hex(SHA256(rule||0x00||secret))[:16]. No salt — KNOWN_ISSUES K-G19
	// found the HMAC's one claimed protection (resisting offline
	// dictionary confirmation of a leaked report) was never real, since
	// no report or macro/guard.json output ever renders FP as a string;
	// the audit log itself already stores full plaintext (K-G3), so a
	// salt added nothing at that tier either. See internal/guard.Fingerprint.
	FP string `json:"fp,omitempty"`
}
