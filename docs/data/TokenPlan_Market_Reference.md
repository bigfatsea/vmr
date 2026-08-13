<!-- Ver 2026-08-06 23:00, by Opus 5 -->

# Token-Plan / Coding-Plan 市场事实参考（快照：2026-07-26）

**用途**：`vmr` 额度感知路由（见 `docs/VirtualModelRouter_Design_v4_Quota.md`）的事实依据与测试夹具。
设计决策、配置样例、单元测试的期望值都应引用本文，而不是每次重新联网拉取。

**数据来源**：<https://vibecoding.dreamfree.space/>（站点自述覆盖 31 个平台的 Coding Plan / Token Plan 对比）。
原始数据已随仓库留档：

- `docs/data/tokenplan-market-plans-2026-07-26.json` —— 109 条套餐记录（含 28 条已下线），本文的全部数字来自它。
- `docs/data/tokenplan-market-payg-2026-07-26.json` —— 配套的按量计费价格表。

**快照日期 2026-07-26**（站点自述更新日期）。套餐条款变动频繁，超过三个月后引用前请重新核对。

---

## 1. 读这份数据前必须知道的三件事

1. **`5h/周/月 Token` 三列不是厂商公布的额度，是站点的等效折算值。**
   站点对 Credits 制 / 金额制套餐统一按 **"输入:输出 = 99:1、输入缓存命中率 90%"** 折算成等效 Token 上限，
   目的是让不同计量单位的套餐可以横向比较。**不能直接抄进 `limits.amount`**，
   但它恰恰证明了这些套餐的真实计费机制是**按分量折算**的（见 §3）。单位为**百万 Token（M）**。
2. **"无限制"和"未公开"是两回事。** 前者是厂商明确不限该维度（如字节·方舟明确"没有请求次数限制"），
   后者是厂商没公布。统计时必须区分，否则会把 30 个"无限制"误算成"有限额"。
3. **厂商的计数单位未必等于 vmr 能观测到的单位。** 阶跃星辰按 prompt 计数，
   站点给出的换算是 **1 prompt ≈ 15 次请求**。配置 `amount` 时必须换算到 vmr 在网络层看到的口径。

---

## 2. 计量单位的分布（在售 81 个套餐）

| 分类 | 数量 | 占比 |
|---|---|---|
| 有 Token 维度限额 | 62 | 77% |
| 有数值型请求数限额 | 32 | 40% |
| **仅 Token 限额**（请求数无限制/未公开） | 44 | 54% |
| **仅请求数限额** | 14 | 17% |
| **两者并存** | 18 | 22% |
| 两者都无/都未公开 | 5 | 6% |

按套餐类型拆开，结论完全相反：

| 类型 | 数量 | 有请求限额 | 有 Token 限额 |
|---|---|---|---|
| **Coding Plan** | 31（38%） | 23（**74%**） | 15（48%） |
| **Token Plan** | 50（**62%**） | 9（18%） | 47（**94%**） |

**结论：**

- **"国内套餐以 requests 计量为主"是错的。** 它只在 Coding Plan 这个子集内成立，
  而 Coding Plan 只占在售套餐的 38%；数量更多的 Token Plan 有 94% 按 Token 计量。
  整体看 **Token 才是主流口径（77% vs 40%）**。
- **22% 的套餐同时受请求数和 Token 两种限额约束**，所以"选一种 metric"这个问法本身就是错的——
  同一个账号必须能同时挂多种 metric 的 Limit。
- 市场趋势是 **Coding Plan → Token Plan**（站点首页明确提示"Coding Plan 正在收缩"），
  这个方向会让 Token/Credits 计量的占比继续上升。

---

## 3. 最关键的发现：Credits 制套餐按**分量**折算，且比例极端

多家厂商的额度单位是 Credits（积分），而 Credits 的扣减是**按 input / cache-hit / output 分别折算**的，
比例差距不是百分之几，而是数倍到上百倍：

