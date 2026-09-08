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

### 汇总候选清单（6 个 Domain 侦察 Agent + 主控 deadcode 扫描 + 决策考古）

工具：`golang.org/x/tools/cmd/deadcode`（`-test` 与非 `-test` 两遍）、全仓 `git grep` 跨包引用、逐消费点核实、KNOWN_ISSUES §1/§2 与四份设计文档决策表对照。

#### B 类 — 已证实死代码（机械删除）

| ID | 位置 | 描述 | 证据 |
| --- | --- | --- | --- |
| B-J1 | `journey/digest.go` 全文 + `digest_test.go` | `ComputeJourneyDigest`/`JourneyDigestHex` 零生产调用方；§7.2 单-journey 指纹从未接线 | deadcode 两遍；3 个 Agent 独立确认；先例 §1.4 line 143 删 `router.TokenCounters` |
| B-J2 | `journey/render_md_sysprompt.go`、`journey/render_modelusage.go` | 墓碑文件（仅 `// Deprecated` 注释 + `package journey`，零声明） | `grep -c "func\|var\|const\|type"` = 0 |
| B-J3 | `journey/journey_stepfacts.go:28` `parseManifestBody` | 被 `parseManifestBodyIncremental` 取代（journey.go:454） | deadcode 两遍；已核实替代者 |
| B-J4 | `journey/journey_stepfacts.go:98` `stepContextPoint` | 被 `stepFactState.updateContext` 取代（journey_stepfacts.go:71） | deadcode 两遍；已核实替代者 |
| B-J5 | `journey/benchmarks_coverage.go:81,93` `journeyAnthropicCoverageNote`/`Codes` | 被 viewmodel 层 `vmAnthropicCoverageCodes` 取代 | deadcode 两遍；`anthropicCoverageNote`（语料级）仍现役，勿误删 |
| B-J6 | `journey/benchmarks.go:43` `BenchmarksReportFile` + 常量 `BenchmarksFile`/`BenchmarksJSONFile` | 死函数 + 仅被它引用的孤儿常量（cmd/vmr 用字面量） | deadcode；`git grep` 常量零外部引用 |
| B-J7 | `journey/viewmodel_test.go:90,129,158` | `vmEquivalenceSysChangeFixture`/`boolStr`/`jsonRoundTripSummary` 死测试 helper | deadcode `-test` |
| B-R1 | `report/requests.go:156,163,170,177,196,203` | `turnCell`/`msgsCell`/`msOrDash`/`finishCell`/`freshCachedOut`/`cacheEffTurn` — D7 退役 markdown 索引族遗留 cell 渲染器 + `TestFinishCell` | deadcode 两遍；`orDashModel`/`outcomeCell`/`detailCell`/`sessTaskCell` 仍现役勿动 |
| B-R2 | `report/export.go:71` `ExportMacroSlices` | 纯转发包装（cmd 直接调 `WriteMacroSlices`） | deadcode 两遍；主控核实 |
| B-R3 | `report/manifest.go:76,85,349` `ParseTimePoint`/`TimePoint.Time()`/`HashBytes` | 全仓零调用方（含测试） | deadcode + git grep |
| B-R4 | `report/render_cells.go:192` `sanitize` | 零调用方 | deadcode 两遍；主控核实 |
| B-R5 | `report/viewmodel_doc.go:75` `report.Markdown` | 单行别名 of `MacroMarkdown`，零调用方 | deadcode |
| B-R6 | `report/viewmodel_golden_test.go:78` `withSessionErrs` | 死测试 helper | deadcode `-test` |
| B-N1 | `respnorm/respnorm.go:856` `classifyEvent` | 4 行死包装，生产用 `classifyEventAcc`（line 524） | deadcode `-test`；主控 + RI Agent 独立确认；测试引用仅注释 |
| B-N2 | `respnorm/usagesniff.go:25` `var messageStartMarker` | 死包级 var，side 分类已下沉 `chatmsg.ExtractUsageSides` | git grep 仅声明行 |
| B-C1 | `chatmsg/usage.go:138` `var messageStartMarker` | 死包级 var，判定走 `isAnthropicMessageStart`；订正 usage.go:134-137 注释 | git grep 仅注释 + 定义 |
| B-P1 | `pricing/resolve.go:248` `Complete(spec)` 包级函数 + `resolve_test.go:TestComplete*` | `metric: cost` 加载期硬门遗留——KNOWN_ISSUES §2.58a 明记"随它一起删除后" | git grep 无生产调用方；`Rate.Complete()` 方法仍现役 |
| B-CF1 | `config/pricing.go:46-47` `MapLegacy`/`OverridesLegacy` 字段 + 2 处赋值 | 只写不读（兼容逻辑用局部 `raw.MapOld`/`OldOver`） | git grep 仅 4 行；注释自承投机 |
| B-Q1 | `quota/period.go:144` `PeriodEnd` + 测试改取 `PeriodBounds` 第二值 | 生产零调用（走 `PeriodBounds`，F9） | deadcode 非-test；无 KNOWN_ISSUES 背书 |
| B-S1 | `sticky/sticky.go:72` `NewBounded` | 测试专用构造器（注释谎称 "resource-constrained setups" 用） | deadcode 非-test；生产唯一构造器 `sticky.New()` |
| B-I1 | `imgprep/cache.go:127` `const defaultCacheCapBytes = DefaultCacheCapBytes` | 无意义未导出别名 | git grep |
| B-D1 | `digest/digest.go:49,62` `DigestHex`/`EncodeUint64` | 零非测试引用（`EncodeInt64`/`Bool`/`Float64`/`String` 在用） | deadcode；YAGNI |
| B-T1 | `dashboard/dashboard_test.go:335-358` `replaceAll`/`indexOf`/`contains` | 手工重造 `strings.ReplaceAll`/`Index`/`Contains`，24 行 | 功能与 stdlib 完全一致 |
| B-I18N | `i18n` 死字段：`report_toolwaste.go` (13/17)、`journey_indicators.go` (14/18)、4 个 `*Close`、`report_doc.go:49 PerClientLabel` | 随 D6 自包含 HTML / 标签改由 `MetricLabel` / 序列化器硬编码而失效 | 逐字段 report/journey 零 `.Field` 访问；archtest 仅查文件配对 |
| B-CMD1 | `cmd/vmr/cmd_report_journeys_link.go` `loadStoriesLink`→`loadJourneysLink`、局部 `storiesLink`、注释 `ensureStoriesDir` | 退役 `vmr story` 遗留命名（全 unexported/局部） | git grep；纯重命名 |

