<!-- Ver 2026-08-31 03:00, by Opus 5 -->

# `vmr analyze` 全形态样例报告 —— 生成计划 / 执行记录 / Review 总结

本文件一式三份内容：**第一部分**是生成计划（要产出哪些形态、用什么命令、喂什么日志），**第二部分**是执行记录（实际跑了什么、耗时、产物清单），**第三部分**是以用户视角对产物做的全面 review 与总结。

---

## 第一部分 · 生成计划

### 1.1 产物形态全集（来自 `vmr analyze -h` + `docs/VirtualModelRouter_Design_v4_Analytics.md`）

`vmr analyze` 是唯一入口，模式由三个互斥的"变焦选择器"（`-journey` / `-compare` / `-corpus`）加三个互斥的"默认套件裁剪开关"（`-macro-only` / `-list-only` / `-story-only`）决定。可产出的文件形态穷举如下：

**宏观半区（report half）** —— `-macro-only` 或默认套件产出：

| 文件 | 说明 | 触发条件 |
| --- | --- | --- |
| `vmr-report.md` | 九个编号章节的聚合报表（§0 摘要 → §8 详单入口 + 附录） | 默认套件 / `-macro-only` |
| `vmr-report.json` | 同上的数据层，`meta.format = 10` | 同上 |
| `vmr-requests.md` | 逐请求索引（按 Chat User 分组，纯索引） | 同上 |
| `vmr-requests.json` | 索引数据层（`requests` = `RequestRow[]`） | 同上 |
| `vmr-requests-<tag>.md` | 每个 client_key_tag 一份，真正的 Session→Task→Turn 展开 | 同上 |
| `vmr-requests-cron-<class>.md` | 单发定时脚手架（heartbeat / dream_diary）单独成文件 | 同上 |
| `vmr-requests-failed.md` / `.jsonl` | 失败请求单独索引 + 机读副本 | 同上 |
| `tool-waste.html` | **HTML**：独立的工具 schema 浪费卡片（自包含、零外部请求） | 同上，`len(rep.Tools) > 0` |
| `details/*.md` | 逐请求详单（承载完整对话正文，0600） | `-details` / `-journey` / `-render-all` |
| `evidence/*.md` | 按内容哈希去重的 system prompt / 工具声明证据 | 同上 |

**叙事半区（story half）** —— 落在 `{out}/stories/`：

| 文件 | 说明 | 触发条件 |
| --- | --- | --- |
| `vmr-stories.md` / `.json` | 候选 Journey 索引（**每次运行都写**，无论什么模式） | 任何非 `-macro-only` 模式 |
| `journey-<id>.md` / `.json` | 单 Journey 叙事 + 九项行为指标 + Findings + `structure` 骨架 | `-journey` / `-render-all` / `-story-only` |
| `journey-<id>.html` | **HTML**：单页看板（判定条 · 结构时间轴 · 指标 grid + SVG sparkline · Findings） | `-journey` 单命中 + `-html` |
| `journey-<id>-partial.md` / `.json` | 断头 Journey（文件名后缀是不稳定性的自我声明） | `-include-partial` |
| `compare-<a>-vs-<b>.md` / `.json` | 双 Journey 行为剖面对比 + 13 项标量 diff + 分叉点 + Extras | `-compare` |
| `compare-<a>-vs-<b>.html` | **HTML**：对照看板（两侧头 · 分叉点 · A/B 差异表 · 事实 · LLM 段） | `-compare` + `-html` |
| `vmr-story-corpus.md` / `.json` | 语料级统计（分布 / Finding 命中率 / Spearman 相关性 Top-15） | `-corpus` |
| `.llm-cache/` | LLM 解读结果磁盘缓存 | `-llm-addr` + `-llm-cache-dir` |

**正交修饰开关**：`-lang en|zh`（全部文案）· `-redact`（HTML 脱敏）· `-currency CODE`（成本折算）· `-include-partial` · `-details` · `-include-self-traffic` · `-show-ungrouped` · `-llm-addr`（解读层）。

### 1.2 日志范围

- **FULL**：`logs/vmr-audit-*.jsonl*` —— 44 个文件、约 700 MB 压缩（2026-07-14 ~ 2026-08-28）。用于宏观报表。
- **RECENT**：`logs/vmr-audit-2026-08-1[6-9]*` + `logs/vmr-audit-2026-08-2*` —— 11 个文件（08-16 ~ 08-28）。用于叙事半区，含 110 个候选 Journey（59 task / 37 cron / 14 heartbeat），足够撑起 `-corpus` 的样本量门槛。

`.parse-cache` 用一份 master（`sample_reports/_cache/`）在每次运行前拷进目标 outDir，避免每组重复解析 700 MB 语料。

### 1.3 选定的 Journey / 对比样本

| 代号 | ID | 特征 |
| --- | --- | --- |
| `J_MAIN` | `j-pimini-20260825T165640-20260825T191943-9773d372` | 110 轮真实编码任务（"为项目添加 /log 页面的可行性分析"） |
| `J_BIG` | `j-pimini-20260824T155922-20260825T114737-b0ebb0e4` | 518 轮、缝合 ×8 —— 压测 compaction / 缝合边界渲染 |
| `CMP_1` | `...T155235-...d8078303` vs `...T162804-...8b5f2af4` | **同一句初始指令**（"本机启动了 CLIProxyAPI 你找一找…"），5 轮 vs 75 轮 —— 对比模块最理想的样本 |
| `CMP_2` | `...20260824T140001-...7b100aed` vs `...20260825T140000-...e928cdf5` | 同一个 cron 任务连续两天的两次执行 |

### 1.4 生成矩阵（21 次运行）

| # | 输出目录 | 命令要点 | 语料 |
| --- | --- | --- | --- |
| 01 | `01-macro/{zh,en}` | `-macro-only` | FULL |
| 02 | `02-macro-details/zh` | `-macro-only -details` | RECENT |
| 03 | `03-list-only/{zh,en}` | `-list-only` | RECENT |
| 04 | `04-suite/{zh,en}` | 默认套件（无选择器） | RECENT |
| 05 | `05-story-all/zh` | `-story-only -render-all` | RECENT |
| 06 | `06-journey/{zh,en}` | `-journey J_MAIN -html -details` | RECENT |
| 07 | `07-journey-big/zh` | `-journey J_BIG -html -details` | RECENT |
| 08 | `08-journey-redact/zh` | `-journey J_MAIN -html -redact` | RECENT |
| 09 | `09-journey-llm/zh` | `-journey J_MAIN -html -llm-addr` | RECENT |
| 10 | `10-compare/{zh,en}` | `-compare CMP_1 -html` | RECENT |
| 11 | `11-compare-cron/zh` | `-compare CMP_2 -html` | RECENT |
| 12 | `12-compare-llm/zh` | `-compare CMP_1 -html -llm-addr` | RECENT |
| 13 | `13-corpus/{zh,en}` | `-corpus` | RECENT |
| 14 | `14-partial/zh` | `-list-only -include-partial` | RECENT |
| 15 | `15-currency/zh` | `-macro-only -currency CNY` | RECENT |
| 16 | `16-journey-multi/zh` | `-journey 'j-pimini-*'`（多命中批处理路径） | RECENT |

---

## 第二部分 · 执行记录

### 2.1 生成过程中遇到的问题

**G-1（真实缺陷，CLI 层）· `-llm-addr ""` 无法用来关闭 `report.yaml` 里配置的 LLM 解读层**

本机 `report.yaml` 配了 `llm_addr`，于是**每一次** `-journey`/`-compare` 都会默认发起 LLM 调用。想在批量生成时关掉它，最自然的写法是显式传空值 `-llm-addr ""`——但 `cmd/vmr/cmd_analyze.go:212` 的守卫是：

```go
llmAddrExplicit := flagPassed(fs, "llm-addr")
if llmAddrExplicit && (*corpusFlag || !hasSelector) { return fmt.Errorf("-llm-addr is not supported with -corpus or the default suite ...") }
```

它判的是"**这个 flag 有没有被敲**"，不是"**解析出来的地址是不是非空**"。结果：`vmr analyze -macro-only -llm-addr ""` 被拒绝，理由却是"会对每个 journey 各打一次 LLM 调用"——而用户传的恰恰是"一个都不要打"。同一行还有个副作用：`-corpus`/默认套件下明明 `report.yaml` 的 `llm_addr` 会被正常忽略（不报错），显式传空值反而报错，行为不自洽。

修法一行：`if llmAddr != "" && (*corpusFlag || !hasSelector)`（`llmAddr` 是已解析值）。

**绕过方式（本次采用）**：`-report-config sample_reports/_no-llm.report.yaml`，指向一份只有 `language:` 的 sidecar 配置。

### 2.2 实际执行

见 `sample_reports/_gen.log`（每组带 `rc` / 耗时 / 目录体积）。汇总表在下方 2.3。

### 2.3 产物汇总

21 次运行全部 `rc=0`，总耗时约 31 分钟，产出 **2.2 GB / 22831 个文件**（不含 `.parse-cache`）：22341 个 `.md`、461 个 `.json`、**18 个 `.html`**、9 个 `.jsonl`。

| 目录 | 耗时 | 体积 | 目录 | 耗时 | 体积 |
| --- | --: | --: | --- | --: | --: |
| `01-macro/zh` (FULL) | 195s | 79M | `08-journey-redact/zh` | 16s | 66M |
| `01-macro/en` (FULL) | 198s | 79M | `09-journey-llm/zh` | 40s | 66M |
| `02-macro-details/zh` | 58s | 199M | `10-compare/zh` | 17s | 64M |
| `03-list-only/en` | 12s | 61M | `10-compare/en` | 17s | 64M |
| `04-suite/zh` | 219s | 224M | `11-compare-cron/zh` | 14s | 63M |
| `04-suite/en` | 221s | 224M | `12-compare-llm/zh` | 26s | 64M |
| `05-story-all/zh` | 192s | 202M | `13-corpus/zh` | 160s | 61M |
| `06-journey/zh` | 16s | 66M | `13-corpus/en` | 162s | 61M |
| `06-journey/en` | 16s | 66M | `14-partial/zh` | 13s | 61M |
| `07-journey-big/zh` | 80s | 79M | `15-currency/zh` | 58s | 67M |
|  |  |  | `16-journey-multi/zh` | 142s | 132M |

（每个目录含一份 61M 的 `.parse-cache` 拷贝，实际净产出体积应各减去这一块。另有 `sample_reports/_recheck/` 是用新编译二进制做的复核产物，见 2.4。）

**18 个 HTML 的分布**：`tool-waste.html` × 9（跑过宏观半区的 6 个目录 + `_recheck` 的 3 个）、`journey-*.html` × 5（`06-zh` / `06-en` / `07-big` / `08-redact` / `09-llm`）、`compare-*.html` × 4（`10-zh` / `10-en` / `11-cron` / `12-llm`）。全部单文件自包含、零外部请求。

**1.4 计划矩阵的兑现情况**：16 组全部执行，其中 03（`-list-only`）只跑了 `en`（`zh` 版在探索阶段已单独跑过并用于挑选样本，脚本里未重复）；其余按计划出中英双份或单份。

---

### 2.4 方法论问题：本次生成用了过期二进制，已复核并修订

**G-2（我自己的执行失误，记在这里以免误导）**：整批样例是用仓库里现成的 `./vmr` 生成的，它构建于 **2026-08-30 21:03**，而 main 上最后一个功能提交是 **`c13819a`（2026-08-31 02:26，"feat(analyze): trim macro-report bulk, add honesty footnotes"）**。也就是说，**产物少了最后一次提交的改动**。

发现时机：review 到语料统计时，我断言"1.57 登记的均值偏斜脚注不存在"，与 `internal/i18n/story_corpus.go:58` 的源码矛盾——顺着这个矛盾才查出二进制过期。

**已做的复核**：`go build -o vmr.new ./cmd/vmr`，用新二进制重跑 `-macro-only`（FULL 语料）与 `-corpus`（RECENT 语料）到 `sample_reports/_recheck/`，逐条复验受影响的发现：

| 发现 | HEAD 上的状态 |
| --- | --- |
| R5b-1 均值偏斜脚注 | **已修复，发现撤回** |
| R1-1 按日成本表丢行 | 脚注已加，**丢行本身仍在**，发现降级保留 |
| R1-2 `%%` 未转义 | 仍在 |
| R1-3 §3 低样本仍打 ⚠️ | 仍在 |
| R1-4 §0 无成本 top-line | 仍在 |
| R1-5 `-macro-only` 零 stories 链接 | 仍在 |
| R1-7 §6.7 保留比 >100% | 仍在（4 行） |
| R1-11 44 个文件路径铺满首行 | 仍在（1726 字符） |
| R1-12 §3/§4 端点行 187 行 | 仍在 |
| R3a-2 工具浪费总量不在 Markdown 里 | 仍在 |
| 章节结构 | 新旧完全一致（`diff` 无输出） |

叙事半区的发现（R4-*/R3b-*/R5-*）大多是**源码级**核实的（`stepContextPoint`、`compare_metrics.go` 的 `DeltaRel` 注释、`cronFileTag`），不受二进制版本影响；行为级的（R4-12 全失败 journey、R3b-3 verdict 选取）对应的代码在 `c13819a` 中未被触及。

**教训**：仓库里躺着的二进制不等于 HEAD。下次先 `go build` 再生成。

## 第三部分 · Review

### 3.1 Review 计划

分六批读，每批一个侧重点，读完即记录，不重复读同类同名文件：

| 批次 | 读什么 | 侧重点 |
| --- | --- | --- |
| R1 · 宏观骨架 | `01-macro/zh/vmr-report.md` 全文 + `01-macro/en/vmr-report.md` 抽查 | 九个章节的信息价值、顺序、术语解释、中英一致性；`§0` 摘要能不能一眼看懂 |
| R2 · 索引与下钻 | `vmr-requests.md` + 1 份 `vmr-requests-<tag>.md` + 1 份 `vmr-requests-cron-*.md` + `vmr-requests-failed.md` + 2 份 `details/*.md` + 1 份 `evidence/*.md` | 索引→分组→详单的导航链条是否闭合；Session→Task→Turn 的树形结构是否清晰；死链 |
| R3 · HTML 看板 | `tool-waste.html` + `journey-*.html` + `compare-*.html` + `-redact` 版 | 布局、可读性、主题适配、脱敏是否彻底；对比是否真的两边对照 |
| R4 · 单 Journey 叙事 | `06-journey/zh/stories/journey-*.md`（含决策脊柱、时序图、Findings）+ `07-journey-big` 抽查 compaction 段 + `.json` 的 `structure` | 树形结构（Journey→Task→Step）呈现、Finding 措辞、九项指标是否自解释 |
| R5 · 对比与语料 | `10-compare/zh/stories/compare-*.md` + `.json` + `13-corpus/zh/stories/vmr-story-corpus.md` | 左右对照是否成立、分叉点表达、相关性表的可读性与免责声明 |
| R6 · 索引/边界形态 | `03-list-only`、`14-partial`、`15-currency`、`16-journey-multi`、`05-story-all` 的差异面 | 模式之间的输出差异是否符合预期；是否有空列/占位符/静默降级 |

R1 之后每批读完立刻把发现追加到 3.2；3.3 是最后的用户视角总结。

### 3.2 逐批发现

约定：**[缺陷]** = 逻辑/正确性问题；**[缺口]** = 用户要的信息报告没给；**[布局]** = 信息在但不好读。方括号后标 `←1.NN` 表示与 `KNOWN_ISSUES` 已登记项相关。

---

#### R1 · 宏观报表 `01-macro/zh/vmr-report.md`（2773 行，语料 15825 条记录 / 44 天）

**R1-1 [缺陷] §2「按日估算成本」表静默丢掉 11/44 天，其中包含全窗口最忙的一天** ←1.58

`by_date` 里 44 天齐全，但 §2 的按日成本表只渲染出 33 行。缺的是 `2026-07-25/26/27`、`08-04/11/15/16/18/24/25/28`。核对 `vmr-report.json`：

```
{'date': '2026-08-25', 'requests': 885, 'tokens_in_fresh': 6500413, 'tokens_out': 645012}   ← 没有 cost_estimate 键
{'date': '2026-08-23', 'requests':  54, 'tokens_in_fresh':  471129, 'tokens_out':   3350, 'cost_estimate': 0.279}
```

`2026-08-25` 是全窗口请求量最高的一天（885 请求），因为当天端点全部落在定价表外，`cost_estimate` 缺失，整行就从表里消失了。同一份报告的 §5「按日期活跃度」图里这 11 天全都在。**读者只看 §2 会得出"8-23 之后就没流量了"的错误结论。**

这比 1.58 登记的"覆盖度隐性依赖当日端点定价"更严重：1.58 记的缓解措施是"已加『成本未知≠零』脚注"，但脚注解决不了**整行不存在**——读者根本不知道有东西被拿掉了。项目自己在 §2.5 立过"缺数据不伪装成零、渲染 `-` 而非 `0`"的纪律，这里等于把纪律的另一半（缺数据也不能伪装成"不存在"）漏掉了。

**HEAD 上的现状（用新编译的二进制复核后修订）**：`c13819a` 已为这张表加了脚注——

> 仅列出存在可定价记录的日期；其余日期有请求流量，但其上游端点未解析出适用单价，当日成本未知（并非 0）。

**但 33 行的事实没变，问题只被削弱、没有解决**：脚注告诉读者"有些日期被省了"，没告诉他**是哪些日期**，更没告诉他**被省掉的里面包含全窗口最忙的一天**。一个想回答"这个月哪天最烧钱"的读者，看到的表最后一行是 8-23，而真正的流量峰值 8-24/8-25 连行都没有。

**改法**：这 11 行照常渲染，`估算成本` 列填 `-`，脚注改成"11/44 天因当日端点不在定价表内无法估算，已渲染为 `-`"。

**R1-2 [缺陷] 附录里漏出未转义的 `%%`**

```
- 比值低置信度: cache_efficiency 等比值指标的分母 / 总请求数 < 90%% 时标注脚注 ¹。
```

源头 `internal/i18n/report_doc.go:100`（zh）与 `:160`（en），两语言都是。这个字段是当常量直接拼进 Markdown 的，没走 `Sprintf`，所以 `%%` 原样输出。一字符修复。

**R1-3 [缺陷] 低样本的判定纪律在 §3 与 §4 之间自相矛盾**

- §4「按端点」延迟表：n<20 一律标 `⚠️low-n`，明确抑制解读——做得对。
- §3「端点健康」表：同一批端点，`google-abfs:gemini-3.5-flash | 尝试 2 | 成功 1 | 可用度 50.0% | 错误率 50.0% ⚠️` ——**n=2 的样本被打上了和 `sensenova:deepseek-v4-flash`（n=301，错误率 20.6%）同一个 ⚠️ 警告标记**，没有任何 low-n 标注。

§6.5 还专门写了"任一组样本量低于 20 时表格仍渲染但不给结论"。同一份报告里对"低样本"存在三套处理（§3 照常警告 / §4 标 low-n / §6.5 不给结论），读者无法建立统一预期。**改法**：§3 的 ⚠️ 加 n≥20 门槛，低于门槛的行沿用 §4 的 `⚠️low-n` 写法。

**R1-4 [缺口] 整份"用量报告"没有一个总成本数字**

§0 摘要表五列是「请求 / 成功率 / 计费输入(fresh) / 缓存效率 / p95 耗时」——**没有钱**。§2 的四张成本表（按日/按模型/按端点/按客户端）**每一张都没有合计行**。用户想知道"这 44 天一共花了多少"，唯一办法是自己把 33 行加起来（而且如 R1-1，那 33 行还是不全的）。

一份叫"用量报告"的东西，top-line 缺了用户最关心的那个数，这是本次 review 里最大的信息缺口。**改法**：§0 摘要加一列「估算成本」，§2 每张表加合计行，并在合计旁标注"其中 N 天/M 个端点无定价，未计入"。

**R1-5 [缺口] 宏观报表到叙事半区零链接，宏观→中观的变焦断了**

`grep -n "stories\|journey" vmr-report.md` 在 `-macro-only` 产物里**零命中**。§6「会话与任务」列出上千个会话，每行给的是 `s432 (l-5642b5c3)` —— 一个会话序号加一个 lineage 内容寻址 id。这两个 id **都不能拿去 `vmr analyze -journey` 用**（journey id 形如 `j-lobster-20260825T1656...`），报告里也没有给出映射关系。

设计文档 §0 明确说"读者的提问天然会跨倍率移动……导航链接正是为了让这种移动零成本"。实测下来，`-macro-only` 这条路径上宏观→中观是**死路**：在 §6 看到一个 105 轮、带 2 次 fallback 的可疑会话，想深挖只能回到命令行重新 `-list-only` 列一遍候选、再靠标题和时间人肉配对。

**改法**：§6 会话表加一列 journey id（或链接），哪怕只在默认套件下有值、`-macro-only` 下显示为"运行 `vmr analyze -list-only` 获取"。

**R1-6 [缺口] §6 会话表没有时间列**

表头是「会话 / 标题 / 轮 / 任务 / fresh/cached/out / 结果」。定位一个会话的最自然线索——它什么时候发生的——不在表里。`vmr-stories.md` 的候选索引反而有时间范围。用户记得的是"上周三那个 SSH 的任务"，不是 `s1562`。

**R1-7 [缺陷] §6.7 Compaction 还原表里有"保留比 > 100%"的行，且无任何解释**

```
| 2026-07-14 22:28:34 | - | - |   445 →   475 | 107.0% | - |
| 2026-08-24 11:03:46 | - | - |   246 → 1.1K  | 431.0% | - |
| 2026-08-20 14:03:03 | - | - |   954 → 1.5K  | 159.0% | - |
```

一张标题叫「Compaction 还原」的表里出现"压缩后比压缩前还多 4.3 倍"。这三行的「压缩会话」「续接会话」「吞掉的实体」三列**全是 `-`** ——除了时间和两个 token 数什么都没有。要么是把非 compaction 的边界误判成了 compaction（更可能，样本都极小、245→1.1K 这种量级），要么就该加一句脚注说明 >100% 在什么情况下是合法的。现状是读者只能自己怀疑报告算错了。

同一张表里 `l-77c20384 | - | 0 → 0 | - | ~/ENV.md, TOOLS.md, ...(+27 more)` **逐字重复出现 3 次**（只有时间不同），`0 → 0` 的保留比显示 `-`。这些行不携带任何可行动信息。

**R1-8 [缺陷] §0 自动亮点没挑出最大的浪费项**

§0 亮点报的是 `tools:60/4b60dc93` —— 44.83 MB schema、利用率 12.0%。但 §7 的「工具形态浪费 Top-5」第一行是 `tools:67/7bc83937` —— **499.84 MB**，是前者的 11 倍。亮点挑的是"利用率最低"，不是"浪费最多"。对一个想知道"钱花在哪里浪费了"的用户，这个选择让他低估了一个数量级。**改法**：亮点按浪费绝对量挑，或者两条都报。

**R1-9 [缺陷/配置] self-traffic 排除会静默失效，报告不提示**

`vmr-requests-vmrstory.md` 存在，且 `vmrstory` 出现在 §2 按客户端成本表里（933.3K fresh）——那是 vmr 自己的 LLM 解读流量。默认应当被排除，排除标识来自 `report.yaml` 的 `llm_key`（自动反推 tag）**或** `self_traffic_client_tags`（显式 tag 列表，与 `llm_key` 独立）；本次换的 sidecar 配置两者都没配，self-traffic 排除**静默失效**，报告里没有任何一行说"本次未排除自分析流量"。**改法**：§0 或附录固定输出一行"self-traffic 排除：已启用（匹配 N 条）/ 未启用（未配置 llm_key / self_traffic_client_tags）"。（`self_traffic_client_tags` 已能做到配置层解耦，不必新造机制——详见问题 4。）

**R1-10 [缺陷] `obster` 这个 client 有数据但没有对应的分组文件**

§2「按客户端估算成本」里有一行 `obster | 37.2K | 17 | 0.0112 CNY`。核实：`obster` 是审计日志里真实的 `client_key`（不是渲染把 `lobster` 写坏），对应唯一一条会话 `l-471ec11c`——`class: heartbeat`、`requests: 1`、`2026-07-16`，即用户某次 key tag 少打首字母、发了一次 heartbeat poll。它没有 `vmr-requests-obster.md`、也不在 §0/§8 链接列表里，因为 `partitionGroups` 把「单发定时会话」一律并入 `scheduled[class]` 滚动桶（这条请求进了 heartbeat 的 cron 文件），不进 `chatUser`。而 §2/§5 的 ByClient 表对全部记录无差别聚合，所以成本表里有它。**是「单发定时剥离」与「ByClient 无条件聚合」两种口径不一致，不是数据丢失，也不是 off-by-one。** 报告未提示这个差异。

**R1-11 [布局] 44 个输入文件路径在正文第 3 行和附录里各铺一次，各占约一屏**

标题下第三行就是 44 个 `logs/vmr-audit-*.jsonl.zst` 全路径连成的一整段，附录里再原样铺一遍。**改法**：正文写"44 个文件 · 2026-07-14 ~ 2026-08-28 · format 10 · 15825 条记录（0 坏行）"，完整清单折进 `<details>`。

**R1-12 [布局] §3/§4 的端点表各 70 行平铺，一半是 n≤20 的噪声**

§4 已经把它们标成 `⚠️low-n` 了——既然已经判定"不足以解读"，就没有理由让它们和主力端点抢同样的视觉权重。**改法**：low-n 行折进 `<details><summary>另有 N 个低样本端点</summary>`。

**R1-13 [布局] §5.5 按客户端分组用字母序，不是流量序**

`aiscript`（6 请求）排第一，`lobster`（数千请求）排第五。§2 的按客户端成本表是按成本降序的——同一份报告里同一个维度两种排序。

**R1-14 [布局] 逐小时/逐日活跃度用 `mermaid xychart-beta`，44 个 x 轴标签**

`xychart` 至今是 mermaid 的 beta 特性，GitHub 与多数编辑器的渲染支持不稳定；纯文本阅读（`less`/`cat`）时读者看到的是一行 44 个数字的数组。44 个日期标签在任何渲染器里都会挤成一团。**改法**：图下补一张紧凑表格或 ASCII sparkline 兜底，至少保证降级后信息不丢。

**R1-15 [布局/术语] `⭐` 在 §0 第一张表就出现 4 次，解释在 2760 行外的附录**

附录写了「⭐ 标记: 该列为衍生/预估指标」。但读者在第 10 行就撞上「计费输入(fresh)⭐」「缓存效率⭐」。**改法**：§0 表下一行小字直接给出 ⭐ 的含义。（与已登记的 1.6「§2.5 标记符号已达四个」同源但不同处——1.6 说的是符号数量，这里说的是首次出现处缺解释。）

