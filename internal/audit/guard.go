// Ver 2026-09-23 03:30, by Claude Opus 5.5

// Agent Guard's audit contract. Every field is omitempty and Guard itself is a nilable
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
	// rule/fingerprint universe — see KNOWN_ISSUES.
	Ver int `json:"ver"`
	// OutMode is the outbound mode active when this record was produced
	// (guard.outbound.mode: off | audit_only | block).
	OutMode string `json:"out_mode,omitempty"`

	Hits []Hit `json:"hits,omitempty"`

	// SanitizedRunes counts invisible/steganographic Unicode code points
	// removed from the response by category ("tags"/"bidi"/"zwsp"/...) —
	// the one online inbound intervention left in place.
	SanitizedRunes map[string]int `json:"sanitized_runes,omitempty"`
}

// Hit is one rule match within a request.
type Hit struct {
	Rule string `json:"rule"`
	Tier int    `json:"tier"`
	// Count is how many times Rule matched within this single request —
	// the raw data behind the "context amplification factor" metric
	// (the Agent Guard spec's context-amplification definition): a
	// long session can resend the same credential dozens of times per
	// request as history accumulates, which is evidence of amplification,
	// not of "N distinct leaks."
	Count int `json:"count"`
	// FP is this credential's deterministic fingerprint:
	// hex(SHA256(rule||0x00||secret))[:16]. Unsalted — see KNOWN_ISSUES:
	// the HMAC's one claimed protection (resisting offline
	// dictionary confirmation of a leaked report) was never real, since
	// no report or macro/guard.json output ever renders FP as a string;
	// the audit log itself already stores full plaintext, so a
	// salt added nothing at that tier either. See internal/guard.Fingerprint.
	FP string `json:"fp,omitempty"`
}
