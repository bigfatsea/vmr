<!-- Ver 2026-08-05 (rev 2), by Sonnet 5 -->

# VMR 时区处理现状分析与统一方案

> **范围**：本文档只做分析和方案设计，不改动任何现有代码。覆盖 vmr 的两大半区——路由主干（审计日志写入）与分析半区（`vmr report`/`vmr story`）——凡是涉及"时间怎么捕获、怎么存、怎么显示"的代码点，逐一过了一遍真实源码（不是从设计文档转述），确认下面每条判断都对应当前 `git status` 显示的工作树实际状态。

## 一、任务要求 brief

用户提出的核心诉求，归纳为三条：

1. **要有一个统一的地方读取"系统默认时区"**——而不是像现在这样，各处各写各的。
2. **日志、审计记录、Markdown 报告**这些"写时间"的地方，最终都要按这个系统默认时区来表示时间：
   - **JSON / JSONL 这类原始记录**：必须带明确的时区标识（不能是裸时间戳，读者/程序不知道该按哪个时区解释）。
   - **日志输出、Markdown 报告**这类给人看的呈现层：可以不带时区后缀——因为约定俗成就是"系统默认时区"，不需要每行都重复标注。
3. **一个悬而未决的取舍**：JSON/JSONL 原始记录里，时间到底应该存"GMT（UTC）"还是存"系统默认时区（带偏移量）"？两种各有利弊，需要给出建议。

下面先逐文件排查现状，再回答这三条。

## 二、现状排查（逐文件）

### 2.1 时间捕获层——审计日志怎么写时间

**`internal/audit/audit.go`**
- `Record.TS time.Time` (第 46 行，`json:"ts"`)：请求到达时间。写入方是 `internal/server/server.go:111`：`TS: time.Now()`。
- `Logger.now func() time.Time`（第 470 行）：`New()` 里赋值为 `time.Now`（第 484 行），是个可注入字段（测试用）。文件轮转边界 `l.now().Format("2006-01-02")`（第 516/556 行）也用它。
- **Go 的 `time.Now()` 默认返回 `Local` 时区**（即进程所在 OS/容器的 `TZ` 环境变量或 `/etc/localtime` 决定的时区）。`time.Time` 的默认 `MarshalJSON` 用 `RFC3339Nano` **并保留该 `Time` 自带的 `Location`**——所以 `Record.TS` 序列化进 JSONL 时，**已经是"系统默认时区 + 显式偏移量"的格式**（例如 `"2026-08-05T14:30:00.123456789+08:00"`），不是 GMT，也没有裸时间戳。
- 审计文件轮转（`vmr-audit-YYYY-MM-DD.jsonl` 按天分文件）同样用 `l.now()`（=`time.Now()`=本地时区）算"今天"是哪天。`docs/VirtualModelRouter_Design_v4_Core.md` 第 544 行也明确写着"本地时区，写入时轮转"——这是**已经写进设计文档、且已经正确实现**的既有约定。

**结论**：捕获层（`audit.go`/`server.go`）已经隐式做对了——用的是进程的系统默认时区，JSON 里也带着显式偏移量。问题不在这里，而在于"系统默认时区"这个概念完全是隐式的（散落在每个 `time.Now()` 调用背后），没有一个显式、可读、可测试、可复用的读取点。

**其他捕获点**（`internal/replay/replay.go:438`、`internal/diagnose/diagnose.go:276/395`、`internal/router/router.go` 多处、`internal/report/aggregate.go:430`、`internal/report/detail.go:225`）：全部是 `time.Now()`，同理，隐式用系统默认时区，行为正确但没有统一入口。

### 2.2 JSON/JSONL 原始记录层——是否带时区

- `audit.Record.TS`（JSONL 每行一条）：✅ 带显式偏移量（如上）。
- `internal/report/aggregate.go:130`：`Meta.GeneratedAt: now.Format(time.RFC3339)` → `vmr-report.json` 的生成时间戳。✅ 带偏移量。
- `internal/report/aggregate.go:602-603`：`Meta.From`/`Meta.To`（`vmr-report.json` 里的统计区间）用 `time.RFC3339`。✅ 带偏移量。
- `internal/report/aggregate.go:726`：`RequestRow.TS`（`vmr-requests.jsonl` 每行）用 `"2006-01-02T15:04:05Z07:00"`（等价于 RFC3339）。✅ 带偏移量。
- `internal/report/aggregate.go:955`：Compaction 记录的 `TS`。✅ 同上。

**结论**：JSON/JSONL 这一层**已经全部符合"带显式时区"的要求**，没有发现裸时间戳。这条诉求在当前代码里已经达标，唯一要确认的是"存的到底是 GMT 还是本地偏移"——答案是**本地偏移**（因为源头是 `time.Now()`）。第四节会给出这一点的取舍建议。

