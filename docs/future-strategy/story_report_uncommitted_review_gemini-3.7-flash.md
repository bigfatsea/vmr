<!-- Ver 2026-08-21 15:40, by Gemini 3.7 Flash -->

# 工作区未提交改动全面复审报告 (Uncommitted Changes Review)

## 0. 复审背景与范围

### 0.1 复审定位
本复审针对当前工作区所有**未提交（Uncommitted）**的修改内容进行全面复核。
本轮未提交的修改定位为：
1. **战略与复核文档体系更新**：回填架构文档、完成三份独立评审的核实分流（Triage）、更新 `KNOWN_ISSUES` 清单与后续开发计划（DevPlan P11–P15）；
2. **零风险随手小修与代码卫生**：修复代码死引用、补充叶子包依赖守卫、收紧文件行数预算、消除 nil 解引用风险、清理未引用的 i18n 废弃字段等；
3. **不做既有大方案的越权重构**：本轮不包含也不应当包含后续 P11–P15 的重构代码落地，重点在于检查当前未提交改动**自身**的正确性、前后一致性、数据准确性及逻辑闭环度。

### 0.2 复审对象清单（15 份文件）

| 分类 | 文件 | 变动性质 |
| --- | --- | --- |
| **代码与测试 (Go)** | `internal/story/render_spine_step.go` | nil 解引用防御修复（`s.Manifest` 时间戳与链接） |
| | `internal/report/detail.go` | 注释修正（反映 P3.1 删除 `.json`） |
| | `internal/story/render_md.go` | 注释修正（消除死引用与 tag 矛盾描述） |
| | `internal/fmtutil/fmtutil.go` | 注释修正（消除死引用） |
| | `internal/chatmsg/messages.go` | 注释修正（消除死引用） |
| | `internal/chatmsg/pairing.go` | 注释修正（明确 OpenClaw 0% 与 F9 不变量关系） |
| | `internal/i18n/reqdetail_detail.go` | 删除全仓零引用的 `NoValue` 结构体字段及赋值 |
| | `internal/archtest/file_sizes_test.go` | 收紧 `report/detail.go` 行数豁免预算（1150 → 350） |
| | `internal/archtest/import_boundaries_test.go` | 增加 `internal/reqdetail` 禁止依赖边界守卫 |
| | `cmd/vmr/cmd_report.go` | 更新 `-report-config` 命令行 help 文案 |
| | `cmd/vmr/cmd_story.go` | 更新 `-report-config` 命令行 help 文案 |
| **全局与门面文档** | `CLAUDE.md` | 更新分析半区主入口为 `vmr analyze`、补齐 `i18n` 目录映射 |
| **架构与战略文档** | `docs/future-strategy/story_report_architecture_opus-5.md` | 回填 §7.4（作废 <50KB）、§7.5、§7.6c（详单内减法）、§7.9 |
| | `docs/KNOWN_ISSUES_sonnet-5.md` | 补入 `§1.35`–`§1.43`、更新 ROI 总表、补全 `## 2` 标题、撤回 `§1.32` 闭环 |
| | `docs/future-strategy/story_report_review_triage_opus-5.md` (未跟踪) | 三份独立审阅报告（Terra/Gemini/Sonnet）的核实分流报告 |

---

## 1. 自动化与构建健康度核验

针对当前工作区状态进行了全量自动化测试与静态检查，全部通过：

1. `go build ./...`：**PASS**（全部 36 个 package 编译无 warning）
2. `go test ./...`：**PASS**（全量单元与集成测试全部绿灯）
3. `go test ./internal/archtest/...`：**PASS**（单向依赖边界、文件行数、函数行数、文档引用全绿）
4. `go vet ./...`：**PASS**（零 vet 告警）
5. `gofmt -l .`：**PASS**（代码格式 100% 符合规范）

---

## 2. 发现的问题、疏漏与矛盾清单

### 2.1 【中】`KNOWN_ISSUES_sonnet-5.md` §0 统计数字存在算术不一致

* **位置**：`docs/KNOWN_ISSUES_sonnet-5.md` 第 22 行
* **原文**：
  > `§1 分布（2026-08-21 全面复查 + 三份独立审阅报告核实后重算）：高危 3 项（1.35/1.36 同属"证据层体积纪律"，1.41 是"渲染表示正确性"且是前两项的硬前置）、中危 8 项、低危 17 项（另有 1 项已评估决定不做，登记备查，不计入分布）。`
