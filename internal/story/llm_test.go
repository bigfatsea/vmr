// Ver 2026-07-30 22:00, by Sonnet 5

package story

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

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// mockChatServer returns an httptest.Server that answers any POST with a
// fixed assistant message, and a pointer to a call counter so tests can
// assert how many times it was actually hit (e.g. a cache hit must not hit
// it a second time). It also records the last request body/headers seen,
// for tests that need to inspect what was actually sent.
func mockChatServer(t *testing.T, reply string) (*httptest.Server, *int, **http.Request, *[]byte) {
	t.Helper()
	calls := 0
	var lastReq *http.Request
	var lastBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		lastReq = r.Clone(r.Context())
		body, _ := io.ReadAll(r.Body)
		lastBody = body
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": reply}},
			},
		})
	}))
	t.Cleanup(ts.Close)
	return ts, &calls, &lastReq, &lastBody
}

func testPack(t *testing.T) EvidencePack {
	t.Helper()
	a := JourneySummary{ID: "j-a", Title: "A", Metrics: Metrics{ModelMS: 1000}}
	b := JourneySummary{ID: "j-b", Title: "B", Metrics: Metrics{ModelMS: 2000}}
	cmp := Compare(a, b, i18n.EN)
	return EvidencePack{Comparison: cmp, TaskTitlesA: []string{"t1"}, TaskTitlesB: []string{"t1"}}
}

// TestBuildEvidencePack_FromRealJourney covers BuildEvidencePack/
// buildToolIndex/journeyTaskTitles against a real *Journey built from audit
// records — every other test in this file exercises Interpret/callLLM/
// caching against the hand-built literal testPack() returns, which never
// goes through these three functions. cmd_story.go's compareJourneys calls
// BuildEvidencePack(jA, jB, cmp, i18n.EN) in production, so this closes a real gap:
// the per-Journey evidence-pack assembly (tool index, task titles) had zero
// direct unit coverage.
func TestBuildEvidencePack_FromRealJourney(t *testing.T) {
	at1 := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 7, 28, 0, 1, 0, 0, time.UTC)
	rec1 := mkExtrasRec(at1, "sys", "调研任务", "openai-completions:p:m", 100, 10, 0, "tool_calls",
		[]map[string]any{writeToolCall("exec", "", "")})
	rec2 := mkExtrasRec(at2, "sys", "调研任务", "openai-completions:p:m", 110, 10, 0, "stop", nil)
	path := writeJSONL(t, []audit.Record{rec1, rec2})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	s := Summarize(j, i18n.EN)
	cmp := Compare(s, s, i18n.EN)

	pack := BuildEvidencePack(j, j, cmp, i18n.EN)

	if len(pack.TaskTitlesA) == 0 || pack.TaskTitlesA[0] == "" {
		t.Errorf("TaskTitlesA should list the journey's task title(s), got %+v", pack.TaskTitlesA)
	}
	if len(pack.ToolIndexA) != 2 {
		t.Fatalf("ToolIndexA = %+v, want 2 entries (one per step)", pack.ToolIndexA)
	}
	if pack.ToolIndexA[0].Seq != 1 || len(pack.ToolIndexA[0].Tools) != 1 || pack.ToolIndexA[0].Tools[0] != "exec" {
		t.Errorf("ToolIndexA[0] = %+v, want seq=1 tools=[exec]", pack.ToolIndexA[0])
	}
	if pack.ToolIndexA[1].Seq != 2 || len(pack.ToolIndexA[1].Tools) != 0 {
		t.Errorf("ToolIndexA[1] = %+v, want seq=2 with no tool calls", pack.ToolIndexA[1])
	}
	if pack.ToolIndexA[1].Brief == "" {
		t.Error("ToolIndexA[1].Brief should never be empty (falls back to the placeholder when RespText is empty)")
	}
	// A and B are the same Journey here (jA==jB), so ToolIndexB must match
	// ToolIndexA exactly — this also confirms BuildEvidencePack doesn't
	// silently share/alias state between the two sides.
	if len(pack.ToolIndexB) != len(pack.ToolIndexA) {
		t.Errorf("ToolIndexB = %+v, want same shape as ToolIndexA", pack.ToolIndexB)
	}
}