| 厂商 | 折算口径（原文） | fresh input : cache read : output |
|---|---|---|
| **小米·MiMo** | 缓存 2.5 Credits/Token、输入 300 Credits/Token、输出 600 Credits/Token | 300 : 2.5 : 600 → **cache 比 input 便宜 120 倍** |
| **阿里·百炼** | 输入 5K Token/Credit、输入命中 25K Token/Credit、输出 0.83K Token/Credit | cache 比 input 便宜 **5 倍**，output 比 input 贵 **6 倍** |
| **DeepSeek Pro** | 缓存命中输入 ¥0.025/M、未命中 ¥3/M、输出 ¥6/M | cache 比 input 便宜 **120 倍** |
| **DeepSeek Flash** | 缓存命中输入 ¥0.02/M、未命中 ¥1/M、输出 ¥2/M | cache 比 input 便宜 **50 倍** |
| **字节·方舟** | 积分制 AFP，1 AFP = 1111 Token，按最优模型 **9 倍系数** | 模型系数 9x |
| **OpenCode** | 美元 Credits：基础 $12 / 周 $30 / 月 $60，实际按 token 消耗计费 | 三窗口 + 金额计量 |

**量化影响**：按站点统一采用的"输入输出 99:1、缓存命中率 90%"口径，
若把额度当作**等权总 Token**来记账，相对真实的分量折算会**高估 3～8 倍**：

- 小米·MiMo：分量折算 3792.75 Credits/100 Token，等权按 input 价算是 30000 Credits/100 Token → **高估 7.9 倍**
- 阿里·百炼：分量折算 0.006749 Credit/100 Token，等权按 input 价算是 0.02 Credit/100 Token → **高估 3.0 倍**

对 Agent 工作流（长上下文、高缓存命中率）而言，等权记账会让一个刚用掉 15% 的账号显示成"已耗尽"。
**这直接推翻了"token 只记总量、砍掉分量加权"的简化方案。**

---

## 4. 窗口结构

- **5h / 周 / 月三层并存是常态**：字节·方舟明确"有 5 小时、周、月的 Token 限制"；
  Codex、Claude、Kimi、智谱、优云智算等均有 5h + 周 + 月三列数据。
- 也有明确**只有单层**的：小米·MiMo "无 5 小时限额，支持集中消耗"；
  阶跃星辰"官方只有周限量无月限量"；阿里·百炼 Token Plan 团队版无小时/周限。
- **"数周/数月"型周期真实存在**：阶跃星辰只有周限量，站点按"1 月 = 4 周"折算月度值。

---

## 5. 在售套餐全表（81 条，快照 2026-07-26）

Token 列单位为**百万 Token（M）**，且是站点按 99:1 / 90% 缓存命中折算的**等效值**，非厂商公布额度。
`—` = 字段缺失，`未公开` / `无限制` 按原文保留。

