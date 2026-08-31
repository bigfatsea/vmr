<!-- Ver 2026-08-22, by Sonnet 5 -->

# Virtual Model Router (vmr) — 设计方案 · Token-Plan 额度感知路由（Quota-Aware Routing）

**这是 v4 版设计文档里路由半区的一个子系统专篇。** 路由核心本身（虚拟模型、协议透传、
Adapter、调度与健康、审计日志格式）见 `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）；
本文描述的额度感知重排是挂在它的调度链上的一步（见 §6.1 在 `Serve` 中的位置），
之所以独立成篇而不并进 Part 1，是因为它自带一整套计量/定价/周期模型，体量已经撑起一份
完整设计文档，而 Part 1 只需要知道"`Sort` 之后、`sticky` 之前多了一步重排"。

配套文档：
- `docs/VirtualModelRouter_Design_v4_Strategy.md` —— 战略与竞品，定义"为什么做"。
- `docs/VirtualModelRouter_Design_v4_Analytics.md`（Part 2）—— 分析半区。它是本文
  §9.3 那份状态文件的离线读者，两侧的口径一致性纪律见 §12.1「额度公式的唯一实现」。

本文只解决"怎么做"，范围严格限定在 Router 侧。
**§1–§13 描述的是设计终态；实际交付分四批，见 §14——第一批的范围远小于终态。
当前逐项落地状态、已知缺口与后续建议见文末「现状与后续计划」一节。**

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

## 2. 套餐的形态分类

> **本节只做分类，不记录"哪家用哪种"。** 具体厂商用什么计量、什么额度、什么折扣，是**配置阶段的数据**，
> 不是设计阶段的输入——它churn 得很快（同一家厂商的新旧两代套餐就可能从次数制换成 Token 制），
> 写进设计文档只会变成一条读起来权威、实际过期的断言。
> 实例数据全部留在 `docs/data/TokenPlan_Market_Reference.md`（81 个在售套餐的结构化快照 +
> 原始 JSON 留档），本节只引用它的**统计结论**——因为统计分布才是驱动设计的证据。

### 2.1 三个正交维度

任何一个套餐都可以由三个维度描述，它们**互相独立、可自由组合**：

| 维度 | 取值 |
|---|---|
| **计量单位** | 次数（requests） / Token 总量 / 金额或 Credits |
| **限额窗口** | 单层 或 多层并存；每层各有长度（数小时～数月）、锚点、以及固定重置 or 滚动 |
| **折算规则** | 无 / 按模型的倍率 / 按 token 分量的不同比例 / 两者兼有 |

把它们组合起来，实际市场上稳定出现的是这几类（后文一律用类型代号，不点名厂商）：

| 类型 | 计量 | 窗口 | 折算 |
|---|---|---|---|
| **A** | 次数 | 多层并存（时/周/月） | 无 |
| **B** | 次数 | 滚动短窗 + 固定长窗 | 按模型倍率 |
| **C** | Token 总量 | 单层 | 无 |
| **D** | Token 总量 | 多层并存 | 按模型倍率 + 分量比例 |
| **E** | 金额 / Credits | 单层 | 按模型的分量费率 + 账号折扣 |
| **F** | 金额 / Credits | 多层并存 | 同 E |
| **—** | 纯按量计费端点 | 无限额 | — |

### 2.2 统计结论与设计推论

完整的 81 个套餐结构化数据、按厂商的明细都在 `docs/data/TokenPlan_Market_Reference.md`，
不在本文重复；这里只留下驱动设计决策的结论本身：

| 结论 | 数据 | → 设计推论 |
|---|---|---|
| **Token 才是整体主流口径，requests 不是** | 全部套餐 77% 有 Token 限额，仅 40% 有请求数限额；"次数计量为主"只在 Coding Plan（38%）内成立、其内 74% 按次数计，而数量更多的 Token Plan（62%，且份额仍在向它收缩）94% 按 Token/Credits 计 | 三种 metric 都要支持，不能二选一 |
| **双 metric 并存是常态** | 22% 的套餐同时受请求数与 Token 两种 metric 约束 | 同一账号必须能挂多条不同 metric 的 Limit |
| **Credits 制套餐按分量折算，且比例极端** | 缓存读 vs 新鲜输入费率差 **5～120 倍**；按"输入输出 99:1、缓存命中率 90%"（市场对比站统一口径）量化，等权总 Token 记账会**高估 3～8 倍**——高缓存命中的 Agent 工作流，一个刚用掉 15% 的账号会显示成"已耗尽" | 必须支持分量加权 |
| **多窗口并存是常态，但并非所有套餐都有** | 有的三层（时/周/月）并存，有的只有单层，也有只有周限、没有月限的 | 窗口数量与长度都必须是配置项，不能写死 |
| **"上游无配额 API"要修正为"无标准 API，但部分厂商有私有 API"** | 已确认存在返回 5 小时 + 周窗口 + 模型级明细的私有查询接口，也存在带反爬、或只在客户端侧暴露百分比的形态 | 本地累计不该被写死成唯一数据源（§7.3） |

### 2.4 一个跨类型的共性陷阱：厂商计数单位 ≠ vmr 可观测单位

部分套餐按"一次用户提问"计数，而一次提问在网络层会展开成十几到二十几次上游 API 调用
（工具调用往返）。vmr 在网络层看到的是后者。

所以 `amount` 必须按 **vmr 可观测的口径**配置，不能直接抄套餐标称值。
这是**单位换算问题，不是精度问题**——必须在用户文档写明，
并且是"官方用量 API 校准"最有价值的场景。

## 3. 全集：额度约束的完整模型

把上表抽象干净，模型最终收敛为**高内聚的 Limit 实体列表**（P1/P2 初版曾设想为"账号级折算 + 窗口级限制"两层，P3 引入多窗口后根据真实需求订正为完全下沉至每条 Limit，见下文说明）：

```
Limit 规则实体（一条 Limit）—— 自包含计量单位、周期窗口、上限、Scope 范围与专属折算系数
  Metric           requests | tokens | cost
  Window           { 长度 N×{min,h,d,w,mo}, 锚点 since, 类型 tumbling | rolling }
  Amount           该窗口内的上限（vmr 可观测口径）
  Scope (models)   作用模型范围与计数隔离（缺省 = 全模型共享；["*"] = 全模型各自独立；[name,...] = 指定模型各自独立）
  TokenWeights     四分量权重 {in_fresh, cache_read, cache_write, out}（仅 metric: tokens 时有效，缺省全 1.0）
  ModelMultipliers 按模型扣减系数，支持 "*" 兜底（仅 requests/tokens 档有效，缺省 1.0）
```

**为什么折算规则最终下沉到 Limit 级而不是账号级**：
* **P1/P2 的原假设**：当时认为"一次调用消耗多少额度"是账号记账属性，所有窗口应共享同一套系数，以避免重复配置。
* **P3 的真实修正**：引入多窗口复合配额（例如 RPM 短窗限速 + 月度账单桶）后，真实场景中短窗速率闸往往只按原始次数或等权计费（不分缓存命中，也不乘模型系数），而月度账单桶则需要精准的四分量加权或模型倍率。如果留在账号级，用户将被迫让所有窗口共享同一套不适用的系数——这在表达力上是硬伤。因此在 P3 中正式将 `token_weights` 与 `model_multipliers` 移入每条 Limit 内部，赋予每条规则完全独立的计量与折算能力。

于是单条 Limit 的扣减量计算公式为：

```
charge(L) = base(L.Metric, L.TokenWeights) × L.ModelMultipliers[model]

  base(requests, _) = 1
  base(tokens, TW)  = Σ_c  tokens_c × TW[c]
  base(cost, _)     = Σ_c  tokens_c × Rate[provider, model, c, ts]     ← 费率表（§4.2 ①）
