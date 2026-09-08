<!-- Ver 2026-09-09, by Sonnet 5 -->

# vmr 全局 Review 与清理报告

**执行者**：Sonnet 5（claude-sonnet-5）
**日期**：2026-09-09
**依据**：`docs/prompts/prompt-project-cleanup.md`

**范围铁律**：不改变任何外部可观测行为（对外 API、输出格式、报表内容、审计记录结构、配置语义均不变）。
不做功能增强、不做行为优化。功能性问题只登记不动手。忽略 `_tmp/ archived/ logs/ reports/ _review/ _eval/`。

---

## 1. 基线与已知权衡地图

### 1.1 基线结果（2026-09-09，go1.26.5 darwin/arm64）

| 检查 | 首次结果 | 处理后 |
| --- | --- | --- |
| `go build ./...` | ✅ | ✅ |
| `go vet ./...` | ✅ | ✅ |
| `gofmt -l .` | ✅ 干净 | ✅ 干净 |
| `go test ./...` | ❌ `internal/archtest` 2 项失败 | ✅（见批次 A0） |
| `go test -race`（health/audit/router/quota/sticky/server/respnorm/cmd） | ✅ | ✅ |
| `go test ./internal/archtest/...` | ❌ | ✅ |

绿色安全网自 A0 起建立。除 A0 涉及的 archtest 失败外，基线其余项首次即绿。

**基线失败详情（A0，已修复）**：`c5a05bc remove obsolete docs` 删除了
`docs/future-strategy/analyze_architecture_redesign_opus-5.md`，但 9 处源码注释（+1 处测试夹具）
仍引用它，触发 `TestArchitecture_DocReferences_SourceComments` / `_Negative` 失败。
这是标准 A 类文档漂移，作为第一个 commit 修复以建立绿色安全网。

### 1.2 已知权衡地图（决策考古结论）

通读 `CLAUDE.md`、`docs/KNOWN_ISSUES.md`（§1 刻意取舍 / §2 待办 / §3 优先级）、
四份设计文档的决策与取舍表。核心结论：

**项目哲学高度一致**：KISS / YAGNI / 单二进制 / 零代码侵入 / 字节保真透传。
大量"看起来像死代码/可简化"的东西是**有记录的刻意保留**，命中即不改，只登记"核实仍成立"。

**明确不可触碰的"看似死代码，实则保留"清单**（来自 KNOWN_ISSUES §1）：

- `health.Registry.Available` — 无生产调用方，但是唯一无副作用的路由资格查询，测试依赖。
- `ctxgraph.Manifest.MsgIdx` — 无生产消费者，是包外验证内容寻址不变量的唯一通道。
- `respnorm` 观测标记 `crlf_framing_suspected` / `thinking_process_pattern_detected` — 被 `reqdetail` / `report` 消费。
- `respnorm.Read` 返回 `(0, nil)` — 唯一消费方 `copyFlush` 显式处理，刻意设计。
- `ReleaseProbe` vs `ReportNeutral`（行为相同，语义不同，刻意两个方法）。
- `i18n` 一批微文件不合并（archtest 强制与 `report/viewmodel_*.go` 一一配对）。
- `i18n` 的 `type XxxText` + `if lang == ZH` 样板不改写成 `map[Lang]T`。
- `core/core.go` 不按领域拆文件；`core` 准入例外清单（`HealthKey`/`Name`/`Freeze`）是显式豁免。
- `internal/probe` 不登记进 `zeroInternalDepPackages`。
- `adapter` 协议字段字面量不从 `jsonscan` 导出复用；`jsonscan` 改写函数不迁 `adapter`（Q17）。
- `internal/report/cost.go` 端点标签切分不并入 `core.SplitEndpointLabel`。
- `core.StickyBackstopTTL` 不迁回 `internal/sticky`。
- 降级 token 估算请求侧/响应侧 fallback 刻意不对称。
- `archtest` 包边界守卫单向，不加反向守卫；不加圈复杂度检查。
- 行数预算是"提醒式绊线"，未触线不焦虑，触线按职责拆分或临时调高豁免。

**已评估并否决的简化提案**（Quota §12.2、Core §11、Analytics §5/§7）：SWRR、预定义 `Source` 接口、
额度硬熔断、`mode: shared|per_model` 字段、多处"统一实现"提案等——重提前必须有新理由。

**§2 待办**：几乎全是"登记待触发"的性能项（大语料内存/耗时），均为 D 类，只登记不做。

### 1.3 KNOWN_ISSUES 自身的文档缺陷（盘点中发现）

- **§2.55 编号冲突**：两个不同条目都编号 `2.55`（`journey.BuildAll` 内存项 / `CHANGELOG 发版前归整`）。
- **孤儿条目**：`funcLineExemptions` 键设计那条（约在 §2.55 CHANGELOG 条目和 §2.79 之间）丢失了 `#### 2.xx` 标题。

（分类与处理见 §2。）

---

## 2. 清理候选清单（分类 / 证据 / 风险）

> A=直接修（零风险）｜B=机械清理（已证实等价）｜C=结构性简化（需完整推理 + archtest）｜D=只登记不做

