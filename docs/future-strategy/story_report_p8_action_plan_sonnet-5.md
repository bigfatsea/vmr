// Ver 2026-08-20, by Sonnet 5

# vmr 日志分析体系重构 — P8 ActionPlan（JSON 语言策略统一）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_2_sonnet-5.md`（下称"第二期 DevPlan"）**P8
阶段**的执行级细化，基于本仓库 P8 起点（P0–P7 全部已完成并落地，commit `2884d51`；工作区干净）
的真实代码状态编写。方向依据是 `docs/future-strategy/json_lang_policy_plan_sonnet-5.md`（下称
"策略文档"，已通读全文）——本文只把该文档 §2/§3/§5 的结论收敛进可执行步骤，不重新论证"要不要
统一"这件事。

**DevPlan review 结论**（本次 review 的产出）：P8 的任务范围/边界/验收标准逐条对照当前代码核实后
**全部成立，未发现需要调整第二期 DevPlan 正文的落差**——P0–P7 的改动没有触及 `internal/report`
的语言相关代码路径，也没有触及 `internal/story/compare.go`/`internal/i18n/story_compare.go`，
P8 起点与 DevPlan 写就时描述的状态完全一致。DevPlan 正文未作修改。

**本文对以下关键事实做过独立核实**（不是转述策略文档的转述）：
- `internal/story/compare.go:207` 的 `Compare(a, b JourneySummary) Comparison` 确认不接收
  `lang`；`internal/report/metrics.go:395` 的 `buildFindingsForJSON(rep)` 确认硬编码
  `i18n.EN`，唯一调用方是 `aggregate.go:520`（`Build`/`BuildCached` 共享的 `buildInternal`）。
- **P8.2 的路径裁决已经做完，结论是 (b)**：全仓 grep `rep.Efficiency` 的读取点，除了
  `aggregate.go:520` 的写入与三处测试按 `.Code` 匹配外，唯一的其他读取是
  `cmd/vmr/i18n_e2e_test.go:132`（也是按结构体字段读取，不依赖英文文本本身）——`Build()`
  内部没有任何逻辑依赖 `rep.Efficiency` 的英文文本内容，(b) 确认是低风险、小改动面的路径。
- **`internal/story/compare.go` 现为 846/850 行**（`internal/archtest/file_sizes_test.go:66`
  登记的豁免预算），只剩 4 行余量——P8.1 的改动（新增 `lang` 参数、改写 `Compare` 的 doc
  comment、新增 `"vmr/internal/i18n"` import）必然超线，DevPlan 未预判到这一点（DevPlan 本身
  是指导级文档，不含这类实现期才会暴露的行数预算细节），本文 §2 给出具体拆分方案。
- `internal/i18n/story_compare.go` 的 `MetricLabels` 查表（EN/ZH 各一份，230/141 行起）已经
  覆盖全部 14 个 `metricSpecs` 条目（`compare.go:116-131`），逐一比对键名/条目数一致，不需要补充
  任何新的本地化文本。
- `internal/story/render_compare.go:19-21`（`RenderComparisonMarkdown` 的 doc comment）与
  `internal/story/render_compare.go:41`（渲染循环内 `i18n.MetricLabel(lang, string(r.Metric))`
  的调用）确认：一旦 `Compare()` 自己算出本地化 `Label`，这处渲染层的重复查表调用即成为死代码，
  P8.1 顺带清掉。
- `internal/story/metricSpec.Label` 字段（`compare.go:108`）确认**全仓唯一读取点**就是
  `Compare()` 自己那一行（`compare.go:218`）——`render_corpus.go`/`corpus.go` 都只读
  `.Code`/`.Kind`/`.Value`，从未读过 `.Label`。P8.1 把 `Compare()` 改用
  `i18n.MetricLabel(lang, string(spec.Code))` 之后，`.Label` 字段全仓无人读取，一并删除
  （不是"顺手清理"，是避免留下一个刚创建就死掉的字段）。

---

## 1. 执行前置检查

```bash
cd /Volumes/SSD2T/code/vmr
git status --short                     # 确认工作区干净
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
ls logs/*.jsonl.zst 2>/dev/null | head  # 确认本机有真实样本日志可用于人工核对
```

四个子任务的依赖顺序：**P8.1 → P8.2 互不依赖，可并行**；**P8.3（文档）依赖 P8.1+P8.2 都落地**
（否则文档描述的是尚不存在的状态）；**P8.4（测试）依赖 P8.1+P8.2**（新增的一致性测试要三种
JSON 同时验证）。本文按 P8.1 → P8.2 → P8.3 → P8.4 的顺序执行，与 DevPlan 编号顺序一致。每做完
一个子任务跑一次 `go test ./internal/report/... ./internal/story/... ./internal/i18n/...
./cmd/vmr/... ./internal/archtest/...`，不要攒到最后一起查错。

---

## 2. 任务一：`story.Compare()` 接入语言（P8.1）

### 2.1 现状（已读代码确认，见 §0）

`internal/story/compare.go:207`：

```go
func Compare(a, b JourneySummary) Comparison {
	ma, mb := a.Metrics, b.Metrics
	// Labels here are fixed English literals, not routed through i18n —
	// Compare is deliberately language-agnostic ...
	rows := make([]MetricDiff, len(metricSpecs))
	for i, spec := range metricSpecs {
		rows[i] = metricDiff(spec.Code, spec.Label, spec.Kind, spec.Value(ma), spec.Value(mb))
	}
	return Comparison{A: journeyRef(a), B: journeyRef(b), Rows: rows, Tools: toolShareDiff(ma.ToolCallDist, mb.ToolCallDist)}
}
```

`spec.Label`（`metricSpec.Label`，`compare.go:108`）是硬编码英文字面量（`metricSpecs` 数组，
`compare.go:116-131`，14 条）。唯一调用方 `cmd/vmr/cmd_story.go:463`：
`cmp := story.Compare(sA, sB)`，紧接在 `sA, sB := story.Summarize(jA, lang), story.Summarize(jB,
lang)` 之后——`lang` 已经在作用域内，不需要新计算。

