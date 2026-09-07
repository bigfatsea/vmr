// Ver 2026-09-07, by Claude (pi)

package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// viewModelLiteralAllowlist holds string literals in viewmodel builder files
// that look like English prose but are deliberately not i18n'd, each with its
// reason. Everything else matching the prose shape must live in the paired
// internal/i18n file — a builder hard-codes copy means one language silently
// ships to both (D4).
//
// Currently allowed:
//   - internal panic diagnostics (programmer-facing, not user copy);
//   - the mermaid flowchart prefix (a diagram-syntax artifact, not prose).
var viewModelLiteralAllowlist = map[string]string{
	"TableVM: row has %d cells, header has %d": "internal panic: builder bug diagnostic, not rendered copy",
	"RenderMarkdown: unknown block type %T":    "internal panic: serializer bug diagnostic, not rendered copy",
	"```mermaid\\nflowchart LR\\n":             "mermaid diagram syntax, language-independent",
	"```":                                      "mermaid diagram fence, language-independent",
	"\n```":                                    "mermaid diagram fence close, language-independent",
}

// proseLiteral matches a capitalized word followed by more words, or any run
// of three or more space-separated lowercase words — the shape of an English
// sentence. Single words, format placeholders, dates, code identifiers and
// table alignment hints don't match; comments never reach this check because
// the scanner walks the AST, not the raw bytes.
var proseLiteral = regexp.MustCompile(`([A-Z][a-z]+(?:\s+[a-z][a-zA-Z0-9'’,\-.]*)+)|([a-z]{3,}\s+[a-z]{3,}\s+[a-z]{3,})`)

// TestArchitecture_ViewModelNoBareLiterals guards D4 (all copy lives in the
// ViewModel's paired i18n tables, the renderer expresses only structure):
// viewmodel builder files must not carry user-facing English prose as string
// literals. §9 of the analyze architecture redesign lists this guard.
func TestArchitecture_ViewModelNoBareLiterals(t *testing.T) {
	root := repoRootDir(t)
	targets := []string{
		filepath.Join(root, "internal", "report"),
		filepath.Join(root, "internal", "journey"),
	}
	for _, dir := range targets {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "viewmodel") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f := filepath.Join(dir, name)
			for _, lit := range stringLiterals(t, f) {
				if !proseLiteral.MatchString(lit) {
					continue
				}
				if _, ok := viewModelLiteralAllowlist[lit]; ok {
					continue
				}
				t.Errorf("%s carries un-i18n'd prose literal %q — move it to the file's paired internal/i18n text struct (D4), or allowlist it here with a reason if it is not user-facing copy", f, lit)
			}
		}
	}
}

// stringLiterals parses f and returns every interpreted string literal's
// unquoted value. Walking the AST (not the bytes) keeps comments out.
func stringLiterals(t *testing.T, f string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", f, err)
	}
	var lits []string
	ast.Inspect(file, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		v := bl.Value
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			if s, err := strconv.Unquote(v); err == nil {
				lits = append(lits, s)
			}
		}
		return true
	})
	return lits
}
