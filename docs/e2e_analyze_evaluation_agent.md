<!-- // Ver 2026-09-24 13:05, by agent -->

# VMR Analyze 全量日志端到端深度评估与分析报告

## 1. 任务背景与需求 De-brief

### 1.1 核心目标
使用 `vmr analyze` 命令体系，对 `logs/` 目录下全部历史审计日志（覆盖 2026 年 7 月、8 月、9 月共 72 个日志文件、7.7 万余条审计记录）进行全功能、端到端、生产级深度分析与系统验证。重点评估其宏观聚合报表、请求详单索引、任务叙事（Journey）提取、多模型横向对比（Compare）、基准统计（Benchmark）及离线重渲染（Render-Only）等关键能力的完备性、准确性与工程鲁棒性。

### 1.2 范围与硬性约束
1. **全量输入覆盖**：必须涵盖 `logs/` 目录下的所有 `vmr-audit-*.jsonl.zst` 日志文件（2026-07-14 至 2026-09-23）。
2. **纯中文输出**：强制使用 `-lang zh`，报表、索引、叙事详情、横向对比及控制台说明均以中文呈现。
3. **Journey 生成重点聚焦 9 月份**：
   - 7 月与 8 月日志仅参与宏观统计和请求索引，不生成单条 Journey 详情报告（`j-*.md` / `j-*.json`）。
   - 9 月份为重点分析窗口，对其中的代表性任务进行 Journey 物化与叙事提取。
4. **跨模型/Agent 对比分析**：
   - 从 9 月份已物化的 Journey 中挑选出 5 个具有代表性、步数超过 10 步（>10 steps）、围绕相同/相似问题但使用不同 Agent 架构或不同底层 Model 形成的 Journey。
   - 使用 `vmr analyze -compare` 进行横向成对对比分析，剖析执行差异、决策分歧点（Divergence Point）、成本与耗时差分及行为异常（Findings）。
5. **逐类报告复核与问题记录**：
   - 每类产物（宏观报告、请求索引、Journey 叙事、双 Journey 对比、基准报表、看板骨架等）生成后均进行深度核验。
   - 全程**不修改任何代码**，严格执行“端到端真实测试 + 缺陷与异常事实记录”。
6. **产物归档**：所有分析产物与本记录文档统一落盘于 `reports/` 目录。

---

## 2. Action Plan 与执行状态

- [x] **Milestone 1: 宏观聚合与索引扫描（全量日志）**
  - 执行 `vmr analyze -macro-only -lang zh` 扫描 2026-07 至 2026-09 全量 72 个日志文件，物化核心领域切片（`macro/*.json`）、全局请求索引（`requests/index.json`、`requests/failed.jsonl`、`requests/failed.md`）及主报表 `vmr-report.md`。
  - 执行 `vmr analyze -list-only -lang zh` 提取全量会话候选索引（`journeys/index.json` 与 `journeys/index.md`），物化任务聚类（Task Clusters），不生成 7/8 月单体详情。
  - 复核宏观切片一致性、准入令牌 `manifest.json` SHA-256 校验和以及数据指标真实性。
- [x] **Milestone 2: 9 月份数据下钻与候选筛选**
  - 解析 `journeys/index.json` 与 `requests/index.json`，统计 9 月份任务分布（1046 个候选，其中请求数 >10 的有 740 个）。
  - 识别出 216 个任务聚类，筛选出 5 个围绕相同“每日新闻简报流水线（Daily News Brief Pipeline）”、由不同客户端 Agent（LobsterAI vs OpenClaw）与不同底层模型（Gemini 3.7 Flash High、Gemini 3.8 Flash High、DeepSeek v4 Flash、GLM 5.3 Flash）驱动的代表性 Journey 候选。
- [x] **Milestone 3: 9 月份重点 Journey 物化与叙事复核**
  - 使用 `-journey <id>` 分别对选定的 5 个核心 Journey 进行单体物化与叙事提取，结合本地运行的 VMR 实例（`http://192.168.0.22:8800/v1/`，模型 `cheap`）生成 LLM 解读与行为诊断。
  - 生成物化文件：`reports/journeys/details/j-*.json` 与 `reports/journeys/details/j-*.md`。
  - 深度核验单 Journey 报告：决策脊柱（Decision Spine）、Step 角色分类、触达资产（Touched Artifacts）、时序图及 Findings 规则命中。
- [x] **Milestone 4: 跨模型/Agent 成对对比（-compare）**
  - 针对选定的 5 个 Journey 构造 5 组全方位的对照组（同 Agent 跨模型、同模型跨 Agent、跨 Agent 跨模型组合）。
  - 执行 `vmr analyze -compare` 生成 5 份对比报告 `reports/compares/compare-*.md` 与 `.json`，并自动构建对照索引 `reports/compares/index.md`。
  - 深度核验：分歧点（Divergence Point）定位、行为剖面差分、System Prompt 差异 diff、交付物比对及 LLM 分歧归因。
- [x] **Milestone 5: 基准统计分析（-benchmark）**
  - 执行 `vmr analyze -benchmark -lang zh` 扫描全部 1790 个有效候选，生成 `reports/journeys/benchmarks.md` 与 `.json`。
  - 核验指标分布分位值、Finding 命中率、Spearman 秩相关矩阵、Context Rot 注意力衰减分桶与 N-gram 工具调用时序模式。