`internal/story/compare.go` 当前 846/850 行（登记豁免见 `internal/archtest/file_sizes_test.go:66`）。

### 2.2 目标设计

**(a) 文件拆分（先做，为后续改动腾出预算）**：把 `compare.go` 里"纯 metric-diff 计算机制"这一块
（`MetricKind`/`notableFloor`/`MetricDiff`/`MetricCode`/`metricSpec`/`metricSpecs`/
`metricDiff()`/`abs()`，即 `compare.go` 第 21–152 行，132 行，已核实这段代码不使用
`chatmsg`/`core`/`taskseg`/`sort`/`strings`/`time`/`json` 中任何一个，可以原样搬家不带 import
包袱）搬到新文件 `internal/story/compare_metrics.go`。**不搬** `ToolShareDiff`/
`toolShareDiff()`/`JourneyRef`/`journeyRef()`/`Comparison`/`Compare()`（这些是"比较结果的形状与
编排"，留在 `compare.go` 作为包的主入口）。拆分后 `compare.go` ≈ 846 − 132 = 714 行，仍需要一条
豁免登记（预算默认 700），但从 850 降到一个更贴近实际的数字（§5 收尾时按拆分+改动后的实测值
+15~20% 重新登记，不是继续沿用 850）。`compare_metrics.go` 新文件 132 行左右，远低于全局默认
700 行预算，不需要登记。

**(b) `Compare()` 签名与实现**：

```go
func Compare(a, b JourneySummary, lang i18n.Lang) Comparison {
	ma, mb := a.Metrics, b.Metrics
	rows := make([]MetricDiff, len(metricSpecs))
	for i, spec := range metricSpecs {
		rows[i] = metricDiff(spec.Code, i18n.MetricLabel(lang, string(spec.Code)), spec.Kind, spec.Value(ma), spec.Value(mb))
	}
	return Comparison{A: journeyRef(a), B: journeyRef(b), Rows: rows, Tools: toolShareDiff(ma.ToolCallDist, mb.ToolCallDist)}
}
```

`compare.go` 顶部 import 块加 `"vmr/internal/i18n"`（`internal/story` 包内其它文件早已依赖
`i18n`，不新增依赖边）。doc comment 里"Labels here are fixed English literals..."那一段整段改写，
说明现在直接用 `i18n.MetricLabel` 按 `lang` 取本地化标签，删除对旧"JSON 契约"设计文档小节的引用
（那一节 P8.3 会重写，不留一处指向旧结论的悬空引用）。

**(c) `metricSpec.Label` 字段删除**：`compare_metrics.go`（拆分后的新家）里 `metricSpec` 结构体
去掉 `Label string` 字段；`metricSpecs` 字面量的 14 行各自去掉对应的字符串列（`{Code, Kind,
Value}` 三元组，不再是四元组）。`metricDiff()` 函数签名不变（仍接收一个 `label string` 参数——
它是纯计算函数，不需要知道 label 是怎么来的，调用方现在传 `i18n.MetricLabel(...)` 而不是
`spec.Label`，函数体零改动）。

**(d) `render_compare.go` 的死代码清理**：`RenderComparisonMarkdown`（`render_compare.go:22`）
的渲染循环（第 41 行）从

```go
w("| %s%s | %s | %s | %s |\n", i18n.MetricLabel(lang, string(r.Metric)), mark, ...)
```

改为直接用 `r.Label`：

```go
w("| %s%s | %s | %s | %s |\n", r.Label, mark, ...)
```

因为 `cmp.Rows[i].Label` 现在已经是 `Compare(a, b, lang)` 用同一个 `lang` 算好的本地化文本——
`RenderComparisonMarkdown(cmp, lang)` 的调用方永远传同一个 `lang` 给 `Compare` 和
`RenderComparisonMarkdown`（`cmd_story.go` 里两次调用共享同一个局部变量），二次查表是纯粹的重复
计算。同步改写第 19–21 行的 doc comment（"cmp.Rows[].Label is always English..."这句现在是错的，
改为说明 `Label` 已经是 `Compare` 按调用时的 `lang` 算好的本地化文本，本函数直接读它）。

**(e) `internal/i18n/story_compare.go` 包注释里的过期计数**：包注释第 3 行"the 12 behavior-profile
metric labels"，实际是 14 个（`metricSpecs`/`MetricLabels` 均为 14 条，已核实），顺手改成 14——
不是本次改动引入的偏差，是发现即改，避免误导下一个数条目的读者。

### 2.3 具体步骤

1. 新建 `internal/story/compare_metrics.go`：`package story` + 一行版本头注释 + 从
   `compare.go` 剪切第 21–152 行（`MetricKind` 到 `abs()`）粘贴进去；`metricSpec` 去掉 `Label`
   字段；`metricSpecs` 字面量每行去掉对应的英文字符串列。
2. `internal/story/compare.go`：
   - import 块加 `"vmr/internal/i18n"`。
   - `Compare(a, b JourneySummary) Comparison` 改签名为
     `Compare(a, b JourneySummary, lang i18n.Lang) Comparison`；循环体改用
     `i18n.MetricLabel(lang, string(spec.Code))`；doc comment 按 §2.2(b) 改写。
3. `internal/story/render_compare.go`：第 41 行改用 `r.Label`；第 19–21 行 doc comment 改写；
   确认改完之后这个文件是否还需要在 import 块保留 `"vmr/internal/i18n"`——`i18n.Compare(lang)`
   （构造 `t := i18n.Compare(lang)`，第 24 行）与其它 `t.XxxTitle` 字段仍然需要它，不要误删。
4. `internal/i18n/story_compare.go`：包注释"12"改"14"。
5. `cmd/vmr/cmd_story.go:463`：`cmp := story.Compare(sA, sB)` 改为
   `cmp := story.Compare(sA, sB, lang)`。
