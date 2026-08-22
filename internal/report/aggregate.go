// Ver 2026-07-29 23:55, by Sonnet 5

// This file is the aggregation pass behind `vmr report`: it reads audit
// JSONL and fills in the Report2 buckets declared in rows.go. Rendering
// lives in render_doc.go + one section_*.go per numbered section; the
// per-request detail files in detail.go/render.go; session and task
// grouping in session.go; token extraction in chatmsg.ExtractUsage; the
// optional pricing sidecar in pricing.go. Per-bucket accumulation
// (TrafficStats.Ingest and friends) lives in ingest.go; per-record
// extraction (buildRec2 and friends) lives in recextract.go — this file is
// buildInternal's own orchestration: aggState plus its three phases
// (scanFiles/finishBuckets/sortBuckets).
//
// The report is organized around nine numbered sections (§0-§8) plus §6.5
// sticky effectiveness, §6.6 endpoint value, and §6.7 compaction — see
// docs/VirtualModelRouter_Design_v4_Analytics.md's `vmr report` section.
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
	"sort"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/pricing"
	"vmr/internal/taskseg"
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
	// estInFresh/estOut hold the degraded byte-count estimate when usage
	// couldn't be sniffed (usageOK false); both 0 otherwise. See
	// tokenest.go's estimateDegradedTokens.
	estInFresh, estOut       int64
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

// diagnosticNormMarker is the subset of NormalizerStream.Applied()'s vocabulary
// (internal/respnorm's noteApplied call sites) worth surfacing as
// a per-endpoint frequency stat via EndpointRow.NormCounts: an actual vendor
// content quirk vmr silently worked around, not a routine transport/protocol
// step every successful response goes through regardless of vendor
// (model_rewrite fires on ~100% of them, done_appended/buffered/opaque/
// resumed_stream/overflow_raw_passthrough/crlf_framing_suspected describe
// vmr's own transport handling, not a vendor's content behavior). Kept here
// rather than as an exported list in internal/router: the router package has
// no reason to know which of its own marker strings a downstream analytics
// consumer finds diagnostically interesting.
var diagnosticNormMarker = map[string]bool{
	"think_strip":                       true,
	"thinking_process_strip":            true,
	"soft_block_detected":               true,
	"thinking_process_pattern_detected": true,
}

// aggState is buildInternal's per-run working state: every bucket map plus
// the collectors/inputs its three phases (scanFiles/finishBuckets/
// sortBuckets, below) need. Split out so buildInternal itself is just the
// three-call orchestration — see that function's own doc comment.
type aggState struct {
	rep         *Report2
	sess        *SessionAnalysis
	sessionInfo map[string]*SessionInfo

	byModel    map[string]*Row
	byDate     map[string]*Row
	hours      map[string]*HourRow
	hoursOfDay map[int]*HourRow
	eps        map[string]*EndpointRow
	epsAll     map[string]*EndpointRow
	byClient   map[string]*ClientRow
	workloads  map[string]*WorkloadRow
	sessions   map[string]*SessionRow

	stickyCol         *stickyCollector
	clientEndpointCol *clientEndpointCollector
	pricingSrc        *pricing.Resolver

	// excludeClientTags is P6.4's self-traffic exclusion set — a record
	// whose ClientKeyTag is a member never reaches any bucket. Computed
	// once by cmd/vmr (the identification rule's one definition, see
	// audit.KeyTag's use in cmd/vmr's selfTrafficExcludeTags) and threaded straight
	// through; nil/empty means "exclude nothing", the default when no
	// llm_key is configured.
	excludeClientTags map[string]bool

	from, to time.Time
}

func newAggState(rep *Report2, sess *SessionAnalysis, pricingSrc *pricing.Resolver, excludeClientTags map[string]bool) *aggState {
	sessionInfo := map[string]*SessionInfo{}
	for _, s := range sess.Sessions {
		sessionInfo[s.ID] = s
	}
	return &aggState{
		rep:               rep,
		sess:              sess,
		sessionInfo:       sessionInfo,
		byModel:           map[string]*Row{},
		byDate:            map[string]*Row{},
		hours:             map[string]*HourRow{},
		hoursOfDay:        map[int]*HourRow{},
		eps:               map[string]*EndpointRow{},
		epsAll:            map[string]*EndpointRow{},
		byClient:          map[string]*ClientRow{},
		workloads:         map[string]*WorkloadRow{},
		sessions:          map[string]*SessionRow{},
		stickyCol:         newStickyCollector(),
		clientEndpointCol: newClientEndpointCollector(),
		pricingSrc:        pricingSrc,
		excludeClientTags: excludeClientTags,
	}
}

