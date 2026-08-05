<!-- Ver 2026-08-05 04:00, by Sonnet 5 -->

# VMR Story/Journey 深度化 —— 开发计划与架构设计

> **范围**：本文档是 `vmr_story_journey_deepdive_sonnet-5.md`（下称"深挖报告"）的下游产物——把深挖报告里提到的每个增强方向落成一份可执行的开发计划：分档排期、架构设计、模块设计、具体改动点。
>
> **方法**：写这份计划前，直接读了 `internal/story`、`internal/report`、`internal/ctxgraph`、`internal/chatmsg`、`internal/archtest` 的真实源码（不只是设计文档的文字描述），确认了本文档里每一处"复用 XX 现有机制"或"XX 文件已接近行数预算"之类的判断都对应真实代码状态，不是从深挖报告转述后直接假设成立。
>
> **现状（2026-08-05）**：Phase 1 + Phase 2 均已按本文档的设计落地、测试通过、真实语料校准过——见下面"Phase 1 完成情况与复盘""Phase 2 完成情况与复盘"两节。Phase 3 建议暂缓，理由见"Phase 3 建议"一节。本文档 §0-§7 其余部分保留作为原始设计参考，不再代表"尚未开工"的状态。

---

## Phase 1 完成情况与复盘（2026-08-05）

**结论**：本文档 §3 设计的 Phase 1 全部条目已落地——`internal/story` 新增 `findings.go`/`render_spine.go`，`internal/i18n` 新增 `story_findings.go`/`story_spine.go`，`compare.go`/`metrics.go`/`render_md.go`/`cmd_story.go` 按设计改动，`go test ./... -race`/`go vet`/`gofmt -l`/`internal/archtest` 全部通过，golden fixture 已重新生成核对，`docs/VirtualModelRouter_Design_v4_Analytics.md` 已同步补充 §3.5a/§3.5b 与分叉点段落。**方案本身被验证可行，没有出现需要推翻架构设计的意外**——§2 的三条基础设施先行判断、§2.3.1 发现的 `RenderMarkdown` 签名协调点、§2.4 的文件预算前注册，实际开工后全部按原计划工作。

**与原计划的一处排期调整**：I1（`chatmsg.ToolResultList`）原定与 I0/I2 一起在 Phase 1 做（本文档 §0 的理由是"现在加零风险、以后加更贵"）。开工前复核发现 Phase 1 的 5.1/5.2/5.4/5.5/5.8 五个检测器和 4a/4b/4c/4e 呈现层没有一项实际消费 I1，只有 Phase 2 的 5.3/5.6/5.7 需要它——提前做违反用户既有的 YAGNI 工程纪律，遂将 I1 挪到 Phase 2（它第一次被真正消费的地方）。这不影响可行性判断，只是原计划对这一项的排期理由本身不够严格。

**校准发现的真实问题——§2.7 的纪律不是走过场**：五个 Finding 检测器提交前，在 `logs/vmr-audit-2026-07-1[4578]/2[0125-7].jsonl(.zst)`（约 130 个真实候选 Journey，覆盖 lobster/hermes/pi/openclaw 等多个真实 Agent 框架）上跑了一次性校准脚本（非产品代码），发现并修复了两个真实缺陷、收紧了一处适用范围：

1. **`reasoning_action_mismatch` 初版假阳性率约 90%**——扫描整段 Reasoning 全文、要求实体字符串完全相等，撞上两个真实模式：(a) 推理文本常态化叙述多步计划（"1. 查A 2. 查B 3. 查C"，当前 Step 只做了其中一步，其余被误判"没做"）；(b) 同一文件路径因正则词边界起点不同，在推理侧和参数侧被抽成不同字符串（`~/.hermes/SOUL.md` 抽出 `hermes/SOUL.md`，`/Users/x/.hermes/SOUL.md` 抽出 `Users/x/.hermes/SOUL.md`，字面不等但是同一个文件）。改成"只扫推理的最后一句 + 双向子串匹配"后，同一批语料上命中数从 24 降到 5，人工抽查全部可信。这正是深挖报告 §3 规则化边界表警告过的陷阱——"看起来像规则问题、实际是语义/鲁棒性问题"；本文档最初对这条的判断（"规则可判定、零 LLM 成本"）方向没错，但第一版规则的具体实现过于天真，这是对"怎么规则化"的修正，不是对"能否规则化"的推翻。
2. **`plan_execution_misalignment` 初版漏看同一 Step 内的执行**：只扫描 `Task.Steps[1:]`，而"计划宣告和第一步执行在同一个 Step 里"是常见情形（模型说完计划立刻发起第一个 `tool_call`），导致假阳性。修复为把宣告计划的 Step 自己的 `tool_call` 也纳入"后续引用"扫描范围。
3. **长编号列表退化成文档而非计划**：项数超过 8 项时，"全部未匹配"开始压倒性地成为主流命中模式——人工核查后发现这类样本几乎都是长篇战略文档/报告，不是可执行计划；加了 `maxPlanItems` 上限，超过就整体跳过，不再对文档误用"计划-执行"框架。

三处修复全部落成了 `findings_test.go` 里的具名回归测试，不是只留在这段记录里。

## Phase 2 重新评估（2026-08-05，Phase 1 完成后写的预判——见下方"完成情况与复盘"核对预判是否成立）

基于 Phase 1 的实际落地与校准经验，对本文档 §4 的 Phase 2 排期做以下调整——**不是推翻，是收窄和加固**：

- **I1（`ToolResultList`）保留在 Phase 2 开工首位，排期判断不变**：5.3/5.6/5.7 三项都直接依赖它。
- **5.3/5.6/5.7 开工前应预留"字符串匹配脆弱性"的缓冲**：Phase 1 的两个真实 bug（路径前缀不一致、多步计划 narration 污染整体扫描）都是"实体/字符串匹配在真实中文语料上比想象中脆弱"这同一类问题的不同表现。5.6（未被利用的工具结果）、5.7（上下文污染/幻象存活）都要做类似的实体匹配，开工前就该把"校准 + 至少一轮修正"算进工作量估算，而不是当作意外返工——本文档 §2.7 已经有这条纪律，Phase 1 用两个真实 bug 把它坐实了，不是新增判断。
- **7.1 语料级统计现在有更可信的输入**：Phase 1 校准后的 Finding 命中率已经是"人工抽查基本可信"的水平（不是最初约 90% 假阳性的版本），7.1/7.2（Finding 分组验证）可以按本文档原计划直接消费这批 Finding，不需要在开工前再单独验证一轮 Finding 本身的可靠性——Phase 1 的校准工作本身就是 7.1 隐含的前置条件，现在已经满足，7.1 的排期不需要因此顺延。
- **8a（约束文本存在性检查）的优先级不变，但校准要求更具体**：8a 复用 `ExtractEntities` 做"字符串对字符串"的存在性比较，Phase 1 已经证明这类比较在真实中文语料上容易被路径前缀差异、分句方式坑到；8a 落地前必须先跑一遍类似 §2.7 的校准，不能假定 `ExtractEntities` 的输出可以直接拿来做存在性判断而不踩同样的坑（本文档原表述已经要求校准，这里是把"要校准什么"说得更具体）。
- **"自我限定适用范围上界"值得成为 Phase 2 每个新检测器设计时的默认动作**：`maxPlanItems` 不是事后打的补丁，是一个可复用的模式——5.6/5.7/8a 开工时应该主动问一遍"这个信号在什么规模/长度下会退化成噪声"，把范围上界写进第一版设计，而不是等校准撞见了才加。
- **没有发现需要推翻 Phase 2 排期本身的证据**——本文档 §2 的架构判断（三项基础设施先行、`RenderMarkdown` 签名协调点、`archtest` 预算前注册）全部按计划工作，Phase 2 的模块设计（§4）不需要因为 Phase 1 的经验重新设计，只需要在开工节奏上预留校准迭代的时间。

## Phase 2 完成情况与复盘（2026-08-05）

