# vmr — 全量审计报告

> **范围**：仓库 `vmr`（Virtual Model Router），Go 1.25.1 单二进制 LLM 路由代理。
> **方法**：逐文件精读 + 分模块评审；按"目录→文件"顺序记录。最后做三梯队汇总。
> **本文件性质**：纯分析报告，不涉及任何代码改动。
> **审计时间**：2026-07-15（commit 状态见 .git）

---

## 0. 项目结构概览

### 0.1 顶层布局
```
vmr/
├── cmd/vmr/                            # main 入口 + 子命令 + 测试
├── internal/
│   ├── adapter/                        # 协议适配器框架 + 协议族（openai/anthropic）
│   ├── audit/                          # JSONL 审计读写 + 旧文件压缩/清理
│   ├── config/                         # YAML 加载/校验/热加载
│   ├── core/                           # 协议无关的请求预处理（当前体量小）
│   ├── diagnose/                       # `vmr diagnose` 子命令（DNS/TLS/连通性 + 真实小请求）
│   ├── health/                         # 端点健康状态 + 冷却
│   ├── imgprep/                        # 内联图片缩放 + 内容哈希缓存
│   ├── replay/                         # `vmr replay` 子命令（重放审计中的请求）
│   ├── report/                         # `vmr report` 子命令（统计 + session 分组 + 明细导出）
│   ├── router/                         # 端点选择 + 响应归一化
│   ├── rundir/                         # 解析 log/image cache 目录的兜底逻辑
│   ├── server/                         # HTTP 服务 + 校验/限流/审计接入
│   └── strategy/                       # 端点排序策略
├── docs/                               # 设计文档与背景分析
├── config.example.yaml                 # 配置示例（与 config.yaml 同结构）
├── config.yaml                         # 用户本地配置（含真实 key，已 .gitignore）
├── vmr.sh                              # 启动/服务管理脚本（dev + launchd/systemd）
├── README.md / README.zh.md            # 用户文档
├── LICENSE                             # MIT
├── go.mod / go.sum                     # 依赖：yaml.v3, fsnotify, klauspost/compress(zstd), golang.org/x/image
└── vmr-report.{json,md} /              # 历史跑出来的报告
  vmr-requests-index.md /              # （保留供回归对照，不审计语义）
  vmr-requests.jsonl                   # 同上
```

### 0.2 目录统计
- **目录数（含 .git/details/logs）**：2151 个（绝大部分在 `details/`）
- **目录数（剔除 details/.git/logs）**：20 个
- **总文件数**：~2151（其中 2070+ 是 `details/` 中的历史 JSON/MD 审计快照）
- **代码相关文件数（不含 .git/details/logs/构建产物/历史报告）**：78 个 Go 源 + 5 个 docs + 2 个 README + 1 LICENSE + 1 vmr.sh + 1 go.mod + 1 go.sum + 1 config.example.yaml + 1 .gitignore = **~90 个待审计文件**

### 0.3 代码量分布（行数）
| 模块 | 行数 | 主要职责 |
|---|---|---|
| `internal/report` | ~5350 | 报告聚合、session 分组、明细导出（最大模块）|
| `internal/server` | ~3170 | HTTP 服务 + 全量测试（第二）|
| `internal/imgprep` | ~1204 | 内联图片缩放与缓存 |
| `internal/router` | ~1330 | 端点选择 + 响应归一化 |
| `cmd/vmr` | ~1031 | main + 子命令注册 |
| `internal/diagnose` | ~808 | 诊断子命令 |
| `internal/replay` | ~1081 | 重放子命令 |
| `internal/audit` | ~1012 | 审计读写/压缩/保留 |
| `internal/config` | ~1003 | 配置加载与校验 |
| `internal/adapter` | ~747 | 适配器接口 + 两个实现 + classify |
| `internal/health` | ~315 | 健康状态机 |
| `internal/strategy` | ~110 | 排序策略 |
| `internal/rundir` | ~103 | 目录兜底解析 |
| `internal/core` | ~191 | 当前较薄 |

**总计**：~19863 行 Go（不含测试 = 约 ~11000 行生产代码）。

### 0.4 关键技术决策（README 与 docs 摘要）
- **字节级保真**：vmr 不做中间表示（IR）转换；除 model 名改写 + 极少有据可查的 MiniMax-M3 修复外，请求/响应与直连等价。
- **协议族隔离**：`models.<protocol>` 只能引用 `providers.<protocol>`，避免 OpenAI/Anthropic 跨界转换。
- **被动健康 + 冷却**：每错误类（rate limit / 鉴权 / 内容策略）独立冷却；冷过期后单飞探针。
- **Agent session 分组**：在 report 阶段离线规则化完成，不在请求路径上做有状态处理。
- **审计压缩 + 保留**：旧日期自动 zstd 压缩（20-75×），可配 `audit_retention_days`。
- **目录优先 config**：log/image cache 目录是 config 字段而非环境变量；服务模式下 vmr.sh 自动从 shell 抓 `${VAR}` 写 `~/.config/vmr/env`。
- **代理仅显式**：env 上的 HTTP_PROXY 等故意忽略；要走代理必须在 config 写 `${HTTPS_PROXY}` 或具体值。

---

## 1. 逐文件评审（流水账）

> 每条目格式：**文件路径 + 行数 + 主要内容 + 发现的问题（带严重度）**。
> 严重度标记：`[S]` 严重 / `[M]` 中等 / `[L]` 轻微 / `[I]` 信息性 / `[Q]` 问题/质疑。
>
> 已读完批次的总结在每批末尾；跨文件的问题会在汇总章节集中归类。

### 1.A 基础工具层（rundir / strategy / core）

---

#### 1.A.1 `internal/rundir/rundir.go`（60 行）
- **主要内容**：解析运行时目录的三层兜底链——`~/.vmr/<sub>` → `os.TempDir()/vmr_<sub>` → `<cwd>/<sub>`。被 `config.applyDefaults` 用来填 `LogDir`/`ImageCacheDir` 的默认。`home()`/`cwd()` 各自容错，注释清楚解释了为什么不用 TMPDIR（macOS 3 天清理会悄删审计）。
- **测试覆盖**（`rundir_test.go`）：4 个用例，含 `Resolve` 的导出版检查；用纯函数 `resolve(homeDir, …)` 注入测试避免环境干扰——好做法。
- **问题**：
  - `[I]` `cwd()` 失败时返回 `"."`，调用方 `filepath.Join(".", "vmr_logs")` 在不同 OS 上行为稳定；可以接受。
  - `[L]` 没有任何并发约束；不过它是个纯函数，没共享状态，也不需要。

#### 1.A.2 `internal/strategy/strategy.go`（76 行）
- **主要内容**：排序策略注册中心（`Register`/`Build`/`Sort`）+ 一个内置 `priority` 维度。声明接口为 `Dimension`，未来加 `weight`/`round_robin` 等只需 `Register`，**router 不变**。`Sort` 用 `sort.SliceStable`，确保同 priority 时保持配置文件顺序。
- **测试**（`strategy_test.go`）：2 个用例，稳定排序 + 未知维度报错。
- **问题**：
  - `[Q]` 注释承诺"round_robin 状态在 dimension 实例里管理"——目前只有一个 `priority` 实现是 stateless 的，stateful 维度的并发安全（被 router 的多 goroutine 并发调用 `Sort`）是不是真被覆盖？测试里没看到。属于低优，因为目前只有 priority；以后加 round_robin 之前补一个测试即可。
  - `[L]` `Sort` 每次调用都跑 `compare`，O(n log n)；不过 endpoint 数量都很小，没意义。
  - `[I]` `Register` 在重复注册时 panic——合理（启动期一次性注册）。`strategy` 包目前没有 init 之外的注册。

#### 1.A.3 `internal/core/core.go`（115 行）
- **主要内容**：定义 `MarshalNoEscape`（`SetEscapeHTML(false)`+去尾换行）/ `CanonicalRequest`（只解 `model`+`stream`，Raw 保其他字节）/ `ErrorClass`（9 个枚举：4 个 health 关心的 + 4 个仅给审计分类的 + `ErrClient`）/ `Endpoint`+`HealthKey`/`Name`。
- **设计要点**：
  - `HealthKey = AdapterType/Provider/Model/<sha256(apiKey)前8位>`——把 AdapterType 塞进 key 是为了支持同 provider 名跨协议组（同一个 `openrouter` 在 `providers.openai` 和 `providers.anthropic` 是两个 endpoint）。注释清楚。
  - `Name`（暴露在 `X-VMR-Endpoint` 和审计里）**不含 API key**——测试明确锁定了这个不变量。
  - `ErrBuild`/`ErrNetwork`/`ErrCanceled`/`ErrTruncated` 这 4 个新枚举是**只为审计分类**而存在的——注释强调它们不改 failover/health 行为。这是干净的设计。
- **测试**（`core_test.go`）：5 个用例覆盖 HealthKey 三种语义边界（协议去重 / key 区分 / 稳定性 / Name 不含 key）+ ErrorClass.String() + MarshalNoEscape。
- **问题**：
  - `[Q]` `MarshalNoEscape` 的注释说"json.Encoder adds newline"——但实际上它是在 Encode 的结尾加 `\n`，所以 `bytes.TrimSuffix(..., "\n")` 一定对。但如果未来传入一个已经被 encoder 半路 flush 的 Buffer（不太可能），可能会 trim 掉真正的数据。属于理论性问题，可忽略。
  - `[L]` `ErrorClass` 用 iota 直接当 JSON 数字序列化——但代码里用 `String()` 输出字符串，所以审计里存的是字符串不是数字。OK。
  - `[L]` `HealthKey` 包含 API key 的 sha256 前 4 字节 (8 hex chars)。理论上两个 endpoint hash 同样前 4 字节的概率 ~1/2^16 = 0.0015%，碰撞会让两个 endpoint 共享 cooldown 状态——极端低概率，可忽略。**如果想严格**，可以加 `...[:8]` 到 8 字节，但成本/收益不对等。

---

### 1.B 适配器层（adapter / openai / anthropic）

#### 1.B.1 `internal/adapter/adapter.go`（70 行）
- **主要内容**：`Adapter` 接口（`Protocol`/`BuildRequest`/`ClassifyError`）+ 注册中心 `Register`/`Get`/`Names`。`BuildRequest` 的签名返回 `(*http.Request, []byte, error)`——多返回那个 `[]byte` 是为了**避免 router 在审计时再 GetBody 一遍**（multi-MB body 二次拷贝成本），注释清楚。
- **问题**：
  - `[I]` 接口小、职责清晰；`database/sql driver` 风格的 init() 注册模式也简洁。
  - `[L]` 没有未注册 adapter 的 fallback；调用方必须先注册。OK。

#### 1.B.2 `internal/adapter/classify.go`（269 行）
- **主要内容**：`DefaultClassify` 把 HTTP status + body 片段映射到 `core.ErrorClass`。关键规则：
  - 451/403+content-hint/400+content-hint → `ErrContent`（不罚健康）
  - 401 → `ErrAuth`
  - 402/404 → `ErrEndpoint`
  - 408 → `ErrTransient`
  - 429 + 含 "insufficient"/"quota"/"balance"/"credit" → `ErrEndpoint`；否则 `ErrRateLimit`
  - 4xx 内含 "model"+"unknown/not found/.../supported" → `ErrEndpoint`
  - 其他 4xx → `ErrClient`（不重试）
  - 5xx → `ErrTransient`
  - 32 KB body 截断嗅探。
- **`RewriteModel`**：手写 JSON 字节扫描定位 top-level `model` 值的位置，做 splice（字节级替换）。**关键不变量**：
  - 嵌套 `model` key 不动（测试 `TestRewriteModel_NestedModelKeysUntouched`）
  - content 里的 `"model":"x"` 是 escaped 字符串，不当 key（测试 `TestRewriteModel_EscapedTextInContentUntouched`）
  - 同名零拷贝（测试 `TestRewriteModel_SameNameZeroCopy`）
  - 缺 key 走 generic fallback（parse → 加 model → re-serialize）
  - 未知/将来字段原样保留（`TestRewriteModel_UnknownFieldsPreserved`）
  - `MarshalNoEscape` 防 `< > &` 被转义（`TestRewriteModel_NoHTMLEscaping`）
