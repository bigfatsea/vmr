// Ver 2026-09-13, by Sonnet 5

package guard

import "testing"

func TestFingerprint_Deterministic(t *testing.T) {
	a := Fingerprint("aws-access-key", []byte("AKIAEXAMPLE12345678"))
	b := Fingerprint("aws-access-key", []byte("AKIAEXAMPLE12345678"))
	if a != b {
		t.Errorf("Fingerprint not deterministic: %q != %q", a, b)
	}
	if len(a) != FingerprintLen*2 { // hex-encoded
		t.Errorf("Fingerprint length = %d, want %d", len(a), FingerprintLen*2)
	}
}

// TestFingerprint_RuleNameBound confirms the same raw secret bytes matched
// by two different rules never collide to the same fingerprint (guard.go's
// doc comment promise) — relevant because a value can legitimately satisfy
// more than one rule's shape (e.g. a real openai-legacy-key also matches
// the looser generic-sk-prefix Tier2 pattern).
func TestFingerprint_RuleNameBound(t *testing.T) {
	secret := []byte("sk-" + repAlnum(48))
	a := Fingerprint("openai-legacy-key", secret)
	b := Fingerprint("generic-sk-prefix", secret)
	if a == b {
		t.Error("Fingerprint must differ across rule names for the same secret bytes")
	}
}

func TestFingerprint_SecretSensitive(t *testing.T) {
	a := Fingerprint("aws-access-key", []byte("AKIAEXAMPLE00000000"))
	b := Fingerprint("aws-access-key", []byte("AKIAEXAMPLE11111111"))
	if a == b {
		t.Error("Fingerprint must differ for different secrets")
	}
}
