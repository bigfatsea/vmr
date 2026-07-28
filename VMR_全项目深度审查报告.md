# VMR (Virtual Model Router) 全项目深度审查报告

> **这是一份时点快照（审查于 2026-07-27）**，不随代码更新。此后已落地、因而与下文描述有出入的部分：`/admin/status` 增加了 `instance`（进程身份 + 版本 + 配置新鲜度）、`reload`（最近一次热重载结果）与 `sticky.entries`；响应头增加 `X-VMR-Route-Reason`/`X-VMR-Failover`；审计 `attempts[]` 增加 `upstream_model`；新增 `vmr version` 子命令与 `vmr.sh ps`；`internal/report` 的渲染层按章节拆分为 `render_doc.go` + `section_*.go`，数据形状单列 `rows.go`，并新增 §6.5/§6.6 两章。当前实况以设计文档与 UserGuide 为准。

## 一、任务 Debrief

**审查目标**：对 VMR 项目进行全量、系统性深度审查，覆盖所有文档、源码、配置文件，从中提取项目的架构设计、关键实现细节、工程决策与 trade-off，最终形成一份可供类似项目参考学习的完整分析报告。

**审查范围**：
- 设计文档（`docs/VirtualModelRouter_System_Design_v3.md`）
- 用户指南（`docs/UserGuide.md`）
- 对比 LiteLLM 文档
- 完整源代码（`cmd/vmr/`、`internal/` 下全部 16 个包）
- 配置文件（`config.example.yaml`、`pricing.yaml`）
- 负载测试（`loadtest/`）
- 架构测试（`internal/archtest/`）

**审查方法**：逐文件阅读，记录每个模块的核心逻辑、设计决策、接口约定、边界条件处理，最后汇总形成综合评价。

---

## 二、项目整体目录结构

```
vmr/
├── README.md / README.zh.md          # 项目概览（中英双语）
├── go.mod / go.sum                   # 4 个直接依赖，零重量级框架
├── config.example.yaml               # 配置示例，含完整注释
├── config.yaml                       # 实际运行配置
├── pricing.yaml                      # 定价 sidecar
├── LICENSE                           # MIT
├── vmr.sh                            # 开发模式生命周期管理脚本
├── vmr-loadtest.sh                   # 负载测试脚本
├── docs/
│   ├── VirtualModelRouter_System_Design_v3.md  # 核心设计文档（880行，中文）
│   ├── UserGuide.md / UserGuide.zh.md          # 用户指南（中英双语）
│   ├── Why_vmr_over_LiteLLM.md / *.zh.md       # 与 LiteLLM 对比
│   ├── OUTSTANDING_ISSUES_opus-5.md
│   ├── vmr_future_strategy_v2_sonnet-5.md
│   └── SensitiveWordFilter_Analysis_Fable5.md
├── cmd/vmr/
│   ├── main.go           # CLI 入口 + blank import 注册点
│   ├── cmd_start.go      # 启动服务
│   ├── cmd_check.go      # 配置校验 + 路由预览
│   ├── cmd_status.go     # 运行实例状态
│   ├── cmd_report.go     # 审计报告生成
│   ├── cmd_dirs.go       # 目录路径查询
│   ├── cmd_diagnose.go   # 连通性诊断
│   ├── cmd_replay.go     # 请求重放
│   └── summary.go        # 配置摘要渲染（start/check 共用）
├── internal/
│   ├── core/             # 共享类型：CanonicalRequest, ErrorClass, Endpoint, FilterClientHeaders
│   ├── config/           # YAML 加载、${ENV} 展开、严格校验、热加载 watch
│   ├── adapter/          # Adapter 接口 + 注册表 + 错误分类 + model 字节级改写 + 指纹
│   │   ├── openai/       # OpenAI 协议透传 Adapter
│   │   └── anthropic/    # Anthropic 协议透传 Adapter
│   ├── router/           # 核心：failover 循环、快照、响应归一化、流式转发、并发闸
│   ├── health/           # 被动健康状态机：冷却、退避、半开单飞
│   ├── strategy/         # Dimension 接口 + Condition 接口 + 注册表
│   ├── sticky/           # 会话亲和注册表
│   ├── server/           # HTTP 入口、鉴权、审计录制、RequestFacts 计算
│   ├── audit/            # 审计 JSONL 落盘 + 压缩 + 保留清理
│   ├── report/           # 统计聚合、会话分析、详单渲染、定价
│   ├── probe/            # 探测请求原语（diagnose/router 共用）
│   ├── replay/           # 请求重放：从审计记录重建并重发
│   ├── diagnose/         # 连通性诊断工具
│   ├── imgprep/          # 图片降采样 + 磁盘缓存
│   ├── fmtutil/          # 展示格式化（Bytes/Tokens/Seconds）
│   ├── rundir/           # 默认目录解析（~/.vmr → temp → cwd）
│   └── archtest/         # 可执行架构不变式（import 边界、文件行数上限）
├── loadtest/
│   ├── README.md         # 负载测试 runbook
│   ├── config.yaml       # 负载测试用配置
│   ├── runner/main.go    # 自动化负载测试 runner
│   ├── mockupstream/main.go  # 模拟上游
│   └── gentargets/main.go    # 生成测试目标
└── logs/                 # 审计日志 + 错误日志（运行时产生）
```

---

## 三、执行计划

审查按以下顺序逐层深入：

1. **项目概览**：README、设计文档、用户指南
2. **配置层**：`config.example.yaml`、`config.go`、`watch.go`
3. **核心类型**：`core/core.go`、`core/headers.go`
4. **Adapter 机制**：`adapter/adapter.go`、`classify.go`、`fingerprint.go`、`openai/`、`anthropic/`
5. **路由核心**：`router/router.go`、`snapshot.go`、`limiter.go`、`transport.go`、`logfmt.go`
6. **响应归一化**：`router/response.go`、`router/responsefix.go`
7. **健康与策略**：`health/health.go`、`strategy/strategy.go`、`conditions.go`、`sticky/sticky.go`
8. **服务器层**：`server/server.go`、`facts.go`、`recorder.go`
9. **审计与报告**：`audit/audit.go`、`housekeep.go`、`read.go`、`report/` 各文件
10. **辅助工具**：`probe/`、`replay/`、`diagnose/`、`imgprep/`、`fmtutil/`、`rundir/`
11. **CLI 层**：`cmd/vmr/` 各文件
12. **测试与质量**：`archtest/`、`loadtest/`
13. **综合评估**：架构设计、安全、可观测性、错误处理、代码质量

