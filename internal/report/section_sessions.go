// Ver 2026-08-01, by Sonnet 5

// §6 会话: per-session rollups and the compaction chains that link a
// summarized session to the one continuing from it.
package report

import (
	"fmt"
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// ---- §6 会话与任务 ----
// Only interactive-class sessions are listed here (scheduled single-shots -
// heartbeat/dream_diary/… - live in vmr-requests.md's own 定时任务 rollups,
// see requests.go); grouped by client (Chat User), matching vmr-requests.md's
// grouping, so a "类" column would be redundant (every row is interactive).
func renderSessions(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	t := i18n.Sessions(lang)
	w("## %s\n\n", t.Title)
	var interactive []SessionRow
	for _, s := range rep.Sessions {
		if s.Class == "interactive" {
			interactive = append(interactive, s)
		}
	}
	if len(interactive) == 0 {
		w("%s", t.NoInteractive)
		renderCompactionChains(w, rep, lang)
		return
	}

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

	th := t.TableHeaders
	for _, ck := range clientOrder {
		rows := byClient[ck]
		if len(rows) == 0 {
			continue
		}
		w("**%s**\n\n", ck)
		tbl := newTable(w, th[0], th[1], th[2], th[3], th[4], th[5])
		for _, s := range rows {
			renderSessionRow(tbl, s, t)
		}
		w("\n")
	}
	// compaction chains: mermaid for chains ≥3 nodes
	renderCompactionChains(w, rep, lang)
}

func renderSessionRow(tbl *mdTable, s SessionRow, t i18n.SessionsText) {
	outcome := "ok"
	if s.Errors > 0 {
		outcome = t.OutcomeOKErrors(s.Errors)
	}
	if s.Fallbacks > 0 {
		outcome += t.OutcomeFallback(s.Fallbacks)
	}
	id := s.ID
	if s.Alias != "" {
		// s.ID is now content-addressed (l-<hash8>) and no longer the
		// short s%02d readers are used to scanning for within one report
		// — show both: the alias for at-a-glance reference, the real id
		// (also this row's join key against story's Journey index) for
		// anyone following a link.
		id = s.Alias + " (" + s.ID + ")"
	}
	// EscapeHTML on top of row()'s own EscapeCell: the title is free-form
	// user/model text, so an unclosed "<!--" would otherwise swallow the
	// rest of the file in an HTML-aware renderer (B4).
	tbl.row(id, reqdetail.EscapeHTML(truncateTitle(s.Title, 28)), strconv.Itoa(s.Requests), strconv.Itoa(s.Tasks),
		fmt.Sprintf("%s / %s / %s", fmtutil.FmtTokens(s.TokensInFresh), fmtutil.FmtTokens(s.TokensInCached), fmtutil.FmtTokens(s.TokensOut)),
		outcome)
}

// truncateTitle shortens s to at most maxRunes runes, appending an ellipsis
// when cut. Rune-based, unlike a byte slice - a truncated CJK title never
// splits a multi-byte UTF-8 sequence into mojibake (e.g. a trailing "�").
func truncateTitle(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// renderCompactionChains builds head->current chains from SessionRow.ContinuedFrom
// and renders a mermaid flowchart for any chain with ≥3 nodes (≥2 compaction
// hops). Shorter chains are noted inline as text. (V2 A3 / M5)
func renderCompactionChains(w func(string, ...any), rep *Report2, lang i18n.Lang) {
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
			w("```mermaid\nflowchart LR\n")
			for i := 0; i < len(chain)-1; i++ {
				w("    %s[\"%s\"] -->|compacted| %s[\"%s\"]\n", chain[i], chain[i], chain[i+1], chain[i+1])
			}
			w("```\n\n")
		} else if len(chain) == 2 {
			// text arrow, inline note
			w("%s", t.CompactionChainNote(chain[1], chain[0]))
		}
	}
}
