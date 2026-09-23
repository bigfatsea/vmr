// Ver 2026-09-24 15:20, by dev

package taskseg

import (
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
)

// StepInput is one recorded turn submitted to session/task segmentation.
type StepInput struct {
	Manifest       *ctxgraph.Manifest
	RealUsers      RealUsers
	TotalMsgs      int
	NoReply        bool
	Compaction     bool
	StitchBoundary bool // first record of a lineage joined onto a stitched chain
	StitchNewTask  bool // stitch evidence says a genuinely new instruction bridged it
}

// StepBoundary is the canonical segmentation decision for one step.
type StepBoundary struct {
	DeltaStart     int
	LCP            int
	NewTask        bool
	HumanInitiated bool
	Instruction    string
	SysChanged     bool
	Parent         int // index of the predecessor record in the input sequence (-1 = none)
	ReplacedTail   int // predecessor messages beyond the common prefix
}

// IsCompaction applies the three-signal compaction heuristic: a summarization
// system prompt, or the no-tools + max_completion_tokens + empty-trace shape.
// Both consumers of the segmentation assembly must classify compaction
// records identically — a divergent classification would reintroduce exactly
// the predecessor disagreement this package exists to settle.
func IsCompaction(body map[string]any, leadSys int, msgs []chatmsg.Message, traceID string) bool {
	if body == nil {
		return false
	}
	_, hasMaxCT := body["max_completion_tokens"]
	sysText := ""
	if leadSys > 0 && len(msgs) > 0 {
		sysText = msgs[0].Text
	}
	if strings.Contains(strings.ToLower(fmtutil.CapStr(sysText, 200)), "summarization") ||
		(len(chatmsg.ToolNames(body)) == 0 && hasMaxCT && traceID == "") {
		return true
	}
	return false
}

// ManifestSysChanged reports whether cur's leading system block differs from
// prev's. Segmentation (and the real-user index prefix-reuse gate) compares
// against the canonical non-compaction predecessor; the standalone function
// is exported because journey additionally needs the same comparison against
// a positional cross-lineage predecessor at stitch boundaries.
func ManifestSysChanged(cur, prev *ctxgraph.Manifest) bool {
	return prev != nil && cur != nil &&
		(cur.HasSys != prev.HasSys || (cur.HasSys && prev.HasSys && cur.SysHash != prev.SysHash))
}

// Segmenter assembles canonical task boundaries across successive records of
// one lineage (or one stitched lineage chain). Its single predecessor rule:
// a compaction record is never the predecessor of a later record — later
// records compare against the most recent non-compaction record, so report
// (which excludes compaction records from session grouping) and journey
// (which renders them as steps) compute identical DeltaStart/new-task
// boundaries for the same record. A lineage boundary resets the tracking:
// structural comparison never crosses a lineage break.
type Segmenter struct {
	seq     int
	last    StepInput // most recent non-compaction step
	hasLast bool
	lastIdx int

	// Per-step phase-1 cache (Pred → Commit); Commit clears it.
	predReady     bool
	predM         *ctxgraph.Manifest
	predIdx       int
	edit          *ctxgraph.Edit
	lineageOpened bool // no structural predecessor for this step
}

// Pred resolves the predecessor facts derivable before the step's real-user
// index exists: the canonical predecessor's manifest, the delta start it
// implies, and whether the leading system block changed against it. These
// are exactly what gates an incremental real-user index (deltaStart decides
// which prefix may be reused; sysChanged when it must not be). atStitch marks
// a record that opens a lineage: no structural comparison applies, Pred
// returns zero values, and the caller keeps its own positional cross-lineage
// predecessor for rendering-level comparisons. Pred must be followed by
// Commit for the same step; Commit calls it lazily when skipped (callers
// whose real-user index doesn't depend on deltaStart can Commit directly).
func (s *Segmenter) Pred(cur *ctxgraph.Manifest, lineageStart bool) (prev *ctxgraph.Manifest, deltaStart int, sysChanged bool) {
	if cur == nil {
		return
	}
	s.predReady = true
	s.lineageOpened = lineageStart || !s.hasLast
	if s.lineageOpened {
		s.predM, s.predIdx, s.edit = nil, -1, nil
		return
	}
	e := ctxgraph.Classify(s.last.Manifest, cur)
	s.predM, s.predIdx, s.edit = s.last.Manifest, s.lastIdx, &e
	return s.predM, cur.LeadSys + e.LCP, ManifestSysChanged(cur, s.predM)
}

