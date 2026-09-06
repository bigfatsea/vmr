// Ver 2026-09-15, by pi

// Package journey owns journey and task narrative modeling, metrics, and journey-level fingerprints (D8 / §7.2).
package journey

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Digest computes an ordered, unambiguous, non-canceling SHA-256 digest
// over length-prefixed components (D8 / architecture redesign §7.2).
//
// Properties:
// 1. Order-sensitive: Digest(a, b) != Digest(b, a) (for a != b).
// 2. Unambiguous concatenation: Digest("ab", "c") != Digest("a", "bc").
// 3. Repetition non-canceling: Digest(a, a) != Digest() and Digest(a, a) != Digest(a).
func Digest(components ...[]byte) [32]byte {
	h := sha256.New()
	var buf [binary.MaxVarintLen64]byte
	for _, c := range components {
		n := binary.PutUvarint(buf[:], uint64(len(c)))
		h.Write(buf[:n])
		h.Write(c)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ComputeJourneyDigest computes the single-journey verification fingerprint (§7.2):
// Digest(步骤 manifest 哈希按序展开…, 定价指纹, 格式版本).
// The step manifest hashes are raw 16-byte MD5 hashes from ctxgraph, feeding into
// the ordered SHA-256 digest chain without altering the underlying MD5 addressing foundation (D8).
func ComputeJourneyDigest(manifestHashes [][16]byte, pricingFP []byte, formatVersion int) [32]byte {
	var components [][]byte
	components = append(components, encodeInt64(int64(len(manifestHashes))))
	for _, mh := range manifestHashes {
		components = append(components, mh[:])
	}
	components = append(components, pricingFP)
	components = append(components, encodeInt64(int64(formatVersion)))
	return Digest(components...)
}

// JourneyDigestHex computes ComputeJourneyDigest and returns the lowercase hex string.
func JourneyDigestHex(manifestHashes [][16]byte, pricingFP []byte, formatVersion int) string {
	d := ComputeJourneyDigest(manifestHashes, pricingFP, formatVersion)
	return hex.EncodeToString(d[:])
}

func encodeInt64(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}
