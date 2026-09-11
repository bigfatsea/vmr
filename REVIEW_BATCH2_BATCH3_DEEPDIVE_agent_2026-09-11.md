<!-- Ver 2026-09-11, by Agent (pi) — 独立复核轮，仅分析与方案，未改动任何代码与既有文档 -->

# Review 批次 2 / 批次 3 问题深度评估报告

**评估对象**：`PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 附录 A.4 中「批次 2 — 中 ROI，需较完整测试 / 校准配套」与「批次 3 — 需先设计 / 待触发（价值高、易做错，禁止仓促）」两批共 6 项问题。
**评估基线**：当前工作树（批次 1 的 9 项顺手修复已落地，本文 6 项均未改动，`git status` 可证）。
**评估方法**：逐项回到当前源码独立核实机制与影响面，不采信前两轮（subagent 轮、Sonnet 5 复核轮）的任何结论；对每项给出：问题描述 → 客观存在性判定 → 改与不改的区别 → 根因分析 → 候选方案与明确推荐 → 完整 ROI 评估。
**本文档性质**：纯分析与方案，不修代码、不动既有文档。

---

## 总判定速览

| # | KI 编号 | 问题 | 机制是否客观存在 | 实际影响是否客观存在 | 报告建议方案是否合理 | 本文推荐 |
|---|---|---|:---:|:---:|:---:|---|
| 3-4 | §2.109 | 图片 span 计入文档计费 | ✅ 成立 | ⚠️ 成立，但比报告说的**更宽**（文档标记可被正文文本误命中） | ⚠️ 基本合理，但 `data` marker 歧义未解 | 方案 b（span 标类型 + media_type 回嗅） |
| 5-3 | §2.115 | 重复工具调用检测无时空局部性 | ✅ 成立 | ✅ 成立（长会话误报概率相当高） | ✅ 合理，窗口参数需校准 | 方案 b（相邻间隔约束式局部性） |
| 6-2 | §2.119 | jsonscan 畸形元素偏移未对齐 | ✅ 成立 | ❌ **实际影响接近零**（仅畸形 JSON 触发，且畸形字节本就会原样透传被上游 400） | ✅ 方向合理（WalkArrayElements 重构） | 方案 a（重构 + fail-open） |
| 1-1 | §2.86 | respnorm 扣留保活帧 | ✅ 成立 | ✅ 成立（反代 + 慢上游组合场景，感知差且难归因） | ✅ 合理但过于宽泛 | 方案 a（白名单旁路精确保活帧） |
| 5-7 | §2.100 | report 不消费 Attempt.tokens | ✅ 成立 | ✅ 成立（degraded 场景两侧口径必然对不上） | ⚠️ 合理但有语义陷阱（stamp 是 fresh、无 side 标志） | 方案 a（stamp 优先 + body 兜底，先不动 schema） |
| 5-5 | §2.57 | computeTimeSplit 无间隙上限 | ✅ 成立 | ✅ 成立（scheduler/`--continue` 场景必然命中，Median 逃过、Mean 与 ratio 失真） | ⚠️ 部分合理（「归 idle」不诚实） | 方案 b（cap + 新 unattributed 桶） |

---

# 一、3-4（§2.109）：图片附件 Span 被计入文档 token 计费

## 1.1 问题描述

`internal/server/facts.go` 中，`attachmentSpans` 用 4 个 marker 扫描请求体，把附件 payload 的字节区间收进一个**匿名 span 列表**（`[][2]int`，只有位置没有类型）：

- `"url":"data:` 与 `"image_url":"data:` —— OpenAI 系图片（Chat Completions 的 `image_url.url` data URI、Responses 的扁平 `image_url`）；
- `"file_data":"` —— Responses 的 `input_file` 文档块；
- `"data":"` —— **歧义 marker**：Anthropic 的图片 `source.data` 与文档 `source.data` 用的是同一个字段名。

`computeRequestFacts` 对这份匿名 span 列表做三笔加总：

```go
EstimatedTokens = estimateTextTokens(body, spans)      // 非 span 文本
                + int64(imageCount)*imageTokenEstimate // 每图固定 3000
                + estimateDocumentTokens(body, spans)  // 见下
```

而 `estimateDocumentTokens` 的逻辑是：只要 body 里**任何位置**出现文档标记（`"type":"document"` / `application/pdf` / `"type":"file"` / `input_file`），就把**全部 span 的字节数**（含图片 span）按 `bytes/20` 折算成文档 token。

**具体案例**：用户发一个 500KB 的 PNG 截图，正文里写「帮我把这份 application/pdf 转成 Markdown」。此时：

1. 图片 span（500KB）先按 `imageTokenEstimate=3000` 计一次（正确）；
2. 正文命中 `application/pdf` 文档标记 → `estimateDocumentTokens` 把这个图片 span 的 500KB 再按 `500_000/20 = 25,000` 计一次（虚增 8 倍以上）；
3. 合计 `EstimatedTokens ≈ 28,000`，而真实 input 可能不到 5,000。

这笔虚增数字流经两处消费方（见 1.2）。

## 1.2 客观存在性判定

**机制层：完全客观存在**，当前源码逐行可证（`facts.go` 的 `attachmentSpans`/`estimateDocumentTokens`/`computeRequestFacts`）。

**影响层：成立，且比报告的表述更宽一档**。附录 A 把触发条件概括为「图片+文档同请求 × degraded 扣费」双重罕见条件。但独立核实发现第一个条件其实**更松**：`documentMarkers` 是对整个 body 的 `bytes.Contains`——**纯图片请求**只要正文文本（用户消息、工具回显、代码块）里提到 `application/pdf` 或 `"type":"document"` 字样，就会点亮 hasMarker，把全部图片 span 计入文档折算。「提到 PDF 的话题里贴截图」是相当自然的对话形态，并非刻意构造。

