// Ver 2026-07-29 23:55, by Sonnet 5

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/story"
)

func writeStoryJSONL(t *testing.T, recs []audit.Record) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func storySSE(text string) string {
	return `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}
data: [DONE]`
}

func storyMsg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

func storyRec(ts time.Time, msgs []any, respBody any) audit.Record {
	body := map[string]any{"model": "agent", "stream": true, "messages": msgs}
	return audit.Record{
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: map[string][]string{}, Body: body},
			Response: &audit.Message{Status: 200, Headers: map[string][]string{}, Body: respBody},
		},
	}
}

// TestCmdStory_ListAndRender exercises the `vmr story` CLI end to end — a
// path design-doc review §6.3 flagged as untested: internal/story's own
// tests cover Build/RenderMarkdown directly, but nothing exercised
// cmd_story.go's flag parsing, candidate listing (batched PreviewTitles),
// or the -journey render-to-file path. Two records sharing the same opening
// user message form one 2-manifest lineage — the minimum ListCandidates
// will offer as a journey.
func TestCmdStory_ListAndRender(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "调研一下 A 股新股打新收益")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("开工"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("完成"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")

	listing := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, path}); err != nil {
			t.Fatalf("cmdStory (list): %v", err)
		}
	})
	if !strings.Contains(listing, "调研一下 A 股新股打新收益") {
		t.Fatalf("listing missing preview title:\n%s", listing)
	}
	if !strings.Contains(listing, "1 candidate journey") {
		t.Errorf("listing should report exactly 1 candidate journey:\n%s", listing)
	}

	var idLine string
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, "调研一下") {
			idLine = line
			break
		}
	}
	if idLine == "" {
		t.Fatalf("could not find the candidate line in listing:\n%s", listing)
	}
	fields := strings.Fields(idLine)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "j-") {
		t.Fatalf("candidate line doesn't start with a journey id (j-...):\n%s", idLine)
	}
	id := fields[0]

	render := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-journey", id, path}); err != nil {
			t.Fatalf("cmdStory (render): %v", err)
		}
	})
	if !strings.Contains(render, "journey-"+id+".md") {
		t.Errorf("render output missing the written file path:\n%s", render)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatalf("reports/stories not created: %v", err)
	}
	// One journey now writes two files: journey-<id>.md (the narrative) and
	// journey-<id>.json (design doc §6.5's behavior profile).
	if len(entries) != 2 {
		t.Fatalf("want 2 story files (.md + .json), got %d: %v", len(entries), entries)
	}
	content, err := os.ReadFile(filepath.Join(outDir, "stories", "journey-"+id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "调研一下 A 股新股打新收益") {
		t.Errorf("rendered journey missing root instruction:\n%s", content)
	}
	jsonData, err := os.ReadFile(filepath.Join(outDir, "stories", "journey-"+id+".json"))
	if err != nil {
		t.Fatalf("journey-%s.json not written: %v", id, err)
	}
	var summary story.JourneySummary
	if err := json.Unmarshal(jsonData, &summary); err != nil {
		t.Fatalf("journey-%s.json is not valid JSON: %v\n%s", id, err, jsonData)
	}
	if summary.ID != id {
		t.Errorf("journey-%s.json's own id field = %q, want %q", id, summary.ID, id)
	}
}

// TestCmdStory_RenderAll covers -render-all: two independent candidate
// lineages must both be rendered in one pass, with no -journey id needed
// (design-doc review follow-up: picking an id by hand for every journey was
// the friction this flag removes).
func TestCmdStory_RenderAll(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")

	uA := storyMsg("user", "调研一下 A 股新股打新收益")
	rA1 := storyRec(at(0), []any{sys, uA}, storySSE("开工"))
	rA2 := storyRec(at(1), []any{sys, uA, storyMsg("assistant", "done")}, storySSE("完成"))

	uB := storyMsg("user", "帮我写个 release note")
	rB1 := storyRec(at(10), []any{sys, uB}, storySSE("好的"))
	rB2 := storyRec(at(11), []any{sys, uB, storyMsg("assistant", "done")}, storySSE("写好了"))

	path := writeStoryJSONL(t, []audit.Record{rA1, rA2, rB1, rB2})
	outDir := filepath.Join(t.TempDir(), "out")

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-render-all", path}); err != nil {
			t.Fatalf("cmdStory -render-all: %v", err)
		}
	})
	if !strings.Contains(out, "2 个 journey 已渲染到") {
		t.Errorf("summary line missing or wrong count:\n%s", out)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatalf("reports/stories not created: %v", err)
	}
	// Two journeys, each now writing a .md + .json pair.
	if len(entries) != 4 {
		t.Fatalf("want 4 story files (2 journeys x .md+.json), got %d: %v", len(entries), entries)
	}
	var all string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(outDir, "stories", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		all += string(content)
	}
	if !strings.Contains(all, "调研一下 A 股新股打新收益") || !strings.Contains(all, "帮我写个 release note") {
		t.Errorf("both journeys' root instructions should appear across the two .md files:\n%s", all)
	}
}

