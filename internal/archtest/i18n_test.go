// Ver 2026-09-15, by gemini-3.7-flash

package archtest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// i18nReportExceptions maps an internal/i18n/report_<name>.go file whose
// report-half counterpart is NOT a viewmodel_<name>.go file onto the file
// it actually pairs with (its own header comment names that counterpart).
// The one-file-per-section convention covers the sections; these three are
// exceptions by design. "doc" is a whole-document builder, not a section;
// "requests" pairs with the requests-side detail files; "toolwaste"'s card
// counterpart was retired with the self-contained HTML renderers (D6) and
// its remaining consumer is the efficiency section's tool-waste block.
var i18nReportExceptions = map[string]string{
	"doc":       "viewmodel_doc.go",
	"requests":  "requests.go",
	"toolwaste": "viewmodel_efficiency.go",
}

// reportI18nPairingProblems compares the viewmodel_*.go module-name set
// with the report_*.go one, one message per drifted pairing in either
// direction. viewmodelNames/i18nNames hold the glob-star module names
// ("cost" for viewmodel_cost.go); reportFiles holds every production
// internal/report/*.go basename, so an exception's claimed counterpart can
// be verified to exist.
func reportI18nPairingProblems(viewmodelNames, i18nNames map[string]bool, reportFiles map[string]bool) []string {
	var problems []string

	names := make([]string, 0, len(viewmodelNames))
	for name := range viewmodelNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !i18nNames[name] {
			problems = append(problems,
				"internal/report/viewmodel_"+name+".go has no internal/i18n/report_"+name+".go counterpart — a view model builder's user-facing strings belong in the paired i18n file, not inline in the builder")
		}
	}

	names = names[:0]
	for name := range i18nNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if viewmodelNames[name] {
			continue
		}
		target, ok := i18nReportExceptions[name]
		if !ok {
			problems = append(problems,
				"internal/i18n/report_"+name+".go has no internal/report/viewmodel_"+name+".go counterpart and is not in i18nReportExceptions — pair it with its view model builder or register the exception")
			continue
		}
		if !reportFiles[target] {
			problems = append(problems,
				"internal/i18n/report_"+name+".go's exception counterpart internal/report/"+target+" does not exist")
		}
	}
	return problems
}

// loadReportI18nSets collects the viewmodel/i18n module-name sets plus
// every production internal/report/*.go basename.
func loadReportI18nSets(t *testing.T, root string) (viewmodelNames, i18nNames, reportFiles map[string]bool) {
	t.Helper()
	viewmodelNames, i18nNames, reportFiles = map[string]bool{}, map[string]bool{}, map[string]bool{}
	module := func(base, prefix string) (string, bool) {
		if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, ".go") {
			return "", false
		}
		return strings.TrimSuffix(strings.TrimPrefix(base, prefix), ".go"), true
	}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "report"))
	if err != nil {
		t.Fatalf("read internal/report: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		reportFiles[e.Name()] = true
		if name, ok := module(e.Name(), "viewmodel_"); ok {
			viewmodelNames[name] = true
		}
	}
	entries, err = os.ReadDir(filepath.Join(root, "internal", "i18n"))
	if err != nil {
		t.Fatalf("read internal/i18n: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if name, ok := module(e.Name(), "report_"); ok {
			i18nNames[name] = true
		}
	}
	return viewmodelNames, i18nNames, reportFiles
}

// TestArchitecture_ReportI18nPairing enforces the one-file-per-section
// pairing the module map states — i18n/report_*.go sits next to
// internal/report/viewmodel_*.go (the ViewModel builders that consume the
// strings, formerly the section_*.go renderers), so a wording change stays
// next to the builder that renders it. The pairing used to live only in
// convention: a new builder could hardcode its strings (or a new i18n file
// could describe a section that doesn't exist) and nothing failed.
// Builders that legitimately pair with a non-builder file are registered
// in i18nReportExceptions, whose targets are verified to exist.
func TestArchitecture_ReportI18nPairing(t *testing.T) {
	viewmodelNames, i18nNames, reportFiles := loadReportI18nSets(t, repoRootDir(t))
	for _, p := range reportI18nPairingProblems(viewmodelNames, i18nNames, reportFiles) {
		t.Error(p)
	}
}

// TestArchitecture_ReportI18nPairing_Negative drives
// reportI18nPairingProblems over synthetic drifted sets to prove the guard
// trips in each direction rather than passing vacuously.
func TestArchitecture_ReportI18nPairing_Negative(t *testing.T) {
	cases := []struct {
		name           string
		viewmodelNames map[string]bool
		i18nNames      map[string]bool
		reportFiles    map[string]bool
		wantFragment   string
	}{
		{
			name:           "builder without i18n counterpart",
			viewmodelNames: map[string]bool{"cost": true},
			i18nNames:      map[string]bool{},
			reportFiles:    map[string]bool{"viewmodel_cost.go": true},
			wantFragment:   "viewmodel_cost.go has no internal/i18n/report_cost.go",
		},
		{
			name:           "i18n file without builder counterpart",
			viewmodelNames: map[string]bool{},
			i18nNames:      map[string]bool{"nosuch": true},
			reportFiles:    map[string]bool{},
			wantFragment:   "report_nosuch.go has no internal/report/viewmodel_nosuch.go",
		},
		{
			name:           "exception whose counterpart vanished",
			viewmodelNames: map[string]bool{},
			i18nNames:      map[string]bool{"doc": true},
			reportFiles:    map[string]bool{},
			wantFragment:   "exception counterpart internal/report/viewmodel_doc.go does not exist",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reportI18nPairingProblems(tc.viewmodelNames, tc.i18nNames, tc.reportFiles)
			if len(got) == 0 {
				t.Fatal("no problems reported, want at least one")
			}
			for _, p := range got {
				if !strings.Contains(p, tc.wantFragment) {
					t.Errorf("problem %q should mention %q", p, tc.wantFragment)
				}
			}
		})
	}

	// The mirror image: fully paired sets, plus a registered exception whose
	// counterpart exists, must stay silent.
	if got := reportI18nPairingProblems(
		map[string]bool{"cost": true},
		map[string]bool{"cost": true, "doc": true},
		map[string]bool{"viewmodel_cost.go": true, "viewmodel_doc.go": true},
	); len(got) != 0 {
		t.Errorf("fully paired sets reported %v, want none", got)
	}
}
