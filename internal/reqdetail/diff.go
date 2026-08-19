// Ver 2026-08-20 00:00, by Sonnet 5

// Full-list diffs（全部列出，仅标记变化）— split out of detail.go once it
// crossed the archtest file-size budget; this half is the "compare two
// versions of the same shape" half (headers, body fields, messages, tools),
// detail.go is the document skeleton that calls into it.
package reqdetail

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// headerTable renders headers as a plain two-column table (no comparison).
func headerTable(h http.Header, t i18n.DetailText) string {
	if len(h) == 0 {
		return t.HeaderTableEmpty
	}
	keys := core.SortedKeys(h)
	var b strings.Builder
	fmt.Fprintf(&b, "| %s | %s |\n|---|---|\n", t.HeaderColumn, t.ValueColumn)
	for _, k := range keys {
		fmt.Fprintf(&b, "| %s | %s |\n", k, escapeCell(truncCell(strings.Join(h[k], ", "), 120, t)))
	}
	return b.String()
}

// diffHeaderTable lists the union of both header sets, marking additions,
// removals and changes relative to base. Returns the table and change count.
func diffHeaderTable(base, other http.Header, t i18n.DetailText) (string, int) {
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range other {
		keys[k] = true
	}
	sorted := core.SortedKeys(keys)

	var b strings.Builder
	changed := 0
	fmt.Fprintf(&b, "| | %s | %s |\n|---|---|---|\n", t.HeaderColumn, t.ValueColumn)
	for _, k := range sorted {
		bv, inBase := base[k]
		ov, inOther := other[k]
		// Named bs/ovs, not bs/os: "os" would shadow a stdlib os import if
		// this function ever needs one — harmless today, but a footgun for
		// whoever adds an os.* call here next.
		bs, ovs := strings.Join(bv, ", "), strings.Join(ov, ", ")
		switch {
		case !inBase:
			changed++
			fmt.Fprintf(&b, "| 🟢 | %s | %s |\n", k, escapeCell(truncCell(ovs, 120, t)))
		case !inOther:
			changed++
			fmt.Fprintf(&b, "| 🔴 | %s | ~~%s~~ |\n", k, escapeCell(truncCell(bs, 120, t)))
		case bs != ovs:
			changed++
			fmt.Fprintf(&b, "| 🔶 | %s | %s → %s |\n", k, escapeCell(truncCell(bs, 60, t)), escapeCell(truncCell(ovs, 60, t)))
		default:
			fmt.Fprintf(&b, "| | %s | %s |\n", k, escapeCell(truncCell(bs, 120, t)))
		}
	}
	return b.String(), changed
}

func unionLen(a, b http.Header) int {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return len(keys)
}

// renderBodyDiff compares the client request body against an attempt request
// body: top-level fields in one marked table, the bulky conversation fields
// (messages/tools/system) compared entry-by-entry so a single downscaled
// image or rewritten model name stands out without re-printing 75 messages.
func renderBodyDiff(b *strings.Builder, clientBody, attemptBody any, t i18n.DetailText) {
	cObj, cOK := clientBody.(map[string]any)
	aObj, aOK := attemptBody.(map[string]any)
	if !cOK || !aOK {
		if reflect.DeepEqual(clientBody, attemptBody) {
			b.WriteString(t.BodyIdentical)
		} else {
			b.WriteString(t.BodyDifferentNonJSON(fmtutil.FmtBytes(BodyBytes(clientBody)), fmtutil.FmtBytes(BodyBytes(attemptBody))))
			if attemptBody != nil {
				renderRawBody(b, t.UpstreamRequestBody, attemptBody, t)
			}
		}
		return
	}

	bulky := map[string]bool{"messages": true, "tools": true, "system": true, "input": true, "instructions": true}
	keys := map[string]bool{}
	for k := range cObj {
		keys[k] = true
	}
	for k := range aObj {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		if !bulky[k] {
			sorted = append(sorted, k)
		}
	}
	sort.Strings(sorted)

	var tb strings.Builder
	changed := 0
	fmt.Fprintf(&tb, "| | %s | %s |\n|---|---|---|\n", t.FieldColumn, t.ValueColumn)
	for _, k := range sorted {
		cv, inC := cObj[k]
		av, inA := aObj[k]
		switch {
		case !inC:
			changed++
			fmt.Fprintf(&tb, "| 🟢 | %s | %s |\n", k, escapeCell(summarizeVal(av, t)))
		case !inA:
			changed++
			fmt.Fprintf(&tb, "| 🔴 | %s | ~~%s~~ |\n", k, escapeCell(summarizeVal(cv, t)))
		case !reflect.DeepEqual(cv, av):
			changed++
			fmt.Fprintf(&tb, "| 🔶 | %s | %s → %s |\n", k, escapeCell(summarizeVal(cv, t)), escapeCell(summarizeVal(av, t)))
		default:
			fmt.Fprintf(&tb, "| | %s | %s |\n", k, escapeCell(summarizeVal(cv, t)))
		}
	}
	b.WriteString(Details(t.BodyFieldDiffSummary(len(sorted), changed), tb.String()))

	// system is compared as part of chatMessages (anthropic renders it as
	// message #0 on both sides), tools separately by entry.
	renderMessagesDiff(b, clientBody, attemptBody, t)
	renderToolsDiff(b, cObj["tools"], aObj["tools"], t)
}

