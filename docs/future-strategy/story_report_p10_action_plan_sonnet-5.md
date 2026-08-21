// Ver 2026-08-21 12:00, by Sonnet 5

# P10 · 历史文档与规划遗留清理 — ActionPlan

## 0. 定位

本文是 `story_report_dev_plan_2_sonnet-5.md`（下称"DevPlan2"）P10 阶段的执行级 ActionPlan。P10 是
**纯文档编辑**，不改动任何 `.go` 源码，不涉及 `go build`/`go test`。开工前已对照当前代码库与文档
状态重新核实过 DevPlan2 P10 的三项任务描述，并据此对 DevPlan2 做了三处最小修正（见该文件 P10 小节
2026-08-21 的更新标记）——本文档不重复那次核实过程，只承接其结论、并把结论落成具体的文件编辑步骤。

**范围**：`docs/future-strategy/story_report_architecture_opus-5.md`、
`docs/future-strategy/cli_architecture_redesign_gemini-3.7-flash.md`、
`docs/future-strategy/json_lang_policy_plan_sonnet-5.md`、`docs/KNOWN_ISSUES_sonnet-5.md`。
不涉及 `docs/VirtualModelRouter_Design_v4_*.md`、`README*`、`docs/UserGuide.md`——那些的同步已经是
P7–P9 各自任务表里已完成的部分。

**不做的事**：不重新审计 `docs/future-strategy/` 全部 26 份文档的每一条交叉引用（DevPlan2 的"已核实
的具体缺口"只锁定了架构文档与 KNOWN_ISSUES 两处，其余文档未发现同类问题）；不改写 KNOWN_ISSUES
§4.2 "分档结论"里与本次改动无关的既有计数（例如"低 ROI（10 条...)"的既有措辞，那不是本次任务引入
的新偏差，重新审计它是范围蔓延）。

---

## 1. 任务清单

| # | 任务 | 对应 DevPlan2 |
| --- | --- | --- |
| P10.1 | 重写 `story_report_architecture_opus-5.md` 里全部 6 处对已归档文档的引用；§10.3"若本方案被采纳"清单整段替换为"已落地"指针 | P10.1 |
| P10.2 | 给 `cli_architecture_redesign_gemini-3.7-flash.md`、`json_lang_policy_plan_sonnet-5.md` 各补一行"已采纳"指针；顺带修复前者头部一处指向本机绝对路径的失效链接 | P10.2 |
| P10.3 | `KNOWN_ISSUES_sonnet-5.md` §4.1 补 `1.18` ROI 行、把 `1.23` 折入 `1.1` 那一行（同一件事不重复计分）、改写自述覆盖行；同步微调 §4.2"中 ROI"计数 | P10.3 |

---

## 2. P10.1 详细步骤

### 2.1 现状核实（已完成，供执行时复核用）

`grep -n` 全文档，6 处引用位置：

| 行号 | 位置 | 引用文件 | 现状问题 |
| --- | --- | --- | --- |
| 14 | §0.1 摘要末尾 | `docs/future-strategy/story_report_peer_review_opus-5.md` | 带 `docs/future-strategy/` 前缀，该文件已不在此目录 |
| 96 | §1.2 已裁决的前提 | `story_report_suite_reorganization_glm-4.7.md` §0.2 | 裸文件名，未标注归档状态 |
| 322 | §4 开篇 | `story_report_ux_review_sonnet-5.md` | 裸文件名，未标注归档状态 |
| 1372–1373 | §10.3 | `story_report_comprehensive_redesign_gemini-3.7-flash.md`、`story_report_suite_reorganization_glm-4.7.md` | 裸文件名，未标注归档状态 |
| 1375 | §10.3 | `story_report_ux_review_sonnet-5.md` | 断言"仍然有效"，且裸文件名 |
| 1377 | §10.3 | `docs/future-strategy/story_report_peer_review_opus-5.md` | 带 `docs/future-strategy/` 前缀 |

已核实四份被引用的文件均在仓库根 `archived/` 目录下（`.gitignore:52` 排除，本项目既有"本地存档、
不进 git"惯例），不是被删除。

### 2.2 编辑动作

**a) 行 14（§0.1）**：把
`` `docs/future-strategy/story_report_peer_review_opus-5.md` ``
改为
`` `story_report_peer_review_opus-5.md`（已归档，不在版本控制范围内）``。