真实扣费影响还需叠加 degraded 条件（流在 in-side usage 嗅到之前截断，`TokenCountersSides` 走 `inEst = Facts.EstimatedTokens` 估算扣费）。但即使不走到 degraded 扣费，`EstimatedTokens` 还有第二处消费：`internal/strategy/strategy.go` 的 `WithinContext`（`facts.EstimatedTokens <= ep.MaxContextTokens`）——它被刻意设计成**非 Condition**（带 fallback，不会清空候选集），但虚高的估算会让大图请求更容易越过端点声明的上下文窗口线，触发 ctx-fallback 重排，把本可服务的首选端点让位给次选。

**结论：问题客观存在；「双重罕见」的说法低估了第一个条件的宽度；「只影响配额展示」的说法漏掉了 WithinContext 这条路由侧消费。**

## 1.3 改与不改的区别

**不改**：
- degraded 扣费场景下，含图请求的配额扣费系统性偏大——方向保守（不会少扣 providers 的钱），但对用户是真实的额度虚耗，且无法从 `/status` 的 estimated_pct 之外察觉；
- `WithinContext` 的重排在大图请求上更容易误触发，路由质量有可感知的（虽不致命的）劣化；
- 代码注释自认「known imprecision, safe direction, not a correctness bug」——注释诚实，但「方向安全」不等于「该虚高」，这笔账一直挂在每一个混合附件请求上。

**改**：
- degraded 扣费回归「图片按图片计、文档按文档计」的直觉口径；
- `WithinContext` 判定恢复精度；
- 注释里的已知不精确项收敛，facts 模块的可信度基线上移。

不修的实际风险敞口：单机单用户 + 当前 token-plan 配置下，触发 degraded 扣费的频率取决于上游流稳定性；一旦某 provider 频繁在 usage 块前断流，这个问题会从「统计瑕疵」升级为「每天多扣 N 万 token」。

## 1.4 根因分析

**信息在产生时最丰富、在消费时才发现不够。** `attachmentSpans` 扫描时明明知道每个 span 是被哪个 marker 命中的（marker 即类型证据），但签名只返回 `[][2]int`，类型信息在产生点被丢弃。之后两个消费者对同一份匿名产物做相反的操作：`estimateTextTokens` 排除全部 span（防止图片按文本重复计费——注释里还专门写了 500KB 图片变 10 万幻影 token 的案例），`estimateDocumentTokens` 却累加全部 span。这不是某个消费者的 bug，是**产物类型设计不足以支撑两个消费者的语义**。

`"data":"` 的歧义性是第二个根因：Anthropic 的图片与文档 payload 共用 `"data":"` 字段名，单靠 marker 无法分类——这正是原方案只做「marker 标类型」不够用的原因。

## 1.5 候选方案与推荐

| 方案 | 内容 | 评估 |
|---|---|---|
| **a. marker 标类型** | span 带产生 marker 的类型：`url/image_url` → image；`file_data` → document；`data` → 保守按 document | 增量小；但 Anthropic 形态下图片与文档共用 `data`，混合请求仍误分类——**只修了 OpenAI 系，没修 Anthropic 系** |
| **b. 标类型 + media_type 回嗅**（推荐） | 在 a 基础上，对 `"data":"` 歧义 span，向 span 起点之前的 ~64 字节窗口回嗅 `media_type":"image/`——Anthropic 的 `source` 对象里 `media_type` 恒在 `data` 之前（`{"type":"base64","media_type":"image/png","data":"..."}`） | 增量同样小，分类精确覆盖三大协议；嗅不到（畸形/截断）按现状当 document，fail-open |
| **c. imgprep 协议感知解析接管** | 让 facts 复用 imgprep 已做的 content-block 遍历拿附件字节 | 架构上"正确"但重：imgprep 只对图片计数不持字节区间，要它返回 span 集合是新的跨包契约；拒绝 |

**推荐：方案 b。** 关键点：`media_type` 是三大协议附件对象共有的字段且都先于 data 值出现，回嗅窗口 64 字节足够（`"media_type":"image/png",` 约 30 字节）；无法判定时维持现状（保守方向），绝不因分类失败而少计。

**配套验证**：facts 单测覆盖三大协议 × 图片/文档/混合/正文误命中矩阵；复核 `cmd/vmr/quota_parity_test.go`——该测试的 parity 口径是「live 扣费 vs replay 扣费」，两侧都吃同一个 `facts.estimated_tokens` 值，facts 内部分类变化不影响 parity 恒等式，但 fixture 里的期望值若硬编码了旧估算数字需同步。

## 1.6 ROI 评估

- **Return**：中。① degraded 扣费口径回归真实（用户价值：不再为图片字节付两次钱）；② `WithinContext` 路由重排恢复精度（稳定性：不再因估算虚高让位首选端点）；③ 消除一个注释自认的长期 imprecision（易维护性）。
- **Investment**：小-中。`attachmentSpans` 签名从 `[][2]int` 改为带 kind 的结构（约 3 处调用方 + 2 个估算函数），media_type 回嗅约 15 行；测试矩阵 + parity fixture 同步约半天。
- **风险**：低。fail-open 设计（分类失败按现状）保证任何意外都退回今天的行为；不影响 exact 扣费路径（有真实 usage 时估算根本不参与）。
- **结论**：**值得做，批次 2 内应排首位**（三项中唯一同时触及扣费与路由两处的）。报告把它排在批次 2 而非批次 1 是合理的——「正文误命中文档标记」这个更宽触发面是本轮核实新发现的，值得在实施时一并写进测试矩阵。

---

# 二、5-3（§2.115）：exact_repeat_tool_call 检测无时空局部性

## 2.1 问题描述

