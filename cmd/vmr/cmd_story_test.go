// Ver 2026-07-29 16:15, by Sonnet 5

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
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
	if len(entries) != 1 {
		t.Fatalf("want 1 story file, got %d", len(entries))
	}
	content, err := os.ReadFile(filepath.Join(outDir, "stories", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "调研一下 A 股新股打新收益") {
		t.Errorf("rendered journey missing root instruction:\n%s", content)
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
	if len(entries) != 2 {
		t.Fatalf("want 2 story files, got %d: %v", len(entries), entries)
	}
	var all string
	for _, e := range entries {
		content, err := os.ReadFile(filepath.Join(outDir, "stories", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		all += string(content)
	}
	if !strings.Contains(all, "调研一下 A 股新股打新收益") || !strings.Contains(all, "帮我写个 release note") {
		t.Errorf("both journeys' root instructions should appear across the two files:\n%s", all)
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

func captureStdoutErr(t *testing.T, fn func() error) error {
	t.Helper()
	var err error
	captureStdout(t, func() { err = fn() })
	return err
}