- **`contentHint`** 嗅探清单包含 EN+ZH 双语；1026/1027 是 MiniMax-M3 内容违规的特定代号（注释有据）。
- **测试**（`classify_test.go`）：8+ 个 unit + 2 个 benchmark。
- **问题**：
  - `[M]` **扫描器实现的复杂度风险**：`topLevelModelValues`/`skipJSONString`/`skipJSONValue` 三个函数手工模拟了一个 mini JSON parser。任何 edge case（如注释、trailing garbage、surrogate pair across boundary）都可能让它返回 `false` 走 generic 路径——generic 路径丢失 key order。注释有提，但**目前的测试集只覆盖了 happy path 与几个 specific edge**——比如没有：
    - 含 BOM 的 body
    - 单行超大（>1MB）字符串里的 `"model":"x"`（`skipJSONString` 走 `bytes.IndexByte` 应当 OK，但没测）
    - Unicode surrogate pair split across buffer（不重要，因为是 JSON escape 范畴）
    - `{"model": "x"` 后无 closing brace 的截断 body（应 fallback，目前确实 fallback 因为 `i >= len(raw)` 触发）
    - 多重嵌套 `[]`/`{}` 中键名带转义双引号
    建议：在 `classify_test.go` 里加几个**fuzz 测试**（Go 1.18+ 支持 `testing.F`），覆盖率能立刻上来。**风险等级 M**，因为这部分代码直接关联"字节级保真"的承诺。
  - `[M]` **`contentHint` 的 false positive 风险**：注释说"宁可宽（多一次无害 failover）"——但 32KB body 里偶然出现"flagged"或"敏感"关键词（比如客户把日志塞进 content）会触发 `ErrContent`，**switch 一次**——单次额外 failover 是无害的；所以权衡是对的。OK。
  - `[L]` `classifySnippetBytes = 32 << 10`，32KB 内存拷贝 on 4xx；hot path 不走，正常。
  - `[L]` 错误路径：`strings.ToLower(string(body[:min(len(body), classifySnippetBytes)]))`——在 32KB body 上分配 ~32KB 字符串。GC 压力存在但低频（4xx 路径），可忽略。
  - `[I]` `DefaultClassify` 里 `case status >= 400 && status < 500` 分支先看 content，再看 model，再 fall through 到 `ErrClient`。顺序很重要（注释明确解释了——"content flags may mention 'model' too"）。OK。
  - `[Q]` 注释提到的 "MiniMax returns 400 (not 404) for unknown model"——但 `case status == 402 || status == 404` 跳过了 400，让 400 走到下面的 generic 4xx 分支；那里有 "unknown"/"not found"/"supported" 关键词检测，所以 MiniMax 的 400+"invalid params, unknown model 'x' (2013)" 仍能匹配 `ErrEndpoint`。逻辑对的，但路径多绕了一步；如果未来 MiniMax 改 wording，会失效。**建议**：把 `400+unknown model` 直接短路到 `ErrEndpoint`，减少耦合。

#### 1.B.3 `internal/adapter/openai/openai.go`（57 行）
- **主要内容**：`OpenAI{}` 实现 `Adapter`：`BuildRequest` 把 `req.Header`（server 层组装的协议头）拷贝过来，再强制设 `Content-Type: application/json` 和 `Authorization: Bearer <api_key>`——注释解释：客户端的 Authorization 是给 vmr 的，不是给上游的。
- **测试**（`openai_test.go`）：1 个 BuildRequest + 1 个 ClassifyError 表驱动测试覆盖 16+ 用例。
- **问题**：
  - `[L]` `strings.TrimRight(ep.BaseURL, "/")` 后追加 `/chat/completions`——如果用户配的 `base_url` 已经含了 path（如 `https://api.example.com/openai/v1/`），路径会被保留。OK。
  - `[L]` `httpReq.Header.Add(k, v)` 在循环里——多 value header 会重复。OK，因为 `Header.Add` 是累加的；如果一个 key 既在 `req.Header` 又有 adapter 自己设的（`Content-Type`/`Authorization`），会**双重**——adapter 自己设的最后调 `Set` 覆盖，符合预期。但比如 `Authorization` 已在 client request header 里出现了（不应该），仍然会被 adapter 覆盖。OK。
  - `[I]` 没有清理 client header 的 blocklist 在这里做；注释说"blocklist filter"是 server 层的职责（`chatHandler` 入口过滤）。OK。

#### 1.B.4 `internal/adapter/anthropic/anthropic.go`（64 行）
- **主要内容**：同 OpenAI，但路径 `/messages`、用 `x-api-key` 头、加 `anthropic-version` 默认（`2023-06-01`），如果客户端没自带 version。
- **`ClassifyError` 额外处理**：529（Anthropic-specific `overloaded_error`）→ `ErrTransient`，否则走 `DefaultClassify`。
- **测试**（`anthropic_test.go`）：3 个用例（含 `TestBuildRequestForwardsProtocolHeaders` 验证客户端自定义 `anthropic-version`/`anthropic-beta` 不被覆盖）。
- **问题**：
  - `[L]` 默认 version 是 `2023-06-01`，硬编码。Anthropic 自己现在推荐的是更新版本号；不过 Anthropic 通常向后兼容旧 version，没问题。如果要"newest known"，可改成 `2024-10-22` 之类——但这就不是稳定行为了，不改也行。
  - `[L]` 没有显式处理 OpenRouter 转 Anthropic 协议——OpenRouter 的 anthropic-face 错误格式可能不同。但因为走 `DefaultClassify` 兜底，且测试里有"DeepSeek anthropic endpoint"的实际 wording 用例，至少这条路验过。

#### 1.B.5 **第一批跨文件观察**
- **设计一致性**：所有 adapter 都把 `req.Header` 拷过去再 `Set` 自家关键 header——逻辑统一。
- **单测覆盖**：build request、classify error（表驱动 16+ 用例），赞。
- **缺**：adapter 包没有公共接口的 mock 用于上层测试（router 层自己做 fake）；不是问题，只是个 note。
- **API key 注入机制一致**：都没用 env 兜底；key 必须在 config 显式给。

---

### 1.C 健康与审计层（health / audit）

#### 1.C.1 `internal/health/health.go`（166 行）
- **主要内容**：被动健康状态机。**两类冷却**：
  - `transient` (base 2s, cap 5min) — 网络/5xx
  - `long` (base 10min, cap 1h) — auth/endpoint
  - `Retry-After` 被尊重（429/503），**但 cap 1h**——防被恶意/buggy upstream 锁死。
- **半开探针（half-open single-flight probe）**：`Acquire` 在 cooldown 过期后给第一个调用者发探针位（`probing=true`），其余被拒；探针必须以 `Success`/`Failure`/`Neutral` 之一结算。
- **`ReportNeutral`**：content-policy flag、client cancel 等"和健康无关"的结局——只释放 probe 槽，**不**增加 fail count、不加重 cooldown。注释清楚强调"每次 acquire 必须以三者之一结束，否则 endpoint 永锁"。
- **`Status`**：用于 `/admin/status` 输出。
- **测试**（`health_test.go`）：7 个用例，含 transient backoff 曲线、long cooldown、Retry-After、half-open 单飞、Neutral 不加重、Success 重置、cap 验证。
- **问题**：
  - `[I]` `transientBase`/`transientCap`/`longBase`/`longCap` 都是不可调常量；假如某个用户想要"重一点"或"轻一点"的冷却，没法 config 调。**算不算问题**？取决于定位——目前的卖点是"零调参开箱即用"，所以常量是设计选择。低优。
  - `[L]` `Available(key, now)` 返回 `!now.Before(s.cooldownUntil) && !(s.fails > 0 && s.probing)`——后半段当 `fails == 0` 时短路；但探针槽永远清不掉吗？不，`probing=false` 在 healthy 状态正常值，所以 `!(false)` = true。OK。
  - `[Q]` `Status` 的 `LastError` 只暴露类名（"auth"/"rate_limit"/…），不含原始 err msg。这是个安全选择（避免在 `/admin/status` 里漏 provider 的原始错误细节）；但用户排查问题时，会想看原始 msg 才能知道是 "invalid API key" vs "API key revoked" vs "subscription expired"——这三者都归 `ErrAuth`，但排障动作不同。**建议**：在 `/admin/status` 输出里附 `last_error_detail`（已经被 audit 记录了，没必要保密），debug 体验大幅提升。低优（要权衡"admin 是 loopback only"——目前 README 说 "loopback-only; no api_key required"，本来就在信任边界内，可以放）。
  - `[L]` `ReportFailure` 用 `min(retryAfter, longCap)`——`min` 是 Go 1.21+ 内置；go.mod 要求 1.25.1，OK。

#### 1.C.2 `internal/audit/audit.go`（406 行）
- **主要内容**：核心数据结构 + Logger。
  - `Record` 字段：ts, dur_ms, **ttft_ms** (client-view first token), model, protocol, stream, outcome, client (Exchange), attempts ([]Attempt), images, replay_of, client_key_tag。
  - `OutcomeFor(status, canceled)`：统一 ok/error/canceled 语义——live server 和 replay 都共用，避免不一致。
  - `Exchange`/`Attempt`/`Message`：每条记录 both layers（client↔vmr、vmr↔upstream），headers 都过 `Redact`（credential masking）。
  - **`ImageInfo`** 包含 11 字段（含 CacheHit、Downscaled*）——无论是否启用 downscale 都填充 metadata，report 的 `图片/压缩` 列才有真实数据。
  - **`Redact`**：6 类 credential header 掩码（保留 scheme 前缀如 `Bearer ` + 后 4 字符）；**不修改原始 header**（测试验证）。
  - **`KeyTag(key)`**：取 key 末尾 6 字符窗口，含 `-` 时只保留最后一个 `-` 之后的内容。注释给了 9 个例子。**配合 config 16 字符最小长度限制**确保不会泄漏整 key。
  - **`Logger`**：JSONL appender。**午夜轮转**：每天 `vmr-audit-YYYY-MM-DD.jsonl` 一个文件；旋转触发 `scheduleHousekeeping`。
  - **`scheduleHousekeeping`**：用 `housekeeping atomic.Bool` 防重叠 sweep，goroutine + WaitGroup 让测试可等。
  - **`Close`**：关 fd + 置 `closed=true`；**故意不等 housekeeping**（注释："compression is crash-safe, shutdown shouldn't block on it"）。Late write 返回 error，**永不开新文件**（防止跨午夜 reopen）。
- **测试**（`audit_test.go`）：8 个用例，含 daily rotation、Redact、KeyTag（10 个 case）、EncodeBody、NIL logger no-op、WriteAfterClose。
- **问题**：
  - `[S]` **`housekeeping` 通过全局 `var retentionDays atomic.Int64`**——这是一个**进程级全局状态**！`config.Config` 改了之后要通过 `audit.SetRetentionDays` 显式推过来。如果哪天有人加 hot-reload listener 漏调一次，retention 不会跟随 config 变化。**当前实现**应该是 `cmd/vmr/main.go` 里的 watcher 在 reload 时调用了（待验证——我会看 main.go）。**风险 S**，但若调用链完整则无问题；交叉验证后归类。
  - `[M]` **`EncodeBody` 文档说"the slice is referenced, not cloned"**——这是性能优化。但调用方若后面修改了 slice（特别是 `Recorder` 那种 streaming buffer），就会污染已写入 JSONL 的内存表示。当前所有调用方都承诺"自己拥有 slice"——是个隐式约定，**靠注释和 review 维持**。**建议**：要么复制（多花 multi-MB 内存代价），要么更明确地用 doc 标 "do not mutate after this call"。
  - `[M]` **`RawPreStrip`** 字段类型 `any`——comments 说是"the buffered segment only (whatever the normalizer had accumulated at that moment)"。但类型 `any` 不利于消费者 schema 化（report 会去类型断言回 []byte）。**建议**：改成 `json.RawMessage` 或注释清楚"始终是 []byte"。低优。
  - `[L]` `Message.Headers http.Header` 直接序列化——`http.Header` 的 `MarshalJSON` 会输出 `map[string][]string`，是个标准格式。OK。
  - `[L]` `Logger.Write` 里 `if l.closed { return error }`——但调用方对 `Write` 的错误处理是"return for the caller to log and otherwise ignore"。这是 OK 的（注释明确），但意味着 hot path 在 shutdown 期间会持续刷错误日志到 server log。**建议**：在 server 关停流程里**先 drain pending requests 再调 Close**，否则会被噪声刷屏。
  - `[I]` `Record.TTFTMS` 注释说明"0 (omitted) when nothing was written or response was instant (<1ms local rejects)"——0 含义有歧义（"未测量" vs "真零"），但 report 处理时统一当 0 = 未测量。OK。

