<!-- Ver 2026-07-12 00:40, by Fable 5 -->

# 审计日志的 Agent 会话/任务分组与关键信息提取 — 可行性分析与综合方案

> **状态：已实现（2026-07-12）**。落地于 `internal/report/session.go`（分组与特征提取）、`export.go`（`vmr-requests.jsonl` 与 `tools[]`/`sessions[]`/`workloads[]` 聚合）、`detail.go`（详单头部/增量区/INDEX 分组视图）、`usage.go`（finish_reason 提取 + reasoning_tokens）;报表 JSON format 升至 6（rows 增 finish_reasons/truncated/tokens_reasoning/TTFT 分位,新增 hours[] 与 workloads[] 段,Markdown 相应增章节）;测试见 `session_test.go`,双日志端到端验证通过（518 条,工具调用计数与 §1.7 实证完全一致）。使用说明见 README「Audit log & usage reports」。
> 实现期修正两处规则（复查发现）:`Attached image(s) from tool result` 与 compaction summary 消息均为脚手架而非用户指令——前者曾导致任务过度切分（s02 被切成 6 任务,应为 3）,后者曾污染续接会话的标题;已并入 `isRealUser` 排除清单（§1.5 的 wrapper 词表）。

**问题**:`vmr report` 的逐请求详单目前是一条条孤立的记录,但 Agent 工作负载下,多条请求实际属于同一个 Agent 会话内同一个任务的连续轮次(每轮重发全部历史 + 追加增量)。能否**不调用 LLM**、纯靠日志内信息:①把请求按「Agent 会话 → 任务 → 轮次」分组;②在每条详单里标出本轮真正新增的部分;③把每个请求的关键特征提取成结构化数据,供后续统计分析;④提取每个请求**当期**发起的 tool call(历史重复不计),形成各 Agent 的工具使用画像,支撑"把不用的工具从 Agent 配置里裁掉"的决策?

**结论先行**:四项全部可行,而且是确定性的,不需要 LLM。会话级、任务级、逐轮增量三层分组可行;跨 compaction 的会话串联可确定性完成;8 类逐请求特征(trace_id、chat_id、请求形态、finish_reason、reasoning_tokens、截断标志、模板标签等)可直接提取;当期 tool call 从 **response** 中提取,天然免疫历史重复计数,已用两天日志 395/395 对交叉验证零误差(§1.7)。依据是对 2026-07-09(225 条、323MB)与 2026-07-11(292 条、388MB)两份真实日志的全量实证,以及对外部报告 `vmr-audit-extractable-info-2026-07-11-minimax-m3.md`(下称 M3 报告)的逐条交叉核对。

---

## 1. 数据实证

§1.2–1.6 基于 2026-07-09 日志;§1.7 的工具画像同时覆盖 2026-07-11 日志(292 条,已从运行 vmr 的机器取回本地 `logs/`)。

### 1.1 流量构成

07-09 全天 226 行:225 条 OpenClaw(OpenAI 协议、`model=agent`、UA `OpenAI/JS 6.39.1`)+ 1 条被拒的 curl。无 Anthropic 协议流量(Claude Code 未走 vmr)。07-11 结构相同(292 条,含 heartbeat/dream cron 任务与 11 条 compaction)。

Header 层的会话信号:`Traceparent` **并非每请求独立**(初版报告此处有误,已在 §1.6 修正)——trace_id 按"轮次突发"复用,是有效的分组信号;但没有显式的 session id / conversation id。会话级分组的主信号仍来自 **message 列表本身**——这是所有 Agent 客户端的共性,不依赖某家客户端恰好发了什么头。

### 1.2 会话指纹:首条非 system 消息

对每条记录取「首条非 system 消息的内容 hash」作为锚点(anchor),225 条记录立即分成 5 组 + 2 条独立小请求:

| anchor | 请求数 | 时间跨度 | messages 数走势 | 是什么 |
| --- | --- | --- | --- | --- |
| cda44f29 | 13 | 00:31–12:23 | 75→99 | 主会话(compaction 续接体,跨 12 小时) |
| 96f5411a | 68 | 00:45–01:12 | 808→948 | Lobster token 调研会话 |
| e362da2d | 142 | 06:43–12:14 | 59→345 | AmberArk logo 会话(compaction 续接体) |
| 5d69b7eb / 3274af46 | 各 1 | 06:43:21 | 2 | **compaction 调用本身**(system = "You are a context summarization assistant") |

