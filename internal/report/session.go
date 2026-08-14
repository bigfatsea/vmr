// Ver 2026-07-29 22:30, by Sonnet 5

// Session analysis: group audit records into agent sessions → tasks → turns
// and extract per-request features, all offline and rule-based (no LLM).
// Method and evidence: design doc's "Agent 会话分析" section.
//
// The core signal is protocol-generic — agent clients resend the whole
// conversation each turn, so the first non-system message fingerprints the
// session and the longest common prefix (LCP) against a previous request
// isolates this turn's delta. Client-specific signals (Traceparent trace_id,
// OpenClaw wrapper templates, chat_id, Claude Code metadata.user_id) are
// used when present and silently skipped when not: a request that matches
// nothing still groups by the generic rule, it just carries fewer tags.
//
// Grouping itself is a thin consumer of
// internal/ctxgraph: AnalyzeSessions runs ctxgraph.Scan/StitchGraph over the
// same paths and uses its already-split Lineages as the one-session-per-
// Lineage grouping unit, and ctxgraph.Classify for each record's delta
// against its predecessor — replacing this package's own former private
// message-hash vector + LCP window search (the exact duplication design doc
// flagged: "同一个数据结构，被四个功能各自绕过"). Every OTHER feature
// collect() extracts (Tags, ToolsDeclared, RoleChars/Tokens, chat_id, NoReply,
// realUsers, …) stays exactly as it was — those are report-domain concerns
// ctxgraph has no reason to know about.
package report

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/taskseg"
)

// tailPrevKeep is how many trailing message previews each request retains,
// for rendering the parent's replaced tail. Replaced tails observed in real
// logs are 1-2 messages; beyond this window only counts are reported.
const tailPrevKeep = 8

// ReqInfo is the analysis result for one audit record: grouping coordinates
// plus rule-extracted features. Fields are best-effort — absent signals stay
// zero-valued.
type ReqInfo struct {
	// identity within the input set
	Path string
	Line int
	TS   time.Time

	Model, Protocol, Outcome string

	// ClientKeyTag is audit.Record.ClientKeyTag, copied verbatim: "" when
	// auth was disabled and the client sent no credential, or nothing
	// matched. Drives the by-tag sibling exports in export.go/detail.go —
	// see design doc's "按调用方分组导出" section.
	ClientKeyTag string

	// grouping
	SessKey      string // metadata session id or anchor hash; "" = ungrouped
	SessionID    string // "s01"… assigned after grouping
	TaskID       string // "t01"… within the session
	TaskSeq      int    // 1-based turn number within the task
	SessSeq      int    // 1-based turn number within the session
	Parent       *ReqInfo
	DeltaStart   int  // absolute message index where this request's new part begins
	Msgs         int  // total message count (incl. leading system)
	ReplacedTail int  // parent messages beyond the common prefix (replaced/edited)
	SysChanged   bool // system prompt differs from parent's

	// features
	TraceID        string
	ChatID         string
	ToolsSig       string // "tools:<n>/<hash8>"; "" when no tools field
	ToolsDeclared  []string
	Tags           []string
	Compaction     bool
	Summarizes     string // compaction only: session id it condensed
	ContinuesTo    string // compaction only: session id continuing from its output
	NewInstruction string // preview of the real user instruction in this delta
	ToolCalls      []string
	Finish         string
	Truncated      bool
	Usage          chatmsg.Usage
	UsageOK        bool

	DetailFile string // deterministic detail filename (assigned in ts order)

	// Aggregates that the SessionRows / Workloads consumers need to roll up.
	MessagesKnown    int              // requests whose body parsed as a chat object
	RoleChars        map[string]int64 // per-role displayed-character totals
	RoleTokens       map[string]int64 // per-role estimated-token totals (core.EstimateTextTokens)
	Fallbacks        int              // requests that needed >1 attempt
	Images           int              // inline request images detected
	ImagesCompressed int              // subset that triggered downscaling

	// working state (analysis only, dropped from JSON)
	//
	// manifest is this record's ctxgraph.Manifest, correlated by (Path,Line)
	// after ctxgraph.Scan runs — the message-hash vector, system-prompt
	// hash, and leading-system-message count all live there now; this
	// package no longer computes its own copy. nil
	// for a record ctxgraph couldn't build a Manifest for at all (body
	// wasn't a parseable chat object — same case collect() already bails
	// out of early, see the body-parse guard below).
	manifest  *ctxgraph.Manifest
	tailPrev  []string       // previews of the last tailPrevKeep messages
	realUsers map[int]string // absolute idx → preview, real user instructions
	firstText string         // first non-system message text (capped)
	respText  string         // reassembled response content (compaction linking)
	// NoReply is true when the assistant's reply was empty or just "NO_REPLY"
	// (OpenClaw's skip-on-memory-flush pattern). Such records are typically
	// retried by the client a few minutes later; the retry carries the
	// user's actual instruction and is the one that gets processed. The
	// session analyzer treats NoReply parents as NOT opening a new task
	// boundary — the next record's "new instruction" is a retry of
	// the skipped one, not a fresh user intent.
	NoReply   bool
	errClass  string // last attempt error class (filename suffix)
	realModel string // model segment of the final attempt's endpoint
	declBytes int64  // serialized size of the declared tools array
	endpoint  string // final attempt endpoint
	attempts  int
	durMS     int64
	ttftMS    int64
	stream    bool
}

