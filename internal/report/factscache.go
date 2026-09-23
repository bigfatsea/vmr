// Ver 2026-09-23 04:15, by Claude Opus 5.5

// The other half of the shared parse cache (see internal/ctxgraph's
// cache.go): report's own per-record aggregation facts, cached alongside
// ctxgraph's Manifests so a file-hash cache hit lets scanFiles skip
// reopening and re-decoding that file too, not just AnalyzeSessionsCached's
// manifest pass. Marshaled into ctxgraph.CachedFile.Facts, a field that
// package treats as opaque (round-trips it, never interprets it) — see that
// field's own doc comment for why the type lives here, not there.
package report

import (
	"encoding/json"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/reqdetail"
	"vmr/internal/taskseg"
)

// FactsSchemaVersion gates report's own per-record aggregation payload (fileFacts)
// freshness. Unlike prior versions where Facts relied on ctxgraph.CacheSchemaVersion,
// report's Facts now carries its own version stamped in CachedFile.FactsVersion and
// fileFacts.Version. Bump this whenever the report fact extraction logic or
// recordFacts shape changes.
const FactsSchemaVersion = 2

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
	// Forwarded is the router's authoritative "I charged quota for this"
	// signal (audit.Attempt.Forwarded, set by router.forwardSuccess). On
	// new-format records this is the only signal IsForwarded needs; on
	// pre-v4 records (cache entries written before this field existed) the
	// legacy rule (Response present, Status<400, ErrorClass=="") is the
	// fallback — see IsForwarded's doc comment for why.
	Forwarded bool `json:"forwarded,omitempty"`
}

// IsForwarded mirrors audit.Attempt.IsForwarded's exact-vs-legacy rule:
// a true Forwarded is authoritative; a false one falls back to the
// pre-v4 fallback (a < 400 response with no error class). New-format
// softblock records (ErrorClass=="content" on a < 400 response) never
// satisfy the fallback, so they are correctly excluded. Do not re-derive
// this condition at each call site — the predicate exists exactly to
// keep that decision in one place.
func (a attemptFacts) IsForwarded() bool {
	if a.Forwarded {
		return true
	}
	return a.HasResponse && a.Status < 400 && a.ErrorClass == ""
}

// recordFacts is one audit.Record's canonical extraction result: the unified
// single source of truth for both session grouping and macro aggregation.
// Cached per file content hash so warm runs decode zero audit lines.
type recordFacts struct {
	Line     int       `json:"line"`
	TS       time.Time `json:"ts"`
	Model    string    `json:"model,omitempty"`
	Protocol string    `json:"protocol,omitempty"`
	Outcome  string    `json:"outcome,omitempty"`
	Stream   bool      `json:"stream,omitempty"`
	DurMS    int64     `json:"dur_ms,omitempty"`
	TTFTMS   int64     `json:"ttft_ms,omitempty"`

	BytesIn    int64  `json:"bytes_in,omitempty"`
	BytesOut   int64  `json:"bytes_out,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	ClientKey  string `json:"client_key,omitempty"`

	Truncated        bool  `json:"truncated,omitempty"`
	Images           int   `json:"images,omitempty"`
	ImagesCompressed int   `json:"images_compressed,omitempty"`
	Fallbacks        int   `json:"fallbacks,omitempty"`
	EstInFresh       int64 `json:"est_in_fresh,omitempty"`
	EstOut           int64 `json:"est_out,omitempty"`

	Attempts []attemptFacts `json:"attempts,omitempty"`

	// Guard carries audit.Record.Guard through the cache verbatim —
	// caching it now avoids a second CacheSchemaVersion bump later, and
	// guardcol.go needs it fed through both the fresh-decode and cache-hit
	// paths identically.
	Guard *audit.GuardRecord `json:"guard,omitempty"`
	// GuardScan is the Fallback Path's result: computed by
	// scanRecordForGuard (guardscan.go) only when Guard is nil, which is
	// every real record today. Caching it is what makes the fallback
	// forensic numbers survive a cache-hit rerun identically to a
	// fresh-decode run — the same reason Guard itself is cached above.
	GuardScan *GuardScanFacts `json:"guard_scan,omitempty"`
	// GuardScanFailed is true when a panic occurred during fallback scan.
	GuardScanFailed bool `json:"guard_scan_failed,omitempty"`

	// Session / request-level features extracted once per record:
	TraceID       string            `json:"trace_id,omitempty"`
	ChatID        string            `json:"chat_id,omitempty"`
	ToolsSig      string            `json:"tools_sig,omitempty"`
	ToolsDeclared []string          `json:"tools_declared,omitempty"`
	DeclBytes     int64             `json:"decl_bytes,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Compaction    bool              `json:"compaction,omitempty"`
	ToolCalls     []string          `json:"tool_calls,omitempty"`
	Finish        string            `json:"finish,omitempty"`
	Usage         chatmsg.Usage     `json:"usage,omitempty"`
	UsageInOK     bool              `json:"usage_in_ok,omitempty"`
	UsageOutOK    bool              `json:"usage_out_ok,omitempty"`
	RoleChars     map[string]int64  `json:"role_chars,omitempty"`
	RoleTokens    map[string]int64  `json:"role_tokens,omitempty"`
	RealUsers     taskseg.RealUsers `json:"real_users,omitempty"`
	FirstText     string            `json:"first_text,omitempty"`
	RespText      string            `json:"resp_text,omitempty"`
	NoReply       bool              `json:"no_reply,omitempty"`
	RealModel     string            `json:"real_model,omitempty"`
	Msgs          int               `json:"msgs,omitempty"`
}

