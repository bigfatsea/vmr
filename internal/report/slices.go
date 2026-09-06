// Ver 2026-09-06, by Claude
//
// Domain-sliced macro report schemas and writers (§3.2, §0.3 D2).
// Deconstructs the monolithic Report2 into five independent domain slices:
//   - macro/summary.json: overall traffic, efficiency findings, opening highlights
//   - macro/finance.json: cost attribution by model/client/endpoint, provider accounts, quota compliance
//   - macro/reliability.json: endpoint availability, error classification, failover, latency, sticky effectiveness
//   - macro/workloads.json: temporal distributions, workload classes, client-endpoint routing
//   - macro/context-efficiency.json: session growth, compaction loss proxy, tool schema waste
//
// Slices do not cross-reference values directly (D1). All outputs are written atomically (0600).
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// CostCoverage captures structured disclosures on pricing completeness (§3.3):
// unpriced endpoints that served traffic, incomplete rates missing components,
// and the share of estimated spend from degraded usage.
type CostCoverage struct {
	UnpricedCount       int     `json:"unpriced_count"`
	IncompleteRateCount int     `json:"incomplete_rate_count"`
	DegradedEstimatePct float64 `json:"degraded_estimate_pct"`
}

// BuildCostCoverage derives the cost coverage disclosure facts from rep's endpoints.
func BuildCostCoverage(rep *Report2) CostCoverage {
	if rep == nil {
		return CostCoverage{}
	}
	var unpriced int
	var pricedSum float64
	var degraded float64
	var incomplete int

	eps := rep.EndpointsAll
	if len(eps) == 0 {
		eps = rep.Endpoints
	}
	for _, e := range eps {
		if e.CostRateIncomplete {
			incomplete++
		}
		degraded += e.CostEstimateEst
		if e.CostEstimate != nil {
			pricedSum += *e.CostEstimate
		} else if e.Forwarded > 0 {
			unpriced++
		}
	}
	var degPct float64
	if pricedSum > 0 && degraded > 0 {
		degPct = round2(degraded / pricedSum * 100)
	}
	return CostCoverage{
		UnpricedCount:       unpriced,
		IncompleteRateCount: incomplete,
		DegradedEstimatePct: degPct,
	}
}

// SummarySlice is the macro/summary.json schema (§3.2): headline traffic,
// success rate, spend, findings, and opening highlights.
type SummarySlice struct {
	Overall    Row       `json:"overall"`
	Efficiency []Finding `json:"efficiency,omitempty"`
	Highlights []string  `json:"highlights,omitempty"`
}

// FinanceSlice is the macro/finance.json schema (§3.2): cost attribution by
// model/client/endpoint, provider accounts, and quota compliance.
type FinanceSlice struct {
	ByModel                       []Row              `json:"by_model,omitempty"`
	ByClient                      []ClientRow        `json:"by_client,omitempty"`
	Providers                     []ProviderRow      `json:"providers,omitempty"`
	ProviderQuotas                []ProviderQuotaRow `json:"provider_quotas,omitempty"`
	ProviderQuotaSkippedAttempts  int                `json:"provider_quota_skipped_attempts,omitempty"`
	ProviderQuotaSkippedProviders []string           `json:"provider_quota_skipped_providers,omitempty"`
	CostCoverage                  CostCoverage       `json:"cost_coverage"`
}

// ReliabilitySlice is the macro/reliability.json schema (§3.2): endpoint
// availability, error classification, failover performance, latency
// percentiles, and sticky model effectiveness.
type ReliabilitySlice struct {
	Endpoints    []EndpointRow `json:"endpoints,omitempty"`
	EndpointsAll []EndpointRow `json:"endpoints_all,omitempty"`
	Sticky       *StickyEffect `json:"sticky,omitempty"`
}

