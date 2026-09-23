// Ver 2026-09-23 01:51, by Sonnet 5
package jsonscan

import (
	"bytes"
	"testing"
)

// TestWalkArrayElements_DoesNotReadPastArrEnd locks in a boundary fix: the
// entry check used to compare against len(raw) instead of arrEnd, so a
// caller passing a whitespace-only [arrStart,arrEnd) range with a real '['
// sitting later in the shared raw buffer (past arrEnd) would have the scan
// walk into bytes outside its intended window. No current production caller
// can trigger this (every arrStart they pass already sits on non-whitespace,
// via TopLevelValues' valStart), but the exported function's own contract —
// "the array whose value occupies raw[arrStart:arrEnd]" — must not silently
// escape that window for a future caller that doesn't share that invariant.
func TestWalkArrayElements_DoesNotReadPastArrEnd(t *testing.T) {
	t.Parallel()
	// raw[3:6) is "   " (all whitespace, no '['); a real '[' array sits at
	// raw[6:9) but that's outside [3,6).
	raw := []byte(`{}   [1]`)
	arrStart, arrEnd := 2, 5 // raw[2:5) == "}   " has no '[' inside the window
	_, ok := WalkArrayElements(raw, arrStart, arrEnd, func(int, int) bool { return false })
	if ok {
		t.Errorf("WalkArrayElements should decline (ok=false) when no '[' lies within [%d,%d), got ok=true", arrStart, arrEnd)
	}
}

// TestElementRole_LeadingWhitespaceTolerated matches ElementRole's sibling
// entry points (TopLevelValues, WalkArrayElements), which both skip leading
// whitespace before checking for their expected structural character.
// ElementRole previously required raw[elemStart] to be '{' with no
// whitespace tolerance — inconsistent with the rest of the package's public
// API, even though every current caller (WalkArrayElements' visit callback)
// already hands it a whitespace-trimmed elemStart.
func TestElementRole_LeadingWhitespaceTolerated(t *testing.T) {
	t.Parallel()
	raw := []byte(`   {"role":"user"}`)
	role, ok := ElementRole(raw, 0, len(raw))
	if !ok || role != "user" {
		t.Errorf("ElementRole with leading whitespace = (%q, %v), want (\"user\", true)", role, ok)
	}
}

// TestElementRole_EmptyRangeDeclines covers elemStart==elemEnd, which
// SkipJSONWS(raw, elemStart) can legitimately return unchanged (no
// whitespace to skip) — the i >= elemEnd guard must still catch it. Passing
// elemEnd beyond len(raw) is a separate, pre-existing (not a B1 change)
// out-of-contract-call class every function here trusts the caller not to
// do, same as WalkArrayElements' arrEnd — not this fix's scope.
func TestElementRole_EmptyRangeDeclines(t *testing.T) {
	t.Parallel()
	raw := []byte(`{}`)
	if _, ok := ElementRole(raw, 1, 1); ok {
		t.Error("ElementRole with elemStart == elemEnd should decline")
	}
}

// TestSkipJSONString_HostileIndex pins the entry guard. Every in-repo caller
// reaches SkipJSONString through a `case '"'` dispatch on a bounds-checked
// index, so none of these inputs occurs today — but the function is exported
// from a leaf package with an open caller set, and both misuse shapes fail
// badly without the guard: the first three panicked on the b[i+1:] slice,
// and the last returned (4, true) by locking onto the NEXT quote in the
// buffer, a confidently wrong offset rather than a refused scan.
func TestSkipJSONString_HostileIndex(t *testing.T) {
	cases := []struct {
		name string
		b    string
		i    int
	}{
		{"i at len", `"x"`, 3},
		{"i past len", `"x"`, 9},
		{"i negative", `"x"`, -1},
		{"i far negative", `"x"`, -5},
		{"empty buffer", ``, 0},
		{"not a quote at i", `a"x"`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := SkipJSONString([]byte(tc.b), tc.i)
			if ok || n != 0 {
				t.Errorf("SkipJSONString(%q, %d) = (%d, %v), want (0, false)", tc.b, tc.i, n, ok)
			}
		})
	}
}

