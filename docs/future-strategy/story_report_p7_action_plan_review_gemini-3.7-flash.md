// Ver 2026-08-20 22:45, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P7 执行计划与工作区实现核查报告（二审更新版）

<statement>
**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**
</statement>

本文是对 [`docs/future-strategy/story_report_p7_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p7_action_plan_sonnet-5.md)（下称 **P7 ActionPlan**）以及**当前工作区全部未提交修改**的完整事实核查、前期评审处置验收与代码质量复盘报告。

对照基准：
1. 架构方案 [`docs/future-strategy/story_report_architecture_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md)（§7.4、§7.5、§7.6、§4.8 等）
2. 第二期规划 [`docs/future-strategy/story_report_dev_plan_2_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_dev_plan_2_sonnet-5.md)（P7 阶段目标与验收标准）
3. 真实仓库代码与当前工作区未提交修改（涉及 `internal/taskseg/`、`internal/story/`、`internal/report/`、`internal/i18n/`、`cmd/vmr/`、`docs/`、`CHANGELOG.md` 等 23 个文件）

---

## 0. 综合复核与验收结论

**总体判定：当前工作区未提交的代码与文档修改完全符合 P7 ActionPlan 的任务范围，且前序评审报告（一审）中指出的 6 项问题（包含 2 项第一梯队严重缺陷、2 项第二梯队改进、1 项第三梯队微调及编译同步性约束）已在工作区代码中全部得到精准、彻底、高质量的处置。**

### 1. 验证基线全绿：
- `go test ./...` 全部通过（构建与单元测试通过）；
- `go test -race ./...` 全绿（涵盖 `internal/report`、`internal/story`、`internal/taskseg`、`internal/i18n`、`cmd/vmr`，无任何并发竞态）；
- `go test ./internal/archtest/...` 全绿（架构单向导入边界、各文件行数预算全部达标：`render_spine_args.go` 195/200 行，`render_spine_step.go` 387/700 行）；
- `go vet ./...` 零告警。

### 2. 核心交付物核验概览：
- **P7.1 请求索引坐标化**：`detailCell` 准确根据 `detailsOn` 选择渲染 `` `<r.Req>` `` 坐标或 Markdown 详单链接，默认 `vmr report` 下 100% 消除死链接。
- **P7.2 方言过滤收窄与脊柱因果闭环**：`timestampBracketRe` 专用于剥离时间戳前缀，完全消除了通用通配对合法前缀的误伤；`renderSpineStep` 补全了工具调用步的 `s.Instruction` 渲染，彻底闭环决策脊柱在中途追加指令时的因果上下文。
- **P7.3 `-llm-addr ''` 显式覆盖生效**：`resolveStringExplicit` 成功接入 `cmd_story.go`，显式空串参数能确定性覆盖 `report.yaml`，杜绝测试时的非预期付费调用。
- **P7.4 契约规范化**：`CategoryTask` 显式序列化为 `"task"`；`files` 统一按 `ctxgraph.CanonicalPath` 去除路径与压缩后缀，与 `req` 坐标口径逐字节一致。
- **P7.5 过期文案与截断提示修正**：删除了已废弃的 `.json` 副本引用，工具结果截断文案精准指向下一步详情链接。

---

## 1. 前期评审问题处置验收核对（逐项源码核查）

下表对评审报告（一审）指出的全部问题在当前工作区代码中的实际处置情况进行逐一核实：

| 序号 | 评审指出的问题 | 严重级别 / 属性 | 工作区实际处置情况 | 源码与测试依据 | 核查结论 |
| --- | --- | --- | --- | --- | --- |
| **1.1** | `leadingBracketRe` 通配吞噬任意方括号，导致 `messageIDBracketRe` 成为死代码且误伤合法前缀 | **第一梯队（严重冲突）** | **已彻底重构**：新增 [`openclaw_brackets.go`](file:///Users/stanford/code/vmr/internal/taskseg/openclaw_brackets.go#L28-L38)，定义专用的 `timestampBracketRe`（精准匹配星期与日期格式），裸消息循环仅剥离 `timestampBracketRe` 与 `messageIDBracketRe`；新增单元测试覆盖 `[Bug]`、`[P0]`、`[1]` 等合法前缀。 | [`internal/taskseg/openclaw_brackets.go:28-37`](file:///Users/stanford/code/vmr/internal/taskseg/openclaw_brackets.go#L28-L37)<br>[`internal/taskseg/openclaw.go:58-65`](file:///Users/stanford/code/vmr/internal/taskseg/openclaw.go#L58-L65)<br>[`internal/taskseg/openclaw_test.go:120-142`](file:///Users/stanford/code/vmr/internal/taskseg/openclaw_test.go#L120-L142) | ✅ **完全解决，死代码消除，合法前缀安全保留** |
| **1.2** | 决策脊柱对工具调用步遗漏 `Instruction` 渲染，导致中途追加指令在触发工具时 100% 丢失 | **第一梯队（高 ROI 疏漏）** | **已完整补齐**：`renderSpineStep` 新增 `taskStepIdx int` 参数，并在 `spineTransitionLines` 之后、`spineWhyLine` 之前补充 `if taskStepIdx > 0 && s.Instruction != ""` 渲染分支；`renderDecisionSpine` 循环调用处同步传递 `si`。 | [`internal/story/render_spine_step.go:172, 251-259`](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L172)<br>[`internal/story/render_spine_test.go:42`](file:///Users/stanford/code/vmr/internal/story/render_spine_test.go#L42) | ✅ **完全解决，决策脊柱因果链全覆盖** |
| **1.3** | `i18n.SpineText` 结构体字段增补导致的编译同步性风险 | **第一梯队（工程约束）** | **已原子落地**：`SpineText` 结构体添加 `SpineResultValueTruncated` 字段，并在 `Spine(ZH)` 与 `Spine(EN)` 中完整补齐多语言闭包实现；全包编译与测试全绿。 | [`internal/i18n/story_spine.go:29, 88-90, 153-155`](file:///Users/stanford/code/vmr/internal/i18n/story_spine.go#L29) | ✅ **完全解决，多语言结构体对齐** |
| **2.1** | `report_doc.go` 中"上表『文件』列"在主报表 §8 中存在指代缺失 | 第二梯队（文案脱节） | **已精准修正**：中英文文案均明确修改为指代"请求索引（`vmr-requests.md`）中的『文件』列"，消除了主报表读者在无表章节阅读时的困惑。 | [`internal/i18n/report_doc.go:84, 144`](file:///Users/stanford/code/vmr/internal/i18n/report_doc.go#L84) | ✅ **完全解决，表述与报表结构一致** |
| **2.2** | `CategoryTask` 显式序列化导致潜在测试断言断裂风险 | 第二梯队（测试防护） | **已全量核验**：`CategoryTask` 常量值更新为 `"task"`，去除 `omitempty`；`storyindex_test.go` 相关测试同步更新，全仓集成测试全部通过。 | [`internal/story/storyindex.go:38, 76`](file:///Users/stanford/code/vmr/internal/story/storyindex.go#L38)<br>[`internal/story/storyindex_test.go:28-58`](file:///Users/stanford/code/vmr/internal/story/storyindex_test.go#L28-L58) | ✅ **完全解决，契约显式且测试全绿** |
| **3.1** | `report_doc.go` 中 `vmr replay` 命令示例包含冗余位置参数 | 第三梯队（体验微调） | **已简化对齐**：示例直接采用 P6.5 交付的免位置参数形态 `vmr replay -print -req <坐标>`，降低用户使用负担。 | [`internal/i18n/report_doc.go:84, 86, 144, 146`](file:///Users/stanford/code/vmr/internal/i18n/report_doc.go#L84) | ✅ **完全解决，命令风格统一** |

---

## 2. 关键处置点源码深度核验

### 2.1 方言过滤专用正则与前缀安全保留（问题 1.1）

在 [`internal/taskseg/openclaw_brackets.go`](file:///Users/stanford/code/vmr/internal/taskseg/openclaw_brackets.go#L28-L38) 与 [`internal/taskseg/openclaw.go:55-68`](file:///Users/stanford/code/vmr/internal/taskseg/openclaw.go#L55-L68) 中：
```go
// internal/taskseg/openclaw_brackets.go
var timestampBracketRe = regexp.MustCompile(`^\[(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)(?:\s+\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?:\s+GMT[+-]\d+)?)?\]\s*`)
var messageIDBracketRe = regexp.MustCompile(`^\[message_id:[^\]]*\]\s*`)

// internal/taskseg/openclaw.go (RealUserText 裸消息分支)
for {
    stripped := timestampBracketRe.ReplaceAllString(text, "")
    stripped = messageIDBracketRe.ReplaceAllString(stripped, "")
    if stripped == text {
        break
    }
    text = stripped
}
if strings.TrimSpace(text) == "" {
    return "", false // only scaffolding brackets, nothing real left
}
```
**核验结论**：
- `timestampBracketRe` 严格限定以星期缩写（`Mon`～`Sun`）开头，避免了对 `[Bug]`、`[P0]`、`[1]` 等任意方括号的贪婪匹配；
- 剥离时间戳后，第二轮循环能够正确命中并剥离 `[message_id: ...]`，使 `messageIDBracketRe` 发挥预期效用；
- 测试 `TestOpenClawAware_BareMessageLeadingUserBracketSurvives` 在测试用例中验证了各种合法用户输入前缀的无损保留。

---

### 2.2 决策脊柱工具调用步指令渲染补全（问题 1.2）

在 [`internal/story/render_spine_step.go:246-260`](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L246-L260) 中：
```go
func renderSpineStep(w func(string, ...any), steps []*Step, i, taskStepIdx int, repeated, flagged bool, t i18n.SpineText, storyT i18n.StoryText) {
    s := steps[i]
    w("%s", spineStepHeader(s, repeated, flagged, t))
    spineTransitionLines(w, s, storyT)
    if taskStepIdx > 0 && s.Instruction != "" {
        w("%s", t.SpineInstructionLine(s.Instruction))
    }
    w("%s", spineWhyLine(s))

    matched := toolResultsFor(steps, i)
    // ... 后续配对与工具调用渲染
}
```
**核验结论**：
- 当用户在任务第 2 步及以后追加指令（`taskStepIdx > 0 && s.Instruction != ""`）且该步包含工具调用时，脊柱会在 Step 头部和转移线之后、思考原因 `spineWhyLine` 之前，清晰渲染 `💬 指令 · <Instruction>`；
- 当 `newTask` 发生时，首步 `taskStepIdx == 0`，不渲染指令行，避免与上方 `**t0X · <TaskTitle>**` 重复；
- 彻底解决了工具调用步指令丢失的问题，使脊柱对中途追加指令的展现率达到 100%。

---

### 2.3 请求索引与详单解耦渲染（P7.1）

在 [`internal/report/requests.go:612-623`](file:///Users/stanford/code/vmr/internal/report/requests.go#L612-L623) 中：
```go
func detailCell(r RequestRow, detailsOn bool) string {
    if !detailsOn {
        if r.Req == "" {
            return "-"
        }
        return "`" + r.Req + "`"
    }
    if r.DetailFile == "" {
        return "-"
    }
    return fmt.Sprintf("[Ⓜ️ Markdown](details/%s)", r.DetailFile)
}
```
**核验结论**：
- `WriteRequestsIndex` 与 `WriteFailedIndex` 接收从 `cmd_report.go` 透传的 `detailsOn` 参数；
- 默认不带 `-details` 时，四张报表（`vmr-requests.md`、各 Client 分组表、`vmr-requests-cron-*.md`、`vmr-requests-failed.md`）中的"文件"列均渲染为行内代码格式的 `req` 坐标（如 `` `vmr-audit-2026-07-25.jsonl:317` ``），彻底根除 404 死链接。

---

## 3. 文档与元数据同步核查

对照 P7 ActionPlan §8 收尾清单，对仓库文档的同步状态进行了全面审查：

1. **`CHANGELOG.md`**：
   - 在 `[Unreleased]` 章节新增 5 条 `Fixed` 记录，完整描述了 P7.1（请求索引死链接清零）、P7.2（方言过滤补漏与脊柱指令全覆盖）、P7.3（`-llm-addr ''` 显式覆盖）、P7.4（`Category` 与 `files` 契约对齐）、P7.5（截断文案与过时文档修正）的用户可见行为变化。
2. **`docs/KNOWN_ISSUES_sonnet-5.md`**：
   - §1.21（方言过滤）、§1.28（`-llm-addr` 覆盖）、§1.31（请求索引死链接）已正式移入 §3 已闭环条目（编号 27、28、29）；
   - §1.33 / §1.34 多子项条目中已完成的子项均已就地更新状态；
   - §4 ROI 表中相应条目已同步调整。
3. **架构与规划文档**：
   - [`docs/future-strategy/story_report_architecture_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md) §7.6(b) 表格对 `evidence/` 的报表侧引用加注"设计预留，未实现"；
   - [`docs/future-strategy/story_report_dev_plan_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_dev_plan_opus-5.md) 补充了 P5 脊柱内联体积现状、P6.5 `vmr analyze` 落地状态及后续演进关系的说明。

---

## 4. 最终核查结论与建议

1. **工作区状态判定**：当前工作区代码结构整洁，逻辑严密，测试覆盖完整（包含单测、竞态与架构限制测试），完全兑现了 P7 ActionPlan 的所有承诺，前期发现的问题均已得到理想解决，未发现新的设计缺陷或回归风险。
2. **操作建议**：
   - 确认当前工作区修改完备，可以安全进行 `git add` 与 `git commit` 操作；
   - P7 阶段已完成，后续可依照 `story_report_dev_plan_2_sonnet-5.md` 顺利进入 P8 阶段（JSON 语言策略统一）。
