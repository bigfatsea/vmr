<!-- Ver 2026-08-02 13:30, by Sonnet 5 -->

# vmr — 用户指南

[English](UserGuide.md) | 简体中文

完整的配置参考、协议行为细节、CLI 说明。如果只是想先跑起来，先看 [README](../README.zh.md) 的快速开始——跑通之后再回来看这份。

## 配置

`providers` 是一个扁平列表——一个账号一条，不管它实际讲两种入口协议（`openai` / `anthropic`）里的几种。`base_url` 本身按协议分 key，所以一个账号的两个协议面写在同一条里，不需要重复声明两遍。`models` 按虚拟模型名分组；`endpoints` 列表里每一条自带 `protocol` 字段，所以同一个虚拟模型名下可以同时挂一条 openai 协议的候选列表和一条 anthropic 协议的候选列表——两个入口各自独立可达。一条 endpoint-group 的 `models:` 列表可以写多个上游模型名，每个展开成独立的、各自健康跟踪的候选，共享这条 entry 的其余字段：

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

**启动/热重载的合理性告警**：除了上面的严格解析之外，vmr 在每次启动和每次热重载（fsnotify 或 SIGHUP，包括服务管理器自动重启触发的那次）都会顺带跑一遍*操作性*检查——跟 `vmr check`/`vmr diagnose` 打印的是同一套——命中的每一条都会打一行 `WARN config check: ...` 日志。最值得知道的一条：`api_key` 拼错或没导出的 `${ENV_VAR}` 展开成空串在 YAML 层面完全合法，所以加载和热重载都不会报错——这个 provider 下的每个请求会一直 401 到有人发现为止。这个告警是唯一的信号；它从不阻断启动或重载，因为 Check() 发现的问题按定义是"能跑但可能不对"。

**单请求内存预算**：三个各自独立、各自合理的缓冲上限乘起来看：`max_request_body_mb`（缺省 8MB，入站请求体）、响应归一化缓冲（8MB，防止需要缓冲而非直接流式转发时被失控上游撑爆）、审计响应副本（16MB，限制审计记录保留一条响应正文的上限，比归一化缓冲留了更多余量——审计副本被截断丢的是 `vmr report`/`vmr story` 需要的信息本身，不只是"聪明处理"）。三者都是按当下 ~1M-token 上下文窗口（约 3-4MB 字节量）留约 2 倍余量估算出来的，不是拍脑袋的整数。最坏情况大致是三者之和——约 32MB——每个在途请求都可能吃到这么多，还没算上 `bytes.Buffer` 扩容期间的额外开销。`max_concurrency` 缺省不限，所以这个数字唯一的上界就是同时涌进来多少个请求。单用户本地实例上这纯属背景噪音；共享实例上，请把 `max_concurrency` 设成一个具体数字，而不是让这四个数字的乘积保持无界。

**上游代理——只认显式配置，默认关闭**：`http_proxy`/`https_proxy` 只声明代理服务器**在哪**，本身不会替任何 provider 打开代理。一个 provider 是否真的走代理，完全由它自己的 `proxy: true`/`false` 决定（缺省 `false` = 直连，没有全局默认可继承——每个 provider 独立、显式决定）；只有它是 `true` 时，才按 base_url 的 scheme 选用 `https_proxy`/`http_proxy`。**推荐写法**：只给个别需要代理的 provider（通常是海外厂商）写 `proxy: true`，其余不写——单点意图，新增 provider 默认直连，不会被意外牵连。**代理环境变量被有意忽略**——隐式旋钮悄悄改变流量走向，最容易被忽略、排障时最难想到；要引用它就显式写 `https_proxy: ${HTTPS_PROXY}`。`proxy: true` 但没配对应的代理地址是校验错误（拒绝加载），不是运行时惊喜。`vmr check` 与启动摘要逐 provider 打印生效代理（凭证掩码）。YAML 1.2 语法：写 `true`/`false`，不能写 `on`/`off`。

**base_url 必须自带版本号**：vmr 在初始化时预计算每个 provider 的完整上游 URL——直接把协议的裸路径（OpenAI Chat Completions 为 `/chat/completions`，Anthropic 为 `/messages`，OpenAI Responses 为 `/responses`）拼在 `base_url` 后面，不做任何归一化或重叠检测。所以 `base_url` 必须已经带上该 provider 自己的完整 API 版本号，不管它叫什么：`https://api.example.com/v1`、`https://api.minimaxi.com/anthropic/v1`、`https://ark.example.com/api/coding/v3`。这条规则的原因是：不是所有 provider 的 OpenAI/Anthropic 兼容面都叫 `v1`——比如火山引擎 coding plan 的 OpenAI 端点版本号是 `v3`——所以 vmr 不会替你猜版本号；写错了会立刻在你写的这个 base_url 上报 404，而不是被悄悄"纠正"成别的样子。URL 在配置加载时一次性计算并存入 Endpoint，adapter 直接使用，不在每次请求时构造或归一化 URL。

**`role_map`——按 endpoint-group 做 role 改写**：有些 OpenAI 兼容 provider 会拒绝它上游不认识的 role——典型场景是 OpenAI 为 o1/o3 系列模型引入的 `developer` role，部分网关（如 DashScope/千问）会直接拒收。在 `models.<name>.endpoints[]` 的某条 entry 下写 `role_map: {developer: system}`，vmr 会在请求发往上游之前，把顶层 `messages` 数组（若这条 entry 是 `protocol: openai-responses`，则是顶层 `input` 数组）里匹配到的 `"role"` 值原地改写，客户端完全不用改。它是一个纯粹的旧→新字符串映射，只作用于列出的那几个 role——请求的其余每一个字节（键序、空白、未知字段、消息内容）原样透传，跟 `RewriteModel` 改写 model 字段用的是同一套字节级拼接手法。这个开关挂在 endpoint-group 一级，不是挂在 provider 或整个虚拟模型上——因为同一个账号可能背靠好几个虚拟模型、好几族不同的上游模型，不见得都要用同一套改写规则；某个模型如果从不发送被映射的那个 role，配不配 `role_map` 对它没有影响。不配置（或留空）`role_map` 的 entry 保持默认行为：所有 role 原样通过。

