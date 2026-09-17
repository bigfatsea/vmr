// Ver 2026-09-13, by Sonnet 5

package report

import (
	"strings"
	"testing"

	"vmr/internal/i18n"
)

// TestVMGuardSection_NilIsZeroValue is the ADR-12 backward-compat
// assertion at the unit level: a Report2 with Guard == nil (every report
// today) renders no "guard" section at all — the zero SectionVM
// BuildMacroReportVM's own loop already skips (vmStickySection's pattern).
func TestVMGuardSection_NilIsZeroValue(t *testing.T) {
	rep := &Report2{}
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		sec := vmGuardSection(rep, lang)
		if sec.Title != "" || len(sec.Blocks) != 0 {
			t.Errorf("lang=%v: vmGuardSection on nil Guard = %+v, want zero value", lang, sec)
		}
	}
}

// TestBuildGuardSlice_NilOnNoGuard confirms the JSON slice side of the same
// contract: no Guard means no slice to write (slices.go's WriteMacroSlices
// skips macro/guard.json entirely in that case).
func TestBuildGuardSlice_NilOnNoGuard(t *testing.T) {
	if gs := BuildGuardSlice(&Report2{}); gs != nil {
		t.Errorf("BuildGuardSlice(no Guard) = %+v, want nil", gs)
	}
	if gs := BuildGuardSlice(nil); gs != nil {
		t.Errorf("BuildGuardSlice(nil Report2) = %+v, want nil", gs)
	}
}

// syntheticGuardReport builds a Report2 with a populated GuardSummary —
// the shape aggregate.go's guardCollector would produce once M3/M4 wiring
// exists, used here to exercise the render path end-to-end without that
// wiring.
func syntheticGuardReport() *Report2 {
	return &Report2{
		Guard: &GuardSummary{
			RecordsScanned:  120,
			RecordsWithHits: 5,
			RulesetVersion:  1,
			Rules: []GuardRuleRow{
				{Rule: "gcp-api-key", Tier: 1, UniqueFP: 2, TotalHits: 8, RecordsWith: 3, MaxPerRecord: 4},
				{Rule: "generic-sk-prefix", Tier: 2, UniqueFP: 6, TotalHits: 40, RecordsWith: 4, MaxPerRecord: 12},
			},
		},
	}
}

// TestVMGuardSection_Populated confirms both languages render the section
// with the tier split, the amplification note, and the block/restore
// lines — end-to-end proof the M2 wiring (aggregate -> Report2 -> ViewModel)
// actually produces readable output once a producer (M3/M4) exists.
func TestVMGuardSection_Populated(t *testing.T) {
	rep := syntheticGuardReport()
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		sec := vmGuardSection(rep, lang)
		if sec.Title == "" {
			t.Fatalf("lang=%v: vmGuardSection on populated Guard returned zero value", lang)
		}
		var rendered strings.Builder
		for _, blk := range sec.Blocks {
			switch b := blk.(type) {
			case ParaVM:
				rendered.WriteString(b.Text)
			case *TableVM:
				for _, row := range b.Rows {
					rendered.WriteString(strings.Join(row, "|"))
					rendered.WriteByte('\n')
				}
			}
		}
		out := rendered.String()
		for _, want := range []string{"gcp-api-key", "generic-sk-prefix"} {
			if !strings.Contains(out, want) {
				t.Errorf("lang=%v: rendered section missing %q:\n%s", lang, want, out)
			}
		}
	}
}

// TestBuildGuardSlice_Populated confirms the JSON slice carries the
// summary through unchanged.
func TestBuildGuardSlice_Populated(t *testing.T) {
	rep := syntheticGuardReport()
	gs := BuildGuardSlice(rep)
	if gs == nil {
		t.Fatal("BuildGuardSlice returned nil for a populated Guard")
	}
	if gs.Summary.RecordsScanned != 120 || len(gs.Summary.Rules) != 2 {
		t.Errorf("GuardSlice.Summary = %+v, did not carry Report2.Guard through unchanged", gs.Summary)
	}
}
