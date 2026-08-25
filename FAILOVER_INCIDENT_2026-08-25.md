<!-- Ver 2026-08-25 20:55, by ox-alpha -->
<!-- 2026-08-25 23:10 复查定稿：最小修复已落地并经逐文件源码复查（发现并修正 5 处文档残留，见 §9.2）；执行记录与验收结果见 §九。未 commit，待人工复核 -->
<!-- 事件分析报告（历史记录，非当前状态文档）。分析对象：logs/vmr-audit-2026-08-25.jsonl（885 条记录，737MB） -->
<!-- 配套产物：vmr analyze 全套输出在 /tmp/vmr-analysis-0825/（vmr-report.{md,json}、vmr-requests-failed.{md,jsonl}、stories/journey-j-lobster-*.md） -->
<!-- 2026-08-25 决策：§七 全量 quirk 模块方案经评审后**延后**（YAGNI，登记 KNOWN_ISSUES 1.48），本次按最小修复落地（见 §八 修订版）；该节 review 修订结论全部保持有效 -->
<!-- 2026-08-25 review 修订（经源码逐条核实）：①quirk profile 匹配键从 provider 名改为 model glob（provider 名是用户 config 自起名，改名即静默失效；且 thought_signature 按 providers=[google] 收窄会让经中继（如本配置的 cliproxy）转发的 gemini 流量退回 ErrClient，属行为回归）；②C1 勘误：L287「已知边界」属响应归一化（respnorm）侧，与本方案无关，不动它，并补全真正要改的文档落点；③D3 勘误：历史日志的 error_class 在写盘时已固化，重跑 vmr analyze 不可能把 9 次 rc 事件重计为 quirk -->

# 事件分析：Lobster 任务反复中断与 failover「失效」 — 2026-08-25

## 结论（TL;DR）

1. **当天 Lobster 的 10 次任务中断里，9 次是同一个错误**：`bai:deepseek-v4-flash` 返回 400
   *"The `reasoning_content` in the thinking mode must be passed back to the API."*
2. **failover 没接管的直接原因**：这批 400 被 `DefaultClassify` 归为 **ErrClient**，而路由对
   ErrClient 的策略是"立即原样返回客户端、不 failover"（`internal/router/router.go`
   `handleErrorResponse` 的 client 分支）。但这个归类在本场景是**误判**——请求本身是合法的
   OpenAI 协议消息序列，只是历史里混入了不含 `reasoning_content` 的 assistant tool-call 轮次；
   换任何一个候选端点（openrouter / volc / cliproxy）都大概率能接住。设计初衷"真·协议错误换端点也没用"
   是对的，坏在这些错误不是那一类。
3. **根因是跨供应商会话历史的结构性冲突**：DeepSeek thinking 系端点要求回传 reasoning_content，
   而多供应商路由下历史必然混入不产出该字段的轮次（volc / openrouter 的响应，甚至 bai 自己
   偶尔也不带）；叠加 bai 后端集群校验不一致（同样的请求 76 次成功、9 次失败），形成"随机爆炸"。
   客户端重试无法修掉深埋在历史里的毒轮次，于是同一个任务反复死在同一堵墙上。

---

## 一、当日流量与错误总览

| 维度 | 数值 |
| --- | --- |
| 审计记录总数 | 885（ok 869 / error 14 / canceled 2） |
| Lobster（`client_key_tag=lobster`）请求数 | 288 |
| Lobster 错误 | 10 次（9 次 reasoning_content + 1 次输入超限），全部 ❌client 类 |
| Lobster 取消 | 2 次（用户/客户端主动断开，非 vmr 故障） |
| 非 Lobster 错误 | 4 次（pimini ×1、dummy ×1、curl smoke test ×2） |

`vmr analyze` 侧的交叉印证：`vmr-report.md` 端点表显示 `openai:bai:deepseek-v4-flash` 当日
335 次 attempt、首要错误 `client ×9`——与本报告逐条核对的 9 次 reasoning_content 400 一一对应。

## 二、主线：9 次 reasoning_content 400 的完整因果链

### 2.1 错误本体

每次都是单 attempt、status 400、耗时 1.2~2.5s：

```json
{"error": {"message": "The `reasoning_content` in the thinking mode must be passed back to the API.",
           "type": "invalid_request_error", "code": "invalid_request_error"}}
```

发生时刻：16:34:27、16:52:48、17:02:30、17:08:02、17:22:12、17:23:52、17:34:00、19:27:18、19:38:01。
审计行号见附录 A。

### 2.2 为什么历史里会有缺 reasoning_content 的 tool-call 轮次

以 16:34:27 那条失败请求为例（62 条 message）：其中 assistant-with-tool_calls 消息共 19 条，
**3 条没有 `reasoning_content` 字段**（索引 [9]、[38]、[52]）。这些"毒轮次"有三个来源，当天全都出现了：

| 来源 | 证据 |
| --- | --- |
| **volc_coding_plan 的响应** | 该会话 15:53 起跑在 volc 上；毒轮次 [38]、[52] 分别产生于 16:29:57→16:30:42 与 16:32:39 两个 volc-served 请求的响应。volc 自己从不拒绝缺 rc 的历史 |
| **bai 自己偶尔也不产 rc** | 早上 06:53–06:59 的纯 bai 会话（OpenClaw heartbeat），[7]/[15]/[21]/[23] 四个毒轮次全是 bai 自己生成的，且 bai 当时照单全收——auto thinking 关闭时 deepseek-v4-flash 可以返回不带 rc 的 tool-call 轮次 |
| **openrouter 的响应** | 14:16–14:24 会话中段切到 openrouter stealth/ox-alpha（其推理内容走自有格式，不落 `reasoning_content`），之后的历史同样携带毒轮次 |