// SessionInfo is one grouped agent session.
type SessionInfo struct {
	ID             string
	Title          string
	ChatID         string
	ContinuedFrom  string // session id this one continues via compaction
	IsContinuation bool   // anchor is a compaction summary (link may be off-log)
	Recs           []*ReqInfo
	Tasks          []*TaskInfo
}

// TaskInfo is one user-turn burst within a session.
type TaskInfo struct {
	ID    string
	Title string
	Recs  []*ReqInfo
}

// SessionAnalysis is the whole input set analyzed.
type SessionAnalysis struct {
	Recs        []*ReqInfo // ts order
	Sessions    []*SessionInfo
	Compactions []*ReqInfo
	Ungrouped   []*ReqInfo
	byKey       map[string]*ReqInfo // "path\x00line" lookup for render pass
}

// Lookup returns the analysis for the record at path:line, nil if unknown.
func (a *SessionAnalysis) Lookup(path string, line int) *ReqInfo {
	if a == nil {
		return nil
	}
	return a.byKey[fmt.Sprintf("%s\x00%d", path, line)]
}

// AnalyzeSessions reads the audit files and produces the session grouping
// plus per-request features. Unparseable lines are skipped (Build counts
// them); records without a chat body land in Ungrouped.
//
// Each file is read and collect()ed independently — collect() is a pure
// function of one record, with no shared mutable state across records or
// files (verified: every package-level var it reaches is a constant or a
// compiled *regexp.Regexp, both safe for concurrent use) — so the per-file
// work runs on a bounded worker pool (analysisWorkerCount goroutines)
// instead of strictly one file after another. This was measured as the
// single largest phase of `vmr report` on an 11-day/6663-record corpus
// (~48s), so it's the highest-value target for parallelizing this command.
//
// This is safe specifically because the only genuinely cross-record step —
// sort by timestamp, then assignNames/group/linkCompactions — still runs
// serially afterward, once every file's records are merged back together in
// original path order (and each file's own records stay in their original
// line order — ForEachLine within one file is untouched, still a plain
// sequential scan). A stable sort by TS over that merged, correctly-ordered
// slice produces byte-identical results to the old strictly-sequential
// version, tie-breaks included: parallelizing which file gets read first
// never changes what the final sort sees.
//
// ctxgraph.Scan/StitchGraph read the SAME paths independently, in a
// goroutine alongside collect()'s own pass —
// group() needs the resulting Graph to assign sessions by Lineage instead
// of collect()'s former private hash-vector grouping. Running both passes
// concurrently rather than back-to-back keeps this from roughly doubling
// `vmr report`'s wall-clock time on a large corpus, at the cost of
// transiently oversubscribing CPU (each pass already bounds its own worker
// pool to NumCPU) — an acceptable trade for a command that runs occasionally
// offline, not in a hot path.
//
// On error, every already-dispatched file still finishes reading (unlike
// the old version, which stopped at the first failing file) before the
// first error in path order is returned — wasted work on a path that's
// rare and not performance-sensitive, traded for not needing goroutine
// cancellation machinery.
// AnalyzeSessions is AnalyzeSessionsCached with no prior file-hash cache,
// always interpreting agent-dialect conventions (real-instruction/no-reply/
// chat_id detection — see collect()) through taskseg.OpenClawAware — every
// call re-runs ctxgraph.Scan's JSON-decode/message-hash pass on every input
// file, same as always. Kept as the stable entry point every existing
// caller/test already uses, the same "always the default profile" role
// Build itself plays relative to BuildCached (see build_cached.go's own doc
// comment on why); AnalyzeSessionsCached is the one Build's cached variant
// (BuildCached, see aggregate.go) actually calls, and the one that accepts a
// caller-chosen Profile.
func AnalyzeSessions(paths []string) (*SessionAnalysis, error) {
	a, _, err := AnalyzeSessionsCached(paths, nil, taskseg.OpenClawAware)
	return a, err
}