### 2.3 展示层——问题集中在这里

这是真正不一致、需要统一的地方。逐个看：

**① `internal/report/requests.go`（`vmr-requests.md`/`vmr-requests-<tag>.md`）——硬编码 UTC+8**

```go
// 第16-17行注释：
// All displayed timestamps are rendered in UTC+8 regardless of the source
// record's own offset.
var utc8 = time.FixedZone("UTC+8", 8*3600)   // 第38行

func fmtUTC8Full(ts string) string { … return t.In(utc8).Format("2006-01-02 15:04:05") }  // 第478-487行
func fmtUTC8Time(ts string) string { … return t.In(utc8).Format("15:04:05") }              // 第489-497行
```

这是**唯一一处**真正做了时区转换的地方，但转换目标是硬编码的中国时区，不是"系统默认时区"。`internal/i18n/report_requests.go` 里的图例文案（中英文都有）也硬编码成"所有时间为 UTC+8"（`ChatUserLegend`），`docs/UserGuide.md`/`docs/UserGuide.zh.md` 同样写死了"UTC+8"。当前团队确实主要在中国时区运营，所以今天看不出问题，但这是对"系统默认时区"这条要求最直接的违反——如果哪天在非 UTC+8 的机器上跑 `vmr report`，展示的时间和机器本地时间会永久错位，且没有任何配置能改。

**② `internal/report/render_doc.go`（`vmr-report.md` 头部统计区间）——完全不转换，还截断出畸形格式**

```go
w("%s\n", t.MetaLine(…, cut(rep.Meta.From, 19), cut(rep.Meta.To, 19)))   // 第75-76行、第215行
```

`Meta.From`/`Meta.To` 是 RFC3339 字符串（如 `2026-07-24T00:17:58+08:00`），`cut(s, 19)` 只是裸截断前 19 个字符，结果是 `2026-07-24T00:17:58`——**保留了原始偏移量对应的时刻，但把偏移量本身砍掉了，还留了个字面的 "T" 分隔符**，既不是"系统默认时区人类可读格式"，也不是机器可解析格式，是两头不靠的中间态。

**③ `internal/story` 全家（`journey.go`/`render_md.go`/`render_spine.go`/`render_compare.go`）与 `cmd/vmr/cmd_story.go`——完全不做时区转换**

```go
// internal/story/render_md.go:34
w("%s", t.JourneyMeta(…, j.From.Format("2006-01-02 15:04:05"), j.To.Format("15:04:05")))
// internal/story/render_spine.go:61/67/75/79/89、render_compare.go:28-29、cmd_story.go:210/238 同理
```

这些地方直接对 `time.Time` 调 `.Format(...)`，**没有 `.Local()`、没有 `.In(...)`，用的是这个 `time.Time` 值自带的 `Location`**。因为 `story`/`report` 是读盘上的 JSONL 反序列化出来的（不是同进程里刚 `time.Now()` 出来的），`time.Parse(time.RFC3339, ...)` 解析后拿到的 `Location` 是**字符串里那个偏移量对应的固定时区**（Go 会生成一个 `+08:00` 这样的匿名 `FixedZone`），**不是运行 `vmr story`/`vmr report` 这台机器当下的系统默认时区**。

单机自用场景下（同一台机器写审计日志、同一台机器跑 `vmr story`、时区从没变过）两者数值上凑巧相等，看不出问题。但一旦跨机器（比如把审计日志拷到笔记本上跑 `vmr story` 复盘，笔记本时区和服务器不同）或者服务器 `TZ` 配置变过，展示出来的时间就是"记录产生时那台机器的时区"，而不是"现在看报告这台机器的系统默认时区"——这既不满足用户"按系统默认时区输出"的要求，也和 ① 的 `requests.go`（强制转 UTC+8）互相矛盾：同一份数据，`vmr-requests.md` 里的时间和 `vmr story` 生成的 Journey 文档里的时间，可能是两个不同的数值。

**④ `internal/report/detail.go`（每请求详情页头部）——唯一显式带偏移量的展示**

```go
rec.TS.Format("2006-01-02 15:04:05.000 -07:00")   // 第414行
```

这一处反而是"最老实"的——直接把记录自带的偏移量原样打印出来（`-07:00`），不掩盖真实来源。但这跟用户的诉求（"日志、Markdown 这类可以不带时区，因为默认就是用户的时区"）方向相反：用户期望的是"统一转换到系统默认时区、然后不用每行都写偏移量"，而这里是"不转换、但每行都写着来源偏移量"。跟 ①③ 对比，三种展示逻辑三个样子，没有一个是彼此一致的。