也就是说：**只要一个长会话经历过任何一次换端点（或 bai 自身的一次"没想"），历史就被永久污染**。

### 2.3 为什么 bai 时而接受时而拒绝

这是本案最关键的观测——**校验是非确定性的**：

- 结构完全相同的请求形态（历史含毒轮次 + 以 tool result 结尾）当日共 85 次：
  **76 次成功，9 次失败**；
- 失败请求尾部"连续缺 rc 的 tool-call 轮次数"从 0 到 4 都有（0 的两次说明连最新轮次都不缺也照样拒）；
- 16:32:53 → 17:02:21 之间，同一个带毒会话在 bai 上连续成功十余次，中间却穿插 3 次失败。

排除掉消息结构、末尾形态、请求大小等所有可测特征后仍无法分离 ok/error，唯一自洽的解释是：
**bai 的 `deepseek-v4-flash` 后端是一组异构副本（或按负载分流到不同版本的校验层），只有一部分
强制执行 DeepSeek thinking 模式的 rc 回传校验**。约 10% 的命中率与"少数严格副本"的图像吻合。

### 2.4 为什么 vmr 没 failover —— ErrClient 短路

`internal/adapter/classify.go` 的 `DefaultClassify` 对 4xx 的判定顺序：contentHint →
contextLimitHint → unknown-model → upstreamHint → vendorQuirkHint → 兜底 **ErrClient**。
这条错误体不含任何已登记的 marker（`vendorQuirkHint` 只认 Gemini 的 thought_signature 系措辞，
`contextLimitHint` 只认 context_length/prompt too long 系措辞），于是落进兜底 ErrClient。

而 `internal/router/router.go` `handleErrorResponse` 对 ErrClient 的策略是刻意的：

> Bad request: every endpoint would fail the same way. Return as-is.

立即把 400 原样写回客户端、终止整个 failover walk（有测试
`TestServe_ClientErrorDoesNotFailover` 守护这个语义）。**这个前提对"真·协议畸形"成立，
对今天这 9 次全不成立**：候选列表里的 openrouter/volc/cliproxy 都不做该校验，任意一个都能救活。
分类器少了一类知识——*"400 里点名了一个只有部分供应商要求的协议字段"*——正是
`vendorQuirkHint` 注释里自己声明的适用场景，只是 DeepSeek 的 `reasoning_content` 措辞没被登记进去。

### 2.5 为什么客户端重试也救不回来 —— 任务中断时间线

失败后 Lobster 的自动重试只会在尾部追加/微调消息（对比相邻两条记录可见 nmsg ±1、索引整体位移），
**深埋在历史中的毒轮次一个也去不掉**，所以重试撞上严格副本就再死一次。story 半区还原的用户视角：

| 时间 | 任务 | 发生了什么 |
| --- | --- | --- |
| 16:28–16:34 | t01「对比分析国内几家DNS…」（volc 上顺利推进） | 16:32:53 volc 429 限流 → **failover 正常接管**切到 bai；16:34:27 bai 严格副本首次拒绝，t01 死 |
| 16:37 | t02 "Continue from what's left over." | 用户手动续命；同会话继续间歇性踩雷 |
| 16:40–17:02 | t03/t04 补实测、加海外 DNS | 16:52:48、17:02:30 两次中断 |
| 17:07 / 17:18 / 17:23 | t05/t06/t07 连续三个 "Continue from what's left over." | 17:08:02、17:22:12、17:23:52 三连爆后会话放弃 |
| 17:28–17:34 | 新会话「之前做了一份DNS的对比分析报告…」 | 旧历史被重新注入（est≈94k），17:34:00 又炸 |
| 17:39 | t01 标题即用户原话：**「怎么又报错了？再检查一下。」** | 19:27:18 再炸 |
| 19:36 | t03「去分析一下为什么最近老是出现Agent的 failed」——用户让 Agent 自己查自己 | 两分钟后 19:38:01 同一错误把这次"自我诊断"也杀掉了；此后当天 Lobster 只剩心跳 |

9 次错误后的空窗合计约 50 分钟（198s+2s+3s+652s+72s+295s+324s+522s+927s）。

## 三、其余错误逐条归因

### 3.1 14:03:42 bai vision-exp 输入超限 —— 同一种短路，第二次误伤

Daily Finance Brief 定时任务跑到 est≈96k tokens 时，sticky 指着的 `bai:deepseek-v4-flash-vision-exp`
返回 400 `"Input token exceed the limit"` + `quota_limit_reached`。这个措辞同样不在
`contextLimitHint` 的 marker 表里 → ErrClient → 不 failover。而普通 `deepseek-v4-flash`
（同一候选列表、优先级相同）窗口更大，本可以立刻接住。任务死后 Lobster 重开新任务
（14:04 起 est 从 9.8k 重新爬），14:15、14:31 又各重试一轮才跑完。

### 3.2 两次 canceled —— 不是故障

