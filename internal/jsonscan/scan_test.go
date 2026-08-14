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
