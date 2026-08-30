// Ver 2026-08-30, by Sonnet 5 — 精简版：批次 1-3（自传播视觉资产）已交付并标记为已完成；深度分析与实现细节已移除；§9/§10 合并进「下一步任务与优先级」。本文为过渡文档，待下一步清单落地后即可删除。

# VMR 增长突破与全域推广战略（精简版）

> **本文定位**：GTM 与产品级自传播的方向盘，不是实现规范。已落地的部分只留一句话描述 + 状态；未落地的部分在 §8 按优先级排序。
> 已实现功能的权威记录在 `CHANGELOG.md` 与 `docs/UserGuide.md`，不在本文。

---

## 1. 核心价值主张（对外话术的四个锚点）

用户掏出注意力的底层动机只有三条：**别多花冤枉钱、别让活儿半道挂了、出事别抓瞎**。据此收拢为四大高感知卖点：

| 卖点 | 目标人群 | 一句话话术 |
|---|---|---|
| **省钱牌**（守 Prompt Cache） | 天天用 Claude Code / Cursor / OpenClaw、看 Token 账单肉疼的中重度用户 | 「你以为在做高可用，其实把多轮缓存折扣搞丢了。VMR 锁定缓存会话，不断流的同时账单立减。」 |
| **稳定性牌**（防暴毙安全气囊） | 让 Agent 跑批量 / 长流程 / 夜间无人值守的自动化用户 | 「别让凌晨三点一次 429，毁掉跑了两小时的任务。错误分级容灾 + 软拦截逃生。」 |
| **认知与排障牌**（Agent 黑匣子） | 遇到死循环 / 乱调工具 / 幻觉却无从排查的开发者 | 「Agent 跑飞别盲猜。像空难黑匣子一样标出哪一步丢了上下文约束、哪个工具被误调用。」 |
| **精打细算牌**（多套餐管家） | 手握 GLM / MiniMax / Kimi / DeepSeek 多个包月套餐的羊毛极客 | 「Headroom Pacing 按到期时间平滑释放额度，把每一分套餐榨干，溢出流量自动兜底。」 |

**包装原则**：对外不谈「单二进制 N 万行 Go / 无转译三协议 / 零数据库」这类工程自嗨——用户不在乎。把底层能力翻译成上面四条购买理由，配可视化工件分发。

---

## 2. 上手门槛极简化（全部待实现，见 §8 第一梯队）

增长公式的分母是「首次运行耗时 × 配置门槛」。当前从下载到跑通第一个请求仍需约 5 分钟，目标压到 30~60 秒。四个入口（均未实现）：

- **`vmr init`** —— 交互式点火向导：扫描 Shell 里的 `*_API_KEY` → 三问模板（厂商 / Key / 常用 Agent）→ 写精简配置 → 自动跑 `vmr diagnose` 出绿灯。
- **`vmr connect <agent>`** —— 一键对接助手：输出 Claude Code / Cursor / Aider 对应的 `BASE_URL` + `API_KEY` 环境变量与可复制脚本。
- **`vmr proxy --upstream ... --key ...`** —— 零配置临时代理：内存生成临时路由，立刻提供透明代理 + Prompt Cache 亲和 + Audit 日志，把单次调试门槛降到极限。
- **`vmr run -- <agent>`** —— 宿主透明包装器：作为父进程启动子 Agent，自动注入 `BASE_URL`，退出时打印本次 Token 与节省战报。

**`/help.html`**（已存在，含各 Agent 接入指引 + 一键复制）可增强：页内直接发测试请求看命中节点/延迟、局域网直连二维码。

---

## 3. 自传播视觉资产

把 VMR 的运行数据转成用户愿意主动分享的「社交货币」。