// AnalyzeSessionsCached is AnalyzeSessions plus a file-hash-keyed cache
// (ctxgraph.FileCache/ScanCached) for its ctxgraph.Scan pass — the
// analyzeFile pass just below (report's own, independent per-request
// parse into ReqInfo) is NOT cached by this: it still reparses every file
// on every call, same as AnalyzeSessions always has. See
// docs/VirtualModelRouter_Design_v4_Analytics.md's vmr-requests.json
// section for why only the ctxgraph.Manifest-based half is cached this
// round. prior may be nil (identical to AnalyzeSessions). prof is the
// taskseg.Profile collect() uses to recognize real user instructions, a
// deliberate no-reply skip, and a framework-specific chat_id — resolved
// once at cmd/vmr's composition root (see resolveTaskProfile), not decided
// independently by report and story.
func AnalyzeSessionsCached(paths []string, prior *ctxgraph.FileCache, prof taskseg.Profile) (*SessionAnalysis, *ctxgraph.FileCache, error) {
	a := &SessionAnalysis{byKey: map[string]*ReqInfo{}}

	var g *ctxgraph.Graph
	var cache *ctxgraph.FileCache
	var scanErr error
	var scanWG sync.WaitGroup
	scanWG.Add(1)
	go func() {
		defer scanWG.Done()
		g, cache, scanErr = ctxgraph.ScanCached(paths, prior)
		if scanErr == nil {
			ctxgraph.StitchGraph(g)
		}
	}()

	results := make([]fileAnalysisResult, len(paths))
	sem := make(chan struct{}, analysisWorkerCount(len(paths)))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = analyzeFile(path, prof)
		}(i, path)
	}
	wg.Wait()
	scanWG.Wait()
	if scanErr != nil {
		return nil, nil, scanErr
	}

	for _, res := range results {
		if res.err != nil {
			return nil, nil, res.err
		}
		for _, r := range res.recs {
			a.Recs = append(a.Recs, r)
			a.byKey[fmt.Sprintf("%s\x00%d", r.Path, r.Line)] = r
		}
	}

	sort.SliceStable(a.Recs, func(i, j int) bool { return a.Recs[i].TS.Before(a.Recs[j].TS) })
	assignNames(a.Recs)
	group(a, g)
	linkCompactions(a)
	return a, cache, nil
}

// analysisWorkerCount bounds how many files AnalyzeSessions reads
// concurrently: zstd decompression is CPU-bound, so more workers than
// cores (or than there are files to read) just adds scheduling overhead.
func analysisWorkerCount(files int) int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if files > 0 && n > files {
		n = files
	}
	if n < 1 {
		n = 1
	}
	return n
}

// fileAnalysisResult is one file's independently-collected records — see
// AnalyzeSessions for why computing this on its own goroutine is safe.
type fileAnalysisResult struct {
	recs []*ReqInfo
	err  error
}

// analyzeFile reads and collect()s every record in one audit file. Error
// formatting matches exactly what the old sequential AnalyzeSessions
// returned for each failure mode (OpenLogFile's own error already names the
// path; a scan error gets path-wrapped here) — callers must not wrap
// res.err again.
func analyzeFile(path string, prof taskseg.Profile) fileAnalysisResult {
	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return fileAnalysisResult{err: err}
	}
	defer rc.Close()
	var recs []*ReqInfo
	line := 0
	scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
		line++
		var rec audit.Record
		if err := json.Unmarshal(lineBytes, &rec); err != nil {
			return
		}
		recs = append(recs, collect(&rec, path, line, prof))
	}, func() { line++ }) // skipped oversized lines still advance the physical line number
	if scanErr != nil {
		return fileAnalysisResult{err: fmt.Errorf("%s: %w", path, scanErr)}
	}
	return fileAnalysisResult{recs: recs}
}

// ---- per-record collection ----

