// Ver 2026-08-31, by Sonnet 5

package report

import (
	"fmt"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

// TestRenderToolWasteTotals: §7's top-line carries the four window totals
// tool-waste.html leads with (问题 3 / R3a-2) — they otherwise appeared
// nowhere in vmr-report.md.
func TestRenderToolWasteTotals(t *testing.T) {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }

	rep := &Report2{Tools: []ToolShapeRow{
		{Shape: "tools:8/aaa", Requests: 40, SchemaBytesShipped: 8_000_000, SchemaWasteBytes: 6_000_000},
		{Shape: "tools:2/bbb", Requests: 10, SchemaBytesShipped: 2_000_000, SchemaWasteBytes: 0},
	}}
	renderToolWasteTotals(w, rep, i18n.EN)
	got := b.String()
	for _, want := range []string{"Total shipped", "Dead weight", "tokens wasted", "Tool-set shapes", "(60%)", "2"} {
		if !strings.Contains(got, want) {
			t.Errorf("§7 top-line missing %q:\n%s", want, got)
		}
	}

	// no tool shapes → no line at all
	b.Reset()
	renderToolWasteTotals(w, &Report2{}, i18n.EN)
	if b.Len() != 0 {
		t.Errorf("empty rep.Tools should emit nothing, got %q", b.String())
	}
}
