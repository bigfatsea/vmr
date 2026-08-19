// Ver 2026-08-19 23:30, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P2 执行计划（ActionPlan）深度评审与事实核查

**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**

## 0. 评审概述与裁决结论

本文档对 `docs/future-strategy/story_report_p2_action_plan_sonnet-5.md`（以下简称 **P2 ActionPlan**）进行了逐行事实核查与架构推演。核查基准包括：
1. **架构与规划文档**：`docs/future-strategy/story_report_architecture_opus-5.md` 与 `docs/future-strategy/story_report_dev_plan_opus-5.md`；
2. **代码库现实状态**：当前 Git HEAD（commit `30c5159`，P1 落地完成态）下的 `internal/ctxgraph/`、`internal/report/`、`internal/story/`、`internal/audit/`、`internal/archtest/` 源码。

### 核心评审结论

P2 ActionPlan 的大方向（坐标定义与发布、解析缓存规范化、详单做减法并下沉为纯函数、确定性命名）完全符合 Opus 架构文档与 DevPlan 的设计意图。**但在执行级细节上存在数处关键事实冲突、自相矛盾的操作指令以及设计留白**。如果不加修正直接执行，会导致编译失败、二阶段日志读取（`BlobIndex.FetchAll` / `FetchRecords`）大面积 `ENOENT` 崩溃，或者导致 `report` 与 `story` 对同一条错误记录生成互不匹配的文件名。

所有问题已按 **严重程度 + 长期 ROI** 分级排列。严重问题和高 ROI 改进全部收录于**第一梯队**。

---

## 1. 事实核查表（Codebase vs ActionPlan）

| 核查项 | ActionPlan 声称 | 源码实际情况（HEAD: 30c5159） | 核查判定 | 影响与后果 |
| --- | --- | --- | --- | --- |
| **`RequestRow` 构造位置** | `rows.go:506` 附近（§2.2 第 5 点） | `rows.go:506` 仅是 `StickyEffect` 的结构体注释；实际构造在 `internal/report/recextract.go:21` 的 `buildRequestRow` | ❌ **事实错误** | 按文档查找构造点会扑空，遗漏 `recextract.go` 的装配链 |
| **`BuildManifest` 与 `m.Path`** | `Path: CanonicalPath(path)`（§2.2 第 1 点 & §2.3 第 2 步） | `BlobIndex.FetchAll` (`blobindex.go:97`) 与 `FetchRecords` (`records.go:82`) 直接把 `m.Path` 传给 `audit.OpenLogFile` | ❌ **破坏性冲突** | 剥离目录和扩展名会导致所有跨文件提取逻辑抛出 `ENOENT` |
| **`Manifest.Req()` 方法定义** | `return m.Path + ":" + strconv.Itoa(m.Line)`（§2.2） | 若 `m.Path` 保持物理 I/O 路径，该拼法会输出带目录和 `.zst` 的非规范化字符串 | ❌ **契约冲突** | 违反坐标规范化定义（`basename:line`） |
| **`detailFileName` 的 `ErrorClass`** | 称 `FileName` 只需 `Manifest` 即可算出（§3.2） | `detail.go:355-360` 将 `errorClass(rec)` 追加到 `outcome` 后，而 `ctxgraph.Manifest` **没有** `ErrorClass` | ❌ **设计盲区** | `report`（读 rec）与 `story`（读 Manifest）对失败记录算出的文件名不一致 |
| **`FileCache` 跨运行复用** | 仅将 `FileCache` map key 改为 `CanonicalPath` | `CachedFile.Manifests` 中序列化的 `Path` 在跨目录/绝对路径调用时未重绑定 | ⚠️ **潜在隐患** | 从旧缓存载入的 Manifest 在新路径下读取正文会失败 |
| **P1 状态与基线测试** | P1 已提交 `30c5159`，测试全绿（§0, §1） | 确实已提交 `30c5159`，但当前工作区有未完成的 `reqcoord` 草稿导致编译失败 | ⚠️ **状态提示** | 需清理/重构草稿代码以恢复清洁测试基线 |

---

## 2. 第一梯队：严重缺陷与高 ROI 改进（Tier 1）

### 2.1 [严重缺陷] `Manifest.Path` 规范化直接破坏 `BlobIndex.FetchAll` 与 `FetchRecords`

* **严重级别**：阻断级（Blocker）
* **涉及章节**：ActionPlan §2.1（Line 69-71）、§2.2（Line 117）、§2.3（Line 158）

