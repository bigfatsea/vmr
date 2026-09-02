<!-- Ver 2026-09-02, by sub-agent S5 (D6+D7 Infrastructure, Protocols & Tools) -->

# Domain Review 报告：D6+D7 基础设施与协议/工具域

> 审查范围：
> - D6 基础设施与门禁域：`internal/audit/`、`internal/logtee/`、`internal/imgprep/`、`internal/rundir/`、`internal/buildinfo/`、`internal/sysinfo/`、`internal/archtest/`
> - D7 核心类型与协议/工具域：`internal/core/`、`internal/fmtutil/`、`internal/tokenutil/`、`internal/jsonscan/`、`internal/adapter/`（含 openai / anthropic / openairesponses 三子包）、`internal/diagnose/`、`internal/replay/`
> 审查基线：`main@4e0d962`
> 审查模式：源码级只读审查（带文件与行号锚点，含 `-race` 与 Fuzz 真实性验证）

---

## 0. 领域审查概览

| 模块 | 核心职责 | 状态 | 关键结论 |
| --- | --- | --- | --- |
| `internal/audit/` | JSONL 两层审计记录落盘、zstd 压缩与保留、文件锁与只读共享 | ✅ 健壮 | 0600/0700 权限严格；Unix flock 防双实例破坏；legacy 协议归一化仅在读侧拦截；写路径内存池与无锁化设计优秀 |
| `internal/jsonscan/` | 零分配字节级扫描与 splice 改写引擎 | ⚠️ 发现缺陷 | 整体扫描原语与回退完备，但 `RewriteModel` 使用 `strconv.AppendQuote` 导致非 ASCII/非 UTF-8 字符转义为非法 JSON（Fuzz 可复现） |
| `internal/adapter/` | 协议适配器接口、注册表、三协议子包、错误分类与指纹提取 | ✅ 健壮 | 注册表 lock-free load + mutex 写保护；三协议独立子包编译期注册；错误嗅探词表层级分明；Responses `input` 数组与角色改写对齐规范 |
| `internal/imgprep/` | 探测与请求内联图片降采样、防炸弹闸门、磁盘缓存 | ✅ 健壮 | 16MP 防炸弹闸门有效；Fail-open 恢复保护；按日节流的磁盘 TTL 与容量双重淘汰；内存上界与 32 位溢出契合已知问题声明 |
| `internal/core/` | 跨半区领域模型、准入规则与枚举定义 | ✅ 健壮 | 零内部依赖契约成立；豁免清单完备；`Rate` 显式以 `*float64` 严格区分 unknown vs 0.0 free |
| `internal/fmtutil/` `tokenutil/` `logtee/` | 格式化展示、零分配 token 估算、控制台日志广播 bus | ✅ 健壮 | `tokenutil` 细致覆盖 CJK 全角标点防虚高；`fmtutil.DisplayZone` 统一人读出口；`logtee` 环形缓冲 + 有界丢行保证主热路径无阻塞 |
| `internal/archtest/` | 架构门禁（包依赖、行数/函数预算、文档引用、eval 编译） | ✅ 健壮 | 门禁执行力强；负向测试完备；函数长度豁免键同名方法共用问题验证与 KNOWN_ISSUES §2.8 吻合 |
| `internal/diagnose/` `internal/replay/` | 连通性测试与精确请求重放 | ✅ 健壮 | 100% 复用生产链路 `BuildRequest` 与 `NewUpstreamClient`；replay 严格清除打码占位符并按真实口径计量配额 |

---

## 一、各任务审查详细结论

### 任务 1: 审计日志（`internal/audit/`）

- **两层记录与稳定性** (`audit.go:42-83`):
  - `Record` 采用双层结构：`Client` 记录调用方与 VMR 交互（保留完整原始 `Request.Body`），`Attempts` 记录 upstream 尝试。成功 attempt 响应 body 缺省不存（与 `Client.Response.Body` 相同），通过 `Norm` / `RawPreStrip` / `ObservedModel` 精确记录改动，符合设计文档 §9.2 规范。
  - `Facts` (`audit.go:73`) 保持为 `Client.Request` 的兄弟字段，在 `server.go` 计算后单次落盘，不反向污染客户端原始报文。