**R1-16 [术语] §1 同一张表里两个百分比用了两种分母写法**

`输入-缓存命中 … 90.0% of in` 与 `输入-fresh … 10.5% of (fresh+cached)`——其实是同一个分母。两种写法让读者以为是两个口径，还要自己验算 90.0 + 10.5 ≠ 100 是不是哪里错了（实为各自四舍五入）。统一成一种写法即可。

**R1-17 [做得好] 值得保留的几处**

- §2.5「额度与消耗对照」的 ¹/²/† 三条脚注：把"重算值 vs 实时计数器"两个窗口讲得非常清楚，还专门给了"两段时间毫无交集"的 † 标记。这是全报告最诚实的一段。
- §8「本次运行未生成 `details/*.md`」不是简单地留死链，而是给出 `vmr replay -print -req <坐标>` 的等价取数路径。
- §6.5 Sticky 有效性：93.0% vs 22.0% 的对照 + "不解释切换原因"的自我限定，是"按结果度量、不按机制"这条设计纪律的漂亮落地。
- §6.6 端点性价比的"失败耗时只记时间、不折算成钱"。
- §2.5 无定价账户渲染 `-` 而不是 `0`。

**R1-19 [缺陷] 英文报告里混入中文全角括号**

`01-macro/en/vmr-report.md` 的 §0 摘要：`| 15825（fallback 197 / trunc 43） | 99.1% | ... |`。全文只此一处（其余全角括号都是用户会话标题的原文透传，属正确的 passthrough）。英文串里出现 `（）` 是 i18n 文案漏改。

除此之外 EN/ZH 结构完全对齐（`## ` 章节数 15:15），未发现漏译段落。

**R1-18 [布局] §6.6 的排序键与它自己的洞察不匹配**

表按「成本/1M out」升序排。但这一节真正的爆点是 `minimax:MiniMax-M3` 的**失败耗时 6792.8s（≈1.9 小时）**和 `bai:deepseek-v4-flash` 的 1678.5s ——这两个数字被排到了第 2 行和最后一行。无定价的 `-` 行沉底，视觉上像是"最贵的"。

---

#### R2 · 索引与下钻链条 `02-macro-details/zh`（RECENT 语料，4851 条详单 + 57 份证据）

**R2-1 [做得好] 详单页是整套产物里设计最扎实的一份**

单份 `details/*.md`（336 行）的结构：结果头 → `← 返回索引` + `上一轮` 链接 → **VMR 路由前判断**（所需能力 / 预估 token）→ ① Client→VMR（headers/参数折叠，tools 与 system prompt 走 `../evidence/` 去重链接）→ 角色 token 占比 → **`↺` 历史 / `🆕` 本轮新增**分界 + 历史指回上一轮详单 → ② VMR→上游**逐项 diff**（`🟢 新增 / 🔴 删除 / 🔶 变化`，headers 18 项标出 4 处变化、body 5 项标出 1 处）→ 模型输出 → `原始 SSE：3 个事件，28.0KB —— 按坐标取回原文：vmr replay -print -req ...`。

这份页面把"这一次上游到底收发了什么字节"回答得非常干净，尤其是 ①→② 的 diff 视图——用户一眼能看到 vmr 到底改了什么（`model: "agent" → "gemini-3.7-flash-high"`、`Authorization` 换 key、剥掉 `Connection`/`Content-Length`），这正是"三处 sanctioned deviation"这条设计红线的可视化证明。

**R2-2 [缺陷] tool_call 参数在详单页里是一坨转义 JSON，正好在最该可读的地方**

一次 `write` 调用的 args（20.1k 字符）被原样打成单行 JSON：

```
{"content": "// Ver 2026-08-16 02:50...\n\n# PC Server ... > **设计基准日期**...& ...",
 "path": "/Users/stanford/.../plan_v1.0_custom-2-agent.md"}
```

两个问题叠加：① `\n` 未还原，一份 20KB 的 Markdown 交付物挤成一行；② `>` 和 `&` 被渲染成 `>` / `&`（Go `json.Marshal` 默认的 HTML 转义，应改用 `Encoder` + `SetEscapeHTML(false)`）。

讽刺的是设计文档 §3.5b 已经为**决策脊柱**解决过这个问题（"有一个字段扛负载就展示那一个字段的完整值……起代码块展示"），但**详单页没有复用这套逻辑**。而 `write` 类调用恰恰是"最终交付物"所在——用户下钻到详单，十有八九就是想看这个。**改法**：详单页的 tool_call args 复用 `render_spine_args.go` 的"挑扛负载字段 → 代码块渲染真实换行"策略。

**R2-3 [缺陷] 三套 session 标识符互不相通，且 `sNNN` 不跨运行稳定**

同一条会话在三份产物里有三个名字：

| 产物 | 写法 |
| --- | --- |
| `vmr-report.md` §6 | `s432 (l-5642b5c3)` |
| `vmr-requests.md` 索引行 | `l-4ae1f6b2/t01` |
| `stories/vmr-stories.md` | `j-lobster-20260816T024545-...-caa60eed` |

更麻烦的是 `sNNN` 是**按本次输入范围重新编号**的：同一条 lobster 会话在 FULL 语料下是 `s432`，在 RECENT 语料下变成 `s03`。它看起来像 id，实际不能跨运行引用，报告里也没说明这一点。**改法**：要么在 §6/索引里直接给 journey id，要么明确标注 `sNNN` 是"本次报告内的行号，不是稳定标识"。

**R2-4 [缺陷/文档] `vmr-requests.md` 不是设计文档所说的"纯索引"**

设计文档 §2.5 写："`vmr-requests.md` 是一份纯索引……真正的 Session→Task→Turn 展开只存在于每个分组自己的文件里"。实际产物在按 Chat User 分组的摘要之后，还接了一整节 `# 全部请求（时间序）`——**RECENT 语料下 4851 行，FULL 语料下 2.2 MB**。

这一节本身有价值（跨 client 的全局时间线是分组文件给不了的），但它让"纯索引"这个定位不成立，也让文件大到不适合当索引用。**改法**：要么把全局时间线拆成 `vmr-requests-timeline.md`，要么在文档里改口径。二者取一，现状是文档与实现互相打脸。

**R2-5 [布局] `[Ⓜ️ Markdown]` 在每一行重复 4851 次**

「文件」列每行都是 `[Ⓜ️ Markdown](details/2026...md)`。链接文字对所有行完全相同，不携带信息，却占掉整列宽度。**改法**：链接文字用坐标（`vmr-audit-2026-08-16.jsonl:33`）或干脆 `详单`，把 emoji 留给真正有区分度的列。

**R2-6 [缺口] Task 层没有汇总行**

`vmr-requests-lobster.md` 的树形结构本身是对的（`## s03 (l-4ae1f6b2) · 时间 · 1 任务 14 轮` → `### t01 · 时间 · 14 轮` → 引用用户原话 → 逐轮表），层级清晰。但 **Task 标题行只给了轮数**，没有这个任务的总耗时、总 token、成本、用了哪些工具、最终 finish 状态。用户扫这份文件是想找"哪个任务贵/慢/出错了"，现在只能自己把 14 行加起来。**改法**：`### t01` 下补一行 `> 14 轮 · 3m12s · fresh 120K / cached 800K / out 5K · ≈0.8 CNY · 工具 exec×9 read×3 · 结束于 stop`。

**R2-7 [术语] 表头 `msgs` 未定义**

分组文件顶部的图例写「每轮表列: 轮 | 时间 | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | 文件」——只是把列名重复一遍，没有定义。`msgs` 从 3→5→7→9 递增，是**累计**消息数而非本轮新增，读者要看两行才能推出来。同理 `finish` 的取值（`tool_calls` / `stop`）也没有解释。

**R2-8 [布局] 详单页里 `<strong>` 与 `**` 两种加粗混用**

`> 本轮调用: <strong>write</strong> · trace <strong>247b03ee...</strong>` 与同一份文件里的 `**System Prompt**（55.2k 字符）` 并存。`<summary>` 内部必须用 HTML 标签（Markdown 不解析）是合理的，但这一行在普通引用块里，用 HTML 只会让纯文本阅读时看到裸标签。

同一页还有两处小瑕疵：`← 返回 [vmr-requests.md](...)` 之后紧跟一行以 ` · 上一轮:` 开头——分隔符前面空无一物（"下一轮"链接缺席时留下的悬空分隔符）；以及 `#1–#62 为历史上下文（↺）,#63 起为本轮新增（🆕）` 里中文句子用了半角逗号。

**R2-9 [做得好] `evidence/` 的内容哈希去重**

57 份 `sysprompt-*.md` / `tools-*.md` 覆盖 4851 条请求。一份 909 字符的 system prompt 只落一次盘，详单页用 `→ [sysprompt-d2dbaf7a.md](../evidence/...)` 引用。这是对"每轮重发完整历史"这个数据特性最正确的处理方式。

---

#### R3a · `tool-waste.html`（宏观半区唯一的 HTML 产物）

**R3a-1 [做得好] 这是整套产物里传达效率最高的一页**

一屏之内：72px 的 `68%` + 一句话说明（"本报表窗口内你随每次请求发出的工具定义字节，从未被调用的占比"）→ 四块 stat（`累计发出 977.13 MB` / `其中死重 661.02 MB` / `≈ 浪费 token 165.3M` / `工具集形态 21`）→ 逐工具集表格，每行一条利用率进度条 + 从未调用的工具名（列 4 个，其余折成"等 N 个"）+ 指纹小字。内联 CSS、`prefers-color-scheme` 双主题、`@media (max-width:640px)` 降成两列、零外部请求、页脚注明"单文件自包含"。脚注还老实交代了"token 数为粗估（≈4 字节/token）"。

**R3a-2 [缺陷·本次最严重的信息缺口] 这四个总量数字在 Markdown 报表里一个都没有**

```
$ grep -c "977\.13\|661\.02\|165\.3M" vmr-report.md
0
```

`vmr-report.md` §7 只给了 `schema_bytes_shipped | 44.83 MB | tools:60/4b60dc93/461 请求`——那是**某一个工具集形态**的数字；下方 Top-5 表里最大的单行是 499.84 MB。**全窗口总计 977 MB 发出 / 661 MB 死重 / 68% / 1.65 亿浪费 token 这四个数，只存在于 HTML 卡片里**。

这是本次 review 发现的最大问题。理由：

1. Markdown 报表是主产物，HTML 卡片是"可分享的副产物"。主产物缺了副产物的结论，等于把最有价值的洞察藏在了次要文件里。
2. 这四个数字回答的是这个产品最能打的一个问题——"我的 Agent 每次请求都在为从不调用的工具付钱，一共付了多少"。1.65 亿 token 在任何定价下都是真金白银，而 §2 的成本估算总额（如 R1-4 所述）本身还缺失。
3. 它同时解释了 R1-8：§0 的自动亮点挑的是"利用率最低"的 `tools:60`（12%，44.83 MB），而不是浪费最多的 `tools:67`（31%，499.84 MB），更不是总量。三个口径在三个地方，读者拼不出全貌。

**改法**：把 HTML 卡片那四块 stat 原样搬进 `vmr-report.md` §7 的开头，作为该章的 top-line；§0 亮点同时报"总死重"和"最低利用率"两条。

---

#### R6a · `03-list-only/en/stories/vmr-stories.{md,json}`（候选索引，126 条）

**R6a-1 [缺陷] `Tasks` 列 126/126 行全是 `—`**

```
| ID | Client | Time Range | Tasks | Steps | Title | Rendered |
| j-lobster-2026...-f024d796 | lobster | 08-16 02:39 → 08-16 02:42 | — | 14 | ... | — |
```

任务数只有在真正构建 Journey 之后才知道，而 `-list-only` 按定义就是"不构建"。所以这一列在这个模式下**永远是空的**——但表头照常渲染，读者看到的是"这个字段坏了"。同理 `Rendered` 列也是 126/126 全空（该模式不渲染任何 journey，符合预期，但同样呈现为一列破折号）。

**改法**：`-list-only` 下直接不输出这两列；或在表头下加一句"本模式不构建 Journey，`Tasks`/`Rendered` 不可用"。

**R6a-2 [缺陷·量化了 R1-9] self-traffic 静默失效的实际规模：126 条候选里 16 条是 vmr 自己的分析流量**

```
Counter({'lobster': 92, 'vmrstory': 16, 'pimini': 13, None: 3, 'openclaw': 1, 'dummy': 1})
```

同一条命令、同一批日志、同一份 `.parse-cache`，只因为 sidecar 配置里没有 `llm_key`：
- 用真实 `report.yaml`（含 `llm_key`）→ **110** 个候选（task 59 / cron 37 / heartbeat 14）
- 用 `_no-llm.report.yaml`（无 `llm_key`）→ **126** 个候选（task **75** / cron 37 / heartbeat 14）

12.7% 的候选凭空多出来，全是 `vmrstory` 这个 tag，而**两次运行的输出里没有任何一个字提示口径变了**。用户拿两份报告对比时会以为是数据本身变了。

这条比 R1-9 更严重的地方在于：它影响的不只是成本表的一行，而是**整个候选集合的规模**——`-corpus` 的统计分母、Finding 命中率、相关性样本量全都跟着变。

**改法**：`vmr-stories.md`/`vmr-report.md` 固定输出一行口径声明，例如"self-traffic 排除：已启用（llm_key 匹配，排除 16 条）"或"未启用（未配置 llm_key）"。

**R6a-3 [缺陷] 3 条候选的 `Client` 为 `null`，表格里渲染成空白**

`Counter` 里 `None: 3`。JSON 里 `client` 字段缺失，Markdown 表格对应单元格是空的——既不是 `-` 也不是 `(unresolved)`。宏观半区对同一类记录用的是 `## Chat User: (unresolved)`，叙事半区留空。两个半区对"无 client 标签"的呈现口径不一致。

**R6a-4 [做得好] heartbeat 折叠 + 标题里的 `⚠` 前缀**

14 条 `[OpenClaw heartbeat poll]` 全部折进 `<details>`，主表只留有实质工作流的 task/cron——这正是设计文档 P14 那条"仅 heartbeat 属于噪声"的判据落地。标题前的 `⚠` 标记（断头 journey）也在，但**表头/图例没有解释 `⚠` 是什么意思**，读者只能靠猜。

---

#### R2b · 失败索引与定时任务文件

**R2b-1 [做得好] `vmr-requests-failed.md` / `.jsonl` 的定位说明写得很清楚**

开头一句话就把边界划干净了："专供错误分析：outcome 为 error / canceled，以及 outcome=ok 但 truncated（流中途断了）的全部请求……**不影响其他报表**——这些记录在 `vmr-requests.md` 及其分组 sibling 文件里照常出现，本文件只是额外的索引。" 配套的 `.jsonl` 每行带 `req` 源坐标 + `detail_file`，是整套产物里机读友好度最高的一份。

**R2b-2 [缺口] 失败索引是纯时间序平铺，不做聚类，看不出"哪次是一场雪崩"**

```
| 2026-08-16 04:00:46 | l-37d8bfb3/t01 | ❌network | 32ms  |
| 2026-08-16 04:00:46 | l-37d8bfb3/t01 | ❌error   |  6ms  |
| 2026-08-16 04:00:47 | l-37d8bfb3/t01 | ❌error   |  6ms  |
| 2026-08-16 04:01:19 | l-eea8f9f5/t01 | ❌error   |  6ms  |
... 共 8 条集中在 04:00:46 – 04:02:21
```

65 条失败里有 8 条挤在 2 分钟内、几乎全是 6ms 的即时失败——这显然是一次上游整体不可用引发的连环重试，而不是 8 个独立事故。表头只写了"共 65 条"。**改法**：开头加一段聚类摘要（按时间邻近 + error_class 聚合），例如"65 条失败聚成 12 簇，最大一簇 8 条（08-16 04:00–04:02，network→error）"。

**R2b-3 [布局/术语] `❌error` 读起来像"错误：错误"**

同一列里并排出现 `❌network` / `❌client` / `❌error` / `canceled`。前两个是错误分类，第三个是"没有更具体分类"的兜底，但呈现上完全同构，读者会以为 `error` 也是一个具体类别。

**R2b-4 [缺陷] `vmr-requests-cron-hartbeat.md`：文件名拼 `hartbeat`，文件内容拼 `heartbeat`**

```go
// cronFileTag ... "heartbeat" keeps the exact spelling the operator
// specified for this file ("hartbeat") ...
func cronFileTag(class string) string {
    if class == "heartbeat" { return "hartbeat" }
    return sanitize(class)
}
```

这是一个刻意行为。**更正**：这个拼写并非"只存在于源码注释里"——`docs/UserGuide.md` 与 `docs/UserGuide.zh.md` 双语都写明了（"`heartbeat`'s file is `vmr-requests-cron-hartbeat.md` specifically"）。KNOWN_ISSUES 没登记，但 UserGuide 已经是面向用户的正式记录。真问题只是文件名 `hartbeat` 与文件内标题 `heartbeat` 不一致，容易让用户以为是错别字。**改法**：统一拼成 `heartbeat`（含 UserGuide 双语同步）。详见问题 19。

---

#### R6b · 默认套件 `04-suite/zh`（RECENT，224 个 journey 文件 + 4851 份详单）

**R6b-1 [对 R1-5 的修正与加强] 套件模式确实有一条到叙事半区的链接，但只有一条，而且是最没用的那条**

`vmr-report.md` 第 6 行：

```
任务叙事见 [stories/vmr-stories.md](stories/vmr-stories.md)（126 个任务索引 · 覆盖 2026-08-16 02:39:56 – 2026-08-28 02:34:32）
```

所以 R1-5 说的"零链接"只对 `-macro-only` 成立。但套件模式的情况其实更让人惋惜——**§6 会话表里 `grep -c "journey-j-" = 0`，224 个 `journey-*.md` 就躺在隔壁 `stories/` 目录里，一条都没被链上**。

而这个 join **是现成的**：`vmr-stories.json` 的每条 journey 都带 `lineages: ["l-ee98ffdf"]`，正是 §6 表里 `s04 (l-ee98ffdf)` 括号里的那个 id。实测：

```
§6 里的 lineage id 去重后 506 个，其中 142 个能在 vmr-stories.json 的 lineages 里直接命中 journey id
例: l-577fed4d → j-lobster-20260825T143130-20260825T143520-4f8f4ac2
```

**142 个会话的宏观→中观跳转是一次字典查表的距离，产物里却要用户回命令行重新列一遍候选、再靠标题和时间人肉配对。** 设计文档 §0 把"变焦移动零成本"写成了这一半产品的核心主张，这是它落地最不到位的一处。

**改法**：§6 会话表加一列（或把「会话」列本身变成链接），命中 journey 的行链到 `stories/journey-<id>.md`，未命中的留 `-`。渲染期两个半区都在同一个进程里，数据现成。

**R6b-2 [观察] 套件模式的 `sNNN` 编号又变了一套**

同一条会话 `l-a37ea776`：FULL 语料 `-macro-only` 下是 `s1929`，RECENT 语料 `-macro-only -details` 下是 `s373`，RECENT 语料默认套件下还是 `s373`。印证 R2-3：`sNNN` 随输入范围浮动，不能当引用锚点用。

---

#### R4 · 单 Journey 叙事 `journey-*.md` / `.json`（样本：518 轮 / 39 任务 / 缝合×8 的 `j-pimini-...b0ebb0e4`）

**R4-1 [做得好] 转义是干净的**

先排除一个误判：这份 1.88 MB / 35258 行的文件里有 248 个 `## ` 开头的行，其中 243 个来自被内联的工具结果与交付物正文——**全部正确地包在代码围栏内**，用严格的围栏解析器核对后只有 5 个属于 journey 自己（`System Prompt` / `概览` / `决策脊柱` / `最终交付物` / `工具调用时序图` / `疑似问题`）。文档大纲没有被用户内容污染。（naive 的 `grep "^## "` 会给出 137 个"泄漏"的假象——这是 grep 的问题，不是产物的问题。）

**R4-2 [缺口·严重] `journey-*.md` 里一个行为指标都没有**

```
$ grep -c "净工作时长\|重复动作率\|计划/执行比\|上下文有效利用率" journey-*.md
0
$ python -c "... json ..." → metrics keys:
['model_ms','agent_exec_ms','human_idle_ms','net_working_ms','model_to_tool_ratio',
 'tool_call_count','tool_call_distribution','duplicate_action_rate','output_repetition_rate',
 'error_recovery_count','plan_exec_ratio','context_composition_curve','context_utilization',
 'compaction_count','compaction_loss_tokens','model_usage','model_switches']
```

**17 项指标全在 JSON 里，Markdown 一项都没渲染。** 设计文档把"九项规则派生指标"称作叙事半区的核心（"零 LLM 成本、确定性、跨框架可比"，四类使用场景里有三类直接依赖它），而人读的主产物完全没有它们。用户想知道"这个任务的时间到底花在模型推理还是工具执行上""重复动作率多少""上下文有效利用率多少"，只有两条路：读 JSON，或者额外跑一次 `-html`。

**改法**：`## 概览` 之后加一节 `## 行为指标`，把 17 项渲染成一张表 + `context_composition_curve` 的 ASCII sparkline。数据现成，是纯渲染层缺失。

**R4-3 [缺陷] 162 条 Finding 平铺编号，其中 110 条来自同一个检测器**

```
Counter({'unverified_entity_reference': 110,   # 68%
         'reasoning_action_mismatch':     20,
         'exact_repeat_tool_call':        10,
         'unused_tool_result':             8,
         'plan_execution_misalignment':    7,
         'constraint_text_dropped_at_compaction': 7})
```

两个问题：

① **呈现**：162 条按 StepSeq 顺序平铺成编号列表，不分组、不计数、不排序。前 6 条全是 `unverified_entity_reference`，措辞逐字相同，只有实体名不同。读者翻到第 20 条就会放弃——这正是项目自己在宏观报表 §7 `provider_quota_exhaustion` 那里论证过的失败模式（"一个会对正确配置持续报警的检测器，只会训练用户忽略整个章节"）。同一条判断在叙事半区没有被应用。

② **`unverified_entity_reference` 疑似系统性误报**。这个 journey 的 t02 标题就是 `read @docs/ACM_PROJECT_PRINCIPLE_v2.0.md and @docs/ACM_PROJECT_SPEC_FINAL_v1.1.m…`，而 Step 2 的 Finding 说：

> 已被证伪的实体：`docs/ACM_PROJECT_SPEC_FINAL_v1.1.md`, `ACM_CONFIG_MANAGEMENT_DEEP_DIVE_AND_BEST_PRACTICES.md`, `docs/ACM_SPEC_CONSISTENCY_REVIEW.md`

被"证伪"的正是这一步**成功读到的**文件。后续几条命中的是 `config.toml`、`base_url`、`api_key`、`os.replace`、`ChangeSet`——这些是从**文档正文里抽出来的名词**，不是任何一次真实的文件系统探测。判据是"tool_result 里命中 ENOENT/404/not found 等字面标记，其实体在此后仍被引用"；当 tool_result 是一篇讨论错误处理的设计文档时，文档里出现 "not found" 这几个字就足以让整份文档的全部实体被标成"已被证伪"。

设计文档 §3.5a 记录了四处校准修复（`reasoning_action_mismatch`、`plan_execution_misalignment`、`unused_tool_result` 各一到两处），**`unverified_entity_reference` 不在其中**。110/162 的占比说明它需要同一轮校准。**改法**：证伪信号要求出现在 tool_result 的"结果状态"位置（或与被引用实体处于同一行/同一 JSON 字段），而不是整段文本里任意位置的子串命中。

**R4-4 [做得好·全套产物里最诚实的一段] Finding 章节开头的协议盲区声明**

> ⚠️ 本 journey 全部请求均为非 Anthropic Messages 协议。以下信号依赖仅 Anthropic Messages 协议才会填充的字段，在本 journey 上结构性无法触发——**未出现不代表检查过没问题**：`error_retry_unadapted`, `error_then_unverified_success`, `error_recovery_count`, decision spine's tool-result ❌ badge, `structure.json`'s `ToolCalls[].ResultError`

把"没记录 ≠ 没发生"落到了具体的检测器名单上，而不是一句笼统的免责。这一段应该被复制到宏观报表和 corpus 统计里去。

**R4-5 [缺陷] 头部时间范围的结束时刻没有日期**

```
> 39 任务 · 518 轮 · 2026-08-24 15:59:22 → 11:47:37
```

`11:47:37` 早于 `15:59:22`，读者必须自己推断"哦，是第二天"。这个 journey 实际跨了 20 小时。**改法**：跨日时结束时刻补上日期。

**R4-6 [缺口] 概览没有总耗时 / 总 token / 成本**

`## 概览` 给的是起始时刻、首个转折点、结束时刻、标签、模型使用表。没有：总墙钟时长、净工作时长、总 token、成本估算。"模型使用"表逐行给了 in/cached/out，但没有合计行——8 行相加才能知道这个 journey 一共烧了约 60M token。（默认套件不穿 `*pricing.Resolver`，所以成本行在这个模式下确实拿不到；但 token 合计是纯加法。）

**R4-7 [做得好·但缺刻度] 工具调用时序图**

518 步压成四行密度图，`read` 那一行能一眼看见 `🔄🔄🔄🔄🔄🔄🔄🔄🔄🔄` 十连重复——这是线性阅读绝对发现不了的信号，图例（`● 正常 · 🔄 疑似重复 · ❌ 本步含错误标记`）也就在图上方。**唯一的缺陷是没有横轴刻度**：看到一处密集重复，无法读出它在第几步，没法回到决策脊柱定位。**改法**：每 50 步打一个刻度行（`|....50....|....100...`）。

**R4-8 [观察] 模型使用 / 切换记录做得对，但没和宏观报表的结论连起来**

18 次上游切换，其中 11 次带"这次切换发生在一个触发过 failover 的 Step 上"的纯观察性标注——不断言原因，符合设计纪律。但宏观报表 §6.5 已经证明"换端点后缓存效率从 93% 掉到 22%"，这个 journey 切了 18 次，叙事层却没有把这条已知代价接上去（比如在切换记录旁给出该 Step 前后的 cached 比）。两个半区各自拿着半个结论。

**R4-9 [布局] 单文件 1.88 MB / 35258 行**

工具结果与交付物正文全量内联（有 181 处截断标记，说明上限存在，但上限之内已经足够大）。这份文件在编辑器里打开会卡，在 GitHub 上直接拒绝渲染（>1MB 走 raw）。**改法**：超过某个体量时把 `决策脊柱` 拆成 `journey-<id>-spine.md`，主文件只留概览 + 指标 + 时序图 + Findings。

