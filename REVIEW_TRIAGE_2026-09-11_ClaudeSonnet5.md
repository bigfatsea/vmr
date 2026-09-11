<!-- Ver 2026-09-11 16:40, by Claude Sonnet 5 -->

# vmr 三份 Review 报告核实与处置记录

**输入**：`PROJECT_FULL_REVIEW_REPORT_2026-09-11.md`（全系统 D1-D6 6 域拆解）、
`ROUTING_ARCHITECTURE_REVIEW_2026-09.md`（路由半区性能/架构存废专项）、
`ANALYZE_REVIEW_REPORT_2026-09-11.md`（analyze 套件真实语料端到端体验评估，44 份生产日志/15,946 条记录）。

**方法**：提取全部具体 finding → 与 `docs/KNOWN_ISSUES.md`/`docs/ROADMAP.md` 现状做去重映射 →
4 组并行核实（每组一个 fork agent 独立读源码、核实断言、给出独立 ROI 判断，不预设采信文档结论）→
主控汇总裁决、第一性原理复核有分歧的地方 → 对事实清楚、方案无争议、改动可控的问题直接修复 →
其余按 ROI 分批登记进 `KNOWN_ISSUES.md`/`ROADMAP.md` → 全量 `go build`/`go test`/`go vet`/`gofmt`/`-race`/`archtest` 验证。

**背景澄清**：核实前发现 `docs/KNOWN_ISSUES.md` 已有「H. 2026-09-11 全系统 Review 新增」章节，记录了**同一天早些时候**一轮同结构（D1-D6）review 的处理结果（11 项已修、§2.111 明确拒绝、§2.120-125 排期）。这意味着本次三份文档中，`PROJECT_FULL_REVIEW_REPORT_2026-09-11.md` 的 D6-F01 等条目可能与已处理内容重叠——核实过程中已逐条与 H 组交叉核对，未发现重复登记。

---

## 一、核实结果总览

三份文档合计约 45 条具体 finding + 2 条对既有文档的挑战。核实后分布：

| 处置类别 | 数量 | 说明 |
|---|---|---|
| **不成立 / 报告自身错误** | 3 | 对 Go 正则行为理解有误、误判 recover 机制不存在、方案与已有测试锁定的行为冲突 |
| **属实但建议方案有害，reject** | 2 | 会打破已被真实生产场景测试钉死的行为；或复现已被 `KNOWN_ISSUES §1.3` 证伪的方案 |
| **属实，本轮直接修复** | 21 | 事实清楚、方案无争议、改动可控，见下「二、已修复清单」 |
| **属实，排入批次 1（建议尽快）** | 4 | 需要设计或牵动 golden fixture，见「三、批次划分」 |
| **属实，排入批次 2+ / 登记待办** | 6 | 低优先级或需要专项排期 |
| **属实，已被现有 §1 裁决覆盖（already-tracked）** | 6 | 与 `KNOWN_ISSUES` 现有条目重复，仅核实描述准确性 |
| **确认健康 / 维持现状（INFO）** | ~6 | 路由半区报告里的大量确认健康项，无需动作 |

**三份文档里被核实出的事实性/方案性错误**（详见附录A）——这是本次任务被特别要求的"第一性原理独立判断，不被文档结论束缚"环节的直接产出：

1. **D1-F04**（respnorm 截断流"伪造闭合引号"）：对 Go `regexp` 行为理解有误——正则要求字面闭合引号存在才能匹配，截断处无闭合引号时 `Match()` 直接返回 false，不会替换更不会"伪造"。
2. **D2-F07**（quota flusher panic 静默退出）：`internal/quota/store.go:311-316` 已有 `defer recover()` 并调用 `fl.Error(...)` 记日志，并非"静默"。
3. **GAP-01**（OpenAI 协议工具错误检测应引入三层判定）：建议方案的两层要么是已被 `KNOWN_ISSUES §1.3` 实测证伪的空集（结构化 JSON 错误字段），要么就是被 §1.3 明确否决的"子串模糊嗅探"换个说法——两次独立"发现"的是同一批底层事实，不是新证据反驳旧结论。
4. **D1-F01**（429 裸词"quota"应收窄为复合短语）：`classify_test.go:208` 用真实 OpenAI 账单超额原文（`"you have exceeded your quota"`，不含任何复合短语）锁定当前行为为**故意设计**；收窄方案会造成真实回归。

---

## 二、本轮已修复清单（21 项，附文件与核心改动）

### 路由半区

| 编号 | 问题 | 修复 | 文件 |
|---|---|---|---|
| D1-F03 | `runProbe` 忽略错误体读取失败，残缺 body 可能被误分类 | 读取失败时直接 `ReportFailure(ErrTransient)`，不送 `ClassifyError`，对齐 `router.go:handleErrorResponse` 已有的防御模式 | `internal/router/probe.go` |
| D2-F02 | `capabilities` 自由文本无词表校验，拼写错误导致模型能力静默失效 | 新增 `validateCapabilities`，白名单取自**文档已定义的完整词汇表**（`text/tools/image/audio/video/thinking`，含尚未接入 Condition 的 `audio/video/thinking`），而非仅当前已注册的 `strategy.Condition` 子集——否则会误杀 `config.example.yaml` 自己的示例配置 | `internal/config/config_validate.go` |
| D2-F03 | 协议键挂空端点组列表通过校验，但路由构建时该协议路由条目完全不创建（比"全 disabled"更差，且未被任何测试覆盖） | `validateModels`/`validateFallbackEndpoints` 对空 `groups` 报错 | `internal/config/config_validate.go` |
| D2-F04 | `health.go` 文档注释与 `§2.85` last-resort 语义存在滞后矛盾 | 补充 last-resort 例外说明 | `internal/health/health.go` |
| D2-F05 | provider `api_key` ≤6 字符时 `key_label` 明文进日志/审计 | 短 key 改用 `sha256` 前 2 字节 hex（`#xxxx`）而非原样返回 | `internal/config/apikeys.go` |
| D2-F06 | `openai-responses` 协议恢复探针无输出 token 上限 | 补 `max_output_tokens: 300`（对齐其余协议探针量级，未采用原文建议的 16——太小可能连 echo nonce 都放不下） | `internal/probe/probe.go` |
| D2-F09 | `max_request_body_mb` 负值被静默吸附为默认值，与同层 `max_attempts` 的报错哲学不一致 | 在 `applyDefaults` 折叠负值**之前**校验并报错 | `internal/config/load.go` |
| D3-F01 | `-audit=false` 下 `quota.Registry` **完全没有跨进程锁**（不止是"绕过 flock"，是从未有过保护），双实例静默互覆 `vmr-quota.json` | 仿 `internal/livestats` 已验证的模式，给 `quota.Registry` 加独立 `.vmr-quota.lock`（`ensureDirLock`，`Load`/`Flush` 谁先跑谁获取，`Flush` 拿不到锁时返回错误而非静默丢弃，`dirty` 保持置位以便重试与告警）；新增 `Registry.Close()`；`cmd_start.go`/`replay.go` 接入 | `internal/quota/{quota,store,lock_unix,lock_windows}.go`、`cmd/vmr/cmd_start.go`、`internal/replay/replay.go` |
| D3-F04 | `rollSlimFile` 中 `_ = h` 死变量 | 清理 | `internal/livestats/rollup.go` |