// collect extracts everything needed from one record while its parsed JSON
// is in hand; only compact metadata is retained. prof recognizes real user
// instructions, a deliberate no-reply skip, and a framework-specific
// chat_id — see taskseg.Profile.
func collect(rec *audit.Record, path string, line int, prof taskseg.Profile) *ReqInfo {
	r := &ReqInfo{
		Path: path, Line: line, TS: rec.TS,
		Model: rec.Model, Protocol: rec.Protocol, Outcome: rec.Outcome,
		ClientKeyTag: rec.ClientKeyTag,
		realUsers:    map[int]string{},
	}
	if tp := rec.Client.Request.Headers.Get("Traceparent"); tp != "" {
		if parts := strings.Split(tp, "-"); len(parts) >= 2 {
			r.TraceID = parts[1]
		}
	}
	for _, at := range rec.Attempts {
		if attemptErrorClass(at) == "truncated" && rec.Outcome == "ok" {
			r.Truncated = true
		}
	}
	r.errClass = errorClass(rec)
	r.realModel = realModel(rec)
	r.endpoint = lastEndpoint(rec)
	n, compressed := countImages(rec.Images)
	r.Images, r.ImagesCompressed = n, compressed
	r.attempts = len(rec.Attempts)
	if r.attempts > 1 {
		r.Fallbacks = 1
	}
	r.durMS, r.ttftMS, r.stream = rec.DurMS, rec.TTFTMS, rec.Stream
	// bytesIn/bytesOut are NOT computed here even though they're cheap to
	// derive from rec.Client.*.Body: nothing in this package ever reads
	// them off ReqInfo (Build's own pass computes its own copy, into rec2,
	// since that's the one that actually feeds the aggregate report) — this
	// used to duplicate that same json.Marshal-based sizing on every
	// record's full body for no reason.
	//
	// Attempt.Norm is likewise NOT pulled onto ReqInfo here: it's an
	// attempt-level, per-endpoint signal (which endpoint's response needed a
	// think_strip/soft_block quirk fix), not a per-request/session one — a
	// "last non-empty attempt" copy would silently drop an earlier failed
	// attempt's marker on any request with a failover chain. aggregate.go's
	// own addAttempt reads rec.Attempts[i].Norm directly, attributed to the
	// specific EndpointRow that attempt hit (EndpointRow.NormCounts).
	if rec.Client.Response != nil {
		r.Usage, r.UsageOK = chatmsg.ExtractUsage(rec.Client.Response.Body)
		if s := responseSummary(rec.Client.Response.Body); s != nil {
			r.Finish = s.Finish
			for _, tc := range s.ToolCalls {
				if tc.Name != "" {
					r.ToolCalls = append(r.ToolCalls, tc.Name)
				}
			}
			r.respText = fmtutil.CapStr(strings.TrimSpace(s.Content), 256<<10)
			// A deliberate no-reply skip (e.g. OpenClaw's empty-content or
			// explicit "NO_REPLY" marker convention — see prof.NoReply):
			// the record is sent successfully but the LLM skipped acting on
			// it — the next record carrying the same user instruction is a
			// retry of THIS one, not a new task.
			r.NoReply = prof.NoReply(r.Finish, r.respText)
		}
	}

	body, ok := rec.Client.Request.Body.(map[string]any)
	if !ok {
		return r
	}
	r.ToolsDeclared = chatmsg.ToolNames(body)
	if tools, hasTools := body["tools"]; hasTools || len(r.ToolsDeclared) > 0 {
		r.ToolsSig = toolsSig(r.ToolsDeclared)
		if raw, err := json.Marshal(tools); err == nil {
			r.declBytes = int64(len(raw))
		}
	}
	// SessKey (metadata.user_id, else "anchor:" + first non-system message
	// hash) is NOT computed here — group() sources it straight from this
	// record's correlated ctxgraph.Manifest.SessKey once ctxgraph.Scan has
	// run (one computation, not two).

	msgs := chatmsg.Messages(body) // anthropic system becomes message #0 — same shape both protocols
	r.Msgs = len(msgs)
	r.MessagesKnown = 1 // body parsed as chat object
	for role, c := range roleChars(body) {
		if r.RoleChars == nil {
			r.RoleChars = map[string]int64{}
		}
		r.RoleChars[role] += c
	}
	for role, t := range roleTokens(body) {
		if r.RoleTokens == nil {
			r.RoleTokens = map[string]int64{}
		}
		r.RoleTokens[role] += t
	}
	rawMsgs := chatmsg.RawArray(body)
	// leadSys mirrors ctxgraph.Manifest.LeadSys's definition (count of
	// contiguous leading role=="system" messages) — recomputed here as a
	// cheap, hash-free loop bound purely to skip that block in THIS loop;
	// nothing outside collect() reads it (the grouping code below reads
	// r.manifest.LeadSys instead, once correlated).
	leadSys := 0
	var lastUser string
	for i, m := range msgs {
		if m.Role == "system" && i == leadSys { // leading system block
			leadSys++
			continue
		}
		if r.firstText == "" {
			r.firstText = fmtutil.CapStr(m.Text, 512<<10)
		}
		if m.Role == "user" {
			lastUser = m.Text
			if text, ok := prof.RealUserText(m, rawMsgs, i-chatmsg.MsgOffset(body)); ok {
				r.realUsers[i] = preview(text)
			}
		}
	}
	for i := max(0, len(msgs)-tailPrevKeep); i < len(msgs); i++ {
		r.tailPrev = append(r.tailPrev, msgs[i].Role+": "+preview(msgs[i].Text))
	}

	r.ChatID = prof.ChatID(msgs)

	// Compaction: summarization system prompt, or the no-tools +
	// max_completion_tokens shape (three-signal compaction heuristic).
	_, hasMaxCT := body["max_completion_tokens"]
	sysText := ""
	if leadSys > 0 {
		sysText = msgs[0].Text
	}
	if strings.Contains(strings.ToLower(fmtutil.CapStr(sysText, 200)), "summarization") ||
		(len(r.ToolsDeclared) == 0 && hasMaxCT && r.TraceID == "") {
		r.Compaction = true
	}

	r.Tags = templateTags(r.firstText, lastUser, r.Compaction)
	return r
}

