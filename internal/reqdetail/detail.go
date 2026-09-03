// Ver 2026-08-20 00:00, by Sonnet 5

// Per-request detail page: one audit record rendered as Markdown, following
// its physical path — ① client→vmr request, ② vmr→upstream attempts,
// ③ vmr→client response — with every comparison listing ALL items, marking
// only what changed (🟢 added / 🔴 removed / 🔶 changed) so the unchanged
// context stays visible but quiet.
//
// Render depends only on the record itself (plus its own and its lineage
// predecessor's ctxgraph.Manifest, and a taskseg.Profile for the two
// dialect-aware judgments — NoReply and chat-id extraction): no
// report.ReqInfo, no session/task position, no cross-record analysis
// conclusion. That is a deliberate subtraction, not an oversight — see
// docs/future-strategy/story_report_architecture_opus-5.md §7.6a for
// why these fields were cut: a leaf
// does not need to know its own position in a tree the caller already
// renders around it (session id, task id, turn number, the compaction
// links a report-side text match established) — that context belongs to
// whichever index or spine is linking to this page, not to the page
// itself.
package reqdetail

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// toolArgsInlineThreshold: tool-call args shorter than this render inline
// in the model output summary; longer ones go in a <details> fold (a multi-KB
// JSON blob would drown the document otherwise).
const toolArgsInlineThreshold = 600

// FileName is the deterministic filename for a detail page:
// {ts}_{virtual}_{real}_{outcome}_{hash8}.md. ts renders in the record's
// own timezone offset (never fmtutil.DisplayZone — see internal/story's
// journey.go deriveID for the existing precedent of this same exception),
// so the name is identical no matter which machine/timezone generates it.
// hash8 (ctxgraph.ReqHash8(req)) is what actually guarantees uniqueness;
// the rest is a purely decorative, human-scannable prefix — no batch
// counter, no collision suffix, because the coordinate is already unique.
//
// Deliberately built from four plain strings plus the coordinate, not from
// a *ctxgraph.Manifest or *audit.Record directly: a caller computing a
// "previous turn" link target only ever has the predecessor's Manifest in
// hand (see FileNameForManifest), while a caller naming the record it is
// about to render has the full audit.Record (see FileNameForRecord) — both
// must produce the exact same name for the same record, so they funnel
// through this one shared formatter instead of each re-deriving the format.
//
// The outcome segment is deliberately just the outcome string (e.g. "ok",
// "error"), without the structured error-class suffix the pre-P2 naming
// used to append (detailFileName's "outcome + "-" + errorClass"). That
// extra detail lives on audit.Attempt, which a bare Manifest does not
// carry — and this name must be reconstructible identically from either
// shape. It stays one click away inside the page itself (renderAttempts
// already shows it per-attempt); hash8, not the decorative prefix, is what
// actually guarantees this name is unique.
func FileName(ts time.Time, virtualModel, realModel, outcome, req string) string {
	if virtualModel == "" {
		virtualModel = "(rejected)"
	}
	if realModel == "" {
		realModel = "none"
	}
	return fmt.Sprintf("%s_%s_%s_%s_%s.md",
		ts.Format("20060102-150405.000"),
		sanitizeName(virtualModel), sanitizeName(realModel), sanitizeName(outcome),
		ctxgraph.ReqHash8(req))
}

// FileNameForRecord is FileName's convenience wrapper for the common case:
// the caller has rec's full body and its own scan coordinate in hand.
func FileNameForRecord(rec *audit.Record, path string, line int) string {
	return FileName(rec.TS, rec.Model, RealModel(rec), rec.Outcome, ctxgraph.ReqCoord(path, line))
}

