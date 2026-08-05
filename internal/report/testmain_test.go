// Ver 2026-08-05, by Sonnet 5
package report

import (
	"os"
	"testing"
	"time"

	"vmr/internal/fmtutil"
)

// TestMain pins fmtutil.DisplayZone to UTC for this package's whole test
// binary. Nearly every fixture in this package builds timestamps with
// time.UTC (e.g. aggregate_test.go's `t0 := time.Date(2026, 7, 24, 0, 0, 0,
// 0, time.UTC)`) and asserts exact date/hour bucket keys — those assertions
// are about bucketing logic, not about the host machine's real timezone, so
// they must not depend on whatever timezone happens to run `go test`.
// Individual tests that specifically want to prove a DisplayZone conversion
// took place (e.g. TestPricingRateMatchesConvertsToDisplayZone) still
// override it locally with their own defer-restore.
func TestMain(m *testing.M) {
	fmtutil.DisplayZone = time.UTC
	os.Exit(m.Run())
}
