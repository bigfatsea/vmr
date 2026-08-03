// Ver 2026-08-01, by Sonnet 5

// Per-request detail export: every audit record becomes one Markdown file
// under {out}/details/, named so lexical order equals arrival order. The
// document follows the request's physical path — ① client→vmr request,
// ② vmr→upstream attempts, ③ vmr→client response — and every comparison
// lists ALL items, marking only what changed (🟢 added / 🔴 removed /
// 🔶 changed) so the unchanged context stays visible but quiet.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/i18n"
)

// toolArgsInlineThreshold: tool-call args shorter than this render inline
// in the model output summary; longer ones go in a <details> fold (a multi-KB
// JSON blob would drown the document otherwise).
const toolArgsInlineThreshold = 600

// detailWorkerCount bounds how many records get rendered+written
// concurrently in WriteDetails. Capped well below NumCPU on large machines:
// each job is two small-file writes, and past a point more goroutines just
// contend on the filesystem/GC instead of finishing faster.
func detailWorkerCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if n > 16 {
		n = 16
	}
	return n
}

// detailJob is one record's render-and-write work, queued for a worker. wg
// (optional) is Done() by the worker that processes this job — see
// detailWriter.submit.
type detailJob struct {
	rec  *audit.Record
	info *ReqInfo
	name string
	wg   *sync.WaitGroup
}

// writeOneDetail renders and writes one record's .md + .json pair. Errors
// are reported through recordErr rather than returned, since this runs on a
// worker goroutine, not the caller's.
func writeOneDetail(dir string, lang i18n.Lang, j detailJob, n *int64, recordErr func(error)) {
	if err := os.WriteFile(filepath.Join(dir, j.name), []byte(renderDetail(j.rec, j.info, lang)), 0o600); err != nil {
		recordErr(err)
		return
	}
	// Same-named .json alongside the .md: the raw record, for readers who
	// want to jq/query a single request instead of parsing the Markdown.
	if raw, err := json.MarshalIndent(j.rec, "", "  "); err == nil {
		jsonName := strings.TrimSuffix(j.name, ".md") + ".json"
		if err := os.WriteFile(filepath.Join(dir, jsonName), raw, 0o600); err != nil {
			recordErr(err)
			return
		}
	}
	atomic.AddInt64(n, 1)
}

// DetailWriter is a bounded worker pool that renders and writes one .md +
// one .json per submitted record — the reusable half of what used to be
// WriteDetails' own, self-contained implementation. It has two callers now:
// WriteDetails itself (drives it from its own file-scanning loop, one
// submit per record, batched per file via a *sync.WaitGroup so its progress
// line still reports real per-file elapsed time), and Build's onRecord hook
// (cmd/vmr constructs one and passes its Submit method — driven directly
// during Build's existing aggregation pass, no file scan of its own at all;
// see Build's doc comment for why). Every record's detail page depends only
// on that record's own (audit.Record, *ReqInfo) pair, so there's no
// cross-record ordering constraint either caller needs to preserve.
//
// Submit/submit are safe to call from multiple goroutines concurrently —
// the fallback-naming `used` map is mutex-guarded — even though both
// current callers happen to drive it from a single goroutine each
// (WriteDetails' own scan loop; Build's per-record loop).
type DetailWriter struct {
	dir      string
	lang     i18n.Lang
	usedMu   sync.Mutex
	used     map[string]int
	jobs     chan detailJob
	poolWG   sync.WaitGroup
	n        int64
	errMu    sync.Mutex
	firstErr error
}

// NewDetailWriter creates dir and starts the worker pool (detailWorkerCount
// goroutines) rendering every submitted record's detail page in lang.
// Callers must eventually call Close to drain it.
func NewDetailWriter(dir string, lang i18n.Lang) (*DetailWriter, error) {
	// 0o700/0o600 throughout: detail files carry the same full conversation
	// bodies as the audit JSONL they were derived from, which is
	// deliberately written 0600 — the exports must not silently loosen that.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	numWorkers := detailWorkerCount()
	dw := &DetailWriter{
		dir:  dir,
		lang: lang,
		used: map[string]int{},
		jobs: make(chan detailJob, numWorkers*4),
	}
	dw.poolWG.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer dw.poolWG.Done()
			for j := range dw.jobs {
				writeOneDetail(dw.dir, dw.lang, j, &dw.n, dw.recordErr)
				if j.wg != nil {
					j.wg.Done()
				}
			}
		}()
	}
	return dw, nil
}

