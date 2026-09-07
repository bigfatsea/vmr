// Ver 2026-07-29 23:55, by Sonnet 5

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/journey"
)

func writeJourneyJSONL(t *testing.T, recs []audit.Record) string {
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

func journeySSE(text string) string {
	return `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}
data: [DONE]`
}

func journeyMsg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

func journeyRec(ts time.Time, msgs []any, respBody any) audit.Record {
	body := map[string]any{"model": "agent", "stream": true, "messages": msgs}
	return audit.Record{
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai-completions", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: map[string][]string{}, Body: body},
			Response: &audit.Message{Status: 200, Headers: map[string][]string{}, Body: respBody},
		},
	}
}

// TestCmdAnalyze_ListAndRender exercises the `vmr analyze -journey` CLI end to end — a
// path flagged as untested: internal/journey's own
// tests cover Build/RenderMarkdown directly, but nothing exercised
// cmd_journey.go's flag parsing, candidate listing (batched PreviewTitles),
// or the -journey render-to-file path. Two records sharing the same opening
// user message form one 2-manifest lineage — the minimum ListCandidates
// will offer as a journey.
func TestCmdAnalyze_ListAndRender(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "调研一下 A 股新股打新收益")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("开工"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("完成"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")

	listing := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-list-only", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -list-only: %v", err)
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
		if err := cmdAnalyze([]string{"-journey", id, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -journey: %v", err)
		}
	})
	if !strings.Contains(render, id+".md") {
		t.Errorf("render output missing the written file path:\n%s", render)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "journeys"))
	if err != nil {
		t.Fatalf("journeys/ not created: %v", err)
	}
	// One journey now writes two files: j-<id>.md (the narrative) and
	// j-<id>.json (the behavior profile) in journeys/details/, plus
	// index.json/.md at the journeys/ root — the earlier bare listing call
	// already wrote the index pair, this render just updates them in place.
	var sawIndex bool
	for _, e := range entries {
		if !e.IsDir() {
			sawIndex = true
		}
	}
	detailsEntries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatalf("journeys/details not created: %v", err)
	}
	if !sawIndex {
		t.Fatalf("journeys/index not written: %v", entries)
	}
	if len(detailsEntries) != 2 {
		t.Fatalf("want 2 files (journey .md+.json) in journeys/details, got %d: %v", len(detailsEntries), detailsEntries)
	}
	content, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(id)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "调研一下 A 股新股打新收益") {
		t.Errorf("rendered journey missing root instruction:\n%s", content)
	}
	jsonData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", strings.TrimSuffix(journey.JourneyReportFile(id), ".md")+".json"))
	if err != nil {
		t.Fatalf("j-%s.json not written: %v", id, err)
	}
	var summary journey.JourneySummary
	if err := json.Unmarshal(jsonData, &summary); err != nil {
		t.Fatalf("journey-%s.json is not valid JSON: %v\n%s", id, err, jsonData)
	}
	if summary.ID != id {
		t.Errorf("journey-%s.json's own id field = %q, want %q", id, summary.ID, id)
	}
	// P4: writeJourneyFile is j-<id>.json's only production writer,
	// and it builds JourneySummary via its own literal rather than calling
	// journey.Summarize (which has its own Metrics/Findings it must reuse
	// rather than recompute — see journey.NewJourneySummary's doc comment).
	// That literal silently missed the Structure field for one build during
	// P4's own execution (caught only by manually inspecting real-corpus
	// output, not by any test) — this assertion is what should have caught
	// it, and is what guards the next field the same way.
	if len(summary.Structure.Tasks) == 0 {
		t.Fatal("j-*.json's structure.tasks is empty — writeJourneyFile likely isn't populating Structure (see journey.NewJourneySummary)")
	}
	gotSteps := 0
	for _, task := range summary.Structure.Tasks {
		gotSteps += len(task.Steps)
	}
	if gotSteps != 2 {
		t.Errorf("structure.tasks has %d steps total, want 2 (matching this fixture's r1/r2)", gotSteps)
	}
}

// TestCmdAnalyze_RenderAll covers -render-all: two independent candidate
// lineages must both be rendered in one pass, with no -journey id needed
// (design-doc review follow-up: picking an id by hand for every journey was
// the friction this flag removes).
func TestCmdAnalyze_RenderAll(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	uA := journeyMsg("user", "调研一下 A 股新股打新收益")
	rA1 := journeyRec(at(0), []any{sys, uA}, journeySSE("开工"))
	rA2 := journeyRec(at(1), []any{sys, uA, journeyMsg("assistant", "done")}, journeySSE("完成"))

	uB := journeyMsg("user", "帮我写个 release note")
	rB1 := journeyRec(at(10), []any{sys, uB}, journeySSE("好的"))
	rB2 := journeyRec(at(11), []any{sys, uB, journeyMsg("assistant", "done")}, journeySSE("写好了"))

	path := writeJourneyJSONL(t, []audit.Record{rA1, rA2, rB1, rB2})
	outDir := filepath.Join(t.TempDir(), "out")

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-render-all", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -render-all: %v", err)
		}
	})
	if !strings.Contains(out, "2 journey(s) rendered to") {
		t.Errorf("summary line missing or wrong count:\n%s", out)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatalf("journeys/details not created: %v", err)
	}
	// Two journeys, each writing a .md + .json pair in journeys/details/.
	if len(entries) != 4 {
		t.Fatalf("want 4 files (2 journeys x .md+.json), got %d: %v", len(entries), entries)
	}
	var all string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		all += string(content)
	}
	if !strings.Contains(all, "调研一下 A 股新股打新收益") || !strings.Contains(all, "帮我写个 release note") {
		t.Errorf("both journeys' root instructions should appear across the two .md files:\n%s", all)
	}
}

// TestCmdAnalyze_JourneyCommaSeparatedList covers -journey id1,id2: both
// journeys must render, batched through the same renderJourneys path
// -render-all uses (proven by the "N journey(s) rendered to" summary line,
// distinct from single-journey render's own RenderedNote wording).
func TestCmdAnalyze_JourneyCommaSeparatedList(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-journey", idA + "," + idB, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -journey (comma list): %v", err)
		}
	})
	if !strings.Contains(out, "2 journey(s) rendered to") {
		t.Errorf("expected the batched-render summary line:\n%s", out)
	}
	for _, id := range []string{idA, idB} {
		if _, err := os.Stat(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(id))); err != nil {
			t.Errorf("j-%s.md not written: %v", id, err)
		}
	}
}

