// Ver 2026-07-29 11:30, by Sonnet 5

package story

import (
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
		w("> ⚠️ **本 journey 的开头是从上一段上下文断裂而来**（%s：%s，lcp=%d，覆盖率=%.0f%%）——两段之间的关系尚未确认，本轮（第一步）不做跨断点缝合，只如实标出断点。\n\n",
			j.Break.Edit.Kind.String(), breakReasonHint(j.Break.Edit.Kind), j.Break.Edit.LCP, j.Break.Edit.Coverage*100)
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
		w("> 编辑: %s (lcp=%d, cov=%.0f%%)\n\n", s.Edge.Kind.String(), s.Edge.LCP, s.Edge.Coverage*100)
	}

	for _, ev := range s.NewEvents {
		renderEvent(w, ev)
	}

	if len(s.ToolCalls) > 0 {
		names := make([]string, len(s.ToolCalls))
		for i, tc := range s.ToolCalls {
			names[i] = tc.Name
		}
		w("- 🔧 调用工具: %s\n", strings.Join(names, ", "))
	}
	if s.NoReply {
		w("- ⏭️ **本轮 LLM 未实际回复**（NO_REPLY 或空内容）——下一轮可能是重试\n")
	}
	if s.Finish != "" {
		w("- finish: `%s`\n", s.Finish)
	}
	w("\n")
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
// for the general pattern of what does and doesn't get shared).
func codeFence(s string) string {
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
	return f + "\n" + s + f + "\n"
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
