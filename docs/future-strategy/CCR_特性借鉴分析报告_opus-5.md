<!-- Ver 2026-07-28 14:05, by Opus 5 -->

# 从 claude-code-router 提取可借鉴特性 —— VMR 演进分析报告

> 分析对象：`claude-code-router` v3.0.16（本地路径 `~/opensource/claude-code-router`，HEAD `d2867bd`）
> 参照对象：`vmr`（本仓库）
> 信息来源：CCR 的 README / README_zh、完整中文文档站（`docs/src/content/docs/zh/**`，18 篇配置文档 + 6 篇指南）、`packages/core/src` 下路由、pipeline、upstream executor、fusion、hosted-web-search、context-archive、credential-pool、limits、model-catalog、observability 等源码。两份既有 review 文档（`VMR_全项目深度审查报告.md`、`CRITIQUE_REVIEW.md`）仅用作索引，所有结论以源码与官方文档为准，出入之处已在文中标注。

---

## 0. 任务 Debrief

**你的核心意图**：不是做一次"CCR vs VMR 谁更好"的对比，而是把 CCR 当作**用户需求的探针**——它在开源市场跑到 36k+ star，说明它踩中的痛点是真实的。任务分三步：

1. 真正读懂 CCR 每个特性**在干什么、怎么实现的、解决什么场景**（尤其是 Fusion 这类只看名词完全不知所云的东西），不能停在名词层面；
2. 提取与 VMR 的差集，按**用户需求价值**排三个梯队（第三梯队 = 不推荐引入）；
3. 由 CCR 的特性延伸，头脑风暴 CCR 没做、但 VMR 值得探索的新特性。

**你的方法论约束**：发散阶段不带 VMR 的架构原则（byte-faithful、单二进制、零依赖、不做规则引擎……）做过滤，先纯从生态/场景/用户需求出发；原则冲突作为**最后的决策筛子**单独处理。本文严格按这个顺序组织：第 2–4 章不带原则判断，第 6 章才做原则复核。

---

## 1. 先厘清一件事：CCR 和 VMR 根本不是同一类产品

这一节不是客套，它直接决定后面 90% 的取舍判断。

| 维度 | CCR | VMR |
|---|---|---|
| 产品形态 | Electron 桌面应用 + Web UI + npm CLI + Docker，四个发行渠道 | 单个 Go 二进制 |
| 存储 | SQLite（配置、请求日志、上下文归档、用量、插件数据） | 无数据库，YAML 配置 + JSONL 审计文件 |
| 协议处理 | **翻译型**：4–5 种协议互转（anthropic_messages ↔ openai_chat_completions ↔ openai_responses ↔ gemini_generate_content ↔ gemini_interactions），核心转换由外部依赖 `@the-next-ai/ai-gateway` 承担 | **透传型**：两个协议各自独立路由，永不互转 |
| 请求处理 | 16 阶段 pipeline，每阶段主动改写请求体/头，并记录 JSON Pointer 级变更 | 只改 model 名 + 少量 provider quirk 修复，其余字节忠实 |
| 产品重心 | **Agent 控制平面**：管 agent 进程、配置文件、登录态、MCP、浏览器、IM 机器人 | **数据平面 + 事后取证**：路由、failover、审计、报告 |
| 目标用户 | 所有想换模型的 coding agent 用户（含完全不看命令行的） | 关心"这一夜 agent 到底发生了什么"的技术用户 |

**一句话**：CCR 是"把所有 agent 和 provider 管在一个地方"的**控制面产品**，模型网关只是它的一个组件；VMR 是"agent 运行时的黑匣子"的**数据面工具**。

这个差异有两个直接推论：

- CCR 相当大一部分特性（Fusion、ToolHub、Bot 接力、内置浏览器、插件市场、仪表盘）**根本不在 LLM API 请求路径上**，它们是围绕桌面应用长出来的。VMR 抄这些等于换赛道，而且是换到 CCR 已经占稳的赛道。
- 但 CCR 有一批特性是**纯粹的数据面能力**，只是因为 VMR 一直在打磨 byte-faithful 而没顾上——这些才是真正的差集金矿：凭据池、模型元数据目录、限额、账户余额、路由原因回传。（模型发现原本也列在这里，核对源码后发现 VMR 早已实现，见 T1-3。）

---

## 2. CCR 特性详解（重点：讲清楚原理和例子）

按"这是什么 → 怎么实现的 → 举个例子 → 解决什么痛点"的格式。CCR 独有的、名词唬人的特性讲透；显而易见的从简。

### 2.1 Fusion 组合模型（最需要解释的一个）

**这不是模型融合、不是 MoE、不是模型集成。** 它的本质是：**在网关侧给一个基座模型"外挂"一组由网关自己执行的工具，然后把这个"基座模型 + 工具集"打包成一个新的、可被路由和选择的虚拟模型名。**

**实现机制**（`packages/core/src/mcp/fusion-config.ts` + `mcp/fusion-vision-mcp.ts`）：

1. 用户在 UI 里创建一个 Fusion 模型，比如"基座 = GLM-5.2，能力 = `ccr-fusion-builtins / vision_understand`，视觉模型 = GLM-5V-Turbo"。
2. CCR 保存后，为这个 Fusion profile **生成一个 stdio 传输的 MCP server 进程配置**，写进 core gateway 的运行配置。这个 MCP server 是 CCR 自带的一个 bundled 脚本（`fusion-vision-mcp.ts` 的编译产物），通过环境变量注入参数：

   ```
   FUSION_BUILTIN_TOOL_KIND = vision
   FUSION_TOOL_NAME         = vision_understand
   VISION_GATEWAY_BASE_URL  = http://127.0.0.1:<core>/v1   ← 回指 CCR 自己
   VISION_GATEWAY_API_KEY   = <内部 token>
   VISION_MODEL             = GLM-5V-Turbo
   ```

3. 请求打到这个 Fusion 模型时，core gateway 把 `vision_understand` 这个工具**注入到发给基座模型的 tools 列表里**，然后在网关内部跑一个 **agentic 工具循环**（文档明确写："Fusion 工具循环不再设置轮次上限或工具调用次数上限"）。
4. 基座模型（GLM-5.2，纯文本模型）决定调用 `vision_understand`，MCP server 收到调用后，**回头请求 CCR 自己的 `/v1`**，用 GLM-5V-Turbo 分析图片，把结果作为文本返回给基座模型。
5. 基座模型拿着这段文本证据继续推理、写代码、给出最终回答。

**例子**：你日常用 GLM-5.2 写代码手感很好，但它不认图。你截了一张报错界面图。传统做法是切到 GLM-5V-Turbo，但那个模型代码能力差。用 Fusion 后：你选 `GLM-5.2V` 这个虚拟模型 → GLM-5.2 拿到请求 → 它调 `vision_understand` → GLM-5V-Turbo 描述截图内容 → GLM-5.2 基于描述给出修复方案。对客户端来说，它只是"用了一个会看图的 GLM-5.2"。

**同一机制的三个变体**：
- **内置图像能力**（上面这个）
- **内置联网搜索**：工具变成 `web_search`，后端可选 Brave / Bing / Google CSE / Serper / SerpAPI / Tavily / Exa，或者 **In-app Browser**（用 Electron 隐藏窗口真的打开搜索结果页抓可见文本，无需任何搜索 API Key）
- **自定义 MCP 工具**：把任意 stdio / streamable-http / sse MCP server 的工具绑到某个模型上
- **媒体生成**：图片生成/编辑、文生视频/图生视频，也是同一套 Fusion 工具形态；产物落 CCR 私有数据目录，返回本地路径 + MIME + SHA-256 + 限时 URL；付费提交支持 `idempotency_key` 防止网络重试重复计费

**痛点**：用户对某个模型有"手感依赖"（写代码稳、指令跟随好），不愿意为了单一能力（看图/联网）整体换模型。Fusion 让能力可以"打补丁"式叠加。

**代价**：网关变成了一个 agent runtime——它要跑工具循环、管 MCP 子进程、缓存媒体产物。这是 CCR 和 VMR 分野最深的地方。

---

### 2.2 Hosted Web Search 协议桥（CCR 最精巧的一个特性，也最被低估）

**这是什么**：Anthropic / OpenAI / Gemini 各自都有**服务端托管的搜索工具**（Anthropic 的 `web_search_20250305`、OpenAI 的 `web_search_options`、Gemini 的 `google_search`）。客户端（比如 Claude Code）会在请求里声明它。但你把请求路由到一个不支持这个能力的第三方 provider（DeepSeek、GLM、Kimi）时，上游要么报错要么忽略。

**CCR 的做法**（`gateway/features/hosted-web-search/`，6 个文件，其中 `response-transform.ts` 1252 行）：

1. `discovery.ts` 检测请求体里是否声明了托管搜索工具（三种协议各自的形态判定）；
2. 如果有、且路由到的 provider 不支持，CCR **自己执行搜索**（用 Fusion 的搜索后端）；
3. `response-transform.ts` 把搜索结果**按客户端所用协议的原生形态重新编码**回响应流——对 Anthropic 就生成 `server_tool_use` + `web_search_tool_result` 块，SSE 与非 SSE 两条路径都有对应实现。