// TestSkipJSONString_ValidInputUnchanged is the other half: the guard must
// not have narrowed what the function accepts.
func TestSkipJSONString_ValidInputUnchanged(t *testing.T) {
	cases := []struct {
		b    string
		i    int
		want int
	}{
		{`"abc"`, 0, 5},
		{`{"k":"v"}`, 1, 4},
		{`"a\"b"`, 0, 6},
		{`""`, 0, 2},
		{`"unterminated`, 0, 0}, // no closing quote: (0, false)
	}
	for _, tc := range cases {
		n, ok := SkipJSONString([]byte(tc.b), tc.i)
		wantOK := tc.want != 0
		if n != tc.want || ok != wantOK {
			t.Errorf("SkipJSONString(%q, %d) = (%d, %v), want (%d, %v)", tc.b, tc.i, n, ok, tc.want, wantOK)
		}
	}
}

// TestScanners_HostileIndexNeverPanics extends the SkipJSONString guard above
// to the rest of this package's index-taking exported surface. Same rationale:
// no in-repo caller passes any of these, but every one of them panicked
// before, and an exported function in a zero-internal-dependency leaf package
// has an open caller set. Each case below was verified to panic without the
// entry guards.
func TestScanners_HostileIndexNeverPanics(t *testing.T) {
	// "[  " / "{  " open a container and then run to EOF on whitespace: that
	// is what walks i up to len(raw) while an over-long end bound keeps the
	// `i >= end` guard from firing, which is how an out-of-range end reaches
	// a raw[i] read. A well-formed input never gets that far.
	arr := []byte("[  ")
	obj := []byte("{  ")
	full := []byte(`[{"role":"user"}]`)
	noVisit := func(start, end int) bool { return false }

	t.Run("SkipJSONValue negative index", func(t *testing.T) {
		if n, ok := SkipJSONValue(full, -1); ok || n != 0 {
			t.Errorf("= (%d, %v), want (0, false)", n, ok)
		}
	})
	t.Run("WalkArrayElements negative start", func(t *testing.T) {
		if found, ok := WalkArrayElements(full, -1, len(full), noVisit); found || ok {
			t.Errorf("= (%v, %v), want (false, false)", found, ok)
		}
	})
	t.Run("WalkArrayElements end past buffer", func(t *testing.T) {
		if found, ok := WalkArrayElements(arr, 0, len(arr)+5, noVisit); found || ok {
			t.Errorf("= (%v, %v), want (false, false)", found, ok)
		}
	})
	t.Run("WalkArrayElements inverted range", func(t *testing.T) {
		if found, ok := WalkArrayElements(full, 5, 2, noVisit); found || ok {
			t.Errorf("= (%v, %v), want (false, false)", found, ok)
		}
	})
	t.Run("FirstArrayElement end past buffer", func(t *testing.T) {
		if b, ok := FirstArrayElement(arr, 0, len(arr)+5); ok || b != nil {
			t.Errorf("= (%q, %v), want (nil, false)", b, ok)
		}
	})
	t.Run("ElementRole negative start", func(t *testing.T) {
		if r, ok := ElementRole(full, -1, len(full)); ok || r != "" {
			t.Errorf("= (%q, %v), want (\"\", false)", r, ok)
		}
	})
	t.Run("ElementRole end past buffer", func(t *testing.T) {
		if r, ok := ElementRole(obj, 0, len(obj)+5); ok || r != "" {
			t.Errorf("= (%q, %v), want (\"\", false)", r, ok)
		}
	})
}