func (dw *DetailWriter) recordErr(err error) {
	dw.errMu.Lock()
	if dw.firstErr == nil {
		dw.firstErr = err
	}
	dw.errMu.Unlock()
}

func (dw *DetailWriter) hasErr() bool {
	dw.errMu.Lock()
	defer dw.errMu.Unlock()
	return dw.firstErr != nil
}

// submit queues one record's render+write. wg is optional: pass one to wait
// for a batch to finish (WriteDetails waits per file); pass nil for
// fire-and-forget, relying on a later Close to drain everything (Build's
// hook, via Submit below — it has no natural "batch" boundary of its own).
func (dw *DetailWriter) submit(rec *audit.Record, info *ReqInfo, wg *sync.WaitGroup) {
	name := ""
	if info != nil {
		name = info.DetailFile // assigned in ts order by the analysis
	}
	if name == "" {
		dw.usedMu.Lock()
		name = detailFileName(rec, dw.used)
		dw.usedMu.Unlock()
	}
	if wg != nil {
		wg.Add(1)
	}
	dw.jobs <- detailJob{rec: rec, info: info, name: name, wg: wg}
}

// Submit queues one record's render+write, fire-and-forget — the exported
// entry point for a caller (e.g. Build's onRecord hook) with no per-batch
// wait of its own. A no-op once a prior job has already failed, matching
// WriteDetails' own short-circuit, so a broken output directory (e.g. disk
// full) doesn't queue thousands more doomed jobs once it's known bad.
func (dw *DetailWriter) Submit(rec *audit.Record, info *ReqInfo) {
	if dw.hasErr() {
		return
	}
	dw.submit(rec, info, nil)
}

// Close drains the pool and returns the total records written and the
// first error encountered, if any.
func (dw *DetailWriter) Close() (int, error) {
	close(dw.jobs)
	dw.poolWG.Wait()
	return int(atomic.LoadInt64(&dw.n)), dw.firstErr
}

// WriteDetails renders every record in the given audit files into dir (one
// .md + one same-named .json per record, in lang). Returns the number of
// record files written. Reruns overwrite deterministically. sess (optional,
// nil = plain mode) supplies the session grouping: detail headers gain
// session/task coordinates and a delta section.
//
// Callers must pass the same paths (same order) here and to AnalyzeSessions:
// filenames come from the analysis pass (assignNames, ts order — stable
// regardless of path order), but the per-record lookup is keyed path:line,
// and the no-analysis fallback (sess == nil, or a record the analysis never
// saw) numbers same-millisecond collisions in read order. cmd/vmr sorts the
// glob expansion once and feeds both — keep it that way.
//
// This is a standalone alternative to Build's onRecord hook — a second,
// independent read of the same audit files, for callers that want detail
// export without running the full aggregation pass. `vmr report` itself no
// longer calls this (Build's hook covers it in one pass instead); it stays
// for tests and any other standalone use.
//
// progress (optional, nil = silent) gets one line per input file.
func WriteDetails(paths []string, dir string, sess *SessionAnalysis, progress io.Writer, lang i18n.Lang) (int, error) {
	dw, err := NewDetailWriter(dir, lang)
	if err != nil {
		return 0, err
	}

	var outerErr error
	for fileIdx, path := range paths {
		fileStart := time.Now()
		before := atomic.LoadInt64(&dw.n)
		rc, err := audit.OpenLogFile(path)
		if err != nil {
			outerErr = err
			break
		}
		line := 0
		var fileWG sync.WaitGroup
		scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
			line++
			if dw.hasErr() {
				return
			}
			var rec audit.Record
			if err := json.Unmarshal(lineBytes, &rec); err != nil {
				return // Build already counts parse errors
			}
			info := sess.Lookup(path, line)
			dw.submit(&rec, info, &fileWG)
		}, func() { line++ }) // skipped lines still advance the counter so sess.Lookup keys stay aligned with AnalyzeSessions
		rc.Close()
		fileWG.Wait() // drain this file's jobs so the progress line below reflects real elapsed time
		if progress != nil {
			fmt.Fprintf(progress, "[%d/%d] %s  done: %d detail file pairs (%s)\n",
				fileIdx+1, len(paths), path, atomic.LoadInt64(&dw.n)-before, time.Since(fileStart).Round(time.Millisecond))
		}
		if scanErr != nil {
			outerErr = fmt.Errorf("%s: %w", path, scanErr)
			break
		}
		if dw.hasErr() {
			break
		}
	}

	n, err := dw.Close()
	if outerErr != nil {
		return n, outerErr
	}
	return n, err
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