// TestCmdAnalyze_JourneyWildcardMatchesMultiple covers -journey '*' style
// globbing: a pattern with no exact-prefix relationship to either id (a
// wildcard match against the id's suffix, which prefix matching alone could
// never express) must still resolve and batch-render every match.
func TestCmdAnalyze_JourneyWildcardMatchesMultiple(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-journey", "j-*", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -journey (wildcard, all): %v", err)
		}
	})
	if !strings.Contains(out, "2 journey(s) rendered to") {
		t.Errorf("expected both journeys to match 'j-*':\n%s", out)
	}
	for _, id := range []string{idA, idB} {
		if _, err := os.Stat(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(id))); err != nil {
			t.Errorf("j-%s.md not written: %v", id, err)
		}
	}
}

// TestCmdAnalyze_JourneyWildcardMatchesOne covers a glob that pins down
// exactly one journey (by its content-addressed suffix, which a plain
// prefix can't select on) — it must still take the single-journey render
// path (RenderedNote's "(N tasks, N turns)" wording, not the batched
// summary), same as passing the full id directly.
func TestCmdAnalyze_JourneyWildcardMatchesOne(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)
	pattern := "*" + idA[len(idA)-8:]

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-journey", pattern, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -journey (wildcard, one): %v", err)
		}
	})
	if !strings.Contains(out, idA+".md") || !strings.Contains(out, "tasks") {
		t.Errorf("expected the single-journey RenderedNote line for %s:\n%s", idA, out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idB))); err == nil {
		t.Errorf("j-%s.md should not have been rendered (pattern only matches idA)", idB)
	}
}

// TestCmdAnalyze_JourneySelectorNoMatchErrors covers the per-token "fail loud"
// contract: one real id plus one bogus token must error, not silently
// render only the real match.
func TestCmdAnalyze_JourneySelectorNoMatchErrors(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)

	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-journey", idA + ",no-such-id", "-o", outDir, path})
	})
	if err == nil {
		t.Fatal("expected an error when one comma-separated token matches nothing")
	}
	if !strings.Contains(err.Error(), "no-such-id") {
		t.Errorf("error should name the unmatched token: %v", err)
	}
}

// TestCmdAnalyze_JourneyMultiMatchRejectsLLM covers the same "-llm-addr wants
// exactly one journey" rule -render-all/-corpus already enforce, extended to
// a -journey selector that resolves to more than one match.
func TestCmdAnalyze_JourneyMultiMatchRejectsLLM(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-journey", idA + "," + idB, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", "-o", outDir, path})
	})
	if err == nil {
		t.Fatal("expected an error: -llm-addr with a multi-match -journey selector")
	}
}

// TestCmdAnalyze_Compare covers Differential analysis: -compare id1,id2 resolves
// two candidate journeys by id prefix and writes one comparison
// Markdown+JSON pair. Journey B's much larger model time should surface as
// a notable row.
func TestCmdAnalyze_Compare(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	uA := journeyMsg("user", "调研一下 A 股新股打新收益")
	rA1 := journeyRec(at(0), []any{sys, uA}, journeySSE("开工"))
	rA2 := journeyRec(at(1), []any{sys, uA, journeyMsg("assistant", "done")}, journeySSE("完成"))

	uB := journeyMsg("user", "帮我写个 release note")
	rB1 := journeyRec(at(10), []any{sys, uB}, journeySSE("好的"))
	rB2 := journeyRec(at(11), []any{sys, uB, journeyMsg("assistant", "done")}, journeySSE("写好了"))

	path := writeJourneyJSONL(t, []audit.Record{rA1, rA2, rB1, rB2})
	outDir := filepath.Join(t.TempDir(), "out")

	listing := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-list-only", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -list-only: %v", err)
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
		if err := cmdAnalyze([]string{"-compare", idA + "," + idB, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -compare: %v", err)
		}
	})
	wantBase := "compare-" + idA + "-vs-" + idB
	if !strings.Contains(out, wantBase+".md") {
		t.Errorf("output missing the written comparison path:\n%s", out)
	}

	mdData, err := os.ReadFile(filepath.Join(outDir, "compares", wantBase+".md"))
	if err != nil {
		t.Fatalf("comparison .md not written: %v", err)
	}
	md := string(mdData)
	linkA := "[" + idA + "](../journeys/details/" + journey.JourneyReportFile(idA) + ")"
	linkB := "[" + idB + "](../journeys/details/" + journey.JourneyReportFile(idB) + ")"
	for _, want := range []string{linkA, linkB, "调研一下 A 股新股打新收益", "帮我写个 release note", "Model Time", "Evidence Provenance", ctxgraph.CanonicalPath(path)} {
		if !strings.Contains(md, want) {
			t.Errorf("comparison markdown missing %q:\n%s", want, md)
		}
	}

	// -compare automatically generated the individual journey files
	for _, id := range []string{idA, idB} {
		if _, err := os.Stat(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(id))); err != nil {
			t.Errorf("j-%s.md should have been auto-generated by -compare: %v", id, err)
		}
		if _, err := os.Stat(filepath.Join(outDir, "journeys", "details", strings.TrimSuffix(journey.JourneyReportFile(id), ".md")+".json")); err != nil {
			t.Errorf("j-%s.json should have been auto-generated by -compare: %v", id, err)
		}
	}

	jsonData, err := os.ReadFile(filepath.Join(outDir, "compares", wantBase+".json"))
	if err != nil {
		t.Fatalf("comparison .json not written: %v", err)
	}
	var cmp journey.Comparison
	if err := json.Unmarshal(jsonData, &cmp); err != nil {
		t.Fatalf("comparison .json is not valid JSON: %v\n%s", err, jsonData)
	}
	if cmp.A.ID != idA || cmp.B.ID != idB {
		t.Errorf("comparison json ids = %q/%q, want %q/%q", cmp.A.ID, cmp.B.ID, idA, idB)
	}
	wantReportA := filepath.ToSlash(filepath.Join("..", "journeys", "details", journey.JourneyReportFile(idA)))
	wantReportB := filepath.ToSlash(filepath.Join("..", "journeys", "details", journey.JourneyReportFile(idB)))
	if cmp.A.ReportFile != wantReportA || cmp.B.ReportFile != wantReportB {
		t.Errorf("comparison json report files = %q/%q, want %q/%q", cmp.A.ReportFile, cmp.B.ReportFile, wantReportA, wantReportB)
	}
	if len(cmp.Rows) == 0 {
		t.Error("comparison json has no metric rows")
	}
	// Evidence-provenance addition: Extras.Sources must carry the resolved
	// input path(s) this comparison was built from, not just be left empty —
	// otherwise the "证据溯源" text above would be asserting against a
	// section that silently renders nothing.
	wantSource := ctxgraph.CanonicalPath(path)
	if cmp.Extras == nil || len(cmp.Extras.Sources) != 1 || cmp.Extras.Sources[0] != wantSource {
		t.Errorf("comparison json Extras.Sources = %+v, want [%q]", extrasSources(cmp), wantSource)
	}
}