#### 1.C.3 `internal/audit/housekeep.go`（148 行）
- **主要内容**：扫描 `dir`、正则匹配 `vmr-audit-YYYY-MM-DD.jsonl[.zst]`、对非今日的 plain 文件做 zstd 压缩（写到 `.tmp` 后 rename + remove 原文——crash safe），对超 retention 的文件做删除。
- **关键设计**：
  - 文件名带日期，**不依赖 mtime**——单次 `os.ReadDir` 就够。
  - 中途进程死掉（rename 已完成，remove 未完成）→ 下次 sweep 检测到 `.zst` 已存在就直接 remove 原 plain 文件，**resume**。
  - 压缩+保留**同一轮**判定（不跨天延迟）。
- **测试**（`housekeep_test.go`）：6 个用例，覆盖压缩只压旧日、跳过已压缩、忽略非审计文件、retention 默认禁用、retention 实际删除、resume interrupted compress、round trip。
- **问题**：
  - `[L]` `compressOne` 用 `os.Remove(tmp)` 在错误路径上——但 tmp 文件是**新的、可能空**；删除失败的话下次 sweep 会看到 `.zst.tmp` 孤儿。**算边缘 case**，现实几乎不可能（tmp 创建失败的话不会写任何字节）。可接受。
  - `[L]` `purgeOne` 用 `os.Remove` 不区分 `.jsonl` 还是 `.jsonl.zst`——单次 sweep 内某文件可能先被压缩再被保留判定命中删除（注释里专门解释了），测试 `TestHousekeep_RetentionDeletesOldFiles` 锁定了这个行为。OK。
  - `[L]` `housekeep` 启动后调 `os.Stderr.Fprintf`——这种"后台 goroutine 往 stderr 写"对 systemd/launchd 服务模式的用户不友好（stderr → service log 文件）。可接受——服务模式把 service log 重定向到 vmr.log。

#### 1.C.4 `internal/audit/read.go`（96 行）
- **主要内容**：`MaxLogLine = 128MB`、`OpenLogFile(path)`（自动判 `.zst` 解压）、`ForEachLine(reader, maxLine, fn, onSkip)` 流式逐行回调。
- **`ForEachLine` 设计**：用 `bufio.Reader.ReadSlice` 读，遇到超长行**drain + skip + 回调 onSkip** 而不是 abort——一个坏行不会毁掉整个文件。复用 `buf` 切片（fn 不能 retain）。
- **测试**（`read_test.go`）：5 个用例（skip oversize、final 无换行、plain open、zst open、reject garbage zst）。
- **问题**：
  - `[L]` `ForEachLine` 在 `bufio.ErrBufferFull` 时会 continue 把当前行继续读——但 `bufio.Reader.ReadSlice` 的 max buffer 默认 4096，所以需要先 `NewReaderSize(r, 1<<20)`（1MB）才能一次返回 ~1MB 片段。代码就是这样做的。OK。
  - `[L]` `zstdReadCloser.Close` 先关 decoder 再关 file——decoder 不持有 fd（它从 `io.Reader` 读），所以关 decoder 只是释放内部 buffer；关 file 才真关 fd。OK。
  - `[Q]` `ForEachLine` 的 `buf` 在 onSkip 路径会被 `buf[:0]` 重置——但此时 `buf` 可能仍持有 `cap` 的内存；长期跑下来分配池大小由 maxLine 控制（128MB）。这意味着每个 reader **最多一次** 128MB 分配。OK。

#### 1.C.5 **本批跨文件观察**
- **retention 通过全局 atomic 同步**——这点在 main.go hot reload 处需要确认；否则会成为 bug 源。
- **`audit` 包的 API 简洁**，只有 Logger + 读侧工具类 + 数据类型。Report/replay 都基于它。
- **`audit.Record.TTFTMS` 字段名 vs doc**：JSON tag `ttft_ms`，字段名 `TTFTMS`——不一致（首字母不是缩写）。Go 风格上 Go 官方推荐 `TTFT` 不推荐 `TTFTMS`；但与 audit package 内的命名（`DurMS`、`APIKey`、`CacheHit`）对照——`DurMS` 也是这样写的，所以是 **package convention**，可接受。

---

### 1.D 配置层（config）

#### 1.D.1 `internal/config/config.go`（366 行）
- **主要内容**：`Load`/`Parse` → `expandEnv` → `yaml.Unmarshal` → `applyDefaults` → `validate`。`Duration` 自定义 YAML unmarshaler。
- **核心数据结构**：`Provider`（含 `Proxy *bool` 三态）、`EndpointConfig`、`ModelConfig`（含 `ImageDownscaleMaxPx *int` 区分 unset 与显式 0）、`Timeouts`、`Config`（顶层）。
- **`ProxySpecFor`**：按 base_url scheme 选 http_proxy/https_proxy；provider 自己 `Proxy: false` 直接短路。
- **`applyDefaults`**：listen、timeouts、image_downscale clamp 到 0、image_cache_ttl_days ≤ 0 用 default 7、max_concurrency < 0 → 0、audit_retention < 0 → 0、max_body_mb ≤ 0 → 8、log/image_cache 目录用 rundir 兜底、strategy 空 → ["priority"]。
- **`validate`**：listen host:port 解析；APIKeys < 16 拒；proxy 字段格式；provider base_url 格式；adapter 协议存在；model endpoints 引用合法性。
- **测试**（`config_test.go` + `config_proxy_test.go` + `config_dirs_test.go`）：~30 个用例，覆盖 env expansion、defaults、image downscale 两种 unset vs 0 区分、proxy 三态、validation 错误等。
- **问题**：
  - `[S]` **见 1.C.2 的 `[S]`**：retention 通过全局 atomic；hot reload 时需要 main.go 主动调 `audit.SetRetentionDays`。**待交叉验证**。
  - `[L]` `expandEnv` 用 `envRe.ReplaceAllStringFunc`，对 `${VAR}` 形式。不识别 `$VAR` 或 `\$` 转义——未文档化。如果用户在 api_key 里**字面**写 `$abc` 会被吞成空。**低优**：实际配置里基本不会这么写。
  - `[L]` `Duration.UnmarshalYAML` 不支持整数值（如 `timeouts.connect: 30` 当 30 秒 vs 30 ns 的歧义）。要求必须是字符串。OK，行为一致。
  - `[Q]` `applyDefaults` 在 `Models[name][name] = m` 改了之后写回 map——这是 Go map 的常见 idiom，但说明 `m` 是值类型，不能改字段就生效。OK。
  - `[M]` **YAML 配置错误信息不够友好**——`yaml.Unmarshal` 返回的错误可能很难定位行号（尤其是 nested 结构）。**建议**：在 `yaml.Unmarshal` 错误时 hint 行号（`yaml.Node.Line`），或者把错误格式化到 "yaml parse error at line N"——debug 体验改进。低优。
  - `[I]` `validate` 里 `if _, ok := adapter.Get(protocol); !ok`——意味着 `providers.<unknown>` 会被拒，**也意味着 adapter 必须在 config 解析前 init() 注册**。测试里都用 `_ "vmr/internal/adapter/openai"` `_ "vmr/internal/adapter/anthropic"` 显式 blank import 触发。**生产代码 main.go 必须同样 import**——否则连 yaml 都解不出来。**待交叉验证**（main.go）。
  - `[L]` `expandTilde` 不处理 `~user` 形式——注释明确说了，OK。
  - `[L]` `minAPIKeyLen = 16`——硬编码常量，不配置化。但 audit.KeyTag window 是 6，所以理论上 7 字符以上就不会泄露整 key，16 是个安全裕度。OK。
  - `[Q]` 没有处理 **重复 provider 名 + 同 protocol**：例如 `providers.openai.openrouter: ...` 出现两次。yaml 会取最后一个，validate 不报错。**问题**？不算 bug，只是个 note（重复配置是用户自己的事）。

#### 1.D.2 `internal/config/watch.go`（53 行）
- **主要内容**：`Watch(path, onChange)` 用 `fsnotify` 监听 config 所在目录（避免编辑器 replace-in-place 不触发），300ms 防抖。
- **问题**：
  - `[L]` **没有处理 fsnotify 事件丢失**（buffered channel 满了会丢事件）。300ms 防抖能部分缓解，但极端场景（同时改两次）可能只触发一次回调。OK 在配置场景下足够。
  - `[L]` watcher 只 watch 目录，rename 事件可能指向不存在的 path；`fsnotify.Rename` 通过 `& Write|Create` 接收，下次 reconcile 由 onChange 实现——目前行为依赖 onChange 端的 retry-on-ENOENT（待看 main.go）。**低优**。
  - `[I]` 没有测试——该文件 53 行没有单测。这个 watchdog 是 hot reload 的入口，缺测试不算严重，但**值得补一个集成测试**（用 fsnotify 触发临时文件 change）。
  - `[Q]` `watcher.Errors` channel 的 err 被静默 drop（注释没写）。**低优**：watcher 出错很难恢复，进程最好重启。

---

### 1.E 路由层（router / strategy 已读，router 主体）

#### 1.E.1 `internal/router/router.go`（678 行）
- **主要内容**：核心 failover 循环 + 快照原子切换 + 并发闸 + upstream client 构造 + 响应归一化编排 + 4 个 200/4xx 出口。
- **设计要点**：
  - `Snapshot` 用 `atomic.Pointer[Snapshot]` 持有，hot reload 换时旧 in-flight 请求继续用旧 snapshot（`Install` 显式 `old.CloseIdleConnections()` 释放空闲池，但**不**等活跃连接）。
  - `NewUpstreamClient` 把 `http.Transport` 配置抽象出来：被 router/Install/cmd 的 diagnose/replay 三处共用，避免代码重复。
  - `ProxySpecFor` 的解耦：`mode` + `proxyURL` 二元组，**没有环境变量回退**——`router.go:170` 注释明确"proxy env vars are deliberately not consulted"。
  - **probe 释放路径覆盖度**：`Health.Acquire` 在 3 个 release 路径都正确释放（Success/ReportNeutral/ReportFailure），且每次必有一解（注释："every acquired probe must end in exactly one of Success / Failure / Neutral, or the endpoint stays locked out forever"）。`server_probe_test.go` 锁定了其中 2 个；缺 1 个是 `ReportSuccess` 后探测已恢复——由 `TestSuccessResets` 间接覆盖。
  - **`copyFlush` 的 watchdog + goroutine 架构**：32KB chunk + 同步 channel + idle timer。SSE 路径真流转发。body 4xx stall 也走同一路径（`io.LimitReader` + `time.AfterFunc` 兜底）。
  - **`IngressPath(protocol)` 导出供 server/replay 共用**——单点修改防止 drift。
