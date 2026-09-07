// Ver 2026-09-07, by Claude (claude-sonnet-5)

# vmr analyze 架构重构方案落地情况独立评审

**评审基准**：`docs/future-strategy/analyze_architecture_redesign_opus-5.md`（下称"方案"），裁决 D1–D21 + 正文 §1–§11。
**实施范围**：`050ad25`（实施起点）以来 64 个 commit，至 `HEAD = 4efd7b0`。
**评审方法**：以方案为唯一基准，逐项核验代码是否落地；不阅读项目中已有的其他 review 记录，独立判断。
**基线**：`go build ./cmd/vmr` 通过；`go test ./...` 全绿（评审开始时）。

> 本评审执行原则：事实清楚、方案无争议、有十分把握的问题 → 当场修复并留痕；复杂或有争议的 → 记录待裁决。

---

## 第一部分：Action Plan（评审事项清单）

按方案章节展开为 13 个核验簇（C1–C13），每簇下列具体核验点。执行结论见第二部分。

| 簇 | 范围 | 关键裁决 |
|---|---|---|
| C1 | §2 概念归一：story→journey / corpus→benchmark 的彻底清除 | — |
| C2 | §3.1–§3.2 领域切片拆分 + 原 Report2 字段映射 | D1, D2 |
| C3 | §3.3 补齐渲染期现算的 7 类事实 | — |
| C4 | §3.4 产物提交顺序、manifest 准入、orphan 清扫 | D20 |
| C5 | §3.6 Journey JSON 自包含（bodies 表 / 三级 match / 截断口径） | D18 |
| C6 | §3.7 人读请求索引删除 + §3.8 compares 索引 | D7, D21 |
| C7 | §4 目标目录拓扑逐项比对 | D16, D17 |
| C8 | §5 ViewModel 双轨 + 单一渲染路径 + §5.6 不落盘/时间双字段 | D3, D4, D5, D10, D11, D12 |
| C9 | §6 骨架页 + fetch / 自包含 HTML 废弃 / `/reports/` 鉴权 / 版本 banner | D6, D9, D13, D14, D15, D16 |
| C10 | §7 单一 Digest 构造 + 三级缓存 + 指纹取值规范 + 失效矩阵 | D1, D8 |
| C11 | §8 manifest format 10→11 + 职责边界 | D2 |
| C12 | §9 测试守卫的去处（既有守卫迁移 + 17 条新增守卫） | — |
| C13 | §11 五条不变量 | — |

---

## 第二部分：逐簇执行详情

### C1 — 概念归一（§2）

- **story→journey 代码符号**：`internal/story` 已更名 `internal/journey`；`internal/story` 目录不存在。✅
- **CLI**：`vmr report` / `vmr story` 子命令别名已删除；`-corpus`→`-benchmark`、`-story-only`→`-journey-only` 已更名且无别名。✅（`cmd/vmr/main.go` usage、`cmd_analyze.go` flag 定义均已收敛）
- **产物路径**：`stories/`→`journeys/`；`vmr-stories.*`→`journeys/index.*`；`vmr-story-corpus.*`→`journeys/benchmarks.*`。✅（`saveJourneyIndex`、`writeJourneyFile`、benchmarks 写盘路径均已改）
- **残留（见新发现 N4/N5/N6）**：
  - 渲染产物中仍有指向 `stories/` 的**失效链接**（会话表、journey 回链）——已修复。
  - i18n 文案中仍有指向 `vmr-story-corpus.json` / `vmr-report.json` 的**过时引用**——已修复。
  - 200+ 处注释仍写 `vmr story` / `vmr report` 作为命令名——记录待裁决（N9）。
  - `cmd_journey_setup.go` 保留读取旧 `stories/vmr-stories.json` 的兼容 shim——已修复（违反 §8.1）。

**结论：C1 部分完成**（核心已落地，渲染产物链接/文案残留已在本轮修复，注释清理待专项处理）。

---

### C2 — 领域切片拆分（§3.1–§3.2，D1/D2）

- 5 个 macro 切片 schema 与 writer 齐备：`internal/report/slices.go` 的 `SummarySlice` / `FinanceSlice` / `ReliabilitySlice` / `WorkloadsSlice` / `ContextEfficiencySlice` + `WriteMacroSlices`。✅
- 原 `Report2` 21 字段 → 切片映射与方案 §3.2 表一致：`Overall`/`Efficiency`→summary；`ByModel`/`ByClient`/`Providers`/`ProviderQuota*`→finance；`Endpoints`/`EndpointsAll`/`Sticky`→reliability；`ByDate`/`Hours`/`HoursOfDay`/`Workloads`/`ClientEndpoints`→workloads；`Sessions`/`Compactions`/`Tools`→context-efficiency。✅
- 切片间不交叉引用数值：`summary.json` 的"总支出"独立落盘（`SummaryMeta`），非查 finance。✅
- D2「单体 `vmr-report.json` 一步删除」：`6eefb85` 已移除；`summary.json` 携 `SummaryMeta` 溯源、`finance.json` 携 `Pricing` 元数据。✅
- `TestMacroSlices_EquivalenceWithReport2`（迁移期等价断言，§9）存在。✅
- **偏差（见新发现 N7）**：`WriteRequestsIndex` 在 `dir` 非 `requests` 结尾时会额外写一份顶层 `vmr-requests.json` 兼容副本（`requests.go:157-159`）。当前调用点传的是 `.../requests`，该分支实际为死代码，但与 §8.1「无兼容视图」相悖。记录待裁决。

**结论：C2 已全部完成**（N7 为死分支，不影响产物，列入待裁决清理项）。

---

### C3 — 补齐渲染期现算事实（§3.3）

| # | 事实 | 落地情况 |
|---|---|---|
| 1 | 统计置信度（`tokens_coverage_pct` / `dur_low_n`） | ✅ `Row.TokensCoveragePct` 等字段已在 rows.go 暴露 |
| 2 | 成本覆盖披露（`CostCoverage`） | ✅ `slices.go` `CostCoverage{unpriced_count, incomplete_rate_count, degraded_estimate_pct}` + `BuildCostCoverage` |
| 3 | 脚注/免责注册表（`meta.footnotes` / `meta.disclaimers`） | ✅ `manifest.go` `BuildFootnotesAndDisclaimers`，落进 `Manifest.Footnotes/Disclaimers` |
| 4 | 首屏亮点句（`highlights`） | ✅ `BuildSummarySlice` 调 `highlights(r, lang)` 落进 `SummarySlice.Highlights` |
| 5 | 会话/任务标题映射投影 | ✅ `RequestsIndex.Sessions map[string]SessionMeta`（`WriteRequestsIndex` 投影 title/alias/tasks） |
| 6 | 跨产物链接（request→journey） | ✅ `RequestsIndex.JourneyLink`（`loadStoriesLink` 派生 lineage→rendered 映射） |
| 7 | 时间双字段（`ts` + `ts_display`） | ⚠️ **部分完成** — 见下 |

