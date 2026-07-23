// Ver 2026-07-23 09:00, by Sonnet 5（撤回与 session.go 统一指纹算法、写入审计日志的方案（§2.2）——两边都是简单 md5，为复用而复用没有实际收益，保持独立实现；工作量相应下调）

# 条件路由（Condition-based Routing）与 Sticky Model — 设计方案

本文档是条件路由与 Sticky Model 两项特性收敛后的最终设计，取代此前逐轮反馈产生的分析稿。过程记录（哪一轮用户提出了什么、哪个判断被哪份调研验证）已经大幅压缩，只保留对结论有实质影响的依据；完整的迭代过程如需追溯，见本文件的 git 历史。

两者不是两个独立特性，是同一个调度管线里相邻的两个阶段：

```
健康过滤（既有） → 条件过滤（§1，硬性淘汰） → 优先级/权重排序（既有 Dimension） → 会话亲和重排（§2，软性置顶）
```

条件路由解决"这个端点根本不能处理这条请求"（能力不匹配、上下文放不下）；Sticky Model 解决"这个端点能处理，但换一个端点会打掉正在生效的 prompt cache，不换更划算"——条件路由（尤其上下文长度维度）一旦独立上线而没有 Sticky Model 配套，在长会话场景下可能是净负收益的：上游 prompt cache 按精确字节前缀匹配（设计文档 §7.1 已经强调过这一点），条件路由如果把同一条会话动态换到不同端点，会打掉正在累积的缓存，"选了更合适的模型、总成本反而更高"。两者必须一起设计、按上面的顺序接入。

**贯穿全文的成本原则**：所有和"估算/识别"有关的机制，一律优先用长度、字节数、廉价的存在性标记去推断，而不是解析内容本身；推断可以不精确，代价由 #3（`docs/vmr_next_features_analysis_sonnet-5.md` 里的"上下文超限独立错误类 + failover"，下文简称 #3）兜底——多用一次大上下文模型是可以接受的代价，解析成本和实现复杂度不是。

---

## 1. 条件路由

### 1.1 config.yaml 设计

```yaml
sticky_ttl: 10m                     # 全局默认（§2.3），未设置时套用内建默认值

models:
  openai:
    agent:                          # 虚拟模型名
      strategy: [priority]
      endpoints:                    # 顺序即优先级——不再声明数字型 priority 字段，见下方说明
        - provider: minimax
          model: MiniMax-M3
          capabilities: [text, image, tools]     # 多模态输入 + 是否支持 tool calling
          max_context_tokens: 1000000            # 声明的上下文上限（token 数量级）
        - provider: deepseek
          model: deepseek-chat
          capabilities: [text, tools]             # 不支持图像输入
          max_context_tokens: 128000
        - provider: anthropic_direct
          model: claude-opus
          capabilities: [text, image, tools, thinking]  # 支持 extended thinking
          max_context_tokens: 200000
```

两个新字段，都挂在 `EndpointConfig`（`internal/config/config.go:84-88`）：

* **`capabilities: []string`**——自由字符串数组，覆盖输入模态（`text`/`image`/`audio`/`video`）和动作能力（`tools`/`thinking`），不区分"模态"和"能力"两类字段：在 Condition 框架里它们是同一种事实（"这个端点支不支持 X"）。**未声明该字段 = 不限制**（零配置迁移不受影响）；一旦声明就是穷尽式的（allowlist）——运营者需要把端点真正支持的能力全部列出来，遗漏会导致端点被误判为不支持而被条件过滤挡在候选之外。这是数组式声明的已知代价，缓解手段是 `vmr check` 必须把每个端点声明的 `capabilities`/`max_context_tokens` 原样打印出来，让配置错误在输出里一眼可见。
* **`max_context_tokens: int`**——单个数值，端点声明的上下文窗口上限，未声明 = 不限制。

这两个字段和 §2.4 引入的端点级 `sticky_ttl` 共用同一条设计原则：**任一字段没配置，对应的行为就直接跳过/继承默认，绝不因为字段缺失而产生新的限制**。这是让存量 `config.yaml` 升级后行为不变的唯一方式——三个字段都是本文档新增的，任何在这之前写的配置文件里都不会有它们，必须保证"没有它们"和"升级前的行为"完全一致。

