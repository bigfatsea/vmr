<!-- Ver 2026-08-15 21:00, by opus-5 -->

# VMR 两份架构评审的全条目源码核实报告

> **本文定位**：把 `docs/VMR_Comprehensive_Architecture_Review_and_Refactoring_gemini-3.7-flash.md`（下称 **v1**）与
> `docs/VMR_Comprehensive_Architecture_Review_and_Refactoring_gemini-3.7-flash-v2.md`（下称 **v2**）中提出的**每一条**
> 问题、建议与批次结论，逐一回到源码验证——**不区分它们自称已完成还是未完成**，一律重新核。
>
> **这不是权威待办清单**。当前状态的唯一权威清单是 `docs/KNOWN_ISSUES_sonnet-5.md`。本文是一次性的核实快照，
> 用途是回答「那两份文档说的到底哪些是真的」。

## 核实方法与基线

- **基线**：`main @ 2646b1d`（2026-08-15），`go build ./...` 通过，`go test -race ./...` 32 个包全绿，
  `gofmt`/`go vet`/`shellcheck` 干净。
- **判据**：只采信 `grep` / `wc -l` / `go list` / `go test` 的实测结果。两份文档自己的批注（含 v1 的 opus-5 批注、
  v2 的第三轮批注）**一律不作为前提**，只当线索回查。
- **编号**：沿用两份文档自己的编号。v1 用 `D#`（第一份 review 提出）/ `R#`（第二份反馈提出）/ `N#`（复审新发现）/
  `L#`（远期池）；v2 用 `P0-A` ~ `P3-F` 与 `ISSUE-P*`。v1 的 N 编号跳过了 N5 与 N10（原文即无）。
- **覆盖范围**：v1 的 D1–D7 + D5.2a + R1–R6 + N1–N15 + L0–L5，v2 的 Part 7 全 13 条 + Part 8 全 12 条 + 8.6 全 7 条，
  以及 `KNOWN_ISSUES` §1 的 13 条现存待定项。

## 状态图例

| 标记 | 含义 |
| :---: | :--- |
| ✅ | **已完成**：源码可验证地落地，且有测试守护 |
| 🟡 | **部分完成**：主体落地，但有明确的剩余缺口 |
| ❌ | **未完成**：问题今天仍然成立，且未被处理 |
| 🚫 | **前提不成立 / 已失效**：描述与源码不符，或问题已被别的改动顺带消解 |
| 🔒 | **明确不修**：经论证的刻意取舍，已写入 `KNOWN_ISSUES` §2 |
| 📋 | **已登记待定**：成立但主动搁置，已在 `KNOWN_ISSUES` §1 有条目 |

---

## 汇总

| 分组 | ✅ | 🟡 | ❌ | 🚫 | 🔒 | 📋 | 小计 |
| :--- | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
| 1 正确性与数据一致性 | 9 | 0 | 0 | 0 | 0 | 2 | 11 |
| 2 架构守卫（archtest） | 10 | 0 | 0 | 1 | 2 | 2 | 15 |
| 3 包边界与领域归属 | 5 | 1 | 0 | 0 | 3 | 0 | 9 |
| 4 重复实现收敛 | 5 | 0 | 0 | 1 | 0 | 0 | 6 |
| 5 大文件与大函数 | 3 | 0 | 0 | 1 | 0 | 2 | 6 |
| 6 文档与注释一致性 | 7 | 0 | 0 | 0 | 1 | 0 | 8 |
| 7 性能与运行时 | 0 | 0 | 0 | 1 | 1 | 5 | 7 |
| 8 展示口径与可观测性 | 0 | 0 | 0 | 0 | 0 | 5 | 5 |
| 9 明确否决 | 1 | 0 | 0 | 0 | 5 | 0 | 6 |
| **合计** | **40** | **1** | **0** | **4** | **12** | **16** | **73** |

**一句话结论**：两份文档提出的问题里，**没有一条是「成立且无人处理」的**（❌ 列为 0）。40 条已落地并有守护，
16 条经评估主动搁置并在 `KNOWN_ISSUES` §1 登记，12 条判定为刻意取舍写入 §2，4 条前提本身不成立。
唯一的 🟡 是 `D5.2a`——它的现象部分消解了，剩下的那条依赖是刻意保留的。

---

## 1. 正确性与数据一致性

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| N2 / ISSUE-P0-01 | v1 B0 / v2 | `tokens` 部分退化时 report 渲染精确数字而 router 收字节估算，静默发散 | ✅ |
| N12 | v1 B0 落地 | 多 API key 故障转移时请求级指标翻倍累加 | ✅ |
| N13 | v1 B0 落地 | 「嗅到就精确、没嗅到按字节估算」有三份独立实现 | ✅ |
| N7 / ISSUE-P0-03 | v1 B1 / v2 | 热路径字节改写无 fuzz 保护 | ✅ |
| ISSUE-P0-02 | v2 | 同 N12 | ✅ |
| P0-A | v2 Part 8 | `metric: cost` 混合定价端点静默低估 | ✅ |
| — | v1 B3 收尾 | `cost` 假零：usage 未嗅到时渲染 $0.0000 | ✅ |
| — | v2 追加 | `cost` 假 UNKNOWN：全失败窗口渲染 `-` 而 router 收了 $0.00 | ✅ |
| — | v2 追加 | `model_multipliers` 每次计费向上取整（最高 +100% 高估） | ✅ |
| KI §1.3 | KNOWN_ISSUES | 客户端流中途断开与正常成功在审计日志中不可区分 | 📋 |
| KI §1.6 | KNOWN_ISSUES | §2 成本表 $ 列与 Token 列口径不一致 | 📋 |