**结论**：Phase 2 全部条目（I1 + 5.3/5.6/5.7/8a 四个检测器 + 5.9/6c LLM 解读层扩展 + 7.1/7.2 语料级统计）已落地、测试通过、真实语料校准过，并且额外用一个真实双 Agent 对比案例（`j-openclaw-...-8b175da9` vs `j-lobster-...-d6b04665`，同一个 A 股新股打新调研任务，`reports/compare-...json` 那份旧版基准报告分析过的同一对 Journey）做了端到端质量复核。上一节的预判基本命中——"5.6/5.7/8a 要预留字符串匹配脆弱性缓冲"这条判断被两次真实校准直接坐实，不是事后诸葛亮。

**与原计划的一处偏差，比预想的大**：dev plan §4.2 原计划"两条都不改动 `llm.go` 的核心调用链路"。实际开工后发现这个目标在 Go 里做不到字面意义上的"零改动"——`Interpret`/`cacheKey`/`buildUserPrompt` 原本硬编码只认 `EvidencePack` 一种类型，要让 5.9/6c 复用同一套缓存/HTTP/降级逻辑而不是各写一份，必须让这三个函数变成能接受多种证据包类型的形式。最终做法：新增一个 `evidencePackKind` 接口（`promptSpec(lang) promptSpec`，每种场景各自实现一次，返回自己的 system prompt/缓存版本号），三个核心函数改成 `[T evidencePackKind]` 泛型。这个改动的风险已经被验证过是可控的——`llm_test.go` 原有的 13 个测试（覆盖缓存命中、model 隔离、Bearer 认证、失败降级等）全部原样通过，`-compare` 现有调用点一行代码都没有改（Go 的类型推断补上了区别），不是"重构了核心链路但没测"。选择这条路而不是"三条路径各写一份 Interpret"（更贴近原计划字面意思，但会制造三份几乎相同、容易走偏的缓存/HTTP/降级逻辑），是因为已有的强测试覆盖把重构风险压到了足够低，重复代码的长期维护成本更值得避免。

**真实语料校准发现的问题（两轮，共四处）**：

1. `unused_tool_result`（5.6）初版按**单个实体**判定——一次工具结果里任意一个文件名/URL 后续没被再提到就算一条 Finding。在 137 个候选 Journey 的真实语料上，单个 Journey 最高命中 92 条，全语料合计 918 条——原因是目录列表/搜索结果类工具结果天然一次列出十几到几十个文件，Agent 按设计只会挑其中几个跟进，这是正常 triage，不是浪费。改成只在**整个结果的所有实体都没被再引用**时才触发后，命中数降到 297，人工抽查基本可信。
2. `plan_execution_misalignment`（5.8，Phase 1 已有检测器）在这轮用真实双 Agent 对比案例复核时暴露了 Phase 1 校准没测出来的第二个缺陷：一段推理如果先后出现两个独立的编号列表（先是"拆解一下这个请求"式的主题分解，再是"让我规划一下方法"式的真实执行计划），原实现把两段列表拼成一个 8 项"计划"，产出"6/8 项未匹配"这种虚高结论。改成只取**最后一段连续编号**（编号一旦不再递增——最典型是从 1 重新开始——就判定为进入了下一段列表）后，同一条真实推理正确识别出"3/4 项未匹配"，且这个数字现在有实际意义：Journey 只有 22 轮就结束了，确实没走完"分析规律→写指南→看未来新股"这几步。这条提醒了一件事：**Phase 1 的校准语料规模不够，不代表某个检测器已经"验证充分"**——`plan_execution_misalignment` 在 Phase 1 就已经跑过一轮校准、也修过一次 bug，仍然在 Phase 2 这轮用不同语料复核时暴露了新问题。
3. `error_retry_unadapted`（5.3）真实语料上 0 命中——与 `ErrorRecoveryCount`（九项指标）共享同一个已知限制：只认 Anthropic 协议的 `is_error` 字段，OpenAI 协议无对应标准字段，本批语料 OpenAI 协议占比高时这类检测器会系统性偏低估，是文档过的限制，不是新发现的 bug。
4. `constraint_text_dropped_at_compaction`（8a）真实语料上仅命中 1 次（137 个候选 Journey）——符合它自己的触发条件本来就稀有（只在缝合边界且确实有实体消失时触发），但样本量这么小，还远远谈不上"已验证误报率可接受"，这条直接影响下面的 Phase 3 判断。

四处修复/发现全部固化成了 `findings_test.go`/`findings_toolresult_test.go` 里的具名回归测试。

**真实案例质量复核的正向结果**（不只是找 bug，也确认了新增价值）：6a/6b 分叉点检测在 `j-openclaw-...-8b175da9` vs `j-lobster-...-d6b04665` 这对真实 Journey 上正确定位到第一步就是重度分叉——openclaw 先调用 `memory_search`+`read`（查已有上下文/记忆），lobster 直接 `web_fetch`（抓数据）——这是旧版报告（`reports/compare-...json`，人工/deepseek 逐项核对产出）完全没有覆盖的一层定位，旧版报告停留在"33 次 vs 64 次工具调用"这类聚合数字。语料级统计（7.1/7.2）在这批语料上也给出了第一个有数据支撑的行为模式：`exact_repeat_tool_call`/`plan_execution_misalignment` 命中组的净工作时长中位数比未命中组高 34%/38%，越过了既有的 30% Notable 阈值。

## Phase 3 建议：暂缓（2026-08-05）

Phase 3 两条条目（7.4 过度探索偏离主线任务、8b 约束衰减检查的 LLM 扩展）本文档 §5 各自给出的触发条件，现在都可以用 Phase 2 的真实数据核实：

- **7.4 的触发条件**（"§7.1 的语料级统计已经能提供真实的探索深度/持续时长分布"）**没有满足**——7.1 目前统计的是九项行为剖面指标的分布，不包含"探索深度"这个信号本身；要满足这条触发条件，得先新增一项指标（比如"连续读取不同目录层级的次数"），这是一项新的检测器设计工作，不是直接跑 7.1 就能拿到的副产品，原计划对这条触发条件的表述偏乐观。
- **8b 的触发条件**（"8a 已经在真实语料上跑过、误报率可接受"）**技术上跑过了，但样本量不支持下结论**——137 个候选 Journey 里 8a 只命中 1 次，1 个样本没法说"误报率可接受"，也没法说"误报率不可接受"，只能说"目前的语料规模测不出来"。在一条置信度本身还测不出来的规则之上叠加 LLM 判断，双重不确定性，ROI 判断不出来。

**建议**：暂缓 Phase 3，不是因为方向错了，是因为两条的触发条件都还没有真正满足，而满足它们所需的工作（7.4 需要新设计一个"探索深度"信号；8b 需要更大规模语料让 8a 积累到有意义的样本量）本身的优先级，应该放在"日常使用中自然积累更多真实语料"之后，而不是现在专门为了凑够样本量去人工造语料。Phase 1+2 目前的功能集合（9 个规则 Finding、决策脊柱、分叉点检测、三条 LLM 解读路径、语料级统计）已经覆盖了用户最初提出的三个方向（单 Journey 分析、双 Journey 对比、语料规律），足以作为现阶段的可用版本；下一次回来时，建议先看这段时间内 8a 的真实命中样本量是否涨到两位数以上，再决定是否重新评估 8b，而不是设一个固定的重新评估时间点。

---

## 0. ROI 分档方法论

深挖报告提了十几个具体方向，价值、成本、风险、对现有架构的依赖程度都不一样，不能按"用户提的三个方向"这种表面分组来排期。按你的要求，评估维度是：

- **短期收益**：直接命中用户诉求的程度、有没有真实案例/学术证据支撑（深挖报告 §3 的规则化边界表已经是一份现成的"这个能不能做、做出来可信不可信"的过滤器，本文档继续沿用）。
- **长期收益**：对架构健康度、可维护性、未来功能的可扩展性有没有贡献——包括"现在做很便宜，以后做很贵"的时机性收益（比如某个数据结构现在只有一两个消费者，改起来影响面小；等三四个新功能都依赖了它的旧形态，再改就要动多处）。这类投入即使短期不直接对应一个用户可见的功能，也计入回报。
- **成本**：新增代码量、是否需要新的数据结构、是否有多个变体需要维护。
- **风险**：判据本身站不站得住（深挖报告 §3 表格已经标注过一部分——"看起来像规则问题实际是语义问题"这类陷阱）、有没有编造精度的可能、会不会需要在生产验证前先返工。