**⑤ `internal/report/pricing.go`——按小时匹配价格档位，时区语义未定义**

```go
func (r PricingRate) matches(ts time.Time) bool {
    …
    h := ts.Format("15:04")   // 第102/111/115行
    …
}
```

`pricing.yaml` 里配的 `hour_from`/`hour_to`（如"22:00~06:00 时段价"）是运营者按某个时区的心智模型写的（大概率是中国时区的"晚上10点到早上6点"），但这里直接对 `ts`（记录自带偏移量）取小时，**没有转换到任何约定时区**。如果记录的偏移量和 pricing.yaml 作者设想的时区不一致（同样是"跨机器/TZ 变更"场景），价格档位匹配就会算错——这是目前唯一一处"时区不一致会导致金额计算错误"而不只是"显示不好看"的点，优先级应该更高。

**⑥ CLI 实时输出层——已经做对，可以当模板**

- `cmd/vmr/cmd_status.go:166/169/186/201`：`st.Reload.At.Local().Format(...)`、`ep.CooldownUntil.Local().Format(...)`——**显式调用 `.Local()`**，等价于"转换到系统默认时区再显示"，这正是用户想要的模式。
- `cmd/vmr/cmd_start.go:47`（`stampWriter`）、`cmd/vmr/cmd_report.go:26`（`timestampWriter`）：用 `time.Now().Format(...)`（未显式 `.Local()`，但 `time.Now()` 本身默认就是 Local，效果一致，注释里也写明"local time"）。这两处是"当下产生的时间戳"（日志行、进度行），不是"读取历史记录再显示"，所以直接用 `time.Now()` 没有 ③ 那种"记录来源时区 ≠ 当前系统时区"的错位风险。

**结论**：`cmd_status.go` 的 `.Local()` 模式是目前代码库里唯一"正确对齐系统默认时区"的展示写法；`requests.go`/`render_doc.go`/`story` 全家/`pricing.go` 各写各的，互不一致，其中 `story` 和 `report` 两个包对同一批 JSONL 数据算出的显示时间甚至可能对不上。

### 2.4（2026-08-05 复核更正）Journey ID 的 `.UTC()` 其实是个真实疏漏——但修法不是切到 `DisplayZone`

```go
// internal/story/journey.go:529-530（修正前）
start := root.TS.UTC().Format(idTimeLayout)   // idTimeLayout = "20060102T150405"
end := last.TS.UTC().Format(idTimeLayout)
```

本节原判断"这是对的，不需要改"是**错的**——用户在实际使用中真的踩到了这个坑：`vmr story -compare j-openclaw-20260727T160544-...` 里的 ID 时间片段是 UTC，在 UTC+8 的机器上看永远比本地墙钟时间少 8 小时（当天 00:05 显示成前一天 16:05），每次都要心算时区，且这个 ID 直接就是 `journey-<id>.md`/`compare-<id>.vs-<id>.md` 的文件名，`ls reports/stories/` 里看到的全是这种"错位"时间。

真正的修法不是把 `.UTC()` 换成 `.In(fmtutil.DisplayZone)`（第二轮独立复核时先考虑过这个方向，也是 Gemini 3.6 Flash 独立分析给出的建议）——那会让"ID 跨机器稳定"这条本节原本想保护的性质直接失效：`DisplayZone` 是**读取机器**的时区，同一份数据在两台不同时区的机器上跑 `vmr story` 会算出两个不同的 ID 字符串。

最终采纳的修法：**两个 `.UTC()` 全部去掉，直接用 manifest 自带的 `time.Time` 格式化**（不经过任何 `.In()`/`.UTC()` 转换），即：

```go
start := root.TS.Format(idTimeLayout)
end := last.TS.Format(idTimeLayout)
```

这个 `time.Time` 本身携带的偏移量，是这条审计记录**写入那一刻**、写入它的那台服务器自身的本地时区（比如 `+08:00`）——这是数据的属性，不是读取环境的属性，所以：
- **稳定性不受影响**：同一份审计数据不管在哪台机器（哪个时区设置）上跑 `vmr story`，`root.TS`/`last.TS` 解析出来的 `time.Time` 都携带同一个原始偏移量，格式化结果自然一致，本节最初想保护的"ID 跨机器可复现"这条性质完全保留。
- **可读性问题同时解决**：因为团队自己的部署场景是单一时区（中国大陆、无夏令时），"写入时那台机器的时区"在实践中几乎总是等于"现在看报告这台机器的时区"，所以 ID/文件名里显示的数字直接就是用户在 agent 系统上看到的本地时间，不需要心算。

唯一的理论代价：如果未来把来自不同时区服务器的审计日志合并到一起分析（团队当前的自托管单团队场景下没有这个需求），`ls` 的字典序排序在跨偏移量边界处可能不再严格等于时间先后——这是一个已知、可接受的边界情况，不是本次决定的目标场景。