### 已确认候选

| ID | 分类 | 位置 | 描述 | 证据 | 风险 |
| --- | --- | --- | --- | --- | --- |
| A0 | A | 9 .go 文件 + `doc_refs_test.go` | 引用已删除的 `analyze_architecture_redesign_opus-5.md` | archtest 失败；`git log` 确认 `c5a05bc` 删除 | 无（注释 + 测试夹具字符串）— **已完成** |
| A1 | A | `internal/journey/findings.go:68` | 注释中 tab 拼接错位（`slices\t// matches,`） | 肉眼可见的合并事故 | 无 — **已完成**（与 A0 同 commit） |

### 主控 deadcode 扫描（`golang.org/x/tools/cmd/deadcode -test ./...`，权威列表 21 项）

以下为"含测试根仍不可达"的函数，待各 Worker 交叉核实间接引用后归类：

| 候选 | 位置 | 初判 | 备注 |
| --- | --- | --- | --- |
| `BenchmarksReportFile` | `journey/benchmarks.go:43` | B（死） | 导出，零引用 |
| `JourneyDigestHex` | `journey/digest.go:28` | B（死） | 导出 hex 包装，零引用；`ComputeJourneyDigest` 测试在用 |
| `journeyAnthropicCoverageNote` + `journeyAnthropicCoverageCodes` | `journey/benchmarks_coverage.go:81,93` | B（死对） | 被 viewmodel 层 `vmAnthropicCoverageCodes` 取代；`viewmodel_build.go:238` 注释需同步 |
| `parseManifestBody` | `journey/journey_stepfacts.go:28` | B?（死） | doc 称"buildFrom's preamble"但 buildFrom 不调用——需核实 |
| `stepContextPoint` | `journey/journey_stepfacts.go:98` | B?（死） | `metrics.go:306` 仅注释引用——需核实 ContextPoint 现由谁算 |
| `ExportMacroSlices` | `report/export.go:71` | 待核实 | 导出，"写 5 个 macro 切片"——生产是否另有写入路径？ |
| `ParseTimePoint` | `report/manifest.go:76` | B?（死） | 零引用 |
| `HashBytes` | `report/manifest.go:349` | B?（死） | 零引用，疑被 `internal/digest` 取代 |
| `sanitize` | `report/render_cells.go:192` | 待核实 | 需查 |
| `turnCell`/`msgsCell`/`msOrDash`/`freshCachedOut`/`cacheEffTurn` | `report/requests.go` | B（死） | 已删除的 `vmr-requests*.md` 表的 cell 渲染器；`failed.md` 只用子集。`WriteRequestsJSON`/`finishCell` 疑测试专用 |
| `classifyEvent` | `respnorm/respnorm.go:856` | ⚠️ 高度谨慎 | 字节保真关键路径；`respnorm.go:271-277` 注释描述其存在理由，测试有注释引用——需彻查是否真死 |
| 测试辅助 5 个 | `journey/viewmodel_test.go`, `report/viewmodel_golden_test.go`, `tools/gen_standard_pricing/main_test.go` | B（死辅助） | 死测试 helper |

### 其他主控发现

| ID | 分类 | 位置 | 描述 |
| --- | --- | --- | --- |
| L-1 | A | `internal/audit/legacy_protocol.go:38` | `// TODO(2026-10): ...remove` 的日期框架与 KNOWN_ISSUES §1.2 冲突（拆除条件是"语料 grep 零命中"这个事实，不是日期） |
| L-2 | B/决策 | `NOTES_FOR_LEAD.md`（已提交） | 内容（L2/L3 缓存 CHANGELOG 建议）已全部落地 CHANGELOG；按 multi-agent 约定此文件本不应提交。倾向删除，升级决策 |
| L-3 | 登记 | `docs/KNOWN_ISSUES.md` | §2.55 编号冲突（两条）；`funcLineExemptions` 那条丢 `#### 2.xx` 标题 |

（各 Domain Worker 侦察报告汇总中。）

---

## 3. 执行计划与批次

| 批次 | 分类 | 内容 | 状态 |
| --- | --- | --- | --- |
| A0 | A | 修复 A0 + A1（stale doc refs + 注释错位），绿化基线 | 进行中 |

（后续批次待盘点完成后规划。）

---

## 4. 分批执行与验收记录

### 批次 A0 — stale doc references（绿化基线）

- **改动**：9 处源码注释删除对已删除文档的悬空引用，`aggregate.go` 保留对 Analytics 设计文档的泛化引用；
  `findings.go` 修复注释 tab 错位；`doc_refs_test.go:413` 正例夹具改指向存在的 future-strategy 文档。
- **验收**：`go build` ✅ / `go vet` ✅ / `gofmt -l` ✅ / `go test ./internal/archtest/...` ✅ / `go test ./...` ✅
- **行为保持**：纯注释 + 测试夹具字符串，无逻辑变更。

---

## 5. 总结与 D 类保留清单

（任务完成后填写。）

### 待用户决策清单

（自主决策规则下升级留档的事项，任务结束时汇总。）
