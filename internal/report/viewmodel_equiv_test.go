// Ver 2026-09-15, by Opus 5

// The Phase-3 transition guard (§8.3 of the analyze architecture
// redesign): the legacy renderer (Markdown over render_doc.go +
// section_*.go) and the new ViewModel path (MacroMarkdown over
// viewmodel_*.go + the fixed serializer) coexist for one cycle, and this
// test asserts they emit byte-identical Markdown for the same aggregated
// input — string comparison, so no float tolerance applies. When the
// legacy path is deleted, this file goes with it.
package report

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// equivFixture builds a Report2 hand-tuned to walk every branch both
// renderers have: priced and unpriced, low-n folds, per-protocol buckets,
// error/quirk distributions, quota markers (⭐ ‡ †), session long-tail
// fold, compaction chains, tool-waste details, highlights, and the
// self-traffic / reconciliation footnotes.
func equivFixture() *Report2 {
	f := func(v float64) *float64 { return &v }
	rep := &Report2{
		Meta: Meta{
			Format: Format, Inputs: []string{"logs/a.jsonl", "logs/b.jsonl"},
			Records: 123, ParseErrors: 2,
			From: "2026-07-20T08:17:58+08:00", To: "2026-07-24T23:59:59+08:00",
			SlowThreshold: SlowThresholdMS, PercentileMethod: "nearest-rank",
			ReportConfigPath:           "/etc/vmr/report.yaml",
			SelfTrafficExclusionActive: true,
			SelfTrafficExcluded:        3,
			DetailsEnabled:             false,
			QuotaJSONPath:              "/var/lib/vmr/vmr-quota.json",
			QuotaInputOutsideLogDir:    true,
		},
		Overall: fixedRow(""),
		ByModel: []Row{
			withSpeed(fixedRow("coding"), "openai-completions", 2100.5, f(12.5)),
			withSpeed(fixedRow("agent"), "anthropic-messages", 1300.2, nil),
		},
		ByDate: []Row{
			withDate(fixedRow("2026-07-23"), f(9.75)),
			withDate(fixedRow("2026-07-24"), nil),
		},
		HoursOfDay: []HourRow{
			{Hour: 9, TrafficStats: TrafficStats{Requests: 30, Errors: 1, TokensIn: 900_000}},
			{Hour: 14, TrafficStats: TrafficStats{Requests: 60, Errors: 4, TokensIn: 1_500_000}},
			{Hour: 22, TrafficStats: TrafficStats{Requests: 12, Errors: 2, TokensIn: 300_000}},
		},
		EndpointsAll: []EndpointRow{
			{
				Endpoint: "openai-completions:prov1:gpt-x", Attempts: 30, OK: 28, Forwarded: 29, Failed: 2,
				Availability: 0.9333, ErrorRate: 6.7, ErrorClasses: map[string]int{"rate_limit": 1, "timeout": 1},
				Requests: 28, RequestsOK: 25, TokensIn: 400_000, TokensInCached: 300_000, TokensInFresh: 100_000,
				TokensOut: 80_000, TokensReasoning: 5_000, TokensKnown: 25, CacheEfficiency: 0.75,
				TTFTKnown: 25, TTFTMSP50: 300, TTFTMSP95: 900,
				RequestsWithDur: 25, DurMSP50: 1200, DurMSP95: 8000, DurMSMax: 31_000, SlowRequests: 2,
				TokOutPerSec: 55.5, InTokP50: 9000, InTokP95: 30_000, OutTokP50: 2000, OutTokP95: 9000,
				CostEstimate: f(9.75), CostEstimateEst: 0.75, WastedMS: 5000,
			},
			{
				Endpoint: "anthropic-messages:prov2:claude-y", Attempts: 10, OK: 9, Forwarded: 9, Failed: 1,
				Availability: 0.9, ErrorRate: 10, ErrorClasses: map[string]int{"overloaded": 1},
				NormCounts: map[string]int{"think_strip": 3, "soft_block_detected": 1},
				Requests:   9, RequestsOK: 8, TokensIn: 90_000, TokensInCached: 10_000, TokensInFresh: 80_000,
				TokensOut: 20_000, TokensKnown: 8, CacheEfficiency: 0.1111,
				RequestsWithDur: 8, DurMSP50: 2000, DurMSP95: 9000, DurMSMax: 12_000,
				TokOutPerSec: 40.1, InTokP50: 8000, InTokP95: 20_000, OutTokP50: 1500, OutTokP95: 4000,
				CostRateIncomplete: true, WastedMS: 20_000,
			},
			{
				Endpoint: "other-proto:prov3:m3", Attempts: 25, OK: 20, Forwarded: 0, Failed: 5,
				Availability: 0.8, ErrorRate: 20, WastedMS: 1000,
			},
		},
		Providers: []ProviderRow{
			{
				Provider: "prov1", Models: []string{"gpt-x", "gpt-y"}, Requests: 28, RequestsOK: 25, Attempts: 30,
				Failed: 2, ErrorRate: 6.7, ErrorClasses: map[string]int{"rate_limit": 2},
				TokensIn: 400_000, TokensInCached: 300_000, TokensInFresh: 100_000, TokensOut: 80_000,
				TokensKnown: 25, CacheEfficiency: 0.75, DurMSMean: 2500, CostEstimate: f(9.75),
			},
			{
				Provider: "prov2", Models: []string{"claude-y"}, Requests: 9, RequestsOK: 8, Attempts: 10, Failed: 1,
				ErrorRate: 10, TokensIn: 90_000, TokensInCached: 10_000, TokensInFresh: 80_000, TokensOut: 20_000,
				DurMSMean: 4000,
			},
		},
		ProviderQuotas: []ProviderQuotaRow{
			{
				Provider: "prov1", Metric: "tokens", Amount: 50_000_000,
				WindowConsumed: 12_345_678.5, WindowEstimatedPct: 96.2,
				Live: &LiveQuota{Used: 69_450_000, Pct: 138.9, EstimatedPct: 96.2,
					PeriodStart:  time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
					PeriodEndsAt: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)},
				PeriodStart:  time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
				PeriodEndsAt: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), PeriodElapsedPct: 20.5,
			},
			{
				Provider: "prov2", Metric: "requests", Amount: 500, Models: []string{"claude-y"},
				WindowConsumed: 9, WindowNoOverlap: true, LiveConfigChanged: true,
				PeriodStart:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				PeriodEndsAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), PeriodElapsedPct: 77.4,
			},
		},
		ProviderQuotaSkippedAttempts:  4,
		ProviderQuotaSkippedProviders: []string{"provX", "provY", "provZ", "provW"},
		ClientEndpoints: []ClientEndpointRow{
			{ClientKey: "claw-a", Endpoint: "openai-completions:prov1:gpt-x", Requests: 10, TokensIn: 200_000, TokensInFresh: 50_000, TokensInCached: 150_000, TokensOut: 30_000},
			{ClientKey: "claw-a", Endpoint: "anthropic-messages:prov2:claude-y", Requests: 5, TokensIn: 50_000, TokensInFresh: 40_000, TokensInCached: 10_000, TokensOut: 8_000},
			{ClientKey: "claw-b", Endpoint: "openai-completions:prov1:gpt-x", Requests: 3, TokensIn: 20_000, TokensInFresh: 15_000, TokensInCached: 5_000, TokensOut: 2_000},
		},
		Sticky: &StickyEffect{
			Continued: StickyGroup{Requests: 30, TokensKnown: 28, TokensInCached: 300_000, TokensInFresh: 100_000, CacheEfficiency: 0.75},
			Switched:  StickyGroup{Requests: 12, TokensKnown: 10, TokensInCached: 30_000, TokensInFresh: 90_000, CacheEfficiency: 0.25},
			First:     8, Ungrouped: 2,
			ByModel: []StickyModelRow{
				{
					Model: "coding", Protocol: "openai-completions",
					Continued: StickyGroup{Requests: 20, TokensKnown: 20, TokensInCached: 200_000, TokensInFresh: 60_000, CacheEfficiency: 0.77},
					Switched:  StickyGroup{Requests: 8, TokensKnown: 8, TokensInCached: 20_000, TokensInFresh: 60_000, CacheEfficiency: 0.25},
				},
				{
					Model: "agent", Protocol: "anthropic-messages",
					Continued: StickyGroup{Requests: 3, TokensKnown: 1, TokensInCached: 100, TokensInFresh: 900, CacheEfficiency: 0.1},
					Switched:  StickyGroup{Requests: 2, TokensKnown: 1, TokensInCached: 200, TokensInFresh: 800, CacheEfficiency: 0.2},
				},
			},
		},
		Compactions: []CompactionRow{
			{
				TS: "2026-07-23T10:00:00+08:00", Summarizes: "l-abc12345", ContinuesTo: "l-def67890",
				TokensIn: 120_000, TokensOut: 9_000,
				SwallowedEntities: []string{"e1", "e2", "e3", "e4", "e5"},
			},
			{TS: "2026-07-24T11:30:00+08:00", Summarizes: "l-aaa11111"},
		},
		Tools: []ToolShapeRow{
			{
				Shape: "tools:64", Requests: 40, DeclaredBytes: 200_000,
				Declared:           []string{"read", "write", "exec", "search"},
				Calls:              map[string]int{"read": 35, "exec": 12, "write": 3},
				NeverCalled:        []string{"search"},
				SchemaBytesShipped: 8_000_000, DistinctCalled: 3, DeclareUtilization: 0.75, SchemaWasteBytes: 9_000_000,
			},
			{
				Shape: "tools:2", Requests: 5, DeclaredBytes: 4_000,
				Declared: []string{"a", "b"}, Calls: map[string]int{"a": 4, "b": 2},
				SchemaBytesShipped: 20_000, DistinctCalled: 2, DeclareUtilization: 1.0, SchemaWasteBytes: 0,
			},
		},
		ByClient: []ClientRow{
			withClient("claw-a", fixedStats(40, 38, 2, 400_000, 300_000, 90_000), f(7.25)),
			withClient("claw-b", fixedStats(10, 10, 0, 90_000, 10_000, 80_000), f(2.5)),
		},
		Workloads: []WorkloadRow{
			withWorkload("interactive", 40, 38, 2, 400_000, 300_000, 90_000, 38, 0.75, 0.4),
			withWorkload("heartbeat", 10, 10, 0, 90_000, 10_000, 80_000, 10, 0.1111, 0),
		},
		Pricing: &Pricing{Currency: "USD", StandardGeneratedAt: "2026-08-01", ProviderOverrides: 2},
	}

	// Sessions: 25 interactive rows for claw-a (head 20 + folded tail of 5
	// short sessions), plus compaction chains and special rows.
	for i := 0; i < 25; i++ {
		rep.Sessions = append(rep.Sessions, SessionRow{
			ID:    fmt.Sprintf("l-a%08d", i),
			Alias: fmt.Sprintf("s%02d", i+1),
			Title: "fix the parser | attempt <!--2", Class: "interactive", ClientKey: "claw-a",
			Tasks: 2, From: "2026-07-23T02:39:00+08:00", To: "2026-07-23T02:52:00+08:00",
			TrafficStats: fixedStats(4+i%3, 4, i%2, 40_000, 30_000, 10_000),
		})
	}
	// compaction chain of 3: l-c0 <- l-c1 <- l-c2 (walk gives head l-c0)
	rep.Sessions = append(rep.Sessions,
		SessionRow{ID: "l-c0", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-23T09:00:00+08:00", To: "2026-07-23T09:10:00+08:00",
			TrafficStats: fixedStats(3, 3, 0, 30_000, 20_000, 10_000)},
		SessionRow{ID: "l-c1", ContinuedFrom: "l-c0", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-23T10:00:00+08:00", To: "2026-07-23T10:05:00+08:00",
			TrafficStats: fixedStats(2, 2, 0, 20_000, 15_000, 5_000)},
		SessionRow{ID: "l-c2", ContinuedFrom: "l-c1", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-23T11:00:00+08:00", To: "2026-07-23T11:04:00+08:00",
			TrafficStats: fixedStats(2, 2, 0, 20_000, 15_000, 5_000)},
		// a 2-node pair
		SessionRow{ID: "l-d0", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-24T09:00:00+08:00", To: "2026-07-24T09:03:00+08:00",
			TrafficStats: fixedStats(1, 1, 0, 10_000, 8_000, 2_000)},
		SessionRow{ID: "l-d1", ContinuedFrom: "l-d0", Class: "interactive", ClientKey: "claw-b", Tasks: 1,
			From: "2026-07-24T10:00:00+08:00", To: "2026-07-24T10:02:00+08:00",
			TrafficStats: fixedStats(1, 1, 0, 10_000, 8_000, 2_000)},
		// a session with an alias + journey link + errors/fallbacks
		withSessionErrs(SessionRow{ID: "l-e0e0e0e0", Alias: "s26", Title: "short title", Class: "interactive", ClientKey: "claw-a",
			Fallbacks: 1, Tasks: 3,
			From: "2026-07-24T08:00:00+08:00", To: "2026-07-25T09:30:00+08:00",
			TrafficStats: fixedStats(6, 4, 1, 60_000, 40_000, 20_000)}),
		// non-interactive: excluded from §6
		SessionRow{ID: "l-cron", Class: "heartbeat", ClientKey: "claw-a", Tasks: 1,
			TrafficStats: fixedStats(1, 1, 0, 1_000, 0, 1_000)},
	)
	rep.requests = []RequestRow{{Req: "logs/a.jsonl:42", TS: rep.Meta.From}}
	return rep
}

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

