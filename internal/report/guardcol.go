// Ver 2026-09-16, by Sonnet 5

// Agent Guard's M2 offline aggregation: rolls every ingested record's
// verdict -- audit.Record.Guard when non-nil (the Authoritative Fast
// Path), else guardscan.go's Fallback Path result (rc.guardScan, ADR-12) --
// into a GuardSummary. Unlike stickyCollector (sticky.go), this needs no
// session ordering — it is a flat per-record accumulation — so it stays a
// single pass with no buffering, fed inline from ingestRecord like
// clientEndpointCollector.
package report

import (
	"sort"

	"vmr/internal/audit"
	"vmr/internal/core"
)

// guardCollector accumulates Agent Guard verdicts across the whole ingest
// pass. byRule/uniqueFP mirror the outbound-hit ranking table; providers/
// providerFP the M2.4 attribution; the rune/tool/echo fields the M2.3
// inbound forensics block.
type guardCollector struct {
	stamped         int
	scanned         int
	recordsWithHits int
	scanFailed      int
	rulesetVersion  int
	versionConflict bool // true once two verdicts disagree on Ver

	byRule   map[string]*GuardRuleRow
	uniqueFP map[string]map[string]bool // rule -> set of FP

	providers  map[string]*GuardProviderRow
	providerFP map[string]map[string]bool // provider -> set of FP

	runeCounts          map[string]int
	sanitizedRuneCounts map[string]int // online-stripped runes (Record.Guard.SanitizedRunes), see addSanitizedRunes
	toolCallsInspected  int
	toolFindings        map[string]*GuardToolRiskRow // risk category -> row
	toolFindingTools    map[string]map[string]bool   // risk category -> set of tool names
	toolEchoEvents      int
	textEchoEvents      int
	echoTools           map[string]bool
}

func newGuardCollector() *guardCollector {
	return &guardCollector{
		byRule:           map[string]*GuardRuleRow{},
		uniqueFP:         map[string]map[string]bool{},
		providers:        map[string]*GuardProviderRow{},
		providerFP:       map[string]map[string]bool{},
		toolFindings:     map[string]*GuardToolRiskRow{},
		toolFindingTools: map[string]map[string]bool{},
		echoTools:        map[string]bool{},
	}
}

// add ingests one record's Guard verdict, keyed by the OUTBOUND side's
// source: a record is either outbound-stamped (guardOutboundStamped(rc.guard),
// the Authoritative Fast Path — Hits come from the stamp) or
// fallback-scanned on its outbound side (extractRecordFacts always
// attempts that scan in that case, whether or not it found anything to
// report; rc.guardScan is nil only when nothing was found, not when
// nothing was attempted). The two groups partition every non-excluded
// record exactly once, so RecordsScanned/RecordsStamped/
// RecordsFallbackScanned are true coverage counts, not just a count of
// records that happened to produce a Finding.
func (gc *guardCollector) add(rc *rec2) {
	var hits []audit.Hit
	var ver int
	if guardOutboundStamped(rc.guard) {
		gc.stamped++
		hits, ver = rc.guard.Hits, rc.guard.Ver
	} else {
		gc.scanned++
		ver = guardEngine().Version()
		if rc.guardScan != nil {
			hits = rc.guardScan.Hits
		}
	}

	switch total := gc.stamped + gc.scanned; {
	case total == 1:
		gc.rulesetVersion = ver
	case ver != gc.rulesetVersion:
		gc.versionConflict = true
	}

	provider := providerOf(rc.endpoint)
	if rc.guardScanFailed {
		gc.scanFailed++
	}
	if len(hits) > 0 {
		gc.recordsWithHits++
		gc.addHits(hits, provider)
	}
	if rc.guardScan != nil {
		gc.addInbound(rc.guardScan)
	}
	if rc.guard != nil && len(rc.guard.SanitizedRunes) > 0 {
		gc.addSanitizedRunes(rc.guard.SanitizedRunes)
	}
}

