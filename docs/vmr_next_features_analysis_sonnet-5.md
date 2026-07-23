// Ver 2026-07-24 12:00, by Sonnet 5（第三次修订：第 6 项——条件路由框架 + Sticky Model——已实现并合并进 docs/VirtualModelRouter_System_Design_v3.md，本文档对应章节压缩为指向该文档的简述；其余 5 项仍是未开工的候选分析，原样保留）

# VMR 下一步特性候选分析 — 6 项推荐 + 落地方案初评

> **输入**：`docs/extensions.md`（两轮功能拓展建议，含一份基于真实代码的收窄分析）+ `docs/vmr_future_strategy_deepseek-v4-flash.md`（竞品格局 + A1-A26 候选清单 + 2026-07-16 复核校准）+ `docs/VirtualModelRouter_System_Design_v2.md`（项目总体设计方案，第二轮修订时通读，用于评估架构扩展点）
> **方法**：本文档不重复罗列原始清单，只从中挑出最值得做的功能，逐项对照当前代码实现（`internal/*`、`cmd/vmr/main.go`、`vmr.sh`）给出具体改动点分析和粗略工作量评估。原始版本挑了 5 项；第 6 项最初是用户追问"能力感知调度"后追加的单一特性（按图像输入过滤端点），第二轮修订应用户要求把它从"一个具体功能"上升为"一类可扩展的架构能力"——见 §6。
> **范围**：本轮只做分析，不改代码。用于下一步"选一项或几项开工"的决策依据。
> **前提约束**（贯穿全文，来自输入文档一致的哲学）：不破坏 byte-faithful 透传、不引入新运行时依赖、不做 SaaS/DB/RBAC、保持单二进制、**不做规则引擎/条件表达式 DSL**（§6 会展开说明为什么"可扩展"不等于"给用户一门配置语言"）。

---

## 0. 6 项推荐一览

| # | 特性 | 来源 | 类型 | 粗估工作量 |
|---|------|------|------|-----------|
| 1 | 成本核算报表（token → 货币，report-only） | extensions.md TOP1 / 分析项1 | 可观测性 | 中（1.5-2.5 人天） |
| 2 | 软拦截（2xx 但内容为空/被替换）升级为可 failover | extensions.md 分析项2 | 可靠性 | 中偏高（2-3 人天） |
| 3 | 上下文超限（context length exceeded）独立错误类 + failover | extensions.md 分析项3 | 可靠性 | 小（0.5-1 人天） |
| 4 | `vmr.sh doctor` + `check`/`status` 补齐 `--json`（对应 R3+R1） | strategy.md §3.6 | 易用性/可脚本化 | 小（0.5-1 人天） |
| 5 | per-virtual-model 预算硬闸（token/成本日预算，触顶拒绝） | extensions.md 分析项4 后半 | 安全/防呆 | 中（1.5-2 人天） |
| 6 | ✅ **已完成**——条件路由框架 + Sticky Model（可插拔端点过滤条件 + 会话亲和路由） | 用户三轮追问，原两份输入文档未覆盖；设计文档路线图有一行未展开的"能力/标签路由"远期条目 | 架构/可靠性/调度 | 实际交付：框架 + `image`/`tools` 能力 + `max_context_tokens` + 完整 Sticky Model；详见 `docs/VirtualModelRouter_System_Design_v3.md`「调度与健康」一节 |

选择原则：优先挑"两份文档都指向、且已经对照真实代码收窄过范围"的项——即 `extensions.md` 里编号 1-4 的那组分析（它本身已经是对更早的 Policy Router / Cost-aware Router / Capability Router 三个"★★★★★"构想做过一轮基于代码的批判性收窄，明确反对把成本/能力接入实时路由决策）。第 4 项另取自 strategy.md 的审计驱动清单,因为它是"本周内、成本 30-60 分钟级"的最高杠杆动作,理应和前四项一起排产。

