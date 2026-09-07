// Ver 2026-09-15, by Opus 5

// The VM-structure golden (§9 of the analyze architecture redesign): the
// builders' output is compared as a STRUCTURED view model — JSON-serialized
// for the golden — not as a final string, so a diff points at the exact
// field that drifted and unrelated serializer changes don't break it. The
// serializer keeps its own small end-to-end smoke (viewmodel_test.go), and
// legacy-path byte equivalence is pinned separately (viewmodel_equiv_test.go).
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// fixedRow is a Row with every rendered field populated; identity fields
// are overwritten by the with* helpers.
func fixedRow(id string) Row {
	r := Row{Date: id, Model: id, Protocol: "openai-completions"}
	r.TrafficStats = fixedStats(50, 45, 3, 500_000, 350_000, 120_000)
	r.Canceled = 1
	r.Truncated = 1
	r.Fallbacks = 3
	r.FallbackRecovered = 2
	r.FallbackFailed = 1
	r.SuccessRate = 0.9
	r.CacheHitRate = 0.7
	r.ReasoningShare = 0.05
	r.TokensKnown = 45
	r.CacheEfficiency = 0.72
	r.RequestsWithDur = 44
	r.DurMSP50 = 1500
	r.DurMSP95 = 9000
	r.TTFTKnown = 44
	r.TTFTMSP50 = 320
	r.TTFTMSP95 = 950
	r.DurMSMax = 35_000
	r.SlowRequests = 2
	r.TokOutPerSec = 50.25
	r.RoleChars = map[string]int64{"user": 12_000, "assistant": 48_000, "tool": 140_000}
	r.RoleTokens = map[string]int64{"user": 3_000, "assistant": 12_000, "tool": 35_000}
	return r
}

// fixedStats fills the TrafficStats core: requests/ok/errors, token split.
func fixedStats(reqs, ok, errs int, in, cached, out int64) TrafficStats {
	return TrafficStats{
		Requests: reqs, OK: ok, Errors: errs,
		TokensIn: in, TokensInCached: cached, TokensInFresh: in - cached, TokensOut: out,
	}
}

func withSpeed(r Row, proto string, tokPerSec float64, cost *float64) Row {
	r.Protocol = proto
	r.TokOutPerSec = tokPerSec
	r.CostEstimate = cost
	return r
}

func withWorkload(class string, reqs, ok, errs int, in, cached, out int64, known int, cacheEff, toolRate float64) WorkloadRow {
	w := WorkloadRow{Class: class}
	w.TrafficStats = fixedStats(reqs, ok, errs, in, cached, out)
	w.TokensKnown = known
	w.CacheEfficiency = cacheEff
	w.ToolCallRate = toolRate
	return w
}

func withSessionErrs(s SessionRow) SessionRow {
	s.Errors = 1
	return s
}

func withDate(r Row, cost *float64) Row {
	r.CostEstimate = cost
	return r
}

func withClient(key string, stats TrafficStats, cost *float64) ClientRow {
	c := ClientRow{ClientKey: key}
	c.TrafficStats = stats
	c.SuccessRate = 0.9
	c.InTokP50 = 9000
	c.InTokP95 = 31_000
	c.OutTokP50 = 2000
	c.OutTokP95 = 9100
	c.CostEstimate = cost
	return c
}