// WorkloadsSlice is the macro/workloads.json schema (§3.2): temporal
// distributions, workload class attribution, and client-endpoint routing.
type WorkloadsSlice struct {
	ByDate          []Row               `json:"by_date,omitempty"`
	Hours           []HourRow           `json:"hours,omitempty"`
	HoursOfDay      []HourRow           `json:"hours_of_day,omitempty"`
	Workloads       []WorkloadRow       `json:"workloads,omitempty"`
	ClientEndpoints []ClientEndpointRow `json:"client_endpoints,omitempty"`
}

// ContextEfficiencySlice is the macro/context-efficiency.json schema (§3.2):
// session growth, compaction loss proxy, and declared-tool schema waste.
type ContextEfficiencySlice struct {
	Sessions    []SessionRow    `json:"sessions,omitempty"`
	Compactions []CompactionRow `json:"compactions,omitempty"`
	Tools       []ToolShapeRow  `json:"tools,omitempty"`
}

// BuildSummarySlice projects rep into SummarySlice with localized highlights and findings.
func BuildSummarySlice(r *Report2, lang i18n.Lang) SummarySlice {
	if r == nil {
		return SummarySlice{}
	}
	findings := r.Efficiency
	if lang != i18n.EN || len(findings) == 0 {
		findings = buildFindings(r, lang)
	}
	hl := highlights(r, lang)
	return SummarySlice{
		Overall:    r.Overall,
		Efficiency: findings,
		Highlights: hl,
	}
}

// BuildFinanceSlice projects rep into FinanceSlice including CostCoverage.
func BuildFinanceSlice(r *Report2) FinanceSlice {
	if r == nil {
		return FinanceSlice{}
	}
	return FinanceSlice{
		ByModel:                       r.ByModel,
		ByClient:                      r.ByClient,
		Providers:                     r.Providers,
		ProviderQuotas:                r.ProviderQuotas,
		ProviderQuotaSkippedAttempts:  r.ProviderQuotaSkippedAttempts,
		ProviderQuotaSkippedProviders: r.ProviderQuotaSkippedProviders,
		CostCoverage:                  BuildCostCoverage(r),
	}
}

// BuildReliabilitySlice projects rep into ReliabilitySlice.
func BuildReliabilitySlice(r *Report2) ReliabilitySlice {
	if r == nil {
		return ReliabilitySlice{}
	}
	return ReliabilitySlice{
		Endpoints:    r.Endpoints,
		EndpointsAll: r.EndpointsAll,
		Sticky:       r.Sticky,
	}
}

// BuildWorkloadsSlice projects rep into WorkloadsSlice.
func BuildWorkloadsSlice(r *Report2) WorkloadsSlice {
	if r == nil {
		return WorkloadsSlice{}
	}
	return WorkloadsSlice{
		ByDate:          r.ByDate,
		Hours:           r.Hours,
		HoursOfDay:      r.HoursOfDay,
		Workloads:       r.Workloads,
		ClientEndpoints: r.ClientEndpoints,
	}
}

// BuildContextEfficiencySlice projects rep into ContextEfficiencySlice.
func BuildContextEfficiencySlice(r *Report2) ContextEfficiencySlice {
	if r == nil {
		return ContextEfficiencySlice{}
	}
	for i := range r.Compactions {
		r.Compactions[i].EnsureTimeFields()
	}
	return ContextEfficiencySlice{
		Sessions:    r.Sessions,
		Compactions: r.Compactions,
		Tools:       r.Tools,
	}
}

// WriteMacroSlices writes the 5 domain slices into <dir>/macro/*.json atomically (0600).
func WriteMacroSlices(dir string, r *Report2, lang i18n.Lang) error {
	if r == nil {
		return fmt.Errorf("cannot write nil report slices")
	}
	macroDir := filepath.Join(dir, "macro")
	if err := os.MkdirAll(macroDir, 0700); err != nil {
		return fmt.Errorf("mkdir macro dir: %w", err)
	}

	summary := BuildSummarySlice(r, lang)
	if err := writeJSONAtomic(macroDir, "summary.json", summary); err != nil {
		return err
	}

	finance := BuildFinanceSlice(r)
	if err := writeJSONAtomic(macroDir, "finance.json", finance); err != nil {
		return err
	}

	reliability := BuildReliabilitySlice(r)
	if err := writeJSONAtomic(macroDir, "reliability.json", reliability); err != nil {
		return err
	}

	workloads := BuildWorkloadsSlice(r)
	if err := writeJSONAtomic(macroDir, "workloads.json", workloads); err != nil {
		return err
	}

	ctxEff := BuildContextEfficiencySlice(r)
	if err := writeJSONAtomic(macroDir, "context-efficiency.json", ctxEff); err != nil {
		return err
	}

	return nil
}