// addSanitizedRunes folds one record's ONLINE sanitizer stamp
// (Record.Guard.SanitizedRunes) into a running total kept separate from
// runeCounts. The two are complementary, not overlapping facts: by the
// time a sanitized record's Client.Response.Body reaches the audit log,
// the A/B-tier runes the online sanitizer stripped are already gone --
// runeCounts (built from re-scanning that same body) can only ever see
// what survived sanitization (mostly C-tier). SanitizedRunes is the only
// record of what existed before the strip. Merging the two into one
// counter would silently understate "what the upstream actually sent" and
// contradict K-G14's own distinction between "already happened" and
// "was intercepted" -- this keeps them two clearly labeled numbers instead
// (independent review finding: this stamp previously had no consumer at
// all, docs/KNOWN_ISSUES §2.163).
func (gc *guardCollector) addSanitizedRunes(counts map[string]int) {
	if gc.sanitizedRuneCounts == nil {
		gc.sanitizedRuneCounts = map[string]int{}
	}
	for cat, n := range counts {
		gc.sanitizedRuneCounts[cat] += n
	}
}

// addHits folds one record's outbound Hits into the rule ranking table and
// (M2.4) the provider exposure table. Each Hit already carries the caller's
// per-(rule,FP) dedup key (aggregateHits/scanOutbound both key that way), so
// Hit.Count IS the "same credential resent N times" amplification factor
// its own doc comment describes -- MaxPerRecord takes the max of Count
// across a rule's distinct credentials in this record, never their sum, or
// several different credentials pasted once each in the same request reads
// as one credential amplified by the number of credentials.
func (gc *guardCollector) addHits(hits []audit.Hit, provider string) {
	seenRule := map[string]bool{}
	maxPerRule := map[string]int{}
	providerTotal := 0
	for _, hit := range hits {
		row := gc.byRule[hit.Rule]
		if row == nil {
			row = &GuardRuleRow{Rule: hit.Rule, Tier: hit.Tier}
			gc.byRule[hit.Rule] = row
			gc.uniqueFP[hit.Rule] = map[string]bool{}
		}
		row.TotalHits += hit.Count
		seenRule[hit.Rule] = true
		if hit.Count > maxPerRule[hit.Rule] {
			maxPerRule[hit.Rule] = hit.Count
		}
		if hit.FP != "" {
			gc.uniqueFP[hit.Rule][hit.FP] = true
		}
		if provider != "" {
			providerTotal += hit.Count
			if hit.FP != "" {
				if gc.providerFP[provider] == nil {
					gc.providerFP[provider] = map[string]bool{}
				}
				gc.providerFP[provider][hit.FP] = true
			}
		}
	}
	for rule := range seenRule {
		row := gc.byRule[rule]
		row.RecordsWith++
		if n := maxPerRule[rule]; n > row.MaxPerRecord {
			row.MaxPerRecord = n
		}
	}
	if provider != "" && providerTotal > 0 {
		pr := gc.providers[provider]
		if pr == nil {
			pr = &GuardProviderRow{Provider: provider}
			gc.providers[provider] = pr
		}
		pr.TotalHits += providerTotal
		pr.RecordsWith++
	}
}

// addInbound folds one record's Fallback Path inbound forensics (M2.3)
// into the running rune/tool/echo tallies.
func (gc *guardCollector) addInbound(gs *GuardScanFacts) {
	for cat, n := range gs.InboundRunes {
		if gc.runeCounts == nil {
			gc.runeCounts = map[string]int{}
		}
		gc.runeCounts[cat] += n
	}
	gc.toolCallsInspected += gs.ToolCallsInspected
	gc.textEchoEvents += gs.TextEchoCount
	for _, tf := range gs.ToolFindings {
		if tf.Echoed {
			gc.toolEchoEvents++
			gc.echoTools[tf.Tool] = true
		}
		if tf.Hit == "" {
			continue
		}
		row := gc.toolFindings[tf.Hit]
		if row == nil {
			row = &GuardToolRiskRow{Category: tf.Hit, CWE: tf.CWE}
			gc.toolFindings[tf.Hit] = row
			gc.toolFindingTools[tf.Hit] = map[string]bool{}
		}
		row.Count++
		gc.toolFindingTools[tf.Hit][tf.Tool] = true
	}
}