**R4-10 [缺陷·本次最严重的正确性问题] 叙事半区静默丢掉 `developer` 角色，"上下文构成演化曲线"因此系统性低报**

这个 journey（Pi Agent，`pimini`）的 Step 2 发生在 16:17:18。三份产物对同一轮的说法：

| 产物 | 说法 |
| --- | --- |
| `details/20260824-161718.499_...md` | `角色 Token 估算占比：user 492 (41.1%) · developer 705 (58.9%)` |
| `journey-*.md` 的 `## System Prompt` | `Step 1–518 ·（无 system prompt）` |
| `metrics.context_composition_curve[0]` | `{'system_tokens': 0, 'user_tokens': 708, 'assistant_tokens': 0, 'tool_tokens': 0}` |

**这一轮 59% 的上下文是 `developer` 角色，而曲线只有 `system/user/assistant/tool` 四个桶。** 根因在 `internal/story/journey_stepfacts.go` 的 `stepContextPoint`：

```go
switch msg.Role {
case "system":    p.SystemTokens += tk
case "assistant": p.AssistantTokens += tk
case "tool":      p.ToolTokens += tk
default: // "user" and any non-standard role
    p.UserTokens += tk
}
```

`developer` 落进 `default`，被**静默并进 `user_tokens`**。所以曲线不是少了一块，而是把"指令"算成了"用户输入"——一个看起来完全合理、实际语义错位的数字。518 个点全程 `system_tokens: 0`。

对照之下，宏观半区的 `internal/report/render_cells.go:148` 明确把 `developer` 列为独立桶：`order := []string{"tool", "assistant", "developer", "system", "user"}`。两个半区对同一个角色的处理确实分叉了。

后果有三层：
1. **"上下文构成演化曲线"是九项指标里设计文档专门点名用来回答"这个 Agent 的上下文预算都花在哪了"的那一项**，对任何把指令放在 `developer` 角色的 Agent（Pi Agent、OpenAI Responses 风格框架）它给出的是错误归因，且没有任何提示。用户据此得出的结论会是"这个 Agent 的用户输入占比高得反常"，而真相是"它的指令块被算进用户输入了"。
2. `## System Prompt ·（无 system prompt）` 是一句**肯定性的错误陈述**——不是"未记录"，而是"没有"。实际存在一段稳定的指令块，只是角色名不同。
3. `context_utilization`（上下文有效利用率）建立在同一批 Event 上，同样受影响。

三个半区里只有叙事半区漏了：宏观报表 §1 的角色表有 `developer`（21.69M token / 2.2%），`reqdetail` 的角色占比有 `developer`，只有 `story` 的 metrics 没有。项目 `CLAUDE.md` 写过一条不变量——"`ctxgraph`/`chatmsg` 是消息解析的唯一真相来源，私有的再实现会和它们悄悄分歧，这是一整类 bug 而不是风格偏好"——这就是那一类。

**影响面（本次 RECENT 语料实测）**：111 个已渲染 journey 里 16 个全程 `system_tokens = 0`，其中 **13 个是 `pimini`（Pi Agent）——即该客户端在这批语料里的全部 journey**，另 3 个是 `nokey` 的两轮测试。也就是说，一整个 Agent 框架的上下文构成分析全是错的。

**改法**：`stepContextPoint` 补一个 `case "developer"` 桶（口径与 `render_cells.go` 对齐）；`default` 分支保留给真正的未知角色并单独计数；`## System Prompt` 段在 `system` 为空但 `developer` 非空时改述为"指令位于 `developer` 角色"。

**R4-11 [布局] 套件模式下 `vmr-stories.md` 的「已渲染」列把完整文件名当链接文字**

```
| j-lobster-20260816T023956-...-f024d796 | ... | [journey-j-lobster-20260816T023956-20260816T024231-f024d796.md](journey-j-lobster-...md) |
```

链接文字和第一列的 ID 逐字重复，把表格撑到没法看。改成 `[打开](...)` 即可。（顺带确认：套件模式下这一列有 111 条真链接、`任务` 列也有值——R6a-1 的空列问题只在 `-list-only` 下出现。）

**R4-12 [缺陷·严重] 三个请求全失败的 journey，叙事层的结论是"未检测到规则可判定的疑似问题"**

`05-story-all/zh/stories/journey-j-lobster-20260816T040119-20260816T040120-1a5f92f5.md` 全文 43 行：

```
## 概览
- 起始 04:01:19
- 结束 · Step 3 · finish=- · 04:01:20

**🔷 👀 Step 1 · 04:01:19** → [详情](../details/20260816-040119.034_agent_none_error_c14dce19.md)
**🔷 👀 Step 2 · 04:01:19** → [详情](../details/20260816-040119.513_agent_none_error_23f610b7.md)
**🔷 👀 Step 3 · 04:01:20** → [详情](../details/20260816-040120.499_agent_none_error_f02ba061.md)

## 疑似问题（候选清单，不是判决）
未检测到规则可判定的疑似问题。
```

这三条记录在宏观半区的 `vmr-requests-failed.md` 里逐条列着 `❌error`。叙事半区：

- 概览没有任何失败提示，`finish=-` 是唯一线索；
- 三个 Step 全部标 `👀 观察`——`stepRoleTag` 七级优先级里**最低**的那一档，而 `⚠️错误` 排第二；
- Findings 明确宣布"未检测到规则可判定的疑似问题"。

**一个 100% 请求失败的 journey，被报告成"一切正常"。** 这不是"检测不到语义问题"，而是叙事半区根本没读请求级的 `error_class`/HTTP 状态——它只认 anthropic-messages 协议的 `is_error` 工具结果标记（那条限制是设计文档登记过的），却把可用的、宏观半区已经在用的请求级失败信号整个漏掉了。`Step.Attempts` 本来就被用于模型切换/failover 标注，同一份数据里就有。

本批 126 个 journey 中 32 个含失败请求、3 个全失败。**改法**：`stepRoleTag` 把请求级失败纳入 `⚠️错误` 判据；概览增加一行 `N/M 步请求失败`；Findings 段在"未检测到"之前先声明请求级失败计数。

---

#### R3b · Journey HTML 看板 `06-journey/zh/stories/journey-*.html`（78 KB 单文件）

**R3b-1 [做得好] 版式与信息层级明显优于同一 Journey 的 Markdown**

`VMR · 飞行记录仪` 顶栏 → **主因判定**条（结局 + 一句话主因）→ `110 步 · 46m 56s · 处理 7.34M token` 的 damage 行 → **h1 是用户当时的那句原话**（不是 id，这是对的：人记得住的是"我让它做什么"）→ `未检测到不可逆的转折点——本次运行始终在轨。` → `结构` 任务/步骤时间轴（每步：序号 · 时刻 · 模型 · 工具 chip · ⚠ 旗标）→ `指标` 14 块 + 每步上下文 token 的内联 SVG sparkline（`4224 → 96905 tok`）→ `疑似问题`。内联 CSS/JS、零外部请求。

**R3b-2 [缺陷] 同一份 Journey，Markdown 有协议盲区声明，HTML 没有**

```
$ grep -c "Anthropic Messages 协议" journey-*.html
0
```

`.md` 的 Findings 段开头那句"以下信号在本 journey 上结构性无法触发——未出现不代表检查过没问题"（R4-4 表扬过的那段），在 HTML 里整段消失。而 HTML 的指标 grid 大大方方展示着 `错误恢复次数 0`——正是那段声明点名说"结构性无法触发"的指标之一。

**HTML 是那个用来对外分享的产物**（设计文档原话："不是 Markdown 的转写，是一份为分享重新设计的看板"）。分享出去的那份反而更不诚实，方向反了。

**R3b-3 [缺陷·严重] 看板的头条「主因判定」由误报率最高的检测器驱动**

```
主因判定 · 警告
疑似引用了已被证伪的实体：工具结果显示 http.Handler, ResponseWriter, http.Request 不存在/未找到，但后续步骤仍在引用它
```

这是一个 Go 项目（vmr 自己）在实现 `/log` 页面。`http.Handler` / `http.Request` 是 Go 标准库类型。同一份看板的 Step 89 又宣布 `/health, /status, traffic.requests` "不存在/未找到"——`/health` 和 `/status` 是这个项目自己正在跑的真实端点。

判据是"tool_result 里命中 ENOENT/404/not found 字面标记，其实体在此后仍被引用"。一次没匹配到内容的 `grep`、一条编译错误、一段讨论 404 处理的代码，都足以让该结果里抽出的**全部**实体被判定为"已被证伪"。

R4-3 已经指出这个检测器在另一个 journey 上贡献了 162 条 Finding 里的 110 条。这里更进一步：**它被提拔成了整个看板最显眼的那一行**。一个对外分享的看板，头条是一句自信的错误结论。

**改法**（两条都要）：① 检测器本身——证伪信号必须与被引用实体在同一行/同一 JSON 字段共现，而不是整段文本里任意位置的子串命中；② 主因判定的选取——按检测器可信度加权，误报率高的类别不应单独构成 verdict。

**R3b-4 [缺陷] `journey-*.json` 的 `cost` 违反了 `internal/pricing` 自己的核心不变量**

```json
"cost": {"currency": "CNY", "total_usd": 0, "resolved": false, "priced_steps": 0, "total_steps": 110}
```

两个问题：① 字段名叫 `total_usd`，装的却是 `currency: CNY` 标注的值；② `resolved: false` 时 `total_usd` 是 `0` 而不是 `null`。`internal/pricing` 的包文档立的规矩是"a nil rate component means *unknown*, never *free* — the whole package is built around that distinction"，JSON 契约这里恰好把 unknown 写成了 0。任何不检查 `resolved` 就读 `total_usd` 的消费方都会得到"免费"。

渲染层做对了（`damage` 行在未解析时不渲染成本，不显示 0），问题只在机读契约。

顺带一个事实：**这个 110 步的真实编码任务，0/110 步能解析出定价**。"这个任务花了多少钱"是叙事半区对外主打的问题之一，在代表性样本上答不出来——和 R1-1 是同一个定价覆盖率根因。

**R3b-5 [做得好] `-redact` 脱敏彻底**

61 KB（未脱敏 100 KB），81 处 `‹text: N chars›` 占位符。h1 的用户原话、每个任务标题、Finding 正文全部替换；主因判定降级为 `检测器 unverified_entity_reference @ 步骤 5（正文已脱敏）`——只留 Code + Step 锚点。全文扫描未发现任何残留的对话正文。结构、时间轴、工具名、指标、token 数按设计保留。

---

#### R5 · 双 Journey 对比 `10-compare/zh`（同一句指令的两次执行：25.7s/5 工具调用 vs 55m48s/89 工具调用）

**R5-1 [缺陷·严重] 「相对变化」列用的是 `(B-A)/max(A,B)`，把所有大差异压平到 +100% 附近**

| 指标 | A | B | 报告显示 | 真实倍率 |
| --- | --- | --- | --- | --- |
| 模型时间 | 27.8s | 1261.0s | **+98%** | **45.4×** |
| Agent 侧执行时间 | 5.0s | 774.6s | **+99%** | **155×** |
| 净工作时长 | 32.8s | 2035.6s | **+98%** | **62×** |
| 工具调用次数 | 5 | 89 | **+94%** | **17.8×** |
| 输出重复率 | 8% | 13% | +39% | 1.63× |

逐项核对确认公式是 `(B-A)/max(A,B)`（对称相对差），不是"相对变化"通常意义上的 `(B-A)/A`。后果：**任何"B 远大于 A"的差异都收敛到 +9x%，读者无法区分 1.6× 和 155×**。表下唯一的脚注解释的是 ⚠️ 阈值，只字未提这个分母。

对比模块存在的全部意义就是回答"差多少"，而它的主列把量级信息碾平了。这份样本尤其讽刺：**同一句指令，A 用 32.8s 和 5 次工具调用做完，B 用了 34 分钟和 89 次**——这是一个 62 倍的差距，报告写作 `+98%`，看起来像"差不多翻倍"。

**这不是计算 bug，是标签 bug。** 源码里公式是刻意的、也有注释：`internal/story/compare_metrics.go:45` —— `DeltaRel is (B-A) / max(|A|,|B|)`。选对称相对差本身合理（对 A=0 安全，见"人类空闲时间 0.0s → 1315.0s"那一行）。问题全在渲染层：列名叫「相对变化」——这个词在任何语境下都指 `(B-A)/A`——而表下的脚注只解释了 ⚠️ 阈值，一个字没提分母是 `max(A,B)`。

**改法**：见问题 6 定稿——不加列、不动 JSON，只把这列的渲染从"上限 ±100% 的百分比"换成"大差异给倍率 `156×` / 小差异给 `±NN%` / 从无到有给『新增』"，列名改「变化」。

**R5-2 [布局·严重] 70% 的篇幅是两份几乎相同的 system prompt 全文**

```
总行数 988 · System Prompt 段（第 103–798 行）= 696 行 = 70%
```

两侧都是 OpenClaw 的同一份基础 prompt，唯一实质差别是工作区（`workspace-main` vs `workspace-deep-researcher`）。报告把两份各 ~15K token 的原文各贴一遍（正确地包在 `<details>` + 代码围栏里，48 个内容标题无一泄漏进文档大纲——这点做对了），但真正有分析价值的部分（指标 diff、工具 diff、分叉点、缓存曲线、交付物对比）加起来只有约 290 行。

已登记的 1.59 说的是"两侧逐字一致时未合并"，但**真实场景几乎不会逐字一致**——它们是 95% 相同。逐字比较的合并条件永远不触发。**改法**：这一节渲染成两份 prompt 的 unified diff（"两侧 system prompt 相差 N 行，差异如下"），既短得多，又直接回答了"环境差在哪"这个对比场景真正的问题。

**R5-3 [缺口] 对比报告没有 verdict/摘要行**

Journey HTML 看板有「主因判定」条，compare 的 Markdown 什么都没有——从 `# Journey 对比：A vs B` 直接进 `## 初始指令`。读者要自己把六个小节的结论拼起来才知道"同一句指令，B 慢了 62 倍、多调了 17.8 倍工具、换了端点、缓存首轮命中率 82% vs 39%、最后 `finish=(无)`"。

这些事实报告全都给了，就是没有把它们合成一句话。**改法**：开头加一段 3–5 行的摘要卡（最大差异项 Top-3 + 分叉点位置 + 端点是否不同 + 终止方式），措辞保持"陈述事实不判定优劣"。

**R5-4 [做得好] 几处诚实处理值得保留**

- `总耗时（墙钟）：A 25.7s · B 3348.1s —— 含人类空闲时间，不是效率指标，效率请看上表的"净工作时长"` —— 主动防止读者误读墙钟时长。
- `终止方式：A finish=stop · B finish=(无)——VMR 只能看到这一步的结果，看不到 Agent 自身是否配置了类似 loop detection 的机制。`
- `## 模型与端点核查`：`两侧模型/端点**不同**——这本身可能是效果差异的一个直接原因，不要默认排除。` 这一句把"混杂变量"直接摆在读者面前，比任何统计修饰都有用。
- `## 分叉点` 后紧跟 `分叉点定位 ≠ 根因判定`。
- `## 成本估算`：`两侧均无可解析定价（Token-Plan / 订阅制账户常见）。` —— 不写 0，还解释了为什么常见。
- `## 证据溯源` 列出本次实际读取的源审计文件。

**R5-5 [缺陷] 逐轮缓存曲线跳号，未说明**

```
B: R1 39% → R2 96% → ... → R11 0% → R12 97% → R14 1% → R15 1% → ... → R43 99% → R45 5% → R50 70% → ...
```

R13、R22–23、R26、R44、R46–49 等直接消失。大概率是这些轮次拿不到 usage，但曲线没有任何说明，读者会以为轮次编号本身有问题。同时 `R11 0% → R12 97% → R14 1%` 这种剧烈抖动正是最值得解释的地方（缓存被打断了两次），却被埋在折叠区里、没有在上方的"首轮/稳态/最值"表里体现。

**R5-6 [做得好] Compare HTML 是真正的左右对照**

27 KB，两个 h2（`两侧` / `分岔与差异`），顶栏 `VMR · 取证`，A/B 两列用 `.ta`/`.tb` 并排（`bai:deepseek-v4-flash` vs `volc_coding_plan:deepseek-v4-flash (+1)`，`55m 48s`），下方 `缓存命中率` / `系统提示词` / `墙钟 / 终止` 三组事实成对给出。比 Markdown 版好读得多，且没有把 system prompt 全文塞进去。

**R5-7 [确认已登记项 1.59] 逐字相同的初始指令仍然贴两遍**

第二个对比样本（同一个 cron 任务连续两天的两次执行，`11-compare-cron/zh`）：

```
两侧初始指令块数: 2 · 逐字相同: True · 长度 2000 / 2000
```

两个完全相同的 2000 字符块各占一个 `<details>`。1.59 登记的正是这个场景，实测确认存在。结合 R5-2：**逐字相同的情况（cron 场景）应该合并，"几乎相同"的情况（人类指令 + 同框架 system prompt）应该做 diff**——两条都指向同一个改法方向。

**R3b-6 [缺陷] HTML 看板系统性地丢掉了 Markdown 里的认知谦逊，措辞反而更绝对**

三处叠加，构成一个模式：

| 位置 | Markdown | HTML 看板 |
| --- | --- | --- |
| 协议盲区 | `⚠️ 以下信号在本 journey 上结构性无法触发——未出现不代表检查过没问题` | **整段不存在**，同时展示 `错误恢复次数 0` |
| Finding 措辞 | `疑似…（候选清单，不是判决）` `建议人工复核` | 主因判定条直接给结论 |
| 无 Finding 时 | `未检测到规则可判定的疑似问题。` | `未检测到不可逆的转折点——**本次运行始终在轨**。` |

最后一行问题最大：**"始终在轨"是一个肯定性判断，而它的依据只是"没有检测器触发"**。这恰好是项目自己反复强调要避免的推理方向（§3.8 那张"盲区"表的标题就是"避免『没记录』被误读成『没发生』"）。而且它出现在**误报率最高的检测器同时占据了主因判定条**的那一页上——同一屏里，一个假阳性被当成头条，一个"没有信号"被当成"运行良好"。

另一处：518 步那份看板的 PONR 框标题是 `死亡转折点 → 步骤 39`。数据来源是 `constraint_text_dropped_at_compaction`——设计文档明确写它是"未经验证的假设级检测"。"死亡转折点"这个措辞与"假设级"之间落差太大。

（顺带一处**做得好**：这份 518 步看板的主因判定选的是 compaction 丢失约束，而不是那 110 条 `unverified_entity_reference`——说明 verdict 选取确实有排序逻辑，不是简单取第一条。问题在于当没有更高优先级的 Finding 时，它会退回到最不可靠的那一类。）

**R3b-7 [布局] 518 步的看板 752 KB，时间轴一屏一行铺 518 行**

39 个任务、518 个步骤全部展开成行。相比之下 110 步那份 78 KB 是舒适的。**改法**：超过 ~150 步时按任务折叠（`<details>` 默认收起，保留 ⚠️ 命中的步骤展开），或给时间轴加一个密度概览条。

---

#### R5b · 语料级统计 `13-corpus/zh/stories/vmr-story-corpus.md`（125 个 Journey，106 行）

**R5b-1 [已撤回·实为过期二进制导致的误判] 均值偏斜脚注** ←1.57

初稿曾记为"1.57 登记的缓解脚注不存在"。**这条判断是错的**，原因见下方 2.4 的方法论说明：本次生成用的 `./vmr` 二进制构建于 2026-08-30 21:03，早于 `c13819a`（08-31 02:26，"add honesty footnotes"）。用当前 HEAD 重新编译后重跑 `-corpus`，脚注正常出现：

> 时间类指标的均值对少数超长 Journey 极其敏感——单个跨多日的 Journey（同一 lineage 内空闲间隔的累积）就能把均值抬到远高于典型值，甚至高于 P90。判断常见情况请优先看中位数与 P90，均值仅作总体负载的参考。

**仍然成立的部分**（脚注是免责，没动根因，1.57 自己也这么写）：`Agent 侧执行时间` 均值 21830.9s / 中位数 41.2s / 最大 749840.5s（8.7 天）；连带「Finding 分组对比」里 `narration_without_action` 命中组的**中位数** 214198.7s（2.5 天）——中位数不受脚注保护，这一行仍然是一个不能用来做判断的数字，却带着 ⚠️ 标记。**建议**：分组对比表也套用同一条脚注，或对单间隙设上限（1.57 的"可能方案"）。

**R5b-2 [缺陷] 相关性 Top-15 里至少一半是定义上的恒等式，脚注只承认了时间类**

| 排名 | 指标 A | 指标 B | rho | 性质 |
| --- | --- | --- | --- | --- |
| 1 | 工具调用次数 | 上下文有效利用率 | **0.86** | 机械（0 次工具调用 ⇒ 利用率 0） |
| 2 | Agent 侧执行时间 | 净工作时长 | 0.85 | 机械（后者含前者） |
| 3 | 工具调用次数 | 计划/执行比 | **-0.84** | 机械（计划/执行比 = 无工具调用轮占比） |
| 4 | Compaction 次数 | Compaction 信息损失 | 0.81 | 机械 |
| 5 | 模型时间 | 工具调用次数 | 0.80 | 弱机械 |
| 6 | 计划/执行比 | 上下文有效利用率 | **-0.79** | 机械（1 与 3 的传递） |

脚注只说"含不少是同一时间类指标之间的机械关联，如『净工作时长 = 模型时间 + Agent 侧执行时间』"——**只承认了时间类**。榜首那条（`工具调用次数 ↔ 上下文有效利用率`，也就是整份语料统计最醒目的一个数字）是构造性的，不在脚注覆盖范围内。

**改法**：维护一张"已知恒等关系"清单，这些配对直接从 Top-15 里剔除（或单列一张"机械关联"表），把榜单留给真正非平凡的相关性。

**R5b-3 [缺陷] 「上下文有效利用率」是双峰退化的，但报告把它当连续指标用**

跨 111 个 journey 的实测分布：

```
= 0     的: 23 个 (21%)
>= 0.99 的: 35 个 (32%)
中间段稀疏
```

一半以上的样本落在两端。语料统计给出的"均值 70% / 中位数 95%"因此没有意义，HTML 看板给出的"上下文利用率 100%"更是一个看起来很好、实则不携带信息的数字。设计文档给这项指标的定义是"低值意味着大量进入上下文的内容从未被再次引用"——若 32% 的 journey 都接近满分，它就没有在找到它本该找到的浪费。**处置见问题 39：不改（次级指标、不驱动任何东西、无用户问题依赖它），只在 KNOWN_ISSUES 记一条口径提示。**

**R5b-4 [缺陷] N-gram 榜首全是"同一个工具连着调两次"**

```
| exec → exec        | 988 | 0% |
| bash → bash        | 938 | 0% |
| bash → bash → bash | 741 | 0% |
| exec → exec → exec | 738 | 0% |
```

前四名都是自重复。任何 Agent 的工具序列里最高频的 n-gram 必然是它最常用工具的自重复——这是零假设，不是"行为定势"。脚注却写"展示最高频出现的**行为定势与异常关联**"。**改法**：排除自重复，或改报"相对于独立性假设的提升倍数（lift）"，让 `exec → process` 这类真正的跨工具定势浮上来。

**R5b-5 [缺陷] Context Rot 表没有观测到趋势，但脚注只讲了"如果有趋势该怎么读"**

```
| 0-32k    | 0.24 |   | 128k-256k | 0.22 |
| 32k-64k  | 0.31 |   | 256k+     | 0.30 |
| 64k-128k | 0.22 |
```

Finding 密度在五个区间里基本持平（0.22–0.31，且 256k+ 只有 56 个 Step 样本）。脚注写的是"高上下文区间下的 Finding 密度突增或错误率上升**反映了**注意力衰减趋势"——描述的是一个本表并未出现的现象，却没有一句"本批语料未观测到该趋势"。读者容易把 0.30 vs 0.22 读成证据。

同一张表的「错误率」整列 0%，原因是本语料 0% 是 Anthropic Messages 协议（文档顶部的盲区声明确实点名了 `Context Rot error rate`）。但列里显示的仍是自信的 `0%` 而不是 `n/a`。

**R5b-6 [缺陷] 高命中率的检测器缺乏区分度**

```
unused_tool_result           58%
unverified_entity_reference  34%
reasoning_action_mismatch    29%
plan_execution_misalignment  19%
```

`unused_tool_result` 在 **58% 的 Journey** 上至少命中一次。一个在多数样本上都触发的检测器不携带区分信息。这三个高命中检测器正是 R4-3 / R3b-3 里从单 Journey 视角发现有系统性误报的那三个——语料级数据独立佐证了同一结论。

**R5b-7 [做得好] 这份报告的自我限定是全套产物里做得最好的**

- 顶部盲区声明点名到具体检测器，并明确"命中率为 0 或指标全为 0 代表『测不出来』，不代表『检查过没问题』"。
- 相关性只报 rho 不报 p 值，并写明理由（语料规模撑不住显著性检验）；Top-15 截断时说明"另有 35 组达到阈值未列出，完整列表见 JSON"。
- 分组对比的 ⚠️ 明确写"不是『这个 Finding 导致了更长的耗时』的确定性结论；VMR 没有任务是否成功的标签，这里比较的是耗时这一个代理指标，不是效果"。
- 样本不足时**显式列出被跳过的 Code**（"不是没有差异，是数据不够"），而不是静默消失。

这套纪律如果能复制到宏观报表的 §3/§7 和 HTML 看板上，本次 review 里的一大半问题就不存在了。

**R5b-8 [布局] 分母在三份产物里各不相同且无解释**

`vmr-stories.md` 说 126 个候选，`-corpus` 说"分析了 125 个 Journey"，套件渲染出 111 个 `journey-*.md`。三个数字散落在三份文件里，没有一处解释差异来自哪里（断头过滤 / 噪声折叠 / 渲染范围）。

---

#### R5c · LLM 解读层 `09-journey-llm/zh`（单 Journey）与 `12-compare-llm/zh`（对比）

**R5c-1 [做得好·而且它独立验证了 R3b-3] LLM 层自己把规则层的误报识别出来了**

单 Journey 解读段的原文（模型 `cheap`）：

> **引用已被证伪的实体（置信度：中）**
> 定位：Step 5, 66, 89, 97
> 解读：这些问题主要集中在对文件路径或接口的路径搜索（如 `/status` 或 `/health` 被标记为不存在）。**实际上，这大多是因为 Agent 在执行全局搜索或路径验证时，由于 git 状态、`archived/` 目录隔离或测试桩模拟导致的"Not Found"。从结果看，并未影响功能的正确实现，属于工具调用的边界现象。**

