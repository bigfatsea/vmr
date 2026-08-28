// Ver 2026-07-29 23:30, by Sonnet 5

// Package story turns one internal/ctxgraph.Lineage into a readable
// narrative: a sequence of user-instruction Tasks, each a sequence of
// request/response Steps, plus a globally de-duplicated Event stream
// (reading only the final request's message list misses 26%-99% of what
// actually happened; the event stream is built by walking every step and
// keeping only each message's FIRST appearance).
//
// Each Journey represents a single Lineage (no cross-lineage stitching)
// yet (that's phase 2). A Lineage that starts
// mid-conversation (BrokeFrom != nil) is rendered with an explicit "context
// was rebuilt here, not yet reconnected" notice rather than silently
// treated as a fresh start.
package story

import (
	"crypto/md5"
	"encoding/json"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// Journey is a stitched chain of one or more Lineages rendered as one
// continuous narrative. Chain is oldest
// lineage first, exactly as ctxgraph.ChainFrom returns it — Step 1's "one
// Lineage, one Journey" is the len(Chain)==1 degenerate case, not a
// separate code path.
type Journey struct {
	ID       string
	Partial  bool // head-truncated — see BuildAll
	Title    string
	From, To time.Time
	Tasks    []*Task
	Events   []*Event // full de-duped event stream, first-appearance order
	// Break is Chain[0]'s own BrokeFrom — i.e., even after best-effort
	// stitching (ctxgraph.StitchGraph), THIS journey's beginning is still
	// an unresolved break (StitchOutcome NoPredecessorFound or
	// AmbiguousMatch). nil when Chain[0] opened its bucket cleanly, or was
	// itself successfully stitched onto an earlier lineage (in which case
	// ChainFrom would have included that predecessor, making IT Chain[0]).
	Break *ctxgraph.BreakInfo
	Chain []*ctxgraph.Lineage
}

// JourneyReportFile is the single source of truth for a journey report's
// markdown filename - cmd/vmr's file writer and compare's link rendering
// must agree on it, and a private second copy would drift (a silent bad
// link reads exactly like a working one). The .json sibling shares the
// stem.
func JourneyReportFile(id string, partial bool) string {
	base := "journey-" + id
	if partial {
		base += "-partial"
	}
	return base + ".md"
}

// Task is one user-instruction burst within a Journey.
type Task struct {
	Title string
	Steps []*Step
}

// Step is one request/response turn.
type Step struct {
	Seq        int // 1-based, across the whole Journey
	Manifest   *ctxgraph.Manifest
	Rec        *audit.Record
	Edge       *ctxgraph.Edit // nil for the Journey's first step, or at a stitch boundary
	DeltaStart int            // absolute message index where this step's new content begins
	NewEvents  []*Event       // events first introduced by this step, in order
	// PrevManifest is the immediately preceding Manifest in this Step's OWN
	// ctxgraph.Lineage — nil for a Lineage's first Step, including at a
	// stitch boundary (StitchEdge != nil). Deliberately NOT the predecessor
	// lineage's tail Manifest at a stitch boundary, even though buildFrom
	// computes and uses that value locally for sysChanged/CompactionInfo:
	// internal/report's session grouping (session.go's group/attach) is
	// strictly per-Lineage too, so a Lineage's first record always has
	// ReqInfo.Parent == nil on that side — this field exists so
	// reqdetail.EnsureRendered's "prev" argument can agree with report's,
	// keeping the two commands' generated detail pages byte-identical for
	// the same record.
	PrevManifest *ctxgraph.Manifest
	// StitchEdge is non-nil exactly when this Step is the first manifest of
	// a non-first Lineage in the Journey's Chain — the evidence
	// ctxgraph.StitchGraph found connecting it to the previous Lineage.
	// DeltaStart is always 0 for such a Step (the whole manifest is scanned
	// for new events; the global seen-hash dedup — not a computed LCP delta
	// — is what correctly suppresses content the predecessor already
	// showed, since Classify's structural LCP has no meaning across a
	// stitch boundary).
	StitchEdge *ctxgraph.StitchEdge
	// SysChanged is true when this manifest's leading system block differs
	// from the logically-preceding manifest's (the previous manifest in the
	// same lineage, or — at a stitch boundary — the predecessor lineage's
	// last manifest). System prompt changes are their own analysis-worthy
	// event (model switch, tool-set change, platform injection change)
	// independent of whatever edit classification the message-content
	// transition got.
	SysChanged bool
	// Compaction is non-nil only at a stitch boundary (StitchEdge != nil) —
	// the information-loss summary (CCR N-4's promise) calls for: token
	// count before/after, and which identifiable entities
	// (file paths, URLs) mentioned in the swallowed predecessor content
	// stopped being mentioned versus which survived into this step.
	Compaction *CompactionInfo
	// HumanInitiated is true when this Step's OWN opening carries a
	// genuinely new real user instruction (taskseg.HasNewInstruction, or the
	// dedup-aware equivalent at a stitch boundary — see
	// newInstructionTitleAtStitch) — as opposed to a pure tool-loop
	// continuation, a trace-id change, or a stitch boundary with nothing
	// new to say. Metrics' F10 gap classification uses this to tell "the
	// human went quiet and came back" apart from "the agent
	// kept working on its own" for the gap immediately BEFORE this step.
	// True for the Journey's very first step by construction — every
	// Journey opens on a real instruction.
	HumanInitiated bool
	// Instruction is this Step's triggering real-user instruction, already
	// filtered through prof.RealUserText and preview-truncated (see
	// taskseg.LastInstruction/Preview) — "" when the Step isn't
	// HumanInitiated (or is the Journey's first step, whose instruction is
	// already shown via the Task title instead) or the delta range had no
	// real instruction. Computed once in buildFrom from the same `ru` index
	// newTask's title derivation already uses, not re-derived from raw
	// NewEvents text.
	Instruction string

	Finish    string
	NoReply   bool
	ToolCalls []chatmsg.ToolCall
	RespText  string
	// Reasoning is the response's thinking/reasoning-content block, when the
	// provider reported one (chatmsg.StreamSummary.Reasoning) — "" when
	// absent, not when-a-provider-doesn't-support-it vs. empty-this-turn are
	// not distinguished, same convention RespText already uses.
	Reasoning string
}

// CompactionInfo is the information-loss summary attached to a stitch
// boundary Step. Purely rule-derived — token counts come
// straight from recorded Usage, entities from a rough regex scan (file
// paths, URLs) over the predecessor's last rendered manifest vs this step's
// own — "宁可粗糙也不猜语义": this doesn't try to understand what was lost,
// only to point at it so a human can look.
type CompactionInfo struct {
	TokensBefore, TokensAfter int64
	SwallowedEntities         []string // seen in the predecessor's last manifest, absent from this step
	SurvivedEntities          []string // seen in the predecessor's last manifest, still present in this step
	PredecessorTextExcerpt    string   // Phase 1b: predecessor message text excerpt (<=3000 runes) for constraint loss analysis
}

// Event is one message's first appearance anywhere in the Journey.
type Event struct {
	Hash         ctxgraph.Hash
	Msg          chatmsg.Message
	FirstStepSeq int
	// Revises is non-nil when this Event's message sits exactly at a
	// ctxgraph.Splice edge's divergence point — the "revision" relation:
	// without this, the global seen-hash dedup would render the
	// rewritten message as a brand-new, unrelated Event, reading as if the
	// same thing got said twice instead of the earlier one being replaced
	// in place. The referenced Hash is the message it replaces — usually
	// already rendered as an earlier Event, so callers can cross-reference.
	Revises *ctxgraph.Hash
}

// Build renders one Lineage into a Journey using prof for the
// agent-specific real-instruction/no-reply judgment calls, WITHOUT
// stitching — the degenerate len(Chain)==1 case of BuildChain, kept as its
// own entry point for callers previewing/testing a single lineage in
// isolation. Rendering a lineage's actual stitched chain needs
// BuildChain(ctxgraph.ChainFrom(l, byIdx), prof) instead.
func Build(l *ctxgraph.Lineage, prof taskseg.Profile, lang i18n.Lang) (*Journey, error) {
	return BuildChain([]*ctxgraph.Lineage{l}, prof, lang)
}

// BuildChain renders a full stitched chain — oldest lineage first, exactly
// as ctxgraph.ChainFrom returns it — into one continuous Journey. It
// re-fetches every manifest's full audit.Record (a Lineage on its own only
// carries content hashes) in one batched pass per source file across the
// WHOLE chain. Rendering many chains at once should use BuildAll instead —
// calling BuildChain in a loop re-fetches from scratch per chain, which
// re-scans a source file once per chain touching it instead of once total.
func BuildChain(chain []*ctxgraph.Lineage, prof taskseg.Profile, lang i18n.Lang) (*Journey, error) {
	if prof == nil {
		return nil, errNilProfile
	}
	if len(chain) == 0 {
		return nil, errEmptyLineage
	}
	for _, l := range chain {
		if len(l.Manifests) == 0 {
			return nil, errEmptyLineage
		}
	}
	var locs []ctxgraph.Loc
	for _, l := range chain {
		locs = append(locs, manifestLocs(l)...)
	}
	recs, err := ctxgraph.FetchRecords(locs)
	if err != nil {
		return nil, err
	}
	return buildFrom(chain, prof, recs, lang)
}

// BuildAll renders many stitched chains into Journeys. Two independent costs
// are batched/parallelized here, both identified by measuring a real
// -render-all run rather than guessed at:
//
// 1. I/O: every chain's manifest-record fetch is batched into a single
// ctxgraph.FetchRecords call — FetchRecords already groups its reads by
// source file (zstd isn't seekable, so each file is scanned at most
// once regardless of how many lines are wanted from it), turning "read
// every candidate's records" from one pass over the source files PER
// CANDIDATE into one pass total, same fix PreviewTitles applied to the
// listing path.
// 2. CPU: buildFrom's own work (re-rendering each manifest's full message
// list, event-hash dedup, jsonIndent on tool payloads) turned out to be
// the larger cost — a single request's body already carries its ENTIRE
// accumulated history, so buildFrom's cost per lineage grows with the
// square of its turn count, not linearly, and doing that serially leaves
// every candidate's CPU work on one core. Each chain's Journey is
// independent of
// every other's (buildFrom only reads the shared recs map, never
// mutates it), so this runs on the same bounded worker pool
// scanWorkerCount uses in internal/ctxgraph.
//
// Order of the returned slice matches chains; a per-chain error aborts the
// whole batch (matches BuildChain's own all-or-nothing contract for a
// single chain).
func BuildAll(chains [][]*ctxgraph.Lineage, prof taskseg.Profile, lang i18n.Lang) ([]*Journey, error) {
	if prof == nil {
		return nil, errNilProfile
	}
	for _, chain := range chains {
		if len(chain) == 0 {
			return nil, errEmptyLineage
		}
		for _, l := range chain {
			if len(l.Manifests) == 0 {
				return nil, errEmptyLineage
			}
		}
	}

	var locs []ctxgraph.Loc
	for _, chain := range chains {
		for _, l := range chain {
			locs = append(locs, manifestLocs(l)...)
		}
	}
	recs, err := ctxgraph.FetchRecords(locs)
	if err != nil {
		return nil, err
	}

	out := make([]*Journey, len(chains))
	errs := make([]error, len(chains))
	sem := make(chan struct{}, buildWorkerCount(len(chains)))
	var wg sync.WaitGroup
	for i, chain := range chains {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, chain []*ctxgraph.Lineage) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i], errs[i] = buildFrom(chain, prof, recs, lang)
		}(i, chain)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