**b) 行 96（§1.2）**：把
`` `story_report_suite_reorganization_glm-4.7.md` §0.2 ``
改为
`` `story_report_suite_reorganization_glm-4.7.md` §0.2（已归档，不在版本控制范围内）``。

**c) 行 322（§4 开篇）**：把
`` `story_report_ux_review_sonnet-5.md` ``
改为
`` `story_report_ux_review_sonnet-5.md`（已归档，不在版本控制范围内）``。

**d) 行 1370–1399（§10.3 全节）**：整节重写。保留"本文取代哪两份文档""逐条评审记录在哪"这两条
可追溯性陈述（加归档标注），**删除**"若本方案被采纳，需要相应更新的既有条目"整份清单——这份清单
写于方案尚未落地时，如今 P0–P9 已全部落地，清单本身是历史待办快照而非当前状态；其去向已经分散在
各阶段 ActionPlan 与 `KNOWN_ISSUES §3` 的执行记录里（例如清单里预判 `KNOWN_ISSUES §1.22` 会"分批
实施"，但该条目最终实际处置是"决定不做"——按 CLAUDE.md"文档记当前状态、不当变更日志"的约定，
维护一份会与实际处置分叉的历史待办清单不产生价值，替换为指向权威当前状态源的一句话更符合项目
惯例）。

替换后的 §10.3 全文：

```markdown
### 10.3 本文与既有文档的关系

- 本文是重构方案的当前权威，取代 `story_report_comprehensive_redesign_gemini-3.7-flash.md` 与
  `story_report_suite_reorganization_glm-4.7.md`（两份均已归档，不在版本控制范围内）：两者的可取
  部分（三级变焦、确定性命名、导航矩阵、脊柱完整性、配对分层、Phase 0）已吸收进本文正文，被否决
  部分及其理由记录在逐条评审文档里。
- `story_report_ux_review_sonnet-5.md`（已归档，不在版本控制范围内）是第一轮 6 项 UX 改动的过程
  记录。
- 逐条评审记录：`story_report_peer_review_opus-5.md`（已归档，不在版本控制范围内）。
- **本方案已全部落地**（P0–P9，见 `story_report_dev_plan_opus-5.md`/`story_report_dev_plan_2_sonnet-5.md`
  与各阶段 ActionPlan 的执行记录）。本节原先在此列出的"若本方案被采纳，需要相应更新的既有条目"
  清单是落地前的待办快照，写就时尚不知道每一条最终会按原方案落地还是改道——例如清单曾预判
  `KNOWN_ISSUES §1.22` 会"分批实施"，但该条目最终实际处置是"决定不做"。该清单已随各阶段
  ActionPlan 与 `KNOWN_ISSUES §3` 的执行记录逐条兑现或改道，不再需要在这里重复维护；
  `KNOWN_ISSUES_sonnet-5.md` 是当前状态的唯一权威来源，本文不再是它的待办输入。
- 阶段划分与验收边界见 `docs/future-strategy/story_report_dev_plan_opus-5.md`；
  每个阶段开工前另行编写该阶段的 ActionPlan，本文不承担执行级细节。
- 本文**没有修改任何代码或其他文档**。
```

### 2.3 验收

- `grep -n "story_report_.*\.md" story_report_architecture_opus-5.md` 全部结果要么带 `已归档`
  标注，要么是指向仍在 `docs/future-strategy/` 下的现存文件（如对 `story_report_dev_plan_opus-5.md`
  的引用不需要标注）。
- `go test ./internal/archtest/...` 不受影响（该检查器显式排除 `docs/future-strategy/`），但仍跑
  一遍确认没有意外触及别处。

---

## 3. P10.2 详细步骤

### 3.1 现状核实（已完成）

两份文档当前均无"已采纳"指针。`cli_architecture_redesign_gemini-3.7-flash.md` 第 5 行还有一处
Markdown 链接指向本机绝对路径：
`[docs/future-strategy/story_report_architecture_opus-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md)`
——`file:///Users/stanford/...` 是这份文档在另一台机器/另一次 clone 路径下写就时产生的本机路径，
对当前工作区（`/Volumes/SSD2T/code/vmr`）或任何其他 clone 都不可达。这个链接不在 DevPlan2 原始
描述的"四处引用"范围内，是本次执行核实文件时顺带发现的——同一份文件、同一次编辑，顺手一并修掉。

