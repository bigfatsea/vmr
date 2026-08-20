// Ver 2026-08-01, by Sonnet 5

package story

import (
	"fmt"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RenderMarkdown renders j as a self-contained Markdown document in lang:
// a system-prompt header (links to shared evidence, not inline text —
// render_md_sysprompt.go), an overview card, and the decision spine
// (render_spine.go/render_spine_step.go) — the spine is the ONLY per-Step
// content layer; there is no separate per-Task/per-Step "fact layer"
// walking the full event stream below it (P5.1 removed that duplication —
// every Step renders exactly once, in the spine, complete with its own
// "→ detail" link to the full record and, where applicable, the
// cross-record analysis facts — Edit/StitchEdge/SysChanged/Compaction —
// the deleted fact-layer used to carry, see spineTransitionLines in
// render_spine_step.go). m/findings are computed once by the caller
// (cmd/vmr/cmd_story.go's writeJourneyFile) and passed in rather than
// recomputed here, so the Markdown and JSON outputs for the same Journey
// are guaranteed to agree on both. Purely a view over already-computed
// facts — no judgment calls happen here, only formatting.
func RenderMarkdown(j *Journey, m Metrics, findings []Finding, lang i18n.Lang, reportMDExists bool) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	t := i18n.Story(lang)

	w("# Journey %s\n\n", j.ID)
	w("> %s\n\n", j.Title)
	w("%s", t.JourneyMeta(len(j.Tasks), stepCount(j), j.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"), j.To.In(fmtutil.DisplayZone).Format("15:04:05")))
	// Back links (P6.2d): vmr-stories.md is always safe to link — same
	// directory, this run just wrote it. vmr-report.md is another
	// command's product, existence-gated by the caller (reportMDExists)
	// so this function stays a pure formatter with no I/O of its own.
	reportLink := ""
	if reportMDExists {
		reportLink = "../vmr-report.md"
	}
	w("%s", t.BackLinkLine(reportLink))

	if j.Break != nil {
		w("%s", t.BreakWarning(j.Break.Edit.Kind.String(), breakReasonHint(j.Break.Edit.Kind, t), editStatsHint(j.Break.Edit, t)))
	}

	renderSystemPromptHeader(w, j, t)
	renderOverviewCard(w, j, m, lang)
	renderModelUsage(w, m, lang)
	renderDecisionSpine(w, j, findings, lang)
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

// renderCompactionInfo shows a stitch boundary's information-loss summary
// (CCR N-4's promise): token counts before/after, plus
// which file-path/URL-shaped entities the predecessor's tail mentioned but
// this step's opening doesn't — versus which survived. Folded by default
// like everything else here; the point is that it's THERE to check, not
// that every reader needs to open it every time. Called from the decision
// spine (render_spine_step.go's spineTransitionLines) — P5.1 removed the
// separate fact-layer that used to be this function's only caller, but the
// function itself, and the judgment it renders, are unchanged.
func renderCompactionInfo(w func(string, ...any), c *CompactionInfo, t i18n.StoryText) {
	ratio := "—"
	if c.TokensBefore > 0 {
		ratio = fmtutil.FmtPercent(float64(c.TokensAfter)/float64(c.TokensBefore), 1)
	}
	w("<details><summary>%s</summary>\n\n",
		t.CompactionSummary(fmtutil.FmtTokens(c.TokensBefore), fmtutil.FmtTokens(c.TokensAfter), ratio, len(c.SwallowedEntities), len(c.SurvivedEntities)))
	if len(c.SwallowedEntities) > 0 {
		w("%s", t.SwallowedEntities(strings.Join(c.SwallowedEntities, t.ListSep)))
	}
	if len(c.SurvivedEntities) > 0 {
		w("%s", t.SurvivedEntities(strings.Join(c.SurvivedEntities, t.ListSep)))
	}
	w("</details>\n\n")
}

// codeFence wraps s in a fenced code block whose fence is longer than any
// backtick run inside s, so message content (an agent quoting code, a tool
// result containing a Markdown snippet) can never break out of its block —
// a fixed ``` fence would silently corrupt the rest of the document the
// moment real content contains one. Same rationale/implementation as
// internal/report/render.go's codeFence, plus an optional lang tag
// (report's own codeFence has no caller that needs one).
//
// Deliberately duplicated rather than shared: this is a tiny, stable,
// purely cosmetic Markdown helper with no domain knowledge and no
// plausible reason to change again — the bar internal/fmtutil's own
// admission reasoning applies (a shared display helper earns its place
// when the two copies can drift in a way a reader would notice; this one
// cannot). Naming it here so it isn't re-raised as an oversight every time
// someone greps for duplicate helpers across the two commands.
//
// No longer takes a language tag (P5.1 removed this file's one caller that
// used one, the deleted fact-layer's tool-call JSON block) — every
// remaining caller across the package wants a plain fence.
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

// pctStr is story's local 0-decimal alias for fmtutil.FmtPercent —
// narrative text wants "92%", not report's denser-table "91.7%" (that
// package's own pctStr aliases the same function at 1 decimal instead).
func pctStr(f float64) string {
	return fmtutil.FmtPercent(f, 0)
}
