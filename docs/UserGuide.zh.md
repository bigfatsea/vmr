<!-- Ver 2026-07-24 23:30, by Sonnet 5 -->

# vmr — 用户指南

[English](UserGuide.md) | 简体中文

完整的配置参考、协议行为细节、CLI 说明。如果只是想先跑起来，先看 [README](../README.zh.md) 的快速开始——跑通之后再回来看这份。

## 配置

`providers`/`models` 都按协议分两层：外层 key 是协议（`openai` / `anthropic`），内层才是名字。一个 model 的 endpoints 只能引用同协议分组下的 provider——跨协议混用没有语法能表达它，而不是靠校验去抓。同一账号的两个协议面可复用同一个短名（`openrouter`），不需要后缀区分：

```yaml
listen: 127.0.0.1:8800
# api_keys:                    # 可选：保护 vmr（Bearer 或 x-api-key 都认）；每把 key 在
#   - ${VMR_KEY_ALICE}          # `vmr report` 里按各自的尾部打标签分组统计（见下文"多调用方场景"）。
#   - ${VMR_KEY_OPENCLAW}       # 旧的单把 api_key 已移除——配置里还写着它会在加载时被拒绝并提示迁移
# max_attempts: 0              # 每请求上游尝试数上限（缺省 0 = 试遍全部候选）
# probe_mode: active            # active（缺省）| passive —— 端点冷却到期后如何重新验证恢复，再放行真实流量，见下文"故障切换与健康"
# probe_timeout: 15s            # 仅 active 模式生效：一次后台恢复探测的超时上限
# max_request_body_mb: 8       # 入站请求体大小上限（仅为稳定性考虑；审计日志始终原样全量记录，不受此项限制）
# max_concurrency: 8           # 全局并发上限，超限请求挂起等待（缺省不限）
# https_proxy: http://127.0.0.1:7890   # https 型 base_url 的代理服务器地址——vmr 用代理的唯一途径
#                                      # （环境变量被忽略；要引用就显式写 ${HTTPS_PROXY}）。
#                                      # 只是声明代理在哪，不代表默认开启——见下面的 `proxy`
# http_proxy: http://127.0.0.1:7890    # http 型 base_url 同理（如局域网 llama.cpp）
# proxy: false                  # 全局默认值：没有自己 proxy 开关的 provider 走这个（缺省 false）。
#                                # 推荐写法：全局保持关闭，个别需要代理的 provider 自己写 proxy: true——
#                                # 显式的单点意图，不会让以后新加的 provider 悄悄被一个全局开关决定
# image_downscale: 512         # 请求内联图片长边像素上限，缺省关闭（可被虚拟模型自身设置覆盖，见下文）
# image_cache_ttl_days: 7      # 降采样结果缓存的失效期（缺省 7 天）
# audit_retention_days: 30     # 超过此天数的审计文件自动删除（缺省永久保留）
# timeouts:
#   connect: 10s               # 连接上游
#   response_header: 120s      # 上游首字节
#   stream_idle: 120s          # 上游 body 静默看门狗（流式/非流式/错误体都覆盖）

providers:
  openai:
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}
      proxy: true              # 永远走 https_proxy/http_proxy，无视全局 proxy 默认值——
                               # 给海外 provider 开代理的推荐写法
    minimax:
      base_url: https://api.minimaxi.com/v1
      api_key: ${MINIMAX_API_KEY}
      # proxy: false           # 这里不需要写——推荐的基线（全局 proxy 关闭）本来就对
                               # 这个 provider 默认直连
  anthropic:
    openrouter:                # 同一账号的 Anthropic 面，同名不冲突（两层 map 天然隔离）
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}

models:
  openai:
    coding:
      endpoints:
        - {provider: openrouter, model: z-ai/glm-5.2}   # 不写 priority：列表顺序就是尝试顺序
  anthropic:
    claude:                    # anthropic 协议 → 走 /v1/messages
      endpoints:
        - {provider: openrouter, model: minimax/minimax-m3}
```

全部字段与校验规则见设计文档 §10。修改配置数秒内热生效；坏配置被拒绝、不影响运行实例。解析是严格的：未知或拼错的配置键（如 `max_concurency: 8`）会直接导致加载失败，绝不会被静默忽略、让你误以为设置已生效。

