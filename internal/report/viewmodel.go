// Ver 2026-09-15, by Opus 5

// The report side's ViewModel layer (D3/D4 of the analyze architecture
// redesign): every section's business formatting, confidence marking, i18n
// lookup and typesetting decision happens in the viewmodel_*.go builders,
// which produce this flat, fully localized, memory-only structure; the
// fixed serializer below turns it into Markdown. No template engine — the
// VM's shape is fixed, so serialization is a deterministic function of it.
//
// Field set vs the design doc's §5.2 sketch, and why the deviations:
//   - Highlights live in §0's blocks, not on MacroReportVM: the old
//     document renders them after the summary table, and byte equivalence
//     with the legacy path is the transition test's assertion.
//   - SectionVM carries an ordered Blocks list rather than separate
//     Intro/Tables fields: the current sections interleave paragraphs,
//     group headings, charts and several tables, and the fixed serializer
//     must reproduce that order exactly.
//   - TableVM drops Aligns: every current table renders as plain
//     left-aligned pipes, so an alignment field would be passthrough with
//     no formatting decision behind it (the §10 benefit criterion).
//
// The VM is memory-only (D12) — it is never persisted; the JSON slices are
// the machine-readable contract, this is the Markdown-only projection.
package report

import (
	"fmt"
	"strings"

	"vmr/internal/reqdetail"
)

// MacroReportVM is vmr-report.md's whole content, fully localized and
// formatted. Render it with RenderMarkdown.
type MacroReportVM struct {
	// Title is the document H1, already localized.
	Title string
	// Meta holds the header blocks between the H1 and the first section:
	// the data-source line (timestamp/range included), the report-config
	// line, the collapsible input list, the details link and any cross-
	// product links. All composed by the builder.
	Meta []BlockVM
	// Sections are §0..appendix in render order. The last one (appendix)
	// normally has no blocks of its own — its body is Disclaimers and
	// Footnotes below, which the serializer emits after every section.
	Sections []SectionVM
	// Disclaimers are the unconditional closing lines (method notes,
	// metric bases), each fully composed and written verbatim.
	Disclaimers []string
	// Footnotes are the data-conditional closing lines, filtered by this
	// run's actual data (like TableVM.Notes) and written verbatim.
	Footnotes []FootnoteVM
}

// FootnoteVM is one conditional closing line. ID is a stable, language-
// independent identifier (e.g. "self-traffic"); Text is the composed,
// localized line.
type FootnoteVM struct {
	ID   string
	Text string
}

// SectionVM is one "##" section. ID is a stable language-independent
// anchor identity; Title is localized; Blocks are the ordered body.
type SectionVM struct {
	ID     string
	Title  string
	Blocks []BlockVM
}

// BlockVM is one element of a section body. The set is deliberately small:
// paragraph, table, collapsible block. Everything else (mermaid charts,
// blockquote notes, group headings) is a Para whose text the builder
// composed — typesetting decisions live in the builder (D4).
type BlockVM interface{ isBlock() }

// ParaVM is a verbatim Markdown fragment: prose, a group heading, a
// blockquote, a mermaid chart. Text is written as-is — leading/trailing
// blank lines included — so the builder owns all whitespace.
type ParaVM struct{ Text string }

func (ParaVM) isBlock() {}

// DetailsVM is a collapsible <details> block. Body is the inner content
// including its trailing whitespace; the serializer wraps it.
type DetailsVM struct {
	Summary string
	Body    string
}

func (DetailsVM) isBlock() {}

// TableVM is one Markdown table plus its attached notes (§5.2). Headers,
// Rows and Notes are already localized, formatted and escaped; Title is
// the sub-heading line rendered above the table (empty for none); Fold is
// the <details> summary line that collapses the table (empty for inline).
type TableVM struct {
	Title   string
	Fold    string
	Headers []string
	Rows    [][]string
	Notes   []string
}

func (*TableVM) isBlock() {}

// row appends one data row, escaping each cell for table-cell safety —
// the same treatment the legacy mdTable.row applied. A cell/headers
// mismatch is a programmer error in a builder, not malformed input, so it
// panics immediately rather than emitting a ragged table.
func (t *TableVM) row(cells ...string) {
	if len(cells) != len(t.Headers) {
		panic(fmt.Sprintf("TableVM: row has %d cells, header has %d", len(cells), len(t.Headers)))
	}
	esc := make([]string, len(cells))
	for i, c := range cells {
		esc[i] = reqdetail.EscapeCell(c)
	}
	t.Rows = append(t.Rows, esc)
}

// note appends one pre-composed note line (verbatim, whitespace included).
func (t *TableVM) note(s string) {
	t.Notes = append(t.Notes, s)
}

// RenderMarkdown serializes a MacroReportVM to Markdown. Structure only:
// heading levels, table geometry, the <details> wrapper and block order —
// not one word of copy. The output order is: H1 → Meta blocks → Sections
// (## title + blocks) → Disclaimers → Footnotes.
func RenderMarkdown(vm *MacroReportVM) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", vm.Title)
	for _, blk := range vm.Meta {
		renderBlock(&b, blk)
	}
	for _, s := range vm.Sections {
		fmt.Fprintf(&b, "## %s\n\n", s.Title)
		for _, blk := range s.Blocks {
			renderBlock(&b, blk)
		}
	}
	for _, d := range vm.Disclaimers {
		b.WriteString(d)
	}
	for _, f := range vm.Footnotes {
		b.WriteString(f.Text)
	}
	return b.String()
}

func renderBlock(b *strings.Builder, blk BlockVM) {
	switch x := blk.(type) {
	case ParaVM:
		b.WriteString(x.Text)
	case DetailsVM:
		b.WriteString("<details><summary>" + x.Summary + "</summary>\n\n")
		b.WriteString(x.Body)
		b.WriteString("</details>\n\n")
	case *TableVM:
		renderTable(b, x)
	default:
		panic(fmt.Sprintf("RenderMarkdown: unknown block type %T", blk))
	}
}

// renderTable writes one table: optional title line, optional <details>
// fold opening, header + separator + data rows, then the fold close or the
// blank line, then the table's notes verbatim. The geometry is byte-for-
// byte the legacy newTable/mdTable output so the transition test can
// compare the two renderers.
func renderTable(b *strings.Builder, t *TableVM) {
	if t.Title != "" {
		fmt.Fprintf(b, "%s\n\n", t.Title)
	}
	if t.Fold != "" {
		b.WriteString(t.Fold)
	}
	fmt.Fprintf(b, "%s", "| "+strings.Join(t.Headers, " | ")+" |\n")
	seps := make([]string, len(t.Headers))
	for i := range seps {
		seps[i] = "---"
	}
	fmt.Fprintf(b, "%s", "|"+strings.Join(seps, "|")+"|\n")
	for _, r := range t.Rows {
		fmt.Fprintf(b, "%s", "| "+strings.Join(r, " | ")+" |\n")
	}
	if t.Fold != "" {
		b.WriteString("\n</details>\n\n")
		return
	}
	b.WriteString("\n")
	for _, n := range t.Notes {
		b.WriteString(n)
	}
}

// vmNotes converts the write-stream that followed a legacy table's rows
// into TableVM.Notes. The serializer already emits the blank line right
// after the rows, so one leading "\n" is absorbed here; the remaining
// parts are notes verbatim. Empty results mean no notes at all.
func vmNotes(parts ...string) []string {
	s := strings.Join(parts, "")
	if s == "" {
		return nil
	}
	s = strings.TrimPrefix(s, "\n")
	if s == "" {
		return nil
	}
	return []string{s}
}
