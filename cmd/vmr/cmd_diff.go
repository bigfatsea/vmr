// Ver 2026-09-08, by coding assistant
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"vmr/internal/auditdiff"
	"vmr/internal/ctxgraph"
	"vmr/internal/replay"
)

// cmdDiff compares two audit records structurally: Header, System prompt,
// Tools declared, Messages (longest common prefix and divergence), and a
// Verdict — see internal/auditdiff for the comparison algorithm and
// internal/replay.LoadRecord for how a "basename:line" coordinate resolves
// to a record (the same resolver and search rules `vmr replay -req` uses).
func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "path to config file (optional, for log_dir search)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: vmr diff [-c config.yaml] <coordA> <coordB>")
	}
	return runDiff(fs.Arg(0), fs.Arg(1), *cfgPath, os.Stdout)
}

// runDiff locates both records, computes their structural divergence, and
// writes the formatted report to out.
func runDiff(coordA, coordB, cfgPath string, out io.Writer) error {
	recA, pathA, lineA, err := replay.LoadRecord(coordA, "", cfgPath)
	if err != nil {
		return fmt.Errorf("coordA: %w", err)
	}
	recB, pathB, lineB, err := replay.LoadRecord(coordB, "", cfgPath)
	if err != nil {
		return fmt.Errorf("coordB: %w", err)
	}

	mA, okA := ctxgraph.BuildManifest(recA, pathA, lineA)
	if !okA {
		return fmt.Errorf("%s:%d: request body is not a valid chat object", pathA, lineA)
	}
	mB, okB := ctxgraph.BuildManifest(recB, pathB, lineB)
	if !okB {
		return fmt.Errorf("%s:%d: request body is not a valid chat object", pathB, lineB)
	}

	report := auditdiff.Compute(coordA, coordB, recA, recB, mA, mB)
	auditdiff.Render(out, report)
	return nil
}
