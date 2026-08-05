// Ver 2026-07-29 23:55, by Sonnet 5

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
// path flagged as untested: internal/story's own
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
	// journey-<id>.json (the behavior profile) — plus vmr-stories.json/.md,
	// written on every invocation (the earlier bare listing call already
	// wrote them, this render just updates them in place).
	if len(entries) != 4 {
		t.Fatalf("want 4 files (journey .md+.json, vmr-stories .md+.json), got %d: %v", len(entries), entries)
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
	if !strings.Contains(out, "2 journey(s) rendered to") {
		t.Errorf("summary line missing or wrong count:\n%s", out)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatalf("reports/stories not created: %v", err)
	}
	// Two journeys, each writing a .md + .json pair, plus vmr-stories.json/.md.
	if len(entries) != 6 {
		t.Fatalf("want 6 files (2 journeys x .md+.json, vmr-stories .md+.json), got %d: %v", len(entries), entries)
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

// TestCmdStory_Compare covers Step 4's 4d module: -compare id1,id2 resolves
// two candidate journeys by id prefix and writes one comparison
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
		if err := cmdStory([]string{"-o", outDir, "-compare", idA + "," + idB, path}); err != nil {
			t.Fatalf("cmdStory -compare: %v", err)
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
	for _, want := range []string{idA, idB, "调研一下 A 股新股打新收益", "帮我写个 release note", "Model Time", "Evidence Provenance", path} {
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
	// Evidence-provenance addition: Extras.Sources must carry the resolved
	// input path(s) this comparison was built from, not just be left empty —
	// otherwise the "证据溯源" text above would be asserting against a
	// section that silently renders nothing.
	if cmp.Extras == nil || len(cmp.Extras.Sources) != 1 || cmp.Extras.Sources[0] != path {
		t.Errorf("comparison json Extras.Sources = %+v, want [%q]", extrasSources(cmp), path)
	}
}

func extrasSources(cmp story.Comparison) []string {
	if cmp.Extras == nil {
		return nil
	}
	return cmp.Extras.Sources
}

// TestCmdStory_CompareRequiresTwoIDs covers the usage error when -compare
// isn't given exactly two comma-separated ids (missing second id, or a
// trailing/leading empty one from a stray comma).
func TestCmdStory_CompareRequiresTwoIDs(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "hello")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("a"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("b"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	for _, val := range []string{"j-something", "j-something,", ",j-something"} {
		err := captureStdoutErr(t, func() error {
			return cmdStory([]string{"-o", outDir, "-compare", val, path})
		})
		if err == nil {
			t.Errorf("-compare %q should be a usage error", val)
		}
	}
}

// TestCmdStory_CompareUnknownID covers -compare id1,id2 reporting which side
// failed to resolve when an id prefix doesn't match any candidate — the
// error must name whether it's the first or second id, not just "no journey
// found".
func TestCmdStory_CompareUnknownID(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "hello")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("a"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("b"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", outDir, "-compare", "no-such-id,j-", path})
	})
	if err == nil || !strings.Contains(err.Error(), "-compare first id") {
		t.Errorf("expected a -compare first id error, got: %v", err)
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

	if err := cmdStory([]string{"-o", outDir, "-compare", idPartial + "," + idB, path}); err == nil {
		t.Error("comparing a partial-head journey without -include-partial should error")
	}

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-include-partial", "-compare", idPartial + "," + idB, path}); err != nil {
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
	if !strings.Contains(out, "ungrouped record") || !strings.Contains(out, filepath.Base(path)) {
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

// TestCmdStory_PartialHeadFilenameSuffix covers the fix: a
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
	// vmr-stories.json/.md are still written (every invocation gets one),
	// but no journey-*.md/.json — the partial-head candidate was skipped.
	entries, _ := os.ReadDir(filepath.Join(outDir, "stories"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "journey-") {
			t.Errorf("no journey file should be written without -include-partial, got %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories", "vmr-stories.json")); err != nil {
		t.Errorf("vmr-stories.json should still be written: %v", err)
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
	var journeyFiles []os.DirEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "journey-") {
			journeyFiles = append(journeyFiles, e)
		}
	}
	if len(journeyFiles) != 2 {
		t.Fatalf("want 2 journey story files (.md + .json), got %d: %v", len(journeyFiles), entries)
	}
	for _, e := range journeyFiles {
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

// captureStderr runs fn with os.Stderr redirected and returns what it wrote
// — the -llm-* degradation tests assert on the warning text cmdStory prints
// there (design doc C.7: a failed LLM call must warn, not fail the command).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// writeTwoCandidateJourneys builds a minimal two-journey audit log (same
// shape TestCmdStory_Compare uses) and returns its path plus both journeys'
// ids, resolved by listing once — shared setup for the -llm-* CLI tests
// below, which only care about the compare/LLM plumbing, not journey
// construction itself.
func writeTwoCandidateJourneys(t *testing.T, outDir string) (path, idA, idB string) {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	uA := storyMsg("user", "调研一下 A 股新股打新收益")
	rA1 := storyRec(at(0), []any{sys, uA}, storySSE("开工"))
	rA2 := storyRec(at(1), []any{sys, uA, storyMsg("assistant", "done")}, storySSE("完成"))
	uB := storyMsg("user", "帮我写个 release note")
	rB1 := storyRec(at(10), []any{sys, uB}, storySSE("好的"))
	rB2 := storyRec(at(11), []any{sys, uB, storyMsg("assistant", "done")}, storySSE("写好了"))
	path = writeStoryJSONL(t, []audit.Record{rA1, rA2, rB1, rB2})

	// The id-discovery listing deliberately runs against its own scratch
	// -o, not the caller's outDir: since vmr-stories.json/.md are now
	// written on every invocation (including a bare listing), reusing
	// outDir here would leave reports/stories/ already populated before
	// the caller's own cmdStory call — which some callers (the -llm-dry-run
	// tests) specifically assert creates nothing.
	listing := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", filepath.Join(t.TempDir(), "discover"), path}); err != nil {
			t.Fatalf("cmdStory (list): %v", err)
		}
	})
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
	return path, idA, idB
}

// TestCmdStory_LLMFlagValidation covers resolveLLMOptions' guard rails: the
// -llm-* flag combinations that must be rejected before anything is scanned.
func TestCmdStory_LLMFlagValidation(t *testing.T) {
	path, idA, idB := writeTwoCandidateJourneys(t, filepath.Join(t.TempDir(), "out"))
	compareArgs := []string{"-compare", idA + "," + idB, path}

	cases := map[string][]string{
		"-llm-dry-run without -llm-addr":           append([]string{"-llm-dry-run"}, compareArgs...),
		"-llm-model without -llm-addr":             append([]string{"-llm-model", "agent"}, compareArgs...),
		"-llm-key without -llm-addr":               append([]string{"-llm-key", "sk-x"}, compareArgs...),
		"-llm-addr without -llm-model or -dry-run": append([]string{"-llm-addr", "127.0.0.1:1"}, compareArgs...),
	}
	for name, args := range cases {
		if err := captureStdoutErr(t, func() error { return cmdStory(args) }); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}

	// -llm-addr WITH -journey is 5.9 — allowed (not rejected the way -compare's
	// restriction used to blanket-reject -journey/-render-all together). A
	// connection failure (127.0.0.1:1 refuses every connection) must degrade
	// away per design doc C.7, not fail the command — this call returning nil
	// is itself the regression guard against silently reintroducing the old
	// "-llm-addr is only supported with -compare" rejection.
	if err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", filepath.Join(t.TempDir(), "out2"), "-journey", idA, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", path})
	}); err != nil {
		t.Errorf("-llm-addr with -journey should degrade away on connection failure, not fail the command: %v", err)
	}

	// -llm-addr with -render-all must still be rejected — one LLM call per
	// rendered journey in a batch pass is a different cost profile than a
	// single -journey/-compare call, deliberately not supported.
	if err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", filepath.Join(t.TempDir(), "out3"), "-render-all", "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", path})
	}); err == nil {
		t.Error("-llm-addr with -render-all should be rejected")
	}

	// -llm-addr with -corpus must be rejected the same way as -render-all —
	// same "one LLM call per journey in a batch pass" cost-profile reasoning.
	if err := captureStdoutErr(t, func() error {
		return cmdStory([]string{"-o", filepath.Join(t.TempDir(), "out4"), "-corpus", "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", path})
	}); err == nil {
		t.Error("-llm-addr with -corpus should be rejected")
	}
}