**明确不选的原因**（避免遗漏交代）：
- **Policy Router / Capability Router / Cost-aware Router**（extensions.md 早期"★★★★★"构想）：本质是规则引擎/能力抽象层，和 vmr"透明路由器"定位摩擦大，`extensions.md` 自己的后续分析（项1）也已经否决了"让价目表影响实时路由"这一步，只保留报表侧。**这个判断没有被 §6 推翻**——§6 提议的"条件路由框架"解决的是同一类问题（端点能力差异化调度），但刻意选择"Go 代码层可插拔 Condition + 配置里只填声明式字段"的形态，不是给用户一门条件表达式语言；price 条件最终也确实没做（归入排序维度，不是过滤条件），与这里否决的"规则引擎"/"价目表进实时路径"是两回事。
- **端点级 RPM/并发限流**（extensions.md TOP3）：`extensions.md` 分析项4 自己给出的结论是"价值中等,vmr 的 failover 已经把 429 的用户可见伤害压得很低",相比之下预算硬闸价值更高,所以本文档选了后者、放弃了前者。
- **thinking-strip 未触发告警（R14）**：audit 报告里唯一的 `[S]` 级发现,值得做,但和上面 5 项相比是纯观测性小修（预计 <0.5 人天）,建议作为"顺手做"的第 6 项,不占 5 个名额。

---

## 1. 成本核算报表（token → 货币）

### 用户价值
多 provider 混用（国内/OpenRouter/直连）时,`vmr report` 现在只有 token 数量,没有 `$`/`¥` 换算。Stanford 自己就是这个多账号场景的用户,这是"高频自用需求",不是假设的用户故事。

### 现状代码基础
- `internal/report/report.go` 的 `Row`（56-224行）、`EndpointRow`（225-261行）已经按 `(date, protocol, model)` 和 `(date, endpoint)` 两种粒度聚合了 `TokensIn` / `TokensInCached` / `TokensInCacheWrite` / `TokensOut`（report.go:93-97, 235-239）。两家厂商的 cache 语义已经在这里做了归一化——这是原本最难的部分,已经有了。
- `EndpointRow.Endpoint` 的取值来自 `router.go:438` 的 `strings.Join([]string{ep.AdapterType, ep.Provider, ep.Model}, ":")`,也就是 `protocol:provider:model` 三段式,与 extensions.md §12.2 设想的价目表 key 格式天然吻合,不需要额外做一次归一化映射。
- `internal/report/usage.go` 的 `Usage` 结构（19-26行）已经把 In/Out/CacheRead/CacheWrite/Reasoning 拆好了,cost 公式要用的输入齐了。

### 实现方案初步分析
1. 新增一个价目表配置（建议 `prices.yaml`,独立文件,不塞进 `config.yaml`——价目表会漂移,和路由配置分开管理、分开版本化更合理）,key 为 `protocol:provider:model`,值为 `{input_per_1m, output_per_1m, cache_read_per_1m, cache_write_per_1m}`,货币单位允许混用（`$`/`¥`）,不做汇率换算(说清楚"这是原始货币,不是统一到 USD",避免过度设计)。
2. `vmr report` 增加一个可选 `-prices` flag 指向该文件;不传时行为完全不变(costs 相关列不出现)——保持"默认配置不用改也能用"的向后兼容原则。
3. 在 `report.Build`（report.go:281 起）的聚合循环里,读到价目表后按 `EndpointRow.Endpoint` 的三段式 key 查表算出 `CostIn`/`CostOut`/`CostTotal`,累加到 Row 和 EndpointRow 里新增的对应字段(`json:"cost_total,omitempty"` 等,omitempty 保证没传价目表时 JSON 输出不变)。
4. Markdown 渲染（`internal/report/markdown.go`）加一列"预估费用",按 by_model / by_date 两个维度都能看到费用分布。
5. **明确不做**:不让这份价目表影响任何路由/health/failover 决策——`extensions.md` 分析项1 已经论证过,一个会过期的外部文件不该进实时路径,只做报表侧的“事后算账”。