**例子**：Claude Code 发起带 web search 的请求 → 你把它路由到 DeepSeek → DeepSeek 完全不知道 web search 是什么 → CCR 拦下，用 Tavily 搜了，把结果编成 Anthropic 协议的 `web_search_tool_result` 事件塞进 SSE 流 → Claude Code 界面上正常显示"搜索了 3 个来源"，完全不知道背后是 DeepSeek + Tavily。

**痛点**：客户端的能力假设（它以为在跟 Anthropic 说话）和实际 provider 的能力不匹配时，用户看到的是难以理解的报错。这类"**能力垫片（capability shim）**"是所有跨 provider 路由都会撞上的问题。

**代价**：需要为每个协议、每种传输模式（SSE / JSON）写一套响应重编码，`response-transform.ts` 光这一个特性就 1252 行——这是翻译型网关必然要付的维护税。

---

### 2.3 ToolHub —— MCP 工具的懒加载检索层

**这是什么**：当你接了 10 个 MCP server、总共 120 个工具，Claude Code 每次请求都要把这 120 个工具的完整 schema 放进 system prompt，可能吃掉 20k+ token，而且模型更容易选错工具。

**CCR 的做法**：向 agent 只暴露**一个** MCP server（`ccr-toolhub`），里面只有两个元工具：

- `tool_hub.resolve(任务描述)` —— 用一个**独立的轻量检索模型**（文档建议 `deepseek-v4-flash` 这类 Flash 价位模型）去读全部后端 MCP server 的工具目录，挑出本轮任务可能需要的 N 个工具（默认 10，可配 1–20），返回工具包
- `tool_hub.invoke(工具名, 参数)` —— 调用刚才选中的真实工具

**例子**：agent 接到"帮我查一下生产环境 Postgres 里昨天的订单数"→ 它调 `tool_hub.resolve("query production postgres")` → 检索模型从 120 个工具里挑出 `postgres.query`、`postgres.describe_table` 两个返回 → agent 再 `tool_hub.invoke("postgres.query", {...})`。整个过程中，那 118 个无关工具的 schema 从未进入主模型的上下文。

**附带的一个大特性**：ToolHub 开启后可以打开"内置浏览器自动化"，让 agent 用 CCR Desktop 的 Electron 内置浏览器做真实网页操作（导航、点击、输入、滚动、等待）。遇到登录/验证码/CAPTCHA 时，CCR **弹出浏览器窗口让人类接管**，最长等 10 分钟，用户点 Done 后 agent 继续。还配了一个 Chrome 解包扩展，可以把系统 Chrome 中指定域名的 cookies + localStorage 导入内置浏览器，实现"复用你已登录的网站状态"。

**痛点**：MCP 工具膨胀导致的上下文成本和选择精度下降，是 2026 年 agent 用户的普遍抱怨。

---

### 2.4 Context Archive —— 对抗 compaction 信息丢失

**这是什么**：agent 长会话跑到上下文快满时会做 compaction（压缩历史）。压缩必然丢信息，之后 agent 经常"忘了前面为什么这么做"。

**CCR 的做法**（`gateway/context-archive.ts` 840 行 + `context-archive/store.ts` + `protocol.ts` 798 行）：

1. 网关**把每次请求的完整 body 存进 SQLite**，按 `session_id` 组织成带 `generation` 版本号的链（`parent_archive_id` 指向上一代）；
2. 检测到 compaction 信号（Claude Code 的显式 header `x-ccr-context-compact`、Codex 的 `compaction_trigger` item、`/responses/compact` 路径）时，CCR 在压缩后的响应里**追加一段 handoff footer**，内容大意是"历史细节已归档，archive_id = xxx，需要时用 archive 工具取回"；
3. 同时通过一个 MCP server 暴露一个归档访问工具，入参是 `{ archive_id, session_token, task }`（源码 `context-archive.ts:704-711`），agent 用它按需检索历史；
4. 保留策略：最大快照数 + 最大字节数 + 保留天数，且**会保护当前会话的 lineage 链不被清理**，文件权限 0600/0700。

**例子**：一个跑了 3 小时的重构任务，中途 compact 了两次。第三小时 agent 想不起来"为什么当初决定不改 `foo.go`"——它调归档工具，传入 `task: "why did we decide not to modify foo.go"`，CCR 从归档链里检索出当时那一轮对话的相关片段返回。

**痛点**：无人值守长任务的上下文断层。这是目前市面上很少有网关碰的问题。

---

### 2.5 Subagent 模型标签路由（`<CCR-SUBAGENT-MODEL>`）

**这是什么**：Claude Code 的 Agent / Task / Workflow 工具会派生子请求。默认这些子请求跟主请求用同一个模型——但子任务往往只是"搜代码""读文件"，用便宜模型就够了。

**CCR 的做法**（文档 `routing.md` + `gateway/claude-code-router-plugin.ts`）：

1. 用户在 CCR 的模型页给模型填 **Description**（任务导向的描述，如"适合代码搜索、文件梳理、摘要、低成本并行 Subagent"）；
2. 主请求命中内置路由时，CCR 检查工具列表，如果至少有一个模型配了 Description，就把"可用模型 + 说明"注入到 `Agent` / `Task` 工具的 description 和 `prompt` 参数说明里（组织成 "Configured CCR gateway models"）；
3. 如果工具列表里有 `Workflow`，还额外要求 workflow 内部创建的每个 agent 的 prompt 第一行也要带标签；
4. Claude Code 调用 Task 时，自己在 prompt 第一行写 `<CCR-SUBAGENT-MODEL>供应商/模型</CCR-SUBAGENT-MODEL>`；
5. 派生请求进 CCR 后，CCR **从 system 或前两条 user message 中提取并删除**这个标签，然后路由到标签指定的模型。

**例子**：主请求用 Opus 级模型做架构分析，它派生 5 个并行 subagent 去搜索代码——这 5 个请求的 prompt 第一行带着 `<CCR-SUBAGENT-MODEL>DeepSeek/deepseek-v4-flash</CCR-SUBAGENT-MODEL>`，全部路由到便宜模型。成本可能降一个数量级。

**巧妙之处**：CCR 没有去猜"这个请求是不是 subagent"，而是**让模型自己声明**——用 prompt 注入把选择权交给上层 agent，配合模型 Description 让它做出有依据的选择。文档明确说 `x-claude-code-agent-id` 这类 header 只用于观测，模型选择以标签为准。

---

### 2.6 Codex apply_patch 工具协议桥

**这是什么**：Codex CLI 原生的 `apply_patch` 是一个 **custom/freeform 工具**（入参是原始 patch 文本，不是 JSON），只有 GPT 系模型训练过这种形态。第三方模型接进来后，会绕过它去生成 `sed -i`、`python` 脚本来改文件——极其危险且不可靠。

**CCR 的做法**：出站时把 `apply_patch` 转成名为 `virtual_apply_patch` 的**普通 function tool**，并在工具说明里注入完整的 `apply_patch.lark` 语法定义，要求模型把 patch 写进 `patch` 字段。模型返回后，CCR 再把 `virtual_apply_patch` 的调用**转换回** Codex 期望的 `custom_tool_call`（`name = apply_patch`，`input = 原始 patch 文本`）。CCR 不执行 patch，真正改文件的还是 Codex 客户端。

**开关逻辑很有品味**：对非 GPT 模型自动启用，**不受 Codex 内置路由开关影响**；GPT 命名模型和实际基模为 GPT 的 Fusion 模型继续走原生 freeform 路径。

**痛点**：客户端的工具协议假设与实际模型的能力不匹配。这是 2.2 的同一类问题（能力垫片），只是发生在工具层而非搜索层。

---

### 2.7 上游凭据池（credential pool）

**这是什么**：一个 provider 下挂多把 API Key，网关自动调度。

**实现**（`providers/credential-pool.ts` + `gateway/upstream/executor.ts:834-885`）：

每把 key 可配 `priority`（数字小优先）、`weight`（同级内权重，越大越优先）、以及一份 **限额 JSON**：

```json
{ "rpm": 60, "tpm": 100000, "ipd": 500 }
```

支持 `rpm/rph/rpd`（请求）、`tpm/tph/tpd`（token）、`ipm/iph/ipd`（图片），外加自定义窗口 `maxRequests + windowMs` / `maxTokens + quotaWindowMs`。

**选择算法**（源码确认）：
```
先按 (priority 升序, utilization 升序, weight 降序, index 升序) 排序
若最高优先级组内所有 key 的 utilization 都 >= 80%（spillover 阈值）
  → 切换成全局 (utilization 升序, priority 升序, weight 降序, index 升序) 排序
```
即：**平时严格按优先级用主号，主号快打满了才全局按利用率摊平。** 失败（401/403/429/5xx）的 key 进 60 秒内存态冷却。响应头 `x-ccr-provider-credential-id` 回报实际用了哪把 key，`x-ccr-provider-credential-saturated` 标记饱和状态。

