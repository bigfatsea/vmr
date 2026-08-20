// Ver 2026-08-20, by Sonnet 5

// renderSystemPromptHeader and its supporting systemPromptEras: split out of
// render_md.go purely to stay under its archtest line budget (see
// file_sizes_test.go) — no behavior split, this is one self-contained piece
// of RenderMarkdown's header rendering.
package story

import (
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// systemPromptEra is one contiguous run of Steps sharing the same system
// prompt content — grouped by Manifest.HasSys/SysHash, NOT by scanning
// NewEvents. A prior version of this function grouped by scanning each
// Step's NewEvents for a role=="system" message, but that only ever works
// at a Journey's first Step or a stitch boundary: for i>0 continuations
// (append.go's deltaStart = m.LeadSys + e.LCP >= m.LeadSys),
// appendNewEvents structurally never scans indices below LeadSys, so a
// genuine mid-Lineage system-prompt change (SysChanged == true on an
// ordinary continuation Step — e.g. a model or tool-set switch mid
// conversation) never produced a NewEvent and was silently invisible here.
// Grouping by the same (HasSys, SysHash) pair journey.go's own SysChanged
// computation uses (see journey.go's sysChanged assignment) fixes that and
// keeps this function and spineTransitionLines' SysChangedLine agreeing on
// exactly when "the system prompt changed" is true — one judgment, two
// renderings of it, not two independent (and driftable) ones.
type systemPromptEra struct {
	HasSys         bool
	SysHash        ctxgraph.Hash
	FromSeq, ToSeq int
	// Owner is the era's first Step — carries the Manifest/Rec needed to
	// compute a char-count summary (sysPromptEraChars) and the evidence
	// link (reqdetail.SysPromptEvidenceFileName).
	Owner *Step
}

func systemPromptEras(j *Journey) []systemPromptEra {
	var eras []systemPromptEra
	for _, s := range journeySteps(j) {
		if s.Manifest == nil {
			continue
		}
		m := s.Manifest
		last := len(eras) - 1
		if last < 0 || m.HasSys != eras[last].HasSys || (m.HasSys && m.SysHash != eras[last].SysHash) {
			eras = append(eras, systemPromptEra{HasSys: m.HasSys, SysHash: m.SysHash, FromSeq: s.Seq, ToSeq: s.Seq, Owner: s})
			continue
		}
		eras[last].ToSeq = s.Seq
	}
	return eras
}

// sysPromptEraChars computes an era's leading-system-block char count from
// the SAME text ctxgraph.Manifest.SysHash (and reqdetail's evidence blob)
// are derived from — ctxgraph.LeadingSystemText applied to e.Owner's own
// request body — so the number shown here can never disagree with what's
// actually behind the link.
func sysPromptEraChars(e systemPromptEra) int {
	body, ok := e.Owner.Rec.Client.Request.Body.(map[string]any)
	if !ok {
		return 0
	}
	text := ctxgraph.LeadingSystemText(chatmsg.Messages(body), e.Owner.Manifest.LeadSys)
	return len([]rune(text))
}

// renderSystemPromptHeader renders one line per distinct system-prompt era
// the Journey used: effective Step range, char count, and a link to the
// shared evidence blob (P5.3 — moved from render_md.go, which formerly
// Step 1's (and any later SysChanged Step's) own Messages section, since
// every request in a session carries the same prefix and repeating it
// inline just pushes the decision spine and the Step content a reader
// actually wants further down the document. Full text now lives at
// evidenceDir/sysprompt-<hash>.md (reqdetail.EnsureSysPromptEvidence,
// materialized by EnsureJourneyDetails before this renders) — the report
// itself only ever holds the address, not the content.
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
		if !e.HasSys {
			w("%s", t.SysPromptEraNoSys(e.FromSeq, e.ToSeq))
			continue
		}
		filename := reqdetail.SysPromptEvidenceFileName(e.SysHash)
		w("%s", t.SysPromptEraLink(e.FromSeq, e.ToSeq, sysPromptEraChars(e), "../evidence/"+filename))
	}
	w("\n")
}