| 资产 | 命令 | 状态 |
|---|---|---|
| **空难调查报告**（PROBABLE CAUSE 判定 + 死亡转折点 + token/时间/成本损耗行；飞行记录仪视觉；redact 兼容） | `vmr analyze -journey <id> -html [-redact]` | **已交付（2026-08-29）** |
| **跨模型对战计分卡**（tale-of-the-tape 头 + 分岔点 + 逐指标 A/B + 每侧成本；不宣布赢家） | `vmr analyze -compare a,b -html [-redact]` | **已交付（2026-08-29）** |
| **Tool Schema 浪费卡片**（「X% 从未调用」大字 hero + 累计发出/死重/≈浪费 token + 形态表） | `vmr analyze` 自动写 `{out}/tool-waste.html` | **已交付（2026-08-29）** |
| story 半区成本口径（`ComputeJourneyCost`，与宏观报表 `$` 同源不漂移） | 内部共享 | **已交付（2026-08-29）** |
| **省钱战报**（cache 效率 + token 经济 + failover 救回；`$` 仅在定价可解析时出现并标注「标价估算」） | 待建 | **未实现 · 第三梯队**（触发：需要面向泛受众的物料时） |
| **开源 Issue 事故还原包**（脱敏两层 Raw Byte + 1-Click Replay 命令的标准 Markdown 块） | `vmr export-issue <coord>`（待建） | **未实现 · 第三梯队**（触发：已有用户在外部仓库提 issue 时） |

> 已交付三件的实现细节、关键决策与多轮 review 记录见 `CHANGELOG.md` 的 `[Unreleased]` 段与 git 历史。剩余打磨项进 §8。

---

## 4. 竞品格局与差异化

不打「泛网关」口水仗，在「AI Agent 专用可靠性路由 + 飞行记录仪」这个位置建立不可替代认知。

| 维度 | 中转网关<br>(One-API/New-API) | 翻译网关<br>(LiteLLM/Bifrost) | 桌面控制台<br>(CCR) | 逆向压缩代理<br>(OpenProxy) | **VMR** |
|---|---|---|---|---|---|
| 定位 | 多 Key 简单轮询 | 多模型协议统一转译 | 全功能桌面 + 插件市场 | 客户端适配 + RTK Token 压缩 | **Agent 可靠性路由 + 取证** |
| 依赖 | 需 MySQL/Redis | 需 Redis/PostgreSQL | Electron 桌面端 | Rust 单二进制 + UI | **Go 单静态二进制，零外部 DB** |
| 协议 | 强转 OpenAI | 强统一为 OpenAI IR | 转译 + Fusion 注入 | 转译 + 文本剪裁 | **字节保真（三协议直通）** |
| 长文本缓存 | 随机轮询摧毁缓存 | 基础 Affinity | 无显式亲和 | RTK 压缩破坏前缀缓存 | **跨协议 Session-Sticky** |
| 调试取证 | 仅 HTTP 状态码 | 扁平请求日志 + Cost | 历史会话归档 | 终端输出 / 抓包 | **步骤级 Story + 1-Click Replay + 对比归因** |

**差异化话术**：
1. **对标 CCR**：要 Electron 界面 + 插件市场选 CCR；要常驻内存低一个数量级、透明容灾、真正的黑匣子取证，VMR 是唯一的单二进制 Unix 工具。
2. **对标 OpenProxy**：RTK 强压请求文本看似省 20% Token，但破坏 Prompt Cache 前缀匹配，反而丢掉 70%~90% 缓存折扣；VMR 坚持字节保真 + Session-Sticky，多轮编码综合账单更低、Tool Call 绝无损坏。
3. **对标 LiteLLM / Bifrost**：协议翻译在复杂 Agent 时代有害（吃私有字段、损坏 Tool Call 结构）；VMR 不翻译，只做极致字节保真与可靠性路由。

> **对外发布前须一次独立核实**：CCR star 数、OpenProxy 代码行数、以及任何吞吐/内存倍数。没有 VMR 自己的 benchmark 就一律用定性表述（「低一个数量级」「显著更低」），不写未实测的精确倍数。

---

## 5. 全渠道推广打法

**「内容基底沉淀 + 自动化借势截流 + 突发事件第一响应」** 三维打法。

### 渠道矩阵