// FileNameForManifest computes a record's filename from just its Manifest —
// the shape a "previous turn" link, or a future spine Step's "→ detail"
// link (see the architecture doc's P5.2), has on hand instead of the full
// record: correlating every record across a run and retaining each one's
// full body for this purpose would defeat the point of not physicalizing
// detail pages up front. real is parsed from m.Endpoint (the last attempt's
// own protocol:provider:model label — the same value LastEndpoint(rec)
// would have returned), the same fallback AttemptUpstream uses for older
// logs whose structured fields are empty.
func FileNameForManifest(m *ctxgraph.Manifest) string {
	_, _, real, _ := core.SplitEndpointLabel(m.Endpoint)
	return FileName(m.TS, m.Model, real, m.Outcome, m.Req)
}

// sessionFeatures is the record-only subset of what report.ReqInfo used to
// carry into rendering — recomputed here directly from rec (and prof for
// the two dialect-aware calls) instead of received from a caller, so
// Render depends on nothing but its own arguments. Every field here is a
// pure function of rec alone; see facts.go's package doc for why that
// matters.
type sessionFeatures struct {
	traceID, chatID, toolsSig string
	toolCalls                 []string
	finish, respText          string
	truncated, noReply        bool
	usage                     chatmsg.Usage
	usageOK                   bool
}

func extractSessionFeatures(rec *audit.Record, prof taskseg.Profile) sessionFeatures {
	var f sessionFeatures
	if tp := rec.Client.Request.Headers.Get("Traceparent"); tp != "" {
		if parts := strings.Split(tp, "-"); len(parts) >= 2 {
			f.traceID = parts[1]
		}
	}
	for _, at := range rec.Attempts {
		if AttemptErrorClass(at) == "truncated" && rec.Outcome == "ok" {
			f.truncated = true
		}
	}
	if body, ok := rec.Client.Request.Body.(map[string]any); ok {
		names := chatmsg.ToolNames(body)
		if _, hasTools := body["tools"]; hasTools || len(names) > 0 {
			f.toolsSig = ToolsSig(names)
		}
		msgs := chatmsg.Messages(body)
		f.chatID = prof.ChatID(msgs)
	}
	if rec.Client.Response != nil {
		f.usage, f.usageOK = chatmsg.ExtractUsageWithProtocol(rec.Client.Response.Body, rec.Protocol)
		if s := taskseg.ResponseSummary(rec.Client.Response.Body); s != nil {
			f.finish = s.Finish
			for _, tc := range s.ToolCalls {
				if tc.Name != "" {
					f.toolCalls = append(f.toolCalls, tc.Name)
				}
			}
			f.respText = fmtutil.CapStr(strings.TrimSpace(s.Content), 256<<10)
			// A deliberate no-reply skip (e.g. OpenClaw's empty-content or
			// explicit "NO_REPLY" marker convention — see prof.NoReply):
			// the record was sent successfully but the LLM skipped acting
			// on it.
			f.noReply = prof.NoReply(f.finish, f.respText)
		}
	}
	return f
}

// callsCell compacts a turn's tool calls ("exec×2, write"); "-" when none.
func callsCell(calls []string) string {
	if len(calls) == 0 {
		return "-"
	}
	counts := map[string]int{}
	var order []string
	for _, c := range calls {
		if counts[c] == 0 {
			order = append(order, c)
		}
		counts[c]++
	}
	parts := make([]string, 0, len(order))
	for _, c := range order {
		if counts[c] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", c, counts[c]))
		} else {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, ", ")
}

// ---- document skeleton ----

