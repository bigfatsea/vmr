// Ver 2026-09-22 18:50, by Sonnet 5
package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// defaultFileLineLimit bounds every production file under funcBudgetRoots
// (func_sizes_test.go) that isn't exempted below, counted in net code lines
// (empty lines, pure // comments, and /* ... */ block comments excluded).
//
// 500 comes from the net code lines distribution across 302 production files
// (p50 96, p90 305, p95 407, max 548). Only four files naturally exceed 500,
// and all four are registered below with ~15% headroom.
const defaultFileLineLimit = 500

// fileLineExemptions overrides the default in EITHER direction.
//
// Upward entries provide ~15% headroom for cohesive files whose scope naturally
// exceeds 500 net code lines. Downward entries act as tripwires on files
// split in earlier refactors, ensuring modularized logic does not drift back up.
var fileLineExemptions = map[string]int{
	// Response normalizer state machine handling SSE frame splitting, model
	// rewrite, done delimiter completion, and vendor quirk repairs.
	"internal/respnorm/respnorm.go": 630,

	// LLM interpretation layer finding extractor: heuristic finding patterns,
	// schema definitions, and explanation models.
	"internal/journey/llm_findings.go": 625,

	// Report session aggregator: attaches requests, groups tasks, and computes
	// session-level metrics.
	"internal/report/session.go": 605,

	// Report aggregation tripwire: keeps report aggregation logic from absorbing
	// new section rendering (which belongs in viewmodel_*.go).
	"internal/report/aggregate.go": 475,

	// Ingest pipeline tripwire: prevents raw record decoding and normalization
	// from regrowing.
	"internal/report/ingest.go": 250,

	// Record fact extraction tripwire: prevents per-record parsing logic from
	// expanding.
	"internal/report/recextract.go": 235,

	// Report detail tripwire: keeps detail page formatting separated from
	// record fact extraction.
	"internal/report/detail.go": 200,

	// Markdown rendering tripwire for journey narrative.
	"internal/journey/render_md.go": 55,

	// Timeline spine renderer tripwire.
	"internal/journey/render_spine.go": 60,

	// Spine argument formatting tripwire.
	"internal/journey/render_spine_args.go": 65,

	// Finding detector registration and localization sites.
	"internal/journey/findings.go": 435,

	// Tool result finding detectors.
	"internal/journey/findings_toolresult.go": 250,

	// Journey metrics calculation tripwire.
	"internal/journey/metrics.go": 330,

	// Benchmark aggregation tripwire.
	"internal/journey/benchmarks.go": 300,

	// Benchmark Markdown report rendering tripwire.
	"internal/journey/render_benchmarks.go": 125,

	// MiniMax vendor quirk repair tripwire.
	"internal/respnorm/minimax.go": 175,

	// Config validation CLI command tripwire: validates providers, models, and routes.
	"cmd/vmr/cmd_check.go": 415,

	// Status inspection CLI command tripwire.
	"cmd/vmr/cmd_status.go": 260,

	// Error classification tripwire: keeps classify.go a thin mapper; JSON scanning
	// belongs in internal/jsonscan.
	"internal/adapter/classify.go": 170,

	// Low-level byte scanning primitive tripwire.
	"internal/jsonscan/scan.go": 155,

	// JSON string/array walk primitive tripwire.
	"internal/jsonscan/walk.go": 165,

	// Byte-splice rewrite primitive tripwire.
	"internal/jsonscan/rewrite.go": 270,

	// Task segmentation interface tripwire.
	"internal/taskseg/taskseg.go": 30,

	// OpenClaw dialect profile tripwire.
	"internal/taskseg/openclaw.go": 120,

	// Task segmentation state machine tripwire.
	"internal/taskseg/segment.go": 165,

	// Skeleton page embed and writer tripwire; HTML/JS assets live in embedded assets.
	"internal/dashboard/dashboard.go": 105,
}

// TestArchitecture_CoreFileSizes bounds every production file under
// funcBudgetRoots to its net code line budget (empty lines and comments excluded).
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
			n := codeLines(data)
			limit, exempt := fileLineExemptions[rel]
			if !exempt {
				limit = defaultFileLineLimit
			} else {
				seen[rel] = true
			}
			if n > limit {
				t.Errorf("%s is %d lines, over the %d-line budget (net code lines, comments excluded). Either raise the number in the table when the logic is cohesive, or split it — a split must introduce a named abstraction (a type or struct), not just spread parameters across helpers.", rel, n, limit)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// Staleness check: an entry naming a file that no longer exists reads as
	// "this file is still oversized" and silently hands its headroom to whatever
	// is written there next.
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