### 风险与取舍
- 价目表本身是手工维护的静态数据,厂商调价后不会自动更新——这是设计上接受的限制,文档里要写清楚"这是估算,不是账单"。
- key 格式(`protocol:provider:model`)如果哪天 `ep.AdapterType`/`Provider`/`Model` 的拼接方式变了(目前看不出会变),价目表要跟着改——但这本来就是 debug 时能通过 `vmr report` 本身的 endpoint 列表反查的,风险低。

### 工作量评估
- prices.yaml 解析 + 校验: 0.3 天
- report.Build 里接入 cost 计算(Row/EndpointRow 两处): 0.5 天
- Markdown/JSON 输出改造: 0.4 天
- 测试(价目表命中/未命中/货币混用场景): 0.3-0.5 天
- **合计:约 1.5-2 人天**,是 5 项里收益/成本比最高的一项。

---

## 2. 软拦截（2xx 但内容为空/被替换）升级为可 failover 的失败

### 用户价值
国内厂商特有的"HTTP 200 但内容被替换/清空"的软屏蔽,agent 完全无感知,基于空回复继续往下跑——深夜无人值守场景比 429 更危险,因为 429 会触发现有 failover,软拦截不会。

### 现状代码基础
- `internal/router/response.go` 已经在做检测:`containsSoftBlockMarker`(155-160行,匹配 `"input_sensitive":true` / `"output_sensitive":true`)在流式(`emitBlock`, 411-413行)和非流式(`finalizeBuffered`, 469-471行)两条路径上都会触发,目前只做 `s.noteApplied("soft_block_detected")` 打标,写入 audit 的 norm 字段(`internal/report/detail.go:41` 有对应的中文注释),**不影响转发给客户端的字节,也不影响 health**。
- 关键架构约束(`internal/router/router.go:563-565` 的注释原文):"Success: report health, then forward. From the first byte written the response is committed — no failover past this point." 也就是说,现在 `ReportSuccess` 和"开始转发"几乎是同一时刻发生的,发生在 response.go 的规整化/软拦截检测**之前**。这意味着"检测到软拦截就 failover"不是加一个 if 分支那么简单——要让检测结果能触发 failover,必须让"完整读完并规整化响应体"发生在"上报健康 + 开始给客户端写字节"之前。

### 实现方案初步分析(比 extensions.md 原描述更具体的落地路径)
1. **范围收窄到非流式(buffered)响应**——`finalizeBuffered`(response.go:443-475)本来就是"整个 body 读完、规整化完,再一次性写出"的模式,天然满足"检测完再决定转发还是 failover"的时间顺序。流式(passthrough)响应从第一个 payload event 起就已经在边读边转发,检测点出现时前面的字节可能已经写给客户端了——这部分**技术上做不到 failover**,只能继续保持"仅观测"。这一点必须在实现和文档里都写清楚,不能承诺做不到的事(这正是 extensions.md 原文的提醒)。
2. 具体改法:在 `router.go` 的 `tryOne`(424行起)里,2xx 分支(563行之后)不能再无条件立刻 `ReportSuccess` + 转发。需要新增一步:对于会走 buffered 路径的响应(非 SSE,或 SSE 但首个 payload event 命中 MiniMax thinking 形状——这两类本来就已经被 response.go 判定为要整体缓冲),把"规整化 + 检测"提前到 `ReportSuccess`/写字节之前完成;只有规整化结果里没有 `soft_block_detected` 才继续走现有的"报告成功 + 转发"逻辑;命中则视为一次可 failover 的失败(不惩罚端点健康,复用 `ReportNeutral`,语义上等同 `ErrContent` 分支的处理方式——router.go:538-545 已有现成的对称写法可以参照),转向下一个候选端点。
3. **判据要收窄**,不是"命中标记就直接 failover":要求"标记命中 且 有效内容为空/极短",避免把"确实检测到标记但正文其实还有内容"的情况也 failover 掉,产生误伤。这需要在规整化逻辑里补一个"有效内容长度"的判断(可以复用 `ExtractUsage`/内容提取的已有代码路径,不必重新写一套解析)。
4. 归入的错误语义建议直接复用 `core.ErrContent`(core.go:70,"content policy/moderation flag: request-specific, switch WITHOUT health penalty")而不新增一个 ErrorClass——软拦截在效果上就是"这条请求被内容策略拦了,换个端点也许能过",和 ErrContent 的语义完全一致,没必要新增分类。

