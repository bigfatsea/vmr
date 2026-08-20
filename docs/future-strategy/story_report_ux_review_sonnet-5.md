// Ver 2026-08-19 12:20, by Sonnet 5

# `vmr story` 报告可读性优化 — 分析记录与决策留白

本文档是对用户给出的两份样例报告（`compare-j-openclaw-...-vs-j-lobster-...md`、
`journey-j-openclaw-...md`）提出的 7 项优化诉求的第一性原理分析记录。每一条结论都基于
**读源码 + 用本机真实 `~/code/vmr/logs/` 审计日志重新跑一遍 `vmr story` 验证**，不是凭对
报告内容或用户描述的印象判断。方法论细节见文末"验证方法"。

结论分两类：

- **已直接处理**（结论清晰 + 改动风险低）：见"第一部分"，已改代码、改测试、`go build && go
  test ./...` 全绿、并用真实日志重新生成报告肉眼核对过。第一轮分析曾把 journey 报告的 3
  条诉求（脊柱截断/system prompt 位置/工具调用结果）判定为"需要你先拍板"而搁置——**第二轮
  复核推翻了这个判断**：把"脊柱截断多少字符"和"system prompt 放哪"两个问题拆开后，发现
  它们并不像最初以为的那样互相牵连，各自都有清晰、低风险的解法，已经一并做完，详见第一
  部分 4-6。
- **留待你确认**（方案存在多个合理选项，或者改动本身有架构层面的取舍）：见"第二部分"，
  每条给出我的分析、推荐方案和它的代价，不代替你做最终决定。第二轮复核后，这部分只剩 2
  条实质性留白（Compare 报告开篇完整消息、fact-layer 改为链接 detail report）——原来的
  "决策脊柱截断+system prompt 位置"、"工具调用结果展示"两条已经转移到第一部分。