// TestSkipJSONValue_BareLiteralAtEOF covers the one shape no in-repo caller
// produces (every one of them scans inside a container, where a delimiter
// always arrives before EOF): a scalar that runs to the end of the buffer.
// Refusing it would contradict SkipJSONValue's own doc comment, and this is an
// exported leaf-package function whose caller set is open. The negative cases
// pin the zero-progress rule that must survive alongside it — a buffer that is
// nothing but a delimiter, or nothing at all, still fails.
func TestSkipJSONValue_BareLiteralAtEOF(t *testing.T) {
	cases := []struct {
		b    string
		want int // 0 == expect ok=false
	}{
		{"123", 3},
		{"true", 4},
		{"false", 5},
		{"null", 4},
		{"-1.5e10", 7},
		{"123 ", 3},  // trailing space still wins, unchanged
		{"123,", 3},  // trailing delimiter still wins, unchanged
		{`"s"`, 3},   // string branch, untouched by this rule
		{"[1,2]", 5}, // container branch, untouched by this rule
		{",", 0},     // delimiter only: no token at all
		{"}", 0},
		{" ", 0},
	}
	for _, tc := range cases {
		n, ok := SkipJSONValue([]byte(tc.b), 0)
		wantOK := tc.want != 0
		if n != tc.want || ok != wantOK {
			t.Errorf("SkipJSONValue(%q, 0) = (%d, %v), want (%d, %v)", tc.b, n, ok, tc.want, wantOK)
		}
	}
}

// TestSkipJSONWS_OutOfRangeSaturatesForward pins the property the rest of the
// package leans on: SkipJSONWS can be handed any int, and anything outside
// the buffer comes back >= len(b), so a caller's `i >= bound` check catches
// it. A negative index must NOT clamp to 0 — that would silently scan from
// the start of the buffer and let WalkArrayElements(raw, -1, ...) walk a real
// array in answer to a nonsense request.
func TestSkipJSONWS_OutOfRangeSaturatesForward(t *testing.T) {
	b := []byte("  [1]")
	for _, i := range []int{-1, -5, -1 << 30} {
		if got := SkipJSONWS(b, i); got < len(b) {
			t.Errorf("SkipJSONWS(%q, %d) = %d, want >= len(b)=%d", b, i, got, len(b))
		}
	}
	if got := SkipJSONWS(b, len(b)+3); got < len(b) {
		t.Errorf("SkipJSONWS(%q, %d) = %d, want >= len(b)=%d", b, len(b)+3, got, len(b))
	}
	// Unchanged for every valid index.
	if got := SkipJSONWS(b, 0); got != 2 {
		t.Errorf("SkipJSONWS(%q, 0) = %d, want 2", b, got)
	}
	if got := SkipJSONWS(b, 3); got != 3 {
		t.Errorf("SkipJSONWS(%q, 3) = %d, want 3", b, got)
	}
}

func TestTopLevelValuesPrefix(t *testing.T) {
	t.Parallel()

	t.Run("truncated after model finds key", func(t *testing.T) {
		raw := []byte(`{"id":"x","model":"upstream-model","choices":[{"message":{"content":"half`)
		ranges, ok := TopLevelValuesPrefix(raw, modelKeyLiteral)
		if !ok || len(ranges) != 1 {
			t.Fatalf("expected 1 range, got ok=%v, ranges=%v", ok, ranges)
		}
		got := string(raw[ranges[0][0]:ranges[0][1]])
		if got != `"upstream-model"` {
			t.Errorf("got %q, want %q", got, `"upstream-model"`)
		}
	})

	t.Run("truncated before model returns false", func(t *testing.T) {
		raw := []byte(`{"id":"x","mod`)
		ranges, ok := TopLevelValuesPrefix(raw, modelKeyLiteral)
		if ok || len(ranges) != 0 {
			t.Errorf("expected ok=false, got ok=%v, ranges=%v", ok, ranges)
		}
	})

	t.Run("complete object finds key", func(t *testing.T) {
		raw := []byte(`{"id":"x","model":"m"}`)
		ranges, ok := TopLevelValuesPrefix(raw, modelKeyLiteral)
		if !ok || len(ranges) != 1 {
			t.Fatalf("expected 1 range, got ok=%v, ranges=%v", ok, ranges)
		}
		if string(raw[ranges[0][0]:ranges[0][1]]) != `"m"` {
			t.Errorf("got %q, want %q", string(raw[ranges[0][0]:ranges[0][1]]), `"m"`)
		}
	})

	t.Run("nested model untouched", func(t *testing.T) {
		raw := []byte(`{"id":"x","tools":[{"model":"nested"}],"choices":`)
		ranges, ok := TopLevelValuesPrefix(raw, modelKeyLiteral)
		if ok || len(ranges) != 0 {
			t.Errorf("nested model should not be returned, got ok=%v, ranges=%v", ok, ranges)
		}
	})

	t.Run("not an object", func(t *testing.T) {
		raw := []byte(`["model","m"]`)
		ranges, ok := TopLevelValuesPrefix(raw, modelKeyLiteral)
		if ok || len(ranges) != 0 {
			t.Errorf("array should decline, got ok=%v, ranges=%v", ok, ranges)
		}
	})
}

