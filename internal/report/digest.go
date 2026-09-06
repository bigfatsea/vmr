// Ver 2026-09-15, by pi

// Package report owns the macro analytics half and product-level caching (§7, D1/D8).
package report

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
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

// DigestHex computes Digest over components and returns the lowercase hex string.
func DigestHex(components ...[]byte) string {
	d := Digest(components...)
	return hex.EncodeToString(d[:])
}

// EncodeInt64 encodes an int64 scalar as 8 bytes in big-endian order (D8).
func EncodeInt64(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

// EncodeUint64 encodes a uint64 scalar as 8 bytes in big-endian order (D8).
func EncodeUint64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// EncodeFloat64 encodes a float64 scalar via math.Float64bits in big-endian order (D8).
func EncodeFloat64(v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return b
}

// EncodeBool encodes a boolean scalar as a single byte (0x01 for true, 0x00 for false) (D8).
func EncodeBool(v bool) []byte {
	if v {
		return []byte{1}
	}
	return []byte{0}
}

// EncodeString encodes a string into its raw byte representation (D8).
func EncodeString(s string) []byte {
	return []byte(s)
}
