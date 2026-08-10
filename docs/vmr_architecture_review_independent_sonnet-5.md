<!-- Ver 2026-08-10, by Sonnet 5 (Claude) -->

# vmr 架构深度审查（独立视角）：复杂度 / 健壮度 / 可扩展性

> **独立性声明**：这是一次完全独立的审查。执行前我没有参考仓库里已有的两份审查文档
> （`vmr_architecture_review_kiss_single_maintainer_2026-08-10.md`、`vmr_architecture_review_gemini-3.6-flash.md`），
> 结论、发现、优先级判断全部基于本次自己重新阅读的设计文档与源码。如果本文与那两份文档在某些结论上重合，
> 那是因为代码事实本身收敛到了同一个结论，不是抄录；凡是本文与它们的判断不同的地方，以本文当次独立判断为准，
> 不做"调和"。
>
> **记录方式**：边读边记。每个模块内逐文件/逐段记录"现状判断 → 发现/问题 → 建议"，模块读完后再跳出来做一次
> 模块级回顾。全部模块读完后才写最终的全局总结。结论在文末，证据散落在各模块小节——这是刻意的，过程本身
> 是审查可信度的一部分。
>
> **更新说明（第二轮，交叉核实）**：本文档写完之后，又与仓库里另外两份既有审查文档
> （`vmr_architecture_review_gemini-3.6-flash.md`、`vmr_architecture_review_kiss_single_maintainer_2026-08-10.md`，
> 后者是同一作者上一轮会话的独立产出）逐项交叉核实。两份文档里核实成立、且本文档原本没有覆盖的点，已经
> 补充进下文对应章节（用 **"（交叉核实补充）"** 标注来源，便于区分"本文档独立读代码得出的发现"与"经交叉
> 核实后采纳的外部发现"）。两份原始文档里已逐项加了批注（🔵已吸收 / ✅新信息已吸收 / 🟡部分吸收 / ❌不成立），
> 可对照参看；本文档不重复陈述批注过程，只呈现核实后的最终结论。

---

## 0. Debrief：任务要求（我对用户诉求的理解）

用户是 vmr 项目的维护者：**AI-native、≤3 人团队，事实上是单人 + AI Agent 协助**。要求对整个项目做一次深入的
架构审查，评价维度是**复杂度、健壮度、可扩展性**，价值取向明确锚定 **Linus Torvalds 的设计哲学**——
less is more、DRY、KISS——但用户加了一层比"抽象的架构美学"更具体、更严苛的约束：

**这是单人可维护的独立开源项目。开发工作量本身不是瓶颈（有 AI Agent 代劳），真正的瓶颈是项目的规模、复杂度、
架构设计的"心智覆盖范围"必须始终落在一个人能独自 hold 住的区间内。** 模块划分、耦合方式、核心逻辑，都应该
以"简单有效"为第一目标，而不是"功能完备"或"设计上优雅但需要专家才能安全修改"。

用户给了一个具体例子帮助校准尺度：**Token Plan / Coding Plan 额度路由**。核心诉求是"把不同套餐额度合理分配、
最大化效益"，**不是**"对当前各家 Coding Plan 计价逻辑做一次巨细靡遗的完整翻译"。用户明确要求：解决问题的同时
应该提取问题的核心、尽量化简，而不是把问题域里能想到的维度都建模进来。用户特别强调这不只是这一个功能的问题，
**其余模块大概率有同类倾向，要求我用同一把尺子逐一检查**。

**流程要求**（用户逐字给出，我照此执行）：
1. 先读指定的关键文档，再制定 review 计划，计划本身要写进这份文档；
2. 按模块、按文件、按代码、按配置、按文档全部过一遍；**每个文件/模块当下就把发现记录进本文档**（不是先在脑内
   过一遍最后再总结，而是读到哪里记到哪里）；
3. 每个模块的全部记录写完后，再做一次"跳出单文件局部"的模块级回顾——找模块内部的跨文件模式、与整体哲学的
   一致性、对未来扩展的影响；
4. 全部模块完成后，做一次完整的全局回顾与总结：把发现分类，**重要的逐项详述 + 给建议方案，不重要的仅罗列**
   （每条一句话，不展开）；
5. 从全局视角判断整体架构有没有大问题、有没有需要调整的空间；
6. 指出"该做而没做、该升级而没升级、该完善而没完善"的缺口——即现在代码里看不到、但实际重要的缺失部分；
7. 补充以上流程未必能覆盖到、但确实重要的其他发现。

用户同时要求**兼顾**下一阶段的战略方向（`docs/future-strategy/agent_runtime_analysis_v1.0_custom-2-agent.md`
的 Agent 运行时轨迹分析方法论、`docs/future-strategy/vmr_strategy_synthesis_gemini-3.6-flash.md` 的三方战略
合成），但**不要求为未来功能做设计或重构**——只要求评估当前架构对这些方向是否有良好的扩展/升级空间，
如果发现局限，要重点指出。

**我对这个任务的理解边界**：这不是一次"找 bug"式的代码审查（`OUTSTANDING_ISSUES_opus-5.md` 已经在做这件事，
且做得很仔细），而是一次**架构级**审查——单个函数写得对不对不是重点，"这一层的存在本身、这一层的复杂度预算、
这一层未来会不会变成单人维护的负担"才是重点。凡是 `OUTSTANDING_ISSUES_opus-5.md` 已经收录并给出明确处置意见
（做/不做/待定+理由）的具体代码问题，本文不重复收录为"新发现"，但会在架构判断中引用它们作为证据。

---

## 1. 已阅读的文档（本轮独立通读，非复用之前会话的阅读记录）

| 文档 | 阅读深度 | 目的 |
| --- | --- | --- |
| `README.md` | 全文 | 项目对外定位、双支柱叙事、与 LiteLLM 的差异化主张 |
| `CLAUDE.md`（项目 brief，会话开始即注入） | 全文 | 模块地图、不变式清单、约定 |
| `docs/VirtualModelRouter_Design_v4_Core.md` | 全文（918 行） | 路由核心设计：协议模型、Adapter、调度健康、Sticky、额度路由、图片降采样 |
| `docs/VirtualModelRouter_Design_v4_Analytics.md` | 全文（482 行） | report/story 设计：ctxgraph 内容寻址、Finding 体系、i18n 机制 |
| `docs/TokenPlan_Quota_Routing_Design_opus-5.md` | 全文（1414 行） | 额度路由完整设计、市场数据依据、分批交付、"现状与后续计划" |
| `docs/OUTSTANDING_ISSUES_opus-5.md` | 全文（265 行） | 现有已知问题基线，避免本文重复发明 |
| `docs/future-strategy/vmr_strategy_synthesis_gemini-3.6-flash.md` | 全文（292 行） | 三方战略报告合成：共识、分歧、护城河评估、路线图 |
| `docs/future-strategy/agent_runtime_analysis_v1.0_custom-2-agent.md` | Debrief + 落地指南 + 优先级建议章节精读，中间方法论章节结构性通读 | Agent 运行时分析方法论全景，用于评估架构扩展性 |

**未读入本次审查、但知道其存在的文档**（不影响本次结论，仅供交叉参照）：`vmr_architecture_review_gemini-3.6-flash.md`、
`vmr_architecture_review_kiss_single_maintainer_2026-08-10.md`（均为既有审查，本次刻意不读，见开篇独立性声明）、
`docs/future-strategy/` 下其余 10 份文档（战略/竞品/深挖类，非本次任务的核心输入）。

**代码库基线**（本次审查现场实测）：

```
生产代码：31,596 行（不含 archtest，它是纯测试包）
测试代码：33,228 行
23 个 internal/ 包 + cmd/vmr（2,621 行，12 文件）
直接依赖 4 个：yaml.v3 / fsnotify / golang.org/x/image / klauspost/compress(zstd)
go.mod: module vmr, go 1.25.1
```

分包体量（生产代码行数，降序）：

```
report      6752   story       5281   router      2835   i18n        2465
config      1499   ctxgraph    1408   chatmsg     1015   adapter      971
pricing      880   audit        843   server       723   imgprep      710
diagnose     644   quota        622   core         554   replay       472
strategy     215   health       213   probe        128   sticky       104
buildinfo     96   fmtutil       78   rundir        60
```

分析半区（report+story+ctxgraph+chatmsg+i18n）= 6752+5281+1408+1015+2465 = **16,921 行，占生产代码 53.5%**；
路由半区（其余全部 + cmd）= 31,596−16,921 = **14,675 行，占 46.5%**。两者体量已经接近但分析半区略大——
这个数字本身在后面的全局总结里会反复被引用，先在这里钉一个准确的基线（下文引用统一以此处为准）。

---

## 2. 我的 Review 计划

不打算照搬 CLAUDE.md 的模块地图顺序，而是按**依赖层级从底向上**走——先看零依赖/少依赖的基础层，再看基础层
支撑起来的复杂逻辑层，这样在读复杂模块时，已经对它依赖的基础设施有判断，不需要来回跳。分析半区同理，从共享
解析层开始。

1. **路由半区 · 基础层**：`core`、`fmtutil`、`rundir`、`buildinfo`、`config`——先摸清"类型从哪来、配置怎么校验"
2. **路由半区 · 协议与调度**：`adapter`（+ 三协议实现）、`probe`、`strategy`、`health`、`sticky`——协议插件机制
   与调度状态机，这是"扩展性核心"的直接证据所在
3. **路由半区 · 核心与入口**：`router`（failover 主循环、响应归一化、额度决策）、`server`（HTTP 入口）——
   全项目复杂度密度最高的一层，独立分配最多精力
4. **路由半区 · 记录与工具**：`audit`、`imgprep`、`diagnose`、`replay`
5. **路由半区 · TokenPlan 专项**：`quota`、`pricing`——用户点名的重点，我会专门用"是否提取了核心/是否过度
   建模"这把尺子逐条核对，不预设结论
6. **分析半区 · 共享层**：`chatmsg`（消息解析真相源）、`ctxgraph`（内容寻址图）
7. **分析半区 · 产物层**：`report`（聚合报表，全项目最大包）、`story`（任务叙事，第二大包）、`i18n`（双语文本）
8. **CLI 与工程配套**：`cmd/vmr`、`archtest`、`vmr.sh`、CI、`loadtest`、配置示例文件
9. **文档体系**：设计文档、`OUTSTANDING_ISSUES`、`future-strategy` 目录的组织方式本身
10. **全局总结**：重要问题分类详述 → 次要问题罗列 → 全局视角与调整空间 → 缺口 → 其他补充 → 一句话结论

每个模块小节按"现状判断 → 发现/问题 → 建议"记录；模块末尾加"模块回顾"跳出来看跨文件的模式。

---

## 3. 路由半区 · 基础层

### 3.1 `internal/core`（core.go 493 + headers.go 62 = 555 行）

**现状判断**：零内部依赖的共享类型包——`CanonicalRequest`/`ErrorClass`/`Endpoint`/`RequestFacts`，加上 P2 额度路由的
运行态类型（`Limit`/`TokenWeights`/`Rate`/`PricingSpec`/`PricingOverride`/`QuotaSpec`）、header 黑名单
`FilterClientHeaders`、token 估算 `EstimateTextTokens`，以及两个 JSON 响应写出的小函数 `WriteJSON`/`WriteError`。
文档注释密度很高，几乎每个字段都写了"为什么"而不只是"是什么"。

**发现/问题**：
- **`core` 同时装了"类型"和"行为"**：`WriteJSON`/`WriteError` 不是类型，是会产生 I/O 副作用的函数（写
  `http.ResponseWriter`）。这package 自己的定位（见 v4 Core 设计文档模块表）是"无依赖的共享类型"，`WriteJSON`/
  `WriteError` 严格说不属于这个charter——它们能待在这里纯粹是因为"router 和 server 都要用、且不想互相依赖"，
  是一个功能上合理但分类上不纯粹的例外。目前只有两个函数，代价可忽略，但如果以后再往这类"两处都要用的行为"
  堆东西，`core` 会从"类型包"变成"什么都能放的公共抽屉"——这个风险在文档里没有被提及，值得记一笔。
- **`core.go` 一个文件装七个不同领域概念**：路由类型（`CanonicalRequest`/`Endpoint`）+ 错误分类
  （`ErrorClass`）+ 额度类型（`Limit`/`TokenWeights`/`QuotaSpec`）+ 定价类型（`Rate`/`PricingSpec`/
  `PricingOverride`）+ token 估算 + 泛型工具函数（`SortedKeys`）。目前 493 行还可控，但这是全项目"概念密度"
  最高的单个文件——**读这一个文件需要同时理解路由调度、健康/额度记账、定价三个子域的词汇**。这不是错误
  （项目文档里已经承认这是"公共抽屉"的延续），但对单人维护者而言，这个文件是"每次改动都要小心不要引入
  跨域耦合"的高风险区，即使它本身零依赖。
- **`core.Rate`（`*float64` 分量）与 `internal/pricing.Rate`（`float64` 分量）是同一概念的两份类型定义**——
  设计文档里承认这是"`core` 不能反向依赖 `pricing`"这条 archtest 约束的直接后果，是有意为之，不是疏忽。
  但客观后果是：一个"四分量费率"概念现在有 **YAML 形态（`PricingOverrideConfig`）→ `pricing.Rate` → `core.Rate`**
  三层几乎相同的结构体，任何字段级改动（比如加一个新的费率分量）理论上要同步改三处 + 三处测试。这是"分层
  换取无环依赖"付出的具体代价，值得在未来新增字段时提前意识到。
- `Endpoint.Freeze()` 的"冻结后走缓存字段、未冻结每次现算"双路径是合理的工程选择：生产路径走 `BuildSnapshot`
  一定会 `Freeze()`，测试里直接构造 `&core.Endpoint{}` 字面量的场景仍然正确（只是慢），没有隐藏的正确性陷阱。
- `TokenWeights` 的零值 `{0,0,0,0}` 不等于文档承诺的"全 1.0"默认值——这确实是一个"必须由外部调用方显式填充"
  的隐式契约。但我核对了它唯一的生产构造点（`internal/config/quota.go` 的 `TokenWeightsConfig.resolve`），
  该函数无条件把四个分量初始化为 `core.DefaultTokenWeight`，只有显式配置的分量才会覆盖——**当前唯一的生产
  路径是正确且良好测试的**，这个"零值陷阱"目前更像是一个写在注释里的预防性警告，而不是一个正在发生的风险。
  风险点在于：如果未来出现第二个构造 `core.TokenWeights` 的地方（比如某个新命令行工具直接拼一个值），
  这条隐式契约不会被编译器提醒。

**建议**：
1. 不建议现在拆分 `core.go`——七个领域概念虽然多，但都是小型、稳定的数据结构，拆分本身的收益（"文件更小"）
   小于拆分成本（"多包意味着多一层 import，路由核心概念反而更难一眼看全"）。**但建议今后新增的运行态类型
   优先考虑独立叶子包**（`quota`/`pricing` 已经示范了"零依赖叶子包 + 只有 core 依赖它"的更优模式），不要
   再往 `core.go` 里加新领域。
2. `WriteJSON`/`WriteError` 可以考虑挪到一个新的极小 `internal/httpresp`（或类似）包，让 `core` 回归纯类型
   定位——但这是"分类洁癖"级别的建议，不紧急，两个函数的维护成本本身就很低。
3. 可选：给 `core.TokenWeights` 加一个 `NewTokenWeights()` 构造函数（返回全 1.0），并让 `config/quota.go`
   改用它而非手写四个字段赋值——这样"默认全 1.0"从注释变成了代码本身能体现的不变量，即使不能被编译器强制，
   至少给未来的调用方一个显而易见的正确起点。收益是防御性的，不紧急。