### 环境变量

vmr 涉及的环境变量全部在此——除此之外不读任何环境变量：

| 变量 | 作用 |
| --- | --- |
| config.yaml 里引用的任意 `${VAR}` | 加载配置时展开（每次热重载重新展开）；未设置的变量展开为空串。这是环境变量进入 vmr 的**唯一**通道——API Key、可选的 `${HTTPS_PROXY}`、可选的目录路径，都走这一条。 |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` / `ALL_PROXY` | **被忽略。** 代理只认上面的 `http_proxy`/`https_proxy` 配置项；要用环境变量的值，在 config 里显式引用 `${HTTPS_PROXY}`。 |

目录（`log_dir`/`image_cache_dir`，见下）和代理都是 config 字段，不是环境变量——理由一致：vmr 往哪写、怎么连网络，不该依赖隐式的运行环境。service 模式下，`vmr.sh service install` 会把 config 引用的全部 `${VAR}` 从当前 shell 快照进 `~/.config/vmr/env`（0600，已存在则不覆盖）——不需要再注入任何别的东西，二进制自己读 config。

## 透传与归一化

**原则：直连等价**。客户端经 vmr 收到的内容——字节、头部、传输节奏——与直连供应商一致。仅有的偏离：

- `model` 字段——请求侧改成上游真实名，响应侧改回虚拟名（SDK 假设 `response.model === request.model`）。请求侧改写是只针对顶层 `model` 值的字节 splice：其余每一个字节——键序、空白、未知参数——都按客户端原文逐字节到达上游；
- 两个 **MiniMax-M3 专属修复**，各自只在确认命中其确切形态时触发：剥 content 里的内联 `<think>…</think>` 推理（不剥会持久化进历史，把模型锁进反馈循环），以及剥 MiniMax 某个思考模式下（这个模式不写 `<think>` 标签）以纯文本输出的「Thinking Process:」思考段。两个修复都靠一个字面量措辞守卫触发——响应内容仍具备泄漏思考段的形状（编号小节、篇幅长）但没命中守卫时，会在 `attempts[].norm` 打一个 `thinking_process_pattern_detected` 标记：字节不改动，纯粹用于在剥离规则因措辞变化而失效之前先观测到征兆；
- `data: [DONE]` 哨兵——**仅** OpenAI 协议流式且上游未发时补；绝不重复，绝不注入 Anthropic 流。

流式是真的：事件到达即转发；只有检测到思考形态才缓冲，`</think>` 闭合后立即恢复实时流。带 `Content-Encoding` 的压缩体零变换直通。上游 3xx 重定向绝不跟随——301/302/303 的原始状态、`Location` 头、体原样到达客户端，和直连一模一样（`http.Client` 默认策略会把 POST 301/302/303 静默改写成 GET，这会破坏字节级保真）。响应头与请求头同一策略——除 hop-by-hop 外全部透传；错误响应连状态、头（含 `Retry-After`）、体原样返回。每个请求实际生效的归一化记录在审计日志 `attempts[].norm`，上游与客户端之间的任何字节差异都有逐请求的解释。

正因为透传是字节级的，三个协议任何一侧新增的请求/响应字段都不需要 vmr 改代码就能到达上游或客户端——这正是透传的意义所在。vmr **不做**的事：只路由 `POST /v1/chat/completions`、`POST /v1/messages`、`POST /v1/responses` 三个入口，其他 OpenAI/Anthropic surface（`/v1/realtime`、`/v1/images`、`/v1/audio` 等）不在范围内——这类需求请直接把客户端指向供应商自己的 base URL。`openai-responses` 协议面比另外两个更新、覆盖也更窄：截至本文撰写，只有 DeepSeek 和 OpenRouter 提供了 Responses 兼容端点（MiniMax 尚未支持），且两家都强制无状态——`store: true` 或非空 `previous_response_id` 会被上游直接拒绝，不是 vmr 拦的（vmr 从不检查或剥离客户端字段，只负责路由）。如果你对着一个真支持这些字段的上游使用 `previous_response_id` 或手动回放加密 reasoning item，注意 vmr 的 failover 可能把同一段对话的后续轮次路由到创建那份状态的端点之外——下文的 Sticky Model 能降低这种情况的概率，但不能从结构上根除它。

## 故障切换与健康

上游失败即按端点列表顺序逐个尝试，直到成功或全部耗尽（`max_attempts` 可选设上限）。健康完全由失败驱动，按响应逐条分类，确保每次失败既受到匹配的惩罚，也得到正确的"还要不要继续切换"的判断：

- 网络/5xx：短冷却指数退避；401/额度耗尽/模型不存在/某个网关或中转层报告了它自己的转发失败（而不是请求本身有问题）：长冷却；429/503：尊重 `Retry-After`；
- 400 类**客户端**错误——确实是请求本身写错了——直接返回，不切换、不冷却：换哪个端点都会被同样拒绝，切换解决不了任何问题；
- **内容合规拦截**切换下一家，但**不惩罚**被拦端点——它只是拒绝了这一条请求，并没有坏。

**冷却端点如何恢复**：端点冷却一到期，vmr 立刻在后台发一个专门的轻量探测请求（受 `probe_timeout` 约束，缺省 15s），而不是让下一个真实请求自己去踩一脚。真实流量在端点被确认恢复之前**完全不会碰到它、也不会等它**——探测没出结果之前，请求照样路由到下一个候选，不管探测本身要跑多久。探测会要求模型原样回显一个一次性 token，所以网关返回一个缓存/兜底的"假成功"不会被当成恢复。

```yaml
probe_timeout: 15s      # 一次后台探测的时间上限
```

全部候选失败时原样返回最后一次上游错误。流式只在首字节前切换。

## 条件路由

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

## Sticky Model（会话亲和）

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

## 审计日志与用量报表

默认开启：每个请求一行 JSONL，双层记录（调用方↔vmr 与每次 vmr↔上游尝试）、凭证掩码、生效的归一化清单，以及请求内联图片的元数据（格式/宽高/字节数，以及是否触发压缩/是否命中缓存——不论该虚拟模型是否开启了图片压缩，都会采集）。body 一律原样全量记录，不设审计侧截断上限（上面的 `max_request_body_mb` 只管入站请求体大小，与审计记录无关）。每次上游尝试同时携带一个人类可读的 `endpoint` 标签（`protocol:provider:model`）和拆开的三个结构化字段（`protocol`/`provider`/`model`），并在自由文本 `error` 之外新增一个类型化的 `error_class`。凭证掩码默认覆盖 `Authorization`/`X-Api-Key`/`Api-Key`/`X-Auth-Token`/`Cookie`/`Set-Cookie`/`Proxy-Authorization`；如果客户端自己发了一个 vmr 不认识的自定义鉴权 header（如 `X-Custom-Token`），需要配 `extra_redact_headers`（见上文"配置"）才会一并打码，否则会明文落进审计文件。

每条记录还带一个 `facts` 对象——vmr 自己对这条请求的路由前判断（`has_image`/`has_tools`/`estimated_tokens`），和路由当时用来选端点的值完全一样，原样落盘，不是事后重新算的。它是这条请求的兄弟字段，不是请求本身的一部分，所以记录下来的请求体依旧对客户端原始请求保持字节忠实。请求在路由开始之前就被拒绝时（鉴权失败、JSON 解析不了）这个字段整体不出现，不是一个全零值的对象。

```bash
./vmr start -c config.yaml                 # 写入 config 的 log_dir（`vmr check -c config.yaml log` 可核对）
./vmr start -c config.yaml -audit=false    # 关闭
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.jsonl（明文/.zst 混着传也行）
```

`vmr report` 同时统计 tokens 与字节（上游不回报 usage 时以字节兜底），Markdown 按九个编号章节组织，每章回答一个运维问题：`§0` 摘要（headline 数字 + 最多 3 条自动亮点——缓存效率低、工具 schema 浪费、端点异常）、`§1` 成本与 Token 经济（缓存命中/fresh/cache_write/reasoning 拆分，按模型缓存效率，按角色的消息字符/预估 token 占比）、`§2` 成本估算（一旦加载了定价配置就渲染，见下文"成本估算与 `pricing.yaml`"；按模型/端点/客户端各一张表，末尾还会原样折叠嵌入本次实际使用的定价配置，让报告的 $ 数字即使在 `pricing.yaml` 之后被改过也能追溯）、`§3` 可靠性（端点可用度/错误率、错误类别拆分，因为 openai/anthropic 两个协议面各自独立路由，两张表都按协议再拆一次，外加每小时错误数图表）、`§4` 延迟与吞吐（按模型、按端点的 ttft/耗时分位数，都按吞吐量降序排列，各自带样本量，n<20 标 `⚠️low-n`）、`§5` 负载分布（按虚拟模型、按工作负载类——交互 vs 定时脚手架——、按端点、按客户端——后两张表还带每请求输入/输出 token 分位数——外加每小时和每日的请求量/输入 token Mermaid 图表）、`§6` 会话与任务（只列 interactive 会话，按 Chat User 分组；单发定时会话改放进请求详单里，见下文）、`§6.5` Sticky 有效性（见下）、`§6.6` 端点性价比（见下）、`§6.7` Compaction 还原（本期每一次独立的历史压缩 LLM 调用：链接到哪个会话、tokens_in→tokens_out、保留比，以及一份规则筛出的被吞掉内容样例——不靠 LLM 判断，只呈现可观察的事实）、`§7` 效率与浪费（自动发现 + 每个声明工具集的完整"已调用/从未调用"明细）、`§8` 指向请求详单的链接。每张表都控制在几列以内；分位数都是每个桶的真值——每个桶在单趟遍历里直接收自己的原始样本、自己算 p50/p95，不做跨桶近似（合并"已经算完的"桶算不出真百分位，因为原始值早被释放了）。`⭐` 标记衍生/预估指标（相对上游原始返回值而言）。每小时/每日活跃度和每小时错误数都用 Mermaid `xychart-beta` 图表渲染。

运行进度写到 stdout，每一行都带 `yyyy-MM-dd HH:mm:ss.SSS` 时间戳，方便看清每个阶段实际花了多久：会话分析最先跑（按输入文件并行处理——在天数多的语料上这是耗时最长的单一阶段——过程本身不打印逐文件的进度行），然后聚合与详单导出合并成一趟：一个文件一行 `[i/N] <path>  done: M records (Ts)`，详单渲染在自己的 worker 池上跑，与喂给它数据的文件扫描并发进行——因为一条记录的详单页面只依赖它自己的内容，跟其他记录累积出来的任何东西都无关。JSON（`vmr-report.json`）是二次开发（图表/Dashboard）的数据源——Markdown 里只展示 Top-5 或做了折叠的明细，JSON 里都是全量。

**成本估算与 `pricing.yaml`。** `-pricing pricing.yaml` 显式指定一份定价配置；不加这个参数时，`vmr report` 会自动读取当前目录下的 `./pricing.yaml`（文件不存在就安静跳过，不报错，也不显示 $ 估算）。价格按"provider+model"配置，跟协议无关——同一个上游账号/模型不管走 vmr 的 openai 面还是 anthropic 面成本都一样，一条规则就够。四个每百万 token 价格字段（`in_fresh_per_1m`/`cache_read_per_1m`/`cache_write_per_1m`/`out_per_1m`——只有第一、三、四个会计入 $ 估算，缓存命中按各厂都当免费处理）既可以写纯数字（用文件顶层的 `currency`），也可以写带货币前缀的字符串（`"USD0.14"`、`"jpy 1.2"`，大小写不敏感，前缀和数字之间的空格可有可无），换算靠顶层的 `exchange_rate` 列表（成对给出等值货币，例如 `{USD: 1, CNY: 7}` 表示 1 美元 = 7 人民币；`USD→EUR→CNY` 这种链式换算即使没有 USD 直连 CNY 的条目也能算出来）。价格用了一个 `exchange_rate` 里没定义、又不是顶层 `currency` 本身的货币符号，是配置加载阶段的报错，不会静默按 0 处理。同一个 provider+model 可以配多条规则，各自用可选的 `date_range: [start, end]`（`"2026-07-01"`，yyyy-MM-dd）和/或 `hour_range: [start, end]`（`"22:00"`，HH:MM——起始晚于结束表示跨过午夜）限定生效时段，用来表达随时段变化的价格（比如低谷折扣、促销窗口）；`vmr report` 按文件里的先后顺序取第一条时间窗口覆盖该请求时间戳的规则，所以更窄/促销性质的窗口要写在它要覆盖的兜底规则前面。完整的、带注释的示例见仓库自带的 `pricing.yaml`。

`vmr report` 还能读懂 Agent 工作负载——全部离线、纯规则、不调用 LLM（方法与实证见 `docs/VirtualModelRouter_Design_v4_Analytics.md` §2.1）：

- **会话 → 任务 → 轮次分组**。每轮重发同一段渐增对话的请求以首条非 system 消息做指纹（Claude Code 的 `metadata.user_id` 存在时优先），按最长公共前缀成链——多个 Agent 会话即使在时间上互相穿插也能干净分开。任务边界来自 Traceparent trace-id 变化与增量中的新用户指令，两个信号互为交叉验证。Compaction 调用被识别并双向链接，会话与其压缩后的续接体串成同一条线程。
- **`vmr-requests.jsonl`** —— 每请求一行特征（会话/任务/轮次、trace 与 chat id、请求形态、`heartbeat` 等标签、当轮 tool 调用、finish_reason、"ok 但截断"标志、含 reasoning 的 token 细分、增量大小、最新指令），jq / DuckDB / pandas 直接可用。
- **Sticky 有效性（§6.5）⭐** —— Sticky Model 存在的唯一理由是让上游 prompt cache 保温，这一节是它有没有兑现的证据：同一会话内，落回**上一条请求所用端点**的请求 vs 换了端点的请求，比较两组缓存效率。按结果（端点连续性）而非按机制度量，所以 sticky 指针命中却落到一个冷端点照样算切换。会话首条无前驱，计数但不入组；任一组带 usage 的样本 < 20 条时只出表、不下结论。**不解释切换原因**——sticky_ttl 到期、端点冷却、条件路由淘汰、该模型没开 sticky，事后无法区分。再按虚拟模型拆一张表：sticky 是按虚拟模型配的，那才是能动手的粒度。

- **端点性价比（§6.6）⭐** —— 不是"这个端点花了多少钱"（§2 已经答了），而是"单位产出的代价，以及它的失败让你多等了多久"：成本/1M out token、成本/成功请求、失败尝试数、**失败尝试累计墙钟时间**。一个单价便宜但经常失败的端点不便宜，但这在按端点的花费列里看不出来——钱记在最终成功的那一家头上。**只记时间不折算成钱**：失败尝试拿不到 usage，厂商通常也不对失败请求计费，给它标金额会是编造。

- **工具使用报告（§7）** —— 按声明工具集分组：声明的工具 vs **当轮实际调用**的工具（从响应中提取,历史重发绝不重复计数），外加"声明但从未调用"清单——两者都折叠进每个工具集自己的 `<details>` 块（numbered list + 字母序，自然让 `feishu_*` 同前缀聚类，避免 60+ 工具的 schema 撑爆文档）——及其每请求字节成本，为从 Agent 配置里裁掉没用的工具提供直接依据。

`vmr report` 还会把每条记录导出为一个人类可读的 Markdown 详单**外加一个同名 JSON 文件**（原始 record，方便 jq/脚本查询），落在 `{out}/details/` 下，用于深挖单个请求：头部一行定位（trace / chat user / tools，取值加粗），紧接一段 `VMR 路由前判断`，读上文提到的 `facts` 对象——只列出**实际探测到**的能力（`image`、`tools`，各自渲染成一个反引号包裹的小标签，都没探测到时显示"无"），加预估 token 数——该记录没有 `facts` 时这一段完全不出现，再是**完整消息列表**（每条消息默认 `<details>` 折叠；本轮新增的消息在 summary 上加 🆕 前缀，末尾追加一行 `🆕 本轮增量（相对上一轮,+N 条,#1–#M 为历史上下文）` 汇总）、每次上游尝试的 headers 与 body 字段全量对照（变化项以 emoji 标记：🟢 新增 / 🔴 删除 / 🔶 变化）——若该次尝试剥离了 `<think>…</think>` 推理块，还会展示剥离前的完整内容及对应原始 SSE（字段缺失的旧格式日志显示"未保留"提示）、客户端响应部分把 SSE 流重组成模型实际输出并保留原始事件全文。文件名以零填充时间戳开头，按名字排序即按时间排序。加 `-details=false` 可关闭详单导出。