- **文件权限与目录安全** (`audit.go:230, 297`, `housekeep.go:171`, `lock_unix.go:24`):
  - 目录创建统一 `0700` (`os.MkdirAll(dir, 0o700)`)；审计文件与锁文件统一 `0600` (`os.OpenFile(..., os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)`)，无权限放宽隐患。
- **进程排他锁与并发保护** (`lock_unix.go:20-41`, `lock_windows.go:12-21`):
  - Unix 平台使用 `syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)` 锁定 `.vmr-audit.lock`，防止双进程并发写入同一 JSONL 或交错进行 zstd 压缩导致归档破坏。被锁时精确提取持有者 PID。
  - Windows 平台按设计声明（KNOWN_ISSUES §1.1）降级为 no-op，依赖临时文件名防归档冲突。
- **日志写入与并发流水线** (`audit.go:259-307`):
  - `writeBufPool` 结合 `sync.Pool` 在锁外执行 `json.NewEncoder(buf).Encode(rec)`，CPU 密集型序列化完全并发；仅在最终 `f.Write` 与跨日切换时获取 `l.mu.Lock()`。
  - 内存防泄露：`buf.Cap() <= maxPooledWriteBufCap` (1MB) 才放回池中，避免超大 multimodal 请求导致池内存常驻。
- **zstd 压缩与保留机制** (`housekeep.go:48-188`):
  - 轮转触发：通过 `scheduleHousekeeping()` 以 `atomic.Bool` 保证后台仅有一个 sweep 协程运行 (`audit.go:245`)。
  - 压缩原子性：`compressOne` 写入 `.audit-compress-*.tmp` 临时文件，`Sync()` 落地后 `os.Rename`，确认落地后再删原文件。若因 crash 导致 `.zst` 与源文件同时存在，重启时自动续跑清理源文件 (`housekeep.go:110`)。
  - 孤儿临时文件清理：`isStaleTemp` 自动清理超过 24 小时的 `.audit-compress-*` 和 `.vmr-quota-*` 临时文件。
- **共享读取与 Legacy 协议归一化** (`read.go:25-115`, `legacy_protocol.go:17-48`):
  - `OpenLogFile` 透明支持 `.jsonl` 与 `.jsonl.zst`；`scanLines` 采用 1MB 缓冲与 `MaxLogLine = 128MB` 上限，超限行安全丢弃 (`onSkip`) 而不崩溃。
  - `LineAt` 在找到目标 1-based 行号后立即中断解压/扫描，大幅降低坐标定位耗时。
  - `Record.UnmarshalJSON` (`audit.go:88`) 是唯一的历史兼容咽喉点，自动将旧日志中的 `"openai"` / `"anthropic"` 映射至 `core.ProtocolOpenAICompletions` / `core.ProtocolAnthropicMessages`，写路径绝不调用。

---

### 任务 2: 字节扫描与 splice 引擎（`internal/jsonscan/`）

- **扫描原语正确性** (`scan.go:12-140`):
  - `SkipJSONWS`: 针对负数入参主动饱和至 `len(b)`，避免越界 panic 或误从 0 扫描。
  - `IndexUnescapedQuote`: 利用 `bytes.IndexByte` 配合连续反斜杠奇偶性判定 (`n%2 == 0`)，实现毫秒级跳过 MB 级 JSON 字符串。
  - `SkipJSONValue`: 针对标量/字面量扫描加入 `i == start` 零进展防护 (`scan.go:120`)，彻底杜绝遇到非法分隔符时的死循环。