- 14:13:05：bai 流式正常输出 114s 后客户端断开（用户中止/客户端超时）；
- 14:30:27：拨号 openrouter 才 2.6s、响应头未到即被取消（前一手 14:24:31 刚跑完一个 356s 的超长轮次）。

### 3.3 对照组：pimini 在 cliproxy 上的"同因不同类"

凌晨 01:12 cliproxy 的 OAuth grant 过期，同一个底层问题先后以两种形态出现：

| 时刻 | 上游返回 | 分类 | 结果 |
| --- | --- | --- | --- |
| 01:12:26 | 400 `{"error":"invalid_grant"}` | **ErrClient** → 立即返回，不 failover | pimini 请求死亡 |
| 01:12:48 | 503 `auth_unavailable: no auth available` | ErrTransient → 冷却 + failover | bai 接管，成功 |

OAuth RFC 6749 的 `invalid_grant` 是教科书级认证失败（应映射 ErrAuth：冷却 + 切换），
只因裹在非标准形状的 400 里就落进了 ErrClient。这不是 Lobster 的事，但完美演示了同一缺陷。

### 3.4 其余三条（与 Lobster 无关）

- 17:05:45 dummy → google gemini-3.1-flash-lite 400 `Unknown name "store"`：OpenAI-only 的
  `store:false` 字段未被 google 端点的 BuildRequest 剥离。小适配器缺口，顺带记录；
- 17:07:55 curl ×2 打 `/v1/responses`：`agent` 未配置任何 openai-responses 协议端点，
  dur=0、零 attempt，属预期内的 no-candidates（smoke test 用错协议）。

## 四、"failover 没起作用"的完整回答

**failover 本身当天工作正常**——19 个请求经多 attempt 成功恢复（rate_limit、network、5xx 类全部照常接管），
包括 16:32:53 那次 volc 429 → bai 的关键切换（讽刺的是正是它把带毒会话送到了 bai 面前）。
bai 上 8 次 rate_limit attempt、4 次 network attempt 无一漏到顶层 outcome。

真正没接管的只有一类：**被归为 ErrClient 的错误**（Lobster 10 次 + pimini 1 次 + dummy 1 次），
它们 100% 单 attempt 终结。机制总结成一句：

> ErrClient 短路的设计假设是"每个端点都会同样失败"；今天这 11 次全是该假设的误伤——
> 请求合法、仅个别供应商的私有校验不满足，换端点即可治愈，但分类器不认识这类错误。

## 五、影响评估

- 直接中断的 Lobster 任务：DNS 对比调研主线（6 次断点）、其两次续写会话（3 次断点）、
  Daily Finance Brief cron（1 次，连带 3 轮人工重试）；
- 用户感知："怎么又报错了？"直接写进了任务标题，最后一次尝试让 Agent 自查也被同一错误杀死；
- 空窗时间约 50 分钟（不含用户重发 prompt、重试的成本）；
- vmr 侧无数据损失、无误计费：所有错误均为上游明确 4xx，计费路径未受影响。

## 六、建议

**vmr 侧（按收益排序，均为 `classify.go` marker 表补充，改动面小）：**

1. **【已采纳，2026-08-25】** `vendorQuirkHint` 增加 `"must be passed back"`，目标类取括号里的
   零冷却形态：新增 `ErrQuirk` 类承接（切换 + 不冷却），不用 ErrEndpoint（长冷却会误伤健康端点，
   同病相怜的既有词条 thought_signature 一并修正）。这一条直接消灭当天 9/10 的中断；
2. **【已采纳，2026-08-25】** `contextLimitHint` 增加 bai 措辞 `"input token exceed"`；400 +
   `quota_limit_reached` 组合不再裸奔进 ErrClient（"quota" 关键字只在 429 分支生效）；
3. **【已采纳，2026-08-25】** 4xx 体含 OAuth 标准错误码（`invalid_grant` / `invalid_token` /
   `token has expired`）→ ErrAuth，让 cliproxy 型故障走冷却 + 切换，与它自己的 503 形态行为一致。

**流程侧：**

4. **【已执行，2026-08-25】** 三类误判的修复登记进 `docs/KNOWN_ISSUES_sonnet-5.md`；
   ErrClient 短路本身维持原判（真·畸形请求仍单 attempt 终结）；全量 quirk 模块方向
   登记为 §1 待定项（1.48）；

**客户端侧（知悉即可）：**

5. Lobster/pi 无法预知下游哪个供应商要求 rc 回传；在 vmr 修复分类之前，规避手段只有
   "发现 400 reasoning_content 后开新会话"，这正是用户当天实际在做的事。

**长期可选（需过设计关，本报告不拍板）：**

6. **【延后】** 在 adapter 层做证据制导的请求侧 quirk 修复（如对不产 rc 的供应商历史补齐/
   剥离标记字段）。注意这触碰"byte-faithful passthrough"不变式，须按 Part 1 设计文档
   sanctioned deviation 的门槛论证。与 §七 的全量模块方案一并登记 KNOWN_ISSUES 1.48 待定。

---

## 附录 A：当日 16 error + 2 canceled 明细

audit 行号对应 `logs/vmr-audit-2026-08-25.jsonl`，与 `vmr-requests-failed.md` 索引一致。