```

**一个账号可以挂多条 Limit，语义是 AND（最紧约束生效）：各 Limit 独立累计、独立判定，综合打分取木桶最低（见 §5）。**

验证这个模型能装下全集：

| 类型（§2.1） | 表达 |
|---|---|
| **A** 次数 + 多层窗口 | 3 条 Limit：`(requests, 5h)` / `(requests, 1w)` / `(requests, 1mo)` |
| **B** 次数 + 滚动短窗 + 模型倍率 | 2 条：`(requests, 1min)` / `(requests, 7d, model_multipliers: {premium: 3})` |
| **C** Token 总量 + 单层 | 1 条：`(tokens, 1w)`，无任何折算配置 |
| **D** Token 总量 + 多层 + 差异折算 | 3 条：`(tokens, 1min)`（无额外权重）/ `(tokens, 1w/1mo, token_weights: {...}, model_multipliers: {...})` |
| **E** 金额/Credits + 单层 | 1 条：`(cost, 1mo)`，分量费率来自费率表 + 账号折扣 |
| **F** 金额/Credits + 多层 | 3 条：`(cost, 5h/1w/1mo)` |
| **—** 纯按量端点 | 0 条 |
| 模型独立子额度 / 独立 RPM | 多条 Limit，各自 `models` 指向 `["*"]` 或具体模型列表，获得完全独立的计数器（Bucket） |

> 有一类纯金额制产品（订阅内含固定额度的编程助手）在模型上同样落在 **E**，
> 但它们**没有可重定向 `base_url` 的 API 出口，接不进 vmr**——
> 模型表达得了，不代表是目标场景。

模型成立。下一节做简化。

---

## 4. 简化：砍什么、留什么

原则：**在不损害"动态平衡"目标的前提下，砍掉配置与实现复杂度**。每条决策都必须有事实依据。

### 4.1 保留（有事实依据，砍了功能就不成立）

| 保留项 | 依据 | 实现代价 |
|---|---|---|
| **多条并存 Limit** | 22% 套餐两种 metric 并存；三层窗口是常态 | 极低——多窗口天然可用 `min()` 归并（见 §5） |
| **metric: requests** | Coding Plan 74% 按次数计 | **最低**——计数器 +1，完全不需要解析响应 |
| **metric: tokens** | 类型 C / D（Token 总量桶） | 中——需要 usage 嗅探 + 降级估算 |
| **metric: cost（分量折算）** | **Token Plan 94% 按 Token/Credits 计，且 Credits 按分量折算，比例差 5～120 倍** | 中——复用既有的按模型分项费率表 |
| **模型倍率（Limit 级，P3 订正）** | 类型 B / D 普遍存在，实测倍率跨度 2～9 倍 | 极低——一个 `map[string]float64` |
| **token 分量权重（Limit 级，P3 订正）** | 类型 D 是 Token 桶，但四个分量的扣减比例不均一 | 极低——四个 float，缺省全 1.0 |
| **Scope（按模型限定）** | 存在返回模型级明细的用量接口，提示可能有独立子额度 | 极低——charge 时一次模型名匹配 |
| **rolling 窗口** | 类型 B 的短窗是滚动的 | 低——分桶近似，约 40 行（见 §8） |

### 4.2 砍掉（有依据的简化）

**① 定价分三层：内置标准表 → 用户补充表 → 账号覆盖**

拆成三层，各自回答一个不同的问题：

| 层 | 回答的问题 | 键空间 | 位置 |
|---|---|---|---|
| **标准表** | 这个模型的**列表价**是多少 | canonical model id | 随二进制 `go:embed`（`internal/pricing`） |
| **补充表** | 标准表**没收录**的模型价格 | 同上（可回贡上游） | 用户文件，`pricing.supplement` |
| **账号覆盖** | **我这个账号**的实际成交价/折扣 | vmr 的 `provider` + `model`（用户自取名） | `config.yaml` 的 `providers[].pricing` |

三层而非两层，是因为补充表（列表价缺口，与账号无关、可回贡）与账号覆盖（谈到的折扣，永远不回贡）**键空间不同**。解析顺序：`providers[].pricing.overrides` 首条匹配（`discount` 形式 = 下层费率 × discount；显式费率形式直接采用）→ 补充表 ∪ 标准表（冲突时补充表胜出）→ 都没有则该 provider+model 无费率（`vmr report` 该行无 $ 估算；`metric: cost` **加载期错误**）。`rate` 是 `(provider, model)` 的纯函数，无时间戳参数（见 ④）。

**表内查键的四步**（`internal/pricing.resolveCanonicalKey`）：① `providers[].pricing.map` 的显式映射；② `<provider>/<model>`（provider 是 vmr 自取的名字，只在它恰好等于厂商前缀时命中）；③ 裸模型名——先当 canonical key 直查，再查表的**别名**（下文 ⑥）；④ `*/model` 后缀匹配，按**厂商优先级**定胜负（下文 ⑦）。任一步不命中就落到下一步。四步全不命中、且请求名带 `/` 时，还有一步**裸名重试**：用 `ModelBasename` 掐掉 org/路径前缀后，把同一套解析在裸名上重跑（递归，四步全部）——因为表侧 key 已被生成器归一化成两段，而聚合商 API 强制带 org 前缀的上游名（openrouter 的 `meta-llama/...`、together 的 `google/gemma-...`）不降级就永远够不到表里的行。重试只拓宽：原本四步能命中的名字答案不变，带前缀的名字命中后与裸名请求的答案**逐字节一致**（决策主体与规则都没变，只是输入形态被降到了同一命名空间）。仍未命中即“无费率”，不猜。`ModelBasename` 是“裸名”的唯一权威定义，生成器建 key、解析器降级、歧义报告分组三处同源引用，不存在第二份实现。

**⑥ 别名：裸模型名 → canonical key 的引用（不是价格拷贝）**

别名写在 `standard_price_curated.yaml` 的 `aliases:` 段（用户补充表也可以写，同名时覆盖）。它存的是**目标 key**，不是数字——刷新 generated 表时，每条别名的费率跟着一起动，别名本身永不过期。**只解析一跳**：目标本身又是别名，是加载期错误（`Table.ValidateAliases`），因此结构上不可能成环。目标不存在同样是加载期错误——否则一个拼错的别名会静默落回第④步，命中另一家厂商的价。

**别名负责"钉死一个模型该用谁的价"，优先级负责"运行时兜底"**——不是"优先级为主、别名补漏"。理由是两者的失效姿态不同：别名的目标消失是**加载期报错**（响亮），而优先级翻转是**静默**的，一个原本有价的名字会悄悄变成无价，报表里只是少一行。

2026-08-31 那次刷新就是活证据：`dashscope`（阿里百炼）新增了它转售 DeepSeek / Zhipu / Moonshot 的行，于是 `deepseek-v4-flash`、`glm-5.1`、`glm-5.2`、`kimi-k2.7-code` 四个模型一夜之间从"一个第一方，优先级能判"变成"两个第一方，优先级判不了"。其中 `deepseek-v4-flash` 是本仓库日志里流量最大的模型。没有报错，没有告警——如果当时不是已经写了别名，它会静默失价。

同一次刷新还暴露了优先级（第④步后缀匹配）在没有官方行可依时会**判错**：`deepseek-v4-flash-0731` 在 DeepSeek 自己名下没有行，优先级于是把 `dashscope`（一个转售商）当成第一方选中了它的加价。**注意这是第④步的猜错，与第②步不同：当 provider 本身就是某个平台（如 `dashscope`）且该平台有自己针对该模型的公开定价行时，第②步命中该行是平台自定价，不是猜错。**

根因在于："是不是转售商"是 **per (厂商, 模型)** 的属性——`dashscope` 对 Qwen 是第一方、对 DeepSeek 是转售商——per-vendor 的排序在原理上就装不下这种分裂。这不是名单没列全，是这个模型本身的天花板。

**别名不是手写清单，是标准表自己的一部分——覆盖全表，不是只覆盖某个部署路由的模型**。`standard_price_curated.yaml` 是给所有 vmr 用户用的公共参考表，只把某个部署恰好用到的模型钉死，等于让其余用户继续暴露在同一个静默失效的风险里。

**`aliases:` 只有一层，全在 `standard_price_curated.yaml`（生成器永不写别名）**。2026-08-31 快照共 **11 条**，只钉三类：① 两个非转售厂商共用同一裸名（per (厂商, 模型) 的分裂，per-vendor 排序原理上装不下，见上文 dashscope 例）；② 目标行本身是 curated 手工加的（LiteLLM 快照没有）；③ 网关自造的模型 id（压根不是任何厂商发布的名字）。曾经有过“生成器每次刷新自动给全表裸名钉别名”的设计（`generateAliases`，2026-08-31 覆盖 342 条），已彻底移除：生成表是纯机器价目表，“谁钉谁、钉向哪里”是人工决策，自动钉别名把这两层边界重新模糊掉；且单一厂商的裸名本就由厂商优先级无歧义解析、不需要别名免疫——刷新引入新的撞车时，靠 `tools/gen_standard_pricing` 每次跑完打印的歧义报告显式亮出来，而不是让一个 342 条的自动别名集悄悄兜底。

别名与厂商优先级共用**同一条判据**（`pricing.IsAggregatorVendor`）：`tools/gen_standard_pricing` 的歧义报告与运行时解析器都走 `Table.Ambiguities` 的同一份实现，不会各自推断而漂移。

**优先级不再沉默**：`tools/gen_standard_pricing` 每次刷新后打印一份歧义报告——哪些裸名撞车、各自被别名钉住还是交给优先级、优先级判给了谁、哪些打平未解析。两次刷新的输出一 diff，就是"这次刷新改变了我们用谁家的价"的答案。报告与解析器共用 `Table` 的数据和 `LookupPreferredSuffix` 的规则（`Table.Ambiguities`），不另起一套推断——否则报告本身就会和现实漂移。

**⑦ 后缀匹配的歧义由厂商优先级判定，而不是一律放弃**

裸模型名撞车是常态而非例外：2026-08-31 快照的合并表（804 行 generated + 9 行 curated，其中 2 条 curated 整行覆盖同键 generated 行，共 811 个唯一 key）里，709 个裸名中有 78 个被多个厂商前缀共享。实测这 78 处**全部**是“第一方 vs 转售/托管商”的形态，从来不是两个第一方对自家模型报不同价。于是规则是：**唯一一个非转售厂商的行直接胜出**（它的价就是这个模型的列表价，正是离线 $ 估算要表达的东西）；最高档位仍有多个候选时，回到“不猜”（两个转售商对别人家的模型各报各的，其中没有一个是权威答案）。78 处里 6 个被 curated 别名钉死、51 处因此自动解开，剩下 21 处全是纯转售商托管的开源模型——本来就不存在“官方列表价”。

转售商名单（`aggregatorVendors`）刻意取**短的那一边**：没听说过的厂商更可能是新的第一方而不是新的聚合商，误判成第一方在不撞车时零代价。单一命中（不论档位）行为不变——这条规则只会让更多名字解析出来，永远不会改变一个原本无歧义的名字解析到哪一行。

`metric: cost` 的严格性：**不仅"查不到条目"报错，"条目存在但缺少四分量中的任何一项"也报错**（缺失当 0 会低估消耗→账号显宽裕→拿更多流量→超支，见 §12.1"缺失费率的失败姿态"）。显式 `0.0`（上游对免费缓存就这么写）算"存在"，与"字段缺失"必须区分——生成脚本**不得把缺失补成 0**。

> **覆盖度必须诚实说明**：上游标准数据对西方主流厂商四分量齐全，对**国产第一方厂商明显偏弱**（全表 `cache_read` 覆盖率仅 23%、`cache_write` 仅 8%，而缓存费率恰是 `cost` 档最要命的、分量差 5～120 倍）。所以标准表是**消除入门断崖的基线，不是 `metric: cost` 的充分数据源**——设计上假定它经常缺失，这正是"四项不齐即报错"存在的理由。

标准表内部拆两块（合并后对外一张表）：`standard_price_generated.yaml`（脚本从上游生成、可整体覆盖）+ `standard_price_curated.yaml`（项目手工补国产厂商、脚本不得触碰——否则每次刷新手工行被清掉）。

**② 格式不向上游看齐，但键空间对齐**：**单位**保留 per-1M（比科学计数法少一个数量级出错机会）；**分量语义**与上游 `input_cost_per_token`/`cache_read_*`/`cache_creation_*` 等价、可机械转换；**币种**保留 vmr 的 `currency`/`exchange_rate`（否则表达不了非美元套餐）；曾有的 `date_*`/`hour_*` 促销时间窗功能面已移除（见 ④）。唯一改动的是**键**：标准表与补充表用上游的 canonical key 空间（生成脚本归一化成 `<litellm_provider>/<basename>`），账号覆盖仍用 vmr 自己的 `provider` + `model`——两套键空间因为它们回答两个不同的问题，这正是 `map` 字段存在的原因。

**③ 标准表以开源参考表的形式维护**——是一份参考数据文件加一个刷新脚本，不是产品也不是服务。四条护栏：过期比缺失更危险（表内带生成时间戳，`vmr check` 与报表免责声明一并显示、超阈值提醒）｜溯源可见（免责声明说明每行费率来自哪一层）｜许可与署名（生成部分派生自 MIT 上游数据）｜不承诺准确性（价格以厂商官方为准）。

**刷新节奏：两个月一次，或换用新模型时立刻刷。** 生成脚本自带 `-url`（默认直拉 LiteLLM 的 raw 地址），刷新是一条无参数命令：

```bash
go run ./tools/gen_standard_pricing -generated-at $(date +%F)   # 拉取 + 覆写 docs/data 快照 + 重生成表
```

脚本会把拉到的字节先校验成合法 JSON 对象再覆写 `docs/data/` 里的快照——404 页面或半截传输不会毁掉一份好快照——所以仓库里始终留着"这张表是从哪些字节生成的"这个可复现的输入。`vmr check` 在表龄超过 **60 天**时提示刷新。阈值从原来的 180 天收紧，是因为实测过：一份只有 20 天的快照，已经缺了本仓库自身流量正在用的四个模型的列表价；半年才报警的护栏不是护栏。

**手工行的取值纪律**：`standard_price_curated.yaml` 的每一个数字必须来自厂商自己的定价页并注明出处，**不做推断**。曾有 `zhipu/glm-5.*` 的 `cache_read` 按"输入价的 50%"推断（依据是缓存文档里的一个例子），后来对照定价页的"缓存命中"列发现推断值高了 2～2.3 倍。在缓存命中率 70–99% 的真实负载里 `cache_read` 是账单主项，推断出来的缓存价不是舍入误差。限时促销价（如"限时免费"的缓存写入）**留空而不是写 0**——本表没有时间维度，促销结束后那个 0 会永远低估。

**④ 折扣与促销归入价格层，不再借道 `model_multipliers`**：折扣若只在 `model_multipliers`（一个额度概念），`vmr report` 仍按列表价计算、系统性高报支出；写进价格层则**额度计量与成本报表同时受益**。整个账号打折 → `overrides: [{model: "*", discount: 0.6}]`；单模型价格不同 → 写显式四分量费率。有起止日期的限时活动、分时段价差**不做**——价格层只服务 `vmr report` 的 $ 精度这个二阶价值，不为一个使用频率未知的场景背上一整套时间可达性分析。`model_multipliers` 因此收敛回**纯额度语义**（"这个模型烧几倍计数单位"），**只作用于 `requests`/`tokens`**；某 provider 的 Limit 全是 `cost` 却配了 `model_multipliers` → 报错。

**⑤ 把 per-provider 定价并入 `config.yaml`**：`cost` 把定价放到了**请求路径**上，而独立的 `pricing.yaml` 不在热重载链路里，留在外面要另建文件监听 + 原子换表。并入后自动进 Snapshot / 热重载。落点：新叶子包 `internal/pricing`（与 `internal/quota` 同层，只依赖 `core`），`internal/config` 在 `validate()` 阶段解析 `providers[].pricing` + `pricing.supplement`、产出随 `core.Endpoint`/`core.Limit` 进 Snapshot——`metric: cost` 的"四项费率是否齐全"校验因此天然发生在加载期。`cmd/vmr/cmd_report.go` 的 `LoadPricing` 同样依赖 `internal/pricing`，与 config 层共用同一份解析逻辑（**两个消费者、一份实现**）；`internal/report` 依旧只接收已解析对象、绝不 import `config` 或 `pricing`（archtest 强制）。**破坏性变更**：独立 `pricing.yaml` 与 `vmr report -pricing` 取消，存量行迁入 `providers[].pricing.overrides`（多数行在有标准表后可直接删）。无 config.yaml 时跑 `vmr report` 降级为只用标准表列表价、不含账号折扣。

> **套餐账号的 $ 含义**：包月套餐边际价格是 0（钱已花）。报表的 $ 应理解为"这些流量若按量计费要花多少"——这恰好是判断"套餐买得值不值"的那个数。

**⑧ 报表金额的正式口径：按量计费等价成本（pay-as-you-go equivalent）**

上面那句注解现在是产品里明写的口径，`vmr analyze` 的 §2 标题与免责声明都按它措辞。完整定义：**这些流量若按定价体系里可解析到的公开价（渠道自定价或第一方列表价）逐 Token 计费，要花多少钱。**

三条推论，都是有意的：

- **它不是实付金额。** 包月/套餐账号的边际成本是 0；经转售商或代理（`bai`/`sensenova`/`cliproxy` 这类）的实际单价只有用户自己知道。
- **转售商没有自己的公开定价记录时，按第一方列表价计价是正确的默认**，不是将就。渠道有自定价时（第②步命中 provider 自有行，如 `dashscope` 对 DeepSeek 模型的自定价），用渠道价；需要覆盖时，配 `providers[].pricing.overrides` 或 `pricing.supplement` 即可——那正是第三层存在的理由。
- **它回答的问题是"这个套餐/代理买得值不值"**，而不是"我这个月被扣了多少"。后者以厂商控制台为准，vmr 不试图复现。

**⑨ 记账货币的折算因子是全局的，不挂在 provider 上**

USD 标准表 →（`pricing.exchange_rate`）→ 记账货币 `pricing.currency` →（`-currency` 的显示汇率）→ 报表展示币种。第一跳的因子是 `pricing:` 块的全局属性，**不是** per-provider 属性：审计日志会点名**曾经跑过**的每一个 provider（改过名的、删掉的、被 `api_keys` 拆分过的），这些名字在今天的 `config.yaml` 里没有条目。把因子挂在 per-provider 的策略上，它们就会以 1.0 折算，于是同一条 canonical 价格行在同一张表的相邻两行里，一行是 USD、一行是 CNY，差着整整一个汇率，而两行看上去都很正常。因子因此挂在 `pricing.Resolver` 上（`config.Config.PricingAccounting()` 提供），对每个 provider 一视同仁。

汇率换算一律走 **USD 单跳中枢**，不做 M×N 的任意币种链。

**⑥ 折算规则下沉至 Limit 级**：多窗口复合配额里，短窗速率闸通常按原始请求数/等权计量，长周期账单桶需要精确的 `token_weights` 或 `model_multipliers`。若留在账号级，用户被迫让所有窗口共享同一套不适用的系数——是表达力硬伤。因此 `token_weights` 与 `model_multipliers` 完全下沉至单条 Limit 内部，配置校验防呆（`token_weights` 仅在 `metric: tokens` 下合法、`model_multipliers` 在全 `cost` 下报错）。

**⑦ 不做 per-provider 的四个金额全局权重**：类型 E/F 的 Credits 折算率在厂商文档里就是**逐模型标注**的，一组 per-provider 全局权重对多模型账号必然系统性偏差，而费率表天然按模型。（与 ⑧ 的 `token_weights` 不冲突——那服务 token 桶型套餐，其账号内各模型共用一套分量比例。）

**⑧ `metric: tokens` 支持分量权重，缺省 1:1:1:1**：类型 C/D 账面单位是 token 但四分量扣减比例未必均一。缺省全 1.0 时 `base(tokens)` 恰好退化成 `In + Out`（简单场景零配置）；不必为非金额套餐去维护费率表；与 `cost` 结构同构（同一个"四分量加权求和"函数、两个权重来源）。判据：账号内各模型共用一套分量比例 → `tokens` + `token_weights`；分量费率按模型分化 → `cost` + 费率表。

**⑨ 时段倍率：三档 metric 都不做**——只在个别厂商出现、自身还在频繁调整，为它撑起一套时间可达性分析成本不成比例。代价是对这类账号系统性低估，缓解靠调低 `amount` 或接官方用量 API 校准。

**⑩ `(every, since)` 二元组**取代 `reset_period` + `reset_day`：`every: 1mo, since: 2026-08-14` 即"每月 14 号重置"，同一机制表达周锚点/5h 锚点/"每 2 周"/"每 3 天"，免掉字段膨胀。

**⑪ `initial_consumed_tokens` 初始偏置 → 砍**：只在第一个周期有意义，之后不会自己消失、长期造成系统性偏差。中途接入的对齐用手工编辑状态文件解决。

**⑫ SWRR 平滑加权轮询 → 砍，改用贪心（见 §5.3）**

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

四个关键性质：
- **无量纲**，因此次数制账号与 token 制账号可以直接比较；
- **自稳定**：线性消耗时分子分母同比缩小，`raw` 恒为 1。偏离自动纠正——这是一个不需要整定参数的比例控制器；
- **解决重置日错位**：§1.1 的例子中 C 的 `raw = 0.4/0.133 ≈ 3.0` 最高，正确胜出。
- **`raw < 1` 恰好等价于「已用% > 周期已过%」**：`(1-u)/(1-e) < 1 ⟺ 1-u < 1-e ⟺ u > e`。
  这个恒等式让"超前消耗"有了一个**不需要理解 headroom 就能读懂的等价表述**，任何要判断
  "这个账号烧得太快了吗"的下游消费者都应当直接用它，而不是自己另定一个绝对阈值——
  绝对阈值在不同周期长度下含义不同（一个 `every: 5h` 的账号在每个周期末尾都会正常越过
  90%，据此报警等于对正确配置持续报警）。`vmr report` 的额度耗尽 Finding 正是据此设计成
  **绝对阈值 ∧ 相对项**双条件：绝对量吃紧（≥90%）**且** `u > e`（烧得比周期流逝快）。

### 5.2 桶 vs 闸：两类 Limit 的角色不同

这是本设计最容易做错的一处。并非所有 Limit 都该驱动"快去烧额度"：

* **桶（Bucket）**——对应**计费周期**的那条 Limit。未用完的额度真的对应到花掉的钱，
  **use-it-or-lose-it 成立**，`raw > 1` 时应当**主动提升**权重。
* **闸（Gate）**——更短的窗口（5h、周）。它们是厂商用来平滑负载的**速率上限**，
  用满它们没有任何经济价值。**闸只应该在接近饱和时压制流量，绝不应该提升流量。**

判定规则，零配置：**周期最长的那条 tumbling Limit 是桶，其余全是闸。**
套餐费按最长周期收取，只有这个周期的未用额度才对应到真实浪费。该规则在每一个观察到的套餐上都成立
（类型 A：月=桶，周/时=闸；类型 B：周=桶，滚动短窗=闸；只配一条 tumbling 时它自己就是桶）。

**rolling 窗口永远不能当桶**——这是必须显式写死的一条边界，否则会静默算错：
滚动窗口的额度是持续再生的，不存在"到期作废"，因此 use-it-or-lose-it 对它不成立。
而且它的 `time_left_frac ≡ 1`，`raw` 恒 ≤ 1，当桶时永远给不出 `> 1` 的提升信号，
表现会退化成一个和闸一模一样、却顶着"桶"名义的畸形结构。
所以：**若一个账号的所有 Limit 都是 rolling，则它没有桶，全部按闸处理**——
语义上正确（没有会作废的额度，就没有"该主动烧掉"的理由）。

```
headroom(L) = raw(L)              L 是桶
            = min(1, raw(L))      L 是闸

