
### 二、TOP  功能拓展推荐（按用户价值从高到低排序）
所有推荐均严格遵循 vmr 的核心设计原则：不破坏字节保真、不引入冗余依赖、不偏离“Agent 侧透明路由器”定位，同时解决真实用户的高频痛点。

#### TOP 1：多模型成本核算与费用统计报表
**功能描述**：在现有 `vmr report` 的 token 用量统计基础上，增加可配置的模型单价价目表，自动生成按厂商、模型、日期、会话维度的费用统计报表，输出总费用、成本占比、用量趋势等数据。
**解决的核心痛点**：当前仅能统计 token 输入输出量，无法直接换算成真实货币成本；多账号、多厂商混用时，用户需要手动对照各家单价对账，核算成本极高；Agent 场景 token 消耗大，成本管控是用户最核心的诉求之一。
**用户价值**：
- 零额外操作即可从审计日志直接产出成本账单，大幅降低多厂商用量对账成本。
- 可延伸出预算阈值告警、单会话/单任务成本核算能力，完美适配 Agent 无人值守场景的成本管控需求。
**适配性**：纯离线统计能力，完全不侵入请求链路，不破坏字节保真原则，符合 vmr 工具化定位。

#### TOP 3：端点级精细化流量控制（单端点 RPM / 并发限制）
**功能描述**：在现有全局并发闸的基础上，支持为每个上游端点单独配置 RPM（每分钟请求数）上限、并发数上限，主动限流排队，避免单厂商速率限制被打满触发 429。
**解决的核心痛点**：当前只有全局并发上限，不同厂商/账号的速率限制差异很大，高并发下容易把低配额账号打穿，导致频繁触发限流冷却，反而降低整体吞吐；被动故障转移始终是事后补救，主动限流能大幅减少失败次数。
**用户价值**：
- 从“失败了再切换”升级为“主动规避失败”，显著提升整体请求成功率和吞吐稳定性。
- 适配多账号混部场景：高配额账号跑主流量，低配额账号做备用，通过限流各自匹配厂商限速规则。
**适配性**：调度层能力增强，不修改请求内容，完全符合透传原则；是对现有故障转移体系的前置补充。

-----



# TOP1：Policy Router（★★★★★）

这是我最推荐的。

目前 Router 主要依据：

* priority
* health
* cooldown
* protocol

其实还缺少：

> **请求属性路由。**

例如：

```yaml
if:
    prompt_tokens > 50000

route:
    deepseek-v4

---

if:
    has_image

route:
    gemini

---

if:
    tools_count > 5

route:
    claude

---

if:
    model == coding

route:
    kimi-k2
```

或者

```
reasoning=true

↓

DeepSeek

reasoning=false

↓

GPT-4o-mini
```

本质就是

Rule Engine。

---

为什么价值高？

因为 Agent 最大的问题就是：

**不是所有请求都应该用一个模型。**

用户真正想要的是：

> 一个 Virtual Model 自动选最合适模型。

这仍然属于 Router。

不是 Workflow。

---

# TOP2：Cost-aware Router（★★★★★）

现在 VMR 基本不关心钱。

但现实中：

OpenRouter

每分钟价格都可能变化。

不同 Provider：

```
Claude

$15

OpenRouter

$14

MiniMax

￥6

Volcengine

￥3
```

Router 应该知道：

```
成本

↓

Latency

↓

Quality

↓

Availability
```

用户可以写：

```
optimize: cost
```

或者

```
max_price_per_1m: 3$
```

Router 自动选。

---

进一步：

预算。

例如：

```
本月预算：

100 RMB
```

超过以后：

自动降级。

---

这是所有个人开发者都会喜欢的。

---

# TOP3：Capability Router（★★★★★）

目前 Virtual Model 更像：

```
coding
cheap
claude
```

未来可以进一步抽象：

例如：

```
vision

ocr

long_context

reasoning

fast

cheap

creative

translation
```

然后 Provider 注册：

```
DeepSeek

supports:
    reasoning
    128k

Claude

supports:
    vision
    tools

Gemini

supports:
    vision
    video
```

用户：

```
model:
    vision
```

Router 自动选。

以后 Provider 更新：

不用改客户端。

---

价值非常高。

因为：

Agent 真正需要的是：

**Capability**

不是

Provider Name。