**上游代理——只认显式配置，默认关闭**：`http_proxy`/`https_proxy` 只声明代理服务器**在哪**，本身不会替任何 provider 打开代理。一个 provider 是否真的走代理，由三层解析决定：provider 自己的 `proxy: true`/`false` 最高优先；没写就跟随全局 `proxy` 开关（缺省同样是 `false`）；解析结果是"开"时，才按 base_url 的 scheme 选用 `https_proxy`/`http_proxy`。**推荐写法**：全局 `proxy` 保持关闭，个别需要代理的 provider（通常是海外厂商）自己写 `proxy: true`——这是显式的单点意图，不会让以后新增的 provider 悄悄继承一个已经过时的全局默认值。（只有想让"默认走代理"成为基线、再用个别 provider 的 `proxy: false` 挖例外时，才把全局 `proxy` 设为 `true`。）**代理环境变量被有意忽略**——隐式旋钮悄悄改变流量走向，最容易被忽略、排障时最难想到；要引用它就显式写 `https_proxy: ${HTTPS_PROXY}`。`proxy: true`（不管全局还是 provider 级）但没配对应的代理地址是校验错误（拒绝加载），不是运行时惊喜。`vmr check` 与启动摘要逐 provider 打印生效代理（凭证掩码）。YAML 1.2 语法：写 `true`/`false`，不能写 `on`/`off`。

**base_url 必须自带版本号**：vmr 在初始化时预计算每个 provider 的完整上游 URL——直接把协议的裸路径（OpenAI 为 `/chat/completions`，Anthropic 为 `/messages`）拼在 `base_url` 后面，不做任何归一化或重叠检测。所以 `base_url` 必须已经带上该 provider 自己的完整 API 版本号，不管它叫什么：`https://api.example.com/v1`、`https://api.minimaxi.com/anthropic/v1`、`https://ark.example.com/api/coding/v3`。这条规则的原因是：不是所有 provider 的 OpenAI/Anthropic 兼容面都叫 `v1`——比如火山引擎 coding plan 的 OpenAI 端点版本号是 `v3`——所以 vmr 不会替你猜版本号；写错了会立刻在你写的这个 base_url 上报 404，而不是被悄悄"纠正"成别的样子。URL 在配置加载时一次性计算并存入 Endpoint，adapter 直接使用，不在每次请求时构造或归一化 URL。

**`role_map`——按 provider 做 role 改写**：有些 OpenAI 兼容 provider 会拒绝它上游不认识的 role——典型场景是 OpenAI 为 o1/o3 系列模型引入的 `developer` role，部分网关（如 DashScope/千问）会直接拒收。在 provider 下写 `role_map: {developer: system}`，vmr 会在请求发往上游之前，把顶层 `messages` 数组里匹配到的 `"role"` 值原地改写，客户端完全不用改。它是一个纯粹的旧→新字符串映射，只作用于列出的那几个 role——请求的其余每一个字节（键序、空白、未知字段、消息内容）原样透传，跟 `RewriteModel` 改写 model 字段用的是同一套字节级拼接手法。这个开关挂在 provider 一级（跟 `base_url`/`api_key` 同级），不是挂在虚拟模型上——因为"拒收某个 role"通常是上游网关本身的特性，不是它背后某一个模型的特性；某个模型如果从不发送被映射的那个 role，配不配 `role_map` 对它没有影响。不配置（或留空）`role_map` 的 provider 保持默认行为：所有 role 原样通过。

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
- 两个 **MiniMax-M3 专属修复**，各自只在确认命中其确切形态时触发：剥 content 里的内联 `<think>…</think>` 推理（不剥会持久化进历史，把模型锁进反馈循环），以及剥 MiniMax 某个思考模式下（这个模式不写 `<think>` 标签）以纯文本输出的「Thinking Process:」思考段；
- `data: [DONE]` 哨兵——**仅** OpenAI 协议流式且上游未发时补；绝不重复，绝不注入 Anthropic 流。