// fileFacts is one input file's full recordFacts payload, the shape
// marshaled into ctxgraph.CachedFile.Facts.
type fileFacts struct {
	Version     int           `json:"version,omitempty"`
	Profile     string        `json:"profile,omitempty"`
	ParseErrors int           `json:"parse_errors,omitempty"`
	Records     []recordFacts `json:"records,omitempty"`
}

// extractRecordFacts computes recordFacts for one audit.Record: the single
// fact extraction function across the whole analytics half. line is the
// record's 1-based line within its file.
func extractRecordFacts(arec *audit.Record, line int, prof taskseg.Profile) recordFacts {
	if prof == nil {
		prof = taskseg.OpenClawAware
	}
	rf := recordFacts{
		Line: line, TS: arec.TS, Model: arec.Model, Protocol: arec.Protocol,
		Outcome: arec.Outcome, Stream: arec.Stream, DurMS: arec.DurMS, TTFTMS: arec.TTFTMS,
		ClientKey: arec.ClientKeyTag,
	}
	rf.BytesIn = reqdetail.BodyBytes(arec.Client.Request.Body)
	if arec.Client.Response != nil {
		rf.BytesOut = reqdetail.BodyBytes(arec.Client.Response.Body)
	}
	rf.Endpoint, rf.ErrorClass = endpointInfo(arec)
	rf.RealModel = reqdetail.RealModel(arec)
	rf.Images, rf.ImagesCompressed = reqdetail.CountImages(arec.Images)
	if len(arec.Attempts) > 1 {
		rf.Fallbacks = 1
	}
	if arec.Outcome == "ok" {
		for _, a := range arec.Attempts {
			if reqdetail.AttemptErrorClass(a) == "truncated" {
				rf.Truncated = true
				break
			}
		}
	}
	var respBody any
	if arec.Client.Response != nil {
		respBody = arec.Client.Response.Body
	}
	rf.EstInFresh, rf.EstOut = chatmsg.EstimateDegradedTokens(arec.Facts, arec.Client.Request.Body, respBody)
	rf.Attempts = attemptFactsFrom(arec.Attempts)
	rf.Guard = arec.Guard
	if arec.Guard == nil || !guardOutboundStamped(arec.Guard) {
		rf.GuardScan, rf.GuardScanFailed = guardScanSafe(func() *GuardScanFacts { return scanRecordForGuard(arec) })
	} else {
		rf.GuardScan, rf.GuardScanFailed = guardScanSafe(func() *GuardScanFacts { return scanInboundFacts(arec) })
	}

	if tp := arec.Client.Request.Headers.Get("Traceparent"); tp != "" {
		if parts := strings.Split(tp, "-"); len(parts) >= 2 {
			rf.TraceID = parts[1]
		}
	}

	var respContent string
	if arec.Client.Response != nil {
		rf.Usage, rf.UsageInOK, rf.UsageOutOK = chatmsg.ExtractUsageSides(arec.Client.Response.Body, rf.Protocol)
		if s := taskseg.ResponseSummary(arec.Client.Response.Body); s != nil {
			rf.Finish = s.Finish
			for _, tc := range s.ToolCalls {
				if tc.Name != "" {
					rf.ToolCalls = append(rf.ToolCalls, tc.Name)
				}
			}
			respContent = strings.TrimSpace(s.Content)
			rf.NoReply = prof.NoReply(rf.Finish, respContent)
		}
	}

	body, ok := arec.Client.Request.Body.(map[string]any)
	if !ok {
		return rf
	}

	rf.ToolsDeclared = chatmsg.ToolNames(body)
	if tools, hasTools := body["tools"]; hasTools || len(rf.ToolsDeclared) > 0 {
		rf.ToolsSig = reqdetail.ToolsSig(rf.ToolsDeclared)
		if raw, err := json.Marshal(tools); err == nil {
			rf.DeclBytes = int64(len(raw))
		}
	}

	msgs := chatmsg.Messages(body)
	rf.Msgs = len(msgs)
	for role, c := range reqdetail.RoleChars(body) {
		if rf.RoleChars == nil {
			rf.RoleChars = map[string]int64{}
		}
		rf.RoleChars[role] += c
	}
	for role, t := range reqdetail.RoleTokens(body) {
		if rf.RoleTokens == nil {
			rf.RoleTokens = map[string]int64{}
		}
		rf.RoleTokens[role] += t
	}

	rawMsgs := chatmsg.RawArray(body)
	off := chatmsg.MsgOffset(body)
	leadSys := 0
	var firstText, lastUser string
	for i, m := range msgs {
		if m.Role == "system" && i == leadSys {
			leadSys++
			continue
		}
		if firstText == "" {
			firstText = m.Text
		}
		if m.Role == "user" {
			lastUser = m.Text
		}
	}

	rf.RealUsers = taskseg.IndexRealUsers(prof, msgs, rawMsgs, off)
	rf.ChatID = prof.ChatID(msgs)

	_, hasMaxCT := body["max_completion_tokens"]
	sysText := ""
	if leadSys > 0 {
		sysText = msgs[0].Text
	}
	if strings.Contains(strings.ToLower(fmtutil.CapStr(sysText, 200)), "summarization") ||
		(len(rf.ToolsDeclared) == 0 && hasMaxCT && rf.TraceID == "") {
		rf.Compaction = true
	}

	rf.Tags = templateTags(firstText, lastUser, rf.Compaction)

	if rf.Compaction {
		rf.FirstText = fmtutil.CapStr(firstText, 64<<10)
		rf.RespText = fmtutil.CapStr(respContent, 64<<10)
	} else if firstText != "" {
		rf.FirstText = fmtutil.CapStr(firstText, 16<<10)
	}

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
			// Forwarded carries the router's authoritative signal forward
			// into the cache so a cache hit reproduces the same Forwarded
			// count without having to re-derive from the legacy fallback.
			// pre-v4 cache entries (no Forwarded field) fall back to
			// IsForwarded's legacy rule at read time.
			Forwarded: a.Forwarded,
		}
		if a.Response != nil {
			af.HasResponse = true
			af.Status = a.Response.Status
		}
		out[i] = af
	}
	return out
}