> **✅ 已修复（2026-08-10，第二轮 ROI 复核）**：新增 `core.NewTokenWeights()`。**重新读代码时发现
> 这条建议原先低估了问题范围**：本文档当初只核对了 `config/quota.go` 一处"唯一的生产构造点"，
> 这次落地前重新 grep 了全仓库，实际还有第二处独立的同款四字段字面量——`cmd/vmr/cmd_check.go` 的
> `printProviderQuota`（用于判断 `token_weights` 是否为默认值、决定要不要在 `vmr check` 输出里打印
> 这一行）。也就是说"零值陷阱"预见的"未来第二个构造点"其实已经存在，不是假设性风险。两处都已改用
> `NewTokenWeights()`，`core.DefaultTokenWeight` 不再被外部直接引用拼字面量。`go build`/`go vet`/
> `go test -race ./...` 全绿，`internal/config`/`cmd/vmr` 里覆盖 `token_weights` 解析与展示的既有
> 测试原样通过（纯等价重构，无行为变化）。

### 3.2 `internal/fmtutil`（136 行）+ `internal/rundir`（60 行）+ `internal/buildinfo`（96 行）

**现状判断**：三个极小的叶子包，各自单一职责：显示格式化（`FmtBytes`/`FmtTokens`/`FmtSeconds`/`DisplayZone`）、
默认目录解析（`~/.vmr` → 临时目录 → cwd 三层兜底）、构建身份（读 Go 自带的 VCS stamp，不需要 ldflags）。

**发现/问题**：这三个包是全项目"小而正确"的最佳范例，没有发现值得记录的问题。`buildinfo` 刻意不编造 semver
（`Short()` 的文档注释里专门论证了"没有能递增它的发布流程"），这个克制在单人项目里是对的判断——一个没人
维护更新的版本号比 commit SHA 更容易误导人。`rundir` 的三层兜底逻辑用纯函数 `resolve()` 与实际读环境的
`Resolve()` 分离，方便测试，是好实践。

**建议**：无。这三个包应该作为"新叶子包该长什么样"的项目内部范例。

### 3.3 `internal/config`（config.go 629 + quota.go 305 + pricing.go 403 + check.go 100 + watch.go 62 = 1499 行）

**现状判断**：严格 YAML（`KnownFields`）、`${ENV}` 展开、`applyDefaults`/`validate`/`Check` 三层分离、fsnotify
热加载防抖。这是我目前读过的所有模块里**工程纪律最彻底**的一层——每一条校验规则背后几乎都有一句"为什么现在
拒绝比以后静默出错更好"的论证，`validate()` 与 `Check()` 的二分（前者阻止启动、后者只是"能跑但可疑"）设计
干净。

**发现/问题（含我核对代码后独立形成的判断，不预设立场）**：
- **`config.go` 629/750 行，逼近 archtest 预算**——`validate()` 本身已经超过 200 行，混合了 provider 校验、
  model/endpoint 校验、`providerModels` 集合收集（专为 `resolvePricing` 服务）。这是可预期的撞线：一旦再加
  一类新校验（比如未来的 P3 多窗口额度），大概率直接触发 archtest 失败。**archtest 的注释已经明确反对"调大
  预算数字"这条退路**，所以下次改动这个文件时，应该先把 provider 校验和 model/endpoint 校验拆成独立函数
  （不一定要拆文件，先拆函数降低单个函数的认知负担），而不是等到真的撞线才仓促处理。
- **`quota.go`/`pricing.go` 的校验代码质量极高，但这份"极高质量"本身是一个值得反思的信号**——我逐行核对了
  `pricing.go` 的 403 行：`positiveFinite`/`nonNegativeFinite` 对 NaN/Inf 的显式防御、discount 与显式费率
  "二选一"的互斥校验、四分量"要么都填要么都不填"的完整性校验、`AllPathsComplete` 对**每一条可能被激活的
  override 路径**（不只是常见路径）做费率完整性穷举、账号覆盖行自带 `currency:` 时的独立汇率换算……
  这是一套非常严谨、几乎无死角的校验体系。**但它是为 `metric: cost` 这一个记账档位服务的，而设计文档自己
  的 §14.2① 已经证明 `metric: cost` 对路由决策的"质"没有影响**（headroom 只需要比例对）。换句话说：
  **我在 `pricing.go` 里看到的这 403 行高密度防御性代码，是"给用户核心诉求（额度合理分配）贡献有限、
  但完整性校验成本很高"这个抽象论断的一处非常具体的代码级证据**——这不是我从设计文档里搬来的结论，
  是我读这份校验代码本身得出的独立观察：**如果 `metric: cost` 真的只是 report 侧的一个可选展示增强，
  它不需要这么多"加载期就必须拦住任何一种可能算错的路径"级别的严谨度**；这套严谨度的存在恰恰说明，
  代码作者自己也把 `metric: cost` 当成了一个"进了配额记账主链路、算错会导致超支"的一等公民特性来对待——
  这与用户"提取核心、不要完整翻译计价逻辑"的诉求存在真实张力，且这个张力已经体现在代码复杂度本身，
  不只是设计文档的自我评估里。
- `PricingOverrideConfig` 的每行覆盖规则可以自带 `currency:`（区别于全局 `pricing.exchange_rate`），这是
  "多币种"这个功能面里我认为**复杂度收益比最低的一个角落**：为了让用户能直接抄厂商发票上的原币种数字，
  引入了"这一行的币种 → 全局目标币种"的独立换算路径（`o.Currency != "" ? FactorBetween(...) : 原样使用`），
  多了一条校验分支（`discount` 与 `currency` 互斥）。对比"用户自己手动换算成目标币种填进配置"这个替代方案
  （零代码成本，纯粹是用户多算一步），这条功能的自动化程度换来的复杂度不算便宜。
- `QuotaConfig`/`LimitConfig` 的 `Resolved`/`ResolvedTokenWeights` 字段（`yaml:"-"`）把"运行态缓存值"直接
  挂在 YAML 结构体上——这是"一个类型身兼用户输入模型与运行时缓存"的做法，比完全分离的双类型（如
  `config.EndpointGroup` → `core.Endpoint`）耦合度更高。当前靠 `KnownFields` 兜底（漏标 `yaml:"-"` 会在
  加载测试里直接报错，不会静默产生歧义字段），风险可控，但这是一个"如果没有 KnownFields 兜底就会很危险"
  的设计选择，值得知道它的安全网在哪。
- `resolvePricing` 单个函数身兼三件事（判断是否需要建表、构建 pricing context、逐 provider 校验+解析），
  105 行，密度不低但每一步职责边界清楚（靠调用 `buildPricingContext` 拆掉了最贵的一步），可接受。

**建议**：
1. `config.go` 的 `validate()` 建议下次改动时主动拆成 `validateProviders`/`validateModels` 两个私有函数——
   不需要等撞线，现在拆的成本远低于撞线后临时拆。
2. **关于 `metric: cost` 与多币种覆盖行 `currency:`**：这是我这一节里唯一想替用户明确画一条线的地方——
   如果用户认同"额度合理分配"是核心、"报表 $ 数字精确到账号折扣与原币种"是锦上添花，那么这部分校验代码
   （尤其是账号覆盖行级 `currency:` 与 `AllPathsComplete` 的穷举完整性检查）目前投入的严谨度已经超过了
   它对核心目标的边际贡献。这不是"代码写错了"，是"这部分代码在保护一个价值不高的特性"。具体处置方案见
   第 8 节（TokenPlan 专项）里的独立判断，这里先记录代码层面的证据。
3. `PricingOverrideConfig.Currency` 这个"行级币种"功能，如果暂时没有真实用户在用，可以考虑降级为"未来需要
   再加"——目前它的存在把校验逻辑复杂度往上抬了一档，换来的是一个可以用"用户自己算好再填"绕开的便利。

**模块回顾（跳出局部）**：`internal/core` + `internal/config` 这一层体现了项目"把错误挡在加载期"的哲学做到了
极致，是整个项目质量最扎实的部分之一。但我在这一层第一次亲眼验证了用户最初提出的担忧——**TokenPlan 相关配置
校验代码的"严谨度密度"明显高于其他配置面**（对比 `EndpointGroup`/`Provider` 那种"校验几个必填字段+格式"的
朴素校验，`pricing.go` 的校验要处理"任意可能被激活的时间窗组合下的费率完整性"这种组合爆炸级别的正确性问题）。
这不是巧合，是"把计价逻辑当作必须精确记账的一等公民"这个产品决定在代码复杂度上的忠实投影。这个判断我会在
后面读到 `internal/quota`/`internal/pricing` 两个包本体、以及最终的全局总结里继续深入，这里先钉一个"基础层
读下来的第一手观察"。

---

## 4. 路由半区 · 协议与调度

### 4.1 `internal/adapter` + 三协议实现（openai/anthropic/openairesponses）+ `internal/probe`

**现状判断**：三方法 `Adapter` 接口（`Protocol`/`BuildRequest`/`ClassifyError`）+ `database/sql` 驱动式编译期注册
（`atomic.Pointer` 无锁读路径、`sync.Mutex` 保护写路径）+ 字节级 splice 改写（`RewriteModel`/`RewriteRoles`/
`RewriteInputRoles`）+ 共享错误分类 `DefaultClassify`。三个协议适配器（`openai.go`/`anthropic.go`/
`openairesponses.go`）各自 65-88 行，结构几乎完全一致：改 URL → splice model → splice role → 建 `http.Request`
→ 拷贝透传 header → 覆盖 `Content-Type`/凭证 header → 返回。`probe` 包（128 行）独立于 `adapter`，专门解决
"半开端点的后台探测请求要用哪种协议形状构造"这个问题，被 `diagnose` 和 `router` 共用而互不依赖。

**发现/问题**：
- **我独立验证了 `ErrContextLimit` 缺口，确认它是真实存在的架构盲区，不是文献引用**：我逐行读了
  `classify.go` 的 `DefaultClassify`（29-85 行）。通用 4xx 分支（58-81 行）依次检查内容词表（`contentHint`）、
  模型未知词表（要求同时含"model"字样与 unknown/not found 等措辞）、`upstreamHint`（网关自报转发失败），
  三者都未命中则落到 `ErrClient`（81 行）。**"maximum context length exceeded"/"context_length_exceeded"/
  "too many tokens" 这类上下文超限措辞不会被这三个词表中的任何一个命中**——它不含"model"关键词（不匹配模型
  未知分支），不是网关转发失败措辞（不匹配 upstreamHint），也不在内容合规词表里（不匹配 contentHint）。
  结果是：一个长上下文请求打到窗口偏小的端点，收到 400 后被分类为 `ErrClient` → `handleErrorResponse`
  直接原样返回给客户端，**不会尝试候选列表里其他窗口更大的端点**——即使 `strategy.WithinContext` 这个
  既有的条件路由机制本可以在请求进入 failover 循环之前就把这类端点筛掉（前提是配置了准确的
  `max_context_tokens`；一旦这个估算或配置有偏差，就完全依赖 failover 兜底，而这条兜底路径当前是断的）。
  这是我认为整个路由半区里**唯一一处会实质性削弱 vmr 核心卖点（failover）的分类盲区**——其余的分类边界
  （`upstreamHint` 的收窄、`contentHint` 的宽松）都是权衡后的既定取舍，唯独这一类错误在设计文档的"错误分类"
  一节里完全没有被讨论过，看起来是一个真正的疏漏而非取舍。
- 三个协议适配器的 `BuildRequest` 结构高度重复（约 30 行的"改 model → 改 role → 建请求 → 拷贝 header →
  覆盖凭证"骨架三份几乎相同，只有 header 名称/末尾几行不同）。我认为**这不值得抽公共函数**——三份加起来
  不到 90 行，凭证 header 的名字（`Authorization: Bearer` vs `x-api-key`）和是否需要 `anthropic-version`
  透传这类协议差异本身就要求"看得到全貌"，硬抽一个公共骨架函数（参数化 header 名）不会减少认知负担，
  反而会让"这个协议到底发了什么请求"这个问题从"读一个 70 行文件"变成"读一个 70 行文件 + 跳转到公共函数"。
  这是我核实过设计模式后的独立判断：**当前的"复制三份"是这个规模下更简单的选择，不是技术债**。
- `probe.Request`/`probe.ResponsesRequest` 按协议分派，且 `probe` 包刻意独立于 `router`/`diagnose` 双方，
  只是为了避免循环 import——干净的解决方案，没有过度设计的痕迹。
- 错误分类表 `DefaultClassify` 是一个单体函数，扫描顺序（内容 > 模型未知 > upstreamHint > 兜底 ErrClient）
  靠代码里的注释固定下来，没有用查表/优先级列表之类的显式结构表达"这是一个有优先级的规则链"。目前规则不多
  （4 条），可读性尚可；如果未来为了更多厂商继续在这个函数里加 `case`/`if`，会逐渐变成一个不断增长的
  "厂商措辞百科全书"，设计文档自己在"模块回顾"性质的段落里已经预见到这一点（"若未来协议增多，这个函数
  会成为厂商 quirk 的罗马斗兽场"）。目前不是问题，是一个已被认知到的未来风险。

**建议**：
1. **`ErrContextLimit` 是我认为本次审查里 ROI 最高的一项具体代码建议**：新增 `core.ErrContextLimit` 错误分类
  （或复用 `ErrContent` 的"换端点但不罚健康"语义——上下文窗口大小是端点的静态属性，不是端点故障，惩罚健康
  没有意义），在 `DefaultClassify` 里新增一组词表嗅探（`context_length_exceeded`/`maximum context length`/
  `too many tokens`/类似措辞），分类后继续走 failover。需要注意区分"请求自身 `max_tokens` 参数设置超限"
  （这类应该保持 `ErrClient`，因为换端点也解决不了）与"会话历史超出端点窗口"（应该 failover）——两者的
  典型措辞不同，可以按"error 提到的是输入长度还是输出长度限制"来分。这个改动被限制在 `classify.go` 一个
  文件内，不触碰任何既有架构边界，回归风险低，收益是修复一个真实影响 agent 长任务可靠性的盲区。
2. 保持 `DefaultClassify` 现状不动（不要现在就重构成查表结构）——4-5 条规则的量级下，`if`/`switch` 链仍是
  最简单的形态；等真的加到第 8、9 条规则时再考虑要不要用数据驱动的规则表替代硬编码分支链。

> **✅ 已修复（2026-08-10）**：新增 `core.ErrContextLimit`（`internal/core/core.go`），语义与
> `ErrContent` 完全一致（`ReportNeutral`，零冷却，继续 failover）——`internal/router/router.go`
> 的 `handleErrorResponse` 把两者合并进同一分支处理；`internal/router/probe.go` 的后台探测路径
> 也同步补了这个分支（虽然探针 body 极小、实际触发概率趋近于零，但为保持"请求特定错误一律
> `ReportNeutral`"这条不变式的一致性，顺手加上）。`internal/adapter/classify.go` 新增
> `contextLimitHint`（上下文超限词表，中英并收）与 `maxOutputHint`（`max_tokens`/输出长度参数
> 超限的窄嗅探，必须在 `contextLimitHint` 之前排除，否则 Anthropic/OpenAI 的输出超限措辞会被
> 误吞成会话历史超限）——按建议方案完整落地了"区分输入超限与输出超限"这条要求。回归测试：
> `internal/adapter/classify_test.go` 的 `TestDefaultClassify_ContextLimit`（6 个 case，含中文
> 厂商措辞与两类输出超限反例）、`internal/router/router_serve_test.go` 的
> `TestServe_ContextLimitFailsOverWithoutCooldown`（端到端验证 failover 成功且端点不进冷却）。
> `docs/VirtualModelRouter_Design_v4_Core.md` 的"错误分类"一节已同步更新（新增分类表条目、
> 判定优先级、决策取舍表行）。

