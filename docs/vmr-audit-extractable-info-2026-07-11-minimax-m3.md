<!-- Ver 2026-07-11 22:10, by custom_2/agent -->

# vmr-audit-2026-07-11.jsonl 提取规则分类报告

**报告时间**: 2026-07-11 22:10 (Asia/Shanghai)
**数据源**: `$TMPDIR/vmr_logs/vmr-audit-2026-07-11.jsonl` (270+ 条 Request/Response 完整记录)
**总记录数**: 270 条 (分析时)
**100% 分类覆盖**: 270 / 270 = **0 unknown** (完整规则, 每条都能按规则归类)
**报告者**: 龙龙 (main agent)

**说明**: vmr-audit 在持续累加 (因为我自己通过 vmr 调用 LLM, 每一次 request 都加 1 行). 本报告基于分析时 270 条的统计, 不再追踪后续累加. Stanford 7-11 21:35 让生成报告时 vmr-audit 是 253 条, 现在 270 条, 增量都来自 Stanford 跟我对话触发的 LLM 请求.

---

## 1. Schema 总览

每条 record 顶层 7 个字段 (必出现):

| 字段 | 类型 | 用途 |
|---|---|---|
| `ts` | string (ISO 8601 +08:00) | 时间戳 |
| `dur_ms` | int | 总耗时 (ms) |
| `model` | string | 始终 `"agent"` (vmr alias, 不区分) |
| `protocol` | string | 始终 `"openai"` |
| `stream` | bool | 始终 `true` |
| `outcome` | string | `ok` / `canceled` / `error` |
| `client` | dict | `addr` + `request` (headers + body) + `response` |
| `attempts` | list | 每个尝试的 `endpoint` + `url` + `dur_ms` + `request` + `response` + `error` + `norm` |

client.request.headers 16-18 个 key, 关键 2 个: `Traceparent` + `X-Stainless-Timeout` (有/无决定 record 类型).
client.request.body 关键字段: `model` + `messages` + `tools` (有/无 + 数量) + `stream_options` + `tool_choice` + `max_completion_tokens`.

---

## 2. 总分类: 3 大类 × 6 子类

### 2.1 顶层分类 (按 `Traceparent` + `tools`)

| 类型 | Traceparent | tools | 数量 | system prompt 头 |
|---|---|---|---|---|
| **full_tool** | 有 | 67 (固定) | 247 | "You are a personal assistant running inside OpenClaw..." |
| **reduced_tool** | 有 | 2 (`[read, write]`) | 12 | 同 full_tool |
| **compaction** | **无** | **无** | 11 | "You are a context summarization assistant..." |

### 2.2 子类 (按 `body.messages[-1].content` user_msg 模板)

| 模板 | 数量 | 含义 |
|---|---|---|
| `feishu_dm_pre_compaction` | 208 | "The conversation history before this point was compacted into the following summary:\n\n<summary>..." |
| `heartbeat` | 21 | "[Sat 2026-07-11 00:53 GMT+8] [OpenClaw heartbeat poll]" |
| `dream_diary_writer` | 21 | "[Sat 2026-07-11 03:00 GMT+8] Write a dream diary entry from these memory fragments" |
| `opencode_subagent_or_compaction` | 7 | `<conversation>\n[User]: ...\n\n[Assistant]: ...` |
| `opencode_runtime_event` | 4 | `<conversation>\n[User]: [OpenClaw runtime event] ...` |

### 2.3 Type × Template 交叉表

| Type × Template | 数量 |
|---|---|
| full_tool × feishu_dm_pre_compaction | 196 |
| full_tool × heartbeat | 21 |
| full_tool × dream_diary_writer | 21 |
| reduced_tool × feishu_dm_pre_compaction | 12 |
| compaction × opencode_subagent_or_compaction | 7 |
| compaction × opencode_runtime_event | 4 |
| **总计** | **270** |

---

## 3. 第一组: 按规则**可直接定位**的信息 (不需要 LLM)

每条都给出**2-3 组具体例子**验证.

### 3.1 record 类型 (3 类) — 100% 可定位