#### A 类 — 注释/文档修正（零风险）

| ID | 位置 | 描述 |
| --- | --- | --- |
| A-1 | `router/router.go:135` | 注释 tab 拼接错位（同 A1 型），136-138 悬空 |
| A-2 | `audit/legacy_protocol.go:38` (=L-1) | `// TODO(2026-10): remove` 日期式拆除时点，与同文件 doc + KNOWN_ISSUES §1.2 冲突 |
| A-3 | `diagnose/diagnose.go:63` | `Result.Phase` doc 漏 `check` phase |
| A-4 | `config/quota.go:36-39` | `nonNegativeFinite` doc 提已删的 `Counters.Cost` |
| A-5 | `health/health.go:284-293` | `Status.Available` doc "backward compatibility with existing consumers" 误导 |
| A-6 | `chatmsg/messages.go:40-43`、`ctxgraph/stitch.go:122-124` | 注释病句（坏编辑残留） |
| A-7 | `dashboard/dashboard.go:89-93` | `AssetNames` doc 称有 analyze wiring 调用方，实际仅测试用 |
| A-8 | `chatmsg/sse.go:20`、`reqdetail/diff.go:3`、`reqdetail/render.go:125` | 代码注释混入中文（违反 CLAUDE.md「comments are English」） |
| A-9 | `loadtest/addr/addr.go:8` | 引不存在的 KNOWN_ISSUES "§3 loadtest entry" |
| A-10 | `cmd/vmr/cmd_analyze.go:14`、`selftraffic.go:29-31` | 注释引已不在仓库的 ActionPlan / 过期的 `-llm-key added P15.3` |
| A-11 | `tools/gen_standard_pricing/main.go:11`、`check_pricing.go:28-30` | flag 示例注释过期/自相矛盾 |
| A-12 | `archtest/doc_refs_test.go` ~L88 | 注释示例 `i18n/report_doc.go's "[vmr-requests.md](...)"` 已不存在 |
| A-13 | AP-11/RJ-15 stale redesign 引用批（散，~18 处）：`dashboard.go:4,90`、`digest/digest.go:4`、`i18n/journey_spine.go:42`、`i18n/reqdetail_detail.go:69,70`、`journey/compare_metrics.go:149`、`journey/finding_tier.go:4`、`journey/render_indicators.go:1`、`journey/render_spine.go:3`、`journey/viewmodel.go:3`、`journey/metrics.go:305-306`、`report/requests.go:214-215`、`report/session.go:534`、`reqdetail/detail.go:60,88,301,531,571`、`reqdetail/ensure.go:92` | 钉死已删 redesign 文档的 P/§ 编号；改按名引用或删悬空标记。D3/D4/D11/D12 等仍有效的设计决策 ID 保留 |
| A-14 | `adapter/fingerprint.go:14-29`、`imgprep/imgprep.go:94` | 注释计数不符 / 反引号笔误（极低价值，顺带） |
| A-D1 | `docs/KNOWN_ISSUES.md` | §2.55 编号冲突（renumber 一条）；`funcLineExemptions` 条目补 `#### 2.xx` 标题；§1.3「五个入口」→ 四（`PreviewTitle` 已不存在） |
| A-D2 | `docs/VirtualModelRouter_Design_v4_Core.md` §14 表 | 「健康注册表删除端点跨热重载残留…复杂度不成比例」行失效（`health.Registry.Prune` 已实现 + snapshot.go:388 调用），删行 |
| A-D3 | `docs/KNOWN_ISSUES.md` §1.4 line 128 | B-J1 落地后「report/journey 均为其调用方」→「report 为唯一调用方」 |

