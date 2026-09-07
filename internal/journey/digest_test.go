// Ver 2026-09-15, by pi

package journey

import (
	"bytes"
	"testing"
)

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