score(provider) = min( over all its Limits ) headroom(L)      // 最紧的约束说了算
                  再 clamp 到 [0, HeadroomCap]                 // HeadroomCap = 5
```

闸的公式是一个**硬封顶**，不是渐进压制：`raw ≥ 1`（没落后于自己的节奏）时贡献中性的 `1.0`，
一旦落后（`raw < 1`）就和桶用同一条曲线往下掉。**这条硬封顶只做一件事——闸永远不会把分数抬到
比"刚好按节奏消耗"更高——不需要一个额外的、可调的"提前预警"缓冲带。**

> 不做提前压制的缓冲带（早先的 `GateReserve=0.5`，即 `raw < 2×` 就开始压制）：缓冲带的具体宽度没有任何数据支撑，而"闸不能把分数抬过中性值"这条真正要紧的不变量硬封顶已经完整给出。少一个无依据的魔数，比多一点"提前警觉"更值。

验证（类型 A，三层窗口）：5h 窗口已用 5500/6000（剩 8.3%）且只剩 1h（时间剩 20%）：

```
raw(5h)  = 0.083 / 0.2    = 0.417
闸(5h)   = min(1, 0.417)  = 0.417
桶(月)   = 1.0                       （按进度消耗）
score    = min(0.417, 1.0) = 0.417
```

压制幅度直接等于窗口自己落后节奏的比例，5h 窗口翻转后自动恢复；月度桶的进度不受影响。

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

**score 是每个新会话重算一次的**，所以"平手"不会造成持续独吞：几个刚重置的账号 score 相等时，
排第一的接下第一个会话、随即因计费而 score 下降，队首立刻换人——边际行为接近轮转。
真正的粒度损失只发生在两账号 score 交叉之前的那一小段，量级是一个会话。

残余的一种情形是并发突发：N 个新会话在任何一个计费落地前同时选中同一账号。
其额度影响已在 §7.4 量化（上界约 0.5%）；其并发影响由账号侧 429 → 既有 health 冷却与 failover 兜住。
**注意这不是本功能引入的**——今天没有额度感知时，`priority` 打平后的稳定排序本来就把全部流量
交给第一个端点，本功能只会改善它。

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

> **为什么不做硬门控淘汰（超额返回 429）**：本地额度计数是估算值（降级估算 ±30% 误差、多实例不共享状态、初始未校准偏差），拿不完全可信的本地数字直接拒绝对外请求是"自制故障"。真正的硬门控由上游 429/402 触发，`internal/health` 状态机已完备兜底。额度归零仅意味着该端点排到梯队末位（不从候选集剔除）——全部端点均超额时软重排仍会挑超额最轻的端点尝试一次（可能上游实际并未超额而成功），失败后由 failover 兜底。跨周期惰性清零使系统无需维护"淘汰后何时恢复"的状态机。

### 6.2 重排算法

```
func reorderByQuota(candidates []*core.Endpoint, dims []strategy.Dimension,
                    reg *quota.Registry, now time.Time) (changed bool)

  // 1. 切分并列梯队：沿 candidates 走，相邻两端点若对 dims 链中每个
  //    Dimension.Compare 都返回 0，则同属一个梯队。
  //    （不预设 priority 在链里；Sort 稳定，梯队内保持配置文件顺序；
  //     dims 为空则全体同梯队——config 的 applyDefaults 会把 strategy 补成 ["priority"]，
  //     所以空 dims 只在直接构造 ModelRoute 的测试里可达，仍需正确处理。）
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
| `tokens` | `Σ_c tokens_c × TokenWeights[c]`，权重账号级、缺省全 1.0 | 类型 C / D | 中，见 §7.2 |
| `cost` | `Σ_c tokens_c × Rate[provider, model, c, ts]`，费率见 §4.2 ① 的三层解析 | 类型 E / F | 同 `tokens`，多一次查表 |

`c` 遍历四个分量 `{in_fresh, cache_read, cache_write, out}`，其中
`in_fresh = usage.In − usage.CacheRead − usage.CacheWrite`
（`chatmsg.Usage` 的文档已明确 CacheRead/CacheWrite 都含在 In 里）。

**"一次扣减"的准确定义**（离线复算与对账都依赖它，必须写死）：计费发生在
`forwardSuccess` 内，即**每一次被转发出去的上游响应**（`status < 400`）扣一次，
且**转发途中流被截断也照扣**（字节已经过去了）。由此推出三条容易搞错的推论：

- 一个所有 attempt 都失败的请求，**一次都不扣**——但它在请求级统计里仍算作一次请求，
  记在最后尝试的那个端点头上。用"请求数"当扣减基数会系统性高估。
- 一次 failover（A 失败 → B 成功）只扣 B 一次，不扣 A。
- 截断的 2xx **扣了**，但它在多数"成功"口径里会被排除。所以"成功请求数"当基数会系统性低估。

于是对 `metric: requests`，"路由半区实际扣了多少"存在一个**恒等式**而非近似：
`已转发响应数 × ModelMultipliers[model]`（每次扣减都是 `base=1`，乘的是同一个常数，且不取整
——见下文"精度"一段）。任何离线复算若不用这个基数，就只是一个估算——这一点在 §12.1
「额度公式的唯一实现」一行里被立成了纪律。

**精度：`Counters` 全线 `float64`，计费时不取整。** 早先把五个原始分量存成 `int64`、`model_multipliers` 计费时 `math.Ceil` 向上取整（"取整方向必须偏安全"）——但取整的偏差不受配置者直觉控制（系数 `2.5` → `3` 是 +20%，`2.9` → `3` 只有 +3.4%，离整数边界越近偏差越离谱），且必要性完全来自"容器是整数"这个自我设限。改 `float64` 直接精确相乘后取整问题连根拔除；未配 `model_multipliers` 的账号（`mult == 1.0` 短路）字段永远是精确整数值浮点数、与旧 `int64` 逐位一致。

`tokens` 与 `cost` 是**同一个加权求和函数的两个权重来源**，实现上不是两条代码路径——
差别只在权重是账号级内联比例、还是按模型查价目表。
`TokenWeights` 全为 1.0 时 `base(tokens)` 恰好退化成 `In + Out`，简单场景零配置。

三档最终都要有，因为各自覆盖的市场份额都不小（见 §2.1）。但**交付分批**：
`requests` 与等权 `tokens` 属第一批（§14.3 P1），`cost` 与 `token_weights` 属第二批——
理由见 §14.2 的依据 ①：`headroom` 是账号内部的比值，计量单位上的常数倍偏差会自动约掉，
所以路由决策不必等到绝对单位做准。

**`cost` 不是可选项**：Token Plan 占在售套餐 62%，其中 94% 按 Token/Credits 计量，
而 Credits 的扣减是按分量折算的（cache read 比 fresh input 便宜 5～120 倍）。
用 `tokens` 的等权总量去记 Credits 制套餐，会**高估 3～8 倍**——
一个刚用掉 15% 的账号会显示成已耗尽，路由和看板同时失真。

### 7.2 token 计量：嗅探 + 降级

```
1) 上游 usage（权威）   ← respnorm.NormalizerStream 嗅探，复用 chatmsg 的解析口径
2) 本地估算（降级）     ← tokenutil.Estimate(请求体 / 响应体)
```

不能只用估算：误差约 ±30%，对"三个账号里选哪个"无所谓，但计数器同时要回答"这个月烧到 80% 了没有"，
并驱动阶段二的预测看板——那里 ±30% 不可接受。所以**能拿真值就用真值，拿不到才降级，
并在状态里标出本周期的估算占比**，让使用者知道数字有多可信。

拿不到 usage 的三种情况：上游响应带 `Content-Encoding`（`internal/respnorm` 按设计对压缩响应完全不解析）、
上游不返回 usage 字段、流被中途截断。

实现要点：挂在 `internal/respnorm` 已有的事件切分上嗅探，命中才 JSON 解析，绝大多数 token delta
事件直接跳过；解析与合并复用 `chatmsg` 既有能力，不在 `router` 里重新实现（守住 CLAUDE.md
"`chatmsg` 是消息解析唯一真相源"这条不变量）；只在 `forwardSuccess` 成功路径计费（429 基本
无消耗；中途截断的输入消耗算作可接受的低估）。唯一必须写死的正确性约束：**门禁必须作用于
重组后的完整事件（以及缓冲模式下的完整响应体），绝不能作用于原始 TCP 分片**——一个 usage
对象被网络层从中间切开时，逐分片检测会静默漏扫；相关的误报/边界场景已评估，见 §12.2。

**降级时的分量拆分**：`tokenutil.Estimate` 只能给出总量，拆不出 cache 命中比例。
所以降级路径按**无缓存**折算——请求体估算全部计入 `in_fresh`，响应体估算全部计入 `out`。
这个方向是**保守的（高估消耗）**：对闸而言偏安全，对桶而言会略微少用套餐。
每一笔降级计量都累加进 `account.estimated`，`/status` 因此能给出本周期的估算占比，
让运维者知道这个账号的数字有多可信——**这正是 `cost` 档最需要盯的指标**，
因为它的降级偏差比 `tokens` 档更大（缓存分量的费率差可达 5～120 倍）。

> **算这个占比时分子分母必须同单位**，这是个真实的坑：`estimated` 是**未加权**的原始
> token 数，而 `used` 已经套过 `base(metric)`。拿前者除以后者，只要有任何一个权重不是 1.0
> 结果就是错的（`out` 权重 5 倍时，一个 100% 靠估算的周期会被报成 20% 估算）。
> 正确的分母是**原始四分量之和**；`cost` 档则另算——它的分子是 `estimated_cost`、
> 分母是 `Counters.Cost`，两者都已经是金额。公式在 `internal/quota` 里只有一份
> （`EstimatedPct`），路由半区的 `/status` 与离线读者共用（§12.1）。

### 7.3 外部用量校准：留位置，但先不抽接口

§2 中"厂商有私有用量 API"那条结论意味着本地累计终究不该是唯一来源：
已确认存在的私有用量查询接口能一举消解本地计数最大的三条限制
（绕过 vmr 的流量、单位换算偏差、时段倍率）。

但**现在不定义 `Source` 之类的接口**。在只有一个实现时，接口只是把一个函数调用包了一层；
真实适配器的形状（认证方式、返回粒度、拉取频率、与本地计数的合并策略）要等写第一个适配器时才清楚，
提前定的接口大概率要返工。**写第一个适配器时再抽**（§14.3 P4）。

留位置的方式是**数据形状而非接口**：`Registry` 以 Provider 为 key、按窗口分别记账，
外部权威值将来可以直接覆盖某个窗口的 `used`，不需要改动存储结构——
这已经足够，不必再加一层预留抽象。

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
# 全局定价。standard 内置随二进制（无需配置）；supplement 是用户补充表，
# 按 canonical model id 与标准表合并，冲突时补充表胜出。
pricing:
  currency: CNY
  exchange_rate: {CNY: 7.1}
  supplement: ./pricing.yaml    # 可选；给了路径但文件不存在 = 加载期错误，不静默跳过

