<!-- Ver 2026-08-06 14:00, by Sonnet 5 -->

# vmr — 用户指南

[English](UserGuide.md) | 简体中文

完整的配置参考、协议行为细节、CLI 说明。如果只是想先跑起来，先看 [README](../README.zh.md) 的快速开始——跑通之后再回来看这份。

## 目录

- [配置](#配置)
  - [配置文件结构](#配置文件结构)
  - [启动与热重载检查](#启动与热重载检查)
  - [单请求内存预算](#单请求内存预算)
  - [上游代理](#上游代理)
  - [base_url 与 API 版本号](#base_url-与-api-版本号)
  - [角色改写 role_map](#角色改写-role_map)
  - [端点尝试顺序 priority 与 strategy](#端点尝试顺序-priority-与-strategy)
  - [环境变量](#环境变量)
- [请求处理与路由](#请求处理与路由)
  - [透传与归一化](#透传与归一化)
  - [故障切换与健康](#故障切换与健康)
  - [条件路由](#条件路由)
  - [Sticky Model 会话亲和](#sticky-model-会话亲和)
  - [额度感知路由 Quota-Aware Routing](#额度感知路由-quota-aware-routing)
- [审计与报表](#审计与报表)
  - [审计日志](#审计日志)
  - [用量与成本报表 vmr report](#用量与成本报表-vmr-report)
  - [Agent 任务叙事重建 vmr story](#agent-任务叙事重建-vmr-story)
- [请求图片自动降采样](#请求图片自动降采样)
  - [模型级覆盖](#模型级覆盖)
  - [降采样结果缓存](#降采样结果缓存)
  - [审计目录和缓存目录到底落在哪](#审计目录和缓存目录到底落在哪)
- [CLI 与端点参考](#cli-与端点参考)

## 配置

### 配置文件结构

`providers` 是一个扁平列表——一个账号一条，不管它实际讲三种入口协议（`openai` / `anthropic` / `openai-responses`）里的几种。`base_url` 本身按协议分 key，所以一个账号的两个协议面写在同一条里，不需要重复声明两遍。`models` 按虚拟模型名分组；`endpoints` 列表里每一条自带 `protocol` 字段，所以同一个虚拟模型名下可以同时挂一条 openai 协议的候选列表和一条 anthropic 协议的候选列表——两个入口各自独立可达。一条 endpoint-group 的 `models:` 列表可以写多个上游模型名，每个展开成独立的、各自健康跟踪的候选，共享这条 entry 的其余字段：

```yaml
listen: 127.0.0.1:8800
# api_keys:                    # 可选：保护 vmr（Bearer 或 x-api-key 都认）；每把 key 在
#   - ${VMR_KEY_ALICE}          # `vmr report` 里按各自的尾部打标签分组统计（见下文"多调用方场景"）。
#   - ${VMR_KEY_OPENCLAW}       # 旧的单把 api_key 已移除——配置里还写着它会被当作未知字段拒绝加载
# max_attempts: 0              # 每请求上游尝试数上限（缺省 0 = 试遍全部候选）
# probe_timeout: 15s            # 一次后台恢复探测的超时上限，见下文"故障切换与健康"
# max_request_body_mb: 8       # 入站请求体大小上限（仅为稳定性考虑；审计日志始终原样全量记录，不受此项限制）
# max_concurrency: 8           # 全局并发上限，超限请求挂起等待（缺省不限）——共享实例上线前先看下文"单请求内存预算"
# https_proxy: http://127.0.0.1:7890   # https 型 base_url 的代理服务器地址——vmr 用代理的唯一途径
#                                      # （环境变量被忽略；要引用就显式写 ${HTTPS_PROXY}）。
#                                      # 只是声明代理在哪，不代表默认开启——见下面的 `proxy`
# http_proxy: http://127.0.0.1:7890    # http 型 base_url 同理（如局域网 llama.cpp）
# image_downscale: 512         # 请求内联图片长边像素上限，缺省关闭（可被虚拟模型自身设置覆盖，见下文）
# image_cache_ttl_days: 7      # 降采样结果缓存的失效期（缺省 7 天）
# audit_retention_days: 30     # 超过此天数的审计文件自动删除（缺省永久保留）
# extra_redact_headers:        # 额外需要在审计日志里打码的客户端请求 header 名，处理方式与内置的
#   - X-Custom-Token            # Authorization/X-Api-Key/Cookie 等列表一致（大小写不敏感）。
#                                # 缺省/留空不改变任何行为。
# timeouts:
#   connect: 10s               # 连接上游
#   response_header: 120s      # 上游首字节
#   stream_idle: 120s          # 上游 body 静默看门狗（流式/非流式/错误体都覆盖）

providers:
  - name: openrouter
    base_url: {openai: https://openrouter.ai/api/v1, anthropic: https://openrouter.ai/api/v1}
    api_key: ${OPENROUTER_API_KEY}
    proxy: true              # 走 https_proxy/http_proxy——给海外 provider 开代理的
                             # 推荐写法（缺省 false，即直连）
  - name: minimax
    base_url: {openai: https://api.minimaxi.com/v1}
    api_key: ${MINIMAX_API_KEY}
    # proxy: false           # 这里不需要写——false 本来就是缺省值

models:
  coding:                      # 只有 openai 协议 → 走 /v1/chat/completions
    endpoints:
      - {protocol: openai, provider: openrouter, models: [z-ai/glm-5.2]}   # 不写 priority：列表顺序就是尝试顺序
  claude:                      # 只有 anthropic 协议 → 走 /v1/messages
    endpoints:
      - {protocol: anthropic, provider: openrouter, models: [minimax/minimax-m3]}
  agent:                       # 只有 openai-responses 协议 → 走 /v1/responses
    endpoints:
      - {protocol: openai-responses, provider: openrouter, models: [z-ai/glm-5.2]}
```

全部字段与校验规则见设计文档 Part 1 §10。修改配置数秒内热生效；坏配置被拒绝、不影响运行实例。解析是严格的：未知或拼错的配置键（如 `max_concurency: 8`）会直接导致加载失败，绝不会被静默忽略、让你误以为设置已生效。

### 启动与热重载检查

除了严格解析之外，vmr 在每次启动和每次热重载（fsnotify 或 SIGHUP，包括服务管理器自动重启触发的那次）都会顺带跑一遍*操作性*检查——跟 `vmr check`/`vmr diagnose` 打印的是同一套——命中的每一条都会打一行 `WARN config check: ...` 日志。最值得知道的一条：`api_key` 拼错或没导出的 `${ENV_VAR}` 展开成空串在 YAML 层面完全合法，所以加载和热重载都不会报错——这个 provider 下的每个请求会一直 401 到有人发现为止。这个告警是唯一的信号；它从不阻断启动或重载，因为 Check() 发现的问题按定义是"能跑但可能不对"。

### 单请求内存预算

三个各自独立、各自合理的缓冲上限乘起来看：`max_request_body_mb`（缺省 8MB，入站请求体）、响应归一化缓冲（8MB，防止需要缓冲而非直接流式转发时被失控上游撑爆）、审计响应副本（16MB，限制审计记录保留一条响应正文的上限，比归一化缓冲留了更多余量——审计副本被截断丢的是 `vmr report`/`vmr story` 需要的信息本身，不只是"聪明处理"）。三者都是按当下 ~1M-token 上下文窗口（约 3-4MB 字节量）留约 2 倍余量估算出来的，不是拍脑袋的整数。最坏情况大致是三者之和——约 32MB——每个在途请求都可能吃到这么多，还没算上 `bytes.Buffer` 扩容期间的额外开销。`max_concurrency` 缺省不限，所以这个数字唯一的上界就是同时涌进来多少个请求。单用户本地实例上这纯属背景噪音；共享实例上，请把 `max_concurrency` 设成一个具体数字，而不是让这四个数字的乘积保持无界。

### 上游代理

只认显式配置，默认关闭。`http_proxy`/`https_proxy` 只声明代理服务器**在哪**，本身不会替任何 provider 打开代理。一个 provider 是否真的走代理，完全由它自己的 `proxy: true`/`false` 决定（缺省 `false` = 直连，没有全局默认可继承——每个 provider 独立、显式决定）；只有它是 `true` 时，才按 base_url 的 scheme 选用 `https_proxy`/`http_proxy`。**推荐写法**：只给个别需要代理的 provider（通常是海外厂商）写 `proxy: true`，其余不写——单点意图，新增 provider 默认直连，不会被意外牵连。**代理环境变量被有意忽略**——隐式旋钮悄悄改变流量走向，最容易被忽略、排障时最难想到；要引用它就显式写 `https_proxy: ${HTTPS_PROXY}`。`proxy: true` 但没配对应的代理地址是校验错误（拒绝加载），不是运行时惊喜。`vmr check` 与启动摘要逐 provider 打印生效代理（凭证掩码）。YAML 1.2 语法：写 `true`/`false`，不能写 `on`/`off`。

### base_url 与 API 版本号

vmr 在初始化时预计算每个 provider 的完整上游 URL——直接把协议的裸路径（OpenAI Chat Completions 为 `/chat/completions`，Anthropic 为 `/messages`，OpenAI Responses 为 `/responses`）拼在 `base_url` 后面，不做任何归一化或重叠检测。所以 `base_url` 必须已经带上该 provider 自己的完整 API 版本号，不管它叫什么：`https://api.example.com/v1`、`https://api.minimaxi.com/anthropic/v1`、`https://ark.example.com/api/coding/v3`。这条规则的原因是：不是所有 provider 的 OpenAI/Anthropic 兼容面都叫 `v1`——比如火山引擎 coding plan 的 OpenAI 端点版本号是 `v3`——所以 vmr 不会替你猜版本号；写错了会立刻在你写的这个 base_url 上报 404，而不是被悄悄"纠正"成别的样子。URL 在配置加载时一次性计算并存入 Endpoint，adapter 直接使用，不在每次请求时构造或归一化 URL。

### 角色改写 role_map

有些 OpenAI 兼容 provider 会拒绝它上游不认识的 role——典型场景是 OpenAI 为 o1/o3 系列模型引入的 `developer` role，部分网关（如 DashScope/千问）会直接拒收。在 `models.<name>.endpoints[]` 的某条 entry 下写 `role_map: {developer: system}`，vmr 会在请求发往上游之前，把顶层 `messages` 数组（若这条 entry 是 `protocol: openai-responses`，则是顶层 `input` 数组）里匹配到的 `"role"` 值原地改写，客户端完全不用改。它是一个纯粹的旧→新字符串映射，只作用于列出的那几个 role——请求的其余每一个字节（键序、空白、未知字段、消息内容）原样透传，跟 `RewriteModel` 改写 model 字段用的是同一套字节级拼接手法。这个开关挂在 endpoint-group 一级，不是挂在 provider 或整个虚拟模型上——因为同一个账号可能背靠好几个虚拟模型、好几族不同的上游模型，不见得都要用同一套改写规则；某个模型如果从不发送被映射的那个 role，配不配 `role_map` 对它没有影响。不配置（或留空）`role_map` 的 entry 保持默认行为：所有 role 原样通过。

### 端点尝试顺序 priority 与 strategy

`endpoints:` 下每条 entry 都可以写 `priority: N`（整数，缺省 0）；端点按 priority 升序排列后再尝试，打平的情况（最常见——没人去设它）保持配置文件里的原始顺序，因为排序是稳定的。实际用法就是：把端点按你想要的尝试顺序列出来就够了；只有想在不重排列表本身的前提下调整顺序时，才需要显式写 `priority`。`priority` 是虚拟模型 `strategy` 列表里的一个维度（`strategy: [priority]` 是缺省值，截至本文写作时也是唯一实际注册的排序维度——这个列表暂时没有别的可加），所以绝大多数配置都用不上 `strategy`。

### 环境变量

vmr 涉及的环境变量全部在此——除此之外不读任何环境变量：

| 变量 | 作用 |
| --- | --- |
| config.yaml 里引用的任意 `${VAR}` | 加载配置时展开（每次热重载重新展开）；未设置的变量展开为空串。这是环境变量进入 vmr 的**唯一**通道——API Key、可选的 `${HTTPS_PROXY}`、可选的目录路径，都走这一条。 |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` / `ALL_PROXY` | **被忽略。** 代理只认上面的 `http_proxy`/`https_proxy` 配置项；要用环境变量的值，在 config 里显式引用 `${HTTPS_PROXY}`。 |

目录（`log_dir`/`image_cache_dir`，见下）和代理都是 config 字段，不是环境变量——理由一致：vmr 往哪写、怎么连网络，不该依赖隐式的运行环境。service 模式下，`vmr.sh service install` 会把 config 引用的全部 `${VAR}` 从当前 shell 快照进 `~/.config/vmr/env`（0600，已存在则不覆盖）——不需要再注入任何别的东西，二进制自己读 config。

## 请求处理与路由

### 透传与归一化

**原则：直连等价**。客户端经 vmr 收到的内容——字节、头部、传输节奏——与直连供应商一致。仅有的偏离：

- `model` 字段——请求侧改成上游真实名，响应侧改回虚拟名（SDK 假设 `response.model === request.model`）。请求侧改写是只针对顶层 `model` 值的字节 splice：其余每一个字节——键序、空白、未知参数——都按客户端原文逐字节到达上游；
- 两个 **MiniMax-M3 专属修复**，各自只在确认命中其确切形态时触发：剥 content 里的内联 `<think>…</think>` 推理（不剥会持久化进历史，把模型锁进反馈循环），以及剥 MiniMax 某个思考模式下（这个模式不写 `<think>` 标签）以纯文本输出的「Thinking Process:」思考段。两个修复都靠一个字面量措辞守卫触发——响应内容仍具备泄漏思考段的形状（编号小节、篇幅长）但没命中守卫时，会在 `attempts[].norm` 打一个 `thinking_process_pattern_detected` 标记：字节不改动，纯粹用于在剥离规则因措辞变化而失效之前先观测到征兆；
- `data: [DONE]` 哨兵——**仅** OpenAI 协议流式且上游未发时补；绝不重复，绝不注入 Anthropic 流。

流式是真的：事件到达即转发；只有检测到思考形态才缓冲，`</think>` 闭合后立即恢复实时流。带 `Content-Encoding` 的压缩体零变换直通。上游 3xx 重定向绝不跟随——301/302/303 的原始状态、`Location` 头、体原样到达客户端，和直连一模一样（`http.Client` 默认策略会把 POST 301/302/303 静默改写成 GET，这会破坏字节级保真）。响应头与请求头同一策略——除 hop-by-hop 外全部透传；错误响应连状态、头（含 `Retry-After`）、体原样返回。每个请求实际生效的归一化记录在审计日志 `attempts[].norm`，上游与客户端之间的任何字节差异都有逐请求的解释。

正因为透传是字节级的，三个协议任何一侧新增的请求/响应字段都不需要 vmr 改代码就能到达上游或客户端——这正是透传的意义所在。vmr **不做**的事：只路由 `POST /v1/chat/completions`、`POST /v1/messages`、`POST /v1/responses` 三个入口，其他 OpenAI/Anthropic surface（`/v1/realtime`、`/v1/images`、`/v1/audio` 等）不在范围内——这类需求请直接把客户端指向供应商自己的 base URL。`openai-responses` 协议面比另外两个更新、覆盖也更窄：截至本文撰写，只有 DeepSeek 和 OpenRouter 提供了 Responses 兼容端点（MiniMax 尚未支持），且两家都强制无状态——`store: true` 或非空 `previous_response_id` 会被上游直接拒绝，不是 vmr 拦的（vmr 从不检查或剥离客户端字段，只负责路由）。如果你对着一个真支持这些字段的上游使用 `previous_response_id` 或手动回放加密 reasoning item，注意 vmr 的 failover 可能把同一段对话的后续轮次路由到创建那份状态的端点之外——下文的 Sticky Model 能降低这种情况的概率，但不能从结构上根除它。

### 故障切换与健康

上游失败即按端点列表顺序逐个尝试，直到成功或全部耗尽（`max_attempts` 可选设上限）。健康完全由失败驱动，按响应逐条分类，确保每次失败既受到匹配的惩罚，也得到正确的"还要不要继续切换"的判断：

- 网络/5xx：短冷却指数退避；401/额度耗尽/模型不存在/某个网关或中转层报告了它自己的转发失败（而不是请求本身有问题）：长冷却；429/503：尊重 `Retry-After`；
- 400 类**客户端**错误——确实是请求本身写错了——直接返回，不切换、不冷却：换哪个端点都会被同样拒绝，切换解决不了任何问题；
- **内容合规拦截**切换下一家，但**不惩罚**被拦端点——它只是拒绝了这一条请求，并没有坏。

**冷却端点如何恢复**：端点冷却一到期，vmr 立刻在后台发一个专门的轻量探测请求（受 `probe_timeout` 约束，缺省 15s），而不是让下一个真实请求自己去踩一脚。真实流量在端点被确认恢复之前**完全不会碰到它、也不会等它**——探测没出结果之前，请求照样路由到下一个候选，不管探测本身要跑多久。探测会要求模型原样回显一个一次性 token，所以网关返回一个缓存/兜底的"假成功"不会被当成恢复。

```yaml
probe_timeout: 15s      # 一次后台探测的时间上限
```

全部候选失败时原样返回最后一次上游错误。流式只在首字节前切换。

### 条件路由

同一个虚拟模型背后挂的端点不必是完全等价的。声明每个端点实际支持什么,一条请求需要而某个端点没声明的能力,该端点会被直接跳过——而不是照样把请求打过去,得到一个必然失败的结果:

```yaml
models:
  agent:
    capabilities: [text, tools]        # 基线：下面每个端点都继承这个
    max_context_tokens: 128000         # 基线：同上
    endpoints:
      - protocol: openai
        provider: minimax
        models: [MiniMax-M3]
        capabilities: [image]          # 叠加在基线之上 -> 生效集合是 text, tools, image
        max_context_tokens: 1000000    # 覆盖基线，只对这个端点生效
      - protocol: openai
        provider: deepseek
        models: [deepseek-chat]        # 两个都不声明 -> 原样继承基线
```

两个字段在虚拟模型层和端点层都是可选的，缺省即**不限制**：虚拟模型不声明 `capabilities` 就没有基线，端点不声明自己的就视为支持模型基线里的一切（如果哪一层都没声明，就是什么都支持）——现有配置文件行为完全不变。`capabilities` 在端点层是**叠加**语义（与模型基线取并集）,`max_context_tokens` 则是**覆盖或继承**（单个数值没法取并集）。端点的生效能力集合一旦非空就是穷尽式的（把它真正支持的能力全部列出来，不是只列你想让 vmr 检查的那几个）；`vmr check` 会把每个虚拟模型的基线、以及每个端点自己声明的叠加/覆盖值打印出来，配置遗漏在这里一眼可见。

两类条件性质不同：

- **`image` / `tools`**——确定性的硬要求。请求需要某个能力但找不到任何候选声明支持时，直接快速失败，返回 `vmr_no_candidates` 并点名缺失的能力，而不是白白浪费一次必然被拒绝的尝试。`image` 的判断是结构性的（请求里是不是真的有 `image_url`/`source` 图片块），不是靠猜文本内容——正文里恰好提到"image"这个词的纯文本请求不会被误判；一张 vmr 自己的解码器认不出格式的图片，依然算作有图片（"检测到"和"解得出格式"是两回事）。（`thinking`/`audio`/`video` 暂不检测——这几项的请求侧探测逻辑在各厂协议上还没有确认，现在声明它们也不会有任何效果。）
- **上下文长度**——一个刻意保守的**粗估**，不是确定值：请求字节按 ASCII（约 4 字节/token）和多字节 UTF-8/中文等（约 2 字节/token，故意估得偏高）分类估算，每张检测到的内联图片按固定约 3000 token 计，检测到的文档/PDF 附件按其 base64 载荷长度 ÷ 20 估算——全程只做廉价的结构标记扫描，不解析内容。因为只是估算，它永远不会单独把一条请求拒之门外：如果所有端点声明的 `max_context_tokens` 看起来都不够，vmr 不会直接报错，而是照样在能力匹配的候选里尝试——高估的代价最多是浪费一次尝试，不会是一条本该成功的请求被拒。

完整设计与 token 估算的调研依据：`docs/VirtualModelRouter_Design_v4_Core.md`「条件路由」一节。

### Sticky Model 会话亲和

上游的 prompt cache 是按精确字节前缀匹配的。如果一条多轮 agent 对话在中途被路由到不同端点，上游的缓存就会失效，一次"看起来更合适"的路由选择反而可能让总成本更高——上面的条件路由本身就可能触发这种情况（比如 agent 压缩上下文后，估算出的长度缩小到低于另一个端点声明的上限）。Sticky Model 会把一条对话尽量留在最近一次成功服务过它的端点上：

```yaml
sticky_ttl: 10m              # 全局默认：粘性偏好保持有效的时长

models:
  agent:
    # sticky: true 是默认值，不用写；只有真正的单次调用场景（没有多轮价值可保护）
    # 才需要显式写 sticky: false
    endpoints:
      - protocol: openai
        provider: minimax
        models: [MiniMax-M3]
        # 继承全局的 10 分钟 sticky_ttl
      - protocol: openai
        provider: deepseek
        models: [deepseek-chat]
        sticky_ttl: 2h      # DeepSeek 磁盘缓存寿命数小时到数天——单独为这个端点覆盖
```

- **身份识别**：对话锚点取自 system prompt **和**第一条非 system 消息的哈希——两者都只哈希、从不记录或以其他方式暴露。两个恰好用同一句话开场的不同 Agent 不会被混同，因为它们的 system prompt（进而它们在上游真正的缓存前缀）不同；如果只哈希首条用户消息、不含 system prompt，恰好会漏掉这个场景。
- **`sticky_ttl` 是端点级的，不是模型级的**——缓存寿命是上游厂商的属性（Anthropic/OpenAI/MiniMax 大约 5-10 分钟；DeepSeek 数小时到数天），所以同一个虚拟模型下的不同端点可以各自声明自己的窗口，不必强行统一成一个值。全局 `sticky_ttl`（默认 10 分钟）是没有显式覆盖的端点的兜底值。
- **`sticky_ttl`（全局或端点级）不能超过 24 小时**——粘性注册表自己会在一条记录闲置 24 小时后把它从内存里清掉，不管端点自己声明的 TTL 是多少，所以写一个更长的值能加载成功，但会悄悄失效。`vmr check`/`vmr start`/热加载都会拒绝这类配置，并在报错里点名是哪个模型/端点。
- 亲和性只会在已经通过健康检查和条件过滤的端点里重新排序——一个之后变得不健康、或者不再满足某项必要能力的端点，不会仅仅因为它是上次的粘性选择就被复活。
- 每次成功完成请求（含 failover 后的成功）都会更新粘性指针，所以它始终跟随对话实际生效的缓存所在——一个过时的指针会在下一次成功请求时自动纠正，不需要额外的失效检测逻辑。

完整设计（身份信号的取舍、TTL 默认值背后的调研、为什么这里的指纹和下文 `vmr report` 离线会话分组是两套独立实现）：`docs/VirtualModelRouter_Design_v4_Core.md`「Sticky Model」一节。

### 额度感知路由 Quota-Aware Routing

如果你手上有好几个按周期计费的套餐（绑在某个厂商账号上的"编程计划"或"Token 计划"），外加几个按量计费的端点做兜底，额度感知路由会把新会话优先导向**相对自己账期进度还有余力**的那个账号——而不是简单地导向剩余额度绝对值最多的那个。这个区别很关键，因为各账号的重置日很少对齐：一个刚重置的套餐按朴素算法看起来"剩得最多"，但一个月度周期已经过完 90%、额度却还没用完的套餐，才是真正快要白白浪费额度的那个。按账号配置：

```yaml
providers:
  - name: plan-a
    base_url: {openai: https://example.invalid/v1}
    api_key: ${PLAN_A_KEY}
    quota:
      limits:
        - metric: requests       # 或 tokens（输入+输出，等权重）
          every: 1mo             # N{h,d,w,mo} —— 也可以是 5h、2w、3d
          since: 2026-08-01      # 周期锚点；后续周期自动推算
                                  # （缺省值：1mo -> 当月 1 日，1w -> 周一，其余 -> 当日）
          amount: 90000          # 该窗口的上限，按 vmr 自己观测到的口径填写——见下文
```

没配 `quota:` 的端点行为和之前完全一样——这是逐个 provider 的可选功能，不是全局开关。

**目前交付的范围**（完整现状见 `docs/VirtualModelRouter_Design_v4_Quota.md`「现状与后续计划」一节）：每个 provider 恰好一条 `limits:`，`metric` 可以是 `requests`、`tokens`，也可以是 `cost`（见下文[定价与 cost 计量档位](#定价与-cost-计量档位)），只支持固定（非滚动）窗口。单账号多条窗口、`rolling: true`、`models:` 子额度——写了仍会在**加载期直接报错**，点名是哪个字段、并说明它计划在后续批次（P3）提供。没有一项会被静默忽略；如果你写了这些字段而配置居然加载成功了，那是 bug，不是特性。

**`amount` 必须按 vmr 自己观测到的口径标定，不能照抄套餐的宣传数字。** 有些厂商把"一次用户提问"算作一个计量单位，但一个 Agent 客户端（工具调用、重试、多步骤流程）会把它展开成一到二十多次 vmr 真正看到并计数的 HTTP 请求。请用你自己的真实流量来标定 `amount`——跑几天看 `/admin/status` 的 `quota` 段，或者跑一次 `vmr report`——而不是抄官网价目表上的数字。标错了也不会导致故障（一个额度配少了的账号只会更早被降权），但路由决策的准确度会打折扣。

对 `metric: tokens`，vmr 优先使用上游返回的真实 usage（精确值），只有在拿不到时才降级为按字节数估算——拿不到的三种情况是：响应被压缩、上游不返回 usage 字段、或流在中途被截断。降级估算刻意偏保守（**宁可高估**），一个账号本周期内有多少比例的计数来自降级估算，会显示为 `/admin/status` 里的 `estimated_pct`。

**它不会做的事**：它从不会把某个端点从候选列表里剔除——一个额度耗尽的账号只是在自己的 priority 梯队里排到最后，其它端点都不可用时 failover 仍然会尝试它。它不会覆盖 Sticky Model——已建立的对话即使对应账号额度已经紧张，也会继续留在原端点；重排只对新会话生效。它也从不会主动触发降级——额度耗尽不会像真实的 429/402 那样让端点进入冷却，那仍然是 `internal/health` 的职责。

`vmr replay -provider NAME ...` 会按成功（状态码 `< 400`）响应计入同一份额度状态——它是拿真实流量打真实上游账号，所以计费方式和实时流量一致。`-dry-run` 从不计费（本来就没有发出请求）。

**如何查看**：`vmr check` 会打印每个 provider 配置的额度（以及周期边界所依据的生效时区——见下面的时区提示）；`/admin/status` 的 `quota` 段和 `vmr status` 会展示实时消耗（`used`/`amount`/`pct`/`headroom`/`period_ends_at`/`estimated_pct`、原始的 fresh/cache_read/cache_write/output 四分量明细，以及配置了的话该账号生效中的 `token_weights`/`model_multipliers`）；响应头 `X-VMR-Route-Reason` 在重排真正改变了排在最前面的端点时会显示 `pick=quota`。

周期边界（以及所有面向人的时间戳）都按 vmr 进程所在服务器的本地时区渲染（`vmr check` 的 `timezone:` 一行会打印出实际生效的值）——容器里如果没设 `TZ`，会悄悄按 UTC 处理，跟你以为的时区可能差好几个小时，且没有其它任何提示，值得部署后检查一下这一行。

#### 让数字更精确：`token_weights` 与 `model_multipliers`（P2.1）

一个普通的 `metric: tokens` 账号对新鲜输入、缓存读、缓存写、输出四个分量**等权重**计数——对一个纯粹按"总 Token 数"计费的套餐是准确的，但对 Credits 制套餐会**系统性高估**消耗：这类套餐的缓存命中价格往往只是新鲜输入的一个零头（市场实测比例从 5 倍到 120 倍不等）。一个实际只花掉预算 15% 的账号，在等权计数下可能显示为"已耗尽"，被降权，白白浪费掉套餐里没花完的大头。

```yaml
providers:
  - name: plan-d
    quota:
      token_weights: {in_fresh: 1.0, cache_read: 0.1, cache_write: 1.25, out: 4.0}
      model_multipliers: {"*": 1.0, heavy-model: 9}
      limits:
        - {metric: tokens, every: 1mo, amount: 1249000000}
```

- **`token_weights`** 在计算 headroom 以及 `/admin/status` 的 `used`/`pct` 时，对 `metric: tokens` 账号的四个分量重新加权——账号级（对该 provider 下所有 `limits:` 条目生效），未写的分量缺省为 `1.0`，且只对确实配了 `metric: tokens` Limit 的账号生效（配在纯 `requests` 账号上是加载期错误）。当账号的折算比例**在各个模型间统一**时用它；如果折算比例是**按模型分化**的，改用带 `pricing:` 块的 `metric: cost`（见下文）——按模型分化的价格没法用一组共享比例表达。
- **`model_multipliers`** 按实际命中的上游模型，对一次计费的**每个**分量（包括 `requests`）整体缩放——`"*"` 是通配兜底，没匹配上具名项也没有通配项时按 `1.0`（不缩放）。和 `token_weights` 不同，它在**计费落地的那一刻**就生效，不是读取时才套用——vmr 内部计数器按 provider 聚合、不细分到具体模型，读取时已经无法反推某一段计数来自哪个模型。非整数倍率永远向上取整（例如 1.5 倍作用在 3 个 token 上算成 5，不是 4）——安全的方向是宁可多算，绝不能少算账号的真实消耗。`model_multipliers` 只影响 `requests`/`tokens` 档；纯 `cost` 账号的价格分化完全由 `pricing:` 块承担，两者同时配置是加载期错误。

两个字段都不配置时行为不变——`token_weights` 缺省等同于 P1 一直在用的纯等权求和，`model_multipliers` 缺省让每笔计费保持 1 倍。

#### 定价与 cost 计量档位

`metric: cost` 直接按真实金额而不是次数/token 数给账号计费——适合按模型分化定价的 Credits 制套餐，这正是 `token_weights` 那套单一共享比例表达不了的场景。定价来自两层：**内置在二进制里的标准价目表**（不需要任何配置——数据源自一份公开的 LiteLLM 格式快照，MIT 许可，定期刷新）叠加你在 `config.yaml` 里写明的、与你账号不同的部分。

**推荐的默认做法：大多数部署完全用不到下面这些。** 如果你哪个账号都不用 `metric: cost`（纯 `requests`/`tokens` 档位完全不涉及定价），整个 `pricing:` 块直接跳过。如果用了、也不介意统一按美元记账，只需要 `pricing: {currency: USD}` 一行——`exchange_rate` 留空不用，下面每条 `providers[].pricing.overrides` 费率直接填美元数字就行。只有当某个账号真正的配额上限是用非美元货币计的（比如某个国产厂商的人民币套餐），才需要用到 `exchange_rate`。

```yaml
pricing:
  currency: CNY # 只要有账号用 metric: cost 就必填；标准表本身是 USD
  exchange_rate: {CNY: 7.1} # 一张通用的"1 美元 = X <货币代码>"映射表——只要 currency 不是 USD 就必填
  supplement: ./pricing.yaml # 可选：你自己补充的条目，格式与内置表相同，合并进来（冲突时你的胜出）——参见 pricing.example.yaml

providers:
  - name: anthropic # 把 provider 命名成厂商本名有助于自动解析——见下文
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 500} # 每月 500 元
    pricing:
      map: {my-claude-alias: anthropic/claude-3-7-sonnet-20250219} # 只在自动解析猜不出你的模型名时才需要
      overrides:
        - {model: my-model-x, in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54} # 这个账号对某个模型的实际谈判价，已经是 CNY（上面的 pricing.currency）
        - {model: my-model-y, currency: USD, in_fresh: 1, cache_read: 0.1, cache_write: 1.25, out: 4} # 或者：直接抄厂商美元发票上的数字，靠 pricing.exchange_rate 自动换算
        - {model: "*", discount: 0.6} # 兜底：其余模型统一按列表价 6 折
```

**一个模型的价格怎么找到**：先看 `providers[].pricing.map`（显式的"本地模型名 → 标准表条目"映射），再依次尝试三步自动解析——`<provider 名>/<模型名>`、裸模型名、或者在标准表里对 `*/<模型名>` 后缀做**唯一**匹配。如果最后一步能匹配上不止一条，vmr 会拒绝瞎猜（宁可没有价格，也不猜错厂商）——这时加一条 `map` 消除歧义即可。`vmr check` 会把每个 provider 实际解析到的结果打印出来，所以这一步从不需要你自己猜测。

**`providers[].pricing.overrides`** 是一条 first-match-wins 的规则列表：每条要么是 `discount`（对"下层解析出的费率"打折——下层可以是标准表，也可以是列表里更靠后的另一条 override），要么是显式的四分量费率（`in_fresh`/`cache_read`/`cache_write`/`out` 必须**四个一起给**——只给一部分会被拒绝，因为"其余的免费"和"其余的没写"是两件不同的事，vmr 不会替你猜是哪一种）。没有时间维度——具体模型的规则要写在 `"*"` 通配兜底规则**前面**，不能写在后面：既然某条规则命中与否不再取决于请求发生的时刻，一条被更早规则重复覆盖的模型模式就永远是死配置，`vmr check`/`vmr start`/热重载都会在加载期直接拒绝，而不是让它悄悄地永远不生效。显式费率还可以自带 `currency:`——省得你把厂商发票上的数字先手工换算成 `pricing.currency` 再填进来；`discount` 规则不能带 `currency:`（它是个无量纲乘数，不存在货币这回事）。

**缺失永远比错误安全**：一个 `metric: cost` 账号在加载期就会被拒绝——`vmr check`/`vmr start`/热重载走的是同一条校验路径——除非它配置要服务的**每一个**上游模型，在**所有可能的 override 组合**下都能解析出完整的四分量费率，而不只是常见情形下。显式写 `0.0` 算"已定价"（有些分量确实免费）；缺失字段不算——把缺失的 `cache_read` 静默当 0，会让账号显得比实际便宜，进而拿到更多流量、超支。这是 vmr 唯一一处刻意不做优雅降级的地方。

**`pricing.supplement` 的行也可以是非美元原生的**——补充表/standard-override 文件（见 `pricing.example.yaml`）里的某一行可以自带 `currency:`，直接抄厂商官网的原生货币价目表，不用手工换算。折算成 USD 用的汇率来自一个 `exchange_rate:` 块，可以就写在补充表文件自己里面（**推荐**：这样这份文件完全自包含、可以直接搬到别的部署用，而且它解析出来的 USD 价格不会因为某个 `config.yaml` 出于记账原因调整了自己的汇率而跟着漂移），对补充表没声明汇率的货币，则退回 `config.yaml` 自己的 `pricing.exchange_rate`。

**从旧版 `pricing.yaml` 侧车迁移**（P2.2 之前的机制，现已移除——`vmr report` 不再识别 `-pricing` 参数；和上面 `pricing.supplement: ./pricing.yaml` 只是撞了同一个文件名，两者并不是一回事——旧侧车的字段形状（`in_fresh_per_1m`/`date_range`/`updated_at` 等）不是新补充表的形状）：旧文件 `rates:` 里的每一条，对应改写成匹配 provider 下的一条 `providers[].pricing.overrides`，用显式四分量费率表达（`in_fresh_per_1m`/`cache_read_per_1m`/`cache_write_per_1m`/`out_per_1m` 直接对应新的 `in_fresh`/`cache_read`/`cache_write`/`out`）。`date_range`/`hour_range` 两元数组没有对应字段了（限时促销/错峰时间窗已作为复杂度/价值不匹配的一处简化被砍掉——见 `docs/VirtualModelRouter_Design_v4_Quota.md` 定价相关章节）——直接丢弃，只保留覆盖你当前实际生效价格的那一行。旧文件顶层的 `currency`/`exchange_rate`/`updated_at` 对应新的全局 `pricing:` 块的 `currency`/`exchange_rate`（`updated_at` 没有对应字段——标准表自带生成日期）。很多行在核对过标准表是否已经覆盖同一模型、且价格可接受之后，可以直接**删掉**——内置价目表存在的意义正是让这份文件的大部分内容变得不再必要。

`vmr report` 的 $ 估算走的是同样两层，在生成报表时独立解析——从 `-c` 指定的 config.yaml（默认 `./config.yaml`）读取，找不到时优雅降级为只用标准列表价。它对费率完整性的要求**刻意比** `metric: cost` **宽松**——报表遇到价格缺口只是那一行不显示 $ 数字，不是整份报表失败，因为报表的设计哲学就是"定价问题绝不能拖累报表其余部分"。`vmr report` 自己的展示币种选项（和上面的记账币种相互独立）见下文[成本估算与定价](#成本估算与定价)。

完整设计，包括留给后续批次的一切（单账号多窗口、滚动窗口、接官方用量 API 校准）：`docs/VirtualModelRouter_Design_v4_Quota.md`。

## 审计与报表

### 审计日志

默认开启：每个请求一行 JSONL，双层记录（调用方↔vmr 与每次 vmr↔上游尝试）、凭证掩码、生效的归一化清单，以及请求内联图片的元数据（格式/宽高/字节数，以及是否触发压缩/是否命中缓存——不论该虚拟模型是否开启了图片压缩，都会采集）。body 一律原样全量记录，不设审计侧截断上限（上面的 `max_request_body_mb` 只管入站请求体大小，与审计记录无关）。每次上游尝试同时携带一个人类可读的 `endpoint` 标签（`protocol:provider:model`）和拆开的三个结构化字段（`protocol`/`provider`/`model`），并在自由文本 `error` 之外新增一个类型化的 `error_class`。凭证掩码默认覆盖 `Authorization`/`X-Api-Key`/`Api-Key`/`X-Auth-Token`/`Cookie`/`Set-Cookie`/`Proxy-Authorization`；如果客户端自己发了一个 vmr 不认识的自定义鉴权 header（如 `X-Custom-Token`），需要配 `extra_redact_headers`（见上文[配置文件结构](#配置文件结构)）才会一并打码，否则会明文落进审计文件。

每条记录还带一个 `facts` 对象——vmr 自己对这条请求的路由前判断（`has_image`/`has_tools`/`estimated_tokens`），和路由当时用来选端点的值完全一样，原样落盘，不是事后重新算的。它是这条请求的兄弟字段，不是请求本身的一部分，所以记录下来的请求体依旧对客户端原始请求保持字节忠实。请求在路由开始之前就被拒绝时（鉴权失败、JSON 解析不了）这个字段整体不出现，不是一个全零值的对象。

```bash
./vmr start -c config.yaml                 # 写入 config 的 log_dir（`vmr check -c config.yaml log` 可核对）
./vmr start -c config.yaml -audit=false    # 关闭
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl
```

### 用量与成本报表 vmr report

```bash
./vmr report "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.json（明文/.zst 混着传也行）
./vmr report                                                          # 同上，完全不带 glob——见下文
```

**大多数情况不需要指定输入文件。** `vmr report` 和 `vmr story` 都接受零个位置参数：完全不写 glob，两者会自己从 `-c config.yaml` 的 `log_dir` 解析出来（`<log_dir>/vmr-audit-*`，明文 `.jsonl` 和压缩过的 `.jsonl.zst` 都能匹配）——只为读这一个字段而加载一次 config。所以对着你正在跑的这个实例做报表，直接 `./vmr report`（或 `./vmr story`）就够了；上面那种 `$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*` 的写法是留给指向*另一个*目录（别的实例的日志、归档的文件集）或自定义 glob 的场景用的。

`vmr report` 同时统计 tokens 与字节（上游不回报 usage 时以字节兜底）。

#### 报告章节

Markdown 按九个编号章节组织，每章回答一个运维问题。下面的 §2.5 与 §5.5 是嵌在相邻大节内的增强子章节，不是独立编号的大节——章节总数仍是九个。

- **§0 摘要** —— headline 数字 + 最多 3 条自动亮点（缓存效率低、工具 schema 浪费、端点异常）。
- **§1 成本与 Token 经济** —— 缓存命中/fresh/cache_write/reasoning 拆分，按模型缓存效率，按角色的消息字符/预估 token 占比。
- **§2 成本估算** —— 只要定价数据能解析出结果就渲染（见下文[成本估算与定价](#成本估算与定价)）；按模型/端点/客户端各一张表，末尾附一份本次用了哪些定价来源的摘要（标准表生成日期、补充表、override 条数）。
- **§2.5 账户（Provider）消耗与额度** —— 按上游账户（config.yaml 的 `providers[].name`）上卷的跨模型汇总：token/缓存效率/均值耗时/可靠性（含主要错误类，如 `rate_limit 12(63%)`——用来区分这个账户是硬额度耗尽还是短时被限流）/$ 估算。对没有 $ 定价的 Token Plan/AFP 类账户尤其有用：即使算不出 $，也能通过下方子表看到 token 消耗与配置额度的量级对比。主表本身不含额度列——账户在 config.yaml 里声明的额度（`quota`）只出现在**"额度与消耗对照"子表**里：只列配了 `quota:` 的账户，把本报表窗口重算的消耗与路由半区 `<log_dir>/vmr-quota.json` 的实时计数器并排给出（已用%/周期已过% 并排，另加该周期消耗中有多少来自降级估算而非精确 usage 的标注）——两个消耗数字是两个不同的时间窗口，故意不做减法；计数器仍停留在更早周期时显示 `-`。重算列的精度按 metric 不同，表下脚注会写明：`metric: requests` 是**恒等复现**路由半区的记账（它数的是已转发的上游成功响应，正是路由记账的那一刻——不是客户端请求数，因为所有 attempt 都失败的请求路由半区一分不记）；`tokens`/`cost` 是带已知出入源的估算，且在"有流量但什么都算不出来"（usage 全不可解析、或全无定价）时渲染 `-` 而不是会误导人的 `0`。
- **§3 可靠性** —— 端点可用度/错误率、错误类别拆分，以及 Quirk 修复频次拆分（每个端点的成功响应里，有多少比例需要剥离 think/thinking-process 或命中软屏蔽检测——单条请求的详情页本来就会逐一叙述这些步骤，这里是跨请求的频次统计，不用把几千个详情页挨个打开才能看出规律），因为每个入口协议面都各自独立路由，三张表都按实际用到的协议各拆一份（openai 排第一、anthropic 第二，其余协议按字母序），外加每小时错误数图表。
- **§4 延迟与吞吐** —— 按模型、按端点的 ttft/耗时分位数，都按吞吐量降序排列，各自带样本量，n<20 标 `⚠️low-n`。
- **§5 负载分布** —— 按虚拟模型、按工作负载类（交互 vs 定时脚手架）、按端点、按客户端（后两张表还带每请求输入/输出 token 分位数），外加每小时和每日的请求量/输入 token Mermaid 图表。
- **§5.5 按客户端的上游归属** —— 每个客户端命中了哪些上游端点（`protocol:provider:model`）、各自拿走了多少 token——按客户端分组、组内按 token 降序，回答"这个 Agent 的流量到底落到哪几个账户/模型上了"（同一模型名可能挂在两个不同账户下，只按模型名合并会抹掉这一刀）。
- **§6 会话与任务** —— 只列 interactive 会话，按 Chat User 分组（单发定时会话改放进请求详单里，见下文[索引文件](#索引文件)）。§6.5（Sticky 有效性）和 §6.6（端点性价比）的内容见下文[Agent 感知分析](#agent-感知分析)；§6.7（Compaction 还原）是本期每一次独立历史压缩 LLM 调用的记录：链接到哪个会话、tokens_in→tokens_out、保留比，以及一份规则筛出的被吞掉内容样例（不靠 LLM 判断，只呈现可观察的事实）。
- **§7 效率与浪费** —— 自动发现 + 每个声明工具集的完整"已调用/从未调用"明细（见下文[Agent 感知分析](#agent-感知分析)）。
- **§8 请求索引** —— 指向 `vmr-requests.md`（见下文[索引文件](#索引文件)）。

每张表都控制在几列以内；分位数都是每个桶的真值——每个桶在单趟遍历里直接收自己的原始样本、自己算 p50/p95，不做跨桶近似（合并"已经算完的"桶算不出真百分位，因为原始值早被释放了）。`⭐` 标记衍生/预估指标（相对上游原始返回值而言）。每小时/每日活跃度和每小时错误数都用 Mermaid `xychart-beta` 图表渲染。

运行进度写到 stdout，每一行都带 `yyyy-MM-dd HH:mm:ss.SSS` 时间戳，方便看清每个阶段实际花了多久：会话分析最先跑（按输入文件并行处理——在天数多的语料上这是耗时最长的单一阶段——过程本身不打印逐文件的进度行），然后聚合与详单导出合并成一趟：一个文件一行 `[i/N] <path>  done: M records (Ts)`，详单渲染在自己的 worker 池上跑，与喂给它数据的文件扫描并发进行——因为一条记录的详单页面只依赖它自己的内容，跟其他记录累积出来的任何东西都无关。JSON（`vmr-report.json`）是二次开发（图表/Dashboard）的数据源——Markdown 里只展示 Top-5 或做了折叠的明细，JSON 里都是全量。

#### 成本估算与定价

`vmr report`（用 `-c config.yaml`，跟它找 `log_dir` 用的是同一个参数）用的是和 `metric: cost` 额度完全相同的两层定价模型——二进制内置的标准价目表，叠加你 `config.yaml` 里声明的 `providers[].pricing`/全局 `pricing:` 块——完整配置形态见上文[定价与 cost 计量档位](#定价与-cost-计量档位)（`map`/`overrides`/`discount` 全部原样适用）。找不到 `config.yaml` 时优雅降级为只用标准表的列表价、没有账号覆盖——不会因此拖累报表的其余部分。和 `metric: cost` 额度账号不同（只要有一个模型解析出不完整的费率就在加载期直接拒绝），报表里某一行价格解析不完整或缺失时，只是那一行不显示 $ 数字——报表的哲学是"定价缺口只丢一个数字，不丢整份报表"。

**展示币种和记账币种相互独立。** `-currency CODE`（或 `report.yaml` 的 `currency`）决定报表 $ 列实际显示成什么币种——比如记账用 `pricing.currency: USD`，但想给别人看一份 `-currency CNY` 的报表。这是一次纯粹的展示层最终换算（`internal/pricing.Resolver.WithDisplayFactor`），发生在每个数字已经按记账币种算完之后——从不改变 `metric: cost` 账号实际被扣掉的金额。所需的汇率来自 `config.yaml` 的 `pricing.exchange_rate` 和/或 `report.yaml` 自己的 `exchange_rate`（同样"1 美元 = X `<货币代码>`"的形状——见 `report.example.yaml`），后者在 key 撞车时优先；而且当完全没有 `config.yaml` 可用时，这是唯一能让 `-currency` 生效的办法，因为 `report.yaml` 本来就设计成能独立使用。`-currency` 解析不出汇率时降级为显示记账币种、打一行警告——不是硬错误。

#### Agent 感知分析

`vmr report` 还能读懂 Agent 工作负载——全部离线、纯规则、不调用 LLM（方法与实证见 `docs/VirtualModelRouter_Design_v4_Analytics.md` 的"两遍读取：`AnalyzeSessions` + `Build`"一节）：

- **会话 → 任务 → 轮次分组。** 每轮重发同一段渐增对话的请求以首条非 system 消息做指纹（Claude Code 的 `metadata.user_id` 存在时优先），按最长公共前缀成链——多个 Agent 会话即使在时间上互相穿插也能干净分开。任务边界来自 Traceparent trace-id 变化与增量中的新用户指令，两个信号互为交叉验证。Compaction 调用被识别并双向链接，会话与其压缩后的续接体串成同一条线程。
- **`vmr-requests.json`** —— `requests` 字段：每请求一行特征（会话/任务/轮次、trace 与 chat id、请求形态、`heartbeat` 等标签、当轮 tool 调用、finish_reason、"ok 但截断"标志、含 reasoning 的 token 细分、增量大小、最新指令），jq / DuckDB / pandas 直接可用（这之前是逐行一个 JSON 对象的 `vmr-requests.jsonl`，现在合并成单一文档）；外加 `files` 字段——按内容哈希索引的输入文件解析缓存，日志目录大部分没变时，重跑 `vmr report` 能跳过没变文件的重新解析（和 `vmr story` 的 `vmr-stories.json` 是同一套机制，见下文）。
- **Sticky 有效性（§6.5）⭐** —— Sticky Model 存在的唯一理由是让上游 prompt cache 保温，这一节是它有没有兑现的证据：同一会话内，落回**上一条请求所用端点**的请求 vs 换了端点的请求，比较两组缓存效率。按结果（端点连续性）而非按机制度量，所以 sticky 指针命中却落到一个冷端点照样算切换。会话首条无前驱，计数但不入组；任一组带 usage 的样本 < 20 条时只出表、不下结论。**不解释切换原因**——sticky_ttl 到期、端点冷却、条件路由淘汰、该模型没开 sticky，事后无法区分。再按虚拟模型拆一张表：sticky 是按虚拟模型配的，那才是能动手的粒度。
- **端点性价比（§6.6）⭐** —— 不是"这个端点花了多少钱"（§2 已经答了），而是"单位产出的代价，以及它的失败让你多等了多久"：成本/1M out token、成本/成功请求、失败尝试数、**失败尝试累计墙钟时间**。一个单价便宜但经常失败的端点不便宜，但这在按端点的花费列里看不出来——钱记在最终成功的那一家头上。**只记时间不折算成钱**：失败尝试拿不到 usage，厂商通常也不对失败请求计费，给它标金额会是编造。
- **工具使用报告（§7）** —— 按声明工具集分组：声明的工具 vs **当轮实际调用**的工具（从响应中提取,历史重发绝不重复计数），外加"声明但从未调用"清单——两者都折叠进每个工具集自己的 `<details>` 块（numbered list + 字母序，自然让 `feishu_*` 同前缀聚类，避免 60+ 工具的 schema 撑爆文档）——及其每请求字节成本，为从 Agent 配置里裁掉没用的工具提供直接依据。

#### 逐请求详单

`vmr report` 还会把每条记录导出为一个人类可读的 Markdown 详单**外加一个同名 JSON 文件**（原始 record，方便 jq/脚本查询），落在 `{out}/details/` 下，用于深挖单个请求：头部一行定位（trace / chat user / tools，取值加粗），紧接一段 `VMR 路由前判断`，读上文提到的 `facts` 对象——只列出**实际探测到**的能力（`image`、`tools`，各自渲染成一个反引号包裹的小标签，都没探测到时显示"无"），加预估 token 数——该记录没有 `facts` 时这一段完全不出现，再是**完整消息列表**（每条消息默认 `<details>` 折叠；本轮新增的消息在 summary 上加 🆕 前缀，末尾追加一行 `🆕 本轮增量（相对上一轮,+N 条,#1–#M 为历史上下文）` 汇总）、每次上游尝试的 headers 与 body 字段全量对照（变化项以 emoji 标记：🟢 新增 / 🔴 删除 / 🔶 变化）——若该次尝试剥离了 `<think>…</think>` 推理块，还会展示剥离前的完整内容及对应原始 SSE（字段缺失的旧格式日志显示"未保留"提示）、客户端响应部分把 SSE 流重组成模型实际输出并保留原始事件全文。文件名以零填充时间戳开头，按名字排序即按时间排序。加 `-details=false` 可关闭详单导出。

#### 索引文件

`vmr-requests.md`（与 `vmr-report.md` 并列，在 `details/` 上一级）是一份纯索引：每个分组一条 `## Chat User: <key> · N 会话 N 任务 N 轮`（或 `## 定时任务 · <class> 单发会话 × N`），带一行摘要引用块和指向该分组自己那份完整详单的链接。真正的 **Chat User → Session → Task → Turn** 展开——每个会话一个 `## sNN · <时间> · N 任务 N 轮` 标题，每个任务一个 `### tNN · <时间> · N 轮` 标题，带首条消息的引用块和轮次表（`轮 / 时间 / msgs / finish / dur / ttft / fresh/cached/out / cache-eff⭐ / 文件`；所有时间戳统一转本机系统默认时区，不管原始记录自带什么时区）——只存在于对应的独立文件里，不会在索引里重复一遍。独立文件的命名：每个真实 `client_key_tag` 对应 `vmr-requests-<tag>.md`，没有标签的会话归到 `vmr-requests-unresolved.md`，每个定时任务类别对应 `vmr-requests-cron-<class>.md`（`heartbeat` 这一个的文件名固定是 `vmr-requests-cron-hartbeat.md`；以后新增的定时任务类别照 `-cron-<class>` 这个模式来）。单发的定时会话（heartbeat/dream_diary——只有一次请求、没有真实来回）不管是哪个客户端发起的，永远归到它对应类别的 cron 文件里，这样一堆近乎重复的轮询请求既不会淹没真实对话，也不会同时出现在两种分组下；轮次数大于一的定时会话（真正的多步 cron 任务）则作为普通会话卡片挂在自己调用方名下。索引文末的 `# 全部请求（时间序）` 依旧是一张不分组的时间序全量表。每一处"文件"列都同时链接到 Markdown 详单和同名的 JSON 详单。

**错误/截断索引（`vmr-requests-failed.jsonl`/`.md`）**：每次 `vmr report` 运行都会额外写一份按时间排序的过滤视图——只包含 `outcome == error`/`canceled`，以及任何"ok 但被截断"的响应——这样排查"到底哪里出了问题"不用先翻过全部成功的请求。纯叠加：不会把这些请求从 `vmr-report.md`、`vmr-requests.md` 或它们所属的会话分组里挪走，只是多了一条过滤后直达同一批记录的路径。`.md` 是人类可读的索引（和 `vmr-requests.md` 各个子文件同一种"一行摘要+链接"格式）；`.jsonl` 是没有会话/任务分组、也没有自己文件缓存段的扁平逐行 dump，给 `jq`/脚本用。

#### 输出语言

`vmr report`/`vmr story` 默认输出英文（上文这些示例展示的是切到中文之后的样子）。在当前目录放一份 `report.yaml`（写 `language: zh`）即可切换成中文，或者在命令行上加 `-lang en|zh` 只影响这一次运行——`-lang` 优先级高于 `report.yaml`。`report.yaml` 是独立的一份小文件，跟 `config.yaml` 完全无关：是可选的，存在就从当前目录自动加载（`-report-config path` 可以指向别的路径）。它可以放一个真实的密钥（`llm_key`，见下文），所以跟 `config.yaml` 一样 `.gitignore`——仓库根目录提交的是模板 `report.example.yaml`，照着复制一份改。这个开关只影响 Markdown 文档的文字——`vmr-report.json`/`journey-*.json`/`compare-*.json` 不受影响：里面的叙述性字段（比如 `efficiency[].finding`、`compare-*.json` 的 `rows[].label`）不管 `-lang` 是什么，永远是英文，写脚本解析这些 JSON 不需要考虑报告是用哪种语言生成的。

`report.yaml` 不止管语言：`-o`/`-details`/`-include-partial`/`-currency`/`-llm-addr`/`-llm-model`/`-llm-key`/`-llm-cache-dir` 都可以在这份文件里预先写好默认值（`currency`/`exchange_rate`——见上文[成本估算与定价](#成本估算与定价)），同名命令行 flag 显式传了照样优先——完整字段和注释见仓库根目录的 `report.example.yaml`。`llm_key` 可以直接写明文（这份文件已经 `.gitignore`），也可以写成 `${VMR_LLM_KEY}` 这样引用一个已有的环境变量，两种都行；`llm_cache_dir` 没有隐式默认路径，两处（flag、`report.yaml`）都不设就完全不缓存 LLM 调用结果。

#### 多调用方场景

如果一个 vmr 实例被多个调用方共用（队友、另一个 Agent、CI 任务），想在事后统计里把各自的用量分开看，就给每个调用方在 `api_keys` 下各配一把 key（见上文[配置文件结构](#配置文件结构)），不要多人共用同一把。每个请求会用命中的那把 key 自身的尾部给审计记录打标签（`client_key_tag`，取法见 `KeyTag`：末 8 个字符，若这 8 个字符里有 `-`，只保留最后一个 `-` 之后的部分——所以 key 以 `...-alice` 结尾时标签就读作 `alice`；建议有意义的部分留 ≥3-4 位，太短容易和别的调用方撞标签）。`vmr report` 会自动识别，不需要加参数：每观测到一个不同的标签，就多写一份 `vmr-requests-<tag>.md` 详单（见上文[索引文件](#索引文件)）——同目录下，标签文件里 `details/…` 链接不用做任何调整。`vmr-report.md`/`.json`、`vmr-requests.json` 和 `details/` 本身永远不分组、不重复：单条请求的详单只写一份，与调用方无关；`vmr-requests.json` 永远覆盖全部记录；汇总报告永远覆盖所有人。不配置 `api_keys` 就什么都不会变——不多一个文件，不多一列。完整设计说明见 `docs/VirtualModelRouter_Design_v4_Analytics.md` 的"逐请求详单与索引"一节。

纯内网、根本不想要真实鉴权？`api_keys` 不配置——门照样完全敞开——但客户端自愿发来的任意 Authorization/x-api-key 值依旧会走同一套标签提取逻辑并记录下来，vmr 侧不需要配置任何东西：每个客户端自己把发出去的值末尾带上 `-<标签>` 即可对 `vmr report` 自报家门。这个模式下没有 16 字符下限（本来就不是要保护的秘钥）；什么都不发的客户端依旧是未打标签的记录。

#### 保留期与压缩

Agent 场景下每一轮都会把完整对话历史重新发一遍，单日日志动辄几个 GB——而且这种冗余主要出现在**行与行之间**，不是单行内部。每天的日志文件一旦不再是"今天"就自动轮转压缩：用 zstd 压缩整个文件（而不是逐行压缩）才能吃到跨行的重复内容，实测压缩比 20~75 倍——这是逐条记录单独压缩根本达不到的量级，因为单条记录看不到上一轮几乎重复的请求体。`vmr report` 对 `.jsonl` 和 `.jsonl.zst` 一视同仁，通配符同时覆盖两者即可。设置 `audit_retention_days` 还能让过期文件自动删除（缺省永久保留，不设置不会删任何东西）；压缩和清理都只看文件名里的日期，不需要扫描或逐个 `stat` 整个日志目录。背后的实测数据和方案取舍见设计文档 Part 1 §9.5。

**不要让两个 vmr 实例共用同一个 `log_dir`。** 每个实例的 housekeeping 清扫只靠文件名里的日期判断"这份日志今天写完了"（可以压缩，过了保留期还能删）——它没有办法知道另一个进程是不是还在往同一个文件里追加。两个实例共用 `log_dir` 又都跨过了午夜，才会踩到这个坑：实例 A 轮转到今天的文件，看到昨天的文件已经"写完"，于是压缩、删除——而实例 B（还停在昨天的日期，或者轮转得慢一点）还在往那个已经被删除的 inode 里写。给每个实例（包括为了测试临时起的第二个 checkout）都配一个独立的 `log_dir`。

### Agent 任务叙事重建 vmr story

`vmr report` 回答的是"这段时间总共花了多少、整体怎么样"；`vmr story` 读的是同一份审计 JSONL，但回答的是"这一个任务具体发生了什么，一步一步地看"——它把单次 Agent 任务的完整执行过程重建成上下文演化过程：每一轮进了什么新内容、模型拿它做了什么，以及（如果发生过）一次历史压缩具体丢了什么。

```bash
./vmr story "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"           # 列出候选任务
./vmr story -journey j-agent-20260716T152238-20260716T153122-42f908fa \
    "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"                   # 按 id（前缀即可）渲染一个
./vmr story -journey 'j-agent-*,j-openclaw-*' \
    "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"                   # 批量渲染两个模式各自匹配到的全部候选
./vmr story -render-all "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # 一次批量渲染全部候选
./vmr story -corpus "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"       # 跨全部候选的语料级统计
./vmr story                                                                       # 和 vmr report 一样的文件解析捷径——不用写 glob
```

不带 `-journey` 时列出全部候选任务：id、任务/轮次数、时间范围、标题预览（开场的真实指令）——挑一个传给 `-journey`。`-journey` 接受逗号分隔的多个 token，每个 token 要么是 id/id 前缀，要么是匹配完整 id 的 shell 风格通配符（`*`、`?`、`[...]`）——通配符记得在 shell 里加引号，避免被 shell 自己展开。选择器只解出一个 journey 时直接渲染（也是唯一支持 `-llm-addr` 的形式）；解出不止一个时走 `-render-all` 同一条批处理路径，共享底层的文件扫描，不会每个候选各自重新扫一遍源文件。产物落在 `{out}/stories/journey-<id>.md`（叙事正文）与 `journey-<id>.json`（同一任务的行为剖面，见下文）——权限与 `details/` 一致（0600/0700），两者都承载完整对话内容。`-show-ungrouped` 打印前几条未能归组进任何会话的记录的来源位置——候选列表比预期短时的排查手段。

不管带不带任何选择性 flag（无参数列表、`-journey`、`-render-all`、`-compare`、`-corpus`），每次运行都会额外写一份 `{out}/stories/vmr-stories.json`/`.md`——候选索引（字段和上面终端列表一致，外加每个候选的 chain 涉及哪些文件、渲染过就带上 `journey-<id>.md` 的链接），把以前"跑完只打印到终端、关掉就找不到"的候选列表落了盘。`vmr-stories.json` 的 `files` 字段同时是一份按内容哈希索引的解析缓存——没变过的输入文件直接复用已解析的结果，不用重新扫；变了的或全新的文件才重新解析，Journey 关系图始终基于完整文件集重建，不会因为缓存而漏看任何文件，只是省掉了解析这一步的开销。设计说明见 `docs/VirtualModelRouter_Design_v4_Analytics.md` 的 `vmr-stories.json` 一节。

如果一个任务自己的开头看起来像是在续接一段本次没有加载进来的更早历史（表现为：真实的多轮开场恰好出现在你最早那个输入文件的最开头），默认会跳过——它的 id 在不同的文件加载范围下不保证稳定。加 `-include-partial` 可以照样渲染，文件名会带上 `-partial` 后缀，提醒你这个 id 换一批输入文件可能会变。

#### 怎么读这份叙事

每个任务按用户指令切成若干轮（`Step`）分组归入对应的任务（`Task`）。每一轮把 **Messages**（这一轮新进入上下文的内容）与 **LLM Response**（模型自己产出的内容——推理块、回复文本、每个工具调用的完整参数）分开展示，"Agent 拿到了什么"和"Agent 决定做什么"不会混进同一份笼统的历史视图里。一次历史压缩事件（靠结构判断，不是靠认某个特定 Agent 框架的标记文本——原因见设计文档）会渲染成一段带标注的边界：压缩前后的 token 数，以及压缩前提到过的文件路径/URL 里，哪些在压缩之后再也没被提到过。这里的每一个数字都能追溯回某次请求自己记录的 usage，每一条"被吞掉"的判断也都是一次可以自己打开原文核对的简单字符串比对——不是猜出来的。

#### 决策脊柱与疑似问题

完整逐轮叙事之前，每个渲染出来的 Journey 会先给一张概览卡（起始/首个错误标记/首个转折点/结束时间，加上"工具密集型""重试多"这类基于阈值的粗分类标签），紧接一张**模型使用**小表（用过的上游模型/账户各自的 Step 数与 token）+ 切换记录（仅发生过切换时才渲染，标出第几步、从哪换到哪，以及是否恰好落在一个触发过 failover 的 Step 上），再是一条决策脊柱——按 Task 分组、只列工具调用（工具名 + 参数摘要）的紧凑列表，命中规则检测器的调用打 ⚠️。每个 `Step` 标题也会带一个角色标签（🔧执行 / 📋规划 / 💬汇报 / 👀观察 / 🔄重试 / ⚠️错误 / 🧹压缩），有工具调用的 Journey 末尾还会附一张 ASCII 工具调用时序图（每个工具一行、每个 Step 一列），用于发现线性阅读容易漏掉的重试密集模式。**疑似问题**小节列出这些规则检测器的实际命中——九个零 LLM 成本、纯字符串/结构匹配的检测（同一工具调用重复出现、只叙述不动手、错误后未经复核就当成功、推理里提到的东西下一次工具调用没碰、宣告的计划后续条目没被执行、重试参数和出错那次一字不差、工具结果里的实体后续再没被提及、结果看起来像失败但引用还在延续、以及压缩边界悄悄丢掉的约束文本）。每条 Finding 都写成"检测到疑似 N 次，建议人工复核"，不是判决——是给人看的候选清单，不是自动化的根因结论。

#### 行为剖面

`journey-<id>.json`，与 `.md` 同时写出：九项规则派生、零 LLM 成本的指标——模型时间/Agent 侧执行时间/人类空闲时间的三分解、工具调用分布、重复动作率、错误恢复次数、计划/执行比、上下文构成演化曲线（每一轮请求体里各角色 token 占比,让上下文预算的构成随任务推进的变化可见）、上下文有效利用率（进入上下文的内容有多少后来真的被再次引用过）、compaction 次数与信息损失，再加一项列表型的**模型使用与切换**——这个 Journey 用过哪些上游模型/账户、各自的 Step 数与 token，以及每一次切换发生在第几步、从哪换到哪（取值来自实际请求的上游端点，不是客户端请求的虚拟模型名，因为后者一个 Journey 内几乎不变）。不管背后是 Claude Code、OpenClaw 还是别的框架，这套数字定义都一样——能横向对比不同 Agent 框架正是收集它们的初衷。

#### 对比两个任务

`-compare <id1,id2>` 直接对比两个任务的行为剖面（不需要先分别 `-journey` 渲染），产物是 `compare-<id1>-vs-<id2>.md` + `.json`，与单任务的文件放在同一目录。每一行都同时展示两侧的值和相对变化，差异大到超过固定阈值时打 ⚠️ 标记（规则化判定，不是主观判断）——适合回答"换了 Agent 框架/prompt 之后，这个任务的完成方式到底有没有变"这类问题。报告里还包含以下同样零 LLM 成本的规则事实：双方各自用到的端点、逐轮 Prompt 缓存命中率曲线、双方 system prompt 的规模与稳定性（含有边界的节选，默认前 2 万字符——够覆盖两侧真实验证用例里"加载了哪些项目上下文文件"这类声明在原文中出现的位置，但仍是从开头截断的一段前缀，不保证覆盖任意长度 system prompt 的全部信息量）、末轮上下文按角色的构成、总耗时（紧邻已有的"净工作时长"一起展示，不单独当效率指标看）、双方的终止方式，以及——如果任务产出是通过一次"参数形状像文件写入"的工具调用落盘的——双方最终交付物本身的内容节选。两个开场可比的 Journey（同一任务、换了 prompt/模型/框架重跑）还会得到一个规则派生的**分叉点**：两侧工具使用结构第一次出现差异的位置（换了工具，或同一工具但参数不同），标轻度或重度。这只是一个结构事实——"从这里开始不一样了"——不对哪一方更好或为什么下判断，渲染出来的报告里也会明确带一句免责声明。报告末尾的"证据溯源"小节列出本次对比实际读取的源审计文件路径，方便独立核对。

#### 语料级统计

`-corpus` 把 `-compare` 的"两个 Journey"对比扩展成"输入文件里找到的每一个候选 Journey"——指标分布（`-compare` 对比的十三项行为剖面数值各自的均值/中位数/最小/最大/p90）、Finding 命中率、指标两两之间的 Spearman 秩相关（只报效应量，不报 p 值——当前语料规模撑不住严格的显著性检验）、以及 Finding 分组比较（命中某个 Finding 的 Journey vs 未命中的，比较净工作时长中位数）。产物落在 `{out}/stories/vmr-story-corpus.md`/`.json`，复用 `-render-all` 同一套批量文件扫描。和 `-compare` 一样，不带任何成功/失败标签——VMR 没有任务是否真正完成的信号，只有耗时这类规则派生的代理指标。`-corpus` 不接受 `-journey`/`-render-all`/`-compare`/`-llm-addr`。

#### 可选的 LLM 解读小节

加上 `-llm-addr host:port -llm-model name`（一个已经在跑的 VMR 实例的地址和它暴露的虚拟模型名——不会自动拉起该实例；如果那台实例配置了鉴权还需要 `-llm-key`），可以在只解出一个 journey 的 `-journey` 或 `-compare` 上（不支持 `-render-all`/`-corpus`，也不支持解出不止一个 journey 的 `-journey` 选择器——那样每个 Journey 都要各打一次 LLM 调用，这笔费用必须按次显式开启）追加一段明确标注、完全可选的解读。在 `-journey` 上，它对该 Journey 自己的疑似问题清单做优先级排序和串联解读（不会凭空发现清单之外的新问题，除非明确标注这是模型自己的阅读判断），外加对逐轮工具调用序列的叙述性解读。在 `-compare` 上，是一句话结论、一张"候选根因 | 直接证据 | 置信度（高/中/低）| 改进建议"表 + 一句话因果链、对逐轮工具调用序列的叙述性解读、以及一段"VMR 看不到什么"的诚实声明——如果定位到了分叉点，还会追加第二次、单独缓存的调用，只解读*为什么*两侧可能在这里分道扬镳（标置信度的推测，绝不判断哪一方更好）。置信度分档写死在 prompt 里：只有能在证据表或原文节选里指认出具体证据的候选才能标"高"，仅凭排除法/直觉的必须诚实标"低"（但仍会列出，不会因为不确定就不提）。喂给模型的只有上面的规则事实，加上有边界的原文节选，不是完整对话正文，且 prompt 明确要求不得编造给定证据之外的数字，"节选里没提到"也不能被模型当成"确实不存在"来断言。加 `-llm-dry-run` 只打印证据包大小估算并退出，不实际调用。结果会缓存在 `stories/.llm-cache/` 下（key 同时包含 journey id、证据内容与所用模型——换 `-llm-model` 不会误用别的模型的缓存结果）；任何失败（地址不可达、非 2xx 等）只会跳过这一节并打印警告，报告的其余部分不受影响。

完整设计（背后的内容寻址模型、lineage/compaction 检测原理、九项行为剖面指标 + 模型使用/切换、已知盲区）：`docs/VirtualModelRouter_Design_v4_Analytics.md`。

## 请求图片自动降采样

可选，默认关闭。开启后，超过设定长边的内联 base64 图片附件会被等比缩小、转 JPEG 再发上游——为截图密集的 agent 工作流削减 vision token 成本。只处理请求，不碰响应，不抓远程 URL；GIF（不论单帧多帧）与解码失败一律原样透传（fail-open）——GIF 为什么一律不缩放，见设计文档 Part 1 §7。

图片检测始终开启，与这个开关无关：不论该虚拟模型是否开启了降采样，请求里的每张内联图片都会做一次廉价的头部解析（格式/宽高/字节数，不解码像素）并写入审计日志，所以即使某个模型关闭了压缩，`vmr-report.json` 里的 `images`/`images_compressed` 字段也能反映真实的图片流量。

```yaml
image_downscale: 512   # 全局长边像素上限；0 或缺省 = 关闭
image_cache_ttl_days: 7   # 降采样结果缓存的失效期（缺省 7 天，见下）

models:
  coding:
    image_downscale: 1024   # 覆盖全局值，只对这一个虚拟模型生效
    endpoints: [...]
  cheap:
    image_downscale: 0      # 显式关闭：即使全局开启，这个模型也不降采样
    endpoints: [...]
```

### 模型级覆盖

每个 virtual model 都可以设置自己的 `image_downscale`，优先级高于全局值；不写则继承全局设置。`image_downscale: 0` 在模型层面是一个明确的"关闭"指令，即使全局开着也照样关——因为"没写"和"写了 0"含义不同（前者继承，后者强制关闭）。

### 降采样结果缓存

同一张原始图片、同一个目标像素上限，第一次处理后会把结果（JPEG 字节）缓存到磁盘。缓存 key 是**内容哈希 + 目标尺寸**——文件名为 `<原始字节的 sha256>-<maxPx>.jpg`，所以同一张图降到 512px 和 256px（不同模型的不同覆盖值）是两个互相独立的条目，绝不会串（目录取配置项 `image_cache_dir`，见下）。后续请求命中同一张图片时直接复用缓存字节，不再重新解码/缩放/编码。这带来两个好处：省 CPU（agent 场景每轮都会把完整对话历史连同图片重发一遍），以及避免破坏上游的 prompt cache——上游的缓存是按精确字节/token 匹配的，同一张图片如果每次都重新编码，输出字节可能有细微差异，足以让上游缓存失效；用缓存后的完全相同字节，上游缓存才能命中。缓存条目按"最近一次被命中"的时间做 TTL 淘汰（`image_cache_ttl_days`，缺省 7 天；命中会刷新计时，长对话里反复引用的图片不会被提前清掉），淘汰扫描搭在缓存目录访问上触发，不额外起定时器。

### 审计目录和缓存目录到底落在哪

两者都是 config 字段——

```yaml
# log_dir: ~/.vmr/logs                  # 审计 JSONL 目录；有设置就原样使用（~/ 会展开）；改动需要重启才生效
# image_cache_dir: ~/.vmr/image_cache   # 降采样缓存目录；规则同上；随热重载即时生效
```

——有设置就原样使用（开头的 `~/` 展开为 home 目录），否则落在持久的 `~/.vmr/logs`/`~/.vmr/image_cache`，再否则（解析不出 home 目录）退到系统临时目录下的 `vmr_logs`/`vmr_image_cache` 子目录，最后才是二进制所在目录的 `./logs`/`./image_cache`。默认持久化是刻意的：macOS 会清理约 3 天未访问的临时目录条目，会静默删掉审计数据——而它是 `vmr report` 唯一的数据源。想知道实际解析出来的路径，直接跑 `vmr check -c config.yaml log` / `vmr check -c config.yaml cache`（不带参数的 `vmr check` 与启动摘要也会打印），不用真的启动服务。`vmr.sh` 只查询 `vmr check log` 来定位 server log 落点，而不是在 bash 里另写一份猜测逻辑——dev 模式和 `service install` 因此不会对"数据到底存在哪"这件事产生分歧。两个目录都没有对应的环境变量——想从环境注入，在 `log_dir`/`image_cache_dir` 里显式写 `${VAR}` 即可。

## CLI 与端点参考

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic Messages 协议入口（流式 + 非流式） |
| `POST /v1/responses` | OpenAI Responses 协议入口（流式 + 非流式）；需要一条 `protocol: openai-responses` 的端点 |
| `GET /v1/models` | Virtual Model 列表（两种 SDK 均可解析） |
| `GET /admin/status` | 进程身份（pid/listen/版本/配置路径/uptime）、配置是否过期的判定、热重载结果、逐端点健康、Sticky Model 注册表大小、并发指标，以及——对每个配了 `quota:` 的 provider——一段展示实时消耗的 `quota` 段（见上文"额度感知路由"），含某个端点当前是否正被一次后台恢复探测占着单飞名额（仅 loopback）——下文的 `vmr status` 是这份数据的 CLI 前端 |
| `vmr start -c config.yaml [-audit=false]` | 前台运行路由器（Ctrl-C 停止）；`-audit=false` 关闭 JSONL 审计日志（默认开启）。`./vmr.sh start` 是它的后台托管版本，也是脚本唯一接管的一条命令——前台/开发场景直接跑这条 |
| `vmr check -c config.yaml` | 校验配置、跑一致性扫描（`api_key` 缺失、重复端点……），打印路由表、Key 状态与每个 provider 的生效代理——有问题的取值带内联 ⚠️，末尾附 `=== Failed ===` 汇总。末尾带 `log`\|`cache` 参数时改为只打印那一个生效目录（`log_dir`/`image_cache_dir` 缺省后的值）——`vmr.sh` 内部就是问这个 |
| `vmr status -c config.yaml` | 渲染运行实例的身份（pid / listen / uptime / 配置绝对路径）+ 健康与并发占用。`-addr host:port` 改成直接查那个端口上的实例、完全不加载 config——本机跑着多个实例、或者你手上根本没有那份 config 时用它；`-brief` 只打一行 Tab 分隔的摘要（`./vmr.sh ps` 就是拿它拼表） |
| `vmr report [-c config.yaml] [-o dir] [-lang en\|zh] [-currency CODE] [-report-config report.yaml] [glob...]` | 审计日志（明文或 `.zst`）→ 用量统计 + 会话/工具分析 + 逐请求特征（`vmr-requests.json`）+ 错误/截断索引（`vmr-requests-failed.jsonl`/`.md`）+ 详单（`-details=false` 关闭）；只要定价数据能解析出结果就渲染 §2 成本估算章节——内置标准表始终生效，`-c` 指定的 config.yaml 若能读到，会在其上叠加账号覆盖（见上文[成本估算与定价](#成本估算与定价)）。`glob` 是可选的——完全不写就对着 `-c config.yaml` 自己的 `log_dir` 出报表。输出语言默认英文，`-lang` 或 `report.yaml` 的 `language:`（见上文[输出语言](#输出语言)）可切换成中文。`-currency` 决定 $ 列的展示币种，和它实际计算时用的币种相互独立（见上文[成本估算与定价](#成本估算与定价)） |
| `vmr story [-journey <id\|id前缀\|通配符>[,...] \| -render-all \| -compare <id1,id2> \| -corpus] [-lang en\|zh] [-report-config report.yaml] [glob...]` | 把一次 Agent 任务的完整执行过程还原成可读的 Markdown 叙事（见上文[Agent 任务叙事重建](#agent-任务叙事重建-vmr-story)）；不带参数列出候选任务及其 id，`-journey` 接受逗号分隔的多个 id/id 前缀/shell 风格通配符（`*`/`?`/`[...]`），匹配到的全部渲染——只匹配到一个就直接渲染，多个就走 `-render-all` 同一条批处理路径（`-render-all` 本身是一次批量渲染全部候选），`-compare id1,id2` 对比两个已渲染任务的行为剖面（含分叉点检测），`-corpus` 计算跨全部候选的语料级统计。`-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]` 可在只匹配到一个 journey 的 `-journey` 或 `-compare` 上追加可选的 LLM 解读小节（不支持 `-render-all`/`-corpus`，也不支持多匹配的 `-journey`）。`-lang`/`report.yaml` 控制输出语言，与 `vmr report` 一致；`glob` 同样是可选的（见上文"大多数情况不需要指定输入文件"） |
| `vmr version` | 打印本二进制的构建标识（git SHA，脏工作区加 `-dirty` 后缀，外加 commit 时间与 Go 版本）。不需要 ldflags：Go 默认把 VCS 状态压进任何仓库内构建的二进制，运行时读出来即可。运行中实例的同一个值在 `/admin/status` 与 `./vmr.sh ps` 的 VERSION 列里，可以直接对比"那个进程跑的是不是我刚编的这版" |
| `vmr diagnose [-c config.yaml]` | 比 `check` 的静态预览更进一步：对每个 provider 做 DNS/TLS/代理连通性检查，再发一次真实的最小请求到每个配置的端点，要求对方原样回显一个一次性 token（并发执行，`-test-timeout` 控制单项超时，默认 15s）——拿到 200 但没回显这个 token 会标成警告而不是直接判通过，用来抓那种网关/中转层拿缓存或兜底响应假装成功的情况——并给出标注了检测结果的路由顺序预览（`-no-test-routing` 跳过真实请求，`-json` 供脚本消费；只要有检查失败就以非零退出码结束） |
| `vmr replay -provider NAME <audit.jsonl>` | 用 vmr 自己构造请求的同一条代码路径，从一条审计记录重建并重发请求——`-dry-run` 只打印不发送，`-record path` 把这次回放的结果也写成一条独立的审计记录，`-model`/`-protocol` 可覆盖记录里原有的值，`-stream true\|false` 强制开关流式，`-max-time` 限制上游等待时长。选择要回放哪条记录：`-detail file`（`vmr report` 产出的 `details/*.json` 文件，不用数行）、`-ts <timestamp>`（匹配 `vmr-requests.json` 或原始审计日志里的 `ts` 字段）、`-line N`（默认取文件里最后一条）——三者互斥 |
| `./vmr.sh start\|stop\|…` | dev 模式生命周期（自己监督） |
| `./vmr.sh ps` | 列出本机所有 vmr 实例（不限于本 checkout）：pid、监听地址、uptime、模型数、配置文件绝对路径。三步各司其职——`pgrep` 找进程、`lsof` 找它占的端口（监听地址只写在那个进程的 config 里，命令行上没有）、再用 `vmr status -addr … -brief` 问实例自己要其余信息。缺 `lsof`、或进程不应答 `/admin/status` 时，退化成只有 pid + 命令行上那个 `-c` 参数的行并标注原因，不会把实例整个漏掉 |
| `./vmr.sh service install\|uninstall\|start\|…` | init 系统服务（launchd/systemd：崩溃重启、登录自启） |
| `./vmr.sh <上表任一命令> [参数]` | 脚本不认识的子命令一律原样转发给二进制（`./vmr.sh check`、`./vmr.sh diagnose`、`./vmr.sh report …`），不是白名单——二进制新增的子命令当天就能用。转发时做两件事：**回到调用者原来的目录**（相对路径、glob、`-o` 的含义与直接跑 `vmr` 完全一致），以及**没写 `-c` 时补上脚本所在 checkout 的 `config.yaml` 绝对路径**——前提是这个子命令确实定义了 `-c`（`start`/`check`/`status`/`diagnose`/`replay`/`report`/`story` 都算——`report`/`story` 也在内，因为不给 glob 时两者都要靠它解析 `log_dir`，见上文"大多数情况不需要指定输入文件"）。前台 `vmr start` 是唯一被脚本遮蔽的命令——脚本的 `start` 是后台版，要前台就直接跑 `./vmr start -c config.yaml` |

经路由的响应带 `X-VMR-Endpoint`（实际命中端点）、`X-VMR-Attempts`（尝试次数）与 `X-VMR-Route-Reason`（为什么选中它：`pick=order|quota|sticky`、`eligible=N/M`，以及真正发生过时才出现的 `cooldown=` / `conditions=` / `ctx_fallback=1`）；只要有失败过的尝试，再带一个 `X-VMR-Failover`（如 `deepseek/deepseek-v4:429, minimax/m2:500`，构建/网络失败记 `:err`）——**请求成功时也带**，所以"这次是第三次 failover 才成功的"在终端里直接看得见，不用事后翻审计日志。

```bash
# 怀疑配置有问题——先诊断一遍，而不是对着日志里的 401 干瞪眼。
./vmr diagnose -c config.yaml

# 某个请求失败了，先看看 vmr 本来会发出什么，不真的发送。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# 同一个请求，真的发送，把上游响应打印到 stdout。
./vmr replay -c config.yaml -provider openrouter \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# 是在 vmr-requests.md / vmr-report.md 里找到的那条失败请求？
# 直接指向它的 details/*.json，不用数行号。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -detail out/details/20260713-153042.100_coding_z-ai-glm-5.2_error.json

# 或者用 vmr-requests.json / vmr-report.md 里看到的精确时间戳来定位。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -ts 2026-07-13T15:30:42.100+08:00 \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"
```