`vmr-requests.md`（与 `vmr-report.md` 并列，在 `details/` 上一级）是一份纯索引：每个分组一条 `## Chat User: <key> · N 会话 N 任务 N 轮`（或 `## 定时任务 · <class> 单发会话 × N`），带一行摘要引用块和指向该分组自己那份完整详单的链接。真正的 **Chat User → Session → Task → Turn** 展开——每个会话一个 `## sNN · <时间> · N 任务 N 轮` 标题，每个任务一个 `### tNN · <时间> · N 轮` 标题，带首条消息的引用块和轮次表（`轮 / 时间 / msgs / finish / dur / ttft / fresh/cached/out / cache-eff⭐ / 文件`；所有时间戳统一转 UTC+8，不管原始记录自带什么时区）——只存在于对应的独立文件里，不会在索引里重复一遍。独立文件的命名：每个真实 `client_key_tag` 对应 `vmr-requests-<tag>.md`，没有标签的会话归到 `vmr-requests-unresolved.md`，每个定时任务类别对应 `vmr-requests-cron-<class>.md`（`heartbeat` 这一个的文件名固定是 `vmr-requests-cron-hartbeat.md`；以后新增的定时任务类别照 `-cron-<class>` 这个模式来）。单发的定时会话（heartbeat/dream_diary——只有一次请求、没有真实来回）不管是哪个客户端发起的，永远归到它对应类别的 cron 文件里，这样一堆近乎重复的轮询请求既不会淹没真实对话，也不会同时出现在两种分组下；轮次数大于一的定时会话（真正的多步 cron 任务）则作为普通会话卡片挂在自己调用方名下。索引文末的 `# 全部请求（时间序）` 依旧是一张不分组的时间序全量表。每一处"文件"列都同时链接到 Markdown 详单和同名的 JSON 详单。