providers:
  # ── 类型 A：次数制 + 三层窗口 ────────────────────────────────
  - name: plan-a
    base_url: {anthropic-messages: https://example.invalid/anthropic}
    api_key: ${PLAN_A_KEY}
    quota:
      limits:
        - {metric: requests, every: 5h, amount: 6000}
        - {metric: requests, every: 1w, amount: 45000}
        - {metric: requests, every: 1mo, amount: 90000}   # 周期最长的 tumbling → 桶

  # ── 类型 B：次数制 + 短窗速率闸 + 模型倍率 ─────────────────────
  # amount 按 vmr 可观测的“上游 API 调用次数”配置，不是厂商标称的“提问数”（§2.4）。
  # rolling 仍不支持（见 §14.3「实际交付 vs 原始终态」）——用 min/h 级 tumbling 窗口
  # 近似同一类“短窗速率闸”场景；token_weights/model_multipliers 按 Limit 各写一份。
  - name: plan-b
    quota:
      limits:
        - {metric: requests, every: 1min, amount: 120, model_multipliers: {premium-model: 3}}
        - {metric: requests, every: 7d, amount: 35000, model_multipliers: {premium-model: 3}}

  # ── 类型 C：纯 Token 桶，什么都不配 = 分量 1:1:1:1 ─────────────
  - name: plan-c
    quota:
      limits:
        - {metric: tokens, every: 1w, since: 2026-08-14, amount: 65000000}

  # ── 类型 D：Token 桶 + 分量权重 + 模型系数 ────────────────────
  # 账号内各模型共用一套分量比例 → 用 tokens + token_weights，不必去维护费率表。
  # token_weights/model_multipliers 是 Limit 自己的字段（见 §12.1「折算规则的层级」
  # 一行的订正）——三条窗口若真的共用同一套比例，就得三处各写一遍；这里假设 5h 的
  # 速率闸窗口够短，厂商对它不做缓存折扣区分，所以只在两条更长的窗口上写了权重。
  - name: plan-d
    quota:
      limits:
        - {metric: tokens, every: 5h,  amount: 83000000}
        - metric: tokens
          every: 1w
          amount: 624000000
          token_weights: {in_fresh: 1.0, cache_read: 0.1, cache_write: 1.25, out: 4.0}
          model_multipliers: {"*": 1.0, heavy-model: 9}
        - metric: tokens
          every: 1mo
          amount: 1249000000
          token_weights: {in_fresh: 1.0, cache_read: 0.1, cache_write: 1.25, out: 4.0}
          model_multipliers: {"*": 1.0, heavy-model: 9}

  # ── 类型 E：金额 / Credits 制，单层窗口 ───────────────────────
  # 分量费率按模型分化 → 走 cost；价格三层解析（§4.2 ①）
  - name: plan-e
    pricing:
      map: {my-model-x: vendor/model-x}        # 仅在自动解析对不上 canonical key 时才需要
      overrides:
        # 该模型的实际分量费率与列表价不同 → 写显式费率（每 1M token），必须写在
        # 下面的通配兜底规则之前——first-match-wins，反过来写会让这条规则变成死配置
        - {model: my-model-x, in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54}
        # 兜底：整账号其余模型按列表价 6 折
        - {model: "*", discount: 0.6}
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 198}

  # ── 类型 F：金额制 + 多层窗口 ────────────────────────────────
  - name: plan-f
    quota:
      limits:
        - {metric: cost, every: 5h,  amount: 12}
        - {metric: cost, every: 1w,  amount: 30}
        - {metric: cost, every: 1mo, amount: 60}

  # ── 模型级独立子额度（Scope，已交付 P3）────────────────────────
  # models: 同时决定"哪些模型适用"和"是否独立计数"——不写 = 共享一个池，
  # 写了（无论是 "*" 还是具体列表）= 命中的每个模型各自独立开一个 bucket。
  - name: plan-with-submodel-cap
    quota:
      limits:
        - {metric: requests, every: 1mo, amount: 50000}                       # 账号总限，共享池
        - {metric: requests, every: 1d, amount: 200, models: [premium-model]} # 仅 premium-model 适用，它自己独立一个 bucket

  # ── 按模型独立限速，覆盖账号下全部模型（Scope 通配，已交付 P3）─────
  - name: plan-per-model-rpm
    quota:
      limits:
        - {metric: requests, every: 1min, amount: 60, models: ["*"]}  # 每个模型各自 60 次/分钟，互不影响
        - {metric: requests, every: 1mo,  amount: 90000}              # 账号总限，共享池
```

**全局定价字段**（`pricing`）：

| 字段 | 说明 | 缺省 |
|---|---|---|
| `currency` | `amount`（`cost` 档）与费率的基准币种 | 必填（有 `cost` Limit 时） |
| `exchange_rate` | 通用的"1 美元 = X `<货币代码>`"映射表，写成 `{CNY: 7.1}`——折算标准表币种（USD）到 `currency`，也是 `providers[].pricing.overrides` 里任何一行自带 `currency:` 字段的换算来源（override 就写在这份文件里，没有"退回到哪"的问题）；对 `pricing.supplement`/`pricing.standard` 文件而言只是**兜底**——那类文件可以在自己内部再声明一份同形状的 `exchange_rate:` 块，key 撞车时**文件自己的优先**，这样一份 supplement 可以脱离某个具体 `config.yaml` 独立搬用，不会因为部署改了记账汇率就让文件里"厂商官网核实过"的价格跟着漂移（见 `pricing.example.yaml`）；也是 `vmr report` 的 `-currency` 展示币种（当它和 `currency` 不同时）的换算来源。每个用到的货币都要有条目，USD 本身隐含为 1.0、不用写 | **必填（`currency` 非 USD 时）**——标准表同时喂给 `metric: cost` 计费和 `vmr report` 的 $ 列，缺汇率会产出"按 USD 算、按 `currency` 标"的数字；确实不需要换算时显式写 `{CNY: 1.0}` |
| `supplement` | 用户补充表路径，按 canonical key 与内置标准表合并、冲突时它胜出 | 空 |
| `standard` | 覆盖内置标准表（自备整表时用） | 内置 |

**账号级定价字段**（`providers[].pricing`，只写"和标准列表价不一样"的部分）：

| 字段 | 说明 | 缺省 |
|---|---|---|
| `map` | `本地 model 名 → 标准表 canonical key` 的映射，只在自动解析失败时才需要 | 空 |
| `overrides` | 费率覆盖规则**列表**，first-match-wins，静态按模型区分（没有时间窗——见"②格式要不要向上游看齐"一节）。每条含 `model`（支持 `"*"`）+ 二选一的 `discount`（乘下层解析出的费率）或四项显式费率；显式费率还可选带 `currency:`（这一行自己的币种，比如直接抄厂商美元发票的数字），加载期通过全局 `exchange_rate` 换算到 `currency`——只对显式费率有意义，和 `discount` 一起写是错误。一条模型专属规则必须写在覆盖它的通配 `"*"` 规则**之前**——反过来写、或两条规则的 `model` 完全相同，都是加载期错误（`firstDeadOverride`）：first-match-wins 下第二条永远轮不到 | 空 |

`map` 缺省时的**自动解析顺序**（`vmr check` 会把解析结果打出来，可审计）：
① `map` 显式项 → ② `<provider 名>/<model>` → ③ 裸 `<model>`（西方厂商多为此形）→
④ 全表中形如 `*/<model>` 的**唯一**匹配。四步都命中不了、或第 ④ 步匹配到多条（有歧义）
时，若请求名带 org/路径前缀，再按 `ModelBasename` 掐成裸名把①–④重跑一遍（同上选）。**最终仍命中不了、
或重跑后依然歧义时不做猜测**，按“无费率”处理——猜错一个费率比没有费率危险得多。

**窗口级字段**（`limits[]`，描述"在多长的周期里累计多少，以及这条 Limit 自己的折算规则"）：

| 字段 | 说明 | 缺省 |
|---|---|---|
| `metric` | `requests` \| `tokens` \| `cost` | 必填 |
| `every` | `N{min,h,d,w,mo}`，覆盖"数分钟/数小时/数日/数周/数月" | 必填 |
| `since` | 周期锚点时间，后续周期自动推算。三种写法：`YYYY-MM-DD`（当日 0 点）、RFC3339 完整时间戳（精确到秒+时区）、纯时间 `hh:mm[:ss]`（今天该时刻，**仅 `every: min/h` 合法**，`d/w/mo` 上用会在加载期报错——省得为了表达"每小时的第几分钟"硬凑一个无意义的日期） | 不写＝取配置加载/热重载那一刻，**对齐到固定日历边界**（min/h/d→当日午夜、w→周一 0 点、mo→月初）。这个对齐是必需的：`LimitKey` 不含 `since`，桶 key 稳定，但缺省锚点若取原始 `now`，每次热重载都会把锚点挪到重载时刻、`resetIfStaleLocked` 随之清零账号累计消耗（B2）。锚点定在午夜使周期栅格锁死到"日"——**同一自然日内的任意热重载对任意 `every` N 都解析出同一锚点、计数存活**。残余收窄为：`every: Nh` 且 N∤24（如 `5h`）或 `every: Nmin` 且 N∤1440（如 `7min`），且热重载/重启跨过自然日——此时至多一次相移重置。需要精确相位就显式写 `since` |
| `rolling` | 滚动窗口（分桶近似）；否则固定对齐窗口。**rolling 永不当桶**（§5.2）。仍是加载期"计划在后续批次支持"报错——见 §14.3「实际交付 vs 原始终态」，未随 P3 交付 | `false` |
| `amount` | 该窗口上限（vmr 可观测口径；`cost` 档为 `pricing.currency` 的币种） | 必填，>0 |
| `models` | 该 Limit 的 Scope，已随 P3 交付。**不写**＝所有模型共享一个池；写 **`["*"]`**＝规则适用全部模型，但每个模型各自独立开一个 bucket；写**具体列表**＝只有列出的模型适用，且同样各自独立。写不写这个字段，同时决定了"哪些模型适用"和"是否独立计数"——不需要额外的 `mode:` 字段。见 §12.1「Scope 的判定」一行 | 全部（共享） |
| `token_weights` | 仅本条 Limit 生效的分量权重，只在 `metric: tokens` 时合法——**按 Limit 配置，不是账号级**，见 §12.1「折算规则的层级」一行的订正 | 全 `1.0` |
| `model_multipliers` | 仅本条 Limit 生效的按模型倍率，`metric: cost` 时非法——**按 Limit 配置**，理由同上 | 未命中模型/无通配 = `1.0` |

**命名**：额度块叫 `quota`、价格块叫 `pricing`，都不叫 `budget`。`vmr report` 已有一套以金额为中心的
成本估算，配置里再出现一个 `budget` 却可能指次数或 token，是可预见的混淆源。

**校验**（`config.validate`，沿用现有 fail-fast 风格）：

* `metric` 枚举；`every` 语法与 `N > 0`（支持 `min`, `h`, `d`, `w`, `mo`）；`amount > 0`；
* `since` 格式校验：接受 `YYYY-MM-DD`、RFC3339 以及纯时间 `hh:mm[:ss]`；若使用纯时间格式，**仅允许**在 `every: min` 或 `every: h` 的 Limit 上配置，若用于 `d`/`w`/`mo` 则加载期报错；
* `models`（Scope）校验：`"*"` 为通配保留字面量，严禁与具名模型混写（如 `models: ["*", "gpt-4"]` 为加载期错误）；不支持 glob 模式；
* 同一 provider 下 Limit 冲突/重叠检测（`quota.ModelSetsOverlap`）：两条 Limit 若 `(metric, every)` 相同且其 `models` 作用域存在交集（同为共享池、或一方为 `*`、或具体模型列表重叠），判定为重复定义并**加载期报错**；
* `model_multipliers` / `token_weights` 的值必须 `> 0` 且为有限数（非 NaN / 非 Inf）；
* 某条 `metric: cost` 的 Limit 却配了 `model_multipliers` → 报错，提示改用 `pricing.overrides`
  （同一件事只留一种写法；这条校验按 Limit 各自独立判断）；
* `overrides` 每条规则的 `discount` 与显式费率**二选一**，同时出现是错误；`currency` 只对显式
  费率有意义，和 `discount` 一起写同样是错误（`discount` 是无量纲乘数，不存在币种）；
* `token_weights` 出现在**自己 `metric` 不是 `tokens`** 的 Limit 上 → 报错（配了不生效的字段，
  按 `KnownFields` 的同一精神必须显式失败，不能静默无效；同一 provider 下别的 Limit 是不是
  `tokens` 不影响这条判断，因为折算规则已经是 Limit 自己的字段——见 §12.1 的订正）；
* `pricing.map` 的每一条显式映射，其 canonical key 必须在合并后的标准表∪补充表里存在 → 否则**加载期错误**：
  用户手写的映射打错字时，若继续按四步解析往下走，可能静默匹配到**另一个模型**的费率——
  正是本节"有歧义不猜"要防的那类失败，只是这次的歧义来自 typo 而不是表本身；
* 所有数值字段（`amount`/`token_weights`/`model_multipliers`/`discount`/显式费率/`exchange_rate`）
  必须是**有限数**：YAML 的 `.nan`/`.inf` 是合法标量，而 `v <= 0` 对 NaN 恒为 false，
  只做符号检查会让 NaN 一路穿进 `ApplyModelMultiplier` 的乘法、`Counters.Add` 的累加、
  以及 `vmr-quota.json` 的持久化——NaN 一旦写入某个账号的 `Counters`，之后每一次 `Add`
  都会把污染传染给同一个桶的全部后续记账（`NaN + x` 恒为 `NaN`），`Pct`/`Headroom` 排序
  也随之失去确定性（`NaN` 与任何值比较都是 false）；
* `metric: cost` 涉及的每个上游模型，都必须解析出四项费率齐全的费率（显式写 `0.0` 算齐全，字段
  缺失不算）→ 否则**加载期错误**。绝不把缺失当 0：那会低估消耗、让账号显示得比实际宽裕，进而
  超支——是最坏的失效方向。没有时间维度后，一个 provider+model 只有唯一一条确定的解析路径
  （first-match-wins 命中的第一条规则，或落到 Base），校验只需要沿这条路径走一遍，不再需要
  对"这条规则在哪些时刻生效"做可达性分析；`firstDeadOverride`（上面 `overrides` 字段的说明）
  在此之前就已经把任何永远轮不到的规则拒之门外，所以这里遇到的第一条匹配规则必然就是唯一
  可达的那条。

### 9.2 运行态

**折算发生在读取侧还是计费侧，按 metric 分**：`Counters` 只存原始事实、`token_weights` 在读取侧套用（账号内共用同一套四分量权重，读取时用当前配置重新加权、历史数据永远有效）。但另外两项在**计费那一刻**就乘进去：

* **`model_multipliers`**——`Registry` 按 provider 聚合、不细分到 model；聚合完成后"这些量来自哪个模型、该乘几倍"已丢失。账号一旦配了它，`Counters.Requests`/`.Fresh` 的语义从"原始数"变成"模型加权后的等价单位"（一处需要在 `/status` 展示上讲清楚的语义变化）。
* **`metric: cost` 的金额**必须计费时算好写进 `Counters.Cost`。费率会随时间变化（标准表刷新、账号覆盖改配置），若读取时才重算，配置变更前后消耗的 token 混进同一聚合桶、只能整体按"读取那一刻"的费率算钱，旧费率下产生的那一笔金额就错了。`Counters` 对 `cost` 因此从"事实存储"变成"计费时刻预计算结果的累加器"。

```go
package quota   // internal/quota，仅依赖 core（周期数学是纯函数，无 I/O）

// Counters 的 Fresh/CacheRead/CacheWrite/Out/Requests 五个字段存原始事实，
// token_weights 全部在读取侧套用——改配置不会让已累计的历史作废。全部是
// float64，不是 int64：model_multipliers 一旦配置，就要求这些字段能精确
// 存下一个非整数倍率的计费结果（不取整——见上文"精度"一段），未配置时
// 它们永远是精确的整数值浮点数，与整数语义等价。
// Cost 是例外：metric: cost 的 Limit 在计费那一刻就把 $ 金额算好写入这里，
// 之后费率表再变也不影响已记录的历史值（理由见上）；requests/tokens 档它恒为 0。
type Counters struct {
    Fresh, CacheRead, CacheWrite, Out, Requests float64
    Cost float64
}

// bucket 是一个 (provider, limitKey) 的实时状态：当前认为自己在哪个周期、
// 该周期内累计了多少。惰性重置——见下文 Registry.Charge/Used。
type bucket struct {
    PeriodStart int64
    C           Counters
    Estimated   float64
}

