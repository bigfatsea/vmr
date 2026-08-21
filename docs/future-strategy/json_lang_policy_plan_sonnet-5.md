<!-- Ver 2026-08-17, by Sonnet 5 -->

# `vmr report`/`vmr story` JSON 输出的语言策略统一方案

> **文档性质**：这是一份方案/大纲文档，**不是实施记录**。截至本文档写就时，方案本身尚未落地——只有一次意外的、局部的先行改动（见第 1 节），本文档的目的是把方向定下来、把改动范围列出来，供下一次正式推进时直接对照执行，而不是从零重新分析一遍。
>
> **现状（2026-08-21）**：本文档 §2/§3/§5 定型的方向已在 P8 阶段基本全盘采纳落地。
> 本文档保留作实施期的模块级大纲参考，不需要作为独立提案重新论证方向。
>
> **背景**：`phase1a_phase1b_implementation_review_opus-5.md`（已归档，不在版本控制范围内）复核发现问题 3-1（`journey-<id>.json` 缺 LLM Finding），`phase1_issues_implementation_plan_gemini-3.7-flash.md`（已归档，不在版本控制范围内）给出了修复方案并已落地。修复过程中，`internal/story` 顺带把 `journey-<id>.json` 的叙述文本从"固定英文"改成了"跟随 `-lang`"，这个改动本身没有在原计划里，复核后发现它撞上了一条项目级、有专门回归测试守护的既有规则（`docs/VirtualModelRouter_Design_v4_Analytics.md` §4.3《JSON 契约：叙述字段固定英文》）。经讨论，我们认为"JSON 也应该跟随目标语言"这个方向本身是对的，但完整统一是一次跨 `report`/`story` 两个包、涉及签名变更和测试反转的改动，不适合在修 3-1 的过程中顺手做完，因此单独立项、写成这份方案文档，本次先暂停在一个自洽的中间状态。

---

## 0. 一句话总结

**方向**：JSON 输出和 Markdown 输出的叙述性文本应该统一跟随 `-lang`/`report.yaml` 的 `language` 设置，不再保留"JSON 里的叙述句子永远是英文"这条特例。程序化消费真正应该依赖的稳定锚点是 `FindingCode`/`MetricCode` 这类枚举标识符和 `EvidenceAnchor` 这类原文逐字摘录——它们本来就不受 `-lang` 影响，"叙述句子恒英文"这层规则保护的东西，这两类字段已经保护到了，是多余的第二道保险。

**现状**：这个方向目前只有 `story` 包的 `journey-<id>.json`（`Findings`/`LLMFindings` 两个字段）已经按新方向实现了，而且是修 3-1 时的副产品，不是按这份方案有计划地推进的。`compare-<a>-<b>.json` 的 `MetricDiff.Label`、`report` 包的 `vmr-report.json` 的 `efficiency[]` 仍然固定英文，维持旧规则。我们决定**保留**这个局部改动（不回退），同时**不在这次继续往下做**——完整统一是下一次的大动作。

---

## 1. 当前所处的位置（2026-08-17，暂停点快照）

| 输出 | 字段 | 当前行为 | 本次改动？ |
|---|---|---|---|
| `journey-<id>.json` | `Findings[].{finding,evidence,action}` | 跟随 `-lang`（`Summarize(j, lang)`） | 是（3-1 修复的副产品） |
| `journey-<id>.json` | `LLMFindings[].{finding,evidence,action}` | 跟随 `-lang` | 是（3-1 修复本身） |
| `journey-<id>.json` / `compare-*.json` | `Code`/`FindingCode`/`MetricCode` | 恒定英文枚举值，从不本地化 | 否，本来就如此，不属于这次讨论范围 |
| `compare-<a>-<b>.json` | `Rows[].MetricDiff.Label` | **固定英文**（`Compare()` 不接收 `lang`） | 否，未改动 |
| `vmr-report.json` | `efficiency[].{finding,implicated,action}` | **固定英文**（`buildFindingsForJSON` 强制 `i18n.EN`） | 否，未改动 |
| `*.md`（journey/compare/report 各自的 Markdown） | 全部叙述文本 | 一直都是跟随 `-lang`（本来就对） | 否 |

**为什么判断这个中间状态可以作为暂停点保留，而不需要回退**：

1. 已经改的那部分（`journey-<id>.json`）本身的方向和第 2 节确定的原则一致，不是"改错了要退回去"，只是"改得不完整、没有配套决定和文档"。
2. 唯一真正有风险的是**自相矛盾**——`internal/story/findings.go` 曾经有两处注释仍然写着"journey-<id>.json (always EN)"，跟同一次改动里 `internal/i18n/story_findings.go` 新写的"文本跟随 lang"互相打脸；这一点已经在本次顺手修掉（见下）。修掉之后，当前状态是"部分完成、有清楚记录"，不是"半吊子且没人知道"。
3. 影响面是可控的：`journey-<id>.json` 目前唯一已知的程序化消费方是 `_eval/calibrate_p1b.go`，它只匹配 `EvidenceAnchor`（原文摘录），不依赖 `Finding`/`Action` 文本的语言，因此这个中间状态不会破坏任何现存工具。

