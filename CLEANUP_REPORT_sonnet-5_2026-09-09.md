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

---

## 6. 二次复审：§1.2 权衡地图与 D 类事项的第一性原理重估

**执行者**：Sonnet 5（claude-sonnet-5）
**日期**：2026-09-09（首轮之后独立追加）
**任务**：把首轮 cleanup 里"直接排除掉"的两类事项重新拿出来，以今时今日的源码为基础、从第一性原理出发，
检验旧结论是否依然 solid——不受首轮 Issue 里已钉死角色的约束。
**授权边界**（用户确认）：① 行为可有限放宽——第一性原理下明显更优且高把握的，可直接改（含小幅行为变更），报告详述；
② 已处理项全部留工作区，不 commit；③ 已否决的设计文档级简化提案（SWRR 等）只做轻量复核。

### 6.0 方法与总体结论

- **G1 — KNOWN_ISSUES §1「看似死代码实则保留」清单（15 项，全深度）**：逐项 `deadcode` / 全仓 `git grep` / 逐消费点核实。
  14 项结论**完全成立、无需改动**；1 项（`Manifest.MsgIdx`）结论更成立、但**依据事实已过时**——已直接订正。
- **G2 — 已否决简化提案（轻量复核）**：SWRR / `Source` 接口 / `mode: shared\|per_model` / 额度硬熔断 E3，**四项否决理由今天全部仍成立**。
- **G3 — 首轮 D-1..D-9（全深度）**：D-3 部分处理（i18n 缺口，详见 6.3）；D-5 / D-6 的旧"不做"理由偏弱、给出更清晰建议留决策；
  其余 6 项旧结论成立。
- **G4 —「§2 待办全部 = D 类只登记不做」的定性**：**基本成立**——§2 里每个可动手项要么是触发条件未到的性能项、要么是用户 hold、要么是产品路线。
  两处小瑕疵（§2.59 是唯一"随时可做"项但属功能微调、非清理；§2.29/§2.51/§2.66/§2.79 违反 KNOWN_ISSUES 自身"已修复不进本文档"的维护铁律）见 6.4。

**一句话**：首轮的排除判断整体扎实。真正需要动的只有"文档事实随代码演进而过时"这一类，和一处首轮明确划到范围外、
但用户本轮授权可做的 i18n 缺口。没有发现被误判为"不值得"的高价值项。

### 6.1 G1 — KNOWN_ISSUES §1「看似死代码实则保留」清单逐条

> 结论列：**成立** = 旧论证今天仍完整，命中即不改；**订正** = 结论对、依据事实需更新。

