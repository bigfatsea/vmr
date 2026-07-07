<!-- Ver 2026-07-07 (V2.2 增量计划追加), by Fable 5 -->

# VMR MVP 开发执行计划（兼进度跟踪）

> 依据：`VirtualModelRouter_v2_Fable5.md`（V2.2）。
> 状态：MVP（M0–M7）**全部完成**；V2.2 增量（M8–M11）见文末。实现总结见 `IMPLEMENTATION_REPORT_Fable5.md`。
> 状态图例：`[ ]` 未开始 · `[x]` 完成 · `[~]` 进行中 · `[-]` 取消/移入 Phase 2

## M0 — 工程骨架

- [x] `go mod init vmr`（Go 1.25，依赖仅 `gopkg.in/yaml.v3` + `fsnotify`）
- [x] 目录结构：`cmd/vmr` + `internal/{core,config,adapter,adapter/openai,health,strategy,router,server}`
- [x] `.gitignore`（Go 二进制、`_tmp/`、`.DS_Store`、本地配置）
- [x] `config.example.yaml`（minimax / openrouter / deepseek 三 Provider）

## M1 — core + config

- [x] `core`：`CanonicalRequest`、`ErrorClass`（Client/Auth/RateLimit/Endpoint/Transient）、`Endpoint`（含稳定健康键 `provider/model/key指纹`）
- [x] `config`：YAML 解析、`${ENV}` 展开、默认值（strategy=[priority]、max_attempts=3、body 上限 8MB、超时）
- [x] `config`：校验（listen 合法、provider 引用存在、adapter type 已注册、base_url 可解析、endpoints 非空）
- [x] 单测：展开、校验失败用例、默认值、自定义超时（`Duration` 类型支持 "3s" 写法）

## M2 — adapter + registry

- [x] `adapter`：接口（BuildRequest / TransformBody / ClassifyError）+ `database/sql` 式注册表
- [x] `adapter/openai`：透传型实现——URL 拼接、注入 `Authorization: Bearer`、model 字段改写（`map[string]json.RawMessage` 局部替换）
- [x] `ClassifyError` 分类表：400→Client（嗅探 unknown model→Endpoint）、401/403→Auth、402/404→Endpoint、408→Transient、429→RateLimit（嗅探额度→Endpoint）、5xx→Transient
- [x] 单测：model 改写保留未知字段、分类表 15 用例全覆盖（含 MiniMax 400 与 OpenRouter 402 实测样本）

## M3 — health + strategy

- [x] `health`：注册表（稳定键、跨热重载存活）、被动冷却、指数退避（2s ×2 封顶 5min）、Auth/Endpoint 10min 起（封顶 1h）、Retry-After（秒 + HTTP-date）
- [x] `health`：半开单飞探针（冷却到期只放行一个请求，其余跳过）
- [x] `strategy`：`Dimension` 接口 + `priority` 维度 + 稳定多键排序
- [x] 单测：退避曲线、探针单飞、Retry-After、成功清零、排序稳定性

## M4 — router + server

- [x] `router`：候选生成（健康过滤 + 排序）→ failover 循环（每候选一次、默认最多 3 次）
- [x] 请求体入口缓冲（8MB 上限 → 413）；ErrClient 直接返回；全败→最后上游错误原样返回；无候选→503；`X-VMR-Attempts`
- [x] 流式：2xx 后逐块转发 + Flush + idle 看门狗；首字节后错误只断流并记日志
- [x] `server`：`POST /v1/chat/completions`、`GET /v1/models`（OpenAI list 格式）、`GET /admin/status`（仅 loopback）、可选 Bearer 鉴权、其余 404
- [x] Header 白名单；每请求一行 stderr 日志
- [x] 集成测试（httptest 模拟上游）：500 触发 failover、400 不 failover、429 冷却+恢复、流式透传、全败语义、413、鉴权、/v1/models、/admin/status（含 `-race` 全绿）

## M5 — config 热加载 + CLI

- [x] fsnotify 监听（300ms 防抖，监听目录以兼容编辑器原子替换）+ SIGHUP 兜底；坏配置拒绝并保留旧配置；原子指针交换；健康状态不清零
- [x] CLI：`vmr start -c`、`vmr check -c`、`vmr status [-c]`（stdlib flag）
- [x] 实测：追加模型数秒生效；写坏 YAML 被拒绝且服务不受影响（日志可见 rejected）