## 三、现状总结表

| 层次 | 代表文件 | 当前行为 | 是否符合诉求 |
| --- | --- | --- | --- |
| 捕获（写入时刻） | `audit.go`/`server.go`/`replay.go`/`diagnose.go` | `time.Now()`，隐式系统默认时区 | ✅ 行为对，但无统一读取点 |
| JSON/JSONL 存储 | `audit.Record.TS`、`report.Meta.*`、`RequestRow.TS` | RFC3339 + 显式偏移量（本地偏移，非 GMT） | ✅ 已带显式时区 |
| 展示 · `vmr-requests.md` | `report/requests.go` | 强制转换到硬编码 `UTC+8` | ❌ 应改为系统默认时区 |
| 展示 · `vmr-report.md` 头部 | `report/render_doc.go` | 裸截断 RFC3339 字符串，不转换、格式畸形 | ❌ 应转换并重新格式化 |
| 展示 · `vmr story` 全部输出 | `story/render_*.go`、`cmd_story.go` | 直接用记录自带偏移量，不转换 | ❌ 应转换到系统默认时区 |
| 展示 · 单请求详情页 | `report/detail.go` | 保留原始偏移量，逐行显式标注 | ⚠️ 语义与其它展示点不一致 |
| 计算 · 价格档位匹配 | `report/pricing.go` | 按记录自带偏移量取小时，时区语义未定义 | ❌ 有算错金额的风险，优先级最高 |
| 实时 CLI 输出 | `cmd_status.go` | 显式 `.Local()` | ✅ 已经是目标模式 |
| 内部 ID 生成 | `story/journey.go` | 显式 `.UTC()` → 已改为直接用记录自带偏移量（不转换） | ❌→✅ 见 2.4 节 2026-08-05 复核更正 |

## 四、核心取舍：JSON/JSONL 原始记录该存 GMT 还是本地偏移量

用户自己提出的两个选项：

- **方案 A：存本地系统时区 + 显式偏移量**（当前实际行为，`time.Now()` 默认结果）。
- **方案 B：统一转成 GMT/UTC 存储**，读取时再按需转换成目标时区显示。

逐条对比：

| 维度 | 方案 A（本地偏移，现状） | 方案 B（统一 GMT） |
| --- | --- | --- |
| 机器可解析的无歧义性 | ✅ RFC3339 带偏移量，任何解析器都能算出绝对时刻，不存在歧义 | ✅ 同样无歧义 |
| 人肉直接读 JSON/JSONL 文件 | ✅ 打开就是当地墙钟时间，不用心算时区 | ❌ 需要手动 +8（或对应偏移）才是本地时间，用户在需求里明确提到这是方案 B 的痛点 |
| 多地不同时区的写入源汇总到一起 | ⚠️ 如果未来出现"多台机器、不同时区都往同一批分析产物写数据"，本地偏移各不相同，肉眼比较原始文本不直观（但程序解析完全没问题） | ✅ 所有原始时间戳文本层面就是同一基准，便于肉眼比较 |
| 与"按天轮转审计文件"这条既有设计的耦合 | ✅ 天然一致——轮转边界就是当地日历日的午夜，`vmr-audit-2026-08-05.jsonl` 这个文件名本身就该是"当地这一天"的记录，用本地时间取 `.Format("2006-01-02")` 直接对；文档也明确写了"本地时区，写入时轮转" | ❌ 若记录本身存 UTC，轮转判断"今天是哪天"还是必须另外用本地时区算一遍，等于要维护两套时钟口径（存储用 UTC、轮转判断用 Local），复杂度不降反升 |
| 改动量 | 零——`Record.TS`/`Meta.*`/`RequestRow.TS` 所有捕获点全部维持 `time.Now()` 不动 | 需要在每个捕获点补一次 `.UTC()`（或在 `audit.Logger`/`report.aggregate` 等处统一转换），并核对没有遗漏，改动面覆盖捕获层全部文件 |
| 团队现状匹配度 | ✅ 团队是 ≤3 人、主要面向中国大陆的自托管单实例部署（见 `CLAUDE.md`"Identity & Philosophy"），不是多地区多时区协作的 SaaS，"多时区写入源汇总比较原始文本"这个 GMT 的核心优势目前用不上 | 面向的是"未来可能扩展成多地区部署"的场景，目前没有这个具体需求 |

**建议：维持方案 A（本地系统时区 + 显式偏移量），不切换到 GMT。**

