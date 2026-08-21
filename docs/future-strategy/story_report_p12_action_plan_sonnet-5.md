// Ver 2026-08-21, by Sonnet 5

# P12 · 渲染表示的正确性 — ActionPlan

> 依据：`story_report_full_review_opus-5.md` 第 6 章「后续开发计划（P11–P15）」§6.5 对 P12 的定义
> （目标、范围、任务表、阶段验收），以及该文 §2.7/§2.8、Package B、
> `KNOWN_ISSUES_sonnet-5.md` §1.37/§1.41 的具体依据。P11 已完成（commit `b771043`），本阶段是
> P11 之后的第一个阶段。
>
> 本计划不沿用上述文档对任务范围的字面判定——按 DevPlan §6.1 的约定，**每个阶段开工前基于该阶段
> 起点的真实仓库状态重新分析**。下面 §0 记录了这次重新分析在原判定基础上发现的出入。

---

## 0. 开工前重新核实：与原计划的出入

对 §1.41（跳过谓词）和 §1.37（`<summary>` 未转义）逐行核实了当前源码（`internal/reqdetail/ensure.go`、
`render.go`、`detail.go`，`internal/story/render_spine_step.go`、`render_spine_args.go`），
两条现状描述与代码完全一致，P11 的清理没有touch这两个文件的相关逻辑（P11 diff 里
`internal/reqdetail/evidence.go`/`facts.go` 的改动是删除 `contentHash8`/`ErrorClass`，与
`EnsureSysPromptEvidence`/`EnsureToolsEvidence`/`EnsureRendered`/`escapeHTML` 均无交集）。**核实
未发现任何需要推翻的判定**——这与 P11 §0 大幅收窄范围的情况不同。

但通读 `render_spine_args.go`/`render_spine_step.go` 全文（而不只是 KNOWN_ISSUES 点名的行号）时，
发现同一文件里还有 **4 处此前未被登记、同属"原文直接写进 Markdown 正文，无转义无围栏"这一类**的
注入点，且都在本阶段本就要修改的两个文件内：

| # | 位置 | 内容 | 未被登记的原因（推测） |
| --- | --- | --- | --- |
| N1 | `render_spine_args.go` `toolCallLine` 的 `allShort` 分支（第 165–171 行） | 短标量字段拼成 `key=value, key=value` 直接输出 | 复查聚焦"折叠块"（`<details>`）与两处"inline 分支"，这是第三种形态——多字段拼接，字面上不是"一处 inline 分支" |
| N2 | `render_spine_step.go` `renderDecisionSpine` 的 `t.SpineTaskLine(ti+1, task.Title)`（第 169 行） | Task 标题，来自 `taskseg.LastInstruction`/`FirstInstruction`（真实用户输入，只做空白折叠+截断，见 `taskseg/segment.go` 的 `Preview`） | 标题类内容分散在 i18n 模板函数里，不在"折叠块渲染"这个搜索范围内 |
| N3 | `render_spine_step.go` `renderSpineStep`/`renderSpineBriefStep` 的 `t.SpineInstructionLine(s.Instruction)`（第 263、365 行） | 任务中途的用户指令原文 | 同上 |
| N4 | `render_spine_step.go` `renderSpineBriefStep` 的 `t.SpineReportLine(...)`（第 367、369 行） | 非工具调用 Step 的 `RespText`/`Reasoning` 摘要 | 同上 |

N2/N3 比已登记的三处 `<summary>` 更严重：`<summary>` 里的一段失控 HTML 注释最多吞掉这一个折叠块
内部的内容（`</summary>` 或后续边界会限制影响范围），而 N2/N3 是**顶层正文里的裸文本**——一个未
闭合的 `<!--` 会一直吞到文档后面某处出现 `-->` 为止，可能是整个 Journey 报告剩余部分。

判定：这四处与 KNOWN_ISSUES 原文列出的 5 处同属一个缺陷（`internal/story` 把原文直接写进渲染输出，
没有复制 `internal/reqdetail` 的 `escapeHTML`），且都在本阶段要动的同一两个文件里，用同一个修法
（转义）以同一次改动关闭——分开处理没有任何工程收益，还会让"同一文件里 5 处修了、4 处没修"这种
不一致状态被审阅者当成疏漏重新提出。**本计划把 P12.2/P12.3 的范围从 KNOWN_ISSUES 原文的 5 处扩到
9 处**，扩大部分收口进 KNOWN_ISSUES 的更新记录（§5），不静默扩大后又不留痕迹。

