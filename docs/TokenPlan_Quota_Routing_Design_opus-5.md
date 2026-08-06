<!-- Ver 2026-08-06 22:24, by Opus 5 -->

# 阶段一设计：Token-Plan 额度感知路由（Quota-Aware Routing）

配套文档：
- `docs/TokenPlan_Routing_and_Forensics_Strategy.md` —— 战略与竞品，定义"为什么做"。
- `docs/TokenPlan_Routing_and_Forensics_Design_gemini-3.6-flash.md` —— 已逐条审核并就地批注，转为审核留痕，不作为实施依据。

本文只解决"怎么做"，范围严格限定在 Router 侧。

---

## 1. 问题定义

### 1.1 要优化的目标函数

团队手上有 N 个按周期打包的套餐（每个绑定一个厂商账号），外加若干按量计费端点。
套餐额度**周期性作废、不结转**。要最小化的是：

```
浪费 = Σ(周期结束时未用完的套餐额度) + Σ(套餐用尽后溢出到按量计费的花费)
```

这两项是同一枚硬币的两面：把流量喂给 A 就是不喂给 B。所以这不是"负载均衡"问题（那是要均分压力），
而是**"在每个套餐各自作废之前恰好把它烧完"的配速问题**。

这个区分直接否掉了直觉公式 `remaining / total`：

> 三个月度套餐，重置日分别是 1 / 15 / 20 号（套餐是一个一个买的，重置日不对齐是常态）。今天 16 号。
> A 过完 50% 周期，B 刚重置（过完 3%），C 过完 87%、**4 天后余量作废**。
> 按剩余比例加权，B 拿走绝大部分新会话，**而最该被优先烧掉的 C 权重最低**。

剩余比例这个"存量"信号，在重置日不对齐时会给出系统性反向的结论。

### 1.2 本轮补充的三个维度

在上述基础上，真实套餐还有三个维度必须建模，且都不是假想：

1. **计量单位有三种**：次数（requests）、token、金额/Credits（cost）。
2. **限额窗口有多个且并存**：同一账号可能同时受 5 小时、周、月三层约束。
3. **折算不是均一的**：同一套餐内不同模型的扣减倍率不同（甚至有独立子额度）；
   token 的四个分量（fresh input / cache read / cache write / output）扣减比例也不同。

下一节用市场事实确认这三条，然后据此建模。

---

## 2. 市场调研：套餐的真实形态

**完整事实依据见 `docs/TokenPlan_Market_Reference.md`**（81 个在售套餐的结构化快照，
原始数据留档在 `docs/data/tokenplan-market-plans-2026-07-26.json`）。本节只摘取直接驱动设计的结论。

### 2.1 计量单位分布（在售 81 个套餐）

| 分类 | 数量 | 占比 |
|---|---|---|
| 有 Token 维度限额 | 62 | **77%** |
| 有数值型请求数限额 | 32 | **40%** |
| 仅 Token 限额 | 44 | 54% |
| 仅请求数限额 | 14 | 17% |
| **两者并存** | 18 | **22%** |

| 类型 | 数量 | 有请求限额 | 有 Token 限额 |
|---|---|---|---|
| **Coding Plan** | 31（38%） | 23（**74%**） | 15（48%） |
| **Token Plan** | 50（**62%**） | 9（18%） | 47（**94%**） |

### 2.2 五条结论，每条都直接改变设计

1. **Token 是整体主流口径，不是 requests。** "国内套餐以次数计量为主"只在 Coding Plan 这个
   38% 的子集内成立；数量更多的 Token Plan 有 94% 按 Token/Credits 计量。而且市场正从
   Coding Plan 向 Token Plan 收缩，这个方向只会让 Token 口径的占比继续上升。
2. **22% 的套餐同时受两种 metric 约束**，所以"选一种 metric"这个问法本身是错的——
   同一账号必须能同时挂多种 metric 的 Limit。
3. **Credits 制套餐按分量折算，比例差 5～120 倍**，这是本轮最重要的发现：

   | 厂商 | 折算口径 | fresh : cache read : output |
   |---|---|---|
   | 小米·MiMo | 缓存 2.5 / 输入 300 / 输出 600 Credits per Token | cache 便宜 **120 倍** |
   | 阿里·百炼 | 输入 5K / 命中 25K / 输出 0.83K Token per Credit | cache 便宜 5 倍，output 贵 **6 倍** |
   | DeepSeek Pro | 缓存 ¥0.025/M、未命中 ¥3/M、输出 ¥6/M | cache 便宜 **120 倍** |
   | 字节·方舟 | 积分制 AFP，1 AFP = 1111 Token，最优模型 **9 倍系数** | 模型系数 9x |
   | OpenCode | 美元 Credits：基础 $12 / 周 $30 / 月 $60 | 三窗口 + 金额计量 |

   按"输入输出 99:1、缓存命中率 90%"（该市场对比站统一采用的口径）量化：
   **把额度当等权总 Token 记账，相对真实折算会高估 3～8 倍**
   （小米·MiMo 7.9 倍、阿里·百炼 3.0 倍）。对高缓存命中率的 Agent 工作流，
   等权记账会让一个刚用掉 15% 的账号显示成"已耗尽"。

4. **多窗口并存是常态。** 字节·方舟明确"有 5 小时、周、月的 Token 限制"；
   阿里云 Coding Plan Pro 是 5h 6,000 / 周 45,000 / 月 90,000 三层。
   反过来也有只有单层的（小米·MiMo 无 5h 限额；阶跃星辰只有周限量无月限量）。
5. **"上游无配额 API"这个前提要修正为"无*标准* API，但有厂商*私有* API"**：
   MiniMax `GET /v1/api/openplatform/coding_plan/remains`（5h + 周 + 模型级明细）、
   智谱 `/api/monitor/usage/quota/limit`（返回 `type`/`percentage`/`nextResetTime`，有反爬）、
   Claude Code statusLine 的 `{five_hour,seven_day}.used_percentage`。
   这意味着本地累计不必是唯一数据源——设计上要为外部校准源留出扩展点。

### 2.3 一个容易踩的坑：厂商计数单位 ≠ vmr 可观测单位

阶跃星辰官方按 **prompt** 计数，市场对比站给出的换算是 **1 prompt ≈ 15 次请求**；
智谱老套餐同样按 prompt 计（每 prompt 约 15–20 次模型调用）。vmr 在网络层看到的是那 15–20 次。

所以 `amount` 必须按 **vmr 可观测的口径**配置，不能直接抄套餐标称值。
这是**单位换算问题，不是精度问题**，必须在用户文档里写死，
并且是"官方用量 API 校准"最有价值的场景。