---
## 用户原始需求记录
<use-request>
    read @reports/stories/compare-j-openclaw-20260728T000544-20260728T001259-8b175da9-vs-j-lobster-20260728T000549-20260728T002156-d6b04665.md
    我们需要对这份对比报告做以下优化:
    - 首先开篇对应的两个Journey的报告，我们需要挂上链接。而且它的初始化的这个Message的完整内容是要展示出来的，不包括系统提示词，只包括User message部分。然后呢，再把链接挂出来，就是每一个A和B对应的它的Journey Report的链接。这样我们想看Detail的时候可以直接跳过去。
    - 在这个报告的证据溯源里头,它引用了很多完全无关的日志文件，这个就很奇怪。我们在跑 'vmr story'指令的时候是会生成 vmr-stories.json 文件, 这份文件里头就已经说明了每一个Story，每一个Journey，它所关联的日志文件究竟有哪一些。我们应该基于这个去找到相关的日志文件，然后去进行分析，而不是把所有的文件都跑一遍。从逻辑上不应该这样做，最后的Report里更不应该把它们作为数据来源文件全部列出来。这块需要你深入去分析一下他们所有的源码逻辑，应该是充分利用 vmr-stories.json 作为Index文件的价值。
    - 此外，在证据溯源后面有两段LLM解读，这个会有点让人感觉好像重复了，能不能够用一个大的章节把它们扩起来？比如说把LLM解读这一部分作为一个大的章节，然后把下边两段解读呢，它们分别针对的场景作为两个子章节; 或者换一种思路，就是两个LLM解读还作为大章节，但是把后面的根因、候选根因呀、工作方式呀、VMR看不到什么呀等等，这些交叉点解读啊，这些作为它们LLM解读下的子章节。当然我不知道这个是不是这样合理啊，你来评估一下，选择最合理的方案。总之是让它在目录结构上看起来更清晰、更容易理解。
    
    read @reports/stories/journey-j-openclaw-20260728T000544-20260728T001259-8b175da9.md
    我们需要对这份报告做以下优化:
    - 决策脊柱：每一个step它所对应的原始的这个消息是要写完整的，现在都是被截断。例如 Step1、2
    - 系统提示词应该放在头部，因为每一个请求都会带它，所以我们并不需要在每个请求里头去重复每一步去重复。但是在整个项目的开头要把它列出来，并且自动折叠，因为它可能很长。包括它自动附带的Tools等等，就是这些系统上下文的内容，要先在头部做一下罗列，但默认都是自动折叠的。
    - 决策脊柱：在每一步当中，它的工具调用为例，要把它的结果展示出来。这个结果展示呢，有两种方式哈，你看哪一种方便。第一种呢，就是像现在一样，它三个，比如说调了三个工具，还是两个工具。他调这个两个工具之后呢，这是他大语言模型反馈回来说我要用这两个工具。然后呢，你可以再补一段叫做工具调用的结果。那这个应该是从下一轮的请求里头能够看到，然后把这个结果呢直接贴进去。当然如果你能做的更精细，可以把调用工具的结果对应到它每一个工具调用上，就是它如果调了三个工具，那么每一个工具调用它的结果拆开去给它们分别对应上。当然，如果这个拆开不好拆，就是它可能涉及到文本的解析啊什么的，搞得会比较复杂的话，那我们也可以考虑用一大块去对应整个，三个一组或者两个一组，那整个它所有的工具调用，我们把所有的结果一次性呈现出来，这样的话简化我们对工具结果的这个展示的这个这个处理。这个工具调用结果呢，默认也是折叠的，就是因为它的内容可能会很长。
    - 决策脊柱：有一些工具调用，像比如说Exec Command等等，它这种多行的，如果识别出来的话，也是默认折叠，只在冒号后面，把第一行或者是前多少个字符显示出来。注意，这显示出来的部分也是要替换掉换行的。现在我看到的情况是，如果只有一行，那它就直接显示出来了。如果是多行的话，它会在下边显示一个block。那这个block呢，我觉得是默认是折叠。然后呢，在冒号后面呢，哪怕是多行，也会固定的把第一行的前多少个字符显示出来。这样的话，我们就既能够直观的看到这个命令是什么，又能够在想要看全部的时候，从那个折叠的部分展开去看，又不会特别的占用这个纵向的空间。具体可以看一下我给你这个示例报告的 Step4-6
    - 现在在展示完决策脊柱之后。还会继续列T01、T02。例如：
        ```
        ## t01 · [Tue 2026-07-28 00:05 GMT+8] [message_id: om_x100b694c53b4eca8b1cd50932b7aefe] o…
        ## t02 · (缝合自更早片段 · head_prune，覆盖率 33%)
        ```
        在我看来，这个可能不是很有必要，你来评估一下。我认为可以有一个更简单的办法，就是在决策脊柱里头，把detail report的链接挂进来就可以了。当我们想去看到像后期这些每一个任务的detail的时候，我们完全可以跳转到对应的请求链接，这个更完整。而且大部分时间其实我们还是看脊柱为主，只有少量的时间才需要看到这些detail。所以说我们完全不必要再重新把它输出一遍。但这是我的看法，你来评估一下这个逻辑合理性。如果逻辑合理的话，我们就按这个思路去处理：把每一步对应的detail report，就是LLM请求的那个原始的detail report挂到角色脊柱里头每一步的描述里，然后通过链接链到detail目录下的对应的文件。当然这也要求我们去生成journey reports的时候，一定是要把对应的detail report生成出来。如果它还没有生成出来的话，我们需要以某种方式自动替它生成出来。
    
    以上呢是多个任务的一个综合，我希望你呢，首先要去理解我们这个需求是什么，然后结合给定的参考的样例报告，也找到相关的源码去做第一性原理去深入做分析。注意所有的分析呢，一定要基于源码，基于报告的原始内容，避免凭印象主观臆断，也避免盲信提示词/用户指令/文档等声称的内容。而且判断的时候呢，你不光是要去判断怎么做，还要判断他的这套建议、他的思路、他的方案是不是合理，有没有更好的方案，甚至于说这个建议根本就不值得做，你要有自己的主见。
    
    把你的分析过程和结论写到一个独立的Markdown当中，然后作为我们分析阶段的一个关键记录。然后其中呢，对于结论清晰，处理起来也比较无风险的，你就直接处理掉。对于那些方案存疑，或者是建议暂时搁置等等，就这些相对来说不适合马上处理的，那么你在这份独立的Markdown文档当中去进行说明，然后列出后选项啊，或者是给出进一步的建议啊，whatever，就是在这里头说明,等我阅读并且确认方案之后，再进一步看如何处理。不要在过程当中问我，因为我马上要离开一段时间。有任何问题都留到文档当中，我会回来统一处理。
    
    Debrief一下任务要求，然后看看有没有什么需要马上澄清的，如果没有的话就开始执行。
</user-request>
---

## 第零部分：一个关键的前提澄清

你给的两份样例报告，`compare-*.md` 的文件 mtime 是 8 月 17 日，而 `git log` 显示
`34d2840 feat(story): auto-generate missing journey reports on compare and link them in
comparison report` 这个 commit 同样是 8 月 17 日提交、且就发生在你给的两份报告所在的最近
5 个 commit 之内。也就是说：**样例报告很可能是用一个比当前代码略旧的构建生成的**。

我用当前代码重新构建了 `vmr` 二进制（未提交，只是本地验证用），对着你日志目录里这两个
Journey 真实的审计数据重新跑了一次 `vmr story -compare`，发现：

- 开篇的 Journey 链接（你的诉求 1 的前半部分）**在当前代码里已经生效**，样例报告里没有
  链接只是因为它是旧构建生成的。
- 但"证据溯源"列出无关文件的问题（诉求 2）**在当前代码里依然存在**——这是一个真实、
  当前仍未修的 bug，和构建新旧无关。

这也是为什么下面第一部分里"链接"这一条我标成"已经是现状，不需要动"，而"证据溯源"这一条
我改了代码。**请不要因为样例报告里没链接就怀疑这条没做**——用真实数据重新生成过，链接
确实在。

---

## 第一部分：已直接处理

### 1. Compare 报告：证据溯源改为按 Journey 精确定位（原诉求 2）

