// Ver 2026-07-25, by Sonnet 5

// ToolShapeRow/ToolShapes: per declared-tool-set usage aggregation, reused
// by report2's §7 tool-waste section (internal/report2 converts every field
// into its own richer ToolShapeRow — see report2.go's buildTools).
package report

import "sort"

// ToolShapeRow aggregates tool declaration vs. actual use for one request
// shape (a distinct declared-tool set). "Actual use" counts only each
// request's own turn — extracted from the response, so tool calls repeated
// through resent history are never double-counted.
type ToolShapeRow struct {
	Shape         string         `json:"shape"`
	Requests      int            `json:"requests"`
	Declared      []string       `json:"declared"`
	Calls         map[string]int `json:"calls,omitempty"`
	NeverCalled   []string       `json:"never_called,omitempty"`
	DeclaredBytes int64          `json:"declared_bytes"` // per-request cost of the tools JSON
}

// ToolShapes aggregates the analysis into per-shape tool usage rows,
// most-used shape first. Shapeless records (no tools field) are skipped.
func (a *SessionAnalysis) ToolShapes() []ToolShapeRow {
	byShape := map[string]*ToolShapeRow{}
	for _, r := range a.Recs {
		if r.ToolsSig == "" && len(r.ToolCalls) == 0 {
			continue
		}
		sig := r.ToolsSig
		if sig == "" {
			sig = "tools:0"
		}
		row := byShape[sig]
		if row == nil {
			row = &ToolShapeRow{Shape: sig, Declared: r.ToolsDeclared,
				Calls: map[string]int{}, DeclaredBytes: r.declBytes}
			byShape[sig] = row
		}
		row.Requests++
		for _, name := range r.ToolCalls {
			row.Calls[name]++
		}
	}
	out := make([]ToolShapeRow, 0, len(byShape))
	for _, row := range byShape {
		for _, name := range row.Declared {
			if row.Calls[name] == 0 {
				row.NeverCalled = append(row.NeverCalled, name)
			}
		}
		sort.Strings(row.NeverCalled)
		if len(row.Calls) == 0 {
			row.Calls = nil
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	return out
}