流式是真的：事件到达即转发；只有检测到思考形态才缓冲，`</think>` 闭合后立即恢复实时流。带 `Content-Encoding` 的压缩体零变换直通。上游 3xx 重定向绝不跟随——301/302/303 的原始状态、`Location` 头、体原样到达客户端，和直连一模一样（`http.Client` 默认策略会把 POST 301/302/303 静默改写成 GET，这会破坏字节级保真）。响应头与请求头同一策略——除 hop-by-hop 外全部透传；错误响应连状态、头（含 `Retry-After`）、体原样返回。每个请求实际生效的归一化记录在审计日志 `attempts[].norm`，上游与客户端之间的任何字节差异都有逐请求的解释。

正因为透传是字节级的，两个协议任何一侧新增的请求/响应字段都不需要 vmr 改代码就能到达上游或客户端——这正是透传的意义所在。vmr **不做**的事：只路由 `POST /v1/chat/completions` 和 `POST /v1/messages` 两个入口，其他 OpenAI/Anthropic surface（`/v1/responses`、`/v1/realtime`、`/v1/images`、`/v1/audio` 等）不在范围内——这类需求请直接把客户端指向供应商自己的 base URL。

## 故障切换与健康

上游失败即按端点列表顺序逐个尝试，直到成功或全部耗尽（`max_attempts` 可选设上限）。健康完全由失败驱动，按响应逐条分类，确保每次失败既受到匹配的惩罚，也得到正确的"还要不要继续切换"的判断：

- 网络/5xx：短冷却指数退避；401/额度耗尽/模型不存在/某个网关或中转层报告了它自己的转发失败（而不是请求本身有问题）：长冷却；429/503：尊重 `Retry-After`；
- 400 类**客户端**错误——确实是请求本身写错了——直接返回，不切换、不冷却：换哪个端点都会被同样拒绝，切换解决不了任何问题；
- **内容合规拦截**切换下一家，但**不惩罚**被拦端点——它只是拒绝了这一条请求，并没有坏。

**冷却端点如何恢复**（`probe_mode`，缺省 `active`）：

- `active`（缺省）：端点冷却一到期，vmr 立刻在后台发一个专门的轻量探测请求（受 `probe_timeout` 约束，缺省 15s），而不是让下一个真实请求自己去踩一脚。真实流量在端点被确认恢复之前**完全不会碰到它、也不会等它**——探测没出结果之前，请求照样路由到下一个候选，不管探测本身要跑多久。探测会要求模型原样回显一个一次性 token，所以网关返回一个缓存/兜底的"假成功"不会被当成恢复。
- `passive`：更早期的行为——冷却到期后**第一个真实请求就是探针**（单飞，防止大量并发请求同时涌向刚恢复的端点）。这个探针请求本身跑多久、多大，就决定了这次恢复检测要花多久；并发场景下，其他打向同一端点的请求在这段时间里都会被导流到下一个候选。不想多花这一次探测请求，或者本来就不会有高并发场景，可以切回这个模式。

```yaml
probe_mode: active      # active（缺省）| passive
probe_timeout: 15s      # 仅 active 模式生效：一次后台探测的时间上限
```

全部候选失败时原样返回最后一次上游错误。流式只在首字节前切换。

## 条件路由

同一个虚拟模型背后挂的端点不必是完全等价的。声明每个端点实际支持什么,一条请求需要而某个端点没声明的能力,该端点会被直接跳过——而不是照样把请求打过去,得到一个必然失败的结果:

```yaml
models:
  openai:
    agent:
      endpoints:
        - provider: minimax
          model: MiniMax-M3
          capabilities: [text, image, tools]   # 自由字符串标签：这个端点接受什么
          max_context_tokens: 1000000          # 声明的上下文窗口
        - provider: deepseek
          model: deepseek-chat
          capabilities: [text, tools]           # 没有 "image"——带图片的请求会跳过这个端点
          max_context_tokens: 128000
```

两个字段都是可选的，缺省即**不限制**：端点不声明 `capabilities` 就视为什么都支持，不声明 `max_context_tokens` 就没有上限——现有配置文件行为完全不变。`capabilities` 一旦声明就是穷尽式的（把端点真正支持的能力全部列出来，不是只列你想让 vmr 检查的那几个）；`vmr check` 会把每个端点声明的能力和上下文上限打印出来，配置遗漏在这里一眼可见。