// buildWorkerCount bounds Journey-construction concurrency the same way
// ctxgraph's scanWorkerCount does: more workers than cores (or than
// chains) just adds scheduling overhead, not throughput.
func buildWorkerCount(chains int) int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if chains > 0 && n > chains {
		n = chains
	}
	if n < 1 {
		n = 1
	}
	return n
}

// manifestLocs is l's manifests' source coordinates, in order — the batch
// FetchRecords needs to resolve them to full audit.Records.
func manifestLocs(l *ctxgraph.Lineage) []ctxgraph.Loc {
	locs := make([]ctxgraph.Loc, len(l.Manifests))
	for i, m := range l.Manifests {
		locs[i] = ctxgraph.Loc{Path: m.Path, Line: m.Line}
	}
	return locs
}

// stepContinuation resolves buildFrom's i > 0, non-stitch-boundary case: the
// applied Edit, delta range, previous Manifest, whether this step opens a
// new Task, whether it's HumanInitiated, its filtered+preview-truncated
// triggering instruction (Step.Instruction, "" when not HumanInitiated),
// and the "revision" relation for a Splice edge's divergence point — split
// out of buildFrom purely to stay under the architecture review's
// function-length budget.
func stepContinuation(l *ctxgraph.Lineage, i int, m *ctxgraph.Manifest, ru taskseg.RealUsers, msgs []chatmsg.Message, prevNoReply bool) (edge *ctxgraph.Edit, deltaStart int, prevManifest *ctxgraph.Manifest, newTask, humanInitiated bool, instr string, revisesHash *ctxgraph.Hash) {
	e := l.Edges[i-1]
	edge = &e
	deltaStart = m.LeadSys + e.LCP
	prevManifest = l.Manifests[i-1]
	traceChanged := m.TraceID != "" && prevManifest.TraceID != "" && m.TraceID != prevManifest.TraceID
	hasNewInstr := taskseg.HasNewInstruction(ru, taskseg.ManifestKeySet(prevManifest), m, deltaStart, len(msgs))
	newTask = taskseg.IsNewTask(traceChanged, prevNoReply, hasNewInstr)
	humanInitiated = hasNewInstr
	if humanInitiated {
		instr = taskseg.LastInstruction(ru, deltaStart)
	}
	// The "revision" relation: a Splice edge's divergence point
	// (prevManifest.Keys[e.LCP]) is a message being rewritten in place, not
	// a coincidental new one — attached to the first NewEvent below, so it
	// doesn't render as "the same thing said twice".
	if e.Kind == ctxgraph.Splice && e.LCP < len(prevManifest.Keys) {
		h := prevManifest.Keys[e.LCP]
		revisesHash = &h
	}
	return
}

