<!-- Ver 2026-08-02 14:00, by Sonnet 5 -->

# Virtual Model Router (vmr) — 设计方案 · Part 1：路由核心

本文档描述 vmr 路由核心的完整设计：定位、架构、机制与关键决策。读完即可维护与二次开发路由主线（`internal/{core,config,adapter,health,probe,strategy,sticky,router,server,audit,imgprep,diagnose,replay,buildinfo,rundir,archtest}` + `cmd/vmr` 里除 `cmd_report.go`/`cmd_story.go` 外的全部子命令）。使用文档见 `README.md`（英文）/ `README.zh.md`（中文）。

**这是 v4 版设计文档的 Part 1（共两部分）**：审计日志的两个离线消费方——聚合报表 `vmr report` 与 Agent 任务叙事重建 `vmr story`（`internal/{report,story,ctxgraph,chatmsg}` + `cmd_report.go`/`cmd_story.go`）——已独立成篇，见姊妹文档 `docs/VirtualModelRouter_Design_v4_Analytics.md`（Part 2）。拆分理由：两者体量已经各自撑起一份完整设计文档，且只通过审计日志的 JSONL 格式（本文档 §9.2）耦合，物理上是两个独立进程/命令，不共享任何路由期状态——继续挤在一份文档里已经不利于阅读。**v3**（`VirtualModelRouter_System_Design_v3.md`）是本文档的上一版，内容已全部拆分到 Part 1/Part 2 两份 v4 文档中，不再维护；早期版本的过程记录如需追溯，见 git 历史。

---

## 1. 定位

本地运行、单二进制、配置驱动的 LLM 路由器。客户端只连稳定的 Virtual Model 名（如 `coding` / `cheap` / `claude`），Provider、账号、Key、优先级、故障切换全部由 vmr 隐藏。Unix 风格工具：零数据库、零 Web UI、零运行时插件。

**边界**（永久有效的"不做"清单）：Dashboard、用户管理、计费、Prompt 管理、Workflow、运行时插件系统、MCP 框架、企业级 AI Gateway、**跨协议转换**（见「协议模型」一节）。每个新需求先过一道门：它增加的是能力，还是复杂度？

---

## 2. 核心概念

| 概念 | 职责 |
| --- | --- |
| **Virtual Model** | 对外暴露的模型名，代表能力而非厂商；对应一组 Endpoint-Group，每组自带协议标签，同一虚拟模型名可以同时挂 openai 面和 anthropic 面 |
| **Provider** | 一个可复用的上游账号定义：`name` + 按协议分 key 的 `base_url` map（`{openai: ..., anthropic: ...}`，至少声明一个）+ 共享的 api_key/proxy；协议信息现在挂在 base_url 的 key 上，不再由 provider 在配置里的位置决定 |
| **Endpoint** | 最小调度单位：Provider × 实际模型名 × 调度属性；一个 Endpoint-Group 的 `models:` 列表展开成多个独立 Endpoint，共享同一组 provider/protocol/capabilities |
| **Adapter** | 协议插件：构造上游请求、转换响应、归类错误；声明自己的协议 |
| **Strategy** | 候选排序器：健康与条件过滤后按维度序列做稳定多键排序 |

Health、并发闸、Sticky Model 亲和状态都不是独立概念，而是运行时状态（见「调度与健康」「并发闸」两节）。

---

## 3. 协议模型：多入口，永不翻译

vmr 同时暴露三个聊天入口，**不做任何跨协议转换**：

```
POST /v1/chat/completions   OpenAI Chat Completions 协议 → 只路由到该协议兼容端点
POST /v1/messages           Anthropic Messages 协议      → 只路由到该协议兼容端点
POST /v1/responses           OpenAI Responses 协议        → 只路由到该协议兼容端点
```

**决策逻辑**：双向流式翻译（Anthropic 的 `message_start`/`content_block_delta` 事件流 vs OpenAI 的 chunk 流，tool-use / thinking 块的语义映射）是 LiteLLM 复杂度失控的根源之一；而主流厂商（MiniMax、DeepSeek、OpenRouter）已原生提供两种兼容面，翻译不创造价值。协议内透传保证零损耗、对上游新字段前向兼容。**Responses 协议同理**：即使它和 Chat Completions 同属"OpenAI 家族"，请求/响应形状（`input`/`instructions`/`output` vs `messages`/`choices`）、流式事件形态（typed SSE events vs delta chunk）都完全不同，把两者揉进一个 Adapter 里翻译，复杂度病灶和 Anthropic↔OpenAI 互转是同一类；按"新协议 = 新 Adapter + 新路由行"接入，不新增任何跨协议转换逻辑。

落实机制：

* 协议是 Adapter 的属性（`Protocol() string`），也是 `models.<name>.endpoints[]` 每条 endpoint-group 自带的字段。一个 endpoint-group 引用的 provider 必须在对应协议下声明了 base_url——引用一个没声明该协议 base_url 的 provider，是加载期错误（"provider 有没有这个协议的 base_url"），不是配置写不出来；但单条 endpoint-group 内部混用两种协议依旧没有语法能表达它，每条 entry 天生只属于一个协议（见「配置参考」）。
* 模型存在但协议不符 → 404，message 指明正确入口（`router.IngressPath` 按协议字符串返回对应路径，三个协议各一条 case，不是二元 if/else——新协议接入时这里必须显式加一条分支，落进默认分支会给出错误的入口提示）。
* 三种协议的请求体都是顶层 `model` + `stream` 字段，路由解析层（`CanonicalRequest`）天然协议无关，`internal/config`（`base_url` 校验、`protocol` 字段校验）、`router.BuildSnapshot`、`internal/strategy`（Condition/Dimension）同样协议无关——新协议接入这几处零代码改动，只需注册 Adapter。
* vmr 自产的错误体为两种客户端都能解析的合并形态：`{"type":"error","error":{"type","message"}}`（OpenAI SDK 读 `error.message`，Anthropic SDK 认 `type:"error"` 信封）。`GET /v1/models` 同理（`object:"list"` + `has_more` + `type:"model"` 并存）。
* 新增协议入口（如 gemini）= 新 Adapter + 新路由行，同样透传。

已接入的厂商协议面（均实测）。同一账号的多个协议面现在共用同一条 provider 条目——`base_url` 是按协议分 key 的 map，各协议面各自一个 key，天然不冲突：

