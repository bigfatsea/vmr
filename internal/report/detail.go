// Ver 2026-07-10 01:10, by Fable 5

// Per-request detail export: every audit record becomes one Markdown file
// under {out}/details/, named so lexical order equals arrival order. The
// document follows the request's physical path — ① client→vmr request,
// ② vmr→upstream attempts, ③ vmr→client response — and every comparison
// lists ALL items, marking only what changed (🟢 added / 🔴 removed /
// 🔶 changed) so the unchanged context stays visible but quiet.
package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"vmr/internal/audit"
)

// normDescriptions translates audit norm-trail steps (internal/router
// response normalizer) into human language for the detail files.
var normDescriptions = map[string]string{
	"model_rewrite":          "上游返回的真实模型名被改写回虚拟模型名",
	"done_appended":          "上游未发送 `data: [DONE]`，VMR 补发了终止哨兵",
	"think_strip":            "剥离了 `<think>…</think>` 推理块（防止思考内容进入会话历史）",
	"thinking_process_strip": "剥离了 \"Thinking Process:\" 纯文本推理草稿",
	"buffered":               "整个响应被缓冲后一次性归一化（非逐事件透传）",
	"resumed_stream":         "`<think>` 块结束后由缓冲恢复为流式转发",
	"soft_block_detected":    "检测到 MiniMax 软屏蔽标志（input/output_sensitive）——仅记录，字节未改动",
}

// WriteDetails renders every record in the given audit files into dir (one
// .md per record plus INDEX.md) and returns the number of record files
// written. Reruns overwrite deterministically.
func WriteDetails(paths []string, dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	type indexEntry struct {
		ts       time.Time
		file     string
		model    string
		endpoint string
		outcome  string
		durMS    int64
		attempts int
		usage    Usage
		usageOK  bool
	}
	var entries []indexEntry
	used := map[string]int{}

	for _, path := range paths {
		rc, err := openAuditFile(path)
		if err != nil {
			return len(entries), err
		}
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 1<<20), 128<<20)
		for sc.Scan() {
			var rec audit.Record
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				continue // Build already counts parse errors
			}
			name := detailFileName(&rec, used)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(renderDetail(&rec)), 0o644); err != nil {
				rc.Close()
				return len(entries), err
			}
			e := indexEntry{ts: rec.TS, file: name, model: displayModel(&rec),
				endpoint: lastEndpoint(&rec), outcome: rec.Outcome,
				durMS: rec.DurMS, attempts: len(rec.Attempts)}
			if rec.Client.Response != nil {
				e.usage, e.usageOK = ExtractUsage(rec.Client.Response.Body)
			}
			entries = append(entries, e)
		}
		rc.Close()
		if err := sc.Err(); err != nil {
			return len(entries), fmt.Errorf("%s: %w", path, err)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].ts.Equal(entries[j].ts) {
			return entries[i].ts.Before(entries[j].ts)
		}
		return entries[i].file < entries[j].file
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# VMR 请求详单索引\n\n共 %d 条记录\n\n", len(entries))
	b.WriteString("| 时间 | 模型 | 上游 | 结果 | 耗时 | 尝试 | tokens in/out | 文件 |\n|---|---|---|---|---|---|---|---|\n")
	for _, e := range entries {
		tok := "-"
		if e.usageOK {
			tok = fmtN(e.usage.In) + " / " + fmtN(e.usage.Out)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d | %s | [%s](./%s) |\n",
			e.ts.Format("01-02 15:04:05.000"), escapeCell(e.model), escapeCell(e.endpoint),
			outcomeMark(e.outcome), ms(e.durMS), e.attempts, tok, e.file, e.file)
	}
	if err := os.WriteFile(filepath.Join(dir, "INDEX.md"), []byte(b.String()), 0o644); err != nil {
		return len(entries), err
	}
	return len(entries), nil
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
// the client saw), or "-" when the request never reached an upstream.
func lastEndpoint(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}

