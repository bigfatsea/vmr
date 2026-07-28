// Ver 2026-07-26, by Sonnet 5
package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fileLineLimits pins a finding from an architecture review: router.go had
// grown to 948 lines against the design doc's own stated ~550-line budget
// ("若主流程显著变大，说明抽象错了"), and nobody noticed because the budget
// only ever lived in a comment. A file split
// (snapshot.go/limiter.go/transport.go/logfmt.go) brought it back to 538.
// The limit here is set with headroom above that real post-split number,
// not at the design doc's original round figure — the point is catching
// regrowth back toward 948, not fighting over every line.
var fileLineLimits = map[string]int{
	"internal/router/router.go": 700,
	// internal/report's two largest files, budgeted after the same kind of
	// split: the renderer used to be one 1053-line aggregate_render.go and
	// is now render_doc.go (running order + shared table primitive) plus one
	// section_*.go per numbered section. The budget is what keeps a new
	// report section arriving as a new file rather than as another 90 lines
	// appended to whichever file is already the biggest.
	"internal/report/aggregate.go":  1000,
	"internal/report/render_doc.go": 400,
}

// TestArchitecture_CoreFileSizes counts non-blank lines the same way `wc -l`
// does (this test's own budget above was set from that same count) so a
// contributor can reproduce a failure locally without reading this file's
// counting logic first.
func TestArchitecture_CoreFileSizes(t *testing.T) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	repoRoot := filepath.Dir(strings.TrimSpace(string(out)))

	for rel, limit := range fileLineLimits {
		path := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		n := bytes.Count(data, []byte("\n"))
		if n > limit {
			t.Errorf("%s is %d lines, over its %d-line budget: split it "+
				"into another file under internal/router, don't just raise "+
				"this number", rel, n, limit)
		}
	}
}