**问题确认**：`cmd/vmr/cmd_story.go` 的 `-compare` 分支把 `resolveInputPaths` 解析出的
**全部**输入文件（`paths`，也就是本次 `vmr story` 命令扫描的所有审计日志）直接透传给
`compareJourneys` 当作 `extras.Sources`（旧代码第 161 行 `compareJourneys(..., paths,
...)`，第 453 行 `extras.Sources = sources`）。这跟这两个 Journey 实际用到哪些文件完全
无关——只要你是拿整个 `log_dir` 跑的 `vmr story`，"证据溯源"就会把当天全部审计文件都列
出来，哪怕这个 Journey 只涉及其中一两个。用真实日志验证：旧逻辑列出 10 个文件
（07-20 到 07-29 全部日志），而这两个 Journey 实际只用到 `vmr-audit-2026-07-28.jsonl.zst`
一个文件。

**你的诊断是对的**：`vmr-stories.json`（`internal/story/storyindex.go`）里的
`JourneyIndexRow.Files` 字段本来就已经精确算好了"这个 Journey 依赖哪些文件"（每个
候选 Journey 在 `BuildJourneyIndexRow` 里通过遍历它 stitch 链上所有 manifest 的
`.Path` 去重后得到），`-compare` 流程里 `idx` 这个 `*story.StoryIndex` 本来就已经作为
参数传进了 `compareJourneys`，只是没被用来算 Sources——是一个"数据已经算好但没接上"的
连线问题，不是需要新写扫描逻辑。

**改动**：
- `internal/story/storyindex.go` 新增 `SourceFiles(idx *StoryIndex, ids ...string)
  []string`：按 Journey id 从 `idx.Journeys[].Files` 里取并集、去重、排序（用于两个
  Journey 共享同一个 stitch 边界文件的情况）。
- `cmd/vmr/cmd_story.go`：`compareJourneys` 不再接收全局 `sources []string` 参数，
  改为在拿到 `jA.ID`/`jB.ID` 后调用 `story.SourceFiles(idx, jA.ID, jB.ID)`。

**验证**：真实日志重新生成后，"证据溯源"只列出 1 个文件（这两个 Journey 实际所在的
`vmr-audit-2026-07-28.jsonl.zst`），不再有其余 9 个无关文件。`go test ./...` 全绿。

### 2. Compare 报告：LLM 解读改为大章节 + 子章节（原诉求 3）

**先纠正一个源头误解**：这两段"LLM 解读"下面的 "候选根因" / "工作方式与阶段解读" /
"VMR 看不到什么" / "分叉点解读" 这些二级标题（`## `），**不是 Go 代码拼出来的，是我们
在 system prompt 里指示 LLM 自己按这个 Markdown 结构输出的**（见
`internal/i18n/story_llm.go` 的 `llmSystemPromptZH`/`llmDivergenceSystemPromptZH` 等常量，
规则 6/5 明确写了"然后依次是"## 候选根因"...三个小节""）。`RenderLLMSection`
（`internal/story/llm.go:403`）本身只是把 LLM 返回的原文整段拼接在 `## LLM 解读（...）`
标题后面，不做任何二次改写。所以要调整标题层级，动的是发给 LLM 的 prompt 文本，不是
渲染代码。

**方案选择**：你提的两个方案里，"两段解读各自保留为 `##`，内部子话题降一级为 `###`"
改动面明显更小——不需要碰 `RenderLLMSection`/`SectionTitle`（那个函数本来就已经在给
"整体对比"和"分叉点"两段生成两个语义清晰、互不冲突的 `##` 标题，问题只出在**每段内部**
的三个子标题跟外层标题同级，看起来像是四五个平级章节），只需要把 prompt 里让 LLM 输出
的 `## 候选根因` 等改成 `### 候选根因`。而"整段包一个新的父级 `## LLM 解读`"那个方案需要
额外改 `SectionTitle`/`RenderLLMSection` 的标题生成逻辑，改动面更大却没有解决更多问题——
两段解读本来就是独立的两次 LLM 调用（整体对比 evidence pack 和分叉点 evidence pack
内容完全不同，`compareLLMSections` 里是两次独立的 `story.Interpret` 调用，没有可以合并
的理由，也不建议合并：分叉点场景的证据包是精确围绕分叉点前后几步的小窗口，整体对比的
证据包是全量 Metrics+两段节选，语义不同，强行合并只会让 prompt 更啰嗦），所以没有必要
为了"看起来像一个大章节"而改变调用结构。

**改动**：只改了 `internal/i18n/story_llm.go` 里四段 prompt（中英文各两段：compare 的
"整体对比"和"分叉点"），把规则里要求 LLM 输出的 `## 候选根因`/`## 工作方式与阶段解读`/
`## VMR 看不到什么`/`## 分叉点解读` 统一改成 `### ...`，并在规则里补充了"因为它们是
LLM 解读这个大章节下的子章节"的说明，让 LLM 更容易理解为什么要用三级标题（而不是机械
替换）。**没有改** `-journey`（单 Journey 复盘）用的 `llmSingleJourneySystemPromptZH/EN`
——那边只有一段 LLM 解读，不存在"两段解读并列显得混乱"的问题，维持 `##`。

