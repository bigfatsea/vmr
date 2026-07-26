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
var forbiddenImports = map[string][]string{
	"vmr/internal/report": {
		"vmr/internal/router",
		"vmr/internal/server",
		"vmr/internal/config",
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