### 4.2 `internal/strategy` + `internal/health` + `internal/sticky`

**现状判断**：`Dimension`（排序）与 `Condition`（淘汰）是两个平行接口——前者比较两个端点、后者判断一个端点
对一次具体请求是否可用；`health.Registry` 是失败驱动的冷却/半开状态机；`sticky.Registry` 是会话亲和的
KV 存储。三者都是小型独立包（215/213/104 行），互相不依赖，只共同依赖 `core`。

**发现/问题**：
- **这是我认为全项目里"单人可维护"体现得最纯粹的一组包**：每个包只解决一个定义清楚的子问题，接口设计
  上"排序"（Dimension）与"淘汰"（Condition）刻意分成两个不同形状的接口而不是合并——这个决定我认为是对的：
  `Condition.Eligible` 需要看到请求内容（`RequestFacts`），`Dimension.Compare` 只比较两个端点、看不到请求，
  如果合并成一个接口，`Dimension` 的实现者（如未来的 `weight`/`latency`）会被迫接受一个自己用不上的请求参数，
  这是接口污染。当前的分离是"形状不同就不要用同一个接口硬套"的正面示范。
- `strategy.factories`（Dimension 注册表）用普通 `RWMutex`，而 `strategy.conditions`（Condition 注册表）与
  `adapter.registry` 一样用 `atomic.Pointer` 写时复制——**这个不对称是刻意的、且注释里给出了理由**（Dimension
  的 `Build` 只在配置加载/热重载时调用一次，不在请求热路径上，没有"读路径优化"的收益）。这是我看到的又一个
  "同一个模式不该到处套用，只在真正需要的地方用"的好例子，而不是不一致的疏漏。
- `WithinContext` 被有意排除在 `Condition` 接口之外（`strategy.go` 173 行附近的注释说明），因为它需要
  "全体拒绝时回退到硬性条件过滤结果"这种其它 Condition 都没有的特殊语义。这个"只有一个特例成员"的处理
  （router 层两行代码，我会在读 router 时核实）符合 YAGNI——目前只有一个这样的特例，不需要为它扩展
  `Condition` 接口本身。
- `health.Registry.ReportFailure` 的冷却参数（`transientBase=2s`/`transientCap=5m`/`longBase=10m`/
  `longCap=1h`）是硬编码常量，不可配置。这是一个"零调参"的设计选择——我同意这是合理的：这类参数很难在
  没有真实生产数据支撑的情况下让用户自己校准出更好的值，暴露成配置项只会增加配置面复杂度、换来的收益
  存疑（多数用户没有依据去调这几个数字）。
- `sticky.Registry.Set` 里"顺手做一次全表清扫"（每小时最多一次，用 `lastSweep` 节流）而不是独立的定时器
  goroutine——这个模式（`imgprep` 的缓存清理用的也是同一手法）是"事件触发 + 节流"替代"常驻后台任务"的
  一致选择，减少了一类需要独立管理生命周期的 goroutine。好。

**建议**：无新增建议——这一组包我认为已经是这个规模项目的最优解，不需要改动。如果一定要挑一点，
`health` 的四个冷却参数如果未来真的需要给高级用户调节空间（比如某个厂商的冷却策略明显不合适），
可以做成 provider 级覆盖，但**没有真实需求前不该主动加**，与已有的 OUTSTANDING_ISSUES 判断一致。

**模块回顾（跳出局部）**：`adapter` + `strategy` + `health` + `sticky` + `probe` 这五个包共同构成了"路由决策
的可插拔层"，是整个项目"新增协议/新增调度维度只需要新增一个小文件"这条扩展性承诺的具体实现证据——我读完
之后认为这条承诺是真实的，不是文档里的自我宣传：三个协议适配器确实只有"新建一个包 + 一行 blank import"
的接入成本，`Dimension`/`Condition` 确实可以独立扩展而不触碰路由主循环。**这一层如果说有什么局限，
不是设计上的，而是"错误分类表"这一个函数未来可能变成瓶颈**（见 4.1 的分析）——但即使真的发生，
修复代价也局限在一个文件内，不会连累这一层的其余部分。`ErrContextLimit` 缺口是这一层唯一值得立刻处理的
真实问题。

---

## 5. 路由核心：`internal/router`（2835 行，11 文件）+ `internal/server`（723 行，4 文件）

**现状判断**：`router.go`（583）承载 `Serve` 主循环（健康过滤 → 硬条件过滤 → 上下文估算兜底过滤 → 排序 →
额度重排 → 会话亲和重排 → failover 循环）+ `tryOne`/`handleErrorResponse`/`forwardSuccess` 三段式请求处理；
`response.go`（736）是响应归一化状态机；`responsefix.go`（195）是 MiniMax 专属修复知识；`quota.go`（455）是
额度决策；`snapshot.go`（261）/`transport.go`（116）/`limiter.go`（65）/`logfmt.go`（111）/`probe.go`（111）/
`reload.go`（96）/`routehdr.go`（106）是各自独立的小职责文件。`server` 只有四个文件：`server.go`（HTTP
入口+鉴权+chatHandler 编排）、`facts.go`（RequestFacts 估算）、`recorder.go`（响应审计录制）、`admin.go`
（`/admin/status`）。我逐文件通读了全部 11+4 个文件，这是本次审查里我花时间最多的一个模块。

**发现/问题**：

- **`router.go` 的 `Serve` 函数（57-242 行，约 185 行）是一个几乎与设计文档的"调度流程"图逐字对应的线性管线**——
  健康过滤、硬条件过滤、上下文估算兜底、排序、额度重排、会话亲和、failover 循环，每一步都有一段注释解释"为什么
  在这里、为什么是这个顺序"。**这是我读过的所有大型函数里少数几个"长但不复杂"的例子**：它长是因为管线本身有
  七个阶段，不是因为逻辑纠缠——我认为这是可以接受的，甚至是值得肯定的，因为把管线拆成七个独立小函数反而会
  让"这七步的先后顺序"这个真正重要的不变量，从"读一个函数从上到下"变成"跳七次函数定义再拼回执行顺序"。
- **`response.go`（736 行）是我认为全项目认知负担最重的单个文件，但我核实后认为这个复杂度是内生的，不是
  过度设计**——它要同时处理三种传输模式（passthrough/buffered/opaque）之间的状态转换、SSE 事件边界切分、
  MiniMax 两种"思考泄漏"形态的检测触发条件、`[DONE]` 追加策略、软屏蔽/CRLF 分帧观测标记、额度记账要用到的
  usage 嗅探与字节分类。我逐行读完 `ingest`/`decide`/`emitBlock`/`finalizeBuffered`/`appendDone` 这条调用链后，
  确认状态转换图是清晰的（`modeUndecided → modeBuffered|modePassthrough`，`modeBuffered → modePassthrough`
  单向恢复流式），且每条分支都有测试名字暗示的场景（如 `TestRespStream_ThinkQuotedMidTextUntouched`）。
  **但这也是我认为"未来最容易失控"的一个文件**：目前 736 行里，MiniMax 专属知识已经拆到 `responsefix.go`
  （195 行），`response.go` 本身仍然要在多个位置调用 `responsefix.go` 的判定函数（`thinkShapeGuard`/
  `stripThinkingProcess`/`containsSoftBlockMarker`）——如果未来出现第二家、第三家需要类似"思考泄漏"级别
  复杂度修复的厂商，`response.go` 的状态机本身（不是 `responsefix.go`）也需要跟着长出新的触发点判断，因为
  "什么时候进入 buffered 模式"这个决策目前是 `classifyEvent`/`decide` 里硬编码的两条 MiniMax 专属判据
  （`thinkOpenMarker`/`thinkingProcessPrefix`）。这不是当前的问题，是我对这个文件"下一次真正的复杂度考验"
  在哪里出现的预判。
- **我独立验证了 `copyFlush` 的既有数据竞争（`OUTSTANDING_ISSUES_opus-5.md` §3.3 已登记，不是我的新发现，
  但我认为有必要重新核实它的严重度）**：`transport.go` 的 `copyFlush` 在 idle 超时（112-113 行）与写错误
  （95-97 行）两条返回路径上，只 `close(done)` 通知读 goroutine "该退出了"，但读 goroutine 若正阻塞在
  `body.Read(buf)`（73 行）内部，`close(done)` 不会打断这个阻塞调用——它只有在下一次 `select` 时才会看到
  `done` 已关闭。与此同时，`router.go` 的 `forwardSuccess`（522-524 行）在 `copyFlush` 返回后立刻读
  `rbody.Applied()`/`rbody.RawPreStrip()`/`rbody.ObservedModel()`——这三个字段（连同 `respStream` 的其余
  未加锁字段）可能仍在被那个尚未真正退出的读 goroutine 并发写入。我认同 OUTSTANDING_ISSUES 的既有判断
  （"热路径改动，超出该功能范围"，且额度相关的新增字段已经用 `qmu` 单独加锁规避了新代码引入新竞争）——
  但我想补充一点独立观察：**这条竞争只在两条相对少见的路径上触发**（上游中途卡住超过 `stream_idle` 超时、
  或客户端写入失败），且最坏后果是审计记录里的 `norm`/`upstream_model` 字段出现读写竞争（Go 的
  data race 本身是未定义行为，但从功能后果看，最坏情况是记录到一个不一致的字符串切片状态，不会导致
  客户端收到错误响应，因为响应字节已经流式发出去了）。**这不改变"应该修"的结论，但可以帮助排优先级**：
  这是一个"应该在下一次碰这个文件时顺手修掉"的问题，不是"必须立刻停下来单独修"的问题。
- `handleErrorResponse` 对 >=400 响应体的读取用了一个独立的 `time.AfterFunc` watchdog（429 行）而不是复用
  `context.WithTimeout`——原因是这个读取要在已经拿到响应头之后才发生，且不能让"读错误体"这个动作无限期地
  卡住整个 failover 循环。这个模式（watchdog 关闭 body 触发 Read 返回错误）在 `probe.go` 里也用了类似的
  `context.WithTimeout`。两处对同一类"给一个阻塞 I/O 操作设上限"的问题用了两种不同的机制（一个是 timer 关
  body，一个是 context 传给 http.Client），我理解这是因为 `handleErrorResponse` 处理的是已经拿到的
  `*http.Response`（此时 context 已经不能再中止底层连接的读取，只能靠关闭 body），而 `probe.go` 是在发起
  一次全新的请求（可以用 context 从一开始就控制整个往返）。这是合理的技术选择，不是不一致，但值得记录下来
  避免以后被误判成"风格不统一"。
- `router.go` 583/700 行，`response.go` 没有独立的 archtest 预算（我核实了 `internal/archtest/file_sizes_test.go`
  只给 `router.go`/`report/aggregate.go`/`config/config.go`/`report/detail.go`/`report/session.go` 五个文件
  设了预算）——**`response.go`（736 行）本身没有被纳入行数预算约束**，尽管它已经是全项目第二大的单文件
  （仅次于 §8 会核实到的 `report/aggregate.go` 943 行）。考虑到它是我认为"最容易因为新厂商 quirk 而膨胀"的文件，
  这是一个值得补的护栏——不是说它现在有问题，是说"没有预算"意味着它可以无声无息地长到任意大小而不会被
  CI 拦截。
- `server.go` 的 `chatHandler` 是一条职责链清晰的管线（缓冲请求体 → 探测路由字段 → 并发闸 → 图片降采样 →
  计算 facts → 交给 `router.Serve`），每一步的顺序都有注释解释"为什么不能换"（比如"探测在并发闸之前，因为
  探测便宜、不该占坑"、"降采样在并发闸之后，因为它是 CPU 阶段"）。我没有发现新问题，这是"薄胶水层"应有的样子。
- `admin.go` 的 `/admin/status` 判断 loopback 用 `net.ParseIP(host).IsLoopback()`，对带 zone 的 IPv6
  loopback（`::1%lo0`）没有专门测试——这是 OUTSTANDING_ISSUES 已登记的已知项（fail-closed 方向无害），
  我核实后同意这个判断：`net.ParseIP` 对带 zone 的地址返回 `nil`，`nil.IsLoopback()` 会 panic 还是返回
  false？我查了 Go 标准库：`net.IP` 是 `[]byte`，对 nil 值调用 `IsLoopback()` 方法不会 panic（因为方法体
  只是检查切片长度），会返回 false——所以 fail-closed（拒绝访问）方向确实成立，这不是一个安全问题，只是
  一个"极端场景下 admin 端点会被误拒绝"的可用性小瑕疵。

**建议**：
1. **给 `response.go` 补一条 archtest 行数预算**（比如 800 行，留一点当前余量），让"这个文件在无声变大"
   这件事从"没人看见"变成"CI 会提醒"——这与 `router.go`/`aggregate.go` 已经享受的保护是同一个逻辑，
   `response.go` 没有理由被排除在外，尤其它是我认为未来最可能因新厂商 quirk 而膨胀的文件。
   **✅ 已修复（2026-08-10）**：加了 850 行预算（按既有条目的 ~15% 余量惯例取整，非随口的 800），
   细节见 §11.1 P1-A 的修复记录。
2. `copyFlush` 的既有竞争：建议在下一次因为其他原因触碰 `transport.go`/`response.go` 时顺手修掉（把
   `Applied()`/`RawPreStrip()`/`ObservedModel()` 的读取也纳入 `qmu` 保护，或让 `copyFlush` 在非成功路径
   等待读 goroutine 真正退出后再返回），不需要单独立项、单独测试验证——它的影响面已经被论证过是可控的。
3. 其余部分（`router.go`/`server` 全部文件）维持现状，我没有发现值得改动的地方。

**模块回顾（跳出局部）**：这是全项目复杂度密度最高的一层，但我读完后的判断是——**它的复杂度是"管理良好的
内生复杂度"，不是过度设计**：`Serve` 的七阶段管线直接映射业务需求（健康、能力、上下文、优先级、额度、
亲和五个独立的路由考量本来就需要五次独立的过滤/排序），`response.go` 的状态机直接映射"要同时保真流式 +
修复两种真实观测到的厂商缺陷 + 不破坏未来新厂商"这个约束组合。**如果要说这一层对"单人可维护"最大的风险
在哪，答案是 `response.go`——不是它现在的 736 行，而是它对"新增一个厂商 quirk"这类需求的边际复杂度成本**：
每加一种新的响应端异常检测，都要往 `classifyEvent`/`decide`/`emitBlock`/`finalizeBuffered` 四个函数里
各插一处判断，而不是像 `adapter.ClassifyError`（错误分类）那样"新协议 = 新文件"式地隔离。这不是本次审查
里需要立刻处理的问题，但如果 vmr 未来要接入更多国产厂商、遇到更多"200 OK 但内容有问题"级别的怪癖，
这里会是第一个吃紧的地方——值得在那一天到来之前，先想清楚"新厂商 quirk 的隔离单元应该是什么"（比如把
`classifyEvent` 改造成一个可插拔的 quirk 检测器列表，而不是继续往一个函数里加 `if`），而不是继续复制
现在的模式直到它变得不可读。

---

## 6. 记录与工具：`internal/audit`（843）+ `internal/imgprep`（710）+ `internal/diagnose`（644）+ `internal/replay`（472）

