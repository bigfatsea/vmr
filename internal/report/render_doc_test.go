// Ver 2026-08-05, by Sonnet 5
package report

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// TestSummaryInteractiveShare locks P-07's computation: total - interactive
// is exactly the scheduled/compaction overhead, and an empty Workloads
// (no buckets at all) reports -1 so the caller skips the note.
func TestSummaryInteractiveShare(t *testing.T) {
	rep := &Report2{
		Workloads: []WorkloadRow{
			{Class: "interactive", TrafficStats: TrafficStats{Requests: 40}},
			{Class: "heartbeat", TrafficStats: TrafficStats{Requests: 5}},
			{Class: "compaction", TrafficStats: TrafficStats{Requests: 3}},
		},
	}
	if n := summaryInteractiveShare(rep); n != 40 {
		t.Errorf("interactive share = %d, want 40", n)
	}
	if n := summaryInteractiveShare(&Report2{}); n != -1 {
		t.Errorf("empty Workloads = %d, want -1", n)
	}
	if n := summaryInteractiveShare(nil); n != -1 {
		t.Errorf("nil Report2 = %d, want -1", n)
	}
}

// TestSummaryRendersInteractiveNote drives the note through renderSummary:
// a report whose Workloads mix interactive and scheduled traffic must emit
// the interactive-share line after the summary table; one with no
// Workloads at all must not (avoid a "0/0" or "N/0" footnote on a report
// whose records never reached workload bucketing).
func TestSummaryRendersInteractiveNote(t *testing.T) {
	mk := func() *Report2 {
		return &Report2{
			Workloads: []WorkloadRow{
				{Class: "interactive", TrafficStats: TrafficStats{Requests: 40}},
				{Class: "heartbeat", TrafficStats: TrafficStats{Requests: 10}},
			},
		}
	}
	var b strings.Builder
	renderSummary(func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }, mk(), Row{TrafficStats: TrafficStats{Requests: 50}}, i18n.EN)
	out := b.String()
	if !strings.Contains(out, "interactive workload accounts for 80.0%") {
		t.Errorf("EN note missing: %q", out)
	}
	if !strings.Contains(out, "(40/50)") {
		t.Errorf("EN note missing raw counts: %q", out)
	}

	b.Reset()
	renderSummary(func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }, &Report2{}, Row{TrafficStats: TrafficStats{Requests: 10}}, i18n.ZH)
	if strings.Contains(b.String(), "interactive") || strings.Contains(b.String(), "工作负载占") {
		t.Errorf("empty Workloads must skip the note, got: %q", b.String())
	}
}

// TestFmtDisplayFullConvertsToDisplayZone proves fmtDisplayFull (used by
// Markdown's header/appendix "period" lines via rep.Meta.From/rep.Meta.To,
// and by requests.go's session/task headers via RequestRow.TS) actually
// converts through fmtutil.DisplayZone rather than reading the input
// timestamp's own embedded offset — the behavior change from the old
// cut(rep.Meta.From, 19) truncation this replaced. The input below carries
// a +08:00 offset; DisplayZone is overridden to a different, known
// -05:00 zone, so a correct conversion and a "just truncate the string"
// bug produce visibly different wall-clock output.
func TestFmtDisplayFullConvertsToDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.FixedZone("TEST-05:00", -5*3600)
	defer func() { fmtutil.DisplayZone = origZone }()

	// 2026-07-24T08:17:58+08:00 is 2026-07-24T00:17:58 UTC, which is
	// 2026-07-23T19:17:58 in the -05:00 TEST zone above.
	const in = "2026-07-24T08:17:58+08:00"
	const want = "2026-07-23 19:17:58"

	got := fmtDisplayFull(in)
	if got != want {
		t.Errorf("fmtDisplayFull(%q) = %q, want %q (DisplayZone conversion not applied)", in, got, want)
	}

	// Sanity: the old cut(rep.Meta.From, 19) behavior would have produced
	// this instead — assert we're NOT seeing it, to make the "still just
	// truncating" regression explicit rather than implicit in the want above.
	if oldStyle := cut(in, 19); got == oldStyle {
		t.Errorf("fmtDisplayFull(%q) = %q matches the old cut()-truncation output %q; DisplayZone conversion appears to be a no-op", in, got, oldStyle)
	}
}

// TestFmtDisplayFullUsesSpaceSeparator proves the other half of the
// behavior change from the old cut()-based rendering: a space between date
// and time ("2026-07-24 00:17:58"), not RFC3339's "T" ("2026-07-24T00:17:58").
// Uses UTC input/DisplayZone so the assertion is purely about the
// separator, independent of the conversion proven above.
func TestFmtDisplayFullUsesSpaceSeparator(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	const in = "2026-07-24T00:17:58Z"
	const want = "2026-07-24 00:17:58"

	got := fmtDisplayFull(in)
	if got != want {
		t.Errorf("fmtDisplayFull(%q) = %q, want %q (space separator, not RFC3339 T)", in, got, want)
	}
}

// The report is the durable artifact; stderr is not. A run that never found
// its report.yaml (wrong cwd, stale -report-config) otherwise looks exactly
// like a run that was never meant to have one — and the difference matters,
// because self-traffic exclusion silently fails open in the first case.
func TestMarkdownNamesItsReportConfigSource(t *testing.T) {
	base := &Report2{Meta: Meta{Format: Format, Inputs: []string{"a.jsonl"}}}
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		loaded := *base
		loaded.Meta.ReportConfigPath = "/etc/vmr/report.yaml"
		if md := Markdown(&loaded, lang, nil, nil); !strings.Contains(md, "/etc/vmr/report.yaml") {
			t.Errorf("lang=%v: loaded report.yaml path missing from the meta header", lang)
		}
		absent := *base
		md := Markdown(&absent, lang, nil, nil)
		if strings.Contains(md, "report.yaml)") || !strings.Contains(md, i18n.Doc(lang).MetaReportConfig("")) {
			t.Errorf("lang=%v: 'no report.yaml loaded' must still say so explicitly:\n%s", lang, md)
		}
	}
}
