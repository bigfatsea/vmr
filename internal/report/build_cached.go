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

// BuildCached builds a Report2 using an optional file-hash cache for session scanning,
// an explicit task profile, optional live provider quota references, and an optional
// self-traffic exclusion set (P6.4, nil = exclude nothing — see aggState.excludeClientTags).
func BuildCached(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo), prof taskseg.Profile, prior *ctxgraph.FileCache, quotas map[string][]ProviderQuotaRef, excludeClientTags map[string]bool) (*Report2, *SessionAnalysis, *ctxgraph.FileCache, error) {
	return buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, prof, prior, quotas, excludeClientTags)
}
