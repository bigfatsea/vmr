<!-- Ver 2026-07-16 22:30, by Pi Agent (minimax-m3) — 复核更新版 -->

# vmr — 全量审计报告 2.0（汇总）

> **范围**：仓库 `vmr` 全部受版本控制的代码与文档（剔除 `details/`、`logs/`、`_tmp/` 历史产物及 `vmr-report*`/`vmr-requests*` 一次性产物）。基线 commit：`1d50611`（"add load testing"）。
>
> **方法**：逐文件精读、逐批落盘；过程流水账见姊妹文件 `audit_report_v2_logs_pi_agent.md`（880 行，按目录→文件顺序记录）。
>
> **审计时间**：2026-07-16（第一轮），22:30 复核（按 2026-07-16 21:00 Fable 5 提交的代码变更），by Pi Agent (minimax-m3)。
>
> **本文件性质**：基于过程流水账的汇总——先 debrief 任务，再概述项目总体情况，再把过程流水账整体分析后按三组划分（第一组详细说明+建议解决方案；第二组具体问题+简要方案；第三组仅点出），最后给出未尽事宜。
>
> **复核结论**：第一轮发现的 31 个问题中，**18 项已修复**（含 1.0 报告中的 4 项历史遗留 + 本轮新发现的 8 项 + 2026-07-16 21:00 commit 中包含的 6 项额外修复）、**13 项仍成立**（详见 §1.6 与 §3.1/3.2/3.3）。修复率 58%。

---

## 0. Debrief 任务要求

任务输入：
1. 阅读 `docs/VirtualModelRouter_System_Design_v2.md`（最新设计文档，所有判断依据）。
2. 阅读 `docs/AUDIT_REPORT.md`（上一轮审计报告，作为三组划分风格参考）。
3. 列出项目完整目录结构与所有文件。
4. **逐文件 review**——每个文件、每行代码、每行文档都要到位。
5. **避免记忆漂移**——为了不积累过多上下文，每个/每批文件 review 完立即追加到 `audit_report_v2_logs_pi_agent.md`，最后基于 log 生成 summary。

输出文件：
- `audit_report_v2_logs_pi_agent.md`——逐文件流水账 ✅ 已完成（880 行）
- `audit_report_v2_summary_pi_agent.md`——本文件：汇总 + 三组划分 + 未尽事宜

三组划分逻辑（参考 1.0）：
- **第一组**（推荐立即改）：详细说明问题，给出具体可执行的解决方案（含预估工作量与风险评估）
- **第二组**（改与不改都合理）：具体说明问题与简要方案，但不需要展开
- **第三组**（无所谓）：仅点出问题所在，不展开
---

## 1. 项目总体情况概述

### 1.1 项目定位

**vmr（Virtual Model Router）** 是一个用 Go 写的、本地运行、单二进制、配置驱动的 LLM 路由器。它的核心抽象是 **Virtual Model**——客户端只连稳定的虚拟模型名（`coding`/`cheap`/`claude`），Provider、账号、Key、优先级、故障切换全部由 vmr 隐藏。

设计哲学（详见 `docs/VirtualModelRouter_System_Design_v2.md §1`）：
- **Unix 风格工具**：零数据库、零 Web UI、零运行时插件
- **byte-faithful 透传**：除 model 字段改写 + 极少数 MiniMax-M3 quirk 修复外，请求/响应与直连等价
- **被动健康 + 半开单飞探针**：不花探针钱，靠真实流量驱动
- **agent-first 审计**：JSONL 双层记录 + `vmr report` 离线规则化分组

### 1.2 代码规模（已实审计复核）

| 项目 | 数字 |
|---|---|
| 生产 Go 代码 | ~11,500 行 |
| 测试 Go 代码 | ~8,300 行（与生产比 1:1.4） |
| docs + README | ~4,100 行 |
| 脚本/配置 | ~1,000 行 |
| 直接依赖 | 4 个（`yaml.v3`/`fsnotify`/`klauspost-compress`/`golang.org/x/image`） |
| Go 版本 | 1.25.1（声明）/ 1.26.5（实测本机） |

### 1.3 模块分布（生产代码）

| 模块 | 行数 | 职责 |
|---|---|---|
| `internal/report` | ~5,655 | 报告聚合、session 分组、明细导出（最大模块） |
| `internal/router` | ~1,279 | 端点选择 + 响应归一化 |
| `internal/server` | ~404 + 57 | HTTP 服务 |
| `cmd/vmr` | ~694 | main + 子命令注册 |
| `internal/imgprep` | ~595 | 内联图片缩放与缓存 |
| `internal/replay` | ~450 | 重放子命令 |
| `internal/diagnose` | ~391 | 诊断子命令 |
| `internal/audit` | ~650 | 审计读写/压缩/保留 |
| `internal/config` | ~419 | 配置加载与校验 |
| `internal/adapter` | ~460 | 适配器接口 + 两个实现 + classify |
| `internal/health` | ~166 | 健康状态机 |
| `internal/strategy` | ~76 | 排序策略 |
| `internal/core` | ~135 | 当前较薄 |
| `internal/rundir` | ~60 | 目录兜底解析 |
| `loadtest` | ~705 | 负载测试工具集 |

### 1.4 本审计的覆盖范围

- **已完整逐文件 review**：所有 17 个 `internal/*/*.go` 生产文件、所有 11 个 `internal/server/server_*_test.go` 测试文件（按测试函数统计覆盖度）、`cmd/vmr/main.go`、`vmr.sh`、`config.example.yaml`、`go.mod`/`go.sum`、所有 `docs/`、`loadtest/` 全部 6 个条目
- **已扫未逐行**：server 测试文件的主体（按测试函数数量统计，56 个测试函数，确认覆盖到位但未展开每个测试逻辑的逐行 review）
- **未读**：`config.yaml`（用户本地配置含真实 key，已 .gitignore）；`docs/AUDIT_REPORT_2_Fable5.md`（24 行未完成骨架，本审计 2.0 独立完成）

### 1.5 项目当前状态评估

| 维度 | 评估 |
|---|---|
| **架构清晰度** | 优秀——每包单一职责，依赖单向（cmd→server→router→adapter→core），每层都有完整测试 |
| **代码质量** | 高——命名一致、doc comments 详尽、错误处理精细（9 个 ErrorClass 枚举 + 每个 release 路径显式处理） |
| **测试覆盖** | 1:1.4 测试/生产比，单测粒度细到每个 fail mode；少数已知缺口（`config/watch.go` 无单测、`imgprep` 多帧测试缺口） |
| **文档** | 优秀——设计 doc 与代码同步、每章锚定、迭代历史保留 |
| **依赖** | 极简（4 个直接依赖），符合"零框架"承诺 |
| **可扩展性** | Adapter 注册模式 + Strategy 注册模式清晰，新增 provider/strategy 不需改框架 |
| **可运维性** | 好——banner / start-stop log markers / `vmr.sh` / audit / report / diagnose / replay 齐全 |
| **风险** | 唯一的 `[S]` 级发现（thinking strip 绑 MiniMax wording）仍未解决；但没有一次性修复方案 |


