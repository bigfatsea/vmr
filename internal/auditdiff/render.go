// Ver 2026-09-13, by Sonnet 5

package auditdiff

import (
	"fmt"
	"io"
)

// Render outputs the human-readable diff matching vmr diff's formatting convention.
func Render(w io.Writer, rep Report) {
	fmt.Fprintf(w, "vmr diff %s vs %s\n\n", rep.CoordA, rep.CoordB)

	// Determine column widths across all sections
	wA := 20
	wB := 20
	for _, row := range append(append(rep.HeaderRows, rep.SystemRows...), rep.ToolsRows...) {
		if len(row.ValA) > wA {
			wA = len(row.ValA)
		}
		if len(row.ValB) > wB {
			wB = len(row.ValB)
		}
	}

	renderSection(w, "Header", rep.HeaderRows, wA, wB)
	renderSection(w, "System", rep.SystemRows, wA, wB)
	renderSection(w, "Tools", rep.ToolsRows, wA, wB)

	fmt.Fprintf(w, "Messages (A=%d, B=%d, LCP=%d)\n", rep.MsgCountA, rep.MsgCountB, rep.LCP)
	fmt.Fprintln(w, rep.DivLine)
	fmt.Fprintln(w, rep.TailLineA)
	fmt.Fprintln(w, rep.TailLineB)
	fmt.Fprintln(w)

	fmt.Fprintln(w, rep.Verdict)
}

func renderSection(w io.Writer, title string, rows []Row, wA, wB int) {
	fmt.Fprintf(w, "%s\n", title)
	for _, r := range rows {
		if r.Ann != "" {
			fmt.Fprintf(w, "  %-12s %-*s %-*s %s\n", r.Label, wA, r.ValA, wB, r.ValB, r.Ann)
		} else {
			fmt.Fprintf(w, "  %-12s %-*s %s\n", r.Label, wA, r.ValA, r.ValB)
		}
	}
	fmt.Fprintln(w)
}