---

## 四、逐模块详细审查记录

### 4.1 项目概览与设计哲学

**核心定位**：本地运行、单二进制、配置驱动的 LLM 路由器。核心价值主张——**字节级忠实透传（byte-faithful passthrough）**——不做中间表示、不做跨协议转换。

**关键设计原则**：
- **永不翻译协议**：`POST /v1/chat/completions`（OpenAI）和 `POST /v1/messages`（Anthropic）各自独立路由，只路由到同协议端点
- **零内部中间表示**：不存在"把请求解析成规范格式再转换"这一层
- **前向兼容**：未知 API 参数透传，新 provider 功能无需修改 vmr 代码
- **Unix 哲学**：单二进制、零数据库、零 Web UI、零运行时插件

**依赖极简**：`go.mod` 中仅 4 个直接依赖——`gopkg.in/yaml.v3`（配置解析）、`fsnotify`（文件监听）、`golang.org/x/image`（图片解码）、`github.com/klauspost/compress/zstd`（压缩）。无 Web 框架、无 Provider SDK。

### 4.2 配置层（`internal/config/`）

**文件**：`config.go`（490+ 行）、`watch.go`（50 行）

**核心设计亮点**：

1. **严格字段校验（`KnownFields`）**：使用 `yaml.Decoder.KnownFields(true)`，拼写错误的配置键（如 `max_concurency`）是加载错误，不是静默忽略。这是"fail-fast"哲学在配置层的体现。

2. **`${ENV}` 展开**：正则 `\$\{([A-Za-z_][A-Za-z0-9_]*)\}` 仅匹配 `${NAME}` 形式，裸 `$` 保持原样。未设置变量展开为空字符串。这是 vmr 唯一从环境读取数据的途径。

3. **代理配置三重决策**：`http_proxy`/`https_proxy` 仅声明代理地址，不默认开启；每个 provider 有独立的 `proxy: true/false` 开关；全局 `proxy` 作为缺省。推荐模式是"全局关闭 + 逐 provider 显式开启"——显式意图优于隐式全局默认。

4. **代理环境变量被刻意忽略**：`HTTP_PROXY`/`HTTPS_PROXY` 等环境变量不被 vmr 读取，代理必须显式配置。设计文档说这是"路由器不应该有的隐式旋钮"。

5. **目录解析三层回退**：`log_dir`/`image_cache_dir` 使用 `internal/rundir` 的三层默认公式（`~/.vmr/<subdir>` → `os.TempDir()/<subdir>` → `./<subdir>`），默认值落在持久目录而非临时目录（macOS 会清理临时目录）。

6. **热加载**：`fsnotify` 监听目录（兼容编辑器原子替换），300ms 防抖 + SIGHUP 兜底。新配置完整校验失败则保留旧配置。路由表（含 `http.Client`）随快照原子指针交换。

7. **StickyTTL 硬上限校验**：配置的 `sticky_ttl` 超过 `core.StickyBackstopTTL`（24h）时在加载阶段拒绝——防止"配置写了但不生效"的静默陷阱。

8. **`api_keys` 最小长度强制**：≥16 字符，否则 `KeyTag` 的末 8 位窗口可能就是整把密钥，导致密钥泄露进审计报告。

9. **`role_map` 按 provider 配置 role 改写**：针对 DashScope/千问拒绝 `developer` role 的场景，在 provider 级别配置 `role_map: {developer: system}`，`RewriteRoles` 在发出请求前用字节级拼接替换 `messages` 数组中匹配的 `"role"` 值。

### 4.3 核心类型（`internal/core/`）

**文件**：`core.go`（~200 行）、`headers.go`（~50 行）

**核心设计亮点**：

1. **`MarshalNoEscape`**：`json.Marshal` 但不做 HTML 转义（`<` `>` `&` 不转成 `\uXXXX`），且不带尾部换行。VMR 所有重新序列化客户端 JSON 的地方（model 改写、图片降采样）都用这个函数，保证字节级一致性。

2. **`CanonicalRequest`**：路由视图，只解析 `model`/`stream` 两个字段——`Raw` 保留原始 `json.RawMessage`，前向兼容。

3. **`RequestFacts`**：预计算的请求特征，一次计算、三处使用（路由决策、审计记录、条件过滤）。`HasImage` 来自 `imgprep.Downscale` 的结构化遍历结果而非子串扫描——避免纯文本请求因引用 `image_downscale=512px` 被误判为需要图片支持。

4. **`ErrorClass` 类型划分精细**：10 个枚举值覆盖各种失败场景，分三组：
   - 路由决策组：`ErrClient`/`ErrAuth`/`ErrRateLimit`/`ErrEndpoint`/`ErrTransient`/`ErrContent`——决定是否 failover 和冷却时间
   - 审计专用组：`ErrBuild`/`ErrNetwork`/`ErrCanceled`/`ErrTruncated`——从不到达健康逻辑，纯粹为审计 trail 提供统一类型

5. **`Endpoint.Freeze()` 预计算**：`HealthKey()`（SHA-256 哈希 API Key）和 `Name()` 在 `BuildSnapshot` 时预计算一次，后续热路径上变成纯字段读取。`HealthKey` 包含 `AdapterType`——因为同一 provider 名可出现在两个协议组。

6. **`FilterClientHeaders` 黑名单策略**：默认透传 + 小型黑名单（而非白名单）。LLM SDK 发出的 header 集合已知且固定，不含危险 header，所以默认透传是安全的。黑名单包含：`Authorization`/`x-api-key`（凭证）、`Cookie`/`Proxy-Authorization`（浏览器状态）、`X-Forwarded-*`（IP 欺骗向量）、`Host`/`Content-Length`/`Transfer-Encoding`/`Connection`（Go Transport 自动管理）、`Accept-Encoding`（阻止 Go 透明 gzip）。

7. **`EstimateTextTokens` 保守估算**：英文 4 字节/token、中文/多字节 UTF-8 2 字节/token（故意偏高），直接扫原始字节分类（`b >= 0x80`），不解码 UTF-8 rune。

