// Ver 2026-09-23 08:30, by Claude Opus 5.5

package main

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/report"
	"vmr/internal/taskseg"
)

// TestSegmentParity_ReportAndJourneyAgreeOnCompactionAndMultiTask locks in the
// invariant that report's ReqInfo and journey's Step agree on task boundaries,
// DeltaStart, and instructions across both compaction-interleaved sessions
// and normal multi-task sessions.
func TestSegmentParity_ReportAndJourneyAgreeOnCompactionAndMultiTask(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := func(m, s int) time.Time { return time.Date(2026, 9, 24, 10, m, s, 0, zone) }

	// Session 1: two normal requests with a compaction request in between.
	// r1: normal opening turn
	sys1 := journeyMsg("system", "You are a helpful assistant.")
	u1 := journeyMsg("user", "First ask: please write a sorting algorithm.")
	a1 := journeyMsg("assistant", "Here is merge sort.")
	r1 := journeyRec(at(0, 0), []any{sys1, u1, a1}, journeySSE("done"))
	r1.Client.Request.Body.(map[string]any)["metadata"] = map[string]any{"user_id": "sess_compaction_parity"}

	// comp: compaction call in the same session. Appends onto r1 so they stay in one Lineage.
	// Has summarization system prompt, no tools, max_completion_tokens, empty traceID.
	compSys := journeyMsg("system", "You are a context summarization assistant. Summarize.")
	uMid := journeyMsg("user", "Now optimize it for memory usage.")
	comp := journeyRec(at(1, 0), []any{compSys, u1, a1, uMid}, journeySSE("summary of conversation"))
	compBody := comp.Client.Request.Body.(map[string]any)
	compBody["metadata"] = map[string]any{"user_id": "sess_compaction_parity"}
	compBody["max_completion_tokens"] = 16000

	// r2: continuation after compaction.
	// Contains uMid from comp, followed by assistant tool execution.
	// Report compares r2 against r1 (skipping comp): deltaStart=3, sees uMid, opens Task 2.
	// Journey previously compared r2 against comp: deltaStart=4, misses uMid, treats as continuation!
	a2 := journeyMsg("assistant", "Here is the memory-optimized version.")
	r2 := journeyRec(at(2, 0), []any{sys1, u1, a1, uMid, a2}, journeySSE("done"))
	r2.Client.Request.Body.(map[string]any)["metadata"] = map[string]any{"user_id": "sess_compaction_parity"}

	// Session 2: normal multi-task session (no compaction).
	// s2_r1: Task 1 opening
	sys2 := journeyMsg("system", "You are a coding assistant.")
	s2_u1 := journeyMsg("user", "Task 1: create a web server.")
	s2_a1 := journeyMsg("assistant", "Web server created.")
	s2_r1 := journeyRec(at(10, 0), []any{sys2, s2_u1, s2_a1}, journeySSE("done"))
	s2_r1.Client.Request.Body.(map[string]any)["metadata"] = map[string]any{"user_id": "sess_multitask_parity"}

	// s2_r2: Task 2 opening (new real user instruction)
	s2_u2 := journeyMsg("user", "Task 2: add authentication middleware.")
	s2_a2 := journeyMsg("assistant", "Auth middleware added.")
	s2_r2 := journeyRec(at(11, 0), []any{sys2, s2_u1, s2_a1, s2_u2, s2_a2}, journeySSE("done"))
	s2_r2.Client.Request.Body.(map[string]any)["metadata"] = map[string]any{"user_id": "sess_multitask_parity"}

	// s2_r3: Task 2 continuation (tool loop continuation, no new instruction)
	s2_tool := journeyMsg("assistant", "Checking auth tokens.")
	s2_r3 := journeyRec(at(12, 0), []any{sys2, s2_u1, s2_a1, s2_u2, s2_a2, s2_tool}, journeySSE("tokens valid"))
	s2_r3.Client.Request.Body.(map[string]any)["metadata"] = map[string]any{"user_id": "sess_multitask_parity"}

	allRecs := []audit.Record{r1, comp, r2, s2_r1, s2_r2, s2_r3}
	filePath := writeJourneyJSONL(t, allRecs)

	// 1. Run report side
	repAnalysis, _, err := report.AnalyzeSessionsCached([]string{filePath}, nil, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("report.AnalyzeSessionsCached: %v", err)
	}

	// 2. Run journey side
	g, _, err := ctxgraph.ScanCached([]string{filePath}, nil)
	if err != nil {
		t.Fatalf("ctxgraph.ScanCached: %v", err)
	}
	byIdx := ctxgraph.LineageIndex(g)
	tails := ctxgraph.StitchedSuccessorSet(g)
	var journeys []*journey.Journey
	for _, l := range g.Lineages {
		if tails[l.Idx] {
			continue
		}
		j, err := journey.BuildChain(ctxgraph.ChainFrom(l, byIdx), taskseg.OpenClawAware, i18n.EN)
		if err != nil {
			t.Fatalf("journey.BuildChain: %v", err)
		}
		journeys = append(journeys, j)
	}

	// Map journey steps by line number
	type stepInfo struct {
		step      *journey.Step
		isNewTask bool
		taskTitle string
	}
	journeySteps := make(map[int]stepInfo)
	for _, j := range journeys {
		for _, task := range j.Tasks {
			for idx, step := range task.Steps {
				journeySteps[step.Manifest.Line] = stepInfo{
					step:      step,
					isNewTask: idx == 0,
					taskTitle: task.Title,
				}
			}
		}
	}

	// In report, inspect each session's records
	for _, s := range repAnalysis.Sessions {
		for _, r := range s.Recs {
			js, ok := journeySteps[r.Line]
			if !ok {
				t.Errorf("line %d present in report session %s but missing from journey steps", r.Line, s.ID)
				continue
			}

			// Assert DeltaStart matches
			if r.DeltaStart != js.step.DeltaStart {
				t.Errorf("line %d DeltaStart divergence: report=%d, journey=%d",
					r.Line, r.DeltaStart, js.step.DeltaStart)
			}

			// Assert NewTask boundary matches: in report, r.TaskSeq == 1 means new task
			repNewTask := r.TaskSeq == 1
			if repNewTask != js.isNewTask {
				t.Errorf("line %d NewTask divergence: report=%v, journey=%v",
					r.Line, repNewTask, js.isNewTask)
			}

			// Assert instruction matches when opening a new task (beyond root step)
			if repNewTask && r.SessSeq > 1 {
				if r.NewInstruction != js.step.Instruction {
					t.Errorf("line %d NewInstruction divergence: report=%q, journey=%q",
						r.Line, r.NewInstruction, js.step.Instruction)
				}
			}
		}
	}
}