**输出语言。** `vmr report`/`vmr story` 默认输出英文（上文这些示例展示的是切到中文之后的样子）。在当前目录放一份 `report.yaml`（写 `language: zh`）即可切换成中文，或者在命令行上加 `-lang en|zh` 只影响这一次运行——`-lang` 优先级高于 `report.yaml`。`report.yaml` 是独立的一份小文件，跟 `config.yaml` 完全无关：不含任何敏感信息，是可选的，跟 `pricing.yaml` 一样从当前目录自动加载（`-report-config path` 可以指向别的路径）。这个开关只影响 Markdown 文档的文字——`vmr-report.json`/`journey-*.json`/`compare-*.json` 不受影响：里面的叙述性字段（比如 `efficiency[].finding`、`compare-*.json` 的 `rows[].label`）不管 `-lang` 是什么，永远是英文，写脚本解析这些 JSON 不需要考虑报告是用哪种语言生成的。

**多调用方场景。** 如果一个 vmr 实例被多个调用方共用（队友、另一个 Agent、CI 任务），想在事后统计里把各自的用量分开看，就给每个调用方在 `api_keys` 下各配一把 key（见上文配置），不要多人共用同一把。每个请求会用命中的那把 key 自身的尾部给审计记录打标签（`client_key_tag`，取法见 `KeyTag`：末 8 个字符，若这 8 个字符里有 `-`，只保留最后一个 `-` 之后的部分——所以 key 以 `...-alice` 结尾时标签就读作 `alice`；建议有意义的部分留 ≥3-4 位，太短容易和别的调用方撞标签）。`vmr report` 会自动识别，不需要加参数：每观测到一个不同的标签，就多写一份 `vmr-requests-<tag>.md` 详单（见上文）——同目录下，标签文件里 `details/…` 链接不用做任何调整。`vmr-report.md`/`.json`、`vmr-requests.jsonl` 和 `details/` 本身永远不分组、不重复：单条请求的详单只写一份，与调用方无关；`vmr-requests.jsonl` 永远覆盖全部记录；汇总报告永远覆盖所有人。不配置 `api_keys` 就什么都不会变——不多一个文件，不多一列。完整设计说明见 `docs/VirtualModelRouter_Design_v4_Analytics.md` §2.5。