type Registry struct {                        // 形状对齐 health.Registry：挂在 Router 上，不在 Snapshot 里
    mu       sync.Mutex
    accounts map[string]map[string]*bucket     // provider name -> limitKey -> bucket
    path     string
    dirty    bool
}
```

**职责切分**：Registry 只存"消耗了多少"这个**事实**；`amount`/`token_weights` 这套**政策**
始终从 Snapshot 现读，于是热重载改它们立刻生效、且不重置计数，无需任何迁移逻辑——
这条对 `model_multipliers`/费率表不成立（见上），是上面那条修正的直接原因，不是这里的例外。
"不重置计数"对**缺省 `since`** 的 Limit 同样成立，但依赖 `DefaultSince` 把缺省锚点对齐到
日历边界（见上方 `since` 行、及"已评估并否决的改进提案"表的 B2 条目）——早先直接取 `now` 会让每次热重载
经 `resetIfStaleLocked` 清零，那是一个真 bug，不是相位选择。

> 存原始分量 token 而不是存折算后的数值，这条选择对 `token_weights`（读取时套用）与
> `metric: cost`（`Fresh`/`CacheRead`/`CacheWrite`/`Out` 与计费时算好的 `Cost` 一起存，
> 前者供 `/status` 的分量明细展示用，不参与路由决策）依然成立：改配置、改价目表
> 都不需要数据迁移。`model_multipliers` 是唯一的例外——它在写入时就把倍率乘进了
> `Counters` 本身（见上），账号一旦配了它，`Fresh`/`Requests` 等字段存的就已经是
> 加权后的等价单位，不再是原始分量，这正是"计费时套用"这个选择带来的直接代价。

**Key 用 provider name，刻意不含 API Key 哈希**——与 `Endpoint.HealthKey()` 相反，是有意为之：
HealthKey 含密钥哈希是为了"换 key 就重新试探健康"，方向安全；但对额度而言，轮换密钥（同一账号）
清零当月计数会直接导致超支。两个方向的风险不对称，必须在代码注释里写明原因，否则后人一定会"顺手统一"。

**Spec 落点**：分成 **YAML 形状**与**运行态形状**两个类型，对齐 `config.EndpointGroup → core.Endpoint`
这个已有先例，而不是让一个类型同时背两副担子：

* `config.QuotaConfig` / `config.LimitConfig`——带 yaml 标签，`every`/`since` 需要自己的解析逻辑
  （Go 的 `time.ParseDuration` 不认识 `1mo`/`1w`，`config.Duration` 复用不了）；`validate()` 校验枚举与取值域。
* `core.QuotaSpec` / `core.Limit`——运行态形状，**无 yaml 标签**，周期已解析成结构化字段。

放 `core` 的理由仍成立且是硬约束：`core.Endpoint` 要挂这个指针，而 `core` 受 archtest 的
**零内部依赖**约束（`zeroInternalDepPackages`），不可能反向 import `quota`；同时 `config → core`
本来就存在（`validate` 已引用 `core.StickyBackstopTTL`），不产生环。

`BuildSnapshot` 负责转换，并把同一个 `*core.QuotaSpec` 指针挂到该 Provider 展开出的所有
`core.Endpoint` 上（`nil` = 无套餐），于是排序时取额度是一次字段读，而不是对 `Cfg.Providers` 做线性查找。

**定价解析结果的挂点不一样，因为它的粒度不一样**：`QuotaSpec` 是账号级、同一 Provider 下所有
`core.Endpoint` 共享一个指针是对的——额度本来就按账号记账。但价格是**按模型分化**的（§4.2⑦已论证，
市场数据也证实 Credits 折算率逐模型标注），账号级挂一份单一费率装不下多模型账号，所以
`PricingRate`（`internal/pricing.Resolve` 的产物）挂在 `core.Endpoint.PricingRate` 上——
`BuildSnapshot` 对每个 `provider+model` 组合各自解析一次，不像 `QuotaSpec` 那样整个账号共享。
`nil` 表示该端点没有解析出费率；一个配了 `metric: cost` 的账号若有端点解析不出费率，
在校验阶段就已经报错（§9.1 校验清单），不会留到运行时才发现 `nil`。

**实现落点**：`every`/`since` 保持普通 `string` 字段，解析与报错都在 `validate()` 里一次性做完、产出写进 `LimitConfig.Resolved core.Limit`（`yaml:"-"`，对 `KnownFields` 隐身）——不用自定义 `UnmarshalYAML`，符合 KISS，`PricingConfig`/`PricingOverride` 同理。`quota.LimitKey` 对 per-model Limit 带 `#model=<name>` 后缀（一条 `models: ["*"]`/具体列表的 Limit 给每个实际命中的模型各开一个独立 bucket，不共享计数器）。rolling 窗口未交付（§15.2 #4），`Registry` 因此是简单的 `map[string]map[string]*bucket`（provider name → `LimitKey` → bucket）、每个 bucket 是惰性重置的单一计数器，从 P1 沿用至今；等真交付 rolling 再把 `bucket` 换成能装下 `Ring` 的类型，不现在为一个还没有真实需求的功能预留骨架。

### 9.3 持久化

* 文件 `<log_dir>/vmr-quota.json`，0600（与审计文件同级同权限；不匹配 `vmr-audit-*` glob，
  不污染 `vmr report` 的输入）。不新增配置项。
* `Charge` 只置 `dirty`；由 `vmr start` 启动的单个 flusher goroutine 每 5s 落盘，
  进程退出时（已有的 SIGINT/SIGTERM + `srv.Shutdown` 路径）强制 flush。临时文件 + `rename` 原子替换。
* 硬 kill 最坏损失 5 秒计量，对周期额度可忽略。
* 文件缺失/损坏 = 从零开始，只打一条日志，**绝不阻止启动**：统计辅助设施不该有能力让路由停摆。

**这个文件是有离线读者的**（`vmr report` 的额度对照表），所以惰性重置的两个副作用从
"内部实现细节"升级成了**对外契约**，两者都必须由读者自己处理：

* **盘上的 `period_start` 可能属于更早的周期。** 重置是惰性的（读到才比较、才归零），
  进程停了一整个周期就不会有人去归零它。离线读者**必须**先把盘上的 `period_start` 与
  "此刻应处的周期起点"比对，不相等就不能当作"本周期已用"渲染——否则会把上个月的数字
  当本月展示。这条不是可选的健壮性处理，是这个文件格式的读取前提。
* **被替换掉的 `limitKey` 永不删除。** key 是 `metric + "/" + everyText`，而 `Registry` 是
  整份读入、整份写回、从不清理。把 `every: 1mo` 改成 `1w`，旧的 `requests/1mo` 桶会**永久**
  留在文件里。对路由半区无害（它只读当前 key），但对任何"这个账号有没有数据"的判断都是
  噪声源，且这个噪声**没有时间戳可以判断它有多旧**。清理的正确落点是路由半区
  `Registry.Load` 之后、首次 `Flush` 之前用当前配置的合法 key 集合过滤一次——它是这个文件
  的唯一写者；让离线读者去清理会把纯读路径变成读写路径，凭空引入并发写冲突。**尚未实现，
  见 §15.3。**

---

## 10. 与既有机制的交互

| 机制 | 交互 | 结论 |
|---|---|---|
| Sticky | quota 重排先跑，sticky `moveToFront` 后跑并覆盖 | 会话黏性优先，明确不为省钱打断 cache |
| Health | 额度耗尽**不**触发冷却；真正的耗尽信号是上游 402/429，由既有状态机处理 | 两套机制各管各的，不交叉 |
| Failover | quota 只重排不淘汰，候选集大小不变 | failover 语义零改动 |
| 热重载 | Registry 挂 Router、不在 Snapshot 里 | 计数跨重载存活（缺省 `since` 亦然，依赖 `DefaultSince` 的日历对齐——见"已评估并否决的改进提案"表 B2）；额度值现读现用，改配置立刻生效 |
| 并发 | `Charge` 每次成功响应一次，`score` 每个新会话一次 | 普通 `sync.Mutex` 足够（对比一次 HTTP 往返，锁竞争不值一提），沿用 `health.Registry` 形状 |
| `vmr replay` | **已计费**（2026-08-11 交付）——一次性 `quota.Registry` 加载 + 成功响应后计费 + 退出前 flush，不需要后台 flusher；usage 来自 `chatmsg.MergeUsageBytes` 读取已完整缓冲的响应体（而非 `internal/respnorm` 的增量嗅探），退化路径复用 `tokenutil.Estimate` | 计费管线（metric 分发 + model_multiplier + cost 定价）从 `chargeQuota` 抽成 `router.ChargeResponse`，供 `internal/replay` 与 `router` 共用同一实现；`>=400` 响应不计费，`-dry-run` 不触碰状态文件；见文末「现状与后续计划」一节 |
| 后台探针 `probe` | 消耗少量额度，但不走 `forwardSuccess` | 不计费。与审计不记探针是同一口径，`KNOWN_ISSUES` 已有记录 |
| 上游"额度耗尽"的硬信号 | `internal/adapter/classify.go` 已把 429 响应体里的 `quota`/`balance`/`credit` 关键词归类为 `ErrEndpoint` | 即**长冷却**（10 分钟起，指数退避到 1 小时）+ 切走。这正是"不做硬熔断"所依赖的既有机制，无需新增 |

---

## 11. 可观测性

* **`X-VMR-Route-Reason`**：`routeReason` 增加 `quota` 字段，重排真正改变队首时渲染
  `pick=quota q=<provider>:<最紧 Limit>:<score>`。**因为 `server/recorder.go` 已把响应头写进审计记录，
  这条路由理由自动进入飞行记录仪，零 schema 变更、零 `internal/report` 连带改动**——
  这是刻意选择的方案，替代"给 `audit.Record` 加字段"（后者按 CLAUDE.md 必须同步改 report 及其测试）。
* **`/status`**：新增 `quota` 段，每个 Provider 的每条 Limit 一行：
  `metric` / `window` / `used` / `amount` / `pct` / `headroom` / `role(bucket|gate)` / `window_ends_at` /
  `estimated_pct`，**以及 `used` 的四分量明细（fresh / cache_read / cache_write / out）**——
  计数器本来就按分量存，把明细露出来近乎免费，却让用户**第一天就能算出自己的换算系数**
  （高缓存命中的账号，等权口径与真实扣减能差 3～8 倍，见 §14.2），
  而不必等一个完整周期才凭经验标定 `amount`。
  外加该 Provider 的最终 `score` 与它由哪条 Limit 决定，
  以及生效中的 `model_multipliers` / `token_weights`——
  **促销倍率过期后忘记改配置是可预见的失效模式，把生效值显示出来是最便宜的防呆**。
  这是运维者回答"为什么流量都压在这个账号上"的第一现场。
* **`vmr check`**：打印各 Provider 的 Limit 配置（纯静态，不读运行态），**并打印生效时区**——
  周期边界（`every`/`since` 的锚点推算）按 `fmtutil.DisplayZone`（= `time.Local`）判定，
  而容器里未设 `TZ` 时它就是 UTC，与运维者心智模型差好几个小时且完全无声。
  把生效值显示出来是最便宜的防呆，与显示生效倍率是同一条思路。
* **`vmr status`**：`cmd/vmr/cmd_status.go` 用**类型化结构体**解析 `/status` 的 JSON，不是通用透传——
  所以 `/status` 新增 quota 段后 `vmr status` **不会自动显示**，必须同步扩展那个结构体，
  否则会出现"接口有数据但 CLI 看不见"的割裂。
* **live log**：**不加**。日志行已很密，额度是"当前状态"而非"本次请求发生了什么"，它属于 `/status`。
* **`vmr report`**：**已交付，但形态与原计划不同**——不是独立的 `section_quota.go`，而是挂在
  §2.5（账户消耗汇总）下的一张"额度与消耗对照"子表，外加 §7 的一条额度耗尽 Finding。
  原因是复核时发现独立小节会与 `vmr status` 的实时额度视图重复；真正的缺口是
  **§2.5 只有分母（配置的 `amount`）没有分子**。子表把两个数并排给出并各自标注窗口：
  **本报表窗口消耗**（从审计日志重算）与**本周期已用**（读 `vmr-quota.json` 的实时计数器）。
  两者**分属不同时间窗口，渲染层强制不做减法、不算比值**——报表窗口与计费周期不对齐是常态，
  给一个比值只会诱导过度解读。设计细节归
  `docs/VirtualModelRouter_Design_v4_Analytics.md`，本文只记录它对路由半区的两条依赖：
  §9.3 的状态文件读取契约，和 §12.1「额度公式的唯一实现」那条纪律。

---

## 12. 决策与取舍

### 12.1 设计决策