**风险**：这是纯粹的 prompt 文本改动，不改变任何 Go 结构；唯一的风险是 LLM 没有严格
遵守新指令、偶尔仍输出 `##`——考虑到现有 prompt 已经对表格列名、置信度取值这类更细节的
格式都要求得很严格且实测遵守良好，这个风险可以接受，且后果只是"某次报告里标题层级没对齐"
这种纯格式瑕疵，不影响内容正确性。搜索了 `internal/story/*_test.go`，没有测试断言这几个
中文标题字符串，不会破坏现有测试。

**验证**：用真实日志重新生成 compare 报告，两段解读下的标题层级已经变成
`## LLM 解读（模型：cheap · 整体对比）` → `### 候选根因` / `### 工作方式与阶段解读` /
`### VMR 看不到什么`，`## LLM 解读（模型：cheap · 分叉点）` → `### 分叉点解读` /
`### VMR 看不到什么`。目录结构清晰多了。

### 3. Journey 报告：决策脊柱里多行/超长工具调用参数默认折叠（原诉求 3，你举的 Step 4-6 例子）

**问题确认**：`internal/story/render_spine_args.go` 的 `payloadBlock` 此前的逻辑是：
单行且不超过 120 字符 → 内联 `key: value`；否则（含换行，或单行但超长）→ 直接展开一个
` ``` ` 围栏代码块，**从不折叠**。跟你的观察完全一致："单行直接显示，多行就是一个默认
展开的 block"。

**方案**：改成折叠块，`<details><summary>` 里放一段"拉平"（换行/多余空白折成单个空格
后截断）的预览文本，展开后是原来那个完整的、带截断上限保护（`spineFullCap`=3000 字符）
的围栏代码块。有一个和你描述略有出入的实现选择，写在这里供你确认：**预览文本不是严格
"第一行的前 N 个字符"，而是"整段内容拉平换行之后的前 160 个字符"**。原因是很多 exec
调用的第一行本身没有信息量（比如 heredoc 的 `python3 << 'PYEOF'`，或者只是一行注释），
严格截第一行经常比拉平预览更没用；而 Step 1-3 里那种单行长 URL 的场景，两种做法结果
完全一样。如果你更想要"严格第一行"的语义，这是一行代码的调整（`oneLineTruncate` 换成
只切 `strings.SplitN(val, "\n", 2)[0]` 再截断），我可以随时改。

**改动**：`internal/story/render_spine_args.go` 的 `payloadBlock` + 新增
`spinePreviewLen` 常量；同步更新了 `render_spine_args_test.go` 里断言旧格式的一个用例。

**验证**：真实日志重新生成后，Step 5/6 的 exec 命令现在是：
```
🔧 `exec` `command`: <details><summary>curl -s --proxy http://127.0.0.1:7890 'https://datacenter-web.eastmoney.com/api/data/v1/get?reportName=RPT_IPO_ISSUEAMOUNT&columns=ALL&pageNumber=1&pageSize=500…</summary>

```
（完整命令原文）
```
</details>
```
默认折叠，纵向空间不再被多行命令占满，展开后内容跟以前一样完整（`spineFullCap` 保护逻辑
未变）。`go test ./internal/story/... ./internal/archtest/...` 全绿（改动后
`render_spine_args.go` 精确卡在 archtest 197/200 行预算内）。

### 4. Journey 报告：决策脊柱 Step 的原始消息不再截断（原诉求 1）

**第一轮判断的错误之处**：第一轮分析把这条和"system prompt 位置"（原诉求 2）绑在一起，
理由是"脊柱截断多少字符，取决于下面 fact-layer 还留不留一份完整备份"，因而整条搁置为
"留待你确认"。**这个理由站不住**：第 3 条（工具调用参数折叠）已经证明了"折叠而不是截断"
这个模式——`payloadBlock` 对超长/多行内容不是截断丢弃，而是默认折叠、展开即完整。把同一
个模式原样搬到 `spineWhyLine`（Step 的 `RespText`/`Reasoning`），"完整" 和 "脊柱不膨胀"
这两个目标根本不冲突，不需要先决定 system prompt 或 fact-layer 的去留。

**改动**：`internal/story/render_spine.go` 的 `spineWhyLine` + `oneLineTruncate` 移到新文件
`internal/story/render_spine_step.go`（连同 `renderDecisionSpine`/`renderSpineStep`，纯粹
是给第 6 条腾行数预算，见下）；原来"超过 400/200 字符直接截断"的逻辑改成新的
`foldWhyLine`——文本在阈值内保持内联（拉平成一行，跟原来视觉效果一致），超过阈值不再丢
尾部，而是折叠展示（`<details><summary>` 拉平预览 + 完整原文）。`spineWhyRespCap`/
`spineWhyReasoningCap` 两个常量保留，语义从"截断长度"变成"内联/折叠的分界线"。

**验证**：Step 1 那段被截断成"...Let me break down the request: 1. **Data Collection**:
Gather all A-share IPO data from the…"的推理文本，现在完整可见（点开折叠块）：

```
> 🤔 <details><summary>The user wants me to conduct a comprehensive investigation into
A-share IPO (打新) returns over the past year. Let me break down the request: 1. **Data
Collection**: Gather all A-share IPO data from the…</summary>