func extrasSources(cmp journey.Comparison) []string {
	if cmp.Extras == nil {
		return nil
	}
	return cmp.Extras.Sources
}

// TestCmdAnalyze_CompareRequiresTwoIDs covers the usage error when -compare
// isn't given exactly two comma-separated ids (missing second id, or a
// trailing/leading empty one from a stray comma).
func TestCmdAnalyze_CompareRequiresTwoIDs(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "hello")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("a"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("b"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	for _, val := range []string{"j-something", "j-something,", ",j-something"} {
		err := captureStdoutErr(t, func() error {
			return cmdAnalyze([]string{"-compare", val, "-o", outDir, path})
		})
		if err == nil {
			t.Errorf("-compare %q should be a usage error", val)
		}
	}
}

// TestCmdAnalyze_CompareUnknownID covers -compare id1,id2 reporting which side
// failed to resolve when an id prefix doesn't match any candidate — the
// error must name whether it's the first or second id, not just "no journey
// found".
func TestCmdAnalyze_CompareUnknownID(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "hello")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("a"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("b"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-compare", "no-such-id,j-", "-o", outDir, path})
	})
	if err == nil || !strings.Contains(err.Error(), "-compare first id") {
		t.Errorf("expected a -compare first id error, got: %v", err)
	}
}

// TestCmdAnalyze_ComparePartialGating covers compareJourneys' own
// -include-partial gate: a partial-head candidate on either side must be
// rejected the same way a single -journey render is, and accepted once
// -include-partial is passed — with partiality carried as data (D19): no
// "-partial" filename suffix, a partial:true field in the JSON, a banner in
// the .md, and a partial mark in compares/index.md.
func TestCmdAnalyze_ComparePartialGating(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	// Partial candidate: looks mid-conversation already (>2 non-system keys)
	// within the first lines of the only loaded file — same fixture shape as
	// TestCmdAnalyze_PartialHeadFilenameSuffix.
	u1 := journeyMsg("user", "第一轮指令")
	a1 := journeyMsg("assistant", "第一轮回复")
	u2 := journeyMsg("user", "第二轮追问")
	rPartial1 := journeyRec(at(0), []any{sys, u1, a1, u2}, journeySSE("continuing"))
	rPartial2 := journeyRec(at(1), []any{sys, u1, a1, u2, journeyMsg("assistant", "第二轮回复")}, journeySSE("done"))

	uB := journeyMsg("user", "一个普通的新任务")
	rB1 := journeyRec(at(10), []any{sys, uB}, journeySSE("好的"))
	rB2 := journeyRec(at(11), []any{sys, uB, journeyMsg("assistant", "done")}, journeySSE("写好了"))

	path := writeJourneyJSONL(t, []audit.Record{rPartial1, rPartial2, rB1, rB2})
	outDir := filepath.Join(t.TempDir(), "out")

	listing := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-list-only", "-include-partial", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze (list -include-partial): %v", err)
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

	if err := cmdAnalyze([]string{"-compare", idPartial + "," + idB, "-o", outDir, path}); err == nil {
		t.Error("comparing a partial-head journey without -include-partial should error")
	}

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-include-partial", "-compare", idPartial + "," + idB, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -compare with -include-partial: %v", err)
		}
	})
	if !strings.Contains(out, "compare-") || !strings.Contains(out, ".md") {
		t.Errorf("comparison output should mention the written compare files:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "-partial") {
			t.Errorf("compare filename must not carry the retired -partial suffix (D19): %s", line)
		}
	}

	// Partiality rides as data: JSON field, .md banner, index mark.
	comparesDir := filepath.Join(outDir, "compares")
	entries, err := os.ReadDir(comparesDir)
	if err != nil {
		t.Fatalf("compares dir: %v", err)
	}
	var cmpJSONName string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "compare-") && strings.HasSuffix(e.Name(), ".json") {
			cmpJSONName = e.Name()
		}
	}
	if cmpJSONName == "" {
		t.Fatalf("no compare-*.json written: %v", entries)
	}
	cmpData, err := os.ReadFile(filepath.Join(comparesDir, cmpJSONName))
	if err != nil {
		t.Fatalf("read compare json: %v", err)
	}
	var cmp struct {
		Partial bool `json:"partial"`
	}
	if err := json.Unmarshal(cmpData, &cmp); err != nil {
		t.Fatalf("unmarshal compare json: %v", err)
	}
	if !cmp.Partial {
		t.Error("compare json should carry partial:true when a side is head-truncated")
	}
	cmpMD, err := os.ReadFile(filepath.Join(comparesDir, strings.TrimSuffix(cmpJSONName, ".json")+".md"))
	if err != nil {
		t.Fatalf("read compare md: %v", err)
	}
	if !strings.Contains(string(cmpMD), "truncated") {
		t.Errorf("compare md should carry the partial banner:\n%.200s", cmpMD)
	}
	indexMD, err := os.ReadFile(filepath.Join(comparesDir, "index.md"))
	if err != nil {
		t.Fatalf("read compares/index.md: %v", err)
	}
	if !strings.Contains(string(indexMD), "partial") {
		t.Errorf("compares/index.md should mark the partial side:\n%s", indexMD)
	}
}

