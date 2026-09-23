// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/dashboard"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/pricing"
	"vmr/internal/report"
	"vmr/internal/taskseg"
)

// LLMOptions bundles the -llm-* options after validation — wraps journey.LLMOptions
// and includes DryRun.
type LLMOptions struct {
	journey.LLMOptions
	DryRun bool
}

// Run holds already-resolved configuration and options for executing an analyze run.
type Run struct {
	Paths                   []string
	OutDir                  string
	Lang                    i18n.Lang
	IncludePartial          bool
	IncludeSelfTraffic      bool
	SelfTrafficTags         []string
	LLMSelfTag              string
	ExcludeClientTags       map[string]bool
	PriceRes                *pricing.Resolver
	PricingInfo             *report.Pricing
	PricingFingerprint      []byte
	Currency                string
	DisplayCCY              string
	ExchangeRate            map[string]float64
	Quotas                  map[string][]report.ProviderQuotaRef
	QuotaJSONPath           string
	QuotaInputOutsideLogDir bool
	ReportConfigSource      string
	Profile                 taskseg.Profile
	DetailsOn               bool
	ShowUngrouped           bool
	NoCache                 bool

	BenchmarkFlag bool
	CompareArg    string
	JourneyArg    string
	RenderAllFlag bool
	MacroOnly     bool
	ListOnly      bool
	JourneyOnly   bool

	LLMKey          string
	LLMOpts         LLMOptions
	LLMAddrExplicit bool
	ValidateLLMOpts func() error

	Progress io.Writer
}

func (r *Run) profile() taskseg.Profile {
	if r.Profile != nil {
		return r.Profile
	}
	return taskseg.OpenClawAware
}

type session struct {
	*Run
	setup *JourneySetup
}

// Execute runs the complete analyze workflow according to the configured Run options.
func (r *Run) Execute() error {
	chatmsg.ResetUnrecognizedShapeCounts()

	mode := analyzeModeString(r)
	targetL2, ok := computeTargetL2(r, mode)
	if ok && tryL2Cache(r, targetL2, mode) {
		return nil
	}

	if r.MacroOnly {
		return r.runMacroOnly()
	}

	su, err := r.setupJourneyRun()
	if err != nil {
		return err
	}

	s := &session{Run: r, setup: su}

	switch {
	case r.ListOnly:
		err = s.listJourneys()
	case r.BenchmarkFlag:
		err = s.renderBenchmarks()
	case r.CompareArg != "":
		err = s.dispatchCompare()
	case r.JourneyArg != "":
		err = s.dispatchJourney()
	default:
		var rep *report.Report
		rep, err = s.dispatchDefaultSuite()
		if err == nil {
			return r.finishAnalyze(rep)
		}
	}
	if err != nil {
		return err
	}
	return r.finishAnalyze(nil)
}

func (s *session) dispatchCompare() error {
	ids := strings.Split(s.CompareArg, ",")
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return fmt.Errorf("-compare wants exactly two comma-separated ids: -compare id1,id2")
	}
	if s.ValidateLLMOpts != nil {
		if err := s.ValidateLLMOpts(); err != nil {
			return err
		}
	}
	return s.compareJourneys(ids[0], ids[1])
}

func (s *session) dispatchJourney() error {
	ids := make([]string, len(s.setup.Cands))
	for i, ch := range s.setup.Chains {
		ids[i] = journey.ID(ch)
	}
	targets, err := resolveJourneySelector(s.setup.Cands, ids, s.JourneyArg)
	if err != nil {
		return err
	}
	if len(targets) == 1 {
		if s.ValidateLLMOpts != nil {
			if err := s.ValidateLLMOpts(); err != nil {
				return err
			}
		}
		return s.renderJourney(targets[0])
	}
	if s.LLMAddrExplicit {
		return fmt.Errorf("-llm-addr is not supported when -journey matches more than one journey (%d matched by %q) — use a single id/pattern that resolves to exactly one journey", len(targets), s.JourneyArg)
	}
	return s.renderJourneys(targets,
		"no matching journeys to render (all skipped as partial-head; pass -include-partial)", true)
}

func (s *session) dispatchDefaultSuite() (*report.Report, error) {
	scope := s.setup.Cands
	if !s.RenderAllFlag {
		scope = renderableCandidates(s.setup)
	}
	if err := s.renderAllJourneys(scope, s.RenderAllFlag); err != nil {
		return nil, fmt.Errorf("analyze (journey half): %w", err)
	}

	activeIDs := make([]string, len(s.setup.Cands))
	for i, ch := range s.setup.Chains {
		activeIDs[i] = journey.ID(ch)
	}
	_, _ = journey.CleanOrphanJourneys(filepath.Join(s.OutDir, "journeys", "details"), activeIDs)

	var rep *report.Report
	if !s.JourneyOnly {
		var err error
		rep, err = s.runReportHalf()
		if err != nil {
			return nil, err
		}
		if err := renderAllFromDisk(s.OutDir, s.Lang); err != nil {
			return nil, fmt.Errorf("render from disk: %w", err)
		}
	}
	return rep, nil
}

func (r *Run) runMacroOnly() error {
	rep, err := r.runReportHalf()
	if err != nil {
		return err
	}
	return r.finishAnalyze(rep)
}

func (r *Run) finishAnalyze(rep *report.Report) error {
	if err := dashboard.WriteSkeletons(r.OutDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed (pages may be stale until next analyze): %v\n", err)
	}
	if err := RebuildComparesIndex(filepath.Join(r.OutDir, "compares"), r.Lang); err != nil {
		fmt.Fprintf(os.Stderr, "compares index rebuild failed (stale until next analyze): %v\n", err)
	}
	if rep == nil {
		if err := r.commitManifest(rep); err != nil {
			return err
		}
	}
	recordPostAnalyzeCache(r)
	return nil
}

func (r *Run) commitManifest(rep *report.Report) error {
	return writeReportManifest(r.OutDir, rep, r.Lang)
}
