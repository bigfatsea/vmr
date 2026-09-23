// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/journey"
)

type journeyRowFacts struct {
	cost         *float64
	costCurrency string
	netWorkingMS int64
	model        string
}

func updateJourneyRow(idx *journey.JourneyIndex, id string, tasks, steps int, rendered string, facts *journeyRowFacts) {
	for i := range idx.Journeys {
		if idx.Journeys[i].ID == id {
			idx.Journeys[i].Tasks = tasks
			idx.Journeys[i].Steps = steps
			if rendered != "" {
				idx.Journeys[i].Rendered = rendered
			}
			if facts != nil {
				idx.Journeys[i].Cost = facts.cost
				idx.Journeys[i].Currency = facts.costCurrency
				idx.Journeys[i].NetWorkingMS = facts.netWorkingMS
				idx.Journeys[i].Model = facts.model
			}
			return
		}
	}
}

func rowFacts(m journey.Metrics, cost *journey.CostFact) *journeyRowFacts {
	f := &journeyRowFacts{netWorkingMS: m.NetWorkingMS, model: journey.DominantModel(m)}
	if cost != nil && cost.Total != nil {
		f.cost = cost.Total
		f.costCurrency = cost.Currency
	}
	return f
}

func saveJourneyIndex(idx *journey.JourneyIndex, outDir string, lang i18n.Lang) error {
	journeysDir, err := ensureJourneysDir(outDir)
	if err != nil {
		return err
	}
	if idx != nil && len(idx.Journeys) > 1 {
		idx.Clusters = journey.ComputeTaskClusters(idx.Journeys)
	}
	indexPath := filepath.Join(journeysDir, "index.json")
	if err := idx.Save(indexPath); err != nil {
		return err
	}
	diskIdx := journey.LoadJourneyIndex(indexPath)
	md := journey.RenderJourneyIndexMarkdown(diskIdx, lang)
	if err := writeAtomic(journeysDir, "index.md", []byte(md)); err != nil {
		return err
	}
	return ctxgraph.SaveCacheDir(filepath.Join(outDir, ".cache", "parse"), idx.Cache)
}

func resolveJourneyID(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, pat string) (*ctxgraph.Lineage, []*ctxgraph.Lineage, error) {
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		if journeyPatternMatches(journey.ID(chain), pat) {
			return l, chain, nil
		}
	}
	return nil, nil, fmt.Errorf("no journey matching %q (run without -journey to list candidates)", pat)
}

func journeyPatternMatches(id, pattern string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		ok, err := path.Match(pattern, id)
		return err == nil && ok
	}
	return strings.HasPrefix(id, pattern)
}

