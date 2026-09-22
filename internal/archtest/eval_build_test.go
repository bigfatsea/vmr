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
// blind spot means nothing else verifies _eval/calibrate_p1b.go itself
// still compiles against the internal packages it calls into
// (ctxgraph.ScanCached, journey.BuildChain, journey.ComputeLLMFindings).
// Without this test, a signature change to any of them breaks
// _eval/calibrate_p1b.go silently.
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