// buildInternal is Build/BuildCached's shared body — see build_cached.go
// for both entry points' doc comments and the full rationale (two-read
// design, onRecord's independence from Build's own success/failure, the
// session-analysis-failure error message below). Kept to session analysis
// plus wiring aggState's three phases (scanFiles/finishBuckets/sortBuckets)
// in sequence — no behavior split from the pre-B4 single function, only a
// declaration/accumulation split (see rows.go's TrafficStats and
// ingest.go's per-type Ingest methods) and a phase split.
// Not a MetricAggregator interface: a single-threaded batch loop over one
// record type has no caller that needs to swap the aggregator at runtime,
// so an interface would buy polymorphism nobody uses.
func buildInternal(paths []string, now time.Time, progress io.Writer, pricingInfo *Pricing, pricingSrc *pricing.Resolver, onRecord func(*audit.Record, *ReqInfo), prof taskseg.Profile, prior *ctxgraph.FileCache, quotas map[string][]ProviderQuotaRef, excludeClientTags map[string]bool) (*Report2, *SessionAnalysis, *ctxgraph.FileCache, error) {
	sess, cache, err := AnalyzeSessionsCached(paths, prior, prof)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("session analysis failed (%w) — no report was written. "+
			"This step reads every input file a second time; the most common real-world cause "+
			"is one of them being rotated/compressed by the audit housekeeping sweep (a running "+
			"`vmr start` instance) while this scan was in progress. Rerun; if it persists, check "+
			"whether any input path still exists under its original name (housekeeping renames "+
			"rotated files to .zst) and that it isn't corrupt", err)
	}
	// Self-traffic exclusion (P6.4) must also reach sess.Recs/Compactions
	// here, not just ingestRecord's own per-record skip below: buildTools/
	// buildCompactions (§5/§6.7) read straight from sess, a completely
	// separate pass from the scanFiles loop ingestRecord runs in — an
	// excluded record's tool calls/compaction entry would otherwise still
	// surface in those two sections even though it never gets a
	// RequestRow or contributes to Overall. sess.Sessions is deliberately
	// left untouched: rep.Sessions (§6) is already correctly filtered
	// because a self-traffic session never gets a SessionRow in the first
	// place (every one of its records is skipped in ingestRecord below),
	// so there is nothing reading sess.Sessions directly that this would
	// need to protect. Meta.SelfTrafficExcluded is NOT incremented here —
	// ingestRecord's own per-record check below counts every excluded
	// record exactly once, from the scanFiles pass every record goes
	// through regardless of whether it also appears in sess.
	excludeSelfTrafficFromSessionAnalysis(sess, excludeClientTags)

	rep := &Report2{Meta: Meta{
		Format: Format, GeneratedAt: now.Format(time.RFC3339), Inputs: paths,
		SlowThreshold:    SlowThresholdMS,
		PercentileMethod: "true per-bucket from raw dur_ms/ttft_ms/stream_ms; cross-day merges use pre-aggregated *_all/hours_of_day siblings",
	}}

	st := newAggState(rep, sess, pricingSrc, excludeClientTags)
	if err := st.scanFiles(paths, progress, onRecord, cache); err != nil {
		return nil, nil, nil, err
	}
	st.finishBuckets(pricingInfo, quotas, now, progress)
	st.sortBuckets()
	return rep, sess, cache, nil
}