**本次顺手做的、属于"止损"而非"推进方案"的修改**：

- `internal/story/findings.go`：`ComputeFindings` 的文档注释、`Finding` 结构体里 `Finding`/`Evidence`/`Action` 三个字段的文档注释——原文断言"journey-<id>.json 恒为英文"，已改为如实描述现状（`journey-<id>.json` 目前跟随 `lang`），并指向本文档。
- `internal/story/metrics.go`：`Summarize` 的文档注释补充一句指向本文档的说明，避免读者误以为这是完整、经过审慎决定的最终状态。
- `docs/VirtualModelRouter_Design_v4_Analytics.md` §4.3：加了一条更新提示，说明本节描述的规则目前只对 `compare-*.json`/`vmr-report.json` 仍然成立，`journey-<id>.json` 已偏离，指向本文档。**没有重写这一节的正文**——正文的重写要等方案真正统一实施完之后再做（见第 5 节）。
- `docs/KNOWN_ISSUES_sonnet-5.md`：新增 1.19 条目，登记这个待决问题，指向本文档，避免它变成"只存在于某次会话记录里、下次没人知道"的暗知识。

**明确没有做的事**：没有给 `internal/story/compare.go` 的 `Compare()` 加 `lang` 参数，没有碰 `internal/report` 任何代码，没有重写设计文档正文，没有反转任何现有测试的断言方向。这些都留给第 5 节说的"下一步"。

---

## 2. 确定下来的语言原则

> 这是本文档最核心的部分——下次推进这项工作时，应该以这一节为准，不需要重新论证一遍"要不要统一"。

### 2.1 原则本身

**叙述性文本统一跟随 `-lang`（含 `report.yaml` 的 `language` 字段解析出的最终值），JSON 输出与 Markdown 输出必须使用同一份语言渲染。** 不再存在"某些字段的 JSON 版本固定英文、只有 Markdown 版本跟随语言"这种特例。具体覆盖：

- `Finding.Finding`/`Finding.Evidence`/`Finding.Action`（`story` 与 `report` 两个包的 Finding 类型都算）
- `MetricDiff.Label`（`story.Compare` 产出的对比标签）
- 任何未来新增的、走 i18n 模板生成的叙述句子

### 2.2 明确保持不变、不受这条原则影响的字段

- **`FindingCode`/`MetricCode` 一类枚举标识符**：永远是英文字符串常量（如 `"tool_result_misinterpretation"`、`"model_switch_count"`），从不参与本地化。这是程序化消费方**唯一应该依赖**的稳定锚点。
- **`EvidenceAnchor`**：原文逐字摘录，它的"语言"由被摘录的原始对话内容决定，从来就不是模板文本，这条原则不适用、也不需要适用。
- **发给 LLM 的 system prompt**（`internal/i18n/story_llm.go` 的 `i18n.LLM(lang)`，design doc §4.5《LLM 解读层的语言联动》）：这部分已经正确跟随 `lang`，本来就没有"固定英文"的问题，不在本方案讨论、也不需要改动。

### 2.3 为什么改（论证，供下次直接引用，不需要重新想一遍）

1. **`Code` 已经是真正稳定的机器锚点**。"叙述句子固定英文"这条规则原本要保护的东西（外部脚本可以不受语言选择影响地消费 JSON），`Code` 字段已经保护到了。叙述句子再额外锁死英文，是重复保险，不是唯一防线。
2. **这个项目里唯一真实存在的 JSON 消费脚本已经证明不需要这层保险**：`_eval/calibrate_p1b.go` 对 Finding 数据做机械校验时，匹配的是 `EvidenceAnchor`（原文逐字摘录），完全不触碰 `Finding`/`Evidence`/`Action` 的模板文本，也不关心它们是什么语言。"锁死英文是为了保护外部工具"这个理由，在这个项目当前的实际使用场景里找不到对应的真实需求。
3. **团队规模与使用场景**：≤3 人、聚焦中国大陆场景的团队，`report.yaml` 的 `language: zh` 基本就等于"操作者只想全程看中文"。JSON 里混一半英文、一半中文，对这个使用模式没有实际价值，只会增加"这个字段为什么是英文而那个字段是中文"的认知负担。
4. **维护成本**：单一语言原则更简单、更不容易再复现这次实际发生的问题——同一个包里两份文档互相矛盾（一份说"恒为英文"，一份说"跟随语言"），根源就是规则本身有特例、且特例没有被完整地贯彻到每一处。统一之后，"JSON 和 Markdown 用的是同一个 `lang`"是唯一需要记住的规则，不需要在每个字段旁边分别注明它属于哪一类。