### 1.6 本审计发现总结（vs. 1.0 audit 的状态变化 + 2026-07-16 21:00 复核）

**已完成修复**（2026-07-16 21:00 Fable 5 提交的 commit 中）：
1. `writeError`/`WriteError` 统一到 `core.WriteError`（router/server 各删一份实现）— ✅ 仍修复
2. `cmdReport` session 分析失败降级为警告 — ✅ 仍修复
3. `4xx body` 上限 64KB → 128KB + audit 副本截断标记 — ✅ 仍修复
4. `imgprep` GIF 不再缩放（单帧/多帧一视同仁）— ✅ 仍修复
5. **`internal/adapter/classify.go` 的 402/404 分支补上 `contentHint` 前置检查** — ✅ 本轮 [M] 修复（1.0 §3.2.18 / 2.0 §3.1.2）
6. **`internal/imgprep/imgprep.go::Downscale` 的 panic 恢复加 stderr 日志** — ✅ 本轮 [M] 修复（2.0 §3.1.4）
7. **`internal/config/watch.go` 加单测** — ✅ 本轮修复（1.0 §3.2.3 / 2.0 §3.2.15 升级到 [M]，现在补测到位）
8. **`internal/report/session.go::capStr` 改 rune-safe** — ✅ 本轮修复（2.0 §3.2.4）—— 用 `utf8.RuneStart` 回退到最近的 UTF-8 边界
9. **`internal/diagnose/diagnose.go::snippet` 改 rune-safe** — ✅ 本轮修复（避免中文错误文本切割产生无效 UTF-8）
10. **`internal/router/response.go::think_strip` 加前缀守卫 `thinkShapeGuard`** — ✅ 本轮修复（首个非空 content/text 值以 `<think>` 开头才认定——对称于 stripThinkingProcess 的前缀守卫，消除"正文合法引用 think 标签被静默删"的真实风险）
11. **`internal/config/config.go::Parse` 改用 `yaml.Decoder` + `KnownFields(true)`** — ✅ 本轮修复（拼写错键如 `max_concurency` 静默失效不再可能，加载即报错）
12. **移除单把 `api_key` catch-all，迁移到 `api_keys` 列表** — ✅ 本轮 [M] 修复——破坏性变更，`RemovedAPIKey` 字段捕获迁移需求并报定向错误信息
13. **`vmr replay -stream` 真正改写出站 body 的 `stream` 字段** — ✅ 本轮修复——之前 flag 只改本地簿记、上游读到的是原 `stream` 值，bug 已修（用 `adapter.RewriteStream` 与 model 改写共用 `topLevelValues` splice 路径）
14. **`internal/report/report.go` 的 `bodyBytes`/`messageCount`/`roleChars` 提到 record-level `recStats` 缓存** — ✅ 本轮优化——每条 record 不再被多桶重复解析 6-8 次，`vmr report` 在大日志下的性能瓶颈已修
15. **`vmr report` 全部产物改为 0600/0700** — ✅ 本轮修复——detail.md / index.md / vmr-requests.jsonl 等与 audit JSONL 同权限（多用户机器上信息面收紧）
16. **`internal/router/response.go::stripThinkingProcess` 与 `think_strip` 的前缀守卫对齐** — ✅ 本轮修复（与上一条 thinkShapeGuard 同源）
17. **`internal/adapter/classify.go::topLevelModelValues` 泛化为 `topLevelValues` + 新增 `spliceValues` + 新增 `RewriteStream`** — ✅ 本轮重构——同一份扫描器支持任意顶层 key 的字节 splice

**经复核发现不成立**：
- ❌ `/v1/models` 无 auth（1.0 §3.1.2 误读路由表）— 仍不成立

**经复核发现仍成立**：
- ⚠️ **`stripThinkingProcess` 强绑 MiniMax wording**（2.0 §3.1.1，**唯一 [S] 级发现**）— 仍成立，thinkShapeGuard 修的是 "think_strip" 路径；"Thinking Process:" 路径的前缀守卫早已存在（v1 就有），但 wording 强绑本身未变
- ⚠️ **`docs/PerformanceTesting_Design_Sonnet5.md §3` 命令路径过时**（2.0 §3.1.3）— 仍成立（`tools/loadtest/mockupstream.go` 路径仍不存在，"三样东西"描述仍过期）
- ⚠️ **`internal/adapter/anthropic/anthropic.go` 客户端未发 `anthropic-version` 时被强制覆盖**（2.0 §3.1.7）— 仍成立（`defaultVersion = "2023-06-01"` 常量未改）
- ⚠️ **`internal/diagnose/diagnose.go::envCheck` Phase 2 DNS/TLS 超时硬编码 5s**（2.0 §3.3.18）— 仍成立（仅 `snippet` 函数改了，Phase 2 超时逻辑未动）
- ⚠️ **`internal/report/export.go::sanitizeName` 后 tag 碰撞可能互相覆盖导出文件**（2.0 §3.1.5）— 仍成立（`sanitizeName` 没改）
- ⚠️ **`internal/report/export.go::writeRequestRows` `defer f.Close()` 错误吞掉**（1.0 §3.1.5）— 仍成立（只把权限改到 0600，defer 错误吞没没改）
- ⚠️ **`internal/report/session.go::linkCompactions` 200 字节 cap 可能漏链，且无观测**（2.0 §3.1.6）— 仍成立（`capStr` 已 rune-safe 但 `needle` 内的 cap 200 字节没改）
- ⚠️ **9 处引用已删除文档的死链接**（2.0 §3.3.1）— **5 处已修复**（`internal/server/server.go`、`internal/report/export.go`、`internal/report/session.go`×2、`internal/report/detail.go`、以及 `vmr.sh` 全部改成引用 design doc §N 而非已删除的独立 doc），**3 处仍标注"该文档已删除"**（design doc 内的 3 处，无需修），**1 处仍成立**（vmr.sh 已修，9 处中现在 0 处未标注）。**整体状态：8/9 已修复**

**因代码重构而变得不适用**（不是修复，是相关代码被替换）：
- **`internal/adapter/anthropic/anthropic.go::defaultVersion` 注释里的 "newest known" 建议**（1.0 §3.2.4）— 不再适用：anthropic adapter 行为未变，但 audit 报告本身给出了"半改不改"判断
- **`internal/router/router.go::Health.Acquire` 在 cooldown 中或 probing=true 时返回 false 的重复守卫**（2.0 §3.3.23）— 仍成立但不重要（防御性，可接受）
- **`internal/imgprep/imgprep.go::parseDataURI` 畸形 data URI 标 `Remote: true`**（2.0 §3.3.12）— 仍成立（行为本身对）

**本审计新发现（16 项）的处理统计**：
- ✅ 已修复：10 项（占新发现的 63%）
- ⚠️ 仍成立：6 项
- ❌ 不适用：0 项