// TestCmdStory_CompareLLMDryRun covers -llm-dry-run: it must print a size
// estimate and return before writing anything, and must never dial the
// given address (127.0.0.1:1 refuses every connection on virtually every
// system — if the dry run actually tried to connect, this test would fail
// with a connection-refused error surfacing as a non-nil return).
func TestCmdStory_CompareLLMDryRun(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-compare", idA + "," + idB, "-llm-addr", "127.0.0.1:1", "-llm-dry-run", path}); err != nil {
			t.Fatalf("cmdStory -llm-dry-run: %v", err)
		}
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("dry-run output missing the size estimate line: %q", out)
	}
	base := "compare-" + idA + "-vs-" + idB
	if _, err := os.Stat(filepath.Join(outDir, "stories", base+".md")); err == nil {
		t.Error("-llm-dry-run should return before writing the compare .md")
	}
	// -llm-dry-run must not leave even an empty stories/ directory behind —
	// code-review finding: ensureStoriesDir used to run before the dry-run
	// check, so a "dry run" and "not configured" left different filesystem
	// state even though both should be pure no-ops.
	if _, err := os.Stat(filepath.Join(outDir, "stories")); err == nil {
		t.Error("-llm-dry-run should not create reports/stories/ at all")
	}
}

// TestCmdStory_CompareWithLLM covers the full path: a real (mock) VMR
// endpoint, the rendered .md gaining the "## LLM Interpretation" section with the
// mock's reply, and a cache file appearing under stories/.llm-cache.
func TestCmdStory_CompareWithLLM(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "一句话结论：这是 mock 的解读内容。"}},
			},
		})
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)
	cacheDir := filepath.Join(outDir, "stories", ".llm-cache")

	if err := cmdStory([]string{"-o", outDir, "-compare", idA + "," + idB, "-llm-addr", addr, "-llm-model", "agent", "-llm-cache-dir", cacheDir, path}); err != nil {
		t.Fatalf("cmdStory -llm-addr: %v", err)
	}
	base := "compare-" + idA + "-vs-" + idB
	mdData, err := os.ReadFile(filepath.Join(outDir, "stories", base+".md"))
	if err != nil {
		t.Fatalf("comparison .md not written: %v", err)
	}
	md := string(mdData)
	for _, want := range []string{"## LLM Interpretation", "一句话结论：这是 mock 的解读内容。", "not the fact layer"} {
		if !strings.Contains(md, want) {
			t.Errorf("comparison markdown missing %q:\n%s", want, md)
		}
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected at least one cache file under %s: %v", cacheDir, err)
	}
}