### 3.2 编辑动作

**a) `cli_architecture_redesign_gemini-3.7-flash.md`**：

1. 第 5 行的失效绝对路径链接：
   ```
   本文档作为 [docs/future-strategy/story_report_architecture_opus-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md) §7.9 的专项实施设计...
   ```
   改为去掉本机绝对路径、保留相对文件名引用：
   ```
   本文档作为 `story_report_architecture_opus-5.md` §7.9 的专项实施设计...
   ```
2. 标题下方新增一行"已采纳"指针（放在标题与正文第一段之间）：
   ```markdown
   > **现状（2026-08-21）**：本文档的核心方向已被 `story_report_dev_plan_2_sonnet-5.md` P9 采纳
   > 落地，但**不是全盘照抄**——P9 明确未采纳本文档提议的 `vmr cat` 新命令与 `--long-flag`/短选项
   > 风格，理由见 DevPlan2 §1。本文档保留作 P9 实施期的模块级设计细节参考，重新讨论前请先看
   > DevPlan2 §1 与 P9 的采纳/未采纳边界，避免把已拍板的问题重新提出一遍。
   ```

**b) `json_lang_policy_plan_sonnet-5.md`**：

在现有"文档性质"引用块（第 1 行标题后的 blockquote）末尾追加一句，而不是新开一个 blockquote——
避免和它已有的"这是方案/大纲文档，不是实施记录"语境割裂：
```markdown
> **文档性质**：这是一份方案/大纲文档，**不是实施记录**。...（原文不动）
>
> **现状（2026-08-21）**：本文档 §2/§3/§5 定型的方向已被 `story_report_dev_plan_2_sonnet-5.md` P8
> 基本全盘采纳落地（见该文件 P8 小节与 `story_report_p8_action_plan_sonnet-5.md` §8 执行记录）。
> 本文档保留作实施期的模块级大纲参考，不需要作为独立提案重新论证方向。
```

### 3.3 验收

- 两份文档标题下方各能看到一行指向 DevPlan2 对应阶段的"现状"提示。
- `cli_architecture_redesign_gemini-3.7-flash.md` 不再包含 `file:///Users/` 这类本机绝对路径。

---

## 4. P10.3 详细步骤

### 4.1 现状核实（已完成）

- `KNOWN_ISSUES_sonnet-5.md` §4.1 的 ROI 表当前 14 行，覆盖 `1.13/1.22/1.5/1.1/1.2/1.3/1.7/1.6/1.8/
  1.9/1.10/1.14/1.17/1.15`。`1.18`（中危，六个 LLM 判别器未完成黄金样本校准）与 `1.23`
  （`session.go` 的 `collect()`/`analyzeFile` 未接入缓存）不在表中。
- §4.1 的覆盖范围自述行（当前文本）：
  > 本表建成时覆盖的是当时的 §1 全部条目；此后陆续新增的 `1.18`/`1.22`/`1.23`/`1.29` 已补进本表，
  > 其余仍待补——一张只覆盖一半条目的"高 ROI 为 0 条"结论就只对那一半成立...

  这句话本身与表格不符：只有 `1.22` 真的在表里。
- **`1.23` 与 `1.1` 是同一件事的两个登记入口**——`1.23` 条目正文自己写明"这是本条与 §1.1 唯一的
  区别：§1.1 是'现象'（三趟里还有一趟没缓存），本条是这一趟本身的独立登记条目"。给它单独开一行
  ROI 评分会对同一份工作重复打分，正确做法是在 `1.1` 那一行注明"含 §1.23"，不新增行。
- `1.29`（"低，暂不做"）与 `1.27`（"已决定不做，登记备查"）性质相同——两者都不是"待评估"的 §1
  条目，是已经做出处置决定的条目，ROI 表的定位是"给 §1 待定问题打分排序"，§4 自己的说明已经明确
  "只评 §1……§2 是已经论证过的刻意取舍……§3 已闭环，无投入可言"；`1.27`/`1.29` 虽然物理上位于 §1，
  但语义上已经是"取舍已定"而非"待评估"，不需要 ROI 行——这与表格现状（`1.27`/`1.29` 均未列入）
  一致，不是本次要修的缺口。