三档定义：

| 档位 | 定义 |
|---|---|
| **第一档（Phase 1）** | 高价值 + 成本/风险可控；或价值一般但成本/风险极低、收益明显大于投入的项——包括为下游铺路、现在做便宜以后做贵的架构投入 |
| **第二档（Phase 2）** | ROI 大致为 1 的平衡区——高价值但也高成本/风险，或低成本低风险但也低价值/依赖 Phase 1 先落地才有意义 |
| **第三档（Phase 3）** | 投入大、短期收益小，或者本身还没有可信的实现依据（需要先做语料校准/先验证 Phase 2 假设），暂缓到最后 |

---

## 1. 全量条目分档总览

来源标注沿用深挖报告的章节号（§4/§5/§6/§7/§8）。除了报告里明确列出的条目，本文档还识别出三项深挖报告没有单独列、但落地时绕不开的**基础设施项**（标 I- 前缀）——这三项本身的 ROI 不能只看它们各自"产出"了什么用户可见的东西，要看它们分别解锁/统一了下游几个条目的实现，具体分析见 §2.2/§2.3/§2.3.2。

| 编号 | 条目 | 档位 | 一句话依据 | 状态（2026-08-05） |
|---|---|---|---|---|
| I0 | `story.Finding`/`FindingCode` 共享基础设施（含 `RenderMarkdown` 新签名，见 §2.3/§2.3.1） | **Phase 1** | §5 全部 8 条 Finding 候选的公共前提，必须先行 | ✅ 已完成 |
| I1 | `chatmsg.ToolResultList`（tool_call↔tool_result 精确配对，见 §2.2） | ~~Phase 1~~ **改期 Phase 2** | 复核发现 Phase 1 没有一项实际消费它，提前做违反 YAGNI，见"Phase 1 完成情况与复盘" | 未开始（Phase 2 首位） |
| I2 | `toolCallRepeats`（重复工具调用的定位，见 §2.3.2） | **Phase 1** | 4c/4e/5.1 三处都要判断"这次调用是不是在重复更早的调用"，现状只有一个不够用的聚合版本（`duplicateActionRate`）——不提前统一，三处会各自实现、判据大概率不一致 | ✅ 已完成 |
| 4a | 决策脊柱核心机制（默认折叠/展开） | **Phase 1** | 零新增计算，纯渲染层重分层，直接命中用户诉求一；与 I0 有一处必须协调的接口变更，见 §2.3.1 | ✅ 已完成（落地为紧凑动作列表，未做"默认折叠正常 Step"那层，见下方说明） |
| 4b | 顶层 3 秒摘要卡 | **Phase 1** | 数据全部现成（`JourneySummary`/`Metrics`），零新增计算 | ✅ 已完成 |
| 4c | Step 角色标注（7 类 tag） | **Phase 1** | 纯渲染层启发式，复用已有结构信号；"🔄重试"标签依赖 I2，不是独立信号 | ✅ 已完成 |
| 4e | 工具调用时序图（ASCII） | **Phase 1** | 纯渲染、零新增计算；"重试"符号同样依赖 I2 | ✅ 已完成 |
| 4f | 阶段聚类（工具类别粗分） | Phase 2 | 价值中等，且"阶段边界"本身是更软的判断，不像 4a-4e 那样是纯结构信号 | 未开始 |
| 5.1 | 精确重复 loop 候选 | **Phase 1** | 建立在 I2 之上（不再是"复用 duplicateActionRate 改粒度"这一句话就够了，见 §2.3.2 的修正）；MAST 最高发模式（15.7%）+ 真实 GitHub issue | ✅ 已完成，真实语料校准过 |
| 5.2 | "只说不做"循环候选 | **Phase 1** | 检测逻辑简单（连续无 tool_call 的高相似度 Step），真实案例支撑 | ✅ 已完成 |
| 5.4 | 静默声明成功候选 | **Phase 1** | 全批候选里证据最重（三起真实生产事故），检测成本中低，不需要 I1 | ✅ 已完成 |
| 5.5 | 推理-行动不一致候选 | **Phase 1** | MAST 第二高发模式（13.2%），检测复用现成的 `ExtractEntities` | ✅ 已完成，但初版假阳性率约 90%，校准后重写为"只扫最后一句 + 双向子串匹配"，详见复盘 |
| 5.8 | 计划-执行错位候选 | **Phase 1** | 自我限定为字符串匹配，风险最低的一条，检测逻辑简单 | ✅ 已完成，校准后新增 `maxPlanItems` 上限，详见复盘 |
| 6a | 分叉点检测核心机制 | **Phase 1** | 深挖报告里唯一被三份独立分析同时提出的方向；数据全部现成；直接命中用户诉求三 | ✅ 已完成，真实语料验证过 |
| 6b | 分叉点轻/重两档严重度 | **Phase 1** | 6a 的低成本延伸，一起做 | ✅ 已完成 |
| 5.3 | 错误后无适应重试 vs 有适应重试（精细化：下游影响范围 + 精确配对） | Phase 2 | 精细版依赖 I1；基础版（现有 `errorRecoveryCount`）已经存在，不算新开发项 | ✅ 已完成（落地为 `error_retry_unadapted`——"下游影响范围"追踪未落地，见复盘） |
| 5.6 | 未被利用的工具结果候选（Step 级定位） | Phase 2 | 依赖 I1 才能干净识别 tool_result；价值增量是粒度而非新信号，中等优先级 | ✅ 已完成，真实语料校准后从"按单实体判定"改为"整个结果都未被引用才判定"，详见复盘 |
| 5.7 | 上下文污染/幻象存活候选 | Phase 2 | 依赖 I1 的证伪信号识别；检测逻辑最复杂的规则候选，且报告本身标注"只是可疑信号" | ✅ 已完成 |
| 8a | 约束文本存在性检查（compaction 边界） | Phase 2 | 检测逻辑中等，且是"未经验证的假设级检测"（报告原话），需要先跑一轮真实语料看命中情况 | ✅ 已完成；真实语料上 137 个候选 Journey 里只命中 1 次，样本太少还谈不上"验证过"，详见复盘 |
| 5.9 | 单 Journey LLM 解读层扩展 | Phase 2 | 依赖 §5 规则 Finding 先落地；复用 `llm.go` 机制，增量成本低但价值是锦上添花 | ✅ 已完成；为复用 `Interpret` 调用链路，`llm.go` 改成了 `evidencePackKind` 接口 + 泛型，比原计划"完全不改核心链路"多动了一点，详见复盘 |
| 6c | 分叉点证据包 → LLM 解读 | Phase 2 | 依赖 6a 先落地；复用 4a `EvidencePack` 模式，同上 | ✅ 已完成，同上一起做的接口改造 |
| 7.1 | 语料级描述统计 + 相关性分析 | Phase 2 | 价值高（深挖报告 §7 的核心论点），但需要新的批量分析模块，且依赖 §5 Finding 已有数据可用于分组 | ✅ 已完成（`vmr story -corpus`），真实语料上相关性表格需要 Top-15 截断，详见复盘 |
| 7.2 | Finding 分组验证 | Phase 2 | 7.1 落地后的低增量后续步骤，绑定一起排 | ✅ 已完成，与 7.1 一起落地 |
| 4f | （见上，已列） | | | |
| 8b | 约束衰减检查的 LLM 扩展 | **Phase 3** | 建在 8a 之上，而 8a 本身置信度未经验证——在规则层验证有效之前叠加 LLM 层是双重风险 | 未开始 |
| 7.4 | "过度探索偏离主线任务"候选 | **Phase 3** | 报告明确标注"需要先用真实语料校准阈值，不建议直接实现" | 未开始 |
| — | 逐轮根因自动定位、跨 Journey 自动生成并直接落地 prompt 改进建议、任何编造置信度数字 | **不做** | 深挖报告 §3/§7/§9 已经用学术证据 + 行业共识明确排除，不进入任何一档的排期 | 不做 |

---

## 2. 架构总体设计（先看全局，再看模块）

这一节回答你提的关键问题：**要不要为了这整套功能，提前调整路由核心、审计日志格式、原始数据保留策略？**

### 2.1 结论：路由核心和审计日志格式不需要改