// assertByteEqual fails with a line-level context diff around the first
// differing byte — readable enough to spot a whitespace or ordering slip
// between the two renderers.
func assertByteEqual(t *testing.T, want, got string) {
	t.Helper()
	if want == got {
		return
	}
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	lo := strings.LastIndexByte(want[:i], '\n') + 1
	if lo < 0 {
		lo = 0
	}
	hiW := strings.IndexByte(want[i:], '\n')
	if hiW < 0 {
		hiW = len(want)
	} else {
		hiW += i + 1
	}
	hiG := strings.IndexByte(got[i:], '\n')
	if hiG < 0 {
		hiG = len(got)
	} else {
		hiG += i + 1
	}
	t.Fatalf("outputs differ at byte %d:\n--- legacy ---\n%q\n--- vm ---\n%q",
		i, want[lo:min(hiW, len(want))], got[lo:min(hiG, len(got))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// withDisplayZone pins the display timezone for the duration of f.
func withDisplayZone(t *testing.T, f func()) {
	t.Helper()
	orig := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = orig }()
	f()
}

// TestTransition_LegacyMarkdownEqualsVM is THE Phase-3 transition guard:
// both renderers must emit byte-identical Markdown for the same aggregated
// input, in both languages, with and without the cross-product links.
// This is a string comparison — full equality, no float tolerance.
func TestTransition_LegacyMarkdownEqualsVM(t *testing.T) {
	fixture := equivFixture()
	journeyLink := map[string]string{"l-e0e0e0e0": "j-claw-a-1.md", "l-c2": "j-claw-b-9.md"}
	stories := &StoriesLinkInfo{
		Path: "journeys/index.md", JourneyCount: 7,
		FromDisplay: "2026-07-20 08:17:58", ToDisplay: "2026-07-24 23:59:59",
	}
	withDisplayZone(t, func() {
		for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
			for _, tc := range []struct {
				name    string
				stories *StoriesLinkInfo
			}{{"no-stories", nil}, {"with-stories", stories}} {
				t.Run(string(lang)+"/"+tc.name, func(t *testing.T) {
					want := Markdown(fixture, lang, tc.stories, journeyLink)
					got := MacroMarkdown(fixture, lang, tc.stories, journeyLink)
					assertByteEqual(t, want, got)
				})
			}
		}
	})
}

// TestTransition_DetailsEnabledBranch flips §8's capture/on-demand branch
// and exercises the no-report-config / no-quota-source meta lines.
func TestTransition_DetailsEnabledBranch(t *testing.T) {
	fixture := equivFixture()
	fixture.Meta.DetailsEnabled = true
	fixture.Meta.ReportConfigPath = ""
	fixture.Meta.QuotaJSONPath = ""
	fixture.Meta.QuotaInputOutsideLogDir = false
	withDisplayZone(t, func() {
		for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
			assertByteEqual(t, Markdown(fixture, lang, nil, nil), MacroMarkdown(fixture, lang, nil, nil))
		}
	})
}

// TestTransition_DegradedFixture covers the empty/degenerate branches both
// renderers must agree on: no pricing, no tools, no sessions, no
// endpoints, no clients, no compactions — only the overall row.
func TestTransition_DegradedFixture(t *testing.T) {
	rep := &Report2{
		Meta: Meta{Format: Format, Inputs: []string{"only.jsonl"}, Records: 5, SlowThreshold: SlowThresholdMS, PercentileMethod: "nearest-rank"},
	}
	rep.Overall = fixedRow("")
	withDisplayZone(t, func() {
		for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
			assertByteEqual(t, Markdown(rep, lang, nil, nil), MacroMarkdown(rep, lang, nil, nil))
		}
	})
}