两类条件性质不同：

- **`image` / `tools`**——确定性的硬要求。请求需要某个能力但找不到任何候选声明支持时，直接快速失败，返回 `vmr_no_candidates` 并点名缺失的能力，而不是白白浪费一次必然被拒绝的尝试。`image` 的判断是结构性的（请求里是不是真的有 `image_url`/`source` 图片块），不是靠猜文本内容——正文里恰好提到"image"这个词的纯文本请求不会被误判；一张 vmr 自己的解码器认不出格式的图片，依然算作有图片（"检测到"和"解得出格式"是两回事）。（`thinking`/`audio`/`video` 暂不检测——这几项的请求侧探测逻辑在各厂协议上还没有确认，现在声明它们也不会有任何效果。）
- **上下文长度**——一个刻意保守的**粗估**，不是确定值：请求字节按 ASCII（约 4 字节/token）和多字节 UTF-8/中文等（约 2 字节/token，故意估得偏高）分类估算，每张检测到的内联图片按固定约 3000 token 计，检测到的文档/PDF 附件按其 base64 载荷长度 ÷ 20 估算——全程只做廉价的结构标记扫描，不解析内容。因为只是估算，它永远不会单独把一条请求拒之门外：如果所有端点声明的 `max_context_tokens` 看起来都不够，vmr 不会直接报错，而是照样在能力匹配的候选里尝试——高估的代价最多是浪费一次尝试，不会是一条本该成功的请求被拒。

完整设计与 token 估算的调研依据：`docs/VirtualModelRouter_System_Design_v3.md`「条件路由」一节。

## Sticky Model（会话亲和）

上游的 prompt cache 是按精确字节前缀匹配的。如果一条多轮 agent 对话在中途被路由到不同端点，上游的缓存就会失效，一次"看起来更合适"的路由选择反而可能让总成本更高——上面的条件路由本身就可能触发这种情况（比如 agent 压缩上下文后，估算出的长度缩小到低于另一个端点声明的上限）。Sticky Model 会把一条对话尽量留在最近一次成功服务过它的端点上：

```yaml
sticky_ttl: 10m              # 全局默认：粘性偏好保持有效的时长

models:
  openai:
    agent:
      # sticky: true 是默认值，不用写；只有真正的单次调用场景（没有多轮价值可保护）
      # 才需要显式写 sticky: false
      endpoints:
        - provider: minimax
          model: MiniMax-M3
          # 继承全局的 10 分钟 sticky_ttl
        - provider: deepseek
          model: deepseek-chat
          sticky_ttl: 2h      # DeepSeek 磁盘缓存寿命数小时到数天——单独为这个端点覆盖
```

- **身份识别**：对话锚点取自 system prompt **和**第一条非 system 消息的哈希——两者都只哈希、从不记录或以其他方式暴露。两个恰好用同一句话开场的不同 Agent 不会被混同，因为它们的 system prompt（进而它们在上游真正的缓存前缀）不同；如果只哈希首条用户消息、不含 system prompt，恰好会漏掉这个场景。
- **`sticky_ttl` 是端点级的，不是模型级的**——缓存寿命是上游厂商的属性（Anthropic/OpenAI/MiniMax 大约 5-10 分钟；DeepSeek 数小时到数天），所以同一个虚拟模型下的不同端点可以各自声明自己的窗口，不必强行统一成一个值。全局 `sticky_ttl`（默认 10 分钟）是没有显式覆盖的端点的兜底值。
- **`sticky_ttl`（全局或端点级）不能超过 24 小时**——粘性注册表自己会在一条记录闲置 24 小时后把它从内存里清掉，不管端点自己声明的 TTL 是多少，所以写一个更长的值能加载成功，但会悄悄失效。`vmr check`/`vmr start`/热加载都会拒绝这类配置，并在报错里点名是哪个模型/端点。
- 亲和性只会在已经通过健康检查和条件过滤的端点里重新排序——一个之后变得不健康、或者不再满足某项必要能力的端点，不会仅仅因为它是上次的粘性选择就被复活。
- 每次成功完成请求（含 failover 后的成功）都会更新粘性指针，所以它始终跟随对话实际生效的缓存所在——一个过时的指针会在下一次成功请求时自动纠正，不需要额外的失效检测逻辑。

