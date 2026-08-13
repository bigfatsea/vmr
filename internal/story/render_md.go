// Ver 2026-08-01, by Sonnet 5

package story

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RenderMarkdown renders j as a self-contained Markdown document in lang:
// a decision-spine layer (overview card, per-Task action list — see
// render_spine.go) followed by the event stream organized by Task/Step,
// each Step's genuinely-new content shown inline and its full recorded body
// available in a folded <details> block, then a tool-call timeline and the
// Findings candidate list. m/findings are computed once by the caller
// (cmd/vmr/cmd_story.go's writeJourneyFile) and passed in rather than
// recomputed here, so the Markdown and JSON outputs for the same Journey
// are guaranteed to agree on both. Purely a view over already-computed
// facts — no judgment calls happen here, only formatting.
func RenderMarkdown(j *Journey, m Metrics, findings []Finding, lang i18n.Lang) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	t := i18n.Story(lang)
	st := i18n.Spine(lang)

	w("# Journey %s\n\n", j.ID)
	w("> %s\n\n", j.Title)
	w("%s", t.JourneyMeta(len(j.Tasks), stepCount(j), j.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"), j.To.In(fmtutil.DisplayZone).Format("15:04:05")))

	if j.Break != nil {
		w("%s", t.BreakWarning(j.Break.Edit.Kind.String(), breakReasonHint(j.Break.Edit.Kind, t), editStatsHint(j.Break.Edit, t)))
	}

	renderOverviewCard(w, j, m, lang)
	renderModelUsage(w, m, lang)
	renderDecisionSpine(w, j, findings, lang)

	isRepeatStep := map[int]bool{}
	for _, o := range toolCallRepeats(journeySteps(j)) {
		if o.IsRepeat {
			isRepeatStep[o.StepSeq] = true
		}
	}

	for ti, task := range j.Tasks {
		w("## t%02d · %s\n\n", ti+1, task.Title)
		for _, step := range task.Steps {
			renderStep(w, step, t, st, isRepeatStep[step.Seq])
		}
	}

	renderToolTimeline(w, j, lang)
	renderFindingsSection(w, findings, lang)
	return b.String()
}

func stepCount(j *Journey) int {
	n := 0
	for _, t := range j.Tasks {
		n += len(t.Steps)
	}
	return n
}

func breakReasonHint(k ctxgraph.EditKind, t i18n.StoryText) string {
	switch k {
	case ctxgraph.Contract:
		return t.BreakReasonContract
	case ctxgraph.Fork:
		return t.BreakReasonFork
	default:
		return t.BreakReasonDefault
	}
}

// editStatsHint renders an Edit's LCP/Coverage as plain language instead of
// the "lcp=N, cov=P%" abbreviations a reader has no way to decode from the
// document alone (design-doc review follow-up: a real reader asked "这里的
// LCP是什么？cov又是谁"). LCP: how many messages at the START of this turn
// are byte-identical to the previous turn, in order — everything after that
// point is what actually changed. Coverage: what fraction of THIS turn's
// messages already existed SOMEWHERE in the previous turn (content-only,
// not position) — the number Contract/Fork's "did this really keep going,
// or is it a new conversation" judgment call is based on.
func editStatsHint(e ctxgraph.Edit, t i18n.StoryText) string {
	return t.EditStatsHint(e.LCP, e.Coverage*100)
}

func renderStep(w func(string, ...any), s *Step, t i18n.StoryText, st i18n.SpineText, isRepeat bool) {
	m := s.Manifest
	w("### %s Step %d · %s · %s", stepRoleTag(s, isRepeat, st), s.Seq, m.TS.In(fmtutil.DisplayZone).Format("15:04:05"), fmtutil.FmtSeconds(msDuration(m.DurMS), 1))
	if m.TTFTMS > 0 {
		w(" (ttft %s)", fmtutil.FmtSeconds(msDuration(m.TTFTMS), 1))
	}
	if m.UsageOK {
		w(" · %s/%s/%s", fmtTokens(m.Usage.Fresh()), fmtTokens(m.Usage.CacheRead), fmtTokens(m.Usage.Out))
	}
	w(" · %s\n\n", m.Endpoint)

	if s.Edge != nil {
		w("%s", t.EditLine(s.Edge.Kind.String(), editStatsHint(*s.Edge, t)))
	}
	if s.StitchEdge != nil {
		w("%s", t.StitchLine(s.StitchEdge.Kind.String(), pctStr(s.StitchEdge.Score), pctStr(s.StitchEdge.Confidence)))
	}
	if s.SysChanged {
		w("%s", t.SysChangedLine)
	}
	if s.Compaction != nil {
		renderCompactionInfo(w, s.Compaction, t)
	}

	if len(s.NewEvents) > 0 {
		w("**Messages**\n\n")
		for _, ev := range s.NewEvents {
			renderEvent(w, ev, t)
		}
	}

	renderLLMResponse(w, s, t)

	if s.NoReply {
		w("%s", t.NoReplyLine)
	}
}

// renderLLMResponse shows what the model itself produced this turn — the
// part the old renderer dropped almost entirely (design-doc review
// follow-up: a real Journey's tool-calling step rendered as a bare "🔧 调用
// 工具: read, read", no arguments, no ids, no reasoning; the full content
// only surfaced later, folded into the NEXT step's Messages section once it
// became history — one step later than where it actually happened, and
// only as raw re-serialized text, not the response's own shape). Reasoning
// and the tool-call block get their own <details>, same folded-by-default
// convention renderEvent already uses for Messages; a plain-text reply is
// previewed like a Message so it's still scannable at a glance.
func renderLLMResponse(w func(string, ...any), s *Step, t i18n.StoryText) {
	if s.Reasoning == "" && s.RespText == "" && len(s.ToolCalls) == 0 {
		if s.Finish != "" {
			w("- finish: `%s`\n\n", s.Finish)
		}
		return
	}
	w("**LLM Response**\n\n")

	if s.Reasoning != "" {
		w("<details><summary>%s</summary>\n\n%s</details>\n\n",
			t.ReasoningSummary(len([]rune(s.Reasoning))), codeFence(s.Reasoning))
	}
	if s.RespText != "" {
		w("<details><summary>%s</summary>\n\n%s</details>\n\n",
			t.ReplySummary(escapeHTML(preview(s.RespText))), codeFence(s.RespText))
	}
	if len(s.ToolCalls) > 0 {
		names := make([]string, len(s.ToolCalls))
		for i, tc := range s.ToolCalls {
			names[i] = tc.Name
		}
		var body strings.Builder
		for _, tc := range s.ToolCalls {
			fmt.Fprintf(&body, "🔧 **tool_call** `%s` [id=%s]\n%s\n", tc.Name, tc.ID, codeFenceLang(prettyJSON(tc.Args), "json"))
		}
		w("<details><summary>finish: %s (%s)</summary>\n\n%s</details>\n\n",
			s.Finish, strings.Join(names, ", "), body.String())
	} else if s.Finish != "" {
		w("- finish: `%s`\n\n", s.Finish)
	}
}

// prettyJSON re-indents s if it's valid JSON (tool_calls' arguments arrive
// as a compact, single-line JSON string), falling back to s verbatim when
// it isn't — a mid-stream truncation can leave a tool call's arguments
// incomplete, and this must never panic or drop content on that.
func prettyJSON(s string) string {
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}

func renderEvent(w func(string, ...any), ev *Event, t i18n.StoryText) {
	head := fmt.Sprintf("▸ %s", ev.Msg.Role)
	if ev.Revises != nil {
		// The "revision" relation: without this marker, a Splice-rewritten
		// message would render as an unrelated new Event — reading as "the
		// same thing said twice" instead of "this replaces that".
		head += t.RevisionMarker(ev.Revises.String()[:8])
	}
	if ev.Msg.Text == "" {
		w("%s", t.EmptyEvent(head))
		return
	}
	summary := preview(ev.Msg.Text)
	w("<details><summary>%s · %s</summary>\n\n%s</details>\n\n", head, escapeHTML(summary), codeFence(ev.Msg.Text))
}

// renderCompactionInfo shows a stitch boundary's information-loss summary
// (CCR N-4's promise): token counts before/after, plus
// which file-path/URL-shaped entities the predecessor's tail mentioned but
// this step's opening doesn't — versus which survived. Folded by default
// like everything else here; the point is that it's THERE to check, not
// that every reader needs to open it every time.
func renderCompactionInfo(w func(string, ...any), c *CompactionInfo, t i18n.StoryText) {
	ratio := "—"
	if c.TokensBefore > 0 {
		ratio = fmtutil.FmtPercent(float64(c.TokensAfter)/float64(c.TokensBefore), 1)
	}
	w("<details><summary>%s</summary>\n\n",
		t.CompactionSummary(fmtTokens(c.TokensBefore), fmtTokens(c.TokensAfter), ratio, len(c.SwallowedEntities), len(c.SurvivedEntities)))
	if len(c.SwallowedEntities) > 0 {
		w("%s", t.SwallowedEntities(strings.Join(c.SwallowedEntities, t.ListSep)))
	}
	if len(c.SurvivedEntities) > 0 {
		w("%s", t.SurvivedEntities(strings.Join(c.SurvivedEntities, t.ListSep)))
	}
	w("</details>\n\n")
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// codeFence wraps s in a fenced code block whose fence is longer than any
// backtick run inside s, so message content (an agent quoting code, a tool
// result containing a Markdown snippet) can never break out of its block —
// a fixed ``` fence would silently corrupt the rest of the document the
// moment real content contains one. Same rationale/implementation as
// internal/report/render.go's codeFence (duplicated, not exported: it's a
// tiny, stable, purely cosmetic helper — see that package's chatmsg_compat.go
// for the general pattern of what does and doesn't get shared), plus an
// optional lang tag (report's own codeFence has no caller that needs one).
// codeFence itself is the plain "" case every caller but tool-call
// arguments uses.
func codeFence(s string) string { return codeFenceLang(s, "") }

func codeFenceLang(s, lang string) string {
	n := 3
	run := 0
	for _, r := range s {
		if r == '`' {
			run++
			if run >= n {
				n = run + 1
			}
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", n)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return f + lang + "\n" + s + f + "\n"
}

// msDuration converts a millisecond count (as stored in the audit record)
// to a time.Duration for fmtutil.FmtSeconds.
func msDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

// fmtTokens renders an actual (already-billed) per-step token count —
// K/M-scaled, no "(est)" marker: the numbers here are the request's
// recorded usage.In/CacheRead/Out, not an estimate, so carrying one would
// claim the opposite of what's true. Same rationale internal/report/
// detail.go's fmtTokensPlain applies for its own (estimated, but
// contextually labeled) token count, and internal/router/logfmt.go's
// estTokenField/usageTokenField for the live router log's inline version
// of the same est-vs-actual distinction.
func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// pctStr is story's local 0-decimal alias for fmtutil.FmtPercent —
// narrative text wants "92%", not report's denser-table "91.7%" (that
// package's own pctStr aliases the same function at 1 decimal instead).
func pctStr(f float64) string {
	return fmtutil.FmtPercent(f, 0)
}