纯内网、根本不想要真实鉴权？`api_keys` 不配置——门照样完全敞开——但客户端自愿发来的任意 Authorization/x-api-key 值依旧会走同一套标签提取逻辑并记录下来，vmr 侧不需要配置任何东西：每个客户端自己把发出去的值末尾带上 `-<标签>` 即可对 `vmr report` 自报家门。这个模式下没有 16 字符下限（本来就不是要保护的秘钥）；什么都不发的客户端依旧是未打标签的记录。

Agent 场景下每一轮都会把完整对话历史重新发一遍，单日日志动辄几个 GB——而且这种冗余主要出现在**行与行之间**，不是单行内部。每天的日志文件一旦不再是"今天"就自动轮转压缩：用 zstd 压缩整个文件（而不是逐行压缩）才能吃到跨行的重复内容，实测压缩比 20~75 倍——这是逐条记录单独压缩根本达不到的量级，因为单条记录看不到上一轮几乎重复的请求体。`vmr report` 对 `.jsonl` 和 `.jsonl.zst` 一视同仁，通配符同时覆盖两者即可。设置 `audit_retention_days` 还能让过期文件自动删除（缺省永久保留，不设置不会删任何东西）；压缩和清理都只看文件名里的日期，不需要扫描或逐个 `stat` 整个日志目录。背后的实测数据和方案取舍见设计文档 Part 1 §9.5。

