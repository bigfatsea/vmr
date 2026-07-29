// Ver 2026-07-29 17:15, by Sonnet 5

// Package story turns one internal/ctxgraph.Lineage into a readable
// narrative: a sequence of user-instruction Tasks, each a sequence of
// request/response Steps, plus a globally de-duplicated Event stream
// (design doc §2.3/F1 — reading only the final request's message list
// misses 26%-99% of what actually happened; the event stream is built by
// walking every step and keeping only each message's FIRST appearance).
//
// Step 1 scope (design doc Appendix C.3): one Lineage == one Journey — no
// cross-lineage stitching yet (Appendix C.4/Phase 2). A Lineage that starts
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
	"vmr/internal/story/profile"
)

// newUserWindow mirrors internal/report/session.go's identical constant:
// a real user message only counts as a NEW instruction (opening a task)
// when it sits within this many messages of the request's end. Ported
// rationale: an in-place history edit can push the delta boundary far
// back and sweep an old user message into the "new" range; those must not
// open a task.
const newUserWindow = 8

// Journey is one lineage rendered as a narrative.
type Journey struct {
	ID       string
	Partial  bool // head-truncated (design doc §11 D1) — see BuildAll
	Title    string
	From, To time.Time
	Tasks    []*Task
	Events   []*Event // full de-duped event stream, first-appearance order
	Break    *ctxgraph.BreakInfo
	Lineage  *ctxgraph.Lineage
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
	Edge       *ctxgraph.Edit // nil for the Journey's first step
	DeltaStart int            // absolute message index where this step's new content begins
	NewEvents  []*Event       // events first introduced by this step, in order

	Finish    string
	NoReply   bool
	ToolCalls []chatmsg.ToolCall
	RespText  string
}

// Event is one message's first appearance anywhere in the Journey.
type Event struct {
	Hash         ctxgraph.Hash
	Msg          chatmsg.Message
	FirstStepSeq int
}

// Build renders one Lineage into a Journey using prof for the
// agent-specific real-instruction/no-reply judgment calls. It re-fetches
// every manifest's full audit.Record (a Lineage on its own only carries
// content hashes) in one batched pass per source file. Rendering many
// lineages at once should use BuildAll instead — calling Build in a loop
// re-fetches from scratch per lineage, which re-scans a source file once
// per lineage rooted in (or passing through) it instead of once total.
func Build(l *ctxgraph.Lineage, prof profile.Profile) (*Journey, error) {
	if len(l.Manifests) == 0 {
		return nil, errEmptyLineage
	}
	recs, err := ctxgraph.FetchRecords(manifestLocs(l))
	if err != nil {
		return nil, err
	}
	return buildFrom(l, prof, recs)
}

