// Ver 2026-07-29 23:55, by Sonnet 5

// This file is the aggregation pass behind `vmr report`: it reads audit
// JSONL and fills in the Report2 buckets declared in rows.go. Rendering
// lives in render_doc.go + one section_*.go per numbered section; the
// per-request detail files in detail.go/render.go; session and task
// grouping in session.go; token extraction in chatmsg.ExtractUsage; the
// optional pricing sidecar in pricing.go.
//
// The report is organized around nine numbered sections (§0-§8) plus §6.5
// sticky effectiveness, §6.6 endpoint value, and §6.7 compaction — see
// docs/VirtualModelRouter_Design_v4_Analytics.md §2.
//
// Meta.Format (const Format, rows.go) encodes one invariant: every bucket
// keeps its own raw dur_ms / ttft_ms / stream_ms slices and computes true
// p50/p95 directly — no cross-bucket roll-up, no percentile-of-percentiles.
// stream_ms (dur-ttft) is collected as its own per-request slice for the
// same reason: P95(dur)-P95(ttft) != P95(dur-ttft).
//
// Coupled to the audit record's shape at compile time: changing
// audit.Record means changing this package and its tests in the same edit.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
)

// rec2 is Build's per-record working struct: raw fields from audit.Record
// joined to ReqInfo's grouping/features. Built once per record, shared
// read-only by every bucket.
type rec2 struct {
	ts                       time.Time
	date                     string
	hour                     int
	model, protocol, outcome string
	stream                   bool
	durMS, ttftMS            int64
	streamMS                 int64
	streamOK                 bool
	usage                    chatmsg.Usage
	usageOK                  bool
	msgs                     int
	bytesIn, bytesOut        int64
	finish                   string
	truncated                bool
	fallbacks                int
	images, imagesCompressed int
	clientKey                string
	endpoint                 string // last successful attempt's endpoint
	errClass                 string // last attempt's error class (index display)
	toolDeclBytes            int64
	toolDeclCount            int
	toolCalls                []string
	roleChars                map[string]int64
	roleTokens               map[string]int64
	// from ReqInfo
	sessionID, taskID       string
	taskSeq, sessSeq        int
	tags                    []string
	workloadClass           string
	compaction              bool
	summarizes, continuesTo string
	detailFile              string
	newInstruction          string
	path                    string
	line                    int
}

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
func Build(paths []string, now time.Time, progress io.Writer, pricing *Pricing, onRecord func(*audit.Record, *ReqInfo)) (*Report2, *SessionAnalysis, error) {
	sess, err := AnalyzeSessions(paths)
	if err != nil {
		return nil, nil, fmt.Errorf("session analysis failed (%w) — no report was written. "+
			"This step reads every input file a second time; the most common real-world cause "+
			"is one of them being rotated/compressed by the audit housekeeping sweep (a running "+
			"`vmr start` instance) while this scan was in progress. Rerun; if it persists, check "+
			"whether any input path still exists under its original name (housekeeping renames "+
			"rotated files to .zst) and that it isn't corrupt", err)
	}
	// valid session IDs (for SessionRow accumulation) + lookup by id
	sessionInfo := map[string]*SessionInfo{}
	for _, s := range sess.Sessions {
		sessionInfo[s.ID] = s
	}

	rep := &Report2{Meta: Meta{
		Format: Format, GeneratedAt: now.Format(time.RFC3339), Inputs: paths,
		SlowThreshold:    SlowThresholdMS,
		PercentileMethod: "true per-bucket from raw dur_ms/ttft_ms/stream_ms; cross-day merges use pre-aggregated *_all/hours_of_day siblings",
	}}
	var from, to time.Time

	// bucket maps
	byModel := map[string]*Row{}
	byDate := map[string]*Row{}
	hours := map[string]*HourRow{}
	hoursOfDay := map[int]*HourRow{}
	eps := map[string]*EndpointRow{}
	epsAll := map[string]*EndpointRow{}
	byClient := map[string]*ClientRow{}
	workloads := map[string]*WorkloadRow{}
	sessions := map[string]*SessionRow{}
	stickyCol := newStickyCollector()

	addA := func(r *Row, rc *rec2) {
		r.Requests++
		switch rc.outcome {
		case "ok":
			r.OK++
		case "canceled":
			r.Canceled++
		default:
			r.Errors++
		}
		if rc.stream {
			r.Streams++
		}
		if rc.fallbacks > 0 {
			r.Fallbacks++
			if rc.outcome == "ok" {
				r.FallbackRecovered++
			} else {
				r.FallbackFailed++
			}
		}
		if rc.truncated {
			r.Truncated++
		}
		r.BytesIn += rc.bytesIn
		r.BytesOut += rc.bytesOut
		if rc.usageOK {
			r.TokensIn += rc.usage.In
			r.TokensInCached += rc.usage.CacheRead
			r.TokensInCacheWrite += rc.usage.CacheWrite
			r.TokensOut += rc.usage.Out
			r.TokensReasoning += rc.usage.Reasoning
			r.TokensKnown++
		}
		if rc.durMS > 0 {
			r.RequestsWithDur++
			r.durs = append(r.durs, rc.durMS)
			r.DurMSSum += rc.durMS
			if rc.durMS > r.DurMSMax {
				r.DurMSMax = rc.durMS
			}
			if rc.durMS > SlowThresholdMS {
				r.SlowRequests++
			}
			if rc.usageOK {
				r.tokDurMS += rc.durMS
			}
		}
		if rc.ttftMS > 0 {
			r.TTFTKnown++
			r.TTFTMSSum += rc.ttftMS
			r.ttfts = append(r.ttfts, rc.ttftMS)
		}
		if rc.streamOK {
			r.StreamKnown++
			r.streamMS = append(r.streamMS, rc.streamMS)
		}
		r.Images += rc.images
		r.ImagesCompressed += rc.imagesCompressed
		if len(rc.roleChars) > 0 {
			if r.RoleChars == nil {
				r.RoleChars = map[string]int64{}
			}
			for role, c := range rc.roleChars {
				r.RoleChars[role] += c
			}
		}
		if len(rc.roleTokens) > 0 {
			if r.RoleTokens == nil {
				r.RoleTokens = map[string]int64{}
			}
			for role, t := range rc.roleTokens {
				r.RoleTokens[role] += t
			}
		}
	}

	addHour := func(h *HourRow, rc *rec2) {
		h.Requests++
		switch rc.outcome {
		case "ok":
			h.OK++
		case "canceled":
		default:
			h.Errors++
		}
		if rc.fallbacks > 0 {
			h.Fallbacks++
		}
		if rc.truncated {
			h.Truncated++
		}
		h.BytesIn += rc.bytesIn
		h.BytesOut += rc.bytesOut
		if rc.usageOK {
			h.TokensIn += rc.usage.In
			h.TokensInCached += rc.usage.CacheRead
			h.TokensInCacheWrite += rc.usage.CacheWrite
			h.TokensOut += rc.usage.Out
			h.TokensKnown++
		}
		if rc.durMS > 0 {
			h.RequestsWithDur++
			h.durs = append(h.durs, rc.durMS)
			if rc.durMS > h.DurMSMax {
				h.DurMSMax = rc.durMS
			}
			if rc.durMS > SlowThresholdMS {
				h.SlowRequests++
			}
		}
		if rc.ttftMS > 0 {
			h.TTFTKnown++
			h.ttfts = append(h.ttfts, rc.ttftMS)
		}
		if rc.streamOK {
			h.StreamKnown++
			h.streamMS = append(h.streamMS, rc.streamMS)
		}
		h.Images += rc.images
	}

	// Endpoint attempt-level accounting (G-family).
	addAttempt := func(e *EndpointRow, a audit.Attempt) {
		e.Attempts++
		if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
			e.OK++
		} else {
			e.Failed++
			e.WastedMS += a.DurMS
			cls := attemptErrorClass(a)
			if cls == "" {
				cls = "unknown"
			}
			if e.ErrorClasses == nil {
				e.ErrorClasses = map[string]int{}
			}
			e.ErrorClasses[cls]++
		}
	}
	// Request-level metrics attach to the endpoint that served the client.
	addEndpointReq := func(e *EndpointRow, rc *rec2) {
		e.Requests++
		if rc.outcome == "ok" {
			e.RequestsOK++
		}
		if rc.usageOK {
			e.TokensIn += rc.usage.In
			e.TokensInCached += rc.usage.CacheRead
			e.TokensInCacheWrite += rc.usage.CacheWrite
			e.TokensOut += rc.usage.Out
			e.TokensReasoning += rc.usage.Reasoning
			e.TokensKnown++
			e.inToks = append(e.inToks, rc.usage.In)
			e.outToks = append(e.outToks, rc.usage.Out)
		}
		if rc.ttftMS > 0 {
			e.TTFTKnown++
			e.ttfts = append(e.ttfts, rc.ttftMS)
		}
		if rc.durMS > 0 {
			e.RequestsWithDur++
			e.durs = append(e.durs, rc.durMS)
			e.DurMSSum += rc.durMS
			if rc.durMS > e.DurMSMax {
				e.DurMSMax = rc.durMS
			}
			if rc.durMS > SlowThresholdMS {
				e.SlowRequests++
			}
		}
		if rc.streamOK {
			e.StreamKnown++
			e.streamMS = append(e.streamMS, rc.streamMS)
		}
	}

	addClient := func(c *ClientRow, rc *rec2) {
		c.Requests++
		switch rc.outcome {
		case "ok":
			c.OK++
		case "canceled":
		default:
			c.Errors++
		}
		if rc.usageOK {
			c.TokensIn += rc.usage.In
			c.TokensInCached += rc.usage.CacheRead
			c.TokensInCacheWrite += rc.usage.CacheWrite
			c.TokensOut += rc.usage.Out
			c.TokensReasoning += rc.usage.Reasoning
			c.TokensKnown++
			c.inToks = append(c.inToks, rc.usage.In)
			c.outToks = append(c.outToks, rc.usage.Out)
		}
		if rc.durMS > 0 {
			c.RequestsWithDur++
			c.durs = append(c.durs, rc.durMS)
			if rc.durMS > SlowThresholdMS {
				c.SlowRequests++
			}
		}
		if rc.ttftMS > 0 {
			c.ttfts = append(c.ttfts, rc.ttftMS)
		}
		if rc.streamOK {
			c.streamMS = append(c.streamMS, rc.streamMS)
		}
	}

	addWorkload := func(w *WorkloadRow, rc *rec2) {
		w.Requests++
		if rc.usageOK {
			w.TokensIn += rc.usage.In
			w.TokensInCached += rc.usage.CacheRead
			w.TokensInCacheWrite += rc.usage.CacheWrite
			w.TokensOut += rc.usage.Out
			w.TokensKnown++
		}
		if rc.durMS > 0 {
			w.RequestsWithDur++
			w.durs = append(w.durs, rc.durMS)
			if rc.durMS > SlowThresholdMS {
				w.SlowRequests++
			}
		}
		if rc.streamOK {
			w.streamMS = append(w.streamMS, rc.streamMS)
		}
		w.ToolCalls += len(rc.toolCalls)
		if len(rc.toolCalls) > 0 {
			w.RequestsWithToolCalls++
		}
	}

	addSession := func(s *SessionRow, rc *rec2) {
		s.Requests++
		switch rc.outcome {
		case "ok":
			s.OK++
		case "canceled":
		default:
			s.Errors++
		}
		if rc.fallbacks > 0 {
			s.Fallbacks++
		}
		if rc.truncated {
			s.Truncated++
		}
		if rc.usageOK {
			s.TokensIn += rc.usage.In
			s.TokensInCached += rc.usage.CacheRead
			s.TokensInCacheWrite += rc.usage.CacheWrite
			s.TokensOut += rc.usage.Out
			s.TokensKnown++
		}
		if rc.durMS > 0 {
			s.RequestsWithDur++
			s.durs = append(s.durs, rc.durMS)
			if rc.durMS > s.DurMSMax {
				s.DurMSMax = rc.durMS
			}
		}
		if rc.ttftMS > 0 {
			s.TTFTKnown++
			s.ttfts = append(s.ttfts, rc.ttftMS)
		}
		s.Images += rc.images
		if len(rc.roleChars) > 0 {
			if s.RoleChars == nil {
				s.RoleChars = map[string]int64{}
			}
			for role, c := range rc.roleChars {
				s.RoleChars[role] += c
			}
		}
	}

	// ---- single pass over files, joined to ReqInfo ----
	for fileIdx, path := range paths {
		fileStart := time.Now()
		var fileRecords int
		rc, err := audit.OpenLogFile(path)
		if err != nil {
			return nil, nil, err
		}
		line := 0
		scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
			line++
			var arec audit.Record
			if err := json.Unmarshal(lineBytes, &arec); err != nil {
				rep.Meta.ParseErrors++
				return
			}
			rep.Meta.Records++
			fileRecords++
			if from.IsZero() || arec.TS.Before(from) {
				from = arec.TS
			}
			if arec.TS.After(to) {
				to = arec.TS
			}
			ri := sess.Lookup(path, line)
			if onRecord != nil {
				onRecord(&arec, ri)
			}
			rc := buildRec2(&arec, ri, path, line)

			date := rc.date
			hour := rc.hour
			model := rc.model
			if model == "" {
				model = "(rejected)"
			}

			// 1. Overall
			addA(&rep.Overall, rc)
			// 2. ByModel
			mk := model + "\x00" + rc.protocol
			mr := byModel[mk]
			if mr == nil {
				mr = &Row{Model: model, Protocol: rc.protocol}
				byModel[mk] = mr
			}
			addA(mr, rc)
			// 3. ByDate
			dr := byDate[date]
			if dr == nil {
				dr = &Row{Date: date}
				byDate[date] = dr
			}
			addA(dr, rc)
			// 4. Hours + HoursOfDay
			hk := fmt.Sprintf("%s\x00%02d", date, hour)
			hr := hours[hk]
			if hr == nil {
				hr = &HourRow{Date: date, Hour: hour}
				hours[hk] = hr
			}
			addHour(hr, rc)
			hod := hoursOfDay[hour]
			if hod == nil {
				hod = &HourRow{Hour: hour}
				hoursOfDay[hour] = hod
			}
			addHour(hod, rc)
			// 5. Endpoints + EndpointsAll (attempts), request-level on success ep
			for _, a := range arec.Attempts {
				k := date + "\x00" + a.Endpoint
				e := eps[k]
				if e == nil {
					e = &EndpointRow{Date: date, Endpoint: a.Endpoint}
					eps[k] = e
				}
				addAttempt(e, a)
				ea := epsAll[a.Endpoint]
				if ea == nil {
					ea = &EndpointRow{Endpoint: a.Endpoint}
					epsAll[a.Endpoint] = ea
				}
				addAttempt(ea, a)
				if a.Endpoint == rc.endpoint {
					addEndpointReq(e, rc)
					addEndpointReq(ea, rc)
				}
			}
			// 6. ByClient (skip empty tag - auth disabled / no match)
			if rc.clientKey != "" {
				c := byClient[rc.clientKey]
				if c == nil {
					c = &ClientRow{ClientKey: rc.clientKey}
					byClient[rc.clientKey] = c
				}
				addClient(c, rc)
			}
			// 7. Workloads
			wc := rc.workloadClass
			if wc == "" {
				wc = "interactive"
			}
			w := workloads[wc]
			if w == nil {
				w = &WorkloadRow{Class: wc}
				workloads[wc] = w
			}
			addWorkload(w, rc)
			// 8. Sessions (only grouped)
			if rc.sessionID != "" && sessionInfo[rc.sessionID] != nil {
				s := sessions[rc.sessionID]
				if s == nil {
					info := sessionInfo[rc.sessionID]
					s = &SessionRow{ID: info.ID, Title: info.Title, Tasks: len(info.Tasks),
						ContinuedFrom: info.ContinuedFrom, Class: wc, ClientKey: rc.clientKey}
					if len(info.Recs) > 0 {
						s.From = info.Recs[0].TS.Format(time.RFC3339)
						s.To = info.Recs[len(info.Recs)-1].TS.Format(time.RFC3339)
					}
					sessions[rc.sessionID] = s
				}
				addSession(s, rc)
			}
			// cost (if pricing): overall + by-model (existing) plus
			// by-endpoint (epsAll, cross-date — matches §3 端点健康's basis)
			// and by-client, when either bucket applies to this record.
			if pricing != nil && rc.endpoint != "" {
				provider, model := splitEndpointProviderModel(rc.endpoint)
				pr, ok := pricing.RateFor(provider, model, rc.ts)
				if ok {
					c := costFor(pr, rc)
					if rep.Overall.CostEstimate == nil {
						rep.Overall.CostEstimate = new(float64)
					}
					*rep.Overall.CostEstimate += c
					if mr.CostEstimate == nil {
						mr.CostEstimate = new(float64)
					}
					*mr.CostEstimate += c
					if ea := epsAll[rc.endpoint]; ea != nil {
						if ea.CostEstimate == nil {
							ea.CostEstimate = new(float64)
						}
						*ea.CostEstimate += c
					}
					if rc.clientKey != "" {
						if cl := byClient[rc.clientKey]; cl != nil {
							if cl.CostEstimate == nil {
								cl.CostEstimate = new(float64)
							}
							*cl.CostEstimate += c
						}
					}
				}
			}
			// Sticky Model effectiveness (sticky.go): buffered per session,
			// resolved after the pass — it needs each session in order.
			stickyCol.add(rc)
			// per-request export row
			rep.requests = append(rep.requests, buildRequestRow(rc))
		}, func() {
			rep.Meta.ParseErrors++
		})
		rc.Close()
		if scanErr != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, scanErr)
		}
		if progress != nil {
			fmt.Fprintf(progress, "[%d/%d] %s  done: %d records (%s)\n",
				fileIdx+1, len(paths), path, fileRecords, time.Since(fileStart).Round(time.Millisecond))
		}
	}

	if !from.IsZero() {
		rep.Meta.From = from.Format(time.RFC3339)
		rep.Meta.To = to.Format(time.RFC3339)
	}

	// ---- finish all buckets ----
	finishRow(&rep.Overall)
	for _, r := range byModel {
		finishRow(r)
		rep.ByModel = append(rep.ByModel, *r)
	}
	for _, r := range byDate {
		finishRow(r)
		rep.ByDate = append(rep.ByDate, *r)
	}
	for _, h := range hours {
		finishHour(h)
		rep.Hours = append(rep.Hours, *h)
	}
	for _, h := range hoursOfDay {
		finishHour(h)
		rep.HoursOfDay = append(rep.HoursOfDay, *h)
	}
	for _, e := range eps {
		finishEndpoint(e)
		rep.Endpoints = append(rep.Endpoints, *e)
	}
	for _, e := range epsAll {
		finishEndpoint(e)
		rep.EndpointsAll = append(rep.EndpointsAll, *e)
	}
	for _, c := range byClient {
		finishClient(c)
		rep.ByClient = append(rep.ByClient, *c)
	}
	for _, w := range workloads {
		finishWorkload(w)
		rep.Workloads = append(rep.Workloads, *w)
	}
	for _, s := range sessions {
		finishSession(s, sessionInfo[s.ID])
		rep.Sessions = append(rep.Sessions, *s)
	}
	rep.Tools = buildTools(sess)
	rep.Compactions = buildCompactions(sess)
	rep.Sticky = stickyCol.result()
	rep.Efficiency = buildFindingsForJSON(rep)
	rep.Pricing = pricing

	// ---- sort all slices ----
	sortRows(rep.ByModel, "model")
	sortRows(rep.ByDate, "date")
	sort.Slice(rep.Hours, func(i, j int) bool {
		if rep.Hours[i].Date != rep.Hours[j].Date {
			return rep.Hours[i].Date < rep.Hours[j].Date
		}
		return rep.Hours[i].Hour < rep.Hours[j].Hour
	})
	sort.Slice(rep.HoursOfDay, func(i, j int) bool { return rep.HoursOfDay[i].Hour < rep.HoursOfDay[j].Hour })
	sort.Slice(rep.Endpoints, func(i, j int) bool {
		if rep.Endpoints[i].Date != rep.Endpoints[j].Date {
			return rep.Endpoints[i].Date < rep.Endpoints[j].Date
		}
		return rep.Endpoints[i].Endpoint < rep.Endpoints[j].Endpoint
	})
	// Every comparator below sorts primarily by a count/byte-size value that
	// legitimately repeats across rows (two endpoints can both have exactly
	// 1 attempt; two sessions can both have exactly 2 requests). Each also
	// appends the bucket's own identity field as a tie-break — Endpoint/
	// ClientKey/Class/ID/Shape are each guaranteed unique within their own
	// slice (they're literally what these rows were grouped by), so the
	// comparator always returns a strict answer for two distinct rows.
	// Without it, ties fell back to whatever order the slice already had —
	// which itself comes from ranging over a Go map a few lines up
	// (byModel/epsAll/byClient/... above), an order the language spec
	// deliberately does not guarantee is the same from one run to the next.
	// The result: rows sharing a tied value would silently swap places
	// between two otherwise-identical runs of the same binary against the
	// same input (caught by TestBuildIsDeterministic — first noticed by
	// comparing loadtest-report.md across two runs).
	sort.Slice(rep.EndpointsAll, func(i, j int) bool {
		a, b := rep.EndpointsAll[i], rep.EndpointsAll[j]
		if a.Attempts != b.Attempts {
			return a.Attempts > b.Attempts
		}
		return a.Endpoint < b.Endpoint
	})
	sort.Slice(rep.ByClient, func(i, j int) bool {
		a, b := rep.ByClient[i], rep.ByClient[j]
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.ClientKey < b.ClientKey
	})
	sort.Slice(rep.Workloads, func(i, j int) bool {
		a, b := rep.Workloads[i], rep.Workloads[j]
		if a.TokensIn != b.TokensIn {
			return a.TokensIn > b.TokensIn
		}
		return a.Class < b.Class
	})
	sort.Slice(rep.Sessions, func(i, j int) bool {
		// interactive first (by requests), then scheduled; stable within
		a, b := rep.Sessions[i], rep.Sessions[j]
		if (a.Class == "interactive") != (b.Class == "interactive") {
			return a.Class == "interactive"
		}
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.ID < b.ID
	})
	sort.Slice(rep.Tools, func(i, j int) bool {
		a, b := rep.Tools[i], rep.Tools[j]
		if a.SchemaWasteBytes != b.SchemaWasteBytes {
			return a.SchemaWasteBytes > b.SchemaWasteBytes
		}
		return a.Shape < b.Shape
	})
	return rep, sess, nil
}

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
		Path:       rc.path,
		Line:       rc.line,
	}
	if rc.usageOK {
		rr.TokensIn = rc.usage.In
		rr.TokensInCached = rc.usage.CacheRead
		rr.TokensInFresh = freshTokens(rc.usage.In, rc.usage.CacheRead, rc.usage.CacheWrite)
		rr.TokensOut = rc.usage.Out
		rr.CacheEff = cacheEff(rc.usage.CacheRead, rr.TokensInFresh)
	}
	return rr
}