func resolveJourneySelector(cands []*ctxgraph.Lineage, ids []string, selector string) ([]*ctxgraph.Lineage, error) {
	matched := make([]bool, len(cands))
	for _, tok := range strings.Split(selector, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("-journey %q: empty id/pattern between commas", selector)
		}
		hit := false
		for i, id := range ids {
			if journeyPatternMatches(id, tok) {
				matched[i] = true
				hit = true
			}
		}
		if !hit {
			return nil, fmt.Errorf("no journey matching %q (run without -journey to list candidates)", tok)
		}
	}
	var out []*ctxgraph.Lineage
	for i, l := range cands {
		if matched[i] {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *session) listJourneys() error {
	idx := s.setup.Idx
	g := s.setup.G
	t := i18n.CLI(s.Lang)
	excluded := len(g.Lineages) - len(idx.Journeys)
	fmt.Printf("%d candidate journey(s) (%d total lineage(s), %d single-request/scheduled excluded or absorbed into a stitched chain):\n\n", len(idx.Journeys), len(g.Lineages), excluded)

	skippedPartial := 0
	for _, r := range idx.Journeys {
		if r.Partial && !s.IncludePartial {
			skippedPartial++
			continue
		}
		mark := ""
		if r.Partial {
			mark = t.HeadTruncatedMark
		}
		if r.Stitched > 1 {
			mark += t.StitchedMark(r.Stitched)
		}
		fmt.Print(t.ListLine(r.ID, mark, r.Requests, r.Start.In(fmtutil.DisplayZone).Format("01-02 15:04"), r.End.In(fmtutil.DisplayZone).Format("15:04"), r.Title))
	}
	if skippedPartial > 0 {
		fmt.Print(t.SkippedPartialNote(skippedPartial))
	}
	fmt.Print(t.RenderHint)
	return saveJourneyIndex(idx, s.OutDir, s.Lang)
}

const maxUngroupedShown = 10

func printUngrouped(ms []*ctxgraph.Manifest, lang i18n.Lang) {
	if len(ms) == 0 {
		return
	}
	t := i18n.CLI(lang)
	n := len(ms)
	if n > maxUngroupedShown {
		n = maxUngroupedShown
	}
	fmt.Print(t.UngroupedHeader(n))
	for _, m := range ms[:n] {
		fmt.Printf("    %s:%d  ts=%s\n", m.Path, m.Line, m.TS.In(fmtutil.DisplayZone).Format("01-02 15:04:05"))
	}
	if len(ms) > n {
		fmt.Print(t.UngroupedMore(len(ms) - n))
	}
}

type journeyFileOpts struct {
	LLMInterp          *journey.LLMInterpretation
	LLMFindings        []journey.Finding
	Cost               *journey.CostFact
	MaterializeDetails bool
	Recs               map[ctxgraph.Loc]*audit.Record
}

func (s *session) renderJourney(target *ctxgraph.Lineage) error {
	t := i18n.CLI(s.Lang)
	chain := ctxgraph.ChainFrom(target, s.setup.ByIdx)
	partial := journey.IsPartialHead(chain, s.setup.FirstPath)
	if partial && !s.IncludePartial {
		return fmt.Errorf("journey %s looks head-truncated — pass -include-partial to render it anyway", journey.ID(chain))
	}
	j, err := journey.BuildChain(chain, s.profile(), s.Lang)
	if err != nil {
		return err
	}
	j.Partial = partial
	m := journey.ComputeMetrics(j)
	findings := journey.ComputeFindings(j)

	llmOpts := s.LLMOpts
	if llmOpts.Addr != "" && llmOpts.DryRun {
		fmt.Print(journey.FormatLLMDryRun(journey.EstimateLLMDryRun(j, m, findings, s.Lang)))
		return nil
	}

	var llmInterp *journey.LLMInterpretation
	var llmFindings []journey.Finding
	if llmOpts.Addr != "" {
		if findingsLLM, err := journey.ComputeLLMFindings(context.Background(), j, llmOpts.LLMOptions, s.Lang); err == nil && len(findingsLLM) > 0 {
			llmFindings = findingsLLM
			findings = append(findings, llmFindings...)
			sort.SliceStable(findings, func(a, b int) bool {
				if findings[a].StepSeq != findings[b].StepSeq {
					return findings[a].StepSeq < findings[b].StepSeq
				}
				return findings[a].Code < findings[b].Code
			})
		}
		pack := journey.BuildSingleJourneyEvidencePack(j, m, findings, s.Lang)
		chars := pack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
		res, err := journey.Interpret(context.Background(), llmOpts.LLMOptions, pack, s.Lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
		}
		llmInterp = journey.NewLLMInterpretation(llmOpts.LLMOptions, res, err, "", s.Lang)
	}

	cost := journey.ComputeJourneyCost(j, s.PriceRes, s.Currency)
	outPath, err := s.writeJourneyFile(j, m, findings, journeyFileOpts{
		LLMInterp:          llmInterp,
		LLMFindings:        llmFindings,
		Cost:               &cost,
		MaterializeDetails: true,
	})
	if err != nil {
		return err
	}
	fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
	updateJourneyRow(s.setup.Idx, j.ID, len(j.Tasks), journeySteps(j), filepath.ToSlash(filepath.Join("details", journeyBaseName(j)+".md")), rowFacts(m, &cost))
	return saveJourneyIndex(s.setup.Idx, s.OutDir, s.Lang)
}

func (s *session) renderJourneys(cands []*ctxgraph.Lineage, noneMsg string, materializeDetails bool) error {
	var toRender [][]*ctxgraph.Lineage
	var toRenderPartial []bool
	skippedPartial := 0
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, s.setup.ByIdx)
		partial := journey.IsPartialHead(chain, s.setup.FirstPath)
		if partial && !s.IncludePartial {
			skippedPartial++
			continue
		}
		toRender = append(toRender, chain)
		toRenderPartial = append(toRenderPartial, partial)
	}
	if len(toRender) == 0 {
		fmt.Println(noneMsg)
		return saveJourneyIndex(s.setup.Idx, s.OutDir, s.Lang)
	}

	journeysDir, err := ensureJourneysDir(s.OutDir)
	if err != nil {
		return err
	}
	t := i18n.CLI(s.Lang)
	rendered := 0
	for _, br := range batchByBytes(toRender, renderBatchBudgetBytes) {
		start, end := br[0], br[1]
		journeys, batchRecs, err := journey.BuildAllWithRecords(toRender[start:end], s.profile(), s.Lang)
		if err != nil {
			return err
		}
		for i, j := range journeys {
			j.Partial = toRenderPartial[start+i]
			m := journey.ComputeMetrics(j)
			findings := journey.ComputeFindings(j)
			cost := journey.ComputeJourneyCost(j, s.PriceRes, s.Currency)
			outPath, err := s.writeJourneyFile(j, m, findings, journeyFileOpts{
				Cost:               &cost,
				MaterializeDetails: materializeDetails,
				Recs:               batchRecs,
			})
			if err != nil {
				return err
			}
			fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
			updateJourneyRow(s.setup.Idx, j.ID, len(j.Tasks), journeySteps(j), filepath.ToSlash(filepath.Join("details", filepath.Base(outPath))), rowFacts(m, &cost))
		}
		rendered += len(journeys)
	}
	if skippedPartial > 0 {
		fmt.Print(t.AllRenderedSkipped(skippedPartial))
	}
	fmt.Print(t.AllRenderedNote(rendered, journeysDir))
	return saveJourneyIndex(s.setup.Idx, s.OutDir, s.Lang)
}