func TestLLMOptions_ChatURL(t *testing.T) {
	cases := map[string]string{
		"192.168.0.117:8800":      "http://192.168.0.117:8800/v1/chat/completions",
		"127.0.0.1:8800":          "http://127.0.0.1:8800/v1/chat/completions",
		"127.0.0.1:8800/":         "http://127.0.0.1:8800/v1/chat/completions",
		"https://example.com:443": "https://example.com:443/v1/chat/completions",
		// A trailing "/v1" (someone typing an OpenAI-style base URL out of
		// habit) must not double up into "/v1/v1/..." — code-review finding.
		"192.168.0.117:8800/v1":      "http://192.168.0.117:8800/v1/chat/completions",
		"192.168.0.117:8800/v1/":     "http://192.168.0.117:8800/v1/chat/completions",
		"https://example.com:443/v1": "https://example.com:443/v1/chat/completions",
	}
	for addr, want := range cases {
		opts := LLMOptions{Addr: addr}
		if got := opts.chatURL(); got != want {
			t.Errorf("chatURL(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestLLMOptions_Enabled(t *testing.T) {
	if (LLMOptions{}).Enabled() {
		t.Error("no Addr should mean disabled")
	}
	if !(LLMOptions{Addr: "127.0.0.1:8800"}).Enabled() {
		t.Error("a non-empty Addr should mean enabled")
	}
}

func TestEvidencePack_EstimateChars(t *testing.T) {
	pack := testPack(t)
	n := pack.EstimateChars()
	if n <= 0 {
		t.Fatalf("EstimateChars() = %d, want > 0", n)
	}
}

// TestEvidencePack_EstimateChars_CountsRunesNotBytes covers the code-review
// finding that EstimateChars must count Unicode characters, not UTF-8 bytes
// — a pack full of Chinese text (task titles, excerpts, briefs) would
// otherwise report roughly 3x its actual character count.
func TestEvidencePack_EstimateChars_CountsRunesNotBytes(t *testing.T) {
	asciiPack := EvidencePack{TaskTitlesA: []string{strings.Repeat("x", 30)}}
	zhPack := EvidencePack{TaskTitlesA: []string{strings.Repeat("测", 30)}}

	asciiChars := asciiPack.EstimateChars()
	zhChars := zhPack.EstimateChars()

	// Same rune count on both sides (30 repeats of a single character, plus
	// identical JSON scaffolding) should give the same EstimateChars —
	// under the old byte-counting behavior, zhChars would come out ~3x
	// asciiChars instead (each 测 is 3 UTF-8 bytes vs 1 for x).
	if zhChars != asciiChars {
		t.Errorf("EstimateChars() = %d (Chinese) vs %d (ASCII), want equal rune counts for equal-length strings", zhChars, asciiChars)
	}
}

// TestInterpret_CallsServerAndParsesReply covers the happy path: a plain
// POST to the given addr, model sent verbatim, reply content extracted.
func TestInterpret_CallsServerAndParsesReply(t *testing.T) {
	ts, calls, _, lastBody := mockChatServer(t, "一句话结论：测试通过。\n\n## 核心假设\n...")
	addr := strings.TrimPrefix(ts.URL, "http://")
	opts := LLMOptions{Addr: addr, Model: "agent"}
	pack := testPack(t)

	res, err := Interpret(context.Background(), opts, pack, i18n.EN)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Cached {
		t.Error("first call should not be a cache hit")
	}
	if !strings.Contains(res.Text, "一句话结论") {
		t.Errorf("Text = %q, want it to contain the mock reply", res.Text)
	}
	if *calls != 1 {
		t.Fatalf("server calls = %d, want 1", *calls)
	}

	var sentReq chatCompletionRequest
	if err := json.Unmarshal(*lastBody, &sentReq); err != nil {
		t.Fatalf("request body not valid JSON: %v\n%s", err, *lastBody)
	}
	if sentReq.Model != "agent" {
		t.Errorf("sent model = %q, want agent", sentReq.Model)
	}
	if sentReq.Stream {
		t.Error("request must not be streaming")
	}
	if len(sentReq.Messages) != 2 || sentReq.Messages[0].Role != "system" || sentReq.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want [system, user]", sentReq.Messages)
	}
	if !strings.Contains(sentReq.Messages[1].Content, `"a_journey"`) {
		t.Error("user message should embed the evidence pack JSON")
	}
	// v2 prompt requirements: the
	// confidence-tiered root-cause table plus the excerpt-boundary caveat
	// must actually be in the instructions sent, not just described in a
	// comment — this is the regression guard against silently reverting to
	// the old flat "assumption list" format.
	for _, want := range []string{"Candidate Root Cause", "Confidence", "causal chain", "not that it doesn't exist beyond it"} {
		if !strings.Contains(sentReq.Messages[0].Content, want) {
			t.Errorf("system prompt missing %q (v2 confidence-tier/excerpt-boundary requirement):\n%s", want, sentReq.Messages[0].Content)
		}
	}
}

// TestInterpret_APIKeySentAsBearer covers -llm-key: when set, it must be
// sent as an Authorization: Bearer header; when unset, no such header.
func TestInterpret_APIKeySentAsBearer(t *testing.T) {
	ts, _, lastReq, _ := mockChatServer(t, "ok")
	addr := strings.TrimPrefix(ts.URL, "http://")

	if _, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent", APIKey: "sk-test-123"}, testPack(t), i18n.EN); err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if got := (*lastReq).Header.Get("Authorization"); got != "Bearer sk-test-123" {
		t.Errorf("Authorization header = %q, want Bearer sk-test-123", got)
	}
}

func TestInterpret_NoAPIKeyMeansNoAuthHeader(t *testing.T) {
	ts, _, lastReq, _ := mockChatServer(t, "ok")
	addr := strings.TrimPrefix(ts.URL, "http://")

	if _, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent"}, testPack(t), i18n.EN); err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if got := (*lastReq).Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header = %q, want empty (no -llm-key given)", got)
	}
}

