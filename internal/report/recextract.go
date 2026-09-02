// Ver 2026-08-15, by Sonnet 5

// Per-record extraction and the small standalone builders that read
// SessionAnalysis after the fact (buildCompactions/buildTools) — split out
// of aggregate.go to keep that file focused on orchestration. buildRec2 is
// the single place that joins an audit.Record to its ReqInfo into a rec2.
package report

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/reqdetail"
)

// buildRequestRow maps a rec2 to its per-request export row.
func buildRequestRow(rc *rec2) RequestRow {
	rr := RequestRow{
		TS:         rc.ts.Format("2006-01-02T15:04:05Z07:00"),
		Session:    rc.sessionID,
		Task:       rc.taskID,
		Turn:       rc.taskSeq,
		SessTurn:   rc.sessSeq,
		Model:      rc.model,
		Protocol:   rc.protocol,
		Outcome:    rc.outcome,
		ClientKey:  rc.clientKey,
		Endpoint:   rc.endpoint,
		Finish:     rc.finish,
		DurMS:      rc.durMS,
		TTFTMS:     rc.ttftMS,
		Msgs:       rc.msgs,
		Fallbacks:  rc.fallbacks,
		Truncated:  rc.truncated,
		ErrorClass: rc.errClass,
		DetailFile: rc.detailFile,
		Req:        ctxgraph.ReqCoord(rc.path, rc.line),
		Path:       rc.path,
		Line:       rc.line,
	}
	if rc.usageOK {
		rr.TokensIn = rc.usage.In
		rr.TokensInCached = rc.usage.CacheRead
		rr.TokensInFresh = rc.usage.Fresh()
		rr.TokensOut = rc.usage.Out
		rr.CacheEff = cacheEff(rc.usage.CacheRead, rr.TokensInFresh)
	}
	return rr
}

// WriteJSON writes the aggregate report JSON (vmr-report.json). Per-request
// rows are NOT included (they live in vmr-requests.json). Call
// LocalizeEfficiency(rep, lang) first if rep.Efficiency should reflect the
// report's actual display language — WriteJSON serializes rep exactly as
// given, it never localizes anything itself; a caller that skips
// LocalizeEfficiency gets Build's English default silently, with no error.
func WriteJSON(rep *Report2, path string) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// sortRows sorts Row slices by the given key ("model" or "date").
func sortRows(rows []Row, key string) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if key == "date" {
			return a.Date < b.Date
		}
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Protocol < b.Protocol
	})
}

// buildRec2 joins rf — one record's arec-derived facts, either freshly
// extracted (extractRecordFacts) or replayed from the parse cache
// (factscache.go) — with ri (its ReqInfo, nil for a record the session
// analyzer didn't correlate) into the aggregator's working struct. This is
// the ONLY place that performs this join: both scanFiles' fresh-decode
// path and its cache-hit path call it, so a cache hit and a cache miss can
// never disagree about how ReqInfo overrides rf.
func buildRec2(rf recordFacts, ri *ReqInfo, path string) *rec2 {
	// date/hour bucket keys use fmtutil.DisplayZone, not rf.TS's own offset.
	r := &rec2{
		ts:            rf.TS,
		date:          rf.TS.In(fmtutil.DisplayZone).Format("2006-01-02"),
		hour:          rf.TS.In(fmtutil.DisplayZone).Hour(),
		model:         rf.Model,
		protocol:      rf.Protocol,
		outcome:       rf.Outcome,
		stream:        rf.Stream,
		durMS:         rf.DurMS,
		ttftMS:        rf.TTFTMS,
		path:          path,
		line:          rf.Line,
		bytesIn:       rf.BytesIn,
		bytesOut:      rf.BytesOut,
		toolDeclCount: rf.ToolDeclCount,
		toolDeclBytes: rf.ToolDeclBytes,
		endpoint:      rf.Endpoint,
		errClass:      rf.ErrorClass,
		clientKey:     rf.ClientKey,
		truncated:     rf.TruncatedRaw, // may be OR'd with ri.Truncated below
	}
	if rf.DurMS > 0 && rf.TTFTMS > 0 {
		r.streamMS = rf.DurMS - rf.TTFTMS
		if r.streamMS < 0 {
			r.streamMS = 0
		} else {
			r.streamOK = true
		}
	}
	// images + fallbacks: the rf-derived half only matters when there is no
	// ReqInfo to join — the ri != nil block below overwrites both
	// unconditionally.
	if ri == nil {
		r.images, r.imagesCompressed, r.fallbacks = rf.ImagesRaw, rf.ImagesCompressedRaw, rf.FallbacksRaw
	}
	// join ReqInfo (grouping + expensive features it already computed)
	if ri != nil {
		r.usage = ri.Usage
		r.usageOK = ri.UsageOK
		r.finish = ri.Finish
		r.truncated = r.truncated || ri.Truncated
		r.fallbacks = ri.Fallbacks
		r.images = ri.Images
		r.imagesCompressed = ri.ImagesCompressed
		r.toolCalls = ri.ToolCalls
		r.roleChars = ri.RoleChars
		r.roleTokens = ri.RoleTokens
		r.msgs = ri.Msgs
		r.sessionID = ri.SessionID
		r.taskID = ri.TaskID
		r.taskSeq = ri.TaskSeq
		r.sessSeq = ri.SessSeq
		r.tags = ri.Tags
		r.compaction = ri.Compaction
		r.summarizes = ri.Summarizes
		r.continuesTo = ri.ContinuesTo
		r.detailFile = ri.DetailFile
		r.newInstruction = ri.NewInstruction
		r.workloadClass = workloadClassOf(ri)
	}
	if !r.usageOK && r.endpoint != "" {
		r.estInFresh, r.estOut = rf.EstInFresh, rf.EstOut
	}
	return r
}