// unsafeName matches filename characters we replace; keeps letters, digits,
// dot, underscore, hyphen.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeName(s string) string {
	s = unsafeName.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func displayModel(rec *audit.Record) string {
	if rec.Model == "" {
		return "(rejected)"
	}
	return rec.Model
}

// lastEndpoint is the endpoint of the final attempt (the one whose outcome
// the client saw, "protocol:provider:model"), or "-" when the request never
// reached an upstream.
func lastEndpoint(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}

// realModel is the upstream model of the final attempt.
func realModel(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "none"
	}
	if _, _, m := attemptUpstream(rec.Attempts[len(rec.Attempts)-1]); m != "" {
		return m
	}
	return "none"
}

// attemptUpstream returns the attempt's protocol/provider/model, preferring
// the structured fields (new logs) and falling back to splitting Endpoint
// for logs written before they existed — Endpoint was "/"-joined
// protocol/provider/model back then (":"-joined now). SplitN(…, 3) rather
// than a plain Split: the model segment can itself contain "/" (OpenRouter
// names like "z-ai/glm-5.2", a documented config example), so only the
// first two separators are structural — anything after them, slashes
// included, belongs to the model name.
func attemptUpstream(a audit.Attempt) (protocol, provider, model string) {
	if a.Protocol != "" || a.Provider != "" || a.Model != "" {
		return a.Protocol, a.Provider, a.Model
	}
	if parts := strings.SplitN(a.Endpoint, "/", 3); len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return "", "", ""
}

// detailFileName builds "{20060102-150405.000}_{virtual}_{real}_{outcome}.md".
// The zero-padded local timestamp leads so lexical sort equals time order;
// same-millisecond collisions get a numeric suffix.
func detailFileName(rec *audit.Record, used map[string]int) string {
	outcome := rec.Outcome
	if outcome == "error" {
		if cls := errorClass(rec); cls != "" {
			outcome += "-" + cls
		}
	}
	base := fmt.Sprintf("%s_%s_%s_%s",
		rec.TS.Format("20060102-150405.000"),
		sanitizeName(displayModel(rec)), sanitizeName(realModel(rec)), sanitizeName(outcome))
	used[base]++
	if n := used[base]; n > 1 {
		base = fmt.Sprintf("%s-%d", base, n)
	}
	return base + ".md"
}

// errorClass returns the last attempt's structured error class.
func errorClass(rec *audit.Record) string {
	for i := len(rec.Attempts) - 1; i >= 0; i-- {
		if cls := attemptErrorClass(rec.Attempts[i]); cls != "" {
			return cls
		}
	}
	return ""
}

func outcomeMark(outcome string) string {
	switch outcome {
	case "ok":
		return "✅ ok"
	case "canceled":
		return "🚫 canceled"
	default:
		return "❌ " + outcome
	}
}

// recordUsage returns this record's extracted token usage, preferring the
// already-computed info.Usage/info.UsageOK — session.go's collect() already
// ran ExtractUsage once for this exact record during session analysis — over
// recomputing it from the response body a second time. Recompute only
// happens when info is nil (ungrouped/rejected records, or detail rendering
// with no session analysis at all).
func recordUsage(rec *audit.Record, info *ReqInfo) (chatmsg.Usage, bool) {
	if info != nil {
		return info.Usage, info.UsageOK
	}
	if rec.Client.Response == nil {
		return chatmsg.Usage{}, false
	}
	return chatmsg.ExtractUsage(rec.Client.Response.Body)
}

// ---- document skeleton ----