// TestCmdStory_NoLLMCacheDirConfiguredMeansNoCaching covers the explicit
// behavior change: -llm-cache-dir has no implicit default (unlike the old
// hardcoded {out}/stories/.llm-cache) — with neither the flag nor
// report.yaml's llm_cache_dir set, an -llm-addr call must still succeed
// (the LLM section renders) but must leave no .llm-cache directory behind
// anywhere under outDir.
func TestCmdStory_NoLLMCacheDirConfiguredMeansNoCaching(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "一句话结论：无缓存路径。"}},
			},
		})
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)

	if err := cmdStory([]string{"-o", outDir, "-journey", idA, "-llm-addr", addr, "-llm-model", "agent", path}); err != nil {
		t.Fatalf("cmdStory -journey -llm-addr (no -llm-cache-dir): %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "stories", "journey-"+idA+".md"))
	if err != nil {
		t.Fatalf("journey .md not written: %v", err)
	}
	if !strings.Contains(string(mdData), "一句话结论：无缓存路径。") {
		t.Error("LLM section should still render even with no cache configured")
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories", ".llm-cache")); err == nil {
		t.Error("no .llm-cache directory should exist when -llm-cache-dir is unset both on the CLI and in report.yaml")
	}
}

// TestCmdStory_ReportYamlProvidesLLMDefaults covers report.yaml's
// llm_addr/llm_model/llm_cache_dir feeding -journey's LLM interpretation
// layer when the corresponding -llm-* flags aren't passed at all — the same
// merge order TestCmdReport_ReportYamlDefaultsOutputAndDetails covers for
// -o/-details.
func TestCmdStory_ReportYamlProvidesLLMDefaults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "一句话结论：来自 report.yaml 的配置。"}},
			},
		})
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)
	cacheDir := filepath.Join(t.TempDir(), "llmcache")

	reportConfigPath := filepath.Join(t.TempDir(), "report.yaml")
	yaml := "llm_addr: " + addr + "\nllm_model: agent\nllm_cache_dir: " + cacheDir + "\n"
	if err := os.WriteFile(reportConfigPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := cmdStory([]string{"-o", outDir, "-journey", idA, "-report-config", reportConfigPath, path}); err != nil {
		t.Fatalf("cmdStory -journey (llm settings from report.yaml): %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "stories", "journey-"+idA+".md"))
	if err != nil {
		t.Fatalf("journey .md not written: %v", err)
	}
	if !strings.Contains(string(mdData), "一句话结论：来自 report.yaml 的配置。") {
		t.Error("report.yaml's llm_addr/llm_model should have enabled the LLM interpretation section")
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected at least one cache file under report.yaml's llm_cache_dir %s: %v", cacheDir, err)
	}
}

// TestCmdStory_Corpus covers -corpus: two independent candidate journeys
// must produce vmr-story-corpus.md + .json under {outDir}/stories, and the
// "no candidates" path (an audit log that groups into zero lineages at all)
// must return without error and without writing either file, matching
// corpusStats' own len(toRender)==0 early return.
func TestCmdStory_Corpus(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, _, _ := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-corpus", path}); err != nil {
			t.Fatalf("cmdStory -corpus: %v", err)
		}
	})
	if !strings.Contains(out, "2 journey(s) analyzed") {
		t.Errorf("summary line missing or wrong journey count:\n%s", out)
	}

	mdPath := filepath.Join(outDir, "stories", "vmr-story-corpus.md")
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("vmr-story-corpus.md not written: %v", err)
	}
	if len(mdData) == 0 {
		t.Error("vmr-story-corpus.md is empty")
	}

	jsonPath := filepath.Join(outDir, "stories", "vmr-story-corpus.json")
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("vmr-story-corpus.json not written: %v", err)
	}
	var stats story.CorpusStats
	if err := json.Unmarshal(jsonData, &stats); err != nil {
		t.Fatalf("vmr-story-corpus.json is not valid JSON: %v\n%s", err, jsonData)
	}
	if stats.JourneyCount != 2 {
		t.Errorf("stats.JourneyCount = %d, want 2", stats.JourneyCount)
	}
}

