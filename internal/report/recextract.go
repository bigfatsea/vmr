// Ver 2026-09-23 03:00, by Claude Opus 5.5

// Per-record extraction and the small standalone builders that read
// SessionAnalysis after the fact (buildCompactions/buildTools) — split out
// of aggregate.go to keep that file focused on orchestration. buildRow is
// the single place that joins an audit.Record to its ReqInfo into a recRow.
package report

import (
	"sort"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/quota"
	"vmr/internal/reqdetail"
)

// buildRequestRow maps a recRow to its per-request export row.
func buildRequestRow(rc *recRow) RequestRow {
	rr := RequestRow{
		TS:         rc.ts.UnixMilli(),
		TSDisplay:  rc.ts.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
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
	if rc.usageInOK {
		rr.TokensIn = rc.usage.In
		rr.TokensInCached = rc.usage.CacheRead
		rr.TokensInFresh = rc.usage.Fresh()
		rr.CacheEff = cacheEff(rc.usage.CacheRead, rr.TokensInFresh)
	}
	if rc.usageOutOK {
		rr.TokensOut = rc.usage.Out
	}
	rr.UsageInOK = rc.usageInOK
	rr.UsageOutOK = rc.usageOutOK
	return rr
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

// buildRow joins rf — one record's canonical extracted facts — with ri
// (its ReqInfo, containing session grouping information) into the
// aggregator's working struct recRow. Because rf is the single source of
// truth for all per-record facts, this is a direct assembly with no
// conflict arbitration or fallback overriding.
func buildRow(rf recordFacts, ri *ReqInfo, path string) *recRow {
	// date/hour bucket keys use fmtutil.DisplayZone, not rf.TS's own offset.
	r := &recRow{
		ts:               rf.TS,
		date:             rf.TS.In(fmtutil.DisplayZone).Format("2006-01-02"),
		hour:             rf.TS.In(fmtutil.DisplayZone).Hour(),
		model:            rf.Model,
		protocol:         rf.Protocol,
		outcome:          rf.Outcome,
		stream:           rf.Stream,
		durMS:            rf.DurMS,
		ttftMS:           rf.TTFTMS,
		path:             path,
		line:             rf.Line,
		bytesIn:          rf.BytesIn,
		bytesOut:         rf.BytesOut,
		endpoint:         rf.Endpoint,
		errClass:         rf.ErrorClass,
		clientKey:        rf.ClientKey,
		truncated:        rf.Truncated,
		images:           rf.Images,
		imagesCompressed: rf.ImagesCompressed,
		fallbacks:        rf.Fallbacks,
		usage:            rf.Usage,
		usageInOK:        rf.UsageInOK,
		usageOutOK:       rf.UsageOutOK,
		finish:           rf.Finish,
		toolCalls:        rf.ToolCalls,
		roleChars:        rf.RoleChars,
		roleTokens:       rf.RoleTokens,
		msgs:             rf.Msgs,
		guard:            rf.Guard,
		guardScan:        rf.GuardScan,
		guardScanFailed:  rf.GuardScanFailed,
	}
	if rf.DurMS > 0 && rf.TTFTMS > 0 {
		r.streamMS = rf.DurMS - rf.TTFTMS
		if r.streamMS < 0 {
			r.streamMS = 0
		} else {
			r.streamOK = true
		}
	}
	if ri != nil {
		r.sessionID = ri.SessionID
		r.taskID = ri.TaskID
		r.taskSeq = ri.TaskSeq
		r.sessSeq = ri.SessSeq
		r.compaction = ri.Compaction
		r.detailFile = ri.DetailFile
		r.newInstruction = ri.NewInstruction
		r.workloadClass = workloadClassOf(ri)
	} else {
		r.workloadClass = workloadClassOfFacts(rf.Compaction, rf.Tags)
	}
	// Per-side degraded estimate: fill only the side whose usage is missing,
	// and only when an endpoint actually served the request (nothing served
	// means nothing was charged). The fold (In fully to Fresh, Out max'd
	// with the placeholder) is quota.TokenCountersSides' — the same rule the
	// router charges and costFor prices through — applied here to recover
	// the per-side estimates ingest and cost consume.
	if r.endpoint != "" {
		tu := quota.TokenUsage{Fresh: r.usage.Fresh(), CacheRead: r.usage.CacheRead, CacheWrite: r.usage.CacheWrite, Out: r.usage.Out}
		raw, _ := quota.TokenCountersSides(tu, r.usageInOK, r.usageOutOK, rf.EstInFresh, rf.EstOut)
		if !r.usageInOK {
			r.estInFresh = int64(raw.Fresh)
		}
		if !r.usageOutOK {
			r.estOut = int64(raw.Out)
		}
	}
	return r
}

// endpointInfo returns the endpoint that served the client (via
// audit.Record.ServedEndpoint) and the last attempt's error class (for
// index display).
func endpointInfo(arec *audit.Record) (endpoint, errClass string) {
	if len(arec.Attempts) > 0 {
		errClass = reqdetail.AttemptErrorClass(arec.Attempts[len(arec.Attempts)-1])
	}
	return arec.ServedEndpoint(), errClass
}

// workloadClassOf derives the workload class from a ReqInfo's Compaction +
// Tags fields.
func workloadClassOf(ri *ReqInfo) string {
	if ri == nil {
		return "interactive"
	}
	return workloadClassOfFacts(ri.Compaction, ri.Tags)
}

func workloadClassOfFacts(compaction bool, tags []string) string {
	if compaction {
		return "compaction"
	}
	for _, t := range tags {
		if t == "heartbeat" {
			return "heartbeat"
		}
		if t == "dream_diary" {
			return "dream_diary"
		}
	}
	return "interactive"
}

// buildCompactions derives the compaction rows from the
// analysis's standalone compaction calls.
// "Before/after" tokens are the compaction call's OWN input/output - how
// much history it was asked to compress vs how big the resulting summary
// is — not either neighboring session's own token counts, which stay
// whatever they legitimately were (see TestContextGrowthDoesNotCrossContractBreak).
// Entity loss reuses chatmsg.ExtractEntities, the same rough file-path/URL
// scan internal/journey's own CompactionInfo uses (sunk to chatmsg so both
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
		if c.UsageInOK {
			row.TokensIn = c.Usage.In
		}
		if c.UsageOutOK {
			row.TokensOut = c.Usage.Out
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