// buildFrom is BuildChain's actual assembly logic, factored out so BuildAll
// can share one batched recs lookup across many chains instead of each
// chain doing its own FetchRecords call. chain is oldest-lineage-first
// (ctxgraph.ChainFrom's contract); every Lineage after chain[0] is joined
// at a stitch boundary — the first manifest of chain[c] for c>0 — using
// that Lineage's own Stitch.Edge as the evidence (guaranteed non-nil:
// ChainFrom only walks through Stitched outcomes).
func buildFrom(chain []*ctxgraph.Lineage, prof taskseg.Profile, recs map[ctxgraph.Loc]*audit.Record, lang i18n.Lang) (*Journey, error) {
	head, tail := chain[0], chain[len(chain)-1]
	j := &Journey{
		ID:    deriveID(chain),
		Break: head.BrokeFrom,
		Chain: chain,
		From:  head.Manifests[0].TS,
		To:    tail.Manifests[len(tail.Manifests)-1].TS,
	}

	seen := map[ctxgraph.Hash]*Event{}
	var curTask *Task
	seq := 0
	prevNoReply := false
	// firstRu is the earliest-processed step's RealUsers index — j.Tasks[0]
	// is always built from this same step (newTask fires unconditionally on
	// the first non-skipped iteration below), so deriveTitle reads it back
	// instead of re-parsing that step's request body and re-running
	// RealUserText a second time over it.
	var firstRu taskseg.RealUsers
	firstRuSet := false
	for ci, l := range chain {
		for i, m := range l.Manifests {
			rec := recs[ctxgraph.Loc{Path: m.Path, Line: m.Line}]
			if rec == nil {
				continue // defensive: FetchRecords silently drops a line it couldn't parse a second time
			}
			body, _ := rec.Client.Request.Body.(map[string]any)
			msgs := chatmsg.Messages(body)
			rawMsgs := chatmsg.RawArray(body)
			off := chatmsg.MsgOffset(body)
			// Built once per step and threaded into HasNewInstruction/
			// LastInstruction/newInstructionTitleAtStitch below instead of
			// each re-scanning msgs itself — this manifest's real-instruction
			// regex work used to run up to 2-3 times per step before B3.
			ru := taskseg.IndexRealUsers(prof, msgs, rawMsgs, off)
			if !firstRuSet {
				firstRu, firstRuSet = ru, true
			}

			atStitchBoundary := ci > 0 && i == 0
			var edge *ctxgraph.Edit
			var stitchEdge *ctxgraph.StitchEdge
			var compaction *CompactionInfo
			var prevManifest *ctxgraph.Manifest
			// stepPrevManifest becomes Step.PrevManifest — unlike
			// prevManifest above (which sysChanged/buildCompactionInfo
			// legitimately want compared across a stitch boundary too),
			// this one stays nil at a stitch boundary; see Step.PrevManifest's
			// doc comment for why.
			var stepPrevManifest *ctxgraph.Manifest
			var revisesHash *ctxgraph.Hash
			deltaStart := 0
			// A stitch boundary is NOT automatically a new Task — only a
			// genuinely new instruction bridged across it is (decided in the
			// atStitchBoundary arm below). A mid-task compaction stays in
			// curTask; its Step still carries StitchEdge/Compaction, which
			// the renderers surface inline regardless of task position. This
			// matches taskseg.IsNewTask's "new instruction or new trace"
			// rule instead of inflating len(j.Tasks) with every compaction
			// (B9).
			newTask := ci == 0 && i == 0
			// Default: true only for the Journey's very first step (every
			// Journey opens on a real instruction, by construction). Both
			// branches below override this for their
			// own cases; a plain tool-loop continuation (neither branch
			// fires) correctly stays false.
			humanInitiated := ci == 0 && i == 0
			// instr becomes Step.Instruction — stays "" for the Journey's
			// first step and at a stitch boundary (both cases' instruction
			// is already shown via the Task title, see Step.Instruction's
			// doc comment); only the i > 0 case below computes it.
			var instr string

			switch {
			case atStitchBoundary:
				stitchEdge = l.Stitch.Edge
				predLineage := chain[ci-1]
				prevManifest = predLineage.Manifests[len(predLineage.Manifests)-1]
				if predRec := recs[ctxgraph.Loc{Path: prevManifest.Path, Line: prevManifest.Line}]; predRec != nil {
					compaction = buildCompactionInfo(predRec, prevManifest, m, msgs)
				}
				if newInstructionTitleAtStitch(ru, m, msgs, rawMsgs, off, seen) != "" {
					newTask = true // a real new instruction bridged the stitch — see B9 note above
				}
				// deltaStart stays 0: Classify's structural LCP has no
				// meaning across a stitch boundary, so the whole manifest
				// is scanned — the global seen-hash dedup below (not a
				// computed delta) is what correctly suppresses content the
				// predecessor already showed.
			case i > 0:
				edge, deltaStart, prevManifest, newTask, humanInitiated, instr, revisesHash =
					stepContinuation(l, i, m, ru, msgs, prevNoReply)
				stepPrevManifest = prevManifest
			}
			sysChanged := prevManifest != nil &&
				(m.HasSys != prevManifest.HasSys || (m.HasSys && prevManifest.HasSys && m.SysHash != prevManifest.SysHash))

			if newTask || curTask == nil {
				var title string
				if atStitchBoundary {
					title, humanInitiated = titleAtStitchBoundary(ru, m, msgs, rawMsgs, off, seen, stitchEdge, lang)
				} else {
					title = taskseg.TaskTitle(taskseg.LastInstruction(ru, deltaStart), i18n.Story(lang).ToolLoopTitle)
				}
				curTask = &Task{Title: title}
				j.Tasks = append(j.Tasks, curTask)
			}

			seq++
			step := buildStep(seq, m, rec, edge, stitchEdge, sysChanged, compaction, deltaStart, humanInitiated, instr, stepPrevManifest, prof)
			prevNoReply = step.NoReply
			appendNewEvents(j, step, m, msgs, rawMsgs, off, deltaStart, revisesHash, seen)
			curTask.Steps = append(curTask.Steps, step)
		}
	}

	j.Title = deriveTitle(firstRu, j.Tasks, lang)
	return j, nil
}