### 核实细节

**N2 / ISSUE-P0-01 ✅** — `cmd/vmr/quota_parity_test.go` 实测 5 个差分测试：`TestQuotaParity_RequestsMetric_ReportMatchesRouter`
（222）、`_RequestsMetric_NonIntegerMultiplier`（275）、`_TokensMetric_ReportMatchesRouter`（371）、
`_TokensMetric_NonIntegerMultiplier`（413）、`_CostMetric_ReportMatchesRouter`（470）。v1 只要求 tokens + cost 两组，
实际落地 5 组。

**N13 ✅** — 三份实现已收敛为一份导出入口 `router.TokenCounters`（`internal/router/quota.go:168`）。
`internal/replay/replay.go:257` 与 `internal/router/quota.go:145` 都调用它，`quota_parity_test.go:136` 也驱动它——
**测试驱动的是路由侧真正的导出入口，不是复述公式**，这正是 CLAUDE.md 那条「差分测试必须调用路由自己的导出入口」不变量。

**N12 / ISSUE-P0-02 ✅** — `internal/report/aggregate.go:308` 的 `reqAttributed` 一次性标记存在，
`:323` 的守卫是 `if a.Endpoint == rc.endpoint && !reqAttributed`。根因（`core.EndpointLabel` 是
`protocol:provider:model`，不含 key 分量）与修法都核实无误。

**N7 / ISSUE-P0-03 ✅** — `internal/jsonscan/` 实测 6 个 fuzz 目标（v1/v2 都只说 4 个）：`FuzzTopLevelValues`、
`FuzzWalkArrayElements`、`FuzzRewriteModel`、`FuzzRewriteStream`、`FuzzRewriteRoles`、`FuzzRewriteInputRoles`。
⚠️ **两份文档都漏记了一件事**：`a4cd787` 与 `b4247a1` 事后又关闭了整个导出面的**敌意索引 panic 类**（负索引解引用、
越界窗口）。fuzz 覆盖的是「合法索引 + 任意字节」，**参数本身越界是另一条当时没被覆盖的通道**。

**P0-A ✅（本轮落地）** — 现象核实成立：`internal/report/cost.go` 的 `accumulateCost` 在
`pricingSrc.RateFor` 返回 `!ok` 时直接 return，该记录完全不计入任何桶；而 `providerquota.go` 的 `costAnyPriced`
是全有全无的，只在**全部**端点未定价时渲染 `-`。混合时给出偏低的精确金额。

但 **v2 把它评为 P0 不成立**：`internal/config/pricing.go:360-381` 在加载期强制 `metric: cost` 账户的每个
`models:` 模型必须完整定价（`pricing.Resolve` 失败或 `pricing.Complete` 不全 → config error），所以 v2 描述的
「账户下既有已定价端点又有价表未收录的冷门模型端点」这种**稳态配置根本启动不了**。真实触发条件窄得多：
审计日志比 config 更旧（模型改名/移除），或旧格式 `/` 分隔标签。

**v2 给的修法也是错的**：它说「复用 `CostEstimateEst` 与 `window_estimated_pct` 机制再接一个维度」——那两个记的是
**降级估算**（usage 没嗅到），这里是**完全无费率**，是正交的量。已落地的修法是新增 `WindowUnpricedPct` +
`◇` 标记，**以请求数而非金额计**：没有费率正是它缺失的原因，任何金额都是编造。四个测试含负向验证。

---

## 2. 架构守卫（archtest）

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| N4 | v1 B0 | `archtest` 无函数长度预算（`buildInternal` 625 行畅通） | ✅ |
| N6 | v1 B0 | `cmd/vmr/` 无任何行数预算 | ✅ |
| N8 | v1 B3 | `story ⊀ report` 注释描述了一个从未落地的计划 | ✅ |
| N11 / ISSUE-P3-01 | v1 B6 / v2 | 文档漂移无可执行守卫 | ✅ |
| N14 | v1 B6 落地 | 守卫的符号正则漏掉 `.Field` 尾缀，本批新写的漂移全部逃逸 | ✅ |
| N15 | v1 B6 落地 | 负向测试从不调用被测逻辑，恒绿 | ✅ |
| P1-A / N16 | v2 Part 8 | 文件行数守卫仍是白名单，11 个 ≥400 行文件裸奔 | ✅ |
| P2-B / N19 | v2 Part 8 | `internal/audit` 三文件无行数预算 | ✅ |
| P3-C / N20 | v2 Part 8 | `diagnose`/`replay` 无文件预算 | ✅ |
| — | 本轮新发现 | `func_sizes_test.go` 引用的 `fileLineLimits` 已改名，成悬空符号 | ✅ |
| P1-B / N17 | v2 Part 8 | 当前状态类文档落在文档守卫扫描范围外 | 🔒 |
| P3-E / N21 | v2 Part 8 | `internal/probe` 未登记进 `zeroInternalDepPackages` | 🔒 |
| P3-F / N23 / KI §1.7 | v2 / KNOWN_ISSUES | 函数豁免键无法区分同文件重名方法 | 📋 |
| P3-D | v2 Part 8 | `session.go`/`cmd_story.go`/`compare.go` 观察项 | 📋 |
| L3 | v1 远期池 | `archtest` 增加圈复杂度检查 | 🚫 |

