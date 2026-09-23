// Ver 2026-09-12 12:00, by dev

package report

import (
	"fmt"
	"io"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/pricing"
	"vmr/internal/taskseg"
)

// BuildOptions configures a report Build run. Fields correspond to the original
// build parameters; zero values provide defaults (e.g. Now defaults to
// time.Now(), Profile defaults to taskseg.OpenClawAware).
type BuildOptions struct {
	Paths             []string
	Now               time.Time
	Progress          io.Writer
	PricingInfo       *Pricing
	PricingSrc        *pricing.Resolver
	OnRecord          func(*audit.Record, *ReqInfo)
	Profile           taskseg.Profile
	PriorCache        *ctxgraph.FileCache
	Quotas            map[string][]ProviderQuotaRef
	ExcludeClientTags map[string]bool
}

// Build builds a Report using an optional file-hash cache for session scanning,
// an explicit task profile, optional live provider quota references, and an optional
// self-traffic exclusion set (nil = exclude nothing).
func Build(opts BuildOptions) (*Report, *SessionAnalysis, *ctxgraph.FileCache, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	prof := opts.Profile
	if prof == nil {
		prof = taskseg.OpenClawAware
	}

	sess, cache, err := AnalyzeSessionsCached(opts.Paths, opts.PriorCache, prof)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("session analysis failed (%w) — no report was written. "+
			"This step reads every input file a second time; the most common real-world cause "+
			"is one of them being rotated/compressed by the audit housekeeping sweep (a running "+
			"`vmr start` instance) while this scan was in progress. Rerun; if it persists, check "+
			"whether any input path still exists under its original name (housekeeping renames "+
			"rotated files to .zst) and that it isn't corrupt", err)
	}

	excludeSelfTrafficFromSessionAnalysis(sess, opts.ExcludeClientTags)

	rep := &Report{Meta: Meta{
		Format:                     Format,
		GeneratedAt:                now.Format(time.RFC3339),
		Inputs:                     opts.Paths,
		SlowThreshold:              SlowThresholdMS,
		SelfTrafficExclusionActive: len(opts.ExcludeClientTags) > 0,
		PercentileMethod:           "true per-bucket from raw dur_ms/ttft_ms/stream_ms; cross-day merges use pre-aggregated *_all/hours_of_day siblings",
	}}

	st := newAggStateWithProfile(rep, sess, opts.PricingSrc, opts.ExcludeClientTags, prof)
	if err := st.scanFiles(opts.Paths, opts.Progress, opts.OnRecord, cache); err != nil {
		return nil, nil, nil, err
	}
	st.finishBuckets(opts.PricingInfo, opts.Quotas, now, opts.Progress)
	st.sortBuckets()
	return rep, sess, cache, nil
}