| # | 事项 | 核实结果 | 结论 |
| --- | --- | --- | --- |
| G1-1 | `health.Registry.Available` 无生产调用方 | 全仓 `grep -v _test` 零命中，仅 `health` / `router` 三个测试文件断言端点状态用它；`Acquire` 会占 half-open 名额、不能替代 | **成立** |
| G1-2 | `ctxgraph.Manifest.MsgIdx` 无生产消费者 | **已过时**：`vmr diff`（`cmd/vmr/cmd_diff.go`，2026-09-08 落地）用它把 manifest 哈希位置映射回真实消息角色（`tailLine` / `msgRoleAt`），是一等生产消费者 | **订正**（已改 KNOWN_ISSUES §1.4） |
| G1-3 | `respnorm` 观测标记 `crlf_framing_suspected` / `thinking_process_pattern_detected` 不删 | `report/aggregate.go` 的 `diagnosticNormMarker`、`report/rows.go`、`i18n/reqdetail_detail.go` 三处现役消费 | **成立** |
| G1-4 | `respnorm.Read` 等更多字节时返回 `(0, nil)` | 对照 `router/transport.go` 的 `copyFlush`：`(0,nil)` 让读循环每次拿到 upstream 分块就重置 idle 定时器——慢但活着的 upstream 在一个大事件中途不会误触发 idle 超时；改内部阻塞会让 `copyFlush` 卡在单次 `body.Read`、定时器照走 → 误判断流。源码 doc comment 已写明 | **成立** |
| G1-5 | `ReleaseProbe` vs `ReportNeutral`（函数体逐字节相同，刻意两个方法） | `ReleaseProbe` 唯一调用点是 `forwardSuccess`（"名额先还、健康结论等 `reportStreamOutcome`"）；`ReportNeutral` 是"这次结果对健康无信息量"。合一会让 `forwardSuccess` 读起来像已下终局结论。成本 ~10 行 | **成立**（边际，但合并 ROI 为负） |
| G1-6 | `i18n` 29 个微文件不合并 | `archtest` 强制与 `report/viewmodel_*.go` 一一配对；最大单文件 480 行，合并击穿 700 预算 | **成立** |
| G1-7 | `i18n` `type XxxText` + `if lang == ZH` 样板不改 `map[Lang]T` | 抽查 `report_sticky.go`：多个字段是 `func(...)` 闭包（参数化字符串），根本无法退化成扁平 `map[Lang]string`；改写只省 2 行/文件、新增泛型 helper + "key 缺失"分支 | **成立** |
| G1-8 | `core/core.go` 不按领域拆文件；`core` 准入例外清单是显式豁免 | `core.go` 506 行（低于 700）；`core` 包零内部依赖（archtest `zeroInternalDepPackages` 已登记）；Q18 收敛，KNOWN_ISSUES 明文"不要再逐个提案外移" | **成立** |
| G1-9 | `internal/probe` 不登记进 `zeroInternalDepPackages` | 该表语义是"**承诺**永远零依赖"；`probe` 独立成包是为破 `diagnose`→`router` 循环，未来 import `core` 合理 | **成立** |
| G1-10 | `adapter` 协议字面量不从 `jsonscan` 导出；`jsonscan` 改写函数不迁 `adapter`（Q17） | 架构未变：`jsonscan` 仍是字节扫描/splice 引擎，`adapter` 仍是协议路由语义层。KNOWN_ISSUES 明文"不要再提案移动或恢复旧措辞" | **成立** |
| G1-11 | `report/cost.go` 端点标签切分（严格 `:`）不并入 `core.SplitEndpointLabel`（兼容 `:` 与 `/`） | 放宽 `/` 会改旧格式日志的历史报表金额（`report` 与 `journey` 共用 `RateForEndpoint`，两侧一致）。**新情况**：Pricing 架构极简化（决策 6）后脏费率影响面已收窄到只污染离线 `vmr analyze` 的 $ 估算——但触碰它仍是 report 输出的行为变更，价值近零 | **成立**（值得在 KNOWN_ISSUES 补一句"影响面已收窄") |
| G1-12 | `core.StickyBackstopTTL` 不迁回 `internal/sticky` | canonical 在 `core`，`config/config_validate.go` 两处校验 `sticky_ttl ≤ backstop`；迁回制造 `config`→`sticky` 新依赖边只为读一个常量 | **成立** |
| G1-13 | 降级 token 估算请求侧/响应侧 fallback 刻意不对称 | `TestEstimateDegradedBasis_FallbackAsymmetry` + quota parity 双向钉死；KNOWN_ISSUES 明文"任何统一方向都已论证过是复现已修过的 bug" | **成立** |
| G1-14 | `archtest` 包边界守卫单向；不加圈复杂度检查 | 单向与规则本身同构（"分析半区不 import 路由半区"是单向禁令）；圈复杂度是"一次只加一个守卫"，函数行数预算落地未久 | **成立** |
| G1-15 | 行数预算是"提醒式绊线" | 元原则，非代码项 | **成立** |

**G1 唯一发现（G1-2）详述**

- **问题描述**：KNOWN_ISSUES §1.4 的 `Manifest.MsgIdx` 条目写"没有生产消费者……`structure_test.go` 靠它验证不变量"。
  `vmr diff`（`7f381ae`，2026-09-08，早于首轮 cleanup 一天）在 `cmd/vmr/cmd_diff.go` 用 `mA.MsgIdx` / `mB.MsgIdx` 把
  manifest 的哈希位置映射回真实消息角色，用于结构分歧报告——是一等生产消费者，已进 `main.go` 子命令表、UserGuide、Analytics 设计文档。
- **改与不改的影响**：不改——下一个读到该条目的人会以为 `MsgIdx` 只有测试在用，可能提案删除或收窄，而它现在删除会同时砍掉 `vmr diff`。
  改——文档与代码现实一致，"不可删"的理由从"测试不变量"升级为"承载性导出 API + 测试不变量"，更硬。
- **根因**：`vmr diff` 落地与首轮 cleanup 只差一天，KNOWN_ISSUES（Ver 2026-08-31）未同步；首轮 §1.2 盘点也照抄了旧表述。
  这是标准的"文档是当前状态、代码演进后未同步"漂移。
- **建议方案 / ROI**：**已直接处理**——重写 KNOWN_ISSUES §1.4 该条为"承载性导出数据，两类消费方（`vmr diff` 生产 / `structure_test` 不变量）"，
  并注明"曾写'没有生产消费者'——`vmr diff` 落地后已过时"。零风险（纯文档事实订正，archtest 文档守卫通过）。ROI 高。