| 平台 | 受众 | 频次 | 切入点 |
|---|---|---|---|
| **X (Twitter)** | 海外 AI 开发者、Indie Hackers | 每周 2~3 条 Thread + 战报图 | "Why your Prompt Cache is quietly broken" / "Agent crash forensics at 3 AM" |
| **Reddit** (`r/LocalLLaMA`/`r/ClaudeAI`/`r/Cursor`/`r/selfhosted`) | 硬核折腾派 | 每周 3~5 个排障帖答疑 | 帮楼主诊断 429 / Tool Call 截断 / 缓存命中率，回帖自然给出 VMR 方案 |
| **Hacker News** | 资深架构师、开源爱好者 | 单次里程碑 Show HN | `Show HN: vmr – A zero-DB router & flight recorder for AI agents (Go)`；强调 No-DB / Single-binary / Byte-faithful / Local-first |
| **知乎** | 国内 AI 工程师、Agent 开发者 | 每周 2~3 篇（专栏 + 定向问答） | 「如何降低 Claude Code / Cursor 成本」「本地 LLM 网关怎么选」「Agent 幻觉/死循环怎么排查」 |
| **V2EX** (`/go`/`/create`/`/programmer`/`/ai`) | 国内独立开发者、Go 工程师 | 单次里程碑 | 《撸了个开源 AI Agent 路由与飞行记录仪：单二进制无 DB，解决多账号额度与 Prompt Cache 冲突》 |
| **Dev.to / 掘金** | Go/AI 开发者 | 每 2 周 1 篇 | 三协议无锁隔离、零拷贝透传、环形缓冲探测的 Go 实现剖析 |

### 借势截流 80/20 法则

回帖模板：40% 共情 + 根因定位（「Anthropic/DeepSeek 要求前缀精确匹配才命中缓存，路由一切端点/改 header 命中率就归零……」）+ 40% 原理级通用解法（「保证 proxy 对同一上游实例做 5-10 分钟 session-sticky……」）+ 20% 工具佐证（「想要本地零配置工具原生处理这个，看看 vmr……」）。

**痛点关键词池**：`Claude Code expensive` / `Prompt cache miss` / `Cursor API bill` / `DeepSeek 429` / `Agent infinite loop` / `Tool call schema broken` / `LLM silent failover` / `LiteLLM local alternative` / `Coding plan pacing`。

### 突发事件第一响应

- **主流 API 大面积宕机 / 429 降级** → 1 小时内发《5 分钟配置：VMR 实现故障时自动无感降级》。
- **厂商上线新 API 特性**（新 Reasoning 字段 / Responses 协议）→ 发《翻译型网关全挂了，VMR 字节保真第一天零代码直通》。
- **知名 Agent 跑飞刷爆账单新闻** → 发深度复盘《用 VMR 压缩边界检测 + 配额闸门，从网络层掐死 Agent 死循环》，附脱敏空难报告。

---

## 6. 爆款选题库

- **《为什么你的 Prompt Cache 总失效？多账号 Agent 网关的隐藏成本》** —— 拆前缀匹配原理；无状态轮询 vs Session-Sticky 命中率实测；多账号既配速分流又死守缓存亲和。
- **《凌晨 3 点 Agent 跑飞了？别猜，用「飞行记录仪」1 秒重放复现》** —— `vmr analyze -journey` 步骤还原 + `vmr replay` 原生字节重放；Zero-Instrumentation 捕获现场。
- **《拒绝 10 个 Docker 容器：为什么用纯 Go 单二进制重写 AI Agent 路由》** —— 单二进制、内存环形缓冲、Raw Byte 审计、无外部 DB；压测实录。
- **《GLM + DeepSeek + Claude 混合编队：如何榨干每一分 Token-Plan 额度》** —— Headroom Pacing 桶/闸模型；重置周期内恰好耗完，溢出自动降级。

---

## 7. 已交付里程碑

- **2026-08-29 — 批次 1-3：自传播视觉资产落地。** 空难调查报告、对战计分卡、Tool Schema 浪费卡片三件可分享 HTML 工件，加共享的 story 半区成本口径。工程：新增 9 文件、改 ~14 文件；`go test -race`、`archtest`、`gofmt` 全绿；UserGuide + `.zh` + CHANGELOG 已同步。经三轮 review，唯一实质 bug（严重度判定条与死亡转折点相互矛盾）已修。详见 `CHANGELOG.md` `[Unreleased]` 与 git 历史。

---

## 8. 下一步任务与优先级

> 工作量口径：≤3 人 AI-native 团队，遵守本仓库测试纪律（`archtest` 行数预算、differential test、i18n 双语、`.zh` 兄弟文档同步），单位「专注人日」。

### 第一梯队（建议马上处理）

传播资产（分子）已就位，但落地入口（分母）仍是 5 分钟 —— 这是当前最大瓶颈。

