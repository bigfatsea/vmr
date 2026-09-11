// Ver 2026-09-15, by pi

package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"vmr/internal/digest"

	"vmr/internal/pricing"
)

// TestComputePricingFingerprint verifies pricing fingerprint sensitivity and determinism.
func TestComputePricingFingerprint(t *testing.T) {
	gen := "2026-08-15"
	rates := map[string]float64{"CNY": 7.2, "EUR": 0.9}
	discount := 0.2
	policies := map[string]pricing.ProviderPolicy{
		"deepseek": {
			Aliases: map[string]string{"chat": "deepseek-v3"},
			Overrides: []pricing.OverrideRule{
				{Model: "deepseek-v3", Discount: &discount},
			},
		},
	}

	base := ComputePricingFingerprint(gen, rates, policies)

	// 1. Determinism across same inputs
	same := ComputePricingFingerprint(gen, rates, policies)
	if !bytes.Equal(base, same) {
		t.Fatalf("ComputePricingFingerprint should be deterministic")
	}

	// 2. Standard table date change
	genDiff := ComputePricingFingerprint("2026-09-01", rates, policies)
	if bytes.Equal(base, genDiff) {
		t.Fatalf("Changing StandardGeneratedAt must change pricing fingerprint")
	}

	// 3. Exchange rate change
	ratesDiff := ComputePricingFingerprint(gen, map[string]float64{"CNY": 7.3, "EUR": 0.9}, policies)
	if bytes.Equal(base, ratesDiff) {
		t.Fatalf("Changing exchange rate must change pricing fingerprint")
	}

	// 4. Policy discount change
	discountDiffVal := 0.25
	policiesDiff := map[string]pricing.ProviderPolicy{
		"deepseek": {
			Aliases: map[string]string{"chat": "deepseek-v3"},
			Overrides: []pricing.OverrideRule{
				{Model: "deepseek-v3", Discount: &discountDiffVal},
			},
		},
	}
	policiesDiffFP := ComputePricingFingerprint(gen, rates, policiesDiff)
	if bytes.Equal(base, policiesDiffFP) {
		t.Fatalf("Changing discount must change pricing fingerprint")
	}

	// 5. Policy alias change
	policiesAliasDiff := map[string]pricing.ProviderPolicy{
		"deepseek": {
			Aliases: map[string]string{"chat": "deepseek-r1"},
			Overrides: []pricing.OverrideRule{
				{Model: "deepseek-v3", Discount: &discount},
			},
		},
	}
	aliasDiffFP := ComputePricingFingerprint(gen, rates, policiesAliasDiff)
	if bytes.Equal(base, aliasDiffFP) {
		t.Fatalf("Changing alias must change pricing fingerprint")
	}
}

// TestComputeAnalysisParamsFingerprint verifies sampling parameter sensitivity and tag sorting.
func TestComputeAnalysisParamsFingerprint(t *testing.T) {
	baseParams := AnalysisParams{
		Lang:               "en",
		TaskProfile:        "openclaw_aware",
		IncludePartial:     false,
		IncludeSelfTraffic: false,
		SelfTrafficTags:    []string{"tagB", "tagA"},
		DisplayCCY:         "USD",
		RenderAll:          false,
		Details:            false,
		Mode:               "default",
	}

	base := ComputeAnalysisParamsFingerprint(baseParams)

	// Determinism with different tag order (must sort tags internally)
	shuffledTagsParams := baseParams
	shuffledTagsParams.SelfTrafficTags = []string{"tagA", "tagB"}
	shuffledFP := ComputeAnalysisParamsFingerprint(shuffledTagsParams)
	if !bytes.Equal(base, shuffledFP) {
		t.Fatalf("ComputeAnalysisParamsFingerprint must be invariant to SelfTrafficTags order")
	}

	// Lang change
	langDiff := baseParams
	langDiff.Lang = "zh"
	if bytes.Equal(base, ComputeAnalysisParamsFingerprint(langDiff)) {
		t.Fatalf("Changing Lang must change analysis params fingerprint")
	}

	// Profile change
	profDiff := baseParams
	profDiff.TaskProfile = "generic"
	if bytes.Equal(base, ComputeAnalysisParamsFingerprint(profDiff)) {
		t.Fatalf("Changing TaskProfile must change analysis params fingerprint")
	}

	// IncludePartial change
	partDiff := baseParams
	partDiff.IncludePartial = true
	if bytes.Equal(base, ComputeAnalysisParamsFingerprint(partDiff)) {
		t.Fatalf("Changing IncludePartial must change analysis params fingerprint")
	}

	// IncludeSelfTraffic change
	selfDiff := baseParams
	selfDiff.IncludeSelfTraffic = true
	if bytes.Equal(base, ComputeAnalysisParamsFingerprint(selfDiff)) {
		t.Fatalf("Changing IncludeSelfTraffic must change analysis params fingerprint")
	}

	// Currency change
	ccyDiff := baseParams
	ccyDiff.DisplayCCY = "CNY"
	if bytes.Equal(base, ComputeAnalysisParamsFingerprint(ccyDiff)) {
		t.Fatalf("Changing DisplayCCY must change analysis params fingerprint")
	}

	// Mode change
	modeDiff := baseParams
	modeDiff.Mode = "macro-only"
	if bytes.Equal(base, ComputeAnalysisParamsFingerprint(modeDiff)) {
		t.Fatalf("Changing Mode must change analysis params fingerprint")
	}
}