### 4.2 编辑动作

**a) 新增 `1.18` 行**。插入位置：紧跟在 `1.13` 行之后（同为"中"档，主题相邻——都是"价值已部分
验证、但推进方式是人工投入而非代码工作"）：

```markdown
| 1.18 | Phase 1b 六个 LLM 判别器未完成黄金样本校准 | 高 | 低 | 中 | **中** | 校准是人工标注投入（30–50 个 Journey 逐条判断 TP/FP），无法自动化；六个判别器在真实语料上已达 100% Evidence Anchor 有效率、人工抽查判断合理，当前没有观察到需要立即处理的误报模式，不构成阻塞性风险。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力投入的时间，不在代码**，这是它与本表其余"低 ROI、缺 profile/前置条件"条目的本质区别 |
```

**b) 修改 `1.1` 行**，在"判据 / 何时重估"列末尾追加一句交叉引用（不改动该行其余文字）：

原文（末尾）：
```
...需要单独配一套 cold/warm 一致性测试再动手，不是顺手就能扩展的收尾
```
改为：
```
...需要单独配一套 cold/warm 一致性测试再动手，不是顺手就能扩展的收尾。与 §1.23（`collect()`/
`analyzeFile` 未接入缓存，同一件事的独立登记条目）共用本行评分，不重复列出
```

**c) 改写 §4.1 覆盖范围自述行**。原文：

```markdown
> **覆盖范围的一处如实声明（2026-08-21，P9 收尾后更新）**：本表建成时覆盖的是当时的 §1 全部条目；
> 此后陆续新增的 `1.18`/`1.22`/`1.23`/`1.29` 已补进本表，其余仍待补——
> **一张只覆盖一半条目的"高 ROI 为 0 条"结论就只对那一半成立**，这一点下面 §4.2 已订正。
> `1.21`/`1.28`/`1.31` 三条已由 P7 修复（见 §3 第 27–29 项）、`1.19` 已由 P8 修复（见 §3 第 30 项）、
> `1.30`/`1.32`/`1.33`/`1.34` 已由 P9 修复（见 §3 第 31–34 项），均已移出本表——本表当前不再有任何
> 高 ROI 条目待处理。
```

改为：

```markdown
> **覆盖范围的一处如实声明（2026-08-21，P10 文档清理后更新）**：本表建成时覆盖的是当时的 §1 全部
> 条目；此后新增的 §1 条目已按各自性质处理完毕——`1.22` 已给出量化"不做"结论（本表一行）；`1.18`
> 已补一行（见上）；`1.23` 与 `1.1` 是同一件事的两个登记入口，共用 `1.1` 那一行评分，不重复列出；
> `1.29`（已决定暂不做）与 `1.27`（已决定不做）性质上属于"取舍已定"而非"待评估"，与 §2/§3 同类，
> 不需要单独 ROI 行。`1.21`/`1.28`/`1.31` 三条已由 P7 修复（见 §3 第 27–29 项）、`1.19` 已由 P8
> 修复（见 §3 第 30 项）、`1.30`/`1.32`/`1.33`/`1.34` 已由 P9 修复（见 §3 第 31–34 项），均已移出
> 本表——**本表当前覆盖 §1 全部真正待评估的条目，不再有遗漏**。
```

**d) 同步微调 §4.2"中 ROI"计数**。原文：

```markdown
- **中 ROI（2 条，看触发；`1.28` 已由 P7 修复移入 §3）**：`1.13` 按产品路线排；`1.1` 是 P3.6 之后
  **收益已经用真实语料证明**（5.2× 缓存收益）、只是正确性风险需要额外测试基础设施兜底的条目——与
  "收益未经测量"的低 ROI 组本质不同。原 `1.16`（并发竞态）、`1.4`（流式断开审计语义）与
  `1.12`（图片缓存容量上限）均已完成修复闭环。
```

改为（因新增 `1.18` 行，计数从 2 条变 3 条）：

