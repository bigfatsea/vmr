// Ver 2026-08-14, by Opus 5

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

// defaultFuncLineLimit bounds a single function body, in lines, for every
// production function under internal/ and cmd/ that isn't listed in
// funcLineExemptions below.
//
// Why this test exists at all: a file can sit comfortably inside
// file_sizes_test.go's budget while containing one function nobody can hold in
// their head. internal/report/aggregate.go was exactly that — 975 lines
// against a 1000-line budget, with a single 639-line buildInternal inside it,
// and the file budget green the whole time.
//
// Why a global default with exemptions, rather than a whitelist: a whitelist
// only ever constrains what someone remembered to register, so a brand-new
// 400-line function lands green. This inverts that — growing past the bound is
// a deliberate, reviewed act of adding a line below. file_sizes_test.go has
// since been inverted the same way, for the same reason.
//
// 120 was picked from the actual distribution (976 production functions;
// p95 ≈ 50 lines, 20 over 120) so it lands above ordinary code and below the
// handful of genuinely oversized ones, all of which are named below. Tighten
// it once that list is shorter, not before — a limit that forces a dozen
// unrelated refactors on the day it lands gets reverted, not respected.
const defaultFuncLineLimit = 120

// funcLineExemptions is every production function currently over the limit,
// with the bound it is held to. Recorded at ~current size, NOT rounded up:
// the point is that these cannot grow further without a deliberate edit here.
//
// Adding an entry is legitimate — some functions really are long (a protocol
// state machine, a validation pass with 30 independent checks). Raising an
// existing entry's number to make room for new code is what this table is
// designed to make visible.
var funcLineExemptions = map[string]int{
	// Top-level command/entry-point bodies: flag parsing, wiring, and a
	// linear happy path. Long because they are compositions, not algorithms —
	// splitting them tends to produce helpers with one caller and no
	// independent meaning.
	"internal/diagnose/diagnose.go:Run":    190,
	"internal/replay/replay.go:Run":        160,
	"cmd/vmr/cmd_start.go:cmdStart":        160,
	"cmd/vmr/cmd_story.go:compareJourneys": 125,
	// cmdReport/cmdStory/cmdAnalyze themselves stay below the default limit
	// once P9.1 (CLI convergence) pulled their linear pipelines out into
	// runReport/setupStoryRun/dispatchAnalyze below — those three inherit
	// the "composition, not an algorithm" reasoning above; the top-level
	// entry points no longer need an entry here at all (removing an entry
	// a function has outgrown, rather than leaving a now-meaningless
	// higher ceiling, is the same hygiene this table asks of growth in the
	// other direction).
	"cmd/vmr/cmd_report.go:runReport": 121,

	// internal/router/router.go's Serve is the failover loop itself — the
	// design doc's own budget for it is the FILE (see file_sizes_test.go), on
	// principle that the sequence health→condition→sort→quota→sticky→retry
	// reads as one thing. Bounded here so it can't quietly absorb more.
	"internal/router/router.go:Serve":       190,
	"internal/server/server.go:chatHandler": 175,

	"internal/report/section_reliability.go:renderReliability": 135,

	// One config-validation pass with many independent checks, and one SSE
	// reassembly state machine.
	"internal/config/config.go:validate":    145,
	"internal/chatmsg/sse.go:ReassembleSSE": 125,
}

// funcBudgetRoots are the trees this test governs: the shipped binary's own
// code. loadtest/ and tools/ are developer utilities that never run in a
// user's process, and holding a throwaway generator's main() to the same bar
// as the routing core buys nothing.
var funcBudgetRoots = []string{"internal", "cmd"}

// funcBudgetExemptPkgs are packages whose "functions" are string tables, not
// control flow. internal/i18n's per-section constructors are a `return
// XxxText{...}` literal and nothing else — internal/i18n/report_detail.go's
// Detail is 305 lines of translated strings with zero branching beyond the
// one `if lang == ZH`. A line budget there measures how much text a report
// section renders, which is not a complexity signal and not something anyone
// should refactor to satisfy.
var funcBudgetExemptPkgs = map[string]bool{
	"internal/i18n": true,
}

// TestArchitecture_FuncSizes bounds single-function length. Counts body lines
// the same way `wc -l` counts a file (closing brace line minus opening brace
// line), so a failure is reproducible by eye without reading this file first.
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
				n := fset.Position(fd.Body.End()).Line - fset.Position(fd.Body.Pos()).Line
				limit, exempt := funcLineExemptions[key]
				if !exempt {
					limit = defaultFuncLineLimit
				} else {
					seen[key] = true
				}
				if n > limit {
					if exempt {
						t.Errorf("%s is %d lines, over its %d-line exemption: shorten it, "+
							"don't raise the number in archtest's funcLineExemptions", key, n, limit)
					} else {
						t.Errorf("%s is %d lines, over the %d-line default: split it into "+
							"named helpers. Adding it to archtest's funcLineExemptions is for "+
							"functions that genuinely can't be split, not for making room", key, n, defaultFuncLineLimit)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// A stale exemption is worse than none: it reads as "this function is
	// still oversized" long after someone fixed it, and quietly grants 150
	// lines of headroom to whatever gets written there next.
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

// repoRootDir locates the module root the same way file_sizes_test.go does,
// via the go tool rather than by walking up looking for go.mod.
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