### 4.4 Adapter 机制（`internal/adapter/`）

**文件**：`adapter.go`、`classify.go`、`fingerprint.go`、`openai/openai.go`、`anthropic/anthropic.go`

**核心设计亮点**：

1. **极简接口**：仅 4 个方法——`Protocol()`、`ResolveURL()`、`BuildRequest()`、`ClassifyError()`。新增协议 = 一个包 + 一行 `blank import`。

2. **编译期注册（`database/sql` 模式）**：`init()` 调用 `Register()`，`atomic.Pointer` + `sync.Mutex` 实现 lock-free 读路径。注册表写路径用 `copy-on-write` + `mutex` 保护——设计文档明确指出"裸 copy-on-write 不加锁会在并发写入时静默丢失更新"。

3. **`BuildRequest` 返回 `([]byte, error)` 双返回值**：出站 body 字节直接返回，审计 trail 直接引用，省去 `GetBody+ReadAll` 再拷一份的开销。

4. **字节级 model 改写**：`RewriteModel` 用 `topLevelValues` 扫描器定位**顶层** `"model"` 键的值区间，然后前缀 + 新值 + 后缀三段拼接。避免"整体 unmarshal 成 map 再重新序列化"，后者在 200KB body 上实测 99µs vs 5 次分配。嵌套在 `messages`/`tool schema` 里的 `model` 键不受影响。扫描器搞不定的形态回退到 `map[string]json.RawMessage` 重建路径。

5. **`topLevelValues` 扫描器**：单趟分配无关扫描——字符串跳过走 `bytes.IndexByte`（memchr 速度），多 MB content 也是 memchr 速度。跳过 JSON 值用 `skipJSONValue` 递归处理嵌套对象/数组。

6. **`RewriteRoles` 字节级 role 改写**：复用 `topLevelValues` 定位 `messages` 数组，遍历每个消息对象查找 `role` 键，按 `roleMap` 替换值。余额字节原样保留。

7. **错误分类（`DefaultClassify`）**：这是决定 failover 质量的关键。必须做 body 嗅探——厂商习惯不一：
   - MiniMax 未知模型返回 400（非 404），内容违规 1026/1027
   - DeepSeek Anthropic 口错误模型没说 "The supported API model names are …"
   - OpenRouter 402=余额不足，403=moderation flag
   - 有厂商额度耗尽也发 429
   - 嗅探词表中英并收：`content_filter`/`content_policy`/`moderation`/`flagged`/`guardrail`/`inappropriate`/`sensitive`/`敏感`/`违规`/`合规`
   - 分类顺序：内容词表 > 模型未知词表 > `upstreamHint` > 兜底 `ErrClient`

8. **`upstreamHint`**：识别"网关/中转层自报转发失败"的措辞——"upstream request failed" / "error from provider" / "bad gateway" / "gateway timeout"。刻意收窄匹配（不匹配单独出现的 "upstream"/"gateway"），避免命中真正的内容错误。

9. **`SessionFingerprint`**：Sticky Model 的会话指纹计算——对 system prompt 和第一条非 system 消息分别算 MD5。必须包含 system prompt（prompt cache 前缀从请求最前面开始比较），但不包含 `tools`（结构化数据可能序列化出不同字节）。OpenAI 侧走消息数组遍历，只扫到第一个非 system 元素——代价与对话历史长度无关。

10. **`TopLevelProbe`**：一次结构化扫描同时提取 `model`/`stream`/`hasTools` 三个顶层字段，替代了原来反射 `json.Unmarshal` + 独立的 `tools` 数组扫描——一次探测，三处复用。

### 4.5 路由核心（`internal/router/`）

**文件**：`router.go`（~300 行）、`snapshot.go`、`limiter.go`、`transport.go`、`logfmt.go`、`probe.go`

**核心设计**：`router.go` 是项目的核心，设计文档要求它保持小巧（archtest 强制 700 行上限）。把一个 ~950 行的文件拆成了 6 个文件。

**`Serve` 方法的完整流程**：

```
1. 查 Virtual Model（protocol × model）
   ├─ 查到 → 继续
   └─ 查不到 → 404（含"协议不符"提示：model 在另一个协议组存在）

2. 健康过滤 + 主动探测触发（probe_mode: active）
   ├─ 半开 + 无探测在跑 → Acquire 抢单飞名额 → 起后台 goroutine 发探测请求
   └─ 半开 + 探测在跑 → 跳过

3. 硬性条件过滤（image/tools 等确定性 Condition）
   └─ 候选集为空 → 503（带 rejection summary）

4. 上下文长度过滤（WithinContext，估算值，非确定性）
   ├─ 过滤后非空 → 使用
   └─ 过滤后为空 + hardFiltered 非空 → 回退使用 hardFiltered（宁可让请求尝试，不让估算堵死）

5. 稳定多键排序（strategy.Sort）

6. Sticky Model 亲和重排
   ├─ 计算会话指纹（system + first non-system message）
   ├─ 查 sticky registry → 找到 → 移到候选列表首位
   └─ 找不到或 TTL 过期 → 保持原序

7. Failover 循环
   ├─ 每个候选：Acquire（passive 模式的半开探针）
   ├─ tryOne → 成功 → 更新 sticky 指针 → 返回
   ├─ tryOne → 失败 → 分类错误 → 按类冷却 → 试下一个
   └─ 全部失败 → 返回最后一个上游错误（原样转发 status + headers + body）
```

**关键设计决策**：

1. **`probe_mode: active`（默认）**：半开端点永远不放行真实请求。发现端点半开时，抢下单飞名额后起后台 goroutine 发探测请求，真实请求本身把该端点视为不可用。探测与真实流量完全解耦——被动模式下"谁先撞上半开端点谁就当探针"的问题（大请求拖慢探测）被消除。`internal/health` 本身零改动支撑这个模式——`Acquire`/`ReportSuccess`/`ReportFailure`/`ReportNeutral` 两种模式原样复用。

2. **`probe_mode: passive`**：完整保留原有行为，配置 `probe_mode: passive` 即可切回。