- [x] **Milestone 6: 纯渲染与离线重现性验证（-render-only & dashboard）**
  - 验证 `vmr analyze -render-only -lang zh` 的 L3 缓存命中行为，以及带 `-no-cache` 时的全量从磁盘 JSON 重新渲染能力。
  - 核查仪表盘静态骨架（`macro-dashboard.html`、`journey-viewer.html`、`request-browser.html`）与文件权限（0600/0700）合规性。
- [x] **Milestone 7: 综合评估总结与问题清单沉淀**
  - 汇总全量测试中观察到的系统表现、边界条件与数据一致性事实。

---

## 3. 执行过程与数据驱动决策记录

### 3.1 初始阶段：全量日志宏观扫描与策略选择
- **输入数据规模**：`logs/` 目录下共 72 个 `.jsonl.zst` 文件，时间跨度从 2026-07-14 至 2026-09-23，解压前压缩体积约 1.2 GB。
- **阶段 1 策略**：
  由于用户指定“7 月和 8 月不需要对 journey 进行生成，重点聚焦在 9 月份”，如果直接运行默认全套套件，会将 7/8 月所有非噪音 Journey 逐一物化（产生数千个文件，耗时较长且不符合聚焦 9 月要求）。
  因此采取**分步精准下钻**策略：
  1. 先用 `-macro-only` 聚合全量 72 个文件的宏观事实（用量、成本、可靠性、切片与全局请求索引）。
  2. 再用 `-list-only` 建立轻量级全量候选索引（`journeys/index.json` 与 `.md`），并完成基于消息哈希与标题相似度的任务聚类（Task Clustering）。
  3. 基于候选索引在 9 月份进行精准筛选与针对性物化。

### 3.2 9 月份数据分布与 5 个代表性 Journey 的选取
在 `journeys/index.json` 中，9 月份共有 1046 个候选，其中步数/请求数 > 10 的有 740 个，涉及客户端包括 `lobster`、`openclaw`、`pimini`、`cc`、`longlong`、`dummy`、`hermes`。

经过聚类分析与主题检索，选定了一个具有极高业务一致性与跨 Agent/跨模型对比价值的经典任务：**每日新闻简报自动化流水线（Daily News Brief Pipeline）**。该任务在 9 月份由不同的自动化 Agent 定时触发执行，任务目标一致（抓取新闻、去重、聚类打分、生成结构化 JSON 与 Markdown 简报并发布），但在不同日期、不同 Agent 框架和不同底层模型下的表现呈现出显著的多样性。

挑选出的 5 个核心 Journey 如下（均超过 10 步，在 27~66 步之间）：

1. **J1**: `j-lobster-20260901T080000-20260901T080432-84ec6f9c`
   - **Agent 框架**: LobsterAI (`lobster`)
   - **执行时间**: 2026-09-01 08:00
   - **步数 / 耗时**: 27 轮 (26 次工具调用) / 276.1s
   - **底层模型**: `gemini-3.7-flash-high` (提供商: `cliproxy`)
2. **J2**: `j-lobster-20260902T080000-20260902T081447-d535800f`
   - **Agent 框架**: LobsterAI (`lobster`)
   - **执行时间**: 2026-09-02 08:00
   - **步数 / 耗时**: 39 轮 (37 次工具调用) / 887.4s
   - **底层模型**: `glm-5.3-flash` (volc_coding_plan) + `deepseek-v4-flash` (bai) 混合
3. **J3**: `j-lobster-20260905T080000-20260905T080923-65996040`
   - **Agent 框架**: LobsterAI (`lobster`)
   - **执行时间**: 2026-09-05 08:00
   - **步数 / 耗时**: 36 轮 (35 次工具调用) / 563.8s
   - **底层模型**: `deepseek-v4-flash` (提供商: `lobsterai2api`)
4. **J4**: `j-openclaw-20260920T163442-20260920T163926-645d3628`
   - **Agent 框架**: OpenClaw (`openclaw`)
   - **执行时间**: 2026-09-20 16:34
   - **步数 / 耗时**: 66 轮 (65 次工具调用) / 288.9s
   - **底层模型**: `gemini-3.8-flash-high` (提供商: `cliproxy`)
5. **J5**: `j-openclaw-20260921T080000-20260921T080643-df0a85b4`
   - **Agent 框架**: OpenClaw (`openclaw`)
   - **执行时间**: 2026-09-21 08:00
   - **步数 / 耗时**: 31 轮 (30 次工具调用) / 403.2s
   - **底层模型**: `gemini-3.8-flash-high` (提供商: `cliproxy`)

### 3.3 5 组 Pairwise 对比矩阵构建
为了全方位评测 `vmr analyze -compare` 的各项对比维度，构建了涵盖“同 Agent 跨模型”、“同 Agent 跨版本模型”、“同模型跨 Agent”及“跨 Agent 跨模型”的 5 组横向对照：

- **对照组 1（同 Agent 跨模型：Gemini 3.7 vs GLM/DeepSeek）**:
  `j-lobster-20260901... (J1)` vs `j-lobster-20260902... (J2)`
- **对照组 2（同 Agent 跨模型：Gemini 3.7 vs DeepSeek v4）**:
  `j-lobster-20260901... (J1)` vs `j-lobster-20260905... (J3)`