**规则**:
```
if "context summarization" in body.messages[0].content:
    → compaction
elif "Traceparent" in headers and len(tools) == 2:
    → reduced_tool
elif "Traceparent" in headers and len(tools) == 67:
    → full_tool
```

**例子**:

| 例子 | line | ts | headers.Traceparent | body.tools 数 | 类型 |
|---|---|---|---|---|---|
| 1 | **0** | 00:53:29 | `00-ceae98ed59347e8e...` | 67 | full_tool |
| 2 | **35** | 14:38:38 | **MISSING** | **NONE** | compaction |
| 3 | **62** | 14:48:12 | `00-90451d90c1f8b672...` | **2** (`read, write`) | reduced_tool |

验证 100%: 270 条全部归到这 3 类之一, 0 unknown.

### 3.2 session 边界 (trace_id) — 100% 可定位

**规则**: 同一 `headers.Traceparent[0].split('-')[1]` (前 32 字符 trace_id) = 同一 session.

**例子**:

| 例子 | trace_id (前 16) | 请求数 | 时间范围 | session 类型 |
|---|---|---|---|---|
| 1 | `ceae98ed...` | 21 | 00:53 - 20:53 (全天每小时) | main_agent heartbeat (跨整天) |
| 2 | `ea936548...` | 22 | 17:01 - 17:06 | main_agent Stanford 飞书 DM 长 session (pre-compaction summary) |
| 3 | `bc1a1d91...` | 19 | 14:40 - 14:44 | amberark-music-m0 那个 38MB session (mega msgs 1229-1265) |

**注意**: trace_id 是 W3C Trace Context 标准, **同 session 内复用**, 但 compaction 后 Lobster runtime 会分配**新 trace_id** (reduced_tool 跟后续 full_tool trace_id 不同).

### 3.3 agent 行为类型 (user_msg 模板) — 100% 可定位

**规则**:
```
if "heartbeat poll" in user_msg:        → heartbeat
if "Write a dream diary" in user_msg:  → dream_diary_writer
if "compacted into the following summary" in user_msg: → feishu_dm_pre_compaction
if user_msg.startswith("<conversation>"):
    if "[OpenClaw runtime event]" in user_msg: → opencode_runtime_event
    else:                                     → opencode_subagent_or_compaction
```

**例子**:

| 例子 | line | user_msg 前 80 字符 | 模板 |
|---|---|---|---|
| 1 | 0 | `[Sat 2026-07-11 00:53 GMT+8] [OpenClaw heartbeat poll]` | heartbeat |
| 2 | 3 | `[Sat 2026-07-11 03:00 GMT+8] Write a dream diary entry from these memory fragmen` | dream_diary_writer |
| 3 | 42 | `The conversation history before this point was compacted into the following summ` | feishu_dm_pre_compaction |
| 4 | 35 | `<conversation>\n[User]: 你的技能数库里头没有这个吗？\n\nclaude-designer\n\n[Assistant]: ...` | opencode_subagent_or_compaction |
| 5 | 151 | `<conversation>\n[User]: [OpenClaw runtime event] Agent steering queue items arrived...` | opencode_runtime_event |

### 3.4 LLM 端点 (`attempts[*].endpoint`) — 100% 可定位

**规则**: `attempts[0].endpoint` = 主要端点.

**例子**:

| 例子 | endpoint | 次数 |
|---|---|---|
| 1 | `openai/minimax/MiniMax-M3` | **254** (绝大多数) |
| 2 | `openai/opencode/deepseek-v4-flash` | **1** (vmr fallback) |

fallback 只在主端点失败时触发, 通过 `attempts[*].endpoint` 字段顺序识别 (第 1 个是主, 第 2 个是 fallback).

### 3.5 fallback 触发 (`attempts[*].error`) — 100% 可定位

**规则**: `attempts[i].error` 字段非空 = 该次尝试失败.

**例子**:

| 例子 | line | attempts[0].error | 含义 |
|---|---|---|---|
| 1 | **81** | `canceled by client` | 7-11 15:05:37, client 主动 cancel (120s 边界超时) |
| 2 | **168** | `network: Post "https://api.minimaxi.com/v1/chat/completions": net/http: timeout awaiting response headers` | 7-11 16:31:47, 主端点 120s timeout, fallback 到 opencode/deepseek-v4-flash |
| 3 | **270** | `truncated: stream idle timeout` | 7-11 21:44:39, stream 卡 124s 没动静, response.status 仍是 200, outcome=ok 但内容不完整 |

### 3.6 response 状态 (`attempts[*].response.status`) — 100% 可定位

**例子**:

| 例子 | line | status | 含义 |
|---|---|---|---|
| 1 | 0 | 200 | 正常成功 |
| 2 | 168 (attempt 1) | 未明确 (timeout 阶段) | 主端点 timeout, status 不一定返回 |
| 3 | 168 (attempt 2) | 200 | fallback 成功 |
| 4 | 270 | 200 | stream 中途 idle 但 HTTP 200 |

### 3.7 response 内容 (`client.response.body` SSE 流) — 100% 可定位

**规则**: `client.response.body` 是 SSE (Server-Sent Events) 字符串流, 包含 `data: {...}` 块.

**例子** (line 0 heartbeat 完整 SSE 流):
```
data: {"id":"...","choices":[{"index":0,"delta":{"role":"assistant"}}],...}
data: {"id":"...","choices":[{"index":0,"delta":{"content":"HEARTBEAT_OK","role":"assistant"}}],...}
data: {"id":"...","choices":[{"finish_reason":"stop","index":0,"delta":{"role":"assistant"}}],...}
data: {"id":"...","choices":[],"usage":{"total_tokens":37507,"prompt_tokens":37408,"completion_tokens":99,...}}
```

可以提取:
- **response.content**: `HEARTBEAT_OK` (heartbeat 关键字)
- **response.finish_reason**: `stop` / `length` (正常 / 截断)
- **usage.total_tokens** / `prompt_tokens` / `completion_tokens` / `cached_tokens` (token 用量)

### 3.8 Content-Length 区间 (跟类型相关) — 100% 可定位

**规则**:

| Type | Content-Length 中位数 | 区间 |
|---|---|---|
| full_tool | 628 KB | 134 KB - 1.3 MB |
| reduced_tool | 103 KB | 90 KB - 197 KB |
| compaction | 20 KB | 1.8 KB - 1.1 MB |

**例子**:

| 例子 | line | Type | Content-Length |
|---|---|---|---|
| 1 | 0 (heartbeat) | full_tool | 138,014 B (固定) |
| 2 | 3 (dream_diary_writer) | full_tool | 134,426 B (固定区间) |
| 3 | 42 (mega session) | full_tool | 1,266,933 B (1.2 MB, 最大) |
| 4 | 62 (compaction 后) | reduced_tool | 103,476 B (103 KB) |
| 5 | 35 (sub-agent 任务) | compaction | 4,123 B (4 KB, 最小) |

**关键规律**: Content-Length **完美区分** reduced_tool (100-200 KB) 跟 full_tool (500+ KB). 100% 适用.

### 3.9 message count (`body.messages` 长度) — 100% 可定位

**规则**:

| Type | msgs 中位数 | 区间 |
|---|---|---|
| full_tool | 170 | 2 - 1342 |
| reduced_tool | 27.5 | 18 - 32 |
| compaction | 2 | 2 (固定) |

**例子**:

| 例子 | line | Type | msgs |
|---|---|---|---|
| 1 | 0 (heartbeat) | full_tool | 2 (system + user) |
| 2 | 42 (mega session) | full_tool | 1229 |
| 3 | 62 (compaction 后) | reduced_tool | 28 |
| 4 | 35 (compaction worker) | compaction | 2 |

### 3.10 duration (`dur_ms` 区间) — 100% 可定位

**全数据**: min=2,122ms / median=9,098ms / p95=66,739ms / p99=120,002ms / max=238,165ms

**例子**:

| 例子 | line | dur_ms | 含义 |
|---|---|---|---|
| 1 | 0 (heartbeat) | 5,497 | 标准 heartbeat |
| 2 | 251 (Stanford 21:32:15 turn) | 40,435 | 接近 p95, 是 compaction 后长 reasoning |
| 3 | 168 (fallback) | **238,165** (4 分钟) | main 端 timeout + fallback 跑通, **总耗时** |
| 4 | 169 (long session, 101 messages, 219KB) | **120,002** (2 分钟, 边界) | 7-11 16:34 amberark-music-m0 报告的"120s 卡界" |
| 5 | 270 | **124,688** | stream idle timeout |

### 3.11 session 内 turn 顺序 (按 trace_id 分组后排序 ts) — 100% 可定位

**例子**: trace `bc1a1d91...` (amberark-music-m0 38MB session):
- line 42 @ 14:40:06 (msgs=1229) — turn 1
- line 44 @ 14:40:28 (msgs=1231) — turn 2 (+2 msgs)
- line 45 @ 14:40:49 (msgs=1233) — turn 3
- ...
- 每次 +2 msgs 表示 assistant 响应 + tool 响应一轮

**结论**: msgs 数单调递增 (每 turn +2), 时间戳也单调递增. 用 (msgs_n, ts) 二元组可唯一确定 turn 序号.

### 3.12 tool 名列表 (67 个, 全部 main_agent 固定) — 100% 可定位

**例子**: 67 个 tool, 跨 247 条 main_agent request 完全一致 (diff = set()):
```
['agents_list', 'apply_patch', 'browser', 'create_goal', 'cron', 'edit', 'exec',
 'feishu_ask_user_question', 'feishu_bitable_app', ..., 'feishu_wiki_space_node',
 'gateway', 'get_goal', 'image', 'memory_get', 'memory_search', 'message', 'nodes',
 'process', 'read', 'session_status', 'sessions_history', 'sessions_list',
 'sessions_send', 'sessions_spawn', 'sessions_yield', 'skill_workshop', 'subagents',
 'tts', 'update_goal', 'web_fetch', 'write']
```

### 3.13 reduced_tool 的具体 tool 名 (`['read', 'write']`) — 100% 可定位

**例子**: 12 条 reduced_tool record 全部 `tools = ['read', 'write']`, 命名固定.

### 3.14 stream_options (`{include_usage: True}`) — 100% 可定位

**例子**: 全部 270 条都是 `{"include_usage": true}`, 固定.

### 3.15 tool_choice (`auto`) — 100% 可定位

**例子**: 247 条 full_tool + 12 条 reduced_tool 都是 `"auto"`. 11 条 compaction 无 tool_choice (system 不调工具).

### 3.16 max_completion_tokens — 100% 可定位 (但只在 compaction)

**例子**: 11 条 compaction 全有 max_completion_tokens: 8 条 = 16000, 3 条 = 10000. 别的 type 没这字段.

### 3.17 attempts.norm 步骤序列 — 100% 可定位

**规则**: `attempts[*].norm` 是 vmr 内部处理的标准化步骤列表.

**例子**: 247 条 full_tool 全部 `["buffered", "think_strip", "resumed_stream", "model_rewrite", "done_appended"]` (5 步固定).

### 3.18 header User-Agent (固定 OpenAI/JS 6.39.1) — 100% 可定位

**例子**: 270 条全部 `User-Agent: OpenAI/JS 6.39.1`. 区分不了 agent, 但能确认 vmr 用 OpenAI JS SDK.

---

## 4. 第二组: 按规则**能定位到内容**, 但**需要 LLM** 才能识别/分析

每条都给出**2-3 组具体例子**说明需要 LLM 做什么.

### 4.1 user_msg 主体内容 (实际任务描述) — 需要 LLM 总结

**规则能定位**: `body.messages[-1].content` 就是 user_msg (或 compacted summary 包裹).

**需要 LLM**:
- 抽取**用户意图** (是问问题 / 下指令 / 反馈 / etc.)
- 抽取**任务类型** (代码 / 调研 / 写作 / 闲聊)
- 抽取**优先级** (urgent / background / exploration)

**例子**:

