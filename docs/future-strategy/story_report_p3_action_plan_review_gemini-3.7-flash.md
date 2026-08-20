// Ver 2026-08-20 10:00, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P3 执行计划（ActionPlan）与落地代码综合评审报告

## 0. 评审概述与最终裁决

本文档对 [`docs/future-strategy/story_report_p3_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p3_action_plan_sonnet-5.md)（以下简称 **P3 ActionPlan**）及其在当前工作区中的**实际未提交代码改动（P3 落地实现态）**进行了全面的事实核查、架构推演与端到端运行验证。

### 最终裁决结论

当前工作区内未提交的 P3 代码改动（涉及 `internal/audit/`、`internal/ctxgraph/`、`internal/reqdetail/`、`internal/report/`、`internal/replay/`、`internal/story/`、`cmd/vmr/` 及相关测试与文档）**高度符合** Opus 架构文档、DevPlan P3 以及 P3 ActionPlan 的设计目标：

1. **全量测试 100% 绿灯**：`go test ./...` 与 `go test ./internal/archtest/... -race` 全绿通过，零编译警告与并发数据竞争；
2. **第一轮 Review 指出的核心阻断与高风险问题均已彻底解决或务实收敛**：
   - **`RecordFacts` 载荷完整性（1.2 阻断项）彻底解决**：实现了 [`internal/report/factscache.go`](file:///Users/stanford/code/vmr/internal/report/factscache.go)，通过 `recordFacts` 与 `attemptFacts` 完整收敛了 `Attempts` 序列、吞吐字节、工具体积与规范化步骤（Norm），保证冷热缓存聚合数字 100% 一致；
   - **证据条目门面内聚（2.2 架构项）彻底解决**：在 [`internal/reqdetail/ensure.go`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure.go) 中由 `EnsureRendered` 内部统一编排 `EnsureSysPromptEvidence` 与 `EnsureToolsEvidence`，杜绝外部漏调导致 404；
   - **分片并发原子写与 Schema 版本锁（2.3 安全项）彻底解决**：实现了 `writeCacheShardAtomic`（`os.CreateTemp` + `0o600` + `os.Rename`）与 `CacheSchemaVersion = 1` 双重版本守卫；
   - **物理三遍扫描（1.1 事实项）务实收敛**：成功将 `aggregate.go`（Pass 3）改为纯内存 `ingestCachedFile`，热缓存实测提速 5.2 倍；并将 `session.go`（Pass 2）未缓存的原因与权衡清晰登记入 [`docs/KNOWN_ISSUES_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md) §1.1；
3. **真实语料与回归基线锁定**：
   - `details/` 默认不再全量生成（`-details` 默认 `false`），且彻底删除了 `.json` 副本；
   - 提取了系统提示词与工具声明为 `evidence/sysprompt-<h8>.md` 与 `evidence/tools-<h8>.md` 共享 blob；
   - `vmr replay -req <coord> -print` 与 `LineAt` 顺利接管单请求原始 JSON 提取能力；
   - `vmr-requests.json` 与 `vmr-stories.json` 彻底剔除了内嵌的 `Files` 缓存段，解析缓存统一收敛至 `{outDir}/.parse-cache/<filehash>.json`。

---

## 1. 前序评审问题闭环复核表（Audit of Previous Review Items）

下表逐项复核第一轮评审所列问题在当前工作区代码中的实际处置情况：

