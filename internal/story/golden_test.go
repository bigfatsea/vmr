// Ver 2026-07-29 12:00, by Sonnet 5

package story

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/story/profile"
)

// goldenRec builds one audit.Record for the golden fixture below — like
// journey_test.go's mkRec, but also sets Attempts (mkRec doesn't) so
// RenderMarkdown's endpoint column has something other than "-" to render.
func goldenRec(ts time.Time, durMS int64, msgs []any, sseBody string) audit.Record {
	body := map[string]any{"model": "agent", "stream": true, "messages": msgs}
	return audit.Record{
		TS: ts, DurMS: durMS, Model: "agent", Protocol: "openai", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: map[string][]string{}, Body: body},
			Response: &audit.Message{Status: 200, Headers: map[string][]string{}, Body: sseBody},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai:provider:agent", DurMS: durMS}},
	}
}

// goldenSSE builds a minimal SSE response carrying a usage block — sseText
// (journey_test.go) deliberately omits usage, but this golden test wants to
// exercise the Step-header token-count column too: a real bug (actual
// usage mislabeled as an "EST" pre-call estimate) was only caught because
// this column was rendered.
func goldenSSE(content string, promptTokens, completionTokens, cachedTokens int) string {
	return fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":%q}}],"model":"agent"}

data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"prompt_tokens_details":{"cached_tokens":%d}}}

data: [DONE]
`, content, promptTokens, completionTokens, cachedTokens)
}

// goldenFixture builds the 3-request, 1-lineage corpus this golden test
// renders: step 1 a plain-text answer, step 2 a tool_call/tool_result pair,
// step 3 a new user instruction (opens a new task). Built in Go rather than
// checked in as a testdata/*.jsonl file: *.jsonl is gitignored repo-wide
// (audit data may carry real conversation bodies — see .gitignore), and
// every other test fixture in this codebase is likewise constructed in code
// and written to a t.TempDir(), never committed as raw JSONL.
func goldenFixture() []audit.Record {
	at := func(sec int) time.Time { return time.Date(2026, 7, 29, 10, 0, sec, 0, time.UTC) }
	sys := msg("system", "You are a helpful assistant.")
	u1 := msg("user", "帮我查一下 A 股新股打新收益")
	a1 := map[string]any{"role": "assistant", "content": "好的，我来搜索相关数据。", "tool_calls": []any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "web_search", "arguments": "{}"}},
	}}
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "2026年A股新股打新平均收益率为12.5%，中签率0.03%"}
	a2 := msg("assistant", "根据搜索结果，2026年A股新股打新平均收益率为12.5%，中签率约0.03%。")
	u2 := msg("user", "继续，把前10名列出来")

	return []audit.Record{
		goldenRec(at(0), 1500, []any{sys, u1}, goldenSSE("好的，我来搜索相关数据。", 120, 15, 80)),
		goldenRec(at(2), 3200, []any{sys, u1, a1, t1}, goldenSSE("根据搜索结果，2026年A股新股打新平均收益率为12.5%，中签率约0.03%。", 280, 30, 200)),
		goldenRec(at(6), 5000, []any{sys, u1, a1, t1, a2, u2}, goldenSSE("好的，前10名的新股打新收益如下…", 400, 50, 300)),
	}
}

// TestGoldenMarkdown locks in RenderMarkdown's exact byte output for a small
// fixed corpus — the acceptance criterion ("golden test: a small testdata/
// corpus, Markdown output byte-stable, idempotent across re-runs"), and the
// gap flagged: nothing before this pinned the FULL rendered document, only
// substrings within it
// (TestRenderMarkdown_BasicStructure and friends) — a change that shifted
// step numbering, broke a <details> pairing, or reordered fields would slip
// through those. Runs both languages (this is the one place test
// infrastructure itself has to fork by language, since a golden
// test's whole point is pinning the FULL byte-for-byte output, which is
// necessarily language-specific) — regenerate both with `UPDATE_GOLDEN=1 go
// test ./internal/story/ -run TestGoldenMarkdown` after a deliberate
// rendering change, review the diff, then commit it.
func TestGoldenMarkdown(t *testing.T) {
	for _, tc := range []struct {
		lang     i18n.Lang
		goldenMD string
	}{
		{i18n.EN, filepath.Join("testdata", "golden.md")},
		{i18n.ZH, filepath.Join("testdata", "golden_zh.md")},
	} {
		t.Run(tc.lang.String(), func(t *testing.T) {
			path := writeJSONL(t, goldenFixture())

			g, err := ctxgraph.Scan([]string{path})
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if len(g.Lineages) != 1 {
				t.Fatalf("want 1 lineage in the golden fixture, got %d", len(g.Lineages))
			}
			j, err := Build(g.Lineages[0], profile.Generic, tc.lang)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			got := RenderMarkdown(j, tc.lang)

			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(tc.goldenMD, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Skipf("regenerated %s — review the diff, then re-run without UPDATE_GOLDEN", tc.goldenMD)
			}

			want, err := os.ReadFile(tc.goldenMD)
			if err != nil {
				t.Fatalf("reading golden file (run with UPDATE_GOLDEN=1 to create it): %v", err)
			}
			if got != string(want) {
				t.Errorf("golden output mismatch — after reviewing why, regenerate with UPDATE_GOLDEN=1 and diff the result before committing.\n=== got ===\n%s\n=== want ===\n%s", got, string(want))
			}
		})
	}
}