| 厂商 | 套餐 | 类型 | 月价 | 5h请求 | 周请求 | 月请求 | 5h Token | 周 Token | 月 Token |
|---|---|---|---|---|---|---|---|---|---|
| Kimi | Andante | Coding Plan | ¥49 | 未公开 | 未公开 | 未公开 | 5 | 7 | 28 |
| Kimi | Moderato | Coding Plan | ¥99 | 未公开 | 未公开 | 未公开 | 6 | 27 | 108 |
| Kimi | Allegretto | Coding Plan | ¥199 | 未公开 | 未公开 | 未公开 | 27 | 135 | 540 |
| Kimi | Allegro | Coding Plan | ¥699 | 未公开 | 未公开 | 未公开 | 80 | 400 | 1600 |
| Ollama | Pro | Coding Plan | $20 | 未公开 | 未公开 | 未公开 | - | 125 | 500 |
| Ollama | Max | Coding Plan | $100 | 未公开 | 未公开 | 未公开 | - | 625 | 2500 |
| TaoToken | Pro | Coding Plan | ¥149 | 2000 | 15000 | - | — | — | — |
| TaoToken | Max | Coding Plan | ¥388 | 6000 | 35000 | - | — | — | — |
| 京东云 | Lite | Coding Plan | ¥40 | 1200 | 9000 | 18000 | — | — | — |
| 京东云 | Pro | Coding Plan | ¥200 | 6000 | 45000 | 90000 | — | — | — |
| 优云智算 | Mini | Coding Plan | ¥49 | 300 | 750 | 1900 | 10 | 25 | 65 |
| 优云智算 | Lite | Coding Plan | ¥99 | 600 | 1500 | 3800 | 20 | 50 | 130 |
| 优云智算 | Basic | Coding Plan | ¥199 | 1200 | 3000 | 7600 | 40 | 100 | 260 |
| 优云智算 | Pro | Coding Plan | ¥499 | 3000 | 7500 | 19000 | 100 | 250 | 650 |
| 优云智算 | Max | Coding Plan | ¥799 | 4800 | 12000 | 31000 | 160 | 400 | 1040 |
| 优云智算 | Ultra | Coding Plan | ¥999 | 6000 | 15000 | 39000 | 200 | 500 | 1300 |
| 商汤·日日新 | Free·公测 | Coding Plan | ¥0 | 1500 | 未公开 | 未公开 | — | — | — |
| 字节·方舟 | Lite | Coding Plan | ¥40 | 1200 | 9000 | 18000 | 17 | 125 | 250 |
| 字节·方舟 | Pro | Coding Plan | ¥200 | 6000 | 45000 | 90000 | 83 | 624 | 1249 |
| 摩尔线程 | Free Trial | Coding Plan | ¥0 | 未公开 | 未公开 | 未公开 | — | — | — |
| 移动云 | Lite | Coding Plan | ¥40 | 1200 | 9000 | 18000 | — | — | — |
| 移动云 | Pro | Coding Plan | ¥200 | 6000 | 45000 | 90000 | — | — | — |
| 讯飞·星火 | 高效版 | Coding Plan | ¥199 | 6000 | 45000 | 90000 | — | — | — |
| 讯飞·星火 | 速通版 | Coding Plan | ¥699 | - | - | - | — | — | — |
| 超算 | Lite | Coding Plan | ¥20 | 1200 | 9000 | 18000 | — | — | — |
| 超算 | Pro | Coding Plan | ¥100 | 6000 | 45000 | 90000 | — | — | — |
| 阶跃星辰 | Flash Mini | Coding Plan | ¥49 | 1500 | 6000 | 24000 | — | — | — |
| 阶跃星辰 | Flash Plus | Coding Plan | ¥99 | 6000 | 24000 | 96000 | — | — | — |
| 阶跃星辰 | Flash Pro | Coding Plan | ¥199 | 22500 | 90000 | 360000 | — | — | — |
| 阶跃星辰 | Flash Max | Coding Plan | ¥699 | 75000 | 300000 | 1200000 | — | — | — |
| 阿里·百炼 | Pro | Coding Plan | ¥200 | 6000 | 45000 | 90000 | 200 | 1500 | 3000 |
| Claude | Pro | Token Plan | $20 | Pro 基准 | Pro 基准 | 未公开 | 23 | 104 | 416 |
| Claude | Max *5 | Token Plan | $100 | Pro 的 5 倍 | Pro 的 5 倍 | 未公开 | 115 | 520 | 2080 |
| Claude | Max *20 | Token Plan | $200 | Pro 的 20 倍 | Pro 的 20 倍 | 未公开 | 460 | 2080 | 8320 |
| Codex | Plus | Token Plan | $20 | 无限制 | 无限制 | 无限制 | 120 | 120 | 480 |
| Codex | Pro *5 | Token Plan | $100 | 无限制 | 无限制 | 无限制 | 600 | 600 | 2400 |
| Codex | Pro *20 | Token Plan | $200 | 无限制 | 无限制 | 无限制 | 2400 | 2400 | 9600 |
| DeepSeek Flash | 虚拟套餐 | Token Plan | ¥200 | 无限制 | 无限制 | 无限制 | — | — | 1462 |
| DeepSeek Pro | 虚拟套餐 | Token Plan | ¥200 | 无限制 | 无限制 | 无限制 | — | — | 527 |
| GitHub | 学生 | Token Plan | $0 | 未公开 | 未公开 | 未公开 | — | — | — |
| GitHub | Pro | Token Plan | $10 | 未公开 | 未公开 | 未公开 | — | — | — |
| GitHub | Pro+ | Token Plan | $39 | 未公开 | 未公开 | 未公开 | — | — | — |
| MiniMax | 新Plus | Token Plan | ¥49 | 1500 | 15000 | 60000 | — | — | 600 |
| MiniMax | 新Max | Token Plan | ¥119 | 4500 | 45000 | 180000 | — | — | 1800 |
| MiniMax | 新Ultra | Token Plan | ¥469 | 15000 | 150000 | 600000 | — | — | 7100 |
| OpenCode | Go | Token Plan | $10 | 未公开 | 未公开 | 未公开 | — | — | 146 |
| 华为云 | Lite | Token Plan | ¥59 | 无限制 | 无限制 | 无限制 | — | — | 50 |
| 华为云 | Standard | Token Plan | ¥149 | 无限制 | 无限制 | 无限制 | — | — | 130 |
| 华为云 | Pro | Token Plan | ¥399 | 无限制 | 无限制 | 无限制 | — | — | 380 |
| 华为云 | Max | Token Plan | ¥799 | 无限制 | 无限制 | 无限制 | — | — | 880 |
| 字节·方舟 | Small | Token Plan | ¥40 | 200 | 700 | 2000 | — | — | 22 |
| 字节·方舟 | Medium | Token Plan | ¥200 | 1000 | 3500 | 10000 | — | — | 111 |
| 字节·方舟 | Large | Token Plan | ¥500 | 2500 | 8750 | 25000 | — | — | 278 |
| 字节·方舟 | Max | Token Plan | ¥1000 | 5000 | 17500 | 50000 | — | — | 556 |
| 小米·MiMo | Lite | Token Plan | ¥39 | 无限制 | 无限制 | 无限制 | — | — | 108 |
| 小米·MiMo | Standard | Token Plan | ¥99 | 无限制 | 无限制 | 无限制 | — | — | 290 |
| 小米·MiMo | Pro | Token Plan | ¥329 | 无限制 | 无限制 | 无限制 | — | — | 1002 |
| 小米·MiMo | Max | Token Plan | ¥659 | 无限制 | 无限制 | 无限制 | — | — | 2162 |
| 智谱AI | 新Lite | Token Plan | ¥118 | 未公开 | 未公开 | 未公开 | 13 | 65 | 260 |
| 智谱AI | 新Pro | Token Plan | ¥538 | 未公开 | 未公开 | 未公开 | 79 | 395 | 1580 |
| 智谱AI | 新Max | Token Plan | ¥1078 | 未公开 | 未公开 | 未公开 | 184 | 920 | 3680 |
| 智谱国际版 | 新Lite | Token Plan | $18 | 未公开 | 未公开 | 未公开 | 13 | 65 | 260 |
| 智谱国际版 | 新Pro | Token Plan | $80 | 未公开 | 未公开 | 未公开 | 79 | 395 | 1580 |
| 智谱国际版 | 新Max | Token Plan | $168 | 未公开 | 未公开 | 未公开 | 184 | 920 | 3680 |
| 百度·千帆 | Mini | Token Plan | ¥9.9 | 无限制 | 无限制 | 无限制 | — | — | 10 |
| 百度·千帆 | Lite | Token Plan | ¥40 | 无限制 | 无限制 | 无限制 | — | — | 42 |
| 百度·千帆 | Pro | Token Plan | ¥200 | 无限制 | 无限制 | 无限制 | — | — | 230 |
| 百度·千帆 | Max | Token Plan | ¥600 | 无限制 | 无限制 | 无限制 | — | — | 700 |
| 联通云 | 个人 Lite | Token Plan | ¥15 | 无限制 | 无限制 | 无限制 | — | — | 6 |
| 联通云 | 个人 Pro | Token Plan | ¥30 | 无限制 | 无限制 | 无限制 | — | — | 12 |
| 联通云 | 个人 Max | Token Plan | ¥45 | 无限制 | 无限制 | 无限制 | — | — | 18 |
| 联通云 | 团队 Lite | Token Plan | ¥198 | 无限制 | 无限制 | 无限制 | — | — | 200 |
| 联通云 | 团队 Pro | Token Plan | ¥698 | 无限制 | 无限制 | 无限制 | — | — | 800 |
| 联通云 | 团队 Max | Token Plan | ¥1398 | 无限制 | 无限制 | 无限制 | — | — | 2000 |
| 腾讯云 | Lite | Token Plan | ¥39 | 无限制 | 无限制 | 无限制 | — | — | 35 |
| 腾讯云 | Standard | Token Plan | ¥99 | 无限制 | 无限制 | 无限制 | — | — | 100 |
| 腾讯云 | Pro | Token Plan | ¥299 | 无限制 | 无限制 | 无限制 | — | — | 320 |
| 腾讯云 | Max | Token Plan | ¥599 | 无限制 | 无限制 | 无限制 | — | — | 650 |
| 阿里·百炼 | 标准 | Token Plan | ¥198 | 无限制 | 无限制 | 无限制 | — | — | 375 |
| 阿里·百炼 | 高级 | Token Plan | ¥698 | 无限制 | 无限制 | 无限制 | — | — | 1500 |
| 阿里·百炼 | 尊享 | Token Plan | ¥1398 | 无限制 | 无限制 | 无限制 | — | — | 3750 |