// buildStep assembles one Step from its manifest/record plus buildFrom's
// already-resolved edit/stitch/compaction context for this iteration,
// filling in the response-derived fields (finish reason, reply text, tool
// calls, reasoning, NoReply) when the record has a response — split out of
// buildFrom purely to stay under the architecture review's function-length
// budget, not because it's an independently meaningful step.
func buildStep(seq int, m *ctxgraph.Manifest, rec *audit.Record, edge *ctxgraph.Edit, stitchEdge *ctxgraph.StitchEdge, sysChanged bool, compaction *CompactionInfo, deltaStart int, humanInitiated bool, instr string, prevManifest *ctxgraph.Manifest, prof taskseg.Profile) *Step {
	step := &Step{Seq: seq, Manifest: m, Rec: rec, Edge: edge, StitchEdge: stitchEdge,
		SysChanged: sysChanged, Compaction: compaction, DeltaStart: deltaStart,
		HumanInitiated: humanInitiated, Instruction: instr, PrevManifest: prevManifest}
	if rec.Client.Response != nil {
		if s := taskseg.ResponseSummary(rec.Client.Response.Body); s != nil {
			step.Finish = s.Finish
			step.RespText = strings.TrimSpace(s.Content)
			step.ToolCalls = s.ToolCalls
			step.Reasoning = strings.TrimSpace(s.Reasoning)
		}
		step.NoReply = prof.NoReply(step.Finish, step.RespText)
	}
	return step
}

