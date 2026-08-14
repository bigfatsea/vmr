// Ver 2026-08-14, by Sonnet 5

package main

import "vmr/internal/taskseg"

// resolveTaskProfile is the one place `vmr report` and `vmr story` both
// resolve which taskseg.Profile to interpret agent-dialect conventions
// with — a composition-root function rather than each command hardcoding
// its own choice, so the day a second real profile (or a config-driven/
// Detect-based selection) is worth building, there's exactly one call site
// to change. Both commands used to independently default to OpenClawAware
// (`vmr story` hardcoded it, `vmr report` never had a choice at all — its
// session.go carried its own private, always-OpenClaw-shaped copy of the
// heuristics); this makes that shared default a single explicit fact.
func resolveTaskProfile() taskseg.Profile {
	return taskseg.OpenClawAware
}
