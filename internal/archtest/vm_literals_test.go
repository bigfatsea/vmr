// Ver 2026-09-21 21:30, by Sonnet 5

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
// renderer's paired i18n tables, the renderer expresses only structure):
// report's viewmodel builder files and journey's render_*.go files must not
// carry user-facing English prose as string literals. §9 of the analyze
// architecture redesign lists this guard.
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
			// journey's renderers are named render_*.go, not viewmodel_*.go —
			// both prefixes carry user-facing copy and are guarded alike.
			if e.IsDir() || !(strings.HasPrefix(name, "viewmodel") || strings.HasPrefix(name, "render_")) || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
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

// paraVMBudget is the ceiling on ParaVM{...} construction points across
// internal/report's viewmodel_*.go builders. R2 ("单一结构化 VM")
// downgrades ParaVM from "delete it" to "explicit escape hatch": the type
// stays, but new blocks are expected to be
// structured (HeadingVM/NoteVM/ChartVM/FlowVM/TableVM/DetailsVM), so this
// count must only go down, never up by accident. Raising it is allowed —
// same policy as the file/function line budgets in
// func_sizes_test.go/file_sizes_test.go — but it must be a deliberate PR
// decision, not a silent drift. Current value is the real count after the
// Phase 3 (R2-b) migration structured every group heading, blockquote note,
// generative chart/flowchart and the one DetailsVM-bypass out of ParaVM
// (56 → 43); the 43 remaining are the genuinely unstructured cases
// (plain i18n-authored prose paragraphs, plus a handful with builder-owned
// manual whitespace).
const paraVMBudget = 43

// TestParaVMBudget guards R2-a: new report VM blocks should be structured
// rather than another ParaVM literal.
// Reuses stringLiterals' file-selection logic (viewmodel_*.go, excluding
// _test.go) but counts ParaVM{...} composite literals instead of string
// literals.
func TestParaVMBudget(t *testing.T) {
	root := repoRootDir(t)
	dir := filepath.Join(root, "internal", "report")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	total := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "viewmodel") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		total += countParaVMLiterals(t, filepath.Join(dir, name))
	}
	if total > paraVMBudget {
		t.Errorf("internal/report/viewmodel_*.go carries %d ParaVM{...} construction points, budget is %d — new blocks should use a structured VM (TableVM/DetailsVM/a narrower type); if this one genuinely can't be structured, raise paraVMBudget with a reason", total, paraVMBudget)
	}
}

// countParaVMLiterals parses f and counts ParaVM{...} composite literals —
// an AST walk rather than a text grep so a ParaVM-shaped string inside a
// comment or another literal never inflates the count.
func countParaVMLiterals(t *testing.T, f string) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", f, err)
	}
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id, ok := cl.Type.(*ast.Ident); ok && id.Name == "ParaVM" {
			count++
		}
		return true
	})
	return count
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