### 6.2 G2 — 已否决简化提案（轻量复核）

| 提案 | 出处 | 否决理由今天是否仍成立 | 结论 |
| --- | --- | --- | --- |
| **SWRR 平滑加权轮询** | Quota / Strategy 设计文档 | ① 需持久化累加器；② 撒开流量伤 prefix-cache 局部性，与"缓存命中率优先"的核心取舍相反；③ SWRR 只答"选谁"，同梯队稳定排序还答了 failover 顺序。三条与 prompt cache 中心地位一同不变 | **仍否决** |
| **官方用量 API 预抽象 `Source` 接口** | Core 设计文档 / KNOWN_ISSUES §1.4 | 全仓 `grep` 无任何厂商私有用量接口接入；YAGNI 前提未破 | **仍否决** |
| **独立 `mode: shared \| per_model` 字段** | Quota 设计文档决策表 | 真实需求是三态（账号总限 / 全模型独立 / 具名模型独立），已由 `models:` 单字段（不写 / `["*"]` / 列表）表达并随 P3 交付、稳定运行；独立 `mode` 只多一种"写错组合" | **仍否决** |
| **额度耗尽硬熔断 / 虚拟模型级预算硬闸 E3** | Quota 设计文档 / KNOWN_ISSUES §2.52 | 按估算值执行破坏性拒绝 = 自制故障；硬信号（上游 402/429）已由 health 状态机长冷却覆盖。§2.52 记录 E3 是"给一夜烧光设确定性上限"的不同目标，**用户 hold** | **维持**（用户决策项，非技术债） |

**轻量复核未发现任何一条否决理由被新事实动摇。** 项目对 prompt-cache 局部性的依赖、单机零持久化的坚持、零埋点前提均无变化。

### 6.3 G3 — 首轮 D-1..D-9 重估

#### D-1 `ctxgraph` 有界 worker 池样板 ×4 + `FetchRecords`/`ForEachRecord` 重复

- **问题描述**：`scan.go` / `cache.go` / `records.go`（×2）四处逐字重复同一 fan-out 样板
  （`results := make([]T, len(paths))` + `sem := make(chan struct{}, scanWorkerCount(...))` + `wg` + `for` 起 goroutine + 串行 merge）；
  `FetchRecords` 实质是 `ForEachRecord` + 一个 map 累加（`byPath` 分组、`paths` 提取、fan-out 三段各 ~10 行逐字相同）。
- **改与不改的影响**：不改——~50 行重复稳定存在，四处 fan-out 逻辑独立维护（改一处并发语义要记得改四处）。
  改——抽 `parallelByFile[T any](paths, fn func(string) T) []T`，每处塌成一行；`FetchRecords` 可写成
  `ForEachRecord` + `sync.Mutex` 保护的一行 `out[loc]=rec`（重活 zstd+unmarshal 仍在锁外）。净省 ~50 行，并发样板单一来源（真实的正确性小收益）。
- **根因**：四处按"能跑"逐个写出，未回头抽公共形状。串行 merge 尾巴各不相同（`[]fileResult` / `[]error` / manifest 合并 / map 累加），
  掩盖了 fan-out 头部完全一致。
- **建议方案**：抽 `parallelByFile[T]`；`FetchRecords` 改建在 `ForEachRecord` 上。
- **ROI**：中低。旧结论（"应作独立专项 + 完整 `-race`/archtest 复审，非清理 pass drive-by"）**方向正确、措辞略重**——
  实际约 1 小时。但它触 5 个并发站点跨 2 包，需 `-race` 全绿 + archtest 行/函数预算复查，且当前代码正确、`-race` 干净、注释完整、
  重复量有界且不增长。**建议**：作为一个约 1 小时的独立小任务做掉（不是"大专项"，也不该在 review pass 里顺手），本轮不动。

#### D-2 `router/snapshot.go` `BuildQuotaSpecs` / `BuildQuotaSpecsDisabled` / `ProviderLimits` 内层 `resolveLimits` 循环 ×3

- **问题描述**：三处各有一段解析 Limit 的内层循环，可抽私有 helper 去重。
- **改与不改**：不改——三段小重复。改——去重，但 `BuildQuotaSpecs` 双形态（`Disabled` 感知 / 不感知）是 KNOWN_ISSUES §2.81 **明文钉死的刻意设计**
  （`replay.chargeReplay` 需要不感知 disabled 的原形态），且这块受 quota parity 差分测试保护。
