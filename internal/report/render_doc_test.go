// Ver 2026-08-05, by Sonnet 5
package report

import (
	"testing"
	"time"

	"vmr/internal/fmtutil"
)

// TestFmtDisplayFullConvertsToDisplayZone proves fmtDisplayFull (used by
// Markdown's header/appendix "period" lines via rep.Meta.From/rep.Meta.To,
// and by requests.go's session/task headers via RequestRow.TS) actually
// converts through fmtutil.DisplayZone rather than reading the input
// timestamp's own embedded offset — the behavior change from the old
// cut(rep.Meta.From, 19) truncation this replaced. The input below carries
// a +08:00 offset; DisplayZone is overridden to a different, known
// -05:00 zone, so a correct conversion and a "just truncate the string"
// bug produce visibly different wall-clock output.
func TestFmtDisplayFullConvertsToDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.FixedZone("TEST-05:00", -5*3600)
	defer func() { fmtutil.DisplayZone = origZone }()

	// 2026-07-24T08:17:58+08:00 is 2026-07-24T00:17:58 UTC, which is
	// 2026-07-23T19:17:58 in the -05:00 TEST zone above.
	const in = "2026-07-24T08:17:58+08:00"
	const want = "2026-07-23 19:17:58"

	got := fmtDisplayFull(in)
	if got != want {
		t.Errorf("fmtDisplayFull(%q) = %q, want %q (DisplayZone conversion not applied)", in, got, want)
	}

	// Sanity: the old cut(rep.Meta.From, 19) behavior would have produced
	// this instead — assert we're NOT seeing it, to make the "still just
	// truncating" regression explicit rather than implicit in the want above.
	if oldStyle := cut(in, 19); got == oldStyle {
		t.Errorf("fmtDisplayFull(%q) = %q matches the old cut()-truncation output %q; DisplayZone conversion appears to be a no-op", in, got, oldStyle)
	}
}

// TestFmtDisplayFullUsesSpaceSeparator proves the other half of the
// behavior change from the old cut()-based rendering: a space between date
// and time ("2026-07-24 00:17:58"), not RFC3339's "T" ("2026-07-24T00:17:58").
// Uses UTC input/DisplayZone so the assertion is purely about the
// separator, independent of the conversion proven above.
func TestFmtDisplayFullUsesSpaceSeparator(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	const in = "2026-07-24T00:17:58Z"
	const want = "2026-07-24 00:17:58"

	got := fmtDisplayFull(in)
	if got != want {
		t.Errorf("fmtDisplayFull(%q) = %q, want %q (space separator, not RFC3339 T)", in, got, want)
	}
}