**明确不做的扩大**：`internal/story/render_md.go`（Journey 标题 `j.Title`）、`storyindex.go`
（索引表格里的标题列）、`render_compare.go`（`SideBlock` 的 `title` 参数）——这几处同样把
`Title`/`taskTitle` 原文写进输出，性质与 N2 完全一致，但分布在三个**本阶段未计划修改**的文件里，
把它们并入意味着扩大本阶段的文件面、需要新的一轮字节级回归验证。登记为 KNOWN_ISSUES 新条目（低于
`1.37` 的严重度——运营者读的是自己 agent 会话的记录，不是被第三方注入的内容，最坏后果是报告里
一段内容被浏览器渲染时静默吃掉，不是安全漏洞），留给下一次触及这几个文件时顺手处理或独立立项。

---

## 1. 目标与范围（继承自 DevPlan §6.5，范围按 §0 扩大 4 处）

**目标**：让一份详单的内容与"请求它的那次运行"一致，而不是与"第一次生成它的那次运行"一致；
同时让人读产物（决策脊柱）不再静默吞掉内容。

**范围**：`internal/reqdetail` 的跳过谓词与页面指纹；`internal/story` 的
`render_spine_step.go`/`render_spine_args.go` 里全部 9 处原文直接进 Markdown/HTML 输出的位置；
两个包之间转义辅助函数的归属。**不涉及体积、不涉及产物集合**（那是 P13）。

**边界**：`internal/reqdetail.Render` 的输出会多一行机器可读指纹（P12.1），这是本阶段唯一允许的
"内容形状"变化；除此之外，任何不含 `< > &` 的既有测试夹具，其渲染结果必须逐字节不变——转义函数
对无特殊字符的输入是恒等函数，这是保证不产生无关 diff 的前提，需在验收里用现有测试套件全绿来确认。

---

## 2. 任务清单

### P12.1 跳过谓词从"假设"改为"校验"

**问题**：`reqdetail.EnsureRendered`（`ensure.go:41`）用 `os.Stat(target) == nil` 判断是否跳过渲染，
但 `Render` 的输出还依赖 `lang`/`linkEvidence`，两者都不进文件名——同一文件名可能对应两种不同内容。

**改法**：

1. `internal/reqdetail/render.go` 新增：
   - `const renderTemplateVersion = 1`——不属于 `lang`/`evidence` 的第三个失效维度：未来任何改变
     `Render` 输出形状的改动（例如 P13 的详单内容减法），把这个数字加一即可让所有已写盘的旧详单
     在下次 `EnsureRendered` 时被判定过期重写，不需要引入第四个参数。
   - `func renderFingerprint(lang i18n.Lang, linkEvidence bool) string`——返回一行 HTML 注释
     （`<!-- reqdetail:v1 lang=en evidence=false -->\n`），浏览器/Markdown 渲染器不显示，机器可读。
2. `internal/reqdetail/detail.go` 的 `Render`：把 `renderFingerprint(...)` 作为输出的第一行写入
   （`Builder` 的第一次 `WriteString`），其余渲染逻辑不变。
3. `internal/reqdetail/ensure.go`：
   - 新增 `readRenderFingerprint(target string) (string, error)`——`bufio.Reader.ReadString('\n')`
     只读第一行（有界读，不读整份文件；P13 之前详单可能有几 MB，每次 `EnsureRendered` 调用都要做
     这次读，必须是有界的）；文件不存在返回 `("", nil)`（让后续比较自然走向"重写"分支）。
   - `EnsureRendered` 把 `os.Stat` 判断换成 `readRenderFingerprint(target) == renderFingerprint(lang, linkEvidence)`
     判断；不匹配（含"文件不存在"和"存在但指纹不同"两种情况）时都走原有的渲染+`writeFileAtomic`
     路径——`writeFileAtomic` 本来就是"总是整份覆盖写"的实现，不需要改。
   - 改写 `EnsureRendered` 的 doc comment，去掉那句自我矛盾的"an existence check is a correct
     and sufficient skip condition"，换成"校验而非假设"的实际逻辑与理由。
