// Ver 2026-09-23 04:17, by Claude Opus 5.5

package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// defaultFuncLineLimit bounds a single function body, in net code lines, for
// every production function under internal/ and cmd/ that isn't listed in
// funcLineExemptions below (empty lines, pure // comments, and /* ... */ block
// comments excluded).
//
// 95 was chosen from the actual distribution of net code lines across 1711
// production functions (p50 12, p90 38, p95 51, p99 85, max 127). Only six
// functions exceed 95, all of which are linear compositions registered below.
const defaultFuncLineLimit = 95

// funcLineExemptions is every production function currently over the default
// limit, with ~15% headroom over its current net code line size.
var funcLineExemptions = map[string]int{
	// Top-level command composition: flag parsing, configuration loading,
	// component wiring, and process signal lifecycle.
	"cmd/vmr/cmd_start.go:cmdStart": 150,

	// Diagnostic suite entry point: sequential probe dispatch and terminal
	// report composition.
	"internal/diagnose/diagnose.go:Run": 150,

	// Report reliability section builder: multi-table reliability viewmodel
	// assembly across error classes and fallback cascades.
	"internal/report/viewmodel_reliability.go:vmReliabilitySection": 125,

	// The analytics half's canonical per-record fact extraction: one pass
	// over an audit.Record producing the whole report-side recordFacts
	// projection (aggregation + session features + guard forensics). Linear
	// field collection, no branching depth.
	"internal/report/factscache.go:extractRecordFacts": 140,

	// Core HTTP ingress handler: linear request lifecycle composition (auth,
	// body buffering, probe check, guard gate, image downscale, facts
	// extraction, and router dispatch).
	"internal/server/server.go:chatHandler": 115,

	// Audit record replay runner: coordinates record resolution, upstream client
	// construction, and response comparison.
	"internal/replay/replay.go:Run": 115,

	// Unified report loader: reads and validates all JSON artifact slices from
	// disk into an in-memory Report document.
	"internal/report/viewmodel_doc.go:LoadReport": 115,

	// Journey assembly loop: walks one stitched lineage chain step by step,
	// resolving segmentation, body parsing and event de-duplication in the
	// order they depend on each other.
	"internal/journey/journey.go:buildFrom": 115,
}

// funcBudgetRoots are the trees this test governs: the shipped binary's own
// code.
var funcBudgetRoots = []string{"internal", "cmd"}

// funcBudgetExemptPkgs are packages whose "functions" are string tables, not
// control flow.
var funcBudgetExemptPkgs = map[string]bool{
	"internal/i18n": true,
}

// TestArchitecture_FuncSizes bounds single-function length in net code lines
// (empty lines and comments excluded).
func TestArchitecture_FuncSizes(t *testing.T) {
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
			if funcBudgetExemptPkgs[filepath.ToSlash(filepath.Dir(rel))] {
				return nil
			}
			fset := token.NewFileSet()
			f, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				t.Errorf("%s: %v", rel, parseErr)
				return nil
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := rel + ":" + fd.Name.Name
				n := codeLinesInRange(fset, f, fd.Body.Pos(), fd.Body.End())
				limit, exempt := funcLineExemptions[key]
				if !exempt {
					limit = defaultFuncLineLimit
				} else {
					seen[key] = true
				}
				if n > limit {
					t.Errorf("%s is %d lines, over the %d-line budget (net code lines, comments excluded). Either raise the number in the table when the logic is cohesive, or split it — a split must introduce a named abstraction (a type or struct), not just spread parameters across helpers.", key, n, limit)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// Staleness check: an entry naming a function that no longer exists reads
	// as "this function is still oversized" long after someone refactored it.
	var stale []string
	for key := range funcLineExemptions {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("archtest's funcLineExemptions lists %s, but no such function exists (renamed, moved, or already split) — delete the entry", key)
	}
}

// repoRootDir locates the module root via the go tool rather than by walking
// up looking for go.mod.
func repoRootDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("go env GOMOD: not inside a module")
	}
	return filepath.Dir(gomod)
}
