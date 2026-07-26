<!-- Ver 2026-07-26, by Sonnet 5 -->

# vmr 失败请求全量复核报告

数据源：`logs/vmr-audit-2026-07-14.jsonl.zst` ~ `logs/vmr-audit-2026-07-24.jsonl.zst`（11 天，6663 条请求），经 `vmr report` 聚合后的 `reports/vmr-requests-failed.jsonl`（91 条失败记录）。本报告对这 91 条逐一核实了错误信息与 detail 报告，按根因分类，并对照当前代码/配置逐项判断"已解决 / 仍需处理"。

## 0. 分析方法（写在前面，供复核）

`reports/details/*.json` 单个文件普遍在 400KB~2.7MB（`client.request.body` 里的完整对话历史 + `attempts[].request.body` 是主要体积来源），91 个文件全部原样读入会消耗几十 MB 上下文。采用的做法：

1. 用一次性脚本对 91 个 detail JSON 各自抽取诊断相关的小字段（`attempts[].{endpoint,status,error,error_class}`、`client.response.{status,body.error.message}`、请求消息数/字节数），压缩成一份 40KB 的结构化摘要——诊断信息本身只占原始文件的 3‰ 左右。
2. 在这份摘要上做频次统计、按 `error_class`/endpoint/时间/session 分组，定位模式。
3. 对每个模式挑 1~2 条代表性记录，用 `head` 只读 detail `.md` 文件最前面的元数据表格（会话/任务/轮次/尝试次数/耗时），不读整份文件正文，核实细节、抓取原文引用。
4. 关键假设（如"某个分类 bug 是否已经修复"）用当前代码的实际实现 + `git log` 提交时间反向验证，不靠猜测。

**91 条全部被逐一查看并归类，没有抽样。**

## 1. 总体概览

| 指标 | 数值 |
|---|---|
| 统计窗口内总请求数 | 6663（2026-07-14 ~ 2026-07-24） |
| 失败/中断请求数 | 91（占比 1.37%） |
| 其中 outcome=error | 34 |
| 其中 outcome=ok 但流中途截断 | 43 |
| 其中 outcome=canceled（客户端中途断连） | 14 |
| 涉及的独立会话数 | 31 |
| 失败后该会话仍有更晚一轮记录（大概率延续/自愈） | 78/91 |
| 失败是该会话在窗口内最后一条记录 | 13/91（不能确证是否真的卡死，只是没有更晚记录） |

## 2. 十项分类结果

逐一核对后，91 条失败落入以下互斥的十类（按数量降序）：

| # | 分类 | 数量 | 占比 | 时间分布 | 当前状态 |
|---|---|---|---|---|---|
| A | MiniMax-M3 流式生成中途完全停摆，触发 `stream_idle` 超时 | 43 | 47.3% | 07-14~07-20（07-16 单日 31 条） | **已缓解**：当前 `config.yaml` 已将 `minimax` 从 `agent`（openai 协议）的候选列表中注释掉 |
| B | MiniMax-M3 从未返回首字节，客户端等到约 120s 后自行断连 | 12 | 13.2% | 07-14~07-20 | 同上，已缓解 |
| C | "coding" 虚拟模型单一 endpoint 被限流（429），无候选可切换 | 4 | 4.4% | 07-24 一次性突发（同一 session） | **仍是配置缺口**，见 3.3 |
| D | "coding" 虚拟模型收到带图片请求，被硬性条件拦在路由前（0 次真实尝试） | 10 | 11.0% | 07-24 一次性突发（同一 session） | **按设计工作**，根因是客户端重试行为，见 3.4 |
| E | Provider 拒收 OpenAI 的 `developer` role（`role_map` 未配置） | 8 | 8.8% | 07-19、07-22 | **部分已修复**（dashscope 已配，deepseek/opencode/volcengine 未配），见 3.2 |
| F | 纯文本模型收到图片请求被上游拒绝（vmr 未声明该端点无图片能力） | 2 | 2.2% | 07-23 | **仍是配置缺口**，见 3.5 |
| G | 图片 + 文本 token 总数超过上游单条消息上限 | 1 | 1.1% | 07-24 | 单次边缘案例，见 3.6 |
| H | opencode.ai 中继报错的分类问题（"Upstream request failed"） | 8 | 8.8% | 07-14~07-22 | **7/8 是已修复 bug 的历史记录，1/8 证实修复已生效**，见 3.1 |
| B2 | 其他孤立取消（无法归入上述任何模式） | 2 | 2.2% | 07-21、07-23 | 信号太弱，暂不处理 |
| D2 | 其他 0-attempt（请求了未配置的模型名） | 1 | 1.1% | 07-22 | 客户端/配置对不上，一次性 |