3. **上下文长度降级规则**：`EstimatedTokens` 可能明显偏高（文档类附件公式在扫描件场景可能高估几十倍），`WithinContext` 过滤后如果候选集为空但 `hardFiltered` 非空，回退使用 `hardFiltered`——宁可让"看起来太大"的请求真的打到端点上，不让 vmr 自己的估算把路堵死。

4. **Sticky Model 自愈设计**：粘性指针在每一次成功完成请求后都更新（包括 failover 后的成功），不只是第一次建立时。一次故障转移会让指针自动跟随移动到"实际生效缓存"真正所在的端点。

5. **`X-VMR-Endpoint` / `X-VMR-Attempts`**：成功响应带这两个 header，客户端可知道实际服务的端点和尝试次数。

6. **`copyFlush` 流式转发**：独立的 goroutine 读取上游 body，通过 channel 传给写入端，每次写入后 flush。带 `stream_idle` 超时保护——上游静默超过配置时间则 abort。

7. **`NewUpstreamClient` 的 `CheckRedirect`**：返回 `http.ErrUseLastResponse`，上游 3xx 原样到达客户端（POST 301/302/303 在默认 `http.Client` 策略下会被静默改写成 GET）。

### 4.6 响应归一化（`internal/router/response.go` + `responsefix.go`）

**设计原则**：直连等价。客户端经 VMR 收到的字节应与直连上游一致，仅有以下偏离：
- (a) `model` 字段改回虚拟名
- (b) 两个 MiniMax-M3 专属修复（仅在确认命中确切形态时触发）

**双传输模式**：
- **passthrough**（SSE 缺省）：逐 SSE 事件实时转发，除 model 改写外字节一致
- **buffered**：整体缓冲，EOF 时单遍 regex 归一化。用于非 SSE 响应、MiniMax 思考形态、未及判定即 EOF 的小流

**变换清单**（每项触发条件独立）：
| 变换 | 触发条件 | 守门机制 |
|------|---------|---------|
| model 改写 | 事件/响应体中出现顶层 `"model":"..."` | regex 匹配，JSON 转义的 `\"` 不会误命中 |
| 剥 ` thinking` 块 | 首个非空 content/text 值以 ` thinking` 开头 | 守卫：literal prefix 匹配，正文中段引用原样透传 |
| 剥 "Thinking Process:" | 首个 content 值以 `Thinking Process:` 开头 + 存在 `Looks good. Pro` 标记 | 守卫：literal prefix 匹配 |
| 追加 `[DONE]` | 仅 openai + SSE + 上游未发 | 永不重复追加 |
| 软拦截标记 | 嗅探 `input_sensitive`/`output_sensitive` | 纯观测，不改字节 |

**关键设计决策**：

1. **处理单位是完整 SSE 事件**，不是字节流或整个响应体。事件内 JSON 天然完整，model 改写不会跨界；跨事件的 think 块在确认命中后才进入缓冲。

2. **quirk 修复靠全局嗅探而非按端点声明**。think-strip / Thinking Process strip 对所有 provider 的响应做形态检测，不是只对声明了该 quirk 的 endpoint 启用。触发守卫仅当首个非空 content/text 以标记开头才认定——正文中段合法引用原样透传。引入 endpoint 级 `quirks:` 配置是新概念+新配置面，为剩余极低误伤面增加配置维度不划算。

3. **`thinkShapeGuard`** 检查第一个非空 content/text 值是否以 ` thinking` 开头——对称于 `stripThinkingProcess` 的 `Thinking Process:` 前缀守卫。被守卫保护后，正文中段引用 ` thinking...` 不会被误删。

4. **`thinking_process_pattern_detected` 纯观测标记**：当响应看起来像 MiniMax 的 leaked thinking outline（足够多的编号子节标记 + 足够长的内容字节），但实际 strip 没触发时，打上这个标记——让运维在"措辞变化导致 strip 静默失效"之前就能发现。

5. **`responsefix.go` 独立文件**：包含 MiniMax 专属的 pattern 知识，`response.go` 保持通用传输机制。测试文件覆盖了各种边界场景（`TestRespStream_ThinkQuotedMidTextUntouched` 等）。

### 4.7 健康状态机（`internal/health/`）

**核心设计**：被动状态机，失败驱动冷却，无主动轮询。

**冷却策略**：
- `ErrTransient`（5xx/408/网络/超时）：2s 起，指数退避 ×2，封顶 5min
- `ErrAuth`/`ErrEndpoint`（401/403/402/404/额度耗尽）：10min 起，封顶 1h
- `ErrRateLimit`（429）：优先 `Retry-After`，封顶 1h（`Retry-After` 是上游可控输入，畸形大值不能把端点锁死到进程重启）
- `ErrContent`：零冷却

**关键设计决策**：

1. **`ErrContent` 零冷却**：被内容审核拦截的端点本身完全健康，不能因此进冷却。若该端点恰处半开探针，只释放探针、不加深退避。

2. **`ErrClient` 直接返回客户端**：不切换、不冷却——每个端点都会以同样方式拒绝。

3. **健康注册表以 `provider/model/key指纹` 为稳定键**，独立于配置快照存活——热重载不清零冷却。重启即重置，不持久化。

4. **`ReportNeutral`**：释放半开探针槽但不改变失败计数或冷却。用于内容拦截、客户端取消、`ErrClient` 等"与端点健康无关"的结局——每个 acquired 的探针必须以 Success/Failure/Neutral 三者之一结束。

5. **`Acquire` 单飞探针**：半开端点同一时刻只允许一个调用方持有探针槽，避免惊群效应。

### 4.8 策略系统（`internal/strategy/`）

**核心设计**：调度 = 过滤 + 稳定多键排序。`Dimension`（排序）和 `Condition`（淘汰）是两个独立接口，不合并。

**`Dimension` 接口**：`Compare(a, b *core.Endpoint) int`——比较两个端点，不访问请求。当前实现 `priority`（数字小优先，平手保持配置文件顺序）。

**`Condition` 接口**：`Eligible(ep *core.Endpoint, facts core.RequestFacts) bool`——请求感知的准入判断。已注册的 Condition 全部无条件参与过滤（纯 AND 语义，顺序无关）。

**已注册 Condition**：
- `image`：`f.HasImage` → `ep.HasCapability("image")`
- `tools`：`f.HasTools` → `ep.HasCapability("tools")`
- `thinking`：**故意未注册**——请求侧检测逻辑未实现，协议形状未确认。注册一个永远不触发的条件不如不注册。