```
（完整推理原文，四段）
```
</details>
```

`go test ./internal/story/... ./internal/archtest/...` 全绿。

### 5. Journey 报告：system prompt 移到文档头部、折叠一次（原诉求 2）

**第一轮判断的错误之处**：第一轮分析发现"system prompt 不是每步重复，只在 Step 1 出现
一次"，这个事实性纠正是对的；但由此推出"要不要动这个位置，取决于第 4 条脊柱截断怎么定"
是不必要的谨慎——把 system prompt 挪到头部、Step 1 不再重复渲染它，跟脊柱截断长度是两件
完全independent 的事：前者是"这段内容该出现在文档的什么位置"，后者是"某个字段该展示多
少"，没有共享的前置决策点。

**改动**：新文件 `internal/story/render_md_sysprompt.go`：
- `systemPromptEras(j *Journey) []systemPromptEra`：走一遍 `journeySteps(j)`，按
  `NewEvents` 里 `Role=="system"` 的消息首次出现的位置分段——不是猜的，是复用
  `ctxgraph`/`journey.go` 本来就有的"消息级去重"（同一份 system prompt 内容只在第一次
  出现时进 `NewEvents`），一个 Journey 全程只切换过一次系统提示词就是一段，切换过 N 次
  就是 N 段，各自标注生效的 Step 区间。
- `renderSystemPromptHeader`：在 `RenderMarkdown` 最开头（Journey 元信息之后、总览卡片
  之前）渲染一个"## System Prompt"区块，每段一个折叠块，默认折叠。
- `render_md.go` 的 `renderStep`：Step 的 `**Messages**` 区块过滤掉 `Role=="system"` 的
  `NewEvents`（已经在头部展示过，不再重复）。

**代价确认**：这个改动只影响 Markdown 渲染（`RenderMarkdown`），不改变
`journey-<id>.json`（`JourneySummary`，只含 `ID/Title/From/To/Partial/Metrics/
Findings/LLMFindings`，不含逐消息内容）——不涉及 JSON 契约，风险比预想的更低。

**验证**：`TestGoldenMarkdown` 的黄金基线跑出的 diff 正是预期效果——system prompt 从
Step 1 的 Messages 区块消失，改在文档开头出现一次：
```diff
+## System Prompt
+
+<details><summary>Step 1–3 · 28 chars</summary>
+```
+You are a helpful assistant.
+```
+</details>
+
 ## Overview
 ...