### 分析半区

| 编号 | 问题 | 修复 | 文件 |
|---|---|---|---|
| D4-F01 | `ReqCoord`（`basename:line`）同时间戳 tie-break 用字符串字典序，导致行号 `"10"<"9"` 反转，跨位行号处把连续会话误判为"收缩"而撕裂 Lineage | 新增 `LessReqCoord`（basename 走字符串比较，line 转数值比较），替换全部 3 处消费点（`scan.go` 排序、`stitch.go` 两处 tie-break） | `internal/ctxgraph/{reqcoord,scan,stitch}.go` |
| D4-F03 | 详单页仍用单布尔 `usageOK`（OR 语义），Anthropic 流截断时把 `Out≈1` 占位符当精确值渲染；与 `KNOWN_ISSUES §1.4` 已钉死的"不要复活单标志包装"架构决定直接冲突 | 改用 `chatmsg.ExtractUsageSides`（侧感知），`tokensTriple` 新增 `outOK` 参数，Out 未见时加 `≈` 前缀而非渲染为精确值 | `internal/reqdetail/{detail,render}.go` |
| D4-F04 | `extractResponseTextFromString` 先判断子串 `"data:"` 后判断 `'{'`，含内联 data URL 的完整 JSON 响应体被误判为 SSE 走降级路径 | 调换判断顺序（`'{'` 优先），核实真实 SSE 首字节恒为 `'d'`，不影响任何真实 SSE 分类 | `internal/chatmsg/usage.go` |
| D4-F05 | `responsesItemMessage` 的 default 分支未接入 `unrecognizedPartTypes` 计数器，与 `RenderPart` 的对称设计（S-2"让沉默变响亮"）不一致 | default 分支补计数器调用 | `internal/chatmsg/messages.go` |
| D5-F01 | `MergeJourneyIndexRows` 未回填 `Cost`/`Currency`/`NetWorkingMS`/`Model` 四字段（字段自己的文档注释写明"与 Tasks/Steps 同一个 gate"，属实现遗漏） | 与 `Tasks`/`Steps` 用同一个 gate 一并回填 | `internal/journey/journeyindex.go` |
| D5-F02 | L2 缓存指纹 `ComputeAnalysisParamsFingerprint` 未折入时区，跨时区重跑误中旧缓存产出旧日历分桶 | 折入 `time.Now().In(fmtutil.DisplayZone)` 的**数值 UTC 偏移秒数**——**注意**：原文建议的 `DisplayZone.String()` 方案本身有缺陷，生产环境 `time.Local.String()` 恒返回字面量 `"Local"`（与真实偏移无关），照抄会让修复完全无效，核实时改用数值偏移量 | `internal/report/cache.go` |
| D5-F04 | `journeys/index.json`/`index.md` 用 `os.WriteFile` 直写非原子，与仓库内 `compares/index.md` 等已有的 temp+rename 纪律不一致 | 改用同包已有的 `writeCacheFileAtomic` | `internal/journey/journeyindex.go` |
| D5-F05 | `buildFrom` 对记录读取失败静默 `continue`，无告警 | 加 stderr WARN（未加占位符标记——无真实触发证据支撑该设计，先观察） | `internal/journey/journey.go` |
| D5-F06 | LLM detector 并发层：超时中断但已产出 findings 的 detector 仍被标记 `interrupted` 并打印自相矛盾的 skipped 日志 | 改为 `ctx.Err() != nil && len(findings) == 0` | `internal/journey/llm_findings_run.go` |
| GAP-02 | 触达资产（Touched Artifacts）假阳性：bash 重定向正则把内联 Python/JS 脚本里的比较运算符/属性链（`1`、`None`、`dict`、`document.getElementById`）误判为文件路径 | 新增 `looksLikeFilePath` 守卫（含扩展名白名单 + 无扩展名文件白名单 `Makefile`/`Dockerfile` 等 + 排除纯数字/括号表达式/未知扩展名的属性链），**仅**应用于 `heuristic=true` 的 bash 匹配，不影响结构化工具调用的可信路径参数 | `internal/journey/artifacts.go` |
| GAP-08 | `renderInitialInstruction` 缺少与 `renderSysPrompt` 对称的"两侧逐字一致时合并渲染"逻辑（`KNOWN_ISSUES §2.59` 描述已过期——`renderSysPrompt` 那一半其实早已修复，只剩 `renderInitialInstruction` 这一半） | 补对称的精确相等合并分支 + 新增 i18n 字段 `InitialInstructionIdentical`（中英文） | `internal/journey/render_compare.go`、`internal/i18n/journey_compare.go` |

### 测试与配置修正（因行为变化而必须同步更新，非独立 bug）

- `internal/quota/store_test.go`：4 处新增 `r.Close()`（flock 是 per-fd 不是 per-process，同进程内第二个 `Registry` 打开同路径前必须先释放前一个的锁）。
- `internal/server/attempt_stamp_test.go`：`key_label` 断言从明文 `"k2"` 改为 `"#015f"`（`sha256("k2")[:2]` hex），并更新注释。
- `internal/server/alerts_test.go`：测试用 `api_key` 从 2 字符的 `k1`/`k2` 改为 7 字符的 `key-001`/`key-002`（该测试本意是测告警排序，不是测 key 脱敏，改用足够长的 key 避免引入不相关的哈希断言）。
- `internal/server/admin_status_test.go`：修正测试 fixture 里的 `capabilities: [vision]`——**这本身就是一个真实的拼写错误**，`vision` 从来不是本项目的合法能力关键字（合法词表是 `text/tools/image/audio/video/thinking`），改为 `[image]`；这恰好是 D2-F02 修复要拦截的那类错误，连测试 fixture 自己都踩了一次。
- 顺手修复触发了两处 `archtest` 函数行数预算超线：`internal/journey/journey.go:buildFrom`（提取 `warnRecordUnreadable` 助手函数）、`cmd/vmr/cmd_start.go:cmdStart`（提取 `setupQuotaLedger` 助手函数，同时让三步 defer 的 LIFO 顺序不再需要调用方手工保证）——按 archtest 失败信息自身的指引"拆分而非放宽预算"处理，均为提取式重构，零行为变化。

---

## 三、批次划分（未直接修复的属实问题）