// templateTags classifies known message shapes. Unknown shapes get no tag —
// never a wrong one.
func templateTags(firstText, lastUser string, compaction bool) []string {
	var tags []string
	if compaction {
		tags = append(tags, "compaction")
	}
	if strings.Contains(fmtutil.CapStr(firstText, 200), "compacted into the following summary") {
		tags = append(tags, "compacted_session")
	}
	if strings.HasPrefix(firstText, "<conversation>") {
		tags = append(tags, "conversation_feed")
	}
	if strings.Contains(lastUser, "[OpenClaw heartbeat poll]") {
		tags = append(tags, "heartbeat")
	}
	if strings.Contains(lastUser, "Write a dream diary") {
		tags = append(tags, "dream_diary")
	}
	return tags
}

// toolsSig fingerprints a declared tool set: count plus name-list hash.
func toolsSig(names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	sum := md5.Sum([]byte(strings.Join(sorted, ",")))
	return fmt.Sprintf("tools:%d/%x", len(names), sum[:4])
}

// responseSummary reassembles a recorded client response body (SSE string or
// JSON object) into the model's output.
func responseSummary(body any) *chatmsg.StreamSummary {
	switch b := body.(type) {
	case string:
		return chatmsg.ReassembleSSE(b)
	case map[string]any:
		if s, ok := chatmsg.FinalMessage(b); ok {
			return s
		}
	}
	return nil
}

// ---- grouping ----

// assignNames gives every record its deterministic detail filename in ts
// order, so WriteDetails and the requests export agree on links.
func assignNames(recs []*ReqInfo) {
	used := map[string]int{}
	for _, r := range recs {
		r.DetailFile = detailFileNameFromInfo(r, used)
	}
}

// recLoc is the (path, line) coordinate both this package's ReqInfo and
// ctxgraph's Manifest key their records by — shared with
// session_conformance_test.go's cross-package grouping comparison.
type recLoc struct {
	Path string
	Line int
}

