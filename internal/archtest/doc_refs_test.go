// Ver 2026-08-15 14:30, by gemini-3.7-flash

package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docWorld is everything the reference checks are judged against: which
// internal packages exist, and which exported identifiers each one declares.
// Extracting it (and checkDocRefs below) from the test body is what lets the
// negative test drive the real checking logic over synthetic bad input
// instead of asserting a tautology about the real tree — a guard whose
// failure path is never exercised is exactly the "tripwire nobody sees trip"
// this package exists to eliminate.
type docWorld struct {
	root string
	// pkgPaths holds both "internal/x" and "vmr/internal/x" spellings.
	pkgPaths map[string]bool
	// symbols maps a package's short name to its exported identifiers.
	symbols map[string]map[string]bool
}

var (
	// A bare docs/... path mentioned anywhere in prose.
	reDocPath = regexp.MustCompile(`\bdocs/[a-zA-Z0-9_\-./*]+\.md\b`)
	// A markdown link whose destination is a local .md file.
	reMarkdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)#\s]+\.md)(?:#[^)]*)?\)`)
	// An internal package or source-file path.
	reInternalRef = regexp.MustCompile(`(?:vmr/)?internal/([a-zA-Z0-9_]+(?:/[a-zA-Z0-9_\-.*]+)*)`)
	// A backticked Go identifier: `pkg.Symbol`, `pkg.Symbol(args)`,
	// `pkg.Symbol(args).Field`, `pkg.Type.Field`. Anchored on the opening
	// backtick and allowed to run to the closing one, so a trailing call or
	// selector chain no longer lets a reference escape the check — the two
	// drifts this guard was written for (`i18n.StoryLLM(lang).SystemPrompt`,
	// `adapter.TopLevelProbeResult.Model`) both had exactly that shape. Only
	// the first two segments are validated; a field/method on a resolved
	// type is not something a package-level symbol table can confirm.
	reSymbol = regexp.MustCompile("`([a-z][a-zA-Z0-9]*)\\.([A-Z][a-zA-Z0-9_]*)[^`]*`")
)

// docsWithInternalPaths are the docs whose internal/... references must
// resolve. Review reports and strategy material are excluded on purpose:
// they discuss historical and hypothetical package names as part of their
// argument, and rewriting history to satisfy a guard is the wrong trade.
// Production .go source files (docRel ending in ".go") are held to this
// same standard unconditionally — a source comment citing a moved/deleted
// file or doc is exactly the class of drift this guard exists to catch
// (see TestArchitecture_DocReferences_SourceComments), and unlike a dated
// review report a source comment claims to describe the code as it is now.
func docHasInternalPaths(docRel string) bool {
	return strings.HasSuffix(docRel, ".go") ||
		docRel == "CLAUDE.md" ||
		strings.HasPrefix(docRel, "docs/VirtualModelRouter_Design_v4_") ||
		docRel == "docs/KNOWN_ISSUES.md"
}

// docHasSymbols marks the docs that describe current state and therefore
// must not name a Go symbol that does not exist. Deliberately NOT extended
// to .go source files the way docHasInternalPaths is: Go doc comments
// backtick-quote all sorts of things (parameter names, stdlib types) in a
// `pkg.Symbol`-like shape that would make repo-wide symbol checking noisy
// far beyond what this task set out to guard — path references only.
func docHasSymbols(docRel string) bool {
	return docRel == "CLAUDE.md" ||
		strings.HasPrefix(docRel, "docs/VirtualModelRouter_Design_v4_") ||
		docRel == "docs/KNOWN_ISSUES.md" ||
		strings.HasPrefix(docRel, "README") ||
		strings.HasPrefix(docRel, "docs/UserGuide")
}

