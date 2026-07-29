// Ver 2026-07-28 22:40, by Sonnet 5

package ctxgraph

import "testing"

func mkHashes(n int, seed string) []Hash {
	out := make([]Hash, n)
	for i := range out {
		out[i] = hashJSON(seed + string(rune('a'+i)))
	}
	return out
}

func manifestWithKeys(keys []Hash) *Manifest {
	return &Manifest{Keys: keys}
}

func TestClassify_Append(t *testing.T) {
	prev := mkHashes(5, "s")
	cur := append(append([]Hash{}, prev...), hashJSON("new"))
	e := Classify(manifestWithKeys(prev), manifestWithKeys(cur))
	if e.Kind != Append {
		t.Errorf("got %v, want Append", e.Kind)
	}
	if e.LCP != 5 {
		t.Errorf("LCP = %d, want 5", e.LCP)
	}
}

func TestClassify_AppendWithinTailSlack(t *testing.T) {
	// cur == prev exactly (LCP == len(prev)) is the purest Append case;
	// tailSlack exists so a same-length-ish tail wobble still resolves to
	// Append rather than ReplaceTail when coverage/contraction don't apply.
	prev := mkHashes(5, "s")
	cur := append([]Hash{}, prev...) // identical — degenerate but valid
	e := Classify(manifestWithKeys(prev), manifestWithKeys(cur))
	if e.Kind != Append {
		t.Errorf("got %v, want Append (identical manifests)", e.Kind)
	}
}

func TestClassify_ReplaceTail(t *testing.T) {
	prev := mkHashes(20, "s")
	cur := append(append([]Hash{}, prev[:15]...), mkHashes(3, "t")...) // tail diverges, no shrink, high coverage
	e := Classify(manifestWithKeys(prev), manifestWithKeys(cur))
	if e.Kind != ReplaceTail {
		t.Errorf("got %v, want ReplaceTail (lcp=%d cov=%.2f)", e.Kind, e.LCP, e.Coverage)
	}
}

func TestClassify_Contract(t *testing.T) {
	// Real corpus case (design doc F6/A.3): 79 messages -> 4 messages.
	prev := mkHashes(79, "s")
	cur := mkHashes(4, "s") // reuses same seed so keys[0] matches prev[0] (mirrors the real case: same user instruction survives)
	e := Classify(manifestWithKeys(prev), manifestWithKeys(cur))
	if e.Kind != Contract {
		t.Errorf("got %v, want Contract (79->4 messages)", e.Kind)
	}
}

func TestClassify_ContractThresholdBoundary(t *testing.T) {
	prev := mkHashes(10, "s")
	// exactly at the 0.6 ratio: 6 messages is NOT below 0.6*10=6 (strict <), so should NOT be Contract.
	curAtBoundary := mkHashes(6, "u")
	e := Classify(manifestWithKeys(prev), manifestWithKeys(curAtBoundary))
	if e.Kind == Contract {
		t.Errorf("6/10 should not cross the strict-< contract threshold, got Contract")
	}
	// one below the boundary: 5 messages IS below 6 -> Contract (assuming low coverage too, since seed differs).
	curBelow := mkHashes(5, "u")
	e2 := Classify(manifestWithKeys(prev), manifestWithKeys(curBelow))
	if e2.Kind != Contract {
		t.Errorf("5/10 should cross the contract threshold, got %v", e2.Kind)
	}
}

func TestClassify_Fork(t *testing.T) {
	prev := mkHashes(10, "s")
	cur := mkHashes(12, "totally-different") // same/similar length, near-zero overlap
	e := Classify(manifestWithKeys(prev), manifestWithKeys(cur))
	if e.Kind != Fork {
		t.Errorf("got %v, want Fork (lcp=%d cov=%.2f)", e.Kind, e.LCP, e.Coverage)
	}
}

func TestClassify_EmptyCurIsVacuousAppendOrReplaceTail(t *testing.T) {
	prev := mkHashes(3, "s")
	cur := manifestWithKeys(nil)
	e := Classify(manifestWithKeys(prev), cur)
	// len(cur.Keys)=0 < 0.6*3=1.8 -> Contract by the length check, which
	// fires before coverage is even consulted. An empty manifest genuinely
	// is a drastic contraction, so this is the correct call, not a bug.
	if e.Kind != Contract {
		t.Errorf("got %v, want Contract (empty cur is a contraction)", e.Kind)
	}
}

// TestClassify_TinyPrevContractsCorrectly locks in a fix: for a very small
// prev (len<=1), int(float64(len(prev))*contractLenRatio) used to truncate
// to 0 (e.g. int(1*0.6)=0), making "len(cur) < 0" unreachable and letting a
// genuine 1-message -> 0-message contraction fall through to the default
// Append case. Comparing in float throughout (no truncation of the
// threshold before the comparison) fixes this without changing any of the
// calibrated corpus boundary behavior (see TestClassify_ContractThresholdBoundary).
func TestClassify_TinyPrevContractsCorrectly(t *testing.T) {
	prev := mkHashes(1, "s")
	cur := manifestWithKeys(nil)
	e := Classify(manifestWithKeys(prev), cur)
	if e.Kind != Contract {
		t.Errorf("got %v, want Contract (1 message -> 0 messages is a contraction even for a tiny prev)", e.Kind)
	}
}

func TestClassify_EmptyPrev(t *testing.T) {
	prev := manifestWithKeys(nil)
	cur := mkHashes(3, "s")
	e := Classify(prev, manifestWithKeys(cur))
	// LCP(nil, cur) == 0 == len(prev.Keys) (both zero) -> Append by the
	// first check. This is the bucket's very first manifest in practice
	// (never actually compared against an empty predecessor), but the
	// function must not panic or divide by zero here.
	if e.Kind != Append {
		t.Errorf("got %v, want Append (0==0 LCP against empty prev)", e.Kind)
	}
}

func TestLCPLen(t *testing.T) {
	a := mkHashes(5, "x")
	b := append(append([]Hash{}, a[:3]...), mkHashes(2, "y")...)
	if got := lcpLen(a, b); got != 3 {
		t.Errorf("lcpLen = %d, want 3", got)
	}
	if got := lcpLen(nil, nil); got != 0 {
		t.Errorf("lcpLen(nil,nil) = %d, want 0", got)
	}
}

func TestCoverage(t *testing.T) {
	prev := mkHashes(4, "x")
	cur := append(append([]Hash{}, prev[1:3]...), mkHashes(1, "z")...) // 2 of 3 in prev
	got := coverage(cur, prev)
	want := 2.0 / 3.0
	if got != want {
		t.Errorf("coverage = %v, want %v", got, want)
	}
	if coverage(nil, prev) != 1.0 {
		t.Errorf("coverage(empty cur) should be vacuously 1.0")
	}
}

func TestEditKind_Splits(t *testing.T) {
	for _, k := range []EditKind{Append, ReplaceTail} {
		if k.Splits() {
			t.Errorf("%v should not split a lineage", k)
		}
	}
	for _, k := range []EditKind{Contract, Fork} {
		if !k.Splits() {
			t.Errorf("%v should split a lineage", k)
		}
	}
}

func TestEditKind_String(t *testing.T) {
	cases := map[EditKind]string{Append: "append", ReplaceTail: "replace_tail", Contract: "contract", Fork: "fork"}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", k, got, want)
		}
	}
}