**`WithinContext`**：上下文长度判断，**不是 Condition**——因为它建立在估算之上，需要特殊的降级规则（估算值不能独自清空非空候选集）。这个降级规则只有调用方（已知 pre-context-filter 候选集）才能正确应用。

**注册表**：`Condition` 注册表用 `atomic.Pointer` + `copy-on-write` + `mutex`（与 adapter 注册表同样模式），每请求每端点读一次。`Dimension` 注册表用 plain `sync.RWMutex`（`Build` 只在配置加载时调用一次）。

### 4.9 Sticky Model（`internal/sticky/`）

**核心设计**：独立的轻量包，与 `internal/health` 平行。`Registry` 本身不知道任何端点/TTL 细节——它只是一个带 mtime 的键值存储。

**接口**：
- `Peek(key string) (endpointKey string, lastUsed time.Time, ok bool)`
- `Set(key, endpointKey string)`

**Key 组成**：`client_key_tag + ":" + hex(sysHash) + ":" + hex(firstMsgHash)`——`client_key_tag` 作为命名空间，不是主键。

**TTL 判定留给调用方**：`Peek` 返回 `lastUsed` 但不判断有效性。调用方（`router.Serve`）查到 `endpointKey` 后才知道该用哪个端点的 `StickyTTL`。

**内存淘汰**：`BackstopTTL` = 24h（`core.StickyBackstopTTL`），独立于任何端点的 TTL。`Set` 时每小时触发一次惰性扫描清理过期条目。

**配置校验**：`config.validate()` 强制保证全局 `sticky_ttl` 与任意端点的 `sticky_ttl` 不超过 `BackstopTTL`——否则配置在加载阶段直接拒绝。

**默认开启**：`ModelConfig.Sticky *bool`，`nil` 视为 `true`——指纹计算开销可忽略，多轮 agent 流量是主要受众。

### 4.10 服务器层（`internal/server/`）

**文件**：`server.go`、`facts.go`、`recorder.go`

**`chatHandler` 完整流程**：

```
1. 创建审计 Record（如果审计开启）
2. 鉴权：authenticate() → tag + ok
3. 缓冲请求体（≤MaxRequestBodyBytes，413 超限）
4. TopLevelProbe：一次结构化扫描取 model/stream/hasTools
5. AcquireSlot：并发闸（在缓冲完成之后获取，慢客户端上传不占槽）
6. 解析路由 → EffectiveImageDownscaleMaxPx
7. imgprep.Downscale：图片降采样（按模型覆盖的 maxPx）
8. computeRequestFacts：一次计算，两处使用（路由 + 审计）
9. FilterClientHeaders：黑名单过滤
10. rt.Serve：路由决策 + failover
```

**关键设计决策**：

1. **并发闸在缓冲完成之后获取**：慢客户端上传不占槽，闸覆盖 CPU + 上游往返阶段。

2. **图片降采样在并发闸之内**：受闸保护，不引入新的无界并发。

3. **`imgprep.Downscale` 是一次调用、三处复用**：`images` 切片同时喂给审计 `rec.Images`、`computeRequestFacts` 的 `imageCount`（驱动 `HasImage` 和图片 token 估算）。**零额外探测成本**。

4. **`computeRequestFacts` 的 `hasTools` 来自 `TopLevelProbe`**：不重新扫描请求体，复用同一次结构性扫描的结果。

5. **`recorder` 包装 `ResponseWriter`**：捕获客户端响应（status/headers/body/TTFT），保留 `Flusher` 接口，流式时延零影响。body 记录上限 64MB（`recorderBodyCap`），仅审计侧截断，客户端始终收到完整字节。

6. **鉴权双模式**：Bearer 和 x-api-key 都接受，`subtle.ConstantTimeCompare` 防时序攻击。Bearer 前缀大小写不敏感（`strings.EqualFold`）。

7. **无鉴权时自声明标签**：不配置 `api_keys` 时，客户端自愿发来的 `Authorization`/`x-api-key` 值仍通过 `KeyTag` 打标签——纯内网场景下零 vmr 侧配置地自报身份。

### 4.11 审计与报告（`internal/audit/` + `internal/report/`）

**审计设计**（`audit/`）：

1. **两层记录**：`Client`（调用方↔vmr）+ `Attempts[]`（vmr↔上游，每次 failover 尝试一条）
2. **成功尝试的响应 body 不存**：透传恒等，与 `client.response.body` 字节相同，差异由 `norm` 列表解释
3. **凭证掩码**：`Authorization`/`X-Api-Key`/`Cookie`/`Set-Cookie`/`Proxy-Authorization` 的值只保留末 4 字符
4. **JSON 编码在锁外**：`writeBufPool` 池化 `bytes.Buffer`，编码在 `l.mu.Lock()` 之前完成——否则多 MB 的 JSON 编码会在全局锁下串行化
5. **按天轮转**：`vmr-audit-YYYY-MM-DD.jsonl`，权限 0600
6. **历史压缩**：zstd 默认级别（MB 级窗口），实测 20-75× 压缩比（跨行重复被 zstd 大窗口捕获，gzip 的 32KB 窗口做不到）
7. **Crash-safe**：temp 文件 + rename 落地，确认后删原文件
8. **保留策略默认关闭**：`audit_retention_days` 缺省 0=永久保留——审计日志是成本核算唯一数据源，默认删除是数据丢失风险

**报告系统**（`report/`）：

1. **两遍读取**：第一遍 `AnalyzeSessions` 做会话/任务分组（并行，791 行启发式核心），第二遍 `Build` 做聚合 + 详单渲染（流式扫描，worker pool）
2. **真百分位**：每个桶独立收原始值，各自算 p50/p95——不是跨桶拿已算好的百分位再汇总
3. **`stream_ms` 独立收集**：`dur_ms − ttft_ms` 作为第三条原始切片，不是拿 dur 的 p95 减 ttft 的 p95
4. **Agent 会话分析**：纯规则、离线、不调 LLM——会话指纹、max-LCP 选父、compaction 双向链接、Traceparent 任务边界
5. **工具使用报告**：声明工具 vs 当轮实际调用 + never_called 清单
6. **定价 sidecar**：可选，本地 YAML，支持多货币换算（BFS 图遍历）、时间窗口定价（`date_range`/`hour_range`）
7. **九个章节的 Markdown 报告**：§0 摘要、§1 成本、§2 定价、§3 可靠性、§4 延迟、§5 分布、§6 会话、§7 效率、§8 详单入口
8. **多调用方分组**：`api_keys` 的 `client_key_tag` 自动驱动 `vmr-requests-<tag>.md` 分组输出

