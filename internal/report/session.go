// Ver 2026-09-23 03:25, by Pi Agent

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
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/reqdetail"
	"vmr/internal/taskseg"
)

// ReqInfo is the analysis result for one audit record: grouping coordinates
// plus rule-extracted features. Fields are best-effort — absent signals stay
// zero-valued.
type ReqInfo struct {
	// Facts is the canonical recordFacts extraction result this ReqInfo was
	// built from (nil on test-constructed instances).
	Facts *recordFacts

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
	// UsageInOK/UsageOutOK report which sides of the usage ledger the
	// upstream actually reported (see chatmsg.ExtractUsageSides for the
	// side rule). The old single UsageOK bool conflated the two; consumers
	// pick their side (In-side stats gate on UsageInOK, Out-side on
	// UsageOutOK).
	UsageInOK  bool
	UsageOutOK bool

	DetailFile string // deterministic detail filename (assigned in ts order)

	// Aggregates that the SessionRows / Workloads consumers need to roll up.
	RoleChars        map[string]int64 // per-role displayed-character totals
	RoleTokens       map[string]int64 // per-role estimated-token totals (tokenutil.Estimate)
	Fallbacks        int              // requests that needed >1 attempt
	Images           int              // inline request images detected
	ImagesCompressed int              // subset that triggered downscaling

	// working state (analysis only, dropped from JSON)
	//
	// manifest is this record's ctxgraph.Manifest, correlated by (Path,Line)
	// after ctxgraph.ScanCached runs — the message-hash vector, system-prompt
	// hash, and leading-system-message count all live there now; this
	// package no longer computes its own copy. nil for a record ctxgraph
	// couldn't build a Manifest for at all (body wasn't a parseable chat
	// object).
	manifest *ctxgraph.Manifest
	// prevManifest is the manifest immediately preceding this record's own
	// within its ctxgraph.Lineage (nil at a lineage's first manifest) —
	// the same "prev" internal/journey's Step.PrevManifest carries, so both
	// commands hand reqdetail the identical (m, prev) pair and render the
	// byte-identical page. Derived from the lineage directly, NOT from
	// the attached-record chain: a compaction-tagged record excluded from
	// s.Recs still counts here, exactly as it does on journey's side.
	prevManifest *ctxgraph.Manifest
	// realUsers: absolute msg idx → previewed real user instruction. Held for
	// every record in the corpus (SessionAnalysis keeps all ReqInfo), which is
	// why taskseg stores the preview rather than the raw text — see
	// taskseg.IndexRealUsers' doc comment.
	realUsers taskseg.RealUsers
	firstText string // first non-system message text (capped)
	respText  string // reassembled response content (compaction linking)
	// NoReply is true when the assistant's reply was empty or just "NO_REPLY"
	// (OpenClaw's skip-on-memory-flush pattern). Such records are typically
	// retried by the client a few minutes later; the retry carries the
	// user's actual instruction and is the one that gets processed. The
	// session analyzer treats NoReply parents as NOT opening a new task
	// boundary — the next record's "new instruction" is a retry of
	// the skipped one, not a fresh user intent.
	NoReply   bool
	realModel string // model segment of the final attempt's endpoint — assignNames' FileName input
	declBytes int64  // serialized size of the declared tools array
	attempts  int
	durMS     int64
	ttftMS    int64
	stream    bool
}