**#7 未达标**：方案 §3.3/§5.6/§11.1#4 要求「每个时间点」同时落 `ts`（epoch ms）与 `ts_display`。实际：
- `TimePoint` 类型已建（`manifest.go`），`CompactionRow` 通过自定义 `MarshalJSON` 落双字段。✅
- 但 `RequestRow.TS`（`json:"ts"`，RFC3339 字符串，**无 `ts_display`**）、`SessionRow.From/To`（string）、`Row.Date/From/To`（string）、`HourRow.Date`（string）均为**单字段**。
- 直接后果：`request-browser.html` 的 Time 列 `data-sort="ts_display"` 读到 `undefined` → **请求浏览器时间列空白**（见新发现 N1）。
- §9 要求的「切片内每个时间点必须同时有 `ts` 与 `ts_display`，缺一即失败」守卫**不存在**：`TestDualTimeFields` 只覆盖 `TimePoint` 与 `CompactionRow`。

**结论：C3 部分完成**（#1–#6 完成；#7 仅 CompactionRow 达标，其余时间点及守卫缺失 → 详见 N1）。

---

### C4 — 产物提交顺序 / manifest 准入 / orphan 清扫（§3.4，D20）

- **manifest 最后写**：`dispatchAnalyze` 的 `finishAnalyze` 退出序列 = skeleton 刷新 → compares 索引重建 → `commitManifest`（最后）。✅ 失败运行不触发。✅
- **原子写**：`writeJSONAtomic` = CreateTemp + Chmod 0600 + Rename。✅
- **Go 侧完整校验**：`ValidateManifest` 校验 `format` 一致 + 每个 recorded slice 的 sha256 匹配。✅
- **准入两档**：Go 侧完整校验；浏览器侧 `common.js` `versionBehavior` 轻量比对 `format`（不做哈希校验）。✅（设计上）
- **D20 作业清单来自索引**：`renderAllFromDisk` 遍历 `journeys/index.json` 的 id，不扫目录。✅
- **D20 orphan 清扫**：`CleanOrphanJourneys` 严格限定 `journeys/details/`，非递归，只删 `j-*.{json,md}`，不碰 `compares/` 与 `requests/`。✅ `TestCleanOrphanJourneys_EdgeCases` 验证了不碰 compares/requests。✅ L2 命中路径也执行清扫（`cmd_analyze_cache.go:113` 仅 `mode == "default"`）。✅
- **偏差（见新发现 N8）**：`ValidateManifest` 只校验 manifest **已记录**的切片；若某切片写盘失败未进 `Manifest.Slices`，校验仍通过。§8.2 规定清单「就是」5 macro + index×2 + benchmarks 共 8 份，但代码不强制这 8 份齐全。记录待裁决（低危：`BuildManifest` 在 5 macro 切片都写完后才跑，缺失概率极低）。

**结论：C4 已全部完成**（N8 为健壮性边角，列入待裁决）。

---

### C5 — Journey JSON 自包含（§3.6，D18）

- **bodies blob 表**：`JourneySummary.Bodies map[string]string json:"bodies,omitempty"`，顶层字段（与方案 §3.6 示例一致），按内容 sha256 去重（`blobStore.put`）。✅
- **三级 match**：`ToolResultRef.Match` = `"exact" | "normalized" | "positional"`，`pairToolResults` 三 pass。✅ `TestBuildStructure_ThreeLevelToolPairing` 覆盖。✅
- **截断口径归一**：`maxBodyExcerptChars = 3000` 统一用于 tool args / tool result / compaction 摘录；`resp_ref`（RespText/Reasoning）不截断。✅ `TestBuildStructure_TruncationLimits` 覆盖。✅
- **Compaction 前驱摘录**：`CompactionRef.PredecessorExcerptRef` 引用 bodies。✅
- **`LosslessReconstruction` 改写**（§9）：`TestBuildStructure_LosslessReconstruction` 已改为「仅给 j-<id>.json 文件 → .md 逐字节渲染」，审计日志不在输入内。✅
- **bodies 无孤儿/无悬引用守卫**（§9）：`TestBuildStructure_BodiesNoOrphansNoDangling` + `TestBuildStructure_BodiesIntegrity`（含去重断言）。✅
- **偏差（见新发现 N2）**：方案 §3.6 明确「若启用 `-llm-addr`，解读结果（模型、耗时、状态与正文）**同理落入 `j-<id>.json` 的 `llm_interpretation`，彻底杜绝旁路拼接**」。实际：
  - `JourneySummary` 有 `LLMFindings`（✅ 检测器发现落 JSON），但**没有 `LLMInterpretation` 结构字段**。
  - `writeJourneyFile` 把 `llmSection`（渲染好的 Markdown）**只 append 到 `.md`**，不进 JSON。
  - `-render-only` 只能靠 `strings.Index(oldData, "\n## LLM ")` 从旧 `.md` 里**刮回**解读段（`cmd_render_only.go:94-97, 137-140`）——这正是方案说要"杜绝的旁路拼接"。
  - 当前功能未坏（hack 生效），但脆弱。记录待裁决（N2）。

**结论：C5 部分完成**（bodies/三级 match/截断/守卫全部达标；`llm_interpretation` 入 JSON 未落地，仍是旁路拼接 → N2）。

---

### C6 — 人读请求索引删除（§3.7 D7）+ compares 索引（§3.8 D21）

- **D7 人读请求索引**：`vmr-requests.md` / `vmr-requests-<tag>.md` / `vmr-requests-cron-*.md` 不再写出；`WriteRequestsIndex` 只写 `requests/index.json`。✅
- **`requests/failed.md` 保留**：`renderFailedIndexFromDisk` → `WriteFailedIndex`。✅
- **`requests/failed.jsonl` 下沉**：✅
- **偏差（见新发现 N3）**：`internal/report/requests.go` 里旧 Markdown 渲染机器**仍作为死代码存在**（~250 行）：`renderChatUserDoc` / `renderScheduledDoc` / `renderSessionCard` / `partitionGroups` / `groupSessions` / `cronFileTag` / `titleMaps` / `indexEntry` 全部无生产调用点（仅 `requests.go` 内部互引 + `tagSummary` 被 test 引用）。`clientsWithSiblingFile` 退化为恒返回 `nil` 的桩，仍被 `viewmodel_doc.go` 调用。配套 `i18n/report_requests.go` 里大量 `RequestsText` 字段（`ChatUserLegend` / `SessionCardHeader` / `ScheduledTableHeader` 等）也随之成为死文案。§8.1「所有仓内消费者在同一变更内同步更新」+ CLAUDE.md「确定无用就整体删除」→ 应删除。记录待裁决（N3，删除量较大）。
- **D21 compares 索引**：`cmd/vmr/compares_index.go` `RebuildComparesIndex` 扫 `compares/compare-*.json` 现算，每次 analyze 重建，空目录出引导。✅ `compares/` 整树不进 manifest。✅ 手删自愈（`TestRebuildComparesIndex_ScanAndSelfHealing`）。✅
- **偏差（见新发现 N5）**：`compares/index.md` 的文案（`# Journey Comparisons`、`Total comparisons`、`Side A (Baseline)` 等）**硬编码英文**，`RebuildComparesIndex` 不接收 `lang` 参数。方案 D4「全部文案进 ViewModel，天然双语」+ §5.5「lang-follows-everywhere」+ §5.4 表格明确 `compares/index.md` 走 `-render-only` → 应跟随 manifest 语言。记录待裁决（N5）。