// Render renders one audit record's detail page. path/line are this
// record's own scan coordinate — used to compute Req when m is nil (a
// record whose body never parsed as a chat object, so
// ctxgraph.BuildManifest returned ok=false for it; such a record can never
// be another record's lineage predecessor either, so this case never
// affects anyone else's prev link). m is rec's own Manifest when it
// exists. prev is the immediately preceding Manifest in the SAME
// ctxgraph.Lineage (nil for a lineage's first record, or when m is nil) —
// NOT internal/story's stitched-chain predecessor; that distinction is a
// mid-tier concern this leaf does not know about.
// linkEvidence, when true, switches the system prompt and declared tool
// set from inline rendering to a link into ../evidence/ — see
// renderClientRequest and evidence.go. Render never touches the
// filesystem either way (true just changes which pure string it writes):
// the linked-to file's actual write is EnsureSysPromptEvidence/
// EnsureToolsEvidence's job, called separately by whichever caller drives
// EnsureRendered (see report/detail.go's writeOneDetail) — both that write
// and this link name the file with the exact same content hash, so the
// two can never disagree about the filename. Pass false to keep the old
// fully-inline rendering (every test that doesn't care about evidence
// linking uses this).
func Render(rec *audit.Record, path string, line int, m, prev *ctxgraph.Manifest, prof taskseg.Profile, lang i18n.Lang, linkEvidence bool) string {
	var b strings.Builder
	b.WriteString(renderFingerprint(lang, linkEvidence, m, prev, prof))
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	t := i18n.Detail(lang)
	f := extractSessionFeatures(rec, prof)

	// Title + overview.
	w("# %s [%s] · %s · %s · %s\n\n",
		DisplayModel(rec), rec.Protocol, outcomeMark(rec.Outcome), ms(rec.DurMS),
		rec.TS.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05.000"))
	w("%s", t.BackToIndexLine)
	renderSessionHeader(&b, m, prev, f, t)
	stream := t.StreamNo
	if rec.Stream {
		stream = t.StreamYes
	}
	tok := "-"
	if f.usageOK {
		tok = tokensTriple(f.usage.In, f.usage.CacheRead, f.usage.Out)
	}
	ttft := "-"
	if rec.TTFTMS > 0 {
		ttft = ms(rec.TTFTMS)
	}
	oh := t.OverviewHeaders
	w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n|---|---|---|---|---|---|---|---|---|\n",
		oh[0], oh[1], oh[2], oh[3], oh[4], oh[5], oh[6], oh[7], oh[8])
	w("| %s | %s | %s | %s | %s | %d | %s | %s | %s |\n\n",
		EscapeCell(DisplayModel(rec)), EscapeCell(LastEndpoint(rec)), outcomeMark(rec.Outcome),
		ms(rec.DurMS), ttft, len(rec.Attempts), stream, tok, EscapeCell(rec.Client.Addr))
	renderFactsLine(&b, rec, t)

	renderClientRequest(&b, rec, m, prev, f, t, linkEvidence)
	renderAttempts(&b, rec, t)
	renderClientResponse(&b, rec, path, line, t)
	return b.String()
}

// renderFactsLine surfaces vmr's own pre-routing analysis of this request —
// core.RequestFacts, computed once in server.go before any routing decision
// and carried through unchanged into audit.Record.Facts (never recomputed
// here from the stored body; see that field's doc comment). Placed right
// after the overview table, before the detailed sections: it's an overall
// judgment about the whole request, the same kind of "at a glance" fact the
// overview table already gives, not something scoped to any one section
// below. Silently omitted when Facts is nil — the request was rejected
// before fact computation ever ran (bad auth, unparseable JSON, missing
// model field), so there is nothing to show.
func renderFactsLine(b *strings.Builder, rec *audit.Record, t i18n.DetailText) {
	f := rec.Facts
	if f == nil {
		return
	}
	var caps []string
	if f.HasImage {
		caps = append(caps, "`image`")
	}
	if f.HasTools {
		caps = append(caps, "`tools`")
	}
	capsText := t.FactsCapsNone
	if len(caps) > 0 {
		capsText = strings.Join(caps, t.ListSep)
	}
	b.WriteString(t.FactsLine(capsText, fmtutil.FmtTokensPlain(f.EstimatedTokens)))
}