`internal/journey/findings.go` 的 `detectExactRepeatToolCall`：跨**整条 Journey**按 `(tool_name, args)` 精确键分组计数，达到 `exactRepeatThreshold = 3` 即产出 Finding「疑似精确重复循环：X 已被相同参数调用 N 次」，`RelatedSeq` 列出全部历史出现位置。

**具体案例（误报）**：一个 200 步的编码会话，第 5 步、第 80 步、第 190 步各执行一次 `git status`（参数完全一致——空参 `git status` 在长会话里出现 3 次几乎是必然）。产出一条 Finding，把相隔 185 步的三次调用说成「疑似死循环」，`RelatedSeq: [5, 80]`——读者看到这条要么忽略（脱敏），要么被误导去排查根本不存在的循环。

**具体案例（真阳性）**：agent 反复对同一文件执行相同编辑、每次都同样失败——claude-code#15909 类事故，调用紧邻连续，才是这个检测器建模的真实形态。

## 2.2 客观存在性判定

**成立**。机制与影响都客观存在。代码注释自陈「3 是 early-warning bar，所以文案用 suspected 不是 confirmed」——这承认了误报可能性，但没有回答核心问题：**全局累计把「散布的重复」与「循环的重复」混为一谈**，而这两者对读者的含义完全不同。长会话（journey 半区的主要服务对象恰恰是长任务）中，常用只读工具（`git status`/`ls`/`cat` 同参数）散布出现 3 次的概率非常高，误报不是边缘场景而是常态。

## 2.3 改与不改的区别

**不改**：
- 长任务 journey 的 findings 列表携带常态性噪音，「suspected」字样随时间脱敏（狼来了）；
- `-compare`/`-benchmark` 中 finding 计数口径偏大，跨框架对比时这个指标的区分度被稀释；
- 真循环依然能被抓住（全局累计的召回率 100%）——这是维持现状唯一的实质论点。

**改**：
- 散布型重复不再触发，真循环（紧邻重复）保留；
- 检测器的「循环」语义与名字、文案首次对齐；
- 代价：校准基线变化——设计文档的 Findings 分布注记、既有 corpus 上的产出会变（正是附录 A 把它放批次 2 的原因）。

## 2.4 根因分析

检测器把「重复」建模成了**无时间结构的计数问题**，而「死循环」本质上是一个**时间局部性现象**：紧邻的重复才构成循环，散布的重复是正常工作节奏。`exactRepeatThreshold = 3` 这个标定值对「紧邻」语义是合理的（连打 3 次同参数调用确实可疑），对「全局」语义则必然过敏。注释里引用的真实事故（数百次重复才被发现）进一步说明：真循环的信号强度远超阈值，收窄判定窗口不会漏掉它们。

## 2.5 候选方案与推荐

| 方案 | 内容 | 评估 |
|---|---|---|
| **a. 尾部滑窗** | 最近 N 步窗口内同 key 出现 ≥3 次即触发 | 实现简单；但窗口大小 N 是新的任意常数，5 步/8 步的取舍又要一轮校准 |
| **b. 相邻间隔约束**（推荐） | 同 key 的**相邻两次**出现间隔 ≤ `maxRepeatGap = 2` 步，且连续满足 3 次（即 `Seqs[i+1]-Seqs[i] <= maxRepeatGap` 对连续两对成立） | 直觉上就是「循环」：反复打转中间偶尔夹一步别的也算，隔 80 步再调用不算；无窗口大小这个自由度，只有一个 gap 语义常数 |
| **c. 保留全局 + 分级文案** | 全局累计不变，散布型（首尾间隔大）降级为低置信提示 | 不改判定只改文案——噪音依旧进列表，compare 计数依旧偏大，没有解决根因 |
| **d. 提高全局阈值** | 3 → 5/8 | 弱化真循环的 early-warning 价值，误报只是被推迟（长会话总会到 5 次） |

**推荐：方案 b。** 理由：①「相邻间隔」直接编码了循环的定义，无新增自由维度；② 对真实事故形态（紧邻重复）召回不变；③ 实现是 `groupToolCallsByKey` 产出 Seqs 后的一个线性扫描；④ 参数只有一个 `maxRepeatGap`，按真实循环「偶尔夹一步修正」的形态取 2，写入命名常量 + 校准注。

**配套验证**：在既有审计语料上跑新旧对比，产出「删除的误报数 / 保留的真阳性数」进差分测试与设计文档的 Findings 校准注；`RelatedSeq` 语义随之改为「窗口内的前序出现」。

## 2.6 ROI 评估

- **Return**：中。journey findings 的信噪比是叙事半区的核心卖点；compare 的 finding 计数恢复区分度。真循环召回不受损（关键前提，已论证）。
- **Investment**：中。实现本身小（半天内），但校准 + 设计文档同步 + 差分测试是完整闭环的另一天——这正是它被分到批次 2 而非批次 1 的全部原因，分批正确。
- **风险**：低。纯分析半区、纯展示层 finding，误杀的真阳性下限有 corpus 对比兜底。
- **结论**：**做，按批次 2 节奏**。报告建议合理；本文补充的 b 方案比报告的「局部滑窗」少了一个人为参数。

---

# 三、6-2（§2.119）：jsonscan 畸形元素扫描偏移未对齐

## 3.1 问题描述

`internal/jsonscan/rewrite.go` 的 `rewriteRolesInTopLevelArray` 手写了「遍历 messages 数组元素、在元素内找 role 键」的循环。内层循环在三种情况 `break`：键位置不是 `"`（`// malformed`）、`SkipJSONString` 失败、`SkipJSONValue` 失败。**break 后 `i` 停在畸形点，没有快进到当前元素对象的结束符**，外层循环从畸形点继续扫描——可能把嵌套在内容里的 `{` 误认成新的顶层消息对象。