---

## 6. 计费口径原文摘录

以下为原始数据 `note` 字段的去重摘录，是上文各项结论的直接依据，也是将来写单元测试夹具时的取值来源。

**智谱国际版 / 新Lite**

  • 中值约 0.65 亿 Token/周（官方区间 0.43～0.87 亿），Quarterly -20%，Yearly -30%

**智谱国际版 / 新Pro**

  • 6 倍 Lite 用量，中值约 3.95 亿 Token/周，Quarterly -20%，Yearly -30%

**智谱国际版 / 新Max**

  • 14 倍 Lite 用量，中值约 9.20 亿 Token/周，Quarterly -20%，Yearly -30%

**OpenCode / Go**

  • 套餐额度按美元 Credits 计：基础 12 美元、周额度 30 美元、月额度 60 美元
  • 实际计费按 token 消耗。用量按照GLM-5.2, 90%缓存命中率计算

**字节·方舟 / Lite**

  • 官方6.8-8.8期间2.5折活动（首两个月），可与9.5折邀请活动叠加（5.19-11.19）
  • DeepSeek-V4-Pro、GLM-5.1，Kimi-K2.6限时用量2.5倍，GLM-5.2限时4倍用量

**字节·方舟 / Small**

  • 积分制 AFP 计费，见https://www.volcengine.com/docs/82379/2366394。
  • 按照最优模型9倍系数计算，1AFP=1111Token。
  • 有5小时、周、月的Token限制，没有请求次数限制，表格里按照最优模型，每次请求11KToken计算。