### 风险与取舍
- 这是 5 项里**架构改动面最大**的一项:需要把 router.go 里"2xx=立刻报告成功并转发"这个目前隐含的前提拆开,插入一个"先决定要不要 failover,再报告健康"的判断点。要仔细验证不会破坏现有的流式转发路径(那部分必须保持零改动,继续走老逻辑)。
- 明确排除 extensions.md §12.1 提到的"敏感词替换插件"方案——那涉及改写响应正文,和 byte-faithful 摩擦大、词库维护是无底洞,不在本项范围内,建议永久搁置。
- 覆盖面有限(仅非流式/流式早期),要在 README/设计文档里明确写清楚这个边界,不能让用户误以为所有软拦截都会被兜住。

### 工作量评估
- response.go 内容长度判据 + 判定逻辑: 0.5 天
- router.go tryOne 的 2xx 分支重构(拆开 ReportSuccess 时机): 0.8-1.2 天(这是最容易引入回归的部分,需要额外小心)
- 测试(buffered 命中/未命中、流式路径不受影响的回归测试): 0.7-1 天
- **合计:约 2-3 人天**,是 5 项里第二贵的,但价值也高(唯一针对"无人值守 agent 静默吃错误回复"这个最危险场景的项)。

---

## 3. 上下文超限（context length exceeded）独立错误类 + failover

### 用户价值
长会话 agent 跑到某个点,请求体超过便宜端点的 context 上限,厂商返回 400。按现在的分类逻辑,这类 400 会落到 `ErrClient`(不 failover,直接返回客户端)——一个跑了两小时的任务就此终止,即使候选队列里可能正躺着一个 context 更大的端点。

### 现状代码基础
- `internal/adapter/classify.go` 的 `DefaultClassify`(28-84行)已经是"先过内容词表(`contentHint`,101-107行)→ 再过模型词表(64-67行)→ 再过 `upstreamHint`(77-79行)→ 兜底 `ErrClient`"这套完全一致的模式。加一类新判定就是在这条链上再插一段词表匹配,是这套代码里改动成本最低、复用度最高的一项。
- `core.ErrorClass`(core.go:64-86)是一个简单的 int 枚举 + `String()` 方法,新增一个值(如 `ErrContextLimit`)只需要在这一处加 case,`router.go` 里对 `ErrEndpoint`/`ErrTransient` 的处理分支(558行,`cd := rt.Health.ReportFailure(...)`)已经是"switch + 记录 cooldown"的统一路径,新错误类只要决定"要不要惩罚端点健康"就能直接接进去。
- extensions.md 原文建议行为上与 `ErrContent` 同构(切换、不惩罚端点健康——端点没病,是这个请求太大),这个决策是合理的:端点因为请求超限而拒绝,不代表它对其他正常大小的请求也有问题。

### 实现方案初步分析
1. `classify.go` 新增一组词表(`maximum context length`、`context_length_exceeded`、`too many tokens`、`输入长度超过`、`context window` 等,中英双语,复用现有 `containsAny` helper),放在 `contentHint` 检查之后、模型词表检查之前(避免"model"相关词表误吞掉本该属于 context 超限的 400)。
2. `core.ErrorClass` 新增 `ErrContextLimit`,`String()` 加对应 case;`router.go:538` 附近仿照 `ErrContent` 的处理分支(`ReportNeutral` + 不惩罚健康 + 继续 failover)。
3. 配套(可选,建议本期先不做,后置):`EndpointConfig` 加一个可选 `context_limit` 声明字段,让 `internal/strategy/strategy.go` 的排序能把"大 context 端点"排在后面当兜底——这一步涉及 strategy 排序维度的扩展,是独立的小特性,不阻塞"先把 400 不再一头撞死"这个核心修复。
4. 复用现成的分类测试骨架(`internal/adapter` 下应该已有 classify 相关测试模式,新增用例照抄格式即可)。