**结论：C6 部分完成**（D7 数据层/failed.md 达标，但旧渲染死代码未清 N3；D21 索引达标，但 index.md 未双语 N5）。

---

### C7 — 目标目录拓扑（§4，D16/D17）

| 拓扑项 | 落地 |
|---|---|
| `manifest.json`（最后写，读取方准入） | ✅ |
| `macro/{summary,finance,reliability,workloads,context-efficiency}.json` | ✅ |
| `requests/index.json`（无 .md） | ✅ |
| `requests/failed.jsonl` / `failed.md` | ✅ |
| `requests/details/r-<...>.md`（D17 `r-` 前缀） | ✅（`reqdetail.FileName` 单点改，`386f92f`/`229a896`） |
| `requests/evidence/sysprompt-<h8>.md` | ✅ 下沉 |
| `journeys/index.{json,md}` | ✅ |
| `journeys/benchmarks.{json,md}` | ✅ |
| `journeys/details/j-<id>.{json,md}`（D19 去 `journey-` 前缀、去 `-partial` 后缀） | ✅（`JourneyReportFile` 归一，`TestJourneyReportFile_Normalization`） |
| `compares/index.{json,md}` + `compare-<a>-vs-<b>.{json,md}` | ✅ |
| `*.html` 骨架页（根级平铺） | ✅ 6 页（见 C9） |
| `.cache/parse/<filehash>.json` | ✅（`.parse-cache`→`.cache/parse`，`386f92f`） |
| `.cache/llm/<key>.json` | ✅ 无隐式默认，文档推荐值 |
| 目录 0700 / 文件 0600 | ✅ `writeJSONAtomic` / `WriteSkeletons` / journey 写盘均 0600/0700 |

**结论：C7 已全部完成。**

---

### C8 — ViewModel 双轨 + 单一渲染路径（§5，D3/D4/D5/D10/D11/D12）

- **D11 单一渲染路径**：`renderAllFromDisk`（`cmd_render_only.go`）被全量运行与 `-render-only` **共用**；全量运行先写 JSON 再走同一后半段。✅ `TestRenderOnly_ByteEquivalenceWithFullRun` + `_MacroOnlyByteEquivalence` + `_BenchmarkByteEquivalence`。✅
- **D3 无模板引擎**：无 `text/template`、无 `-templates`、无模板指纹。Markdown = 固定序列化器（`SerializeJourneyVM` / report 侧 `viewmodel.go`）。✅
- **D4 文案全进 ViewModel/i18n**：report 侧 `viewmodel_*.go` builder + 配对 `i18n/report_*.go`；`archtest` `TestArchitecture_ReportI18nPairing` + `vm_literals_test.go`（`TestArchitecture_NoUnlocalizedProseInViewModels`）强制。✅
  - **例外**：`compares/index.md` 文案硬编码（N5，见 C6）。
- **D5 `-render-only` 覆盖面**：`vmr-report.md` / `journeys/index.md` / `benchmarks.md` / `journeys/details/j-<id>.md` / `compares/index.md` + `compare-*.md` / `requests/failed.md` 全覆盖；`details/`、`evidence/` 永久排除。✅
- **D10 语言继承**：`runRenderOnly` 强制 `-lang` 与 manifest 语言一致，否则拒绝。✅ `TestRenderOnly_LanguageMismatchRejected`。✅
- **D12 ViewModel 不落盘**：ViewModel 仅驻内存；HTML 侧直接消费切片 + 自带 `common.js` 格式化。✅
- **§5.6 数值格式化跨语言 fixture**：`internal/dashboard/testdata/fmt_cases.json` + Go 侧 `fmtutil` 测试 + JS 侧 `TestJS_PureFunctionsAndFixture`（Node，不进构建链）。✅
- **§5.6 时间双字段**：⚠️ 部分（见 C3 #7 / N1）。
- **§5.4 骨架页幂等刷新**：`renderAllFromDisk` step 6 + `dispatchAnalyze` + `-render-only` 均调 `dashboard.WriteSkeletons`。✅
- **golden 下沉到 VM 结构**（§9）：`viewmodel_golden_test.go` 比对 JSON 序列化的 VM 结构而非最终字符串。✅

**结论：C8 部分完成**（D3/D4/D5/D10/D11/D12 + fmt fixture 全部达标；仅时间双字段 §5.6 未完整 N1，compares index.md 文案 N5）。

---

### C9 — HTML 看板 / 自包含废弃 / `/reports/` 鉴权 / 版本 banner（§6）

- **D6 自包含 HTML 废弃**：`render_html*.go` / `render_compare_html.go` / `toolwaste_html.go` / `story/assets/` 均已删除；6 个骨架页 `internal/dashboard/assets/*.html`（macro-dashboard / request-browser / journey-viewer / journey-compare / benchmarks / tool-waste）。✅
- **D15 `-redact` 删除**：无 `-redact` / `-html` flag，无 `redact bool` 分支。✅
- **D13 `#data=` hash 传参**：骨架页根级平铺，`#data=` 相对路径与磁盘/HTTP 布局逐字一致。✅（`dashboard.go` 注释与实现一致）
- **D9 `/reports/` 鉴权**：`internal/server/reports.go`：
  - 显式开关 `analytics.serve`（关则不挂路由）。✅
  - `len(APIKeys) == 0` → 全部 403（不复用会放行的 `s.auth`）。✅
  - 路径校验：`filepath.Clean` + 前缀校验 + 全链符号链接拒绝 + 禁目录列表。✅
  - 分层鉴权：`.html` 骨架免鉴权，`.json/.jsonl/.md` 走鉴权。✅
  - `server` 不 import 分析半区（`archtest` `import_boundaries_test.go` 强制）。✅
- **D16 serve_dir**：默认 `./reports`，与 `analyze -o` 同默认；`analytics.serve_dir` 可覆盖；目录不存在 → 404 + 一次性日志，非启动错误；不走 `rundir`。✅
- **D14 / §6.6 版本 banner**：`common.js` `versionBehavior(expected, actual)` + `EXPECTED_MANIFEST_FORMAT = 11`；三分支（ok / banner / missing）；纯函数 Node 测试覆盖（`TestJS_PureFunctionsAndFixture` 的 `vbCases`）。✅（逻辑层面）
- **`file://` 降级**：骨架页有 `banner-file` 提示分支。✅

