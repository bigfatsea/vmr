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

func parseManifestBodyIncremental(rec *audit.Record, prof taskseg.Profile, deltaStart int, state *stepFactState) (msgs []chatmsg.Message, rawMsgs []any, off int, ru taskseg.RealUsers) {
	body, _ := rec.Client.Request.Body.(map[string]any)
	msgs = chatmsg.Messages(body)
	rawMsgs = chatmsg.RawArray(body)
	off = chatmsg.MsgOffset(body)
	ru = state.computeRU(prof, msgs, rawMsgs, off, deltaStart)
	return
}

// fillStepFacts extracts everything a Step's downstream consumers used to
// read back off Step.Rec, so the Record itself doesn't have to outlive
// buildFrom. Called once per Step, right after buildStep, with the
// message list buildFrom already parsed for this manifest. j receives the
// one Journey-level fact derived per Step: the SysHash -> leading-system
// text table (deduped, so a 500-step Journey on one system prompt stores
// it once).
func fillStepFacts(j *Journey, step *Step, rec *audit.Record, msgs []chatmsg.Message, rawMsgs []any, off, deltaStart int, state *stepFactState) {
	step.Outcome = rec.Outcome
	if rec.Outcome == "error" {
		for _, a := range rec.Attempts {
			if a.ErrorClass != "" {
				step.ErrorClass = a.ErrorClass
				break
			}
		}
	}

	if len(rec.Attempts) > 0 {
		step.Attempts = make([]AttemptFact, len(rec.Attempts))
		for i, a := range rec.Attempts {
			step.Attempts[i] = AttemptFact{Provider: a.Provider, Model: a.Model}
		}
	}

	step.Context = state.updateContext(step.Seq, msgs, deltaStart)

	step.NewToolResults = chatmsg.ToolResultList(deltaRawMsgs(rawMsgs, off, deltaStart))

	step.SysChars = state.updateSysChars(j, step, msgs)
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
		accumulateContextToken(&p, msg.Role, tk)
	}
	return p
}

type roleTokens struct {
	role   string
	tokens int64
}

// stepFactState maintains incremental extraction state across steps in a
// lineage to avoid repeating O(N^2) token estimation and real-user parsing
// over unedited message history prefixes.
type stepFactState struct {
	prevRu       taskseg.RealUsers
	prevTokens   []roleTokens
	prevSysChars int
}

func (s *stepFactState) computeRU(prof taskseg.Profile, msgs []chatmsg.Message, rawMsgs []any, off, deltaStart int) taskseg.RealUsers {
	if deltaStart <= 0 || len(s.prevRu) == 0 {
		ru := taskseg.IndexRealUsers(prof, msgs, rawMsgs, off)
		s.prevRu = ru
		return ru
	}
	ru := make(taskseg.RealUsers, len(s.prevRu)+(len(msgs)-deltaStart))
	for k, v := range s.prevRu {
		if k < deltaStart {
			ru[k] = v
		}
	}
	for i := deltaStart; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role != "user" {
			continue
		}
		if text, ok := prof.RealUserText(m, rawMsgs, i-off); ok {
			ru[i] = taskseg.Preview(text)
		}
	}
	s.prevRu = ru
	return ru
}

func (s *stepFactState) updateContext(seq int, msgs []chatmsg.Message, deltaStart int) ContextPoint {
	curTokens := make([]roleTokens, len(msgs))
	p := ContextPoint{Seq: seq}
	copyLen := deltaStart
	if copyLen > len(s.prevTokens) {
		copyLen = len(s.prevTokens)
	}
	if copyLen < 0 {
		copyLen = 0
	}
	for i := 0; i < copyLen; i++ {
		curTokens[i] = s.prevTokens[i]
		accumulateContextToken(&p, curTokens[i].role, curTokens[i].tokens)
	}
	for i := copyLen; i < len(msgs); i++ {
		tk := tokenutil.EstimateText(msgs[i].Text)
		curTokens[i] = roleTokens{role: msgs[i].Role, tokens: tk}
		accumulateContextToken(&p, curTokens[i].role, tk)
	}
	s.prevTokens = curTokens
	return p
}

func accumulateContextToken(p *ContextPoint, role string, tk int64) {
	switch role {
	case "system":
		p.SystemTokens += tk
	case "assistant":
		p.AssistantTokens += tk
	case "tool":
		p.ToolTokens += tk
	default:
		p.UserTokens += tk
	}
}

func (s *stepFactState) updateSysChars(j *Journey, step *Step, msgs []chatmsg.Message) int {
	if !step.Manifest.HasSys {
		return 0
	}
	var chars int
	if !step.SysChanged && s.prevSysChars > 0 {
		chars = s.prevSysChars
	} else {
		chars = len([]rune(ctxgraph.LeadingSystemText(msgs, step.Manifest.LeadSys)))
		s.prevSysChars = chars
	}
	if _, seen := j.SysText[step.Manifest.SysHash]; !seen {
		if j.SysText == nil {
			j.SysText = map[ctxgraph.Hash][]string{}
		}
		j.SysText[step.Manifest.SysHash] = leadingSystemParts(msgs, step.Manifest.LeadSys)
	}
	return chars
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
