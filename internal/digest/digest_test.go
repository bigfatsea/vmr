// Ver 2026-09-15, by pi

package digest

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

// TestDigest_OrderSensitive verifies that reordering components produces different digests.
func TestDigest_OrderSensitive(t *testing.T) {
	a := []byte("alpha")
	b := []byte("beta")
	d1 := Digest(a, b)
	d2 := Digest(b, a)
	if bytes.Equal(d1[:], d2[:]) {
		t.Fatalf("Digest should be order-sensitive: Digest(a, b) == Digest(b, a)")
	}
}

// TestDigest_UnambiguousConcatenation verifies that length prefixing prevents boundary collision.
func TestDigest_UnambiguousConcatenation(t *testing.T) {
	d1 := Digest([]byte("ab"), []byte("c"))
	d2 := Digest([]byte("a"), []byte("bc"))
	if bytes.Equal(d1[:], d2[:]) {
		t.Fatalf("Digest should be unambiguous: Digest(\"ab\", \"c\") == Digest(\"a\", \"bc\")")
	}

	d3 := Digest([]byte("hello"), []byte("world"))
	d4 := Digest([]byte("hell"), []byte("oworld"))
	if bytes.Equal(d3[:], d4[:]) {
		t.Fatalf("Digest should be unambiguous: Digest(\"hello\", \"world\") == Digest(\"hell\", \"oworld\")")
	}
}

// TestDigest_RepetitionNonCanceling verifies that repeating a component does not cancel or collide.
func TestDigest_RepetitionNonCanceling(t *testing.T) {
	a := []byte("compaction_replay")
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

// TestDigest_EmptyVsSingleEmpty verifies that no components differs from one empty component.
func TestDigest_EmptyVsSingleEmpty(t *testing.T) {
	dNone := Digest()
	dEmpty := Digest([]byte{})
	dTwoEmpty := Digest([]byte{}, []byte{})

	if bytes.Equal(dNone[:], dEmpty[:]) {
		t.Fatalf("Digest() must not collide with Digest([]byte{})")
	}
	if bytes.Equal(dEmpty[:], dTwoEmpty[:]) {
		t.Fatalf("Digest([]byte{}) must not collide with Digest([]byte{}, []byte{})")
	}
}

// TestDigest_Determinism verifies identical inputs yield byte-identical results.
func TestDigest_Determinism(t *testing.T) {
	c1 := []byte("test_component_1")
	c2 := []byte("test_component_2")
	d1 := Digest(c1, c2)
	d2 := Digest(c1, c2)
	if !bytes.Equal(d1[:], d2[:]) {
		t.Fatalf("Digest must be deterministic: %x != %x", d1, d2)
	}

	hex1 := DigestHex(c1, c2)
	if hex1 != hex.EncodeToString(d1[:]) {
		t.Fatalf("DigestHex mismatch: got %s, want %x", hex1, d1)
	}
}

// TestDigest_WireFormat pins the construction itself (uvarint length prefix,
// then raw bytes, over sha256) against a hand-computed vector, independent of
// the implementation. If the prefix encoding or hash input framing ever
// changes, this trips even if the change is internally consistent.
func TestDigest_WireFormat(t *testing.T) {
	h := sha256.New()
	var lenBuf [binary.MaxVarintLen64]byte
	for _, c := range [][]byte{[]byte("component-one"), {0x01, 0x02}} {
		n := binary.PutUvarint(lenBuf[:], uint64(len(c)))
		h.Write(lenBuf[:n])
		h.Write(c)
	}
	want := [32]byte{}
	copy(want[:], h.Sum(nil))

	if got := Digest([]byte("component-one"), []byte{0x01, 0x02}); got != want {
		t.Fatalf("Digest wire format drifted: got %x, want %x", got, want)
	}
}

// TestDigest_ScalarEncoders verifies fixed-width big-endian scalar encodings (D8).
func TestDigest_ScalarEncoders(t *testing.T) {
	// Int64
	i1 := int64(-42)
	i2 := int64(42)
	b1 := EncodeInt64(i1)
	b2 := EncodeInt64(i2)
	if len(b1) != 8 || len(b2) != 8 {
		t.Fatalf("EncodeInt64 must produce 8 bytes")
	}
	if bytes.Equal(b1, b2) {
		t.Fatalf("Different int64 values must produce different bytes")
	}
	if got := int64(binary.BigEndian.Uint64(b1)); got != i1 {
		t.Fatalf("EncodeInt64 roundtrip mismatch: got %d, want %d", got, i1)
	}

	// Uint64
	u1 := uint64(1000)
	u2 := uint64(2000)
	bu1 := EncodeUint64(u1)
	bu2 := EncodeUint64(u2)
	if len(bu1) != 8 || len(bu2) != 8 {
		t.Fatalf("EncodeUint64 must produce 8 bytes")
	}
	if bytes.Equal(bu1, bu2) {
		t.Fatalf("Different uint64 values must produce different bytes")
	}
	if got := binary.BigEndian.Uint64(bu1); got != u1 {
		t.Fatalf("EncodeUint64 roundtrip mismatch: got %d, want %d", got, u1)
	}

	// Float64
	f1 := 3.141592653589793
	f2 := -0.0001
	bf1 := EncodeFloat64(f1)
	bf2 := EncodeFloat64(f2)
	if len(bf1) != 8 || len(bf2) != 8 {
		t.Fatalf("EncodeFloat64 must produce 8 bytes")
	}
	if bytes.Equal(bf1, bf2) {
		t.Fatalf("Different float64 values must produce different bytes")
	}
	if math.Float64bits(f1) != binary.BigEndian.Uint64(bf1) {
		t.Fatalf("EncodeFloat64 must use math.Float64bits")
	}

	// Bool
	if !bytes.Equal(EncodeBool(true), []byte{1}) || !bytes.Equal(EncodeBool(false), []byte{0}) {
		t.Fatalf("EncodeBool must emit 0x01/0x00")
	}
}