// TestCmdAnalyze_ShowUngrouped covers -show-ungrouped: a record with no
// non-system messages and no metadata.user_id gets no SessKey at all
// (ctxgraph.Graph.Ungrouped), and -show-ungrouped must print its source
// location.
func TestCmdAnalyze_ShowUngrouped(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sysOnly := journeyRec(at(0), []any{journeyMsg("system", "sys, nothing else")}, journeySSE("ok"))
	path := writeJourneyJSONL(t, []audit.Record{sysOnly})
	outDir := filepath.Join(t.TempDir(), "out")

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-show-ungrouped", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -show-ungrouped: %v", err)
		}
	})
	if !strings.Contains(out, "1 ungrouped record") {
		t.Errorf("expected 1 ungrouped record reported:\n%s", out)
	}
	if !strings.Contains(out, "ungrouped record") || !strings.Contains(out, filepath.Base(path)) {
		t.Errorf("-show-ungrouped should print the record's source location:\n%s", out)
	}
}

// TestCmdAnalyze_NoInputFiles mirrors TestCmdReport_NoInputFiles: `vmr analyze`
// with no positional args is a usage error, not a silent no-op.
func TestCmdAnalyze_NoInputFiles(t *testing.T) {
	if err := cmdAnalyze([]string{}); err == nil {
		t.Error("cmdAnalyze with no input files should return an error")
	}
}

// TestCmdAnalyze_UnknownJourney covers the -journey-with-no-match error path.
func TestCmdAnalyze_UnknownJourney(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "hello")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("a"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("b"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-journey", "no-such-id", "-o", outDir, path})
	})
	if err == nil {
		t.Error("cmdAnalyze -journey with an unmatched id prefix should return an error")
	}
}

// TestCmdAnalyze_PartialHeadFilenameSuffix covers the fix: a
// head-truncated Journey's rendered filename must self-disclose that its ID
// isn't stable, via a "-partial" suffix, without requiring the reader to
// open the file and find the warning line first. The first record already
// carries a multi-turn-looking manifest (sys + 2 user/assistant pairs) at
// line 0 of the only loaded file — journey.IsPartialHead's signal for "this
// conversation's real opening lives outside the loaded range".
func TestCmdAnalyze_PartialHeadFilenameSuffix(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "第一轮指令")
	a1 := journeyMsg("assistant", "第一轮回复")
	u2 := journeyMsg("user", "第二轮追问")
	r1 := journeyRec(at(0), []any{sys, u1, a1, u2}, journeySSE("continuing"))
	r2 := journeyRec(at(1), []any{sys, u1, a1, u2, journeyMsg("assistant", "第二轮回复")}, journeySSE("done"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")

	// Without -include-partial, the candidate is skipped and -render-all
	// writes nothing.
	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-render-all", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -render-all (no -include-partial): %v", err)
		}
	})
	if !strings.Contains(out, "skipped as partial-head") {
		t.Errorf("expected the candidate to be skipped as partial-head:\n%s", out)
	}
	// journeys/index.json/.md are still written (every invocation gets one),
	// but no j-*.md/.json — the partial-head candidate was skipped.
	entries, _ := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "j-") {
			t.Errorf("no journey detail file should be written without -include-partial, got %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "journeys", "index.json")); err != nil {
		t.Errorf("journeys/index.json should still be written: %v", err)
	}

	// With -include-partial, it renders. Per D19 the -partial filename
	// suffix is retired — the partial state lives in the JSON field and the
	// .md banner instead.
	out = captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-render-all", "-include-partial", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -render-all -include-partial: %v", err)
		}
	})
	if !strings.Contains(out, ".md") {
		t.Errorf("render output should mention the written file:\n%s", out)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatalf("journeys/details not created: %v", err)
	}
	var journeyFiles []os.DirEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "j-") {
			journeyFiles = append(journeyFiles, e)
		}
	}
	if len(journeyFiles) != 2 {
		t.Fatalf("want 2 journey files (.md + .json), got %d: %v", len(journeyFiles), entries)
	}
	for _, e := range journeyFiles {
		if strings.Contains(e.Name(), "-partial") {
			t.Errorf("file %s must not carry the retired -partial suffix (D19)", e.Name())
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
// — the -llm-* degradation tests assert on the warning text cmdAnalyze prints
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
// shape TestCmdAnalyze_Compare uses) and returns its path plus both journeys'
// ids, resolved by listing once — shared setup for the -llm-* CLI tests
// below, which only care about the compare/LLM plumbing, not journey
// construction itself.
func writeTwoCandidateJourneys(t *testing.T, outDir string) (path, idA, idB string) {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	uA := journeyMsg("user", "调研一下 A 股新股打新收益")
	rA1 := journeyRec(at(0), []any{sys, uA}, journeySSE("开工"))
	rA2 := journeyRec(at(1), []any{sys, uA, journeyMsg("assistant", "done")}, journeySSE("完成"))
	uB := journeyMsg("user", "帮我写个 release note")
	rB1 := journeyRec(at(10), []any{sys, uB}, journeySSE("好的"))
	rB2 := journeyRec(at(11), []any{sys, uB, journeyMsg("assistant", "done")}, journeySSE("写好了"))
	path = writeJourneyJSONL(t, []audit.Record{rA1, rA2, rB1, rB2})

	// The id-discovery listing deliberately runs against its own scratch
	// -o, not the caller's outDir: since journeys/index.{json,md} are now
	// written on every invocation (including a bare listing), reusing
	// outDir here would leave journeys/ already populated before
	// the caller's own cmdAnalyze call — which some callers (the -llm-dry-run
	// tests) specifically assert creates nothing.
	listing := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-list-only", "-o", filepath.Join(t.TempDir(), "discover"), path}); err != nil {
			t.Fatalf("cmdAnalyze (list): %v", err)
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

// TestCmdAnalyze_LLMFlagValidation covers resolveLLMOptions' guard rails: the
// -llm-* flag combinations that must be rejected before anything is scanned.
func TestCmdAnalyze_LLMFlagValidation(t *testing.T) {
	path, idA, idB := writeTwoCandidateJourneys(t, filepath.Join(t.TempDir(), "out"))
	compareArgs := []string{"-compare", idA + "," + idB, path}

	cases := map[string][]string{
		"-llm-dry-run without -llm-addr":           append([]string{"-llm-dry-run"}, compareArgs...),
		"-llm-model without -llm-addr":             append([]string{"-llm-model", "agent"}, compareArgs...),
		"-llm-key without -llm-addr":               append([]string{"-llm-key", "sk-x"}, compareArgs...),
		"-llm-addr without -llm-model or -dry-run": append([]string{"-llm-addr", "127.0.0.1:1"}, compareArgs...),
	}
	for name, args := range cases {
		if err := captureStdoutErr(t, func() error { return cmdAnalyze(args) }); err == nil {
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
		return cmdAnalyze([]string{"-journey", idA, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", "-o", filepath.Join(t.TempDir(), "out2"), path})
	}); err != nil {
		t.Errorf("-llm-addr with -journey should degrade away on connection failure, not fail the command: %v", err)
	}

	// -llm-addr with -render-all must still be rejected — one LLM call per
	// rendered journey in a batch pass is a different cost profile than a
	// single -journey/-compare call, deliberately not supported.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-all", "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", "-o", filepath.Join(t.TempDir(), "out3"), path})
	}); err == nil {
		t.Error("-llm-addr with -render-all should be rejected")
	}

	// -llm-addr with -benchmark must be rejected the same way as -render-all —
	// same "one LLM call per journey in a batch pass" cost-profile reasoning.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-benchmark", "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", "-o", filepath.Join(t.TempDir(), "out4"), path})
	}); err == nil {
		t.Error("-llm-addr with -benchmark should be rejected")
	}
}

// TestCmdAnalyze_CompareLLMDryRun covers -llm-dry-run: it must print a size
// estimate and return before writing anything, and must never dial the
// given address (127.0.0.1:1 refuses every connection on virtually every
// system — if the dry run actually tried to connect, this test would fail
// with a connection-refused error surfacing as a non-nil return).
func TestCmdAnalyze_CompareLLMDryRun(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-compare", idA + "," + idB, "-llm-addr", "127.0.0.1:1", "-llm-dry-run", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -llm-dry-run: %v", err)
		}
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("dry-run output missing the size estimate line: %q", out)
	}
	base := "compare-" + idA + "-vs-" + idB
	if _, err := os.Stat(filepath.Join(outDir, "compares", base+".md")); err == nil {
		t.Error("-llm-dry-run should return before writing the compare .md")
	}
	// -llm-dry-run writes no comparison artifacts. The compares/ index
	// itself is scan-derived on every analyze invocation (D21), so its
	// presence is expected; what must stay absent is any compare-*.json.
	entries, _ := os.ReadDir(filepath.Join(outDir, "compares"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "compare-") {
			t.Errorf("-llm-dry-run should not write %s", e.Name())
		}
	}
}