func renderDetail(rec *audit.Record, info *ReqInfo, lang i18n.Lang) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	t := i18n.Detail(lang)

	// Title + overview.
	w("# %s [%s] · %s · %s · %s\n\n",
		displayModel(rec), rec.Protocol, outcomeMark(rec.Outcome), ms(rec.DurMS),
		rec.TS.Format("2006-01-02 15:04:05.000 -07:00"))
	renderSessionHeader(&b, info, t)
	stream := t.StreamNo
	if rec.Stream {
		stream = t.StreamYes
	}
	tok := "-"
	if u, ok := recordUsage(rec, info); ok {
		tok = tokensTriple(u.In, u.CacheRead, u.Out)
	}
	ttft := "-"
	if rec.TTFTMS > 0 {
		ttft = ms(rec.TTFTMS)
	}
	oh := t.OverviewHeaders
	w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n|---|---|---|---|---|---|---|---|---|\n",
		oh[0], oh[1], oh[2], oh[3], oh[4], oh[5], oh[6], oh[7], oh[8])
	w("| %s | %s | %s | %s | %s | %d | %s | %s | %s |\n\n",
		escapeCell(displayModel(rec)), escapeCell(lastEndpoint(rec)), outcomeMark(rec.Outcome),
		ms(rec.DurMS), ttft, len(rec.Attempts), stream, tok, rec.Client.Addr)
	renderFactsLine(&b, rec, t)

	renderClientRequest(&b, rec, info, t)
	renderAttempts(&b, rec, t)
	renderClientResponse(&b, rec, t)
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
	b.WriteString(t.FactsLine(capsText, fmtTokensPlain(f.EstimatedTokens)))
}

// fmtTokensPlain renders an estimated token count for human-facing detail
// pages ("27.3 KT") — same K/M scaling as fmtutil.FmtTokens but without its
// "EST" unit marker (that marker exists to keep the live router log's
// req=xxxKB/xxxESTKT column from being mistaken for billed usage at a
// glance; a labeled estimated-token-count field on a detail page already
// carries that context, so the terser unit reads better here).
func fmtTokensPlain(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1f MT", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1f KT", float64(n)/1000)
	default:
		return fmt.Sprintf("%d T", n)
	}
}

// renderSessionHeader emits the grouping coordinates line and, when this
// turn diverged from its parent, the notable events (replaced tail, system
// prompt change, truncation).
func renderSessionHeader(b *strings.Builder, info *ReqInfo, t i18n.DetailText) {
	if info == nil {
		return
	}
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	switch {
	case info.Compaction:
		w("%s", t.CompactionCallLabel)
		if info.Summarizes != "" {
			w("%s", t.CompactionSummarizes(info.Summarizes))
		}
		if info.ContinuesTo != "" {
			w("%s", t.CompactionContinues(info.ContinuesTo))
		}
		w("\n")
	case info.SessionID != "":
		w("%s", t.SessionTaskLine(info.SessionID, info.TaskID, info.TaskSeq, info.SessSeq))
		if info.Parent != nil {
			w("%s", t.PrevTurnLink(info.Parent.TS.Format("15:04:05.000"), info.Parent.DetailFile))
		}
		w("\n")
		var meta []string
		if len(info.ToolCalls) > 0 {
			meta = append(meta, t.ThisTurnCalls+"<strong>"+callsCell(info.ToolCalls)+"</strong>")
		}
		if info.TraceID != "" {
			meta = append(meta, t.TraceLabel+"<strong>"+capStr(info.TraceID, 16)+"</strong>")
		}
		if info.ChatID != "" {
			meta = append(meta, t.ChatLabel+"<strong>"+info.ChatID+"</strong>")
		}
		if info.ToolsSig != "" {
			meta = append(meta, "<strong>"+info.ToolsSig+"</strong>")
		}
		// Tags are intentionally NOT shown in the header: per-record tags
		// like "compacted_session" fire on every turn after compaction
		// (the OpenClaw summary message re-injects it), so they look like
		// noise.
		if len(meta) > 0 {
			w("> %s\n", strings.Join(meta, " · "))
		}
	default:
		return
	}
	if info.Truncated {
		w("%s", t.TruncatedWarning)
	}
	if info.NoReply {
		w("%s", t.NoReplyWarning)
	}
	b.WriteString("\n")
}