- **测试**（`router_test.go` + `response_test.go` + `router_proxy_test.go`）：~30 个用例覆盖 ImageDownscale 解析、proxy 分组（按 mode+URL 而非 provider 分组）、end-to-end 代理（用 `upstream.invalid` 黑洞证明 proxy 真实生效）、响应归一化各路径（model rewrite、think strip、cross-chunk think、done sentinel、opaque passthrough、Anthropic no-done、soft block）。
- **问题**：
  - `[M]` **`Response.Body` 在 4xx 路径用了 64KB `LimitReader`**——但 `RequestHeaderTimeout` 已经守了 headers，错误体超过 64KB 会被截断。注释说"64KB within that window is generous"，真实上 4xx 错误体多数 < 8KB（OpenAI/MiniMax 风格），但**Anthropic DeepSeek 的 4xx 错误体**含 trace、errors[] 数组等可能 10+KB。**建议**：提到 128KB 或改为 `8<<10` 的循环读取（错不了，且 4xx 不是 hot path）。
  - `[M]` **`writeError` 在 JSON `Encode` 失败时无 fallback**（`router.go:633`）——目前用 `json.NewEncoder` + `Encode`，错误会 buffer；极少见但 panic risk 极低。
  - `[L]` `parseRetryAfter` 的 HTTP-date 解析（`http.ParseTime`）支持三种格式（RFC1123/RFC850/ANSI C asctime），但当前常见的是 seconds 整数或 RFC1123。OK。
  - `[L]` `modelNames` 返回所有 virtual model name 按字典序——用于 404 时的"可用的"列表提示。**注意**：`Models[protocol]` 只在当前协议下找，所以 404 消息只会列出本协议的可用 model，不含另一个协议的。OpenClaw 用"client 走错入口"的诊断场景下这是友好的。
  - `[Q]` `otherProtocolFor` 当用户在 `/v1/chat/completions` 调用 anthropic 命名的模型时返回 "use POST /v1/messages"——注释提到 "wrong entry point" 诊断。**好设计**。
  - `[L]` `IngressPath` 中 fallback 是 `/v1/chat/completions`（默认 openai）——**未来加新协议时**如果忘记更新这里，新协议也会走错路径。**建议**：让 `IngressPath` 查 adapter registry（`adapter.Get(name)` 调 `Protocol()`，从 `Protocol()` 推 path；或新增 `IngressPath()` 到 Adapter 接口）。目前只有 2 个协议，靠注释提醒。
  - `[L]` `installLimiter` 的"capacity change 期间短暂过度准入"风险——注释承认了。实际窗口是 ns 级。**低优**。

#### 1.E.2 `internal/router/response.go`（590 行）
- **主要内容**：响应归一化器（model 改写 + think 块剥离 + [DONE] 追加 + soft block 嗅探），三种传输模式（undecided/buffered/passthrough）的状态机。
- **设计要点**：
  - **决策延迟**（sentinel-driven）：undecided 模式积累事件直到第一个 payload 事件决定走 buffered 还是 passthrough；`scanned` offset 防止 ping 风暴把决策变成 O(n²)。
  - **resume after think**：think 块触发的 buffered 在 </think> 之后切回 passthrough，前缀已 strip 的部分先 emit，剩余的流式——客户端在 thinking 阶段本来就看不到内容，所以无感知延迟。
  - **溢出降级**：bufferedCap=32MB / undecided 同样 32MB，超出走 raw passthrough + "overflow_raw_passthrough" applied 标记。`TestRespStream_UndecidedOverflowDegradesToOpaque` 锁定此行为。
  - **`stripThinkingProcess` 严格触发保护**：只对首个 content value 以 "Thinking Process:" 起头的情况触发，避免吃掉正常代码审查里的 "Looks good. Proceed with the merge" 之类。`TestStripThinkingProcess_LooksGoodInNormalStream` 锁这个。
  - **`softBlockMarkers` 只观测不改字节**：2xx 响应里嵌入的 `input_sensitive`/`output_sensitive` 只在 audit `applied` 里留痕，不改响应。failover 看不见（status 2xx）——这是已知盲区（design doc §5）。