| 行号 | 时刻 | 客户端 | 端点 | status | 错误 | 分类 | 结局 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 109 | 01:12:26 | pimini | cliproxy:gemini-3.7-flash-high | 400 | invalid_grant | client | 单 attempt 终结 |
| 455 | 14:03:42 | lobster | bai:deepseek-v4-flash-vision-exp | 400 | Input token exceed the limit / quota_limit_reached | client | 单 attempt 终结 |
| 466 | 14:13:05 | lobster | bai:deepseek-v4-flash | 200 | canceled by client（114.9s 流中） | canceled | — |
| 487 | 14:30:27 | lobster | openrouter_free-bfs:stealth/ox-alpha | - | canceled by client（2.6s，头未到） | canceled | — |
| 513 | 16:34:27 | lobster | bai:deepseek-v4-flash | 400 | reasoning_content must be passed back | client | 单 attempt 终结 |
| 548 | 16:52:48 | lobster | 同上 | 400 | 同上 | client | 同上 |
| 574 | 17:02:30 | lobster | 同上 | 400 | 同上 | client | 同上 |
| 600 | 17:05:45 | dummy | google:gemini-3.1-flash-lite | 400 | Unknown name "store" | client | 单 attempt 终结 |
| 618 | 17:07:55 | (curl) | —（agent 无 responses 端点） | - | no candidates | error | dur=0 |
| 619 | 17:07:55 | (curl) | 同上 | - | 同上 | error | dur=0 |
| 621 | 17:08:02 | lobster | bai:deepseek-v4-flash | 400 | reasoning_content | client | 单 attempt 终结 |
| 661 | 17:22:12 | lobster | 同上 | 400 | 同上 | client | 同上 |
| 664 | 17:23:52 | lobster | 同上 | 400 | 同上 | client | 同上 |
| 695 | 17:34:00 | lobster | 同上 | 400 | 同上 | client | 同上 |
| 872 | 19:27:18 | lobster | 同上 | 400 | 同上 | client | 同上 |
| 884 | 19:38:01 | lobster | 同上 | 400 | 同上 | client | 同上 |

## 附录 B：关键代码位置

- `internal/adapter/classify.go` — `DefaultClassify` 的 4xx 判定链与四张 marker 表
  （contentHint / contextLimitHint / upstreamHint / vendorQuirkHint）；本次三类误判全部因为
  错误措辞不在任何表里而落入兜底 `ErrClient`；
- `internal/router/router.go` — `handleErrorResponse` 的 `class == core.ErrClient` 分支
  （"Bad request: every endpoint would fail the same way. Return as-is."，约 L471）；
  `Serve` 的 failover 主循环；
- `internal/router/router_serve_test.go` — `TestServe_ClientErrorDoesNotFailover`（守护现状语义；
  2026-08-25 修复落地后**原样保留**：其 body "invalid request" 不含任何 quirk 词条，真坏请求仍单
  attempt 终结）；新增 `TestServe_VendorQuirkFailsOverWithoutCooldown` 守护 rc-400 零冷却切换。
  三类误判的词表级验收在 `internal/adapter/classify_test.go`（三个真实 body 各归目标类）与
  `internal/core/core_test.go`（`ErrQuirk` → "quirk" 标签锁定）；
- `config.yaml` — `agent` 的候选序列（bai/sensenova → openrouter → cliproxy → volc → 全局 fallback），
  即被 ErrClient 短路浪费掉的整条救援链。

---

## 七、方案：端点级 quirk 模块（统一处理机制）

### 7.1 现状：错误分类目前是怎么处理的（代码事实）

先核实一遍现状，方案才能贴着架构走：

- **分类器拿不到端点上下文**：`Adapter.ClassifyError(status int, body []byte) core.ErrorClass`
  （`internal/adapter/adapter.go:46`）只收到 status + body——provider、model、协议一概不知。
  因此"bai 的 deepseek 系强制 rc 回传"这类结构性事实，目前只能靠 body 措辞反推。
- **三协议 adapter 全部委托同一个全局分类器**：openai / anthropic / openairesponses 的
  `ClassifyError` 都只调 `adapter.DefaultClassify`；唯一协议级特判是 anthropic 的 529→`ErrTransient`
  （协议语义，已实证）。`DefaultClassify`（`classify.go`）的 4xx 判定链是
  contentHint → contextLimitHint → unknown-model → upstreamHint → vendorQuirkHint → 兜底 `ErrClient`，
  四张全局 marker 表把**所有**厂商的知识混排在一起。
- **现有惯例已经预告了正确方向**：`openairesponses.go:78` 的注释写明"任何这个端点专属的 quirk
  （DefaultClassify 词表没覆盖的措辞）应在此处针对真实响应补上"——即惯例本就是 per-adapter 文件
  特判，只是到今天为止只有 529 一个实例，其余 vendor 知识全部堆在共享词表里。
- **先例**：`internal/respnorm/minimax.go`——MiniMax 的响应侧 quirk 知识单独成文件，
  `respnorm.go` 只留通用机制、在决策点调用它。这是本方案要推广的既有模式。
- **既有约束**：KNOWN_ISSUES §2.2「不引入端点级通用运行时 quirks 插件系统：坚持编译期确定性，
  只对已证实的厂商行为差异做受控修复」；Part 1 设计文档的已知边界「quirk 修复靠全局嗅探而非
  按端点声明」（当时否决的是**配置面**与新概念，不是 per-vendor 文件本身）。