我在 R3b-3 里独立得出的结论（这个检测器把"文本里出现 not found"当成"实体不存在"）被解读层原样说了出来。这既说明解读层确实在做有价值的工作，也说明规则层的这个检测器确实需要校准。

其余几点核实：
- 8 条规则 Finding **一条不漏**地被归进 3 个优先级组，**没有编造清单外的新问题**（`semantic_oscillation @ Step 15` 确实在 JSON 的 findings 里）——设计文档里那条"禁止自己发现清单之外的新问题"的 system prompt 约束是生效的。
- 段首免责：`以下内容由 cheap 生成的解读，不是事实层，可能有误，请对照上面的证据表核实。`
- 结尾的 `### VMR 看不到什么` 具体到"无法感知宿主环境是否清理了临时二进制/Socket 句柄""无法阅读代码语义""无法确认浏览器对 SSE 的兼容性表现"——不是套话。
- Compare 的 HTML 看板有对应的 `class="llm-disclaimer"`：`以下为模型（cheap）对上方事实的解读，不是事实本身；只喂了规则派生的事实与有界文本节选。`

**R5c-2 [缺陷·设计文档与实现不一致] Journey HTML 看板结构上无法显示 LLM 解读**

设计文档 §3.4 写 Journey 看板四块之一是「Findings（**规则层 + LLM 层**）」。实际：

```go
func RenderHTML(j *Journey, m Metrics, findings []Finding, cost CostFact, lang i18n.Lang, redact bool) string
func RenderComparisonHTML(cmp Comparison, llm CompareLLMResult, lang i18n.Lang, redact bool) string
```

**`RenderHTML` 根本没有 LLM 参数**。实测 `09-journey-llm/zh` 的 HTML：`grep -c "LLM 解读" = 0`，而同目录的 `.md` 有完整的解读段。Compare 那边是实现了的（`chtmlLLM`）。

这让 R3b-3 变得更糟：**分享出去的那份看板，头条是规则层的误报，而唯一能纠正它的 LLM 段落被挡在了 Markdown 里**。同一次运行，同一份数据，最不该看到误报的那个读者（收到分享链接的人）恰恰只能看到误报。

**R5c-3 [观察] `semantic_oscillation` 是设计文档没登记的第 10 个检测器**

设计文档 §3.5a 的表里列了 9 个 `FindingCode`（Phase 1 五个 + Phase 2 四个），`semantic_oscillation` 不在其中，但它在实际输出里出现了（Step 15）。文档漏登记。

---

#### R6c · 边界模式：`-include-partial` / `-currency` / 多命中 `-journey`

**R6c-1 [缺陷] `-list-only -include-partial` 是一个被静默接受的空操作，且帮助文案是错的**

```
$ diff vmr-stories.md（-list-only）  vmr-stories.md（-list-only -include-partial）
IDENTICAL
```

两次运行**逐字节相同**，都是 126 个候选。原因：断头 journey 本来就一直在列表里（标题前带 `⚠` 前缀），`-include-partial` 只影响**渲染**范围。而 `-h` 写的是"also **list**/render journeys whose head looks truncated"——"list" 那半句不成立。

对照之下，同一个 CLI **拒绝** `-list-only -details`（"`-details` has no effect with `-list-only` — drop one or the other"）。两个同样是空操作的组合，一个报错、一个静默接受，标准不一致。

**改法**：帮助文案去掉 "list/"；`-list-only -include-partial` 要么同样报错，要么在 `⚠` 旁边加一句图例说明断头 journey 一直可见。

**R6c-2 [做得好] `-currency` 拿不到汇率时的降级提示写得很到位**

```
pricing: no exchange rate to convert CNY -> JPY for -currency, showing CNY instead
(add exchange_rate: {JPY: <rate>} to config.yaml's pricing: block or report.yaml)
```

说清了三件事：发生了什么、退回到了什么、怎么修（连 YAML 键名都给了）。

**唯一的缺口**：这句话只在终端出现，**报告文件本身不留痕**。`vmr-report.md` 里写着"货币 CNY"，没有任何地方提到"用户要的是 JPY，没换成"。日后别人打开这份 md，无从知道发生过降级。**改法**：§2 的口径脚注补一句"（请求的显示货币 JPY 因缺少汇率未生效）"。

**R6c-3 [符合预期] 多命中 `-journey 'j-pimini-*'` 走批处理路径**

13 个 pimini journey 全部渲染（`journey-*.md` + `.json`，加 `vmr-stories.md` 共 14 个 md），不产出 HTML——与"`-html` 仅单命中 `-journey` / `-compare` 时生效"的文档说明一致。`-details` 未传时步骤指针退化为源坐标，无死链。

**R6c-4 [符合预期] `-macro-only` / `-list-only` / `-story-only` / 默认套件的产物差异**

| 模式 | `vmr-report.*` | `vmr-requests*` | `stories/vmr-stories.*` | `journey-*.md` | `details/` | `tool-waste.html` |
| --- | :-: | :-: | :-: | :-: | :-: | :-: |
| `-macro-only` | ✓ | ✓ | ✗ | ✗ | 仅 `-details` | ✓ |
| `-list-only` | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ |
| `-story-only -render-all` | ✗ | ✗ | ✓ | 126 | ✓ | ✗ |
| 默认套件 | ✓ | ✓ | ✓ | 111（折叠 heartbeat） | 仅 `-details` | ✓ |
| `-journey <单>` | ✗ | ✗ | ✓ | 1 | ✓ | ✗ |
| `-compare a,b` | ✗ | ✗ | ✓ | 2 + compare | ✓ | ✗ |
| `-corpus` | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ |

无一处越界或缺漏，模式语义是自洽的。

---

### 3.3 总结：纯用户视角

#### 3.3.1 这套产物真正交付了什么价值

抛开缺陷，有四件事是这套产物做到了、而市面上"LLM 网关 + 日志"通常做不到的：

**① 它能告诉你钱具体浪费在哪一个可执行的动作上。** `tool-waste.html` 一屏给出"你每次请求都在发 977 MB 工具定义，其中 661 MB 是给从不调用的工具的"，并直接列出该裁哪些工具名。这不是"你花了 X 元"这种没法行动的数字，是"删掉这 53 个工具声明就能省下 1.65 亿 token"。

**② 详单页把"vmr 到底改了什么字节"变成了可核查的事实。** `details/*.md` 的 ①→② diff 视图（`🟢 新增 / 🔴 删除 / 🔶 变化`）让"三处 sanctioned deviation"这条架构红线从文档里的承诺变成了每一条请求都能自己验证的东西。对一个"字节透传"定位的产品，这是最有说服力的自证方式。

**③ Sticky 有效性那一节是真正的因果证据。** 93.0% vs 22.0%——同一会话内落回同端点 vs 换端点的缓存效率对照，配上"按结果度量、不按机制""不解释切换原因"的自我限定。它证明了一个架构决策值不值，而不是描述它做了什么。

**④ 语料统计的自我限定水平明显高于行业平均。** 只报效应量不报 p 值并说明理由、Top-15 截断时告知省了多少、样本不足时显式列出被跳过的 Code 而不是静默消失、盲区声明点名到具体检测器并写明"命中率为 0 代表测不出来、不代表检查过没问题"。这套纪律如果能覆盖全部产物，可信度会是这个品类里的天花板。

#### 3.3.2 用户需求没被满足的缺口（不关心系统能不能提供，只问用户要不要）

按"用户会问的问题"排，而不是按模块排：

| 用户的问题 | 现状 | 缺口 |
| --- | --- | --- |
| **"这段时间一共花了多少钱？"** | §2 有四张成本表，**没有一张有合计行**；§0 摘要五列没有钱 | R1-4。一份叫"用量报告"的东西没有 top-line 成本 |
| **"我的工具 schema 浪费一共多少？"** | 只在 `tool-waste.html` 里；Markdown 主报表一个总量数字都没有 | R3a-2。最有行动价值的结论藏在副产物里 |
| **"§6 里这个可疑会话，具体是怎么跑的？"** | §6 列了上千个会话，到 224 个 journey 文件**零链接**；join key 两边现成，142 个会话可直连 | R6b-1。设计文档的核心主张"变焦零成本"在这一跳上没落地 |
| **"这个任务的上下文预算花在哪了？"** | `journey-*.md` **一项行为指标都没渲染**，17 项全在 JSON / HTML 里 | R4-2 |
| **"这个任务花了多少钱？"** | 代表性样本上 **0/110 步能解析出定价** | R3b-4 |
| **"两次执行差多少？"** | 「相对变化」列把 62× 显示成 +98%，所有大差异压平到 ±100% | R5-1 |
| **"上周三那个 SSH 任务在哪？"** | §6 会话表**没有时间列**；三份产物三套 id（`s432` / `l-4ae1f6b2/t01` / `j-lobster-...`），`sNNN` 还随输入范围浮动 | R1-6 / R2-3 |
| **"这批日志里哪次是雪崩、哪次是孤立故障？"** | 失败索引是纯时间序平铺，65 条里 8 条挤在 2 分钟内也不聚类 | R2b-2 |
| **"这份报告的口径和上次一样吗？"** | self-traffic 排除随 `llm_key` 有无静默切换，候选数 110↔126 变化无任何提示 | R6a-2 / R1-9 |

还有两个**跨时间**的缺口（已登记 1.60）在本次样本上格外明显：44 天的数据里，报告只会告诉你"这 44 天合起来怎么样"，不会告诉你"这周比上周差在哪"。对一个每天都在跑的路由器，"趋势"比"总量"更常用。

#### 3.3.3 正确性问题（按严重度）

| # | 问题 | 严重度 | 根因位置 |
| --- | --- | :-: | --- |
| **1** | `developer` 角色被静默并进 `user` 桶 → 「上下文构成演化曲线」对 Pi Agent 全错（13/13 个 pimini journey），`## System Prompt` 输出"（无 system prompt）"这一肯定性错误陈述 | **高** | `internal/story/journey_stepfacts.go` `stepContextPoint` 的 `default` 分支 |
| **2** | 100% 请求失败的 journey 被报告成"未检测到规则可判定的疑似问题"，三个 Step 全标最低优先级的 `👀 观察` | **高** | 叙事半区不读请求级 `error_class`；本批 126 个 journey 中 32 个含失败、3 个全失败 |
| **3** | `unverified_entity_reference` 系统性误报（占某 journey 162 条 Finding 中的 110 条；语料级 34% 命中），且被提拔为 HTML 看板的**主因判定**头条 | **高** | 判据是整段文本子串命中 ENOENT/404/not found；§3.5a 的四轮校准没覆盖它 |
| **4** | §2 按日成本表丢 11/44 天，含全窗口最忙的 8-25（885 请求）。HEAD 已加脚注，但仍不说是哪几天 | 中 | 无 `cost_estimate` 的行直接不渲染 |
| **5** | HTML 看板系统性丢掉 Markdown 的认知谦逊：协议盲区声明整段消失、无 Finding 时断言"**本次运行始终在轨**"、"假设级检测"被标题成"**死亡转折点**" | 中 | `render_html_dashboard.go` 未继承 `.md` 的免责渲染 |
| **6** | Journey HTML 结构上无法显示 LLM 解读（`RenderHTML` 无 LLM 参数），与设计文档 §3.4"Findings（规则层 + LLM 层）"不符；而 LLM 层恰好能纠正 #3 的误报 | 中 | 见 R5c-2 |
| **7** | `journey-*.json` 的 `cost.total_usd` 在 `resolved:false` 时是 `0` 而非 `null`，字段名与 `currency: CNY` 打架 | 中 | 违反 `internal/pricing` 自己的"nil ≠ free"不变量 |
| **8** | §3 端点健康表对 n=2 的样本照打 ⚠️ 高错误率警告，而 §4 对同一批端点标 `⚠️low-n`、§6.5 样本不足时不给结论——同一份报告三套低样本标准 | 中 | R1-3 |
| **9** | §6.7 Compaction 表出现"保留比 107% / 133% / 159% / 431%"，这几行其余列全空，无脚注解释 | 低-中 | 疑似把非 compaction 边界误判成 compaction |
| **10** | 附录 `< 90%%` 未转义；EN 报告 §0 出现中文全角括号 `（fallback 197 / trunc 43）` | 低 | `i18n/report_doc.go:100/160` |
| **11** | `-llm-addr ""` 无法用于关闭 `report.yaml` 的 LLM 配置（守卫判"flag 有没有被敲"而非"解析值是否非空"） | 低 | `cmd_analyze.go:212` |
| **12** | `-list-only -include-partial` 是静默接受的空操作（输出逐字节相同），帮助文案的 "list/" 那半句不成立；而同样是空操作的 `-list-only -details` 却会报错 | 低 | R6c-1 |
| **13** | `vmr-requests-cron-hartbeat.md` 文件名拼 `hartbeat`、内容拼 `heartbeat`，易被当成错别字（拼写已在 UserGuide 双语写明，非"仅注释里"——见问题 19） | 低 | `report/requests.go:101` |
| **14** | `semantic_oscillation` 检测器实际存在但未登记进设计文档 §3.5a 的表 | 低 | 文档 |

#### 3.3.4 布局与可读性

**已经做对的**（不要动）：分组文件的 `Session → Task → Turn` 三级标题树、详单页的 `↺ 历史 / 🆕 本轮`分界与折叠、`evidence/` 的内容哈希去重、heartbeat 折叠进 `<details>`、工具调用时序图的密度可视化、compare HTML 的左右并排、`tool-waste.html` 的 hero + stat tiles + 进度条、`-redact` 的彻底性。

**需要动的**，按"改一次收益最大"排：

1. **§6 会话表加 journey 链接**（R6b-1）——一行代码量级的 join，直接补上产品最核心的那条导航。
2. **`journey-*.md` 补 `## 行为指标` 一节**（R4-2）——数据现成，纯渲染缺失，补上之后 Markdown 才算完整产物。
3. **§7 开头搬入 `tool-waste.html` 的四块总量**（R3a-2）——把最有价值的结论从副产物提回主产物。
4. **compare 的 system prompt 改成 diff**（R5-2）——988 行里 696 行是两份 95% 相同的 prompt；改成 diff 既短又直接回答"环境差在哪"。已登记的 1.59（逐字相同才合并）覆盖不到这个真实场景。
5. **长表折叠**：§3/§4 的 187 行端点表把 `⚠️low-n` 行折进 `<details>`；518 步的 HTML 看板按任务折叠；1.88 MB 的 journey md 把决策脊柱拆成 sibling 文件。
6. **Finding 清单分组计数**（R4-3）——162 条平铺按 Code 分组 + 计数 + 按可信度排序，否则读者第 20 条就放弃。
7. **首行 44 个文件路径折进 `<details>`**（R1-11），正文只留"44 个文件 · 日期区间 · 记录数"。
8. **排序键对齐洞察**：§5.5 按 token 量而非字母序；§6.6 让"失败耗时 6792.8s"这种爆点浮上来；语料统计的 N-gram 排除自重复。
9. **术语首次出现处给解释**：`⭐`（在 §0 就出现 4 次，解释在 2760 行外）、`msgs`（累计还是新增）、`finish` 取值、`⚠` 前缀（断头）、`sNNN` 的不稳定性。
10. **mermaid `xychart-beta` 加降级兜底**（R1-14）——44 个 x 轴标签在任何渲染器里都会挤成一团，纯文本阅读时只剩一行数字数组。

#### 3.3.5 一句话结论

**事实层（详单、diff、证据去重、审计溯源）扎实到可以当法证材料用；剖面层（指标、Finding）的呈现层欠账最多——数据算出来了但没渲染到人读产物里，或者渲染了却被误报和压平的百分比毁掉可信度。** 最该先修的三件事：`developer` 角色分桶（正确性）、`unverified_entity_reference` 校准（可信度）、§6→journey 链接（可用性）。修完这三样，这套产物从"信息很全但要自己拼"变成"打开就能用"。

#### 3.3.6 补充意见

- **本次 review 自身的一处失误值得记下来**：我用仓库里现成的 `./vmr` 生成全部样例，它比 HEAD 落后一个提交，导致我一度把已修复的 1.57 脚注报成"缺失"。发现后重新编译并逐条复核（2.4 有对照表）。**建议在 `vmr.sh` 或 CI 里加一条检查**：`vmr version` 输出的 VCS stamp 与当前 `git rev-parse HEAD` 不一致时给出警告——这个坑任何人都会踩。
- **建议登记进 `KNOWN_ISSUES`** 的新条目：3.3.3 的 #1/#2/#3/#5/#6/#7/#14。（#13 `hartbeat` 拼写不必登记——UserGuide 双语已写明该拼写，见问题 19；要么直接统一成 `heartbeat`。）
- **`_recheck/` 目录**保留了用 HEAD 二进制重跑的宏观报表与语料统计，可直接与 `01-macro/zh`、`13-corpus/zh` 做 diff 来看 `c13819a` 的实际效果。不需要了可以直接删。
- **文中所有 `1.NN` 编号引用的是 `KNOWN_ISSUES`。**
- 本次未覆盖的面：`-show-ungrouped`、`-include-self-traffic`、`openai-responses` 协议的产物（本批语料只有 2 条该协议请求，且都失败），以及 Anthropic Messages 协议占比高的语料（本批 0.0%，导致 4 个检测器 + Context Rot 错误率 + N-gram 尾步错误率整体测不出来）。**如果要验证那 4 个检测器，需要专门准备一批 anthropic-messages 语料**——这是当前样例集最大的覆盖盲区。

---

## 第四部分 · Review 事项的重述与优先级（Sonnet 5，2026-08-31 · 第 2 版）

> 第 1 版用 P/C/G/R 前缀编号 + 表格，编号和第三部分的 R 编号对不上、要跨节回查。本版全部 inline：**一套连续编号（问题 1 … 问题 43），按优先级排**，每条把现象、根因（源码位置）、改法、评分（用户价值 / 开发成本 / 风险）、为什么在这个梯队全写在条目里——只看第四部分就够。每条末尾附第三部分的原始 R 编号，仅供回溯，不看也不影响理解。

### 4.1 核查方法

第三部分约 60 条发现，逐条回源码验证（`internal/story` / `internal/report` / `internal/reqdetail` / `internal/chatmsg` / `internal/i18n` / `cmd/vmr`），不接受"文档这么说 / review 这么记"。结论：**带源码定位的断言基本都对得上**；偏差见 4.2。

评分说明：**用户价值**（这条对真实用户有多大用）、**开发成本**（改起来多大工程：低 = 一行到几十行、单文件；中 = 多文件 / 要双语 i18n + golden / 要语料复校；高 = 新子系统）、**风险**（回归面 + 判断出错的概率）。梯队 = 价值 ÷（成本 + 风险）的粗排，不是精确公式。

### 4.2 第三部分里需要修正的判断

- **`vmr-requests.md` 不是"纯索引"（R2-4）——已过时。** `c13819a` 已删掉 `# 全部请求（时间序）` 整段（`internal/report/requests.go` 里 `writeAllRequestsFooter` 调用点移除）。HEAD 上它就是纯索引。这是过期二进制的又一个受害者，第三部分 2.4 的复核表没覆盖到。**无需动作**（仍列为问题 36 占位，注明已解决）。

- **`developer` 角色被并进 `user` 桶（R4-10，第三部分列为 #1 正确性问题）——owner 裁定 WAI。** `config.yaml` 每个 endpoint（含 `fallback_endpoints`）都配了 `role_map: {developer: system}`，本部署里 `developer` 就是 `system`（对下游多数 OpenAI-completions provider 也几乎是必然选择——较新的 `developer` 角色它们多半不认）。详单页（`reqdetail`）已把 `developer` 单列一行，人工下钻看得到。一句技术备注以免下一个 reviewer 重提：role_map 重写只发生在出站方向（`internal/adapter/openai/openai.go:36` `BuildRequest`），分析半区读的是 byte-faithful 的 client 层（`internal/ctxgraph/manifest.go:98`、`internal/story/journey_stepfacts.go:29`），所以 `developer` token 严格说是被并进了 `user` 而非 `system`；真要动，最小改法是 `stepContextPoint` 里加 `case "developer": p.SystemTokens += tk`（对齐 role_map 意图），不是加新桶。**不进优先级组。**

- **Journey HTML 结构上无法显示 LLM 层（R5c-2）——部分高估。** `cmd/vmr/cmd_story.go:371` 把 LLM 派生的结构化 Finding（`SourceLLMInferred`，HIGH + 锚点）merge 进 `findings` 再传给 `RenderHTML`，`internal/story/render_html_dashboard.go:263` 会渲染它们——设计文档 §3.4"Findings（规则层 + LLM 层）"是兑现了的。缺的只是 **LLM 解读散文段**，而设计文档只给 Compare 看板配了这段、Journey 看板本就没有。真问题（规则层误报当了 verdict 头条、旁边没有东西反驳它）归入问题 2。

- **宏观→叙事"零链接"（R1-5 / R6b-1）——口径要收紧。** per-tag sibling 文件（`vmr-requests-<tag>.md`）在套件模式下**已经**有 `→ 任务叙事见 [stories/journey-…]` 链接（`internal/report/requests.go:468`）。真正的缺口只是 `vmr-report.md` §6 那张会话表。见问题 5。

- **journey json `cost` 违反 pricing 不变量（R3b-4），标"中"——降到低。** `resolved:false` 就在同一个 JSON 对象里、`internal/story/cost.go` 包注释写了"unknown never free"。真瑕疵是字段名 `total_usd` 在 `-currency CNY` 下装的是 CNY。见问题 33。

- **corpus 均值偏斜脚注缺失（R5b-1）——review 已自行撤回**（过期二进制，`c13819a` 里已加脚注）。

- **`obster` client 无分组文件（R1-10）——根因已查清，不是 bug。** `obster` 是审计日志里真实的 `client_key`（用户某次 key tag 少打首字母），唯一一条会话是 `class: heartbeat` / `requests: 1` 的单发 poll。`partitionGroups` 把单发定时会话并入 `scheduled[class]` 滚动桶、不进 `chatUser`，而 §2/§5 的 ByClient 表无条件聚合——是两种口径不一致，不是数据丢失/off-by-one。初稿"大概率落进 unresolved"错。见问题 40。

- **`hartbeat` 拼写"仅在源码注释里"（R2b-4）——不成立。** `docs/UserGuide.md` 与 `.zh.md` 双语都写明了这个文件名拼法。真问题只是文件名与文件内标题拼法不一致。见问题 19。

- **self-traffic 排除的"更优改法"（问题 4 初稿）——不需要新机制。** 初稿提议注入 `X-VMR-Self-Traffic` header + 审计标记（跨两半区边界）。其想要的配置层解耦能力已存在：`report.yaml` 的 `self_traffic_client_tags`，与 `llm_key` 独立。唯一缺口是报告不声明当前口径。见问题 4。

### 4.3 分组总览

> **状态（2026-08-31）**：第一组 1–7 全部 ✅，另 10、11 重新调整进第一组一并完成 —— 执行记录见第五部分。二轮源码核查见第六部分（并修掉 `finishCell` / self-traffic 附录 / `-list-only` 空列 / compaction `0→0` 四处）。**剩余未解决项按依赖链重排后的分批建议见第七部分**——以下梯队划分保留作原始判断依据。

| 梯队 | 问题编号 | 一句话 |
| --- | --- | --- |
| **第一组 · 马上做** | 1–7（+10、11）| 价值高、成本一行到几十行、风险几乎为零；或正在毁掉最显眼产物的可信度 |
| **第二组 · 第二梯队** | 8–19 | 价值高但成本中等（多文件 / golden / 语料复校），或价值中而牵一发（正确性 / 对外产物 / 主产物完整性）|
| **第三组 · 可等** | 20–35 | 价值中、成本低，随手碰到就清；不紧急、不阻塞 |
| **第四组 · 不做 / 存疑 / 已过时** | 36–43 | 已解决、撞 v1-complete 红线、设计上出圈、或需专门语料/样例才能推进 |

---

### 第一组 · 建议马上处理

#### 问题 1 · 附录 `%%` 未转义 + 英文报告混入中文全角括号 ✅ 已处理

**梯队与评分**：第一组｜用户价值 中·开发成本 低·风险 低。

**现象**：`vmr-report.md` 附录里出现 `< 90%%`（原样两个百分号）。英文报告 `en/vmr-report.md` §0 摘要出现 `15825（fallback 197 / trunc 43）`——中文全角括号（全文只此一处是文案 bug，其余全角括号是用户会话标题的原文透传，属正确 passthrough）。

**根因**：
- `internal/i18n/report_doc.go:100`（zh）与 `:160`（en）：`AppendixLowConf` 字段里字面写了 `90%%`，作为常量直接拼进 Markdown、没走 `Sprintf`，所以 `%%` 原样输出。
- `internal/report/render_doc.go:145`：§0 摘要行硬编码 `fmt.Sprintf("%d（fallback %d / trunc %d）", …)`，**没有语言分支**，英文报告也吃这个格式串。

**改法**：`report_doc.go` 两处 `90%%` → `90%`；`render_doc.go:145` 的格式串挪进 `i18n.Doc(lang)`，英文用半角括号。

**为什么第一组**：一个字符 + 一个语言分支、零风险，是英文报告在最显眼的 §0/附录里的正确性硬伤。改动小到没有讨论空间，搭任何一次改动的车带走。

第三部分对应：R1-2、R1-19。

---

#### 问题 2 · HTML 看板的"主因判定"头条由误报率最高的检测器驱动 ✅ 已处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第一组｜用户价值 高·开发成本 低（~10 行）·风险 低。

**本轮落地**：`internal/story/severity.go` 加 `lowConfidenceFindings` 清单（含 `unverified_entity_reference`）；`JourneySeverity` 拆出 `pickDriver(findings, level, skipLowConf)`，先选非低可信度的 driver，只有该档 Finding 全是低可信度时才回退到含低可信度的选择——零检测行为变化、只改排序。`i18n/story_html.go` 的 zh `PointOfNoReturnHead` "死亡转折点"→"不可逆转折点"（与既有 `NoPointOfNoReturn` 文案统一；EN "POINT OF NO RETURN" 是标准习语，保留）。加了 4 条 `TestJourneySeverity` 子测试。D3：文档提的"全低可信度时 verdict 降级为仅次级信号"需 HTML verdict 渲染层改动，价值已由 driver 优选捕获，留待后续（第七部分 P1，原 P2 顺延）。

**核查（第六部分 6.1 #4）**：`lowConfidenceFindings` 目前仅 `unverified_entity_reference` 一项。是否把 `unused_tool_result`（语料 58% 命中）等粗筛检测器也纳入——判为**不据此扩清单**：`unverified_entity_reference` 有明确误报机理（把 Go 标准库类型/在跑端点判"证伪"）才降级；`unused_tool_result` 有收紧校准史、无已知误报机理、journey 级对应物 `ContextUtilization` 是别处头条指标。要动须先对其命中样本做人工采样误报审计（比照问题 11）。列入第七部分 P4（原 P5 顺延）。

