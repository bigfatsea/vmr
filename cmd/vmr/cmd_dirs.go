// Ver 2026-07-26, by Sonnet 5
package main

import (
	"flag"
	"fmt"

	"vmr/internal/config"
)

// cmdDirs prints the effective runtime directory for "log" (config
// log_dir) or "cache" (config image_cache_dir) — the resolved value after
// defaults, exactly what a `vmr start` with the same config would use.
// vmr.sh queries this instead of keeping its own copy of the resolution
// logic, so its server-log placement can never disagree with where the
// running process actually writes.
func cmdDirs(args []string) error {
	fs := flag.NewFlagSet("dirs", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: vmr dirs [-c config.yaml] {log|cache}")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	switch fs.Arg(0) {
	case "log":
		fmt.Println(cfg.LogDir)
	case "cache":
		fmt.Println(cfg.ImageCacheDir)
	default:
		return fmt.Errorf("usage: vmr dirs [-c config.yaml] {log|cache}")
	}
	return nil
}
