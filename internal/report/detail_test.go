// Ver 2026-08-20 00:00, by Sonnet 5
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func TestReassembleSSEOpenAI(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}],"model":"agent"}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"hmm"}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"pa"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":1}"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		``,
		`data: [DONE]`,
	}, "\n")
	s := chatmsg.ReassembleSSE(raw)
	if s == nil {
		t.Fatal("nil summary")
	}
	if s.Content != "Hello" || s.Reasoning != "hmm" || s.Finish != "tool_calls" || s.Model != "agent" {
		t.Errorf("got %+v", s)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "read" || s.ToolCalls[0].Args != `{"path":1}` {
		t.Errorf("tool calls: %+v", s.ToolCalls)
	}
}

func TestReassembleSSEAnthropic(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":10}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me see"}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hi "}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"there"}}`,
		``,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t1","name":"search"}}`,
		``,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"x\"}"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
	}, "\n")
	s := chatmsg.ReassembleSSE(raw)
	if s == nil {
		t.Fatal("nil summary")
	}
	if s.Content != "Hi there" || s.Reasoning != "let me see" || s.Finish != "end_turn" || s.Model != "claude-x" {
		t.Errorf("got %+v", s)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "search" || s.ToolCalls[0].Args != `{"q":"x"}` {
		t.Errorf("tool calls: %+v", s.ToolCalls)
	}
}

func TestReassembleSSEUnparseable(t *testing.T) {
	if s := chatmsg.ReassembleSSE("plain text, not SSE at all"); s != nil {
		t.Errorf("expected nil, got %+v", s)
	}
}

func TestImagePlaceholder(t *testing.T) {
	got := chatmsg.RenderContent([]any{
		map[string]any{"type": "text", "text": "look:"},
		map[string]any{"type": "image_url", "image_url": map[string]any{
			"url": "data:image/png;base64," + strings.Repeat("A", 4096)}},
	})
	if strings.Contains(got, "AAAA") {
		t.Error("base64 payload leaked into output")
	}
	if !strings.Contains(got, "🖼 [image image/png ~3.0KB]") {
		t.Errorf("placeholder missing: %q", got)
	}
}

func TestChatMessagesAnthropicSystem(t *testing.T) {
	body := map[string]any{
		"system": "be nice",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "name": "run", "input": map[string]any{"cmd": "ls"}},
			}},
		},
	}
	msgs := chatmsg.Messages(body)
	if len(msgs) != 3 || msgs[0].Role != "system" || msgs[0].Text != "be nice" {
		t.Fatalf("msgs = %+v", msgs)
	}
	if !strings.Contains(msgs[2].Text, "🔧 tool_use run") {
		t.Errorf("tool_use not rendered: %q", msgs[2].Text)
	}
}

func TestFinalMessageJSON(t *testing.T) {
	body := map[string]any{
		"model": "m",
		"choices": []any{map[string]any{
			"finish_reason": "stop",
			"message":       map[string]any{"role": "assistant", "content": "done"},
		}},
	}
	s, ok := chatmsg.FinalMessage(body)
	if !ok || s.Content != "done" || s.Finish != "stop" {
		t.Errorf("ok=%v s=%+v", ok, s)
	}
	if _, ok := chatmsg.FinalMessage("not json"); ok {
		t.Error("string body should not parse")
	}
}

// findByOutcome returns the single details/ .md file whose name contains
// "_<outcome>_" — coordinate-hash naming (internal/reqdetail.FileName) means
// tests can no longer hardcode the exact filename, only the decorative
// parts they control (model/outcome).
func findByOutcome(t *testing.T, dir, outcome string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var matches []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") && strings.Contains(e.Name(), "_"+outcome+"_") {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 %q file in %s, got %v", outcome, dir, matches)
	}
	return matches[0]
}