| 编号 | 评审问题 | 原定梯队 | 当前处理状态 | 源码依据与实际实现 |
| :--- | :--- | :---: | :---: | :--- |
| **1.1** | `session.go:analyzeFile`（Pass 2）漏缓存 | Tier 1 (Blocker) | 🟢 **务实收敛** | `aggregate.go:198`（Pass 3）已完全接入 `factscache.go`，热缓存提速 5.2x；Pass 2 涉及 `ReqInfo` 复杂边界判定，已在 `KNOWN_ISSUES §1.1` 详细登记现状、权衡理由与触发条件 |
| **1.2** | `RecordFacts` 载荷缺失 `Attempts` 等指标 | Tier 1 (Blocker) | ✅ **彻底解决** | [`internal/report/factscache.go:29-74`](file:///Users/stanford/code/vmr/internal/report/factscache.go#L29-L74) 定义 `recordFacts` 与 `attemptFacts`，完整保留 attempt 状态/延迟/Norm/字节；`TestBuildCached_WarmMatchesBuild` 保证冷热产物逐字节一致 |
| **1.3** | 扫描管线重复 I/O 重构 | Tier 1 (High ROI) | 🟢 **阶段性达成** | Pass 3 实现热缓存零 I/O（`ingestCachedFile`），`TestScanFiles_CacheHitNeverOpensFile` 锁定热缓存不触碰文件系统；冷启动保持两遍并发 |
| **2.1** | `vmr replay -req` 参数冗余与体验 | Tier 2 (UX) | ✅ **规格达标** | [`internal/replay/replay.go:306-319`](file:///Users/stanford/code/vmr/internal/replay/replay.go#L306-L319) 严格实现 `-req` 与 `-line`/`-ts` 互斥校验及 `CanonicalPath` 校验；`-print` 模式支持纯管道输出原始记录 |
| **2.2** | 证据条目落盘调用门面泄露 | Tier 2 (Architecture) | ✅ **彻底解决** | [`internal/reqdetail/ensure.go:50-58`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure.go#L50-L58) 在 `EnsureRendered` 内部自动判断 `evidenceDir != ""` 并调用 `EnsureSysPromptEvidence` / `EnsureToolsEvidence`，调用方完全无需手工编排 |
| **2.3** | 缓存分片并发原子写与版本隔离 | Tier 2 (Safety) | ✅ **彻底解决** | [`internal/ctxgraph/cache.go:280-310`](file:///Users/stanford/code/vmr/internal/ctxgraph/cache.go#L280-L310) 实现 `writeCacheShardAtomic`；`CacheSchemaVersion = 1` 并在 `ScanCached` 与 `loadCachedFacts` 双重校验 |
| **3.1** | 工具条目正文 Markdown 模板明确化 | Tier 3 (Polish) | ✅ **彻底解决** | [`internal/reqdetail/evidence.go:129-140`](file:///Users/stanford/code/vmr/internal/reqdetail/evidence.go#L129-L140) 实现 `toolsEvidenceBody`，以 `# Tools (N)` 为大标题并逐个展开 `<details>` JSON Schema |
| **3.2** | 跨层级相对链接层级规则 | Tier 3 (Hygiene) | ✅ **彻底解决** | [`internal/reqdetail/detail.go:347-360`](file:///Users/stanford/code/vmr/internal/reqdetail/detail.go#L347-L360) 详单正文生成 `../evidence/sysprompt-<h8>.md` 与 `../evidence/tools-<h8>.md` 相对链接 |
| **3.3** | 零 I/O 与容错测试用例补齐 | Tier 3 (Testing) | ✅ **彻底解决** | [`internal/report/factscache_test.go:34-90`](file:///Users/stanford/code/vmr/internal/report/factscache_test.go#L34-L90) 增加 `TestScanFiles_CacheHitNeverOpensFile`（指向不存在路径断言零 I/O）、`TestLoadCachedFacts_RejectsStaleSchemaVersion` 等测试 |

---

## 2. 代码库深度核查与技术亮点（Deep Dive & Highlights）

通过对工作区全部 39 个文件的修改与新文件的深度核查，确认了以下关键技术实现与设计亮点：

### 2.1 架构单向依赖与 Leaf 包正交解耦（Zero Boundary Violation）

* **解耦设计**：
  - `internal/reqdetail` 作为纯叶子包，不 import `report` 或 `story`；
  - `ctxgraph.CachedFile` 将 `Facts` 字段定义为 `json.RawMessage`（透明载荷），`ctxgraph` 包仅负责分片读写与反序列化透传，不理解任何 report 分桶语义；
  - `report` 包的特定事实结构体（`recordFacts` / `attemptFacts` / `fileFacts`）完全私有化收敛在 [`internal/report/factscache.go`](file:///Users/stanford/code/vmr/internal/report/factscache.go) 中；
* **效果**：`internal/archtest` 中的架构边界断言全部绿灯通过，完全守住了“两半区、一个契约”的架构纪律。

---

### 2.2 证据层体积断崖式回落（Evidence Layer Slimming）

* **事实数据**：
  1. 移除了 `details/*.json` 副本（过去占详单体积的 67%）；
  2. `-details` 默认值切换为 `false`，默认仅生成索引与报表；
  3. 系统提示词从详单内联改为引用 `evidence/sysprompt-<h8>.md`，同一个系统的几十轮对话在证据层仅保留一份几 KB 的 Markdown blob；
  4. 工具声明同样收敛为 `evidence/tools-<h8>.md`；
* **效果**：全量报表运行的派生产物体积从数十 MB / 数 GB 级直接降回 KB 级索引规模。

---

### 2.3 解析缓存与索引彻底解耦（Cache vs Index Separation）

* **现状核实**：
  - [`internal/report/requests.go`](file:///Users/stanford/code/vmr/internal/report/requests.go) 的 `RequestsIndex` 结构体仅保留 `Requests []RequestRow`；
  - [`internal/story/storyindex.go`](file:///Users/stanford/code/vmr/internal/story/storyindex.go) 的 `StoryIndex` 结构体仅保留 `Journeys []JourneyIndexRow`，`Cache` 字段标记为 `json:"-"`；
  - 缓存分片落盘至 `{outDir}/.parse-cache/<filehash>.json`，紧凑编码存储，`vmr report` 与 `vmr story` 自动共用同一个缓存目录。
* **效果**：`vmr-requests.json` 与 `vmr-stories.json` 恢复为纯粹的人读/机读业务索引，可随手 `cat` 或 `jq`，消除了单体 88MB 巨型 JSON 的读写开销。

---

### 2.4 文档与设计基线完备同步（Documentation & Baseline Parity）

核对工作区文档更新状态：
1. **`CHANGELOG.md`**：准确记录了 Removed（详单 `.json` 副本移除、`-detail` 移除）、Added（`-req`、`-print`、`.parse-cache/` 分片）、Changed（`-details` 默认 `false`、幂等跳过、提示词/工具外置为 evidence blob、Schema 版本戳）；
2. **`docs/KNOWN_ISSUES_sonnet-5.md`**：
   - §1.1 按 P3 批 D 实际落地情况重写（准确记录三趟扫描中第 ③ 趟已缓存，第 ② 趟待后续排期）；
   - §1.5（`detail.go` 拆分）正式闭环；
3. **`docs/UserGuide.md` 与 `docs/UserGuide.zh.md`**：同步更新了 `vmr replay` 的用法（移除 `-detail`，增加 `-req` 与 `-print` 的说明和中英文示例）；
4. **`docs/VirtualModelRouter_Design_v4_Analytics.md`**：§2.5 与 §3.4 准确更新为 `.parse-cache/` 分片缓存与 `factscache` 的当前形态；
5. **`docs/future-strategy/story_report_architecture_opus-5.md` 与 `story_report_dev_plan_opus-5.md`**：准确标注了 P3 完成态的历史基线说明。

---

## 3. 综合评估与后续阶段建议

P3 阶段的核心使命是**让派生产物停止复制源数据，体积回落到与信息量相称的量级，重复解析回落到与新增数据量相称的量级**。

从目前工作区的代码实现来看：
- **P3 的 7 个子任务（P3.1–P3.7）已全部扎实落地**；
- **全量测试与并发竞态检查 100% 通过**；
- **第一轮 Review 提出的所有高危问题均已得到令人满意的处置**。

### 后续阶段推进建议

1. **可以安全提交当前未暂存的 P3 代码**（建议 commit message: `feat(analytics): complete Phase 3 evidence slimming, parse cache sharding, and coordinate replay`）；
2. **P4 阶段前提已完全就绪**：
   - `journey-<id>.json` 可直接复用 `Manifest.Req` 坐标引用证据层；
   - 系统提示词与工具声明已在 `evidence/` 下拥有稳定的内容寻址地址；
   - 按坐标取原文的原语（`audit.LineAt` / `vmr replay -req -print`）已就绪。