// BuildAll renders many Lineages into Journeys. Two independent costs are
// batched/parallelized here, found by measuring an actual -render-all run
// against a real 15-file/253-candidate corpus (design-doc review
// follow-up):
//
//  1. I/O: every lineage's manifest-record fetch is batched into a single
//     ctxgraph.FetchRecords call — FetchRecords already groups its reads by
//     source file (zstd isn't seekable, so each file is scanned at most
//     once regardless of how many lines are wanted from it), turning "read
//     every candidate's records" from one pass over the source files PER
//     CANDIDATE into one pass total, same fix PreviewTitles applied to the
//     listing path.
//  2. CPU: buildFrom's own work (re-rendering each manifest's full message
//     list, event-hash dedup, jsonIndent on tool payloads) turned out to be
//     the larger cost on that real corpus — a single request's body already
//     carries its ENTIRE accumulated history, so buildFrom's cost per
//     lineage grows with the square of its turn count, not linearly. Doing
//     that serially, one candidate at a time, left 253 candidates' worth of
//     CPU work on a single core. Each lineage's Journey is independent of
//     every other's (buildFrom only reads the shared recs map, never
//     mutates it), so this runs on the same bounded worker pool
//     scanWorkerCount uses in internal/ctxgraph.
//
// Order of the returned slice matches ls; a per-lineage error aborts the
// whole batch (matches Build's own all-or-nothing contract for a single
// lineage).
func BuildAll(ls []*ctxgraph.Lineage, prof profile.Profile) ([]*Journey, error) {
	for _, l := range ls {
		if len(l.Manifests) == 0 {
			return nil, errEmptyLineage
		}
	}

	var locs []ctxgraph.Loc
	for _, l := range ls {
		locs = append(locs, manifestLocs(l)...)
	}
	recs, err := ctxgraph.FetchRecords(locs)
	if err != nil {
		return nil, err
	}

	out := make([]*Journey, len(ls))
	errs := make([]error, len(ls))
	sem := make(chan struct{}, buildWorkerCount(len(ls)))
	var wg sync.WaitGroup
	for i, l := range ls {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, l *ctxgraph.Lineage) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i], errs[i] = buildFrom(l, prof, recs)
		}(i, l)
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
// lineages) just adds scheduling overhead, not throughput.
func buildWorkerCount(lineages int) int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if lineages > 0 && n > lineages {
		n = lineages
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

// buildFrom is Build's actual assembly logic, factored out so BuildAll can
// share one batched recs lookup across many lineages instead of each
// lineage doing its own FetchRecords call.
func buildFrom(l *ctxgraph.Lineage, prof profile.Profile, recs map[ctxgraph.Loc]*audit.Record) (*Journey, error) {
	j := &Journey{
		ID:      deriveID(l),
		Break:   l.BrokeFrom,
		Lineage: l,
		From:    l.Manifests[0].TS,
		To:      l.Manifests[len(l.Manifests)-1].TS,
	}

	seen := map[ctxgraph.Hash]*Event{}
	var curTask *Task
	seq := 0
	prevNoReply := false
	for i, m := range l.Manifests {
		rec := recs[ctxgraph.Loc{Path: m.Path, Line: m.Line}]
		if rec == nil {
			continue // defensive: FetchRecords silently drops a line it couldn't parse a second time
		}
		body, _ := rec.Client.Request.Body.(map[string]any)
		msgs := chatmsg.Messages(body)
		rawMsgs, _ := body["messages"].([]any)
		off := chatmsg.MsgOffset(body)

		var edge *ctxgraph.Edit
		deltaStart := 0
		newTask := i == 0
		if i > 0 {
			e := l.Edges[i-1]
			edge = &e
			deltaStart = m.LeadSys + e.LCP
			prev := l.Manifests[i-1]
			traceChanged := m.TraceID != "" && prev.TraceID != "" && m.TraceID != prev.TraceID
			hasNewInstr := deltaHasNewInstruction(prof, msgs, rawMsgs, off, m, prev, deltaStart)
			newTask = traceChanged || (!prevNoReply && hasNewInstr)
		}
		if newTask || curTask == nil {
			curTask = &Task{Title: taskTitle(lastInstructionInDelta(prof, msgs, rawMsgs, off, deltaStart))}
			j.Tasks = append(j.Tasks, curTask)
		}

		seq++
		step := &Step{Seq: seq, Manifest: m, Rec: rec, Edge: edge, DeltaStart: deltaStart}
		if rec.Client.Response != nil {
			if s := responseSummary(rec.Client.Response.Body); s != nil {
				step.Finish = s.Finish
				step.RespText = strings.TrimSpace(s.Content)
				step.ToolCalls = s.ToolCalls
			}
		}
		step.NoReply = prof.NoReply(step.Finish, step.RespText)
		prevNoReply = step.NoReply

		for idx := deltaStart; idx < len(msgs); idx++ {
			h := eventHashAt(m, msgs, rawMsgs, off, idx)
			if _, dup := seen[h]; dup {
				continue
			}
			ev := &Event{Hash: h, Msg: msgs[idx], FirstStepSeq: seq}
			seen[h] = ev
			step.NewEvents = append(step.NewEvents, ev)
			j.Events = append(j.Events, ev)
		}
		curTask.Steps = append(curTask.Steps, step)
	}

	j.Title = deriveTitle(prof, j.Tasks)
	return j, nil
}

// idTimeLayout renders a manifest timestamp for use inside a Journey id:
// no colons (filename-safe on every OS, including Windows) and a fixed
// width, so lexical sort of ids/filenames matches chronological order.
// Always UTC — a local-timezone id would depend on which machine/timezone
// ran the tool, breaking the "stable across independent runs" property
// deriveID otherwise guarantees.
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
// client is the root manifest's (sanitized) ClientKeyTag, start/end are the
// lineage's first/last manifest timestamps, and code is a short prefix of
// RootHash — enough to disambiguate two lineages that otherwise share a
// client and exact start/end second, not the identity itself (design-doc
// review follow-up: putting client+time first instead of a bare hash means
// `ls reports/stories/` and a bare `-journey <id>` listing both sort
// meaningfully — grouped by client, chronological within each — instead of
// by content-hash noise).
//
// Still fully content-addressed and stable across independent runs
// regardless of which other files were also loaded (design doc §11 D1):
// every component here comes from the lineage's own manifests, never from
// load order or which file it was read from.
func deriveID(l *ctxgraph.Lineage) string {
	root, last := l.Manifests[0], l.Manifests[len(l.Manifests)-1]
	client := sanitizeIDComponent(root.ClientKeyTag)
	start := root.TS.UTC().Format(idTimeLayout)
	end := last.TS.UTC().Format(idTimeLayout)
	code := l.RootHash().String()[:idCodeLen]
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

// deltaHasNewInstruction ports internal/report/session.go's
// deltaHasNewInstruction: a message only counts as a new instruction if
// it's within newUserWindow of the request's end AND its content wasn't
// already present somewhere in the parent manifest (a set check, not a
// position check — a mid-task history prune can shift an old message into
// the tail window without it being new).
func deltaHasNewInstruction(prof profile.Profile, msgs []chatmsg.Message, rawMsgs []any, off int, cur, prev *ctxgraph.Manifest, deltaStart int) bool {
	prevSet := make(map[ctxgraph.Hash]bool, len(prev.Keys))
	for _, k := range prev.Keys {
		prevSet[k] = true
	}
	total := len(msgs)
	for idx := deltaStart; idx < total; idx++ {
		if idx < total-newUserWindow {
			continue
		}
		if msgs[idx].Role != "user" {
			continue
		}
		if !prof.IsRealUser(msgs[idx], rawMsgs, idx-off) {
			continue
		}
		if idx >= cur.LeadSys {
			ki := idx - cur.LeadSys
			if ki < len(cur.Keys) && prevSet[cur.Keys[ki]] {
				continue // identical content already existed in the parent — shifted, not new
			}
		}
		return true
	}
	return false
}

// lastInstructionInDelta returns the preview of the newest real user
// instruction inside the delta (no newUserWindow bound here — unlike
// deltaHasNewInstruction, this picks the task's TITLE, which should reflect
// whatever the user actually asked even if it's not near the very end);
// "" when this turn is a pure tool-loop step.
func lastInstructionInDelta(prof profile.Profile, msgs []chatmsg.Message, rawMsgs []any, off, deltaStart int) string {
	best := -1
	var bestText string
	for idx := deltaStart; idx < len(msgs); idx++ {
		if msgs[idx].Role != "user" {
			continue
		}
		text, ok := prof.RealUserText(msgs[idx], rawMsgs, idx-off)
		if !ok {
			continue
		}
		if idx > best {
			best, bestText = idx, text
		}
	}
	if best < 0 {
		return ""
	}
	return preview(bestText)
}

func taskTitle(newInstruction string) string {
	if newInstruction != "" {
		return newInstruction
	}
	return "(工具循环延续)"
}

// deriveTitle is the Journey's own title: the earliest real user
// instruction in its very first step (searched over the WHOLE message
// list, not just the delta — the opening ask, not the latest turn),
// falling back to the first task with a real title, then a placeholder.
func deriveTitle(prof profile.Profile, tasks []*Task) string {
	if len(tasks) > 0 && len(tasks[0].Steps) > 0 {
		first := tasks[0].Steps[0]
		if body, ok := first.Rec.Client.Request.Body.(map[string]any); ok {
			msgs := chatmsg.Messages(body)
			rawMsgs, _ := body["messages"].([]any)
			off := chatmsg.MsgOffset(body)
			best := -1
			var bestText string
			for idx, m := range msgs {
				if m.Role != "user" {
					continue
				}
				text, ok := prof.RealUserText(m, rawMsgs, idx-off)
				if !ok {
					continue
				}
				if best == -1 || idx < best {
					best, bestText = idx, text
				}
			}
			if best >= 0 {
				return preview(bestText)
			}
		}
	}
	for _, t := range tasks {
		if t.Title != "" && t.Title != "(工具循环延续)" {
			return t.Title
		}
	}
	return "(无标题)"
}

// responseSummary reassembles a recorded client response body (SSE string
// or JSON object) into the model's output — the same dual-dispatch
// internal/report/session.go's responseSummary does, expressed directly
// over chatmsg's exported ReassembleSSE/FinalMessage.
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

// preview returns a single-line, length-capped excerpt of s — same
// rationale as internal/report/render.go's preview (duplicated rather than
// exported: it's a tiny, stable, purely cosmetic helper).
const previewLen = 80

func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > previewLen {
		return string(r[:previewLen]) + "…"
	}
	return s
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
