<!-- Ver 2026-07-07 (含 V2.2 增量), by Fable 5 -->

# VMR MVP 实现总结报告

日期：2026-07-07 · 状态：**MVP 完成，五条验收标准全部实测通过；V2.2 增量（Anthropic 入口 + 并发限制）完成，见文末第 7 节**

---

## 1. 过程概览

按 `DEV_PLAN.md` 的 M0–M7 顺序一次走完，无返工：

1. **动工前终审**：对 V2 设计做最后审核，并用真实 API 探测三家 Provider 的错误行为，发现 8 处问题（详见设计文档 §0.5），全部修订合入 `VirtualModelRouter_v2_Fable5.md`。最有价值的实测发现：**MiniMax 对未知模型返回 400 而非 404**——若按原设计"400 一律不切换"，配错模型名的端点将永远无法 failover。
2. **实现**：Go 1.25，8 个包，非测试代码 **1339 行**（目标 ≤2500）；依赖仅 `yaml.v3` + `fsnotify`，其余全标准库。
3. **测试**：30+ 单测断言 + 11 个集成测试场景（httptest 模拟上游），`go test -race ./...` 全绿。
4. **真实验证**：以 MiniMax / OpenRouter / DeepSeek 为默认 Provider 实测（用系统环境变量 `MINIMAX_PLAN_KEY` / `OPENROUTER_API_KEY` / `DEEPSEEK_API_KEY`）。

## 2. 验收结果（对照设计 §6 Phase 1）

| # | 标准 | 结果 |
| --- | --- | --- |
| 1 | 单二进制 + YAML 一条命令启动，OpenAI 客户端直接可用 | ✅ `./vmr start -c config.yaml`，curl 走 `cheap` 返回 PONG |
| 2 | 第一优先 Key 配错，自动落到第二优先，客户端无感知 | ✅ 坏 Key 端点 401 → DeepSeek 接管，HTTP 200，`X-VMR-Attempts: 2` |
| 3 | 上游失败进入冷却（日志可见），到期自动恢复 | ✅ 401 → 10min 冷却，`vmr status` 显示 `COOLDOWN (auth)`；429 冷却+恢复由集成测试覆盖 |
| 4 | 流式逐块转发，代理 TTFB 开销 < 5ms | ✅ MiniMax SSE 实测逐块转发；本地 mock 对比 20 次取中位：直连 0.257ms vs 经代理 0.366ms，**开销 ≈ 0.11ms** |
| 5 | 改配置数秒生效；坏配置不影响运行实例 | ✅ 加模型约 1s 生效；坏 YAML 日志 `rejected, keeping current config` |

附加实测：`/v1/models`、`/admin/status`（仅 loopback）、`vmr check`、`vmr status` 均可用。

## 3. 最终结构

```
cmd/vmr/main.go          CLI: start / check / status（201 行）
internal/core            CanonicalRequest、ErrorClass、Endpoint（65 行）
internal/config          加载、${ENV} 展开、校验、Duration、fsnotify watch（227 行）
internal/adapter         接口 + 注册表（database/sql 模式，63 行）
internal/adapter/openai  透传 Adapter + 错误分类表（95 行）
internal/health          被动冷却、指数退避、半开单飞探针（150 行）
internal/strategy        Dimension 接口 + priority + 稳定多键排序（76 行）
internal/router          快照构建 + failover 循环 + 流转发（327 行）
internal/server          HTTP 入口、鉴权、三个端点（135 行）
```

## 4. 决策记录（备选方案与建议）

按你的要求，列出实现中我拍板的决策、备选方案、对比与建议：

**D1. `ErrQuota` 扩展为 `ErrEndpoint` + 400 body 嗅探**
- 选择：额度耗尽、模型不存在（404 及 MiniMax 式 400）、402 统一归入 `ErrEndpoint`（长冷却+切换）；400 响应嗅探 body 关键词（"unknown model" 等）。
- 备选：严格按 HTTP status 映射（400 一律 ErrClient）。
- 对比：严格映射实现更纯粹、无误判风险，但实测证明会把"端点配置错误"误判为"客户端错误"，直接违背验收标准 2 的精神；嗅探的风险是关键词误命中（概率低，误判后果只是多一次无害切换）。
- 建议：维持现状；Phase 2 若接入更多厂商，把嗅探词表下沉为各 Adapter 自己的知识。

**D2. 健康状态跨热重载保留（稳定键 carry-over）**
- 选择：健康注册表独立于配置快照，键为 `provider/model/key指纹`。
- 备选：重载即清零（实现最简）。
- 对比：清零方案会导致"每次改配置都把冷却中的端点放出来重打一轮 429/401"，而 carry-over 只多十几行代码；副作用是彻底删掉的端点会留下少量僵尸状态（无害，重启消失）。
- 建议：维持现状。

**D3. 半开恢复用"单飞探针"而非"到期即全放行"**
- 选择：冷却到期后只放行一个真实请求当探针，其余请求跳过该端点，探针成败决定恢复或加深退避。
- 备选：到期即对所有请求可见。
- 对比：全放行在高并发下会把一批请求同时砸向可能仍未恢复的端点（惊群）；单飞探针多一个状态位，但把试错成本压到一个请求。
- 建议：维持现状。