### 7.2 问题：全局词表为什么撑不住

1. **vendor 知识作用域错位**：quirk 本质是 provider×model 专属的，却被放在协议共享的全局表里。
   加一个新厂商 = 往共享表加词，没有任何一个文件为单个端点负责；将来这些行为还会变
   （MiniMax 已证明每个厂商都会各自出各自的问题），共享表只长不拆。
2. **分类时没有端点上下文**：词表能表达的只有"这段措辞→这个类"，表达不了
   "这个端点家族强制某条协议约束"这种归属关系。
3. **类别决策是文本嗅探赌注**：词表没命中就落 `ErrClient`（当天 9 次）；命中了映射到哪个类
   也是拍脑袋——`vendorQuirkHint`→`ErrEndpoint` 会对一个明明健康的端点记长冷却，制造流量振荡。
4. **表达不了"端点没坏，坏的是会话历史形态"**：rc 回传错误与内容违规（ErrContent）、
   上下文超窗（ErrContextLimit）语义不同，硬套会让审计标签说谎。
5. **可扩展性**：厂商越多共享表越长，互不相关的词互相干扰，判定链越来越难读。

### 7.3 设计原则

- **编译期确定性**：per-vendor 模块文件 + `init()` 静态注册，不是运行时插件系统——
  这是 §2.2 立场的**细化**（从"全局词表"到"per-vendor 文件"），不是推翻。
- **零新配置面**：不加 `quirks:` 配置。守住"新配置面 = 用户须理解各厂内部"的反对理由；
  模块归属对用户不可见、也不该可见，`config.yaml` 一行不改。
- **行为归其行为所有者**：分类行为留在 adapter，vendor 知识留在各自的 quirk 模块文件——
  core 准入规则（"core 只装跨半区共享类型"）不动，router 不新增分类逻辑。
- **一个统一机制，知识按端点分布**：`respnorm/minimax.go` 模式的推广，从响应侧推广到错误分类侧。
- **每个 quirk 是声明式规则**：status + 措辞 → 目标类；新增 vendor 不再改通用判定链，
  只新增一个模块文件。

### 7.4 机制设计

**(a) 新 ErrorClass `ErrQuirk`** —— 语义："端点专属协议约束拒绝了这条请求"；处理与
`ErrContent`/`ErrContextLimit` 相同（**切换 + ReportNeutral 零冷却**——端点没坏，坏的是这条
会话的历史形态），但审计标签独立（`"quirk"`），不把"历史形态"误标成"上下文超窗"。
落点：`internal/core/core.go`（枚举 + `String()` 加 case）；`internal/router/router.go`
（neutral 分支 `class == core.ErrContent || class == core.ErrContextLimit` 加 `|| core.ErrQuirk`）；
`internal/router/probe.go` 的 neutral 分支同加（一致性；探针体极小实际永不触发）。
audit/report 按 ErrorClass 字符串自动分桶，无需改动。

**(b) 接口签名加端点上下文**：`ClassifyError(status int, body []byte, ep *core.Endpoint)`。
调用点：`router.go:444`（`handleErrorResponse` 增加 `ep` 参数，tryOne L416 传入）、`probe.go:91`。
三个 adapter 实现改为委托统一入口 `adapter.Classify(ep, status, body)`；anthropic 保留 529
前置短路。diagnose 不调 ClassifyError，无改动。

**(c) 统一入口 + 注册表（新文件 `internal/adapter/quirk.go`）**：

```go
type Quirk struct {
    Name    string           // 稳定标签，日志/审计用
    Status  []int            // 匹配的 status；空 = 任意
    Markers []string         // body 小写子串，any-of；空 = 仅按 status
    Target  core.ErrorClass  // 命中后的目标类
}

type QuirkProfile struct {
    Name      string
    Providers []string   // 配置里的 provider 名；空 = 任意。慎用：provider 名是用户
                         // config 自起名（非稳定标识），按它匹配 = 用户改名即静默失效；
                         // 本方案三个 profile 全部留空，靠 model glob + 措辞特异度兜住
    Models    []string   // model glob；空 = 任意
    Rules     []Quirk
}

func RegisterQuirkProfile(p QuirkProfile)        // init() 注册
func ProfileFor(ep *core.Endpoint) *QuirkProfile // 首匹即止。不依赖注册顺序做优先级
                                          // （Go 跨文件 init 顺序只是工具链惯例、非语言规范），
                                          // profiles 必须互不重叠并由测试守护
func Classify(ep *core.Endpoint, status int, body []byte) core.ErrorClass
```

- **统一处理顺序**：端点 profile（最具体）→ 协议级覆盖（529）→ `DefaultClassify`（通用兜底）。
  更具体的先判；`DefaultClassify` 四张通用表原样保留（content/context/model/upstream 是真跨厂商
  语义，不属于任何单个端点）。
- **注册表读写**：照抄 `adapter.go` 的 `Register`/`Get` 模板（mutex 保护的 copy-on-write +
  atomic 读）——CLAUDE.md 不变式要求并发写不丢条目，模板现成。
- **ProfileFor 是冷路径**：只在 ≥400 时调用一次，注册表 < 10 条，线性扫描可接受。

### 7.5 分类规则归属总表（本次三类误判 + 迁移项各归其位）

