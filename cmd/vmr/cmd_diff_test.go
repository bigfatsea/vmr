// Ver 2026-09-08, by coding assistant
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
)

func makeTestRecord(model, protocol, sysPrompt string, msgs []map[string]string, tools []string, inTok, outTok, cacheRead int64) audit.Record {
	body := map[string]any{
		"model": model,
	}
	var msgList []any
	if sysPrompt != "" {
		msgList = append(msgList, map[string]any{
			"role":    "system",
			"content": sysPrompt,
		})
	}
	for _, m := range msgs {
		msgList = append(msgList, map[string]any{
			"role":    m["role"],
			"content": m["content"],
		})
	}
	body["messages"] = msgList

	if len(tools) > 0 {
		var toolList []any
		for _, t := range tools {
			toolList = append(toolList, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t,
				},
			})
		}
		body["tools"] = toolList
	}

	rec := audit.Record{
		TS:       time.Now(),
		Model:    model,
		Protocol: protocol,
		Outcome:  "ok",
		Attempts: []audit.Attempt{
			{
				Endpoint: "provider:mock:model",
				Response: &audit.Message{Status: 200},
			},
		},
	}
	rec.Client.Request.Body = body

	respBody := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":           inTok,
			"completion_tokens":       outTok,
			"input_tokens":            inTok - cacheRead,
			"output_tokens":           outTok,
			"cache_read_input_tokens": cacheRead,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": cacheRead,
			},
		},
	}
	rec.Client.Response = &audit.Message{
		Body: respBody,
	}

	return rec
}

func recordLine(rec audit.Record) string {
	b, _ := json.Marshal(rec)
	return string(b) + "\n"
}

func TestCmdDiff_ArgValidation(t *testing.T) {
	if err := cmdDiff([]string{}); err == nil {
		t.Error("expected error for 0 args, got nil")
	}
	if err := cmdDiff([]string{"coord1"}); err == nil {
		t.Error("expected error for 1 arg, got nil")
	}
	if err := cmdDiff([]string{"coord1", "coord2", "coord3"}); err == nil {
		t.Error("expected error for 3 args, got nil")
	}
}

func TestCmdDiff_LocateFailures(t *testing.T) {
	// 1. Bad coordinate format
	if err := cmdDiff([]string{"no-colon", "audit.jsonl:1"}); err == nil {
		t.Error("expected error for coord with no colon, got nil")
	}
	if err := cmdDiff([]string{"audit.jsonl:0", "audit.jsonl:1"}); err == nil {
		t.Error("expected error for line 0, got nil")
	}
	if err := cmdDiff([]string{"audit.jsonl:abc", "audit.jsonl:1"}); err == nil {
		t.Error("expected error for non-integer line, got nil")
	}

	// 2. Nonexistent file
	if err := cmdDiff([]string{"nonexistent_audit_file_99999.jsonl:1", "audit.jsonl:1"}); err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}

	// 3. Line out of range
	auditFile := writeTempFile(t, "test-audit.jsonl", `{"model":"m1"}`+"\n")
	coordValid := fmt.Sprintf("%s:1", auditFile)
	coordOutOfRange := fmt.Sprintf("%s:999", auditFile)
	if err := cmdDiff([]string{coordValid, coordOutOfRange}); err == nil {
		t.Error("expected error for line out of range, got nil")
	}
}