// appendNewEvents scans step's manifest from deltaStart, appending each
// not-yet-seen message (by content hash) as a new Event to both
// step.NewEvents and j.Events — the global first-appearance dedup that
// makes a Journey's Events stream show each distinct message exactly once,
// regardless of how many later Steps' history still carries it.
func appendNewEvents(j *Journey, step *Step, m *ctxgraph.Manifest, msgs []chatmsg.Message, rawMsgs []any, off, deltaStart int, revisesHash *ctxgraph.Hash, seen map[ctxgraph.Hash]*Event) {
	for idx := deltaStart; idx < len(msgs); idx++ {
		h := eventHashAt(m, msgs, rawMsgs, off, idx)
		if _, dup := seen[h]; dup {
			continue
		}
		ev := &Event{Hash: h, Msg: msgs[idx], FirstStepSeq: step.Seq}
		if idx == deltaStart {
			ev.Revises = revisesHash
		}
		seen[h] = ev
		step.NewEvents = append(step.NewEvents, ev)
		j.Events = append(j.Events, ev)
	}
}

// stitchTaskTitle titles the Task a stitch boundary opens when there's no
// genuine new user instruction right there — toolLoopTitle would otherwise
// claim this is "just a tool loop continuing", which understates what
// actually happened (a structural context break was bridged).
func stitchTaskTitle(e *ctxgraph.StitchEdge, lang i18n.Lang) string {
	return i18n.Story(lang).StitchedTaskTitle(e.Kind.String(), pctStr(e.Score))
}

