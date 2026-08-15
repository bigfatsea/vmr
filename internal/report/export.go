// Ver 2026-07-25, by Sonnet 5

// ToolShapeStats/ToolShapes: per declared-tool-set usage aggregation,
// consumed by the report §7 tool-waste section (recextract.go's buildTools
// converts every field into the report's own richer ToolShapeRow, rows.go).
package report

import "sort"

// ToolShapeStats aggregates tool declaration vs. actual use for one request
// shape (a distinct declared-tool set). "Actual use" counts only each
// request's own turn — extracted from the response, so tool calls repeated
// through resent history are never double-counted. Named distinctly from
// the package's own richer ToolShapeRow (rows.go) — that one adds
// derived waste/utilization fields on top of these raw counts.
type ToolShapeStats struct {
	Shape         string         `json:"shape"`
	Requests      int            `json:"requests"`
	Declared      []string       `json:"declared"`
	Calls         map[string]int `json:"calls,omitempty"`
	NeverCalled   []string       `json:"never_called,omitempty"`
	DeclaredBytes int64          `json:"declared_bytes"` // per-request cost of the tools JSON
}

// ToolShapes aggregates the analysis into per-shape tool usage rows,
// most-used shape first. Shapeless records (no tools field) are skipped.
func (a *SessionAnalysis) ToolShapes() []ToolShapeStats {
	byShape := map[string]*ToolShapeStats{}
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
			row = &ToolShapeStats{Shape: sig, Declared: r.ToolsDeclared,
				Calls: map[string]int{}, DeclaredBytes: r.declBytes}
			byShape[sig] = row
		}
		row.Requests++
		for _, name := range r.ToolCalls {
			row.Calls[name]++
		}
	}
	out := make([]ToolShapeStats, 0, len(byShape))
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