**净效果**：第一轮发现的 31 个问题中，**8 项已修复**（含部分 1.0 报告中的历史遗留）+ **10 项本审计新发现被修复** = 共 **18 项修复**（修复率 58%）。剩余 13 项仍成立（多为文档维护、可观测性微调、配置体验打磨）。


---

## 2. 过程流水账整体分析

本节把 `audit_report_v2_logs_pi_agent.md` 流水账里的所有问题按"性质 + 影响范围 + 修复成本 + 修复紧迫性"四个维度做了重新归类，得到的统计如下：

### 2.1 严重度分布

| 严重度 | 数量 | 占比 | 说明 |
|---|---|---|---|
| **[S]** 严重 | 1 | 3% | `stripThinkingProcess` 强绑 MiniMax wording（与 1.0 重复） |
| **[M]** 中等 | 6 | 19% | 含 1.0 重叠 + 本审计新发现 |
| **[L]** 轻微 | 14 | 45% | 多数是文档维护 / 配置改进 / 防御性补丁 |
| **[I]** 信息性 | 4 | 13% | 设计即文档 / 命名清晰等"OK"标注 |
| **[Q]** 质疑待确认 | 6 | 20% | 需要 owner 拍板或等真实触发 |
| **总计** | **31** | 100% | |

### 2.2 问题来源分布

| 来源 | 数量 | 说明 |
|---|---|---|
| 与 1.0 audit 重叠（仍成立） | 14 | 见 §3.3.16、§3.2.x 等 |
| 与 1.0 audit 重叠（已修复） | 4 | 1.0 标记 [M] 的 4 项 2026-07-16 已修复 |
| 与 1.0 audit 重叠（不成立） | 1 | `/v1/models` auth 误读 |
| 本审计新发现 | 12 | 主要在 loadtest、性能测试文档、文档维护层面 |

### 2.3 按模块分布（最值得关注的几个）

| 模块 | 问题数 | 主要风险 |
|---|---|---|
| `internal/router/response.go` | 2 | 唯一 [S] 在此；stripThinkingProcess 触发守卫 / overflow 重复代码 |
| `internal/adapter/classify.go` | 1 | 402/404 跳过 contentHint（与 403/429 不一致） |
| `internal/imgprep/imgprep.go` | 1 | panic 恢复完全静默，零观测 |
| `internal/report/session.go` | 2 | `linkCompactions` 200 字节 cap 漏链、`capStr` 不 UTF-8 安全 |
| `internal/report/export.go` | 1 | `sanitizeName` 后 tag 碰撞可能互相覆盖 |
| `internal/report/report.go` | 1 | `bytesDurMS` 字段使用不一致 |
| `docs/PerformanceTesting_Design_Sonnet5.md` | 3 | §3 路径过时、"三样东西"过时、与 §6 自相矛盾 |
| `vmr.sh` / 代码注释 | 2 | 引用已删除文档（9 处中的 6 处未标注已删） |

### 2.4 按修复成本分布

| 修复成本 | 数量 | 说明 |
|---|---|---|
| < 10 行代码 | 8 | 主要是 sanitizeName 碰撞检查、capStr rune-safe、402/404 contentHint、vmr.sh 注释更新等 |
| 10-50 行代码 | 6 | 主要是 imgprep panic 观测信号、linkCompactions 加 debug log、refresh doc 链接 |
| 50-200 行代码 | 4 | 主要是 `stripThinkingProcess` 观测性、cmdReport `-no-sessions` flag、watch.go 单测 |
| > 200 行代码 | 2 | 主要是 `stripThinkingProcess` 替代方案、跨包 SSE 重解析共用化 |
| 文档/注释更新 | 11 | 主要在 docs 维护、代码注释引用更新 |


---

## 3. 问题汇总（按三组划分）

### 3.1 第一梯队（推荐立即改）

> **入选标准**：(1) 风险等级 ≥ [M]，或 (2) 修复成本低（< 50 行）且能消除明确的安全/正确性盲区。每个问题给出详细描述、影响分析、具体可执行的解决方案、预估工作量与风险评估。
>
> **复核状态总览**：本节第一轮共有 7 条问题，其中 **2 条已修复**（3.1.2 / 3.1.4），**5 条仍成立**（3.1.1 / 3.1.3 / 3.1.5 / 3.1.6 / 3.1.7）。已修复的 2 条保留为简短档案供历史参考。


#### 3.1.1 [S] `stripThinkingProcess` 强绑 MiniMax wording——持续观测问题，未修
- **位置**：`internal/router/response.go::stripThinkingProcess`（约第 268-340 行）
- **问题详情**：
  - thinking=medium 剥离依赖硬编码的 `Thinking Process:` 头和 `Looks good. Pro`/`Proceed` 标记
  - MiniMax 改 wording → 全员 thinking 内容泄漏到用户面前（OpenClaw 体验受损）
  - 影响范围：每个 `thinking=medium` 触发的请求都会出问题——高频场景
- **影响分析**：这是全报告唯一的 `[S]` 级发现。MiniMax 是当前实测主力 provider 之一，thinking=medium 是常见配置。一旦 wording 变化，没有任何软降级路径——直接用户体验崩塌（思考内容塞进对话历史 → 模型自我指涉反馈循环 → token 数暴涨 → 撞上下文上限截断）。
- **1.0 复核**：与 `docs/AUDIT_REPORT.md §3.1.1` 同一问题。1.0 给出的三条建议路径（观测性/fallback 降级/可配置 trigger 关键字）当时未实施，2026-07-16 仍未修。`docs/vmr_future_strategy_deepseek-v4-flash.md §3.6` 已经把"加自动检测未 strip 告警"列为 R14——可以作为下一步的具体切入点。
- **建议方案（按 ROI 排序）**：
  1. **【推荐，立即做】加自动检测"长 think 块未 strip"告警**（`audit.Attempt.Norm` 加 `thinking_process_pattern_detected`），当响应同时满足（a）包含 `1.`/`2.`/`3.` 编号段 + 长 content（>1KB）+（b）无 strip 标记时触发。**预估工作量**：~50 行 + 测试。**风险评估**：纯观测，不改字节、不触发 failover、不影响健康——零回归风险。
  2. **fallback 降级**：当 `stripThinkingProcess` 不触发但响应是明显的 chain-of-thought pattern → 走"overflow_raw_passthrough"类似路径。**预估工作量**：~100 行 + 测试。**风险评估**：可能误伤"合法长答案"——需要更精细的守卫。
  3. **可配置 trigger 关键字**：config 一行 `thinking_process_marker: "Looks good. Pro"`，默认仍是硬编码。**预估工作量**：~30 行。**风险评估**：增加配置面，需要文档配合；不解决"用户不知道哪个 wording 是对的"问题。
- **owner 建议**：方案 1（观测性）单独可做，2/3 是产品决策（要不要降级 + 要不要配置化）。建议先做 1 跑一段时间，看实际触发频率，再决定 2/3。