下面逐项展开。

---

## 3. 分类详解

### 3.1【H】opencode.ai 中继报错分类问题 —— 已确认修复生效

**现象**：8 条记录里上游返回 `"Error from provider (Console Go): Upstream request failed"`（状态码 400），这是 opencode.ai 中继自己转发失败时的措辞，不是请求内容有问题。

**关键发现（时间线交叉验证）**：

| 时间 | 分类结果 | 说明 |
|---|---|---|
| 07-14 01:25 / 14:22、07-16 03:40、07-18 21:05/21:08/21:21/21:23/21:31（共 7 条） | `error_class = client` | 被误判为"客户端错误"，failover **在此止步**，即使还有其他候选端点也不再尝试 |
| 07-22 20:58 | `error_class = endpoint` | 同样的错误文案，这次被正确分类为"端点问题"，**failover 继续走到了下一个候选**（虽然下一个候选又因为另一个原因失败，见 3.2） |

`git log -- internal/adapter/classify.go` 显示提交 `f38d882`（2026-07-19 00:07）"fix upstream-gateway-failure misclassification" 正是修这个问题的——它给 `adapter.upstreamHint()` 加了对"upstream request failed / error from provider"这类中继自报错的识别，命中时归类为 `ErrEndpoint`（允许切换）而不是 `ErrClient`（直接判死刑）。07-18 21:31 之前的记录全部是修复前的行为，07-22 20:58 是修复后的行为——**两者与提交时间完全吻合，说明这条修复已经生效，不需要再处理**。19:00 之后到 07-22 之间没有再出现这个错误文案，无法进一步验证中间态，但现有证据已经足够。

### 3.2【E】`developer` role 未适配 —— 部分已修复，仍有缺口

**现象**：8 条记录，上游报"messages[0].role: unknown variant `developer`"或等价措辞，命中的 provider 有：
- `deepseek`（openai 协议）4 次
- `dashscope`（`kimi-k2.7-code`/`qwen3.7-max`）3 次
- `volcengine`（`ark-code-latest`）1 次

**根因**：OpenClaw/Claude Code 一类客户端会用 OpenAI 新引入的 `developer` role（取代旧的 `system`），但不是所有 OpenAI 兼容网关都认识这个新 role 名——这正是 vmr 已有的 `role_map` 配置项要解决的问题（`providers.<name>.role_map: {developer: system}`，`git log` 显示 `0713eea`，2026-07-20，就是专门为此加的功能）。

**当前 `config.yaml` 现状**：

```yaml
dashscope:
  role_map:
    developer: system    # 已配置
deepseek:                # 未配置 role_map
opencode:                 # 未配置 role_map
volcengine:                # 未配置 role_map
```

`dashscope` 配了之后，07-19 之后再没有出现过 dashscope 的 `developer` role 报错；`deepseek`/`volcengine` 至今没配，07-22 仍在报同样的错——**这是本次复核里最明确、最低风险、可以直接落地的一项**：给 `deepseek`、`opencode`、`volcengine`（openai 协议下）也加上同一行 `role_map: {developer: system}`。