// group clusters records into sessions and segments tasks, using g's already
// -split Lineages as the grouping unit — one SessionInfo per Lineage,
// instead of this package's former per-SessKey bucketing that never split
// on a hidden Contract/Fork edit. r.
// Compaction-tagged records are pulled out into a.Compactions exactly as
// before — that's a report-only, body-sniffed concept ctxgraph doesn't
// share, orthogonal to lineage boundaries.
func group(a *SessionAnalysis, g *ctxgraph.Graph) {
	manifestByLoc := make(map[recLoc]*ctxgraph.Manifest)
	lineageByLoc := make(map[recLoc]*ctxgraph.Lineage)
	for _, l := range g.Lineages {
		for _, m := range l.Manifests {
			loc := recLoc{m.Path, m.Line}
			manifestByLoc[loc] = m
			lineageByLoc[loc] = l
		}
	}
	for _, m := range g.Ungrouped {
		manifestByLoc[recLoc{m.Path, m.Line}] = m
	}

	sessionOfLineage := make(map[int]*SessionInfo)
	var order []*SessionInfo
	for _, r := range a.Recs {
		if r.Compaction {
			a.Compactions = append(a.Compactions, r)
			continue
		}
		loc := recLoc{r.Path, r.Line}
		m := manifestByLoc[loc]
		r.manifest = m // nil when the body never parsed as a chat object
		if m == nil || m.SessKey == "" {
			a.Ungrouped = append(a.Ungrouped, r)
			continue
		}
		r.SessKey = m.SessKey
		lin := lineageByLoc[loc]
		s := sessionOfLineage[lin.Idx]
		if s == nil {
			s = &SessionInfo{}
			sessionOfLineage[lin.Idx] = s
			order = append(order, s)
		}
		attach(s, r)
	}
	for i, s := range order {
		s.ID = fmt.Sprintf("s%02d", i+1)
		for j, t := range s.Tasks {
			t.ID = fmt.Sprintf("t%02d", j+1)
		}
		for _, r := range s.Recs {
			r.SessionID = s.ID
		}
		for _, t := range s.Tasks {
			for _, r := range t.Recs {
				r.TaskID = t.ID
			}
		}
		s.Title = sessionTitle(s)
		s.ChatID = sessionChatID(s)
		s.IsContinuation = len(s.Recs) > 0 && hasTag(s.Recs[0], "compacted_session")
	}
	a.Sessions = order
	linkStitchedLineages(g, sessionOfLineage)
}

// linkStitchedLineages sets SessionInfo.ContinuedFrom from ctxgraph's own
// structural stitch resolution wherever a session's underlying Lineage broke
// away from an earlier one (BrokeFrom != nil) and was matched back to it
// with enough evidence (Stitch.Outcome == Stitched): this is what makes a
// hidden Contract/Fork split still render as "the same conversation,
// continued" instead of two unrelated sessions, something today's report
// couldn't even express before (a stitched pair used to BE one single
// SessionInfo, so there was nothing to link).
//
// This complements, not replaces, linkCompactions' text-based link for
// standalone compaction LLM calls: that one connects two sessions THROUGH a
// compaction record excluded from both (predecessor.Summarizes/
// ContinuesTo/successor.ContinuedFrom), a case ctxgraph's exact
// message-hash matching cannot always resolve — a full-history-rewrite
// compaction need not share a single verbatim message with its predecessor,
// so there is nothing in the blob index to stitch on (real corpus cases show
// the hash match that DOES work is against later, still-verbatim tool
// messages, not the summary text itself). linkCompactions runs after this
// and only fills ContinuedFrom where it's still empty, so it never clobbers
// a Stitch-derived link.
func linkStitchedLineages(g *ctxgraph.Graph, sessionOfLineage map[int]*SessionInfo) {
	for _, l := range g.Lineages {
		if l.Stitch == nil || l.Stitch.Outcome != ctxgraph.Stitched {
			continue
		}
		succ := sessionOfLineage[l.Idx]
		pred := sessionOfLineage[l.Stitch.Edge.PredIdx]
		if succ == nil || pred == nil || succ.ContinuedFrom != "" {
			continue // one side has no non-compaction records of its own, or already linked
		}
		succ.ContinuedFrom = pred.ID
	}
}