// endpointInfo returns the endpoint that served the client and the last
// attempt's error class (for index display). "Served" prefers a strictly
// successful attempt (no error, 2xx) but falls back to the last attempt that
// got a 2xx response header at all: a stream truncated mid-transfer already
// committed its status to the client via that endpoint before dying, so the
// bytes/tokens/cost the client received are genuinely this endpoint's, not
// unattributable — only SetSuccessResponse's status matters here, not
// whether SetTruncated ran afterward.
func endpointInfo(arec *audit.Record) (endpoint, errClass string) {
	var successEp, servedEp string
	for _, a := range arec.Attempts {
		// Same predicate as EndpointRow.Forwarded / report's
		// providerquota.go basis — see audit.Attempt.IsForwarded's doc
		// comment for the historical-format compat rule. softblock paths
		// (ErrorClass="content" on a < 400 response) are excluded.
		if !a.IsForwarded() {
			continue
		}
		servedEp = a.Endpoint
		if a.Error == "" {
			successEp = a.Endpoint
		}
	}
	if successEp == "" {
		successEp = servedEp
	}
	if len(arec.Attempts) > 0 {
		errClass = reqdetail.AttemptErrorClass(arec.Attempts[len(arec.Attempts)-1])
	}
	return successEp, errClass
}

// workloadClassOf derives the workload class from a ReqInfo's Compaction +
// Tags fields.
func workloadClassOf(ri *ReqInfo) string {
	if ri == nil {
		return "interactive"
	}
	if ri.Compaction {
		return "compaction"
	}
	for _, t := range ri.Tags {
		if t == "heartbeat" {
			return "heartbeat"
		}
		if t == "dream_diary" {
			return "dream_diary"
		}
	}
	return "interactive"
}

// buildCompactions derives §6.7/CCR N-4's compaction rows from the
// analysis's standalone compaction calls.
// "Before/after" tokens are the compaction call's OWN input/output - how
// much history it was asked to compress vs how big the resulting summary
// is — not either neighboring session's own token counts, which stay
// whatever they legitimately were (see TestContextGrowthDoesNotCrossContractBreak).
// Entity loss reuses chatmsg.ExtractEntities, the same rough file-path/URL
// scan internal/story's own CompactionInfo uses (sunk to chatmsg so both
// packages share one implementation).
// IMPORTANT — caller contract: this function reads c.firstText and c.respText
// on every compaction ReqInfo in sess.Compactions, so callers must populate
// sess.Compactions (linkCompactions is what does it, in session.go) BEFORE
// this is called AND must NOT release those fields on compaction records
// in between. session.go's releaseTextBuffers keeps them alive only on
// per-session first records and compaction records for exactly this reason
// — if a future refactor reorders the two passes, compaction rows will be
// silently empty.
func buildCompactions(sess *SessionAnalysis) []CompactionRow {
	out := make([]CompactionRow, 0, len(sess.Compactions))
	for _, c := range sess.Compactions {
		row := CompactionRow{
			TS: c.TS.Format(time.RFC3339), Summarizes: c.Summarizes, ContinuesTo: c.ContinuesTo,
		}
		if c.UsageOK {
			row.TokensIn, row.TokensOut = c.Usage.In, c.Usage.Out
		}
		survived := map[string]bool{}
		for _, e := range chatmsg.ExtractEntities(c.respText) {
			survived[e] = true
		}
		for _, e := range chatmsg.ExtractEntities(c.firstText) {
			if survived[e] {
				row.SurvivedEntities = append(row.SurvivedEntities, e)
			} else {
				row.SwallowedEntities = append(row.SwallowedEntities, e)
			}
		}
		out = append(out, row)
	}
	return out
}

// buildTools derives the tool-waste fields from the analysis's ToolShapes.
func buildTools(sess *SessionAnalysis) []ToolShapeRow {
	shapes := sess.ToolShapes()
	out := make([]ToolShapeRow, 0, len(shapes))
	for _, t := range shapes {
		row := ToolShapeRow{
			Shape:         t.Shape,
			Requests:      t.Requests,
			Declared:      t.Declared,
			DeclaredBytes: t.DeclaredBytes,
			Calls:         t.Calls,
			NeverCalled:   t.NeverCalled,
		}
		row.SchemaBytesShipped = t.DeclaredBytes * int64(t.Requests)
		// Declared tools that were actually called — NOT len(t.Calls), which
		// also counts tools a response invoked without declaring them in this
		// shape (a tool added mid-conversation, a client/vendor quirk). Letting
		// those inflate the count pushed utilization past 100% and produced a
		// negative "wasted bytes" figure on the shareable card.
		row.DistinctCalled = len(t.Declared) - len(t.NeverCalled)
		if len(t.Declared) > 0 {
			row.DeclareUtilization = round2(float64(row.DistinctCalled) / float64(len(t.Declared)))
		}
		row.SchemaWasteBytes = int64(float64(row.SchemaBytesShipped) * (1 - row.DeclareUtilization))
		out = append(out, row)
	}
	return out
}
