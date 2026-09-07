// Ver 2026-08-20, by Sonnet 5

// The Task/Step/Event/ToolCall structural skeleton journey-<id>.json
// publishes as its "structure" field (architecture doc §7.4b). This is assembly,
// not new computation: everything here already sits on Journey/Step/Event
// (see journey.go); the judgment call this file makes is the inline-vs-reference
// boundary: a Step's OWN decision content (RespText/Reasoning/tool-call args,
// plus rule-derived classifications ABOUT content — edit kind, stitch evidence,
// compaction token/entity counts) is inlined (resp/reasoning/args unlimited or
// capped at maxBodyExcerptChars — see the per-field comments), a tool call's
// paired RESULT lives in the same file's deduplicated bodies blob table (D18 /
// §3.6) referenced by ToolCallRef.Result, and an ordinary conversation-history
// MESSAGE (NewEvents) is a hash reference only — the message text itself still
// lives only in the audit log (nothing renders it; the error-marker signal the
// renderer needs is stamped per Step as HasErrorMarker).
//
// Edit/StitchEdge/Compaction are GRAPH-level analysis facts with no other
// machine-readable home (they cannot be recomputed from a single audit record
// via Req the way NewEvents' text can), and a tool result is conversation
// history (it reappears verbatim as the next Step's tool-role NewEvent) rather
// than this-turn decision content. Both are reflected in the shape below.
package journey

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
)

// maxBodyExcerptChars is the uniform 3000-character data-layer truncation cap
// for tool-call arguments, tool-call results, and compaction predecessor excerpts (D18 / §3.6).
// RespText/Reasoning stored under RespRef has no character limit.
const maxBodyExcerptChars = 3000

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

type blobStore map[string]string

func (b blobStore) put(text string) string {
	h := hashText(text)
	b[h] = text
	return h
}

// EventRef is one message's structural identity within the Journey's
// globally de-duped event stream — a REFERENCE, never its text: an ordinary
// conversation message is history, not this turn's decision (architecture
// doc §7.4b). Hash is the same digest ctxgraph.Manifest.Keys already
// carries for every non-leading-system message in the owning Step's own
// record (md5 of the message's raw decoded JSON value, or its flattened
// text when no raw form is available — ctxgraph.BuildManifest computes this
// identically; a consumer wanting to re-derive it independently should
// build a fresh Manifest for the Step's Req record and match against
// Manifest.Keys/MsgIdx rather than reimplementing the hash byte-for-byte).
// A consumer that needs the actual TEXT follows the owning Step's Req
// coordinate to the audit record (audit.LineAt + chatmsg, or a fresh
// ctxgraph.BuildManifest to recover the Hash↔position mapping) or the
// record's rendered detail page (internal/reqdetail) — journey-<id>.json is
// tree, the audit log is blob, tree only holds references.
type EventRef struct {
	Hash ctxgraph.Hash `json:"hash"`
	Role string        `json:"role"`
	// FirstStepSeq always equals the owning StepStructure's own Seq here —
	// NewEvents is nested under exactly the Step that introduced it, so this
	// is redundant in that context by construction (appendNewEvents in
	// journey.go only ever appends an Event to the Step that just
	// discovered it). It is kept anyway for a DIFFERENT consumption shape:
	// a reader that flattens every Task/Step's NewEvents into one global,
	// first-appearance-ordered event stream (reproducing Journey.Events,
	// see JourneyStructure's doc comment) needs this to know which Step
	// introduced each event WITHOUT walking back up to a parent pointer —
	// not redundancy, a field shaped for a different access pattern.
	FirstStepSeq int            `json:"first_step_seq"`
	Revises      *ctxgraph.Hash `json:"revises,omitempty"`
}

// ToolResultRef is one tool call's paired result reference, confidence level,
// and error status (D18 / §3.6).
type ToolResultRef struct {
	Ref     string `json:"ref"`
	Match   string `json:"match"` // "exact" | "normalized" | "positional"
	IsError bool   `json:"is_error,omitempty"`
}