## M6 — 真实 Provider 验证（对照 §6 验收标准）

- [x] 验收 1：单二进制 + YAML 启动，curl 直接可用 `coding` / `cheap`（DeepSeek 返回 PONG）
- [x] 验收 2：第一优先配坏 Key（minimax_badkey），请求自动落到 deepseek，`X-VMR-Attempts: 2`，客户端无感知
- [x] 验收 3：401 触发 10min 冷却（日志与 `vmr status` 可见）；429 冷却+到期恢复由集成测试覆盖
- [x] 验收 4：流式逐块转发（MiniMax-M2 SSE 实测）；本地 mock 实测 TTFB 开销中位数 ≈ 0.11ms（直连 0.257ms vs 经代理 0.366ms），远低于 5ms
- [x] 验收 5：热改配置约 1s 生效；坏配置不影响运行实例
- [x] `/v1/models`、`/admin/status`、`vmr check`、`vmr status` 全部实测可用

## M7 — 交付物

- [x] `README.md`
- [x] 实现总结报告 `IMPLEMENTATION_REPORT_Fable5.md`（含决策点与备选方案对比）
- [x] 代码量核对：非测试代码 1339 行（目标 ≤2500）；router 包 327 行

---

# V2.2 增量：Anthropic 协议入口 + 并发限制（设计见文档 §0.6）

## M8 — 协议抽象与 anthropic Adapter

- [x] `adapter`：接口新增 `Protocol() string`；抽出共享默认错误分类表（openai / anthropic 复用），嗅探词表补 "supported"+"model"（DeepSeek Anthropic 口实测措辞）
- [x] `core`：`CanonicalRequest` 新增 `Header`（白名单协议头：`anthropic-version`、`anthropic-beta`）
- [x] `adapter/anthropic`：透传型——`{base_url}/messages`、注入 `x-api-key`、缺省 `anthropic-version: 2023-06-01`、model 改写、529→Transient
- [x] `main.go` blank import anthropic
- [x] 单测：BuildRequest（URL/头/版本缺省与透传/model 改写）、分类表（含 529、DeepSeek 400 措辞）

## M9 — 协议绑定路由 + 并发闸

- [x] `router`：`ModelRoute.Protocol`（由 endpoints 的 Adapter 推断；混协议 → BuildSnapshot 报错）；`Serve` 校验入口协议，不符 → 404 + 指引 message
- [x] `router`：全局信号量 `max_concurrency`（0=不限），挂起等待、ctx 取消即出队；容量不变时跨热重载复用；`in_flight` 计数
- [x] `config`：`max_concurrency` 字段
- [x] `server`：`POST /v1/messages` 入口；鉴权同时接受 Bearer 与 `x-api-key`；并发闸包住两个聊天入口；`/v1/models` 合并形态（object+has_more+type）；`/admin/status` 增加 protocol 与并发指标
- [x] 单测/集成：混协议校验报错、协议不符 404、/v1/messages failover、x-api-key 鉴权、并发闸（limit=1 串行化 + 等待者取消）、models 合并格式

## M10 — 文档与配置

- [x] `config.example.yaml` / `config.yaml`：新增 `minimax_a` / `deepseek_a`（anthropic 口）与 Anthropic 协议模型、`max_concurrency` 示例
- [x] `README.md`：双协议入口、并发限制说明
- [x] 设计文档 §0.6（已完成）；实现报告追加 V2.2 章节与决策记录

## M11 — 端到端真实验证

- [x] `vmr check`：混协议模型报错可复现；正常配置通过
- [x] `/v1/messages` 非流式 + 流式（MiniMax / DeepSeek Anthropic 口实测）
- [x] Anthropic 口坏 Key failover：p1 401 → p2 接管，客户端无感知
- [x] 协议隔离：OpenAI 模型走 `/v1/messages` 被拒；反之亦然
- [x] 并发限制实测：`max_concurrency` 生效，超限请求挂起后依次放行；`/admin/status` 可见 in_flight
- [x] 全套回归：`go test -race ./...` 全绿