- **根因**：双形态本身是正确的语义分叉，不是重复。
- **建议方案 / ROI**：**维持不做**。旧结论成立。改动易引发对双形态的再论证，收益 < 扰动。§2.81 已注明"后续若改任一取舍，先改本条再改代码"。

#### D-3 `respnorm` 的 `think_pattern_detected` / `truncated_flush` / `truncated_withheld` 无 i18n 消费方 —— **部分已处理**

- **问题描述**：`respnorm` 经 `noteApplied` 发出 13 个 `norm` 标记，其中这 3 个在 `i18n/reqdetail_detail.go` 的
  `NormDescriptions`（EN/ZH）里**没有条目**。`reqdetail` 详单页的 `writeNorms` 对缺失项回退到 `UnknownNormStep` =
  `(unknown step)` / `（未知步骤）`。而截断（上游流式中途断流）是 relay 层最常见的失败形态，`truncated_*` 会真实出现在详单页上。
- **更深一层的根因**：`internal/reqdetail/detail_test.go` 的 `TestNormDescriptions_AllKnownStepsHaveText` **本就是防这个的守卫**
  （doc comment 原话："guards against a norm step name being added to the router-side trail without a matching entry"），
  但它用一份**手抄的 11 项硬编码清单**而非从 `respnorm` 实际词表派生——这 3 个标记后加进 `respnorm` 时，i18n map 和这份清单**一起漏更**。
  守卫因为是拷贝而非派生，在它唯一的职责上失效了。跨半区（`reqdetail` 是分析半区，不能 import `respnorm`）使"派生"不平凡——
  标记字符串是经审计记录（JSONL 契约）过来的，手抄某种程度上不可避免，除非把词表下沉到 `core`。
- **已处理**：
  - `i18n/reqdetail_detail.go` EN + ZH 各补 3 条 `NormDescriptions`（`think_pattern_detected` 注明"无阈值、正文引用标记也会命中"；
    `truncated_flush` / `truncated_withheld` 按 `flushRawOnError` 的真实语义描述）。**纯详单页人读文本变化**，不碰任何 JSON 契约。
  - `detail_test.go` 的硬编码清单补上这 3 项，并加注释说明"手抄、跨半区无法 import `respnorm`、加标记时记得同步"。
  - 验收：`go test ./internal/reqdetail/... ./internal/i18n/... ./internal/archtest/...` + 全量 `go test ./...` 全绿。
- **调查后建议不做的部分**：首轮 D-3 还提到把 `think_pattern_detected` 接进 `report/aggregate.go` 的 `diagnosticNormMarker`
  （跨请求频率预警，像它的兄弟 `thinking_process_pattern_detected` 那样）。**看似漏接线，实则不该接**：
  `thinking_process_pattern_detected` 有阈值（`>1024` 字节 + `≥3` 个编号命中）专门压假阳；`think_pattern_detected` **无任何阈值**——
  正文里出现一次字面 `<think>`（coding agent 讨论 HTML/模板/思考模型时很常见，`respnorm` 测试夹具本身就是
  `"Here is how you quote <think> tags in markdown"`）就命中。接进频率聚合会让它被良性内容驱动的高计数淹没，
  正是 `NormCounts` doc comment 警告的"dominated by near-100%-hit-rate noise"失败模式。**旧结论（不接）成立**——
  但理由不是首轮写的"超范围/行为变更"，而是**这个标记缺假阳阈值，不适合频率聚合**。若真要接，先在 `respnorm` 给它加阈值
  （路由半区行为变更，独立任务）。
- **ROI**：i18n 部分——高（详单页在最常见失败形态上不再显示"未知步骤"，零契约风险，用户本轮明确授权）。聚合部分——负（噪声）。

#### D-4 一批 exported 但仅包内 + 测试用的符号

- **首轮清单**：`quota.Headroom`/`UsedFrac`/`PerModelPrefix`、`tokenutil.AnalyzeString`/`IsCJK`/`IsEnglishSymbol`、
  `fmtutil.CurrencySymbol`、`pricing.Rate.MissingComponents`、`pricing.Complete(spec)`、`quota.PeriodEnd`、
  `sticky.NewBounded`、`digest.EncodeUint64`。