6. 测试调用点补 `lang` 实参（全部已核实不断言具体 Label 文本值，只在 `%q` 格式化诊断信息里用到
   `r.Label`，统一传 `i18n.EN`，两个文件都已导入 `i18n`，不需要新增 import）：
   - `internal/story/compare_test.go`：第 251/315/336/360/373/389/426/460 行，共 8 处
     `Compare(...)` 调用（第 389 行是 `RenderComparisonMarkdown(Compare(a, b), i18n.EN)` 这种
     嵌套写法，改成 `RenderComparisonMarkdown(Compare(a, b, i18n.EN), i18n.EN)`）。
   - `internal/story/llm_test.go`：第 51/75 行，共 2 处。
7. `go vet ./internal/story/...` 确认没有遗漏的调用点（比这里列出的更可靠）。

### 2.4 验收标准（对照 DevPlan P8.1）

- `internal/story/compare.go`、`internal/story/compare_metrics.go` 均通过
  `internal/archtest` 的行数预算检查（前者若仍超 700 默认值，登记一条新的豁免，数字按实测+
  15~20% 而非沿用旧的 850）。
- 同一次 `-lang zh` 下，`vmr story -compare` 产出的 `compare-*.json` 的 `rows[].label` 与
  `compare-*.md` 表格里对应行的中文标签逐字一致（用 `jq`/肉眼核对至少 3 行）。
- `go test ./internal/story/... ./internal/i18n/... ./cmd/vmr/...` 全绿（`cmd/vmr` 这一步此时
  预期 `TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish` 会失败——这是预期中的，留给 P8.4 处理，
  不要在这一步误以为改错了）。

---

## 3. 任务二：`report` 侧语言接入路径裁决（P8.2）

### 3.1 现状（已读代码确认，见 §0——路径裁决已经做完）

`internal/report/aggregate.go:520`：`rep.Efficiency = buildFindingsForJSON(rep)`，是
`buildInternal`（`Build`/`BuildCached` 共享的内部实现）里唯一给 `rep.Efficiency`赋值的地方。

`internal/report/metrics.go:395`：

```go
func buildFindingsForJSON(rep *Report2) []Finding {
	return buildFindings(rep, i18n.EN)
}
```

`internal/report/section_efficiency.go:25`：Markdown 渲染路径已经在用
`findings := buildFindings(rep, lang)`——一次独立的、按调用方真实 `lang` 计算的本地化副本，渲染完
即丢弃，从不回写 `rep.Efficiency`。

`cmd/vmr/cmd_report.go:344-351`：

```go
rep.Meta.DetailsEnabled = detailsOn
jsonPath := filepath.Join(outDir, "vmr-report.json")
mdPath := filepath.Join(outDir, "vmr-report.md")
if err := report.WriteJSON(rep, jsonPath); err != nil {
	return err
}
storiesLink, lineageToJourney := loadStoriesLink(outDir)
if err := os.WriteFile(mdPath, []byte(report.Markdown(rep, lang, storiesLink)), 0o600); err != nil {
	return err
}
```

`lang` 在此处已经在作用域内（`resolveLanguage` 更早解析出来，`report.Markdown(rep, lang, ...)`
紧随 `WriteJSON` 之后就用到了它）。

### 3.2 路径裁决（DevPlan P8.2 要求"动手前先确认"，本文的确认结论）

**选 (b)：`Build`/`BuildCached` 保持语言无关，在 `cmd_report.go` 的 `WriteJSON` 调用之前，用
已经拿到手的 `lang` 现场重算 `rep.Efficiency`。不选 (a)。**

理由（DevPlan 要求"选定路径有一句话记录，为什么选它、不选另一条"）：全仓 grep 确认
`rep.Efficiency` 除了 `aggregate.go:520` 的写入之外，唯一的读取点是三处测试
（`TestBuildFindingsIsDeterministic` 系列，按 `.Code` 比较，`i18n_e2e_test.go:132` 按结构体字段
读取）——**没有任何内部逻辑依赖它的英文文本本身**，(b) 的前提条件成立。给 `Build`/`BuildCached`
（签名已经分别有 6/9 个参数）再加一个 `lang` 参数，会牵动这两个已经很长的函数、以及全仓所有直接
调用它们的测试（`internal/report` 包内至少数十处），而 (b) 只需要在 `cmd_report.go` 一处调用点、
`internal/report` 新增一个纯函数，改动面小两个数量级，且不改变 `Build`/`BuildCached` 的
可缓存性（P3.6 的 `.parse-cache/` 与 `Build`/`BuildCached` 的确定性无关，`rep.Efficiency` 的
语言从来不是缓存键的一部分）。

**一个刻意的设计选择，需要在这里写清楚原因**：`section_efficiency.go` 的 `renderEfficiency`
**不改**，继续保留它自己对 `buildFindings(rep, lang)` 的独立调用，**不去读**
`cmd_report.go` 新写入的 `rep.Efficiency`（即使这次它们会算出相同的值，因为传的是同一个
`lang`）。这与 P8.1 对 `render_compare.go` 的处理**刻意不对称**——`story.Compare(a, b, lang)`
把 `lang` 直接做成函数参数，同一次调用产出的 `Label` 保证和调用时的 `lang` 一致，`render_compare
.go` 复用 `cmp.Rows[].Label` 没有额外的顺序依赖风险；而 `report` 这边选择的是"`Build()` 之后由
`cmd_report.go` 在特定顺序下覆写 `rep.Efficiency`"，如果 `renderEfficiency` 也去读被覆写后的
`rep.Efficiency`，就会引入一条隐藏的调用顺序契约（`Markdown()` 必须在 `LocalizeEfficiency()`
之后调用，否则读到的是 `Build()` 内部算的英文默认值），任何未来新增的调用点（测试、`vmr
analyze` 的变体、直接调用 `internal/report` 包的脚本）一旦不遵守这个顺序就会静默产出语言不一致
的 Markdown，且没有编译期或运行期报错。保留 `renderEfficiency` 自己独立计算，让 `Markdown()`
在任何调用顺序下都自给自足、行为不依赖外部是否先调用过某个函数——多付出的成本只是一次内存内的
纯函数重复调用（`buildFindings` 本身零 I/O），可忽略不计。