// renderSessionHeader emits the lineage-relationship line: the previous
// turn's link (when m has a predecessor in its own lineage) and this
// turn's own features (tool calls, trace id, chat id, tool signature). No
// session/task position (that's the caller's index/spine to render, not
// this leaf's) and no compaction cross-reference (a report-side §6.7
// analysis conclusion, not a fact of this record). See this file's package
// doc for the full "what was cut and why" list.
func renderSessionHeader(b *strings.Builder, m, prev *ctxgraph.Manifest, f sessionFeatures, t i18n.DetailText) {
	if m == nil {
		return
	}
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	if prev != nil {
		w("%s", t.PrevTurnLink(prev.TS.In(fmtutil.DisplayZone).Format("15:04:05.000"), FileNameForManifest(prev)))
	}
	var meta []string
	if len(f.toolCalls) > 0 {
		meta = append(meta, t.ThisTurnCalls+"**"+callsCell(f.toolCalls)+"**")
	}
	if f.traceID != "" {
		meta = append(meta, t.TraceLabel+"**"+fmtutil.CapStr(f.traceID, 16)+"**")
	}
	if f.chatID != "" {
		meta = append(meta, t.ChatLabel+"**"+f.chatID+"**")
	}
	if f.toolsSig != "" {
		meta = append(meta, "**"+f.toolsSig+"**")
	}
	// Tags are intentionally NOT shown in the header: per-record tags
	// like "compacted_session" fire on every turn after compaction
	// (the OpenClaw summary message re-injects it), so they look like
	// noise. (This carries the reasoning forward from the pre-P2
	// implementation; the tag computation itself never moved here — it
	// was always report-side aggregation, not a per-record fact.)
	if len(meta) > 0 {
		w("> %s\n", strings.Join(meta, " · "))
	}
	if f.truncated {
		w("%s", t.TruncatedWarning)
	}
	if f.noReply {
		w("%s", t.NoReplyWarning)
	}
	b.WriteString("\n")
}