// docHasMarkdownLinks marks the docs whose `[text](target.md)` syntax means
// what it means in a normal Markdown document: a same-repo cross-reference
// that must resolve. .go source files are excluded — a `[label](x.md)`
// string inside a Go source file is, in this codebase, i18n/render text
// being assembled into a *generated* report (e.g. i18n/report_doc.go's
// "[vmr-requests.md](./vmr-requests.md)", which names an output artifact
// that will exist next to the rendered file at runtime, and
// report/render_doc.go's `[-%s](./vmr-requests-%s.md)`, a printf template
// with no meaning as a repo path at all) — not a claim that some path
// exists in this checkout right now.
func docHasMarkdownLinks(docRel string) bool {
	return !strings.HasSuffix(docRel, ".go")
}

// checkDocRefs returns one message per broken reference found in content.
// docRel is used both for the message prefix and to resolve relative
// markdown links; docDir is the directory content was read from.
func checkDocRefs(w docWorld, docRel, content string) []string {
	var problems []string

	docDir := filepath.Dir(filepath.Join(w.root, docRel))
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	for _, raw := range reDocPath.FindAllString(content, -1) {
		if strings.Contains(raw, "*") {
			if m, _ := filepath.Glob(filepath.Join(w.root, raw)); len(m) == 0 {
				problems = append(problems, docRel+": doc pattern "+raw+" matches no files")
			}
			continue
		}
		if !exists(filepath.Join(w.root, raw)) {
			problems = append(problems, docRel+": doc path "+raw+" does not exist")
		}
	}

	if docHasMarkdownLinks(docRel) {
		for _, m := range reMarkdownLink.FindAllStringSubmatch(content, -1) {
			target := m[1]
			// http(s) is someone else's uptime problem; file:// carries a
			// machine-specific absolute path that says nothing about this
			// checkout.
			if strings.Contains(target, "://") {
				continue
			}
			resolved := filepath.Join(docDir, target)
			if filepath.IsAbs(target) {
				resolved = target
			}
			if !exists(filepath.Clean(resolved)) {
				problems = append(problems, docRel+": link target "+target+" does not exist")
			}
		}
	}

	if docHasInternalPaths(docRel) {
		for _, m := range reInternalRef.FindAllStringSubmatch(content, -1) {
			sub := strings.TrimRight(m[1], ".,;:()")
			if sub == "" {
				continue
			}
			if strings.HasSuffix(sub, ".go") {
				// A glob (internal/report/section_*.go) is how the docs
				// name a file convention rather than one file; it holds as
				// long as something still matches it.
				if strings.Contains(sub, "*") {
					if m, _ := filepath.Glob(filepath.Join(w.root, "internal", sub)); len(m) == 0 {
						problems = append(problems, docRel+": source pattern internal/"+sub+" matches no files")
					}
					continue
				}
				if !exists(filepath.Join(w.root, "internal", sub)) {
					problems = append(problems, docRel+": source file internal/"+sub+" does not exist")
				}
				continue
			}
			if strings.Contains(sub, "*") {
				continue // a package-level glob is not a path claim
			}
			if w.pkgPaths["internal/"+sub] || w.pkgPaths["vmr/internal/"+sub] {
				continue
			}
			// A directory holding only subpackages (internal/adapter's
			// per-protocol dirs are referenced that way) is still a valid
			// thing to point at.
			if info, err := os.Stat(filepath.Join(w.root, "internal", sub)); err == nil && info.IsDir() {
				continue
			}
			problems = append(problems, docRel+": internal/"+sub+" is not a valid package")
		}
	}

	if docHasSymbols(docRel) {
		for _, m := range reSymbol.FindAllStringSubmatch(content, -1) {
			pkg, sym := m[1], m[2]
			// Only packages this repo owns are checkable; `time.Duration`
			// and friends are deliberately out of scope.
			syms, ours := w.symbols[pkg]
			if ours && !syms[sym] {
				problems = append(problems, docRel+": symbol "+pkg+"."+sym+" does not exist")
			}
		}
	}
	return problems
}