完整设计（身份信号的取舍、TTL 默认值背后的调研、为什么这里的指纹和下文 `vmr report` 离线会话分组是两套独立实现）：`docs/VirtualModelRouter_System_Design_v3.md`「Sticky Model」一节。

## 审计日志与用量报表

默认开启：每个请求一行 JSONL，双层记录（调用方↔vmr 与每次 vmr↔上游尝试）、凭证掩码、生效的归一化清单，以及请求内联图片的元数据（格式/宽高/字节数，以及是否触发压缩/是否命中缓存——不论该虚拟模型是否开启了图片压缩，都会采集）。body 一律原样全量记录，不设审计侧截断上限（上面的 `max_request_body_mb` 只管入站请求体大小，与审计记录无关）。每次上游尝试同时携带一个人类可读的 `endpoint` 标签（`protocol:provider:model`）和拆开的三个结构化字段（`protocol`/`provider`/`model`），并在自由文本 `error` 之外新增一个类型化的 `error_class`。

每条记录还带一个 `facts` 对象——vmr 自己对这条请求的路由前判断（`has_image`/`has_tools`/`estimated_tokens`），和路由当时用来选端点的值完全一样，原样落盘，不是事后重新算的。它是这条请求的兄弟字段，不是请求本身的一部分，所以记录下来的请求体依旧对客户端原始请求保持字节忠实。请求在路由开始之前就被拒绝时（鉴权失败、JSON 解析不了）这个字段整体不出现，不是一个全零值的对象。

```bash
./vmr start -c config.yaml                 # 写入 config 的 log_dir（`vmr dirs -c config.yaml log` 可核对）
./vmr start -c config.yaml -audit=false    # 关闭
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report "$(./vmr dirs -c config.yaml log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.jsonl（明文/.zst 混着传也行）
```

`vmr report` 同时统计 tokens 与字节（上游不回报 usage 时以字节兜底）。每条 record 在一次遍历内被同步 push 到**所有相关桶**（`Rows` 按日期×协议×模型、`Overall`、`ByModel`、`ByDate`，加原有的 `Hours`/`Endpoints` 及其"全部日期合并"版本 `HoursOfDay`/`EndpointsAll`）——每个桶都在这一趟遍历里直接收自己的原始值、各自算自己的 p50/p95，所以每个表格里的分位都是**真值**，没有跨桶近似。`HoursOfDay`/`EndpointsAll` 是独立收原始值的桶，不是拿逐日的 `Hours`/`Endpoints` 二次合并出来的——合并"已经算完的"桶算不出真百分位，因为每个桶算完自己的分位后就会把原始值释放掉，再合并时已经没有东西可算了。Markdown 表格共享统一的列定义（`Req/Fall/Trunc / 成功率 / Tokens In/CacheHit/Out / 图片/压缩 / 平均Tokens In/Out / 字节 In / Out / 平均消息数 / p50/p95 首字延迟 / p50/p95 请求耗时 / 平均吞吐 (tok/s)`）——请求数、Fallback 数、截断数合并进一个单元格（全 0 时显示 `-`），`图片/压缩` 显示该行的内联图片总数与其中触发压缩的数量（无图片时显示 `-`）。各表再加自己的主键列与特异列（`Tool 调用`，工作负载表里带"发生过调用的请求占比" / `错误分布`）。每模型的健康信号：finish_reason 分布（`length` = 输出被 token 上限截断）。Token 统计还拆出缓存读取、（Anthropic）缓存写入与 reasoning tokens。另有每小时活跃度（`hours_of_day[]`）和工作负载切分（`workloads[]`：交互工作 vs heartbeat/日记 cron 这类定时脚手架），一眼看清请求和账单到底来自哪里。运行进度写到 stdout，每个文件一行（`[i/N] <path>  done: M records, K parse errors (Ts)`）。JSON 是二次开发（图表/Dashboard）的数据源。