**痛点**：国内 coding plan 号池、免费额度轮换是极高频的真实用法。VMR 目前只能靠"复制一份 provider 条目 + 不同 key"来近似，而且没有限额感知。

---

### 2.8 客户端 API Key 的过期与本地限额

CCR 的客户端 Key（"CCR 客户端访问 Key"，区别于上游 provider 凭据）可以配：

- **过期时间**：永不 / 7 天 / 30 天 / 90 天 / 自定义精确时刻
- **本地限额**：按请求数 / token 数 / 图片数 × 每分钟 / 每小时 / 每天，多条规则叠加

限额用内存态窗口计数器（`gateway/limits/window-limiter.ts`），入站 token 用字符数 ÷ 4 估算 + `max_tokens` 作为输出估计。命中即拒绝。

**痛点**：把网关给同事/CI/多个 agent 实例共用时，需要限制单个调用方的消耗。

---

### 2.9 模型元数据目录（models.json）

CCR 仓库里有一个 **16 MB 的 `packages/core/models.json`**，由 `scripts/generate-models-json.mjs` 从三个上游源合成：

- LiteLLM 的 `model_prices_and_context_window.json`
- models.dev 的 API
- OpenRouter 的 `/api/v1/models`

内容包含每个模型的：`displayName`、`aliases`、`limits`（contextTokens / inputTokens / outputTokens / maxTokens / supports1MContext）、`modalities`（input/output）、`capabilities`、`reasoningEffort` 配置、以及来源记录。

**用途**：成本估算、上下文窗口判断、模型能力展示、`/v1/models` 响应的 display_name。

**痛点**：手工维护每个模型的定价、上下文窗口、能力标记，是所有网关的隐性维护负担，而且很容易过期。

---

### 2.10 模型发现（`/v1/models`）+ 零配置接入

CCR 在网关上实现 `GET /models` 和 `GET /v1/models`，而且**按调用方分形态**（`gateway/features/model-discovery.ts`）：

- Claude Code CLI（识别 User-Agent）：返回 Claude 兼容的模型列表 → 用户在 CLI 里输入 `/model` 就能看见并切换所有 CCR 暴露的模型（含 Fusion 模型）
- Claude App：返回 Claude App 能识别的 inference models 结构，把 `供应商/模型` 和 Fusion 模型映射成 Claude App 认得的模型项，用 display name 保留真实语义
- 其他：标准 OpenAI 兼容的 `{object: "list", data: [...]}`

配合 Agent Profile 写入 `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`。文档把 Claude App 的体验称为**"零配置（zero-config）"**：从 CCR 桌面应用打开 Claude App，CCR 自动写入网关地址、API Key、模型发现列表和独立用户数据目录，用户什么都不用做。

**痛点**：用户配好网关后的第一个问题永远是"我怎么知道有哪些模型能用 / 客户端为什么列不出来"。

---

### 2.11 Agent Profile —— 多开、隔离、一键启动

这是 CCR 桌面产品的核心体验，也是它 star 数的主要来源之一。

**机制**：每个 Profile 有独立 `id`，选择"仅从 CCR 打开时生效"后：

| 隔离项 | 做法 |
|---|---|
| 配置文件 | 按 profile id 在 CCR 目录下生成独立的 Claude Code settings / Codex `config.toml` / OpenCode JSON / Kimi `config.toml` / Pi `models.json` |
| 启动器 | 每个 profile 生成独立的 wrapper 脚本，`ccr-app "Claude Code - Work"` 直接启动 |
| HOME 隔离 | `GROK_HOME` / `KIMI_CODE_HOME` / `PI_CODING_AGENT_DIR` 指向 profile 专属目录，**初始从用户配置复制，之后独立，不回写原文件** |
| App 数据目录 | Claude App / ChatGPT / ZCode App 用按 id 区分的 userData 目录，可以同时开多个实例 |
| 凭据隔离 | 隔离 `auth.json`，避免本机 xAI/Anthropic OAuth token 覆盖 CCR Key |

**痛点**：用户最大的顾虑是"我配了网关会不会把我原来能用的 Claude Code 搞坏"。CCR 用"完全不碰你的默认配置"消解了这个恐惧，这是极聪明的产品设计。

---

### 2.12 本机 Agent 登录态导入

添加 provider 时，CCR 扫描本机是否已有 Claude Code / Codex / ZCode / Kimi CLI 的登录凭据：

- **Claude Code**：读 OAuth 凭据 → 建一个 `Claude Code API` provider（`anthropic_messages`）+ OAuth provider plugin，把认证转成 Claude Code 登录态；账号用量走 Anthropic OAuth 用量接口
- **Codex**：读登录文件 + 模型缓存 → 建 `Codex API` provider（`openai_responses`），能自动刷新 token，能读 Codex 额度/余额/token 统计
- **ZCode**：读本机 ZCode 配置里的 provider key / base URL / 模型列表

**例子**：你有 Claude Max 订阅，登录过 Claude Code。导入后，你可以让 Codex CLI 或 Cursor 通过 CCR 用你的 Claude 订阅额度——不需要额外买 API key。

**痛点**：订阅制账号（Claude Max、ChatGPT Plus、Kimi Code）没有 API key，但值钱。

---

### 2.13 账户余额 / 额度读取（account connector）

CCR 能在 UI 和托盘里展示每个 provider 的余额、套餐额度、重置时间、状态。三种模式：

- **标准端点**：尝试 `/.well-known/ccr/account`、`/v1/account/limits`（CCR 自定义的一套标准，希望 provider 适配）
- **HTTP JSON**：用户手填一个接口 URL + 方法 + headers + body，然后用 **轻量 JSONPath** 把响应字段映射到语义字段：

  | 写法 | 含义 |
  |---|---|
  | `$.balance.remaining` | 读对象字段 |
  | `$.items[0].value` | 数组下标 |
  | `$.limits[?(@.type=="TOKENS")].remaining` | 数组内简单等值查找 |
  | `100 - $.data.percentage` | **数值减法表达式**，把"已用百分比"转成"剩余百分比" |

  UI 上有"测试用量请求"按钮，测完把响应 JSON 的可选字段列出来，点一下就填进对应映射字段。

- **raw connector JSON**：直接编辑 connector 数组，支持 `standard` / `http-json` / `plugin` / `local-estimate` 四种类型

**痛点**："我这个 plan 号还剩多少额度"是国内 coding plan 用户每天要问的问题，而每家的余额接口格式都不一样。

---

### 2.14 自定义路由规则（条件式 + 脚本式）

**条件式**：来源（`request.header` / `request.body`）+ 字段路径 + 操作符 + 值。操作符包括 `==` `!=` `>` `>=` `<` `<=` `starts with` `contains` `contains deep` `not contains`。`contains deep` 会递归遍历对象和数组——用来在 `messages` / `tools` 里找内容特别有用。

命中后执行 **rewrite 列表**，操作集是：`set` / `delete` / `array-append` / `array-prepend` / `array-remove` / `array-replace`，作用于 `request.body.*` 或 `request.header.*`。

**例子**：`request.body.messages contains deep "image"` → `set request.body.model = 视觉供应商/模型`。

**脚本式**（`routing/route-script-runtime.ts`）：条件表达不了时改用 Node.js 脚本规则。这是 CCR 工程上最讲究的一块：

```
RouteScriptRuntime
  ├─ Worker 池（2–4 个），least-pending 负载均衡，每 slot 串行执行
  ├─ 资源限制：Old Gen 64MB / Young Gen 16MB / 硬超时 = timeoutMs + 250ms
  ├─ 熔断器：同一脚本 60s 内失败 3 次 → 断路 30s；脚本内容变了重新编译并重置计数
  ├─ 验证缓存：按脚本 SHA-256 缓存编译结果
  └─ 队列保护：最多 64 个 pending，满则拒绝
```

脚本是一个**异步函数体**（不是模块），注入 `input`（body / headers / method / url / model / tokenCount / sessionId / apiKeyId / summary 摘要）和 `api`：

| API | 能力 |
|---|---|
| `api.fetch(url, opts)` | HTTP 请求，body ≤ 256 KiB，响应 ≤ 1 MiB，不跟随重定向 |
| `api.fs.*` | exists / readText / readJson / list / stat / writeText / writeJson，单次 ≤ 1 MiB |
| `api.env(name)` | 读 CCR 进程环境变量 |
| `api.hash(value)` | 稳定 32 位无符号哈希，**专门用于灰度分桶** |

返回 `{ match?, model?, rewrites?, fallback? }`。异常/超时/返回值无效时 **fail-open**：记诊断，继续下一条规则。

**例子**（文档给的完整示例）：读 `x-tenant-id` header → 从本地 JSON 读租户策略 → 可选地请求远程策略服务覆盖 → 用 `api.hash(sessionId) % 100` 做稳定灰度分桶 → 返回目标模型 + 温度改写 + 备用模型链。

**值得注意的诚实**：文档明确写"Worker 隔离不是操作系统级安全沙箱……只应运行可信脚本"。

---

### 2.15 Fallback 模型

三种模式：