// goldenFixture is a compact but branch-rich input: enough to exercise
// paragraphs, tables with conditional notes, a folded table, a details
// block, mermaid, footnotes, and both languages' full string sets —
// without the volume that would make the golden unreadable.
func goldenFixture() *Report2 {
	f := func(v float64) *float64 { return &v }
	rep := &Report2{
		Meta: Meta{
			Format: Format, Inputs: []string{"a.jsonl", "b.jsonl"},
			Records: 7, ParseErrors: 1,
			From: "2026-07-23T02:39:00+08:00", To: "2026-07-24T10:00:00+08:00",
			SlowThreshold: SlowThresholdMS, PercentileMethod: "nearest-rank",
			ReportConfigPath:           "/etc/vmr/report.yaml",
			SelfTrafficExclusionActive: true,
			SelfTrafficExcluded:        2,
			DetailsEnabled:             false,
		},
		Overall: fixedRow(""),
		ByModel: []Row{withSpeed(fixedRow("coding"), "openai-completions", 100.5, f(3.25))},
		ByDate:  []Row{withDate(fixedRow("2026-07-23"), f(3.25))},
		HoursOfDay: []HourRow{
			{Hour: 9, TrafficStats: TrafficStats{Requests: 5, Errors: 1, TokensIn: 90_000}},
			{Hour: 14, TrafficStats: TrafficStats{Requests: 2, Errors: 0, TokensIn: 30_000}},
		},
		EndpointsAll: []EndpointRow{
			{
				Endpoint: "openai-completions:prov1:gpt-x", Attempts: 30, OK: 28, Forwarded: 29, Failed: 2,
				Availability: 0.9333, ErrorRate: 6.7, ErrorClasses: map[string]int{"rate_limit": 2},
				Requests: 28, RequestsOK: 25, TokensIn: 400_000, TokensInCached: 300_000, TokensInFresh: 100_000,
				TokensOut: 80_000, TokensKnown: 25, CacheEfficiency: 0.75,
				TTFTKnown: 25, TTFTMSP50: 300, TTFTMSP95: 900,
				RequestsWithDur: 25, DurMSP50: 1200, DurMSP95: 8000, DurMSMax: 31_000, SlowRequests: 2,
				TokOutPerSec: 55.5, InTokP50: 9000, InTokP95: 30_000, OutTokP50: 2000, OutTokP95: 9000,
				CostEstimate: f(3.25), WastedMS: 5000,
			},
			{
				Endpoint: "anthropic-messages:prov2:claude-y", Attempts: 4, OK: 4, Forwarded: 4, Failed: 0,
				Availability: 1.0, NormCounts: map[string]int{"think_strip": 2},
				Requests: 4, RequestsOK: 4, TokensIn: 40_000, TokensInCached: 10_000, TokensInFresh: 30_000,
				TokensOut: 8_000, TokensKnown: 4, CacheEfficiency: 0.25,
				RequestsWithDur: 4, DurMSP50: 2000, DurMSP95: 3000, TokOutPerSec: 40.1, WastedMS: 0,
			},
		},
		Providers: []ProviderRow{{
			Provider: "prov1", Models: []string{"gpt-x"}, Requests: 28, RequestsOK: 25, Attempts: 30,
			Failed: 2, ErrorRate: 6.7, ErrorClasses: map[string]int{"rate_limit": 2},
			TokensIn: 400_000, TokensInCached: 300_000, TokensInFresh: 100_000, TokensOut: 80_000,
			TokensKnown: 25, CacheEfficiency: 0.75, DurMSMean: 2500, CostEstimate: f(3.25),
		}},
		ProviderQuotas: []ProviderQuotaRow{{
			Provider: "prov1", Metric: "tokens", Amount: 50_000_000,
			WindowConsumed: 123_456,
			Live: &LiveQuota{Used: 69_450_000, Pct: 138.9, EstimatedPct: 20.0,
				PeriodStart:  time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
				PeriodEndsAt: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)},
			PeriodStart:  time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
			PeriodEndsAt: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), PeriodElapsedPct: 20.5,
		}},
		ClientEndpoints: []ClientEndpointRow{
			{ClientKey: "claw-a", Endpoint: "openai-completions:prov1:gpt-x", Requests: 10, TokensIn: 200_000, TokensInFresh: 50_000, TokensInCached: 150_000, TokensOut: 30_000},
		},
		Sticky: &StickyEffect{
			Continued: StickyGroup{Requests: 30, TokensKnown: 28, TokensInCached: 300_000, TokensInFresh: 100_000, CacheEfficiency: 0.75},
			Switched:  StickyGroup{Requests: 12, TokensKnown: 10, TokensInCached: 30_000, TokensInFresh: 90_000, CacheEfficiency: 0.25},
			First:     8, Ungrouped: 2,
		},
		Compactions: []CompactionRow{{
			TS: "2026-07-23T10:00:00+08:00", Summarizes: "l-abc12345", ContinuesTo: "l-def67890",
			TokensIn: 120_000, TokensOut: 9_000,
			SwallowedEntities: []string{"e1", "e2", "e3", "e4"},
		}},
		Tools: []ToolShapeRow{{
			Shape: "tools:4", Requests: 6, DeclaredBytes: 8_000,
			Declared:           []string{"read", "write", "exec", "search"},
			Calls:              map[string]int{"read": 5, "exec": 2},
			NeverCalled:        []string{"search", "write"},
			SchemaBytesShipped: 48_000, DistinctCalled: 2, DeclareUtilization: 0.5, SchemaWasteBytes: 24_000,
		}},
		ByClient: []ClientRow{withClient("claw-a", fixedStats(7, 7, 0, 300_000, 200_000, 100_000), f(3.25))},
		Workloads: []WorkloadRow{
			withWorkload("interactive", 5, 5, 0, 200_000, 150_000, 50_000, 5, 0.75, 0.4),
			withWorkload("heartbeat", 2, 2, 0, 90_000, 10_000, 80_000, 2, 0.1111, 0),
		},
		Pricing: &Pricing{Currency: "USD", StandardGeneratedAt: "2026-08-01", ProviderOverrides: 1},
	}
	// 22 interactive sessions for claw-a: head 20 inline + folded tail of 2.
	for i := 0; i < 22; i++ {
		rep.Sessions = append(rep.Sessions, SessionRow{
			ID:    fmt.Sprintf("l-a%08d", i),
			Alias: fmt.Sprintf("s%02d", i+1),
			Title: "morning routine", Class: "interactive", ClientKey: "claw-a",
			Tasks: 1, From: "2026-07-23T02:39:00+08:00", To: "2026-07-23T02:52:00+08:00",
			TrafficStats: fixedStats(3, 3, 0, 40_000, 30_000, 10_000),
		})
	}
	// a 2-node compaction pair + an aliased session with a journey link
	rep.Sessions = append(rep.Sessions,
		SessionRow{ID: "l-d0", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-24T09:00:00+08:00", To: "2026-07-24T09:03:00+08:00",
			TrafficStats: fixedStats(1, 1, 0, 10_000, 8_000, 2_000)},
		SessionRow{ID: "l-d1", ContinuedFrom: "l-d0", Alias: "s23", Title: "follow-up <!-- ok", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-24T10:00:00+08:00", To: "2026-07-24T10:02:00+08:00",
			TrafficStats: fixedStats(1, 1, 0, 10_000, 8_000, 2_000)},
	)
	rep.requests = []RequestRow{{Req: "a.jsonl:3", TS: 1753318800000}}
	return rep
}

