// Ver 2026-08-05, by Sonnet 5

package report

import (
	"io"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// Build reads audit JSONL files and aggregates them into a Report2. It calls
// AnalyzeSessions for grouping (one read), then does its own pass
// (second read) joining each record to its ReqInfo via sess.Lookup.
//
// onRecord (optional, nil = skip) is called once per successfully-parsed
// record, right where this pass already has both the raw *audit.Record and
// its *ReqInfo in hand — the same pair a third, independent read used to
// re-derive for detail export (WriteDetails, before it grew this hook).
// Detail rendering depends only on a record's own (audit.Record, *ReqInfo)
// pair, never on anything accumulated across records, so there's no reason
// it needs its own pass at all: cmd/vmr now hands this pass a
// DetailWriter.Submit bound to a live worker pool instead, cutting `vmr
// report`'s total reads of the (possibly gigabyte-scale, zstd-compressed)
// audit source from three down to two. Build's own success/failure is
// entirely independent of onRecord's outcome — it doesn't inspect or
// propagate whatever onRecord does with what it's handed (by design: a
// broken detail-output directory must not cost the caller an otherwise-good
// vmr-report.json/md, exactly as before when detail export was a separate,
// independently-failing step run after Build returned).
//
// Unlike the old (now removed) `vmr report` aggregator — which ran its
// deterministic aggregation first and only attempted session analysis
// afterward, so a session-analysis failure degraded to a warning instead of
// losing the whole report — this two-pass design needs AnalyzeSessions to
// succeed before the second pass can even start (every record's usage/
// tokens now comes from its ReqInfo, not a second independent extraction).
// A failure here is fatal to the whole command. In practice the only way
// AnalyzeSessions fails is a per-file I/O error (bad open, or a read error
// mid-scan — malformed JSON lines are just skipped, not fatal), and this
// pass reads the exact same files the same way a moment later, so the
// error surface is the same either way. The most likely real-world trigger
// is a race with `internal/audit/housekeep.go`'s rotation sweep — a
// long-running `vmr start` compressing/deleting a log file out from under
// a concurrently running `vmr report` — not a code bug, so the message
// below names that possibility explicitly.
//
// Build itself is buildInternal (aggregate.go) with no file-hash cache —
// every call parses every input file's ctxgraph.Scan pass from scratch,
// same as before this cache existed. Kept as the stable, cache-free entry
// point every existing caller/test already uses; BuildCached below is the
// one cmd_report.go actually calls.
func Build(paths []string, now time.Time, progress io.Writer, pricing *Pricing, onRecord func(*audit.Record, *ReqInfo)) (*Report2, *SessionAnalysis, error) {
	rep, sess, _, err := buildInternal(paths, now, progress, pricing, onRecord, nil)
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
func BuildCached(paths []string, now time.Time, progress io.Writer, pricing *Pricing, onRecord func(*audit.Record, *ReqInfo), prior *ctxgraph.FileCache) (*Report2, *SessionAnalysis, *ctxgraph.FileCache, error) {
	return buildInternal(paths, now, progress, pricing, onRecord, prior)
}