#### 机制剖析
ActionPlan 在 §2.1 中明确写道：
> *“关键约束，落地时不能踩：`audit.OpenLogFile(path)`（`scan.go`/`WriteDetails` 都在用）需要能实际打开文件的路径（含目录、含 `.zst` 后缀）。规范化只能作用于作为坐标/key 用的字符串，绝不能替换掉用来做文件 I/O 的那个 `path` 变量——这是本任务最容易踩的坑。”*

然而在 §2.2 目标设计第 1 点与 §2.3 具体步骤第 2 步中，ActionPlan 却直接给出了相反的指令：
> *“`manifest.go`：`BuildManifest` 改 `Path: CanonicalPath(path)`。”*

一旦 `Manifest.Path` 被覆写为 `CanonicalPath(path)`（例如 `vmr-audit-2026-07-28.jsonl`）：
1. `BlobIndex.refs` 记录的路径变为纯文件名；
2. `story` 运行生成 Journey 报告时，`FetchRecords`（`records.go:82`）和 `BlobIndex.FetchAll`（`blobindex.go:97`）调用 `audit.OpenLogFile(loc.Path)`；
3. 当用户通过相对路径（如 `logs/vmr-audit-...jsonl.zst`）运行 `vmr story` 时，`OpenLogFile` 将在当前进程工作目录下寻找文件，直接抛出 `os.ErrNotExist` 错误，导致所有需要回溯原始消息/正文的叙事渲染彻底崩溃！

#### 修复方案
必须清晰划分 **“物理 I/O 路径”** 与 **“逻辑身份标识”** 的职责边界：
1. `Manifest.Path` **必须保留可解析的物理路径**（或者由 `ScanCached` 在内存中维护规范名到物理路径的映射）；
2. `Manifest` 上的坐标获取统一通过方法计算：
   ```go
   // internal/ctxgraph/reqcoord.go
   func (m *Manifest) Req() string {
       return ReqCoord(m.Path, m.Line)
   }
   ```
   `ReqCoord` 内部负责调用 `CanonicalPath(path)`，确保无论 `m.Path` 是绝对路径、相对路径还是带 `.zst`，输出的 `Req` 永远是统一的 `<basename>:<line>`。

---

### 2.2 [关键设计盲区] `FileName` 纯 `Manifest` 计算与 `detailFileName` 中 `ErrorClass` 的冲突

* **严重级别**：高（High）
* **涉及章节**：ActionPlan §3.1（Line 209-210）、§3.2（Line 250-254）

#### 机制剖析
Opus 架构文档 §7.3(c) 与 DevPlan P2.3 规定：**详单文件名必须可以只凭 `ctxgraph.Manifest` 算出，无需读取记录正文**。这是使得中观叙事层（`story`）和宏观索引层能够在文件生成前预先渲染链接的核心前提。

然而，在当前的 `internal/report/detail.go:355-360` 中：
```go
outcome := rec.Outcome
if outcome == "error" {
    if cls := errorClass(rec); cls != "" {
        outcome += "-" + cls
    }
}
```
`errorClass(rec)` 是通过遍历 `rec.Attempts` 的结构化错误信息提炼出的（如 `bad_gateway`、`rate_limited`）。
但查看 `internal/ctxgraph/manifest.go`：
`Manifest` 结构体仅包含 `Outcome string`（即 `rec.Outcome`，通常为 `"ok"`、`"error"`、`"canceled"`），**根本没有存储 `ErrorClass`**！

如果 `reqdetail.FileName(rec)` 试图读取 `rec.Attempts` 追加 `errorClass`，而 `reqdetail.FileNameFromManifest(m)` 无法得知 `errorClass`，两者计算出的文件名将产生静默分裂：
- `report` 生成的文件：`20260728-000544.000_model_real_error-bad_gateway_a1b2c3d4.md`
- `story` 渲染的链接：`20260728-000544.000_model_real_error_a1b2c3d4.md`
结果就是：**所有失败请求的下钻链接全部 404**。

