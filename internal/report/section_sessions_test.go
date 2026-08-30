// Ver 2026-08-31, by Sonnet 5

package report

import (
	"fmt"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

func sessionRows(client string, turns ...int) []SessionRow {
	out := make([]SessionRow, len(turns))
	for i, n := range turns {
		out[i] = SessionRow{
			ID:           fmt.Sprintf("l-%08d", i),
			Alias:        fmt.Sprintf("s%d", i),
			Class:        "interactive",
			ClientKey:    client,
			Tasks:        1,
			TrafficStats: TrafficStats{Requests: n},
		}
	}
	return out
}

func renderSessionsString(t *testing.T, rep *Report2) string {
	t.Helper()
	var b strings.Builder
	renderSessions(func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }, rep, i18n.EN)
	return b.String()
}

// TestRenderSessions_LongTailFold: past sessionsHeadRows, the run of
// short (<= sessionsLongTailTurnCap turns) sessions folds into a <details>;
// a substantial session never folds even when it sorts past the cutoff;
// a small client stays fully inline.
func TestRenderSessions_LongTailFold(t *testing.T) {
	t.Run("short tail folds, head stays inline", func(t *testing.T) {
		// 15 big (20 turns) + 10 small (5 turns), already in Requests-desc order.
		turns := make([]int, 0, 25)
		for i := 0; i < 15; i++ {
			turns = append(turns, 20)
		}
		for i := 0; i < 10; i++ {
			turns = append(turns, 5)
		}
		rep := &Report2{Sessions: sessionRows("alice", turns...)}
		out := renderSessionsString(t, rep)

		openIdx := strings.Index(out, "<details><summary>")
		if openIdx < 0 {
			t.Fatalf("expected a <details> fold for the short tail:\n%s", out)
		}
		if !strings.Contains(out, "+ 5 more sessions (all ≤ 12 turns)") {
			t.Errorf("fold summary should report 5 folded sessions:\n%s", out)
		}
		// head session l-00000000 renders before the fold; folded session
		// l-00000024 (the 25th, a 5-turn one) renders after it.
		if hi := strings.Index(out, "l-00000000"); hi < 0 || hi > openIdx {
			t.Errorf("head session should render before the fold (hi=%d open=%d):\n%s", hi, openIdx, out)
		}
		if ti := strings.Index(out, "l-00000024"); ti < openIdx {
			t.Errorf("folded session should render inside/after the <details> (ti=%d open=%d):\n%s", ti, openIdx, out)
		}
		if !strings.Contains(out, "</details>") {
			t.Errorf("fold must be closed:\n%s", out)
		}
	})

	t.Run("substantial session past the cutoff is not folded", func(t *testing.T) {
		// 22 big (20 turns) + 3 small (5 turns): head extends past
		// sessionsHeadRows to keep every big session inline.
		turns := make([]int, 0, 25)
		for i := 0; i < 22; i++ {
			turns = append(turns, 20)
		}
		for i := 0; i < 3; i++ {
			turns = append(turns, 5)
		}
		rep := &Report2{Sessions: sessionRows("alice", turns...)}
		out := renderSessionsString(t, rep)

		if !strings.Contains(out, "+ 3 more sessions (all ≤ 12 turns)") {
			t.Errorf("only the 3 short sessions should fold:\n%s", out)
		}
		openIdx := strings.Index(out, "<details><summary>")
		// l-00000021 is the 22nd big session — sorts past sessionsHeadRows
		// but must stay inline.
		if bi := strings.Index(out, "l-00000021"); bi < 0 || bi > openIdx {
			t.Errorf("a 20-turn session must never be folded:\n%s", out)
		}
	})

	t.Run("small client renders fully inline", func(t *testing.T) {
		rep := &Report2{Sessions: sessionRows("alice", 30, 20, 10, 8, 3)}
		out := renderSessionsString(t, rep)
		if strings.Contains(out, "<details>") {
			t.Errorf("a client with <= sessionsHeadRows sessions should not fold:\n%s", out)
		}
	})
}

// TestRenderSessions_CompactionChainSurvivesFold locks in that the long-tail
// <details> does not swallow renderCompactionChains: the fold is per-client
// inside the loop, the chain mermaid comes after the loop, and it must stay
// there (a >=3-node ContinuedFrom chain still renders, past the last
// </details>).
func TestRenderSessions_CompactionChainSurvivesFold(t *testing.T) {
	turns := make([]int, 0, 25)
	for i := 0; i < 15; i++ {
		turns = append(turns, 20)
	}
	for i := 0; i < 10; i++ {
		turns = append(turns, 5)
	}
	rows := sessionRows("alice", turns...) // 25 sessions → the short tail folds
	rows[1].ContinuedFrom = rows[0].ID     // 0 <- 1 <- 2 : a 3-node compaction chain
	rows[2].ContinuedFrom = rows[1].ID
	out := renderSessionsString(t, &Report2{Sessions: rows})

	closeIdx := strings.LastIndex(out, "</details>")
	if closeIdx < 0 {
		t.Fatalf("expected the long-tail fold to be present:\n%s", out)
	}
	mermaidIdx := strings.Index(out, "```mermaid")
	if mermaidIdx < 0 {
		t.Fatalf("compaction-chain mermaid missing — the fold must not swallow renderCompactionChains:\n%s", out)
	}
	if mermaidIdx < closeIdx {
		t.Errorf("compaction chain should render after the fold closes (mermaid=%d close=%d):\n%s", mermaidIdx, closeIdx, out)
	}
	if !strings.Contains(out, "flowchart LR") {
		t.Errorf("expected a flowchart mermaid block for the 3-node chain:\n%s", out)
	}
}
