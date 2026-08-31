// Ver 2026-08-10, by Sonnet 5

// Package addr holds the loadtest fixture's listen addresses — the one
// value loadtest/runner and loadtest/gentargets both need to agree on.
// loadtest/config.yaml's `listen`/`base_url` fields still have to be kept
// in sync by hand (YAML can't import a Go constant); this package only
// removes two of the previously three manually-synced copies — see
// docs/KNOWN_ISSUES.md's §3 loadtest entry.
//
// Not part of the shipped vmr binary — `go build ./cmd/vmr` never touches
// this directory.
package addr

const (
	// VMR is where runner.go starts the vmr binary under test and where
	// gentargets' generated Vegeta targets point — must match
	// loadtest/config.yaml's `listen`.
	VMR = "127.0.0.1:8801"
	// Mock is where runner.go starts loadtest/mockupstream and where
	// loadtest/config.yaml's providers' `base_url` point.
	Mock = "127.0.0.1:9900"
)