1. **`vmr init` + `vmr connect <agent>`**（约 3–5 人日）—— 把 Time-to-Value 从 5 分钟压到 30 秒。`init` 做环境变量扫描 + 三问模板 + 写配置 + 自动 `diagnose`；`connect` 输出 Claude Code / Cursor / Aider 的环境变量与复制脚本。没有它，看到内容的人还是留不下来。
2. **F-1：`tool-waste.html` 工具名可读化**（约 0.5 人日）—— 卡片「工具集形态」列现在显示哈希 `tools:67/7bc83937`，对一张要发推的卡片是死信息。数据已在 `ToolShapeRow.NeverCalled` / `Declared`，改成预览几个真实（未调用的）工具名即可。三件已发布工件里最弱的一个。
3. **CRITICAL 空难报告实机样张**（无代码，素材采集）—— `logs/vmr-audit-2026-08-24.jsonl.zst` 的 `j-pimini-…-b0ebb0e4` 是一个纯规则判定的 CRITICAL（264 步 / 1h 46m / 36.42M token，Step 39 一次压缩丢弃 11 个具名约束），可直接作为 GTM 首发配图。

### 第二梯队

- **`vmr proxy` / `vmr run -- <agent>`**（约 2–3 人日）—— onboarding 的补充形态，`init`/`connect` 之后做。
- **F-2：`-compare` 的 ID 解析支持通配符**（约 0.5 人日）—— `-journey` 已支持 glob，`-compare` 的 `resolveJourneyID` 只支持精确前缀，统一到 `journeyPatternMatches`。
- **F-3 / F-6：compare 页小修**（约 0.5 人日）—— 单侧缺定价时 `—` 加脚注「定价未收录」；两侧都无交付物时跳过空事实行。
- **LLM `goal_drift` 定位到 Step 1**（约 1 人日）—— 第一步无「原目标」可漂移，语义不通，疑似 `llm_findings.go` 的 StepSeq 归一化问题。超出传播页范围，属既有检测器行为。
- **journey / compare 的 `.md` 补成本行**（约 0.5 人日）—— 目前成本只在 JSON + HTML，`.md` 没有，口径对齐一下。

### 第三梯队 / 触发式

- **F-4 / F-5**：脱敏模式 Finding code 加中文友好映射；超长无空格 CJK 标题在窄视口调小 `jhead h1` 字号。
- **`tool-waste.html` 工具名脱敏 `-redact`**：默认可见（`read_file`/`bash` 类几乎无敏感性），如需 hash 工具名再加。
- **`/help.html` 增强**：页内发测试请求、局域网直连二维码。
- **资产 1「省钱战报」**（约 5–8 人日）—— 触发：团队确定要一个面向泛受众的物料。轮廓：`cost_without_cache` 反事实数学 + 卡片渲染器 + PNG 导出；框架围绕 cache 效率 + token 经济 + failover 救回；`$` 仅在定价解析成功时出现并标注「list-price estimate, not your bill」。
- **资产 5「`vmr export-issue`」**（约 3–5 人日）—— 触发：star / issue 量表明已有用户在别处提 issue。轮廓：复用 replay 记录选择，新增保守的正文脱敏 pass（默认全部正文→占位，`-include-system` / `-include-last-turn` 显式 opt-in），产出含两层请求/响应 + 错误分级 + HTTP 状态 + `vmr replay` 行的 Markdown 块。脱敏做错 = 把用户私有代码泄进公开 issue，必须保守。

### GTM 执行（非代码，与第一梯队并行）

- 第一梯队样张就绪后：HN Show HN 首发 · 知乎 3 大问题占位回答（成本 / 选型 / 排障）· X thread + 战报图 · V2EX / Dev.to 长文。
- Piggyback：每日扫 Twitter + Reddit 痛点关键词（见 §5），产出 2~3 条 80/20 借势回帖草稿。
- Newsjacking 待命：大厂宕机 / 新特性 / Agent 刷账单新闻的 1 小时响应模板备好。
- GitHub Issues 截流：在主流 Agent 仓库帮排障（资产 5 落地后配标准还原包）。

### 与四大价值主张的对齐

| 价值主张 | 承载它的传播页面 |
|---|---|
| 省钱牌 | 资产 1（诚实版，未建）+ 资产 3（浪费侧，已建） |
| 稳定性牌 | 资产 2（failover 徽章 + 死亡转折点，已建） |
| 认知与排障牌 | **资产 2（主力，已建）** + 资产 4（步级分歧，已建） |
| 精打细算牌 | `vmr report` §2.5 额度对照子表（已存在，未单独做传播页） |

资产 2 同时覆盖「稳定性」与「排障」，是 ROI 最高的一件 —— 已交付。