关键验证:**三个会话在时间上完全交错**(00:31–12:23 的会话与 00:45–01:12、06:43–12:14 的会话互相穿插),单靠时间排序无法分开,anchor 分组则干净利落。这就是当前 INDEX.md 纯时间排序读不懂的根本原因。

### 1.3 前缀性质:同会话内请求互为前缀的比例与全部反例

对每条消息取 hash(role+content+tool_calls),组内按时间序两两比较,220 对相邻请求中:

- **197 对(89.5%)严格前缀**:上一请求的消息列表是下一请求的严格前缀,增量 = 新尾部。增量构成全部是 `assistant`(上轮回复)+ `tool`(工具结果),偶含 `user`(新指令)。
- **23 对断链,逐一核查后全部归因于 4 类已识别机制**,无一例外:

| 机制 | 表现 | 例证(行号) | 占比 |
| --- | --- | --- | --- |
| **临时尾部替换** | OpenClaw 每轮在尾部注入 1–2 条临时 user 消息("OpenClaw runtime context…"、"[时间戳] Conversation info (untrusted metadata)…"),下一轮被持久化后的真实用户消息替换 → lcp = 上轮长度 − 2 | 1→213, 217→219, 221→222, 2→3 | 断链主体(约 18/23) |
| **消息原地改写** | 旧 user 消息前缀 `[Thu … GMT+8]` 被改写为 `[Feishu ou_… Thu … GMT+8]`(渠道信息补注) | 7→8 | 少量 |
| **图片剪除** | 已处理的图片 tool result 被原地改写为 `[image data removed - already processed by model]`,且相邻的附图 user 消息被删除 → 历史**变短** | 85→86(85 条→84 条,lcp=42) | 1 对 |
| **system prompt 变更 / 上下文剪枝** | system 消息中途变化(工具清单变更,43374→37421 字符)导致 hash 从第 0 条就断;跳过 system 重算后 lcp=197/211,前缀关系恢复。另有 contextPruning 把历史截到 13–20 条的分支请求 | 148→149(跳过 system 后恢复), 80→81(剪枝,lcp=1) | 3 对 |

推论:**LCP(最长公共前缀)算法必须跳过 system 消息**(system 单独对比,变更作为事件标注);其余断链要么是可识别的临时尾部(lcp ≥ 上轮长度−2,归类为"尾部被替换"),要么是罕见的原地编辑(1/220,如实报告"历史被客户端改写"即可,不影响分组)。

### 1.4 Compaction 可确定性串联

06:43:21 的两条 2-message 小请求是 OpenClaw 的 compaction 调用(把旧会话喂给"context summarization assistant")。实测两个方向的链接都是**确定性子串匹配**:

- **向后**:compaction 调用的模型输出(summary 全文)逐字包含在新会话锚点消息("The conversation history before this point was compacted into the following summary:\n\n<summary>…")之内。已验证匹配成功。
- **向前**:compaction 调用的输入(`<conversation>\n[User]: …`)包含旧会话的首条用户指令原文(去掉时间戳前缀后)。已验证匹配成功。

因此「旧会话 → compaction 调用 → 新会话」可以串成一条**逻辑 Agent 线程**,不需要 LLM、不需要猜。当日的 e362da2d 会话即 96f5411a 会话经 compaction 的延续。(跨天场景:compaction 前身在前一天的日志文件里;`vmr report` 本来就接受多文件 glob,喂进来就能串上,只喂单日则如实标注"续接自不在输入内的会话"。)

### 1.5 任务边界与"本轮新增"

