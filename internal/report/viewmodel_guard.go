// Ver 2026-09-14, by Sonnet 5

// Agent Guard's M2 offline bidirectional forensic audit section view model
// (the Agent Guard spec §4.7). Reads rep.Guard —
// nil only when the Fallback Path never had a single record to cover (see
// that field's own doc comment) — rendering, in order: coverage/source,
// the Tier1/Tier2 outbound-credential ranking (M2.2), the M2.4 provider
// exposure table, and the M2.3 inbound forensics block. Pairs with
// internal/i18n/report_guard.go.
package report

import (
	"sort"
	"strconv"

	"vmr/internal/i18n"
)

func vmGuardSection(rep *Report2, lang i18n.Lang) SectionVM {
	g := rep.Guard
	if g == nil {
		return SectionVM{}
	}
	t := i18n.Guard(lang)
	sec := SectionVM{ID: "guard", Title: t.Title}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.Intro(g.RecordsScanned, g.RecordsWithHits)})
	if g.RecordsScanFailed > 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ScanFailedLine(g.RecordsScanFailed)})
	}
	if g.RecordsStamped > 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.SourceLine(g.RecordsStamped, g.RecordsFallbackScanned)})
	}

	tier1 := guardRulesForTier(g.Rules, 1)
	tier2 := guardRulesForTier(g.Rules, 2)
	if len(tier1) > 0 {
		sec.Blocks = append(sec.Blocks, HeadingVM{Level: 3, Text: t.Tier1Label})
		sec.Blocks = append(sec.Blocks, guardRuleTable(tier1, t))
	}
	if len(tier2) > 0 {
		sec.Blocks = append(sec.Blocks, HeadingVM{Level: 3, Text: t.Tier2Label})
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.Tier2Note})
		sec.Blocks = append(sec.Blocks, guardRuleTable(tier2, t))
	}
	if len(g.Rules) > 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.Amplification})
	}
	if len(g.Providers) > 0 {
		sec.Blocks = append(sec.Blocks, HeadingVM{Level: 3, Text: t.ProvidersLabel})
		sec.Blocks = append(sec.Blocks, guardProviderTable(g.Providers, t))
	}
	if g.Inbound != nil {
		sec.Blocks = append(sec.Blocks, vmGuardInboundBlocks(g.Inbound, t)...)
	}
	return sec
}

func guardRulesForTier(rules []GuardRuleRow, tier int) []GuardRuleRow {
	var out []GuardRuleRow
	for _, r := range rules {
		if r.Tier == tier {
			out = append(out, r)
		}
	}
	return out
}

func guardRuleTable(rules []GuardRuleRow, t i18n.GuardText) *TableVM {
	tbl := &TableVM{Headers: t.TableHeaders[:]}
	for _, r := range rules {
		tbl.row(r.Rule, strconv.Itoa(r.Tier), strconv.Itoa(r.UniqueFP),
			strconv.Itoa(r.TotalHits), strconv.Itoa(r.RecordsWith), strconv.Itoa(r.MaxPerRecord))
	}
	return tbl
}

func guardProviderTable(rows []GuardProviderRow, t i18n.GuardText) *TableVM {
	tbl := &TableVM{Headers: t.ProviderTableHeaders[:]}
	for _, r := range rows {
		tbl.row(r.Provider, strconv.Itoa(r.TotalHits), strconv.Itoa(r.UniqueCredentials), strconv.Itoa(r.RecordsWith))
	}
	return tbl
}

// vmGuardInboundBlocks renders M2.3's inbound forensics: rune counts (only
// the categories actually seen — a run with zero A/B-tier hits, the real-
// corpus baseline, renders nothing here at all), the online sanitizer's
// own SanitizedRuneCounts stamp as a SEPARATE table (what existed before
// the strip, not a subset of what's left — see rows.go's own doc comment
// on why the two never merge), tool-call risk findings, and the
// credential-echo line. K-G14 governs every string here: this describes
// what the client already received (or, for the sanitized table, would
// have received without ADR-15's one remaining online intervention),
// never anything blocked — there is no block path left to describe.
func vmGuardInboundBlocks(in *GuardInboundSummary, t i18n.GuardText) []BlockVM {
	var out []BlockVM
	out = append(out, HeadingVM{Level: 3, Text: t.InboundLabel})
	out = append(out, ParaVM{Text: t.InboundIntro(in.ToolCallsInspected)})
	if len(in.RuneCounts) > 0 {
		names := make([]string, 0, len(in.RuneCounts))
		for name := range in.RuneCounts {
			names = append(names, name)
		}
		sort.Strings(names)
		tbl := &TableVM{Headers: t.RuneTableHeaders[:]}
		for _, name := range names {
			tbl.row(t.RuneCategoryLabel(name), strconv.Itoa(in.RuneCounts[name]))
		}
		out = append(out, tbl)
	}
	if len(in.SanitizedRuneCounts) > 0 {
		names := make([]string, 0, len(in.SanitizedRuneCounts))
		for name := range in.SanitizedRuneCounts {
			names = append(names, name)
		}
		sort.Strings(names)
		out = append(out, ParaVM{Text: t.SanitizedRunesLabel})
		tbl := &TableVM{Headers: t.RuneTableHeaders[:]}
		for _, name := range names {
			tbl.row(t.RuneCategoryLabel(name), strconv.Itoa(in.SanitizedRuneCounts[name]))
		}
		out = append(out, tbl)
	}
	if len(in.ToolFindings) > 0 {
		tbl := &TableVM{Headers: t.ToolRiskTableHeaders[:]}
		for _, f := range in.ToolFindings {
			tbl.row(f.Category, f.CWE, strconv.Itoa(f.Count), joinTools(f.Tools))
		}
		out = append(out, tbl)
	}
	out = append(out, ParaVM{Text: t.EchoLine(in.ToolEchoEvents, in.TextEchoEvents)})
	return out
}

func joinTools(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	out := tools[0]
	for _, tl := range tools[1:] {
		out += ", " + tl
	}
	return out
}