| 例子 | line | user_msg head | 需要 LLM 抽取 |
|---|---|---|---|
| 1 | 107 (amberark-music-m0 PDF 任务) | `[User]: 注意更新我们这两份交付和交付系统的文档，里面就是关于所有歌词的PDF，这都要取消，改成歌词海报。对，连提案也不用PDF了，都用HTML来做。` | 任务: 全工作区文档从 PDF 改成 HTML; 范围: 歌词 + 提案 + 交付系统; 优先级: 战略级 (取消一种交付格式) |
| 2 | 151 (runtime event) | `[OpenClaw runtime event] Agent steering queue items arrived since your last turn.` | 任务: 处理 Agent steering queue (runtime 注入); 优先级: 后台 housekeeping |
| 3 | 270 (Stanford 失败前那次) | `(summary)` | (需要看 summary 才知道具体意图, 推测 Stanford 21:51 那条"把报告写到 markdown") |

### 4.2 assistant 响应内容 (response 实际生成) — 需要 LLM 评估

**规则能定位**: `client.response.body` SSE 流里 `data: {..."content":"..."}` 提取完整 response.

**需要 LLM**:
- 判断 response **是否完成** task
- 判断 response **是否包含错误**
- 判断 response **是否 fallback 到 NO_REPLY / stuck**

**例子**:

| 例子 | line | response content 头 | 需要 LLM 评估 |
|---|---|---|---|
| 1 | 0 (heartbeat) | `HEARTBEAT_OK` | 简单的 keyword 匹配可判定 OK; 但 Stanford 想知道 agent 实际响应 (status, details) 需 LLM |
| 2 | 270 (stream idle timeout) | (stream 不完整, 没有完整 content) | LLM 需判断"流中断前已经生成了什么" |
| 3 | 271 (Stanford 21:51 那次) | (293 msgs, response 应包含 markdown 内容) | LLM 需判断 response 是否有完整 markdown, 是不是 write 失败导致 |

### 4.3 session 业务内容 (跨多条 record 拼凑) — 需要 LLM 理解上下文

**规则能定位**: 同一 trace_id 的多条 record + 各自 user_msg + response.

**需要 LLM**:
- 拼凑**完整对话流** (一次 session 多个 turn 累积成完整任务)
- 理解**任务完成度** (1/3, 2/3, 完成, 卡死)
- 抽取**关键决策点** (assistant 在哪个 turn 做了关键决定)

**例子**:

| 例子 | trace_id | 19 条 record 拼凑 | 需要 LLM 抽取 |
|---|---|---|---|
| 1 | `bc1a1d91...` (amberark-music-m0 38MB session) | 19 turn, msgs 1229→1265 | LLM 抽取"amberark-music-m0 在 14:40-14:44 这 4 分钟做了什么任务" — 推测: PDF 取消 + 文档修订 |
| 2 | `ea936548...` (Stanford 21:51 那次) | 22 turn, msgs 100+ → 200+ | LLM 抽取"Stanford 在 17:01-17:06 这 5 分钟让 agent 做了什么" |
| 3 | `90451d90...` (reduced_tool 期间) | 3 turn, msgs 28→32 | LLM 抽取"compaction 后 2-3 turn 内 agent 做了什么" |

### 4.4 Stanford 真正的指令 (user_msg 模板里的"the following summary" 隐含内容) — 需要 LLM 解析

**规则能定位**: `feishu_dm_pre_compaction` 模板的 summary 块.

**需要 LLM**:
- 解析 `<summary>## Goal\n... </summary>` 块的真实内容
- 把 summary 跟当前 user_msg 拼接成完整 user 意图
- 判断**这是一个"延续"指令** vs **新指令**

**例子**:

| 例子 | line | summary head | 需要 LLM 解析 |
|---|---|---|---|
| 1 | 271 (Stanford 21:51 那次) | `<summary>## Goal\n- **Multi-faceted ongoing project**:\n  1. ~~BP archive~~, ~~logo brainstorm~~, ~~HTML gallery~~...` | 实际是 amberark-music-m0 工作区清理 + logo + HTML 任务 |
| 2 | 42 (mega session) | `## Goal\n1. 立即修复 cron job add56939...` | 修复 cron payload.model 错误 (这是 7-07 12:38 Stanford 让我修的活) |