**不再使用 `priority` 字段**：`EndpointConfig.Priority`（`config.go:87`）是既有字段，本文档的示例不再使用它——`endpoints` 本身是有序列表，`strategy.Sort` 的稳定排序保证平手时保留配置文件顺序（设计文档 §11），调整顺序直接挪动 YAML 里的位置即可，不需要再手工维护一个数字、操心冲突和连续性。该字段保留的唯一理由是"未来 `weight`/`latency` 等排序维度（设计文档 §12.2，均未实现）需要表达分层语义"，但目前没有第二个维度需要与它组合，`priority` 100% 与列表顺序功能重叠。要不要把它从 `config.go`/`strategy.go` 里删掉是一个独立于本文档的既有代码变更决定（现有配置文件的迁移成本需要单独评估），本文档只是不再在新示例里使用它，也建议真要保留"逃生舱"就等 `weight` 真正设计时再定分层怎么表达。

### 1.2 架构：Condition 接口

设计文档 §6.1 把调度定义为"过滤 + 稳定多键排序"，但排序维度 `Dimension.Compare(a, b *core.Endpoint) int`（`internal/strategy/strategy.go`）**看不到请求**——priority/weight/latency 这类维度确实不需要，但"这个端点能不能接这条具体请求"是一个新的判断类型，语义上是准入（elimination），不是排序，需要一个新的、请求感知的接口，与 `Dimension` 平行：

```go
// Condition tests whether one endpoint may serve a request at all. Unlike
// Dimension (endpoint-vs-endpoint ordering, no request access), a Condition
// is request-aware and elimination-only.
type Condition interface {
    Name() string
    Eligible(ep *core.Endpoint, facts core.RequestFacts) bool
}
```

`core.RequestFacts`：请求侧廉价、预计算一次的特征集合，仿照 `imgprep.HasImageMarker` 已经验证过的"廉价子串扫描"模式，在 `server.go` 里请求体缓冲完成后算一次，存进 `core.CanonicalRequest`（新增 `Facts RequestFacts` 字段）：

```go
type RequestFacts struct {
    HasImage        bool
    HasAudio        bool  // 骨架预留，见 §1.3①
    HasVideo        bool  // 同上
    HasTools        bool
    WantsThinking   bool
    EstimatedTokens int64 // 见 §1.4
}
```

已注册的 Condition **全部无条件参与过滤**，不需要像 `ModelConfig.Strategy` 那样在配置里声明一份名字列表——Condition 之间是纯 AND 语义、顺序无关，且"端点没声明某个能力字段"天然等价于"对这个条件不设限"，没有 Dimension 那种"要不要参与排序、参与顺序"的选择要做。

接入点（`router.go Serve()`，既有健康过滤循环里加一步）：

```go
for _, ep := range route.Endpoints {
    if !healthy(ep) { continue }                        // 既有：健康过滤
    if !strategy.Eligible(ep, creq.Facts) { continue }   // 新增：条件过滤（§1.5 有一个例外）
    candidates = append(candidates, ep)
}
```

诊断：过滤后候选集为空时，只在这条失败路径上（不在热路径）额外跑一遍，找出是哪个 Condition 淘汰了最后剩下的端点，把原因写进错误消息（如 `rejected by condition "image"`），而不是复用现有"all cooling down or none configured"这句容易误导的话；`vmr check` 同步展示每个端点声明的 `capabilities`/`max_context_tokens`。

### 1.3 四个条件的最终定义

**① 多模态能力**（`image`/`audio`/`video`/`text`，`capabilities` 数组）——`image` 直接复用 `imgprep.HasImageMarker`，零新增探测成本；`audio`/`video` 目前只保留 `RequestFacts` 里的骨架字段，探测逻辑暂不设计——两家协议目前都没有成熟稳定的内联音频/视频输入形状，过早锁定一个探测正则，协议下次小版本更新就要重写，等字段形状稳定后再补，符合设计文档 §5.5 quirk 检测"只在确认命中确切形态才触发"的既有克制原则。`text` 恒真，不需要检测。

**② 上下文长度**（`max_context_tokens`，单个数值）——核心设计约束：**估算宁可偏大，不可偏小**。低估的后果是把请求路由到一个放不下的端点，触发 400，此时 #3 兜底 failover（浪费一次尝试，不是灾难）；高估的后果是跳过了一个其实能处理的端点，代价是次优而非错误。两种误差不对称，估算公式因此必须偏保守。完整估算方案见 §1.4；估算把所有候选都淘汰时的降级规则见 §1.5（本轮新增，弥补了此前设计的一个真实缺口）。