- **任务边界** = 增量(delta)或被替换尾部中出现**真实用户指令**。观察:96f5411a 会话 68 个请求中只有 3 处新指令 —— 其余 65 个请求全是同一任务内的 tool-use 循环。边界清晰。注意:新指令常出现在"尾部替换"类断链处(临时 wrapper 被真实用户消息替换),所以任务检测必须同时看严格前缀的 delta 和替换尾部的 diff,不能只看前者。
- 需要过滤的伪 user 消息:OpenClaw 的 wrapper("OpenClaw runtime context for the immediately preceding user message"、"[…] Conversation info (untrusted metadata)")是 user 角色但非指令;Anthropic 协议下 tool_result 也以 user 消息出现。规则:**user 消息且内容非纯 tool_result、非已知 wrapper 形态 → 新任务**。wrapper 识别是 OpenClaw 特定的显示优化(与设计文档 §5.5 quirk 修复同一哲学:证据驱动、失手无害——认不出 wrapper 顶多多切一个任务组,不丢数据)。
- **本轮新增** = `messages[lcp:]`。典型构成:上轮 assistant 回复(含 tool_calls)+ 本轮 tool 结果 + (偶尔)新用户指令。"最新指令"= delta 中最后一条真实 user 消息;没有则本轮是工具循环的延续,delta 中的 tool 结果就是"驱动本轮的新信息"。这正好回答"这一轮我们在干什么":展开 delta,折叠历史。

### 1.6 交叉验证:M3 报告的信号逐条核对(07-09 日志实测)

M3 报告(基于 07-11 日志)列出 23 个可提取维度。逐条拿 07-09 日志核对后,**采纳 8 项、修正 1 项、舍弃 6 项**;其余为已有能力(usage/endpoint/error/norm 等 report 已覆盖)。另有 1 项 M3 报告遗漏、本次核对中新发现的信号(chat_id)。

**✅ 采纳(实测成立)**:

1. **`Traceparent` trace_id 复用 —— 但粒度是"任务/轮次突发",不是会话**(修正 M3 报告的"同 session 复用"和本报告初版的"每请求独立",两个都不准确)。07-09 实测:223 条带 Traceparent 的记录只有 26 个 distinct trace_id;**每个 trace_id 只属于一个会话,无一跨会话**;组内 trace 段完全连续不交错。更关键的对齐:96f5411a 会话 68 请求分 3 个 trace 段(1+5+62),边界恰好是 3 次新用户指令(00:45 / 00:52 / 00:55);cda44f29 会话 4 个 trace 段对应 "say hi" / 查巡检 / 停 cron / 后续,与消息内容核对全部吻合。**结论:trace_id 段 = OpenClaw 一次用户轮的 tool-loop 突发,是任务切分的免费强信号**,与 LCP 用户指令检测互为交叉验证;compaction 调用无 Traceparent(实测 2/2)。该信号是 OpenClaw(OpenAI JS SDK + OTel)特定的,有则用、无则回退 LCP 规则。
2. **请求形态分类(tools 签名)**:tools 数量 × system 头部三分天下——67 工具的完整 agent(219 条)/ 2 工具 `[read, write]` 的缩减形态(4 条,即 §1.3 的 contextPruning 分支,system 也同步变小)/ 无工具的 compaction(2 条)。与 M3 报告在 07-11 数据上的 247/12/11 结构完全同构。实现上**不硬编码工具数量**,用「工具数 + 工具名列表 hash」做签名,签名变化即形态变化。
3. **compaction 记录的三重特征**:system 含 "context summarization assistant" + 无 Traceparent + 无 tools + 独有 `max_completion_tokens` 字段(实测 2/2 全部成立,与 M3 报告 11/11 一致)。多特征互为佐证,单看任一都够。
4. **finish_reason 可提取且有分析价值**:07-09 实测 tool_calls 203 / stop 18 / 无 4。`length`(截断)与 `stop`/`tool_calls` 的比例是任务健康度的直接指标;当前 report 完全没碰这个字段。
5. **usage 细分字段**:`completion_tokens_details.reasoning_tokens` 实测 6/221 条存在(MiniMax thinking 模式);`prompt_tokens_details.cached_tokens` 221/221。reasoning_tokens 值得在存在时提取(推理开销单列),缺失时置空不硬造。
6. **"ok 但截断"标志**:outcome=ok 且 attempts 末次 error 为 `truncated: …` 的记录(客户端拿到 200 但流中途断掉,内容不完整)。07-09 实测 2 条(`truncated: stream idle timeout`)。当前 report 把它埋在 endpoint 错误分布里,请求级视图里完全不可见——应提升为逐请求 flag 并进 INDEX 标记。
7. **已知模板标签(user 消息形态)**:heartbeat("[…] [OpenClaw heartbeat poll]")、dream diary、compaction 续接("compacted into the following summary")、`<conversation>` 喂料、runtime-context wrapper。07-09 无 heartbeat/dream 实例(那是 07-11 才配置的 cron 任务),但其余标签全部命中(compacted_session 155 条、wrapper 213 条、conversation_feed 2 条)。**词表必须可扩展、失配无害**:标不出就不标,数据一条不丢。heartbeat 类标签对统计尤其有用——把机械心跳从"真实工作量"里剔出去。
8. **chat_id(M3 报告遗漏,本次新发现)**:OpenClaw 的 "Conversation info (untrusted metadata)" wrapper 内嵌 JSON 含 `chat_id`(如 `user:ou_238fc3d9…`),**219/225 条可提取**,同会话内恒定,能区分驱动流量的飞书用户/群——这就是 M3 报告 §6 认为"❌ 不能定位"的 accountId 维度的日志内替代品(6 条缺失 = 2 compaction + 4 剪枝分支)。作为 best-effort 身份标签采纳。