---
1. Cost accounting：把 token 统计换算成钱

用户痛点：多 provider 路由的第一动机就是成本套利，但 vmr 现在只能告诉你"用了 94.4M input token、cache hit 92%"，不能告诉你"这个月花了多少、cache 帮你省了多少、同一批 workload 换到 endpoint B 会便宜多少"。

为什么是第一：数据已经全在审计里了（含 tokens_in_cached/cache_write 的两家归一化，这是最难的部分），唯一缺的是一张外部价目表 + report 里几列。设计文档 §12.2 连 key 格式约束都定好了（protocol:provider:model 冒号分隔）。这是全清单里 收益/成本比最高 的一项，没有之一。

范围建议：只做报表侧（vmr report 加成本列 + 一个 prices.yaml），不要顺手做 cost 排序维度。价目表会漂移，让它只影响事后报表是安全的；让它影响实时路由，就等于把一个会过期的外部文件塞进主路径。
---

2. 软拦截（HTTP 200 + 空/替换内容）升级为可 failover 的失败

用户痛点：这是国内厂商特有的最恶劣失败形态——agent 收到 200，内容是空的或被替换，上层完全无感，然后基于一个空回复继续往下跑。深夜无人值守时，这比直接 429 危险得多，因为 429 会 failover，软拦截不会。

现状：§5.5 已经能嗅探 input_sensitive/output_sensitive 并记 soft_block_detected，但"仅观测、不干预"。观测阶段的目的是"先把频率变成可量化的数字"——如果你的审计里这个标记已经出现过若干次，第一阶段的使命就完成了，可以进入第二阶段。

范围建议：只做 失败判定 + failover，不做 §12.1 规划的敏感词替换插件。判据收窄到"标记命中 且 有效内容为空/极短"，归入 ErrContent 的语义（切换但零冷却惩罚——因为端点本身健康）。词库替换那条路涉及改写请求正文，与 byte-faithful 摩擦大、词库维护是无底洞，建议永久搁置。

注意：流式场景下这个标记可能在首字节之后才出现，那时已经不能 failover。所以这一项的现实覆盖面是"非流式 + 流式早期"，要在文档里说清楚，别承诺做不到的事。

3. 上下文超限（context length exceeded）作为独立错误类，触发降级 failover

用户痛点：agent 长会话跑到某个点，请求体超过便宜端点的 context 上限，厂商返回 400。按现在的分类逻辑，400 过完内容词表和模型词表后兜底为 ErrClient → 直接返回客户端，不 failover。一个跑了两小时的 agent 任务就此终止，而候选队列里可能正躺着一个 context 更大的端点。

为什么值得做：这是现有嗅探机制的自然延伸——加一组词表（maximum context length、context_length_exceeded、too many tokens、输入长度超过 等）+ 一个新的 ErrContextLimit 类。行为上与 ErrContent 同构：切换、不惩罚端点健康（端点没病，是这个请求太大）。改动面小到可以复用现成的测试骨架。

配套：endpoint 上加一个可选的 context_limit 声明字段，让 strategy 能把"大 context 端点"排在后面当兜底——但这一步可以推后，先把"不再一头撞死"这件事做掉。

4. 主动限流 + 预算硬闸

用户痛点：现在的健康机制全是事后反应——撞上 429，冷却，切换。对 bootstrap 用户还有第二个痛点：一个进入死循环的 agent 可以在一夜之间烧掉整月额度，vmr 全程忠实地转发，一句话不说。

两件事，价值不同：

per-endpoint rpm/并发令牌桶（路线图已有）：价值中等。收益是少几次无谓的失败尝试和尾延迟抖动，但 vmr 的 failover 已经把 429 的用户可见伤害压得很低了。
per-virtual-model 的 token/成本日预算硬闸：价值明显更高。触顶后的行为应该是明确拒绝并给出可解析的错误，而不是静默降级到便宜模型——静默降级会让 agent 在完全不知情的情况下换脑子，比失败更难排查。

风险提示：预算闸需要跨请求的累计状态，而 vmr 目前"重启即重置、不持久化"。要么接受"重启后预算清零"这个弱语义（简单、诚实、可文档化），要么就得引入持久化——后者是滑向 DB 的第一步，坚决不要。建议选前者，并在文档里直说它是防呆而非防欺诈。