4. `internal/reqdetail/render.go`/`detail.go`/`evidence.go` 内部对 `escapeHTML` 的调用同步改名
   （见 P12.4）。

**不做的两个备选**（DevPlan/KNOWN_ISSUES 已否决，原样继承，不重新论证）：把 `lang` 编进文件名；
每份详单配 sidecar 元数据文件。

### P12.2/P12.3 `internal/story` 的 9 处未转义注入点（含 §0 新发现的 4 处）

统一修法：所有原文直接进入 Markdown/HTML 输出（无论是在 `<summary>` 里、Markdown 正文行内、还是
`key=value` 拼接）的位置，转义前先经过已有的截断/拉平（`oneLineTruncate`/`strings.Fields` 拼接）
再转义——避免转义膨胀（`&` → `&amp;`）打乱既有的截断长度语义,也避免在实体引用中间截断。

| # | 文件:位置 | 修法 |
| --- | --- | --- |
| 1 | `render_spine_args.go` `payloadBlock` inline 分支 | `val` → `escapeHTML(val)` |
| 2 | `render_spine_args.go` `payloadBlock` 折叠分支 summary | `preview` → `escapeHTML(preview)` |
| 3 | `render_spine_args.go` `toolCallLine` `allShort` 分支（新发现，N1） | `f.val` → `escapeHTML(f.val)` |
| 4 | `render_spine_step.go` `foldWhyLine` inline 分支 | `flat` → `escapeHTML(flat)` |
| 5 | `render_spine_step.go` `foldWhyLine` 折叠分支 summary | `oneLineTruncate(text, capLen)` → `escapeHTML(oneLineTruncate(text, capLen))` |
| 6 | `render_spine_step.go` `toolResultLine` summary | `oneLineTruncate(r.Text, spinePreviewLen)` → `escapeHTML(...)` |
| 7 | `render_spine_step.go` `renderDecisionSpine` 的 `SpineTaskLine`（新发现，N2） | `task.Title` → `escapeHTML(task.Title)` |
| 8 | `render_spine_step.go` `renderSpineStep`/`renderSpineBriefStep` 的 `SpineInstructionLine`（新发现，N3，两处调用点） | `s.Instruction` → `escapeHTML(s.Instruction)` |
| 9 | `render_spine_step.go` `renderSpineBriefStep` 的 `SpineReportLine`（新发现，N4，两处调用点：`RespText`/`Reasoning`） | `oneLineTruncate(...)` → `escapeHTML(oneLineTruncate(...))` |

`codeFence` 包裹的完整内容（折叠块展开后的部分）不需要改——围栏代码块内 CommonMark 不解析 HTML，
这正是它已经安全的原因，也是判断"要不要转义"的唯一标准：**离开围栏就必须转义，在围栏内不需要**。

### P12.4 `escapeHTML` 的归属定案

**决定**：下沉，不复制。`internal/story` 已经在生产代码里 import `internal/reqdetail`
（`ensure_details.go`、`render_spine_step.go` 的 `FileNameForManifest`），架构上不新增依赖边界；
`archtest` 的 import 边界测试已经允许这条边（story 是叶子层之上的中间层，反向禁止的是
`reqdetail` import `story`，不是反过来）。

**改法**：

1. `internal/reqdetail/render.go`：`escapeHTML` 改名导出为 `EscapeHTML`，doc comment 更新为泛化
   描述（不只是"给 summary 用"）并说明为什么现在导出。更新包内 3 个调用点
   （`render.go` 的 `renderMessageSection`、`detail.go` 的两处、`evidence.go` 的 `toolsEvidenceBody`）。
2. `internal/story/render_md.go`：新增本地薄包装

   ```go
   // escapeHTML neutralizes user/model-derived text before it enters raw
   // Markdown/HTML output. Re-exported from internal/reqdetail rather than
   // reimplemented: P12's review found this exact helper was the one piece
   // NOT copied when this package's folded-block rendering pattern (fence +
   // summary + escape) was duplicated from reqdetail — codeFence (just
   // above) came along, escapeHTML didn't, and that gap is what let content
   // silently disappear on real corpus data (KNOWN_ISSUES §1.37). Sharing
   // the one three-line implementation removes this failure mode
   // structurally; codeFence stays duplicated on purpose (see its own doc
   // comment) because it can't drift the same way.
   func escapeHTML(s string) string {
       return reqdetail.EscapeHTML(s)
   }
   ```

   加 `"vmr/internal/reqdetail"` import。`render_spine_args.go`/`render_spine_step.go` 直接调用包内
   的 `escapeHTML(...)`（不需要各自 import `reqdetail`，沿用 `codeFence` 已建立的"包内裸调用"惯例）。

