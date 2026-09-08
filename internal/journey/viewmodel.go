// Ver 2026-09-15, by pi

// The journey-side ViewModel layer (architecture redesign §5.0/§5.2, D3/D4/D11/D12):
// the single rendering path for j-<id>.md. BuildJourneyVM consumes ONLY the
// self-contained JourneySummary — the shape j-<id>.json publishes — and absorbs
// every business formatting decision (argument shape-picking, multi-level step
// role tags, badge determination), every escaping/truncation call, and every
// i18n lookup, producing a flat, print-ready, in-memory-only block sequence.
// SerializeJourneyVM is the fixed serializer: a deterministic walk over that
// structure with no copy, no formatting and no i18n of its own (D3 — no
// template engine; the VM is Markdown's deterministic source).
//
// Two entry points, one path: RenderMarkdownFromSummary(s) builds the VM and
// serializes it — the same path -render-only will take once cmd wiring lands
// (Phase 3, D11). The pre-existing RenderMarkdown (render_md.go, eats the
// in-memory *Journey) stays until 3C deletes it; the transition equivalence
// between the two is pinned by test, not assumed.
package journey

import (
	"strings"

	"vmr/internal/i18n"
)

// VM block kinds.
const (
	vmText    = "text"    // Text is a complete print-ready Markdown chunk
	vmTable   = "table"   // Table is set
	vmDetails = "details" // Details is set
)

// VMTable is one print-ready Markdown table. The two halves' table semantics
// deliberately differ (§11.1), so this is journey-side's own shape, not a
// shared report type: Header is the complete localized header+separator
// block, each row one complete print-ready line.
type VMTable struct {
	Header string   `json:"header"`
	Rows   []string `json:"rows,omitempty"`
}

// VMDetails is one print-ready folded block, serialized to exactly:
// Prefix + "<details><summary>" + Summary + "</summary>\n\n" + Body + "</details>\n\n".
// Prefix is the lead-in before the fold (e.g. the "> " of a folded why-line,
// the "🔧 `name` `key`: " of a payload block); Body is everything between the
// opening summary line and the closing tag — already fenced/complete, and
// ending with the blank line the old renderer put before </details> on the
// fenced folds (the compaction fold, whose body is optional, ends without
// one) — see the builders in viewmodel_spine.go.
type VMDetails struct {
	Prefix  string `json:"prefix,omitempty"`
	Summary string `json:"summary"`
	Body    string `json:"body,omitempty"`
}

// VMBlock is one block of the print-ready document. Exactly one field is set.
type VMBlock struct {
	Kind    string     `json:"kind"`
	Text    string     `json:"text,omitempty"`
	Table   *VMTable   `json:"table,omitempty"`
	Details *VMDetails `json:"details,omitempty"`
}

// JourneyVM groups the print-ready blocks by document section — the structure
// golden tests compare (§9: golden sunk to VM structure), and the fixed order
// SerializeJourneyVM walks. All text is final; the serializer only concatenates.
type JourneyVM struct {
	Header     []VMBlock `json:"header,omitempty"`     // id, title, meta, back links, partial/break banners
	SysPrompt  []VMBlock `json:"sys_prompt,omitempty"` // system-prompt era list
	Overview   []VMBlock `json:"overview,omitempty"`   // 3-second overview card
	Indicators []VMBlock `json:"indicators,omitempty"` // behavior indicators + model usage
	Artifacts  []VMBlock `json:"artifacts,omitempty"`  // workspace file mutation targets
	Spine      []VMBlock `json:"spine,omitempty"`      // the decision spine incl. final deliverable
	Timeline   []VMBlock `json:"timeline,omitempty"`   // tool-call timeline
	Findings   []VMBlock `json:"findings,omitempty"`   // findings section
	LLM        []VMBlock `json:"llm,omitempty"`        // -llm-addr interpretation section, last (§3.6: rendered from s.LLMInterpretation)
}

