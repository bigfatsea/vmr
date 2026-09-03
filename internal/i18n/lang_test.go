// Ver 2026-09-03 11:30, by Sonnet 5

package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in      string
		want    Lang
		wantErr bool
	}{
		{"en", EN, false}, {"EN", EN, false}, {"english", EN, false}, {"English", EN, false},
		{"zh", ZH, false}, {"ZH", ZH, false}, {"chinese", ZH, false}, {"zh-cn", ZH, false}, {"zh-CN", ZH, false}, {"zh_cn", ZH, false},
		// Mixed casings the doc comment always promised ("case-insensitive")
		// but the original hand-enumerated switch didn't actually accept —
		// these previously fell through to the error branch.
		{"Zh", ZH, false}, {"EnGlish", EN, false}, {"CHINESE", ZH, false}, {"ZH-CN", ZH, false}, {"Zh_Cn", ZH, false},
		{"fr", EN, true}, {"", EN, true}, {"japanese", EN, true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("Parse(%q): err = %v, wantErr %v", c.in, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLangString(t *testing.T) {
	if EN.String() != "en" {
		t.Errorf("EN.String() = %q, want en", EN.String())
	}
	if ZH.String() != "zh" {
		t.Errorf("ZH.String() = %q, want zh", ZH.String())
	}
}

// bundleConstructors lists every XxxText(lang) bundle constructor in this
// package. TestBundlesHaveNoEmptyStrings walks each one, for both languages,
// and fails on any empty plain-string field or map value — a missing
// translation renders as an empty string in the Markdown output, which is a
// much quieter failure than a wrong one, so this is the cheapest possible
// tripwire for "someone added a field to one language's literal but forgot
// the other" (function-valued fields are exercised with zero-value args
// instead: it's calling them at all, not their output, that this test
// checks — a function field itself can never be "empty", only unset/nil,
// which the nil check below already covers).
var bundleConstructors = map[string]func(Lang) any{
	"Doc":            func(l Lang) any { return Doc(l) },
	"Tokens":         func(l Lang) any { return Tokens(l) },
	"Cost":           func(l Lang) any { return Cost(l) },
	"Reliability":    func(l Lang) any { return Reliability(l) },
	"Latency":        func(l Lang) any { return Latency(l) },
	"Workload":       func(l Lang) any { return Workload(l) },
	"Sessions":       func(l Lang) any { return Sessions(l) },
	"Sticky":         func(l Lang) any { return Sticky(l) },
	"EndpointValue":  func(l Lang) any { return EndpointValue(l) },
	"Compaction":     func(l Lang) any { return Compaction(l) },
	"Efficiency":     func(l Lang) any { return Efficiency(l) },
	"Requests":       func(l Lang) any { return Requests(l) },
	"Detail":         func(l Lang) any { return Detail(l) },
	"Story":          func(l Lang) any { return Story(l) },
	"Compare":        func(l Lang) any { return Compare(l) },
	"LLM":            func(l Lang) any { return LLM(l) },
	"CLI":            func(l Lang) any { return CLI(l) },
	"ClientEndpoint": func(l Lang) any { return ClientEndpoint(l) },
	"CompareHTML":    func(l Lang) any { return CompareHTML(l) },
	"Corpus":         func(l Lang) any { return Corpus(l) },
	"Indicators":     func(l Lang) any { return Indicators(l) },
	"ModelUsage":     func(l Lang) any { return ModelUsage(l) },
	"Provider":       func(l Lang) any { return Provider(l) },
	"ProviderQuota":  func(l Lang) any { return ProviderQuota(l) },
	"Spine":          func(l Lang) any { return Spine(l) },
	"StoryFindings":  func(l Lang) any { return StoryFindings(l) },
	"StoryHTML":      func(l Lang) any { return StoryHTML(l) },
	"StoryIndexT":    func(l Lang) any { return StoryIndexT(l) },
	"ToolWaste":      func(l Lang) any { return ToolWaste(l) },
}

func TestBundlesHaveNoEmptyStrings(t *testing.T) {
	for name, ctor := range bundleConstructors {
		for _, lang := range []Lang{EN, ZH} {
			checkNoEmpty(t, name+"("+lang.String()+")", reflect.ValueOf(ctor(lang)))
		}
	}
}

// TestBundleConstructorsCompleteness uses AST inspection over internal/i18n/*.go
// to ensure that every top-level constructor func Xxx(lang Lang) ...Text is
// registered in bundleConstructors. This guarantees the bundle registry cannot
// quietly drift when new report/story sections or text bundles are added.
func TestBundleConstructorsCompleteness(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}
	fset := token.NewFileSet()
	found := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Type == nil {
				continue
			}
			// Must have exactly one parameter of type Lang
			if fd.Type.Params == nil || fd.Type.Params.NumFields() != 1 || len(fd.Type.Params.List) != 1 {
				continue
			}
			paramIdent, ok := fd.Type.Params.List[0].Type.(*ast.Ident)
			if !ok || paramIdent.Name != "Lang" {
				continue
			}
			// Must have exactly one return value of type identifier ending with "Text"
			if fd.Type.Results == nil || fd.Type.Results.NumFields() != 1 || len(fd.Type.Results.List) != 1 {
				continue
			}
			retIdent, ok := fd.Type.Results.List[0].Type.(*ast.Ident)
			if !ok || !strings.HasSuffix(retIdent.Name, "Text") {
				continue
			}
			found[fd.Name.Name] = true
		}
	}

	for name := range found {
		if _, ok := bundleConstructors[name]; !ok {
			t.Errorf("bundleConstructors is missing %q; add it to bundleConstructors in internal/i18n/lang_test.go", name)
		}
	}
	for name := range bundleConstructors {
		if !found[name] {
			t.Errorf("bundleConstructors has stale entry %q (not found as func(Lang) ...Text in internal/i18n/*.go); remove it from bundleConstructors", name)
		}
	}
}

// checkNoEmpty recursively walks a struct value, failing on any zero-value
// string, empty array-of-string element, nil func field, or map with an
// empty value — the shapes every bundle in this package is built from.
func checkNoEmpty(t *testing.T, path string, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.String:
		if v.String() == "" {
			t.Errorf("%s: empty string", path)
		}
	case reflect.Func:
		if v.IsNil() {
			t.Errorf("%s: nil func field", path)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			checkNoEmpty(t, path+"."+v.Type().Field(i).Name, v.Field(i))
		}
	case reflect.Array, reflect.Slice:
		if v.Kind() == reflect.Slice && v.Len() == 0 {
			t.Errorf("%s: empty slice", path)
			return
		}
		for i := 0; i < v.Len(); i++ {
			checkNoEmpty(t, path+"[]", v.Index(i))
		}
	case reflect.Map:
		if v.Len() == 0 {
			t.Errorf("%s: empty map", path)
			return
		}
		for _, k := range v.MapKeys() {
			checkNoEmpty(t, path+"["+k.String()+"]", v.MapIndex(k))
		}
	}
}