#### C 类 — 结构性简化

| ID | 位置 | 描述 | 处理 |
| --- | --- | --- | --- |
| C-1 | `reqdetail/render.go:144` `roleStatLine(chars, withChars, bold)` | 生产恒传 `true,true`，`false` 分支只测试走 | 内联 flag + 删死分支 + 更新测试。做，archtest 验收 |

#### D 类 — 只登记不做

| ID | 位置 | 描述 | 不做的理由 |
| --- | --- | --- | --- |
| D-1 | `ctxgraph` scan.go/cache.go/records.go 有界 worker 池样板×4 + `FetchRecords`/`ForEachRecord` 重复 | 等价重构，中等工作量，触并发核心 | 应作独立专项任务 + 完整 `-race`/archtest 复审，非清理 pass drive-by |
| D-2 | `router/snapshot.go` `BuildQuotaSpecs`/`BuildQuotaSpecsDisabled`/`ProviderLimits` 内层 `resolveLimits` 循环×3 | 抽私有 helper 可去重 | 触 quota parity；§2.81 已明定双形态刻意，改动易引发再论证 |
| D-3 | `respnorm` `think_pattern_detected`（RI-5）/ `truncated_flush`/`truncated_withheld`（RI-6）无 i18n 消费方 | 观测标记与消费方脱节，详单页落"(unknown step)" | 补 `diagnosticNormMarker`/i18n 描述 = 改分析产物契约与详单输出 = 行为变更，超范围 |
| D-4 | `quota` `Headroom`/`UsedFrac`/`PerModelPrefix`、`tokenutil` `AnalyzeString`/`IsCJK`/`IsEnglishSymbol`、`fmtutil.CurrencySymbol`、`pricing.Rate.MissingComponents` | 导出但仅包内 + 测试用 | 收窄可见性收益极低，扰动"score math fully unit-testable"取向的测试；`MissingComponents` 可能是 §2.58b 脚手架 |
| D-5 | `report/manifest.go:355` `HashFile` vs `ctxgraph.HashFile` 字节等价 | manifest 自包含可能是刻意 | 与 §1.4「core/cost.go 端点标签不并入」同型判断，需单独评审 |
| D-6 | `cmd/vmr/*.go` 57 处 pinned `§N.M`/`P<n>.<x>` 阶段引用 | 违反 CLAUDE.md「generic not pinned」 | 既有风格，量大，批改高 churn 低价值（A-13 只处理指向"已删符号/文档"的悬空引用） |
| D-7 | `router/pin.go` `applyPin` 第二返回值 bool 仅测试消费 | 生产 `out,_:=` 丢弃 + 先判 active | 收益小，牵动 pin_test |
| D-8 | `ctxgraph/manifest.go:389-394` `fastRawDigest` 死 `case int`/`int64` | json 只产 float64 | 仅缓存 key 摘要，命不中只回退重算，无正确性影响 |
| D-9 | `report.Build`/`report.AnalyzeSessions`/`journey.Build` 测试专用薄包装（=RJ-14） | §1.4 line 143 先例应删 | 被大量测试用作简单入口，删除 = 大量测试改写、覆盖清晰度下降；改 doc 注释为 "test-only convenience" 即可（并入 A 类） |

