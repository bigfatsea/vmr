// Ver 2026-09-12 12:00, by dev

// The report side's end-to-end Markdown smoke (Phase 3, Step 3.7 / weight
// doc's阶段 F "补齐" item): internal/journey already has one
// (golden_test.go's TestGoldenMarkdown, builder -> VM -> byte-level .md
// golden); internal/report only had a structured VM golden
// (viewmodel_golden_test.go) plus targeted unit assertions
// (viewmodel_test.go) — nothing pinned the final Markdown bytes produced
// by the full builder chain. This file closes that gap using the same
// goldenFixture() the VM-structure golden already exercises, so both
// goldens describe the same underlying report.
package report

import (
	"os"
	"path/filepath"
	"testing"

	"vmr/internal/i18n"
)

// TestGoldenReportMarkdown pins MacroMarkdown's byte output for the
// goldenFixture() Report (viewmodel_golden_test.go), one file per
// language. Regenerate after an INTENTIONAL rendering change with
// UPDATE_MD_GOLDEN=1 go test ./internal/report/ -run TestGoldenReportMarkdown
// — review the diff, then commit.
func TestGoldenReportMarkdown(t *testing.T) {
	fixture := goldenFixture()
	journeyLink := map[string]string{"l-d1": "j-claw-b-1.md"}
	journeyIdx := &JourneysLinkInfo{Path: "journeys/index.md", JourneyCount: 2,
		FromDisplay: "2026-07-23 02:39:00", ToDisplay: "2026-07-24 10:00:00"}

	for _, tc := range []struct {
		lang   i18n.Lang
		golden string
	}{
		{i18n.EN, filepath.Join("testdata", "golden_report.md")},
		{i18n.ZH, filepath.Join("testdata", "golden_report_zh.md")},
	} {
		t.Run(tc.lang.String(), func(t *testing.T) {
			got := MacroMarkdown(fixture, tc.lang, journeyIdx, journeyLink)

			if os.Getenv("UPDATE_MD_GOLDEN") != "" {
				if err := os.WriteFile(tc.golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Skipf("regenerated %s — review the diff, then re-run without UPDATE_MD_GOLDEN", tc.golden)
			}

			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("reading golden file (run with UPDATE_MD_GOLDEN=1 to create it): %v", err)
			}
			if got != string(want) {
				t.Errorf("golden Markdown mismatch — after reviewing why, regenerate with UPDATE_MD_GOLDEN=1 and diff the result before committing.\n=== got ===\n%s\n=== want ===\n%s", got, string(want))
			}
		})
	}
}