- **结构化遍历与边界** (`walk.go:19-141`):
  - `TopLevelValues`: 仅解析顶层键，嵌套在 messages/tools/metadata 内的同名键完全不受影响。遇到非合法对象结构时安全返回 `ok=false`，触发调用方 generic 回退。
  - `WalkArrayElements` 与 `ElementRole`: 严格校验 `inRange(raw, start, end)`，防止读取逃逸出指定窗口。
- **改写与回退机制** (`rewrite.go:26-215`):
  - `RewriteModel` / `RewriteStream`:
    - 快速路径：扫描顶层键范围后执行 `spliceValues`，当目标值相同时零拷贝返回；当发生替换时，预计算精确容量 `len(raw)+extra`，单次分配拼接完成。
    - 回退路径：`rewriteModelGeneric` 使用 `map[string]json.RawMessage` 兜底；对 `raw` 为 JSON literal `null` 做了 `m == nil` 显式防 panic 校验 (`rewrite.go:94`)。
  - `RewriteRoles` / `RewriteInputRoles`:
    - 分别针对 Chat Completions 的 `"messages"` 数组与 Responses 的 `"input"` 数组进行逐元素扫描与 `"role"` 字段原地替换。
    - 预计算全部替换项的长度增量并一次性 forward splice，避免多次重分配。
- **⚠️ 发现缺陷（D5-01）** (`rewrite.go:27`):
  - `RewriteModel` 在构造替换字节时使用了 `mv := strconv.AppendQuote(make([]byte, 0, len(model)+2), model)`。
  - `strconv.AppendQuote` 遵循 Go 语言字面量转义标准（包含 `\xNN`、`\a`、`\v` 等），而不是 RFC 8259 JSON 字符串规范（仅支持 `\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX`）。
  - 若 `model` 中包含非法 UTF-8 字节或非打印控制字符，`RewriteModel` 会拼装出包含 `\xba` 等非法转义序列的 JSON，造成上游 400 拒绝。该缺陷已被 `FuzzRewriteModel` 捕获。
  - 详细分析与修复建议见第二节。

---

### 任务 3: 协议适配器（`internal/adapter/` 及其子包）

- **Adapter 接口与注册表** (`adapter.go:20-72`):
  - 接口定义极简且职责正交：`Protocol()`, `ResolveURL()`, `BuildRequest()`, `ClassifyError()`。
  - 注册表采用 `atomic.Pointer[map[string]Adapter]` 实现请求热路径无锁加载 (`Get()` / `Names()`)；写路径通过 `registerMu sync.Mutex` 保护 copy-on-write，避免并发 `Register` 丢失（`-race` 验证通过）。
- **统一出站请求构建** (`request.go:25-50`):
  - `BuildUpstreamRequest` 统一收敛模型改写、角色重映射、Header 拷贝、凭证注入与 `Content-Type: application/json`。
  - 凭证注入严格从 `ep.APIKey` 读取，阻断客户端传入的 VMR 自身 Key。
- **协议差异化实现**:
  - `openai/openai.go`: 绑定 `chatCompletionsPath = "/chat/completions"`，注入 `Authorization: Bearer <key>`，使用 `RewriteRoles`。
  - `anthropic/anthropic.go`: 绑定 `messagesPath = "/messages"`，注入 `x-api-key: <key>`，使用 `RewriteRoles`。`ClassifyError` 特判 529 (`overloaded_error`) 归类为 `ErrTransient`。不默认伪造 `anthropic-version`，保持原样透传。
  - `openairesponses/openairesponses.go`: 绑定 `responsesPath = "/responses"`，注入 `Authorization: Bearer <key>`，使用 `RewriteInputRoles`（支持 top-level `input` 数组）。