**不要让两个 vmr 实例共用同一个 `log_dir`。** 每个实例的housekeeping 清扫只靠文件名里的日期判断"这份日志今天写完了"（可以压缩，过了保留期还能删）——它没有办法知道另一个进程是不是还在往同一个文件里追加。两个实例共用 `log_dir` 又都跨过了午夜，才会踩到这个坑：实例 A 轮转到今天的文件，看到昨天的文件已经"写完"，于是压缩、删除——而实例 B（还停在昨天的日期，或者轮转得慢一点）还在往那个已经被删除的 inode 里写。给每个实例（包括为了测试临时起的第二个 checkout）都配一个独立的 `log_dir`。

## Agent 任务叙事重建（`vmr story`）

`vmr report` 回答的是"这段时间总共花了多少、整体怎么样"；`vmr story` 读的是同一份审计 JSONL，但回答的是"这一个任务具体发生了什么，一步一步地看"——它把单次 Agent 任务的完整执行过程重建成上下文演化过程：每一轮进了什么新内容、模型拿它做了什么，以及（如果发生过）一次历史压缩具体丢了什么。

```bash
./vmr story "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"           # 列出候选任务
./vmr story -journey j-agent-20260716T152238-20260716T153122-42f908fa \
    "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"                   # 按 id（前缀即可）渲染一个
./vmr story -render-all "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # 一次批量渲染全部候选
```

不带 `-journey` 时列出全部候选任务：id、任务/轮次数、时间范围、标题预览（开场的真实指令）——挑一个，把它的 id（精确或唯一前缀均可）传给 `-journey`。`-render-all` 把所有候选的渲染合并成一次批处理，共享底层的文件扫描，不会每个候选各自重新扫一遍源文件。产物落在 `{out}/stories/journey-<id>.md`（叙事正文）与 `journey-<id>.json`（同一任务的行为剖面，见下文）——权限与 `details/` 一致（0600/0700），两者都承载完整对话内容。`-show-ungrouped` 打印前几条未能归组进任何会话的记录的来源位置——候选列表比预期短时的排查手段。

如果一个任务自己的开头看起来像是在续接一段本次没有加载进来的更早历史（表现为：真实的多轮开场恰好出现在你最早那个输入文件的最开头），默认会跳过——它的 id 在不同的文件加载范围下不保证稳定。加 `-include-partial` 可以照样渲染，文件名会带上 `-partial` 后缀，提醒你这个 id 换一批输入文件可能会变。

**怎么读这份叙事**：每个任务按用户指令切成若干轮（`Step`）分组归入对应的任务（`Task`）。每一轮把 **Messages**（这一轮新进入上下文的内容）与 **LLM Response**（模型自己产出的内容——推理块、回复文本、每个工具调用的完整参数）分开展示，"Agent 拿到了什么"和"Agent 决定做什么"不会混进同一份笼统的历史视图里。一次历史压缩事件（靠结构判断，不是靠认某个特定 Agent 框架的标记文本——原因见设计文档）会渲染成一段带标注的边界：压缩前后的 token 数，以及压缩前提到过的文件路径/URL 里，哪些在压缩之后再也没被提到过。这里的每一个数字都能追溯回某次请求自己记录的 usage，每一条"被吞掉"的判断也都是一次可以自己打开原文核对的简单字符串比对——不是猜出来的。

**行为剖面**（`journey-<id>.json`，与 `.md` 同时写出）：九项规则派生、零 LLM 成本的指标——模型时间/Agent 侧执行时间/人类空闲时间的三分解、工具调用分布、重复动作率、错误恢复次数、计划/执行比、上下文构成演化曲线（每一轮请求体里各角色 token 占比,让上下文预算的构成随任务推进的变化可见）、上下文有效利用率（进入上下文的内容有多少后来真的被再次引用过）、compaction 次数与信息损失。不管背后是 Claude Code、OpenClaw 还是别的框架，这套数字定义都一样——能横向对比不同 Agent 框架正是收集它们的初衷。

**对比两个任务**：`-compare <id1,id2>` 直接对比两个任务的行为剖面（不需要先分别 `-journey` 渲染），产物是 `compare-<id1>-vs-<id2>.md` + `.json`，与单任务的文件放在同一目录。每一行都同时展示两侧的值和相对变化，差异大到超过固定阈值时打 ⚠️ 标记（规则化判定，不是主观判断）——适合回答"换了 Agent 框架/prompt 之后，这个任务的完成方式到底有没有变"这类问题。报告里还包含以下同样零 LLM 成本的规则事实：双方各自用到的端点、逐轮 Prompt 缓存命中率曲线、双方 system prompt 的规模与稳定性（含有边界的节选，默认前 2 万字符——够覆盖两侧真实验证用例里"加载了哪些项目上下文文件"这类声明在原文中出现的位置，但仍是从开头截断的一段前缀，不保证覆盖任意长度 system prompt 的全部信息量）、末轮上下文按角色的构成、总耗时（紧邻已有的"净工作时长"一起展示，不单独当效率指标看）、双方的终止方式，以及——如果任务产出是通过一次"参数形状像文件写入"的工具调用落盘的——双方最终交付物本身的内容节选。报告末尾的"证据溯源"小节列出本次对比实际读取的源审计文件路径，方便独立核对。