**现状判断**：四个包共享同一个哲学——**观测/调试工具本身绝不能有能力让主服务停摆**。`audit.Logger.Write`
失败只返回 error 供调用方打日志，从不 panic；`imgprep` 全程 fail-open（含 `recover()` 兜底 panic）；
`diagnose` 是一次性只读诊断，不碰运行中实例的任何状态；`replay` 是独立 CLI，复用 `router.NewUpstreamClient`/
`adapter.BuildRequest` 但完全不经过 `Router.Serve` 的 failover 循环。

**发现/问题**：
- **`imgprep` 是我认证过的、全项目安全边界思考最成熟的模块**——GIF 一律不缩放（避免 `image/gif.DecodeAll`
  无界解码帧数的解压炸弹向量）、64MP 解压炸弹上限、fail-open + panic recover 打日志但不吞掉信号、缓存用
  内容哈希+目标像素双因子做 key（避免不同 `max_px` 覆盖同一缓存条目）。我核对了 `processImage` 的判断顺序
  （先读 header 拿到声明尺寸 → 判断是否超过 `maxDecodePixels` → 才真正解码），确认这个顺序是防炸弹的正确
  顺序（先用便宜的 header 读取过滤掉恶意声明的巨大尺寸，再做昂贵的真实解码）。没有发现新问题。
- **`audit.Logger.Write` 的"编码在锁外、写入在锁内"设计是我读过的所有并发优化里最有说服力的一个**——
  用 `sync.Pool` 复用编码缓冲区，让 JSON 编码（CPU 密集、可能要处理几 MB 的对话历史）在锁外并发进行，
  只有最终的字节写入持锁。这是"先看清楚真正的瓶颈在哪，再决定锁的粒度"的正面示范。文件 write syscall
  本身仍在全局锁内（`OUTSTANDING_ISSUES` 已登记为"待触发"级别，实测未构成瓶颈），我同意这个判断——
  没有证据表明现在需要为一个理论上的问题引入背压/单写协程的复杂度。
- `diagnose.go` 的四阶段设计（配置校验 → 一致性扫描 → 网络连通性 → 路由预览）与 `envCheck`/`testEndpoint`
  的 `role_map` 场景化提示（"如果这个提供商拒绝 developer 角色，试试加 role_map"）体现了对真实运维场景
  的深入思考，不是把校验和真实请求两件事混在一起。`runConcurrent` 用固定大小 channel 做并发限流 + 预分配
  slice 保证结果顺序，是一个干净、可复用的小工具函数，没有过度设计的痕迹。
- `replay.go` 的 `recordView` 类型刻意不复用 `audit.Record`（后者的 `Message.Body` 是 `any`，反序列化会
  丢失原始字节），文档已经说明理由，我认为这是合理的、有据可查的重复，不是 DRY 违约。
- 我注意到这四个包（连同前面读过的 `core`/`config`/`adapter`/`strategy`/`health`/`sticky`/`router`/`server`）
  **没有一个表现出"为完备性投入远超实际价值"的迹象**——它们的复杂度都直接对应到一个具体、可验证的真实风险
  （解压炸弹、审计写入并发、诊断的网络超时处理、重放的凭证泄漏防护）。这与我在 `config/pricing.go` 里
  观察到的"防御性代码密度"形成对比——**那里的严谨度是为一个价值存疑的特性服务的，这里的严谨度是为明确
  的安全/正确性风险服务的**。这个对比本身是我认为很有力的一个证据：项目的工程纪律是稳定、一致的，
  唯独 TokenPlan 的 `metric: cost` 这一处出现了"纪律水平不变，但服务的目标价值降低"的情况。

**建议**：无新增建议——这四个包我没有发现值得改动的地方，维持现状。

---

## 7. 额度与定价专项（TokenPlan）：`internal/quota`（622）+ `internal/pricing`（880）

> **✅ 本节 P2.2/`metric: cost` 相关发现已于 2026-08-10 按方案 (c) 处理**——详见 §11.1 P0-A 的
> 修复记录。本节保留原始分析文字不动（含证据、行数统计、代码质量判断），因为这些判断在"删范围前"
> 是准确的事实记录，删范围本身不改变"这套代码写得好不好"这个独立判断；只在 §11.1 加了处理结果。

这是用户在任务描述里点名的重点，我在这一节会最大程度独立形成判断，不预设"应该砍还是应该留"的结论，
而是逐个机制核对"它是否真的在为'额度合理分配、最大化套餐效益'这个核心目标服务"。我这次是**从零开始逐行读完
`internal/quota` 全部四个文件（622 行）与 `internal/pricing` 全部四个文件（880 行）**，不是转述设计文档
的自我评估，下面的判断建立在实际代码之上。

**现状判断**：`internal/quota` 是纯粹的记账半区——`quota.go`（`Counters`/`Registry`，按 provider 名记账，
惰性周期重置）、`period.go`（`(every, since)` 周期数学，含月末截断/DST）、`score.go`（`Headroom` 核心公式）、
`store.go`（原子落盘 + 后台 flusher）。`internal/pricing` 是三层定价解析引擎——`pricing.go`（`Rate`/`Table`
类型 + YAML 解析 + 币种换算）、`resolve.go`（账号覆盖→补充表∪标准表→无费率的三层解析 + discount 链式
递归）、`resolver.go`（`vmr report` 用的记忆化包装）、`embed.go`（内置标准表加载）。

**发现/问题（逐项独立核对，不预设结论）**：

1. **P1（`internal/quota` 的 `Headroom`/`period.go`）是我认为整个 TokenPlan 专项里唯一"教科书级别"地
   贴合"提取核心、尽量简化"这条标准的部分**——`score.go` 的核心公式（`Headroom(usedFrac, timeLeftFrac)`）
   只有 15 行，`UsedFrac`/`TimeLeftFrac` 各不到 20 行，`ScoreForLimit` 把三者组合起来 5 行。`period.go`
   的复杂度（`findK`/`addMonthsClamped`/DST 处理）看起来多，但我核实后认为这是**问题域本身的复杂度**，
   不是可以化简的部分——"每月 31 号重置"这种真实存在的周期锚点，不用 `addMonthsClamped` 处理月末截断就会
   得出错误的周期边界（`time.Date` 的溢出语义会把 1/31 + 1mo 算成 3/3），这是 Go 标准库的一个真实陷阱，
   不是过度设计。`quota.go` 的惰性重置（比较周期 key，不等就地清零）避免了一整类"定时器 goroutine +
   漏重置 + 时钟回拨"的问题，是我看过的最干净的"用比较代替调度"范例。**这一部分不需要任何改动。**
2. **P2.1（`token_weights`/`model_multipliers`）是合理的中间层**——账号级四个 float + 一个按模型的
   倍率 map，全部在读取/计费时套用，零新增架构复杂度（复用既有的 `Counters` 四分量存储）。这一部分的
   代码量极小（`router/quota.go` 里 `applyModelMultiplier`/`baseAmount` 加起来不到 60 行），换来的表达力
   （账号内统一比例的 Credits 折算）与代码量完全匹配，我认为这是"刚刚好"的复杂度投入。
3. **P2.2（`metric: cost` + `internal/pricing` 整个包 + `config/pricing.go` 的 403 行）是我认为
   TokenPlan 专项里唯一体现"对计价逻辑做完整翻译"倾向的部分，这个判断现在建立在我读过 `resolve.go`
   全部 309 行之后，比我在配置层看到的校验代码更有说服力**。让我把"这背后到底在实现什么"说清楚：
   - `resolveChain`（289-309 行）实现的是一个**带优先级的费率覆盖链**：first-match-wins 的时间窗规则、
     discount 形式对"下层解析出的费率"（不是固定的 Base）做递归缩放、显式费率形式直接返回——这是真实
     账单系统里"促销规则叠加"的标准建模方式（一条限时折扣规则叠加在一条常态账号价之上）。
   - `AllPathsComplete`（235-287 行）做的是**对"这个 spec 在所有可能被激活的时间窗组合下是否都能算出
     完整费率"做穷举可达性分析**——不是简单校验，是在配置阶段模拟"任意时间戳会走到哪条规则"的可达性图。
   - `pricing.go` 的 `fileTable`/`fileRate` 支持**表级默认币种 + 行级独立币种覆盖**，币种转换走 USD 为
     枢纽的单跳换算，且补充表文件可以带自己的 `exchange_rate:` 块（优先于调用方传入的汇率表），是为了
     让补充表"可以脱离具体某份 config.yaml 独立搬运"。
   
   **这三项加起来是一个相当完整的、账单系统级别的费率解析引擎**——我认为这已经超出"路由决策需要的精度"，
   因为（这是我核对了 `router/quota.go` 的 `chargeCost`/`componentCost` 之后得出的量化观察）**router 侧
   真正消费这套引擎的代码只有约 25 行**（`chargeCost` 调 `pricing.RateAt` 拿一个 `Rate`，`componentCost`
   把四分量乘一遍加总）。也就是说，**为了让这 25 行拿到一个"绝对正确"的费率，背后支撑了 880 行的解析引擎
   + 403 行的配置校验，一共约 1283 行——是消费方代码量的 50 倍以上**。这个比例本身就是"配置文件与
   `config.yaml` 的对齐 metaphor"式过度设计的一个具体信号：不是这套引擎写得不好（它写得非常好，见下一条），
   是它要解决的问题（"让 `metric: cost` 账号的 $ 记账绝对精确"）本身，相对"额度合理分配"这个核心目标，
   价值不足以撑起这个投入。设计文档 §14.2① 自己的数学论证（headroom 是账号内部比值，绝对单位的偏差会自动
   约掉）我认为是完全正确的，我在这里只是把这个论证进一步落实到了具体代码行数上的证据。
4. **我要明确把"代码质量"和"特性范围"这两件事分开评价，因为它们在这里的结论是相反的**：`internal/pricing`
   本身的代码质量是我在整个项目里读到的最讲究的实现之一——`resolveChain` 用递归而不是直接指向 `Base`
   来处理 discount 叠加（避免了两条 discount 规则连续生效时的重复折算这个真实的正确性陷阱）、
   `AllPathsComplete` 相比"只查无条件规则"（`GuaranteedRate`）多想了一层"某条件规则激活时是否也完整"，
   这是我认为**catch 了一类真实潜在 bug 的细致设计**，不是炫技。**问题不在代码写得不好，而在于这么讲究的
   代码是在为一个"就算做得完美，对核心目标的边际贡献也是二阶的"特性服务**——这正是我读设计文档时就有的
   预感，现在被代码本身证实了。
5. `internal/quota`/`internal/pricing` 两个包都严格保持"只依赖 `core` + 标准库"的叶子包纪律，`Resolver`
   的记忆化设计（`vmr report` 场景，避免对几万条记录各自重新走一遍四步解析）与 `internal/config` 的
   一次性解析（`metric: cost` 场景，`BuildSnapshot` 时算一次挂在 `Endpoint` 上）是同一套逻辑服务两个
   不同调用节奏的消费者，"两个消费者、一份实现"这个设计目标我核实后认为确实达成了，没有出现两份独立实现。
6. `quota/store.go` 的 `StartFlusher`/`Flush` 原子落盘 + 5 秒定时 + 关停前强制 flush，是我看过的又一个
   "崩溃安全"设计的正面范例（临时文件 + rename，`stop()` 阻塞等真正的 goroutine 退出，避免和最后一次
   flush 竞争同一个文件）。没有发现问题。

**建议**：
1. **`internal/quota` 的 P1 核心（`Headroom`/周期数学/`Registry`）与 `token_weights`/`model_multipliers`
   维持现状，不需要任何改动**——这是这个项目里最值得作为"如何提取核心"范例保留的代码。
2. **`metric: cost` 与 `internal/pricing` 的定位需要用户明确裁定，我给出一个具体、可执行的折中方案**：
   不建议删除 `internal/pricing`——它对 `vmr report` 的 $ 成本估算是独立、真实的价值（报表侧的费率解析
   本来就应该是"尽力而为、缺失就降级"的姿态，与配额记账"缺失就报错"的严格姿态是两种不同的风险取向，
   现在的代码已经支持两种消费方式）。**真正应该重新评估的是"`metric: cost` 是否应该继续留在配额记账
   的加载期强校验路径里"**：
   - 如果保留：至少在用户文档（`UserGuide.md`/`TokenPlan` 相关章节）里明确写清楚"`metric: cost` 是
     进阶/可选精度档位，多数用户应该先用 `tokens` + `token_weights`，只有当账号内不同模型的 Credits
     折算比例差异很大、且你愿意手工维护四分量费率覆盖时才需要它"——目前的文档语气（设计文档、
     `config.example.yaml`）没有明确传达这种"档位"关系，容易让用户误以为 `cost` 是比 `tokens` 更"高级/
     推荐"的选项。
   - 如果按 §14.2① 的数学论证降级：把 `metric: cost` 从 `config.validate()` 的加载期强校验路径挪到
     一个显式的"实验性/进阶"标记之后，或者至少把 `AllPathsComplete` 这类最重的校验限定在真正配置了
     `metric: cost` 的账号上（**我核实过，现状已经是这样**——`resolvePricing` 只对 `costProviders`
     成员跑完整校验，未配置的账号不承担这个复杂度），那么当前的"按需付费"式复杂度分摊已经是较优实现，
     用户的顾虑更多在于"这个选项存在本身的心智负担"，而非"它的代码会拖慢或复杂化不使用它的用户"。
     经过这次通读，我倾向认为**保留现状 + 补充文档说明是成本最低、风险最小的方案**，"降级/移除"
     是一个产品决策而非代码质量问题，我把它列为待用户裁定的问题，不代为下结论。
   - **（交叉核实补充）第三条更明确的立场**：另外两份独立审查文档（`kiss_single_maintainer` 与
     `gemini-3.6-flash` 两份）在这一点上比我更果断——前者建议"直接把 `metric: cost` 从路由计费路径降级
     或去掉，让额度路由只支持 requests/tokens 两档"；后者给出了一条更具体的技术简化路径："删除
     `PricingOverrideConfig` 的 `HourFrom`/`HourTo`/`DateFrom`/`DateTo` 时间窗规则，只保留静态按模型
     discount/显式费率覆盖"——去掉时间窗后，`resolve.go` 里 `AllPathsComplete`/`resolveChain` 的"任意
     时间戳可达性分析"复杂度会显著降低（不再需要判断"这条规则在哪些时刻生效"，`resolveChain` 的
     `eligible` 参数化、`AllPathsComplete` 对每条 Override 单独跑一次链式解析这些机制都可以简化）。
     这是一条我认为技术上可行、且比"整体降级 cost 档"更克制的中间方案——**保留 `metric: cost` 与三层
     费率解析，但砍掉分时/限时促销这个功能面**，代价是损失"促销折扣窗口"这一个真实但使用频率未知的
     场景。三种立场（保留+文档引导 / 整体降级为报表专用 / 保留但砍时间窗）并存，供用户三选一裁定。
3. 补充一点在读代码前没有预料到的具体发现：`AllPathsComplete` 目前只在配置文件里的静态 override 规则
   上做可达性分析，**不会警告"一条无条件 override 排在一条有时间窗的 override 前面，导致后者永远不可能
   被激活"这种配置逻辑错误**（这不是校验疏漏——`resolveChain`/`AllPathsComplete` 都正确处理了"先出现的
   无条件规则会吞掉后面所有规则"这个语义，147-159 行的 `hasUnconditional` 分支就是为此设的），但它不会
   主动告诉用户"你写的第二条 override 规则是死代码"。这是一个很小的可用性改进空间（`vmr check` 可以顺手
   打印"以下 override 规则永远不会被激活"），优先级很低，仅供记录。