// TestInterpret_Disabled covers the no -llm-addr case: an explicit error,
// not a silent no-op, so the caller can tell "never configured" apart from
// "configured but the call failed" if it ever needs to (today both just
// mean "skip the section", but the distinction costs nothing to keep).
func TestInterpret_Disabled(t *testing.T) {
	if _, err := Interpret(context.Background(), LLMOptions{}, testPack(t), i18n.EN); err == nil {
		t.Error("Interpret with no Addr should return an error")
	}
}

// TestInterpret_ServerErrorDegradesToError covers the failure path: a non-2xx
// response must surface as an error (for the caller to log and skip the LLM
// section), never as a panic or a fabricated success.
func TestInterpret_ServerErrorDegradesToError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("upstream on fire"))
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	_, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent"}, testPack(t), i18n.EN)
	if err == nil {
		t.Fatal("Interpret should error on a non-2xx response")
	}
}

// TestInterpret_CacheHitSkipsSecondCall covers the disk cache: same model +
// same pack must not hit the server twice.
func TestInterpret_CacheHitSkipsSecondCall(t *testing.T) {
	ts, calls, _, _ := mockChatServer(t, "cached reply")
	addr := strings.TrimPrefix(ts.URL, "http://")
	cacheDir := filepath.Join(t.TempDir(), ".llm-cache")
	opts := LLMOptions{Addr: addr, Model: "agent", CacheDir: cacheDir}
	pack := testPack(t)

	res1, err := Interpret(context.Background(), opts, pack, i18n.EN)
	if err != nil {
		t.Fatalf("first Interpret: %v", err)
	}
	if res1.Cached {
		t.Error("first call should not be cached")
	}

	res2, err := Interpret(context.Background(), opts, pack, i18n.EN)
	if err != nil {
		t.Fatalf("second Interpret: %v", err)
	}
	if !res2.Cached {
		t.Error("second call with identical (addr, model, pack) should be a cache hit")
	}
	if res2.Text != res1.Text {
		t.Errorf("cached text = %q, want it to match the original %q", res2.Text, res1.Text)
	}
	if *calls != 1 {
		t.Errorf("server calls = %d, want 1 (second call should hit the cache, not the server)", *calls)
	}
}