**未采纳的备选**：两个包各留一份并互相点名注释——`codeFence` 已经示范过这个模式对"稳定、无理由
再变"的辅助函数成立,但 `escapeHTML` 恰恰是这次真实暴露"两份拷贝会静默漂移"的那个函数,继续复制
等于把已经发生过一次的失效模式原样留下。

---

## 3. 测试

- `internal/reqdetail/ensure_test.go`：
  - `TestEnsureRendered_WritesOnce` 保留（同参数重复调用，指纹相同，mtime 不变——语义不变，仍是
    有效回归）。
  - `TestEnsureRendered_NeverOverwritesAPreexistingFile` 改名/改写为
    `TestEnsureRendered_RewritesAFileWithoutAMatchingFingerprint`——sentinel 内容模拟"P12 之前
    生成的旧详单"（无指纹行），断言第二次调用后文件内容变成 `Render` 会产出的内容（不再是原
    sentinel）——这是把 KNOWN_ISSUES §1.41 描述的根因直接钉成回归测试，而不只是修复代码。
  - 新增 `TestEnsureRendered_RewritesOnLanguageChange`：同一 `dir`/`rec` 先用 `i18n.EN` 调用一次
    （断言内容含 `"① Client → VMR Request"`），再用 `i18n.ZH` 调用（断言文件名不变、内容改为含
    `"① Client → VMR 请求"`）——直接对应 DevPlan 引用的真实语料证据（M8）。
  - 新增 `TestEnsureRendered_RewritesOnEvidenceModeChange`：构造一条带系统提示词的记录，先以
    `evidenceDir=""` 调用（断言内容内联系统提示词原文），再以非空 `evidenceDir` 调用（断言内容改为
    证据链接、且 `evidenceDir` 下出现对应文件）——对应 DevPlan 引用的真实语料证据（M9,尽管这里验证
    的是"补建"而不是"删除后重建"，两者由同一个指纹机制保证)。
- `internal/story/render_spine_args_test.go`：`TestToolCallLine` 补子测试，覆盖 inline/折叠/
  `allShort` 三种分支各自对 `<`/`>`/`&`/`<!--` 的转义。
- `internal/story/render_spine_test.go`：`TestRenderDecisionSpine` 补子测试，覆盖 Task 标题、
  mid-task 指令行、report 行（RespText/Reasoning）、why-line（`foldWhyLine` 两分支）、工具结果
  （`toolResultLine`）各自对同一类内容的转义——用 DevPlan 引用的真实反例形状
  （`"<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> real content"`）而不是任意特殊字符,让测试失败时
  的报错信息就是可读的"这是那个真实案例"。

---

## 4. 阶段验收（对齐 DevPlan §6.3 通用完成定义）

- [x] `go build ./...` 绿
- [x] `go test ./... -race` 绿
- [x] `go test ./internal/archtest/...` 绿（`render_spine_args.go` 改动后仍是 196 行，200 行硬预算内）
- [x] `gofmt -l .` 无输出
- [x] `go vet ./...` 绿
- [x] 两个端到端场景（同目录先 EN 后 ZH；带系统提示词的记录从内联切到证据链接）落成 §3 所述的常驻
      测试（`TestEnsureRendered_RewritesOnLanguageChange`/`TestEnsureRendered_RewritesOnEvidenceModeChange`）
- [x] 默认路径实测（DevPlan 新增完成定义第 7 条）：`~/.vmr/logs/` 下 2 个文件 18 条真实记录，
      改动前后各跑一次 `vmr report -details`（两轮，首版范围与外部审阅处置后的最终范围各一次），
      `diff -rq` 均只有 18 份详单各恰好 +1 行指纹不同，其余文件全部字节相同——这批语料不含
      `<`/`>`/`&`/`|`，未触发转义路径，转义那一半由 §3+§6.8 的 12 个直接单元测试用真实反例形状钉住