// TestComputeL2L3Digest_RendererVersionOrthogonality verifies row 5 of the invalidation matrix:
// RendererVersion bump changes L3 but leaves L2 unchanged (§7.4).
func TestComputeL2L3Digest_RendererVersionOrthogonality(t *testing.T) {
	in1 := digest.Digest([]byte("input1"))
	in2 := digest.Digest([]byte("input2"))
	inHashes := [][]byte{in1[:], in2[:]}
	pricingFP := digest.Digest([]byte("pricing"))
	paramsFP := digest.Digest([]byte("params"))
	vmFP := digest.Digest([]byte("vm_slices"))

	base := ComputeL2Digest(inHashes, pricingFP[:], ManifestFormat, paramsFP[:])
	again := ComputeL2Digest(inHashes, pricingFP[:], ManifestFormat, paramsFP[:])

	if !bytes.Equal(base[:], again[:]) {
		t.Fatalf("L2 digest should be deterministic")
	}

	// Row 5 of the invalidation matrix (§7.4) is enforced by the API shape:
	// rendererVersion is not an input to ComputeL2Digest, so a renderer bump
	// cannot reach L2 by construction — there is no argument to vary. What
	// the digest functions do let us pin is both halves of that row: L2 is
	// fully determined by its four data inputs (each of them moves it, so a
	// regression that drops one from the digest is caught), and L3 does move
	// with the renderer version.
	otherPricing := digest.Digest([]byte("other-pricing"))
	otherParams := digest.Digest([]byte("other-params"))
	l2DiffInputs := ComputeL2Digest([][]byte{in1[:]}, pricingFP[:], ManifestFormat, paramsFP[:])
	if bytes.Equal(base[:], l2DiffInputs[:]) {
		t.Fatalf("Changing input hashes must change L2 digest")
	}
	l2DiffPricing := ComputeL2Digest(inHashes, otherPricing[:], ManifestFormat, paramsFP[:])
	if bytes.Equal(base[:], l2DiffPricing[:]) {
		t.Fatalf("Changing pricing fingerprint must change L2 digest")
	}
	l2DiffFormat := ComputeL2Digest(inHashes, pricingFP[:], ManifestFormat+1, paramsFP[:])
	if bytes.Equal(base[:], l2DiffFormat[:]) {
		t.Fatalf("Changing format version must change L2 digest")
	}
	l2DiffParams := ComputeL2Digest(inHashes, pricingFP[:], ManifestFormat, otherParams[:])
	if bytes.Equal(base[:], l2DiffParams[:]) {
		t.Fatalf("Changing analysis params fingerprint must change L2 digest")
	}

	l3V1 := ComputeL3Digest(vmFP[:], 1, "en")
	l3V2 := ComputeL3Digest(vmFP[:], 2, "en")

	if bytes.Equal(l3V1[:], l3V2[:]) {
		t.Fatalf("Bumping RendererVersion must change L3 digest")
	}
}

// TestCacheRecord_SaveAndLoad verifies atomic persistence and 0600/0700 permissions.
func TestCacheRecord_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	rec := &CacheRecord{
		L2Digest:        "l2_hex_value",
		VMFingerprint:   "vm_hex_value",
		L3Digest:        "l3_hex_value",
		FormatVersion:   ManifestFormat,
		RendererVersion: RendererVersion,
	}

	if err := SaveCacheRecord(dir, rec); err != nil {
		t.Fatalf("SaveCacheRecord: %v", err)
	}

	// Verify directory permissions
	cacheDir := filepath.Join(dir, CacheDirName)
	dirInfo, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("Stat cache dir: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatalf("Expected %s to be directory", cacheDir)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("Expected 0700 permissions on cache dir, got %o", dirInfo.Mode().Perm())
	}

	// Verify file permissions (0600)
	filePath := CachePath(dir)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat cache file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("Expected 0600 permissions on cache file, got %o", fileInfo.Mode().Perm())
	}

	// Load and verify
	loaded, err := LoadCacheRecord(dir)
	if err != nil {
		t.Fatalf("LoadCacheRecord: %v", err)
	}
	if loaded.L2Digest != rec.L2Digest || loaded.L3Digest != rec.L3Digest ||
		loaded.FormatVersion != rec.FormatVersion || loaded.RendererVersion != rec.RendererVersion {
		t.Fatalf("Loaded cache record mismatch: %+v vs %+v", loaded, rec)
	}
}