// TestCmdStory_CorpusNoCandidates covers corpusStats' own early return when
// there are zero candidate journeys to analyze (here: a single record with
// no non-system messages, which ctxgraph groups into Ungrouped rather than
// any Lineage at all — same fixture shape as TestCmdStory_ShowUngrouped).
// The command must not error, and must not write vmr-story-corpus.md/.json
// (nothing to analyze) — but vmr-stories.json/.md still get written, same
// as every other invocation (an empty candidate list is still a real,
// worth-recording result, unlike -llm-dry-run's "should I even run this"
// pure query, which is why that one still leaves no directory at all).
func TestCmdStory_CorpusNoCandidates(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sysOnly := storyRec(at(0), []any{storyMsg("system", "sys, nothing else")}, storySSE("ok"))
	path := writeStoryJSONL(t, []audit.Record{sysOnly})
	outDir := filepath.Join(t.TempDir(), "out")

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-corpus", path}); err != nil {
			t.Fatalf("cmdStory -corpus (no candidates): %v", err)
		}
	})
	if !strings.Contains(out, "no candidate journeys to analyze") {
		t.Errorf("expected the no-candidates message:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories", "vmr-story-corpus.md")); err == nil {
		t.Error("-corpus with zero candidates should not write vmr-story-corpus.md")
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories", "vmr-stories.json")); err != nil {
		t.Errorf("vmr-stories.json should still be written even with zero candidates: %v", err)
	}
}