| 事件 | 端点 | 现状分类 | 归属（新模块） | 新分类 |
| --- | --- | --- | --- | --- |
| reasoning_content ×9（16:34–19:38） | bai:deepseek-v4-flash | ErrClient | `quirk_deepseek.go`：providers 留空 × models=[deepseek-v4-flash*]，rules=[400 + "must be passed back" → ErrQuirk]。**不按 provider 名匹配**：该措辞是 DeepSeek 官方错误文本，任何转售 deepseek 系模型的中继都可能返回；provider 名是用户 config 自起名（"bai" 只是本配置给 api.b.ai 起的名），按它收窄 = 换名即静默失效。marker 用完整短语而非裸 "reasoning_content"（后者可能在无关错误里出现） | **quirk**（切换、零冷却） |
| Input token exceed（14:03:42） | bai vision-exp | ErrClient | `classify.go` 的 contextLimitHint 全局词表（"输入超窗"是通用语义族，换任何厂商都受益；且 contextLimitHint 在判定链中先于 upstreamHint，能压住 body 里的 quota_limit_reached 措辞） | **context_limit** |
| invalid_grant（01:12:26） | cliproxy | ErrClient | `quirk_relay.go`：OAuth 标准错误码（invalid_grant / invalid_token / token has expired）→ ErrAuth（长冷却 + 切换，与它自己的 503 形态行为一致）。同样不按 provider 名匹配（"cliproxy" 是本地中继的自起名）：RFC 6749 错误码出现在 4xx body 里几乎必是认证层问题，按措辞全局判定即可 | **auth** |
| thought_signature（既有词条） | google gemini | vendorQuirkHint → ErrEndpoint | 迁出全局表 → `quirk_google.go`（providers 留空 × models=[gemini*]），行为不变、位置变。**providers 必须留空**：本配置里 cliproxy 就在转售 gemini-3.7-flash-high，按 providers=[google] 收窄会让中继上的同错误从现状的 ErrEndpoint（failover）退回 ErrClient（短路不 failover），是行为回归 | **endpoint**（不变） |
| Unknown name "store"（17:05:45） | google | ErrClient | `quirk_google.go` 可选项（见 7.6） | **quirk**（可选） |

### 7.6 边界与刻意不做

- **请求侧 quirk 不做**：给 google 剥离 `store` 字段、给历史补 rc——都触碰 byte-faithful
  passthrough 不变式，且 Part 1 对 `store:true` 已有明文决定（"交给上游自然拒绝，vmr 不替
  客户端做校验"）。本方案只动**分类侧**：把 google 的 store 400 映射到 ErrQuirk 让 failover
  试下一候选是无害的（下一候选可能接受该字段），但优先级低、可延后。
- **不拆通用词表**：content/context/model/upstream 四表是真跨厂商语义，留在全局。
- **不预埋请求侧 hook**：`QuirkProfile` 未来若要扩展 `RewriteRequest`，是类型层面的增量，
  现在不预埋接口（YAGNI）。
- **不做 402/404 拆分**：ProviderGroup 记录里的"分级 Failover"维持原判，与本方案无关。

### 7.7 与既有约束的关系

- §2.2「不引入端点级通用运行时 quirks 插件系统」：本方案是编译期 per-vendor 模块
  （init() 静态注册、无配置面、无运行时加载）——立场不变，形式细化，需同步修订该条措辞。
- Part 1 已知边界「quirk 修复靠全局嗅探而非按端点声明」：修订为"按厂商编译期声明"，
  但守住该边界当初的两条理由（不加配置面、不引入运行时插件）。
- core 准入规则：只新增一个 ErrorClass 枚举值，不改 `core.Endpoint`、不新增 core 类型——
  最小侵入。

## 八、Action Plan（执行计划）

> **【2026-08-25 修订版：按决策改为最小修复，以下为实际执行的版本】** 原 Action Plan
> （A1–D3，服务于 §七 全量模块）随 §七 一并延后；本次执行不新增注册表、不改
> `ClassifyError` 接口签名、不加配置面。改动共 4 个源文件 + 测试 + 文档。

### M1. core 新增 `ErrQuirk`

- 文件：`internal/core/core.go`
- 改动：ErrorClass 枚举在 `ErrContextLimit` 之后加 `ErrQuirk`，注释写明语义（端点专属协议
  约束拒绝了这条请求：会话历史形态问题，端点本身健康；处理同 ErrContent/ErrContextLimit：
  切换 + 零冷却）与为什么不复用 ErrContextLimit（审计标签诚实性）；`String()` 加 case
  `"quirk"`（default 兜底落 "transient"，必须显式加 case 才有正确标签）。
- 验收：`TestErrorClassString` 加 `ErrQuirk: "quirk"` 条目。

### M2. classify.go 词表修复（三类误判各归其位）

- 文件：`internal/adapter/classify.go`
- 改动：
  - `vendorQuirkHint` 增加 marker `"must be passed back"`（完整短语，不用裸
    `"reasoning_content"`——后者可能出现在无关错误的描述文字里）；映射从 `ErrEndpoint`
    改为 `ErrQuirk`（thought_signature 同步受益：健康端点不再因会话历史形态被记长冷却）；
  - `contextLimitHint` 增加 `"input token exceed"`（bai 措辞；判定链中先于 upstreamHint
    与 model-unknown，能压住 body 里同时出现的 `quota_limit_reached`——"quota" 关键字
    只在 429 分支生效，互不干扰）；
  - 4xx 判定链在 contentHint 之后增加 OAuth 错误码判定（`invalid_grant` / `invalid_token` /
    `token has expired` → `ErrAuth`）：镜像 403 分支"content 先于 auth"的既有结构；
    RFC 标准错误码出现在 4xx body 里几乎必是认证层问题。