func TestCmdDiff_SyntheticRecords_Extension(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "you are helpful", []map[string]string{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi there"},
	}, []string{"read"}, 100, 20, 0)

	recB := makeTestRecord("coding", "openai-completions", "you are helpful", []map[string]string{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi there"},
		{"role": "user", "content": "what is 2+2?"},
		{"role": "assistant", "content": "4"},
	}, []string{"read"}, 150, 25, 0)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_ext.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "Messages (A=2, B=4, LCP=2)") {
		t.Errorf("expected LCP=2 and counts (2, 4), got:\n%s", out)
	}
	if !strings.Contains(out, "[2] first divergence: B extends A with 2 new message(s) (first role=user)") {
		t.Errorf("expected extension divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "A tail: 0") || !strings.Contains(out, "B tail: 2 new message(s)") {
		t.Errorf("expected tail lines, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B is an extension of A") {
		t.Errorf("expected extension verdict, got:\n%s", out)
	}
	if !strings.Contains(out, "Structural fact, not a root cause.") {
		t.Errorf("expected disclaimer, got:\n%s", out)
	}
}

func TestCmdDiff_SyntheticRecords_TruncatedRetry(t *testing.T) {
	recA := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
		{"role": "assistant", "content": "doing step 1"},
		{"role": "user", "content": "do step 2"},
	}, []string{"tool1"}, 1000, 200, 500)

	recB := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
		{"role": "assistant", "content": "doing step 1 differently"},
	}, []string{"tool1"}, 800, 150, 400)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_retry.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "Messages (A=3, B=2, LCP=1)") {
		t.Errorf("expected LCP=1, got:\n%s", out)
	}
	if !strings.Contains(out, "[1] first divergence: A msg#1 (role=assistant) rewritten in B (hash differs)") {
		t.Errorf("expected rewritten divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "A tail: 2 new message(s)") {
		t.Errorf("expected A tail: 2 new message(s), got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B looks like a context-truncated retry of A (tail replaced, same system).") {
		t.Errorf("expected truncated retry verdict, got:\n%s", out)
	}
}

func TestCmdDiff_SyntheticRecords_TruncatedPrefix(t *testing.T) {
	recA := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
		{"role": "assistant", "content": "step 1 done"},
		{"role": "user", "content": "step 2"},
	}, []string{}, 100, 50, 0)

	recB := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
	}, []string{}, 50, 10, 0)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_prefix.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "Messages (A=3, B=1, LCP=1)") {
		t.Errorf("expected LCP=1, got:\n%s", out)
	}
	if !strings.Contains(out, "[1] first divergence: B truncated after 1 message(s)") {
		t.Errorf("expected truncated divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "A tail: 2 new message(s)") {
		t.Errorf("expected A tail, got:\n%s", out)
	}
	if !strings.Contains(out, "B tail: 0") {
		t.Errorf("expected B tail: 0, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B looks like a context-truncated prefix of A (2 message(s) truncated, same system).") {
		t.Errorf("expected truncated prefix verdict, got:\n%s", out)
	}
}

func TestCmdDiff_SyntheticRecords_UnrelatedContext(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "", []map[string]string{
		{"role": "user", "content": "write python script"},
	}, nil, 10, 10, 0)

	recB := makeTestRecord("coding", "openai-completions", "", []map[string]string{
		{"role": "user", "content": "explain quantum physics"},
	}, nil, 10, 10, 0)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_unrelated.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "Messages (A=1, B=1, LCP=0)") {
		t.Errorf("expected LCP=0, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B and A share no common message context (LCP=0, unrelated context).") {
		t.Errorf("expected unrelated context verdict, got:\n%s", out)
	}
}

func TestCmdDiff_SyntheticRecords_SystemChanged(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "System instructions v1", []map[string]string{
		{"role": "user", "content": "hello"},
	}, nil, 10, 10, 0)

	recB := makeTestRecord("coding", "openai-completions", "System instructions v2", []map[string]string{
		{"role": "user", "content": "hello"},
	}, nil, 10, 10, 0)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_sys.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "sys_hash") && strings.Contains(l, "(same)") {
			t.Errorf("sys_hash line should not contain (same): %s", l)
		}
	}
	if !strings.Contains(out, "Verdict: B uses a new system prompt relative to A (system hash differs).") {
		t.Errorf("expected new system prompt verdict, got:\n%s", out)
	}
}

func TestCmdDiff_SyntheticRecords_Identical(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "System instructions", []map[string]string{
		{"role": "user", "content": "hello"},
	}, []string{"grep"}, 100, 50, 40)

	recB := makeTestRecord("coding", "openai-completions", "System instructions", []map[string]string{
		{"role": "user", "content": "hello"},
	}, []string{"grep"}, 100, 50, 40)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_same.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "first divergence: none (all 1 message(s) identical)") {
		t.Errorf("expected identical divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B has identical message context to A (all messages and system prompt match).") {
		t.Errorf("expected identical verdict, got:\n%s", out)
	}
}