// loadDocWorld enumerates internal packages and their exported identifiers.
func loadDocWorld(t *testing.T, root string) docWorld {
	t.Helper()
	w := docWorld{root: root, pkgPaths: map[string]bool{}, symbols: map[string]map[string]bool{}}

	cmd := exec.Command("go", "list", "./internal/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./internal/...: %v", err)
	}
	for _, line := range strings.Fields(string(out)) {
		w.pkgPaths[line] = true
		w.pkgPaths[strings.TrimPrefix(line, "vmr/")] = true
	}

	fset := token.NewFileSet()
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		node, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		syms, ok := w.symbols[node.Name.Name]
		if !ok {
			syms = map[string]bool{}
			w.symbols[node.Name.Name] = syms
		}
		for _, decl := range node.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							syms[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								syms[name.Name] = true
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					syms[d.Name.Name] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan internal/ for exported symbols: %v", err)
	}
	return w
}

// TestArchitecture_DocReferences guards against documentation drift: a doc
// that names a file, package, or exported symbol that does not exist reads
// as authoritative and misleads every later reader. Two such drifts
// (`docs/VirtualModelRouter_Design_v4_Strategy.md` and `fmtutil.FmtTokens`,
// both referenced long before they existed) were only ever caught by hand.
func TestArchitecture_DocReferences(t *testing.T) {
	root := repoRootDir(t)
	w := loadDocWorld(t, root)

	// Top-level docs/ only, deliberately not docs/future-strategy/: those
	// are dated point-in-time strategy notes and review reports whose whole
	// value is recording what was true when they were written — several
	// legitimately discuss files that have since been deleted. Holding them
	// to current-state accuracy would either produce permanent noise or
	// pressure someone into editing history to silence a test.
	docs := []string{"CLAUDE.md", "README.md", "README.zh.md"}
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatalf("read docs/: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			docs = append(docs, "docs/"+e.Name())
		}
	}

	for _, docRel := range docs {
		content, err := os.ReadFile(filepath.Join(root, docRel))
		if err != nil {
			t.Errorf("read %s: %v", docRel, err)
			continue
		}
		for _, p := range checkDocRefs(w, docRel, string(content)) {
			t.Error(p)
		}
	}
}