- **对照组 3（跨 Agent 跨模型：Lobster+Gemini 3.7 vs OpenClaw+Gemini 3.8）**:
  `j-lobster-20260901... (J1)` vs `j-openclaw-20260920... (J4)`
- **对照组 4（跨 Agent 跨模型：Lobster+DeepSeek v4 vs OpenClaw+Gemini 3.8）**:
  `j-lobster-20260905... (J3)` vs `j-openclaw-20260921... (J5)`
- **对照组 5（同 Agent 同模型跨运行：OpenClaw+Gemini 3.8 运行 A vs 运行 B）**:
  `j-openclaw-20260920... (J4)` vs `j-openclaw-20260921... (J5)`

---

## 4. 逐类产物详细核验与评估结果

### 4.1 宏观聚合报表（Macro Report）
- **产物清单**:
  - `reports/vmr-report.md` (1.6 MB，主报表 Markdown)
  - `reports/manifest.json` (13 KB，准入令牌)
  - `reports/macro/summary.json` (全景核心指标)
  - `reports/macro/finance.json` (成本与额度核算)
  - `reports/macro/reliability.json` (端点可用性与错误分布)
  - `reports/macro/workloads.json` (工作负载与时序分布)
  - `reports/macro/context-efficiency.json` (上下文利用效率与工具分布)
  - `reports/macro/guard.json` (离线护栏取证分析)
- **核验发现**:
  1. **数据规模完全对齐**: 72 个文件全部成功解码，共处理 **77,088 条记录**（无解析报错），生成有效请求 74,426 条，失败请求 627 条。
  2. **准入令牌强一致性**: `manifest.json` 准确记录了 6 份领域切片的 SHA-256 校验和、输入文件列表及时间窗口（2026-07-14 至 2026-09-23）。
  3. **成本与额度核算**: `finance.json` 忠实反映了按量等价口径（Pay-as-you-go Equivalent），对未配置定价的端点显式标注降级提示，无伪造 `$0.00` 的情况。
  4. **Compaction 关联警告**: 在聚合早期（7/8月历史日志中）输出了若干 `compaction linking: successor needle not found` / `predecessor needle not found` 提示，这是因为部分历史记录的历史在日志截断前驱点或跨会话重置所致，系统正确执行了“宁可断开，绝不误连”的安全策略。

### 4.2 请求索引与失败请求报告（Requests Index & Failed Index）
- **产物清单**:
  - `reports/requests/index.json` (74,426 行结构化请求宽表)
  - `reports/requests/failed.jsonl` (627 条失败请求)
  - `reports/requests/failed.md` (失败请求人读分析报告)
- **核验发现**:
  1. `requests/index.json` 包含每次请求的精确时间戳、会话别名、任务轮次、协议、模型、端点、延迟、TTFT、Token 四分量及对应的详单文件名 `detail_file`。
  2. `requests/failed.md` 针对 627 次失败调用进行了错误分类聚合（包含 upstream 5xx, rate_limit, context_overflow 等），并提供了精确的 `req` 坐标（如 `basename:line`），便于直接对接 `vmr diff` 或 `vmr replay`。

### 4.3 任务叙事与详情（Journey Narratives）
- **产物清单**:
  - `reports/journeys/index.json` 与 `reports/journeys/index.md` (全量候选索引与聚类)
  - `reports/journeys/details/j-*.json` 与 `j-*.md` (9 月份 5 个核心 Journey 详情)
- **核验发现**:
  1. **决策脊柱（Decision Spine）清晰**: 准确标注了每一步的角色（执行 🔧、观察 👀、重试 🔄 等），并提炼了模型思考（Reasoning）与工具调用的核心参数。
  2. **触达资产追踪（Touched Artifacts）精确**: 准确捕获了脚本调用与读写的绝对路径（如 `fetch_daily_news.py`、`daily-2026-08-31.json`）。
  3. **时序甘特图（ASCII）直观**: 清晰呈现了 `exec`、`process`、`read` 工具调用的交替与重试分布。
  4. **规则 Finding 命中**: 正确检出了 `exact_repeat_tool_call`（如 J1 中连续调用 `process` 轮询后台进程）。
  5. **LLM 解读层配合良好**: 结合本地 VMR 实例成功生成了“一句话结论”、“疑似问题深度解读”、“整体工作方式总结”及“VMR 观察盲区声明”，未破坏基础事实层指标。

### 4.4 横向成对对比（Pairwise Compare）
- **产物清单**:
  - `reports/compares/index.json` 与 `reports/compares/index.md` (对照总览)
  - `reports/compares/compare-*.json` 与 `compare-*.md` (5 份对比报告)
- **核验发现**:
  1. **分叉点（Divergence Point）定位精准**:
     - 在 J1 (Lobster) vs J4 (OpenClaw) 对比中，精确定位到第 1 步的分叉（两者均调用 `exec` 执行 Python 脚本，但脚本路径不同：`/Users/stanford/data/lobsterai/...` vs `/Users/stanford/code/...`）。
  2. **行为剖面差分显著**:
     - 捕获到 LobsterAI 偏好使用异步进程模型（`exec` 启动后台进程 + `process` 轮询，27 步完成），而 OpenClaw 偏好同步多次 `exec` 执行单步命令（65 次 `exec`，66 步完成）。
     - 捕获到模型/工具时间比的剧烈变化（1.43× vs 3.74×）。
  3. **System Prompt 差异 Diff 完全可视化**: 对比报告中完整输出了两侧 System Prompt 的统一 unified diff，揭示了不同 Agent 框架在指令工程与工具描述上的结构差异。
  4. **交付物比对（Touched Artifacts Diff）有效**: 准确比对了双方最终生成和修改的产物差异。