### 2.4 需要一并想清楚、但本文档不预先下结论的问题

- **外部脚本的语言假设是否需要一个显式声明**：即使统一之后，如果未来真的出现团队外部/长期存档的自动化脚本读取这些 JSON 做文本层面的匹配（而不是只匹配 `Code`），它们需要知道"这份 JSON 的叙述文本语言由生成它的那次命令行调用决定，不是恒定的"。这大概率应该写进 UserGuide 而不是代码注释里，具体怎么写留到实施时再定。
- **是否需要在 JSON 里额外记录"这份文件是用哪个 `lang` 生成的"这一元信息**（比如 `JourneySummary`/`Report2` 顶层加一个 `lang` 字段），方便消费方在需要时自行判断，而不是只能通过外部约定知道。这个问题在这次讨论里被提出但没有定论，留给实施时权衡（多一个字段 vs. 多一层复杂度）。

---

## 3. 完整统一需要改的范围（大纲级别，不含逐行细节）

> 下面按模块列出"要改什么、大概涉及哪些文件/函数"，**不含具体到某一行代码的改法**——那部分留到真正动手实施时，对照本大纲逐项去源码里核实、再决定具体写法。

### 3.1 `internal/report`

- **`Build`/`BuildCached`（`internal/report/aggregate.go`/`build_cached.go`）**：目前完全不接收 `lang` 参数，是这次要解决的核心架构问题。两个候选方向：
  - (a) 给 `Build`/`BuildCached` 增加 `lang` 参数，内部直接用它调用 `buildFindings(rep, lang)` 而不是固定 `i18n.EN`；
  - (b) 保持 `Build` 语言无关（现状），改为在 `cmd/vmr/cmd_report.go` 调用 `report.WriteJSON(rep, jsonPath)` 之前，用调用点已经拿得到的 `lang`（就在附近几行，`Markdown(rep, lang)` 紧随其后）现场覆盖/重算 `rep.Efficiency` 字段——这条路径改动面明显更小，不需要给 `Build`/`BuildCached` 这两个已经很长的函数签名再加参数。
  - 选哪条路径需要先确认：`rep.Efficiency` 在 `Build()` 内部有没有被其他逻辑读取、依赖其英文文本本身（而不是 `Code`）；如果没有，(b) 应该是优先选项。
- **`buildFindingsForJSON`（`internal/report/metrics.go`）**：这个专门"强制传 `i18n.EN`"的包装函数本身要么删掉（如果改走方案 (b)，调用点不再需要它），要么改造为接收 `lang`。
- **`section_efficiency.go`**：目前的注释解释了"为什么 Markdown 渲染要对 `buildFindings` 再单独调一次"——如果最终 JSON 和 Markdown 用的是同一个 `lang`，这处重复计算本身可能也可以顺手消掉；但要在动手时先确认这次"重复调用"是否还有其他隐藏理由（比如渲染层需要的调用时机跟 JSON 序列化的时机本来就不同），不要想当然地假设可以直接合并。

### 3.2 `internal/story`

- **`compare.go` 的 `Compare(a, b JourneySummary) Comparison`**：需要新增 `lang` 参数；`Rows` 里 `MetricDiff.Label` 从当前的硬编码英文字面量改成走 `i18n.MetricLabel(lang, code)`——这张查表函数（`internal/i18n/story_compare.go`）已经存在，目前只喂给 Markdown 渲染用，改造成本主要是把它也接进 `Compare()` 自己的 JSON 构造路径，不需要新写查表逻辑。
- **所有 `Compare(a, b)` 的调用点**：`cmd/vmr/cmd_story.go` 的 `compareJourneys`，以及 `internal/story/compare_test.go`/`llm_test.go` 里所有直接构造 `Compare(...)` 的测试，都要跟着补上 `lang` 实参。
- **`metrics.go`/`cmd_story.go` 这次已经改完的部分**（`Summarize(j, lang)`、`writeJourneyFile` 直接用调用方已算好的 `findings` 构造 JSON）：基本不需要再动，只需要在这次统一真正完成之后，把这次加的临时性"待决"注释（指向本文档那几处）清理成正式的、不再需要外部指针的说明。

### 3.3 `internal/i18n`

- 基本不需要新增机制。`MetricLabel`、`StoryFindings` 这些按 `lang` 返回文本/闭包的函数已经存在、已经支持任意 `lang`——缺的只是"JSON 生成路径要不要也调用它们"这一步接线，不是缺能力。