理由收敛成一句话：**方案 B 唯一的真实优势（多时区写入源的原始文本可比性）在这个项目当前的定位下不成立，而它的代价（改动捕获层全部文件、和"按本地日历日轮转"这条已经写进设计文档的既有约定产生双时钟口径）是实打实的。** RFC3339 带偏移量本身已经解决了"机器解析无歧义"这个问题——GMT 相对于本地偏移量，在机器可解析性上没有增量收益，只是在"肉眼直接读原始 JSON"这一项上此消彼长（GMT 有利于跨时区汇总比较，本地偏移有利于本机直接读）。用户自己在需求里也提到"这两个我都可以接受"，权衡下来本地偏移量胜在**零改动、和现有轮转设计零冲突、当前唯一实际使用场景（自托管单机/单团队）下更直观**。

如果未来团队真的发展出"多地区部署、需要横向比较多台机器原始日志文本"的场景，再切到 GMT 存储也不迟——因为无论哪种方案，展示层都必须做一次显式转换（第五节的方案要求所有展示点都过一遍统一的转换函数），到时候只需要把捕获层的转换目标从"不转换（本地）"改成"转成 UTC"，展示层的统一转换逻辑完全不用动。换句话说，**现在把展示层的统一转换点建好，就是在为将来切换成本最低地留了后路**，不需要现在就为这个假设的未来场景买单。

## 五、统一方案

### 5.1 一个显式的"系统默认时区"读取点

新增（不在本轮实施，仅设计）：

```go
// internal/fmtutil/timezone.go
package fmtutil

// DisplayZone is the process's system default timezone — every
// human-facing rendering of a persisted timestamp must convert through
// this, never through a hardcoded FixedZone and never by trusting a
// parsed time.Time's own embedded offset as-is. time.Local already
// resolves the OS/container TZ setting; this var exists so every call
// site is grep-able and so tests can override it deterministically.
var DisplayZone = time.Local
```

放在 `internal/fmtutil`（不是最初草案里的 `internal/core`——见第七节"与 Gemini 3.6 Flash 独立分析的交叉核对"：`fmtutil` 的既有定位就是"展示格式化，被 `router` 的实时日志和 `report` 的渲染共用"，跟这里要解决的问题完全同构，`story` 也已经在用 `fmtutil.FmtSeconds`）。这是全部方案里唯一新增的抽象，刻意做成一个变量而不是一整套时区配置系统——`CLAUDE.md` 的 KISS/YAGNI 原则下，`time.Local` 本身已经就是"系统默认时区"，不需要重新发明一遍读取逻辑，只需要一个**统一、可发现、可在测试里覆盖**的引用点。

**捕获层**：`audit.Logger.now`、`server.go`、`replay.go`、`diagnose.go`、`aggregate.go`、`detail.go` 里的 `time.Now()` 保持不变——`time.Now()` 本身已经用的是这个时区，不需要额外包一层。

**展示层**：所有"把一个已经持久化的 `time.Time` 格式化给人看，或者用它来算聚合分桶 key"的地方，一律改成先 `.In(fmtutil.DisplayZone)` 再 `.Format(...)`：

- `report/requests.go`：删掉 `utc8`/`FixedZone`，`fmtUTC8Full`/`fmtUTC8Time` 改用 `fmtutil.DisplayZone`（函数名同步改成不带 `UTC8` 字样，避免继续暗示固定中国时区）。
- `report/render_doc.go`：`cut(rep.Meta.From, 19)`/`cut(rep.Meta.To, 19)` 改成先 `time.Parse(time.RFC3339, …)` 再 `.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")`，不再裸截断。
- `report/aggregate.go` 的 `buildRec2`（第 789-790 行）：`date`/`hour` 两个聚合分桶字段改成先 `arec.TS.In(fmtutil.DisplayZone)` 再取 `.Format("2006-01-02")`/`.Hour()`——这条是从 Gemini 报告吸收的点，之前遗漏，详见第七节。
- `story/render_md.go`、`render_spine.go`、`render_compare.go`、`cmd_story.go`：所有 `xxx.TS.Format(...)`/`j.From.Format(...)`/`j.To.Format(...)` 改成先 `.In(fmtutil.DisplayZone)`。
- `report/detail.go`：把 `-07:00` 偏移量后缀去掉，改成和其它展示点一致的"转换到系统默认时区、不带偏移量后缀"，靠文档/图例统一说明一次即可（呼应用户"Markdown 可以不带时区"这条）。
- `report/pricing.go`：`matches()` 里先 `ts.In(fmtutil.DisplayZone)` 再取 `.Format("15:04")`/`.Format("2006-01-02")`，明确 `pricing.yaml` 的 `hour_from`/`hour_to`/`date_from`/`date_to` 都按系统默认时区解释——这条同时也是修正一个"可能算错金额"的潜在 bug，不只是展示层美化。