**Kimi / Andante**

  Agent 4 倍速

**Kimi / Moderato**

  4 倍额度, Agent 多任务并行

**Kimi / Allegretto**

  20 倍额度

**Kimi / Allegro**

  60 倍额度

**智谱AI / 新Lite**

  • 中值约 0.65 亿 Token/周（官方区间 0.43～0.87 亿），连续包月 8 折，连续包年 7 折

**智谱AI / 新Pro**

  • 6 倍 Lite 用量，中值约 3.95 亿 Token/周，连续包月 8 折，连续包年 7 折

**智谱AI / 新Max**

  • 14 倍 Lite 用量，中值约 9.20 亿 Token/周，连续包月 8 折，连续包年 7 折

**DeepSeek Flash / 虚拟套餐**

  • 官方定价：缓存命中输入 ¥0.02/M Token，缓存未命中输入 ¥1/M Token，输出 ¥2/M Token
  • 按输入输出99:1、输入缓存命中率90%估算：100 Token 约消耗 99×90%×0.02/1000000 + 99×10%×1/1000000 + 1×2/1000000 = 0.000013682 元
  • 约等于 1 元≈7.31M Token，再乘月费 200 元，得到月 Token 上限约 1462M

**DeepSeek Pro / 虚拟套餐**

  • 官方定价：缓存命中输入 ¥0.025/M Token，缓存未命中输入 ¥3/M Token，输出 ¥6/M Token
  • 按输入输出99:1、输入缓存命中率90%估算：100 Token 约消耗 99×90%×0.025/1000000 + 99×10%×3/1000000 + 1×6/1000000 = 0.0000379275 元
  • 约等于 1 元≈2.64M Token，再乘月费 200 元，得到月 Token 上限约 527M