- [x] `KNOWN_ISSUES_sonnet-5.md` §1.37/§1.41 移入 §3（已闭环，第 36/37 项，按 §6.8 处置后的最终
      范围重写），§0/§4 的分布统计与 ROI 表同步更新；未新增 §1.44——外部审阅指出的三处遗漏当场
      修复，不留待办（见 §6.8）
- [x] `docs/future-strategy/story_report_full_review_opus-5.md` 第 6 章 P12 行标记为已完成
- [x] 本 ActionPlan 文档补执行记录与总结（见 §6，含 §6.8 外部审阅处置）

---

## 5. 收尾：`KNOWN_ISSUES` 与 DevPlan 更新计划

1. `§1.41`/`§1.37` 整条移入 `§3` 已闭环列表（第 36、37 项），格式对齐既有条目（现状→修法→真实
   语料/测试验证→"曾登记为 §1.xx"）。
2. `§1.37` 收尾条目里注明范围比原登记扩大了 4 处（N1–N4），并说明理由（同缺陷、同文件、同修法）。
3. 新增一条低严重度条目，登记 §0 里明确不做的三处（`render_md.go`/`storyindex.go`/
   `render_compare.go` 的 Title 未转义）——避免这次发现被静默丢弃。
4. `§0` 当前状态分布重算：高危 3→2（`1.35`/`1.36` 仍开放），中危 7→6（`1.38`/`1.42`/`1.43` 三条
   [中]/[中低] 仍开放，`1.37` 移出），低危新增 1 条（第 3 点的新登记），总数相应调整。
5. `§4` ROI 总表删除 `1.41`/`1.37` 两行，§4.1 覆盖范围说明与 §4.2 分档结论的枚举文字同步更新。

---

## 6. 执行记录

### 6.1 P12.1 — 跳过谓词从"假设"改为"校验"

按计划执行，无偏差。`internal/reqdetail/render.go` 新增 `renderTemplateVersion`（=1）与
`renderFingerprint(lang, linkEvidence)`；`detail.go` 的 `Render` 把它写作输出的第一行；
`ensure.go` 新增 `readRenderFingerprint`（`bufio.Reader.ReadString('\n')`，有界读，文件不存在时
返回 `("", nil)` 而非错误），`EnsureRendered` 的判据从 `os.Stat(target) == nil` 换成指纹比对，
doc comment 同步改写去掉了那句自我矛盾的"existence check is sufficient"。

### 6.2 P12.2/P12.3 — `internal/story` 的 9 处未转义注入点

按 §0 扩大后的范围执行：`render_spine_args.go` 3 处（`payloadBlock` 的 inline/折叠分支、
`toolCallLine` 的 `allShort` 分支）+ `render_spine_step.go` 6 处（`foldWhyLine` 的 inline/折叠
分支、`toolResultLine` 的折叠分支、`SpineTaskLine`/`SpineInstructionLine`（两处调用点）/
`SpineReportLine`（两处调用点））全部转义，`codeFence` 包裹的完整内容不动。截断/拉平先于转义
（`escapeHTML(oneLineTruncate(...))` 而非反过来），避免转义膨胀打乱既有截断长度语义。

`render_spine_args.go` 修改后仍是 196 行（原地替换表达式，未新增语句行），远低于其 200 行硬预算；
`render_spine_step.go` 395→约 400 行，远低于默认 700 行预算。

### 6.3 P12.4 — `escapeHTML` 归属定案：下沉，不复制

`internal/reqdetail/render.go` 的 `escapeHTML` 改名导出为 `EscapeHTML`，doc comment 从"给
summary 用"泛化为"给任何要进 Markdown/HTML 输出的原文用"；包内 3 个调用点
（`render.go`/`detail.go` 两处/`evidence.go`/`diff.go` 两处，比 §2 原计划多发现 2 处
`escapeHTML(` 的存量调用点，一并改名）同步更新。`internal/story/render_md.go` 新增三行本地薄
包装 `func escapeHTML(s string) string { return reqdetail.EscapeHTML(s) }`，`render_spine_args.go`/
`render_spine_step.go` 直接裸调用（不各自 import `reqdetail`，沿用 `codeFence` 已建立的包内调用
惯例）。`codeFence` 按计划保持两份独立实现，不下沉。

### 6.4 测试

新增/改写按 §3 计划执行：