* **问题核实**：
  * 实数 §1 正文中的全部条目（共 27 项）：
    * **高危 3 项**：`1.35`、`1.36`、`1.41`
    * **中危 8 项**（按 ROI 表与分档）：`1.1`、`1.13`、`1.18`、`1.37`、`1.38`、`1.39`、`1.42`、`1.43`
    * **有效低危 15 项**：`1.2`、`1.3`、`1.5`、`1.6`、`1.7`、`1.8`、`1.9`、`1.10`、`1.14`、`1.15`、`1.17`、`1.22`、`1.23`、`1.29`、`1.40`
    * **另有 1 项**：`1.27`（登记备查不做，不计入分布）
  * 合计：`3 + 8 + 15 = 26`（若计入 `1.23` 与 `1.1` 合并后算 15 项，若单列为 16 项）。
  * 文档中写出的 **"低危 17 项"** 与实际条目数不符（`3 + 8 + 17 = 28`，超出了总条目数）。
* **根因分析**：上一版本为「中危 3 项、低危 14 项」。本次新增 9 项条目（3 高、5 中、1 低）时，在计算低危数字时误将新增的高危 3 项加到了原低危 14 项上（`14 + 3 = 17`），而非加上新增的低危条目（`14 + 1 = 15`）。
* **建议**：订正为 `中危 8 项、低危 15 项`（或列明 15 项有效待评估 + 1 项持续约束 1.15）。

---

### 2.2 【中】`story_report_architecture_opus-5.md` §7.6c 详单体积构成表标签与数值对调

* **位置**：`docs/future-strategy/story_report_architecture_opus-5.md` 第 1004–1010 行
* **原文表格**：
  ```markdown
  | 组成 | 字节 | 占比 | 性质 |
  | --- | --- | --- | --- |
  | `Raw SSE, full` 折叠块 | 62.4MB | **41.1%** | `rec.Client.Response.Body` 的逐字复制 |
  | ① 段第一个 `🆕` 标记之前的历史消息 | 79.2MB | **52.1%** | 上一轮详单已经渲染过一遍的同一批消息 |
  | 本轮真正新增的内容 | ~6.7MB | 4.4% | — |
  | 其余（②attempts、③重组输出、头部） | ~3.7MB | 2.4% | — |
  ```
* **问题核实**：
  * 对照 `story_report_full_review_opus-5.md` §2.2（第 408 行）原始实测基线数据：
    * ① Client → VMR Request 段总计 **82.9MB（54.6%）**，其中历史消息占 **79.2MB**，本轮新增的 Prompt/Message 增量占 `82.9 - 79.2 =` **3.7MB（2.4%）**；
    * 其余部分（②attempts、③重组输出、文档头部）总计 **~6.7MB（4.4%）**；
  * `architecture_opus-5.md` 在补入第 5 条时，将 **3.7MB** 和 **6.7MB** 的标签与数值对调了：
    * 它将 6.7MB 标注为了 "本轮真正新增的内容"；
    * 将 3.7MB 标注为了 "其余（②attempts、③重组输出、头部）"。
* **根因分析**：制表时将「① 段内部的新增增量（3.7MB）」与「① 段之外的解读/尝试内容（6.7MB）」的数值混淆颠倒。
* **建议**：调整为：
  * `① 段本轮新增消息增量`：`~3.7MB`（`2.4%`）
  * `其余（②attempts、③重组输出、头部）`：`~6.7MB`（`4.4%`）

---

### 2.3 【低】`story_report_review_triage_opus-5.md` §5.4 标题小计与表格实际行数不符

* **位置**：`docs/future-strategy/story_report_review_triage_opus-5.md` 第 580 行
* **原文标题**：`### 5.4 D 组 — 不采纳（4 条，均记录理由以免重提）`
* **问题核实**：
  * 该小节正文表格中实际列出了 **7 项** 不采纳的建议：
    1. 详单/证据文件名改用 SHA-256 ≥96bit (Terra P2)
    2. 为 OpenAI 工具返回加 error 嗅探 (Gemini P1-1)
    3. `loadStoriesLink` 改为内存透传 (Gemini P2-1)
    4. `.parse-cache/`/`evidence/` 加孤儿回收 (Gemini P2-2)
    5. 承认三条稳定入口、去掉 deprecated 叙述 (Terra P3(a))
    6. Cron 复合判定阈值升级 (Gemini P1-2)
    7. 为所有 future-strategy 文档加路径检查脚本 (Terra P4 后半)
* **根因分析**：初稿梳理时列了 4 项，后续将另外 3 项被否决的方案补充进表时，未同步更新小节标题中的数字计数。
* **建议**：小节标题订正为 `### 5.4 D 组 — 不采纳（7 条，均记录理由以免重提）`。