| 模式 | 行为 | 触发状态码 |
|---|---|---|
| `off` | 只请求一次 | — |
| `retry` | 同模型重试 N 次（0–9999） | `408` `409` `429` `5xx` |
| `model-chain` | 按顺序切换备用模型 | **任意 `4xx` 或 `5xx`** |

`model-chain` 对 4xx 也切换的理由，文档写得很清楚："模型不存在、鉴权或供应商侧拒绝等错误可能只影响当前目标"。重试前会等待：优先遵守上游正数 `Retry-After`，否则从 1 秒起指数退避，单次上限 30 秒。

**可观测性**：响应头带 `x-ccr-fallback-attempts`、`x-ccr-fallback-failures`、`x-ccr-fallback-delays-ms`、`x-ccr-fallback-model`。

Fallback 可以配全局默认，也可以每条路由规则覆盖。

---

### 2.16 Route Trace —— pipeline 变更的结构化追踪

CCR 的 16 阶段 pipeline，每个阶段对请求做的每一处改动都被记录成：

```json
{ "operation": "add", "path": "/headers/x-ccr-route-reason", "before": null, "after": "...", "scope": "headers" }
```

**JSON Pointer 风格路径**，落进 SQLite 的 `request_route_traces` 表（hops + changes 两层）。

**预算控制**（`observability/route-trace.ts`）：max 256KB trace、max 64 hops、每 hop max 64 changes、深度 max 6 层、单字符串 max 1024 字符、单值 max 2048 字节。**关键设计**：不主动 snapshot body，由 mutation 站点主动上报——所以 trace 成本正比于"实际改了多少"，而不是"请求有多大"。敏感字段名自动脱敏。

**痛点**：翻译型网关最难排查的问题是"我发的和上游收到的不一样，到底是哪一步改的"。

---

### 2.17 其余特性速览

| 特性 | 一句话说明 |
|---|---|
| **代理模式（MITM）** | CCR 起一个本地 HTTP/HTTPS 代理，装自签 CA 到系统信任库，劫持并解密 HTTPS，把识别出的模型请求接管进 CCR。可以设为系统代理让所有 App 自动经过。这是"接管一个我改不了配置的客户端"的终极手段 |
| **协议探测 + 连通性检测** | 保存 provider 前，先探测这个 base_url 支持哪些协议，再用真实（限制输出长度的）请求逐模型验证 key/模型名/协议。UI 提示"只勾要确认的模型，避免额外消耗" |
| **一键导入 deeplink** | 文档站上每个 provider 一个 `ccr://provider?name=...&base_url=...&protocol=...&models=...` 链接，点击即预填配置，确认后保存 |
| **插件系统** | 两层（Electron wrapper 插件 / core gateway 插件），11 种细粒度权限（trusted-code / apps / gateway-routes / proxy-routes / http-backends / provider-account-connectors / core-gateway-config / core-provider-plugins / virtual-model-profiles / sqlite-store / system-launcher），从 GitHub manifest 拉市场，支持 SHA-256 完整性校验 |
| **Bot / IM 接力（AgentClaw）** | 通过 Slack / Discord / Telegram / LINE / 微信 / 企微 / 飞书 / 钉钉把 agent 接力到手机。命令域收窄到 `/project` 和 `/session` 两个，其余自然语言直接进 agent。有幂等保证、outbox、去重记录 |
| **概览仪表盘** | 可拖拽编辑的组件化 dashboard，10 种指标、多种图表样式，还有 6 种"分享卡片"（AI 用量年报、路由图、模型排行榜、Token 日历海报、消费小票），导出 1080×1350 PNG —— 明显是为社交传播设计的增长功能 |
| **Cursor 兼容** | 检测 Cursor 发来的"简化版" OpenAI chat 请求（只有 user 消息、没有 system 和 tools），注入配置好的 system prompt 和 tools |
| **请求日志保留策略** | 只保留**本地当天**数据，跨天后下一次读写时清理前一天——文档明确说"适合当日排查，不适合长期审计归档" |

---

## 3. 差异矩阵

只列**有实质差异**的项。"VMR 已有"栏是核对过本仓库源码/配置的结论。

| 能力 | CCR | VMR | 差异性质 |
|---|---|---|---|
| 上游多 Key 池 + 优先级/权重/利用率/spillover | 完整 | 无（需复制 provider 条目） | **真实缺口** |
| 上游 Key 级限额（rpm/tpm/ipm…） | 有 | 无 | **真实缺口** |
| 客户端 Key 过期 + 配额 | 有 | 无（key 只做鉴权 + tag） | **真实缺口** |
| 模型元数据目录（定价/上下文/能力自动获取） | 16MB 生成式 models.json | 手写 `pricing.yaml`，context/capabilities 手写在 config | **真实缺口** |
| `/v1/models` 模型发现 | 有，且按客户端分形态 | **有**（合并形态，兼容两侧解析） | 无缺口 —— 本表原始判断有误，见 T1-3 |
| 账户余额 / 额度读取 | 有，JSONPath 可配 | 无 | **真实缺口** |
| 路由原因回传响应头 | `x-ccr-route-reason/source/routed-model` | 只有 `X-VMR-Endpoint` / `X-VMR-Attempts` | 部分缺口 |
| Failover 失败细节回传 | `x-ccr-fallback-attempts/failures/delays-ms/model` | 无 | 部分缺口 |
| 同端点重试（非换端点） | `retry` 模式 | 无（只换端点） | 部分缺口 |
| 遵守 `Retry-After` | 在 fallback 等待中遵守 | 在健康冷却中遵守（封顶 1h） | 语义不同，各有道理 |
| 4xx 触发换端点 | model-chain 模式对任意 4xx 换 | `ErrClient` 直接返回 | **理念分歧**（见 6.2） |
| 上下文超限独立处理 | 无显式处理 | 已列 P1 | **VMR 领先** |
| 内容软拦截检测 | 无 | 有观测标记，failover 已列 P1 | **VMR 领先** |
| 字节忠实透传 | 不做（16 阶段主动改写） | 核心原则 | **VMR 领先** |
| 审计完整性 | 只留当天 | 默认永久保留 + zstd（20–75×） | **VMR 领先** |
| 事后分析报告 | UI 面板 | `vmr report` 九章 + 会话/任务分组 + 工具使用分析 | **VMR 领先** |
| 请求重放 | 无（有 context archive replay，语义不同） | `vmr replay` 复用真实 BuildRequest 路径 | **VMR 领先** |
| 连通性诊断 | UI 内检测 | `vmr diagnose` 四阶段 | 相当 |
| Prompt cache 亲和 | 无显式 sticky 机制 | Sticky Model + 自愈 | **VMR 领先** |
| Agent Profile / 启动器 / 多开 | 完整 | 无 | 缺口（但属控制面） |
| 本机登录态导入 | Claude Code / Codex / ZCode / Kimi | 无 | 缺口（但属控制面） |
| 条件路由规则 | 条件式 + JS 脚本式 | 无（strategy 是编译期注册） | 理念分歧 |
| Fusion / ToolHub / Hosted Search / 媒体 | 有 | 无 | 不同赛道 |
| Context Archive | 有 | 无 | 不同赛道 |
| MITM 代理模式 | 有 | 无 | 不同赛道 |
| Bot / IM 接力 | 有 | 无 | 不同赛道 |
| Web UI / 仪表盘 / 分享卡片 | 有 | 明确不做 | 不同赛道 |
| 插件系统 / 市场 | 有 | 明确不做（只有编译期注册） | 不同赛道 |

---

## 4. 三个梯队（纯用户需求视角，暂不做原则过滤）

排序依据只有一条：**这个能力被真实用户需要的频率 × 强度 ÷ 引入成本**。

### 第一梯队 —— 强烈建议引入

#### T1-1. 上游多 Key 凭据池（含优先级、权重、冷却） — ⏸ 暂缓（2026-07-28，待决策）

> **"架构零阻力"只对了一半**：`HealthKey()` 确实已含 key 指纹（冷却天然按 key 隔离），端点展开也只是加一层循环；但 `Endpoint.Name()` **不含 key**，同 provider 同 model 挂两把 key 会得到完全相同的显示名，污染 `vmr status`、`X-VMR-Endpoint`、实时日志与 `internal/report` 的按端点聚合——这是跨模块的格式变更，不是加个循环。另有配置命名冲突待定：顶层 `api_keys` 是**客户端**鉴权 key，provider 级同名字段将是**上游**凭据，方向相反。
>
> 顺带一个本条没提到的好消息：sticky 按 `HealthKey()` 钉，天然把会话钉在同一把 key 上——而上游 prompt cache 本来就是按 key 隔离的，两者免费正确组合。

