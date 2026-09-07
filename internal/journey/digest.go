// Ver 2026-09-15, by pi

// Package journey owns journey and task narrative modeling, metrics, and journey-level fingerprints (D8 / §7.2).
package journey

import (
	"encoding/hex"

	"vmr/internal/digest"
)

// ComputeJourneyDigest computes the single-journey verification fingerprint (§7.2):
// Digest(步骤 manifest 哈希按序展开…, 定价指纹, 格式版本).
// The step manifest hashes are raw 16-byte MD5 hashes from ctxgraph, feeding into
// the ordered SHA-256 digest chain without altering the underlying MD5 addressing foundation (D8).
func ComputeJourneyDigest(manifestHashes [][16]byte, pricingFP []byte, formatVersion int) [32]byte {
	var components [][]byte
	components = append(components, digest.EncodeInt64(int64(len(manifestHashes))))
	for _, mh := range manifestHashes {
		components = append(components, mh[:])
	}
	components = append(components, pricingFP)
	components = append(components, digest.EncodeInt64(int64(formatVersion)))
	return digest.Digest(components...)
}

// JourneyDigestHex computes ComputeJourneyDigest and returns the lowercase hex string.
func JourneyDigestHex(manifestHashes [][16]byte, pricingFP []byte, formatVersion int) string {
	d := ComputeJourneyDigest(manifestHashes, pricingFP, formatVersion)
	return hex.EncodeToString(d[:])
}