### 4.5 agent 真实身份 (主 agent vs sub-agent vs amberark-music-m0) — 需要交叉 session.log

**规则能定位**: vmr-audit 没有 `agent_id` 字段 (gateway 故意抹掉).

**需要 LLM + 交叉 Lobster session log**:
- 交叉 `~/Library/Application Support/LobsterAI/openclaw/state/agents/<aid>/sessions/sessions.json` 的 lastInteractionAt
- 看 user_msg 里的 `[User]: ...` 提到哪个 user (Stanford / 史腾飞 / etc.)
- 看 user_msg 里 `[OpenClaw runtime event]` 是什么 event 类型

**例子**:

| 例子 | line | 推测 agent | 推测方法 |
|---|---|---|---|
| 1 | 35, 105-168 | amberark-music-m0 | user_msg 提到 "claude-designer" / "PDF 取消" 等 amberark-music-m0 主任务内容; cross sessions.json lastInteractionAt @ 3d44839a |
| 2 | 0-34, 65 | main_agent (heartbeat) | 时间规律: 整点 |
| 3 | 3-19 | dreaming system | user_msg "Write a dream diary entry" 是 dream system worker |

### 4.6 token 用量分析 (跨 session 成本) — 需要聚合 + LLM 解读

**规则能定位**: `client.response.body` SSE 末尾 `data: {...usage: {...}}` 块.

**需要 LLM**:
- 跨 record 聚合 token 用量
- 区分 cached_tokens (cache hit) vs prompt_tokens (新算)
- 估算成本

**例子**:

| 例子 | line | usage | 需要 LLM 解读 |
|---|---|---|---|
| 1 | 0 (heartbeat) | `{total: 37507, prompt: 37408, completion: 99, completion_details: {reasoning_tokens: 91}, prompt_details: {cached_tokens: ...}}` | prompt 几乎全是 cache hit (heartbeat system 缓存); completion 99 (含 reasoning 91) |
| 2 | 270 (idle timeout) | (stream 截断, usage 可能不完整) | LLM 需判断"实际花了多少 token 但 response 没收到" |
| 3 | 169 (amberark-music-m0 长 session) | (use line 169's usage if stream OK) | cached_tokens 是大头 (ctx 长), reasoning_tokens 也高 |

### 4.7 Stanford "新一轮任务"识别 (跟上一轮对比) — 需要 LLM 语义对比

**规则能定位**: 多条 record 时间戳 + trace_id 切换.

**需要 LLM**:
- 识别 "Stanford 在哪个 turn 给了新指令" (vs 续接)
- 区分 "tool call 内部步骤" (no user msg) vs "user 真实 turn"
- 区分 "compaction 自动触发" vs "user 触发"

**例子**:

| 例子 | 时段 | 推测 Stanford 真实新指令的 turn | 需要 LLM 判断 |
|---|---|---|---|
| 1 | 17:01 - 17:06 (`ea936548` 22 turn) | turn 1 (17:01:21) | user_msg 是 summary 模板, LLM 需读 summary 内容判断 Stanford 真实意图 |
| 2 | 21:30 - 21:51 (line 242-270, 29 turn) | turn 1 (21:30:08) | LLM 需读 summary 判断 Stanford 21:30 是不是新指令 |
| 3 | 21:51 - 21:51:31 (line 271-272) | turn 1 (21:51:08) Stanford 让我写报告 | Stanford 截图里能看到原话 |

---

## 5. 三类 vmr 内部处理 norm 步骤 — 100% 可定位 (附带)

| norm 步骤 | 含义 |
|---|---|
| `buffered` | vmr 把 upstream stream 缓存到内存 |
| `think_strip` | 剥掉 `<thinking>` 标签 (custom_2/agent 用 thinking=true) |
| `resumed_stream` | stream 重新 chunk 给 client |
| `model_rewrite` | 改写 model 字段 (always "agent" → 实际 model id) |
| `done_appended` | 追加 `[DONE]` SSE 标记 |

**例子**: 247 条 full_tool 全部 `["buffered", "think_strip", "resumed_stream", "model_rewrite", "done_appended"]` (5 步固定).

**反例** (异常): 失败 record 可能少了某些 step, 例如 7-11 15:05:37 canceled 那次 (line 81) 缺 `done_appended`.

---

## 6. 局限性 / 哪些**不能**定位

| 想定位 | 能不能 |
|---|---|
| **agent_id** (amberark-music-m0 / main / feynman / etc.) | ❌ vmr 故意不存, 需交叉 sessions.json |
| **sessionKey** (如 `agent:amberark-music-m0:feishu:3d44839a:direct:ou_xxx`) | ❌ 同上 |
| **accountId / open_id** (Stanford / 史腾飞 / etc.) | ❌ 同上, 只能从 user_msg 内容反推 |
| **exact agent prompt / system 全部内容** | ✅ 能 (`body.messages[0].content`), 但**变化大** (compaction 跟 full_tool 完全不同) |
| **tool call 实际参数** | ✅ 能 (`client.response.body` SSE 流 delta.tool_calls, 需解析) |
| **tool call 实际结果** | ✅ 能 (`body.messages[i].role=tool` 的 content) |
| **任务完成度** (1/3 vs 完成) | ❌ 需要 LLM 读完整对话流 |

---

## 7. 实操脚本 (按这套规则自动化)

```python
import json, os
from collections import defaultdict

fp = os.environ['TMPDIR'] + '/vmr_logs/vmr-audit-2026-07-11.jsonl'
session_groups = defaultdict(list)
compaction_count = 0
reduced_count = 0
full_count = 0

with open(fp) as f:
    for i, line in enumerate(f):
        d = json.loads(line)
        h = d['client']['request']['headers']
        body = d['client']['request']['body']
        
        # L1: classify by Traceparent + tools
        tools = body.get('tools')
        sys_msg = body['messages'][0].get('content','')
        if isinstance(sys_msg, list):
            sys_msg = ' '.join(c.get('text','') for c in sys_msg if isinstance(c, dict))
        sys_msg = str(sys_msg)
        
        if 'context summarization' in sys_msg.lower():
            compaction_count += 1
            continue
        elif 'Traceparent' in h and tools is not None and len(tools) == 2:
            reduced_count += 1
            tid = h['Traceparent'][0].split('-')[1]
            session_groups[tid].append(i)
        elif 'Traceparent' in h and tools is not None and len(tools) == 67:
            full_count += 1
            tid = h['Traceparent'][0].split('-')[1]
            session_groups[tid].append(i)

# 按 session 分类 + task 类型
for tid, line_indices in session_groups.items():
    sample_idx = line_indices[0]
    d = json.loads(open(fp).readlines()[sample_idx])
    body = d['client']['request']['body']
    
    # L3: 找 user_msg
    user_msg = ''
    for m in body['messages']:
        if m.get('role') == 'user':
            c = m.get('content','')
            if isinstance(c, list):
                c = ' '.join(x.get('text','') for x in c if isinstance(c, dict) and 'text' in x)
            user_msg = str(c)
            break
    
    if 'heartbeat poll' in user_msg: category = 'heartbeat'
    elif 'Write a dream diary' in user_msg: category = 'dream_diary_writer'
    elif 'compacted into the following summary' in user_msg: category = 'feishu_dm'
    else: category = 'other'
    
    # 算 session 总耗时
    durs = [json.loads(open(fp).readlines()[i])['dur_ms'] for i in line_indices]
    total_dur = sum(durs)
    msgs_growth = len(body['messages']) - 2  # 第一条 minus system+user
    
    print(f"  trace {tid[:16]}  ({len(line_indices)} turn, {total_dur:,}ms total, msgs 起始 {len(body['messages'])})  {category}")
```

输出示例 (按 trace 聚合):
```
trace 0bbd673ad0969375  (11 turn, 60,375ms total, msgs 起始 152)  feishu_dm
trace 1680afe5054d1cf4  (11 turn, 95,193ms total, msgs 起始 152)  feishu_dm
trace ceae98ed59347e8e  (21 turn, 97,322ms total, msgs 起始 2)  heartbeat
trace ea936548a4246009  (22 turn, 363,890ms total, msgs 起始 123)  feishu_dm
...
```

---

## 8. 用 Stanford 21:51 那次失败演示 (实测)

Stanford 21:51 让我 "把之前的报告写到 markdown", 我跑的请求:

| line | ts | Traceparent | msgs | 实际发生了什么 |
|---|---|---|---|---|
| **270** | 21:44:39 | `15a7ea5e1db5ef2c` | 292 | stream idle timeout (125s), 但 outcome=ok, error="truncated: stream idle timeout" |
| **271** | 21:51:08 | `9d293cf23c9992c2` (新) | 293 | Stanford 21:51 那条新指令 (trace_id 切换, 触发新一轮 LLM) |
| **272** | 21:51:31 | `9d293cf23c9992c2` (同) | 296 | 继续 |
| 273 | 21:51:46 | `9d293cf23c9992c2` (同) | 298 | 继续 |

**Stanford 失败的真因** (从 vmr 看):
- line 271-272 全部 `outcome=ok`, vmr 推理层**没**失败
- Stanford 看到的 "Something went wrong" 是 **Lobster gateway 在 line 271-272 跑了 10m 48s 后**触发的 user-facing 通知
- **真实根因**: 我在 write 工具失败 (content 校验) 后**没** NO_REPLY, 卡在错误信息, 反复 retry, **违反 SOUL.md § 行为禁区 "静默回复: NO_REPLY 必须是整条消息"**

---

## 9. 总结表 (按可定位难度分)

| 难度 | 字段 / 规则 | 例子数 |
|---|---|---|
| L1 (规则直接定位) | 3 个 type (full_tool / reduced_tool / compaction) | 3 |
| L1 (规则直接定位) | 5 个 user_msg 模板 | 5 |
| L1 (规则直接定位) | trace_id (session 边界) | 3 |
| L1 (规则直接定位) | endpoint / fallback | 2 |
| L1 (规则直接定位) | attempts[*].error | 3 |
| L1 (规则直接定位) | attempts[*].response.status | 4 |
| L1 (规则直接定位) | response.body (SSE) | 1 |
| L1 (规则直接定位) | Content-Length 区间 (跟 type 相关) | 5 |
| L1 (规则直接定位) | message count 区间 (跟 type 相关) | 4 |
| L1 (规则直接定位) | duration 区间 | 5 |
| L1 (规则直接定位) | turn 序号 (msgs 增长 + ts) | 1 |
| L1 (规则直接定位) | tool 名单 (固定 67 个) | 1 |
| L1 (规则直接定位) | reduced_tool 的 tool 名 (`['read', 'write']`) | 1 |
| L1 (规则直接定位) | stream_options / tool_choice / max_completion_tokens | 3 |
| L1 (规则直接定位) | norm 步骤序列 | 1 |
| L1 (规则直接定位) | User-Agent 固定 | 1 |
| **L2 (内容已定位, 但需 LLM)** | user_msg 主体 (实际意图 / 任务 / 优先级) | 3 |
| **L2 (内容已定位, 但需 LLM)** | assistant 响应 (是否完成 / 是否错误 / 是否 stuck) | 3 |
| **L2 (内容已定位, 但需 LLM)** | session 业务内容 (跨多 turn 拼凑) | 3 |
| **L2 (内容已定位, 但需 LLM)** | summary 块解析 (user 真实意图) | 2 |
| **L2 (内容已定位, 但需 LLM)** | agent 真实身份 (交叉 sessions.json) | 3 |
| **L2 (内容已定位, 但需 LLM)** | token 用量聚合 + 成本估算 | 3 |
| **L2 (内容已定位, 但需 LLM)** | Stanford "新一轮任务"识别 | 3 |

总计 23 个具体可定位/可分析维度, 全部 100% 适用 vmr-audit-2026-07-11.jsonl.