### 3.3 目标设计

`internal/report/metrics.go`，在 `buildFindingsForJSON` 附近新增一个导出函数：

```go
// LocalizeEfficiency recomputes rep.Efficiency in lang, overwriting the
// English default Build/BuildCached always populates internally
// (buildFindingsForJSON) — call this once, after Build/BuildCached
// returns, before WriteJSON, so vmr-report.json's efficiency[] narrative
// fields match the language the accompanying Markdown will render in.
// Build/BuildCached deliberately stay language-agnostic (no lang
// parameter) — see json_lang_policy_plan_sonnet-5.md §3.1 for why this
// path was chosen over adding lang to their signatures. Cheap and pure:
// same already-aggregated rep, no I/O, and buildFindings' "pick the worst
// one" selection logic doesn't depend on lang (TestBuildFindingsIsDeterministic
// already pins that), so this can never select a different set of Codes
// than the English default did — only their rendered text changes.
func LocalizeEfficiency(rep *Report2, lang i18n.Lang) {
	rep.Efficiency = buildFindings(rep, lang)
}
```

`buildFindingsForJSON` 本身**不删除、不改造**——它仍然是 `Build()`/`BuildCached()` 内部计算
"默认英文"这个确定性基线的机制，`LocalizeEfficiency` 是它的调用方之外，另一个独立的、显式的
覆写步骤，二者不冲突（DevPlan P8.2 给出的"删除或改造"是二选一的候选描述，本文这里选定的是第三条
——都不动，新增一个下游步骤）。`buildFindingsForJSON` 的 doc comment 里"so Report2.Efficiency
(and therefore vmr-report.json) never varies with the report's display language"这句话在 P8
落地后不再成立，需要改写为准确描述：这是 `Build()` 计算的确定性默认值，`cmd_report.go` 在写
JSON 之前会用 `LocalizeEfficiency` 按实际语言覆写它。

`cmd/vmr/cmd_report.go`，在 `WriteJSON` 调用之前插入一行：

```go
rep.Meta.DetailsEnabled = detailsOn
report.LocalizeEfficiency(rep, lang)
jsonPath := filepath.Join(outDir, "vmr-report.json")
...
```

### 3.4 具体步骤

1. `internal/report/metrics.go`：紧邻 `buildFindingsForJSON`（第 395 行）之后新增
   `LocalizeEfficiency`（§3.3 给出的实现）；`buildFindingsForJSON` 自己的 doc comment 按 §3.3
   末段改写。
2. `cmd/vmr/cmd_report.go`：在 `rep.Meta.DetailsEnabled = detailsOn` 之后、
   `jsonPath := filepath.Join(...)` 之前插入 `report.LocalizeEfficiency(rep, lang)`。
3. `wc -l internal/report/metrics.go`——**更正**：该文件未在 `internal/archtest/file_sizes_test.go`
   的 `fileLineExemptions` 登记豁免（`470` 是 `internal/story/metrics.go` 的登记值，容易混淆，
   注意区分），适用全局默认 700 行预算。当前 443 行，新增函数约 20 行，预计到 463/700，余量充足，
   跑一次 `go test ./internal/archtest/...` 确认即可，不存在跨线风险。

### 3.5 验收标准（对照 DevPlan P8.2）

- 本文 §3.2 已经是"选定路径的一句话记录"，不需要再补。
- 同一次 `vmr report -lang zh` 下，`vmr-report.json` 的 `efficiency[].finding` 为中文，与
  `vmr-report.md` §7 表格里同一条 finding 的中文文案逐字一致。
- `buildFindingsForJSON` 保留，`go test ./internal/report/...` 全绿（包括
  `TestBuildFindingsIsDeterministic` 不受影响——它调用 `Build()` 不调用 `LocalizeEfficiency`，
  验的是"选中同一个 Code"这条不变式，与语言无关）。

---

## 4. 任务三：文档回填（P8.3）

### 4.1 现状（已读代码/文档确认，见 §0；本节额外核实了设计文档里的完整引用面）

`docs/VirtualModelRouter_Design_v4_Analytics.md` 里与"JSON 恒英文"这条规则相关的引用，全文
grep 确认共 **4 处**（DevPlan 原文只点名了 §4.3，本文核实后发现还有 3 处联动引用，一并列出，
避免重写完 §4.3 之后留下三处仍然自相矛盾的旁注）：

1. **第 56 行**（§4 开篇概览一句话）："`vmr-report.json` 的叙述性字段（`Finding.Finding` 等）
   固定英文，不随语言变化——`Build` 本身完全不接收 `lang`。"——"`Build` 完全不接收 `lang`"这半句
   在 P8.2 选定路径 (b) 之后**仍然成立**（`Build`/`BuildCached` 确实没加 `lang` 参数），但"固定
   英文，不随语言变化"这半句不再成立。
2. **第 196 行**（§4 `vmr story` 小节）："`journey-<id>.json`/`compare-*.json` 里其余的叙述字段
   （`MetricDiff.Label`）仍然固定英文——`Compare` 本身不接收 `lang`（§4.2）。"——P8.1 之后
   `Compare` 已经接收 `lang`，这句话整句不再成立。
3. **第 413–415 行**（§4.3 标题 + 2026-08-17 更新提示）：整节标题"JSON 契约：叙述字段固定英文，
   本地化只发生在渲染层"与正文（416-421 行）都要按策略文档 §2 的结论重写；已有的更新提示（415
   行，指向策略文档）在重写完成后可以删除——它存在的目的就是"过渡期指路牌"，目的地文档本身
   （本节）改完之后，指路牌自然失效。