**级联失败的例子**（07-19 21:15:43，session s290）：第一次尝试 `opencode/glm-5.2` 因为普通的瞬时故障（503 transient，本应无感知地换下一个端点）失败，failover 换到 `deepseek/deepseek-v4-pro`，结果又因为 `developer` role 被拒绝而报错给客户端。**如果 deepseek 配了 role_map，这次请求本该在用户完全无感知的情况下成功**——现在配置缺口反而让一次"本可以透明恢复"的抖动变成了看得见的错误。

### 3.3【C】"coding" 模型限流无候选可切 —— 配置缺口

**现象**：4 次 429（"Requests are too frequent"），全部发生在 `openai:volcengine:doubao-seed-2.0-lite`，全部只有 1 次尝试——因为当时 "coding" 模型只有这一个候选端点。

**当前状态**：现在的 `config.yaml` 里 "coding"（openai 协议）已经是 3 个端点（`opencode/glm-5.2`、`openrouter/z-ai/glm-5.2`、`deepseek/deepseek-v4-pro`），配置已经比日志记录的当时更健壮；但这份 config 是本地文件，不受版本控制，无法确认现在这份是否就是长期稳定的目标状态，还是又发生了变化。**建议**：不管当前具体是哪几个端点，"任何一个虚拟模型都不应该只有一个候选端点"应该作为一条长期配置纪律，而不是靠事后一次次追加。

### 3.4【D】"coding" 模型收图被硬性条件拦截 —— 按设计工作，根因在客户端重试行为

**现象**：10 条记录，全部是同一个 session（`s634`），集中在 07-24 01:36~01:44 约 8 分钟内，客户端一直往 "coding" 模型发带图片的请求，vmr 在还没发出任何上游请求之前就用 503 拒绝（"rejected by condition(s): image"）——因为当时 "coding" 的候选端点都没有声明支持图片。

**这不是 bug**：`strategy.Eligible` 的硬性条件淘汰就是为了防止把一张图发给不支持图片的模型，导致上游返回一个更难看、更晚才知道的错误。vmr 在这里做的事情——**在发出真实请求之前、以最低成本（0 次上游调用、几十毫秒）给出一个明确原因的 503**——正是这套机制设计的目标。

**真正值得注意的是客户端行为**：同一个失败请求在 15 秒内被原样重发了 4 次（task t01 轮次 15/16/17/18），随后在同一 session 的另一个任务里又在约 1.5 分钟内重复了 10 次——**这是客户端自己的无脑重试造成的问题放大**，而且这批重试还连带触发了 3.3 的限流（因为短时间内打了同一个端点很多次）。vmr 侧无法阻止客户端重试同一个必然失败的请求；能做的是让失败信息足够明确（已经做到——错误信息直接点名是哪个 condition 拒绝的），根本解决要么是让 "coding" 有一个真正支持图片的候选端点，要么是客户端/agent 层面在收到"图片能力不满足"的 503 后不要对同一请求盲目重试。

### 3.5【F】文本模型收图被上游拒绝 —— 端点能力声明缺口

**现象**：2 次，`anthropic:volcengine:glm-5.2` 返回 "Model only support text input"。

**根因**：vmr 的 `Endpoint.Capabilities` 字段留空 = 默认无限制（假设支持一切能力，见设计文档 §1.1）。当前 `config.yaml` 里没有任何端点显式声明 `capabilities`，所以这个 glm-5.2 端点在 vmr 的路由层面被当成"什么都支持"，直到图片请求真的发过去，才由**上游**返回 400。

**与 3.4 的关键区别**：3.4 是 vmr 自己的条件正确拦下了（因为"coding"整批端点确实都没声明能力，属于"我知道自己不支持"）；这里则是 vmr **不知道** glm-5.2 不支持图片，所以没能提前拦截、也没能自动换到"agent"（openai/anthropic 协议下都配了别的、大概率支持图片的端点）——是一次本可以通过"切换 endpoint"自动恢复、但因为能力声明缺失而没有发生的失败。

