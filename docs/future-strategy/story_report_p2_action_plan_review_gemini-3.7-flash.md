// Ver 2026-08-20 00:30, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P2 执行计划（ActionPlan）与落地代码综合评审报告

## 0. 评审概述与最终裁决

本文档对 `docs/future-strategy/story_report_p2_action_plan_sonnet-5.md`（以下简称 **P2 ActionPlan**）及其在当前工作区中的**实际未提交代码改动（P2 落地实现态）**进行了全面的事实核查、架构推演与端到端运行验证。

### 最终裁决结论

当前工作区内未提交的 P2 代码改动（涉及 `internal/ctxgraph/`、`internal/reqdetail/`、`internal/report/`、`internal/i18n/`、`cmd/vmr/` 及相关测试与文档）**高度符合** Opus 架构文档与 DevPlan P2 的设计目标：
1. **全量测试 100% 绿灯**：`go test ./...` 与 `go test ./internal/archtest/... -race` 全绿通过，零编译警告与并发数据竞争；
2. **第一轮 Review 指出的所有阻断级与高风险问题均已彻底解决或合理处置**：包括物理路径 I/O 保护、文件名消除 `ErrorClass` 分裂、缓存跨工作目录重绑定、`PrevTurnLink` 接口闭环等；
3. **真实语料端到端验证通过**：在实际多文件审计日志（`logs/vmr-audit-*.jsonl.zst`）上运行，`vmr report`（`vmr-requests.json`）与 `vmr story`（`vmr-stories.json`）产出的请求坐标 `req` 达到 **100% 互通 join**（322/322），详单命名完全一致且具备跨时区确定性。

---

## 1. 前序评审问题闭环复核表（Audit of Previous Review Items）

下表逐项复核第一轮评审所列问题在当前工作区代码中的实际处置情况：

| 编号 | 评审问题 | 原定梯队 | 当前处理状态 | 源码依据与实际实现 |
| --- | --- | :---: | :---: | --- |
| **2.1** | `Manifest.Path` 规范化破坏文件 I/O | Tier 1 (Blocker) | ✅ **彻底解决** | `internal/ctxgraph/manifest.go:96` 保持 `Path: path`（可解析物理路径），新增 `Req: ReqCoord(path, line)` 字段与方法；`BlobIndex.FetchAll` 正常打开文件 |
| **2.2** | `FileName` 纯 Manifest 计算与 `ErrorClass` 冲突 | Tier 1 (High) | ✅ **彻底解决** | `internal/reqdetail/detail.go:69-80` 剔除装饰性 `errorClass`，严格对齐 `{ts}_{virtual}_{real}_{outcome}_{hash8}.md`；`FileNameForRecord` 与 `FileNameForManifest` 逐字节相等 |
| **2.3** | `FileCache` 跨工作目录复用导致路径失效 | Tier 1 (High) | ✅ **彻底解决** | `internal/ctxgraph/cache.go:154-156` 命中缓存时将 `cached.Manifests` 的 `m.Path` 重新绑定为当前扫描路径，并补齐回归测试 `TestScanCached_HitRebindsManifestPathToCurrentInvocation` |
| **2.4** | `PrevTurnLink` 接口缺口待定 | Tier 1 (High ROI) | ✅ **彻底解决** | `reqdetail.FileNameForManifest(prev)` 确定性从 predecessor Manifest 计算文件名，彻底终结传递庞大 `*audit.Record` 的复杂方案 |
| **2.5** | `session.go` 反向依赖 `reqdetail` | Tier 1 (Architecture) | 🟢 **务实收敛** | `reqdetail` 定位为“per-record 事实提取 + 详单渲染”，`CLAUDE.md` 明确文档定义；`reqdetail` 保持纯叶子包，不反向 import `report`，架构单向边界成立 |
| **3.1** | `RequestRow` 装配点定位笔误 | Tier 2 (Fact) | ✅ **彻底解决** | `internal/report/recextract.go:43` 正确为 `rr.Req` 赋值，`rows.go:505` 发布 `Req string \`json:"req,omitempty"\`` |
| **3.2** | 非聊天/被拒绝记录（`m == nil`）边界渲染 | Tier 2 (Edge Case) | ✅ **彻底解决** | `reqdetail/detail_test.go:471` 增加 `TestRender_RejectedRecordNoManifest`，断言 `(rejected)`/`none` 降级，零 panic |
| **3.3** | 既有测试 `cache.Files` 索引同步 | Tier 2 (Testing) | ✅ **彻底解决** | `internal/report/build_cached_test.go:97` 与 `internal/ctxgraph/cache_test.go` 全量更新为 `cache.Files[CanonicalPath(path)]` |
| **4.1** | 废弃批次计数器与锁清理 | Tier 3 (Hygiene) | ✅ **彻底解决** | `DetailWriter.used` 与 `session.go` 的 `detailFileNameFromInfo` 已完全删除，`assignNames` 采用坐标哈希计算 |
| **4.2** | 包体积与预算拆分 | Tier 3 (Hygiene) | ✅ **彻底解决** | `internal/reqdetail` 拆分为 `detail.go` (585)、`facts.go` (258)、`render.go` (201)、`diff.go` (325) 四个清晰文件；`report/detail.go` 降至 287 行 |