// extractEntities moved to chatmsg.ExtractEntities: internal/report needed
// the same file-path/URL scan for its own compaction section, and chatmsg
// is the one package both already depend on without crossing either side's
// archtest boundary.
func extractEntities(text string) []string { return chatmsg.ExtractEntities(text) }

// buildCompactionInfo computes a stitch boundary's information-loss
// summary: token counts before (the predecessor's last manifest) and after
// (this step, the successor's opening), plus which entities (file-path-like
// or URL tokens) mentioned in the predecessor's last rendered request
// stopped appearing here versus which survived.
func buildCompactionInfo(predRec *audit.Record, predManifest, curManifest *ctxgraph.Manifest, curMsgs []chatmsg.Message) *CompactionInfo {
	info := &CompactionInfo{}
	if predManifest.UsageOK {
		info.TokensBefore = predManifest.Usage.In
	}
	if curManifest.UsageOK {
		info.TokensAfter = curManifest.Usage.In
	}

	predBody, _ := predRec.Client.Request.Body.(map[string]any)
	var predText, curText strings.Builder
	for _, pm := range chatmsg.Messages(predBody) {
		predText.WriteString(pm.Text)
		predText.WriteByte('\n')
	}
	for _, cm := range curMsgs {
		curText.WriteString(cm.Text)
		curText.WriteByte('\n')
	}
	// Membership is tested against the FULL, uncapped curText — not against
	// extractEntities(curText.String())'s own MaxEntities-capped list. Both
	// sides independently truncate to their own first 30 distinct entities
	// (in order of first appearance in THAT text), so a predecessor entity
	// that survives but only reappears as curText's 31st+ distinct entity
	// would fall outside a capped curEntities set and get misreported as
	// swallowed — asserting a false "this was lost" instead of just omitting
	// it, which the design's "宁可粗糙也不猜语义" principle doesn't license.
	// A plain substring check sidesteps this: entities are already exact
	// literal tokens the regex matched, so testing for their literal
	// presence in curText doesn't need re-running (or re-capping) the scan.
	curTextStr := curText.String()
	predTextStr := predText.String()
	info.PredecessorTextExcerpt, _ = truncateText(predTextStr, 3000)
	for _, e := range extractEntities(predTextStr) {
		if strings.Contains(curTextStr, e) {
			info.SurvivedEntities = append(info.SurvivedEntities, e)
		} else {
			info.SwallowedEntities = append(info.SwallowedEntities, e)
		}
	}
	return info
}