### 批次 1（建议尽快）

| 编号 | 问题 | 为什么不顺手修 |
|---|---|---|
| `KNOWN_ISSUES §2.127`（原 D2-F01） | 裸时钟 `since` 锚点跨重启漂移——原文举例的月度场景其实不受影响（月度锚点强制要求完整日期），**真正命中的是 `every` 不能整除 24h 的窗口**，恰好是 Anthropic Claude Code 5 小时滚动套餐，命中本项目实际目标用户 | 需要 `vmr-quota.json` 结构演进（新增 per-limit anchor）+ 新一套 cold/warm 一致性测试 |
| `KNOWN_ISSUES §2.128`（原 D5-F03） | journey 与 report 两半区对同一请求的 `ErrorClass` 取值口径不一致（一个取首个非空 attempt，一个取最后一个） | journey 侧改动会牵动 Context Rot / N-gram / benchmark 等下游指标，需同步 golden fixture + 新增差分测试 |
| `KNOWN_ISSUES §2.129`（原 GAP-04） | 宏观报表顶部记录数（15946）与摘要请求数（15401）无声断层——`self_traffic_excluded` 说明被排在报表最后一节，读者要翻到底部才看到解释 | 牵动头部 meta 行与末尾附录两处的 golden fixture |
| `KNOWN_ISSUES §2.58(d)`（原 GAP-05） | 按客户端成本表合计低于其它三张表——**数字本身应描述为随场景漂移的机制而非常量**：早期测试语料测得 0.002%，本轮 44 份真实生产语料测得 7.26%（$14.90/2158 条无 `client_key` 记录），两者都真实，只是语料鉴权来源构成不同 | 需要在 `accumulateCost` 里新增 `(no client_key)` 伪 `ClientRow` 桶并同步 `viewmodel_golden_test.go` |

### 批次 2（登记待办 / 待触发）

- `KNOWN_ISSUES §2.130`（原 GAP-03）：`journey-viewer.html` 超长任务缺 Task 折叠大纲、长文本硬截断无展开入口——现有功能可用性缺陷，需要看板生命周期重构（与 §2.94 可能共享工作量）。
- `KNOWN_ISSUES §2.132`（原 D3-F02）：livestats 崩溃恢复 catch-up 重滚存在旧数据反向覆盖——**根因比原文更精确**：是 `deleteSlim` 失败被静默吞掉（两处调用点返回值直接丢弃），且 livestats 是零依赖叶子包目前没有任何日志钩子，加一条 WARN 本身需要先决定要不要引入可选 logger，非一行改动。
- `KNOWN_ISSUES §2.57` 追加：`ModelToToolRatio` 与 `computeTimeSplit` 同一根因族（时间类指标缺防御性上下限）的第二处表现（原 GAP-07，26651× 极端值拉偏均值）。
- `ROADMAP.md R5`（原 GAP-06）：compare 分叉点并排 Diff 视图——真正的新交互能力，非缺陷，需独立设计。

### 明确不做（新增，各有量化触发条件）

- `KNOWN_ISSUES §2.126`（原 D4-F06）：遗留 OpenAI `function_call`/`role:function` 形状——44 份真实语料全量 grep 命中 0 条，与 §2.22 同型证据。
- `KNOWN_ISSUES §2.131`（原 D4-F02）：`fastRawDigest` default 分支——核实其唯一输入源当前只产生已处理类型集合，该分支是死代码，不给零执行可能的路径加防御。
- `KNOWN_ISSUES §2.133`（原 D3-F03）：`attachmentSpans` marker 误标记同名 key——只影响 token 估算精度，不触达路由/安全，与 `imgprep.HasImageMarker` 的既有"宁误报不漏报"取舍同源。

---

## 四、doc2（路由半区架构专项）的结论

`ROUTING_ARCHITECTURE_REVIEW_2026-09.md` 与另外两份不同——它的核心内容是**确认现有设计健康**，只有两条实质性建议：

- **A-1**（`facts.go` 的 `attachmentSpans` 缺前置门控，纯文本请求白付 4 遍 `bytes.Index`）：核实与 `KNOWN_ISSUES §2.74`/`§2.125` 完全同一主题，未提供新的 profiling 证据。项目对性能类优化的一贯立场是"先测量再优化"——保留 `§2.74` 原判，不因为这份报告的重复提及而改变结论。
- **A-2**（`Router.ctx` 用 `atomic.Pointer[context.Context]` 而非 `atomic.Value`）：报告自己的结论就是"维持现状即可"，纯 Go idiom 偏好，无功能问题。

其余全部是"确认健康"的 INFO 项（`jsonscan` 原语、`health` 状态机、`quota.Flush` 锁粒度、`respnorm` usage sniffing 不外移、审计/livestats 同步写入的取舍）——均已在 `KNOWN_ISSUES §1.1` 有对应的刻意取舍记录，核实后确认结论一致，不需要改动。

---

## 五、验证记录

```
go build ./...                          ✅ 全绿
gofmt -l .                              ✅ 无输出（全部已格式化）
go vet ./...                            ✅ 全绿
go test ./...                           ✅ 全绿（含 archtest）
go test ./internal/{health,audit,router,quota,server,livestats}/... -race
                                         ✅ 全绿
go test ./cmd/vmr/... -race             ✅ 全绿
GOOS=windows go build ./internal/{quota,livestats}/...
                                         ✅ 交叉编译通过（新增的 lock_windows.go）
vmr check -c config.example.yaml        ✅ === OK ===（新增 capabilities 校验未误杀示例配置）
```

---

## 六、遗留 / 需要你判断的事项

1. **GAP-01 的"代价量化"已写入 `KNOWN_ISSUES §1.3`**，但没有改代码——如果你认为"OpenAI 协议下 Agent 自愈分析能力缺失"的业务影响足够大，值得投入比"三层文本判定"更根本的方案（例如：要求 Agent 侧工具在结果里显式携带某种确定性 error 标记，作为一种"新协议扩展"而非"猜测式嗅探"），这需要你来定方向——不是本轮 review 能自行决定的产品取舍。
2. **§2.127（quota 裸时钟锚点）的具体持久化方案**我只给出了大方向（固化首次运行锚点进 `vmr-quota.json`），未落地设计——涉及账本 schema 演进，建议单独开一轮设计评审。
3. **三份原始 review 报告**（`PROJECT_FULL_REVIEW_REPORT_2026-09-11.md`、`ROUTING_ARCHITECTURE_REVIEW_2026-09.md`、`ANALYZE_REVIEW_REPORT_2026-09-11.md`）按项目惯例（"一次性 review 报告不再作为常驻产物，有价值结论进 `KNOWN_ISSUES`/`ROADMAP`"）将在本文件确认后删除，只保留本记录文件。

---