-<details><summary>▸ system · You are a helpful assistant.</summary>
-...
-</details>
```
已用 `UPDATE_GOLDEN=1` 重新生成并肉眼审过 diff（EN/ZH 两份都符合预期）后提交。用真实
openclaw/lobster 两条 Journey 重新生成也确认：多版本 system prompt 会按顺序分段列出
（这两条样例都只有 1 段，全程未变）。`go test ./...` 全仓库全绿。

### 6. Journey 报告：决策脊柱补充工具调用结果（原诉求 3）

**第一轮判断的错误之处**：第一轮分析以"需要先验证 `tool_call_id` 配对可靠性"为由把这条
定性为"工作量最大、需要先做探针验证"而搁置。**这个顾虑是对的方向，但结论下早了**——
`findings_toolresult.go` 的 `toolResultsFor` 已经是这次验证需要的探针本身：它不是要不要
写的新代码，是 Phase 2 Finding 检测器已经在用、`go test` 里已有覆盖的现成函数。直接接上
渲染层，用真实日志跑一遍就是最快的验证方式，不需要另起一次"验证性调查"再决定要不要做。

**改动**：新文件 `internal/story/render_spine_step.go`（同时把 `renderDecisionSpine`/
`renderSpineStep`/`spineWhyLine` 从 `render_spine.go` 移过来，纯粹是给 archtest 行数
预算腾地方——`render_spine.go` 改动前 379/380 行，两个新函数塞不进去）：
- `toolResultLine`：把一个 `chatmsg.ToolResult` 按 `payloadBlock` 同款折叠约定渲染
  （`↩️`/`❌` 前缀 + 拉平预览 + 完整原文，复用现成的 `capFull`/`oneLineTruncate`/
  `spinePreviewLen`），不是新发明一套格式。
- `renderDecisionSpine`：为每个 Step 计算它在 `journeySteps(j)` 里的扁平下标，调用
  `toolResultsFor(steps, idx)` 拿到这个 Step 的工具调用在下一个 Step 里被回答的结果。
- `renderSpineStep`：按 `ToolCall.ID` 把结果关联回具体的那次调用（不是整批堆在一起），
  你诉求里"更精细"的那个选项——`toolResultsFor` 本来就是按 ID 精确配对，拆分并不难。

**验证时的意外发现（重要，直接影响这条改动的实际价值）**：用真实日志重新生成两条样例
Journey 后，**openclaw（33 次工具调用）和 lobster（64 次工具调用）的配对成功率都是
0%**。逐字节核对 `~/code/vmr/logs/vmr-audit-2026-07-28.jsonl.zst` 原始 JSON 后确认根因：
这条 `openai:opencode:deepseek-v4-pro` 链路里，模型在响应流里给出的 tool_call id（例如
`call_00_xHodGBOXHzFfUnFh4mZR8555`，带下划线）到了下一轮请求的 `tool_call_id`/
`tool_calls[].id` 里变成了 `call00xHodGBOXHzFfUnFh4mZR8555`（下划线消失）——两次 ID 不是
同一个字符串。用 `zstdcat | grep` 直接核对原始字节确认这不是 VMR 解析出的偏差（`chatmsg.
ReassembleSSE` 对 tool_call id 是逐字段赋值，没有任何拼接/改写逻辑），而是 `opencode`
这个中间代理/网关在转发过程中把 ID 改写了——一个 VMR 管不到、但会让"按 ID 精确配对"这个
本该由协议保证成立的假设失效的上游行为。详细结论、影响面、和一个可选的位置兜底
方案，已登记为 `docs/KNOWN_ISSUES_sonnet-5.md` §1.21——这条我判断**不该现在顺手加兜底
逻辑**，因为它已经不是"读取事实"，而是在协议保证之外引入一条推断规则，需要你先认可
"要不要接受这种推断"、以及"配对结果要不要标注来源是 ID 精确匹配还是位置推断"。

**结论**：功能本身实现正确、防御性好（配不上号时干净地不渲染，不会猜配/配错），对不
经过这类 ID 改写代理的 provider/client 组合应该是有效的；但对你实际给的这两份样例数据
来说，目前视觉上不会有变化（因为配对结果是空的）。`go test ./internal/story/...
./internal/archtest/...` 全绿。

---

## 第二部分：留待你确认的事项

以下几条我判断"方案本身需要你做取舍"或者"跟现有架构的一条明确边界冲突"，所以没有直接
动代码，只把分析和推荐方案写清楚。

### 7. Compare 报告开篇：完整展示两侧的初始 User Message（原诉求 1 后半部分）

**现状**：`SideBlock` 引用块里展示的是 `JourneyRef.Title`，来自
`taskseg.Preview()`——**故意**限制在 80 个 rune 以内（`internal/taskseg/segment.go:177`
`previewLen = 80`）。这个字段同时被用在候选 Journey 列表（`vmr story` 不带参数时的那张
表）、`vmr-stories.md`、Finding 描述等很多"一行摘要"场景，不能直接放大这个字段的长度
上限，否则那些表格场景会被拉爆。

**可行性**：完整的首条 User 消息文本其实已经在内存里，不需要新的解析逻辑——
`Step.NewEvents[].Msg.Text`（`chatmsg.Message`，`internal/story/journey.go:76-99` 的
`Step`/`Event` 结构）在渲染时是完整未截断的原文（`RenderMarkdown` 的 Messages 一节就是
直接展开这个字段），只是从来没有作为一个独立字段暴露给 `JourneySummary`/`JourneyRef`。
从 `j.Tasks[0].Steps[0].NewEvents` 里取第一条 `Role=="user"` 的 `Msg.Text`，就是"不含
系统提示词、只含 User message"的完整首条消息。

**为什么没有直接做**：这不是"数据拿不到"的问题，而是三个需要你拍板的设计选择，
每一个都会实际影响这个改动的形状：

1. **要不要设上限**：你说"完整内容都要展示"，但语料库里已经出现过反例——
   `j-nokey-20260729T232905-...` 这条 Journey 的首条消息就是一段贴进来的
   **完整 JSON 对比数据**（几千字符）。如果完全不设上限，遇到这种输入 compare 报告
   会被撑得很大。我倾向于沿用这份报告里 system prompt / 最终交付物节选已经用的同一套
   "有界 + 折叠 + 截断时明确标注"惯例（`compare.go` 的 `sysPromptExcerptChars`/
   `deliverableExcerptChars`，`render_compare.go` 的 `renderExcerpt`），而不是新开一种
   "真正无界"的展示方式——但这意味着"完整"其实是"在一个慷慨但依然有限的字符数以内
   完整"，跟你说的字面意思有出入，需要你认可这个折衷。
2. **放在哪、什么样式**：是替换掉现在 `SideBlock` 里那句被截断的引用块（`> ...`），
   还是保留那句短摘要（继续起"一眼概览"的作用）、在下面新加一个折叠的
   "A 的完整初始消息"区块（跟 system prompt 节选一个风格）？我倾向于后者——短摘要负责
   "扫一眼是什么任务"，折叠块负责"要看全文时展开"，两者不冲突；但这是我的偏好，不是
   唯一合理答案。
3. **JSON 契约要不要跟着加字段**：`JourneyRef`（`compare.go:166-172`）会序列化进
   `compare-*.json`，加一个 `FirstUserMessage`/类似字段是一次新增字段（`omitempty`，
   向后兼容），但这个 JSON 是 `internal/story/llm.go` 的 evidence pack 也会引用的
   "契约"的一部分（design doc 提到的"JSON 契约"小节），加字段前建议你确认一下是否
   希望这份摘要证据也让 LLM 解读层看到（如果看到，等于给 LLM 解读又多喂了一段原文，
   多少会增加 evidence pack 体积和调用成本；如果不想给 LLM 看，就只在 Markdown
   渲染层取数据，不进 JSON 结构体）。

**我的推荐**（如果你认可，下次可以直接照这个做）：新增
`FirstUserMessageExcerpt(j *Journey) (text string, truncated bool)`（`journey.go`），
复用 `deliverableExcerptChars`（6000 字符）级别的上限；在 `compareJourneys`
（cmd_story.go）里跟设置 `extras.Sources`一样，在拿到 `jA`/`jB` 后直接赋值到
`cmp.A`/`cmp.B` 的新字段；`render_compare.go` 的 `renderSysPrompt`/`renderDeliverable`
旁边加一个 `renderFirstMessage`，复用现成的 `renderExcerpt` 折叠样式；`SideBlock` 本身
不动，短摘要继续用现在的 `Title`。这样改动集中在 3 个文件、可以复用三处已有的折叠约定，
风险可控。**但没有你对上面三点的确认我不会先动手**——尤其是第 3 点，直接影响每次
`-compare -llm-addr` 调用的 token 成本，这个不该我替你决定。

### 8. Journey 报告：去掉 `## t01`/`### Step N` fact-layer，脊柱改为链接到 detail report（原诉求 4）