### 核实细节

**N4 ✅** — `internal/archtest/func_sizes_test.go` 存在，`defaultFuncLineLimit = 120`，
**采用「全局默认 + 豁免表」而非白名单**。`buildInternal` 实测 24 行（v1 时是 625），`aggregate.go` 503 行。

**N6 ✅** — `file_sizes_test.go` 实测登记 `cmd_story.go` 850、`cmd_check.go` 610、`cmd_report.go` 500、
`cmd_status.go` 370，比 v1 要求的三个多一个。

**N8 ✅** — `import_boundaries_test.go` 实测 `report` 与 `story` 的黑名单**双向都已强制**：
`"vmr/internal/report"` 条目里有 `"vmr/internal/story"`，反向亦然。注释里明写「Only story's direction was ever
enforced」，说明这条是后补的——v1 的 N8 描述准确。

**N11 / N14 / N15 ✅** — `doc_refs_test.go` 实测有 `checkDocRefs`（纯函数，73 行）、
`TestArchitecture_DocReferences`（225）与 `_Negative`（262）。N14 的正则修复可见：
`reSymbol` 现在是 ``"`([a-z][a-zA-Z0-9]*)\\.([A-Z][a-zA-Z0-9_]*)[^`]*`"``——锚定开引号、允许调用与选择器链。
N15 的修复也在：负向测试驱动 `checkDocRefs` 本身（10 组坏引用 + 6 组必须沉默的好引用）。
⚠️ **v1/v2 都写错了函数名**：真实名字是 `TestArchitecture_DocReferences`，
两份文档反复引用的 `TestArchitecture_ClaudeMdReferences` **从不存在**。

**P1-A / N16 ✅（本轮落地）** — v2 的论证核实成立且有力：`func_sizes_test.go` 早已反转为
「全局默认 + 豁免」，它自己的注释就论证了白名单为什么不行，而文件侧没跟上。后果不对称——新写的 900 行文件落地是绿的，
已登记文件多 1 行就红。

但 **v2 的落地建议是错的**：它说「超过默认值的 6 个按现状 +15% 首次登记」。实测生产文件分布
（169 个文件，p50 131 / p90 503 / p95 637）显示，默认取 700 时**所有 ≥700 的文件都已在原表里，一条首次登记都不需要**。
已按此落地：`defaultFileLineLimit = 700` + `fileLineExemptions`（豁免可双向覆盖，多数条目比 700 更紧），
负向验证通过（800 行临时文件让测试变红，删除后恢复）。P2-B 与 P3-C 随之自动闭环。

**P1-B / N17 🔒** — 现象核实成立：`docHasSymbols`/`docHasInternalPaths` 白名单有意排除 review 报告。
但 v2 的**方案一**（把「当前状态类」文档纳入守卫）判定不做：谁来判定一份 review 是「当前状态类」？按文件名前缀判定
又回到白名单的老毛病，且 review 报告天然会讨论「建议新增的 XXX 函数」，纳入守卫等于逼作者改写论证。
**采纳 v2 自己提的方案二——用定位而非机制解决**：v2 文档顶部已加历史记录声明，权威清单只留 `KNOWN_ISSUES`。
已写入 §2.5。

**P3-E / N21 🔒** — `internal/probe` 实测确为零内部依赖。但 **v2 误解了那张表的语义**：
`zeroInternalDepPackages` 的注释写的是「guards the leaf packages every other package is free to import without a
boundary concern — that promise only holds if they never grow an internal dependency」——它是**承诺**，不是**现状快照**。
`probe` 的包注释明写它独立成包是为避免 `diagnose`→`router` 的 import cycle，是路由半区的协议原语，未来 import `core`
完全合理。登记等于给一个从未做出的承诺加锁。同理不登记 `rundir`/`buildinfo`。已写入 §2.4。

**L3 🚫** — 圈复杂度检查未实现（`grep -rn "cyclomatic\|complexity" internal/archtest/` 唯一命中是注释里的
"complexity" 一词）。v1 自己的裁决是「在函数长度预算跑满一个季度前不加第二个指标，一次加一个守卫」——
**这不是遗漏，是按计划没做**。

---