// idTimeLayout renders a manifest timestamp for use inside a Journey id:
// no colons (filename-safe on every OS, including Windows) and a fixed
// width, so lexical sort of ids/filenames matches chronological order.
// Deliberately NOT run through fmtutil.DisplayZone and NOT forced to UTC —
// it's formatted straight off the manifest's own parsed time.Time, which
// carries whatever offset the audit record was written with (e.g. +08:00
// for a China-local server). That offset is a property of the data, not of
// whichever machine later runs `vmr story`, so two machines processing the
// same audit files still derive the identical id string — the "stable
// across independent runs" property this id needs — while the digits a
// human sees are the same local wall-clock time the record was captured
// at, not a UTC figure that needs +8h translation to make sense. Converting
// through DisplayZone here would reintroduce exactly the instability this
// comment warns about (the reading machine's zone leaking into the id).
const idTimeLayout = "20060102T150405"

// idCodeLen is how many hex characters of RootHash back a Journey id's
// trailing disambiguator — see deriveID. 8 hex chars (32 bits) is ample:
// by the time client tag + start + end timestamp already agree, two
// distinct lineages colliding on top of that is not a realistic scenario
// this needs to defend hard against, unlike RootHash itself (still hashed
// in full — see ctxgraph.Lineage.RootHash) which is the actual identity
// check.
const idCodeLen = 8

// deriveID identifies a Journey as "j-<client>-<start>-<end>-<code>":
// client and code come from chain[0] (the chain's own root — client is its
// root manifest's sanitized ClientKeyTag, code a short prefix of its
// RootHash), start is chain[0]'s opening timestamp, end is the LAST
// lineage's closing timestamp — so a stitched Journey's id spans its whole
// reconnected timeline, not just its most recent lineage's slice of it.
// Enough to disambiguate two lineages/chains that otherwise share a client
// and exact start/end second, not the identity itself (design-doc review
// follow-up: putting client+time first instead of a bare hash means `ls
// reports/stories/` and a bare `-journey <id>` listing both sort
// meaningfully — grouped by client, chronological within each — instead of
// by content-hash noise).
//
// Still fully content-addressed and stable across independent runs
// regardless of which other files were also loaded: every component here
// comes from the chain's own manifests (and
// ctxgraph.StitchGraph's evidence, itself derived purely from manifest
// content), never from load order or which file it was read from.
func deriveID(chain []*ctxgraph.Lineage) string {
	head, tail := chain[0], chain[len(chain)-1]
	root := head.Manifests[0]
	last := tail.Manifests[len(tail.Manifests)-1]
	client := sanitizeIDComponent(root.ClientKeyTag)
	start := root.TS.Format(idTimeLayout)
	end := last.TS.Format(idTimeLayout)
	code := head.RootHash().String()[:idCodeLen]
	return "j-" + client + "-" + start + "-" + end + "-" + code
}

// idUnsafeRe matches anything not safe to put in a filename/CLI-argument
// component unescaped.
var idUnsafeRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitizeIDComponent lowercases s and collapses every run of
// filename-unsafe characters into a single "-", trimming any that land at
// the edges. "" (auth disabled, no key matched) becomes "nokey" rather than
// an empty id segment, which would otherwise collapse two adjacent "-"
// separators into one and make the id ambiguous to split back apart.
func sanitizeIDComponent(s string) string {
	s = idUnsafeRe.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "nokey"
	}
	return s
}

// eventHashAt returns idx's content hash for event-stream de-duplication
// purposes. For idx >= m.LeadSys this is simply m.Keys[idx-m.LeadSys] — the
// exact hash ctxgraph.BuildManifest already computed (MsgIdx[j] == LeadSys+j
// for every j, by construction — see manifest.go). Only the leading system
// block (idx < LeadSys) needs a hash computed here: ctxgraph folds those
// into one running SysHash for lineage-splitting purposes, but the event
// stream wants each shown (and de-duplicated) as its own entry.
//
// For system messages (idx < LeadSys) this hashes via json.Marshal (a
// quoted-string digest), not via md5.Sum([]byte(text)) the way SysHash
// does. The two serve different purposes: SysHash is compared across
// manifests to detect system-prompt changes; event-stream hashes only need
// self-consistency for dedup within one Build run. The two hash spaces are
// never compared against each other, so the difference is harmless.
func eventHashAt(m *ctxgraph.Manifest, msgs []chatmsg.Message, rawMsgs []any, off, idx int) ctxgraph.Hash {
	if idx >= m.LeadSys {
		return m.Keys[idx-m.LeadSys]
	}
	var raw any = msgs[idx].Text
	if ri := idx - off; ri >= 0 && ri < len(rawMsgs) {
		raw = rawMsgs[ri]
	}
	b, _ := json.Marshal(raw)
	return md5.Sum(b)
}