| 决策 | 选择 | 理由 | 放弃的备选 |
|---|---|---|---|
| 计量单位 | requests / tokens / cost 三档最终都要有，分两批交付（§14） | 三者各自覆盖的市场份额都不小：Coding Plan 74% 按次数、Token Plan 94% 按 Token/Credits；漏掉任一档都会让功能对相应人群失效 | 只做 tokens（对 Coding Plan 失效）；只做 requests（对 62% 的 Token Plan 失效）|
| 权重信号 | `headroom = 剩余额度比例 / 剩余时间比例` | 目标是"过期前烧完"，配速问题不是存量问题；无量纲故可跨 metric 比较 | `remaining/total`——重置日不对齐时信号反向，且多窗口/多 metric 下无定义 |
| 多窗口归并 | 取所有 Limit 的 `min` | "最紧的约束说了算"是多约束的标准语义，一个循环 | 加权平均（会让一个已触顶的闸被其他窗口稀释掉） |
| 桶 vs 闸 | 周期最长者为桶，其余为闸（闸只压制不提升） | 用满 5h 窗口没有经济价值，只有计费周期的未用额度对应真实浪费；零配置且在每个观察到的套餐上都成立 | 全部当桶（会在 5h 窗口末尾制造无意义的冲量）；全部当闸、不区分角色（会让 P1/P2 已经在跑的"周期过完前主动多用没花完的预付费额度"这个能力也一起消失——是否需要保留这条能力，取决于账号是否真的存在"过期作废的预付费额度"，不是一个能从代码本身推出答案的问题）|
| 闸的压制公式 | **P3 复核后订正为硬封顶** `min(1, raw)`，不再是渐进带 `min(1, raw/GateReserve)` | `GateReserve=0.5`（"提前几倍开始压制"）这个具体系数没有任何数据支撑，是纯拍脑袋的调参数字；闸真正必须成立的不变量只有"不能把分数抬过中性值 1.0"，硬封顶已经完整给出这条，不需要额外的缓冲带才能成立 | 保留渐进带（多一个无依据的魔数，换不来任何已证实的收益） |
| 分配策略 | 贪心取最高 score（稳定排序） | 无状态、确定性、failover 顺序自动正确、对 prefix cache 友好 | SWRR——需持久化累加器、只解决"选谁"、撒开流量反伤 cache |
| 接入点 | `Sort` 之后、`sticky` 之前的独立一步 | Sticky 已是同形状先例；`Dimension.Compare` 结构上看不到请求，且是 CLAUDE.md 明令不得扩展的接口 | 新增 `Dimension` |
| 重排粒度 | 并列梯队内，只在"挂了 Limit"的成员之间做占位重排 | 保住 `priority` 语义；让配置一个 Provider 的额度不产生隔空影响 | 全局重排；把未挂额度端点当 score=0 |
| 耗尽处理 | 只降到梯队末位，不熔断 | 计数器是估算值，按估算值执行破坏性动作 = 自制故障；硬信号是上游 402/429，health 已覆盖 | `on_exhausted: block` |
| 窗口实现 | 环形分桶，tumbling=1 桶、rolling=K 桶 | 一套结构覆盖两类；滚动误差 ≤1/K；tumbling 硬近似 rolling 误差可达 100% | 精确滑动窗口（需存每请求时间戳）；只支持 tumbling |
| 周期重置 | 惰性比较桶起点 | 无 goroutine、无漏重置、无时钟回拨、重启自动补偿 | 定时器/cron 式重置 |
| 周期表达 | `(every, since)` 二元组 | 一个机制覆盖数小时/数日/数周/数月 + 任意锚点，免掉字段膨胀 | `reset_period` + `reset_day`（装不下 5h/周，且要不断加字段） |
| 分量加权 | **必做**，两条路径：账号内比例统一走 `tokens` + `token_weights`；按模型分化走 `cost` + 费率表 | Credits 制套餐 cache read 比 fresh input 便宜 5～120 倍，等权总量记账高估 3～8 倍；而类型 D 这类非金额套餐不该被迫去反推货币单价 | 只做等权总 token（高估 3～8 倍）；只做 `cost`（逼着 token 桶套餐维护价目表）；per-provider 四个**金额**全局权重（费率实际按模型给出） |
| 折算规则的层级 | **P3 订正为按 Limit**（`limits[].token_weights`/`.model_multipliers`），不再是账号级 | P1/P2 时"同账号各窗口系数一致"只是当时唯一观测到的样本；P3 引入多窗口后遇到的真实场景是**同一账号的短窗速率闸与长周期账单桶折算方式不同**（例：闸按次数不分缓存命中、桶按 Credits 精确折算），账号级字段装不下这种差异，会逼用户为了"给一条窗口设权重"而让另一条窗口被迫共享同一套不适用的比例——这比"同一事实抄 N 遍"的重复风险更危险，因为它不是维护成本问题，是**表达力不够，装不下真实存在的配置需求**。抄 N 遍的风险仍然存在，但通过 `config.validate()` 的显式校验（`token_weights` 只在自己是 `tokens` 的 Limit 上合法）缓解——写错位置是加载期错误，不是运行期悄悄失效 | 继续账号级（P1/P2 原决策，装不下折算方式随窗口分化的账号）；折中方案——账号级默认值 + 逐 Limit 覆盖（两套语义共存，"这条 Limit 到底用了哪个值"需要读两处配置才能确定，复杂度不比"就是有点重复"更低） |
| Scope 的判定 | `models:` 一个字段同时决定"哪些模型适用"与"是否独立计数"——不写＝共享一个池；写了（`"*"` 或具体列表）＝命中的每个模型各自独立一个 bucket | 真实需求是三态，不是"共享/不共享"二元：账号总限（共享）、给每个模型各自开一份同样的限速（`"*"`）、只给几个具名模型各自开子额度（具体列表），后两种在经济含义上都是"每个模型互不影响"，唯一的区别只是成员范围——用同一个字段的"空/`*`/列表"三种写法表达，不需要另开一个 `mode:` 字段来回答"共享还是独立"，也就不会出现 `mode` 与 `models` 互相矛盾（比如 `mode: shared` 却给了 `models:` 列表）这种需要额外校验的组合 | 独立的 `mode: shared \| per_model` 字段（gemini 方案原提议，两个字段表达同一组信息，多一种"写错组合"的可能）；`models:` 列表语义为"共享池限定到这几个模型"（即最初方案落地时的实现——不是真实需求，也和"共享/独立"两种真实场景都对不上，被撤回） |
| 定价来源 | 两层：随二进制内置的**标准列表价** + `config.yaml` 里的 **per-provider 覆盖** | 今天的 `pricing.yaml` 实为"与 config.yaml 逐条对齐的部署清单"，不写就完全没有 $ 估算；标准表消除这个断崖，覆盖写在账号侧只补差异 | 继续手写全量 `pricing.yaml`（入门断崖 + 两文件漂移）；只要标准表（对国产第一方覆盖不足，见 §13） |
| 定价的落点 | 并入 `config.yaml` 的 `providers[].pricing`，解析逻辑放新叶子包 `internal/pricing`（P1 交付后修订，原方案只规划了 `cmd` 侧） | 已核对 `LoadPricing` 在 `cmd` 侧、`report` 只接收已解析的表，故不触碰 `report ↛ config` 禁令；更关键的是 `cost` 把定价放到了请求路径上，独立 `pricing.yaml` 根本不在热重载链路里。P1 已经验证并投入使用同一个模式（`internal/config` 依赖只依赖 `core` 的叶子包 `internal/quota`，在 `validate()` 阶段把配置解析成运行态值）——`internal/pricing` 是这条模式在定价上的复用，让 `metric: cost` 的费率校验和 P1 的额度校验一样发生在加载期，而不必等 `vmr report` 跑一次才发现 | 保留独立文件（需另建文件监听 + 原子换表，凭空多一个子系统）；只让 `cmd/vmr/cmd_report.go` 一侧解析、`metric: cost` 另开一条运行时校验路径（两份实现，容易漂移） |
| 折扣与促销 | 归入价格层：`discount` 或显式费率，静态按模型区分（P0-A 移除了曾经的 `date_*`/`hour_*` 时间窗功能面，见"②格式要不要向上游看齐"一节） | 写进价格层则**额度计量与 `vmr report` 的 $ 数字同时变准**；只写进 `model_multipliers` 会让报表按列表价系统性高报 | 用 `model_multipliers` 表达折扣（把价格概念塞进额度字段，且报表仍按列表价） |
| `model_multipliers` 的语义 | 收敛为**纯额度**语义，只作用于 `requests`/`tokens` | 价格分化已由价格层承担；同一件事只保留一种写法 | 让它同时承担折扣（与价格层重复计算的隐患） |
| 缺失费率的失败姿态 | `metric: cost` 下四项费率不齐 → **加载期错误** | 把缺失的 `cache_read` 当 0 会低估消耗 → 账号显得更宽裕 → 拿到更多流量 → 超支。失效方向危险，必须显式失败 | 静默按 0；按 input 费率兜底（方向安全但会白白少用套餐，且掩盖配置缺陷） |
| 存储粒度 | 存原始分量 token，折算在读取侧套 | 折算参数是可变政策；烘焙进历史数据会让每次调参都需要数据迁移 | 存折算后的数值 |
| 额度公式的唯一实现 | `base(metric)`、`model_multipliers` 缩放、估算占比三个公式下沉到叶子包 `internal/quota`（`weight.go`），路由半区与离线读者共用同一份 | 与 `internal/pricing` 的"两个消费者、一份实现"同一条先例。**但只统一公式是不够的**：喂进公式的**基数**（哪些原始计数算一次扣减）是两侧各自决定的，选错了和选对了长得一模一样，测试也照样绿。所以配套立一条硬纪律——**任何声称复现路由半区扣减量的离线计算，必须由一个差分测试钉住**：同一批合成记录分别喂给 `router.ChargeResponse` 与离线路径，断言相等。该测试只能放在同时看得见两个半区的组合根（`cmd/vmr`），因为 archtest 禁止分析半区 import 路由半区 | 两侧各自实现一遍公式（"三处改了两处"）；只共用公式、基数靠注释约定（实测会漂：曾出现用请求数当扣减基数而系统性高估、以及 usage 嗅探失败时离线计 0 而路由记了估算账的两处反向偏差） |
| rolling 与桶 | rolling 永不当桶；全 rolling 的账号没有桶 | 滚动窗口额度持续再生、不作废，use-it-or-lose-it 不成立；且其 `time_left_frac ≡ 1` 使 `raw ≤ 1`，当桶会退化成畸形闸 | 按"周期最长"一刀切（会静默产生一个给不出提升信号的假桶） |
| 计量精度 | usage 为准、估算降级、标注估算占比 | 粗决策容忍 ±30%，阶段二额度预测看板不容忍 | 全程估算；无 usage 时放弃计费 |
| 并发超冲 | 不做预扣 | 超冲上界约 0.5%，换掉一个三态子系统与其泄漏风险 | 预扣 + 对账 + 回滚 |
| Registry key | Provider name，不含密钥哈希 | 轮换密钥不应清零当周期计数；与 HealthKey 的风险方向相反 | 照抄 `HealthKey()` |
| 计量来源 | 先只做本地累计；**不预先抽接口**，写第一个官方用量适配器时再抽 | 只有一个实现时接口等于多包一层；真实适配器的形状要等写了才知道。留位置靠数据结构（按 Provider × 窗口记账）而非预留接口 | 现在就定义 `Source` 接口（投机性抽象，大概率返工） |
| 可解释性 | 编码进已有的 `X-VMR-Route-Reason` | 该头已被 recorder 记进审计；零 schema 变更 | 给 `audit.Record` 加字段（必须同步改 report 及其测试） |

---

### 12.2 已评估并否决的改进提案

**这一节存在的目的是防止同一批意见被反复提出。** 下列提案都经过核对与评估，
判定与理据记在此处；重新提出前请先看这里。

| 提案 | 判定 | 理据 |
|---|---|---|
| score 平手时加抖动/轮转因子（原子计数取模），避免刚重置的账号被"单点全量压测" | **否决** | 贪心是**每个新会话重算一次**的：A 只要计费一次，score 就低于 B，队首随即换人——平手不会造成持续独吞，边际行为接近轮转。残余的并发突发（N 个新会话在任何一个计费前同时选中 A）是 §7.4 已量化的同一现象（上界约 0.5%），且**今天没有本功能时，`priority` 打平后稳定排序本来就把全部流量给第一个端点**，本功能只会改善它。而该提案会同时废掉 §5.3 的无状态、确定性、cache 局部性三条属性——用确定的损失换不确定的收益 |
| usage 门禁 `bytes.Contains` 会被正文里的 `"usage"` 误触发 | **否决** | 门禁误开只是多做一次 JSON 解析，**不产生错误计量**——解析后找不到顶层 `usage` 键就什么都不合并。且 JSON 会把内容里的引号转义成 `\"`，`"usage"` 这个字节序列本就匹配不上，与 `modelFieldPattern` 注释里记录的同一条推理。为此加复杂度不值得 |
| usage 门禁不能作用于原始 TCP chunk，否则跨包截断会漏扫 | **本就如此，非问题** | 门禁作用于**重组后的完整事件**（`emitBlock`）与**完整响应体**（`finalizeBuffered`），从不作用于 `ingest` 拿到的原始 chunk。这一点已在 §7.2 写死，因为它正是最容易实现错的地方 |
| `every: 1mo` 不能裸调 `time.AddDate` | **成立，已处理** | Go 的 `AddDate` 会把 2 月 31 日归一化溢出（1/31 + 1mo 得到 3/3）。实现必须自带月末截断，已写入 P1 开发计划的周期数学一步 |
| P1 等权 token 会让高缓存命中的 Credits 套餐被误判耗尽 | **成立，但对策不同** | 确实存在：等权记账高估 3～8 倍，而用户在第一个周期结束前无从凭经验标定 `amount`。但后果是**被错误降权、浪费套餐**，不是"无法工作"（score 归零只排到梯队末位，不淘汰）。对策不采用"把 amount 放大 3～5 倍"这种猜数——P1 本就按分量存原始 token，**把分量明细放进 `/status` 即可让用户第一天就算出自己的换算系数**（§11、§14.2） |
| `metric: cost` 与两层定价表永久从热路径砍掉，路由侧只留 `requests`/`tokens`，$ 全部交给离线 `vmr report` | **否决** | 复杂度诊断部分成立，但分批与 opt-in 早已是本设计的既有姿态——`cost` 与两层定价表本来就排在 P2、只在配了 `metric: cost` 的账号上才触发解析、缺失费率是加载期显式报错而非静默降级（§9.1 校验清单、§12.1"缺失费率的失败姿态"）；国产厂商覆盖率低这条已在 §4.2①、§13 如实写明，不是这轮复核的新发现。永久砍掉的真实代价被低估：`token_weights` 是账号级统一四分量比例，装不下类型 E/F（Credits/金额制）账号"折算率按模型分化"这一实测特征（§4.2⑦已论证 per-provider 全局金额权重会系统性偏差），砍掉 `cost` 等于让这部分账号（Token Plan 62% 里的多数）永久停留在等权 token 记账的 3～8 倍高估里——与本功能要防止的"套餐被误判耗尽而浪费"直接冲突。P2 范围与门禁不变 |
| 给 `quota.Registry` 加内存级 per-provider `in_flight` 原子计数器，`score' = score - α × in_flight`，压制并发新会话瞬间挤爆同一梯队队首端点 | **搁置，留给 P3 用数据判断；即使做，落点未必在 quota** | 问题本身成立，但与 quota 无关——它是"同梯队打平时新请求集中冲向队首"的一个特例：quota 出现之前，`priority` 打平时的稳定排序早就有同样效果（§5.3 已引用同一事实否决过另一个抖动提案），quota 只是继承了这个既有行为，不是它的制造者。给 quota 专门加一个 `α` 是把一个通用路由问题焊死在这一个维度上——换个打平维度（未来若加新 Dimension）还得再修一次；且 `α` 是一个没有实测依据的新魔数，与本设计"每个魔数都要有依据"的自设标准冲突。实现成本也不是"几行"：需要在 `tryOne` 的失败循环里精确控制 acquire/release 时机（只在真正发起 attempt 时 +1、attempt 结束时 -1，而非排序阶段），量级接近 `limiter.go` 的 `AcquireSlot`。**处置**：P1/P2 不做；P3 若真实 429 数据显示同梯队并发确有代价，优先在候选选择层（`strategy` 或 `Serve` 的排序步骤）做一个与 quota 正交的通用打散机制，而不是塞进 headroom 分数——这样纯 `priority` 打平的场景也一并受益，quota 场景不需要单独处理 |
| **硬门控（用淘汰代替重排）+ 全部超额时返回 429 快速熔断** | **否决** | ① 本地额度计数本质是估算值（降级估算 ±30%、多实例部署状态不互通、厂商口径偏差、新上线未校准），用估算值执行不可逆的拒绝服务动作是自制故障；② 上游真正的 429/402 信号已由 `internal/health` 状态机（长冷却 + 半开单飞 + failover）精准覆盖；③ 软重排在全部超额时挑超额程度最轻的端点做最后尝试，若成功则免除故障，若失败则自然 failover；④ 跨周期惰性清零（`resetIfStaleLocked`）在时钟跨界时 O(1) 瞬间自愈，无需维护"从候选集移除后如何恢复"的状态机 |
| **新增 `mode: shared \| per_model` 枚举字段** | **否决** | 需求是三态（共享、全模型独立、限定模型独立），由 `models:` 字段（不写 / `["*"]` / `[name,...]`）单一字段直接表达更自然。引入独立的 `mode` 会产生 `mode: shared` 配合具体 `models` 列表等矛盾组合，增加不必要的配置面与校验复杂度 |
| **支持 5 种显式时间格式（含空格分隔、纯时间、分钟级完整时间）** | **部分采纳** | 完整日期时间由 RFC3339 覆盖；纯日期由 `YYYY-MM-DD` 覆盖；空格分隔仅是排版偏好，无实质收益。**采纳纯时间 `hh:mm[:ss]` 格式**（仅限 `min`/`h` 周期），消除了短窗 RPM 速率闸为了对齐分钟而强凑假日期的啰嗦；严禁在 `d`/`w`/`mo` 上使用纯时间以防语义模糊 |
| **Scope 模型名强制校验"是否在当前 Provider 路由表中"** | **否决（撤回）** | 路由表只代表"当前恰好路由了哪些模型"，而 Provider 实际能提供的模型是更宽的集合。强制校验会把预配置或临时摘除路由的模型误判为 typo；且系统没有全局权威的模型支持清单作为基准 |
| **未配 `since` 时的热重载锚点漂移问题**（B2） | **已修复（2026-08）** | 最初裁为"非问题（撤回）"，理由只覆盖了**相位对齐**——"不显式配 `since` 就是不在乎精确相位"。但真正的后果不是相位漂移，是数据丢失：`LimitKey` 不含 `since`，桶 key 稳定；缺省锚点取原始 `now` 时，每次 `config.Load`（启动 + 每次热重载）都把锚点挪到加载时刻，`PeriodStart` 随之改变，`resetIfStaleLocked` 就地清零该账号的累计消耗。这是一个真 bug，不是相位选择。修法：`DefaultSince(now, unit)` 对齐到固定日历边界（min/h/d→当日午夜、w→周一 0 点、mo→月初），**同一自然日内的任意热重载对任意 `every` N 都解析出同一锚点、计数存活**。此前"去除五种时间单位的日历对齐分支复杂度"这个理由被推翻——那点复杂度（`period.go` 已有全部日历原语，约 10 行的一个 switch）换的是子系统核心状态不被静默摧毁。残余收窄为：`every: Nh` 且 N∤24 或 `every: Nmin` 且 N∤1440，**且**热重载/重启跨过自然日——此时至多一次相移重置；显式写 `since` 可钉死。回归测试见 `internal/quota/period_test.go` / `quota_test.go`（`TestDefaultSince_SurvivesReload` / `TestRegistry_DefaultSinceReloadDoesNotReset`）。 |
| **多 Limit 扁平打分 `Score = min(Score1, ...)`（不区分桶/闸）** | **否决** | 计费周期的未用额度是真实预付费（use-it-or-lose-it 成立，欠用 `raw > 1` 应主动加分争取流量）；而短窗速率闸（如 1min RPM）是防超频阀门，用不满无任何经济收益，若允许闸提升分数会导致短窗恰好空闲的账号虚高优先级。采纳桶/闸模型：桶可提升，闸 `min(1, raw)` 硬封顶 |

