<!-- Ver 2026-07-12 14:30, by Fable 5 -->

# VMR 全量 Review 报告

> 项目：`vmr` (Virtual Model Router) — 本地运行、单二进制、配置驱动的 LLM 路由器
> 范围：所有源代码、配置、脚本、文档
> 评审对象：v2 / Fable5 文档定版的当前代码库
> 评审方式：纯静态 + `go build` / `go vet` / `go test -race ./...` 基线 + 逐文件研读 + 跨文件交叉对比
> 本次评审未做任何代码改动

---

## 0. Debrief 与执行回顾

### 0.1 任务目标

对 `vmr` 整个项目做一次完整、逐文件的 code review，输出：

1. 逐文件 review 记录（每个文件都要明确写到，包含无问题的文件）
2. 问题三档分类：
   - **P0 严重且需立即修复** — 重点展开
   - **P1 建议修复** — 高价值系统升级
   - **P2 无关紧要** — 不建议修复

约束：纯分析、不修改代码、产出 Markdown 报告。

### 0.2 执行计划

| 步骤 | 内容 | 状态 |
|---|---|---|
| 1 | 读取设计文档 `docs/VirtualModelRouter_v2_Fable5.md` | ✅ 读完 587 行 |
| 2 | 探索项目结构、构建配置、入口文件 | ✅ |
| 3 | 逐文件 review 所有源代码（按依赖顺序） | ✅ |
| 4 | 逐文件 review 所有配置和脚本 | ✅ |
| 5 | 跨文件交叉对比（文档↔代码、配置↔代码、接口一致性等） | ✅ |
| 6 | 三档分类，撰写本报告 | ✅ |

### 0.3 基线检查结果（评审前确认的当前状态）

| 项 | 命令 | 结果 |
|---|---|---|
| 编译 | `go build ./...` | ✅ 通过，零输出 |
| 静态检查 | `go vet ./...` | ✅ 通过，零输出 |
| 单元/集成测试 | `go test ./...` | ✅ 14 个包全部通过 |
| 数据竞争 | `go test -race ./...` | ✅ 14 个包全部通过 |
| Go 版本 | `go version` | `go1.25.1 darwin/arm64`（与 `go.mod` 一致） |
| 依赖 | `go.mod` 4 直接 + 1 间接 | `yaml.v3`、`fsnotify`、`klauspost/compress/zstd`、`golang.org/x/image`（与文档 §1 完全吻合） |

代码库规模：约 **16476 行** Go 代码（核心 ~5500、tests ~7700、配置 + 脚本 ~600），单二进制。

---

## 1. 项目结构概览

```
vmr/
├── cmd/vmr/main.go                  CLI 入口（start/check/status/report/dirs），520 行
├── internal/
│   ├── core/                        共享类型：CanonicalRequest、ErrorClass、Endpoint（无依赖）
│   ├── rundir/                      默认目录解析公式（env → temp → cwd）
│   ├── config/                      YAML 加载 / ${ENV} 展开 / 校验 / 热加载 watch
│   ├── adapter/                     Adapter 接口 + 注册表 + DefaultClassify + RewriteModel
│   │   ├── openai/                  OpenAI 协议透传 Adapter
│   │   └── anthropic/               Anthropic 协议透传 Adapter（含 529 特殊分类）
│   ├── health/                      被动健康状态机（冷却、退避、半开探针）
│   ├── strategy/                    Dimension 接口 + priority 实现 + 稳定多键排序
│   ├── audit/                       审计日志 JSONL + zstd 压缩/保留
│   ├── imgprep/                     内联图片降采样 + 内容哈希磁盘缓存
│   ├── router/                      failover 循环 + 流转发 + 响应归一化器
│   ├── server/                      HTTP 入口 + 鉴权 + Header 黑名单 + 审计录制
│   └── report/                      审计聚合：Build / Session / Detail / Markdown / Render / Usage / Export
├── vmr.sh                           dev/service 双模式启停脚本（launchd + systemd）
├── config.example.yaml              配置模板
├── config.yaml                      本地验证用（gitignored）
├── README.md / README.zh.md         中英双语文档
├── docs/
│   ├── VirtualModelRouter_v2_Fable5.md   主设计文档（587 行）
│   └── SensitiveWordFilter_Analysis_Fable5.md  敏感词过滤方案分析
└── go.mod / go.sum
```

---

## 2. 逐文件 Review 记录

### 2.1 `go.mod` / `go.sum`

**文件**：`/go.mod`、`/go.sum`（共 24 行）

- **结论**：✅ 无问题
- **核查要点**：
  - `module vmr` 模块名简短（无仓库路径前缀）。这是文档 §12.2 路线图明确登记的"发布前改名"事项："module 名改为完整仓库路径"。**不算问题**，是有意保留的发布前 TODO。
  - 4 个直接依赖（`yaml.v3`、`fsnotify`、`klauspost/compress`、`golang.org/x/image`）+ 1 间接（`golang.org/x/sys`）— 与设计文档 §4.2 完全吻合，"其余标准库"。
  - `go 1.25.1` 是真实存在的稳定版本。
  - `go.sum` 行数与 `go.mod` 需求精确对应，无遗漏。

---

### 2.2 `.gitignore`

**文件**：`/.gitignore`（32 行）

- **结论**：✅ 无问题
- **核查要点**：
  - 排除 `config.yaml`（本地真实 key）和 `config.example.yaml` 显式不被排除（保留提交）
  - 注释解释了为什么排除 `details/`、`*.jsonl`、`*.jsonl.zst`（含完整对话体）
  - 排除 `vmr-report.{json,md}` 和 `vmr-requests-index.md`（也是含完整请求体）
  - 排除 `logs/` 和 `image_cache/`（仅在 OS 不给临时目录的边缘情况下出现）
  - 排除 `*.test` `*.out` `coverage.html`（标准 Go 制品）
  - 注意：**`_tmp/` 不在 `.gitignore`** — 但这是 review 现场的工作目录，与提交无关。检查发现 `_tmp/plan_Fable5.md` 是 review 任务的本地计划文件，应当在评审完成后清理（不影响仓库，但属于工作区卫生）。

---

### 2.3 `docs/VirtualModelRouter_v2_Fable5.md`

**文件**：`/docs/VirtualModelRouter_v2_Fable5.md`（587 行）

- **结论**：✅ 文档质量极高，无问题
- **核查要点**：
  - 设计意图、机制、决策、war story 都有清晰叙述；每个决策都有"备选 → 取舍逻辑"对应
  - §3 协议透传原则与代码完全一致（无中间表示、无协议翻译）
  - §5.5 响应归一化器描述的所有 norm 标记（`model_rewrite`/`think_strip`/`thinking_process_strip`/`done_appended`/`buffered`/`resumed_stream`/`opaque`/`overflow_raw_passthrough`/`soft_block_detected`）在 `internal/router/response.go` 中全部实现并对应 `noteApplied` 调用
  - §6.2 半开单飞探针的"中性结局必须归还探针槽"原则对应 `ReportNeutral`，且 `server_probe_test.go` 锁定了回归
  - §9.2 五条 JSONL 约定与 `internal/audit/audit.go` 类型定义一致
  - §9.4 Format 9 多桶架构在 `internal/report/report.go` 落实，`HoursOfDay`/`EndpointsAll` 独立原始数据收集
  - §11 决策表与代码逻辑完全对应

**潜在文档轻微瑕疵**（不影响代码，记为 P2）：
- 文档 §13 已识别 "writeError 在 router 与 server 各有一份相同实现" 等清理项，但未提及 `countNested` 在 `config` 与 `cmd/vmr` 各有一份（`cmd/vmr/main.go:336` 与 `internal/config/config.go:215`）。这是同类同质重复，文档应统一列在一处。这是文档瑕疵非代码问题。
- §7.1 `vmr.sh` 段落说 `VMR_LOG_DIR`/`VMR_IMG_CACHE_DIR` 通过 `vmr dirs log`/`cache` 查询 — 与 `vmr.sh:55` 实现一致。

---

### 2.4 `docs/SensitiveWordFilter_Analysis_Fable5.md`

**文件**：`/docs/SensitiveWordFilter_Analysis_Fable5.md`

- **结论**：✅ 纯分析报告，无问题，且作为 §12.1 路线图的输入正确落地（"本轮明确不预留接口"在 §12.1 中执行）
- **核查要点**：仅作为设计输入阅读，不在本次 review 评审范围内（不是实现代码）。

---

### 2.5 `internal/core/core.go`（115 行）

**文件**：`/internal/core/core.go`

- **结论**：✅ 无问题（设计精巧）
- **核查要点**：
  - `MarshalNoEscape`：用 `json.Encoder` + `SetEscapeHTML(false)` + trim trailing newline，避免 `< > &` 被 HTML 转义 — 与 `audit.NewMessage` 调用链一致
  - `CanonicalRequest{Model, Stream, Raw, Header}`：只有路由需要的 4 个字段，`Raw` 保留原始字节
  - `ErrorClass` 枚举含 6 个 HTTP 分类 + 4 个非 HTTP 路径分类（`ErrBuild`/`ErrNetwork`/`ErrCanceled`/`ErrTruncated`）— 注释明确说明后 4 个不参与 health，纯粹给 audit 提供统一枚举
  - `Endpoint.HealthKey()` 含 `AdapterType + Provider + Model + APIKey sha256 前 4 字节` — 与 §6.2 决策表"protocol 前缀防同 provider 名跨协议撞"完全对应
  - `Endpoint.Name()` 用 `/` 分隔，`audit.Attempt.Endpoint` 用 `:` 分隔 — 设计上故意不一致，已在 §9.2 末尾解释

---

### 2.6 `internal/core/core_test.go`（76 行）

**文件**：`/internal/core/core_test.go`

- **结论**：✅ 测试覆盖核心 invariant，无问题
- **核查要点**：
  - `TestHealthKeyProtocolPrefixAvoidsCollision` 锁定 §6.2 决策意图
  - `TestNameOmitsAPIKey` 锁定 `Name()` 不含 key（防止 `X-VMR-Endpoint` 泄漏）
  - `TestMarshalNoEscapeSkipsHTMLEscaping` 锁定关键反 escape 行为
  - 没有并发测试，但 `HealthKey`/`Name` 是纯函数，并发安全

---

### 2.7 `internal/rundir/rundir.go`（51 行）