// TestCmdAnalyze_CompareWithLLM covers the full path: a real (mock) VMR
// endpoint, the rendered .md gaining the "## LLM Interpretation" section with the
// mock's reply, and a cache file appearing under .llm-cache.
func TestCmdAnalyze_CompareWithLLM(t *testing.T) {
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
	cacheDir := filepath.Join(outDir, ".llm-cache")

	if err := cmdAnalyze([]string{"-compare", idA + "," + idB, "-llm-addr", addr, "-llm-model", "agent", "-llm-cache-dir", cacheDir, "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -llm-addr: %v", err)
	}
	base := "compare-" + idA + "-vs-" + idB
	mdData, err := os.ReadFile(filepath.Join(outDir, "compares", base+".md"))
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

// TestCmdAnalyze_NoLLMCacheDirConfiguredMeansNoCaching covers the explicit
// behavior change: -llm-cache-dir has no implicit default (unlike the old
// hardcoded .llm-cache) — with neither the flag nor
// report.yaml's llm_cache_dir set, an -llm-addr call must still succeed
// (the LLM section renders) but must leave no .llm-cache directory behind
// anywhere under outDir.
func TestCmdAnalyze_NoLLMCacheDirConfiguredMeansNoCaching(t *testing.T) {
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

	if err := cmdAnalyze([]string{"-journey", idA, "-llm-addr", addr, "-llm-model", "agent", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -journey -llm-addr (no -llm-cache-dir): %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA)))
	if err != nil {
		t.Fatalf("journey .md not written: %v", err)
	}
	if !strings.Contains(string(mdData), "一句话结论：无缓存路径。") {
		t.Error("LLM section should still render even with no cache configured")
	}
	if _, err := os.Stat(filepath.Join(outDir, ".llm-cache")); err == nil {
		t.Error("no .llm-cache directory should exist when -llm-cache-dir is unset both on the CLI and in report.yaml")
	}
}

