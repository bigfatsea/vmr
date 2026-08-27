// Ver 2026-07-28 21:05, by Opus 5
package report

import (
	"fmt"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

func f64(v float64) *float64 { return &v }

func TestEndpointValueDerivesUnitCosts(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{{
		Endpoint: "openai-completions:p1:m1", RequestsOK: 10, TokensOut: 500_000,
		CostEstimate: f64(2.5), Failed: 2, WastedMS: 3000, Availability: 0.8,
	}}}
	rows := endpointValueRows(rep)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	// 2.5 for 500k output tokens = 5.0 per 1M
	if rows[0].costPer1MOut != 5.0 {
		t.Errorf("costPer1MOut = %v, want 5.0", rows[0].costPer1MOut)
	}
	if rows[0].costPerReq != 0.25 {
		t.Errorf("costPerReq = %v, want 0.25", rows[0].costPerReq)
	}
	if !rows[0].hasCost {
		t.Error("hasCost = false, want true")
	}
}

// The section's whole reason to exist: cheapest per unit of output leads,
// so "which of these is actually the better deal" is the first row, not a
// number the reader has to compute across two other tables.
func TestEndpointValueSortsCheapestPerOutputFirst(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "expensive", RequestsOK: 1, TokensOut: 1_000_000, CostEstimate: f64(10)},
		{Endpoint: "cheap", RequestsOK: 1, TokensOut: 1_000_000, CostEstimate: f64(2)},
		{Endpoint: "unpriced", RequestsOK: 1, TokensOut: 1_000_000},
	}}
	rows := endpointValueRows(rep)
	if rows[0].endpoint != "cheap" || rows[1].endpoint != "expensive" {
		t.Errorf("order = %s, %s; want cheap, expensive", rows[0].endpoint, rows[1].endpoint)
	}
	// Unpriced rows can't join the comparison, so they sink below it rather
	// than sorting as if they were free.
	if rows[2].endpoint != "unpriced" {
		t.Errorf("unpriced row = %s, want it last", rows[2].endpoint)
	}
}

// An endpoint with no cost figure must render "-", never 0.0000 — a zero
// there reads as "this one is free".
func TestEndpointValueUnpricedRendersDashNotZero(t *testing.T) {
	rep := &Report2{
		Pricing:      &Pricing{Currency: "CNY"},
		EndpointsAll: []EndpointRow{{Endpoint: "openai-completions:p1:m1", RequestsOK: 3, TokensOut: 100}},
	}
	var b strings.Builder
	renderEndpointValue(func(f string, a ...any) { b.WriteString(fmt.Sprintf(f, a...)) }, rep, i18n.EN)
	out := b.String()
	if !strings.Contains(out, "| - | - |") {
		t.Errorf("unpriced endpoint must render dashes:\n%s", out)
	}
	if strings.Contains(out, "0.0000") {
		t.Errorf("unpriced endpoint rendered a zero cost:\n%s", out)
	}
}

// Endpoints that only ever failed still belong here — their wasted time is
// the point — but one that neither served nor failed has nothing to say.
func TestEndpointValueSkipsEndpointsWithNothingToReport(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "never-used"},
		{Endpoint: "only-failed", Failed: 3, WastedMS: 900},
	}}
	rows := endpointValueRows(rep)
	if len(rows) != 1 || rows[0].endpoint != "only-failed" {
		t.Errorf("rows = %+v, want only the failing endpoint", rows)
	}
}

// Availability is a 0..1 fraction while ErrorRate is 0..100 (metrics.go);
// rendering the former with the latter's convention silently shows 0.8%
// where 80% was meant. Caught in review of this very section's output.
func TestEndpointValueRendersAvailabilityAsPercent(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:p1:m1", RequestsOK: 4, TokensOut: 100, Availability: 0.8, Failed: 1},
	}}
	var b strings.Builder
	renderEndpointValue(func(f string, a ...any) { b.WriteString(fmt.Sprintf(f, a...)) }, rep, i18n.EN)
	if !strings.Contains(b.String(), "80.0%") {
		t.Errorf("availability 0.8 must render as 80.0%%:\n%s", b.String())
	}
}
