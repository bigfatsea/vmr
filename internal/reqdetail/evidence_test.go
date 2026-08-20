// Ver 2026-08-20 00:00, by Sonnet 5

package reqdetail

import (
	"crypto/md5"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

func recWithSysAndTools(sys string, toolNames ...string) *audit.Record {
	var tools []any
	for _, n := range toolNames {
		tools = append(tools, map[string]any{"name": n, "description": "does " + n})
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	if sys != "" {
		body["messages"] = []any{
			map[string]any{"role": "system", "content": sys},
			map[string]any{"role": "user", "content": "hi"},
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	return &audit.Record{Client: audit.Exchange{Request: audit.Message{Body: body}}}
}

func TestEnsureSysPromptEvidence_WritesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	rec1 := recWithSysAndTools("you are a helpful assistant")
	rec2 := recWithSysAndTools("you are a helpful assistant") // same text, different record

	name1, err := EnsureSysPromptEvidence(dir, rec1)
	if err != nil {
		t.Fatal(err)
	}
	if name1 == "" {
		t.Fatal("expected a non-empty filename for a record with a system prompt")
	}
	fi1, err := os.Stat(filepath.Join(dir, name1))
	if err != nil {
		t.Fatal(err)
	}

	name2, err := EnsureSysPromptEvidence(dir, rec2)
	if err != nil {
		t.Fatal(err)
	}
	if name2 != name1 {
		t.Errorf("two records with identical system prompts got different evidence files: %q vs %q", name1, name2)
	}
	fi2, err := os.Stat(filepath.Join(dir, name2))
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("second call re-wrote the file instead of deduping against the first")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want exactly 1 (deduped): %v", len(entries), entries)
	}
}

func TestEnsureSysPromptEvidence_NoSystemMessage(t *testing.T) {
	dir := t.TempDir()
	rec := recWithSysAndTools("")
	name, err := EnsureSysPromptEvidence(dir, rec)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Errorf("expected empty filename for a record with no system message, got %q", name)
	}
}

func TestEnsureSysPromptEvidence_ContentMatchesFilenameHash(t *testing.T) {
	dir := t.TempDir()
	rec := recWithSysAndTools("distinctive system prompt text")
	name, err := EnsureSysPromptEvidence(dir, rec)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "distinctive system prompt text") {
		t.Errorf("evidence file content missing the system prompt text: %q", data)
	}
	wantName := "sysprompt-" + contentHash8("distinctive system prompt text") + ".md"
	if name != wantName {
		t.Errorf("filename = %q, want %q", name, wantName)
	}
}

// TestSysPromptEvidenceFileName_MatchesEnsureSysPromptEvidence locks down
// the contract SysPromptEvidenceFileName exists for: a caller holding only a
// Manifest's SysHash (no rec) — a story spine Step's future "→ system
// prompt" link — must be able to compute the exact filename
// EnsureSysPromptEvidence actually wrote, without re-deriving this
// package's private hash/naming convention.
func TestSysPromptEvidenceFileName_MatchesEnsureSysPromptEvidence(t *testing.T) {
	dir := t.TempDir()
	rec := recWithSysAndTools("distinctive system prompt text")
	written, err := EnsureSysPromptEvidence(dir, rec)
	if err != nil {
		t.Fatal(err)
	}
	sysHash := ctxgraph.Hash(md5.Sum([]byte("distinctive system prompt text")))
	got := SysPromptEvidenceFileName(sysHash)
	if got != written {
		t.Errorf("SysPromptEvidenceFileName(sysHash) = %q, want %q (what EnsureSysPromptEvidence actually wrote)", got, written)
	}
}

func TestEnsureToolsEvidence_WritesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	rec1 := recWithSysAndTools("", "exec", "write")
	rec2 := recWithSysAndTools("", "exec", "write")

	name1, err := EnsureToolsEvidence(dir, rec1)
	if err != nil {
		t.Fatal(err)
	}
	if name1 == "" {
		t.Fatal("expected a non-empty filename for a record declaring tools")
	}
	name2, err := EnsureToolsEvidence(dir, rec2)
	if err != nil {
		t.Fatal(err)
	}
	if name2 != name1 {
		t.Errorf("two records with the same tool set got different evidence files: %q vs %q", name1, name2)
	}
}

func TestEnsureToolsEvidence_NoTools(t *testing.T) {
	dir := t.TempDir()
	rec := recWithSysAndTools("")
	name, err := EnsureToolsEvidence(dir, rec)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Errorf("expected empty filename for a record with no declared tools, got %q", name)
	}
}
