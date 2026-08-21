// Ver 2026-08-05, by Sonnet 5

// Direct unit tests for render_spine_args.go's toolCallLine and its
// helpers (scalarSummary, payloadBlock, capFull) — the per-tool-call
// argument renderer render_spine.go's decision spine calls into.
package story

import (
	"encoding/json"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

// jsonArgs marshals fields into a tool call's Args JSON — used wherever a
// test value might contain characters (newlines, quotes) unsafe to hand-
// build into a JSON string literal.
func jsonArgs(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("jsonArgs: %v", err)
	}
	return string(b)
}

// --- scalarSummary --------------------------------------------------------

func TestScalarSummary(t *testing.T) {
	t.Run("short string: not big", func(t *testing.T) {
		s, big := scalarSummary("hello")
		if s != "hello" || big {
			t.Errorf("scalarSummary(short string) = (%q, %v), want (%q, false)", s, big, "hello")
		}
	})
	t.Run("long string: big, and NOT truncated", func(t *testing.T) {
		long := strings.Repeat("x", spineShortFieldLen+50)
		s, big := scalarSummary(long)
		if s != long || !big {
			t.Errorf("scalarSummary(long string) = (len %d, big=%v), want (unchanged, true)", len(s), big)
		}
	})
	t.Run("multi-line string: big regardless of length", func(t *testing.T) {
		s, big := scalarSummary("a\nb")
		if s != "a\nb" || !big {
			t.Errorf("scalarSummary(multi-line string) = (%q, %v), want (%q, true)", s, big, "a\nb")
		}
	})
	t.Run("number: formatted without decimal noise", func(t *testing.T) {
		s, big := scalarSummary(float64(42))
		if s != "42" || big {
			t.Errorf("scalarSummary(42.0) = (%q, %v), want (\"42\", false)", s, big)
		}
	})
	t.Run("bool", func(t *testing.T) {
		s, big := scalarSummary(true)
		if s != "true" || big {
			t.Errorf("scalarSummary(true) = (%q, %v), want (\"true\", false)", s, big)
		}
	})
	t.Run("nil", func(t *testing.T) {
		s, big := scalarSummary(nil)
		if s != "null" || big {
			t.Errorf("scalarSummary(nil) = (%q, %v), want (\"null\", false)", s, big)
		}
	})
	t.Run("empty array: count only, big", func(t *testing.T) {
		s, big := scalarSummary([]any{})
		if s != "[0]" || !big {
			t.Errorf("scalarSummary(empty array) = (%q, %v), want (\"[0]\", true)", s, big)
		}
	})
	t.Run("array of strings: count + first element", func(t *testing.T) {
		s, big := scalarSummary([]any{"first", "second"})
		if s != "[2] first" || !big {
			t.Errorf("scalarSummary(string array) = (%q, %v), want (\"[2] first\", true)", s, big)
		}
	})
	t.Run("array of objects: count + first element's label-ish field", func(t *testing.T) {
		s, big := scalarSummary([]any{map[string]any{"step": "do the thing"}, map[string]any{"step": "do another"}})
		if s != "[2] do the thing" || !big {
			t.Errorf("scalarSummary(object array) = (%q, %v), want (\"[2] do the thing\", true)", s, big)
		}
	})
	t.Run("array of objects with no known label key: count only", func(t *testing.T) {
		s, big := scalarSummary([]any{map[string]any{"unrelated_key": "x"}})
		if s != "[1]" || !big {
			t.Errorf("scalarSummary(unlabeled object array) = (%q, %v), want (\"[1]\", true)", s, big)
		}
	})
	t.Run("nested object: count of its keys, big", func(t *testing.T) {
		s, big := scalarSummary(map[string]any{"a": 1, "b": 2})
		if s != "{2}" || !big {
			t.Errorf("scalarSummary(object) = (%q, %v), want (\"{2}\", true)", s, big)
		}
	})
}

// --- capFull ---------------------------------------------------------------

func TestCapFull(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("within cap: unchanged", func(t *testing.T) {
		s := strings.Repeat("x", spineFullCap)
		if got := capFull(s, et); got != s {
			t.Error("capFull should not alter a value exactly at the cap")
		}
	})
	t.Run("over cap: truncated with a localized note naming how much more", func(t *testing.T) {
		s := strings.Repeat("x", spineFullCap+37)
		got := capFull(s, et)
		wantPrefix := strings.Repeat("x", spineFullCap)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("capFull(over-cap) should keep the first spineFullCap runes unchanged")
		}
		if !strings.Contains(got, "37") {
			t.Errorf("capFull(over-cap) = %q, want it to name the exact overage (37)", got)
		}
		if strings.HasSuffix(got, "x") {
			t.Errorf("capFull(over-cap) = %q, must append a truncation note, not just cut silently", got)
		}
	})
}