其余反复出现过的路线选择，理据已在 §12.1 的"放弃的备选"列：SWRR、预先定义 `Source` 接口、
额度耗尽硬熔断、per-provider 的四个金额全局权重、预扣对账。

本节记录了历次方案迭代（含 2026-08 独立 ROI 复核以及多轮多 Limit 重构方案讨论）中被否决、搁置或修正的所有技术提案及其裁决理据。重新提出前请先对照此表。

---

## 13. 范围边界与已知限制

`vmr` 的额度计数**本质是一个估算值**。下表把"决定不做的事"和"做了之后剩下的偏差"合并成一张表——
两者本来就是同一个问题的两面：每一条限制都对应一个明确的范围决定，每一个范围决定都留下可预期的后果。

| 事项 | 决定 | 理由 | 后果与缓解 |
|---|---|---|---|
| 厂商计数单位 ≠ vmr 可观测单位 | 需用户实测校准 | "一次提问"这类单位在网络层根本不可见，会展开成十几到二十几次调用 | `amount` 必须按 vmr 口径配置；官方用量 API 是根治手段 |
| 绕过 vmr 的直连流量 | 无法计入 | 结构性不可能 | 本地计数低估；将来接官方用量 API 可消解 |
| 后台探针 `probe` 的消耗 | 不计入 | 绕开 `forwardSuccess`，probe 自己发请求，报文小、频率低 | 低估幅度可忽略；接官方用量 API 同样可消解 |
| `vmr replay` 的消耗 | **已计入**（2026-08-11 交付） | `internal/replay` 是独立一次性 CLI 进程，不经过 `Router`/`forwardSuccess`，但已接入同一个 `quota.Registry` 文件与 `router.ChargeResponse` 计费管线，语义与实时流量一致 | 消解了 2026-08 ROI 复核（见 §12.2）指出的静默漂移问题；仍不覆盖多实例并发写同一状态文件的场景（§13"多实例共享计数"条目原样成立），细节见文末「现状与后续计划」一节 |
| 多实例共享计数 | 不做 | 单二进制、零 DB 是产品前提 | 多实例各自低估；单实例部署，或接官方用量 API |
| 被替换的 `limitKey` 残留在状态文件里 | 暂不清理 | `Registry` 整份读写、从不删 key（§9.3）；路由半区只读当前 key，自身无害 | 改过 `every`/`metric` 的账号会永久留一个孤儿桶，且无时间戳可判断其新旧；离线读者会持续看到"有旧数据但当前 key 无数据"。清理落点在路由半区的 `Load`→首次 `Flush` 之间，随 P3 一并做（§15.3⑥） |
| 从审计日志离线复算扣减量 | 接受偏差，`requests` 档除外 | 审计日志记的是"发生了什么"，不是"扣了多少"——后者是路由半区当时的记账 | `requests` 档可恒等复现（§7.1）；`tokens` 档过去有两处固有偏差，均已消解（B0 批次，2026-08-14）：`model_multipliers` 缩放改为精确乘法、不取整后，"逐请求取整 vs 汇总取整"不再成立；`usage` 嗅探失败时报表侧现在复现同一条退化估算公式（`internal/chatmsg/tokenest.go` 的 `EstimateDegradedTokens`），不再是"路由记了字节估算而离线计 0"。**仍然存在**的是一处不同性质的残差：路由半区数的是**上游**字节（含 opaque 模式），离线读者只有**审计记录里的客户端侧**响应体（经过 model 改写、响应归一化、`recorderBodyCap` 截断之后）——两者在没有归一化改动字节数时完全一致，改动时按差值偏离，这正是 `WindowEstimatedPct` 把复算列标成"估算"而不是"权威值"的原因；`cost` 档另有"复算时的费率可能不同于记账时"。差分测试保证的是公式与基数一致，不是消除这些偏差 |
| 时段倍率（`requests`/`tokens` 档） | 不做 | 需新配置面，且这类规则只在个别厂商出现、自身还在频繁调整 | 对这类账号系统性低估；改用 `cost` 档（价格覆盖的 `hour_*` 直接支持）或调低 `amount` |
| 标准表对国产第一方覆盖不全 | 接受，靠补充表 / 账号覆盖补 | 实测上游数据：部分厂商无缓存字段、部分条目无价格字段、部分厂商零收录；全表 `cache_read` 覆盖率仅 23%、`cache_write` 仅 8%（明细见市场参考文档） | 这些账号要用 `metric: cost` 必须补费率；因四项不齐即加载期报错，缺失不会被静默吞掉；补充表可回贡以逐步消解 |
| 标准列表价会过期 | 接受 | 随二进制内置的是快照 | 表内带生成时间戳，报表免责声明与 `vmr check` 一并显示；`tools/` 下有刷新脚本，用户也可自备标准表 |
| `cost` 档分量比例与价目表不同 | 接受 | 按模型的绝对费率已能覆盖绝大多数情形 | 改用 `tokens` + `token_weights`（换取精确比例，损失按模型粒度） |
| 无 config.yaml 时跑 `vmr report` | 接受降级 | 账号覆盖存在 config.yaml 里 | 只拿到标准列表价，$ 数字不含该账号折扣；需在用户文档写明 |
| 降级估算拆不出缓存分量 | 保守按无缓存折算 | `tokenutil.Estimate` 只给总量 | 高估消耗（对闸安全、对桶略少用）；计入 `estimated` 占比，`/status` 可见 |
| 额度耗尽硬熔断 | 不做 | 按估算值执行破坏性动作 = 自制故障 | 硬信号是上游 402/429，既有 health 状态机已覆盖 |
| 精确滑动窗口 | 不做 | 需存每请求时间戳，内存与持久化高一个量级 | 分桶误差 ≤ 1/K（12 桶即 ≤ 8.3%） |
| 周期边界精确复刻厂商账单 | 不做 | 厂商账单时区与小时精度不公开 | 本地时区 + 日级锚点的近似，需在用户文档写明 |
| 单个长会话独吞一个套餐 | 接受 | sticky 优先级高于额度（§6.1 不变量 2） | 阶段二看板负责让它可见 |
| 按下游用户/业务线的实时配额 | 不做 | 需在请求路径引入下游身份维度的配额状态，比 Provider 级重得多 | 事后可见性走 `ClientKeyTag`（已逐请求进审计，`report`/`story` 已能分组） |
| 并发 / RPM 上限 | 不在本设计范围 | `headroom` 只看额度桶与限额闸 | 撞并发上限由 health 状态机兜底——是另一个问题，不是本设计的缺陷 |
| 并发新会话的计量超冲 | 不做预扣 | 超冲上界约 0.5%（§7.4） | 可忽略；换掉了一个三态子系统与其泄漏风险 |

---

## 14. 分批实施

### 14.1 自评：这份设计是不是过重了

是。十四项机制（三种 metric、多窗口 + `min()` 归并、桶/闸角色、环形分桶、`(every, since)` 周期数学、`model_multipliers`、`token_weights`、Scope、标准定价表 + 生成脚本、per-provider 价格覆盖 + ID 映射、`Source` 抽象、持久化 + 惰性重置、梯队占位重排、usage 嗅探 + 降级）作为"阶段一"确实过重。

**永久砍掉一项**：`Source` 接口——投机性抽象，没有第二个实现之前接口只是把一个函数调用包了一层，等写第一个用量适配器时再抽（形状还会更贴合真实数据）。**Scope 曾降级为"有真实案例才做"**，P3 真实案例出现后已交付——降级判定没有错，门槛也确实被满足了。

剩下的十二项不靠"裁剪"解决，靠**分批**：它们不是同一个问题的十二个面，而是**一条精度阶梯**上的十二级——第一级就已经能解决主要问题。

### 14.2 分批的依据：三条，每条都决定了切口位置

**① Headroom 只需要"比例对"，不需要"绝对单位对"。**

这是让分批成立的关键观察。`headroom = 剩余额度比例 / 剩余时间比例` 是**同一个账号内部的比值**，
所以**计量单位上的常数倍偏差会自动约掉**：若 `used` 被高估 3 倍，而 `amount` 也是按同一口径
标定的（"上次跑满时 vmr 数到 5 亿"），`used / amount` 依然正确。

于是：`token_weights`、`model_multipliers`、`metric: cost`、整套两层定价——
**它们全都是"把绝对单位做准"的特性，而绝对单位是给看板和成本报表用的**
（"我这个月烧到 80% 了没有"），**不是路由决策需要的**。
第一批用等权总 token，路由决策就已经正确。

**抵消的前提是 `amount` 按 vmr 的计数口径标定**，而不是照抄厂商标称值（§2.4 已就单位换算说过同一件事）。
对高缓存命中的 Credits 套餐，这个系数可达 3～8 倍，而用户在第一个周期结束前无从凭经验取得它——
所以 P1 必须把 `used` 的**四分量明细**露在 `/status`（§11），让用户第一天就能算出来。
否则该账号会被错误降权、浪费套餐，正是本功能要防的事。

诚实的边界：这个偏差也不是严格恒定的——缓存命中率会随会话推进从低走高，
等效倍数在 60%～95% 命中率区间里大约漂 ±30%，与我们本来就接受的估算误差同量级。
相对于"完全没有额度感知"，这是二阶问题。

**② 短窗口（5h/周）防的是 429，而 429 今天已经有人管。**

`health` 状态机（冷却 + 半开单飞 + failover）本来就兜住了限流。
所以闸不是"能用"的前提，是"更好"。第一批只做**一条 Limit = 计费周期桶**，
短窗口继续交给 health——**这不是退化，因为今天本来就是这样**。

这一刀砍掉：多条 Limit、`min()` 归并、桶/闸角色判定、rolling 窗口、环形分桶。是最大的一刀。

而且**"要不要做闸"这个问题，第一批上线后能用真实数据回答**：看 429 的实际频率与它造成的尾延迟。
这才是分批最实质的收益——**不是少写代码，而是让后面的决策有事实依据，而不是继续拍脑袋**。

**③ 数据契约先立，实现后补——这是分批可行的技术前提。**

`Counters` 从第一批就**按四分量存原始 token**（存储成本为零），
所以第二批加权重、加 `cost` 时**历史计数不作废、不需要迁移**。
同理 `(every, since)` 的周期表达第一批就定死，第三批加 `rolling: true` 只是加一个开关，不改已有语义。

**如果第一批存的是折算后的标量，第二批就要做数据迁移，分批的成本会陡增。**
这个前提不是为分批临时加的——它本来就是"事实存储侧、口径读取侧"那条设计决定（§9.2）的直接结果。

> **每一批都必须是垂直切片**：端到端跑通 `config → 计量 → 决策 → 可观测`，
> 而不是横向做完"所有 config"再做"所有计量"。否则没有一批是可上线的，
> 也就拿不到运行数据，分批就退化成"分期交付一次大爆炸"。
>
> **每一批的验收标准是"严格优于上一批且无回归"，不是"这个领域做完了"。**
> 今天 = 零额度感知；第一批 = 计费周期均衡，已经解决主要浪费。

### 14.3 四个批次