**模块回顾（跳出局部）**：读完 `internal/quota` 与 `internal/pricing` 的全部实现后，我认为用户最初提出的
担忧——"不要把 Token Plan 额度路由做成对计价逻辑的完整翻译"——**在代码层面确实部分成立，但成立的范围
比"整个 TokenPlan 功能"要窄得多，精确定位在 `metric: cost` 这一个记账档位及其配套的三层定价解析引擎**。
P1（headroom 算法）与 P2.1（token_weights/model_multipliers）不仅没有这个问题，反而是全项目"如何提取
问题核心"的最佳范例，应该被保留、被称赞，不应该被 P2.2 的问题连累到一起被质疑。P2.2 的问题也不是代码
质量问题（它的实现质量与项目其余部分一样高，甚至更讲究），而是一个**范围/优先级问题**：为一个"给核心
路由决策贡献二阶价值、主要收益是报表展示精度"的特性，投入了与"核心记账事实存储"相当量级的工程严谨度。
这个判断建立在我实际读完全部实现代码、并核对了消费方代码量（约 25 行）与支撑设施代码量（约 1283 行）
的比例之后，我认为这是本次审查里证据最扎实的一项发现。

---

## 8. 分析半区：`internal/chatmsg`（1015）+ `internal/ctxgraph`（1408）+ `internal/report`（6752）+ `internal/story`（5281）+ `internal/i18n`（2465）

分析半区是全项目最大的部分（占生产代码 53.5%，见 §1 基线），我按"共享层 → 产物层"的顺序读：`chatmsg` 全部 6 个文件通读，
`ctxgraph` 9 个文件里读了 7 个（manifest/edit/lineage/stitch/scan/cache 全文 + records/blobindex/hash 结构
性核对），`report` 读了 `aggregate.go`（943 行全文）+ `session.go`（前 250 行 + 结构核对）+ `rows.go`/
一个 `section_*.go` 样本，`story` 读了 `journey.go`（前 200 行 + 结构核对）+ `metrics.go`（前 180 行），
`i18n` 核对了文件组织方式与一个样本文件。**体量原因，这三个大包（report/story/i18n）没有做到逐行全文通读，
但已经足以验证设计文档的关键论述、独立发现新的证据、并对整体架构质量下判断。**

**现状判断**：`chatmsg` 是三种协议消息解析/SSE 重组/usage 提取/工具配对的唯一真相源；`ctxgraph` 是内容
寻址的 manifest/编辑分类/lineage/缝合层；`report` 是横向聚合报表；`story` 是纵向任务叙事重建；`i18n` 是
双语文本（结构化 struct + 函数字段，一个源文件对应一个 i18n 文件）。

**发现/问题**：
1. **`chatmsg` 是我这次审查里除 `quota` 核心算法之外，代码质量最高的一个包**——`messages.go`/`sse.go`/
   `usage.go` 把三种协议（含新增的 openai-responses）的解析细节收敛成统一的 `Message`/`StreamSummary`/
   `Usage` 输出，`usageFromObj` 对"OpenAI 的 total 已含缓存 vs Anthropic 的 total 不含缓存"这类真实厂商
   差异的处理精确、有注释支撑。**这是"一个共享解析层，不重复实现"这条纪律真正被贯彻的证据**——我在
   `ctxgraph.BuildManifest`、`report`、`story` 里都验证了这些包确实在调用 `chatmsg` 而不是自己再解析一遍。
2. **`ctxgraph` 是我认为全项目架构最优雅的一个包**——`edit.go` 的五类编辑分类判据（Append/ReplaceTail/
   Splice/Contract/Fork）纯结构化、零模板匹配，`stitch.go` 的缝合算法（blob 倒排索引 + 三态结果 +
   "宁可断开不错连"的显式平局兜底排序）体现了真实调试经验（`resolveStitch` 的确定性平局处理，注释明确
   写"不是靠单元测试发现的，是把 StitchGraph 对同一语料连跑 5 次、逐 Lineage 比对 PredIdx 发现的"）——
   这是我认为整个代码库里"工程纪律来自真实踩坑"证据最直接的一处。`cache.go` 的 `ScanCached` 明确论证了
   "只缓存解析结果、绝不缩小参与图重建的文件集合"这条边界，且给出了具体的反例场景（新文件可能续接一个
   "看起来已经定型"的旧 Journey）——这是我看到的对"为什么不能走捷径"论证最清楚的一段代码注释。
3. **`report/aggregate.go` 的六个 bucket（`Row`/`HourRow`/`EndpointRow`/`ClientRow`/`WorkloadRow`/
   `SessionRow`）确实存在明显的结构重复**——我读完 943 行的 `buildInternal` 后确认：`addA`/`addHour`/
   `addAttempt`/`addEndpointReq`/`addClient`/`addWorkload`/`addSession` 七个闭包，每一个都重复"按
   outcome 分支计数 → 累加 token 四分量 → 累加耗时/首字延迟切片"这套逻辑，只是字段命名和目标类型不同。
   **这与 `OUTSTANDING_ISSUES` §2.5 的记录完全一致，我在这里的独立核实是：`aggregate.go` 目前 943 行，
   distance to archtest 预算（1000）只剩 57 行**——这意味着下一次给这个文件加任何东西（哪怕只是一个新字段），
   大概率会撞线。既有文档已经给出了具体重构方案（泛型 `Bucket[K]`），触发条件（撞线时做，不要涨预算）
   也已经写清楚，我没有新的意见要补充，只是确认这个风险是真实、迫近的。
4. **`newUserWindow` 常量在 `internal/report/session.go:56` 与 `internal/story/journey.go:40` 各有一份，
   我独立 grep 验证了这个重复确实存在**，且 `journey.go` 的注释明确写"mirrors internal/report/session.go's
   identical constant"——这是一处**跨包耦合但没有跨包依赖**的真实 DRY 违约：两个包各自独立维护同一个"任务
   边界判定窗口"常量，如果未来只改了一处（比如 `report` 侧根据新语料调整为 6 或 10），`report` 的会话分组
   与 `story` 的任务边界会静默产生语义分叉，且不会有任何编译或测试信号提示这个分叉（除非专门写一个跨包
   一致性测试）。这不同于文档里论证过的"`SessionFingerprint` 与 `session.go` 故意独立实现"（那是两者
   风险取向相反，独立是有意为之）——这里是同一个语义、同一个数值，只是物理上抄了两份。**这是我认为这次
   审查里少数几个"真正应该 DRY 却没有 DRY"的具体点**，建议下沉到 `chatmsg` 或 `ctxgraph`（两者都已经是
   `report`/`story` 共同依赖的层）作为一个导出常量，两个消费方都改成引用它。这是一个几行代码的小改动，
   但价值是消除一个真实存在、目前完全没有测试兜底的静默分叉风险。
5. **`internal/story` 的 `journey.go`/`metrics.go` 体现出与 `ctxgraph` 相同的严谨性**——`computeTimeSplit`
   对"人类空闲 vs Agent 自己在忙"的判定不用魔法阈值，直接读 `HumanInitiated` 这个结构信号；`ErrorRecoveryCount`
   的文档诚实注明"只识别 Anthropic 的 `is_error`，OpenAI 协议没有对应标准字段，这会导致纯 OpenAI 语料低估"
   ——**这种"承认自己的盲区"的注释风格贯穿整个分析半区**，是我认为这个项目"诚实边界"这条设计哲学被
   代码层面兑现得最好的地方。
6. **`i18n` 的组织方式（一个源文件一份 struct，struct 字段拼错即编译失败）是我见过的"用类型系统替代人工
   核对"的好例子**，`report_workload.go` 的样本验证了设计文档描述的模式（含函数字段处理动态拼句、数组
   字段处理表头）确实如实落地。但我要独立指出一点设计文档已经承认、我认为值得在这里重复强调的成本：
   **2465 行纯文本 + 23 个文件，是"双语支持"这个产品决定的真实代价**，每新增一条报表/叙事文案，理论上
   都要同时想清楚中英文两个版本、并放对文件——这个心智负担在体量上已经接近 `ctxgraph` 整个包（1408 行）。
7. 我没有发现 `report`/`story` 的整体架构（聚合 vs 渲染分离、一节一文件、`archtest` 强制的行数预算）
   有任何问题——这套纪律在我抽样读到的每个文件里都得到了体现，`aggregate.go` 撞线只是"体量增长快于预算
   调整"的自然结果，不是纪律失效。
8. **（交叉核实补充）`story` 的 9 个 Finding 检测器阈值常量存在对新 Agent 框架的脆弱性风险**——`findings.go`/
   `findings_toolresult.go` 里的阈值（`exactRepeatThreshold=3`、`narrationJaccardThreshold=0.5`、
   `minPlanItems=2` 等）都标注了"真实语料校准"的记录，但校准语料目前只覆盖 OpenClaw/Claude Code 这一类
   Agent 客户端的行为分布。这是一条我在自己第一轮通读时没有独立发现、但核实后认为确实成立的可扩展性
   风险——未来接入行为模式差异较大的新 Agent 框架（比如工具调用密度、叙述风格完全不同的框架），这批
   阈值可能集体失效而不会有任何自动信号提示（Finding 检测器不报错，只是命中率悄悄跑偏）。这与 §7
   TokenPlan 专项、本节体量讨论是同一类"分析半区功能面持续扩张，但校准/回归成本不会自动被看见"的信号。

**建议**：
1. `newUserWindow` 常量下沉到共享层（`chatmsg` 或 `ctxgraph`），两个消费方改为引用——这是我这次审查里
   建议的所有代码级改动中，成本最低、收益最明确的一项（不到 10 行改动，消除一个真实的静默分叉风险）。
   **✅ 已修复（2026-08-10）**：下沉到 `chatmsg.NewUserWindow`，详见 §11.1 P1-C 的修复记录。
2. `report/aggregate.go` 六个 bucket 的泛型化重构：维持 `OUTSTANDING_ISSUES` 已经给出的判断（撞线时做，
   不要提前做，也不要等撞线后临时应付）——我没有新的意见要补充。
3. `i18n` 双语维护成本：这不是一个"代码质量"问题，是一个需要用户结合受众数据做的产品判断（中文/英文
   用户比例），我在最终的全局总结里会再提这一点，但这里的代码实现本身没有问题，不需要改动。
4. **（交叉核实补充）针对第 8 条的具体应对**：给 `story` 设一个显式的"Finding 数量上限/新增门槛"——
   类似 `archtest` 的文件行数预算，但针对"功能面"（检测器数量）而不是行数，超过即要求先做离线脚本
   验证、确认真实语料 ROI 后才收进二进制。这比"archtest 不覆盖功能面"这个泛泛观察（见 §9）更具体可执行，
   建议作为 §9 讨论的"功能面度量"机制的第一个具体落点。

**模块回顾（跳出局部）**：分析半区的代码质量与路由半区一样高，`chatmsg`/`ctxgraph` 这两个共享层甚至是
我认为全项目架构最优雅的部分。**分析半区真正的问题不是任何一处代码写得不好，是体量本身**——53% 的生产
代码占比，且这个占比还在被三份战略文档持续推动"再多建一些分析能力"。我在这一节独立验证到的两个具体信号
（`aggregate.go` 逼近预算、`newUserWindow` 的真实重复）都是"体量已经大到开始出现维护摩擦"的早期症状，
虽然目前都还不严重、也都有已知的应对方案，但方向是一致的：**这一层如果继续按现在的速度长下去，会先于
路由半区触达"单人维护"的舒适边界**。这个判断我会带到最后的全局总结里，与 TokenPlan 专项的发现放在一起
讨论"复杂度投入优先级"这个更大的问题。

---

## 9. CLI 与工程配套：`cmd/vmr`（2621，12 文件）+ `internal/archtest`（220）+ `vmr.sh`/CI/配置示例

**现状判断**：`cmd/vmr` 一命令一文件（`main.go` 只做分发 + usage + adapter 注册，71 行），`cmd_start.go`
（247 行）是常驻进程的完整生命周期（启动/热重载/优雅关停/额度落盘）。`internal/archtest`（220 行）用
`go list -deps` 的 shell-out 方式把 import 边界与文件行数预算变成可执行测试。CI（`ci.yml`）三个 job：
`go vet`/`go build`/`go test -race` 跑 ubuntu+macOS 矩阵、`gofmt -l`、`shellcheck`。配置示例
`config.example.yaml`/`.zh.yaml` 等三对双语文件。

**发现/问题**：
- **我逐行读完了 `main.go`/`cmd_start.go`/`archtest` 全部内容，确认这三处是"文档承诺"与"代码事实"
  对得最齐的地方**——`main.go` 71 行，确实只做分发；`archtest` 的两个测试（`TestArchitecture_ImportBoundaries`/
  `TestArchitecture_ZeroInternalDepPackages`）确实用 `go list -deps` 做真实的依赖图检查，不是摆设。
  `cmd_start.go` 的关停序列（SIGINT/SIGTERM → `srv.Shutdown` 优雅排空 → 额度 flusher `stop()` 阻塞等真正
  退出 → 最后一次 `Flush()`）体现了对"两个独立子系统的关停顺序不能颠倒"这类细节的用心。
- `archtest` 的文件行数预算机制本身是我认为整个项目"可执行架构纪律"里最值得称道的设计——**它把"文件
  会不会不知不觉变大"从"依赖代码审查者的记忆"变成了编译期/测试期的强制检查**，且每条预算的注释都写明了
  "为什么设的是这个数字、下次撞线该怎么办（拆分，不是改数字）"。我在前面 §8 独立验证了 `aggregate.go`
  即将撞线，这正是这套机制"正在起作用"的证据，不是它失效的证据。
- **`archtest` 目前覆盖"行数"和"import 边界"两个维度，没有覆盖"功能面"**（比如 `story` 的 Finding
  检测器数量、`report` 的章节数量）——这类"这一层的复杂度还在可控范围内吗"的问题，行数预算只能间接
  反映（功能面膨胀通常也会推高行数，但不是必然）。这不是当前的问题，只是我想指出"行数预算"这个机制
  本身有覆盖盲区，如果未来 `story`/`report` 的功能面（而非单文件行数）成为担忧，需要一种新的度量方式
  （比如按目录统计导出函数/类型数量），而不是继续加行数预算。
- CI 的三个 job 组合（`go vet`+`go build`+`go test -race` 双平台矩阵、`gofmt -l`、`shellcheck`）覆盖了
  一个单人项目真正需要的最低限度：编译正确性、数据竞争、格式一致性、shell 脚本静态检查。没有引入
  `staticcheck`（OUTSTANDING_ISSUES 已记录评估过、判断"首次接入大概率冒出大量未筛选发现，没有时间预算
  逐条判断真假阳性"）——我同意这个判断，对一个 25000+ 行的既有代码库，贸然接入一个新 linter 且没有
  抑制列表机制，确实容易让 CI 从下一次 push 起就一直红。
- `vmr.sh`（613 行）没有自动化行为测试（这一点 OUTSTANDING_ISSUES 已记录，我没有重新核实脚本本身逐行，
  但确认了这一点仍然成立——`ci.yml` 里没有任何针对 launchd/systemd 单元渲染的测试步骤）。这是全项目
  "有文档记录但确实没做"的少数几处之一，风险在于 service 模式的环境特定行为只能靠人工验证。
