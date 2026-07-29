// Ver 2026-07-29 18:00, by Sonnet 5

package story

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
)

// RenderMarkdown renders j as a self-contained Markdown document: the event
// stream organized by Task/Step, each Step's genuinely-new content shown
// inline and its full recorded body available in a folded <details> block.
// Purely a view over already-computed facts (Task/Step/Event) — no
// judgment calls happen here, only formatting (design doc §3.3's layering:
// this is the fact-layer renderer; a narrate-layer on top is Phase C).
func RenderMarkdown(j *Journey) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# Journey %s\n\n", j.ID)
	w("> %s\n\n", j.Title)
	w("> %d 任务 · %d 轮 · %s → %s\n\n",
		len(j.Tasks), stepCount(j), j.From.Format("2006-01-02 15:04:05"), j.To.Format("15:04:05"))

	if j.Break != nil {
		w("> ⚠️ **本 journey 的开头是从上一段上下文断裂而来**（%s：%s；%s）——两段之间的关系尚未确认，本轮（第一步）不做跨断点缝合，只如实标出断点。\n\n",
			j.Break.Edit.Kind.String(), breakReasonHint(j.Break.Edit.Kind), editStatsHint(j.Break.Edit))
	}

	for ti, task := range j.Tasks {
		w("## t%02d · %s\n\n", ti+1, task.Title)
		for _, step := range task.Steps {
			renderStep(w, step)
		}
	}
	return b.String()
}

func stepCount(j *Journey) int {
	n := 0
	for _, t := range j.Tasks {
		n += len(t.Steps)
	}
	return n
}

func breakReasonHint(k ctxgraph.EditKind) string {
	switch k {
	case ctxgraph.Contract:
		return "上下文被大幅收缩（截断/重建）"
	case ctxgraph.Fork:
		return "内容与前段几乎不重叠（可能是同一 anchor 下的另一次对话）"
	default:
		return "结构性断裂"
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
func editStatsHint(e ctxgraph.Edit) string {
	return fmt.Sprintf("最长相同前缀 %d 条消息，内容重合率 %.0f%%", e.LCP, e.Coverage*100)
}

func renderStep(w func(string, ...any), s *Step) {
	m := s.Manifest
	w("### Step %d · %s · %s", s.Seq, m.TS.Format("15:04:05"), fmtutil.FmtSeconds(msDuration(m.DurMS), 1))
	if m.TTFTMS > 0 {
		w(" (ttft %s)", fmtutil.FmtSeconds(msDuration(m.TTFTMS), 1))
	}
	if m.UsageOK {
		fresh := m.Usage.In - m.Usage.CacheRead - m.Usage.CacheWrite
		if fresh < 0 {
			fresh = 0
		}
		w(" · %s/%s/%s", fmtTokens(fresh), fmtTokens(m.Usage.CacheRead), fmtTokens(m.Usage.Out))
	}
	w(" · %s\n\n", m.Endpoint)

	if s.Edge != nil {
		w("> 编辑: %s（%s）\n\n", s.Edge.Kind.String(), editStatsHint(*s.Edge))
	}

	if len(s.NewEvents) > 0 {
		w("**Messages**\n\n")
		for _, ev := range s.NewEvents {
			renderEvent(w, ev)
		}
	}

	renderLLMResponse(w, s)

	if s.NoReply {
		w("- ⏭️ **本轮 LLM 未实际回复**（NO_REPLY 或空内容）——下一轮可能是重试\n\n")
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
func renderLLMResponse(w func(string, ...any), s *Step) {
	if s.Reasoning == "" && s.RespText == "" && len(s.ToolCalls) == 0 {
		if s.Finish != "" {
			w("- finish: `%s`\n\n", s.Finish)
		}
		return
	}
	w("**LLM Response**\n\n")

	if s.Reasoning != "" {
		w("<details><summary>🤔 reasoning · %d 字符</summary>\n\n%s</details>\n\n",
			len([]rune(s.Reasoning)), codeFence(s.Reasoning))
	}
	if s.RespText != "" {
		w("<details><summary>💬 回复 · %s</summary>\n\n%s</details>\n\n",
			escapeHTML(preview(s.RespText)), codeFence(s.RespText))
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

func renderEvent(w func(string, ...any), ev *Event) {
	head := fmt.Sprintf("▸ %s", ev.Msg.Role)
	if ev.Msg.Text == "" {
		w("%s (空)\n\n", head)
		return
	}
	summary := preview(ev.Msg.Text)
	w("<details><summary>%s · %s</summary>\n\n%s</details>\n\n", head, escapeHTML(summary), codeFence(ev.Msg.Text))
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
// K/M-scaled like fmtutil.FmtTokens, but without its "EST" unit marker.
// That marker exists specifically so the live router log's pre-call
// ESTIMATE (core.RequestFacts.EstimatedTokens, used for a routing decision
// made BEFORE the call — see internal/fmtutil's doc comment) is never
// mistaken for billed usage at a glance. The numbers here are the request's
// recorded usage.In/CacheRead/Out — not an estimate — so reusing that
// marker would claim the opposite of what's true. Same rationale
// internal/report/detail.go's fmtTokensPlain already applies for its own
// (still-estimated, but contextually labeled) token count.
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