### 4.12 诊断工具（`internal/diagnose/` + `internal/probe/` + `internal/replay/`）

**`vmr diagnose`**：四阶段诊断
1. Phase 1：配置加载状态
2. Phase 2：DNS/TLS/代理连通性（每 provider，并发上限 8）
3. Phase 3：真实最小请求（每 endpoint，并发上限 8，超时 15s）
4. Phase 4：路由预览（带 Phase 3 结果标注）

**`probe` 包**：构造最小非货请求（`max_tokens: 300`，让模型回显一次性 nonce），`Echoed` 做子串校验。`max_tokens: 300` 而非更小——部分推理模型会把预算花在 ` thinking` 块上，太小的预算会让回显校验大面积假失败。

**`vmr replay`**：从审计记录重建并重发请求，使用与 `vmr start` 完全相同的 `adapter.BuildRequest` 路径。支持三种定位方式：`-detail`（`details/*.json` 文件）、`-ts`（时间戳精确匹配）、`-line`（行号）。支持 `-dry-run` 打印请求、`-record` 写重放审计记录。

### 4.13 图片降采样（`internal/imgprep/`）

**核心设计**：

1. **检测分层**：越靠前越便宜——第一步 `HasImageMarker`（`bytes.Contains` 找 `"image`）是最便宜的子串扫描，无视图片请求直接跳过。只有命中后才解析 JSON。

2. **"检测到"与"解得出格式"独立**：`len(images)` 是结构上确认是图片引用的块数量，不依赖解码器能否读取像素头。解不出的格式仍计入 `imageCount`（上游可能认识），不能被误判为"没有图片"。

3. **GIF 一律跳过缩放**：`image/gif.DecodeAll` 是唯一判断帧数的方式，但该函数对帧数/累计解码内存无上限——一张画布不大但帧数极多的 GIF 能在"判断出它是多帧、于是跳过"之前就把全部帧解码进内存。连单帧 GIF 也一并跳过，`image/gif.DecodeAll` 整条路径上不再被调用。

4. **解压炸弹防护**：声明像素 > 64MP 的图片拒绝解码。

5. **Fail-open + panic recover**：任何解析/解码/编码失败回退到原始字节，`recover()` 向 stderr 打一行日志。

6. **磁盘缓存**：Key = `sha256(原始图片字节) + maxPx`，文件名 `<hex>-<maxPx>.jpg`。查找时机在"确认需要处理"之后——大多数图片根本不需要降采样。命中时 `os.Chtimes` 刷新 mtime。TTL 默认 7 天，主动清理。

7. **缓存的价值**：不仅省 CPU，更重要的是字节一致性——上游 prompt cache 按精确字节匹配，重新 JPEG 编码可能产生不同输出字节导致缓存静默失效。

### 4.14 架构测试（`internal/archtest/`）

**`import_boundaries_test.go`**：强制执行 `internal/report` 不能依赖 `internal/{router,server,config}`——分析层只依赖审计 schema，不依赖路由运行时。

**`file_sizes_test.go`**：强制 `internal/router/router.go` 不超过 700 行——防止核心文件重新膨胀。这个限制来自一次架构 review 的真实发现（router.go 曾从 550 行设计预算膨胀到 948 行）。

### 4.15 负载测试（`loadtest/`）

**11 个场景**覆盖不同成本 profile：`baseline`（路由底板）、`stream_normal`（真 SSE 透传）、`thinking_leak`（全缓冲最坏路径）、`think_tag`（缓冲后恢复流式）、`big_response`（大非流式 body）、`big_image`/`multi_image`（图片解码/缩放/编码）、`gif`（验证永不缩放快速路径）、`long_history`（大 body 解析/审计成本）、`failover`（健康/冷却机制）、`anthropic_baseline`（另一个协议适配器）。

**三轮递增负载**：light (10 req/s × 10s)、moderate (50 req/s × 20s)、heavy (150 req/s × 20s)。

**结果分两组报告**：`plain`（8 场景）和 `image`（3 场景，图片解码是 vmr 最贵的操作，混在一起会拖高所有场景的 p95）。

**图片缓存策略**：`gentargets` 生成 50 个不同的图片变体轮换，避免单张固定图片被缓存命中后所有请求都变成廉价的磁盘读取。

---

## 五、关键特性总结

### 5.1 架构设计亮点

1. **无中心 IR（中间表示）**：这是 VMR 最核心的架构选择。不把请求解析成内部格式再转换出去，而是字节级透传。带来的好处是：前向兼容（新 provider 参数无需修改代码）、零协议转换损耗、代码量极低。

2. **协议内路由，永不做跨协议转换**：OpenAI 和 Anthropic 两个入口各自独立路由。配置结构本身就是 `providers.<protocol>.<name>` 和 `models.<protocol>.<name>`，跨协议混用在语法上就无法表达——不是"配置了会被校验拒绝"，而是"配置这个东西本身写不出来"。

3. **Adapter 接口极简**：4 个方法，新增协议 = 一个包 + 一行 blank import。响应体不经过 Adapter，归一化在 router 层独立完成。

4. **Dimension vs Condition 分离**：排序（`Dimension.Compare`）和淘汰（`Condition.Eligible`）是两个独立接口——`Dimension` 不需要访问请求，`Condition` 需要。这是正确的抽象分层。

5. **上下文长度估算的降级规则**：`WithinContext` 不是 `Condition`——因为估算可能出错，需要特殊降级规则。只放在上下文长度这一个条件上，不推广成 `Condition` 接口的通用能力。

6. **`probe_mode: active` 的探测解耦**：真实流量与恢复探测完全解耦，健康状态机零改动支撑两种模式。被动模式下"谁先撞上半开端点谁就当探针"的问题被消除。

7. **Sticky Model 自愈设计**：每次成功（含 failover 后的成功）都更新粘性指针，指针自动跟随缓存的真实位置。