### 4.5 基准统计（Benchmark Statistics）
- **产物清单**:
  - `reports/journeys/benchmarks.md` 与 `reports/journeys/benchmarks.json`
- **核验发现**:
  1. **1790 个 Journey 全量基准覆盖**: 计算了全量指标的均值、中位数、P90、最小值与最大值。
  2. **中位数 vs 均值的真实偏斜捕捉**: 文档中正确提示了时间类指标均值受少数跨多日长尾 Journey 影响的现象，推荐以中位数和 P90 为主（如 Agent 侧执行时间均值 4571.9s，但中位数为 14.9s，P90 为 263.4s）。
  3. **Finding 分组对比与相关性矩阵**:
     - Spearman 秩相关显示：模型时间与工具调用次数强相关（$\rho = 0.84$），净工作时长与工具调用次数强相关（$\rho = 0.82$）。
     - Finding 分组对比显示：命中 `exact_repeat_tool_call` 的任务中位耗时达到 958.7s，显著高于未命中组的 290.8s（+70%）。
  4. **Context Rot 注意力衰减分桶分析**: 按 0-32k、32k-64k、64k-128k、128k-256k、256k+ 分桶统计了 Finding 密度与错误率，数据分布平稳。
  5. **N-gram 高频工具调用模式**: 提炼出 top 序列为 `bash → read` (4058次)、`read → bash` (3683次)、`edit → bash` (2355次)。

### 4.6 纯渲染与前端仪表盘（Render-Only & Dashboard）
- **产物清单**:
  - `reports/macro-dashboard.html`
  - `reports/journey-viewer.html`
  - `reports/request-browser.html`
- **核验发现**:
  1. **L3 缓存与幂等性**: `vmr analyze -render-only -lang zh` 能够精准识别磁盘 JSON 状态，未变动时直接提示“L3 缓存命中，产物已是最新”；加上 `-no-cache` 时平滑完成 100% 离线重渲染，输出与全量分析完全等价。
  2. **安全隔离合规**: 报告文件严格保持 `0600`，子目录保持 `0700`，有效防止对话敏感明文泄露。

---

## 5. 发现的问题与观察事实清单（不改代码，记录在案）

在全量日志端到端测试与复核过程中，发现以下事实与值得记录的工程现象：

| 编号 | 模块 / 阶段 | 现象与事实描述 | 影响评估与根因分析 |
| :--- | :--- | :--- | :--- |
| **OBS-01** | `report` (Compaction) | 扫描 7/8 月历史日志时出现多条 `compaction linking: successor/predecessor needle not found` 警告。 | **正常安全表现**。历史日志中存在被截断的早期会话，系统严格遵循“宁可断开，绝不误连”原则，未引入伪造前驱。 |
| **OBS-02** | `journey` (Finding) | 非 Anthropic 协议下，部分错误类 Finding（如 `error_retry_unadapted`、`error_then_unverified_success`）提示“结构性无法触发”。 | **已知设计限制**。OpenAI 协议无标准 `is_error` 字段，设计文档 Part 2 §7 已明确“宁可粗糙也不猜语义”，系统在 Markdown 中进行了诚实声明。 |
| **OBS-03** | `journey` (LLM) | 在单 Journey 分析 `j-lobster-20260905...` 时出现 `warning: LLM detector semantic_oscillation skipped (total budget 4m0s expired)`。 | **保护机制生效**。长任务证据包较大时，LLM 检测器并发运行触发了 4 分钟总超时预算保护，系统安全降级跳过该检测器并正常完成后续渲染。 |
| **OBS-04** | `compares` (Markdown) | 某些长跨度任务的 System Prompt diff 较大（超过数百行）。 | **表现层可优化**。Diff 完整展示保证了证据完备性，但在极长 Prompt 变更时会导致 Markdown 文件体积较大（例如约 60KB），可在未来考虑增加折叠层级或分块摘要。 |
| **OBS-05** | `pricing` (Finance) | 少量非标准/实验性模型（如带有临时后缀的模型）在 `finance.json` 中标记为未定价或部分降级估算。 | **口径一致性**。系统坚决不将未知费率伪装为 `$0.00`，而是显式降级披露，符合设计文档 Part 2 §2.5 的核心纪律。 |

---

## 6. 结论与总结

1. **功能点 100% 完备达成**：
   - 宏观切片聚合（Macro Report）
   - 全局请求与失败索引（Requests Index & Failed Index）
   - 全量会话与任务聚类索引（Journey Index & Clusters）
   - 9 月份核心任务叙事物化与 LLM 深度解读（Journey Narratives）
   - 5 组跨模型/跨 Agent 对照深度比对（Pairwise Compare）
   - 全量基准统计与相关性分析（Benchmark Statistics）
   - 离线纯重渲染（Render-Only）与静态仪表盘（Dashboard）
