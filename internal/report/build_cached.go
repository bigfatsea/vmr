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
// No self-traffic exclusion (P6.4) — callers that need it use BuildCached directly.
func Build(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo)) (*Report2, *SessionAnalysis, error) {
	rep, sess, _, err := buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, taskseg.OpenClawAware, nil, nil, nil)
	return rep, sess, err
}

// BuildCached builds a Report2 using an optional file-hash cache for session scanning,
// an explicit task profile, optional live provider quota references, and an optional
// self-traffic exclusion set (P6.4, nil = exclude nothing — see aggState.excludeClientTags).
func BuildCached(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo), prof taskseg.Profile, prior *ctxgraph.FileCache, quotas map[string][]ProviderQuotaRef, excludeClientTags map[string]bool) (*Report2, *SessionAnalysis, *ctxgraph.FileCache, error) {
	return buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, prof, prior, quotas, excludeClientTags)
}