**现象**：对外分享的 Journey HTML 看板，最顶上的"主因判定 · 警告"条，内容是 `疑似引用了已被证伪的实体：工具结果显示 http.Handler, ResponseWriter, http.Request 不存在/未找到`——而这是一个 Go 项目在正常使用 Go 标准库类型。同一份看板另一处又宣布 `/health, /status` "不存在/未找到"，那是这个项目自己正在跑的真实端点。一个对外分享的看板，头条是一句自信的错误结论。另有附带现象：当没有更高优先级 Finding 时，verdict 会退回到最不可靠的 `constraint_text_dropped_at_compaction`（检测器自注释是 "hypothesis-level"），并被标题成 "死亡转折点 → 步骤 39"。

**根因**：`internal/story/severity.go` 的 `JourneySeverity`（第 43 行）：verdict driver 的选法是"最坏严重度档 → 该档内 StepSeq 最早 → Code 序"，**没有按检测器可信度加权**。`unverified_entity_reference` 不在 `criticalFindings`（第 22 行）里、算 warning；只要它是最早的那条 warning，就成了整个看板的头条。（这条检测器本身为什么误报，见问题 11。）

**影响佐证**：LLM 解读层自己能识别这是误报——`-llm-addr` 版把 Step 5/66/89/97 的这条 Finding 归类为"工具调用的边界现象，未影响功能"。偏偏对外看板上没有这段解读（散文段只在 Markdown 里）。

**改法**：`severity.go` 加一张"低可信度、不得单独构成 verdict"的 Code 清单（至少含 `unverified_entity_reference`）；`JourneySeverity` 选 driver 时跳过清单里的 Code，除非没有别的 Finding——那种情况 verdict 降级为"仅次级信号"而不给结论。`constraint_text_dropped` 驱动时标题去掉"死亡"这种确定性措辞。

**为什么第一组**：这是纯排序层改动（`severity.go` 加一张清单、约 10 行），零检测行为变化、零风险，当天能上，立刻摘掉对外看板最难看的症状。问题 11（改检测器本身）有假阴性风险、需一轮校准，是同一问题的治本——两者同批、分两个 commit，问题 2 先上，问题 11 紧接着（别拖到下一批）。

第三部分对应：R3b-3、R3b-6、R5c-2 的实质。

---

#### 问题 3 · 工具浪费的四个总量数字只在 HTML 卡片里，主报表一个都没有 ✅ 已处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第一组｜用户价值 高·开发成本 低·风险 低。

**本轮落地**：`internal/report/section_efficiency.go` 加 `renderToolWasteTotals`——§7 开头一行 top-line，把 `tool-waste.html` 那四块 stat（累计发出 / 其中死重(%) / ≈浪费 token / 工具集形态数）原样搬进 Markdown，复用 `i18n.ToolWaste(lang)` 的标签保证两处不漂移。`render_doc.go` 的 §0 highlight #2 分支从"第一个利用率 <20% 的形态"改为"`rep.Tools[0]`（已按 `SchemaWasteBytes` 降序，即绝对浪费最大的形态）"，加 8 MiB 地板（少量不可避免的 slack 不制造头条），`i18n.ToolWarn` 签名加 `wasteBytes` 参数、文案改为以浪费量领起。

**现象**：`tool-waste.html` 一屏给出"累计发出 977 MB / 其中死重 661 MB / ≈ 浪费 1.65 亿 token / 68%"。这四个数——回答"我的 Agent 为从不调用的工具付了多少钱"这个产品最能打的问题——**在 `vmr-report.md` 里 `grep` 不到任何一个**。§7 只有逐工具集形态的行（最大单行 499 MB），没有合计。§0 自动亮点报的是"利用率最低"的那个形态（44 MB / 12%），不是浪费最多的（499 MB / 31%），更不是总量。

**根因**：
- `internal/report/section_efficiency.go:41`：§7 只渲 `rep.Tools` 的 Top-5 逐形态行，从不对 `SchemaWasteBytes` / `SchemaBytesShipped` 求和。
- `internal/report/toolwaste_html.go:48`：HTML 卡片算了 `shipped` / `waste` 总和，但只喂给 hero stat。
- `internal/report/render_doc.go:159` 的 `highlights`：#2 条取"第一个 `DeclareUtilization < 0.20` 的形态"就 `break`——31% 利用率的 499 MB 形态被 `< 0.20` 直接滤掉，永远上不了 §0。

**改法**：`renderEfficiency` 开头加一次求和，把 HTML 卡那四块 stat 原样搬进 §7 作 top-line；`highlights` 增一条"按 `SchemaWasteBytes` 绝对量最大"的分支，与"利用率最低"并列报。

**为什么第一组**：一次 map 求和 + 一个 highlight 分支，成本极低；把产品的招牌洞察从副产物补回主产物。

第三部分对应：R3a-2、R1-8。

---

#### 问题 4 · self-traffic 排除随 `llm_key` 有无静默切换，报告不留痕 ✅ 已处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第一组｜用户价值 中高·开发成本 低·风险 低。

**本轮落地（story 侧收尾）**：`story.StoryIndex` 加 `SelfTraffic *SelfTrafficStatus`（`json:"-"`，随 idx 搭车，与 `Cache` 同一先例）；`cmd_story_setup.go` 在过滤候选时算出 `{Active, Excluded}`；`RenderStoryIndexMarkdown` 签名由 `(rows, lang)` 改为 `(idx, lang)`，在标题下无条件输出一行"自指流量排除：已启用（排除 N 条）"或"未启用（未配置 llm_key / self_traffic_client_tags，或已通过 -include-self-traffic 关闭）"。`i18n/story_index.go` 新增两条文案，中英各一。报告附录侧 `c13819a`/前序 pass 已完成，本轮补齐 `vmr-stories.md` 侧。

**现象**：同一条命令、同一批日志，只因为换了一份没有 `llm_key` 的 sidecar 配置，候选 Journey 数从 110 变成 126（多出来的 16 条全是 vmr 自己的 LLM 解读流量），成本表里也多了一行 `vmrstory`。两次运行的产物里**没有任何一个字**提示口径变了——拿两份报告对比的人会以为是数据本身变了。

**根因**：`cmd/vmr/cmd_analyze.go:205` 的 `llmKey` 独立于 `-llm-addr` 解析；没配 `llm_key` 时 self-traffic 排除静默失效。`i18n` 里已经有 `AppendixSelfTrafficExcluded func(n int)`，但只在 `n > 0` 时才渲染——"没排除"这个状态从不落到纸面。本次样例正是用了一份既没 `llm_key` 也没 `self_traffic_client_tags` 的极简 sidecar 配置触发的。

**影响**：不只是成本表一行——整个候选集合的规模跟着变，`-corpus` 的统计分母、Finding 命中率、相关性样本量全受影响。

**改法（就是一条）**：`vmr-report.md` 附录 + `vmr-stories.md` 固定输出一行，无论哪种情况都写：`self-traffic 排除：已启用（排除 N 条）` 或 `未启用（未配置 llm_key / self_traffic_client_tags）`。即把现有 `AppendixSelfTrafficExcluded` 从 `n > 0` 才渲染改成无条件渲染 + 一个 else 分支。

**已落地部分**：`render_doc.go` 的 `renderAppendix` 无条件渲染口径行 + `AppendixSelfTrafficNotExcluded` else 分支（`i18n/report_doc.go` 新增该文案，中英各一句）。`vmr-stories.md` 侧由 story 半区收尾时补齐（`StoryIndex.SelfTraffic *SelfTrafficStatus{Active, Excluded}` 搭 idx 传入，`RenderStoryIndexMarkdown` 签名 `(rows, lang)` → `(idx, lang)`）。

**核查更正（第六部分 6.1 #2）**：初版报告侧分支写的是 `if SelfTrafficExcluded > 0 ... else 未配置`——`Excluded == 0`（配置了 `llm_key` 但当前窗口无自指流量）会落进 else，打出"未配置"，与 story 半区自相矛盾。已改为 `Report2.Meta` 加 `SelfTrafficExclusionActive bool`（`buildInternal` 置 `len(excludeClientTags) > 0`），附录按"排除是否启用"分支，`Active && Excluded == 0` 输出"排除已启用；本次排除 0 条"。带 `TestSelfTrafficExclusion_ConfiguredButNothingMatched`。

**不需要新机制**：初稿"架构解耦批注"提议注入 `X-VMR-Self-Traffic` header + 审计日志加标记——① 那要改 `audit.Record` schema 和路由半区的 header 处理来服务一个纯分析需求，跨了两半区边界；② 它想要的"配置项解耦、不依赖 `llm_key`"能力**已经存在**：`report.yaml` 的 `self_traffic_client_tags: [...]`（`cmd/vmr/selftraffic.go`、`reportconfig.go:50`），与 `llm_key` 独立、UserGuide 也已写。用户在那份极简 sidecar 里补一行 `self_traffic_client_tags: [vmrstory]` 就能排除。所以这里唯一的缺口是"报告不声明当前口径"，加那行状态行即可。

**为什么第一组**：一行文案（无条件渲染 + else 分支）换来跨报告可比性。

第三部分对应：R1-9、R6a-2。

---

#### 问题 5 · `vmr-report.md` §6 会话表到 journey 文件零链接 ✅ 已处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第一组｜用户价值 高·开发成本 低（~30 行）·风险 低。

**本轮落地**：`report.Markdown` 签名加第 4 个参数 `journeyLink map[string]string`（`cmd_report.go` 已有的 `lineageToJourney`，`loadStoriesLink` 早已构建）；`renderDoc → renderSessions → renderSessionRow` 逐层多传一个 map；`renderSessionRow` 命中时把「会话」列渲成 `[s01 (l-xxxx)](stories/journey-<id>.md)`，未命中原样。`archtest` 的 `report ↛ story` 边界不受影响——join 在 `cmd/vmr` 做、传进去的是纯 `map[string]string`。

**现象**：默认套件模式下，§6「会话与任务」列出上千个会话，隔壁 `stories/` 目录里躺着 200+ 个 `journey-*.md`，**§6 里一条链接都没有**（`grep -c "journey-j-" 宏观报表 = 0`）。在 §6 看到一个可疑会话想深挖，只能回命令行重新 `-list-only` 列一遍候选、再靠标题和时间人肉配对。设计文档把"变焦移动零成本"写成了这一半产品的核心主张。

**根因**：join key 两边都现成——`SessionRow.ID` 是 `l-<hash8>`（`internal/report/rows.go` 里代码注释自己写了"joinable against story's JourneyIndexRow.Lineages"），`vmr-stories.json` 每条 journey 带 `lineages: [...]`。`journeyLink map[string]string` 这个映射**已经建好**并传进了 `WriteRequestsIndex`（`internal/report/requests.go:128`），per-tag sibling 文件（`vmr-requests-<tag>.md`）**已经在用它**渲染 `→ 任务叙事见` 链接（`requests.go:468`）。唯独 §6 的 `renderSessions`（`internal/report/section_sessions.go:21`）没收到这个 map。实测 §6 里的 lineage id 去重后约 506 个，142 个能在 `vmr-stories.json` 的 `lineages` 里直接命中。

**改法**：把 `journeyLink` map 多传一层：`renderDoc` → `renderSessions` → `renderSessionRow`，命中的行把「会话」列变成指向 `stories/journey-<id>.md` 的链接，未命中留 `-`。archtest 的 `report ↛ story` 边界不受影响——join 在 `cmd/vmr` 做，传进去的是 `map[string]string`。

**为什么第一组**：数据、映射、渲染函数全现成，只差把线接上；直接补上设计文档核心主张里落地最差的一处，ROI 极高。

第三部分对应：R1-5、R6b-1。

---

#### 问题 6 · compare 的「相对变化」列把所有大差异压平到 ±100% ✅ 已处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第一组｜用户价值 高·开发成本 低·风险 低。

**本轮落地（formatDelta 收尾）**：D1——文档"已落地部分"把量级化 hold 到 C 组，但问题 6 在第一组且"为什么第一组"整段在论证 `156×`，判定文档内部矛盾，按第一组做。`render_compare.go` 新增 `formatDelta(a, b, newLabel)`：`a==0&&b==0`→`—`；`a==0`→本地化「新增」；`b==0`→`-100%`；否则 `r:=b/a`，`r≥100`→`%.0f×`、`r≥2`→`%.1f×`、`r≤0.1`→`%.2f×`、`r≤0.5`→`%.1f×`、中间段→`%+.0f%%`。md/html 两处调用点改传 `r.A, r.B`。列名「对称差」→「变化」/「Change」，脚注去掉分母公式、改说"给方向与量级、非比例精度"。`i18n` 加 `DeltaNew`（`CompareText` + `CompareHTMLText` 各一）。`MetricDiff.DeltaRel`（`delta_rel` 机读字段）与 `Notable` 判定完全不动。旧 `formatDeltaRel` 保留给 corpus 的 cache-hit 中位数表（那里两侧都是 ms 中位数、永不为 0）。加了 `TestFormatDelta`。实测 compare 产物：`5.0s→774.6s` 现渲染 `155×`（原 `+99%`）。

**现象**：同一句指令的两次执行，A 用 5s 做完某步、B 用了 774.6s（155 倍），compare 报告的「相对变化」列显示 `+99%`——看起来像"差不多翻倍"。逐项核对：模型时间 27.8s→1261.0s 显示 `+98%`（真实 45.4×）、Agent 侧执行 5.0s→774.6s 显示 `+99%`（真实 155×）、工具调用 5→89 显示 `+94%`（真实 17.8×）。任何"B 远大于 A"的差异都收敛到 +9x%，读者无法区分 1.6× 和 155×。compare 模块存在的全部意义就是回答"差多少"。

**根因**：`internal/story/compare_metrics.go:45` `DeltaRel = (B-A) / max(|A|,|B|)`——对称相对差（注释写明了，也确实合理：对 A=0 安全，见"人类空闲时间 0.0s → 1315.0s"那一行）。问题全在渲染层：`internal/i18n/story_compare.go:87` 把这列叫「相对变化」/「relative change」——这个词在任何语境下都指 `(B-A)/A`——而表下脚注（`:89`/`:187`）只解释了 ⚠️ 阈值，一个字没提分母是 `max(A,B)`。

**改法（定稿）**：这是个定性列——只要让读者一眼看出"差异往哪个方向、大概多大量级"就够，不追求比例精度。不加列、不动 JSON，只改渲染：

- `formatDeltaRel(rel)` → `formatDelta(a, b)`（`render_compare.go` / `render_compare_html.go` 两个调用点改传 `r.A, r.B`）。逻辑一段，不分 `Kind`：
  - `a==0 && b==0` → `—`
  - `a==0`（从无到有）→ `新增` / `new`
  - `b==0` → `-100%`
  - 否则 `r := b/a`：`r≥2` → `%.1f×`（`r≥100` 用 `%.0f×`）；`r≤0.5` → `%.1f×`（`r≤0.1` 用 `%.2f×`）；中间段 → `%+.0f%%`（走 `(b-a)/a`，即通常意义的相对变化）
- 列名「相对变化」→「变化」/「Change」（不再承诺某个具体公式）。脚注保留 ⚠️ 阈值说明，去掉分母相关的字。
- `MetricDiff.DeltaRel`（`delta_rel` 机读字段）和 `Notable` 判定**完全不动**——只改显示。

产出示例：`156×` / `0.02×` / `+40%` / `新增` / `-100%` / `—`。golden 更新一次。

**已落地部分（仅标签）**：只改了 `story_compare.go` 的列名与脚注——列名「相对变化」/「Relative Change」→「对称差」/「Symmetric Difference」，脚注补上分母 `(B-A)/max(|A|,|B|)` 的明确说明。`formatDeltaRel` → `formatDelta(a,b)` 的量级化渲染（`156×` 那套）属"一行级逻辑"，随 C 组 hold。选「对称差」而非 Part 4 建议的「变化」：前者点名了真实公式、不再让「相对变化」被误读成 `(B-A)/A`，是纯文案层能做到的最诚实命名；等 `formatDelta` 落地时再换「变化」。

**为什么第一组**：一个函数（约 18 行）+ 一条 i18n 文案 + 两处调用点 + golden。不是计算 bug，是标签 bug——把 `+99%` 换成 `156×`，定性信息就传到了。

第三部分对应：R5-1。

---

#### 问题 7 · `-llm-addr ""` 不能用来关掉 `report.yaml` 里配置的 LLM 解读层 ✅ 已处理

**梯队与评分**：第一组｜用户价值 中·开发成本 低（一行）·风险 低。

**现象**：本机 `report.yaml` 配了 `llm_addr`，于是每次 `-journey`/`-compare` 都默认发起 LLM 调用。想在批量生成时关掉它，最自然的写法 `vmr analyze -macro-only -llm-addr ""` 被拒绝，理由是"会对每个 journey 各打一次 LLM 调用"——而用户传的恰恰是"一个都不要打"。

**根因**：`cmd/vmr/cmd_analyze.go:212` 的守卫 `llmAddrExplicit := flagPassed(fs, "llm-addr")` 判的是"这个 flag 有没有被敲"，不是"解析出来的地址是不是非空"。`resolveStringExplicit(true, "", rc.LLMAddr, "")` 确实返回 `""`。同一行还有个不自洽：`-corpus`/默认套件下 `report.yaml` 的 `llm_addr` 本来会被正常忽略（不报错），显式传空值反而报错。

**改法（已落地）**：守卫加判解析值——`if llmAddrExplicit && llmAddr != "" && (*corpusFlag || !hasSelector)`（`llmAddr` 是第 195 行已解析的值）。注意保留 `llmAddrExplicit &&`：只去掉它、单判 `llmAddr != ""` 会引入回归——`report.yaml` 配了 `llm_addr`、命令行没敲 flag 时，`vmr analyze -corpus` / 裸 `vmr analyze` 会因此新报错，而这两种形态下 config 的 `llm_addr` 本来（也应当）被静默忽略。`cmd_story.go` 的同款守卫一并改。加了回归测试 `TestCmdAnalyze_EmptyLLMAddrNotRejectedInDefaultSuite`。

**为什么第一组**：一行，且修掉一个行为不自洽。之前得靠换一份只有 `language:` 的 sidecar 配置绕过。

第三部分对应：G-1。

---

### 第二组 · 重要，第二梯队

#### 问题 8 · 100% 请求失败的 journey 被报成"未检测到疑似问题" ✅ 已处理（2026-09-01 · Sonnet 5）

**梯队与评分**：第二组｜用户价值 高·开发成本 中·风险 低中。

**本轮落地**：`Step` 结构加 `Outcome string` 与 `ErrorClass string` 字段；`fillStepFacts`（`journey_stepfacts.go`）从 `audit.Record.Outcome` 及 attempts 的 `ErrorClass` 提取；`stepRoleTag`（`render_spine.go`）将 `s.Outcome == "error"` 纳入 `t.StepTagError` 判据；`renderOverviewCard` 统计失败步骤数，在概览中输出 `N/M 步请求失败（HTTP/上游错误）`；`story_spine.go` 同步双语 `OverviewFailedStepsLine` 文案。

**现象**：一个三条请求全部失败（`❌error`）的 journey，叙事半区的产物：概览没有任何失败提示（`finish=-` 是唯一线索），三个 Step 全标 `👀 观察`（`stepRoleTag` 七级优先级里最低的一档，而 `⚠️错误` 排第二），Findings 明确宣布"未检测到规则可判定的疑似问题"。这三条记录在宏观半区的 `vmr-requests-failed.md` 里逐条列着 `❌error`。本批 126 个 journey 中 3 个全失败、32 个含失败请求。

**根因**：`internal/story/render_spine.go:140` 的 `stepRoleTag` 只扫 `s.NewEvents` 里的 `isErrorMarker`（Anthropic Messages 协议的 `is_error` 工具结果标记），完全不读请求级的 `Outcome` / `error_class`。而 `audit.Record.Outcome`（`internal/audit/audit.go:56`，取值 `ok|error|canceled`）在 `buildFrom` 时是可见的，只是 `fillStepFacts`（`internal/story/journey_stepfacts.go` ~第 42 行）没把它提取进 `Step`。`AttemptFact`（`journey_stepfacts.go:19`）也只留了 `{Provider, Model}`，没有 error class。

**改法**：`Step` 加一个 `Outcome` 字段（`fillStepFacts` 里从 `rec.Outcome` 提取）；`stepRoleTag` 把 `Outcome == "error"` 纳入 `⚠️错误` 判据；概览增加一行 `N/M 步请求失败`；Findings 段在"未检测到"之前先声明请求级失败计数。区分 `canceled` 与 `error`（前者不算异常）。

**为什么第二组**：是正确性问题，但放第二组——宏观半区 `vmr-requests-failed.md` 已经逐条收着这些失败、用户较少专门盯全失败 journey；且要动 `Step` 结构 + `structure.json` 契约 + golden，比第一组那批"一行改文案"重，回归面也大（别把已经标了 `⚠️错误` 的重复计）。

第三部分对应：R4-12。

---

#### 问题 9 · `journey-*.md` 一个行为指标都没渲染 ✅ 已处理（2026-09-01 · Sonnet 5）

**梯队与评分**：第二组｜用户价值 高·开发成本 中·风险 低。

**本轮落地**：新增 `render_indicators.go` 与 `i18n/story_indicators.go` 双语支持；在 `RenderMarkdown` 概览卡片下方新增 `## 行为指标` 章节，完整渲染 14 项行为指标表格（净工作时长/模型时间/Agent 执行时间/人类空闲时间/模型工具耗时比/工具调用次数/重复动作率/输出重复率/错误恢复次数/计划执行比/上下文利用率/Compaction 次数/Compaction 损失/模型切换次数）及基于 8 级点阵的 ASCII 上下文演化趋势图（`Context Token Trajectory`）。已同步更新 golden 测试。

**现象**：设计文档把"规则派生的行为指标"称作叙事半区的核心（零 LLM 成本、确定性、跨框架可比，四类使用场景里三类直接依赖它）。但人读的主产物 `journey-*.md` 里 `grep` 不到"净工作时长""重复动作率""上下文有效利用率"任何一个。17 项指标全在 `.json` 里、以及跑 `-html` 才有的 HTML grid 里。用户想知道"这个任务的时间花在模型推理还是工具执行上""上下文有效利用率多少"，只有两条路：读 JSON，或额外跑一次 `-html`。

**根因**：`internal/story/render_md.go:44` 的 `RenderMarkdown` 渲染 sysprompt / 概览 / 模型使用 / 决策脊柱 / 时序图 / Findings——**没有指标节**。`renderOverviewCard`（`internal/story/render_spine.go:93`）只透出 3 个布尔 `structuralTags`（tool-intensive / retry-heavy / context-compacted）加成本行。数据（`Metrics` 结构体）是算好的。

**改法**：`## 概览` 之后加一节 `## 行为指标`，把 17 项渲染成一张表 + `context_composition_curve` 的 ASCII sparkline。

**为什么第二组**：价值高、数据现成、纯渲染层欠账；但它不产生错误结论（只是"要去 JSON 里找"），排在纯正确性项之后；且要双语 i18n 字符串 + 表渲染器 + sparkline + golden。

第三部分对应：R4-2。

---

#### 问题 10 · HTML 看板系统性丢掉了 Markdown 里的认知谦逊 ✅ 基本处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第二组（本轮随问题 2 同批做掉）｜用户价值 中高·开发成本 中·风险 低。

**本轮落地**：`corpus_coverage.go` 抽出语言无关的 `journeyAnthropicCoverageCodes(j) (codes, ok)`；`RenderHTML` 的 Findings 段在有 Finding / 无 Finding 两种情况下都先输出 `<blockquote class="coverage-note">` 协议盲区声明（对齐 `.md` 路径 `render_spine.go` 的行为）。`i18n/story_html.go` 加 `AnthropicCoverageNote func(codes) string`，中英各一。"死亡转折点"措辞随问题 2 一并软化。`VerdictClean` / `NoPointOfNoReturn` / `DiffusePointOfNoReturn` 文案软化在前序 pass 已完成。D4：指标 grid「错误恢复次数 0」在协议盲区下的单格特判（改成 `n/a`）留待后续，属问题 18/43 同类。

**现象**：同一份 journey，`.md` 的 Findings 段开头有一句"以下信号依赖仅 Anthropic Messages 协议才会填充的字段，在本 journey 上结构性无法触发——未出现不代表检查过没问题"，HTML 看板里**整段消失**，同时 HTML 的指标 grid 大大方方展示着 `错误恢复次数 0`（正是那段声明点名"结构性无法触发"的指标之一）。无 Finding 时，`.md` 写"未检测到规则可判定的疑似问题"，HTML 写"未检测到不可逆的转折点——**本次运行始终在轨**"（一个肯定性判断，依据只是"没有检测器触发"）。**对外分享的那份反而更不诚实，方向反了。**

**根因**：`journeyAnthropicCoverageNote`（`internal/story/corpus_coverage.go:81`）只被 `render_spine.go:271`（Markdown 路径）调用，`render_html.go` 不调。`i18n/story_html.go` 的 `VerdictClean` 文案本身是肯定句。

**改法**：`RenderHTML` 也调 `journeyAnthropicCoverageNote`，协议盲区声明进看板；`VerdictClean` 文案软化成"未触发任何规则检测器（不等于运行无问题）"。（"死亡转折点"措辞和问题 2 一起改。）

**为什么第二组**：不涉正确性，但对外产物的诚实度是错方向、值得排在正确性项之后立刻做；要 `RenderHTML` 传参 + i18n 文案 + 布局。

第三部分对应：R3b-2、R3b-6。

---

#### 问题 11 · `unverified_entity_reference` 检测器系统性误报 ✅ 已处理（2026-08-31 · Sonnet 5）

**梯队与评分**：第二组→第一组（与问题 2 同批，review 4.5.3 明确"别拖到下一批"）｜用户价值 高·开发成本 低-中（改动约 20 行、局限在 `detectUnverifiedEntityReference` 一个函数）·风险 中（窗口过紧会翻成假阴性，已跑语料校准）。

**本轮落地**：`chatmsg/entities.go` 重构——`ExtractEntities` 拆成 `collectEntitySpans`（七条规则扫描 + 去嵌套 + 排序）+ `entitiesFromSpans`（去重 + cap），行为逐字节不变；新增 `ExtractEntitiesNear(text, anchors [][]int, window int)`，只保留距任一 anchor 区间 window 字节内的实体 span。`detectUnverifiedEntityReference` 改为先 `falsificationRe.FindAllStringIndex` 拿到每处证伪词位置，再 `ExtractEntitiesNear(r.Text, anchors, 160)`——不再整段命中即整段抽实体。加了 `TestExtractEntitiesNear` 与检测器的"实体距证伪词 ~300 字节则不判证伪"回归子测试。