// WriteJSON writes the aggregate report JSON (vmr-report.json). Per-request
// rows are NOT included (they live in vmr-requests.jsonl).
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

// buildRec2 extracts the aggregator's per-record fields from an audit.Record joined
// to its ReqInfo (which may be nil for records the analyzer skipped).
func buildRec2(arec *audit.Record, ri *ReqInfo, path string, line int) *rec2 {
	r := &rec2{
		ts:       arec.TS,
		date:     arec.TS.Format("2006-01-02"),
		hour:     arec.TS.Hour(),
		model:    arec.Model,
		protocol: arec.Protocol,
		outcome:  arec.Outcome,
		stream:   arec.Stream,
		durMS:    arec.DurMS,
		ttftMS:   arec.TTFTMS,
		path:     path,
		line:     line,
	}
	if arec.DurMS > 0 && arec.TTFTMS > 0 {
		r.streamMS = arec.DurMS - arec.TTFTMS
		if r.streamMS < 0 {
			r.streamMS = 0
		} else {
			r.streamOK = true
		}
	}
	// bytes (recompute; ReqInfo keeps these unexported)
	r.bytesIn = bodyBytes(arec.Client.Request.Body)
	if arec.Client.Response != nil {
		r.bytesOut = bodyBytes(arec.Client.Response.Body)
	}
	// tool declaration bytes (recompute; ReqInfo.declBytes unexported)
	r.toolDeclCount, r.toolDeclBytes = toolDeclInfo(arec.Client.Request.Body)
	// endpoint served + last error class (recompute; ReqInfo unexported)
	r.endpoint, r.errClass = endpointInfo(arec)
	r.clientKey = arec.ClientKeyTag
	// images
	for _, img := range arec.Images {
		r.images++
		if img.Downscaled {
			r.imagesCompressed++
		}
	}
	// fallbacks
	if len(arec.Attempts) > 1 {
		r.fallbacks = 1
	}
	// truncated: ok outcome with a truncated attempt error
	if arec.Outcome == "ok" {
		for _, a := range arec.Attempts {
			if attemptErrorClass(a) == "truncated" {
				r.truncated = true
				break
			}
		}
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
		if a.Response != nil && a.Response.Status < 400 {
			servedEp = a.Endpoint
			if a.Error == "" {
				successEp = a.Endpoint
			}
		}
	}
	if successEp == "" {
		successEp = servedEp
	}
	if len(arec.Attempts) > 0 {
		errClass = attemptErrorClass(arec.Attempts[len(arec.Attempts)-1])
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

// costFor computes one record's estimated cost from its endpoint's rate.
func costFor(pr PricingRate, rc *rec2) float64 {
	if !rc.usageOK {
		return 0
	}
	fresh := rc.usage.In - rc.usage.CacheRead - rc.usage.CacheWrite
	if fresh < 0 {
		fresh = 0
	}
	return pr.InFreshPer1M/1e6*float64(fresh) +
		pr.CacheWritePer1M/1e6*float64(rc.usage.CacheWrite) +
		pr.OutPer1M/1e6*float64(rc.usage.Out)
}

// splitEndpointProviderModel splits a "protocol:provider:model" endpoint
// label into its provider and model segments — pricing.yaml keys rates by
// provider+model only, protocol-agnostic (see pricing.go). SplitN(…, 3)
// rather than a plain Split: a real-world model name can itself contain ":"
// or "/" (e.g. OpenRouter's "z-ai/glm-5.2"), so this only ever isolates the
// first two colon-separated segments and leaves the third — the model —
// exactly as-is, whatever it contains.
func splitEndpointProviderModel(endpoint string) (provider, model string) {
	parts := strings.SplitN(endpoint, ":", 3)
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], parts[2]
}

// buildCompactions derives §6.7/CCR N-4's compaction rows from the
// analysis's standalone compaction calls.
// "Before/after" tokens are the compaction call's OWN input/output — how
// much history it was asked to compress vs how big the resulting summary
// is — not either neighboring session's own token counts, which stay
// whatever they legitimately were (see TestContextGrowthDoesNotCrossContractBreak).
// Entity loss reuses chatmsg.ExtractEntities, the same rough file-path/URL
// scan internal/story's own CompactionInfo uses (sunk to chatmsg so both
// packages share one implementation).
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
		row.DistinctCalled = len(t.Calls)
		if len(t.Declared) > 0 {
			row.DeclareUtilization = round2(float64(row.DistinctCalled) / float64(len(t.Declared)))
		}
		row.SchemaWasteBytes = int64(float64(row.SchemaBytesShipped) * (1 - row.DeclareUtilization))
		out = append(out, row)
	}
	return out
}