**❌ 舍弃(实测冗余/更弱/越界)**:

| M3 报告项 | 舍弃理由 |
| --- | --- |
| Content-Length 区间分类(full 500K+ / reduced 100–200K) | 被 tools 签名完全替代;区间随会话长度漂移,不稳健。bytes 统计 report 已有 |
| messages 数区间分类 | 同上,是形态的果不是因 |
| turn 序号 = (msgs 数, ts) 二元组 | LCP 选父严格更强(msgs 数在剪枝/图片剪除时会回退,二元组会错序) |
| agent_id / sessionKey / accountId 交叉 Lobster `sessions.json` | 越界:report 只读审计日志,不读第三方应用状态;chat_id 已覆盖主要诉求 |
| User-Agent / stream_options / tool_choice 恒定值 | 无区分度,详单参数区已展示 |
| 全部 L2 项(意图/完成度/优先级/成本解读等需 LLM 的) | 按本任务边界暂舍;分组与特征提取完成后,它们是未来可选的离线后处理(读 report 产物,不碰 vmr) |

### 1.7 当期 tool call 提取与工具使用画像(07-09 + 07-11 双日志实证)

**需求**:统计"每个请求这一轮实际调用了哪些工具",历史 messages 里重复出现的旧 tool call 不重复计数,进而按 Agent/会话聚合出工具使用画像,识别从未使用的工具。

**方法——从 response 提取,而非从 request diff**:一个请求当期发起的 tool call,恰好就是**这个请求的响应**里模型输出的 `tool_calls`(SSE 流中 `delta.tool_calls` 的 `function.name` 每个 call 只出现一次,在首个分片;非流式则在 `choices[].message.tool_calls`)。历史重复全在 request 的 messages 里,response 里只有本轮——**只解析 response 就天然满足"不重复计数"**,且不依赖会话链(detail.go 的 SSE 重组已有同款解析,复用即可)。

**交叉验证**:用会话链做独立对照——请求 N 的 response tool_calls,应等于同会话请求 N+1 的 delta 中 assistant 消息携带的 tool_calls。两份日志全部干净链对上:07-09 为 197/197 一致,07-11 为 198/198 一致,**合计 395/395、0 错配**。方法成立。

**实测画像**(两日合并,完整形态 67 工具的 agent):

- 总计 420 次 tool call,**只覆盖 13 个 distinct 工具**:exec 270(64%)、process 32、write 25、read 19、web_fetch 19、message 18、sessions_yield 12、browser 8、sessions_spawn 8、edit 4、image 3、create_goal 1、memory_get 1。
- **54/67 个声明工具两天内一次都没被调用**:全部 39 个 `feishu_*` 工具 + agents_list、apply_patch、cron、gateway、get_goal、memory_search、nodes、session_status、sessions_history、sessions_list、sessions_send、skill_workshop、subagents、tts、update_goal。
- 会话间画像差异明显(印证"按 Agent 分析"的价值):07-11 的调研会话 web_fetch×19 为主,写作会话 write×4 为主,主会话 exec×98 一边倒;07-09 的 logo 会话用到 browser/image,其他会话没有。
- **裁剪收益可量化**:67 个工具的声明 JSON 占单请求 body 的 97KB/341KB(28%),其中从未使用的 54 个占 74KB——这 74KB 随每个请求重发(两天约 500 请求),即使有 prompt cache,也在每次 cache miss/write 时全额计费。工具裁剪是立竿见影的 token 成本优化项。
- 缩减形态(2 工具 `[read,write]`)与 compaction(0 工具)的调用同样可提取,数据自洽(声明 2 → 调用恰为 read/write)。