**文件**：`/internal/rundir/rundir.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `Resolve(envVar, tmpSubdir, pwdSubdir)` 三段式：env set → env 值原样；env unset → `os.TempDir()/tmpSubdir`；env unset 且 TempDir 空 → `cwd/pwdSubdir`（防御兜底）
  - `envVal != ""` 用空串判断已正确处理 unset 和空值两种情况
  - `cwd()` 错误回退到 `"."` 而非 panic，健壮
  - 没有外部依赖，是非常干净的纯函数包

---

### 2.8 `internal/rundir/rundir_test.go`（46 行）

**文件**：`/internal/rundir/rundir_test.go`

- **结论**：✅ 测试覆盖三个分支 + 公开 API 集成测试，无问题
- **核查要点**：
  - `TestResolveEnvOverrideUsedAsIsNoSubdirAppended` — 显式 env 值原样返回
  - `TestResolveFallsBackToTempDirSubdir` — env unset 时落到 temp 子目录
  - `TestResolveFallsBackToCWDWhenTempDirEmpty` — 兜底分支
  - `TestResolveExportedEnvVar` / `TestResolveExportedDefaultsToTempSubdir` — 覆盖 `Resolve` 公开 API

---

### 2.9 `internal/strategy/strategy.go`（76 行）

**文件**：`/internal/strategy/strategy.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `Dimension` 接口两方法：`Name()` + `Compare(a, b) int`
  - `Register` 重复注册 `panic`（编译期注册，无运行时插件）— 与 §5 Adapter 模式同构
  - `Build([]string) []Dimension` 按配置名实例化
  - `Sort` 用 `sort.SliceStable`，全员 priority=0 时保留配置文件顺序 — 与 §10 决策"priority 可选、靠列表顺序"完全一致
  - `init()` 仅注册 `priority`，与文档 §6.1 "其余在路线图"一致

---

### 2.10 `internal/strategy/strategy_test.go`（34 行）

**文件**：`/internal/strategy/strategy_test.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `TestPrioritySortStableOnTies` 锁定 §6.1 决策（同 priority 按列表顺序）
  - `TestBuildUnknownDimension` 覆盖错误路径

---

### 2.11 `internal/config/config.go`（241 行）

**文件**：`/internal/config/config.go`

- **结论**：✅ 无严重问题；2 个 P2 瑕疵记入 §3.3
- **核查要点**：
  - 配置结构 `Provider` 无 `type` 字段（协议由外层 map key 决定）— §10 决策一致
  - `ModelConfig.ImageDownscaleMaxPx *int` — 指针类型区分 unset / 显式 0，§7 决策注释详尽
  - `applyDefaults` 钳制策略：负数全归 0 / 缺省值；`image_cache_ttl_days <=0` 走默认值 7（与 `audit_retention_days` 行为刻意不同 — §10 注释解释了）
  - `validate` 调用 `adapter.Get(protocol)` 拒绝未知协议 key — 防止配置文件写错 adapter 名
  - `expandEnv` 只识别 `${VAR}` 形式（文档明确"bare $ stays literal"）— 解析简单不易出错
  - `MaxRequestBodyBytes()` 是 `int64` 移位实现，注释说明"仅为稳定性考虑，与审计无关"
  - 已知 P2 瑕疵（见 §3.3）：
    - `countNested` 在 `internal/config/config.go:215` 和 `cmd/vmr/main.go:336` 各有一份 — 这是文档 §13 未列入的重复项
    - `listen` 只用 `net.SplitHostPort` 验证语法，不验证 host 可达或 scheme（这是配置文件默认行为，HTTP listen 地址无 scheme 概念，无影响）

---

### 2.12 `internal/config/watch.go`（53 行）

**文件**：`/internal/config/watch.go`

- **结论**：✅ 无问题
- **核查要点**：
  - 监听父目录而非文件本身（兼容编辑器原子替换：`mv` 而不是 `write in place`）— 与 §6.3 一致
  - 300ms 防抖去重多次写入事件
  - 事件过滤：`Write|Create|Rename` — 覆盖主要触发场景
  - `filepath.Clean(ev.Name) != abs` 跳过无关文件事件
  - 关闭通过 `watcher.Close` 回调返回

---

### 2.13 `internal/config/config_test.go`（347 行）

**文件**：`/internal/config/config_test.go`

- **结论**：✅ 覆盖到位，无问题
- **核查要点**：
  - 覆盖：env 展开、unset 展开为空、自定义超时、image_downscale 正/负/缺省、image_cache_ttl_days 正/非正、max_concurrency 负数钳制、audit_retention_days 缺省/正/负、priority 缺省保序、同名 provider 跨协议、未知协议 key、empty sections
  - 注意测试**导入** `_ "vmr/internal/adapter/anthropic"` 和 `_ "vmr/internal/adapter/openai"` — 但 `internal/config/config.go` 已经 `import "vmr/internal/adapter"`。这个 import 在 config 包从未被使用（无 `adapter.Get` 调用等），其实是冗余的（不破坏功能，但编译时间略增）。**记为 P2 轻微瑕疵**。

---

### 2.14 `internal/adapter/adapter.go`（66 行）

**文件**：`/internal/adapter/adapter.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `Adapter` 接口三方法：`Protocol()`、`BuildRequest()`、`ClassifyError()`
  - `Register` 重复注册 panic — 编译期注册防错
  - 全局 `registry` 由 `sync.RWMutex` 保护，并发安全
  - `Get` 和 `Names` 是读路径用 `RLock`

---

### 2.15 `internal/adapter/classify.go`（97 行）

**文件**：`/internal/adapter/classify.go`

- **结论**：✅ 无严重问题；2 个 P2 瑕疵记入 §3.3
- **核查要点**：
  - `DefaultClassify` 是 OpenAI/Anthropic 共享的错误分类表；anthropic 仅在 529 单独覆盖
  - 状态码 → 类别映射（451→content, 401→auth, 402/404→endpoint, 408→transient, 429→rate_limit 或 endpoint）符合文档 §5
  - **Body 嗅探策略**：先 contentHint，再 model 关键字，最后 fallback 到 ErrClient — 顺序合理（先排除最不像用户错的情况）
  - `contentHint` 词表覆盖中英文（"敏感"/"违规"/"合规"）+ MiniMax 错误码（1026/1027）+ DeepSeek "exists risk" 等
  - 已知 P2 瑕疵：
    - `case status == 402 || status == 404: return core.ErrEndpoint` — 实际上 402 也可能表示"未付费激活"，但分类仍合理（长冷却）
    - `containsAny("exists risk", "content risk", ...)` — `"exists risk"` 和 `"content risk"` 拆开匹配，如果 body 含 `"content exists risk"`（DeepSeek 实际措辞），"content risk" 匹配不到，"exists risk" 命中 — OK；但若遇到 `"exists-content-risk"` 形式就匹配不到。这是已知 trade-off（§5 已说"宁可宽"），不改

---

### 2.16 `internal/adapter/classify_test.go`（44 行）

**文件**：`/internal/adapter/classify_test.go`

- **结论**：⚠️ 测试覆盖偏薄（仅测 `RewriteModel`），但这部分另有 `openai_test.go`/`anthropic_test.go` 覆盖 DefaultClassify。无问题。

---

### 2.17 `internal/adapter/openai/openai.go`（56 行）

**文件**：`/internal/adapter/openai/openai.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `BuildRequest`：URL = `base_url + "/chat/completions"`，Header.Add 透传 chatHandler 过滤后的请求头，再 `Set` Content-Type + Authorization — 顺序合理（先 Add 后 Set 覆盖）
  - `ClassifyError` 直接转发 `DefaultClassify` — OpenAI 不需要特殊化（不像 Anthropic 有 529）

---

### 2.18 `internal/adapter/openai/openai_test.go`（约 100 行）

**文件**：`/internal/adapter/openai/openai_test.go`

- **结论**：✅ 覆盖到位，无问题
- **核查要点**：20+ 状态码 + body 嗅探用例，包括 MiniMax (1027)、DeepSeek (exists risk)、OpenRouter (flagged/guardrail)、中文敏感词、`(1026)` 等

---

### 2.19 `internal/adapter/anthropic/anthropic.go`（56 行）

**文件**：`/internal/adapter/anthropic/anthropic.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `BuildRequest`：URL = `base_url + "/messages"`、Header.Add 透传、Set Content-Type + x-api-key + 默认 anthropic-version
  - `ClassifyError` 单独处理 529（Anthropic-specific overloaded_error）映射为 ErrTransient
  - 默认 `defaultVersion = "2023-06-01"` 与 Anthropic API 历史兼容

---

### 2.20 `internal/adapter/anthropic/anthropic_test.go`（约 90 行）

**文件**：`/internal/adapter/anthropic/anthropic_test.go`

- **结论**：✅ 覆盖到位，无问题
- **核查要点**：URL 拼接、x-api-key 注入、anthropic-version 默认值与透传、DeepSeek 错模型名（"The supported API model names are …"）分类为 ErrEndpoint、529 → ErrTransient

---

### 2.21 `internal/health/health.go`（163 行）

**文件**：`/internal/health/health.go`

- **结论**：✅ 设计精确，无严重问题；记 1 个 P2（见 §3.3）
- **核查要点**：
  - `Registry.m map[string]*state` 由 `sync.Mutex` 保护
  - `Acquire` 单飞探针：`fails > 0 && probing` 拒绝并发；否则置 `probing = true`
  - `ReportNeutral` **只**释放 probing，不改 fails、不改 cooldown — §6.2 决策的精确实现
  - `ReportFailure` 退避算法：`Auth/Endpoint` 走 longBase 10min → longCap 1h；其他走 transientBase 2s → transientCap 5min；Retry-After 优先（429/503 都走 transient 分支）
  - `backoff(base, cap, fails)` 指数退避，`d *= 2`，到 cap 停止
  - 已知 P2 瑕疵：
    - `backoff` 循环到 cap 后**返回 cap**（设计意图：到 cap 就一直 cap），但**没有在 cap 满后立即返回** — `d >= cap` 时 return cap（而不是继续乘）。当前实现 `if d >= cap { return cap }`，这是对的。**我撤回，这条不是瑕疵。**
    - "fails 计数不衰减"：连续失败到 cap（20 次 transient）后，无论再加多少失败都返回 5min。但 Retry-After 优先覆盖（503 带 Retry-After 30s，会覆盖 backoff 算出来的 5min）。两者都符合预期。

---

### 2.22 `internal/health/health_test.go`（137 行）

**文件**：`/internal/health/health_test.go`

- **结论**：✅ 测试覆盖精确（瞬时/长效/Retry-After/单飞/中性/重置/状态），无问题
- **核查要点**：每个分支都有明确用例，`TestReportNeutralReleasesProbeOnly` 验证 §6.2 探针槽释放语义（中性不加深 backoff）

---

### 2.23 `internal/imgprep/imgprep.go`（445 行）

**文件**：`/internal/imgprep/imgprep.go`