// TestCmdAnalyze_ReportYamlProvidesLLMDefaults covers report.yaml's
// llm_addr/llm_model/llm_cache_dir feeding -journey's LLM interpretation
// layer when the corresponding -llm-* flags aren't passed at all — the same
// merge order TestCmdReport_ReportYamlDefaultsOutputAndDetails covers for
// -o/-details.
func TestCmdAnalyze_ReportYamlProvidesLLMDefaults(t *testing.T) {
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

	if err := cmdAnalyze([]string{"-journey", idA, "-report-config", reportConfigPath, "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -journey (llm settings from report.yaml): %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA)))
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

// TestCmdAnalyze_ReportYamlLLMAddrDoesNotBlockBatchPaths is a regression test
// for a real bug: report.yaml's llm_addr is meant as a standing convenience
// default for -journey/-compare (see TestCmdAnalyze_ReportYamlProvidesLLMDefaults),
// but the -render-all/-corpus/multi-match-journey rejection used to trigger
// on llmOpts.Addr being non-empty at all — which made it fire off of
// report.yaml's default even though -llm-addr was never passed on the
// command line, so anyone with an llm_addr configured for convenience could
// no longer run a plain batch render. The guard must key off whether
// -llm-addr was explicitly passed (flagPassed), not merely resolved.
func TestCmdAnalyze_ReportYamlLLMAddrDoesNotBlockBatchPaths(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	reportConfigPath := filepath.Join(t.TempDir(), "report.yaml")
	// Deliberately an address nothing is listening on: none of these
	// batch paths may ever actually dial it.
	yaml := "llm_addr: 127.0.0.1:1\nllm_model: agent\n"
	if err := os.WriteFile(reportConfigPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("multi-match -journey", func(t *testing.T) {
		if err := cmdAnalyze([]string{"-journey", idA + "," + idB, "-report-config", reportConfigPath, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -journey (comma list, report.yaml llm_addr default): %v", err)
		}
	})
	t.Run("-render-all", func(t *testing.T) {
		outDir2 := filepath.Join(t.TempDir(), "out2")
		if err := cmdAnalyze([]string{"-render-all", "-report-config", reportConfigPath, "-o", outDir2, path}); err != nil {
			t.Fatalf("cmdAnalyze -render-all (report.yaml llm_addr default): %v", err)
		}
	})
	t.Run("-benchmark", func(t *testing.T) {
		outDir3 := filepath.Join(t.TempDir(), "out3")
		if err := cmdAnalyze([]string{"-benchmark", "-report-config", reportConfigPath, "-o", outDir3, path}); err != nil {
			t.Fatalf("cmdAnalyze -benchmark (report.yaml llm_addr default): %v", err)
		}
	})

	// An explicit -llm-addr on the command line must still be rejected —
	// only report.yaml's own default is exempt.
	t.Run("explicit -llm-addr with -render-all still rejected", func(t *testing.T) {
		outDir4 := filepath.Join(t.TempDir(), "out4")
		err := captureStdoutErr(t, func() error {
			return cmdAnalyze([]string{"-render-all", "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", "-o", outDir4, path})
		})
		if err == nil {
			t.Error("expected an error: explicit -llm-addr with -render-all")
		}
	})
}

// TestCmdAnalyze_Benchmark covers -benchmark: two independent candidate journeys
// must produce benchmarks.md + .json under {outDir}/journeys, and the
// "no candidates" path (an audit log that groups into zero lineages at all)
// must return without error and without writing either file, matching
// renderBenchmarks' own len(toRender)==0 early return.
func TestCmdAnalyze_Benchmark(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, _, _ := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-benchmark", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -benchmark: %v", err)
		}
	})
	if !strings.Contains(out, "2 journey(s) analyzed") {
		t.Errorf("summary line missing or wrong journey count:\n%s", out)
	}

	mdPath := filepath.Join(outDir, "journeys", "benchmarks.md")
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("benchmarks.md not written: %v", err)
	}
	if len(mdData) == 0 {
		t.Error("benchmarks.md is empty")
	}

	jsonPath := filepath.Join(outDir, "journeys", "benchmarks.json")
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("benchmarks.json not written: %v", err)
	}
	var stats journey.BenchmarkStats
	if err := json.Unmarshal(jsonData, &stats); err != nil {
		t.Fatalf("benchmarks.json is not valid JSON: %v\n%s", err, jsonData)
	}
	if stats.JourneyCount != 2 {
		t.Errorf("stats.JourneyCount = %d, want 2", stats.JourneyCount)
	}
}

// TestCmdAnalyze_BenchmarkNoCandidates covers renderBenchmarks' own early return when
// there are zero candidate journeys to analyze (here: a single record with
// no non-system messages, which ctxgraph groups into Ungrouped rather than
// any Lineage at all — same fixture shape as TestCmdAnalyze_ShowUngrouped).
// The command must not error, and must not write benchmarks.md/.json
// (nothing to analyze) — but journeys/index.json/.md still get written, same
// as every other invocation (an empty candidate list is still a real,
// worth-recording result, unlike -llm-dry-run's "should I even run this"
// pure query, which is why that one still leaves no directory at all).
func TestCmdAnalyze_BenchmarkNoCandidates(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sysOnly := journeyRec(at(0), []any{journeyMsg("system", "sys, nothing else")}, journeySSE("ok"))
	path := writeJourneyJSONL(t, []audit.Record{sysOnly})
	outDir := filepath.Join(t.TempDir(), "out")

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-benchmark", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -benchmark (no candidates): %v", err)
		}
	})
	if !strings.Contains(out, "no candidate journeys to analyze") {
		t.Errorf("expected the no-candidates message:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "journeys", "benchmarks.md")); err == nil {
		t.Error("-benchmark with zero candidates should not write benchmarks.md")
	}
	if _, err := os.Stat(filepath.Join(outDir, "journeys", "index.json")); err != nil {
		t.Errorf("journeys/index.json should still be written even with zero candidates: %v", err)
	}
}