- **测试**（`response_test.go`）：~25 个用例，覆盖以上所有路径 + cross-chunk think + 1-byte reads + 多种 SSE 异常形状 + non-streaming JSON 路径。
- **问题**：
  - `[M]` **`stripThinkingProcess` 强绑 MiniMax 行为**——`Thinking Process:` 关键字 + `Looks good. Pro` 标记都硬编码。如果 MiniMax 改 wording，需要重写。**可接受的工程权衡**（上游改就要改），但**风险等级 M**：单点失效触发。
  - `[M]` **`thinkPattern` 的 `(?:\\n|\n)*` 后缀**——`\\n` 匹配 `\n` 两字符（JSON-escaped newline），`\n` 匹配真实换行。MiniMax M3 的 think 块结束后是 `\n\n`（两个换行）作为分隔；这里两个都被吃。OK。
  - `[L]` `containsSoftBlockMarker` 的"敏感词表"只有 2 个 literal——**通用性差**，未来 MiniMax 加新字段（如 `risk_level`）就不会被捕捉。
  - `[L]` `reassembleSSE` 与 detail.go 的功能重复——`router/response.go` 处理 raw bytes（响应路径），`report/render.go` 的 `reassembleSSE` 重新解析（报告路径）。两套独立实现同样逻辑。**潜在 drift 风险**——但因为测试覆盖完整，OK。
  - `[I]` 代码用了大量 `bytes.Index`、`bytes.IndexByte` 优化路径；很多 inline `[]byte` 字面量以避免 alloc。这是性能敏感代码（hot path 多次 failover），无可厚非。
  - `[Q]` `read` 中途返回 `(0, nil)` 0-字节读——文档注释解释了"callers idle watchdog tick"。正确。
  - `[L]` `modelFieldPattern = `("model":\s*")[^"]*"` ——只匹配 `key":"value"` 形式（value 没有 escaped quote）。**`\"model\":\"x\"` 在 value 里不会被误改**，因为 `\"` 不会被 `[^"]*` 匹配中断——但要测试覆盖（已通过 `TestRewriteModel_EscapedTextInContentUntouched` 锁）。

---

### 1.F 服务层（server / recorder）

#### 1.F.1 `internal/server/server.go`（362 行）
- **主要内容**：HTTP 入口、auth、header blocklist、image downscaling 嵌入点、audit 录制。
- **关键不变量**：
  - **authenticate** 用 `subtle.ConstantTimeCompare` 防 timing attack，匹配 `api_key`（untagged catch-all）或任一 `api_keys` 条目（返回 `KeyTag` 标签）。**无任何 key 配置时**门全开但仍对客户端自愿发送的值计算 `KeyTag`——`docs/ClientAPIKeyGrouping_Design_Sonnet5.md` 解释了这个"内网自报身份"语义。
  - **headerBlocklist** 12 项（auth、host、content-length、accept-encoding、hop-by-hop 等）；`FilterClientHeaders` 导出供 replay 复用。
  - **chatHandler 流程**：audit record 创建（含 defer 写盘） → auth → body read (`MaxBytesReader`) → 解析 `model`/`stream` → **acquire concurrency slot**（在 body 解析后） → image downscale → 构造 CanonicalRequest → router.Serve。
- **测试**（11 个测试文件，~2700 行）：涵盖 auth、failover、content、probe、hang、headers、imgprep、openclaw scenario、audit、v22（双协议）等。
- **问题**：
  - `[M]` **`chatHandler` 中 `audit rec` 在 auth 之前就创建并 defer 写盘**——意味着**未鉴权成功的请求也会落审计**（含打码后的 Authorization）。这是**有意为之**（`docs/ClientAPIKeyGrouping_Design_Sonnet5.md` 提到这点），但如果用户想"完全关审计 for unauth"，需要新建 `audit-on: false for 401`。当前设计 OK。
  - `[M]` **`newRecorder(w, rec.TS)` 在 audit != nil 时才包，否则裸 w**——意味着 audit off 时 `rec.TTFTMS` 不存在（这是定义，OK）。但 `rec.TTFTMS` 在 audit 关闭的请求里也不被记录，**没有别的 metric 能代替**——如果以后想统计 TTFT（即便没审计），需要单独的 metric 钩子。**低优**。
  - `[L]` `chatHandler` 顺序：probe `model`/`stream` 字段（在 AcquireSlot **之前**）——保证 malformed JSON 请求不被并发闸占。`router_proxy_test.go` 也用同样顺序。OK。
  - `[L]` **chatHandler 在 audit != nil 时才**做 image detection（"Skip entirely when there is no consumer"）——性能优化，但 audit 关 + downscale 关的请求是真正"零开销"路径。
  - `[Q]` `adminStatus` 的 loopback 检查用 `net.SplitHostPort` + `net.ParseIP(host).IsLoopback()`——对 IPv6 zone 标识（`::1%eth0`）支持？测试都是 IPv4。**低优**。
  - `[L]` `models` 端点不要求 auth（无 `s.auth(s.models)`）——`adminStatus` 要 auth 但 README 说"loopback-only; no api_key required even if one is set"——**两处都只对 loopback 开放**（前者无限制——这是个不一致：理论上 models endpoint 也应该 loopback-only？**还是说是有意可远程读**？）——**疑似不一致**：`/v1/models` 远程可读 + 无 auth，`/admin/status` loopback only + 无 auth；前者有泄露内部模型拓扑的微小风险。**建议**：models 端点也应 loopback-only，或者 admin 不需要 loopback。**风险等级 M**。
  - `[M]` **`writeError` 在 server 用了 401 等错误体**（注释）——router 的 writeError 是另一个（同样的实现），但**不共享代码**（`router.go:633` 和 `server.go:325`）。两处重复实现，应该抽到公共文件。**中等问题**。
  - `[L]` `chatHandler` 中在 `s.audit != nil` 分支内才做 `rec.Model, rec.Stream = probe.Model, probe.Stream`——意味着 audit off 时这些字段不被记录。**定义** OK。

#### 1.F.2 `internal/server/recorder.go`（57 行）
- **主要内容**：tee wrapper 记录 client-view 状态/headers/body/TTFT。
- **测试**（`recorder_test.go`）：TTFT 锁。
- **问题**：
  - `[I]` 简单包装器，无状态共享。Flush 透传。**OK**。
  - `[L]` `r.buf.Write(p)` 是 `bytes.Buffer.Write`——并发不安全。但 recorder 整个生命周期都在一个 goroutine（请求处理）里用，**安全**。

---

### 1.G 入口（cmd/vmr）

#### 1.G.1 `cmd/vmr/main.go`（683 行）
- **主要内容**：7 个子命令（start/check/status/report/dirs/diagnose/replay）+ 启动 banner + 启动/停止 log markers + config 摘要 + SIGHUP 热加载 + SIGTERM 优雅退出。
- **关键不变量**：
  - **`audit.SetRetentionDays(cfg.AuditRetentionDays)` 必须在 `audit.New` 之前调用**——`main.go:266` 注释明确解释：`New` 启动时会跑一次 housekeeping sweep，那个 sweep 读 retention 当时的值；如果在 `New` 之后才 set，第一次 sweep 不会 purge。
  - **Hot reload 路径**（`main.go:284-298`）：失败保留旧 cfg；`log_dir` 变化是"restart required"提示（audit logger 持的是启动时 dir）。
  - **SIGHUP + fsnotify** 双重触发 reload。
  - **SIGTERM/SIGINT** 触发 `srv.Shutdown(ctx)` 10s 优雅 drain + `logStop` 写终止 marker。
  - **`logConfigSummary` 把 model 列表按"effective try order"打印**——`EffectiveOrder()` 在 router.BuildSnapshot 时就排序了，startup log 和 check/diagnose 三处共用同一份排序逻辑（router.go:46 注释锁了这个不变量）。
- **测试**（`main_test.go` + `main_diagnose_replay_test.go`）：覆盖 7 个子命令的 CLI flag 解析与错误处理，不深入执行。
- **问题**：
  - `[M]` **`logStop` 不会在 panic 路径调用**——`panic` 直接退出时 `defer` 不会触发（除非显式 `defer logStop`）。**建议**：在 main 函数入口加 `defer func() { if r := recover(); r != nil { logStop(...); panic(r) } }()`，但 `start` 是个内部函数、`defer` 在调用层——**实际** `start` 内 `serveErr` channel 退出时 `logStop` 被调用，**panic 路径仍漏**。**中等问题**，但低发生率。
  - `[M]` **`status` 命令自己启一个 http.Client 拿 `/admin/status`**（`main.go:533`）——如果 `vmr` 没启动，会返回"`is vmr running on %s?`"错误。**OK**。
  - `[L]` `start` 启动 banner (`vmrBanner`) 是固定 ASCII 艺术，**不会随版本变化**。**OK**（注释明确"distinct from the ordinary timestamped lines around it"）。
  - `[L]` `logStart` 把 `path` 写入 banner 后一行——若 path 含特殊字符（`\n` 之类），日志解析可能歧义。**现实几乎不可能**。
  - `[L]` `serveErr` 的 select 没有 `default`——`srv.ListenAndServe()` 启动失败时 channel 必返回 → 走 error 路径。**OK**。
  - `[I]` `cmdStatus` / `cmdCheck` 不并发跑（不像 `cmdReport` 后接 session 分析）。OK。
  - `[M]` **`cmdReport` 在 `sess, err := report.AnalyzeSessions(paths)` 失败时整个 `report` 失败**——意味着如果 session 分析有 bug，**全量 report 都不可用**。**建议**：把 session 分析错误降级为 warn（仍输出主 report，不带 sessions/tools/workloads）。**风险 M**：核心功能被次级功能拖死。
  - `[L]` `cmdReport` 串行写所有输出文件（json/md/req）+ 详情：单大日志（gigabytes）下 O(minutes)。**可接受**——单次跑批。

#### 1.G.2 `cmd/vmr/main_test.go` + `main_diagnose_replay_test.go`（160+188 行）
- **主要内容**：CLI flag 解析、错误处理路径的纯函数测试。
- **问题**：
  - `[I]` 没跑端到端（真起一个 vmr 实例 + curl 一下）——属于集成测试范畴，按现有约定放 CI 即可。**OK**。

---

### 1.H 诊断与重放（diagnose / replay）

#### 1.H.1 `internal/diagnose/diagnose.go`（391 行）
- **主要内容**：`vmr diagnose` 4 阶段：config 校验 + env 检查（DNS/TLS/proxy 联通 + api_key 状态） + 真实小请求连通性测试 + 路由预览（带连通结果标注）。
- **关键设计**：
  - `checkConcurrency = 8` 限制并发，慢场景下不至于串行 60s × N providers。
  - `envCheck` 对**走 proxy 的 provider 跳开 DNS 检查**——这是 `TestEnvCheck_ProxiedProviderSkipsDirectDNS` 锁的"有据 false-positive 修复"（真实路径不走直连，DNS 失败不反映问题）。
  - `testEndpoint` 复用 `router.NewUpstreamClient` + `adapter.BuildRequest`——**和 vmr 自己用同一份代码**，结果可信。
  - `runConcurrent` 泛型 helper。
- **测试**（`diagnose_test.go`）：~12 个用例，覆盖 DNS/TLS/proxy/api_key 状态 + 并发跑 + 状态分类 + 无连通性 + 不合法 config。
- **问题**：
  - `[L]` `testEndpoint` 用 `minimalBody`（`{"model":..., "max_tokens":1, "messages":[{"role":"user","content":"hi"}]}`）——所有 OpenAI/Anthropic 兼容 provider 都接受，**但有 provider 计费"最少 5 tokens"之类**——这些"最小请求"的 token 算入 cost。**低优**。
  - `[L]` `envCheck` 的 DNS 解析不走 system resolver 的 DoT/DoH——若用户刻意只走 DoH，`net.Resolver{}` 会用普通 DNS，诊断结果不可信。**理论性**。
  - `[L]` `TestTimeout` 默认 10s，但对慢启动的 provider（冷启动 5-10s）不够——`Options.TestTimeout` 可调。**OK**。

#### 1.H.2 `internal/replay/replay.go`（450 行）
- **主要内容**：3 种 record 定位方式（`-detail`/`-ts`/`-line`）+ 互斥校验 + 复用 `adapter.BuildRequest` 重建 + dry-run / 真实 replay / `-record` 写新 audit 记录。
- **关键设计**：
  - **`replayHeaders` 双过滤**（`FilterClientHeaders` 过滤 server blocklist + `audit.IsCredentialHeader` 过滤 audit 抹码但 server 仍会转发的 header，如 `Api-Key`）——`TestRun_StripsMaskedCredentialLikeHeaders` 锁。
  - **writeReplayRecord 字段布局对齐 live record**：Client.Request.Body 用 pre-rewrite，Attempt 用 outBody。`TestRun_WritesReplayRecord` 锁。
  - **`recordView.Body` 用 `json.RawMessage`**：保留原 bytes，因为 `audit.Message.Body` 是 `any`，反序列化丢原字节。
  - **`loadRecordByTS` 用毫秒匹配 + 报歧义**（让用户用 -line 消歧）——注释解释了 `time.Parse(time.RFC3339, ...)` 接受任意 fractional 长度，所以 ns 精度和 ms 精度都接受。
- **测试**（`replay_test.go`）：~13 个用例覆盖所有路径。
- **问题**：
  - `[L]` `ResolveModel` 走 `models.<protocol>.<virtualModel>.endpoints` 找 provider——audit 里的 `record.Model` 是**虚拟名**（`rv.Model`）；当 audit 用 `"(rejected)"` 表示拒绝请求时，`ResolveModel` 失败 → 用户必须 `-model` 显式。`TestRun_ModelResolutionError` 锁这个。
  - `[L]` `-record path` 是独立 JSONL 路径（不入主流水线）——这个行为 OK 但**与 vmr 主审计兼容**：可以被 `vmr report <file>` 二次聚合（`audit.OpenLogFile` 都支持）。
  - `[L]` `-max-time` 用了 ctx.WithTimeout，但 `httpReq.WithContext` 后才赋值给 client.Do——流程正确。

---

### 1.I 报告（report）—— 项目最大模块

#### 1.I.1 `internal/report/report.go`（805 行）
- **主要内容**：聚合器，5 个 bucket（Rows 细粒度 + Overall + ByModel + ByDate + Endpoints + Hours）+ Format 9 后的 EndpointsAll / HoursOfDay（all-dates-merged 独立 bucket）。
- **关键设计**：
  - **每个 bucket 独立计算 p50/p95**（`percentiles` 函数 nearest-rank）——避免"per-bucket 算完再合并"（合并的 percentile 数学上不对）。
  - **`EndpointsAll` 与 `HoursOfDay` 是真的独立 bucket**（不是 `Endpoints`/`Hours` 跨日 roll-up）——注释解释了为什么：`finishRow`/`finishHour` 会 free 原始 `durs`/`ttfts` 切片，再想跨日合并就没数据可算了。**好设计**。
  - **`Format = 9`**（常量）——每次结构变化都升级 Format 并写 changelog。**好做法**。
  - **`addRecord` 一趟多 bucket**：每个 record push 到 Rows/Overall/ByModel/ByDate 一次，HourRow/EndpointRow 额外 + 1 pass。**性能优化**。
  - **Cumulative usage in SSE**：Anthropic SSE 在 `message_start` 报 input_tokens（final）、`message_delta` 报 output_tokens（cumulative）——`usageFromObj` 用 `max()` 兼容两种语义。**好**。
  - **`attemptErrorClass` fallback**：新 audit 记录用结构化 `ErrorClass` 字段；旧 log 走 `Error` 字符串前缀 split。**向后兼容**。
  - **`ExtractUsage` cache token 语义**：`In - CacheRead - CacheWrite = fresh tokens`（Anthropic），`In` 已经含 cache hit（OpenAI）。**用 `input_tokens` key 存在与否切规则**。
- **测试**（`report_test.go`）：~13 个用例 + Format 版本行为测试。
- **问题**：
  - `[M]` **`% 5.5` 等输出格式化**用了 `fmt.Sprintf("%.1f%%", ...)`——`pct()` 函数 `O requests = 0` 时返回 `"-"`——**OK**。
  - `[L]` `BodyBytes` 用 `json.Marshal` 重新序列化 JSON 算字节——**multi-MB 重复 marshal 成本**。**只在 4xx/SSE 等需要时**用，可接受。
  - `[L]` `percentiles` 的 nearest-rank：`rank(0.50) = int(0.5*n+0.5)-1 = (n-1)/2`（n≥2）。对 n=1 边界：`int(0.5*1+0.5)-1 = 0`，正确。**OK**。
  - `[M]` **`attemptErrorClass` 的 fallback 解析**：`strings.IndexByte(a.Error, ':')` 取前缀——**但如果错误体是 `network: read tcp ...: i/o timeout`**（包含 3 个冒号），会取到 `network`——对的。**但**如果 `Error` 是 `"error"`（一个没有冒号的 fallback），则 `i < 0` → return `a.Error` = `"error"`。**OK**。

#### 1.I.2 `internal/report/usage.go`（178 行）
- **主要内容**：从 4 种响应 body 抽 token（OpenAI JSON/Anthropic JSON/OpenAI SSE/Anthropic SSE）。
- **关键设计**：
  - **`usageFromObj` 用 `input_tokens` 字段名检测 Anthropic**——隐式但 self-documenting。
  - **`mergeUsage` 用 `max` 合并 SSE 多事件的 cumulative 字段**。
- **测试**（`report_test.go` 中 `TestExtractUsage` 覆盖 4 种 shape）。
- **问题**：
  - `[L]` `usageFromObj` 不处理 DeepSeek 的非标准字段（`prompt_cache_miss_tokens`）——DeepSeek API 文档可能含这类字段。**理论性**。
  - `[I]` 良好设计。

#### 1.I.3 `internal/report/export.go`（460 行）
- **主要内容**：`vmr-requests.jsonl` 写入 + 按 client key tag 写 sibling 文件 + `ToolShapes`/`Workloads`/`SessionRows` 聚合。
- **测试**（`session_test.go` 中 `TestWriteRequestsExport*`）。
- **问题**：
  - `[L]` `requestRow` 28+ 字段——单 record JSON 约 500-1000 bytes；10K records × 1KB = 10MB。**OK**。
  - `[I]` `workloadClass` 优先级：compaction > heartbeat > dream_diary > interactive。

#### 1.I.4 `internal/report/markdown.go`（592 行）
- **主要内容**：5 个表（overall/by-model/endpoints/daily/hourly）+ workloads + sessions + tools，全部以"每个 bucket 自己的 p50/p95"渲染。
- **问题**：
  - `[L]` `p50p95` 输出 `"ms"` 单位——中文表头用"`p50/p95 首字延迟`"——一致。
  - `[I]` 大量 helper 函数（`tokensTriple`/`bytesInOut`/`avgTokensInOut*`）—每个 Row/WorkloadRow/SessionRow 类型有一个变种。**可重构**为泛型（Go 1.18+），但目前清楚。

#### 1.I.5 `internal/report/render.go`（594 行）
- **主要内容**：Markdown 渲染原语（`<details>` 折叠 / code fence / chat message / SSE 重解析 / role chars / tool calls）。
- **问题**：
  - `[L]` `codeFence` 用 3-backtick 起步，按 content 里的最长 backtick run +1 选 fence——正确但**depth 计算简单**（不区分连续 backtick 与空格 backtick 混排——例如 ` ``` ` 在 code block 边界）。
  - `[L]` `renderPart` 的 `image_url` placeholder 用 base64 解码长度（`DecodedLen`）——这是解码前最大长度，不精确。**OK**。
  - `[I]` 大量 emoji 作为视觉符号（`🆕`/`🟢`/`🔴`/`🔶`/`🤔`）— 注释解释了"统一节奏，让读者能扫读 300-message 对话"。

#### 1.I.6 `internal/report/session.go`（782 行）
- **主要内容**：agent session/task/turn 分组 + 每 record 特征提取 + compaction 链接。
- **关键设计**：
  - **LCP-based 分组**（longest common prefix of message hashes）+ `parentWindow = 16` 限搜索范围。
  - **多信号融合**：
    1. `Traceparent` trace ID（强信号，跨请求）
    2. Claude Code `metadata.user_id` 含 `session_<uuid>`（强信号）
    3. 首条非 system 消息的 hash（fallback anchor）
    4. OpenClaw `chat_id`（在 user message 的 envelope 里正则抓取）
  - **OpenClaw envelope 智能剥离**（`stripOpenClawEnvelope`）— 不会丢真实指令。
  - **`NoReply` 模式**：LLM 返回空或 "NO_REPLY" → 不开新 task（视为同一 task 的重试）。
  - **Compaction linking**：`Summarizes`（被压缩的会话）+ `ContinuesTo`（续接的会话）通过 substring match。
  - **`realUserText` 多启发式**：剥离 envelope / 跳过 `OpenClaw runtime context` / 跳过纯 tool_result / 跳过纯时间戳——避免把 scaffolding 当用户指令。