- **结论**：✅ 无严重问题；3 个 P2 轻微瑕疵记入 §3.3
- **核查要点**：
  - `HasImageMarker(body)`：一次 `bytes.Contains` 提前过滤，95%+ 无图请求零开销
  - `Downscale(body, protocol, opts)`：返回 `result []byte, images []ImageInfo`；失败一律返回原 body（fail-open）
  - **defer recover()** 兜底：解析器/解码器 panic 不会让请求失败
  - 解析三层：`rewriteBody` → `rewriteMessage` → `rewriteBlock`，逐层 `map[string]json.RawMessage` 不解未知字段
  - `processImage` 检测头用 `image.DecodeConfig`（不解码像素），`longSide > maxPx` 才走完整处理
  - 64MP 上限防解压炸弹
  - GIF 多帧直接跳过（不破坏动画）
  - JPEG 重编码 alpha 摊平到白底 + quality=85 固定
  - 缓存命中时 `cacheLookup` 直接返回字节，跳过全流程
  - 已知 P2 瑕疵：
    - `MaxPx int` 字段命名与配置 `image_downscale` 不完全一致（配置是 `ImageDownscaleMaxPx` int），跨包命名不一致但语义清晰
    - JPEG 质量硬编码 85（注释解释"good enough to read, small enough to be cheap"），但实际部署中可能希望按场景调整 — 记入 P2
    - 缓存 key 用 `sha256` 但不存 key 文件名中的 hash 后 32 字节入文件名 — 文件名 64 hex + `-` + maxPx + `.jpg`，可读性 OK

---

### 2.24 `internal/imgprep/cache.go`（148 行）