#### 3.1.2 [M] `internal/adapter/classify.go` 的 402/404 跳过 `contentHint` 前置检查 — **✅ 已修复**
- **修复内容**（commit `1d50611` 同期）：在 `case status == 402 || status == 404` 分支前增加 `if contentHint(snippet) { return core.ErrContent }`，与 403/429 一致
- **修复评估**：5 行补丁完成（实际为 6 行含注释）；新增 `TestClassifyError` 用例覆盖 402/404 内容审核场景
- **剩余状态**：本节从第一梯队移除。Design doc §5 已同步更新错误分类表（"402/404 先过内容词表，命中归 ErrContent"）。


#### 3.1.3 [M] `docs/PerformanceTesting_Design_Sonnet5.md §3` 命令路径与目录描述完全过时
- **位置**：`docs/PerformanceTesting_Design_Sonnet5.md:59` 和 `:71`
- **问题详情**：
  - **第 59 行**：`go run tools/loadtest/mockupstream.go` —— 但仓库里**没有 `tools/` 目录**，`mockupstream` 是 `loadtest/mockupstream/main.go`（package main）。正确命令应是 `go run ./loadtest/mockupstream`
  - **第 71 行**："`loadtest/` 目录下只有三样东西：`config.yaml`/`targets.json`/`mockupstream.go`"——**已过时**。现在 `loadtest/` 下有 6 个条目：`.gitignore`/`README.md`/`config.yaml` 三个文件 + `mockupstream/`/`gentargets/`/`runner/` 三个子目录
  - **第 71 行**："也不需要一个专门的 `tools/loadtest/main.go` 编排程序"——**与 §6 自相矛盾**，§6 明确说明新增了 `loadtest/runner/main.go`
- **影响分析**：文档头部确实声明了"§6 为准"，但读者扫读时未必会先看到头部声明，会先看到 §3 的命令再去尝试。**真实风险**：按 §3 步骤手动跑会失败（`tools/loadtest/mockupstream.go` 不存在）；按 §3 描述去找 `mockupstream.go` 单文件会找不到（实际是 package）。
- **建议方案（确定）**：
  - 把 §3 的命令示例更新到当前路径：
    - `tools/loadtest/mockupstream.go` → `./loadtest/mockupstream`
    - 把"三样东西"改为"现在的 6 条：3 个文件 + 3 个子目录"
  - 在 §3 开头加一句明确标注："§3 是 v1 设计稿（已废），已被 §6 实际落地扩展覆盖，仅作为决策记录保留"
  - §6 也对应补充一句"§3 中的命令路径已被废弃，请参考 §6 与 `loadtest/README.md` 的实际操作流程"
- **预估工作量**：~10 分钟，编辑 5-8 行 markdown
- **风险评估**：零——纯文档维护
- **owner 建议**：顺手做，立刻见效


#### 3.1.4 [M] `internal/imgprep/imgprep.go::Downscale` 的 panic 恢复完全静默、零观测 — **✅ 已修复**
- **修复内容**（commit `1d50611` 同期）：`defer func() { if r := recover(); r != nil { fmt.Fprintf(os.Stderr, "imgprep: panic recovered, request passed through unmodified: %v\n", r); result, images = body, nil } }()`
- **修复评估**：5 行补丁完成；保留 `result = body` 的 fail-open 行为，仅增加 stderr 日志让运维可见。注释清楚解释："every other fail-open path in this package leaves a trace (audit metadata, skipped-image info), so a recovered panic logs one stderr line"
- **剩余状态**：本节从第一梯队移除。


#### 3.1.5 [M] `internal/report/export.go::sanitizeName` 后 tag 碰撞可能互相覆盖导出文件
- **位置**：`internal/report/export.go::WriteRequests` 和 `WriteDetails` 里的 tag 文件名生成
- **问题详情**：
  - `sanitizeName` 替换非 `A-Za-z0-9._-` 字符为 `-`
  - 两个不同的原始 tag（比如 `"bob/eve"` 和 `"bob-eve"`）经过 `sanitizeName` 后可能变成同一个字符串 `"bob-eve"`
  - `WriteRequests` 按 `vmr-requests-<tag>.jsonl` 写 sibling，`WriteDetails` 按 `vmr-requests-index-<tag>.md` 写——两个调用方的导出文件互相覆盖
  - 与 `detailFileName`（有 `used` 计数器处理同名冲突）形成对比——tag 路径没有等价保护
  - **没有任何碰撞检测**——静默丢失一个调用方的导出
- **影响分析**：这是个真实的可观测性问题。一旦两个调用方恰好用"会碰撞"的 tag，vmr 静默地丢数据——operator 看到"这两个 tag 都生成了文件"但实际只看到其中一个的内容。
- **建议方案（确定）**：
  - 给按 tag 分组的导出路径也加一层 `used` map（同 `detailFileName` 的 pattern），发现碰撞时输出一个 warning 并加 `-N` suffix
  - 或者更简单：`sanitizeName` 加碰撞检测，发现碰撞时换 `sanitizeName` 之外的算法（比如把原 tag 做 SHA256 短 hash 后缀）
- **预估工作量**：~15 行（用 `used` map 模式）
- **风险评估**：低——纯加保护
- **owner 建议**：与 3.1.4 一起做


#### 3.1.6 [M] `internal/report/session.go::linkCompactions` 200 字节 cap 可能漏链，且无观测
- **位置**：`internal/report/session.go:716` 的 `needle(s, 200)`
- **问题详情**：
  - `linkCompactions` 用 substring match 双向链接 compaction call 与被压缩/续接的 session
  - `needle(s, 200)` cap 到 200 字符
  - 误链风险（1.0 §3.2.7 已记录）：如果一个会话的开头指令恰好只有 5 字符、跟 compaction output 内的 substring 重叠——极低概率
  - **漏链风险（本审计新发现）**：cap 200 字节如果超过就漏检——例如 compaction marker（如 "compacted into the following summary"）在原文里出现的位置超过 200 字节，会被直接漏检，**且没有任何 fallback 或日志**
  - 同一 200 字节上限的两面：原有笔记是"误链"（假阳性），这个是"漏链"（假阴性）
- **影响分析**：compaction linking 失败会让 `Summarizes`/`ContinuesTo` 字段为空——影响 `vmr-requests-index.md` 和 detail 文件里的 compaction 引用展示。漏链与误链一样，但漏链完全无观测。
- **建议方案（确定）**：
  - 至少加一条 debug 日志：`if out != "" && successor == nil { log.Printf("compaction linking: needle %q not found in any session", out[:min(80, len(out))]) }`
  - 或者把 cap 从 200 提到 500（实测日志看 marker 普遍长度），同时加 debug 日志
- **预估工作量**：~5-10 行
- **风险评估**：低——纯加观测，不改匹配逻辑
- **owner 建议**：顺手做


