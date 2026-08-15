// Ver 2026-08-14, by Sonnet 5
package jsonscan

import "testing"

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