// scanFiles is buildInternal's single pass over the input files, joined to
// ReqInfo via st.sess.Lookup. cache is the same *ctxgraph.FileCache
// AnalyzeSessionsCached already returned (hash-fresh for every path in
// paths — see ScanCached's postcondition) — a file whose cached Facts are
// still valid skips reopening/re-decoding it entirely (ingestCachedFile);
// everything else falls back to a real read (scanAndCacheFile), which
// also populates cache for next time. The cache-hit shortcut only applies
// when onRecord is nil (-details off): a caller that needs the raw
// audit.Record for detail rendering needs the file open regardless, so
// there is nothing to save by skipping decode in that case (see
// docs/future-strategy/story_report_architecture_opus-5.md §7.6c on why
// -details' own cost stays separate).
func (st *aggState) scanFiles(paths []string, progress io.Writer, onRecord func(*audit.Record, *ReqInfo), cache *ctxgraph.FileCache) error {
	for fileIdx, path := range paths {
		fileStart := time.Now()
		key := ctxgraph.CanonicalPath(path)
		var fileRecords int
		var err error
		if ff, ok := loadCachedFacts(cache, key); onRecord == nil && ok {
			fileRecords = st.ingestCachedFile(path, ff)
		} else {
			fileRecords, err = st.scanAndCacheFile(path, key, cache, onRecord)
		}
		if err != nil {
			return err
		}
		if progress != nil {
			fmt.Fprintf(progress, "[%d/%d] %s  done: %d records (%s)\n",
				fileIdx+1, len(paths), path, fileRecords, time.Since(fileStart).Round(time.Millisecond))
		}
	}
	return nil
}

// ingestCachedFile replays one file's cached recordFacts — no file I/O, no
// JSON decode of the record bodies — through the exact same
// buildRec2/ingestRecord path a fresh decode would use. Returns the record
// count for the progress line.
func (st *aggState) ingestCachedFile(path string, ff fileFacts) int {
	st.rep.Meta.Records += len(ff.Records)
	st.rep.Meta.ParseErrors += ff.ParseErrors
	for _, rf := range ff.Records {
		ri := st.sess.Lookup(path, rf.Line)
		st.ingestRecord(buildRec2(rf, ri, path), rf.Attempts)
	}
	return len(ff.Records)
}