func TestCmdDiff_ToolsAndUsageFormatting(t *testing.T) {
	recA := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "test"},
	}, []string{"read", "write"}, 53182, 629, 44800)

	recB := makeTestRecord("coding", "openai-completions", "sys", []map[string]string{
		{"role": "user", "content": "test"},
	}, []string{"read", "write", "web_search", "memory_write"}, 12004, 991, 9900)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_tools_usage.jsonl", content)

	coordA := fmt.Sprintf("%s:1", auditPath)
	coordB := fmt.Sprintf("%s:2", auditPath)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{coordA, coordB}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	// Check Header alignment and usage
	if !strings.Contains(out, "model        coding               coding               (same)") {
		t.Errorf("expected model row with (same), got:\n%s", out)
	}
	if !strings.Contains(out, "usage        in 53,182 / out 629  in 12,004 / out 991  (cached 44,800 → 9,900)") {
		t.Errorf("expected usage row with cached annotation, got:\n%s", out)
	}
	// Check tools diff
	if !strings.Contains(out, "toolset      2 tools              4 tools              (+2: web_search, memory_write)") {
		t.Errorf("expected tools row with (+2: web_search, memory_write), got:\n%s", out)
	}
	// Check sys_hash (same)
	if !strings.Contains(out, "sys_hash") || !strings.Contains(out, "(same)") {
		t.Errorf("expected sys_hash to be same, got:\n%s", out)
	}
}

func TestCmdDiff_ToolsRemovedAndReplaced(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, []string{"toolA", "toolB"}, 10, 10, 0)
	recB := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, []string{"toolB", "toolC"}, 10, 10, 0)

	content := recordLine(recA) + recordLine(recB)
	auditPath := writeTempFile(t, "audit_tools_mixed.jsonl", content)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{fmt.Sprintf("%s:1", auditPath), fmt.Sprintf("%s:2", auditPath)}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "(+1: toolC, -1: toolA)") {
		t.Errorf("expected (+1: toolC, -1: toolA), got:\n%s", out)
	}
}

// A toolset with identical names but a changed schema (description / parameters)
// is a real cache break the name-only diff misses — the manifest ToolsHash catches it.
func TestCmdDiff_ToolSchemaChangedSameNames(t *testing.T) {
	mkRec := func(desc string) audit.Record {
		rec := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, nil, 10, 10, 0)
		body := rec.Client.Request.Body.(map[string]any)
		body["tools"] = []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": desc,
			},
		}}
		return rec
	}
	content := recordLine(mkRec("search the web")) + recordLine(mkRec("search the web and local files"))
	auditPath := writeTempFile(t, "audit_tool_schema.jsonl", content)

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{fmt.Sprintf("%s:1", auditPath), fmt.Sprintf("%s:2", auditPath)}); err != nil {
			t.Fatalf("cmdDiff failed: %v", err)
		}
	})

	if !strings.Contains(out, "(same names, tool schema changed)") {
		t.Errorf("expected tools row to flag a schema change, got:\n%s", out)
	}
}

func TestCmdDiff_SearchLogDir(t *testing.T) {
	// Write audit log inside a specific log directory
	dir := t.TempDir()
	rec := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, nil, 10, 10, 0)
	content := recordLine(rec)
	auditInLogDir := filepath.Join(dir, "search_audit.jsonl")
	if err := os.WriteFile(auditInLogDir, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgContent := minimalConfigYAML + fmt.Sprintf("log_dir: %s\n", dir)
	cfgPath := writeTempFile(t, "config.yaml", cfgContent)

	out := captureStdout(t, func() {
		err := cmdDiff([]string{"-c", cfgPath, "search_audit.jsonl:1", "search_audit.jsonl:1"})
		if err != nil {
			t.Fatalf("cmdDiff with log_dir resolution failed: %v", err)
		}
	})
	if !strings.Contains(out, "first divergence: none") {
		t.Errorf("expected diff to resolve from log_dir and succeed, got:\n%s", out)
	}
}