func TestWriteDetailsEndToEnd(t *testing.T) {
	// Two records out of time order in the file; INDEX must sort by ts and
	// the norm trail must be translated in the passthrough note.
	lines := `{"ts":"2026-07-09T10:00:01+08:00","dur_ms":500,"model":"agent","protocol": "openai-completions","stream":false,"outcome":"ok","client":{"addr":"1.2.3.4:5","request":{"method":"POST","path":"/v1/chat/completions","headers":{"Content-Type":["application/json"]},"body":{"model":"agent","messages":[{"role":"user","content":"hi"}]}},"response":{"status":200,"headers":{"Content-Type":["application/json"]},"body":{"model":"agent","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}}},"attempts":[{"endpoint":"openai-completions/prov/real-1","protocol": "openai-completions","provider":"prov","model":"real-1","url":"https://x/v1","dur_ms":450,"request":{"method":"POST","path":"/v1","headers":{"Content-Type":["application/json"]},"body":{"model":"real-1","messages":[{"role":"user","content":"hi"}]}},"response":{"status":200,"headers":{"Content-Type":["application/json"]}},"norm":["model_rewrite"]}]}
{"ts":"2026-07-09T09:00:00+08:00","dur_ms":100,"model":"agent","protocol": "openai-completions","stream":false,"outcome":"error","client":{"addr":"1.2.3.4:5","request":{"method":"POST","path":"/v1/chat/completions","headers":{},"body":{"model":"agent","messages":[]}}},"attempts":[{"endpoint":"openai-completions/prov/real-1","protocol": "openai-completions","provider":"prov","model":"real-1","url":"https://x/v1","dur_ms":90,"request":{"headers":{},"body":{"model":"real-1","messages":[]}},"error":"network: dial tcp: refused","error_class":"network"}]}
`
	dir := t.TempDir()
	src := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(src, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "details")
	n, err := WriteDetails([]string{src}, out, nil, nil, i18n.EN, taskseg.OpenClawAware)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}

	okName := findByOutcome(t, out, "ok")
	okFile, err := os.ReadFile(filepath.Join(out, okName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## ① Client → VMR Request",
		"### Attempt 1/1 · openai-completions/prov/real-1 · ✅ · HTTP 200",
		`| 🔶 | model | "agent" → "real-1" |`,
		"`model_rewrite` — The real upstream model name was rewritten back to the virtual model name",
		"hello", // reassembled final message
	} {
		if !strings.Contains(string(okFile), want) {
			t.Errorf("ok file missing %q", want)
		}
	}

	errName := findByOutcome(t, out, "error")
	errFile, err := os.ReadFile(filepath.Join(out, errName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"❌ **error**: network: dial tcp: refused",
		"(no response record — connection dropped or the request was canceled)",
	} {
		if !strings.Contains(string(errFile), want) {
			t.Errorf("error file missing %q", want)
		}
	}

	// No .json sibling anymore — the raw record is addressable straight
	// from the source audit log via its coordinate (audit.LineAt /
	// `vmr replay -req -print`), so a same-named copy under details/ would
	// just be a byte-for-byte duplicate.
	if _, err := os.ReadFile(filepath.Join(out, strings.TrimSuffix(okName, ".md")+".json")); err == nil {
		t.Errorf("ok record has a .json sibling, want none (removed in P3.1)")
	}

	// Exports carry the same conversation bodies as the 0600 audit source —
	// they must not loosen its permissions (owner-only, no group/other bits).
	for _, p := range []string{out, filepath.Join(out, okName)} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: perm %o leaks group/other access, want owner-only", p, st.Mode().Perm())
		}
	}
}