**严重偏差（见新发现 N1 — 最高优先级）**：
- 6 个骨架页均 `<script src="common.js"></script>`，但 `WriteSkeletons` **只写 `.html`，不写 `common.js`**（`TestWriteSkeletons_BasicShape` 甚至断言"root 只有这 6 个 html"）；`reportsContentTypes` 也**没有 `.js` 条目**，`isReportsSkeleton` 只认 `.html`。
- 后果：用户跑完 `vmr analyze` 打开任何看板页 → `common.js` **404** → `versionBehavior` / `FmtTokens` / `FmtBytes` / `svgLineChart` 等**全部 undefined** → **每一个看板页都渲染失败**。
- 该缺陷自 Phase 2 首个 commit（`b58d2ae`）起一直存在——部署形态从未工作过，只有直接读 embed 源的 JS 单测（`js_test.go`）通过，掩盖了缺口。
- 方案 §0.4 / §6.3 / `dashboard.go` 自身注释均写"看板是手写 HTML + 内联 CSS/JS"——外置 `common.js` 引用本身就是偏离。
- **已在本轮修复**：`WriteSkeletons` 写盘时把 `common.js` 内联进每页（`common.js` 底部有 `typeof module !== 'undefined'` 守卫，浏览器内联安全）+ 回归测试。

**次要偏差（见新发现 N10）**：骨架页 chrome（banner 文案、列头）本地化**不一致** —— `macro-dashboard.html` banner 是中文硬编码，`request-browser.html` 列头是英文硬编码，都不响应数据语言。方案未明确要求骨架 chrome 双语（§6.2「复制即可定制」），记录为优化项。

**结论：C9 部分完成**（鉴权 D9 / 拓扑 D13 / serve_dir D16 / 自包含废弃 D6/D15 全部达标且质量高；但 `common.js` 未部署导致看板整体不可用 N1 已修；chrome 本地化不一致 N10 待定）。

---

### C10 — 缓存层（§7，D1/D8）

- **D8 单一 Digest 构造**：`internal/digest` leaf 包，`Digest(...[]byte) [32]byte` = 长度前缀（uvarint）+ 内容的有序 sha256 链。✅
  - 三性质（顺序敏感 / 无歧义拼接 / 重复不抵消）+ 定宽标量编码（`EncodeInt64` 走 `binary.BigEndian`，`EncodeFloat64` 走 `math.Float64bits`，无 `fmt.Sprintf`）。✅
  - `digest_test.go` 覆盖三性质。✅
  - `d5f5070` 差分测试钉死两个 digest 构造器与 wire format。✅
  - ctxgraph md5 底座未动（`ComputeJourneyDigest` 把 `[16]byte` md5 原样作分量喂进链）。✅
- **L1 归位**：`.parse-cache`→`.cache/parse`。✅
- **L2 产物数据缓存**：`cmd_analyze_cache.go` `computeTargetL2` = `Digest(输入哈希集 ‖ pricingFP ‖ ManifestFormat ‖ paramsFP)`。✅
- **L3 表现层缓存**：`ComputeL3Digest(vmFP, RendererVersion, lang)`。✅
- **§7.2 配置指纹**：`resolvePricingFingerprint(cfg, exchangeRate)` —— 需确认只含影响金额部分（pricing overrides + exchange_rate + 标准表 stamp），未全量哈希 config。核验：`report.ComputeInputHashes` + `resolvePricingFingerprint` 分离，`listen` 地址变化不进指纹（设计意图，代码未逐字核对全部字段，列为低风险观察 N11）。
- **§7.2 分析参数**：`AnalysisParams{Lang, TaskProfile, IncludePartial, IncludeSelfTraffic, SelfTrafficTags, LLMSelfTag, LLMAddr, LLMModel, DisplayCCY, RenderAll, Details, Mode}`。
  - 方案提到"时间窗（-from/-to）"——**`vmr analyze` 实际无 `-from/-to` flag**，输入选择纯靠文件 glob（`resolveInputPaths`），由 `ComputeInputHashes` 完整捕获。方案 §7.2 此处术语与实现不符，但**非缺陷**（文件集身份已覆盖时间覆盖范围）。记录为文档-实现术语不符（N12，仅需方案脚注）。
  - LLM 身份仅在 `journey:`/`compare:` mode 进 paramsFP（其余 mode 不消费）。✅ 合理。
- **D1 整套一个指纹**：无切片级隔离，`-no-cache` 常驻旁路。✅
- **§7.4 失效矩阵**：`TestAnalyzeCache_InvalidationMatrix` 覆盖；`TestAnalyzeCache_ColdWarmAndNoCache`（冷热一致性，§9）。✅
- **§7.2 详单指纹携带 (record, manifest, prev)**：`reqdetail` 渲染指纹折入 `m`/`prev` 身份（`1b780a1` / KNOWN_ISSUES 2.x），`Test... prevmanifest` 存在。✅

**结论：C10 已全部完成**（N11 配置指纹逐字段核对、N12 术语不符列为观察/文档项，均不影响正确性）。

---

### C11 — manifest format + 职责边界（§8）

- **format 10→11**：`ManifestFormat = 11`（`manifest.go:26`）。✅ 切片不带独立版本号。✅
- **§8.2 三职责**：准入与一致性（`Slices map[string]SliceRef` + sha256）；溯源（`Inputs`/`GeneratedAt`/`Timezone`/`TimeRange`/`Lang`/`Format`）；发现（`Slices` 路径→语义 key 映射）。✅
- **manifest 不承载指标数值**：`Manifest` 无任何 metric 字段；record 计数等在 `SummaryMeta`。✅
- **§8.1 无兼容期**：CHANGELOG 记 Breaking Change；`meta` 无 `deprecated`。基本符合。
  - **偏差**：`cmd_journey_setup.go` 读旧 `stories/vmr-stories.json`（已修 N6）；`requests.go` 顶层 `vmr-requests.json` 副本死分支（N7）。

**结论：C11 已全部完成**（N6 已修，N7 待裁决）。

---

### C12 — 测试守卫（§9）