- **核实订正**：首轮把这批统称"生产无调用"，**不准确**。逐个查：
  - `quota.UsedFrac` / `quota.Headroom` —— `score.go` 的 `ScoreForLimit` 里就在调（`Headroom(UsedFrac(used, l.Amount), ...)`）。
    是"包内生产在用 + 额外导出供直测（`TestUsedFrac_ClampsAndGuards`）"，不是死代码。
  - `tokenutil.IsCJK` / `IsEnglishSymbol` / `AnalyzeString` —— `tokenutil.go` 的 `Analyze` / `AnalyzeString` 里在调。同上。
  - `fmtutil.CurrencySymbol` —— `FmtCurrency` / `FmtCurrencyPrecise` 在调。同上。
  - `quota.PerModelPrefix` —— `ExtractModel` 在调（`quota.go`）。同上。
  - **真正"零生产调用、仅测试"的只有**：`pricing.Complete(spec)` 包级函数（§2.58a 记它本应随 `metric: cost` 删）、
    `pricing.Rate.MissingComponents`、`quota.PeriodEnd`（生产走 `PeriodBounds`）、`sticky.NewBounded`、`digest.EncodeUint64`。
- **改与不改**：这 5 个即使删也各有一个具体理由留：`EncodeUint64` 补齐对称编码器集（`EncodeInt64`/`Bool`/`Float64`/`String` 都在用）；
  `PeriodEnd` / `NewBounded` 给活函数（`PeriodBounds` 的 `findK`、`sticky.New` 的默认容量）提供直测锚点；
  `Complete(spec)` / `MissingComponents` 是 §2.58a/b（逐行费率溯源/缺分量披露）大概率会用到的脚手架，且是干净纯函数、无危险语义。
- **根因**：把"exported 但外部没调"直接读成"死代码"，漏了"包内在用、导出是为可测性"这一大类。
- **建议方案 / ROI**：**全部维持**。收窄可见性会牺牲对活逻辑的直测覆盖、扰动"score math fully unit-testable"取向，ROI 为负。
  `§1.4 line 143` 先例（删 `router.TokenCounters`）的删除动机是"危险语义（partial 当 exact）"，**不是"测试专用"**——不适用于这批。
  首轮结论对，只是把"生产无调用"这个措辞用宽了。

#### D-5 `report/manifest.go` 的 `HashFile` 与 `ctxgraph.HashFile` 字节等价

- **问题描述**：`report/manifest.go:335` 自带一份 `HashFile`（sha256 hex of file），与 `ctxgraph.HashFile` 实现逐字节相同，
  用于 manifest 完整性哈希（`manifest.go:180/204/300`）。
- **首轮理由偏弱**：首轮说"manifest 自包含可能是刻意（同 `core/cost.go` 端点标签不并入型判断），需单独评审"。
  但 `internal/report` **已经 import `ctxgraph` 并在 `aggregate.go:246` / `cache.go:34` 直接调 `ctxgraph.HashFile`**——
  `manifest.go` 那份私有拷贝**没有带来任何依赖隔离**，"自包含"的辩护站不住。
- **改与不改**：不改——13 行重复。改——`report/manifest.go` 直接调 `ctxgraph.HashFile`，−13 行、零新依赖。
  唯一实质顾虑：两处**用途不同**（cache key 哈希 vs 产物完整性哈希），耦合后若有人改 `ctxgraph.HashFile`（如加长度前缀）
  会静默改掉 manifest 完整性哈希。但这个顾虑是理论性的——两者都是"文件字节的裸 sha256"，没有会为一方而改的动机；
  且项目自身的 `digest` 包合并先例（KNOWN_ISSUES §1.4："为 ~40 行纯函数建共享叶子包不划算" 后被 review 推翻、下沉为叶子包）
  方向相反。
- **根因**：`manifest.go` 早于 `report` 用上 `ctxgraph.HashFile` 时写下，之后没回收。
- **建议方案 / ROI**：**建议 `report/manifest.go` 改调 `ctxgraph.HashFile`**（1 分钟改动 + `go test ./internal/report/...`）。
  ROI 低但正——留给你一句话决策；不做也无害。首轮"需单独评审"过于保守，但结论（本轮不动）可接受。

#### D-6 `cmd/vmr/*.go`（及 `internal/journey` / `report` / `quota` / `router` / `i18n`）~50 处 pinned `§N.M` / `P<n>.<x>` / "dev plan" 引用

- **问题描述**：违反 CLAUDE.md「Cross-references are generic, not pinned」。首轮 A-13/A2 只清了指向**已删符号 / 已删文档**的悬空引用，
  指向"还在的设计文档章节 / 开发计划阶段号"的那批留着。
