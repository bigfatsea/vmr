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

// sideI18nCounterparts maps every non-report internal/i18n file (the
// journey_* and reqdetail_* ones) onto the production file(s) its header
// comment names as the counterpart it renders for. AGENTS.md's module map
// states this pairing — "i18n/journey_*.go next to internal/journey,
// i18n/reqdetail_detail.go next to internal/reqdetail" — but journey's
// renderers don't share one filename prefix (render_*, viewmodel_* and plain
// *.go files all carry user copy, one i18n file can serve several, and
// journey_compares_index.go renders a cmd/vmr page), so unlike the report
// half the counterpart is stated explicitly per file rather than derived
// from a glob. The test makes "each stated counterpart exists" an enforced
// fact: a renderer renamed, moved or split without updating its i18n anchor
// fails here instead of drifting silently.
var sideI18nCounterparts = map[string][]string{
	"journey_benchmarks.go":     {"internal/journey/render_benchmarks.go"},
	"journey_compare.go":        {"internal/journey/compare.go", "internal/journey/render_compare.go"},
	"journey_compares_index.go": {"cmd/vmr/compares_index.go"},
	"journey_findings.go":       {"internal/journey/findings.go"},
	"journey_index.go":          {"internal/journey/journeyindex.go"},
	"journey_indicators.go":     {"internal/journey/viewmodel_build.go"},
	"journey_llm.go":            {"internal/journey/llm.go"},
	"journey_modelusage.go":     {"internal/journey/viewmodel_build.go"},
	"journey_render.go":         {"internal/journey/journey.go", "internal/journey/viewmodel_build.go", "internal/journey/viewmodel_spine.go"},
	"journey_spine.go":          {"internal/journey/render_spine.go", "internal/journey/viewmodel_spine.go"},
	"reqdetail_detail.go":       {"internal/reqdetail/detail.go"},
}

// sideI18nPairingProblems reports, one message per drift: an i18n file on
// the journey/reqdetail side that is not registered (a new file there must
// state its anchor, or the pairing rule stops covering it), and a registered
// counterpart that no longer exists.
func sideI18nPairingProblems(i18nFiles map[string]bool, counterparts map[string][]string, root string) []string {
	var problems []string

	names := make([]string, 0, len(i18nFiles))
	for name := range i18nFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, c := range counterparts[name] {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(c))); err != nil {
				problems = append(problems,
					"internal/i18n/"+name+"'s counterpart "+c+" does not exist — the renderer was renamed, moved, or split; update the pairing")
			}
		}
		if len(counterparts[name]) == 0 {
			problems = append(problems,
				"internal/i18n/"+name+" is not registered in sideI18nCounterparts — record the file(s) it pairs with (see its header comment) or the pairing drifts silently")
		}
	}
	return problems
}

// loadSideI18nFiles collects the journey_*/reqdetail_* basenames under
// internal/i18n — the files whose pairing sideI18nCounterparts states.
func loadSideI18nFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	files := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "i18n"))
	if err != nil {
		t.Fatalf("read internal/i18n: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.HasPrefix(name, "journey_") || strings.HasPrefix(name, "reqdetail_") {
			files[name] = true
		}
	}
	return files
}

// TestArchitecture_SideI18nPairing enforces the module map's pairing for the
// journey and reqdetail sides: every i18n/journey_*.go sits next to the
// internal/journey file(s) it renders for, and i18n/reqdetail_detail.go next
// to internal/reqdetail/detail.go. Until now that pairing lived only in each
// i18n file's header comment — the exact "documented tripwire nobody sees
// trip" shape this package exists to eliminate.
func TestArchitecture_SideI18nPairing(t *testing.T) {
	root := repoRootDir(t)
	for _, p := range sideI18nPairingProblems(loadSideI18nFiles(t, root), sideI18nCounterparts, root) {
		t.Error(p)
	}
}

// TestArchitecture_SideI18nPairing_Negative drives sideI18nPairingProblems
// over synthetic drifted sets to prove the guard trips in each direction
// rather than passing vacuously.
func TestArchitecture_SideI18nPairing_Negative(t *testing.T) {
	root := repoRootDir(t)
	cases := []struct {
		name         string
		i18nFiles    map[string]bool
		counterparts map[string][]string
		wantFragment string
	}{
		{
			name:         "i18n file without registered counterpart",
			i18nFiles:    map[string]bool{"journey_nosuch.go": true},
			counterparts: map[string][]string{},
			wantFragment: "journey_nosuch.go is not registered in sideI18nCounterparts",
		},
		{
			name:         "counterpart file vanished",
			i18nFiles:    map[string]bool{"reqdetail_detail.go": true},
			counterparts: map[string][]string{"reqdetail_detail.go": {"internal/reqdetail/nope.go"}},
			wantFragment: "'s counterpart internal/reqdetail/nope.go does not exist",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sideI18nPairingProblems(tc.i18nFiles, tc.counterparts, root)
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

	// The mirror image: a registered counterpart that exists must stay silent.
	if got := sideI18nPairingProblems(
		map[string]bool{"reqdetail_detail.go": true},
		map[string][]string{"reqdetail_detail.go": {"internal/reqdetail/detail.go"}},
		root,
	); len(got) != 0 {
		t.Errorf("existing counterpart reported %v, want none", got)
	}
}