**③ tools**——单一布尔标记就是能检测到的全部，也是需要的全部。原因是架构层次问题：MCP 是客户端/编排层协议，MCP client 在本地把发现的工具翻译成标准的 `tools` 数组格式后才发给 LLM API，到达 vmr 时 MCP 来源的工具和手写声明的工具在线格式完全相同——vmr 物理上看不到这个区别，不需要、也无法进一步细分"MCP 工具"和"原生 function call"。检测：顶层 `tools` 数组非空，复用 `internal/adapter/classify.go` 已有的 `topLevelValues` 顶层 key 定位器（本来给 `RewriteModel`/`RewriteRoles` 用的，检测顶层 key 存在与否是同一类操作）。端点声明 `capabilities: [tools]`。

**④ thinking/extended reasoning**——协议形状按厂商分述：

| 厂商 | 请求侧声明形状 |
|---|---|
| Anthropic | 顶层 `"thinking":{"type":"enabled","budget_tokens":N}`；检测须连 `type=="enabled"` 一起判断（`disabled` 也带这个 key） |
| OpenAI（o 系/GPT-5 系推理档位） | 顶层 `"reasoning_effort":"low"\|"medium"\|"high"` |
| DeepSeek | 不是请求参数，是模型选择（`deepseek-reasoner` vs `deepseek-chat`）——这类厂商不需要本条件，是端点选型问题 |
| MiniMax | 请求侧参数的确切形状本文档未能确认，需要在实现前对照当时最新的官方文档核实 |

检测是协议相关的分支（`router.Serve` 已知当前请求走哪个协议入口），复用 `topLevelValues`。端点声明 `capabilities: [thinking]`。

**（不做）price**：不是"这个端点能不能处理"的问题，是"都能处理时先试哪个"的问题——排序（Dimension）关注点，不是准入（Condition）关注点。设计文档 §6.1 已经把 `Cost` 列为未来的排序维度之一，与这个判断一致，不需要新造 Condition。

### 1.4 估算公式

#### 文本：中英文分系数

英文按主流 BPE 分词器公认的 **4 字符/token**；中文由于各厂分词器效率差异很大（老分词器上比英文贵 60%-180%，新的/中文优化过的分词器只贵 8%-24%），在 vmr 实际对接的厂商分词器细节未知的情况下取偏保守的 **1.5 token/字符**（= 2 UTF-8 字节/token）——比调研到的所有已知分词器的真实开销都高，故意往"多算"的方向走。检测不需要 UTF-8 解码出 rune，单趟扫描原始字节按 `b >= 0x80` 分类即可：

```go
var asciiBytes, wideBytes int64
for _, b := range raw {
    if b < 0x80 { asciiBytes++ } else { wideBytes++ }
}
EstimatedTokens = asciiBytes/4 + wideBytes/2
```

直接扫整个原始请求体字节（含 JSON 结构符号），不特意抽取 message content——JSON 的结构性开销本身也会被计入分子，进一步把估算往偏高的方向推。`wideBytes` 不区分中文/日文/韩文/emoji，全部按同一保守系数估算，是可接受的副作用（CJK 语系 token 密度接近，同样偏安全；emoji/重音字符占比通常很低）。

#### 图片：固定常量，不解析像素

OpenAI 与 Anthropic 都公布了精确的像素-token 公式（分别是 `85+170×图块数` 和 `⌈宽/28⌉×⌈高/28⌉`），但拿到像素宽高需要解码图片头——**v1 明确不做这一步**，直接按检测到的图片数量乘一个固定常量：**每张 3000 token**。这个数字取自 Anthropic 官方数据里"1920×1080 全高清截图"（agent/coding 工具最常见的附件尺寸）在高分辨率档的实测开销 2691 token，留了一点余量但不到硬上限 4784 那么夸张。检测复用 `imgprep.HasImageMarker`，零新增成本。（`imgprep` 在很多场景下其实已经解出了真实像素宽高，理论上可以借用算出精确得多的值，但这依赖降采样或审计恰好开启，不总是可用，且和"避免解析"的成本原则相悖——v1 不做这个优化。）

#### 文档类附件（PDF / DOCX / XLSX 及其他未识别的二进制附件）：按体积折算，不解析内容

**结论先行**：DOCX/XLSX 在 vmr 实际代理的原始 Chat Completions / Messages API 这一层，不会被当作独立的二进制附件编码进请求体——Anthropic 官方文档明确说明其 `document` 内容块不支持 .docx/.xlsx，要求客户端"转换成纯文本后直接放进消息内容"；OpenAI 一侧对非 PDF 文档同样是"只提取文本"。也就是说，DOCX/XLSX 的内容如果出现在到达 vmr 的请求里，早已是客户端预先转换好的纯文本，和普通文本消息没有区别，直接被 §1.4 的文本估算覆盖，不需要专门逻辑。Markdown 本身就是纯文本，同理。