- **本轮细分**（比首轮"57 处 in cmd/vmr"更准）：
  - `D<n>` 决策 ID 引用（`D8` / `D14` / `D21` …）——设计文档**刻意用作稳定标识**（KNOWN_ISSUES §2.29 自己就引 `D14`），保留 OK。
  - `§N.M` 章节号引用——违反约定，应改成按名或散文。
  - `P<n>.<x>` 阶段号 + "dev plan" / "plan doc" 引用——指向 `docs/tasks/`（`action_plan_*.md` / `TASK_SPEC_*.md` / `HANDOVER_*.md`，
    看起来是**已完成工作的实施期产物**）。**若 `docs/tasks/` 是 ephemeral 的，这批全部实质悬空。**
  - `review §12.5` 引用——指向一份仓库里不存在的 review 报告（CLAUDE.md：一次性 review 不再产出/保留）。首轮漏掉。
- **已处理**：两处 `review §12.5` 悬空指针（`i18n/journey_render.go` / `cmd/vmr/cmd_journey.go`）——
  改为指向真实的测试守卫（`B10` in `cmd_analyze_test.go`）或直接删掉指针。同首轮 A-13 类别、零风险。
- **改与不改（其余 ~50）**：不改——`§`/`P` 引用继续增殖（下一个人看到 `(P9.1)` 就跟着写 `(P14.2)`），是"破窗"。
  改——批量 churn（散在 6 个包），价值低（多数 `§N.M` 指向的章节还在、读者能找到）。
- **根因**：实施期大量按"计划阶段"写注释，工作完成后没回收成散文；`docs/tasks/` 去留未定。
- **建议方案 / ROI**：① 先定 `docs/tasks/` 是保留还是 ephemeral（影响 ~20 处 `P<n>` 引用是否算悬空）——**这是给你的决策点**；
  ② 若要清，作为一个独立的"注释交叉引用 hygiene" pass（约 1–2 小时），不要 piecemeal；③ `D<n>` 决策 ID 保留。
  本轮只清了 2 处真悬空的 `review §` 引用。

#### D-7 `router/pin.go` `applyPin` 第二返回值 `bool` 仅测试消费

- **问题描述**：`applyPin` 返回 `([]*core.Endpoint, bool)`，bool = "pin 是否 active"；唯一生产调用方 `applyPinToCandidates`
  已先判 `p.active()` 再调、然后 `out, _ :=` 丢弃 bool。
- **改与不改**：不改——一个测试用的返回值。改——签名变单值，牵动 `pin_test`。
- **根因**：`applyPin` 独立于 `applyPinToCandidates` 存在正是**测试接缝**——测试可以直接用构造的 `pin` 结构体驱动过滤逻辑，
  不走 header 解析。bool 是这个接缝的一部分。
- **建议方案 / ROI**：**维持不做**。旧结论成立。收益小、牵动测试、且 bool 有轻微自文档价值。

#### D-8 `ctxgraph/manifest.go` `fastRawDigest` 死 `case int` / `case int64`

- **问题描述**：`fastRawDigest(v any)` 的输入全部来自 `json.Unmarshal` 进 `any`（`encoding/json` 数字恒 `float64`），
  `case int` / `case int64` 不可达（6 行）。与同函数里**可达**的 `case bool` / `case nil`（json 确实产这两个）不一致。
- **改与不改**：不改——6 行无害死代码。改——省 6 行；但这是 `ctxgraph` 消息哈希的单一实现（CLAUDE.md 红线），
  且 `fastRawDigest` 是缓存 key 摘要——即便我对"json 从不产 int"的判断有万一之错，后果也只是缓存 miss 回退重算（`hashMsgJSON`），
  非正确性。风险不对称：留 = 零成本，删 = 极小的缓存 key churn 风险 + 必须复跑 ctxgraph 全测。
- **建议方案 / ROI**：**维持不做**。首轮 D 分类正确。真要动，等到为别的原因改这个函数时顺手删。

#### D-9 `report.Build` / `report.AnalyzeSessions` / `journey.Build` 测试专用薄包装

- **问题描述**：三者生产零调用（生产走 `*Cached` 变体），只填默认 profile / nil cache 参数。是否按 `§1.4 line 143`
  先例（删 `router.TokenCounters`）删除。
- **核实**：`journey.Build` 被 **~70 个测试调用点**（~20 个 test 文件）当作"构建单 lineage journey"的规范入口；
  `report.Build` 被 `cmd/vmr/cost_basis_parity_test.go` + ~5 个 report test 文件用。删除 = 大面积测试重写
  （把 10 参数的 `BuildCached` 签名铺到 70 个点）。