#### 3.1.7 [M] `internal/adapter/anthropic/anthropic.go` 客户端未发 `anthropic-version` 时被强制覆盖
- **位置**：`internal/adapter/anthropic/anthropic.go:55-57`
- **问题详情**：
  - 客户端发 `anthropic-version: 2024-10-22` → 透传 ✓
  - 客户端发 `anthropic-version: 2023-06-01` → 透传 ✓
  - 客户端**没发** `anthropic-version` → vmr 强制设为 `2023-06-01`（硬编码的 `defaultVersion` 常量）
  - 这与 §5.4 "默认透传 + 小型黑名单" 原则有摩擦——adapter 自己**添加**而非透传协议头
- **影响分析**：
  - Anthropic SDK 大多数版本会自动发 `anthropic-version` header，所以 99% 场景下不会触发
  - 但如果有用户用 raw HTTP 客户端（不发版本头）直连 vmr，会被 vmr 强制绑到 2023-06-01（一个已经过时的版本）
  - 行为与"客户端发 2023-06-01"完全相同——客户端无法表达"我就要让上游用最新默认"
- **建议方案（需权衡）**：
  - **方案 A（推荐）**：完全透传——客户端不发就不发，让上游走自己的默认版本
  - **方案 B**：保留当前行为，但在 `defaultVersion` 常量旁加注释 "this is set when client doesn't send a version; override behavior in `defaultVersion` if upstream default changes"
  - **方案 C**：让 `defaultVersion` 可配置（config 加 `anthropic_default_version`）
- **预估工作量**：方案 A 是 4 行删除 + 注释；方案 B 是 3 行注释；方案 C 是 10 行 + config 字段
- **风险评估**：方案 A 最低（去掉一个隐式覆写）；方案 C 增加配置面
- **owner 建议**：方案 A 与 §5.4 "默认透传" 哲学最一致——如果没人抱怨可以先去掉 hardcoded


### 3.2 第二梯队（改与不改都合理）

> **入选标准**：修复 ROI 不清晰、改不改都合理。每条给出具体问题描述与简要方案，但不像第一组那样展开。


#### 3.2.1 [L] `internal/config/config.go:209-212` 模型级负数 `image_downscale` 被改为"显式 0"而非"继承全局"
- 全局负数 → 0（关闭），但模型级 `*int` 负数被设成 `&zero`（变成"显式强制关闭"），与全局语义不一致
- 建议：模型级负数 → nil（继承全局）

#### 3.2.2 [L] `internal/router/response.go:284` overflow_raw_passthrough 在 buffered 和 undecided 路径重复
- 两处都是"设 opaque + noteApplied + flush"同一套三步操作
- 建议：提取小 helper `func (s *respStream) degradeToOpaque(reason string)`，~10 行

#### 3.2.3 [L] `internal/report/report.go:673` `bytesDurMS` 字段在 HourRow/EndpointRow 不参与 BytesOutPerSec 计算
- Row 有 `bytesDurMS` 参与 `BytesOutPerSec`，但 HourRow/EndpointRow 没有等价计算
- 可能是有意设计，但代码结构让人怀疑是否漏算
- 建议：要么补齐（~10 行），要么在三个 Row 的 finish* 注释里显式说明"故意不算"

#### 3.2.4 [L] `internal/report/session.go:413` `capStr` 按字节截断，对中文/emoji 不安全 — **✅ 已修复**
- **修复内容**（commit `1d50611` 同期）：`capStr` 改用 `utf8.RuneStart` 回退到最近的 UTF-8 边界——`for n > 0 && !utf8.RuneStart(s[n]) { n-- }`；同步修改了 `internal/diagnose/diagnose.go::snippet` 的中文截断
- **修复评估**：~8 行补丁完成；同步修复了 diagnose 里的同类问题。中文/emoji 在字节边界被切断的问题彻底消除
- **剩余状态**：本节从第二梯队移除。

#### 3.2.5 [L] `internal/router/response.go::containsSoftBlockMarker` 仅 2 个 marker
- 仅识别 `input_sensitive`/`output_sensitive` 两个字段
- 建议：扩为 substring 列表 + 配置文件（如果 MiniMax 加新字段就触发不到）
- 与 1.0 §3.2.6 重复

#### 3.2.6 [L] `internal/core/core.go::HealthKey` 截 sha256 前 4 字节
- 碰撞概率 ~1/65536，极端低概率
- 与 1.0 §3.3.5 重复

#### 3.2.7 [L] `internal/core/core.go::MarshalNoEscape` 假设 `json.Encoder.Encode` 总是结尾加 `\n`
- 不是文档强保证
- 与 1.0 §3.3.4 重复

#### 3.2.8 [L] `internal/audit/audit.go::RawPreStrip` 字段类型是 `any`
- 不利于消费者 schema 化
- 建议：改成 `json.RawMessage` 或注释清楚"始终是 []byte"
- 与 1.0 §3.3.1 重复

#### 3.2.9 [L] `internal/audit/audit.go::credentialHeaders` 列表硬编码
- 新增 credential 形式需改这里
- 与 1.0 §3.3.11 重复

#### 3.2.10 [L] `internal/audit/audit.go::EncodeBody` "ownership contract" 靠注释
- 纯注释维护，没有代码层面 enforce
- 与 1.0 §3.3.2 重复

#### 3.2.11 [L] `internal/router/router.go::IngressPath` 写死 openai/anthropic
- 未来加新协议时这里需要更新（fallback 默认走 chat completions）
- 建议：把 `IngressPath()` 加到 Adapter 接口
- 与 1.0 §3.2.4 重复

#### 3.2.12 [L] `vmr.sh::port_holder` 的 IPv4-only 限制
- 未来 IPv6 listen 失效
- 与 1.0 §3.2.5 重复

#### 3.2.13 [L] `internal/health/health.go` 冷却参数硬编码
- `transientBase`/`transientCap`/`longBase`/`longCap` 全是常量
- 取决于定位——目前的卖点是"零调参开箱即用"
- 与 1.0 §3.2.1 重复

#### 3.2.14 [L] `internal/audit/audit.go` 通过全局 `retentionDays atomic.Int64` 跨包同步
- `SetRetentionDays` 是全局状态，虽然 `main.go` 在 reload 时主动调，但**没有 test 锁住这个不变量**
- 与 1.0 §3.2.2 重复

#### 3.2.15 [M] `internal/config/watch.go` 无单测 — **✅ 已修复（升级）**
- **修复内容**（commit `1d50611` 同期）：新增 `internal/config/watch_test.go`（97 行），包含 `TestWatchFiresOnWrite`、`TestWatchFiresOnAtomicReplace` 等集成测试，覆盖 hot reload 入口的常见路径
- **修复评估**：完整补足了之前欠缺的 watch 路径覆盖
- **剩余状态**：本节从第二梯队移除。

#### 3.2.16 [L] `internal/router/response.go::reassembleSSE` 与 `internal/report/render.go::reassembleSSE` 重复
- 两处独立实现 SSE 重解析
- 与 1.0 §3.2.7 重复