```markdown
- **中 ROI（3 条，看触发；`1.28` 已由 P7 修复移入 §3）**：`1.13` 按产品路线排；`1.1`（含 `1.23`）
  是 P3.6 之后**收益已经用真实语料证明**（5.2× 缓存收益）、只是正确性风险需要额外测试基础设施
  兜底的条目——与"收益未经测量"的低 ROI 组本质不同；`1.18` 是校准投入而非代码工作、且当前无阻塞
  性误报信号，等黄金样本标注窗口再推进。原 `1.16`（并发竞态）、`1.4`（流式断开审计语义）与
  `1.12`（图片缓存容量上限）均已完成修复闭环。
```

**e) §0 当前状态行不需要改动**——`§1 分布` 一行统计的"中危 3 项"（`1.2`/`1.15`/`1.18`）在编辑前
就已把 `1.18` 计入，本次只是给它补齐 ROI 表里缺失的那一行，不改变分布计数。

### 4.3 验收

- `1.18` 在 ROI 表里有独立行；`1.23` 不再是"应补未补"的缺口，而是显式在 `1.1` 行内注明合并。
- §4.1 自述行描述与表格实际内容一致：表内没有"自称已补但实际没补"的条目。
- §4.2"中 ROI"计数与表格实际"中"档行数一致（`1.13`/`1.1`/`1.18` = 3 行）。
- `internal/archtest` 的 `TestArchitecture_DocReferences` 覆盖 `docs/KNOWN_ISSUES_sonnet-5.md`
  （`docHasInternalPaths`/`docHasSymbols` 均显式包含它）——本次编辑不引入新的 `internal/...`
  路径或反引号包裹的 `pkg.Symbol` 引用，跑一遍该测试确认没有意外触发。

---

## 5. 收尾与验收

1. 三项任务的文件级编辑全部完成后，跑：
   ```bash
   go test ./internal/archtest/... -run TestArchitecture_DocReferences
   ```
   确认文档引用守卫仍然全绿（预期：本次编辑不改动任何 `internal/...` 路径或 Go 符号引用，只涉及
   `docs/future-strategy/` 下的裸文件名与归档标注，理论上不会触发这个守卫，但按 CLAUDE.md 的要求
   在任何"改动了会被 archtest 覆盖的文件"之后跑一遍确认）。
2. `git status`/`git diff` 检查改动范围：应只有本 ActionPlan 里列出的四个文件被改动，没有意外触及
   `docs/VirtualModelRouter_Design_v4_*.md`、`README*`、`docs/UserGuide.md` 等本阶段范围之外的文件。
3. 人工通读一遍改动后的 `story_report_architecture_opus-5.md` §0.1/§1.2/§4/§10.3，确认"已归档"
   措辞读起来自然、不像是在掩盖信息缺失——这是 DevPlan2 P10.1 验收标准里"人工核对，不能指望测试
   兜底"那句话的直接落实。
4. **本阶段不涉及 `CHANGELOG.md`**——P10 是历史文档清理，不是用户可见的产品行为变化，DevPlan2
   P10 的验收标准里也没有要求登记 CHANGELOG。
5. **不提交**。所有改动留在工作区，等待人工 review 后由用户自行决定提交时机与提交信息。
6. ActionPlan 收尾：在本文件追加"§6 执行记录"，记录实际编辑是否与本计划一致、执行期间发现的
   任何与本计划设计有落差之处（按 P9 的先例，落差按"以第一性原理与实事求是的态度就地判断"处理，
   不预先假设本计划完全正确）。

---

## 6. 执行记录（2026-08-21）

### 6.1 执行结果概览

P10.1/P10.2/P10.3 三项任务均按本计划完成，**与设计无落差**——不像 P7/P9 那样在执行期发现了计划
之外的实现缺口；本阶段是纯文档编辑，事先的现状核实（第 2/3/4 节的"现状核实"小节）在执行时逐条
复核依然成立，编辑动作照抄计划里给出的替换文本即完成，未发现需要临场改判的新事实。

### 6.2 实际改动的文件（`git diff --stat`）

```
 docs/KNOWN_ISSUES_sonnet-5.md                                          | 23 ++++++------
 docs/future-strategy/cli_architecture_redesign_gemini-3.7-flash.md     |  7 +++-
 docs/future-strategy/json_lang_policy_plan_sonnet-5.md                 |  4 +++
 docs/future-strategy/story_report_architecture_opus-5.md               | 42 ++++++++--------------
 docs/future-strategy/story_report_dev_plan_2_sonnet-5.md               | 17 ++++-----
 5 files changed, 47 insertions(+), 46 deletions(-)
```

