// Ver 2026-08-20, by Sonnet 5

// The Task/Step/Event/ToolCall structural skeleton journey-<id>.json
// publishes as its "structure" field (architecture doc §7.4b). This is assembly,
// not new computation: everything here already sits on Journey/Step/Event
// (see journey.go); the judgment call this file makes is the inline-vs-reference
// boundary: a Step's OWN decision content (RespText/Reasoning/tool-call args,
// plus rule-derived classifications ABOUT content — edit kind, stitch evidence,
// compaction token/entity counts) is inlined and bounded; an ordinary
// conversation-history MESSAGE (NewEvents, and a tool call's RESULT text —
// see ToolCallRef's doc comment for why the result is history, not
// decision) is a reference only — the audit log is the one place message
// bodies live, journey-<id>.json is tree, not blob.
//
// Edit/StitchEdge/Compaction are GRAPH-level analysis facts with no other
// machine-readable home (they cannot be recomputed from a single audit record
// via Req the way NewEvents' text can), and a tool result is conversation
// history (it reappears verbatim as the next Step's tool-role NewEvent) rather
// than this-turn decision content. Both are reflected in the shape below.
package story

import (
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

// structureExcerptChars bounds every inlined free-text field in this file
// (RespText, Reasoning, a tool call's Args) — reuses compare.go's existing
// excerpt cap (initialInstructionExcerptChars, P1.3's decided bound) rather
// than inventing a second one, so "how much inlined text is too much" has a
// single answer in the codebase. Measured against real corpus data (P4's
// execution record): on a 22-step/33-tool-call sample Journey, only 6/33
// tool-call Args and 0/22 RespText excerpts actually hit this cap — 2000 is
// not aggressive for this workload. If a future corpus shows otherwise,
// split it into its own named constant with a comment explaining why it
// differs; don't let it silently drift from this one.
const structureExcerptChars = initialInstructionExcerptChars

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

// ToolCallRef is one Step's tool call — its own arguments (this turn's
// decision, inlined and bounded) plus whether a paired result was found and
// whether that result was an error. The result's TEXT is deliberately NOT
// carried here: a tool result is conversation-history content, not a
// decision — it is what the client echoes back into the NEXT request, so it
// already exists as that next Step's tool-role NewEvent (verified on a real
// 33-tool-call sample Journey: exactly 33 tool-role NewEvents appear across
// the Journey, one per matched call). Inlining it as well would store the
// same blob under two addresses inside the same tree, which is exactly what
// the architecture doc's blob/tree principle rules out. Matched/ID/Name are
// paired ONLY via findings_toolresult.go's toolResultsFor (exact + id-
// normalized matching — the same precise pairing the decision spine and the
// three Finding detectors already trust, P1.1). The render-layer-only
// positional fallback (positionalToolResults, render_spine_step.go) never
// appears here — it is a guess, and a machine-readable structural contract
// does not carry guesses (architecture doc §5.6). Matched=false means
// toolResultsFor found no pairing for this call; ResultError is then false
// (not a claim that a found result was error-free).
type ToolCallRef struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Args          string `json:"args,omitempty"`
	ArgsTruncated bool   `json:"args_truncated,omitempty"`
	Matched       bool   `json:"matched"`
	ResultError   bool   `json:"result_error,omitempty"`
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

// CompactionRef mirrors CompactionInfo minus PredecessorTextExcerpt — that
// field is a bounded excerpt of the swallowed predecessor's own message
// text (conversation-history content, per this file's inline/reference
// boundary), while TokensBefore/After and the entity lists below are rule-
// derived FACTS about that content (counts and name lists, not the content
// itself), the same class of thing EditRef/StitchRef's classifications are.
// Excluding the excerpt is a real, deliberate narrowing versus what fact-
// layer shows today; if a future consumer needs the excerpt too, it should
// be added here explicitly rather than assumed lost, since nothing else
// carries it once P5.1 removes the rendering.
type CompactionRef struct {
	TokensBefore      int64    `json:"tokens_before"`
	TokensAfter       int64    `json:"tokens_after"`
	SwallowedEntities []string `json:"swallowed_entities,omitempty"`
	SurvivedEntities  []string `json:"survived_entities,omitempty"`
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

	RespText           string        `json:"resp_text,omitempty"`
	RespTextTruncated  bool          `json:"resp_text_truncated,omitempty"`
	Reasoning          string        `json:"reasoning,omitempty"`
	ReasoningTruncated bool          `json:"reasoning_truncated,omitempty"`
	ToolCalls          []ToolCallRef `json:"tool_calls,omitempty"`
	NewEvents          []EventRef    `json:"new_events,omitempty"`
}

// TaskStructure mirrors Task: a title plus its Steps' full structure.
type TaskStructure struct {
	Title string          `json:"title"`
	Steps []StepStructure `json:"steps"`
}

// JourneyStructure is journey-<id>.json's "structure" field — the complete
// Task/Step/Event/ToolCall skeleton, absent any conversation-history
// message body (see EventRef's doc comment). Concatenating every Step's
// NewEvents in Task/Step order reproduces Journey.Events exactly —
// journey.go's appendNewEvents writes to both step.NewEvents and j.Events
// in the same loop — so this deliberately does NOT also carry a top-level
// Events array; that would be the same data published twice.
type JourneyStructure struct {
	Tasks []TaskStructure `json:"tasks"`
}

// BuildStructure assembles j's already-computed Task/Step/Event data into
// its published JSON shape. Purely a projection — no new facts are
// computed here beyond the inline/reference boundary and the excerpt
// truncation it applies to decision-content text fields.
func BuildStructure(j *Journey) JourneyStructure {
	steps := journeySteps(j)
	seq := 0 // steps' global index, kept in lockstep with the Task/Step walk below — journeySteps(j) has the same order, so this avoids re-searching it per Step
	out := JourneyStructure{Tasks: make([]TaskStructure, 0, len(j.Tasks))}
	for _, task := range j.Tasks {
		ts := TaskStructure{Title: task.Title, Steps: make([]StepStructure, 0, len(task.Steps))}
		for _, s := range task.Steps {
			ts.Steps = append(ts.Steps, buildStepStructure(steps, seq, s))
			seq++
		}
		out.Tasks = append(out.Tasks, ts)
	}
	return out
}

// buildStepStructure builds one Step's StepStructure. steps/i are the full
// Journey-order slice and s's index within it — toolResultsFor needs both
// (it looks at steps[i+1] for the paired result), not just s itself.
func buildStepStructure(steps []*Step, i int, s *Step) StepStructure {
	respText, respTrunc := truncateText(s.RespText, structureExcerptChars)
	reasoning, reasoningTrunc := truncateText(s.Reasoning, structureExcerptChars)

	ss := StepStructure{
		Seq:                s.Seq,
		DeltaStart:         s.DeltaStart,
		SysChanged:         s.SysChanged,
		HumanInitiated:     s.HumanInitiated,
		NoReply:            s.NoReply,
		Finish:             s.Finish,
		RespText:           respText,
		RespTextTruncated:  respTrunc,
		Reasoning:          reasoning,
		ReasoningTruncated: reasoningTrunc,
	}
	if s.Manifest != nil {
		ss.Req = s.Manifest.Req
		ss.TS = s.Manifest.TS
		ss.Endpoint = s.Manifest.Endpoint
		ss.DurMS = s.Manifest.DurMS
		ss.TTFTMS = s.Manifest.TTFTMS
		ss.Usage = s.Manifest.Usage
		ss.UsageInOK = s.Manifest.UsageInOK
		ss.UsageOutOK = s.Manifest.UsageOutOK
	}
	if s.Edge != nil {
		ss.Edit = &EditRef{Kind: s.Edge.Kind.String(), LCP: s.Edge.LCP, Coverage: s.Edge.Coverage}
	}
	if s.StitchEdge != nil {
		ss.StitchEdge = &StitchRef{Kind: s.StitchEdge.Kind.String(), Score: s.StitchEdge.Score, Confidence: s.StitchEdge.Confidence}
	}
	if s.Compaction != nil {
		ss.Compaction = &CompactionRef{
			TokensBefore:      s.Compaction.TokensBefore,
			TokensAfter:       s.Compaction.TokensAfter,
			SwallowedEntities: s.Compaction.SwallowedEntities,
			SurvivedEntities:  s.Compaction.SurvivedEntities,
		}
	}

	if len(s.ToolCalls) > 0 {
		matched := toolResultsFor(steps, i)
		byID := make(map[string]chatmsg.ToolResult, len(matched))
		for _, r := range matched {
			byID[r.CallID] = r
		}
		ss.ToolCalls = make([]ToolCallRef, 0, len(s.ToolCalls))
		for _, tc := range s.ToolCalls {
			args, argsTrunc := truncateText(tc.Args, structureExcerptChars)
			ref := ToolCallRef{ID: tc.ID, Name: tc.Name, Args: args, ArgsTruncated: argsTrunc}
			if r, ok := byID[tc.ID]; ok {
				ref.Matched = true
				ref.ResultError = r.IsError
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