**触发条件（本轮核实的关键事实）**：
1. **仅在畸形 JSON 上触发**。valid JSON 的对象键位置（`{` 或 `,` 之后）必有引号，三个 break 分支在合法输入上不可达——`SkipJSONString`/`SkipJSONValue` 失败同样意味着字节流已不是合法 JSON；
2. vmr 的请求体在进入 rewrite 前已**完整读入**（读失败直接拒绝请求，截断 body 不会到达）；
3. 即便重扫产生了错误的 role 改写，产物仍是畸形 JSON，字节保真透传下上游照样 400；
4. 现有 fuzz（`FuzzRewriteRoles`）对 invalid JSON 输入明确不保证输出形状（oracle 注释 "garbage in: no shape guarantee"），所以 fuzz 一直是绿的——不是 fuzz 漏了，是 oracle 层面就豁免了。

## 3.2 客观存在性判定

**机制成立，实际影响接近零。** 这与报告附录 A 的判断一致（「仅畸形 JSON 触发，且字节保真透传本就会把畸形字节发上游」），本轮独立核实结论相同，并且补充了第 2、4 两条支撑（截断 body 不可达、fuzz 豁免是刻意设计）。**这条不应该被理解为「活跃缺陷」，它的真实身份是：一段手写的、与包内既有原语重复的边界管理代码，在畸形输入下行为未定义。**

## 3.3 改与不改的区别

**不改**：没有任何已知的正确性损失——畸形输入的任何输出都会被上游 400，真实流量不受影响。代价是隐性的：`rewrite.go` 里同时存在两套数组元素边界语义（手写循环 vs `WalkArrayElements`），未来维护者改其中一处时容易误推另一处的行为；且「畸形输入行为未定义」这个事实只活在代码评论里。

**改**：消除整类未定义行为（畸形输入整体返回原字节，fail-open、byte-faithful），删除手写边界循环（净代码量大概率下降），`FuzzRewriteRoles` 的 oracle 可以下沉一条更强的断言（valid JSON 上改写只发生在顶层元素的 role 键）。

## 3.4 根因分析

手写内层循环的原因是历史性的：它要在「元素内逐键扫描」的同时记录 role 值的位置用于 splice，而 `WalkArrayElements` 的 visit 回调只给元素区间，看起来「还得在元素内再写一层循环」——于是作者顺手把整个遍历都手写了。但实际上**两层职责本可以分离**：外层元素定界交给 `WalkArrayElements`（它用 `SkipJSONValue` 严格定界，畸形即返回 `ok=false`），内层 role 扫描限定在 `[elemStart, elemEnd)` 内——畸形元素直接放弃整个 rewrite。重复实现边界管理是根因，畸形输入只是让它显形。

## 3.5 候选方案与推荐

| 方案 | 内容 | 评估 |
|---|---|---|
| **a. 重构到 WalkArrayElements**（推荐） | 外层用 `WalkArrayElements(raw, arrStart, arrEnd, visit)`；visit 内在元素区间扫 role；任何畸形（`ok=false`）→ 整体 `return raw, nil`（fail-open） | 代码更少；边界语义单一且复用已 fuzz 原语；fail-open 与 RewriteModel 的「不能安全改写就原样返回」哲学一致 |
| **b. 保留手写循环 + break 快进** | break 处用 `SkipJSONValue` 从元素起点快进到元素尾 | 效果等价于 a，但保留了第二套边界代码——修了 bug 没修根因 |
| **c. 只强化 fuzz oracle，不改实现** | valid JSON 分支断言改写位置精确性 | 不消除隐患本体；且对 invalid JSON 的未定义行为依旧存在 |

**推荐：方案 a。** 配套两件事：① `fuzzRoleRewrite` 的 oracle 在 valid JSON 分支增加「改写只发生在元素顶层 role 键」的位置断言（现在只查 JSON 有效性和顶层键不变）；② 新旧实现在语料 + fuzz 上对拍一轮，确认输出逐字节一致（valid 输入）/ 都返回原字节（invalid 输入）。

## 3.6 ROI 评估

- **Return**：低-中。无活跃 bug 可修；收益是防御性深度（未来 body 校验策略变化时不再依赖「上游会 400 兜底」）+ 代码简洁性 + 隐患显性化。jsonscan 是路由热路径依赖，这段代码的每一次被触碰都值得让边界语义更单一。
- **Investment**：小-中。重构本身约 40 行净变化 + oracle 强化 + 对拍，一天内。**注意一个反直觉点：因为无活跃 bug，这项工作没有「修复收益」，全部回报在防未来——所以它排批次 2（做，但不紧急）而不是更早，是准确的定位。**
- **风险**：低-中。rewrite 在转发热路径上，重构必须带 benchmark 对拍（`BenchmarkRewriteModelSplice` 同族）确认无回退；`WalkArrayElements` 的 visit 回调闭包若阻碍内联，需看一眼 alloc 基线。
- **结论**：**做，维持批次 2 定位**。报告方案合理；本文确认其「更简单，复用已 fuzz 的原语」的判断，并补充了对拍与 oracle 强化的具体前置。

---

# 四、1-1（§2.86）：respnorm undecided 阶段扣留保活帧

## 4.1 问题描述

`internal/respnorm/respnorm.go` 的流式四态机中，SSE 流初始处于 `modeUndecided`——为侦测 MiniMax inline-think 形态，在第一个「payload-bearing」事件（content/text 非 `<think>` 开头、tool_calls、专用 reasoning 字段）到达前，**所有完整事件都囤在 `s.pending`，一个字节都不发给客户端**。`classifyEventAcc` 对没有 content/text/reasoning 痕迹的事件（Anthropic 的 `event: ping`、SSE 注释行、`message_start` role marker）返回 `verdictUndecided`——它们不决定模式，但也**不被放出**。