- `internal/reqdetail/ensure_test.go`：`TestEnsureRendered_NeverOverwritesAPreexistingFile`
  改写为 `TestEnsureRendered_RewritesAFileWithoutAMatchingFingerprint`（断言内容变化 + 断言新
  写入的内容带有正确的指纹行）；新增 `TestEnsureRendered_RewritesOnLanguageChange`（EN→ZH，断言
  `"① Client → VMR Request"` → `"① Client → VMR 请求"`，文件名不变）；新增
  `TestEnsureRendered_RewritesOnEvidenceModeChange`（构造一条带系统提示词的记录，内联→证据链接，
  断言系统提示词原文从页面消失、`evidence/` 出现对应文件）。
- `internal/story/render_spine_args_test.go`：`TestToolCallLine` 补 3 个子测试（`allShort`/
  inline/折叠三种分支），用真实反例形状 `"<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> real content
  after"` 断言转义后不出现裸 `<!--`、且转义后的 `&lt;!--` 与尾随内容都在。
- `internal/story/render_spine_test.go`：`TestRenderDecisionSpine` 补 4 个子测试（Task 标题、
  mid-task 指令、非工具调用 Step 的 report 行、工具调用 Step 的 why-line）；新增
  `TestFoldWhyLine_Escapes`（inline/折叠两分支）与 `TestToolResultLine_EscapesSummary`（直接单元
  测试这两个此前完全没有专属测试的函数，而不是绕经 `renderDecisionSpine` 拼装完整场景）。

全部新测试首次运行即通过；既有测试套件（`go test ./... -race`）全绿，**无一处因转义改动而需要
调整既有断言**——这证明了 §1 边界声明成立：全部既有测试夹具都不含 `<`/`>`/`&`，转义函数对它们是
恒等函数。

### 6.5 默认路径实测

用 `~/.vmr/logs/` 下两份真实审计日志（2026-07-19/07-23，18 条真实记录）跑
`git stash`/`git stash pop` 前后对照：`vmr report -details` 改动前后各跑一次，`diff -rq` 输出
目录。结果：**18 份详单 + `vmr-report.json` 共 19 个文件不同，其余全部文件字节相同**。

逐份核对 18 份详单的 diff：**每份都恰好是新增一行指纹注释，没有其它字节差异**——这批真实记录都
不含 `<`/`>`/`&`，没有触发 P12.2/P12.3 的转义路径，这份语料样本没能覆盖到转义这一半的改动，但
干净地证明了 P12.1 的改动在真实数据上除了指纹行之外零副作用。转义路径的真实性证据来自 §6.4 用
DevPlan 引用的真实反例（`journey-j-pimini-…-754b71e2.md` 那条 `read` 结果的实际形状）构造的回归
测试，而不是这批语料。`vmr-report.json` 的唯一差异是 `generated_at` 运行时间戳（与 P11 收尾记录
的同款良性差异一致）。

### 6.6 阶段验收结果

| 检查项 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `gofmt -l .` | 无输出 |
| `go test ./... -race` | 全绿 |
| `go test ./internal/archtest/...` | 全绿（文件/函数行数预算、导入边界、文档引用守卫，含本次新写入 `KNOWN_ISSUES` 的大段文字未触发任何死引用） |
| 两个端到端场景常驻测试 | `TestEnsureRendered_RewritesOnLanguageChange`/`TestEnsureRendered_RewritesOnEvidenceModeChange`/`TestEnsureRendered_RebuildsDeletedEvidence`（真正的 M9 场景，外部审阅指出首版遗漏后补齐）均通过 |
| 默认路径实测（真实语料，改动前后对照，共两轮） | 第一轮（P12.1–P12.4 首版）：18 份详单各恰好 +1 行指纹，其余相同；第二轮（外部审阅处置后的最终版）：同样 18 份详单各恰好 +1 行指纹，其余相同——两轮结论一致，处置外部审阅意见没有引入这批真实语料能观察到的新副作用 |
| `KNOWN_ISSUES_sonnet-5.md` | `§1.37`/`§1.41` 移入 `§3`（第 36、37 项，均已按外部审阅处置后的最终范围重写）；`§0`/`§4` 的分布统计与 ROI 表同步更新（高危 3→2，中危 7→6，低危 15 不变，§1 总数 26→24）；未新增 `§1.44`——外部审阅指出的三处遗漏当场修复，不留待办 |
| `docs/future-strategy/story_report_full_review_opus-5.md` | 第 6 章 P12 行与 P12 详述小节标记为已完成 |