**边界**:非当日 vmr 流量的工具使用(如 Agent 直连其他模型)不在日志内,画像只代表经 vmr 的流量;"从未调用"的判定窗口 = 输入给 report 的日志范围,窗口越长结论越可靠——建议以 ≥1 周日志做裁剪决策(cron/低频工具可能周期性使用)。

---

## 2. 结论:能做到什么程度

| 目标 | 可行性 | 手段 |
| --- | --- | --- |
| Agent(会话)级分组 | ✅ 确定性 | 首条非 system 消息 hash;Anthropic 流量若带 `metadata.user_id`(Claude Code 会带 session id)则优先用它 |
| 任务级分组 | ✅ 确定性,双信号交叉 | trace_id 段(有 Traceparent 时)+ delta/替换尾部含真实 user 指令;两信号实测完全对齐 |
| 逐轮增量标注 | ✅ | 跳过 system 的 max-LCP 选父;delta 展开、被替换尾部标注 |
| 跨 compaction 串联 | ✅ 确定性 | 双向子串匹配(§1.4)+ compaction 三重特征识别(§1.6-3) |
| 逐请求特征提取(统计用) | ✅ | §3.4 特征表:形态签名、trace/chat_id、模板标签、finish_reason、usage 细分、截断标志等 |
| 当期 tool call 提取与工具画像 | ✅ 确定性,零重复计数 | 从 response 提取本轮 `tool_calls`(§1.7),交叉验证 395/395;按会话/形态聚合出使用画像与"从未调用"清单 |
| 是否需要调 LLM | **不需要** | 结构化信号已足够;LLM 总结进展是独立的可选增强,与分组正交,暂不做 |

已知边界(都不致命):

1. **anchor 碰撞**:两个会话首条用户消息完全相同(如每日 cron 用同一段文字触发)会并成一组。实测中 OpenClaw 给用户消息加了时间戳前缀,天然免疫;真碰撞时 trace_id 与 LCP 选父也会把它们连成可读的分支,不会张冠李戴。接受为已知限制。
2. **原地编辑**(图片剪除类)会让 delta 偏大(把编辑点之后的历史误报为"新增")。1/220 的频率,v1 如实报告"历史被客户端改写,自第 N 条起",后续可对 `[image data removed…]` 占位符做归一化再算 LCP。
3. **无 message 的记录**(被拒请求、非聊天体)归入"未分组"桶,行为同现状。
4. Claude Code 的 `metadata.user_id` 在两份日志中均无实例,实现时作为可选优先键,不可依赖。
5. trace_id / chat_id / 模板标签都是 **OpenClaw 形态的增强信号**:缺失时一切回退到协议通用的 anchor+LCP 主算法,分组照常工作,只是少几个标签。

---

## 3. 方案设计

### 3.1 分析算法(新增 `internal/report/session.go`,纯离线,只在 `vmr report` 里跑)

```
每条记录提取:
  keys[]    = 逐消息 hash(role + content + tool_calls + tool_call_id)
  sysKey    = system 消息(openai: messages[0].role==system;anthropic: 顶层 system 字段)单独 hash
  anchor    = 首条非 system 消息的 hash
  sessKey   = metadata.user_id 的 session 段(若有,Claude Code)|| anchor
  traceID   = headers.Traceparent 第二段(若有)
  chatID    = 从尾部 user wrapper 的 "chat_id" JSON 字段正则提取(若有)
  toolsSig  = len(tools) + hash(tool 名列表)          → 请求形态(完整/缩减/无工具)
  tags      = 模板标签(heartbeat / dream / compacted_session / conversation_feed / wrapper…)
  finish    = finish_reason(SSE 与 JSON 两种形态,复用 usage.go 的解析路径)
  usage+    = 现有四项 + reasoning_tokens(存在时)
  truncated = outcome=ok 且末次 attempt error 以 "truncated" 开头
  calls     = response 中本轮 tool_calls 的 name 列表(§1.7,复用 SSE 重组的解析;
              只看 response,历史重复零计数)
  declared  = request tools 数组的 name 列表(供"声明 vs 实用"对比,toolsSig 的原料)

会话分组:  按 sessKey 聚合(时间交错无影响)
组内选父:  parent = 此前请求中与本请求「非 system 消息序列」LCP 最大者(平手取最近)
增量:      delta = msgs[lcp:];replacedTail = parent.msgs[lcp:](通常是临时 wrapper)
任务切分:  traceID 变化(有 Traceparent 时的主信号)
           || delta/replacedTail 含真实 user 指令(通用信号,两者互为交叉验证)→ task++
compaction: 三重特征识别(§1.6-3)→ 标记为 compaction 调用;
           其输出 ⊂ 某会话 anchor 文本 → 链接新旧会话为同一 thread
编号:      会话 s01, s02…(按首请求时间);任务 t01…;轮次 r01…
会话标题:   首条真实用户指令的前 N 字预览;任务标题同理
```