- **测试**（`session_test.go`）：~13 个用例覆盖分组 / 工具聚合 / write requests / by-tag / ungrouped / collapsed session / 各种 metadata 路径。
- **问题**：
  - `[M]` **`lcp` 算法基于 md5 hash 序列**——`md5` 不是 crypto-secure，但用作 session fingerprint 不需要 collision resistance（碰撞只会让两条不同 record 错误归并到同一 task，**最坏影响是分组错误**）。**可接受**。
  - `[M]` **`parentWindow = 16`**：实测 24-轮 openclaw 测试过了，但**长程 session 的 16 之后 delta**如果触发了早期消息的不在 LCP 范围，会被错误开新 task。`docs/AgentSessionGrouping_Analysis_Fable5.md` 应该解释这个 magic 16。**低优**（实测有效）。
  - `[M]` **`linkCompactions` 用 substring match**——`needle(s, 200)` cap 到 200 字符。如果一个会话的开头指令恰好只有 5 字符、跟 compaction output 内的 substring 重叠，会误链。**极低概率**。
  - `[L]` `deltaHasNewInstruction` 的 "parent 中已存在则不计" 检查——**靠 `parentKeys` map 实现**——这个 map 只在 `Parent != nil` 时构建。**OK**。
  - `[L]` `toolsSig` 用 `md5(sorted_tools)` 头 4 字节——碰撞概率高但**只影响 group**（"工具声明完全不同的两条 record 误归并"）——`md5[:4]` = 32 bits，1M 工具集才有 50% 碰撞概率。**OK**。
  - `[L]` `realUserText` 的 `leadingBracketRe` 只处理 `[Day Mon DD HH:MM TZ]` 格式——OpenClaw 时间戳变体多（如 `[2026-07-08 12:34:56 +0800]`）——不通用。**可接受**。

#### 1.I.7 `internal/report/detail.go`（1249 行）
- **主要内容**：单 request 详情 Markdown + 同名 JSON + index + 按 client key tag 写 sibling index。
- **关键设计**：
  - **emoji-diff 表格**：`renderBodyDiff`（测试 `TestRenderBodyDiffMarksChanges` 锁）— 用 `🟢`/`🔴`/`🔶` 标 field-level 变化。
  - **`<details>` 折叠所有 message**——保证长会话可扫读。
  - **filename 0-padded timestamp + 同毫秒冲突用 `-N` suffix**。
  - **`attemptUpstream` 兼容老 log 格式**（用 `/` split）vs 新 log（结构化字段）。
  - **`chatUserLabel` 剥 `user:` prefix**（OpenClaw chat_id 形式）。
- **测试**（`detail_test.go`）：~14 个用例。
- **问题**：
  - `[L]` `sanitizeName` 替换非 `A-Za-z0-9._-` 字符为 `-`——但**不**去重连续的 `-`——`OpenClaw-r2-dream_diary` 之类可能产生连续 `--`——**理论问题**。
  - `[M]` `renderBodyDiff` 用 `reflect.DeepEqual` 做对比——**只对相同类型字段有效**。如果 field 从 `string` 变 `null` 或 `string` 变 `[]any`，reflect 会处理。**OK**。
  - `[L]` `fileLinksCell` 用 `<a href=details/...>`——纯 HTML（GitHub 渲染时 OK，部分 Markdown 渲染器对 `<a>` 不友好——但本项目是给 GitHub / VS Code 用的，OK）。
  - `[I]` 1249 行超大文件——但内部函数组织清晰，每个函数单一职责。可接受。

---

### 1.J 设计文档（docs/）

#### 1.J.1 `docs/VirtualModelRouter_v2_Fable5.md`（648 行）
- **主要内容**：完整设计文档——定位、协议、架构、Adapter、错误分类、响应归一化、并发、审计、目录、健康、镜像降采样、配置、运维、诊断、replay、compaction 设计。
- **状态**：与代码同步（含 `## §N` 节号锚定）。
- **问题**：
  - `[I]` 一份优秀的"设计即文档"工程范例。**OK**。

#### 1.J.2 `docs/ClientAPIKeyGrouping_Design_Sonnet5.md`（449 行）
- **主要内容**：api_keys 多调用方分组的方案设计与 4 版迭代记录。
- **问题**：
  - `[I]` 自我记录迭代过程。**OK**。

#### 1.J.3 `docs/SensitiveWordFilter_Analysis_Fable5.md`（216 行）
- **主要内容**：sensitive-word 库分析与 VMR 接入评估——**明确"不含实现"**。
- **问题**：
  - `[I]` 研究性文档，不期望有实现代码。**OK**。
  - `[Q]` §3.5 提到"对 agent 上下文的数据损坏风险"是核心权衡——但**没有"go/no-go"决策**。读者不知道是不是要继续做。**注意**——若作为 backlog item 需 owner。

#### 1.J.4 `docs/vmr_future_strategy_deepseek-v4-flash.md`（631 行）
- **主要内容**：竞品全景（LiteLLM、CLIProxyAPI、New API、One API、Bifrost、Portkey、AISIX 等）+ 战略决策矩阵 + 演进路径建议。
- **问题**：
  - `[I]` 由 deepseek-v4-flash 写就——**生成式模型生成的战略报告**——**内容详尽但未必经人手验证**。核心结论（"不要 SaaS、专注 byte-faithful + agent audit"）合理，但具体战术（如"学习 Bifrost 的 semantic cache"）需要 owner 拍板。

---

### 1.K 入口脚本 / 杂项

#### 1.K.1 `vmr.sh`（~600 行）
- **主要内容**：dev-mode + service-mode (launchd/systemd) 启停脚本 + 端口冲突检查 + PID 探测（pgrep -f 用绝对路径） + 自动从当前 shell 抓 env 写 `~/.config/vmr/env`。
- **关键设计**：
  - **lazy `resolve_log_dir`**：避免 stop/status 在 config 损坏时失败（注释明确解释：service-mode 部署出问题时"先修配置"的前置工作会失败，正是不能失败的时候）。
  - **`port_holder` lsof 容错**：`lsof` 没装、IPv6 listen（注释说"本项目都用 IPv4"）→ 静默 no-op。
  - **平台分流**：macOS = launchd (plist) / Linux = systemd (user unit) / 其他 = exit 1。
  - **`LIFO` cleanup**：`t.Cleanup` 风格（实际 `trap`/`Bash function`）—— `release` 通道先关、server 后关，避免 handler hang。
- **问题**：
  - `[L]` 没有测试——**bash 脚本测试难做**（虽然 `bats` 存在但项目未引入）。**低优**。
  - `[L]` `running_pids` 用 `pgrep -f "$MATCH"`（`MATCH=$BIN start`）——若用户用 `bash -c "$BIN start"` 包装启动，可能不匹配。**极低概率**。
  - `[L]` **`port_holder` 的 IPv4-only 限制**——未来 IPv6 配置（`listen: "[::1]:8800"`）会失效。**低优**。

#### 1.K.2 `config.example.yaml`（~160 行）
- **主要内容**：示例配置 + 每个字段的注释。
- **问题**：
  - `[I]` 注释详尽。**OK**。

#### 1.K.3 `.gitignore`、`LICENSE`、`go.mod`、`go.sum`
- **主要内容**：标准。`go.mod` 4 个直接依赖 + 1 个 indirect；`go.sum` 7 行（含 transitive）。极简。**OK**。

---
---

## 2. 跨模块观察（综合）

读完 78 个源文件 + 4 篇 docs 后，几个跨模块模式浮现：

1. **文档与代码同源**：所有"为什么这样写"都沉淀在 doc comments 和 `docs/` 里。设计变更通过升级 `Format` 常量（report）和 doc 历史版本管理。**这是该项目的最强工程实践**。

2. **byte-faithful 是核心承诺**：adapter 层的 `RewriteModel` 字节 splice、server 的 header blocklist 透传、router 的 copyFlush 真流转发——所有路径都在小心翼翼避免"多一道转换"。这条原则渗透到每一个决定（如拒绝 cross-protocol、Annexing SSE 边界）。

3. **MiniMax M3 是核心兼容目标**：think 块剥离、Thinking Process 剥离、1026/1027 错误码、ChatID 通过 OpenClaw envelope 抓取——这套深度耦合。**风险**：单 provider 行为变化会触发多处修改。**已观察到的 mini 修复**（format 9 的 OpenAI Anthropic "input_tokens" 区分）说明这层防御是有效的。

4. **被动健康 + 单飞探针**：5+ 处独立设计/重实现（health.go 主体、Health.Acquire 释放路径、router.go 调用、TestHalfOpenSingleFlightProbe、TestProbeSlotReleasedOnClientCancel、TestProbeSlotReleasedOnClientError、TestConcurrencyGate）——**覆盖度好**，但**复杂度也集中**，未来加新的"failover 决定"需要小心不破坏这个不变量。

5. **审计格式演化**：Record 字段从最初到现在逐个加（TTFTMS、ClientKeyTag、Images、ReplayOf、RawPreStrip、ImageInfo）——每个都是**真实需求驱动的加法**（TTFT 是 OpenClaw streaming 体验问题、ClientKeyTag 是多调用方分组需求、Images 是 audit 的 image-downscale 上下文）。**新字段引入模式好**（加字段而非改字段、不破坏旧 log 读取）。

6. **测试工程**：9228 行测试覆盖 ~11000 行生产代码（1:1.2）——对单二进制工具项目来说比例很高；**单测粒度细**（health 包有 8 个用例覆盖每个 cooldown 路径；response.go 有 25+ 个用例覆盖每条 normalizer 路径）。**好的工程实践**。

7. **依赖极简**：4 个直接依赖（`yaml.v3`/`fsnotify`/`klauspost/compress`/`golang.org/x/image`）——**没有 Web 框架、没有 ORM、没有 provider SDK**。这跟 README 卖点一致（"Unix-style tool, zero DB, zero web UI, zero runtime plugins"）。

8. **未充分测试/缺单测的热点**：
   - `internal/config/watch.go`（hot reload 入口）——无单测
   - `vmr.sh`（bash 脚本）——无测试
   - `internal/audit/housekeep.go` 的 `housekeep()` 函数（除 integration 通过 `TestHousekeep_*` 间接覆盖）——无独立单测
   - `internal/router/response.go` 的 `stripThinkingProcess` 之外的某些 sub-branch（trailing whitespace、Chinese marker "unsupported"）——已有 9 个测试覆盖主要路径
   - `cmd/vmr/main.go` 的 `logStart`/`logStop`/`logConfigSummary` —— 无单测（print 行为）

---

## 3. 问题汇总（按梯队）

> 严重度 = 修复紧迫性 × 修复成本。**第一梯队 = 推荐立即改**，**第二梯队 = 改与不改变动成本相当或 ROI 不清晰**，**第三梯队 = 不动也无所谓**。
> 每个问题给出建议方案，标注**确定**（把握高）/ **需权衡**（需 owner 拍板）/ **风险**（方案本身有 trade-off）。

### 3.1 第一梯队（推荐立即改）

#### 3.1.1 [S/M] `internal/router/response.go::stripThinkingProcess` 强绑 MiniMax wording，单点失效
- **问题**：thinking=medium 的剥离依赖硬编码的 `Thinking Process:` 头和 `Looks good. Pro` 标记。MiniMax 改 wording → 全员 thinking 内容泄漏到用户面前（OpenClaw 体验受损）。
- **影响范围**：每个 `thinking=medium` 触发的请求都会出问题——高频场景。
- **建议方案（确定）**：
  1. **加观测性**：把 `thinkPattern`/`stripThinkingProcess` 触发时的 raw bytes 落 `audit.Attempt.RawPreStrip`（已部分实现）—— 后续分析能确认 MiniMax 改 wording。
  2. **加 fallback 降级**：当 `stripThinkingProcess` 不触发（wording 变了）但响应是明显的 chain-of-thought pattern（e.g. 大段 `1.` `2.` `3.` 编号段）→ 走"overflow_raw_passthrough" 类似的降级。
  3. **建议加可配置 trigger 关键字**（config 一行 `thinking_process_marker: "Looks good. Pro"`），但**默认仍是硬编码**——避免配置成本。
- **建议**：[M] 改 wording 风险高、修复成本可控。

#### 3.1.2 [M] `internal/server/server.go::models` 端点无 auth + 远程可读，暴露内部模型拓扑
- **问题**：`GET /v1/models` 没有走 `s.auth()` 也不限 loopback。远程 attacker 可枚举内部 model 名（知道是 `coding` 还是 `claude-mini`），配合响应时间差异做 fingerprint。
- **影响范围**：公网部署时（README 提到 "trusted private network" 是常见场景，但"公网 + api_key" 也支持）。
- **建议方案（确定）**：与 `/admin/status` 对齐——加 loopback 检查，或加 auth wrapper。
- **实施成本**：~10 行。
- **建议**：[M] 改一下，5 分钟成本换 0 风险。