`vmr report` 还能读懂 Agent 工作负载——全部离线、纯规则、不调用 LLM（方法与实证见设计文档 §9.4"Agent 会话分析"一条）：

- **会话 → 任务 → 轮次分组**。每轮重发同一段渐增对话的请求以首条非 system 消息做指纹（Claude Code 的 `metadata.user_id` 存在时优先），按最长公共前缀成链——多个 Agent 会话即使在时间上互相穿插也能干净分开。任务边界来自 Traceparent trace-id 变化与增量中的新用户指令，两个信号互为交叉验证。Compaction 调用被识别并双向链接，会话与其压缩后的续接体串成同一条线程。
- **`vmr-requests.jsonl`** —— 每请求一行特征（会话/任务/轮次、trace 与 chat id、请求形态、`heartbeat` 等标签、当轮 tool 调用、finish_reason、"ok 但截断"标志、含 reasoning 的 token 细分、增量大小、最新指令），jq / DuckDB / pandas 直接可用。
- **工具使用报告** —— 按请求形态列出：声明的工具 vs **当轮实际调用**的工具（从响应中提取,历史重发绝不重复计数），外加"声明但从未调用"清单（**numbered list + 字母序，自然让 `feishu_*` 同前缀聚类**）及其每请求字节成本——为从 Agent 配置里裁掉没用的工具提供直接依据。

`vmr report` 还会把每条记录导出为一个人类可读的 Markdown 详单**外加一个同名 JSON 文件**（原始 record，方便 jq/脚本查询），落在 `{out}/details/` 下，用于深挖单个请求：头部一行定位（trace / chat user / tools，取值加粗），紧接一行上文提到的 `facts` 读数（图片/Tools 是否命中、预估 token 数——该记录没有 `facts` 时不出现这一行），再是**完整消息列表**（每条消息默认 `<details>` 折叠；本轮新增的消息在 summary 上加 🆕 前缀，末尾追加一行 `🆕 本轮增量（相对上一轮,+N 条,#1–#M 为历史上下文）` 汇总）、每次上游尝试的 headers 与 body 字段全量对照（变化项以 emoji 标记：🟢 新增 / 🔴 删除 / 🔶 变化）——若该次尝试剥离了 `<think>…</think>` 推理块，还会展示剥离前的完整内容及对应原始 SSE（字段缺失的旧格式日志显示"未保留"提示）、客户端响应部分把 SSE 流重组成模型实际输出并保留原始事件全文。文件名以零填充时间戳开头，按名字排序即按时间排序。`vmr-requests-index.md`（与 `vmr-report.md` 并列，在 `details/` 上一级）按 **Chat User** 分组（`chat_id` 字段剥掉 `user:` 前缀）：每个用户一段 `## Chat User xxx`，下辖每个任务的首条用户指令引用块 + 轮次表（`轮 / 时间 / Message / finish / 耗时 / 首字延迟 / Tokens In/CacheHit/Out / 图片/压缩 / 文件`——`Message` 是 `M+N` 格式（M = 历史消息数，N = 本轮新增数），`finish` 为 `tool_calls` 时显示 `tool_call:<工具名>`，`耗时` 把结果/尝试次数信息以尾注形式追加，不单占两列（`❌<结果>` / `🚫取消` / `⚠️截断` / `🔄尝试x{n}`，可并存），文件列是 `md`/`json` 两个短链接）。"全部请求（时间序）"表把模型和上游合并成一个 `VM/API` 列（`protocol | 虚拟模型 | provider:model`，例如 `openai | agent | minimax:MiniMax-M3`——用 `:` 而非 `/` 分隔供应商和上游模型名，因为 OpenRouter 这类供应商的模型名本身就带 `/`）。Compaction 调用、定时任务（heartbeat/dream_diary）与非聊天体/被拒请求一律归入 `## Chat User (unresolved)`，折叠成紧凑的子分组（`### 压缩任务 · compaction 会话 × N`、`### 定时任务 · <class> 单发会话 × N`、`### 其他 · 非聊天体/被拒请求 × N`），不再一次触发就单占一段，也不再单独占一个顶级标题。加 `-details=false` 可关闭详单导出。