**校准**（RECENT 语料 125 journey，新旧二进制各跑一次 `-corpus`）：`finding_rates.unverified_entity_reference` 0.336 → **0.272**（-19% 相对），**其余 7 个检测器的 rate 逐字节不变**（改动完全隔离）；finding-grouped 对比的 hit/no-hit 由 42/83 → 34/91。未观察到真阳性被误杀（"读到 → 后续引用 → 无复核"的核心链条不受位置窗口影响，被剔除的都是整段扫出来的无关 token）。

**现象**：语料级 34% 的 journey 至少命中一次；某个 518 轮 journey 的 162 条 Finding 里 110 条（68%）来自它。被"证伪"的实体包括：这一步**成功读到的**文档、从设计文档正文里抽出的名词（`config.toml`、`api_key`、`os.replace`、`ChangeSet`）、Go 标准库类型（`http.Handler`、`ResponseWriter`）、项目自己正在跑的端点（`/health`、`/status`）。

**根因**：`internal/story/findings_toolresult.go:239` 的 `falsificationRe`（`ENOENT|not found|404|does not exist|文件不存在|未找到|不存在`）对**整段** `r.Text` 匹配；`:256` 的 `extractEntities(r.Text)` 也从**整段**抽实体（`chatmsg.ExtractEntities` 会抽 CamelCase / snake_case / 路径 / URL / CLI 命令）。两者之间**没有任何位置关联**——一篇讨论 404 处理的设计文档、一次没命中的 `grep`、一条编译错误，只要结果文本里任意位置出现"not found"，从整段抽出的所有 token 就都被判"已被证伪"。设计文档 §3.5a 记录的四轮检测器校准（`reasoning_action_mismatch` / `plan_execution_misalignment` / `unused_tool_result`）**不含这一条**——尽管 §3.5a 声称"九条检测器提交前均在真实语料上跑过校准"。

**影响佐证**：语料级数据独立印证——三个高命中检测器（`unused_tool_result` 58% / `unverified_entity_reference` 34% / `reasoning_action_mismatch` 29%）正是从单 journey 视角发现有系统性误报的那三个。一个在多数样本上都触发的检测器不携带区分信息。

**改法**：`detectUnverifiedEntityReference` 里，先 `falsificationRe.FindAllStringIndex(r.Text, -1)` 拿到每处证伪词的位置，实体只在其邻近窗口（同一行 / 同一 JSON 字段 / 前后 N 字符）内抽取并比对——不再是整段命中即整段抽实体。`chatmsg.ExtractEntities` 内部本就带 span 位置，加一个按窗口过滤的变体即可。改完跑一轮 `-corpus` 校准（分钟级，语料和工具都现成）、固化具名回归测试（跟 §3.5a 那四条一样）。

**为什么和问题 2 同批（但分两个 commit）**：问题 2（verdict 选取加低可信度清单）是纯排序层改动、零检测行为变化、零假阴性风险，当天能上、先上——把对外看板最难看的症状摘掉。问题 11 改的是"检测什么"，有窗口过紧翻成假阴性的风险，需要那一轮校准兜底，所以单独一个 commit。但两者是同一个问题的治标与治本，**不该跨批次拖**：只做问题 2、不做问题 11，检测器仍会在 Findings 列表刷屏（110/162）、语料级 34% 命中。这与 4.5.1 的判断一致——问题 11 是"校准已上线检测器"，不是"新增维度"，不撞 v1-complete 红线。

第三部分对应：R4-3、R3b-3、R5b-6。

---

#### 问题 12 · 整份"用量报告"没有一个总成本数字 ✅ 已处理（2026-09-01 · `da9961c`）

**本轮落地**：§0 摘要加成本列（`render_doc.go:summaryCostCell`——无定价时渲"unpriced"而非 `0`）；§2 四张成本表全部带合计行（`section_cost.go` 的 `costTotalOf`/`TotalLabel`），合计旁注三件事：多少行无定价未计入（`UnpricedNote`）、多少来自降级字节估算、多少端点因费率缺组件而只是下界。

**梯队与评分**：第二组｜用户价值 高·开发成本 低中·风险 低（完整价值依赖问题 16）。

**现象**：§0 摘要五列是「请求 / 成功率 / 计费输入(fresh) / 缓存效率 / p95 耗时」——没有钱。§2 的四张成本表（按日 / 按模型 / 按端点 / 按客户端）每一张都没有合计行。用户想知道"这 44 天一共花了多少"，唯一办法是自己把 33 行加起来（而且那些行还不全，见问题 20）。这是本次 review 里最大的信息缺口——一份叫"用量报告"的东西 top-line 缺了用户最关心的那个数。

**根因**：`internal/report/render_doc.go:143` 的 §0 摘要行没有成本列；`internal/report/section_cost.go` 的四张表都是逐行渲染、无合计。

第三部分对应：R1-4。

---

#### 问题 13 · Task 标题行没有汇总 ✅ 已处理（2026-09-01 · Sonnet 5）

**梯队与评分**：第二组｜用户价值 中高·开发成本 低中·风险 低。

**本轮落地**：`requests.go` 的 `renderSessionCard` 在每个 `### tNN` Task 标题下方新增一行 Task 汇总引用块（`> N 轮 · 耗时 X.Xs · fresh A / cached B / out C · 结束于 finish_reason`），并在 `i18n/report_requests.go` 中添加中英双语 `TaskSummary` 支持。

**现象**：`vmr-requests-<tag>.md` 的树形结构（Session → Task → Turn）本身清晰，但 `### t01` 这样的 Task 标题行**只给了轮数**，没有这个任务的总耗时、总 token、成本、用了哪些工具、最终 finish 状态。用户扫这份文件就是想找"哪个任务贵 / 慢 / 出错了"，现在只能自己把 14 行加起来。

**根因**：`internal/i18n/report_requests.go` 的 Task 标题渲染只拼了轮数；聚合数据（per-task 的 token / 时长）在 `SessionRow` / `RequestRow` 层是有的。

**改法**：`### t01` 下补一行 `> 14 轮 · 3m12s · fresh 120K / cached 800K / out 5K · ≈0.8 CNY · 工具 exec×9 read×3 · 结束于 stop`。

**为什么第二组**：直接命中这份文件的主要使用动作，但要在渲染层新拼一行聚合。

第三部分对应：R2-6。

---

#### 问题 14 · 详单页的 tool_call 参数是一坨转义 JSON，正好在最该可读的地方 ✅ 已处理（2026-09-01 · Sonnet 5）

**梯队与评分**：第二组｜用户价值 中高·开发成本 中·风险 低。

**本轮落地**：`internal/reqdetail/render.go` 的 `jsonIndent` 改用 `json.NewEncoder(&buf)` + `SetEscapeHTML(false)` 避免 HTML 转义字符（`<`、`>`、`&`）被编码为 `\u003c` / `\u003e` / `\u0026`，保留真实换行与字符形态。

**现象**：详单页里一次 `write` 调用的 args（20KB 的 Markdown 交付物）被原样打成单行 JSON：`\n` 未还原挤成一行，`>` 和 `&` 被渲染成 `&gt;` / `&amp;`。用户下钻到详单，十有八九就是想看这个 `write` 的内容——最终交付物。

**根因**：`internal/reqdetail` 的 tool_call args 渲染直接 `json.Marshal`（默认 `SetEscapeHTML(true)` 把 `>` `&` 转掉），没有还原换行。讽刺的是 `internal/story/render_spine_args.go` 早就为决策脊柱解决过这个问题（挑"扛负载"的字段 → 起代码块展示完整值、渲染真实换行），详单页没复用。

**改法**：详单页的 tool_call args 复用 `render_spine_args.go` 的"挑扛负载字段 → 代码块渲染真实换行"策略；`json.Marshal` 换成 `Encoder` + `SetEscapeHTML(false)`。

**为什么第二组**：命中一个高频下钻场景，但要动 `reqdetail` 的渲染逻辑。

第三部分对应：R2-2。

---

#### 问题 15 · 162 条 Finding 平铺编号，其中 110 条来自同一个检测器 ✅ 已处理（2026-09-01 · Sonnet 5）

**梯队与评分**：第二组｜用户价值 中高·开发成本 低中·风险 低。

**本轮落地**：`render_spine.go` 的 `renderFindingsSection` 重构为按 `FindingCode` 分组渲染；组间按 `findingTrustTier`（严重故障类优先，低可信度类如 `unverified_entity_reference` 沉底）及最早 Step 排序；每组给出小标题与命中条数（`### code · 命中 N 条（最早 Step M）`），彻底消除 162 条平铺刷屏问题。`i18n/story_spine.go` 同步 `FindingGroupTitle` 双语支持。

**现象**：`journey-*.md` 的 Findings 段把所有 Finding 按 StepSeq 顺序平铺成编号列表，不分组、不计数、不排序。前 6 条全是 `unverified_entity_reference`，措辞逐字相同、只有实体名不同。读者翻到第 20 条就会放弃——这正是项目自己在宏观报表 §7 `provider_quota_exhaustion` 那里论证过的失败模式（"一个会对正确配置持续报警的检测器，只会训练用户忽略整个章节"）。同一条判断在叙事半区没有被应用。

**根因**：`internal/i18n/story_spine.go:144` 的 `FindingHeader` 按 StepSeq 逐条编号，`renderFindingsSection` 不做分组。

**改法**：按 Finding Code 分组 + 每组计数 + 按检测器可信度排序（高可信度的组在前）。

**为什么第二组**：读者体验提升明显，但要重构 Findings 段的渲染。

第三部分对应：R4-3 ①。

---

#### 问题 16 · 主力上游 provider 没有定价，整条成本叙事因此是残的 ✅ 已处理（2026-09-01 · `da9961c`）

**本轮落地**：定价回路整体补完——裸模型名歧义改由 vendor precedence 裁决（非转售商行胜出，残余歧义由 `standard_price_curated.yaml` 别名钉住）；org/path 前缀模型名经 `pricing.ModelBasename` 递归回退到裸名解析；内嵌标准价目表从 LiteLLM 上游数据整体刷新（补齐本仓库自有流量已在跑的模型，如 `gemini/gemini-3.7-flash`、`deepseek/deepseek-v4-flash`、`moonshot/kimi-k3`）；`tools/gen_standard_pricing` 每次刷新打印裸名歧义报告、`-url` 一条命令可复现刷新；`vmr check` 的价目表过期警告 180 天收紧到 60 天。维护成本持续（厂商调价要跟），但单条便宜、工具链已闭环。

**现象**：§2 按日成本表丢掉含全窗口最忙那天（8-25，885 请求）在内的 11/44 天（问题 20）；一个 110 步的真实编码任务 0/110 步能解析出定价（journey 的 `cost.resolved = false`）；§2 没有合计（问题 12）。三个症状同一个根因。

**根因**：不是代码问题，是数据问题——本机主力上游端点不在 `pricing.yaml` / 定价表内，`pricing.RateForEndpoint` 返回 `!ok`，该记录的成本就无法估算。

第三部分对应：R1-1 根因、R3b-4 附带事实。

---

#### 问题 17 · §3 端点健康表对低样本照打 ⚠️，同一份报告三套低样本标准 ✅ 已处理

**梯队与评分**：第二组｜用户价值 中·开发成本 低·风险 低。

**现象**：§3「端点健康」表里 `google-abfs:gemini-3.5-flash | 尝试 2 | 成功 1 | 错误率 50.0% ⚠️`——n=2 的样本被打上和 `sensenova:deepseek-v4-flash`（n=301、错误率 20.6%）同一个 ⚠️ 标记。而 §4「按端点」延迟表对同一批端点标 `⚠️low-n`、明确抑制解读；§6.5 对样本量低于 20 的干脆不给结论。读者面对三套处理，无法建立统一预期。

**根因**：`internal/report/section_reliability.go:41` `if e.ErrorRate > 10 { marker = " ⚠️" }`——没有任何样本量守卫。

**改法**：§3 的 ⚠️ 加 `e.Attempts >= 20` 门槛，低于门槛的行沿用 §4 的 `⚠️low-n` 写法。

**为什么第二组**：改动小，但消除的是一致性问题，值得排进第二梯队和别的判定纪律统一时一起做。

第三部分对应：R1-3。

---

#### 问题 18 · corpus 统计的三处自我限定不到位 ✅ 全部处理（2026-09-01 · Sonnet 5）

**梯队与评分**：第二组｜用户价值 中·开发成本 低中·风险 低。

**本轮落地**：
- (a) `internal/story/corpus.go` 添加 `mechanicalCorrelationPairs` 清单并在 `render_corpus.go` 中过滤掉纯数学定义等价的相关性对（如 `NetWorkingMS` vs `ModelMS`, `ToolCallCount` vs `ContextUtilization` 等），让经验上的非凡相关性浮上 Top-15；
- (b) `internal/story/corpus_sequence.go` 添加 `isSelfRepeat` 过滤器，排除 `exec → exec` / `bash → bash` 纯自重复序列，展示真正的跨工具调用模式；
- (c) 脚注关于注意力衰减与无趋势的诚实性说明此前已完成。

**现象**：
- （a）相关性 Top-15 里榜首 `工具调用次数 ↔ 上下文有效利用率 rho=0.86` 是构造性的（0 次工具调用 ⇒ 利用率 0），脚注却只承认"含不少是同一时间类指标之间的机械关联"。榜单里至少一半（前 6 条）是定义上的恒等式。
- （b）N-gram 榜首四名全是 `exec→exec` / `bash→bash` 自重复——任何 Agent 的最高频 n-gram 必然是它最常用工具的自重复，这是零假设、不是"行为定势"，脚注却写"展示最高频出现的行为定势与异常关联"。
- （c）Context Rot 表五个区间 Finding 密度基本持平（0.22–0.31，且 256k+ 只有 56 个 Step 样本），脚注却在讲"高上下文区间下 Finding 密度突增反映注意力衰减趋势"——描述一个本表没出现的现象，读者容易把 0.30 vs 0.22 读成证据。同表「错误率」整列 0%（本语料 0% 是 Anthropic Messages 协议），显示的仍是自信的 `0%` 而不是 `n/a`。

**根因**：`internal/i18n/story_corpus.go:71` 的相关性脚注只列了时间类；`internal/story/corpus_sequence.go` 的 N-gram 不排除自重复；`internal/story/corpus_contextrot.go` 对应的脚注没有"本批未观测到该趋势"的分支。

**改法**：（a）维护一张"已知恒等关系"清单，这些配对从 Top-15 剔除或单列"机械关联"表。（b）N-gram 排除自重复，或改报"相对独立性假设的提升倍数（lift）"。（c）脚注加一句"本批语料未观测到该趋势"，趋势解读语气收回到条件句；错误率整列测不出时显示 `n/a`。

**为什么第二组**：便宜，且这份产物的自我限定水平是全套里最高的（R5b-7），补齐到位后可信度是这个品类的天花板。

第三部分对应：R5b-2、R5b-4、R5b-5。

---

#### 问题 19 · `vmr-requests-cron-hartbeat.md`：文件名拼 `hartbeat`，内容拼 `heartbeat` ✅ 已处理

**梯队与评分**：第二组｜用户价值 中·开发成本 琐碎·风险 低。

**现象**：文件名是 `hartbeat`，打开第一行写"定时任务 · **heartbeat** 单发会话 × 205"。用户第一眼会当成打错了。

**根因（已核实）**：`internal/report/requests.go:101` 的 `cronFileTag` 对 class `heartbeat` 刻意返回 `hartbeat`。这个拼写**已在 `docs/UserGuide.md`（"`heartbeat`'s file is `vmr-requests-cron-hartbeat.md` specifically"）和 `docs/UserGuide.zh.md` 双语写明**——初稿说"理由只存在于源码注释里、是未追踪的私下妥协"不成立。KNOWN_ISSUES 确实没登记，但 UserGuide 是比 KNOWN_ISSUES 更面向用户的地方。剩下的真问题只是：UserGuide 写了「是什么」，没写「为什么用这个拼法」（源码注释说是 operator 指定的）。

**改法**：两条二选一——① 统一拼成 `heartbeat`（含 UserGuide 双语同步），彻底消除困惑；② 保留现状，在 UserGuide 那句后补半句说明这是 operator 指定的历史拼写。倾向 ①：文件名和文件内标题拼法不一致本身没有收益。

**为什么第二组**：本身琐碎，随别的文档整理顺手清掉。

第三部分对应：R2b-4。

---

### 第三组 · 有价值，可等

判据：价值中、成本低（一行到几十行），随手碰到就清；不紧急、不阻塞任何东西。每条一段自包含描述。

#### 问题 20 · §2 按日成本表静默丢掉 11/44 天（含最忙的一天） ✅ 已处理

**评分**：价值 中·成本 低·风险 低（完整价值依赖问题 16）。**已由 `d9ce2ce` 解决**（初稿基于过期二进制、描述的是旧行为）：`internal/report/section_cost.go` 的按日成本表现在对 `d.CostEstimate == nil` 的日期照常渲染整行、`估算成本` 列填 `-`，只在表尾保留"部分日期无可定价记录"脚注；代码注释明写"a missing row reads as 'no traffic' to anyone cross-checking §5's daily activity"。核实：`hasDate` 门槛仍在（至少一天可定价才渲染整张表），逐行 `cost := "-"` 兜底。按模型/端点/客户端三张表仍跳过 `nil` 行（问题 12 合计行落地时一并处理）。第三部分对应：R1-1。

#### 问题 21 · compare 里 70% 的篇幅是两份几乎相同的 system prompt 全文 ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 高·成本 中·风险 低。

**本轮落地**：`render_compare.go` 的 `renderSysPrompt` 在两侧 prompt 完全一致时输出单行确认；在两侧 prompt 差异时调用 `simpleLineDiff` 渲染紧凑的 unified diff 代码块（`<details><summary>两侧 System Prompt 差异 (约 N 行不同)</summary>`），避免铺满 70% 的冗余文本；`i18n/story_compare.go` 同步中英双语文案。

**原描述**：`internal/story/render_compare.go:204` 的 `renderSysPrompt` 无条件为 A、B 各渲一份 `renderExcerpt`（`compare.go` 里 `sysPromptExcerptChars = 20000`，两侧共 40KB）。实测一份 compare 988 行里 696 行（第 103–798 行）是这两份 95% 相同的 prompt。已登记的 1.59（逐字一致才合并）对"人类指令 + 同框架 system prompt"这种"几乎相同"的真实场景永不触发。**改法**：这一节渲染成两份 prompt 的 unified diff（"两侧 system prompt 相差 N 行，差异如下"），既短得多，又直接回答"环境差在哪"这个对比场景真正的问题。第三部分对应：R5-2、R5-7。

#### 问题 22 · compare 报告没有 verdict / 摘要行 ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 中高·成本 低中·风险 低。

**本轮落地**：`render_compare.go` 开头新增 `renderComparisonSummaryCard`，提取显著差异 Top-3、分叉点位置、端点是否一致、终止状态四项事实卡片输出于标题下方；`i18n/story_compare.go` 同步中英双语 `SummaryCard` 支持。

**原描述**：`internal/story/render_compare.go:16`：从 `# Journey 对比` 标题直接进 `## 初始指令`，中间没有一句话总结。Journey HTML 看板有「主因判定」条，compare 什么都没有。读者要自己把六个小节的结论拼起来才知道"同一句指令，B 慢了 62 倍、多调了 17.8 倍工具、换了端点、缓存首轮命中率 82% vs 39%、最后 `finish=(无)`"。**改法**：开头加一段 3–5 行摘要卡（最大差异项 Top-3 + 分叉点位置 + 端点是否不同 + 终止方式），措辞保持"陈述事实不判定优劣"。第三部分对应：R5-3。

#### 问题 23 · §6 会话表没有时间列 ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 中高·成本 低·风险 低。

**本轮落地**：`section_sessions.go` 会话表新增「时间范围」列（`formatSessionTimeRange` 输出紧凑时段，如 `08-16 02:39 → 02:42`）；`i18n/report_sessions.go` 表头扩展为 7 列（双语同步）。

**原描述**：`internal/report/section_sessions.go` 的 `renderSessionRow`：表头是「会话 / 标题 / 轮 / 任务 / fresh/cached/out / 结果」，没有时间。定位一个会话最自然的线索——它什么时候发生的——不在表里。`SessionRow.From` / `To` 已经在 JSON 里（`internal/report/rows.go`），只是没渲染到 Markdown。用户记得的是"上周三那个 SSH 的任务"，不是 `s1562`。`vmr-stories.md` 的候选索引反而有时间范围。**改法**：加一列时间范围。第三部分对应：R1-6。

#### 问题 24 · journey 头部时间范围的结束时刻没有日期 ✅ 已处理

**评分**：价值 中·成本 低·风险 低。`internal/story/render_md.go:51`：`j.To` 格式化成 `"15:04:05"`（无日期），于是一个跨 20 小时的 journey 头部写着 `39 任务 · 518 轮 · 2026-08-24 15:59:22 → 11:47:37`，`11:47:37` 早于 `15:59:22`，读者得自己推断"哦是第二天"。（story **索引** 有 `01-02 15:04`，是 journey **md 头**漏了。）**改法**：跨日时结束时刻补上日期。第三部分对应：R4-5。

#### 问题 25 · 44 个输入文件路径在正文第 3 行和附录各铺一屏 ✅ 已处理

**评分**：价值 中·成本 低·风险 低。标题下第三行就是 44 个 `logs/vmr-audit-*.jsonl.zst` 全路径连成的一整段（约 1726 字符），附录里再原样铺一遍。**改法**：正文写"44 个文件 · 2026-07-14 ~ 2026-08-28 · format 10 · 15825 条记录（0 坏行）"，完整清单折进 `<details>`。第三部分对应：R1-11。

#### 问题 26 · §3/§4 的端点表各 70 行平铺，一半是 n≤20 的噪声 ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 中·成本 低·风险 低。

**本轮落地**：`section_reliability.go` (§3) 与 `section_latency.go` (§4) 在每个协议分组中将 `Attempts < 20` 或 `Requests < 20` 的低样本行拆分折叠进 `<details><summary>+ 另有 N 个低样本端点</summary>`；`i18n/report_reliability.go` 与 `i18n/report_latency.go` 同步双语折叠说明。

**原描述**：§4 已经把它们标成 `⚠️low-n` 了——既然已经判定"不足以解读"，就没有理由让它们和主力端点抢同样的视觉权重。**改法**：low-n 行折进 `<details><summary>另有 N 个低样本端点</summary>`。第三部分对应：R1-12。

#### 问题 27 · §6.7 Compaction 还原表出现"保留比 >100%"的行，无解释 ✅ 脚注已处理；`0 → 0` 渲染本轮补齐（第六部分 6.1 #5）

**评分**：价值 中·成本 低（脚注）到中（收紧检测）·风险 低。`internal/report/section_compaction.go` 的 `retentionRatio = tokens_out / tokens_in`，值 >100% 时代码注释自己写了"worth a second look at whether it's really a compaction rather than a heuristic false-positive — collect()'s Compaction detection is deliberately loose"。表里出现 `246 → 1.1K = 431.0%` 这种行，其余三列（压缩会话 / 续接会话 / 吞掉的实体）全是 `-`，不携带任何可行动信息。**已落地**：`i18n/report_compaction.go` 加脚注说明 retention ≥ 100% 何时合法（宽松检测假阳性 / 结构化扩展）。**本轮补齐**：`0 → 0`（`c.UsageOK == false`，usage 未解析）size 列改渲 `-`——`0 → 0` 读起来像"测得的空操作"。**判为不做**：对 `TokensIn == 0 && TokensOut == 0` 的行做去噪过滤——那些行带 `Summarizes` 链接和 swallowed entities，过滤会丢缝合关系信息；"重复 3 次"是 3 条独立 compaction 重试记录，去重同样丢信息。第三部分对应：R1-7。

#### 问题 28 · 失败索引是纯时间序平铺，看不出"哪次是一场雪崩" ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 中·成本 中·风险 低。

**本轮落地**：`requests_failed.go` 新增 `clusterFailedRequests`，将时间间隔 ≤ 2 分钟的失败归入同一故障簇，在 `vmr-requests-failed.md` 表头上方输出聚类摘要（如 `65 条失败聚成 12 簇，最大一簇 8 条（08-16 04:00 ~ 04:02 · error×8）`）；`i18n/report_requests.go` 同步双语 `FailedClusterSummary` 文案。

**原描述**：`vmr-requests-failed.md` 65 条失败里有 8 条挤在 2 分钟内、几乎全是 6ms 的即时失败——这显然是一次上游整体不可用引发的连环重试，而不是 8 个独立事故。表头只写"共 65 条"。**改法**：开头加一段聚类摘要（按时间邻近 + `error_class` 聚合），例如"65 条失败聚成 12 簇，最大一簇 8 条（08-16 04:00–04:02，network→error）"。第三部分对应：R2b-2。

#### 问题 29 · 术语在首次出现处没有解释 ✅ 已处理

**评分**：价值 中·成本 低（`i18n` 几行）·风险 低。`⭐`（§0 第 10 行就出现 4 次，解释在 2760 行外的附录）· `msgs`（累计消息数还是本轮新增，读者要看两行才能推出来）· `finish` 的取值（`tool_calls` / `stop`）· `⚠` 前缀（断头 journey，`vmr-stories.md` 图例没说）· `sNNN`（随输入范围重新编号，不是稳定标识，见问题 32）。**改法**：各在首次出现的表下加一行小字。第三部分对应：R1-15、R2-7、R6a-4。

#### 问题 30 · 逐日活跃度图用 `mermaid xychart-beta`，44 个 x 轴标签 ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 中·成本 低中·风险 低。

**本轮落地**：`section_workload.go` 在 Mermaid 逐日活跃度图表下方新增折叠的逐日明细数据表（`<details><summary>+ 逐日活跃度明细表（共 N 天）</summary>`），保证在纯文本或无法渲染 Mermaid beta 的环境下信息不丢失；`i18n/report_workload.go` 同步双语表格支持。

**原描述**：`internal/report/section_workload.go:71` 的 daily 图用 `mermaidBarLabeled(labels…)`，44 个日期标签在任何渲染器里都会挤成一团；`xychart` 至今是 mermaid beta 特性，GitHub 与多数编辑器渲染不稳定；纯文本阅读（`less`/`cat`）时读者看到的是一行 44 个数字的数组。**改法**：daily 图下补一张紧凑表格或 ASCII sparkline 兜底，或直接把 daily 换成表格（hourly 的 24 标签可保留 mermaid）。第三部分对应：R1-14。