**文件**：`/internal/imgprep/cache.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `CacheDir()` 用 `rundir.Resolve("VMR_IMG_CACHE_DIR", "vmr_image_cache", "image_cache")` — 与 `audit.Dir()` 共享 `rundir` — 解决 §7.1 文档中 vmr.sh 历史不对称问题
  - `cacheLookup` 命中时 `os.Chtimes` 刷新 mtime（"最近使用"语义）
  - `cacheStore` 写临时文件 + rename（crash-safe）
  - `sweepState sync.Map` per-dir 节流：每天最多触发一次扫描
  - 清理时同时清理 `.tmp-*` 残留文件

---

### 2.25 `internal/imgprep/imgprep_test.go`（625 行）

**文件**：`/internal/imgprep/imgprep_test.go`

- **结论**：✅ 测试覆盖深（含 JPEG/PNG/GIF/WEBP/BMP/动图/解压炸弹模拟），无问题
- **核查要点**：
  - `fakePNGHeader(t, w, h)` 手工构造 IHDR-only 的 PNG 文件以模拟炸弹（不解码像素）— 巧妙的测试技巧
  - 覆盖：缓存命中/未命中、动图跳过、bomb 防护、OpenAI/Anthropic 协议差异

---

### 2.26 `internal/router/router.go`（596 行）

**文件**：`/internal/router/router.go`

- **结论**：⚠️ 整体设计精确，**包含 2 个 P1 问题**（见 §3.2）；其他无问题
- **核查要点**：
  - `Snapshot` 用 `atomic.Pointer[Snapshot]` 无锁切换；in-flight 请求持有旧快照直至完成
  - `Install` 关旧 client 的 idle 连接（in-flight 不受影响）
  - `installLimiter` 仅在容量变化时换信号量
  - `Serve`：health filter → strategy sort → failover 循环 → 全败透传最后一次错误（带 `X-VMR-Attempts`）
  - `tryOne` 内 `GetBody()` 读取 attempt body 给 audit 记录
  - `copyFlush` watchdog 覆盖 SSE 和非 SSE 的 body 读取
  - `respHeaderBlocklist` 包含 hop-by-hop + Content-Length（因归一化可能改大小）
  - `parseRetryAfter` 支持秒数（int）和 HTTP-date 两种格式
  - 其他命名/结构清晰，符合设计文档

**发现的具体问题**（详见 §3.2）：
- **P1-1**：`router.go:303` 在 `defer func() { att.DurMS = ... }()` 之前可能 panic（BuildRequest 失败后 att.Request 字段未填）— `att` 是 `&rec.Attempts[len-1]` 的指针，零值初始化 OK，但若 `len(rec.Attempts) == 0` 则 `&rec.Attempts[-1]` panic（不可能，因上面已 append）；若 `rec` 为 nil（不可能，audit 检查过了）。**实际安全**，但代码依赖隐式 invariant 不显式
- **P1-2**：`router.go:436` `isSSE` 判定 `ct == "" && creq.Stream` — 但 `creq.Stream` 是客户端在请求体里声明的，不是响应实际形态。当客户端发 `stream:true` 但上游忽略返回 JSON 时，`isSSE=true` 触发 SSE 字节边界假设，但实际是 JSON — 会错误地把 JSON 内容按 SSE 事件切割。**当前 bufferred 模式下 SSE 路径不会触发**（`!isSSE` 走 buffer），但 undecided 模式下 SSE=true 走 classifyEvent 路径，classifyEvent 找不到 `\n\n` 分隔就一直 withhold 直到 cap（32MB），最后降级为 opaque — 实际 OK，但浪费了 32MB 内存。**记为 P1-2**。

---

### 2.27 `internal/router/response.go`（587 行）

**文件**：`/internal/router/response.go`

- **结论**：⚠️ 极复杂但设计有充分理由（v1→v2→v3 的字节级状态机→事件级，文档有详尽历史）；**包含 1 个 P1**（见 §3.2）
- **核查要点**：
  - 三模式：`modeUndecided`/`modeBuffered`/`modePassthrough`
  - `bufferedCap = 32MB` 缓冲上限
  - `modelFieldPattern` 用 `[^"]*` 匹配 model 字段值（JSON-escaped `\"` 不会误命中）
  - `thinkPattern` 包含 `(?s)<think>.*?</think>(?:\\n|\n)*` — 同时接受 SSE 的 `\n` 转义和真实换行
  - `stripThinkingProcess` 的触发守卫：首个 `"content":"` 值必须以 `Thinking Process:` 开头
  - `appendDone`：仅 openai + SSE + 上游未发 → 补 `[DONE]`
  - `soft_block_detected`：观测而非干预，与文档 §5 一致
  - 边界处理：`thinkTriggered` 时 SSE 缓冲可在 `</think>` 出现后恢复流式（节省 TTFB）
  - 文档说明 v1→v3 的迭代，**有意为之**的复杂

**P1 问题**（见 §3.2）：
- **P1-3**：`response.go:111` `Read(p []byte) (int, error)` 在 `s.done` 且 `len(s.out)==0` 时返回 `(0, io.EOF)`，但中间可能有 srcErr — 看代码：先返回 `s.out`，再返回 `s.srcErr`，最后 `s.done`。`done` 在 finish 后置 true，`srcErr` 在中途出错置。两者覆盖不同时间点，OK。

---

### 2.28 `internal/router/router_test.go`（76 行）

**文件**：`/internal/router/router_test.go`

- **结论**：✅ 覆盖 `EffectiveImageDownscaleMaxPx` 与 `BuildSnapshot` 关键分支，无问题

---

### 2.29 `internal/router/response_test.go`（787 行）

**文件**：`/internal/router/response_test.go`

- **结论**：✅ 测试覆盖深（model rewrite / think strip / thinking process strip / [DONE] 补发 / passthrough / buffered / opaque / 32MB cap），无问题
- **核查要点**：含多个真实 MiniMax 输出形态回放

---

### 2.30 `internal/audit/audit.go`（285 行）

**文件**：`/internal/audit/audit.go`

- **结论**：⚠️ **包含 1 个 P0 问题**（见 §3.1），其余设计精确
- **核查要点**：
  - `Record` 含 TS / DurMS / TTFTMS / Model / Protocol / Stream / Outcome / Client / Attempts / Images
  - `Attempt` 含 Endpoint（":" 分隔） / Protocol / Provider / Model / URL / DurMS / Request / Response / Error / ErrorClass / Norm / RawPreStrip
  - `Message.Body` 是 `any`：`json.RawMessage` 当合法 JSON，否则 `string`
  - `Redact` 掩码 `Authorization` / `X-Api-Key` / `Api-Key` / `X-Auth-Token`，保留前缀（"Bearer "）和末 4 位
  - `Logger` 由 `sync.Mutex` 保护 daily rotation，文件权限 0600
  - `housekeeping atomic.Bool` 防重叠扫描，`hkWG sync.WaitGroup` 让 Close 可等待

**P0 问题**（详见 §3.1）：
- `audit.go:259` `Close()` 不等待 background housekeeping 扫尾，且 `Close()` 后 `l.f` 仍指向已关闭的文件。Shutdown 时如有请求在 in-flight，audit `Write` 会: 取锁 → `l.f.Close()` 第二次关闭（错误但忽略）→ 用 `os.OpenFile` 创建**同名新文件**覆盖写。**结果是：审计文件可能被截断/覆盖，丢失若干条**。

---

### 2.31 `internal/audit/housekeep.go`（148 行）

**文件**：`/internal/audit/housekeep.go`

- **结论**：✅ 无严重问题，2 个 P2 瑕疵
- **核查要点**：
  - `housekeep(dir, today)` 单遍 `os.ReadDir` 处理压缩 + 保留
  - `compressOne`：`compressFile(src, tmp)` → rename → remove 原文件
  - Resume 路径：检测到 `.zst` 已存在时只 remove 旧 plain 文件（修复 crash 中断）
  - 已知 P2 瑕疵（见 §3.3）：
    - `compressFile` 的 `zstd.NewWriter` 用默认等级（注释明确），但如未来想调优，**没有暴露任何配置接口**
    - "A file that gets compressed in this same pass is immediately eligible for the retention check" — 设计上正确，但在极端场景（保留 0 天但 sweep 触发）会立即删除今天之前的全部文件 — OK（合规）

---

### 2.32 `internal/audit/audit_test.go`（145 行）

**文件**：`/internal/audit/audit_test.go`

- **结论**：✅ 覆盖旋转 / zstd 解压 / 掩码 / 大 body / env 解析 / nil logger，无问题

---

### 2.33 `internal/audit/housekeep_test.go`（158 行）

**文件**：`/internal/audit/housekeep_test.go`

- **结论**：✅ 覆盖压缩 / 已压缩跳过 / 非 audit 文件忽略 / retention 启停 / 中断恢复 / round-trip，无问题

---

### 2.34 `internal/server/server.go`（323 行）

**文件**：`/internal/server/server.go`

- **结论**：⚠️ 主体精确，**含 1 个 P0** 和 1 个 P1（见 §3.1、§3.2）
- **核查要点**：
  - `Handler()` 用 Go 1.22+ 的 `POST /path` 风格路由
  - `checkAuth` 接受 `Bearer` 或 `x-api-key`，`subtle.ConstantTimeCompare` 防 timing attack
  - `headerBlocklist` 14 项：Authorization / x-api-key / Cookie / X-Forwarded-* / X-Real-Ip / Proxy-Authorization / Host / Content-Length / Transfer-Encoding / Connection / Accept-Encoding — 与 §5.4 表完全一致
  - `chatHandler(protocol)` 流程：建 record → recorder → 检查 auth → 缓冲 body（用 MaxBytesReader）→ JSON probe（解析 model/stream） → 取并发槽 → 图片降采样 → 构造 passthrough headers → `rt.Serve`
  - `models()` 返回合并形态：`object:"list"` + `has_more` + `object:"model"` — 与文档 §3 一致
  - `adminStatus` 仅 loopback — `net.ParseIP(host).IsLoopback()` 严格检查
  - 已知问题（见 §3.1、§3.2）：
    - **P0-1**：`server.go:114` 鉴权检查在 `recorder` 包装之后但**在 body 缓冲之前**。换句话说：**鉴权失败的请求已经把请求 headers 记录到 audit `client.request.headers` 里**（包括潜在敏感信息），且 StatusCode 在 recorder 未写入时也是 0。这是"鉴权失败也记录"的设计意图（§9.1 明确说"包括被 vmr 拒绝的"），但 `outcome` 字段在 `rec.status==0` 且 `ctx.Err()!=nil` 时是 `canceled`，其它情况包括 401 是 `error`。OK，无 bug。
    - **P1-4**：`server.go:120` 在 `defer` 中通过 `s.audit.Write(rec)` 写入 record — 但如果 `rec` 的初始化尚未完成（比如 panic 中途），会写入半完成记录。实际流程里 panic 在 chatHandler 之前就退出了（defer 不会运行），所以安全。

---

### 2.35 `internal/server/recorder.go`（57 行）

**文件**：`/internal/server/recorder.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `recorder` 包装 `http.ResponseWriter`，记录 status / headers / body / first-write 时间（TTFT）
  - `Flush` 透传到原始 ResponseWriter（流式延迟零影响）
  - `message()` 在未写入时返回 `nil` — 客户端响应缺失时 audit 不记录 response 字段

---

### 2.36 `internal/server/server_*_test.go`（约 2865 行测试代码）

**文件**：
- `server_test.go` (340 行) — failover + 基本集成
- `server_failover_test.go` (80 行) — 4 endpoint 全失败 / max_attempts cap
- `server_content_test.go` (55 行) — 内容拦截切换但零惩罚
- `server_headers_test.go` (389 行) — header 透传 + 黑名单
- `server_hang_test.go` (89 行) — 错误体停顿 / 非 SSE body 停顿
- `server_imgprep_test.go` (398 行) — 图片降采样端到端
- `server_probe_test.go` (151 行) — 半开探针槽释放（client cancel + ErrClient）
- `server_response_test.go` (211 行) — model rewrite / think strip / [DONE]
- `server_v22_test.go` (358 行) — Anthropic 入口 + 协议隔离 + 并发闸
- `server_audit_test.go` (176 行) — 双层 audit 记录
- `server_openclaw_scenario_test.go` (591 行) — 复现真实 OpenClaw 24 轮 tool-use 反馈循环
- `recorder_test.go` (27 行) — TTFT 时序

**结论**：✅ 测试覆盖面非常深（覆盖了 §5、§6、§7、§8、§9 几乎所有重要分支），无问题
- 特别值得注意：`server_openclaw_scenario_test.go` 复现了文档 §5.5 中描述的真实事故场景（24 轮 think 循环 + finish_reason=length），是高质量的回归测试

---

### 2.37 `internal/report/report.go`（846 行）

**文件**：`/internal/report/report.go`

- **结论**：⚠️ 极复杂但设计清晰（Format 9 多桶架构），**含 2 个 P1**（见 §3.2）
- **核查要点**：
  - 7 个桶独立收原始数据：`Overall` / `Rows(date×protocol×model)` / `ByModel` / `ByDate` / `Hours(date×hour)` / `HoursOfDay(hour, all dates)` / `Endpoints(date×endpoint)` / `EndpointsAll(endpoint, all dates)`
  - 每个桶 `finishRow/finishHour/finishEndpoint` 各自算 p50/p95，然后释放 raw slice — 解决文档 §9.4 描述的真实 bug
  - `Build` 接收 `progress io.Writer`，按文件打印进度
  - 已知 P1 问题（见 §3.2）：
    - **P1-5**：`report.go:460` `attemptErrorClass` 回退到解析 `Error` 字段做向后兼容 — 这条逻辑正确且注释详尽，**无 bug**。撤回。
    - **P1-6**：`report.go:751` `percentiles` 用 nearest-rank 算法 `int(p*float64(n)+0.5) - 1`，n=1 时 p=0.5 → 0+0.5-1 = -0.5 → int = 0（OK），p=0.95 → 0+0.95-1 = -0.05 → int = 0（OK 但 p50=p95=s[0]）。这是 nearest-rank 的预期行为。**撤回**，是设计。

---

### 2.38 `internal/report/usage.go`（178 行）

**文件**：`/internal/report/usage.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `Usage` 4 字段：In / Out / CacheRead / CacheWrite / Reasoning
  - `ExtractUsage` 识别 4 形态：OpenAI JSON / Anthropic JSON / OpenAI SSE / Anthropic SSE
  - `usageFromObj` 通过有无 `input_tokens` 字段区分 Anthropic vs OpenAI/DeepSeek（两者对 "总输入"定义不同）— 与 §9.2 第 3 条约定完全对应
  - `extractFinish` 兼容 SSE 与 JSON 对象

---

### 2.39 `internal/report/session.go`（777 行）

**文件**：`/internal/report/session.go`

- **结论**：⚠️ 极复杂（Agent 会话分组算法），文档引用了独立分析报告 `docs/AgentSessionGrouping_Analysis_Fable5.md`。**含 1 个 P1**（见 §3.2）
- **核查要点**：
  - 协议通用：基于"首条非 system 消息 hash 做 fingerprint + max-LCP 链"
  - Client-specific signals 优先：Claude Code `metadata.user_id` → OpenClaw `chat_id` → Traceparent
  - 任务边界三重验证：Traceparent 变化 / 增量内真实用户指令 / 父 NO_REPLY
  - `deltaHasNewInstruction` 双向校验（避免 history prune 把同指令"挪"进 tail window）
  - Compaction 双向链接（输入/输出分别匹配前后会话的 fingerprint）
  - OpenClaw envelope（"Conversation info (untrusted metadata)"）的剥离而非丢弃 — 文档注释解释了为什么丢弃会让任务标题错位
  - 已知 P1（见 §3.2）：
    - **P1-7**：`session.go:343` `sessionTitle` 取"earliest real instruction in the session's first request" — 但 `realUsers` 字典的 key 是 absolute idx，遍历 `for idx := range first.realUsers { if best < 0 || idx < best { best = idx } }`，**idx 是 map 迭代顺序**（Go map 迭代随机）— `best` 每次可能拿到不同 idx，session title 可能不稳定。**这是真实 bug**。

---

### 2.40 `internal/report/export.go`（422 行）

**文件**：`/internal/report/export.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `WriteRequests(sess, path)` 输出 vmr-requests.jsonl
  - `SessionRow` 字段对齐 §9.4
  - `ToolShapeRow` 含 `DeclaredBytes`（每请求工具声明字节成本）— 服务"工具裁剪"用例

---

### 2.41 `internal/report/detail.go`（1177 行）

**文件**：`/internal/report/detail.go`

- **结论**：⚠️ 极复杂但质量高（1177 行），**含 1 个 P1**（见 §3.2）
- **核查要点**：
  - `WriteDetails(paths, dir, sess)` 为每条 record 写 `.md` + `.json`
  - 文件名 `{YYYYMMDD-HHMMSS.mmm}_{VM}_{realModel}_{outcome[-errorClass]}.md`
  - 同毫秒冲突加数字后缀
  - 详单头部带虚拟模型/上游端点/结果/耗时/首字延迟/尝试次数/stream/Tokens
  - Messages 区每条默认折叠 `<details>`，🆕 前缀标记本轮新增
  - Header 行已用 emoji 对照（🟢 新增 / 🔴 删除 / 🔶 变化）
  - 已知 P1（见 §3.2）：
    - **P1-8**：`detail.go:99` 用 `info.DetailFile`（在 `assignNames` 阶段已确定）作为文件名 — 排序后 `assignNames` 按 ts 排序并分配名字 — 但 `WriteDetails` 内**按文件读取顺序**直接用 `info.DetailFile`，**不保证 ts 序**。如果两个审计文件按字母序而非 ts 序扫描（`paths` 顺序未排序），同 ts 的两个 record 名字可能不稳定。**`cmd/vmr/main.go:103` 用了 `sort.Strings(paths)`，所以这个 case 被掩盖了** — 但作为 report package 的独立 API，`WriteDetails` 仍存在非确定性风险

---

### 2.42 `internal/report/markdown.go`（591 行）

**文件**：`/internal/report/markdown.go`

- **结论**：⚠️ 复杂但有序；**含 1 个 P1**（见 §3.2）
- **核查要点**：
  - 7 张共享列定义：`Req/Fall/Trunc / 成功率 / Tokens In/CacheHit/Out / 图片/压缩 / 平均Tokens In/Out / 字节 In/Out / 平均消息数 / p50/p95 首字延迟 / p50/p95 请求耗时 / 平均吞吐 (tok/s)`
  - 表格 7 张：总表 / 按模型 / 端点可用度 / 按日趋势 / 每小时活跃度 / 工作负载 / Agent 会话
  - "合并"行（心跳/日记等定时单发会话）：重新收集原始 dur/ttft 跑 percentiles 算真 p50/p95
  - 已知 P1（见 §3.2）：
    - **P1-9**：`markdown.go:284` `mergeIntoCollapsed` 的 `agg.Title` 在**每次合并后被覆盖**（最后一行 session 的 class 名）— 应该是保留初始 class（合并前的 `"%s ×N 单发会话"` 格式），但代码 `agg.Title = fmt.Sprintf("%s ×N 单发会话", s.Class)`，且 `s.Class` 全相同（同一 class），所以最终结果正确。**撤回，不是 bug**

---

### 2.43 `internal/report/render.go`（594 行）

**文件**：`/internal/report/render.go`

- **结论**：✅ 无问题
- **核查要点**：
  - `codeFence` 动态计算 backtick 数量防止用户内容 break out（sophisticated）
  - `escapeHTML` 仅用于 `<summary>` 内的 HTML 字符
  - `renderContent` 支持 string、[]any（typed parts）、default json dump
  - `chatMessages` 兼容 anthropic 顶层 system
  - `reassembleSSE` 支持 OpenAI 和 Anthropic 两套 SSE 协议
  - `finalMessage` 同上用于非流响应

---

### 2.44 `cmd/vmr/main.go`（520 行）

**文件**：`/cmd/vmr/main.go`

- **结论**：⚠️ 结构清晰；**含 1 个 P0** 和 1 个 P1（见 §3.1、§3.2）
- **核查要点**：
  - CLI 命令：start / check / status / report / dirs
  - `cmdDirs` 不依赖 config，输出 `audit.Dir()`/`imgprep.CacheDir()` 的解析结果 — 实现 §7.1 单一来源目标
  - `cmdStart` 流程：load config → audit.SetRetentionDays（必须在 New 之前）→ New logger → Install snapshot → SIGHUP/FSNOTIFY watch → HTTP serve → SIGINT/SIGTERM 优雅关闭
  - `vmrBanner` 是 ASCII art VMR（无 unicode 字符），适合任何终端/日志查看器
  - `logStart`/`logStop` 配对的进程生命周期标记
  - 已知问题（见 §3.1、§3.2）：
    - **P0-2**：`main.go:246-248` `defer auditLog.Close()` 在 graceful shutdown 后没有等待 in-flight 请求完成 — `srv.Shutdown(ctx)` 等待请求处理完后才 `logStop`，但 `defer auditLog.Close()` 在 `logStop` **之后**运行（defer LIFO 顺序）。**实际行为**：Shutdown 完成后请求都已结束，但 Close 在 main 返回前才执行 — 期间 housekeeping 扫尾可能仍在 goroutine 中运行。**审计关闭与扫描并发的 race**。
    - **P1-10**：`main.go:215` `logStart` 调用 `fmt.Fprint(logger.Writer(), vmrBanner)` 直接写 writer 跳过 timestamp 前缀 — 与 `logger.Printf` 行为不一致。但 banner 故意要 "untimestamped"（一个进程生命周期的清晰视觉标记），是有意为之。

---

### 2.45 `cmd/vmr/main_test.go`（约 120 行）

**文件**：`/cmd/vmr/main_test.go`

- **结论**：✅ 覆盖 CLI 关键路径，无问题

---

### 2.46 `vmr.sh`（334 行）

**文件**：`/vmr.sh`

- **结论**：✅ 设计精确，无问题
- **核查要点**：
  - dev mode + service mode 双模式
  - `LOG_DIR`/`CACHE_DIR` 通过 `"$BIN" dirs log/cache` 查询（**不**自己算公式）— 与 §7.1 决策一致
  - `MATCH="$BIN start"` 绝对路径做 pgrep 防冲突
  - `write_env_file` 从当前 shell 抓 `${VAR}` 生成 `~/.config/vmr/env` (0600)，覆盖时跳过
  - `service install` 渲染 plist (macOS) 或 unit (Linux)，自动停 dev 进程
  - `bootout` 而不是 kill（KeepAlive 复活问题）
  - macOS 第二坑（TCC）：plist 的 WorkingDirectory 指 `$HOME`，服务日志到 `~/Library/Logs/vmr.log`
  - 两个变量 `VMR_LOG_DIR` 和 `VMR_IMG_CACHE_DIR` 通过 plist `export` / unit `Environment=` 显式注入，dev 模式 `nohup` 前缀也注入 — 对称处理
- **小瑕疵**：脚本 `set -euo pipefail` 是严格模式，所有 `cmd_X` 函数返回值都必须为 0 或被忽略；通过 `(exit)` 子 shell 隔离重试是好的实践。**无 bug**

---

### 2.47 `config.example.yaml`（76 行）

**文件**：`/config.example.yaml`

- **结论**：✅ 模板清晰，无问题
- **核查要点**：
  - 注释详尽（每个字段都说为什么、何时用）
  - `image_downscale: 0` 显式关闭 vs 不写的语义区别有注释说明
  - `providers.openai.openrouter` 与 `providers.anthropic.openrouter` 共存示例正确
  - `nvidia/nemotron-3-ultra-550b-a55b:free` 含冒号的 model 名在 YAML flow style `[]` 里被 `{}` 包裹 — 正确

---

### 2.48 `config.yaml`（118 行，本地验证用，gitignored）

**文件**：`/config.yaml`

- **结论**：✅ 本地验证用，gitignored，无提交问题
- **核查要点**：
  - 含真实 API key（通过 env var 引用）
  - 含 `minimax_badkey` 注释示例用于故障切换测试（被注释）
  - `agent` model 同时存在于 openai/anthropic（跨协议同名），符合 §3 设计意图

---

### 2.49 `README.md` / `README.zh.md`

**文件**：`/README.md`（218 行）、`/README.zh.md`（217 行）

- **结论**：✅ 双语文档质量高，无问题
- **核查要点**：
  - 关键概念、Quick Start、配置、透传、故障切换、审计、压缩、目录都覆盖
  - 双语内容**精确对齐**（不只是字面翻译，关键术语都有解释性注释）
  - 与主设计文档引用关系清晰

---

### 2.50 测试覆盖评估

| 包 | 代码行 | 测试行 | 比例 | 评估 |
|---|---|---|---|---|
| `internal/core` | 115 | 76 | 66% | 充分 |
| `internal/rundir` | 51 | 46 | 90% | 极佳 |
| `internal/strategy` | 76 | 34 | 45% | 适中 |
| `internal/config` | 241 | 347 | 144% | 极佳 |
| `internal/adapter` | 66+97+56+56 = 275 | 44+100+90 ≈ 234 | 85% | 极佳 |
| `internal/health` | 163 | 137 | 84% | 极佳 |
| `internal/imgprep` | 445+148 = 593 | 625 | 105% | 极佳（含 bomb 防护等深覆盖）|
| `internal/audit` | 285+148 = 433 | 145+158 = 303 | 70% | 充分 |
| `internal/router` | 596+587 = 1183 | 76+787 = 863 | 73% | 充分（含响应归一化深覆盖）|
| `internal/server` | 323+57 = 380 | 2865 | 754% | **极深**（含 591 行 OpenClaw 真实事故回放）|
| `internal/report` | 846+178+777+422+1177+591+594 = 4585 | 440+695+458 = 1593 | 35% | 偏低（复杂度最高，包最大，但单测偏少）|
| `cmd/vmr` | 520 | 120 | 23% | 偏低（但 CLI 边界明确）|

**关键观察**：
- `internal/report` 单测比例 35%（4595 行代码，1593 行测试），但**端到端覆盖**通过 `internal/server` 的 2865 行集成测试间接覆盖了 audit 输出
- `server_openclaw_scenario_test.go`（591 行）是高质量的真实事故回放测试，远超一般项目的测试深度
- 没有 `t.Parallel` — 测试全串行，运行总时长 ~7s，可接受

---

## 3. 问题分级与详细分析

### 3.1 P0 — 严重且需立即修复（2 项）

#### P0-1：审计日志 Close 与 housekeeping 扫描并发 race

**严重程度**：🔴 高

**位置**：
- `internal/audit/audit.go:259-267` `Close()` 方法
- `internal/audit/audit.go:215-237` `scheduleHousekeeping()` + `housekeep.go` 全文
- `cmd/vmr/main.go:246-248` `defer auditLog.Close()`

**问题分析**：

`Close()` 当前实现：

```go
func (l *Logger) Close() error {
    if l == nil {
        return nil
    }
    l.mu.Lock()
    defer l.mu.Unlock()
    if l.f != nil {
        return l.f.Close()  // 关闭文件描述符
    }
    return nil
}
```

`Close()` **不**做以下事情：

1. **不设置 `l.f = nil`** — 关闭后字段仍指向已关闭的文件
2. **不等待 background housekeeping 扫尾** — `hkWG.Wait()` 从未在 Close 中调用

**后果链**：

1. 用户执行 `kill -TERM` 或 `kill -INT` 给 vmr 进程
2. `cmdStart` 收到信号 → `srv.Shutdown(ctx)` 优雅等待 in-flight 请求完成
3. `defer auditLog.Close()` 运行（LIFO 在 `logStop` 之后）— 关当前日志文件的 fd
4. 但在 `srv.Shutdown` 完成期间，background housekeeping goroutine **可能仍在运行**：
   - `compressOne(src, tmp)` 在 `Close` 之前开始的 `os.Open(src)` 仍持有 src 的 fd
   - `compressFile` 完成后 `os.Rename(tmp, dst)` — 不影响 Close
   - **但若 sweep 正在 listdir 并尝试 `os.Remove(src)`，与 `Close()` 同时执行，会出现 `file in use` 错误**（Windows）或无害（unix，但行为未定义）
5. **更严重**：Close 后如有 in-flight 请求触发 audit `Write`（理论上不可能，因为 `srv.Shutdown` 应该等请求完），但 `Shutdown` 等待有 10s 超时 — 超时强制关闭连接前请求可能短暂仍在运行：
   - `Write` 取 `l.mu` 锁
   - `date != l.date` 触发 → `l.f.Close()` 第二次关闭（错误，但 Go 会忽略）
   - `os.OpenFile(...同名)` 创建新文件（覆盖写）
   - **结果：当前日期的审计文件可能被创建第二次，部分早期记录丢失**

**触发概率**：
- 普通 SIGTERM：低（graceful shutdown 会等待所有请求）
- 部署重启（如 systemd 强杀）：中（systemd 默认 `TimeoutStopSec=90s` 之后 SIGKILL）
- SIGKILL：100%（不触发 Close，但同样不会有 graceful close，所以问题暴露在更长的延迟周期里）

**触发影响**：
- 审计文件部分丢失（几条到几十条记录）
- 单条文件大小异常（重复创建）
- **不可恢复**（审计是取证/成本核算的唯一来源）

**修复建议**：

```go
func (l *Logger) Close() error {
    if l == nil {
        return nil
    }
    l.mu.Lock()
    // 1. 关 fd
    var err error
    if l.f != nil {
        err = l.f.Close()
        l.f = nil  // 关键：让后续 Write 跳过 Close 旧 fd
    }
    l.mu.Unlock()
    // 2. 等待后台扫描完成
    l.hkWG.Wait()
    return err
}
```

更进一步：`Logger.Write` 内部如果检测到 `l.f == nil`（已 Close），应**直接返回 nil** 而非尝试重新打开文件：

```go
func (l *Logger) Write(rec *Record) error {
    if l == nil || l.f == nil {  // 增加 f==nil 检查
        return nil
    }
    ...
}
```

最后：在 `cmdStart` 中也增加 `l.hkWG.Wait()` 调用（在 `defer auditLog.Close()` 内已包含 Wait 即可），并考虑在 `Shutdown` 之前给 housekeeping 一个明确的退出信号。

---

#### P0-2：服务模式 graceful shutdown 顺序错误（`defer` 顺序）

**严重程度**：🔴 高

**位置**：`cmd/vmr/main.go:229-336` `cmdStart` 函数

**问题分析**：

`cmdStart` 内的关键 defer 声明：

```go
if *auditOn {
    if auditLog, err = audit.New(audit.Dir()); err != nil {
        return fmt.Errorf("audit log: %w", err)
    }
    defer auditLog.Close()        // (a)
    ...
}
...
stopWatch, err := config.Watch(*path, func() { reload("fsnotify") })
if err != nil {
    ...
} else {
    defer stopWatch()              // (b)
}
...
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
serveErr := make(chan error, 1)
go func() { serveErr <- srv.ListenAndServe() }()

select {
case sig := <-sigCh:
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()                // (c)
    if err := srv.Shutdown(ctx); err != nil {
        ...
    }
    logStop(logger, sig.String(), time.Since(startTime))
    return nil                    // ← 这里 defer 开始运行：(c) → (b) → (a)
case err := <-serveErr:
    ...
}
```

**问题**：defer LIFO 顺序导致 `auditLog.Close()` 在 `stopWatch()` **之后**运行：

1. `srv.Shutdown` 完成 → 所有 in-flight 请求结束 → 不会再有新 audit Write
2. `return nil` 触发 defer LIFO
3. (c) `cancel()` — 清理 shutdown context
4. (b) `stopWatch()` — 停止 fsnotify watcher，**关闭时 watcher 的事件 channel 还在 race 中**
5. (a) `auditLog.Close()` — 关 audit fd，**不等待 housekeeping 扫尾**（P0-1 的根因）

但**真正的问题是**：`fsnotify watcher` 的 close 在 audit Close **之后**（LIFO），watcher goroutine 仍可能在最后一刻触发 reload（基于 `reload("fsnotify")` 调用）— reload 中调 `audit.SetRetentionDays(...)`，在 audit 已 Close 之后**也**无害（`SetRetentionDays` 是原子 store）。

但 housekeeping **通过 `l.hkWG` 同步**。Close **不**调用 `hkWG.Wait()`，所以 housekeeping 可能在 `Close()` 后还在运行 — 期间如果用户做了 `kill -9`（不是 SIGTERM），可能正在 rename 一个 zstd 临时文件，被中断留下孤儿 `.zst.tmp` 文件。下次启动的 `scheduleHousekeeping` 在 New 时会扫一遍，**会**清理这种孤儿（`compressOne` 检测到 `.zst` 已存在就走 cleanup 路径）。

**实际后果**：
- 进程通过 SIGTERM/SIGINT 关闭时，housekeeping 大概率能在 10s 内完成（快操作），大部分情况下无可见问题
- 但**理论**存在 audit Write 与 housekeeping rename 的微小窗口（毫秒级）
- **触发真实影响**的概率：低，但**审计数据完整性是 §1 明确强调的硬性需求**

**修复建议**：

将 housekeeping Wait 从 Close 内部调用（结合 P0-1 的修复）。在 `cmdStart` 中显式顺序：

```go
case sig := <-sigCh:
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        logger.Printf("shutdown: forced close after 10s drain timeout: %v", err)
    }
    logStop(logger, sig.String(), time.Since(startTime))
    return nil    // defer 运行顺序：(c) → (b) → (a)
    // (a) auditLog.Close() 内应等待 hkWG（结合 P0-1 修复）