// TestWriteDetailsByTag covers that ClientKeyTag never affects
// WriteDetails' own output: details/ stays one shared, unfiltered,
// un-duplicated pool of per-request files regardless of how many distinct
// tags the records carry (per-tag *views* over this data are the report's
// job now — vmr-requests-<tag>.md — not WriteDetails').
func TestWriteDetailsByTag(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, zone) }
	sys := msg("system", "sys")

	// Distinct opening user messages so each record anchors its own
	// session — three unrelated callers, not three turns of one
	// conversation (identical content would collapse them into a single
	// shared session, which would defeat the point of this test: three
	// independently-tagged records writing to the same shared details/).
	r1 := mkRec(at(0), "", []any{sys, msg("user", "alice's question")}, nil, sseText("a1"))
	r1.ClientKeyTag = "alice"
	r2 := mkRec(at(1), "", []any{sys, msg("user", "bob's question")}, nil, sseText("b1"))
	r2.ClientKeyTag = "bob"
	r3 := mkRec(at(2), "", []any{sys, msg("user", "an untagged question")}, nil, sseText("untagged"))
	// r3.ClientKeyTag left "" — legacy/catch-all/no-auth traffic.

	src := writeJSONL(t, []audit.Record{r1, r2, r3})
	a, err := AnalyzeSessions([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "details")
	n, err := WriteDetails([]string{src}, out, a, nil, i18n.EN, taskseg.OpenClawAware)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}

	// details/ holds all three records' files, unfiltered, exactly once.
	detailFiles, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(detailFiles) != 3 { // 3 records × .md only (no .json sibling, see P3.1), tags notwithstanding
		t.Fatalf("details/ entries = %d, want 3: %v", len(detailFiles), detailFiles)
	}

	// Nothing tag-aware gets written next to details/ — WriteDetails
	// produces no output of its own beyond the per-record detail pages and
	// (since P3.4) their shared evidence/ blobs, a sibling directory.
	topEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range topEntries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if want := []string{"details", "evidence"}; !slices.Equal(names, want) {
		t.Errorf("top-level entries = %v, want %v", names, want)
	}
}