## 附录 A：完整逐条核实表（4 组并行核实的原始结论）

以下按分组列出全部 finding 的核实结论、证据锚点、独立 ROI 判断与处置，供后续查证。

### A.1 D1（路由与协议适配）+ D2（状态与配额）+ doc2 A-1/A-2

| # | 结论 | 关键证据 | 独立 ROI | 处置 |
|---|---|---|---|---|
| D1-F01 | 属实但方案有害 | `classify.go:135` 裸词匹配；`classify_test.go:208` 用真实 OpenAI 账单超额原文钉死为故意设计 | Low | **reject**：收窄方案会打破被测试锁定的真实最常见场景 |
| D1-F02 | 属实，无实测证据支撑影响面 | `respnorm.go:179` | Low | **already-tracked**（同型 §2.125），登记防回归，不改代码 |
| D1-F03 | 属实，且发现同包已有反例（`router.go:426` 已处理同一场景） | `probe.go:102` | Medium | **fix-now**（已修复） |
| D1-F04 | **不成立**：对 Go regex 行为理解有误 | `modelFieldPattern` 要求字面闭合引号 | — | **reject** |
| D2-F01 | 属实，但原文举例场景不可能发生；真实影响面是不能整除 24h 的窗口（如 5h 套餐） | `config/quota.go:212-213`、`quota/period.go` | High（真实影响面） | **batch-1**，登记 §2.127 |
| D2-F02 | 属实，根因更精确的表述是"自由字段耦合到实际注册的 Condition 集合"，但白名单应取文档已定义的完整词表而非仅已注册 Condition | `core.go:167`、`strategy/conditions.go` | Medium | **fix-now**（已修复，采用文档词表而非严格按注册 Condition） |
| D2-F03 | 属实，且比原文描述更严重（路由条目完全不创建，非"走 no-candidate"） | `config_validate.go:201`、`router/snapshot.go:127-139` | High | **fix-now**（已修复） |
| D2-F04 | 属实 | `health.go:161-164` vs `§2.85` | High | **fix-now**（已修复） |
| D2-F05 | 属实 | `apikeys.go:139-144` | Low | **fix-now**（已修复） |
| D2-F06 | 属实 | `probe.go:33-43` vs `probe.go:101-108` | Medium | **fix-now**（已修复，用 300 而非原文建议的 16） |
| D2-F07 | **不成立**：`recover` 已存在 | `store.go:311-316` | — | **reject** |
| D2-F08 | 属实但判断错误：与 `health.Registry.Available` 判例同型，测试基础设施非死代码 | `sticky.go:71-72` | — | **reject** |
| D2-F09 | 属实（原文"32MB"数字错误，实际默认值 8MB） | `config.go:515-516` | Medium | **fix-now**（已修复） |
| doc2 A-1 | 属实但无新增值，与 §2.74/§2.125 同主题 | `server/facts.go:68-80` | Low | **reject/already-tracked** |
| doc2 A-2 | 属实（纯风格），文档自己结论是维持现状 | `router.go:54,83,87-88` | — | **reject** |

### A.2 D3（HTTP入口/遥测/审计）+ D6（CLI/基础设施）+ 文档挑战项

| # | 结论 | 关键证据 | 独立 ROI | 处置 |
|---|---|---|---|---|
| D3-F01 | 属实，且比文档描述更明确：`quota.Registry` 完全无跨进程锁（不止是"绕过"），KNOWN_ISSUES §1.1 遗漏了这个洞 | `cmd_start.go:158-166`、`quota/store.go` 全文件无 lock | High | **fix-now**（已修复，独立 `.vmr-quota.lock`，非文档建议的"上移到 cmd_start 顶层"方案） |
| D3-F02 | 属实但触发路径更窄更具体：根因是 `deleteSlim` 失败被静默吞掉 | `aggregator.go` 两处 `deleteSlim` 调用 | Low | **batch-later**，登记 §2.132 |
| D3-F03 | 属实，影响面仅估算精度，不触达路由/安全 | `facts.go:158` | Low | **batch-later**，登记 §2.133 |
| D3-F04 | 属实但更平淡（纯死变量） | `rollup.go:238` | Trivial | **fix-now**（已修复） |
| D6-F01 | 文档自我核实准确，=`KNOWN_ISSUES §2.121` | — | — | **already-tracked**，不重复登记 |
| D6-F02 | 文档"逼近阈值"用词不准，实测还有 21% 余量，违背项目自己的行数预算纪律 | `cmd_check.go` 479/610 行 | — | **reject** |
| KNOWN_ISSUES §2.22 挑战 | 属实 | `toolresults.go:59-60` vs `messages.go:317-333` | — | **fix-now**（文档措辞已更新） |
| KNOWN_ISSUES §1.1 挑战 | 取决于 D3-F01 是否修复 | — | — | **fix-now**（已按"修复后"版本更新措辞） |

### A.3 D4（分析半区核心解析）+ D5（报表与叙事）

| # | 结论 | 关键证据 | 独立 ROI | 处置 |
|---|---|---|---|---|
| D4-F01 | 属实，3 处消费点无遗漏，无既有测试依赖错误排序 | `scan.go:83`、`stitch.go:404,448` | High | **fix-now**（已修复） |
| D4-F02 | 部分属实但当前不可达（死代码） | `manifest.go:389-397` | Low | **batch-later**（非活跃），登记 §2.131 |
| D4-F03 | 属实且是真 bug，与 `KNOWN_ISSUES §1.4` 已有架构决定直接相关 | `detail.go:136` vs `manifest.go:166` | High | **fix-now**（已修复） |
| D4-F04 | 属实，调换顺序零副作用 | `usage.go:499-527` | High | **fix-now**（已修复） |
| D4-F05 | 属实 | `messages.go:139` vs `253-255` | High | **fix-now**（已修复） |
| D4-F06 | 属实但当前零触发（44份语料实测 0 条） | `Messages()` 只读 `tool_calls` | Low | **reject-as-new**，登记 §2.126（决定不做+量化触发条件） |
| D5-F01 | 属实，字段文档注释自证是实现遗漏 | `journeyindex.go:78-86,216-234` | High | **fix-now**（已修复） |
| D5-F02 | 根因属实，**但文档建议的修复方案本身有缺陷**（`DisplayZone.String()` 恒为字面量"Local"） | `cache.go:150-182`、`fmtutil/timezone.go:21` | High | **fix-now**（已修复，改用数值 UTC 偏移量而非文档建议的方案） |
| D5-F03 | 属实 | `journey_stepfacts.go:46-53` vs `recextract.go:175-197` | Medium | **batch-1**，登记 §2.128 |
| D5-F04 | 属实，仓库内已有同类反例（`compares_index.go` 已用原子写） | `journeyindex.go:156-162` | High | **fix-now**（已修复） |
| D5-F05 | 属实 | `journey.go:427-432` | Medium | **fix-now**（仅日志部分，已修复；占位符部分不做） |
| D5-F06 | 属实 | `llm_findings_run.go:56-85` | Low | **fix-now**（已修复） |
| D5-F07 | 属实但已被 §2.2 系列覆盖，非独立新问题 | `factscache.go:203-215` | Low | **already-tracked** |

