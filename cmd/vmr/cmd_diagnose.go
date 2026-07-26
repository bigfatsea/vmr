// Ver 2026-07-26, by Sonnet 5
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"vmr/internal/diagnose"
)

// cmdDiagnose validates config and, unless -no-test-routing is set, dials
// every configured provider with a real minimal request — see
// internal/diagnose for what vmr check (a static preview) doesn't cover.
func cmdDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "path to config file")
	noTestRouting := fs.Bool("no-test-routing", false, "skip phase 3 (real connectivity test); only validate config and environment")
	testTimeout := fs.Duration("test-timeout", 15*time.Second, "per-endpoint timeout for the connectivity test")
	jsonOut := fs.Bool("json", false, "print results as a JSON array instead of the human-readable listing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep, err := diagnose.Run(context.Background(), diagnose.Options{
		ConfigPath:  *cfgPath,
		TestRouting: !*noTestRouting,
		TestTimeout: *testTimeout,
		// Progress always goes to stderr, in both output modes: it's pure
		// "this is still running" narration, never part of the reported
		// data, so it can't corrupt -json's stdout even when both streams
		// share a terminal. Redirect stderr away (2>/dev/null) to silence it.
		Progress: os.Stderr,
	})
	if rep == nil {
		return err // config load itself failed; nothing to print
	}
	if *jsonOut {
		data, err := json.MarshalIndent(rep.Results, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(diagnose.FormatTable(rep))
	}
	if n := rep.FailCount(); n > 0 {
		return fmt.Errorf("%d failing check(s)", n)
	}
	return nil
}