4. **第 506 行**（决策与取舍表格一行）："叙述字段（`Finding.Finding`/`MetricDiff.Label`）在
   JSON 里固定英文，只有 Markdown 跟随 `-lang`（§4.3）| JSON 也跟随语言 | JSON 是给脚本消费的
   机器接口；固定英文让任何下游消费者不用先判断……"——这是"已裁决方案 vs 备选方案 vs 理由"表格
   的一行，P8 落地后**已裁决方案与备选方案互换**，理由也要换成策略文档 §2.3 的三条论证（`Code`
   已是稳定锚点、真实消费方只认 `Code`、团队规模与场景不需要双语 JSON）。

`docs/UserGuide.md:433`/`docs/UserGuide.zh.md:431`（各一段，同一处英文/中文版本）：

> "This only changes what the Markdown documents say — `vmr-report.json`/`journey-*.json`/
> `compare-*.json` are unaffected: every narrative field in them ... stays English regardless
> of `-lang` ..."

**这句话在 P8 之前就已经不准确**（`journey-*.json` 早就跟随语言了，DevPlan 原文已指出这一点，
本文核实属实）——P8 落地后 `compare-*.json`/`vmr-report.json` 也跟随，整句话需要改写为"三种 JSON
产物的叙述字段现在都跟随 `-lang`，`Code`/`EvidenceAnchor` 才是稳定不变的机器锚点"这个方向，不是
小修个别词。

`docs/KNOWN_ISSUES_sonnet-5.md` 的 `§1.19`（"JSON 输出的语言策略：`story`/`report` 两个包目前
不一致"）：P8 落地后这条待定问题彻底解决，移入 `§3 已闭环`。

`CHANGELOG.md`：`[Unreleased]` 当前已有 `### Changed`/`### Fixed` 两个分区（P7 收尾时确认过
结构）——P8 是一次行为变化（同一份日志、同一次 `-lang zh`，JSON 产物内容跟以前不同了），按项目
惯例记一条 `Changed`，不是 `Fixed`（不是修 bug，是刻意的策略调整）。

### 4.2 具体步骤

1. `docs/VirtualModelRouter_Design_v4_Analytics.md`：
   - 第 56 行：改为"`vmr-report.json` 的叙述性字段（`Finding.Finding` 等）跟随 `lang`——
     `Build` 本身仍不接收 `lang`，`cmd_report.go` 在写 JSON 前调用
     `report.LocalizeEfficiency(rep, lang)` 按实际语言覆写。完整机制见 §4.3。"
   - 第 196 行：改为"`journey-<id>.json`/`compare-*.json` 里的叙述字段（`MetricDiff.Label`
     等）现在都跟随 `lang`——`Compare` 已接收 `lang` 参数（§4.3）。"
   - §4.3 整节重写（标题改为反映新规则，如"叙述字段跟随 `-lang`，`Code`/`EvidenceAnchor` 是
     稳定机器锚点"），正文基于策略文档 §2 整理，说明 `story`（`Compare` 直接接收 `lang`）与
     `report`（`Build` 不接收，`cmd_report.go` 调用点覆写）两条机制不同的原因（本文 §3.2 已经
     写清楚，直接搬）；删除 2026-08-17 的更新提示行。
   - 第 506 行的决策表格行：互换已裁决/备选两列内容，理由列换成策略文档 §2.3 的三条论证（精简
     成表格单元格能装下的长度）。
   - 全文完整性检查：重写完之后 `grep -n "固定英文\|恒为英文\|叙述字段" docs/VirtualModelRouter_Design_v4_Analytics.md`
     确认不再有遗漏的旧规则表述。