#### 修复方案（第一性原理裁决）
不需要把 `ErrorClass` 塞进 `Manifest`。
在引入了 `hash8`（`ReqHash8(req)`）之后，**每个请求的文件名已经天然全局唯一**。文件名中的 `ts_virtual_real_outcome` 纯粹是提供人类视觉可读性与文件排序，不再承担消歧职责。
- 将文件名中的 `outcome` 严格规范化为 `sanitizeName(rec.Outcome)` / `sanitizeName(m.Outcome)`（只取 `"ok"`、`"error"`、`"canceled"` 或 `""`）；
- 命名公式统一定为：
  `{ts}_{virtual}_{real}_{outcome}_{hash8}.md`
  其中：
  - `ts`: `rec.TS` 或 `m.TS`（自带时区偏移，格式 `20060102-150405.000`）；
  - `virtual`: `rec.Model` 或 `m.Model`（空则为 `rejected`）；
  - `real`: `realModel(rec)` 或 `m.Endpoint` 解析出的 model；
  - `outcome`: `rec.Outcome` 或 `m.Outcome`；
  - `hash8`: `ReqHash8(ReqCoord(path, line))`。
- 这样，`FileNameFromRecord` 与 `FileNameFromManifest` 得到的 5 个元数据字段**完全同源、逐字节一致**。

---

### 2.3 [架构设计漏洞] 跨工作目录运行加载 `FileCache` 导致的历史路径失效

* **严重级别**：高（High）
* **涉及章节**：ActionPlan §2.2（Line 118-120）

#### 机制剖析
ActionPlan 将 `FileCache.Files` 的 key 改为了 `CanonicalPath(path)`，解决了同一日志在同一次扫描中因路径写法不同导致的缓存重复问题。
但是忽略了 **跨持久化运行（Cross-invocation Persistence）** 的场景：
1. 某次运行 `vmr report -o reports/ logs/2026-07-28.jsonl.zst`，缓存序列化进 `vmr-requests.json`，其中 `CachedFile.Manifests` 内每个 `m.Path` 记录为 `logs/2026-07-28.jsonl.zst`；
2. 随后用户在其他目录下或通过绝对路径执行 `vmr story -o reports/ /var/log/vmr/2026-07-28.jsonl.zst`；
3. `ScanCached` 检查文件 sha256 哈希命中，直接复用内存中的 `CachedFile`；
4. 但此时复用的 `m.Path` 依然是旧路径 `logs/...`，当 `story` 发起 `FetchRecords` 时，尝试打开 `logs/...` 失败报错。

#### 修复方案
在 `internal/ctxgraph/cache.go` 的 `ScanCached` 中，当命中缓存并重用 `CachedFile` 时，必须执行一次**轻量级路径重绑定（Path Rebinding）**：
```go
if cached, ok := prior.Files[key]; ok && cached.Hash == hash && !hasNilManifest(cached.Manifests) {
    // 确保内存中 manifest 的 I/O 路径始终对齐本次实际扫描的物理路径
    for _, m := range cached.Manifests {
        m.Path = path
    }
    return fileCacheResult{path: key, entry: cached}
}
```
这样既享受了跳过 JSON 解析的极速缓存收益，又保证了文件 I/O 路径的绝对正确。

---

### 2.4 [高 ROI 架构改进] 彻底闭合 `PrevTurnLink` 接口缺口，终结执行层犹豫

* **严重级别**：高 ROI（High ROI / Architecture Simplification）
* **涉及章节**：ActionPlan §3.2（Line 270-280）、§4.2（Line 353-355）

#### 现状与问题
ActionPlan 在 §3.2 与 §4.2 中指出：`PrevTurnLink` 需要上一轮的详单文件名，但 `Render` 只拿到了 `prev *ctxgraph.Manifest`，没有上一轮的完整 `*audit.Record`。ActionPlan 将其留作为待定项：“选哪个留给实现时定（方向 a vs 方向 b）”。

#### 裁决与终极简化
结合第 2.2 节的成果（文件名完全可由 `Manifest` 独立计算），**方向 (b)（试图为调用方传递上一轮 `*audit.Record`）应当被直接否决**。