## 3. 包边界与领域归属

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| D2 | v1 6.3-2 / B1 | `classify.go` 70% 是通用 JSON 字节引擎，寄居领域包 | ✅ |
| R1 | v1 7.5 / B1 | `fingerprint.go` 的通用词法应一并迁出 | ✅ |
| D4 | v1 6.1-1 / B5 | `core` 里 `WriteJSON`/`WriteError`/`MarshalNoEscape` 归属错位 + 缺准入规则 | ✅ |
| R6 | v1 7.5 / B7 | `respnorm` 从 router 剥离，真实收益是 fuzz 与边界 | ✅ |
| — | v2 Layer 8 | `server` 依赖 `jsonscan` 替代对 `adapter` 底层工具的依赖 | ✅ |
| D5.2a | v1 5.2-1 / B1 | `server → adapter` 的「奇怪依赖」 | 🟡 |
| ISSUE-P1-01 | v2 7.3 | `config` 倒挂耦合用例层三层费率解析 | 🔒 |
| ISSUE-P2-03 | v2 7.4 | `core.go` 517 行应按领域拆子文件 | 🔒 |
| — | v2 8.6 | `chatmsg.ReassembleSSE` 与 `respnorm` SSE 状态机合并 | 🔒 |

### 核实细节

**D2 / R1 ✅** — `internal/jsonscan/` 实测 4 个生产文件（`jsonscan.go`/`scan.go`/`rewrite.go`/`walk.go`），
`go list -f '{{.Imports}}'` 确认**零内部依赖**。R1 的边界判据（「需要知道任何一个具体字段名或角色名，就不属于 jsonscan」）
也守住了：`fingerprint.go` 留下的 5 个函数全部知道具体角色名或字段名——`SessionFingerprint`、
`responsesSessionFingerprint`、`leadingSystemAndFirstOther`、`leadingSystemAndFirstOtherResponses`、`TopLevelProbe`。

**D4 ✅** — 三处迁移全部核实：`MarshalNoEscape` → `internal/jsonscan/jsonscan.go:28`，
`WriteJSON`/`WriteError` → `internal/router/httpjson.go:16/25`，`FilterClientHeaders` → `internal/router/clientheaders.go:56`。
准入规则**已写进 `internal/core` 的包注释**（不是「应确立」，是已落地），并逐条解释了为什么
`FilterClientHeaders` 看起来该留在 `core` 却不该留。

**R6 ✅** — `internal/respnorm/` 实测 2 个生产文件，`FuzzStream` 存在（`fuzz_test.go:130`）。
**直接 import 只有 `chatmsg` 一个内部包**（`fmtutil` 是 `chatmsg` 的传递依赖，不是 respnorm 自己的）——
v2 在 5.3 的说法准确。用量嗅探留在包内的取舍写在包注释里。
⚠️ **两份文档都漏记**：`e35a5e3` 记录 B7 的 fuzz 在落地当天就抓到一个真 `[DONE]` 重复 bug——
这是「剥离出来是为了能 fuzz」这个论证最有力的实证。

**D5.2a 🟡（唯一的部分完成）** — `internal/server/server.go:16` **今天仍然 import `internal/adapter`**。
但现象已部分消解：底层词法依赖确实换掉了（`facts.go:20` import `jsonscan`，`:119` 调用 `jsonscan.IndexUnescapedQuote`），
剩下的唯一用途是 `server.go:165` 的 `adapter.TopLevelProbe(body)`。

**而这条依赖是刻意保留的**：B1 明确把 `TopLevelProbe` 留在 `adapter`（它知道 `model`/`stream`/`tools` 这些协议字段名）。
server 需要在一次结构扫描里同时拿到 model/stream/hasTools，调用协议层的探针是正确做法，不是「奇怪依赖」。
**判定：v1 的 D5.2a 前提部分不成立，剩余状态是合理终态，无需处理。**

**ISSUE-P1-01 🔒** — v2 自己在第三轮撤销得对。`internal/config` 确实 import `internal/pricing`、
在 `validate()` 阶段跑完三层解析（`config/pricing.go:277` 的 `resolvePricing`），现象没错。但这是
`docs/VirtualModelRouter_Design_v4_Quota.md` 决策表明文选定的方案，v2 提议的「后置到 `router.BuildSnapshot`」
正是同一张表已否决的备选（理由：两份实现容易漂移）。后置会摧毁「`metric: cost` 费率不齐 = 加载期错误」这条硬要求。
已按 v2 自己的建议补进 `KNOWN_ISSUES` §2.2。

---

## 4. 重复实现收敛

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| D1 | v1 6.1-3 / B2 | Agent 方言（OpenClaw 正则/NO_REPLY）在 report 与 story 逐字复制 | ✅ |
| R2 | v1 7.5 / B2 | `chatIDRe` 属 OpenClaw 专属方言，应纳入方言层 | ✅ |
| N1 / ISSUE-P1-02 | v1 B3 / v2 | 整套会话/任务切分算法双实现（6 对同名函数 + 边界判定） | ✅ |
| R3 / ISSUE-P1-03 | v1 B4 / v2 | 6 个 Row 类型共享字段核却无共享类型 → 7 个累加闭包约 290 行 | ✅ |
| N9 / R4 / ISSUE-P2-04 | v1 B5 / v2 | `fmtTokens` 四份实现；CLAUDE.md 声称的 `fmtutil.FmtTokens` 不存在 | ✅ |
| R4（后半） | v1 7.5 | Token **估算**逻辑也散落重复 | 🚫 |

