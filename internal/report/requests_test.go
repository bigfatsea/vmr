// Ver 2026-08-31, by Sonnet 5
package report

import "testing"

// TestFinishCell_UnclassifiedFailureIsNotABareDash pins the fix for the
// pre-routing-reject / queue-cancel case: outcome != "ok" with an empty
// ErrorClass (nothing ever reached an upstream to classify it) must still
// render a visible failure marker in the per-turn / scheduled tables, not
// fall through to orDash(Finish) and read as a normal finish. It also
// keeps finishCell consistent with outcomeCell's own "unclassified"
// fallback.
func TestFinishCell_UnclassifiedFailureIsNotABareDash(t *testing.T) {
	cases := []struct {
		name string
		row  RequestRow
		want string
	}{
		{"error, no class", RequestRow{Outcome: "error"}, "❌unclassified"},
		{"canceled while queued", RequestRow{Outcome: "canceled"}, "❌unclassified"},
		{"error, classified", RequestRow{Outcome: "error", ErrorClass: "transient"}, "❌transient"},
		{"canceled, classified", RequestRow{Outcome: "canceled", ErrorClass: "canceled"}, "❌canceled"},
		{"ok, truncated", RequestRow{Outcome: "ok", Truncated: true}, "⚠️trunc"},
		{"ok, normal finish", RequestRow{Outcome: "ok", Finish: "stop"}, "stop"},
		{"ok, no finish reason", RequestRow{Outcome: "ok"}, "-"},
	}
	for _, c := range cases {
		if got := finishCell(c.row); got != c.want {
			t.Errorf("%s: finishCell = %q, want %q", c.name, got, c.want)
		}
	}
}