真正需要单独处理的只有 **PDF**（两家协议都原生支持内联 PDF），以及"检测到某种文件附件但格式未识别"这类兜底场景（含万一出现的 DOCX/XLSX 二进制形式）。这里**不做页数解析**（之前的方案里有一版用扫描 `/Type /Page` 标记估算页数的土办法，本轮按"避免解析内容"的原则去掉，直接用体积折算）：

```
EstimatedTokens += (附件字段的原始 base64 字节数) / 20
```

取原始（未解码的）base64 字节长度——由 `topLevelValues`/`skipJSONValue` 定位字段范围后直接读长度，不需要 base64 解码，零解析成本。常量 20 是一个粗略校准：Anthropic 文档给出的 PDF 每页开销区间是 1500-3000 token，一个排版适中的商用文档页大约 50-150KB 原始体积，据此推出的比例在十几到二十出头字节/token 之间，取 **20** 作为统一常量。这个公式不区分 PDF/DOCX/XLSX/未知格式——它们都是压缩的容器格式，字节密度和内容量的关系同样不稳定，用同一个保守常量统一处理，而不是为每种格式单独调一个数字，这也是"低成本"原则的直接体现。检测：与 `HasImageMarker` 同一模式的廉价标记扫描（Anthropic 的 `"type":"document"`、OpenAI 的 `input_file`/base64 数据 URI 等）。

**已知的方向性偏差**：这个公式对文本密集的 PDF 相对准确，对图片/扫描件密集的 PDF 会大幅高估（一页几 MB 的扫描件可能被估成远超真实开销的 token 数）。按 §1.4 开篇的成本原则，这个偏差本身可以接受（无非多倾向大上下文端点）——真正需要处理的是"高估到把所有候选都淘汰"这个边界情况，见 §1.5。

**唯一无法解决的盲区**：客户端如果用 Files API 模式（先上传拿 `file_id`，后续消息只带引用），真正的文件字节根本不在 vmr 看到的请求体里，无法估算——这是结构性盲区，不是方案疏漏，由 #3 兜底。

#### 音频 / 视频：不做数值估算，只做能力判定

两家官方都未公布"每秒对应多少 token"的精确公式（社区流传的 OpenAI 约 32 token/秒是根据定价反推的非官方估算），且从字节里可靠拿到音频真实时长还需要解码容器格式，不是廉价操作。**检测到音频/视频内容时只交给 §1.3① 的能力条件处理（路由到声明支持的端点），不纳入 `EstimatedTokens` 数值**——一个带音频的请求即使因为音频本身很长超出目标端点上下文，仍由 #3 兜底。视频在 OpenAI/Anthropic 当前协议里还不成立（均不支持内联视频输入），暂不设计。

#### 信息来源

调研于 2026-07-23，均为公开网络信息，实现前建议对照当时最新的官方文档二次核实（尤其 OpenAI 图片公式本次未直接抓到官方原文，以及 DeepSeek/MiniMax 各自分词器的中英文效率数据未覆盖，只覆盖了 GPT 系/Qwen/GLM/DeepSeek-V2 的公开对比）：Anthropic 图片/PDF 官方文档（`platform.claude.com/docs/en/build-with-claude/vision`、`.../pdf-support`、`.../files`）；中英文 token 效率对比综合自 `markhuang.ai` 的多分词器实测数据；OpenAI 图片公式、非 PDF 文档纯文本处理、音频定价均来自技术社区二手转述。

### 1.5 估算过高导致候选全部被淘汰时的降级规则（本轮 review 新增）

复核整个方案时发现一个此前没有处理的缺口：`EstimatedTokens` 是一个可能明显偏高的估算值（尤其 §1.4 的文档类附件公式，扫描件场景可能高估几十倍）——如果按 §1.2 的接入点原样实现，一旦估算值超过**所有**候选端点声明的 `max_context_tokens`，条件过滤会把候选集清空，`router.go Serve()` 直接返回"no available endpoint"错误，**这条请求连一次真实尝试都不会发生**，#3 的 failover 安全网根本没有机会介入——因为 #3 依赖一次真实的上游请求返回真实的 400 才能触发。也就是说，"宁可高估"这个安全原则在极端情况下会反噬：估算错得越离谱，反而越可能造成一次本可避免的硬失败，而不是"退而求其次用一个更贵的端点"。

**区分两类条件**：`image`/`tools`/`thinking` 这些能力条件是**确定的**——一个端点要么支持要么不支持，请求是否需要也是确定的，如果所有端点都不支持，尝试没有意义，直接拒绝是正确行为，不需要降级。上下文长度不同——它建立在一个可能出错的估算之上，天生带不确定性，不应该被当成和能力条件同等硬的门槛。