// TestCmdStory_Compare covers Step 4's 4d module: -compare-a/-compare-b
// resolve two candidate journeys by id prefix and write one comparison
// Markdown+JSON pair. Journey B's much larger model time should surface as
// a notable row.
func TestCmdStory_Compare(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")

	uA := storyMsg("user", "调研一下 A 股新股打新收益")
	rA1 := storyRec(at(0), []any{sys, uA}, storySSE("开工"))
	rA2 := storyRec(at(1), []any{sys, uA, storyMsg("assistant", "done")}, storySSE("完成"))

	uB := storyMsg("user", "帮我写个 release note")
	rB1 := storyRec(at(10), []any{sys, uB}, storySSE("好的"))
	rB2 := storyRec(at(11), []any{sys, uB, storyMsg("assistant", "done")}, storySSE("写好了"))

	path := writeStoryJSONL(t, []audit.Record{rA1, rA2, rB1, rB2})
	outDir := filepath.Join(t.TempDir(), "out")

	listing := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, path}); err != nil {
			t.Fatalf("cmdStory (list): %v", err)
		}
	})
	var idA, idB string
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "j-") {
			continue
		}
		if strings.Contains(line, "调研一下") {
			idA = fields[0]
		} else if strings.Contains(line, "release note") {
			idB = fields[0]
		}
	}
	if idA == "" || idB == "" {
		t.Fatalf("could not find both candidate ids in listing:\n%s", listing)
	}

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-compare-a", idA, "-compare-b", idB, path}); err != nil {
			t.Fatalf("cmdStory -compare-a/-compare-b: %v", err)
		}
	})
	wantBase := "compare-" + idA + "-vs-" + idB
	if !strings.Contains(out, wantBase+".md") {
		t.Errorf("output missing the written comparison path:\n%s", out)
	}

	mdData, err := os.ReadFile(filepath.Join(outDir, "stories", wantBase+".md"))
	if err != nil {
		t.Fatalf("comparison .md not written: %v", err)
	}
	md := string(mdData)
	for _, want := range []string{idA, idB, "调研一下 A 股新股打新收益", "帮我写个 release note", "模型时间"} {
		if !strings.Contains(md, want) {
			t.Errorf("comparison markdown missing %q:\n%s", want, md)
		}
	}

	jsonData, err := os.ReadFile(filepath.Join(outDir, "stories", wantBase+".json"))
	if err != nil {
		t.Fatalf("comparison .json not written: %v", err)
	}
	var cmp story.Comparison
	if err := json.Unmarshal(jsonData, &cmp); err != nil {
		t.Fatalf("comparison .json is not valid JSON: %v\n%s", err, jsonData)
	}
	if cmp.A.ID != idA || cmp.B.ID != idB {
		t.Errorf("comparison json ids = %q/%q, want %q/%q", cmp.A.ID, cmp.B.ID, idA, idB)
	}
	if len(cmp.Rows) == 0 {
		t.Error("comparison json has no metric rows")
	}
}

// TestCmdStory_CompareRequiresBothSides covers the -compare-a-without-
// -compare-b (and vice versa) usage error.
func TestCmdStory_CompareRequiresBothSides(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "hello")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("a"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("b"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", outDir, "-compare-a", "j-something", path})
	})
	if err == nil {
		t.Error("-compare-a without -compare-b should be a usage error")
	}
}

// TestCmdStory_CompareUnknownID covers -compare-a/-compare-b each reporting
// their own side when an id prefix doesn't match any candidate — the error
// must name which of the two flags failed, not just "no journey found".
func TestCmdStory_CompareUnknownID(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "hello")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("a"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("b"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", outDir, "-compare-a", "no-such-id", "-compare-b", "j-", path})
	})
	if err == nil || !strings.Contains(err.Error(), "-compare-a") {
		t.Errorf("expected a -compare-a error naming that flag, got: %v", err)
	}
}

// TestCmdStory_ComparePartialGating covers compareJourneys' own
// -include-partial gate: a partial-head candidate on either side must be
// rejected the same way a single -journey render is, and accepted (with the
// "-partial" filename suffix) once -include-partial is passed.
func TestCmdStory_ComparePartialGating(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")

	// Partial candidate: looks mid-conversation already (>2 non-system keys)
	// within the first lines of the only loaded file — same fixture shape as
	// TestCmdStory_PartialHeadFilenameSuffix.
	u1 := storyMsg("user", "第一轮指令")
	a1 := storyMsg("assistant", "第一轮回复")
	u2 := storyMsg("user", "第二轮追问")
	rPartial1 := storyRec(at(0), []any{sys, u1, a1, u2}, storySSE("continuing"))
	rPartial2 := storyRec(at(1), []any{sys, u1, a1, u2, storyMsg("assistant", "第二轮回复")}, storySSE("done"))

	uB := storyMsg("user", "一个普通的新任务")
	rB1 := storyRec(at(10), []any{sys, uB}, storySSE("好的"))
	rB2 := storyRec(at(11), []any{sys, uB, storyMsg("assistant", "done")}, storySSE("写好了"))

	path := writeStoryJSONL(t, []audit.Record{rPartial1, rPartial2, rB1, rB2})
	outDir := filepath.Join(t.TempDir(), "out")

	listing := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-include-partial", path}); err != nil {
			t.Fatalf("cmdStory (list -include-partial): %v", err)
		}
	})
	var idPartial, idB string
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "j-") {
			continue
		}
		if strings.Contains(line, "第一轮指令") {
			idPartial = fields[0]
		} else if strings.Contains(line, "一个普通的新任务") {
			idB = fields[0]
		}
	}
	if idPartial == "" || idB == "" {
		t.Fatalf("could not find both candidate ids in listing:\n%s", listing)
	}

	if err := cmdStory([]string{"-o", outDir, "-compare-a", idPartial, "-compare-b", idB, path}); err == nil {
		t.Error("comparing a partial-head journey without -include-partial should error")
	}

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-include-partial", "-compare-a", idPartial, "-compare-b", idB, path}); err != nil {
			t.Fatalf("cmdStory -compare with -include-partial: %v", err)
		}
	})
	if !strings.Contains(out, "-partial.md") {
		t.Errorf("comparison output should carry the -partial suffix when a side is partial-head:\n%s", out)
	}
}