### 3.4 文档

- **`docs/VirtualModelRouter_Design_v4_Analytics.md` §4.3**：需要整节重写（不是打补丁）。把"JSON 恒英文，本地化只发生在渲染层"的标题和论证方向，换成"叙述文本统一跟随 `-lang`，`Code`/`EvidenceAnchor` 才是稳定机器锚点"，论证内容基本可以从本文档第 2 节整理后并入。重写完成后，本文档可以归档（在文档开头加一行"已并入设计文档 §4.3，本文档仅作历史记录"），不需要删除。
- **`docs/KNOWN_ISSUES_sonnet-5.md`**：1.19 条目在统一完成后需要移到"已闭环"（第 3 节）。
- **`CHANGELOG.md`**：如果统一实施，这是一次会改变已有 JSON 输出内容的行为变化（哪怕只是文本语言，不是结构），应该在 `[Unreleased]` 里用 `Changed` 记一条，说明"在 `-lang zh` 下，`vmr-report.json`/`compare-*.json` 的叙述字段现在会是中文，不再固定英文"，方便任何人（包括未来的自己）排查"为什么这次报告文本变了"。
- **`docs/UserGuide.md`/`.zh`**：如果 2.4 节提到的"要不要显式声明 JSON 语言由生成命令决定"有了结论，同步写进用户文档。

### 3.5 测试

- **`cmd/vmr/i18n_e2e_test.go` 的 `TestE2E_ReportLangFlagZh`**（断言 `vmr-report.json` 恒英文）**和 `TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish`**（断言 `compare-*.json` 恒英文）：这两个测试的名字本身就是旧规则的化身，统一完成后需要整体反转断言方向。建议保留测试、改名 + 改断言内容（而不是直接删除）——删除会丢失"这条规则曾经被专门验证过、现在是有意识地改变而不是意外破坏"这个历史信号。
- **`internal/story/compare_test.go`**：所有 `Compare(a, b)` 调用点补 `lang` 实参（大概率是最机械、改动点最多的一步）。
- **新增一个端到端测试**：在同一次 `-lang zh` 运行下，验证 `journey-<id>.json`、`compare-*.json`、`vmr-report.json` 三种输出的叙述文本语言**同时**一致。这一条本身就是这次"分阶段统一"要解决的问题——三个包各自的测试分别通过，不等于整体一致，这正是当前这个未完成状态之所以发生的原因（`story` 先动了，`report` 没跟上，且没有任何测试会同时盯着两边）。

---

## 4. 明确排除在这次范围之外的事项

- 不改 `FindingCode`/`MetricCode` 本身的取值——它们永远是英文枚举字符串，这是第 2.2 节已经定下的。
- 不改 `EvidenceAnchor` 的来源逻辑。
- 不改 LLM system prompt 的语言联动机制（design doc §4.5）——那部分已经是正确的，不需要碰。
- **`TestCmdStory_JourneyWithRealLLM`**（这次顺手加的、会在 `report.yaml` 配置了可达 `llm_addr` 时真的发起 LLM 网络请求的测试）：这是另一个话题（测试是否应该在本机默认 `go test` 时打真实网络请求、要不要加显式 opt-in 闸门），已经在复核中单独提出，跟本文档讨论的语言策略无关，不在这里处理。

---

## 5. 下一步：真正推进这件事时的建议顺序

1. **`internal/story` 内部先行**（3.2）：`Compare()` 加 `lang`，因为已经有现成的 `i18n.MetricLabel` 可以直接复用，改动面相对集中在一个包内。
2. **`internal/report`**（3.1）：需要先拍板 `Build`/`BuildCached` 要不要接收 `lang`（还是走"调用点现场覆盖"的路径），这是本方案里唯一需要在动手前做架构判断的地方，建议单独花时间想清楚再写代码。
3. **文档回填**（3.4）：设计文档 §4.3 整节重写、`KNOWN_ISSUES` 1.19 闭环、`CHANGELOG` 记录。
4. **测试反转与新增端到端断言**（3.5）：两个"恒英文"测试改名反转，`compare_test.go` 补参数，新增跨三种输出的语言一致性端到端测试。

每一步都应该单独跑一遍 `go test ./... -race`（`report`/`story`/`cmd/vmr` 三个都要覆盖）与 `go test ./internal/archtest/...`，且最后要用真实语料对 `-lang zh` 和默认 `-lang en` 各跑一遍 `-journey`/`-compare`/`vmr report`，人工打开三种 JSON 输出核对语言真的统一了——不能只看单元测试全绿就判定完成，这次的部分完成状态本身就是"单元测试各自通过、整体没人核对"造成的。
