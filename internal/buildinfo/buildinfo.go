// Ver 2026-07-28 14:05, by Opus 5

// Package buildinfo answers "which build of vmr is this" without a build
// system. Go already stamps the VCS state of any binary built inside a
// repository (vcs.revision / vcs.time / vcs.modified, on by default since
// -buildvcs=auto), so the answer is readable at runtime from the binary
// itself — no -ldflags, no generated version.go, no Makefile to remember.
// That matters here specifically: this project builds with a bare
// `go build -o vmr ./cmd/vmr`, and vmr.sh already carries a whole
// warn_if_stale heuristic (source newer than binary) precisely because
// "am I running the build I think I am" was previously unanswerable.
//
// vcs.modified is the field with the most operational value: it means the
// binary was built from a dirty working tree, i.e. its exact source no
// longer exists anywhere. A running instance reporting "dirty" is a
// perfectly good reason to distrust a bug report about it.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Info is the build identity, all fields best-effort: a binary built
// outside a VCS checkout (a release tarball, `go run`) simply has empty
// VCS fields, which every caller must render as "unknown" rather than
// treat as an error.
type Info struct {
	Revision  string `json:"revision,omitempty"` // full git SHA
	Time      string `json:"time,omitempty"`     // commit time, RFC3339
	Modified  bool   `json:"modified,omitempty"` // built from a dirty working tree
	GoVersion string `json:"go_version,omitempty"`
}

// Read extracts the VCS stamp. Cheap enough to call per request (it reads
// an in-binary table), but callers on a hot path should still hoist it.
func Read() Info {
	info := Info{GoVersion: runtime.Version()}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.time":
			info.Time = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// Short is the one-token form for a status line or a table column:
// "fbc034c", "fbc034c-dirty", or "unknown". Deliberately not a semver —
// this project has no release process to produce one, and a fabricated
// version number that nothing increments is worse than a commit SHA that
// is always exactly true.
func (i Info) Short() string {
	if i.Revision == "" {
		return "unknown"
	}
	short := i.Revision
	if len(short) > 7 {
		short = short[:7]
	}
	if i.Modified {
		short += "-dirty"
	}
	return short
}

// String is the multi-field form for `vmr version`.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString("vmr " + i.Short())
	if i.Time != "" {
		b.WriteString("  committed " + i.Time)
	}
	if i.GoVersion != "" {
		b.WriteString("  built with " + i.GoVersion)
	}
	if i.Modified {
		b.WriteString("\n  (built from a modified working tree — its exact source is not in any commit)")
	}
	return b.String()
}
