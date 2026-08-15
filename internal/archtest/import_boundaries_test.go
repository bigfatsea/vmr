// Ver 2026-07-29 23:55, by Sonnet 5

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
// depends on the audit log schema and internal/ctxgraph, never on the
// routing runtime it analyzes — this test exists so it stays that way as
// the package grows, not because it's currently violated.
//
// internal/ctxgraph (the content-addressed manifest/lineage layer behind
// `vmr story`, and now behind internal/report's own session grouping too;
// see docs/VirtualModelRouter_Design_v4_Analytics.md's internal/ctxgraph
// content-addressing layer section) is
// held to the same island rule, plus one more: it must not depend on
// internal/report — report now legitimately depends on ctxgraph in
// production code (one-directional), and ctxgraph depending back on report
// would be a real import cycle risk, not just a layering preference.
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
		// taskseg started depending on ctxgraph in the architecture review's
		// B3 batch (Hash/Manifest types for its shared session/task-boundary
		// primitives) — the same one-directional-dependency risk as report's
		// entry just above: ctxgraph depending back on taskseg would be a
		// real import cycle, not just a layering preference.
		"vmr/internal/taskseg",
	},
	// internal/story (the `vmr story` narrative renderer) sits on top of
	// ctxgraph, never on report — the same reasoning as ctxgraph's own
	// rule. report's session/task grouping already consumes ctxgraph
	// directly (session.go's Lineage/Classify use) and shares its
	// agent-dialect Profile with story via internal/taskseg, but the two
	// consumers remain independent of each other: story must not reach past
	// this boundary just because report happens to have similar-looking
	// helpers, and vice versa.
	"vmr/internal/story": {
		"vmr/internal/router",
		"vmr/internal/server",
		"vmr/internal/report",
	},
	// adapter builds requests and classifies errors for both the live
	// routing path (router) and the offline tools (diagnose/replay) that
	// reuse it precisely so they exercise the same code real traffic does
	// (see those packages' doc comments) — that only holds if adapter
	// itself never depends back on router or server, or "reuses the same
	// construction code" would stop being a one-directional fact.
	"vmr/internal/adapter": {
		"vmr/internal/router",
		"vmr/internal/server",
	},
	// router is the failover core server.go's HTTP layer sits on top of;
	// router depending back on server would be a real import-cycle risk,
	// not just a layering preference (CLAUDE.md's module map already states
	// this direction — this test is what makes it stay true).
	"vmr/internal/router": {
		"vmr/internal/server",
	},
	// respnorm (the response-normalization state machine — architecture
	// review's Part 8 batch B7 extracted it from internal/router precisely
	// so it could be fuzzed at the pure io.Reader level, independent of
	// Router/Snapshot) sits below router in the routing half, the same
	// "shared leaf, consumer never imports back" shape as taskseg below
	// report/story: router.go/quota.go depend on it (Wrap/NormalizerStream),
	// and it depending back on router or server would be a real import
	// cycle, not just a layering preference.
	"vmr/internal/respnorm": {
		"vmr/internal/router",
		"vmr/internal/server",
	},
	// taskseg (agent-dialect Profile plus, since the architecture review's B3
	// batch, the session/task-segmentation primitives built on it —
	// real-instruction indexing, new-task detection, task titling) is the
	// shared leaf both report's session.go and story's journey.go depend on
	// — B2 merged what used to be story's own private internal/story/profile
	// package with a byte-identical copy report carried in session.go, B3
	// converged the two packages' independent session/task-boundary
	// algorithms the same way. Neither consumer may depend back on it, or
	// "shared leaf" stops being true; same reasoning as ctxgraph's and
	// chatmsg's own zero-dependency-on-consumers rule. CLAUDE.md states
	// taskseg depends only on {chatmsg, fmtutil, ctxgraph} — the entries
	// below make that an enforced fact rather than only a stated one, same
	// rationale as every other row in this table. vmr/internal/audit is
	// deliberately NOT listed here even though it once was (B2's own
	// closing note): B3 added ctxgraph as a legitimate direct dependency
	// (for its Hash/Manifest types), and ctxgraph itself depends on audit —
	// audit is now an expected transitive dependency, not a violation to
	// guard against.
	"vmr/internal/taskseg": {
		"vmr/internal/router",
		"vmr/internal/server",
		"vmr/internal/config",
		"vmr/internal/report",
		"vmr/internal/story",
		"vmr/internal/adapter",
		"vmr/internal/pricing",
		"vmr/internal/quota",
	},
}

// zeroInternalDepPackages must not depend on any other vmr/internal/*
// package at all, per CLAUDE.md's module map: core is "shared types, no
// internal deps" and fmtutil is "same layer as core". jsonscan joined this
// list when the architecture review's B1 batch extracted it from
// internal/adapter's classify.go/fingerprint.go — a pure JSON byte-range
// scanning engine with no reason to depend on anything but the standard
// library. Checked separately from forbiddenImports above because "must
// have zero deps" isn't expressible as a finite forbidden list without
// silently going stale every time a new internal package is added elsewhere
// in the tree.
var zeroInternalDepPackages = []string{
	"vmr/internal/core",
	"vmr/internal/fmtutil",
	"vmr/internal/jsonscan",
}

// TestArchitecture_ZeroInternalDepPackages guards the two packages every
// other package in this project is free to import without a boundary
// concern — that promise only holds if they never grow an internal
// dependency of their own.
func TestArchitecture_ZeroInternalDepPackages(t *testing.T) {
	for _, pkg := range zeroInternalDepPackages {
		out, err := exec.Command("go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		for _, d := range strings.Fields(string(out)) {
			if d != pkg && strings.HasPrefix(d, "vmr/internal/") {
				t.Errorf("%s must have zero vmr/internal dependencies, but depends on %s", pkg, d)
			}
		}
	}
}

// TestArchitecture_ImportBoundaries shells out to `go list -deps` rather
// than adding a Go dependency-graph library: vmr's own stated policy is
// zero non-essential dependencies (see the design doc's dependency-policy section),
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
