// Ver 2026-08-05, by Sonnet 5

// toolCallLine and its helpers: renderDecisionSpine's (render_spine.go)
// per-tool-call argument renderer. Split out of render_spine.go purely to
// stay under internal/archtest's line budget — no behavior split, this is
// one self-contained piece of pure text formatting with no dependency on
// the rest of the decision-spine layer.
package story

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
)

// spineShortFieldLen classifies one JSON-decoded argument value as a short,
// flag-shaped scalar (an action name, a session id, a bool, a small count)
// versus the call's real payload (a command, a path, a URL, a search
// query, file content, a plan array, ...) — purely a classification
// threshold now, not a display truncation length: a value on either side
// of it is always shown complete (see spineInlineLen/spineFullCap for how
// the payload side is laid out).
const spineShortFieldLen = 60

// spineInlineLen: a payload value that's a single line no longer than this
// renders inline as "key: value", complete; anything longer, or anything
// with an embedded newline, renders in its own fenced code block instead
// (see toolCallLine) — still complete, up to spineFullCap — rather than a
// same-looking truncated prefix. This is the fix for the decision spine's
// original failure mode: every `exec` call's preview began with the
// identical `{"command": "`, and a heredoc's opening line
// ("python3 << 'PYEOF'") is boilerplate every such call shares too, so a
// fixed-width one-line preview made genuinely different commands
// indistinguishable. Showing the value's actual shape (inline when it
// fits on one reasonable line, fenced and complete otherwise) is the
// general fix — not a bigger number, a different display strategy.
const spineInlineLen = 120

// spineFullCap bounds a fenced payload block's total size — generous
// enough to show the large majority of real shell commands/scripts/URLs/
// queries in full, but not unbounded: a pathologically large single
// argument (a multi-KB file write's content, say) would otherwise make one
// Step's block dwarf the rest of the document. The byte-identical full
// value is always available one section down regardless — render_md.go's
// renderLLMResponse renders every tool call's complete prettyJSON args,
// unconditionally, never summarized — so capping here only affects the
// spine's own copy, never the reader's ability to see the rest.
const spineFullCap = 3000

// scalarSummary renders one JSON-decoded argument value as a short display
// string, plus whether it's the call's payload (spineShortFieldLen-or-
// longer, multi-line, or a nested array/object) as opposed to a short
// flag-shaped scalar (a bool, a number, a short string like an action name
// or session id). For an array, also peeks at its first element for a
// common label-ish field (several tool conventions' plan/step/item shapes
// use one of these key names) — a plain item count alone throws away
// exactly the information a plan- or list-shaped argument is built
// around. The returned string is never truncated here — toolCallLine
// decides how much of it to show.
func scalarSummary(raw any) (s string, big bool) {
	switch x := raw.(type) {
	case string:
		return x, len([]rune(x)) > spineShortFieldLen || strings.Contains(x, "\n")
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), false
	case bool:
		return strconv.FormatBool(x), false
	case nil:
		return "null", false
	case []any:
		count := "[" + strconv.Itoa(len(x)) + "]"
		if len(x) == 0 {
			return count, true
		}
		if first, ok := x[0].(string); ok {
			return count + " " + first, true
		}
		if first, ok := x[0].(map[string]any); ok {
			for _, k := range []string{"step", "title", "name", "description", "content", "text"} {
				if s, ok := first[k].(string); ok {
					return count + " " + s, true
				}
			}
		}
		return count, true
	case map[string]any:
		return "{" + strconv.Itoa(len(x)) + "}", true
	default:
		return "", false
	}
}

// capFull rune-truncates s to spineFullCap, appending t's localized "more
// content lives in this Step's own tool_call section" note when it does —
// this must never look like a silent cut, since it changes what a reader
// entering `-journey` to read this exact command believes they've seen.
func capFull(s string, t i18n.SpineText) string {
	r := []rune(s)
	if len(r) <= spineFullCap {
		return s
	}
	return string(r[:spineFullCap]) + t.SpineValueTruncated(len(r)-spineFullCap)
}

// toolCallLine renders one tool_call's complete display block: "🔧
// `name`" plus its arguments, shape-picked generically by the decoded
// value shapes present — Args' schema is entirely up to whichever tool the
// agent declared, so this must degrade sanely for a tool this package has
// never heard of, not a per-tool-name table:
//   - every field short/scalar (e.g. {"action":"poll","sessionId":"..."})
//     → every field, compactly, as key=value pairs, inline — already
//     complete, nothing to expand
//   - one field carries the real payload → that field, picked as the
//     single longest-rendered top-level field (in practice always the
//     payload — a command/query/content string, or a summarized array, is
//     never shorter than a tool's other scalar arguments); shown inline
//     ("key: value") when it's one line no longer than spineInlineLen,
//     else in its own fenced block, complete up to spineFullCap
//
// Falls back to the args' raw text (same fenced/inline choice, same cap)
// when Args doesn't decode as a non-empty JSON object at all — defensive:
// every tool call this package has seen so far does, but Args is
// passthrough text, not a guaranteed shape.
func toolCallLine(tc chatmsg.ToolCall, t i18n.SpineText) string {
	head := "🔧 `" + tc.Name + "`"

	var v map[string]any
	if err := json.Unmarshal([]byte(tc.Args), &v); err != nil || len(v) == 0 {
		return payloadBlock(head, "", tc.Args, t)
	}

	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order — map iteration isn't

	type field struct{ key, val string }
	fields := make([]field, len(keys))
	allShort := true
	longest := 0
	for i, k := range keys {
		s, big := scalarSummary(v[k])
		fields[i] = field{k, s}
		if big {
			allShort = false
		}
		if len([]rune(s)) > len([]rune(fields[longest].val)) {
			longest = i
		}
	}

	if allShort {
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = f.key + "=" + f.val
		}
		return head + "(" + strings.Join(parts, ", ") + ")\n\n"
	}
	f := fields[longest]
	return payloadBlock(head, f.key, f.val, t)
}

// payloadBlock lays out head (the "🔧 `name`" lead-in) plus one payload
// value, complete: inline as "head key: value" when it's a single line
// within spineInlineLen, else as its own fenced block under "head key:".
// key is "" for the no-JSON-object fallback (nothing to label).
func payloadBlock(head, key, val string, t i18n.SpineText) string {
	label := head
	if key != "" {
		label += " `" + key + "`"
	}
	if !strings.Contains(val, "\n") && len([]rune(val)) <= spineInlineLen {
		return label + ": " + val + "\n\n"
	}
	return label + ":\n" + codeFence(capFull(val, t)) + "\n"
}