#### 3.1.3 [M] `internal/router/router.go::writeError` 与 `internal/server/server.go::writeError` 重复实现
- **问题**：两处独立实现相同的 error envelope（`{"type":"error","error":{"type","message"}}`）。未来改格式需要改两处。
- **建议方案（确定）**：抽到 `internal/core` 包（`core.WriteError(w, status, type, msg)`），router 和 server 都调用。
- **实施成本**：~30 行（含 import 调整）+ 1 个共享单测。
- **建议**：[M] DRY 化，明确收益。

#### 3.1.4 [M] `cmd/vmr/main.go::cmdReport` session 分析失败导致全量 report 失败
- **问题**：`sess, err := report.AnalyzeSessions(paths)` 返回 err → 整个 `vmr report` 失败。Session 分析是"增值功能"（sessions/tools/workloads），不应让主 report（aggregate）跟着死。
- **建议方案（确定）**：
  - 把 `AnalyzeSessions` 错误降级为 warning（`log.Printf` 到 stderr），主 report 继续写。
  - 加 `-no-sessions` flag 给想快的用户（已经 `vmr report` 的常见抱怨是"百万行 audit log 跑 10 分钟"）。
- **实施成本**：~20 行。
- **建议**：[M] 解耦核心/增值。

#### 3.1.5 [M] `internal/router/router.go::tryOne` 4xx body 截断 64KB 可能不够
- **问题**：DeepSeek Anthropic 风格 4xx body 含 `errors[].details` 数组可能 10+KB；64KB 应该够，但**未实测验证**。`io.LimitReader(resp.Body, 64<<10)` 截断后 audit 看到的 body 不完整。
- **建议方案（确定）**：提到 128KB 或 256KB（4xx 路径不是 hot path）。同时在 audit `Attempts[i].Response.Body` 末尾追加 `"...(truncated, N bytes total)"` 标记。
- **实施成本**：~5 行。
- **建议**：[M] 一次性修。

#### 3.1.6 [M] `cmd/vmr/main.go` 的 SIGHUP 监听 + `defer` 不覆盖 panic 路径的 `logStop`
- **问题**：`start` 内部 `panic` 会绕过 `logStop` 写停止 marker。
- **建议方案（确定）**：在 `cmdStart` 入口加 `defer func() { if r := recover(); r != nil { logStop(...); panic(r) } }()`。
- **实施成本**：~5 行。
- **建议**：[M] 一次性修。

#### 3.1.7 [M] `internal/router/response.go::stripThinkingProcess` 之外的小路 fallback 覆盖度
- **问题**：`TestStripThinkingProcess_ChineseEndorsement` 锁了"Chinese marker 不支持 → pass through"——但**生产中如果客户端用了中文 thinking 表达（未来多语言模型）**，strip 失败但 pass through——thinking 全部到用户面前。
- **建议方案（需权衡）**：与 3.1.1 合并处理。可考虑：
  - 加 metric（`audit.Attempt.Norm` 加 `thinking_process_unsupported_marker` 记录"识别了 thinking 但 marker 不认识"）。
  - 让 `workloadClass` 之外也加"thinking leak 风险"信号。
- **建议**：[M] 与 3.1.1 一并。

### 3.2 第二梯队（改与不改都合理）

#### 3.2.1 [L] `internal/health/health.go` 冷却参数硬编码
- **问题**：`transientBase=2s/transientCap=5min/longBase=10min/longCap=1h` 全是常量。
- **建议方案（需权衡）**：可加 config 字段 `cooldown_transient_base`/`cooldown_transient_cap`/`cooldown_long_base`/`cooldown_long_cap`——但 README 卖点是"零调参开箱即用"。**owner 拍板**：要不要暴露调参？
- **建议**：[L] 不动也行，加了更好。

#### 3.2.2 [L] `internal/audit/audit.go` 通过全局 `retentionDays atomic.Int64` 跨包同步
- **问题**：`SetRetentionDays` 是全局状态——任意 import 它的代码都能改。虽然 `main.go` 在 reload 时主动调，但**没有 test 锁住这个不变量**（reload 路径未测）。
- **建议方案（需权衡）**：把 `retentionDays` 改成 `audit.Logger` 的字段（构造时传入）——更显式，但需要改 main.go 多处。
- **建议**：[L] 当前 OK，重构时改。

#### 3.2.3 [M] `internal/config/watch.go` 无单测
- **问题**：hot reload 入口无单测。`main.go:284-298` 的 reload 路径只通过 `main_test.go::TestCmdCheck_*` 间接验证（不测 reload 本身）。
- **建议方案（确定）**：加 1-2 个集成测试——用 `t.TempDir()` + 写文件 + 触发 fsnotify。
- **建议**：[M] 补测试 30 分钟。

#### 3.2.4 [L] `internal/router/router.go::IngressPath` 写死 openai/anthropic
- **问题**：未来加新协议时这里需要更新（否则新协议走错路径）。2 个协议时风险低。
- **建议方案（需权衡）**：把 `IngressPath()` 加到 Adapter 接口。每个 adapter 自己声明 path。
- **建议**：[L] 等真加第三个协议时再做。

#### 3.2.5 [L] `vmr.sh` 端口探测仅 IPv4
- **问题**：`port_holder` 的注释明确"IPv4 only"，未来 listen IPv6 会失效。
- **建议方案（需权衡）**：用 `ss` 或 `lsof -i6` 替代。但目前 config 仅支持 IPv4 host:port。
- **建议**：[L] 跟随 config 演化。

#### 3.2.6 [L] `internal/router/response.go::containsSoftBlockMarker` 仅 2 个 marker
- **问题**：仅识别 `input_sensitive`/`output_sensitive` 两个字段。MiniMax 加新字段不会触发。
- **建议方案（需权衡）**：扩为 substring 列表 + 配置文件。
- **建议**：[L] 暂可，等 MiniMax 变化时再改。

#### 3.2.7 [M] `internal/router/response.go::reassembleSSE` 与 `internal/report/render.go::reassembleSSE` 重复
- **问题**：两处独立实现 SSE 重解析（router 在响应路径，report 在报告路径）。未来 SSE 协议细节变化需改两处。
- **建议方案（需权衡）**：抽到 `internal/report/sseparse` 包，两处共用。**潜在循环依赖风险**（router 已 import audit，audit import report？——不，audit 不 import report）——**OK**。
- **建议**：[M] 收益一般，看项目节奏。

#### 3.2.8 [L] `internal/router/router.go::Install` "old snapshot's idle connections 关掉"时序
- **问题**：`Install` 切换时 `old.CloseIdleConnections()` —— 注释没明说 in-flight 请求的影响。
- **建议方案（确定）**：补 1 个 doc comment 解释"in-flight 请求的连接不受影响，因为它们不是 idle"。
- **建议**：[L] 1 行注释。

#### 3.2.9 [L] `internal/server/server.go::chatHandler` 顺序是 audit-rec-create-then-auth
- **问题**：401 请求会落审计（intentional，per docs/ClientAPIKeyGrouping）。如果用户希望"401 不计费/不计审计"，无法关。
- **建议方案（需权衡）**：加 config 字段 `audit_401: true|false`。
- **建议**：[L] 默认 true，配置化延后。

#### 3.2.10 [L] `internal/imgprep/imgprep.go::processImage` 缩放用 BiLinear，画质一般
- **问题**：用 `draw.BiLinear.Scale`——不是 `CatmullRom` 或 `ApproxBiLinear`（更高质）。
- **建议方案（需权衡）**：换 `draw.ApproxBiLinear`（速度/质量平衡）——但 8% 质量提升 vs 0.2% 性能差异，**不值**。
- **建议**：[L] 不动。

#### 3.2.11 [L] `internal/imgprep/imgprep.go::jpegQuality = 85` 硬编码
- **问题**：用户不可调。
- **建议方案（需权衡）**：同 3.2.10，配置化收益小。
- **建议**：[L] 不动。

#### 3.2.12 [L] `internal/imgprep/cache.go::sweepState` 用 `sync.Map` 做 throttle
- **问题**：throttle state 按目录 key 持久在进程内——重启后丢失。**预期行为**（重启后第一天可能 sweep 一次）——OK。
- **建议方案（需权衡）**：不持久化（重启 sweep 一次是好事）。
- **建议**：[L] 不动。

#### 3.2.13 [L] `internal/report/report.go::percentiles` nearest-rank 公式
- **问题**：`int(0.5*n+0.5)-1` 当 n=1 → 0; n=2 → 0.5-1 = -0.5 → int 截 0 → 0，OK。n=3 → 1-1=0... 等等，应该是 1 = `s[1]`，对（n=3，p50 应该是第 2 个，索引 1）。**n=3 算对**。但**n=4**：`int(2+0.5)-1 = 1` = `s[1]`（应是 `s[1]`？——p50 of 4 是 avg(s[1], s[2])，nearest-rank 简化到 s[1]，OK）。
- **建议方案（需权衡）**：n=1 时返回 `s[0]` 而不是 0——现在的实现对 n=1 也对（int(0.5)-1 = -1，clamp 到 0 = s[0]）。**OK**。
- **建议**：[L] 不动。

#### 3.2.14 [M] `internal/diagnose/diagnose.go::testEndpoint` 真实发请求，可能计费
- **问题**：诊断会向每个 provider 发 1 token 的"hi"——MiniMax 的 free tier 允许，OpenRouter 计费但 1 token < 0.001 USD。
- **建议方案（需权衡）**：加 `-dry-run`（已经 `-no-test-routing` 实现了部分）——但 Phase 3 跳了意味着端到端连通性不验。
- **建议**：[L] 现实成本可忽略，文档化就好。

#### 3.2.15 [L] `internal/server/server.go::recorder` 没 flush 跟踪
- **问题**：`r.Flush()` 透传到 `ResponseWriter`——但客户端 SSE chunks 之间需要 flush。**实际** `copyFlush` 显式 `flusher.Flush()`（`router.go:508`）——OK，recorder 透传够用。

#### 3.2.16 [M] `internal/server/server_test.go::upstream.lastHeaders.Store` 用 `atomic.Value` 存 `http.Header`
- **问题**：`atomic.Value` 对 `http.Header` 写入需要"全等"——并发安全但读侧需要类型断言。OK 实际。

#### 3.2.17 [L] `docs/SensitiveWordFilter_Analysis_Fable5.md` 缺 go/no-go 决策
- **问题**：研究性文档读完读者不知道"做不做"。backlog item 缺 owner。
- **建议方案（确定）**：在文档末尾加 "## 决策" 段，给 owner 拍板"推迟/启动"。

### 3.3 第三梯队（无所谓）

#### 3.3.1 [L] `internal/router/response.go::RawPreStrip` 类型是 `any`
- **问题**：消费者（如 report/detail.go）需要类型断言。
- **建议方案（需权衡）**：改成 `json.RawMessage`。
- **建议**：[L] 不动，影响范围小。

#### 3.3.2 [L] `internal/audit/audit.go::EncodeBody` "ownership contract" 靠注释
- **问题**："the slice is referenced, not cloned" 靠注释维护。代码层面无 enforce。
- **建议方案（需权衡）**：复制（贵）或加 lint 规则（重）。**当前约定工作良好**。
- **建议**：[L] 不动。

#### 3.3.3 [L] `cmd/vmr/main.go::logStart` banner 固定 ASCII
- **问题**：banner 不随版本号变化。
- **建议方案（需权衡）**：不变更好（grep 友好）。
- **建议**：[L] 不动。

#### 3.3.4 [L] `internal/core/core.go::MarshalNoEscape` "TrimSuffix(`\n`)" 假设 encoder 总是结尾加 \n
- **问题**：理论上不符合 `json.Encoder.Encode` 文档保证（它确实加，但哪天 Go 改呢？）。
- **建议方案（需权衡）**：手动 `json.Marshal` 而非 Encoder。
- **建议**：[L] 不动。

#### 3.3.5 [L] `internal/core/core.go::HealthKey` 截 sha256 前 4 字节
- **问题**：碰撞概率 ~1/65536。
- **建议方案（需权衡）**：取全 sha256。
- **建议**：[L] 不动。

#### 3.3.6 [L] `internal/config/config.go` YAML 错误信息缺行号
- **问题**：bad config 报错没行号。
- **建议方案（需权衡）**：格式化 yaml.Node.Line。
- **建议**：[L] 用户少。