---

## 2. 代码库深度核查与新发现（Deep Dive & New Findings）

通过对工作区全部 diff 与真实运行产物的校验，发现以下几处值得记录的技术细节与防护建议：

### 2.1 [验证脚本陷阱] `vmr-stories.json` 的 JSON 嵌套结构

* **位置**：ActionPlan §2.3 具体步骤第 7 步验证脚本
* **现象**：ActionPlan 提供的 Python 验证代码片段写为：
  ```python
  b = {f"{m['path']}:{m['line']}" for f in sto['files'].values() for m in f.get('manifests', []) if m}
  ```
* **事实**：`StoryIndex.Files` 结构体为 `FileCache{Files: map[string]CachedFile}`，序列化为 JSON 后的层级为 `sto['files']['files']`。原脚本直接对 `sto['files'].values()` 迭代会取到 inner map 而非 `CachedFile` 对象。
* **正确验证逻辑**：
  ```python
  import json
  req = json.load(open('/tmp/p2verify/vmr-requests.json'))
  sto = json.load(open('/tmp/p2verify/stories/vmr-stories.json'))
  a = {r['req'] for r in req['requests'] if 'req' in r}
  b = {m['req'] for f in sto['files']['files'].values() for m in f.get('manifests', []) if m and 'req' in m}
  print('report side:', len(a), 'story side:', len(b), 'joinable:', len(a & b), 'report-only:', len(a - b))
  # 实测输出: report side reqs: 322 story side reqs: 322 joinable: 322 report-only: 0
  ```
* **评价**：代码库实现本身完全正确，`Manifest.Req` 在两端均已正确发布，仅为验证脚本的示例取值层级笔误。

---

### 2.2 [架构防护建议] 在 `archtest` 中补充 `reqdetail` 的禁止引入规则

* **位置**：`internal/archtest/import_boundaries_test.go`
* **现状**：
  `CLAUDE.md` 中已写明：
  > *“Two halves, one contract. `report`/`story`/`ctxgraph`/`taskseg`/`chatmsg`/`reqdetail` never import `router`/`server`/`config`”*
  但目前 `import_boundaries_test.go` 的 `forbiddenImports` 表中已注册 `report`、`ctxgraph`、`story`、`taskseg`、`chatmsg`，**尚未登记 `reqdetail`**。
* **建议（可作为收尾或后续步骤）**：
  在 `forbiddenImports` 中补充：
  ```go
  "vmr/internal/reqdetail": {
      "vmr/internal/router",
      "vmr/internal/server",
      "vmr/internal/config",
      "vmr/internal/report",
      "vmr/internal/story",
  },
  ```
  进一步巩固“叶子包永不依赖消费方与路由半区”的自动化断言防御。

---

### 2.3 [文档与基线同步状态核查]

核对当前未提交的工作区文档改动：
1. **`CHANGELOG.md`**：在 `[Unreleased]` 下准确补充了 `req` 坐标发布、`internal/reqdetail` 下沉、确定性命名与时区解耦的变更条目；
2. **`CLAUDE.md`**：模块表准确增加了 `reqdetail` 的职责定位描述；
3. **`docs/KNOWN_ISSUES_sonnet-5.md`**：§1.5（`detail.go` 拆分闭环）、§1.20（P2 前提达成，挂链接排期至 P5.2）均已按真实完成态更新；
4. **`docs/VirtualModelRouter_Design_v4_Analytics.md`**：§4.6 涉及的文案与顿号迁移引用已对齐新路径 `internal/reqdetail/detail.go`；
5. **`docs/future-strategy/story_report_architecture_opus-5.md`**：§2.2 增加了 P1/P2 完成态的历史基线说明备注。

---

## 3. 综合评估与后续阶段建议

P2 阶段的核心使命是**建立统一的请求级坐标，并将单请求详单彻底剥离为纯函数**。

从目前工作区的代码实现来看：
- **微观层（`reqdetail`）** 已实现极高的内聚度与确定性：无论通过 `vmr report` 全量扫描、单文件扫描还是未来的 `vmr story` 按需生成，针对同一条物理记录生成的 Markdown 内容与文件名**逐字节完全一致**；
- **坐标层（`req`）** 为后续 P3（按坐标直取原始记录、微观机读层消除 JSON 副本）、P4（中观叙事机读结构化）、P5（人读层脊柱挂链接）提供了坚实自洽的基础。

**结论**：当前工作区代码质量扎实，逻辑严密，测试覆盖完整，完全达到 P2 阶段的验收与交付标准，可以安全提交。