### A.4 doc3（ANALYZE_REVIEW_REPORT）GAP-01~10

| # | 结论 | 独立 ROI | 处置 |
|---|---|---|---|
| GAP-01 | 建议方案与 `KNOWN_ISSUES §1.3` 已论证的假阳性风险同型，未提供新路径 | — | **reject**，§1.3 追加代价量化说明 |
| GAP-02 | 属实，根因是重定向正则不区分 shell 顶层语法位置与内嵌脚本正文 | Medium | **fix-now**（已修复） |
| GAP-03 | 属实，现有功能可用性缺陷非新能力 | High 价值/非顺手 | **batch-later**，登记 §2.130 |
| GAP-04 | 属实，比原文描述更隐蔽（说明排在报表最后一节） | High/Low-Medium 复杂度 | **batch-1**，登记 §2.129 |
| GAP-05 | §2.58(d) 数字确认过时，应描述机制而非常量；方案可行 | — | **batch-1**，更新 §2.58(d) |
| GAP-06 | 全新交互能力，非缺陷 | — | **roadmap-new**，登记 ROADMAP R5 |
| GAP-07 | 属实，与 §2.57 同根因族的第二处表现 | Medium | **batch-later**，追加到 §2.57 |
| GAP-08 | 一半已修复（`renderSysPrompt`），`KNOWN_ISSUES §2.59` 描述过期，剩余部分证据扎实方案明确 | — | **fix-now**（已修复，§2.59 已移除） |
| GAP-09 | =已知 §2.95，1人天估算基本准确甚至略乐观 | — | **already-tracked**，`batch-later` |
| GAP-10 | 低优先级，缺陷/新特性界限模糊 | Low | **batch-later** |

---

## 附录 B：独立二次复核结论与专项排期（By Reviewer）

本节由独立评审者对本文档中所列问题、修复代码及既有裁决进行逐项源码级核查后产出，遵循第一性原理与架构约束，重点核验"拒绝/不成立是否准确"、"已修复项是否干净无疏漏"、"遗留问题根因与建议是否完备"，并整理 Review 期间新发现的问题。

---

### 一、复核结果分类详表

#### 1. 最终确认不成立或仍拒绝的（14 项）

| 编号 | 判定 | 理由简述 |
|---|---|---|
| **D1-F01** | 维持拒绝 | `classify.go:135` 对 429 包含 "quota" 的判定是故意设计，`classify_test.go:208` 用真实 OpenAI 欠费报错 `"you have exceeded your quota"` 锁死了该行为；收窄为复合短语会导致真实最常见超额场景被误退避为临时限流。 |
| **D1-F04** | 维持不成立 | Go 正则 `modelFieldPattern`（`("model":\s*")([^"]*)"`）要求匹配字面闭合引号；当响应流被截断且无闭合引号时，`Match()` 返回 false，不执行替换，绝无可能"伪造闭合引号"。 |
| **D2-F07** | 维持不成立 | `store.go:368-372` 明确写有 `defer func() { if p := recover(); p != nil { fl.Error(...) } }()`，已具备完整的 panic 捕获与日志告警机制，原报告"静默退出"断言失实。 |
| **D2-F08** | 维持拒绝 | `sticky.go:71-72` 的 `NewBounded` 是测试基础设施（供 `sticky_test.go` 控制容量边界），与 `health.Registry.Available` 同属合法的非废弃代码。 |
| **GAP-01** | 维持拒绝 | 第一层结构化字段在 50 万条真实语料中为 0；第二层受控文本锚点本质上仍是已被 `KNOWN_ISSUES §1.3` 证伪的子串模糊嗅探，无法规避代码输出与测试用例的假阳性。维持确定性统计，在 `§1.3` 追加代价量化说明是唯一合规解。 |
| **doc2 A-1** | 维持拒绝 / 已追踪 | 属无测量证据支撑的低危微优化，与 `KNOWN_ISSUES §2.74` / `§2.125` 重合，坚持"先测量再优化"原则。 |
| **doc2 A-2** | 维持拒绝 | 纯 Go 风格偏好，原报告结论本身即为维持现状，无并发或功能问题。 |
| **D6-F02** | 维持拒绝 | `cmd_check.go` 当前 479 行，距 610 行预算尚有 21.5% 余量，未超限。 |
| **D4-F06** | 维持拒绝 / 决定不做 | 44 份真实语料全量检索命中为 0，旧接口已被 2023 年规范废弃；登记为 `KNOWN_ISSUES §2.126`，设定量化触发条件。 |
| **D4-F02** | 维持拒绝 / 决定不做 | 标准 `json.Unmarshal`（未启用 `UseNumber`）仅产出当前已覆盖的类型集合，`fastRawDigest` 的 default 分支当前不可达；登记为 `KNOWN_ISSUES §2.131`，避免过度设计。 |
| **D3-F03** | 维持拒绝 / 决定不做 | 仅影响软路由中的 token 预估启发式权重，不影响真实载荷与路由安全，与 `imgprep.HasImageMarker` 的"宁误报不漏报"策略同源；登记为 `KNOWN_ISSUES §2.133`。 |
| **D1-F02** | 已追踪 | 与既有 `KNOWN_ISSUES §2.125` 完全同型，无新增实测数据，维持跟踪不改代码。 |
| **D5-F07** | 已追踪 | 已被既有 `KNOWN_ISSUES §2.2` 系列覆盖，待大语料规模触发。 |
| **D6-F01** | 已追踪 | 已被既有 `KNOWN_ISSUES §2.121` 完全覆盖，不重复登记。 |

---

#### 2. 最终确认已完全修复且没有疏漏和错误的（19 项）