- **错误分类与词表层级** (`classify.go:48-124`, `classify_hints.go:8-85`):
  - `errorSnippet`: 优先解构 `error.message` / `error.type` / `detail`，避免提取 verbose debug 噪音。
  - 优先级顺序严格：
    1. 451 -> `ErrContent`；401 -> `ErrAuth`；408 -> `ErrTransient`；
    2. 403 / 402 / 404 先走 `contentHint` 嗅探（防止违规错误误降权健康端点）；
    3. 429 检查 `balanceExhaustedHint`（欠费当 `ErrEndpoint` 长退避，普通限流当 `ErrRateLimit`）；
    4. 4xx 依次检查 `contentHint` -> `balanceExhaustedHint` -> `authHint` (OAuth 错误) -> `contextLimitHint` (排除 `maxOutputHint` 后识别超长上下文) -> 缺失模型 -> `upstreamHint` (网关级报错) -> `vendorQuirkHint` (`ErrQuirk` 零惩罚切换) -> 兜底 `ErrClient`。
- **会话指纹与 TopLevelProbe** (`fingerprint.go:37-235`):
  - `SessionFingerprint`:
    - OpenAI / Anthropic: 高效抓取 system prompt 与首条用户消息的 MD5。
    - Responses: `responsesSessionFingerprint` 支持 `instructions` 顶层字段与 `input` 数组内 `role:"system"` / `role:"developer"` 的多重指令合并哈希，以及 bare string input。
  - `TopLevelProbe`: 单遍扫描完成 `model`、`stream`、`tools`（判断是否非空数组）三字段提取，代替传统的两次反序列化，null 值安全忽略，后写覆盖。

---

### 任务 4: 图片降采样（`internal/imgprep/`）

- **探测与解码安全闸门** (`imgprep.go:37-43, 275-285`):
  - `HasImageMarker` 在入口以 `bytes.Contains` 快速短路非图片请求（`"image` 或 `input_image`），零内存与 JSON 解析开销。
  - 降采样前调用 `image.DecodeConfig` 仅读图片头元数据（宽、高、格式）。
  - 防炸弹闸门：`cfg.Width*cfg.Height > maxDecodePixels (16_000_000)` 自动放弃降采样，原样透传。
  - 健壮性：`Downscale` 顶层 `defer recover()` 捕获任意解码器底层 panic 并记录 stderr，保证请求 fail-open 交付。
- **已知问题验证 (§2.17 / §2.49)**:
  - 16MP 对应解码 RGBA 缓冲区峰值约 64MB，与文档单请求 32MB 目标预算存在量纲差异（KNOWN_ISSUES §2.17 吻合，逐张解码逐张释放，单图不累加）。
  - `cfg.Width*cfg.Height` 表达式为 `int` 相乘，在 32 位平台存在溢出风险（KNOWN_ISSUES §2.49 吻合，当前 64 位平台安全）。
- **磁盘缓存与双重淘汰** (`cache.go:20-135`):
  - 缓存 Key：`sha256(data) + "-" + maxPx + ".jpg"`，确保不同模型不同分辨率不冲突。
  - 命中刷新：命中时调用 `os.Chtimes` 刷新 mtime。
  - 节流异步清理：`maybeSweepCache` 结合 `sync.Map` 保证每目录每自然日最多触发一次清理协程。
  - 淘汰逻辑：`sweepCacheDirWithCap` 清理 1 小时前残留的 `.tmp-*`；按 `CacheTTLDays` 清理过期文件；当超过 `defaultCacheCapBytes` (50MB) 时，按 mtime 升序淘汰最老缓存。

---

### 任务 5: 核心类型与工具（`internal/core/`、`fmtutil`、`tokenutil`）

- **`core` 准入规则与豁免清单** (`core.go:1-25, 145-200`):
  - 严格保持零内部依赖（`zeroInternalDepPackages` 守护）。
  - 仅容纳双半区公共类型：`CanonicalRequest`, `RequestFacts`, `ErrorClass`, `Endpoint`, `Limit`, `TokenWeights`, `Rate`, `PricingSpec`, `QuotaSpec`。
  - 显式豁免符号：`Endpoint.HealthKey` / `Name` / `Freeze`（值对象属性及预计算方法）、`StickyBackstopTTL`（配置层校验常量），无违规外溢。
