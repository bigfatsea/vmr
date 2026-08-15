// Ver 2026-08-14, by Opus 5

// Package report provides degraded token estimation helpers for offline analytics.
package report

import (
	"vmr/internal/audit"
	"vmr/internal/core"
)

// estimateDegradedTokens computes input/output token estimates for records where exact
// usage was not sniffed, falling back to Facts.EstimatedTokens or EstimateTextTokens.
func estimateDegradedTokens(arec *audit.Record) (inEst, outEst int64) {
	if arec.Facts != nil {
		inEst = arec.Facts.EstimatedTokens
	} else {
		inEst = core.EstimateTextTokens(bodyRaw(arec.Client.Request.Body))
	}
	if arec.Client.Response != nil {
		outEst = core.EstimateTextTokens(bodyRaw(arec.Client.Response.Body))
	}
	return inEst, outEst
}