func (s *session) renderAllJourneys(cands []*ctxgraph.Lineage, materializeDetails bool) error {
	return s.renderJourneys(cands, "no candidate journeys to render (all skipped as partial-head; pass -include-partial)", materializeDetails)
}

func (s *session) renderBenchmarks() error {
	var toRender [][]*ctxgraph.Lineage
	skippedPartial := 0
	for _, l := range s.setup.Cands {
		chain := ctxgraph.ChainFrom(l, s.setup.ByIdx)
		partial := journey.IsPartialHead(chain, s.setup.FirstPath)
		if partial && !s.IncludePartial {
			skippedPartial++
			continue
		}
		toRender = append(toRender, chain)
	}
	if len(toRender) == 0 {
		fmt.Println("no candidate journeys to analyze (all skipped as partial-head; pass -include-partial)")
		return saveJourneyIndex(s.setup.Idx, s.OutDir, s.Lang)
	}

	var journeys []*journey.Journey
	for _, br := range batchByBytes(toRender, renderBatchBudgetBytes) {
		js, err := journey.BuildAll(toRender[br[0]:br[1]], s.profile(), s.Lang)
		if err != nil {
			return err
		}
		journeys = append(journeys, js...)
	}
	stats := journey.ComputeBenchmarkStats(journeys)

	journeysDir, err := ensureJourneysDir(s.OutDir)
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(journeysDir, "benchmarks.json")
	data, err := json.MarshalIndent(stats, "", "  ")
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
	var diskStats journey.BenchmarkStats
	if err := json.Unmarshal(diskData, &diskStats); err != nil {
		return err
	}
	mdPath := filepath.Join(journeysDir, "benchmarks.md")
	if err := os.WriteFile(mdPath, []byte(journey.RenderBenchmarksMarkdown(diskStats, s.Lang)), 0o600); err != nil {
		return err
	}
	if skippedPartial > 0 {
		fmt.Printf("%d head-truncated journey(s) skipped (pass -include-partial to include them)\n", skippedPartial)
	}
	fmt.Printf("%d journey(s) analyzed → %s\n", len(journeys), mdPath)
	for _, j := range journeys {
		updateJourneyRow(s.setup.Idx, j.ID, len(j.Tasks), journeySteps(j), "", rowFacts(journey.ComputeMetrics(j), nil))
	}
	return saveJourneyIndex(s.setup.Idx, s.OutDir, s.Lang)
}