- **类型系统中的 Unknown vs Free 区分** (`core.go:270-285`):
  - `core.Rate` 中的价格字段为 `*float64`（`InFresh`, `CacheRead`, `CacheWrite`, `Out`）。
  - 明确契约：`nil` 表示未知（Unknown），`0.0` 表示免费（Free），类型系统严格阻断歧义。
- **展示与时区工具（`internal/fmtutil/`）** (`fmtutil.go:14-160`, `timezone.go:10-25`):
  - 统一人读格式化：`FmtBytes`, `FmtDuration`, `FmtSeconds`, `FmtPercent`, `FmtTokens`, `FmtTokensPlain`, `FmtTokensCompact`, `CapStr`, `SortedKeys`, `ModelLabel`。
  - `DisplayZone = time.Local`：全系统人读报表与日志时区唯一出口，原始审计记录保持物理时间戳。
- **Token 估算引擎（`internal/tokenutil/`）** (`tokenutil.go:10-140`):
  - 零堆分配线性回归模型：`tokens ≈ 0.488*Sym + 0.206*Eng + 0.746*Digits + 0.507*CJK + 0.043*Space + 1.830*Other`。
  - `IsCJK` 完备覆盖中日韩统一表意文字、扩展 A-G、假名、谚文及 CJK 全角符号与标点（`0x3000-0x303F`, `0xFF00-0xFFEF`），杜绝全角中文标点落入 OtherChars 导致估算系统性虚高。

---

### 任务 6: archtest 门禁（`internal/archtest/`）

- **导入依赖与零依赖叶子包门禁** (`import_boundaries_test.go:20-155`):
  - `zeroInternalDepPackages` 守卫：`core`, `fmtutil`, `i18n`, `jsonscan`, `logtee`, `tokenutil` 严格无任何内部依赖。
  - `forbiddenImports` 双向拦截：分析层（`report`, `story`, `ctxgraph`, `taskseg`, `reqdetail`）绝不依赖路由运行时（`router`, `server`, `config`）；`adapter` 绝不依赖 `router`/`server`；`router` 绝不反向依赖 `server`。
- **代码体量与复杂度预算** (`file_sizes_test.go:20-95`, `func_sizes_test.go:20-140`):
  - 文件默认上限 700 行，函数默认上限 120 行，配合白名单精准盯防代码回潮。
  - 自动检查无用/陈旧豁免条目。
- **已知问题验证 (§2.8)**:
  - `func_sizes_test.go:88` 使用 `key := rel + ":" + fd.Name.Name`，未包含接收者类型。当同一文件中存在多个同名方法时（如 `ingest.go` 中不同类型的 `Ingest`），豁免额度将被共享，完全印证 KNOWN_ISSUES §2.8。
- **文档与注释引用保真度** (`doc_refs_test.go:20-270`):
  - 覆盖 `.md` 与生产 `.go` 源码注释，正则校验内部包路径、源文件路径、Markdown 链接及 `pkg.Symbol` 符号有效性。
  - `TestArchitecture_DocReferences_Negative` 保证断链与失效引用 100% 触发报错。

---

### 任务 7: 诊断与重放（`internal/diagnose/`、`internal/replay/`）

- **诊断工具管线（`internal/diagnose/`）** (`diagnose.go:70-280`):
  - 阶段划分：Phase 1 配置校验 -> Phase 1b 一致性扫描（阻断严重错误） -> Phase 2 代理/DNS/TLS 环境探测 -> Phase 3 真实最小连通性探活 -> Phase 4 最终路由预览。
  - 代理感知：当配置了代理时，跳过对远端 host 的直连 DNS/TLS，仅探测代理可达性，避免假阳性误报。
  - 探针验证：利用 `probe.RoleCompatRequest` (OpenAI 协议测试 "developer" 角色及 role_map 兼容性) 与 `probe.ResponsesRequest`，校验 Nonce 真实回显，防网关假 200。
  - 并发受控：`checkConcurrency = 8` 有界并发，预分配结果槽位，保证输出确定性。