**规则**：条件过滤分两步。先用 `image`/`tools`/`thinking` 等确定性条件过滤出 `hardFiltered`；如果 `hardFiltered` 非空，再用上下文长度条件在其上进一步筛出最终候选集。**如果这一步筛完是空的，但 `hardFiltered` 本身不是空的，直接回退到使用 `hardFiltered`**（即忽略这一次的上下文长度判断，按正常的健康/优先级顺序照常尝试）——宁可让请求真的打到一个"看起来太小"的端点上、交给 #3 兜底，也不要让 vmr 自己凭一个不确定的估算把路堵死。这个特殊处理只放在上下文长度这一个条件上，不推广成 `Condition` 接口的通用能力（没有第二个条件需要这种语义，接口没必要为一个还不存在的需求预先复杂化）。

---

## 2. Sticky Model：会话亲和路由

### 2.1 识别"同一个会话"

**分 API Key 的方案（用户最初提议）**：给不同 Agent 配不同的 vmr API Key，靠 `client_key_tag` 区分。这个方案值得作为独立动作推荐（`client_key_tag` 本来就是为"按调用方分组导出"设计的，见设计文档 §9.4/§4.3，零新增代码，今天就能用），但不能替代真正的会话级识别——同一个 Agent、同一把 Key 下通常仍有多个并发或先后的独立会话，压缩前后属于**同一把 Key 内部**的同一条对话，`client_key_tag` 这个粒度天生看不见这类漂移。

**更优方案：复用 vmr 已经验证过的会话指纹算法**。`internal/report/session.go`（设计文档 §9.4）离线分析对每条审计记录取"第一条非 system 消息"的字节做 md5，作为会话锚点——这个算法已经在真实的大规模审计日志上跑过、验证过。在线版本的改动是把它从"整体 unmarshal 再遍历"改成"只扫描定位目标字节范围，不解析其余内容"，复用 `internal/adapter/classify.go` 里已有的 `topLevelValues`（定位 Anthropic 顶层 `system` 字段）和 `RewriteRoles` 的数组遍历骨架（定位 OpenAI 消息数组里的前导 `system` 消息，扫到第一个非 system 元素就停止，代价只取决于前导 system 消息的大小，不随对话历史增长而变慢）。

**锚点必须包含 system prompt，不能只用第一条用户消息**——如果两个不同 Agent 恰好用相同的话开场（如一句简单的 "hi"），只看第一条用户消息会把两个本该独立的会话误判成同一个。这不只是巧合场景：prompt cache 是从请求最前面开始的字节前缀匹配，system prompt 排在 messages 之前，天然是缓存前缀里最先决定命中与否的一段——两个会话即使后续用户消息逐字相同，只要 system prompt 不同，上游的前缀匹配早就分道扬镳了。`session.go` 的离线算法不需要包含 system（它服务的是报表分组，风险取舍是"漏判比误判更影响可读性"），但 Sticky Model 服务的是路由决策，风险取舍相反，值得多算一段。

**锚点不包含 `tools`**：MiniMax 官方文档确认其缓存前缀匹配顺序是"工具列表 → system prompt → 用户消息"，理论上 tools 也在缓存前缀里。但 `tools` 是结构化数据（JSON 数组），如果 agent 框架内部动态生成/枚举工具列表（比如运行时发现的 MCP 工具），即使逻辑上的工具集合不变，序列化细节也可能变化，导致锚点无谓跳变——这是会实际影响可靠性的风险。`system` 和首条消息都是纯文本，字节天然稳定，没有这个问题。就 OpenClaw 的实际场景而言也不存在"system 相同但 tools 不同"的情况，纳入 tools 换来的判别力没有实际收益。

**一个可以立即验证、零新增代码的诊断建议**：`vmr report` 已经会从消息文本里解析 OpenClaw 的 `chat_id` 字段（`session.go:338-342`），并在 `vmr-requests-index.md` 里按它分组展示。建议在实现 Sticky Model 之前，先跑一次 `vmr report`，看这个分组是否恰好对应已知的不同 Agent——如果是，它是一个比锚点哈希更直接的信号（读一个字段 vs 哈希消息内容），可以作为补充；但它的提取依赖正则扫描 OpenClaw 特定的文本包装格式，比锚点哈希更脆弱，本文档仍然把锚点哈希当作核心机制，`chat_id` 只作为验证或补充。

### 2.2 不与 session.go 强行统一实现，也不落盘

一度考虑把在线（Sticky）和离线（`session.go`）两边的指纹计算收敛成一个共享函数，并把在线算出的哈希写进审计日志供离线复用。复核后放弃了这个想法，两点都不做：