// TestBuildOnRecordMatchesWriteDetails is the regression test for merging
// Build's aggregation pass with detail export: Build's onRecord hook
// (DetailWriter.Submit called inline, one pass over the audit source) must
// produce byte-identical output (filename AND content) to the old two-pass
// path (AnalyzeSessions -> a separate WriteDetails pass, an independent
// second read of the same file). This is also the cross-path consistency
// proof for internal/reqdetail.Render/FileName.
func TestBuildOnRecordMatchesWriteDetails(t *testing.T) {
	dir := t.TempDir()
	records := smallAuditRecords()
	path := writeTempJSONL(t, dir, records)

	sess, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	oldDir := filepath.Join(dir, "old-details")
	oldN, err := WriteDetails([]string{path}, oldDir, sess, nil, i18n.EN, taskseg.OpenClawAware)
	if err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(dir, "new-details")
	dw, err := NewDetailWriter(newDir, i18n.EN, taskseg.OpenClawAware)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Build([]string{path}, time.Now(), nil, nil, nil, dw.Submit); err != nil {
		t.Fatal(err)
	}
	newN, err := dw.Close()
	if err != nil {
		t.Fatal(err)
	}

	if oldN != newN {
		t.Fatalf("record count mismatch: old=%d new=%d", oldN, newN)
	}
	if oldN == 0 {
		t.Fatal("expected at least one detail file written; test fixture produced none")
	}

	oldFiles, err := os.ReadDir(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	newFiles, err := os.ReadDir(newDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldFiles) != len(newFiles) {
		t.Fatalf("file count mismatch: old=%d new=%d", len(oldFiles), len(newFiles))
	}
	for _, fi := range oldFiles {
		// File NAMES must match too, not just content — the whole point of
		// coordinate-hash naming is that both paths compute the identical
		// name for the same record, so an os.ReadFile miss here (not just
		// a content mismatch below) is itself part of what this test guards.
		oldBytes, err := os.ReadFile(filepath.Join(oldDir, fi.Name()))
		if err != nil {
			t.Fatal(err)
		}
		newBytes, err := os.ReadFile(filepath.Join(newDir, fi.Name()))
		if err != nil {
			t.Fatalf("missing in new-details (name mismatch between the two paths): %s", fi.Name())
		}
		if string(oldBytes) != string(newBytes) {
			t.Fatalf("content mismatch for %s:\n--- old ---\n%s\n--- new ---\n%s", fi.Name(), oldBytes, newBytes)
		}
	}
}

// TestWriteDetails_SubsetMatchesFullCorpus is P2.2/P2.3's other headline
// acceptance criterion: a record's detail page must be byte-identical
// (name AND content) whether it was rendered as part of a full-corpus scan
// or a scan of just the one file/subset containing it — proof that detail
// rendering no longer depends on session/task position (a run-scoped
// coordinate that shifts with the input file set), only on the record
// itself and its own lineage predecessor.
func TestWriteDetails_SubsetMatchesFullCorpus(t *testing.T) {
	dir := t.TempDir()
	zone := time.FixedZone("CST", 8*3600)
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, zone) }
	sys := msg("system", "sys")
	// Two turns of ONE conversation (r2's opening messages are r1's plus a
	// reply and a follow-up) so r2 has a real lineage predecessor — the
	// case that exercises the previous-turn link and delta highlight, the
	// two features that depend on cross-record correlation.
	r1 := mkRec(at(0), "", []any{sys, msg("user", "first question")}, nil, sseText("first answer"))
	r2 := mkRec(at(1), "", []any{sys, msg("user", "first question"), msg("assistant", "first answer"), msg("user", "follow-up question")}, nil, sseText("second answer"))
	// An unrelated third file that only belongs to the "full corpus" run —
	// proves the subset run doesn't need it to reproduce r1/r2 identically.
	other := mkRec(at(5), "", []any{sys, msg("user", "unrelated")}, nil, sseText("unrelated answer"))

	targetPath := filepath.Join(dir, "target.jsonl")
	writeRecordsTo(t, targetPath, []audit.Record{r1, r2})
	otherPath := filepath.Join(dir, "other.jsonl")
	writeRecordsTo(t, otherPath, []audit.Record{other})

	fullSess, err := AnalyzeSessions([]string{targetPath, otherPath})
	if err != nil {
		t.Fatal(err)
	}
	fullOut := filepath.Join(dir, "full-details")
	if _, err := WriteDetails([]string{targetPath, otherPath}, fullOut, fullSess, nil, i18n.EN, taskseg.OpenClawAware); err != nil {
		t.Fatal(err)
	}

	subsetSess, err := AnalyzeSessions([]string{targetPath})
	if err != nil {
		t.Fatal(err)
	}
	subsetOut := filepath.Join(dir, "subset-details")
	if _, err := WriteDetails([]string{targetPath}, subsetOut, subsetSess, nil, i18n.EN, taskseg.OpenClawAware); err != nil {
		t.Fatal(err)
	}

	// Every file target.jsonl produced in the subset run must exist in the
	// full run under the exact same name, with identical content.
	subsetFiles, err := os.ReadDir(subsetOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(subsetFiles) != 2 { // 2 records × .md only (no .json sibling, see P3.1)
		t.Fatalf("subset details/ entries = %d, want 2", len(subsetFiles))
	}
	for _, fi := range subsetFiles {
		subsetBytes, err := os.ReadFile(filepath.Join(subsetOut, fi.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fullBytes, err := os.ReadFile(filepath.Join(fullOut, fi.Name()))
		if err != nil {
			t.Fatalf("%s present in subset run but not in full-corpus run (name not reproducible across input-set size)", fi.Name())
		}
		if string(subsetBytes) != string(fullBytes) {
			t.Fatalf("content differs for %s between subset and full-corpus runs:\n--- subset ---\n%s\n--- full ---\n%s",
				fi.Name(), subsetBytes, fullBytes)
		}
	}
}

// writeRecordsTo is writeTempJSONL's audit.Record-typed sibling: this
// test needs mkRec's richer audit.Record fixtures (for a real lineage
// relationship between r1/r2), not smallAuditRecords' map[string]any shape.
func writeRecordsTo(t *testing.T, path string, recs []audit.Record) {
	t.Helper()
	var b strings.Builder
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}