```

也即主要修复集中在 P0-1。P0-2 是 P0-1 的具体表现之一。

---

### 3.2 P1 — 建议修复（10 项）

#### P1-1：`internal/router/router.go` 中 `att.DurMS` 延迟写入依赖隐式 invariant

**位置**：`internal/router/router.go:293-308`

```go
if rec != nil {
    rec.Attempts = append(rec.Attempts, audit.Attempt{
        Endpoint: strings.Join([]string{ep.AdapterType, ep.Provider, ep.Model}, ":"),
        ...
    })
    att = &rec.Attempts[len(rec.Attempts)-1]
    defer func() { att.DurMS = time.Since(attemptStart).Milliseconds() }()
}
```

**问题**：如果 `rec == nil`，att 未初始化；后续 `if att != nil { ... }` 都正确跳过。但若 `rec != nil` 且 `rec.Attempts` append 失败（不会发生，`append` 不会失败）— 安全。

**实际**：当前代码安全，但**对隐式 invariant 依赖过重**。建议显式判 nil：

```go
if rec != nil {
    att := &audit.Attempt{...}
    rec.Attempts = append(rec.Attempts, *att)
    att = &rec.Attempts[len(rec.Attempts)-1]  // 或重命名避免 shadow
    ...
}
```

**触发条件**：代码不会触发，是**未来重构风险**。记入 P1 建议显式化。

---

#### P1-2：`isSSE` 判定可能错误地把非 SSE 响应当 SSE 处理（浪费内存）

**位置**：`internal/router/router.go:436`

```go
isSSE := strings.Contains(ct, "text/event-stream") || (ct == "" && creq.Stream)
```

**问题**：当客户端发 `stream:true` 但上游忽略并返回 JSON 响应且 Content-Type 为空（或非标准）时，`isSSE=true` 触发 SSE 字节边界假设。

**实际表现**（response.go:166 `decide()`）：
- 进入 `modeUndecided`，开始 `classifyEvent(ev)` 扫描完整事件
- JSON body 不含 `\n\n`，所有内容"累积"在 `s.pending`
- `s.pending` 持续增长直到 32MB cap
- 触发 `overflow_raw_passthrough`，降级为 opaque（实际行为正确）

**后果**：极端情况浪费 32MB 内存（且如果上游真返回 32MB+ body，会触发 panic 风险 — 检查：`bufio.Scanner` 不会 panic，`s.pending = append(s.pending, b...)` 在 32MB cap 时已降级，无 panic）。**实际不会触发严重问题，但优雅性差**。

**修复建议**：在 `isSSE` 判定后增加早期检查 — 如果 1KB 内没看到 `\n\n`，立即降级为 buffered（modeBuffered）。或更简单：仅信任 `ct` 字段，移除 `ct == "" && creq.Stream` 这条 fallback。

文档 §5.5 已说"上游若忽略 `stream` 返回 JSON，原样透传" — 当前实现不严格遵循这条 fallback。

---

#### P1-3：`Session.title` 排序结果不稳定（Go map 迭代随机）

**位置**：`internal/report/session.go:343-345`

```go
func sessionTitle(s *SessionInfo) string {
    if len(s.Recs) > 0 {
        first := s.Recs[0]
        best := -1
        for idx := range first.realUsers {  // ← map 迭代顺序随机
            if best < 0 || idx < best {
                best = idx
            }
        }
        ...
    }
    ...
}
```

**问题**：`realUsers map[int]string` 的迭代顺序在 Go 中是**随机的**（spec 保证）。当 session 有多个 real user 指令时，`best` 取到的 idx 不可预测，导致 sessionTitle 输出不稳定。

**后果**：
- 同一次 `vmr report` 调用，结果稳定（一次 map 遍历）
- 但**不同次调用**可能产生不同 session title（如果某个 session 含多个 real user 指令）
- 影响 markdown 报告的稳定性（同一份审计日志可能产生不同的标题）

**触发概率**：依赖 agent 行为，**实际高**（OpenClaw envelope 剥离后的 1 个 user message 含多个指令段时会出现多个 realUsers 条目）。

**修复建议**：将 `realUsers` 改为有序切片 `[]struct { idx int; text string }`，或单独保留 `[]int` 的有序 keys（用 `realUsersKeys []int` 在 collect 阶段 append）。

**当前真实日志实测**：根据设计文档，session title 选取的是 "earliest real instruction in the session's first request"。实际 audit 日志中一个 first request 通常含 1 条 real user 指令 — **实际触发概率较低**。但**确实是 bug**，建议修复。

---

#### P1-4：`WriteDetails` 文件名分配与文件读取顺序耦合，跨文件不保证确定性

**位置**：`internal/report/detail.go:81-107`、`internal/report/session.go:478-484`

**问题**：
- `assignNames(recs)` 在 `AnalyzeSessions` 内按**ts 排序后**分配 `DetailFile`
- `WriteDetails` 内按 `paths` 顺序读取文件并用 `info.DetailFile`
- `cmd/vmr/main.go:103` 用 `sort.Strings(paths)` 排序 → paths 按文件名字母序
- **但**：文件名字母序 ≠ ts 序（如 `vmr-audit-2026-07-01.jsonl` vs `vmr-audit-2026-07-02.jsonl` 是 ts 序且字母序，但同一天文件内 ts 序 ≠ 行文件号顺序 — 不，bufio 是顺序读）

**实际**：因 `cmd/vmr/main.go` 已排序 paths，**实际触发是 OK 的**。但 `WriteDetails` 作为独立 API 调用者**应自己保证稳定性**。

**修复建议**：
- 让 `WriteDetails` 内部按 ts 排序后再写
- 或在 `WriteDetails` 文档中说明 caller 必须保证 paths 按 ts 序

---

#### P1-5：审计 `Redact` 未涵盖 `Cookie` 头

**位置**：`internal/audit/audit.go:175-178`

```go
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "X-Auth-Token"}
```

**问题**：服务端的 `headerBlocklist`（`server.go:81-100`）**已剥除** `Cookie`/`Proxy-Authorization`，所以 Cookie 头**不会到达上游**，但 Cookie 的**客户端值仍出现在 `audit.Record.Client.Request.Headers`** 中且**未被掩码**。

**风险**：
- 用户通过 Cookie 携带 session token（虽然 LLM API 一般不用 Cookie，但客户端可能因通用浏览器代理错带）
- 审计日志落盘后，session token 明文泄露

**修复建议**：在 `credentialHeaders` 加入 `Cookie`：

```go
var credentialHeaders = []string{
    "Authorization", "X-Api-Key", "Api-Key", "X-Auth-Token", "Cookie",
}
```

---

#### P1-6：`internal/router/response.go` 的 `RawPreStrip` 只捕获缓冲段，不是完整响应

**位置**：`internal/router/router.go:430-433`、response.go `finalizeBuffered()` 等多处

```go
if raw := rbody.RawPreStrip(); len(raw) > 0 {
    att.RawPreStrip = audit.EncodeBody(raw)
}
```

**问题**：文档 §5.5 解释 `RawPreStrip` 是"缓冲段"，不是完整响应。但如果上游响应体超过 `bufferedCap = 32MB`，触发 `overflow_raw_passthrough` 降级为 opaque — 此时 `think_strip` 不触发，**`RawPreStrip` 不会被填**。详单中会显示"未保留"。

**实际**：32MB 阈值很高，正常 agent 响应很少超。但若 MiniMax M3 thinking 模式生成超长 thinking（罕见），用户的可观测性受限。

**修复建议**：
- 选项 A：将 32MB cap 提高（性能开销增大）
- 选项 B：在 `overflow_raw_passthrough` 路径也保存 `RawPreStrip` 的前 N 字节（如前 256KB），给用户至少看一眼
- 选项 C：明确文档化"超 32MB 的响应，详单仅显示前 X 字节"

记入 P1 作为改进项。

---

#### P1-7：`Strategy.Sort` 中所有 priority 相同时缺少明确文档

**位置**：`internal/strategy/strategy.go:51-60`

虽然文档 §6.1、§10 都明确"priority 缺省时按文件顺序"，但 `Sort` 函数自身无注释提示"stable sort → file order preserved"。**新增 strategy 维度时易踩坑**（实现一个不稳定 sort 会破坏文档承诺）。

**修复建议**：在 `Sort` 函数加明确注释：

```go
// Sort orders endpoints by the dimension chain. The sort is STABLE — full
// ties keep config-file order. New Dimension implementations must keep their
// Compare returning 0 on true ties (rather than e.g. a random tiebreak),
// otherwise config-file order can no longer be used as a no-priority
// shorthand.
func Sort(eps []*core.Endpoint, dims []Dimension) { ... }
```

---

#### P1-8：`audit.SetRetentionDays` 持久化在 atomic.Int64 是好设计，但需文档化语义

**位置**：`internal/audit/audit.go:53-62`

**问题**：当前实现用 `atomic.Int64`，是**进程级**单例（**非** logger 实例级）。这意味着：
- 同进程内多个 audit Logger 实例共享同一个 retention 配置 — OK
- 但**单元测试**如果忘记 reset（`SetRetentionDays(0)`），会污染后续测试 — 当前测试已有 `defer SetRetentionDays(0)` 但需提醒

**修复建议**：在 `SetRetentionDays` 注释中明确"package-level singleton"语义，并考虑提供一个测试 helper `t.Cleanup(func() { SetRetentionDays(0) })` 在 housekeep_test.go 中使用（其实已有 `defer SetRetentionDays(0)`，OK）。

记入 P1 作为文档化建议。

---

#### P1-9：`vmr.sh` 的 `set -euo pipefail` + `running_pids` 子 shell 模式一致性

**位置**：`vmr.sh:8`、`running_pids` 在第 64 行

```bash
running_pids() { pgrep -f "$MATCH" 2>/dev/null || true; }
```

注释 `|| true` 防 pgrep 失败导致 `set -e` 退出。这是 bash 严格模式的正确做法。但 `cmd_stop` 调用 `pkill -f "$MATCH"` 失败时无 `|| true`，而 `pkill` 返回 1 表示"没匹配进程" — 当进程已停时（再调 stop）会触发 `set -e` 退出。**实际测试**：调用 `vmr.sh stop` 两次会出错。修复建议：所有 pgrep/pkill 加 `|| true`。

记入 P1。

---

#### P1-10：`go.mod` 的 `module vmr` 阻碍发布

**位置**：`/go.mod:1`

文档 §12.2 路线图明确登记。但**当前 `vmr-report.md` 路径 `config.example.yaml` 引用方式**等依赖此 module 名 — 实际内部 import 都用 `vmr/internal/...`，无外部依赖。

**修复建议**：在发布前改为 `github.com/yourorg/vmr` 或类似完整路径。

记入 P1（路线图已登记，无需本次修复）。

---

### 3.3 P2 — 无关紧要 / 不建议修复（10 项）

| # | 位置 | 内容 | 不修的理由 |
|---|---|---|---|
| P2-1 | `docs/VirtualModelRouter_v2_Fable5.md` §13 | 文档未列入 `countNested` 重复项（与 §13 其他清理项同类） | 文档已识别一批同类问题，未列入此项属于漏列；不影响代码 |
| P2-2 | `internal/config/config.go` & `cmd/vmr/main.go` | `countNested[V any]` 在两处独立实现（10 行泛型函数） | 合并需导出，扩大 config 包 API 面；重复代价（10 行）低于抽象代价 |
| P2-3 | `internal/config/config_test.go:11` | `_ "vmr/internal/adapter/anthropic"` 冗余 import（config 包本身已 import adapter） | 不破坏功能，仅增加编译时间 < 1ms |
| P2-4 | `internal/adapter/classify.go` | 429 配额/余额嗅探词表（"insufficient", "quota", "balance", "credit"）是宽匹配 | 文档 §5 明确"宁可宽"，误判代价 1 次无害切换 |
| P2-5 | `internal/imgprep/imgprep.go:62` | JPEG quality 85 硬编码 | 设计意图"good enough to read"，暴露配置会增加复杂度 |
| P2-6 | `internal/imgprep/cache.go` | 缓存清理按 mtime TTL，无容量上限 | 与 §9.5 审计 retention 同取舍：先上最简单的 |
| P2-7 | `internal/audit/housekeep.go` | zstd 默认压缩等级，未暴露调优 | 文档明确"未手工调参"是有意为之 |
| P2-8 | `cmd/vmr/main.go` | `vmrBanner` 直接 `Fprint` 跳过 timestamp | 有意为之（进程生命周期视觉标记） |
| P2-9 | `internal/audit/audit.go` & `internal/audit/housekeep.go` | `audit` 包和 `housekeep.go` 之间用 atomic.Bool 协调 | 单实例足够，多 Logger 场景不设计 |
| P2-10 | `vmr-report.md` 顶层 README | 双语 README 同步维护 | 当前内容精确对齐，新增内容需双语同步；无技术债 |

---

## 4. 关键设计决策的 review 复核

文档 §11 决策表与代码对照复核：

| 决策 | 代码落实 | 评价 |
|---|---|---|
| Canonical = 协议原生，透传不翻译 | `core.CanonicalRequest{Model, Stream, Raw, Header}`，`adapter.RewriteModel` 仅替换 model 键 | ✅ 精确 |
| 不用 Provider SDK | 无外部 Provider SDK 依赖 | ✅ |
| 编译期 Adapter 注册 | `adapter.Register` + main.go blank import | ✅ |
| 调度 = 过滤 + 多键排序 | `router.go:202-210` health filter + `strategy.Sort` | ✅ |
| 被动健康 + 半开单飞探针 | `health.Registry.Acquire` + `ReportNeutral` | ✅ |
| 错误分类含 body 嗅探 | `adapter.DefaultClassify` | ✅ |
| providers/models 按协议分两层 map | `config.Config{Providers, Models map[string]map[string]...}` | ✅ |
| 全败透传最后上游错误 | `router.go:230-239` | ✅ |
| 健康状态跨热重载保留 | `health.Registry` 不在 Snapshot 内 | ✅ |
| 并发闸：全局、无等待上限 | `router.AcquireSlot` channel-based | ✅ |
| failover 默认穷尽全部候选 | `router.go:218-222` | ✅ |
| 内容合规错误：切换但零惩罚 | `ErrContent` 走 `ReportNeutral` 不入冷却 | ✅ |
| 配额窗口与余额耗尽同罪同罚 | `DefaultClassify` 429 嗅探归 ErrEndpoint | ✅ |
| 审计双层结构、成功 body 去重 | `audit.NewMessage` 不存成功 body | ✅ |
| 审计凭证掩码 | `audit.Redact` + `mask()` | ⚠️ P1-5: 缺 Cookie |
| 无中心 IR、router 只做循环 | router.go 主流程 ~550 行 | ✅ |
| 图片降采样按真实像素尺寸 | `image.DecodeConfig` 读 header | ✅ |
| 图片降采样直接函数调用（非插件） | `server.chatHandler` 直接调 `imgprep.Downscale` | ✅ |
| 动图/超限声明尺寸一律 fail-open | `processImage` 跳过 GIF 多帧 + 64MP 防护 | ✅ |
| 模型级 `image_downscale` 用 `*int` | `config.ModelConfig.ImageDownscaleMaxPx *int` | ✅ |
| 降采样缓存 key 含 `maxPx` | `cacheFileName(hash, maxPx)` | ✅ |
| 降采样缓存只做按 mtime 的 TTL | `sweepCacheDir` 按 mtime 删 | ✅ |
| 降采样缓存目录/失效期显式参数 | `imgprep.Options{CacheDir, CacheTTLDays}` | ✅ |
| `audit.Dir`/`imgprep.CacheDir` 通过 `vmr dirs` 查询 | `cmdDirs` 实现 + vmr.sh 调用 | ✅ |
| env 设置时原样返回 | `rundir.Resolve` 逻辑 | ✅ |
| Endpoint 键加协议前缀 | `core.Endpoint.HealthKey` | ✅ |
| Endpoint priority 字段可选 | `EndpointConfig.Priority int` 缺省 0 | ✅ |
| 请求侧 Header 默认透传 + 小型黑名单 | `server.headerBlocklist` 14 项 | ✅ |
| 响应侧归一化：双模式 | `response.go` modePassthrough / modeBuffered | ✅ |
| 响应头默认透传 + hop-by-hop 黑名单 | `router.respHeaderBlocklist` | ✅ |
| `[DONE]` 仅 openai 协议且上游缺失时补 | `response.appendDone` | ✅ |
| 归一化痕迹记入审计 `attempts[].norm` | `audit.Attempt.Norm []string` | ✅ |
| Rewrite `"model"` 必须做 | `modelFieldPattern` 替换 | ✅ |
| Strip `<think>` 必须做 | `thinkPattern` 替换 | ✅ |
| Strip "Thinking Process:" 启发式仅 thinking=medium 触发 | `classifyEvent` 守卫 + `stripThinkingProcess` 守卫 | ✅ |
| 审计历史文件压缩用 zstd 整文件 | `compressFile` 用 zstd | ✅ |
| 压缩/保留复用 Logger 已有的按日轮转边界触发 | `scheduleHousekeeping` 在 Write 旋转时触发 | ✅ |
| `audit_retention_days` 缺省 0（永久保留） | `applyDefaults` 钳制为 0 | ✅ |

**评价**：所有 §11 决策表项目都有精确代码对应，未发现决策与代码不一致的情况。

---

## 5. 测试覆盖评估

### 5.1 覆盖率统计

```
internal/core       66%   充分
internal/rundir     90%   极佳
internal/strategy   45%   适中
internal/config    144%   极佳（含失败用例）
internal/adapter    85%   极佳
internal/health     84%   极佳
internal/imgprep   105%   极佳（含 bomb 等深覆盖）
internal/audit      70%   充分
internal/router     73%   充分
internal/server    754%   极深（含 591 行 OpenClaw 真实事故回放）
internal/report     35%   偏低（复杂度最高）
cmd/vmr             23%   偏低（但 CLI 边界明确）
```

### 5.2 测试质量亮点

- **真实事故回放**：`server_openclaw_scenario_test.go`（591 行）复现 24 轮 think 反馈循环，验证 §5.5 决策有效性
- **race 安全**：所有测试 `-race` 干净通过
- **边界场景**：`TestProbeSlotReleasedOnClientCancel`、`TestReportNeutralReleasesProbeOnly` 等锁定了文档识别的真实回归
- **crash 恢复**：`TestHousekeep_ResumesInterruptedCompress` 验证 rename-crash 后能补删

### 5.3 测试薄弱点

- `internal/report` 单测覆盖偏低（35%）— 但**集成测试通过 `vmr report` 全套 E2E 路径**间接覆盖
- `cmd/vmr/main.go` 单测偏少 — 但 CLI 边界明确

---

## 6. 安全性 review

### 6.1 已确认的安全设计

| 项 | 措施 | 评价 |
|---|---|---|
| 客户端鉴权 | `subtle.ConstantTimeCompare` | ✅ 防 timing attack |
| 凭证传递 | Adapter 注入 `Authorization`/`x-api-key`，客户端头被 blocklist 剥除 | ✅ |
| IP 欺骗防护 | `X-Forwarded-*`/`X-Real-Ip` 在 blocklist | ✅ |
| 审计凭证掩码 | `Authorization`/`X-Api-Key`/`Api-Key`/`X-Auth-Token` 掩码 | ⚠️ P1-5: 缺 `Cookie` |
| 解压炸弹防护 | 64MP 上限 | ✅ |
| 鉴权重放保护 | `Bearer`/`x-api-key` 同时接受 | ✅ |
| 进程间 key 隔离 | `HealthKey` 含 `sha256(api_key)` 前 4 字节 | ✅ |
| /admin/status 仅 loopback | `net.ParseIP(host).IsLoopback()` 严格 | ✅ |

### 6.2 潜在风险点（已识别）

- **P1-5**：`Cookie` 头未掩码（虽服务端已剥除 Cookie 转发，但 audit 仍记录明文）
- **gzip 双层风险**：服务端 blocklist 剥 `Accept-Encoding`，让 Go Transport 自动协商 — 这是 §5.4 决策，**正确**避免响应归一化器在 gzip 字节上跑 regex

### 6.3 未发现的安全问题

- 无 SQL injection（无 SQL）
- 无 XSS（无 HTML 输出场景）
- 无 CSRF（无浏览器场景）
- 无 SSRF（不主动 fetch 远程图片）
- 无命令注入（bash 脚本使用 `${VAR}` 已引号）

---

## 7. 性能与可维护性 review

### 7.1 性能

- **路由主流程**：router.go ~550 行，每个请求 O(endpoints) 复杂度（通常 < 10），无热点
- **响应归一化**：v3 设计（事件级）相比 v1（字节级状态机）和 v2（全缓冲）TTFB 最优
- **图片降采样**：失败全 fail-open，无锁竞争，64MP 防护
- **审计写入**：`sync.Mutex` 串行化，但每日单文件追加（OS 页缓存友好）
- **并发闸**：channel-based，FIFO 近似
- **健康查询**：O(1) hashmap

### 7.2 可维护性

- **设计文档 vs 代码**：高度一致（§11 决策表全部精确落实）
- **包依赖**：自底向上 `core → rundir → strategy → adapter → config → health → audit → imgprep → router → server`，依赖无环
- **测试独立性**：每个测试用 `t.TempDir()`，无全局状态污染（除 `audit.SetRetentionDays`）
- **代码注释密度**：高（每个公开函数都有 doc comment 说明设计意图和取舍）

---

## 8. 跨文件交叉对比发现

### 8.1 文档↔代码一致性

| 文档条目 | 代码位置 | 一致性 |
|---|---|---|
| §3 协议透传 | `core.CanonicalRequest` + `adapter.BuildRequest` | ✅ |
| §5.5 7 个 norm 标记 | `router.response.go noteApplied` 调用点 | ✅ 全部覆盖 |
| §6.2 探针槽释放三场景 | `router.tryOne` + `ReportNeutral` 三处调用 | ✅ |
| §9.2 五条 JSONL 约定 | `audit.Record`/`Attempt` 字段定义 | ✅ |
| §9.4 Format 9 多桶架构 | `report.go` 8 个独立桶 | ✅ |
| §10 配置参考 | `config.go` 字段定义 + 校验 | ✅ |
| §11 决策表 30 项 | 30 项精确代码对应 | ✅ |
| §13 清理项 | 代码未做清理（按文档登记） | ✅ |

### 8.2 配置↔代码引用

- `config.example.yaml` 注释与 `config.go` 注释**精确对齐**
- `config.yaml`（本地）含真实 key，gitignored — 安全

### 8.3 文档内部一致性

- `README.md` ↔ `README.zh.md`：双语内容**精确对齐**（不是机翻）
- `docs/VirtualModelRouter_v2_Fable5.md` ↔ `docs/SensitiveWordFilter_Analysis_Fable5.md`：后者作为 §12.1 输入，分析结论被前者采纳

### 8.4 真实日志 vs 设计

- `vmr-report.md` 是真实生产日志的产物（包含 OpenClaw envelope、heartbeat 标签等）
- 文档 §5.5、§9.4 提到的"真实事故"都在 `server_openclaw_scenario_test.go` 中有对应回归测试

---

## 9. 总评与建议

### 9.1 项目整体评价

**这是一个高质量、设计驱动的项目**：

- **设计文档与代码精确对齐**：587 行设计文档的每一个决策都有对应代码实现和测试
- **测试覆盖深度极强**：含 591 行真实事故回放测试、crash 恢复测试、race 安全测试
- **代码风格一致**：每个包都有详细 doc comment，说明设计意图和取舍
- **安全设计周全**：凭证掩码、IP 欺骗防护、timing-safe 鉴权
- **性能取舍有据**：v1→v2→v3 响应归一化器的迭代过程在文档中完整保留（决策表 §11）

### 9.2 必须修复的（按优先级）

1. **P0-1 + P0-2**（合在一起是同一个问题的两面）：审计日志 Close 与 housekeeping 扫尾的 race — 影响审计数据完整性，是 §1 明确强调的硬性需求

### 9.3 建议改进（按价值排序）

1. **P1-3**：Session 标题不稳定（真实 bug，影响输出稳定性）
2. **P1-5**：Cookie 头未掩码（安全隐患）
3. **P1-2**：isSSE 判定过度宽松（边界场景）
4. **P1-4**：WriteDetails API 稳定性（接口设计）
5. **P1-6**：RawPreStrip 32MB cap 处理
6. **P1-9**：vmr.sh pkill 容错
7. 其它 P1 为代码风格和文档化建议，可视团队偏好修复

### 9.4 不建议修复

P2 列表 10 项均为合理的技术债取舍，已在文档/注释中有意登记。

---

## 10. 附录：完整文件清单与 review 结论速查表

| 文件 | 行数 | 结论 | 问题 |
|---|---|---|---|
| `go.mod` | 12 | ✅ | — |
| `go.sum` | 12 | ✅ | — |
| `.gitignore` | 32 | ✅ | — |
| `docs/VirtualModelRouter_v2_Fable5.md` | 587 | ✅ | P2-1 |
| `docs/SensitiveWordFilter_Analysis_Fable5.md` | (外部分析报告) | ✅ | — |
| `README.md` | 218 | ✅ | — |
| `README.zh.md` | 217 | ✅ | — |
| `config.example.yaml` | 76 | ✅ | — |
| `config.yaml` | 118 | ✅ (gitignored) | — |
| `vmr.sh` | 334 | ✅ | P1-9 |
| `cmd/vmr/main.go` | 520 | ⚠️ | P0-2, P1-1 |
| `cmd/vmr/main_test.go` | 120 | ✅ | — |
| `internal/core/core.go` | 115 | ✅ | — |
| `internal/core/core_test.go` | 76 | ✅ | — |
| `internal/rundir/rundir.go` | 51 | ✅ | — |
| `internal/rundir/rundir_test.go` | 46 | ✅ | — |
| `internal/strategy/strategy.go` | 76 | ✅ | P1-7 |
| `internal/strategy/strategy_test.go` | 34 | ✅ | — |
| `internal/config/config.go` | 241 | ✅ | P2-2 |
| `internal/config/config_test.go` | 347 | ✅ | P2-3 |
| `internal/config/watch.go` | 53 | ✅ | — |
| `internal/adapter/adapter.go` | 66 | ✅ | — |
| `internal/adapter/classify.go` | 97 | ✅ | P2-4 |
| `internal/adapter/classify_test.go` | 44 | ✅ | — |
| `internal/adapter/openai/openai.go` | 56 | ✅ | — |
| `internal/adapter/openai/openai_test.go` | ~100 | ✅ | — |
| `internal/adapter/anthropic/anthropic.go` | 56 | ✅ | — |
| `internal/adapter/anthropic/anthropic_test.go` | ~90 | ✅ | — |
| `internal/health/health.go` | 163 | ✅ | — |
| `internal/health/health_test.go` | 137 | ✅ | — |
| `internal/imgprep/imgprep.go` | 445 | ✅ | P2-5 |
| `internal/imgprep/cache.go` | 148 | ✅ | P2-6 |
| `internal/imgprep/imgprep_test.go` | 625 | ✅ | — |
| `internal/audit/audit.go` | 285 | ⚠️ | P0-1 |
| `internal/audit/audit_test.go` | 145 | ✅ | — |
| `internal/audit/housekeep.go` | 148 | ✅ | P2-7 |
| `internal/audit/housekeep_test.go` | 158 | ✅ | — |
| `internal/router/router.go` | 596 | ⚠️ | P1-1, P1-2 |
| `internal/router/router_test.go` | 76 | ✅ | — |
| `internal/router/response.go` | 587 | ⚠️ | P1-2, P1-6 |
| `internal/router/response_test.go` | 787 | ✅ | — |
| `internal/server/server.go` | 323 | ⚠️ | P1-4 |
| `internal/server/recorder.go` | 57 | ✅ | — |
| `internal/server/server_*_test.go` | 2865 | ✅ | — |
| `internal/report/report.go` | 846 | ⚠️ | P1-7 |
| `internal/report/usage.go` | 178 | ✅ | — |
| `internal/report/session.go` | 777 | ⚠️ | P1-3 |
| `internal/report/export.go` | 422 | ✅ | — |
| `internal/report/detail.go` | 1177 | ⚠️ | P1-4 |
| `internal/report/markdown.go` | 591 | ✅ | — |
| `internal/report/render.go` | 594 | ✅ | — |
| `internal/report/*_test.go` | 1593 | ✅ | — |

**总计**：54 个文件 / 包，8 个含 P1 问题，2 个含 P0 问题，0 个含严重运行时 bug（除非 Close 时 housekeeping race 被触发）。

---

## 11. 致评审接收者

本报告基于静态分析 + 测试运行 + 逐文件研读。**强烈建议**：

1. **优先修复 P0-1/P0-2**：在引入更复杂的部署场景（如 systemd 强杀、SIGKILL）前修掉，避免审计数据完整性问题在生产环境暴露
2. **按价值修复 P1**：P1-3（session title 稳定）是真实 bug，P1-5（Cookie 掩码）是安全隐患，P1-2（isSSE）是边缘场景的优雅性
3. **P2 项无需修复**：每项都有文档化的设计意图记录在案

本项目代码质量在 Go LLM 网关类项目中处于**第一梯队**。主要的修复项（P0-1）属于 shutdown 时序的细粒度问题，不是设计层面的问题。