// --- toolCallLine / payloadBlock -------------------------------------------

func TestToolCallLine(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("all-short fields: compact key=value pairs, sorted by key, inline", func(t *testing.T) {
		got := toolCallLine(tc("process", `{"action":"poll","sessionId":"abc123"}`), et)
		want := "🔧 `process`(action=poll, sessionId=abc123)\n\n"
		if got != want {
			t.Errorf("toolCallLine(process poll) = %q, want %q", got, want)
		}
	})

	t.Run("single short field: still the compact key=value form (short is short, one field or many)", func(t *testing.T) {
		got := toolCallLine(tc("read", `{"path":"a.go"}`), et)
		want := "🔧 `read`(path=a.go)\n\n"
		if got != want {
			t.Errorf("toolCallLine(read) = %q, want %q", got, want)
		}
	})

	t.Run("single field over spineInlineLen but one line: fenced, complete", func(t *testing.T) {
		longURL := "https://example.com/api?" + strings.Repeat("q=1&", 40)
		got := toolCallLine(tc("web_fetch", `{"url":"`+longURL+`"}`), et)
		if !strings.Contains(got, "```") {
			t.Errorf("toolCallLine(long single-line url) = %q, want a fenced block", got)
		}
		if !strings.Contains(got, longURL) {
			t.Errorf("toolCallLine(long single-line url) must contain the COMPLETE url, got %q", got)
		}
	})

	t.Run("multi-line command: folded, complete inside, not reduced to one line", func(t *testing.T) {
		cmd := "python3 << 'PYEOF'\nimport pandas\nresult = fetch_eastmoney_ipo_data()\nprint(result)\nPYEOF"
		got := toolCallLine(tc("exec", jsonArgs(t, map[string]any{"command": cmd})), et)
		if !strings.HasPrefix(got, "🔧 `exec` `command`: <details><summary>") {
			t.Errorf("toolCallLine(exec) = %q, want the folded-block lead-in", got)
		}
		if !strings.Contains(got, "</summary>\n\n```\n") || !strings.Contains(got, "\n```\n\n</details>") {
			t.Errorf("toolCallLine(exec) = %q, want a fenced block inside the fold", got)
		}
		for _, line := range strings.Split(cmd, "\n") {
			if !strings.Contains(got, line) {
				t.Errorf("toolCallLine(exec) missing line %q from the full command — got %q", line, got)
			}
		}
	})

	t.Run("two exec calls that only differ deep in a heredoc now render distinguishably", func(t *testing.T) {
		mk := func(inner string) string {
			return jsonArgs(t, map[string]any{"command": "python3 << 'PYEOF'\n" + inner + "\nPYEOF"})
		}
		a := toolCallLine(tc("exec", mk("result = fetch_eastmoney_ipo_data()")), et)
		b := toolCallLine(tc("exec", mk("result = fetch_akshare_stock_data()")), et)
		if a == b {
			t.Errorf("toolCallLine produced identical output for genuinely different commands: %q", a)
		}
		if !strings.Contains(a, "fetch_eastmoney_ipo_data") || !strings.Contains(b, "fetch_akshare_stock_data") {
			t.Errorf("toolCallLine must show each command's actual distinguishing content, got a=%q b=%q", a, b)
		}
	})

	t.Run("plan array: count + first step inline, not a raw JSON blob", func(t *testing.T) {
		got := toolCallLine(tc("update_plan", jsonArgs(t, map[string]any{"plan": []map[string]string{{"step": "获取A股打新数据"}, {"step": "分析数据"}}})), et)
		want := "🔧 `update_plan` `plan`: [2] 获取A股打新数据\n\n"
		if got != want {
			t.Errorf("toolCallLine(update_plan) = %q, want %q", got, want)
		}
	})

	t.Run("Args that isn't a JSON object at all: falls back to the raw text, complete", func(t *testing.T) {
		got := toolCallLine(tc("weird", "not json"), et)
		want := "🔧 `weird`: not json\n\n"
		if got != want {
			t.Errorf("toolCallLine(non-JSON args) = %q, want %q", got, want)
		}
	})

	t.Run("empty JSON object: falls back to the raw text", func(t *testing.T) {
		got := toolCallLine(tc("noop", "{}"), et)
		want := "🔧 `noop`: {}\n\n"
		if got != want {
			t.Errorf("toolCallLine(empty object) = %q, want %q", got, want)
		}
	})

	t.Run("oversized payload: fenced and capped, with a truncation note", func(t *testing.T) {
		huge := strings.Repeat("a", spineFullCap+100)
		got := toolCallLine(tc("write", `{"content":"`+huge+`"}`), et)
		if strings.Count(got, "a") < spineFullCap {
			t.Error("toolCallLine(oversized) should keep spineFullCap runes of the real content")
		}
		if !strings.Contains(got, "100") {
			t.Errorf("toolCallLine(oversized) = %q, want it to name the exact overage", got)
		}
	})

	// The following three lock in KNOWN_ISSUES §1.37/P12.2-3: real corpus
	// content (an HTML comment header, e.g. "<!-- Ver ... -->") landing in
	// a tool call's arguments must render literally, never get parsed as
	// HTML by whatever renders the Markdown — which is exactly what used
	// to silently swallow content on a real journey report.
	adversarial := "<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> real content after"

	t.Run("all-short fields: the value is escaped, not interpreted as HTML", func(t *testing.T) {
		got := toolCallLine(tc("note", jsonArgs(t, map[string]any{"text": adversarial})), et)
		if strings.Contains(got, "<!--") {
			t.Errorf("toolCallLine(all-short) leaked a raw HTML comment marker: %q", got)
		}
		if !strings.Contains(got, "&lt;!--") || !strings.Contains(got, "real content after") {
			t.Errorf("toolCallLine(all-short) = %q, want the escaped form with the trailing content preserved", got)
		}
	})

	t.Run("inline payload: the value is escaped", func(t *testing.T) {
		got := toolCallLine(tc("read", jsonArgs(t, map[string]any{"path": adversarial})), et)
		if strings.Contains(got, "<!--") {
			t.Errorf("toolCallLine(inline payload) leaked a raw HTML comment marker: %q", got)
		}
		if !strings.Contains(got, "&lt;!--") {
			t.Errorf("toolCallLine(inline payload) = %q, want the escaped form", got)
		}
	})

	t.Run("folded payload: the <summary> preview is escaped, the fenced full value is not (fences are already safe)", func(t *testing.T) {
		long := adversarial + strings.Repeat(" filler", 30)
		got := toolCallLine(tc("exec", jsonArgs(t, map[string]any{"command": long})), et)
		summaryEnd := strings.Index(got, "</summary>")
		if summaryEnd < 0 {
			t.Fatalf("toolCallLine(folded payload) missing a <summary> block: %q", got)
		}
		if strings.Contains(got[:summaryEnd], "<!--") {
			t.Errorf("toolCallLine(folded payload) summary leaked a raw HTML comment marker: %q", got[:summaryEnd])
		}
		if !strings.Contains(got[:summaryEnd], "&lt;!--") {
			t.Errorf("toolCallLine(folded payload) summary = %q, want the escaped form", got[:summaryEnd])
		}
		// Past the summary, inside the fenced block, the raw marker is
		// fine and expected — CommonMark doesn't parse HTML inside a
		// fenced code block, and the fenced copy must stay byte-identical
		// to the real argument.
		if !strings.Contains(got[summaryEnd:], "<!-- Ver 2026-07-24 14:45, by Sonnet 5 -->") {
			t.Errorf("toolCallLine(folded payload) fenced body should keep the raw, unescaped full value: %q", got[summaryEnd:])
		}
	})

	t.Run("other HTML metacharacters (< > &) are escaped too, not just <!--", func(t *testing.T) {
		got := toolCallLine(tc("note", jsonArgs(t, map[string]any{"text": `<script>alert(1)</script> foo & bar`})), et)
		if strings.Contains(got, "<script>") || strings.Contains(got, "</script>") {
			t.Errorf("toolCallLine leaked a raw <script> tag: %q", got)
		}
		if !strings.Contains(got, "&lt;script&gt;") || !strings.Contains(got, "foo &amp; bar") {
			t.Errorf("toolCallLine = %q, want &, <, > all escaped", got)
		}
	})
}