// TestCmdStory_CorpusExclusivity covers the exclusivity check that rejects
// -corpus combined with -journey/-render-all/-compare — each must be
// rejected before any input file is even scanned, and must leave no
// reports/stories/ directory behind.
func TestCmdStory_CorpusExclusivity(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "hello")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("a"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("b"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	cases := map[string][]string{
		"-corpus with -journey":    {"-corpus", "-journey", "j-something"},
		"-corpus with -render-all": {"-corpus", "-render-all"},
		"-corpus with -compare":    {"-corpus", "-compare", "a,b"},
	}
	for name, extra := range cases {
		outDir := filepath.Join(t.TempDir(), "out")
		args := append(append([]string{"-o", outDir}, extra...), path)
		err := captureStdoutErr(t, func() error { return cmdStory(args) })
		if err == nil {
			t.Errorf("%s: expected an error, got none", name)
			continue
		}
		wantErr := "-corpus is exclusive with -journey/-render-all/-compare — run it on its own"
		if err.Error() != wantErr {
			t.Errorf("%s: error = %q, want %q", name, err.Error(), wantErr)
		}
		if _, statErr := os.Stat(filepath.Join(outDir, "stories")); statErr == nil {
			t.Errorf("%s: reports/stories/ should not be created when the exclusivity check rejects the args", name)
		}
	}
}

// TestCmdStory_JourneyWithLLM mirrors TestCmdStory_CompareWithLLM but for
// -journey: a real (mock) VMR endpoint, the rendered journey .md gaining the
// "## LLM Interpretation" section with the mock's reply, and a cache file
// appearing under stories/.llm-cache.
func TestCmdStory_JourneyWithLLM(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "一句话结论：这是 mock 的单journey解读内容。"}},
			},
		})
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)
	cacheDir := filepath.Join(outDir, "stories", ".llm-cache")

	if err := cmdStory([]string{"-o", outDir, "-journey", idA, "-llm-addr", addr, "-llm-model", "agent", "-llm-cache-dir", cacheDir, path}); err != nil {
		t.Fatalf("cmdStory -journey -llm-addr: %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "stories", "journey-"+idA+".md"))
	if err != nil {
		t.Fatalf("journey .md not written: %v", err)
	}
	md := string(mdData)
	for _, want := range []string{"## LLM Interpretation", "一句话结论：这是 mock 的单journey解读内容。", "not the fact layer"} {
		if !strings.Contains(md, want) {
			t.Errorf("journey markdown missing %q:\n%s", want, md)
		}
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected at least one cache file under %s: %v", cacheDir, err)
	}
}

// TestCmdStory_JourneyLLMDryRun mirrors TestCmdStory_CompareLLMDryRun but
// for -journey: -llm-dry-run must print a size estimate and return before
// writing anything (including reports/stories/ itself), and must never dial
// the given address.
func TestCmdStory_JourneyLLMDryRun(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, "-journey", idA, "-llm-addr", "127.0.0.1:1", "-llm-dry-run", path}); err != nil {
			t.Fatalf("cmdStory -journey -llm-dry-run: %v", err)
		}
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("dry-run output missing the size estimate line: %q", out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories", "journey-"+idA+".md")); err == nil {
		t.Error("-llm-dry-run should return before writing the journey .md")
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories")); err == nil {
		t.Error("-llm-dry-run should not create reports/stories/ at all")
	}
}

// TestCmdStory_CompareLLMFailureDegrades covers design doc C.7's "the whole
// layer degrades away" rule: an unreachable -llm-addr must not fail the
// -compare command — the .md/.json still get written, just without the LLM
// section, and a warning goes to stderr.
func TestCmdStory_CompareLLMFailureDegrades(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	var cmdErr error
	stderr := captureStderr(t, func() {
		cmdErr = cmdStory([]string{"-o", outDir, "-compare", idA + "," + idB, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", path})
	})
	if cmdErr != nil {
		t.Fatalf("cmdStory should not fail when the LLM endpoint is unreachable: %v", cmdErr)
	}
	if !strings.Contains(stderr, "warning") {
		t.Errorf("expected a warning on stderr about the failed LLM call, got: %q", stderr)
	}

	base := "compare-" + idA + "-vs-" + idB
	mdData, err := os.ReadFile(filepath.Join(outDir, "stories", base+".md"))
	if err != nil {
		t.Fatalf("comparison .md should still be written: %v", err)
	}
	if strings.Contains(string(mdData), "## LLM Interpretation") {
		t.Error("comparison markdown should NOT contain an LLM section when the call failed")
	}
	if !strings.Contains(string(mdData), "Model Time") {
		t.Error("the rule-layer report should still be complete despite the LLM failure")
	}
}
