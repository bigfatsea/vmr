// Ver 2026-09-22 03:20, by Sonnet 5

// Pairs with internal/reqdetail/detail.go (the per-request details/*.md
// pages shared by internal/report and internal/journey).
package i18n

import "fmt"

// DetailText is reqdetail's rendering text, in one language.
type DetailText struct {
	NormDescriptions map[string]string
	UnknownNormStep  string

	StreamYes, StreamNo string
	OverviewHeaders     [9]string // vm, upstream endpoint, outcome, dur, ttft, attempts, stream, tokens, client

	FactsCapsNone string
	ListSep       string // joins e.g. multiple detected capabilities ("`image`、`tools`" vs "`image`, `tools`")
	FactsLine     func(caps, estTokens string) string

	// BackToIndexLine is the "detail → request browser" return edge
	// (P6.2e). A detail page lives at requests/details/r-<...>.md; the
	// browser skeleton is two levels up at the report root, and its path
	// never varies regardless of which command rendered this page
	// (generation-time guarantee, same class as PrevTurnLink below).
	// D7 deleted the markdown request index this used to point at.
	BackToIndexLine  string
	PrevTurnLink     func(ts, file string) string
	ThisTurnCalls    string // "本轮调用: " prefix
	TraceLabel       string
	ChatLabel        string
	TruncatedWarning string
	NoReplyWarning   string

	ClientRequestTitle    string
	BodyNonJSON           string
	ParamsSummary         func(n int) string
	ToolsSummary          func(n int, preview string) string
	SysPromptEvidenceLink func(chars, file string) string
	ToolsEvidenceLink     func(n int, file string) string
	MessagesTitle         func(n int) string
	RoleTokenShare        func(line string) string
	HistoryVsNewNote      func(deltaStart int) string
	// HistoryFoldedNote (P13.3) replaces re-rendering each message before
	// deltaStart with one link to the previous turn's own detail page —
	// those messages are byte-identical to what that page already shows.
	HistoryFoldedNote func(n int, ts, file string) string
	IncrementNote     func(n, deltaStart int) string

	AttemptsTitle          func(n int) string
	NoAttempts             string
	RequestDiffIntro       string
	HeadersDiffSummary     func(union, changed int) string
	ResponseTitle          string
	NoResponse             string
	ResponseHeadersSummary func(n int) string
	PassthroughBody        string
	NormStepsTitle         string

	NoRawPreStripKept string
	RawPreStripSized  func(size string) string
	RawPreStrip       string

	ClientResponseTitle        string
	NoResponseRecord           string
	ResponseHeadersDiffSummary func(union, changed int) string
	EmptyBody                  string
	ModelOutputSSE             func(events int) string
	// RawSSERef points at the coordinate-based retrieval primitive
	// (`vmr replay -print -req <coord>`) instead of inlining the raw
	// SSE bytes a second time — renderStreamSummary just reassembled the
	// same bytes into reasoning/content/tool_calls above this line, which
	// is interpretation and stays; this was pure duplication.
	RawSSERef        func(events int, size, coord string) string
	BodyNonJSONSSE   string
	ModelOutputTitle string
	FullResponseJSON func(size string) string

	ReasoningChars    func(chars string) string
	ToolCallArgsChars func(name, id, chars string) string
	FinishModelLine   func(finish, model string) string

	HeaderTableEmpty string
	HeaderColumn     string
	ValueColumn      string

	BodyIdentical        string
	BodyDifferentNonJSON func(clientSize, upstreamSize string) string
	UpstreamRequestBody  string
	FieldColumn          string
	BodyFieldDiffSummary func(n, changed int) string

	MsgClientOnly            func(idx int, role, chars string) string
	MsgUpstreamOnly          func(idx int, role, chars string) string
	UpstreamContent          func(idx int, role string) string
	MsgUnchanged             func(idx int, role, chars string) string
	MsgChanged               func(idx int, role, charsC, charsA string) string
	UpstreamContentSeeClient func(idx int, role string) string
	MessagesDiffNoChange     func(n int) string
	MessagesDiffChanged      func(n, changed int) string

	ToolClientOnly    func(name string) string
	ToolUpstreamOnly  func(name string) string
	ToolDefUpstream   string
	ToolChanged       func(name string) string
	SeeClientSide     string
	ToolsDiffNoChange func(n int) string
	ToolsDiffChanged  func(n, changed int) string

	ArrayItems   func(n int) string
	ObjectFields func(n int) string

	ResponseBodyLabel string
	SizedLabel        func(label, size string) string

	TruncSuffix    func(n int) string
	EmptyMessage   func(prefix, head string) string
	MessageSummary func(prefix, head, chars, preview string) string
}