---

# M12 — OpenRouter top-weekly 低价模型接入（2026-07-07）

- [x] 抓取 https://openrouter.ai/models?order=top-weekly（top 17），按官方 API 定价交叉匹配 slug
- [x] 筛选：输入与输出单价均 < $5/M → 12 个（排除 Opus 4.7/4.8、Sonnet 4.6/5、GPT-5.5 档）+ poolside 同日孪生 1 个，共 13 个候选
- [x] 全部加入 config.yaml（虚拟模型名 = OpenRouter slug，单端点透传），逐个最小请求实测（"Say OK"，max_tokens 16–512）
- [x] 实测通过 10 个：deepseek-v4-flash / xiaomi-mimo-v2.5 / minimax-m3 / tencent-hy3-preview / z-ai-glm-5.2 / deepseek-v4-pro / stepfun-step-3.7-flash / nvidia-nemotron-3-ultra:free / poolside laguna-xs.2:free + laguna-m.1:free
- [x] Google 三个（gemini-3-flash-preview / 2.5-flash / 2.5-flash-lite）403 "provider ToS"——直连 OpenRouter 复现，确认为账号/上游策略层拦截，非 vmr 问题；已注释保留于配置并附原因

---

# M13 — OpenRouter 双协议接入 + 审计日志（2026-07-07）

## 13a OpenRouter Anthropic 口（纯配置）
- [x] 实测确认：`https://openrouter.ai/api/v1/messages`，x-api-key 与 Bearer 均可（已完成探测）
- [x] config.yaml / config.example.yaml：新增 `openrouter_a` provider；`claude` 增加 OR 兜底端点；新增 `claude-or` 演示模型
- [x] E2E：/v1/messages 经 openrouter_a 真实调用通过

## 13b 审计日志（internal/audit）
- [x] Record 结构：双层 request/response（client 层 + attempts[] 逐次上游尝试），时间戳、时延、headers（凭证掩码）、body（JSON 原样嵌入/非 JSON 字符串，1MiB 截断标记）
- [x] 成功尝试的上游 body 不重复存（与 client.response.body 相同，透传恒等）
- [x] Logger：JSONL 追加、按天分文件 `vmr-audit-YYYY-MM-DD.jsonl`、目录取 `VMR_LOG_DIR` 缺省系统临时目录、写失败不影响请求
- [x] 集成：server 包 ResponseWriter 录制器（保留 Flusher）、router 填充 attempts；覆盖被拒请求（鉴权/413/坏 JSON/协议不符）
- [x] CLI：`vmr start -audit=false` 关闭（默认开启）；启动日志打印审计文件路径
- [x] 单测：轮转、掩码、截断、JSON/非 JSON body；集成测：failover 双 attempt 记录、流式 SSE body、禁用时无文件
- [x] E2E：真实请求后检查 JSONL 记录完整性

## 13c 文档全面重写
- [x] 设计文档：去 changelog 化，重写为当前态完整方案（架构、协议模型、Adapter、调度健康、并发、审计格式 spec、配置参考、关键决策及逻辑、不做清单、路线图）
- [x] README：精简，快速上手 + 运维视角；补双协议 OpenRouter 与审计日志；与设计文档去重
- [x] 回归：`go test -race ./...` 全绿；`vmr check` 通过

---

# M14 — 穷尽式 failover + 全面核查 + 首次 commit（2026-07-07）

- [x] failover 语义：max_attempts 缺省 0 = 不限（每个可用候选各试一次直到成功或耗尽）；正数为可选上限
- [x] 测试：4 端点前 3 个失败 → 第 4 个接住（旧默认下会被截断的场景）；config 默认值断言更新
- [x] 文档同步：设计文档 §4.1/§9/决策表、config.example.yaml 注释
- [x] 全面核查：通读核心代码、回归 go vet + test -race、vmr check 两份配置、E2E 冒烟
- [x] git commit 当前版本

---

# M15 — 统计分析工具 vmr report（2026-07-07）

