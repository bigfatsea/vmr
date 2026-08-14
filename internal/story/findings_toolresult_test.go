// Ver 2026-08-05, by Sonnet 5

package story

import (
	"reflect"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func TestDetectUnadaptedRetry(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "run the build")
	toolUse := map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "tu1", "name": "bash", "input": map[string]any{"cmd": "go build"}},
	}}
	toolResultErr := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "tu1", "is_error": true, "content": "build failed"},
	}}

	t.Run("verbatim retry after an error: fires", func(t *testing.T) {
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build"}`}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResultErr}, sseToolCalls([]any{
			map[string]any{"id": "tu2", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build"}`}},
		}))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingUnadaptedRetry {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected error_retry_unadapted finding")
		}
		if found.StepSeq != 2 {
			t.Errorf("StepSeq = %d, want 2 (the retrying step)", found.StepSeq)
		}
		if !reflect.DeepEqual(found.RelatedSeq, []int{1}) {
			t.Errorf("RelatedSeq = %v, want [1] (the erroring step)", found.RelatedSeq)
		}
	})

	t.Run("retry with adjusted arguments: no finding", func(t *testing.T) {
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build"}`}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResultErr}, sseToolCalls([]any{
			map[string]any{"id": "tu2", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build -v"}`}},
		}))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnadaptedRetry {
				t.Fatalf("unexpected finding: retry arguments changed: %+v", f)
			}
		}
	})

	t.Run("no error at all: no finding", func(t *testing.T) {
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build"}`}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "ack")}, sseToolCalls([]any{
			map[string]any{"id": "tu2", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build"}`}},
		}))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnadaptedRetry {
				t.Fatalf("unexpected finding: no error was ever reported: %+v", f)
			}
		}
	})
}

func TestDetectUnusedToolResult(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "back up the data directory")

	t.Run("entity in the result never referenced again: fires", func(t *testing.T) {
		toolUse := map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "tu1", "name": "list_files", "input": map[string]any{}},
		}}
		toolResult := map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "found archive.backup.tar.gz in the directory"},
		}}
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "list_files", "arguments": "{}"}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResult}, sseText("done, moving on to the next task"))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingUnusedToolResult {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected unused_tool_result finding")
		}
		if found.StepSeq != 1 {
			t.Errorf("StepSeq = %d, want 1 (the calling step)", found.StepSeq)
		}
	})

	t.Run("entity referenced later: no finding", func(t *testing.T) {
		toolUse := map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "tu1", "name": "list_files", "input": map[string]any{}},
		}}
		toolResult := map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "found archive.backup.tar.gz in the directory"},
		}}
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "list_files", "arguments": "{}"}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResult}, sseText("now restoring from archive.backup.tar.gz"))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnusedToolResult {
				t.Fatalf("unexpected finding: entity was referenced in the very next response: %+v", f)
			}
		}
	})

	t.Run("directory-listing-shaped result: only SOME entities used: no finding", func(t *testing.T) {
		// Calibration regression (logs/vmr-audit-2026-07-1x/2x): the first
		// version fired per unused entity, so a directory listing naming
		// dozens of files — of which an agent normally follows up on only
		// a handful by design — produced ~40 findings per affected
		// Journey. Only firing when the ENTIRE result went unreferenced
		// fixes this: using even one of several listed files is ordinary
		// behavior, not a wasted result.
		toolUse := map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "tu1", "name": "list_files", "input": map[string]any{}},
		}}
		toolResult := map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "found a.md, b.md, c.md, d.md in the directory"},
		}}
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "list_files", "arguments": "{}"}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResult}, sseText("reading b.md now, the rest look irrelevant"))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnusedToolResult {
				t.Fatalf("unexpected finding: b.md (one of four listed files) was used, the rest being unused is ordinary triage: %+v", f)
			}
		}
	})
}

func TestDetectUnverifiedEntityReference(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "update the config")
	toolUse := map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "tu1", "name": "read", "input": map[string]any{"path": "missing.txt"}},
	}}
	toolResultNotFound := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "ENOENT: missing.txt not found"},
	}}

	t.Run("falsified entity still referenced later: fires", func(t *testing.T) {
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "read", "arguments": `{"path":"missing.txt"}`}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResultNotFound}, sseToolCalls([]any{
			map[string]any{"id": "tu2", "function": map[string]any{"name": "write", "arguments": `{"path":"missing.txt","content":"x"}`}},
		}))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingUnverifiedEntityReference {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected unverified_entity_reference finding")
		}
		if found.StepSeq != 1 {
			t.Errorf("StepSeq = %d, want 1 (the calling step)", found.StepSeq)
		}
	})

	t.Run("falsified entity never referenced again: no finding", func(t *testing.T) {
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "read", "arguments": `{"path":"missing.txt"}`}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResultNotFound}, sseText("ok, moving on to something unrelated"))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnverifiedEntityReference {
				t.Fatalf("unexpected finding: entity was never referenced again: %+v", f)
			}
		}
	})

	t.Run("no falsification marker: no finding", func(t *testing.T) {
		toolResultOK := map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "contents of missing.txt: hello"},
		}}
		r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
			map[string]any{"id": "tu1", "function": map[string]any{"name": "read", "arguments": `{"path":"missing.txt"}`}},
		}))
		r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResultOK}, sseToolCalls([]any{
			map[string]any{"id": "tu2", "function": map[string]any{"name": "write", "arguments": `{"path":"missing.txt","content":"x"}`}},
		}))
		path := writeJSONL(t, []audit.Record{r1, r2})
		l := onlyLineage(t, path)
		j, err := Build(l, taskseg.Generic, i18n.EN)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got := ComputeFindings(j, i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnverifiedEntityReference {
				t.Fatalf("unexpected finding: the result never reported the entity missing: %+v", f)
			}
		}
	})
}

func TestDetectConstraintTextDropped(t *testing.T) {
	t.Run("swallowed entities present: fires", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1},
			{Seq: 2, Compaction: &CompactionInfo{
				TokensBefore: 1000, TokensAfter: 200,
				SwallowedEntities: []string{"HEARTBEAT.md"}, SurvivedEntities: []string{"AGENTS.md"},
			}},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingConstraintTextDropped {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected constraint_text_dropped_at_compaction finding")
		}
		if found.StepSeq != 2 {
			t.Errorf("StepSeq = %d, want 2", found.StepSeq)
		}
	})

	t.Run("no compaction: no finding", func(t *testing.T) {
		steps := []*Step{{Seq: 1}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingConstraintTextDropped {
				t.Fatalf("unexpected finding: no compaction boundary at all: %+v", f)
			}
		}
	})

	t.Run("compaction with nothing swallowed: no finding", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Compaction: &CompactionInfo{TokensBefore: 100, TokensAfter: 100, SurvivedEntities: []string{"AGENTS.md"}}},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingConstraintTextDropped {
				t.Fatalf("unexpected finding: nothing was swallowed: %+v", f)
			}
		}
	})
}
