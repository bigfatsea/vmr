<!-- Ver 2026-07-17 02:00, by Sonnet 5 -->
# vmr — 审计 3.0(综合复核 + 三梯队定稿)

> **性质**:本报告是对已有两份独立 2.0 审计——`docs/AUDIT_REPORT_2_Fable5.md`/`AUDIT_SUMMARY_2_Fable5.md`(Fable 5)与 `docs/audit_report_v2_logs_pi_agent.md`/`audit_report_v2_summary_pi_agent.md`(Pi Agent, minimax-m3)——的**逐条核对与合并定稿**,不是第三次从零重审。两份 2.0 报告各自独立完成、部分结论互相冲突(尤其是"哪些项目已修复"),本报告以**当前代码库实际状态**(`git log` HEAD `fb2d760`,"复核结果")为唯一裁判标准,逐条重新核实后归档。
>
> **基线**:两份 2.0 报告都以 `1d50611`(add load testing)为起点分析;`fb2d760` 是同一天之内在此基础上落地的修复提交。本报告核实的是 `fb2d760`(当前 HEAD)的真实状态,`git diff 1d50611 HEAD` 显示 39 个文件、+2725/-185 行改动。
>
> **方法**:对两份 2.0 报告中列出的全部条目(合计约 45 条去重后的独立问题),逐条用 `grep`/`sed` 读取当前源码核实真实状态,而非采信任一报告的自述。核实中发现两份报告之间及各自与代码之间均有出入(见下文各条"核实结论")。核实后按 1.0/2.0 沿用的三梯队标准重新分组;过程中未发现值得单列的全新缺陷(代码库这一层已被两轮审计充分覆盖),但对若干条目的严重度/梯队归属做了调整。
>
> 严重度:`[S]` 严重 / `[M]` 中等 / `[L]` 轻微 / `[I]` 信息性。梯队标准沿用 1.0/2.0:**第一梯队** = 推荐尽快改(问题+方案+工作量);**第二梯队** = 改与不改都合理(问题+简要方案);**第三梯队** = 点出即可,不展开。

---

## 0. 结论先行

- **没有发现任何 `[S]` 级别之外的严重缺陷**,也没有发现会导致数据丢失、凭证泄漏或服务不可用的新问题。项目仍处于"工程稳定期",可继续放心用于生产。
- 两份 2.0 报告加起来声称的"18 项已修复"经核实**基本属实**,但有两点需要更正(均已在本轮处理):
  1. Pi Agent 报告称 **`internal/adapter/anthropic/anthropic.go` 的 `defaultVersion` 强制覆盖问题"仍成立"**——核实**属实,确实未修**;本轮已修复,见 §1 第 17 条。
  2. Pi Agent 报告称 **`sanitizeName` tag 碰撞"仍成立"**,Fable 5 报告未把它列入已修复项——核实**属实,确实未修**;但按 owner 决定,这不再作为代码问题跟踪,改为在配置文件加说明、由配置方自行规避,见 §1.1。
- **本报告唯一需要读者注意的新变化**:两份 2.0 审计报告文件(`AUDIT_REPORT_2_Fable5.md` 等 4 个文件)在 `fb2d760` 中已正式提交入库,不再是 untracked 状态——1.0/2.0 报告里反复提到的"待 owner 处置的游离文件"问题已自然解决。另外 `docs/AUDIT_REPORT.md`(1.0 报告)当前在工作区被标记为已删除但**尚未 commit**——这是本次会话开始前就存在的工作区状态,不属于审计发现,仅供你确认是否为有意操作。

