// Ver 2026-08-30 21:45, by Sonnet 5

package story

import (
	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/taskseg"
	"vmr/internal/tokenutil"
)

// AttemptFact is the per-attempt slice of an audit.Record the story half
// still needs after buildFrom drops the full Record: modelusage.go reads
// every distinct (Provider, Model) any attempt touched, render_html_
// dashboard.go reads len() for its "failed over" marker. The routing-half
// detail (URLs, request/response bodies, timing) never had a story
// consumer.
type AttemptFact struct {
	Provider string
	Model    string
}

// parseManifestBody is buildFrom's per-manifest preamble: decode the
// request body once and derive the message list, its raw form, the
// index offset between them, and the real-user-instruction index every
// downstream step-level decision reads.
func parseManifestBody(rec *audit.Record, prof taskseg.Profile) (msgs []chatmsg.Message, rawMsgs []any, off int, ru taskseg.RealUsers) {
	body, _ := rec.Client.Request.Body.(map[string]any)
	msgs = chatmsg.Messages(body)
	rawMsgs = chatmsg.RawArray(body)
	off = chatmsg.MsgOffset(body)
	ru = taskseg.IndexRealUsers(prof, msgs, rawMsgs, off)
	return
}

// fillStepFacts extracts everything a Step's downstream consumers used to
// read back off Step.Rec, so the Record itself doesn't have to outlive
// buildFrom. Called once per Step, right after buildStep, with the
// message list buildFrom already parsed for this manifest. j receives the
// one Journey-level fact derived per Step: the SysHash -> leading-system
// text table (deduped, so a 500-step Journey on one system prompt stores
// it once).
func fillStepFacts(j *Journey, step *Step, rec *audit.Record, msgs []chatmsg.Message, rawMsgs []any, off, deltaStart int) {
	if len(rec.Attempts) > 0 {
		step.Attempts = make([]AttemptFact, len(rec.Attempts))
		for i, a := range rec.Attempts {
			step.Attempts[i] = AttemptFact{Provider: a.Provider, Model: a.Model}
		}
	}

	step.Context = stepContextPoint(step.Seq, msgs)

	step.NewToolResults = chatmsg.ToolResultList(deltaRawMsgs(rawMsgs, off, deltaStart))

	if step.Manifest.HasSys {
		step.SysChars = len([]rune(ctxgraph.LeadingSystemText(msgs, step.Manifest.LeadSys)))
		if _, seen := j.SysText[step.Manifest.SysHash]; !seen {
			if j.SysText == nil {
				j.SysText = map[ctxgraph.Hash][]string{}
			}
			j.SysText[step.Manifest.SysHash] = leadingSystemParts(msgs, step.Manifest.LeadSys)
		}
	}
}

// leadingSystemParts is the individual text of msgs[:leadSys] — compare.go's
// sysPromptStats wants both a per-part token sum and a "\n\n"-joined
// excerpt, so the parts are kept separate rather than pre-joined.
func leadingSystemParts(msgs []chatmsg.Message, leadSys int) []string {
	n := leadSys
	if n > len(msgs) {
		n = len(msgs)
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = msgs[i].Text
	}
	return parts
}

// stepContextPoint sums a request body's estimated tokens by message role —
// the per-Step composition metrics.go's contextCurve renders. Formerly
// recomputed in metrics.go by re-parsing Step.Rec's body; extracted here so
// the body parse happens once, in buildFrom, alongside every other use of
// the same message list.
func stepContextPoint(seq int, msgs []chatmsg.Message) ContextPoint {
	p := ContextPoint{Seq: seq}
	for _, msg := range msgs {
		tk := tokenutil.EstimateText(msg.Text)
		switch msg.Role {
		case "system":
			p.SystemTokens += tk
		case "assistant":
			p.AssistantTokens += tk
		case "tool":
			p.ToolTokens += tk
		default: // "user" and any non-standard role (chatmsg.Messages' "?" fallback)
			p.UserTokens += tk
		}
	}
	return p
}

// deltaRawMsgs is the slice of a decoded body's raw "messages" array that
// this Step introduced — mirrors positionalToolResults' own deltaIdx math
// (DeltaStart is a Messages()-index; RawArray excludes the synthetic
// leading system message, so MsgOffset corrects the base). A non-positive
// delta means "scan the whole array" (a stitch boundary, or a Journey's
// first Step); a delta past the end means "nothing new here".
func deltaRawMsgs(rawMsgs []any, off, deltaStart int) []any {
	deltaIdx := deltaStart - off
	if deltaIdx <= 0 {
		return rawMsgs
	}
	if deltaIdx >= len(rawMsgs) {
		return nil
	}
	return rawMsgs[deltaIdx:]
}

// firstRealInstruction is the raw (un-Preview'd) text of a Journey's
// opening real user instruction — compare.go's initialInstructionStats
// wants it without segment.Preview's whitespace-collapse and length cap
// (it applies its own, wider, initialInstructionExcerptChars bound). Same
// scan initialInstructionStats did against steps[0].Rec: first user-role
// message the profile accepts as a real instruction. "" when none.
func firstRealInstruction(prof taskseg.Profile, msgs []chatmsg.Message, rawMsgs []any, off int) string {
	for i, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if raw, ok := prof.RealUserText(m, rawMsgs, i-off); ok {
			return raw
		}
	}
	return ""
}