**优云智算 / Mini**

  • 倍率：DeepSeek-V4-Flash x1，MiniMax-M2.7 x2，Kimi-K2.6 x5，GLM-5.2 x2（限时），GLM-5.1 x6
  • 限流：3 并发
  • 价格较贵，但费率明确，无隐藏倍率

**优云智算 / Lite**

  • 倍率：DeepSeek-V4-Flash x1，MiniMax-M2.7 x2，Kimi-K2.6 x5，GLM-5.2 x2（限时），GLM-5.1 x6
  • 限流：5 并发
  • 价格较贵，但费率明确，无隐藏倍率

**优云智算 / Basic**

  • 倍率：DeepSeek-V4-Flash x1，MiniMax-M2.7 x2，Kimi-K2.6 x5，GLM-5.2 x2（限时），GLM-5.1 x6
  • 限流：10 并发
  • 价格较贵，但费率明确，无隐藏倍率

**优云智算 / Ultra**

  • 倍率：DeepSeek-V4-Flash x1，MiniMax-M2.7 x2，Kimi-K2.6 x5，GLM-5.2 x2（限时），GLM-5.1 x6
  • 限流：10 并发
  • 价格较贵但费率明确，无隐藏倍率

**Codex / Plus**

  • 无5h限制，只有周限制

**Claude / Pro**

  • 部分用户订阅时可能被要求实名认证
  • 套餐额度不可用于 OpenClaw、Hermes 等第三方编程 Agent

**Ollama / Pro**

  • 支持 GLM-5.2、Kimi-K2.6、MiniMax-M3、DeepSeek-V4-Pro，模型丰富
  • 倍率大约x10，用量按照GLM-5.2估计，

**Ollama / Max**

  • 暂时停售 • 支持 GLM-5.2、Kimi-K2.6、MiniMax-M3、DeepSeek-V4-Pro，模型丰富
  • 用量按照GLM-5.2估计

**阿里·百炼 / Pro**

  开始限量购买了，不确定每天放不放，放多少

**阿里·百炼 / 标准**

  • Qwen-3.6-Plus，输入5K Token/Credit，输入命中25K Token/Credit，输出0.83K Token/Credit。合输入¥1.58/MToken，缓存¥0.32/MToken，输出¥9.54/MToken
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%/5000 + 99×90%/25000 + 1/830 = 0.00675 Credit，即 1 Credit≈14.82K Token。表格按15K Token/Credit算