- 验收：classify_test 新增/更新用例——rc body → `ErrQuirk`；thought_signature 三例期望改
  `ErrQuirk`；14:03:42 真实 body → `ErrContextLimit`；01:12:26 真实 body → `ErrAuth`；
  既有负例（真坏请求仍 ErrClient）全部保持。

### M3. router/probe 的 neutral 分支加 ErrQuirk

- 文件：`internal/router/router.go`、`internal/router/probe.go`
- 改动：`handleErrorResponse` 的 neutral 分支（ReportNeutral + 继续 failover、零冷却）与
  `runProbe` 的 neutral 分支各加 `core.ErrQuirk`。
- 验收：router_serve_test 新增 `TestServe_VendorQuirkFailsOverWithoutCooldown`（rc-400 →
  下一候选成功、首端点无冷却）；既有 `TestServe_ClientErrorDoesNotFailover` 保持绿
  （其 body "invalid request" 不含任何 quirk 词条）；server_test 的
  `TestVendorQuirkThoughtSignatureContinuesFailover` 行为断言不变（仍 failover），仅注释同步。

### M4. 文档同步

- `docs/VirtualModelRouter_Design_v4_Core.md`：错误分类节 ErrorClass 表补 ErrQuirk 行 +
  判定链说明；流程图行（L98）补零冷却切换路径；Responses 段（L77）"未新增 vendor sniff"
  一句按新现状改写；健康探测段（L314）neutral 分类集合补 ErrContextLimit/ErrQuirk
  （顺带修正漏掉 ErrContextLimit 的既有 stale）；决策表补一行（ErrQuirk vs 复用
  ErrContextLimit）。不动 L287 已知边界条目（属 respnorm 响应侧，与本改动无关）。
- `docs/KNOWN_ISSUES_sonnet-5.md`：三类误判修复闭环记录；§1 新增 1.48（端点级 quirk 模块
  统一机制 + sticky 降级优化，延后待定）；§0 计数与 §4 总表同步。
- `CHANGELOG.md`：[Unreleased] Fixed 三条。
- 本报告：§六 建议标注采纳状态（已同步）；附录 B 补测试位置。

### 验收清单

- [x] `go build ./...`、`go test ./... -race` 全绿（含 `go vet`、`gofmt -l` 空、`archtest` 全绿）
- [x] 三类误判在单测中各落目标类（quirk / context_limit / auth，`classify_test.go` 用当日真实 body 逐字断言）
- [x] 真坏请求（无 quirk 词条）仍单 attempt 终结（`TestServe_ClientErrorDoesNotFailover` 保持绿）
- [x] rc-400 后零冷却、下一候选被尝试（`TestServe_VendorQuirkFailsOverWithoutCooldown`）
- [x] 无新配置面、无接口签名变更，config.yaml 零改动
- [x] 设计文档 / KNOWN_ISSUES / CHANGELOG / 本报告四方一致


## 九、执行情况与最终总结（2026-08-25 复查后定稿）

### 9.1 执行内容

按 §八 修订版（最小修复方案 B）完成，全部改动经逐文件源码复查：

**代码（4 个源文件 + 5 个测试文件，无接口签名变更、无配置面）**

| 文件 | 改动 |
| --- | --- |
| `internal/core/core.go` | 新增 `ErrQuirk` 枚举（位于 `ErrContextLimit` 之后、audit-only 四值之前），`String()` 显式 case `"quirk"`；doc comment 写明语义与不复用 ErrContextLimit 的审计标签理由 |
| `internal/adapter/classify.go` | ① `vendorQuirkHint` 增加 marker `"must be passed back"`，映射 `ErrEndpoint` -> `ErrQuirk`；② `contextLimitHint` 增加 `"input token exceed"`；③ 新增 `authHint`（`invalid_grant`/`invalid_token`/`token has expired` -> ErrAuth），插在 contentHint 之后（镜像 403 分支"content 先于 auth"结构）。文件恰为 200 行（archtest 预算上限） |
| `internal/router/router.go` | `handleErrorResponse` 的 neutral 分支（ReportNeutral + 继续 failover）加入 `ErrQuirk` |
| `internal/router/probe.go` | `runProbe` 的 neutral 分支同步加入 `ErrQuirk`（探针槽归还不变式） |

**测试**

- `internal/adapter/classify_test.go`：三个真实错误 body 逐字入库（audit 行 513/455/109）--rc 回传 -> `ErrQuirk`、bai input token exceed（含 `quota_limit_reached` 同现）-> `ErrContextLimit`、cliproxy `invalid_grant` -> `ErrAuth`；thought_signature 三例期望改为 `ErrQuirk`；全部真坏请求负例保持 `ErrClient`。
- `internal/router/router_serve_test.go`：新增 `TestServe_VendorQuirkFailsOverWithoutCooldown`（rc-400 -> 下一候选 200、首端点 `Health.Available` 仍为 true）；`TestServe_ClientErrorDoesNotFailover` 原样保留且保持绿（其 body "invalid request" 不含任何 quirk/auth/context 词条）。
- `internal/core/core_test.go`：`TestErrorClassString` 锁定 `ErrQuirk -> "quirk"`（report 按 error_classes 字符串分桶，防 rename 静默碎裂）。
- `internal/server/server_test.go` / `active_probe_test.go`：仅注释同步（thought_signature 现归 ErrQuirk；neutral 分支描述更新）。