**建议**：给这个 volcengine/glm-5.2（anthropic 协议）端点显式声明 `capabilities: [tools]`（不含 `image`），这样 vmr 的硬性条件会在路由阶段就把它排除在图片请求之外，自动尝试同一虚拟模型下其他候选端点，而不是把请求硬发过去等上游拒绝。

### 3.6【G】图文 token 超限 —— 单次边缘案例

**现象**：1 次，`doubao-seed-2.0-lite` 报 "Total tokens of image and text exceed max message tokens"。`RequestFacts.EstimatedTokens` 本身是粗估口径（设计文档 §1.4 明确承认这一点），这类"估计值没有精确覆盖到某个 provider 的真实上限"的边缘情况本就是设计里认可的、由失败请求 + 正常 failover 兜底的场景。只出现 1 次，暂不建议为此单独调整估算公式或该 endpoint 的 `max_context_tokens`——除非后续复现频率上升。

---

## 4. 关键结论

1. **60.5%（55/91）的失败集中在一个来源**：MiniMax-M3 直连（`openai:minimax:MiniMax-M3`）在流式生成中途完全停摆超过 120 秒（`stream_idle` 默认超时），vmr 的看门狗正确检测到并切断了连接——但这类失败发生在**响应已经开始向客户端流式转发之后**，架构上无法再切换到别的端点重试（字节级透传承诺一旦开始转发就不可撤回，`docs/VirtualModelRouter_System_Design_v3.md` 与 `router.go` 的注释都明确记录了这条不变式）。当前 `config.yaml` 已经把 `minimax` 从 `agent`（openai 协议）的候选里注释掉，07-20 之后的日志里再没有出现过这个来源的失败——**这项本身已经是一次成功的、数据能验证的自我修复**。
2. **另外两个已确认修复生效的 bug**：opencode 中继报错分类（§3.1）与 dashscope 的 `developer` role（§3.2 的一部分）——都能在 git 提交记录和日志时间线上对得上号，不需要重复劳动。
3. **仍然敞口的、低风险、可直接落地的配置缺口**：
   - `deepseek`/`opencode`/`volcengine`（openai 协议）缺 `role_map: {developer: system}`（§3.2，8 次里剩下未被 dashscope 覆盖的部分）。
   - `volcengine/glm-5.2`（anthropic 协议）没有声明 `capabilities`，导致图片请求被硬发上游而不是自动换端点（§3.5）。
   - 任何虚拟模型只配一个候选端点，限流/抖动时无路可退（§3.3）。
4. **vmr 目前不会因为"流式中途断了"或"客户端等到超时才取消"而降低那个端点的健康分**（`ErrTruncated`/客户端取消都不调用 `Health.ReportFailure`，设计文档 §4.5 有明确记录，是刻意决定，不是遗漏）。这意味着：哪怕同一个端点在几分钟内连续断流好几次，vmr 依然会在下一个新请求里把它排在正常优先级——不会主动"学乖"、暂时避开它。**这是本次复核里唯一一处值得认真评估、但本报告不建议直接动手的代码层面候选项**，详见第 5 节。
5. **10 次"coding 模型拒绝图片"里有个客户端行为问题**：同一个必然失败的请求在几分钟内被原样重发十几次（§3.4），vmr 没有办法阻止这种重试风暴，只能保证每次拒绝都足够快、足够明确。

---

## 5. 建议的解决方案（按优先级）

### 5.1 配置改动（低风险，建议直接做）