### 6.7 总结

P12 最终交付范围比 DevPlan 原计划**宽**（这与 P11"范围被证明偏窄"正好相反，两次都验证了同一条
方法论：**开工前必须基于真实仓库状态重新核实，不能只信任上一份文档点名的具体位置**）——原登记
的 5 处注入点核实无误，但通读全文额外发现 4 处同类缺陷，且其中 3 处（Task 标题、mid-task 指令、
report 行）是文档正文裸文本，比已登记的 `<summary>` 注入点更严重：一段失控的 HTML 注释可以吞掉
文档中从该处到下一个 `-->` 之间的任意内容，而 `<summary>` 内的失控注释天然被折叠块边界限住。
把这 4 处一并修复而不是另开一个阶段，理由是它们与已登记的 5 处同文件、同缺陷、同修法，分开处理
除了制造"同一文件里修一半"的不一致状态外没有任何收益。

首版实现原本在这里划了一条边界：`render_md.go`/`storyindex.go`/`render_compare.go` 里同样未转义
的 Journey/Task 标题因为"分布在本阶段未计划修改的文件里"而被排除，登记为 `§1.44` 留给下一次处理。
**这条边界站不住**——§6.8 记录的外部独立审阅当场指出两点：`render_md.go` 已经因为要放
`escapeHTML` 薄包装而被本次改动触及，"未计划修改"这个前提本身是错的；而 `storyindex.go` 的标题
列不是"少看一段内容"这个量级的问题，是原文一个 `|` 字符就能撕裂 `vmr-stories.md`（首要导航入口）
整行表格结构。核实后当场追加修复这三处，`§1.44` 未曾正式登记进 `KNOWN_ISSUES` 就被撤回。

`escapeHTML` 的归属定案（下沉到 `reqdetail.EscapeHTML`，`internal/story` 只留三行薄包装）直接
针对复查报告 §2.7 指出的根因——不是 `codeFence` 会漂移，是**没有被一起复制过去的那个函数**会
漂移；这次没有重复同一个错误，而是用共享实现把这类漂移在结构上排除掉,与 `codeFence` 保留独立
实现的决定并不矛盾（两者面对的是不同性质的风险：`codeFence` 自身不含判断分支，`escapeHTML` 若
被复制则每次都要靠人记得同步）。

真实语料默认路径实测覆盖了 P12.1 的改动（证明零意外副作用），但未能覆盖 P12.2/P12.3 的转义路径
（这批 18 条真实记录恰好不含需要转义的字符）——这不是验证空洞：转义行为由 12 个直接单元测试
（用真实反例的确切形状构造）钉住，比"恰好从这批语料里采到一条含 `<!--` 的记录"更精确、更可复现，
且不依赖语料本身是否凑巧包含触发条件。这与 DevPlan §2.11 记录的"验证方法论盲点"（体积问题只有
真的跑一遍量出来才会现身）是不同性质的风险：体积是涌现属性，必须用真实规模量；转义是逐字符的
确定性行为，一个精确构造的反例比一批恰好没触发的真实语料更可靠。

**对 P13 的影响**：P12.1 完成后，`details/` 目录从"一旦写下就永远不变"变成"跟随渲染逻辑演进"——
`renderTemplateVersion` 这个新增的第三个失效维度就是为 P13 准备的：P13 的两条内容减法（删除
Raw SSE 全文块、历史消息折叠为链接）会改变 `Render` 的输出而不改变文件名,只需要把这个常量加一，
所有已写盘的旧详单就会在下次 `EnsureRendered` 时被判定过期并重写,不需要再引入第四个参数。
P13 现在可以按 DevPlan 排期开工。

### 6.8 外部独立审阅（gemini-3.7-flash）的核查与处置

首版实现（§6.1–§6.7 记录的内容，即 P12.1–P12.4 按原计划范围完成后）写入
`story_report_p12_action_plan_sonnet-5.md` 后，`story_report_p12_action_plan_review_gemini-3.7-flash.md`
对其做了一次独立事实核查与架构审阅。逐条核实（读当时的真实代码状态，而不是只信报告自身的描述）
后，处置如下。

