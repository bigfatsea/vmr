// Ver 2026-07-26, by Sonnet 5

// Package archtest holds executable checks for architectural invariants
// this project has stated but never enforced with code: a documented
// tripwire with no automated check is a tripwire nobody actually sees trip.
// Every test here exists because someone (a design doc, a review) already
// wrote down a rule; this package just makes violating it a test failure
// instead of a fact that quietly stops being true.
package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// forbiddenImports pins the rule that the analysis/report layer only
// depends on the audit log schema, never on the routing runtime it
// analyzes. internal/report is already structurally an island (only
// vmr/internal/{audit,core} today) — this test exists so it stays that way
// as the package grows, not because it's currently violated.
//
// internal/ctxgraph (the content-addressed manifest/lineage layer behind
// `vmr story` — see docs/Agent任务叙事报告_设计与价值论证_2026-07-28_opus-5.md
// §7.3) is held to the same island rule, plus one more: it must not depend
// on internal/report either, even though internal/report's _test.go files
// import ctxgraph the other way around (a parity check, not a production
// dependency — go list -deps doesn't see test-only imports, so that stays
// invisible here). ctxgraph existing to eventually replace report's own
// grouping logic (Phase 3) would be undermined if it quietly depended back
// on the thing it's meant to decouple.
var forbiddenImports = map[string][]string{
	"vmr/internal/report": {
		"vmr/internal/router",
		"vmr/internal/server",
		"vmr/internal/config",
	},
	"vmr/internal/ctxgraph": {
		"vmr/internal/router",
		"vmr/internal/server",
		"vmr/internal/config",
		"vmr/internal/report",
	},
	// internal/story (the `vmr story` narrative renderer) sits on top of
	// ctxgraph, never on report — the same reasoning as ctxgraph's own
	// rule: report's session/task grouping is an independent, still-
	// authoritative implementation until Phase 3 migrates it onto
	// ctxgraph (design doc §7.2), and story must not quietly reach past
	// that boundary just because report happens to have similar-looking
	// helpers.
	"vmr/internal/story": {
		"vmr/internal/router",
		"vmr/internal/server",
		"vmr/internal/report",
	},
}

// TestArchitecture_ImportBoundaries shells out to `go list -deps` rather
// than adding a Go dependency-graph library: vmr's own stated policy is
// zero non-essential dependencies (see design doc §4.2's dependency list),
// and every environment that can run `go test` already has the `go` binary
// on PATH by construction.
func TestArchitecture_ImportBoundaries(t *testing.T) {
	for pkg, forbidden := range forbiddenImports {
		out, err := exec.Command("go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		deps := make(map[string]bool)
		for _, d := range strings.Fields(string(out)) {
			deps[d] = true
		}
		for _, f := range forbidden {
			if deps[f] {
				t.Errorf("%s must not depend on %s: "+
					"the analysis layer only depends on the audit schema, never on the routing runtime", pkg, f)
			}
		}
	}
}