// Commit records one step and returns its canonical boundary. When Pred was
// skipped, it runs first — the two phases then agree because they read the
// same predecessor state.
func (s *Segmenter) Commit(in StepInput) StepBoundary {
	idx := s.seq
	s.seq++
	if in.Manifest == nil {
		return StepBoundary{Parent: -1}
	}
	if !s.predReady {
		s.Pred(in.Manifest, in.StitchBoundary)
	} else {
		s.lineageOpened = s.lineageOpened || in.StitchBoundary
	}
	defer func() { s.predReady = false }()

	cur := in.Manifest
	var b StepBoundary
	switch {
	case s.lineageOpened:
		// First structurally-comparable record of a lineage: the whole
		// request is new. At a stitched boundary the stitch evidence decides
		// instead (a stitch is not automatically a task boundary — only a
		// genuinely new instruction bridged across it is). A lineage-opening
		// compaction record produces the same zero boundary but does not
		// become the tracking predecessor.
		newTask := true
		humanInitiated := true
		if in.StitchBoundary {
			newTask = in.StitchNewTask
			humanInitiated = in.StitchNewTask
		}
		// Step.Instruction stays empty at a stitch boundary (and for a
		// lineage-opening compaction record): the opening instruction is
		// already surfaced via the Task title.
		instr := ""
		if !in.Compaction && !in.StitchBoundary {
			instr = LastInstruction(in.RealUsers, 0)
		}
		b = StepBoundary{NewTask: newTask, HumanInitiated: humanInitiated, Instruction: instr, Parent: -1}
	default:
		e := s.edit
		deltaStart := cur.LeadSys + e.LCP
		sysChanged := ManifestSysChanged(cur, s.predM)
		traceChanged := cur.TraceID != "" && s.predM.TraceID != "" && cur.TraceID != s.predM.TraceID
		hasNewInstr := HasNewInstruction(in.RealUsers, ManifestKeySet(s.predM), cur, deltaStart, in.TotalMsgs)
		// A no-reply predecessor means the parent turn was deliberately
		// skipped, so this record's instruction is a retry of the same user
		// intent, not a fresh one (a user types once → one task).
		newTask := IsNewTask(traceChanged, s.last.NoReply, hasNewInstr)
		instr := ""
		if hasNewInstr {
			instr = LastInstruction(in.RealUsers, deltaStart)
		}
		b = StepBoundary{
			DeltaStart:     deltaStart,
			LCP:            e.LCP,
			NewTask:        newTask,
			HumanInitiated: hasNewInstr,
			Instruction:    instr,
			SysChanged:     sysChanged,
			Parent:         s.predIdx,
			ReplacedTail:   len(s.predM.Keys) - e.LCP,
		}
	}

	if s.lineageOpened {
		// Reset at every lineage boundary (including a stitched one): the
		// next intra-lineage step compares against THIS record, never
		// across the break.
		s.last, s.hasLast, s.lastIdx = StepInput{}, false, -1
	}
	if !in.Compaction {
		s.last, s.hasLast, s.lastIdx = in, true, idx
	}
	return b
}

// Segment runs the full assembly over a sequence of turns (one lineage, or a
// stitched lineage chain in ctxgraph.ChainFrom order).
func Segment(inputs []StepInput) []StepBoundary {
	seg := NewSegmenter()
	res := make([]StepBoundary, len(inputs))
	for i, in := range inputs {
		res[i] = seg.Commit(in)
	}
	return res
}

// NewSegmenter initializes an empty assembly state.
func NewSegmenter() *Segmenter { return &Segmenter{} }