// TestInterpret_CacheKeyIncludesModel verifies that cache keys include the model name
// in the alternative proposal's cache key: switching -llm-model must not
// silently reuse a different model's cached answer.
func TestInterpret_CacheKeyIncludesModel(t *testing.T) {
	ts, calls, _, _ := mockChatServer(t, "reply")
	addr := strings.TrimPrefix(ts.URL, "http://")
	cacheDir := filepath.Join(t.TempDir(), ".llm-cache")
	pack := testPack(t)

	if _, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent", CacheDir: cacheDir}, pack, i18n.EN); err != nil {
		t.Fatalf("Interpret (model=agent): %v", err)
	}
	res, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "coding", CacheDir: cacheDir}, pack, i18n.EN)
	if err != nil {
		t.Fatalf("Interpret (model=coding): %v", err)
	}
	if res.Cached {
		t.Error("a different -llm-model must not hit the previous model's cache entry")
	}
	if *calls != 2 {
		t.Errorf("server calls = %d, want 2 (each model should call through once)", *calls)
	}
}

func TestRenderLLMSection(t *testing.T) {
	opts := LLMOptions{Model: "agent"}
	md := RenderLLMSection(opts, InterpretResult{Text: "一句话结论：测试。", Cached: false}, i18n.EN, "")
	for _, want := range []string{"## LLM Interpretation", "agent", "not the fact layer", "一句话结论：测试。"} {
		if !strings.Contains(md, want) {
			t.Errorf("RenderLLMSection missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "cache hit") {
		t.Error("uncached result should not claim a cache hit")
	}

	cachedMD := RenderLLMSection(opts, InterpretResult{Text: "x", Cached: true}, i18n.EN, "")
	if !strings.Contains(cachedMD, "cache hit") {
		t.Error("cached result should say so")
	}
}

// TestDowngradeH2Headings covers the P1.4 render-layer fallback: even
// though the prompt already asks the model for level-3 subsection
// headings (i18n/story_llm.go), a model that ignores the instruction and
// still emits "## X" must not corrupt the document's own outline —
// RenderLLMSection downgrades it to "### X" deterministically, except
// inside fenced code blocks, where "## " is literal content.
func TestDowngradeHeadingLevels(t *testing.T) {
	t.Run("a stray H2 outside any fence is downgraded to H3", func(t *testing.T) {
		got := downgradeHeadingLevels("intro\n## 候选根因\nbody")
		if !strings.Contains(got, "### 候选根因") {
			t.Errorf("got %q, want the H2 downgraded to H3", got)
		}
		if strings.Contains(got, "\n## 候选根因") {
			t.Errorf("got %q, the original H2 line must not survive", got)
		}
	})

	t.Run("a '## ' line inside a ``` fenced code block is left alone", func(t *testing.T) {
		text := "before\n```\n## not a heading\n```\nafter"
		got := downgradeHeadingLevels(text)
		if !strings.Contains(got, "## not a heading") {
			t.Errorf("got %q, a fenced '## ' line must stay untouched", got)
		}
	})

	t.Run("a '## ' line inside a ~~~ fenced code block is left alone", func(t *testing.T) {
		text := "before\n~~~\n## not a heading\n~~~\nafter"
		got := downgradeHeadingLevels(text)
		if !strings.Contains(got, "## not a heading") {
			t.Errorf("got %q, a ~~~-fenced '## ' line must stay untouched", got)
		}
	})

	t.Run("mixed: only the fence-external H2 is downgraded", func(t *testing.T) {
		text := "## real heading\n```\n## fenced\n```\n## another real heading"
		got := downgradeHeadingLevels(text)
		if strings.Count(got, "### real heading") != 1 || !strings.Contains(got, "### another real heading") {
			t.Errorf("got %q, want both fence-external H2s downgraded", got)
		}
		if !strings.Contains(got, "## fenced") {
			t.Errorf("got %q, want the fenced H2 left untouched", got)
		}
	})

	// TestDowngradeHeadingLevels/preserves_the_LLM.s_own_internal_hierarchy
	// covers the review finding this test was added for: a model that
	// nests "### 1.1" under its own "## 1." without shifting the whole
	// block down to fit under this document's outer "## LLM Interpretation"
	// wrapper. Rewriting only "## " -> "### " would leave "### 1.1" at the
	// SAME level as the now-"### 1." parent — flattening a two-level
	// hierarchy into siblings. Every level must shift uniformly by one.
	t.Run("preserves the LLM's own internal hierarchy: every level shifts by one, not just H2", func(t *testing.T) {
		text := "## 1. 候选根因\n### 1.1 工具参数错误\n#### 1.1.1 detail"
		got := downgradeHeadingLevels(text)
		want := "### 1. 候选根因\n#### 1.1 工具参数错误\n##### 1.1.1 detail"
		if got != want {
			t.Errorf("got %q, want %q — parent/child relationship must survive the shift", got, want)
		}
	})

	t.Run("level 6 is already the deepest ATX level and stays at 6, not 7", func(t *testing.T) {
		text := "###### already max depth"
		if got := downgradeHeadingLevels(text); got != text {
			t.Errorf("got %q, want unchanged %q (no level 7 in ATX Markdown)", got, text)
		}
	})

	t.Run("level 1 is left alone: the prompt never asks for it and there's no wrapper level to nest under", func(t *testing.T) {
		text := "# top level\nbody"
		if got := downgradeHeadingLevels(text); got != text {
			t.Errorf("got %q, want unchanged %q", got, text)
		}
	})

	t.Run("no headings at all: text unchanged", func(t *testing.T) {
		text := "just a paragraph, no headings\n"
		if got := downgradeHeadingLevels(text); got != text {
			t.Errorf("got %q, want unchanged %q", got, text)
		}
	})
}

// TestRenderLLMSection_ScopeDistinguishesTwoSectionsInOneDocument covers
// -compare's own real failure mode: two independent LLM calls (the overall
// comparison, and — when a divergence point was found — a second call
// scoped to just that point) land in the SAME Markdown document. Without a
// distinguishing scope label, both would render under the byte-identical
// heading "## LLM Interpretation (model: X)", making the document's own
// outline look like the same section pasted in twice even though the
// content differs.
func TestRenderLLMSection_ScopeDistinguishesTwoSectionsInOneDocument(t *testing.T) {
	opts := LLMOptions{Model: "agent"}
	overall := RenderLLMSection(opts, InterpretResult{Text: "overall text"}, i18n.EN, i18n.LLM(i18n.EN).ScopeOverall)
	divergence := RenderLLMSection(opts, InterpretResult{Text: "divergence text"}, i18n.EN, i18n.LLM(i18n.EN).ScopeDivergence)

	overallTitle := strings.SplitN(overall, "\n", 2)[0]
	divergenceTitle := strings.SplitN(divergence, "\n", 2)[0]
	if overallTitle == divergenceTitle {
		t.Errorf("the two sections' titles must differ, got the same %q for both", overallTitle)
	}
	if !strings.Contains(overallTitle, "overall comparison") {
		t.Errorf("overall section title = %q, want it to name its scope", overallTitle)
	}
	if !strings.Contains(divergenceTitle, "divergence point") {
		t.Errorf("divergence section title = %q, want it to name its scope", divergenceTitle)
	}

	// scope "" (renderJourney's single-section document) keeps the plain,
	// unlabeled title — no behavior change for the case that was never
	// ambiguous in the first place.
	plain := RenderLLMSection(opts, InterpretResult{Text: "x"}, i18n.EN, "")
	plainTitle := strings.SplitN(plain, "\n", 2)[0]
	if strings.Contains(plainTitle, "·") {
		t.Errorf("plain (scope-less) title = %q, must not carry a scope separator", plainTitle)
	}
}

// TestInterpret_EmptyCacheFileTreatedAsMiss is the R93 regression: a 0-byte
// (or whitespace-only) cache file must be treated as a miss — a hit is
// served verbatim and never re-calls, so an empty entry would otherwise
// surface as an "empty LLM response" on every later run, permanently.
func TestInterpret_EmptyCacheFileTreatedAsMiss(t *testing.T) {
	ts, calls, _, _ := mockChatServer(t, "real reply")
	addr := strings.TrimPrefix(ts.URL, "http://")
	cacheDir := t.TempDir()
	pack := testPack(t)
	key, err := cacheKey("agent", pack, i18n.EN)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	for _, content := range []string{"", "   \n\t "} {
		if err := os.WriteFile(cachePath(cacheDir, key), []byte(content), 0o600); err != nil {
			t.Fatalf("seed corrupt cache: %v", err)
		}
		res, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent", CacheDir: cacheDir}, pack, i18n.EN)
		if err != nil {
			t.Fatalf("Interpret: %v", err)
		}
		if res.Cached || res.Text != "real reply" {
			t.Errorf("corrupt cache (content=%q) must be a miss, got cached=%v text=%q", content, res.Cached, res.Text)
		}
		if *calls != 1 {
			t.Errorf("server calls = %d, want 1 (corrupt cache must force a fresh call)", *calls)
		}
		// The fresh call overwrites the bad entry, and no temp file leaks.
		data, err := os.ReadFile(cachePath(cacheDir, key))
		if err != nil || string(data) != "real reply" {
			t.Errorf("cache entry should now hold the fresh reply, got %q err=%v", data, err)
		}
		entries, _ := os.ReadDir(cacheDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp") {
				t.Errorf("temp cache file %s leaked after successful write", e.Name())
			}
		}
		*calls = 0
	}
}

// TestInterpret_CacheWriteFailOpenWithWarning is the R93 fail-open
// regression: an unusable cache directory must not fail (or panic) the
// analysis — the reply still comes back uncached.
func TestInterpret_CacheWriteFailOpenWithWarning(t *testing.T) {
	ts, _, _, _ := mockChatServer(t, "uncached reply")
	addr := strings.TrimPrefix(ts.URL, "http://")
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent", CacheDir: filepath.Join(blocker, "cache")}, testPack(t), i18n.EN)
	if err != nil {
		t.Fatalf("cache write failure must stay fail-open, got error: %v", err)
	}
	if res.Cached || res.Text != "uncached reply" {
		t.Errorf("unexpected result %+v", res)
	}
}

func TestCallLLM_RetryOn429WithRetryAfter(t *testing.T) {
	t.Parallel()
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "success after retry"}},
			},
		})
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	res, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent"}, testPack(t), i18n.EN)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Text != "success after retry" {
		t.Errorf("res.Text = %q, want 'success after retry'", res.Text)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestCallLLM_RetryOn5xx(t *testing.T) {
	t.Parallel()
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("service unavailable"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "recovered"}},
			},
		})
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	res, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent"}, testPack(t), i18n.EN)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Text != "recovered" {
		t.Errorf("res.Text = %q, want 'recovered'", res.Text)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestCallLLM_NonRetryable401DoesNotRetry(t *testing.T) {
	t.Parallel()
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	_, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent"}, testPack(t), i18n.EN)
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (non-retryable 401 should not retry)", calls)
	}
}

func TestCallLLM_MaxRetriesExhausted(t *testing.T) {
	t.Parallel()
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	_, err := Interpret(context.Background(), LLMOptions{Addr: addr, Model: "agent"}, testPack(t), i18n.EN)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	// Initial + 2 retries = 3 calls
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (1 initial + 2 retries)", calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	if d := parseRetryAfter(h); d != 0 {
		t.Errorf("empty header = %v, want 0", d)
	}

	h.Set("Retry-After", "10")
	if d := parseRetryAfter(h); d != 10*time.Second {
		t.Errorf("parseRetryAfter(10) = %v, want 10s", d)
	}

	h.Set("Retry-After", "0")
	if d := parseRetryAfter(h); d != 0 {
		t.Errorf("parseRetryAfter(0) = %v, want 0", d)
	}

	h.Set("Retry-After", "invalid")
	if d := parseRetryAfter(h); d != 0 {
		t.Errorf("parseRetryAfter(invalid) = %v, want 0", d)
	}

	future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	h.Set("Retry-After", future)
	if d := parseRetryAfter(h); d <= 0 || d > 6*time.Second {
		t.Errorf("parseRetryAfter(HTTP-date) = %v, want ~5s", d)
	}
}