### 待用户决策清单（升级留档）

| # | 事项 | 各方案权衡 | 倾向 |
| --- | --- | --- | --- |
| DEC-1 | `NOTES_FOR_LEAD.md`（已提交，`3946c5e`）是否删除 | 内容（L2/L3 缓存 CHANGELOG 建议）100% 已落地 CHANGELOG；multi-agent 约定此文件不提交；CLAUDE.md「one-off review 报告不再产出」。留着零成本但是 cruft；删除 git history 可恢复 | **删除**（独立 commit，易 revert） |
| DEC-2 | KNOWN_ISSUES §1.3「`/help.html` 不做服务端模板渲染」表述是否收窄 | `server/help.go` 实际对 `{{BASE_URL_*}}` 做服务端 `bytes.ReplaceAll`；不涉密，与"服务端拿不到用户 Key"论据不矛盾。可能是 doc 滞后于代码演进 | 需先核实是 doc 滞后还是代码越界；若前者，收窄 §1.3 表述为「Agent 配置片段/model 列表在浏览器就地装配」 |
| DEC-3 | `report.Build`/`AnalyzeSessions`/`journey.Build` 三个测试专用薄包装是否按 §1.4 line 143 先例删除 | 先例明确（`router.TokenCounters` 因"零生产调用方仅测试保活"被删）；但此三者被大量测试用作简单入口，删除需重写多处测试、且无"partial 当 exact"那类语义危险 | 本轮只修 doc 注释误导（"every caller uses"→"test-only"），删除留待专项 |

---

## 3. 执行计划与批次

原则：无依赖独立项先行；同包变更聚合成批；每批一次 commit（短、祈使、无 trailer）；每批复跑验收；
死代码删除批含"与被删代码直接绑定的注释更新"，独立的陈旧注释归 A 批。主控串行 inline 执行。

| 批次 | 分类 | 包 | 内容 | 状态 |
| --- | --- | --- | --- | --- |
| A0 | A | 多 | stale doc refs + 注释错位（绿化基线） | ✅ 已完成（commit 9738399） |
| B1 | B | `journey` | B-J1..J7（digest.go/墓碑文件/stepfacts/coverage/benchmarks/测试 helper）+ 绑定注释 | 待执行 |
| B2 | B | `report` | B-R1..R6（requests cell/export/manifest/render_cells/viewmodel_doc/测试 helper） | 待执行 |
| B3 | B | `respnorm` `chatmsg` | B-N1、B-N2、B-C1（classifyEvent + messageStartMarker×2）+ 注释订正 | 待执行 |
| B4 | B | `pricing` `config` `quota` `sticky` `imgprep` | B-P1、B-CF1、B-Q1、B-S1、B-I1 + A-4（quota 注释） | 待执行 |
| B5 | B | `digest` `i18n` | B-D1、B-I18N（死字段） | 待执行 |
| B6 | B | `cmd/vmr` | B-CMD1（loadStoriesLink 重命名）+ `dashboard_test.go` B-T1 | 待执行 |
| C1 | C | `reqdetail` | C-1（roleStatLine flag 内联）+ archtest | 待执行 |
| A1 | A | 多（代码注释） | A-1、A-3、A-5、A-6、A-7、A-8、A-10、A-11、A-12、A-13、A-14 | 待执行 |
| A2 | A | `docs/` | A-2（audit TODO 措辞）、A-D1、A-D2、A-D3 | 待执行 |
| A3 | A/决策 | root | DEC-1（删 `NOTES_FOR_LEAD.md`，如采纳） | 待执行 |
| Z | — | — | 全局验收（build+test+race+vet+gofmt+archtest）+ 整体 diff 复核 + CHANGELOG + 总结 | 待执行 |

---

## 4. 分批执行与验收记录

全部批次串行 inline 执行，每批一次 commit，每批复跑受影响包 + archtest + 全量 `go test`。