// renderClientRequest emits section ①: what the caller sent to vmr. m/prev
// (both may be nil) drive the 🆕 delta highlight — which messages this
// turn actually added versus its lineage predecessor. deltaStart is
// recomputed here from ctxgraph.Classify(prev, m) rather than received
// from a caller: it is a pure function of the two manifests, the same
// computation internal/report's session.go attach() already runs for
// exactly this purpose (see edit.go's Classify), so both packages derive
// it identically instead of one trusting the other's precomputed copy.
func renderClientRequest(b *strings.Builder, rec *audit.Record, m, prev *ctxgraph.Manifest, f sessionFeatures, t i18n.DetailText, linkEvidence bool) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	req := rec.Client.Request
	w("## %s\n\n", t.ClientRequestTitle)
	msgs := chatmsg.Messages(req.Body)
	tools := chatmsg.ToolNames(req.Body)
	w("`%s %s` · body %s", req.Method, req.Path, fmtutil.FmtBytes(BodyBytes(req.Body)))
	if len(msgs) > 0 {
		w(" · %d messages", len(msgs))
	}
	if len(tools) > 0 {
		w(" · %d tools", len(tools))
	}
	w("\n\n")
	b.WriteString(Details(fmt.Sprintf("Headers (%d)", len(req.Headers)), headerTable(req.Headers, t)))

	obj, isObj := req.Body.(map[string]any)
	if !isObj {
		if req.Body != nil { // non-JSON body (rejected requests etc.)
			b.WriteString(Details(t.BodyNonJSON, codeFence(fmt.Sprintf("%v", req.Body))))
		}
		return
	}
	// Params: everything except the bulky conversation fields.
	params := map[string]any{}
	for k, v := range obj {
		if k != "messages" && k != "tools" && k != "system" && k != "input" && k != "instructions" {
			params[k] = v
		}
	}
	if len(params) > 0 {
		b.WriteString(Details(t.ParamsSummary(len(params)), codeFence(jsonIndent(params))))
	}
	if arr, _ := obj["tools"].([]any); len(arr) > 0 {
		if linkEvidence {
			filename := "tools-" + toolsHash8(tools) + ".md"
			w("%s", t.ToolsEvidenceLink(len(arr), filename))
		} else {
			var tb strings.Builder
			for i, tn := range arr {
				name := "?"
				if i < len(tools) {
					name = tools[i]
				}
				tb.WriteString(Details(EscapeHTML(name), codeFence(jsonIndent(tn))))
			}
			b.WriteString(Details(t.ToolsSummary(len(arr), EscapeHTML(taskseg.Preview(strings.Join(tools, ", ")))), tb.String()))
		}
	}
	if len(msgs) > 0 {
		w("\n### %s\n\n", t.MessagesTitle(len(msgs)))
		if line := roleStatLine(RoleTokens(req.Body), true, true); line != "" {
			w("%s", t.RoleTokenShare(line))
		}
		// leadSys is recomputed here (not read off m.LeadSys) so this
		// works even when m is nil — the same reasoning evidence.go's
		// leadingSystem gives for not depending on a caller-supplied
		// Manifest; both compute it identically from msgs alone, so
		// they can never disagree about which messages are "leading
		// system" ones.
		leadSys, sysText := leadingSystem(msgs)
		if linkEvidence && leadSys > 0 {
			filename := SysPromptEvidenceFileName(ctxgraph.Hash(md5.Sum([]byte(sysText))))
			w("%s", t.SysPromptEvidenceLink(fmtCount(len([]rune(sysText))), filename))
		}
		deltaStart := 0
		haveDelta := prev != nil && m != nil
		if haveDelta {
			e := ctxgraph.Classify(prev, m)
			deltaStart = m.LeadSys + e.LCP
			if deltaStart > 0 {
				w("%s", t.HistoryVsNewNote(deltaStart))
			}
		}
		foldedHistory := false
		for i, msg := range msgs {
			if linkEvidence && i < leadSys {
				continue // already covered by the evidence link above
			}
			// Messages before deltaStart are
			// byte-identical to what prev's own detail page already
			// rendered (deltaStart is exactly the point renderSessionHeader
			// tags with the "🆕 rest is new" split) — folding them into one
			// link instead of re-rendering each is the same cut the
			// evidence-link branch above already makes for the leading
			// system prompt, applied to the rest of the shared history.
			// prev == nil (haveDelta false: a lineage's first Step, or a
			// stitch boundary) always falls through to full rendering — the
			// chain has to have a starting point somewhere.
			if haveDelta && i < deltaStart {
				if !foldedHistory {
					w("%s", t.HistoryFoldedNote(deltaStart,
						prev.TS.In(fmtutil.DisplayZone).Format("15:04:05.000"), FileNameForManifest(prev)))
					foldedHistory = true
				}
				continue
			}
			prefix := ""
			if haveDelta && i >= deltaStart {
				prefix = "🆕 "
			}
			b.WriteString(renderMessageSection(i+1, msg, prefix, t))
			b.WriteString("\n")
		}
		if haveDelta {
			if n := len(msgs) - deltaStart; n > 0 {
				w("%s", t.IncrementNote(n, deltaStart))
			}
		}
	}
}