**JSON/JSONL 层**：不做任何改动——第四节已经论证过，现状（RFC3339 + 本地偏移量）已经符合"原始记录带显式时区"的要求。

**文档/文案**：`internal/i18n/report_requests.go` 的 `ChatUserLegend`（中英文）、`docs/UserGuide.md`/`docs/UserGuide.zh.md` 里"所有时间为 UTC+8"这类措辞，改成通用表述（如"所有时间均为运行 `vmr report`/`vmr story` 这台机器的系统默认时区"），不再写死 UTC+8。

**已更正的点**：`story/journey.go` 里 Journey ID 生成的 `.UTC()`（第 2.4 节 2026-08-05 复核更正）——原方案判断"故意如此不应改"是错的，已改为直接用记录自带的原始偏移量（既不转 UTC，也不转 `DisplayZone`），同时满足"ID 跨机器稳定"与"文件名对人可读"两条诉求。

### 5.2 方案带来的一致性收益

统一之后：
- `vmr-requests.md`、`vmr-report.md`、`vmr story` 生成的所有 Markdown/CLI 输出，对同一条审计记录显示的时间数值永远一致（都过 `fmtutil.DisplayZone`），不会再出现"同一份数据两个报告显示两个时间"的情况。
- 换一台时区不同的机器读同一批 JSONL（比如把日志拷到笔记本上复盘），展示出来的时间自动是"现在看报告这台机器"的当地时间，而不是"当初写日志那台机器"的时区——这才是"系统默认时区"这个词真正该有的语义（当下环境决定展示，而不是历史数据决定展示）。
- `pricing.yaml` 的时段价格规则、`vmr-report.md` 的按小时/按日期聚合图表，都有了明确、单一的时区口径，消除潜在的统计失真和计费误差。

## 六、修改计划

分三批，按风险和依赖顺序排列；每批改完都要跑 `go build ./... && go test ./... -race && go vet ./... && gofmt -l . && go test ./internal/archtest/...`。

**第一批 · 基础设施（无行为影响，纯新增）**
1. 新增 `internal/fmtutil/timezone.go`：`var DisplayZone = time.Local`，附上如 5.1 所述的注释说明。
2. `internal/fmtutil` 补一个测试验证 `DisplayZone` 默认值及可覆盖性（`DisplayZone` 是包级 `var`，测试可以临时替换成 `time.FixedZone(...)` 再用 `defer` 还原，不需要额外的注入机制）。

**第二批 · 展示层与聚合层改造（有行为影响，需要重新生成 golden fixture）**
3. `internal/report/requests.go`：删 `utc8`/`FixedZone`，`fmtUTC8Full`→`fmtDisplayFull`、`fmtUTC8Time`→`fmtDisplayTime`（或类似改名），改用 `fmtutil.DisplayZone`；同步删掉文件头部"All displayed timestamps are rendered in UTC+8"的注释，改写成引用 `fmtutil.DisplayZone` 的表述。
4. `internal/report/render_doc.go`：`Meta.From`/`Meta.To` 的展示改成 parse-then-format，不再用 `cut`。
5. `internal/report/detail.go`：详情页头部时间格式统一，去掉逐行偏移量后缀。
6. `internal/report/pricing.go`：`matches()` 补时区转换；补一个"记录偏移量与系统默认时区不同"场景下的单测，确认转换后小时判断正确（当前测试里 `zone := time.FixedZone("CST", 8*3600)` 这类 fixture 正好可以复用来构造"记录时区≠展示时区"的用例）。
7. `internal/report/aggregate.go` 的 `buildRec2`：`date`/`hour` 两个聚合分桶字段先转到 `fmtutil.DisplayZone` 再提取——这是从 Gemini 报告吸收的点（第七节），比纯展示问题优先级更高，因为影响的是报告本身的统计数字而不只是某一行文字；补一个"记录偏移量≠ `DisplayZone`"场景下的分桶单测，确认一条本会跨日历日边界的记录落进正确的 `byDate`/`hoursOfDay` 桶。
8. `internal/story/render_md.go`、`render_spine.go`、`render_compare.go`、`cmd/vmr/cmd_story.go`：所有展示用的 `.Format(...)` 前补 `.In(fmtutil.DisplayZone)`。
9. 受影响的 golden fixture 重新生成并人工核对一遍：`internal/story/testdata/golden.md`、`golden_zh.md`，以及 `report`/`story` 各处 `_test.go` 里内嵌的期望字符串（`detail_test.go`、`session_test.go`、`journey_test.go`、`aggregate_test.go` 等——这些测试目前用 `time.FixedZone("CST", 8*3600)`/`time.UTC` 构造输入，展示层改动后要确认它们的期望输出是否需要同步，或者干脆把 fixture 时区统一换成一个与 `fmtutil.DisplayZone` 无关的第三方时区，专门用来验证"转换确实发生了"而不是凑巧数值相等）。