| commit | 批次 | 摘要 | 验收 |
| --- | --- | --- | --- |
| `9738399` | A0 | 9 处源码注释 + 1 测试夹具删除对已删 `analyze_architecture_redesign_opus-5.md` 的悬空引用；修 `findings.go` tab 错位 | build/vet/gofmt/test/archtest ✅ |
| `d0fcb64` | — | 追踪文档 | — |
| `dbcb4c7` | B1 | `internal/journey` redesign 遗留死代码：`digest.go`（+test）、2 个墓碑文件、`parseManifestBody`/`stepContextPoint`/`journeyAnthropicCoverage*`、`BenchmarksReportFile`+孤儿常量、3 个死测试 helper；绑定注释同步 | journey/i18n/archtest/full ✅ |
| `487b9f5` | B2 | `internal/report` 死代码：`requests.go` 6 个 D7 遗留 cell 渲染器（`TestFinishCell` 改测 `outcomeCell`）、`ExportMacroSlices`、`ParseTimePoint`/`HashBytes`/`TimePoint.Time`、`sanitize`、`Markdown` 别名（~13 测试点改 `MacroMarkdown`）、`withSessionErrs` | report/archtest/cmd/full ✅ |
| `6551c51` | B3 | `respnorm.classifyEvent`、`chatmsg`/`respnorm` 两个死 `messageStartMarker` 包级 var + 注释订正 | respnorm/chatmsg `-race`/archtest/full ✅ |
| `208066e` | B4 | `config.ProviderPricingConfig.MapLegacy`/`OverridesLegacy`（只写不读）、`imgprep` 无谓 const 别名 | config/imgprep/archtest/full ✅ |
| `d6fdeac` | B5 | `digest.DigestHex`（+ 冗余测试断言）；~30 个死 `i18n` chrome 字段（`ToolWasteText` 13、`IndicatorsText` 14、4 个 `*Close`、`report_doc.PerClientLabel`）——D6 自包含 HTML 退役 / 标签改走 `MetricLabel` / 序列化器硬编码闭合 tag 后失效；保留字段字符串逐字节不变 | i18n/journey/report/digest/archtest/full ✅ |
| `49099b3` | B6 | `loadStoriesLink`→`loadJourneysLink` 等 `vmr story` 遗留命名（全 unexported/局部）；`dashboard_test.go` 手写 `strings.*` 复刻改回 stdlib | dashboard/cmd/archtest/full ✅ |
| `d55f248` | C1 | `reqdetail.roleStatLine` 去掉生产恒为 `true,true` 的 `withChars`/`bold` flag + 死分支；测试改测真实（bold）形态 | reqdetail/archtest/full ✅ |
| `c703791` | A1 | 12 处 garbled / 内容失实 / 混入中文的代码注释（`router.go` tab 错位、`health.Status.Available` doc、`config.nonNegativeFinite` 提已删 `Counters.Cost`、`dashboard.AssetNames` doc、`diagnose.Result.Phase` 漏 `check`、等） | build/vet/test ✅ |
| `1a9e393` | A2 | ~18 处指向已删 redesign 文档 / review 报告 / 已删函数的 pinned `§/P` 注释引用（journey/report/reqdetail/i18n/dashboard/cmd/tools/loadtest）；3 个 `render_*.go` 文件级 `// Deprecated:` 前缀改为「现在是什么」；`audit` legacy TODO 日期式措辞改条件式 | build/vet/test ✅ |
| `8d5f4aa` | A3 | KNOWN_ISSUES §2.55 编号冲突（→ §2.97）、`funcLineExemptions` 孤儿条目补标题（§2.98）、§1.3「五个入口」→四、§1.4 digest 条目订正；Core 设计文档 decided-not-to-fix 表删除「健康注册表残留」失效行（`health.Registry.Prune` 已落地） | archtest ✅ |
| `1e35830` | DEC | 删除已提交的陈旧 `NOTES_FOR_LEAD.md`（内容 100% 已落地 CHANGELOG）；KNOWN_ISSUES §1.3 收窄「/help 不做服务端模板渲染」表述（`renderHelp` 确实填 `{{BASE_URL_*}}`）；`report.AnalyzeSessions` doc 不再谎称有生产调用方 | report/server/archtest/full ✅ |

---

## 5. 总结

### 5.1 清理成果统计

