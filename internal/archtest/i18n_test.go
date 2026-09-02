// Ver 2026-09-14, by gemini-3.7-flash

package archtest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// i18nReportExceptions maps an internal/i18n/report_<name>.go file whose
// report-half counterpart is NOT a section_<name>.go file onto the file it
// actually pairs with (its own header comment names that counterpart). The
// one-file-per-section convention covers the sections; these three are
// whole-document renderers, not sections, so they pair with a non-section
// file by design.
var i18nReportExceptions = map[string]string{
	"doc":       "render_doc.go",
	"requests":  "requests.go",
	"toolwaste": "toolwaste_html.go",
}

// reportI18nPairingProblems compares the section_*.go module-name set with
// the report_*.go one, one message per drifted pairing in either direction.
// sectionNames/i18nNames hold the glob-star module names ("cost" for
// section_cost.go); reportFiles holds every production internal/report/*.go
// basename, so an exception's claimed counterpart can be verified to exist.
func reportI18nPairingProblems(sectionNames, i18nNames map[string]bool, reportFiles map[string]bool) []string {
	var problems []string

	names := make([]string, 0, len(sectionNames))
	for name := range sectionNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !i18nNames[name] {
			problems = append(problems,
				"internal/report/section_"+name+".go has no internal/i18n/report_"+name+".go counterpart — a section's user-facing strings belong in the paired i18n file, not inline in the section")
		}
	}

	names = names[:0]
	for name := range i18nNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sectionNames[name] {
			continue
		}
		target, ok := i18nReportExceptions[name]
		if !ok {
			problems = append(problems,
				"internal/i18n/report_"+name+".go has no internal/report/section_"+name+".go counterpart and is not in i18nReportExceptions — pair it with its section or register the exception")
			continue
		}
		if !reportFiles[target] {
			problems = append(problems,
				"internal/i18n/report_"+name+".go's exception counterpart internal/report/"+target+" does not exist")
		}
	}
	return problems
}

// loadReportI18nSets collects the section/i18n module-name sets plus every
// production internal/report/*.go basename.
func loadReportI18nSets(t *testing.T, root string) (sectionNames, i18nNames, reportFiles map[string]bool) {
	t.Helper()
	sectionNames, i18nNames, reportFiles = map[string]bool{}, map[string]bool{}, map[string]bool{}
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
		if name, ok := module(e.Name(), "section_"); ok {
			sectionNames[name] = true
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
	return sectionNames, i18nNames, reportFiles
}

// TestArchitecture_ReportI18nPairing enforces the one-file-per-section
// pairing the module map states — i18n/report_*.go sits next to
// internal/report/section_*.go, so a wording change stays next to the
// section it renders. The pairing used to live only in convention: a new
// section could hardcode its strings (or a new i18n file could describe a
// section that doesn't exist) and nothing failed. Whole-document renderers
// that legitimately pair with a non-section file are registered in
// i18nReportExceptions, whose targets are verified to exist.
func TestArchitecture_ReportI18nPairing(t *testing.T) {
	sectionNames, i18nNames, reportFiles := loadReportI18nSets(t, repoRootDir(t))
	for _, p := range reportI18nPairingProblems(sectionNames, i18nNames, reportFiles) {
		t.Error(p)
	}
}

// TestArchitecture_ReportI18nPairing_Negative drives
// reportI18nPairingProblems over synthetic drifted sets to prove the guard
// trips in each direction rather than passing vacuously.
func TestArchitecture_ReportI18nPairing_Negative(t *testing.T) {
	cases := []struct {
		name         string
		sectionNames map[string]bool
		i18nNames    map[string]bool
		reportFiles  map[string]bool
		wantFragment string
	}{
		{
			name:         "section without i18n counterpart",
			sectionNames: map[string]bool{"cost": true},
			i18nNames:    map[string]bool{},
			reportFiles:  map[string]bool{"section_cost.go": true},
			wantFragment: "section_cost.go has no internal/i18n/report_cost.go",
		},
		{
			name:         "i18n file without section counterpart",
			sectionNames: map[string]bool{},
			i18nNames:    map[string]bool{"nosuch": true},
			reportFiles:  map[string]bool{},
			wantFragment: "report_nosuch.go has no internal/report/section_nosuch.go",
		},
		{
			name:         "exception whose counterpart vanished",
			sectionNames: map[string]bool{},
			i18nNames:    map[string]bool{"doc": true},
			reportFiles:  map[string]bool{},
			wantFragment: "exception counterpart internal/report/render_doc.go does not exist",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reportI18nPairingProblems(tc.sectionNames, tc.i18nNames, tc.reportFiles)
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
		map[string]bool{"section_cost.go": true, "render_doc.go": true},
	); len(got) != 0 {
		t.Errorf("fully paired sets reported %v, want none", got)
	}
}