// vmToJSON is the golden's serialization: field-stable, indented, no HTML
// escaping (the VM carries Markdown that would otherwise get mangled).
func vmToJSON(vm *MacroReportVM) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(vm); err != nil {
		panic(err)
	}
	return buf.String()
}

// goldenVMLines renders the golden as one comparable string per language.
// To regenerate after an intentional VM shape change, run:
//
//	UPDATE_VM_GOLDEN=1 go test ./internal/report/ -run TestGoldenVMStructure
//
// and paste the printed JSON into the two constants below.
func TestGoldenVMStructure(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	fixture := goldenFixture()
	journeyLink := map[string]string{"l-d1": "j-claw-b-1.md"}
	stories := &JourneysLinkInfo{Path: "journeys/index.md", JourneyCount: 2,
		FromDisplay: "2026-07-23 02:39:00", ToDisplay: "2026-07-24 10:00:00"}

	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		got := vmToJSON(BuildMacroReportVM(fixture, lang, stories, journeyLink))
		var want string
		switch lang {
		case i18n.EN:
			want = goldenVMEN
		case i18n.ZH:
			want = goldenVMZH
		}
		if want == "" {
			if dir := os.Getenv("UPDATE_VM_GOLDEN"); dir != "" {
				p := filepath.Join(dir, "vm_golden_"+strings.ToLower(lang.String())+".json")
				if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote golden for %v to %s", lang, p)
			}
			continue
		}
		if got != want {
			t.Errorf("golden mismatch for %v:\n%s", lang, firstDiff(want, got))
		}
	}
}

// firstDiff returns a line-level context window around the first differing
// line — the readable-diff property the VM golden exists for.
func firstDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := len(wl)
	if len(gl) < n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			lo := i - 3
			if lo < 0 {
				lo = 0
			}
			hi := i + 4
			if hi > n {
				hi = n
			}
			var b strings.Builder
			fmt.Fprintf(&b, "first diff at line %d:\n", i+1)
			for j := lo; j < hi; j++ {
				marker := " "
				if j == i {
					marker = ">"
				}
				if j < len(wl) {
					fmt.Fprintf(&b, "%s want| %s\n", marker, wl[j])
				}
				if j < len(gl) {
					fmt.Fprintf(&b, "%s  got| %s\n", marker, gl[j])
				}
			}
			return b.String()
		}
	}
	if len(wl) != len(gl) {
		return fmt.Sprintf("line counts differ: want %d, got %d", len(wl), len(gl))
	}
	return "no line diff found but strings differ"
}
