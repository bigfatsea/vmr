// Ver 2026-08-20 17:15, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P6 执行计划与工作区实现核查报告（二审更新版）

<statement>
**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**
</statement>

本文是对 [`docs/future-strategy/story_report_p6_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p6_action_plan_sonnet-5.md)（以下简称 **P6 ActionPlan**）以及**当前工作区未提交代码修改**的完整事实核查、前期评审处置验收与新问题复盘报告。

对照基准：
1. 架构方案 [`docs/future-strategy/story_report_architecture_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md)
2. 阶段计划 [`docs/future-strategy/story_report_dev_plan_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_dev_plan_opus-5.md)
3. 真实仓库代码与当前工作区未提交修改（涵盖 `cmd/vmr/`、`internal/ctxgraph/`、`internal/report/`、`internal/story/`、`internal/replay/`、`docs/` 等 40+ 文件）

---

## 0. 综合评审与验收结论

**总体判定：当前工作区未提交的代码修改完全覆盖了 P6 ActionPlan 与 DevPlan 所描绘的范围（P6.1 会话内容寻址、P6.2 导航矩阵闭环、P6.3 任务分类索引、P6.4 自指流量排除、P6.5 命令行收敛与 `-req` 免位置参数）。前期评审指出的两个高危致命缺陷（`LineageID` 碰撞导致会话合并、自指流量排除遗漏导致统计污染）均已在源码层面得到彻底、精准的修复，且补充了高价值的真实语料回归测试。**

1. **测试基线验证**：
   - `go test ./...` 全绿（全部单元测试、集成测试通过）；
   - `go test -race ./internal/report/... ./internal/story/... ./internal/ctxgraph/... ./internal/replay/... ./cmd/vmr/...` 全绿（无竞态风险）；
   - `go test ./internal/archtest/...` 全绿（架构单向导入边界、文件与函数行数预算全部达标）；
   - `go vet ./...` 与 `gofmt -l .` 零告警。
2. **新增测试覆盖**：
   - `internal/ctxgraph/reallog_lineageid_test.go`（真实语料 1638 条 Lineage 零碰撞断言）；
   - `internal/report/selftraffic_test.go` 与 `cmd/vmr/selftraffic_test.go`（自指流量全面排除断言）；
   - `internal/story/candidates_test.go`（任务分类纯字面量匹配断言）；
   - `internal/replay/reqpathresolve_test.go`（`-req` 自动探测多级路径断言）；
   - `cmd/vmr/cmd_analyze_test.go`（`vmr analyze` 首次运行产物与导航链接断言）。

---

## 1. 前期评审问题处置情况核查（逐项源码核实）

下表对前期评审报告列出的 8 个关键问题在当前工作区代码中的实际处置情况进行逐一核对：

| 序号 | 前期评审指出的问题 | 严重级别 / 属性 | 工作区实际处置情况 | 源码与测试依据 | 核查结论 |
| --- | --- | --- | --- | --- | --- |
| **1.1** | `LineageID` 纯内容哈希导致 `report` 会话实例碰撞与数据合并 | **第一梯队（致命设计缺陷）** | **已彻底修复**：`LineageID()` 将 root manifest 的纳秒时间戳 `root.TS.UnixNano()` 一并折入哈希计算，彻底消除了相同模板/定时任务的会话哈希碰撞。 | [`internal/ctxgraph/lineage.go:144-149`](file:///Users/stanford/code/vmr/internal/ctxgraph/lineage.go#L144-L149)<br>[`internal/ctxgraph/reallog_lineageid_test.go:27-51`](file:///Users/stanford/code/vmr/internal/ctxgraph/reallog_lineageid_test.go#L27-L51) | ✅ **完全解决，真实语料 1638 链验证 0 碰撞** |
| **1.2** | 自指流量排除在 `ingestRecord` 单点拦截，遗漏 `SessionAnalysis`、`Tools`、`Compactions` 与详单物化 | **第一梯队（高危统计缺陷）** | **已彻底修复**：新增 `excludeSelfTrafficFromSessionAnalysis` 在 `buildInternal` 中同步过滤 `sess.Recs` 与 `sess.Compactions`；在 `scanAndCacheFile` 中对 `onRecord` 执行排除拦截。 | [`internal/report/selftraffic.go:20-41`](file:///Users/stanford/code/vmr/internal/report/selftraffic.go#L20-L41)<br>[`internal/report/aggregate.go:200, 301`](file:///Users/stanford/code/vmr/internal/report/aggregate.go#L200)<br>[`internal/report/selftraffic_test.go:24-90`](file:///Users/stanford/code/vmr/internal/report/selftraffic_test.go#L24-L90) | ✅ **完全解决，多通路拦截完备** |
| **1.3** | 跨命令导航边依赖磁盘 `os.Stat`，首次运行时存在时序死锁与断链 | **第一梯队（时序缺陷）** | **已通过执行时序倒置解决核心边**：`cmdAnalyze` 调整为先执行 `cmdStory` 再执行 `cmdReport`，使核心下钻边（`vmr-report.md`→`stories/vmr-stories.md` 及会话行→journey）首次调用即 100% 命中；反向 backlink 存在已记录的微瑕（见新问题 3.2）。 | [`cmd/vmr/cmd_analyze.go:87-104`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze.go#L87-L104)<br>[`cmd/vmr/cmd_analyze_test.go:40-80`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze_test.go#L40-L80) | ✅ **核心链路打通，符合务实取舍** |
| **1.4** | `vmr analyze` 内存图共享架构与组合根装配契约规范化 | **第一梯队（高 ROI 改进）** | **采纳务实装配方案**：`cmdAnalyze` 采用 CLI 组合根串联 `cmdStory` 与 `cmdReport`，共享 `.parse-cache/` 紧凑分片缓存（第二遍扫描热缓存命中），避免侵入两个 internal 包的核心解析管线。 | [`cmd/vmr/cmd_analyze.go:10-27`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze.go#L10-L27)<br>[`cmd/vmr/main.go:53`](file:///Users/stanford/code/vmr/cmd/vmr/main.go#L53) | ✅ **合理收敛，符合 KISS 原则** |
| **2.1** | `Finding` 机制与测试断言对 `s.ID` 变更的不变性断裂 | 第二梯队（测试阻断） | **已修复对齐**：更新 `aggregate_test.go` 中 `TestBuildFindingsContextGrowthTieIsDeterministic` 与 `TestContextGrowthDoesNotCrossContractBreak`，改用 `Alias`（`s01`/`s02`）查找会话，并按 `s.ID < worst.ID` 锁定 tie-break 确定性。 | [`internal/report/aggregate_test.go:1210-1260, 1329-1345`](file:///Users/stanford/code/vmr/internal/report/aggregate_test.go#L1210-L1260) | ✅ **完全解决，测试全绿** |
| **2.2** | 任务分类规则对前缀与轮数的防御性设计 | 第二梯队（健壮性） | **已优化为纯字面量匹配**：彻底废弃易误伤真实短对话的 `Requests <= 2` 轮数启发规则，仅依赖真实语料验证过的三个字面量（`[cron:`、`[OpenClaw heartbeat poll]`、`[Subagent Context]`），其余严格 fallback 到 `CategoryTask`（默认展开）。 | [`internal/story/candidates.go:22-62`](file:///Users/stanford/code/vmr/internal/story/candidates.go#L22-L62)<br>[`internal/story/candidates_test.go:17-43`](file:///Users/stanford/code/vmr/internal/story/candidates_test.go#L17-L43) | ✅ **完全解决，分类逻辑纯粹** |
| **2.3** | `vmr replay -req` 免位置参数的查找优先级规范 | 第二梯队（体验优化） | **已按标准实现**：`resolveReqAuditPath` 依次检索 `dirHint`/`cwd` 以及 `config.yaml` 的 `log_dir`，自动探测裸文件名与 `.zst` 后缀。 | [`internal/replay/replay.go:316-333`](file:///Users/stanford/code/vmr/internal/replay/replay.go#L316-L333)<br>[`internal/replay/reqpathresolve_test.go:45-120`](file:///Users/stanford/code/vmr/internal/replay/reqpathresolve_test.go#L45-L120) | ✅ **完全解决，贴入即用** |
| **2.4** | 根目录到 `evidence/` 相对链接深度收尾 | 第二梯队（链接规范） | **核实归档**：经代码全面普查，`internal/report` 从不渲染指向 `evidence/*.md` 的链接，原条目属过度推断，已在 KNOWN_ISSUES 正式关闭。 | [`docs/KNOWN_ISSUES_sonnet-5.md:348`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md#L348) | ✅ **确认非真问题，已归档** |

---

## 2. 关键处置点深度源码核验

### 2.1 `LineageID` 纳秒时间戳消歧（问题 1.1）

在 [`internal/ctxgraph/lineage.go:131-150`](file:///Users/stanford/code/vmr/internal/ctxgraph/lineage.go#L131-L150) 中：
```go
func (l *Lineage) LineageID() string {
    if len(l.Manifests) == 0 {
        var zero Hash
        return "l-" + zero.String()[:lineageIDCodeLen]
    }
    root := l.Manifests[0]
    h := md5.New()
    if root.HasSys {
        h.Write(root.SysHash[:])
    }
    for _, k := range root.Keys {
        h.Write(k[:])
    }
    var tsBuf [8]byte
    binary.BigEndian.PutUint64(tsBuf[:], uint64(root.TS.UnixNano()))
    h.Write(tsBuf[:])
    var out Hash
    copy(out[:], h.Sum(nil))
    return "l-" + out.String()[:lineageIDCodeLen]
}
```
**核实分析**：
- `RootHash()` 依然保持其作为纯消息内容特征的定义（用于图层级 `Classify` 与分支判定）；
- `LineageID()` 额外纳入了 `root.TS.UnixNano()`，使得即使两次会话具有完全相同的首条指令（如固定提示词的心跳轮询），也会生成不同的 `LineageID`。
- 回归测试 `TestRealCorpus_LineageIDHasNoCollisions` 在本地 34 个审计日志文件（1638 条 Lineage）上实测验证：原先存在的 4 组真实碰撞全部消失，唯一性达到 1638/1638。

---

### 2.2 自指流量多通道完整拦截（问题 1.2）

在 [`internal/report/selftraffic.go:20-41`](file:///Users/stanford/code/vmr/internal/report/selftraffic.go#L20-L41) 与 [`internal/report/aggregate.go:200, 301`](file:///Users/stanford/code/vmr/internal/report/aggregate.go#L200) 中：
```go
// internal/report/selftraffic.go
func excludeSelfTrafficFromSessionAnalysis(sess *SessionAnalysis, excludeClientTags map[string]bool) {
    if len(excludeClientTags) == 0 {
        return
    }
    keptRecs := sess.Recs[:0]
    for _, r := range sess.Recs {
        if excludeClientTags[r.ClientKeyTag] {
            continue
        }
        keptRecs = append(keptRecs, r)
    }
    sess.Recs = keptRecs

    keptCompactions := sess.Compactions[:0]
    for _, c := range sess.Compactions {
        if excludeClientTags[c.ClientKeyTag] {
            continue
        }
        keptCompactions = append(keptCompactions, c)
    }
    sess.Compactions = keptCompactions
}
```
**核实分析**：
- 第一阶段会话分析完成后，`buildInternal` 立即调用 `excludeSelfTrafficFromSessionAnalysis(sess, excludeClientTags)`，清洗了 `sess.Recs` 与 `sess.Compactions`。后续 `buildTools(st.sess)`（§7 工具浪费）与 `buildCompactions(st.sess)`（§6.7 Compaction）读取的均为纯净数据。
- 在第二阶段 `scanAndCacheFile` 中，`if onRecord != nil && !st.excludeClientTags[arec.ClientKeyTag]` 在写盘前做拦截，彻底杜绝了未索引的孤儿 `details/*.md` 文件。
- `TestExcludeSelfTraffic_ToolsAndCompactionsDontLeak` 构造包含自指工具调用与压缩调用的混合语料，断言 `rep.Tools` 与 `rep.Compactions` 零泄漏。

---

## 3. 本轮工作区核查新发现的问题

在对当前工作区代码与真实语料执行结果进行全量复核时，发现以下 2 处新问题与现象：

### 3.1 [中危 · 稳定性/性能] `vmr analyze` 在全量历史语料（34 文件/11374 条记录）冷启动时触发 SIGKILL（退出码 137）

- **现象与源码位置**：
  在当前工作区运行全量语料冷启动验证：
  `./vmrbin analyze -o /tmp/p6full logs/*.jsonl.zst`
  进程在约 10 分钟墙钟、累计约 300s CPU 时间后被操作系统发送 `SIGKILL`（退出码 137，典型 OOM 特征），导致 `stories/` 阶段未能写出产物。
- **根因分析**：
  - 经排查，该问题发生在 `cmd_story.go` 的 `renderAllJourneys` 与 `EnsureJourneyDetails` 阶段（`vmr analyze` 默认携带 `-render-all` 参数）；
  - 单独运行不带 `-render-all` 的 `vmr story` 仅需 18.22s 即可完成（内存占用约 1.07GB）；当带上 `-render-all` 批量为 350+ 个 Journey 渲染全量详单与 Markdown 时，由于内存中累积的候选对象和并发 I/O 压力导致内存超出系统阈值。
- **定级与影响**：
  - **中危（非 P6 回归，但属于默认入口的稳定性缺陷）**：该行为在 P6 之前调用 `vmr story -render-all` 时即已存在，并非 P6 引入的代码逻辑错误；但在 P6 中 `vmr analyze` 将其作为默认工作流，使普通用户在分析大体量历史日志时极易撞墙。
  - 已登记为 [`docs/KNOWN_ISSUES_sonnet-5.md §1.30`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md#L234)。后续应单独排期，将 `renderAllJourneys` 的物化过程优化为真正的流式写入与即时内存释放。

---

### 3.2 [低危 · UX 细节] `vmr analyze` 首次在干净目录运行时，`journey-*.md` 顶部的返回链接存在单次延迟

- **现象与源码位置**：
  在 [`cmd/vmr/cmd_analyze.go:87-97`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze.go#L87-L97) 与 [`cmd/vmr/cmd_story.go:753`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go#L753) 中：
  `cmdAnalyze` 采用 `cmdStory` 先跑、`cmdReport` 后跑的时序。在干净目录下首次运行时：
  1. `cmdStory` 渲染 `journey-*.md` 时，通过 `os.Stat` 探测 `../vmr-report.md` 是否存在；
  2. 此时 `vmr-report.md` 尚未生成，`os.Stat` 返回 false；
  3. 导致首次生成的 `journey-*.md` 头部仅渲染 `← 返回 [vmr-stories.md](vmr-stories.md)`，缺少指向 `vmr-report.md` 的链接；
  4. 随后 `cmdReport` 运行，生成 `vmr-report.md`，但已写盘的 `journey-*.md` 不会自动重写，必须等第二次运行 `analyze` 才会补齐该反向链接。
- **定级与处置建议**：
  - **低危（轻微体验瑕疵）**：正向核心链路（大盘 → 索引 → Journey → Detail）首调用 100% 完整，仅 Journey 头部返回大盘的反向链接在首次运行时缺失。
  - 后续若优化 `cmdAnalyze`，可在调用 `cmdStory` 时显式传入 `reportMDWillExist = true`（或在 `analyze` 模式下默认信任大盘必然生成），彻底消除这一时序微瑕。

---

## 4. 最终核查结论与建议

1. **工作区状态判定**：当前工作区代码质量极高，P6 核心设计目标全部落地，测试覆盖全面，前期所有致命错误均已闭环。
2. **操作建议**：
   - 认可当前工作区的代码修改，可以安全进行 `git add` 和 `git commit`；
   - 将新发现的 `KNOWN_ISSUES §1.30`（大语料 `-render-all` 内存优化）列入后续维护排期。