### 风险与取舍
- 词表维护和 `contentHint`/`upstreamHint` 一样是"总会有新厂商的新措辞"的持续性任务,不是一次性完成——这是可接受的,和现有 classify.go 的维护模式一致,不算新增风险类型。
- 词表误判的代价很小(最坏情况多切换一次,不会造成安全问题),适合"宁可宽松"的判定策略,和 `contentHint` 的注释里"lean wide"的原则一致。

### 工作量评估
- 词表 + 分类逻辑: 0.2-0.3 天
- ErrorClass 新增 + router.go 分支: 0.2 天
- 测试: 0.2-0.3 天
- **合计:约 0.5-1 人天**,是 5 项里改动面最小、风险最低、复用度最高的一项,建议优先做。

---

## 4. `vmr.sh doctor` + `check`/`status` 补齐 `--json`

### 用户价值
新用户排查"为什么连不上"时,现在要分别跑 `vmr check`(纯文本)+ `vmr diagnose`(已有 `--json`)+ `vmr status`(纯文本,依赖进程已启动)三条命令自己拼线索。`vmr.sh doctor` 把三者串成一条命令、一个红绿灯摘要,是新用户和自查时的第一入口。

### 现状代码基础
- `vmr.sh`(项目根目录)已经是一个成熟的进程管理脚本,有 `start`/`stop`/`status`/`service *` 等子命令,`cmd_status()`(232-237行)已经在调用 `"$BIN" status -c "$CFG"`——加一个 `doctor` 子命令是往这个既有框架里加一格,不是从零搭。
- `cmd/vmr/main.go` 里 `cmdCheck`(572-616行)和 `cmdStatus`(618-...)目前都是纯 `fmt.Printf` 直接打印,**没有** `--json` flag;对照的是 `cmdDiagnose`(264-299行)和 `cmdReplay`(213-...)——这两个已经有 `-json bool` flag(269行 diagnose,replay 的 `-list` 也有,main.go:269 附近)的完整实现可以直接抄写模式。
- `cmdStatus` 内部本来就是对 `http://<listen>/admin/status` 发一个 GET 拿 JSON(631-639行),然后 unmarshal 成一个匿名 struct 再重新格式化成文本——也就是说底层数据早就是 JSON 了,加 `--json` 只是"跳过 unmarshal+重新格式化这一步,直接把 body 打印出来"这么便宜的事。
- `cmdCheck` 目前完全是过程式的 `fmt.Printf` 调用(585-614行),要支持 JSON 需要先把打印内容重构成一个结构体(`{listen, providers, models, image_downscale, ..., models: [...]}`),再在文本/JSON 两种模式间分支——比 `cmdStatus` 多一点工作量,但结构不复杂。

### 实现方案初步分析
1. `cmdStatus` 加 `-json bool` flag:命中时直接 `os.Stdout.Write(body)`(或者 `json.Indent` 美化一下),不 unmarshal;这一步几乎零成本。
2. `cmdCheck` 把 585-614 行的打印内容抽成一个 `checkResult` struct,加 `-json bool` flag,按现有 `cmdDiagnose` 的两段式(`if *jsonOut {...} else {...}`)模式实现。
3. `vmr.sh` 新增 `doctor` 子命令(参照现有 `cmd_status`/`svc_status` 的写法),内部依次跑:
   - `vmr check -c "$CFG"`(配置本身合法吗)
   - `vmr diagnose -c "$CFG"`(连通性,已有 `--json`)
   - 若进程在跑:`vmr status -c "$CFG"`(实时健康/冷却状态)
   给出一个整体红绿灯摘要(比如"config: OK / connectivity: 3/3 OK / runtime: not running"这种一行结论 + 各部分详情),文本模式给人看,可选 `--json` 给脚本用(三段结果拼进一个顶层 JSON 对象)。
4. 这一项本身就是最好的"传播素材"——strategy.md §3.6 已经指出,做完之后正好是发 HN/Reddit 帖子"一条命令看清一切"的演示钩子,不需要等到别的特性做完。