### 核实细节

**D1 / R2 / N1 ✅** — `internal/taskseg/` 实测 4 个生产文件（422 行）。`segment.go` 持有切分算法的唯一实现：
`IndexRealUsers`（48）、`ManifestKeySet`（67）、`HasNewInstruction`（87）、`LastInstruction`（105）、
`FirstInstruction`（125）、`IsNewTask`（144）、`TaskTitle`（155）、`ResponseSummary`（164）、`Preview`（189）。
R2 的 `chatIDRe` 已进入方言层——`Profile` 接口有 `ChatID(msgs []chatmsg.Message) string` 方法
（`taskseg.go:56`），`openclaw.go:103` 是 OpenClaw 实现，`generic.go:38` 返回空串。

**R3 / ISSUE-P1-03 ✅** — `TrafficStats` 定义在 `rows.go:110`，被 **5 个类型内嵌**（141/198/335/355/374 行处）。
`buildInternal` 实测 **24 行**（v1 时 625），`aggregate.go` 503 行、预算已从 1000 **收紧到 600**（不是保持原值）。
⚠️ v1 的 R3 批注说「6 个 Row 类型」，实际内嵌的是 5 个——`EndpointRow` 刻意不内嵌
（它的 `Requests`/`TokensIn` 等是 `omitempty` 而这 5 个不是，且要区分 attempt 级与 request 级，
正是 N12 那个 bug 的根源）。这条区分在 `rows.go` 的注释里写着。

**N9 / R4 / ISSUE-P2-04 ✅** — `internal/fmtutil` 实测导出 7 个函数：`FmtBytes`、`FmtSeconds`、`FmtPercent`、
**`FmtTokens`**、**`FmtTokensPlain`**、**`FmtTokensCompact`**、`CapStr`。
`grep -rn "func fmtTokens" --include='*.go' .` **零命中**——四份私有实现已全部消失。
v1 建议两个明确命名的函数，实际落地三个（多一个 Markdown 对齐版），比提议更细。

**R4 后半 🚫** — v1 的 opus-5 批注已判定「Token 估算那半是错的」（估算逻辑是复用不是复制）。
本轮复核确认该判定成立：`core.EstimateTextTokens`/`EstimateTokensFromCounts` 是唯一实现，其余都是调用。

---

## 5. 大文件与大函数

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| D3 | v1 6.1-2 / B4 | `buildInternal` 单函数 625 行 | ✅ |
| — | v1 B0 连带 | `aggregate.go` 撞 1000 行预算，按规则拆出 `tokenest.go` 而非抬高数字 | ✅ |
| — | 本轮连带 | `buildProviderQuotaRows` 超函数预算，拆出 `accumulateQuotaWindow` | ✅ |
| 异味 2 | v1 6.1-2 | 分析层大文件逼近容量上限 | 🚫 |
| P2-A / ISSUE-P2-01 / L2 / KI §1.4 | v2 / v1 远期池 | `detail.go` 1047/1150，四项正交职责 | 📋 |
| ISSUE-P2-02 / P3-D | v2 | `session.go` 特征提取交织 | 📋 |

### 核实细节

**D3 ✅ 及两次连带** — 除 `buildInternal` 24 行外，本轮还发生了一次同类事件：给 `providerquota.go` 加
`WindowUnpricedPct` 时，`buildProviderQuotaRows` 涨到 184 行、超出 155 的豁免，**守卫报警**。
按守卫自己的失败信息（"shorten it, don't raise the number"）拆出 `accumulateQuotaWindow`，
两个函数回落到 120 默认之下，那条 155 的豁免随之删除。**这是护栏第三次在落地当天先拦作者一次。**

**异味 2 🚫** — v1 原文的行数与预算都写错了（`detail.go` 说 1063 实为当时 1047，1150 是预算不是行数）。
v1 自己的 opus-5 批注已标注「数据错误，且量错了对象」。前提不成立。

**P2-A 📋** — 现象核实成立：`detail.go` 实测 **1047 行 / 1150 预算 = 91%**，是全项目预算占用率最高的文件；
`grep "^func "` 显示四段清晰可分的职责（worker pool 调度 44-268、字段提取 271-408、Markdown 渲染 410-828、
diffing 830-1047）。

**但主动搁置**：它已被 `archtest` 纳管，超预算会红——**这是一个会自己举手的问题**。项目一贯做法是
「预算报警了再拆，拆完按实测 +15~20% 重新登记」，提前拆是替一个会自己举手的问题排队。
⚠️ v2 的验收标准「各子文件 < 400 行」**没有依据**，那个数字凭空而来。已按项目惯例改写并登记 `KNOWN_ISSUES` §1.4。

**ISSUE-P2-02 📋** — `session.go` 实测 **834 行 / 1100 预算 = 76%**（B3 之后从 993 降下来，本轮注释精简后又降到 834）。
比 `detail.go` 宽松得多，**在它之前动没有理由**。

---