| 分类 | 处理项数 | 说明 |
| --- | --- | --- |
| **A（直接修）** | ~35 处 | 注释 garbled / 失实 / 混中文 / pinned 引用 / KNOWN_ISSUES 编号与失效事实 / Core 设计文档失效表行 |
| **B（机械清理）** | ~55 个符号 | 已证实死代码：函数、类型方法、包级 var、结构体字段、常量、墓碑文件、死测试 helper；`deadcode -test` + 全仓 `git grep` + 间接引用三重证据 |
| **C（结构性简化）** | 1 项 | `reqdetail.roleStatLine` 去死 flag（等价变换 + archtest 复核） |
| **D（只登记不做）** | 9 项 | 见 §5.2 |
| **待用户决策** | 3 项已自主裁决 | 见 §5.3 |

**代码量净变化**（相对基线 `c5a05bc`，14 个 commit）：
- 生产 `.go`：66 文件，+181 / −534（**净 −353 行**）
- 测试 `.go`：12 文件，+38 / −168（**净 −130 行**）
- 文档：KNOWN_ISSUES / Core 设计文档 / Analytics 设计文档小幅订正
- **无任何外部可观测行为变化**：路由、`vmr analyze` 全部产物、审计 JSONL 结构、config 语义、错误信封、`/status` 形状均逐字节不变；i18n 保留字段字符串、`roleStatLine` 生产输出、`imgprep` 缓存上限值均经差分核实一致。

### 5.2 D 类保留清单（本次不做，附理由）

| # | 项 | 不做的理由 |
| --- | --- | --- |
| D-1 | `ctxgraph` 有界 worker 池样板 ×4 + `FetchRecords`/`ForEachRecord` 重复 (~30 行) | 中等工作量的并发重构，应作独立专项 + 完整 `-race`/archtest 复审，非清理 pass 顺带 |
| D-2 | `router.BuildQuotaSpecs` / `BuildQuotaSpecsDisabled` / `ProviderLimits` 内层 `resolveLimits` 循环 ×3 | 触 quota parity；§2.81 已明定双形态刻意，改动易引发再论证。可抽私有 helper，但收益 < 扰动 |
| D-3 | `respnorm` 的 `think_pattern_detected`（无 i18n/report 消费方，与 `thinking_process_pattern_detected` 不对称，`c363e9b` 漏接线）；`truncated_flush`/`truncated_withheld` 缺 `i18n.NormDescriptions` → 详单页落「(unknown step)」 | 补齐 = 改分析产物契约（`NormCounts` JSON）与详单输出 = 行为变更，超范围。**登记为功能缺口** |
| D-4 | `quota.PeriodEnd`（生产走 `PeriodBounds`）、`pricing.Complete(spec)` 包级函数（§2.58a 说本应随 `metric:cost` 删）、`pricing.Rate.MissingComponents`、`sticky.NewBounded`、`digest.EncodeUint64`、`quota.Headroom`/`UsedFrac`/`PerModelPrefix`、`tokenutil.AnalyzeString`/`IsCJK`/`IsEnglishSymbol`、`fmtutil.CurrencySymbol` | 均为「生产无调用、仅测试用」。但删除会：牺牲对活函数（`PeriodBounds`/`resolveChain` 的 idx 逻辑）的真实测试覆盖、破坏对称编码器 API、扰动「score math fully unit-testable」取向的测试。`Complete`/`MissingComponents` 可能是 §2.58b（逐行费率溯源）脚手架。逐个删除 ROI 为负，`§1.4 line 143` 先例的删除动机是「危险语义」不是「测试专用」 |
| D-5 | `report.HashFile` 与 `ctxgraph.HashFile` 字节等价 | manifest 自包含可能是刻意（同 §1.4「`report/cost.go` 端点标签不并入 `core`」型判断），需单独评审 |
| D-6 | `cmd/vmr/*.go` ~57 处 pinned `§N.M` / `P<n>.<x>` 阶段引用 | 违反 CLAUDE.md「generic not pinned」，但既有风格、量大，全量批改 churn 高价值低。A2 只处理了指向「已删符号 / 已删文档」的悬空引用 |
| D-7 | `router.pin.applyPin` 第二返回值 `bool` 仅测试消费 | 收益小，牵动 `pin_test`；生产 `out,_:=` + 先判 `active` 已使守卫在生产路径死 |
| D-8 | `ctxgraph.fastRawDigest` 死 `case int`/`int64` | 仅缓存 key 摘要，命不中只回退重算，无正确性影响 |
| D-9 | `report.Build` / `report.AnalyzeSessions` / `journey.Build` 测试专用薄包装 | §1.4 line 143 先例本可删，但被数十个测试点用作简单入口，删除 = 大面积测试改写、覆盖清晰度下降，且无危险语义。本次只订正 `AnalyzeSessions` 的误导性 doc（DEC 批） |

