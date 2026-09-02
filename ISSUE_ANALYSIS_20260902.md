# vmr 问题深析与修复规划
<!-- 源自 PROJECT_REVIEW_REPORT_opus5_20260902.md；每条均经源码核实 -->

---

## 说明

本文档对 Review 报告中所有成立的问题逐条展开分析，格式统一为：
- **问题描述**：通俗化，结合可感知的实例
- **负面影响**：修与不修的实际区别
- **根因**：一句话精准定位
- **建议方案**：简洁可操作

最后按 ROI × Domain 分组，规划子 Agent 处理单元。

---

## S-3 · 分析半区在"猜"路由半区的行为，而不是读它记下来的事实

**ROI：极高 | Domain：跨 D1/D2/D5 | 批次：二（优先）**

### 问题描述

想象餐厅收银系统：服务员完成送餐后，系统记一笔"已收费"；但账本程序不读这个标记，而是靠推理——"状态码 200 且有响应体"就认为收费了。这套推理大部分时间是对的，直到厨房加了"食物安全检查"：发现问题时退回食物并退款，但 HTTP 状态码仍是 200，只在错误字段写了"content"。结果账本多记一笔，实际上从未收费。

这就是 vmr 的 soft-block（软阻断）情形：

- **路由侧**（[`softblock.go:88`](file:///Users/stanford/code/vmr/internal/router/softblock.go#L88)）：判定为软阻断时，设 `ErrorClass="content"`、保持 2xx 状态码，然后**直接 return，不进 `forwardSuccess`，配额一分钱不扣**。
- **分析侧**（[`report/ingest.go:146`](file:///Users/stanford/code/vmr/internal/report/ingest.go#L146)）：判断是 `a.HasResponse && a.Status < 400`——软阻断完全满足，`Forwarded++`。
- **破坏的恒等式**（[`report/rows.go:266-273`](file:///Users/stanford/code/vmr/internal/report/rows.go#L266)）：注释明写"§2.5 重算列 = Forwarded × multiplier"，每次软阻断让重算列比实扣多一次请求。

第二个错误是 anthropic 截断流的 usage 判定：

- 路由侧（[`respnorm/usagesniff.go:100-105`](file:///Users/stanford/code/vmr/internal/respnorm/usagesniff.go#L100)）：`message_start` 事件的 Out 是约 1 的占位值，路由侧故意只标记 In 侧已嗅到。
- 分析侧（[`chatmsg/usage.go:88`](file:///Users/stanford/code/vmr/internal/chatmsg/usage.go#L88)）：判断是 `u.In > 0 || u.Out > 0`（析取），只要 In > 0 就认为 usage 完全可信，把 Out≈1 当精确值用。
- 结果：[`report/providerquota.go:247-252`](file:///Users/stanford/code/vmr/internal/report/providerquota.go#L247) 的 §2.5 重算得 Out=1，路由实扣 `max(1, outEst)`，**系统性少算 output token 且渲染为精确值**。

### 负面影响

| 不修 | 修后 |
|------|------|
| 每次 soft-block，§2.5 重算列多算一次请求，操作员核对配额时数字对不上 | `Forwarded` 布尔直接读路由事实，恒等式重新成立 |
| anthropic 截断流的 out-token 永远显示精确值 1，实际扣费远高于此 | 分侧 `UsageInOK`/`UsageOutOK` 让报表知道哪侧精确、哪侧是估算 |
| 差分测试（`quota_parity_test.go`）显示绿色，制造虚假安全感——比没有测试更危险 | 测试词汇表扩充后能捕捉到后续任何新的 attempt 形状 |

### 根因

`201fa08` commit 新增"分侧 usage sniff"，只改了 5 个路由侧文件，**没有同步扩展审计记录能表达的状态**。`Manifest.UsageOK` 是单布尔，物理上无法表示"In 已知、Out 未知"。分析侧读旧字段、做旧推理，完全不知道路由侧语义已经变了。

### 建议方案

① [`audit/audit.go`](file:///Users/stanford/code/vmr/internal/audit/audit.go) 的 `Attempt` struct 增 `Forwarded bool`，唯一置位点是 `router.forwardSuccess`；分析侧改读这个字段而不是反推。
② [`chatmsg/usage.go`](file:///Users/stanford/code/vmr/internal/chatmsg/usage.go) 增 `ExtractUsageSides`，把 `respnorm.usageBlockSides` 规则下沉过来；`ctxgraph/manifest.go` 和 `report/session.go` 的 `UsageOK` 换成 `UsageInOK`/`UsageOutOK`。
③ 短期兜底：[`ingest.go:146`](file:///Users/stanford/code/vmr/internal/report/ingest.go#L146) 和 [`recextract.go:174`](file:///Users/stanford/code/vmr/internal/report/recextract.go#L174) 各加 `&& a.ErrorClass != "content"`。
④ `quota_parity_test.go` fixture 扩为 `protocol × {正常, 截断, 4xx, softblock}` 四形状，消灭假绿。

---

## P-2-1 · 审计落盘的 HTML 转义破坏字节保真

**ROI：极高 | Domain：D2 服务端与审计 | 批次：一**

### 问题描述

vmr 作为代理，核心承诺之一是"审计日志里的请求体就是客户端原封不动发过来的字节"。但实际上，Go 标准库的 `json.NewEncoder` 默认会对 `<`、`>`、`&` 进行 HTML 转义——把 `<` 变成 `\u003c`，把 `>` 变成 `\u003e`。

对于一个主要处理代码相关负载（代码里到处是 `<`、`>`）的 LLM 代理来说，这意味着：**审计文件里存的请求体不是原始字节**。

具体位置：[`audit/audit.go:599`](file:///Users/stanford/code/vmr/internal/audit/audit.go#L599)：
```go
if err := json.NewEncoder(buf).Encode(rec); err != nil {
```
这一行没有调 `enc.SetEscapeHTML(false)`。

有意思的是，全仓另外两处已经知道这个问题并修复了：
- [`jsonscan/jsonscan.go:35-36`](file:///Users/stanford/code/vmr/internal/jsonscan/jsonscan.go#L35)：`MarshalNoEscape` 已关闭转义
- [`reqdetail/render.go:125-126`](file:///Users/stanford/code/vmr/internal/reqdetail/render.go#L125)：输出侧已关闭转义

**最该保真的存储侧反而漏了**，原因是消费者（JSON 解码器）对转义透明，所以没有任何功能报错。

### 负面影响

| 不修 | 修后 |
|------|------|
| `vmr replay` 重放时发送的 body 字节与 Content-Length 都与原始请求不同，"byte-for-byte 重现"的承诺在线缆层面不成立 | replay 真正字节保真 |
| 含 `<`/`&` 的 body（代码负载是主要流量）每字节膨胀到 6 字节，审计文件无故增大 | 审计体积正常 |
| 与仓库自己已确立的 `MarshalNoEscape` 标准不一致 | 全仓标准统一 |

### 根因

写这行时按惯用法取了 `json.Encoder`（为了它自带的尾部 `\n`，避免 `json.Marshal` 后再 `append('\n')` 的一次重分配），注释在 [`audit.go:595-598`](file:///Users/stanford/code/vmr/internal/audit/audit.go#L595) 明确写了这个选择，但没意识到默认 HTML 转义与"审计 = 原样记录"存在冲突。

### 建议方案

```go
enc := json.NewEncoder(buf)
enc.SetEscapeHTML(false)
if err := enc.Encode(rec); err != nil { ... }
```
`replay.writeReplayRecord`（[`replay.go:658`](file:///Users/stanford/code/vmr/internal/replay/replay.go#L658)）同理。补一条字节级测试：含 `<` 的 body 落盘后原样读回。历史文件无需迁移（消费者对转义透明）。

---

## P-1-1 · `respnorm.stream.opaque` 的数据竞争

**ROI：高 | Domain：D1 路由内核 | 批次：一**

### 问题描述

想象一个多人共用的黑板，规定写字之前必须先锁门。`respnorm.stream` 里大部分字段都遵守这个规定，由 `s.mu` 这把锁保护。但 `opaque` 字段是个例外——它有两处**无锁写入**。

决定性证据（[`respnorm.go:459`](file:///Users/stanford/code/vmr/internal/respnorm/respnorm.go#L459) 和 [`:474`](file:///Users/stanford/code/vmr/internal/respnorm/respnorm.go#L474)）：
```go
s.opaque = true           // ← 无锁写
s.noteApplied("overflow_raw_passthrough")  // ← noteApplied 内部会加锁
```
**写 `opaque` 的下一行就是加锁的 `noteApplied`**——说明作者完全知道这里需要同步，只是 `opaque` 这一个字段漏掉了。

而读取侧（[`respnorm.go:903`](file:///Users/stanford/code/vmr/internal/respnorm/respnorm.go#L903)）：
```go
func (s *stream) OutTokens() int64 {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.opaque {  // ← 持锁读
```

无锁写 + 持锁读 = 数据竞争。Go 的内存模型不保证这种情况下读到的值有意义，可能导致偶发的乱序行为。

### 负面影响

| 不修 | 修后 |
|------|------|
| 大响应（>8MB）触发溢出时，`opaque` 的写入与 `OutTokens()` 的读取存在竞争，Go race detector 可直接检出 | 无竞争，`OutTokens()` 的返回值可靠 |
| 可能导致偶发配额计算错误（`OutTokens` 被用于配额 charge） | 计费稳定 |
| CI 跑了 `-race` 但用例不覆盖"大响应 + 并发读"场景，竞争潜伏在生产环境 | 补用例后 CI 可检出 |

### 根因

`opaque` 是后加字段。加锁纪律写在 `mu` 的注释里（"守护 stream 的可变状态"），不在类型系统里——新字段不会被这条纪律自动覆盖，只能靠人工记住，而这次漏了。

### 建议方案

两处无锁写包进 `s.mu`，或将 `opaque` 与相邻由 `noteApplied` 守护的状态合并到一个内嵌 struct，让"漏一个字段"在类型层面变难。补 `-race` 用例：在溢出分支触发的同时并发调用 `OutTokens()`。

---

## S-2 · 新协议形状默认落进兜底分支（静默失效）

**ROI：高 | Domain：跨 D1/D4/D5/D6 | 批次：一（P-D4-1 立即修）+ 四（机制）**

### 问题描述

vmr 支持三种协议（OpenAI Chat、Anthropic Messages、OpenAI Responses）。每当新增一种协议或上游加了新的响应格式，代码里的 `switch`/`if-else` 枚举列表就多一项——但如果漏了，程序不会报错，而是悄悄走"默认分支"，用错误的方式处理数据。

最典型的例子是 `mergeUsage`（[`chatmsg/usage.go:154-168`](file:///Users/stanford/code/vmr/internal/chatmsg/usage.go#L154)）：
```go
for _, holder := range []any{obj["usage"], Nested(obj, "message", "usage")} {
```
这里只探测两个位置找 usage 数据。但 openai-responses 协议的流式响应里，usage 藏在 `obj["response"]["usage"]`——两个位置都找不到，结果 `UsageOK=false`，整笔流量永久拿不到精确计费。

最有力的证据：**同一个包的内容解析侧已经知道这个嵌套**——[`sse.go:149`](file:///Users/stanford/code/vmr/internal/chatmsg/sse.go#L149) 的 `case "response.completed"` 显式下钻 `obj["response"]`。内容侧知道，usage 侧不知道。

另一处是 `RenderPart`（[`chatmsg/messages.go:123`](file:///Users/stanford/code/vmr/internal/chatmsg/messages.go#L123)）：anthropic 的 `document` 类型（PDF 附件，数 MB base64）落进 `default: jsonIndent(m)`，整段进 detail 页的代码块和 token 计数，导致 detail 页巨大、user token 数虚高。而同函数里 OpenAI Responses 的 `input_file` 被正确占位处理了，anthropic 的没有。

还有两处属于同族：
- [`respnorm.go:456/472`](file:///Users/stanford/code/vmr/internal/respnorm/respnorm.go#L456)：>8MB 响应触发溢出时跳过 model 字段改写
- [`corpus_contextrot.go:71-73`](file:///Users/stanford/code/vmr/internal/story/corpus_contextrot.go#L71)：`UsageOK=false` 的步骤归入"0-32k"桶，影响 context rot 分析

### 负面影响

| 不修 | 修后 |
|------|------|
| openai-responses 流量（路由侧 `usagesniff.go` 走同一函数）永久拿不到精确计费，只有估算值 | `mergeUsage` 一行修复，精确计费立即生效 |
| anthropic PDF 附件请求的 detail 页体积异常，token 统计不准 | 正确占位，统计准确 |
| `c963db9`（加 openai-responses 支持，54 文件 2117 行）留下的 P-D4-1 至今未被发现，说明纯靠人工枚举无法保证完整性 | 协议覆盖测试（`protocol_coverage_test.go`）机械检出漏项 |

### 根因

两层：
1. 仓库的"不猜"教义（`chatmsg/sse.go:128-135` 的"无法识别就跳过"）被实现为**静默不处理**，而不是**显式记录遇到了未处理的形状**。"不猜"是对的，"不猜且不说"就成了静默失效。
2. **测试的输入形状词汇表和生产代码的枚举列表是同一份手工清单**，所以永远同时漏同一形态——`usage_protocol_test.go` 的 SSE 用例只有 anthropic 形状，`quota_parity_test.go` 的 protocol 硬编码 `openai-completions`。

### 建议方案

① **立即修**：`mergeUsage` 补第三个 holder `Nested(obj, "response", "usage")`（一行）；`RenderPart` 补 `case "document":` 比照已有的 `input_file`。
② **让沉默变响**：`chatmsg` 对未识别的 content part / usage holder 计数，在 `vmr analyze` 输出里露一行"本次语料 N 个未识别形状"——不需要预知会漏哪个形状。
③ **让词汇表从枚举来**：新增 `cmd/vmr/protocol_coverage_test.go`，由 `adapter.Names()`（[`adapter/adapter.go:92`](file:///Users/stanford/code/vmr/internal/adapter/adapter.go#L92)）驱动，遍历已注册协议 × 典型响应形状。

---

## P-2-2 · `chargeReplay` 违反 `TokenCounters` 调用契约

**ROI：高 | Domain：D2 服务端与审计 | 批次：二（随 S-3 一起做）**

### 问题描述

这个问题的荒诞之处在于：**违反者与被违反者相隔十行**。

`router/quota.go` 里的 `TokenCounters` 函数有一段明确的 doc comment（[`:186-189`](file:///Users/stanford/code/vmr/internal/router/quota.go#L186)）：
> "a caller that can only say 'some usage was seen' must use `TokenCountersSides` instead, because partial usage (real input, placeholder output) billed as exact is precisely the failure `TokenCountersSides` exists to prevent."

而 [`replay/replay.go:298`](file:///Users/stanford/code/vmr/internal/replay/replay.go#L298) 的调用：
```go
raw, estimated := router.TokenCounters(u, u.In > 0 || u.Out > 0,
    tokenutil.Estimate(reqBody), tokenutil.Estimate(respBody))
```
`u.In > 0 || u.Out > 0` 正是那个"only say 'some usage was seen'"的写法——doc comment 明确说这种情况必须用 `TokenCountersSides`。

结果：replay 重放 anthropic 截断流时，Out≈1 被当作精确计费写进持久账本 `vmr-quota.json`，且 `estimated=0`（看起来是精确值），**R46 在 live 侧已消灭的这个问题在 replay 侧复活了**。

第二处分叉：In 侧的基不同。Live 用剔除了 base64 的 `creq.Facts.EstimatedTokens`（[`server/facts.go:81-85`](file:///Users/stanford/code/vmr/internal/server/facts.go#L81)），replay 用全量 `tokenutil.Estimate(reqBody)`——含内联图的记录相差约两个数量级，但都写进同一个 `vmr-quota.json`。

### 负面影响

| 不修 | 修后 |
|------|------|
| replay 重放含截断流的记录时，`vmr-quota.json` 被写入错误数字，且无法自愈（持久账本） | 正确用 `TokenCountersSides`，精确/估算分侧 |
| 含内联图的 replay 记录，In 计费比 live 高两个数量级，污染配额统计 | In 侧改用审计记录里的 `facts.EstimatedTokens` |
| `vmr-quota.json` 的 `estimated_pct` 被毒化，操作员看到的配额健康度不可信 | 配额健康度准确 |

### 根因

replay 拿到的是完整字节而不是流对象，无法像 live 那样用 `respnorm.stream.UsageSides()` 增量维护分侧感知，于是退化成单 bit——而 `recordView`（[`replay.go:70-81`](file:///Users/stanford/code/vmr/internal/replay/replay.go#L70)）没有解码审计记录里现成的 `facts.EstimatedTokens` 字段。

### 建议方案

① S-3 的 `chatmsg.ExtractUsageSides` 落地后，`chargeReplay` 改调 `router.TokenCountersSides`。
② `recordView` 增加解码 `facts`，`inEst` 优先取 `facts.EstimatedTokens`，缺失时才回退 `tokenutil.Estimate`。
③ `quota_parity_test.go` 覆盖从 report↔router 扩到 replay↔router（补齐第三个角）。

---

## S-1 · 注释里声称的调用关系没有机器守卫

**ROI：高（元级） | Domain：跨域（D1/D2/D4/D7） | 批次：三**

### 问题描述

这是一类特殊的问题：**注释说"A 调用了 B"，但实际上 A 调用的是 C，B 成了死代码**。

vmr 的 archtest 会检查注释里的 `` `pkg.Symbol` `` 引用是否"符号存在"——但它不检查"这个符号是否真的被那个声称调用它的地方调用"。所以以下五处断言全部为假，且全部通过 CI：

| 位置 | 注释声称 | 实际情况 |
|------|---------|---------|
| [`pricing/pricing.go:280-286`](file:///Users/stanford/code/vmr/internal/pricing/pricing.go#L280) | `gen_standard_pricing` 调 `IsAggregatorVendor` | 它调 `Ambiguities()`（[`main.go:151`](file:///Users/stanford/code/vmr/tools/gen_standard_pricing/main.go#L151)） |
| `docs/VirtualModelRouter_Design_v4_Quota.md:209` | 同上 | 同上 |
| [`chatmsg/usage.go:92`](file:///Users/stanford/code/vmr/internal/chatmsg/usage.go#L92) | `respnorm` 需要 `MergeUsageBytes` | 它调 `MergeUsageWithProtocol` |
| [`replay/replay.go:285`](file:///Users/stanford/code/vmr/internal/replay/replay.go#L285) | `MergeUsageBytes` 是 respnorm 内部所调 | 两边都调 `MergeUsageWithProtocol` |
| [`router/quota.go:90`](file:///Users/stanford/code/vmr/internal/router/quota.go#L90) | replay 经 `MergeUsageBytes` 取 usage | 同上 |

连带产生三处死代码：
- **`pricing.IsAggregatorVendor`**（[`pricing.go:287`](file:///Users/stanford/code/vmr/internal/pricing/pricing.go#L287)）：全仓零引用
- **`logtee.Recent` / `logtee.Subscribe`**（[`logtee.go:123`](file:///Users/stanford/code/vmr/internal/logtee/logtee.go#L123)/[`:141`](file:///Users/stanford/code/vmr/internal/logtee/logtee.go#L141)）：生产零调用，`/log` 走的是 `Follow()`
- **`chatmsg.MergeUsageBytes`**（[`usage.go:104`](file:///Users/stanford/code/vmr/internal/chatmsg/usage.go#L104)）：全仓零引用

**全仓仅有的三处死代码，每一处都带着一段声称"我为某个具名调用者存在"的注释。**

还有第二种形态（行为等价断言）：[`reportconfig.go:82-89`](file:///Users/stanford/code/vmr/cmd/vmr/reportconfig.go#L82) 的注释说与 `config.go` 的注入守卫"duplicated"、"Same fail-fast rule applies"，但 `config.go:373` 有**四项**守卫（含 `HasPrefix(TrimSpace(v), "#")`），[`reportconfig.go:97`](file:///Users/stanford/code/vmr/cmd/vmr/reportconfig.go#L97) 只有**三项**。`report.yaml` 携带 `llm_key`，缺少的那条会导致以 `#` 开头的 secret 值被 YAML 截断，引发神秘 401。

第三种形态：`docs/KNOWN_ISSUES.md:113` 声称 i18n 文件与 `section_*.go` 的一一配对由"archtest 强制"，而 archtest 五个文件里根本没有这项检查。

### 负面影响

| 不修 | 修后 |
|------|------|
| 注释越权威，腐化的代价越大——D3 sub-agent 被明确要求"先读代码再下结论"，仍然把 `IsAggregatorVendor` 判为活代码 | `doc_refs_test.go` 扩展后，零引用符号自动报错 |
| "KNOWN_ISSUES 声称的护栏不存在"这个错觉，比缺护栏本身更危险——让下一个人放弃手工检查 | 护栏要么真存在，要么文档撤销声称 |
| `report.yaml` 里以 `#` 开头的 secret 被 YAML 截断，config 会 fail-fast 但 reportconfig 不会 | 两侧守卫完全对齐 |

### 根因

可复现机制：*为具名调用者 X 导出符号 S → X 改用更精确的变体 S′ → S 失去调用者但因为导出而无人报错 → 注释从"记录真实约束"变成"虚构约束"*。符号存在性检查（archtest）够不到调用关系检查。

### 建议方案

① **删死代码**：删除 `pricing.IsAggregatorVendor`、`logtee.Recent`/`Subscribe`、`chatmsg.MergeUsageBytes` 及其虚构注释；修正 `docs/VirtualModelRouter_Design_v4_Quota.md:209` 的符号名。
② **扩展 `doc_refs_test.go`**：反引号 `` `pkg.Symbol` `` 若在全仓非注释代码中零引用即为错误，配 `fileLineExemptions` 豁免表（精度实测恰好命中 2 处真问题）。
③ **补 i18n↔section 配对检查**（`i18n/report_*.go` ↔ `internal/report/section_*.go` 目录列表比对，十余行），或删掉 KNOWN_ISSUES 里那句"archtest 强制"——二选一，不能留着。
④ `expandReportEnv` 补第四条守卫（`HasPrefix(TrimSpace(v), "#")`），或把三/四条守卫抽成叶层小函数共用，补表驱动等价测试。

---

## P-1-2 · 上游连接全线无 HTTP/2

**ROI：中 | Domain：D1 路由内核 | 批次：一**

### 问题描述

vmr 到所有 LLM 供应商的连接，实际上全部使用 HTTP/1.1，而不是 HTTP/2。

原因藏在 Go 标准库的一个非显然行为里：`http.Transport` 只有在**没有**自定义 `Dial`/`DialContext`/`TLSClientConfig` 时才自动开启 HTTP/2；只要设了任何一个，就禁用自动升级，除非显式设置 `ForceAttemptHTTP2 = true`。

[`router/transport.go:47`](file:///Users/stanford/code/vmr/internal/router/transport.go#L47) 设置了自定义 `DialContext`（为连接超时控制），触发了这条规则。全仓 `grep ForceAttemptHTTP2` 零结果。

这不是有意为之的决定，也不是已知项——**没有任何注释、KNOWN_ISSUES 条目或测试记录"我们是 HTTP/1.1"这个事实**。它是一个无人知晓的现状。

### 负面影响

| 不修 | 修后 |
|------|------|
| 长流式响应无法用 HTTP/2 多路复用，每个请求占一条 TCP 连接 | 支持多路复用，降低连接开销 |
| 丢失 HTTP/2 头压缩（HPACK），重复请求头的网络开销增加 | 头压缩生效 |
| 更重要：这是一个**隐性现状**，下一个改 transport 的人不会知道他的改动影响了 H2 行为 | 变成显式决定（开启 H2 或在 KNOWN_ISSUES 记录"刻意 H1.1 及原因"） |

### 根因

自定义 `DialContext` 对 H2 的副作用是 stdlib 的非显然行为，不看文档不会知道。没有任何机制记录"这是 HTTP 几"这一基础事实。

### 建议方案

`transport.go` 的 `http.Transport` 里加一行：
```go
ForceAttemptHTTP2: true,
```
然后用 `vmr diagnose`（已有真实请求路径可复用）实测确认协商结果。若刻意要 HTTP/1.1，**必须写进 `KNOWN_ISSUES` 并在 `transport.go` 注明理由**——按仓库自己的规矩，只写在源码注释里的取舍不算被追踪。

---

## P-3-2 + P-7-3 · `${ENV}` 展开两条防线不等价，但注释声称等价

**ROI：中-高 | Domain：D3 配置 + D7 CLI | 批次：三**

### 问题描述

vmr 的配置文件（`config.yaml`）支持 `${ENV_VAR}` 语法注入环境变量。为了防止注入的值破坏 YAML 结构（比如值里含换行、含 YAML 注释符号 `#`），代码里有 fail-fast 守卫。

这套守卫有两份实现：
- `config.expandEnv`（[`config/config.go:373`](file:///Users/stanford/code/vmr/internal/config/config.go#L373)）：**四**项守卫，含 `strings.HasPrefix(strings.TrimSpace(v), "#")`
- `reportconfig.expandReportEnv`（[`reportconfig.go:97`](file:///Users/stanford/code/vmr/cmd/vmr/reportconfig.go#L97)）：**三**项守卫，缺少 `#` 那条

而 [`reportconfig.go:82-89`](file:///Users/stanford/code/vmr/cmd/vmr/reportconfig.go#L82) 的注释白纸黑字写着："The injection guards are duplicated with it … Same fail-fast rule config.yaml applies"。**注释声称等价，实际不等价。**

`report.yaml` 携带 `llm_key`——若 secret 的值以 `#` 开头（如 `#my-secret-key`），`config.yaml` 会 fail-fast 并报错，但 `report.yaml` 会静默接受，YAML 解析后截断为空字符串，API 调用返回神秘 401。

另一个独立问题：两者都不排除注释行，所以 YAML 注释里的 `${...}`（`config.example.yaml` 有 9 处）也会被当作真实注入点触发"未设置变量"警告，**把整个 `EmptyEnvRefs` 提示训练成噪音**。

### 负面影响

| 不修 | 修后 |
|------|------|
| 以 `#` 开头的 secret 在 `report.yaml` 里静默截断，只引发神秘 401，与 `config.yaml` 的 fail-fast 行为不一致 | 两侧完全对齐，相同的值产生相同的行为 |
| YAML 注释里的 `${...}` 触发"未设置变量"警告，让操作员忽略真正的警告 | 跳过注释行，警告信噪比恢复 |
| 这是 S-1 形态二（行为等价断言）的**唯一已知实例**，不修就没有示范修复路径 | 为 S-1 形态二建立修复范式 |

### 根因

刻意重复的代码（`cmd/vmr` 不依赖 `internal/config` 是有理由的分层选择），但重复被注释宣布为等价，而不是被测试钉住等价。后来 `config.expandEnv` 加了第四条守卫，只改了原件。

### 建议方案

① `expandReportEnv` 补第四条守卫 `HasPrefix(TrimSpace(v), "#")`；或把三/四条纯字符串守卫抽成一个不引入 `internal/config` 依赖的叶层小函数，两边共用。
② 补一条同时驱动 `expandEnv` 与 `expandReportEnv` 的表驱动测试——防住第五条守卫再次只加一边。
③ 顺手让两者跳过 `#` 起始的整行，消除注释里 `${...}` 的误报。

---

## P-5-1 · `tok_out_per_sec` 同名不同基

**ROI：中 | Domain：D5 report | 批次：三/四**

### 问题描述

报表里有一列叫 `tok_out_per_sec`（output token 生成速度），但同一份报表里，这个名字的列在两张不同的表里计算方式不同：

- **行级表**（[`report/metrics.go:139-141`](file:///Users/stanford/code/vmr/internal/report/metrics.go#L139)）：分母是 `tokDurMS`——只累加**有精确 usage 记录**的请求时长
- **端点级表**（[`report/metrics.go:175-177`](file:///Users/stanford/code/vmr/internal/report/metrics.go#L175)）：分母是 `DurMSSum`——累加**所有**请求时长（含无 usage 的降级估算）

两张表用同一列名，但统计口径不同。当 usage 覆盖率低于 100% 时（如有大量 anthropic 截断流），端点级的数字会被系统性低估，而读者会理所当然地横向对比这两列，根本没有任何提示说它们的基不同。

好比餐厅公布"平均出餐速度"，翻台数据按"仅统计有计时记录的桌"，但店面汇总数据按"所有桌（含没计时的）"，两个数字一起出现在同一张报表里，没有任何注释。

### 负面影响

| 不修 | 修后 |
|------|------|
| 操作员看到两个"tok_out_per_sec"数字不一致，却无从判断哪个更准，或者误以为是 bug | 改名后两个列名自解释，各自的统计逻辑一目了然 |
| 在有降级估算流量的账号上，端点级 TPS 被系统性低估，可能误导容量规划 | 统计口径透明，可信度有据 |

### 根因

两个聚合器各自演化，分母的选择在各自上下文里都自洽（行级看"可比样本"，端点级看"总吞吐"），但**指标名没有跟着基一起分化**。CLAUDE.md 的 SSOT 教义只管跨半区，没有管半区内部同名列的命名纪律。

### 建议方案

最小改动：**改名而非改算法**。端点级列改为 `tok_out_per_sec_wall`（按墙钟时间），行级保持 `tok_out_per_sec`（按采样时间）；并在 i18n 文案里说清分母定义。若要统一口径，建议统一到"有 usage 的样本"一侧，因为无 usage 记录的 token 数是估计值，混进吞吐率会让精确与估计混算。

---

## P-5-2 · §2.5 配额窗口对 provider 名失配静默漏计

**ROI：中-高 | Domain：D5 report | 批次：四**

### 问题描述

`vmr analyze` 会生成 §2.5 表，用来核对实际消耗与配额限制的差距——这是操作员日常核对配额的主表。

但当审计日志里的 provider 名与配置文件里的 provider 名不一致时（比如配置改名了、或者审计日志跨越了改名的时间点），[`report/providerquota.go:203-206`](file:///Users/stanford/code/vmr/internal/report/providerquota.go#L203) 的处理是：
```go
refs, ok := quotas[provider]
if !ok {
    continue  // 静默跳过
}
```
直接跳过，不计数、不标记、不提示。整个 §2.5 表里没有任何迹象表明有记录被丢弃。

讽刺的是，同一个函数的注释里写着「missing data is not a zero」——这条原则在 cost 度量上做了 `costUnpricedReqs` 披露，但 tokens/requests 度量违反了它。**同一条原则在同一个包里既被写下又被违反。**

### 负面影响

| 不修 | 修后 |
|------|------|
| §2.5 少算请求数，但表里看不出来，操作员以为数字完整 | 输出一行"N attempts skipped (unknown provider: X)"，操作员知道有丢失 |
| 配置改名后，配额核对的数字会悄悄下降，不知原因 | 原因清晰可查 |

### 根因

未知输入落进静默分支，与 S-2 同族。违反了仓库自己写下的"missing data is not a zero"原则，但 tokens/requests 度量没有 cost 度量那样的披露机制。

### 建议方案

累计跳过数，在 §2.5 表下渲染一行 `"N attempts skipped (unknown provider: …)"`。不需要修复归因逻辑，**只需让沉默变响**——与 S-2 的建议①是同一件事，可以一起做，约十行代码。

---

## P-D4-3 · `ctxgraph` stitch 赢家遮蔽

**ROI：中 | Domain：D4 分析底座 | 批次：五**

### 问题描述

`ctxgraph` 负责把不同会话拼接成连贯的对话线路（stitch）。它的工作原理是：在候选前驱会话里，按分数（内容重叠度）选出最佳匹配者。

问题在于：[`ctxgraph/stitch.go:344-367`](file:///Users/stanford/code/vmr/internal/ctxgraph/stitch.go#L344) 的候选排序按 `score → gap → idx` 三级，其中 `overGap`（是否超过 72 小时间隔阈值）**参与了计算但不参与选择**：
1. 赢家按分数选出
2. 再在 `:395-399` 判断：若赢家超 72h，降级为 `AmbiguousMatch`

结果：一个分数稍高但超 72h 的候选会先赢下选择，再被降级，**遮蔽了一个分数稍低但在 72h 内的合法前驱**——后者从来没有机会被考虑。

类比：招聘时先选分数最高的候选人，再发现他不满足资格要求，但已经不再考虑第二名了。正确做法应该是先过滤掉不满足资格的人，再在剩下的人里选最高分。

对照 `strategy` 包的设计原则：CLAUDE.md 把"淘汰（`Condition`）与排序（`Dimension`）必须分开"列为不可破的不变量。`ctxgraph` 里把淘汰塞进了排序之后，正是那条不变量禁止的形状——只是它不在 `strategy` 包里，所以没有护栏拦住。

### 负面影响

| 不修 | 修后 |
|------|------|
| 跨 lineage 缝合偶发把两段本不相干的会话接在一起（超 72h 的候选赢了窗内候选），story 叙事出现错误跳接 | 缝合结果稳定，lineage 纯净 |
| journey 统计、§6.x 会话统计受到污染 | 统计基础可靠 |
| 错误率低，难以发现（只在有多个候选且超 72h 候选分数恰好更高时触发） | 修复后附带确立"淘汰优先于排序"在全仓的通用形状 |

### 根因

`overGap` 是后加的约束，加时选择了"事后降级"而非"事前淘汰"——忽略了 `strategy` 包里已有的正确范式。

### 建议方案

把 gap 约束**前移为候选过滤**：超阈值者不进入排序，保留降级标记用于"过滤后无候选"的诊断输出。改动集中在 [`stitch.go:344-399`](file:///Users/stanford/code/vmr/internal/ctxgraph/stitch.go#L344) 一段，约 15 行。

---

## P-D6-8 · `extractRootUserIntent` 绕过 dialect 过滤

**ROI：中 | Domain：D6 story | 批次：五**

### 问题描述

`vmr story` 生成 journey 叙事时，会把用户的"根意图"作为首句话展示给操作员——这是读者最先看到的内容。

[`story/llm_single.go:63-74`](file:///Users/stanford/code/vmr/internal/story/llm_single.go#L63) 的实现是直接取 Events 里第一条 `role=user` 的消息：
```go
func extractRootUserIntent(j *Journey) string {
    for _, t := range j.Tasks {
        for _, s := range t.Steps {
            for _, ev := range s.NewEvents {
                if ev.Msg.Role == "user" && ev.Msg.Text != "" {
                    return ev.Msg.Text  // ← 第一条 user 消息
```

问题是 OpenClaw（Claude Code）这类 agent 客户端会把系统脚手架（heartbeat、环境信息等）伪装成 `user` 角色注入。`extractRootUserIntent` 抓到的不是用户的真实意图，而是这段脚手架。

而**同包里已有正确做法**：`j.InitialInstruction`（[`story/journey.go:122-126`](file:///Users/stanford/code/vmr/internal/story/journey.go#L122)），它的注释恰好论证了为什么不能直接拿 Events 首条 user（scaffold/heartbeat 会以 user 角色注入）。正确答案就在旁边，新函数没用它。

### 负面影响

| 不修 | 修后 |
|------|------|
| OpenClaw 流量的 journey 首句话是脚手架文本而非用户意图，操作员看到的是无意义的系统消息 | `j.InitialInstruction` 经过 dialect 过滤，首句话准确反映用户意图 |
| 这是操作员最先读到的一句话，错误影响整个 journey 阅读体验 | story 叙事可信度提升 |

### 根因

正确做法在同一个包里、还带着解释为什么的注释，新代码没用它。收敛的成果（`InitialInstruction`）没有变成唯一入口，只是变成了一个更好的选项——这是本次审查反复出现的形态。

### 建议方案

`extractRootUserIntent` 改调 `j.InitialInstruction`；若语义确有差异，把差异写进注释并让两者共享同一个 dialect 过滤前置步骤。同族的 P-7-7（`vmr analyze` 对 `config.yaml` 做两次独立 `Load`）和 P-D6-5（journey 展示层是 `metricSpecs` 清单的第三份手抄）一起做，约定"收敛后的入口必须是唯一入口"。

---

## P-7-1 · `vmr.sh` 的 `-c` 注入清单漏掉 `analyze`

**ROI：中 | Domain：D7 CLI | 批次：一**

### 问题描述

`vmr.sh` 会自动给某些子命令注入 `-c config.yaml` 参数，省去用户手动指定的麻烦。这个白名单在 [`vmr.sh:603`](file:///Users/stanford/code/vmr/vmr.sh#L603)：
```
start|check|status|diagnose|smoke|replay|report|story)
```

**`analyze` 不在其中**。而 `analyze` 正是 CLAUDE.md 宣告的统一分析入口，`report` 和 `story` 是它的弃用别名——两个弃用别名都在白名单里，正式入口反而不在。

结果：`vmr.sh analyze` 不会自动注入 `-c`，行为与 `vmr.sh report`（弃用别名）不一致，用户迁移时会遇到困惑。

### 负面影响

| 不修 | 修后 |
|------|------|
| 迁移期间用 `vmr.sh analyze` 需要手动加 `-c`，与弃用别名行为不一致，增加迁移摩擦 | `analyze` 与其别名行为完全一致 |
| 新用户按文档用 `analyze`，发现需要额外参数，而用旧命令不需要——助长继续用弃用别名 | 正式入口体验优于弃用别名，引导自然迁移 |

### 根因

引入 `analyze` 作为新主入口的那次改动，没有回头扫描"所有按子命令名枚举的地方"。又一处手工清单（S-2 的形态），且在 shell 脚本里，`go test`、archtest、`go vet` 全都看不见它。

### 建议方案

`vmr.sh:603` 加上 `analyze`（一个词的修复）。更根本地：让 `vmr.sh` 从 `vmr help` 的输出派生子命令清单，或在 CI 的 shellcheck 步骤旁加一条断言——`vmr` 的子命令集合 ⊆ 脚本白名单。

---

## i18n 纪律三连（P-7-2 / P-7-5 / P-5-5）

**ROI：中-高 | Domain：D5/D7 | 批次：三**

### 问题描述

三条同根，共同点是：**文档声称有护栏，实际没有**。

**① P-7-2**：`docs/KNOWN_ISSUES.md:113` 写道 i18n 微文件与 `internal/report/section_*.go` 的一一配对由"archtest 强制"。核实 archtest 的全部五个文件：只有 import 边界、文件行数、函数行数、文档引用四类检查，**没有任何一处校验这个配对**。今天新建一个 section 不配 i18n 文件，CI 全绿，且操作员读到"archtest 强制"后不会再手工检查。

**② P-7-5**：EN/ZH 空翻译绊线只覆盖 17 个手工注册的 bundle，实际约 24 个——扩展包时如果漏了注册，绊线形同虚设。

**③ P-5-5**：[`report/pricing.go:44-50`](file:///Users/stanford/code/vmr/internal/report/pricing.go#L44) 的币种降级文案硬编码在生产代码里，是该包唯一一处 i18n 文本泄漏，ZH 用户看到英文。

当前实测无缺翻、无错配——问题不是现行 bug，是**三处护栏缺口被文档描述成护栏存在**，让下一个人放弃了手工核查。

### 负面影响

| 不修 | 修后 |
|------|------|
| "archtest 强制"的错觉让新 section 缺 i18n 文件时无任何提醒，中文用户看到英文 | 配对检查真正存在，CI 兜底 |
| 手工注册的 bundle 列表随功能扩展必然不同步 | 遍历全部导出 bundle，注册与检查统一 |
| 唯一一处硬编码中文文案泄漏，与仓库 i18n 规范不一致 | 归入 i18n 包统一管理 |

### 根因

i18n 的"一文件对一 section"是纯约定型纪律：有明确规则、有文档背书、却没有任何可执行检查。KNOWN_ISSUES 那句"archtest 强制"让所有后续读者相信它已被守住，是 S-1 危害的教科书案例。

### 建议方案

① 补 `i18n/report_*.go` ↔ `internal/report/section_*.go` 目录列表比对检查（约十余行），或**删掉** KNOWN_ISSUES 里那句断言——二选一，绝不能留着。
② 空翻译绊线改为遍历 `i18n` 包的全部导出 bundle，而非手工注册表。
③ `pricing.go:44-50` 的文案移进 `i18n` 包。三条合计一天以内。

---

## P-7-9 + §4.2 · 行数豁免表从绊线退化为登记簿

**ROI：中 | Domain：D7 / 架构 | 批次：五**

### 问题描述

archtest 的行数预算（默认文件 700 行/函数 120 行）原本是"绊线"——碰到就必须拆分。但现在这个机制正在退化为"登记簿"，有两个证据：

**证据一**：全仓有九个文件停在 646–695 行，全部紧贴 700 的默认线下方：`story/compare.go`(695)、`story/llm.go`(680)、`replay/replay.go`(677)、`router/router.go`(669)……这不是巧合，是激励结构的直接产物——开发者的理性反应就是停在线下一点点。

**证据二**：`cmd_story.go` 豁免 850 行、实际 832 行，余量 2%，是现状续期而不是"拆了一次"后的防回弹线。`file_sizes_test.go:75-77` 自己写着"CLI 层过线意味着逻辑该进 internal 包"，但 `cmd_story.go` 里的 `renderJourneys`（分批/过滤/写文件编排，含领域知识）正符合这个描述，没有下沉。

机制还在起作用（没有文件超线），但它已经**从"强制拆分"变成"给懒惰行为上了一个上限"**。

### 负面影响

| 不修 | 修后 |
|------|------|
| 九个文件同时在线下潜伏，下一次任何一个包的自然生长都会同时撞线，引发批量重构压力 | 逼近告警提前暴露，避免积压 |
| 豁免表成为惰性续期的工具，失去"拆了一次"的语义 | 批准日期 + 自然到期机制恢复豁免表的本意 |
| `cmd_story.go` 的领域知识留在 CLI 层，阻碍测试和复用 | 下沉后可独立测试 |

### 根因

只有"过线"一种反馈，没有"逼近"的反馈。开发者的理性反应就是停在线下一点点，或申请豁免。九个文件同时停在 646–695 是激励结构的直接产物，不是工程师的纪律问题。

### 建议方案

① 豁免表每项加"批准日期 + 拆分计划"字段，超过 N 次发版未变小即失效（自然到期）。
② 加"逼近告警"：≥90% 阈值时 `go test` 打印 warning 但不失败——把沉默的逼近变可见。
③ `cmd_story.go` 按 CLI 层的自述纪律真正下沉部分逻辑到 `internal/story`。
**注意：不能改成"提高数字"——`file_sizes_test.go` 的失败信息自己就禁止了这一点。**

---

## P-7-10 · `tokenutil` 回归系数无来源、无再校准路径

**ROI：中 | Domain：D7 / 架构 | 批次：五**

### 问题描述

`internal/tokenutil` 里有六个回归系数，用于从字符数估算 token 数。这六个数字是**路由侧配速（quota pacing）与全部报表 token 估算的公共基数**——影响面和 `internal/pricing` 的费率常量同等量级。

但两者的治理天壤之别：
- `internal/pricing`：有 `tools/gen_standard_pricing` 生成工具，有表龄提醒机制，有来源标注
- `tokenutil`：**六个裸数字，没有来源标注，没有生成工具，没有表龄提醒，没有再校准脚本**

没有人知道这六个数字从什么语料、什么时间点回归得来，也没有人知道怎么更新它们。

### 负面影响

| 不修 | 修后 |
|------|------|
| 没有人敢改六个不知来源的系数，随着模型演进（tokenizer 变化）估算精度可能悄悄下降 | 来源文档化后，再校准有路可循 |
| `estimated_pct` 吸收了误差，表面上没问题——但操作员无法量化当前误差到底多大 | 反算脚本直接量化当前估算精度 |

### 根因

pricing 的数字会被用户直接看到并质疑（钱），所以治理压力自然到位；tokenutil 的数字只体现为"估算值"，误差被 `estimated_pct` 吸收，没有人会来质疑它。治理强度跟着可见度走，而不是跟着影响面走。

### 建议方案

在 `tokenutil` 包注释里写清：系数从什么语料、什么 tokenizer、什么时间点回归得到，以及"重新校准需要跑什么"。若来源已不可考，如实写"来源已不可考"——这本身就是有价值的信息，告诉下一个人不要假装这些数字有依据。进一步可加一个 `tools/` 下的校准脚本，用审计日志里已有的真实 usage 反算当前误差分布（数据现成，`estimated_pct` 就是它的产物）。

---

## P-5-4 · `tagSummary` 用 `TokensIn > 0` 代理 `usageOK`

**ROI：中 | Domain：D5 report | 批次：随 Agent C 一起做**

### 问题描述

`vmr-requests.md` 的每个用户/标签组，顶部有一行摘要，显示该组的请求数、token 消耗和缓存效率。这行摘要由 [`report/requests.go:213`](file:///Users/stanford/code/vmr/internal/report/requests.go#L213) 的 `tagSummary` 计算。

`tagSummary` 判断一条记录是否"有精确 usage 数据"的方式是：

```go
if r.TokensIn > 0 {    // ← 用 TokensIn 是否大于 0 代理 usageOK
    s.fresh += r.TokensInFresh
    s.cached += r.TokensInCached
    s.out += r.TokensOut
    s.tokensKnown++
}
```

问题在于：[`RequestRow`](file:///Users/stanford/code/vmr/internal/report/rows.go#L527) 这个结构体只带了 `TokensIn`（数值），**没有携带 `usageOK` 布尔标志**。所以 `tagSummary` 只能拿 `TokensIn > 0` 作为代理判断。

这有什么问题？当一条记录的 usage 是"降级估算"（`usageOK=false`）但估算出的 `EstTokensIn > 0` 时，实际上 `TokensIn` 字段也会被填入这个估算值（[`rows.go`](file:///Users/stanford/code/vmr/internal/report/rows.go) 的聚合逻辑里，估算 token 被合并进了 `TokensIn`）。结果：`tagSummary` 把估算值当精确值统计，`tokensKnown` 被虚高，缓存效率计算也混入了不可信数据——而操作员看到的摘要行里没有任何提示。

### 负面影响

| 不修 | 修后 |
|------|------|
| 摘要行的 `tokensKnown` 计数虚高，混入了估算记录，让操作员高估精确 usage 的覆盖率 | 只统计真正 `usageOK=true` 的记录 |
| 缓存效率数字混入估算流量，对含大量 anthropic 截断流的账号可能系统性偏低 | 缓存效率仅基于可信数据 |

### 根因

`RequestRow` 这个跨层的 DTO 在设计时没有携带 `usageOK` 标志——token 数值被"压扁"了，丢失了"精确 vs 估算"这一维度。这是 S-3 的同族问题：`UsageOK` 的单布尔在跨层传递时丢失了分侧信息。

### 建议方案

最简单的修法：`RequestRow` 增加 `UsageOK bool` 字段，在 [`recextract.go`](file:///Users/stanford/code/vmr/internal/report/recextract.go) 构建 `RequestRow` 时填入 `ri.UsageOK`；`tagSummary` 改读这个字段。长期随 Agent C 的 `UsageInOK`/`UsageOutOK` 拆分一起统一处理。

---

## P-D6-4 · `corpus_contextrot` 把 `UsageOK=false` 的步骤归入"0-32k"桶

**ROI：中 | Domain：D6 story | 批次：随 Agent C 一起做**

### 问题描述

`vmr analyze` 会生成"Context Rot"分析——研究随着 context window 增大，错误率如何变化。它把每个 step 按 input token 数分桶（如 0-32k、32k-64k、64k-128k……），统计各桶内的错误比例。

[`story/corpus_contextrot.go:70-74`](file:///Users/stanford/code/vmr/internal/story/corpus_contextrot.go#L70)：

```go
var tokens int64
if s.Manifest != nil {
    tokens = s.Manifest.Usage.In  // ← 如果 UsageOK=false，Usage.In 为 0 或占位值
}
bIdx := bucketIndexForTokens(tokens)  // 0 → 归入最小桶 "0-32k"
```

当一个 step 的 usage 未成功嗅到（`UsageOK=false`，比如 openai-responses 流量、或 anthropic 截断流），`Usage.In` 是 0 或占位值，`bucketIndexForTokens(0)` 把这条记录塞进"0-32k"桶。

结果是：**所有没有精确 usage 的 step 全堆进了最小桶**，人为抬高了"0-32k"桶的 step 数和错误计数，让这个桶的错误率失真——而真正处于 0-32k context 的 step 淹没在噪音里，操作员看到的 Context Rot 曲线在最左侧被污染。

### 负面影响

| 不修 | 修后 |
|------|------|
| "0-32k"桶混入大量无 usage 的噪音记录，错误率虚高，Context Rot 图的最左端不可信 | 无精确 usage 的 step 进入专门的"unknown"桶，或直接排除在桶统计之外 |
| 随着 openai-responses 流量增加（P-D4-1 修复之前），噪音比例会持续上升 | P-D4-1 修复后 + 此处修复，桶分布准确 |

### 根因

S-2 的同族：`UsageOK=false` 时 `Usage.In=0`，但代码没有区分"真的是 0 input token"和"usage 未知"，两种情况全部落进最小桶。这是"不猜且不说"教义缺失的另一个实例——没有专门的"未知"桶来承接这类情况。

### 建议方案

在 `bucketIndexForTokens` 之前判断 `s.Manifest == nil || !s.Manifest.UsageOK`（S-3 修复后改读 `UsageInOK`），命中则跳过或归入专门的"usage unknown"伪桶，在 Context Rot 表下方注明"N steps excluded: no usage data"——与 S-2 ①（让沉默变响）保持同一风格。

---

## P-5-3 · `buildRec2` 的 (path, line) join 无时间戳交叉校验

**ROI：低-中 | Domain：D5 report | 批次：五（随 Agent G 补文档）**

### 问题描述

`report` 包为了避免每次重新解析全量审计日志，使用了一个 parse cache（`.parse-cache/` 目录）。缓存的 key 是审计文件内容的 SHA-256 hash——如果文件内容没变，就复用缓存结果。

[`report/recextract.go:95`](file:///Users/stanford/code/vmr/internal/report/recextract.go#L95) 的 `buildRec2` 把 parse cache 的结果（`recordFacts`）与 session 分析结果（`ReqInfo`）按 **(path, line)** 配对——即"这个文件的第 N 行"。

潜在问题：如果同一个 `path` 下的文件内容发生了变化（比如手动追加了日志行，或 zstd 压缩轮转导致 line 偏移），缓存里存的 `(path, line)` 对可能对应到**不同的记录**。两侧的时间戳（`rf.TS` 和 `ri.TS`）是可以交叉核对的，但 `buildRec2` 没有做这个校验。

不过如评审报告所述：**审计日志是追加型的**，正常运行下同一个文件的既有行不会被修改；zstd 压缩会生成新 path（`.jsonl.zst`），hash 会变，cache 自然 miss，触发重解析。所以这个问题**只在非常规操作（手动编辑、拼接日志文件）下才会触发**。

### 负面影响

| 不修 | 修后 |
|------|------|
| 非常规操作下（手动编辑审计文件），(path, line) join 可能配错记录，产生静默的数据错乱 | 增加 TS 交叉校验后，错配立即被发现并跳过 |
| 当前无任何告警，错配完全静默 | 错配时记录警告，可追查 |

### 根因

审计日志的"追加不变性"是一个运维层面的约定，没有在代码层面被显式依赖或验证。(path, line) join 的隐式前提是"这个文件从未被修改过"，但没有代码保证这一点。

### 建议方案

`buildRec2` 在 join `ReqInfo` 之前，若 `rf.TS` 和 `ri.TS` 差距超过合理阈值（比如 1 秒），记录 warning 并跳过 join（退化为仅使用 `rf` 侧数据）。这是一条加固项，**当前不修也可以**；更重要的是写进 `KNOWN_ISSUES`，说明"(path, line) join 依赖日志追加不变性，非常规操作下可能错配"。

---

## P-D4-6 · `.parse-cache` 分片的文件名与内容 key 不一致

**ROI：低 | Domain：D4 分析底座 | 批次：五（随 Agent G 补文档）**

### 问题描述

`.parse-cache/` 目录里每个缓存分片的文件名是 `<hash>.json`，其中 `hash` 是对应审计文件内容的 SHA-256。但 [`ctxgraph/cache.go:226-233`](file:///Users/stanford/code/vmr/internal/ctxgraph/cache.go#L226) 的 `LoadCacheDir` 说明：加载时不是用文件名作为 key，而是用分片里内嵌的 `CanonicalPath` 字段作为 key。

也就是说：
- **文件名** = 内容哈希（用于快速判断"内容变了没"）
- **cache key** = 内嵌的 `CanonicalPath`（审计文件的绝对路径）

两者**不对齐**：看到一个 `abc123.json` 的缓存分片，不打开它就不知道它对应哪个审计文件；反过来，想找某个审计文件的缓存，必须遍历所有分片读 `CanonicalPath`。

这造成了两个小麻烦：
1. 手工检查或清理特定审计文件的缓存时，没有直接的文件名映射关系
2. `LoadCacheDir` 实现里需要打开每个分片读 `CanonicalPath`，而不能直接用文件名 index

不过 [`cache.go` 的注释](file:///Users/stanford/code/vmr/internal/ctxgraph/cache.go#L232)已经明确说明这是"best-effort"——corrupt shard 就跳过，缓存的损失是可接受的。评审报告本身也同意"暂不动"。

### 负面影响

| 不修 | 修后 |
|------|------|
| 运维时无法快速定位某个审计文件对应的缓存分片 | 文件名直接反映内容（或路径 hash），可直接 grep |
| `LoadCacheDir` 需扫描所有分片 | 无影响（当前实现已经 best-effort 扫描，成本可接受） |

### 根因

缓存分片的设计在"可以避免重命名冲突"（content hash 文件名）和"可以快速按路径查找"（path-keyed 文件名）之间选择了前者，但没有记录这个取舍。

### 建议方案

**当前不修是合理的**。建议只做一件事：在 `LoadCacheDir` 的 doc comment 里补一句说明"为什么 key 是内嵌的 `CanonicalPath` 而不是文件名"，让下一个阅读这段代码的人不必重新推导。写进 `KNOWN_ISSUES` 作为已知的设计取舍。

---

## 登记项（核实成立但不单独展开）

以下各条均经源码核实成立，建议写进 `KNOWN_ISSUES`；或已被上述某条系统性问题覆盖。

| ID | 一句话 | 处置 |
|----|--------|------|
| P-1-3 | `TopLevelProbe` 不校验尾随字节 | 修复建议驳回——加尾随检查背离字节保真；改为文档说明契约为"探测"非"校验" |
| P-1-4 | `respnorm.go:456/472` 溢出分支跳过 model rewrite | 已作为 S-2 第五个实例；可达性低，随 S-2 机制一起解决 |
| P-2-3 | `attachmentSpans` 对大 body 重复扫描 | 性能项而非安全项（本地运行，客户端即操作员）；记入 KNOWN_ISSUES |
| P-2-4 | `imgprep` 两处静默丢弃（recover 回退、anthropic 分支） | recover 分支不可修；anthropic 分支应记 warning |
| P-2-5 | Connect 卡片 JS 有三份手抄 | 降级为观察，不作为独立问题；代码量小，影响面极低 |
| P-3-1 | 配置 hot-reload 在高频写入下可乱序 | 成立；触发面窄，但副作用是"从未被校验的混合态" |
| P-3-3 | 空 `api_key` 定级为 `SeverityError` | 应为 warning——空 key 在某些自建上游是合法的 |
| P-3-4 | 评分层无 NaN 纵深防御 | 当前不可达；保留为加固项 |
| P-3-5 | `quota` 包注释谎报依赖关系 | 成立；已并入 S-1（虚构注释断言类）；随 S-1 ① 处理 |
| P-5-3 | `buildRec2` 的 (path,line) join 无 TS 交叉校验 | 成立；审计日志是追加型，实际只在良性形态出现；记入 KNOWN_ISSUES |
| P-5-4 | `tagSummary` 用 `TokensIn > 0` 代理 `usageOK` | 成立；根因是 `RequestRow` 不携带 `usageOK`，属 S-3 同族；随 Agent C 的 `UsageInOK`/`UsageOutOK` 拆分一起修 |
| P-D4-4 | `renderFingerprint` 未折入 `taskseg.Profile` 身份 | 预防项；当前两个 Profile 不产生渲染差异 |
| P-D4-5 | `FileNameForRecord` 与 `FileNameForManifest` 的 `realModel` 来源不同 | 同一记录可能产生两个文件名 |
| P-D4-6 | `.parse-cache` 分片命名约定 | 成立，同意"暂不动"；记入 KNOWN_ISSUES |
| P-D4-7 | `chatmsg.MergeUsageBytes` 零调用（与 D-4 同条） | 已升级并入 S-1 死代码三件套 |
| P-D6-1 | LLM 自由文本未净化即进 `.md`（`<`/`>` 未处理） | 严重度低-中——产物是本地文件，非 web 渲染 |
| P-D6-2 | `isErrorMarker` 是跨包字符串契约的无守卫硬编码副本（7 处） | 两轨存在已记录；新意是无守卫；随 S-1 ② 一起解决 |
| P-D6-3 | `stepFactState` 前缀复用假设 LeadSys 不变 | 危害仅限 ContextCurve 角色份额偏移 |
| P-D6-4 | `corpus_contextrot.go:71-73`：`UsageOK=false` 的步骤归入"0-32k"桶 | 已作为 S-2 第六个实例；随 S-3 的 `UsageInOK`/`UsageOutOK` 拆分后修复桶判断逻辑 |
| P-D6-5 | journey 展示层是 `metricSpecs` 清单的第三、四份手抄 | S-2 的"手工清单"形态；随 P-D6-8 一起收敛 |
| P-D6-6 | `-llm-addr` 串行最多 7 次调用、无总预算 | 应加总时间预算与部分失败降级 |
| P-D6-7 | `searchableTranscript` 大语料下 O(N²) 复杂度 | 大语料唯一的复杂度悬崖；需提前排期 |
| P-7-4 | archtest 只守单向包边界（"only coupling"那半句无守卫） | 严重度低-中；规则本身是单向的，护栏与规则一致 |
| P-7-6 | 弃用别名 `vmr story` 的 flag 校验落后于 `vmr analyze` | 迁移期别名行为宽于主入口，方向反了 |
| P-7-7 | 一次 `vmr analyze` 对 `config.yaml` 做两次独立 `Load` | 随 P-D6-8 一起收敛为唯一入口 |
| P-7-8 | `sysinfo` 把系统调用失败折叠成 0 | 违反"missing data is not a zero"原则 |
| X-1 | 截断 anthropic 流：分侧 usage sniff 只在路由半区，分析半区用单布尔误判为精确 | 已作为 S-3 的核心实例；根因分析见 S-3 |
| X-2 | `report/detail.go:70-73` 的防御性回退已失效（恒不可达） | 防御性代码本身是不变量的漏洞；应删除并加注释说明原因 |
| X-3 | soft-block：2xx + ErrorClass=content 的 attempt 被分析侧误判为 Forwarded | 已作为 S-3 的核心实例；根因分析见 S-3；同时破坏 `quota_parity_test.go` 的假绿 |
| D-1 | `pricing.IsAggregatorVendor`（`pricing.go:287`）全仓零引用 | 死代码；注释声称被 `gen_standard_pricing` 调用，实际该工具调 `Ambiguities()`；随 S-1 ① 删除 |
| D-2 | `logtee.Recent` / `logtee.Subscribe`（`logtee.go:123/:141`）生产零调用 | 死代码；`/log` 走 `Follow()`，两者是被淘汰的前身；`Follow` 还复制了 `Recent` 的环读循环；随 S-1 ① 删除 |
| D-4 | `chatmsg.MergeUsageBytes`（`usage.go:104`）全仓零引用 | 死代码；三处注释声称被 `respnorm`/`replay` 使用，实际都调 `MergeUsageWithProtocol`；随 S-1 ① 删除 |

## 分组规划与子 Agent 任务单元

### 分组原则

按两个维度交叉：
1. **ROI 优先级**：决定处理顺序（高 ROI 先）
2. **Domain 聚合**：同一 Domain 的问题由同一个 Agent 处理，减少上下文切换成本，让 Agent 能在修改时一次性看到相关代码

### 批次安排

```
批次一（并行，约半天）──────────────────────────
  Agent A: 两行快修 · D1/D2 并发安全 + 审计保真
  Agent B: 形状立即修 · D4 mergeUsage + D7 vmr.sh

批次二（串行，2-3天，依赖批次一）────────────────
  Agent C: 记录事实停止反推 · 跨 D1/D2/D5（S-3 主线）

批次三（并行，2-3天，不依赖批次二）───────────────
  Agent D: 断言可机检 · D7 archtest + 死代码 + i18n（S-1 主线）

批次四（串行，1-2天，依赖批次二）────────────────
  Agent E: 让沉默变响 · D4/D5 未识别形状上报（S-2 机制）

批次五（并行，按需，不阻塞）────────────────────
  Agent F: 机制保养 · D4 stitch + D6 story 收敛 + D7 archtest 告警
  Agent G: 文档与系数 · tokenutil 来源 + KNOWN_ISSUES 补录
```

---

### 详细任务单元

#### Agent A：两行快修 · D1/D2 并发安全 + 审计保真
**批次：一 | 估时：半天 | 风险：零**

负责解决所有"两行修复"级别且不依赖其他变更的缺陷：

| 问题 | 改动 |
|------|------|
| P-1-1 `respnorm.opaque` 数据竞争 | `respnorm.go:459` 和 `:474` 的 `s.opaque = true` 包进 `s.mu`；补 `-race` 用例 |
| P-2-1 审计落盘 HTML 转义 | `audit.go:599` 加 `enc.SetEscapeHTML(false)`；`replay.go:658` 同改；补字节级测试 |
| P-1-2 上游无 HTTP/2 | `transport.go` 加 `ForceAttemptHTTP2: true`，或写进 KNOWN_ISSUES 说明刻意 H1.1 的原因 |

**前置条件**：无
**输出物**：3 处代码修改 + 对应测试

---

#### Agent B：形状立即修 · D4 mergeUsage + D7 vmr.sh
**批次：一 | 估时：半天 | 风险：极低**

负责 S-2 中可立即修的枚举漏项，以及 CLI 脚本的一词修复：

| 问题 | 改动 |
|------|------|
| P-D4-1 `mergeUsage` 缺 `response.usage` | `chatmsg/usage.go:155` 的 holder 列表加 `Nested(obj, "response", "usage")`（一行） |
| P-D4-2 `RenderPart` 缺 `document` case | `chatmsg/messages.go` 补 `case "document":` 比照 `input_file` |
| P-7-1 `vmr.sh` 漏 `analyze` | `vmr.sh:603` 在白名单加 `analyze` |

**前置条件**：无
**输出物**：3 处代码修改；P-D4-1/P-D4-2 需补对应协议的测试用例

---

#### Agent C：记录事实停止反推 · 跨 D1/D2/D5（S-3 主线）
**批次：二 | 估时：2-3 天 | 风险：中（跨包字段变更）**

这是全表 ROI 最高的主线，修的不是三个 bug 而是一整类漂移：

| 步骤 | 改动 |
|------|------|
| ① audit 增 `Forwarded` 字段 | `audit/audit.go` 的 `Attempt` struct 增 `Forwarded bool`，唯一置位点是 `router/router.go` 的 `forwardSuccess` |
| ② 分析侧改读字段 | `report/ingest.go:146` 和 `report/recextract.go:174` 改读 `a.Forwarded`，删掉反推谓词 |
| ③ chatmsg 增分侧函数 | `chatmsg/usage.go` 增 `ExtractUsageSides`，从 `respnorm/usagesniff.go:100-105` 下沉规则 |
| ④ UsageOK 拆分 | `ctxgraph/manifest.go` 和 `report/session.go` 的 `UsageOK bool` 换成 `UsageInOK`/`UsageOutOK`；更新 `report/recextract.go` 和 `story/cost.go` 的门控逻辑 |
| ⑤ replay 修正 | `replay/chargeReplay` 改调 `router.TokenCountersSides`；`recordView` 解码 `facts.EstimatedTokens` |
| ⑥ parity 测试扩充 | `quota_parity_test.go` fixture 扩为 `protocol × {正常, 截断, 4xx, softblock}`，消灭假绿 |

**前置条件**：批次一（Agent A/B）完成
**输出物**：跨 5 个包的字段变更 + 完整测试覆盖

**注意**：`audit.Record` 字段变更会影响历史 JSONL 读取——新字段 Go 零值为 `false`，历史记录默认 `Forwarded=false` 即"未转发"，与实际不符。Agent C 需评估是否需要回退兜底（如历史记录中 `Forwarded` 为 false 但 `Status < 400 && ErrorClass == ""` 时，仍按旧逻辑处理）。

---

#### Agent D：断言可机检 · S-1 主线（D7 archtest + 死代码 + i18n）
**批次：三 | 估时：2-3 天 | 风险：低**

负责 S-1 的修复：让虚构的注释断言要么被删除，要么被机器检查：

| 步骤 | 改动 |
|------|------|
| ① 删死代码 | 删 `pricing.IsAggregatorVendor`、`logtee.Recent`/`Subscribe`、`chatmsg.MergeUsageBytes` 及其虚构注释；修正 `docs/VirtualModelRouter_Design_v4_Quota.md:209` |
| ② 扩展 `doc_refs_test.go` | 反引号符号若在非注释代码中零引用即报错，配 `fileLineExemptions` 豁免表 |
| ③ i18n 配对检查 | 补 `i18n/report_*.go` ↔ `internal/report/section_*.go` 目录列表比对检查，**或**删掉 KNOWN_ISSUES 里"archtest 强制"那句话——二选一 |
| ④ expandEnv 对齐 | `expandReportEnv` 补第四条守卫；补表驱动等价测试；两者都改为跳过注释行（消除 P-3-2 误报） |
| ⑤ i18n bundle 扫描 | 空翻译绊线改为遍历全部导出 bundle（非手工注册表） |
| ⑥ pricing 文案归位 | `report/pricing.go:44-50` 的文案移进 `i18n` 包 |

**前置条件**：无（与批次二并行）
**输出物**：死代码清理 + archtest 扩展 + i18n 机制加固

---

#### Agent E：让沉默变响 · D4/D5 未识别形状上报（S-2 机制）
**批次：四 | 估时：1-2 天 | 风险：低**

负责 S-2 的机制层修复，让未识别的协议形状不再静默失效：

| 步骤 | 改动 |
|------|------|
| ① chatmsg 计数器 | `chatmsg` 对未识别的 content part / usage holder 计数；在 `vmr analyze` 输出里露一行"本次语料 N 个未识别形状" |
| ② providerquota 跳过上报 | `report/providerquota.go` 累计跳过数，在 §2.5 表下渲染 `"N attempts skipped (unknown provider: …)"` |
| ③ 协议覆盖测试 | 新增 `cmd/vmr/protocol_coverage_test.go`，由 `adapter.Names()` 驱动，遍历已注册协议 × 典型响应形状 |
| ④ parity fixture 覆盖补全 | 在 Agent C 基础上，把"新增一种 attempt 结束方式时必须同步扩充词汇表"写进 KNOWN_ISSUES |

**前置条件**：批次二（Agent C）完成（③④依赖 C 的 fixture 扩充）
**输出物**：可观测性增强 + 协议覆盖测试机制

---

#### Agent F：机制保养 · D4 stitch + D6 story 收敛 + D7 archtest 告警
**批次：五 | 估时：按需，不阻塞主线 | 风险：低**

负责不阻塞主线的长期机制改善：

| 问题 | 改动 |
|------|------|
| P-D4-3 stitch 赢家遮蔽 | `ctxgraph/stitch.go:344-399` 把 gap 约束前移为候选过滤（约 15 行） |
| P-D6-8 `extractRootUserIntent` | 改调 `j.InitialInstruction`；P-7-7 两次 Load 收敛为一次；P-D6-5 journey 展示层收敛到 `metricSpecs` |
| P-7-9 豁免表退化 | `file_sizes_test.go` 加批准日期字段 + 自然到期；加 ≥90% 逼近告警；`cmd_story.go` 下沉逻辑 |

**前置条件**：无
**输出物**：stitch 逻辑修正 + story 收敛 + archtest 机制改善

---

#### Agent G：文档与系数 · tokenutil 来源 + KNOWN_ISSUES 补录
**批次：五 | 估时：按需 | 风险：极低**

负责纯文档/注释类工作和登记项的 KNOWN_ISSUES 补录：

| 工作 | 内容 |
|------|------|
| P-7-10 tokenutil 系数文档化 | 在 `tokenutil` 包注释里写清系数来源；可选：加 `tools/` 下的误差反算脚本 |
| 登记项补录 | 把 P-2-3、P-2-4、P-3-1、P-3-3、P-3-4、P-D4-4、P-D4-5、X-2、P-D6-1、P-D6-6、P-D6-7、P-7-4、P-7-6、P-7-8 逐条写进 `KNOWN_ISSUES` |
| X-2 死分支清理 | 删除 `report/detail.go:70-73` 的恒不可达防御分支，加注释说明原因 |

**前置条件**：无
**输出物**：KNOWN_ISSUES 更新 + tokenutil 包注释

---

### 执行顺序总览

```
批次一（Agent A + Agent B，并行）
     ↓
批次二（Agent C，依赖批次一）+ 批次三（Agent D，独立并行）
     ↓
批次四（Agent E，依赖批次二）
     ↓
批次五（Agent F + Agent G，独立并行，可任意时间启动）
```

**关键路径**：批次一 → 批次二（Agent C）→ 批次四（Agent E）

所有批次五的工作（Agent F/G）不阻塞任何主线，可在批次一启动的同时并行进行。

---

*文档生成时间：2026-09-02 | 基于 PROJECT_REVIEW_REPORT_opus5_20260902.md，每条均经源码核实*

