// Ver 2026-09-13, by Sonnet 5

package guard

import (
	"crypto/sha256"
	"encoding/hex"
)

// FingerprintLen is the byte length of the truncated Fingerprint returns
// (before hex encoding: the audit field is twice this many hex chars).
const FingerprintLen = 16

// Fingerprint computes the audit-safe identifier for one matched
// credential value (ADR-12 / GuardRecord.Hit.FP): a truncated SHA-256
// binding the rule name into the digest input so the same raw bytes
// matched by two different rules never collide to the same fingerprint.
//
// Deterministic, no salt — a first-principles re-review (KNOWN_ISSUES
// K-G19, superseding the salt-keyed HMAC ADR-12 originally specified)
// found the HMAC's one claimed protection (an operator-held salt makes a
// leaked report's fingerprints "correlatable but not independently
// confirmable") was never real: every `vmr analyze` output (macro/guard.json,
// Markdown/HTML) renders only integer counts (UniqueFP/UniqueCredentials),
// never the FP string itself — grep the whole analytics half and the only
// consumer is guardcol.go's own map-key dedup. The audit file itself
// already stores request bodies in full plaintext (K-G3: local
// single-user, 0600), so a salt added nothing at that tier either.
// Determinism also fixes two real bugs a salt caused: the offline
// fallback scan's per-run random salt made the same credential's FP drift
// across separate `vmr analyze` runs (and across a Facts-cache hit vs. a
// fresh scan within one run), and — more subtly — the online path's
// persistent salt and the offline path's random salt never agreed in the
// first place, so a credential appearing in both an online-stamped record
// and an offline-fallback-scanned record within the SAME run was already
// miscounted as two distinct credentials. A future consumer that
// genuinely needs an unguessable-without-a-secret identifier can
// reintroduce HMAC then, with an actual use case to design the salt
// lifecycle around instead of a speculative one.
func Fingerprint(ruleName string, secret []byte) string {
	h := sha256.New()
	h.Write([]byte(ruleName))
	h.Write([]byte{0})
	h.Write(secret)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:FingerprintLen])
}