| §9 守卫 | 状态 |
|---|---|
| journey golden 下沉到 VM 层 | ✅ `viewmodel_golden_test.go` 比对 VM 结构 |
| `LosslessReconstruction` 改写为真自包含断言 | ✅ `structure_test.go` |
| `e2e_test.go` + 切片并集≡Report2 等价 | ✅ `TestMacroSlices_EquivalenceWithReport2` |
| 详单跨 report/journey 字节一致（+ 渲染器版本） | ✅ `ensure_test.go` / `journey_prevmanifest_test.go` |
| i18n_e2e + "ViewModel 无裸字面量" 守卫 | ✅ `archtest/vm_literals_test.go` |
| archtest 行数预算登记 `viewmodel_*.go` | ✅ `func_sizes_test.go` / `file_sizes_test.go` |
| archtest i18n 配对对象改 ViewModel builder | ✅ `i18n_test.go` |
| archtest：`server` 不 import 分析半区 | ✅ `import_boundaries_test.go` |
| **新增** render-only 与全量运行字节一致 | ✅ `TestRenderOnly_ByteEquivalenceWithFullRun` |
| **新增** 缓存命中 vs 冷启动一致（浮点容差） | ✅ `TestAnalyzeCache_ColdWarmAndNoCache` |
| **新增** 跨语言格式化 fixture | ✅ `TestJS_PureFunctionsAndFixture` + fmtutil 侧 |
| **新增** 每个时间点必须有 `ts` + `ts_display` | ❌ **缺失**（`TestDualTimeFields` 仅覆盖 TimePoint/CompactionRow，不是全切片遍历）→ N1 |
| **新增** 骨架页版本探测纯函数测试 | ✅ `js_test.go` 的 `vbCases` |
| **新增** bodies 无孤儿/无悬引用（双向） | ✅ `TestBuildStructure_BodiesNoOrphansNoDangling` |
| **新增** 三级 `match` 与脊柱渲染一致 | ✅ `TestBuildStructure_ThreeLevelToolPairing`（+ golden 端到端） |
| **新增** 更窄时间窗重跑无 orphan 残留 | ✅ `TestCleanOrphanJourneys` / `TestAnalyzeCache_L2HitSweepsOrphanJourneys` |
| **新增** orphan 清扫不动 compares/requests | ✅ `TestCleanOrphanJourneys_EdgeCases` |
| **新增** `compares/index.json` ≡ 目录内容 | ✅ `TestRebuildComparesIndex_ScanAndSelfHealing` |

**结论：C12 部分完成**（17 条新增守卫中 16 条落地；「每个时间点 ts+ts_display」守卫缺失，且正是该守卫缺失让 N1 的 `RequestRow` 缺 `ts_display` 未被发现）。

**附注**：多处 test 函数仍名为 `TestCmdStory_*`（如 `TestCmdStory_Compare`）——过时命名，非功能问题，列入 N9 注释清理范畴。

---

### C13 — 不变量（§11.1）

| 不变量 | 核验 |
|---|---|
| 1. 两半区一条契约（`report`/`journey`/... 不 import `router`/`server`/`config`；`server` 不 import 分析半区） | ✅ `archtest/import_boundaries_test.go` |
| 2. 内容寻址底座不动摇（缓存判据只能是内容哈希） | ✅ `digest` 包 + `ctxgraph.HashFile` sha256，无 mtime fast path |
| 3. 权限底线 0600/0700（含骨架页、缓存目录） | ✅ |
| 4. 单一时区权威（`ts` + `ts_display` 双字段） | ⚠️ **部分**（见 N1）——`RequestRow` 等未落 `ts_display`，"前端不做时区换算"在 request-browser 处实际未兑现 |
| 5. 不新增双账本（manifest 不放指标 / 切片不交叉引用 / 前端不重算） | ✅ manifest 无指标；切片独立落标量 |

**结论：C13 部分完成**（不变量 1/2/3/5 达标；不变量 4 因 N1 未完整兑现）。

---

## 第三部分：总结

### A. 基于方案的 Review 事项完成度

| 簇 | 事项 | 状态 |
|---|---|---|
| C1 | 概念归一 story→journey / corpus→benchmark | **部分完成** |
| C2 | 领域切片拆分 D1/D2 | **已全部完成** |
| C3 | 补齐渲染期现算 7 类事实 | **部分完成** |
| C4 | 提交顺序 / manifest 准入 / orphan 清扫 D20 | **已全部完成** |
| C5 | Journey JSON 自包含 D18 | **部分完成** |
| C6 | 人读请求索引删除 D7 / compares 索引 D21 | **部分完成** |
| C7 | 目标目录拓扑 D16/D17 | **已全部完成** |
| C8 | ViewModel 双轨 / 单一渲染路径 D3/D4/D5/D10/D11/D12 | **部分完成** |
| C9 | 骨架页 / 自包含废弃 / 鉴权 / 版本 banner D6/D9/D13/D14/D15/D16 | **部分完成** |
| C10 | 单一 Digest / 三级缓存 / 指纹规范 D1/D8 | **已全部完成** |
| C11 | manifest format 10→11 / 职责边界 D2 | **已全部完成** |
| C12 | 测试守卫（既有迁移 + 17 新增） | **部分完成** |
| C13 | 五条不变量 | **部分完成** |

#### 部分完成事项详述

##### C1 — 概念归一（部分完成）

- **问题描述**：核心更名（包、CLI、写盘路径）已完成，但 (a) 渲染产物中有指向 `stories/` 的失效链接；(b) i18n 文案指向已删除的 `vmr-report.json` / `vmr-story-corpus.json`；(c) ~200 处注释与 test 函数名仍用 `vmr story`/`vmr report` 作命令名、`-corpus` 作 flag 名；(d) `cmd_journey_setup.go` 保留旧索引兼容 shim。
- **根因分析**：多 agent 并行实施，重命名主体（结构/路径）由专门任务完成，但"渲染输出里的字符串常量"和"注释里的历史称谓"分散在 i18n / viewmodel / 各 leaf 包，未纳入同一次机械替换；§8.1「同一变更内同步更新所有消费者」在文案层未贯彻。
- **建议方案**：(a)(b)(d) 本轮已直接修复（见 B 部分 N4/N5/N6）；(c) 单独一次 `sed` 式专项：`vmr story`→`vmr analyze`（命令语境）、`vmr-story-corpus`→`journeys/benchmarks`、test 函数 `TestCmdStory_*`→`TestCmdAnalyze_*`，逐文件人工过一遍避免误伤"report/story 两个消费者角色"的合理描述。
- **ROI**：(c) 中等工作量（~30 文件注释 + ~15 test 重命名），零功能风险，纯可读性/一致性收益。建议作为一次独立低优 commit，不阻塞。

##### C3 / C13#4 — 时间双字段（部分完成）

- **问题描述**：`ts` + `ts_display` 双字段只在 `TimePoint` 类型和 `CompactionRow` 落地；`RequestRow`（`json:"ts"` RFC3339 字符串，无 `ts_display`）、`SessionRow.From/To`、`Row.Date/From/To`、`HourRow.Date` 均为单字段。`request-browser.html` 的 Time 列 `data-sort="ts_display"` 因此读到 `undefined`。§9 要求的"每个时间点 ts+ts_display 缺一即失败"守卫不存在。
- **根因分析**：`TimePoint` 类型是正确的底座，但只在新增的 `CompactionRow` 上启用；既有 row 类型的时间字段（本就是 string）在切片化时被原样搬运，没有统一改造。骨架页 JS 按"理想契约"（`ts` 数值 + `ts_display` 串）编写，数据层只兑现了一半，两侧漂移未被测试拦住。
- **建议方案**：
  1. 最小修复（已在本轮做）：给 `RequestRow` 加 `TSDisplay string json:"ts_display"`，`buildRequestRow` 用 `rc.ts.In(fmtutil.DisplayZone)` 填充——直接修好 request-browser 时间列。
  2. 完整方案（待裁决）：将 `RequestRow.TS` / `SessionRow.From,To` / `HourRow` 等全部收敛为 `TimePoint`（`ts` epoch ms + `ts_display`），更新对应 builder、golden、`requests_failed.go` 的时间解析、骨架页 JS 排序键；补 §9 的"全切片时间点遍历"守卫。
  3. `ts` 用 epoch ms 而非 RFC3339 的理由：`.Format("...Z07:00")` 用源偏移，跨不同时区来源的记录字符串排序会错位，方案 §5.6"epoch ms 供排序"正是为此。