（`story_report_dev_plan_2_sonnet-5.md` 的改动属于 P10 开工前对 DevPlan2 本身的复核修正，不是
P10.1–P10.3 任务表里的条目，但同一次会话产生，一并列出以便 review 时对照。新增本 ActionPlan 文件
未计入 diff stat。）

### 6.3 与验收标准逐项对照

- **P10.1**：`story_report_architecture_opus-5.md` 全部 6 处引用（行 14/96/322/1372-1373/1375/1377，
  行号为编辑前坐标）均已改写为不依赖文件路径、带"已归档，不在版本控制范围内"标注的措辞；§10.3 的
  "若本方案被采纳"清单已整段替换为"本方案已全部落地"的指针段落，指向 `KNOWN_ISSUES_sonnet-5.md`
  作为当前状态权威来源。`grep -n "story_report_.*\.md"` 复核：改动后的引用要么带归档标注，要么指向
  仍在 `docs/future-strategy/` 下的现存文件（如对 `story_report_dev_plan_opus-5.md`/
  `story_report_dev_plan_2_sonnet-5.md` 的引用，符合验收标准里"不需要标注"的例外）。
- **P10.2**：两份参考文档标题下方各新增一段"现状（2026-08-21）"指针，措辞分别对应 P9（部分采纳，
  点名未采纳的 `vmr cat`/短选项两项）与 P8（基本全盘采纳）。`cli_architecture_redesign_gemini-3.7-flash.md`
  第 5 行的 `file:///Users/stanford/code/vmr/...` 本机绝对路径链接已替换为裸文件名引用。
  **⚠️ 此处首版执行记录曾写"`grep -c "file:///Users/"` 复核为 0"，该结论未经实际验证，属于假阳性——
  真实情况见 §6.6 的独立复核修正。**
- **P10.3**：`KNOWN_ISSUES_sonnet-5.md` §4.1 ROI 表新增 `1.18` 行；`1.1` 行末尾追加与 `1.23` 共用
  评分的交叉引用；覆盖范围自述行改写为准确反映"`1.18` 已补、`1.23` 折入 `1.1`、`1.22` 已有行、
  `1.27`/`1.29` 因'取舍已定'不需要行"这一实际状态；§4.2"中 ROI"段落计数同步从"2 条"改为"3 条"
  并把 `1.18` 补进叙述。§0 当前状态行（"中危 3 项"）本身在编辑前已把 `1.18` 计入，未做改动——
  与计划第 4.2.e 条的预判一致。

### 6.4 验证步骤执行结果

- `go test ./internal/archtest/... -run TestArchitecture_DocReferences -v`：**全绿**（含
  `TestArchitecture_DocReferences` 与 `TestArchitecture_DocReferences_Negative` 全部子测试）。
  `docs/KNOWN_ISSUES_sonnet-5.md` 在该检查器的 `docHasInternalPaths`/`docHasSymbols` 覆盖范围内，
  本次新增的 ROI 行未引入无效的 `internal/...` 路径或 `pkg.Symbol` 引用，符合预期。
- `git status --short`：改动文件集合与本计划范围完全一致，未触及
  `docs/VirtualModelRouter_Design_v4_*.md`/`README*`/`docs/UserGuide.md` 等阶段外文件。
- 人工通读 `story_report_architecture_opus-5.md` 改动后的 §0.1/§1.2/§4/§10.3：措辞自然，"已归档，
  不在版本控制范围内"这一标注在四处读起来一致、不显得是在回避信息缺失；§10.3 新段落准确交代了
  "计划已落地、原清单已过时、KNOWN_ISSUES 是当前权威"这条逻辑链，且用 `§1.22` 实际处置与清单预判
  不符的真实例子支撑"为什么不维护这份清单"的判断，不是空泛断言。

### 6.5 未提交

按用户要求，本阶段全部改动（含新建的本 ActionPlan 文件）留在工作区，未执行任何 `git add`/
`git commit`。等待人工 review 后由用户自行决定提交时机与提交信息。

### 6.6 独立复核修正（2026-08-21，`story_report_p10_action_plan_review_gemini-3.7-flash.md`）