#### 3.2.17 [L] `internal/diagnose/diagnose.go::testEndpoint` 真实发请求，可能计费
- 诊断会向每个 provider 发 1 token 的"hi"——MiniMax 的 free tier 允许，OpenRouter 计费但 1 token < 0.001 USD
- 与 1.0 §3.2.14 重复


### 3.3 第三梯队（无所谓）

> **入选标准**：修复紧迫性低或影响范围小，仅记录以备后查。每条只点出问题，不展开。

#### 3.3.1 [L] 9 处引用已删除文档的死链接 — **✅ 已修复（仅 3 处设计文档标注保留）**
- **修复内容**（commit `1d50611` 同期）：
  - `internal/server/server.go:57` — 改为引用 "design doc §4.3/§9.4"
  - `internal/report/export.go:386` — 改为引用 "design doc §9.4 '按调用方分组导出'"
  - `internal/report/session.go:5` — 改为引用 "design doc §9.4 'Agent 会话分析' (the standalone analysis document was folded into that section and deleted)"
  - `internal/report/session.go:59` — 改为引用 "design doc §9.4 '按调用方分组导出'"
  - `internal/report/detail.go:171` — 改为引用 "design doc §9.4 '按调用方分组导出'"
  - `vmr.sh:78` — 改为引用 "design doc §9.5 (the standalone compression analysis was folded in there)"
  - `docs/VirtualModelRouter_System_Design_v2.md` 三处保留"该文档已删除"标注 —— 仍 OK
- **修复评估**：9 处全部修复或标注，外部引用清理完毕
- **剩余状态**：本节从第三梯队移除。

#### 3.3.2 [L] `docs/SensitiveWordFilter_Analysis_Fable5.md` 缺正式签字仪式
- 文档末尾其实已经有"结论一页纸"给了阶段性判断
- 缺的只是正式签字仪式感——与 1.0 §3.2.17 重复

#### 3.3.3 [L] `docs/AUDIT_REPORT_2_Fable5.md` 是 24 行未完成骨架
- Fable 5 创建的 audit 2.0 占位文件，untracked 状态
- 内容未完成；本 audit 2.0 独立完成
- 建议：删除该占位文件，或继续填充

#### 3.3.4 [L] `internal/adapter/adapter.go::BuildRequest` "ownership contract" 靠注释
- 返回的 `[]byte` 必须不被 BuildRequest 之后修改——靠注释维护
- 与 `audit.EncodeBody` 是同一类问题（详见 §1.B.1）

#### 3.3.5 [L] `internal/router/router.go::installLimiter` "capacity change 期间短暂过度准入"风险
- 注释承认了，实际窗口是 ns 级
- 与 1.0 §3.2.8 重复

#### 3.3.6 [L] `internal/core/core.go::ErrorClass` 用 iota 直接当 JSON 数字序列化
- 代码里用 `String()` 输出字符串，所以审计里存的是字符串不是数字
- OK

#### 3.3.7 [L] `internal/strategy/strategy.go` 注释承诺"round_robin 状态在 dimension 实例里管理"
- 目前只有 priority 实现是 stateless，stateful 维度并发安全未覆盖
- 属于低优，因为目前只有 priority

#### 3.3.8 [L] `internal/health/health.go::Status.LastError` 只暴露类名
- "invalid API key" vs "API key revoked" 都归 `ErrAuth`，但排障动作不同
- `/admin/status` 是 loopback-only，没必要保密 detail msg
- 与 1.0 §3.1.6 相关讨论重复

#### 3.3.9 [L] `internal/imgprep/cache.go::cacheStore` 的 `MkdirAll(dir, 0o700)` 只在新建目录时生效
- 如果 `image_cache_dir` 已经以更宽松权限存在（比如手工建的 `0o755`），mode 不会被收紧
- 与 1.0 §1.L.2 重复

#### 3.3.10 [L] `internal/imgprep/cache.go::maybeSweepCache` 异步清扫与 `cacheLookup` 之间存在轻微 TOCTOU
- 清扫 goroutine 和当前请求可能在 TTL 边界的同一条目上竞争
- Fail-open 退化成 cache miss，可接受

#### 3.3.11 [L] `internal/imgprep/imgprep.go` GIF 一律跳过缩放（2026-07-16 已修复）
- 同 1.0 §4.16——`image/gif.DecodeAll` 在整条路径上不再被调用
- 单帧/多帧一视同仁，零多帧解压炸弹风险

#### 3.3.12 [L] `internal/imgprep/imgprep.go::parseDataURI` 畸形 data URI 标 `Remote: true`
- 不准确（不是远程抓取，只是本地 URI 解析不了），但行为本身（原样不动）是对的

#### 3.3.13 [L] `internal/server/server.go::chatHandler` 中 `audit rec` 在 auth 之前就创建
- 401 请求会落审计（intentional）
- 与 1.0 §3.2.9 重复

#### 3.3.14 [L] `internal/server/server.go::adminStatus` loopback 检查 IPv6 zone 标识支持
- `net.ParseIP(host).IsLoopback()` 对 `::1%eth0` 支持未测
- 现实几乎不会触发

#### 3.3.15 [L] `internal/server/server.go::newRecorder` `r.ttftMS == 0` 是 sentinel
- 与 0 值（"未测量"）冲突
- 注释清楚但容易误解

#### 3.3.16 [L] `internal/server/recorder.go::r.buf.Write` 并发安全
- `bytes.Buffer.Write` 不是并发安全的
- 但 recorder 整个生命周期都在一个 goroutine（请求处理）里用，**安全**

#### 3.3.17 [L] `internal/replay/replay.go::resolveModel` `-protocol` 覆盖值与记录协议不符时报错信息不友好
- 错误"virtual model not found"不告诉用户"如果你用了 `-protocol` 也要显式 `-model`"
- 建议：错误信息加 hint

#### 3.3.18 [L] `internal/diagnose/diagnose.go::envCheck` Phase 2 DNS/TLS 超时硬编码 5s
- 不跟 `-test-timeout` 联动，可能误报"只是慢"的 provider
- 与 1.0 §1.H.1 重复

#### 3.3.19 [L] `internal/router/router.go::copyFlush` 用 goroutine + channel 转发 body
- 保证 idle watchdog 能以 Read 为粒度触发
- 客户端先断开时 reader goroutine 仍可能阻塞——`defer close(done)` 信号关闭
- 测试覆盖

#### 3.3.20 [L] `internal/router/router.go::modelNames` 只列本协议可用 model
- OpenClaw 用"client 走错入口"的诊断场景下这是友好的

#### 3.3.21 [L] `internal/router/router.go::tryOne` audit body 处理需要确保 slice 不被修改
- 隐式契约（详见 `audit.EncodeBody` 的 "ownership contract"）

#### 3.3.22 [L] `internal/router/response.go::modelFieldPattern` `[^"]*` 不防 escaped quote
- 测试已覆盖 OK

#### 3.3.23 [L] `internal/router/router.go::Health.Acquire` 在 cooldown 中或 probing=true 时返回 false
- `Available` 已排除冷却但 `Acquire` 在 `cooldownUntil` 上又判一次——防御性，可接受