`reqdetail` 应提供统一的文件名计算函数：
```go
package reqdetail

// FileName computes the deterministic filename from raw coordinate and metadata parts.
func FileName(ts time.Time, virtualModel, endpoint, outcome, req string) string

// FileNameFromManifest computes the filename directly from a ctxgraph.Manifest.
func FileNameFromManifest(m *ctxgraph.Manifest) string {
    if m == nil {
        return ""
    }
    _, _, realMod := core.SplitEndpointLabel(m.Endpoint)
    if realMod == "" {
        realMod = "none"
    }
    virtualMod := m.Model
    if virtualMod == "" {
        virtualMod = "(rejected)"
    }
    return FileName(m.TS, virtualMod, realMod, m.Outcome, m.Req())
}

// FileNameFromRecord computes the filename from an audit.Record and its physical location.
func FileNameFromRecord(rec *audit.Record, path string, line int) string {
    req := ctxgraph.ReqCoord(path, line)
    // ... 对齐提取逻辑 ...
}
```
在 `reqdetail.Render` 内部：
```go
if prev != nil {
    prevFile := FileNameFromManifest(prev)
    prevTS := prev.TS.Format("15:04:05.000") // 遵循记录自带时区
    b.WriteString(t.PrevTurnLink(prevTS, prevFile))
}
```
**收益**：
1. 接口完全闭合，签名无需扩展大对象；
2. `Render` 彻底成为纯函数 `func Render(rec *audit.Record, path string, line int, m, prev *ctxgraph.Manifest, prof taskseg.Profile, lang i18n.Lang) string`；
3. 杜绝了为了取上一轮 record 而在扫描期维护复杂对象图的负担。

---

### 2.5 [职责越界与分层倒置] `session.go` 反向依赖 `reqdetail` 的设计坏味道

* **严重级别**：高 ROI（Architecture Cleanliness）
* **涉及章节**：ActionPlan §3.1（Line 212-224）、§3.2（Line 282-288）

#### 机制剖析
ActionPlan 提议将 `session.go` 内部的 `RoleTokens`/`ToolCalls`/`toolsSig` 等提取逻辑搬迁至新包 `internal/reqdetail`，然后让 `session.go` 的 `collect()` 改为调用 `reqdetail.XxxFrom(...)`。

这一设计存在明显的分层倒置：
- `internal/reqdetail` 的职责是 **微观详单的 Markdown 渲染器**；
- `internal/report/session.go` 的职责是 **宏观/中观的会话与任务切分分析器**。
让核心分析模块（`session.go`）反向依赖一个专门用来渲染单请求 Markdown 页面的模块（`reqdetail`），在概念模型上是不合理的。

#### 修复方案
1. `chatmsg` 本身已经是消息分析的基础叶子包（包含 `Messages()`、`ExtractUsage()`、`ToolCalls()` 等）；
2. `roleTokens(body)`、`toolsSig` 本质上是对 `chatmsg.Messages` 的通用计算：
   - 提取函数可以直接放在 `internal/chatmsg` 或 `internal/taskseg`（本身就是共享分析叶子包）；
   - 或者 `reqdetail` 与 `session.go` 各自调用 `chatmsg` 导出的底层分析函数。
3. `internal/reqdetail` 保持纯粹的渲染与命名定位，只由 `report` 和 `story` 单向 import，不被核心分析层反向依赖。

---

## 3. 第二梯队：中度疏漏与边界测试（Tier 2）

### 3.1 [事实修正] `RequestRow` 的装配链定位错误

* **涉及章节**：ActionPlan §2.2（Line 139-140）
* **事实核查**：
  ActionPlan 描述：“在 `RequestRows`（`rows.go:506` 附近，构造每一行 `RequestRow` 的地方）填这个字段：`Req: ctxgraph.ReqCoord(r.Path, r.Line)`”。
  查阅代码：
  - `internal/report/rows.go:502-503` 是 `RequestRow` 的结构体定义；
  - `internal/report/rows.go:506` 是 `StickyEffect` 的字段注释；
  - 真正遍历并构建 `RequestRow` 的是 `internal/report/recextract.go:21` 中的 `buildRequestRow(rc *rec2)`。
* **修正操作**：
  1. `rows.go`：在 `RequestRow` 中增加 `Req string \`json:"req,omitempty"\``；
  2. `recextract.go`：在 `buildRec2` 中保留 `rc.path` 与 `rc.line`，在 `buildRequestRow` 中赋值：
     ```go
     rr.Req = ctxgraph.ReqCoord(rc.path, rc.line)
     ```

---

### 3.2 [边界场景] 非聊天/被拒绝请求（`m == nil`）的防御性渲染

* **涉及章节**：ActionPlan §3.2（Line 240-246）
* **场景说明**：
  当审计日志记录为畸形 JSON、认证失败（401 Unauthorized）、缺少 model 字段等情况时，`ctxgraph.BuildManifest` 会返回 `ok = false`（即 `m = nil`）。
  `vmr report` 在全量扫描时仍然会为这类请求生成 `details/*.md`。