| Provider 名 | base_url（openai 面 / anthropic 面 / openai-responses 面） |
| --- | --- |
| minimax | `https://api.minimaxi.com/v1` / `https://api.minimaxi.com/anthropic/v1` / 未提供（[MiniMax-AI/MiniMax-M2#112](https://github.com/MiniMax-AI/MiniMax-M2/issues/112) 仍是 open 的 feature request） |
| deepseek | `https://api.deepseek.com/v1` / `https://api.deepseek.com/anthropic/v1` / `https://api.deepseek.com`（Responses 面截至上线时只开放 `deepseek-v4-flash`，`deepseek-v4-pro` 待补；强制 `store:false`/`previous_response_id:null`，见下方「Responses 协议接入」） |
| openrouter | `https://openrouter.ai/api/v1`（同一 base，各协议面由各自 Adapter 拼 `/messages`/`/responses`；Responses 面 Beta 状态，同样强制无状态） |

### 3.1 Responses 协议接入（`internal/adapter/openairesponses`）

OpenAI 官方力推的下一代协议（`POST /v1/responses`，[迁移指南](https://developers.openai.com/api/docs/guides/migrate-to-responses)），DeepSeek/OpenRouter 已提供 OpenAI 兼容实现。与 Chat Completions 的关键形状差异：请求体顶层是 `input`（字符串或 Item 数组）+ 可选 `instructions`（system/developer 指导，可选地也能以 `role:"system"`/`role:"developer"` 的 Item 出现在 `input` 数组里），不是 `messages`；响应是 `output[]`（`message`/`reasoning`/`function_call` 等 typed Item 混合数组），不是 `choices[]`；流式是**类型化 SSE 事件**（`response.output_text.delta`/`response.completed`/…），不是 `delta` chunk，也没有 `[DONE]` 哨兵——终止靠类型化的完成事件本身。

**天然复用（协议无关，零改动）**：顶层 `model`/`stream` 字段解析（`CanonicalRequest`）、顶层 `model` 字节 splice 重写（`adapter.RewriteModel`）、响应侧 `model` 字段正则重写（全局字面量扫描，不区分嵌套层级，Responses 把 `model` 放在 `response.created`/`response.completed` 事件体内层同样命中）、`[DONE]` 追加策略（门槛是 `protocol=="openai"`，新协议字符串天然被排除）、顶层 `tools` 数组非空探测（`HasTools`）、按字节估算 token 的 `EstimatedTokens`、审计落盘（body 原样存，不关心内部字段形状）。

**协议专属实现（新写，不能套用 Chat Completions 的实现）**：

* `adapter.RewriteInputRoles`：`role_map` 在 Responses 协议下作用于顶层 `input` 数组而不是 `messages`，且数组元素混杂 message 型（带 `role`）与 function_call/reasoning 等无 `role` 的 Item——与 `RewriteRoles` 共享同一段字节扫描逻辑（`rewriteRolesInTopLevelArray`，只是扫描的顶层键不同），不是重新发明。
* `adapter.SessionFingerprint` 的 `"openai-responses"` 分支：system 等价信号来自顶层 `instructions` **和/或** `input` 数组里前导的 `role:"system"`/`role:"developer"` Item（Responses 的 role 取值比 Chat Completions 多一个 `developer`），两者拼接后一起哈希；`input` 为纯字符串时整串作为首条消息锚点。这不是可选项——Responses 协议下手动回传上下文（`context += res1.output`）时，上一轮的加密 reasoning Item（`encrypted_content`）只有创建它的那个端点能解密，一旦 failover/条件路由换了端点，跟着换的还有"新端点看不懂上一轮的加密状态"这个问题；Sticky Model 的会话亲和正是为同一类问题（prompt cache 按端点失效）设计的机制，扩展它的指纹计算即可原样覆盖 Responses 的这个新问题，不需要另造一套状态保护。
* `imgprep`：Responses 的图片块是 `{"type":"input_image","image_url":"data:...","detail":"...","file_id":"..."}`——`image_url` 是**扁平字符串字段**，不是 Chat Completions 那种嵌套对象 `{"image_url":{"url":...}}`（见 [openai-python SDK 的 `ResponseInputImageParam`](https://github.com/openai/openai-python)，OpenAPI 生成、字段名的权威来源）。`rewriteResponsesImage` 单独实现；`HasImageMarker` 额外加了 `input_image` 子串检查——常见的内联图片场景能靠 `"image_url"` 这个 KEY 本身命中原有 marker，但 `file_id`-only（Files API 引用，无 `image_url` 字段）的块两个既有 marker 都命中不到，需要专门兜底。
* `server/facts.go` 的 `estimateDocumentTokens`：Responses 的 `input_file` 块内联数据字段名是 `file_data`，不是 Anthropic 的 `data`——字段名真的不同，不是嵌套方式不同，需要多认一个 marker。
* `internal/probe.ResponsesRequest` + `internal/router/probe.go` 的 `runProbe` 协议分派：半开端点的后台恢复探测原先无条件用 `probe.Request`（Chat-Completions 形状，顶层 `messages`）——一个 `openai-responses` 端点一旦冷却过一次，后台探测发给它的 body 缺 `input` 字段，几乎必然被拒（400），分类落入 `ErrClient` → `ReportNeutral`，端点永远停在半开态、真实流量视为不可用，**永久锁死**（回归测试：`internal/router/router_probe_test.go`）。`internal/diagnose.testEndpoint` 有同构的三选一分支。

**`internal/replay` 不需要专门适配，已验证**：`vmr replay` 的请求重建全程走 `adapter.BuildRequest`/`router.IngressPath`，两者都已经是协议无关的（前者靠注册表分派到 `OpenAIResponses.BuildRequest`，后者已修好三路分支）——一条 `protocol: openai-responses` 的审计记录不需要 `internal/replay` 包本身有任何改动就能正确重放，回归测试见 `internal/replay/replay_test.go` 的 `TestRun_RealReplayOpenAIResponsesProtocol`。

**明确不做（第一版范围收窄，非遗漏）**：`internal/report`/`internal/story`/`internal/chatmsg`（Part 2 分析工具对 `input`/`output` 结构的消息级解析与会话分组）——这是 Part 2 设计文档覆盖的独立子系统，审计落盘本身对 body 内部结构无感知，Responses 请求/响应体原样落盘、可被 `vmr report`/`vmr story` 读到，只是消息级解析/会话分组暂时把它当"识别不出结构"处理（降级，不是崩溃）；错误分类词表未针对 DeepSeek/OpenRouter 的 Responses 端点新增任何 vendor 专属 sniff——`ClassifyError` 起步复用 `DefaultClassify`，遵循「必须做 body 嗅探，因为实测显示各家习惯不一」这条原则本身要求的前提：先有真实错误样本，再补词表，不能凭空编造。

---

## 4. 系统架构

### 4.1 请求流程

```
Client ── POST /v1/chat/completions | /v1/messages
  │
Server     审计记录开始 → 鉴权(可选) → 缓冲请求体(≤8MB,413) → 并发闸
  │        → 图片降采样(可选) → 解析 model/stream，其余字节不动 → 计算 RequestFacts
  ▼
Router     查 Virtual Model → 校验协议 → 健康过滤 → 条件过滤 → 稳定多键排序 → 会话亲和重排 → 候选序列
  │
  ▼ failover 循环（每个可用候选各试一次，直到成功或候选耗尽；max_attempts 可选设上限）
Adapter    BuildRequest：改 URL / 注入 Key / 改写 model 字段（其余透传）
  │
  ▼
Upstream   ├─ 2xx → 响应归一化 → 转发 → 上报健康成功 → 更新粘性指针 → 审计落盘
           ├─ 4xx/5xx → ClassifyError → ErrClient 直接返回；其余记冷却、试下一个
           └─ 网络错误 → 短冷却，试下一个
```

硬规则：

* **请求体一律入口缓冲**（流式也是）：failover 重放的前提。
* **流式只在首字节发出前允许 failover**；实现上该约束自然成立——仅上游 2xx 后才开始向客户端写，此前的一切失败都发生在写出之前。首字节后的上游错误只能断流并记日志。
* **失败语义**：有真实上游尝试 → 原样返回最后一次上游错误（status+headers+body，`Retry-After` 等原样到达客户端，保留客户端可解析的厂商错误结构）；无候选可试 → 503，消息按具体原因区分（全员冷却中、或某个 Condition 拒绝了全部候选，见「调度与健康」）。凡进入 failover 循环的响应带 `X-VMR-Attempts` 与 `X-VMR-Route-Reason`（成功另带 `X-VMR-Endpoint`），有过失败尝试时再带 `X-VMR-Failover`；路由之前被拒的请求（401/404/413/坏 JSON）不带。`X-VMR-Route-Reason` 形如 `pick=sticky eligible=2/5 cooldown=1 conditions=2 ctx_fallback=1`，只印真正发生过的部分（最常见的按序选中只剩 `pick=order eligible=3/3`）；`X-VMR-Failover` 形如 `deepseek/deepseek-v4:429, minimax/m2:500`，无 HTTP 响应的构建/网络失败记 `:err`。两者都沿用既有的 `X-VMR-*` 例外（§5.4），内容不超出 `X-VMR-Endpoint` 已暴露的范围。**实现约束**：`X-VMR-Failover` 必须在每次尝试**之前**写入截至目前的失败——成功的那次尝试在 `forwardSuccess` 内部自行 `WriteHeader` 后直接返回，不再回到 `Serve`，循环后才写就只有全败路径生效。

* **`role_map`：按 endpoint-group 做 role 改写**：部分 OpenAI 兼容 provider 会拒收它上游不认识的 role（典型：DashScope/千问拒收 OpenAI 为 o1/o3 系列引入的 `developer` role）。`models.<name>.endpoints[]` 的某条 entry 下配 `role_map: {developer: system}`，`adapter.RewriteRoles`（`internal/adapter/classify.go`）在 `RewriteModel` 之后、发出请求之前，用同一套字节级扫描/拼接手法（`topLevelValues`/`skipJSONValue` 与 `RewriteModel` 共享）定位顶层 `messages` 数组里每个消息对象的 `"role"` 键，命中 `role_map` 就地替换值，其余字节（键序、空白、消息正文、未知字段）原样保留；未命中任何映射时零拷贝返回原 slice。挂在 endpoint-group 一级而非 provider——同一账号可能背靠不止一个虚拟模型/上游模型族，不见得都需要同一套改写规则；`core.Endpoint.RoleMap` 随 `BuildSnapshot` 从 `config.EndpointGroup.RoleMap` 原样传下去。审计日志无需为此单独打标：`Attempt.Request.Body` 记录的就是改写后、真正发给上游的字节，与改写前的客户端原始请求对照即可看出差异（同 `RewriteModel` 的既有做法，未走 `Attempt.Norm`——那个字段专属响应侧归一化）。

### 4.2 模块划分

```
cmd/vmr                    CLI（stdlib flag），一命令一文件：main.go（分发 + usage + Adapter 的 blank import 注册点）
  ├─ cmd_start.go/cmd_check.go/cmd_status.go/cmd_diagnose.go/cmd_replay.go/cmd_version.go  各子命令（本文档范围；`cmd_check.go` 一并吸收了原 `cmd_dirs.go` 的职责——尾随 `log`/`cache` 位置参数）
  ├─ cmd_report.go/cmd_story.go/auditpaths.go  `vmr report`/`vmr story` 子命令 + 两者共用的输入路径解析——见 Part 2
  └─ summary.go  start/check 共用的配置摘要渲染（logConfigSummary/providerProxyEntries 等）

internal/core              CanonicalRequest（含 RequestFacts）、ErrorClass、Endpoint（无依赖的共享类型；HealthKey()/Name() 由 BuildSnapshot 调 Freeze() 预计算一次，见 §11"无中心 IR"决策行）+ FilterClientHeaders（header 黑名单，server/replay 共用）
internal/fmtutil           FmtBytes/FmtTokens/FmtSeconds：展示格式化，从 core 拆出，router 实时日志与 report 渲染共用（不该为了打印一个数字而依赖 core 的路由域类型）
internal/rundir            默认目录解析公式（~/.vmr → 系统临时目录 → cwd），config 的 log_dir/image_cache_dir 缺省值共用
internal/config            YAML 加载、${ENV} 展开、校验、热加载 watch
internal/adapter           Adapter 接口 + 注册表 + 共享错误分类表/model 改写/role 改写（classify.go 的 `rewriteRolesInTopLevelArray` 同时驱动 messages 与 input 两种顶层数组形态）；fingerprint.go：SessionFingerprint（Sticky Model 用，按协议分派，含 openai-responses 的 instructions+input 分支）、TopLevelProbe（一次结构化扫描同时取 model/stream/tools-非空，server.go 用于 ingress 预检）
internal/adapter/openai    OpenAI Chat Completions 协议透传 Adapter
internal/adapter/anthropic Anthropic Messages 协议透传 Adapter
internal/adapter/openairesponses  OpenAI Responses 协议透传 Adapter（`POST /v1/responses`，见「协议模型」§3.1）
internal/health            失败驱动的健康状态机（冷却、退避、半开单飞名额）——不知道、也不需要知道半开端点具体怎么被重新验证，那是 router 的策略
internal/probe             探测请求原语：构造带一次性 nonce 回显要求的最小请求 + 校验响应是否回显（diagnose 与 router 共用，二者互不依赖，避免循环 import）；`Request`（messages 形状）与 `ResponsesRequest`（input 形状）按端点协议由调用方分派，body 形状必须匹配协议，否则半开端点的后台恢复探测会被上游当坏请求拒绝、永久锁在半开态
internal/strategy          Dimension 接口 + priority 维度 + 稳定多键排序；Condition 接口 + 编译期注册表（image/tools）+ WithinContext
internal/sticky            Sticky Model 亲和注册表：Peek/Set，不知道任何端点/TTL 细节
internal/router            failover 循环（Serve/tryOne + handleErrorResponse/forwardSuccess，核心，router.go）
  ├─ snapshot.go  ModelRoute/Snapshot 类型 + BuildSnapshot + Install；ModelRoute.EffectiveOrder（start/check/diagnose 三处共用）
  ├─ limiter.go   并发闸（AcquireSlot/Concurrency）
  ├─ transport.go NewUpstreamClient（diagnose/replay 复用）+ copyFlush（流式转发）
  ├─ logfmt.go    实时路由日志的行格式化
  ├─ response.go  响应归一化器：通用状态机（事件切分/model 改写/[DONE] 策略/缓冲-直通决策）；`newRespStream` 按协议短路（`!isSSE`→buffered、`openai-responses`→passthrough，理由同 `!isSSE` 那条——没有已知怪癖形态就不等，见 §3.1）
  ├─ responsefix.go  MiniMax quirk 知识（<think>/Thinking Process 剥离、soft-block marker），response.go 在需要时调用
  └─ probe.go  半开端点的后台探测 goroutine；按 `ep.AdapterType` 分派 `probe.Request`/`probe.ResponsesRequest`（见 §3.1）
internal/server            HTTP 入口、鉴权、审计录制、五个端点（含 `POST /v1/responses`；header 黑名单见 internal/core.FilterClientHeaders）
  └─ facts.go  RequestFacts 计算：文本/图片/文档 token 粗估；model/stream/hasTools 由调用方（server.go 的 adapter.TopLevelProbe 调用）传入，不在这里重新扫描

internal/audit             审计日志（JSONL 落盘）+ 共享的日志文件读取（OpenLogFile/ForEachLine，report/replay 共用）+ OutcomeFor（server/replay 共用的 outcome 判定）
  └─ housekeep.go  历史文件压缩（zstd）+ 按保留期清理

internal/report, internal/story, internal/ctxgraph, internal/chatmsg
                            `vmr report`/`vmr story` 的完整实现——见 Part 2（本节 §9.4 已指路），
                            这里只记它们在 import 图里的位置：均不被 internal/router、internal/server 依赖
                            （只读 internal/audit 的 Record 类型），internal/archtest 强制这条边界

internal/diagnose          `vmr diagnose`：配置校验 + DNS/TLS/代理连通性 + 真实最小请求 + 路由预览
internal/replay            `vmr replay`：从审计记录重建并重发一个请求，用于调试
internal/imgprep           请求内联图片降采样
internal/archtest          可执行的架构不变式（import 边界、核心文件行数上限），见 §11"无中心 IR"决策行
```

依赖 `gopkg.in/yaml.v3`、`fsnotify`、`golang.org/x/image`（图片降采样的 WEBP/BMP 解码与缩放，Go 官方扩展库）、`github.com/klauspost/compress/zstd`（审计历史文件压缩，纯 Go 无 cgo）、`github.com/tsenart/vegeta`（仅 `loadtest/` 性能验证用，非运行时依赖），其余标准库——不用 Web/CLI 框架、不用任何 Provider SDK（透传路由只需"改 URL、注 Key、改 model 字段"，SDK 只带来二进制膨胀与版本纠缠）。

### 4.3 端点一览

| 端点 | 说明 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions 协议入口 |
| `POST /v1/messages` | Anthropic Messages 协议入口 |
| `POST /v1/responses` | OpenAI Responses 协议入口（见「协议模型」§3.1） |
| `GET /v1/models` | 全部 Virtual Model，合并格式，带 `vmr_protocol` 字段 |
| `GET /admin/status` | `instance`（进程身份 + 配置新鲜度）+ `reload`（最近一次热重载结果）+ `sticky.entries` + 端点健康 + 并发指标 JSON（健康部分含 `probing` 字段——某个端点当前是否正被一次后台恢复探测占着单飞名额）；仅接受 loopback 来源 |

**`instance` 块**（`pid` / `listen` / `models` 数 / `version` / `config` 绝对路径 / `started_at` / `uptime_seconds` / `config_stale` / `config_mtime`）回答"接到这个端口的人，怎么知道应答的是哪一个 vmr"。本机跑多个实例时外部只有端口号可依据，而**监听地址只存在于那个进程的 config 里、不在命令行上**，进程表回答不了。`config` 取绝对路径（`WithInstance` 里 `filepath.Abs`，那是进程还知道自己原始工作目录的唯一时刻）；未调用 `WithInstance` 时（测试、嵌入）整块省略而非输出零值。

`version` 来自 `internal/buildinfo`：Go 默认把 VCS 状态压进任何仓库内构建的二进制，`runtime/debug.ReadBuildInfo()` 运行时读得到，**不需要 ldflags/生成文件/Makefile**。渲染为 `fbc034c` 或 `fbc034c-dirty`，后者说明该二进制的源码不在任何 commit 里。不编造 semver——没有能递增它的发布流程。

**`config_stale` + `reload` 块**关掉一个静默失败面：热重载拒绝坏配置后继续用旧快照服务（这是对的），代价是进程会一直服务一份与磁盘不符的配置而外部看不出来。`ConfigStale` 拿配置文件 mtime 与**最后一次成功加载**的时刻（`ReloadState.OKAt`：进程启动或最后一次被接受的重载）相比——用 `OKAt` 而非最后一次*尝试*是关键，否则被拒的那次重载会抹掉自己的证据。它同时覆盖"重载被拒"和"重载压根没触发"（fsnotify 不可用、编辑器换 inode）两种形态，`vmr status` 印成顶部 WARNING 行。

鉴权（可选 `api_keys`，字符串列表）同时接受 `Authorization: Bearer` 与 `x-api-key`，作用于 `/v1/*`。命中哪把 key 就用 `audit.KeyTag`（那把 key 自身的尾部）给这次请求打上 `client_key_tag`，供 `vmr report` 事后按调用方分组导出（见「审计日志」）——`api_keys` 是唯一的鉴权面，`authenticate()` 只有这一条代码路径。不配置 `api_keys` 时鉴权整体关闭，但 `authenticate()` 仍会对客户端自愿发来的 `Authorization`/`x-api-key` 值调用同一个 `audit.KeyTag`（无 16 字符下限——这个模式下它不是需要保护的密钥），让纯内网场景下客户端可以零 vmr 侧配置地自报身份标签；不发任何凭证的请求依旧是未打标签的记录。

---

## 5. Adapter 机制（扩展性核心）

接口三个方法；注册用 `database/sql` 驱动模式（编译期注册，非运行时插件）。响应体不经过 Adapter——协议内透传是硬原则，仅有的响应处理（见下文「响应侧归一化」）在 router 层，Adapter 接口不预留响应变换钩子（预留即负债：没有第二种实现验证的接口形状大概率是错的）：

```go
type Adapter interface {
    Protocol() string          // "openai" | "anthropic"：该 Adapter 服务的入口协议
    BuildRequest(ctx, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error)  // 第二个返回值 = 出站 body 字节，审计直接引用，省掉 GetBody+ReadAll 再拷一份
    ClassifyError(status int, body []byte) core.ErrorClass
}
// 新增 Provider 协议 = internal/adapter/<name>/ 一个包 + main.go 一行 blank import
```

`CanonicalRequest{Model, Stream, Raw, Header, Facts}`：只解析路由所需字段，`Raw` 保留原始字节（前向兼容）；`Header` 是黑名单过滤后的客户端 header（凭证已剥除，含 `anthropic-version` 等协议头）；`Facts` 是条件路由用的请求特征（见「调度与健康」）。model 改写是字节级 splice：单趟免分配扫描定位**顶层** `model` 键的值区间（字符串跳跃走 `bytes.IndexByte`，多 MB content 也是 memchr 速度；嵌套在 messages/tool schema 里的 `model` 键不受影响），然后前缀 + 新值 + 后缀三段拼接——除 model 值外**逐字节保留客户端原文**（键序、空白全保留）。这是 failover 每次 attempt 都要执行的主路径操作，字节 splice 比"整体 unmarshal 成 map 再重新序列化"快约一个数量级（200KB body 实测 99µs、分配 5 次），代价是只能处理规整的顶层 key；虚拟名与上游名相同时零拷贝直接返回原 slice。扫描器搞不定的形态（非对象、无顶层 model 键、语法异常）回退到一条 `map[string]json.RawMessage` 重建路径，兼顾"缺键则补"这类边界语义。

### 错误分类（决定 failover 质量的关键）

```go
ErrClient     请求本身有问题 → 直接返回客户端，不切换
ErrAuth       401 / 403（非内容类）→ 长冷却（10min 起），切换
ErrRateLimit  429 → 尊重 Retry-After（秒/HTTP-date），切换
ErrEndpoint   端点持续不可用（额度耗尽/402、模型不存在/404 或 400+嗅探；402/404 先过内容词表，命中归 ErrContent；
              或 4xx body 里出现"网关/中转层自报转发失败"措辞——upstreamHint，见下）→ 长冷却，切换
ErrTransient  5xx/408/529/超时/网络 → 短冷却（2s 指数退避；带 Retry-After 则从其值），切换
ErrContent    内容合规拦截 → 切换，但不惩罚端点健康（零冷却）
```

`core.ErrorClass` 还有四个值只在审计侧使用（`ErrBuild`/`ErrNetwork`/`ErrCanceled`/`ErrTruncated`，字符串分别是 `build`/`network`/`canceled`/`truncated`）：它们对应 HTTP 响应到达之前就失败的路径（构建上游请求出错、拨号失败、客户端中途取消、成功后流断），从不经过 `ClassifyError`，也从不传给 `Health.ReportFailure`/`ReportNeutral`——健康/failover 逻辑完全不知道它们存在，纯粹是给 `internal/audit`（`Attempt.ErrorClass`）提供一套统一取值，不必另造一套字符串。

`ErrContent` 是"按请求"而非"按端点"的错误：各厂对内容的敏感度不同，换端点常能成功，所以必须继续 failover；但被拦的端点本身完全健康，绝不能因此进冷却（否则一条敏感请求就会把健康端点打下线）。若该端点恰处半开探针中，以 `health.ReportNeutral` 只释放探针、不加深退避。全部候选都被拦时，客户端原样收到最后一次内容错误。

分类表两 Adapter 共享（`adapter.DefaultClassify`），差异点各自覆盖（如 anthropic 的 529）。**必须做 body 嗅探**，因为实测/官方文档显示各家习惯不一：

* MiniMax 未知模型返回 400（非 404）；内容违规错误码 1026/1027；
* DeepSeek Anthropic 口对错模型名的措辞是 "The supported API model names are …"；内容风险走 400 + "risk" 类消息（其官方错误码表 400/401/402/422/429/500/503 中无内容专码）；
* OpenRouter：402 余额不足；**403 = moderation flag / guardrail 拦截**（body 带 "flagged"、`metadata.reasons`）；429 与 503 都可能带 Retry-After；
* 有厂商额度耗尽也发 429（body 见 insufficient/quota/balance/credit）。

嗅探词表：模型类 = `model` × {unknown, not found, does not exist, invalid model, supported}；内容类 = {content_filter, content_policy, moderation, flagged, guardrail, inappropriate, exists risk, data_inspection, (1026), (1027), sensitive, 敏感, 违规, 合规}（中英并收）+ 状态码 451。取舍：误判的代价只是一次无害切换，漏判的代价是永不 failover（400 内容错被当 ErrClient）或误罚健康端点（403 被当 ErrAuth）——宁可宽。

`upstreamHint`（网关转发失败嗅探）是这套宽松取舍里唯一一处刻意收窄的例外：只匹配"upstream request failed" / "upstream error" / "upstream connect error" / "error from provider" / "bad gateway" / "gateway timeout" 这类**明确把失败归给转发这一跳本身**的措辞，不匹配单独出现的 "upstream"/"gateway" 字样——那样宽松匹配的话会连带命中真正的请求内容错误（错误信息里恰好提到这两个词）。触发场景：某个 relay/网关层自己转发失败，返回一个不点名任何请求字段的 4xx（例：`{"message":"Error from provider (X): Upstream request failed", ...}`），若无此规则会被兜底判成 `ErrClient` 而直接放弃 failover——换任何端点都不会重试，即便队列里还有健康的候选，这正是这条规则要堵上的口子。判定顺序：内容词表 > 模型未知词表 > `upstreamHint` > 兜底 `ErrClient`，三者都命中同一段文本时内容/模型判定优先。

**已知边界**：个别厂商（如 MiniMax）会在 HTTP 200 响应内嵌合规标记（`input_sensitive`/`output_sensitive` 等字段）并可能返回空/替换内容。响应归一化器会嗅探这两个标记并记入审计 `norm`（`soft_block_detected`，见下文「响应侧归一化」），但**仅观测、不干预**：字节原样到达客户端，不触发 failover、不影响端点健康——这是先把频率变成可量化的数字，再决定要不要做请求预处理插件（见「路线图」）的第一阶段。把这类响应变成主动拦截或自动 failover 仍是未实现的未来方向。

### 5.4 请求侧 Header 透传策略

策略是「默认透传 + 小型黑名单」，而非严格白名单。LLM SDK 发出的 header 集合是已知且固定的——里面**没有**危险 header（不会发 `Cookie` / `X-Forwarded-For` / `Proxy-Authorization`），所以默认透传是安全的；反过来，白名单会丢掉客户端的合法元数据：User-Agent、OpenAI JS SDK 的 `X-Stainless-*` 系列、OpenTelemetry 的 `Traceparent`——MiniMax 看到的若是 Go 默认 UA，就丢了「这是 OpenAI 兼容客户端」这个信号，可能走不同的服务路径。需要显式 blocklist 的是真正会出问题的少数几项：

| Header | 原因 |
| --- | --- |
| `Authorization` / `x-api-key` | 客户端发的是「给 VMR 的凭证」，VMR 注自己的 key 上游，绝不能让客户端的 key 漏到上游 |
| `Cookie` / `Proxy-Authorization` | 浏览器/代理会话状态，与 LLM API 无关 |
| `X-Forwarded-*` / `X-Real-Ip` | IP 欺骗向量，上游可能据此做访问控制 |
| `Host` / `Content-Length` / `Transfer-Encoding` / `Connection` | Go Transport 自动管理，传过去会冲突 |
| `Accept-Encoding` | 手工设置会关闭 Go Transport 的透明 gzip：上游若返回压缩体，响应归一化的 regex 会在 gzip 字节上跑，且客户端只收到 Content-Type（拿不到 Content-Encoding）——必须让 Transport 自己协商并解压，各层始终处理明文 |

**与「必须由 Adapter 覆盖」的几项不冲突**：`Authorization` 在 blocklist 里**也是**由 Adapter 用 `Header.Set` 覆盖，blocklist 是第二道防线（如果上游意外处理了一个客户端的 Authorization，VMR 至少不会主动转发）。这种「belt and suspenders」是必要的——Header.Set 覆盖只对 Adapter 构造的请求有效，对 VMR 自己生成的请求（如 `/admin/status`）不适用。

### 5.5 响应侧归一化（`internal/router/response.go`）

上游的响应进入 VMR 后、转发到客户端之前，经过一个**归一化层**。响应体不经过 Adapter，协议内透传、**Adapter 之间不做转换**是全文的设计原则。但光透传会让上游的「指纹」原样到达客户端，**部分客户端 SDK 会因此失灵**：

| 问题 | 表现 | 原因 |
| --- | --- | --- |
| 响应 `model` 是上游名（如 `"MiniMax-M3"`）而客户端发的是虚拟名（如 `"agent"`） | OpenAI JS SDK 按 `model` 做 prompt cache 关联 + per-model hook，**静默丢消息** | SDK 假设 `response.model === request.model` |
| 上游发 `<think>...</think>` 标签在 content 里 | 思考被持久化进 assistant message，下一轮请求的 prompt 含上轮思考 → **模型陷入自我指涉的反馈循环** | MiniMax M3 在 thinking 模式下把推理放在 content 里 |
| 上游发 `<think>...</think>` 后无 trailing newline trim | 助手消息以两个空行起头，每轮累积 | MiniMax 的固定格式 |
| 上游不发 `data: [DONE]` | 客户端 SDK 的 stream 终止逻辑靠 EOF 而非 `[DONE]`，正常路径没问题，**边界条件下触发 `APIUserAbortError`** | MiniMax 直接关 TCP |
| MiniMax thinking=medium 下以纯文本 `Thinking Process:` + 编号小节 1-5 + `Final Polish` 草稿输出思考 | 思考+草稿直接展示给用户（`Reasoning: off` 是 UI 开关，**不影响模型行为**） | 模型在 thinking=medium 下不写 `<think>` 标签 |

**指导原则：直连等价**。客户端经 VMR 收到的字节应与直连上游一致，仅有的偏离是（a）`model` 字段改回虚拟名（虚拟模型抽象的根基），（b）两个 MiniMax-M3 专属修复——且只在**确认命中其确切形态**时才触发，失手时的行为=直连行为，永不更差。

**双传输模式**（按响应逐个决定）：

* **passthrough（SSE 缺省）**：逐 SSE 事件实时转发——真流式，除 model 改写外字节一致。首个承载有效载荷的事件（非空 `content`/`text`/`reasoning_content`/`thinking` 值、`tool_calls`、`partial_json`）一旦证明响应**不是** MiniMax 思考形态，立即定型为透传；此前仅暂扣 role marker/ping 等零载荷小事件。
* **buffered**：整体缓冲，EOF 时单遍 regex 归一化。用于：非 SSE 响应（单 JSON，客户端本来就等完整体）；首个载荷事件含 `<think>` 或 content 以 `Thinking Process:` 开头的 SSE（思考期间客户端本就无正文可看，缓冲无体验损失）；未及判定即 EOF 的小流。**`<think>` 触发的缓冲在 closer 到达后恢复流式**（剥掉 think 块、已缓冲部分一次吐出、其余实时转发）——客户端只等了思考阶段；`Thinking Process:` 形态的结束标记在响应末尾才可识别，无法恢复，只能全缓冲。缓冲上限 32MB，超限放弃归一化降级为原样透传（=直连行为）。
* **opaque**：上游响应带 `Content-Encoding`（Go Transport 未透明解压的压缩形态）→ 原样转发零变换——对压缩字节跑 regex 只会损坏它。

**变换清单**（每项触发条件独立，实际生效的集合记入审计 `attempts[].norm`）：

| 变换 | 触发条件 | norm 标记 |
| --- | --- | --- |
| `"model"` 改写回虚拟名 | 事件/响应体中出现顶层 `"model":"…"`（JSON 转义的 `\"` 不会误命中 content 内文本） | `model_rewrite` |
| 剥 `<think>...</think>` 块 + closer 后的 `\n` 填充（转义与真实换行都收） | **守卫：首个非空 `content`/`text` 值以 `<think>` 开头**（与 Thinking Process 的守卫对称）+ 缓冲模式且 regex 命中 | `think_strip` |
| 剥 "Thinking Process:" 结构化思考 | **守卫：首个 `"content":"` 值以 `Thinking Process:` 开头**（前导转义空白可跳过）+ 存在 `Looks good. Pro(ceed)` 自认可标记；按 `\n\n` 切 data: line，弃中间思考行、末行从标记后截取；marker 即首行时原地截取不复制 | `thinking_process_strip` |
| 追加 `data: [DONE]\n\n` | **仅 openai 协议 + SSE + 上游未发**（MiniMax 直接关流；上游已发绝不重复；Anthropic 协议无此哨兵，永不追加） | `done_appended` |
| 软拦截标记嗅探（**不改字节，仅记录**） | 响应体（buffered 整体或 passthrough 单个事件块）内出现 `"input_sensitive":true` 或 `"output_sensitive":true` | `soft_block_detected` |
| CRLF 分帧嗅探（**不改字节，仅记录**） | `eventSep`（`\n\n`）全程未找到边界、退到整体缓冲兜底时，累积字节里出现 `\r\n\r\n` | `crlf_framing_suspected` |
| Thinking Process 泄漏形态嗅探（**不改字节，仅记录**） | `thinking_process_strip` 未触发（首个 content/text 值没以字面量 `Thinking Process:` 开头），但累计响应内容里 `\n<数字>.` 编号小节标记命中 ≥3 次且总字节 >1KB——覆盖 passthrough 与 buffered 两条路径，因为该形态判定同时也是 `decide()` 选择 passthrough/buffered 的依据 | `thinking_process_pattern_detected` |

`isSSE` 由上游响应的 `Content-Type` 判定（含 `text/event-stream`；缺失时回退到客户端的 `stream` 字段），而非盲信请求参数——上游若忽略 `stream` 返回 JSON，原样透传。

**已知边界：quirk 修复靠全局嗅探而非按端点声明**。think-strip / Thinking Process strip 对所有 provider 的响应做形态检测，而不是只对声明了该 quirk 的 endpoint 启用。两种形态的触发守卫是对称的：**只有首个非空 `content`/`text` 值以触发标记开头**（`<think>` / `Thinking Process:`）才认定为思考形态——正文中段合法引用 `<think>...</think>` 字面量（比如用户请模型解释 think 标签格式、代码示例里复现它）原样透传，不缓冲不剥离（回归测试 `TestRespStream_ThinkQuotedMidTextUntouched` 等锁定）。理论误伤面因此收窄到「回复恰好以触发标记开头」这一种；endpoint 级 `quirks:` 配置是一个新概念 + 新配置面 + 用户须理解各厂内部行为才能填对，为剩余误伤面引入配置维度不划算。若未来实际发生误伤，升级路径是加 endpoint 级开关，嗅探逻辑可整体复用。

**为什么处理单位是完整 SSE 事件，而不是字节流或整个响应体**：归一化必须解决两个互斥的诉求——事件内 JSON 要完整（model 改写、正则匹配都不能跨 JSON 边界，否则任意字节切分点都是潜在 corner case），同时不能牺牲真流式（整体缓冲到 EOF 才处理，TTFB 会退化成完整生成时长，逼近甚至触发客户端 SDK 的超时预算）。以完整 SSE 事件为单位同时满足两者：事件内 JSON 天然完整，model 改写不会跨界；跨事件的 think 块只在确认命中触发形态后才进入缓冲，缓冲段内部仍是对完整事件做单遍正则，不是字节级状态机。复杂度病灶从来不在"什么时候要切换模式"，而在"用什么粒度切分字节"——按字节切分才会有装不下的 carry、状态半途丢失、重入吐残留这类 corner case；按事件切分则没有。

---

**上游自报模型名的采集（`attempts[].upstream_model`）**：响应侧把 `model` 改回虚拟名的那次替换，是这条信息唯一存在过的时刻——审计有意不存成功尝试的响应体，客户端那份的 `model` 已被改写。故在替换前读出上游原值，**只在它与请求的真实模型名（`ep.Model`）不同时**记录，相同即不记。

**只记原值、不下判断**：版本钉死（`gpt-4o` → `gpt-4o-2024-08-06`）、厂商前缀、套餐别名（`ark-code-latest` → `doubao-seed-code-251015`）每次都会合法地不一致，区分"别名"与"中转商偷换"只能靠聚合分布（稳定映射是别名），属于离线的 `vmr report`。代价是每个响应多一次正则扫描（`modelSeen` 闩住，不是每个 SSE chunk），纯读不改字节。

## 6. 调度与健康

### 6.1 调度 = 过滤 + 稳定多键排序

```go
type Dimension interface {
    Name() string
    Compare(a, b *core.Endpoint) int   // <0: a 优先；0: 平手交给下一维度
}
候选序列 = 稳定多键排序( 健康过滤(endpoints), 配置的维度列表 )
```

Priority、Weight、RoundRobin、Latency、Cost 都只是排序维度，任意组合（`strategy: [priority, weight]`）；新增策略 = 新增维度实现，路由主流程永不改。当前实现 `priority`（数字小优先，平手保持配置文件顺序）；其余在路线图。

### 6.2 健康：冷却 + 半开恢复探测（后台探测）

* 失败按类别计冷却：Transient 2s 起指数退避（×2 封顶 5min）；Auth/Endpoint 10min 起（封顶 1h）；RateLimit 与 Transient 优先 `Retry-After`（429/503 都可能携带），**但同样封顶 1h**——Retry-After 是上游可控输入，一个畸形的超大值不该把端点锁死到进程重启。内容合规（ErrContent）零冷却。
* 冷却中被健康过滤剔除；到期进入半开，此时半开端点永远不放行真实请求：发现某个端点半开且当前没有探测在跑，就用 `Health.Acquire` 抢下单飞名额，起一个后台 goroutine（`internal/router/probe.go` 的 `runProbe`）发一个 `internal/probe` 构造的最小请求（要求模型原样回显一个一次性 nonce，`internal/probe.Echoed` 做子串校验），真实请求本身仍旧把这个端点当不可用处理，直接路由到下一候选。探测结果走跟真实请求完全相同的 `ad.ClassifyError` 判定，落到 `ReportSuccess`/`ReportFailure`/`ReportNeutral` 三者之一——2xx 视为恢复（回显没对上只记日志、不惩罚，避免模型偶尔不遵循指令误伤一个其实健康的端点）；4xx 且分类为 `ErrClient`/`ErrContent` 视为"探测请求本身的问题，与端点健康无关"（`ReportNeutral`）；其余（含探测超时，受 `probe_timeout` 约束，默认 15s）视为真失败（`ReportFailure`，按原分类计相应冷却）。这条路径要解决的问题：如果放任"谁先撞上半开端点谁就当探针"，探针请求本身很大很慢时（比如一段几十万 token 的长对话），恢复检测的时长就跟这个具体请求的体量强绑定，期间同一进程里所有其他并发调用方也会被连带拖累；探测跟真实流量解耦之后，这个连带效应被消除，真实请求永远不必等探测、也不会因为探测变慢。
* **探针槽必须在每种结局下都归还**——中性结局共两类：内容拦截（ErrContent）、ErrClient（坏请求原样返回）；漏掉任何一类的释放，探针一旦撞上对应类型的请求，`probing` 就会永久为 true，端点锁死到进程重启（这个不变式由回归测试锁定：`internal/server/server_active_probe_test.go`）。`upstreamHint` 命中的 `ErrEndpoint` 必须真的走到 `ReportFailure`、不能被误并进上面的中性分支——这条路径的同步 `tryOne` 侧由 `TestUpstreamGatewayFailureContinuesFailover`（`server_test.go`）锁定，异步 `runProbe` 侧由镜像的 `TestActiveProbe_UpstreamFailureGoesToReportFailure`（`server_active_probe_test.go`）锁定，两条路径各一份，互不替代。
* 客户端主动断连不计入端点失败（与上游健康无关，防状态污染）——真实请求从不持有探针槽，所以断连也不需要额外释放探针的逻辑。
* **配额窗口 vs 余额耗尽不做区分**：两者都归 ErrEndpoint（10min 起指数退避封顶 1h）。不对"N 小时窗口配额"单设更长冷却（如 5h 后再试）——厂商错误信号无法可靠区分两种耗尽，且现行封顶 1h 意味着最坏情况每小时只花一次失败探针请求，充值/窗口刷新后一小时内自动回归；专设长冷却省下的探针成本可忽略，代价却是恢复迟钝。
* 健康注册表以 `provider/model/key指纹` 为稳定键、独立于配置快照存活——**热重载不清零冷却**（否则每次改配置都会把 429 中的端点放出来重打）。重启即重置，不持久化。
* 每次某个端点从冷却恢复都会多花一次探测请求（`internal/probe.Request` 默认 `max_tokens: 300`——实测部分推理模型会把预算耗在 `<think>` 块上，太小的预算会让回显校验大面积假失败，见文件内注释）——这笔额外成本按次计费而非按时间轮询（没有独立的后台定时探测器，触发点完全绑定"真的有请求撞上这个半开端点"），且冷却机制本身已经把"同一个端点反复失败"的探测频率限制在秒级到小时级的退避区间内，不会失控增长。权衡细节和被否决的替代方案（提示词/请求参数尝试压制模型思考过程）不在本设计文档维护范围，只记录最终取舍：不做 provider 专属的探测参数特判，保持 `internal/probe` provider-agnostic。

### 6.3 热加载

fsnotify（监听目录，兼容编辑器原子替换，300ms 防抖）+ SIGHUP 兜底。新配置完整校验，失败保留旧配置并打日志——绝不带病上线。路由表（含 http.Client）随快照原子指针交换，运行中请求持有旧快照直至完成；换入时关闭旧连接池的 idle 连接（in-flight 连接不受影响），重载成功后打印配置摘要。

### 6.4 条件路由（Condition-based Routing）

同一个虚拟模型背后的端点不一定等价——有的支持图像输入，有的不支持；不同厂商的上下文窗口也可能相差一个数量级。「调度=过滤+排序」一节的 `Dimension.Compare(a, b *core.Endpoint) int` 只比较两个端点，看不到请求本身，这对 priority/未来的 weight/latency 是对的（它们本来就不需要知道请求内容），但表达不出"这个端点能不能处理这条具体请求"——这是一个准入（elimination）判断，不是排序（ordering）判断，需要一个与 `Dimension` 平行的新接口：

```go
// internal/strategy
type Condition interface {
    Name() string
    Eligible(ep *core.Endpoint, facts core.RequestFacts) bool
}
```

已注册的 Condition **全部无条件参与过滤**，不需要像 `VirtualModel.Strategy` 那样在配置里声明一份名字列表——Condition 之间是纯 AND 语义、顺序无关，且"端点没声明某个能力字段"天然等价于"对这个条件不设限"，没有 Dimension 那种"要不要参与排序、参与顺序"的选择要做。

**`core.RequestFacts`**（挂在 `core.CanonicalRequest.Facts`）是请求侧廉价、预计算一次的信号集合，`internal/server/facts.go` 的 `computeRequestFacts(body []byte, imageCount int, hasTools bool) core.RequestFacts` 在请求体缓冲完成后算一次。`imageCount`/`hasTools` 都由调用方（`server.chatHandler`）传入，不在 `computeRequestFacts` 内部重新扫描请求体——`hasTools` 来自 `adapter.TopLevelProbe`（Phase 2 item 2.2）一次结构化扫描同时提取的 model/stream/tools-非空三个顶层字段，替代了原来 `json.Unmarshal` 反射解码 model/stream、外加一次独立的 `HasNonEmptyTopLevelArray(body,"tools")` 扫描；`imageCount`——它就是「请求图片自动降采样」（§7）里 `imgprep.Downscale` 那一次调用已经算出来的 `len(images)`，这一次结构化遍历（走 `messages[].content[]`，识别真正的 `image_url`/`source` 图片块）同时喂给三个消费者：审计的 `images[]`、这里的 `HasImage`（`imageCount > 0`）、以及下文 `EstimatedTokens` 里图片那部分的常量项——一次探测，三处复用，一条没有图片的请求全程只付一次探测成本，不会因为三个消费者各自扫一遍而扫三次。`HasImage` 刻意不再走"子串扫描命中即认为有图"这条路径：这个字段要喂给下文的硬性淘汰条件，子串扫描对一句引用了类似 `"image_downscale=512px"` 的纯文本内容一样会命中，曾经真实导致一个不含任何图片的请求被硬性条件误判、把候选端点全部淘汰（无 fallback，直接 503）——`imgprep.Downscale` 的结构化遍历只认 JSON 结构上真正的图片块，不受消息文本内容影响。**"检测到"与"解得出格式"是两件独立的事**：一个结构上确认是图片引用、但 payload 解不出真实格式（不认识的格式、数据损坏）的块，仍然计入 `imageCount`——vmr 自己的解码器认不出，不代表上游也认不出，这个块对上游而言仍是一张真图片，不能因为 vmr 解不出就误判成"没有图片"。

```go
type RequestFacts struct {
    HasImage, HasAudio, HasVideo, HasTools, WantsThinking bool
    EstimatedTokens int64
}
```

`HasAudio`/`HasVideo`/`WantsThinking` 目前只是类型占位——两家协议的音视频输入形状、MiniMax 的 thinking 请求参数形状都没有确认，没有检测逻辑填充它们，恒为 false（见下文④）。

**接入点**（`router.Serve()`，既有健康过滤循环里加一步）：

```go
for _, ep := range route.Endpoints {
    if !healthy(ep) { continue }                        // 既有：健康过滤
    if !strategy.Eligible(ep, creq.Facts) { continue }   // 新增：条件过滤（下文有一个例外：上下文长度）
    candidates = append(candidates, ep)
}
```

**诊断**：过滤后候选集为空是最容易让用户困惑的情形（"为什么明明配了好几个端点却说没有可用的"）。只在这条"空候选集"的失败路径上（不在热路径）额外跑一遍，找出是哪个 Condition 淘汰了最后剩下的端点，把原因写进错误消息（`rejected by condition(s): image`），不复用容易误导的"all cooling down or none configured"文案。`vmr check` 同步把每个端点声明的 `capabilities`/`max_context_tokens` 打印出来，让配置缺口在运行前就可见。

配置侧，虚拟模型在 `VirtualModel.Capabilities []string` / `VirtualModel.MaxContextTokens int64` 里声明这个组下所有端点共享的**基线**——同一批可互换的端点通常支持面差不多，写一次即可；端点自己的 `EndpointGroup.Capabilities []string`（`text`/`image`/`audio`/`video`/`tools`/`thinking`，不区分"模态"与"能力"两类——在 Condition 框架里它们是同一种事实）是**叠加**在基线之上的（取并集），`EndpointGroup.MaxContextTokens int64` 则是**覆盖**基线（单个数值没法取并集，声明即覆盖，不声明就原样继承）：

```yaml
models:
  agent:
    capabilities: [text, tools]          # 基线：下面每个端点都继承
    max_context_tokens: 128000           # 基线：同上
    endpoints:
      - protocol: openai
        provider: minimax
        models: [MiniMax-M3]
        capabilities: [image]            # 叠加 -> 生效集合 text,image,tools
        max_context_tokens: 1000000      # 覆盖基线，只对这个端点生效
      - protocol: openai
        provider: deepseek
        models: [deepseek-chat]          # 两者都不声明 -> 原样继承基线
```

两个字段在模型层和端点层都是**未声明 = 不限制**（假设支持一切/无上限，保证旧配置零改动迁移，模型层也不声明时端点层的语义和这个字段引入之前完全一样）；端点的生效能力集合一旦非空就是穷尽式的（allowlist）——运营者需要把端点真正支持的能力全部列出来（基线 + 自己叠加的那部分），遗漏会导致端点被误判为不支持而被条件过滤挡在候选之外，这是数组式声明的已知代价，缓解手段就是上面提到的 `vmr check` 展示（现在按模型基线 + 每个端点自己的叠加/覆盖值分层打印）。

**四类条件的最终定义**：

**① 多模态能力**（`image`/`audio`/`video`/`text`，`capabilities` 数组）——`image` 复用 `imgprep.Downscale` 结构化遍历的结果（见上文 `RequestFacts` 小节），零新增探测成本；`audio`/`video` 只保留 `RequestFacts` 里的骨架字段，探测逻辑未实现——两家协议目前都没有成熟稳定的内联音视频输入形状，过早锁定一个探测正则，协议下次小版本更新就要重写，等字段形状稳定后再补，符合响应归一化 quirk 检测"只在确认命中确切形态才触发"的既有克制原则。`text` 恒真，不需要检测。

**② 上下文长度**（`max_context_tokens`，单个数值）——核心设计约束：**估算宁可偏大，不可偏小**。低估的后果是把请求路由到一个放不下的端点，触发 400，此时上游会用一个明确的 400 拒绝，交由普通的 failover 兜底（浪费一次尝试，不是灾难）；高估的后果是跳过了一个其实能处理的端点，代价是次优而非错误。两种误差不对称，估算公式因此必须偏保守，见下文估算公式。

**③ tools**——单一布尔标记就是能检测到的全部，也是需要的全部。原因是架构层次问题：MCP 是客户端/编排层协议，MCP client 在本地把发现的工具翻译成标准的 `tools` 数组格式后才发给 LLM API，到达 vmr 时 MCP 来源的工具和手写声明的工具在线格式完全相同——vmr 物理上看不到这个区别，不需要、也无法进一步细分"MCP 工具"和"原生 function call"。检测：顶层 `tools` 数组非空，复用 `internal/adapter/classify.go` 已有的 `topLevelValues` 顶层 key 定位器（本来给 `RewriteModel`/`RewriteRoles` 用的，检测顶层 key 存在与否是同一类操作）。

**④ thinking/extended reasoning**——协议形状按厂商分述，实现前建议对照当时最新的官方文档二次核实：

| 厂商 | 请求侧声明形状 |
|---|---|
| Anthropic | 顶层 `"thinking":{"type":"enabled","budget_tokens":N}`；检测须连 `type=="enabled"` 一起判断（`disabled` 也带这个 key） |
| OpenAI（o 系/推理档位） | 顶层 `"reasoning_effort":"low"\|"medium"\|"high"` |
| DeepSeek | 不是请求参数，是模型选择（`deepseek-reasoner` vs `deepseek-chat`）——这类厂商不需要本条件，是端点选型问题 |
| MiniMax | 请求侧参数的确切形状未确认 |

`thinking` 目前**未注册对应 Condition**——注册一个永远不触发的条件不如不注册；协议形状确认后，检测逻辑复用 `topLevelValues` 即可接入，与 image/tools 走同一模式。

**（不做）price**：不是"这个端点能不能处理"的问题，是"都能处理时先试哪个"的问题——排序（Dimension）关注点，不是准入（Condition）关注点，等 `weight`/`cost` 排序维度真正要做时按 Dimension 模式加。

**估算公式**：所有估算都遵循同一条成本原则——优先用长度、字节数、廉价的存在性标记去推断，不解析内容本身；推断可以不精确，代价由 failover 兜底（多用一次大上下文模型是可以接受的代价，解析成本和实现复杂度不是）。

* **文本**：英文按 4 字节/token；中文分词器效率差异很大，在实际对接厂商分词器细节未知的情况下取偏保守的 2 字节/token（比调研到的所有已知分词器的真实开销都高，故意往"多算"的方向走）。单趟扫描原始字节按 `b >= 0x80` 分类，不解码 UTF-8 rune：
  ```go
  var asciiBytes, wideBytes int64
  for _, b := range raw {
      if b < 0x80 { asciiBytes++ } else { wideBytes++ }
  }
  EstimatedTokens = asciiBytes/4 + wideBytes/2
  ```
  直接扫整个原始请求体字节（含 JSON 结构符号），不特意抽取 message content——结构性开销本身也会被计入分子，进一步把估算往偏高的方向推。
* **图片**：不解析像素，按检测到的图片数量乘一个固定常量——每张 3000 token，取自"1920×1080 全高清截图"（agent/coding 工具最常见的附件尺寸）在高分辨率档的实测开销（约 2691 token）留一点余量。图片数量复用 `imgprep.Downscale` 结构化遍历算出的 `imageCount`（同一个值也驱动上文①的 `HasImage`），零新增成本。
* **文档类附件（PDF 及其他未识别的二进制附件）**：DOCX/XLSX 在 vmr 实际代理的原始 API 层不会被当作独立二进制附件编码进请求体——两家协议官方文档都要求客户端"转换成纯文本后直接放进消息内容"，已经被文本估算覆盖。真正需要单独处理的只有 PDF（两家协议原生支持内联 PDF）和"检测到某种文件附件但格式未识别"的兜底场景。不做页数解析，直接按体积折算：`EstimatedTokens += 附件字段的原始 base64 字节数 / 20`——常量 20 校准自 PDF 每页开销 1500-3000 token、常见页面 50-150KB 原始体积的比例区间，不区分格式，统一用同一个保守常量处理，检测走与图片同一模式的廉价标记扫描。已知偏差：对图片/扫描件密集的 PDF 会大幅高估，是可接受的方向性偏差（无非多倾向大上下文端点）。唯一无法解决的盲区：客户端若用 Files API 模式（先上传拿 `file_id`，后续消息只带引用），真正的文件字节根本不在 vmr 看到的请求体里，无法估算，由 failover 兜底。
* **音频/视频**：不做数值估算，只做能力判定（见①）——两家官方都未公布精确的时长换算公式，且从字节里可靠拿到时长还需要解码容器格式，不是廉价操作。

**估算过高导致候选全部被淘汰时的降级规则**：`EstimatedTokens` 是一个可能明显偏高的估算值（尤其文档类附件公式，扫描件场景可能高估几十倍）——如果条件过滤对上下文长度也一视同仁，一旦估算值超过**所有**候选端点声明的 `max_context_tokens`，候选集会被清空，请求连一次真实尝试都不会发生，也就没有机会靠一次真实的上游 400 触发 failover。区分两类条件：`image`/`tools`/`thinking` 是**确定的**——一个端点要么支持要么不支持，所有端点都不支持时直接拒绝是正确行为；上下文长度建立在一个可能出错的估算之上，不该被当成和能力条件同等硬的门槛。

**规则**：条件过滤分两步。先用 `image`/`tools`/`thinking` 等确定性 Condition 过滤出 `hardFiltered`；再用 `strategy.WithinContext`（独立函数，**不**注册进 `Condition` 接口）在其上进一步筛。**如果这一步筛完是空的，但 `hardFiltered` 本身非空，直接回退用 `hardFiltered`**——宁可让一个"看起来太大"的请求真的打到某个端点上，也不让 vmr 自己凭一个可能错得离谱的估算把路堵死。这个特殊处理只放在上下文长度这一个条件上，不推广成 `Condition` 接口的通用能力（没有第二个条件需要这种语义）。

### 6.5 Sticky Model（会话亲和路由）

上游 prompt cache 按精确字节前缀匹配。条件路由一旦生效，尤其是上下文长度这一维——agent 压缩上下文后，同一条对话的估算体积可能突然小到另一个更便宜/更快的端点也能接，若因此换端点，会打掉正在累积的缓存，"选了更合适的模型、总成本反而更高"。Sticky Model 让同一条对话优先留在最近一次成功服务过它的端点上，抵消这个副作用。两者是同一个调度管线里相邻的两个阶段：

```
健康过滤 → 条件过滤（硬性淘汰） → 优先级/权重排序（既有 Dimension） → 会话亲和重排（软性置顶）
```

**识别"同一条对话"**：`internal/adapter.SessionFingerprint(raw, protocol)` 对 system prompt（若存在）和第一条非 system 消息分别算 md5，返回两个独立哈希，不合并、不解析其余内容——只做字节范围定位（复用 `topLevelValues` 定位 Anthropic 的顶层 `system` 字段；OpenAI 侧用同款的消息数组遍历骨架，只扫到第一个非 system 元素为止，代价与对话历史长度无关）。**必须包含 system prompt**：prompt cache 前缀从请求最前面开始比较，system prompt 排在 messages 之前，两个 system 不同的对话即使后续消息逐字相同，上游前缀匹配也早已分道扬镳——只哈希首条用户消息会把不同 Agent 的相同开场白误判成同一条对话。**不包含 `tools`**：`tools` 是结构化数据，若客户端动态枚举工具列表，同一批工具在不同请求里可能序列化出不同字节，会让锚点无谓跳变；`system` 和首条消息都是纯文本，没有这个风险。哈希本身的开销可忽略——`system` prompt 加首条消息常见场景是几 KB 到几十 KB，纯 Go `crypto/md5` 在现代硬件上处理 1MB 数据是个位数毫秒，相对一次真实的 LLM 请求往返（几百毫秒到几秒）可以忽略不计，不是需要优化的性能问题。

**这套指纹计算与 `internal/report/session.go` 的离线会话分组算法是两套独立实现，不共用代码，也不落审计日志**：两者虽然都是"对首条消息取哈希"的思路，但风险取舍相反——`session.go` 服务的是事后报表分组，容忍 system prompt 逐轮漂移（用独立的 `SysChanged` 字段记录，不拿它拆分组）；Sticky Model 服务的是路由决策，必须把 system 算进去才能避免误判。`session.go` 的哈希是它本来就要做的整体消息遍历的免费副产品，调用一个为在线场景优化的字节扫描函数换不来任何速度收益；反过来把这套字节扫描结果写进审计日志给 `session.go` 读，也没有一个真实消费者会用到它。两边保持独立，互不牵制。

**Key 组成**：`client_key_tag`（既有的 `audit.KeyTag` 机制）作为命名空间，不是主键——`sticky_key = client_key_tag + ":" + hex(sysHash) + ":" + hex(firstMsgHash)`。

**TTL 挂在端点，不是虚拟模型**：调研 Anthropic/OpenAI/MiniMax/DeepSeek 四家官方 prompt cache 寿命，前三家落在 5-10 分钟区间，DeepSeek 磁盘缓存"数小时到数天"，差 2-3 个数量级——cache 寿命是上游 provider 的属性，不是虚拟模型的属性，一个虚拟模型完全可能同时挂快缓存和慢缓存两种端点。`EndpointGroup.StickyTTL *Duration` 覆盖单个端点，未设置继承全局 `Config.StickyTTL`（缺省 10 分钟，覆盖 Anthropic 下限和 OpenAI 典型区间）。主要挂 DeepSeek 的端点应该显式声明 `sticky_ttl: 2h` 才能吃到磁盘缓存的真实收益。

**内存淘汰与粘性有效性判定是两件事，不共用同一个数字**：一个 `Registry` 里同时装着分钟量级和小时量级的条目，判定粘性有效性时必须用**这条记录当时指向的那个端点**自己的 TTL；内存淘汰则用一个统一的、比任何端点 TTL 都宽松的粗粒度兜底值——`internal/sticky.BackstopTTL`，24 小时，只负责内存卫生，不参与路由决策。这个 24 小时上限由 `config.validate()` 强制保证：全局 `sticky_ttl` 与任意端点的 `sticky_ttl` 只要超过 `internal/sticky.BackstopTTL`，配置在加载阶段直接拒绝，`vmr check`/`vmr start`/热重载三处共用同一个 `validate()`，都会挡住这类配置——否则会出现"配置写了却不生效"的静默陷阱（该端点的粘性记录会在写入的 TTL 到期前，先被内存清理兜底删掉）。

**架构落地**：独立小包 `internal/sticky`（与 `internal/health` 平行，不塞进 `internal/strategy`——亲和性是独立的运行时状态概念，不是排序/过滤维度）。`Registry` 本身不需要知道任何端点/TTL 的细节——它只是一个带 mtime 的键值存储：

```go
type Registry struct { /* map[string]entry + mutex，仿 health.Registry 结构 */ }

func (r *Registry) Peek(key string) (endpointKey string, lastUsed time.Time, ok bool)
func (r *Registry) Set(key, endpointKey string)  // 命中时刷新 mtime，用于兜底清理
```

"这条记录对不对应端点的 TTL 而言还有效"这个判断留给调用方（`router.Serve`），因为只有调用方在查到 `endpointKey` 之后才知道该用哪个端点的 TTL。

**`Sticky` 默认开启**（`VirtualModel.Sticky *bool`，`nil` 视为 `true`）：哈希本身的开销可以忽略不计，"默认关闭、需要显式声明才开启"不是必要的保护措施，反而会让大多数真正受益的多轮 agent 场景需要多写一行配置才能拿到默认应有的行为。让"不需要 sticky"的少数场景显式声明更符合"服务多数场景"的默认值原则。字段类型用 `*bool` 而不是发明一个否定式的字段名（如 `no_sticky`）——`nil`（配置里没写）= 默认开启，显式 `false` = 关闭，与 `ImageDownscaleMaxPx *int`/`StickyTTL *Duration` 是同一套"指针语义表达三态"的既有模式。

**接入点**（`router.Serve`，紧接条件过滤和既有排序之后）：亲和性重排只在已经通过健康与条件过滤的候选集里生效（找不到匹配端点、或找到但已过 TTL，都直接跳过，什么都不做）——这保证它永远不会把一个当前不健康或不满足本次请求硬性条件的端点复活。粘性指针在**每一次成功完成请求后都更新**，包括 failover 后的成功，不只是第一次建立时——这样一次故障转移会让指针自动跟着移动到"实际生效缓存"真正所在的端点，是自愈设计，不需要额外的失效检测逻辑。

```yaml
sticky_ttl: 10m                     # 全局默认，覆盖 Anthropic/OpenAI/MiniMax 的典型区间；硬上限 24h

models:
  openai:
    agent:                          # sticky 默认开启，不用写 sticky: true
      endpoints:
        - provider: minimax           # 跟随全局 10 分钟
          model: MiniMax-M3
          capabilities: [text, image, tools]
          max_context_tokens: 1000000
        - provider: deepseek          # 磁盘缓存，寿命远超全局默认，端点级显式覆盖
          model: deepseek-chat
          sticky_ttl: 2h
          capabilities: [text, tools]
          max_context_tokens: 128000

    one-shot-summarizer:              # 单次摘要调用，没有多轮价值，显式关闭 sticky
      sticky: false
      endpoints:
        - provider: minimax
          model: MiniMax-M3
```

Compaction（上下文压缩）场景下机制依然成立：压缩本身就会让 cache miss 一次，与 vmr 选哪个端点无关；压缩后的后续轮次共享新锚点，粘性照常生效；`system` prompt 在压缩前后通常不变，是锚点里更稳定的一段。

---

## 7. 请求图片自动降采样

Agent 场景里请求经常带截图/照片附件，但视觉理解通常不需要原始分辨率——图越大，vision token 消耗越高。vmr 提供一个可选开关，在入口把超限的内联图片等比缩小、统一转码，上游收到的是缩小后的版本。

**范围**：只处理请求里的内联 base64 图片（OpenAI `image_url` 的 data URI／Anthropic `source.type=base64`）；不处理 response，也不 fetch 远程图片 URL——两者都超出"改写本地已有字节"的边界。与路线图规划的敏感词过滤插件共享同一接入点（body 解析后、`router.Serve` 之前），但不是同一套机制：图片降采样是具体、确定的处理，不经过预留的插件注册表。

**开关，全局 + 逐虚拟模型覆盖**：全局配置项 `image_downscale`（int，长边像素上限；0/缺省=关闭）——开关即参数，不设独立的 enabled 字段。每个 virtual model 也可以在 `models.<name>.image_downscale` 单独设置，**模型自身的值优先于全局值**；不写则继承全局。`config.VirtualModel.ImageDownscaleMaxPx` 是 `*int` 而非 `int`：nil 代表"未设置，继承全局"，非 nil（含指向 0 的指针）代表"模型显式设置"，0 在模型层面是明确的"强制关闭"——即使全局开着，这个模型也不降采样。用 `int` 存不下这个区分（0 到底是"没写"还是"写了 0"），这是选指针类型的唯一原因。负数（全局或模型级）在 `applyDefaults` 里一律钳制为 0，与既有惯例一致。

运行时对应关系：`router.ModelRoute` 携带同名的 `ImageDownscaleMaxPx *int` 字段（`BuildSnapshot` 从 `VirtualModel` 透传），并提供 `EffectiveImageDownscaleMaxPx(globalMaxPx int) int` 方法解出对某个模型实际生效的上限——nil 接收者安全（未知模型直接回退全局，调用方不用先判空）。`server.chatHandler` 因此需要在做降采样之前先解出 `probe.Model`：JSON 探测解析（`model`/`stream` 两个字段）在并发闸获取与图片降采样**之前**完成——探测本身够便宜，不需要等并发闸，这个顺序也让"坏 JSON / 缺 model"这两类 400 提前返回，不再白占一个并发槽位。

**检测分层，越靠前越便宜**：

1. **无图请求**（预期占比 95%+）：对已缓冲的请求体做一次 `bytes.Contains` 子串扫描（找 `` "image `` 标记），不解析 JSON——这是唯一的常态开销。
2. **命中标记的请求**：反序列化为 `map[string]json.RawMessage`（与 model 字段改写同一模式），只识别 `messages[].content[]` 里 `image_url`/`image` 类型块，未知字段字节不动。
3. **单张图片是否需要处理**：`image.DecodeConfig` 读文件头拿到真实像素宽高（不解码像素数据），长边 ≤ 上限则跳过。用真实尺寸判断而非按字节数估算——同样便宜（只读文件头），但没有"压缩率不同导致误判"的问题：512px 的照片可能是 30KB 或 300KB，字节数阈值在两者间必然选错一个。

**处理**：解码 → 等比缩放（`golang.org/x/image/draw`，`BiLinear`）→ 透明通道摊平到白底 → 统一编码为 JPEG（quality 85）。格式检测支持 JPEG/PNG/GIF（标准库）+ WEBP/BMP（`golang.org/x/image`，Go 官方扩展库而非第三方野包），格式以文件头嗅探为准，不信任声明的 mime type；**GIF 只检测（format/width/height 记入 audit），从不缩放**——原因见下方安全边界。

**安全边界**：

* **GIF 一律跳过缩放，不分单帧/多帧**：跳过多帧动图的原因是缩放会把动画塌缩成单帧，语义改变；但"是不是多帧"这件事本身，标准库唯一的判定方式是 `image/gif.DecodeAll`，而这个函数对帧数、累计解码内存都**没有上限**——一张画布不大（远低于下面 64MP 上限）但帧数极多的构造 GIF，能在"判断出它是多帧、于是跳过"之前就已经把全部帧解码进内存，构成一个真实的解压炸弹向量（且触发条件不苛刻：只要画布长边超过配置的 `image_downscale` 上限即可命中这条代码路径）。为了"缩放少数单帧 GIF 静态图"这个边缘场景，专门为它解出帧数、从而必须先付出这个无界解码代价，不划算——干脆连单帧 GIF 也一并跳过，`image/gif.DecodeAll` 在整条路径上不再被调用，风险连根拔掉。`golang.org/x/image/webp` 本身不支持动图解析，遇到动图 WEBP 通常直接解码失败，副作用上也是跳过，不受此调整影响。
* 解压炸弹防护：`DecodeConfig` 拿到的声明像素总数超过 64MP 的图片拒绝解码，原样透传（一张几十字节的畸形 PNG 足以声明出几亿像素）。
* **fail-open**：解析/解码/编码路径上任何失败（含 panic，`recover` 兜底）都回退到原始字节——这一步的 bug 绝不能让本可成功的请求失败。panic 恢复不完全静默：`recover()` 会向 stderr 打一行 `imgprep: panic recovered ...`，让解码器对对抗性输入的崩溃对运维可见，而不是降采样"永久失效"却无任何信号。

**对现有机制的影响**：改动点仍在 `server.chatHandler` 里，`rec.Client.Request.Body` 记录**之后**——审计的"客户端层"仍记录原始请求；"上游尝试层"（`attempts[].request`）自然记录降采样后的实际出站内容，语义与既有的 model 字段改写一致，无需改动审计结构。并发闸（见下文）天然限制了图片处理的并发数，不引入新的无界并发。

### 7.1 降采样结果磁盘缓存

**动机**：同一张源图片在同一个目标像素上限下，降采样结果是确定的——没有理由重复计算。更重要的是字节一致性：上游（尤其是 Anthropic）的 prompt cache 按精确字节/token 匹配，如果同一张图片每次请求都重新走一遍 JPEG 编码，编码器不保证逐字节输出相同，任何细微差异都会让上游缓存静默失效，而这类失效在日志里几乎不可见——只会看到 `tokens_in_cached` 莫名其妙地低。有缓存的话，同一张图第二次发出的就是完全相同的字节，上游缓存才谈得上稳定命中。

**Key**：`sha256(原始图片字节)` + 目标 `maxPx`。maxPx 必须入 key——同一张图对不同虚拟模型可能用不同的降采样目标（逐模型覆盖），两者是两份不同的结果，不能共享一个缓存条目。文件名 `<hex>-<maxPx>.jpg`，值就是降采样后的 JPEG 字节（输出格式固定为 JPEG，见上文）。

**目录**：缓存目录来自配置项 `image_cache_dir`，审计目录来自 `log_dir`（见「审计日志」）——**都是 config.yaml 字段**，没有对应的环境变量：目录属于"vmr 把数据写到哪"这类必须在 config 里读得出来的事实，与代理同理不留隐式旋钮；要从环境注入就显式写 `log_dir: ${VMR_LOG_DIR}`，`${VAR}` 展开一视同仁。显式值原样使用、不追加子目录（开头的 `~/` 展开为 home）；未设置时走 `internal/rundir.Resolve(homeSubdir, tmpSubdir, pwdSubdir)` 的三层默认：

1. `~/.vmr/<homeSubdir>`——最常见的情况，**持久目录而非系统临时目录**：macOS 会清理约 3 天未访问的用户临时目录条目（重启也会清），而审计日志是 `vmr report` 成本核算的唯一数据源——"默认永久保留"与"默认放在会被 OS 清理的目录"是自相矛盾的，所以默认必须落在持久目录。缓存对应 `~/.vmr/image_cache`，审计对应 `~/.vmr/logs`。
2. 否则 `os.TempDir()/<tmpSubdir>`——只有解析不出 home 目录（如被剥空环境的 service 场景）才会走到；加 `vmr_` 前缀子目录是因为系统临时目录是全机器共享的。缓存对应 `vmr_image_cache`，审计对应 `vmr_logs`。
3. `<cwd>/<pwdSubdir>`——只有 `os.TempDir()` 本身返回空字符串才会走到这里；Go 支持的平台实际上不会触发，纯粹是防御性兜底。缓存对应 `./image_cache`，审计对应 `./logs`。

`vmr check [-c config.yaml] {log|cache}` 打印生效值（缺省后的 `cfg.LogDir`/`cfg.ImageCacheDir`）——不带 `log`/`cache` 参数的普通 `vmr check` 与启动摘要也各打印一行 `dirs log=… image_cache=…`；`vmr.sh` 用 `vmr check -c $CFG log` 查询 server log 的落点，而不是在 bash 里复刻公式（这个单独打印用法原本是独立的 `vmr dirs` 子命令，后来并入 `check`，功能不变）。生效语义：`image_cache_dir` 随快照热重载即时生效；`log_dir` 在启动时打开一次，热重载改它会打"需重启"提示、审计继续写旧目录。

**查找时机**：只在"确认需要处理"（`longSide > maxPx` 且未触发解压炸弹防护）之后才计算哈希、查缓存——绝大多数图片根本不需要降采样，在这条路径之外查缓存只会给最常见的场景白加一次哈希开销。命中则直接返回缓存字节，跳过解码/缩放/编码整段；未命中则走原有全量处理，处理完成后再写入缓存。

**失效策略：按最近命中时间（mtime）的 TTL，不设容量上限**。命中时 `os.Chtimes` 把 mtime 刷新到当前时间——语义是"最近使用"而非"创建时间"，避免一个长会话里反复引用的截图，仅仅因为会话跑得久就被 TTL 判定过期。容量上限被有意省略：类比历史文件保留的取舍（当时也只做了按天数的 retention，没做体积上限），图片缓存的体积由"不同源图片数 × 不同 maxPx 数 × TTL 窗口"共同界定，实践中量级有限（典型部署里 maxPx 的取值种类是个位数——一个全局值加少数几个模型覆盖）；真出现磁盘占用问题，再按路线图补一个容量上限，不为一个尚未出现的问题预先加复杂度。

配置项 `image_cache_ttl_days`（int，缺省/非正数 → 7 天）。和 `audit_retention_days` 的"0=永久保留"刻意不同：审计日志有取证/成本核算价值，删除是数据丢失风险，需要用户显式选择清理；图片缓存纯粹是性能优化，没有对应的"数据丢失"属性，主动清理是更安全的默认值，不需要用户显式开启。

**触发时机**：不额外起 ticker/goroutine。仿照审计 housekeeping 的"事件触发 + 节流"思路，每次命中"确认需要处理"分支时调用一次节流检查——按缓存目录各自记录"今天是否已经扫过"，每个目录每天最多触发一次异步清理（单次 `os.ReadDir` + 按文件 mtime 判断，顺带清理因进程崩溃遗留的 `.tmp-*` 半写文件）。

**写入**：`os.CreateTemp` + `os.Rename`，与审计压缩落盘同一套 crash-safety 模式；失败一律静默忽略（fail-open——缓存只是优化，写盘失败不该让一个已经处理成功的请求失败）。

**vmr.sh 对齐方式**：目录是 config 字段，vmr.sh 对目录的全部参与只剩一件事——用 `"$BIN" check -c "$CFG" log` 查出 `log_dir`，把自己的 server stderr 日志放在旁边（`$LOG_DIR/vmr.log`）；二进制自己读 config，不存在"环境没带对"这个失败模式，脚本不需要额外注入任何目录变量。**这个查询是惰性的**：`vmr check log` 会完整加载并校验 config.yaml，不是纯函数式的目录解析，所以 vmr.sh 只在真正要用到 `$LOG_DIR`/`$SERVER_LOG` 的分支才调用它（`resolve_log_dir`，触发点是 `start`、`service install`、`logs`），不在脚本顶层无条件跑一遍——否则 config.yaml 处于任何损坏状态（哪怕只是编辑中途的语法错误）时，连不需要读 config 的 `stop`/`status`/`service uninstall` 都会先于自身逻辑因为这次查询失败而整体退出，而这恰恰是最需要能停掉进程的时刻。

---

## 8. 并发闸

可选全局上限 `max_concurrency`（缺省 0 = 不限）：

* 两个聊天入口共用一个信号量；超限请求**在内存中挂起等待**（channel 阻塞，近似 FIFO），不排队列、不设等待超时（客户端自有超时，服务端再加一层是重复机制）。
* **闸在请求体缓冲完成之后获取**：慢客户端的上传阶段不占槽，闸覆盖的是 CPU（图片降采样）与上游往返阶段——否则几个慢 POST 就能占满全局并发饿死正常请求。
* 等待期间客户端断开 → 立即出队。
* 只闸聊天入口；`/v1/models`、`/admin/status` 不受限。
* 热重载仅当容量变化才换信号量；换闸瞬间新旧持有者叠加、短暂超额（秒级边界行为，可接受）。
* `/admin/status` 暴露 `limit` / `in_flight` / `waiting`。

每 Endpoint 的 rpm/并发精细限流是另一个问题（服务于主动避免 429），在路线图中。

---

## 9. 审计日志

**目标**：原始、完整、可追溯地记录每一个聊天请求的两层往返（调用方↔vmr、vmr↔上游），只记录不分析；请求数、Token 用量等统计由**外部脚本**事后读取 JSONL 完成——本节格式即该脚本的输入契约。

### 9.1 运行行为

| 项 | 行为 |
| --- | --- |
| 开关 | 默认开启；`vmr start -audit=false` 关闭 |
| 目录 | 配置项 `log_dir`（有设置则原样使用，`~/` 展开）否则持久的 `~/.vmr/logs`（`internal/rundir` 的三层默认规则，与 `image_cache_dir` 共用同一套公式；不用系统临时目录——macOS 会清理它，审计会被静默删除）。启动日志打印实际路径，`vmr check -c <cfg> log` 也可单独查询；改 `log_dir` 需重启生效（热重载会打提示） |
| 文件 | 每天一个：`vmr-audit-YYYY-MM-DD.jsonl`（本地时区，写入时轮转），权限 0600 |
| 时机 | 请求完成后追加一行（含流式全程），不影响 TTFB |
| 失败 | 写盘失败仅打 stderr 日志，绝不影响请求服务 |
| 覆盖 | 两个聊天入口的所有请求，含被 vmr 拒绝的（401/413/坏 JSON/未知模型/协议不符）；`/v1/models`、`/admin/status` 不记 |

### 9.2 记录结构（JSONL，每行一个 Record）

```jsonc
{
  "ts": "2026-07-07T12:15:20.123+08:00",  // 请求到达时刻（RFC3339 毫秒）
  "dur_ms": 864,                          // 总耗时（含并发闸等待与流式全程）
  "ttft_ms": 120,                         // 首字延迟：到达 → 首个响应 body 字节写回客户端；未写出或 <1ms 本地拒绝时省略（0 值视为"无测量"）
  "model": "claude-failtest",             // Virtual Model；解析前被拒则为 ""
  "protocol": "anthropic",                // 入口协议：openai | anthropic
  "stream": false,
  "outcome": "ok",                        // ok（客户端拿到 2xx）| error | canceled（未写出任何响应）
  "replay_of": "vmr-audit-2026-07-13.jsonl:42",  // 仅 `vmr replay --record` 产出的记录才有此字段：来源记录的 "路径:行号"；vmr 自身写入的记录永远没有这个字段
  "client": {                             // 第一层：调用方 ↔ vmr
    "addr": "127.0.0.1:54321",
    "request":  { "method": "POST", "path": "/v1/messages", "headers": {...}, "body": {...} },
    "response": { "status": 200, "headers": {...}, "body": {...} }   // 未写出时缺省
  },
  "images": [                             // 请求内联图片（仅请求侧，vmr 不生成图片）；无图片时省略
    { "message_index": 3, "format": "jpeg", "bytes": 812000, "width": 3024, "height": 4032,
      "downscaled": true, "downscaled_width": 1024, "downscaled_height": 1365, "downscaled_bytes": 96000,
      "cache_hit": false }                // 元数据字段：仅头部解析（DecodeConfig），不论该虚拟模型是否开启降采样都会采集；remote:true 的远程 URL 图片其余字段皆为零值
  ],
  "facts": {                              // vmr 自己的路由前判断（core.RequestFacts 原样落盘，见「调度与健康」§6.4）；
                                           // 是 client.request 的兄弟字段，不嵌入其中——client.request 必须对客户端原始请求保持字节忠实，
                                           // 这里记的是 vmr 从中推导出的东西，放在旁边，不污染原始记录。请求在拿到 model 字段之前就被拒绝
                                           // （鉴权失败、坏 JSON）时整个字段省略，不是零值——"没算过"和"算过、结果是 false"是两回事
    "has_image": false, "has_tools": true, "estimated_tokens": 1280
  },
  "attempts": [                           // 第二层：vmr ↔ 上游，每次 failover 尝试一条，按序
    {
      "endpoint": "anthropic:minimax_badkey:MiniMax-M3",   // 展示用标签，protocol:provider:实际模型（":" 分隔，见下方三段式说明）
      "protocol": "anthropic", "provider": "minimax_badkey", "model": "MiniMax-M3",  // 同样三段，但结构化——读侧不必再解析 endpoint 字符串（历史日志没有这三个字段时，internal/report 的 attemptUpstream() 回退到按 "/" 切分旧版 endpoint，见「配置参考」的 Endpoint 讨论）
      "url": "https://api.minimaxi.com/anthropic/v1/messages",
      "dur_ms": 543,
      "request":  { "headers": {...}, "body": {...} },   // 出站请求（model 已改写）
      "response": { "status": 401, "headers": {...}, "body": {...} },
      "error": "auth",                    // 失败原因：错误类别 | "network: …" | "build: …" | "canceled by client" | "truncated: …"
      "error_class": "auth"               // 与 error 同步设置的类型化枚举值，见下方约定 5
    },
    {
      "endpoint": "anthropic:deepseek:deepseek-v4-flash",
      "protocol": "anthropic", "provider": "deepseek", "model": "deepseek-v4-flash",
      "url": "https://api.deepseek.com/anthropic/v1/messages",
      "dur_ms": 320,
      "request":  { "headers": {...}, "body": {...} },
      "response": { "status": 200, "headers": {...} },   // 见下：成功尝试不存 body
      "norm": ["model_rewrite", "done_appended"],        // 实际生效的响应归一化步骤
      "upstream_model": "doubao-seed-code-251015"        // 上游自报的模型名，仅在与上面的 model 不同时出现
    }
  ]
}
```

六条约定（统计脚本必须知道）：

1. **成功尝试的响应 body 不存**：透传恒等，它与 `client.response.body` 字节相同，只在 client 层存一份；两者的字节差异**完整由 `norm` 列表解释**（`model_rewrite`/`think_strip`/`thinking_process_strip`/`done_appended`/`buffered`/`resumed_stream`/`opaque`/`overflow_raw_passthrough`）——**唯一例外是 `soft_block_detected`和 `crlf_framing_suspected`**：两者都是纯观测标记，不对应任何字节改动，出现时 upstream body 与 client body 仍然完全相同（见「响应侧归一化」）。失败尝试的错误 body（≤128KB，`router.errBodyCap`）存在 attempt 内；超出上限时转发给客户端的字节仍是未改动的截断前缀（byte-faithful 对客户端始终成立），只有 attempt 内的审计副本会在末尾追加 `...(truncated at N bytes)` 标记（N = 上限本身，不是上游真实大小——`io.LimitReader` 故意不读过上限，真实大小未知）。成功尝试后流中断时 `error` 为 `"truncated: <原因>"`（客户端已收到 2xx，outcome 仍为 ok——status 与 error 并存即"当时 200 但中途断了"）。
2. **body 编码，不截断**：合法 JSON 原样嵌入（可直接用 jq 查询，如 `.client.response.body.usage`）；非 JSON（如 SSE 流文本）为字符串。**审计侧不设记录上限**——不论原始 body 有多大都原样记录，没有 `max_body_mb` 这类联动配置，也没有 `body_truncated` 标记。入站请求体大小仍有一个独立的、纯粹为稳定性考虑的上限（`max_request_body_mb`，缺省 8MiB，超限 413）——它只决定 vmr 愿不愿意接受这个请求，与审计记录是否完整无关：只要 vmr 接受了，审计里就是完整的那一份。流式响应的 usage 通常在末尾 SSE 事件里，脚本需从字符串 body 中解析。
3. **凭证掩码**：`Authorization` / `X-Api-Key` / `Api-Key` / `X-Auth-Token` / `Cookie` / `Set-Cookie` / `Proxy-Authorization` 的值只保留末 4 字符（`"Bearer ***abcd"`），其余 header 原样。后三项虽然被 server 层黑名单挡在上游之外，但客户端发来时会进入审计的 client 层记录，明文落盘同样有外泄风险。这是对"完整 header"要求的唯一偏离——审计文件常驻磁盘，明文密钥外泄风险大于取证价值。这份列表与 `core.headerBlocklist` 是两张独立维护、故意不完全重合的表：前者决定"记审计时要不要打码"，后者决定"转发给上游前要不要剔除"，`Api-Key`/`X-Auth-Token` 在前者但不在后者（活的客户端流量里这两个 header 是真值，vmr 默认放行转发；但审计记录里存的是打过码的占位符）。`internal/audit` 导出了 `IsCredentialHeader(name string) bool` 判定函数，`vmr replay` 重建请求头时用它把这批 header 额外剔除一遍——否则会把打码占位符当真实凭据发给上游。
4. **`attempts[].error` / `error_class` 的形态**：`error` 是自由文本（错误类别裸词、或带详情的 `"network: …"` / `"build: …"` / `"truncated: …"` / `"canceled by client"`），供人读；`error_class` 是与它同步设置的类型化枚举字符串（复用 `core.ErrorClass.String()`：`client`/`auth`/`rate_limit`/`endpoint`/`transient`/`content`，加上四个只在 HTTP 响应之前的失败路径出现的值 `build`/`network`/`canceled`/`truncated`），`vmr report` 直接按这个字段归桶。**必须容忍缺失该字段的日志文件**：一部分历史留存的审计文件没有 `error_class`（只有 `error` 自由文本）——`internal/report` 的 `attemptErrorClass()` 辅助函数在 `error_class` 为空时回退到解析 `error`（6 种 HTTP 分类错误本来就是不带冒号的裸类名，直接原样使用；`build`/`network`/`canceled`/`truncated` 这四种非 HTTP 路径本来就是 `"class: 详情"` 前缀，取冒号前半部分），使错误分布、`truncated` 计数在混用新旧格式日志时依然正确，而不是退化成 `unknown`。`internal/audit` 仍是无外部依赖的叶子包，`Attempt.ErrorClass` 类型是 `string` 而非 `core.ErrorClass` 本身，只是复用同一组取值。
5. **`images[]` 的采集范围**：只记录请求侧的内联图片（vmr 不生成图片，响应侧不采集）；`message_index` 是该图片所在消息在 `chatMessages` 里的 0-based 下标。检测**始终进行**，与该虚拟模型是否开启了 `image_downscale` 无关——只做一次廉价的 `image.DecodeConfig`（只读文件头拿 format/width/height，不解码像素），`downscaled`/`downscaled_*`/`cache_hit` 只在实际触发了压缩路径时才有意义。远程 URL 图片（vmr 未拉取内容）记一条 `remote:true`，其余字段皆为零值。
6. **`facts` 是原样落盘，不是事后重新计算**：`server.go` 只算一次 `core.RequestFacts`（喂给路由决策），把同一个值原样存进 `rec.Facts` 再写审计；`internal/replay`/`internal/report` 都不会、也不需要从存下来的请求体反推一份新的 `facts`——这类衍生字段的原则是"算一次、到处传"，不是"谁要用谁自己再算一遍"。字段整体省略（不是每个子字段为零值的对象）代表这条请求在拿到 `model` 字段、真正开始算 `facts` 之前就被拒绝了。

### 9.3 实现要点

`internal/audit`：Record 类型 + Logger（互斥追加、按日期轮转，轮转时异步触发历史文件压缩/保留扫描，见后文）。server 层用包装 `ResponseWriter` 的录制器捕获 client 层响应（保留 `Flusher`，流式时延零影响，`bytes.Buffer` 无上限增长——不再有审计侧截断）；router 层在 failover 循环中逐次填充 attempts，包括 `protocol`/`provider`/`model`/`error_class` 四个结构化字段。Record 经 `Serve` 参数显式传递（nil = 关闭，零开销）。

### 9.4 统计分析工具 `vmr report` / `vmr story`

审计 JSONL 的离线消费方——聚合报表（`vmr report`）与 Agent 任务叙事重建（`vmr story`）——完整设计见姊妹文档
`docs/VirtualModelRouter_Design_v4_Analytics.md`（Part 2）。两者与本文档描述的路由核心只通过审计日志格式耦合（本节 §9.2 的 Record 结构是它们唯一的输入契约），不共享任何路由期状态，也不反向影响路由决策——`internal/report`/`internal/story`/`internal/ctxgraph`/`internal/chatmsg` 均不出现在 `internal/router`/`internal/server` 的依赖图里（`internal/archtest` 强制这条边界）。

### 9.5 历史文件压缩与保留（`internal/audit/housekeep.go`）

Agent 场景每轮请求都重发完整对话历史，单日审计文件可达 1~2GB，且体积主因是**跨行**冗余（相邻甚至相隔很远的记录之间大段重复），不是单行内部冗余。据此只压缩**已经轮转完毕、不再写入**的历史文件，且用能看到跨行重复的算法：

* **触发时机**：复用 `Logger.Write` 已有的"日期变化即轮转"判断（无新增定时器/轮询）——检测到 `date != l.date` 时，除了切到新文件，额外对目录做一次 housekeeping 扫描；`New()` 也在启动时扫一次，补上进程重启期间错过的轮转。两处都异步执行（独立 goroutine，`atomic.Bool` 防止扫描重叠），绝不阻塞审计写入或请求服务。
* **压缩**：zstd（`github.com/klauspost/compress/zstd`，纯 Go、无 cgo；库默认压缩级别，未手工调参）。选它是因为 stdlib 的 `compress/gzip` 只有 32KB 滑动窗口，看不到相隔几十万到百万字节的跨行重复，实测压缩比被死死摁在 ~3.3×；zstd 默认窗口是 MB 级别，天然覆盖这种重复模式，实测压缩比 20~75×。写入临时文件（`.zst.tmp`）→ 校验 → `rename` 落地 → 确认落地后才删除原文件，中途崩溃不会丢数据也不会留半截 `.zst`；重启后遇到"明文+`.zst` 同时存在"（rename 后、删除原文件前崩溃）视为续跑，直接补删原文件，不重新压缩。
* **保留**：配置项 `audit_retention_days`（缺省 **0 = 永久保留，不清理**）。默认关闭是刻意的产品判断——审计日志是 `vmr report` 成本核算的唯一数据源，静默按天数删除对没读文档的用户是数据丢失风险，需要显式设置天数才启用。
* **零全盘扫描**：审计文件名自带日期（`vmr-audit-YYYY-MM-DD.jsonl[.zst]`），压缩/保留判定只需一次 `os.ReadDir`（目录内条目数 = 保留的天数，不是磁盘总量）+ 文件名正则取日期比较，不解析文件内容、不 `stat` 全盘。同一次目录扫描里，一个文件如果"既该压缩又已过保留期"，本轮就直接压缩后立即删除，不用等到下一天的扫描才清理。
* **`vmr report` 的配套**：`Build` 按扩展名分支，`.zst` 输入透明解压后再喂给同一套 JSONL 解析——历史压缩文件与当天明文文件可以混在同一次 glob 里（`vmr report 'vmr-audit-*.jsonl*'`），调用方不需要关心哪个是哪个。

---

## 10. 配置参考

```yaml
listen: 127.0.0.1:8800        # 缺省 127.0.0.1:8800
api_keys:                     # 可选：vmr 自身鉴权（Bearer 或 x-api-key）；字符串数组，非具名映射；每把 ≥16 字符
  - ${VMR_KEY_ALICE}          #   （校验强制，否则 KeyTag 的末 8 位窗口可能就是整把密钥）。命中的那把给请求打上
  - ${VMR_KEY_OPENCLAW}       #   client_key_tag = audit.KeyTag(该 key)，供 vmr report 按调用方分组导出。
                              #   旧的单把 api_key 已移除，配置里仍写着会被当作未知字段拒绝加载
max_attempts: 0               # 上游尝试数上限；缺省 0 = 不限，试遍所有可用候选（正数用于约束尾延迟）
probe_timeout: 15s            # 半开端点一次后台恢复探测的时间上限（缺省 15s，远小于 response_header 的 120s——探测要的是快且便宜，等不到就是等不到）；只做全局开关，不支持按模型覆盖（刻意的 YAGNI）
max_request_body_mb: 8        # 入站请求体大小上限（缺省 8，超限 413）；仅为稳定性考虑，与审计记录无关——vmr 接受的请求，审计里永远是完整的那一份
max_concurrency: 8            # 全局并发上限（缺省 0 = 不限）
https_proxy: http://...       # 可选：https 型 base_url 的代理服务器地址。这是 vmr 用代理的唯一途径——代理环境变量被有意忽略；想引用它就显式写 ${HTTPS_PROXY}。只声明代理在哪，不代表默认开启，见下方 providers[].proxy
http_proxy: http://...        # 可选：http 型 base_url 同理（如局域网 llama.cpp）；按 base_url 的 scheme 选用
log_dir: ~/.vmr/logs          # 可选：审计日志目录。显式值原样使用（~/ 展开）；缺省 ~/.vmr/logs（三层默认，见「请求图片自动降采样」）。改动需重启生效
image_cache_dir: ~/.vmr/image_cache  # 可选：降采样缓存目录。规则同上，缺省 ~/.vmr/image_cache；随热重载即时生效
image_downscale: 0            # 请求内联图片长边像素上限；缺省 0 = 关闭；模型自身的 image_downscale（下方）优先于这个全局值
image_cache_ttl_days: 7       # 降采样结果缓存的失效期；缺省/非正数 = 7 天
audit_retention_days: 0       # 审计文件保留天数；缺省 0 = 永久保留，不清理；历史文件压缩为 .zst 与此项无关，无条件在轮转时发生
sticky_ttl: 10m                # Sticky Model 粘性偏好的全局默认有效期；缺省/非正数 = 10 分钟，硬上限 24 小时（超过拒绝加载）。按端点可覆盖（下方 endpoints.sticky_ttl）
timeouts:
  connect: 10s                # 连接上游（缺省 10s）
  response_header: 120s       # 上游首字节（缺省 120s）
  stream_idle: 120s           # 上游 body 静默看门狗（缺省 120s）：SSE 流、非流式响应体、错误响应体的读取全部受此约束——响应头之后的一切上游读取都有超时兜底，任何上游停滞都不能把请求永久卡住

providers:                       # "我有什么"——扁平列表，一个账号一条
  - name: <name>
    base_url: {openai: https://..., anthropic: https://...}  # 按协议分 key 的 map，至少声明一个；
                                 # 必须自带版本号；openai 型拼 /chat/completions，anthropic 型拼 /messages
    api_key: ${ENV_VAR}        # 支持 ${VAR} 展开；未设置的变量展开为空串；两个协议面共享同一把 key
    proxy: false               # 可选布尔开关，缺省 false：true = 该 provider 走 http(s)_proxy（海外厂商的
                               # 推荐写法）；false/缺省 = 直连（国内厂商的典型写法，也是唯一的缺省值——
                               # 没有全局默认可继承，每个 provider 独立、显式决定）。没有环境变量回退。
                               # true 但没配对应 scheme 的代理地址是校验错误（拒绝加载）。yaml.v3 是
                               # YAML 1.2，必须写 true/false（on/off 不是 bool）

models:                          # "对外叫什么、按什么顺序用"——按虚拟模型名分组，协议信息挂在每条 endpoint-group 上
  <virtual-model-name>:
    strategy: [priority]       # 缺省 [priority]
    image_downscale: 512       # 可选；覆盖全局 image_downscale，只对这一个虚拟模型生效；写 0 表示对这个模型强制关闭，即使全局开着
    sticky: true                # 可选；Sticky Model 开关，*bool，缺省（不写）视为 true；只有确实不需要
                                 # 会话亲和的单次调用场景才需要显式写 false
    capabilities: [text, tools]   # 可选；这个虚拟模型下所有端点共享的能力基线，缺省 = 无基线（不限制）
    max_context_tokens: 128000    # 可选；同上，端点共享的上下文窗口基线，缺省/0 = 无基线（不限制）
    endpoints:
      - protocol: openai        # openai | anthropic | openai-responses | 未来任何已注册的 adapter 名——引用的
                                 # provider 必须在这个协议下声明了 base_url；同一虚拟模型名可以再挂多条不同
                                 # protocol 的 entry，各入口各自独立可达（见「协议模型」§3）
        provider: <name>       # 必须引用 providers 列表里已定义的账号名
        models: [<上游真实模型名>, ...]   # 一个或多个；每个名字展开成独立的、各自健康跟踪的端点，
                                          # 按列表顺序参与 priority 排序，共享本条 entry 的其余字段
        priority: 1            # 可选；缺省 0，同优先级按文件顺序（稳定排序）——多数场景不必写这个字段，直接按想要的顺序排列 endpoints 即可
        capabilities: [image]        # 可选；叠加在虚拟模型的 capabilities 基线之上（取并集），不是替换；
                                      # 缺省 = 不额外叠加；生效集合一旦非空就是穷尽式的
        max_context_tokens: 1000000  # 可选；覆盖虚拟模型的 max_context_tokens 基线（单个数值，覆盖不叠加）；
                                      # 缺省/0 = 原样继承基线
        role_map:               # 可选；这条 entry 拒收的 role → 改写成什么，缺省不改写
          developer: system     # 例：DashScope 拒收 OpenAI o1/o3 系列的 developer role
        sticky_ttl: 2h          # 可选；覆盖全局 sticky_ttl，只对这一个端点生效——挂在端点而不是
                                 # 虚拟模型上，因为 prompt cache 寿命是上游 provider 的属性；同样受 24 小时硬上限约束
```

**扁平 provider 列表 + 按协议分 key 的 base_url，而不是两层 map**：早期版本按协议把 provider/model 分成两层 map（`providers.<protocol>.<name>`），协议就是配置里的位置；现在 provider 是扁平列表，`name` 是显式字段，`base_url` 改成按协议分 key 的 map，同一账号的两个协议面（如 MiniMax）合并成一条 provider 条目而不是重复两份。协议改为 endpoint-group 自带的 `protocol:` 字段：一个 model 的某条 endpoint-group 引用的 provider，必须在 `protocol:` 对应的 key 下声明了 base_url——跨协议引用不是配置写不出来，而是加载期"provider 没有这个协议的 base_url"错误。这带来的好处不变：同一账号一条 provider 条目自然覆盖两个协议面，不需要重复声明或后缀区分；同一个 virtual model 名下可以同时放一条 `protocol: openai` 和一条 `protocol: anthropic` 的 entry，两个入口各自独立可达（`router.BuildSnapshot` 按 entry 的 `protocol` 拆成两条独立路由，见 `internal/router/snapshot.go`）。`Endpoint` 的 `HealthKey()`/`Name()`（进而健康 key、`X-VMR-Endpoint` 响应头、实时日志）仍然是三段式 `<protocol>/<provider>/<model>`——即便现在只有一条 provider 条目，两个协议面用的是同一个 provider 名、同一个 API Key，两段式的 `provider/model` 键依然会把它们的健康状态错认成同一个端点。**审计日志的 `attempts[].endpoint` 是独立拼接的展示字符串，不复用 `Name()`**：同样三段但用 `:` 分隔（`<protocol>:<provider>:<model>`），因为审计侧另有三个结构化字段 `protocol`/`provider`/`model`——`endpoint` 纯粹是给人读的标签，程序需要这三段时应该直接读结构化字段，不解析任何分隔符；两处的三段式含义相同，只是各自独立维护，分隔符不必强求一致。**兼容旧格式日志**：一部分历史留存的审计文件的 attempt 只有 `endpoint`（`/` 分隔），没有 `protocol`/`provider`/`model` 三个结构化字段——`internal/report` 的 `attemptUpstream()` 在三个字段皆空时按 `SplitN(endpoint, "/", 3)` 拆出三段（只切前两个分隔符，不切整串：model 段本身可能带 `/`，例如 OpenRouter 的 `z-ai/glm-5.2`，`Split` 会把它切成 4 段而不是 3 段，误判为"格式不认识"，`SplitN` 才能正确保留），使 `realModel()`、详单索引的 `VM/API` 列在混用新旧格式日志时都不会退化成 `none`/`-`。

**Priority 是可选的逃生舱，不是必填项**：`strategy.Sort` 用稳定排序，同优先级（含全员缺省的 0）保留配置文件顺序。日常写法是完全不写 `priority`，靠 endpoints 的列表顺序表达优先级；只有需要表达"这几个是同一档位、组内再按 weight/latency 等维度决胜"这类分层语义时才需要显式数字。`vmr check` 按实际生效顺序打印 `1. 2. 3.`（跑一遍 `strategy.Sort`），而不是回显原始 priority 数字，所以不管你写没写这个字段，看到的都是真实的尝试顺序。

**校验规则**：**YAML 严格解析**（`KnownFields`，未知/拼错的配置键直接拒绝加载——`max_concurency` 这类 typo 绝不静默忽略）、已移除的单把 `api_key` 出现即被当作未知字段拒绝加载（不再有专门的迁移提示——早已全量迁移到 `api_keys`，没有外部用户需要照顾这条兼容路径）、已移除的 `probe_mode`（半开端点恢复探测不再有可选的被动模式，永远是后台探测）同样出现即被当作未知字段拒绝加载、listen 可解析、providers/models 非空、每个 provider 的 `name` 非空且在列表内唯一、`base_url` 至少声明一个协议、`base_url` 的每个 key 已注册为 adapter 且值是带 scheme+host 的合法 URL、endpoint-group 的 `protocol` 已注册为 adapter、引用的 provider 存在（按 `name` 在扁平列表里查找）且在该 `protocol` 下声明了 base_url（这是旧版"provider 引用存在于同协议分组内"校验的新等价物）、`models` 列表至少一项且每项非空、`http_proxy`/`https_proxy` 非空时必须是带 scheme+host 的合法 URL、provider `proxy: true` 但没配对应 scheme 的代理 = 校验错误（配置自身就能陈述的矛盾，拒绝加载而不是运行时警告；`proxy` 没有全局默认可继承，每个 provider 独立决定）、`max_context_tokens` 必须 ≥0、`sticky_ttl`（全局与端点级）必须为正且不超过 `internal/sticky.BackstopTTL`（24 小时，见「调度与健康」）、`api_keys` 每一项 ≥16 字符（`minAPIKeyLen`，防止 `audit.KeyTag` 的末 8 位窗口就是整把密钥）；`image_downscale`（全局与模型级）、`audit_retention_days` 负数均在加载期钳制为 0（拒绝配置不如静默纠正——这不是能表达"错误意图"的字段）；`image_cache_ttl_days` 非正数钳制为默认值 7，而不是 0（图片缓存没有 `audit_retention_days` 那种"0=永久保留"的产品含义）。模型级 `image_downscale` 在解析层是 `*int`：省略该字段与显式写 `0` 在校验后仍然是两种不同的状态（前者继承全局，后者强制关闭），这是唯一一个"缺省值"和"显式 0"语义不同的字段。

**`vmr check` 与 `Config.Check`**：validate() 之外还有一层不影响加载、但值得在真正联网之前拦下的"一致性检查"（`internal/config/check.go` 的 `Config.Check() []Issue`）——provider 的 `api_key` 为空、`probe_timeout` 没有明显小于 `response_header`（违反后台探测"绝不占用和真实请求一样长的预算"这条设计前提，见上文 `DefaultProbeTimeout`）、同一个虚拟模型里出现完全重复的 `protocol/provider/model` 端点。这些问题不是 validate() 那种"配置自身就能陈述的矛盾"（校验期硬拒绝），而是"能跑但大概率不是你想要的"，所以拆成单独一层：`vmr check` 把每一条渲染成对应字段后面的 ⚠️，末尾再汇总成 `=== Failed ===` 列表（配合每个字段固定宽度对齐、每个 provider 的 `api_key` 脱敏展示、每个虚拟模型基线 capabilities/max_context_tokens 与每个端点自己叠加/覆盖值的分层展示）；`vmr diagnose` 复用同一个 `Config.Check`，一旦有结果就跳过 Phase 2（Environment）/Phase 3（Connectivity）这两个真正拨网络的阶段——配置还没理顺就没必要浪费时间等连接超时。

CLI：`vmr start -c <cfg> [-audit=false]`、`vmr check -c <cfg>`（校验 + `Config.Check` 一致性扫描 + 按生效顺序打印路由表，含每个模型的 image_downscale/sticky 覆盖标记、每个端点的 capabilities/max_context_tokens/sticky_ttl、每个 provider 的生效代理）、`vmr status [-c <cfg>]`（渲染健康与并发）、`vmr report [-o dir] <glob>...`（见「审计日志」）、`vmr check [-c <cfg>] {log|cache}`（打印生效的 `log_dir`/`image_cache_dir`，`vmr.sh` 内部用它定位 server log 落点）、`vmr version`（构建标识，见 §4.3 `instance` 块）。环境变量：**只有一类**——配置内 `${VAR}` 展开引用的任意变量（API Key、可选的 `${HTTPS_PROXY}`、可选的目录……都走这一条）。除此之外 vmr 不读任何环境变量：目录（`log_dir`/`image_cache_dir`）与代理环境变量（`HTTPS_PROXY` 等）均**有意不作为隐式来源**（见下段）。

**上游代理：显式配置，两级解析，默认关闭**：`http_proxy`/`https_proxy` 只声明代理服务器的 URL——本身不替任何 provider 打开代理。是否真的走代理完全由 provider 自己的 `proxy: true`/`false` 决定（缺省 `false` = 直连，没有全局默认可继承）；只有它是 `true` 时，才按 base_url 的 scheme 选用 `http_proxy`/`https_proxy`。**推荐配置方法**：只给个别确实需要代理的 provider（典型是访问受限的海外厂商）显式写 `proxy: true`，其余不写——单点意图，新增 provider 默认直连、不会意外被牵连进代理。（早期设计里曾有一层全局 `proxy` 默认开关，供"多数 provider 都要代理"的场景整体翻转；实测这个场景从未出现——真实部署里全局开关始终是缺省关闭，只有个别海外 provider 逐个显式 `proxy: true`——于是连同它的三层解析、专属矛盾校验一起去掉，见下方决策表。）**没有环境变量回退**：隐式改变流量走向的旋钮容易被忽略、排障时最难想到——一个只在某次交互式 shell 里临时设置过的 `HTTPS_PROXY`，一旦被 vmr 悄悄读取，就会让接下来启动的所有实例在不知情的情况下把全部上游流量导进代理。vmr 的原则是流量去哪必须在 config.yaml 里读得出来；想引用环境变量就显式写 `https_proxy: ${HTTPS_PROXY}`——`${VAR}` 展开对它一视同仁，vmr.sh 的通用 `${VAR}` 抓取会自然把它带进 service 环境。`proxy: true` 但没配对应 scheme 的代理地址是校验错误——这个矛盾配置自身就能陈述，不需要等到运行时。实现上不做每请求动态判断：`router.Install` 按"生效代理解析结果"分组建 `http.Client`（典型 1~2 个），同组 provider 共享连接池，endpoint 在快照期绑定到组（`Snapshot.clientFor`），请求期零额外开销；config 内的代理值随热重载即时生效。启动摘要与 `vmr check` 逐 provider 打印生效代理（URL 内凭证经 `url.Redacted` 掩码）。

**启动摘要**：`vmr start` 在启动与每次热重载成功后向 stderr 打印生效配置——listen/鉴权开关/各上限/超时、每个 provider 的生效代理（凭证掩码）、每个 virtual model 的端点生效顺序与 key 状态（同 `vmr check` 的口径），控制台即可核对运行实例的真实配置。

**vmr.sh（唯一脚本入口，双模式 + 原生命令透传）**：脚本不认识的子命令原样转发给二进制（`check|diagnose|report|story|replay|version|…`），**刻意不做白名单**——二进制新增子命令无需改脚本，拼错由二进制打自己的 usage。转发只调整路径语义两处：① `cd` 回调用者原目录（`report`/`story` 的 audit glob、`-o`、`-detail` 都该按调用者所站目录解析）；② 未给 `-c` 时补上 checkout 的 config 绝对路径——这是 ① 的后果（二进制的 `-c` 缺省是相对路径）。补 `-c` 的子命令硬编码为实际定义了 `-c` 标志的那几个（`start/check/status/diagnose/replay/report/story`）；漏进列表的新子命令退化为"自己敲 `-c`"，不会拿到错配置。

dev 模式（`start/stop/restart/status/ps/logs`）nohup 后台、人肉监督，无 PID 文件、按二进制绝对路径 `pgrep -f` 匹配，start 前先 `vmr check` 拒绝坏配置。`ps` 列出**本机全部** vmr 实例而不只是本 checkout：`pgrep` 找进程 → `lsof -a -p PID -iTCP -sTCP:LISTEN` 找端口（监听地址只在那个进程的 config 里，命令行上没有）→ `vmr status -addr … -brief` 向实例自己要其余信息。`-a` 是必须的：lsof 的选择条件默认 OR，漏掉会返回别的守护进程的端口。JSON 解析留在二进制里（`-brief` 输出 Tab 分隔一行），不引入 jq 依赖。**刻意不引入 PID/registry 文件**：那会给一个有意无状态的二进制加上进程生命周期状态（陈旧文件、pid 复用、写盘失败），只为省掉一个 lsof 依赖——而 `ps` 是诊断命令，不是热路径。`status` 复用同一条端口发现路径兜底：`-c` 加载失败时改用 lsof 查出的端口走 `-addr`，因为配置坏掉恰是这条命令最该能用的时刻。缺 lsof 或进程不应答时退化成"pid + 命令行 `-c` 参数"并标注原因，不漏掉实例。

service 模式（`service install/uninstall/start/stop/restart/status/logs`）把监督交给 init 系统——macOS 渲染 launchd user agent（`~/Library/LaunchAgents/com.vmr.plist`，KeepAlive+RunAtLoad，stop 走 `bootout` 避免 KeepAlive 复活被杀进程），Linux 渲染 systemd 用户单元（`Restart=always`；系统级部署把 unit 拷到 `/etc/systemd/system` 去掉 `--user` 即可）。模板内嵌于脚本（heredoc 注入绝对路径），不设独立模板目录。**环境是 service 模式的第一大坑**：init 系统不继承 shell 的 export，`install` 自动从当前 shell 抓取 config 引用的 `${VAR}` 生成 `~/.config/vmr/env`（0600，存在则不覆盖；只抓 config 显式引用的变量，不隐式抓代理变量——代理是 config 字段，config 里写了 `${HTTPS_PROXY}` 这条通用抓取自然覆盖），launchd 经 `set -a; . env` 加载（不 `set -a` 则 source 的变量不会进入 exec 后的进程环境），systemd 经 `EnvironmentFile=`。目录（`log_dir`/`image_cache_dir`）是 config 字段、二进制自己读取，脚本不注入任何目录变量——只用 `"$BIN" check -c "$CFG" log` 查出 `log_dir` 来放自己的 server stderr 日志。macOS 第二坑：TCC 禁止 launchd/sh 对外置卷做文件操作（spawn 报 EX_CONFIG / Operation not permitted），但 vmr 进程自身写卷不受限——故 plist 的 WorkingDirectory 指 `$HOME`、服务日志落 `~/Library/Logs/vmr.log`（macOS 惯例），审计照常写 `log_dir`。两模式互斥：service install/start 自动停 dev 进程。均经 macOS 实机全周期验证（install→E2E→kill -9 自愈→stop→start→uninstall）。

---

## 11. 关键决策与取舍

| 决策 | 备选 | 取舍逻辑 |
| --- | --- | --- |
| Canonical 格式 = 协议原生格式，透传不翻译 | 自研中间表示（IR） | IR 意味着永远追各家新字段，是 LiteLLM 复杂度失控的根源；透传对新参数天然前向兼容 |
| 不用 Provider SDK | 官方 SDK | 路由只需改 URL/Key/model 字段；SDK 带来二进制膨胀与版本纠缠 |
| 编译期 Adapter 注册（blank import） | 运行时插件（.so/脚本/外部进程） | 满足"插件式扩展、写法统一"，不引入运行时插件的任何成本 |
| 调度 = 过滤+多键排序 | 策略类枚举（Priority/RR/Weighted 各一套） | 组合能力来自排序键叠加；新策略不改主流程 |
| 半开恢复统一用后台探测，不保留"下一个真实请求自己当探针"的被动模式 | 定期轮询式主动探测（不管有没有真实请求都固定间隔探测每个端点）；或保留被动模式作为可选降级 | 定期轮询无论如何都要花钱、且探测闲置端点毫无意义；后台探测仍然完全由真实请求触发（没有独立的后台定时器），只是触发之后交给一个跟真实流量解耦的后台请求去做，而不是让真实请求自己当探针——这样"探针请求多大/多慢，恢复检测就要多久，且期间连累同一端点的其他并发请求"的问题被消除，代价是每次半开恢复多花一次小额探测请求；被动模式从未在实际部署中启用过，保留它只是徒增一个配置维度和 `router.go` 主循环里的一个分支，予以移除 |
| 错误分类含 body 嗅探 | 严格按 HTTP status 映射 | 实测各家 status 习惯不一（400 当 404 用等）；漏判 = 永不 failover，误判只是一次无害切换 |
| providers/models 按协议分两层 map，协议即配置位置 | 扁平 map + provider.type / model.protocol 显式字段 | 显式字段要么冗余（与 endpoint 实际协议重复）要么矛盾（写错/漏改）；把协议变成 map 的外层 key 之后，"一个 model 混用两种协议" 连语法都写不出来，不是运行时校验能不能查到，是从设计上不给它写的机会 |
| 全败透传最后上游错误 | 合成统一 502 | 保留客户端 SDK 可解析的厂商错误结构；聚合信息在日志里 |
| 健康状态跨热重载保留 | 重载清零 | 清零会把冷却中的端点放出来重打；carry-over 仅十几行 |
| 并发闸：全局、无等待上限 | 每端点限流 / 排队超时 | 全局闸覆盖"保护本机与总用量"诉求且实现极简；客户端自有超时 |
| failover 默认穷尽全部候选 | 固定尝试上限（旧默认 3） | 配了兜底端点就该兜到底，固定上限会让后位端点永远轮不到；尾延迟由可选 max_attempts 与各超时约束 |
| 内容合规错误：切换但零惩罚（ErrContent） | 当普通 4xx 处理 | 该错误按请求不按端点：不切换会中断长程任务，惩罚健康会让一条敏感请求打掉整个端点；靠 403/451 + 中英文词表嗅探识别 |
| 配额窗口与余额耗尽同罪同罚 | 按窗口设 5h 级长冷却 | 厂商信号无法可靠区分两者；封顶 1h 的探针成本 ≤1 失败请求/小时/端点，恢复及时性优先 |
| 审计双层结构、成功 body 去重 | 每层完整存两份 | 透传恒等，重复存储只膨胀文件；失败 body 各自保留因为各不相同 |
| 审计凭证掩码（留末 4 位） | 完整记录 header | 密钥落盘外泄风险 > 取证价值；末 4 位足以区分 Key |
| 无中心 IR、router 只做循环 | —— | router 主流程（`router.go`，Serve/tryOne/handleErrorResponse/forwardSuccess）约 530 行，响应归一化器拆成通用状态机（`response.go`，约 500 行）+ MiniMax quirk 专属逻辑（`responsefix.go`，约 185 行，Phase 2 item 2.3 拆出）；并发闸/快照构建安装/upstream transport/实时日志格式化已按职责拆到同包的 `limiter.go`/`snapshot.go`/`transport.go`/`logfmt.go`（见 §4.2），不算进这条门槛——门槛盯的是 failover 主流程本身的复杂度，不是同包文件总行数。若 `router.go` 显著变大，说明抽象错了；`internal/archtest` 的 `TestArchitecture_CoreFileSizes` 把这条阈值变成了可执行检查（2026-07-26 起，此前只是本表里的一句话，`router.go` 曾在无人察觉的情况下长到 948 行才被发现——归一化器的增长对应的是实证 quirk 清单，另算，暂未纳入同一检查） |
| 图片降采样跳过判定用真实像素尺寸（`DecodeConfig`） | 按字节数估算换算像素 | 压缩率随内容剧烈波动，字节数与像素尺寸不是稳定映射；读文件头一样便宜且没有换算误差 |
| 图片降采样直接实现为函数调用 | 复用/预建请求预处理插件框架（见「路线图」） | 插件框架的词库形态、按 Provider 差异化等问题还未定型，图片降采样是具体确定的处理，不该为一个未来设计买单 |
| 动图/超限声明尺寸一律 fail-open 跳过 | 尝试部分处理或报错 | 动图缩放会破坏语义，畸形声明尺寸可能是解压炸弹；跳过的代价只是错过一次可选优化，处理的代价可能是内存暴涨或输出错误 |
| 模型级 `image_downscale` 用 `*int` 区分"未设置"与"显式 0" | 用 `int`，0 统一当"关闭/继承" | 需求明确要求"模型自己没设置就跟随全局"和"模型显式设为 0"是两种不同状态；`int` 存不下这个区分，只有指针能让"没写"在解析后仍可判定 |
| 降采样缓存 key 含 `maxPx` | 只按源图片哈希建 key | 同一张源图对不同虚拟模型可能配了不同的降采样目标；只按图片哈希会让后写入的结果覆盖或误命中前一个模型的缓存，返回错误尺寸的图片 |
| 降采样缓存只做按 mtime 的 TTL，不设容量上限 | TTL + 容量双重限制 | 类比审计 retention 的取舍：先上最简单、可预测的单一机制；图片缓存的体积由源图片种类 × maxPx 种类 × TTL 窗口共同界定，实践中量级有限，真出现磁盘问题再加容量上限，不为未发生的问题预先加复杂度 |
| 降采样缓存目录/失效期通过显式参数传入 `imgprep.Downscale`，不用包级可变状态 | 仿照 `audit` 包用 Set* 全局单例 | `Downscale` 每请求调用一次，调用方（`server.chatHandler`）本来就持有解析好的配置快照；显式传参没有额外成本，还让测试能用 `t.TempDir()` 互相隔离，不用担心跨测试的全局状态污染。唯一必要的包级状态是"缓存目录今天是否已经扫过"的节流簿记，与配置无关，纯粹是防抖动 |
| 默认目录公式（~/.vmr → temp → cwd）单点实现于 `internal/rundir`，`vmr.sh` 靠 `vmr check -c <cfg> {log\|cache}` 查询生效值，不在 bash 里重写一份 | bash 自己复刻同一套判断逻辑 | 两份独立实现迟早会跑偏——公式只写一遍、bash 只负责问答，结构上排除了跑偏的可能 |
| `log_dir`/`image_cache_dir` 显式值原样使用，不追加子目录（开头 `~/` 展开）；未设置才落到 `~/.vmr/logs`/`~/.vmr/image_cache` | 无论是否设置都统一追加子目录 | 用户显式设置这个字段，语义就是"这是我要的目录"，再悄悄拼一层子目录会让人诧异；子目录命名空间只在"我们自己选的默认值"这个场景下才有意义 |
| 目录是 config 字段 `log_dir`/`image_cache_dir`，没有对应的环境变量 | 环境变量（或 env 覆盖 config 的双通道） | 与代理同一判断：vmr 往哪写数据必须在 config.yaml 里读得出来，隐式环境状态是排障时最难想到的旋钮；service 模式的 env 文件只需要 config 显式引用的 `${VAR}` 一条通道，不需要额外注入目录变量。代价：`vmr check log`/`cache` 依赖 config（须带 `-c`）；`log_dir` 热重载改不动（audit logger 启动时打开一次，重载打"需重启"提示），`image_cache_dir` 照常热生效 |
| 默认目录用持久的 `~/.vmr/`，不用系统临时目录 | 系统临时目录 | macOS 会定期清理约 3 天未访问的临时目录条目；"审计默认永久保留"与"默认目录会被 OS 清理"自相矛盾。图片缓存同理——它的价值就在跨天的字节级复用（上游 prompt cache 按字节匹配） |
| Retry-After 冷却封顶 1h | 无条件信任上游值 | Retry-After 是上游可控输入；封顶与 Auth/Endpoint 的 longCap 一致，最坏情况每小时一次失败探针，恢复及时性优先 |
| Endpoint 键（HealthKey/Name）加协议前缀（`protocol/provider/model`） | 保持两段式 `provider/model` | provider 名允许跨协议复用之后，同名同 Key 同上游模型串会在两段式键下撞车，把两个真实不同的端点误判成同一个健康状态实体；三段式从根上消除这个碰撞面，代价是 `X-VMR-Endpoint` 的格式多一段，是人读字符串，没有内部逻辑解析它（审计 `attempts[].endpoint` 是独立拼接的 `:` 分隔三段式，不共用这个方法） |
| Endpoint priority 字段保留但可选，鼓励省略、靠列表顺序 | 删掉 priority，强制纯列表顺序 | 稳定排序下全员缺省 priority=0 就是列表顺序，日常写法已经不需要这个字段；但删掉它会丢失"这几个是同一档位，组内再按 weight/latency 决胜"这类分层表达能力，为未来的排序维度组合保留逃生舱 |
| 请求侧 Header 默认透传 + 小型黑名单 | 严格白名单 | LLM SDK 发的 header 集合已知且无危险（不会发 Cookie / X-Forwarded-For），全杀掉反而丢 User-Agent / X-Stainless-* / Traceparent 这些上游做 cache 路由决策需要的元数据；白名单没能区分「协议实现内部白名单」与「代理透传黑名单」这两种不同职责。blocklist 只剥真正会出问题的几项（凭证、IP 欺骗、Go Transport 管理的几个），其余透传 |
| 响应侧归一化：双模式——事件级透传缺省，确认命中思考形态才缓冲 | 全量缓冲单遍正则 / 字节级状态机 | 字节级状态机的复杂度病灶在切分粒度：carry 装不下超长思考块、状态半途丢字节、重入吐残留，这类 corner case 在字节粒度下几乎不可避免；全量缓冲单遍正则正确但假流式——TTFB=完整生成时长，逼近甚至触发客户端 SDK 的超时预算，且让不需要修复的 provider 也一并失去流式。以完整 SSE 事件为处理单位两头都占：事件内 JSON 完整、正则无跨界；think 缓冲在 closer 后恢复流式；失手时退化为直连行为，永不更差 |
| 响应头默认透传 + hop-by-hop 黑名单 | 只转发 Content-Type | 与请求侧同一逻辑：白名单会丢 `Retry-After`（客户端 SDK 自身退避失效）、`x-ratelimit-*`、request id（找厂商排障的唯一凭据）。错误路径同样透传，全败时最后一次上游错误连头带体原样返回 |
| [DONE] 仅 openai 协议且上游缺失时补 | 无条件追加 | 无条件追加会对已发 [DONE] 的上游（DeepSeek/OpenRouter）产生双哨兵、对 Anthropic 流注入协议外内容；条件化后与直连字节一致 |
| 归一化痕迹记入审计 `attempts[].norm` | 不记录 | 成功尝试不存 body 的约定建立在"透传恒等"上，归一化打破了恒等；norm 列表让"上游发的和客户端收的差在哪"在日志里自解释，debug 不用猜 |
| Rewrite `"model"` 字段必须做 | 上游 model 名原样转发 | OpenAI JS SDK 假设 `response.model === request.model`，不一致会按 model 做 prompt cache 关联时**静默丢消息**。这是「代理」和「路由」概念被破坏的根——「我发了 agent 收的也必须是 agent」是虚拟模型抽象的根基 |
| Strip `<think>` 标签必须做 | 原样转发 | MiniMax M3 thinking 模式下把推理放在 content 里。如果不剥，思考被持久化进 assistant message，下一轮 prompt 含上轮思考 → 模型陷入自我指涉的反馈循环：模型反复重试不存在的操作，prompt token 数逐轮暴涨，最终撞上下文上限而被截断 |
| Strip "Thinking Process:" 启发式只对 thinking=medium 触发 | 总是触发 | OpenClaw 的 `Reasoning: off` 是 UI 开关，**不影响模型行为**——模型在 thinking=medium 下不写 `<think>` 标签，直接以纯文本 "Thinking Process:" + 编号小节 1-5 + Final Polish 草稿输出思考。**触发守卫：首个 `"content":"` 值以 "Thinking Process:" 字面量开头**——没有这道守卫，任何合法回复只要含有 "Looks good. Pro" 这类短语（比如代码评审说"Looks good. Proceed"）就会被误判成思考形态，前置内容被静默丢弃。启发式看的是 SSE `\n\n` 分隔的 data: line（JSON-escaped 内容里没有真实 `\n\n`），丢弃含 thinking 的中间 line，保留首条（role marker）和末条（含 "Pro" 标记），从 `Pro` / `Proceed` 之后开始截取最终回复；marker 即首行时原地截取不复制，重组时保留末尾空元素以维持 `[DONE]` 前的 SSE 分隔 |
| 审计历史文件压缩用 zstd（整文件、轮转时触发），不做单条记录压缩 | 逐条记录 base64/zip 编码 | 单条记录粒度的压缩（无论 gzip 还是 zip+base64）天花板很低，因为 Agent 场景的冗余主要在跨记录（同一会话每轮重发历史），压缩窗口锁在一条记录内根本看不见；整文件 zstd（默认窗口已是 MB 级）能覆盖这种跨行重复，压缩比高一个数量级。逐条压缩还会打破"合法 JSON 原样嵌入、可直接 jq 查询"的契约，且落在写路径上；整文件压缩挂在轮转边界，只碰不再写入的历史文件，当天文件保持明文可查询 |
| 压缩/保留复用 Logger 已有的按日轮转边界触发，不设独立 ticker/cron | 周期性 timer 扫描 / 依赖外部 logrotate | 审计文件名自带日期，一次 `os.ReadDir` 即可判定压缩与保留对象，不需要周期性触发就能保证"至多晚一天生效"；新增 ticker 是额外的 goroutine 生命周期管理，外部 logrotate 依赖破坏 vmr"单二进制自包含"的定位 |
| `audit_retention_days` 缺省 0（永久保留） | 缺省一个"合理"天数（如 30） | 审计日志是 `vmr report` 成本核算的唯一数据源，非用户主动设置就被静默删除的风险 > 磁盘空间收益；压缩（无条件发生）已经解决了大头的磁盘占用问题，保留期清理是可选的第二层 |
| model 改写用字节 splice，只动顶层 `model` 值 | `map[string]json.RawMessage` 全量 unmarshal + 重新序列化 | 每次 failover attempt 都要重复这个操作——整体 unmarshal 再重新序列化是主路径上最大的单项 CPU 成本，且会改写键序/空白，偏离"直连等价"。splice 单趟免分配扫描 + 三段拼接，客户端原文除 model 值外逐字节保留；扫不动的形态回退到 unmarshal 路径，行为不变 |
| `BuildRequest` 一并返回出站 body；`audit.EncodeBody` 引用不克隆 | router 用 `GetBody()+io.ReadAll` 再读一份；EncodeBody 防御性拷贝 | 改写后的 body 本来就在 adapter 手里，为审计再拷两份纯属浪费（大 body 每 attempt 多两次全量拷贝）。代价是一条所有权契约：交给 EncodeBody 的 slice 此后不得改写——五个调用点（client 请求缓冲、recorder 响应缓冲、attempt 出站 body、上游错误 body、归一化 pre-strip 快照）都是终态字节，契约天然成立 |
| 代理纯显式两级解析：provider 级 `proxy`（**缺省 false，无全局默认可继承**）→ `http(s)_proxy` 的 URL，**无环境变量回退**；按解析结果分组建 Client | `http(s)_proxy` 一配上就对全体 provider 默认生效 / 单一 `ProxyFromEnvironment` / config 优先 + env 回退 / 每请求动态 Proxy 回调 / config 级 `no_proxy` 清单 / 一层可翻转的全局默认开关 | "配了代理 URL 就默认全体生效"这个初版设计混淆了"代理在哪"和"要不要用"两件事：给单个海外 provider 配代理，会不知不觉把所有 provider 都导流进去，直到有人手工给每个国内 provider 补一条 `proxy: false` 才能收住——补漏式配置容易漏，且新增 provider 默认就"继承"了代理。纯环境变量是全有全无：国内直连 + 海外走代理混配时只能靠 `NO_PROXY` 在 vmr 之外绕。env 回退也不采用：隐式旋钮悄悄决定流量走向，最容易被忽略、排障时最难想到——流量去哪必须在 config.yaml 里读得出来，要引用 env 就显式写 `${HTTPS_PROXY}`。provider 级布尔开关粒度恰好（provider ≙ base_url ≙ host），config 级 `no_proxy` 因此多余。全显式的附带收益：`proxy: true` 无代理可跟从这个矛盾变成静态可判的校验错误；解析不再依赖运行环境，热重载语义完整。每请求回调换取不到任何灵活性——解析对 provider 是静态的，快照期分组建 Client（典型 1~2 组，连接池按组共享，请求期零开销）。**2026-08-02 追加简化**：中间还短暂存在过一层"没写就跟随全局 `proxy` 开关（同样缺省 false）"，供"多数 provider 都要代理、少数国内厂商直连"这种反过来的部署场景整体翻转默认值。上线后复核真实配置：从未用过这层——所有 provider 要么显式 `proxy: true`，要么直接不写，全局开关始终缺省关闭。既然文档自己都写"推荐保持关闭，个别 provider 自己开"，这层为小概率场景保留的翻转能力就是从未使用的可选特性，遂删——`Provider.Proxy` 从三态 `*bool` 收窄为普通 `bool`，`Config.Proxy` 字段、它专属的矛盾校验、`vmr check` 里"继承全局但 scheme 不匹配"那条一致性检查一并去掉。真要出现"多数需要代理"的部署，逐个 provider 显式 `proxy: true` 也不过是多写几行 YAML，不构成恢复这层的理由 |
| `vmr diagnose` 对走代理的 provider 跳过直连 DNS/TLS 检查，只测代理本身可达性 | 不论是否配代理，一律先测目标 host 的直连 DNS/TLS | `router.NewUpstreamClient` 对走代理的 provider 从不直连目标 host——直连检查测的是一件真实请求路径上根本不会发生的事，只会把"只能通过代理访问"（项目本身面向的国内网络场景很常见）的健康 provider 误报成故障 |
| `vmr diagnose` Phase 2/3 有界并发（`checkConcurrency=8`），每个检查写自己的预分配槽位、不加锁 | 顺序执行 / 无界并发 | diagnose 恰好是"怀疑某个 provider 有问题"时才会跑的工具，顺序执行下 N 个同时不可达的 provider 会把等待时间线性放大到分钟级——这正是最需要快速给出结论的场景；无界并发在配置规模较大或 provider 端有并发连接限制时无必要地激进，8 是与 `router.go` 连接池 `MaxIdleConnsPerHost` 同量级的保守取值 |
| `vmr replay` 定位记录支持 `-line`/`-ts`/`-detail` 三种互斥方式 | 只保留 `-line`（原始设计） | `-line` 要求用户先数出目标记录在文件里是第几行，这个坐标在 jq/vmr report 等实际排障工作流里根本拿不到，文件按天轮转后也对不上；`-ts` 匹配 `ts` 字段（容忍 `vmr-requests.jsonl` 的毫秒精度与原始审计日志的纳秒精度），`-detail` 直接读 `vmr report` 已生成的 `details/*.json`（一文件一条记录，天然无歧义）——两者都是用户真实拿在手上的定位符；`-line` 保留作为脚本化场景的兜底，不删 |
| `vmr replay --record` 写出的记录字段布局模仿真实流量的约定（`Client.Response` 存全量，`Attempts[0].Response.Body` 仅失败时存） | 无条件把响应体存两份 | 让 replay 产出的记录能被 `vmr report`/`jq`/再次 `vmr replay` 当作普通审计记录正确消费，不需要为"这是 replay 产出的"开一条特殊解析路径；`Client.Request.Body` 存的是回放前的原文（虚拟模型名），不是改写后发给上游的字节——同一约定，读侧不用区分来源 |
| `vmr replay` 重建请求头时，在 `core.FilterClientHeaders` 之外再按 `audit.IsCredentialHeader` 剔除一遍 | 只用 `FilterClientHeaders`（与 chatHandler 共用同一份逻辑） | 两张表故意不同源：`headerBlocklist` 决定"活的请求转发前要不要剔除"，`credentialHeaders` 决定"记审计时要不要打码"，交集不是全集（`Api-Key`/`X-Auth-Token` 只在后者）。replay 的输入是**审计记录里已经打码的值**，不是活的请求——直接套用 `FilterClientHeaders` 会把打码占位符当真实凭据转发给上游 |
| `router.ModelRoute.EffectiveOrder()` / `router.IngressPath` / `audit.OutcomeFor` 从各命令各自实现改为导出共享 | 维持"各自一份、不值得统一"的既有先例 | 这三处不是"恰好长得像"的独立实现，是 `vmr diagnose`/`vmr replay` 新增后同一段路由排序/协议路径/结果判定逻辑第三、四次被复制——多份拷贝下次协议/排序规则变化时会不同步漂移，且提取成本低（纯函数，无状态），故这三处选择统一，其余仍按既有判断维持现状 |
| `writeError`/`writeJSON` 从 `router`/`server` 两包各一份改为 `core.WriteError`/`core.WriteJSON` 导出共享 | 维持原判断（两处各留一份） | 两处是字节级相同的错误信封实现，而不是"恰好长得像"——这是跨层必须一致的客户端可见契约（OpenAI/Anthropic 客户端都按这个形状解析）；`core` 已经是 router/server 共同依赖的底层包（`CanonicalRequest`/`ErrorClass` 都在那），加两个函数不引入新耦合 |
| `router.tryOne` 的 4xx 错误体上限 64KB→128KB（`errBodyCap`），审计副本超限时追加截断标记，转发给客户端的字节不变 | 维持 64KB，或调大但不加标记 | 单纯调大数字没有实测证据支撑（没有观察到过真实截断案例），但双倍的内存/时间成本在 4xx 非热路径上可忽略，且给审计标记本来就该做——一个未来看审计文件的人不该把"body 只有这么长"和"body 被截断了"混淆。转发给客户端的字节必须保持原判断（不能加标记）：那是 byte-faithful 承诺覆盖的路径，标记只允许进审计专用的副本 |
| 鉴权只保留 `api_keys` 列表，移除单把 `api_key`（破坏性变更） | 两者并存 | 单把 catch-all 能做的事列表全都能做（一把 key 的列表就是它），并存的代价是 `authenticate()` 第二条代码路径、配置面第二个概念、文档里"两者关系"一整段解释；对一个 breaking change 成本极低的本地工具，删掉换简单是划算的。迁移靠 config 校验的定向报错（不是 yaml 泛型错误），指明挪进 `api_keys` 与 ≥16 字符要求 |
| 配置 YAML 严格解析（`KnownFields`，未知键拒绝加载） | 宽松解析（未知键静默忽略） | 拼错的键（`max_concurency`）静默失效是配置驱动工具最常见的真实事故：用户以为限流/超时生效了，实际没有。"坏配置拒绝启动"的既有契约本来就该覆盖这种坏法；代价是配置里不能再放自造的注释性键——本来也不该放 |
| think_strip 触发加前缀守卫：首个非空 content/text 值以 `<think>` 开头才认定思考形态 | 任意位置出现 `<think>` 即触发 | 任意位置触发对"正文合法引用 think 标签"（用户问标签格式、代码示例复现它）会静默删掉引用片段——真实的数据损坏向量，且与 Thinking Process 形态的前缀守卫不对称。MiniMax 真实思考输出永远以标记开头，收紧触发条件不丢任何真实修复场景（回归测试锁定两个方向） |
| `vmr replay -stream` 改写出站 body 的顶层 `stream` 字段（复用 splice 扫描器，缺键则补） | 只改 replay 本地簿记，flag 本身不生效 | 上游读的是 body 里的 `stream` 字段，不改字节等于没改。改写走与 model 改写同一条 `topLevelValues` splice 路径，除该字段外逐字节保留；`--record` 产出的记录同步反映覆盖后的请求 |
| `vmr report` 全部产物 0600/目录 0700（与审计文件同权限） | 0644/0755 | details/、索引、报表与 vmr-requests.jsonl 承载与审计 JSONL 完全相同的完整对话正文——源头刻意 0600，派生副本放宽到全局可读是自相矛盾的。多用户机器上这是真实的信息面差异，单用户机器上无感知 |
| 条件路由用新接口 `Condition`（elimination，感知请求），不扩展 `Dimension` | 给 `Dimension.Compare` 加一个 request 参数 | `Dimension` 的现有实现（priority）和未来实现（weight/latency）本来就不需要看请求，硬塞一个参数会强迫每个排序维度都感知请求；`Condition` 语义上是准入不是排序，混进同一个接口是把两种不同的事情绑在一起。两个接口平行存在，`router.Serve` 分两步跑，互不干扰 |
| 上下文长度条件（`WithinContext`）不注册进 `Condition` 接口，单独一个函数 | 也注册成一个普通 Condition | 唯一需要"全体拒绝时不能真的拒绝"这个降级行为的条件，其余（image/tools）都是确定性的，全体拒绝就该直接拒绝——为一个目前只有一个成员的特例改动整个接口的语义不划算，`router.Serve` 里两行代码就能表达清楚这个特例 |
| `sticky_ttl` 挂在 `EndpointGroup`，不是 `VirtualModel` | 挂在虚拟模型层级 | 调研到 prompt cache 寿命四家官方数据横跨 5 分钟到数天 3 个数量级，是上游 provider 的属性，不是虚拟模型的属性；模型级设计会逼着用户把不同缓存寿命的端点拆成不同虚拟模型才能各自配置 TTL，端点级消除了这个别扭 |
| Sticky Model 的会话指纹（`adapter.SessionFingerprint`）不与 `internal/report/session.go` 的离线分组算法共用实现，也不写入审计日志 | 抽一个共享函数，把在线算出的哈希落盘给 `session.go` 读 | 两者风险取舍相反（`session.go` 容忍 system prompt 逐轮漂移，Sticky Model 不能），共用一份实现要么污染其中一方的语义，要么两边都要加分支；`session.go` 的哈希是它本来就要做的整体消息遍历的免费副产品，调用一个为在线场景优化的字节扫描函数换不来速度收益，日志落盘也没有真实消费者 |
| Sticky Model 默认开启（`VirtualModel.Sticky *bool`，`nil` 视为 `true`） | 默认关闭，显式 opt-in | 实测两次 md5（system prompt + 首条消息，通常几 KB 到几十 KB）相对一次真实 LLM 请求往返可以忽略，不是需要用户权衡是否值得开启的成本；agent 多轮会话又是 vmr 的核心场景，默认关闭只会让大多数用户忘记开启而拿不到本该有的收益 |
| `sticky_ttl`（全局与端点级）增加 24 小时硬上限校验，超过直接拒绝加载 | 只在设计文档里承诺"内存淘汰兜底值比任何端点 TTL 都宽松"，不做代码校验 | 承诺没有代码校验就是没有承诺——`internal/sticky.Registry` 的内存淘汰兜底值固定 24 小时，用户配置一个更长的 `sticky_ttl` 会加载成功但静默失效（粘性记录在写入的 TTL 到期前就先被兜底清理删掉），且没有任何错误提示。校验成本是一次数值比较，配置期直接拒绝换来的是运行时零意外 |
| Responses 协议命名用 `openai-responses`，独立协议字符串、独立 `base_url` key，不复用 `openai` | 复用 `protocol: openai` 字符串，靠某种"子模式"字段在 Adapter 内部分叉；或裸用 `"responses"` | `protocol` 字符串在整个架构里等价于"选哪个 Adapter"，给同一个字符串塞两种完全不同的请求体形状/错误词表/流式解析逻辑，会把这条现在成立的单射关系破坏掉，代价只是 provider 配置里多写一行 `base_url`（哪怕 DeepSeek/OpenRouter 的 Responses 端点和 Chat Completions 端点是同一个 host）；裸 `"responses"` 语义上比协议族名更宽泛、也更容易和未来其他厂商的同名概念混淆，`openai-responses` 自解释且和现有 `openai`/`anthropic` 命名同构 |
| Responses 协议第一版归一化器不做任何 quirk 检测，直接短路到真流式 | 给 Responses 的 typed SSE 事件（如 `"delta":"..."`）也加一套 marker 表，复刻 Chat Completions 的"确认命中形态才缓冲"判定 | Responses 协议原生把 reasoning 做成独立 typed Item，从设计上就不会出现 MiniMax 那种"思考混进 content"的怪癖——没有已知怪癖形态可确认，猜测性地加 marker 表是无凭无据的过度设计；`newRespStream` 的协议短路（直接进 `modePassthrough`）与 `!isSSE` 那条已有短路是同一逻辑：没有已知怪癖就不等待判定 |
| `previous_response_id`/`store:true` 不在 vmr 侧拦截，交给上游自然拒绝 | vmr 主动校验并拒绝这两个字段，或做特殊的 failover 保护逻辑 | 调研到 vmr 目前唯二提供 Responses 兼容面的上游（DeepSeek、OpenRouter）都在协议层强制无状态（`store:true`/非空 `previous_response_id` 直接 400），这个问题现状下并不存在；vmr 的"协议内透传、不替客户端做校验"原则决定了不该主动剥离或拦截客户端发送的字段——真正需要处理"有状态续接端点不可跨候选替换"这个第一性原理问题，要等接入一个真支持它的上游（如 OpenAI 官方账号）才有意义，到时候的方向是扩展 Sticky Model 的强度而不是发明新的错误分类规则 |

---

## 12. 性能验证

**必要性判断**：vmr 的真实使用场景是个人/小团队的 agent 流量（每分钟几十个请求、并发几个 agent），远远够不上会暴露 Go HTTP 服务性能问题的量级，全程也没有任何用户反馈或审计发现指向性能问题。在没有具体问题指向的前提下预先搭一整套压测基础设施，是"给还不存在的问题找解法"。但因为可以做得很便宜，还是值得跑一次：单测和 `-race` 覆盖不到 goroutine 泄漏、并发下的锁竞争（health 注册表、audit 写入）这类问题；图片降采样和 thinking 全缓冲这两条已知最贵的路径，此前从未实际量过。**结论落地成一次性/偶尔手动跑的健全性检查，不接 CI，不做成永久维护的测试套件**——跑一次数字都正常就翻篇，只有真发现某条路径异常才值得针对性 profiling。

**工具选型：Vegeta**（`github.com/tsenart/vegeta`）。核心诉求是"配置文件驱动"（不为测个性能再学一门脚本语言）和"能应对 SSE 流式响应"；调研过 k6（功能最全但要学 JS + 自己编译 SSE 扩展）、oha/hey（命令行参数驱动，不支持按请求变化 body）、Artillery（引入 Node.js 依赖）。Vegeta 是 Go 原生、JSON-lines targets 文件、内置延迟百分位统计，与项目技术栈最贴合。**一个关键简化**：不需要压测工具懂 TTFB/流式细节——vmr 自己的审计日志已经把每条请求的 `ttft_ms`/`dur_ms` 记下来。于是分工很清楚：Vegeta 只负责"客户端视角的总延迟/吞吐/成功率"，"是不是因为全缓冲变慢了"这类更细的按场景归因，`loadtest/runner` 自己直接扫一遍这次运行产生的审计 JSONL 现算（`computeServerStats`，`runner/main.go`）；场景区分也顺势用虚拟模型名当标签，mock 上游按 model 名决定要模拟哪种响应形态。唯一需要自己写的是这个 mock 上游（`loadtest/mockupstream`）——没有任何通用压测工具会假装自己是一个 LLM provider，更不用说模拟已知的具体怪癖形态（MiniMax 的 thinking 泄漏文本、SSE 分块节奏）。**`loadtest/runner` 刻意不跑 `vmr report`、不 import `internal/report`**：压测测的是 vmr 的 HTTP 路由/转发层，它的结果不该系于另一个命令（`vmr report`）的渲染管线是否碰巧还能跑通——`internal/report` 的分析管线本身会独立演进（会话分组、成本估算等），压测不需要、也不该被它拖着走。这条边界由 `go list -deps ./loadtest/runner` 里不出现任何 `vmr/internal/*` 包来保证。

**场景矩阵**（`loadtest/config.yaml` 里每个场景一个虚拟模型，覆盖开销特征明显不同的代码路径，不做协议交叉/并发梯度扫描——一次性健全性检查不是要画一条完整性能曲线）：`baseline`（路由开销下限）、`stream_normal`（真流式透传）、`thinking_leak`（已知最差路径——全程缓冲到 EOF）、`think_tag`（`<think>` 标签形态，先缓冲后恢复流式）、`big_response`（大体积非流式响应）、`big_image`/`multi_image`（图片降采样的完整 decode→scale→encode 链路，单图与多图）、`gif`（确认永不缩放的快速跳过路径依然便宜）、`long_history`（长对话历史的 JSON 探测扫描 + model splice + 审计全量写盘开销）、`failover`（health 状态机 + 冷却 + 故障切换循环开销）、`anthropic_baseline`（确认 Anthropic 协议适配器与 openai 协议共享的归一化代码没有额外成本）、`responses_baseline`（确认 openai-responses 协议适配器——`input` 数组扫描、`RewriteInputRoles`、`newRespStream` 的协议短路——同样没有额外成本）。`loadtest/runner`（`go run ./loadtest/runner`）把起 mock 上游、起 vmr、生成 targets、按 `light`/`moderate`/`heavy` 三档递增负载（10/50/150 req/s）依次跑 Vegeta、再现算按场景/按端点的汇总，全部串成一条命令，产物落在项目原有的 `logs/loadtest/`（独立子目录，每次运行前清空）与 `reports/loadtest-report.md`，不与真实数据混放。图片处理场景（`big_image`/`multi_image`/`gif`）单独分组统计客户端视角百分位——它是唯一真正做 decode/scale/encode 的路径，混进其余场景会把"正常请求"的 p95/p99/max 也一起拉高，失真明显。

**`big_image`/`multi_image` 用一批变体图片，不是同一张图反复发送**：`internal/imgprep` 的降采样磁盘缓存按内容哈希 + 目标像素上限做 key（见「请求图片自动降采样」§7.1），如果三档负载全程只用同一张固定图，除了全程第一次，其余请求都会命中缓存——真正的 decode→scale→encode 反而被缓存挡在外面，测不到这两个场景本该测的东西。`loadtest/gentargets` 因此给这两个场景各生成 `cacheBustVariants`（50）张内容不同、但尺寸/复杂度相同的变体，Vegeta 按目标文件的行序循环发送；`gif` 场景虽不需要变体（`imgprep` 从不缩放 GIF，没有缓存可言），但同样重复写 50 行——Vegeta 按目标文件的**行数**而非 scenario 名字均分流量，行数不对齐会让 `gif` 拿不到它该有的 1/3 份额。**`loadtest/config.yaml` 单独声明 `image_cache_dir`（`logs/loadtest` 下的子目录，随 `logs/loadtest` 一起在每次运行前清空）**：这批变体图片是确定性生成的（同样的 seed 每次产出同样的字节），如果和真实部署共用默认的 `~/.vmr/image_cache`，第二次跑测试时上一次生成的"新"变体早就被缓存预热过，测试会在不知不觉间悄悄退回"全程命中缓存"的老问题。

运行方式与如何读数字的完整操作说明见 [`loadtest/README.md`](../loadtest/README.md)，本节只记录设计判断与结论。

**实测结论（测试时间：2026-08-02，独立、清空过的 `image_cache_dir`，新增 `responses_baseline` 后的 12 场景版本）**：三档负载共 4150 个请求（12 个场景 × 3 轮）全部 100% 成功率。

客户端视角（Vegeta，plain/image 分组，单位 ms）：

| 负载档 | 分组 | 速率 | 请求数 | p50 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|
| light | plain | 8/s | 80 | 1.4 | 4.9 | 5.6 | 5.7 |
| light | image | 3/s | 30 | 105.4 | 112.7 | 115.5 | 115.5 |
| moderate | plain | 38/s | 760 | 1.1 | 4.8 | 6.1 | 6.6 |
| moderate | image | 13/s | 260 | 24.6 | 85.4 | 86.6 | 93.9 |
| heavy | plain | 113/s | 2260 | 0.6 | 3.8 | 5.5 | 10.1 |
| heavy | image | 38/s | 760 | 11.4 | 29.9 | 55.0 | 89.8 |

**image 分组这一档比一档"看起来更快"，是变体池大小相对每档样本量的直接结果，不是错觉也不是变快了**：50 个变体里，`light` 档 image 组总共只有 30 个请求（3 个场景各 10 个左右），全部落在变体池范围内，等于这一档**全部**是真·decode→scale→encode，没有一次缓存命中——它测的是"缓存完全没预热"的最坏情形。`moderate`（260 个请求，每场景约 87 个）与 `heavy`（760 个请求，每场景约 253 个）里，头 50 次是真实 miss，之后开始循环、命中缓存，档位越重、真实处理占比越低——`heavy` 档 `big_image`/`multi_image` 各自约 20%（50/253）的请求是真实 miss，其余都是廉价的磁盘读，p50 自然被摊薄；但 p95/p99/max 三档都还留着真实处理的尾巴（`heavy` 档 image 组 max 89.8ms，同量级的真实成本依然可见）。三档测的其实是同一件事在不同缓存命中率下的样子，不是三次独立的性能测量，读数字时不要拿 p50 跨档直接比较"谁更快"（本节数字与代码改动无关时会随机器负载小幅波动，属正常现象，不代表回归）。

服务端视角（`loadtest/runner` 自算——`computeServerStats`，直接扫这次运行自己的审计 JSONL，不经过 `vmr report`——按虚拟模型分桶的 p50/p95 请求耗时，三轮合并，混合了真实处理与缓存命中）：`big_image` 18ms/101ms、`multi_image` 10ms/43ms（三图，含一张过阈值触发降采样的）、`gif` 1ms/2ms（确认永不缩放的快速跳过路径确实便宜，不受变体数量影响），其余九个场景（含新增的 `responses_baseline`）全部在 0-3ms 之间——`responses_baseline` 本身 1ms/1ms，与 `anthropic_baseline`（1ms/1ms）同一量级，确认 openai-responses 协议适配器（`input` 数组扫描、`RewriteInputRoles`、`newRespStream` 的协议短路）没有引入额外开销。**与必要性判断的预期一致：vmr 自己的路由/透传/归一化/协议适配开销可以忽略不计**，`thinking_leak` 场景确认了全缓冲路径相对真流式确实有可观测的 TTFB 代价（这是已知、接受的设计权衡，不是 bug），`failover` 场景确认冷却/切换机制按预期工作：`mock_fail1`/`mock_fail2` 全程各自只被真实尝试了 1 次（首次失败后进入冷却，此后请求全部直接落到 `mock_ok`），端点可用度表里两者的尝试数与成功率也如实反映了这一点。跑完这一轮，性能这条线索关闭——除非未来某条具体路径的数字明显异常，否则不需要再往下细分（更细粒度的 `go test -bench` 微基准继续不做）。

---

## 13. 路线图

### 13.1 请求预处理插件（敏感词过滤，已规划未实现）

目标：请求发往上游前做关键词过滤/替换（外部词库），降低触发厂商内容合规拦截的概率；对 2xx 内嵌的"软拦截"（如 MiniMax `input_sensitive`）也是唯一的**事前**防线——响应侧现在能**事后观测**到这类拦截（`soft_block_detected`，见「Adapter 机制」），但不能阻止它发生，也不自动 failover。

**本轮明确不预留接口**。理由：插件的词库形态、替换策略、是否需要按 Provider 差异化都未定，先挖的接口大概率与真实插件对不上；预留即负债。待插件设计定型后与其一起实现。图片降采样已经证明这个接入点可用，但走的是直接函数调用而非插件注册表——两者不是同一套机制。

届时的架构改动点（缝已经存在，改动是局部的）：

* **接入位置**：`server.chatHandler` 中 body 解析之后、`router.Serve` 之前——此处已持有完整缓冲的 `CanonicalRequest.Raw`，改写后 failover 重放自然用的是改写后内容。若需按 Provider 差异化处理，则下沉到 `router.tryOne` 的 `BuildRequest` 之前（每次尝试各改一次）。
* **注册方式**：沿用编译期注册（`filter.Register` + blank import），与 Adapter/Dimension 同构；配置里按 virtual model 或全局启用。
* **需要一并决策的问题**：审计日志记原文还是改写后（或两层都记）；改写是否影响 `stream` 等路由字段（不应）；词库热加载是否走同一 fsnotify 通道；过滤失败（词库损坏）时 fail-open 还是 fail-close。

### 13.2 其他方向

* **排序维度**：`weight`（加权随机）、`round_robin`、`latency`（滑动窗口实测）。`weight` 已有过一轮方案设计（同 priority 内按权重加权随机，不做 session sticky——那一版的 sticky 概念已被 Sticky Model 取代），本轮未落地；`round_robin`/`latency` 未设计。
* **Endpoint 级限流**：每端点 rpm/并发（内存令牌桶），主动避免 429。
* **直连语法**：`model: "openrouter:gpt-5"` 绕过 Virtual Model 直达指定 Provider（调试用）。
* **模型改写**：Endpoint 级参数覆盖（强制 temperature、注入 OpenRouter `provider` 路由参数等）。
* **报表/叙事增强**：`vmr report`/`vmr story` 自己的路线图见 Part 2。
* **可观测**：`/metrics`（Prometheus 文本格式，手写无依赖）。原设想的 `vmr test <model>`（对每候选发最小请求）已被 `vmr diagnose` 的连通性测试阶段覆盖（见「调试工具」）。
* **更多协议入口**：gemini；embeddings / images 的同构路由。
* **发布**：goreleaser + Homebrew tap；届时 module 名改为完整仓库路径。
* **`vmr replay` 的输出对比**：产出"原始响应 vs 本次回放响应"的结构化 diff 视图（现状：只打印新响应，对比靠用户自己 `diff`/`delta`）。
* **`thinking`/`audio`/`video` 能力条件**：见「调度与健康」——协议形状未确认，暂不注册。
* **price 排序维度**：不是能力条件，等 `weight`/`cost` 排序维度真正要做时按 Dimension 模式加。
* 远期：本地用量统计（可选嵌 SQLite）。

---

## 14. 已识别、暂不落地的清理项

已识别、但判定"动它的收益低于扰动成本"的项。每项都不是 bug，改与不改行为一致；列在这里是为了下次有人盯着它们犹豫时不必重新论证。

| 项 | 现状 | 不动的理由 |
| --- | --- | --- |
| `cmdCheck`/`cmdStart` 与 `vmr diagnose` 曾各自实现"按生效顺序打印路由表" | **排序部分已统一**为 `router.ModelRoute.EffectiveOrder()`（三处调用同一份 `append+strategy.Sort`）；**打印格式仍分别实现**（输出目标 stdout/logger 不同，且 diagnose 要额外标注连通性测试结果） | 统一打印格式需要 writer+格式抽象，比各自 10 来行的格式化代码更复杂；排序逻辑本身（会随协议/策略演进）已经不重复了，剩下的纯格式化差异不值得再抽象 |
| `respStream.Read` 会返回 `(0, nil)`（等待更多字节时） | io.Reader 文档不鼓励该形态 | 唯一消费方是 `copyFlush`（显式处理）；改成阻塞式内部循环会让 idle 看门狗失去以读取为粒度的心跳 |
| 健康注册表中被配置删除的端点条目跨热重载残留 | 每条目几十字节，重启清零 | 有界（≤ 历史配置的端点总数），加清理逻辑需要 diff 新旧快照，复杂度不成比例 |
| 测试里存在三个各自为政的 mock 上游（`upstream`/`probeUpstream`/`stallingUpstream`） | 各 30~50 行，职责不同（脚本化状态 / 探针时序 / 停滞） | 测试代码合并会互相牵连；等真实收敛需求出现再说 |
| `runProbe`（半开端点后台探测）是 fire-and-forget goroutine，不挂在 `vmr start` 优雅关闭的 `srv.Shutdown` drain 之下 | SIGTERM/SIGINT 时正在跑的探测协程不会被主动取消，自己跑到 `probe_timeout` 或拿到响应为止 | 最坏情况只是丢一次探测结果（下次启动从零状态开始，不是数据损坏或死锁）；接上关闭信号需要新增一条 context 传递链路，复杂度不成比例 |
| `audit.Logger.Close` 不等待后台 housekeeping 收尾 | `hkWG.Wait()` 只给测试用 | 压缩 crash-safe（tmp+rename+重启续跑），housekeeping 只碰已轮转的历史文件、与 Close 关闭的当日 fd 零交集；让关停阻塞在一次可能数 GB 的 zstd 上没有收益。Close 后的迟到 Write 由 `closed` 标志拒绝，不会重开文件 |
| `vmr report` 不区分 `vmr replay --record` 产出的记录（`replay_of` 字段）与真实流量 | 指向包含两者的 glob 时，回放记录会被当普通请求计入统计 | 属于用户主动行为——`--record` 默认不写、写了也是独立文件，只有显式把 glob 指向它才会混入；混入本身有时是期望行为（比如想验证回放请求的 token 用量）。真出现"不小心混进日常统计"的抱怨再加过滤 |

---

## 15. 调试工具：`vmr diagnose` / `vmr replay`

两者都不新造路由/协议逻辑：请求怎么建、上游 client 怎么造，跟 `vmr start` 走的是同一段代码（`adapter.BuildRequest`、`router.NewUpstreamClient`），保证"诊断/回放看到的"和"真实流量会发生的"字节级一致——这是二者存在的前提，不是附加特性。

### 15.1 `vmr diagnose`

`vmr check`（既有命令）只做静态校验：YAML 语法、字段完整性、按 `strategy.Sort` 排出的理论尝试顺序（`router.ModelRoute.EffectiveOrder()`，与 `cmdStart` 共用）——不发一个字节出去。`vmr diagnose` 补的是"实际连不连得通"这一层，分四个阶段：

1. **配置校验**：复用 `config.Load` + `router.BuildSnapshot`；失败直接退出，不进入后续阶段。
2. **环境检查**（`envCheck`，每个 provider 一条结果）：DNS 解析 + TLS 握手（仅 https 且未配代理时），或代理可达性（配了代理时）；`api_key` 是否非空。**代理感知是关键**：`cfg.ProxySpecFor` 判定这个 provider 的真实流量是否经过代理——是的话跳过对目标 host 的直连检查（那条路径真实请求从不会走），只测代理本身；否则会把"只能通过代理访问"的健康 provider 系统性误报成故障。DNS 查询用 `(&net.Resolver{}).LookupHost(ctx, host)` 加 5 秒上限，不用不带超时的 `net.LookupHost`——诊断工具本身绝不能因为一次网络黑洞而无限挂起。
3. **连通性测试**（`testEndpoint`，每个去重后的 `(protocol, provider, model)` 三元组一条结果）：用 `adapter.BuildRequest` 拼一个最小请求，要求模型原样回显一份随请求生成的一次性 nonce，按状态码 + 回显结果归类给出可操作的提示（401/403→查 key，404→查 model 拼写，429→限流，5xx→上游故障；200 但 `probe.Echoed` 没在响应体里找到 nonce → 警告而非直接判通过——单纯的 200 状态码证明不了模型真的跑了，一个网关/中转层用缓存或兜底响应假装成功也会是 200，回显校验能把这类"看似健康实则可疑"的端点单独标出来）。**探测请求的 role 按协议区分**：`protocol: anthropic` 的端点用 `internal/probe.Request`（单条 `role: "user"` 消息，两种协议共用的最小形态）；`protocol: openai` 的端点改用 `internal/probe.RoleCompatRequest(model, "developer")`——首条 `role: "developer"`（OpenAI o1/o3 系列引入、部分自称兼容 OpenAI 协议的 provider 实际拒收的那个 role），末条普通 `role: "user"` 带回显 nonce，同样走 `adapter.BuildRequest`/`RoleMap` 那条流水线。**没有独立的第二次请求**：developer role 走不通，等价于这个端点连不通——不为它单开一个阶段或再打一次请求，直接算作这一条 `testEndpoint` 结果的失败，提示里按 `ep.RoleMap` 是否已配置给出对应建议（没配→提示加 `role_map: {developer: system}`；配了但仍失败→提示检查改写目标 role 名）。**两条消息而非一条**：如果只发一条非 `user` 的消息，部分 provider 会因为"消息数组只有一条且不是 user"这个形状问题直接拒收，跟"这个 provider 不认 developer role"是两个不同的失败原因，混在一起会把纯粹的请求形状问题错判成 role 不兼容；两条消息（角色消息 + 用户消息）也正是真实客户端的发送形态。**只读，不写**：不碰 `internal/health`、不写审计日志——诊断是观察者不是参与者，这在架构上是自动成立的（诊断是独立的一次性进程，物理上碰不到一个正在跑的 `vmr start` 进程的内存态，`health.Registry` 从不跨进程共享）。这条判定规则跟运行时的半开端点后台探测共用同一个 `internal/probe.Request`/`Echoed`（运行时探测不区分 role，永远是 `Request` 的单条 `user` 消息——developer-role 探测只在 `vmr diagnose` 这个一次性诊断工具里做），但对"回显没对上"的处理不同——`vmr diagnose` 是给人看的报告，宁可多报一次警告让人自己判断；运行时探测则只要 2xx 就算恢复，回显缺失只记日志不惩罚，避免把偶尔不遵循指令的健康端点误判下线。去重时若同一个 `(protocol, provider, model)` 三元组被多条不同 `role_map` 的 endpoint-group 引用，取第一个出现的——这是没打算特意处理的边界情况。
4. **路由预览**：对每个虚拟模型打印 `EffectiveOrder()` 排出的尝试顺序，用本轮连通性测试的结果标注每个端点。**不查活实例的实时健康状态**——`vmr status` 才是那个职责，二者边界刻意分开：`diagnose` 回答"现在直连会发生什么"，`status` 回答"那个正在跑的 vmr 现在什么状态"。

阶段 2/3 都以 `checkConcurrency=8` 的有界并发执行（每个检查写自己预分配的结果槽位，无锁）。诊断恰好是"怀疑某个 provider 有问题"时才会跑的工具，顺序执行下几个同时不可达的 provider 会把等待时间线性放大到分钟级——精确发生在最需要快速给出结论的场景。

### 15.2 `vmr replay`

从一条审计记录重建请求、重发到指定 provider。核心约束：`audit.Message.Body` 是 `any` 类型（合法 JSON 存成 `json.RawMessage`，反序列化进 `any` 字段会变成 `map[string]interface{}`，原始字节丢失），所以 replay 不能直接 `json.Unmarshal` 进 `audit.Record`——包内有一个私有的 `recordView`，专门用 `json.RawMessage` 接住 `client.request.body`。

**定位要回放哪条记录**，三种互斥方式：

* `-detail <file>`：直接读一个 `vmr report` 生成的 `details/*.json` 文件——这类文件本来就是 `json.MarshalIndent(&rec, ...)` 的单条完整记录，不用扫描、没有行号歧义，也是用户排障时最先拿到手的东西（先跑 `vmr report`，在 `vmr-requests.md` 里定位到失败请求，再回放）。
* `-ts <timestamp>`：按毫秒精度匹配 `ts` 字段，容忍两种精度来源——`vmr-requests.jsonl` 用毫秒精度格式化，原始 `audit.jsonl` 是 `time.Time` 默认的纳秒精度序列化；`time.Parse(time.RFC3339, ...)` 对两种精度都能正确解析（Go 对小数秒位数天然宽容，与 layout 声明的精度无关），双方都 `Truncate(time.Millisecond)` 后比较即可统一。同一毫秒内有多条记录匹配时报错要求改用 `-line`，不做静默猜测。
* `-line N`：原始的、基于行号的兜底方式（默认 0 = 文件最后一条可解析记录），行号在实际排障流程里很难提前知道，保留是因为脚本化场景仍然有用，零维护成本。

**重建请求**：`replayHeaders()` 在 `core.FilterClientHeaders`（与 `chatHandler` 共用同一份 blocklist）之外，额外按 `audit.IsCredentialHeader` 剔除一遍——原因见「审计日志」的约定 3：审计记录里的凭证类 header 存的是打码占位符，`FilterClientHeaders` 的黑名单和 `audit` 的打码列表是两张故意不完全重合的表，直接套用前者会把打码值当真凭据转发给上游。model 字段用记录里的**虚拟名**（不是 `Attempts[*]` 里已经改写过的真实上游名）过 `adapter.BuildRequest`，与真实流量走同一条改写路径。`-stream true|false` 覆盖时会真正改写 body 顶层 `stream` 字段（`adapter.RewriteStream`，与 model 改写共用同一个顶层字段 splice 扫描器；记录的 body 没有该键时走 generic 路径补上）——上游读的是 body 里的字段，只改本地簿记等于没改；`--record` 产出的记录同步反映覆盖后的请求。

**`--record`**：把这次回放的请求/响应也写一条 `audit.Record`，追加到用户指定的独立文件（不写入常规 `log_dir`，不会被 `vmr report` 的常规 glob 意外扫到）。字段布局刻意模仿真实流量的既有约定，而不是简单地把能填的都填满：`Client.Request.Body` 存回放前的原文（虚拟模型名，不是发给上游的改写后字节——那部分在 `Attempts[0].Request.Body`）；`Client.Response` 存完整响应；`Attempts[0].Response.Body` 只在失败（状态码 ≥400）时存，成功时省略——与 `router.tryOne` 的既有约定一致（透传恒等，省略是因为与 `Client.Response.Body` 字节相同）。这样 `--record` 产出的文件可以被 `vmr report`/`jq`/再次 `vmr replay` 当成普通审计记录正确消费，不需要为"这是回放产出的"单开一条解析路径；记录本身带 `replay_of` 字段（"来源文件:行号"或 `-detail` 的文件路径）标注来源，供人工排查。`DurMS`/`Attempts[0].DurMS` 测的是完整响应体传输完成后的总耗时，不是拿到响应头就停表——与 server/router 对这两个字段"总耗时"的定义一致，否则一个响应头快、body 慢的请求会被记成"很快"。