- **根因**：先例的删除动机是**危险语义**（`router.TokenCounters` 的单 "some usage seen" 位混淆 partial/exact 记账）——
  这三个只填默认参数，**无任何语义风险**。"仅测试用"从来不是删除触发。
- **建议方案 / ROI**：**维持不做**（DEC-3 裁决成立）。`report.Build` 的误导性 doc 已在 DEC 批订正；
  `journey.Build` 的 doc（"kept as its own entry point for callers previewing/testing a single lineage"）本就诚实。

#### G3 小结

| D-# | 首轮结论 | 本轮复审 |
| --- | --- | --- |
| D-1 | 独立专项，不 drive-by | 方向对、措辞略重；建议作 ~1h 独立小任务 |
| D-2 | 不做（§2.81 双形态刻意） | **成立** |
| D-3 | 只登记（超范围） | **i18n 部分已处理**；聚合部分不做的理由改为"缺假阳阈值" |
| D-4 | 不做（ROI 负） | **成立**，订正"生产无调用"措辞（多数包内在用） |
| D-5 | 需单独评审 | "自包含"辩护偏弱；建议改调 `ctxgraph.HashFile`，留决策 |
| D-6 | 不做（churn 高价值低） | 大批维持；**2 处真悬空 `review §` 已清**；`docs/tasks/` 去留是决策点 |
| D-7 | 不做 | **成立** |
| D-8 | 不做 | **成立** |
| D-9 | 不删只订正 doc | **成立**（DEC-3） |

### 6.4 G4 —「§2 待办全部 = D 类只登记不做」定性复核

逐条扫 §2（A–G 七组约 40 条），验证"没有价值高、成本低、被误降级"的项：

- **A 组（大语料内存/耗时）**：§2.2 / §2.1 / §2.55 / §2.56 / §2.96 / §2.69 / §2.50——全部触发条件明确（语料 > 约 3 万条 /
  RSS > 4GB / 语料再涨 5 倍…），且"先测量再优化"是项目一贯拒绝反过来的顺序。**定性成立。**
- **B 组（指标口径正确性）**：§2.57（时间归因无上限）/ §2.58 系列（费率覆盖 / 溯源）/ §2.64（双峰退化）——
  改动都要动指标语义 + 更新 Analytics 设计文档 + 差分测试，是"改契约"不是"清理"。**定性成立。**
- **C 组（LLM 校准）**：§2.18——成本在人工标注，非代码。**定性成立。**
- **D 组（展示契约）**：§2.59（compare 同源节选合并）—— KNOWN_ISSUES §3 自己标"**立即可做（界限清楚）**"，
  **不是**触发待办。但它是 compare `.md` 的输出变化 + 自带设计选择（"只做精确相等合并"），属**功能微调、非清理**，首轮划到范围外正确。
  这是 §2 里唯一"随时可做"的项，值得单独指出——但不构成"被误判为不值得"。
- **E 组（新能力）**：§2.61 / §2.60 / §2.13 / §2.62 / §2.93–95——产品路线或需独立设计，明确不属清理。**定性成立。**
- **F 组（路由配额/请求路径）**：§2.52（E3，用户 hold）/ §2.85a / §2.86 / §2.14 / §2.10 / §2.9 / §2.48——
  性能项待触发或需设计。**定性成立。**
- **G 组（工程工具）**：§2.97（CHANGELOG 发版前归整，[中]）——**这个不是"不做"，是"发第一个含 analyze 重构的 tag 之前必做"**，
  已在 §2.97 写明触发条件。首轮把 §2 统称"只登记不做"对这条不精确——它是"待发版触发"，不是"无限期搁置"。
  §2.98（`funcLineExemptions` 键设计）/ §2.80（`sysinfo` 折叠 0）/ §2.75 / §2.77——真加固项，触发条件明确。**定性成立。**

**结论**：首轮"§2 = D 类只登记不做"的定性**基本准确**，两处措辞需收紧：
① §2.97 是"发版触发必做"而非"搁置"；② §2.59 是"随时可做但属功能微调"。都不影响"没有高价值项被误埋"这个核心判断。