逐条核对了 Phase 1+Phase 2 的全部条目对数据的需求，结论是**全部可以从 audit.Record 已经记录的字节直接推导**，不需要新增审计字段，不需要碰 `internal/router`/`internal/server`/`internal/audit` 里任何一行。这不是巧合——这正是深挖报告 §2 第一性原理论证的直接推论：byte-faithful 记录意味着凡是"发生过"的信息都已经在里面了，缺的从来不是数据，是**从已经记录的字节里提取信号的分析代码**。具体核对：

- §5 全部 8 条 Finding 候选：只需要 `Step.ToolCalls`（含 `tc.Name`/`tc.Args`/`tc.ID`）、`Step.NewEvents`/`Event.Msg.Text`、`Step.Reasoning`/`Step.RespText`、`Step.Finish`——这些字段已经在 `internal/story/journey.go` 里，来自 `chatmsg` 对 `audit.Record.Client.Request.Body` 的解析，不需要审计层多记一个字节。
- §6 分叉点检测：只需要两个 Journey 各自的 `Manifest`/`Step` 序列，同上。
- §7 语料级统计：只需要批量的 `Metrics`（已有）+ Finding 命中情况（Phase 1 产出）+ `Journey.From`/`To`（已有，算耗时）+ `Step.Finish`（已有，见 §2.6）。
- §4 呈现层：纯渲染，不需要任何新数据。

**唯一值得单独讨论、但结论仍是"不改"的候选点**：要不要在审计层新增一个"任务是否成功"的显式标记，给 §7 的语料级统计一个干净的结果变量？见 §2.6——考虑过，结论是不做，理由是这会破坏 VMR 零埋点这个核心差异化前提（详见该节）。

**结论对整个计划的意义**：Phase 1/Phase 2 的全部工作量都落在 `internal/story`（+ 少量 `internal/chatmsg`、`internal/i18n`、`cmd/vmr`）范围内，物理上碰不到 `internal/router`/`internal/server`，`internal/archtest` 的 import 边界规则（`story`/`report`/`ctxgraph` 均不依赖 router/server）天然保证这一点不会被无意破坏。

### 2.2 提前做的内部结构调整：`chatmsg.ToolResultList`（tool_call↔tool_result 精确配对）

这是你问的"有没有必要为了后期少走弯路,提前做一些调整"最实打实的一个例子，值得展开说明。

**现状**：`internal/chatmsg` 已经有 `ToolCallList`（解出 assistant 消息里发起的工具调用，含 `ID`/`Name`/`Args`）和 `CheckToolPairing`（验证每个 tool_call 都有对应的 tool_result，F9 不变量：全语料零孤儿）。但**没有一个函数把 tool_result 的内容/是否报错/对应哪个 tool_call ID 解出来给调用方用**——现在 `story` 侧唯一的"是否出错"信号是 `errorRecoveryCount` 里一个粗糙的字符串匹配（`strings.Contains(ev.Msg.Text, "❌ is_error")`），只能回答"这个 Step 的新增内容里有没有出现过错误标记"，回答不了"具体是哪个 tool_call 的哪个 tool_result 报的错、报的什么错、后续哪次调用是在针对它重试"。

**为什么现在做便宜，以后做贵**：Phase 2 的三个条目（5.3 精细化、5.6、5.7）全部需要这个精确配对能力。如果现在不做，Phase 2 开工时会各自发明一套"从 Text 里猜 tool_result 属于哪次调用"的启发式，三份重复实现、三份潜在不一致；等 render_md.go、compare.go 等更多渲染路径也开始依赖当前"tool_result 内容被拍扁进 `Event.Msg.Text`"这个既有形状之后再回头改，影响面只会更大。现在加，是纯新增（不改 `chatmsg.Message`/`Event` 现有字段，也不改任何现有调用点），风险为零。

**具体设计**：新增一个与 `ToolCallList` 同构的函数，不改动任何现有类型：

```go
// internal/chatmsg —— 新增，不改动 Message/Event 现有形状
type ToolResult struct {
    CallID  string // 对应 ToolCall.ID
    Text    string // 复用现有 RenderPart 的渲染逻辑得到人类可读文本
    IsError bool
}

func ToolResultList(rawMsgs []any) []ToolResult
```

实现上复用 `CheckToolPairing` 已经写好的两协议扫描逻辑（OpenAI 顶层 `tool_call_id`+content、Anthropic 内容块 `tool_use_id`+`is_error`），只是从"只统计 ID 是否配对"改成"把内容和错误标记也取出来"。`story` 侧的 Finding 检测代码在需要精确配对时，直接对 Step 关联的 `audit.Record` 请求体调用这个函数，不需要经过 `Event`/`Message` 这层拍扁后的表示——**这是一个纯粹加在 chatmsg 层的新函数，不涉及审计格式，不涉及现有渲染路径**，符合 §2.1 的结论。

### 2.3 `story.Finding`/`FindingCode` 共享基础设施

对应 I0，是 §5 全部 8 条规则 Finding 的公共地基，必须最先做，和 `internal/report` 已验证的 `Finding`/`FindingCode` 模式**同构但不共享类型**——`internal/archtest` 的 import 边界规则明确禁止 `internal/story` 依赖 `internal/report`（反过来也不行，两者都只能单向依赖 `internal/ctxgraph`），所以不能直接 `import` report 的类型，只能照抄同一套设计原则，各自维护一份。这不是重复劳动的浪费：report 的 Finding 是**聚合级**（跨全部请求算出的一行统计），story 的 Finding 是**Step 级**（定位到某个 Journey 里的具体一步），形状本来就不一样，早年设计如果真的抽出一个共享类型，大概率是一次为了"复用"而做的、对两边都别扭的过度抽象——不做是对的。

新增文件 `internal/story/findings.go`（放在新文件而不是塞进 `journey.go`，理由见 §2.4）：

```go
type Finding struct {
    Code       FindingCode `json:"code"`
    StepSeq    int         `json:"step_seq"`              // 定位到具体哪一步
    RelatedSeq []int       `json:"related_seq,omitempty"` // 比如重复调用的更早出现位置
    Finding    string      `json:"finding"`               // 叙述文本，固定英文写入 JSON（同 report 的约定）
    Evidence   string      `json:"evidence,omitempty"`     // 有边界长度的证据节选
    Action     string      `json:"action,omitempty"`
}

type FindingCode string

const (
    FindingExactRepeatToolCall       FindingCode = "exact_repeat_tool_call"
    FindingNarrationWithoutAction    FindingCode = "narration_without_action"
    FindingUnverifiedSuccess         FindingCode = "error_then_unverified_success"
    FindingReasoningActionMismatch   FindingCode = "reasoning_action_mismatch"
    FindingUnusedToolResult          FindingCode = "unused_tool_result"          // Phase 2
    FindingUnverifiedEntityReference FindingCode = "unverified_entity_reference" // Phase 2
    FindingPlanExecutionMisalignment FindingCode = "plan_execution_misalignment"
    FindingConstraintTextDropped     FindingCode = "constraint_text_dropped_at_compaction" // Phase 2
)

// ComputeFindings 是每个 detector 的调度入口；detector 本身各自一个函数，
// 与 report.buildFindings 的组织方式一致。lang 只影响 Finding 字段的展示文案，
// 不影响判定逻辑本身（同 report 的 buildFindings/buildFindingsForJSON 两次调用模式）。
// 必须导出（大写）——见下面 §2.3.1，report.buildFindings 能是小写私有函数是
// 因为 report.Build 把聚合、发现、渲染全包在一个包内部；story 没有这样一个
// 统包的入口，cmd/vmr（不同包）需要直接调用它。
func ComputeFindings(j *Journey, lang i18n.Lang) []Finding
```

**一个现在就该定好、否则 Phase 2 会返工的设计要求**：`Finding` 的字段形状要为 Phase 2 的 LLM 解读层扩展（5.9）"就绪"——`StepSeq`/`RelatedSeq`/`Evidence` 这三个字段的存在，正是为了让 Phase 2 组装 `EvidencePack` 时可以直接从 `[]Finding` 里取数据拼证据包，不需要回头给 `Finding` 加字段。这是唯一一处"Phase 1 要为 Phase 2 多想一步"的地方，多花的成本是设计时多想清楚三个字段，不是提前实现 Phase 2 的任何逻辑。

