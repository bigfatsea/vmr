// Ver 2026-09-03, by Sonnet 5
package report

import (
	"testing"

	"vmr/internal/chatmsg"
)

func TestEndpointRow_TokOutPerSec_UsageOutOKBasis(t *testing.T) {
	var e EndpointRow
	// rec1: sniffed usage with 100 output tokens and 1000ms duration.
	rec1 := &rec2{
		outcome:    "ok",
		usageOutOK: true,
		durMS:      1000,
		usage:      chatmsg.Usage{Out: 100},
	}
	// rec2: degraded estimation with 50 estimated output tokens and 1000ms duration (no sniffed usage).
	rec2Est := &rec2{
		outcome:    "ok",
		usageOutOK: false,
		durMS:      1000,
		estOut:     50,
	}

	e.IngestRequest(rec1)
	e.IngestRequest(rec2Est)
	finishEndpoint(&e)

	// TokOutPerSec denominator must only accumulate duration from usageOutOK records (1000ms),
	// yielding 100 / (1000/1000) = 100, rather than the un-gated DurMSSum basis (2000ms -> 50).
	const want = 100.0
	if e.TokOutPerSec != want {
		t.Errorf("e.TokOutPerSec = %v, want %v (tokDurMS basis, not DurMSSum)", e.TokOutPerSec, want)
	}
}