**你的判断本身是对的**——决策脊柱（`## 决策脊柱`）和后面的 `## t01 · ...` /
`### 🔷 Step N ...` fact-layer 确实是同一批底层数据的两种展示：前者是摘要，后者是
带完整消息体的详情。摘要之后不需要在同一份文档里再完整摊开一遍详情，只在需要时点开
更合适。

**但你提议的具体实现路径撞上了这个项目一条明确的、`archtest` 强制检查的架构边界**：
`internal/story` 和 `internal/report` 被显式禁止互相 import
（`internal/archtest/import_boundaries_test.go` 第 64-67 行：`"vmr/internal/story"` 的
禁止列表里明确列了 `"vmr/internal/report"`）。你设想的"每一步链接到 per-request 的
detail report，如果还没生成就自动生成"里的"detail report"，指的是 `vmr report`
（不是 `vmr story`）生成的 `reports/details/*.md`（`internal/report/detail.go`）——
**要在 `internal/story` 内部调用 report 的渲染函数去"自动生成缺失的 detail report"，
必须 import `internal/report`，这会直接把 `go test ./internal/archtest/...` 跑挂**。
这不是我不愿意做，是这条路径在当前架构下**编译不过**（archtest 会在 CI 拦下来）。

进一步说，就算绕开 import 边界（比如把生成逻辑挪到 `cmd/vmr` 这个"唯一允许同时看到两个
半区"的组合根去做），还有一个更麻烦的事实性障碍：`detail.go` 里给每个 detail 文件命名
用的是 `detailFileName(rec *audit.Record, used map[string]int)`
（`internal/report/detail.go:354`），文件名格式是
`{时间戳}_{virtual}_{real}_{outcome}.md`，其中 `used` 这个去重计数器是**跨整批记录**
累积状态（同一毫秒时间戳出现碰撞时靠这个计数器加后缀区分）——也就是说，`story` 侧
即使拿到了同一条 `audit.Record`，也没办法在不重新走一遍 `report` 那一整批记录的处理
流程的情况下，**独立、确定性地推算出**这条记录对应的 detail 文件到底叫什么名字。这
不是"多写一个函数"就能解决的，需要 report 和 story 之间要么新增一份两边都认的稳定 ID
（比如给每条 audit record 一个内容寻址的 hash 当文件名的一部分，替代现在依赖批次顺序的
计数器），要么接受"链接可能失效"的风险。

**推荐的现实路径**（如果你认可这个方向，值得作为一个独立的小任务去做，但不是这次能
顺手做的事）：

1. **不追求"自动生成"，只做"如果存在就链接"**：`story` 侧不调用 `report` 的任何代码，
   只是在渲染时按约定的相对路径（`../details/...`）拼一个链接；`report`/`story`
   两边约定分开跑，用户想要详情链接可用，就先跑一次 `vmr report`。这条路径不违反
   import 边界（`story` 不需要知道 `report` 的任何类型，只是拼一个字符串路径），改动
   集中在 `render_md.go`（渲染时按 Step 的时间戳/model/outcome 拼测的文件名）+ 一句
   文档说明"这个链接的前提是你也跑过 `vmr report`"。**唯一的硬约束**是上面提到的文件
   命名依赖批次去重计数器的问题——如果这批记录里同一毫秒出现过多次碰撞，`story` 侧拼出
   来的文件名可能猜不准，需要先把 `detailFileName` 的命名规则改成不依赖运行时状态的
   确定性方案（比如内容 hash 前缀），这是这条路径里唯一必须先做的地基工作。