**附带发现（KNOWN_ISSUES 自身维护铁律）**：§2.29 / §2.51 / §2.66 / §2.79 标着 `[已解决]` / `[已消解]` / `[已闭环]` 仍留在 §2，
违反本文档开头维护原则第 1 条（"已修复的问题不进这里……留一条'已解决'记录毫无价值"）。其中 §2.51 / §2.29 带"防重提"信息
（"若未来重建 web 渲染层……"），可折进 §1 或改成一句话；§2.66 / §2.79 是纯"done"、可直接删。
**建议**：一次轻量 §2 清扫（逐条判"防重提价值是否够留"）。本轮未做——每条是独立编辑判断，且属 KNOWN_ISSUES 结构维护、
不在本任务核心。

### 6.5 本轮已处理项（留工作区，未 commit）

| 项 | 文件 | 变更 | 验收 |
| --- | --- | --- | --- |
| G1-2 | `docs/KNOWN_ISSUES.md` | §1.4 `Manifest.MsgIdx` 条目重写——从"没有生产消费者"订正为"承载性导出数据，`vmr diff` + `structure_test` 两类消费方" | archtest 文档守卫 ✅ |
| D-3（i18n） | `internal/i18n/reqdetail_detail.go` | EN + ZH `NormDescriptions` 各补 `think_pattern_detected` / `truncated_flush` / `truncated_withheld` 三条——详单页不再对截断类标记显示"(unknown step)" | `go test ./internal/i18n/...` ✅ |
| D-3（守卫） | `internal/reqdetail/detail_test.go` | `TestNormDescriptions_AllKnownStepsHaveText` 硬编码清单补齐这 3 项 + 注释说明"手抄、跨半区无法 import respnorm、加标记时同步" | `go test ./internal/reqdetail/...` ✅ |
| D-6（悬空 ref） | `internal/i18n/journey_render.go`、`cmd/vmr/cmd_journey.go` | 两处 `review §12.5` 悬空指针 → 指向真实测试守卫（`B10`）或删除 | `go build` / `go vet` / 全量 `go test` ✅ |

**全局验收**：`go build ./...` ✅ ｜ `go vet ./...` ✅ ｜ `gofmt -l`（改动文件）✅ ｜ `go test ./...` ✅ 全绿 ｜
`go test ./internal/archtest/...` ✅。**无 JSON / 审计 / config / 错误信封契约变更**；`NormDescriptions` 是详单页人读文本的加性补全
（首轮铁律下属 A 类文案修正，且用户本轮明确授权此例）。

### 6.6 留待你决策的项

| # | 事项 | 倾向 | 一句话理由 |
| --- | --- | --- | --- |
| REV-1 | `docs/tasks/` 保留还是 ephemeral | —（需你定） | 决定 ~20 处 `P<n>.<x>` / "dev plan" 注释引用是不是悬空；影响 D-6 后续清理范围 |
| REV-2 | 是否做 D-6 的"注释交叉引用 hygiene" pass（清 `§N.M` / `P<n>` → 按名/散文，保留 `D<n>` 决策 ID） | 倾向做，作独立 1–2h 任务 | CLAUDE.md 明文约定；不清会继续增殖（破窗）；但 piecemeal 不值 |
| REV-3 | D-1：抽 `parallelByFile[T]` + `FetchRecords` 建在 `ForEachRecord` 上 | 倾向做，作独立 ~1h 任务（带 `-race`） | 净省 ~50 行 + 并发样板单一来源；触 5 个并发站点，不该在 review pass 顺手 |
| REV-4 | D-5：`report/manifest.go` 改调 `ctxgraph.HashFile`（−13 行） | 倾向做（1 分钟） | `report` 已 import 并调 `ctxgraph.HashFile`，私有拷贝零隔离价值；唯一顾虑（用途不同）是理论性的 |
| REV-5 | D-3 聚合部分：`think_pattern_detected` 是否进 `diagnosticNormMarker` | **倾向不做**（除非先在 `respnorm` 给它加假阳阈值） | 无阈值 → 频率聚合被良性内容（讨论 `<think>` 标签）驱动的噪声淹没 |
| REV-6 | KNOWN_ISSUES §2 轻量清扫（`[已解决]` 条目：折进 §1 / 缩一句话 / 直接删） | 倾向做，作 KNOWN_ISSUES 维护小任务 | §2.29/§2.51/§2.66/§2.79 违反本文档自身维护铁律第 1 条 |
| REV-7 | G1-11 / G2 的 E3：均维持现状 | 无需动作 | 列此仅为闭环——E3 是用户 hold 项，G1-11 影响面已因决策 6 收窄但仍不值得碰 |

**未改动的 §1.2 权衡地图条目（G1-1、G1-3..G1-15）与 G2 四项否决**：本轮第一性原理复审确认全部依然 solid，命中即不改的纪律继续有效。