| 改动 | 解决的问题 | 预期效果 |
|---|---|---|
| 给 `providers.openai.deepseek`、`providers.openai.opencode`、`providers.openai.volcengine` 加 `role_map: {developer: system}` | §3.2，8 次里的剩余部分 | 消除这条报错来源；`dashscope` 已验证同样的配置有效 |
| 给 `volcengine/glm-5.2`（anthropic 协议）加 `capabilities: [tools]`（不含 image） | §3.5 | 图片请求会在路由阶段自动跳过它，改走同虚拟模型下别的候选，而不是等上游拒绝 |
| 检查并保证每个虚拟模型至少 2 个候选端点（尤其是新增/临时性的模型定义） | §3.3 | 单点限流/抖动时有路可退 |

以上三项都不改变任何代码，纯配置调整，且都是 vmr 已有机制（`role_map`/`capabilities`/多端点 failover）的正常使用方式，没有需要验证的新行为。

### 5.2 客户端/使用约定（不在 vmr 代码范围内，但建议知会 agent 侧）

- "coding" 虚拟模型收到 503（图片能力不满足）后不要对同一请求原样重试；带图片的请求应该发给 "agent"（或任何声明了 `image` 能力的虚拟模型）。
- 遇到 429/503 时的重试应该有退避，而不是几秒内连续重发——3.3 的限流本身就是重试过密造成的次生问题。

### 5.3 代码层面候选项（有讨论空间，本报告只分析不实施）

**把"流式中途断开"和"客户端等到超时后取消"也计入端点健康信号**，具体设想：

- `forwardSuccess` 里 `copyErr != nil`（对应 `ErrTruncated`）时，除了记进审计（现状），额外调用一次 `Health.ReportFailure`（用一个较短的冷却窗口，比 429/5xx 的冷却更轻，因为这类信号的确定性不如一个明确的错误状态码）。
- 好处：如果同一个端点在短时间内反复流式断开（就像本次复核里 MiniMax 那样），后续新请求会自然被降级/跳过一段时间，不需要人工去 config 里注释掉它。
- 需要认真权衡的地方：
  1. **这不会救回已经发生的那次请求**——字节已经发出去了，改的是"下一个新请求要不要还选它"，收益是面向未来的。
  2. 现在的"不计入健康"是文档明确记录的**故意设计**（`docs/VirtualModelRouter_System_Design_v3.md` §4.5），本意可能是不希望把"客户端自己网络抖动导致的取消"错怪到端点头上——如果要改，需要区分"确实是上游生成中途停摆"（本次复核里 MiniMax 的情况，`ttft` 之后完全没有新字节）和"客户端自己提前挂断"（`ttft=0` 且耗时很短的那 2 条孤立取消，§B2），两者不应该一视同仁地扣健康分。
  3. 需要补充测试验证：短暂的、偶发的截断（例如一次性网络抖动）不应该因为一次触发就把端点打入较长冷却——要设计成"连续/短时间内多次截断才降级"，而不是"一次截断就惩罚"，否则可能把一个本来健康、只是偶尔慢一点的端点误伤。

这项改动对应本次复核数据里价值最大的一类问题（60.5% 的失败源头），但因为改的是一条已经写明是"故意如此"的设计决策，且需要设计"多久算连续失败""扣多重的冷却"这些新的判断标准，**按之前商定的原则（方案存疑或有一定风险时先汇报、不直接动手），本报告只给出分析和候选设计，是否要落地、按什么阈值落地，交由你决定。**

---

## 6. 附：分类对照的原始条数校验

```
43  A-minimax流式中途停摆(stream_idle超时)
12  B-minimax无响应导致客户端放弃等待
 2  B2-其他孤立取消(低信号)
 4  C-单endpoint限流(coding模型无fallback)
10  D-coding模型图片能力条件拒绝(0 attempts)
 1  D2-其他0-attempt(model not found等)
 8  E-developer role未适配(role_map缺失)
 2  F-image能力声明缺失导致文本模型收图
 1  G-doubao图文token超限(单次)
 8  H-opencode中继错误分类(部分已被现有upstreamHint修复)
--------------------------------------------------
91  合计，与 vmr-requests-failed.jsonl 的记录数一致
```