// detailRow holds this file's literal templates, one row per Lang (Table's
// own doc comment). No field here has internal branching — every
// interpolated field is pure concatenation with the same argument order in
// both languages (unlike report_doc.go's ToolWarn), so no field needs
// explicit %[n] indices.
type detailRow struct {
	normDescriptions map[string]string
	unknownNormStep  string

	streamYes, streamNo string
	overviewHeaders     [9]string

	factsCapsNone string
	listSep       string
	factsLineFmt  string

	backToIndexLine  string
	prevTurnLinkFmt  string
	thisTurnCalls    string
	traceLabel       string
	chatLabel        string
	truncatedWarning string
	noReplyWarning   string

	clientRequestTitle       string
	bodyNonJSON              string
	paramsSummaryFmt         string
	toolsSummaryFmt          string
	sysPromptEvidenceLinkFmt string
	toolsEvidenceLinkFmt     string
	messagesTitleFmt         string
	roleTokenShareFmt        string
	historyVsNewNoteFmt      string
	historyFoldedNoteFmt     string
	incrementNoteFmt         string

	attemptsTitleFmt          string
	noAttempts                string
	requestDiffIntro          string
	headersDiffSummaryFmt     string
	responseTitle             string
	noResponse                string
	responseHeadersSummaryFmt string
	passthroughBody           string
	normStepsTitle            string

	noRawPreStripKept   string
	rawPreStripSizedFmt string
	rawPreStrip         string

	clientResponseTitle           string
	noResponseRecord              string
	responseHeadersDiffSummaryFmt string
	emptyBody                     string
	modelOutputSSEFmt             string
	rawSSERefFmt                  string
	bodyNonJSONSSE                string
	modelOutputTitle              string
	fullResponseJSONFmt           string

	reasoningCharsFmt    string
	toolCallArgsCharsFmt string
	finishModelLineFmt   string

	headerTableEmpty string
	headerColumn     string
	valueColumn      string

	bodyIdentical           string
	bodyDifferentNonJSONFmt string
	upstreamRequestBody     string
	fieldColumn             string
	bodyFieldDiffSummaryFmt string

	msgClientOnlyFmt            string
	msgUpstreamOnlyFmt          string
	upstreamContentFmt          string
	msgUnchangedFmt             string
	msgChangedFmt               string
	upstreamContentSeeClientFmt string
	messagesDiffNoChangeFmt     string
	messagesDiffChangedFmt      string

	toolClientOnlyFmt    string
	toolUpstreamOnlyFmt  string
	toolDefUpstream      string
	toolChangedFmt       string
	seeClientSide        string
	toolsDiffNoChangeFmt string
	toolsDiffChangedFmt  string

	arrayItemsFmt   string
	objectFieldsFmt string

	responseBodyLabel string
	sizedLabelFmt     string

	truncSuffixFmt    string
	emptyMessageFmt   string
	messageSummaryFmt string
}