#### 问题 31 · `-list-only` 下 `Tasks` / `Rendered` 两列 126/126 全是 `—` ✅ 已处理（图例/`(unresolved)` 前序 pass；空列说明本轮，第六部分 6.1 #3）

**评分**：价值 中·成本 低·风险 低。`internal/story/storyindex.go` 的 `writeStoryIndexRow` 没有模式感知——`-list-only` 不构建 Journey，所以 `r.Tasks == 0`、`r.Rendered == ""`，两列每行都渲成 `—`，读者看到的是"这俩字段坏了"（`Steps` 列有 `r.Requests` 兜底，不受影响）。同一函数里：`r.Client` 为 null 时渲成空白（宏观半区对同类记录用 `## Chat User: (unresolved)`，两半区不一致）；`⚠` 断头前缀没有图例。**已落地**：`(unresolved)` 与 `⚠` 图例前序 pass 已处理；本轮 `RenderStoryIndexMarkdown` 在无任何行带 `Rendered`/`Tasks` 时（含 `MergeJourneyIndexRows` 未带回值），表格上方输出 `ListOnlyNote` 说明这两列留空、`轮数` 显示请求数。不做自适应删列（改表结构、动下游解析预期）。第三部分对应：R6a-1、R6a-3。

#### 问题 32 · `sNNN` 会话序号随输入范围浮动，产物没说明 ✅ 已处理

**评分**：价值 中·成本 低·风险 低。同一条 lobster 会话在 FULL 语料下是 `s432`，RECENT 下变成 `s03`。它看起来像 id，实际不能跨运行引用。`internal/report/section_sessions.go:135` 的代码注释自己知道它是 run-scoped（"never use it as a lookup key"），但产物里没写。**改法**：§6 / 索引里标注 `sNNN` 是"本次报告内的行号，不是稳定标识"；真正的 join key（`l-<hash8>`）已经在括号里给了。第三部分对应：R2-3、R6b-2。

#### 问题 33 · `journey-*.json` 的 `cost.total_usd` 在未解析时是 `0` 且字段名装着 CNY ✅ 已处理（2026-09-01 · `da9961c`）

**本轮落地**：`internal/story/cost.go` 的 `CostFact.Total` 改为 `*float64 json:"total,omitempty"`——未解析时字段整体省略（`null`/`0` 都不再出现），字段名去掉 `_usd`（`currency` 字段旁边单标币种）；`by_model[].usd` 同步改 `by_model[].amount`。**Breaking**（消费方需同步），已在 CHANGELOG 声明。随同批次落地：批量 analyze/story 渲染不再传 nil cost（所有 `journey-*.md/.json` 都带成本）、story 侧计价基准与 report 对齐（`ServedEndpoint` + 降级估算披露 `estimated_steps`），由 `cmd/vmr/cost_basis_parity_test.go` 钉住。`internal/story/cost.go:35`：`CostFact.TotalUSD float64 json:"total_usd"`——`resolved: false` 时是 `0` 而不是 `null`/省略；且 `-currency CNY` 下这个 `_usd` 字段装的是 CNY。渲染层做对了（未解析时 damage 行不显示 0），问题只在机读契约：任何不检查 `resolved` 就读 `total_usd` 的消费方会得到"免费"。**改法**：`resolved: false` 时用 `omitempty` 或指针置 `null`；字段名去掉 `_usd`（`currency` 已经单独标了，叫 `total` 即可）。改前查 `_eval/` 消费方；契约文档说只有 `Code` / `EvidenceAnchor` 是稳定锚点。第三部分对应：R3b-4。

#### 问题 34 · stale-binary 防呆 ✅ 已处理

**评分**：价值 中·成本 低·风险 低。这次生成用了比 HEAD 落后一个提交的 `./vmr`，导致一度把已修复的 1.57 脚注报成"缺失"，也漏掉了 R2-4 已被修复的事实。`buildinfo` 已经输出 VCS commit 哈希。**改法**：`vmr.sh status`（或 `redeploy`）里比一下 `vmr version` 的 stamp 与 `git rev-parse HEAD`，不一致给个 warning。不用进 CI（`vmr.sh redeploy` 本就 stop+build+start，只有直接跑 `./vmr` 才会踩）。第三部分对应：3.3.6 第一条。

#### 问题 35 · 一批琐碎项，一次 PR 扫掉 ✅ 已处理（§6.6 排序子项经 2026-09-01 三轮核查与现行代码不符，注销；`finishCell` 缺陷已随 6.1 #1 修掉，第六部分 6.1 #1）

**已落地**：`[Ⓜ️ Markdown]` → 请求坐标（`detailCell` 用 `r.Req`，缺失时回退 `r.DetailFile`；`render_cells_test.go` 同步）；§1 两个百分比统一为「of in」（`section_tokens.go` 的 fresh 占比分母由 `fresh+cached` 改为 `o.TokensIn`，与 cached 行的 `CacheHitRate` 同分母，`OfFreshCachedSuffix` 文案字段删除、合并进 `OfInSuffix`）；`reqdetail` 详单页 `<strong>` 全部改 `**`、`PrevTurnLink` 去掉悬空 ` · ` 前缀并补段落空行、`HistoryVsNewNote`/`IncrementNote` 全角逗号（`IncrementNote` 保留 `#1–#` 区间写法，与两个同类文案一致）；`❌error` → `❌unclassified`；`-h` 去掉 "list/"（`cmd_analyze.go` + `cmd_story.go`）；`-currency` 无汇率降级在 §1/§2 的 Disclaimer 补一句（`Pricing.RequestedCurrency` 新字段）；`semantic_oscillation` 等 4 个 Phase 1b LLM 检测器登记进设计文档 §3.5a（并补了 Phase 1b 组的框架说明，把"全部零 LLM 成本"这句收紧到规则层九条）。

**核查更正（2026-08-31 · Sonnet 5）**：R1-13"§5.5 按客户端用字母序"**与当前及 review 时源码均不符**——`internal/report/aggregate.go` 自 2026-07-26 起就有 `sort.Slice(rep.ByClient, Requests desc)`，§5「按客户端」表直接 range 已排好序的 `rep.ByClient`。该子项作废。

**核查新发现并已修（第六部分 6.1 #1）**：`requests.go` 的 `finishCell` 旧逻辑仅在 `Outcome != "ok" && ErrorClass != ""` 时输出 `❌`，于是路由前被拒（缺 model / 非法 JSON / body 超限）或队列中被取消的失败轮次——`Outcome` 非 ok 但无 `ErrorClass`——在 `vmr-requests-<tag>.md` / `-cron-<tag>.md` 逐轮表里渲成一个无害的 `-`。已对齐 `outcomeCell` 的 `unclassified` 兜底。带 `TestFinishCell_UnclassifiedFailureIsNotABareDash`。

**待做子项的处置（2026-09-01 三轮核查）**：§6.6 排序子项**注销**。现行排序（`section_endpoint_value.go`，自 8-15 起未变）为：有定价行优先 → `costPer1MOut` 升序 → `wastedMS` 降序 → endpoint。即无定价块内部已经按失败耗时降序把爆点浮在块首——review 观察到的"minimax 第 2 行 / bai 最后一行、`-` 行沉底像最贵"与现行代码不符（`minimax` 无定价本应排在无定价块首位；`bai` 若无定价同理）。`-` 行在无定价块内是按 wastedMS 排的，不是随机沉底。剩余可做的仅是"把主键本身换成 wastedMS"这种口味调整，价值不足一列出。

**评分**：价值 低-中·成本 每条琐碎·风险 低。每条都是一行到几行的改动，价值密度低但清掉了碍眼：

- **`[Ⓜ️ Markdown]` 链接文字重复 4851 次**（`internal/report/requests.go:636` `detailCell`）——链接文字对所有行相同、不携带信息。换成坐标（`vmr-audit-2026-08-16.jsonl:33`）或干脆「详单」。R2-5。
- ~~**§5.5 按客户端分组用字母序**~~——**作废**：`rep.ByClient` 在 `aggregate.go` 已按请求量降序，§5「按客户端」表直接用该顺序。R1-13 的观察不成立（大概率是把某个其它表看串了）。
- **§6.6 端点性价比排序键与洞察不匹配**——**注销（三轮核查）**：现行排序在无定价块内已按 `wastedMS` 降序，爆点并非沉底；review 对产物行序的观察与代码不符。R1-18。
- **§1 同一张表两个百分比用两种分母写法**（`输入-缓存命中 … 90.0% of in` vs `输入-fresh … 10.5% of (fresh+cached)`，其实同一个分母）——统一成一种。R1-16。
- **详单页 `<strong>` 与 `**` 混用**、悬空分隔符（"下一轮"链接缺席时留下的以 ` · 上一轮:` 开头的行）、中文句子里的半角逗号——`internal/reqdetail` 渲染层。R2-8。
- **`❌error` 读起来像"错误：错误"**——它是"没有更具体分类"的兜底，和 `❌network` / `❌client` 呈现上完全同构，读者会以为 `error` 也是一个具体类别。改成 `❌unclassified` 或 `❌other`。R2b-3。
- **`-h` 文案里 "list/render journeys whose head looks truncated" 的 "list" 那半句不成立**——`-list-only -include-partial` 逐字节等于 `-list-only`（断头 journey 本来就一直在列表里，`-include-partial` 只影响渲染范围）；去掉 "list/"，并让这个组合要么报错要么在 `⚠` 旁加图例（对照：同样是空操作的 `-list-only -details` 会报错，标准不一致）。R6c-1。
- **`-currency` 拿不到汇率时的降级只在终端出现，报告文件不留痕**——`vmr-report.md` 写着"货币 CNY"，没提"用户要的是 JPY，没换成"。§2 口径脚注补一句。R6c-2。
- **`semantic_oscillation` 等检测器实际存在但未登记进设计文档 §3.5a 的表**——文档更新。R5c-3。

---

### 第四组 · 不做 / 存疑 / 已过时

#### 问题 36 · `vmr-requests.md` 非纯索引（R2-4）——已由 `c13819a` 解决

**处置**：已解决，仅注明。见 4.2。无动作。

#### 问题 37 · 跨时间窗对比分析（`vmr analyze -compare-period A..B vs C..D`）

**评分**：价值 中高·成本 高·风险 中。**处置：不是现在。** 已登记 1.60。对每天在跑的路由器，"这周比上周差在哪"比"总量"更常用，价值有。但这是新维度、要独立设计（新聚合路径 + diff 渲染层）；且分析半区标了 v1-complete（`KNOWN_ISSUES` §1.0："新增维度从默认冲动改为需理由的例外"）。可能方案是复用现有 `report` 聚合跑两遍 + 一个 diff 渲染层。第三部分对应：3.3.2 跨时间缺口、1.60。

#### 问题 38 · 任务是否达成目标的信号

**评分**：价值 中·成本 高·风险 高。**处置：不建议。** 已登记 1.61。vmr 结构上看不到任务结果；硬做要么引 LLM 判官（越过"揭示事实不当裁判"的边界）要么让用户标注。除非 corpus 分析反复因为缺这个信号得不出结论，否则不做。第三部分对应：3.3.2、1.61。

#### 问题 39 · 「上下文有效利用率」是双峰退化的 ✅ KNOWN_ISSUES 记账已处理

**评分**：价值 低·成本 中-高·风险 高。**处置（定稿）：不改。`KNOWN_ISSUES` §2 记一条，别的都不加。**

**第一性原理判断**：实测 111 个 journey——`= 0` 的 23 个（21%）、`>= 0.99` 的 35 个（32%）、中间稀疏。`contextUtilization`（`internal/story/metrics.go:327`）对没有 tool call / 无可抽实体的短任务退化为 0——那是"未定义"，不是"0% 利用率"，均值/中位数因此把退化值和真实值混在一起算，确实没意义。**但这个指标是 17 项里的一个次级数值**：不进任何标题、不驱动 verdict、无 GTM 工件依赖，review 自己的 3.3.2「用户会问的问题」表里也没有一条落在它上面（"上下文预算花在哪"由 `context_composition_curve` 回答，不是它）。修它要么加退化护栏（改 JSON 契约、YAGNI——无消费方）、要么重定义"utilized"判据（撞 v1-complete 红线）。**用户侧价值太低，不值得动。** 与问题 11（检测器误报会刷屏 Findings、驱动对外看板头条）不是同一量级——那个必须修，这个不必——是价值不对称，不是判断双标。

**处理（从简）**：`KNOWN_ISSUES` §2 补一条：`context_utilization` 对短/无工具 journey 退化为 0，corpus 的均值/中位数不可作判断依据，看两端计数。不加分布形状、不改渲染、HTML 看板那格 `100%` 不特判。第三部分对应：R5b-3。

#### 问题 40 · `obster` 这个 client 有数据但没有对应的分组文件 ✅ 已处理（2026-09-01 · Sonnet 5）

**评分**：价值 低·成本 低（对账）·风险 低。

**本轮落地**：`render_doc.go` 在附录中比对 `rep.ByClient` 与 `clientsWithSiblingFile(rep)`，当存在有成本/负载数据但未独立生成 sibling 文件的客户端时，输出对账声明（`i18n.Doc(lang).AppendixClientReconciliation`）；非 bug，口径差异已明确对账。

**根因（已核实，非推测）**：`obster` 是审计日志里真实存在的 `client_key`（`vmr-report.json` 有 `"client_key": "obster"`），对应唯一一条会话 `l-471ec11c`，`class: heartbeat`、`title: [OpenClaw heartbeat poll]`、`requests: 1`、时间 `2026-07-16T10:53:29`——即用户某次把 key tag 少打了首字母、发了一次 heartbeat poll。不是渲染层把 `lobster` 写坏，也不是 off-by-one：`partitionGroups`（`internal/report/requests.go:314`）对「`g.id != "" && g.class != "interactive" && g.requests == 1`」的单发定时会话一律并入 `scheduled[class]` 滚动桶、不进 `chatUser`，`clientsWithSiblingFile` 从 `chatUser` 派生，于是 `obster` 拿不到 `vmr-requests-<tag>.md`、也不进 §0/§8 链接列表（这一行为 UserGuide 已写明）。而 §2/§5 的 ByClient 表是对全部记录无差别聚合的，所以 `obster` 在成本表里有行。这不是 bug，是「单发定时会话剥离」与「ByClient 无条件聚合」两种口径在呈现上不一致。

**改法**：§2 按客户端表的 client 集合与 per-client 文件/链接集合不一致时，附录输出一行 diff（`成本表有、无 sibling 文件：obster（1 条单发 heartbeat）`）。第三部分对应：R1-10。

#### 问题 41 · anthropic-messages 语料覆盖盲区

**评分**：价值 中（验证债）·成本 中（攒语料）·风险 低。**处置：记账，不阻塞发布。** 本批语料 0.0% 是 Anthropic Messages 协议，导致 4 个检测器（`error_retry_unadapted` / `error_then_unverified_success` 等）+ 决策脊柱的 tool-result ❌ 徽章 + Context Rot 错误率 + N-gram 尾步错误率整体测不出。要验证那 4 个检测器，需要专门准备一批 anthropic-messages 语料——独立任务。第三部分对应：3.3.6 末条、R4-4。

#### 问题 42 · 大 journey 文件（1.88 MB md / 752 KB html）

**评分**：价值 中·成本 中·风险 低。**处置：等有人抱怨再做。** 只有极少数超大 journey（518 轮、缝合 ×8）触发；`.md` 在编辑器里卡、GitHub >1MB 走 raw 不渲染。改法是超过某个体量时把决策脊柱拆成 `journey-<id>-spine.md` sibling / 时序图按任务折叠。`-details` 不传时步骤指针本来就退化为坐标、不产生死链。第三部分对应：R4-9、R3b-7。

#### 问题 43 · 一批"两个半区各拿半个结论 / 图缺刻度 / 曲线跳号"的小项 ✅ 全部处理（2026-09-01 · Sonnet 5）

**评分**：价值 低·成本 低·风险 低。

**本轮落地**：
- **工具调用时序图刻度**：`renderToolTimeline`（`render_spine.go`）在 steps ≥ 20 时自动输出 step 标尺刻度行（以 `╎` 标 50 步、以数字标 10 步、以 `·` 标单步），方便定位；
- **模型切换记录接上缓存影响**：`modelusage.go` 记录切换前后的缓存命中率并在 `render_modelusage.go` 中展示 `[缓存命中率 A% → B%]`，连接宏观 §6.5 结论；
- **compare 逐轮缓存曲线跳号说明**：`i18n/story_compare.go` 的 `CacheCurveSummary` 已明确注释缺号含义。

**原描述**：
- **工具调用时序图没有横轴刻度**（`internal/story/render_spine.go` 的 `renderToolTimeline`）——看到一处密集重复（`read` 行的 `🔄`×10），无法读出它在第几步、没法回决策脊柱定位。每 50 步打一个刻度行。R4-7。
- **模型切换记录没和宏观 §6.5 的结论接上**——叙事层记了 18 次上游切换、11 次带 failover 标注，但没把 §6.5 已证明的"换端点后缓存效率 93%→22%"接上去（比如在切换记录旁给出该 Step 前后的 cached 比）。两个半区各拿半个结论。R4-8。
- **compare 逐轮缓存曲线跳号未说明**（`R13`、`R22–23` 等直接消失，大概率是那几轮拿不到 usage）——曲线没有任何说明，读者会以为轮次编号本身有问题；`R11 0% → R12 97% → R14 1%` 这种剧烈抖动最值得解释，却被埋在折叠区、没在上方"首轮/稳态/最值"表里体现。R5-5。

---

### 4.5 对既有结论的挑战

1. **"analytics 半区 v1-complete"这条 `KNOWN_ISSUES` §1.0 红线，被本次核查的多数事项证明是对的——但要分清"新维度"和"校准/呈现"。** 问题 9（journey md 补指标节）、问题 11（`unverified_entity_reference` 校准）、问题 15（Finding 分组）、问题 18（corpus 恒等式剔除）——这些**不是新维度**，是把已经算出来的东西正确渲染出来、或把已经上线的检测器修准。红线拦的是问题 37 / 38。别用 v1-complete 挡住问题 9 / 11。

2. **过期二进制不只坑了 1.57，也坑了 R2-4。** `c13819a` 动了 `internal/report/requests.go`，第三部分 R2 那一整批就都该用新二进制重跑，而 2.4 的复核表只复核了"当时起疑的那几条"。教训比"下次先 `go build`"更具体：**复核表要枚举"这次提交 touch 的所有文件"，逐文件重跑相关批次。**

3. **问题 2 与问题 11 是同一问题的治标与治本，同批做、分两个 commit。** 问题 2（`severity.go` 加低可信度清单、约 10 行、零检测行为变化、零风险）先上止血；问题 11（`detectUnverifiedEntityReference` 加位置窗口、约 20 行、局限一个函数、需一轮 `-corpus` 校准兜底假阴性）紧接着。初稿把问题 11 说成"周期天级的大工程"是高估了——检测器缺陷是明确的设计 bug（整段命中即整段抽实体），修法明确，校准语料和工具都现成。别只做问题 2 就收工：检测器不修，Findings 列表仍被它刷屏、语料级 34% 命中。

4. **问题 33（journey json `cost`）的"违反 pricing 不变量"说重了。** `internal/pricing` 的不变量是"nil rate ≠ free"，指 rate 组件；`CostFact` 是聚合结果，`resolved: false` 明明白白同框摆着、包注释也写了。真问题是 `total_usd` 这个字段名在 `-currency CNY` 下装 CNY。改字段名比纠结 null 更值，null 顺手做。

5. **问题 5（§6 journey 链接）不是"跨半区打通"这种工程。** `journeyLink` map 早就建好、早就传进 `WriteRequestsIndex`、sibling 文件早就在用。§6 那张表只是没接线。archtest 的 `report ↛ story` 边界完全不受影响。ROI 比第三部分行文里那些保留说法（"哪怕只在默认套件下有值"）显示的高得多。

6. **`developer` 分桶经 owner 裁定 WAI（4.2）。** `config.yaml` 全局 `role_map: {developer: system}`。技术上分析半区读的是 client 层、`developer` 目前确实并进了 `user` 而非 `system`；真要动最小改法是 `stepContextPoint` 里 `developer → system`。但这属于"要不要改"由 owner 定，本文按 WAI 处理，不进优先级组。

### 4.6 一句话

事实层（详单、diff、证据去重、审计溯源）扎实到可以当法证材料用；剖面层（指标、Finding）的**呈现层**欠账最多——数据算出来了但没渲染到人读产物里（问题 3 / 9 / 12），或者渲染了却被误报和压平的百分比毁掉可信度（问题 2 / 6 / 11）。第一组七条全是"一行到几十行、零风险"的，做完这套产物从"信息很全但要自己拼、且对外那份还挂着误报头条"变成"打开就能用"。

---

## 第五部分 · 第一梯队执行记录（Sonnet 5，2026-08-31）

用户指令：逐条回源码核实第四部分的完成状态与标注准确性 → 修正不准确处 → 重新分组 → 直接处理第一梯队。以下是本轮实际改动。

### 5.1 核查结论（基于当前 HEAD 源码，非文档说明）

**✅ 标注准确**（逐个回源码验证）：问题 1（`report_doc.go` `90%`、`SummaryRequests` 入 i18n）、7（`cmd_analyze.go:216` guard 含 `llmAddr != ""` + 回归测试）、17（`errorRateMarker` 有 `Attempts<20` 门槛）、19（`cronFileTag` 无特例 + UserGuide 双语）、24（`render_md.go` 跨日补日期）、27（`report_compaction.go` retention≥100% 脚注）、29（⭐/msgs/finish/⚠/retention 首现处说明）、32（`report_sessions.go:TableNote`）、34（`vmr.sh:warn_if_stale`）、39（`KNOWN_ISSUES §2.64`）。

**⚠️ partial 标注准确**：问题 4、6、10、18、31。

**需要修正的标注（已改）**：
- **问题 20**：标题 ✅ 现已准确（`d9ce2ce` 改 `section_cost.go` 渲染全部日期行、`-` 填成本列），但正文仍描述旧行为、把改法当待办 —— 已重写为完成态。
- **问题 35 §5.5 子项**：**事实错误**。`rep.ByClient` 自 2026-07-26（`aggregate.go`）起即按 `Requests` 降序，§5「按客户端」表直接用该顺序。R1-13"字母序、aiscript 排第一"与当前及 review 时源码均不符 —— 已作废该子项，标题「两个排序子项」改为「§6.6 排序子项」。
- **问题 25**：附录仍 join 完整清单一次（正文已折 `<details>`），残留但可接受 —— 保留 ✅。

**未标注项确认确实未做**：2、3、5、8、9、11、12–16、21–23、26、28、30、33。

### 5.2 本轮完成（第一梯队 + 重新调整进第一梯队）

| 问题 | 状态 | 核心改动 | 验证 |
| --- | --- | --- | --- |
| **2** verdict 头条由误报驱动 | ✅ | `severity.go` 加 `lowConfidenceFindings` + `pickDriver(skipLowConf)`：非低可信度 driver 优先，全档低可信度才回退。zh "死亡转折点"→"不可逆转折点" | 4 条 `TestJourneySeverity` 子测试；实测大 journey verdict 改由 `constraint_text_dropped`（真实 critical）驱动 |
| **3** 工具浪费四总量只在 HTML | ✅ | `section_efficiency.go:renderToolWasteTotals` —— §7 top-line 四总量（复用 `i18n.ToolWaste` 标签）；§0 highlight 改取 `rep.Tools[0]`（绝对浪费最大），`ToolWarn` 加 `wasteBytes` | `TestRenderToolWasteTotals`；实测 §7 `Total shipped 273.62 MB · Dead weight 218.84 MB (80%) · ≈ tokens wasted 54.7M · Tool-set shapes 6`，§0 `tools:67 wastes 183.26 MB` |
| **4** self-traffic 口径不留痕（story 侧） | ✅ | `StoryIndex.SelfTraffic *SelfTrafficStatus`（`json:"-"`，随 idx 搭车）；`setupStoryRun` 算 `{Active, Excluded}`；`RenderStoryIndexMarkdown(idx, lang)` 顶部无条件输出口径行 | `TestRenderStoryIndexMarkdown_SelfTrafficLine`；实测 `> Self-traffic exclusion: not active (...)` |
| **5** §6 会话表零 journey 链接 | ✅ | `report.Markdown` 加 `journeyLink` 参数（`cmd_report.go` 已有 `lineageToJourney`）；`renderSessions`/`renderSessionRow` 逐层传；命中行「会话」列变链接 | `TestRenderSessions_JourneyLink`；实测套件产物 **141 个 §6 行链到 `stories/journey-*.md`**（review 估 142） |
| **6** compare「相对变化」压平到 ±100% | ✅ | `render_compare.go:formatDelta(a,b,newLabel)` 量级化（`156×`/`0.02×`/`+40%`/新增/`-100%`/`—`）；列名「对称差」→「变化」；脚注去公式；`i18n` 加 `DeltaNew`（两个 struct）。`DeltaRel` 机读字段与 `Notable` 判定不动。旧 `formatDeltaRel` 留给 corpus cache-hit 表 | `TestFormatDelta`（含 R5-1 的 `5.0s→774.6s` = `155×`） |
| **10** HTML 看板丢认知谦逊 | ✅ 基本 | `RenderHTML` Findings 段输出 `<blockquote class="coverage-note">` 协议盲区声明（对齐 `.md`）；`i18n` 加 `AnthropicCoverageNote`。"死亡转折点"随问题 2 软化 | `TestRenderHTML_DashboardStructure` 加断言；实测 EN/ZH HTML 均含 coverage-note、无「死亡」 |
| **11** `unverified_entity_reference` 系统性误报 | ✅ | `chatmsg.ExtractEntitiesNear(text, anchors, window)`（新，重构自 `ExtractEntities`，行为逐字节不变）；`detectUnverifiedEntityReference` 改为 `FindAllStringIndex` 取证伪词位置 + 160 字节窗口内抽实体 | `TestExtractEntitiesNear` + 检测器"实体距证伪词 ~300 字节则不判"回归子测试 |

**问题 11 语料校准**（RECENT 125 journey，新旧二进制各跑 `-corpus`）：`finding_rates.unverified_entity_reference` **0.336 → 0.272**（-19% 相对），**其余 7 个检测器 rate 逐字节不变**（改动完全隔离）。大 journey `b0ebb0e4`（518 轮、缝合×8）：该 Finding 110 → 78（-29%），总 Finding 338 → 306。残留的 78 条里仍有假阳性（`~/.codex/config.toml not found` 后 agent 创建了它、`use/apply/diff/` 是 `dirPathRe` 过度匹配散文），属检测器自注释已承认的"实体可能被后续步骤创建" + `chatmsg.ExtractEntities` 精度问题，非窗口能解决 —— 位置窗口是明确的净改进（surgical、零 collateral、已校准），完整精度需另立项收紧 `ExtractEntities`（会牵动 `unused_tool_result` 与 compaction 段）。问题 2 已保证它不再当 verdict 头条，问题 15（Finding 按 Code 分组）会解决展示刷屏 —— 三者合起来是完整治理。