**阿里·百炼 / 高级**

  • Qwen-3.6-Plus，输入5K Token/Credit，输入命中25K Token/Credit，输出0.83K Token/Credit。合输入¥1.40/MToken，缓存¥0.28/MToken，输出¥8.41/MToken
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%/5000 + 99×90%/25000 + 1/830 = 0.00675 Credit，即 1 Credit≈14.82K Token。表格按15K Token/Credit算

**阿里·百炼 / 尊享**

  • Qwen-3.6-Plus，输入5K Token/Credit，输入命中25K Token/Credit，输出0.83K Token/Credit。合输入¥1.12/MToken，缓存¥0.22/MToken，输出¥6.74/MToken
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%/5000 + 99×90%/25000 + 1/830 = 0.00675 Credit，即 1 Credit≈14.82K Token。表格按15K Token/Credit算

**小米·MiMo / Lite**

  • 4.1B Credits，无5小时限额，支持集中消耗
  • MiMo-V2.5-Pro：缓存 2.5 Credits/Token，输入 300 Credits/Token，输出 600 Credits/Token
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%×300 + 99×90%×2.5 + 1×600 = 3792.75 Credits，即 1B Credits≈26.37M Token。表格按 MiMo-V2.5-Pro 口径算，MiMo-V2.5 实际更高

**小米·MiMo / Standard**

  • 11B Credits，无5小时限额，支持集中消耗
  • MiMo-V2.5-Pro：缓存 2.5 Credits/Token，输入 300 Credits/Token，输出 600 Credits/Token
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%×300 + 99×90%×2.5 + 1×600 = 3792.75 Credits，即 1B Credits≈26.37M Token。表格按 MiMo-V2.5-Pro 口径算，MiMo-V2.5 实际更高

**小米·MiMo / Pro**

  • 38B Credits，无5小时限额，支持集中消耗
  • MiMo-V2.5-Pro：缓存 2.5 Credits/Token，输入 300 Credits/Token，输出 600 Credits/Token
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%×300 + 99×90%×2.5 + 1×600 = 3792.75 Credits，即 1B Credits≈26.37M Token。表格按 MiMo-V2.5-Pro 口径算，MiMo-V2.5 实际更高

**小米·MiMo / Max**

  • 82B Credits，无5小时限额，支持集中消耗
  • MiMo-V2.5-Pro：缓存 2.5 Credits/Token，输入 300 Credits/Token，输出 600 Credits/Token
  • 按缓存命中率90%、输入输出99:1算：100 Token 约消耗 99×10%×300 + 99×90%×2.5 + 1×600 = 3792.75 Credits，即 1B Credits≈26.37M Token。表格按 MiMo-V2.5-Pro 口径算，MiMo-V2.5 实际更高

**百度·千帆 / Mini**

  • 限时首购 5 折（每日限量）
  • 1000 万 Token 额度，统一抵扣不区分模型倍率与输入/输出

**百度·千帆 / Lite**

  • 限时首购 5 折（每日限量）
  • 4200 万 Token 额度，统一抵扣不区分模型倍率与输入/输出

**百度·千帆 / Pro**

  • 限时首购 5 折（每日限量）
  • 2.3 亿 Token 额度，统一抵扣不区分模型倍率与输入/输出

**百度·千帆 / Max**

  • 限时首购 5 折（每日限量）
  • 7 亿 Token 额度，统一抵扣不区分模型倍率与输入/输出

**华为云 / Lite**

  • 限个人用户购买，每个用户限购 1 套
  • 订阅后不支持退订
  • 订阅后开通 Token Plan 专属 URL 直接接入 Coding / Agent 工具，无需单独开通模型服务
  • 仅限 AI Coding 与 Agent 工具使用，违规使用可能导致 Plan 被中止

**华为云 / Standard**

  • 限个人用户购买，每个用户限购 1 套
  • 订阅后不支持退订
  • 订阅后开通 Token Plan 专属 URL 直接接入 Coding / Agent 工具，无需单独开通模型服务
  • 仅限 AI Coding 与 Agent 工具使用，违规使用可能导致 Plan 被中止
  • 页面标"最受欢迎"档