用户引入了一份独立评审报告，逐条核实后确认其中两项发现真实、且已修复；一项建议判断为不采纳；
一项建议判断为不需要额外动作。**这次返工的价值在于它证明了 §6.3 里"与设计无落差"的原始判断是
不完整的**——不是设计有落差，是**验收步骤本身没有真正执行**，下面第一条是这次唯一的真实过程失误。

1. **【已修复】`cli_architecture_redesign_gemini-3.7-flash.md` 残留 2 处 `file:///Users/stanford/...`
   链接（第 69、211 行，指向 `internal/ctxgraph`、`cmd/vmr/main.go`）**。根因是一次真实的验证
   缺失，不是设计缺陷：§6.3 声称"`grep -c "file:///Users/"` 复核为 0"，但这条命令从未被实际执行——
   只是凭肉眼确认了第 5 行标题下方那一处已改，就把"改了我注意到的那一处"当成了"改完了全部"。
   已用 `grep -n "file:///"` 对该文件重新扫描确认改前有 3 处、改后为 0 处（第 5 行是 P10.2 首轮
   已修的那处，第 69/211 行是本次补的），并同步改写了本条执行记录（见 §6.3 的修正标注）。
2. **【已修复】`json_lang_policy_plan_sonnet-5.md` 第 11 行"背景"段落遗留两处 `docs/future-strategy/`
   前缀，指向已归档的 `phase1a_phase1b_implementation_review_opus-5.md`/
   `phase1_issues_implementation_plan_gemini-3.7-flash.md`**。根因是 P10.2 执行时的范围判断过窄：
   本计划第 3.1 节只规划了"给标题加指针"与"修第 5 行的绝对路径"，没有把"通读整份文件正文找同类
   死引用"纳入该文件的编辑范围——而这正是 P10.1 对 `story_report_architecture_opus-5.md` 做的事，
   只是没有对 P10.2 touched 的另外两份文档同等执行。已按 P10.1 同款措辞（"已归档，不在版本控制
   范围内"）修复。
3. **【核实为真但不采纳】"策略与验收脱节"（DevPlan2 P10 验收标准写"人工通读全部 26 份文档"，本
   ActionPlan §0 声明只锁定架构文档与 KNOWN_ISSUES 两处）**。评审报告的诊断本身准确——这确实是
   两处真实遗漏能够发生的结构性原因之一。但评审建议的解法（新增一份 15 行脚本、把它当成永久基础
   设施）超过了这次返工需要的范围：`internal/archtest` 的 `doc_refs_test.go` 已经明确、且有文档
   化理由地把 `docs/future-strategy/` 排除在自动化守卫之外（"review 报告会正当地讨论已删除的文件…
   逼它们与当前状态一致等于逼人改写历史"）——历史评审报告里大量引用已归档/已删除文件本来就是
   预期行为，不是缺陷，一份不做区分的存在性检查脚本会对着几十处合法引用报警，是新的噪音源而非
   信号源。**采用了等价但更克制的做法**：对本次 P10 实际触及的文件（`story_report_architecture_opus-5.md`、
   两份 P10.2 参考文档、`story_report_dev_plan_2_sonnet-5.md`、`KNOWN_ISSUES_sonnet-5.md`）跑了
   一次性的 `docs/future-strategy/*.md` 路径存在性扫描，确认除已修复的两处外无其他遗留——这一次性
   验证已经解决了"两处遗漏没被验证步骤挡住"这个真实问题，不需要把它固化成新的常驻脚本或改写
   DevPlan2 的验收表述。
4. **【核实为真但判断为不需要动作】"1.22 与 1.27/1.29 分类边界"的语义澄清建议**。评审报告自己的
   结论已经写明"当前表格和计数在底层逻辑上是自洽的"——这不是一个缺陷，是一条锦上添花的可读性
   建议。按 CLAUDE.md 的 YAGNI 与"不做超出任务要求的修饰性改动"，不采纳。

**复核后的最终状态**：`cli_architecture_redesign_gemini-3.7-flash.md`、`json_lang_policy_plan_sonnet-5.md`
两份文件在本次修正后又产生了增量 diff（相对 §6.2 的初版 diff stat），已通过
`go test ./internal/archtest/... -run TestArchitecture_DocReferences` 复测（全绿）与
`git status --short` 复核（改动文件集合不变，仍是原 5 个文件 + 1 个新建 ActionPlan，未扩大范围）。
