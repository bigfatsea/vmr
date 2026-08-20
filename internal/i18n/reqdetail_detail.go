// Ver 2026-08-20 00:00, by Sonnet 5

// Pairs with internal/reqdetail/detail.go (the per-request details/*.md
// pages shared by internal/report and internal/story).
package i18n

import "strconv"

// DetailText is reqdetail's rendering text, in one language.
type DetailText struct {
	NormDescriptions map[string]string
	UnknownNormStep  string

	StreamYes, StreamNo string
	NoValue             string    // "-"
	OverviewHeaders     [9]string // vm, upstream endpoint, outcome, dur, ttft, attempts, stream, tokens, client

	FactsCapsNone string
	ListSep       string // joins e.g. multiple detected capabilities ("`image`、`tools`" vs "`image`, `tools`")
	FactsLine     func(caps, estTokens string) string

	// BackToIndexLine is the "detail → vmr-requests.md" return edge
	// (P6.2e). details/ is always a direct sibling of vmr-requests.md
	// under the same output root regardless of which command rendered
	// this page, so the relative path never varies and needs no
	// existence check (generation-time guarantee, same class as
	// PrevTurnLink below).
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
	IncrementNote         func(n, deltaStart int) string

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
	RawSSEFull                 func(events int, size string) string
	BodyNonJSONSSE             string
	ModelOutputTitle           string
	FullResponseJSON           func(size string) string

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

func Detail(lang Lang) DetailText {
	if lang == ZH {
		return DetailText{
			NormDescriptions: map[string]string{
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
			},
			UnknownNormStep: "（未知步骤）",

			StreamYes: "是", StreamNo: "否",
			NoValue:         "-",
			OverviewHeaders: [9]string{"虚拟模型", "上游端点", "结果", "耗时", "首字延迟", "尝试次数", "stream", "Tokens In/CacheHit/Out", "客户端"},

			FactsCapsNone: "无",
			ListSep:       "、",
			FactsLine: func(caps, estTokens string) string {
				return "> **VMR 路由前判断**：\n> 请求所需能力：" + caps + "\n> 预估Token数量：" + estTokens + "\n\n"
			},

			BackToIndexLine:  "← 返回 [vmr-requests.md](../vmr-requests.md)\n\n",
			PrevTurnLink:     func(ts, file string) string { return " · 上一轮: [" + ts + "](./" + file + ")" },
			ThisTurnCalls:    "本轮调用: ",
			TraceLabel:       "trace ",
			ChatLabel:        "chat ",
			TruncatedWarning: "> ⚠️ **客户端收到 2xx 但流中途断开**——内容不完整（attempts 内有 truncated 错误）\n",
			NoReplyWarning:   "> ⏭️ **LLM 主动跳过回复**（response 为空或 NO_REPLY）——本轮指令未实际处理，下一条可能是重试。\n",

			ClientRequestTitle: "① Client → VMR 请求",
			BodyNonJSON:        "Body（非 JSON）",
			ParamsSummary:      func(n int) string { return "请求参数 (" + strconv.Itoa(n) + ")" },
			ToolsSummary:       func(n int, preview string) string { return "Tools (" + strconv.Itoa(n) + "): " + preview },
			SysPromptEvidenceLink: func(chars, file string) string {
				return "**System Prompt**（" + chars + " 字符） → [" + file + "](../evidence/" + file + ")\n\n"
			},
			ToolsEvidenceLink: func(n int, file string) string {
				return "**Tools**（" + strconv.Itoa(n) + " 个） → [" + file + "](../evidence/" + file + ")\n\n"
			},
			MessagesTitle:  func(n int) string { return "Messages (" + strconv.Itoa(n) + ")" },
			RoleTokenShare: func(line string) string { return "角色 Token 估算占比：" + line + "\n\n" },
			HistoryVsNewNote: func(deltaStart int) string {
				return "#1–#" + strconv.Itoa(deltaStart) + " 为历史上下文（↺）,#" + strconv.Itoa(deltaStart+1) + " 起为本轮新增（🆕）\n\n"
			},
			IncrementNote: func(n, deltaStart int) string {
				return "\n🆕 **本轮增量（相对上一轮,+" + strconv.Itoa(n) + " 条,#1–#" + strconv.Itoa(deltaStart) + " 为历史上下文）**\n"
			},

			AttemptsTitle:    func(n int) string { return "② VMR → 上游（" + strconv.Itoa(n) + " 次尝试）" },
			NoAttempts:       "无上游尝试（请求在路由前被拒绝）。\n\n",
			RequestDiffIntro: "**请求对比**（相对 ①，🟢 新增 / 🔴 删除 / 🔶 变化，未标记 = 未变）\n\n",
			HeadersDiffSummary: func(union, changed int) string {
				return "Headers 对比 (" + strconv.Itoa(union) + " 项，" + strconv.Itoa(changed) + " 处变化)"
			},
			ResponseTitle:          "**响应**",
			NoResponse:             "（无响应——请求未完成）\n\n",
			ResponseHeadersSummary: func(n int) string { return "响应 Headers (" + strconv.Itoa(n) + ")" },
			PassthroughBody:        "body：**透传** —— 与 ③ 客户端收到的字节一致，仅差以下归一化步骤：\n\n",
			NormStepsTitle:         "归一化步骤：\n\n",

			NoRawPreStripKept: "⚠️ 该记录采集时未保留剥离前原始内容（think_strip 归一化步骤名有记录，原始 SSE 未保留）\n\n",
			RawPreStripSized: func(size string) string {
				return "剥离前原始内容（" + size + "，含完整 &lt;think&gt; 块与对应原始 SSE）"
			},
			RawPreStrip: "剥离前原始内容（含完整 &lt;think&gt; 块与对应原始 SSE）",

			ClientResponseTitle: "③ VMR → Client 响应",
			NoResponseRecord:    "（无响应记录——连接中断或请求被取消）\n\n",
			ResponseHeadersDiffSummary: func(union, changed int) string {
				return "Headers 对比（相对上游响应，" + strconv.Itoa(union) + " 项，" + strconv.Itoa(changed) + " 处变化）"
			},
			EmptyBody: "（body 为空）\n\n",
			ModelOutputSSE: func(events int) string {
				return "模型输出（由 " + strconv.Itoa(events) + " 个 SSE 事件重组）"
			},
			RawSSEFull: func(events int, size string) string {
				return "原始 SSE 全文（" + strconv.Itoa(events) + " events · " + size + "）"
			},
			BodyNonJSONSSE:   "Body（非 JSON/SSE）",
			ModelOutputTitle: "模型输出",
			FullResponseJSON: func(size string) string { return "完整响应 JSON（" + size + "）" },

			ReasoningChars: func(chars string) string { return "🤔 reasoning · " + chars + " 字符" },
			ToolCallArgsChars: func(name, id, chars string) string {
				return "🔧 tool_call <code>" + name + "</code> [id=" + id + "] · args " + chars + " 字符"
			},
			FinishModelLine: func(finish, model string) string {
				return "finish_reason: `" + finish + "` · model 字段: `" + model + "`\n\n"
			},

			HeaderTableEmpty: "（无）\n",
			HeaderColumn:     "Header",
			ValueColumn:      "值",

			BodyIdentical: "Body：与 ① 完全一致\n\n",
			BodyDifferentNonJSON: func(clientSize, upstreamSize string) string {
				return "Body：🔶 与 ① 不同（客户端 " + clientSize + " / 上游 " + upstreamSize + "，非 JSON 对象，无法逐字段对比）\n\n"
			},
			UpstreamRequestBody: "上游请求 body",
			FieldColumn:         "字段",
			BodyFieldDiffSummary: func(n, changed int) string {
				return "Body 字段对比 (" + strconv.Itoa(n) + " 项，" + strconv.Itoa(changed) + " 处变化)"
			},

			MsgClientOnly: func(idx int, role, chars string) string {
				return "- 🔴 #" + strconv.Itoa(idx) + " " + role + " · " + chars + " 字符 · 仅客户端侧有\n"
			},
			MsgUpstreamOnly: func(idx int, role, chars string) string {
				return "- 🟢 #" + strconv.Itoa(idx) + " " + role + " · " + chars + " 字符 · 仅上游侧有\n"
			},
			UpstreamContent: func(idx int, role string) string { return "上游侧内容 #" + strconv.Itoa(idx) + " " + role },
			MsgUnchanged: func(idx int, role, chars string) string {
				return "- #" + strconv.Itoa(idx) + " " + role + " · " + chars + " 字符\n"
			},
			MsgChanged: func(idx int, role, charsC, charsA string) string {
				return "- 🔶 #" + strconv.Itoa(idx) + " " + role + " · " + charsC + " → " + charsA + " 字符\n"
			},
			UpstreamContentSeeClient: func(idx int, role string) string {
				return "上游侧内容 #" + strconv.Itoa(idx) + " " + role + "（客户端侧见 ①）"
			},
			MessagesDiffNoChange: func(n int) string { return "Messages 对比 (" + strconv.Itoa(n) + " 条，无变化)" },
			MessagesDiffChanged: func(n, changed int) string {
				return "Messages 对比 (" + strconv.Itoa(n) + " 条，" + strconv.Itoa(changed) + " 处变化 🔶)"
			},

			ToolClientOnly:    func(name string) string { return "- 🔴 " + name + " · 仅客户端侧有\n" },
			ToolUpstreamOnly:  func(name string) string { return "- 🟢 " + name + " · 仅上游侧有\n" },
			ToolDefUpstream:   "上游侧定义 ",
			ToolChanged:       func(name string) string { return "- 🔶 " + name + " · 定义有变化\n" },
			SeeClientSide:     "（客户端侧见 ①）",
			ToolsDiffNoChange: func(n int) string { return "Tools 对比 (" + strconv.Itoa(n) + " 个，无变化)" },
			ToolsDiffChanged: func(n, changed int) string {
				return "Tools 对比 (" + strconv.Itoa(n) + " 个，" + strconv.Itoa(changed) + " 处变化 🔶)"
			},

			ArrayItems:   func(n int) string { return "[" + strconv.Itoa(n) + " 项]" },
			ObjectFields: func(n int) string { return "{" + strconv.Itoa(n) + " 字段}" },

			ResponseBodyLabel: "响应 body",
			SizedLabel:        func(label, size string) string { return label + "（" + size + "）" },

			TruncSuffix:  func(n int) string { return "… (共 " + strconv.Itoa(n) + " 字符)" },
			EmptyMessage: func(prefix, head string) string { return prefix + "**" + head + "** · (空)\n" },
			MessageSummary: func(prefix, head, chars, preview string) string {
				return "<b>" + prefix + head + "</b> · " + chars + " 字符 · " + preview
			},
		}
	}
	return DetailText{
		NormDescriptions: map[string]string{
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
		},
		UnknownNormStep: "(unknown step)",

		StreamYes: "yes", StreamNo: "no",
		NoValue:         "-",
		OverviewHeaders: [9]string{"Virtual Model", "Upstream Endpoint", "Outcome", "Duration", "TTFT", "Attempts", "stream", "Tokens In/CacheHit/Out", "Client"},

		FactsCapsNone: "none",
		ListSep:       ", ",
		FactsLine: func(caps, estTokens string) string {
			return "> **VMR pre-routing judgment**:\n> Capabilities required: " + caps + "\n> Estimated token count: " + estTokens + "\n\n"
		},

		BackToIndexLine:  "← Back to [vmr-requests.md](../vmr-requests.md)\n\n",
		PrevTurnLink:     func(ts, file string) string { return " · previous turn: [" + ts + "](./" + file + ")" },
		ThisTurnCalls:    "this turn's calls: ",
		TraceLabel:       "trace ",
		ChatLabel:        "chat ",
		TruncatedWarning: "> ⚠️ **Client received a 2xx but the stream broke mid-way** — content is incomplete (attempts contains a truncated error)\n",
		NoReplyWarning:   "> ⏭️ **LLM deliberately skipped replying** (empty response or NO_REPLY) — this turn's instruction wasn't actually handled; the next entry may be a retry.\n",

		ClientRequestTitle: "① Client → VMR Request",
		BodyNonJSON:        "Body (non-JSON)",
		ParamsSummary:      func(n int) string { return "Request Params (" + strconv.Itoa(n) + ")" },
		ToolsSummary:       func(n int, preview string) string { return "Tools (" + strconv.Itoa(n) + "): " + preview },
		SysPromptEvidenceLink: func(chars, file string) string {
			return "**System Prompt** (" + chars + " chars) → [" + file + "](../evidence/" + file + ")\n\n"
		},
		ToolsEvidenceLink: func(n int, file string) string {
			return "**Tools** (" + strconv.Itoa(n) + ") → [" + file + "](../evidence/" + file + ")\n\n"
		},
		MessagesTitle:  func(n int) string { return "Messages (" + strconv.Itoa(n) + ")" },
		RoleTokenShare: func(line string) string { return "Estimated token share by role: " + line + "\n\n" },
		HistoryVsNewNote: func(deltaStart int) string {
			return "#1–#" + strconv.Itoa(deltaStart) + " are prior context (↺), #" + strconv.Itoa(deltaStart+1) + "+ are new this turn (🆕)\n\n"
		},
		IncrementNote: func(n, deltaStart int) string {
			return "\n🆕 **This turn's increment (vs. the previous turn, +" + strconv.Itoa(n) + ", #1–#" + strconv.Itoa(deltaStart) + " are prior context)**\n"
		},

		AttemptsTitle:    func(n int) string { return "② VMR → Upstream (" + strconv.Itoa(n) + " attempts)" },
		NoAttempts:       "No upstream attempts (the request was rejected before routing).\n\n",
		RequestDiffIntro: "**Request diff** (vs. ①, 🟢 added / 🔴 removed / 🔶 changed, unmarked = unchanged)\n\n",
		HeadersDiffSummary: func(union, changed int) string {
			return "Headers diff (" + strconv.Itoa(union) + " items, " + strconv.Itoa(changed) + " changed)"
		},
		ResponseTitle:          "**Response**",
		NoResponse:             "(no response — request never completed)\n\n",
		ResponseHeadersSummary: func(n int) string { return "Response Headers (" + strconv.Itoa(n) + ")" },
		PassthroughBody:        "body: **passthrough** — identical to what the client received in ③, save for the following normalization steps:\n\n",
		NormStepsTitle:         "Normalization steps:\n\n",

		NoRawPreStripKept: "⚠️ This record's capture didn't retain the pre-strip raw content (the think_strip normalization step is logged, but the raw SSE wasn't kept)\n\n",
		RawPreStripSized: func(size string) string {
			return "Pre-strip raw content (" + size + ", including the full &lt;think&gt; block and its raw SSE)"
		},
		RawPreStrip: "Pre-strip raw content (including the full &lt;think&gt; block and its raw SSE)",

		ClientResponseTitle: "③ VMR → Client Response",
		NoResponseRecord:    "(no response record — connection dropped or the request was canceled)\n\n",
		ResponseHeadersDiffSummary: func(union, changed int) string {
			return "Headers diff (vs. the upstream response, " + strconv.Itoa(union) + " items, " + strconv.Itoa(changed) + " changed)"
		},
		EmptyBody: "(empty body)\n\n",
		ModelOutputSSE: func(events int) string {
			return "Model Output (reassembled from " + strconv.Itoa(events) + " SSE events)"
		},
		RawSSEFull: func(events int, size string) string {
			return "Raw SSE, full (" + strconv.Itoa(events) + " events · " + size + ")"
		},
		BodyNonJSONSSE:   "Body (non-JSON/SSE)",
		ModelOutputTitle: "Model Output",
		FullResponseJSON: func(size string) string { return "Full Response JSON (" + size + ")" },

		ReasoningChars: func(chars string) string { return "🤔 reasoning · " + chars + " chars" },
		ToolCallArgsChars: func(name, id, chars string) string {
			return "🔧 tool_call <code>" + name + "</code> [id=" + id + "] · args " + chars + " chars"
		},
		FinishModelLine: func(finish, model string) string {
			return "finish_reason: `" + finish + "` · model field: `" + model + "`\n\n"
		},

		HeaderTableEmpty: "(none)\n",
		HeaderColumn:     "Header",
		ValueColumn:      "Value",

		BodyIdentical: "Body: identical to ①\n\n",
		BodyDifferentNonJSON: func(clientSize, upstreamSize string) string {
			return "Body: 🔶 differs from ① (client " + clientSize + " / upstream " + upstreamSize + ", not a JSON object — can't diff field-by-field)\n\n"
		},
		UpstreamRequestBody: "Upstream request body",
		FieldColumn:         "Field",
		BodyFieldDiffSummary: func(n, changed int) string {
			return "Body field diff (" + strconv.Itoa(n) + " items, " + strconv.Itoa(changed) + " changed)"
		},

		MsgClientOnly: func(idx int, role, chars string) string {
			return "- 🔴 #" + strconv.Itoa(idx) + " " + role + " · " + chars + " chars · client side only\n"
		},
		MsgUpstreamOnly: func(idx int, role, chars string) string {
			return "- 🟢 #" + strconv.Itoa(idx) + " " + role + " · " + chars + " chars · upstream side only\n"
		},
		UpstreamContent: func(idx int, role string) string { return "Upstream content #" + strconv.Itoa(idx) + " " + role },
		MsgUnchanged: func(idx int, role, chars string) string {
			return "- #" + strconv.Itoa(idx) + " " + role + " · " + chars + " chars\n"
		},
		MsgChanged: func(idx int, role, charsC, charsA string) string {
			return "- 🔶 #" + strconv.Itoa(idx) + " " + role + " · " + charsC + " → " + charsA + " chars\n"
		},
		UpstreamContentSeeClient: func(idx int, role string) string {
			return "Upstream content #" + strconv.Itoa(idx) + " " + role + " (client side: see ①)"
		},
		MessagesDiffNoChange: func(n int) string { return "Messages diff (" + strconv.Itoa(n) + ", no changes)" },
		MessagesDiffChanged: func(n, changed int) string {
			return "Messages diff (" + strconv.Itoa(n) + ", " + strconv.Itoa(changed) + " changed 🔶)"
		},

		ToolClientOnly:    func(name string) string { return "- 🔴 " + name + " · client side only\n" },
		ToolUpstreamOnly:  func(name string) string { return "- 🟢 " + name + " · upstream side only\n" },
		ToolDefUpstream:   "Upstream definition ",
		ToolChanged:       func(name string) string { return "- 🔶 " + name + " · definition changed\n" },
		SeeClientSide:     " (client side: see ①)",
		ToolsDiffNoChange: func(n int) string { return "Tools diff (" + strconv.Itoa(n) + ", no changes)" },
		ToolsDiffChanged: func(n, changed int) string {
			return "Tools diff (" + strconv.Itoa(n) + ", " + strconv.Itoa(changed) + " changed 🔶)"
		},

		ArrayItems:   func(n int) string { return "[" + strconv.Itoa(n) + " items]" },
		ObjectFields: func(n int) string { return "{" + strconv.Itoa(n) + " fields}" },

		ResponseBodyLabel: "response body",
		SizedLabel:        func(label, size string) string { return label + " (" + size + ")" },

		TruncSuffix:  func(n int) string { return "… (" + strconv.Itoa(n) + " chars total)" },
		EmptyMessage: func(prefix, head string) string { return prefix + "**" + head + "** · (empty)\n" },
		MessageSummary: func(prefix, head, chars, preview string) string {
			return "<b>" + prefix + head + "</b> · " + chars + " chars · " + preview
		},
	}
}
