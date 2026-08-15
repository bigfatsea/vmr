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

// Build reads audit JSONL files and aggregates them into a Report2. It calls
// AnalyzeSessions for grouping (one read), then does its own pass (second
// read) joining each record to its ReqInfo via sess.Lookup.
//
// onRecord (optional, nil = skip) is called once per successfully-parsed
// record, where this pass already holds both the raw *audit.Record and its
// *ReqInfo. Detail rendering depends only on that pair, never on anything
// accumulated across records, so it needs no pass of its own: cmd/vmr hands
// this a DetailWriter.Submit bound to a live worker pool, keeping `vmr
// report`'s reads of the (possibly gigabyte-scale, zstd-compressed) audit
// source at two rather than three. Build's own success is independent of
// onRecord's outcome by design — a broken detail-output directory must not
// cost the caller an otherwise-good vmr-report.json/md.
//
// An AnalyzeSessions failure is fatal to the whole command: the second pass
// cannot start without it, since every record's usage/tokens comes from its
// ReqInfo. In practice it only fails on a per-file I/O error (malformed JSON
// lines are skipped, not fatal), and the second pass reads the same files the
// same way moments later, so the error surface is identical either way. The
// likeliest real trigger is a race with internal/audit/housekeep.go's rotation
// sweep — a long-running `vmr start` compressing a log out from under a
// concurrent `vmr report` — so the message below names that explicitly.
//
// Build is buildInternal (aggregate.go) with no file-hash cache, always
// interpreting agent-dialect conventions through taskseg.OpenClawAware. Kept
// as the stable, cache-free entry point every existing caller/test uses;
// BuildCached below is the one cmd_report.go calls and the one that threads a
// caller-chosen Profile.
func Build(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo)) (*Report2, *SessionAnalysis, error) {
	rep, sess, _, err := buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, taskseg.OpenClawAware, nil, nil)
	return rep, sess, err
}

// BuildCached is Build, plus a file-hash-keyed cache (see
// ctxgraph.FileCache/ScanCached) for AnalyzeSessionsCached's ctxgraph.Scan
// pass — the report package's OWN separate per-request parse (buildInternal's
// "single pass over files, joined to ReqInfo") still reparses every file
// every call; only the ctxgraph.Manifest-based session-grouping pass is
// cached this round (see docs/VirtualModelRouter_Design_v4_Analytics.md's
// vmr-requests.json section for why that's the deliberately scoped-down
// near-term version, not the deeper "report consumes ctxgraph.Manifest
// directly" unification). prior may be nil (identical to Build).
//
// prof is the taskseg.Profile session.go's collect() uses to recognize real
// user instructions, a deliberate no-reply skip, and a framework-specific
// chat_id — resolved once at cmd/vmr's composition root (resolveTaskProfile,
// the same entry point `vmr story` uses) rather than decided independently
// here.
//
// quotas (may be nil) is §2.5's provider quota reference — Build always
// passes nil (it stays quota-blind on purpose, so its 26 existing call
// sites never need to change); only BuildCached, cmd_report.go's actual
// production entry point, threads it through.
func BuildCached(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo), prof taskseg.Profile, prior *ctxgraph.FileCache, quotas map[string]ProviderQuotaRef) (*Report2, *SessionAnalysis, *ctxgraph.FileCache, error) {
	return buildInternal(paths, now, progress, pricingInfo, pricingSrc, onRecord, prof, prior, quotas)
}
