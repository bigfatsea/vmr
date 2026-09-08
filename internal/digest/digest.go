// Ver 2026-09-07, by Claude (pi)

// Package digest owns the system's single cache-digest construction (D8):
// a length-prefixed, ordered sha256
// chain. Every cache-admission judgment in the analytics half is a call site
// of Digest — the properties below are what make the chain safe, and none of
// them may be weakened:
//
//   - Order-sensitive: Digest(a, b) != Digest(b, a) (for a != b). Journey
//     step sequences and slice lists are ordered; a reorder must re-digest.
//   - Unambiguous concatenation: Digest("ab", "c") != Digest("a", "bc").
//     Without the length prefix, `model ‖ endpoint` style joins could
//     collide silently across field boundaries.
//   - Repetition non-canceling: Digest(a, a) != Digest() and
//     Digest(a, a) != Digest(a). Compacted history replays put the same
//     manifest hash into one journey twice; any XOR/sum-fold would cancel
//     them.
//
// Components are raw bytes, not their hex strings; scalars are encoded at
// fixed width (big-endian int64/uint64, math.Float64bits for float64) —
// never via fmt.Sprintf, where a format-verb tweak would silently rotate
// every fingerprint. This is a cache-criterion hash only: ctxgraph's md5
// content-addressing base is a separate, deliberately untouched identity
// system.
package digest

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// Digest computes the ordered, length-prefixed sha256 chain over components.
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

// EncodeInt64 encodes an int64 scalar as 8 bytes, big-endian.
func EncodeInt64(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

// EncodeUint64 encodes a uint64 scalar as 8 bytes, big-endian.
func EncodeUint64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// EncodeFloat64 encodes a float64 scalar via math.Float64bits, big-endian.
func EncodeFloat64(v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return b
}

// EncodeBool encodes a boolean scalar as a single byte (0x01 true, 0x00 false).
func EncodeBool(v bool) []byte {
	if v {
		return []byte{1}
	}
	return []byte{0}
}

// EncodeString encodes a string as its raw bytes.
func EncodeString(s string) []byte {
	return []byte(s)
}