**可选的 LLM 解读小节**：加上 `-llm-addr host:port -llm-model name`（一个已经在跑的 VMR 实例的地址和它暴露的虚拟模型名——不会自动拉起该实例；如果那台实例配置了鉴权还需要 `-llm-key`），会追加一段明确标注、完全可选的解读——一句话结论、一张"候选根因 | 直接证据 | 置信度（高/中/低）| 改进建议"表 + 一句话因果链、对逐轮工具调用序列的叙述性解读、以及一段"VMR 看不到什么"的诚实声明。置信度分档写死在 prompt 里：只有能在证据表或原文节选里指认出具体证据的候选才能标"高"，仅凭排除法/直觉的必须诚实标"低"（但仍会列出，不会因为不确定就不提）。喂给模型的只有上面的规则事实，加上两段有边界的原文节选（system prompt、最终交付物），不是完整对话正文，且 prompt 明确要求不得编造给定证据之外的数字，"节选里没提到"也不能被模型当成"确实不存在"来断言。加 `-llm-dry-run` 只打印证据包大小估算并退出，不实际调用。结果会缓存在 `stories/.llm-cache/` 下（key 同时包含两个 journey id、证据内容与所用模型——换 `-llm-model` 不会误用别的模型的缓存结果）；任何失败（地址不可达、非 2xx 等）只会跳过这一节并打印警告，报告的其余部分不受影响。

完整设计（背后的内容寻址模型、lineage/compaction 检测原理、九项行为剖面指标、已知盲区）：`docs/VirtualModelRouter_Design_v4_Analytics.md`。

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

**模型级覆盖**：每个 virtual model 都可以设置自己的 `image_downscale`，优先级高于全局值；不写则继承全局设置。`image_downscale: 0` 在模型层面是一个明确的"关闭"指令，即使全局开着也照样关——因为"没写"和"写了 0"含义不同（前者继承，后者强制关闭）。

**降采样结果缓存**：同一张原始图片、同一个目标像素上限，第一次处理后会把结果（JPEG 字节）缓存到磁盘。缓存 key 是**内容哈希 + 目标尺寸**——文件名为 `<原始字节的 sha256>-<maxPx>.jpg`，所以同一张图降到 512px 和 256px（不同模型的不同覆盖值）是两个互相独立的条目，绝不会串（目录取配置项 `image_cache_dir`，见下）。后续请求命中同一张图片时直接复用缓存字节，不再重新解码/缩放/编码。这带来两个好处：省 CPU（agent 场景每轮都会把完整对话历史连同图片重发一遍），以及避免破坏上游的 prompt cache——上游的缓存是按精确字节/token 匹配的，同一张图片如果每次都重新编码，输出字节可能有细微差异，足以让上游缓存失效；用缓存后的完全相同字节，上游缓存才能命中。缓存条目按"最近一次被命中"的时间做 TTL 淘汰（`image_cache_ttl_days`，缺省 7 天；命中会刷新计时，长对话里反复引用的图片不会被提前清掉），淘汰扫描搭在缓存目录访问上触发，不额外起定时器。

**审计目录和缓存目录到底落在哪**：两者都是 config 字段——

```yaml
# log_dir: ~/.vmr/logs                  # 审计 JSONL 目录；有设置就原样使用（~/ 会展开）；改动需要重启才生效
# image_cache_dir: ~/.vmr/image_cache   # 降采样缓存目录；规则同上；随热重载即时生效
```