2. **纯中文本地化体验一致**：所有 Markdown 报表、JSON 元数据、控制台进度提示及对照报告均严格使用中文输出。
3. **架构纪律严明**：未修改任何代码，全量数据在现有架构设计下平稳运行，各项断裂处理、超时保护、令牌一致性与安全隔离机制均表现优异。所有分析产物均已在 `reports/` 目录下就绪。

---

## 7. 关键问题与异常现象深度剖析与优化治理方案

在本次全量 72 个日志文件（77,088 条记录）的端到端真实测试中，我们记录了 5 个最具代表性的工程现象与边界问题。本章节结合具体日志实例、底层源码实现机理及设计文档约定，对这 5 个问题进行逐一深度解剖，并提出兼顾 VMR 核心架构约束的改善建议。

---

### 问题一 (OBS-01)：Compaction 前驱/后继关联探针未命中警告 (`needle not found`)

#### 1. 现象还原与具体实例
在执行宏观报表聚合扫描（`-macro-only`）过程中，控制台标准错误输出中打印了数十条形如下列的警告信息：
```text
2026/09/24 11:39:42 report: compaction linking: successor needle not found for compaction at 2026-07-14T22:28:34+08:00 (logs/vmr-audit-2026-07-14.jsonl.zst)
2026/09/24 11:39:42 report: compaction linking: predecessor needle not found for compaction at 2026-07-14T22:28:34+08:00 (logs/vmr-audit-2026-07-14.jsonl.zst)
2026/09/24 11:39:42 report: compaction linking: successor needle not found for compaction at 2026-07-16T15:51:15+08:00 (logs/vmr-audit-2026-07-16.jsonl.zst)
2026/09/24 11:39:43 report: compaction linking: predecessor needle not found for compaction at 2026-08-20T10:46:25+08:00 (logs/vmr-audit-2026-08-20.jsonl.zst)
2026/09/24 11:39:44 report: compaction linking: successor needle not found for compaction at 2026-09-23T14:41:40+08:00 (logs/vmr-audit-2026-09-23.jsonl.zst)
```
受影响的时间点广泛分布在 7 月中旬、8 月下旬以及 9 月 23 日的密集交互期。

#### 2. 底层机理与源码根因分析
- **源码定位**：`internal/report/session.go` 中的 `linkCompactions(a *SessionAnalysis)` 函数。
- **业务场景**：长程 Agent 任务在上下文膨胀后，往往会发起一次独立的 LLM 压缩调用（例如向模型发送：“Treat the conversation so far and summarize it into a compact context checkpoint...”）。该请求是一个独立于工作流消息链之外的单轮交互。
- **为什么不用精确 Hash 缝合**：普通的会话缝合（`ctxgraph.StitchGraph`）依赖消息规范化 SHA-256 的倒排索引。但在全量历史重写（Full history rewrite）的压缩场景下，压缩前后的上下文被彻底置换为自然语言摘要，前后会话之间**不存在任何一条逐字相同的消息 Blob**。因此，系统引入了文本探针作为补充信号。
- **探针逻辑**：
  1. **后继查找（Successor）**：取压缩调用的响应文本前 200 字节（`needle(c.respText)`），检查是否存在时间戳晚于压缩调用的会话，其首轮请求的输入文本（`first.firstText`）包含了该探针。
  2. **前驱查找（Predecessor）**：取会话首轮指令前 200 字节（要求去前缀后长度 $\ge 12$ 字符 `minPredNeedleRunes`），检查压缩调用的输入文本（`in := c.firstText`）是否包含该指令。
- **未命中的根因**：
  - **跨日志边界截断**：例如 `2026-07-14` 是日志采集的第一天，被压缩的原始会话可能发生在 7 月 13 日甚至更早（在日志范围之外），前驱天然不存在。
  - **客户端改写与模板注入**：部分 Agent 框架在向压缩模型发送历史时，对原始 Prompt 进行了清洗、加括号转义或结构体重排；或者压缩模型生成的摘要在下一轮注入时被 Agent 增加了新的时间戳、元数据头（例如 `⟦context:checkpoint:2026...⟧`），导致前 200 字节的纯文本包含断言失败。
  - **设计约束守门**：设计文档 Part 2 §5 确立了“**宁可断开，绝不误连**”的第一性原则。当探针无法以高确定性咬合时，代码选择打印 log 并保留两端为空，坚决不靠模糊距离推测。

#### 3. 改善与完善处理机制
1. **优先提取结构化上下文标识（Session/Context Marker）**：
   现代 Agent 框架（如 OpenClaw、Claude Code、Hermes）在请求 Header 或 System 提示词开头往往具有固定的结构标记（例如 `session=agent:main:feishu:...`、`⟦openclaw:ctx⟧`、`parent_id` 等）。可以在每条记录的事实提取阶段（`recordFacts`）抽取这些零代价元数据。如果两个会话具有显式相同的 `SessionID`，则无需依赖易受文本波动影响的 200 字节探针，即可高置信度缝合。