**第三批 · 文案、文档与 `CLAUDE.md` 原则同步**
10. `internal/i18n/report_requests.go`：`ChatUserLegend` 中英文措辞去掉 "UTC+8" 字样。
11. `docs/UserGuide.md`/`docs/UserGuide.zh.md`：同步措辞。
12. `docs/VirtualModelRouter_Design_v4_Analytics.md`：补一节说明"展示层统一按 `fmtutil.DisplayZone`（系统默认时区）渲染，原始 JSON/JSONL 保留写入时的本地偏移量"，把本文档第四节的取舍结论落成正式设计记录，避免以后被当成"看起来奇怪的行为"重新审视。`docs/VirtualModelRouter_Design_v4_Core.md` 第 544 行"本地时区，写入时轮转"那句可以加一句交叉引用（按项目约定，引用文档名+段落名，不用编号）。
13. `CLAUDE.md`（项目根，checked-in 那份）：在"Invariants to not accidentally break"或"Conventions"里补一条时区处理原则——概括为："系统默认时区是唯一权威时区（`time.Local`，经 `fmtutil.DisplayZone` 引用）；原始 JSON/JSONL 记录保留写入时的本地偏移量（RFC3339 自带，不转 GMT）；所有展示层（日志行、Markdown 报告、CLI 输出、聚合分桶）渲染前必须显式 `.In(fmtutil.DisplayZone)`，不得依赖某个 `time.Time` 碰巧带的原始偏移量，也不得硬编码固定时区"，并点名 `story` 的 Journey ID 生成用 `.UTC()` 是故意的例外。

**风险与注意事项**
- `internal/archtest` 对 `report`/`story` 部分文件有行数预算（见模块地图 `archtest` 一行），改动前用 `go test ./internal/archtest/...` 摸一下当前余量，`requests.go`/`render_doc.go`/`pricing.go`/`aggregate.go` 这几个文件本轮只是替换实现、不新增代码行数，预计不会碰到预算上限；`journey.go`/`render_md.go`/`render_spine.go` 每处改动只是加一个 `.In(...)` 调用，同理影响很小。
- `report`/`story` 两个包各自维护独立的 `Finding`/展示逻辑（`archtest` 强制不共享），本次改动要在两边分别做，不能指望改一处两边都生效。
- 全部改完后要做一轮全局复查：`grep -rn "FixedZone\|UTC+8" --include="*.go" internal cmd`（排除 `_test.go` 里刻意构造 fixture 的用法）应该只剩测试文件命中；再用 `grep -rn '\.TS\.Format\|\.TS\.Hour()\|Meta\.From\|Meta\.To' --include="*.go" internal/report internal/story cmd/vmr` 逐个核对每个命中点是否都经过了 `fmtutil.DisplayZone`，确认没有漏网的展示/聚合点。
- 改完第二批后，**必须**用真实（或构造的跨时区）审计数据跑一遍 `vmr report`/`vmr story`，肉眼确认 `vmr-requests.md`、`vmr-report.md`、`vmr story` 输出的时间彼此一致，且都等于当前机器的本地时间——这是本轮改动最终要达成的可验证目标，光靠单测通过不够，需要手工过一遍真实产物（对应 `CLAUDE.md`"Verification: Run relevant tests or perform manual verification after changes"）。

## 七、与 Gemini 3.6 Flash 独立分析的交叉核对

用户另外让 Gemini 3.6 Flash 跑了一份独立分析（`timezone_analysis_gemini_3_6_flash.md`），核对后处理如下：

**吸收（真实遗漏，已并入第六节修改计划）**
- **`internal/report/aggregate.go` 的 `buildRec2`**（第 789-790 行）：`date: arec.TS.Format("2006-01-02")`、`hour: arec.TS.Hour()`，直接从记录自带的原始偏移量提取"日期"和"小时"，用来给 `byDate`/`hoursOfDay`/`HourRow` 这些聚合分桶算 key。这是本报告第 2.3 节遗漏的一个点——之前只查了"展示格式对不对"，没查"聚合分桶用的时间口径对不对"。这个比纯展示问题更严重：如果记录偏移量和当前系统默认时区不一致，受影响的不是某一行文字的显示，而是 `vmr-report.md` 里"按小时活跃度""按日期活跃度"这些图表本身的统计数字会被分进错误的桶。已并入第六节第二批修改范围。
- **`internal/fmtutil` 而不是 `internal/core` 作为 `DisplayZone` 的落脚点**：Gemini 建议新建 `internal/tz` 包或放进 `fmtutil`。新建一个包对这么小的一个变量是过度设计，但"放 `fmtutil` 而不是 `core`"这个选址建议本身是对的——`fmtutil` 的既有定位就是"展示格式化，被 `router` 的实时日志和 `report` 的渲染共用"（模块地图原文），跟本次要解决的"CLI 日志 + Markdown 报告都要按同一个时区显示"完全同构；且 `story`/`render_md.go`/`render_compare.go` 已经在用 `fmtutil.FmtSeconds`，加一个 `DisplayZone` 进去不引入新的包依赖边界。第五节原方案里的落脚点 `internal/core` 相应调整为 `internal/fmtutil`。

