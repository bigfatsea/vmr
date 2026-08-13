// Ver 2026-08-13, by Opus 5
package report

import "testing"

func TestPctStr64_ZeroOrNegativeDenominator(t *testing.T) {
	if got := pctStr64(5, 0); got != "-" {
		t.Errorf("pctStr64(5, 0) = %q, want \"-\"", got)
	}
	if got := pctStr64(5, -1); got != "-" {
		t.Errorf("pctStr64(5, -1) = %q, want \"-\"", got)
	}
}

func TestPctStr64_NormalValue(t *testing.T) {
	if got := pctStr64(1, 4); got != "25.0%" {
		t.Errorf("pctStr64(1, 4) = %q, want \"25.0%%\"", got)
	}
}

// TestPctStr64_BeyondInt32Range is the reason this int64 twin of pctStr2
// exists at all — a value like this would silently wrap if narrowed to int
// on a 32-bit build, which pctStr2's call sites never had to worry about
// but a raw token count crossing 2^31 legitimately can.
func TestPctStr64_BeyondInt32Range(t *testing.T) {
	const beyond32Bit = int64(1) << 33 // 8,589,934,592 — well past math.MaxInt32
	if got := pctStr64(beyond32Bit, beyond32Bit*2); got != "50.0%" {
		t.Errorf("pctStr64 with a beyond-int32 numerator/denominator = %q, want \"50.0%%\"", got)
	}
}