// TestCmdAnalyze_BenchmarkExclusivity covers the exclusivity check that rejects
// -benchmark combined with -journey/-render-all/-compare — each must be
// rejected before any input file is even scanned, and must leave no
// journeys/ directory behind.
func TestCmdAnalyze_BenchmarkExclusivity(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "hello")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("a"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("b"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	cases := map[string][]string{
		"-benchmark with -journey":    {"-benchmark", "-journey", "j-something"},
		"-benchmark with -render-all": {"-benchmark", "-render-all"},
		"-benchmark with -compare":    {"-benchmark", "-compare", "a,b"},
	}
	for name, extra := range cases {
		outDir := filepath.Join(t.TempDir(), "out")
		args := append(append([]string{"-o", outDir}, extra...), path)
		err := captureStdoutErr(t, func() error { return cmdAnalyze(args) })
		if err == nil {
			t.Errorf("%s: expected an error, got none", name)
			continue
		}
		if !strings.Contains(err.Error(), "exclusive") && !strings.Contains(err.Error(), "drop one or the other") {
			t.Errorf("%s: error = %q, want an exclusivity error", name, err.Error())
		}
		if _, statErr := os.Stat(filepath.Join(outDir, "journeys")); statErr == nil {
			t.Errorf("%s: journeys/ should not be created when the exclusivity check rejects the args", name)
		}
	}
}

// TestCmdAnalyze_JourneyWithLLM mirrors TestCmdAnalyze_CompareWithLLM but for
// -journey: a real (mock) VMR endpoint, the rendered journey .md gaining the
// "## LLM Interpretation" section with the mock's reply, the rendered
// journey .json gaining populated llm_findings, and a cache file
// appearing under .llm-cache.
func TestCmdAnalyze_JourneyWithLLM(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		w.Header().Set("Content-Type", "application/json")

		// Each case matches the actual JSON *key* the corresponding
		// EvidencePack marshals (see llm_findings_types.go), escaped as it
		// appears on the wire: the pack's JSON is marshaled once, then
		// embedded as a string value inside the outer chat-completion
		// request body, so json.Marshal escapes its quotes a second time
		// (`"candidates"` becomes `\"candidates\"`) — a bare, unescaped
		// match never fires. Matching only the escaped key also sidesteps
		// prose collisions: several of these words (root_user_intent,
		// plan_items, excerpts, candidates) additionally appear bare, as
		// English words, inside the system prompts themselves. A branch
		// that doesn't match falls through to the free-text default, which
		// the detector fails to parse as JSON and treats as "no finding" —
		// silently, by design (fail-open).
		var replyContent string
		switch {
		case strings.Contains(bodyStr, `\"verification_commands_observed\"`): // CompletionClaimEvidencePack
			replyContent = `{"claim_status": "CLAIM_WITHOUT_VERIFICATION", "confidence": "HIGH", "evidence_anchor": "done", "missing_verification": "no build or test executed"}`
		case strings.Contains(bodyStr, `\"root_user_intent\"`): // GoalDriftEvidencePack; goalDriftResult is a single object, not an array
			replyContent = `{"drift_detected": false, "drift_step_seq": 0, "confidence": "LOW", "evidence_anchor": "", "drift_explanation": ""}`
		case strings.Contains(bodyStr, `\"plan_items\"`): // PlanAuditEvidencePack; planAuditResult is a single object, not an array
			replyContent = `{"has_misalignment": false, "confidence": "LOW", "evidence_anchor": "", "explanation": ""}`
		case strings.Contains(bodyStr, `\"candidates\"`): // SemanticOscillationEvidencePack
			replyContent = `[]`
		case strings.Contains(bodyStr, `\"excerpts\"`): // CompactionConstraintEvidencePack
			replyContent = `[]`
		default:
			replyContent = "一句话结论：这是 mock 的单journey解读内容。"
		}

		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": replyContent}},
			},
		})
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)
	cacheDir := filepath.Join(outDir, ".llm-cache")

	if err := cmdAnalyze([]string{"-journey", idA, "-llm-addr", addr, "-llm-model", "agent", "-llm-cache-dir", cacheDir, "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -journey -llm-addr: %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA)))
	if err != nil {
		t.Fatalf("journey .md not written: %v", err)
	}
	md := string(mdData)
	for _, want := range []string{"## LLM Interpretation", "一句话结论：这是 mock 的单journey解读内容。", "not the fact layer"} {
		if !strings.Contains(md, want) {
			t.Errorf("journey markdown missing %q:\n%s", want, md)
		}
	}

	jsonData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", strings.TrimSuffix(journey.JourneyReportFile(idA), ".md")+".json"))
	if err != nil {
		t.Fatalf("journey .json not written: %v", err)
	}
	var summary journey.JourneySummary
	if err := json.Unmarshal(jsonData, &summary); err != nil {
		t.Fatalf("unmarshal journey .json: %v", err)
	}
	if len(summary.LLMFindings) == 0 {
		t.Fatalf("expected non-empty llm_findings in journey .json, got: %s", string(jsonData))
	}
	f := summary.LLMFindings[0]
	if f.Code != journey.FindingUnverifiedCompletionClaim || f.Confidence != journey.ConfidenceHigh || f.Source != journey.SourceLLMInferred {
		t.Errorf("unexpected LLM finding: %+v", f)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected at least one cache file under %s: %v", cacheDir, err)
	}
}

// TestCmdAnalyze_JourneyWithRealLLM tests against a live LLM endpoint configured
// in report.yaml when available and reachable, gracefully skipping otherwise.
func TestCmdAnalyze_JourneyWithRealLLM(t *testing.T) {
	reportYamlPath := filepath.Join("..", "..", "report.yaml")
	configData, err := os.ReadFile(reportYamlPath)
	if err != nil {
		t.Skip("no report.yaml found in repo root")
	}
	var cfg reportConfig
	if err := yaml.Unmarshal(configData, &cfg); err != nil || cfg.LLMAddr == "" {
		t.Skip("report.yaml has no valid llm_addr configured")
	}

	probeURL := cfg.LLMAddr
	if !strings.Contains(probeURL, "://") {
		probeURL = "http://" + probeURL
	}
	probeURL = strings.TrimRight(probeURL, "/")
	probeURL = strings.TrimSuffix(probeURL, "/v1") + "/v1/models"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, probeURL, nil)
	if cfg.LLMKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.LLMKey)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		t.Skipf("configured LLM endpoint %s not reachable: %v", cfg.LLMAddr, err)
	}
	resp.Body.Close()

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)

	if err := cmdAnalyze([]string{"-journey", idA, "-report-config", reportYamlPath, "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze with real report.yaml: %v", err)
	}
	mdData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA)))
	if err != nil {
		t.Fatalf("journey .md not written: %v", err)
	}
	if !strings.Contains(string(mdData), "## LLM") && !strings.Contains(string(mdData), "## AI") && !strings.Contains(string(mdData), "解读") {
		t.Errorf("expected LLM interpretation section in markdown:\n%s", string(mdData))
	}
}

// TestCmdAnalyze_JourneyLLMDryRun mirrors TestCmdAnalyze_CompareLLMDryRun but
// for -journey: -llm-dry-run must print a size estimate and return before
// writing anything (including journeys/ itself), and must never dial
// the given address.
func TestCmdAnalyze_JourneyLLMDryRun(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, _ := writeTwoCandidateJourneys(t, outDir)

	out := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-journey", idA, "-llm-addr", "127.0.0.1:1", "-llm-dry-run", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -journey -llm-dry-run: %v", err)
		}
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("dry-run output missing the size estimate line: %q", out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA))); err == nil {
		t.Error("-llm-dry-run should return before writing the journey .md")
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories")); err == nil {
		t.Error("-llm-dry-run should not create journeys/ at all")
	}
}