**多调用方场景。** 如果一个 vmr 实例被多个调用方共用（队友、另一个 Agent、CI 任务），想在事后统计里把各自的用量分开看，就给每个调用方在 `api_keys` 下各配一把 key（见上文配置），不要多人共用同一把。每个请求会用命中的那把 key 自身的尾部给审计记录打标签（`client_key_tag`，取法见 `KeyTag`：末 8 个字符，若这 8 个字符里有 `-`，只保留最后一个 `-` 之后的部分——所以 key 以 `...-alice` 结尾时标签就读作 `alice`；建议有意义的部分留 ≥3-4 位，太短容易和别的调用方撞标签）。`vmr report` 会自动识别，不需要加参数：每观测到一个不同的标签，就在原有产物旁多写一份 `vmr-requests-<tag>.jsonl` 和 `vmr-requests-index-<tag>.md`——同目录下，标签文件里 `details/…` 链接不用做任何调整。`vmr-report.md`/`.json` 和 `details/` 本身永远不分组、不重复：单条请求的详单只写一份，与调用方无关；汇总报告永远覆盖所有人。不配置 `api_keys` 就什么都不会变——不多一个文件，不多一列。完整设计说明见设计文档 §9.4"按调用方（`client_key_tag`）分组导出"一条。

纯内网、根本不想要真实鉴权？`api_keys` 不配置——门照样完全敞开——但客户端自愿发来的任意 Authorization/x-api-key 值依旧会走同一套标签提取逻辑并记录下来，vmr 侧不需要配置任何东西：每个客户端自己把发出去的值末尾带上 `-<标签>` 即可对 `vmr report` 自报家门。这个模式下没有 16 字符下限（本来就不是要保护的秘钥）；什么都不发的客户端依旧是未打标签的记录。

Agent 场景下每一轮都会把完整对话历史重新发一遍，单日日志动辄几个 GB——而且这种冗余主要出现在**行与行之间**，不是单行内部。每天的日志文件一旦不再是"今天"就自动轮转压缩：用 zstd 压缩整个文件（而不是逐行压缩）才能吃到跨行的重复内容，实测压缩比 20~75 倍——这是逐条记录单独压缩根本达不到的量级，因为单条记录看不到上一轮几乎重复的请求体。`vmr report` 对 `.jsonl` 和 `.jsonl.zst` 一视同仁，通配符同时覆盖两者即可。设置 `audit_retention_days` 还能让过期文件自动删除（缺省永久保留，不设置不会删任何东西）；压缩和清理都只看文件名里的日期，不需要扫描或逐个 `stat` 整个日志目录。背后的实测数据和方案取舍见设计文档 §9.5。

## 请求图片自动降采样

可选，默认关闭。开启后，超过设定长边的内联 base64 图片附件会被等比缩小、转 JPEG 再发上游——为截图密集的 agent 工作流削减 vision token 成本。只处理请求，不碰响应，不抓远程 URL；GIF（不论单帧多帧）与解码失败一律原样透传（fail-open）——GIF 为什么一律不缩放，见设计文档 §7。

图片检测始终开启，与这个开关无关：不论该虚拟模型是否开启了降采样，请求里的每张内联图片都会做一次廉价的头部解析（格式/宽高/字节数，不解码像素）并写入审计日志，所以即使某个模型关闭了压缩，`vmr report` 的 `图片/压缩` 列也能反映真实的图片流量。