2. **决策脊柱的每个 Step 标题旁加这个链接**（如果文件存在的话），`## t01`/
   `### Step N` fact-layer 整段可以删除或收缩成一句"完整请求见 <链接>"——这一步就是
   纯粹的 `render_md.go` 改动，不涉及跨包问题，只是依赖上面第 1 点先落地。

**这条我完全没有动手**，因为它既涉及架构边界（需要先决定要不要为此改
`report/detail.go` 的命名机制），也涉及"story/report 到底要不要在产出物层面耦合"这个
比这次 7 条改动本身更大的产品决策，超出"这次顺手改掉"的范围。

---

## 验证方法（如何得出以上结论，供你复核）

1. 通读了 `internal/story/`（`compare.go`/`render_compare.go`/`render_spine.go`/
   `render_spine_args.go`/`render_md.go`/`journey.go`/`storyindex.go`/`llm.go`）、
   `internal/i18n/story_*.go`、`cmd/vmr/cmd_story.go`，逐条对照报告实际内容找到生成
   它的确切代码位置，不凭包名/函数名猜测。
2. 用 `git log` 核对了样例报告文件的 mtime 和最近改动 `vmr story` 的 commit
   （`34d2840`）的时间关系，确认样例报告可能是旧构建产物。
3. **用真实审计日志重新验证**（两轮都做了，第二轮额外补了单 Journey `-journey` 渲染
   核对脊柱/system prompt/工具结果三处新改动）：`go build -o ./vmr ./cmd/vmr` 构建当前
   代码的二进制，对 `~/code/vmr/logs/vmr-audit-2026-07-28.jsonl.zst` 重新跑了
   `vmr story -compare j-openclaw-...,j-lobster-...` 和分别对两条 Journey 各自的
   `vmr story -journey <id>`，肉眼核对了"证据溯源"/"LLM 解读"标题层级/决策脊柱 exec 折叠
   /决策脊柱完整消息/system prompt 头部折叠/工具调用结果配对，一共六处输出。（这两轮
   命令行都真实调用了 `192.168.0.22:8800` 上一个正在跑的 VMR 实例的 `cheap` 模型来生成
   LLM 解读部分，产生了真实的 LLM API 调用/token 消耗，是本次验证带来的一个副作用，
   供你知悉。）
4. 每一处"已直接处理"的改动都跑过 `go build ./...`、`go vet ./...`、
   `go test ./...`（全仓库，不只是改到的包，含 `TestGoldenMarkdown` 用
   `UPDATE_GOLDEN=1` 重新生成并肉眼审过 diff）、`gofmt -l .`，全部通过。
5. 每一处"留待确认"的结论，都用 `grep`/`Read` 定位到了具体文件和行号作为依据（正文里
   已经标注），没有一条是纯粹根据报告内容或用户描述反推的猜测。
6. 第 6 条（工具调用结果）验证时发现的 `tool_call_id` 被上游代理改写的问题，是逐字节
   `zstdcat | grep` 核对原始审计日志 JSON 后确认的，不是从渲染出的 Markdown 反推的猜测。

## 改动文件清单（已直接处理部分）

- `internal/story/storyindex.go`：新增 `SourceFiles`
- `cmd/vmr/cmd_story.go`：`compareJourneys` 签名瘦身，`Sources` 改为按 Journey 精确定位
- `internal/i18n/story_llm.go`：4 段 prompt 的小节标题层级 `##` → `###`
- `internal/story/render_spine_args.go`：`payloadBlock` 改为折叠展示，新增
  `spinePreviewLen`
- `internal/story/render_spine_args_test.go`：同步更新一个断言旧格式的测试用例
- `internal/story/render_spine.go`：移出 `renderDecisionSpine`/`renderSpineStep`/
  `spineWhyLine`，只保留 `oneLineTruncate`（给行数预算腾地方）
- `internal/story/render_spine_step.go`（新文件）：`renderDecisionSpine`/
  `renderSpineStep`/`spineWhyLine`/新增 `foldWhyLine`/新增 `toolResultLine`
- `internal/story/render_md.go`：`renderStep` 的 Messages 区块过滤掉 system-role
  事件；调用新增的 `renderSystemPromptHeader`
- `internal/story/render_md_sysprompt.go`（新文件）：新增 `systemPromptEras`/
  `renderSystemPromptHeader`
- `internal/i18n/story_render.go`：新增 `SysPromptHeaderTitle`/
  `SysPromptHeaderChanged`/`SysPromptEraSummary`（中英文）
- `internal/story/testdata/golden.md`、`golden_zh.md`：随 system prompt 位置改动
  用 `UPDATE_GOLDEN=1` 重新生成
- `docs/KNOWN_ISSUES_sonnet-5.md`：移除已解决的旧 1.21/1.22 条目、新增 §1.21（工具调用
  结果 ID 配对问题）、§3 追加 5 条已闭环说明