var detailRows = Table[detailRow]{
	EN: {
		normDescriptions: map[string]string{
			"model_rewrite":                     "The real upstream model name was rewritten back to the virtual model name",
			"done_appended":                     "Upstream didn't send `data: [DONE]`; VMR appended the terminator sentinel",
			"think_strip":                       "Stripped a `<think>…</think>` reasoning block (kept it out of the conversation history)",
			"thinking_process_strip":            "Stripped a plain-text \"Thinking Process:\" reasoning draft",
			"buffered":                          "The whole response was buffered and normalized as a single unit (not forwarded event-by-event)",
			"resumed_stream":                    "Resumed streaming forwarding after buffering through a `<think>` block",
			"soft_block_detected":               "Detected a MiniMax soft-block flag (input/output_sensitive) — recorded only, bytes unchanged",
			"opaque":                            "Response carried Content-Encoding (upstream self-compressed, not transparently decoded); the normalizer skipped it entirely — bytes were not inspected or altered",
			"overflow_raw_passthrough":          "Response body exceeded the 8MB buffering cap; the normalizer gave up and passed the remaining bytes through as-is — subsequent steps like model rewrite/think stripping no longer run, equivalent to a direct connection",
			"crlf_framing_suspected":            "Suspected CRLF (`\\r\\n\\r\\n`) framed SSE response — the normalizer only recognizes `\\n\\n` event boundaries; when not found, the whole response is treated as one buffered unit (content is still correct, only the token-by-token streaming effect degrades)",
			"thinking_process_pattern_detected": "Response content contains a numbered reasoning section resembling a MiniMax thinking=medium leak, but didn't trip the existing \"Thinking Process:\" strip trigger — bytes unchanged, recorded only as an observation, used to judge whether that strip rule has gone stale",
			"think_pattern_detected":            "Response content contained a literal `<think>` / `</think>` marker but the `<think>`-block strip did not fire — the tag-form counterpart to `thinking_process_pattern_detected`. No threshold: benign content merely quoting or demonstrating the markers also trips it. Bytes unchanged, recorded only as an observation, used to judge whether the `<think>` strip rule has gone stale",
			"truncated_flush":                   "Upstream cut the stream mid-response; the bytes already received and safe to deliver (a non-SSE partial JSON body, or a `modePassthrough` SSE tail) were flushed to the client as-is, then the connection was aborted — the client sees a broken transfer, not a well-formed empty 200",
			"truncated_withheld":                "Upstream cut the stream mid-response while the SSE was in buffered/undecided mode — the withheld tail is a MiniMax thinking shape awaiting stripping, and flushing it raw would leak an unclosed `<think>` block, so it was discarded entirely and the connection aborted (the client gets nothing for the thinking phase anyway)",
		},
		unknownNormStep: "(unknown step)",

		streamYes: "yes", streamNo: "no",
		overviewHeaders: [9]string{"Virtual Model", "Upstream Endpoint", "Outcome", "Duration", "TTFT", "Attempts", "stream", "Tokens In/CacheHit/Out", "Client"},

		factsCapsNone: "none",
		listSep:       ", ",
		factsLineFmt:  "> **VMR pre-routing judgment**:\n> Capabilities required: %s\n> Estimated token count: %s\n\n",

		backToIndexLine:  "← Back to [request-browser.html](../../request-browser.html)\n\n",
		prevTurnLinkFmt:  "Previous turn: [%s](./%s)\n\n",
		thisTurnCalls:    "this turn's calls: ",
		traceLabel:       "trace ",
		chatLabel:        "chat ",
		truncatedWarning: "> ⚠️ **Client received a 2xx but the stream broke mid-way** — content is incomplete (attempts contains a truncated error)\n",
		noReplyWarning:   "> ⏭️ **LLM deliberately skipped replying** (empty response or NO_REPLY) — this turn's instruction wasn't actually handled; the next entry may be a retry.\n",

		clientRequestTitle:       "① Client → VMR Request",
		bodyNonJSON:              "Body (non-JSON)",
		paramsSummaryFmt:         "Request Params (%d)",
		toolsSummaryFmt:          "Tools (%d): %s",
		sysPromptEvidenceLinkFmt: "**System Prompt** (%s chars) → [%s](../evidence/%s)\n\n",
		toolsEvidenceLinkFmt:     "**Tools** (%d) → [%s](../evidence/%s)\n\n",
		messagesTitleFmt:         "Messages (%d)",
		roleTokenShareFmt:        "Estimated token share by role: %s\n\n",
		historyVsNewNoteFmt:      "#1–#%d are prior context (↺), #%d+ are new this turn (🆕)\n\n",
		historyFoldedNoteFmt:     "↺ #1–#%d (prior context) — see the previous turn's detail page: [%s](./%s)\n\n",
		incrementNoteFmt:         "\n🆕 **This turn's increment (vs. the previous turn, +%d, #1–#%d are prior context)**\n",

		attemptsTitleFmt:          "② VMR → Upstream (%d attempts)",
		noAttempts:                "No upstream attempts (the request was rejected before routing).\n\n",
		requestDiffIntro:          "**Request diff** (vs. ①, 🟢 added / 🔴 removed / 🔶 changed, unmarked = unchanged)\n\n",
		headersDiffSummaryFmt:     "Headers diff (%d items, %d changed)",
		responseTitle:             "**Response**",
		noResponse:                "(no response — request never completed)\n\n",
		responseHeadersSummaryFmt: "Response Headers (%d)",
		passthroughBody:           "body: **passthrough** — identical to what the client received in ③, save for the following normalization steps:\n\n",
		normStepsTitle:            "Normalization steps:\n\n",

		noRawPreStripKept:   "⚠️ This record's capture didn't retain the pre-strip raw content (the think_strip normalization step is logged, but the raw SSE wasn't kept)\n\n",
		rawPreStripSizedFmt: "Pre-strip raw content (%s, including the full &lt;think&gt; block and its raw SSE)",
		rawPreStrip:         "Pre-strip raw content (including the full &lt;think&gt; block and its raw SSE)",

		clientResponseTitle:           "③ VMR → Client Response",
		noResponseRecord:              "(no response record — connection dropped or the request was canceled)\n\n",
		responseHeadersDiffSummaryFmt: "Headers diff (vs. the upstream response, %d items, %d changed)",
		emptyBody:                     "(empty body)\n\n",
		modelOutputSSEFmt:             "Model Output (reassembled from %d SSE events)",
		rawSSERefFmt:                  "Raw SSE: %d events, %s — fetch the exact bytes: `vmr replay -print -req %s`\n\n",
		bodyNonJSONSSE:                "Body (non-JSON/SSE)",
		modelOutputTitle:              "Model Output",
		fullResponseJSONFmt:           "Full Response JSON (%s)",

		reasoningCharsFmt:    "🤔 reasoning · %s chars",
		toolCallArgsCharsFmt: "🔧 tool_call <code>%s</code> [id=%s] · args %s chars",
		finishModelLineFmt:   "finish_reason: `%s` · model field: `%s`\n\n",

		headerTableEmpty: "(none)\n",
		headerColumn:     "Header",
		valueColumn:      "Value",

		bodyIdentical:           "Body: identical to ①\n\n",
		bodyDifferentNonJSONFmt: "Body: 🔶 differs from ① (client %s / upstream %s, not a JSON object — can't diff field-by-field)\n\n",
		upstreamRequestBody:     "Upstream request body",
		fieldColumn:             "Field",
		bodyFieldDiffSummaryFmt: "Body field diff (%d items, %d changed)",

		msgClientOnlyFmt:            "- 🔴 #%d %s · %s chars · client side only\n",
		msgUpstreamOnlyFmt:          "- 🟢 #%d %s · %s chars · upstream side only\n",
		upstreamContentFmt:          "Upstream content #%d %s",
		msgUnchangedFmt:             "- #%d %s · %s chars\n",
		msgChangedFmt:               "- 🔶 #%d %s · %s → %s chars\n",
		upstreamContentSeeClientFmt: "Upstream content #%d %s (client side: see ①)",
		messagesDiffNoChangeFmt:     "Messages diff (%d, no changes)",
		messagesDiffChangedFmt:      "Messages diff (%d, %d changed 🔶)",

		toolClientOnlyFmt:    "- 🔴 %s · client side only\n",
		toolUpstreamOnlyFmt:  "- 🟢 %s · upstream side only\n",
		toolDefUpstream:      "Upstream definition ",
		toolChangedFmt:       "- 🔶 %s · definition changed\n",
		seeClientSide:        " (client side: see ①)",
		toolsDiffNoChangeFmt: "Tools diff (%d, no changes)",
		toolsDiffChangedFmt:  "Tools diff (%d, %d changed 🔶)",

		arrayItemsFmt:   "[%d items]",
		objectFieldsFmt: "{%d fields}",

		responseBodyLabel: "response body",
		sizedLabelFmt:     "%s (%s)",

		truncSuffixFmt:    "… (%d chars total)",
		emptyMessageFmt:   "%s**%s** · (empty)\n",
		messageSummaryFmt: "<b>%s%s</b> · %s chars · %s",
	},
	ZH: {
		normDescriptions: map[string]string{
			"model_rewrite":                     "上游返回的真实模型名被改写回虚拟模型名",
			"done_appended":                     "上游未发送 `data: [DONE]`，VMR 补发了终止哨兵",
			"think_strip":                       "剥离了 `<think>…</think>` 推理块（防止思考内容进入会话历史）",
			"thinking_process_strip":            "剥离了 \"Thinking Process:\" 纯文本推理草稿",
			"buffered":                          "整个响应被缓冲后一次性归一化（非逐事件透传）",
			"resumed_stream":                    "`<think>` 块结束后由缓冲恢复为流式转发",
			"soft_block_detected":               "检测到 MiniMax 软屏蔽标志（input/output_sensitive）——仅记录，字节未改动",
			"opaque":                            "响应带 Content-Encoding（上游自行压缩，未被透明解码），归一化器整体跳过，字节未做任何检查或改动",
			"overflow_raw_passthrough":          "响应体超过 8MB 缓冲上限，归一化器放弃处理并原样透传剩余字节——后续的 model 改写/think 剥离等步骤不再执行，等同直连行为",
			"crlf_framing_suspected":            "疑似 CRLF（`\\r\\n\\r\\n`）分帧的 SSE 响应——归一化器只识别 `\\n\\n` 事件边界，未找到时整段响应会被当作一次性缓冲处理（内容仍正确，仅逐 token 流式效果退化）",
			"thinking_process_pattern_detected": "响应内容含类似 MiniMax thinking=medium 泄漏的编号推理小节，但未命中现有 \"Thinking Process:\" 剥离触发条件——字节未改动，仅作观测标记，用于判断该剥离规则是否已经失效",
			"think_pattern_detected":            "响应内容出现了字面的 `<think>` / `</think>` 标记但未触发 `<think>` 块剥离——`thinking_process_pattern_detected` 的标签形态对应项。无阈值，正文里引用或演示这两个标记也会命中；字节未改动，仅作观测，用于判断 `<think>` 剥离规则是否已经失效",
			"truncated_flush":                   "上游在响应中途断流；此前收到、可安全交付的字节（非 SSE 的部分 JSON，或 `modePassthrough` 的 SSE 尾部）已原样 flush 给客户端，随后连接被中止——客户端看到的是断掉的传输而非格式良好的空 200",
			"truncated_withheld":                "上游在响应中途断流，且 SSE 此时处于 buffered/undecided 模式——被扣留的尾部是待剥离的 MiniMax thinking 形态，原样 flush 会泄漏未闭合的 `<think>` 块，因此整段丢弃并中止连接（思考阶段客户端本就拿不到内容）",
		},
		unknownNormStep: "（未知步骤）",

		streamYes: "是", streamNo: "否",
		overviewHeaders: [9]string{"虚拟模型", "上游端点", "结果", "耗时", "首字延迟", "尝试次数", "stream", "Tokens In/CacheHit/Out", "客户端"},

		factsCapsNone: "无",
		listSep:       "、",
		factsLineFmt:  "> **VMR 路由前判断**：\n> 请求所需能力：%s\n> 预估Token数量：%s\n\n",

		backToIndexLine:  "← 返回 [request-browser.html](../../request-browser.html)\n\n",
		prevTurnLinkFmt:  "上一轮: [%s](./%s)\n\n",
		thisTurnCalls:    "本轮调用: ",
		traceLabel:       "trace ",
		chatLabel:        "chat ",
		truncatedWarning: "> ⚠️ **客户端收到 2xx 但流中途断开**——内容不完整（attempts 内有 truncated 错误）\n",
		noReplyWarning:   "> ⏭️ **LLM 主动跳过回复**（response 为空或 NO_REPLY）——本轮指令未实际处理，下一条可能是重试。\n",

		clientRequestTitle:       "① Client → VMR 请求",
		bodyNonJSON:              "Body（非 JSON）",
		paramsSummaryFmt:         "请求参数 (%d)",
		toolsSummaryFmt:          "Tools (%d): %s",
		sysPromptEvidenceLinkFmt: "**System Prompt**（%s 字符） → [%s](../evidence/%s)\n\n",
		toolsEvidenceLinkFmt:     "**Tools**（%d 个） → [%s](../evidence/%s)\n\n",
		messagesTitleFmt:         "Messages (%d)",
		roleTokenShareFmt:        "角色 Token 估算占比：%s\n\n",
		historyVsNewNoteFmt:      "#1–#%d 为历史上下文（↺），#%d 起为本轮新增（🆕）\n\n",
		historyFoldedNoteFmt:     "↺ #1–#%d（历史上下文）—— 见上一轮详单：[%s](./%s)\n\n",
		incrementNoteFmt:         "\n🆕 **本轮增量（相对上一轮，+%d 条，#1–#%d 为历史上下文）**\n",

		attemptsTitleFmt:          "② VMR → 上游（%d 次尝试）",
		noAttempts:                "无上游尝试（请求在路由前被拒绝）。\n\n",
		requestDiffIntro:          "**请求对比**（相对 ①，🟢 新增 / 🔴 删除 / 🔶 变化，未标记 = 未变）\n\n",
		headersDiffSummaryFmt:     "Headers 对比 (%d 项，%d 处变化)",
		responseTitle:             "**响应**",
		noResponse:                "（无响应——请求未完成）\n\n",
		responseHeadersSummaryFmt: "响应 Headers (%d)",
		passthroughBody:           "body：**透传** —— 与 ③ 客户端收到的字节一致，仅差以下归一化步骤：\n\n",
		normStepsTitle:            "归一化步骤：\n\n",

		noRawPreStripKept:   "⚠️ 该记录采集时未保留剥离前原始内容（think_strip 归一化步骤名有记录，原始 SSE 未保留）\n\n",
		rawPreStripSizedFmt: "剥离前原始内容（%s，含完整 &lt;think&gt; 块与对应原始 SSE）",
		rawPreStrip:         "剥离前原始内容（含完整 &lt;think&gt; 块与对应原始 SSE）",

		clientResponseTitle:           "③ VMR → Client 响应",
		noResponseRecord:              "（无响应记录——连接中断或请求被取消）\n\n",
		responseHeadersDiffSummaryFmt: "Headers 对比（相对上游响应，%d 项，%d 处变化）",
		emptyBody:                     "（body 为空）\n\n",
		modelOutputSSEFmt:             "模型输出（由 %d 个 SSE 事件重组）",
		rawSSERefFmt:                  "原始 SSE：%d 个事件，%s —— 按坐标取回原文：`vmr replay -print -req %s`\n\n",
		bodyNonJSONSSE:                "Body（非 JSON/SSE）",
		modelOutputTitle:              "模型输出",
		fullResponseJSONFmt:           "完整响应 JSON（%s）",

		reasoningCharsFmt:    "🤔 reasoning · %s 字符",
		toolCallArgsCharsFmt: "🔧 tool_call <code>%s</code> [id=%s] · args %s 字符",
		finishModelLineFmt:   "finish_reason: `%s` · model 字段: `%s`\n\n",

		headerTableEmpty: "（无）\n",
		headerColumn:     "Header",
		valueColumn:      "值",

		bodyIdentical:           "Body：与 ① 完全一致\n\n",
		bodyDifferentNonJSONFmt: "Body：🔶 与 ① 不同（客户端 %s / 上游 %s，非 JSON 对象，无法逐字段对比）\n\n",
		upstreamRequestBody:     "上游请求 body",
		fieldColumn:             "字段",
		bodyFieldDiffSummaryFmt: "Body 字段对比 (%d 项，%d 处变化)",

		msgClientOnlyFmt:            "- 🔴 #%d %s · %s 字符 · 仅客户端侧有\n",
		msgUpstreamOnlyFmt:          "- 🟢 #%d %s · %s 字符 · 仅上游侧有\n",
		upstreamContentFmt:          "上游侧内容 #%d %s",
		msgUnchangedFmt:             "- #%d %s · %s 字符\n",
		msgChangedFmt:               "- 🔶 #%d %s · %s → %s 字符\n",
		upstreamContentSeeClientFmt: "上游侧内容 #%d %s（客户端侧见 ①）",
		messagesDiffNoChangeFmt:     "Messages 对比 (%d 条，无变化)",
		messagesDiffChangedFmt:      "Messages 对比 (%d 条，%d 处变化 🔶)",

		toolClientOnlyFmt:    "- 🔴 %s · 仅客户端侧有\n",
		toolUpstreamOnlyFmt:  "- 🟢 %s · 仅上游侧有\n",
		toolDefUpstream:      "上游侧定义 ",
		toolChangedFmt:       "- 🔶 %s · 定义有变化\n",
		seeClientSide:        "（客户端侧见 ①）",
		toolsDiffNoChangeFmt: "Tools 对比 (%d 个，无变化)",
		toolsDiffChangedFmt:  "Tools 对比 (%d 个，%d 处变化 🔶)",

		arrayItemsFmt:   "[%d 项]",
		objectFieldsFmt: "{%d 字段}",

		responseBodyLabel: "响应 body",
		sizedLabelFmt:     "%s（%s）",

		truncSuffixFmt:    "… (共 %d 字符)",
		emptyMessageFmt:   "%s**%s** · (空)\n",
		messageSummaryFmt: "<b>%s%s</b> · %s 字符 · %s",
	},
}