func TestReplaceTopLevelValuePrefix(t *testing.T) {
	t.Parallel()

	t.Run("truncated after model rewrites value", func(t *testing.T) {
		raw := []byte(`{"id":"x","model":"upstream-model","choices":[{"message":{"content":"half`)
		newVal := []byte(`"agent"`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, newVal)
		if !ok {
			t.Fatalf("expected ok=true, got ok=false")
		}
		want := `{"id":"x","model":"agent","choices":[{"message":{"content":"half`
		if string(out) != want {
			t.Errorf("got %q, want %q", string(out), want)
		}
	})

	t.Run("truncated before model returns false and unchanged raw", func(t *testing.T) {
		raw := []byte(`{"id":"x","mod`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, []byte(`"agent"`))
		if ok {
			t.Errorf("expected ok=false, got ok=true")
		}
		if !bytes.Equal(out, raw) {
			t.Errorf("got %q, want unchanged raw %q", string(out), string(raw))
		}
	})

	t.Run("complete object rewrites model", func(t *testing.T) {
		raw := []byte(`{"id":"x","model":"old","stream":false}`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, []byte(`"new"`))
		if !ok {
			t.Fatalf("expected ok=true, got ok=false")
		}
		want := `{"id":"x","model":"new","stream":false}`
		if string(out) != want {
			t.Errorf("got %q, want %q", string(out), want)
		}
	})

	t.Run("same value returns zero-copy raw", func(t *testing.T) {
		raw := []byte(`{"model":"same","tail":123`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, []byte(`"same"`))
		if !ok {
			t.Fatalf("expected ok=true, got ok=false")
		}
		if &out[0] != &raw[0] {
			t.Errorf("expected zero-copy raw slice pointer, got different slice")
		}
	})

	t.Run("multiple matching keys replaced", func(t *testing.T) {
		raw := []byte(`{"model":"a","model":"b","partial":`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, []byte(`"c"`))
		if !ok {
			t.Fatalf("expected ok=true, got ok=false")
		}
		want := `{"model":"c","model":"c","partial":`
		if string(out) != want {
			t.Errorf("got %q, want %q", string(out), want)
		}
	})

	t.Run("nested model key untouched", func(t *testing.T) {
		raw := []byte(`{"id":"x","tools":[{"model":"nested"}],"choices":`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, []byte(`"agent"`))
		if ok {
			t.Errorf("expected ok=false for nested model, got ok=true")
		}
		if !bytes.Equal(out, raw) {
			t.Errorf("got %q, want unchanged raw %q", string(out), string(raw))
		}
	})

	t.Run("not an object declined", func(t *testing.T) {
		raw := []byte(`["model","old"]`)
		out, ok := ReplaceTopLevelValuePrefix(raw, modelKeyLiteral, []byte(`"agent"`))
		if ok {
			t.Errorf("expected ok=false for non-object, got ok=true")
		}
		if !bytes.Equal(out, raw) {
			t.Errorf("got %q, want unchanged raw %q", string(out), string(raw))
		}
	})
}