// SessionInfo is one grouped agent session.
type SessionInfo struct {
	// ID is the underlying ctxgraph.Lineage's content-addressed identity
	// (Lineage.LineageID(), "l-<hash8>") — a Session IS one Lineage (see
	// group's own doc comment), so it reuses that unit's identity rather
	// than inventing a run-scoped one. This is also what makes a report's
	// session row and a journey JourneyIndexRow.Lineages entry joinable by
	// set membership instead of a cross-command hash-and-compare (see
	// session grouping's own doc comment).
	ID string
	// DisplayAlias is the old s%02d positional label, kept purely for
	// human scannability within a single report (report readers already
	// use "s01"/"s02" to refer to sessions in conversation) — it carries
	// no identity role and must never be used as a lookup key.
	DisplayAlias   string
	Title          string
	ChatID         string
	ContinuedFrom  string // session id this one continues via compaction
	IsContinuation bool   // anchor is a compaction summary (link may be off-log)
	Recs           []*ReqInfo
	Tasks          []*TaskInfo

	segmenter *taskseg.Segmenter
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

// AnalyzeSessionsCached reads the audit files and produces the session
// grouping plus per-request features. Unparseable lines are skipped
// (Build counts them); records without a chat body land in Ungrouped. prior may be
// nil. prof is the taskseg.Profile extraction uses to recognize real user
// instructions, a deliberate no-reply skip, and a framework-specific chat_id —
// resolved once at cmd/vmr's composition root (see resolveTaskProfile), not
// decided independently by report and journey.
//
// The file-hash-keyed cache (ctxgraph.FileCache) covers both the
// ctxgraph.Manifests and report's own recordFacts. On a cache hit, analyzeFile
// reuses the cached facts and manifests with ZERO audit-file decoding; on a
// miss, a single pass decodes each line once to produce both Manifests and
// recordFacts.
func AnalyzeSessionsCached(paths []string, prior *ctxgraph.FileCache, prof taskseg.Profile) (*SessionAnalysis, *ctxgraph.FileCache, error) {
	if prof == nil {
		return nil, nil, errors.New("report: prof is nil")
	}
	if err := ctxgraph.CheckPathCollisions(paths); err != nil {
		return nil, nil, err
	}
	a := &SessionAnalysis{byKey: map[string]*ReqInfo{}}

	cache := &ctxgraph.FileCache{Files: make(map[string]ctxgraph.CachedFile, len(paths))}
	if prior != nil {
		for k, v := range prior.Files {
			cache.Files[k] = v
		}
	}

	results := make([]fileAnalysisResult, len(paths))
	sem := make(chan struct{}, analysisWorkerCount(len(paths)))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = analyzeFile(path, prior, prof)
		}(i, path)
	}
	wg.Wait()

	for _, res := range results {
		if res.err != nil {
			return nil, nil, res.err
		}
		for _, r := range res.recs {
			a.Recs = append(a.Recs, r)
			a.byKey[fmt.Sprintf("%s\x00%d", r.Path, r.Line)] = r
		}
		cache.Files[res.hash] = res.cf
	}

	g, cache, err := ctxgraph.ScanCached(paths, cache)
	if err != nil {
		return nil, nil, err
	}
	ctxgraph.StitchGraph(g)

	sort.SliceStable(a.Recs, func(i, j int) bool { return a.Recs[i].TS.Before(a.Recs[j].TS) })
	assignNames(a.Recs)
	group(a, g)
	linkCompactions(a)
	releaseTextBuffers(a)
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
	path      string
	hash      string
	recs      []*ReqInfo
	manifests []*ctxgraph.Manifest
	noBody    int
	cf        ctxgraph.CachedFile
	fromCache bool
	err       error
}

func hasNilManifest(ms []*ctxgraph.Manifest) bool {
	for _, m := range ms {
		if m == nil {
			return true
		}
	}
	return false
}

// analyzeFile resolves one file's analysis: on cache hit, it restores ReqInfo
// from cached facts without opening the audit file; on cache miss, it parses
// each record once to produce both Manifests and recordFacts.
func analyzeFile(path string, prior *ctxgraph.FileCache, prof taskseg.Profile) fileAnalysisResult {
	hash, err := ctxgraph.HashFile(path)
	if err != nil {
		return fileAnalysisResult{path: path, err: err}
	}
	if prior != nil {
		if cached, ok := prior.Files[hash]; ok &&
			cached.SchemaVersion == ctxgraph.CacheSchemaVersion &&
			cached.FactsVersion == FactsSchemaVersion &&
			!hasNilManifest(cached.Manifests) {
			if ff, ok := loadCachedFacts(prior, hash, prof); ok {
				cached.CanonicalPath = ctxgraph.CanonicalPath(path)
				recs := make([]*ReqInfo, 0, len(ff.Records))
				for i := range ff.Records {
					recs = append(recs, reqInfoFromFacts(path, &ff.Records[i]))
				}
				return fileAnalysisResult{
					path:      path,
					hash:      hash,
					recs:      recs,
					manifests: cached.Manifests,
					noBody:    cached.NoBody,
					cf:        cached,
					fromCache: true,
				}
			}
		}
	}

	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return fileAnalysisResult{path: path, hash: hash, err: err}
	}
	defer rc.Close()

	var manifests []*ctxgraph.Manifest
	var recs []*ReqInfo
	var ff fileFacts
	ff.Profile = prof.Name()
	noBody := 0
	line := 0
	scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
		line++
		var rec audit.Record
		if err := json.Unmarshal(lineBytes, &rec); err != nil {
			noBody++
			ff.ParseErrors++
			return
		}
		if m, ok := ctxgraph.BuildManifest(&rec, path, line); ok {
			m.Bytes = len(lineBytes)
			manifests = append(manifests, m)
		} else {
			noBody++
		}
		rf := extractRecordFacts(&rec, line, prof)
		ff.Records = append(ff.Records, rf)
		recs = append(recs, reqInfoFromFacts(path, &rf))
	}, func() {
		line++
		noBody++
		ff.ParseErrors++
	})
	if scanErr != nil {
		return fileAnalysisResult{path: path, hash: hash, err: fmt.Errorf("%s: %w", path, scanErr)}
	}

	ff.Version = FactsSchemaVersion
	factsData, err := json.Marshal(ff)
	if err != nil {
		return fileAnalysisResult{path: path, hash: hash, err: err}
	}

	cf := ctxgraph.CachedFile{
		Hash:          hash,
		SchemaVersion: ctxgraph.CacheSchemaVersion,
		FactsVersion:  FactsSchemaVersion,
		CanonicalPath: ctxgraph.CanonicalPath(path),
		Manifests:     manifests,
		NoBody:        noBody,
		Facts:         factsData,
	}

	return fileAnalysisResult{
		path:      path,
		hash:      hash,
		recs:      recs,
		manifests: manifests,
		noBody:    noBody,
		cf:        cf,
	}
}