内存:逐会话只需保留少量历史请求的 key 向量(选父窗口,如最近 8 条)+ anchor,不必全量驻留;225 条/天的量级下即使全量驻留也无压力。

### 3.2 详单渲染变更(`detail.go`)

1. **头部标识行**(概要表上方):`会话 s02「深入调研 Lobster AI token 成本…」 · 任务 t03「先停掉这个定时任务…」 · 任务内第 5 轮 / 会话内第 41 轮 · 本轮调用: exec×2, write · 上一轮: [链接]`,并列出 trace/chat_id/形态签名(有则显示)。compaction 调用类记录标 `[compaction] 为会话 s02 生成摘要 → 续接为 s04`。
2. **新增「本轮增量」区**,置于 ① 的 Messages 全列表之前:
   - delta 消息**全文展开**(这是本轮真正要读的内容;历史消息维持现有折叠);
   - "最新指令"高亮(delta 中最后一条真实 user 消息;无则标注"工具循环延续,无新指令");
   - `replacedTail` 标注:"上一轮尾部 2 条临时消息被替换"(🔴 列出);
   - system 变更时标注:"system prompt 相对上一轮有变化(43.4K→37.4K 字符)";
   - 截断请求显著标注:"⚠️ 客户端收到 200 但流中途断开(truncated: stream idle timeout)"。
3. 历史前缀部分(Messages 列表)每条加一个轻量标记(如 `#1..#73 ↺ 历史`),不重复渲染成本。

### 3.3 INDEX.md 分组(`WriteDetails`)

现有全局时间序总表保留(全局视角仍有价值),其上新增分组视图;截断/心跳等标签直接进表:

```
## 会话 s02 · 「你现在的任务是深入调研适合 Lobster AI…」 · chat user:ou_238f… · 68 轮 3 任务 · 00:45–01:12 · tokens 41M/86K
### t01 (承接自日志外的既有任务) · 1 轮
### t02 · 「你还记得 Amberark 这个项目吗…」 · 5 轮
| 轮 | 时间 | 增量 | finish | 结果 | 耗时 | tokens in/out | 文件 |
### t03 · 「…」 · 62 轮
…
## 会话 s04 · (s02 经 compaction 续接) · 142 轮 18 任务 · 06:43–12:14
```

文件名维持现状(时间戳排序语义不动),会话/任务信息只进 INDEX 与详单头部;若日后想在文件浏览器里直接按会话聚簇,再考虑加 `_s02-t03` 后缀,本轮不做。

### 3.4 逐请求特征导出:`vmr-requests.jsonl`(统计分析的核心交付物)

report 输出目录新增一个**每请求一行**的特征文件,把 §3.1 提取的全部字段落成扁平 JSONL——这是"后面做统计、做分析"的直接数据源,jq / DuckDB / pandas 一行就能读:

```jsonc
{
  "ts": "2026-07-09T00:52:12.491+08:00",
  "session": "s02", "task": "t02", "turn": 5, "session_turn": 41,
  "trace_id": "6d192e9d…", "chat_id": "user:ou_238f…",
  "shape": "tools:67/ab12cd34",            // 请求形态签名
  "tags": ["compacted_session"],            // 模板标签,可空
  "model": "agent", "protocol": "openai", "outcome": "ok",
  "endpoint": "openai/minimax/MiniMax-M3", "attempts": 1,
  "dur_ms": 8478, "ttft_ms": 1200,
  "msgs": 809, "delta_msgs": 3, "new_instruction": "你还记得Amberark这个项目吗？…",  // 前 80 字
  "finish_reason": "tool_calls", "truncated": false,
  "tool_calls": ["exec", "exec", "write"],   // 本轮实际调用(response 提取,可空;重复名 = 一轮多 call)
  "tools_declared": 67,                       // 声明数;全名单在 report JSON 的 tools 段,不逐行重复
  "tokens_in": 483000, "tokens_in_cached": 460000, "tokens_in_cache_write": 0,
  "tokens_out": 1200, "tokens_reasoning": 91,      // 无则省略
  "bytes_in": 391702, "bytes_out": 15230,
  "norm": ["model_rewrite", "done_appended"],
  "detail_file": "20260709-005212.491_agent_MiniMax-M3_ok.md"
}
```

设计约束:字段**全部来自规则提取**(§1.6/§1.7 采纳清单),提不出的省略而非硬造;该文件与 `vmr-report.json` 的关系是「明细 vs 聚合」——聚合表粒度不动,想按会话/任务/标签/finish_reason/工具切统计的,直接在明细上自己聚。report JSON 可同步加一个轻量 `sessions[]` 汇总段(会话数、每会话请求/任务数、token 合计),v1 可选。

### 3.5 工具使用聚合报告(服务于工具裁剪决策)

在 report JSON 新增 `tools[]` 段、Markdown 新增一张表,粒度 **形态签名 × 工具**(想看会话级画像的,用 §3.4 明细自己聚):

```jsonc
"tools": [
  { "shape": "tools:67/ab12cd34",
    "requests": 488,                       // 该形态请求数
    "declared": ["agents_list", "…67 个"],
    "calls": { "exec": 270, "process": 32, "write": 25, "…": 0 },   // 当期调用计数
    "never_called": ["feishu_bitable_app", "…54 个"],
    "declared_bytes": 99328 }              // 声明 JSON 的单请求字节成本
]
```

Markdown 渲染成两块:①调用排行(工具 × 次数 × 覆盖请求数);②**"声明但从未调用"清单 + 其字节成本**——这是"把不用的 tool 从 Agent 配置里去掉"的直接依据。表头注明统计窗口(输入日志的日期范围),提醒读者:窗口外的低频工具(cron 触发类)不在此判定内,裁剪决策建议用 ≥1 周窗口。

### 3.6 规模预估

`session.go` 约 350–400 行(指纹、分组、选父、任务切分、compaction 链接、特征提取)+ `usage.go` 扩展(finish_reason/reasoning_tokens/response tool_calls,约 80 行——SSE 解析复用 render.go 的重组逻辑)+ `detail.go`/INDEX 渲染改动约 150 行 + requests.jsonl 导出约 60 行 + tools 聚合段约 80 行 + 测试(用真实脱敏样本构造:严格前缀链、尾部替换、剪枝分支、compaction 链接、trace 段切分、截断标志、tool call 提取)。与审计格式无耦合变更——**不改设计文档 §9.2,不改运行时,纯 report 侧**。

---

## 4. 不做什么

- **不调 LLM 做任务总结**:分组与增量标注已让"这一轮在干什么"自解释(最新指令 + 新 tool 结果全文可见)。M3 报告的全部 L2 维度(意图分类/完成度评估/成本解读)属于"内容提炼",与本次"结构组织 + 特征提取"目标正交,留作路线图可选项(若做,也是读 report 产物的独立后处理,不碰 vmr)。
- **不做运行时打标**:所有分析都在 `vmr report` 离线完成,审计写入路径零改动,与 vmr"只记录不分析"的审计定位(设计文档 §9)一致。
- **不读日志之外的状态**:agent_id/sessionKey 的精确身份需要交叉 Lobster 的 `sessions.json`,越出"report 只消费审计日志"的边界;chat_id(日志内自带)已覆盖身份维度的主要诉求。
- **不为 OpenClaw 之外的客户端做投机适配**:trace_id、chat_id、模板词表、Claude Code 的 `metadata.user_id` 都按"有则用、无则回退通用规则"设计,与响应归一化的 quirk 哲学一致——失手时行为 = 现状(不分组也能看),永不更差。