// renderAttempts emits section ②: every upstream try, each compared in full
// against the client request.
func renderAttempts(b *strings.Builder, rec *audit.Record, t i18n.DetailText) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("## %s\n\n", t.AttemptsTitle(len(rec.Attempts)))
	if len(rec.Attempts) == 0 {
		w("%s", t.NoAttempts)
		return
	}
	for i, a := range rec.Attempts {
		mark := "✅"
		status := ""
		if a.Response != nil {
			status = fmt.Sprintf(" · HTTP %d", a.Response.Status)
			if a.Response.Status >= 400 {
				mark = "❌"
			}
		}
		if a.Error != "" {
			mark = "❌"
		}
		w("### Attempt %d/%d · %s · %s%s · %s\n\n", i+1, len(rec.Attempts), a.Endpoint, mark, status, ms(a.DurMS))
		w("`%s`\n\n", a.URL)
		if a.Error != "" {
			w("❌ **error**: %s\n\n", a.Error)
		}

		w("%s", t.RequestDiffIntro)
		hdr, hchanged := diffHeaderTable(rec.Client.Request.Headers, a.Request.Headers, t)
		b.WriteString(Details(t.HeadersDiffSummary(unionLen(rec.Client.Request.Headers, a.Request.Headers), hchanged), hdr))
		renderBodyDiff(b, rec.Client.Request.Body, a.Request.Body, t)

		w("\n%s\n\n", t.ResponseTitle)
		switch {
		case a.Response == nil:
			w("%s", t.NoResponse)
		default:
			b.WriteString(Details(t.ResponseHeadersSummary(len(a.Response.Headers)), headerTable(a.Response.Headers, t)))
			if a.Response.Body == nil && a.Error == "" && a.Response.Status < 400 {
				w("%s", t.PassthroughBody)
				writeNorms(b, a.Norm, t)
			} else if a.Response.Body != nil {
				renderRawBody(b, t.ResponseBodyLabel, a.Response.Body, t)
				if len(a.Norm) > 0 {
					w("%s", t.NormStepsTitle)
					writeNorms(b, a.Norm, t)
				}
			} else if len(a.Norm) > 0 {
				w("%s", t.NormStepsTitle)
				writeNorms(b, a.Norm, t)
			}
			renderRawPreStrip(b, &a, t)
		}
	}
}

// renderRawPreStrip shows the upstream bytes exactly as received, from
// before a think_strip/thinking_process_strip rewrite ran — the reasoning
// content (and the raw SSE events that carried it) that never reaches the
// client. Captured only going forward (internal/respnorm); older
// logs have the norm step listed with no raw bytes to show.
func renderRawPreStrip(b *strings.Builder, a *audit.Attempt, t i18n.DetailText) {
	stripped := false
	for _, n := range a.Norm {
		if n == "think_strip" || n == "thinking_process_strip" {
			stripped = true
		}
	}
	if !stripped {
		return
	}
	if a.RawPreStrip == nil {
		b.WriteString(t.NoRawPreStripKept)
		return
	}
	if s, ok := a.RawPreStrip.(string); ok {
		b.WriteString(Details(t.RawPreStripSized(fmtutil.FmtBytes(int64(len(s)))), codeFence(s)))
		return
	}
	b.WriteString(Details(t.RawPreStrip, codeFence(jsonIndent(a.RawPreStrip))))
}

func writeNorms(b *strings.Builder, norms []string, t i18n.DetailText) {
	for _, n := range norms {
		desc, ok := t.NormDescriptions[n]
		if !ok {
			desc = t.UnknownNormStep
		}
		fmt.Fprintf(b, "- `%s` — %s\n", n, desc)
	}
	b.WriteString("\n")
}