**不共用函数**：两边计算的都是一次简单的 md5，本身没有复杂度可言。`session.go` 早就在自己的整体消息遍历（为了构建 `RoleChars`、工具签名、LCP 匹配用的逐消息哈希等）里顺手算出了它的首条消息哈希，是这次遍历的免费副产品，不需要、也不会因为调用一个外部函数而变得更快——它需要的是已经解析好的消息结构，不是"只扫描定位目标字节范围"这种为在线场景优化的廉价提取。而在线路径恰恰需要避免整体解析。两边在各自的上下文里已经是最简单的实现，**把它们拗成一个共享函数，只会多一层没有实际收益的间接，是为了复用而复用**。两边保持独立实现，互不牵制。

**不落盘**：既然不共用计算，"写进日志给对方读"这个动机也一并消失——`session.go` 自己算这个哈希接近零成本，没有一个真实存在的消费者需要从日志里读取它。除非将来出现一个具体的、独立于 `session.go` 之外的消费者（比如一个不想跑完整会话分析、只想快速按会话分组的轻量脚本），否则这是在为一个假设的需求预先加复杂度，不做。

`internal/report/session.go` 保持完全不动；Sticky Model 自己需要的 `system` + 首条消息哈希，作为它自己的内部实现细节独立存在（复用 `internal/adapter` 里已有的 `topLevelValues`/`RewriteRoles` 字节扫描骨架，理由是这套扫描工具本来就是干这个的，不是为了和 `session.go` "统一"）。

### 2.3 是否需要先按长度预筛、再决定要不要哈希？

结论：不需要，这个优化在当前设计下没有适用的场景。

长度预筛（"长度都不匹配就不必算哈希"）是用来加速"和一大批已存储的候选逐个比较"这类场景的——比如线性扫描一个列表找近似匹配。但 Sticky Model 的查找方式是**哈希表精确查找**：算出自己的 `anchor` 后直接 O(1) 查表，根本不存在"和多个候选逐个比较"这一步，所以没有可供预筛的对象——要不要哈希不取决于别的候选长什么样，取决于本次请求是否需要生成一个 key，而生成 key 本身就是哈希这一步。

其次，哈希本身的开销在这个数据规模下不构成瓶颈：`system` prompt 加首条消息，常见场景是几 KB 到几十 KB，即使遇到把大量静态上下文塞进 system prompt 的极端 agent 框架、涨到一兆字节量级，纯 Go 的 `crypto/md5`（`internal/report/session.go` 已经在用，标准库、无新依赖）在现代硬件上的吞吐量是数百 MB/s 量级，处理 1MB 数据的耗时在个位数毫秒，相对于一次真实的 LLM 请求往返（几百毫秒到几秒）可以忽略不计。这不是一个需要优化的性能问题。

### 2.4 TTL：全局默认 + 端点级覆盖（不是模型级）

初版分析给的默认 TTL（24-48 小时）是按"内存清理"的直觉给的，远超真实 prompt cache 的寿命。调研到的四家官方数据：

| 厂商 | 官方 TTL |
|---|---|
| Anthropic | 默认 **5 分钟**（2026-03-06 起，此前 1 小时）；可显式声明 `cache_control:{type:"ephemeral",ttl:"1h"}` 换成 1 小时（写入成本从 1.25× 涨到 2×） |
| OpenAI | 典型 **5-10 分钟**不活跃后失效，最长 1 小时必清空；新模型系列（GPT-5.6 起）提高到"至少 30 分钟" |
| MiniMax | 复用与 Anthropic 相同的 `cache_control` 机制，具体 TTL 数值未在官方文档中直接确认，推测同量级，建议实现前二次核实 |
| DeepSeek | 磁盘缓存，"未使用数小时到数天后才清理"，无固定 TTL、无手动配置项——是四家里最长寿的一档，量级与其他三家差 2-3 个数量级 |

**TTL 应该挂在端点上，不是虚拟模型上**——这是用户这轮提出的修正，判断是对的：cache 寿命是上游 provider 的属性，不是虚拟模型的属性。一个虚拟模型完全可以同时挂 MiniMax（5 分钟量级）和 DeepSeek（数小时到数天）两个端点作为优先级 fallback，模型级的单一 TTL 没法同时表达两者，此前的方案里为了绕开这个问题，不得不把 DeepSeek 端点拆成一个单独的虚拟模型（`cheap-deepseek`）才能给它配一个更长的 TTL——这本身就是"配置层级选错了"的信号。改成端点级之后，同一个虚拟模型可以直接把两者放在一起，各自声明各自的 TTL，不需要再人为拆分。