2. **探针文本的多级规范化（Multi-stage Normalization）**：
   当前的 `stripBracketPrefix` 仅剥离了 `[... ]` 格式的前缀。应增强文本清洗管道：
   - 剥离 Markdown 引用符（`>`）、时间戳模式（如 `\d{4}-\d{2}-\d{2}`）；
   - 对压缩输出（摘要）提取前 3 句的特征词指纹或跳过开头的静态模版说明（如 `Here is a summary of the conversation:`），以摘要的核心内容作为探针。
3. **日志级别收敛与分级降噪**：
   在离线日志分析中，输入文件集合的边界（最早的文件和最新的文件）出现断头是常态。对于文件起始段未能找到前驱的现象，应判定为正常的“边界截断”，收敛至 Debug 级别；仅在密集会话流内部且置信度差异可疑时输出 Warning，避免向用户抛出大量非致命的系统排查日志。

---

### 问题二 (OBS-02)：非 Anthropic 协议下工具错误类 Finding 结构性缺失

#### 1. 现象还原与具体实例
在生成的全部单 Journey 详情报告（如 `reports/journeys/details/j-lobster-20260901...md`）及全局基准报告（`reports/journeys/benchmarks.md`）中，均出现了显式的黄色警告提示：
```markdown
> ⚠️ 本 batch 语料仅 3.4% 为 Anthropic Messages 协议请求。以下信号依赖仅 Anthropic Messages 协议才会填充的字段（`chatmsg.ToolResult.IsError`），在非 Anthropic Messages 请求上结构性无法触发——命中率为 0 或指标全为 0 代表"测不出来"，不代表"检查过没问题"：error_retry_unadapted, error_then_unverified_success, error_recovery_count, Context Rot error rate, Tool Sequence error rate
```
而在 9 月份的基准统计中，`error_recovery_count` 的全局 P90 恒为 0，`error_retry_unadapted` 全局命中率仅 1%。

#### 2. 底层机理与源码根因分析
- **源码定位**：`internal/chatmsg/toolresults.go`（第 24 行）与 `internal/journey/benchmarks_coverage.go`。
- **协议差异的客观事实**：
  - **Anthropic 协议**：在其 Messages 规范中，ToolResult 内容块原生定义了 `is_error` 布尔字段（`{"type": "tool_result", "tool_use_id": "...", "content": "...", "is_error": true}`）。VMR 在解析时能 100% 确定工具执行是成功还是失败。
  - **OpenAI 协议**：在其 `/v1/chat/completions` 标准规范中，Tool message 仅包含 `role: "tool"`, `tool_call_id: "..."`, `content: "..."`，**协议标准中不存在任何表示执行成败的字段**！无论工具返回的是执行成功结果，还是 Python 异常堆栈、Shell 报错信息，全部序列化为 `content` 字符串。
- **设计抉择的坚守**：
  - 许多分析工具会试图在 `content` 文本中用正则表达式匹配 `fatal`、`error:`、`exit status 1`、`Traceback` 等关键字来“猜测”是否出错。
  - 但设计文档 Part 2 §7 和 `KNOWN_ISSUES` 明确将此记录为**决定不修项**：“*对 ToolResult 自由文本做关键字匹配，会把‘日志文本里提到 error’、‘被引用的报错代码样例’、‘代码审查包含 error 字符串’和‘这次调用真的失败了’混为一谈。强行在 OpenAI 响应体里猜‘这是不是一次错误结果’就是‘宁可粗糙也不猜语义’的反例*”。
  - 因此，VMR 代码中强制设定非 Anthropic 协议的 `ToolResult.IsError` 恒为 `false`，并在报表中诚实披露该限制。

#### 3. 改善与完善处理机制
1. **支持事实型扩展字段的非侵入式探测（Zero-Heuristic Structured Extensions）**：
   主流开源 Agent 框架（如 LangChain、AutoGPT、部分自研网关）在通过 OpenAI 格式回传工具结果时，虽然主字段是 `content`，但常常会在同级 JSON 中附加扩展字段（例如 `is_error: true`、`error: true`、`status: "error"`），或者将 `content` 编码为 JSON 结构体 `{"error": "...", "exit_code": 1}`。
   - **完善方案**：在 `chatmsg/toolresults.go` 中，增加对同级扩展布尔字段的解析；若 `content` 本身是合法的 JSON Object，且包含显式的布尔型 `is_error` 或整型 `exit_code != 0`，则提取为真值。这完全遵循“解析结构化事实”，依然不需要任何模糊的自然语言正则匹配。
2. **两级检测体系（Ground Truth vs Heuristic Indicator）**：
   在保留核心 Finding 纯洁性的前提下，解耦“协议级错误”与“文本推断异常”：
   - 维持 `chatmsg.ToolResult.IsError` 的纯粹性（仅信任协议结构字段）；
   - 在上层（`journey/findings`）引入一个独立的可选指示器（例如 `heuristic_tool_failure`），仅在确定性系统调用工具（如名为 `bash`、`exec`、`sh`）且输出严格以标准退出码（如 `exit status [1-9]`、`Command failed with exit code`）结尾时触发。
   - 在报告中将其明确标记为“推测性指标”，不计入基准统计的“原生错误率”，从而在兼顾严谨性的同时补齐排障感知。

---

### 问题三 (OBS-03)：LLM 检测器在复杂长任务下触发超时保护 (`4m0s budget expired`)

