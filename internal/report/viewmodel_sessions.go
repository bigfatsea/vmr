// Ver 2026-09-15, by Opus 5

// §6 会话 view model: per-session rollups and the compaction chains that
// link a summarized session to the one continuing from it. Only
// interactive-class sessions are listed here (scheduled single-shots live
// in the requests side's own rollups). Pairs with
// internal/i18n/report_sessions.go.
package report

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

func vmSessionsSection(rep *Report2, journeyLink map[string]string, lang i18n.Lang) SectionVM {
	t := i18n.Sessions(lang)
	sec := SectionVM{ID: "sessions", Title: t.Title}
	var interactive []SessionRow
	for _, s := range rep.Sessions {
		if s.Class == "interactive" {
			interactive = append(interactive, s)
		}
	}
	if len(interactive) == 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.NoInteractive})
		sec.Blocks = append(sec.Blocks, vmCompactionChainBlocks(rep, lang)...)
		return sec
	}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.TableNote})

	byClient := map[string][]SessionRow{}
	var seenOrder []string
	for _, s := range interactive {
		key := s.ClientKey
		if key == "" {
			key = "(unresolved)"
		}
		if _, ok := byClient[key]; !ok {
			seenOrder = append(seenOrder, key)
		}
		byClient[key] = append(byClient[key], s)
	}
	// rep.ByClient order (by request volume) first, then any extra key with
	// no ByClient entry - "(unresolved)" never carries a client_key_tag.
	clientOrder := make([]string, 0, len(rep.ByClient)+1)
	for _, c := range rep.ByClient {
		clientOrder = append(clientOrder, c.ClientKey)
	}
	for _, k := range seenOrder {
		found := false
		for _, o := range clientOrder {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			clientOrder = append(clientOrder, k)
		}
	}

	for _, ck := range clientOrder {
		rows := byClient[ck]
		if len(rows) == 0 {
			continue
		}
		sec.Blocks = append(sec.Blocks, ParaVM{Text: "**" + ck + "**\n\n"})
		head, tail := splitSessionLongTail(rows)
		tbl := &TableVM{Headers: t.TableHeaders[:]}
		for _, s := range head {
			vmSessionRow(tbl, s, journeyLink, t)
		}
		sec.Blocks = append(sec.Blocks, tbl)
		if len(tail) > 0 {
			ttbl := &TableVM{Headers: t.TableHeaders[:], Fold: t.LongTailOpen(len(tail), sessionsLongTailTurnCap)}
			for _, s := range tail {
				vmSessionRow(ttbl, s, journeyLink, t)
			}
			sec.Blocks = append(sec.Blocks, ttbl)
		}
	}
	// compaction chains: mermaid for chains ≥3 nodes
	sec.Blocks = append(sec.Blocks, vmCompactionChainBlocks(rep, lang)...)
	return sec
}

const (
	// sessionsHeadRows is how many sessions per client render un-collapsed
	// in §6 before the tail folds into a <details>.
	// sessionsLongTailTurnCap is the turn count at or below which a tail
	// session is "short" enough to fold. Real corpora put a few hundred
	// near-identical low-turn cron sessions behind every client's handful
	// of real conversations; without a fold §6 is ~45% of the whole macro
	// report and its signal drowns.
	sessionsHeadRows        = 20
	sessionsLongTailTurnCap = 12
)

// splitSessionLongTail divides one client's session rows — already
// sorted by Requests (turns) desc, the rep.Sessions order — into a head
// shown inline and a tail folded into a <details>. The tail is only ever
// the run of short (<= turn cap turns) sessions past the head cutoff: any
// session above that turn count stays in the head even if it sorts past
// the cutoff, so the fold never hides a substantial conversation. Returns
// (rows, nil) when there is nothing worth folding.
func splitSessionLongTail(rows []SessionRow) (head, tail []SessionRow) {
	if len(rows) <= sessionsHeadRows {
		return rows, nil
	}
	split := sessionsHeadRows
	for split < len(rows) && rows[split].Requests > sessionsLongTailTurnCap {
		split++
	}
	if split >= len(rows) {
		return rows, nil
	}
	return rows[:split], rows[split:]
}