- **重放工具管线（`internal/replay/`）** (`replay.go:60-260`):
  - 定位能力：支持 `-req` (坐标)、`-ts` (毫秒时间戳)、`-line` (行号) 三种互斥定位，支持 `-print` 原样查看。
  - 报文保真与安全：
    - `recordView` 采用 `json.RawMessage` 保存请求 body，避免 `map[string]any` 导致的类型失真。
    - `replayHeaders` 在 `FilterClientHeaders` 基础上额外执行 `audit.IsCredentialHeader` 剔除，坚决防止将审计打码占位符 (`***c1d4`) 作为真实 Key 发往上游。
  - 额度闭环：重放成功（<400）时自动调用 `chargeReplay` 计入 `vmr-quota.json`，与生产路由记账一致（锁存在时自动退化为内存记账，不破坏守护进程状态）。

---

## 二、领域级问题汇总

### 1. 中危问题（待修复缺陷）

#### [中危] `jsonscan.RewriteModel` 对非标准/非 UTF-8 字符转义产出非法 JSON
- **源码锚点**：`internal/jsonscan/rewrite.go:27`
- **问题描述**：
  ```go
  func RewriteModel(raw json.RawMessage, model string) ([]byte, error) {
      mv := strconv.AppendQuote(make([]byte, 0, len(model)+2), model)
      ...
  }
  ```
  `strconv.AppendQuote` 是 Go 字符串字面量转义器。当字符串包含非 ASCII 控制符（如 `\x00`、`\a`、`\v`）或非标准 UTF-8 字节（如 `\xba`）时，它会输出 Go 转义序列 `\xba` / `\a`。这些在 RFC 8259 JSON 标准中是非法的（JSON 仅支持 `\u00ba` 或特定转义符）。
  当执行 `RewriteModel` 时，splice 写入的非法转义字符串导致整个出站请求体变为非法 JSON。
- **复现方式**：
  运行 Fuzz 测试即可复现：
  `go test ./internal/jsonscan -run=FuzzRewriteModel -fuzz=FuzzRewriteModel`
  报错：`RewriteModel returned no error but produced invalid JSON: invalid character 'x' in string escape code; raw={"model":[]}, out={"model":"\xba"}`
- **影响范围**：`jsonscan.RewriteModel` 及所有依赖它的出站请求构建链路（`adapter.BuildUpstreamRequest`、`replay.Run`）。常规模型名（如 `gpt-4o`、`claude-3-5-sonnet`）不触发，但若模型名包含非标准 UTF-8 或控制字符时，会导致上游网关报错 400。
- **修复建议**：
  将 `strconv.AppendQuote` 替换为合规的 JSON 转义函数（例如 `jsonscan.MarshalNoEscape(model)`，与 `rewriteRolesInTopLevelArray` 中的做法保持一致）。

---

### 2. 观察项与已知问题跟踪（低危 / 设计声明印证）

#### [低危 / 印证 §2.8] `archtest.funcLineExemptions` 无法区分同文件重名方法
- **源码锚点**：`internal/archtest/func_sizes_test.go:88`
- **验证结论**：`key := rel + ":" + fd.Name.Name` 确实未区分 receiver 类型。当前全仓未出现同名方法超出预算情况，建议在未来需要单独豁免同名方法时引入 `ast.FuncDecl.Recv` 标识。

#### [低危 / 印证 §2.17 & §2.49] `imgprep` 解码内存峰值与 32 位溢出潜在风险
- **源码锚点**：`internal/imgprep/imgprep.go:42, 284`
- **验证结论**：16MP RGBA 瞬时解码约需 64MB 内存；`cfg.Width*cfg.Height` 表达式未转 `int64`。在当前 64 位部署环境下安全，符合已声明的非活跃待定项状态。

---

## 三、对 KNOWN_ISSUES §1.4 包边界相关声明的核实报告

逐条对 `docs/KNOWN_ISSUES.md` §1.4 的声明进行生产代码交叉比对：