**D4. 全部候选失败时原样返回最后一次上游错误**
- 选择：透传最后一次上游错误的 status + body（另加 `X-VMR-Attempts`）。
- 备选：合成统一的 502 + 各端点失败摘要。
- 对比：合成错误信息更全，但破坏 OpenAI 错误格式兼容性（客户端 SDK 按厂商错误结构解析）；透传保证客户端看到真实、可解析的错误。摘要信息已在日志里。
- 建议：维持现状；若排查需求变强，Phase 2 在 `/admin/status` 里加最近失败明细。

**D5. 流式 idle 看门狗进 MVP**
- 选择：实现 `stream_idle`（默认 120s）看门狗，上游静默超时即断流。
- 备选：MVP 不做（设计文档提及但可延后）。
- 对比：不做则挂死的上游连接会让客户端无限等待且占用连接；实现代价约 50 行（单 goroutine + timer）。
- 建议：维持现状。

**D6. Go module 名用短名 `vmr`**
- 选择：`module vmr`。
- 备选：`github.com/<user>/vmr` 完整路径。
- 对比：短名 import 干净、无需预设仓库归属；代价是别人无法 `go get`。这是唯一一个**建议后续变更**的决策：决定开源发布时改成完整路径（一次 sed 全仓替换 + go.mod 一行）。

**D7. MiniMax 默认接 OpenAI 兼容口而非 Anthropic 兼容口**
- 选择：`https://api.minimaxi.com/v1` + `type: openai`（DeepSeek 同理）。
- 备选：用它们的 Anthropic 兼容口（需要先写 anthropic Adapter）。
- 对比：OpenAI 口走零转换透传路径，MVP 无需任何翻译代码；Anthropic 口是 Phase 2 anthropic Adapter 的现成试验场。
- 建议：维持现状；Phase 2 写 anthropic Adapter 时正好用 MiniMax/DeepSeek 双兼容口做对拍验证。

**D8. 客户端断连不惩罚端点**
- 选择：发送中检测到客户端 context 已取消时，不给端点记失败。
- 备选：任何 client.Do 错误都记失败。
- 对比：客户端主动断开（用户按 Ctrl-C、UI 取消）与上游故障无关，记失败会造成健康状态污染。
- 建议：维持现状。

## 5. 已知边界（非缺陷，属 MVP 范围裁剪）

* 仅 `openai` 一种 Adapter、仅 `priority` 一个维度——Phase 2 清单见设计文档 §6。
* 若上游返回 HTTP 200 但 body 内嵌业务错误（个别厂商风格），透传路径不识别；实测三家默认 Provider 均正确使用 HTTP status，不受影响。
* 健康状态不持久化，重启清零（设计即如此）。
* `vmr status` 通过读同一份 config 找到监听地址；如果运行实例用的是别的配置文件，需 `-c` 指定。

## 6. 交付物清单

| 文件 | 说明 |
| --- | --- |
| `VirtualModelRouter_v2_Fable5.md` | 设计文档（含 §0.5 终审修订） |
| `DEV_PLAN.md` | 执行计划，全部勾选完成 |
| `README.md` | 使用文档 |
| `config.example.yaml` | 默认三 Provider 配置模板 |
| `cmd/` `internal/` `go.mod` `go.sum` | 源码（1339 行）+ 测试（648 行） |
| `.gitignore` | 已排除二进制、本地 config.yaml（含真实 key 引用）、.DS_Store |
| 本文件 | 实现总结报告 |

代码尚未 commit（遵循"不主动 commit"约定）；工作区当前即为可提交状态。

---

# 7. V2.2 增量：Anthropic 协议入口 + 全局并发限制（2026-07-07 同日完成）

## 7.1 概览

* **双协议入口，永不翻译**：新增 `POST /v1/messages`（Anthropic 协议）。调用方用哪种协议，就只在该协议的兼容端点间路由——原 Phase 2 的"翻译型 anthropic Adapter"计划**作废**（双向流式翻译正是设计文档 0-③ 警告的复杂度黑洞，且厂商已原生提供 Anthropic 兼容口）。"跨协议转换"已写入设计文档 §7 明确不做。
* **全局并发闸**：`max_concurrency`（缺省不限）。超限请求在内存中挂起（信号量阻塞，近似 FIFO 唤醒），客户端断开立即出队；`/admin/status` 暴露 in_flight/waiting。
* 代码增量：非测试 1339 → **1593 行**，测试 648 → **1034 行**；`go test -race` 全绿。

## 7.2 端到端实测（真实 Provider）