// renderClientRequest emits section ①: what the caller sent to vmr.
func renderClientRequest(b *strings.Builder, rec *audit.Record, info *ReqInfo, t i18n.DetailText) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	req := rec.Client.Request
	w("## %s\n\n", t.ClientRequestTitle)
	msgs := chatmsg.Messages(req.Body)
	tools := chatmsg.ToolNames(req.Body)
	w("`%s %s` · body %s", req.Method, req.Path, fmtBytes(bodyBytes(req.Body)))
	if len(msgs) > 0 {
		w(" · %d messages", len(msgs))
	}
	if len(tools) > 0 {
		w(" · %d tools", len(tools))
	}
	w("\n\n")
	b.WriteString(details(fmt.Sprintf("Headers (%d)", len(req.Headers)), headerTable(req.Headers, t)))

	obj, isObj := req.Body.(map[string]any)
	if !isObj {
		if req.Body != nil { // non-JSON body (rejected requests etc.)
			b.WriteString(details(t.BodyNonJSON, codeFence(fmt.Sprintf("%v", req.Body))))
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
		b.WriteString(details(t.ParamsSummary(len(params)), codeFence(jsonIndent(params))))
	}
	if arr, _ := obj["tools"].([]any); len(arr) > 0 {
		var tb strings.Builder
		for i, tn := range arr {
			name := "?"
			if i < len(tools) {
				name = tools[i]
			}
			tb.WriteString(details(escapeHTML(name), codeFence(jsonIndent(tn))))
		}
		b.WriteString(details(t.ToolsSummary(len(arr), escapeHTML(preview(strings.Join(tools, ", ")))), tb.String()))
	}
	if len(msgs) > 0 {
		w("\n### %s\n\n", t.MessagesTitle(len(msgs)))
		// info.RoleTokens is the exact same computation over the exact same
		// body (session.go's collect() already ran roleTokens(body) for
		// this record during session analysis) — reuse it instead of
		// re-walking the whole message tree here. Recompute only when there
		// is no ReqInfo at all (ungrouped/rejected records, or no session
		// analysis).
		var roleTok map[string]int64
		if info != nil {
			roleTok = info.RoleTokens
		} else {
			roleTok = roleTokens(req.Body)
		}
		if line := roleStatLine(roleTok, true, true); line != "" {
			w("%s", t.RoleTokenShare(line))
		}
		if info != nil && info.SessionID != "" && info.Parent != nil && info.DeltaStart > 0 {
			w("%s", t.HistoryVsNewNote(info.DeltaStart))
		}
		for i, m := range msgs {
			prefix := ""
			if info != nil && info.SessionID != "" && info.Parent != nil && i >= info.DeltaStart {
				prefix = "🆕 "
			}
			b.WriteString(renderMessageSection(i+1, m, prefix, t))
			b.WriteString("\n")
		}
		// Increment summary at the end of the message list.
		if info != nil && info.SessionID != "" && info.Parent != nil {
			n := len(msgs) - info.DeltaStart
			if n > 0 {
				w("%s", t.IncrementNote(n, info.DeltaStart))
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
		b.WriteString(details(t.HeadersDiffSummary(unionLen(rec.Client.Request.Headers, a.Request.Headers), hchanged), hdr))
		renderBodyDiff(b, rec.Client.Request.Body, a.Request.Body, t)

		w("\n%s\n\n", t.ResponseTitle)
		switch {
		case a.Response == nil:
			w("%s", t.NoResponse)
		default:
			b.WriteString(details(t.ResponseHeadersSummary(len(a.Response.Headers)), headerTable(a.Response.Headers, t)))
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
// client. Captured only going forward (internal/router/response.go); older
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
		b.WriteString(details(t.RawPreStripSized(fmtBytes(int64(len(s)))), codeFence(s)))
		return
	}
	b.WriteString(details(t.RawPreStrip, codeFence(jsonIndent(a.RawPreStrip))))
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
// stream reassembled into the actual model output.
func renderClientResponse(b *strings.Builder, rec *audit.Record, t i18n.DetailText) {
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
		strings.Join(resp.Headers.Values("Content-Type"), ", "), fmtBytes(bodyBytes(resp.Body)))

	// Headers compared against the upstream response they were derived from
	// (the successful attempt) — the "received vs sent" view on headers.
	if up := successfulAttemptResponse(rec); up != nil {
		hdr, changed := diffHeaderTable(up.Headers, resp.Headers, t)
		b.WriteString(details(t.ResponseHeadersDiffSummary(unionLen(up.Headers, resp.Headers), changed), hdr))
	} else {
		b.WriteString(details(t.ResponseHeadersSummary(len(resp.Headers)), headerTable(resp.Headers, t)))
	}

	switch body := resp.Body.(type) {
	case nil:
		w("%s", t.EmptyBody)
	case string:
		if s := chatmsg.ReassembleSSE(body); s != nil {
			w("\n### %s\n\n", t.ModelOutputSSE(s.Events))
			renderStreamSummary(b, s, t)
			b.WriteString(details(t.RawSSEFull(s.Events, fmtBytes(int64(len(body)))), codeFence(body)))
		} else {
			renderRawBody(b, t.BodyNonJSONSSE, body, t)
		}
	default:
		if s, ok := chatmsg.FinalMessage(body); ok {
			w("\n### %s\n\n", t.ModelOutputTitle)
			renderStreamSummary(b, s, t)
		}
		b.WriteString(details(t.FullResponseJSON(fmtBytes(bodyBytes(body))), codeFence(jsonIndent(body))))
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
		b.WriteString(details(t.ReasoningChars(fmtCount(len([]rune(s.Reasoning)))), codeFence(s.Reasoning)))
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
			b.WriteString(details(t.ToolCallArgsChars(escapeHTML(tc.Name), escapeHTML(tc.ID), fmtCount(len([]rune(args)))), codeFence(args)))
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
		b.WriteString(details(t.SizedLabel(label, fmtBytes(int64(len(v)))), codeFence(v)))
	default:
		b.WriteString(details(t.SizedLabel(label, fmtBytes(bodyBytes(body))), codeFence(jsonIndent(body))))
	}
}

// ---- full-list diffs（全部列出，仅标记变化）----

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
		// Named bs/ovs, not bs/os: "os" would shadow the imported os package
		// for the rest of this function — harmless today (nothing here calls
		// it), but a footgun for whoever adds an os.* call here next.
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
			b.WriteString(t.BodyDifferentNonJSON(fmtBytes(bodyBytes(clientBody)), fmtBytes(bodyBytes(attemptBody))))
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
	b.WriteString(details(t.BodyFieldDiffSummary(len(sorted), changed), tb.String()))

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
			tb.WriteString(details(t.UpstreamContent(i+1, m.Role), codeFence(m.Text)))
		case cMsgs[i] == aMsgs[i]:
			tb.WriteString(t.MsgUnchanged(i+1, cMsgs[i].Role, fmtCount(len([]rune(cMsgs[i].Text)))))
		default:
			changed++
			c, a := cMsgs[i], aMsgs[i]
			tb.WriteString(t.MsgChanged(i+1, c.Role, fmtCount(len([]rune(c.Text))), fmtCount(len([]rune(a.Text)))))
			tb.WriteString(details(t.UpstreamContentSeeClient(i+1, a.Role), codeFence(a.Text)))
		}
	}
	label := t.MessagesDiffNoChange(n)
	if changed > 0 {
		label = t.MessagesDiffChanged(n, changed)
	}
	b.WriteString(details(label, tb.String()))
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
			tb.WriteString(details(t.ToolDefUpstream+escapeHTML(name(aNames, i)), codeFence(jsonIndent(aArr[i]))))
		case reflect.DeepEqual(cArr[i], aArr[i]):
			fmt.Fprintf(&tb, "- %s\n", name(cNames, i))
		default:
			changed++
			tb.WriteString(t.ToolChanged(name(cNames, i)))
			tb.WriteString(details(t.ToolDefUpstream+escapeHTML(name(aNames, i))+t.SeeClientSide, codeFence(jsonIndent(aArr[i]))))
		}
	}
	label := t.ToolsDiffNoChange(n)
	if changed > 0 {
		label = t.ToolsDiffChanged(n, changed)
	}
	b.WriteString(details(label, tb.String()))
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