- [x] internal/report：JSONL 解析（复用 audit.Record）、usage 提取（OpenAI/Anthropic × JSON/SSE 四种形态）、按 日期×模型 与 日期×端点 聚合、p50/p95
- [x] 输出：vmr-report.json（meta+rows+endpoints，format 版本号）+ vmr-report.md（T1+T2 表）
- [x] CLI：`vmr report [-o dir] <file|glob>...`
- [x] 单测：usage 四形态、聚合正确性、百分位、fallback/可用度、坏行容错
- [x] E2E：对本机真实审计日志跑一遍，人工核对输出
- [x] 文档：README 用法章节；设计文档 §8.4（逻辑、维度、输入契约、与日志格式的同步义务）；路线图去掉"审计统计脚本"
- [x] 回归：go vet + test -race 全绿

---

# M16 — 内容合规错误处理 + 插件空间规划（2026-07-07）

- [x] core：新增 ErrContent（切换但不惩罚端点健康）；调研结论：OpenRouter 403=moderation/guardrail、DeepSeek 内容风险=400+文本、MiniMax 1026/1027 与 200+sensitive 标记
- [x] classify：451→content；403 嗅探（moderation/flagged/guardrail）分流 content vs auth；400 系内容嗅探（含中英文关键词）优先于 model 嗅探
- [x] health：ReportNeutral（释放半开探针但不动冷却/计数）；ErrTransient 也尊重 Retry-After（OpenRouter 503 会带）
- [x] router：ErrContent → 继续 failover + ReportNeutral + 日志 class=content
- [x] 测试：分类新用例、ReportNeutral、transient Retry-After、集成（403-flagged 切换成功且端点不进冷却）
- [x] 文档：设计文档 §5/§6.2/§10 决策表；§11 敏感词过滤插件扩展缝规划（本轮不预留接口的理由与未来改动点）；README failover 一句话；配额窗口 vs 余额决策落文档
- [x] 回归 + commit

---

# M17 — 请求图片自动降采样（2026-07-07）

- [x] 新包 `internal/imgprep`：`bytes.Contains` 快速探测无图请求（零解析开销）；命中后用 `adapter.RewriteModel` 同款 `map[string]json.RawMessage` 模式局部改写 OpenAI `image_url`（data URI）/ Anthropic `source.type=base64` 图片块，未知字段字节不动
- [x] 判定用 `image.DecodeConfig` 读真实像素尺寸（而非字节数估算换算）；超限才解码 → `golang.org/x/image/draw` `BiLinear` 等比缩放 → 透明摊平白底 → 统一编码 JPEG(quality 85)
- [x] 安全边界：动图（GIF 多帧）跳过不处理；声明像素数超 64MP 的解压炸弹防护；解析/解码全链路 fail-open（含 panic recover），绝不因这一步的 bug 影响正常请求
- [x] 格式支持：标准库 JPEG/PNG/GIF + `golang.org/x/image` 的 WEBP/BMP（Go 官方扩展库，非第三方野包）
- [x] `config`：新增 `image_downscale`（int，长边像素上限；0/缺省=关闭；负数钳制为 0）
- [x] `server`：接入点在 `chatHandler` 里 `rec.Client.Request.Body` 记录之后、`probe` 解析之前——审计客户端层记原文，上游尝试层自然记降采样后内容，复用既有两层审计语义
- [x] 单测：imgprep 包 16 用例（各格式、阈值上下边界、动图跳过、损坏数据/畸形 JSON fail-open、解压炸弹守卫、两种协议、remote URL 不 fetch）；config 默认值与负数钳制；server 集成测（真实 httptest 上游验证出站图片确实变小/关闭时原样不动）
- [x] 回归：`go vet` + `go test -race ./...` 全绿；`vmr check` 验证新字段
- [x] 文档：设计文档新增 §7（原 §7–§11 順延为 §8–§12，含全部交叉引用与决策表新增三行）；README 新增用法小节并修正 §引用；config.example.yaml 注释
- [x] 真实 E2E：取本地 8 张真实图片（JPG/PNG/WEBP 混合，含 2 张本就 <512px 的对照组）经 vmr 打 MiniMax-M3 真实接口，对比 `image_downscale` 开/关两组的 `usage.prompt_tokens`——6 张超阈值图降幅 36%–87.5%，2 张对照组请求字节与 token 数不变（确认零副作用跳过路径生效）；全程无 failover、无异常