### 风险与取舍
- 几乎没有架构风险,纯粹是给已有输出加一层结构化包装,不触碰路由/health/audit 任何核心逻辑。
- 需要注意 `vmr.sh doctor` 在 vmr 进程未启动时的行为要优雅(status 那一段应该明确报"not running",而不是让整个 doctor 命令因为一个子步骤失败就退出——参考 `svc_status` 415 行 `"$BIN" status -c "$CFG" || true` 的既有容错写法)。

### 工作量评估
- `cmdStatus -json`: 0.1 天
- `cmdCheck` 结构化 + `-json`: 0.3-0.4 天
- `vmr.sh doctor` 子命令(含红绿灯摘要拼接): 0.3-0.4 天
- 测试(含"进程未启动"这个关键分支): 0.2 天
- **合计:约 0.5-1 人天**,strategy.md 原文自己的估计是"本周内、30-60 分钟级"——本文档给的估计更保守是因为把测试和 `vmr.sh` 侧的容错处理也算进去了,但量级判断一致:5 项里最快能出成果的一项。

---

## 5. per-virtual-model 预算硬闸(token/成本日预算,触顶拒绝)

### 用户价值
现在的健康机制全是事后反应(撞上 429→冷却→切换)。对 bootstrap 用户还有一个更根本的风险:一个进入死循环的 agent 可以在一夜之间烧掉整月额度,vmr 全程忠实转发,一句话不说。触顶后应该是**明确拒绝并给出可解析的错误**,而不是静默降级到便宜模型——静默降级会让 agent 在完全不知情的情况下换了个"脑子",比直接失败更难排查(这一点 extensions.md 原文强调得很清楚,是这项设计里最重要的取舍)。

### 现状代码基础
- `internal/router/router.go` 的 `Serve()`(285-379行)是所有请求的统一入口,在"健康过滤 + 排序"(301-329行)之前插入一个"预算检查"是最自然的位置——不需要改 `tryOne` 内部的任何 attempt 级逻辑,一次检查覆盖整个 virtual model 的所有候选端点。
- `internal/health/health.go` 的 `Registry`(31-36行起)是纯内存状态(`map[string]*state`),**没有任何跨请求的 token/成本累加**——这印证了 extensions.md 原文的判断:预算闸需要的"跨请求累计状态"目前在代码里完全不存在,是一块新状态,不是复用已有机制那么简单。
- `internal/report/usage.go` 的 `ExtractUsage`(28-62行)已经有"从响应体里提取 token 用量"的完整实现,可以复用同一套解析逻辑喂给预算累加器,不需要重新写一遍 usage 解析。

### 实现方案初步分析
1. 新增一个内存态的 `budget.Registry`(仿照 `health.Registry` 的结构),key 为 virtual model 名(`protocol:model` 二元组,和 health key 的粒度保持一致方便交叉查阅),值为"今日已用 token/成本 + 上次重置时间"。
2. 配置侧:`ModelConfig`(config.go:94-98)加一个可选字段,如 `daily_budget: {tokens: N}` 或 `{cost: N, currency: "usd"}`——是否要支持"成本"预算依赖第 1 项(成本核算)是否已经落地;如果第 1 项还没做,本项可以先只支持 token 预算(不依赖价目表,独立可交付),成本预算作为后续增量。
3. `router.go Serve()` 开头(300 行之前)加一步:查 `budget.Registry`,若当日累计超过配置阈值,直接 `core.WriteError`(复用 core.go:42 的 `WriteError`)返回一个如 `429`(或自定义 `vmr_budget_exceeded` 错误类型)、消息里说清楚"virtual model X 今日预算已用尽,阈值 Y,已用 Z"——**必须是可解析的明确错误**,不能吞掉请求或悄悄换端点。
4. 累加时机:在 `tryOne` 的 2xx 成功分支(router.go:563 附近)拿到响应后,用 `ExtractUsage` 解析出的 token 数累加进 `budget.Registry`。
5. 重置语义:每日 UTC(或本地时区,需要确定一个约定)零点重置,进程重启也重置——**明确接受"重启即清零"这个弱语义**,在文档里直说这是"防呆,不是防欺诈"(extensions.md 原文的表述),不要为了持久化这个状态引入文件/DB,那是滑向"vmr 需要持久化状态"的第一步,应该坚决避免。