配置结构：全局 `sticky_ttl`（顶层字段，缺省 = 内建默认 10 分钟，覆盖 Anthropic 的 5 分钟下限和 OpenAI 的 5-10 分钟典型区间），`EndpointConfig` 上再开一个可选的 `sticky_ttl`（`*time.Duration`，`nil` = 继承全局，非 nil = 该端点自己决定）——两层结构不变，只是挂载的层级从 `ModelConfig` 移到 `EndpointConfig`，`BuildSnapshot` 阶段就地解析成 `core.Endpoint.StickyTTL time.Duration`（已解析好的具体值，和 `Priority`/`RoleMap` 走同一条路径，不需要运行时再算一次"生效值"）。主要挂 DeepSeek 的端点应该显式声明 `sticky_ttl: 2h` 才能吃到磁盘缓存的真实收益。

**淘汰机制需要拆成两件事，不再是"一个 TTL 服务两个目的"**：上一版的结论"一个 TTL,不是两个"是在"只有一个候选 TTL 值"的前提下成立的——现在 TTL 变成按端点各自declare，一个 Registry 里同时装着 5 分钟量级和数小时量级的条目，**内存淘汰**（多久没用的记录该从 map 里彻底删掉）和**粘性有效性判定**（这条记录距今是否还在对应端点的 TTL 窗口内）这两件事不能再共用同一个数字：判定粘性有效性时必须用**这条记录当时指向的那个端点**自己的 TTL；内存淘汰则用一个统一的、比任何端点 TTL 都宽松的兜底值（建议 24 小时——比 DeepSeek 磁盘缓存的"数天"上限保守，但足够覆盖所有已知厂商的合理窗口），复用设计文档 §7.1 图片降采样缓存已经验证过的"mtime 刷新 + TTL 淘汰"模式，只是这里的 TTL 是兜底清理用的粗粒度值，不是路由决策用的精确值。

来源：Anthropic `platform.claude.com/docs/en/build-with-claude/prompt-caching`；OpenAI `developers.openai.com/api/docs/guides/prompt-caching`；MiniMax `platform.minimax.io/docs/api-reference/text-prompt-caching`（TTL 数值未直接确认）；DeepSeek `api-docs.deepseek.com/guides/kv_cache/`。调研于 2026-07-23，实现前建议二次核实 MiniMax 的具体数值。

### 2.5 架构落地

新增一个独立小包 `internal/sticky`（与 `internal/health` 平行，不塞进 `internal/strategy`——亲和性是独立的运行时状态概念，不是排序/过滤维度）。`Registry` 本身不需要知道任何端点/TTL 的细节——它只是一个带 mtime 的键值存储，"这条记录对不对应端点的 TTL 而言还有效"这个判断留给调用方（`router.go`），因为只有调用方在查到 `endpointKey` 之后才知道该用哪个端点的 TTL：

```go
type Registry struct { /* map[string]entry + mutex，仿 health.Registry 结构 */ }

func (r *Registry) Peek(key string) (endpointKey string, lastUsed time.Time, ok bool)
func (r *Registry) Set(key, endpointKey string)  // 命中时刷新 mtime，用于兜底清理
```

`Sticky` 开关默认打开，不是默认关闭——这也是这轮的修正：既然 §2.3 已经确认哈希本身的开销可以忽略不计，"默认关闭、需要显式声明才开启"就不再是必要的保护措施，反而会让大多数真正受益的多轮 agent 场景需要多写一行配置才能拿到默认应有的行为。让"不需要 sticky"的少数场景显式声明更符合"服务多数场景"的默认值原则。字段类型用 `*bool`（`config.ModelConfig.Sticky *bool`）而不是发明一个否定式的字段名（如 `no_sticky`）——`nil`（配置里没写）= 默认开启，显式 `false` = 关闭，显式 `true` = 开启（冗余但合法）。这和 `ImageDownscaleMaxPx *int`/`StickyTTL *time.Duration` 是同一套"指针语义表达三态"的既有模式，比新造一个否定式字段名更一致，也不需要用户在"什么时候写 true、什么时候写 false"上多想一层。

接入点（`router.go Serve()`，紧接条件过滤和既有排序之后）：