```yaml
image_downscale: 512   # 全局长边像素上限；0 或缺省 = 关闭
image_cache_ttl_days: 7   # 降采样结果缓存的失效期（缺省 7 天，见下）

models:
  openai:
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

——有设置就原样使用（开头的 `~/` 展开为 home 目录），否则落在持久的 `~/.vmr/logs`/`~/.vmr/image_cache`，再否则（解析不出 home 目录）退到系统临时目录下的 `vmr_logs`/`vmr_image_cache` 子目录，最后才是二进制所在目录的 `./logs`/`./image_cache`。默认持久化是刻意的：macOS 会清理约 3 天未访问的临时目录条目，会静默删掉审计数据——而它是 `vmr report` 唯一的数据源。想知道实际解析出来的路径，直接跑 `vmr dirs -c config.yaml log` / `vmr dirs -c config.yaml cache`（`vmr check` 与启动摘要也会打印），不用真的启动服务。`vmr.sh` 只查询 `vmr dirs` 来定位 server log 落点，而不是在 bash 里另写一份猜测逻辑——dev 模式和 `service install` 因此不会对"数据到底存在哪"这件事产生分歧。两个目录都没有对应的环境变量——想从环境注入，在 `log_dir`/`image_cache_dir` 里显式写 `${VAR}` 即可。

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic 协议入口（流式 + 非流式） |
| `GET /v1/models` | Virtual Model 列表（两种 SDK 均可解析） |
| `GET /admin/status` | 端点健康 + 并发指标，含某个端点当前是否正被一次恢复探测（被动或主动）占着单飞名额（仅 loopback） |
| `vmr check -c config.yaml` | 校验配置、打印路由表、Key 状态与每个 provider 的生效代理 |
| `vmr status -c config.yaml` | 渲染运行实例的健康与并发占用 |
| `vmr report [-o dir] <glob>` | 审计日志（明文或 `.zst`）→ 用量统计 + 会话/工具分析 + 逐请求特征（`vmr-requests.jsonl`）+ 详单（`-details=false` 关闭） |
| `vmr dirs [-c config.yaml] log\|cache` | 打印生效的审计/缓存目录（`log_dir`/`image_cache_dir` 缺省后的值）——`vmr.sh` 内部就是问这个 |
| `vmr diagnose [-c config.yaml]` | 比 `check` 的静态预览更进一步：对每个 provider 做 DNS/TLS/代理连通性检查，再发一次真实的最小请求到每个配置的端点，要求对方原样回显一个一次性 token（并发执行，`-test-timeout` 控制单项超时，默认 15s）——拿到 200 但没回显这个 token 会标成警告而不是直接判通过，用来抓那种网关/中转层拿缓存或兜底响应假装成功的情况——并给出标注了检测结果的路由顺序预览（`-no-test-routing` 跳过真实请求，`-json` 供脚本消费；只要有检查失败就以非零退出码结束） |
| `vmr replay -provider NAME <audit.jsonl>` | 用 vmr 自己构造请求的同一条代码路径，从一条审计记录重建并重发请求——`-dry-run` 只打印不发送，`-record path` 把这次回放的结果也写成一条独立的审计记录，`-model`/`-protocol` 可覆盖记录里原有的值，`-stream true\|false` 强制开关流式，`-max-time` 限制上游等待时长。选择要回放哪条记录：`-detail file`（`vmr report` 产出的 `details/*.json` 文件，不用数行）、`-ts <timestamp>`（匹配 `vmr-requests.jsonl` 或原始审计日志里的 `ts` 字段）、`-line N`（默认取文件里最后一条）——三者互斥 |
| `./vmr.sh start\|stop\|…` | dev 模式生命周期（自己监督） |
| `./vmr.sh service install\|uninstall\|start\|…` | init 系统服务（launchd/systemd：崩溃重启、登录自启） |

经路由的响应带 `X-VMR-Endpoint`（实际命中端点）与 `X-VMR-Attempts`（尝试次数）。

```bash
# 怀疑配置有问题——先诊断一遍，而不是对着日志里的 401 干瞪眼。
./vmr diagnose -c config.yaml

# 某个请求失败了，先看看 vmr 本来会发出什么，不真的发送。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    "$(./vmr dirs -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# 同一个请求，真的发送，把上游响应打印到 stdout。
./vmr replay -c config.yaml -provider openrouter \
    "$(./vmr dirs -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# 是在 vmr-requests-index.md / vmr-report.md 里找到的那条失败请求？
# 直接指向它的 details/*.json，不用数行号。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -detail out/details/20260713-153042.100_coding_z-ai-glm-5.2_error.json

# 或者用 vmr-requests.jsonl / vmr-report.md 里看到的精确时间戳来定位。
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -ts 2026-07-13T15:30:42.100+08:00 \
    "$(./vmr dirs -c config.yaml log)/vmr-audit-2026-07-13.jsonl"
```