// TestCmdAnalyze_CompareLLMFailureDegrades covers design doc C.7's "the whole
// layer degrades away" rule: an unreachable -llm-addr must not fail the
// -compare command — the .md/.json still get written, just without the LLM
// section, and a warning goes to stderr.
func TestCmdAnalyze_CompareLLMFailureDegrades(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	var cmdErr error
	stderr := captureStderr(t, func() {
		cmdErr = cmdAnalyze([]string{"-compare", idA + "," + idB, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", "-o", outDir, path})
	})
	if cmdErr != nil {
		t.Fatalf("cmdAnalyze should not fail when the LLM endpoint is unreachable: %v", cmdErr)
	}
	if !strings.Contains(stderr, "warning") {
		t.Errorf("expected a warning on stderr about the failed LLM call, got: %q", stderr)
	}

	base := "compare-" + idA + "-vs-" + idB
	mdData, err := os.ReadFile(filepath.Join(outDir, "compares", base+".md"))
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

// TestCmdAnalyze_BatchRendersIncludeCost pins Problem 3: default-suite,
// -render-all and multi-target -journey batch rendering previously passed a
// nil cost to writeJourneyFile (on the historical misconception that pricing
// was a zoom-in only feature), leaving every batch-rendered journey-*.md/.json
// without its estimated cost and creating a data gap between the macro
// report and the journey cards. Batch rendering now threads the resolver,
// and this test asserts both .md and .json carry resolved cost.
func TestCmdAnalyze_BatchRendersIncludeCost(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 9, 1, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u := journeyMsg("user", "批量套件必须包含成本")

	// Two turns against a standard-priced endpoint (claude-3-7-sonnet-20250219).
	r1 := journeyRec(at(0), []any{sys, u}, costParitySSE("第一步回答", true))
	r1.Attempts = []audit.Attempt{{Endpoint: "anthropic-messages:anthropic:claude-3-7-sonnet-20250219", Protocol: "anthropic-messages", Provider: "anthropic", Model: "claude-3-7-sonnet-20250219", Response: &audit.Message{Status: 200}}}
	r2 := journeyRec(at(1), []any{sys, u, journeyMsg("assistant", "第一步回答")}, costParitySSE("第二步回答", true))
	r2.Attempts = []audit.Attempt{{Endpoint: "anthropic-messages:anthropic:claude-3-7-sonnet-20250219", Protocol: "anthropic-messages", Provider: "anthropic", Model: "claude-3-7-sonnet-20250219", Response: &audit.Message{Status: 200}}}

	path := writeJourneyJSONL(t, []audit.Record{r1, r2})
	outDir := filepath.Join(t.TempDir(), "out")

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-journey-only", "-render-all", path})
	}); err != nil {
		t.Fatalf("cmdAnalyze batch render: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatalf("read journeys/details dir: %v", err)
	}
	var mdPath, jsonPath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "j-") && strings.HasSuffix(e.Name(), ".md") {
			mdPath = filepath.Join(outDir, "journeys", "details", e.Name())
		}
		if strings.HasPrefix(e.Name(), "j-") && strings.HasSuffix(e.Name(), ".json") {
			jsonPath = filepath.Join(outDir, "journeys", "details", e.Name())
		}
	}
	if mdPath == "" || jsonPath == "" {
		t.Fatalf("expected journey .md and .json in journeys/details, got: %v", entries)
	}

	mdContent, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdContent), "Estimated cost ≈") {
		t.Errorf("batch-rendered markdown missing overview cost line:\n%s", mdContent)
	}

	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var summary journey.JourneySummary
	if err := json.Unmarshal(jsonData, &summary); err != nil {
		t.Fatalf("unmarshal journey json: %v", err)
	}
	if summary.Cost == nil {
		t.Fatal("batch-rendered journey json has nil cost")
	}
	if !summary.Cost.Resolved || summary.Cost.Total == nil || *summary.Cost.Total <= 0 {
		t.Errorf("summary.Cost not properly resolved: %+v", summary.Cost)
	}
	if summary.Cost.PricedSteps != 2 {
		t.Errorf("summary.Cost.PricedSteps = %d, want 2", summary.Cost.PricedSteps)
	}
}

// TestCmdAnalyze_ComparePreservesExistingJourneyLLMInterpretation verifies that
// when ensureJourneyFile runs during -compare, any pre-existing LLM interpretation
// in a journey's .json/.md is preserved rather than overwritten with nil.
func TestCmdAnalyze_ComparePreservesExistingJourneyLLMInterpretation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "这是 journey 的持久化解读内容。"}},
			},
		})
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)

	// Step 1: Run -journey with LLM on idA.
	if err := cmdAnalyze([]string{"-journey", idA, "-llm-addr", addr, "-llm-model", "agent", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -journey with LLM: %v", err)
	}

	jsonPathA := filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA)[:len(journey.JourneyReportFile(idA))-3]+".json")
	mdPathA := filepath.Join(outDir, "journeys", "details", journey.JourneyReportFile(idA))

	dataA, err := os.ReadFile(jsonPathA)
	if err != nil {
		t.Fatal(err)
	}
	var sumA journey.JourneySummary
	if err := json.Unmarshal(dataA, &sumA); err != nil {
		t.Fatal(err)
	}
	if sumA.LLMInterpretation == nil || sumA.LLMInterpretation.Text != "这是 journey 的持久化解读内容。" {
		t.Fatalf("idA missing LLM interpretation before compare: %+v", sumA.LLMInterpretation)
	}

	// Step 2: Run -compare idA,idB (which triggers ensureJourneyFile on both sides).
	if err := cmdAnalyze([]string{"-compare", idA + "," + idB, "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -compare: %v", err)
	}

	// Step 3: Verify that idA's LLMInterpretation is STILL preserved in both .json and .md!
	dataA2, err := os.ReadFile(jsonPathA)
	if err != nil {
		t.Fatal(err)
	}
	var sumA2 journey.JourneySummary
	if err := json.Unmarshal(dataA2, &sumA2); err != nil {
		t.Fatal(err)
	}
	if sumA2.LLMInterpretation == nil || sumA2.LLMInterpretation.Text != "这是 journey 的持久化解读内容。" {
		t.Fatalf("idA LLM interpretation was clobbered by -compare: %+v", sumA2.LLMInterpretation)
	}

	mdA2, err := os.ReadFile(mdPathA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdA2), "这是 journey 的持久化解读内容。") {
		t.Errorf("idA markdown lost LLM section after -compare:\n%s", mdA2)
	}
}
