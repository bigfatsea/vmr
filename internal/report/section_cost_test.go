// Ver 2026-08-12, by Sonnet 5
package report

import (
	"fmt"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

func costPtr(v float64) *float64 { return &v }

// TestRenderCostEstimate_ByDateTable pins the fix for a finding from the
// 2026-08-12 review (VMR_项目全面Review报告 B3): §2 成本估算 had no per-day
// cost breakdown because ByDate rows never got CostEstimate populated (see
// aggregate_test.go's TestPricingAccumulatesToEndpointAndClient for the
// aggregation-level pin). This locks in the rendering half: once ByDate
// carries CostEstimate, the "by date" table must actually appear.
func TestRenderCostEstimate_ByDateTable(t *testing.T) {
	rep := &Report2{
		Pricing: &Pricing{Currency: "USD", StandardGeneratedAt: "2026-07-20"},
		ByDate: []Row{
			{Date: "2026-07-24", TrafficStats: TrafficStats{TokensInFresh: 1000, TokensOut: 500}, CostEstimate: costPtr(1.2345)},
		},
	}
	var buf strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&buf, format, args...) }
	renderCostEstimate(w, rep, i18n.EN)
	out := buf.String()
	if !strings.Contains(out, "Estimated Cost by Date") {
		t.Errorf("output missing the by-date cost table title:\n%s", out)
	}
	if !strings.Contains(out, "2026-07-24") || !strings.Contains(out, "1.2345") {
		t.Errorf("output missing the by-date row content:\n%s", out)
	}
}

// TestRenderCostEstimate_NoDateData_TableAbsent is the negative case: when
// no ByDate row resolved a price, the by-date table must not render at all
// (same "only render what has data" rule as the model/endpoint/client
// tables it sits next to).
func TestRenderCostEstimate_NoDateData_TableAbsent(t *testing.T) {
	rep := &Report2{
		Pricing: &Pricing{Currency: "USD"},
		ByDate:  []Row{{Date: "2026-07-24"}}, // no CostEstimate
	}
	var buf strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&buf, format, args...) }
	renderCostEstimate(w, rep, i18n.EN)
	out := buf.String()
	if strings.Contains(out, "Estimated Cost by Date") {
		t.Errorf("by-date table should be absent when no ByDate row has a CostEstimate:\n%s", out)
	}
}