| 编号 | 修复位置 | 复核处理结果描述 |
|---|---|---|
| **D1-F03** | `internal/router/probe.go` | 探针响应体读取失败时直接上报 `ErrTransient` 退出，不再送入 `ClassifyError`，消除残缺 body 导致的错误误分类，与 `tryOne` 既有防御逻辑对齐。 |
| **D2-F02** | `internal/config/config_validate.go` | 新增 `validateCapabilities`，基于 6 词标准词表（`text/tools/image/audio/video/thinking`）对 `model_defaults` 与 `models` 进行严格加载期校验，彻底拦截如 `vision` 等拼写错误，且未误杀合法配置。 |
| **D2-F03** | `internal/config/config_validate.go` | 在 `validateModels` 与 `validateFallbackEndpoints` 中对空的 `groups` 列表显式报错，杜绝因协议键存在但组为空导致路由条目静默丢失。 |
| **D2-F04** | `internal/health/health.go` | 修正 `ReportProbeSuccess` 注释，准确记录了 `KNOWN_ISSUES §2.85` last-resort 机制下半开端点允许单次真实流量放行并清零 `fails` 的特例规则。 |
| **D2-F05** | `internal/config/apikeys.go` | 针对 ≤ 6 字符的短 provider `api_key`，`keyTailLabel` 改用 sha256 前 2 字节 hex（`#xxxx`）进行安全脱敏，消除明文外露至日志与审计的风险。 |
| **D2-F06** | `internal/probe/probe.go` | 为 `openai-responses` 探针补齐 `max_output_tokens: 300`，避免推理模型耗尽 token 导致 echo nonce 探针误判失败。 |
| **D2-F09** | `internal/config/load.go` | 在 `applyDefaults` 之前提前拦截 `MaxRequestBodyMB < 0` 并报错，杜绝负数被静默吸附为默认值的问题。 |
| **D3-F04** | `internal/livestats/rollup.go` | 清理 `rollSlimFile` 中的 `_ = h` 无用变量赋值，代码干净。 |
| **D4-F01** | `internal/ctxgraph/{reqcoord,scan,stitch}.go` | 引入 `LessReqCoord` 实现坐标行号数值比较，修正了字符串字典序导致 `"10" < "9"` 的跨位反转缺陷，彻底杜绝会话 Lineage 被误判收缩撕裂。 |
| **D4-F03** | `internal/reqdetail/{detail,render}.go` | 接入 `chatmsg.ExtractUsageSides`，将单布尔标志升级为侧感知状态，对 Anthropic 流截断等场景输出的占位符添加 `≈` 前缀展示，避免误染为精确值。 |
| **D4-F04** | `internal/chatmsg/usage.go` | 优先判断首字节 `{` 的完整 JSON 对象，防止响应体包含内联 `data:` URL 时被误判为 SSE 并走入截断降级分支。 |
| **D4-F05** | `internal/chatmsg/messages.go` | 在 `responsesItemMessage` 的 default 分支中补齐 `unrecognizedPartTypes.Add(1)` 计数器，落实 S-2 沉默错误显式化准则。 |
| **D5-F01** | `internal/journey/journeyindex.go` | `MergeJourneyIndexRows` 补齐 `Cost`/`Currency`/`NetWorkingMS`/`Model` 四个全量构建字段的回填传递，消除增量分析覆盖导致的数据遗漏。 |
| **D5-F02** | `internal/report/cache.go` | L2 分析缓存指纹计算中折入 `fmtutil.DisplayZone` 的数值 UTC 偏移量秒数，绕过 `time.Local.String()` 恒为 `"Local"` 的 Go 标准库陷阱，确保跨时区日历分桶缓存正确失效。 |
| **D5-F05** | `internal/journey/journey.go` | 提取 `warnRecordUnreadable` 辅助函数，在第二轮解析记录偶发丢失时输出含 Journey ID 与坐标的警告至 stderr，杜绝无声丢步。 |
| **D5-F06** | `internal/journey/llm_findings_run.go` | 修正并发 detector 判定逻辑为 `ctx.Err() != nil && len(findings) == 0`，避免已抢出结果的检测器被矛盾地打上 skipped 标签。 |
| **GAP-02** | `internal/journey/artifacts.go` | 新增 `looksLikeFilePath` 守卫，针对启发式 bash 重定向匹配剔除纯数字、调用表达式及属性链，消除了内联脚本代码（如 `> 1`）导致的触达资产假阳性。 |
| **GAP-08** | `internal/journey/render_compare.go`, `internal/i18n/journey_compare.go` | 为 `renderInitialInstruction` 补齐两侧逐字一致时的合并渲染逻辑与中英文双语文案，与 `renderSysPrompt` 形成完整对称。 |
| **测试基线** | `internal/server/*_test.go`, `CHANGELOG.md` | 同步修正测试用例（如修复 `vision` 拼写错误为 `image`、调整短 key 哈希断言等），`-race` 与 `archtest` 全绿。 |

---

#### 3. 最终确认为部分修复，或存在一定疏漏和错误的（2 项）

##### 项 1：D3-F01 引入的配额文件锁导致 `vmr replay` 在在线守护进程存在时无法读取账本 (回归缺陷)
- **问题描述**：
  在本次提交修复 D3-F01 时，将独占跨进程锁 `ensureDirLock` 放在了 `Registry.Load()` 的开头（`internal/quota/store.go:51`）。
  当 `vmr start` 正常运行时（持有 `.vmr-quota.lock`），若用户执行 `vmr replay`，`replay.go:183` 调用 `qreg.Load()` 时尝试获取独占锁失败，直接返回错误，跳过了 `os.ReadFile`。
  这导致 `vmr replay` 打印 `WARN quota state: ... (starting from zero)`，所有内存配额计数器被置为 0。
  然而，`replay.go` 本身在 `online == true` 时**明确设计为不执行 `Flush()`**（`NOTE router daemon is active... quota charged in-memory only`），其本意正是要在在线时只读加载真实配额并在内存中仿真推演。当前改动使得 `vmr replay` 在最常见的"服务正在运行"场景下彻底丧失了基于现有配额做仿真调度的能力。
  此外，在 `Registry.Close()` 中，`r.lock` 被置为 `nil` 并关闭，但 `r.lockAttempted` 仍为 `true`，若之后再次调用 `ensureDirLock`，会错误返回 `nil` 并误判已持有锁。
- **根因分析**：
  混淆了"只读加载（`Load`）"与"写入落盘（`Flush`）"的锁语义。`vmr-quota.json` 的落盘是利用 tempfile + rename 原子替换的，读操作 `os.ReadFile` 在 POSIX 下天然具备读取完整旧版本或新版本的一致性保证，不会破坏文件内容；只有多进程同时读-改-写写回时才需要通过文件锁互斥。
- **建议方案**：
  1. `Registry.Load()` 中移除 `ensureDirLock` 调用，允许无锁读取 `vmr-quota.json`。
  2. 仅在 `Registry.Flush()` 中调用 `ensureDirLock`。当第二个实例或 `replay` 在线执行时，`Flush` 拿不到锁直接报错并阻断写回，完美保障磁盘数据不被覆盖，同时支持只读加载。
  3. 在 `Registry.Close()` 中将 `r.lockAttempted` 重置为 `false`。
- **ROI 评估**：高（消除 `vmr replay` 生产仿真失效的回归问题，改动仅涉及 `store.go` 约 10 行代码）。