// EnsureTimeFields ensures TSMS and TSDisplay are populated for CompactionRow (§3.3).
func (c *CompactionRow) EnsureTimeFields() {
	if c.TSMS == 0 && c.TS != "" {
		if t, err := time.Parse(time.RFC3339, c.TS); err == nil {
			c.TSMS = t.UnixMilli()
			c.TSDisplay = t.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
		} else {
			c.TSDisplay = c.TS
		}
	} else if c.TSMS != 0 {
		t := time.UnixMilli(c.TSMS)
		if c.TS == "" {
			c.TS = t.Format(time.RFC3339)
		}
		if c.TSDisplay == "" {
			c.TSDisplay = t.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
		}
	}
}

// MarshalJSON serializes CompactionRow with both epoch ms (ts) and display string (ts_display).
func (c CompactionRow) MarshalJSON() ([]byte, error) {
	c.EnsureTimeFields()
	type Alias struct {
		TSDisplay         string   `json:"ts_display"`
		Summarizes        string   `json:"summarizes,omitempty"`
		ContinuesTo       string   `json:"continues_to,omitempty"`
		TokensIn          int64    `json:"tokens_in"`
		TokensOut         int64    `json:"tokens_out"`
		SwallowedEntities []string `json:"swallowed_entities,omitempty"`
		SurvivedEntities  []string `json:"survived_entities,omitempty"`
	}
	return json.Marshal(struct {
		TS int64 `json:"ts"`
		Alias
	}{
		TS: c.TSMS,
		Alias: Alias{
			TSDisplay:         c.TSDisplay,
			Summarizes:        c.Summarizes,
			ContinuesTo:       c.ContinuesTo,
			TokensIn:          c.TokensIn,
			TokensOut:         c.TokensOut,
			SwallowedEntities: c.SwallowedEntities,
			SurvivedEntities:  c.SurvivedEntities,
		},
	})
}

// UnmarshalJSON deserializes CompactionRow supporting ts as epoch ms or RFC3339 string.
func (c *CompactionRow) UnmarshalJSON(b []byte) error {
	type Alias struct {
		TSDisplay         string   `json:"ts_display"`
		Summarizes        string   `json:"summarizes,omitempty"`
		ContinuesTo       string   `json:"continues_to,omitempty"`
		TokensIn          int64    `json:"tokens_in"`
		TokensOut         int64    `json:"tokens_out"`
		SwallowedEntities []string `json:"swallowed_entities,omitempty"`
		SurvivedEntities  []string `json:"survived_entities,omitempty"`
	}
	var raw struct {
		TS json.RawMessage `json:"ts"`
		Alias
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.TSDisplay = raw.TSDisplay
	c.Summarizes = raw.Summarizes
	c.ContinuesTo = raw.ContinuesTo
	c.TokensIn = raw.TokensIn
	c.TokensOut = raw.TokensOut
	c.SwallowedEntities = raw.SwallowedEntities
	c.SurvivedEntities = raw.SurvivedEntities

	var ms int64
	if err := json.Unmarshal(raw.TS, &ms); err == nil {
		c.TSMS = ms
		t := time.UnixMilli(ms)
		c.TS = t.Format(time.RFC3339)
		if c.TSDisplay == "" {
			c.TSDisplay = t.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(raw.TS, &s); err == nil {
		c.TS = s
		c.EnsureTimeFields()
		return nil
	}
	return nil
}