// loadCachedFacts unmarshals the file identified by key's cached Facts
// payload from cache, when present. key is the file's content hash
// (ctxgraph.HashFile) — the same key ctxgraph's own cache and scanFiles
// key by, never a path or its canonical basename. cache may be nil (no
// prior cache at all). A missing entry, absent/empty Facts, or a stale
// FactsSchemaVersion all report ok=false: the caller falls back to a fresh
// decode.
func loadCachedFacts(cache *ctxgraph.FileCache, key string, prof taskseg.Profile) (ff fileFacts, ok bool) {
	if cache == nil {
		return fileFacts{}, false
	}
	cf, present := cache.Files[key]
	if !present || len(cf.Facts) == 0 || cf.FactsVersion != FactsSchemaVersion {
		return fileFacts{}, false
	}
	if err := json.Unmarshal(cf.Facts, &ff); err != nil || ff.Version != FactsSchemaVersion {
		return fileFacts{}, false
	}
	if prof != nil && ff.Profile != "" && ff.Profile != prof.Name() {
		return fileFacts{}, false
	}
	return ff, true
}

// storeCachedFacts marshals ff into cache.Files[key].Facts, preserving
// that entry's other fields (Hash/Manifests/NoBody) and stamping the
// current FactsSchemaVersion. key is the file's content hash; an empty key
// is a no-op.
func storeCachedFacts(cache *ctxgraph.FileCache, key string, ff fileFacts) {
	if cache == nil || key == "" {
		return
	}
	ff.Version = FactsSchemaVersion
	data, err := json.Marshal(ff)
	if err != nil {
		return
	}
	cf := cache.Files[key]
	cf.FactsVersion = FactsSchemaVersion
	cf.Facts = data
	cache.Files[key] = cf
}
