// Ver 2026-09-22 18:50, by Sonnet 5

package archtest

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
)

var (
	sourceCacheMu sync.RWMutex
	sourceCache   = map[string][][]byte{}
)

// cleanSource replaces all comment bytes with spaces while preserving newlines.
// A line in the resulting slice has non-whitespace characters if and only if
// it contains Go code outside comments.
func cleanSource(src []byte) [][]byte {
	fset := token.NewFileSet()
	f := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(f, src, func(pos token.Position, msg string) {}, scanner.ScanComments)

	clean := make([]byte, len(src))
	copy(clean, src)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			offset := f.Offset(pos)
			for i := 0; i < len(lit); i++ {
				idx := offset + i
				if idx < len(clean) && clean[idx] != '\n' {
					clean[idx] = ' '
				}
			}
		}
	}
	return bytes.Split(clean, []byte("\n"))
}

// codeLines counts net code lines in src: empty lines, pure // comment lines,
// and lines inside /* ... */ block comments are skipped. Lines containing both
// code and comments are counted as code lines.
func codeLines(src []byte) int {
	lines := cleanSource(src)
	count := 0
	for _, l := range lines {
		if len(bytes.TrimSpace(l)) > 0 {
			count++
		}
	}
	return count
}

func getOrCleanLines(filename string, file *ast.File) [][]byte {
	if filename != "" {
		sourceCacheMu.RLock()
		lines, ok := sourceCache[filename]
		sourceCacheMu.RUnlock()
		if ok {
			return lines
		}
	}

	if filename == "" {
		return nil
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil
	}

	lines := cleanSource(data)
	sourceCacheMu.Lock()
	sourceCache[filename] = lines
	sourceCacheMu.Unlock()
	return lines
}

// codeLinesInRange counts net code lines in file within the range [from, to] (inclusive).
func codeLinesInRange(fset *token.FileSet, file *ast.File, from, to token.Pos) int {
	startLine := fset.Position(from).Line
	endLine := fset.Position(to).Line
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}

	filename := fset.Position(from).Filename
	if filename == "" && file != nil {
		filename = fset.Position(file.Pos()).Filename
	}

	lines := getOrCleanLines(filename, file)
	if lines == nil {
		return 0
	}

	count := 0
	for line := startLine; line <= endLine && line <= len(lines); line++ {
		if line >= 1 && len(bytes.TrimSpace(lines[line-1])) > 0 {
			count++
		}
	}
	return count
}

func setTestSource(filename string, src []byte) {
	sourceCacheMu.Lock()
	defer sourceCacheMu.Unlock()
	sourceCache[filename] = cleanSource(src)
}

func clearTestSource(filename string) {
	sourceCacheMu.Lock()
	defer sourceCacheMu.Unlock()
	delete(sourceCache, filename)
}

func TestArchitecture_CodeLines(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "empty source",
			src:  "",
			want: 0,
		},
		{
			name: "only blank lines",
			src:  "   \n\t\n\n   \r\n",
			want: 0,
		},
		{
			name: "pure single line comments",
			src:  "// line 1\n// line 2\n   // line 3\n",
			want: 0,
		},
		{
			name: "pure block comments",
			src:  "/* line 1\n * line 2\n */\n",
			want: 0,
		},
		{
			name: "code with inline comments",
			src:  "package main\n\nvar x = 1 // inline comment\nvar y = 2 /* block inline */\n",
			want: 3,
		},
		{
			name: "block comment starting on code line",
			src:  "var x = 1 /* start block\n   inside comment\n   end block */\n",
			want: 1,
		},
		{
			name: "block comment ending on code line",
			src:  "/* start block\n   inside comment\n   end block */ var x = 1\n",
			want: 1,
		},
		{
			name: "strings containing comment delimiters",
			src:  "package main\n\nvar s = \"// not a comment\"\nvar b = `/* not a block comment */`\n",
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codeLines([]byte(tt.src))
			if got != tt.want {
				t.Errorf("codeLines() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestArchitecture_CodeLinesInRange(t *testing.T) {
	const virtualFile = "test_virtual_file.go"
	baseSrc := `package main

func targetFunc(a int) int {
	// Step 1: initialize
	x := a + 1

	// Step 2: calculate
	/* block comment
	   line 2 */
	y := x * 2 // line comment

	return y
}
`
	setTestSource(virtualFile, []byte(baseSrc))
	defer clearTestSource(virtualFile)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, virtualFile, []byte(baseSrc), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var targetDecl *ast.FuncDecl
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "targetFunc" {
			targetDecl = fd
			break
		}
	}
	if targetDecl == nil {
		t.Fatal("targetFunc not found")
	}

	baseNet := codeLinesInRange(fset, f, targetDecl.Body.Pos(), targetDecl.Body.End())
	// Line 3: func targetFunc(a int) int {
	// Line 5: 	x := a + 1
	// Line 10: 	y := x * 2
	// Line 12: 	return y
	// Line 13: }
	// Total: 5 net code lines.
	if baseNet != 5 {
		t.Fatalf("expected 5 net code lines for base targetFunc, got %d", baseNet)
	}

	// Now add 30 pure comment lines inside the function body:
	srcWith30Comments := strings.Replace(
		baseSrc,
		"\t// Step 1: initialize",
		strings.Repeat("\t// Added comment\n", 30)+"\t// Step 1: initialize",
		1,
	)
	setTestSource(virtualFile, []byte(srcWith30Comments))
	fset2 := token.NewFileSet()
	f2, err := parser.ParseFile(fset2, virtualFile, []byte(srcWith30Comments), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var targetDecl2 *ast.FuncDecl
	for _, decl := range f2.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "targetFunc" {
			targetDecl2 = fd
			break
		}
	}
	if targetDecl2 == nil {
		t.Fatal("targetFunc not found in modified src")
	}

	modifiedNet := codeLinesInRange(fset2, f2, targetDecl2.Body.Pos(), targetDecl2.Body.End())
	if modifiedNet != baseNet {
		t.Errorf("adding 30 pure comment lines changed net lines from %d to %d", baseNet, modifiedNet)
	}
}