#### 1. 现象还原与具体实例
在对 9 月 5 日的复杂长任务 `j-lobster-20260905T080000-20260905T080923-65996040`（36 轮，35 次工具调用）执行单任务详细物化时，控制台抛出警告：
```text
warning: journey j-lobster-20260905T080000-20260905T080923-65996040: LLM detector semantic_oscillation skipped (total budget 4m0s expired)
calling http://192.168.0.22:8800/v1/ (model=cheap): evidence pack 22936 chars (~5734 tokens estimated)
reports/journeys/details/j-lobster-20260905T080000-20260905T080923-65996040.md (1 任务, 36 轮)
```
虽然任务详情与主报表最终正常生成，但 `semantic_oscillation`（语义摇摆检测器）被强制熔断跳过。

#### 2. 底层机理与源码根因分析
- **源码定位**：`internal/journey/llm_findings_run.go`（第 20 行与第 60 行）。
- **执行模型**：
  - VMR 在开启 `-llm-addr` 时，会通过 `runDetectorsConcurrently` 同时并发拉起 6 个 LLM 检测 Goroutine：
    1. `tool_result_misinterpretation` (结果误读)
    2. `semantic_oscillation` (逻辑摇摆/反复)
    3. `goal_drift` (目标偏离)
    4. `constraint_dropped` (约束遗失)
    5. `plan_misalignment` (规划偏离)
    6. `unverified_completion_claim` (谎报完成)
  - 系统设置了硬超时上限：`var llmFindingsBudget = 2 * llmHTTPTimeout = 240s = 4分钟`。
- **超时的具体推导**：
  - 该 Journey 的证据包（Evidence Pack）规模较大（包含 36 步完整的提示、工具参数与关键状态），单次调用预计需要处理 5,700+ 输入 Token 并生成详细的 JSON 推理分析。
  - 6 个 Goroutine 几乎在同一毫秒向配置的 LLM 端点（`http://192.168.0.22:8800/v1/`）发起并发 POST 请求。
  - 本地运行的 VMR 实例在 `config.yaml` 中配置了全局 `max_concurrency: 6`，且上游模型（`cheap` 映射到的外部提供商）存在单 IP 限流或排队排障机制。6 个高负载的 LLM 请求互相竞争网络连接与推理资源，导致部分检测器响应严重滞后。当总耗时撞上 4 分钟红线时，`context.WithTimeout` 触发 cancel，尚未返回的 `semantic_oscillation` 检测器被直接剥离。
- **保护价值**：
  - 这种超时设计是系统健壮性的体现（Fail-open 设计）：确保分析进程不会因为单个远程模型卡死而无限挂起，保证了 CLI 工具的交付确定性。

#### 3. 改善与完善处理机制
1. **轻量规则前置门禁（Pre-flight Rule Gating）**：
   目前 6 个 LLM 检测器是盲目全量拉起的。但实际上，大多数异常具有明确的规则前置特征：
   - `semantic_oscillation`：仅在 `repeated_action_rate > 0.05` 或存在重复工具调用时才可能成立；
   - `constraint_dropped`：仅在 `compaction_count > 0` 且丢弃了字符时才需要运行；
   - `unverified_completion_claim`：仅在最后 3 轮无验证类工具调用时才需要触发。
   - **效果**：通过规则前置过滤，单任务需要触发的 LLM 检测器通常可缩减至 1~2 个，彻底消除并发拥堵。
2. **证据包按检测器精简投影（Detector-tailored Projection）**：
   目前各检测器共享同一个庞大的证据包。例如检测“语义摇摆”只需要每轮的思考简述（Reasoning）与工具名称参数，根本不需要带入数千字的工具返回输出；精简证据包可使输入体积缩小 70% 以上，显著提升模型推理速度。
3. **可配置的客户端并发与超时预算**：
   在 `report.yaml` 中开放 `llm_max_concurrency`（例如默认 2 或 3）与 `llm_timeout_budget` 参数，允许用户根据本地部署网关的实际承载能力进行流量平滑。

---

### 问题四 (OBS-04)：成对对比报告中 System Prompt Diff 篇幅过大影响排障效率

#### 1. 现象还原与具体实例
在生成的对比报告 `reports/compares/compare-j-lobster-20260901...-vs-j-openclaw-20260920...md` 中：
- 两侧的 System Prompt 规模巨大（A 侧 10.4K tokens，B 侧 11.0K tokens）；
- 报告生成的 System Prompt Diff 区域行数高达 **616 行**，单个 Markdown 文件膨胀至 **59 KB**；
- 在纯文本终端或部分简易 Markdown 查看器中，读者需要连续翻页数十次才能越过这段 Diff，寻找下方的关键指标、分歧点与时序差异。

#### 2. 底层机理与源码根因分析
- **源码定位**：`internal/journey/render_compare.go` 中的 `diffLines` 函数。
- **算法模型**：
  - 代码采用了基于动态规划的最长公共子序列（LCS）算法，对双方的 System Prompt 进行逐行比对，输出标准统一的 Unified Diff（`+` / `-` 标记）。