// scanAndCacheFile is scanFiles' fresh-decode path: open path, decode
// every line, extract+ingest each record, then store the freshly
// extracted facts back into cache — regardless of why this path was taken
// (a genuine cache miss, or onRecord forcing a decode a Facts hit would
// otherwise have skipped) — so a later -details=false run over the same
// file benefits even if this run needed the raw records for detail
// rendering. cache may be nil (no prior/output cache at all, e.g. a caller
// using Build instead of BuildCached); storeCachedFacts/loadCachedFacts
// both treat that as a no-op rather than a special case here.
func (st *aggState) scanAndCacheFile(path, key string, cache *ctxgraph.FileCache, onRecord func(*audit.Record, *ReqInfo)) (int, error) {
	f, err := audit.OpenLogFile(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var ff fileFacts
	line := 0
	scanErr := audit.ForEachLine(f, audit.MaxLogLine, func(lineBytes []byte) {
		line++
		var arec audit.Record
		if err := json.Unmarshal(lineBytes, &arec); err != nil {
			st.rep.Meta.ParseErrors++
			ff.ParseErrors++
			return
		}
		st.rep.Meta.Records++
		rf := extractRecordFacts(&arec, line)
		ff.Records = append(ff.Records, rf)
		ri := st.sess.Lookup(path, line)
		// Self-traffic exclusion (P6.4) must gate onRecord too, not just
		// ingestRecord below: onRecord is the -details detail-page writer
		// (setupDetailWriter's callback) — without this check it would
		// still materialize a details/*.md page for an excluded record,
		// an orphan file no index ever links to (the record's RequestRow
		// is never created, so nothing points at it).
		if onRecord != nil && !st.excludeClientTags[arec.ClientKeyTag] {
			onRecord(&arec, ri)
		}
		st.ingestRecord(buildRec2(rf, ri, path), rf.Attempts)
	}, func() {
		st.rep.Meta.ParseErrors++
		ff.ParseErrors++
	})
	if scanErr != nil {
		return 0, fmt.Errorf("%s: %w", path, scanErr)
	}
	storeCachedFacts(cache, key, ff)
	return len(ff.Records), nil
}

// ingestRecord fans rc (already joined to its ReqInfo — see buildRec2) out
// to every bucket it touches, given the same record's attempt-level facts
// (needed only by ingestEndpoints, so not part of rec2 itself).
func (st *aggState) ingestRecord(rc *rec2, attempts []attemptFacts) {
	if st.excludeClientTags[rc.clientKey] {
		// Self-analysis traffic (P6.4): vmr story's own -llm-addr calls
		// route back through this same instance and land in the audit
		// log like any other request — but their cost/tokens are the
		// analysis tool's own overhead, not the workload being analyzed,
		// so they're excluded from every bucket by default (counted, not
		// silently dropped — see Meta.SelfTrafficExcluded).
		st.rep.Meta.SelfTrafficExcluded++
		return
	}
	if st.from.IsZero() || rc.ts.Before(st.from) {
		st.from = rc.ts
	}
	if rc.ts.After(st.to) {
		st.to = rc.ts
	}
	mr, dr := st.ingestRowBuckets(rc)
	st.ingestEndpoints(attempts, rc)
	st.ingestSecondaryBuckets(rc)
	// cost (if pricing): overall + by-model + by-date + by-endpoint (epsAll,
	// cross-date — matches the Endpoint Health section's basis) + by-client,
	// when each bucket applies to this record.
	accumulateCost(st.rep, mr, dr, st.epsAll, st.byClient, st.pricingSrc, rc)
	// Sticky Model effectiveness (sticky.go): buffered per session, resolved
	// after the pass — it needs each session in order.
	st.stickyCol.add(rc)
	st.clientEndpointCol.add(rc)
	// per-request export row
	st.rep.requests = append(st.rep.requests, buildRequestRow(rc))
}

// ingestRowBuckets updates Overall/ByModel/ByDate/Hours/HoursOfDay,
// returning the ByModel/ByDate rows so ingestRecord can price them without
// a second map lookup.
func (st *aggState) ingestRowBuckets(rc *rec2) (mr, dr *Row) {
	model := rc.model
	if model == "" {
		model = "(rejected)"
	}
	st.rep.Overall.Ingest(rc)

	mk := model + "\x00" + rc.protocol
	mr = st.byModel[mk]
	if mr == nil {
		mr = &Row{Model: model, Protocol: rc.protocol}
		st.byModel[mk] = mr
	}
	mr.Ingest(rc)

	dr = st.byDate[rc.date]
	if dr == nil {
		dr = &Row{Date: rc.date}
		st.byDate[rc.date] = dr
	}
	dr.Ingest(rc)

	hk := fmt.Sprintf("%s\x00%02d", rc.date, rc.hour)
	hr := st.hours[hk]
	if hr == nil {
		hr = &HourRow{Date: rc.date, Hour: rc.hour}
		st.hours[hk] = hr
	}
	hr.Ingest(rc)

	hod := st.hoursOfDay[rc.hour]
	if hod == nil {
		hod = &HourRow{Hour: rc.hour}
		st.hoursOfDay[rc.hour] = hod
	}
	hod.Ingest(rc)
	return mr, dr
}

// ingestEndpoints updates Endpoints/EndpointsAll from one record's attempts.
func (st *aggState) ingestEndpoints(attempts []attemptFacts, rc *rec2) {
	// reqAttributed: the `a.Endpoint == rc.endpoint` guard alone does NOT
	// make the request-level half fire once. EndpointLabel is
	// protocol:provider:model with no key component, so one provider's
	// several api_keys all share ONE label and a failover between two of
	// its keys matched twice, double-counting every request-level metric on
	// that row (caught by cmd/vmr/quota_parity_test.go's tokens case).
	reqAttributed := false
	for _, a := range attempts {
		k := rc.date + "\x00" + a.Endpoint
		e := st.eps[k]
		if e == nil {
			e = &EndpointRow{Date: rc.date, Endpoint: a.Endpoint}
			st.eps[k] = e
		}
		e.IngestAttempt(a)
		ea := st.epsAll[a.Endpoint]
		if ea == nil {
			ea = &EndpointRow{Endpoint: a.Endpoint}
			st.epsAll[a.Endpoint] = ea
		}
		ea.IngestAttempt(a)
		if a.Endpoint == rc.endpoint && !reqAttributed {
			reqAttributed = true
			e.IngestRequest(rc)
			ea.IngestRequest(rc)
		}
	}
}

// ingestSecondaryBuckets updates ByClient/Workloads/Sessions.
func (st *aggState) ingestSecondaryBuckets(rc *rec2) {
	// ByClient (skip empty tag - auth disabled / no match)
	if rc.clientKey != "" {
		c := st.byClient[rc.clientKey]
		if c == nil {
			c = &ClientRow{ClientKey: rc.clientKey}
			st.byClient[rc.clientKey] = c
		}
		c.Ingest(rc)
	}
	// Workloads
	wc := rc.workloadClass
	if wc == "" {
		wc = "interactive"
	}
	w := st.workloads[wc]
	if w == nil {
		w = &WorkloadRow{Class: wc}
		st.workloads[wc] = w
	}
	w.Ingest(rc)
	// Sessions (only grouped)
	if rc.sessionID != "" && st.sessionInfo[rc.sessionID] != nil {
		s := st.sessions[rc.sessionID]
		if s == nil {
			info := st.sessionInfo[rc.sessionID]
			s = &SessionRow{ID: info.ID, Alias: info.DisplayAlias, Title: info.Title, Tasks: len(info.Tasks),
				ContinuedFrom: info.ContinuedFrom, Class: wc, ClientKey: rc.clientKey}
			if len(info.Recs) > 0 {
				s.From = info.Recs[0].TS.Format(time.RFC3339)
				s.To = info.Recs[len(info.Recs)-1].TS.Format(time.RFC3339)
			}
			st.sessions[rc.sessionID] = s
		}
		s.Ingest(rc)
	}
}

// finishBuckets computes every bucket's derived fields (percentiles, cache
// efficiency, ...) and the buckets built post-hoc from the finished ones
// (Tools/Providers/ProviderQuotas/Compactions/Sticky/ClientEndpoints/
// Efficiency) — see metrics.go's finishX functions for the per-bucket math.
func (st *aggState) finishBuckets(pricingInfo *Pricing, quotas map[string][]ProviderQuotaRef, now time.Time, progress io.Writer) {
	rep := st.rep
	if !st.from.IsZero() {
		rep.Meta.From = st.from.Format(time.RFC3339)
		rep.Meta.To = st.to.Format(time.RFC3339)
	}

	finishRow(&rep.Overall)
	for _, r := range st.byModel {
		finishRow(r)
		rep.ByModel = append(rep.ByModel, *r)
	}
	for _, r := range st.byDate {
		finishRow(r)
		rep.ByDate = append(rep.ByDate, *r)
	}
	for _, h := range st.hours {
		finishHour(h)
		rep.Hours = append(rep.Hours, *h)
	}
	for _, h := range st.hoursOfDay {
		finishHour(h)
		rep.HoursOfDay = append(rep.HoursOfDay, *h)
	}
	for _, e := range st.eps {
		finishEndpoint(e)
		rep.Endpoints = append(rep.Endpoints, *e)
	}
	for _, e := range st.epsAll {
		finishEndpoint(e)
		rep.EndpointsAll = append(rep.EndpointsAll, *e)
	}
	for _, c := range st.byClient {
		finishClient(c)
		rep.ByClient = append(rep.ByClient, *c)
	}
	for _, w := range st.workloads {
		finishWorkload(w)
		rep.Workloads = append(rep.Workloads, *w)
	}
	for _, s := range st.sessions {
		finishSession(s, st.sessionInfo[s.ID])
		rep.Sessions = append(rep.Sessions, *s)
	}
	rep.Tools = buildTools(st.sess)
	rep.Providers = buildProviders(rep, quotas)
	rep.ProviderQuotas = buildProviderQuotaRows(rep, quotas, now, st.from, st.to)
	rep.Compactions = buildCompactions(st.sess)
	rep.Sticky = st.stickyCol.result()
	rep.ClientEndpoints = st.clientEndpointCol.result()
	if clients, rows := clientEndpointScale(rep.ClientEndpoints); progress != nil && rows > 0 {
		fmt.Fprintf(progress, "§5.5: %d client(s) x %d endpoint row(s)\n", clients, rows)
	}
	rep.Efficiency = buildFindingsForJSON(rep)
	rep.Pricing = pricingInfo
}

// sortBuckets makes every slice's order reproducible across runs. Every
// comparator below sorts primarily by a count/byte-size value that
// legitimately repeats across rows (two endpoints can both have exactly 1
// attempt; two sessions can both have exactly 2 requests). Each also
// appends the bucket's own identity field as a tie-break — Endpoint/
// ClientKey/Class/ID/Shape are each guaranteed unique within their own
// slice (they're literally what these rows were grouped by), so the
// comparator always returns a strict answer for two distinct rows. Without
// it, ties fell back to whatever order the slice already had — which
// itself comes from ranging over a Go map a few lines up (byModel/epsAll/
// byClient/... in aggState), an order the language spec deliberately does
// not guarantee is the same from one run to the next. The result: rows
// sharing a tied value would silently swap places between two otherwise-
// identical runs of the same binary against the same input (caught by
// TestBuildIsDeterministic — first noticed by comparing loadtest-report.md
// across two runs).
func (st *aggState) sortBuckets() {
	rep := st.rep
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
}