- **用户需求**：这是全表最高频的场景。国内 coding plan 通常按号限速，用户手上有 3–5 个号；OpenRouter 免费额度按 key 计；团队共享 key 池。目前 VMR 用户要么复制五份 provider 条目（配置膨胀、语义扭曲——它们其实是同一个 provider），要么放弃。
- **CCR 怎么做**：见 2.7。核心是"priority 优先 + utilization 摊平 + 80% spillover"三段式。
- **VMR 该怎么做**：把 `api_key: string` 扩成 `api_key` 与 `api_keys: []` 二选一，endpoint 展开成 N 个内部候选。**VMR 的架构对此几乎零阻力**——`Endpoint.HealthKey()` 本来就包含 API key 的 SHA-256 指纹，也就是说健康状态机早就是"按 key 隔离冷却"的；`strategy.Sort` 的稳定多键排序天然能接一个新维度。不需要 utilization（那要引入计数状态），MVP 版只做"按配置顺序 + 现有健康冷却自动跳过失效 key"就已经覆盖 80% 场景。
- **成本**：1–1.5 人天（MVP）。

#### T1-2. 模型元数据 sidecar 自动生成（`vmr pricing sync` / `vmr catalog sync`） — ⏸ 暂缓（2026-07-28，已实测否决）

> 拉了 LiteLLM 的 `model_prices_and_context_window.json`（2984 条）逐行对本项目的 `pricing.yaml` 核对：**7 条 rate 行只覆盖 3 条**，且其中 `opencode/deepseek-v4-flash` 是裸模型名撞上的假命中（填进去是 DeepSeek 官方价，恰好正确仅因为本项目手工判断过"opencode 平价转售"，换一家加价的网关会**静默填错**）。`volcengine/ark-code-latest`（套餐别名）、`opencode/*`（中转网关）、`openrouter/z-ai/glm-5.2` 全部零命中。
>
> 更根本的是数据集**没有 currency 字段**（全 USD）、**没有任何时间窗概念**——而本项目 `pricing.yaml` 的正确性恰恰建立在 CNY 基准 + `exchange_rate` 和 `hour_range` 错峰折扣上。"一个网络请求 + 一个映射函数"里，映射才是全部难点，而目标市场（中国厂商、包月套餐、中转网关）正是该数据集最薄的地方。另需承担一个第三方 URL 的长期维护义务。
>
> **更便宜的替代**：`vmr pricing skeleton -c config.yaml`，从自己的 config 生成待填骨架（真实摩擦是"我 9 个端点，哪几个还没配价"），离线零依赖；外加 report 侧提示"有用量但缺 rate 行的端点"。


- **用户需求**：`pricing.yaml` 要手写，价格会变，模型会新增。同时 `max_context_tokens` 和 `capabilities` 也是手写在 config 里的——用户为了用上 VMR 的条件路由，得先去查每个模型的上下文窗口，这是明确的上手摩擦。
- **CCR 怎么做**：见 2.9，从 LiteLLM / models.dev / OpenRouter 三个公开源合成。**关键洞察：LiteLLM 的 `model_prices_and_context_window.json` 是这个生态事实上的公共品**，一个 URL 就能拿到几千个模型的价格、上下文窗口、能力标记。
- **VMR 该怎么做**：加一个离线子命令，拉取该 JSON 生成 `pricing.yaml` 骨架（保留用户已有的自定义条目和多货币配置），并可选地生成 `max_context_tokens` / `capabilities` 建议片段供用户粘贴。**明确不进实时路径**——完全符合既有的"价目表不进实时路由决策"边界。
- **成本**：1 人天。ROI 极高：一个网络请求 + 一个映射函数，换掉一整块手工维护负担。

#### T1-3. `/v1/models` 模型发现端点 — ~~建议引入~~ **本报告有误：VMR 早已实现**

- **更正（2026-07-28 核对源码）**：`internal/server/server.go` 的 `Handler()` 一直注册着 `GET /v1/models`，实测返回合并形态，同一份 payload 同时满足 OpenAI（`object`）与 Anthropic（`type`/`has_more`）两侧解析，并带 `vmr_protocol` 区分同名跨协议模型、`owned_by: vmr`。本条的原始判断建立在对 VMR 现状的错误印象上，**成本从 0.5 人天修正为 0**。
- 教训记在这里：本报告对 VMR 现状的其余判断也应逐条回源码核对，不要直接采信。

#### T1-4. `vmr env` / `vmr run <agent>` —— 消除接入摩擦

- **用户需求**：这是 CCR 三万 star 里被低估的那一半原因。VMR 用户要接 Claude Code，得自己知道要 export `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_MODEL`；接 Codex 要写 `config.toml`；接 OpenCode 要写 JSON。每一个都是一次查文档 + 一次试错。
- **CCR 怎么做**：见 2.11，Agent Profile 生成完整的隔离配置 + 启动器。
- **VMR 该怎么做**：**不做 profile 管理，只做最小的那一层**——`vmr env claude-code` 打印可以 `eval` 的 export 语句；`vmr run claude-code -- <args>` 直接带着环境变量启动。不写任何用户配置文件（这是 CCR 那套复杂度的来源），不做多开隔离。这条能把"从下载到跑通"的时间从十几分钟压到一分钟。
- **成本**：0.5–1 人天。

#### T1-5. 静默换模型检测（CCR 没做，但由 CCR 的中转商生态延伸而来） — ✅ 采集已落地，判定暂缓（2026-07-28）

> **落地范围与本条原始设想不同，且是有意的**：不能离线做——审计有意不存成功尝试的响应体，客户端那份的 `model` 已被改回虚拟名，上游原值在响应归一化那一次替换之后就不存在了。故在 `internal/router/response.go` 的替换点前把原值读出来，写进 `attempts[].upstream_model`（仅在与请求的真实模型名不同时记录）。
>
> **只记原值、不打"不一致"标记**：版本钉死、厂商前缀、套餐别名（`ark-code-latest` → `doubao-seed-code-251015`，本项目自己的配置里就有）每次都会合法地不一致，逐请求启发式必然全是误报。区分别名与偷换只能靠聚合分布（稳定映射是别名），属于离线 report。判定与 N-6 中转商画像一起做，等有第三方中转的真实数据。
>
> 先做采集的唯一理由是**数据有时效性**：今天不记的，永远找不回来。


- **用户需求**：这条是我在读 CCR 的 provider 预设列表（大量第三方中转：AIHubmix、BurnCloud、302.AI、RunAPI、TeamoRouter、Fenno、Unity2、无限星河……）时想到的。**中转商把 `claude-opus` 的请求偷偷转给便宜模型，是这个生态里公开的秘密**，用户几乎无法察觉。
- **VMR 的独有优势**：VMR 是唯一同时具备 (a) 字节级看得见响应体、(b) 记录请求与响应两层审计、(c) 已经在做 model 字段改写 的工具。**上游响应里的 `model` 字段与请求发出的 `model` 不一致时打一个观测标记**（同 `soft_block_detected` 的形态），成本几乎为零——`response.go` 里 model 改写的正则本来就已经定位到那个字段了。
- **产出**：`vmr report` 里加一行"端点 X 有 N% 的响应返回了与请求不符的模型名"。这是**没有任何竞品能提供的能力**，且是绝佳的文章选题。
- **成本**：0.5 人天（观测版）。

#### T1-6. 路由决策原因与 failover 细节回传 — ✅ 已落地（2026-07-28）

> `X-VMR-Route-Reason`（`pick=order|sticky`、`eligible=N/M`，以及真正发生过时才出现的 `cooldown=`/`conditions=`/`ctx_fallback=`）与 `X-VMR-Failover`（`provider/model:状态` 紧凑列表，构建/网络失败记 `:err`）。实现上有一条必须遵守的次序约束，见设计文档 §4.1。

- **用户需求**：请求成功了但"为什么走了这个端点"、请求失败了但"前面几个端点各是什么错"，目前只能事后翻审计。响应头是零成本的实时反馈通道。
- **CCR 怎么做**：`x-ccr-route-reason` / `x-ccr-route-source` / `x-ccr-fallback-attempts` / `x-ccr-fallback-failures` / `x-ccr-fallback-delays-ms`。
- **VMR 该怎么做**：在已有的 `X-VMR-Endpoint` / `X-VMR-Attempts` 基础上补 `X-VMR-Route-Reason`（sticky 命中 / 条件淘汰了几个 / 排序首位）和 `X-VMR-Failover`（紧凑格式，如 `deepseek:429,minimax:5xx`）。信息本来就在 failover 循环的局部变量里。
- **成本**：0.5 人天。

#### T1-7. 客户端 Key 级配额（与已规划的 per-virtual-model 预算合并设计） — ⏸ 暂缓（2026-07-28）

> 它被设计成与一个**还不存在**的功能（战略文档 §6.2 P1 的 per-virtual-model 预算硬闸）合并；先做一半，数据结构在另一半落地时照样要重设计一次，正是本条想避免的。且触顶语义、流式请求的 token 只有跑完才知道等产品决策都未定。**本条真正值钱的建议成本为零**：预算表 key 从一开始设计成 `(client_tag, virtual_model)`，已记入战略文档。

- **用户需求**：VMR 战略文档已把 "per-virtual-model 预算硬闸" 列为 P1（防止死循环 agent 烧光额度）。CCR 的实践给出的补充信息是：**真实用户需要的维度是"按调用方"而不只是"按模型"**——一个网关同时服务本机 Claude Code、CI、和另一个同事。
- **建议**：设计预算硬闸时，key 维度用 VMR 已有的 `client_key_tag`（零新概念——它已经在驱动 `vmr-requests-<tag>.md` 分组），把预算表的 key 设计成 `(client_tag, virtual_model)` 的可选组合，而不是只有 `virtual_model`。这不增加实现成本，但避免半年后再改一次数据结构。
- **成本**：并入已规划的 P1 项，增量约 0.3 人天。