2. `docs/UserGuide.md:433`：整句改写为——"This only changes what the Markdown documents say
   in terms of chrome — `vmr-report.json`/`journey-*.json`/`compare-*.json`'s narrative fields
   (e.g. `efficiency[].finding`, `compare-*.json`'s `rows[].label`) now follow `-lang` too, the
   same as the Markdown; `FindingCode`/`MetricCode`/`EvidenceAnchor` are the stable,
   language-independent anchors a script should key off of instead."（精确措辞以实现为准，
   动手时核对）。
3. `docs/UserGuide.zh.md:431`：同一段的中文翻译，同步改写，语义与英文版一致（不是逐字直译，
   遵循该文件一贯的表达习惯）。
4. `docs/KNOWN_ISSUES_sonnet-5.md`：`§1.19` 整条移入 `§3 已闭环`，仿照 P7 那三条已闭环条目的
   格式（简述做了什么、指向 P8 ActionPlan 的执行记录）；`§0` 的"当前状态"分布统计同步重算
   （中危/低危计数各减一）。
5. `CHANGELOG.md`：`[Unreleased]/### Changed` 补一条，说明 `-lang zh` 下
   `vmr-report.json`/`compare-*.json` 的叙述字段从英文变为中文（`journey-<id>.json` 不用提，
   它早就是这样，不是本次变化）。
6. `internal/story/metrics.go:419` 附近 `Summarize` 的 doc comment（"This is staged progress
   toward a project-wide lang-follows-everywhere policy that isn't fully applied yet —
   compare-*.json's MetricDiff.Label and vmr-report.json's efficiency[] still fix to EN"）：
   P8 落地后这句话不再成立，改写或删除这几句"尚未完成"的说明（保留该函数其余部分关于
   `-compare` 路径重复计算 `Structure` 的说明，那部分与语言策略无关，不动）。
7. `wc -l internal/story/metrics.go`——当前 453/470，改动是净删减（去掉几句过期说明），预期
   行数下降，不会跨线，但仍跑一遍确认。

### 4.3 验收标准（对照 DevPlan P8.3）

- `docs/VirtualModelRouter_Design_v4_Analytics.md` 全文不再有"叙述字段固定英文"一类表述与
  P8 之后的实现矛盾（§4.2 步骤 1 的 grep 检查通过）。
- `docs/UserGuide.md`/`.zh` 不再有与实现不符的语言声明。
- `docs/KNOWN_ISSUES_sonnet-5.md` 的 `§1.19` 已移入 `§3`。
- `CHANGELOG.md` 有对应 `Changed` 条目。

---

## 5. 任务四：测试反转与新增一致性检验（P8.4）

### 5.1 现状（已读代码确认，见 §0 与本文 §2.4/§3.5 的"预期失败"提示）

`cmd/vmr/i18n_e2e_test.go`：
- `TestE2E_ReportLangFlagZh`（第 119 行）：断言 `rep.Efficiency` 里 `tool_schema_waste` 的
  `.Finding` 恒为英文字面量 `"Tool schema waste"`（第 137 行断言）。P8.2 落地后这条断言会失败
  ——`-lang zh` 下应为中文 `"工具 schema 浪费"`（已核实 `internal/i18n/report_efficiency.go:71`
  的 ZH `ToolSchemaWasteFinding` 返回的 `Title` 就是这个值）。
- `TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish`（第 327 行）：断言 `compare-*.json` 的
  `rows[].label`（`model_ms`）恒为英文字面量 `"Model Time"`（第 394 行断言）。P8.1 落地后应为
  中文 `"模型时间"`（已核实 `internal/i18n/story_compare.go:141` 的 ZH `MetricLabels["model_ms"]`
  就是这个值）。

### 5.2 具体步骤

1. `cmd/vmr/i18n_e2e_test.go`：
   - `TestE2E_ReportLangFlagZh` 改名为 `TestE2E_ReportLangFlagZh_EfficiencyFollowsLang`
     （反映断言方向反转，不是删除重写——保留"这条规则曾经是相反的、现在有意识地改变了"这个历史
     信号，与 P7 处理"恒英文"断言反转时的惯例一致）；doc comment 改写为描述新规则；断言从
     `f.Finding != "Tool schema waste"` 改为 `f.Finding != "工具 schema 浪费"`。
   - `TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish` 改名为
     `TestE2E_StoryCompareLangZh_JSONLabelsFollowLang`；doc comment 改写；断言从
     `r.Label != "Model Time"` 改为 `r.Label != "模型时间"`。
2. 新增一个端到端一致性测试（新函数，同一文件）：命名建议
   `TestE2E_LangZh_AllThreeJSONOutputsAgree`。设计：
   - 复用现有 fixture：`e2eReportFixture`（触发 `tool_schema_waste`）与
     `e2eStoryFixture`（产出 2 个候选 Journey，可 `-compare`）目前写的是两份不同的审计日志，各自
     服务不同的命令（`e2eReportFixture` 只给 `cmdReport` 用，`e2eStoryFixture` 只给 `cmdStory`
     用）——**不需要合并成一份日志**，三次独立调用（`cmdReport`、`cmdStory -journey`、
     `cmdStory -compare`）各自用各自现有的 fixture，测试要验证的是"同一次 `-lang zh` 下，三种
     JSON 各自的叙述字段都是中文"，不要求三者读同一份数据。
   - 步骤：对 `e2eReportFixture` 跑 `cmdReport(["-o", outDir, "-lang", "zh", path])`，读
     `vmr-report.json`，断言 `efficiency[].finding` 命中中文。对 `e2eStoryFixture` 先跑
     `cmdStory` 列出候选拿到两个 id（复用
     `TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish` 现有的列出逻辑），再跑
     `cmdStory(["-o", outDir2, "-journey", id, "-lang", "zh", path])`，读
     `stories/journey-<id>.json`，断言 `findings[].finding`（若该 fixture 没有真实
     Finding，退化为断言 JSON 能正常解析且不报错即可——`journey-<id>.json` 的语言跟随行为
     P7 之前就已验证过，这里的重点是三者**同一次测试内**核对，不是重新验证每一种各自的正确性）；
     再跑 `-compare`，读 `compare-*.json`，断言 `rows[].label` 命中中文。
   - 断言意图：不是要求三次调用的输出互相引用或交叉验证，而是**同一个测试函数**里，对三种
     产物分别起手工验证——这正是策略文档 §3.5 点名的"各自通过、整体没人核对"这个空当，用一个
     测试函数把三次断言放在一起，任何一处未来的回归都会在同一个失败点上暴露，不需要分别去
     三个独立测试里定位。
3. `go test ./cmd/vmr/... -run "TestE2E" -v` 确认新测试与两个改名后的测试全部通过，旧名字
   （`TestE2E_ReportLangFlagZh`/`TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish`）在
   `go test` 输出里不再出现（改名生效的直接证据）。

### 5.3 验收标准（对照 DevPlan P8.4）

- 两个"恒英文"测试已改名并反转断言方向，`go test` 通过。
- 新增的端到端一致性测试通过，且是"新测试"而非"扩展现有测试"（DevPlan 原文用词是"新增"）。
- `go test ./... -race` 全绿。

---

## 6. 收尾（P8.1–P8.4 共用）

1. `go build ./cmd/vmr && go test ./... 2>&1 | tail -30`——全绿。
2. `go test -race ./internal/report/... ./internal/story/... ./internal/i18n/... ./cmd/vmr/...`
   ——四个子任务都改了广泛调用的函数签名，`-race` 复核一次成本很低。
3. `go test ./internal/archtest/...`——文件行数预算（`compare.go`/`compare_metrics.go`/
   `metrics.go` 三处都动了行数）、函数行数预算、导入边界、文档引用完整性全部复核；`compare.go`
   若仍需要豁免登记，按实测行数 +15~20% 写入 `internal/archtest/file_sizes_test.go` 的
   `fileLineExemptions`，不要沿用旧的 850。
4. 用真实语料完整重跑：
   ```bash
   vmr report -o /tmp/p8verify/en logs/vmr-audit-2026-07-28.jsonl.zst
   vmr report -lang zh -o /tmp/p8verify/zh logs/vmr-audit-2026-07-28.jsonl.zst
   vmr story -render-all -o /tmp/p8verify/en logs/vmr-audit-2026-07-28.jsonl.zst
   vmr story -render-all -lang zh -o /tmp/p8verify/zh logs/vmr-audit-2026-07-28.jsonl.zst
   ```
   人工打开 `vmr-report.json`（`efficiency[]`）、`journey-*.json`（`findings[]`，若样本触发过）、
   跑一次 `-compare`（`compare-*.json` 的 `rows[].label`），确认 `zh` 目录下三种 JSON 的叙述
   字段都是中文、`en` 目录下都是英文，且分别与同目录下对应的 `.md` 用词一致——不能只信单元测试
   全绿，策略文档 §5 明确要求人工核对这一步。
5. `CHANGELOG.md`/`docs/KNOWN_ISSUES_sonnet-5.md` 已在 P8.3 更新，这里只做最终核对（`git diff`
   过一遍，确认没有遗漏）。
6. 本文按 P7 ActionPlan 的既有惯例，执行完毕后在文末补一节"执行记录"（本文写就时留空，不预先
   编造）。
7. `docs/future-strategy/story_report_dev_plan_2_sonnet-5.md`：P8 完成后，比照 P7 完成时的
   做法，在 §3 总览表的 P8 行与 §4 的"### P8"小节标题各加一个 ✅ 完成标记，指向本文的执行记录
   小节；不重写正文其余部分。

---

## 7. 验收清单（对照第二期 DevPlan P8 的验收标准逐项勾）

- [x] 同一次 `-lang zh` 下 `compare-*.json` 的 `Label` 与对应 Markdown 用词一致（P8.1）。
- [x] 选定路径（(b)）有一句话记录，理由清楚；`vmr-report.json` 的 `efficiency[]` 在 `-lang zh`
      下为中文（P8.2）。
- [x] 三处文档互相一致，无自相矛盾的"本节只对部分产物成立"注记；`UserGuide.md`/`.zh` 不再有与
      实现不符的语言声明（P8.3）。
- [x] 两个"恒英文"测试已反转并改名；新增的端到端一致性测试通过（P8.4）。
- [x] `go test ./... -race` 全绿；`go test ./internal/archtest/...` 全绿。
- [x] 真实语料对 `-lang zh`/`-lang en` 各跑一遍，人工核对三种 JSON 输出语言真正统一。
- [x] `CHANGELOG.md`、`docs/KNOWN_ISSUES_sonnet-5.md`（`§1.19` 移入 `§3`）已同步。
- [x] `story_report_dev_plan_2_sonnet-5.md` 的 P8 行/小节已加完成标记。

---

## 8. 执行记录（2026-08-20）

**范围**：本文 P8.1–P8.4 全部四项任务已按本文设计执行完毕，`go build`/`go test ./...`/
`go test -race ./internal/report/... ./internal/story/... ./internal/i18n/... ./cmd/vmr/...`/
`go test ./internal/archtest/...`/`gofmt -l .`/`go vet ./...` 全绿；用本机真实日志
（`logs/vmr-audit-2026-07-28.jsonl.zst`）对 `-lang en`/`-lang zh` 各跑一遍 `vmr report`、
`vmr story -render-all`、`vmr story -compare`，人工核对三种 JSON 输出与对应 Markdown 逐字一致。
所有改动尚未提交，留待人工 review。

**与本文设计的落差**（均为实现期发现，非本文事先预判到的偏差，按"以第一性原理和实事求是的
态度"就地判断、不预先假设）：

1. **`compare.go` 拆分边界的行号有 2 行误差**：本文 §2.2(a) 写的"第 21–152 行"，实际核对文件
   （`cat -n` 精确核实）应为**第 23–152 行**——`MetricKind` 的 doc comment 从第 23 行开始，
   第 21/22 行是 import 块的收尾 `)` 和空行。这是本文撰写时用 `grep -n` 输出的函数起始行号做
   估算、未逐字节核对造成的小偏差，落地时已用 `cat -n` 精确核对后按正确边界（23–152，130 行）
   剪切，不影响设计本身——移动的内容与本文列出的清单完全一致。
2. **`go vet` 发现本文遗漏的第 10 处 `Compare()` 调用点**：本文 §2.3 步骤 6 列出的 9 处测试调用
   （`compare_test.go` 8 处 + `llm_test.go` 2 处，注：`compare_test.go` 第 389 行是嵌套调用，
   本文按"共 8+2 处"计数）核实无误，但落地后 `go vet ./...` 额外发现
   `internal/story/llm_packs_test.go:205` 也有一处 `Compare(s, s)` 调用（`EvidencePack` 体积
   守卫测试的一个内部闭包），本文规划阶段的 grep 范围（只搜了 `compare_test.go`/`llm_test.go`
   两个文件）未覆盖到这个文件。已同步补上 `i18n.EN` 实参——本文 §1"依赖顺序"一节已经预先安排了
   `go vet` 复核步骤，这个遗漏正是它要捕捉的那类问题，按既定步骤处理，不是设计缺陷。
3. **`compare.go` 的行数预算重新登记比本文预判更宽松**：本文 §2.2(a) 预判"拆分后 `compare.go`
   ≈ 846 − 132 = 714 行"，实测精确值为 713 行（差 1 行，同上条边界计数误差的连带效应）；
   §5"收尾"步骤按"实测 +15~20%"重新登记豁免值，713×1.15≈820，已把
   `internal/archtest/file_sizes_test.go` 的 `internal/story/compare.go` 从 850 改为 820
   （不是简单沿用旧值，也不是本文 §2.2(a) 预判的"850 仍然够用"——850 恰好落在 +15~20% 区间内
   纯属巧合，重新计算后按更贴近实际的数字登记）。
4. **本文 §3.4 步骤 3 把两个不同文件的行数豁免登记搞混了**：写的是"`internal/report/metrics.go`
   当前 453/470 行豁免"，但 `internal/archtest/file_sizes_test.go` 里登记 470 行豁免的其实是
   `internal/story/metrics.go`（P8.3 §4.2 步骤 7 改的正是这一个）——`internal/report/metrics.go`
   从未登记过豁免，适用全局默认 700 行预算，当时实际是 443 行。这是本文撰写时同名文件（两个包
   都有 `metrics.go`）互相混淆造成的事实错误，不影响实际改动（443+20=463，无论是否豁免都远低于
   限制），但本文原文的行数焦虑描述是错的，已在 §3.4 就地更正。
5. **一份独立复核（`story_report_p8_action_plan_review_gemini-3.7-flash.md`）在本文执行过程中
   出现，核实后采纳了其中三项有价值的发现，均已落地**：
   - `internal/report/rows.go` 的 `Finding` 结构体 doc comment（"They are always English in
     this persisted struct...never derived from this struct after the fact"）、
     `internal/report/section_efficiency.go` 顶部注释（"that field is always English...never
     varies by language"）、`internal/i18n/report_efficiency.go` 包注释（"the always-English
     Report2.Efficiency JSON field"）三处——本文规划阶段只检查了 `buildFindingsForJSON` 自己
     的 doc comment，遗漏了这三处同样断言"恒为英文"的关联注释，P8 落地后均已过期。三处均已改写
     为准确描述（`Finding` 结构体注释说明 `LocalizeEfficiency` 会覆写它；
     `section_efficiency.go` 补充说明"为什么不读被覆写后的 `rep.Efficiency`"这条设计选择的
     具体原因；`report_efficiency.go` 改为准确描述三次调用（`Build` 内部 EN 默认值、
     `LocalizeEfficiency` 覆写、`renderEfficiency` 独立计算）而不是两次）。
   - `internal/report/recextract.go` 的 `WriteJSON` 补充一句 doc comment，明确"不调用
     `LocalizeEfficiency` 就不会本地化，且不报错"这条隐性契约——`LocalizeEfficiency`/
     `WriteJSON` 保持两个独立调用（不是把 `lang` 参数并进 `WriteJSON` 签名，那会让一个序列化
     函数承担本地化副作用，混淆两个概念），但两处 doc comment 现在互相指向，一个从任一函数入手
     的读者都能看到完整契约。
   - `TestE2E_ReportConfigFileZh` 补充断言 `vmr-report.json` 的 `efficiency[].finding`——原
     测试只查过 Markdown，`report.yaml` 单独设置语言（不带 `-lang`）时 JSON 是否也正确本地化
     此前只是"由 `lang` 单次解析、必然一致"这条实现事实推出的隐含结论，没有测试显式钉住。
   独立复核同时指出 `compare.go` 的新豁免值可以更紧（760，约 6% 余量），本文按自己 §5 已经
   写明的"+15~20%"标准保留了 820——两种取值都合理，是缓冲比例的偏好差异，不是对错问题，未采纳
   这一条改动。

**真实语料验证要点**（`vmr report`/`vmr story -render-all`/`vmr story -compare` 对同一份单日
日志分别 `-lang en`/`-lang zh` 各跑一遍，人工核对）：

- P8.1：`compare-*.json` 的 `rows[].label`（如 `model_ms` → "模型时间"）与同一次调用产出的
  `compare-*.md` 表格逐字一致；`-lang en` 下同一行为 "Model Time"。
- P8.2：`vmr-report.json` 的 `efficiency[]` 在 `-lang en`/`-lang zh` 下选中的 `Code` 集合完全
  相同（`['cache_miss', 'context_growth', 'slow_requests', 'tool_schema_waste']`），只有
  `finding`/`implicated`/`action` 文本随语言变化（如 `tool_schema_waste` 的 `finding` 从
  "Tool schema waste" 变为"工具 schema 浪费"）——证明 `LocalizeEfficiency` 只覆写文本、不改变
  `buildFindings` 的"挑出最浪费的那个"选择逻辑，与 §3.2 记录的设计前提一致。
- P8.4 新增的一致性测试：`TestE2E_LangZh_AllThreeJSONOutputsAgree` 在同一次测试函数内验证
  `vmr-report.json`/`journey-<id>.json`/`compare-*.json` 三者在 `-lang zh` 下都是中文，通过。

**文档同步**：`docs/VirtualModelRouter_Design_v4_Analytics.md` 的 4 处引用（§开篇概览 L56、
§4 `vmr story` 小节 L196、§4.3 整节、决策取舍表格一行）均已重写，`grep -n "固定英文\|恒为英文"`
确认无遗漏的旧规则表述残留（仅剩历史叙述性引用，见 §4.3 正文自身对 P8 之前状态的描述，措辞
准确、不是自相矛盾）；`docs/UserGuide.md:433`/`docs/UserGuide.zh.md:431` 已改写；
`docs/KNOWN_ISSUES_sonnet-5.md` 的 `§1.19` 移入 `§3`（新增第 30 项），`§0`/`§4` 的分布统计与
ROI 表覆盖范围声明同步重算；`CHANGELOG.md` `[Unreleased]/### Changed` 补一条。

**未做的事**（按 P8 边界声明，本文未预判需要而未做）：`journey-<id>.json`/`compare-*.json` 均
未新增 `schema_version`/`lang` 元字段——`json_lang_policy_plan_sonnet-5.md` §2.4 把这个问题
列为"留给实施时权衡"而未下结论，落地时判断：全仓唯一已知的程序化消费方（`_eval/calibrate_p1b.go`）
只读 `EvidenceAnchor`，不依赖也不需要知道生成时的语言；`KNOWN_ISSUES §1.29` 已经论证过
`journey-<id>.json` 不需要 schema 版本戳的理由（没有消费者就没有人需要探测版本），同一条理由
适用于"记录生成语言"这件事——按 YAGNI 原则不加，与项目一贯的"不为不存在的消费者加机制"的判据
一致，不是遗漏。
