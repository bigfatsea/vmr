<!-- Ver 2026-08-21 21:15, by Gemini 3.7 Flash -->

**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**

---

# P13 执行计划（ActionPlan）全面事实核查、修改审阅与处置验收报告

## 0. 审阅与核查总览

本报告针对当前工作区所有未 commit 修改与 [`docs/future-strategy/story_report_p13_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p13_action_plan_sonnet-5.md) 的执行范围、实际执行结果展开源码级事实核查与闭环验收。

### 核心核查结论
1. **范围与执行结果 100% 吻合**：当前工作区修改完全覆盖了 P13.1 ~ P13.5 的 5 项任务，改动范围严格限制在声明的模块内，无意外 diff 或越界修改。
2. **审阅问题（F-01 ~ F-06）全部得到高标准处置与闭环**：
   - 🔴 **F-01（严重漏洞）**：`ensureJourneyFile` 漏物化详单导致 404 的问题已在 [`cmd/vmr/cmd_story.go:726`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go#L726) 修复，并配备了专属回归测试 [`TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze_test.go#L315)。
   - 🔴 **F-02（高 ROI 优化）**：`detailCell` 逐行 `os.Stat` 导致 2.3 万次系统调用放大的问题已在 [`internal/report/requests.go:618`](file:///Users/stanford/code/vmr/internal/report/requests.go#L618) 优化为单次目录扫描构建内存集合 `buildDetailFileSet`，将复杂度降为纯内存 $O(1)$ 查找，并配齐了 [`TestBuildDetailFileSet`](file:///Users/stanford/code/vmr/internal/report/render_cells_test.go#L75)。
   - 🟡 **F-03 ~ F-06**：涵盖 `-journey` 精准点名物化断言（F-03）、隐藏文件与非 `.md` 过滤（F-04）、`linkEvidence=true` 且 `LCP=0` 边界测试（F-05）、`aggregate_test.go` 等编译适配（F-06）均已全部落地并验证。
3. **工程质量与不变量完整保持**：
   - `go build ./...`、`go vet ./...`、`gofmt -l .`、`go test ./... -race`、`go test ./internal/archtest/...` **全部 100% 通过**。
   - P2 核心不变量（`vmr report -details` 与 `vmr analyze -render-all` 产物逐字节完全一致）通过跨命令端到端测试严格锁定。
   - `CHANGELOG.md`、`docs/KNOWN_ISSUES_sonnet-5.md`（§1.35/§1.36 移入 §3 第 38 项，高危清零）、`story_report_full_review_opus-5.md` 同步更新完毕。
4. **未发现任何新的设计遗漏、事实错误或阻断性缺陷**，当前未 commit 修改已具备完全就绪的提交条件。

---

## 1. 未 commit 修改与 ActionPlan 范围一致性核查

对工作区中涉及的 15 个改动文件与 2 个新增文档逐项核验：

| 模块 / 文件 | ActionPlan 规划范围 | 实际代码变更核查 | 判定 |
|---|---|---|---|
| [`cmd/vmr/cmd_story.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go) | P13.1：`writeJourneyFile`/`renderJourneys`/`renderAllJourneys` 追加 `materializeDetails bool` | 已实现参数透传；单条、`-compare`、`-journey` 多选、`-render-all` 显式传 `true`；修复 F-01 早退分支 | ✅ 完全一致 |
| [`cmd/vmr/cmd_analyze.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze.go) | P13.1：`dispatchAnalyze` 默认套件传 `r.renderAllFlag`，`-journey` 传 `true` | 行 229 与 247 分别传 `true` 与 `r.renderAllFlag` | ✅ 完全一致 |
| [`internal/reqdetail/detail.go`](file:///Users/stanford/code/vmr/internal/reqdetail/detail.go) | P13.2：流式响应删除 Raw SSE 全文，改为 `RawSSERef` 坐标引用；P13.3：`deltaStart` 前历史折叠为上一轮链接 | 行 556 替换为 `RawSSERef`；行 401-411 引入 `haveDelta && i < deltaStart` 折叠并保留摘要行 | ✅ 完全一致 |
| [`internal/reqdetail/render.go`](file:///Users/stanford/code/vmr/internal/reqdetail/render.go) | P13.2/P13.3：`renderTemplateVersion` 从 1 提升至 2 | 行 67 常量置为 `2`，使已存盘旧详单下次运行自动过期重写 | ✅ 完全一致 |
| [`internal/i18n/reqdetail_detail.go`](file:///Users/stanford/code/vmr/internal/i18n/reqdetail_detail.go) | P13.2/P13.3：`RawSSERef` 与 `HistoryFoldedNote` 中英文文案 | 字段及 `Detail(ZH)`/`Detail(EN)` 函数均已实现并指向 `vmr replay -print -req` | ✅ 完全一致 |
| [`internal/report/requests.go`](file:///Users/stanford/code/vmr/internal/report/requests.go) | P13.4：链接判据从 flag 改为事实；F-02：`buildDetailFileSet` 内存集合 | 实现 `buildDetailFileSet`（带 F-04 隐藏文件过滤），`detailCell` 改为内存集合检索 | ✅ 完全一致 |
| [`internal/report/rows.go`](file:///Users/stanford/code/vmr/internal/report/rows.go) | P13.4：更新 `Meta.DetailsEnabled` 注释 | 行 69-80 注释准确反映 P13.4 语义 | ✅ 完全一致 |
| [`cmd/vmr/cmd_report.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_report.go) | P13.4：`runReport` 调用 `detailsPresentFor` 与传递 `detailDir` | 行 258 `detailsPresentFor`、行 389/406 传 `detailDir` | ✅ 完全一致 |
| [`cmd/vmr/cmd_analyze_test.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze_test.go) | P13.5：体积纪律常驻守卫；F-01/F-03 回归测试 | 新增 `DefaultSuiteDoesNotMaterializeDetails`、`JourneySelectorMaterializesOnlyItsOwnDetails`、`CompareMaterializesDetailsEvenIfReportAlreadyExists` | ✅ 完全一致 |
| [`internal/reqdetail/detail_test.go`](file:///Users/stanford/code/vmr/internal/reqdetail/detail_test.go) | P13.2/P13.3 单元测试；F-05 边界测试 | 新增 Raw SSE 引用、历史折叠、`prev==nil`、LCP=0、EvidenceLinked+LCP=0 共 5 个新测试 | ✅ 完全一致 |
| [`internal/report/render_cells_test.go`](file:///Users/stanford/code/vmr/internal/report/render_cells_test.go) | P13.4 单元测试；F-02/F-04 集合测试 | 新增 `TestDetailCell_*` 与 `TestBuildDetailFileSet` | ✅ 完全一致 |
| [`internal/report/aggregate_test.go`](file:///Users/stanford/code/vmr/internal/report/aggregate_test.go) | 测试签名适配（F-06） | 4 处 `WriteRequestsIndex`/`WriteFailedIndex` 改传 `filepath.Join(dir, "details")` | ✅ 完全一致 |
| 文档同步文件 | CHANGELOG、KNOWN_ISSUES、DevPlan 状态收尾 | `CHANGELOG.md` 补全条目；`KNOWN_ISSUES` 移入 §3 第 38 项，高危清零；DevPlan 标记已完成 | ✅ 完全一致 |

---

## 2. 审阅所列问题（F-01 ~ F-06）处置情况逐项核实

### F-01 【严重设计漏洞】`ensureJourneyFile`（`-compare` 路径）漏物化详单漏洞
* **核查结果**：✅ **彻底处置并验证**
* **源码验证**：
  在 [`cmd/vmr/cmd_story.go:726`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go#L726)：
  ```go
  func ensureJourneyFile(j *story.Journey, storiesDir string, lang i18n.Lang, prof taskseg.Profile, detailDir, evidenceDir string) error {
      base := journeyBaseName(j)
      mdPath := filepath.Join(storiesDir, base+".md")
      if _, err := os.Stat(mdPath); err == nil {
          story.EnsureJourneyDetails(os.Stderr, j, detailDir, evidenceDir, prof, lang)
          return nil
      }
      ...
  }
  ```
* **测试验证**：[`TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze_test.go#L315) 模拟了"先运行默认套件生成 `.md` 但不物化详单，随后运行 `-compare`"的完整场景，断言 `details/` 目录下详单必须被补齐物化。测试通过。

---

### F-02 【高 ROI 性能优化】`detailCell` 消除 2.3 万次 `os.Stat` 系统调用放大
* **核查结果**：✅ **彻底处置并验证**
* **源码验证**：
  在 [`internal/report/requests.go:618-632`](file:///Users/stanford/code/vmr/internal/report/requests.go#L618-L632)：
  ```go
  func buildDetailFileSet(detailDir string) map[string]struct{} {
      entries, err := os.ReadDir(detailDir)
      if err != nil {
          return nil
      }
      set := make(map[string]struct{}, len(entries))
      for _, e := range entries {
          name := e.Name()
          if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
              continue
          }
          set[name] = struct{}{}
      }
      return set
  }
  ```
  `WriteRequestsIndex` 与 `WriteFailedIndex` 入口处单次调用 `buildDetailFileSet`，将 `detailSet` 传入 `renderChatUserDoc`、`renderScheduledDoc`、`writeAllRequestsFooter`、`renderSessionCard` 以及 `detailCell`。表格单元格检索完全在内存中 $O(1)$ 完成。
* **测试验证**：[`TestBuildDetailFileSet`](file:///Users/stanford/code/vmr/internal/report/render_cells_test.go#L75) 与 [`TestDetailCell_LinksOnlyWhenFileActuallyExists`](file:///Users/stanford/code/vmr/internal/report/render_cells_test.go#L44) 均通过。

---

### F-03 【高 ROI 测试完备性】常驻守卫补齐 `-journey` 精准点名物化断言
* **核查结果**：✅ **彻底处置并验证**
* **测试验证**：[`TestCmdAnalyze_JourneySelectorMaterializesOnlyItsOwnDetails`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze_test.go#L283) 构造了包含 2 个独立 Candidate 的语料，通过 `-journey <idA>` 点名其中一条，精确断言 `details/` 目录下**恰好生成 2 份详单（仅属于该 Candidate，而非全量 4 份）**。测试通过。

---

### F-04 【边界防御】目录探测与文件集合忽略隐藏文件与非 Markdown 文件
* **核查结果**：✅ **彻底处置并验证**
* **源码验证**：`buildDetailFileSet` 内显式排除了 `e.IsDir()`、`strings.HasPrefix(name, ".")`（如 `.DS_Store`、`.reqdetail-*.tmp`）以及 `!strings.HasSuffix(name, ".md")`。
* **测试验证**：[`TestBuildDetailFileSet`](file:///Users/stanford/code/vmr/internal/report/render_cells_test.go#L75) 专门测试了 `.DS_Store`、`.reqdetail-tmp123.md`、`notes.txt`、`subdir.md/` 等噪音条目，断言它们绝不会误入集合。

---

### F-05 【边界测试】`linkEvidence=true` 且 `leadSys == deltaStart`（LCP=0）边界测试
* **核查结果**：✅ **彻底处置并验证**
* **测试验证**：[`TestRenderClientRequest_EvidenceLinkedZeroLCPDoesNotFold`](file:///Users/stanford/code/vmr/internal/reqdetail/detail_test.go#L420) 构造了带系统提示词且 `LCP=0` 的两轮记录，断言系统提示词走 evidence 链接、用户消息带 🆕 完整渲染，且**绝无 `HistoryFoldedNote`（"previous turn's detail page"）折叠行**。测试通过。

---

### F-06 【文档与签名适配】`aggregate_test.go` 与 `cmd_report.go` 编译修复
* **核查结果**：✅ **彻底处置并验证**
* **源码验证**：[`internal/report/aggregate_test.go:487,790,811,832`](file:///Users/stanford/code/vmr/internal/report/aggregate_test.go#L487) 已将参数改为 `filepath.Join(dir, "details")`；[`cmd/vmr/cmd_report.go:389,406`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_report.go#L389) 已将参数改为 `detailDir`；[`internal/report/rows.go:69`](file:///Users/stanford/code/vmr/internal/report/rows.go#L69) 注释已同步更新。整个仓库编译无报错。

---

## 3. 深度代码走查与潜在风险排查

在本次复核中，对以下关键机制进行了深层穿透排查：

1. **`cmd_report.go:detailDirHasFiles` 的安全性评估**：
   - `detailDirHasFiles` 仅用于为 `rep.Meta.DetailsEnabled`（§8 文案提示）提供布尔值，不参与任何表格单元格的实际链接拼接；
   - 实际表格单元格由 `detailCell` 结合 `buildDetailFileSet` 做严格的文件级存在性与扩展名过滤，因此即使该目录存在未知文件，也绝对**不会产生任何 404 死链接**。
2. **`renderTemplateVersion` 递增到 2 的生效路径**：
   - [`internal/reqdetail/render.go:67`](file:///Users/stanford/code/vmr/internal/reqdetail/render.go#L67) 递增后，[`internal/reqdetail/ensure_test.go:102`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure_test.go#L102) 的 `TestEnsureRendered_RewritesOnStaleTemplateVersion` 仍保持通过，且所有历史 v1 详单在重新跑 analyze/report 时均能被感知并平滑重写。
3. **P2 跨命令生成详单逐字节一致性**：
   - [`cmd/vmr/cmd_story_report_crosscheck_test.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story_report_crosscheck_test.go) 运行 `vmr story -render-all` 与 `vmr report -details`，断言生成的全部详单逐字节比对完全一致，测试通过。
4. **架构守护与代码行数预算（`archtest`）**：
   - `runReport` 函数曾因注释过长接近行数上限，经优化收敛注释后，顺利通过 `TestArchitecture_FuncSizes`（硬预算 121 行内）及所有包依赖边界检查。

---

## 4. 终审结论

1. 当前工作区的所有未 commit 修改**完全符合** P13 ActionPlan 的规划目标与范围，无疏漏、无越界。
2. 前期审阅报告指出的 F-01 至 F-06 共 6 项问题（包括严重漏洞与高 ROI 优化）**已全部被合理、彻底地处置**，并配齐了严格的反证与回归测试。
3. 未发现任何新的代码缺陷或未闭环事项，代码库处于健康、稳定、随时可安全 commit 的状态。