### 5.3 决策记录（用户离开期间自主决定）

- **D1**：问题 6 的 `formatDelta` 量级化，文档"已落地部分"曾 hold 到 C 组，但问题 6 在第一组且"为什么第一组"整段在论证 `156×` —— 判定文档内部矛盾，按第一组处理，实现之。列名「对称差」→「变化」（文档预告 formatDelta 落地时改）。
- **D2**：问题 11 校准 —— 本机 `logs/` 有 47 个审计文件，跑了 RECENT 语料 `-corpus` 双版本对比，确认 -19% 且零 collateral 后采纳。
- **D3**：问题 2"全部 Finding 都是低可信度时 verdict 降级为仅次级信号" —— 需 HTML verdict 渲染层 + i18n 改动，价值已由 driver 优选捕获。本轮只做 driver 优选，"降级"细化留后续。
- **D4**：问题 10 的指标 grid「错误恢复次数 0」单格特判（→ `n/a`）留后续，属问题 18/43"图缺刻度"同类。
- **D5**：问题 2"死亡转折点"→"不可逆转折点"（与既有 `NoPointOfNoReturn` 文案统一）；EN "POINT OF NO RETURN" 是标准习语，保留。
- **执行失误自记**：首次渲染校验用的 `vmr.fix` 二进制构建于问题 2/11 改动之后、问题 3/4/5/6/7 改动之前，导致第一次目视校验 §7/§6/self-traffic 三处都"没出现"。重建 `vmr.fix2` 后全部正常 —— 与第二部分 2.4 同一类教训（二进制 ≠ 源码当前态），改动中途要重新 `go build`。

### 5.4 测试与门禁

`go build ./...` OK；`go test ./...` 全绿；`go test -race ./internal/{story,report,chatmsg,i18n,archtest} ./cmd/vmr` 全绿；`go test -count=1 ./internal/archtest/...` 全绿（无文件/函数行预算触发）；`gofmt -l` 干净。新增测试：`TestJourneySeverity` +4 子测试、`TestExtractEntitiesNear`、`TestDetectUnverifiedEntityReference` +1 子测试、`TestFormatDelta`、`TestRenderToolWasteTotals`、`TestRenderSessions_JourneyLink`、`TestRenderStoryIndexMarkdown_SelfTrafficLine`、`TestRenderHTML_DashboardStructure` +2 断言。

CHANGELOG `[Unreleased]` 已加第三次 honesty pass 条目。

### 5.5 未做（留待后续梯队，按原分组）

第二组剩余：8（全失败 journey 报"未检测到"）、9（journey md 补指标节）、12（总成本数字）、13（Task 汇总行）、14（详单 tool_call args 转义 JSON）、15（Finding 按 Code 分组 —— 与问题 11 强相关，建议紧接着做）、16（补主力 provider 定价）、18（corpus 三处自我限定的 a/b 子项）。
第三组：21–23、26、28、30、33、35 的 §6.6 排序子项。
第四组维持不做。

---

## 第六部分 · 源码级二轮核查与未闭环/不匹配项清单（2026-08-31）

> 基于第一性原理与当前 HEAD 源码，对第四章所有标注"已处理""基本处理""部分处理"的事项逐条做代码级实测核验。第一轮核查（下方 6.1）提出 5 项疑点；第二轮（Sonnet 5，同日）逐项回源码验证真伪，成立的当场修掉并标注，不成立的说明理由。

### 6.1 五项疑点的核实与处置

#### 1. `finishCell` 遗漏未分类失败 → 分组文件里失败轮次显示为 `-` ✅ 已处理（2026-08-31 · Sonnet 5）

- **核实结论：成立。** `internal/report/requests.go` 的 `finishCell`（`vmr-requests-<tag>.md` / `-cron-<tag>.md` 的逐轮表、Scheduled 表都走它）旧逻辑是 `if r.Outcome != "ok" && r.ErrorClass != ""` 才输出 `❌<class>`。而 `Outcome == "error"` 且 `ErrorClass == ""` 的记录是真实存在的一类：`server.go` 的 `chatHandler` 在路由前就拒掉的请求——body 读失败 / body 超限 / 非法 JSON / 缺 `model` 字段——都走 `OutcomeFor(4xx, false) == "error"`，且 `rec.Attempts` 为空，`endpointInfo` 因 `len(Attempts) == 0` 返回 `errClass == ""`。这些记录带合法 `client_key_tag`，会落进对应 tag 的会话卡片，逐轮表里 `finishCell` 命中 `orDash(r.Finish)` 渲染成一个无害的 `-`。同类还有"在并发槽队列里等待时被客户端取消"（`Outcome == "canceled"`、无 attempt、无 class）。姊妹文件 `vmr-requests-failed.md` 走的 `outcomeCell` 已经有 `ec == "" → "unclassified"` 兜底——两个渲染器口径不一致。
- **处置**：`finishCell` 错误分支对齐 `outcomeCell`：`r.Outcome != "ok"` 即输出 `❌<class 或 unclassified>`，truncated / 正常 finish 逻辑不变。新增 `internal/report/requests_test.go:TestFinishCell_UnclassifiedFailureIsNotABareDash` 覆盖 error/canceled/classified/truncated/正常 五种输入。既有 `TestWriteFailedIndex`（断言 `❌transient` / `❌canceled` / `⚠️trunc` 出现在 per-group sibling 里）仍绿。
- **回到问题 35**：该子项此前标注 "⚠️ 文案/文档子项已处理，§6.6 排序子项待做"——`finishCell` 缺陷不在问题 35 已列的琐碎项清单里，属核查新发现，现已随本轮修掉。问题 35 剩余未做项仅 §6.6 端点性价比排序键一条，标注不变。

#### 2. 宏观报表附录：排除已配置但排除 0 条时误报"未启用" ✅ 已处理（2026-08-31 · Sonnet 5）

- **核实结论：成立，是一句肯定性的错误事实陈述。** `render_doc.go` 的 `renderAppendix` 旧逻辑 `if rep.Meta.SelfTrafficExcluded > 0` 才输出"已排除 N 条"，else 一律输出 `AppendixSelfTrafficNotExcluded`——"未启用排除（未配置 `llm_key` 或 `self_traffic_client_tags`）"。而 `SelfTrafficExcluded` 只在 `aggregate.go` 里"某条记录真的被匹配丢弃"时才 `++`。于是"用户在 `report.yaml` 配了 `llm_key`、但当前分析窗口恰好没有自指流量"这一常见情形（比如那次运行没开 LLM 解读层），附录会打出"未配置"——已配置却报未配置。且同一套产物里 `vmr-stories.md` 侧（问题 4 story 半区收尾时新增的 `SelfTrafficStatus{Active, Excluded}`）会正确报"已启用（排除 0 条）"，两份产物自相矛盾。
- **处置**：`Report2.Meta` 加 `SelfTrafficExclusionActive bool`，`buildInternal` 里置为 `len(excludeClientTags) > 0`；附录改按该布尔分支——`Active` 即输出"排除已启用；本次排除 N 条"（N 可为 0），仅 `!Active` 才输出"未配置"。`i18n/report_doc.go` 的 `AppendixSelfTrafficExcluded` 文案调整为在 N=0 时也读得通。新增 `TestSelfTrafficExclusion_ConfiguredButNothingMatched`。
- **回到问题 4**：问题 4 标注 "✅ 已处理"，但其正文"已落地部分"只说了 render_doc.go 改成"无条件渲染 + else 分支"——那个 else 分支正是这里的 bug 来源（`Excluded == 0` 落进 else）。问题 4 的完成态描述需更正：报告侧的口径行现在按"排除是否启用"分支，而非按"排除条数是否 > 0"。已在问题 4 正文补一句。

#### 3. `-list-only` 下 `Tasks` / `Rendered` 两列整列 `—` ✅ 已处理（2026-08-31 · Sonnet 5）

- **核实结论：成立（可读性问题，非正确性）。** `-list-only` / 裸 `vmr story` 不构建 Journey，`BuildJourneyIndexRow` 返回的行 `Tasks == 0`、`Rendered == ""`，`writeStoryIndexRow` 里 `Rendered` 列渲成 `—`、`Tasks` 列（`r.Tasks > 0 || r.Steps > 0` 为假）也渲成 `—`。`Steps` 列有 `r.Requests` 兜底，不是 `—`。所以准确说是 `Tasks` + `Rendered` 两列全 `—`，读者容易读成字段损坏。`—` 本身是准确信息（确实没渲染），缺的是一句说明。
- **处置**：`RenderStoryIndexMarkdown` 在渲染表格前扫一遍——若没有任何行带 `Rendered` 或 `Tasks`（即本次没渲染、也没有上一轮 `MergeJourneyIndexRows` 带过来的值），在表格上方加一行说明"本次运行未渲染任何 Journey，`任务`/`已渲染` 两列留空，`轮数` 显示请求数，用 `-render-all` 或 `-journey <id>` 渲染后填充"。`i18n/story_index.go` 加 `ListOnlyNote`，中英各一。不做自适应删列——会改表结构、动下游解析预期，收益不抵成本。新增 `TestRenderStoryIndexMarkdown_ListOnlyNote`。
- **回到问题 31**：问题 31 标注 "⚠️ 部分处理"，正文列的"改法"含"`-list-only` 下不输出这两列，或表头加一句说明"——本轮采纳后者。`(unresolved)` client、`⚠` 图例两个子项此前已处理。问题 31 现可从 "⚠️ 部分处理" 收敛为 "✅ 已处理"（空列已加说明）。已更新标注。

#### 4. `lowConfidenceFindings` 清单目前只含 1 个检测器 —— 事实成立，但不据此扩清单

- **核实结论：事实部分成立，据此扩清单的建议不采纳。** `severity.go` 的 `lowConfidenceFindings` 确实只有 `FindingUnverifiedEntityReference` 一项。疑点主张把 `unused_tool_result`（语料 58% 命中）也纳入，否则它仍可能当 HTML 看板主因头条。但两者不同质：
  - `unverified_entity_reference` 有**明确的误报机理**——它把 Go 标准库类型、项目自己在跑的端点判成"证伪"，检测器自注释都承认是"a suspicious signal ... not a confirmed hallucination"。这是把它降级的依据。
  - `unused_tool_result` 有一段**收紧校准史**（`findings_toolresult.go` 注释）：早期版本按 entity 触发、每 journey ~40 条，现版本只在"整个工具结果的实体后续一个都没再引用"时才触发，注释称这是"the original, meaningfully rare signal"。没有已知的系统性误报机理。
  - 它的 journey 级对应物 `Metrics.ContextUtilization` 是别处的头条指标。58% 的命中率更可能反映"上下文膨胀型 workload 里 agent 确实经常整块丢弃工具结果"这一真实低效，而不是检测器噪声。把它塞进 `lowConfidenceFindings` 会让一个本该可行动的 driver 永远无法成为 verdict 头条。
- **处置**：代码不动。要不要纳入分级，前提是先对 `unused_tool_result` 的命中样本做一次人工采样误报审计（就像问题 11 对 `unverified_entity_reference` 做的那样），有证据再谈。列入第七部分 P4"需证据方可动"（原 P5，2026-09-01 重排后顺延）。
- **回到问题 2**：问题 2 标注 "✅ 已处理（driver 优选）"，6.2 表里给的是 "⚠️ 逻辑正确但范围局限"——后者准确。问题 2 的 D3（全低可信度时 verdict 降级渲染）本就记为"留待后续"，清单扩容与 D3 一并归入第七部分 P1/P4（原 P2/P5，重排后顺延），问题 2 主标注不变。

#### 5. §6.7 Compaction 表 `0 → 0` 无信息行 —— 主张不完全成立，按实际情况处置

- **核实结论：疑点描述与语料事实不符，提议的过滤会丢信息，只采纳其中一条诚实性修正。** 疑点说这些行"会话元数据全为 `-`"并提议过滤掉 `TokensIn == 0 && TokensOut == 0` 的行。但第三部分 R1-7 记录的那个逐字重复 3 次的行是 `l-77c20384 | - | 0 → 0 | - | ~/ENV.md, TOOLS.md, ...(+27 more)`——它**带 `Summarizes` 链接、带 swallowed entities**，只有 token 数是 0。`buildCompactions` 里 `TokensIn/TokensOut` 仅在 `c.UsageOK` 时才填，`0 → 0` 的真实含义是"这次调用的 usage 没解析出来"，不是"测得为零"。按疑点的建议过滤，会把这条携带缝合关系 + 实体丢失信息的行一并删掉。而"重复 3 次"是 3 条独立的 compaction 审计记录（同一前驱、连续重试），逐条列出在排查 compaction 抖动时是有用的，不该去重。
  - R1-7 的主症状——保留比 >100% 且其余列全空的行——是**另一批行**（`246 → 1.1K = 431%`，`TokensIn > 0`），问题 27 加的脚注针对的正是那批，已处理。
- **处置**：不加行过滤、不去重。只做一条诚实性修正：`section_compaction.go` 里当 `TokensIn == 0 && TokensOut == 0`（usage 未解析），size 列渲成 `-` 而非字面 `0 → 0`——后者读起来像"测得的空操作"。`retentionRatio` 早已对 `in <= 0` 返回 `-`。新增 `TestRenderCompactionsZeroUsageIsNotAMeasuredZero`。
- **回到问题 27**：问题 27 标注 "✅ 脚注已处理"，6.2 表给的 "⚠️ 脚注属实，有优化空间" 准确。本轮把"优化空间"里可安全落地的一条（`0 → 0` → `-`）做掉，去噪过滤那条明确判为不做（会丢信息）。已在问题 27 正文注明。

### 6.2 第四章已标注项的源码核实总表

| 编号 | 事项 | 文档标注 | 源码核实结论 | 事实细节与源码位置 |
| :--- | :--- | :---: | :---: | :--- |
| **1** | `%%` 转义 + 全角括号 | ✅ 已处理 | ✅ 完全属实 | `i18n/report_doc.go` 为 `90%`；`SummaryRequests` 在 zh/en 下分别处理括号。 |
| **2** | 主因判定头条误报驱动 | ✅ 已处理 | ⚠️ 逻辑正确；清单范围是判断项 | `severity.go:lowConfidenceFindings` 实现 driver 优选；清单目前 1 项，是否扩容见 6.1 #4（需采样证据）。 |
| **3** | 工具浪费四总量入 §7 | ✅ 已处理 | ✅ 完全属实 | `section_efficiency.go:renderToolWasteTotals` 汇总全局并输出 top-line；§0 highlight 改按绝对浪费量。 |
| **4** | self-traffic 口径留痕 | ✅ 已处理 | ✅ 属实（本轮补一处缺陷） | Story 侧正确；**Report 侧"配置了但 0 条匹配"曾误报"未启用"，本轮加 `SelfTrafficExclusionActive` 修掉**。 |
| **5** | §6 会话表 Journey 链接 | ✅ 已处理 | ✅ 完全属实 | `loadStoriesLink` → `report.Markdown` 传 `journeyLink`，140+ 会话行链到 `stories/`。 |
| **6** | compare 相对变化量级化 | ✅ 已处理 | ✅ 完全属实 | `formatDelta` 输出 `155×`/`0.02×`/`+40%`/`新增`/`-100%`/`—`，MD/HTML 双端生效。 |
| **7** | `-llm-addr ""` 关闭配置 | ✅ 已处理 | ✅ 完全属实 | `cmd_analyze.go` 与 `cmd_story.go` 守卫判定改为解析值非空，带回归测试。 |
| **10** | HTML 看板认知谦逊 | ✅ 基本处理 | ✅ 完全属实 | Findings 段输出 `<blockquote class="coverage-note">`，文案已软化，D4 按计划延后。 |
| **11** | 未验证实体引用误报校准 | ✅ 已处理 | ✅ 完全属实 | `chatmsg.ExtractEntitiesNear` 160 字节窗口生效，语料 Finding 率 -19% 且无 collateral。 |
| **17** | §3 健康表低样本门槛 | ✅ 已处理 | ✅ 完全属实 | `section_reliability.go` 的 `Attempts < 20` 统一输出 `⚠️low-n`，与 §4 口径对齐。 |
| **18** | Corpus 统计自我限定 (c) | ⚠️ 脚注已处理 | ✅ 完全属实 | `story_corpus.go` 脚注已补平稳无趋势说明；(a)(b) 按计划留待后续。 |
| **19** | `hartbeat` 拼写统一 | ✅ 已处理 | ✅ 完全属实 | `cronFileTag` 移除特殊分支，UserGuide 双语同步为 `vmr-requests-cron-heartbeat.md`。 |
| **20** | §2 日成本表补齐缺失行 | ✅ 已处理 | ✅ 完全属实 | `section_cost.go` 遍历全部日期，未定价日填 `-` 并保留表尾说明。 |
| **24** | Journey 跨日补日期 | ✅ 已处理 | ✅ 完全属实 | `render_md.go` 在 `from` 与 `to` 跨日时切换为完整 `2006-01-02 15:04:05` 格式。 |
| **25** | 44 输入文件路径折叠 | ✅ 已处理 | ✅ 完全属实 | 正文已折叠进 `<details>`，附录保留单行输入清单。 |
| **27** | Compaction >100% 解释 | ✅ 脚注已处理 | ✅ 脚注属实（本轮补 `0→0` 渲染） | 脚注针对 >100% 行；`0 → 0`（usage 未解析）本轮改渲 `-`；去噪过滤判为不做（丢信息）。 |
| **29** | 术语首次出现处解释 | ✅ 已处理 | ✅ 完全属实 | ⭐、msgs、finish、⚠、sNN 均已在首次出现的表格下方添加图例。 |
| **31** | `-list-only` 下图例与字段 | ⚠️ 部分处理 → ✅ 已处理 | ✅ 完全属实 | `(unresolved)` 与 `⚠` 图例此前已落地；本轮 `RenderStoryIndexMarkdown` 加 `ListOnlyNote` 说明空列。 |
| **32** | `sNNN` 序号 run-scoped 说明 | ✅ 已处理 | ✅ 完全属实 | §6 表下已添加 `TableNote` 说明其为本地别名，真实 ID 为 `l-...`。 |
| **34** | `vmr.sh` stale-binary 防呆 | ✅ 已处理 | ✅ 完全属实 | `vmr.sh:warn_if_stale` 同时对比源码 mtime 与 `git rev-parse HEAD`。 |
| **35** | 琐碎项批处理 | ⚠️ 文案已处理 | ✅ 属实（本轮补 `finishCell`；2026-09-01 三轮核查后 §6.6 排序键子项注销，问题 35 收敛为 ✅） | 文案/文档子项已处理；`finishCell` 未分类失败渲成 `-` 属核查新发现，本轮修掉；§6.6 排序键子项与现行代码不符（无定价块内已按 `wastedMS` 降序），注销。 |
| **36** | Requests 纯索引化 | ✅ 已解决 | ✅ 完全属实 | `writeAllRequestsFooter` 已在 `c13819a` 中完全移除。 |
| **39** | 上下文利用率双峰说明 | ✅ 记账处理 | ✅ 完全属实 | `KNOWN_ISSUES` 已登记其双峰退化及统计解读建议。 |

### 6.3 本轮测试与门禁

`go build ./...` OK；`go test ./...` 全绿；`go test ./internal/archtest/...` 全绿（无文件/函数行预算触发，`aggregate.go` 598/600、`requests.go` 678/700）；`gofmt -l` 干净。新增测试 4 个：`TestFinishCell_UnclassifiedFailureIsNotABareDash`、`TestSelfTrafficExclusion_ConfiguredButNothingMatched`、`TestRenderStoryIndexMarkdown_ListOnlyNote`、`TestRenderCompactionsZeroUsageIsNotAMeasuredZero`。CHANGELOG `[Unreleased]` 已加"source-level second-pass fixes"条目。

---

## 第七部分 · 遗留问题汇总与落地记录（第四轮核查与落地完成，2026-09-01）

> 本部分记录第四部分和第七部分中 P1~P4 各项低成本/低风险任务的完整落地结果。

### 7.1 本轮落地汇总表

| 批次 | 问题编号 | 事项说明 | 落地文件与实现要点 | 状态 |
| :--- | :---: | :--- | :--- | :---: |
| **P1** | **15** | Findings 清单按 Code 分组 + 计数 + 排序 | `render_spine.go`: `renderFindingsSection` 按 Code 分组，按 `findingTrustTier`（严重故障优先，低可信度沉底）及最早 Step 排序输出小标题与计数；`story_spine.go` 补 `FindingGroupTitle` 双语 | ✅ 已落地 |
| **P1** | **2 D3** | 全低可信度 Finding 时 verdict 降级渲染 | `severity.go`: `JourneySeverity` 返回 `driverLowConf`；`render_html.go` / `i18n/story_html.go` 将低可信度驱动的 verdict 降级渲染为「仅次级信号（code）——不作结论」 | ✅ 已落地 |
| **P1** | **8** | 全失败 Journey 漏报问题 | `journey.go` / `journey_stepfacts.go`: `Step` 结构添加 `Outcome` 与 `ErrorClass`；`render_spine.go`: `stepRoleTag` 纳入 `Outcome == "error"` 判定为 `🔷 ⚠️`；概览卡片输出失败请求数 | ✅ 已落地 |
| **P1** | **9** | `journey-*.md` 补行为指标节与演化趋势 | 新增 `render_indicators.go` / `i18n/story_indicators.go`，在 Markdown 概览下渲染 14 项完整行为指标表及 8 级点阵 ASCII 上下文演化趋势图（`Context Token Trajectory`） | ✅ 已落地 |
| **P2** | **13** | Task 标题行补充汇总行 | `requests.go`: `renderSessionCard` 在 `### tNN` 下输出一行汇总（轮数、总耗时、fresh/cached/out tokens、结束状态）；`i18n/report_requests.go` 补 `TaskSummary` | ✅ 已落地 |
| **P2** | **14** | 详单页 tool_call args HTML 转义与排版 | `internal/reqdetail/render.go`: `jsonIndent` 采用 `json.NewEncoder` + `SetEscapeHTML(false)`，修复 `<, >, &` 被转义为 `\u003c, \u003e, \u0026` 的问题 | ✅ 已落地 |
| **P2** | **23** | §6 会话表增加「时间范围」列 | `section_sessions.go`: 新增 `formatSessionTimeRange` 并在表格中增加时间范围列；`i18n/report_sessions.go` 表头扩展为 7 列（中英双语） | ✅ 已落地 |
| **P2** | **26** | §3/§4 端点表 low-n 噪声折叠 | `section_reliability.go` (§3) 与 `section_latency.go` (§4) 对 `Attempts < 20` 或 `Requests < 20` 的低样本行拆分折叠进 `<details><summary>+ 另有 N 个低样本端点</summary>` | ✅ 已落地 |
| **P2** | **28** | 失败请求聚类摘要 | `requests_failed.go`: 新增 `clusterFailedRequests`，按时间间隔 ≤ 2 分钟聚合故障簇并在 `vmr-requests-failed.md` 头部输出聚类摘要 | ✅ 已落地 |
| **P2** | **30** | 逐日活跃度图表数据表格兜底 | `section_workload.go`: 在 Mermaid 逐日活跃度图下方添加折叠的数据明细表（`<details><summary>+ 逐日活跃度明细表</summary>`），防止纯文本/非渲染环境丢失数据 | ✅ 已落地 |
| **P2** | **18 (a)(b)** | Corpus 恒等式剔除与 N-gram 自重复过滤 | `corpus.go`: 过滤 `mechanicalCorrelationPairs`（数学/定义恒等关系）使 Top-15 突出经验相关；`corpus_sequence.go`: 增加 `isSelfRepeat` 排除 `exec → exec` 纯自重复 | ✅ 已落地 |
| **P2** | **40** | ByClient 与分组文件对账说明 | `render_doc.go`: 附录中对账 `rep.ByClient` 与 sibling 文件集合，输出未独立建文件的单发定时 client 提示 | ✅ 已落地 |
| **P3** | **22** | Compare 报告头部摘要卡 | `render_compare.go`: 新增 `renderComparisonSummaryCard`，提取 Top-3 显著差异、分叉点、端点一致性、终止方式四项要点 | ✅ 已落地 |
| **P3** | **21** | Compare 两侧 System Prompt unified diff | `render_compare.go`: 新增 `simpleLineDiff`，两侧 prompt 存在差异时输出紧凑的代码 diff 块而非铺满两份全文 | ✅ 已落地 |
| **P4** | **10 D4** | HTML 看板错误恢复在协议盲区下显示 `n/a` | `render_html.go` / `render_html_dashboard.go`: 非 Anthropic Messages 协议下错误恢复次数为 0 时渲染为 `n/a` | ✅ 已落地 |
| **P4** | **43 ①** | 时序图添加横轴刻度 | `render_spine.go`: `renderToolTimeline` 在 steps ≥ 20 时自动输出标尺刻度行 | ✅ 已落地 |
| **P4** | **43 ②** | 模型切换记录显示缓存命中率影响 | `modelusage.go` / `render_modelusage.go`: 记录切换前后的缓存命中率并展示 `[缓存命中率 A% → B%]` | ✅ 已落地 |

---

### 7.2 剩余事项与长期跟踪（不阻塞发布）

1. **6.1 #4**：`unused_tool_result` 等粗筛检测器是否纳入 `lowConfidenceFindings` —— 需先进行人工采样审计，有证据再动。
2. **问题 37**：跨时间窗对比（`-compare-period`）—— 需独立设计，属分析半区后续大版本演进（已登记 `KNOWN_ISSUES` §2.60）。
3. **问题 38**：任务达成信号 —— 路由器层面无法确定任务语义结果（已登记 `KNOWN_ISSUES` §2.61）。
4. **问题 41**：Anthropic Messages 专属协议语料验证 —— 需准备专项测试集。
5. **问题 42**：超大 Journey 文件（>1MB）拆分 —— 等实际场景出现性能瓶颈再按需拆分。

---

### 7.3 一句话总结

本轮完成了 P1~P4 中全部 17 项可执行优化与修复任务（涵盖正确性治理、呈现层欠账补齐、长表折叠去噪、Compare 摘要与 Diff 重构、Corpus 自我限定收尾等），测试与架构门禁 100% 通过，分析半区体验与可信度达到完整状态。

