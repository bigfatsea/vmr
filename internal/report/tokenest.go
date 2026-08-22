// Ver 2026-08-14, by Opus 5

// Package report provides degraded token estimation helpers for offline analytics.
package report

import (
	"vmr/internal/audit"
	"vmr/internal/reqdetail"
	"vmr/internal/tokenutil"
)

// estimateDegradedTokens computes input/output token estimates for records where exact
// usage was not sniffed, falling back to Facts.EstimatedTokens or tokenutil.Estimate.
func estimateDegradedTokens(arec *audit.Record) (inEst, outEst int64) {
	if arec.Facts != nil {
		inEst = arec.Facts.EstimatedTokens
	} else {
		inEst = tokenutil.Estimate(reqdetail.BodyRaw(arec.Client.Request.Body))
	}
	if arec.Client.Response != nil {
		outEst = tokenutil.Estimate(reqdetail.BodyRaw(arec.Client.Response.Body))
	}
	return inEst, outEst
}
