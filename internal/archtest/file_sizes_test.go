// Ver 2026-07-26, by Sonnet 5
package archtest

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// defaultFileLineLimit bounds every production file under funcBudgetRoots
// (func_sizes_test.go) that isn't exempted below.
//
// A global default rather than the whitelist this used to be, for the reason
// func_sizes_test.go already gives for functions: a whitelist only constrains
// what someone remembered to register. Under the old one, 11 production files
// at 400+ lines had no budget at all while a registered file went red for a
// single added line — the guard punished the places already cleaned up.
//
// 700 comes from the real distribution (169 files; p50 131, p90 503) and was
// chosen so every file already over it was one this table had registered
// anyway. Tighten it once the exemption list is shorter, not before.
const defaultFileLineLimit = 700

// fileLineExemptions overrides the default in EITHER direction. Most entries
// are tighter than 700: they are tripwires on files a review already split
// once, and the point is that they cannot drift back up.
//
// What this table exists for: router.go once grew to 948 lines against a
// budget that only ever lived in a design-doc comment, and nobody noticed. A
// limit is set with headroom over a file's real post-split size — the point
// is catching regrowth, not fighting over every line.
var fileLineExemptions = map[string]int{
	// Convention for every entry: ~15% headroom over the file's real size when
	// the budget was set — a paragraph's worth of room, not an invitation to
	// regrow. When one trips, split the file and re-baseline on the result;
	// raising the number in place is what the failure message tells you not to
	// do. Only per-entry facts that aren't that convention are noted below.
	"internal/router/router.go": 700,

	// render_doc.go's budget is what keeps a new report section arriving as a
	// new section_*.go rather than as another 90 lines on the biggest file.
	"internal/report/aggregate.go":  600,
	"internal/report/render_doc.go": 400,
	"internal/report/ingest.go":     310,
	"internal/report/recextract.go": 310,
	// rows.go is the report's JSON contract: a new metric adds a field, so
	// growth is expected. What this catches is the file absorbing accumulation
	// or rendering logic again, which belongs in ingest.go/section_*.go.
	"internal/report/rows.go": 900,
	// detail.go was split into internal/reqdetail in P2, slimming it to ~286
	// lines. internal/config/config.go used to carry a 750 exemption here; it
	// is 699 lines, i.e. under the default, so the exemption was dropped
	// rather than kept as pre-authorized headroom. When it does cross 700,
	// split it by concern (e.g. provider/model validation into its own file)
	// — do not re-add an exemption.
	"internal/report/detail.go":  350,
	"internal/report/session.go": 1100,

	"internal/story/journey.go":             850,
	"internal/story/render_md.go":           350,
	"internal/story/render_spine.go":        380,
	"internal/story/render_spine_args.go":   200,
	"internal/story/findings.go":            580,
	"internal/story/findings_toolresult.go": 320,
	"internal/story/compare.go":             820,
	"internal/story/metrics.go":             470,
	"internal/story/corpus.go":              380,
	"internal/story/render_corpus.go":       150,

	"internal/respnorm/respnorm.go": 950,
	"internal/respnorm/minimax.go":  235,

	// The CLI is thin by design (parse flags, wire, delegate — see CLAUDE.md's
	// module map), so a subcommand crossing its budget means logic belongs in
	// an internal package, not that the number should go up.
	"cmd/vmr/cmd_story.go":  850,
	"cmd/vmr/cmd_check.go":  610,
	"cmd/vmr/cmd_report.go": 500,
	"cmd/vmr/cmd_status.go": 370,

	// classify.go's budget keeps it a thin error-classification file: the
	// generic JSON scanning it used to hold lives in internal/jsonscan now, and
	// a budget-less file can't tell a contributor they're rebuilding it.
	"internal/adapter/classify.go": 200,
	"internal/jsonscan/scan.go":    190,
	"internal/jsonscan/walk.go":    200,
	"internal/jsonscan/rewrite.go": 300,

	"internal/taskseg/taskseg.go":  70,
	"internal/taskseg/openclaw.go": 150,
	"internal/taskseg/segment.go":  200,
}

// TestArchitecture_CoreFileSizes counts newlines, exactly what `wc -l`
// reports (blank lines included — this test's own budgets above were set
// from that same count), so a contributor can reproduce a failure locally
// without reading this file's counting logic first.
//
// Walks funcBudgetRoots (internal/ + cmd/, defined in func_sizes_test.go)
// rather than only the exemption table's keys: the same "every file is
// bounded, not just the remembered ones" inversion defaultFileLineLimit's
// comment describes.
func TestArchitecture_CoreFileSizes(t *testing.T) {
	repoRoot := repoRootDir(t)
	seen := map[string]bool{}

	for _, root := range funcBudgetRoots {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Errorf("%s: %v", rel, readErr)
				return nil
			}
			n := bytes.Count(data, []byte("\n"))
			limit, exempt := fileLineExemptions[rel]
			if !exempt {
				limit = defaultFileLineLimit
			} else {
				seen[rel] = true
			}
			if n > limit {
				// "another file in the same package", not "under
				// internal/router" as this message used to say — the table has
				// covered report/story/config/cmd files for far longer than it
				// has covered only the router.
				t.Errorf("%s is %d lines, over its %d-line budget: split it "+
					"into another file in the same package, don't just raise "+
					"this number", rel, n, limit)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// Same reasoning as funcLineExemptions' staleness check: an entry naming
	// a file that no longer exists reads as "this file is still oversized"
	// and silently hands its headroom to whatever is written there next.
	var stale []string
	for rel := range fileLineExemptions {
		if !seen[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		t.Errorf("archtest's fileLineExemptions lists %s, but no such production file exists (renamed, moved, or deleted) — delete the entry", rel)
	}
}
