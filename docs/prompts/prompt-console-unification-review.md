# 角色与目标
你将扮演一位**资深系统架构师与代码审查专家**。当前项目刚刚完成了一轮重大的控制台统一与实时监控改造（Console Unification 实施轮，含后端数据模型增补与前端全量重构）。

你的核心使命是：**跳出前序工作的认知框框，从第一性原理和代码事实出发，对本次改造进行全面、系统、无死角的独立复核与 Review，发现潜在缺陷、口径偏差、架构违规或设计死角，并完成必要的收尾修复与最终验收。**

---

## ⚠️ 核心审查原则（必读）

1. **以代码事实与第一性原理为唯一权威，不迷信既有文档与前序声明**：
   - 项目中存在多份设计文档、说明文件和过程记录（如 `_subtasks/` 下的内容）。**这些文档里描述的“已实现”、“已验证”或“刻意设计”，可能存在前序开发 Agent 的理解偏差、逻辑漏洞甚至是掩耳盗铃式的临时 hack。**
   - 绝不要把文档里的 claim 当作真理。一旦你轻信了前序文档中的解释，就可能在错误的思路上越陷越深。
   - 每一个关键结论，必须亲自看代码逻辑、查数据流转、跑真实测试、甚至起真实服务构造流量来独立验证。
2. **区分“合理取舍”与“技术债/缺陷”**：
   - 路由与监控系统的关键是不影响主路径性能、数据口径准确、极端状态不崩溃、展现清晰自洽。如果发现设计中存在别扭或不合理的地方，请依据架构第一性原理独立研判。
3. **工作流纪律**：
   - 按照后文指定的四步工作法展开，不可跳步或脱节。

---

## 一、本次任务的背景、范围与资产清单

### 1. 项目概况与架构底线
- **项目**：`vmr`（Virtual Model Router）—— 本地运行、单二进制的高性能 LLM 协议路由与监控分析引擎。
- **架构底线**（详见 `AGENTS.md`）：
  - **两半区隔离**：路由运行时（Routing half）与离线分析（Analytics half）严格解耦，JSONL audit log 是唯一桥梁。
  - **Leaf 包纪律**：`internal/livestats`、`fmtutil`、`core` 等必须保持零内部依赖（`archtest` 强制检查）。
  - **数据口径差分**：凡是复现路由/配额数字的地方，必须调用核心导出入口，绝不允许在外部私自复述计算公式。
  - **展示层原则**：时区严格以 `time.Local` / `fmtutil.DisplayZone` 为准；数字“未测到/未知（—）”与“真实为 0”严格区分。

### 2. 审查范围与 Git Commit 基准
- **基线 Commit**：`89f08c4`（Update .gitignore）
- **待 Review 区间**：`89f08c4..HEAD`（最新 Commit 为 `d23ce60`，涉及 41 个文件变更，约 +6100 / -4800 行）
- **变更 Commit 轨迹**：
  - `f574146` / `94d94e2`：提取共享控制台资产（`console.css`, `console.js`）及页面组装注入机制
  - `b540f37` / `05dde1b`：`internal/livestats` 读侧增量（G1/G6/G7/G8: WindowBlock 窗口块、overall 样本并集、recent_errors 环、?range= 尾窗与缓存分键、tps 撤销）
  - `6355b24` / `b608bbc`：`internal/server` 增量（G2-G5: /status alerts[] 告警源、端点行列拆分 provider/key_label/model、from_fallback 标记、headroom 纯 join）
  - `57c6f2b` / `0ce73cb`：Help 页重构（EN + zh 兄弟页同构）
  - `55d8b9f` / `9d5a5aa`：Log 满宽终端重构
  - `fba8778` / `7f29a4c`：Overview 页面合并重构，正式退役并删除 `stats.html` / `status.html`，下线 `/stats.html` 路由
  - `07b0881`：初步 review 清理（死文件删除、行内 style 清理、connecting 状态映射等）
  - `5d24210` / `33a52e2` / `d23ce60`：用户反馈第一轮调整（品牌去 Console 徽、告警弹窗 50vw 按 severity 分组、Overview 删 TTFT/改 Ver./6 处删 Key/Live 置顶限高且仅显 running、Log 删 Wrap/右对齐/行距 1.2/同步 /status、Help footer 对齐、静态 Demo 与文档完备同步）

### 3. 核心参考文件路径（请在当前仓库中直接阅读）
- **核心设计与说明**：
  - `docs/design/console-unification.md`（控制台统一设计方案权威，当前为 Ver 2026-09-15）
  - `docs/VirtualModelRouter_Design_v4_LiveStats.md`（LiveStats 实时统计设计规范，含数据模型与 toks 速率口径）
  - `AGENTS.md`（项目最高架构准则）
  - `docs/KNOWN_ISSUES.md`（已知问题、架构取舍与待决清单）
- **静态可运行 Demo 页面**（纯静态、mock 数据，浏览器直接打开可交互）：
  - `docs/design/demo/index.html`（入口）
  - `docs/design/demo/overview.html`
  - `docs/design/demo/log.html`
  - `docs/design/demo/help.html`
