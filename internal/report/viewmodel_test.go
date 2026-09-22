// Ver 2026-09-15, by Opus 5

// Unit tests for the ViewModel types and the fixed serializer: structure —
// heading levels, table geometry, fold/details wrappers, block order,
// HeadingVM/NoteVM/ChartVM/FlowVM's exact mermaid/Markdown output — plus a
// couple of targeted lang-following regression guards (most copy
// correctness is pinned by the golden and byte-equivalence tests instead).
package report

import (
	"strings"
	"testing"
	"time"

	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func TestRenderMarkdownStructure(t *testing.T) {
	vm := &MacroReportVM{
		Title: "Report",
		Meta: []BlockVM{
			ParaVM{Text: "meta line\n\n"},
			DetailsVM{Summary: "files", Body: "a.jsonl\n"},
		},
		Sections: []SectionVM{
			{ID: "s1", Title: "One", Blocks: []BlockVM{
				ParaVM{Text: "intro\n\n"},
				func() BlockVM {
					tbl := &TableVM{Title: "**sub**", Headers: []string{"h1", "h2"}}
					tbl.row("a | b", "x")
					tbl.note("> note\n\n")
					return tbl
				}(),
				&TableVM{Fold: "<details><summary>fold 2</summary>\n\n", Headers: []string{"h"}, Rows: [][]string{{"y"}}},
			}},
			{ID: "s2", Title: "Two"},
		},
		Disclaimers: []string{"- method note\n"},
		Footnotes:   []FootnoteVM{{ID: "self-traffic", Text: "- self-traffic off\n"}},
	}
	got := RenderMarkdown(vm)
	want := strings.Join([]string{
		"# Report\n\n",
		"meta line\n\n",
		"<details><summary>files</summary>\n\na.jsonl\n</details>\n\n",
		"## One\n\n",
		"intro\n\n",
		"**sub**\n\n",
		"| h1 | h2 |\n",
		"|---|---|\n",
		"| a \\| b | x |\n",
		"\n",
		"> note\n\n",
		"<details><summary>fold 2</summary>\n\n",
		"| h |\n",
		"|---|\n",
		"| y |\n",
		"\n</details>\n\n",
		"## Two\n\n",
		"- method note\n",
		"- self-traffic off\n",
	}, "")
	if got != want {
		t.Errorf("serializer output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestRenderMarkdownNewBlockTypes pins HeadingVM/NoteVM/ChartVM/FlowVM's
// exact output — the four block types R2-b added so builders stop
// hand-writing Markdown/mermaid syntax into ParaVM.Text.
func TestRenderMarkdownNewBlockTypes(t *testing.T) {
	vm := &MacroReportVM{
		Title: "R",
		Sections: []SectionVM{{ID: "s", Title: "S", Blocks: []BlockVM{
			HeadingVM{Level: 1, Text: "Quota vs. Consumption"},
			HeadingVM{Level: 2, Text: "openai-completions"},
			HeadingVM{Level: 3, Text: "Tier 2 (audit-only)"},
			NoteVM{Text: "**Total shipped** 1 MB"},
			ChartVM{Title: "chart", YLabel: "reqs", Labels: []string{"00", "01"}, Parts: []string{"3", "5"}},
			FlowVM{Nodes: []string{"a", "b", "c"}, Edge: "compacted"},
		}}},
	}
	got := RenderMarkdown(vm)
	want := strings.Join([]string{
		"# R\n\n",
		"## S\n\n",
		"### Quota vs. Consumption\n\n",
		"*openai-completions*\n\n",
		"**Tier 2 (audit-only)**\n\n",
		"> **Total shipped** 1 MB\n\n",
		"```mermaid\nxychart-beta\n    title \"chart\"\n    x-axis [\"00\", \"01\"]\n    y-axis \"reqs\"\n    bar [3, 5]\n```\n\n",
		"```mermaid\nflowchart LR\n    a[\"a\"] -->|compacted| b[\"b\"]\n    b[\"b\"] -->|compacted| c[\"c\"]\n```\n\n",
	}, "")
	if got != want {
		t.Errorf("serializer output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestTableVMRowEscapesAndArity pins the cell treatment the builders get:
// every cell is escaped for table-cell safety (the same treatment the
// legacy mdTable.row applied), and a ragged row is a programmer error.
func TestTableVMRowEscapesAndArity(t *testing.T) {
	tbl := &TableVM{Headers: []string{"a", "b"}}
	tbl.row("pipe | splits", "50%")
	if got := tbl.Rows[0][0]; got != `pipe \| splits` {
		t.Errorf("pipe not escaped: %q", got)
	}
	if got := tbl.Rows[0][1]; got != "50%" {
		t.Errorf("percent mangled: %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("ragged row did not panic")
		}
	}()
	tbl.row("only-one")
}

// TestVMNotesAbsorbsOneLeadingBlank pins vmNotes' contract: the serializer
// already writes the blank line after a table's rows, so exactly one
// leading "\n" of the note stream is absorbed, and an all-blank stream
// becomes no notes at all.
func TestVMNotesAbsorbsOneLeadingBlank(t *testing.T) {
	if got := vmNotes("\n> note\n", "tail\n", "\n"); len(got) != 1 || got[0] != "> note\ntail\n\n" {
		t.Errorf("vmNotes = %#v", got)
	}
	if got := vmNotes("\n"); got != nil {
		t.Errorf("all-blank stream = %#v, want nil", got)
	}
	if got := vmNotes(); got != nil {
		t.Errorf("empty stream = %#v, want nil", got)
	}
}

// TestRenderMarkdownPanicsOnUnknownBlock keeps the serializer's type switch
// exhaustive — a new BlockVM implementation must be rendered, not silently
// dropped.
func TestRenderMarkdownPanicsOnUnknownBlock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("unknown block type did not panic")
		}
	}()
	RenderMarkdown(&MacroReportVM{Sections: []SectionVM{{Title: "x", Blocks: []BlockVM{unknownBlock{}}}}})
}

type unknownBlock struct{}

func (unknownBlock) isBlock() {}

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

func TestMarkdownNamesItsReportConfigSource(t *testing.T) {
	base := &Report2{Meta: Meta{Format: Format, Inputs: []string{"a.jsonl"}}}
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		loaded := *base
		loaded.Meta.ReportConfigPath = "/etc/vmr/report.yaml"
		if md := MacroMarkdown(&loaded, lang, nil, nil); !strings.Contains(md, "/etc/vmr/report.yaml") {
			t.Errorf("lang=%v: loaded report.yaml path missing from the meta header", lang)
		}
		absent := *base
		md := MacroMarkdown(&absent, lang, nil, nil)
		if strings.Contains(md, "report.yaml)") || !strings.Contains(md, i18n.Doc(lang).MetaReportConfig("")) {
			t.Errorf("lang=%v: 'no report.yaml loaded' must still say so explicitly:\n%s", lang, md)
		}
	}
}

// TestMacroMarkdownFindingsFollowLang guards the report-side half of R1's
// "text rot" risk: the JSON products are frozen to English by design
// (buildFindingsForJSON), but vmEfficiencySection deliberately does NOT
// reuse rep.Efficiency — it calls buildFindings(rep, lang) fresh every
// render specifically so Markdown keeps following -lang. That fresh-build
// path is cheap to silently regress (e.g. a future edit reusing the
// English rep.Efficiency slice) with nothing else catching it, since nothing
// else renders §7 in a non-English language. Mirrors journey's
// TestJourneySummaryIsLangInvariant, one package over.
func TestMacroMarkdownFindingsFollowLang(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, heartbeatDreamDiaryTiedRecords())
	rep, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}

	mdEN := MacroMarkdown(rep, i18n.EN, nil, nil)
	mdZH := MacroMarkdown(rep, i18n.ZH, nil, nil)
	if mdEN == mdZH {
		t.Fatal("EN and ZH renders are byte-identical — §7 findings are not following -lang")
	}
	if !strings.Contains(mdEN, "Scheduled-task redundancy") {
		t.Errorf("EN render missing the English cron-redundancy finding title:\n%s", mdEN)
	}
	if strings.Contains(mdZH, "Scheduled-task redundancy") {
		t.Error("ZH render still carries the English cron-redundancy finding title — buildFindings is not being called with lang=zh")
	}
	if !strings.Contains(mdZH, "定时任务冗余") {
		t.Errorf("ZH render missing the Chinese cron-redundancy finding title:\n%s", mdZH)
	}
}

// TestVmCompactionChainBlocks_FlowVM pins vmCompactionChainBlocks' two
// branches: a ≥3-node chain (≥2 compaction hops) becomes a FlowVM, a
// 2-node chain stays an inline ParaVM note. No real corpus in this repo's
// test fixtures has a chain this long (real-corpus verification during
// Phase 3 Step 3.4 found zero matches in two days of production audit
// logs), so this is the only test exercising renderFlow's mermaid output.
func TestVmCompactionChainBlocks_FlowVM(t *testing.T) {
	t.Run("three node chain becomes FlowVM", func(t *testing.T) {
		rep := &Report2{Sessions: []SessionRow{
			{ID: "l-aaa"},
			{ID: "l-bbb", ContinuedFrom: "l-aaa"},
			{ID: "l-ccc", ContinuedFrom: "l-bbb"},
		}}
		blocks := vmCompactionChainBlocks(rep, i18n.EN)
		if len(blocks) != 1 {
			t.Fatalf("got %d blocks, want 1", len(blocks))
		}
		flow, ok := blocks[0].(FlowVM)
		if !ok {
			t.Fatalf("block is %T, want FlowVM", blocks[0])
		}
		wantNodes := []string{"l-aaa", "l-bbb", "l-ccc"}
		if len(flow.Nodes) != len(wantNodes) {
			t.Fatalf("Nodes = %v, want %v", flow.Nodes, wantNodes)
		}
		for i, n := range wantNodes {
			if flow.Nodes[i] != n {
				t.Errorf("Nodes[%d] = %q, want %q", i, flow.Nodes[i], n)
			}
		}
		if flow.Edge != "compacted" {
			t.Errorf("Edge = %q, want %q", flow.Edge, "compacted")
		}
		var b strings.Builder
		renderBlock(&b, flow)
		want := "```mermaid\nflowchart LR\n" +
			"    l-aaa[\"l-aaa\"] -->|compacted| l-bbb[\"l-bbb\"]\n" +
			"    l-bbb[\"l-bbb\"] -->|compacted| l-ccc[\"l-ccc\"]\n" +
			"```\n\n"
		if got := b.String(); got != want {
			t.Errorf("renderBlock(FlowVM) = %q, want %q", got, want)
		}
	})

	t.Run("two node chain stays a ParaVM note", func(t *testing.T) {
		rep := &Report2{Sessions: []SessionRow{
			{ID: "l-aaa"},
			{ID: "l-bbb", ContinuedFrom: "l-aaa"},
		}}
		blocks := vmCompactionChainBlocks(rep, i18n.EN)
		if len(blocks) != 1 {
			t.Fatalf("got %d blocks, want 1", len(blocks))
		}
		if _, ok := blocks[0].(ParaVM); !ok {
			t.Fatalf("block is %T, want ParaVM", blocks[0])
		}
	})
}