## 6. 文档与注释一致性

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| N3 | v1 B6 | `docs/VirtualModelRouter_Design_v4_Strategy.md` 被四处引用但不存在 | ✅ |
| D6/D7 | v1 6.2/5.1 | 目标架构图箭头画反、CA 映射表四处错配 | ✅ |
| — | v1 B6 | CLAUDE.md 从 192 行收敛到 156 行 | ✅ |
| P3-B / N18 | v2 Part 8 | `core` 包注释仍称「两个入口协议」，实为三个 | ✅ |
| — | 本轮新发现 | `func_sizes_test.go` 注释称文件预算是「19 个文件的白名单」，实为 32 条 | ✅ |
| — | 本轮新发现 | 注释里 7 处批次号引用（`Part 8 batch B4/B5/B7`）已无意义 | ✅ |
| — | 本轮新发现 | `server.go` 与 `facts.go` 重复讲同一个 image 检测事故 | ✅ |
| — | v2 自身 | v2 引用了从不存在的 `TestArchitecture_ClaudeMdReferences`，且把 4 个已落地批次写成待办 | 🔒 |

### 核实细节

**N3 ✅** — `docs/VirtualModelRouter_Design_v4_Strategy.md` 实测存在（15525 字节）。

**P3-B / N18 ✅（本轮落地）** — 核实成立：`core.go` 的 `CanonicalRequest` 注释原文写
"Both supported ingress protocols (OpenAI chat completions, Anthropic messages)"，而实际有三个入口协议。
已改为 "All three supported ingress protocols (…, OpenAI responses)"。
v2 判断「不建议为它扩展守卫（Go 注释不在 Markdown 守卫范围内，成本远超收益）」——认同，未扩展。

**本轮的三处新发现 ✅** — 都是这次注释精简扫描时才浮现的，两份文档都没提：
1. `func_sizes_test.go` 的注释说文件预算是「a hand-kept list of 19 files」，实测 32 条——注释自己漂移了。
   （P1-A 落地后这句话整体重写。）
2. 该文件还引用 `fileLineLimits`，而 P1-A 已把它改名为 `fileLineExemptions`——**悬空符号**，已修。
3. `server.go:189` 与 `facts.go:35` 各写了一遍同一个 image 检测事故的完整论证，已改为前者指向后者。

**v2 自身的漂移 🔒** — 这是 N17 的活例子：v2 自称「为后续施工提供权威依据」，却引用了一个从不存在的测试名，
并把 B4–B8 五个已落地批次写成待办，而 CI 全绿——因为守卫有意不扫 review 报告。
**处置是定位而非机制**：v2 文档顶部已加⚠️历史记录声明，逐条列出结项处置。

---

## 7. 性能与运行时

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| 异味 4 / D5 / L1 | v1 6.1-4 / 远期池 | `chatmsg` 的 `map[string]any` 带来 GC 压力 | 🚫 / 📋 |
| — | v2 6.4-① | 流式 Context 断链快速中断 + 客户端取消时停止计费 | 🔒 |
| — | v2 6.4-② | 多窗口滑动限流（Rolling Window） | 📋 |
| KI §1.1 | KNOWN_ISSUES | `vmr report` 多文件输入的两趟扫描开销 | 📋 |
| KI §1.2 | KNOWN_ISSUES | `vmr report` 全内存聚合的记录量上限 | 📋 |
| KI §1.9 | KNOWN_ISSUES | 审计落盘的 `write` syscall 在全局锁内 | 📋 |
| KI §1.11 | KNOWN_ISSUES | 图片降采样磁盘缓存无容量上限 | 📋 |

### 核实细节

**异味 4 / D5 🚫（热路径）+ 📋（离线）** — v1/v2 都断言「在线路由热路径上 `map[string]any` 出现频次为 0」，
v2 更具体地说「`router`/`adapter`/`audit` 三个包的生产代码里出现次数确为 0」。

**实测需要精确化**：`adapter` 与 `audit` 确为 0，但 `router` 有 1 处、`server` 有 8 处。逐一检查位置后
**结论仍然成立**——没有一处在转发热路径上：
- `router/httpjson.go:26` — `WriteError` 的错误响应体构造，只在错误路径
- `server/admin.go` 7 处 — `/admin/status`，回环管理面
- `server/server.go:302` — `/v1/models` 列表端点

`chatmsg` 实测 43 处，全在离线解析路径。v1 的裁决（先在真实审计日志上跑 benchmark 再谈）仍然有效。

**v2 6.4-① 🔒** — **前提不成立**，且提议与一条写明理由的决策冲突。实测：
- `router.go:375` 的 `ad.BuildRequest(r.Context(), ep, creq)` 把客户端 context 直接挂到上游请求上——
  客户端一断，`http.Transport` 就中止上游连接，不需要在 `copyFlush` 里再 select 一次 `Done()`
- `router.go:530` 用 `copyErr != nil && r.Context().Err() == nil` 把「上游死了」和「客户端自己走了」分开
- 「停止计费」的提议直接冲突：`router.go:534-538` 注释逐字写着 *"Charged here regardless of copyErr — a truncated
  response still consumed whatever tokens actually reached the client"*。上游已生成的 token 不会因客户端挂断而退回，
  照收才是与厂商账单对齐的口径。**按 v2 的提议改会让 `vmr report` 系统性低估消耗**——正是 B0 花整批修掉的那类发散。