#### 3.3.7 [L] `internal/router/router.go::otherProtocolFor` "用错入口"提示字符串硬编码
- **问题**：跟 `IngressPath` 同步手动改。
- **建议**：[L] 跟随 3.2.4。

#### 3.3.8 [L] `internal/report/detail.go::sanitizeName` 不去重连续 `-`
- **问题**：罕见。
- **建议**：[L] 不动。

#### 3.3.9 [L] `internal/report/markdown.go` 大量 `*Row` / `*WorkloadRow` / `*SessionRow` 变种 helper
- **问题**：可以泛型化（Go 1.18+）但当前清楚。
- **建议**：[L] 不动。

#### 3.3.10 [L] `internal/router/response.go::modelFieldPattern` `[^"]*` 不防 "key with escaped quote in value"
- **问题**：测试已覆盖 OK。

#### 3.3.11 [L] `internal/audit/audit.go::credentialHeaders` 列表硬编码
- **问题**：新增 credential 形式需改这里。
- **建议方案（需权衡）**：可配置。
- **建议**：[L] 不动。

#### 3.3.12 [L] `docs/vmr_future_strategy_deepseek-v4-flash.md` 由模型生成
- **问题**：战略决策需 owner 验证。
- **建议**：[L] 当工作输入读，不当决策读。

#### 3.3.13 [L] `cmd/vmr/main.go::cmdReport` "session 分析失败 → 全 report 失败" 已在 3.1.4 列为 [M]

---

## 4. 改进建议（高 ROI、低成本的新 feature / 重构）

> 这里只列**回报大于成本**的、可以**短期完成**的想法。每个都估算实施成本 + 预期收益。

### 4.1 [M / 高 ROI] `vmr` 子命令加 `--json` flag 输出 JSON（用于 shell 编排）
- **现状**：`vmr status`/`vmr check`/`vmr diagnose -json`/`vmr dirs` 文本输出——好读但不易被 jq/script 处理。
- **建议**：把 `check/status/dirs` 也加 `--json` 选项（结构化输出）。
- **实施成本**：~100 行（重新用 `encoding/json` 输出 `cmdCheck` 已经在内存里的 snapshot）。
- **收益**：把 vmr 当 tool 调用更易集成到自动化部署（CI/CD、健康检查、Zabbix 探针）。
- **建议**：[M] 高 ROI。

### 4.2 [M / 高 ROI] `vmr report` 加 `-no-sessions` flag 跳过 session 分析
- **现状**：`cmdReport` 调 `AnalyzeSessions` 失败时整个 report 死（3.1.4）。即使成功，gigabytes log 下 session 分析要分钟级。
- **建议**：加 `-no-sessions`，跳过 session 分析，主 report 仍完整。
- **实施成本**：~20 行。
- **收益**：解决"大 log 跑 10 分钟"+ 解决"session 分析 bug 拖死全 report"。
- **建议**：[M] 高 ROI。

### 4.3 [M / 高 ROI] `vmr.sh` 加 `vmr.sh doctor` 一次性自检
- **现状**：用户面对 "vmr 不工作" 需要 5 个命令（`status`/`logs`/`diagnose`/`report`/`config`）拼凑。
- **建议**：加 `vmr.sh doctor` 跑：
  - `vmr check -c config.yaml`（静态）
  - `vmr diagnose -c config.yaml`（连通性）
  - `vmr status -c config.yaml`（运行时，loopback）
  - 输出"红绿灯"摘要
- **实施成本**：~30 行。
- **收益**：日常问题排查时间 -50%。
- **建议**：[M] 高 ROI。

### 4.4 [L / 中 ROI] `vmr report` 输出加 `--web` 模式（一个独立 HTML）
- **现状**：Markdown 报告是 GitHub 友好但本地浏览需要 Markdown viewer。
- **建议**：加 `--web` 渲染一个静态 HTML（带搜索/折叠）— 单文件可 `python3 -m http.server` 分享。
- **实施成本**：~300 行 HTML template（无前端框架，纯 vanilla JS）。
- **收益**：日常审计/账单 review 体验提升。
- **建议**：[L] 中 ROI（视用户场景）。

### 4.5 [L / 中 ROI] audit `Record` 加 `client_ip` 字段（区别于 `Addr`）
- **现状**：`Addr` 是 `r.RemoteAddr`（带端口）— 审计可读但不便聚合。
- **建议**：加 `ClientIP` 字段（剥端口，从 X-Forwarded-For 拉第一个或直接 net.SplitHostPort）。
- **实施成本**：~10 行。
- **收益**：报告里加"按 IP 聚合"维度，定位异常来源更易。
- **建议**：[L] 中 ROI。

### 4.6 [M / 高 ROI] `vmr diagnose` 加 `--diff-config` 模式（与上次成功 snapshot 对比）
- **现状**：hot reload 时若新 config 错，用户只看到 "reload rejected, keeping current config"——不知道**为什么**。
- **建议**：保留上次成功 config 备份，reload 失败时 `vmr diagnose` 能 diff 出"什么字段变了导致 validate 拒"。
- **实施成本**：~100 行（含备份机制）。
- **收益**：hot reload 出错时 debug 时间 -80%。
- **建议**：[M] 高 ROI。

### 4.7 [L / 中 ROI] `vmr replay` 加 `-list` 模式（不解码全部内容，只列摘要）
- **现状**：用户面对 1GB audit log 找特定 record 只能 grep ts。
- **建议**：加 `-list` 模式：每行 `ts | model | outcome | endpoint` 表格。
- **实施成本**：~50 行（复用 `audit.ForEachLine`）。
- **收益**：定位待 replay 的 record 时间 -50%。
- **建议**：[L] 中 ROI。

### 4.8 [L / 中 ROI] audit 日志加 `--rotate-hourly` 配置
- **现状**：按天轮转——大流量日（10GB+）单文件巨大，watching tail 痛苦。
- **建议**：加 `audit_rotate_interval: hour|day`（默认 day）。
- **实施成本**：~30 行（现有 `l.now.Format("2006-01-02")` 改可配置）。
- **收益**：大流量用户运维体验。
- **建议**：[L] 中 ROI（视用户量）。

### 4.9 [M / 高 ROI] `vmr report` 报告加 `--diff` 模式（与上次 report 对比）
- **现状**：看 7 天报告只能横向对比"日趋势"列，没法和上周同一天的同一指标对比。
- **建议**：加 `--diff baseline.json` 对比两个 report 的关键 bucket（TokensIn、Errors、p95 latency）。
- **实施成本**：~200 行。
- **收益**：检测"provider 性能退化"和"使用模式变化"自动化。
- **建议**：[M] 高 ROI。

### 4.10 [L / 中 ROI] `vmr start` 加 `-pprof` flag
- **现状**：性能问题排查无手段。
- **建议**：加 `-pprof :6060` 启 pprof。
- **实施成本**：~10 行。
- **收益**：hot path CPU profile 能力。
- **建议**：[L] 中 ROI（性能调优时用）。

### 4.11 [M / 高 ROI] Adapter 单元加 `--replay`（VM 内部）
- **现状**：诊断在 diagnose 子命令里——不能在生产 vmr 进程内跑。
- **建议**：加 `vmr admin replay` 端点（loopback only），允许"基于当前 config 重建并重发"——给 hot-reload 后的回归测试用。
- **实施成本**：~80 行（`internal/server` 加端点 + 复用 `replay.Run`）。
- **收益**：生产环境变更后立刻验证的能力。
- **建议**：[M] 高 ROI（如果用户场景重视"快速验证变更"）。

### 4.12 [L / 中 ROI] 把 `package main` 拆出 `internal/cli`
- **现状**：`cmd/vmr/main.go` 683 行包含 7 个子命令、banner、config summary、信号处理——单文件 7 个职责。
- **建议**：拆成 `internal/cli/{start,check,status,report,dirs,replay,diagnose}.go` + `internal/cli/cli.go`（共享 main entry）。
- **实施成本**：~200 行（机械重构）。
- **收益**：可读性 + 单元测试覆盖度提升。
- **建议**：[L] 中 ROI（重构向）。

### 4.13 [L / 中 ROI] `vmr check` 输出的 routing table 加可点击 HTML 链接（如果做 4.4 的话联动）
- **现状**：纯文本。
- **建议**：与 4.4 联动。
- **实施成本**：~50 行。
- **建议**：[L] 中 ROI（依赖 4.4）。

### 4.14 [M / 高 ROI] 自动检测"长 think 块未 strip"并警告
- **现状**：`stripThinkingProcess` 不触发时无报警（3.1.1）。
- **建议**：在 `Audit.Attempt.Norm` 里加 `thinking_process_pattern_detected` 当 response 含 `1.`/`2.`/`3.` 编号段 + 长 content（>1KB）但无 strip 标记时——给 owner 信号。
- **实施成本**：~50 行。
- **收益**：上游 wording 变化时第一时间知道。
- **建议**：[M] 高 ROI。

### 4.15 [L / 中 ROI] `vmr replay` 输出加 `--format=curl` 模式
- **现状**：dry-run 输出是 go-friendly 的 `-> POST https://...`。
- **建议**：加 `--format=curl` 直接给可粘贴的 `curl` 命令。
- **实施成本**：~30 行。
- **收益**：用户复现"对某个 audit 记录的请求"零成本。
- **建议**：[L] 中 ROI。

---

## 5. 总结

| 维度 | 评价 |
|---|---|
| **架构清晰度** | 优秀——每个包单一职责、依赖单向（cmd → server → router → adapter → core）、每个 layer 都有完整测试。 |
| **代码质量** | 高——命名一致、doc comments 详尽、错误处理精细（`errClass` 9 枚举+每个 release 路径显式处理）、go vet 友好。 |
| **测试覆盖** | 1:1.2 测试/生产比，单测粒度细到每个 fail mode。**少数热点未测**（watch.go、main.go 的 signal handling）。 |
| **文档** | 优秀——设计 doc 与代码同步、每章锚定、迭代历史保留。 |
| **依赖** | 极简（4 个直接依赖）——符合 README 的"零框架"承诺。 |
| **可扩展性** | Adapter 注册模式清晰、Strategy 注册模式清晰；新增 provider/strategy 不需改框架。 |
| **可运维性** | 好——banner / start/stop log markers / `vmr.sh` / audit / report 齐全。 |
| **可调试性** | 极好——`replay` / `diagnose` / `report` / `check` / `status` / `dirs` / `doctor`(待加) 全套。 |
| **风险** | 1 个 [S]：thinking strip wording 绑 MiniMax 行为；5-6 个 [M]：细节改进。整体低风险。 |

**优先建议**（短期可做）：
1. `vmr.sh doctor`（4.3）—— 5 分钟，最实用
2. `vmr report --no-sessions`（4.2）—— 20 分钟，解锁大 log 场景
3. `cmd/vmr/main.go` 写 `defer panic` 恢复 + `logStop`（3.1.6）—— 5 分钟，补一个 log 完整性缺口
4. `models` 端点加 auth/loopback（3.1.2）—— 10 分钟，0 风险关门
5. audit 加 `client_ip`（4.5）—— 10 分钟，报告维度增强
6. thinking 模式自动检测未 strip 警告（4.14）—— 50 分钟，提前发现 wording drift

**中期建议**（看节奏）：
- `vmr report --json`/`--web`（4.1/4.4）—— 报告可编程/可视化
- Adapter 抽取 IngressPath（3.2.4）—— 等第三个协议时再做
- `cmd/vmr` 拆包（4.12）—— 重构向
- 重复代码抽 `core.WriteError`（3.1.3）—— 30 分钟 DRY

**长期建议**（战略层）：
- 关注 Bifrost 演化（`docs/vmr_future_strategy_*` 提到的竞品）—— 6-12 月内确认 VMR 的差异化（byte-faithful + agent audit）仍独有
- 词过滤研究（`docs/SensitiveWordFilter_*`）—— owner 拍板"做不做"

**结论**：vmr 是一份**高质量、文档化、可维护**的 Go 项目；核心架构稳定，byte-faithful 与 agent audit 是其真正差异化优势；当前 ROI 最高的改动集中在 **debugging 工具完善** 与 **MiniMax 行为漂移的早期预警**——而不是在框架或核心逻辑上。