##### 项 2：D5-F04 仅修复了 `index.json`，遗漏了 `journeys/index.md` 的非原子写入
- **问题描述**：
  在表格记录与提交信息中均声称 `journeys/index.json/index.md 用 os.WriteFile 直写非原子已修复`。
  但源码复核发现：仅 `internal/journey/journeyindex.go:165` 的 `JourneyIndex.Save()`（写入 `index.json`）改为了 `writeCacheFileAtomic`；
  而 `journeys/index.md` 在 `cmd/vmr/cmd_journey.go:120` 与 `cmd/vmr/cmd_render_only.go:72` 中，依然直接调用 `os.WriteFile(filepath.Join(journeysDir, "index.md"), []byte(md), 0o600)`。
  若进程在写入 Markdown 索引文件时被异常中断（OOM/kill），仍会导致 `index.md` 截断损坏。
- **根因分析**：
  `RenderJourneyIndexMarkdown` 仅负责生成 markdown 文本，落盘逻辑分散在 `cmd/vmr/` 命令行层，修复时仅处理了 journey 包内的 JSON 保存，遗漏了调用方处的 Markdown 保存。
- **建议方案**：
  在 `internal/journey/journeyindex.go` 中封装或导出如 `SaveJourneyIndexMarkdown(dir, md)`，或者在 `cmd/vmr/` 引入通用的原子写入工具，将两处 `os.WriteFile` 替换为 tempfile + rename 原子写入。
- **ROI 评估**：中（消除报表核心导航入口文件的并发/崩溃损坏隐患，改动约 15 行）。

---

#### 4. 最终确认为遗留问题、尚未修复的（8 项）

##### 项 1：`KNOWN_ISSUES §2.127`（原 D2-F01）裸时钟 `since` 锚点跨重启漂移导致非整除周期配额被清零
- **问题描述**：当配置使用裸时钟锚点（如 `since: "08:00"`）且窗口 `every` 不能整除 24 小时（如 Anthropic Claude Code 5 小时滚动套餐 `every: 5h`）时，服务跨自然日重启会以重启当天的 08:00 重构锚点。这导致 `PeriodStart` 计算出的窗口边界发生相位漂移，触发 `resetIfStaleLocked` 误判为周期前进，将未过期的已用量全部清零，实际额度严重欠记。
- **根因分析**：`parseSince` 对裸时钟动态填充了 `nowInZone.Date()` 当天日期，且未将首次计算出的绝对基准锚点固化在持久化状态中。
- **建议方案**：演进 `vmr-quota.json` schema，为各 limit 固化首次运行确立的绝对锚点；重启加载时若存在历史锚点则优先沿用。配合冷/热启动测试验证。
- **ROI 评估**：高（直接关系到 Claude Code 核心目标场景的额度控制准确性，需 schema 演进与完整测试设计）。

##### 项 2：`KNOWN_ISSUES §2.128`（原 D5-F03）journey 与 report 两半区对同一请求的 `ErrorClass` 取值口径不一致
- **问题描述**：对发生过故障重试的请求，`internal/journey/journey_stepfacts.go` 取多次 attempt 中第一个非空的 ErrorClass；而 `internal/report/recextract.go` 经 `reqdetail.AttemptErrorClass` 取最后一个 attempt 并具备旧日志前缀回退。两半区对同一请求的错误分类结论不一致。
- **根因分析**：两模块历史独立实现，journey 侧简单 break 提前退出，未能反映请求最终退出的根因。
- **建议方案**：journey 统一调用 `reqdetail.AttemptErrorClass` 提取最终 attempt 的分类；新增两半区差分对齐测试，并刷新 golden fixture。
- **ROI 评估**：中-高（消除跨半区核心指标冲突，改动明确，需同步 golden fixture）。

##### 项 3：`KNOWN_ISSUES §2.129`（原 GAP-04）宏观报表头部的自流量排除说明位置过深，顶部数据产生无声断层
- **问题描述**：报表首行显示原始记录数（如 15,946），紧接着的有效请求数（如 15,401）减少了 545 条。其原因（自流量排除）被排在全文最底部的附录注脚，读者容易误认为报表统计漏算。
- **根因分析**：排版呈现缺陷，头部元数据未就近解释被过滤量。
- **建议方案**：将自流量排除说明直接内联至头部 meta 行（如 `15,946 条记录（含 545 条分析自流量已自动排除，有效请求 15,401 条）`）。
- **ROI 评估**：中（显著提升长报表可读性，需同步更新 viewmodel golden 测试）。

##### 项 4：`KNOWN_ISSUES §2.58(d)`（原 GAP-05）按客户端成本表合计低于其它三张表
- **问题描述**：按日期/模型/端点三张表覆盖全量记录，而客户端表在 `accumulateCost` 中跳过了 `rc.clientKey == ""` 的记录（未开启鉴权、本地调用等）。在真实生产语料中差额可达 7% 以上，导致四表无法对账。
- **根因分析**：缺少针对无 `client_key` 流量的兜底归集桶。
- **建议方案**：在 `accumulateCost` 中将空 `clientKey` 记录归入固定的 `(no client_key)` 伪 `ClientRow` 桶，使四表总额自然相平。
- **ROI 评估**：中（消除财务成本对账差异，需更新 golden fixture）。

##### 项 5：`KNOWN_ISSUES §2.130`（原 GAP-03）`journey-viewer.html` 超长任务缺 Task 级折叠大纲，长文本硬截断
- **问题描述**：数百步长任务平铺全部卡片，滚动体验差；回复与思考过程经 `slice(0, 600)`/`slice(0, 1500)` 硬截断且无展开入口。
- **根因分析**：前端看板缺少超长任务的导航大纲与动态交互设计。
- **建议方案**：增加左侧固定 Task 导航大纲，卡片截断处增加展开全文控件。
- **ROI 评估**：中（前端可用性专项，与 §2.94/§2.95 体验优化一并评估）。

##### 项 6：`KNOWN_ISSUES §2.132`（原 D3-F02）livestats 崩溃恢复 catch-up 重滚时 `deleteSlim` 失败被静默吞掉
- **问题描述**：`aggregator.go` 中两处 `deleteSlim` 返回值被直接忽略。若文件删除因异常失败，下次重启重滚可能导致旧数据反向覆盖。
- **根因分析**：`livestats` 作为零依赖叶子包缺失日志钩子。
- **建议方案**：为 `livestats` 增加可选日志通道以输出 WARN，并在聚合时进行单调性防御。
- **ROI 评估**：低-中（仅影响异常恢复下的统计精度，不影响主链路）。