// goFileComments extracts only the comment text of a Go source file — not
// string literals, not identifiers, not import paths — via go/parser's own
// AST rather than a full-text regex scan. This matters for a file like
// internal/i18n/report_doc.go: its *string literals* legitimately contain
// `[vmr-requests.md](./vmr-requests.md)`-shaped text (rendered into
// generated reports), and a full-text scan has no way to distinguish that
// from a comment's documentation claim without a growing pile of
// docHasMarkdownLinks-style exceptions. Restricting the input to comments
// makes the distinction structural instead of enumerated.
func goFileComments(path string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, cg := range node.Comments {
		b.WriteString(cg.Text())
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// TestArchitecture_DocReferences_SourceComments extends the same guard to
// production .go source comments under internal/ and cmd/ — the class of
// drift TestArchitecture_DocReferences never saw: a comment citing a file
// that has since moved (internal/report/render.go, moved to
// internal/reqdetail/render.go in P2) or a docs/future-strategy/*.md plan
// document that has since been archived. Both classes were found live in
// this tree during the 2026-08-21 review (story_report_full_review_opus-5.md
// §2.6/§4.1) and went unnoticed for two phases specifically because nothing
// checked source comments — see that review's account of the same dead
// reference tripping this package's own doc-reference guard the moment it
// was typed into KNOWN_ISSUES, while the identical dead reference sat
// unnoticed in three .go files' comments the whole time.
//
// _test.go files are excluded (same convention loadDocWorld already uses
// for symbol extraction) and so is _eval/ (a standalone calibration tool
// with its own compile guard — TestArchitecture_EvalToolsCompile — rather
// than being folded into the internal/+cmd/ production surface this test
// walks).
func TestArchitecture_DocReferences_SourceComments(t *testing.T) {
	root := repoRootDir(t)
	w := loadDocWorld(t, root)

	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			comments, rerr := goFileComments(path)
			if rerr != nil {
				return rerr
			}
			docRel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			for _, p := range checkDocRefs(w, docRel, comments) {
				t.Error(p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s for source comment references: %v", top, err)
		}
	}
}

// TestArchitecture_DocReferences_Negative drives checkDocRefs over synthetic
// broken references to prove the guard actually trips. Without this, a regex
// that silently stops matching would leave TestArchitecture_DocReferences
// green forever while checking nothing.
func TestArchitecture_DocReferences_Negative(t *testing.T) {
	w := loadDocWorld(t, repoRootDir(t))

	cases := []struct {
		name    string
		docRel  string
		content string
	}{
		{"missing doc path", "CLAUDE.md", "see docs/nosuchdoc_xyz.md for details"},
		{"missing link target", "CLAUDE.md", "see [guide](docs/nosuchdoc_xyz.md)"},
		{"missing package", "CLAUDE.md", "routing lives in `internal/nosuchpkg`"},
		{"missing source file", "CLAUDE.md", "see internal/router/nosuchfile.go"},
		{"source pattern matching nothing", "CLAUDE.md", "one file per internal/report/nosuch_*.go"},
		{"missing symbol", "CLAUDE.md", "call `core.NoSuchSymbolX` first"},
		// The two shapes that escaped the original regex.
		{"missing symbol behind a call chain", "CLAUDE.md", "`i18n.NoSuchTextX(lang).Field`"},
		{"missing symbol behind a selector", "CLAUDE.md", "`adapter.NoSuchTypeX.Model`"},
		{"missing symbol in a design doc", "docs/VirtualModelRouter_Design_v4_Core.md", "`router.NoSuchFuncX`"},
		{"missing symbol in a README", "README.md", "`report.NoSuchRowX`"},
		// The .go-source-comment branch: same checkDocRefs, a source file
		// docRel instead of a doc's.
		{"missing source file in a .go comment", "internal/report/detail.go", "see internal/report/nosuchfile.go"},
		{"missing future-strategy doc in a .go comment", "internal/report/detail.go", "see docs/future-strategy/nosuch_xyz.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkDocRefs(w, tc.docRel, tc.content); len(got) == 0 {
				t.Errorf("checkDocRefs(%q) found no problem, want at least one", tc.content)
			}
		})
	}

	// And the mirror image: a reference that does resolve must stay silent,
	// or the guard would be unusable noise rather than a signal.
	ok := []struct{ docRel, content string }{
		{"CLAUDE.md", "response normalization lives in `internal/respnorm`"},
		{"CLAUDE.md", "`tokenutil.Estimate` shares its coefficients"},
		{"CLAUDE.md", "`i18n.LLM(lang).SystemPrompt` is the prompt"},
		{"CLAUDE.md", "see internal/router/router.go and docs/UserGuide.md"},
		{"CLAUDE.md", "one section per internal/report/section_*.go"},
		{"CLAUDE.md", "`time.Duration` and `json.RawMessage` are out of scope"},
		{"internal/report/detail.go", "see docs/future-strategy/story_report_architecture_opus-5.md"},
		{"internal/report/detail.go", "moved to internal/reqdetail/render.go"},
		// A .go source comment is NOT held to docHasSymbols (too noisy —
		// see docHasSymbols' doc comment), so a bogus `pkg.Symbol` mention
		// in one must stay silent even though the same text would trip in
		// CLAUDE.md/a README above.
		{"internal/report/detail.go", "`core.NoSuchSymbolX` is unrelated prose"},
		// Markdown-link syntax in a .go file is i18n/render text describing
		// a *generated* artifact's own links, not a repo cross-reference —
		// see docHasMarkdownLinks. This must stay silent even though the
		// identical text in a real .md doc would trip above.
		{"internal/i18n/report_doc.go", `"see [vmr-requests.md](./vmr-requests.md)"`},
	}
	for _, tc := range ok {
		if got := checkDocRefs(w, tc.docRel, tc.content); len(got) != 0 {
			t.Errorf("checkDocRefs(%q) = %v, want no problems", tc.content, got)
		}
	}
}