**具体案例**：vmr 部署在 nginx 后（`proxy_read_timeout` 默认 60s）。上游某个慢/排队的模型在 90 秒内只发 Anthropic 保活 `event: ping`（约每 10s 一帧）。时间线：

1. vmr↔上游：完全健康——ping 每帧都被读入，`copyFlush` 的 idle timer 基于 upstream Read，vmr 不会掐断连接；
2. vmr↔客户端：**90 秒零字节**（200 响应头已发出，body 无限静默）；
3. 第 60 秒 nginx 判定 upstream 无响应，向客户端回 504；
4. 第 90 秒首个 content delta 到达，`decide()` 翻转 passthrough 并放出全部积压——但对已被切断的客户端链路无意义。

用户感知是「网关挂了」，而 vmr 的日志与审计显示这是一次正常完成的转发——**极难归因**。8MB 的 `bufferedCap` overflow 守卫对 ping 形态无效：ping 约 30-100 字节/帧，攒满 8MB 需要数十万帧，现实等待中永远到不了。

## 4.2 客观存在性判定

**成立。** 机制、场景、影响三层都能在源码与部署形态上落实。KNOWN_ISSUES §2.86 已登记为 [低，需先设计]，本轮核实补充两点校准：

1. **触发面比「慢上游」更具体**：需要「客户端侧存在空闲读超时」这个前置——裸连 vmr 的主流 SDK（Anthropic/OpenAI 官方 SDK）默认没有 per-read idle timeout，**反代（nginx/traefik 默认 60s）和部分企业内网代理才是真正的触发面**；
2. **影响不对称性是它难缠的原因**：vmr 侧一切正常（idle timer 被 ping 喂着），受害的只有客户端链路——日志、审计、`/stats` 全部无异常痕迹，用户报障时运维第一反应是查上游而不是查 respnorm。

严重度维持「低」是合理的（触发需要部署组合），但「需要先设计」的定性完全正确。

## 4.3 改与不改的区别

**不改**：反代部署 + 慢/排队上游的组合下，客户端表现为流式假死后 504；无法从 vmr 侧观测定位；受影响用户只能靠「绕过反代直连」规避——而 vmr 的目标部署形态（本地/内网网关）恰恰常见反代。

**改**：该组合场景下 ping 帧实时透传，客户端侧的空闲计时器被持续喂住，nginx 不再超时；代价是触碰核心四态机——这是全路由半区最精密、测试保护最厚的代码，改错一处（比如 buffered 模式下重复发射、或 pending/scanned 偏移错位）会直接破坏字节保真。

## 4.4 根因分析

`verdictUndecided` 的语义被**合并**了两件本不相同的事：「这个事件不能决定模式」（分类事实）与「这个事件必须被扣留」（处理动作）。ping/注释帧天然属于前者但天然不属于后者——它们没有 content 字段（quirk 修复无从触及）、没有 usage（嗅探无关）、没有 model 字段（改写无关），是三类模式（passthrough/buffered/opaque）下处理方式**完全相同**的事件。把「模式无关事件」扣留在「模式未定」的桶里，是分类粒度不足，不是四态机设计错误。§2.86 标记「与 MiniMax buffered 不共存」的旧担忧，恰恰因为 ping 与 buffered 修复在字节层面零交集而不成立——这是设计先行时应当论证掉的第一个疑点。

## 4.5 候选方案与推荐

| 方案 | 内容 | 评估 |
|---|---|---|
| **a. 白名单旁路保活帧**（推荐） | 在 undecided 归类阶段识别**精确形态**的保活帧（`event: ping` SSE 事件、仅注释行 `:` 构成的事件帧、data 恰为 `{"type":"ping"}` 形状），直接写入 `s.out`，不入 `s.pending`，不参与 decide | 最窄改动；放出的字节就是上游字节（byte-faithful）；与三分支正交（见 4.4 论证）；识别失败维持现状（fail-open） |
| **b. 全部 undecided 完整事件提前放出** | 不再区分帧类型 | **拒绝**：decide 一旦翻转为 buffered，输出变成「前半 raw、后半 repaired」的混合流，早期事件逃过 think-strip——破坏 quirk 修复的前提 |
| **c. vmr 注入合成 keepalive**（如每 15s 自造 `: keepalive`） | 服务端生成字节喂客户端 | **拒绝**：向客户端流注入非上游字节是第 6 处协议偏差，违反字节保真不变量的准入纪律 |
| **d. 时间阈值降级 opaque** | undecided 持续 >30s → flush pending raw + 降级 opaque 直通 | 保真（放的是原始字节）但牺牲 MiniMax think-strip；且引入时间参数与「降级后上游恢复又发 think 块」的边界；作为 a 的兜底备选，不作为首选 |

**推荐：方案 a。** 设计先行时要逐条钉死的边界（这正是「禁止仓促」注记的价值）：

1. **pending/scanned 记账**：bypass 发生在事件边界归类之后，被旁路的帧不得进入 `s.pending`（否则 finalize 重复发射），`s.scanned` 偏移不受影响；
2. **mode 翻转后的行为**：已旁路的 ping 不参与 buffered 的 `s.buf` 累积与 think-strip 重放——客户端收到「pings + repaired body」，总字节 = 上游字节减去无（ping 本来就会被 buffered 重放，提前放出不算增减偏差）；
3. **与 usage 嗅探正交**：ping 帧无 usage 字段，`noteUsage` 的 `bytes.Contains(usageFieldMarker)` 门天然过滤；
4. **evidence 留痕**：新增 `ping_bypassed` note（与 `overflow_raw_passthrough` 同机制），让 `vmr analyze` 能统计该路径命中频率——这也是验证修复有效性的观测点；
5. **回归面**：`FuzzStream` 全量 + 既有四态机测试 + 新增「ping 重延迟后 content」集成用例（模拟 4.1 的 90s 场景，时间缩放到 ms 级）。