func detailAndEvidenceDirs(outDir string) (detailDir, evidenceDir string) {
	return filepath.Join(outDir, "requests", "details"), filepath.Join(outDir, "requests", "evidence")
}

func ensureJourneysDir(outDir string) (string, error) {
	journeysDir := filepath.Join(outDir, "journeys")
	if err := os.MkdirAll(journeysDir, 0o700); err != nil {
		return "", err
	}
	return journeysDir, nil
}

func ensureComparesDir(outDir string) (string, error) {
	comparesDir := filepath.Join(outDir, "compares")
	if err := os.MkdirAll(comparesDir, 0o700); err != nil {
		return "", err
	}
	return comparesDir, nil
}

func journeyBaseName(j *journey.Journey) string {
	return strings.TrimSuffix(journey.JourneyReportFile(j.ID), ".md")
}

func (s *session) ensureJourneyFile(j *journey.Journey, cost *journey.CostFact) error {
	m := journey.ComputeMetrics(j)
	findings := journey.ComputeFindings(j)
	_, err := s.writeJourneyFile(j, m, findings, journeyFileOpts{
		Cost:               cost,
		MaterializeDetails: true,
	})
	return err
}

func (s *session) writeJourneyFile(j *journey.Journey, m journey.Metrics, findings []journey.Finding, opts journeyFileOpts) (string, error) {
	journeysDir, err := ensureJourneysDir(s.OutDir)
	if err != nil {
		return "", err
	}
	detailsDir := filepath.Join(journeysDir, "details")
	if err := os.MkdirAll(detailsDir, 0o700); err != nil {
		return "", err
	}
	detailDir, evidenceDir := detailAndEvidenceDirs(s.OutDir)
	base := journeyBaseName(j)
	outPath := filepath.Join(detailsDir, base+".md")
	if opts.MaterializeDetails {
		journey.EnsureJourneyDetails(os.Stderr, j, opts.Recs, detailDir, evidenceDir, s.profile(), s.Lang)
	}

	jsonPath := filepath.Join(detailsDir, base+".json")
	llmInterp := opts.LLMInterp
	llmFindings := opts.LLMFindings
	if llmInterp == nil && len(llmFindings) == 0 {
		if existingBytes, err := os.ReadFile(jsonPath); err == nil {
			var existing journey.JourneySummary
			if err := json.Unmarshal(existingBytes, &existing); err == nil {
				if existing.LLMInterpretation != nil {
					llmInterp = existing.LLMInterpretation
				}
				if len(existing.LLMFindings) > 0 {
					llmFindings = existing.LLMFindings
				}
			}
		}
	}
	summary := journey.NewJourneySummary(j, m, findings, llmFindings, opts.Cost, llmInterp)
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return "", err
	}

	diskData, err := os.ReadFile(jsonPath)
	if err != nil {
		return "", err
	}
	var sum journey.JourneySummary
	if err := json.Unmarshal(diskData, &sum); err != nil {
		return "", err
	}

	_, reportMDErr := os.Stat(filepath.Join(s.OutDir, "vmr-report.md"))
	linkDetails := opts.MaterializeDetails || detailDirHasFiles(detailDir)
	md := journey.RenderMarkdownFromSummary(&sum, s.Lang, reportMDErr == nil, linkDetails)
	if err := os.WriteFile(outPath, []byte(md), 0o600); err != nil {
		return "", err
	}
	return outPath, nil
}

func journeySteps(j *journey.Journey) int {
	steps := 0
	for _, t := range j.Tasks {
		steps += len(t.Steps)
	}
	return steps
}