> **2026-07-17 01:30 更新**:按 owner 指示处理了本报告第一/第二梯队中的 5 项——2 项文档修复(§2.2、config.example.yaml 说明)、2 项代码修复(anthropic 版本透传、Bearer 大小写)、1 项核实为非问题后关闭(429 词表已正确与状态码绑定)。详见 §1(已修复项第 15-18 条)、§1.1(sanitizeName,改配置说明关闭)、§1.2(429 词表,核实非问题关闭)。
>
> **2026-07-17 02:00 更新**:对第二梯队里另外 4 项(SSE CRLF、normDescriptions 缺项、modelFieldPattern 嵌套、containsSoftBlockMarker)做了深入机制分析后,按分析结论逐项执行——SSE CRLF 与 modelFieldPattern 嵌套都判断"结构性修复成本高于现实收益",改为低成本的观测/诚实化方案;normDescriptions 直接补全;containsSoftBlockMarker 复核后确认是设计文档写明的分阶段计划的第一阶段,**不是缺陷**,不做代码改动。详见 §1(已修复项第 19-21 条)、§1.3(containsSoftBlockMarker,核实为设计决策)。
>
> 至此,第一梯队只剩 2.1(`stripThinkingProcess` 观测告警)与 2.2(`needle` 漏链观测)两项,按 owner 要求暂不处理;第二梯队从 14 项收敛到 9 项。全部改动(含新增 3 个测试用例)已跑过 `go build`/`go vet`/全量 `go test`/涉及包的 `go test -race`,全绿。

---

## 1. 已修复项(核实属实,仅作现状归档,不再展开分析)

以下 21 组问题经核实**已在当前代码/文档中修复**(前 14 组为 `fb2d760` 提交所修,第 15-21 组为 2026-07-17 分两批处理),不再重复问题描述与方案,仅记录修复形态供追溯:

| # | 问题(源审计条目) | 修复形态(已核实) |
|---|---|---|
| 1 | `classify.go` 402/404 跳过 `contentHint`(1.0 §1.B.2 / 2.0 两份报告 §3.1) | 402/404 分支前补 `contentHint(snippet)` 检查,与 403/429 一致(`internal/adapter/classify.go:42-49`) |
| 2 | `imgprep.Downscale` panic 恢复零观测(1.0 §4.17 / 2.0 两份 §3.1) | `recover()` 分支加 `fmt.Fprintf(os.Stderr, ...)` 一行痕迹(`internal/imgprep/imgprep.go:132-134`) |
| 3 | 配置未知字段静默忽略(2.0 两份 §3.1) | `Parse` 改用 `yaml.NewDecoder` + `dec.KnownFields(true)`(`internal/config/config.go:180-181`),拼写错误的 key 现在加载即报错 |
| 4 | `config/watch.go` 零测试(1.0 §3.2.3 / 2.0 两份) | 新增 `internal/config/watch_test.go`(97 行),含写入触发/原子替换/无关文件不触发等集成用例 |
| 5 | `think_strip` 触发守卫与 `stripThinkingProcess` 不对称,存在数据损坏向量(2.0 两份 §3.1) | 新增 `thinkShapeGuard`:仅当首个非空 content/text 值以 `<think>` 开头才触发缓冲/剥离,与 `stripThinkingProcess` 对称(`internal/router/response.go:115-134`) |
| 6 | `vmr replay -stream` flag 不生效(2.0 两份 §3.1) | 新增 `adapter.RewriteStream`(与 `RewriteModel` 共用 `topLevelValues`/`spliceValues` splice 路径),真正改写出站 body 的顶层 `stream` 字段(`internal/replay/replay.go:120-134`) |
| 7 | `vmr report` 每记录 body/message/role 统计被桶数放大重复计算 6~8 遍(1.0 §1.I.1 上修,2.0 两份 §3.1) | 引入 `recStats` 结构体,record 循环内只算一次 `bodyBytes`/`messageCount`/`roleChars` 后传各 add 函数(`internal/report/report.go:333-345,517-524`) |
| 8 | `vmr report` 详单/索引产物权限 0644/0755,与审计源 0600/0700 矛盾(2.0 Fable 5 §3.1) | `writeRequestRows`/tag index 的 `os.WriteFile`/`os.OpenFile` 统一改 0600(`internal/report/export.go:414`、`internal/report/detail.go:189`) |
| 9 | `capStr` 字节截断非 rune 安全(1.0 §1.I.6 / 2.0 两份) | 改用 `utf8.RuneStart` 回退最近字符边界(`internal/report/session.go:756-763`) |
| 10 | `diagnose.go::snippet` 同款字节截断问题(2.0 两份新发现) | 同款 rune-safe 截断(`internal/diagnose/diagnose.go:305-317`) |
| 11 | 7~9 处代码/脚本注释仍指向三份已删除的分析文档(1.0 观察 9 / 2.0 两份) | `server.go`/`export.go`/`session.go`/`detail.go`/`vmr.sh` 中的引用全部改为指向设计文档具体章节;`grep` 核实代码/脚本文件中已无残留引用 |
| 12 | 设计文档 §4.3 引用已删除文档未标注"已删除"(2.0 Fable 5 §1.L.1) | 核实设计文档 L443/446/450 三处引用现在均带"该文档已并入本节并删除"字样 |
| 13 | 单把 `api_key` catch-all 无最短长度校验(2.0 两份) | 按 owner 决定整体移除,只保留 `api_keys` 列表(破坏性变更),`config.go` 用 `RemovedAPIKey` 字段捕获旧配置并给出定向迁移错误信息 |
| 14 | 两份 2.0 审计报告文件游离于版本控制之外(2.0 两份互相提及) | 已在 `fb2d760` 中正式提交,不再是 untracked 状态 |
| 15 | `docs/PerformanceTesting_Design_Sonnet5.md` §3 命令路径与目录描述过时(本报告 §2.2,原第一梯队) | §3 命令改为 `go run ./loadtest/mockupstream`,目录描述改为如实的 6 项,并加一句"v1 设计稿,已被 §6 覆盖,仅作决策记录保留" |
| 16 | `sanitizeName` 碰撞后 tag 级 sibling 导出文件静默互相覆盖(本报告 §2.3,原第一梯队) | 按 owner 决定**不做代码修复**,改为在 `config.example.yaml` 的 `api_keys` 说明里加一段:tag 会被 `sanitizeName` 处理成文件名,vmr 不检测两个不同 tag 清洗后撞名,规避责任在配置方——不再作为代码 issue 跟踪,详见 §1.1 |
| 17 | anthropic adapter 客户端未发 `anthropic-version` 时被强制覆盖为硬编码版本(本报告 §2.5,原第一梯队) | 移除 `defaultVersion` 常量与强制 `Set`,客户端不发就不发,与设计文档 §5.4"默认透传"原则一致(`internal/adapter/anthropic/anthropic.go`);`anthropic_test.go` 对应断言同步改为"应保持未设置" |
| 18 | `server.go` 的 `Bearer ` 前缀匹配大小写敏感(2.0 两份,原第二梯队 #6) | 新增 `trimBearerPrefix` 辅助函数,用 `strings.EqualFold` 做大小写不敏感的 scheme 匹配(`internal/server/server.go`);新增 `TestRouterAuthBearerCaseInsensitive` 锁定 `bearer`/`BEARER`/`BeArEr` 三种大小写都能通过鉴权 |
| 19 | SSE 事件分隔符只认 `\n\n`,不认 CRLF 框架(原第二梯队 #7) | **未改判定逻辑本身**(双格式支持成本高、且从未观测到真实触发,ROI 不足),改为低成本观测方案:新增 `noteCRLFFramingIfSuspected`,在"整段响应始终停在 `modeUndecided`"的两个出口(EOF 兜底 `finish()`、`bufferedCap` 溢出降级)检测累积字节里是否出现 `\r\n\r\n`,命中则打 `crlf_framing_suspected` 审计标记(`internal/router/response.go`);新增 2 个测试锁定 EOF 与溢出两条路径都能正确打标记且内容不受损;design doc §5.5 变换清单同步补一行 |
| 20 | `normDescriptions` 缺 `opaque`/`overflow_raw_passthrough` 两个真实会出现的值(原第二梯队 #9) | 补全两行映射,并同步加入本轮新引入的 `crlf_framing_suspected`(`internal/report/detail.go`) |
| 21 | `modelFieldPattern` 对嵌套 `"model"` 字段同样改写,测试注释与实现不符(原第二梯队 #10) | **未做结构性收紧**(需要真正的深度感知扫描器,成本高、现实中未观测到嵌套 model 字段);改为把测试和函数注释改到如实反映现状——`TestRespStream_NestedModelInDelta` 现在真正构造一个嵌套 `"model"` 字段并断言它**也会**被改写(此前只测了顶层,却在注释里声称"仅顶层",从未验证过这句话),函数上方的文档注释同步改为如实描述"无 JSON 深度感知,现实中无害是因为目前对接的厂商响应形状里 model 只出现在顶层" |

### 1.1 `sanitizeName` tag 碰撞——按 owner 决定,不再作为代码问题(原第一梯队 2.3)

- **决定**:静默覆盖的风险本身是真实的,但 owner 判断这不值得在代码里加碰撞检测——改为在配置层面把"tag 会被清洗成文件名,清洗后不能撞名"的约束说清楚,把规避责任交给配置方。
- **落地**:`config.example.yaml` 的 `api_keys` 注释块新增一段,明确说明:①tag 会被 `sanitizeName` 处理(非 `A-Za-z0-9._-` 字符替换为 `-`);②vmr **不检测**两个不同 tag 清洗后撞成同一文件名的情况;③要求配置方选择本身就是文件名安全字符、且清洗后仍彼此不同的 tag 后缀。
- **现状**:`internal/report/export.go`/`internal/report/detail.go` 的实现未改动——碰撞仍会发生,只是现在这是一个文档化的配置约束,而非未文档化的代码缺陷。今后如果 owner 改变主意,方案仍是 §2.3 原文里描述的"加 `used` map + stderr warning"路径,~15-20 行。

### 1.2 `classify.go` 429 quota 嗅探词过宽——核实为非问题,关闭(原第一梯队 2.4)

- **核实过程**:追踪调用链确认——`internal/router/router.go` 里 `ad.ClassifyError(resp.StatusCode, body)` 只在 `resp.StatusCode >= 400` 分支内被调用(`router.go:451` 附近);`DefaultClassify` 内部,quota 词表嗅探(`containsAny(snippet, "insufficient", "quota", "balance", "credit")`)又被包在 `case status == 429:` 分支里(`classify.go:52-55`)。
- **结论**:嗅探逻辑已经严格与 HTTP 状态码绑定两层——`200 OK`(以及任何 `<400` 的响应)根本不会进入这条代码路径,不存在"把成功响应误判"的风险。按 owner 给出的判断标准("如果没有与状态码配合使用,那才是问题"),这一条**确认不成立**,不需要改代码。
- **遗留的次要点(未处理,不在本轮范围)**:429 分支内部词表本身仍是"四选一命中即触发"而非要求与 "insufficient"/"exceeded" 共现——如果未来发现真实 provider 的短时限流消息被误判成长冷却,可以按 §2.3(原报告文本)的思路收紧词表。这是一个独立于"状态码绑定"的次要打磨点,本轮按 owner 指示不处理。

### 1.3 `containsSoftBlockMarker` 仅识别 2 个字段——复核后确认是有意为之的分阶段设计,不是缺陷(原第二梯队 #12)

- **背景核实**:这个函数是为了补上一个 failover 机制天生的盲区——MiniMax 会用 **HTTP 200** 状态码返回一个合规内嵌标记(`input_sensitive`/`output_sensitive`)并给空/替换内容,健康检测和错误分类全部基于状态码,2xx 从不会被送进 `ClassifyError`,所以这类"软拦截"对 failover 完全不可见。
- **设计文档与 `docs/SensitiveWordFilter_Analysis_Fable5.md` 明确记录这是一个两阶段计划的第一阶段**:先用已知的、确定的 MiniMax 字段名把"这类拦截到底多频繁"的数据积累起来(纯观测,`soft_block_detected` 标记,不改字节、不触发 failover、不影响健康),再用真实频率数据决定是否值得投入更复杂的方案(可配置词表、跨厂商扩展、甚至事前请求过滤)。
- **结论**:当前只认 2 个字段是**故意的**,不是遗漏——扩大检测范围在没有真实数据支撑"该扩成什么样"之前,大概率只是在猜。真正的下一步不是改代码,是照设计文档说的:回看已积累的 `soft_block_detected`/`content` 分类样本,把"这个盲区到底多频繁"的数字算出来,再决定是否需要且如何扩展。
- **本轮处理**:不改代码,仅在此归档核实结论,原第二梯队 #12 移除。

---

## 2. 第一梯队(推荐尽快改)

> **状态更新(2026-07-17 01:30)**:本节原有 6 项中的 4 项(2.2 文档路径、2.3 sanitizeName 碰撞、2.4 429 词表、2.5 anthropic 版本覆盖)已按 owner 指示处理完毕,详见 §1/§1.1/§1.2,不再在本节重复。以下 2 项按 owner 指示**暂不处理**,原样保留(2.6 重新编号为 2.2):

### 2.1 [S] `stripThinkingProcess` 强绑定 MiniMax wording,且无任何缓解措施——唯一 [S] 级、仍未动

- **位置**:`internal/router/response.go::stripThinkingProcess`(约 548-600 行附近)。
- **现状核实**:两份 2.0 报告都判定这是唯一的 [S] 级发现,且 Pi Agent 报告给出的"加自动检测告警"方案(3.1.1)**核实未落地**——`grep -rn "pattern_detected"` 在生产代码中无匹配,只有 `response_test.go` 注释里出现"detected"字样(测试注释,非生产实现)。
- **问题**:`Thinking Process:` 头 + `Looks good. Pro(ceed)?` 结束标记全部硬编码。MiniMax 一旦改措辞,thinking=medium 的响应会整段泄漏到用户可见输出,且**没有软降级、没有观测手段**——运维完全不知道剥离已经失效,直到用户报告"AI 把内心独白说出来了"。
- **影响范围**:每一个 `thinking=medium` 请求都跑这条路径,是高频场景,不是边界 case。
- **方案(按 ROI 排序,与两份 2.0 报告结论一致)**:
  1. **【立即做】纯观测告警**:在响应满足"含编号小节(`1.`/`2.`/`3.`)+ 长 content(>1KB)+ 未命中现有 strip 标记"时,往 `Attempt.Norm` 打一个 `thinking_process_pattern_detected` 标记,供 `vmr report` 统计触发频率。~50 行 + 测试,零回归风险(纯观测,不改字节、不影响 failover/健康)。
  2. 视 1 的实测频率决定是否做降级(命中疑似泄漏时退化到 `overflow_raw_passthrough` 同款处理)或加可配置 trigger 关键字——这是产品决策,不建议直接跳过 1 做。
- **工作量**:~50 行 + 1 个测试文件,半天以内。
- **owner 建议**:这是本轮排第一的条目——不是因为改起来难,而是因为它已经被两轮审计连续标记为唯一 [S] 却连最便宜的观测性方案都没有落地。

### 2.2 [M] compaction 链接 `needle` 200 字节上限的漏链无观测

- **位置**:`internal/report/session.go:735-737,751`(`needle`/`capStr`)。
- **现状核实**:`capStr` 本身已 rune-safe(见 §1 已修复项),但 200 字节的截断上限本身未变,且 `linkCompactions` 未匹配时**没有任何日志或标记**。
- **问题**:如果 compaction marker 或会话锚点文本在原文里出现的位置超过 200 字节,该链接会被直接漏检——`vmr-requests-index.md` 里对应的 `Summarizes`/`ContinuesTo` 字段静默留空,排障时无法区分"确实没有关联"还是"链接失败了"。
- **方案(确定)**:未匹配时加一条 debug 级日志(如 `log.Printf("compaction linking: needle %q not found in any session", ...)`),不改变现有匹配逻辑本身。
- **工作量**:~10 行。
- **owner 建议**:纯观测补丁,与 2.1 的"先观测再决定要不要改行为"思路一致。

---

## 3. 第二梯队(改与不改都合理)

> **状态更新(2026-07-17 02:00)**:原第 6 条(Bearer)已修复,移至 §1。原第 7/9/10 条(SSE CRLF、normDescriptions、modelFieldPattern 嵌套)已按深入分析后的方案处理,移至 §1(第 19-21 条)。原第 12 条(containsSoftBlockMarker)复核后判定为**有意为之的分阶段设计,不是待修缺陷**,移至 §1.3 说明,不再留在本节。本节现余 9 项。

1. **[L] `housekeep.go::purgeOne` 在 resume+retention 组合场景下产生误导性 ENOENT 日志**——核实未修(`purgeOne` 仍无 `os.IsNotExist` 判断,`internal/audit/housekeep.go:141-146`)。目录里同时存在 `X.jsonl`(压缩中断残局)与其 `.zst` 且超保留期时,resume 删原文件 + purge 删 `.zst`,随后 `purgeOne` 对同一 `.zst` 再删一次会报错。无实害,纯日志噪音。方案:该分支加 `os.IsNotExist` 静默跳过。

2. **[L] `audit.go::Redact` 浅拷贝非凭证 header 的 value 切片**——核实未修(`out[k] = vs`,`internal/audit/audit.go:210`)。理论上与原 header 共享底层数组,当前无调用方在 Redact 后原地改值,实害为零。方案:需要严格时 clone 切片。

3. **[L] `audit.Attempt.RawPreStrip` 字段类型仍为 `any`**——核实未修(`internal/audit/audit.go:146`)。消费端要类型断言,不利 schema 化。方案:改 `json.RawMessage`。

4. **[L] `strategy.priority.Compare` 用减法比较 `Priority`,极端值可溢出反转排序**——核实未修(`internal/strategy/strategy.go:75`,`a.Priority - b.Priority`)。config 对 `Priority` 取值无范围校验,联动风险同源。方案:改 `cmp.Compare` 零成本消除;或至少给 `Priority` 加合理范围校验。

5. **[L] `core.ErrorClass.String()` 的 `default` 兜底为 `"transient"`,`ErrTransient` 无显式 case**——核实未修(`internal/core/core.go:106-108`)。未来新增枚举忘写 case 会静默变成 "transient",report 包按字符串分桶会被无声破坏。方案:给 `ErrTransient` 显式 case,`default` 返回 `"unknown"`;补全 4 个审计专用值的字符串测试锁定。

6. **[L] 上游 3xx 重定向未显式处理**——核实未修(`router.go` 全文无 `CheckRedirect`)。`http.Client` 默认跟随重定向,POST 301/302/303 会被静默改写成 GET。LLM API 现实中几乎不发 3xx,休眠风险。方案:Transport 配 `CheckRedirect` 不跟随,3xx 当普通响应透传。

7. **[L] `writeRequestRows` 的 `defer f.Close()` 错误被吞**——核实未修(`internal/report/export.go:418`)。磁盘写满时"导出成功"但文件不完整。方案:改成具名返回 + defer 里合并 Close 错误(参考 `housekeep.go::compressFile` 已有的模式)。

8. **[L] `reassembleSSE` 在 `router/response.go` 与 `report/render.go` 两处独立实现**——核实未变(1.0/2.0 一致维持"暂不合并"判断,两侧关注点不同:一个是字节级转发,一个是语义重组)。

9. **[L] `vmr report` 对同一批文件 `Build` 与 `AnalyzeSessions` 各完整读一遍**——核实未修(`cmd/vmr/main.go::cmdReport` 仍是两次独立调用)。GB 级日志下批处理时间翻倍,批处理工具可接受。方案:合并成一趟遍历(改动量较大,>200 行,ROI 不如第一梯队条目)。

---

## 4. 第三梯队(点出即可,不展开)

**核心代码类**:`HealthKey` sha256 前 4 字节截断(碰撞概率可忽略);健康冷却参数(`transientBase`/`longCap` 等)硬编码不可配(是"零调参"的设计选择);`retentionDays` 包级全局 atomic 无测试锁定不变量;`IngressPath` 写死 openai/anthropic 两协议;`${VAR}` 未定义时静默展开为空串(已文档化的预期行为);YAML 解析错误信息不含行号提示;`adminStatus` 对带 zone 的 IPv6(`::1%lo0`)loopback 判断未测(fail-closed 方向,无害);客户端流中途断开(2xx 已提交后)在审计里与完整成功不可区分;流被截断的请求的 tokens/bytes/TTFT 不归入任何端点行;`markdown.go::ms()` 恰好 1000ms 边界的显示格式;`config.example.yaml` 与代码逐项核对一致,无新发现;`RewriteModel`/`RewriteStream` 仍缺 fuzz 测试;scanner 对畸形 JSON(如 `{,}`)的"宽进"依赖上游 `json.Unmarshal` 先行校验这一隐式前提;`contentHint` 的 "sensitive" 命中 "case-sensitive" 等无关措辞的已知取舍;openai/anthropic 两个 adapter 的 Header 拷贝/覆盖逻辑重复约 20 行(体量太小不值得抽公共函数);`detail.go::sanitizeName` 不去重连续 `-`(`+` 量词已合并大部分场景);`fileLinksCell` 裸 HTML 链接。

**脚本/配置/文档类**:`go.mod` 声明 1.25.1 但本机 toolchain 1.26.5,无 `toolchain` 指令(单人项目可接受);`.gitignore` 的 `*.jsonl` 全局忽略对未来测试 fixture 的潜在影响;`vmr.sh` 的 `ExecStart=$BIN start -c $CFG` 未加引号(路径含空格时 systemd 单元损坏,API key 场景不含空格,理论性);`write_env_file` 对含空格/引号的值在 launchd `. env` 下解析失败(同理理论性);`vmr.sh::ensure_bin` 的 `find -newer` 因 `loadtest/` 下的改动触发主二进制重建(无害);`loadtest/` 下 `runner`/`config.yaml`/`gentargets` 三处地址常量(`vmrAddr`/`mockAddr`)需要人工同步(一次性工具可接受,`gentargets` 注释已有提示、`runner` 没有);loadtest 出错中断走 `Process.Kill()`(SIGKILL)而非 Interrupt,留下"有 START 无 STOP"的假崩溃日志痕迹;`loadtest/gentargets/main.go` 的 `out.Write`/`out.Close()` 错误被忽略(磁盘满时 `targets.json` 静默截断);README 的 `admin/status` 示例"无需 api_key"与实现一致,但意味着同机其他用户可读健康拓扑(单机单用户场景可接受,已文档化)。

---

## 5. 执行计划建议

**2026-07-17 01:30 已完成的批次**(按 owner 指示处理,全部改动跑过 `go build`/`go vet`/`go test`/相关包 `go test -race`,全绿):

1. `docs/PerformanceTesting_Design_Sonnet5.md` §3 路径与目录描述更新(原 2.2)。
2. `config.example.yaml` 加 tag 碰撞规避说明,`sanitizeName` 碰撞(原 2.3)按 owner 决定改为配置层文档说明,不再是代码 issue。
3. `classify.go` 429 词表(原 2.4)核实为非问题——已确认严格与状态码绑定,200 OK 不会被嗅探,关闭,未改代码。
4. `internal/adapter/anthropic/anthropic.go` 移除默认版本强制覆盖(原 2.5),同步更新 `anthropic_test.go`。
5. `internal/server/server.go` 的 `Bearer ` 前缀改大小写不敏感匹配(原第二梯队 #6),新增 `TestRouterAuthBearerCaseInsensitive` 回归测试。

**2026-07-17 02:00 已完成的第二批次**(对第二梯队 4 项做深入分析后按分析结论执行,全部改动跑过 `go build`/`go vet`/全量 `go test`/涉及包 `go test -race`,全绿):

6. `internal/router/response.go` 新增 `noteCRLFFramingIfSuspected`,在 CRLF 分帧导致的两个"始终 `modeUndecided`"出口(EOF 兜底、`bufferedCap` 溢出)打 `crlf_framing_suspected` 观测标记(原第二梯队 #7)——**未改** `eventSep` 判定逻辑本身,新增 2 个测试(`TestRespStream_CRLFFramingSuspectedAtEOF`/`...OnOverflow`)锁定标记正确性与内容不受损;design doc §5.5 变换清单同步补一行。
7. `internal/report/detail.go::normDescriptions` 补全 `opaque`/`overflow_raw_passthrough`/`crlf_framing_suspected` 三行映射(原第二梯队 #9,含本批次新引入的标记)。
8. `internal/router/response.go::modelFieldPattern` 的函数注释与 `TestRespStream_NestedModelInDelta` 改为如实反映"无 JSON 深度感知,嵌套 model 字段也会被改写"的实际行为(原第二梯队 #10)——**未做**结构性收紧(需要真正的深度感知扫描器,成本高于现实收益)。
9. `containsSoftBlockMarker` 复核后确认是设计文档写明的分阶段计划第一阶段,不是缺陷,**未改代码**(原第二梯队 #12,归档说明见 §1.3)。

**本轮按 owner 指示暂不处理**:2.1(`stripThinkingProcess` 观测告警,唯一 [S] 级发现)与 2.2(原 2.6,`needle` 漏链观测)。这两项仍是下一批次的推荐候选——尤其是 2.1,已经连续三轮审计(1.0→2.0×2→3.0)被标记为唯一 [S] 级问题,建议下次单独排期处理,不要再等第四轮审计重复确认。

第二梯队 13 项没有强制时间表,建议在下次 touch 到对应文件时顺手处理(如改 export.go 时顺带修 #8 的 Close 错误、改 response.go 时顺带看 #6/#7 的休眠风险是否已经触发过真实 provider 行为)。

第三梯队不建议专门排期,等它们真正在生产触发或恰好 touch 到附近代码时再顺手处理。

---

## 6. 与 1.0/2.0 三份前序报告的关系(供后续审计参考)

- 1.0(`docs/AUDIT_REPORT.md`,当前在工作区被标记删除但未 commit,内容仍可用 `git show HEAD:docs/AUDIT_REPORT.md` 查看)与两份 2.0 报告的全部条目已在本报告中核对合并,不再单独维护三份平行的问题清单——**本报告(V3)是唯一需要跟踪的当前状态**。
- 后续审计应直接在本报告基础上更新条目状态(仿照 1.0/2.0 已经用过的"✅ 已修复 / ⚠️ 仍成立"标注惯例),而不是再起一份从零开始的报告——两轮 2.0 审计各自独立进行导致了大量重复劳动与相互矛盾的结论(如 anthropic 版本覆盖、sanitizeName 碰撞两项,两份报告一份说修了一份说没修,核实后均为"没修"),这本身就是"审计产出不进队列,越审越重复"的例证,值得作为流程教训记录。