## 4.6 ROI 评估

- **Return**：中-高。触发面窄（反代 + 慢上游），但一次触发的用户感知是「网关不可用」且运维归因成本高；修复后该场景整类消除，且 `ping_bypassed` 观测点让「慢上游」本身变得可见（额外的可观测性收益）。
- **Investment**：中-高。核心四态机改动 + 5 项边界设计 + 完整回归，预计 2-3 天设计加实现。**这是批次 3 三项中唯一「改动本身有实险」的，分批正确。**
- **风险**：中。缓解手段齐备（fuzz、字节对拍、evidence note），但回归面是全部流式形态。
- **结论**：**维持「需先设计」定位，但本文已把设计前置项列全——按 4.5 的五条边界走查通过后即可进入实施，不需要无限期等待触发条件。** 报告的触发条件写的是「真实用户报告流式假死」；本文建议把触发条件放宽为「设计走查完成」——因为影响是部署形态决定的，等用户报障属于被动。

---

# 五、5-7（§2.100）：report 不消费 Attempt.tokens 盖章值（token 双路径）

## 5.1 问题描述

同一条审计记录的 token 数，系统里存在两条独立计算路径：

1. **路由侧盖章**：每个 forwarded attempt 被盖上 `Attempt.Tokens`（`audit.TokenCount{In, Out, CacheRead, CacheWrite}`），与 quota 扣费**同一对象、同一时刻**取得；`/stats`（`server/stats.go` → `livestats`）消费它；
2. **分析侧反解析**：`vmr analyze` 的 `internal/report/session.go` 从 `client.response.body` 用 `chatmsg.ExtractUsageSides` 重新解析 usage——与盖章无关。

两条路径在两类场景下分叉：

- **degraded 场景**（流在 usage 块前截断）：quota 走 exact-vs-degraded fold——in 侧没嗅到时 `In = Facts.EstimatedTokens`（估算值）入账；report 反解析同一 body 得到残缺 usage（out≈1 占位被 side 规则抑制），In 列显示 0/unknown。**同一个请求，/stats 说花了估算的 N 万 token，analyze 说 0**；
- **语义层差异**：即使 exact 场景，`Attempt.Tokens.In` 存的是 **fresh**（净 cache，`u.Fresh()` = In − CacheRead − CacheWrite），report 的 `Usage.In` 是 **gross**（厂商报的原始 input 总量）。报表的缓存命中率表（`cacheHitRate = cached/in`）依赖 gross 口径——直接照搬 stamp 会算错，必须用四分量重建 gross。

## 5.2 客观存在性判定

**成立。** KNOWN_ISSUES §2.100 登记的三个分叉点（degraded 口径、fresh vs gross、加性字段未消费）全部在当前源码可证：`session.go` 的 `collectResponse` 只走 `ExtractUsageSides`；`factscache.go` 的 `attemptFacts` 保留了 `IsForwarded` 却丢弃了 `Tokens` 字段——同一结构体里一个盖章字段被消费、另一个被丢弃，正是「欠了一半」的状态。

值得强调的核实结论：**这不是 report 的 bug，report 的数字在自己口径内是对的**。问题的本质是系统对「同一请求的 token 数」存在两个都有道理但互不相认的权威——运维对不上账时，没有任何一处文档告诉他两边为什么差。

## 5.3 改与不改的区别

**不改**：
- degraded 场景的 `/stats` 与 `analyze` 差异永久存在，且每次都需要人工口头解释口径；
- report 在流截断场景下系统性低估（0 vs 估算值）——分析半区恰恰是排查「那次到底花了多少」的工具，最有用的时候数字缺席；
- 每个新的 token 消费方（未来仪表盘、导出）都要再选一次路径，双路径的熵持续扩散。

**改**：
- 三方（quota、/stats、analyze）同源，degraded 场景数字一致；
- report 获得路由侧的 exact/degraded 判定结果（不需要自己重新反推）；
- 旧审计记录（无 stamp 时代）靠回退路径兼容，无迁移成本。

## 5.4 根因分析

路由半区当初加 `Attempt.Tokens` 盖章的动机写在 AGENTS.md 的不变量里：「路由半区能盖章的就不让下游反推」。livestats 当期消费了（它就在路由进程里），report 因为字段是加性的、不消费也编译通过而欠账。这是 **IsForwarded 模式的半成品**：同一个 attempt 结构里，`IsForwarded` 已经走完「盖章优先、启发式兜底」的全流程（`factscache.go` 有完整镜像实现），`Tokens` 停在「盖了章、没人读」。根因不是技术难度，是**改动窗口一直没开**（跨 viewmodel + golden fixture）。

## 5.5 候选方案与推荐

| 方案 | 内容 | 评估 |
|---|---|---|
| **a. stamp 优先 + body 兜底**（推荐，先行） | `collect()` 已持有 `rec.Attempts`（`session.go:404` 在遍历）——取最后一个 `IsForwarded && Tokens != nil` 的 attempt；`gross In = Tokens.In + CacheRead + CacheWrite`（四分量重建）；`UsageInOK/UsageOutOK` 仍从 body 推导（同一信号源，语义不变）；stamp 缺失（旧记录/未盖章场景）→ 现行 body 解析 | 不动 schema；exact 场景完全一致；degraded 场景主差异（in 侧估算 vs 0）消除；实现集中在 collectResponse 一处 |
| **b. stamp 加 per-side seen 标志**（完整方案，后行） | `TokenCount` 增加 `in_seen/out_seen`（schema 加性），report 完全镜像 quota 的 exact/degraded fold，body 解析降级为纯兜底 | 一致性最彻底；但 report 会把「估算值」当 exact 呈现（stamp 不含 estimated 标记的话），要么再加 estimated 标志、要么报表加脚注列——复杂度连环；且需要新旧记录混跑的兼容逻辑 |
| **c. 只加差分测试，不动实现** | 钉住两路径在 exact 场景的一致性 | 不消除 degraded 分叉，只让分叉「可检测」——治标 |