**KI §1.1 / §1.2 📋（本轮补核，用户提出）** — 两条都成立：
- §1.1：`build_cached.go:16` 注释确认两趟读取（"AnalyzeSessions for grouping (one read), then does its own pass
  (second read)"）
- §1.2：`rows.go` 实测 5 处 `[]int64` 原始样本切片常驻（`durs` 130、`ttfts/streamMS` 189/214、
  `durs,ttfts,streamMS,inToks,outToks` 328、`inToks,outToks` 348），用于算真实百分位数

**KI §1.9 📋（本轮补核）** — 成立。`internal/audit/audit.go:548` `l.mu.Lock()` + `:549` `defer l.mu.Unlock()`，
而 `:571` 的 `l.f.Write(buf.Bytes())` **在该锁区间内**。JSON 编码已通过 `sync.Pool` 移出锁外，但 syscall 没有。
⚠️ 顺带修正 v1/v2 的一处措辞：两份文档都说 `sync.Pool` "消除了热路径锁竞争"——准确说法是**大幅降低**，不是消除。

**KI §1.11 📋（本轮补核）** — 成立。`internal/imgprep/cache.go:96` 的 `maybeSweepCache(dir, ttlDays, now)` +
`:110` 的 `sweepCacheDir` 按 **mtime 与 TTL** 清理，全文无 `MaxBytes`/`totalSize`/`capacity` 类总量控制。

---

## 8. 展示口径与可观测性

> 本组只收展示口径本身。`cost` 假零与假 UNKNOWN 两条虽然也出现在报表上，但性质是错误的数字，已计入第 1 组。

| 编号 | 来源 | 主张 | 状态 |
| :--- | :--- | :--- | :---: |
| P2-C / N22 / KI §1.6 | v2 Part 8 | `Row`/`ClientRow` 缺 `CostEstimateEst` 字段 | 📋 |
| KI §1.5 | 本轮新增 | §2.5 表格的标记符号已达四个 | 📋 |
| KI §1.8 | KNOWN_ISSUES | 探针请求绕过审计日志 | 📋 |
| KI §1.10 | KNOWN_ISSUES | `/admin/status` 未暴露 `config.Check()` 的操作性告警 | 📋 |
| KI §1.12 | KNOWN_ISSUES | 额度燃尽看板未交付 | 📋 |

### 核实细节

**P2-C / N22 📋** — 核实成立：`rows.go` 实测只有 `EndpointRow` 有 `CostEstimateEst`（:341），
`Row`（:201）、`ClientRow`（:361）都只有 `CostEstimate` 没有 `Est` 分量。
`cost.go` 的 `accumulateCost` 也印证——只在 `ea`（EndpointRow）上累加 `CostEstimateEst`。

**影响面比 v2 描述的更广**：`section_cost.go` 的四张表（byDate/byModel/byEndpoint/byClient）**全部**是
`TokensInFresh | TokensOut | CostEstimate` 并列，而 `TokensInFresh` 只统计嗅探到 usage 的记录、`CostEstimate` 含估算成分。
连 byEndpoint 表也没渲染它已有的 `CostEstimateEst`。

**主动搁置**：这正是 `KNOWN_ISSUES` §1.6 已记录的方案②，该条目自己写明「会再次改动 `vmr-report.json` 形状，
属于展示口径决策而非缺陷修复」。不重复立项。

**KI §1.5 📋（本轮新增）** — `section_provider.go` 实测确有四个按需渲染的标记：
`⭐`（超额度，:112）、`‡`（配置变更，:123）、`†`（无时间交集，:146）、`◇`（部分流量未计入，:136），
各配一条门控脚注。信息都必要，但四个符号叠在一张表上可能已到「标记多到没人看脚注」的临界点。
已登记为待定，触发条件是真实报表读起来觉得吵。

**KI §1.8 / §1.10 / §1.12 📋（本轮补核）** — 三条全部成立：
- §1.8：`grep -n "audit\." internal/router/probe.go` **零命中**，探活请求确实不写 `audit.Record`
- §1.10：`grep -n "Check()\|Issues\|Warn" internal/server/admin.go` **零命中**，`/admin/status` 确实未暴露告警集合
- §1.12：`grep -rn "burndown\|forecast\|projection" internal/report/` **零命中**，燃尽曲线与预测看板确未实现

---

## 9. 明确否决（刻意取舍）

以下五条经论证判定**不做**，全部已在 `KNOWN_ISSUES` §2 有条目，不需要重新论证；第六条是原提议被修正后落地。

