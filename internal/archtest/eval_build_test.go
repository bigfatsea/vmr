// Ver 2026-08-21, by Sonnet 5

package archtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestArchitecture_EvalToolsCompile guards _eval/ — a standalone-tool
// directory Go's own toolchain convention excludes from every "./..."
// pattern (a leading underscore, same as a leading dot), so it is invisible
// to `go build ./...`, `go test ./...`, and `go vet ./...` alike. That
// exclusion is exactly what P11 relied on to argue ctxgraph.Scan and
// story.Build are live code (they're _eval/calibrate_p1b.go's real
// production calls, not dead) — but the same blind spot means nothing
// verifies _eval/calibrate_p1b.go itself still compiles. Without this test,
// a signature change to either function breaks _eval/calibrate_p1b.go
// silently, and the exact argument P11 used to keep them alive quietly
// stops being true.
func TestArchitecture_EvalToolsCompile(t *testing.T) {
	root := repoRootDir(t)
	src := filepath.Join(root, "_eval", "calibrate_p1b.go")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("_eval/calibrate_p1b.go not present: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", os.DevNull, src)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("_eval/calibrate_p1b.go failed to compile: %v\n%s", err, out)
	}
}