**推荐：先 a 后 b 的两步走。** a 是无 schema 变更的最大收益点，且差分测试（`cmd/vmr/quota_parity_test.go` 的模式：路由侧调路由自己的导出入口，分析侧走 analyze 管线，断言 exact 场景逐字节一致）可以同时钉住 a 的正确性；b 只在 a 落地后 degraded 口径仍有运维投诉时启动——届时把 `estimated` 标志（而非 per-side seen）加进 stamp 才是最小充分集。**一个实施陷阱要写进方案**：report 的缓存表依赖 gross In，而 stamp 是 fresh——直接把 `Tokens.In` 填进现有 `Usage.In` 会悄悄算错所有缓存命中率列，四分量重建是方案 a 的必要组成而非可选优化。

## 5.6 ROI 评估

- **Return**：中-高。analyze 报表的可信度根基是「数字与 /stats、账单对得上」；本项把「对不上」从系统性分叉收敛到可解释的残余（degraded 时 stamp 估算 vs body 缺失——a 落地后两者同源）。财务对账场景的运维价值直接。
- **Investment**：中。collectResponse 改造 + gross 换算 + 差分测试 + golden fixture 更新（已知会变，是成本主体）——约 2-3 天。批次 3「有分析半区改动窗口时一起做」的定位准确：单独为它开窗口不划算，搭车顺手做最经济。
- **风险**：低。纯分析半区、离线、有 golden 对拍兜底；路由侧零改动。
- **结论**：**做，两步走，a 先行。** 报告把它放批次 3 正确；本文的增量是把「stamp 是 fresh、无 side 标志」这两个语义陷阱显性化，避免实施时踩缓存命中率表的坑。

---

# 六、5-5（§2.57）：computeTimeSplit 单间隙归因无上限

## 6.1 问题描述

`internal/journey/metrics.go` 的 `computeTimeSplit` 把每对相邻 Step 之间的 wall-clock 间隙二分：下一步 `HumanInitiated` → 计入 `HumanIdleMS`（人类空闲）；否则 → 计入 `AgentExecMS`（agent 在本地干活：执行工具、规划）。**间隙不设上限。**

**具体案例**：一个 Claude Code 会话周一工作到第 40 步，周五开发者用 `claude --continue` 恢复（未输入新指令，agent 自动续跑）→ 第 40 步响应落地到第 41 步请求到达之间隔了 4 天（345,600,000 ms）。该间隙被全额记入 `AgentExecMS`。后果：

1. 该 journey 的时间拆分（`time_split`）显示「agent 执行占 99.99%」——一张在直觉上即荒谬的图；
2. `-benchmark` 跨 journey 分布：`AgentExecMS` 的 **Mean 被拉到天级**（Median 幸存——分布对少数极端值稳健），KNOWN_ISSUES §2.57 记录的实测「Median 8s / Mean 数小时」即此；
3. `-compare` 的 `ModelToToolRatio`（agent 执行 / 模型时间比值）对该 journey 完全失真——比较视图的核心指标被一个间隙毁掉。

i18n 里已有脚注（`MetricDistFootnote`）：“Mean on the time-based metrics is very sensitive to a few long-lived journeys... Read Median and P90”——免责，不治因。

## 6.2 客观存在性判定

**成立，且是六项中最容易构造复现的一类。** 触发不需要任何罕见组合：凡是「会话生命周期超过单次工作时段」的使用形态（`--continue` 恢复、scheduler/cron 驱动的续跑、跨天挂机监视类任务）都必然命中。根因注释里「agent kept working locally」的假设写于短会话场景（人盯着的交互式会话中，长间隙确实意味着 agent 在本地跑长命令），**场景演化戳破了假设**：现代 agent 会话的存活周期已经远超单次注意力时段。

需要校准的一点：脚注免责并非无效——**Median/P90 是可用的**，被摧毁的是 Mean、journey 级 time_split 和 compare ratio 三个具体出口。损害真实但边界清晰。

## 6.3 改与不改的区别

**不改**：
- benchmark 的 Mean 列在含长命 journey 的语料上永远不可读（脚注挡住了「据 Mean 下结论」，但 Mean 这一列本身等于报废）；
- compare 的 `ModelToToolRatio` 在长命 journey 上是垃圾值，且没有脚注保护（ratio 的 Format 规则只处理了 0 值场景）；
- journey 的 time_split 图对跨天会话必然呈现荒谬比例——「99% agent 执行」会误导事故复盘的读者。

**改**：
- 三个出口全部恢复可读性；
- 代价：指标语义变更——设计文档的时间拆分定义要改、既有 golden/compare 快照要更新、跨语料的指标连续性断一次（历史数据重跑后数字变化，需要 release note 说明）。

## 6.4 根因分析

二分归因规则隐含了一个**未验证的等价式**：「下一步不是人发起」≡「agent 在本地干活」。在交互式会话里二者近似成立；在 `--continue`/scheduler 形态下，间隙的真实归属是「无人在场的等待」——既不是人类空闲（没人看），也不是 agent 执行（agent 进程可能根本没活着）。正确的模型是**三态**：human idle / agent exec / unattributed（不可归因），现在的二态模型把第三态强行塞进了前两态。

另外要指出：**用时间阈值做截断只是治标**。真正精确的归因信号是「下一步的首个新事件是不是响应上一步 tool_call 的 tool_result」——是，则间隙就是本地工具执行时间（agent 真的在干活）；不是，则间隙归属存疑。这个信号 Step 数据里现成可算（`NewEvents`/`ToolCalls` 在 build 阶段就有），但要把 flag 的计算接进 build 管线并重新校准，改动面比 cap 大一档。