——有设置就原样使用（开头的 `~/` 展开为 home 目录），否则落在持久的 `~/.vmr/logs`/`~/.vmr/image_cache`，再否则（解析不出 home 目录）退到系统临时目录下的 `vmr_logs`/`vmr_image_cache` 子目录，最后才是二进制所在目录的 `./logs`/`./image_cache`。默认持久化是刻意的：macOS 会清理约 3 天未访问的临时目录条目，会静默删掉审计数据——而它是 `vmr report` 唯一的数据源。想知道实际解析出来的路径，直接跑 `vmr check -c config.yaml log` / `vmr check -c config.yaml cache`（不带参数的 `vmr check` 与启动摘要也会打印），不用真的启动服务。`vmr.sh` 只查询 `vmr check log` 来定位 server log 落点，而不是在 bash 里另写一份猜测逻辑——dev 模式和 `service install` 因此不会对"数据到底存在哪"这件事产生分歧。两个目录都没有对应的环境变量——想从环境注入，在 `log_dir`/`image_cache_dir` 里显式写 `${VAR}` 即可。

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic Messages 协议入口（流式 + 非流式） |
| `POST /v1/responses` | OpenAI Responses 协议入口（流式 + 非流式）；需要一条 `protocol: openai-responses` 的端点 |
| `GET /v1/models` | Virtual Model 列表（两种 SDK 均可解析） |
| `GET /admin/status` | 端点健康 + 并发指标，含某个端点当前是否正被一次后台恢复探测占着单飞名额（仅 loopback） |
| `vmr start -c config.yaml [-audit=false]` | 前台运行路由器（Ctrl-C 停止）；`-audit=false` 关闭 JSONL 审计日志（默认开启）。`./vmr.sh start` 是它的后台托管版本，也是脚本唯一接管的一条命令——前台/开发场景直接跑这条 |
| `vmr check -c config.yaml` | 校验配置、跑一致性扫描（`api_key` 缺失、重复端点……），打印路由表、Key 状态与每个 provider 的生效代理——有问题的取值带内联 ⚠️，末尾附 `=== Failed ===` 汇总。末尾带 `log`\|`cache` 参数时改为只打印那一个生效目录（`log_dir`/`image_cache_dir` 缺省后的值）——`vmr.sh` 内部就是问这个 |
| `vmr status -c config.yaml` | 渲染运行实例的身份（pid / listen / uptime / 配置绝对路径）+ 健康与并发占用。`-addr host:port` 改成直接查那个端口上的实例、完全不加载 config——本机跑着多个实例、或者你手上根本没有那份 config 时用它；`-brief` 只打一行 Tab 分隔的摘要（`./vmr.sh ps` 就是拿它拼表） |
| `vmr report [-o dir] [-pricing pricing.yaml] [-lang en\|zh] [-report-config report.yaml] <glob>` | 审计日志（明文或 `.zst`）→ 用量统计 + 会话/工具分析 + 逐请求特征（`vmr-requests.jsonl`）+ 详单（`-details=false` 关闭）；加载了定价配置就会渲染 §2 成本估算章节——`-pricing` 显式指定，或者不加这个参数时自动加载当前目录下的 `./pricing.yaml`（存在的话）。输出语言默认英文，`-lang` 或 `report.yaml` 的 `language:`（见上文"输出语言"）可切换成中文 |
| `vmr story [-journey <id> \| -render-all \| -compare <id1,id2>] [-lang en\|zh] [-report-config report.yaml] <glob>` | 把一次 Agent 任务的完整执行过程还原成可读的 Markdown 叙事（见下文"Agent 任务叙事重建"）；不带参数列出候选任务及其 id，`-render-all` 一次批量渲染全部，`-compare id1,id2` 对比两个已渲染任务的行为剖面（规则事实之外，加 `-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]` 可追加可选的 LLM 解读小节）。`-lang`/`report.yaml` 控制输出语言，与 `vmr report` 一致 |
| `vmr version` | 打印本二进制的构建标识（git SHA，脏工作区加 `-dirty` 后缀，外加 commit 时间与 Go 版本）。不需要 ldflags：Go 默认把 VCS 状态压进任何仓库内构建的二进制，运行时读出来即可。运行中实例的同一个值在 `/admin/status` 与 `./vmr.sh ps` 的 VERSION 列里，可以直接对比"那个进程跑的是不是我刚编的这版" |
| `vmr diagnose [-c config.yaml]` | 比 `check` 的静态预览更进一步：对每个 provider 做 DNS/TLS/代理连通性检查，再发一次真实的最小请求到每个配置的端点，要求对方原样回显一个一次性 token（并发执行，`-test-timeout` 控制单项超时，默认 15s）——拿到 200 但没回显这个 token 会标成警告而不是直接判通过，用来抓那种网关/中转层拿缓存或兜底响应假装成功的情况——并给出标注了检测结果的路由顺序预览（`-no-test-routing` 跳过真实请求，`-json` 供脚本消费；只要有检查失败就以非零退出码结束） |
| `vmr replay -provider NAME <audit.jsonl>` | 用 vmr 自己构造请求的同一条代码路径，从一条审计记录重建并重发请求——`-dry-run` 只打印不发送，`-record path` 把这次回放的结果也写成一条独立的审计记录，`-model`/`-protocol` 可覆盖记录里原有的值，`-stream true\|false` 强制开关流式，`-max-time` 限制上游等待时长。选择要回放哪条记录：`-detail file`（`vmr report` 产出的 `details/*.json` 文件，不用数行）、`-ts <timestamp>`（匹配 `vmr-requests.jsonl` 或原始审计日志里的 `ts` 字段）、`-line N`（默认取文件里最后一条）——三者互斥 |
| `./vmr.sh start\|stop\|…` | dev 模式生命周期（自己监督） |
| `./vmr.sh ps` | 列出本机所有 vmr 实例（不限于本 checkout）：pid、监听地址、uptime、模型数、配置文件绝对路径。三步各司其职——`pgrep` 找进程、`lsof` 找它占的端口（监听地址只写在那个进程的 config 里，命令行上没有）、再用 `vmr status -addr … -brief` 问实例自己要其余信息。缺 `lsof`、或进程不应答 `/admin/status` 时，退化成只有 pid + 命令行上那个 `-c` 参数的行并标注原因，不会把实例整个漏掉 |
| `./vmr.sh service install\|uninstall\|start\|…` | init 系统服务（launchd/systemd：崩溃重启、登录自启） |
| `./vmr.sh <上表任一命令> [参数]` | 脚本不认识的子命令一律原样转发给二进制（`./vmr.sh check`、`./vmr.sh diagnose`、`./vmr.sh report …`），不是白名单——二进制新增的子命令当天就能用。转发时做两件事：**回到调用者原来的目录**（相对路径、glob、`-o` 的含义与直接跑 `vmr` 完全一致），以及**没写 `-c` 时补上脚本所在 checkout 的 `config.yaml` 绝对路径**（`report` 没有 `-c`，不补）。前台 `vmr start` 是唯一被脚本遮蔽的命令——脚本的 `start` 是后台版，要前台就直接跑 `./vmr start -c config.yaml` |

经路由的响应带 `X-VMR-Endpoint`（实际命中端点）、`X-VMR-Attempts`（尝试次数）与 `X-VMR-Route-Reason`（为什么选中它：`pick=sticky|order`、`eligible=N/M`，以及真正发生过时才出现的 `cooldown=` / `conditions=` / `ctx_fallback=1`）；只要有失败过的尝试，再带一个 `X-VMR-Failover`（如 `deepseek/deepseek-v4:429, minimax/m2:500`，构建/网络失败记 `:err`）——**请求成功时也带**，所以"这次是第三次 failover 才成功的"在终端里直接看得见，不用事后翻审计日志。

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

# 或者用 vmr-requests.jsonl / vmr-report.md 里看到的精确时间戳来定位。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -ts 2026-07-13T15:30:42.100+08:00 \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"
```