// renderClientResponse emits section ③: what the client received, with the
// stream reassembled into the actual model output. path/line are the
// record's own coordinate (ctxgraph.ReqCoord) — P13.2's raw-SSE reference
// line uses it to point at `vmr replay -print -req <coord>` instead of
// inlining rec.Client.Response.Body's raw bytes a second time (see below).
func renderClientResponse(b *strings.Builder, rec *audit.Record, path string, line int, t i18n.DetailText) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("## %s\n\n", t.ClientResponseTitle)
	resp := rec.Client.Response
	if resp == nil {
		w("%s", t.NoResponseRecord)
		return
	}
	mark := "✅"
	if resp.Status >= 400 {
		mark = "❌"
	}
	w("%s HTTP %d · %s · body %s\n\n", mark, resp.Status,
		strings.Join(resp.Headers.Values("Content-Type"), ", "), fmtutil.FmtBytes(BodyBytes(resp.Body)))

	// Headers compared against the upstream response they were derived from
	// (the successful attempt) — the "received vs sent" view on headers.
	if up := successfulAttemptResponse(rec); up != nil {
		hdr, changed := diffHeaderTable(up.Headers, resp.Headers, t)
		b.WriteString(Details(t.ResponseHeadersDiffSummary(unionLen(up.Headers, resp.Headers), changed), hdr))
	} else {
		b.WriteString(Details(t.ResponseHeadersSummary(len(resp.Headers)), headerTable(resp.Headers, t)))
	}

	switch body := resp.Body.(type) {
	case nil:
		w("%s", t.EmptyBody)
	case string:
		if s := chatmsg.ReassembleSSE(body); s != nil {
			w("\n### %s\n\n", t.ModelOutputSSE(s.Events))
			renderStreamSummary(b, s, t)
			// The raw SSE bytes are a verbatim
			// copy of what renderStreamSummary just reassembled above —
			// unlike that reassembly (reasoning/content/tool_calls, which
			// IS interpretation), inlining the wire bytes a second time is
			// pure duplication (41% of a real-corpus detail page's size,
			// per the 2026-08-21 review). ctxgraph.ReqCoord + `vmr replay
			// -print -req` (P3.2) already exists as the "fetch this exact
			// record's raw bytes on demand" primitive — reuse it instead
			// of a second physical copy.
			w("%s", t.RawSSERef(s.Events, fmtutil.FmtBytes(int64(len(body))), ctxgraph.ReqCoord(path, line)))
		} else {
			renderRawBody(b, t.BodyNonJSONSSE, body, t)
		}
	default:
		if s, ok := chatmsg.FinalMessage(body); ok {
			w("\n### %s\n\n", t.ModelOutputTitle)
			renderStreamSummary(b, s, t)
		}
		b.WriteString(Details(t.FullResponseJSON(fmtutil.FmtBytes(BodyBytes(body))), codeFence(jsonIndent(body))))
	}
}

// successfulAttemptResponse finds the upstream response the client response
// was forwarded from (last attempt with a <400 response and no error).
func successfulAttemptResponse(rec *audit.Record) *audit.Message {
	for i := len(rec.Attempts) - 1; i >= 0; i-- {
		a := rec.Attempts[i]
		if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
			return a.Response
		}
	}
	return nil
}

// renderStreamSummary writes the reassembled model output: reasoning folded,
// content expanded (it is what the user came to read), tool calls, then
// finish reason and usage.
func renderStreamSummary(b *strings.Builder, s *chatmsg.StreamSummary, t i18n.DetailText) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	if s.Reasoning != "" {
		b.WriteString(Details(t.ReasoningChars(fmtCount(len([]rune(s.Reasoning)))), codeFence(s.Reasoning)))
	}
	if s.Content != "" {
		b.WriteString(codeFence(s.Content))
		b.WriteString("\n")
	}
	for _, tc := range s.ToolCalls {
		args := tc.Args
		if pretty := prettyJSONString(args); pretty != "" {
			args = pretty
		}
		if len(args) <= toolArgsInlineThreshold {
			w("🔧 **tool_call** `%s` [id=%s]\n\n%s\n", tc.Name, tc.ID, codeFence(args))
		} else {
			b.WriteString(Details(t.ToolCallArgsChars(EscapeHTML(tc.Name), EscapeHTML(tc.ID), fmtCount(len([]rune(args)))), codeFence(args)))
		}
	}
	if s.Finish != "" || s.Model != "" {
		w("%s", t.FinishModelLine(s.Finish, s.Model))
	}
}

// prettyJSONString re-indents a JSON string; "" when it isn't valid JSON.
func prettyJSONString(s string) string {
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return ""
	}
	return jsonIndent(v)
}

// renderRawBody folds an arbitrary recorded body (JSON or text).
func renderRawBody(b *strings.Builder, label string, body any, t i18n.DetailText) {
	switch v := body.(type) {
	case string:
		b.WriteString(Details(t.SizedLabel(label, fmtutil.FmtBytes(int64(len(v)))), codeFence(v)))
	default:
		b.WriteString(Details(t.SizedLabel(label, fmtutil.FmtBytes(BodyBytes(body))), codeFence(jsonIndent(body))))
	}
}