// ToolCallRef is one Step's tool call — referencing its arguments in the bodies
// blob table (args_ref), plus its paired result reference (result) if found (D18 / §3.6).
type ToolCallRef struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	ArgsRef string         `json:"args_ref,omitempty"`
	Result  *ToolResultRef `json:"result,omitempty"`
	// Repeat is toolCallRepeats' exact-repeat flag for this call — stamped
	// at build time (the full, untruncated arguments the repeat identity
	// hashes are only available here) so the renderer's 🔄 tag and the
	// timeline's 🔄 symbol read a stamped fact instead of re-deriving a
	// hash over the truncated bodies blob.
	Repeat bool `json:"repeat,omitempty"`
}

// EditRef mirrors ctxgraph.Edit — the message-history transition
// classification between this Step's manifest and the logically-preceding
// one (append/replace-tail/splice/… — see ctxgraph/edit.go's calibrated
// classifier). nil exactly when Step.Edge is nil: the Journey's first Step,
// or a stitch boundary (where StitchEdge carries the equivalent evidence
// instead). This is a graph-level analysis fact — Classify(prev, cur)
// compares TWO manifests — so, unlike NewEvents' text, it cannot be
// recomputed from this Step's own Req record alone; if this file didn't
// carry it, fact-layer's per-step edit-kind line would become
// unrecoverable the moment P5.1 deletes that rendering.
type EditRef struct {
	Kind     string  `json:"kind"`
	LCP      int     `json:"lcp"`
	Coverage float64 `json:"coverage"`
}

// StitchRef mirrors ctxgraph.StitchEdge — non-nil exactly when this Step is
// the first manifest of a stitched-in Lineage (Step.StitchEdge's own doc
// comment). Same "graph-level, not single-record" reasoning as EditRef:
// StitchGraph's bucket/coverage search spans the whole corpus, not this one
// record, so Req cannot recover it — this is the only place it survives
// past P5.1's fact-layer deletion.
type StitchRef struct {
	Kind       string  `json:"kind"`
	Score      float64 `json:"score"`
	Confidence float64 `json:"confidence"`
}

// CompactionRef mirrors CompactionInfo, referencing the predecessor excerpt in
// the bodies blob store (D18 / §3.6).
type CompactionRef struct {
	TokensBefore          int64    `json:"tokens_before"`
	TokensAfter           int64    `json:"tokens_after,omitempty"`
	PredecessorExcerptRef string   `json:"predecessor_excerpt_ref,omitempty"`
	SwallowedEntities     []string `json:"swallowed_entities,omitempty"`
	SurvivedEntities      []string `json:"survived_entities,omitempty"`
}