- **ROI**：最小修复 = 高 ROI（4 行代码修好一个可见 UI bug）；完整方案 = 中 ROI（涉及 ~6 row 类型 + 多个 golden + JS 排序键），建议排一个专项任务，与"补 §9 守卫"绑定。

##### C5 — Journey JSON `llm_interpretation`（部分完成）

- **问题描述**：`-llm-addr` 的解读结果（模型/耗时/状态/正文）只 append 到 `j-<id>.md`，不进 `j-<id>.json`。`-render-only` 靠 `strings.Index(oldData, "\n## LLM ")` 从旧 `.md` 刮回。方案 §3.6 明确要求它落 JSON 的 `llm_interpretation` 字段以"彻底杜绝旁路拼接"。
- **根因分析**：`JourneySummary` 在 P4 阶段设计时只考虑了 Metrics/Findings/Structure，`llm_findings`（检测器结果）后来补上了，但"整段解读叙事"（`RenderLLMSection` 的输出）一直被当作渲染期产物 append 到 `.md`。`writeJourneyFile` 先写 JSON 再渲染 MD 再 append `llmSection` 的顺序，天然把 `llmSection` 排除在 JSON 之外。
- **建议方案**：给 `JourneySummary` 加 `LLMInterpretation *InterpretResult json:"llm_interpretation,omitempty"`（含 model / duration / status / body / scope），`NewJourneySummary` 多接一个参数，`writeJourneyFile` 把 `res` 而非渲染好的 `llmSection` 存进去；viewmodel 侧从该字段渲染 `## LLM` 段；删掉 `cmd_render_only.go` 两处 `strings.Index` hack。compare 侧（`Comparison` JSON + `RenderComparisonMarkdown`）同构处理。
- **ROI**：中等工作量（1 个结构字段 + 3~4 处 threading + viewmodel 渲染 + compare 对称 + 删 hack）。当前 hack 功能未坏，所以**不紧急**；但它是方案点名要消除的东西，且 hack 对 `## ` 标题格式敏感（一旦本地化标题改字就断）。ROI 中等，建议纳入下一轮。

##### C6 — 旧请求索引渲染死代码（部分完成，N3）

- **问题描述**：`internal/report/requests.go` 保留 ~250 行旧 Markdown 请求索引渲染代码（`renderChatUserDoc`/`renderScheduledDoc`/`renderSessionCard`/`partitionGroups`/`groupSessions`/`cronFileTag`/`titleMaps`/`indexEntry`），无任何生产调用点；`i18n/report_requests.go` 的对应文案字段同为死文案；`clientsWithSiblingFile` 退化为恒 `nil` 桩仍被调用。
- **根因分析**：D7 的实施把 `WriteRequestsIndex` 改成只写 JSON，但没有回头删掉它上下文里的 Markdown 渲染函数——"改行为"和"删旧实现"是两步，第二步漏了。`archtest` 行数预算是软上限，不会因"有死函数"报错。
- **建议方案**：删除上述函数及其私有依赖、`sessGroup` 类型、`i18n/report_requests.go` 中仅服务这些函数的字段；`clientsWithSiblingFile` 连同 `viewmodel_doc.go:373` 的调用点一起简化（该处 `withSibling` 集恒空）；同步删 `requests_test.go` 中测死代码的用例（`tagSummary` 的 inKnown/outKnown 若确有价值，把该逻辑并入 `RequestRow` 聚合的测试）。
- **ROI**：中等工作量、需要仔细确认无遗漏调用，功能风险低（纯删死代码）。收益：`requests.go` 与 `report_requests.go` 显著瘦身，`archtest` 预算下调，符合方案 §8.1 与 CLAUDE.md。建议排专项。

##### C6 — `compares/index.md` 未双语（部分完成，N5）

- **问题描述**：`renderComparesIndexMarkdown` 的所有标题/表头硬编码英文，`RebuildComparesIndex` 不接 `lang`。
- **根因分析**：`compares_index.go` 由独立 agent 实现，D21 关注点是"扫目录派生索引"这一机制，双语要求（D4/§5.5）没被带进这个任务的验收标准；该文件也不在 `archtest` i18n 配对强制范围内（不是 `viewmodel_*.go`）。
- **建议方案**：新增 `i18n/compares_index.go`（或复用 `i18n.Journey`），`RebuildComparesIndex(comparesDir, lang)` 接收语言，`renderAllFromDisk` / `dispatchAnalyze` / `recordPostAnalyzeCache` / `tryL2Cache` 的调用点传 `r.lang` / manifest 语言。
- **ROI**：小-中工作量（1 个 i18n 文件 + 5 处调用点签名），零功能风险。收益：兑现 lang-follows-everywhere。建议纳入下一轮。

##### C8 — 见 C3（时间双字段）与 C6（compares index.md）

##### C9 — 见 B 部分 N1（common.js，已修）与 N10（chrome 本地化，待定）

##### C12 — "每个时间点 ts+ts_display" 守卫缺失

- 见 C3。补该守卫应与"时间字段收敛为 TimePoint"同一个任务完成，否则守卫一加就红。

---

### B. 评审过程新发现问题

| # | 问题 | 严重度 | 处置 |
|---|---|---|---|
| N1 | `common.js` 未部署 → 所有看板页渲染失败 | **高（看板整体不可用）** | **已直接解决** |
| N2 | `llm_interpretation` 未入 journey JSON，`-render-only` 靠字符串刮取旧 md | 中 | 未解决（待裁决，见 C5） |
| N3 | `requests.go` ~250 行旧渲染死代码 + `report_requests.go` 死文案 | 中 | 未解决（待裁决，见 C6） |
| N4 | 渲染产物中 `stories/` 失效链接（会话表、journey 回链、report 回链层级错） | 中（失效链接） | **已直接解决** |
| N5 | `compares/index.md` 硬编码英文，不跟随语言 | 中 | 未解决（待裁决，见 C6） |
| N6 | `cmd_journey_setup.go` 保留旧 `stories/vmr-stories.json` 兼容 shim | 低 | **已直接解决** |
| N7 | `WriteRequestsIndex` 顶层 `vmr-requests.json` 兼容副本死分支 | 低 | 部分解决（见下） |
| N8 | `ValidateManifest` 不校验 8 份切片是否齐全，只校验已记录的 | 低 | 未解决（待裁决，见下） |
| N9 | ~200 处注释 + test 函数名仍用 `vmr story`/`vmr report`/`-corpus` | 低 | 未解决（待裁决，见下） |
| N10 | 骨架页 chrome 本地化不一致（macro 中文 / request-browser 英文，均不响应数据语言） | 低 | 未解决（待裁决，见下） |
| N11 | `resolvePricingFingerprint` 未逐字段核对是否只含影响金额部分 | 低（观察） | 未解决（观察项） |
| N12 | 方案 §7.2 提"时间窗 -from/-to"，`vmr analyze` 无此 flag | 极低（文档） | 未解决（方案脚注） |
| N13 | `RequestRow` 缺 `ts_display`（N1 的数据层另一半，request-browser Time 列空白） | 中 | **已直接解决**（最小修复） |