**F-01（第一梯队，高危）：`EnsureRendered` 的 evidence 补建被放在指纹比对之后，M9 场景实际上没有
真正修好——采纳，已修复**。审阅走查代码指出：当 `linkEvidence == true` 且详单指纹已匹配时，函数
在 `EnsureSysPromptEvidence`/`EnsureToolsEvidence` 之前的 `return` 语句直接退出，删除 `evidence/`
后重跑（evidence 模式全程不变）永远不会触发这两个调用——这正是 KNOWN_ISSUES §1.41 要修的那个场景，
只是从"文件整体命中跳过"换了个位置，在"指纹命中跳过"里原样复现。**复核方法**：先手工还原审阅
指出的错误顺序，确认新增的 `TestEnsureRendered_RebuildsDeletedEvidence` 先失败（`evidence/` 未被
重建）；再应用审阅给出的修法（两个 `Ensure*Evidence` 调用挪到指纹比对之前，无条件执行），确认同一
测试通过——不是只读代码就采信，是让声称的 bug 先在测试里真实复现过一次。见 KNOWN_ISSUES §3 第 36 项。

**F-02（第一梯队，高危）：转义范围划界有事实错误，且遗漏了比已修位置更严重的注入点——采纳，
已修复**。两点核实均属实：(a) 声称 `render_md.go` 是"本阶段未计划修改的文件"与事实不符——该文件
已经因为要放 `escapeHTML` 薄包装被本次改动触及；(b) `storyindex.go` 索引表格的标题列一个 `|`
字符就能撕裂 `vmr-stories.md`（首要导航入口）整行的表格结构，比已修的 `<summary>` 注入点（最多吞掉
一个折叠块）后果更重。追加修复 `render_md.go`（`j.Title`）、`storyindex.go`（标题列，用
`escapeCell(escapeHTML(...))` 双重处理——单独 `escapeCell` 挡不住 `<!--`，单独 `escapeHTML` 挡不住
`|` 撕裂表格）、`render_compare.go`（`SideBlock` 与复核该文件全文时一并发现的
`DivergenceHeavy`/`DivergenceLight`，审阅本身未点名后两处）。导出 `reqdetail.EscapeCell`（同
`EscapeHTML` 的导出理由）。见 KNOWN_ISSUES §3 第 37 项。

**F-03（第一梯队，高ROI）：`renderTemplateVersion` 机制价值认可，但缺少版本递增触发重写的测试，
且 P13 需要显式的跨阶段契约——部分采纳**。测试缺口按建议补上（`TestEnsureRendered_RewritesOnStaleTemplateVersion`，
手写一个旧版本号的指纹而不是真的先改动常量，验证机制本身可用）。跨阶段契约按建议记入
本文档 §6.7 的"对 P13 的影响"一节（已在首版写明"只需要把这个常量加一"），不额外写入 DevPlan
第 6 章 P13 小节——那属于"P13 开工前基于当时真实状态重新核实"的范围，不是本阶段该替 P13 预判的
细节（DevPlan §6.1 的既定原则）。

**F-04（第二梯队）：`EscapeHTML` 下沉方案的架构评估——审阅确认现有设计已符合 KISS，无需改动**。
不涉及代码改动。

**F-05（第二梯队）：转义测试的特殊字符覆盖面建议——部分采纳**。补了一个 `<script>`+`&` 组合的子
测试（`TestToolCallLine` 的"other HTML metacharacters"）。审阅担心的"转义在截断中间导致半个实体"
这一具体风险经代码走查确认不存在——全部 9+3 处调用点都是先截断/拉平（`oneLineTruncate`/
`strings.Fields` 拼接）、再对截断后的结果转义，没有反过来的调用点，所以不需要额外补一类"截断边界"
专项测试来验证一个代码里已经不可能发生的顺序。

**F-06（第三梯队，文档收尾）：内容与首版 §6 已完成的收尾工作一致**，随本次 KNOWN_ISSUES/DevPlan
的最终版本一并生效，不单独处理。

**处置后的验收**：`go build`/`go vet`/`gofmt`/`go test ./... -race`/`go test ./internal/archtest/...`
全部重新跑过，全绿；真实语料默认路径实测重新做过一次（§6.6 表格第二轮），18 份详单的 diff 结果
与首版一致。F-01 的修复本身也用"先复现失败、再验证修复"的方式核实过（见上），不是只凭代码走查
判定审阅意见成立。