// StepStructure is one Step's complete machine-readable shape: its own req
// coordinate and per-record facts (timing, usage, endpoint — all already on
// Step.Manifest, inlined so a cost/latency profile needs no per-step I/O),
// the graph-level classifications fact-layer shows today (Edit/StitchEdge/
// Compaction — see their own doc comments for why they must be inlined
// here, not referenced), this turn's OWN decision content (bounded —
// RespText, Reasoning, tool-call args), and references (never inlined text)
// to the conversation-history messages it introduced.
type StepStructure struct {
	Seq int       `json:"seq"`
	Req string    `json:"req,omitempty"`
	TS  time.Time `json:"ts"`
	// TSDisplay is TS rendered in fmtutil.DisplayZone (§5.6: the frontend
	// shows this verbatim and never converts a timezone). journey-viewer.html
	// reads it; TS stays the machine form.
	TSDisplay string `json:"ts_display,omitempty"`
	// Model/Protocol/Outcome are Manifest.Model/Protocol/Outcome verbatim —
	// Model (the virtual model name) and Outcome are what
	// reqdetail.FileName needs to recompute this Step's detail-page name
	// (the real-model segment is derivable from Endpoint's
	// protocol:provider:model label); Outcome also gates the spine's error
	// role tag and the overview's failed-steps line; Protocol feeds the
	// per-journey Anthropic-only detector-coverage disclosure.
	Model    string `json:"model,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	// Instruction is Step.Instruction verbatim — the mid-task user
	// instruction the spine renders for a Step that opens with one.
	Instruction string `json:"instruction,omitempty"`
	// HasErrorMarker stamps isErrorMarker containment over this Step's own
	// NewEvents' text — the one thing the renderer needed event TEXT for
	// (the spine's error role tag, the overview's first-error node, the
	// timeline's ❌ column); the events' text itself stays referenced-only
	// (see EventRef), so the derived fact is stamped where the text was
	// actually read instead of dragging every message body into the blob
	// table for a boolean.
	HasErrorMarker bool `json:"has_error_marker,omitempty"`
	// DeltaStart is Step.DeltaStart verbatim — a navigation CONVENIENCE
	// (chatmsg.Messages' 0-based index where this Step's request body stops
	// matching its predecessor's), not the reconstruction mechanism: at a
	// stitch boundary, or wherever this Step's "new" range happens to
	// repeat a message byte-identical to one already seen earlier in the
	// Journey, msgs[DeltaStart:] is a SUPERSET of NewEvents (some entries
	// get filtered by the Journey-wide seen-hash dedup — see journey.go's
	// appendNewEvents). A correct external reconstruction matches each
	// EventRef.Hash against the refetched record's own message hashes
	// (EventRef's doc comment), not a raw DeltaStart slice; DeltaStart is
	// published because it is cheap, already on hand, and gets an external
	// reader straight to the right neighborhood before hash-matching narrows
	// it exactly — see structure_test.go's TestBuildStructure_
	// LosslessReconstruction for the reconstruction this file's contract
	// actually rests on.
	DeltaStart int `json:"delta_start"`

	Endpoint string        `json:"endpoint,omitempty"`
	DurMS    int64         `json:"dur_ms,omitempty"`
	TTFTMS   int64         `json:"ttft_ms,omitempty"`
	Usage    chatmsg.Usage `json:"usage"`
	// Per-side usage-ledger flags (see chatmsg.ExtractUsageSides) — the
	// old single usage_ok conflated a real output total with Anthropic's
	// message_start placeholder.
	UsageInOK  bool `json:"usage_in_ok,omitempty"`
	UsageOutOK bool `json:"usage_out_ok,omitempty"`

	Edit       *EditRef       `json:"edit,omitempty"`
	StitchEdge *StitchRef     `json:"stitch_edge,omitempty"`
	SysChanged bool           `json:"sys_changed,omitempty"`
	Compaction *CompactionRef `json:"compaction,omitempty"`

	HumanInitiated bool   `json:"human_initiated,omitempty"`
	NoReply        bool   `json:"no_reply,omitempty"`
	Finish         string `json:"finish,omitempty"`

	RespRef string `json:"resp_ref,omitempty"`
	// RespIsReasoning disambiguates what RespRef holds: false (omitted) —
	// the Step's stated reply (RespText); true — its Reasoning, stored here
	// only because RespText was empty. RespRef alone cannot distinguish the
	// two, and the renderer treats them differently (🤔 prefix, plan vs
	// report role tag).
	RespIsReasoning bool `json:"resp_is_reasoning,omitempty"`
	// ReasoningRef holds the Step's Reasoning when RespText was ALSO
	// non-empty (both fields can carry content in one response; when
	// RespText was empty Reasoning lives under RespRef with
	// RespIsReasoning set, and this is omitted).
	ReasoningRef string        `json:"reasoning_ref,omitempty"`
	ToolCalls    []ToolCallRef `json:"tool_calls,omitempty"`
	NewEvents    []EventRef    `json:"new_events,omitempty"`

	// SysHash/SysChars identify this Step's leading system block — the
	// (HasSys, SysHash) grouping systemPromptEras (render_md_sysprompt.go,
	// mirrored over this shape by the viewmodel) needs, plus the char
	// count that era line shows. SysHash nil = no leading system block;
	// SysChars is 0 for those Steps and for a manifest without a count.
	SysHash  *ctxgraph.Hash `json:"sys_hash,omitempty"`
	SysChars int            `json:"sys_chars,omitempty"`
}

// TaskStructure mirrors Task: a title plus its Steps' full structure.
type TaskStructure struct {
	Title string          `json:"title"`
	Steps []StepStructure `json:"steps"`
}

// JourneyStructure is journey-<id>.json's "structure" field — the complete
// Task/Step/Event/ToolCall skeleton plus self-contained bodies blob store (D18 / §3.6).
type JourneyStructure struct {
	Tasks  []TaskStructure   `json:"tasks"`
	Bodies map[string]string `json:"-"`
}

// BuildStructure assembles j's already-computed Task/Step/Event data into
// its published JSON shape, populating the deduplicated bodies table (D18 / §3.6)
// and stamping the journey-wide exact-repeat flag per tool call (see
// ToolCallRef.Repeat).
func BuildStructure(j *Journey) JourneyStructure {
	steps := journeySteps(j)
	bodies := make(blobStore)
	seq := 0
	// repeatByStep collects toolCallRepeats' per-call flags in call order per
	// Step, so buildStepStructure can stamp them without re-hashing arguments
	// (and without the truncation the bodies blob table would impose).
	repeatByStep := map[int][]bool{}
	for _, o := range toolCallRepeats(steps) {
		repeatByStep[o.StepSeq] = append(repeatByStep[o.StepSeq], o.IsRepeat)
	}
	out := JourneyStructure{
		Tasks:  make([]TaskStructure, 0, len(j.Tasks)),
		Bodies: bodies,
	}
	for _, task := range j.Tasks {
		ts := TaskStructure{Title: task.Title, Steps: make([]StepStructure, 0, len(task.Steps))}
		for _, s := range task.Steps {
			ts.Steps = append(ts.Steps, buildStepStructure(steps, seq, s, bodies, repeatByStep[s.Seq]))
			seq++
		}
		out.Tasks = append(out.Tasks, ts)
	}
	return out
}

type matchedToolResult struct {
	result chatmsg.ToolResult
	match  string // "exact" | "normalized" | "positional"
}

func pairToolResults(steps []*Step, i int) map[string]matchedToolResult {
	if i < 0 || i >= len(steps) || len(steps[i].ToolCalls) == 0 || i+1 >= len(steps) {
		return nil
	}
	s := steps[i]
	exactIDs := make(map[string]bool, len(s.ToolCalls))
	normToOrig := make(map[string]string, len(s.ToolCalls))
	for _, tc := range s.ToolCalls {
		exactIDs[tc.ID] = true
		normToOrig[chatmsg.NormalizeToolCallID(tc.ID)] = tc.ID
	}

	out := make(map[string]matchedToolResult, len(s.ToolCalls))
	byID := make(map[string]chatmsg.ToolResult, len(s.ToolCalls))

	// Pass 1: exact matches
	for _, r := range steps[i+1].NewToolResults {
		if exactIDs[r.CallID] {
			if _, already := out[r.CallID]; !already {
				out[r.CallID] = matchedToolResult{result: r, match: "exact"}
				byID[r.CallID] = r
			}
		}
	}

	// Pass 2: normalized matches for unresolved calls
	for _, r := range steps[i+1].NewToolResults {
		if exactIDs[r.CallID] {
			continue
		}
		norm := chatmsg.NormalizeToolCallID(r.CallID)
		if orig, ok := normToOrig[norm]; ok {
			if _, already := out[orig]; !already {
				rNorm := r
				rNorm.CallID = orig
				out[orig] = matchedToolResult{result: rNorm, match: "normalized"}
				byID[orig] = rNorm
			}
		}
	}

	// Pass 3: positional fallback for still-unresolved calls
	posByID := positionalToolResults(steps, i, byID)
	for callID, r := range posByID {
		if _, ok := out[callID]; !ok {
			out[callID] = matchedToolResult{result: r, match: "positional"}
		}
	}

	return out
}

// buildStepStructure builds one Step's StepStructure. repeats is this Step's
// per-call exact-repeat flags, in ToolCalls order (see ToolCallRef.Repeat).
func buildStepStructure(steps []*Step, i int, s *Step, bodies blobStore, repeats []bool) StepStructure {
	ss := StepStructure{
		Seq:            s.Seq,
		DeltaStart:     s.DeltaStart,
		SysChanged:     s.SysChanged,
		HumanInitiated: s.HumanInitiated,
		NoReply:        s.NoReply,
		Finish:         s.Finish,
		Outcome:        s.Outcome,
		Instruction:    s.Instruction,
	}
	for _, ev := range s.NewEvents {
		if strings.Contains(ev.Msg.Text, isErrorMarker) {
			ss.HasErrorMarker = true
			break
		}
	}

	// RespText / Reasoning into bodies without character limit (D18 / §3.6),
	// keeping the two distinguishable: RespRef holds the reply when there is
	// one (RespIsReasoning unset), the reasoning otherwise (set); when BOTH
	// carry content the reasoning also goes under ReasoningRef.
	respContent := s.RespText
	if respContent == "" {
		respContent = s.Reasoning
		ss.RespIsReasoning = s.Reasoning != ""
	}
	if respContent != "" {
		ss.RespRef = bodies.put(respContent)
	}
	if s.Reasoning != "" && !ss.RespIsReasoning {
		ss.ReasoningRef = bodies.put(s.Reasoning)
	}

	if s.Manifest != nil {
		ss.Req = s.Manifest.Req
		ss.TS = s.Manifest.TS
		if !s.Manifest.TS.IsZero() {
			ss.TSDisplay = s.Manifest.TS.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
		}
		ss.Endpoint = s.Manifest.Endpoint
		ss.Model = s.Manifest.Model
		ss.Protocol = s.Manifest.Protocol
		ss.DurMS = s.Manifest.DurMS
		ss.TTFTMS = s.Manifest.TTFTMS
		ss.Usage = s.Manifest.Usage
		ss.UsageInOK = s.Manifest.UsageInOK
		ss.UsageOutOK = s.Manifest.UsageOutOK
		if s.Manifest.HasSys {
			h := s.Manifest.SysHash
			ss.SysHash = &h
		}
		ss.SysChars = s.SysChars
	}
	if s.Edge != nil {
		ss.Edit = &EditRef{Kind: s.Edge.Kind.String(), LCP: s.Edge.LCP, Coverage: s.Edge.Coverage}
	}
	if s.StitchEdge != nil {
		ss.StitchEdge = &StitchRef{Kind: s.StitchEdge.Kind.String(), Score: s.StitchEdge.Score, Confidence: s.StitchEdge.Confidence}
	}
	if s.Compaction != nil {
		var excerptRef string
		if s.Compaction.PredecessorTextExcerpt != "" {
			excerpt, _ := truncateText(s.Compaction.PredecessorTextExcerpt, maxBodyExcerptChars)
			excerptRef = bodies.put(excerpt)
		}
		ss.Compaction = &CompactionRef{
			TokensBefore:          s.Compaction.TokensBefore,
			TokensAfter:           s.Compaction.TokensAfter,
			PredecessorExcerptRef: excerptRef,
			SwallowedEntities:     s.Compaction.SwallowedEntities,
			SurvivedEntities:      s.Compaction.SurvivedEntities,
		}
	}

	if len(s.ToolCalls) > 0 {
		paired := pairToolResults(steps, i)
		ss.ToolCalls = make([]ToolCallRef, 0, len(s.ToolCalls))
		for k, tc := range s.ToolCalls {
			var argsRef string
			if tc.Args != "" {
				args, _ := truncateText(tc.Args, maxBodyExcerptChars)
				argsRef = bodies.put(args)
			}
			ref := ToolCallRef{ID: tc.ID, Name: tc.Name, ArgsRef: argsRef}
			if k < len(repeats) {
				ref.Repeat = repeats[k]
			}
			if p, ok := paired[tc.ID]; ok {
				resText, _ := truncateText(p.result.Text, maxBodyExcerptChars)
				resRef := bodies.put(resText)
				ref.Result = &ToolResultRef{
					Ref:     resRef,
					Match:   p.match,
					IsError: p.result.IsError,
				}
			}
			ss.ToolCalls = append(ss.ToolCalls, ref)
		}
	}

	if len(s.NewEvents) > 0 {
		ss.NewEvents = make([]EventRef, 0, len(s.NewEvents))
		for _, ev := range s.NewEvents {
			ss.NewEvents = append(ss.NewEvents, EventRef{
				Hash:         ev.Hash,
				Role:         ev.Msg.Role,
				FirstStepSeq: ev.FirstStepSeq,
				Revises:      ev.Revises,
			})
		}
	}

	return ss
}