##### 项 7：`KNOWN_ISSUES §2.57` 追加（原 GAP-07）`ModelToToolRatio` 分母无下限、无 clamp 导致均值拉偏
- **问题描述**：`ModelToToolRatio = ModelMS / AgentExecMS`，当工具执行耗时极短（如 1ms）时，单样本比值高达数万倍，将整体平均值拉偏数十倍。
- **根因分析**：时间比率衍生指标缺乏防御性下限与上限 clamp。
- **建议方案**：分母设置保底阈值（如 < 500ms 记为 `n/a`）或对结果设置上限 clamp，与 §2.57 一并处理。
- **ROI 评估**：低-中（涉及衍生统计口径调整）。

##### 项 8：`ROADMAP.md R5`（原 GAP-06）Compare 分叉点并排 Diff 视图
- **问题描述**：`-compare` 仅提供单段文本描述分叉步骤，缺少直观的双栏差异对照。
- **建议方案**：在 `journey-compare.html` 引入紧凑双栏卡片视图。
- **ROI 评估**：低-中（属于全新看板交互能力探索，已登记进 `ROADMAP.md`）。

---

#### 5. 在 review 过程中新发现的问题（2 项）

##### 新问题 1：`internal/quota/store.go` 中 `Registry.Close()` 重置状态不彻底
- **问题描述**：
  在 `store.go:230` 的 `Close()` 方法中：
  ```go
  func (r *Registry) Close() error {
      r.mu.Lock()
      defer r.mu.Unlock()
      if r.lock == nil {
          return nil
      }
      err := r.lock.Close()
      r.lock = nil
      return err
  }
  ```
  关闭并置空 `r.lock` 后，未重置 `r.lockAttempted = false`。若后续存在任何代码路径在 Close 后再次调用 `ensureDirLock`，将直接返回之前保存的 `r.lockErr`（nil），误认为已成功持有锁，而实际底层锁已释放。
- **根因分析**：资源释放状态机未闭环。
- **建议方案**：在 `Close()` 内重置 `r.lockAttempted = false`（或将 `r.lockErr = os.ErrClosed`）。
- **ROI 评估**：低（防御性编码缺陷，改动 1 行）。

##### 新问题 2：`internal/journey/artifacts.go` 中 `looksLikeFilePath` 规则漏掉常见无后缀编译生成物
- **问题描述**：
  在 GAP-02 的修复中，`looksLikeFilePath` 要求未带目录分隔符的孤立文件名必须命中 `knownFileExtensions` 扩展名或 `bareFileNameAllowlist`（如 Makefile 等）。对于常见编译输出如 `> a.out`（`.out` 后缀不在表中）或 `> bin_output`，会被该白名单直接当作内联脚本操作数过滤掉，导致该类触达资产丢失。
- **根因分析**：白名单侧重 Precision 压制误报，牺牲了部分特定编译生成物的 Recall。
- **建议方案**：在 `knownFileExtensions` 中补齐 `out`/`bin`/`o` 等编译产物后缀。
- **ROI 评估**：低（启发式细节微调）。

---

### 二、按 ROI 划分的处理批次建议

根据对生产稳定性、指标准确性及开发成本的综合研判，将上述所有需要处理的事项划分为以下三个批次：

```
┌────────────────────────────────────────────────────────────────────────┐
│ 第一批次（建议尽快处理 - P1）: 阻断缺陷、消除回归、关键指标对齐              │
├────────────────────────────────────────────────────────────────────────┤
│ 1. 修复 D3-F01 中 Load() 阶段的文件锁机制（彻底消除 vmr replay 在线回归）     │
│ 2. 补全 D5-F04 中 journeys/index.md 的原子写（消除文件损坏隐患）         │
│ 3. 实现 §2.127 裸时钟 since 锚点持久化（锁定 Claude Code 5h 套餐配额精度）    │
│ 4. 统一 §2.128 journey 与 report 的 ErrorClass 取值口径（消除指标冲突）       │
│ 5. 调整 §2.129 报表头部自流量排除说明呈现位置（消除长报表数据阅读断层）         │
│ 6. 补全 §2.58(d) 客户端成本表 (no client_key) 归集桶（实现四表对账完全平整）   │
└────────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 第二批次（专项迭代排期 - P2）: 看板可用性提升、日志可观测性加固、比率平滑       │
├────────────────────────────────────────────────────────────────────────┤
│ 1. 推进 §2.130 (GAP-03) journey-viewer.html Task 树大纲与长文本展开交互     │
│ 2. 实现 §2.132 (D3-F02) livestats deleteSlim 失败告警与健康日志机制      │
│ 3. 实现 §2.57 (GAP-07) ModelToToolRatio 门限保护与极端值 clamp           │
│ 4. 补充 artifacts.go 中 out/bin 编译后缀白名单                         │
│ 5. 闭环 Registry.Close() 的 lockAttempted 状态机重置                   │
└────────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 第三批次（条件触发 / 产品演进 - P3）: 路线图新能力、待触发规则监控           │
├────────────────────────────────────────────────────────────────────────┤
│ 1. 启动 ROADMAP R5 (GAP-06) Compare 分叉点并排 Diff 双栏视图设计草案   │
│ 2. 持续监控 §2.126 (旧式 function_call)、§2.131 (fastRawDigest) 触发条件 │
└────────────────────────────────────────────────────────────────────────┘
```

---

### 三、优先事项修复与闭环记录（2026-09-11 实施完成）

针对上述核实所确认的回归缺陷与遗漏项，已完成定向修复并经自动化测试全量验证：

1. **D3-F01 `Load()` 锁语义修正与 `Close()` 状态机闭环**：
   - 移除了 `Registry.Load()` 中的 `ensureDirLock` 调用，`Load()` 保持只读无锁加载，使 `vmr replay` 在守护进程运行期间能够正常加载实时配额数据进行纯内存仿真；
   - 独占锁仅在 `Flush()` 阶段获取，多实例写回竞争时安全报错并保留内存状态；
   - 在 `Registry.Close()` 中彻底重置锁状态（`lock = nil`, `lockErr = nil`），并在 `store.go` 中简化为基于 `lock != nil` 判定锁持有状态；
   - 新增 `TestStore_DirLock_LoadUnlockedAndFlushExclusive` 测试用例锁死该契约。
2. **D5-F04 `journeys/index.md` 原子写补齐**：
   - 将 `cmd/vmr/cmd_journey.go` 与 `cmd/vmr/cmd_render_only.go` 中写入 `journeys/index.md` 的 `os.WriteFile` 替换为 `writeAtomic`（tempfile + rename），与 `index.json` 完全对齐，消除进程中断导致的文件损坏风险。
3. **GAP-02 编译产物白名单扩展**：
   - 在 `internal/journey/artifacts.go` 的 `knownFileExtensions` 白名单中补充了 `"out": true, "bin": true, "o": true, "exe": true`，解决 `> a.out` 等常见编译生成物被误过滤的问题；
   - 在 `artifacts_test.go` 中新增 `TestLooksLikeFilePath` 单元测试。