| 编号 | 来源 | 主张 | `KNOWN_ISSUES` 位置 |
| :--- | :--- | :--- | :--- |
| L0 | v1 5.1 / v2 8.6 | 向 Clean Architecture 四环靠拢的整体重构 | §2（多条支撑） |
| L4 | v1 7.5-R1 / v2 8.6 | `imgprep` 的 `map[string]json.RawMessage` 与字节扫描统一 | §2.4 |
| L5 / R5（合并那半） | v1 7.5 / v2 8.6 | `i18n` 26 个微文件合并为 3–4 个 | §2.4 |
| R5 / P3-A（样板那半） | v1 B8 / v2 Part 8 | `i18n` 工厂样板改写为 `map[Lang]T` + 泛型 `pick` | §2.4 |
| — | v1 6.3-1 | 抽 `internal/agentprofile`（原提议的包名与接口签名） | ✅ 以 `taskseg` 的修正形态落地 |
| — | v2 8.6 | `chatmsg.ReassembleSSE` 与 `respnorm` SSE 状态机合并 | §2.4 |

### 核实细节

**R5 / P3-A（i18n 样板）🔒 — 本轮从「可选」升级为「明确不做」** — 现象核实成立：
`internal/i18n/` 实测 26 个生产文件，全部是 `type XxxText struct` + `func Xxx(lang Lang)` 内 `if lang == ZH` 分支的形状，
`grep "func pick\|\[T any\]\|map\[Lang\]"` **零命中**，泛型取值函数从未落地。

**但收益辨析后判定为负**：改成 `map[Lang]XxxText` + `pick[T]` 只消掉每个文件 **2 行**的 `if lang == ZH` 分支——
真正占体量的 struct 定义与两份字段赋值一行都省不掉。26 个文件全改，换约 50 行，还要新引入一个泛型 helper
和「key 缺失怎么办」的语言回退问题。
v1 自己标注它「做与不做都对」，v2 保留为 P3。**本轮判定更强：不做**，已写入 §2.4。

**L0（CA 整体重构）🔒** — v1 的论证是充分的：要把横跨环边界的包「归位」，得为满足图示而拆包插接口，
代价是新的包边界和一层不解决任何真实问题的间接性。
⚠️ 本轮补充一条 v2 丢掉的证据：`internal/config` **import `internal/adapter`**（`config.go:16` 区域，
校验期需要知道协议注册表里有哪些 adapter）。按 v2 自己的 CA 映射表（`config` 在 Frameworks & Drivers、
`adapter` 在 Interface Adapters），这就是一条「外环依赖内环」的合法边——**恰恰是「CA 不是这个项目合适透镜」的证据之一**。
v2 在收敛篇幅时把论证删了、结论留了。

---

## 附录 A：两份文档自身的准确性

核实过程中发现的、**文档本身的错误**（与「问题是否成立」无关，但影响它们能否被当作依据）：

| 类型 | 实例 |
| :--- | :--- |
| **符号不存在** | `TestArchitecture_ClaudeMdReferences`（v1/v2 反复引用，从不存在；真名 `TestArchitecture_DocReferences`） |
| **规模数字失准** | v2 的 30 项规模声明中约 13 项错误：`fingerprint.go` 87 vs 实测 277（差 3 倍）、`report` 16 文件 vs 实测 33、`archtest` 2 文件 406 行 vs 实测 4 文件 890 行、`ctxgraph` 「8 文件 1800+ 行」vs 实测 9 文件 1408 行（两个数方向相反） |
| **状态失准** | v2 把 B4–B8 五个已落地批次写成待办（根因：转述 v1 文本而未重新测量） |
| **自相矛盾** | v2 在 Layer 0 说 `core.go` 拆子文件「不可包装为架构重构」，又在 ISSUE-P2-03 把它列为 P2 待修复 |
| **批次号张冠李戴** | v2 称 i18n 样板是「批次 B8」，而仓库里真实的 `Batch B8`（`4aefb00`）做的是 report 假 UNKNOWN 成本单元格 + taskseg 文本收缩，与 i18n 无关 |
| **落地建议错误** | P1-A 说「超过默认值的 6 个按现状 +15% 首次登记」，实测默认取 700 时零新增登记；P0-A 说「复用 `CostEstimateEst`/`window_estimated_pct` 机制」，而那两个记的是正交的量 |
| **前提不成立** | 流式 Context 断链（取消已传播）、`config`→`pricing` 后置（推翻已记录决策）、`probe` 登记零依赖表（误解表的语义） |

**这不是为了记账**。它支撑一条已写入 `KNOWN_ISSUES` §2.5 的处置：**review 报告一律是历史记录，
权威的当前状态清单只有 `docs/KNOWN_ISSUES_sonnet-5.md` 一份**——用定位而非新机制解决文档漂移。

## 附录 B：核实时守住的四条纪律

沿用 v1/v2 自己总结的三条，加本轮一条：

1. **不照抄任何 review 给出的行数**——本轮实测推翻了 v2 的 13 项规模声明。每一条验收先自己 `wc -l`。
2. **「重复」必须区分「复制」与「调用」**——一个包 import 另一个包的工具函数是复用，不是冗余。
3. **动手前先查 `KNOWN_ISSUES` §2「确定不修」与设计文档的决策表**——推翻是允许的，但必须先知道自己在推翻它。
4. **一条 review 结论的「待修复」状态，保质期只到下一次 `git log`**——任何声称「待修复」的条目，
   落地前先跑一次 `grep` 确认它今天还成立。成本 10 秒，收益是不做一次已经做过的重构。