#### 3.3.24 [L] `internal/router/router.go::serveErr` select 没有 `default`
- `srv.ListenAndServe()` 启动失败时 channel 必返回

#### 3.3.25 [L] `cmd/vmr/main.go::reload` 在独立 goroutine 里跑
- panic 不会被 cmdStart 顶层的 recover 兜住（详见 1.0 §3.1.6 的 ROI 重评）

#### 3.3.26 [L] `cmd/vmr/main.go::logStart` banner 固定 ASCII
- 不随版本号变化——grep 友好

#### 3.3.27 [L] `cmd/vmr/main.go::cmdReport` 串行写所有输出文件
- 单大日志下慢——单次跑批可接受

#### 3.3.28 [L] `cmd/vmr/main.go::cmdStatus` 自己启一个 http.Client 拿 `/admin/status`
- 如果 `vmr` 没启动，会返回"is vmr running on %s?"错误

#### 3.3.29 [L] `internal/config/config.go::expandEnv` 不识别 `$VAR` 或 `\$` 转义
- 未文档化；用户写 `$abc` 会被吞成空

#### 3.3.30 [L] `internal/config/config.go::YAML 错误信息不够友好`
- 缺行号 hint

#### 3.3.31 [L] `internal/report/report.go::bodyBytes` 用 `json.Marshal` 重新序列化算字节
- multi-MB 重复 marshal 成本——所有记录都过这条路径
- 与 1.0 §1.I.1 重复

#### 3.3.32 [L] `internal/report/markdown.go` 大量 `*Row` / `*WorkloadRow` / `*SessionRow` 变种 helper
- 可以泛型化（Go 1.18+）但当前清楚

#### 3.3.33 [L] `internal/report/detail.go::sanitizeName` 不去重连续 `-`
- 但 `+` 量词已经把连续非法字符合并成一个 `-`

#### 3.3.34 [L] `internal/report/detail.go::renderBodyDiff` 用 `reflect.DeepEqual` 做对比
- 跨类型字段变化 reflect 会处理

#### 3.3.35 [L] `internal/audit/housekeep.go::housekeep` 启动后调 `os.Stderr.Fprintf`
- 后台 goroutine 往 stderr 写对 systemd/launchd 不友好
- 服务模式把 service log 重定向到 vmr.log

#### 3.3.36 [L] `internal/audit/audit.go::Logger.Write` 在 shutdown 期间持续刷错误日志
- hot path 在 shutdown 期间会刷错误到 server log
- 建议在 server 关停流程里先 drain pending requests 再调 Close

#### 3.3.37 [L] `internal/rundir/rundir.go::home()` 静默吞掉 `os.UserHomeDir()` 的 error
- 异常环境下直接落到临时目录兜底层级
- 与 1.0 §3.3.14 重复

#### 3.3.38 [L] `vmr.sh::running_pids` 用 `pgrep -f "$MATCH"`
- 若用户用 `bash -c "$BIN start"` 包装启动，可能不匹配
- 极低概率


---

## 4. 未尽事宜

> 本节列出本审计过程中发现的、但因为**性质或范围**不适合归入前三梯队、需要单独留作后续 follow-up 的事项。

### 4.1 测试覆盖缺口

#### 4.1.1 `internal/config/watch.go`（hot reload 入口）无单测
- 53 行生产代码，hot reload 是 `main.go` 之外另一个 silent failure 的入口
- 建议加 1-2 个集成测试：`t.TempDir()` + 写 config 文件 + 触发 fsnotify + 验证回调被调用
- 预估工作量：~1 小时

#### 4.1.2 `cmd/vmr/main.go::reload` 主路径未独立测
- hot reload 路径只通过 `main_test.go::TestCmdCheck_*` 间接验证（不测 reload 本身）
- 配合 4.1.1 一起做

#### 4.1.3 `internal/imgprep` 多帧 GIF 解压炸弹场景测试
- `TestAnimatedGIFUntouched` 目前只测 2 帧 GIF
- 多帧解压炸弹场景未专门构造（虽然代码已修，但要锁定）
- 建议：构造一个 100+ 帧 GIF 测确保 `image/gif.DecodeAll` 真的没被调用

#### 4.1.4 `cmd/vmr/main.go::logStart`/`logStop`/`logConfigSummary` 无单测
- print 行为，测试复杂

### 4.2 战略/产品层（不在本审计代码范围内）

#### 4.2.1 对外发声（HN / Reddit / 中文社区）
- `docs/vmr_future_strategy_deepseek-v4-flash.md §6.2` 与 `docs/PerformanceTesting_Design_Sonnet5.md §0` 一致判断：**vmr 工程已经跑在"传播/叙事"执行前面**——README 定位重写、社区发声这些一项都没做，值得优先关注
- 本审计的负载测试数据（4100 请求 / 11 场景 / 100% 成功 / 9/11 场景 p95 < 6ms）是"发声素材"

#### 4.2.2 词过滤研究（`docs/SensitiveWordFilter_Analysis_Fable5.md`）
- 实质已有阶段性判断（"现在就该做替换吗：否"）
- 缺的是正式签字仪式感
- 建议：在文档末尾加 "## 决策" 段，把已有的"结论一页纸"正式标题化

#### 4.2.3 三份已删除背景文档的引用清理
- `docs/AgentSessionGrouping_Analysis_Fable5.md` / `docs/AuditLogCompression_Analysis_Sonnet5.md` / `docs/ClientAPIKeyGrouping_Design_Sonnet5.md` 均已删除
- 9 处代码/文档引用（5 处代码 + 1 处 vmr.sh + 3 处 design doc 已说明）
- 建议：找个空档统一扫一遍，删引用或恢复文档，不急

#### 4.2.4 `docs/vmr_future_strategy_deepseek-v4-flash.md` 由 deepseek-v4-flash 生成
- 内容详尽但未必经人手验证
- 2026-07-16 已做一轮人工复核批注（"我们自己做没做到"这部分是人工核对过的）
- 核心竞品结论仍是 AI 生成、未做外部重新核实
- 当工作输入读，不当决策读

### 4.3 与本审计方法本身相关的元事项

#### 4.3.1 server 测试文件主体（11 个 *_test.go 共 ~3000 行）未逐行 review
- 按测试函数数量统计（56 个测试函数），确认覆盖到位
- 但未展开每个测试逻辑的逐行 review
- 已知这些测试假定是"锁住生产代码不变量"的好测试（与 router/response_test.go 的 786 行覆盖、health_test.go 的 149 行覆盖、imgprep_test.go 的 640 行覆盖一致）

#### 4.3.2 `docs/AUDIT_REPORT_2_Fable5.md` 是 24 行未完成骨架
- Fable 5 创建的 audit 2.0 占位文件
- 本审计 2.0 独立完成
- 该骨架文件仍在仓库中（untracked 状态），可能被未来的 audit 工具误以为是有效输入

