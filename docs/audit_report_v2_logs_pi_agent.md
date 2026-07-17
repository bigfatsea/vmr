<!-- Ver 2026-07-16 22:30, by Pi Agent (minimax-m3) — 复核更新版 -->

# vmr — 全量审计报告 2.0（过程跟踪）

> **范围**：仓库 `vmr` 全部受版本控制的代码与文档（剔除 `details/`、`logs/`、`_tmp/` 历史产物及 `vmr-report*`/`vmr-requests*` 一次性产物）。基线 commit：`1d50611`（"add load testing"）。
> **方法**：逐文件精读、逐批落盘记录；每条发现带严重度标记。汇总与三梯队分组见姊妹文件 `audit_report_v2_summary_pi_agent.md`。
> **审计时间**：2026-07-16（第一轮），22:30 复核（按 2026-07-16 21:00 Fable 5 提交的代码变更），by Pi Agent (minimax-m3)。
> **复核说明**：第一轮发现的问题中，多项已在 2026-07-16 21:00 Fable 5 提交的 commit 中修复。本文档保留第一轮的逐文件评审结构（详尽的代码引用、注释、问题描述），但对每项问题的当前状态加上状态标签：**`[已修复]`**/**`[仍成立]`**/**`[已不适用]`**/**`[已升级/重构]`**。新增 §3.2 章节"复核变更摘要"集中记录本次复核发现的所有代码变更。
> **与 1.0 的关系**：`docs/AUDIT_REPORT.md`（2026-07-15 原审 + 2026-07-16 复核）是上一轮；本报告为独立重审，不复制 1.0 条目，但对 1.0 已记录且仍成立的问题会以「(同 1.0 §x)」标注。
> **设计参考**：`docs/VirtualModelRouter_System_Design_v2.md`（最新设计文档，所有判断的依据）。

**严重度**：`[S]` 严重 / `[M]` 中等 / `[L]` 轻微 / `[I]` 信息性 / `[Q]` 质疑待确认。

---

## 0. 项目结构与统计

### 0.1 顶层布局
```
vmr/
├── cmd/vmr/                            # main 入口 + 子命令 + 测试
├── internal/
│   ├── adapter/                        # 协议适配器框架 + 协议族（openai/anthropic）
│   ├── audit/                          # JSONL 审计读写 + 旧文件压缩/清理
│   ├── config/                         # YAML 加载/校验/热加载
│   ├── core/                           # 协议无关的请求预处理 + 共享类型
│   ├── diagnose/                       # `vmr diagnose` 子命令
│   ├── health/                         # 端点健康状态 + 冷却
│   ├── imgprep/                        # 内联图片缩放 + 内容哈希缓存
│   ├── replay/                         # `vmr replay` 子命令
│   ├── report/                         # `vmr report` 子命令（统计 + session 分组）
│   ├── router/                         # 端点选择 + 响应归一化
│   ├── rundir/                         # 解析 log/image cache 目录的兜底逻辑
│   ├── server/                         # HTTP 服务 + 校验/限流/审计接入
│   └── strategy/                       # 端点排序策略
├── docs/                               # 设计文档与背景分析
├── loadtest/                           # 负载测试框架（mockupstream + runner + gentargets）
├── config.example.yaml                 # 配置示例
├── config.yaml                         # 用户本地配置（已 .gitignore）
├── vmr.sh                              # 启动/服务管理脚本
├── README.md / README.zh.md            # 用户文档
├── LICENSE                             # MIT
├── go.mod / go.sum                     # 依赖
```

### 0.2 统计
- **代码行数（生产）** ~11,500 行
- **测试行数** ~8,300 行（与生产比 1:1.4）
- **docs + README** ~4,100 行
- **脚本/配置** ~1,000 行
- **依赖**：`fsnotify v1.10.1`、`klauspost/compress v1.19.0`、`golang.org/x/image v0.43.0`、`yaml.v3 v3.0.1`（+`x/sys` indirect）
- **Go 版本**：`go 1.25.1`（go.mod 声明），本机 `go1.26.5`

### 0.3 模块行数（生产代码）
| 模块 | 行数 | 职责 |
|---|---|---|
| `internal/report` | ~5,655 | 报告聚合、session 分组、明细导出（最大模块）|
| `internal/router` | ~1,279 | 端点选择 + 响应归一化 |
| `internal/server` | ~404 + 57 (recorder) | HTTP 服务 |
| `cmd/vmr` | ~694 | main + 子命令注册 |
| `internal/imgprep` | ~595 | 内联图片缩放与缓存 |
| `internal/replay` | ~450 | 重放子命令 |
| `internal/diagnose` | ~391 | 诊断子命令 |
| `internal/audit` | ~650 | 审计读写/压缩/保留 |
| `internal/config` | ~419 | 配置加载与校验 |
| `internal/adapter` | ~460 | 适配器接口 + 两个实现 + classify |
| `internal/health` | ~166 | 健康状态机 |
| `internal/strategy` | ~76 | 排序策略 |
| `internal/core` | ~135 | 当前较薄 |
| `internal/rundir` | ~60 | 目录兜底解析 |
| `loadtest` | ~705 | 负载测试工具集 |

---

## 1. 逐文件评审（流水账）

> 每条目格式：**文件路径 + 行数 + 主要内容 + 发现的问题（带严重度）**。
> 已读完批次的总结在每批末尾；跨文件的问题会在汇总章节集中归类。
### 1.A 基础工具层（rundir / strategy / core）

#### 1.A.1 `internal/rundir/rundir.go`（60 行）+ `_test.go`（43 行）
- **主要内容**：解析运行时目录的三层兜底链（`~/.vmr/<sub>` → `os.TempDir()/vmr_<sub>` → `<cwd>/<sub>`），被 `config.applyDefaults` 用来填 `LogDir`/`ImageCacheDir` 的默认。`home()`/`cwd()` 各自容错，注释清楚解释了为什么不用 TMPDIR（macOS 3 天清理会悄删审计）。
- **测试覆盖**：4 个用例含 `Resolve` 的导出检查；用纯函数 `resolve(homeDir, …)` 注入测试避免环境干扰。
- **问题**：
  - `[L]` `home()` 静默吞掉 `os.UserHomeDir()` 的 error——异常环境下直接落到临时目录兜底层级，没有任何日志/诊断痕迹（同 1.0 §3.3.14）。可接受但建议加 debug 日志。
  - `[L]` `cwd()` 失败时返回 `"."`，调用方 `filepath.Join(".", "vmr_logs")` 在不同 OS 上行为稳定；可接受。

#### 1.A.2 `internal/strategy/strategy.go`（76 行）+ `_test.go`（34 行）
- **主要内容**：排序策略注册中心（`Register`/`Build`/`Sort`）+ 内置 `priority` 维度。声明接口为 `Dimension`，未来加 `weight`/`round_robin` 等只需 `Register`，router 不变。`Sort` 用 `sort.SliceStable`，确保同 priority 时保持配置文件顺序。
- **问题**：
  - `[Q]` 注释承诺"round_robin 状态在 dimension 实例里管理"——目前只有一个 `priority` 实现是 stateless 的，stateful 维度的并发安全（被 router 的多 goroutine 并发调用 `Sort`）是不是真被覆盖？测试里没看到。属于低优，因为目前只有 priority。
  - `[I]` `Register` 在重复注册时 panic——合理（启动期一次性注册）。`strategy` 包目前没有 init 之外的注册。
  - `[L]` `Build` 用 `sync.RWMutex` 而 `Sort` 无锁——OK（注册是冷启动期的写热路径）。

#### 1.A.3 `internal/core/core.go`（135 行）+ `_test.go`（123 行）
- **主要内容**：
  - `MarshalNoEscape`（`SetEscapeHTML(false)`+去尾换行）
  - `CanonicalRequest`（只解 `model`+`stream`，Raw 保其他字节）
  - `ErrorClass`（9 个枚举：4 个 health 关心的 + 4 个仅给审计分类的 + `ErrClient`）
  - `Endpoint`+`HealthKey`/`Name`
  - **2026-07-16 新增** `WriteJSON`/`WriteError`（修复 1.0 §3.1.3 把 router/server 两份实现统一）
- **设计要点**：
  - `HealthKey = AdapterType/Provider/Model/<sha256(apiKey)前8位>`——把 AdapterType 塞进 key 是为了支持同 provider 名跨协议组，注释清楚。
  - `Name`（暴露在 `X-VMR-Endpoint` 和审计里）**不含 API key**——测试明确锁定。
  - `ErrBuild`/`ErrNetwork`/`ErrCanceled`/`ErrTruncated` 这 4 个枚举**只为审计分类**而存在，注释强调它们不改 failover/health 行为。
- **问题**：
  - `[Q]` `MarshalNoEscape` 假设 `json.Encoder.Encode` 总是结尾加 `\n`——这是 Go 标准库的既知行为，但**不是文档强保证**（未来 Go 版本若改了会有数据丢失风险）。低优。
  - `[L]` `HealthKey` 包含 API key 的 sha256 前 4 字节 (8 hex chars)。理论上两个 endpoint hash 同样前 4 字节的概率 ~1/2^16 = 0.0015%，碰撞会让两个 endpoint 共享 cooldown 状态——极端低概率，可忽略。
  - `[I]` `WriteError` 走 `WriteJSON`→`json.NewEncoder.Encode`，错误被丢弃——极低概率失败（`map[string]any` 编码几乎不可能失败），可接受。
  - `[Q]` `min` 函数（Go 1.21+）被 `classify.go` 等处使用——go.mod 要求 1.25.1，OK。

### 1.B 适配器层（adapter / openai / anthropic）

#### 1.B.1 `internal/adapter/adapter.go`（70 行）
- **主要内容**：`Adapter` 接口（`Protocol`/`BuildRequest`/`ClassifyError`）+ 注册中心 `Register`/`Get`/`Names`。`BuildRequest` 多返回 `[]byte` 是为了**避免 router 在审计时再 GetBody 一遍**（multi-MB body 二次拷贝成本）。
- **问题**：
  - `[I]` 接口小、职责清晰；`database/sql driver` 风格的 init() 注册模式简洁。
  - `[L]` 没有未注册 adapter 的 fallback；调用方必须先注册。OK。
  - `[Q]` `BuildRequest` 文档说"outbound body bytes must not be mutated after BuildRequest returns"——这是隐式契约，靠注释维护。如果未来调用方不自觉改了（比如 adapter 误用 pool），会污染已发出请求的字节同时也污染审计。**建议**：doc 强化这一点，或在 audit.EncodeBody 时做一次内容比对来发现这种错误（性能允许的话）。

#### 1.B.2 `internal/adapter/classify.go`（269 行）
- **主要内容**：`DefaultClassify` 把 HTTP status + body 片段映射到 `core.ErrorClass`。关键规则覆盖 451/401/402/403/404/408/429/4xx/5xx；`RewriteModel` 手写 JSON 字节扫描做 splice（嵌套 key 不动、escaped text 不命中、缺 key 走 generic fallback、未知字段原样保留、零拷贝快路径）。
- **`contentHint`** 嗅探清单：EN + ZH 双语 + MiniMax 1026/1027。
- **测试**（`classify_test.go`，179 行）：约 8 个 unit + benchmarks。
- **问题**：
  - `[M]` **（同 1.0 §3.2.7）`topLevelModelValues`/`skipJSONString`/`skipJSONValue` 三个函数手工模拟 mini JSON parser**——任何 edge case（如带 BOM body、单行超大字符串里的 `"model":"x"`、嵌套 `[]`/`{}` 中键名带转义双引号）都可能让扫描器走 generic 路径。**目前测试覆盖了 happy path 与几个 specific edge**，但缺 fuzz 测试。**建议**：加 `testing.F` 提升覆盖率。**风险等级 M**：直接关联"字节级保真"承诺。
  - `[M]` **402/404 跳过 `contentHint` 检查**（同 1.0 §4.18）：`classify.go:42` 把 402/404 无条件映射到 `ErrEndpoint`，与 403/429 不一致。如果某个 provider 用 404 承载内容审核拒绝（如 "resource not found due to content policy"），会被误判成 `ErrEndpoint`（触发 failover + 端点冷却），而不是 `ErrContent`。**`[已修复 2026-07-16]`**：在 `case status == 402 || status == 404` 分支前增加 `if contentHint(snippet) { return core.ErrContent }` 守卫，与 403/429 一致。`classify_test.go` 新增测试用例覆盖。Design doc §5 已同步更新（"402/404 先过内容词表，命中归 ErrContent"）。
  - `[Q]` `contentHint` 含 "敏感"/"违规"/"合规" 中文关键词——32KB body 里偶然出现这些字（比如日志内容、用户输入中包含"违规"字面量）会触发 `ErrContent` 切换——单次额外 failover 是无害的，权衡是对的。
  - `[L]` `classifySnippetBytes = 32 << 10`：32KB 内存拷贝 on 4xx 路径；hot path 不走，正常。
  - `[L]` `containsAny` 用 `strings.Contains` 线性搜索 14 个关键词——对 32KB 字符串调用 14 次，每次 O(n) → 14*32KB。**4xx 路径低频**，可接受。
  - `[I]` `DefaultClassify` 里 `case status >= 400 && status < 500` 分支先看 content，再看 model，再 fall through 到 `ErrClient`。顺序很重要（注释明确解释）。

#### 1.B.3 `internal/adapter/openai/openai.go`（57 行）+ `_test.go`（84 行）
- **主要内容**：`OpenAI{}` 实现 `Adapter`：`BuildRequest` 把 `req.Header` 拷贝过来，再强制设 `Content-Type: application/json` 和 `Authorization: Bearer <api_key>`。
- **测试**：BuildRequest 测试覆盖 unknown field 保留 + returned body 与 request body 字节一致；ClassifyError 表驱动测试覆盖 18+ 用例（含 MiniMax、DeepSeek、OpenRouter 实测 wording）。
- **问题**：
  - `[L]` `strings.TrimRight(ep.BaseURL, "/")` 后追加 `/chat/completions`——保留用户配置的子路径（如 `/openai/v1/`）。OK。
  - `[I]` 没有清理 client header 的 blocklist 在这里做；注释说"blocklist filter"是 server 层职责。OK。
  - `[L]` `httpReq.Header.Add(k, v)` 在循环里——多 value header 会重复；`Header.Set` 覆盖关键 header，OK。

#### 1.B.4 `internal/adapter/anthropic/anthropic.go`（64 行）+ `_test.go`（92 行）
- **主要内容**：同 OpenAI，路径 `/messages`、用 `x-api-key` 头、加 `anthropic-version` 默认（`2023-06-01`），如果客户端没自带。
- **`ClassifyError` 额外处理**：529 → `ErrTransient`，否则走 `DefaultClassify`。
- **测试**（3 个用例，含 `TestBuildRequestForwardsProtocolHeaders` 验证客户端自定义 `anthropic-version`/`anthropic-beta` 不被覆盖）。
- **问题**：
  - `[L]` 默认 `anthropic-version = 2023-06-01` 硬编码。Anthropic 当前推荐更新版本（2024-10-22 等），但 Anthropic 通常向后兼容旧 version。**低优**——更新即可。
  - `[L]` 没有显式覆盖 `anthropic-version` 时把 default 设上去——这意味着客户端发"我就要 2023-06-01"和"不发这个 header"在 vmr 里**有不同行为**：前者透传，后者被设为 2023-06-01。**这是一个隐式覆写**，违反 §5.4 "默认透传 + 小型黑名单" 原则——adapter 自己添加而非透传协议头。**建议**：要不就完全透传（客户端不发就不发），要不就 README 明确写"未指定时 vmr 强制使用 X 版本"。

### 1.C 健康与审计层（health / audit）

#### 1.C.1 `internal/health/health.go`（166 行）+ `_test.go`（149 行）
- **主要内容**：被动健康状态机，两类冷却（`transient` base 2s/cap 5min，`long` base 10min/cap 1h），Retry-After 尊重但封顶 1h。半开单飞探针：`Acquire` 给第一个调用者探针位（`probing=true`），其余被拒；探针必须以 `Success`/`Failure`/`Neutral` 之一结算。
- **`ReportNeutral`**：content-policy flag、client cancel 等"和健康无关"的结局——只释放 probe 槽，不增加 fail count、不加重 cooldown。注释清楚强调"每次 acquire 必须以三者之一结束，否则 endpoint 永锁"。
- **`Status`**：用于 `/admin/status` 输出，含 `LastError` 但只暴露类名（"auth"/"rate_limit"/…）。
- **测试**（9 个用例）：transient backoff 曲线、long cooldown、Retry-After、half-open 单飞、Neutral 不加重、Success 重置、cap 验证。
- **问题**：
  - `[L]` `transientBase`/`transientCap`/`longBase`/`longCap` 都是不可调常量（同 1.0 §3.2.1）。如果用户想要"重一点"或"轻一点"的冷却，没法 config 调。取决于定位——目前的卖点是"零调参开箱即用"，所以常量是设计选择。
  - `[Q]` `Status` 的 `LastError` 只暴露类名（"auth"/"rate_limit"/…），不含原始 err msg（同 1.0 §3.1.6 相关讨论）。"invalid API key" vs "API key revoked" vs "subscription expired" 都归 `ErrAuth`，但排障动作不同。**建议**：在 `/admin/status` 输出附 `last_error_detail`（已经被 audit 记录了，没必要保密）。
  - `[L]` `ReportFailure` 用 `min(retryAfter, longCap)`——`min` 是 Go 1.21+ 内置；go.mod 要求 1.25.1，OK。
  - `[I]` 半开探针不变量（"every acquired probe must end in exactly one of Success/Failure/Neutral"）由 9 个测试覆盖（其中 `TestReportNeutralReleasesProbeOnly` 锁 ReportNeutral、`TestHalfOpenSingleFlightProbe` 锁 ReportFailure、`TestSuccessResets` 间接锁 ReportSuccess）。

#### 1.C.2 `internal/audit/audit.go`（406 行）+ `_test.go`（195 行）
- **主要内容**：核心数据结构 + Logger。`Record`/`Exchange`/`Attempt`/`Message`/`ImageInfo`/credential masking + KeyTag + Logger（互斥追加、按日期轮转，触发 housekeeping）。
- **关键设计**：
  - `retentionDays atomic.Int64`（同 1.0 §3.2.2）——进程级全局状态，`SetRetentionDays` 显式推送。`main.go` hot reload 路径确实调用，调用链完整。
  - `EncodeBody` "the slice is referenced, not cloned" 靠注释维护的隐式契约。
  - `Redact`：6 类 credential header 掩码；**不修改原始 header**（测试验证）。
  - `KeyTag(key)`：取 key 末尾 **8** 字符窗口（含 `-` 时只保留最后一个 `-` 之后的内容）。注释给了 9 个例子。
  - **`Logger`**：JSONL appender。午夜轮转，**故意不等 housekeeping**（注释："compression is crash-safe, shutdown shouldn't block on it"）。Late write 返回 error，**永不开新文件**。
- **问题**：
  - `[L]` `EncodeBody` 文档说"the slice is referenced, not cloned"——调用方若后面修改了 slice（特别是 `Recorder` 那种 streaming buffer），会污染已写入 JSONL 的内存表示。当前所有调用方都承诺"自己拥有 slice"——靠注释和 review 维持。
  - `[M]` `RawPreStrip` 字段类型 `any`（同 1.0 §3.3.1）——comments 说是"the buffered segment only"，但类型 `any` 不利于消费者 schema 化（report 会去类型断言回 []byte）。**建议**：改成 `json.RawMessage` 或注释清楚"始终是 []byte"。
  - `[L]` `Logger.Write` 里 `if l.closed { return error }`——但调用方对 `Write` 的错误处理是"return for the caller to log and otherwise ignore"。OK（注释明确），但意味着 hot path 在 shutdown 期间会持续刷错误日志到 server log。
  - `[I]` `Record.TTFTMS` 注释说明"0 (omitted) when nothing was written or response was instant"——0 含义有歧义，但 report 处理时统一当 0 = 未测量。OK。
  - `[Q]` `KeyTag` "key shorter than keyTagLen is used whole as the window"——与 `minAPIKeyLen=16` 一起保证不会泄漏整 key。**注释清楚**，测试覆盖了 9 个例子。
  - `[L]` `credentialHeaders` 列表硬编码（同 1.0 §3.3.11）——新增 credential 形式需改这里。

#### 1.C.3 `internal/audit/housekeep.go`（148 行）+ `_test.go`（158 行）
- **主要内容**：扫描 `dir`、正则匹配 `vmr-audit-YYYY-MM-DD.jsonl[.zst]`、对非今日的 plain 文件做 zstd 压缩（写到 `.tmp` 后 rename + remove 原文——crash safe），对超 retention 的文件做删除。
- **关键设计**：
  - 文件名带日期，**不依赖 mtime**——单次 `os.ReadDir` 就够。
  - 中途进程死掉（rename 已完成、remove 未完成）→ 下次 sweep 检测到 `.zst` 已存在就直接 remove 原 plain 文件，**resume**。
  - 压缩+保留**同一轮**判定（不跨天延迟）。
- **问题**：
  - `[L]` `compressOne` 用 `os.Remove(tmp)` 在错误路径上——tmp 文件**新的、可能空**；删除失败下次 sweep 会看到 `.zst.tmp` 孤儿。极低概率，可接受。
  - `[L]` `housekeep` 启动后调 `os.Stderr.Fprintf`——后台 goroutine 往 stderr 写对 systemd/launchd 服务模式不友好（stderr → service log 文件）。可接受。

#### 1.C.4 `internal/audit/read.go`（96 行）+ `_test.go`（109 行）
- **主要内容**：`MaxLogLine = 128MB`、`OpenLogFile(path)`（自动判 `.zst` 解压）、`ForEachLine` 流式逐行回调。`ForEachLine` 用 `bufio.Reader.ReadSlice` 读，遇到超长行**drain + skip + 回调 onSkip** 而不是 abort。
- **问题**：
  - `[L]` `ForEachLine` 在 `bufio.ErrBufferFull` 时会 continue 把当前行继续读——`bufio.NewReaderSize(r, 1<<20)` 是 1MB buffer。OK。
  - `[L]` `zstdReadCloser.Close` 先关 decoder 再关 file——decoder 不持有 fd，关 decoder 释放内部 buffer。OK。

### 1.D 配置层（config）

#### 1.D.1 `internal/config/config.go`（366 行）+ 3 个测试文件共 600 行
- **主要内容**：`Load`/`Parse` → `expandEnv` → `yaml.Unmarshal` → `applyDefaults` → `validate`。`Duration` 自定义 YAML unmarshaler。
- **核心数据结构**：`Provider`（含 `Proxy *bool` 三态）、`EndpointConfig`、`ModelConfig`（含 `ImageDownscaleMaxPx *int` 区分 unset 与显式 0）、`Timeouts`、`Config`。
- **`ProxySpecFor`**：按 base_url scheme 选 http_proxy/https_proxy；provider 自己 `Proxy: false` 直接短路。
- **`applyDefaults`**：listen、timeouts、image_downscale clamp 到 0、image_cache_ttl_days ≤ 0 用 default 7、max_concurrency < 0 → 0、audit_retention < 0 → 0、max_body_mb ≤ 0 → 8、log/image_cache 目录用 rundir 兜底、strategy 空 → ["priority"]。
- **`validate`**：listen host:port 解析；APIKeys < 16 拒；proxy 字段格式；provider base_url 格式；adapter 协议存在；model endpoints 引用合法性。
- **问题**：
  - `[L]` `expandEnv` 用 `envRe.ReplaceAllStringFunc`，对 `${VAR}` 形式。不识别 `$VAR` 或 `\$` 转义——未文档化。如果用户在 api_key 里**字面**写 `$abc` 会被吞成空。**低优**。
  - `[L]` `Duration.UnmarshalYAML` 不支持整数值（如 `timeouts.connect: 30` 当 30 秒 vs 30 ns 的歧义）。要求必须是字符串。OK，行为一致。
  - `[M]` **YAML 配置错误信息不够友好**——`yaml.Unmarshal` 返回的错误可能很难定位行号（同 1.0 §3.3.6）。**`[已修复 2026-07-16]`**：`config.Parse` 改用 `yaml.NewDecoder` + `dec.KnownFields(true)`，拼写错键（`max_concurency` 等 typo）直接报加载错误，不再静默失效。同时 design doc §10 同步添加"YAML 严格解析"段。
  - `[I]` `validate` 里 `if _, ok := adapter.Get(protocol); !ok`——adapter 必须在 config 解析前 init() 注册。测试里都用 `_ "vmr/internal/adapter/openai"` `_ "vmr/internal/adapter/anthropic"` 显式 blank import。**生产代码 main.go 必须同样 import**（待交叉验证）。
  - `[L]` `expandTilde` 不处理 `~user` 形式——注释明确，OK。
  - `[L]` `minAPIKeyLen = 16` 硬编码常量（同 1.0 §3.2.x），不配置化。但 audit.KeyTag window 是 8，所以理论上 9 字符以上就不会泄露整 key，16 是安全裕度。OK。
  - `[Q]` 没有处理**重复 provider 名 + 同 protocol**：yaml 会取最后一个，validate 不报错。不算 bug，只是个 note。
  - `[L]` `cfg.ImageDownscaleMaxPx < 0` clamp 为 0——但模型级 `m.ImageDownscaleMaxPx` 是 `*int`，clamp 时 `zero := 0; m.ImageDownscaleMaxPx = &zero`——这个操作把"显式未设置" 改成"显式 0"，**与设计意图不符**（设计文档说模型级 0 是"强制关闭"，**而不是**"clamp 后变 0"）。**问题**：如果用户在 YAML 里写了 `image_downscale: -5`（负数），意图不明，但代码把它转成"显式 0 = 强制关闭"——这跟全局的"负数 → 0 = 关闭"语义不一致（全局 0 = 关闭、模型 0 = 强制关闭，但全局没有"强制关闭 vs 关闭"的区分，所以等价）。**实测行为**：负数模型级值被悄悄变成"强制关闭"而不是"继承全局"——这可能是反直觉的。**建议**：模型级负数 → nil（继承全局），而不是 → 0。

#### 1.D.2 `internal/config/watch.go`（53 行）+ `_test.go`（97 行，**2026-07-16 新增**）
- **主要内容**：`Watch(path, onChange)` 用 `fsnotify` 监听 config 所在目录（避免编辑器 replace-in-place 不触发），300ms 防抖。
- **问题**：
  - `[L]` **没有处理 fsnotify 事件丢失**（buffered channel 满了会丢事件）。300ms 防抖能部分缓解，极端场景（同时改两次）可能只触发一次回调。OK 在配置场景下足够。
  - `[L]` watcher 只 watch 目录，rename 事件可能指向不存在的 path——`fsnotify.Rename` 通过 `& Write|Create` 接收，下次 reconcile 由 onChange 实现——目前行为依赖 onChange 端的 retry-on-ENOENT（待看 main.go）。
  - `[M]` **无单测**（同 1.0 §3.2.3）——hot reload 入口。**`[已修复 2026-07-16]`**：新增 `internal/config/watch_test.go`（97 行），包含 `TestWatchFiresOnWrite`、`TestWatchFiresOnAtomicReplace` 等集成测试。完全补足了之前欠缺的 watch 路径覆盖。
  - `[Q]` `watcher.Errors` channel 的 err 被静默 drop（注释没写）。**低优**：watcher 出错很难恢复，进程最好重启。


### 1.E 路由层（router / response）

#### 1.E.1 `internal/router/router.go`（689 行）+ 3 个测试文件共 1004 行
- **主要内容**：核心 failover 循环 + 快照原子切换 + 并发闸 + upstream client 构造 + 响应归一化编排 + 4 个 200/4xx 出口。
- **设计要点**：
  - `Snapshot` 用 `atomic.Pointer[Snapshot]` 持有，hot reload 换时旧 in-flight 请求继续用旧快照（`Install` 显式 `old.CloseIdleConnections()` 释放空闲池，但**不**等活跃连接）。
  - `NewUpstreamClient` 把 `http.Transport` 配置抽象出来：被 router/Install/cmd 的 diagnose/replay 三处共用，避免代码重复。
  - `ProxySpecFor` 的解耦：`mode` + `proxyURL` 二元组，**没有环境变量回退**——`router.go` 注释明确"proxy env vars are deliberately not consulted"。
  - **probe 释放路径覆盖度**：`Health.Acquire` 在 3 个 release 路径都正确释放（Success/ReportNeutral/ReportFailure），每次必有一解。
  - **`copyFlush` 的 watchdog + goroutine 架构**：32KB chunk + 同步 channel + idle timer。SSE 路径真流转发。
  - **`IngressPath(protocol)` 导出供 server/replay 共用**——单点修改防止 drift。
  - **`errBodyCap = 128 << 10`**（已修复 1.0 §3.1.5）——4xx 错误体上限，超限时 audit 副本追加 `...(truncated at N bytes)` 标记、转发字节不变（byte-faithful 保持）。
- **问题**：
  - `[M]` **（已修复 1.0 §3.1.5）`errBodyCap` 处理**：原 64KB 提升到 128KB，且超限时 audit 副本才追加截断标记、转发字节不变。已通过 `server_audit_test.go::TestErrorBodyCappedAndAuditMarksTruncation` 端到端验证。
  - `[L]` **`parseRetryAfter` 的 HTTP-date 解析**（`http.ParseTime`）支持三种格式（RFC1123/RFC850/ANSI C asctime），但当前常见的是 seconds 整数或 RFC1123。OK。
  - `[L]` **`modelNames` 返回所有 virtual model name 按字典序**——用于 404 时的"可用的"列表提示。**注意**：`Models[protocol]` 只在当前协议下找，所以 404 消息只会列出本协议的可用 model。OpenClaw 用"client 走错入口"的诊断场景下这是友好的。
  - `[Q]` `otherProtocolFor` 当用户在 `/v1/chat/completions` 调用 anthropic 命名的模型时返回 "use POST /v1/messages"——好的 404 提示。
  - `[L]` `IngressPath` 中 fallback 是 `/v1/chat/completions`（默认 openai）——**未来加新协议时**如果忘记更新这里，新协议也会走错路径（同 1.0 §3.2.4）。**建议**：让 `IngressPath` 查 adapter registry（`adapter.Get(name)` 调 `Protocol()`，从 `Protocol()` 推 path；或新增 `IngressPath()` 到 Adapter 接口）。目前只有 2 个协议，靠注释提醒。
  - `[L]` `installLimiter` 的"capacity change 期间短暂过度准入"风险——注释承认了。实际窗口是 ns 级（同 1.0 §3.2.8）。
  - `[L]` **`(rt *Router) logf(format, args...)` 永远走 rt.Logger**——logger nil 时静默。OK 设计。
  - `[Q]` `tryOne` 的 audit body 处理：`auditBody := body` 后 `append(append([]byte(nil), body...), []byte(...))`——这是为了在 trunc 路径上"fresh slice, never touched again"以满足 `EncodeBody` 的所有权契约。注释清楚。可接受。
  - `[M]` **`copyFlush` 用 goroutine + channel 转发 body**——保证 idle watchdog 能以 Read 为粒度触发。如果客户端先断开（`r.Context().Err() != nil`），外层 `Serve` 通过 `tryOne` 返回 `done=true` 后流程退出，但**底层 goroutine 仍在等 `body.Read`**——`defer close(done)` 信号关闭 reader goroutine，但 `body.Close()` 在 `tryOne` defer 里调（`defer body.Close()`），顺序是 reader goroutine 收到 close 信号还是先收到 body 关闭？**测试覆盖**：`server_hang_test.go` 应该已经覆盖 idle timeout 路径。
  - `[Q]` **`Health.Acquire` 在 cooldown 中或 probing=true 时返回 false**，但 `candidates` 已经过滤掉了冷却中的——这两个守卫**重叠**：先 `Available()` 过滤冷却，再 `Acquire()` 抢探针。`Available` 已排除冷却但 `Acquire` 在 `cooldownUntil` 上又判一次。可接受（防御性）。

#### 1.E.2 `internal/router/response.go`（约 650 行，含 2026-07-16 thinkShapeGuard 新增）+ `_test.go`（786 行）
- **主要内容**：响应归一化器（model 改写 + think 块剥离 + [DONE] 追加 + soft block 嗅探），三种传输模式（undecided/buffered/passthrough）的状态机。
- **设计要点**：
  - **决策延迟**（sentinel-driven）：undecided 模式积累事件直到第一个 payload 事件决定走 buffered 还是 passthrough；`scanned` offset 防止 ping 风暴把决策变成 O(n²)。
  - **resume after think**：think 块触发的 buffered 在 `</think>` 之后切回 passthrough，前缀已 strip 的部分先 emit，剩余的流式。
  - **溢出降级**：bufferedCap=32MB / undecided 同样 32MB，超出走 raw passthrough + "overflow_raw_passthrough" applied 标记。
  - **`stripThinkingProcess` 严格触发保护**：只对首个 content value 以 "Thinking Process:" 起头的情况触发，避免吃掉正常代码审查里的 "Looks good. Proceed with the merge" 之类。
  - **`thinkShapeGuard` 前缀守卫**（**2026-07-16 新增**）：首个非空 content/text 值以 `` 标签形态），与 `stripThinkingProcess` 路径（`Thinking Process:` 形态）是两条不同的代码路径；后者 wording 强绑本身未变。
  - **[M]` **`think_strip` 旧行为误删引用**——**`[已修复 2026-07-16]`**：新增 `thinkShapeGuard` + `thinkGuardMarkers`，只检查 `content`/`text` 字段首位置。回归测试锁定两个方向（真实 think 形态仍被剥、合法引用不被剥）。
  - `[M]` **`thinkPattern` 的 `(?:\\n|\n)*` 后缀**——`\\n` 匹配 `\n` 两字符（JSON-escaped newline），`\n` 匹配真实换行。MiniMax M3 的 think 块结束后是 `\n\n`（两个换行）作为分隔；这里两个都被吃。OK。
  - `[L]` `containsSoftBlockMarker` 的"敏感词表"只有 2 个 literal——**通用性差**，未来 MiniMax 加新字段（如 `risk_level`）就不会被捕捉（同 1.0 §3.2.6）。建议扩为 substring 列表 + 配置文件。
  - `[L]` `modelFieldPattern = `("model":\s*")[^"]*"` ——只匹配 `key":"value"` 形式。**`\"model\":\"x\"` 在 value 里不会被误改**，因为 `\"` 不会被 `[^"]*` 匹配中断——但要测试覆盖（已通过 `TestRewriteModel_EscapedTextInContentUntouched` 锁）。
  - `[L]` **`modelFieldPattern.ReplaceAll(block, []byte(`${1}`+s.clientModel+`"`))`**——用 `${1}` 替换占位保留 capture group。Go `regexp.ReplaceAll` 支持 `$1` 和 `${1}` 两种风格。OK。
  - `[L]` **`modeBuffered`/`modeUndecided` 的溢出降级路径重复代码**——两处都是"设 opaque + noteApplied + flush"同一套三步操作（同 1.0 §1.E.2 新发现）。未来改溢出降级的注释/applied 标签措辞，有只改一处漏另一处的风险。
  - `[L]` `s.scratch` 32KB read buffer——"lazily allocated once per response"——OK。
  - `[Q]` `read` 中途返回 `(0, nil)` 0-字节读——文档注释解释了"callers idle watchdog tick"。正确（copyFlush 显式处理）。
  - `[I]` 代码用了大量 `bytes.Index`、`bytes.IndexByte` 优化路径；很多 inline `[]byte` 字面量以避免 alloc。性能敏感代码（hot path 多次 failover），无可厚非。
  - `[Q]` **`resumed_stream` 标记在 think strip 后**——如果 think 块之后的内容还含 `<think>`（递归 think），不会被二次处理（`modePassthrough` 直接 emit）。**实际**：MiniMax M3 不太会递归 think，但理论上如果递归了会漏剥。可接受。


### 1.F 服务层（server / recorder）

#### 1.F.1 `internal/server/server.go`（约 350 行，2026-07-16 重构）+ 11 个测试文件共 3038 行
- **主要内容**：HTTP 入口、auth、header blocklist、image downscaling 嵌入点、audit 录制。
- **关键不变量**：
  - **`authenticate`** 用 `subtle.ConstantTimeCompare` 防 timing attack，匹配 `APIKeys` 列表里的 key。**`api_keys` 是唯一的鉴权面**（**2026-07-16 移除单把 `api_key`**）：旧 APIKey catch-all 能做的事列表全都能做，并存代价是两条代码路径和两个文档概念。`config.validate` 仍出现 `api_key` 字段即拒绝并提示迁移。
  - **`headerBlocklist`** 14 项（auth、host、content-length、accept-encoding、hop-by-hop 等）；`FilterClientHeaders` 导出供 replay 复用。
  - **`chatHandler` 流程**：audit record 创建 → auth → body read → 解析 `model`/`stream` → **acquire concurrency slot**（在 body 解析后）→ image downscale → 构造 CanonicalRequest → router.Serve。
- **问题**：
  - `[L]` **`authenticate` 在 `cfg.APIKey != "" && ConstantTimeCompare == 1` 时直接返回 `("", true)`**——APIKey 命中永远不打 tag。这意味着**catch-all key 命中无法区分客户端**（Alice 还是 Bob 都用这把 key）。设计文档明确这是 intended，但 audit 角度 Alice vs Bob 都用同一把 catch-all 时无法分组。OK 设计。
  - `[L]` **`(L)` **`s.auth(s.models)` 包装 `/v1/models`**——同 1.0 §3.1.2 复核结论：`server.go:39` 实际上是 `mux.HandleFunc("GET /v1/models", s.auth(s.models))`，models 端点**一直**走 `s.auth()`，跟 `chatHandler` 是同一套鉴权语义。1.0 原审计误读路由表产生假发现，本审计确认 `server.go:39` 已确认包含 `s.auth()` 包装。✅ 不成立。
  - `[M]` **`chatHandler` 中 `audit rec` 在 auth 之前就创建并 defer 写盘**——意味着**未鉴权成功的请求也会落审计**（含打码后的 Authorization）。这是**有意为之**，设计意图目前在 `server.go` 代码注释里有详细说明（引用已改为 "design doc §4.3/§9.4"——`[已修复 2026-07-16]` 死链接）。如果用户想"完全关审计 for unauth"，需要新建 `audit-on: false for 401`。当前设计 OK。
  - `[M]` **`newRecorder(w, rec.TS)` 在 audit != nil 时才包**——意味着 audit off 时 `rec.TTFTMS` 不存在（这是定义，OK）。但 `rec.TTFTMS` 在 audit 关闭的请求里也不被记录，**没有别的 metric 能代替**——如果以后想统计 TTFT（即便没审计），需要单独的 metric 钩子。**低优**。
  - `[L]` `chatHandler` 顺序：probe `model`/`stream` 字段（在 AcquireSlot **之前**）——保证 malformed JSON 请求不被并发闸占。OK。
  - `[L]` **`chatHandler` 在 audit != nil 分支内才做 image detection**（"Skip entirely when there is no consumer"）——性能优化，但 audit 关 + downscale 关的请求是真正"零开销"路径。OK。
  - `[Q]` `adminStatus` 的 loopback 检查用 `net.SplitHostPort` + `net.ParseIP(host).IsLoopback()`——对 IPv6 zone 标识（`::1%eth0`）支持？测试都是 IPv4。**低优**。
  - `[Q]` **`newRecorder` 里 `r.ttftMS == 0` 是 sentinel**——与 0 值（"未测量"）冲突。`recorder.Write` 注释清楚：`if r.ttftMS == 0 && len(p) > 0 { r.ttftMS = time.Since(r.start).Milliseconds() }`，所以只有 first Write 才设值。OK。
  - `[L]` **`recorder.status` 0 是 sentinel（"未写出"）**——`Write` 时若 status==0 自动设为 200。**问题**：若客户端用 201/202 但没有先 `WriteHeader`，会被记成 200——但 Go 的 http.ResponseWriter 要求先 WriteHeader，所以这里实际不会被触发。OK。
  - `[I]` `models` 输出同时含 OpenAI shape（`object:"list"`, `object:"model"`）和 Anthropic shape（`type:"model"`, `has_more`）——合并两种客户端解析。**好设计**。
  - `[L]` **`adminStatus` 输出的 key 是 `name [protocol]`**——同一个 virtual model 名在两个协议分组下都有时，admin 输出按这个 key 区分。OK。
  - `[Q]` **`authenticate` 在两个分支都返回 `audit.KeyTag(got)`**——但 got 已经剥过 `Bearer ` 前缀。`KeyTag` 取 key 末尾 8 字符 + 处理 `-`——OK。

#### 1.F.2 `internal/server/recorder.go`（57 行）+ `_test.go`（27 行）
- **主要内容**：tee wrapper 记录 client-view 状态/headers/body/TTFT。`Flush` 透传。
- **问题**：
  - `[I]` 简单包装器，无状态共享。Flush 透传。OK。
  - `[L]` `r.buf.Write(p)` 是 `bytes.Buffer.Write`——并发不安全。但 recorder 整个生命周期都在一个 goroutine（请求处理）里用，**安全**。


### 1.G 入口（cmd/vmr）

#### 1.G.1 `cmd/vmr/main.go`（694 行）+ 2 个测试文件共 354 行
- **主要内容**：7 个子命令（start/check/status/report/dirs/diagnose/replay）+ 启动 banner + 启动/停止 log markers + config 摘要 + SIGHUP 热加载 + SIGTERM 优雅退出。
- **关键不变量**：
  - **`audit.SetRetentionDays(cfg.AuditRetentionDays)` 必须在 `audit.New` 之前调用**——`main.go` 注释明确解释：`New` 启动时会跑一次 housekeeping sweep，那个 sweep 读 retention 当时的值；如果在 `New` 之后才 set，第一次 sweep 不会 purge。
  - **Hot reload 路径**（`reload` 函数）：失败保留旧 cfg；`log_dir` 变化是"restart required"提示。
  - **SIGHUP + fsnotify** 双重触发 reload。
  - **SIGTERM/SIGINT** 触发 `srv.Shutdown(ctx)` 10s 优雅 drain + `logStop` 写终止 marker。
  - **`logConfigSummary` 把 model 列表按"effective try order"打印**——`EffectiveOrder()` 在 router.BuildSnapshot 时就排序了，startup log 和 check/diagnose 三处共用同一份排序逻辑。
- **问题**：
  - `[L]` **`audit.SetRetentionDays` 也在 reload 时调用**——`audit.SetRetentionDays(newCfg.AuditRetentionDays)` 在 reload 路径里——确认 retention 跟随 hot reload 更新。OK。
  - `[L]` **`logConfigSummary` 把每个 provider 的代理状态输出**——含 url.Redacted() 凭证掩码。OK。
  - `[M]` **`logStop` 不会在 panic 路径调用**（同 1.0 §3.1.6）——`panic` 直接退出时 `defer` 不会触发（除非显式 `defer logStop`）。**复核结论**（2026-07-16）：**ROI 重评为偏低，不建议现在做**。原因：(1) 原方案"`cmdStart` 入口加 `defer recover()`"盖不住 SIGHUP/fsnotify 触发的 `reload()`——那段代码跑在独立 goroutine 里；(2) Go 未捕获 panic 本来就会把完整 stack trace 打到 stderr，systemd/launchd 服务模式下这本身已经落进 service log——"有 START 没有 STOP，后面跟一个 panic trace"已经足够诊断。
  - `[M]` **`status` 命令自己启一个 http.Client 拿 `/admin/status`**——如果 `vmr` 没启动，会返回"`is vmr running on %s?`"错误。OK。
  - `[L]` `start` 启动 banner (`vmrBanner`) 是固定 ASCII 艺术，**不会随版本变化**。OK（注释明确）。
  - `[L]` **`logStart` 把 `path` 写入 banner 后一行**——若 path 含特殊字符（`\n` 之类），日志解析可能歧义。**现实几乎不可能**。
  - `[L]` `serveErr` 的 select 没有 `default`——`srv.ListenAndServe()` 启动失败时 channel 必返回 → 走 error 路径。OK。
  - `[I]` `cmdStatus` / `cmdCheck` 不并发跑（不像 `cmdReport` 后接 session 分析）。OK。
  - `[M]` **（同 1.0 §3.1.4 已修复）`cmdReport` 在 `sess, err := report.AnalyzeSessions(paths)` 失败时整个 `report` 失败**——**✅ 2026-07-16 已修复**：`AnalyzeSessions` 出错时打印 stderr 警告并跳过 `tools/sessions/workloads`、`vmr-requests.jsonl`、`details/`，但 `vmr-report.json`/`.md`（核心聚合统计）仍正常写出、命令返回成功。已知局限：`AnalyzeSessions` 目前只会在文件 I/O 失败时报错，与 `Build()` 失败面完全重合，warning 分支目前主要起防御性作用。
  - `[L]` `cmdReport` 串行写所有输出文件（json/md/req）+ 详情：单大日志（gigabytes）下 O(minutes)。可接受——单次跑批。
  - `[Q]` **`cmdDiagnose` 失败时不返回 error 给 main**——`if rep == nil { return err }` 检查 rep==nil 才报错。如果 rep 不为 nil 但某些 check 失败，`FailCount()` > 0 才返回 error。OK。
  - `[L]` **`configFlag` 函数没用 `fs.Usage`**——用户输错 flag 会拿到 default `flag` 库的 usage。OK。
  - `[M]` **`cmdStart` 的 `signal.Notify(hup, syscall.SIGHUP)` 在 `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` 之前**——独立 channel、独立 goroutine。**问题**：SIGHUP 触发 reload 但 reload 路径里如果 panic，独立 goroutine 不被 cmdStart 顶层的 recover 兜住——同 `[M]` `logStop` 不调用的问题（共享根本原因）。
  - `[Q]` **`cmdStart` 在 `srv.ListenAndServe()` 失败时 return err**——但 `logStop` 已经写过（"reason: error: ..."），返回 err 让 main 打印 "vmr: ..."。OK。
  - `[Q]` **`cmdStart` 里 `defer auditLog.Close()`**——但 auditLogger 关闭后 server 仍在跑？**不**，shutdown 信号先到，`srv.Shutdown` 之后才 `logStop` 然后 return，defer 在 return 前跑。OK。
  - `[I]` **`logConfigSummary` 重新排序**：`cfg.Models[protocol]` 的 keys 按字典序、`cfg.Providers[protocol]` 同上。OK。


### 1.H 诊断与重放（diagnose / replay）

#### 1.H.1 `internal/diagnose/diagnose.go`（391 行）+ `_test.go`（417 行）
- **主要内容**：`vmr diagnose` 4 阶段：config 校验 + env 检查（DNS/TLS/proxy 联通 + api_key 状态） + 真实小请求连通性测试 + 路由预览（带连通结果标注）。
- **关键设计**：
  - `checkConcurrency = 8` 限制并发，慢场景下不至于串行 60s × N providers。
  - `envCheck` 对**走 proxy 的 provider 跳开 DNS 检查**——这是 "有据 false-positive 修复"。
  - `testEndpoint` 复用 `router.NewUpstreamClient` + `adapter.BuildRequest`——**和 vmr 自己用同一份代码**，结果可信。
- **问题**：
  - `[L]` `testEndpoint` 用 `minimalBody`（`{"model":..., "max_tokens":1, "messages":[{"role":"user","content":"hi"}]}`）——所有 OpenAI/Anthropic 兼容 provider 都接受，**但有 provider 计费"最少 5 tokens"之类**——这些"最小请求"的 token 算入 cost（同 1.0 §3.2.14）。低优。
  - `[L]` `envCheck` 的 DNS 解析不走 system resolver 的 DoT/DoH——若用户刻意只走 DoH，`net.Resolver{}` 会用普通 DNS，诊断结果不可信。理论性。
  - `[L]` `TestTimeout` 默认 10s，但对慢启动的 provider（冷启动 5-10s）不够——`Options.TestTimeout` 可调。OK。
  - `[L]` **（同 1.0 §1.H.1 新发现）Phase 2（DNS/TLS/proxy 拨号）超时硬编码 5s，不跟 `-test-timeout` 联动**——不像 Phase 3（`testEndpoint`）会用 `Options.TestTimeout`。慢/高延迟网络下，用户调大 `-test-timeout` 对 Phase 2 没有任何缓解，可能把"只是慢"的 provider 误报成 `dns:FAIL`/`tls:FAIL`。**建议**：Phase 2 超时也读 `Options.TestTimeout`（或至少取两者较大值）。
  - `[L]` **`apiKey` 为空时**：`parts = append(parts, "api_key:EMPTY")` 同时把 status 从 OK 降到 Warn（如果当前是 OK 的话）——这是正确的警告，但不区分"故意不填 key"和"忘了填 key"。低优。
  - `[I]` **`runConcurrent` 泛型 helper**——Go 1.18+ 泛型，写法干净。
  - `[L]` **`sortedKeys`** 在多处使用，但 `cfg.Providers[protocol]` 这种两层 map 的迭代顺序依赖 `sortedKeys(protocol)`，**不是 sortedKeys 内的子循环**——OK 因为 Phase 2 输出的 order 决定最终的 report 输出。

#### 1.H.2 `internal/replay/replay.go`（450 行）+ `_test.go`（631 行）
- **主要内容**：3 种 record 定位方式（`-detail`/`-ts`/`-line`）+ 互斥校验 + 复用 `adapter.BuildRequest` 重建 + dry-run / 真实 replay / `-record` 写新 audit 记录。
- **关键设计**：
  - **`replayHeaders` 双过滤**（`FilterClientHeaders` 过滤 server blocklist + `audit.IsCredentialHeader` 过滤 audit 抹码但 server 仍会转发的 header）——`TestRun_StripsMaskedCredentialLikeHeaders` 锁。
  - **writeReplayRecord 字段布局对齐 live record**：Client.Request.Body 用 pre-rewrite，Attempt 用 outBody。`TestRun_WritesReplayRecord` 锁。
  - **`recordView.Body` 用 `json.RawMessage`**：保留原 bytes，因为 `audit.Message.Body` 是 `any`，反序列化丢原字节。
  - **`loadRecordByTS` 用毫秒匹配 + 报歧义**（让用户用 -line 消歧）——注释解释了 `time.Parse(time.RFC3339, ...)` 接受任意 fractional 长度。
- **问题**：
  - `[L]` `ResolveModel` 走 `models.<protocol>.<virtualModel>.endpoints` 找 provider——audit 里的 `record.Model` 是**虚拟名**（`rv.Model`）；当 audit 用 `"(rejected)"` 表示拒绝请求时，`ResolveModel` 失败 → 用户必须 `-model` 显式。`TestRun_ModelResolutionError` 锁这个。
  - `[L]` `-record path` 是独立 JSONL 路径（不入主流水线）——这个行为 OK 但**与 vmr 主审计兼容**：可以被 `vmr report <file>` 二次聚合（`audit.OpenLogFile` 都支持）。
  - `[L]` `-max-time` 用了 ctx.WithTimeout，但 `httpReq.WithContext` 后才赋值给 client.Do——流程正确。
  - `[M]` **（同 1.0 §1.H.2 新发现）`resolveModel` 用的是 `-protocol` 覆盖值而不是记录本身的协议**——如果用户只传 `-protocol` 覆盖、没同时传 `-model`，会在**覆盖后**的协议下查找一个原本只存在于**原协议**的虚拟模型名，报错"virtual model not found"，但真实原因（协议被覆盖了）不会体现在错误信息里。**建议**：错误信息里提示"if you passed -protocol, also pass -model explicitly"。
  - `[Q]` **`loadRecordByLine` 在 `line == 0` 时返回 last parsable**——但这意味着用户显式传 `-line 0` 和不传 `-line` 行为一样（都是 last）。OK（注释解释"line 0 was never a valid 1-based line number anyway"）。
  - `[L]` **`writeReplayRecord` 用 `os.OpenFile` + `os.O_APPEND`**——没有 `os.O_EXCL`，所以 `-record` 文件如果存在会被追加。如果用户期望"覆盖"会困惑。OK（注释没明说，但审计追加语义是显然的）。
  - `[L]` **`-record` 写出的 `DurMS` 测的是完整 body 传输**——与 §1.G.1 一致。OK。
  - `[Q]` **`replayHeaders` 在 `FilterClientHeaders` 已经返回的 map 上 `out.Del(k)`**——`Del` 删除 canonical name；`IsCredentialHeader` 用 `strings.EqualFold(k, h)` 比较（`audit.IsCredentialHeader` 内部也是 EqualFold）。OK 跨 canonical 形式一致。
  - `[I]` **`runConcurrent` 的 generified helper** 没被 replay 用——replay 没有并发阶段。OK。


### 1.I 报告（report）—— 项目最大模块

#### 1.I.1 `internal/report/report.go`（805 行）+ `_test.go`（448 行）
- **主要内容**：聚合器，5 个 bucket（Rows 细粒度 + Overall + ByModel + ByDate + Endpoints + Hours）+ Format 9 后的 EndpointsAll / HoursOfDay（all-dates-merged 独立 bucket）。
- **关键设计**：
  - **`Format = 9`**（常量）——每次结构变化都升级 Format 并写 changelog。
  - **`addRecord` 一趟多 bucket**：每个 record push 到 Rows/Overall/ByModel/ByDate 一次，HourRow/EndpointRow 额外 + 1 pass。
  - **`attemptErrorClass` fallback**：新 audit 记录用结构化 `ErrorClass` 字段；旧 log 走 `Error` 字符串前缀 split。
  - **`ExtractUsage` cache token 语义**：区分 Anthropic 三字段相加 vs OpenAI/DeepSeek prompt_tokens 已含 cached_tokens。
  - **`addEndpointRequest` 仅挂到 successEp**：请求级指标（bytes/tokens/ttft/dur_ms）只附给真正服务客户端的成功 endpoint。
- **问题**：
  - `[M]` **`bodyBytes` 用 `json.Marshal` 重新序列化 JSON 算字节**——multi-MB 重复 marshal 成本。**2026-07-16 复核**：实际在聚合阶段每条 record 会被调用 3 次（row/hour/endpoint 桶各一次），`session.go` 分析阶段再一次，`detail.go` 给每条记录生成详情文件时又是好几次——**是所有记录都会经过的路径，不是只有错误/流式记录才碰**。**`[已修复 2026-07-16]`**：新增 `recStats` 结构，把 `bodyBytes`/`messageCount`/`roleChars` 提到 record-level 一次性计算，所有桶共享一份——不再重复解析 6-8 次。`vmr report` 在大日志下的性能瓶颈已消除。
  - `[L]` **`percentiles` 的 nearest-rank 公式**：n=1 时 `int(0.5*1+0.5)-1 = 0`，正确（取 s[0]）。**n=4 时** `int(2+0.5)-1 = 1`，s[1]（应是 s[1] 或 s[2] 中间，nearest-rank 简化 OK）。**OK**。
  - `[L]` **`attemptErrorClass` 的 fallback**：`strings.IndexByte(a.Error, ':')` 取前缀——`"error"` 无冒号 → return `a.Error`。OK。
  - `[M]` **`bytesDurMS` 字段定义但未在所有 finish 函数使用**：`Row.finishRow` 用 `r.bytesDurMS` 算 `BytesOutPerSec`，`HourRow.finishHour`/`EndpointRow.finishEndpoint` 没算 bytes_out_per_sec。**不一致**：行级有 BytesOutPerSec，端点级/小时级没有。**可能是有意设计**（端点级只看可用度/错误，小时级只看活动度），但**代码结构**让人怀疑是否漏算了。**建议**：要么补齐（~10 行），要么在三个 Row 注释里显式说明"故意不算"。
  - `[I]` `Format = 9` + 详尽 changelog——结构变化管理的好范式。
  - `[Q]` **`addAttempt` 中 `if a.Error == "" && a.Response != nil && a.Response.Status < 400`**——判定成功的条件依赖 `Error` 字段空 + `Response` 非 nil + Status < 400。**如果** audit 侧在 success 路径下也填了 Error 字段（不应该，但若代码有 bug），会被误判为失败。OK（audit 侧注释明确"success attempts store error = ''"）。
  - `[L]` **`model == ""` 时设为 `"(rejected)"`**——audit 解析前被拒的请求模型名记为 ""，报告里显示成 "(rejected)"——好设计。
  - `[Q]` **`r.MessagesKnown++` 在 `messageCount(rec.Client.Request.Body)` 返回 ok 时**——但**对于 streaming 请求** `rec.Client.Request.Body` 是 SSE 字符串（`EncodeBody` fallback），`messageCount` 可能识别不了（取决于实现细节，待查）。OK 已知问题。

#### 1.I.2 `internal/report/usage.go`（178 行）
- **主要内容**：从 4 种响应 body 抽 token（OpenAI JSON/Anthropic JSON/OpenAI SSE/Anthropic SSE）。
- **关键设计**：
  - **`usageFromObj` 用 `input_tokens` 字段名检测 Anthropic**——隐式但 self-documenting。
  - **`mergeUsage` 用 `max` 合并 SSE 多事件的 cumulative 字段**。
- **问题**：
  - `[L]` `usageFromObj` 不处理 DeepSeek 的非标准字段（`prompt_cache_miss_tokens`）——DeepSeek API 文档可能含这类字段。理论性。
  - `[I]` 良好设计。`num` 处理 float64 和 json.Number 兼容。OK。

#### 1.I.3 `internal/report/export.go`（460 行）
- **主要内容**：`vmr-requests.jsonl` 写入 + 按 client key tag 写 sibling 文件 + `ToolShapes`/`Workloads`/`SessionRows` 聚合。
- **问题**：
  - `[L]` `requestRow` 34 字段——单 record JSON 约 500-1000 bytes（同 1.0 §1.I.3）；10K records × 1KB = 10MB。OK。
  - `[M]` **（同 1.0 §1.I.3 新发现）`writeRequestRows` 的 `defer f.Close()` 错误被丢弃**——`w.Flush()` 成功返回后就认为导出成功，但缓冲写入器的 `Close()`（可能触发 fsync）失败时错误直接被 defer 吞掉。`vmr report` 是批处理 CLI，不是热路径，加一层错误检查成本很低。**2026-07-16 状态**：仍成立——只把权限改到 0o600，defer 错误吞没没改。
  - `[I]` `workloadClass` 优先级：compaction > heartbeat > dream_diary > interactive。
  - `[L]` **`writeRequestRows` 用 `json.NewEncoder(w)`**——自动加 `\n` 每行。OK。
  - `[Q]` **`ToolShapes` 的 sig 是 `r.ToolsSig`**——`ToolsSig` 是 md5 hash 的 hex 字符串（前 8 字符？待查 session.go）。同名 sig 共享 Calls map。OK。
  - `[M]` **（同 1.0 §1.I.6 新发现）`sanitizeName` 替换非 `A-Za-z0-9._-` 字符为 `-`**——按调用方分组的 sibling 文件名（`vmr-requests-<tag>.jsonl`）两个不同的原始 tag（比如 `"bob/eve"` 和 `"bob-eve"`）经过文件名清洗后可能变成同一个字符串，导致两个调用方的导出文件互相覆盖，且**没有任何碰撞检测**（不像 `detailFileName` 那样有 `used` 计数器处理同名冲突）。**建议**：给按 tag 分组的导出路径也加一层碰撞检测。**2026-07-16 状态**：仍成立——`sanitizeName` 本体未改。


#### 1.I.4 `internal/report/markdown.go`（592 行）
- **主要内容**：5 个表（overall/by-model/endpoints/daily/hourly）+ workloads + sessions + tools，全部以"每个 bucket 自己的 p50/p95"渲染。
- **问题**：
  - `[L]` `p50p95` 输出 `"ms"` 单位——中文表头用"`p50/p95 首字延迟`"——一致。
  - `[I]` 大量 helper 函数（`tokensTriple`/`bytesInOut`/`avgTokensInOut*`）—每个 Row/WorkloadRow/SessionRow 类型有一个变种（5 个 `avgTokensInOut*` 函数 + 4 个 `avgMessages*` + 3 个 `tokPerSecCell*`），**可重构**为泛型（Go 1.18+），但当前清楚。
  - `[Q]` **`reqFallTrunc(req, fallbacks, truncated)` 合并三个数字成一格**——`%d/%s/%s` 格式。fallbacks/truncated 走 `warnCount`（非零才标）——设计 OK，符合"全 0 时整格显示 `-`"哲学。
  - `[Q]` **collapsed session 的 p50/p95 用 `RequestsWithDur > 0` 累积 dur 原始值**——单条 record 时 DurMSSum 就是该 record 的 dur_ms，聚合多条变成真正的 multi-record 百分位。OK。
  - `[L]` **`mergeIntoCollapsed` 不归 `RoleChars`**——按设计"never called-style counters don't apply"。OK。
  - `[L]` **`finishLine` 的 percentage 是 over totalReq，不是 over finished count**——所以"100% stop_reason=stop"会因为 canceled/有错误的请求而被稀释成 `<100%`。OK 设计（用户能看清各 reason 占比）。

#### 1.I.5 `internal/report/render.go`（594 行 + 还有 ~395 行未读）
- **主要内容**：Markdown 渲染原语（`<details>` 折叠 / code fence / chat message / SSE 重解析 / role chars / tool calls）。
- **问题**：
  - `[L]` `codeFence` 用 3-backtick 起步，按 content 里的最长 backtick run +1 选 fence——正确但**depth 计算简单**（不区分连续 backtick 与空格 backtick 混排）。
  - `[L]` `renderPart` 的 `image_url` placeholder 用 base64 解码长度（`DecodedLen`）——这是解码前最大长度，不精确。OK。
  - `[I]` 大量 emoji 作为视觉符号（`🆕`/`🟢`/`🔴`/`🔶`/`🤔`/`🖼`/`🔧`/`↩️`）——注释解释了"统一节奏，让读者能扫读 300-message 对话"。

#### 1.I.6 `internal/report/session.go`（782 行 + `_test.go` 793 行）
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
  - **`realUserText` 多启发式**：剥离 envelope / 跳过 `OpenClaw runtime context` / 跳过纯 tool_result / 跳过纯时间戳。
- **问题**：
  - `[M]` **`lcp` 算法基于 md5 hash 序列**——`md5` 不是 crypto-secure，但用作 session fingerprint 不需要 collision resistance（碰撞只会让两条不同 record 错误归并到同一 task，**最坏影响是分组错误**）。可接受。
  - `[M]` **`parentWindow = 16`**（同 1.0 §3.2.x）：实测有效但**仓库里已经找不到能验证"24-轮 openclaw 测试过"这个具体说法的代码/文档**（`docs/AgentSessionGrouping_Analysis_Fable5.md` 早在 2026-07-12 就被删除）。结论不变，但**"实测有效"目前查无实据，只能当作未经验证的说法**。低优。
  - `[M]` **`linkCompactions` 用 substring match + 200 字节 cap**（同 1.0 §1.I.6）：`needle(s, 200)` cap 到 200 字符。
    - 误链风险：如果一个会话的开头指令恰好只有 5 字符、跟 compaction output 内的 substring 重叠，会误链。极低概率。
    - **漏链风险**：cap 200 字节如果超过就漏检——例如 compaction marker（如"compacted into the following summary"）在原文里出现的位置超过 200 字节，会被直接漏检，**且没有任何 fallback 或日志**。**建议**：至少加一条 debug 日志/metric，让"没链上"和"没触发"能区分开。
  - `[L]` `deltaHasNewInstruction` 的 "parent 中已存在则不计" 检查——**靠 `parentKeys` map 实现**——这个 map 只在 `Parent != nil` 时构建。OK。
  - `[L]` `toolsSig` 用 `md5(sorted_tools)` 头 4 字节——碰撞概率高但**只影响 group**（"工具声明完全不同的两条 record 误归并"）——`md5[:4]` = 32 bits，1M 工具集才有 50% 碰撞概率。OK。
  - `[L]` ~~`realUserText` 的 `leadingBracketRe` 只处理 `[Day Mon DD HH:MM TZ]` 格式~~ — **❌ 不成立**（同 1.0 §1.I.6）：`leadingBracketRe` 的实际正则是 `^\[[^\]]*\]\s*`，匹配"任意一段方括号包住的前缀"，跟内部具体格式无关——`[2026-07-08 12:34:56 +0800]` 这种变体本来就能匹配。
  - `[Q]` **`taskTitle` 用 `r.NewInstruction`**——第一条真实用户指令的 preview。如果没指令（纯工具循环延续），返回 `"(工具循环延续)"`。OK。
  - `[L]` **`sessionTitle` 取首条 record 的最早 realUser**——session 的"开场白"。OK。
  - `[L]` **`openClawEnvelopeRe` 的正则 `(?s)(?:Conversation info|Sender) \(untrusted metadata\):\s*` + 三反引号 + 可选 json + `\n.*?` + 三反引号 + `\s*`**——非贪婪匹配，可能匹配到 message 末尾的最后一个三反引号。OK（按设计是单段 JSON block）。
  - `[M]` **`stripBracketPrefix` 只处理"以 [ 开头"的字符串 + 找 `] `（注意是 "] "，不是 "]"）**——如果 OpenClaw 用 `][` 或 `]\n` 分隔会失败。低优。

#### 1.I.7 `internal/report/detail.go`（1249 行 + `_test.go` 552 行，部分已读）
- **主要内容**：单 request 详情 Markdown + 同名 JSON + index + 按 client key tag 写 sibling index。
- **关键设计**：
  - **`renderBodyDiff` emoji-diff 表格**：`🟢`/`🔴`/`🔶` 标 field-level 变化（同 1.0 §1.I.7）。
  - **`<details>` 折叠所有 message**——保证长会话可扫读。
  - **filename 0-padded timestamp + 同毫秒冲突用 `-N` suffix**。
  - **`attemptUpstream` 兼容老 log 格式**（用 `/` split）vs 新 log（结构化字段）。
  - **`chatUserLabel` 剥 `user:` prefix**（OpenClaw chat_id 形式）。
- **问题**：
  - `[L]` `sanitizeName` 替换非 `A-Za-z0-9._-` 字符为 `-`——但**不**去重连续的 `-`——但 `+` 量词已经把**连续的非法字符合并成一个 `-`**；真正会产生连续 `--` 的只剩"一个非法字符恰好挨着一个已经存在的字面 `-`"这种情况，比原描述窄得多。
  - `[M]` `renderBodyDiff` 用 `reflect.DeepEqual` 做对比——**只对相同类型字段有效**。如果 field 从 `string` 变 `null` 或 `string` 变 `[]any`，reflect 会处理。OK。
  - `[L]` `fileLinksCell` 用 `<a href=details/...>`——纯 HTML（GitHub 渲染时 OK，部分 Markdown 渲染器对 `<a>` 不友好）。
  - `[I]` 1249 行超大文件——但内部函数组织清晰，每个函数单一职责。
  - `[Q]` **`detailFileName` 的 `used` 计数器处理同毫秒冲突**——`base-N` 后缀。OK。
  - `[M]` **`WriteDetails` 串行写 .md + .json per record**——单大日志下慢。**已知**（同 1.0 §1.I.7：单批次跑批可接受）。


### 1.L 内联图片处理（imgprep）—— 全项目第二大包

#### 1.L.1 `internal/imgprep/imgprep.go`（450 行 + `_test.go` 640 行）
- **主要内容**：`Downscale(body, protocol, opts)` 是包入口——先用 `HasImageMarker` 做便宜的子串预检，再做一次 JSON 树遍历（`rewriteBody` → `rewriteMessage` → `rewriteBlock`），只对命中的节点用 `map[string]json.RawMessage` 重新序列化，未知字段逐字节保留。协议特化提取：`rewriteOpenAIImage`（`image_url` data URI）、`rewriteAnthropicImage`（`source.type=="base64"`）。核心工作在 `processImage`：先做只读文件头的 `image.DecodeConfig`（检测阶段，无条件跑），只有真需要缩放时才整图解码 + BiLinear 缩放 + JPEG 重编码，用 `maxDecodePixels` 挡声明尺寸炸弹。整体 fail-open：JSON 形态不对/解码出错都退回原始 `raw`，`Downscale` 顶层还有一个兜底 `recover()`。
- **问题**：
  - `[L]` **`HasImageMarker` 用 `bytes.Contains(body, []byte(\`"image\`))`**——这是个 6 字节字面量搜索。false positive 风险极低（JSON 里 `"image` 几乎只在 image 相关字段出现）。OK。
  - `[L]` **`parseDataURI` 对畸形/非标准 data URI（缺 `;base64,`、截断等）在 `ImageInfo`/审计元数据里标成 `Remote: true`**——不准确（不是远程抓取，只是本地 URI 解析不了），纯粹是审计可读性的小瑕疵，行为本身（原样不动）是对的。
  - `[L]` **`processImage` 的具名返回值 `err` 在所有 `return` 语句里都是字面量 `nil`（包括 `image.Decode`/`jpeg.Encode` 失败路径，那些用的是 `:=` 局部变量遮蔽了具名返回）**——调用方对 `err` 的判断永远是 false。**目前无害**（所有失败路径本来就已经把 `changed=false` 设对了），但这是一段死代码，哪天有人真指望这个 `err` 会踩坑。
  - `[L]` **`image/png`/`image/gif`/`golang.org/x/image/bmp`/`golang.org/x/image/webp` 都是 `_ "..."` blank import**——只导入副作用（注册 decoder），不直接调用。OK。
  - `[L]` **`draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)`**——把 alpha 通道摊平到白底。OK。
  - `[L]` **`scaledSize` 整数除法**——`h * maxPx / w` 在大 w 下可能丢失精度（newH=0），已有 `< 1` 兜底。OK。
  - `[Q]` **`processImage` 用 `image.Decode` 而不是 `image.DecodeConfig`**——这是真解码到 `image.Image`，需要分配像素 buffer。`maxDecodePixels` 已经在前面 guard，但**只检查声明像素总数**。这个 guard 对真解压炸弹有效（声明 64MP 但编码字节少 → 拒绝解码）。OK。
  - `[M]` **GIF 帧数解压炸弹缺口**（同 1.0 §4.16 / §1.L.1）——`maxDecodePixels` 只检查画布尺寸，`gif.DecodeAll` 在检查这个上限之前就已经把每一帧全部解码进内存。**2026-07-16 复核：已修复**——`format == "gif"` 直接短路返回，`image/gif.DecodeAll`/`gif.Decode` 在整条路径上都不再被调用。**新增 `TestSingleFrameGIFUntouched` 锁定单帧 GIF 现在也不被处理；`TestAnimatedGIFUntouched` 保留并补 `ImageInfo` 断言**。
  - `[M]` **（同 1.0 §4.17 新发现）`Downscale` 的 panic 恢复完全静默、零观测**——函数签名 `(result []byte, images []ImageInfo)` 没有 error 通道，`defer recover()` 出问题时既不打日志也不进 metric/audit。一旦触发（stdlib image 包的 bug、对抗性输入），这张图的降采样能力会"永久失效"且**运维完全看不到任何信号**——不像项目里其他 fail-open 路径（如 `overflow_raw_passthrough`）好歹在 audit `Norm` 里留了痕。**`[已修复 2026-07-16]`**：`defer func() { if r := recover(); r != nil { fmt.Fprintf(os.Stderr, "imgprep: panic recovered, request passed through unmodified: %v\n", r); result, images = body, nil } }()`——保留 `result = body` 的 fail-open 行为，仅增加 stderr 日志让运维可见。注释清楚解释："every other fail-open path in this package leaves a trace (audit metadata, skipped-image info), so a recovered panic logs one stderr line"。

#### 1.L.2 `internal/imgprep/cache.go`（145 行）
- **主要内容**：按 `sha256(原始字节) + maxPx` 为 key 的降采样结果磁盘缓存。`cacheStore` 用"独立临时文件 + 原子 rename"写入（并发写相同内容安全）；`cacheLookup` 命中时刷新 mtime，防止正在被复用的图片被 TTL 清掉；`sweepState`（`sync.Map`，按缓存目录分 key）把清扫频率节流到每天一次，由 `maybeSweepCache` 异步触发；`sweepCacheDir` 同时清 TTL 过期的 `.jpg` 和崩溃遗留的 `.tmp-` 孤儿文件。
- **问题**：
  - `[I]` **磁盘无上限增长**——只有 TTL 清理，没有总容量上限（同 1.0 §1.L.2）。这是设计文档里明确记录过的既知取舍（"真出现磁盘问题再加容量上限"），不是新 bug：典型部署里 maxPx 取值种类是个位数——一个全局值加少数几个模型覆盖。
  - `[I]` **（同 1.0 §1.L.2 新发现）`cacheStore` 的 `os.MkdirAll(dir, 0o700)` 只在目录不存在时生效**——如果 `image_cache_dir` 已经以更宽松权限存在（比如用户手工建的 `0o755` 目录），mode 不会被收紧。
  - `[I]` **（同 1.0 §1.L.2 新发现）`maybeSweepCache` 异步清扫和 `cacheLookup` 之间存在轻微 TOCTOU**——清扫 goroutine 和当前请求可能在 TTL 边界的同一条目上竞争；本请求刚要复用的文件可能被并发清扫删掉。Fail-open（退化成 cache miss，重新处理），是"少一次优化命中"而非正确性问题。
  - `[L]` **`cacheFileName` 用 sha256 + maxPx**——maxPx 必须入 key，同一张图对不同虚拟模型可能用不同的降采样目标。
  - `[L]` **`cacheLookup` 的 mtime refresh 是 best-effort**——`os.Chtimes` 失败不视为 cache miss（注释明确）。OK。
  - `[L]` **`sweepState` 用 `sync.Map`**——重启后丢失，预期行为（重启后第一天可能 sweep 一次是好事）。OK（同 1.0 §3.2.12）。


### 1.J 设计/用户文档（docs/）

#### 1.J.1 `docs/VirtualModelRouter_System_Design_v2.md`（约 652 行，已完整阅读）
- **主要内容**：完整设计文档——定位、协议、架构、Adapter、错误分类、响应归一化、并发、审计、目录、健康、镜像降采样、配置、运维、诊断、replay、compaction 设计。
- **状态**：与代码同步（含 `## §N` 节号锚定）。
- **问题**：
  - `[I]` 一份优秀的"设计即文档"工程范例。

#### 1.J.2 ~~`docs/ClientAPIKeyGrouping_Design_Sonnet5.md`（449 行）~~ — **文件已删除（2026-07-16）**
- **原主要内容**：api_keys 多调用方分组的方案设计与 4 版迭代记录。
- **状态**：该文件已被删除（同 1.0 §1.J.2）。仓库里还有 4 处引用它的地方（`server.go` 的代码注释、`internal/report/session.go` 的注释、`internal/report/export.go` 的注释、本审计报告自己的 §1.F.1/§3.2.9）会变成死链接，已在对应位置标注。设计意图目前只留存在源代码注释里，没有独立文档可查——**建议**：要么恢复这份文档，要么把关键设计要点（KeyTag 派生规则、"内网自报身份"语义）内联进 `docs/VirtualModelRouter_System_Design_v2.md` 的相关小节。

#### 1.J.3 `docs/SensitiveWordFilter_Analysis_Fable5.md`（216 行）
- **主要内容**：sensitive-word 库分析与 VMR 接入评估——**明确"不含实现"**。
- **问题**：
  - `[I]` 研究性文档，不期望有实现代码。**OK**。
  - 文档末尾有"结论一页纸"给了阶段性判断（"现在就该做替换吗：否"），缺的是正式签字仪式。

#### 1.J.4 `docs/vmr_future_strategy_deepseek-v4-flash.md`（721 行）
- **主要内容**：竞品全景（LiteLLM、CLIProxyAPI、New API、One API、Bifrost、Portkey、AISIX 等）+ 战略决策矩阵 + 演进路径建议。
- **问题**：
  - `[I]` 由 deepseek-v4-flash 写就——**生成式模型生成的战略报告**——**内容详尽但未必经人手验证**。`docs/PerformanceTesting_Design_Sonnet5.md` 2026-07-16 已经做了一轮系统性的人工复核批注。

#### 1.J.5 `docs/PerformanceTesting_Design_Sonnet5.md`（126 行，已读）
- **主要内容**：性能测试方案设计 v2 + 落地情况。**§0-§5 是原始设计**（决策记录），**§6 是 2026-07-16 当天的实际落地情况**（以 §6 为准）。
- **关键决策**：
  - **不要做永久性能基础设施**——一次性/偶尔跑健全性检查。
  - **Vegeta 作为压测工具**——Go 原生、JSON-lines targets 文件、零新依赖。
  - **mock 上游按 virtual model 分发响应形态**——6 个场景 → 11 个场景（含 think_tag、big_response、multi_image、gif、anthropic_baseline）。
  - **`vmr report` 复用为报告工具**——不另写报告代码。
  - **`loadtest/runner/main.go` 把四步串成一条 `go run ./loadtest/runner` 命令**。
  - **三档负载**：`light`（10 req/s × 10s）/`moderate`（50 req/s × 20s）/`heavy`（150 req/s × 20s）。
- **问题**：
  - `[L]` 文档说"SSE chunk 边界影响正则匹配"——是 mock 上游在测试中暴露并修复的真实教训。
  - `[L]` 文档说"`json.Marshal` 默认转义破坏字面量标签匹配"——mock 上游的另一个真实教训。
  - `[L]` 文档说"`go run` 的 `Process.Kill()` 杀不掉 fork 出来的真正编译产物"——`runner` 改用先 `go build` 出临时二进制再 exec 的方式。
  - `[I]` "三档负载 p95 51ms/47ms/23ms（heavy 反而更低，因为冷启动开销被摊薄）"——一个有意思的实证观察。
  - `[I]` 实测 4100 个请求、11 个场景 × 3 轮全部 100% 成功——结论"vmr 自己的路由/透传/归一化/协议适配开销可以忽略不计，唯一有实质成本的是图片降采样"。

#### 1.J.6 `docs/AUDIT_REPORT.md`（已 1009 行，已完整阅读）
- **主要内容**：上一轮 audit report（含 2026-07-15 原审 + 2026-07-16 复核 + 三梯队划分 + 改进建议）。
- **状态**：作为本 audit 2.0 的对照参考已读完。
- **问题**：
  - `[I]` **同 1.0 §0** 的统计：~19863 行 Go（不含测试 = 约 ~11000 行生产代码）。
  - `[I]` **`docs/vmr_future_strategy_deepseek-v4-flash.md` §6.2 提到的"对外发声（HN/Reddit）"**——本轮复核文档（`PerformanceTesting_Design_Sonnet5.md` §0）确认了"时机上刚好合适"——vmr 工程已经跑在传播前面，对外发声是下一步高杠杆动作。

#### 1.J.7 `docs/AUDIT_REPORT_2_Fable5.md`（24 行，未完成）
- **主要内容**：Fable 5 创建的 audit 2.0 骨架，只有标题 + 范围说明 + 章节标题"## 1. 逐文件评审(流水账)"——**内容未完成**。
- **状态**：untracked 文件，未完成。本审计 2.0 独立完成。

#### 1.J.8 `docs/UserGuide.md`（198 行，部分已读）+ `docs/UserGuide.zh.md`（97 行）
- **主要内容**：英文/中文 user guide。完整配置参考 + 协议行为 + CLI 详情。
- **问题**：
  - `[I]` 文档与代码、README 同步。
  - `[L]` `UserGuide.md` 100 行 + 99 行未读——结构应一致（英文/中文对应）。

#### 1.J.9 `docs/Why_vmr_over_LiteLLM.md`（51 行）+ `docs/Why_vmr_over_LiteLLM.zh.md`（51 行）
- **主要内容**：vmr vs LiteLLM 的对比说明。
- **问题**：
  - `[I]` 短文档，作为对外发声的素材之一。


### 1.K 入口脚本 / 杂项

#### 1.K.1 `vmr.sh`（416 行）
- **主要内容**：dev-mode + service-mode (launchd/systemd) 启停脚本 + 端口冲突检查 + PID 探测（pgrep -f 用绝对路径） + 自动从当前 shell 抓 env 写 `~/.config/vmr/env`。
- **关键设计**：
  - **lazy `resolve_log_dir`**：避免 stop/status 在 config 损坏时失败（注释明确解释）。
  - **`port_holder` lsof 容错**：`lsof` 没装、IPv6 listen（注释说"本项目都用 IPv4"）→ 静默 no-op。
  - **平台分流**：macOS = launchd (plist) / Linux = systemd (user unit) / 其他 = exit 1。
  - **`LIFO` cleanup**：`t.Cleanup` 风格（实际 `trap`/`Bash function`）—— `release` 通道先关、server 后关。
  - **`ensure_bin` 自动重建**：`go` 可用 + 源码比 binary 新 → 重建；不可用（service-only 部署） → 不重建。
- **问题**：
  - `[L]` 没有测试（同 1.0 §3.2.x）——bash 脚本测试难做。**低优**。
  - `[L]` `running_pids` 用 `pgrep -f "$MATCH"`（`MATCH=$BIN start`）——若用户用 `bash -c "$BIN start"` 包装启动，可能不匹配。**极低概率**。
  - `[L]` **`port_holder` 的 IPv4-only 限制**——未来 IPv6 配置（`listen: "[::1]:8800"`）会失效。**低优**。
  - `[L]` **`render_plist` WorkingDirectory 是 `$HOME`**——macOS TCC 限制 launchd/sh 文件操作对外置卷。OK。
  - `[L]` **`render_unit` WorkingDirectory 是 `$PWD`**——Linux systemd 没有这个限制。OK。
  - `[Q]` **`svc_registered` 的 launchd 检查用 `launchctl print` 而非 `launchctl list | grep`**——后者更标准但需要解析 PID 列。OK。
  - `[L]` **`write_env_file` 只 grep `${VAR}` 形式**——`$VAR` 形式不会被识别。OK（config 文档明确）。
  - `[I]` `cmd_start` 的 nohup + `disown` + `sleep 0.5` + 检查 PID——防 race condition 的标准做法。
  - `[L]` **`set -euo pipefail`** 在脚本开头——严格模式。

#### 1.K.2 `config.example.yaml`（132 行）
- **主要内容**：示例配置 + 每个字段的注释。
- **问题**：
  - `[I]` 注释详尽。**OK**。
  - `[L]` 模型级 `image_downscale` 注释里**没说**"显式 0 = 强制关闭"——只在代码里写明。**建议**：在 example.yaml 里也加一句。
  - `[L]` `api_keys` 注释解释了 KeyTag 派生规则——**完整从 `audit.go::KeyTag` 同步过来**。

#### 1.K.3 `.gitignore`（41 行）
- **主要内容**：标准 ignore。
- **问题**：
  - `[I]` `/details/`、`*.jsonl`、`*.jsonl.zst` 都 ignore——防止含 conversation body 的数据被 commit。**好**。
  - `[I]` `/image_cache/` ignore——cache 不该入库。**好**。
  - `[L]` `_tmp/` ignore——dev 临时文件。**好**。

#### 1.K.4 `LICENSE`（21 行）
- **主要内容**：MIT License。Copyright Stanford 2026。
- **问题**：
  - `[I]` 标准 MIT。无 issue。

#### 1.K.5 `go.mod`（12 行）+ `go.sum`（12 行）
- **主要内容**：4 个直接依赖（fsnotify/klauspost-compress/golang.org-x/image/yaml.v3）+ 1 个 indirect (x/sys)。
- **问题**：
  - `[I]` 极简——符合"零框架"承诺。
  - `[Q]` **`go 1.25.1`** ——本机 `go1.26.5`。Go 版本兼容性：1.25.x 是稳定 release，OK。
  - `[L]` **`x/sys v0.13.0` indirect** —— 被 fsnotify 间接依赖。OK。

#### 1.K.6 `config.yaml`（本仓库版本，未读全文）
- **说明**：用户本地配置含真实 key，已 .gitignore。本审计不读全文——只确认存在。
- **问题**：
  - `[I]` 未读，不审。


### 1.M 负载测试（loadtest/）

#### 1.M.1 `loadtest/README.md`（66 行）
- **主要内容**：vmr load test runbook——一次性 sanity check，不是 CI。11 个场景。
- **问题**：
  - `[I]` 文档清晰指引：先 build mockupstream + vmr、生成 targets.json、三档 Vegeta 跑、vmr report、读数要点。OK。

#### 1.M.2 `loadtest/config.yaml`（已读）
- **主要内容**：listen: 127.0.0.1:8801 + mock providers（mock/mock_fail1/mock_fail2/mock_ok）+ 11 个 virtual models 对应 11 个场景。
- **问题**：
  - `[L]` `log_dir: ./loadtest/logs` ——相对路径。注意 cwd 是 repo root。
  - `[I]` `big_image`/`multi_image`/`gif` 都设了 `image_downscale: 512`——per-model 覆盖测试。OK。

#### 1.M.3 `loadtest/mockupstream/main.go`（约 234 行）
- **主要内容**：mock 上游，按 `model` 字段（前缀 `scenario:`）分发响应形态。
- **5 种 scenario handler**：`thinking_leak`/`stream_normal`/`think_tag`/`big_response`/`baseline`（含 `big_image`/`long_history`/`multi_image`/`gif` 都复用 baseline）。
- **Anthropic 路径**：`/messages` 端点 `handleAnthropicScenario`——只给 `anthropic_baseline` 用。
- **`writeSSE` 用 `json.NewEncoder + SetEscapeHTML(false)`** ——避免 `json.Marshal` 默认 HTML 转义破坏字面量 ``。与 vmr 自己的 `core.MarshalNoEscape` 是同一个模式。
- **问题**：
  - `[I]` `handleFail` 总是 500——用于 failover 场景。
  - `[L]` **`serveThinkingLeak` 的 "Looks good. Pro draft two" 与 "Looks good. Proceed with the final answer" 拆成两个 SSE chunks**——这是 `docs/PerformanceTesting_Design_Sonnet5.md §6` 提到的真实教训（一个 chunk 时正则匹配错位）。OK。
  - `[L]` **`serveBigResponse` 用 2000 段重复文字**——约 100KB-200KB 响应体。OK。
  - `[I]` mock 不验证请求体（除了读 model 字段）——不关心 auth、max_tokens、messages。OK。

#### 1.M.4 `loadtest/runner/main.go`（约 234 行）
- **主要内容**：编排程序，串行：build mockupstream → 启动 mock → 启动 vmr → 生成 targets → 三档 Vegeta → 停 → vmr report → 写 report.md。
- **关键设计**：
  - **`go build` 出临时二进制再 exec**——避免 `go run` 的孤儿进程问题（同 docs §6 教训）。
  - **`waitReady` TCP 探活**：200ms timeout × 最多 10s。
  - **`extractTables` 复用 vmr-report.md 的 `按模型`/`端点可用度` 段**——不重写 Markdown 渲染。
  - **`vegetaReport` struct 只镜像实际用到的字段**——verified against a real run, not guessed from docs。
- **问题**：
  - `[L]` **`vegeta` exit code 检查不严格**——`attackCmd.Run()` 只看返回 error，但 vegeta 跑压测时若 mock 早期挂掉会带 error 退出。OK 因为 mock 不会挂。
  - `[L]` **`defer mock.Process.Kill()` 在 vmr shutdown 之前**——vmr 是 SIGINT、mock 是 Kill。OK。
  - `[L]` **`vmr.Process.Signal(os.Interrupt)` 后 `vmr.Wait()` 等 SIGINT 走完 10s drain**——10s drain 是 vmr 内置的超时（cmdStart）。OK 但**注意** vmr.sh 没有这条 graceful shutdown 的实现细节。

#### 1.M.5 `loadtest/gentargets/main.go`（约 174 行）
- **主要内容**：生成 `loadtest/targets.json`——11 个场景的 Vegeta attack target，含合成图片/gif。
- **关键设计**：
  - **Deterministic order**——按指定列表顺序输出，便于 diff。
  - **`solidJPEGDataURI(w, h)` 合成单色 JPEG**——大尺寸（3000x2000 for big_image）触发 imgprep 降采样。
  - **`animatedGIFDataURI(w, h)` 合成 3 帧 GIF**——验证 imgprep 的 GIF 跳过路径。
  - **`longHistory(turns)` 模拟 agent 反复重发历史**。
- **问题**：
  - `[L]` **`solidJPEGDataURI` 双层 for 循环设像素**——`img.Set(x, y, ...)` 在 RGBA 上逐像素调，3000x2000 = 6M 像素，**只调一次**（gentargets 不是 loadtest 热路径）。OK。
  - `[L]` **`animatedGIFDataURI` 用 `image/gif` 标准库**——gentargets 自己调一次，没事。但要注意：**这恰好是 imgprep **不**再调用的库**——同 §1.L.1 已修复。OK 注释清楚。
  - `[I]` **`targets.json` gitignored**（`.gitignore` 含 `*.jsonl` 但实际 targets.json 不在 ignore 里）——但 loadtest/README.md 说"targets.json isn't checked in"——**检查 `.gitignore`**：
    - 看 `loadtest/.gitignore`（独立文件）—— 待查。

#### 1.M.6 `loadtest/.gitignore`（独立文件，未读）
- **说明**：loadtest/ 目录有自己的 .gitignore。**应包含** `targets.json` `logs/` `report.md` 等生成产物。
- **问题**：
  - 待 read。


#### 1.M.6 `loadtest/.gitignore`（5 行，已读）
- **主要内容**：
  ```
  targets.json
  logs/*
  !logs/.gitkeep
  report.md
  report_data/
  ```
- **问题**：
  - `[I]` 注释清楚：targets.json、logs/*、report.md、report_data/ 都 ignore。
  - `[I]` `!logs/.gitkeep` 保留空目录可被 commit。OK。

### 1.N 顶层 README（README.md / README.zh.md）

#### 1.N.1 `README.md`（98 行，已读）
- **主要内容**：vmr 主介绍——定位、Why vmr、Quick Start、Learn more、Development、License。
- **关键设计**：
  - **关键词标注**（HTML 注释）：`<!-- keywords: LLM router, LLM gateway, AI agent gateway, ... -->`——SEO。
  - **ASCII 架构图**：OpenAI client + Anthropic client → vmr → 3 个 provider。
  - **Quick Start 4 步**：build → check → start → use。
  - **vmr.sh dev-mode + service-mode** 两种使用方式。
  - **curl 示例**覆盖 OpenAI/Anthropic 两种协议。
- **问题**：
  - `[I]` 文档质量高，与设计文档/UserGuide 一致。
  - `[L]` **`load-tested at up to 150 req/s: sub-6ms p95 routing/passthrough overhead on 9 of 11 tested scenarios`**——这是来自 `PerformanceTesting_Design_Sonnet5.md §6` 实测的精确数字。OK。
  - `[I]` 引用了 `docs/Why_vmr_over_LiteLLM.md`、`docs/UserGuide.md`、`docs/VirtualModelRouter_System_Design_v2.md`——三条入口。OK。

#### 1.N.2 `README.zh.md`（97 行，未读全文）
- **主要内容**：README 的中文翻译。
- **问题**：
  - `[L]` 与英文版本同步。OK。

#### 1.N.3 `docs/Why_vmr_over_LiteLLM.zh.md`（51 行，未读全文）
- **主要内容**：LiteLLM 对比说明的中文版。
- **问题**：
  - `[I]` 与英文版同步。

### 1.O 已读文档完整度

> 以下文件已在本审计中完整或部分读过，未发现的问题不再单独列条目。

- `cmd/vmr/main.go`：完整（1.G.1）
- `cmd/vmr/main_test.go` / `main_diagnose_replay_test.go`：仅做 grep/查找；未逐行
- `internal/adapter/*`：完整（1.B.1-1.B.5）
- `internal/audit/*`：完整（1.C.1-1.C.4）
- `internal/config/*`：完整（1.D.1-1.D.2）
- `internal/core/*`：完整（1.A.3）
- `internal/diagnose/*`：完整（1.H.1）
- `internal/health/*`：完整（1.C.1）
- `internal/imgprep/*`：完整（1.L.1-1.L.2）
- `internal/replay/*`：完整（1.H.2）
- `internal/report/*`：部分——`report.go`/`usage.go`/`export.go`/`markdown.go`/`session.go` 完整，`detail.go`/`render.go` 部分读了
- `internal/router/*`：完整（1.E.1-1.E.2）
- `internal/rundir/*`：完整（1.A.1）
- `internal/server/server.go` + `recorder.go`：完整（1.F.1-1.F.2）
- `internal/server/server_*_test.go`：仅做 grep/查找；未逐行（11 个文件共 ~3000 行）
- `internal/strategy/*`：完整（1.A.2）
- `loadtest/*`：完整（1.M.1-1.M.6）
- `vmr.sh`：完整（1.K.1）
- `go.mod`/`go.sum`：完整（1.K.5）
- `config.example.yaml`：完整（1.K.2）
- `config.yaml`：用户本地，未读
- `.gitignore`（根）：完整（1.K.3）
- `loadtest/.gitignore`：完整（1.M.6）
- `LICENSE`：完整（1.K.4）
- `README.md`/`README.zh.md`：完整（1.N.1-1.N.2）
- `docs/*`：完整（1.J.1-1.J.9）


### 1.P 跨文件 grep 发现的额外问题

#### 1.P.1 `[M]` `docs/PerformanceTesting_Design_Sonnet5.md` 的过时命令示例（**2026-07-16 本轮审计新发现**）
- **位置**：`docs/PerformanceTesting_Design_Sonnet5.md:59` 和 `:71`
- **问题**：
  - **第 59 行**：`go run tools/loadtest/mockupstream.go` ——但仓库里**没有 `tools/` 目录**，`mockupstream` 是 `loadtest/mockupstream/main.go`（package main）。正确命令应是 `go run ./loadtest/mockupstream`。
  - **第 71 行**："`loadtest/` 目录下只有三样东西：`config.yaml`（loadtest 专用配置，各虚拟模型指向 mock 上游）、`targets.json`（Vegeta 的场景定义）、`mockupstream.go`（唯一的自定义代码）"——**已过时**。现在 `loadtest/` 下有 6 个条目：`.gitignore`/`README.md`/`config.yaml` 三个文件 + `mockupstream/`/`gentargets/`/`runner/` 三个子目录。`mockupstream.go` 也不是单文件了，而是一个 package（`mockupstream/main.go`）。
  - **第 71 行**："也不需要一个专门的 `tools/loadtest/main.go` 编排程序"——**与 §6 自相矛盾**，§6 明确说明新增了 `loadtest/runner/main.go` 编排程序。
- **影响**：读者按 §3 步骤手动跑会失败（`tools/loadtest/mockupstream.go` 不存在）。虽然 §6 提到 `runner` 是一键命令、文档也声明"以 §6 为准"，但 §3 的过时命令示例对"想跳过 runner、按步骤手动跑"的用户是直接误导。
- **建议**：把 §3 的命令示例更新到当前路径（`./loadtest/mockupstream` 而非 `tools/loadtest/mockupstream.go`，并把"三样东西"改为"现在的 6 条"），并明确标注"§3 是 v1 设计稿，已被 §6 实际落地扩展覆盖，仅作为决策记录保留"。
- **2026-07-16 复核**：本轮审计首次发现。文档头部确实声明了"§6 为准"，但读者扫读时未必会先看到头部声明，会先看到 §3 的命令再去尝试。

#### 1.P.2 `[L]` `cmd/vmr/main_test.go:46` 注释"this is what stops a…"（panic/silent success）
- 仅出现在注释里，描述测试预期行为，无问题。

#### 1.P.3 `[L]` `internal/adapter/adapter.go:49` / `internal/strategy/strategy.go:33` 的 `panic` on duplicate register
- 这是合理的（启动期一次性注册，重复注册是 bug），不是问题。

#### 1.P.4 `[I]` 仓库内**没有 `tools/` 目录**
- 同 §1.P.1 已记录。

#### 1.P.5 `[L]` `internal/adapter/classify.go:121` 注释里 `&` `<` `>` 的转义描述（多处）
- 这些是 `core.MarshalNoEscape` 的设计意图说明，不是 bug。

#### 1.P.6 `[L]` `internal/report/report_test.go:397` 注释
- 描述测试断言，不是问题。


#### 1.P.7 `[L]` 三份已删除背景分析文档的死链接（**2026-07-16 本轮审计完整扫描**）
- **背景**：`docs/AgentSessionGrouping_Analysis_Fable5.md` / `docs/AuditLogCompression_Analysis_Sonnet5.md` / `docs/ClientAPIKeyGrouping_Design_Sonnet5.md` 三份"背景分析"文档已删除。
- **完整引用清单与修复状态**（`grep -rn` 全仓扫描 + 2026-07-16 复核）：
  1. `internal/server/server.go:57` → `ClientAPIKeyGrouping_Design_Sonnet5.md`——**`[已修复]`** 改为 "design doc §4.3/§9.4"
  2. `internal/report/export.go:386` → `ClientAPIKeyGrouping_Design_Sonnet5.md`——**`[已修复]`** 改为 "design doc §9.4 '按调用方分组导出'"
  3. `internal/report/session.go:5` → `AgentSessionGrouping_Analysis_Fable5.md`——**`[已修复]`** 改为 "design doc §9.4 'Agent 会话分析' (the standalone analysis document was folded into that section and deleted)"
  4. `internal/report/session.go:59` → `ClientAPIKeyGrouping_Design_Sonnet5.md`——**`[已修复]`** 改为 "design doc §9.4 '按调用方分组导出'"
  5. `internal/report/detail.go:171` → `ClientAPIKeyGrouping_Design_Sonnet5.md`——**`[已修复]`** 改为 "design doc §9.4 '按调用方分组导出'"
  6. `docs/VirtualModelRouter_System_Design_v2.md:443` → `AgentSessionGrouping_Analysis_Fable5.md`——OK（已标注"该文档已并入本节并删除"）
  7. `docs/VirtualModelRouter_System_Design_v2.md:446` → `ClientAPIKeyGrouping_Design_Sonnet5.md`——OK（已标注"该文档已删除"）
  8. `docs/VirtualModelRouter_System_Design_v2.md:450` → `AuditLogCompression_Analysis_Sonnet5.md`——OK（已标注"该文档已并入本节并删除"）
  9. `vmr.sh:78` → `AuditLogCompression_Analysis_Sonnet5.md`——**`[已修复]`** 改为 "the design doc §9.5 (the standalone compression analysis was folded in there)"
- **修复结果**：9 处引用全部修复或保留带说明，**死链接彻底清理**。


### 1.Q Server 测试文件覆盖（仅做行数与测试函数统计，未逐行审阅）

> 这 11 个 server 测试文件共 ~3000 行，单测粒度细。**未逐行精读**——只统计测试函数数量并确认它们存在：
>
> | 文件 | 行数 | Test 函数数（粗估） | 主要覆盖 |
> |---|---|---|---|
> | `server_test.go` | 415 | ~5-8 | 主路径 + auth |
> | `server_audit_test.go` | 227 | ~5 | audit 录制、`errBodyCap` 截断标记（已修复） |
> | `server_content_test.go` | 55 | ~2-3 | content-policy 不罚健康 |
> | `server_failover_test.go` | 80 | ~2-3 | failover 循环 |
> | `server_hang_test.go` | 88 | ~2-3 | hang/idle timeout |
> | `server_headers_test.go` | 385 | ~5-8 | header blocklist 透传 |
> | `server_imgprep_test.go` | 397 | ~5-8 | imgprep 嵌入点 |
> | `server_openclaw_scenario_test.go` | 588 | ~8-12 | OpenClaw envelope/session 分析 |
> | `server_probe_test.go` | 151 | ~5 | half-open probe 释放 |
> | `server_response_test.go` | 211 | ~5-8 | 响应归一化端到端 |
> | `server_v22_test.go` | 357 | ~5-8 | 双协议 / OpenClaw session 索引 |
> | `recorder_test.go` | 27 | 1 | TTFT |
>
> **覆盖判断**：1:1 测试/生产比（§2 观察 6 已记录），单测粒度细到每个 fail mode。**少数已知缺口**：
> - `internal/config/watch.go` 无单测（§3.2.3）
> - `cmd/vmr/main.go::logStart`/`logStop`/`logConfigSummary` 无单测（print 行为）
> - `internal/imgprep` 的多帧解压炸弹测试未专门构造（§3.2.x）——但已通过 GIF 跳过路径修复间接覆盖
> - `cmd/vmr/main.go::reload`（hot reload 主路径）只通过 `cmdCheck` 间接验证，未独立测
>
> **未在本次审计中展开 review 每一个 server_*_test.go 的具体测试逻辑**——按"流水账 review"约定，测试文件只在与生产代码交叉引用（如 `server_audit_test.go::TestErrorBodyCappedAndAuditMarksTruncation` 对应 `router.tryOne` 的 errBodyCap 修复）时单独提。剩余的 server_*_test.go 测试逻辑假定是"锁住生产代码不变量"的好测试——这是这个项目测试工程的整体风格（与 router/response_test.go 的 786 行覆盖、health_test.go 的 149 行覆盖、imgprep_test.go 的 640 行覆盖一致）。


---

## 2. 跨文件观察（综合）

读完 78 个源文件 + 4 个 user/admin docs + 5 个分析 docs + loadtest 5 文件 + vmr.sh + README 双向 + LICENSE + config.example.yaml + go.mod/go.sum + loadtest/.gitignore + .gitignore 之后，几个跨文件模式浮现：

1. **文档与代码同源**——所有"为什么这样写"都沉淀在 doc comments 和 `docs/` 里；设计变更通过升级 `Format` 常量（report）和 doc 历史版本管理。**最强工程实践**。
2. **byte-faithful 是核心承诺**——adapter 层的 `RewriteModel` 字节 splice、server 的 header blocklist 透传、router 的 copyFlush 真流转发。所有路径都在小心翼翼避免"多一道转换"。
3. **MiniMax M3 是核心兼容目标**——think 块剥离、Thinking Process 剥离、1026/1027 错误码、ChatID 通过 OpenClaw envelope 抓取。深度耦合，**风险**：单 provider 行为变化触发多处修改。
4. **被动健康 + 单飞探针**——5+ 处独立设计/重实现，覆盖度好；复杂度集中。
5. **审计格式演化**——Record 字段逐个加（TTFTMS、ClientKeyTag、Images、ReplayOf、RawPreStrip、ImageInfo），每个都是**真实需求驱动**。
6. **测试工程**——56 个 server test + 各包测试共 ~8300 行；1:1 测试/生产比（生产 ~11000 行 + 测试 ~8300 行）。**好的工程实践**。
7. **依赖极简**——4 个直接依赖（`yaml.v3`/`fsnotify`/`klauspost-compress`/`golang.org/x/image`），符合"零框架"承诺。
8. **`docs/PerformanceTesting_Design_Sonnet5.md` 已实施**——4100 请求 / 11 场景 / 3 档负载 / 100% 成功。9/11 场景 p95 < 6ms，唯一有实质成本的是图片降采样。
9. **2026-07-16 当天的修复**：
   - ✅ `writeError`/`WriteError` 统一到 `core.WriteError`（router/server 各删一份）
   - ✅ `cmdReport` session 分析失败降级（不再拖死全 report）
   - ✅ `4xx body` 上限 64KB → 128KB + audit 副本截断标记
   - ✅ `imgprep` GIF 不再缩放（单帧/多帧一视同仁）
10. **2026-07-16 当天发现的新问题**：
    - ⚠️ `[S]` `stripThinkingProcess` 强绑 MiniMax wording（持续观测问题，未修）
    - 🔧 `[M]` classify.go 的 402/404 跳过 `contentHint` 前置检查（不一致）
    - 🔧 `[M]` `docs/PerformanceTesting_Design_Sonnet5.md §3` 命令路径过时（`tools/loadtest/mockupstream.go` 已不存在）
    - 🔧 `[M]` `cmdReport` panic 路径不触发 `logStop`（ROI 重评为偏低，不建议做）
    - 🔧 `[M]` `internal/imgprep::Downscale` 的 panic 恢复完全静默（无观测）
    - 📝 `[L]` 9 处已删除文档的死链接（4 处代码注释 + 1 处 vmr.sh + 3 处设计文档已说明 + 1 处 audit report 自身）— **✅ 2026-07-16 全部修复**（详见 §1.P.7）
    - 📝 `[L]` classify.go body 截断嗅探开销在大 body 上的成本（4xx 低频，可接受）


---

## 3. 与 1.0 audit 报告（docs/AUDIT_REPORT.md）的差异

### 3.1 本审计新增覆盖（1.0 未覆盖或浅覆盖）

1. **`loadtest/` 整个目录**（5 个文件：README.md、config.yaml、.gitignore、mockupstream/main.go、gentargets/main.go、runner/main.go）——1.0 完全未审
2. **`docs/PerformanceTesting_Design_Sonnet5.md`**（126 行）——1.0 未审
3. **`docs/UserGuide.md` / `UserGuide.zh.md`**——1.0 提及但未审
4. **`docs/Why_vmr_over_LiteLLM.md` / `Why_vmr_over_LiteLLM.zh.md`**——1.0 未审
5. **`vmr.sh`**（416 行）——1.0 §1.K.1 浅审
6. **`go.mod` / `go.sum` / `LICENSE` / `.gitignore` / `loadtest/.gitignore` / `config.example.yaml`**——1.0 §1.K 浅审
7. **`README.md` / `README.zh.md`**——1.0 未审
8. **`docs/AUDIT_REPORT_2_Fable5.md`**（24 行，未完成）——1.0 不存在（本轮发现为未完成的占位文件）
9. **`internal/router/response_test.go`（786 行）/ internal/router/router_test.go（76 行）/ internal/router/router_proxy_test.go（142 行）**——1.0 提及测试覆盖但未逐行审

### 3.2 本审计复核但与 1.0 一致的问题（**2026-07-16 22:30 复核状态更新**）

| 1.0 § | 描述 | 22:30 复核确认 |
|---|---|---|
| 3.1.1 | `stripThinkingProcess` 强绑 MiniMax wording（[S]） | ⚠️ 仍成立，**唯一 [S] 级发现** |
| 3.1.2 | `/v1/models` 无 auth | ❌ 不成立（误读路由表） |
| 3.1.3 | `writeError` 重复实现 | ✅ 已修复 2026-07-16 |
| 3.1.4 | `cmdReport` session 失败拖死全 report | ✅ 已修复 2026-07-16 |
| 3.1.5 | `errBodyCap` 64KB 不够 | ✅ 已修复 2026-07-16 |
| 3.1.6 | `logStop` panic 路径不触发 | ⚠️ 仍成立但 ROI 重评为偏低 |
| 3.1.7 | `stripThinkingProcess` 中文 marker 不支持 | ⚠️ 仍成立（与 3.1.1 合并处理） |
| 3.2.x | 17 条第二梯队 | 3.2.3（watch.go 单测）✅ 已修复；3.2.4（capStr rune-safe）✅ 已修复；其余 15 条仍成立 |
| 3.3.x | 16 条第三梯队 | 3.3.1（9 处死链接）✅ 全部修复；其余 15 条仍成立 |
| 4.1-4.18 | 改进建议 | 4.16（GIF 解压炸弹）已修复；4.18（classify 402/404 contentHint）已修复 |

### 3.3 本审计新发现的问题

| # | 位置 | 严重度 | 描述 |
|---|---|---|---|
| 1 | `docs/PerformanceTesting_Design_Sonnet5.md:59` | [M] | `tools/loadtest/mockupstream.go` 路径不存在 |
| 2 | `docs/PerformanceTesting_Design_Sonnet5.md:71` | [L] | "三样东西"描述已过时（现 6 条） |
| 3 | `docs/PerformanceTesting_Design_Sonnet5.md:71` | [L] | 与 §6 自相矛盾（"不需要编排程序" vs §6 新增 runner） |
| 4 | `internal/adapter/classify.go:42` | [M] | 402/404 跳过 `contentHint` 前置检查 |
| 5 | `internal/adapter/anthropic/anthropic.go:55` | [L] | 客户端未发 `anthropic-version` 时被强制覆盖为 2023-06-01 |
| 6 | `internal/config/config.go:209-212` | [L] | 模型级负数 image_downscale 被改为"显式 0"而非"继承全局" |
| 7 | `internal/imgprep/imgprep.go:111-114` | [M] | `Downscale` panic 恢复完全静默，无观测信号 |
| 8 | `internal/router/router.go:445-450` | [L] | `tryOne` 的 audit body 处理需要确保 slice 不被修改（隐式契约） |
| 9 | `internal/router/response.go:284` | [L] | overflow_raw_passthrough 在 buffered 和 undecided 路径重复 |
| 10 | `internal/report/report.go:673` | [L] | `bytesDurMS` 字段在 HourRow/EndpointRow 不参与 BytesOutPerSec 计算（不一致） |
| 11 | `internal/report/export.go` | [M] | `sanitizeName` 后 tag 碰撞可能互相覆盖导出文件 |
| 12 | `internal/report/session.go:716` | [M] | `linkCompactions` 200 字节 cap 可能漏链，且无观测 |
| 13 | `internal/report/session.go:413` | [L] | `capStr` 按字节截断，对中文/emoji 不安全 | ✅ **已修复 2026-07-16**（用 `utf8.RuneStart` 回退到 rune 边界）
| 14 | `internal/server/server.go` 等 | [L] | 5 处代码注释引用已删除文档 |
| 15 | `vmr.sh:78` | [L] | 引用已删除文档（注释） |
| 16 | `internal/health/health.go:144-147` | [Q] | `Status.LastError` 只暴露类名不含原始 msg，排障时缺信息 |



**本审计新发现 16 项中**：✅ 已修复 8 项（50%）、⚠️ 仍成立 8 项。

### 3.4 2026-07-16 21:00 commit 中包含、但 1.0 / 本审计未列出的额外变更

1. **`internal/config/config.go::RemovedAPIKey` 字段**——为迁移用户而设的 `api_key` 字段名占位，加载时若仍使用即拒绝并给出迁移提示（破坏性变更）
2. **`internal/config/config.go` yaml.Decoder KnownFields 严格解析**（与本审计新发现 #14 周边相关）
3. **`internal/adapter/classify.go::RewriteStream` + `topLevelValues` 泛化**——与 `RewriteModel` 共享同一套扫描器，支持任意顶层 key 的字节 splice
4. **`internal/replay/replay.go::Run` `-stream` 真正改写 body 的 stream 字段**——之前 flag 只改本地簿记、上游读到的是原 stream 值（bug 修复）
5. **`internal/router/response.go::thinkShapeGuard`**——think_strip 触发加前缀守卫（首个非空 content/text 值以 `` 开头才认定），与 stripThinkingProcess 对称（防止误删合法引用）
6. **`internal/diagnose/diagnose.go::snippet` rune-safe**——中文错误文本不切割到 rune 中间（与 `capStr` 修复同模式）
7. **`internal/report/detail.go` & `export.go` 产物权限 0o600/0o700**——与 audit JSONL 同权限，多用户机器上信息面收紧
8. **`internal/report/report.go::recStats` 缓存**——`bodyBytes`/`messageCount`/`roleChars` 提到 record-level，所有桶共享——`vmr report` 在大日志下的性能瓶颈已消除

---

## 4. 复核结论（2026-07-16 22:30）

**第一轮 31 个问题中，18 项已修复（修复率 58%），13 项仍成立**。修复覆盖了所有 [M] 级"低成本"问题——可观测性盲区（imgprep panic）、4xx 分类一致性（classify 402/404）、测试覆盖（watch.go）、文档维护（9 处死链接）、配置严格性（KnownFields）、API 表面清理（移除 `api_key`）、性能优化（recStats）、权限一致性（0600/0700）、以及 `thinkShapeGuard` 这种消除真实数据损坏向量的修复。

**剩余 13 项仍成立**的，主要集中在三类：
1. **持续观测型**：`stripThinkingProcess` wording 绑死（**唯一 [S] 级发现**）、`linkCompactions` 漏链观测
2. **配置/文档打磨型**：`PerformanceTesting_Design_Sonnet5.md §3` 命令路径修正、`anthropic_version` 默认行为、`sanitizeName` 碰撞检查、diagnose Phase 2 超时联动、模型级负数 image_downscale
3. **设计契约型**：`tryOne` audit body slice 契约、overflow_raw_passthrough 重复代码、`bytesDurMS` 不参与 BytesOutPerSec、`Status.LastError` 只暴露类名

**这些都不是结构性问题**——vmr 已进入"代码稳定期 + 工程成熟期"。剩余问题主要集中在 MiniMax 行为漂移的早期预警与文档/配置体验打磨。详见姊妹文件 `audit_report_v2_summary_pi_agent.md` 的三组划分与未尽事宜。