### 5.3 待用户决策清单（已按授权自主裁决，留档备查）

| # | 事项 | 裁决 | 理由 |
| --- | --- | --- | --- |
| DEC-1 | 已提交的 `NOTES_FOR_LEAD.md` 是否删除 | **已删除**（commit `1e35830`） | 内容（L2/L3 缓存 CHANGELOG 建议）100% 已落地 CHANGELOG；multi-agent 指南规定此文件不提交；CLAUDE.md「one-off review 报告不再产出」。git history 可恢复。用户已授权按判断处理 |
| DEC-2 | KNOWN_ISSUES §1.3「/help.html 不做服务端模板渲染」是否收窄 | **已收窄**（commit `1e35830`） | 核实：`server/help.go` 的 `renderHelp` 确实对 `{{BASE_URL_*}}` 做服务端 `bytes.ReplaceAll`，填访客自己的地址（HTML 转义、不需要用户 Key、有 JS 兜底）。代码的 doc comment 已完整解释此行为且合理——是 doc 滞后于代码演进，不是代码越界。收窄措辞使其与代码一致，不推翻决策本身 |
| DEC-3 | `report.Build`/`AnalyzeSessions`/`journey.Build` 三个测试专用薄包装是否按 §1.4 先例删除 | **不删，只订正 doc**（commit `1e35830`，见 D-9） | 先例动机是「危险语义」（`router.TokenCounters` 混淆 partial/exact），此三者只填默认参数、无语义风险；删除需重写数十个测试点，扰动大于收益 |

### 5.4 执行过程摘要

1. **阶段一**：跑全量基线 → 发现 `internal/archtest` 因上个 commit `c5a05bc` 删文档、注释未同步而红 → 作为 A0 修复并绿化。通读 CLAUDE.md / KNOWN_ISSUES §1–§3 / 四份设计文档决策表，建「已知权衡地图」（大量「看似死代码实则刻意保留」清单）。
2. **阶段二**：6 个只读侦察 Agent 按 Domain（leaf/shared、routing-core、routing-io、analytics 解析层、report+journey、cmd+docs）并行盘点；主控独立跑 `golang.org/x/tools/cmd/deadcode`（`-test` 与非 `-test` 两遍）作交叉验证。
3. **阶段三**：汇总去重，逐项对照 KNOWN_ISSUES §1 与设计文档决策表剔除「命中刻意取舍」的项，按包聚批、按风险排序（A0 → B×6 → C×1 → A×3 → DEC）。
4. **阶段四**：主控串行 inline 执行，每批一次 commit（短、祈使、无 trailer），每批复跑受影响包 + archtest + 全量 `go test`，`git diff` 逐行复核行为保持。对每个「测试专用但有专属测试」的候选逐个判断是否值得删（多数降级 D）。
5. **阶段五**：全局复跑 build + vet + gofmt + shellcheck + 全量 `go test` + `-race`（health/audit/router/quota/sticky/server/respnorm/ctxgraph/cmd）+ archtest；整体 `git diff c5a05bc..HEAD` 复核：66 个生产文件的非注释改动全部为「删死代码」或「等价重命名 / flag 内联」，i18n 保留字段与 `roleStatLine` 生产输出经 `git show` 差分确认逐字节一致。CHANGELOG `[Unreleased]` 登记一条 Changed（内部死代码清理，无行为变化）。

### 5.5 验收结果（阶段五全局复跑）

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `gofmt -l .` | ✅ 干净 |
| `shellcheck vmr.sh vmr-loadtest.sh scripts/*.sh` | ✅ 干净 |
| `go test ./...` | ✅ 全绿 |
| `go test -race`（并发敏感包） | ✅ 全绿 |
| `go test ./internal/archtest/...` | ✅ |
| 整体 `git diff` 行为保持复核 | ✅ 无行为变更混入 |