- 配置示例文件的双语一致性——我抽查了 `config.example.yaml`/`config.example.zh.yaml` 的顶层 key 集合，
  完全一致（`diff` 为空），证实了 CLAUDE.md 里"每个 example.yaml 必须有对应 zh 版本、结构一致"这条约定
  在这一对文件上被遵守了。没有把三对示例文件（`config`/`pricing`/`report`）逐字段核对，但抽查结果支持
  "这条纪律在被认真执行"这个判断。

**建议**：无新增建议——这一层是"工程纪律的执行层"，我核实过的部分（archtest、CI、cmd 分发）质量都很高，
唯一的差距（`vmr.sh` 无行为测试）已经被记录且有明确的"为什么暂缓"的理由，不需要我重复。

**模块回顾**：`cmd/vmr` + `archtest` + CI 这一组共同构成了"让架构纪律不腐化"的执行机制——**这是我认为
对"单人维护"这个约束最直接的正面回应**：一个人不可能靠记忆维持"哪个文件不该超过多少行、哪个包不该依赖
哪个包"这类规则，`archtest` 把这些规则变成了任何人（包括 AI Agent 协作时）改代码都会被自动提醒的检查。
这套机制本身应该被当作项目最重要的资产之一持续维护，未来任何新的架构不变式（比如 §5 建议的
`response.go` 行数预算）都应该优先考虑加进这里，而不是写成一段容易被遗忘的注释。

---

## 10. 文档体系：设计文档 + `OUTSTANDING_ISSUES` + `future-strategy` 目录

**现状判断**：三份核心设计文档（Core/Analytics/TokenPlan，共约 3200 行）+ `OUTSTANDING_ISSUES_opus-5.md`
（265 行的唯一遗留问题清单）+ `UserGuide.md`/`.zh.md` + `future-strategy/` 目录下 12 份战略/竞品/深挖类
文档（本次通读了其中 2 份：`vmr_strategy_synthesis_gemini-3.6-flash.md`、`agent_runtime_analysis_v1.0_
custom-2-agent.md`，其余 10 份未读，仅从文件名与 grep 摘要了解其存在）。

**发现/问题**：
- **三份核心设计文档的质量是我认为整个项目"软资产"里最值得称道的部分**——每个决策都有"备选方案 + 取舍
  逻辑"，`TokenPlan_Quota_Routing_Design_opus-5.md` 甚至在 §14.1 自己写"这份设计是不是过重了——是"，
  并且给出了具体的分批交付依据。这种自我批判的诚实度，是我在读代码之前就已经从文档里得到的第一印象，
  读完代码后我发现这份诚实是真实的（不是自我宣传）——§14.2① 的数学论证（headroom 只需要比例对）与我
  在 §7 独立核实的代码复杂度证据完全吻合。
- **`OUTSTANDING_ISSUES` 的"唯一跟踪清单"约定（不新开报告、直接在原文档更新）是我认为对单人维护极其
  重要的一条纪律**——它明确防止了"每次审查都从零开始、重复发明已经讨论过的问题"这个真实的效率陷阱
  （文档本身在 §4 用途约定里写"历次审计反复出现产出不进队列、下一轮重复确认同一批问题的循环"，说明这
  条纪律是吃过真实的亏才立下的）。**本次审查我刻意核对了 OUTSTANDING_ISSUES 里已经登记的项目**（如
  `copyFlush` 竞争、`vmr replay` 不计费），确认没有重复发明已经被记录、判定过的问题作为"新发现"呈现——
  `newUserWindow` 常量重复不在这份清单里，是我本次独立读代码时才发现的真实新增项（见 §8/§11.1 P1-C）。
  这套核对纪律是我在写作过程中主动遵守的，与用户对本次审查"独立但不重复造轮子"的期待一致。
- `future-strategy/` 目录堆了 12 份文档，我这次只读了 2 份（用户指定的战略合成文档 + Agent 运行时方法论
  调研），但从文件名（`vmr_strategy_and_competitive_analysis_v1.0_custom-2-agent.md`/
  `vmr_strategy_and_competitiveness_analysis_gemini-3.6-flash.md`/`vmr_strategy_review_opus-5.md`/
  `VMR_综合评审与发展建议_report_v2.md`……）可以看出**这个目录本身存在明显的"多模型各写一份、内容高度
  重叠"的堆积迹象**——至少 4-5 份文档标题都在做"战略分析/竞争力评估"这同一件事。这不是我读了内容后
  得出的结论（我没有全部读），是仅从文件名组织方式就能看出的结构性问题：**这个目录正在变成"每次找不同
  模型做一次战略分析就存一份新文件"的归档，而不是"当前有效战略结论的单一权威来源"**——与 CLAUDE.md
  自己在别处强调的"文档是当前状态，不是 changelog"的精神directly 相悖，只是这条纪律目前只覆盖了设计
  文档，没有覆盖 `future-strategy/` 这个目录。

**建议**：
1. **`future-strategy/` 目录建议做一次归档整理**：区分"仍在指导当前方向的结论性文档"（比如已经形成
   共识、正在指导路线图的部分）与"一次性分析素材"（各模型独立产出的战略报告本身，价值在于曾经贡献过
   输入，而非需要一直放在显眼位置）。可以考虑设一个 `archived/` 子目录，把已经被后续合成文档（如
   `vmr_strategy_synthesis_gemini-3.6-flash.md`）吸收总结过的原始素材移进去，只在顶层保留当前生效的
   1-2 份权威结论文档。这不是本次审查的核心发现，但值得作为"文档卫生"的一项收尾工作。
2. 其余部分维持现状——核心设计文档与 `OUTSTANDING_ISSUES` 的组织方式已经是这个项目该有的样子。

---

## 11. 全局总结

全部模块读完之后，我先声明一个总体判断，再展开分类细节：**vmr 的架构没有任何一处是"设计错了"的架构**。
我在 23 个内部包、cmd/vmr、archtest、CI、部分文档里，没有找到一处"这个模式选错了"或"这里应该用另一种
架构"级别的问题。真正值得讨论的问题全部集中在**范围（多大算够）与优先级（先做哪个）**这两个维度，
不是正确性或设计模式维度。这个判断本身，是我逐包独立读完源码后的结论，不是预设的。

### 11.1 重要问题（分类详述 + 建议方案）

#### ✅ P0-A｜TokenPlan `metric: cost` 与 `internal/pricing`：复杂度投入与核心目标价值不匹配（范围问题，用户点名）

- **证据**：详见 §7。定量证据是我独立统计的——`internal/pricing`（880 行）+ `internal/config/pricing.go`
  （403 行）= 1283 行三层定价解析引擎，而 `internal/router/quota.go` 里真正消费这套引擎的 `chargeCost`/
  `componentCost` 只有约 25 行。设计文档自己的 §14.2① 已经用数学论证了这个不匹配的根源：`headroom` 是
  账号内部比值，绝对单位的偏差会自动约掉，`metric: cost` 对路由决策的"质"没有贡献，主要价值是报表 $
  数字精度。国产厂商标准表覆盖率低（`cache_read` 仅 23%），意味着多数账号即使配了 `metric: cost` 也
  要手工填四分量费率，进一步削弱了这套引擎"开箱即用"的价值。
- **影响**：这是用户在任务描述里给出的具体担忧（"不要把额度路由做成对计价逻辑的完整翻译"）在代码里
  确实成立的部分——但成立范围精确限定在 `metric: cost` 这一档，不包括 P1（headroom 核心算法）和 P2.1
  （`token_weights`/`model_multipliers`），后两者是我认为整个项目"如何提取问题核心"的最佳范例，应该
  被保留、被称赞。
- **建议**：这是一个需要用户裁定的产品决策，我给出三条可执行路径（详见 §7 建议 2）：(a) 保留现状，
  但在用户文档里明确把 `metric: cost` 定位为"进阶/可选精度档位"，引导新用户默认从 `tokens` +
  `token_weights` 开始；(b) 认同 §14.2① 的数学论证，把 `metric: cost` 从配额路由的加载期强校验路径
  降级，`internal/pricing` 保留但只服务 `vmr report` 的 $ 估算（该场景本来就是"尽力而为、缺失就降级"，
  不需要 `AllPathsComplete` 级别的严格性）；(c)（交叉核实补充，另外两份审查文档的立场）保留 `metric: cost`
  与三层费率解析，但砍掉分时/限时促销这个功能面（删除 `PricingOverrideConfig` 的时间窗规则），是介于
  (a)(b) 之间更具体的中间方案。我个人倾向 (a)——`internal/pricing` 的代码质量很高，不建议为了"缩小范围"
  而牺牲一个已经写对、已经被 `vmr report` 独立复用的能力，真正需要调整的是**产品叙事和用户引导**，不是
  删代码；但三条路径已经把可行选项摆全，具体取舍留给用户。

