// Ver 2026-09-15, by Opus 5

// Unit tests for the ViewModel types and the fixed serializer: structure
// only — heading levels, table geometry, fold/details wrappers, block
// order. Copy correctness (every string localized and formatted) is the
// builders' job and is pinned by the golden and byte-equivalence tests.
package report

import (
	"strings"
	"testing"

	"vmr/internal/i18n"
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