func vmTextBlock(text string) VMBlock { return VMBlock{Kind: vmText, Text: text} }

func vmDetailsBlock(prefix, summary, body string) VMBlock {
	return VMBlock{Kind: vmDetails, Details: &VMDetails{Prefix: prefix, Summary: summary, Body: body}}
}

// serializeDetails is the one folded-block shape every VMDetails renders to —
// the exact byte layout foldWhyLine/payloadBlock/toolResultLine/
// renderCompactionInfo/renderFinalDeliverable emit today.
func serializeDetails(d *VMDetails) string {
	return d.Prefix + "<details><summary>" + d.Summary + "</summary>\n\n" + d.Body + "</details>\n\n"
}

// SerializeJourneyVM is the fixed serializer (D3): walk the sections in
// document order; text blocks are written as-is, tables as header+rows+blank
// line, details blocks through serializeDetails' single fixed shape. No copy,
// no formatting, no i18n — everything here is already print-ready.
func SerializeJourneyVM(vm *JourneyVM) string {
	var b strings.Builder
	write := func(blocks []VMBlock) {
		for _, blk := range blocks {
			switch blk.Kind {
			case vmTable:
				b.WriteString(blk.Table.Header)
				for _, row := range blk.Table.Rows {
					b.WriteString(row)
				}
				b.WriteString("\n")
			case vmDetails:
				b.WriteString(serializeDetails(blk.Details))
			default:
				b.WriteString(blk.Text)
			}
		}
	}
	write(vm.Header)
	write(vm.SysPrompt)
	write(vm.Overview)
	write(vm.Indicators)
	write(vm.Artifacts)
	write(vm.Spine)
	write(vm.Timeline)
	write(vm.Findings)
	write(vm.LLM)
	return b.String()
}

// RenderMarkdownFromSummary renders s — the self-contained JourneySummary that
// j-<id>.json publishes — as a complete Markdown document in lang. This is the
// one rendering path for j-<id>.md (D11): everything it shows comes from s
// alone, so the audit log is never an input (D18's structural guarantee).
// linkDetails/reportMDExists carry the caller's materialization decisions the
// same way RenderMarkdown's do; cost rides on s.Cost.
func RenderMarkdownFromSummary(s *JourneySummary, lang i18n.Lang, reportMDExists, linkDetails bool) string {
	return SerializeJourneyVM(BuildJourneyVM(s, lang, reportMDExists, linkDetails))
}

// BuildJourneyVM builds the print-ready ViewModel for one JourneySummary.
// Every call here is pure: no I/O, no judgment about content, only the
// formatting/badge/i18n decisions this layer owns.
func BuildJourneyVM(s *JourneySummary, lang i18n.Lang, reportMDExists, linkDetails bool) *JourneyVM {
	return &JourneyVM{
		Header:     buildVMHeader(s, lang, reportMDExists),
		SysPrompt:  buildVMSysPrompt(s, lang, linkDetails),
		Overview:   buildVMOverview(s, lang),
		Indicators: buildVMIndicators(s, lang),
		Artifacts:  buildVMArtifacts(s, lang),
		Spine:      buildVMSpine(s, lang, linkDetails),
		Timeline:   buildVMTimeline(s, lang),
		Findings:   buildVMFindings(s, lang),
		LLM:        buildVMLLM(s, lang),
	}
}

// buildVMLLM renders the persisted -llm-addr interpretation record (§3.6) as
// the document's last section — the same "\n" + RenderLLMSection block the
// old writeJourneyFile/compareJourneys append produced, now a pure function
// of the JSON so -render-only reproduces it without scraping the old .md.
// A nil or failed record contributes no block (the full run also left the
// section out), keeping D11 byte-equivalence exact.
func buildVMLLM(s *JourneySummary, lang i18n.Lang) []VMBlock {
	sec := RenderLLMSection(s.LLMInterpretation, lang)
	if sec == "" {
		return nil
	}
	return []VMBlock{vmTextBlock("\n" + sec)}
}