// renderMessagesDiff lists every message on both sides, marking per-entry
// equality; changed/added entries carry the attempt-side full content folded
// inline so "what did the upstream actually get" needs no cross-referencing.
func renderMessagesDiff(b *strings.Builder, clientBody, attemptBody any, t i18n.DetailText) {
	cMsgs := chatmsg.Messages(clientBody)
	aMsgs := chatmsg.Messages(attemptBody)
	if len(cMsgs) == 0 && len(aMsgs) == 0 {
		return
	}
	n := len(cMsgs)
	if len(aMsgs) > n {
		n = len(aMsgs)
	}
	var tb strings.Builder
	changed := 0
	for i := 0; i < n; i++ {
		switch {
		case i >= len(aMsgs):
			changed++
			m := cMsgs[i]
			tb.WriteString(t.MsgClientOnly(i+1, m.Role, fmtCount(len([]rune(m.Text)))))
		case i >= len(cMsgs):
			changed++
			m := aMsgs[i]
			tb.WriteString(t.MsgUpstreamOnly(i+1, m.Role, fmtCount(len([]rune(m.Text)))))
			tb.WriteString(Details(t.UpstreamContent(i+1, m.Role), codeFence(m.Text)))
		case cMsgs[i] == aMsgs[i]:
			tb.WriteString(t.MsgUnchanged(i+1, cMsgs[i].Role, fmtCount(len([]rune(cMsgs[i].Text)))))
		default:
			changed++
			c, a := cMsgs[i], aMsgs[i]
			tb.WriteString(t.MsgChanged(i+1, c.Role, fmtCount(len([]rune(c.Text))), fmtCount(len([]rune(a.Text)))))
			tb.WriteString(Details(t.UpstreamContentSeeClient(i+1, a.Role), codeFence(a.Text)))
		}
	}
	label := t.MessagesDiffNoChange(n)
	if changed > 0 {
		label = t.MessagesDiffChanged(n, changed)
	}
	b.WriteString(Details(label, tb.String()))
}

// renderToolsDiff lists every declared tool, marking per-entry equality.
func renderToolsDiff(b *strings.Builder, clientTools, attemptTools any, t i18n.DetailText) {
	cArr, _ := clientTools.([]any)
	aArr, _ := attemptTools.([]any)
	if len(cArr) == 0 && len(aArr) == 0 {
		return
	}
	cNames := chatmsg.ToolNames(map[string]any{"tools": clientTools})
	aNames := chatmsg.ToolNames(map[string]any{"tools": attemptTools})
	name := func(names []string, i int) string {
		if i < len(names) {
			return names[i]
		}
		return "?"
	}
	n := len(cArr)
	if len(aArr) > n {
		n = len(aArr)
	}
	var tb strings.Builder
	changed := 0
	for i := 0; i < n; i++ {
		switch {
		case i >= len(aArr):
			changed++
			tb.WriteString(t.ToolClientOnly(name(cNames, i)))
		case i >= len(cArr):
			changed++
			tb.WriteString(t.ToolUpstreamOnly(name(aNames, i)))
			tb.WriteString(Details(t.ToolDefUpstream+escapeHTML(name(aNames, i)), codeFence(jsonIndent(aArr[i]))))
		case reflect.DeepEqual(cArr[i], aArr[i]):
			fmt.Fprintf(&tb, "- %s\n", name(cNames, i))
		default:
			changed++
			tb.WriteString(t.ToolChanged(name(cNames, i)))
			tb.WriteString(Details(t.ToolDefUpstream+escapeHTML(name(aNames, i))+t.SeeClientSide, codeFence(jsonIndent(aArr[i]))))
		}
	}
	label := t.ToolsDiffNoChange(n)
	if changed > 0 {
		label = t.ToolsDiffChanged(n, changed)
	}
	b.WriteString(Details(label, tb.String()))
}

// summarizeVal renders a JSON value compactly for a diff table cell: scalars
// verbatim (truncated), containers by size.
func summarizeVal(v any, t i18n.DetailText) string {
	switch tv := v.(type) {
	case nil:
		return "null"
	case string:
		return truncCell(fmt.Sprintf("%q", tv), 60, t)
	case []any:
		return t.ArrayItems(len(tv))
	case map[string]any:
		raw, _ := json.Marshal(tv)
		if len(raw) <= 60 {
			return string(raw)
		}
		return t.ObjectFields(len(tv))
	default:
		return fmt.Sprintf("%v", tv)
	}
}