### 2.3.1 一个初稿没发现的真实缺口：单 Journey 的渲染/JSON 两条输出路径目前不共享任何计算

这条是本轮复核里最重要的一处修正，值得单独展开——初版把"§4 渲染层"和"§5 Finding"当成两条可以基本独立并行的线，实地读了 `cmd/vmr/cmd_story.go` 之后发现这个假设不成立。

**现状**（读 `writeJourneyFile` 函数得到的真实调用链）：`journey-<id>.md` 由 `story.RenderMarkdown(j *Journey, lang i18n.Lang) string` 产出，`journey-<id>.json` 由 `story.Summarize(j *Journey) JourneySummary`（内部调用 `ComputeMetrics(j)`）产出——**这是两个独立调用，互不知道对方的存在，`RenderMarkdown` 现在甚至根本不接收 `Metrics`**。对比场景（`compareJourneys`）没有这个问题：`Compare`/`ComputeComparisonExtras` 的结果被赋给同一个 `cmp` 变量，`RenderComparisonMarkdown(cmp, lang)` 和 `json.MarshalIndent(cmp, ...)` 读的是同一份已经算好的数据——这正是 §3.4（6a/6b 分叉点检测）能直接挂在 `ComparisonExtras` 新增字段上、不用碰调用链路的原因。单 Journey 这条路径没有这层"算一次、两处用"的结构，Phase 1 的 4b（概览卡要读 `Metrics` 阈值）和 4a（决策脊柱要标记命中了 `Finding` 的 Step）第一次让这个缺口暴露出来。

**必须做的改动**（范围刻意收得很小，只碰一个函数）：

```go
// internal/story/render_md.go —— 签名变化，新增两个入参
func RenderMarkdown(j *Journey, m Metrics, findings []Finding, lang i18n.Lang) string

// cmd/vmr/cmd_story.go —— 只改 writeJourneyFile 一处，
// renderJourney/renderAllJourneys/compareJourneys 都不用动，因为它们都是
// 通过 writeJourneyFile 间接产出 .md/.json，改这一个函数的内部实现就够了
func writeJourneyFile(j *story.Journey, storiesDir string, lang i18n.Lang) (string, error) {
    m := story.ComputeMetrics(j)
    findings := story.ComputeFindings(j, lang)          // lang 版，喂给 Markdown
    md := story.RenderMarkdown(j, m, findings, lang)
    // ... 写 md ...
    summary := story.Summarize(j)                        // 不变；内部固定用 i18n.EN 再算一遍 Findings，
                                                           // 同 report.buildFindingsForJSON 的既有约定
    // ... 写 json ...
}
```

**诚实说明一个小代价**：这个改法让 `ComputeFindings` 在渲染一个 Journey 时被调用两次（一次 `lang`、一次 `Summarize` 内部固定的 `i18n.EN`），`Metrics` 也是类似情况（`ComputeMetrics` 在 `writeJourneyFile` 里显式调一次，`Summarize` 内部又调一次）——这是纯内存计算、没有 I/O，成本可以忽略，不值得为了省这一次重复计算去重构出一个更复杂的"算一次到处传"的结构；如果未来某天 Journey 规模大到这点重复计算真的可测量，再优化不迟。**这个改动只影响 `writeJourneyFile` 一个函数**，`renderJourney`/`renderAllJourneys`/`compareJourneys` 都通过它间接产出文件，不需要各自改动——这是刻意选的最小改动面。

**"调用两次"为什么是安全的，不是靠信任，是靠一条必须补的测试**：这个改法能成立的前提是 `ComputeFindings` 的**选择逻辑**（挑出哪些 Finding、定位到哪个 Step）完全不依赖 `lang`，只有 `Finding`/`Evidence`/`Action` 三个文本字段随 `lang` 变——如果这条前提不成立，中文版 Markdown 和英文版 JSON 就可能标出不同的 Finding 集合，那才是真正的风险。`internal/report` 已经用同一个模式解决过完全一样的问题（`buildFindings(rep, lang)` 也是被调两次：一次英文喂 `vmr-report.json`，一次目标语言喂 Markdown），并且专门写了 `TestBuildFindingsIsDeterministic`（`internal/report/aggregate_test.go`）把"两次调用选中同一批 Code"这条前提锁成一个可执行测试，不是靠代码审查时人工确认。`story.ComputeFindings` 落地时要照抄同一个模式：实现时把"选择"和"生成文本"在函数体内部分开写（选择部分只读 `Journey`/`Step` 的结构化字段，不读 `lang`），并补一个 `TestComputeFindingsIsDeterministic`，覆盖"英文调用和中文调用选出的 `Code`/`StepSeq` 集合必须逐一相同"。这一条应该和 `Finding`/`FindingCode` 类型定义一起，算进 I0 的交付范围，不是"以后有空再补"的测试。

**对 §3.5 排期的直接影响**：原计划里"Track C（渲染层）可以先用假数据/空 Finding 列表并行开发骨架"这句话需要修正——`RenderMarkdown` 的函数签名本身要变，这是 Track A（Finding 基础设施）和 Track C（渲染层）唯一一处**必须协调、不能完全独立**的接触点。实际做法是：I0 落地时把 `RenderMarkdown` 的新签名一起定下来（哪怕函数体先返回空 Finding 列表的占位实现），Track C 基于新签名开发，不需要等 Finding 检测器的具体判定逻辑写完，只需要等签名定下来——协调成本是一次接口约定，不是等整条 Track A 做完。

### 2.3.2 另一个被漏掉的共享基础设施：重复工具调用的定位，不止 Finding 需要

`§0` 总览表列的 I1（`ToolResultList`）不是本轮复核发现的唯一共享基础设施缺口。重新过一遍 §4/§5 的具体检测逻辑后发现：**"某次 tool_call 是不是在重复更早的一次同名同参调用、重复了几次、在哪几步"**这个信号，不只是 5.1（精确重复 loop 候选）要用，4c（Step 角色标注的"🔄重试"标签）和 4e（工具调用时序图里标"重试"符号）也要用同一个信号——现状是 `internal/story/metrics.go` 的 `duplicateActionRate` 只算了一个 Journey 级的聚合比率（§2.2 已经指出这一点，但当时只从"5.1 需要更细粒度"的角度分析，没意识到 4c/4e 也要用同一个东西）。

如果这三处各自实现一遍"判断是不是重复调用"的逻辑，最现实的风险不是重复劳动本身，是**三处判据不一致**——比如 5.1 的 Finding 判定阈值是"连续 3 次"，4c 的标签判定不小心写成"连续 2 次"，用户会看到某个 Step 被 4c 标了🔄重试、但 §5 的 findings 列表里却没有对应条目，观感上像是产品 bug。

**修法**：在 `internal/story/metrics.go`（`duplicateActionRate` 已经在这个文件，改动集中）新增一个更细粒度的共享函数，`duplicateActionRate` 本身改为基于它计算（而不是并存两套逻辑）：

```go
// ToolCallOccurrence 是一次工具调用在 Journey 里的重复检测结果。
type ToolCallOccurrence struct {
    StepSeq      int
    IsRepeat     bool  // 这个 (Name, Args) 组合是否在更早出现过
    FirstSeenSeq int   // IsRepeat 为 true 时，第一次出现的 StepSeq
}

// toolCallRepeats 是 duplicateActionRate（聚合比率）、5.1（Finding，定位到具体
// Step）、4c/4e（渲染层"🔄重试"标签/符号）三处的共同底层实现——三个消费者不
// 再各自判断"什么算重复"，只是从同一份结果里取不同的切片/聚合方式。
func toolCallRepeats(steps []*Step) []ToolCallOccurrence
```

这就是 §1 总览表里的 **I2**：和 I0/I1 并列，划进 Phase 1 最先做的基础设施里，而不是等 5.1 开工时顺手写一个只服务 5.1 的私有版本。

### 2.3.3 一个考虑过、但决定不做的抽象：工具"类别"（读/写/执行）分类器