#### 已直接解决项

- **N1（common.js 未部署）**：`WriteSkeletons` 改为写盘时把 `assets/common.js` 内联进每个骨架页（替换 `<script src="common.js"></script>`），保持"页面自包含"（方案 §6.3 / `dashboard.go` 注释的原意）；`common.js` 底部 `typeof module !== 'undefined'` 守卫保证内联进浏览器 `<script>` 安全。新增回归断言：写出的页面不得含 `src="common.js"` 且必须含 `versionBehavior`。选择"内联"而非"额外 serve 一个 .js"：避免扩大 server 的 content-type 面和免鉴权面。
- **N4（stories/ 失效链接）**：
  - `viewmodel_sessions.go`：会话表 journey 链接前缀 `stories/` → `journeys/`（+ golden data 2 处同步）。
  - `viewmodel_build.go`：journey 详单 → report 回链 `../vmr-report.md` → `../../vmr-report.md`（journey 详单在 `journeys/details/`，比旧 `stories/` 深一级）。
  - `i18n/journey_render.go`：`BackLinkLine` 的"返回 vmr-stories.md" → "返回 ../index.md"（双语）。
  - 重新生成 `internal/journey/testdata/golden*.{md,json}`（4 份）。
- **N6（旧索引兼容 shim）**：删除 `cmd_journey_setup.go` 读 `stories/vmr-stories.json` 的 fallback 分支——`LoadJourneyIndex` 本就优雅处理缺失文件，`prior` 退化为空索引即可（§8.1 无兼容期）。
- **N13（RequestRow 缺 ts_display）**：`RequestRow` 加 `TSDisplay string json:"ts_display"`，`buildRequestRow` 用 `fmtutil.DisplayZone` 填充。修好 request-browser Time 列。`ts` 暂保持 RFC3339 字符串（完整收敛为 epoch ms 见 C3 待裁决项）。
- **附带**：i18n 文案 `vmr-report.json -> tools[]` → `macro/context-efficiency.json -> tools[]`（`report_efficiency.go` `ToolWasteTitle`，真实渲染）；`vmr-story-corpus.json` → `journeys/benchmarks.json`（`journey_benchmarks.go` 相关性溢出行）；`report_toolwaste.go` 死 Footnote 文案同步（虽当前未渲染）。

#### 部分解决项

##### N7 — `vmr-requests.json` 顶层兼容副本死分支

- **问题描述**：`WriteRequestsIndex`（`requests.go:147-159`）当 `filepath.Base(dir) != "requests"` 时，除写 `requests/index.json` 外还写一份顶层 `{dir}/vmr-requests.json`。当前唯一调用点（`cmd_report.go:336`）传的就是 `.../requests`，该分支不执行。
- **根因分析**：迁移期为兼容旧调用惯例留的双写，迁移完成后未清理。
- **建议方案**：`WriteRequestsIndex` 直接要求 `dir` 为 `requests` 目录（或内部 `filepath.Join(dir, "requests")` 归一），删掉 `targetDir != dir` 的顶层副本写入。
- **本轮处置**：未改（属 N3 死代码清理同一批，避免碎片化 commit）。ROI：极小工作量，随 N3 一起做。

##### N8 — `ValidateManifest` 不强制 8 份切片齐全

- **问题描述**：`BuildManifest` 遍历 `AllSlicePaths`，`os.Stat` 成功才加入 `Manifest.Slices`；`ValidateManifest` 只校验"已记录的切片存在且哈希匹配"。若某切片（如 `requests/index.json`）写盘失败，manifest 少记一条，校验仍通过 → 读取方拿到"缺切片但 manifest 有效"的快照。
- **根因分析**：方案 §3.4 强调的是"写到一半崩溃 → 缺 manifest → 整套无效"，重点在 manifest 最后写这条纪律；"某切片写成功但内容缺失"不在威胁模型内。但 §8.2 明确清单"就是"这 8 份。
- **建议方案**：`ValidateManifest` 增加一步：`MacroSlicePaths` 5 份必须都在 `m.Slices` 中（`requests/index.json` / `journeys/*` 视 mode 可选——`-macro-only` 不产 journeys）。或 `BuildManifest` 在 5 macro 切片缺任一时直接返回 error 而非静默少记。
- **本轮处置**：未改（需要判断哪些切片在哪些 mode 下是必须的，有设计判断成分）。ROI：小工作量、中等设计判断，建议下一轮明确 mode×切片必备矩阵后一次做对。

#### 未解决项（待裁决）

##### N2 — `llm_interpretation` 入 JSON

见 A 部分 C5 详述。**建议**：下一轮做，与删 `-render-only` 的 `## LLM` hack 绑定。

##### N3 — 旧请求索引渲染死代码清理

见 A 部分 C6 详述。**建议**：专项 commit，一次删干净 `requests.go` + `report_requests.go` + 相关 test。

##### N5 — `compares/index.md` 双语

见 A 部分 C6 详述。**建议**：下一轮做，工作量小。

##### N9 — 注释与 test 函数名的历史称谓清理

- **问题描述**：`grep -rn 'vmr story\|vmr report' --include=*.go` 命中 ~60 文件（含 `internal/ctxgraph`、`fmtutil`、`pricing`、`quota`、`audit` 等 leaf 包的 doc comment），test 函数 `TestCmdStory_*` ~15 个，注释里 `-corpus` / `vmr-stories.json` / `vmr-report.json` 若干。
- **根因分析**：命令更名是结构性改动，注释里的历史称谓分散极广，多 agent 实施时无人负责"全仓注释扫一遍"。部分 `report`/`story` 是在描述**两个消费者角色/两半区**（合理），部分是在指**已删除的命令**（过时）——需要人工甄别，不能无脑替换。
- **建议方案**：一次独立的注释清理 pass：命令语境 `vmr story`→`vmr analyze` / `vmr story -compare`→`vmr analyze -compare`；`vmr-stories.json`（指当前文件时）→`journeys/index.json`；test 函数 `TestCmdStory_*`→`TestCmdAnalyze_*`；保留"report 半区 / journey 半区"这类角色描述。逐文件人工过。
- **ROI**：低优先级，中等工作量，零功能风险。纯一致性与"新读者不被误导"收益。方案 §2.1「把 story 一词从代码、CLI 与产物中一次清干净」在注释层尚未兑现。