* **防护要求**：
  1. 当 `m == nil` 时，`prev` 必定为 `nil`，`DeltaStart` 为 0；
  2. `FileNameFromRecord` 必须优雅处理 `rec.Model == ""`（展示为 `(rejected)`）、`rec.Attempts` 为空（`lastEndpoint` 为 `"-"`、`realModel` 为 `"none"`）；
  3. `reqdetail_test.go` 中必须包含专门针对 `m == nil` 记录的渲染测试，确保零 panic。

---

### 3.3 [测试适配] 既有测试用例中 `cache.Files` 索引方式的同步修正

* **涉及章节**：ActionPlan §2.3（Line 164-166）
* **问题定位**：
  在 `internal/report/build_cached_test.go:97` 中，存在硬编码的断言：
  ```go
  if cache2.Files[path].Hash != cache1.Files[path].Hash { ... }
  ```
  当 P2.1 将 `cache.Files` 的 map key 改为 `CanonicalPath(path)` 后，由于 `path` 是完整的临时目录路径（如 `/tmp/TestBuildCached.../audit.jsonl`），直接用 `path` 索引会导致读取到空 entry 而引发测试失败。
* **操作提示**：
  在 P2.1 执行步骤中明确加入对 `build_cached_test.go` 及相关测试的断言更新：
  ```go
  key := ctxgraph.CanonicalPath(path)
  if cache2.Files[key].Hash != cache1.Files[key].Hash { ... }
  ```

---

## 4. 第三梯队：低风险清理与代码健康度（Tier 3）

### 4.1 清理 `DetailWriter` 与 `session.go` 中废弃的批次计数器与锁

* **说明**：旧逻辑通过 `used map[string]int` 维护同毫秒时间戳的 `-1`、`-2` 后缀。确定性命名（引入 `hash8`）后，坐标天然全局唯一。
* **建议**：
  - 从 `DetailWriter` 结构体中彻底移除 `used map[string]int` 和 `sync.Mutex`；
  - 从 `session.go` 的 `assignNames` 中移除 `detailFileNameFromInfo(r, used)` 逻辑，直接调用 `reqdetail.FileNameFromRecord` 或 `FileNameFromManifest`。

### 4.2 控制新叶子包 `internal/reqdetail` 的文件体积与架构测试预算

* **说明**：`internal/archtest/file_sizes_test.go` 对未豁免文件设有 700 行全局限制。
* **建议**：
  在新建 `internal/reqdetail` 包时，不要将 1048 行的 `detail.go` 整体塞入单一文件，建议拆分为：
  - `render.go`：核心 Markdown 渲染逻辑；
  - `filename.go`：确定性命名计算与辅助函数；
  - `writer.go`：`DetailWriter` 工作池与文件写入调度。
  拆分后各文件均在 300~400 行以内，天然无需在 `file_sizes_test.go` 中申请预算豁免，同时可以顺手注销旧 `internal/report/detail.go` 的 1150 行豁免配置。

---

## 5. P2 ActionPlan 关键修订对比一览

| 维度 | 原 ActionPlan 设计 | 评审修正后设计 |
| --- | --- | --- |
| **`Manifest.Path`** | 改为 `CanonicalPath(path)`（截断目录） | **保持物理路径**，`Req()` 方法内调用 `ReqCoord` |
| **`FileName` 命名公式** | 依赖 `rec.Attempts` 中的 `errorClass` | **剔除 `errorClass`**，严格采用 `{ts}_{virtual}_{real}_{outcome}_{hash8}.md` |
| **`PrevTurnLink` 接口** | 留待实现时二选一 | **直接采用 `FileNameFromManifest(prev)`**，确定性闭环 |
| **函数搬迁与分层** | `session.go` 反向依赖 `reqdetail` | 基础消息分析下沉到 `chatmsg`，`reqdetail` 保持纯叶子渲染器 |
| **缓存多目录复用** | 仅改 key | 命中缓存时**重绑定 `Manifest.Path` 为当前物理路径** |
| **`RequestRow` 注入点** | `rows.go:506` | `recextract.go:21`（`buildRequestRow`） |

---

## 6. 结语

P2 是整个日志分析体系从“基于运行批次的偶发展示”跃迁到“基于全局坐标的确定性寻址”的基石阶段。修正上述第一梯队的物理路径冲突与命名分歧后，P2 的执行计划将具备极高的严密性与确定性，能为后续 P3（证据层瘦身）、P4（中观机读层）以及 P5（人读层瘦身）提供坚固且自洽的底层支撑。