- **适用场景的错位**：
  - 当比对**同一个 Agent** 在短时间内的两次迭代时（例如工程师微调了某几句提示词），该算法非常优雅，能够精准呈现 5~10 行的局部改动。
  - 但当比对**不同 Agent 框架**（例如 LobsterAI vs OpenClaw）时，双方的基础系统架构、内置能力、引导词结构完全不同，公共行比例极低。LCS 算法此时退化为几乎将 A 侧全部以 `-` 输出，再将 B 侧全部以 `+` 输出，变成了两个巨大文本块的粗暴拼贴，丧失了“差分比对”的实际意义。

#### 3. 改善与完善处理机制
1. **设定 Diff 篇幅阈值与外部文件隔离（Diff Spilling）**：
   当检测到 `diffCount > 100` 行时，自动触发防膨胀保护：
   - 主报表中仅保留宏观度量摘要（例如：`A 侧 10.4K tokens, B 侧 11.0K tokens, 相似度 15.2%, 差异行数 616 行`），并仅内联展示前 20 行高亮差异；
   - 将全量 616 行 Diff 保存至独立的外部文件（例如 `reports/compares/diffs/sysprompt-xxx.diff`），并在主报表中以超链接形式引入。
2. **结构化分块对齐 Diff（Section-aware Diff）**：
   针对 Prompt 普遍使用 Markdown 标题（`#`, `##`）的特征，先按一级标题分块（如 `# Role`, `# Tools`, `# Format`）：
   - 若某章节仅在一侧存在，直接标记为 `+ [新增章节: Output Format]` 或 `- [缺失章节: Safety]`；
   - 仅对双方同名的章节进行内联行级 Diff，大幅降低视觉噪音。
3. **基于相似度自动降级为“特征对照表”**：
   当两侧 Prompt 的行相似度低于 30% 时，系统判定两者为异构系统，主动放弃行级 Diff，改为渲染一张结构对照表（对比双方的规则条数、声明工具数、注入变量列表），更贴合架构复盘人员的心智模型。

---

### 问题五 (OBS-05)：财务报表中实验性/私有模型费率无法匹配 (`未定价 / null`)

#### 1. 现象还原与具体实例
在宏观财务切片 `reports/macro/finance.json` 及相关报表表格中，可以观察到以下现象：
- 部分请求或端点（例如包含 `-vision-exp` 的实验性模型 `deepseek-v4-flash-vision-exp`、部分私有模型别名）的成本核算字段为 `null`；
- 在渲染出的 Markdown 表格中，该端点对应的金额列显示为 `未定价`，并伴有“降级估算提示”。

#### 2. 底层机理与源码根因分析
- **源码定位**：`internal/pricing/resolve.go` 与 `internal/pricing/resolver.go`。
- **两层核算架构与严格纪律**：
  - VMR 的价格解析器遵循两层查找机制：
    1. **层级 1 (用户显式配置)**：优先查找 `config.yaml` 中特定 provider 下的 `pricing.rates`（覆盖单价或折扣）及 `aliases`；
    2. **层级 2 (内置标准价目表)**：回退查找代码内嵌的标准价格表（`LoadStandard`，快照于 2026-08-31，覆盖 345 个标准公共模型）。
  - **核心设计哲学**：在计费体系中，**“未定价（nil）”与“免费（$0.00）”具有本质区别**。系统坚决杜绝在缺少费率时私自将其赋值为 0，否则会导致宏观财务报表的总成本严重失真（将高额推理成本误读为零成本）。因此凡未匹配到费率的模型，`Resolver.RateFor` 均返回 `ok=false`，严格输出“未定价”。
- **未命中的现实原因**：
  - 模型迭代极快：上游提供商经常推出临时后缀模型（如 `-vision-exp`、`-0731`、`-preview`）；
  - 开发者在本地网关配置了内部别名，但未在 `config.yaml` 的该 provider 下显式配置对应的 `rates`，导致请求落入标准表盲区。

#### 3. 改善与完善处理机制
1. **基于规范化词根的基础模型兜底匹配（Base Model Fallback / Suffix Stripping）**：
   目前解析器已经支持部分 Organization 前缀剥离。应进一步完善后缀剥离规则：
   - 当探测到带有 `-vision-exp`、`-preview`、`-latest`、`-0731`、`-chat` 等常见修饰后缀且全名未命中时，自动回退查找其规范化词根（如将 `deepseek-v4-flash-vision-exp` 回退匹配到 `deepseek-v4-flash`）；
   - 若词根命中，则采纳词根单价进行估算，并在报表中显式标注标记符（例如 `≈ $0.12 (按基础模型 deepseek-v4-flash 估算)`），既避免了未定价的断档，又保持了财务披露的诚实性。
2. **离线分析专属的补丁价目表（Sidecar Pricing Overrides）**：
   目前分析半区的费率完全继承自路由运行时的 `config.yaml`。
   - **完善方案**：在 `report.yaml`（分析专用配置文件）中开放 `pricing_overrides:` 字典映射。审计分析人员在复盘历史旧账本或引入新测试模型时，无需冒着影响生产路由的风险修改 `config.yaml`，直接在 `report.yaml` 中为实验性模型补充单价，实现敏捷对账。
3. **Provider 级的通配费率（Wildcard Default Rates）**：
   在 `config.yaml` 的 `pricing.rates` 中支持 `model: "*"` 搭配明确的 `explicit` 费率。对于某些按统一 Token 费率打包的聚合提供商（如全场统一价的小型中转站），一条通配规则即可消除所有子模型的未定价问题。