// newInstructionTitleAtStitch is taskseg.LastInstruction's stitch-boundary
// counterpart: scans the WHOLE manifest via ru (deltaStart is always 0 at a
// stitch boundary — see buildFrom) but, unlike LastInstruction, skips any
// candidate whose content hash is already in seen. Without this, a stitch
// boundary whose manifest opens with the same shared anchor the
// predecessor already showed (the common case — the s231 example keeps
// the exact opening instruction verbatim) would title the new task
// with that same instruction again, reading as "asked the same thing
// twice" when nothing new was actually said. Takes ru (already indexed by
// buildFrom's single per-step IndexRealUsers call) rather than prof/rawMsgs,
// so this doesn't re-run RealUserText a second time over the same manifest.
func newInstructionTitleAtStitch(ru taskseg.RealUsers, m *ctxgraph.Manifest, msgs []chatmsg.Message, rawMsgs []any, off int, seen map[ctxgraph.Hash]*Event) string {
	best := -1
	for idx := range ru {
		if _, dup := seen[eventHashAt(m, msgs, rawMsgs, off, idx)]; dup {
			continue
		}
		if idx > best {
			best = idx
		}
	}
	if best < 0 {
		return ""
	}
	return taskseg.Preview(ru[best])
}

// titleAtStitchBoundary resolves the title (and whether this step counts as
// HumanInitiated) for a task opening at a stitch boundary — split out of
// buildFrom's task-opening branch, which the architecture review's function-
// length budget bounds. Only a genuinely NEW instruction (not already in
// seen) should become the title: deltaStart is always 0 at a stitch
// boundary, so naively picking up the shared anchor message (e.g. s231's
// opening instruction, already shown from the predecessor) would title the
// task with it again, reading as "the user asked the same thing twice" even
// though nothing new was actually said. Falling back to stitchTaskTitle's
// more specific wording when there's no such instruction is more
// informative than the generic tool-loop placeholder — a bridged structural
// break is worth calling out on its own.
func titleAtStitchBoundary(ru taskseg.RealUsers, m *ctxgraph.Manifest, msgs []chatmsg.Message, rawMsgs []any, off int, seen map[ctxgraph.Hash]*Event, stitchEdge *ctxgraph.StitchEdge, lang i18n.Lang) (title string, humanInitiated bool) {
	newInstr := newInstructionTitleAtStitch(ru, m, msgs, rawMsgs, off, seen)
	title = taskseg.TaskTitle(newInstr, stitchTaskTitle(stitchEdge, lang))
	return title, newInstr != ""
}

// deriveTitle is the Journey's own title: the earliest real user
// instruction in firstRu — j.Tasks[0]'s own step-level RealUsers index,
// already built by buildFrom's single per-step IndexRealUsers call, not
// re-parsed here — falling back to the first task with a real title, then
// a placeholder.
func deriveTitle(firstRu taskseg.RealUsers, tasks []*Task, lang i18n.Lang) string {
	if t := taskseg.FirstInstruction(firstRu); t != "" {
		return t
	}
	st := i18n.Story(lang)
	for _, t := range tasks {
		if t.Title != "" && t.Title != st.ToolLoopTitle {
			return t.Title
		}
	}
	return st.NoTitle
}

// sortByRootThenTime orders lineages deterministically for listing: by
// their first manifest's timestamp (chronological candidate list is what a
// user scanning "what happened when" wants), falling back to RootHash for
// any exact time tie (should not happen in practice, but keeps output
// order deterministic across runs regardless of map iteration).
func sortByRootThenTime(ls []*ctxgraph.Lineage) {
	sort.SliceStable(ls, func(i, j int) bool {
		ti, tj := ls[i].Manifests[0].TS, ls[j].Manifests[0].TS
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return ls[i].RootHash().String() < ls[j].RootHash().String()
	})
}