// providerOf extracts the actually-served provider name from an
// EndpointLabel-shaped endpoint string (core.EndpointLabel's
// "protocol:provider:model" format) — the M2.4 attribution basis: the
// record's real served attempt, never the virtual model's whole candidate
// set (Failover can land on any one of them). "" when the record has no
// served endpoint (e.g. every attempt failed).
func providerOf(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	_, provider, _, ok := core.SplitEndpointLabel(endpoint)
	if !ok {
		return ""
	}
	return provider
}

// result finalizes the aggregate — nil when Agent Guard produced no
// verdict for any record (total == 0), matching Report2.Guard's "nil,
// not an empty struct" contract so macro/guard.json is only written when
// there is actual data.
func (gc *guardCollector) result() *GuardSummary {
	total := gc.stamped + gc.scanned
	if total == 0 {
		return nil
	}
	rules := make([]GuardRuleRow, 0, len(gc.byRule))
	for name, row := range gc.byRule {
		row.UniqueFP = len(gc.uniqueFP[name])
		rules = append(rules, *row)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].TotalHits != rules[j].TotalHits {
			return rules[i].TotalHits > rules[j].TotalHits
		}
		return rules[i].Rule < rules[j].Rule
	})

	s := &GuardSummary{
		RecordsScanned:         total,
		RecordsWithHits:        gc.recordsWithHits,
		RecordsStamped:         gc.stamped,
		RecordsFallbackScanned: gc.scanned,
		RecordsScanFailed:      gc.scanFailed,
		Rules:                  rules,
		Providers:              gc.buildProviders(),
		Inbound:                gc.buildInbound(),
	}
	if !gc.versionConflict {
		s.RulesetVersion = gc.rulesetVersion
	}
	return s
}

func (gc *guardCollector) buildProviders() []GuardProviderRow {
	if len(gc.providers) == 0 {
		return nil
	}
	out := make([]GuardProviderRow, 0, len(gc.providers))
	for name, row := range gc.providers {
		row.UniqueCredentials = len(gc.providerFP[name])
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalHits != out[j].TotalHits {
			return out[i].TotalHits > out[j].TotalHits
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func (gc *guardCollector) buildInbound() *GuardInboundSummary {
	if len(gc.runeCounts) == 0 && len(gc.sanitizedRuneCounts) == 0 && gc.toolCallsInspected == 0 && len(gc.toolFindings) == 0 && gc.toolEchoEvents == 0 && gc.textEchoEvents == 0 {
		return nil
	}
	in := &GuardInboundSummary{
		RuneCounts:          gc.runeCounts,
		SanitizedRuneCounts: gc.sanitizedRuneCounts,
		ToolCallsInspected:  gc.toolCallsInspected,
		ToolEchoEvents:      gc.toolEchoEvents,
		TextEchoEvents:      gc.textEchoEvents,
	}
	for name := range gc.echoTools {
		in.EchoTools = append(in.EchoTools, name)
	}
	sort.Strings(in.EchoTools)
	for cat, row := range gc.toolFindings {
		for name := range gc.toolFindingTools[cat] {
			row.Tools = append(row.Tools, name)
		}
		sort.Strings(row.Tools)
		in.ToolFindings = append(in.ToolFindings, *row)
	}
	sort.Slice(in.ToolFindings, func(i, j int) bool {
		if in.ToolFindings[i].Count != in.ToolFindings[j].Count {
			return in.ToolFindings[i].Count > in.ToolFindings[j].Count
		}
		return in.ToolFindings[i].Category < in.ToolFindings[j].Category
	})
	return in
}