复核过程里还发现一个潜在的共享需求：4f（阶段聚类，按工具类别粗分）和 5.4（静默声明成功候选，需要判断"后续有没有一次验证类调用"）都要用到"这个工具大概是读类还是写类"这个信号。**结论是不现在做**——VMR 协议无关，不知道任何具体工具/MCP server 的语义，唯一能用的只有工具名字符串本身的启发式猜测（比如名字含"read"/"list"/"get" vs "write"/"delete"/"exec"），4f 目前是 Phase 2 且本身就标了"保守版、不追求精确"，5.4 在 Phase 1 只需要一个很局部的启发式（判断是否和触发错误的那次调用同名，或者名字像读类操作），不需要一个正式的分类器。等 4f 真正开工、且发现 5.4 当时写的局部启发式不够用时，再考虑要不要抽出一个共享分类器——现在两个消费者里有一个还没到 Phase，抽象为时过早，同 `internal/story/profile` 只有两个真实实现才抽象的既有原则。

### 2.4 文件拆分与 `archtest` 预算规划

核对了真实行数（不是估算）：`internal/story/journey.go` **765 行，预算 850 行**——只剩 85 行余量，`internal/story/metrics.go`（361 行）、`compare.go`（592 行）、`llm.go`（378 行）、`render_md.go`（287 行）目前都**没有**独立预算（`archtest/file_sizes_test.go` 只给 `journey.go` 设了预算）。这里有两个直接后果：

1. **Finding 检测逻辑必须放新文件，不能塞进 `journey.go`**——8 个检测函数随便一写就是两三百行，`journey.go` 的余量完全不够。上面已经定的 `internal/story/findings.go` 解决这个问题。
2. **`render_md.go` 会因为 4a-4e 的呈现层改动明显变长**（决策脊柱摘要、概览卡、Step 角色标注、时序图，四块新渲染逻辑），且它目前**没有预算**——这正是 `file_sizes_test.go` 自己的注释点名过的错误模式（"router.go 长到 948 行没人注意，因为预算只活在注释里"；"report 的三个文件在没有预算的情况下长过了 aggregate.go 的预算而没人发现"）。建议：渲染层新功能从一开始就拆成独立文件（比如 `render_spine.go` 装决策脊柱/概览卡/角色标注/时序图，`render_md.go` 保持只做"骨架编排"，含 §2.3.1 那处新签名），并在 `internal/archtest/file_sizes_test.go` 的 `fileLineLimits` 里同时给 `render_md.go`（建议 350）、新增的 `render_spine.go`（建议 400）、`findings.go`（建议 500，8 个 Phase 1+2 检测函数够用）、`compare.go`（建议 700，6a/6b 的分叉点逻辑要加进去）、`metrics.go`（建议 500——I2 的 `toolCallRepeats` 加上 `duplicateActionRate` 的改写会让它比现在的 361 行明显变长）预先挂号，而不是等它们也长到危险线才反应过来——这本身就是"现在做便宜"的一个具体例子，成本是加几行 map 条目，收益是复刻 report 那次事后补救的教训不会在 story 包再发生一次。

### 2.5 corpus 批量分析载体设计（对应 §7）

`internal/story` 已经有现成的批量处理原语，不需要从零搭：`ListCandidates`（枚举一批输入文件里全部够格的 Journey 候选）+ `BuildAll`（批量渲染，I/O 按源文件而不是按候选数摊销）。§7 的语料级分析直接在这两个之上加一层聚合，新增文件 `internal/story/corpus.go`：

```go
type CorpusStats struct {
    JourneyCount int
    MetricDist   map[string]Distribution      // 九项指标各自的分布（均值/中位数/离群值）
    FindingRate  map[FindingCode]float64      // 每类 Finding 的命中率
    Correlations []CorrelationRow             // 见下方统计方法论
}

func ComputeCorpusStats(journeys []*Journey) CorpusStats
```

CLI 侧新增 `vmr story -corpus [-o dir]`（复用现有的 glob 输入解析），产出 `vmr-story-corpus.md`/`.json`，命名风格对齐 `vmr-report.md`/`.json`，物理上落在已有的 `{out}/stories/` 目录下，不新建目录结构。这一层完全建立在 §5 Finding 基础设施和现有 `Metrics`/`ListCandidates`/`BuildAll` 之上，是 Phase 2 而不是 Phase 1 的原因单纯是**依赖顺序**——没有 Finding 可分组之前，语料级统计只能做到"指标分布"这一层，价值打对折。

**统计方法论（避免过度承诺，呼应深挖报告 §3 的纪律）**：

- 相关性分析统一用 **Spearman 秩相关**，不用 Pearson——这与深挖报告 §7 引用的《Beyond Resolution Rates》论文本身的方法一致，且不要求线性关系、不假设正态分布，更适合 duration/token 这类天然右偏的指标。
- Finding（布尔命中）与连续指标之间的关系，用"命中组 vs 未命中组"比较中位数（Mann-Whitney U 类的非参数比较），不用 t 检验——同样是不假设正态分布。
- **不报告 p 值/显著性结论，只报告效应量**，复用 `compare.go` 里 `Notable` 已经验证过的模式（相对变化 ≥ 阈值 且 绝对差值超过噪声下限才标记）——VMR 当前语料规模（作者自用为主）撑不住严格的显著性检验，报告 p 值只会制造虚假的确定性。这是刻意的克制，不是偷懒。
- **明确不做多重比较校正之类的完整统计框架**——语料规模到那个量级之前，这类工程是纯粹的形式主义。

### 2.6 "outcome/success 标签"问题：考虑过，明确不做

§7 引用的《Beyond Resolution Rates》论文能算出"提前改代码 vs 解决率"的相关系数，前提是 SWE-bench Verified 给了客观的 pass/fail 标签。**VMR 没有这个标签，而且不应该主动去造一个**——认真想过是否要在协议层加一个"任务是否成功"的显式信号（比如约定一个 header，或者让 agent 在最后一轮回传一个结构化的完成状态），结论是**不做**，理由直接对应深挖报告 §2 的核心论点：VMR 的差异化前提是"零埋点、agent 不需要为了被观测而配合"，一旦要求 agent 主动上报一个"任务成功"字段，就是在要求 agent 侧协作，这正是 VMR 相对所有需要 SDK 埋点的竞品的护城河所在，不该为了一个统计功能主动放弃。

**替代方案**（都是从已有数据免费推导，不需要新埋点）：

- **耗时/成本**：`Journey.To.Sub(Journey.From)`、`Metrics.NetWorkingMS`、`ContextCurve` 累计 token——全部已有。
- **终止方式的弱信号**：最后一个 Step 的 `Finish` 字段（协议自带的 finish_reason/stop_reason，`compare.go` 的 `lastFinish` 已经在用这个字段做类似用途，不是本文档新发明的用法）——能区分"正常收尾"和"被截断/长度超限"两类粗粒度终止方式，不能确认任务是否真的达成了用户目标。
- **Finding 命中本身作为分组变量**：§7.2 的设计就是这么做的——"命中过某类 Finding 的 Journey" vs "没命中的"，比较两组的耗时/成本/后续行为差异，这个比较不需要任何外部真值，纯粹是内部一致性检验。

**诚实的限制**：这意味着 VMR 的语料级分析能回答"这个行为模式和耗时/成本/其他行为模式有没有系统性关联"，回答不了"这个行为模式和任务是否真的做对了有没有关系"——后者需要一个 VMR 结构性拿不到的真值。这个限制要在 §7 相关功能的文档/输出里写清楚，不能暗示 VMR 在做 SWE-bench 那种级别的成功率分析。

### 2.7 落地纪律：先用校准脚本验证命中率，再产品化

这条不是新原则，是 VMR 自己已经在用、只是没被总结出来的做法——`internal/ctxgraph` 的编辑分类阈值（`contractLenRatio=0.6` 等）、`stitch.go` 的缝合置信度阈值，都是"先在真实语料上跑、看分布是否合理，再定下来"，而不是拍脑袋定一个数字就上生产。§5 的 8 条 Finding 候选、§7 的相关性分析，全部涉及需要校准的具体阈值（"连续几次算精确重复"、"文本相似度多高算'只说不做'"、"多长的推理才纳入不一致检测"）——建议每个检测器落地前，先写一个**一次性校准脚本**（不是产品代码，跑在已有的真实审计语料上，量级参考 Analytics 设计文档记录的 7112 条记录/809K 条消息实例这批已经验证过的语料），输出命中率和几个人工抽查样本，确认误报率在可接受范围再把阈值定下来、接入 `journey-<id>.json` 的默认输出。这个步骤成本是几小时到一天，换来的是不会把一个凭直觉定的阈值直接暴露给用户当作产品行为。

