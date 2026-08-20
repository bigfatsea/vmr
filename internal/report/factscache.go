// Ver 2026-08-20 00:00, by Sonnet 5

// The other half of the shared parse cache (see internal/ctxgraph's
// cache.go): report's own per-record aggregation facts, cached alongside
// ctxgraph's Manifests so a file-hash cache hit lets scanFiles skip
// reopening and re-decoding that file too, not just AnalyzeSessionsCached's
// manifest pass. Before this, only the manifest half was cached — the
// reason report's own hot-cache runs used to be barely faster than cold
// ones (see docs/future-strategy/story_report_architecture_opus-5.md
// §7.10). Marshaled into ctxgraph.CachedFile.Facts, a field that package
// treats as opaque (round-trips it, never interprets it) — see that
// field's own doc comment for why the type lives here, not there.
package report

import (
	"encoding/json"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/reqdetail"
)

// attemptFacts is the projection of one audit.Attempt that
// EndpointRow.IngestAttempt needs — everything else about an attempt
// (headers, bodies, norm details beyond the marker list) belongs to
// per-request detail rendering (internal/reqdetail), not to this
// aggregate-only cache.
type attemptFacts struct {
	Endpoint    string   `json:"endpoint"`
	HasResponse bool     `json:"has_response,omitempty"`
	Status      int      `json:"status,omitempty"`
	Error       string   `json:"error,omitempty"`
	ErrorClass  string   `json:"error_class,omitempty"`
	Norm        []string `json:"norm,omitempty"`
	DurMS       int64    `json:"dur_ms,omitempty"`
}