### 5.2 安全设计亮点

1. **分层防御**：`FilterClientHeaders` 黑名单 + `Adapter.BuildRequest` 的 `Header.Set` 覆盖——"belt and suspenders"。

2. **凭证掩码**：审计日志中的凭证只保留末 4 字符。`api_keys` 最小长度 16 字符强制。

3. **`subtle.ConstantTimeCompare`**：防止时序攻击。

4. **`KeyTag` 的 `-` 分隔规则**：`-alice` 后缀避免固定长度窗口截取到随机字符。

5. **`admin/status` 仅 loopback**：`net.IP.IsLoopback()` 校验。

6. **审计文件 0600、报告目录 0700**：派生报告产物不松弛权限。

7. **代理环境变量被刻意忽略**：隐式旋钮不应存在，必须显式配置。

8. **解压炸弹防护**：图片像素 > 64MP 拒绝解码，panic recover 兜底。

### 5.3 可观测性设计亮点

1. **飞行记录器（flight-recorder）审计日志**：每个请求一行 JSONL，两层往返（client↔vmr、vmr↔upstream），每次 failover 尝试，归一化步骤列表。

2. **`X-VMR-Endpoint` / `X-VMR-Attempts`**：响应头标明实际服务端点和尝试次数。

3. **`/admin/status`**：端点健康 + 并发指标 + probing 状态。

4. **`vmr diagnose`**：四阶段诊断，从配置到 DNS/TLS 到真实请求到路由预览。

5. **`vmr replay`**：从审计记录重建并重发请求，支持 dry-run。

6. **实时日志**：每行包含 client tag、虚拟模型→物理端点映射、capabilities、request size、estimated tokens、status、duration。

7. **`vmr report`**：九个章节的 Markdown 报告 + JSON + 逐请求详单 + 会话/任务分组 + 工具使用分析。

8. **纯观测标记**：`soft_block_detected`、`crlf_framing_suspected`、`thinking_process_pattern_detected`——不改字节，但给运维提供信号。

9. **`facts` 原样落盘**：路由决策所用的 `RequestFacts` 原样存入审计，事后可复盘路由决策。

### 5.4 错误处理设计亮点

1. **精细的错误分类**：10 个 `ErrorClass` 枚举值，每个驱动不同的 failover 和冷却行为。

2. **Fail-open 原则**：图片降采样、模型改写、响应归一化——任何失败都不应让本可成功的请求失败。降级到直连行为。

3. **`ErrContent` 零冷却**：内容拦截是"按请求"而非"按端点"的错误。

4. **`ErrClient` 直接返回**：不切换、不冷却——每个端点都会以同样方式拒绝。

5. **`upstreamHint`**：识别网关转发失败，防止 failover 被 dead-end 卡死。

6. **`Retry-After` 封顶 1h**：上游可控输入，畸形大值不能锁死端点。

7. **探针槽必须归还**：无论成功/失败/中性，每个 acquired 的探针必须以三者之一结束。回归测试覆盖所有分支。

### 5.5 工程 Trade-off 亮点

1. **字节拼接 vs 完整解析**：model 改写用字节拼接（200KB body 实测 99µs），回退到 `map[string]json.RawMessage` 重建路径。成本：只能处理规整的顶层 key。收益：快约一个数量级。

2. **全局嗅探 vs 按端点声明**：quirk 修复靠全局嗅探而非按端点配置。成本：理论上的"回复恰好以触发标记开头"误伤面。收益：零配置、零新概念。

3. **压缩选择 zstd**：gzip 的 32KB 滑动窗口看不到跨行重复（压缩比 ~3.3×），zstd 默认 MB 级别窗口捕获跨行重复（20-75×）。成本：多一个依赖。收益：实际压缩比提升一个数量级。

4. **默认 sticky 开启**：`*bool` 的 `nil` 视为 `true`。成本：每次请求多一次 MD5 哈希。收益：大多数多轮 agent 场景无需配置。

5. **`image_downscale` 开关即参数**：`int` 类型，0/缺省=关闭。不设独立的 `enabled` 字段。逐模型覆盖用 `*int`（nil=继承，0=强制关闭）。

6. **审计不截断**：`max_request_body_mb` 只决定 vmr 接不接受请求，接受的请求审计里是完整的那一份。成本：代理大 body 可能消耗内存。收益：审计完整性。

7. **`probe_mode: active` 按次计费**：不是独立的后台定时探测器，触发点绑定"真的有请求撞上这个半开端点"。成本：每恢复一次多花一次探测请求。收益：没有空闲轮询浪费。

8. **图片缓存 TTL 默认 7 天（主动清理）vs 审计日志默认永久保留**：图片缓存纯粹是性能优化，没有"数据丢失"属性，主动清理是更安全的默认值；审计日志有取证/成本核算价值，删除是数据丢失风险。

### 5.6 代码质量亮点

1. **模块边界清晰**：16 个 `internal/` 包，每个包职责单一。`internal/archtest` 强制执行 import 边界和文件行数上限。

2. **注释风格精准**：只解释"为什么"（隐藏约束、workaround、不变式），不叙述"是什么"。每个非直观的决策都有注释说明原因。

3. **接口小**：`Adapter` 接口 4 个方法，`Dimension` 接口 2 个方法，`Condition` 接口 2 个方法。

4. **nil-safe 设计**：`audit.Attempt` 的 `Set*` 方法都是 nil-safe，调用方不必检查 `att != nil`。`ModelRoute.EffectiveImageDownscaleMaxPx` nil 接收者安全。

5. **所有权契约明确**：`audit.EncodeBody` 的"referenced, not cloned"所有权契约在注释中明确说明。

6. **并发安全**：`atomic.Pointer` 做快照原子交换，`sync.Mutex` 保护注册表写入。`copy-on-write` + `mutex` 模式有注释解释为什么不能只用 `atomic`。

7. **测试覆盖**：大量测试文件，覆盖边界场景、并发场景（`-race`）、探针槽归还、回归测试。

8. **负载测试**：11 个场景覆盖不同成本 profile，三轮递增负载，与真实 `mockupstream` 配合。

9. **`MarshalNoEscape`**：所有 JSON 序列化统一路径，避免 HTML 转义引入不可见的字节差异。