| 批次 | 解决什么 | 解锁的市场覆盖 | 新增机制 | 状态 |
|---|---|---|---|---|
| **P1 单桶均衡** | 多套餐按剩余进度自动分流 | 81 个在售套餐中只有 5 个既无请求限额也无 token 限额 → **约 94% 能配出一条计费周期桶** | 少 | ✅ **已交付**（2026-08-07） |
| **P2 计量准确** | 百分比可信；Credits 制套餐精确记账 | Token Plan 的 Credits/金额制账号（约占在售的 1/3） | 中（范围已按 P1 实测经验修订，见下方 P2 一节） | ✅ **已交付**（2026-08-07，2026-08-09 交付后复核修完七项问题——见「现状与后续计划」一节） |
| **P3 多约束** | 少撞短窗限额造成的 429 抖动 | 多窗口套餐（类型 A / B / D / F） | 中 | ✅ **已交付**（2026-08-22，rolling 窗口除外——见下方 P3 一节） |
| **P4 校准与看板** | 消除本地计数的系统性偏差；成本归因 | — | 小而深 | 未排期 |

当前逐项落地状态（哪些机制已实现、哪些还没有、已知缺口与下一步建议）见文末「现状与后续计划」一节。

---

四批的逐项范围与验收标准是分批时的规划记录；**当前实际落地状态以 §15 为准**，此处只留每批的切口与关键取舍：

| 批 | 切口（新增机制） | 关键取舍 |
|---|---|---|
| **P1 单桶均衡** | `providers[].quota.limits` 恰好一条、`metric: requests|tokens`（等权）、`(every, since)`、仅 tumbling + 惰性重置、`headroom` + 贪心 + 梯队占位重排 + sticky 覆盖 | `Counters` 从第一批就按四分量原始值存（为 P2 预留，存储成本为零）。验收：未配 `quota:` 的既有配置行为逐字节一致（`"quota"` 键整体不出现）；§1.1 的重置日错开反例端到端修复 |
| **P2 计量准确** | `token_weights`、`model_multipliers`（纯额度语义）、`internal/pricing` 叶子包 + `go:embed` 双表 + 生成脚本、`pricing.supplement`、`providers[].pricing`、`metric: cost`（四项费率不齐 = 加载期错误） | 排在 P1 之后是因为这批全是"绝对单位"精度问题、错误方向危险（低估→超支），适合在 P1 计量数据可与厂商控制台对照之后再做。破坏性变更：独立 `pricing.yaml` 与 `vmr report -pricing` 取消 |
| **P3 多约束** | 一个 provider 多条 Limit + `min()` 归并、桶/闸角色、Scope（`models:`）。**不含 rolling + 环形分桶** | 触发方式与原计划不同——不是"429 数据证明值得"，而是一个具体 provider 的复合窗口需求直接触发的（诚实偏离，两者都是合法立项依据）。立项时复核后：桶/闸显式分层**保留**（`min(raw)` 会让宽裕的闸自己提升流量，`TestScoreForLimits_GateNeverBoosts` 钉住）；`GateReserve` 魔数拿掉，闸公式简化为 `min(1, raw)`；rolling 明确砍出本批（`core.Limit`/`config.LimitConfig` 已预留 `rolling` 字段，届时只加窗口实现、不触碰配置形状） |
| **P4 校准与看板** | `vmr report` 的额度燃尽看板 + 按 `ClientKeyTag` 归因、官方用量 API 适配器（写这个适配器时才抽 `Source` 接口）、标准表刷新脚本纳入定期流程 | 直接消解 §13 里最难受的三条限制（单位换算、绕过 vmr 的流量、时段倍率） |

### 14.4 每批内部的落地顺序：先只观测，后再决策

每一批内部都按同一个模式切，**这是本方案最主要的降险手段**：

```
配置解析 + 校验 + vmr check 打印     → 行为零变化
计量接线 + 落盘 + /status            → 路由决策仍零变化   ←── 可在生产停留数天
决策接入（排序/加权/归并）            → 行为开始改变
```

中间那一档是关键：**可以先在生产只跑计量，用 `/status` 对着厂商控制台校准几天**，
确认数字可信之后再开最后一步。整套机制建立在一个估算出来的计数器上，
"先只观测、后再决策"比任何测试都更能暴露单位换算这类问题（见"厂商计数单位 ≠ vmr 可观测单位"一节）。

决策接入一律落在 `internal/router/quota.go` **新文件**，不进 `router.go`——
archtest 有 700 行预算，当前 561 行。

### 14.5 测试

关键覆盖点（实现在 `internal/quota/*_test.go`、`cmd/vmr/quota_parity_test.go` 等）：

- **周期数学**（纯函数，最易错）：`since` 落在 31 日时对短月的截断（1/31 → 2/28）、跨年、`every: 2w`/`3d` 多倍周期、DST 切换日、`PeriodStart`/`PeriodEnd` 自洽性。
- **Headroom**：§1.1 的"三套餐重置日错开"场景做成断言（整个设计的立论依据）；`ε`/`HeadroomCap` 的 clamp。
- **梯队 & 不变量**：`priority` 分层时不跨层重排、未挂 Limit 的成员位置不变；sticky 命中时 quota 重排被覆盖；耗尽端点仍在候选集里且不产生 health 冷却。
- **惰性重置**：跨周期的计费与读取都触发清零；重启后从文件加载并补偿重置。
- **P2 折算**：`token_weights` 全 1.0 时 `base(tokens)` 逐字节等于 `In + Out`；`cost` + `discount: 0.6` 恰为纯 `cost` 的 0.6 倍；真实费率夹具下 `cost` 与等权总 token 比值落在 3～8 倍区间；四项费率不齐的 `metric: cost` 在**加载期**报错（`vmr check`/`start`/热重载三处共用同一个 `validate()`）；生成脚本不得把缺失分量补 0、`standard_price_curated` 手工行重新生成后仍在。
- **P3 桶/闸**：最长 tumbling 为桶、全 rolling 的账号没有桶；闸只压制不提升（`TestScoreForLimits_GateNeverBoosts`）；单 Limit 严格退化为 P1/P2 行为；`models:` 过滤只对匹配模型计费、`model_multipliers` 按**上游**模型名生效。
- **`-race`**：`Registry` 的并发计费/读取/flush。
- **archtest**：`internal/report` 仍不 import `config`；`router` → `quota`/`pricing` 不成环。
- **loadtest**：`requests` 计量开销应完全不可测；token 嗅探每事件一次 `bytes.Contains`，若 `stream_normal` p95 出现可测量变化说明门禁写错了。

---

## 15. 现状与后续计划

> 本节回答一个问题：**§1–§14 描述的设计，到今天实际落地了多少、还差什么、后面打算怎么办。**
> 按状态更新，不按变更历史记录——发现一处偏离就地订正到相应章节（如 §9.2 的折算时机、
> §9.1 的 `pricing.Complete` 完整性校验），本节只做汇总性的现状快照。

### 15.1 一句话结论

**P1、P2、P3 已全部交付并投入使用（rolling 窗口除外）；P4 未启动**。按 §14.1 自评的十四项终态机制计，已落地十二项、永久砍掉一项（`Source` 抽象）、剩一项待 P4（环形分桶/rolling）。另有一件终态清单之外的事已交付：**`vmr-quota.json` 从进程私有状态升级成对外可读的格式**——`vmr report` 现在把它的实时计数器与从审计日志重算的窗口消耗并排展示（§11），带出两条契约（§9.3 的读取前提、§12.1 的"额度公式唯一实现 + 差分测试"纪律），P3 后都已按多 Limit 重新验证过。真正影响可用性的剩余缺口是**内置标准价目表的四分量完整率不高**（§15.3①）。

### 15.2 终态十四项机制逐项现状

对应 §14.1 自评时列出的同一份清单：

| # | 机制 | 状态 | 说明 |
|---|---|---|---|
| 1 | 三种 metric（requests / tokens / cost） | ✅ 全部落地 | `core.QuotaMetric`；`router/quota.go` 的 `ChargeResponse` |
| 2 | 多窗口 + `min()` 归并 | ✅ P3 | `config.QuotaConfig.Limits` 不再限制为一条；`quota.ScoreForLimits` 按桶/闸规则归并 |
| 3 | 桶 / 闸角色 | ✅ P3 | `quota.BucketIndex` + `ScoreForLimits`（最长周期为桶，闸硬封顶 `min(1,raw)`——已去掉 `GateReserve` 魔数），单 Limit 时严格退化为 P1/P2 行为 |
| 4 | 环形分桶（rolling） | ⬜ P4（本批范围内明确排除，非顺延） | `rolling: true` 仍是加载期"计划中"错误；见 §14.3 P3 一节"未随 P3 交付"的说明 |
| 5 | `(every, since)` 周期数学 | ✅ | 含月末截断、跨年、DST；P3 新增 `min` 单位 |
| 6 | `model_multipliers` | ✅ P2，**P3 起改为按 Limit 配置** | 每条 Limit 各自的字段，**计费时**套用，精确相乘、不取整（§9.2）；见 §12.1「折算规则的层级」的订正 |
| 7 | `token_weights` | ✅ P2，**P3 起改为按 Limit 配置** | 每条 Limit 各自的字段，**读取时**套用，缺省全 1.0；理由同上 |
| 8 | Scope（`models:`） | ✅ P3 | 出现真实案例后按 §14.1 的门槛交付；三态语义（不写＝共享、`"*"`＝按模型独立不限成员、具体列表＝按模型独立限定成员），`quota.LimitKey` 按"实际计费的模型"生成 key，不是按 Limit 声明的列表 |
| 9 | 标准定价表 + 生成脚本 | ✅ P2 | `internal/pricing` 的 `go:embed` 双表 + `tools/gen_standard_pricing` |
| 10 | per-provider 价格覆盖 + ID 映射 | ✅ P2（时间窗子功能面已于 P0-A 移除） | `providers[].pricing` 的 `map`/`overrides`（`discount`/显式费率/`"*"` 通配，静态按模型区分，无时间维度） |
| 11 | `Source` 抽象（官方用量 API） | ⛔ 永久砍掉 | §14.1 已定案：写第一个适配器时再抽（P4） |
| 12 | 持久化 + 惰性重置 | ✅ P1 | `vmr-quota.json`（0600），5s flusher + 退出前强制 flush。已成为**对外可读**的格式：`quota.LoadFile`/`Bucket` 供离线消费者只读加载（不构造 `Registry`、不加锁、不写盘），读取契约见 §9.3 |
| 13 | 梯队占位重排 | ✅ P1，P3 扩展到 Scope | `reorderByQuota`，只重排"挂了至少一条适用 Limit"的成员——一条 Limit 的 `models:` 没覆盖到某端点的模型，对该端点等同于没配这条 Limit |
| 14 | usage 嗅探 + 降级估算 | ✅ P1 | 事件级门禁 + 字节估算兜底 |

十四项里：**已落地 12 项（#1、2、3、5、6、7、8、9、10、12、13、14）、P4 待做 1 项
（#4，本批范围内明确排除的环形分桶）、永久砍掉 1 项（#11）。**

### 15.3 已知缺口与后续建议

按"是否影响可用性"排序，不按批次；每条都说明为什么现在不做，不是简单地标"待办"。

**① 内置标准表的四分量完整率不高（影响可用性）。**
`metric: cost` 的加载期门槛是"四项费率全部有值"（缺失当 0 会低估消耗，是最危险的失效方向），
所以多数模型光靠内置表配不出 `metric: cost`，必须写 `providers[].pricing.overrides` 的显式
四分量费率。上游数据本身就是这样（西方主流厂商覆盖完整，国产第一方厂商明显偏弱——部分缺缓存
字段、部分整条缺价格），§4.2①、§13 已如实写明"标准表是消除入门断崖的基线，不是 `cost` 的
充分数据源"，不是实施偏差。`standard_price_curated.yaml` 已陆续补入经过官方定价页核对的国产
第一方厂商条目，但多数仍因 `cache_write`（部分还缺 `cache_read`）未公开而不满足四项齐全——
**逐步补齐更多国产第一方厂商的四分量费率是这项缺口唯一的实质解法，属于持续的数据维护工作，
不是一次性的代码任务，因此不排期，靠社区与项目持续贡献补充表/`standard_price_curated.yaml` 推进。**

**② 报表的"溯源可见"只做到聚合级，没做到逐行级。** `vmr report` §2 只给一个汇总（标准表生成日期 + 补充表路径 + override 条数），单行 $ 数字看不出它走的是标准表/补充表/账号覆盖哪一层。真要做需要在 `pricing.Resolve` 返回值里带上来源标记并一路穿到 `report` 的行结构——不是小改动。低频需求，等 P4 做额度看板（`section_quota.go`）时报表本就要改一轮，届时一并考虑。

**③ 生成脚本的 canonical key 做了归一化，不是上游 JSON 顶层 key 原文。**归一化保留（org 前缀不是模型身份——同一模型在不同卖主处前缀漂移，裸名归一是去重、刷新稳定、厂商优先级比较的前提）。曾经的后果是**桥只修了一半**：表侧归一、请求侧不归一，带 org 前缀的上游名（openrouter 的 `meta-llama/...`、together 的 `google/gemma-...`、fireworks 的 `accounts/fireworks/models/...`）四步全落空、静默 Unpriced，`metric: cost` 直接加载期报错。已修复：新增 `pricing.ModelBasename` 作为“裸名”的唯一权威定义（生成器建 key、解析器降级、歧义报告分组三处同源），`resolveCanonicalKey` 四步落空后把请求名掐成裸名递归重跑——带前缀请求与裸名请求答案逐字节一致，原本命中的名字结果不变。配套：`ParseTable` 强制 key 至多一段 `/`（三段 key 会劈出隐形命名空间，手写补充表直接加载期报错）。已知残余：某网关自造 id 掐掉前缀后恰与另一模型裸名同名时，重试会命中那家的价——与第 ④ 步 substring 匹配同型的极小概率误匹配，可用 `map`/别名先钉（优先级更高）。

**④ `ParseTable` 不校验标准表/补充表内费率的正负与有限性**（账号覆盖那层已有 `positiveFinite`/`nonNegativeFinite` 校验）——因为标准表是脚本产出 + `go:embed` 的受控数据、补充表是用户自备的完整表，要出现负数/NaN 都得先绕过生成脚本或手工构造整表。ROI 不足，登记备查。

**已交付的相关项**：`vmr replay` 的消耗现在计入同一份 `vmr-quota.json`（`internal/replay.Run` 复用导出的 `router.ChargeResponse` 计费管线，`-dry-run` 不触碰状态文件；仍不覆盖多实例并发写同一文件——那本是 §13"多实例共享计数"这条已接受限制）；被替换的孤儿 `limitKey` 由 `quota.Registry.Prune` 在 Snapshot 安装与热重载时按配置白名单自动修剪；P3 放开多窗口后 `internal/report` 的 `ProviderQuotaRef` 已从单 Limit 改为按 `(provider, LimitKey)` 聚合、§2.5 额度对照表一个 Limit 一行。

**P4 里最接近可立刻做的一项**：标准表刷新纳入定期流程——脚本已有，缺的只是把"什么时候跑、谁跑"定下来。