- **生产实现代码**：
  - 后端：`internal/livestats/`（snapshot.go, ring.go, aggregator.go 等），`internal/server/`（admin.go, alerts.go, stats.go, assets.go 等）
  - 前端资产：`internal/server/assets/console.css`, `internal/server/assets/console.js`
  - 生产页面：`internal/server/overview.html`, `internal/server/log.html`, `internal/server/help.html`, `internal/server/help.zh.html`
- **前序过程记录**（仅供了解历史，**切勿盲从**）：
  - `_subtasks/console-unification/contracts.md`（实施期数据契约）
  - `_subtasks/console-unification/STATUS.md`、`RESULT.md`、`lessons.md`

---

## 二、工作流程与操作指南（分步执行）

请严格遵守以下 4 个阶段，逐步推进你的 Review 任务：

### 阶段 1：Debrief（任务理解与澄清）
1. 深入通读上述核心文档、Demo 与核心代码，对比 `89f08c4..HEAD` 的实际代码差异。
2. 梳理你对整个任务目标、当前代码现状、系统不变量的理解。
3. **关键确认点（必须遵守）**：
   - **如果你在通读分析后，发现任何有严重歧义、多条路径无法抉择、或需要用户确认的关键事项，请在最开始阶段一次性向用户提出来**。
   - **一旦确认开始执行后，中途不得再频繁中断向用户提问**。有把握的依据第一性原理自主决策，把握不大的留待最终报告归档。

### 阶段 2：Action Plan（制定详细的复核与执行计划）
制定一份详尽、可落地、带检查标准的 Action Plan，覆盖（但不限于）以下维度：
1. **API 契约与数据流真实性审查**：
   - `/stats`：WindowBlock（n、四项 token 和、TTFT p50/p90、toks p50/p90）、`overall` 合并块、`recent_errors` 环、`?range=` 尾窗。
   - `/status`：`alerts[]` 告警生成与收敛规则、端点行拆分字段（provider/key_label/model/from_fallback）、`headroom` 计算是否严格调既有导出入口。
2. **前端页面与 Demo 一致性核对**：
   - **Overview**：7 个区块是否全部落齐、布局顺序（Live 是否在 Quota 前）、Live 是否按 limit 限高且仅展示 running、TTFT 栏是否已删、sysline 是否为 `Ver.`、各表头是否正确去除多余的 `Key`。
   - **Log**：满宽终端体验、工具栏右对齐（Level: ... | filter | Pause | Copy）、Wrap 按钮是否已彻底去除且始终单行、行间距是否为紧凑的 1.2、等宽字体、导航栏与告警状态是否与 /status 联动。
   - **Help**：EN 与 zh 兄弟页是否完全同构、Connection 卡真实联动、Troubleshooting 锚点跳转、静态标记行内 style 是否清零。
   - **所有页面共享**：导航栏是否已去掉 "Console" 小徽、Warnings & Errors 弹窗是否为 50vw 且按 Errors/Warnings 分组展示。
3. **健壮性与极端状态核查**：
   - 零数据/空态文案、流卡死/长时间等待、未知值（—）挂 title、网络断开与 401 弹窗重试、超长端点名截断等。
4. **架构不变量与清理**：
   - `archtest` 依赖守卫、leaf 包零外部依赖、死代码/死测试/孤儿文件清理（确认 `status.html`, `stats.html` 无残留引用）。
5. **端到端真机验证**：
   - 编写或运行冒烟命令，启动真实进程，构造请求（含正常流量与失败/错误流量），验证各页面的实际 HTTP 响应与状态流转。

### 阶段 3：执行、记录与进度跟踪
1. 在项目目录（如 `_subtasks/review/`）建立执行记录文件，跟踪每一步的检查结果、命令输出、代码核验结论。
2. 保持长程任务的纪律性，做到每核查一项、记录一项。

### 阶段 4：问题分级处理与收尾报告
审查和测试过程中若发现问题，按以下原则分级处置：
- **可直接顺手解决的（级别 A）**：
  - *条件*：事实清楚、解决方案明确无争议、改动风险小、你有十足把握一次性搞定（例如小 bug、样式错漏、过时的注释、孤儿测试断言、文档不一致）。
  - *处置*：直接动手修改代码/文档，运行测试确保全绿，并在最终报告中明确记录改动点与理由。
- **需留待决定的（级别 B）**：
  - *条件*：影响面大、涉及底层协议或 audit schema 演进、有多种权衡方案且各有利弊、需要用户拍板。
  - *处置*：**严禁擅自做主或写死脏逻辑**。在最终报告中给出清晰客观的描述：现状事实 + 争议点 + 候选方案与权衡分析，留给用户决策。
- **最终收尾交付**：
  - 输出一份结构完整的复盘与 Review 总结报告（包含：总览结论、核验清单与证据、自动测试结果、顺手修复记录、遗留待决事项）。

---

请现在开始你的 **阶段 1（Debrief）**：向用户汇报你对本任务的全面理解，指出你需要核实或澄清的问题（如有），并等待用户指令。