---

## 3. Phase 1（第一档）详细设计

### 3.1 I0/I1/I2 基础设施

已在 §2.2/§2.3/§2.3.1/§2.3.2 给出完整设计，这里只强调顺序：I0（`findings.go` 骨架 + `Finding`/`FindingCode` 类型 + `RenderMarkdown` 的新签名约定，见 §2.3.1）、I1（`chatmsg.ToolResultList`）、I2（`toolCallRepeats`）三者互相独立，可以并行做；I0（含新签名约定）必须先于 5.1/5.2/5.4/5.5/5.8/4a/4c/4e 全部完成（哪怕函数体先返回占位空值），因为它们要么调用 `ComputeFindings`，要么调用改了签名的 `RenderMarkdown`。I2 必须先于 5.1 和 4c/4e 完成，理由见 §2.3.2——这三项是本计划里唯一"必须先做、且下游有多个条目依赖"的基础设施，其余 Phase 1 条目互相之间没有这种强依赖。

### 3.2 §5 规则 Finding：5.1/5.2/5.4/5.5/5.8

五个检测器逻辑上不互相依赖，可以并行开发，逐个接入 `ComputeFindings` 内部的调度：

- **5.1 精确重复 loop 候选**：直接消费 I2（`toolCallRepeats`）的输出——遍历返回的 `[]ToolCallOccurrence`，达到校准出的重复次数阈值（§2.7）时在**第 N 次出现的 Step** 产出一条 `FindingExactRepeatToolCall`，`RelatedSeq` 挂 `FirstSeenSeq` 及中间几次出现的位置。**不再是"改造 `duplicateActionRate`"这一句话就够了**——按 §2.3.2 的修正，`duplicateActionRate` 本身也要改成基于 `toolCallRepeats` 计算，避免两套判据。
- **5.2 "只说不做"候选**：遍历连续 `len(Step.ToolCalls)==0` 的 Step 序列，对 `RespText` 做词集合 Jaccard 相似度（不需要更复杂的方法——校准脚本先跑通再谈精细化），连续高相似度序列达到阈值触发 `FindingNarrationWithoutAction`。
- **5.4 静默声明成功候选**：遍历 Steps 维护"未验证错误"状态机——遇到 `NewEvents` 命中 `isErrorMarker` 置位；命中"验证类"调用（同工具再次调用，或名字像读类操作——只是一个局部启发式，不是 §2.3.3 讨论过、决定不做的正式分类器）清位；命中"完成型"信号（`Finish` 非空且后续无更多 ToolCalls，或本 Task 结束）且状态仍置位则触发 `FindingUnverifiedSuccess`。
- **5.5 推理-行动不一致候选**：`ExtractEntities(Step.Reasoning + " " + Step.RespText)` 与 `ExtractEntities(拼接 ToolCalls[].Args)` 做集合差；推理侧存在、行动侧完全不出现的实体（且推理文本长度过一个下限，避免噪声）触发 `FindingReasoningActionMismatch`。
- **5.8 计划-执行错位候选**：正则匹配 `^\s*\d+[.、]\s` 形式的编号列表（Task 内首个推理块里找），逐条与后续 ToolCalls/RespText 做关键词/实体重合度检查，标注每条为"已执行/跳过/顺序变更"；没匹配到编号格式直接跳过，不触发任何 Finding。

### 3.3 §4 呈现层：4a/4b/4c/4e

建议合并成一轮渲染层改动，理由是它们共享同一批输入（`Journey`/`Metrics`/Phase 1 已产出的 `Finding`），拆开做会重复走查同一份数据。**前提**：`RenderMarkdown` 的签名已经按 §2.3.1 改成 `RenderMarkdown(j *Journey, m Metrics, findings []Finding, lang i18n.Lang) string`，`cmd/vmr/cmd_story.go` 的 `writeJourneyFile` 已经按 §2.3.1 改好——这四块渲染逻辑都写在新签名之上，不是各自决定"要不要接 Finding"。

- **4a 决策脊柱**：`render_md.go`（或拆出的 `render_spine.go`，见 §2.4）新增一段紧凑列表：每 Task 的目标（`Task.Title`）→ 关键 Action（`ToolCalls[].Name` + 参数摘要）→ 是否命中入参 `findings` 里的某一条。默认折叠正常 Step，展开命中 Finding 或 `StitchEdge`/`Event.Revises` 非空的 Step。
- **4b 概览卡**：Journey 页顶部新增一句话任务描述（`Journey.Title`）+ 5-8 节点时间线（起始指令/首个错误/非 Append 转折点/终止）+ 基于入参 `m`（`Metrics`）阈值的结构标签（"工具密集型"/"重试多"/"上下文压缩"）——全部现成数据，零新增计算。
- **4c Step 角色标注**：7 类 tag（📋规划/🔧执行/👀观察/🔄重试/⚠️错误/🧹压缩/💬汇报）纯渲染层判定，复用 `len(ToolCalls)`、`isErrorMarker`、`StitchEdge` 已有的判据，"🔄重试"这一类直接查 I2（`toolCallRepeats`）的结果，不重新判断。
- **4e 工具调用时序图**：ASCII，每工具一行，横轴 Step 编号，符号区分成功/失败/重试——数据来自 `Metrics.ToolCallDist` + 逐 Step 遍历，"重试"符号同样查 I2 的结果，纯渲染。

**i18n 工作量**：以上四块加 §5 的 Finding 文案，全部要走 `internal/i18n` 现有模式（`internal/i18n/story_render.go`/新增 `story_findings.go`，按"文案跟着消费它的源文件走"的既有组织原则，函数返回值字段拼错编译期报错的既有纪律）——这是每个条目自身工作量的一部分，不单列成一个新任务。

### 3.4 §6 分叉点检测：6a/6b

新增函数（放进 `compare.go`，预算调整见 §2.4）：给定两个 Journey 已对齐的 Task/Step 序列（按 Task 序号对齐，长度不一致时比较到较短一方的末尾），逐 Step-pair 计算结构签名 `(是否有 tool_call, 排序后的工具名集合)`；第一个签名不同的 Step 即候选分叉点。严重度：工具名集合相同但签名判定为不同（说明是参数层面差异）→ 轻度；工具名集合本身不同 → 重度。产出结构挂在 `ComparisonExtras` 旁边（新增字段，不改动现有字段），供 `render_compare.go` 渲染、也为 Phase 2 的 6c（证据包）提供输入——这条路径不受 §2.3.1 那个问题影响，因为 `compareJourneys` 本来就把 `Compare`/`ComputeComparisonExtras` 的结果放进同一个 `cmp` 变量、同时喂给 `RenderComparisonMarkdown` 和 JSON 序列化，是全篇唯一一处"MD 和 JSON 从一开始就共享同一份计算结果"的路径。

### 3.5 排期建议与并行度

对应 CLAUDE.md 里"AI-native、≤3 人团队"的实际团队规模，Phase 1 的条目互相独立性总体较强，但 §3.1 指出的三项基础设施（尤其 I0 里 `RenderMarkdown` 的签名变更）是真实的协调点，不能像初版计划那样假设渲染层可以完全不管基础设施进度、自己先用假数据跑：

```
Track A: I0 骨架（含 RenderMarkdown 新签名 + ComputeFindings 空实现）→ 5.1 → 5.2 → 5.4 → 5.5 → 5.8
         （同一人做完，因为共享 ComputeFindings 调度逻辑，切换上下文成本低）
Track B: I1（chatmsg.ToolResultList，完全独立）→ 为 Phase 2 铺路，Phase 1 内部没有条目强依赖它，可穿插做
Track C: I2（toolCallRepeats，独立，但要在 5.1/4c/4e 开工前完成）→ 4a/4b/4c/4e 渲染层
         （4a/4b/4c/4e 依赖 Track A 先把 RenderMarkdown 的新签名和 cmd_story.go 的
         writeJourneyFile 改造定下来——这是一次接口约定，不是等 Track A 的 5 个检测器
         全部写完；接口定完之后，Track C 可以用空 Finding 列表并行开发渲染骨架，不用等
         检测器的具体判定逻辑）
Track D: 6a/6b 分叉点检测（完全独立，不经过 §2.3.1 那条改动，可随时开工）
```