---

### 第二梯队 —— 值得做，但需要改造形态或等需求验证

#### T2-1. 账户余额 / 额度读取（`vmr balance`）

- **需求真实且强**："我这个号还剩多少"是每天要问的。但 CCR 的做法（JSONPath 映射 + UI 测试按钮）依赖 UI 交互体验，纯 CLI 下用户要手写 JSONPath 路径，摩擦大。
- **VMR 形态**：做成独立子命令 `vmr balance`，配置里 provider 可选加 `usage: {url, method, headers, map: {...}}`。**绝不进实时路径**。给几个主流 provider（智谱、Kimi、DeepSeek、OpenRouter、MiniMax）内置映射预设，用户只需 `usage: preset-zhipu`。
- **为什么在第二梯队**：价值高，但需要为每家维护映射，是一条持续义务；且 provider 的余额接口经常没有公开文档。建议等有用户明确要求再做。
- **成本**：1.5–2 人天 + 持续维护。

#### T2-2. 同端点重试（`retry` 模式）

- **需求**：VMR 目前 failover 只换端点。只配了一个端点的用户（相当常见的起步姿势）遇到瞬时 5xx / 网络抖动就直接失败了。CCR 的 `retry` 模式 + `Retry-After` + 1s 起指数退避（单次上限 30s）就是为这个场景准备的。
- **VMR 形态**：虚拟模型级可选 `retry: {count, max_delay}`，在 failover 循环走完所有端点之后（或者对单端点情况直接）做有限重试。注意与现有健康冷却的语义不能打架——重试期间不应把端点推进更深的退避。
- **为什么不在第一梯队**：多端点用户（VMR 的目标画像）已经被 failover 覆盖，边际收益递减。
- **成本**：1 人天，但需要仔细设计与 health 状态机的交互。

#### T2-3. 极简条件路由（收窄版）

- **需求**：CCR 的条件规则最高频的两个用法其实很朴素：(a) 按 client header / key 分流到不同虚拟模型；(b) 按原始 model 名前缀分流。剩下的 `contains deep` / JS 脚本 / 灰度分桶属于长尾。
- **VMR 形态**：**绝不引入表达式 DSL**。可以做成一个极窄的映射表——虚拟模型下可选 `aliases: [claude-3-5-sonnet, claude-sonnet-4-*]`，让客户端发来的真实模型名自动映射到虚拟模型。这解决的是"客户端写死了模型名、我改不了"这个真问题（Claude Code 的 `ANTHROPIC_DEFAULT_*_MODEL` 别名、Cursor 的固定模型名），而不引入任何条件逻辑。
- **成本**：0.5 人天（别名版）；条件版不建议做。

#### T2-4. 协议探测（`vmr diagnose` 增强）

- **需求**：用户拿到一个第三方中转的 base_url，不知道它支持 OpenAI 还是 Anthropic 还是都支持。CCR 在保存前做探测。
- **VMR 形态**：`vmr diagnose` 已经有真实请求 phase，扩展成"对给定 base_url 同时试 `/chat/completions` 和 `/messages`，报告哪个可用"。可以做成 `vmr probe <base_url> <key>` 这样的独立调试命令。
- **成本**：0.5 人天。

#### T2-5. Subagent 模型标签路由（VMR 变体）

- **需求**：让派生的 subagent 用便宜模型，成本可能降一个数量级。这是真实的省钱杠杆。
- **CCR 的做法为什么妙**：不去猜，让模型自己声明（2.5）。
- **VMR 的困难**：VMR 不做 prompt 注入（那是主动改请求体，且要维护"模型 Description"这个新概念）。**但 VMR 可以做被动版**：Claude Code 的 Task 派生请求带 `x-claude-code-agent-id` header——VMR 可以把它作为 sticky key 的一部分，或者作为一个可选的条件（"派生请求走另一个虚拟模型"）。这依赖客户端行为，脆弱，需要先验证 header 是否稳定存在。
- **建议**：先在审计里加一个观测——统计带该 header 的请求占比和 token 分布，用数据判断这个杠杆到底值多少钱，再决定要不要做。**观测先行，这本身就是 VMR 的方法论。**
- **成本**：观测版 0.3 人天。

#### T2-6. 归一化步骤的结构化表达（借鉴 Route Trace 的形式）

- **需求**：VMR 审计里的 `norm` 是一个字符串列表。CCR 的 `{operation, path, before, after, scope}` 结构（JSON Pointer 路径）表达力更强，且"成本正比于实际改动量而非请求大小"的预算设计非常对 VMR 的胃口。
- **VMR 形态**：VMR 的改动面本来就极小（model 改写 + 两个 quirk），把 `norm` 从字符串升级成结构化对象的收益有限。但**`before/after` 的记录值得加**——目前审计只说"做了 model 改写"，没说"从什么改成什么"。
- **成本**：0.5 人天，但要改审计 schema（跨包变更，需同步 `internal/report`）。

---

### 第三梯队 —— 不推荐引入

按"为什么不推荐"分组，每组给出理由，避免以后重复论证。

**A. 会把 VMR 变成 agent runtime**
| 项 | 不推荐理由 |
|---|---|
| Fusion 组合模型 | 网关内跑无上限的工具循环 + 管理 MCP 子进程。这是一个完整的 agent 执行引擎，与"路由器"是两个产品。而且它必须解析、重构请求体，byte-faithful 彻底不存在 |
| ToolHub | 同上，还额外引入一个"检索模型"的调用链和成本。且它服务的是 MCP 生态，不在 LLM API 路径上 |
| Hosted Web Search 协议桥 | 概念很妙，但实现代价是每协议 × 每传输模式一套响应重编码（CCR 光这个就 1252 行）。VMR 一旦开这个口子，"能力垫片"会无限增殖 |
| 媒体生成工具 | 完全不同的产品域 |
| Context Archive | 概念上最有价值的一个（对抗 compaction 丢信息），但要求网关持久化全部请求体、注入 MCP 工具、在响应里追加 footer。三条都直接违反 VMR 的核心边界。**唯一可以借的是它的问题意识**——见第 5 章 N-4 |

**B. 属于桌面/控制面产品，不属于 CLI 工具**
| 项 | 不推荐理由 |
|---|---|
| Agent Profile 全套（隔离配置/多开/App 数据目录） | 这是 Electron 应用的价值主张。VMR 只取最小的 `vmr env` / `vmr run`（见 T1-4） |
| 内置浏览器自动化 + Chrome 登录态导入 | 需要 Electron |
| Bot / IM 接力（AgentClaw） | 完全不同的产品，且要维护 8 个平台 SDK |
| Web UI / 仪表盘 / 分享卡片 | 战略文档已明确不做。分享卡片作为增长手段有意思，但 VMR 的等价物是 `vmr report` 的 Markdown 输出——本来就可以直接贴出来 |
| MITM 代理模式 + CA 安装 | 要求装系统根证书，信任模型上不可接受，且是桌面场景专属 |
| 插件系统 / 市场 | 战略文档已明确不做，只保留编译期注册 |

**C. 与 VMR 已有设计冲突且 VMR 的选择更好**
| 项 | 不推荐理由 |
|---|---|
| Node.js 脚本路由规则 | 等于给 VMR 引入一个 JS 运行时。CCR 自己都在文档里承认"Worker 隔离不是操作系统级安全沙箱"。VMR 的扩展方式是编译期注册的 `Condition` / `Dimension` |
| 条件表达式规则引擎（完整版） | 战略文档明确边界。收窄版见 T2-3 |
| SQLite 配置存储 | 与 YAML + 单二进制 + 可 diff 可 git 的配置形态冲突。CCR 自己的文档都要专门写一页"不要在运行时直接编辑 config.sqlite" |
| 请求日志只留当天 | CCR 的这个选择对 VMR 是**反面教材**：VMR 的审计日志是成本核算和事后取证的唯一数据源，默认永久保留 + zstd 压缩是更好的设计 |
| model-chain 对任意 4xx 换端点 | 见 6.2 的讨论——这与 VMR 的 `ErrClient` 语义分歧，两边各有道理，不应盲目跟随 |
| Codex apply_patch 桥接 | 主动改写工具定义和工具调用结果，是最彻底的 byte-faithful 违反。而且它是 Codex 专属的一次性适配 |
| Cursor 简化请求补全 | 同上，且需要用户配置 system prompt / tools 内容，等于让网关持有 prompt |

---

## 5. 延伸：CCR 没做、VMR 值得探索的新特性

这一节是本报告的重点之一。上面每个 CCR 特性背后都有一个"用户真正的困扰"，把困扰抽出来重新问一遍"VMR 的独特位置能怎么解"，得到的答案往往比直接抄更好。

### N-1. Prompt Cache 命中率报告（VMR 独有杠杆） — ✅ 已落地（2026-07-28）