// attach adds a record to a session: its parent is the previous record
// already attached to this SAME session (== the same ctxgraph.Lineage,
// compaction-tagged records excluded), its delta boundary comes from
// ctxgraph.Classify against that parent's own manifest, and it opens a new
// task when warranted. Classify is called fresh on the two manifests rather
// than trusting Lineage.Edges' positional adjacency, so this stays correct
// even when a Compaction-tagged record (excluded from s.Recs) happens to sit
// between them in the raw manifest sequence.
func attach(s *SessionInfo, r *ReqInfo) {
	var parent *ReqInfo
	if len(s.Recs) > 0 {
		parent = s.Recs[len(s.Recs)-1]
	}
	newTask := parent == nil
	if parent != nil {
		p := parent
		e := ctxgraph.Classify(p.manifest, r.manifest)
		r.DeltaStart = r.manifest.LeadSys + e.LCP
		r.ReplacedTail = len(p.manifest.Keys) - e.LCP
		r.SysChanged = p.manifest.SysHash != r.manifest.SysHash
		r.Parent = p
		traceChanged := r.TraceID != "" && p.TraceID != "" && r.TraceID != p.TraceID
		// If the parent record ended in NO_REPLY (the LLM skipped its
		// reply), the user's instruction in this record is a RETRY of the
		// parent's skipped instruction, not a new user intent. We treat
		// the retry as the same task to keep task boundaries aligned with
		// actual user actions (a user types once → one task; multiple
		// records may result from retries / streaming re-sends).
		newTask = traceChanged || (!p.NoReply && r.deltaHasNewInstruction())
	} else {
		r.DeltaStart = 0 // whole request is "new" for the session's first record
	}
	r.NewInstruction = r.lastInstructionInDelta()

	s.Recs = append(s.Recs, r)
	r.SessSeq = len(s.Recs)
	if newTask || len(s.Tasks) == 0 {
		s.Tasks = append(s.Tasks, &TaskInfo{Title: taskTitle(r)})
	}
	t := s.Tasks[len(s.Tasks)-1]
	t.Recs = append(t.Recs, r)
	r.TaskSeq = len(t.Recs)
}

// deltaHasNewInstruction reports whether the delta contains a real user
// instruction near the request's end (see chatmsg.NewUserWindow).
//
// A message only counts as new if its content wasn't already present
// SOMEWHERE in the parent (not just its LCP-matched prefix). LCP diffing is
// positional: pruning or reordering earlier in the history breaks the prefix
// match at that point, so everything after it — including a real-user
// message the parent already had verbatim, just at a different offset —
// looks "new" by position alone. Without this check, a single instruction
// that survives a mid-task context prune reopens as a fresh 1-turn task
// quoting itself (observed in real logs after fixing RealUserText to see
// envelope-wrapped instructions: pruning shifted the same "OK，基于你…"
// message into the tail window a second time).
func (r *ReqInfo) deltaHasNewInstruction() bool {
	var parentKeys map[ctxgraph.Hash]bool
	if r.Parent != nil {
		parentKeys = make(map[ctxgraph.Hash]bool, len(r.Parent.manifest.Keys))
		for _, k := range r.Parent.manifest.Keys {
			parentKeys[k] = true
		}
	}
	for idx := range r.realUsers {
		if idx < r.DeltaStart || idx < r.Msgs-chatmsg.NewUserWindow {
			continue
		}
		if ki := idx - r.manifest.LeadSys; parentKeys != nil && ki >= 0 && ki < len(r.manifest.Keys) && parentKeys[r.manifest.Keys[ki]] {
			continue // identical content already existed in the parent — shifted, not new
		}
		return true
	}
	return false
}

// lastInstructionInDelta returns the preview of the newest real user
// instruction inside the delta; "" when this turn is a pure tool-loop step.
func (r *ReqInfo) lastInstructionInDelta() string {
	best := -1
	for idx := range r.realUsers {
		if idx >= r.DeltaStart && idx > best {
			best = idx
		}
	}
	if best < 0 {
		return ""
	}
	return r.realUsers[best]
}

func taskTitle(r *ReqInfo) string {
	if r.NewInstruction != "" {
		return r.NewInstruction
	}
	if hasTag(r, "heartbeat") {
		return "(heartbeat)"
	}
	// Not localized: this fallback is computed inside AnalyzeSessions, the
	// one full-corpus pass report.Build deliberately runs only once (see
	// aggregate.go's own "one file scan, not three" rationale) — localizing
	// it would mean re-running that whole pass a second time per language
	// just for a rare placeholder string. Kept in English always, same
	// treatment as the sibling "(heartbeat)" fallback right above it.
	return "(tool loop continuation)"
}

func sessionTitle(s *SessionInfo) string {
	// Earliest real instruction in the session's first request — the
	// conversation's opening ask, not the latest turn.
	if len(s.Recs) > 0 {
		first := s.Recs[0]
		best := -1
		for idx := range first.realUsers {
			if best < 0 || idx < best {
				best = idx
			}
		}
		if best >= 0 {
			return first.realUsers[best]
		}
	}
	for _, r := range s.Recs {
		if r.NewInstruction != "" {
			return r.NewInstruction
		}
	}
	if len(s.Recs) > 0 && s.Recs[0].firstText != "" {
		return preview(s.Recs[0].firstText)
	}
	// Not localized — see taskTitle's comment above; same reasoning applies.
	return "(untitled)"
}