// formatSessionTimeRange produces a compact "08-16 02:39 → 02:42" or
// "08-16 02:39 → 08-17 11:47" display in fmtutil.DisplayZone (问题 23).
func formatSessionTimeRange(fromStr, toStr string) string {
	if fromStr == "" && toStr == "" {
		return "-"
	}
	fromT, err1 := time.Parse(time.RFC3339, fromStr)
	toT, err2 := time.Parse(time.RFC3339, toStr)
	if err1 != nil && err2 != nil {
		return "-"
	}
	if err1 != nil {
		return toT.In(fmtutil.DisplayZone).Format("01-02 15:04")
	}
	if err2 != nil {
		return fromT.In(fmtutil.DisplayZone).Format("01-02 15:04")
	}
	f := fromT.In(fmtutil.DisplayZone)
	to := toT.In(fmtutil.DisplayZone)
	if f.Format("2006-01-02") == to.Format("2006-01-02") {
		return f.Format("01-02 15:04") + " → " + to.Format("15:04")
	}
	return f.Format("01-02 15:04") + " → " + to.Format("01-02 15:04")
}

func vmSessionRow(tbl *TableVM, s SessionRow, journeyLink map[string]string, t i18n.SessionsText) {
	outcome := "ok"
	if s.Errors > 0 {
		outcome = t.OutcomeOKErrors(s.Errors)
	}
	if s.Fallbacks > 0 {
		outcome += t.OutcomeFallback(s.Fallbacks)
	}
	id := s.ID
	if s.Alias != "" {
		// s.ID is content-addressed (l-<hash8>) and no longer the short
		// s%02d readers are used to scanning for within one report — show
		// both: the alias for at-a-glance reference, the real id (also
		// this row's join key against the journey index) for anyone
		// following a link.
		id = s.Alias + " (" + s.ID + ")"
	}
	// Link the row to its journey narrative when one was rendered for this
	// lineage in the same output root (问题 5 / P6.2c).
	if journey := journeyLink[s.ID]; journey != "" {
		id = "[" + id + "](journeys/" + journey + ")"
	}
	timeRange := formatSessionTimeRange(s.From, s.To)
	// EscapeHTML on top of row()'s own EscapeCell: the title is free-form
	// user/model text, so an unclosed "<!--" would otherwise swallow the
	// rest of the file in an HTML-aware renderer (B4).
	tbl.row(id, timeRange, reqdetail.EscapeHTML(truncateTitle(s.Title, 28)), strconv.Itoa(s.Requests), strconv.Itoa(s.Tasks),
		fmt.Sprintf("%s / %s / %s", fmtutil.FmtTokens(s.TokensInFresh), fmtutil.FmtTokens(s.TokensInCached), fmtutil.FmtTokens(s.TokensOut)),
		outcome)
}

// truncateTitle shortens s to at most maxRunes runes, appending an
// ellipsis when cut. Rune-based, unlike a byte slice - a truncated CJK
// title never splits a multi-byte UTF-8 sequence into mojibake.
func truncateTitle(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// vmCompactionChainBlocks builds head->current chains from
// SessionRow.ContinuedFrom and renders a mermaid flowchart for any chain
// with ≥3 nodes (≥2 compaction hops). Shorter chains are noted inline as
// text. (V2 A3 / M5)
func vmCompactionChainBlocks(rep *Report2, lang i18n.Lang) []BlockVM {
	t := i18n.Sessions(lang)
	byID := map[string]*SessionRow{}
	for i := range rep.Sessions {
		byID[rep.Sessions[i].ID] = &rep.Sessions[i]
	}
	// child -> parent (ContinuedFrom). A session is a "tip" if nobody continues from it.
	pointedTo := map[string]bool{}
	for _, s := range rep.Sessions {
		if s.ContinuedFrom != "" {
			pointedTo[s.ContinuedFrom] = true
		}
	}
	var blocks []BlockVM
	seen := map[string]bool{}
	for _, s := range rep.Sessions {
		if pointedTo[s.ID] {
			continue // not a tip
		}
		// walk back to head via ContinuedFrom links (string-only, no pointer)
		chain := []string{s.ID}
		parent := s.ContinuedFrom
		for parent != "" && byID[parent] != nil && !seen[parent] {
			chain = append(chain, parent)
			seen[parent] = true
			parent = byID[parent].ContinuedFrom
		}
		// reverse: head -> tip
		for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
			chain[i], chain[j] = chain[j], chain[i]
		}
		if len(chain) >= 3 {
			var b strings.Builder
			fmt.Fprintf(&b, "```mermaid\nflowchart LR\n")
			for i := 0; i < len(chain)-1; i++ {
				fmt.Fprintf(&b, "    %s[\"%s\"] -->|compacted| %s[\"%s\"]\n", chain[i], chain[i], chain[i+1], chain[i+1])
			}
			fmt.Fprintf(&b, "```\n\n")
			blocks = append(blocks, ParaVM{Text: b.String()})
		} else if len(chain) == 2 {
			// text arrow, inline note
			blocks = append(blocks, ParaVM{Text: t.CompactionChainNote(chain[1], chain[0])})
		}
	}
	return blocks
}