// realModel is the model segment of the final attempt's endpoint
// ("openai/minimax/MiniMax-M3" → "MiniMax-M3").
func realModel(rec *audit.Record) string {
	ep := lastEndpoint(rec)
	if ep == "-" {
		return "none"
	}
	if i := strings.LastIndexByte(ep, '/'); i >= 0 {
		return ep[i+1:]
	}
	return ep
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

// errorClass buckets the last attempt error by its prefix, mirroring
// addAttempt's classing ("network: dial tcp…" → "network").
func errorClass(rec *audit.Record) string {
	for i := len(rec.Attempts) - 1; i >= 0; i-- {
		if e := rec.Attempts[i].Error; e != "" {
			if j := strings.IndexByte(e, ':'); j > 0 {
				return e[:j]
			}
			return e
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

// ---- document skeleton ----

func renderDetail(rec *audit.Record) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	// Title + overview.
	w("# %s [%s] · %s · %s · %s\n\n",
		displayModel(rec), rec.Protocol, outcomeMark(rec.Outcome), ms(rec.DurMS),
		rec.TS.Format("2006-01-02 15:04:05.000 -07:00"))
	stream := "否"
	if rec.Stream {
		stream = "是"
	}
	tok := "-"
	if rec.Client.Response != nil {
		if u, ok := ExtractUsage(rec.Client.Response.Body); ok {
			tok = fmt.Sprintf("%s / %s", fmtN(u.In), fmtN(u.Out))
			if u.CacheRead > 0 {
				tok += fmt.Sprintf("（缓存命中 %s）", fmtN(u.CacheRead))
			}
		}
	}
	ttft := "-"
	if rec.TTFTMS > 0 {
		ttft = ms(rec.TTFTMS)
	}
	w("| 虚拟模型 | 上游端点 | 结果 | 耗时 | 首字延迟 | 尝试 | stream | tokens in/out | 客户端 |\n|---|---|---|---|---|---|---|---|---|\n")
	w("| %s | %s | %s | %s | %s | %d | %s | %s | %s |\n\n",
		escapeCell(displayModel(rec)), escapeCell(lastEndpoint(rec)), outcomeMark(rec.Outcome),
		ms(rec.DurMS), ttft, len(rec.Attempts), stream, tok, rec.Client.Addr)

	renderClientRequest(&b, rec)
	renderAttempts(&b, rec)
	renderClientResponse(&b, rec)
	return b.String()
}

// renderClientRequest emits section ①: what the caller sent to vmr.
func renderClientRequest(b *strings.Builder, rec *audit.Record) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	req := rec.Client.Request
	w("## ① Client → VMR 请求\n\n")
	msgs := chatMessages(req.Body)
	tools := toolNames(req.Body)
	w("`%s %s` · body %s", req.Method, req.Path, fmtBytes(bodyBytes(req.Body)))
	if len(msgs) > 0 {
		w(" · %d messages", len(msgs))
	}
	if len(tools) > 0 {
		w(" · %d tools", len(tools))
	}
	w("\n\n")
	if req.BodyTruncated {
		w("⚠️ body 在记录时被截断（超出 audit 上限），以下内容不完整\n\n")
	}
	b.WriteString(details(fmt.Sprintf("Headers (%d)", len(req.Headers)), headerTable(req.Headers)))

	obj, isObj := req.Body.(map[string]any)
	if !isObj {
		if req.Body != nil { // non-JSON body (rejected requests etc.)
			b.WriteString(details("Body（非 JSON）", codeFence(fmt.Sprintf("%v", req.Body))))
		}
		return
	}
	// Params: everything except the bulky conversation fields.
	params := map[string]any{}
	for k, v := range obj {
		if k != "messages" && k != "tools" && k != "system" {
			params[k] = v
		}
	}
	if len(params) > 0 {
		b.WriteString(details(fmt.Sprintf("请求参数 (%d)", len(params)), codeFence(jsonIndent(params))))
	}
	if arr, _ := obj["tools"].([]any); len(arr) > 0 {
		var tb strings.Builder
		for i, t := range arr {
			name := "?"
			if i < len(tools) {
				name = tools[i]
			}
			tb.WriteString(details(escapeHTML(name), codeFence(jsonIndent(t))))
		}
		b.WriteString(details(fmt.Sprintf("Tools (%d): %s", len(arr), escapeHTML(preview(strings.Join(tools, ", ")))), tb.String()))
	}
	if len(msgs) > 0 {
		w("\n### Messages (%d)\n\n", len(msgs))
		if line := roleStatLine(roleChars(req.Body), true); line != "" {
			w("角色字符统计：%s\n\n", line)
		}
		for i, m := range msgs {
			b.WriteString(renderMessageSection(i+1, m))
			b.WriteString("\n")
		}
	}
}

// renderAttempts emits section ②: every upstream try, each compared in full
// against the client request.
func renderAttempts(b *strings.Builder, rec *audit.Record) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("## ② VMR → 上游（%d 次尝试）\n\n", len(rec.Attempts))
	if len(rec.Attempts) == 0 {
		w("无上游尝试（请求在路由前被拒绝）。\n\n")
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

		w("**请求对比**（相对 ①，🟢 新增 / 🔴 删除 / 🔶 变化，未标记 = 未变）\n\n")
		hdr, hchanged := diffHeaderTable(rec.Client.Request.Headers, a.Request.Headers)
		b.WriteString(details(fmt.Sprintf("Headers 对比 (%d 项，%d 处变化)", unionLen(rec.Client.Request.Headers, a.Request.Headers), hchanged), hdr))
		renderBodyDiff(b, rec.Client.Request.Body, a.Request.Body)

		w("\n**响应**\n\n")
		switch {
		case a.Response == nil:
			w("（无响应——请求未完成）\n\n")
		default:
			if a.Response.BodyTruncated {
				w("⚠️ 响应 body 在记录时被截断\n\n")
			}
			b.WriteString(details(fmt.Sprintf("响应 Headers (%d)", len(a.Response.Headers)), headerTable(a.Response.Headers)))
			if a.Response.Body == nil && a.Error == "" && a.Response.Status < 400 {
				w("body：**透传** —— 与 ③ 客户端收到的字节一致，仅差以下归一化步骤：\n\n")
				writeNorms(b, a.Norm)
			} else if a.Response.Body != nil {
				renderRawBody(b, "响应 body", a.Response.Body)
				if len(a.Norm) > 0 {
					w("归一化步骤：\n\n")
					writeNorms(b, a.Norm)
				}
			} else if len(a.Norm) > 0 {
				w("归一化步骤：\n\n")
				writeNorms(b, a.Norm)
			}
		}
	}
}

func writeNorms(b *strings.Builder, norms []string) {
	for _, n := range norms {
		desc, ok := normDescriptions[n]
		if !ok {
			desc = "（未知步骤）"
		}
		fmt.Fprintf(b, "- `%s` — %s\n", n, desc)
	}
	b.WriteString("\n")
}

// renderClientResponse emits section ③: what the client received, with the
// stream reassembled into the actual model output.
func renderClientResponse(b *strings.Builder, rec *audit.Record) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("## ③ VMR → Client 响应\n\n")
	resp := rec.Client.Response
	if resp == nil {
		w("（无响应记录——连接中断或请求被取消）\n\n")
		return
	}
	mark := "✅"
	if resp.Status >= 400 {
		mark = "❌"
	}
	w("%s HTTP %d · %s · body %s\n\n", mark, resp.Status,
		strings.Join(resp.Headers.Values("Content-Type"), ", "), fmtBytes(bodyBytes(resp.Body)))
	if resp.BodyTruncated {
		w("⚠️ body 在记录时被截断，以下内容不完整\n\n")
	}

	// Headers compared against the upstream response they were derived from
	// (the successful attempt) — the "received vs sent" view on headers.
	if up := successfulAttemptResponse(rec); up != nil {
		hdr, changed := diffHeaderTable(up.Headers, resp.Headers)
		b.WriteString(details(fmt.Sprintf("Headers 对比（相对上游响应，%d 项，%d 处变化）",
			unionLen(up.Headers, resp.Headers), changed), hdr))
	} else {
		b.WriteString(details(fmt.Sprintf("Headers (%d)", len(resp.Headers)), headerTable(resp.Headers)))
	}

	switch body := resp.Body.(type) {
	case nil:
		w("（body 为空）\n\n")
	case string:
		if s := reassembleSSE(body); s != nil {
			w("\n### 模型输出（由 %d 个 SSE 事件重组）\n\n", s.Events)
			renderStreamSummary(b, s)
			b.WriteString(details(fmt.Sprintf("原始 SSE 全文（%d events · %s）", s.Events, fmtBytes(int64(len(body)))), codeFence(body)))
		} else {
			renderRawBody(b, "Body（非 JSON/SSE）", body)
		}
	default:
		if s, ok := finalMessage(body); ok {
			w("\n### 模型输出\n\n")
			renderStreamSummary(b, s)
		}
		b.WriteString(details(fmt.Sprintf("完整响应 JSON（%s）", fmtBytes(bodyBytes(body))), codeFence(jsonIndent(body))))
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
func renderStreamSummary(b *strings.Builder, s *streamSummary) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	if s.Reasoning != "" {
		b.WriteString(details(fmt.Sprintf("🤔 reasoning · %s 字符", fmtCount(len([]rune(s.Reasoning)))), codeFence(s.Reasoning)))
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
		if len(args) <= foldThreshold {
			w("🔧 **tool_call** `%s` [id=%s]\n\n%s\n", tc.Name, tc.ID, codeFence(args))
		} else {
			b.WriteString(details(fmt.Sprintf("🔧 tool_call <code>%s</code> [id=%s] · args %s 字符",
				escapeHTML(tc.Name), escapeHTML(tc.ID), fmtCount(len([]rune(args)))), codeFence(args)))
		}
	}
	if s.Finish != "" || s.Model != "" {
		w("finish_reason: `%s` · model 字段: `%s`\n\n", s.Finish, s.Model)
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
func renderRawBody(b *strings.Builder, label string, body any) {
	switch v := body.(type) {
	case string:
		b.WriteString(details(fmt.Sprintf("%s（%s）", label, fmtBytes(int64(len(v)))), codeFence(v)))
	default:
		b.WriteString(details(fmt.Sprintf("%s（%s）", label, fmtBytes(bodyBytes(body))), codeFence(jsonIndent(body))))
	}
}

// ---- full-list diffs（全部列出，仅标记变化）----

// headerTable renders headers as a plain two-column table (no comparison).
func headerTable(h http.Header) string {
	if len(h) == 0 {
		return "（无）\n"
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("| Header | 值 |\n|---|---|\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "| %s | %s |\n", k, escapeCell(truncCell(strings.Join(h[k], ", "), 120)))
	}
	return b.String()
}

// diffHeaderTable lists the union of both header sets, marking additions,
// removals and changes relative to base. Returns the table and change count.
func diffHeaderTable(base, other http.Header) (string, int) {
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range other {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var b strings.Builder
	changed := 0
	b.WriteString("| | Header | 值 |\n|---|---|---|\n")
	for _, k := range sorted {
		bv, inBase := base[k]
		ov, inOther := other[k]
		bs, os := strings.Join(bv, ", "), strings.Join(ov, ", ")
		switch {
		case !inBase:
			changed++
			fmt.Fprintf(&b, "| 🟢 | %s | %s |\n", k, escapeCell(truncCell(os, 120)))
		case !inOther:
			changed++
			fmt.Fprintf(&b, "| 🔴 | %s | ~~%s~~ |\n", k, escapeCell(truncCell(bs, 120)))
		case bs != os:
			changed++
			fmt.Fprintf(&b, "| 🔶 | %s | %s → %s |\n", k, escapeCell(truncCell(bs, 60)), escapeCell(truncCell(os, 60)))
		default:
			fmt.Fprintf(&b, "| | %s | %s |\n", k, escapeCell(truncCell(bs, 120)))
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
func renderBodyDiff(b *strings.Builder, clientBody, attemptBody any) {
	cObj, cOK := clientBody.(map[string]any)
	aObj, aOK := attemptBody.(map[string]any)
	if !cOK || !aOK {
		if reflect.DeepEqual(clientBody, attemptBody) {
			b.WriteString("Body：与 ① 完全一致\n\n")
		} else {
			fmt.Fprintf(b, "Body：🔶 与 ① 不同（客户端 %s / 上游 %s，非 JSON 对象，无法逐字段对比）\n\n",
				fmtBytes(bodyBytes(clientBody)), fmtBytes(bodyBytes(attemptBody)))
			if attemptBody != nil {
				renderRawBody(b, "上游请求 body", attemptBody)
			}
		}
		return
	}

	bulky := map[string]bool{"messages": true, "tools": true, "system": true}
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
	tb.WriteString("| | 字段 | 值 |\n|---|---|---|\n")
	for _, k := range sorted {
		cv, inC := cObj[k]
		av, inA := aObj[k]
		switch {
		case !inC:
			changed++
			fmt.Fprintf(&tb, "| 🟢 | %s | %s |\n", k, escapeCell(summarizeVal(av)))
		case !inA:
			changed++
			fmt.Fprintf(&tb, "| 🔴 | %s | ~~%s~~ |\n", k, escapeCell(summarizeVal(cv)))
		case !reflect.DeepEqual(cv, av):
			changed++
			fmt.Fprintf(&tb, "| 🔶 | %s | %s → %s |\n", k, escapeCell(summarizeVal(cv)), escapeCell(summarizeVal(av)))
		default:
			fmt.Fprintf(&tb, "| | %s | %s |\n", k, escapeCell(summarizeVal(cv)))
		}
	}
	b.WriteString(details(fmt.Sprintf("Body 字段对比 (%d 项，%d 处变化)", len(sorted), changed), tb.String()))

	// system is compared as part of chatMessages (anthropic renders it as
	// message #0 on both sides), tools separately by entry.
	renderMessagesDiff(b, clientBody, attemptBody)
	renderToolsDiff(b, cObj["tools"], aObj["tools"])
}

// renderMessagesDiff lists every message on both sides, marking per-entry
// equality; changed/added entries carry the attempt-side full content folded
// inline so "what did the upstream actually get" needs no cross-referencing.
func renderMessagesDiff(b *strings.Builder, clientBody, attemptBody any) {
	cMsgs := chatMessages(clientBody)
	aMsgs := chatMessages(attemptBody)
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
			fmt.Fprintf(&tb, "- 🔴 #%d %s · %s 字符 · 仅客户端侧有\n", i+1, m.Role, fmtCount(len([]rune(m.Text))))
		case i >= len(cMsgs):
			changed++
			m := aMsgs[i]
			fmt.Fprintf(&tb, "- 🟢 #%d %s · %s 字符 · 仅上游侧有\n", i+1, m.Role, fmtCount(len([]rune(m.Text))))
			tb.WriteString(details(fmt.Sprintf("上游侧内容 #%d %s", i+1, m.Role), codeFence(m.Text)))
		case cMsgs[i] == aMsgs[i]:
			fmt.Fprintf(&tb, "- #%d %s · %s 字符\n", i+1, cMsgs[i].Role, fmtCount(len([]rune(cMsgs[i].Text))))
		default:
			changed++
			c, a := cMsgs[i], aMsgs[i]
			fmt.Fprintf(&tb, "- 🔶 #%d %s · %s → %s 字符\n", i+1, c.Role,
				fmtCount(len([]rune(c.Text))), fmtCount(len([]rune(a.Text))))
			tb.WriteString(details(fmt.Sprintf("上游侧内容 #%d %s（客户端侧见 ①）", i+1, a.Role), codeFence(a.Text)))
		}
	}
	label := fmt.Sprintf("Messages 对比 (%d 条，无变化)", n)
	if changed > 0 {
		label = fmt.Sprintf("Messages 对比 (%d 条，%d 处变化 🔶)", n, changed)
	}
	b.WriteString(details(label, tb.String()))
}

// renderToolsDiff lists every declared tool, marking per-entry equality.
func renderToolsDiff(b *strings.Builder, clientTools, attemptTools any) {
	cArr, _ := clientTools.([]any)
	aArr, _ := attemptTools.([]any)
	if len(cArr) == 0 && len(aArr) == 0 {
		return
	}
	cNames := toolNames(map[string]any{"tools": clientTools})
	aNames := toolNames(map[string]any{"tools": attemptTools})
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
			fmt.Fprintf(&tb, "- 🔴 %s · 仅客户端侧有\n", name(cNames, i))
		case i >= len(cArr):
			changed++
			fmt.Fprintf(&tb, "- 🟢 %s · 仅上游侧有\n", name(aNames, i))
			tb.WriteString(details("上游侧定义 "+escapeHTML(name(aNames, i)), codeFence(jsonIndent(aArr[i]))))
		case reflect.DeepEqual(cArr[i], aArr[i]):
			fmt.Fprintf(&tb, "- %s\n", name(cNames, i))
		default:
			changed++
			fmt.Fprintf(&tb, "- 🔶 %s · 定义有变化\n", name(cNames, i))
			tb.WriteString(details("上游侧定义 "+escapeHTML(name(aNames, i))+"（客户端侧见 ①）", codeFence(jsonIndent(aArr[i]))))
		}
	}
	label := fmt.Sprintf("Tools 对比 (%d 个，无变化)", n)
	if changed > 0 {
		label = fmt.Sprintf("Tools 对比 (%d 个，%d 处变化 🔶)", n, changed)
	}
	b.WriteString(details(label, tb.String()))
}

// summarizeVal renders a JSON value compactly for a diff table cell: scalars
// verbatim (truncated), containers by size.
func summarizeVal(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return truncCell(fmt.Sprintf("%q", t), 60)
	case []any:
		return fmt.Sprintf("[%d 项]", len(t))
	case map[string]any:
		raw, _ := json.Marshal(t)
		if len(raw) <= 60 {
			return string(raw)
		}
		return fmt.Sprintf("{%d 字段}", len(t))
	default:
		return fmt.Sprintf("%v", t)
	}
}