func sessionChatID(s *SessionInfo) string {
	for _, r := range s.Recs {
		if r.ChatID != "" {
			return r.ChatID
		}
	}
	return ""
}

func hasTag(r *ReqInfo, tag string) bool {
	for _, t := range r.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// ---- compaction linking ----

// linkCompactions ties each compaction call to the session it summarized
// (its input quotes that session's first instruction) and to the session
// continuing from it (whose anchor embeds its output). Both are exact
// substring checks — no guessing; unmatched sides stay empty.
//
// Deliberately still a text-needle match, not a ctxgraph.Stitch lookup: a
// standalone compaction LLM call's own
// input/output need not share a single verbatim message with the sessions
// on either side of it (a full-history-rewrite compaction has nothing for
// ctxgraph's exact-hash blob index to match against), so this stays as the
// complementary signal for that case. group()'s linkStitchedLineages already
// ran and may have set some sessions' ContinuedFrom from real structural
// evidence (a hidden Contract/Fork same-lineage break) — this function only fills
// ContinuedFrom where it's still empty, so it can never clobber that.
func linkCompactions(a *SessionAnalysis) {
	for _, c := range a.Compactions {
		out := needle(c.respText)
		in := c.firstText
		var successor, predecessor *SessionInfo
		for _, s := range a.Sessions {
			if len(s.Recs) == 0 {
				continue
			}
			first := s.Recs[0]
			if out != "" && strings.Contains(first.firstText, out) &&
				!first.TS.Before(c.TS) &&
				(successor == nil || first.TS.Before(successor.Recs[0].TS)) {
				successor = s
			}
			if fn := needle(strings.TrimSpace(stripBracketPrefix(first.firstText))); fn != "" &&
				strings.Contains(in, fn) && first.TS.Before(c.TS) &&
				(predecessor == nil || first.TS.After(predecessor.Recs[0].TS)) {
				predecessor = s
			}
		}
		if successor != nil {
			c.ContinuesTo = successor.ID
		} else if out != "" {
			// Distinguishes "genuinely no continuation" from "the 200-byte
			// needle missed" — both leave ContinuesTo empty, and only a log
			// line tells them apart during triage.
			log.Printf("report: compaction linking: successor needle not found for compaction at %s (%s)", c.TS.Format(time.RFC3339), c.Path)
		}
		if predecessor != nil {
			c.Summarizes = predecessor.ID
		} else if in != "" {
			log.Printf("report: compaction linking: predecessor needle not found for compaction at %s (%s)", c.TS.Format(time.RFC3339), c.Path)
		}
		if successor != nil && predecessor != nil && successor != predecessor && successor.ContinuedFrom == "" {
			successor.ContinuedFrom = predecessor.ID
		}
	}
}

// needle caps a containment probe at a length that stays cheap but is far
// beyond accidental-collision territory.
func needle(s string) string {
	s = strings.TrimSpace(s)
	return fmtutil.CapStr(s, 200)
}

// stripBracketPrefix removes a leading "[…] " block (OpenClaw's injected
// timestamp/channel prefix) so instruction text matches across rewrites.
func stripBracketPrefix(s string) string {
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "] "); i >= 0 && i < 120 {
			return s[i+2:]
		}
	}
	return s
}

// ---- filename (shared with detail.go) ----

// detailFileNameFromInfo mirrors detailFileName for the analysis pass, which
// only has the features captured in collect (not the full record). Endpoint/
// error class come from those captured features; both passes therefore
// produce identical names.
func detailFileNameFromInfo(r *ReqInfo, used map[string]int) string {
	outcome := r.Outcome
	if outcome == "error" && r.errClass != "" {
		outcome += "-" + r.errClass
	}
	base := fmt.Sprintf("%s_%s_%s_%s",
		r.TS.In(fmtutil.DisplayZone).Format("20060102-150405.000"),
		sanitizeName(displayModelName(r.Model)), sanitizeName(r.realModel), sanitizeName(outcome))
	used[base]++
	if n := used[base]; n > 1 {
		base = fmt.Sprintf("%s-%d", base, n)
	}
	return base + ".md"
}

func displayModelName(model string) string {
	if model == "" {
		return "(rejected)"
	}
	return model
}