```go
route := snap.Models[protocol][creq.Model]
...
candidates := filterByHealthAndConditions(route.Endpoints, creq.Facts)  // §1，含 §1.5 的降级规则
strategy.Sort(candidates, route.Dims)                                   // 既有排序

if route.Sticky {  // 已在 BuildSnapshot 阶段按 *bool 解析成具体值，nil 视为 true
    if sysHash, firstMsgHash, ok := adapter.SessionFingerprint(creq.Raw, protocol); ok {  // §2.2：Sticky 自己的内部实现，不写审计日志、不与 session.go 共享
        key := clientTag(rec) + ":" + hex.EncodeToString(sysHash[:]) + ":" + hex.EncodeToString(firstMsgHash[:])
        if epKey, lastUsed, found := rt.Sticky.Peek(key); found {
            if ep := findByHealthKey(candidates, epKey); ep != nil && time.Since(lastUsed) < ep.StickyTTL {
                moveToFront(candidates, ep)  // 只在 candidates 里找，找不到（已被过滤淘汰）就什么都不做
            }
        }
    }
}
```

`findByHealthKey` 本来就是 `moveToFront` 需要的查找步骤，不是额外开销——"用哪个端点的 TTL 判定有效性"这一步是顺带完成的，不需要单独一次查找。

三个关键设计点不变：(1) 亲和性只在已经通过健康 + 条件过滤的候选集里生效；(2) 每次成功完成请求（含 failover 后成功）都更新粘性指针，是自愈设计；(3) Compaction 场景下机制仍然成立（分析见上一版，结论不变：压缩本身就会让 cache miss 一次，与 vmr 选哪个端点无关；压缩后的后续轮次共享新锚点，粘性照常生效；`system` prompt 在压缩前后通常不变，是锚点里更稳定的一段）。

### 2.6 config.yaml 示例

```yaml
sticky_ttl: 10m                     # 全局默认，覆盖 Anthropic/OpenAI/MiniMax 的典型区间

models:
  openai:
    agent:                          # sticky 默认开启，不用写 sticky: true
      strategy: [priority]
      endpoints:
        - provider: minimax           # 跟随全局 10 分钟，不用显式写 sticky_ttl
          model: MiniMax-M3
          capabilities: [text, image, tools]
          max_context_tokens: 1000000
        - provider: deepseek          # 磁盘缓存，寿命远超全局默认，端点级显式覆盖
          model: deepseek-chat
          sticky_ttl: 2h
          capabilities: [text, tools]
          max_context_tokens: 128000

    one-shot-summarizer:              # 单次摘要调用，没有多轮价值，显式关闭 sticky
      strategy: [priority]
      sticky: false
      endpoints:
        - provider: minimax
          model: MiniMax-M3
          # 未声明 capabilities/max_context_tokens：两者都不设限，兼容未升级的旧配置文件
```

最后一个端点没有声明 `capabilities`/`max_context_tokens`，直接体现 §1.1 已经确立的原则：**这两个字段任一没配置，对应的条件就直接跳过，不产生任何限制**——连同这里的端点级 `sticky_ttl`（未声明 = 继承全局），三个可选字段用的是同一套"缺省 = 不改变行为"的语义，这正是让存量 `config.yaml`（完全不知道这些新字段）在升级后行为不变的原因，不需要用户为了兼容性做任何迁移。

---

## 3. 工作量与建议

| 项 | 估算 |
|---|---|
| 条件路由框架（Condition 接口 + 注册表 + RequestFacts + Serve() 接入 + 诊断消息 + `vmr check` 展示） | 1-1.5 人天 |
| `image` 能力 | 0.3 人天 |
| `tools` 能力 | 0.3 人天 |
| 上下文长度（含中英文分系数、附件按体积估算、§1.5 降级规则） | 0.6-0.8 人天 |
| Sticky Model（`internal/sticky` 包 + `adapter.SessionFingerprint` + `Serve()` 接入 + 端点级 TTL/`*bool` 开关配置 + 测试） | 1.7-2.1 人天 |
| **合计** | **约 3.9-5.0 人天** |

`thinking`/`audio`/`video` 三项因协议形状未完全确认（MiniMax thinking 参数、两家的音视频输入格式），本轮不给工作量估算，待核实后再排期。`internal/report/session.go` 保持不动，不产生任何工作量（§2.2）。

**建议顺序**：条件路由框架 + `image`/`tools`/上下文长度，与 Sticky Model 作为一次连续迭代实现，不要间隔太久上线——两者在 `router.go Serve()` 里是相邻阶段，理由见文档开头。开工前建议先做两件零成本的事：(1) 跑一次 `vmr report` 核实 `chat_id` 分组是否符合预期（§2.1）；(2) 确认 MiniMax 的 `thinking` 请求参数形状（如果第一期就要覆盖这项能力）。`priority` 字段是否要从代码里移除，是一个独立的、需要单独评估迁移成本的决定，不在本次工作量里。
