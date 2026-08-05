// Ver 2026-08-05, by Sonnet 5
package story

import (
	"os"
	"testing"
	"time"

	"vmr/internal/fmtutil"
)

// TestMain pins fmtutil.DisplayZone to UTC for this package's whole test
// binary. Nearly every fixture in this package builds timestamps with
// time.UTC and golden.md/golden_zh.md assert exact "HH:MM:SS" display
// strings — those assertions are about rendering logic, not about the host
// machine's real timezone, so they must not depend on whatever timezone
// happens to run `go test`. Same rationale/pattern as
// internal/report/testmain_test.go.
func TestMain(m *testing.M) {
	fmtutil.DisplayZone = time.UTC
	os.Exit(m.Run())
}