// TestCmdStory_ShowUngrouped covers -show-ungrouped: a record with no
// non-system messages and no metadata.user_id gets no SessKey at all
// (ctxgraph.Graph.Ungrouped), and -show-ungrouped must print its source
// location.
func TestCmdStory_ShowUngrouped(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sysOnly := storyRec(at(0), []any{storyMsg("system", "sys, nothing else")}, storySSE("ok"))
	path := writeStoryJSONL(t, []audit.Record{sysOnly})
	outDir := filepath.Join(t.TempDir(), "out")

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-show-ungrouped", path}); err != nil {
			t.Fatalf("cmdStory -show-ungrouped: %v", err)
		}
	})
	if !strings.Contains(out, "1 ungrouped record") {
		t.Errorf("expected 1 ungrouped record reported:\n%s", out)
	}
	if !strings.Contains(out, "未归组记录") || !strings.Contains(out, filepath.Base(path)) {
		t.Errorf("-show-ungrouped should print the record's source location:\n%s", out)
	}
}

// TestCmdStory_NoInputFiles mirrors TestCmdReport_NoInputFiles: `vmr story`
// with no positional args is a usage error, not a silent no-op.
func TestCmdStory_NoInputFiles(t *testing.T) {
	if err := cmdStory(nil); err == nil {
		t.Error("cmdStory with no input files should return an error")
	}
}

// TestCmdStory_UnknownJourney covers the -journey-with-no-match error path.
func TestCmdStory_UnknownJourney(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "hello")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("a"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("b"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", outDir, "-journey", "no-such-id", path})
	})
	if err == nil {
		t.Error("cmdStory -journey with an unmatched id prefix should return an error")
	}
}

// TestCmdStory_PartialHeadFilenameSuffix covers design doc §11 D1's fix: a
// head-truncated Journey's rendered filename must self-disclose that its ID
// isn't stable, via a "-partial" suffix, without requiring the reader to
// open the file and find the warning line first. The first record already
// carries a multi-turn-looking manifest (sys + 2 user/assistant pairs) at
// line 0 of the only loaded file — story.IsPartialHead's signal for "this
// conversation's real opening lives outside the loaded range".
func TestCmdStory_PartialHeadFilenameSuffix(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "第一轮指令")
	a1 := storyMsg("assistant", "第一轮回复")
	u2 := storyMsg("user", "第二轮追问")
	r1 := storyRec(at(0), []any{sys, u1, a1, u2}, storySSE("continuing"))
	r2 := storyRec(at(1), []any{sys, u1, a1, u2, storyMsg("assistant", "第二轮回复")}, storySSE("done"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")

	// Without -include-partial, the candidate is skipped and -render-all
	// writes nothing.
	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-render-all", path}); err != nil {
			t.Fatalf("cmdStory -render-all (no -include-partial): %v", err)
		}
	})
	if !strings.Contains(out, "skipped as partial-head") {
		t.Errorf("expected the candidate to be skipped as partial-head:\n%s", out)
	}
	if entries, _ := os.ReadDir(filepath.Join(outDir, "stories")); len(entries) != 0 {
		t.Fatalf("no files should be written without -include-partial, got %v", entries)
	}

	// With -include-partial, it renders — and the filename must carry the
	// "-partial" suffix.
	out = captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-render-all", "-include-partial", path}); err != nil {
			t.Fatalf("cmdStory -render-all -include-partial: %v", err)
		}
	})
	if !strings.Contains(out, "-partial.md") {
		t.Errorf("render output should mention the -partial.md filename:\n%s", out)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatalf("reports/stories not created: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 story files (.md + .json), got %d: %v", len(entries), entries)
	}
	for _, e := range entries {
		if !strings.Contains(e.Name(), "-partial.") {
			t.Errorf("file %s missing the -partial suffix", e.Name())
		}
	}
}

func captureStdoutErr(t *testing.T, fn func() error) error {
	t.Helper()
	var err error
	captureStdout(t, func() { err = fn() })
	return err
}