### 风险与取舍
- **最大的设计决策**是"重启清零"这个弱语义是否可接受——本文档和两份输入文档意见一致:接受。如果 Stanford 认为这个语义太弱(比如担心一天内多次重启被用来绕过预算),需要在开工前明确讨论,因为一旦决定要跨重启持久化,工作量会显著上升(需要引入某种轻量存储,哪怕只是一个本地文件),且违反"不引入 DB"的哲学边界需要重新评估。
- 预算维度选 token 还是成本,决定了本项是否依赖第 1 项(成本核算)——建议先做纯 token 版本作为 MVP,可以独立于第 1 项排期。
- 触顶行为需要想清楚"对正在进行中的多轮 attempt 请求"如何处理——理想情况下预算检查只在请求入口做一次(而不是每个 attempt 都检查),这样行为对客户端来说是确定的("要么整个请求被拒,要么不受预算影响"),避免出现"重试到一半突然被预算拦下"的诡异中间态。

### 工作量评估
- `budget.Registry` 新增(仿 health.Registry 结构): 0.4 天
- 配置字段 + 校验: 0.3 天
- `router.go Serve()` 接入(拒绝分支 + usage 累加时机): 0.5 天
- 测试(含"日期跨零点重置""预算刚好等于阈值"边界): 0.3-0.5 天
- **合计:约 1.5-2 人天**(纯 token 版本;若要支持成本预算,再加上第 1 项的依赖,合计变为两项工作量之和)。

---

## 6. 条件路由框架(Condition-based Routing)+ Sticky Model —— ✅ 已完成

原本节是"能否按图像输入能力自动跳过不支持的端点"这一具体问题的分析，经过几轮反馈后被提炼为一个可扩展的过滤框架，并因为条件路由（尤其上下文长度维度）会打掉上游 prompt cache 的副作用而追加了 Sticky Model 会话亲和路由配套。两者作为一次连续迭代已经实现并上线，完整设计（`Condition` 接口与 `Dimension` 平行而非扩展、`image`/`tools`/上下文长度三个维度的最终定义与估算公式、`thinking`/`audio`/`video` 为何暂不注册、Sticky Model 的会话指纹识别与 TTL 设计、`sticky_ttl` 24 小时硬上限校验等）已经合并进 `docs/VirtualModelRouter_System_Design_v3.md`「调度与健康」一节，本文档不再重复维护这部分内容。price 维度按分析结论未做——它是排序（Dimension）关注点，不是准入（Condition）关注点。

---

## 7. 综合建议 / 实际执行结果

原计划顺序是 #3（上下文超限独立错误类，作为 #6 的兜底前置）→ #4（`vmr.sh doctor` + `--json`，顺手做的低成本项）→ #6（条件路由框架 + Sticky Model，当轮核心）。**实际只交付了 #6**：条件路由框架 + `image`/`tools` 能力 + `max_context_tokens` + 完整 Sticky Model 已实现并上线（见「条件路由框架 + Sticky Model」一节）。#3、#4 连同 #1（成本核算报表）、#2（软拦截升级 failover）、#5（预算硬闸）一样，仍然是**未开工的候选分析**——包括原本打算作为 #6 前置项的 #3，也没有真正落地；#6 最终没有依赖它也顺利完成（条件路由的上下文长度维度自身已经有「估算过高时回退到 `hardFiltered`」的降级规则兜底，不依赖 #3 那个独立错误类）。

**下一步候选**（按原分析的相对优先级排序，尚未安排具体排期）：#3 上下文超限独立错误类（改动小、复用度高）→ #4 `vmr.sh doctor` + `--json` 补齐（低成本、易演示）→ #1 成本核算报表 → #2 软拦截升级 failover → #5 预算硬闸。各项分析内容见本文档对应章节，工作量估算未随本次修订重新核实。