| 声明条目 | KNOWN_ISSUES 声明核心 | 源码实现位置 | 验证结果 | 说明 |
| --- | --- | --- | --- | --- |
| **1. 图片元数据结构拷贝** | `imgprep.ImageInfo` 与 `audit.ImageInfo` 保持结构隔离，避免公共工具依赖审计包 | `internal/imgprep/imgprep.go:68`<br>`internal/audit/audit.go:107` | ✅ 100% 一致 | 两处结构体字段完全镜像，`imgprep` 无 `audit` 导入 |
| **2. SSE 状态机分离** | `chatmsg.ReassembleSSE` 与 `respnorm` 状态机分离 | `internal/chatmsg/sse.go`<br>`internal/respnorm/` | ✅ 100% 一致 | 前者服务离线语义，后者服务在线保真转发，`archtest` 严格隔离 |
| **3. StickyTTL 常量归属** | `core.StickyBackstopTTL` 保留在 `core`，避免 `config` -> `sticky` 依赖 | `internal/core/core.go:193`<br>`internal/config/config.go` | ✅ 100% 一致 | 常量定义在 `core`，`sticky` 仅做别名重导出 |
| **4. 协议字段字面量隔离** | `adapter` 不复用 `jsonscan` 导出的协议常量字面量 | `internal/adapter/fingerprint.go:20-27`<br>`internal/jsonscan/rewrite.go:10-15` | ✅ 100% 一致 | 双方独立维护不可变 byte slice 常量，无交叉依赖 |
| **5. 改写引擎归属 jsonscan** | `RewriteModel` / `RewriteRoles` 等改写函数留在 `jsonscan` | `internal/jsonscan/rewrite.go` | ✅ 100% 一致 | 字节扫描与 splice 统一归属 `jsonscan` 包并由 Fuzz 守护 |
| **6. core 准入规则与豁免清单** | `Endpoint.HealthKey`/`Name`/`Freeze` 显式豁免在 `core` | `internal/core/core.go:15-22` | ✅ 100% 一致 | 包注释明确豁免理由，无新增未登记符号 |
| **7. 零依赖叶子包名单** | `probe` / `rundir` / `buildinfo` 不列入 `zeroInternalDepPackages` | `internal/archtest/import_boundaries_test.go:128` | ✅ 100% 一致 | 仅 `core`, `fmtutil`, `i18n`, `jsonscan`, `logtee`, `tokenutil` 六包承诺零依赖 |
| **8. core.go 单文件凝聚性** | 不将 `core.go` 按领域拆散为多个小文件 | `internal/core/core.go` | ✅ 100% 一致 | 聚合在单个文件中，类型关系清晰 |
| **9. imgprep 深度结构解析** | `imgprep` 不与 `jsonscan` 字节扫描强行统一 | `internal/imgprep/imgprep.go:145-210` | ✅ 100% 一致 | 采用 `map[string]json.RawMessage` 完成深度重组 |
| **10. 裸模块名与版本戳** | `go.mod` 保持 `module vmr`，`buildinfo` 依赖 VCS stamp | `go.mod:1`<br>`internal/buildinfo/buildinfo.go:27` | ✅ 100% 一致 | 无虚假 semver，忠实反映 git commit 状态 |

---

## 四、审查小结

D6+D7 基础设施与协议/工具域在架构设计、并发安全、权限控制和错误防护上表现出极高的一致性与严密性：
1. **边界清晰**：`archtest` 门禁有效防护了两半区隔离与叶子包零依赖承诺。
2. **安全韧性**：审计落盘 0600/0700 权限、Unix flock 互斥、凭证掩码清洗机制完整；图片降采样带有防炸弹与全局 panic fail-open 保护。
3. **关键发现**：在 `internal/jsonscan/rewrite.go:27` 发现 `RewriteModel` 错误使用 `strconv.AppendQuote` 导致非标字符产生非法 JSON 转义的中危缺陷，已形成完整证据链与修复建议。
