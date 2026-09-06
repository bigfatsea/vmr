// Ver 2026-09-15, by pi

package journey

import (
	"bytes"
	"testing"
)

// TestJourneyDigest_OrderSensitive verifies that reordering components produces different digests.
func TestJourneyDigest_OrderSensitive(t *testing.T) {
	a := []byte("alpha")
	b := []byte("beta")
	d1 := Digest(a, b)
	d2 := Digest(b, a)
	if bytes.Equal(d1[:], d2[:]) {
		t.Fatalf("Digest should be order-sensitive: Digest(a, b) == Digest(b, a)")
	}
}

// TestJourneyDigest_UnambiguousConcatenation verifies that length prefixing prevents boundary collision.
func TestJourneyDigest_UnambiguousConcatenation(t *testing.T) {
	d1 := Digest([]byte("ab"), []byte("c"))
	d2 := Digest([]byte("a"), []byte("bc"))
	if bytes.Equal(d1[:], d2[:]) {
		t.Fatalf("Digest should be unambiguous: Digest(\"ab\", \"c\") == Digest(\"a\", \"bc\")")
	}
}

// TestJourneyDigest_RepetitionNonCanceling verifies that repeating a component does not cancel.
func TestJourneyDigest_RepetitionNonCanceling(t *testing.T) {
	a := []byte("step_manifest_md5")
	empty := Digest()
	single := Digest(a)
	double := Digest(a, a)

	if bytes.Equal(double[:], empty[:]) {
		t.Fatalf("Digest(a, a) must not cancel out to Digest()")
	}
	if bytes.Equal(double[:], single[:]) {
		t.Fatalf("Digest(a, a) must not collide with Digest(a)")
	}
}

// TestComputeJourneyDigest_Properties verifies single-journey fingerprint computation (§7.2).
func TestComputeJourneyDigest_Properties(t *testing.T) {
	h1 := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	h2 := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	pricingFP := []byte("pricing_v1")
	formatVersion := 11

	dBase := ComputeJourneyDigest([][16]byte{h1, h2}, pricingFP, formatVersion)

	// Determinism
	dSame := ComputeJourneyDigest([][16]byte{h1, h2}, pricingFP, formatVersion)
	if !bytes.Equal(dBase[:], dSame[:]) {
		t.Fatalf("ComputeJourneyDigest must be deterministic")
	}

	// Step sequence order sensitivity
	dReordered := ComputeJourneyDigest([][16]byte{h2, h1}, pricingFP, formatVersion)
	if bytes.Equal(dBase[:], dReordered[:]) {
		t.Fatalf("Reordering steps must change journey digest")
	}

	// Pricing change
	dPricingDiff := ComputeJourneyDigest([][16]byte{h1, h2}, []byte("pricing_v2"), formatVersion)
	if bytes.Equal(dBase[:], dPricingDiff[:]) {
		t.Fatalf("Changing pricing fingerprint must change journey digest")
	}

	// Format version bump
	dFormatDiff := ComputeJourneyDigest([][16]byte{h1, h2}, pricingFP, 12)
	if bytes.Equal(dBase[:], dFormatDiff[:]) {
		t.Fatalf("Bumping format version must change journey digest")
	}
}