| 验证项 | 结果 |
| --- | --- |
| `/v1/messages` 非流式 | ✅ `claude` → MiniMax Anthropic 口，content 返回 PONG |
| `/v1/messages` 流式 | ✅ 标准 Anthropic SSE（message_start/…）逐块转发 |
| Anthropic 路径 failover | ✅ 坏 Key p1 401 → `deepseek_a` 接管，`X-VMR-Attempts: 2`，客户端无感知；坏端点进入 auth 冷却 |
| 协议隔离 | ✅ `claude` 走 `/v1/chat/completions` 被拒并提示改用 `/v1/messages`；反向同理；上游零请求 |
| 混协议配置 | ✅ `vmr check` / 热重载时报 "mixes protocols"，拒绝上线 |
| 并发闸 | ✅ limit=2 下并发 4 个真实请求：中途 `/admin/status` 显示 `in_flight=2, waiting`；后两个请求耗时约为前两个的 1.7 倍（等待放行），最终全部 200 |
| 回归 | ✅ 既有 MVP 测试与验收行为不变 |

实测确认的接入参数：MiniMax Anthropic 口 `https://api.minimaxi.com/anthropic/v1`、DeepSeek `https://api.deepseek.com/anthropic/v1`（Adapter 追加 `/messages`），鉴权用 `x-api-key`（MiniMax 强制）。

## 7.3 新增决策记录

**D9. 协议归属自动推断，不加配置字段**
- 选择：protocol 是 Adapter 的属性（`Protocol()`），Virtual Model 的协议由 endpoints 推断，混协议在配置校验期报错。
- 备选：models 配置里显式加 `protocol:` 字段并校验一致。
- 对比：显式字段是冗余声明——它要么与 endpoints 一致（无信息量），要么不一致（多出一类要处理的错误）。推断消灭了这类错误本身，符合"重设计问题以消除分支"的项目品味。
- 建议：维持现状。

**D10. 并发限制做全局闸，不做每端点限流**
- 选择：单一全局信号量，闸住两个聊天入口；每端点 rpm/并发精细限流留 Phase 2。
- 备选：直接实现每端点限流（Phase 2 清单项）。
- 对比：你的诉求（保护本机/整体用量、超限挂起）全局闸完全覆盖，实现 ~60 行；每端点限流服务的是"主动避免打出 429"，是另一个问题，且与健康冷却有交互，复杂度高一档。
- 建议：维持现状；出现单一 Provider 429 频繁时再上每端点限流。

**D11. 等待不设上限、不设超时**
- 选择：挂起的请求无等待队列长度上限、无服务端等待超时；唯一出队条件是拿到坑位或客户端断开。
- 备选：加 `queue_timeout` 或最大等待数，超限返回 429。
- 对比：本地工具的客户端本来就有自己的超时（SDK 默认或用户设置），服务端再加一层超时是重复机制；挂起的 goroutine 成本可忽略。若未来共享部署，可再加。
- 建议：维持现状。

**D12. 热重载仅在容量变化时更换信号量**
- 选择：`max_concurrency` 未变时复用现有信号量；变化时换新（换闸瞬间新旧持有者叠加，短暂超额）。
- 备选：精确迁移（带锁的计数器 + 条件变量，可精确收缩容量）。
- 对比：精确迁移代码量与出错面明显增大，收益只是消除"改配置瞬间可能多放行几个请求"这一秒级边界现象。
- 建议：维持现状（设计文档已记录该边界行为）。

**D13. 错误响应统一为双方都能解析的合并形态**
- 选择：vmr 自产错误统一 `{"type":"error","error":{"type","message"}}`；`/v1/models` 同时带 `object:"list"` 与 `has_more`/`type:"model"`。
- 备选：按入口协议分别产出两套错误/列表格式。
- 对比：OpenAI 客户端只看 `error.message`（多余的顶层 `type` 被忽略），Anthropic 客户端需要 `type:"error"` 信封——一个形态同时满足两者，两套格式是纯重复代码。
- 建议：维持现状。

**D14. 协议头白名单经 `CanonicalRequest.Header` 传递**
- 选择：服务端白名单（`anthropic-version`、`anthropic-beta`）随 CanonicalRequest 传给 Adapter；anthropic Adapter 缺省补 `2023-06-01`。
- 备选：Adapter 直接拿完整客户端 Header。
- 对比：完整透传把"哪些头可以外流"的决定权分散到每个 Adapter，白名单集中在 server 一处、Adapter 只见已过滤的子集，边界更干净。
- 建议：维持现状。

## 7.4 交付物增量

| 文件 | 变化 |
| --- | --- |
| `internal/adapter/anthropic/` | 新增（透传 Adapter + 测试） |
| `internal/adapter/classify.go` | 新增（openai/anthropic 共享分类表 + RewriteModel） |
| `internal/router/router.go` | 协议绑定 + 并发闸（327 → 426 行） |
| `internal/server/` | `/v1/messages`、双鉴权头、合并版 `/v1/models`、并发指标；新增 `server_v22_test.go` |
| `VirtualModelRouter_v2_Fable5.md` | §0.6 增量设计；Phase 2 翻译型 Adapter 作废；§7 增"跨协议转换" |
| `DEV_PLAN.md` | M8–M11 全部完成勾选 |
| `README.md` / `config.example.yaml` / `config.yaml` | 双协议 + 并发配置与说明 |