10. **文件行数编码为测试**：`archtest` 的 `TestArchitecture_CoreFileSizes` 给核心文件设行数上限（`router.go` 700 行等）——不是注释里的建议，而是会失败的测试。

---

## 六、综合评价

### 架构设计：⭐⭐⭐⭐⭐（优秀）

VMR 的架构设计是其最突出的亮点。核心选择——"不做中间表示、不做协议转换、字节级透传"——在整个项目中得到了一致、彻底的贯彻。从配置结构（`providers.<protocol>.<name>`）到 Adapter 接口（响应体不经过 Adapter），从 model 改写（字节拼接而非完整解析）到响应归一化（按 SSE 事件而非字节流处理），每个设计决策都服务于"字节忠实"这一核心原则。

模块划分清晰，`Dimension` vs `Condition` 的接口分离体现了对"排序"和"淘汰"两种语义差异的深刻理解。`probe_mode: active` 的设计（健康状态机零改动支撑两种模式）展现了良好的抽象分层。

### 安全性：⭐⭐⭐⭐⭐（优秀）

安全设计全面且扎实。凭证掩码、`ConstantTimeCompare`、`KeyTag` 的 `-` 分隔规则、`api_keys` 最小长度强制、`admin/status` loopback 限制、审计文件 0600 权限、代理环境变量被刻意忽略——这些都不是"看起来安全"的表面功夫，而是每个具体场景都有针对性的防护。分层防御（header 黑名单 + Adapter 覆盖）的做法尤其值得学习。

### 可观测性：⭐⭐⭐⭐⭐（优秀）

可观测性设计是 VMR 的另一大亮点。飞行记录器式审计日志（每请求一行 JSONL，两层往返、每次 failover 尝试、归一化步骤列表）在 LLM 网关领域是少见的深度。`vmr report` 的九个章节报告、Agent 会话分析、工具使用报告、定价集成、逐请求详单——这些功能构成了一套完整的"事后分析"能力。纯观测标记（`soft_block_detected`、`crlf_framing_suspected`、`thinking_process_pattern_detected`）的设计理念特别值得学习：不改字节，但给运维提供信号。

### 错误处理：⭐⭐⭐⭐⭐（优秀）

错误处理设计精细且防御性强。10 个 `ErrorClass` 枚举值驱动不同的 failover 和冷却行为，体现在每种错误场景都有对应的处理策略。`ErrContent` 零冷却、`upstreamHint` 识别网关转发失败、`Retry-After` 封顶、探针槽必须归还——这些细节共同构成了一个健壮的 failover 系统。Fail-open 原则贯穿始终，任何处理失败都不应让本可成功的请求失败。

### 代码质量：⭐⭐⭐⭐⭐（优秀）

代码质量极高。注释风格精准（只解释"为什么"），接口设计小而专注，nil-safe 设计全面，所有权契约明确定义。`archtest` 强制执行架构不变式（import 边界、文件行数上限），这种做法在 Go 项目中很少见但非常有效。依赖管理极其克制（4 个直接依赖），二进制体积小。测试覆盖充分，包含边界场景、并发测试（`-race`）、回归测试。

### 工程 Trade-off：⭐⭐⭐⭐⭐（优秀）

每个设计决策都有清晰的 trade-off 分析。字节拼接 vs 完整解析、全局嗅探 vs 按端点声明、zstd vs gzip、默认 sticky 开启 vs 显式 opt-in、`probe_mode: active` 按次计费 vs 定时轮询——这些决策背后都有量化的成本收益分析，且在文档中记录清楚。"已识别、暂不落地的清理项"（设计文档 §14/§15）这种"知道什么不该做"的自觉同样体现工程成熟度。

### 文档质量：⭐⭐⭐⭐⭐（优秀）

设计文档是该项目的另一个亮点。它不仅是 API 参考，而是完整的"为什么"——每个设计决策都有推理过程、被否决的替代方案、已知边界和暂不落地的清理项。中英双语 README + 用户指南 + 设计文档的组合，让不同背景的读者都能找到合适的信息层级。配置示例文件（`config.example.yaml`）注释详尽，本身就是一个可用的配置模板。

---

## 七、值得类似项目学习的要点

1. **字节忠实透传作为核心原则**：在 LLM 网关/路由器领域，这是 VMR 与 LiteLLM 等翻译型网关的根本差异。如果你的场景是 AI Agent（无人值守运行），字节忠实比 provider 覆盖广度更重要。

2. **接口极简 + 编译期注册**：4 方法 Adapter 接口 + `database/sql` 式注册表，新增协议零侵入。`atomic.Pointer` + `copy-on-write` + `mutex` 的注册表模式可直接复用。

3. **Dimension vs Condition 分离**：排序和淘汰是不同的语义，不应该合并成一个接口。这是正确的抽象原则。

4. **估算值的降级规则**：上下文长度估算建立在可能出错的推测之上——不应该被当成和能力条件同等硬的门槛。估算过高时回退到"宁可尝试也不堵死"的策略。

5. **探测与真实流量解耦**：`probe_mode: active` 的设计思想——被动模式下"谁先撞上半开端点谁就当探针"的问题，通过一个独立的后台探测 goroutine 解决，健康状态机零改动。

6. **飞行记录器式审计**：每请求一行 JSONL，两层往返、完整 failover 轨迹、归一化步骤列表。`vmr report` 的事后分析能力（会话分组、工具使用、成本估算）使审计日志从"被动记录"变成"主动分析"。

7. **纯观测标记**：`soft_block_detected`、`thinking_process_pattern_detected` 等不改字节的标记——先把频率变成可量化的数字，再决定要不要做处理。

8. **可执行的架构测试**：`archtest` 包强制 import 边界和文件行数上限——不是注释里的建议，而是会失败的测试。

9. **依赖极简**：4 个直接依赖，零 Web 框架，零 Provider SDK。透传路由只需"改 URL、注 Key、改 model 字段"。

10. **"知道什么不该做"**：设计文档 §14/§15 的"已识别、暂不落地的清理项"和"决定不修复的问题"清单——这是工程成熟的标志。

---

*审查完成时间：2026 年*  
*审查范围：全部 16 个 internal 包、7 个 cmd 文件、5 个文档文件、3 个 loadtest 文件、配置示例、定价 sidecar、架构测试*