##### N10 — 骨架页 chrome 本地化

- **问题描述**：骨架页的固定文案（banner、列头、空状态提示）部分中文硬编码（`macro-dashboard.html`、`journey-viewer.html` 的 banner）、部分英文硬编码（`request-browser.html` 列头），互不一致，也都不响应数据语言。
- **根因分析**：方案对骨架 chrome 的语言没有硬性要求（§6.2 强调"复制即可定制"，D4 针对的是 Markdown ViewModel）。多 agent 各写各的页，风格没统一。
- **建议方案**：二选一——(a) 统一为英文（骨架是模板，数据区跟随切片语言即可）；(b) 骨架页启动时读 `manifest.json` 的 `lang` 切换一套内置 i18n dict。推荐 (a)，成本低、符合"模板"定位。
- **ROI**：低。视觉一致性收益，非功能问题。建议明确一个方向后统一。

##### N11 — 配置指纹字段核对

- **问题描述**：未逐字段确认 `resolvePricingFingerprint` 只纳入"影响金额的部分"（provider `pricing.rates` overrides + 顶层 `exchange_rate` + 标准表 `GeneratedAt`），且确实排除了 `listen` 等无关字段。
- **建议方案**：读 `resolvePricingFingerprint` 实现 + 补一个"改 listen 地址不使 L2 失效 / 改 rate override 使 L2 失效"的定向测试（`TestAnalyzeCache_InvalidationMatrix` 可能已部分覆盖）。
- **ROI**：低（大概率已正确），作为一次 code-read + 补测。

##### N12 — 方案 §7.2 术语

- 方案 §7.2 列"时间窗（-from/-to）"为分析参数，但 `vmr analyze` 无此 flag。**建议**：方案 §7.2 加一句脚注说明当前实现以文件 glob 选择输入、时间覆盖由输入文件集的内容哈希捕获，不再单列 `-from/-to`。零代码。

---

### C. 进一步改进/优化机会（不属于方案缺口）

1. **流式序列化削峰值内存**（方案 §11.3 顺带机会）：切片化已把产物写入拆段，`WriteMacroSlices` 目前顺序 build 5 个 slice 结构再逐个 marshal，可改为 build-marshal-release 逐切片，顺手削 RSS 峰值。低优。
2. **bodies blob key 用短哈希前缀**：`structure.go` 的 `hashText` 用全长 hex sha256（64 字符）作 `bodies` map key，在 JSON 里每个 ref 出现多次。改用前 16 hex（8 字节，碰撞概率对单 journey 规模可忽略）可缩小 `j-<id>.json` 体积。需权衡：跨 journey 无需全局唯一，前缀足够。低优。
3. **`clientsWithSiblingFile` 彻底移除**：随 N3 一起，`viewmodel_doc.go` 里 `withSibling` 恒空的分支可整段简化。
4. **`structureExcerptChars` 别名**：`structure.go` 保留 `structureExcerptChars = maxBodyExcerptChars` 仅为"向后兼容既有引用"，可一次性把引用点改名后删别名（CLAUDE.md 反对兼容 shim）。
5. **`request-browser.html` 排序键**：默认 `sortKey = 'ts'` 但 `ts` 是 RFC3339 字符串——若源时区不一，字符串排序错位。随"时间字段收敛为 epoch ms"（C3 完整方案）一并解决。

---

## 附：本轮直接修复清单（commit 级）

| commit | 内容 | 涉及文件 |
|---|---|---|
| （见 git log） | fix(analytics): repair cross-artifact links broken by the journeys/ topology move | `report/viewmodel_sessions.go`, `journey/viewmodel_build.go`, `i18n/journey_render.go`, `journey/testdata/golden*`, `report/viewmodel_golden_data_test.go` |
| （见 git log） | fix(i18n): point rendered footnotes at post-redesign slice paths | `i18n/report_efficiency.go`, `i18n/report_toolwaste.go`, `i18n/journey_benchmarks.go` (+ 受影响 golden) |
| （见 git log） | fix(dashboard): inline common.js into skeleton pages so deployed dashboards work | `dashboard/dashboard.go`, `dashboard/dashboard_test.go` |
| （见 git log） | fix(requests): emit ts_display on RequestRow so the request browser's time column renders | `report/rows.go`, `report/recextract.go` (+ 受影响 golden) |
| （见 git log） | refactor(analyze): drop the legacy stories/ index compat shim (§8.1) | `cmd/vmr/cmd_journey_setup.go` |

### 验证结果

- `go build ./...`：通过
- `go vet ./...`：通过
- `go test ./...`：全绿
- `go test -race ./internal/dashboard/... ./internal/report/... ./internal/journey/... ./cmd/vmr/...`：全绿
- `go test ./internal/archtest/...`：通过（i18n 配对 / 导入边界 / 行数预算无回归）
- `gofmt`：本轮改动的文件均 clean（`cmd_journey_setup.go` 等 6 个文件在本机 Go 1.26.5 下有 gofmt 提示，但 `go.mod` 与 CI 用 Go 1.25.1，属 1.26 尾随注释对齐规则变化，非本轮引入，未改）
- **端到端实跑**（`vmr analyze` against `logs/vmr-audit-2026-08-24/25`）：
  - 骨架页 `common.js` 已内联（`grep -c 'src="common.js"'` = 0，`versionBehavior` 存在）
  - `vmr-report.md` 会话表链接 = `[s47 (l-...)](journeys/details/j-...md)`（旧 `stories/` 已消除）
  - `journeys/details/j-*.md` 回链 = `← 返回 [index.md](../index.md) · [vmr-report.md](../../vmr-report.md)`，两个相对路径均能解析到实际文件
  - `requests/index.json` 每行含 `"ts_display": "2026-08-24 00:53:29"`
  - `vmr-report.md` 工具浪费小标题 = `完整明细见 macro/context-efficiency.json -> tools[]`
  - `vmr analyze -render-only` 产物与全量运行**逐字节一致**（`diff -rq` 除 `.cache/` 外无差异，D11 单一渲染路径未受影响）
  - `manifest.json` `format` = 11

### 新发现问题补充：N14

评审中另注意到 `cmd/vmr/compares_index.go`、`internal/archtest/func_sizes_test.go`、`internal/i18n/journey_compare.go`、`internal/i18n/lang_test.go`、`internal/journey/benchmarks_coverage.go` 在 Go 1.26 gofmt 下非 canonical（尾随注释对齐），`go.mod` 声明 `go 1.25.1`。**非缺陷**（CI 用 1.25，当前通过），但仓库升级到 Go 1.26 时需一次全仓 `gofmt -w`。仅作记录，无需本轮处理。