---

## 4. Phase 2（第二档）详细设计

### 4.1 §5.3 精细化 / 5.6 / 5.7 / 8a

四条全部依赖 I1（`ToolResultList`）已经落地：

- **5.3 精细化**：用 `ToolResultList` 精确定位"哪个 tool_call 的哪个 tool_result 报了错"，再看后续 Step 里是否有同工具的 ToolCall（参数是否变化）——区分"逐字重复"vs"参数调整过的重试"，并向后追踪 N 个 Step 判断"下游影响范围"（是否持续偏离正常模式）。现有的 `errorRecoveryCount`（聚合计数）保留不变，这是它的 Step 级、精细化版本，新增而非替换。
- **5.6 未被利用的工具结果**：用 `ToolResultList` 拿到干净的 tool_result 文本（不再依赖 `NewEvents` 里的拍扁文本猜测），`ExtractEntities` 后检查后续 Step 是否再引用。
- **5.7 上下文污染/幻象存活**：`ToolResultList` 识别证伪信号（`ENOENT`/404/"not found" 类正则），关联到具体实体，追踪该实体后续是否仍被引用而未见更正——报告里已经强调这只是"可疑信号"，不是幻觉确认，产出文案要保留这条限定。
- **8a 约束文本存在性**：在 `StitchEdge != nil` 的 Step，取 boundary 前最近一次真实用户指令的关键实体/关键词，检查 boundary 后的摘要文本是否仍能找到——直接复用 `internal/chatmsg.ExtractEntities` + 已有 §3.6 compaction 信息损失摘要机制，属于新增最小的一条。

### 4.2 §5.9/6c：LLM 解读层的对称扩展

两条都是"复用 `llm.go` 现有机制（`EvidencePack` 构建 → prompt → 调用 → 落盘缓存 → 可整体降级），只新写一份 system prompt + 证据包组装函数"：

- **5.9**：单 Journey 版证据包 = Journey 的 Metrics + Phase 1/2 产出的 `[]Finding`（这就是 §2.3 里强调"Finding 要为此就绪"的地方派上用场——不需要回头改 Finding 的形状）。
- **6c**：分叉点版证据包 = 6a 定位到的分叉 Step 前后几步的证据（双方目标/工具选择/工具结果摘要），复用 4a `EvidencePack` 已验证的"受限证据包、必须声明看不到什么"的 prompt 纪律。

两条都不改动 `llm.go` 的核心调用链路（缓存 key、dry-run、fail-open降级）——只是新增证据包构建函数和 system prompt 常量，跟现有 `-compare` 场景的证据包并列，不重构共享部分。**是否值得为三条证据包路径（compare 原有的 + 5.9 + 6c）重构出一层共享的"证据包构建器"抽象**：评估后**不建议现在做**——三个证据包的字段形状本来就不同（两 Journey diff vs 单 Journey vs 分叉点局部证据），强行抽象大概率是为了"看起来更 DRY"而不是真的复用，等真的出现第四个场景时再看是否有共同模式，符合项目自己"只有两个真实实现才抽象"的既有纪律（`story/profile` 的 `OpenClawAware`/`Generic` 就是同一原则的先例）。

### 4.3 §7.1/7.2：语料级统计

设计已在 §2.5/§2.6 给出完整方案（`internal/story/corpus.go`、`vmr story -corpus`、Spearman + Notable 阈值、不用 p 值、不造 success 标签）。7.2（分组验证）是 7.1 落地后的自然延伸——按 Finding.Code 分组，对每组比较 `Metrics`/耗时/成本的中位数差异，复用同一套 Notable 阈值判定，不需要额外的新设计。

---

## 5. Phase 3（第三档，暂缓）

> 这一节是 Phase 2 开工前的原始设计判断；Phase 2 完成后用真实数据核实过这两条触发条件是否已经满足（结论：都还没有），见上面"Phase 3 建议"一节——原始判断方向没错，具体数字现在有真实依据支撑了。

两条都不是"技术做不到"，是"现在做的依据不够扎实"：

- **7.4 过度探索偏离主线任务**：触发条件是 §7.1 的语料级统计已经能提供真实的"探索深度/持续时长"分布——先看数据再定阈值，不是先定一个",4 层 5 步"这样的数字再去验证。
- **8b 约束衰减检查的 LLM 扩展**：触发条件是 8a（规则层的约束文本存在性检查）已经在真实语料上跑过、误报率可接受——在一个自己都标注"未经验证"的规则之上叠加 LLM 判断，是在两层不确定性上面再加一层，没必要抢在 8a 验证完之前做。

---

## 6. 明确不做的事（汇总）

- 审计日志格式/路由核心的任何改动（§2.1）。
- 引入显式的"任务成功/失败"标签或要求 agent 协作上报状态（§2.6）——与 VMR 零埋点的核心差异化前提冲突。
- 逐轮根因自动定位——深挖报告 §3 已用 Who&When/TRAIL 的数据证明这在 2026 年中仍是开放问题，11%-14.2% 的准确率不足以支撑任何产品化承诺。
- 跨 Journey 自动生成 prompt/CLAUDE.md 改进建议并直接落地——行业共识（LangSmith/Arize 都保留人工审核关卡）明确排除。
- 为 5.9/6c 的三条 LLM 证据包路径做提前的共享抽象（§4.2）——YAGNI，等出现第三个真实需要复用的场景再说。
- `story.Finding` 与 `report.Finding` 共享同一个类型——违反 `archtest` 的 import 边界，且两者语义粒度本来就不同（§2.3）。
- 相关性分析报告 p 值/显著性结论（§2.5）——当前语料规模撑不住，只报效应量。
- 为"工具类别（读/写/执行）"提前抽出一个正式分类器（§2.3.3）——4f 和 5.4 目前各只需要一个局部启发式，第二个真实、更讲究的需求出现之前不值得抽象。

---

## 7. 风险与开放问题

- **Finding 检测器的实际误报率是未知数**——§2.7 的校准脚本纪律是主要缓解手段，但 Phase 1 排期应该给"跑校准脚本、调阈值"预留时间，不能假设第一版阈值就能直接用。
- **决策脊柱的"默认折叠"标准需要真实用户反馈**——多少比例的 Step 应该默认展开，是一个体验判断，不是纯技术问题；建议 Phase 1 上线后观察真实使用（作者自用场景）再调，不预先假设一个完美比例。
- **`render_md.go` 拆分的具体切法**（§2.4 建议的 `render_spine.go`）是本文档给出的一个方案，不是唯一方案——实际开工时如果发现更自然的切分边界（比如按"概览卡 vs 决策脊柱 vs 时序图"三个文件而不是一个），应该以代码实际长出来的形状为准，不要削足适履去凑本文档预设的文件名。
- **I0 的接口约定（`RenderMarkdown` 新签名）是 Track A/C 唯一的强协调点**（§2.3.1/§3.5）——如果开工时这个签名没有第一时间敲定、Track C 先按自己的假设写了一版，返工成本会比本来该有的更高；建议这一个接口签名由 Track A/C 各出一人拍板，不要等 Track A 全部检测器写完才对齐。
- **Metrics/Findings 在单 Journey 路径上被重复计算两次**（一次 lang 版给 Markdown，一次 `Summarize` 内部固定 EN 版给 JSON，见 §2.3.1）目前判断是"纯内存计算、可忽略"，前提是 Journey 规模保持在当前语料量级（几十到几百步）；如果未来出现远超此量级的超长 Journey，这个假设需要重新检查，不是现在就要解决的问题。
- **语料规模是否足够支撑 §7 的相关性分析给出有意义的结果**，本文档判断不了（取决于作者当前实际积累的 Journey 数量），建议 7.1 落地后先看样本量，样本量过小（比如远少于几十个 Journey）时，产出应该诚实标注"样本不足，结论仅供参考"而不是压低阈值硬凑出"发现"。