**腾讯云 / Lite**

  注意此为TokenPlan，而非CodingPlan。35M(3500万) Tokens 额度，约 70 轮问答

**腾讯云 / Standard**

  注意此为TokenPlan，而非CodingPlan。100M(1亿) Tokens 额度，可执行约 200 轮问答

**腾讯云 / Pro**

  注意此为TokenPlan，而非CodingPlan。320M(3.2亿) Tokens 额度

**腾讯云 / Max**

  注意此为TokenPlan，而非CodingPlan。650M(6.5亿) Tokens 额度

**GitHub / 学生**

  • 学生认证免费，高级模型可对话 300 次
  • 存在周限额和 session 限额，不同模型有不同扣减倍率
  • 学生版可通过 Auto 模式路由至 GPT-5.3-Codex

**GitHub / Pro**

  • 存在周限额和 session 限额；不同模型有不同高级模型扣减倍率
  • 已改为用 token 计费模式

**讯飞·星火 / 高效版**

  • 高效版已支持 GLM-5.2
  • 讯飞实际可用量比较多，按调用量计数，明示调用量

**讯飞·星火 / 速通版**

  • 价格比较贵，据宣传无需等待

**联通云 / 个人 Lite**

  • 参考文档https://support.cucloud.cn/document/127/591/2357.html?id=2357&arcid=7080
  • 上下文窗口目前仅支持 200K

**联通云 / 团队 Lite**

  • 参考文档https://support.cucloud.cn/document/127/591/2357.html?id=2357&arcid=7080
  • 上下文窗口目前仅支持 200K
  • 以 DeepSeek-V4-Pro 为例 25,000 credits约为 2亿tokens

**移动云 / Lite**

  • 模型较弱
  • 活动价首月 7.9 元，标准价 40 元/月，仅购买一个月时建议购买，不建议长期使用

**阶跃星辰 / Flash Mini**

  • 当前主打 Step 自有模型，模型较弱
  • 官方以 prompt 计数，这里按 1 prompt≈15 次请求换算
  • 官方只有周限量无月限量，这里按照 1 月=4 周计算

**超算 / Lite**

  • 模型较弱

**商汤·日日新 / Free·公测**

  • 模型较弱
  • 免费公测，Lite / Pro 未上线
  • 日额度：SenseNova 6.7 Flash-Lite 1500 次、SenseNova U1 Fast 1500 次、DeepSeek-V4-Flash 150 次

**摩尔线程 / Free Trial**

  • 模型较弱
  • Free Trial 每天上午 10:00 发放，限量 100 名，30 天有效
  • 京东购买兑换码后需到卡券兑换页面兑换

---

## 7. 对 vmr 设计的直接影响

| 事实 | 对设计的影响 |
|---|---|
| Token 计量占 77%，请求计量占 40%，22% 两者并存 | metric 必须多选并存，不能二选一；`tokens` 优先级不低于 `requests` |
| Credits 制按分量折算，比例差 5～120 倍 | **必须支持分量加权**；等权总量记账会高估 3～8 倍 |
| Credits 折算率是**按模型**给出的（阿里的率明确标注 Qwen-3.6-Plus） | 分量权重必须能按模型区分 → 走按模型的价格表（`pricing.yaml`），不能用 per-provider 全局权重 |
| 5h/周/月三层并存 | 一个账号挂多条 Limit + 取最紧约束（`min`） |
| 存在只有周限、无月限的套餐 | 周期表达必须支持任意 `every`，不能写死 monthly/daily |
| 模型系数（字节 9x、限时 2.5x/4x） | 需要 `model_multipliers` |
| 阶跃星辰 1 prompt ≈ 15 次请求 | `amount` 必须按 vmr 可观测口径配置，需在用户文档写明换算 |
| 站点自己也只能靠"实测/折算"给出可比数字 | 佐证本地估算的必要性，也佐证官方用量 API 校准的价值 |
