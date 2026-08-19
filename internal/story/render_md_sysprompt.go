// Ver 2026-08-19, by Sonnet 5

// renderSystemPromptHeader and its supporting systemPromptEras: split out of
// render_md.go purely to stay under its archtest line budget (see
// file_sizes_test.go) — no behavior split, this is one self-contained piece
// of RenderMarkdown's header rendering.
package story

import (
	"strings"

	"vmr/internal/i18n"
)

// systemPromptEra is one contiguous run of Steps sharing the same system
// prompt text.
type systemPromptEra struct {
	Text           string
	FromSeq, ToSeq int
}

// systemPromptEras walks j's Steps in order and groups them by system
// prompt text — computed straight from NewEvents' own global dedup (a
// system-role Event only ever appears once, at the Step where its content
// first differs from whatever came before this Journey; see journey.go's
// SysChanged doc comment), not from SysChanged itself (that flag exists for
// renderStep's own "something changed here" marker, not for grouping). A
// Step whose request carried more than one system-role message (e.g. the
// Responses protocol's separate system+instructions fields) joins them into
// one era entry — they're all "the leading system block" as far as a reader
// is concerned. Returns nil for a Journey with no system-role content at
// all (shouldn't happen in practice — every real request carries one — but
// this must never assume so).
func systemPromptEras(j *Journey) []systemPromptEra {
	steps := journeySteps(j)
	var eras []systemPromptEra
	for _, s := range steps {
		var parts []string
		for _, ev := range s.NewEvents {
			if ev.Msg.Role == "system" {
				parts = append(parts, ev.Msg.Text)
			}
		}
		if len(parts) > 0 {
			eras = append(eras, systemPromptEra{Text: strings.Join(parts, "\n\n---\n\n"), FromSeq: s.Seq})
		}
	}
	for i := range eras {
		if i+1 < len(eras) {
			eras[i].ToSeq = eras[i+1].FromSeq - 1
		} else if len(steps) > 0 {
			eras[i].ToSeq = steps[len(steps)-1].Seq
		}
	}
	return eras
}

// renderSystemPromptHeader renders every distinct system prompt text the
// Journey used, once each, folded by default — moved here from Step 1's
// (and any later SysChanged Step's) own Messages section, since every
// request in a session carries the same prefix and repeating it inline
// just pushes the decision spine and the Step content a reader actually
// wants further down the document.
func renderSystemPromptHeader(w func(string, ...any), j *Journey, t i18n.StoryText) {
	eras := systemPromptEras(j)
	if len(eras) == 0 {
		return
	}
	w("%s", t.SysPromptHeaderTitle)
	if len(eras) > 1 {
		w("%s", t.SysPromptHeaderChanged(len(eras)))
	}
	for _, e := range eras {
		w("<details><summary>%s</summary>\n\n%s</details>\n\n",
			t.SysPromptEraSummary(e.FromSeq, e.ToSeq, len([]rune(e.Text))), codeFence(e.Text))
	}
}