---

### 2.4 【低】全量语料记录数表述存在微小历史残留（11,374 vs 11,274）

* **位置**：
  * `docs/future-strategy/story_report_review_triage_opus-5.md` 第 128 行（`以约 11,374 个独立值估算`）
  * `docs/KNOWN_ISSUES_sonnet-5.md` 第 478 行（`34 文件/11374 条记录` 与后文 `11274 行的 vmr-requests.json`）
* **问题核实**：
  * `story_report_full_review_opus-5.md` 实测基线表（M10/M14）及 `review_triage` 第 37 行已明确全量 34 文件真实语料总请求数为 **11,274 条**。
  * `11,374` 系早期粗估或打字颠倒留下的陈旧数值。
* **建议**：在后续清理时统一收敛为精确测量值 `11,274`。

---

### 2.5 【提示/体例】`KNOWN_ISSUES` 节标题严重级标记与 ROI 表分类层级存在细微语意摩擦

* **位置**：`docs/KNOWN_ISSUES_sonnet-5.md` §1 子标题 vs §4.1 ROI 表
* **细节**：
  * `### 1.13 [低] 额度燃尽看板未交付` vs ROI 表中 `ROI: 中`（§4.2 列入 `中 ROI (8 条)`）；
  * `### 1.1 [低，已部分闭环]` vs ROI 表中 `ROI: 中`；
  * `### 1.39 [中低]` / `### 1.43 [中低]` vs ROI 表中 `ROI: 中`。
* **分析**：
  * §1 子标题中的中括号标记（`[低]`、`[中]`、`[高]`）在历史维护中多代表「严重程度/紧急度」，而 §4 是从「价值 ÷（成本 + 风险）」计算出的 ROI；
  * 虽然文档在 §4 引言中解释了评分口径，但在读者初读时，同一条目头部写 `[低]`、文末总结归入 `中 ROI` 会产生轻微的认知跳跃。
* **建议**：在后续版本可考虑统一标题括号内的语义（例如标明 `[待定/低紧急]` 或直接与 ROI 分档对齐）。

---

## 3. 代码修改细项深度审查

| 文件与修改行 | 审查要点 | 结论与分析 |
| --- | --- | --- |
| `internal/story/render_spine_step.go` (`spineStepHeader`) | `s.Manifest != nil` 检查前移，保护 `s.Manifest.TS` 格式化 | **正确**。原代码在检查前无条件解引用 `s.Manifest.TS`，导致 nil 检查失效。修改后 `s.Manifest == nil` 时 `ts` 为空字符串且不生成 detail link，`stepRoleTag` 内部亦无 `Manifest` 解引用，彻底杜绝 panic。 |
| `internal/report/detail.go` (`DetailWriter`) | 注释更新，消除 `.json` 描述 | **正确**。准确反映 P3.1 删去 `.json` 同构副本的事实，与 `writeOneDetail` 保持一致。 |
| `internal/story/render_md.go` (`codeFence`) | 注释更新，修正迁移路径与语言 tag 描述 | **正确**。将已失效的 `internal/report/render.go` 引用改为 `internal/reqdetail/render.go`，并合并了此前自相矛盾的 tag 说明。 |
| `internal/fmtutil/fmtutil.go` (`FmtPercent`) | 注释更新，消除死引用 | **正确**。去除了已废弃的 `internal/report/render.go's fmtBytes`。 |
| `internal/chatmsg/messages.go` | 注释更新，修正文件位置 | **正确**。准确说明文件迁移历史。 |
| `internal/chatmsg/pairing.go` (`PairingReport`) | 注释重构，厘清 F9 与 OpenClaw 0% 关系 | **正确**。严谨指出数据不变量成立于记录的 ID，解释了严格匹配与归一化两趟匹配的用途边界，解决了注释与生产实际的矛盾。 |
| `internal/i18n/reqdetail_detail.go` | 删除 `NoValue string` | **正确**。经全仓 grep 确认零引用，纯属历史残留，安全清理。 |
| `internal/archtest/file_sizes_test.go` | 收紧 `report/detail.go` 豁免至 350 行 | **正确**。P2 重构拆分后该文件为 282 行，及时收紧豁免阈值防止代码膨胀回潮。 |
| `internal/archtest/import_boundaries_test.go` | `reqdetail` 禁向导入 `report/story/router/server/config` | **正确**。从架构测试层面锁死 `reqdetail` 作为通用叶子包的定位，防止反向依赖。 |
| `cmd/vmr/cmd_report.go` & `cmd_story.go` | help 文案强调 `vmr analyze` | **正确**。契合 P9 CLI 收敛后的用户预期。 |
| `CLAUDE.md` | 全局门面同步 | **正确**。如实反映 `vmr analyze` 成为主入口、别名降级及 `i18n` 文件映射。 |

