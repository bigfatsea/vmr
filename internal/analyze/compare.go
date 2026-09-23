// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"vmr/internal/journey"
)

func (s *session) compareJourneys(idA, idB string) error {
	_, chainA, err := resolveJourneyID(s.setup.Cands, s.setup.ByIdx, idA)
	if err != nil {
		return fmt.Errorf("-compare first id: %w", err)
	}
	_, chainB, err := resolveJourneyID(s.setup.Cands, s.setup.ByIdx, idB)
	if err != nil {
		return fmt.Errorf("-compare second id: %w", err)
	}
	partialA := journey.IsPartialHead(chainA, s.setup.FirstPath)
	partialB := journey.IsPartialHead(chainB, s.setup.FirstPath)
	if (partialA || partialB) && !s.IncludePartial {
		return fmt.Errorf("one or both journeys look head-truncated — pass -include-partial to compare them anyway")
	}

	jA, err := journey.BuildChain(chainA, s.profile(), s.Lang)
	if err != nil {
		return err
	}
	jB, err := journey.BuildChain(chainB, s.profile(), s.Lang)
	if err != nil {
		return err
	}

	jA.Partial = partialA
	jB.Partial = partialB
	sA, sB := journey.Summarize(jA), journey.Summarize(jB)
	cmp := journey.Compare(sA, sB)
	cmp.A.ReportFile = filepath.ToSlash(filepath.Join("..", "journeys", "details", journey.JourneyReportFile(jA.ID)))
	cmp.B.ReportFile = filepath.ToSlash(filepath.Join("..", "journeys", "details", journey.JourneyReportFile(jB.ID)))
	extras := journey.ComputeComparisonExtras(jA, jB, sA.Metrics, sB.Metrics, s.PriceRes, s.Currency)
	extras.Sources = journey.SourceFiles(s.setup.Idx, jA.ID, jB.ID)
	cmp.Extras = &extras

	llmOpts := s.LLMOpts
	if llmOpts.Addr != "" && llmOpts.DryRun {
		pack := journey.BuildEvidencePack(jA, jB, cmp, s.Lang)
		chars := pack.EstimateChars()
		fmt.Printf("evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", chars, chars/4)
		if extras.Divergence.Found {
			divPack := journey.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, s.Lang)
			divChars := divPack.EstimateChars()
			fmt.Printf("divergence evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", divChars, divChars/4)
		}
		return nil
	}

	costA, costB := extras.Cost.A, extras.Cost.B
	if err := s.ensureJourneyFile(jA, &costA); err != nil {
		return err
	}
	if err := s.ensureJourneyFile(jB, &costB); err != nil {
		return err
	}

	llmOverall, llmDiv := s.compareLLMRecords(jA, jB, cmp, extras)
	cmp.LLMInterpretation = llmOverall
	cmp.LLMDivergence = llmDiv

	comparesDir, err := ensureComparesDir(s.OutDir)
	if err != nil {
		return err
	}

	base := "compare-" + jA.ID + "-vs-" + jB.ID
	jsonPath := filepath.Join(comparesDir, base+".json")
	data, err := json.MarshalIndent(cmp, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return err
	}

	diskData, err := os.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	var diskCmp journey.Comparison
	if err := json.Unmarshal(diskData, &diskCmp); err != nil {
		return err
	}

	mdPath := filepath.Join(comparesDir, base+".md")
	if err := os.WriteFile(mdPath, []byte(journey.RenderComparisonMarkdown(diskCmp, s.Lang)), 0o600); err != nil {
		return err
	}
	fmt.Printf("%s\n", mdPath)
	updateJourneyRow(s.setup.Idx, jA.ID, len(jA.Tasks), journeySteps(jA), filepath.ToSlash(filepath.Join("details", journeyBaseName(jA)+".md")), rowFacts(sA.Metrics, &costA))
	updateJourneyRow(s.setup.Idx, jB.ID, len(jB.Tasks), journeySteps(jB), filepath.ToSlash(filepath.Join("details", journeyBaseName(jB)+".md")), rowFacts(sB.Metrics, &costB))
	return saveJourneyIndex(s.setup.Idx, s.OutDir, s.Lang)
}

func (s *session) compareLLMRecords(jA, jB *journey.Journey, cmp journey.Comparison, extras journey.ComparisonExtras) (overall, div *journey.LLMInterpretation) {
	llmOpts := s.LLMOpts
	if llmOpts.Addr == "" {
		return nil, nil
	}
	pack := journey.BuildEvidencePack(jA, jB, cmp, s.Lang)
	chars := pack.EstimateChars()
	fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
	res, err := journey.Interpret(context.Background(), llmOpts.LLMOptions, pack, s.Lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
	}
	overall = journey.NewLLMInterpretation(llmOpts.LLMOptions, res, err, journey.LLMScopeOverall, s.Lang)

	if extras.Divergence.Found {
		divPack := journey.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, s.Lang)
		divChars := divPack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s) for the divergence point: evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, divChars, divChars/4)
		divRes, divErr := journey.Interpret(context.Background(), llmOpts.LLMOptions, divPack, s.Lang)
		if divErr != nil {
			fmt.Fprintf(os.Stderr, "warning: divergence LLM interpretation failed, report will not include it: %v\n", divErr)
		}
		div = journey.NewLLMInterpretation(llmOpts.LLMOptions, divRes, divErr, journey.LLMScopeDivergence, s.Lang)
	}
	return overall, div
}