**文档（四方一致）**

- `docs/VirtualModelRouter_Design_v4_Core.md`：流程图行（零冷却路径）、错误分类表（+ErrQuirk 行、ErrAuth 扩 OAuth 错误码）、ErrQuirk 论证段、vendor 证据 bullet（DeepSeek rc / bai / cliproxy）、嗅探词表清单（+认证类）、`vendorQuirkHint` 展开段（归 ErrQuirk + 判定顺序补认证词表）、Responses 段、探测段两处 neutral 集合（顺带修正漏 ErrContextLimit 的既有 stale）、冷却清单行、决策表新行。
- `docs/KNOWN_ISSUES_sonnet-5.md`：§3 新增第 45 条闭环记录；§1 新增 1.48（全量 quirk 模块 + sticky 降级，延后待定，含触发条件）；§0 分布计数 14 -> 15 条；§2.2 quirk 插件系统条目补注（延后的是规模机制，不是取舍本身）；§4 总表同步 1.48 行。
- `CHANGELOG.md`：[Unreleased] Fixed 首条（英文，与文件语言一致）。
- 本报告：§六 建议标注采纳状态、§七 盖延后横幅（保留勘误后设计供将来升级复用）、§八 整体替换为实际执行的 M1–M4、附录 B 补测试位置、验收清单全勾。

### 9.2 复查发现并修正的问题

落地后按用户要求做了一次逐文件全面复查（对照源码，非只看 diff），发现并当场修正：

1. **设计文档 L243 `vendorQuirkHint` 展开段漏改**：仍写"归类为 `ErrEndpoint`"，且判定顺序缺认证词表--已改写为 ErrQuirk 表述并补全顺序（内容 > 认证 > 上下文超限 > 模型未知 > upstream > vendorQuirk > 兜底）。
2. **设计文档 L209 ErrEndpoint 行的"400+嗅探"易误读**：补明"模型未知、upstreamHint"两类，防止读者以为 quirk 也归 ErrEndpoint。
3. **设计文档 L321 冷却清单只列了 ErrContent 零冷却**：改为"请求侧误配三类零冷却（ErrContent/ErrContextLimit/ErrQuirk）"。
4. **日期错误**：系统时钟为 2026-08-25 22:49，但本次所有修订标注误写为 2026-08-26（共 11 处，涉及本报告与 KNOWN_ISSUES）--已统一改回 2026-08-25，与实际修订时间一致。
5. **CHANGELOG 语言不一致**：新增条目误用中文，而该文件全英文--已重写为英文。
6. CHANGELOG `[0.6]` 历史段落里 vendorQuirkHint 的 ErrEndpoint 表述**保留不改**：CHANGELOG 是过程记录文档，0.6 发布时的行为确实是 ErrEndpoint，改写反而破坏历史。

### 9.3 复查确认无疏漏的方面

- **`ClassifyError` 全部调用点**（`router.go:444`、`probe.go:91`）均已覆盖 ErrQuirk；diagnose/replay/smoke 不调用 ClassifyError（grep 确认），无需改动。
- **健康侧降级安全**：ErrQuirk 永远走 ReportNeutral，不可能到达 `Health.ReportFailure` 的 switch；即便未来误达，其 default 分支也是短冷却（无害降级）。
- **分析侧零改动即可兼容**：report/reqdetail/story 对 error_class 一律按字符串泛型分桶（`map[string]int`），新标签 `"quirk"` 自动出现；`TestErrorClassString` 锁定标签防漂移。
- **`.zh` 姊妹文档**：`UserGuide.zh.md` 与 `README.zh.md` 均不枚举错误类别清单（grep 确认仅泛指 error_class 字段与 breakdown），无需同步；设计文档中文版即本体。
- **Quota/Strategy 两份设计文档**：不涉及本次分类变化（Quota 只引用 429 的 ErrEndpoint 归类，未变）。
- **archtest**：`classify.go` 恰 200 行压线（预算 200，测试断言 `n > limit` 才失败）；`ErrQuirk` 注释块精简过两轮以回到预算内。文档引用完整性检查（doc_refs_test）在所有文档改动后重跑全绿。

### 9.4 验收结果

`go build ./...` / `go vet ./...` / `gofmt -l`（空）/ `go test ./...` / `go test -race ./...`（全量）/ `internal/archtest` 全部通过；`config.yaml` 零改动；无新配置面、无接口签名变更。

### 9.5 未做与后续

- **真实端点回放验证未做**（原计划 D2 性质）：需要活流量与真实上游，等价保障是三个真实 body 已逐字进单测锁定。
- **全量 quirk 模块 + sticky 降级**：延后待定，登记 `docs/KNOWN_ISSUES_sonnet-5.md` §1.48（触发条件：词表互相干扰/误命中，或 sticky 重复往返可观测拖慢中毒会话）；本报告 §七 留有完整设计供升级时复用。
- **提交**：按用户要求未 commit，等待人工复核。
