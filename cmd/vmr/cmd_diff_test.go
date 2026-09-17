// Ver 2026-09-13, by Sonnet 5
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

// makeTestRecord and recordLine only remain here for TestCmdDiff_SearchLogDir,
// which needs a real on-disk audit line to exercise log_dir resolution; the
// algorithm-level tests that used to build synthetic records here now live in
// internal/auditdiff (Compute/Render's own unit tests), calling the domain
// package directly instead of round-tripping through this CLI wrapper.
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
