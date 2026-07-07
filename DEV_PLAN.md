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