> **本条 80% 早已存在**：`Usage.CacheRead/CacheWrite` 一直在提取，缓存效率一直在 Row/EndpointRow/ClientRow/WorkloadRow 各表里渲染。真正缺的只有本条点名的那一项——sticky 命中 vs 未命中的对比，落地为 report §6.5。按**结果**（同一会话内端点连续性）而非按机制度量：不需要新增审计字段，且 sticky 指针命中却落到冷端点照样算切换，这更诚实。任一组带 usage 样本 < 20 条时只出表不下结论；不解释切换原因（TTL 到期/冷却/条件淘汰/没开 sticky 事后不可区分）。

- **困扰来源**：CCR 完全没有 sticky/亲和机制，说明这个问题它还没意识到。而 VMR 已经做了 Sticky Model——但**没有任何方式证明它有效**。
- **想法**：上游返回的 usage 里普遍带 `cache_creation_input_tokens` / `cache_read_input_tokens`（Anthropic）或 `prompt_tokens_details.cached_tokens`（OpenAI）。VMR 的审计已经在记响应体。在 `vmr report` 加一节：**按虚拟模型统计 cache 命中率、按端点统计、以及"sticky 命中 vs sticky 未命中"两组的 cache 率对比**。
- **为什么值得**：(a) 它把一个已有特性从"相信它有用"变成"看得见它省了多少钱"；(b) prompt cache 通常能省 70–90% 的输入成本，这是当前 agent 场景最大的单项成本杠杆；(c) 没有任何竞品在做这件事的量化；(d) 这是一篇文章的完整素材。
- **成本**：1–1.5 人天（纯 report 侧，零路由路径改动）。

### N-2. 端点成本效率排行（不只是花了多少，而是"值不值"） — ✅ 已落地（2026-07-28）

> report §6.6。一处口径修正：**失败尝试的"成本"不可测**——report 只从客户端真正收到的响应提取 usage，失败尝试没有，厂商通常也不对失败请求计费。故浪费一律表述为**墙钟时间**（`EndpointRow.WastedMS`），不折算成钱；成本维度是成本/1M out token 与成本/成功请求，全部由 §2/§3 已有字段在渲染期派生。

- **困扰来源**：CCR 的仪表盘能显示成本，但只有绝对值。用户真正想知道的是"我这三个端点，哪个更划算"。
- **想法**：`vmr report` 已经有成本、延迟、成功率、token。合成两个派生指标：**每千输出 token 的实际成本**（含失败重试的浪费）和 **每次成功请求的端到端成本**（把 failover 中失败尝试的成本摊给最终成功的那次）。后者是关键——一个便宜但经常失败的端点，真实成本可能比贵端点还高，**这个数字目前没有任何工具能给出**。
- **为什么值得**：这是 VMR 的审计结构（两层记录 + 每次 attempt 一条）独有能产出的洞察。它把"路由器"变成"选型顾问"。
- **成本**：1 人天。

### N-3. `vmr bench` —— 同一 prompt 打多个端点做对比

- **困扰来源**：CCR 的"连通性检测"只回答"能不能用"。用户真正的问题是"这三个 provider 跑我的活儿谁更好/更快/更便宜"。
- **想法**：复用 `vmr replay` 的基础设施（它已经能从审计记录重建真实请求），加一个 `vmr bench -detail <file> -endpoints a,b,c`：同一个历史请求并发打到多个端点，输出对比表（TTFT / 总耗时 / 输出 token / 成本 / 响应文本 diff）。
- **为什么值得**：(a) 它把"我该选哪个 provider"这个决策从玄学变成数据；(b) 用的是**用户自己的真实请求**，比任何公开 benchmark 都有说服力；(c) 顺带就是最好的 demo（一张对比表截图）；(d) 战略文档里已有的 "replay 响应 diff 视图"（P2）是这个的子集，合并做更划算。
- **成本**：1.5 人天。

### N-4. Compaction 感知与告警（Context Archive 的观测版）

- **困扰来源**：CCR 花了 1600+ 行做 Context Archive 来"修复"compaction 丢信息。但更前置的问题是：**用户根本不知道自己的会话被 compact 了几次、每次丢了多少。**
- **想法**：VMR 的 report 已经在做 compaction 双向链接（会话分析里有）。往前一步：报告里给出"本次任务发生了 N 次 compaction，每次前后上下文从 X token 降到 Y token"，以及"哪些虚拟模型的会话最容易触发 compaction"。再往前一步：请求进来时估算 token 已接近该端点 `max_context_tokens` 的 80% 时打一个观测标记 / 加一个响应头。
- **为什么值得**：VMR 不需要（也不应该）去修复 compaction，但它可以**成为唯一能告诉你 compaction 发生了什么的工具**。这完全在"黑匣子"定位内，且是 CCR 的重投入特性的低成本替代路径。
- **成本**：0.8 人天。

### N-5. 端点额度重置日历（从审计反推）

- **困扰来源**：CCR 的账户余额读取依赖 provider 提供接口。但绝大多数国内 plan 号**根本没有余额接口**。
- **想法**：VMR 的审计里有每次 429 / 额度耗尽的精确时间戳。**从历史 429 的时间分布反推每个端点的额度窗口和重置时刻**（比如"这个端点的 429 集中在每天 22:00–24:00，重置时刻推测为 00:00"）。在 report 里出一节"端点额度节律"。
- **为什么值得**：这是"没有 API 也能拿到信息"的典型，纯离线分析，零路由改动，且解决的是一个 CCR 的方案完全解不了的场景（无接口的 plan 号）。
- **风险**：推断可能不准。必须表述为"观测到的模式"而非"确定的额度"。
- **成本**：1 人天。

### N-6. 中转商画像（延伸自 T1-5）

- **想法**：在静默换模型检测（T1-5）之上，把 report 里的端点分析扩成一张"中转商体检表"：模型名一致性、响应结构与官方的偏差（比如缺 `usage` 字段、缺 `cache_read` 字段、SSE 事件类型不全）、延迟分布、成功率。
- **为什么值得**：CCR 的 provider 列表里挂了十几家中转商（还都是赞助商），说明这个市场很大且鱼龙混杂。**"帮你验货"是一个没人占的位置**，而 VMR 的字节级可见性是天然的验货工具。这可能是 VMR 最好的传播切入点。
- **成本**：1.5 人天（在 report 里做，零路由改动）。

### N-7. 配置 preset 库（对标一键导入 deeplink）

- **想法**：CCR 的 `ccr://provider?...` deeplink 依赖桌面应用。VMR 的等价物更简单：仓库里维护一个 `presets/` 目录，每家 provider 一个 YAML 片段（base_url + 协议 + 常见模型名）；`vmr init --preset deepseek,openrouter` 生成起步 config。
- **为什么值得**：把"第一次配置"的时间再砍一半。而且 preset 文件是社区最容易贡献 PR 的东西——**这是一个天然的 contribution 入口**，对开源项目的活跃度有独立价值。
- **成本**：0.5 人天 + 内容维护。

### N-8. 无人值守"异常模式"告警摘要

- **想法**：`vmr report` 目前是"你问我答"。加一个 `vmr report -alerts`：只输出反常项——成功率突然下降的端点、成本比昨天高 3 倍的虚拟模型、连续失败但还没进冷却的端点、静默换模型的端点、软拦截率上升的端点。可以配合 `vmr.sh` 做成每日一封的本地摘要。
- **为什么值得**：无人值守是 VMR 的核心场景，但"无人值守"的反面是"没人看报告"。把报告从"全量"改成"只说异常"，才真正闭环。
- **成本**：1 人天。

---

## 6. 最后一层筛子：原则冲突复核

前面五章刻意不带原则判断。现在做这一步。

### 6.1 原则冲突评级

| 项 | byte-faithful | 单二进制/零依赖 | 无状态/无 DB | 不做规则引擎 | 审计可自证 | 结论 |
|---|---|---|---|---|---|---|
| T1-1 多 Key 池 | 无冲突 | 无冲突 | 无冲突 | 无冲突 | 可（key 指纹已在审计里） | ⏸ 待决策（Name() 格式 + 配置命名） |
| T1-2 元数据 sidecar | 无冲突 | 无冲突（离线子命令） | 无冲突 | 无冲突 | 不适用 | ⏸ 实测覆盖率不足，否决 |
| T1-3 `/v1/models` | — | — | — | — | — | **已存在，无需做** |
| T1-4 `vmr env`/`run` | 无冲突 | 无冲突 | 无冲突 | 无冲突 | 不适用 | **做** |
| T1-5 静默换模型检测 | 无冲突（纯观测） | 无冲突 | 无冲突 | 无冲突 | 是（观测值进审计） | ✅ 采集已落地，判定并入 N-6 |
| T1-6 路由原因回传 | 轻微（加响应头） | 无冲突 | 无冲突 | 无冲突 | 可 | ✅ 已落地 |
| T1-7 Key 级配额 | 无冲突 | 无冲突 | **有**：需内存计数器 | 无冲突 | 可 | **做**，但接受"重启即重置"的弱语义（已有先例：健康注册表） |
| T2-1 `vmr balance` | 无冲突 | 无冲突 | 无冲突 | **轻微**：JSONPath 映射接近一门小 DSL | 不适用 | 等需求；若做，用内置 preset 而非通用映射 |
| T2-2 同端点重试 | 无冲突 | 无冲突 | 无冲突 | 无冲突 | 可 | 等需求 |
| T2-3 模型别名 | 无冲突 | 无冲突 | 无冲突 | 无冲突（纯映射表） | 可 | 可做 |
| T2-5 subagent 路由 | **有**（若做 prompt 注入） | — | — | — | — | 只做观测版 |
| N-1 cache 命中率 | 无冲突 | 无冲突 | 无冲突 | 无冲突 | 是 | ✅ 已落地（report §6.5） |
| N-2/N-3/N-4/N-5/N-6/N-8 | 全部无冲突（report/离线侧） | 无冲突 | 无冲突 | 无冲突 | 是 | **做**（按优先级排期） |

