package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"testing"

	"vmr/internal/journey"
	"vmr/internal/report"
)

// TestDigestConstructorsAgree pins the two halves' Digest implementations to
// byte-identical output. D8 says the whole system has one Digest construction;
// the two-halves contract (report must not import journey) forces one copy per
// side, so this differential test is what keeps them from drifting — the same
// pattern as the quota-parity tests: the formula is shared only as an
// assertion, never as an import. See KNOWN_ISSUES' package-boundary section.
func TestDigestConstructorsAgree(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	cases := [][]byte{
		nil,
		{},
		[]byte(""),
		[]byte("a"),
		[]byte("ab"),
		bytes.Repeat([]byte{0x00}, 64),
		bytes.Repeat([]byte{0xff}, 257),
	}
	for i := 0; i < 200; i++ {
		b := make([]byte, rng.Intn(1024))
		rng.Read(b)
		cases = append(cases, b)
	}

	// Same bytes partitioned one-to-five ways must digest identically on both
	// sides: the constructors are total over []byte components, so any drift
	// in the length-prefix or hash construction shows up as a mismatch here.
	var joined []byte
	for _, c := range cases {
		joined = append(joined, c...)
	}
	for n := 1; n <= 5; n++ {
		partition := splitFor(joined, n)
		gotReport := report.Digest(partition...)
		gotJourney := journey.Digest(partition...)
		if gotReport != gotJourney {
			t.Fatalf("partition n=%d: report.Digest != journey.Digest — the two implementations have drifted", n)
		}
	}
}

// TestDigestWireFormat pins the construction itself (uvarint length prefix,
// then raw bytes, over sha256) against a hand-computed vector, independent of
// either implementation. If either side changes its prefix encoding or hash
// input framing, this trips even if the two sides drift in lockstep.
func TestDigestWireFormat(t *testing.T) {
	h := sha256.New()
	var lenBuf [binary.MaxVarintLen64]byte
	for _, c := range [][]byte{[]byte("component-one"), {0x01, 0x02}} {
		n := binary.PutUvarint(lenBuf[:], uint64(len(c)))
		h.Write(lenBuf[:n])
		h.Write(c)
	}
	want := [32]byte{}
	copy(want[:], h.Sum(nil))

	if got := report.Digest([]byte("component-one"), []byte{0x01, 0x02}); got != want {
		t.Fatalf("report.Digest wire format drifted: got %x, want %x", got, want)
	}
	if got := journey.Digest([]byte("component-one"), []byte{0x01, 0x02}); got != want {
		t.Fatalf("journey.Digest wire format drifted: got %x, want %x", got, want)
	}
}

func splitFor(b []byte, n int) [][]byte {
	if len(b) < n {
		n = len(b)
	}
	parts := make([][]byte, 0, n)
	size := len(b) / n
	rem := len(b) % n
	off := 0
	for i := 0; i < n; i++ {
		sz := size
		if i < rem {
			sz++
		}
		parts = append(parts, b[off:off+sz])
		off += sz
	}
	return parts
}