## 6.5 候选方案与推荐

| 方案 | 内容 | 评估 |
|---|---|---|
| **a. cap + 超额归 idle**（KNOWN_ISSUES §2.57 原建议） | 单间隙 > 1h 的部分计入 idleMS | 实现最小；但「无人在场的等待」被标成「人类空闲」——用一个新的不准确替换旧的不准确，idle 列的语义同样被污染。**不推荐** |
| **b. cap + 新 unattributed 桶**（推荐，先行） | 单间隙 > `maxAttributableGap`（1h，命名常量）的部分计入新字段 `UnattributedMS`（JSON 加性）；benchmark 分布表加一行；设计文档同步 | 三态模型的最小实现；诚实（不假装知道归属）；golden/compare 快照更新是主要成本 |
| **c. 工具续接归因**（principled，后行） | build 阶段为每 Step 计算 `isToolResultContinuation`（首新事件是响应上一步 tool_call 的 tool_result）；间隙归因规则改为：continuation → agentMS；HumanInitiated → idleMS；其余 → unattributedMS | 最精确——把「agent 在干活」从猜测变成证据；改动进 build 管线，需要新 flag 的校准与文档；b 落地后 c 只是改「归入哪个桶」的规则，演进路径平滑 |
| **d. 维持现状 + 脚注** | 现状 | Mean/ratio/time_split 三个出口持续失真；脚注对 ratio 无保护 |

**推荐：b 先行、c 后行。** 阈值依据：人类「看一眼回复再继续」的间隔几乎不会超过 1 小时，超过 1h 的沉默更可能是会话被搁置/调度恢复——1h 是把「真实的长本地命令」（15-30 分钟的测试套件、构建）保住在 agentMS 里的合理分界。实施要点：① `UnattributedMS` 进 `Metrics` JSON（加性，下游零破坏）；② `-benchmark` 分布表加行 + 脚注更新（从「Mean 不可读」改为「Mean 含 unattributed 说明」）；③ 差分测试构造一个含 4 天间隙的 fixture，断言旧值进 unattributed 而非 agentMS；④ 设计文档时间拆分一节改写为三态定义。

## 6.6 ROI 评估

- **Return**：中。benchmark 的 Mean 与 compare 的 ratio 恢复可解读性；journey time_split 不再出现反直觉比例。这三个出口是分析半区「效能评估」卖点的直接载体。
- **Investment**：中。b 方案本身小（阈值 + 新桶 + 渲染行），完整闭环（文档 + 差分 + golden 更新）约 2 天；c 方案再加 2-3 天。批次 3「改需指标语义 + 设计文档 + 差分测试」的成本判断准确。
- **风险**：低。纯分析半区、无路由侧影响；最坏情况是新桶数字偏大——它本来就是「不知道」的诚实呈现。
- **结论**：**做，b 先行。** 报告建议的「归 idle」方向需要修正为「归 unattributed」——这是本文对批次 3 清单的唯一实质性方案修正。批次定位（禁止仓促）正确，但「仓促」的风险点在桶语义与 golden 快照，不在实现难度。

---

# 七、综合结论

## 7.1 两批次的整体判断

1. **六项问题全部客观存在**——没有一项是误报。但存在性要分层表述：3-4、5-3、1-1、5-7、5-5 五项有真实可感的运行时影响；**6-2 是唯一「机制存在、实际影响接近零」的项**，它的价值在防御性与代码简洁，实施预期要按这个定位摆。
2. **3-4 的影响面被前两轮低估**：文档标记可被正文文本误命中（不要求真的有文档附件），且 `EstimatedTokens` 除配额外还消费于 `WithinContext` 路由重排——建议实施时把这两个新事实写进测试矩阵与 KNOWN_ISSUES 条目。
3. **两处方案需要修正**：5-5 的「超额归 idle」应改为「归 unattributed 新桶」（不诚实归因换诚实归因才有意义）；1-1 的实施应锁定「白名单旁路」而非更宽的「undecided 事件全放行」（后者破坏 buffered 修复前提）。
4. **一处方案被前轮正确地收缩了**：1-1 若采用「注入合成 keepalive」会违反字节保真准入纪律，任何实施讨论都应先排除该选项。
5. **批次划分本身合理**：批次 2 的三项共同特征是「实现不难但验证闭环不可省」，批次 3 的三项共同特征是「语义变更 + 回归面大」——这与各项的核实结论吻合，无需重排。

## 7.2 建议实施顺序（两批次内部微调）

```
批次 2（顺序建议）：
  ① 5-3 重复调用局部性  —— 纯分析层、零热路径风险，先做先收噪
  ② 3-4 图片 span 分型  —— 涉及扣费口径，测试矩阵最重，紧随其后
  ③ 6-2 jsonscan 重构   —— 无活跃 bug，带 benchmark 对拍收尾

批次 3（顺序建议）：
  ① 5-7 stamp 消费（方案 a）—— 等"分析半区改动窗口"，与 ①②③ 任一搭车
  ② 5-5 unattributed 桶（方案 b）—— 独立窗口
  ③ 1-1 保活帧旁路（方案 a）—— 设计走查（本文 4.5 五条边界）通过后立项
```

## 7.3 需要项目所有者确认的事项

1. 5-5 的 unattributed 桶会改变 `-benchmark` 与 compare 产物的数字（golden 快照更新）——历史语料重跑后「指标连续性断一次」是否可接受；
2. 5-7 两步走中，若方案 a 落地后 degraded 口径仍有对账投诉，是否启动 stamp schema 扩展（方案 b）；
3. 1-1 是否采纳本文建议——把触发条件从「真实用户报告」放宽为「设计走查完成」，主动消除反代场景的定时炸弹。
