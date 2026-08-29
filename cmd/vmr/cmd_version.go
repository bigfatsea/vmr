// Ver 2026-07-28 14:10, by Opus 5
package main

import (
	"flag"
	"fmt"
	"os"

	"vmr/internal/buildinfo"
)

// cmdVersion prints the build identity of *this* binary. The same value is
// reported by a running instance under /status's instance block, so
// `vmr version` and `vmr.sh ps` can be compared directly to answer "is that
// process running the binary I just built?" — which vmr.sh's warn_if_stale
// can only ever guess at from file mtimes.
func cmdVersion(args []string) error {
	// A flag set only to keep `-h` consistent with every other subcommand
	// (flag.ExitOnError exits 0 on -h); `vmr version` itself takes nothing.
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: vmr version") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: vmr version")
	}
	fmt.Println(buildinfo.Read().String())
	return nil
}