// recordFacts is one audit.Record's cache-worthy extraction result: every
// field buildRec2 used to compute directly from *audit.Record (not from
// ReqInfo — those fields need no caching, they're already cheap in-memory
// lookups once AnalyzeSessionsCached has run). Field names ending in "Raw"
// hold the arec-derived half of a value buildRec2 only uses when ReqInfo
// didn't already supply a better one (see that function's doc comment) —
// kept distinct from the final rec2 field so buildRec2's ReqInfo-merge
// logic has exactly one implementation regardless of whether rf came from
// a fresh decode or a cache hit.
type recordFacts struct {
	Line     int       `json:"line"`
	TS       time.Time `json:"ts"`
	Model    string    `json:"model,omitempty"`
	Protocol string    `json:"protocol,omitempty"`
	Outcome  string    `json:"outcome,omitempty"`
	Stream   bool      `json:"stream,omitempty"`
	DurMS    int64     `json:"dur_ms,omitempty"`
	TTFTMS   int64     `json:"ttft_ms,omitempty"`

	BytesIn       int64  `json:"bytes_in,omitempty"`
	BytesOut      int64  `json:"bytes_out,omitempty"`
	ToolDeclCount int    `json:"tool_decl_count,omitempty"`
	ToolDeclBytes int64  `json:"tool_decl_bytes,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	ErrorClass    string `json:"error_class,omitempty"`
	ClientKey     string `json:"client_key,omitempty"`

	TruncatedRaw        bool  `json:"truncated_raw,omitempty"`
	ImagesRaw           int   `json:"images_raw,omitempty"`
	ImagesCompressedRaw int   `json:"images_compressed_raw,omitempty"`
	FallbacksRaw        int   `json:"fallbacks_raw,omitempty"`
	EstInFresh          int64 `json:"est_in_fresh,omitempty"`
	EstOut              int64 `json:"est_out,omitempty"`

	Attempts []attemptFacts `json:"attempts,omitempty"`
}

// fileFacts is one input file's full recordFacts payload, the shape
// marshaled into ctxgraph.CachedFile.Facts.
type fileFacts struct {
	ParseErrors int           `json:"parse_errors,omitempty"`
	Records     []recordFacts `json:"records,omitempty"`
}

// extractRecordFacts computes recordFacts' arec-derived fields — see that
// type's doc comment. line is the record's 1-based line within its file
// (the scan loop's own counter, not derived from arec).
func extractRecordFacts(arec *audit.Record, line int) recordFacts {
	rf := recordFacts{
		Line: line, TS: arec.TS, Model: arec.Model, Protocol: arec.Protocol,
		Outcome: arec.Outcome, Stream: arec.Stream, DurMS: arec.DurMS, TTFTMS: arec.TTFTMS,
		ClientKey: arec.ClientKeyTag,
	}
	rf.BytesIn = reqdetail.BodyBytes(arec.Client.Request.Body)
	if arec.Client.Response != nil {
		rf.BytesOut = reqdetail.BodyBytes(arec.Client.Response.Body)
	}
	rf.ToolDeclCount, rf.ToolDeclBytes = toolDeclInfo(arec.Client.Request.Body)
	rf.Endpoint, rf.ErrorClass = endpointInfo(arec)
	rf.ImagesRaw, rf.ImagesCompressedRaw = reqdetail.CountImages(arec.Images)
	if len(arec.Attempts) > 1 {
		rf.FallbacksRaw = 1
	}
	if arec.Outcome == "ok" {
		for _, a := range arec.Attempts {
			if reqdetail.AttemptErrorClass(a) == "truncated" {
				rf.TruncatedRaw = true
				break
			}
		}
	}
	// Computed unconditionally (buildRec2 decides whether usageOK makes it
	// irrelevant) — cheap relative to what caching it buys: a cache hit
	// must never need arec again for any reason, or the whole point of
	// caching this file is lost.
	rf.EstInFresh, rf.EstOut = estimateDegradedTokens(arec)
	rf.Attempts = attemptFactsFrom(arec.Attempts)
	return rf
}

func attemptFactsFrom(attempts []audit.Attempt) []attemptFacts {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]attemptFacts, len(attempts))
	for i, a := range attempts {
		af := attemptFacts{
			Endpoint: a.Endpoint, Error: a.Error,
			ErrorClass: reqdetail.AttemptErrorClass(a), Norm: a.Norm, DurMS: a.DurMS,
		}
		if a.Response != nil {
			af.HasResponse = true
			af.Status = a.Response.Status
		}
		out[i] = af
	}
	return out
}

// loadCachedFacts unmarshals key's cached Facts payload from cache, when
// present. cache may be nil (no prior cache at all). A missing entry,
// absent/empty Facts, or a stale SchemaVersion (shouldn't happen by the
// time this runs — ctxgraph.ScanCached already invalidated the whole
// entry on a version mismatch — checked again here anyway, since a wrong
// answer here would silently corrupt aggregated numbers, not just cost a
// slower rerun) all report ok=false: the caller falls back to a fresh
// decode.
func loadCachedFacts(cache *ctxgraph.FileCache, key string) (ff fileFacts, ok bool) {
	if cache == nil {
		return fileFacts{}, false
	}
	cf, present := cache.Files[key]
	if !present || len(cf.Facts) == 0 || cf.SchemaVersion != ctxgraph.CacheSchemaVersion {
		return fileFacts{}, false
	}
	if err := json.Unmarshal(cf.Facts, &ff); err != nil {
		return fileFacts{}, false
	}
	return ff, true
}

// storeCachedFacts marshals ff into cache.Files[key].Facts, preserving
// that entry's other fields (Hash/Manifests/NoBody — already correct by
// the time scanFiles runs; see ctxgraph.ScanCached's postcondition) and
// stamping the current schema version. A marshal failure is silently
// dropped (this run's own in-memory results are unaffected either way —
// only the next run's cache hit rate would suffer, not correctness).
func storeCachedFacts(cache *ctxgraph.FileCache, key string, ff fileFacts) {
	if cache == nil {
		return
	}
	data, err := json.Marshal(ff)
	if err != nil {
		return
	}
	cf := cache.Files[key]
	cf.SchemaVersion = ctxgraph.CacheSchemaVersion
	cf.Facts = data
	cache.Files[key] = cf
}