// reqInfoFromFacts creates a ReqInfo view model backed by rf's canonical facts.
func reqInfoFromFacts(path string, rf *recordFacts) *ReqInfo {
	return &ReqInfo{
		Facts:            rf,
		Path:             path,
		Line:             rf.Line,
		TS:               rf.TS,
		Model:            rf.Model,
		Protocol:         rf.Protocol,
		Outcome:          rf.Outcome,
		ClientKeyTag:     rf.ClientKey,
		TraceID:          rf.TraceID,
		ChatID:           rf.ChatID,
		ToolsSig:         rf.ToolsSig,
		ToolsDeclared:    rf.ToolsDeclared,
		declBytes:        rf.DeclBytes,
		Tags:             rf.Tags,
		Compaction:       rf.Compaction,
		ToolCalls:        rf.ToolCalls,
		Finish:           rf.Finish,
		Truncated:        rf.Truncated,
		Usage:            rf.Usage,
		UsageInOK:        rf.UsageInOK,
		UsageOutOK:       rf.UsageOutOK,
		RoleChars:        rf.RoleChars,
		RoleTokens:       rf.RoleTokens,
		Fallbacks:        rf.Fallbacks,
		Images:           rf.Images,
		ImagesCompressed: rf.ImagesCompressed,
		realUsers:        rf.RealUsers,
		firstText:        rf.FirstText,
		respText:         rf.RespText,
		NoReply:          rf.NoReply,
		realModel:        rf.RealModel,
		attempts:         len(rf.Attempts),
		durMS:            rf.DurMS,
		ttftMS:           rf.TTFTMS,
		stream:           rf.Stream,
		Msgs:             rf.Msgs,
	}
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

// ---- grouping ----

// assignNames gives every record its deterministic detail filename, so
// detail export and the requests export agree on links. No batch-order
// state (an earlier design threaded a "used" collision-counter map through
// the batch) is needed any more: the
// name is keyed by this record's own coordinate hash
// (reqdetail.FileName/ctxgraph.ReqCoord), which is unique on its own.
func assignNames(recs []*ReqInfo) {
	for _, r := range recs {
		r.DetailFile = reqdetail.FileName(r.TS, r.Model, r.realModel, r.Outcome, ctxgraph.ReqCoord(r.Path, r.Line))
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
	prevByLoc := make(map[recLoc]*ctxgraph.Manifest)
	for _, l := range g.Lineages {
		for i, m := range l.Manifests {
			loc := recLoc{m.Path, m.Line}
			manifestByLoc[loc] = m
			lineageByLoc[loc] = l
			if i > 0 {
				prevByLoc[loc] = l.Manifests[i-1]
			}
		}
	}
	for _, m := range g.Ungrouped {
		manifestByLoc[recLoc{m.Path, m.Line}] = m
	}

	sessionOfLineage := make(map[int]*SessionInfo)
	var order []*SessionInfo
	// orderLineage[i] is the ctxgraph.Lineage order[i] was built from —
	// same append point as order itself, so the two slices stay in lock
	// step. Needed below to derive s.ID from the Lineage's own content-
	// addressed identity (LineageID) rather than the session's position
	// in this run's iteration order.
	var orderLineage []*ctxgraph.Lineage
	for _, r := range a.Recs {
		loc := recLoc{r.Path, r.Line}
		m := manifestByLoc[loc]
		r.manifest = m // nil when the body never parsed as a chat object
		// Compaction-tagged records keep their report-only, body-sniffed
		// treatment (a.Compactions, excluded from session grouping) but
		// still get their own manifest and lineage prev: detail rendering
		// depends on both, and internal/journey renders the same record with
		// the same pair — leaving these nil here used to make the two
		// commands render DIFFERENT pages for the same record.
		r.prevManifest = prevByLoc[loc]
		if r.Compaction {
			a.Compactions = append(a.Compactions, r)
			continue
		}
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
			orderLineage = append(orderLineage, lin)
		}
		attach(s, r)
	}
	for i, s := range order {
		s.ID = orderLineage[i].LineageID()
		s.DisplayAlias = fmt.Sprintf("s%02d", i+1)
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

// attach adds a record to a session: its delta boundary, task boundary, and
// instruction preview are computed by the canonical taskseg.Segmenter, ensuring
// compaction-tagged records are excluded as predecessors and the segmentation
// logic is identical between report and journey.
func attach(s *SessionInfo, r *ReqInfo) {
	if s.segmenter == nil {
		s.segmenter = taskseg.NewSegmenter()
	}
	var parent *ReqInfo
	if len(s.Recs) > 0 {
		parent = s.Recs[len(s.Recs)-1]
	}
	b := s.segmenter.Commit(taskseg.StepInput{
		Manifest:   r.manifest,
		RealUsers:  r.realUsers,
		TotalMsgs:  r.Msgs,
		NoReply:    r.NoReply,
		Compaction: r.Compaction,
	})
	r.Parent = parent
	r.DeltaStart = b.DeltaStart
	r.ReplacedTail = b.ReplacedTail
	r.SysChanged = b.SysChanged
	r.NewInstruction = b.Instruction

	s.Recs = append(s.Recs, r)
	r.SessSeq = len(s.Recs)
	if b.NewTask || len(s.Tasks) == 0 {
		s.Tasks = append(s.Tasks, &TaskInfo{Title: taskTitle(r)})
	}
	t := s.Tasks[len(s.Tasks)-1]
	t.Recs = append(t.Recs, r)
	r.TaskSeq = len(t.Recs)
}

// taskTitle resolves this task's title: r.NewInstruction (already
// taskseg.LastInstruction-derived) when non-empty, else a fallback —
// heartbeat first, then a generic placeholder. Not localized: this fallback
// is computed inside AnalyzeSessions, the one full-corpus pass report.Build
// deliberately runs only once (see build.go's own "two-read design"
// doc comment) — localizing it would mean re-running that whole pass a
// second time per language just for a rare placeholder string. See
// taskseg.TaskTitle's own doc comment for why it takes the fallback as a
// parameter instead of importing internal/i18n itself.
func taskTitle(r *ReqInfo) string {
	fallback := "(tool loop continuation)"
	if hasTag(r, "heartbeat") {
		fallback = "(heartbeat)"
	}
	return taskseg.TaskTitle(r.NewInstruction, fallback)
}

func sessionTitle(s *SessionInfo) string {
	// Earliest real instruction in the session's first request — the
	// conversation's opening ask, not the latest turn.
	if len(s.Recs) > 0 {
		if t := taskseg.FirstInstruction(s.Recs[0].realUsers); t != "" {
			return t
		}
	}
	for _, r := range s.Recs {
		if r.NewInstruction != "" {
			return r.NewInstruction
		}
	}
	if len(s.Recs) > 0 && s.Recs[0].firstText != "" {
		return taskseg.Preview(s.Recs[0].firstText)
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

// minPredNeedleRunes is the shortest first-instruction that can serve as
// evidence a compaction summarized a given session. A first instruction like
// "ok" or "continue" is a near-certain substring of any tens-of-KB
// compaction prompt, so anything shorter is treated as no needle at all
// (the "predecessor needle not found" log line still fires).
const minPredNeedleRunes = 12

// linkCompactions ties each compaction call to the session it summarized
// (its input quotes that session's first instruction) and to the session
// continuing from it (whose anchor embeds its output). Both are exact
// substring checks — no guessing; unmatched sides stay empty. The
// predecessor side additionally requires the quoted instruction to clear
// minPredNeedleRunes (see that const).
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
			if fn := needle(strings.TrimSpace(stripBracketPrefix(first.firstText))); utf8.RuneCountInString(fn) >= minPredNeedleRunes &&
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

// releaseTextBuffers drops the per-request text buffers (firstText capped at
// 512KiB, respText at 256KiB) that have already served every consumer by the
// time AnalyzeSessionsCached returns, keeping only the records that still
// need them afterwards: each session's first record (sessionTitle reads its
// firstText) and every compaction record (recextract.buildCompactions reads
// both texts). Everything else — the tens of thousands of intermediate
// requests in a large corpus — has no remaining reader, and each holds up to
// ~0.75MB of heap; leaving them resident is how a multi-GB spike becomes an
// OOM. linkCompactions (the last consumer of intermediate records' texts)
// has already run by the time this is called, so nothing is released early.
func releaseTextBuffers(a *SessionAnalysis) {
	keep := make(map[*ReqInfo]bool, len(a.Sessions)+len(a.Compactions))
	for _, s := range a.Sessions {
		if len(s.Recs) > 0 {
			keep[s.Recs[0]] = true
		}
	}
	for _, c := range a.Compactions {
		keep[c] = true
	}
	for _, r := range a.Recs {
		if !keep[r] {
			r.firstText, r.respText = "", ""
		}
	}
}