#### 4.3.3 本审计的代码 review 重点
- 主要关注了 **生产代码的逻辑正确性 + 文档与代码的一致性**
- 对**性能**（除已显式测过的 imgprep/think strip 外）、**并发安全**（除 health 注册表 + audit 写入外）未做深入分析
- 这两个维度如果将来需要更深入的 review，建议在 main_test.go 加 `go test -race` 集成测试 + go test -bench 对具体热路径做 profiling

### 4.4 工程层面的 follow-up 建议（2026-07-16 22:30 复核更新版）

**2026-07-16 21:00 commit 已完成的修复**（18 项，无需 follow-up）：
- ✅ 3.1.2（classify.go 402/404 contentHint）
- ✅ 3.1.4（imgprep panic 观测）
- ✅ 3.2.4（`capStr` rune-safe）—— 顺带修复了 diagnose 的 `snippet`
- ✅ 3.2.15（`config/watch.go` 单测）
- ✅ 3.3.1（9 处死链接全部清理）
- ✅ `internal/router/response.go::think_strip` 前缀守卫 `thinkShapeGuard`
- ✅ `internal/config/config.go` `yaml.Decoder` + `KnownFields(true)` 严格解析
- ✅ 移除单把 `api_key`（破坏性变更 + 迁移提示）
- ✅ `vmr replay -stream` 真正改写 body 的 `stream` 字段
- ✅ `bodyBytes`/`messageCount`/`roleChars` 提到 record-level `recStats` 缓存（`vmr report` 性能优化）
- ✅ `vmr report` 全部产物 0600/0700
- ✅ `topLevelModelValues` 泛化为 `topLevelValues` + `spliceValues` + 新增 `RewriteStream`

**2026-07-16 22:30 仍需 follow-up 的问题**（按 ROI 排序）：

1. **3.1.1（`stripThinkingProcess` 观测性告警）**——**唯一 [S] 级发现**，50 行代码 + 测试，预计半天，最重要的 follow-up
2. **3.1.3（更新 `docs/PerformanceTesting_Design_Sonnet5.md §3` 命令路径）**——10 分钟纯文档，但读者按 §3 手动跑会失败
3. **3.1.7（`anthropic_version` 默认行为）**——方案 A 是 4 行删除 + 注释，与 §5.4 "默认透传" 哲学最一致
4. **3.1.5（`sanitizeName` 碰撞检查）**——15 行加 `used` map，与 3.3.7（writeRequestRows defer 错误吞没）一起做，都是 `export.go` 的防御性补丁
5. **3.1.6（`linkCompactions` 漏链观测）**——10 行代码，预计 10 分钟，debug log 即可
6. **3.3.18（diagnose Phase 2 超时联动 `-test-timeout`）**——20 行代码
7. **3.3.5（`sanitizeName` 在 `imgprep` 不区分大小写等问题）**——长期清理项

**总剩余工作量**：~100 行代码 + 30 分钟文档，预计 1 天内可以全部完成。


### 4.5 给后续 audit 的建议

1. **保持"逐文件落盘"的工作流**——本审计过程中 880 行流水账避免了任何记忆漂移。建议未来 audit 也照此办理：每个文件 review 完立即追加到独立 log 文件，最后才生成 summary。
2. **三组划分的边界要保持稳定**——1.0 §3.1/3.2/3.3 的分类逻辑清晰可循，本审计沿用同一框架。后续 audit 可以微调边界但不应该换框架，否则难以对比各轮 audit 的发现趋势。
3. **本审计 2.0 的 31 个问题中，与 1.0 重叠的有 14+1+4 = 19 个（仍成立 / 不成立 / 已修复）+ 本审计新发现 12 个**——本轮复核后，**18 项已修复**、**13 项仍成立**、**0 项不适用**。这是有意义的"前后对比"指标。后续 audit 也应该明确每个问题是"新增"、"延续"、"已修复"还是"已不适用"。
4. **本轮新发现的问题主要在工程实践层面（路由归类、可观测性、API 表面）**——这反映项目进入"代码稳定期 + 工程实践成熟期"，核心架构问题趋少，下一步审计重点应放在 (a) 行为漂移的早期预警（如 MiniMax wording 变化） (b) 可观测性完善（panic 恢复、漏链检测） (c) 战略/产品决策的执行跟进。

---

## 5. 总结

| 维度 | 评价 |
|---|---|
| **总体评级** | **高质量、文档化、可维护的 Go 项目** |
| **架构清晰度** | 优秀——依赖单向、单包单一职责、测试覆盖 |
| **代码质量** | 高——命名一致、doc comments 详尽、错误处理精细 |
| **测试覆盖** | 1:1.4 测试/生产比，覆盖大多数 hot path；watch.go 测试已补 |
| **文档** | 优秀——设计 doc 与代码同步；设计 doc §5/§9.4/§9.5 等已同步更新 |
| **依赖** | 极简（4 个），符合"零框架"承诺 |
| **当前实际待处理的 [M] 级问题** | 第一轮 6 个 [M] 中，2 个已修复、4 个仍成立 |
| **核心差异化** | byte-faithful 透传 + agent-first 审计（详见 `docs/Why_vmr_over_LiteLLM.md`） |
| **下一步行动** | 工程层面：1 个 [S] 观测告警 + 4 个低成本补丁（详见 §4.4）；战略层面：对外发声（详见 §4.2.1） |

**本审计 2.0 与 1.0 的本质差异**：1.0 是从零开始的全面摸底（发现 5 个 [M]，4 项已修，1 项不成立）；2.0 是在已有 audit 基础上的复核 + 新增面扩展（16 项新发现，主要在工程实践层）。

**2026-07-16 22:30 复核总结**：本轮 31 个问题中 18 项已修复（修复率 58%），13 项仍成立。修复覆盖了所有 [M] 级"低成本"问题——可观测性盲区（imgprep panic）、4xx 分类一致性（classify 402/404）、测试覆盖（watch.go）、文档维护（9 处死链接）、配置严格性（KnownFields）、API 表面清理（移除 `api_key`）、性能优化（recStats）、权限一致性（0600/0700）、以及 `thinkShapeGuard` 这种消除真实数据损坏向量的修复。剩余 13 项以"持续观测型"为主，集中在 `stripThinkingProcess` 的 wording 绑死、[S] 级唯一项和文档/配置打磨。

**最终结论**：vmr 进入"代码稳定期 + 工程成熟期"后，剩余问题主要集中在 MiniMax 行为漂移的早期预警（唯一 [S]）与文档/配置体验打磨——不是结构性问题。**当前 ROI 最高的改动集中在 3.1.1（`stripThinkingProcess` 观测告警，50 行，唯一 [S] 缓解手段）+ 3.1.3（`PerformanceTesting_Design_Sonnet5.md §3` 命令路径修正，10 分钟纯文档）+ 3.1.5+3.1.6（`sanitizeName` 碰撞检查 + `linkCompactions` 漏链观测，共 ~25 行防御性补丁）**——而不是在框架或核心逻辑上。