**核实后判定为不成立（未采纳）**
- **`internal/audit/housekeep.go` 的 `time.Parse("2006-01-02", date)`**：Gemini 认为这里因为 Go 对无时区信息的 layout 默认解析成 `time.UTC` 而存在偏差。实际读代码确认：`todayDate`/`cutoff`/文件名里的 `date` 全部只是"纯日历日期字符串"（不带时分秒），且 `today`→`todayDate` 的 `Format` 和 `date`→`cutoff`/`t` 的 `Parse` 全部走同一套"先转字符串、再解析回来"的路径，两边的 `time.Time` 值虽然名义 Location 都是 `UTC`，但比较的双方来源完全对称，`.Before()` 比较的实质就是纯日历日先后关系，跟 Location 标签是 `UTC` 还是别的什么完全无关。这是一个误报，不需要改。

**未采纳（超出本次诉求范围）**
- **`report.yaml`/`config.yaml` 增加显式 `timezone` 配置项、多级 fallback（配置 > 系统默认 > UTC 兜底）**：用户在原始需求里明确说的是"读取系统默认的时区"，不是"允许用户配置任意时区"；引入配置项和优先级链是给一个没人要求的场景（多时区可配置化）预先铺路，违反 `CLAUDE.md` 的 YAGNI 原则，也让"一个统一读取点"变成"一套时区解析系统"，偏离了用户想要的简单性。`time.Local`（Go 内置，读取 OS/容器的 `TZ`/`/etc/localtime`）本身不存在"加载失败"的情况（那是 `time.LoadLocation("Asia/Shanghai")` 这类具名时区才有的失败模式，本方案不用具名时区），所以 Gemini 提的"加载失败兜底 UTC"这条也不适用。
- **`FormatLocal`/`FormatLocalMS`/`FormatISO8601` 这一组固定格式的包装函数**：现有各展示点用的时间格式本来就彼此不同（详情页要毫秒、轮次表要时分秒、文件名要紧凑无分隔符、Journey 列表要月日+时分），强行收敛成三个固定格式函数要么覆盖不了现有格式多样性，要么还是得让调用点各自传 layout——那样封装就没有实际收益。改成调用点直接 `t.In(fmtutil.DisplayZone).Format(既有layout字符串)`，只加一次 `.In(...)`，不新增格式化 API 面。

其余 Gemini 报告里提到的点（JSON/JSONL 现状排查、`render_doc.go` 的 `cut()` 问题、`story` 全家不转换问题、`cmd_status.go` 的 `.Local()` 正确范例、GMT vs 本地偏移量的取舍结论）跟本报告第二~四节的判断一致，不需要额外动作。

## 八、结论速览

- **JSON/JSONL 原始记录**：已经符合"带显式时区"的要求，建议**继续存本地系统时区 + 偏移量**，不切 GMT——GMT 唯一的优势（多机汇总时原始文本可比性）在当前单团队自托管场景下没有实际收益，代价却是要动全部捕获点、还和"按本地日历日轮转审计文件"这条既有设计打架。
- **展示层与聚合层（日志/Markdown/统计分桶）**是本轮真正要修的地方：`report/requests.go` 硬编码 UTC+8、`render_doc.go` 裸截断不转换、`story` 全家和 `pricing.go` 完全不转换、`detail.go` 显式带偏移量、`aggregate.go` 的 `buildRec2` 用原始偏移量算聚合分桶 key（吸收自 Gemini 报告的独立发现）——五种写法各不相同，同一份数据在不同产物里可能显示不同时间，甚至统计图表本身会失真。统一方案是新增一个 `internal/fmtutil.DisplayZone`（= `time.Local`）作为唯一读取点，所有展示/聚合代码过这一层再 `.Format`/`.Hour()`，原始 JSON/JSONL 完全不动。
- `story/journey.go` 里 Journey ID 原本用 `.UTC()`——2026-08-05 复核确认这是个真实疏漏（用户实际踩到），已改为直接用记录自带偏移量（不转换），同时保住"ID 跨机器稳定"与"文件名人类可读"两条诉求，见第 2.4 节。