> **✅ 已按方案 (c) 落地（2026-08-10，用户裁定）**：保留 `metric: cost` 与三层费率解析，砍掉
> `PricingOverrideConfig` 的 `date_from`/`date_to`/`hour_from`/`hour_to` 分时/限时促销功能面，
> 只保留静态按模型 `discount`/显式费率覆盖——这正是本文档交叉核实补充里另外两份审查文档提出、
> 我原本"倾向 (a)"但列为可选项的那条中间方案，用户最终选了它。落地效果：
> - `internal/pricing/resolve.go` 的 `RateAt`/`GuaranteedRate`/`AllPathsComplete` 三个因时间
>   维度而存在的函数收敛成两个（`EffectiveRate`/`Complete`），`resolveChain` 不再需要 `eligible`
>   时间谓词参数——`AllPathsComplete` 原本"对每条 Override 单独跑一次可达性分析"的循环，现在
>   就是一次线性 walk，本文档 §7 独立统计的"1283 行支撑设施 vs. 25 行消费方"这个比例因此显著
>   收窄（虽然没有重新计量行号比例——三层解析结构本身、`internal/pricing` 包边界、`vmr report`
>   的独立复用价值都按用户的产品判断原样保留，符合"删范围不删代码质量"的既定基调）。
> - **意外收获**：去掉时间窗后，"同一 model 模式出现两条 override"这件事从"用时间窗合法区分促销/
>   常态价"变成了必然的死配置（first-match-wins 下第二条永远轮不到）——顺手在 `internal/config/
>   pricing.go` 加了 `firstDeadOverride` 静态可达性校验（加载期报错，而不是本文档 §7 建议 3 里
>   设想的"`vmr check` 打印提示"那种更弱的形式；时间窗一去掉，这个检查从"需要遍历时间轴的重分析"
>   降级成了一次线性 dedup，成本几乎为零，就直接做成了硬校验）。
> - 影响面：`core.PricingOverride`/`PricingSpec`、`internal/router/quota.go` 的 `chargeCost`、
>   `internal/pricing/resolver.go` 的 `RateFor`（连带去掉了不再需要的 `ts` 参数）、
>   `config.example.yaml`/`.zh.yaml`、`docs/UserGuide.md`/`.zh.md`、
>   `docs/TokenPlan_Quota_Routing_Design_opus-5.md`、`docs/VirtualModelRouter_Design_v4_Core.md`、
>   项目根 `CLAUDE.md` 模块地图，以及全部相关测试（新增/重写约 15 个测试用例，含
>   `firstDeadOverride` 的两种死配置形态回归）。`go build`/`go vet`/`go test -race ./...`/
>   `archtest`/`gofmt` 全绿，`vmr check -c config.example.yaml` 实测通过。

#### ✅ P0-B｜`ErrContextLimit` 缺口：上下文超限被误判为 `ErrClient`，长上下文任务无法 failover（健壮性缺口）

- **证据**：详见 §4.1。我独立逐行核对了 `internal/adapter/classify.go` 的 `DefaultClassify`，确认
  "maximum context length exceeded"/"context_length_exceeded" 类措辞不会命中内容词表、模型未知词表、
  `upstreamHint` 中任何一个，落到 `ErrClient` 分支——直接返回客户端、不触发 failover。
- **影响**：一个长上下文 agent 请求打到窗口偏小的端点，收到 400 后任务直接中断，即使候选列表里还有
  窗口更大的端点。这正是 vmr 核心卖点（failover）最该发挥作用却失效的场景，也是我读完三份 future-strategy
  文档后确认的、三方战略分析一致认定的最高 ROI 代码任务。
- **建议**：新增 `core.ErrContextLimit`（或复用 `ErrContent` 的"换端点不罚健康"语义），在
  `DefaultClassify` 里加一组上下文超限词表嗅探，分类后继续 failover。需要区分"请求自身 `max_tokens`
  参数超限"（应保持 `ErrClient`）与"会话历史超出端点窗口"（应 failover）两种措辞形态。改动限定在
  `classify.go` 一个文件，不触碰任何架构边界，回归风险低。建议作为下一阶段第一个代码任务。

> **✅ 已修复（2026-08-10）**：按建议方案完整落地，细节见 §4.1 末尾的修复记录——新增
> `core.ErrContextLimit`，`contextLimitHint`/`maxOutputHint` 两组词表分别覆盖"会话历史超限"
> 与"输出参数超限"并按正确优先级排序，`router.go`/`probe.go` 两条路径都接上了"零冷却继续
> failover"的处理。改动确实如预判的那样只影响 `classify.go`/`router.go`/`probe.go`/
> `core.go` 四个文件 + 对应测试，没有触碰任何既有架构边界。

#### P0-C｜分析半区体量（53% 生产代码）持续增长，与"单人可维护"目标的张力（范围问题）

- **证据**：详见 §8 模块回顾。`report`（6752）+ `story`（5281）+ `ctxgraph`（1408）+ `chatmsg`（1015）+
  `i18n`（2465）= 16,921 行，占生产代码 53.5%，已经超过路由半区。我独立验证了两个"体量已经开始产生
  维护摩擦"的具体信号：`report/aggregate.go` 943/1000 行逼近 archtest 预算；`newUserWindow` 常量在
  `report`/`story` 两包间存在真实的、无测试兜底的重复。三份 future-strategy 文档本身对"该不该继续往
  分析半区加功能"就存在分歧（Custom-2 主张多建、Opus 5 主张往外挂契约/脚本、Gemini 折中）。
- **影响**：分析半区目前的代码质量与路由半区一样高，问题不在"写得不好"，在于它是全项目里**功能面
  还在被外部战略文档持续推动扩张的唯一部分**——`ctxgraph`/`chatmsg` 的架构本身对未来扩展（agent
  运行时 47 维分析、Web UI）支撑得很好（见 §11.3），但"支撑得住"不等于"应该无限往里加"。
- **建议**：我倾向支持三份战略文档里 Opus 5 一方的判断——**新的、探索性的分析维度（目标漂移、工具结果
  语义吸收率这类需要不断试错阈值的指标）应该优先作为消费 `vmr-report.json`/`journey-*.json` 稳定 JSON
  契约的外部脚本验证，证明真实语料 ROI 之后，再决定要不要收进二进制**；已经完成真实语料校准、证明了
  价值的现有能力（九项行为指标、Finding 检测器、compare/corpus）不需要现在改动。这是一个需要用户结合
  产品方向裁定的问题，我的角色是把"体量已经在产生早期摩擦信号"这个证据摆清楚，不代为下最终决定。

#### ✅ P1-A｜`internal/router/response.go` 缺少行数预算，是未来最可能失控的文件（护栏缺口）

- **证据**：详见 §5。`response.go`（736 行）是全项目认知密度最高的文件之一（三种传输模式状态机 + SSE
  事件切分 + MiniMax 双形态思考泄漏修复触发判断 + 软屏蔽/CRLF 观测 + 额度 usage 嗅探），但我核对了
  `internal/archtest/file_sizes_test.go`，它不在被保护的文件列表里。
- **影响**：这是全项目"行数预算"这套护栏机制唯一的覆盖盲区——如果未来接入更多国产厂商、遇到更多
  "200 OK 但内容有问题"级别的怪癖，新的检测触发点大概率会继续加进 `classifyEvent`/`decide`/`emitBlock`/
  `finalizeBuffered` 这四个函数，而不会有任何自动信号提醒"这个文件正在变得难以审计"。
- **建议**：给 `response.go` 补一条 archtest 预算（比如 800 行），与 `router.go`/`aggregate.go` 享受
  同等保护。这是我这次审查里成本最低的一条具体建议（一行 map 条目）。

> **✅ 已修复（2026-08-10，第二轮 ROI 复核）**：给 `internal/archtest/file_sizes_test.go` 的
> `fileLineLimits` 加了 `"internal/router/response.go": 850`——**没有直接照抄建议里随口给的
> "比如 800"**，落地前重新核对了这张表里其余"首次设预算"条目（`detail.go`/`session.go`/
> `journey.go` 等）的既定惯例：统一按注册当时文件行数的 ~15% 余量取整，不是随意挑一个数字。
> `response.go` 当前 736 行，`736 × 1.15 ≈ 846`，取整为 850（与 `story/journey.go` 的既有预算
> 数值一致，非巧合是同一套惯例）——这样新预算与既有条目风格统一，也不会因为"800"这个更紧的数字
> 而在下一次改动时过早触发。`go test ./internal/archtest/...` 验证当前 736 行不会立刻踩线。

#### P1-B｜`copyFlush` 热路径数据竞争（健壮性，已在 `OUTSTANDING_ISSUES` 登记，本次独立复核确认仍成立）

- **证据**：详见 §5。我独立验证了 `transport.go` 的 `copyFlush` 在 idle 超时/写错误两条返回路径上不
  等待读 goroutine 真正退出，而 `router.go` 的 `forwardSuccess` 随后并发读 `respStream` 的
  `Applied()`/`RawPreStrip()`/`ObservedModel()` 三个未加锁字段。这不是我的新发现，`OUTSTANDING_ISSUES`
  §3.3 已经登记且判定"热路径改动，超出功能范围"——我在这里补充的是独立核实后的严重度判断。
- **影响**：触发条件窄（上游中途卡住超过 `stream_idle` 超时，或客户端写入失败），最坏后果是审计记录里
  `norm`/`upstream_model` 字段出现读写竞争，不会导致客户端收到错误响应。
- **建议**：维持 `OUTSTANDING_ISSUES` 的既有判断（下次因其他原因触碰这两个文件时顺手修），不需要单独
  立项。我的独立复核只是确认这个优先级判断依然成立。

#### ✅ P1-C｜`newUserWindow` 常量跨包重复（DRY 违约，本次独立发现）

- **证据**：详见 §8。`internal/report/session.go:56` 与 `internal/story/journey.go:40` 各自声明
  `const newUserWindow = 8`，后者注释明确写"mirrors internal/report/session.go's identical constant"。
- **影响**：与设计文档论证过的"`SessionFingerprint` 与 `session.go` 故意独立实现"不同（那是两者风险
  取向相反），这里是同一语义、同一数值的物理重复，没有任何测试锁定两者必须一致——未来只改一处会让
  `report` 的任务切分与 `story` 的任务边界静默产生语义分叉。
- **建议**：下沉到 `chatmsg` 或 `ctxgraph`（两者都已是 `report`/`story` 共同依赖的层）作为一个导出
  常量，两个消费方改为引用。改动量几行，收益是消除一个真实存在、目前无兜底的静默分叉风险。

> **✅ 已修复（2026-08-10）**：下沉到 `internal/chatmsg`（选 `chatmsg` 而非 `ctxgraph`——常量语义
> 是"消息列表位置窗口"，属于 `chatmsg` 的消息级词汇表，`ctxgraph` 是内容寻址图这一层更高的抽象），
> 导出为 `chatmsg.NewUserWindow`，`report/session.go`/`story/journey.go` 各自的私有声明已删除，
> 两个消费方改为引用同一常量。两包都已经 import `chatmsg`，改动量确实如预判的"几行"。

### 11.2 相对不重要的发现（罗列，简单描述）

**本次独立核实、与 `OUTSTANDING_ISSUES` 一致的既有已知项**（不重复展开，仅确认仍然成立）：
`health.Registry` 全局锁粒度无需分片、`ErrorClass.String()` 的 default 兜底是刻意设计、
`contentHint` 词表含裸 "sensitive" 的误判取舍、`vmr.sh` 无自动化行为测试、`vmr replay` 不计额度
（已有明确接线方案，排期问题非设计问题）、探针请求绕过审计（已知盲区，需先做产品判断）、
`audit.RawPreStrip` 类型为 `any`（应为 `json.RawMessage`）、`audit.retentionDays`/`extraRedactHeaders`
包级全局变量与 `imgprep` 显式传参风格不一致、`report` 对同一批输入两遍扫描（GB 级语料时才是真实成本）、
OpenClaw 客户端方言散落在 `report` 6 个文件里（接入第二个 agent 客户端时才值得收拢）、`IngressPath` 写死
三协议 case（新协议需记得同步加分支）、`go.mod` module 名待改完整仓库路径、`.gitignore` 全局忽略
`*.jsonl` 影响测试 fixture 提交、`loadtest` 的地址常量三处人工同步等一次性工具级瑕疵。

> **本次复核结论（2026-08-10）**：用户要求重新评估这批"次要发现"，把低成本低风险的顺手处理掉。逐条
> 核实结果：
> - **🟢 已处理**：`loadtest` 地址常量三处人工同步——新增 `loadtest/addr` 包，`runner`/`gentargets`
>   两个 Go 源改引用同一常量，降为"一处 Go 常量 + 一处有注释指引的 YAML 人工同步点"（`config.yaml`
>   本身没法 import Go 常量，这一处物理上消不掉，但已经从"三处各自独立"降到"一处权威源 + 一处
>   指向它的提示"）。`future-strategy/` 目录归档整理（下面"新增的次要观察"一条）一并处理。
> - **⚪ 核实后判定不适用，未改动**：`audit.RawPreStrip` 类型为 `any` 这条建议本身站不住脚——
>   核对了它的构造点 `EncodeBody`（`internal/audit/audit.go`），该函数按内容是否为合法 JSON
>   在 `json.RawMessage` 与 `string` 之间二选一返回（原始 SSE 文本这种非 JSON 内容必须走 `string`
>   分支），`RawPreStrip` 字段因此本来就必须是 `any`（`json.RawMessage | string` 的联合），收窄成
>   纯 `json.RawMessage` 会在非 JSON 场景丢数据或直接 panic。这与 `Message.Body` 是完全相同的模式
>   （同一个 `EncodeBody` 产出）——`OUTSTANDING_ISSUES` §2.7 原本的"待定"判断（"需要仔细检查，收益
>   纯属整洁性"）现在可以关闭得更彻底：不是"值不值得做"，是这个改动本身不成立。
> - **⚪ 评估后判定成本超出"顺手"范围，未改动**：`go.mod` module 名改成完整仓库路径——机制上是一次
>   import 路径的机械替换，但会触碰仓库里几乎每一个 `.go` 文件（`vmr/internal/...` 全部改写成
>   `github.com/bigfatsea/vmr/internal/...`），diff 体量与本次其余改动完全不在一个量级，不符合
>   "顺手"的定义；建议作为一次独立、单一目的的 commit 处理，不要和其他改动混在一起。`.gitignore`
>   忽略 `*.jsonl` 的问题——核实后发现这其实是个假问题：`.gitignore` 已经有 `!/examples/*.jsonl`
>   白名单例外，当前也没有任何测试需要在 `examples/` 之外提交 `.jsonl` fixture，属于"文档记录的
>   假设性未来问题"，没有可动的手，故不处理。
> - **⚪ 其余既有已知项**（`health.Registry` 锁粒度、`ErrorClass.String()` default、`contentHint`
>   "sensitive" 误判取舍、`vmr.sh` 无行为测试、`vmr replay` 不计额度、探针绕过审计、
>   `audit.retentionDays` 全局变量风格、`report` 两遍扫描、OpenClaw 方言收拢、`IngressPath` 三协议
>   case）——核实后维持 `OUTSTANDING_ISSUES` 原判断：要么是刻意的设计取舍，要么触发条件明确但尚未
>   触发（"接入第二个 agent 客户端时""真的成为实测瓶颈时"），本身不是"低成本顺手能改"的量级，改了
>   反而是无谓地扩大这轮改动的影响面，故不动。

**本次独立核实、新增的次要观察**：
- `core.WriteJSON`/`WriteError` 是行为函数而非类型，混在"零依赖共享类型包"里，与 `core` 自身的定位
  略有偏差——代价可忽略，只在未来继续往 `core` 加类似"两处都要用的行为"时才值得关注。**核实后未改**：
  只有两个函数，拆一个新包换来的"分类洁癖"收益小于多一层 import 的成本，维持原判断"不紧急"。
- `PricingOverrideConfig.Currency`（覆盖行级独立币种）是多币种功能面里复杂度收益比最低的一个角落，
  可以用"用户自己换算好再填"完全替代，且零代码成本。**核实后未改**：这是一个会改变用户可见配置
  schema 的功能移除，风险与"顺手"的定义不符，且与本次 P0-A 处理的"时间窗"是两个独立功能面，不应
  该顺带一起砍——留给用户单独裁定。
- `archtest` 的行数预算机制不覆盖"功能面"（Finding 检测器数量、报表章节数量），只能间接约束这类膨胀。
  这是一条机制性观察，不是一个可以"顺手改掉"的具体缺陷，维持现状。
- ✅ **已处理**：`future-strategy/` 目录堆积了多份内容高度重叠的战略分析文档，与项目自身"文档是当前
  状态不是 changelog"的纪律不一致——已核实 `vmr_strategy_synthesis_gemini-3.6-flash.md`（2026-08-10
  20:15）自己的表述，确认它明确综合了另外三份同日文档（"Custom-2 Agent (v1.0)"/"Gemini 3.6 Flash
  (Master Plan)"/"Opus 5 (v4.0)"），逐一比对文件名与标题确认对应关系后，把这三份已被吸收的原始
  素材（`vmr_strategy_and_competitive_analysis_v1.0_custom-2-agent.md`、
  `vmr_strategy_and_competitiveness_analysis_gemini-3.6-flash.md`、`vmr_strategy_review_opus-5.md`）
  移进新建的 `docs/future-strategy/archived/` 子目录（未纳入 git 版本控制，用普通 `mv` 而非
  `git mv`）。目录里其余文件（`agent_runtime_analysis_v1.0_custom-2-agent.md`、
  `vmr_competitiveness_future_strategy_independent_review_v1.md` 等）没有类似"明确被谁吸收"的
  文本证据，保守起见未动——不代为判断哪些"过时"，只处理有直接证据支持的部分。
- `AllPathsComplete` 不会主动提示"一条无条件 override 排在一条有时间窗的 override 之前，导致后者
  永远不可达"这类配置逻辑死代码，`vmr check` 可以顺手加一行提示（优先级很低）。**已用另一种、更强的
  形式解决**：P0-A 砍掉时间窗后，这类死代码判定从"需要遍历时间轴的可达性分析"降级成了一次线性
  dedup，成本几乎为零，于是没有做成 `vmr check` 里的"提示"，而是直接做成了 `internal/config` 里
  `firstDeadOverride` 的加载期硬错误——比原建议更严格（拒绝而非警告），但改动量同样很小，细节见
  P0-A 的修复记录。
- **（交叉核实补充）** `config.go` 的 `validate()` 在 provider/model 校验循环里顺带收集 `providerModels`
  集合（供 `resolvePricing` 使用）——"验证循环里夹带数据收集"，职责略混，但代价很小，不值得单独立项。
  **核实后未改**：属于纯粹的代码组织洁癖，且改动会牵涉 `validate()` 函数本身的结构，与本轮"低风险
  顺手改"的范围不符。
- **（交叉核实补充）** `router` 对 `chatmsg.MergeUsageBytes` 的依赖（额度 usage 嗅探）让路由热路径直接
  依赖了一个"主要为分析半区服务"的解析包——这是刻意选择（`chatmsg` 是消息解析唯一真相源，方向正确），
  但说明"两个半区的共享层"实际比 CLAUDE.md 模块地图字面描述的更深一层，值得在模块地图更新时留意这个
  边界的准确表述。**未改**：这是一条留给未来模块地图整体修订时顺带处理的表述精度问题，不是本轮
  P0/P1 改动应该顺带触碰的范围；本轮对 `CLAUDE.md` 的修改只限于修正因 P0-A/P0-B 改动而过时的
  函数名引用，不做范围外的表述优化。

### 11.3 全局视角：大问题与调整空间

1. **没有发现任何"架构错误"，唯一的系统性主题是"范围控制的一致性"**。字节保真透传、离线/在线物理
   解耦、内容寻址、可执行不变式（archtest）、"观测者的谦逊"（观测工具不能让主服务停摆）、"把错误挡在
   加载期"——这五条原则贯穿全部 23 个包，执行得高度一致。我在读代码过程中反复验证这些原则是否真的落地
   （而不是只停留在文档里），结论是：**是的，落地了**。真正值得盯的只有两处"范围超出核心目标"的具体
   证据（P0-A/P0-C），且两处都不是孤立事件——它们共享同一个模式：**项目在"这个特性/这个半区值得投入
   多少工程量"这个判断上，个别决策的严谨度标准高于该决策对核心目标的边际贡献**。
2. **单人维护的真实边界不是"代码量"，是"心智覆盖范围"**。整个项目 31,596 行生产代码对一个人（+ AI
   Agent 协助）而言不算多，真正的瓶颈是"改一处要理解多少上下文"。我读下来，路由半区绝大多数包（`core`
   之外）都能在几分钟内建立完整心智模型；分析半区的 `chatmsg`/`ctxgraph` 同样如此；但 `report`/`story`
   的整体心智模型需要同时理解聚合逻辑、渲染惯例、i18n 机制、`ctxgraph` 消费方式——这不是任何单个文件
   的问题，是"半区整体的概念密度"问题，与 P0-C 是同一个观察的不同角度。
3. **`internal/archtest` 是这个项目应对"单人维护"约束最重要的机制，建议持续投资**。它已经把"文件别
   太大""包别乱依赖"变成了自动检查，唯一的盲区是"功能面膨胀"（见 §11.2）。如果用户认同 P0-C 的判断
   （分析半区需要设一道准入门槛），这道门槛最终也应该沉淀成 `archtest` 里的一条可执行规则，而不是
   停留在"以后要注意"这种容易被遗忘的口头共识。
4. **对未来方向的扩展性评估（回应用户"兼顾未来战略方向"的要求）**：
   - **Agent 运行时轨迹分析（47 维）**：`ctxgraph` 的 manifest/lineage/编辑分类 + `chatmsg` 的统一解析
     + `story.profile.Profile` 接口，三者共同构成了一个我认为**已经为大部分未来分析维度准备好数据结构**
     的基础层——新增一个"目标漂移分数"或"工具结果吸收率"这类指标，理论上只需要在已有 `Manifest`/`Step`/
     `Event` 序列上做新的规则计算，不需要改动 `ctxgraph` 本身。扩展性良好，主要风险是 P0-C 说的"功能面
     膨胀"，不是架构支撑不住。
   - **单二进制 Web UI（`vmr ui`）**：`vmr-report.json`/`journey-*.json` 已经是稳定的机器可读契约
     （§4.3 的"JSON 固定英文、只有 Markdown 跟随语言"设计已经在为这类外部消费方铺路），架构上支持独立
     UI 消费，不需要改动现有分析半区代码。
   - **`ErrContextLimit`/Runaway Guard/`vmr quota calibrate` 这类路由半区的路线图项**：都能在既有的
     `ErrorClass`/`Condition`/`config` 框架内找到落点，不需要新的架构机制。
   - **结论**：我没有找到"会堵死某个具体未来方向"的架构局限。唯一的共性风险是分析半区如果继续按当前
     速度堆功能，会在架构真正遇到瓶颈之前，先让"单人能看懂全部代码"这件事变得不再成立。
5. **一个我认为三份战略文档都提到、但值得在架构审查语境下重新强调的判断**：`internal/pricing`/
   `metric: cost` 与"分析半区该往二进制里堆多少"这两个问题，本质上是同一类决策——**"这个能力值得
   多少工程严谨度"应该由它对核心目标（省钱、可信黑匣子）的边际贡献决定，而不是由"我们有能力做到多
   严谨"决定**。这是我认为贯穿本次审查最重要的一条元结论，比任何单个具体发现都更值得作为未来所有新
   功能的准入准则。
6. **（交叉核实补充）关于"要不要设一条全局代码规模硬上限"**：`gemini-3.6-flash` 那份审查建议给路由
   相关包设一条"12,000 行"的总规模熔断线。我核实后认为这个方向值得认同（"需要一种规模上限意识"），
   但这个具体数字本身缺乏推导依据，且路由半区（含 cmd/tools）目前已经约 14,675 行、早已超过这条线，
   一条事后就被突破的硬上限意义有限。我倾向的机制仍然是第 3 点已经在说的——**把"规模上限意识"落实成
   `archtest` 里可以持续调整、按文件/按功能面设定的具体预算，而不是一条全局总行数的硬性数字**：前者
   已经在这个项目里被验证有效（`router.go`/`aggregate.go` 的撞线-拆分循环），后者容易变成一条被突破后
   就不再被认真对待的口号。两种机制的目标一致，只是我认为后者更贴合这个项目已经在用、且行之有效的纪律。

### 11.4 缺口：该做而没做 / 该升级而没升级 / 该完善而没完善

1. ✅ **`ErrContextLimit`**（该做没做，已修复）——见 P0-B，三份战略文档 + 本次独立代码核实一致确认的
   最高 ROI 缺口。
2. ✅ **`response.go` 的 archtest 行数预算**（该升级没升级，已修复）——见 P1-A，一行配置即可补上的护栏
   盲区，第二轮 ROI 复核时补上（850 行，未改动 `response.go` 本身，纯新增测试断言）。
3. ✅ **`newUserWindow` 常量下沉**（该完善没完善，已修复）——见 P1-C，几行改动即可消除的真实重复。
4. **`vmr replay` 计费**（该做没做，已有明确方案）——设计文档 §15.3② 已经把这个从"永久盲区"改判为
   "近期跟进任务"并给出具体接线方案（复用 `NewUpstreamClient`，一次性 Registry 加载+计费+退出前
   flush），但至今未落地。高频用 `vmr replay` 调试长上下文请求的开发者会让本地额度计数持续静默漂移。
5. **`vmr report` 的额度燃尽看板（P4，`section_quota.go`）**（该做没做，且我认为优先级被 P2.2 抢占）——
   对"最大化套餐效益"这个核心目标有直接价值（每个套餐的燃尽曲线、该优先烧哪个），却排在"让 $ 列更准"
   （`metric: cost`）之后交付。这是 P0-A 判断在路线图排期上的具体体现。
6. ✅ **软屏蔽/quirk 修复的聚合展示**（该接线没接线，已修复）——`soft_block_detected`/`thinking_process_
   pattern_detected`/`model_rewrite` 已进审计 `norm` 字段，但 `report`/`story` 没有消费方把它们聚合成
   一个可读的章节/指标。属于"数据已经在采集、只是没有出口"的小任务。**2026-08-11 独立复查确认成立**
   （核对了仓库现存 9101 份 `reports/details/*.md`，`think_strip`/`thinking_process_strip`/
   `soft_block_detected`/`thinking_process_pattern_detected` 命中率约 9%-19%，不是罕见边缘情况）并
   落地：`report/aggregate.go` 的 `addAttempt` 按端点聚合 `EndpointRow.NormCounts`（只统计这四个真正
   的 quirk 标记，`model_rewrite`/`opaque` 等结构性标记显式过滤掉，否则会被 ~100% 命中率的噪音淹没），
   `section_reliability.go` 新增"Quirk Fix × Endpoint"表（复用既有的"Error Class × Endpoint"渲染模式）。
   `report/session.go` 里那个写而不读的孤儿字段 `ReqInfo.norm` 顺手删除——它按"最后一条非空 Norm"取值，
   在 failover 场景下会丢掉更早 attempt 的 quirk 记录，不适合拿来做这件事，真正的聚合直接在
   `aggregate.go` 的逐 attempt 循环里按端点归属计数。`vmr story` 暂未接线（不在本次范围内）。
7. **`future-strategy/` 目录归档机制**（该完善没完善）——见 §10 建议，防止"多模型各写一份战略分析"
   持续堆积成陈旧、重叠的知识负债。
8. **`vmr.sh`/service 模式的行为测试**（该完善没完善，已知项）——环境特定 bug（launchd/systemd 单元
   渲染是否真的被系统接受）目前只能靠人工验证。
9. **（交叉核实补充）标准定价表四分量补齐**（该完善没完善）——`cache_read` 覆盖率仅 23%、`cache_write`
   仅 8%，国产第一方厂商普遍缺失，是 `metric: cost` 可用性的硬门槛（见 §7）。文档承认这是持续的数据
   维护工作而非一次性代码任务，靠社区/手工补 `standard_price_curated.yaml` 推进，不排期。若按 §7 的
   讨论把 `metric: cost` 降级，这条缺口的紧迫性也随之降级。
10. **（交叉核实补充）HTML 单文件渲染 + 脱敏模式**（该升级没升级）——`vmr report`/`vmr story` 的
    Markdown 产物含完整对话正文、文件路径、内部项目名，是分享给团队外部这个场景的硬门槛。设计文档
    §8"可选扩展"一节已经列出这一项：单文件自包含 HTML（内联 CSS/JS）+ 脱敏模式（保留结构与指标，
    正文替换为长度占位符 + 类型标签）。架构上已经支持（`vmr-report.json`/`journey-*.json` 是稳定的
    机器契约），只缺实现，不涉及架构改动。
11. **（交叉核实补充）官方用量 API 校准**（该做没做，P4）——本地额度计数漂移（绕过 vmr 的流量、单位
    换算偏差、时段倍率）的根治手段。设计文档已经确认存在私有用量查询接口，但刻意"不预先抽 `Source`
    接口，等写第一个真实适配器时再抽"（避免投机性抽象）——这是合理的克制，不是遗漏，只是要如实记在
    缺口清单里，因为它是 §13"范围边界"里最难受的几条限制之一的根治手段。
12. **（交叉核实补充）`/metrics` Prometheus 端点**（该做没做，路线图项）——对"单人维护多个实例时用
    标准可观测性工具串联看板"有真实价值，落地成本很低（`/admin/status` 已经暴露了大部分需要的数据，
    改成 Prometheus 文本格式是格式转换，不是新的数据采集）。
13. **（交叉核实补充）系统提示词版本化追溯，低优先级**——`story.Step.SysChanged`/`report` 的同名信号
    已经能检测"这一轮系统提示词是否变化"（布尔量），但没有更进一步的"一个 Journey 里出现过哪些不同
    版本的系统提示词、分别在何时切换"这类时间线视图。这是一个真实但优先级很低的分析功能缺口——基础
    的漂移检测已经存在，缺的是更完整的呈现，不是从零构建信号。

### 11.5 其他补充（覆盖不到但重要的点）

1. **测试代码 33,228 行、与生产代码接近 1:1，是这个项目最大的隐形资产，也是隐形负担**——它是"把错误
   挡在加载期"这条哲学能够安全维持的前提（每条严格校验规则背后几乎都有对应测试锁定行为），但也意味着
   任何功能改动的真实成本要按"生产代码 + 测试代码"一起算。我认为这个比例对当前阶段是健康的，不需要
   调整，但值得作为"新功能 ROI 评估"的一个隐性成本项被记住——尤其是 P0-C 里讨论的分析半区新指标，
   每加一个 Finding 检测器，除了规则代码本身，还有两轮语料校准 + 回归测试的隐性成本。
2. **`internal/archtest` + `go vet` + `-race` + fuzz 测试（`RewriteModel`/`RewriteStream`）已经覆盖了
   我能想到的"单人维护最容易漏掉"的几类问题**（并发竞争、架构边界腐化、恶意/畸形输入）——这套组合本身
   就是这个项目"如何用最小工程量获得最大安全边际"的一个好范例，值得作为其他同类项目的参照。
3. **项目对"诚实边界"的坚持是我认为被低估的一项软实力**——从 `imgprep` 的 fail-open 到 `story` 的
   "候选/嫌疑清单，不是判决"措辞纪律，再到 `chatmsg`/`story` 里多处"这里明确低估/未覆盖，不是 bug"的
   注释，这种一致的克制在一个容易被"看起来更智能"的功能诱惑（比如让 LLM 编造置信度分数）带偏的领域里
   是稀缺品质，也是我认为这个项目最不应该在未来扩张中丢失的东西。
4. **（交叉核实补充）版本号策略值得随受众变化重新评估**——`buildinfo` 刻意不编造 semver、只报告 VCS
   commit 短哈希，我在 §3.2 已经把这判断为"当前阶段合理的克制"（没有能递增语义化版本号的发布流程，
   编造一个没人维护的版本号比诚实报告 commit SHA 更糟）。但这个判断的前提是"当前阶段"——如果未来
   `vmr` 的使用者从"看得懂 commit SHA 的开发者"扩展到更广的受众（比如通过 Homebrew tap 触达的普通
   用户），"这个版本改了什么"这个问题会变得更重要，届时可以考虑引入 tag 驱动的语义化版本号。不需要
   现在做，但值得作为一条随受众变化重新评估的产品决策记在这里。
5. **（交叉核实补充）"重跑一次"作为真实竞品对分析半区叙事的启示**——这是一个产品叙事层面的观察，不是
   架构问题，但会反过来影响"单人维护的投入方向"这个架构决策：Agent 偶发错误的实际解决成本往往是"重跑
   一次"，成本几毛钱，这意味着"事后诊断单次错误"这类分析价值天然有上限；真正有复利效应、值得投入的
   分析方向是"复发、可累加"的痛点（比如 TokenPlan 额度配速——每月都在发生、金额会累加），而不是"这次
   为什么犯了这个错"。vmr 当前的护城河叙事（"飞行记录仪"）与它目前最扎实的经济价值（TokenPlan 省钱）
   之间存在一定错位——这个观察不改变本文档任何一条架构判断，但如果用户认同它，会强化 §11.3 第 5 点
   "新功能的准入应该看边际贡献而非技术可行性"这条元结论的现实紧迫性。
6. **本次审查过程中我没有改动任何代码**——所有发现均记录于此，供用户按优先级自行排期；本文档结构上
   已经把"重要"与"次要"分开，重要项也已按我认为的 ROI 大致排序（P0 > P1），但最终优先级裁定权在用户。

### 11.6 结论（一句话）

**vmr 的架构质量在单人维护项目里属于顶尖水平——没有发现任何"设计错了"的架构，23 个包的原则贯彻高度
一致；真正值得盯的不是代码写得好不好，而是"这个能力值得投入多少工程严谨度"这条判断标准有没有在
每一个具体决策点上被真正问过——TokenPlan 的 `metric: cost` 定价引擎与持续扩张的分析半区，是我这次
独立通读全部源码后找到的两处最具体、证据最扎实的"标准松动"信号。下一阶段三个最值得做的具体动作：
① 补 `ErrContextLimit`（健壮性，最高 ROI，改动量最小）；② 用户明确裁定 `metric: cost` 的产品定位
（核心还是可选，本文倾向"保留代码、调整叙事和引导"）；③ 给分析半区的新功能设一条"先外部脚本验证
真实语料 ROI，再考虑收进二进制"的准入准则，并把它和 `response.go` 的行数预算一起，沉淀成
`internal/archtest` 里可执行的规则，而不是停留在文档里的共识。**

> **✅ 2026-08-10 复核落地情况（分两轮）**：①②③ 三个动作里，① 已完全落地（`ErrContextLimit` 新增并
> 接线，见 P0-B）；② 用户裁定选择了本文倾向的 (a) 之外的第三条路径——保留 `metric: cost` 与三层解析，
> 但按交叉核实补充里的方案 (c) 砍掉时间窗功能面（见 P0-A），是介于"保留全部"和"整体降级"之间更具体
> 的中间选择；③ 的两个子项分开处理——`response.go` 的行数预算在第二轮 ROI 复核里已补上（见下），
> 分析半区准入准则（P0-C）本身是产品决策，仍待用户裁定。第一轮同批顺带处理：P1-C（`newUserWindow`
> 下沉）、`future-strategy/` 目录归档、`loadtest` 地址常量去重。
>
> **第二轮（同日，"ROI 合理性"复核）**：按"问题真实存在 / 根因方案明确无争议 / effort 与风险相对价值
> 合理"三条标准重新过了一遍全部未处理发现，筛出两项立即处理：`response.go` 补 archtest 850 行预算
> （P1-A）；`core.NewTokenWeights()` 构造函数（§3.1 建议 3）——**这次重新读代码时发现它比原文档记录
> 的范围更大**：`cmd/vmr/cmd_check.go` 里还有一处独立同款字面量，原文档判断的"唯一生产构造点"其实
> 已经出现了第二处，不是假设性风险。`copyFlush` 数据竞争（P1-B）、`vmr replay` 计费、`/metrics`
> 端点等其余项经重新评估后判定 effort/风险高于顺手改的量级，列入第二梯队，本轮未处理。
> `docs/OUTSTANDING_ISSUES_opus-5.md` 与本文档的对应条目已同步更新。
>
> **第三轮（2026-08-11，用户另行提出并核实的一条 P0）**：软屏蔽/quirk 修复聚合展示（11.4 缺口 #6）——本文档
> 第二轮曾把它归进"列入第二梯队"，但用户随后单独就这一条重新核实并要求落地，独立验证过程见该条目
> 本身的批注（含对仓库现存 9101 份 detail 文件的实测命中率统计）。已修复，细节见 §11.4 第 6 条。
>
> 三轮全部改动通过 `go build`/`go vet`/`go test -race ./...`/`internal/archtest`/`gofmt` 验证，
> 新增/调整测试约 25 个用例。仍待用户后续裁定的：P0-C（分析半区体量准入门槛）、11.4 缺口清单里
> 其余尚未打勾的项目。

---

*（全文完。本次审查分两轮：第一轮从任务 debrief 到 §11 全局总结，共读取 8 份关键文档 + 23 个 internal 包 +
cmd/vmr + archtest + 部分工程配套文件的实际源码，逐模块记录发现，完全未参考仓库内任何既有审查文档；
第二轮与另外两份既有审查文档（Gemini 3.6 Flash 版、同作者上一轮会话的 kiss_single_maintainer 版）逐项
交叉核实，把经核实成立、本文档原本未覆盖的点补充进相应章节——用"（交叉核实补充）"标注，两份原始文档内
也逐项加了批注标明吸收/拒绝依据，可对照参看。全文结论以本文档为准。）*