**一个值得注意的结果**：第一梯队 7 项中有 6 项与 VMR 的核心原则**零冲突**。这不是巧合——它说明 VMR 之前的缺口不是"因为坚持原则而放弃的能力"，而只是**还没顾上**。原则并没有真正约束到这批特性。真正被原则挡住的东西全都落在第三梯队（Fusion、ToolHub、脚本路由、UI、插件），而那些恰好也是"换赛道"的东西。

### 6.2 一个需要单独讨论的理念分歧：4xx 该不该换端点

CCR 的 `model-chain` 模式对**任意 4xx** 都切换备用模型，理由是"模型不存在、鉴权或供应商侧拒绝可能只影响当前目标"。VMR 的 `ErrClient` 则直接返回客户端，理由是"每个端点都会以同样方式拒绝"。

**两边都对，但对的是不同的场景**：
- VMR 的假设成立于"端点是同一个模型的多个来源"——参数非法就是参数非法。
- CCR 的假设成立于"备用模型是**不同的模型**"——A 模型不支持 `tools` 参数，B 模型支持。

VMR 的候选集里其实两种情况都有（`endpoints` 下可以是不同的 `model` 值）。**建议**：不改默认行为（VMR 的选择更安全，避免把一个必然失败的请求打给五个端点），但可以在审计里加一条观测——统计"以 `ErrClient` 结束、且候选队列里还有未尝试端点"的请求数。如果这个数字长期为零，说明 VMR 的假设成立，不必再讨论；如果显著非零，再看那些请求的具体错误内容。**又是观测先行。**

### 6.3 建议的排期顺序

综合价值、成本和相互依赖：

（2026-07-28 状态：T1-3 本就存在=0 成本；T1-5 采集部分、T1-6、N-1、N-2 已落地；T1-2 实测后否决；T1-1/T1-7 暂缓待决策。）

```
第一批（约 3 人天，全部零原则冲突，且集中产出 demo 素材）
  T1-3  /v1/models 模型发现          0.5d
  T1-4  vmr env / vmr run            0.5–1d
  T1-5  静默换模型检测（观测）        0.5d
  T1-6  路由原因 + failover 回传      0.5d
  T1-2  元数据 sidecar 生成           1d

第二批（约 3 人天，实质能力补齐）
  T1-1  多 Key 凭据池                1–1.5d
  T1-7  Key 级配额（并入已规划 P1）   +0.3d
  N-1   prompt cache 命中率报告       1–1.5d

第三批（约 4 人天，把"黑匣子"定位坐实）
  N-2   端点成本效率排行              1d
  N-3   vmr bench                     1.5d
  N-6   中转商画像                    1.5d
```

注意第一批的五项加起来约 3 人天，但它们共同解决的是**同一个问题：从下载到跑通到看懂的全链路摩擦**。这直接服务于战略文档里 §5"分发压倒特性"的判断——它们不是特性，是分发基础设施。

---

## 7. 其他观察与建议

以下是你没问、但我认为值得记下来的判断。

### 7.1 CCR 的真正护城河不在网关，而在"最后一公里"

CCR 的模型转发核心其实是外部依赖（`@the-next-ai/ai-gateway`）。它自己写的 84k 行 TypeScript 里，绝大部分在做：管 agent 进程、写 agent 配置文件、隔离 HOME、导入登录态、跑 MCP、开浏览器、接 IM。

**这意味着 CCR 和 VMR 的竞争其实不激烈**——它们在争夺同一批用户，但用不同的东西留住他们。CCR 留住的是"不想折腾"的用户，VMR 应该留住的是"出了事要能查清楚"的用户。这两拨人的重叠度可能没有想象中高，而且**同一个人在不同阶段会是不同的人**：刚开始都是"不想折腾"，跑砸一次之后就变成"要能查清楚"。

**推论**：VMR 的内容策略不应该是"我比 CCR 轻量"，而应该是"当 CCR 帮你跑起来之后，你需要 VMR 来搞清楚它到底做了什么"。甚至可以直说：**VMR 可以挂在 CCR 后面用**（CCR 的 provider 指向 VMR），这不荒谬——CCR 负责 agent 接入，VMR 负责路由和取证。这是一个被忽略的分发渠道。

### 7.2 CCR 的分享卡片值得单独学一下（不是学功能，是学意图）

CCR 做了六种可导出 PNG 的分享卡片（AI 用量年报、路由图、模型排行榜、Token 日历海报、消费小票），1080×1350 的竖版海报比例——这是明摆着为小红书/朋友圈/Twitter 设计的。**它把"用量数据"变成了社交货币。**

VMR 不该做 PNG 导出，但可以做一件成本几乎为零的事：**让 `vmr report` 的 Markdown 摘要（§0）本身就是可直接贴出来的**。目前的报告是给自己看的，可以再加一个 `vmr report -share` 模式，输出一段脱敏的、格式漂亮的、可直接发到社区的摘要（"过去 30 天：12,400 次请求，3 个 provider，failover 触发 87 次，节省 $23，最贵的一次任务 $1.40"）。用户自发晒出来就是分发。

### 7.3 关于"能力垫片"这个问题类别

CCR 的 Hosted Web Search（2.2）和 Codex apply_patch 桥（2.6）本质是同一个问题：**客户端假设它在跟 A 说话，实际在跟 B 说话，A 和 B 的能力集不一样。**

VMR 的 `Condition` 机制（image / tools capability）其实是这个问题的**另一种解法**：不做垫片，而是**不把请求路由到不具备该能力的端点**。这个解法更干净、成本低一个数量级，而且完全在 byte-faithful 内。

**建议**：把这一点明确写进设计文档和对外材料。"我们不给模型打补丁，我们只把请求送到本来就能干这活的模型那儿"——这是一个清晰、可辩护、且实际上更可靠的立场。同时这提示了一个 Condition 的扩展方向：把 `capabilities` 从 `image` / `tools` 扩到 `web_search` / `computer_use` / `pdf` 等客户端会声明的服务端工具，请求侧检测这些声明（都是请求体顶层的确定性结构，`TopLevelProbe` 一次扫描就能拿到），淘汰掉不支持的端点。**这可能是 VMR 最自然的下一个 Condition。**

### 7.4 关于观测标记这个方法论

VMR 已有的 `soft_block_detected` / `thinking_process_pattern_detected` / `crlf_framing_suspected` 是一个非常好的模式：**先把频率变成数字，再决定要不要处理。**

本报告里我反复用了这个模式来处理不确定的候选（T2-5 subagent 观测版、6.2 的 ErrClient 观测、N-4 compaction 观测、N-5 额度节律推断）。**建议把它显式写成一条项目方法论**，放进设计文档：

> 任何"看起来可能有价值但不确定"的特性，第一版永远是一个观测标记，不改任何字节、不改任何路由行为。等审计数据能回答"这个情况一个月发生几次"之后，再决定要不要做真的处理。

这条方法论本身就是 VMR 相对所有竞品的结构性优势——**因为只有 VMR 有那份能回答问题的审计数据**。CCR 想这么干也干不了，它的请求日志只留当天。

### 7.5 一个风险提示

多 Key 凭据池（T1-1）虽然架构上零阻力，但它会引入一个隐性问题：**审计和报告里的"端点"这个概念会从"provider+model"变成"provider+model+key"**。`vmr report` 的所有分组维度、`internal/report` 与 `audit.Record` 的编译期耦合、以及 `HealthKey()` 的语义都会受影响。虽然 `HealthKey` 本来就含 key 指纹（所以健康侧没问题），但**报告侧的展示需要决定：是把同 provider 的多个 key 合并展示，还是分开**。

建议：**默认合并展示（用户关心的是"DeepSeek 这个端点怎么样"），只在专门的一节里按 key 拆开**（用户排查"哪个号被限了"时才需要）。这个决定要在动手前定下来，否则会返工。

---

*报告完成于 2026-07-28。CCR 侧结论均以 v3.0.16 源码与官方中文文档为准；涉及 `@the-next-ai/ai-gateway` 内部实现的部分（跨协议转换、Fusion 工具循环的图像传递细节）为基于 `package.json` 依赖与 core-runtime 结构的推断，已在正文标注。*
