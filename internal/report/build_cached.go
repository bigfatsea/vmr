// Ver 2026-08-05, by Sonnet 5

package report

import (
	"io"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/pricing"
	"vmr/internal/taskseg"
)

// Build reads audit JSONL files and aggregates them into a Report2 using OpenClawAware profile.
// onRecord is an optional callback executed per valid record. Session analysis failure is fatal.
func Build(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo)) (*Report2, *SessionAnalysis, error) {
	rep, sess, _, err := buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, taskseg.OpenClawAware, nil, nil)
	return rep, sess, err
}

// BuildCached builds a Report2 using an optional file-hash cache for session scanning,
// an explicit task profile, and optional live provider quota references.
func BuildCached(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo), prof taskseg.Profile, prior *ctxgraph.FileCache, quotas map[string]ProviderQuotaRef) (*Report2, *SessionAnalysis, *ctxgraph.FileCache, error) {
	return buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, prof, prior, quotas)
}