---

## 4. 架构决策与后续规划第一性原理再审视

在阅读并核对全部未提交文档（`story_report_architecture_opus-5.md`、`story_report_full_review_opus-5.md`、`story_report_review_triage_opus-5.md`）的过程中，对其中若干核心架构裁决进行了独立第一性原理审视：

### 4.1 详单跳过谓词从「假设」到「校验」（Package B / B4）
* **既有决策**：在详单内写入轻量机器可读渲染指纹（语言 + evidence 链接模式 + 模板版本），`EnsureRendered` 命中 `Stat` 后读首行指纹校验，不匹配则原子重写。明确否决了 sidecar 方案与文件名包含语言方案。
* **独立复核**：**完全认同**。
  * 文件名必须仅由 `Manifest` 决定（zero-I/O 渲染链接的核心不变量）；
  * 引入 sidecar 会使文件数翻倍，再次制造 P3.1 极力消除的同构碎片；
  * 首行指纹读取仅需 ~64 字节读取，相对错误内容（如中文报告配英文详单、45 条链接因 evidence 未重建而永久失效）而言，成本极低且彻底杜绝脏状态。

### 4.2 默认批量分析不物化详单（Package A / A1）
* **既有决策**：`writeJourneyFile` 增加物化控制，默认套件批量渲染时只计算并挂载详单链接，不写入 `details/*.md`；单条 `-journey` 或显式 `-details` 时才物化。
* **独立复核**：**完全认同**。
  * 详单链接目标文件名由 `reqdetail.FileNameForManifest` 纯计算得出，不需要文件实际存在即可闭合；
  * 默认批量物化详单直接导致单日产出 164MB / 全量 3.5GB，其中绝大多数详单用户根本不会点开；
  * 这一改动直接让默认分析产物回归索引量级（~2.6MB / ~58MB），且实现仅在 `cmd/vmr` 一层，风险极低。

### 4.3 详单内容减法：坐标替代 Raw SSE + 历史消息增量化（Package A / A2, A3）
* **既有决策**：Raw SSE 替换为 `vmr replay -req COORD -print` 坐标取用提示；历史消息折叠为指向上一轮详单的 `PrevTurnLink`。
* **独立复核**：**方向正确，且取舍明确**。
  * 详单 93% 为重复数据的实测是扎实的；
  * 虽然让「单份详单自包含」变成了「顺链回溯」，但已有 `SysHash` 走共享 evidence 引用的先例，且首条记录保留全量，链条有锚点；
  * 必须严格遵守 DevPlan 中指出的前置依赖：**B4（指纹校验）必须先于 A2/A3 交付**，否则在已有输出目录下旧详单不会更新。

### 4.4 CLI 别名薄化与分派收敛（Package C）
* **既有决策**：先在 `vmr analyze` 补齐「只跑宏观」与「只列候选」两个模式，再将 `vmr report`/`vmr story` 薄化为纯参数转换与转发。
* **独立复核**：**完全认同**。
  * 拒绝了 Terra 建议的「承认三入口、推翻收敛决策」的妥协方案；
  * 识别出了当前代码「同一句话既称 deprecated alias 又称 remains fully supported」的自相矛盾；
  * 坚持「先具备等价能力、再薄化别名」，保证了向后兼容性与产物逐字节不变。

---

## 5. 复审总结 (Summary)

1. **整体评价**：
   本次工作区未提交的内容整体质量极高，工程逻辑扎实，证据严谨（每一项裁决均有真实语料实测数据作为支撑），全面守住了 `go test`、`archtest`、`go vet` 和 `gofmt` 的自动化红线。
2. **处理定位清晰**：
   改动准确执行了「更新战略与评审文档 + 顺手处理无风险小修」的定位，没有越权实施未经 ActionPlan 分析的大规模重构。
3. **发现的问题性质**：
   本次复审发现的 4 处问题均为**文档层面的统计不一致、制表数值颠倒及数字笔误**（`KNOWN_ISSUES` 低危数量算术偏差、架构文档详单构成表 3.7MB 与 6.7MB 标签颠倒、Triage 报告标题数字滞后、11,374 历史残留），代码与测试层面未发现任何功能缺陷或回归风险。
4. **后续建议**：
   在后续开启 P11（清理与守卫先行）阶段之前，可顺手修正上述 4 处文档笔误，确保文档系统处于完全自洽的状态。