**调研来源**：
[市场对比站原始数据](https://vibecoding.dreamfree.space/)（已留档，见 `docs/TokenPlan_Market_Reference.md`）、
[阿里云 Coding Plan 官方文档](https://help.aliyun.com/zh/model-studio/coding-plan)、
[MiniMax Token Plan 文档](https://platform.minimaxi.com/docs/token-plan/intro)、
[智谱 GLM Coding Plan 套餐概览](https://codingplan.org/plans/zhipu)、
[coding-plan-monitor（用量 API 逆向）](https://github.com/JinHanAI/coding-plan-monitor)、
[GitHub Copilot 转向用量计费](https://github.blog/news-insights/company-news/github-copilot-is-moving-to-usage-based-billing/)、
[Claude Code 用量限制](https://www.truefoundry.com/blog/claude-code-limits-explained)。

---

## 3. 全集：额度约束的完整模型

把上表抽象干净，模型分**两层**——这个分层本身是设计结论，不是排版：

```
账号级（挂在 Provider 上）—— 描述"一次调用怎么折算成额度"，与窗口无关
  TokenWeights   四分量权重 {in_fresh, cache_read, cache_write, out}，缺省全 1.0
  ModelMultipliers  按模型的扣减系数，支持 "*" 通配缺省，缺省 1.0

窗口级（一条 Limit）—— 描述"在多长的周期里累计多少"
  Metric   requests | tokens | cost
  Window   { 长度 N×{h,d,w,mo}, 锚点 since, 类型 tumbling | rolling }
  Amount   该窗口内的上限
  Scope    作用模型集合（缺省 = 该账号全部模型）
```

**为什么折算规则是账号级而不是窗口级**：一次调用消耗多少额度，是"这个账号怎么记账"的属性；
在多长的窗口里累计，是"厂商怎么切周期"的属性。**两者正交。**
观察到的每一个套餐都印证这点：字节·方舟的 9 倍模型系数对它的 5h / 周 / 月三个窗口是同一个值；
智谱 GLM-5.2 的 3 倍对 5h 和 7d 也是同一个值。把折算写在 Limit 上，就是把同一个事实抄 N 遍——
既是配置噪音，也是"三处改了两处"的经典 bug 来源。

于是扣减量是一个统一公式：

```
charge = base(metric) × ModelMultipliers[model]

  base(requests) = 1
  base(tokens)   = Σ_c  tokens_c × TokenWeights[c]
  base(cost)     = Σ_c  tokens_c × Rate[provider, model, c, ts]     ← pricing.yaml
```

**一个账号可以挂多条 Limit，语义是 AND：任一条触顶，该账号即受限。**

验证这个模型能装下全集：

| 套餐 | 表达 |
|---|---|
| 阿里云 Coding Plan Pro | 3 条 Limit：(requests, 5h) / (requests, 1w) / (requests, 1mo) |
| 智谱 GLM Coding Plan（老，prompt 制） | 2 条：(requests, 5h, rolling) / (requests, 7d) + 账号级 `{glm-5.2: 3}` |
| 智谱 GLM Coding Plan（新，Token 制） | 1 条：(tokens, 1w) |
| 字节·方舟（AFP 积分制） | 3 条：(tokens, 5h/1w/1mo) + 账号级 `{最优模型: 9}` + `TokenWeights` |
| 阿里云 Token Plan 团队版 / 小米·MiMo | 1 条：(cost, 1mo)，分量费率来自 `pricing.yaml` |
| MiniMax Token Plan | 2 条：(cost, 5h) / (cost, 1w) |
| OpenCode | 3 条：(cost, 5h/1w/1mo) |
| 纯按量端点 | 0 条 |
| 模型独立子额度 | 多条 Limit，各自 `Scope` 指向不同模型集合 |

> Copilot / Cursor 这类纯 $ 制产品在模型上可表达为 `(cost, 1mo)`，但它们**没有可重定向
> `base_url` 的 API 出口，接不进 vmr**——列在这里只为验证模型的表达力，不是目标场景。

模型成立。下一节做简化。

---

## 4. 简化：砍什么、留什么

原则：**在不损害"动态平衡"目标的前提下，砍掉配置与实现复杂度**。每条决策都必须有事实依据。

### 4.1 保留（有事实依据，砍了功能就不成立）

| 保留项 | 依据 | 实现代价 |
|---|---|---|
| **多条并存 Limit** | 22% 套餐两种 metric 并存；三层窗口是常态 | 极低——多窗口天然可用 `min()` 归并（见 §5） |
| **metric: requests** | Coding Plan 74% 按次数计 | **最低**——计数器 +1，完全不需要解析响应 |
| **metric: tokens** | 纯 token 桶（智谱新版"Token/周"、字节 AFP→Token、Kimi） | 中——需要 usage 嗅探 + 降级估算 |
| **metric: cost（分量折算）** | **Token Plan 94% 按 Token/Credits 计，且 Credits 按分量折算，比例差 5～120 倍** | 中——复用 `pricing.yaml` 的按模型分项费率 |
| **模型倍率（账号级）** | 字节 9 倍系数、限时 2.5x/4x；智谱老套餐 3 倍 | 极低——一个 `map[string]float64` |
| **token 分量权重（账号级）** | 字节 AFP、智谱新版是 token 桶，但分量扣减比例不均一 | 极低——四个 float，缺省全 1.0 |
| **Scope（按模型限定）** | MiniMax 模型级明细；用户明确提出 | 极低——charge 时一次模型名匹配 |
| **rolling 窗口** | 智谱/Claude 是滚动窗口 | 低——分桶近似，约 40 行（见 §8） |

### 4.2 砍掉（有依据的简化）

**① 不引入独立的额度价格表 → `metric: cost` 直接复用已有的 `pricing.yaml`**

Credits 制套餐的折算率（阿里"输入 5K / 命中 25K / 输出 0.83K Token per Credit"）本质就是
**按模型的分项单价**——它与 `pricing.yaml` 的形状（`provider+model` → `in_fresh` / `cache_read` /
`cache_write` / `out` 四项费率）完全同构，仓库里已经有这张表。
所以不新建价格体系，`limits.amount` 以 `pricing.yaml` 的基准币种计价即可。

> **一处必须注意的差异**：`internal/report` 现有的成本公式**刻意排除 cache read**
> （注释原文："every provider below treats a cache hit as free/near-free"）。
> 这个假设对额度记账**不成立**——阿里的 cache 是 ¥0.32/M 对 input ¥1.58/M，
> 在 90% 命中率下 cache 部分占输入成本的 **65%**，排除它会严重低估。
> 因此 `internal/pricing` 应当只提供**原始费率**，成本公式由各调用方自己套：
> report 保持它现有的口径，quota 用 `fresh + cache_read + cache_write + out`。

**② 折扣与促销：不新增机制，由"账号级 `model_multipliers` + `pricing.yaml` 的时间窗费率"两层承担**

`pricing.yaml` 是**标准价目表**，但套餐的实际扣减往往打折，且折扣形态不止一种。逐一对应：

| 折扣形态 | 真实例子 | 承载方式 |
|---|---|---|
| 整个套餐按列表价的固定折扣 | Credits 制套餐普遍低于列表价 | `model_multipliers: {"*": 0.6}` —— 通配缺省项 |
| 单个模型的促销倍率 | 字节"GLM-5.2 限时 **4 倍**用量"（= 按 0.25 折扣扣减） | `model_multipliers: {glm-5.2: 0.25}` |
| 有起止日期的限时活动 | 字节"6.8–8.8 期间 2.5 折" | **`pricing.yaml` 已支持** `date_from`/`date_to`，first-match-wins |
| 分时段价差 | 智谱高峰 14:00–18:00 加倍 | **`pricing.yaml` 已支持** `hour_from`/`hour_to`（支持跨零点） |

后两行是核对代码后的确认，不是设想：`PricingRate` 已经有
`DateFrom`/`DateTo`/`HourFrom`/`HourTo` 四个可选字段，`RateFor` 按 `pricing.yaml` 的书写顺序
first-match-wins，其文档注释举的例子正是"一条 off-peak 折扣 + 一条全价兜底"。
**所以 `cost` 档的限时与分时折扣是零成本获得的**，只需把促销费率写在兜底费率之前。

`model_multipliers` 与 `pricing.yaml` **不重复计算**：前者是无量纲的相对系数，后者是绝对费率。
对 `cost` 档，两者是"列表价 × 本账号折扣"的关系。

**③ 折算规则放在账号级，不放在窗口级**

见 §3。观察到的每个套餐里，模型系数与分量权重在该账号的所有窗口上都是同一个值，
写在 Limit 上就是把同一事实抄 N 遍。**当前配置样例里 `model_multipliers` 在两条 Limit 上重复出现，
本身就是这个问题的现场证据。**

若将来出现"同账号不同 metric 需要不同折算"的情形（未观察到），
加一个 Limit 级 override 是纯增量的可选字段，不需要重构。**按 YAGNI 现在不做。**

**④ 不做 per-provider 的四个**金额**全局权重**

这是 gemini 方案的做法，拒绝理由不变、且被新数据进一步坐实：阿里的折算率原文明确标注了
适用模型（"Qwen-3.6-Plus，输入 5K Token/Credit……"），小米·MiMo 更直接写明
"表格按 MiMo-V2.5-Pro 口径算，MiMo-V2.5 实际更高"——**金额折算率是按模型给的**。
一组 per-provider 的金额权重对多模型账号必然系统性偏差，而 `pricing.yaml` 天然按模型。

> 注意这与 ⑤ 的 `token_weights` 不冲突：`token_weights` 服务的是 **token 桶**型套餐
> （其账号内各模型共用一套分量比例），`pricing.yaml` 服务的是**金额/Credits** 型套餐
> （其费率按模型分化）。两者对应两类不同的套餐，不是同一件事的两种写法。

**⑤ `metric: tokens` 支持分量权重，缺省 1:1:1:1**

token 桶型套餐（字节 AFP、智谱新版）的账面单位是 token，但四个分量的扣减比例未必均一。
让它们各带一个权重、缺省全 1.0，好处有三：

- **简单场景零配置**：真按原始 token 数计的套餐什么都不用写，行为与"总量"完全一致；
- **不必为一个非金额套餐去维护 `pricing.yaml`**：字节的额度以 AFP/Token 计，
  强迫用户把它反推成 ¥ 单价再填进价目表，既绕又容易错；
- **与 `cost` 结构同构**：两者都是"四分量加权求和"，只是权重来源不同
  （内联比例 vs. 按模型的价目表），实现上是**同一个函数、两个权重来源**，不是两条代码路径。

选择哪一档的判据很清楚：**账号内各模型共用一套分量比例 → `tokens` + `token_weights`；
分量费率按模型分化 → `cost` + `pricing.yaml`。**

**⑥ 时段倍率：`cost` 档天然支持，`requests`/`tokens` 档不做**

智谱高峰 14:00–18:00 消耗 2–3 倍这类规则，对 `cost` 档由 `pricing.yaml` 的 `hour_from`/`hour_to`
直接表达（见 ②）。对 `requests`/`tokens` 档**不做**：需要新的配置面，而目前只有智谱一家、
规则本身还在变（官方写明"9 月底前限时福利"）。代价是对这类账号系统性低估，
缓解手段是调低 `amount`、改用 `cost` 档，或接官方用量 API 校准。

**⑦ `reset_period` + `reset_day` 的字段组合 → 统一为 `(every, since)` 二元组**

`every: 1mo, since: 2026-08-14` 即"每月 14 号重置"。同一个机制还表达周锚点、5h 锚点、
"每 2 周"、"每 3 天"，免掉了将来再加 `reset_weekday`/`reset_hour` 的字段膨胀。
用户要求的"数月/数周/数日/数小时 + 周期开始时间，后续自动推算"由此一次性满足。

**⑧ `initial_consumed_tokens` 初始偏置 → 砍**

它只在接入 vmr 的第一个周期有意义，第二个周期起就该是 0，但配置文件里的值不会自己消失，
会长期造成系统性偏差。中途接入的对齐用手工编辑状态文件解决。

**⑨ SWRR 平滑加权轮询 → 砍，改用贪心（见 §5.3）**

---

## 5. 核心算法：Headroom（余量比）

### 5.1 一个无量纲的比值

对每条 Limit：

```
used_frac(L)      = min(1, used(L) / L.amount)              // 已用比例
time_left_frac(L) = time_left(L) / L.duration               // 剩余时间比例，rolling 恒为 1
raw(L)            = (1 - used_frac) / max(time_left_frac, ε)
```

`ε` 只用于防止除零（窗口正好走到边界的一瞬），不承担调参职责——
`raw` 的上界由 §5.2 的 `HeadroomCap` 统一约束，所以 `ε` 取一个足够小的常量即可。

`raw` 的含义是"**剩余额度比例 ÷ 剩余时间比例**"：
- `= 1`：严格按进度消耗；
- `> 1`：欠用，应该多接流量；
- `< 1`：超前，应该少接流量；
- `= 0`：已耗尽。

三个关键性质：
- **无量纲**，因此次数制账号与 token 制账号可以直接比较；
- **自稳定**：线性消耗时分子分母同比缩小，`raw` 恒为 1。偏离自动纠正——这是一个不需要整定参数的比例控制器；
- **解决重置日错位**：§1.1 的例子中 C 的 `raw = 0.4/0.133 ≈ 3.0` 最高，正确胜出。

### 5.2 桶 vs 闸：两类 Limit 的角色不同

这是本设计最容易做错的一处。并非所有 Limit 都该驱动"快去烧额度"：

* **桶（Bucket）**——对应**计费周期**的那条 Limit。未用完的额度真的对应到花掉的钱，
  **use-it-or-lose-it 成立**，`raw > 1` 时应当**主动提升**权重。
* **闸（Gate）**——更短的窗口（5h、周）。它们是厂商用来平滑负载的**速率上限**，
  用满它们没有任何经济价值。**闸只应该在接近饱和时压制流量，绝不应该提升流量。**

判定规则，零配置：**周期最长的那条 tumbling Limit 是桶，其余全是闸。**
套餐费按最长周期收取，只有这个周期的未用额度才对应到真实浪费。该规则在每一个观察到的套餐上都成立
（阿里云 Pro：月=桶，周/5h=闸；智谱：7d=桶，5h=闸；只配一条 tumbling 时它自己就是桶）。

**rolling 窗口永远不能当桶**——这是必须显式写死的一条边界，否则会静默算错：
滚动窗口的额度是持续再生的，不存在"到期作废"，因此 use-it-or-lose-it 对它不成立。
而且它的 `time_left_frac ≡ 1`，`raw` 恒 ≤ 1，当桶时永远给不出 `> 1` 的提升信号，
表现会退化成一个没有 `GateReserve` 缩放的畸形闸。
所以：**若一个账号的所有 Limit 都是 rolling，则它没有桶，全部按闸处理**——
语义上正确（没有会作废的额度，就没有"该主动烧掉"的理由）。

```
headroom(L) = raw(L)                            L 是桶
            = min(1, raw(L) / GateReserve)      L 是闸    (GateReserve = 0.5)

score(provider) = min( over all its Limits ) headroom(L)      // 最紧的约束说了算
                  再 clamp 到 [0, HeadroomCap]                 // HeadroomCap = 5
```

闸的形式含义：**只要该窗口的消耗速率不超过其比例进度的 2 倍，闸保持中性（1.0）；
超过后线性收紧到 0。** 于是闸永远只能把分数往下拉，不会把一个账号顶上去。

验证：阿里云 Pro，5h 窗口已用 5500/6000（剩 8.3%）且只剩 1h（时间剩 20%）：

```
raw(5h)  = 0.083 / 0.2         = 0.417
闸(5h)   = min(1, 0.417 / 0.5) = 0.833
桶(月)   = 1.0                            （按进度消耗）
score    = min(0.833, 1.0)     = 0.833
```

轻度压制，5h 窗口翻转后自动恢复；月度桶的进度不受影响。符合直觉。

**退化行为**：只配一条月度 Limit 时，整套机制退化成 `(1-已用比例)/(剩余时间比例)` 一个除法。
简单场景保持简单。

### 5.3 分配策略：贪心取最高 score，无随机、无累加器

因为 `score` 在每次计费后都会更新，**直接取当前最高者**就已经收敛：
A 拿走流量 → A 的消耗上升 → A 的 score 下降 → 跌破 B 后自动转向 B。

于是分配退化成**同梯队内按 score 降序稳定排序**。相比 SWRR 好在四处：

1. **无状态**：不需要 per-provider 平滑累加器，也就不需要考虑它的持久化、溢出、归一化；
2. **确定性**：不引入 `math/rand`，同输入必然同顺序，测试可断言、线上可复现；
3. **failover 顺序自动正确**：排序作用于整个梯队，第二、第三顺位天然也按 score 排列——
   SWRR 只回答"选谁"，剩下的顺序还得另想办法；
4. **对 Prefix-Cache 友好**：贪心会把新会话在一段时间内集中到同一账号，
   而轮询式撒开恰恰在制造缓存分裂，与项目主线目标反向。

代价是分配粒度较粗（两账号 score 交叉前流量持续压在一个上）。真实场景下不是问题：
Agent 会话本身稀疏且长尾，账号侧并发上限触发会返回 429 → 走既有 health 冷却与 failover。

---

## 6. 调度流程

### 6.1 在 `Serve` 中的位置

```
health 过滤
  → 硬 Condition 过滤
  → 上下文长度过滤（带兜底）
  → strategy.Sort(candidates, dims)          [不变]
  → quota 重排  (新增)                        ← 本设计
  → Sticky moveToFront                        [不变，优先级最高]
  → failover 循环
```

三条不变量：

1. **quota 重排在 `Sort` 之后**：绝不跨越 Dimension 链已确立的次序
   （`priority` 把按量端点压在低优先级作兜底，这个语义必须原样保留）。
2. **Sticky 在 quota 之后**：会话黏性优先级最高。已建立的会话即使命中的账号额度已耗尽，
   只要端点健康就继续走它——打断 prefix cache 的重算成本通常高于短暂溢出的计费成本。
3. **quota 只重排、从不淘汰**：候选集大小不变，failover 语义完全不受影响。

> **推理链纠正**：额度归零**不会**自动触发 failover。读 `router.go` 的失败循环可知，
> failover 只在上游真的返回错误或网络失败时才走向下一候选；权重归零不产生失败。
> 所以"额度耗尽自然滑落到低优先级兜底端点"是**错的**——归零只意味着排到本梯队末位。

### 6.2 重排算法

```
func reorderByQuota(candidates []*core.Endpoint, dims []strategy.Dimension,
                    reg *quota.Registry, now time.Time) (changed bool)

  // 1. 切分并列梯队：沿 candidates 走，相邻两端点若对 dims 链中每个
  //    Dimension.Compare 都返回 0，则同属一个梯队。
  //    （不预设 priority 在链里；Sort 稳定，梯队内保持配置文件顺序；dims 为空则全体同梯队。）
  //
  // 2. 每个梯队内：取出"挂了 Limit"的成员及其下标，按 score 降序稳定排序后写回**原来那些下标**。
  //    未挂 Limit 的成员位置纹丝不动。
```

第 2 步的**占位重排**是刻意的：若把未挂额度的端点也塞进排序，只要用户只给三个账号里的一个配了额度，
另外两个就会被意外降级或提升。占位重排让"给某个 Provider 配额度"的影响**严格局限于该 Provider 自己**。

复杂度：候选数通常 2–6，`O(n²)` 的两两 `Compare` 可忽略。

### 6.3 触发时机：只对新会话

`Serve` 里已经算出 sticky 是否命中，因此"是不是新会话"这个信号**零额外成本**。
实现上不必写显式分支：quota 重排无条件先跑，sticky 命中时的 `moveToFront` 天然覆盖它。

走到"按额度重排"的三种情况均正确：首轮对话（指纹未见过）；sticky TTL 过期（缓存本已凉，重挑一个）；
模型 `sticky: false` 或指纹提取失败（无黏性模型本就该每请求重排）。

---

## 7. 计量

### 7.1 三种 metric 的计量方式

统一公式（§3 已给出）：`charge = base(metric) × ModelMultipliers[model]`。

| metric | `base` | 适用套餐 | 解析成本 |
|---|---|---|---|
| `requests` | `1` | Coding Plan（74% 按次数计） | **零**。不碰响应体 |
| `tokens` | `Σ_c tokens_c × TokenWeights[c]`，权重账号级、缺省全 1.0 | token 桶（智谱新版、字节 AFP、Kimi） | 中，见 §7.2 |
| `cost` | `Σ_c tokens_c × Rate[provider, model, c, ts]`，费率来自 `pricing.yaml` | Credits / 金额制（阿里·百炼、小米·MiMo、DeepSeek 虚拟套餐、OpenCode） | 同 `tokens`，多一次查表 |

`c` 遍历四个分量 `{in_fresh, cache_read, cache_write, out}`，其中
`in_fresh = usage.In − usage.CacheRead − usage.CacheWrite`
（`chatmsg.Usage` 的文档已明确 CacheRead/CacheWrite 都含在 In 里）。

`tokens` 与 `cost` 是**同一个加权求和函数的两个权重来源**，实现上不是两条代码路径——
差别只在权重是账号级内联比例、还是按模型查价目表。
`TokenWeights` 全为 1.0 时 `base(tokens)` 恰好退化成 `In + Out`，简单场景零配置。

三档都必须在阶段一内交付，因为它们各自覆盖的市场份额都不小（见 §2.1）。落地顺序按解析成本递增：
`requests` 完全不碰响应体，是风险最低的第一档；`tokens` 和 `cost` 共用同一套 usage 嗅探。

**`cost` 不是可选项**：Token Plan 占在售套餐 62%，其中 94% 按 Token/Credits 计量，
而 Credits 的扣减是按分量折算的（cache read 比 fresh input 便宜 5～120 倍）。
用 `tokens` 的等权总量去记 Credits 制套餐，会**高估 3～8 倍**——
一个刚用掉 15% 的账号会显示成已耗尽，路由和看板同时失真。

### 7.2 token 计量：嗅探 + 降级

```
1) 上游 usage（权威）   ← respStream 嗅探，复用 chatmsg 的解析口径
2) 本地估算（降级）     ← core.EstimateTextTokens(请求体 / 响应体)
```

不能只用估算：误差约 ±30%，对"三个账号里选哪个"无所谓，但计数器同时要回答"这个月烧到 80% 了没有"，
并驱动阶段二的预测看板——那里 ±30% 不可接受。所以**能拿真值就用真值，拿不到才降级，
并在状态里标出本周期的估算占比**，让使用者知道数字有多可信。

拿不到 usage 的三种情况：上游响应带 `Content-Encoding`（`respStream` 按设计对压缩响应完全不解析）、
上游不返回 usage 字段、流被中途截断。

实现约束：
* 挂在 `respStream` **已有的事件切分**上，不新增一遍全文遍历；
* 每个事件先做 `bytes.Contains(ev, []byte("\"usage\""))` 廉价门禁，命中才 JSON 解析——
  绝大多数 token delta 事件直接跳过，开销约等于零；
* 解析与合并**不在 router 里重写**：在 `chatmsg` 暴露逐块入口（形如 `UsageFromChunk([]byte) (Usage, bool)`，
  内部即现有 `mergeUsage`）。这守住 CLAUDE.md 的"`chatmsg` 是消息解析唯一真相源"不变量——
  否则 Anthropic 的 `message_start`/`message_delta` 两段式累计会出现第二份实现；
* 合并沿用**逐字段取 max**，天然适配流式累计与单个终态对象两种形态；
* 只在 `forwardSuccess` 成功路径计费（429 基本无消耗；中途截断的输入消耗算作可接受的低估）。

**降级时的分量拆分**：`core.EstimateTextTokens` 只能给出总量，拆不出 cache 命中比例。
所以降级路径按**无缓存**折算——请求体估算全部计入 `in_fresh`，响应体估算全部计入 `out`。
这个方向是**保守的（高估消耗）**：对闸而言偏安全，对桶而言会略微少用套餐。
每一笔降级计量都累加进 `account.estimated`，`/admin/status` 因此能给出本周期的估算占比，
让运维者知道这个账号的数字有多可信——**这正是 `cost` 档最需要盯的指标**，
因为它的降级偏差比 `tokens` 档更大（缓存分量的费率差可达 5～120 倍）。

### 7.3 计量源抽象：为官方用量 API 留口

§2 中"厂商有私有用量 API"那条结论意味着本地累计不该被写死成唯一来源：

```go
type Source interface {
    Used(providerName string, l Limit, now time.Time) (amount float64, ok bool)
}
```

阶段一只实现 `LocalSource`（本地累计）。将来的 `ExternalSource`（MiniMax `coding_plan/remains` 一类）
按 Provider 配置注册、周期性拉取、覆盖本地值。这个抽象是**零成本的**（一个接口 + 一个默认实现），
却让"接官方 API"不需要重构，并能一举消解本地计数最大的限制（绕过 vmr 的流量、单位换算偏差、时段倍率）。

### 7.4 不做预扣（reservation）

请求前按估算预扣、完成后对账，可以避免并发新会话在任何一个上报 usage 前全部选中同一账号。
不做：超冲上界是 `并发新会话数 × 单请求消耗`，约 10 × 50K = 500K，对 100M 月度套餐是 0.5% 的噪音；
而预扣要引入"预扣—对账—回滚"三段状态机及失败路径的泄漏风险。
**用 0.5% 的精度换掉一个有状态子系统，是划算的。**

---

## 8. 窗口实现：一个环形分桶覆盖两种类型

`tumbling`（固定重置）与 `rolling`（滚动）用**同一个环形桶结构**实现，只有边界策略不同：

```
Ring{ width: 桶宽, slots: []struct{ startUnix int64; c Counters } }

  tumbling:  slots = 1，桶起点 = periodStart(since, every, now)
             读写前比较桶起点，不等即就地清零           ← 惰性重置
  rolling:   slots = K（窗口 ≤ 1 天取 12，> 1 天按天分桶），桶起点 = floor(now / width)
             读取时求和"落在 [now-window, now] 内"的桶，过期桶清零复用
```

两个直接收益：

* **惰性重置**：没有 ticker goroutine、没有定时任务、没有"重置时刻进程恰好没跑"的漏重置、
  没有时钟回拨导致的重复重置。重启后从文件加载，同一条比较自动完成补偿。重置退化成一次比较。
* **滚动窗口误差有界**：≤ `1/K`（12 桶即 ≤ 8.3%），内存 = K 个计数结构。
  若用 tumbling 硬近似 rolling，窗口刚翻转时误差可达 100%，会把流量送到实际仍在限流的账号，
  白白吃一次 429 + failover 延迟。40 行代码消除这个问题，值得。

**周期推进与月末截断**：`every: 1mo` 从 `since` 起按日历月推进，`since` 落在 31 日时对短月**截断到月末**
（1/31 → 2/28、2/29），而不是报错——按 31 号买的套餐真实存在。

**时区**：周期边界按 `fmtutil.DisplayZone`（= `time.Local`）计算。厂商真实账单时区不公开，
本地时区最贴近运维者心智模型，且符合 CLAUDE.md"人可见的一切都走 DisplayZone"的既定权威。
这是**近似而非精确复刻**，需在用户文档写明。

---

## 9. 配置与运行态

### 9.1 配置形态

```yaml
providers:
  # ① 三层并存的次数制 Coding Plan（阿里云 Pro 形态）
  - name: aliyun-coding-plan
    base_url: {anthropic: https://dashscope.aliyuncs.com/apps/claude-code-proxy}
    api_key: ${ALI_KEY}
    quota:
      limits:
        - {metric: requests, every: 5h, amount: 6000}
        - {metric: requests, every: 1w, amount: 45000}
        - {metric: requests, every: 1mo, amount: 90000}   # 周期最长的 tumbling → 桶

  # ② 滚动窗口 + 模型倍率（智谱老套餐，prompt 制）
  # 注意 amount 按 vmr 可观测的"上游 API 调用次数"配置，不是厂商的 prompt 数。
  #      智谱 1 prompt ≈ 15~20 次调用，Pro 的 400 prompts/5h ≈ 6000~8000 次。
  - name: zhipu-coding-plan
    base_url: {anthropic: https://open.bigmodel.cn/api/anthropic}
    api_key: ${ZHIPU_KEY}
    quota:
      model_multipliers: {glm-5.2: 3, glm-5: 3}    # 账号级：两条 Limit 共用，不重复书写
      limits:
        - {metric: requests, every: 5h, rolling: true, amount: 7000}
        - {metric: requests, every: 7d, amount: 35000}

  # ③ token 桶 + 分量权重（字节·方舟 AFP 形态）
  # 账号内各模型共用一套分量比例 → 用 tokens + token_weights，不必去维护 pricing.yaml
  - name: ark-token-plan
    quota:
      token_weights: {in_fresh: 1.0, cache_read: 0.1, cache_write: 1.25, out: 4.0}
      model_multipliers: {"*": 1.0, doubao-seed-2.0: 9}
      limits:
        - {metric: tokens, every: 5h,  amount: 83000000}
        - {metric: tokens, every: 1w,  amount: 624000000}
        - {metric: tokens, every: 1mo, amount: 1249000000}

  # ④ 纯 token 桶，什么都不配 = 分量 1:1:1:1（智谱新版"中值约 0.65 亿 Token/周"）
  - name: zhipu-new-lite
    quota:
      limits:
        - {metric: tokens, every: 1w, since: 2026-08-14, amount: 65000000}

  # ⑤ Credits / 金额制（阿里·百炼、小米·MiMo 形态）
  # 分量费率按模型分化 → 走 cost，费率来自 pricing.yaml；amount 以其基准币种计价。
  # "*": 0.6 表示本套餐按列表价的 6 折扣减额度（折扣层，与费率表不重复计算）。
  - name: bailian-token-plan
    quota:
      model_multipliers: {"*": 0.6}
      limits:
        - {metric: cost, every: 1mo, amount: 198}

  # ⑥ 三窗口 + 金额制（OpenCode 形态：基础 $12 / 周 $30 / 月 $60）
  - name: opencode-go
    quota:
      limits:
        - {metric: cost, every: 5h,  amount: 12}
        - {metric: cost, every: 1w,  amount: 30}
        - {metric: cost, every: 1mo, amount: 60}

  # ⑦ 模型级独立子额度（Scope）
  - name: plan-with-submodel-cap
    quota:
      limits:
        - {metric: requests, every: 1mo, amount: 50000}                       # 账号总限
        - {metric: requests, every: 1d, amount: 200, models: [premium-model]} # 仅约束该模型
```

**账号级字段**（`providers[].quota`，描述"一次调用怎么折算成额度"）：

| 字段 | 说明 | 缺省 |
|---|---|---|
| `model_multipliers` | 按上游模型名的扣减系数，支持 `"*"` 通配缺省。`>1` = 该模型消耗更快（字节 9 倍系数）；`<1` = 折扣（套餐按列表价打折、限时促销） | 全部 1.0 |
| `token_weights` | `metric: tokens` 的四分量权重 `{in_fresh, cache_read, cache_write, out}` | 全部 1.0 |
| `limits` | 该账号的 Limit 列表 | 必填（`quota` 存在时） |

**窗口级字段**（`limits[]`，描述"在多长的周期里累计多少"）：

| 字段 | 说明 | 缺省 |
|---|---|---|
| `metric` | `requests` \| `tokens` \| `cost` | 必填 |
| `every` | `N{h,d,w,mo}`，覆盖"数小时/数日/数周/数月" | 必填 |
| `since` | 周期锚点时间，后续周期自动推算 | `1mo`→每月 1 日、`1w`→周一、其余→当日 0 点 |
| `rolling` | 滚动窗口（分桶近似）；否则固定对齐窗口。**rolling 永不当桶**（§5.2） | `false` |
| `amount` | 该窗口上限（vmr 可观测口径；`cost` 档为 `pricing.yaml` 的基准币种） | 必填，>0 |
| `models` | 该 Limit 只约束这些上游模型（Scope） | 全部 |

**命名**：账号级块叫 `quota`，不叫 `budget`。`vmr report` 已有一套以金额为中心的成本估算
（`pricing.yaml`），配置里再出现一个 `budget` 却可能指次数或 token，是可预见的混淆源。

**校验**（`config.validate`，沿用现有 fail-fast 风格）：

* `metric` 枚举；`every` 语法与 `N > 0`；`amount > 0`；`since` 可解析；
* `model_multipliers` / `token_weights` 的值 `> 0`；
* `token_weights` 出现在**没有任何 `metric: tokens` 的 Limit** 的账号上 → 报错（配了不生效的字段，
  按 `KnownFields` 的同一精神必须显式失败，不能静默无效）；
* `metric: cost` 但 `pricing.yaml` 缺少对应 `provider+model` 费率 → **加载期错误**，
  绝不静默按 0 计费：静默按 0 会让该账号永远显示"额度充裕"，是最坏的失效模式。

### 9.2 运行态

```go
package quota   // internal/quota，仅依赖 core（周期数学是纯函数，无 I/O）

// Counters 存的是原始事实（各分量的 token 数、请求数），不是折算后的结果——
// 折算公式（TokenWeights / pricing.yaml 费率 / model_multipliers）全部在读取侧套用，
// 于是改配置、改价目表都不会让已累计的历史作废。
type Counters struct{ Fresh, CacheRead, CacheWrite, Out, Requests int64 }

type Registry struct {           // 形状对齐 health.Registry：挂在 Router 上，不在 Snapshot 里
    mu       sync.Mutex
    accounts map[string]*account // key: provider name
    path     string
    dirty    bool
}
type account struct {
    rings     map[string]*Ring   // 每条 Limit 一个环（key = Limit 的稳定标识）
    estimated int64              // 本周期由降级估算贡献的量，用于标注可信度
}
```

**职责切分**：Registry 只存"消耗了多少"这个**事实**；"额度是多少、怎么折算"这套**政策**
（`amount` / `token_weights` / `model_multipliers` / `pricing.yaml` 费率）始终从 Snapshot 现读。
于是热重载改任何一项都立刻生效、且不重置计数，无需任何迁移逻辑。

> 这条切分同时决定了 `Counters` 为什么按分量存原始 token 而不是存折算后的数值：
> 折算参数是可变政策，把它烘焙进历史数据会让每次调参都需要一次数据迁移。

**Key 用 provider name，刻意不含 API Key 哈希**——与 `Endpoint.HealthKey()` 相反，是有意为之：
HealthKey 含密钥哈希是为了"换 key 就重新试探健康"，方向安全；但对额度而言，轮换密钥（同一账号）
清零当月计数会直接导致超支。两个方向的风险不对称，必须在代码注释里写明原因，否则后人一定会"顺手统一"。

**Spec 落点**：`core.QuotaSpec`（`{ModelMultipliers, TokenWeights, Limits []Limit}`）定义在 `core`——`config` 校验它、`quota` 计算它、
`router` 读它，放 `core` 是唯一不产生环的位置（与 `core.StickyBackstopTTL` 同一先例）。
`BuildSnapshot` 把同一个 `*core.QuotaSpec` 指针挂到该 Provider 展开出的所有 `core.Endpoint` 上
（`nil` = 无套餐），于是排序时取额度是一次字段读，而不是对 `Cfg.Providers` 做线性查找。

### 9.3 持久化

* 文件 `<log_dir>/vmr-quota.json`，0600（与审计文件同级同权限；不匹配 `vmr-audit-*` glob，
  不污染 `vmr report` 的输入）。不新增配置项。
* `Charge` 只置 `dirty`；由 `vmr start` 启动的单个 flusher goroutine 每 5s 落盘，
  进程退出时（已有的 SIGINT/SIGTERM + `srv.Shutdown` 路径）强制 flush。临时文件 + `rename` 原子替换。
* 硬 kill 最坏损失 5 秒计量，对周期额度可忽略。
* 文件缺失/损坏 = 从零开始，只打一条日志，**绝不阻止启动**：统计辅助设施不该有能力让路由停摆。

---

## 10. 与既有机制的交互

| 机制 | 交互 | 结论 |
|---|---|---|
| Sticky | quota 重排先跑，sticky `moveToFront` 后跑并覆盖 | 会话黏性优先，明确不为省钱打断 cache |
| Health | 额度耗尽**不**触发冷却；真正的耗尽信号是上游 402/429，由既有状态机处理 | 两套机制各管各的，不交叉 |
| Failover | quota 只重排不淘汰，候选集大小不变 | failover 语义零改动 |
| 热重载 | Registry 挂 Router、不在 Snapshot 里 | 计数跨重载存活；额度值现读现用，改配置立刻生效 |
| 并发 | `Charge` 每次成功响应一次，`score` 每个新会话一次 | 普通 `sync.Mutex` 足够（对比一次 HTTP 往返，锁竞争不值一提），沿用 `health.Registry` 形状 |
| `vmr replay` | 会真实调用上游，因此**会计费** | 与真实流量一致，符合直觉，无需特殊处理 |
| 后台探针 `probe` | 消耗少量额度，但不走 `forwardSuccess` | 不计费。与审计不记探针是同一口径，`docs/OUTSTANDING_ISSUES_opus-5.md` 已有记录 |

---

## 11. 可观测性

* **`X-VMR-Route-Reason`**：`routeReason` 增加 `quota` 字段，重排真正改变队首时渲染
  `pick=quota q=<provider>:<最紧 Limit>:<score>`。**因为 `server/recorder.go` 已把响应头写进审计记录，
  这条路由理由自动进入飞行记录仪，零 schema 变更、零 `internal/report` 连带改动**——
  这是刻意选择的方案，替代"给 `audit.Record` 加字段"（后者按 CLAUDE.md 必须同步改 report 及其测试）。
* **`/admin/status`**：新增 `quota` 段，每个 Provider 的每条 Limit 一行：
  `metric` / `window` / `used` / `amount` / `pct` / `headroom` / `role(bucket|gate)` / `window_ends_at` /
  `estimated_pct`，外加该 Provider 的最终 `score` 与它由哪条 Limit 决定，
  以及生效中的 `model_multipliers` / `token_weights`——
  **促销倍率过期后忘记改配置是可预见的失效模式，把生效值显示出来是最便宜的防呆**。
  这是运维者回答"为什么流量都压在这个账号上"的第一现场。
* **`vmr check`**：在既有路由表预览里打印各 Provider 的 Limit 配置（纯静态，不读运行态）。
* **live log**：**不加**。日志行已很密，额度是"当前状态"而非"本次请求发生了什么"，它属于 `/admin/status`。
* **`vmr report`**：阶段二，`section_quota.go`（新 section = 新文件，符合 archtest 约定）。

---

## 12. 决策与取舍

| 决策 | 选择 | 理由 | 放弃的备选 |
|---|---|---|---|
| 计量单位 | requests / tokens / cost 三档均在阶段一交付 | 三者各自覆盖的市场份额都不小：Coding Plan 74% 按次数、Token Plan 94% 按 Token/Credits；漏掉任一档都会让功能对相应人群失效 | 只做 tokens（对 Coding Plan 失效）；只做 requests（对 62% 的 Token Plan 失效）|
| 权重信号 | `headroom = 剩余额度比例 / 剩余时间比例` | 目标是"过期前烧完"，配速问题不是存量问题；无量纲故可跨 metric 比较 | `remaining/total`——重置日不对齐时信号反向，且多窗口/多 metric 下无定义 |
| 多窗口归并 | 取所有 Limit 的 `min` | "最紧的约束说了算"是多约束的标准语义，一个循环 | 加权平均（会让一个已触顶的闸被其他窗口稀释掉） |
| 桶 vs 闸 | 周期最长者为桶，其余为闸（闸只压制不提升） | 用满 5h 窗口没有经济价值，只有计费周期的未用额度对应真实浪费；零配置且在每个观察到的套餐上都成立 | 全部当桶（会在 5h 窗口末尾制造无意义的冲量） |
| 分配策略 | 贪心取最高 score（稳定排序） | 无状态、确定性、failover 顺序自动正确、对 prefix cache 友好 | SWRR——需持久化累加器、只解决"选谁"、撒开流量反伤 cache |
| 接入点 | `Sort` 之后、`sticky` 之前的独立一步 | Sticky 已是同形状先例；`Dimension.Compare` 结构上看不到请求，且是 CLAUDE.md 明令不得扩展的接口 | 新增 `Dimension` |
| 重排粒度 | 并列梯队内，只在"挂了 Limit"的成员之间做占位重排 | 保住 `priority` 语义；让配置一个 Provider 的额度不产生隔空影响 | 全局重排；把未挂额度端点当 score=0 |
| 耗尽处理 | 只降到梯队末位，不熔断 | 计数器是估算值，按估算值执行破坏性动作 = 自制故障；硬信号是上游 402/429，health 已覆盖 | `on_exhausted: block` |
| 窗口实现 | 环形分桶，tumbling=1 桶、rolling=K 桶 | 一套结构覆盖两类；滚动误差 ≤1/K；tumbling 硬近似 rolling 误差可达 100% | 精确滑动窗口（需存每请求时间戳）；只支持 tumbling |
| 周期重置 | 惰性比较桶起点 | 无 goroutine、无漏重置、无时钟回拨、重启自动补偿 | 定时器/cron 式重置 |
| 周期表达 | `(every, since)` 二元组 | 一个机制覆盖数小时/数日/数周/数月 + 任意锚点，免掉字段膨胀 | `reset_period` + `reset_day`（装不下 5h/周，且要不断加字段） |
| 分量加权 | **必做**，两条路径：账号内比例统一走 `tokens` + `token_weights`；按模型分化走 `cost` + `pricing.yaml` | Credits 制套餐 cache read 比 fresh input 便宜 5～120 倍，等权总量记账高估 3～8 倍；而字节 AFP 这类非金额套餐不该被迫去反推 ¥ 单价 | 只做等权总 token（高估 3～8 倍）；只做 `cost`（逼着 token 桶套餐维护价目表）；per-provider 四个**金额**全局权重（费率实际按模型给出） |
| 折算规则的层级 | 账号级（`providers[].quota`），不放窗口级 | 一次调用扣多少是"账号怎么记账"，在多长窗口累计是"厂商怎么切周期"，两者正交；观察到的套餐里同账号各窗口的系数完全一致 | 每条 Limit 各写一份（同一事实抄 N 遍，"三处改了两处"的经典 bug 源） |
| 折扣与促销 | 不新增机制：固定折扣与单模型促销走 `model_multipliers`（含 `"*"` 通配），限时与分时走 `pricing.yaml` 已有的 `date_*`/`hour_*` 时间窗 | 核对代码确认 `PricingRate` 已有四个时间窗字段且 first-match-wins，其注释举的例子正是"折扣费率 + 全价兜底" | 为折扣新开一张表（与 `pricing.yaml` 形成两套口径）；把折扣烘焙进 `amount`（不可解释、促销结束后无从回滚） |
| 存储粒度 | 存原始分量 token，折算在读取侧套 | 折算参数是可变政策；烘焙进历史数据会让每次调参都需要数据迁移 | 存折算后的数值 |
| rolling 与桶 | rolling 永不当桶；全 rolling 的账号没有桶 | 滚动窗口额度持续再生、不作废，use-it-or-lose-it 不成立；且其 `time_left_frac ≡ 1` 使 `raw ≤ 1`，当桶会退化成畸形闸 | 按"周期最长"一刀切（会静默产生一个给不出提升信号的假桶） |
| 计量精度 | usage 为准、估算降级、标注估算占比 | 粗决策容忍 ±30%，阶段二额度预测看板不容忍 | 全程估算；无 usage 时放弃计费 |
| 并发超冲 | 不做预扣 | 超冲上界约 0.5%，换掉一个三态子系统与其泄漏风险 | 预扣 + 对账 + 回滚 |
| Registry key | Provider name，不含密钥哈希 | 轮换密钥不应清零当周期计数；与 HealthKey 的风险方向相反 | 照抄 `HealthKey()` |
| 计量来源 | 抽象成 `Source` 接口，阶段一只实现本地累计 | 已确认 MiniMax/智谱有私有用量 API；接口零成本，将来接入不必重构 | 写死本地累计 |
| 可解释性 | 编码进已有的 `X-VMR-Route-Reason` | 该头已被 recorder 记进审计；零 schema 变更 | 给 `audit.Record` 加字段（必须同步改 report 及其测试） |

---

## 13. 范围边界与已知限制

`vmr` 的额度计数**本质是一个估算值**。下表把"决定不做的事"和"做了之后剩下的偏差"合并成一张表——
两者本来就是同一个问题的两面：每一条限制都对应一个明确的范围决定，每一个范围决定都留下可预期的后果。

| 事项 | 决定 | 理由 | 后果与缓解 |
|---|---|---|---|
| 厂商计数单位 ≠ vmr 可观测单位 | 需用户实测校准 | prompt / message 这类单位在网络层根本不可见 | `amount` 必须按 vmr 口径配置（阶跃星辰 1 prompt ≈ 15 次请求）；官方用量 API 是根治手段 |
| 绕过 vmr 的直连流量 | 无法计入 | 结构性不可能 | 本地计数低估；`Source` 接口接官方用量 API 可消解 |
| 多实例共享计数 | 不做 | 单二进制、零 DB 是产品前提 | 多实例各自低估；单实例部署，或接官方用量 API |
| 时段倍率（`requests`/`tokens` 档） | 不做 | 需新配置面，且只有智谱一家、规则还在变 | 对这类账号系统性低估；改用 `cost` 档（`pricing.yaml` 的 `hour_*` 直接支持）或调低 `amount` |
| `cost` 档分量比例与价目表不同 | 接受 | 按模型的绝对费率已能覆盖绝大多数情形 | 改用 `tokens` + `token_weights`（换取精确比例，损失按模型粒度） |
| 降级估算拆不出缓存分量 | 保守按无缓存折算 | `EstimateTextTokens` 只给总量 | 高估消耗（对闸安全、对桶略少用）；计入 `estimated` 占比，`/admin/status` 可见 |
| 额度耗尽硬熔断 | 不做 | 按估算值执行破坏性动作 = 自制故障 | 硬信号是上游 402/429，既有 health 状态机已覆盖 |
| 精确滑动窗口 | 不做 | 需存每请求时间戳，内存与持久化高一个量级 | 分桶误差 ≤ 1/K（12 桶即 ≤ 8.3%） |
| 周期边界精确复刻厂商账单 | 不做 | 厂商账单时区与小时精度不公开 | 本地时区 + 日级锚点的近似，需在用户文档写明 |
| 单个长会话独吞一个套餐 | 接受 | sticky 优先级高于额度（§6.1 不变量 2） | 阶段二看板负责让它可见 |
| 按下游用户/业务线的实时配额 | 不做 | 需在请求路径引入下游身份维度的配额状态，比 Provider 级重得多 | 事后可见性走 `ClientKeyTag`（已逐请求进审计，`report`/`story` 已能分组） |
| 并发 / RPM 上限 | 不在本设计范围 | `headroom` 只看额度桶与限额闸 | 撞并发上限由 health 状态机兜底——是另一个问题，不是本设计的缺陷 |
| 并发新会话的计量超冲 | 不做预扣 | 超冲上界约 0.5%（§7.4） | 可忽略；换掉了一个三态子系统与其泄漏风险 |

---

## 14. 实施与验证

### 14.1 落地顺序

每一步都可独立编译、独立验证、独立回滚：

0. **前置重构：`internal/report/pricing.go` → `internal/pricing`**。已核对可干净下沉
   （只依赖 `fmtutil` + `i18n` + yaml，无 report 内部耦合）。新包只提供**原始费率查询**，
   不提供成本公式——`report` 保留自己那套（排除 cache read），`quota` 用自己那套（包含 cache read）。
   纯搬迁 + 一次接口收窄，`report` 的测试必须全绿后才继续。
1. **`internal/quota`**：`Counters` / `Ring`（两种边界策略）/ `PeriodStart`/`PeriodEnd` / `Registry` / 持久化。
   纯逻辑 + 纯函数，全部可单测，不碰请求路径。
2. **`core.QuotaSpec` + `config` 解析校验 + `vmr check` 打印**。此时行为零变化。
3. **接线**：`BuildSnapshot` 挂 spec；`Router` 持有 `*quota.Registry`；启动加载、退出落盘、flusher goroutine。
   仍无路由行为变化。
4. **`metric: requests` 计量**：`forwardSuccess` 里 `+1 × multiplier`。**零解析成本，先落这一档。**
5. **`metric: tokens` / `metric: cost` 计量**：`chatmsg` 增加逐块入口 → `respStream` 嗅探 →
   降级估算 → 两种折算公式（`tokens` 取总量；`cost` 按分量查 `internal/pricing` 的费率）。
   两者共用同一套嗅探，只在最后的折算上分叉。
6. **决策**：`internal/router/quota.go`（**新文件，不进 `router.go`**——archtest 有 700 行预算，
   当前 561 行）：梯队切分 + score 排序；`Serve` 加一行调用；`routeReason` 加 `quota` 字段。

> **风险控制的关键切分**：第 0–5 步做完时**路由决策完全没变**。可以先在生产只跑计量，
> 用 `/admin/status` 对着厂商控制台校准几天，确认数字可信（尤其是 §2.3 指出的**单位换算**问题、
> 以及 `cost` 档的费率是否与该套餐的真实 Credits 折算率成比例）之后再开第 6 步。
> 整套机制建立在一个估算出来的计数器上，"先只观测、后再决策"是主要的降险手段。

### 14.2 测试

* **周期与窗口数学**（纯函数，最易错也最易测）：`since` 落在 31 日时对短月的截断（1/31 → 2/28、2/29）、
  跨年、`every: 2w`/`3d` 一类多倍周期、DST 切换日、`PeriodStart`/`PeriodEnd` 自洽性、
  环形桶在滚动窗口下的求和与过期复用。
* **Headroom**：§1.1 的"三套餐重置日错开"场景直接做成断言——它是整个设计的立论依据，必须钉住；
  桶与闸的角色判定（最长的 **tumbling** Limit 为桶）；**全 rolling 的账号没有桶、全部按闸**；
  闸只压制不提升（`raw > 1` 时 headroom 仍 ≤ 1）；多 Limit 取 min；`ε`/`HeadroomCap` 的 clamp；
  §5.2 阿里云 Pro 那组算例（`score = 0.833`）直接作为数值断言。
* **梯队切分**：`priority` 分层时不跨层重排；`dims` 为空时全体同层；未挂 Limit 的成员位置不变。
* **Scope 与折算**：`models:` 过滤只对匹配模型计费；`model_multipliers` 按**上游**模型名生效
  （非虚拟模型名）；`"*"` 通配项在有具名项时不覆盖具名项。
* **账号级折算的层级语义**：同一账号的多条 Limit 共用同一套 `model_multipliers`/`token_weights`
  ——断言改一处对所有窗口同时生效（这条钉住的正是"折算规则为什么放账号级"）。
* **三种 metric 的折算**：以 `docs/TokenPlan_Market_Reference.md` 的真实折算率做夹具——
  小米·MiMo（缓存 2.5 / 输入 300 / 输出 600 Credits per Token）与阿里·百炼
  （输入 5K / 命中 25K / 输出 0.83K Token per Credit）各做一组，断言 `cost` 档与
  "等权总 token"的比值落在 3～8 倍区间（该文档已量化：MiMo 7.9 倍、百炼 3.0 倍）——
  这条测试钉住的是"为什么必须做分量折算"这个立论本身。
* **`tokens` 档的退化等价性**：`token_weights` 全为 1.0 时 `base(tokens)` 必须逐字节等于 `In + Out`
  ——保证"不配置就是原来的行为"这个承诺。
* **折扣层不重复计算**：`cost` + `model_multipliers: {"*": 0.6}` 的结果必须恰为
  纯 `cost` 的 0.6 倍，断言折扣系数没有被费率表二次套用。
* **`pricing.yaml` 时间窗**：促销费率（`date_from`/`date_to`）与分时费率（`hour_from`/`hour_to`，
  含跨零点）在 quota 计量侧按 first-match-wins 生效——复用 `internal/pricing` 既有测试的同类夹具，
  只补"quota 侧确实按请求时刻取到了正确那条费率"这一条。
* **费率缺失**：`metric: cost` 但 `pricing.yaml` 无对应 `provider+model` 时必须是加载期错误，
  断言它不会静默按 0 计费（静默按 0 会让账号永远显示额度充裕，是最坏的失效模式）。
* **配置校验**：`token_weights` 配在没有任何 `metric: tokens` 的账号上必须报错，不能静默无效。
* **惰性重置**：跨周期的 `Charge`/读取均触发清零；重启后从文件加载并补偿重置。
* **不变量回归**：sticky 命中时 quota 重排结果被覆盖；耗尽端点仍在候选集里（不被淘汰）；
  额度耗尽不产生 health 冷却。
* **`-race`**：`Registry` 的并发 `Charge` / 读取 / flush（health/audit/router 并发改动的既有惯例）。
* **archtest**：`router` → `chatmsg`/`quota` 不触发边界规则（已核对 `chatmsg` 仅依赖 `fmtutil`，无环，
  `forbiddenImports` 无相关条目）；`router.go` 行数仍在预算内。
* **loadtest**：现有场景矩阵跑一遍确认无回归。`requests` 计量开销应完全不可测；
  token 嗅探的额外开销预期落在噪音里（每事件一次 `bytes.Contains`），
  若 `stream_normal` 的 p95 出现可测量变化，说明门禁写错了。

### 14.3 验收标准

1. 未配置 `limits:` 的现有配置，行为与改动前逐字节一致（唯一可接受差异是 `/admin/status` 多一个空段）。
2. 只做到第 5 步（只计量不决策）时，路由行为仍与改动前一致。
3. 三套餐错开重置日的场景下，快到期且有余量的套餐拿到新会话——即 §1.1 的反例被修复。
4. 阿里云 Pro 形态（5h/周/月三层）下，5h 窗口接近饱和时该账号被压制，窗口翻转后自动恢复，
   且月度桶的进度不因此失衡。
5. `/admin/status` 的 `used` 与厂商控制台偏差在可解释范围内（差异来源只应是：绕过 vmr 的流量、
   探针、单位换算、时段倍率）。

---