func Detail(lang Lang) DetailText {
	r := detailRows.Row(lang)
	return DetailText{
		NormDescriptions: r.normDescriptions,
		UnknownNormStep:  r.unknownNormStep,

		StreamYes: r.streamYes, StreamNo: r.streamNo,
		OverviewHeaders: r.overviewHeaders,

		FactsCapsNone: r.factsCapsNone,
		ListSep:       r.listSep,
		FactsLine: func(caps, estTokens string) string {
			return fmt.Sprintf(r.factsLineFmt, caps, estTokens)
		},

		BackToIndexLine:  r.backToIndexLine,
		PrevTurnLink:     func(ts, file string) string { return fmt.Sprintf(r.prevTurnLinkFmt, ts, file) },
		ThisTurnCalls:    r.thisTurnCalls,
		TraceLabel:       r.traceLabel,
		ChatLabel:        r.chatLabel,
		TruncatedWarning: r.truncatedWarning,
		NoReplyWarning:   r.noReplyWarning,

		ClientRequestTitle: r.clientRequestTitle,
		BodyNonJSON:        r.bodyNonJSON,
		ParamsSummary:      func(n int) string { return fmt.Sprintf(r.paramsSummaryFmt, n) },
		ToolsSummary:       func(n int, preview string) string { return fmt.Sprintf(r.toolsSummaryFmt, n, preview) },
		SysPromptEvidenceLink: func(chars, file string) string {
			return fmt.Sprintf(r.sysPromptEvidenceLinkFmt, chars, file, file)
		},
		ToolsEvidenceLink: func(n int, file string) string {
			return fmt.Sprintf(r.toolsEvidenceLinkFmt, n, file, file)
		},
		MessagesTitle:  func(n int) string { return fmt.Sprintf(r.messagesTitleFmt, n) },
		RoleTokenShare: func(line string) string { return fmt.Sprintf(r.roleTokenShareFmt, line) },
		HistoryVsNewNote: func(deltaStart int) string {
			return fmt.Sprintf(r.historyVsNewNoteFmt, deltaStart, deltaStart+1)
		},
		HistoryFoldedNote: func(n int, ts, file string) string {
			return fmt.Sprintf(r.historyFoldedNoteFmt, n, ts, file)
		},
		IncrementNote: func(n, deltaStart int) string {
			return fmt.Sprintf(r.incrementNoteFmt, n, deltaStart)
		},

		AttemptsTitle:    func(n int) string { return fmt.Sprintf(r.attemptsTitleFmt, n) },
		NoAttempts:       r.noAttempts,
		RequestDiffIntro: r.requestDiffIntro,
		HeadersDiffSummary: func(union, changed int) string {
			return fmt.Sprintf(r.headersDiffSummaryFmt, union, changed)
		},
		ResponseTitle:          r.responseTitle,
		NoResponse:             r.noResponse,
		ResponseHeadersSummary: func(n int) string { return fmt.Sprintf(r.responseHeadersSummaryFmt, n) },
		PassthroughBody:        r.passthroughBody,
		NormStepsTitle:         r.normStepsTitle,

		NoRawPreStripKept: r.noRawPreStripKept,
		RawPreStripSized:  func(size string) string { return fmt.Sprintf(r.rawPreStripSizedFmt, size) },
		RawPreStrip:       r.rawPreStrip,

		ClientResponseTitle: r.clientResponseTitle,
		NoResponseRecord:    r.noResponseRecord,
		ResponseHeadersDiffSummary: func(union, changed int) string {
			return fmt.Sprintf(r.responseHeadersDiffSummaryFmt, union, changed)
		},
		EmptyBody:      r.emptyBody,
		ModelOutputSSE: func(events int) string { return fmt.Sprintf(r.modelOutputSSEFmt, events) },
		RawSSERef: func(events int, size, coord string) string {
			return fmt.Sprintf(r.rawSSERefFmt, events, size, coord)
		},
		BodyNonJSONSSE:   r.bodyNonJSONSSE,
		ModelOutputTitle: r.modelOutputTitle,
		FullResponseJSON: func(size string) string { return fmt.Sprintf(r.fullResponseJSONFmt, size) },

		ReasoningChars: func(chars string) string { return fmt.Sprintf(r.reasoningCharsFmt, chars) },
		ToolCallArgsChars: func(name, id, chars string) string {
			return fmt.Sprintf(r.toolCallArgsCharsFmt, name, id, chars)
		},
		FinishModelLine: func(finish, model string) string {
			return fmt.Sprintf(r.finishModelLineFmt, finish, model)
		},

		HeaderTableEmpty: r.headerTableEmpty,
		HeaderColumn:     r.headerColumn,
		ValueColumn:      r.valueColumn,

		BodyIdentical: r.bodyIdentical,
		BodyDifferentNonJSON: func(clientSize, upstreamSize string) string {
			return fmt.Sprintf(r.bodyDifferentNonJSONFmt, clientSize, upstreamSize)
		},
		UpstreamRequestBody: r.upstreamRequestBody,
		FieldColumn:         r.fieldColumn,
		BodyFieldDiffSummary: func(n, changed int) string {
			return fmt.Sprintf(r.bodyFieldDiffSummaryFmt, n, changed)
		},

		MsgClientOnly: func(idx int, role, chars string) string {
			return fmt.Sprintf(r.msgClientOnlyFmt, idx, role, chars)
		},
		MsgUpstreamOnly: func(idx int, role, chars string) string {
			return fmt.Sprintf(r.msgUpstreamOnlyFmt, idx, role, chars)
		},
		UpstreamContent: func(idx int, role string) string { return fmt.Sprintf(r.upstreamContentFmt, idx, role) },
		MsgUnchanged: func(idx int, role, chars string) string {
			return fmt.Sprintf(r.msgUnchangedFmt, idx, role, chars)
		},
		MsgChanged: func(idx int, role, charsC, charsA string) string {
			return fmt.Sprintf(r.msgChangedFmt, idx, role, charsC, charsA)
		},
		UpstreamContentSeeClient: func(idx int, role string) string {
			return fmt.Sprintf(r.upstreamContentSeeClientFmt, idx, role)
		},
		MessagesDiffNoChange: func(n int) string { return fmt.Sprintf(r.messagesDiffNoChangeFmt, n) },
		MessagesDiffChanged: func(n, changed int) string {
			return fmt.Sprintf(r.messagesDiffChangedFmt, n, changed)
		},

		ToolClientOnly:    func(name string) string { return fmt.Sprintf(r.toolClientOnlyFmt, name) },
		ToolUpstreamOnly:  func(name string) string { return fmt.Sprintf(r.toolUpstreamOnlyFmt, name) },
		ToolDefUpstream:   r.toolDefUpstream,
		ToolChanged:       func(name string) string { return fmt.Sprintf(r.toolChangedFmt, name) },
		SeeClientSide:     r.seeClientSide,
		ToolsDiffNoChange: func(n int) string { return fmt.Sprintf(r.toolsDiffNoChangeFmt, n) },
		ToolsDiffChanged: func(n, changed int) string {
			return fmt.Sprintf(r.toolsDiffChangedFmt, n, changed)
		},

		ArrayItems:   func(n int) string { return fmt.Sprintf(r.arrayItemsFmt, n) },
		ObjectFields: func(n int) string { return fmt.Sprintf(r.objectFieldsFmt, n) },

		ResponseBodyLabel: r.responseBodyLabel,
		SizedLabel: func(label, size string) string {
			return fmt.Sprintf(r.sizedLabelFmt, label, size)
		},

		TruncSuffix:  func(n int) string { return fmt.Sprintf(r.truncSuffixFmt, n) },
		EmptyMessage: func(prefix, head string) string { return fmt.Sprintf(r.emptyMessageFmt, prefix, head) },
		MessageSummary: func(prefix, head, chars, preview string) string {
			return fmt.Sprintf(r.messageSummaryFmt, prefix, head, chars, preview)
		},
	}
}
