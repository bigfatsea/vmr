// Ver 2026-08-14, by Sonnet 5
package report

import (
	"testing"

	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/tokenutil"
)

// TestEstimateDegradedTokens_FactsPresent pins the primary path: when Facts
// was computed (the live-traffic case), the request-side estimate is read
// off it verbatim, never re-derived from the request body.
func TestEstimateDegradedTokens_FactsPresent(t *testing.T) {
	arec := &audit.Record{
		Facts: &core.RequestFacts{EstimatedTokens: 4242},
		Client: audit.Exchange{
			Request:  audit.Message{Body: `{"model":"agent","messages":[{"role":"user","content":"hi"}]}`},
			Response: &audit.Message{Body: "some response text"},
		},
	}
	inEst, outEst := estimateDegradedTokens(arec)
	if inEst != 4242 {
		t.Errorf("inEst = %d, want Facts.EstimatedTokens (4242) verbatim", inEst)
	}
	wantOut := tokenutil.Estimate([]byte("some response text"))
	if outEst != wantOut {
		t.Errorf("outEst = %d, want %d", outEst, wantOut)
	}
}

// TestEstimateDegradedTokens_FactsNil pins the fallback this test file exists
// to cover: internal/replay's writeReplayRecord never populates Facts, so a
// replay record reaching this function (endpoint non-empty, usage
// unparseable) must fall back to tokenutil.Estimate over the recorded
// client request body — the exact basis internal/replay's chargeReplay
// charges the router's own quota with — rather than silently contributing 0.
func TestEstimateDegradedTokens_FactsNil(t *testing.T) {
	reqBody := `{"model":"agent","messages":[{"role":"user","content":"a fairly long question about something"}]}`
	arec := &audit.Record{
		Facts: nil,
		Client: audit.Exchange{
			Request:  audit.Message{Body: reqBody},
			Response: &audit.Message{Body: "some response text"},
		},
	}
	inEst, _ := estimateDegradedTokens(arec)
	wantIn := tokenutil.Estimate([]byte(reqBody))
	if inEst != wantIn {
		t.Errorf("inEst = %d, want %d (tokenutil.Estimate over the raw request body, matching internal/replay's chargeReplay)", inEst, wantIn)
	}
	if inEst == 0 {
		t.Error("inEst = 0: the pre-fix bug this test guards against — a nil Facts silently dropping the request-side estimate")
	}
}
