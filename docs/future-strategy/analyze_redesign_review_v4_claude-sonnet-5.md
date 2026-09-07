// Ver 2026-09-07, by Claude (claude-sonnet-5)

# vmr analyze 架构重构方案 —— 第四轮独立落地评审

**评审基准**：`docs/future-strategy/analyze_architecture_redesign_opus-5.md`（下称"方案"），裁决 D1–D21 + 正文 §1–§11 + Phase 1–4 路线图 + §9 守卫清单 + §11 不变量。
**问题来源**：方案本体 + 3 份历史 review（gemini-3.8-flash / minimax-m3 v2 / claude-sonnet-5，含其第 4–10 轮迭代）提出的全部 T/F/N/G/C 编号事项。
**实施范围**：`050ad25`（`refactor: rename internal/story to internal/journey`，方案实施起点）至 `HEAD = 69aa242`，共 90 个 commit，视作一次叠加变更逐项核实。
**评审方法**：
- 以方案为唯一基准，对每一条事项**回到源码与测试断言取证**，不采信任何文档（含 3 份历史 review）的"已完成/已实现"声称。
- `_review/` 下的执行报告、`verification_report_d*` 等其他 review 记录**一概不读**，独立判断。
- 端到端冒烟：`./vmr analyze` against 真实日志，产物拓扑/权限逐一比对方案 §4。
- 行为性验证（非只读）：L2 命中、orphan 清扫、`-render-only` 语言继承、看板渲染实跑。
**执行纪律**：事实清楚、方案无争议、有十分把握 → 当场修 + 留 commit 记录（直接提交 main）；复杂/争议 → 记入"待裁决"，不动手。
**基线**：`go build ./...` / `go vet ./...` / `go test ./...` 评审开始时全绿（38 包）。`gofmt -l` 仅 `cmd/vmr/cmd_journey_setup.go`（Go 1.26 尾随注释对齐；`go.mod` 声明 1.25.1，CI 用 1.25，clean）。

---

## 第一部分：Action Plan（待核实事项全集）

> 本清单从方案 + 3 份历史 review 提炼，**只列"应做什么"与"曾被点名的问题"**，不含任何完成度声称。执行结论见第二部分。
> 标记规则（第二部分回填）：✅ 已全部完成 ｜ 🟡 部分完成 ｜ ❌ 未完成 ｜ ✔️fixed 本轮直接修复 ｜ ⚖️ 待裁决。

### A 组 —— 概念归一 / CLI / 目录拓扑 / 兼容边界（方案 §2 §4 §8）

| ID | 事项 | 方案依据 |
|---|---|---|
| A-01 | `internal/story` → `internal/journey` 包与全部代码符号更名，`story` 一词从代码/CLI/产物清干净 | §2.1 |
| A-02 | `corpus` → `Journey Benchmarks`：文件 `journeys/benchmarks.{json,md}`，flag `-corpus`→`-benchmark`（无别名） | §2.2 |
| A-03 | CLI 收敛：`vmr report`/`vmr story` 子命令别名删除；`-story-only`→`-journey-only`（无别名）；单入口 `vmr analyze` | §2 §8.1 §10 |
| A-04 | 目录拓扑归位：`stories/`→`journeys/`，`vmr-stories.*`→`journeys/index.*`，单任务进 `journeys/details/`，对比提至根级 `compares/` | §3.5 §4 |
| A-05 | `.parse-cache/` → `.cache/parse/`；LLM 缓存 `.cache/llm/` 定为文档推荐值（无隐式默认，解析规则不改） | §3.5 §4 |
| A-06 | `details/`/`evidence/` 下沉为 `requests/details/`、`requests/evidence/`，与 `journeys/details/` 同构，维持懒物化 | §3.5 §4 |
| A-07 | 目录 0700 / 文件 0600 全链路（含骨架页、缓存目录） | §4 §11.1#3 |
| A-08 | `vmr-report.json` 单体一步删除，无兼容视图、无过渡别名（D2） | D2 §3.2 §8.1 |
| A-09 | `manifest.json` `format` 10 → 11；切片不带独立版本戳（整套一个版本单位） | §8.1 §6.6 |
| A-10 | `manifest.json` 三职责（准入一致性 / 溯源 / 发现），不承载任何指标数值 | §8.2 |
| A-11 | 破坏性变更一步到位：CHANGELOG 记一条 Breaking，所有仓内消费者（脚本/看板/测试）同一变更内同步更新 | §8.1 |
| A-12 | 双语文档同步：README/README.zh、UserGuide/.zh、config.example/.zh、CHANGELOG | §8.1 + CLAUDE.md |
| A-13 | v4 Analytics 设计文档按 current-state 改写（报告半区 §1–§3 消费侧）（历史 review T3） | 派生 |
| A-14 | 方案提案文档状态头更新为"已实施"，回写 D2 / `.cache/parse` 裁决结果（历史 review T4） | 派生 |

### B 组 —— 数据层：领域切片 + Schema 演进（方案 §3.1–§3.4）

| ID | 事项 | 方案依据 |
|---|---|---|
| B-01 | `Report2` 21 字段一步解构为五 macro 切片：`summary` / `finance` / `reliability` / `workloads` / `context-efficiency`，映射与 §3.2 表一致 | D1 D2 §3.2 |
| B-02 | 切片间禁止交叉引用具体数值（`summary.json` 的"总支出"独立落盘的标量） | §3.2 §11.1#5 |
| B-03 | §3.3#1 统计置信度下沉：`tokens_coverage_pct` / `dur_low_n` 由数据侧确定性填充，渲染侧只按值决定样式 | §3.3 |
| B-04 | §3.3#2 成本覆盖披露结构化：`CostCoverage{unpriced_count, incomplete_rate_count, degraded_estimate_pct}` | §3.3 |
| B-05 | §3.3#3 脚注与免责注册表：`meta.footnotes: map[marker]text` + `meta.disclaimers: []text` | §3.3 |
| B-06 | §3.3#4 首屏亮点句 `highlights` 进 `macro/summary.json`（带语言） | §3.3 §5.5 |
| B-07 | §3.3#5 会话/任务标题映射投影进 `requests/index.json`（只含分组与标题所需映射） | §3.3 |
| B-08 | §3.3#6 跨产物链接 `request → journey` 作为 `requests/index.json` 字段 | §3.3 |
| B-09 | §3.3#7 时间双字段：每个时间点落 `ts`（epoch ms）+ `ts_display`（`fmtutil.DisplayZone` 定稿） | §3.3 §5.6 §11.1#4 |
| B-10 | §3.4 `manifest.json` 最后写；读取方以 manifest 为准入，不逐切片降级采纳 | D20 §3.4 |
| B-11 | §3.4 准入两档：Go 侧完整校验（sha256 逐份匹配）；浏览器侧轻量校验（`format` 对齐，不做哈希校验） | §3.4 §6.6 |
| B-12 | §3.4 原子写 CreateTemp+Chmod 0600+Rename；多切片定义提交顺序 | §3.4 §1.4 |
| B-13 | manifest 盖章覆盖面 = 五 macro 切片 + `requests/index.json` + `journeys/index.json` + `journeys/benchmarks.json`；逐任务详情/`compares/` 子树在外 | D20 D21 §3.4 §8.2 |

### C 组 —— Journey JSON 自包含（方案 §3.6，D18）

| ID | 事项 | 方案依据 |
|---|---|---|
| C-01 | `journeys/details/j-<id>.json` 补成自包含：`structure` 是 tree + 同文件 `bodies` blob 表（按内容哈希去重） | D18 §3.6 |
| C-02 | 工具配对从 `Matched bool` 升为三级 `match`：`exact` / `normalized` / `positional` | D18 §3.6 |
| C-03 | Compaction 前驱摘录 `predecessor_excerpt_ref` 引用 bodies | §3.6 |
| C-04 | 截断口径数据层统一（tool args / result / compaction 摘录 3000）；`resp_ref`（RespText/Reasoning）**不设上限** | §3.6 |
| C-05 | `bodies` 无孤儿、无悬引用（每个 `*_ref` 可解析、每个 blob 至少被引用一次） | §3.6 §9 |
| C-06 | 若启用 `-llm-addr`：解读结果（模型/耗时/状态/正文）落入 `j-<id>.json` 的 `llm_interpretation`，彻底杜绝旁路拼接（历史 review N2/C5） | §3.6 |
| C-07 | 请求详单**不**照此自包含（O(N²) 物化，显式豁免） | §3.6 §5.0 |

### D 组 —— 请求索引删除 + compares 索引（方案 §3.7 §3.8，D7/D21）

| ID | 事项 | 方案依据 |
|---|---|---|
| D-01 | 人读请求索引整族删除：`vmr-requests.md` + `vmr-requests-<tag>.md` + `vmr-requests-cron-*.md` | D7 §3.7 |
| D-02 | `requests/index.json` 作为请求明细机读单一真源 | D7 §3.5 §3.7 |
| D-03 | `requests/failed.md` 保留为排障入口；`requests/failed.jsonl` 下沉 | D7 §3.5 |
| D-04 | 旧渲染死代码清理：D7 改行为后残留的 Markdown 渲染函数族应删除（历史 review N3） | §8.1 + CLAUDE.md |
| D-05 | `compares/index.{json,md}` 新增：扫 `compares/*.json` 现算，每次 analyze 重建 | D21 §3.8 |
| D-06 | `compares/` 整个子树不进 manifest 指纹 | D21 §3.8 |
| D-07 | 手删对比重跑即自愈，不留指向 404 的行 | §3.8 |
| D-08 | 不做"前端勾选两条 journey 直接跳对比页"；给可复制的 `vmr analyze -compare <a>,<b>` | §3.8 §6.2 |
| D-09 | `compares/index.md` 文案随 manifest 语言（历史 review N5） | D4 §5.5 |

### E 组 —— 文件名约定（方案 §1.1，D17/D19）

| ID | 事项 | 方案依据 |
|---|---|---|
| E-01 | 请求详单文件名加 `r-` 类型前缀（`reqdetail.FileName` 单点 + 包装函数透传） | D17 §1.1 |
| E-02 | Journey 文件名归一：去历史冗余 `journey-` 前缀（原 `journey-j-...`） | D19 §1.1 |
| E-03 | `-partial` 后缀取消：partial 只作 JSON 字段 + `.md` 顶部 banner + 索引行标记 | D19 §1.1 |
| E-04 | compare 文件名同理不带 `-partial` 后缀，partiality 经字段 + banner + 索引行 + 看板徽标表达 | D19（派生） |

### F 组 —— ViewModel 分层 + 单一渲染路径（方案 §5）

| ID | 事项 | 方案依据 |
|---|---|---|
| F-01 | 聚合类产物单一渲染路径：`聚合 → 写 JSON → 读 JSON → ViewModel → 序列化 → 写 Markdown`；全量运行与 `-render-only` 同一条代码路径 | D11 §5.0 |
| F-02 | 详单/证据显式豁免单一渲染路径（输入是原始审计字节，懒物化） | §5.0 §5.4 |
| F-03 | 不引模板引擎：无 `text/template`、无 `-templates`、无模板指纹；Markdown = VM 的确定性固定序列化器 | D3 §5.1 §5.3 |
| F-04 | 全部文案（含章节标题）进 ViewModel/i18n，渲染器只表达结构与顺序，天然双语 | D4 §5.2 |
| F-05 | `i18n/report_*.go` 配对对象从 `section_*.go` 改为 ViewModel 构建器（`viewmodel_*.go`），archtest 强制 | §5.2 §9 |
| F-06 | `-render-only` 覆盖全部常驻人读产物；`details/`/`evidence/` 永久排除 | D5 §5.4 |
| F-07 | `-render-only` 作业清单来自 `journeys/index.json`，不扫目录 | D20 §5.4 |
| F-08 | 每次 analyze 调用（含 `-render-only`）幂等刷新骨架页 | §5.4 §6.6 |
| F-09 | `-render-only` 继承 JSON 语言：`-lang` 与 manifest 不一致则报错并指引全量重跑 | D10 §5.5 |
| F-10 | ViewModel 不落盘：仅驻内存的 Markdown 专用层；前端直接消费领域切片 | D12 §5.6 |
| F-11 | 时间不给前端算：切片落 `ts` + `ts_display`，前端只显示后者、只用前者排序 | §5.6 §11.1#4 |
| F-12 | 数值格式化跨语言 fixture `testdata/fmt_cases.json`：Go 侧 `fmtutil` + JS 侧纯函数各跑一遍 | §5.6 §9 |
| F-13 | `journeys/details/j-<id>.md` 的 `RenderMarkdown` 从吃内存 `*Journey` 改吃 `JourneySummary`（依赖 C 组自包含） | §5.0 §5.4 |
| F-14 | golden 测试下沉到 ViewModel 层（比对 VM 结构而非最终字符串） | §9 |
| F-15 | `LosslessReconstruction` 改写为真自包含断言（只给 `j-<id>.json` → `.md` 逐字节渲染） | §9 |

### G 组 —— HTML 看板（方案 §6）

| ID | 事项 | 方案依据 |
|---|---|---|
| G-01 | `analyze` 只写 JSON，无 HTML 渲染开销；看板是常驻静态骨架，浏览器 fetch 切片渲染 | §6.1 |
| G-02 | 六个看板页：`macro-dashboard` / `request-browser` / `journey-viewer` / `journey-compare` / `benchmarks` / `tool-waste` | §6.2 |
| G-03 | `go:embed` 骨架 + 主题变量系统 + 零依赖内联 SVG（折线/柱状/热力/散点四形态） | §6.1 §6.3 |
| G-04 | `#data=` hash 传参（不用 `?query`）；磁盘布局 = HTTP 路径逐字一致 | D13 §6.2 |
| G-05 | 骨架页根级平铺；`#data=` 值是相对骨架页自身目录的路径 | D13 §6.2 |
| G-06 | 缺省行为：无 hash 参数按约定探测默认索引；索引不存在展示空状态+生成引导，不阻断 | §6.2 |
| G-07 | `journey-viewer` 候选列表：勾选两条 journey 给可复制的 `vmr analyze -compare <a>,<b>` 命令行，不直接跳转 | §6.2 §3.8 |
| G-08 | `file://` 直开不支持：fetch 失败且 `location.protocol === 'file:'` 时显示提示，不做静默空白页 | §6.2 |
| G-09 | 自包含 HTML 三处渲染器（`render_html*` / `render_compare_html` / `toolwaste_html`）+ `story/assets/` 废弃 | D6 §6.4 |
| G-10 | `-redact` 删除（flag + Go 渲染器 `redact bool` 分支 + 双语文档） | D15 §6.4 |
| G-11 | `-html` flag 删除（自包含 HTML 生成开关随渲染器一并废弃） | D6 §1.1 |
| G-12 | `/reports/` HTTP 托管：显式开关 `analytics.serve`（默认关，不开不挂路由） | D9 §6.5 |
| G-13 | 无 `api_keys` 时 `/reports/*` 一律 403（不复用会放行的通用 auth 包装器），启动日志说明原因 | D9 §6.5 |
| G-14 | 路径校验：`filepath.Clean` + 输出目录前缀校验 + 拒绝符号链接 + 禁用目录列表 | D9 §6.5 |
| G-15 | 分层鉴权：HTML 骨架免鉴权直出；`.json`/`.jsonl`/`.md` 走鉴权；key 从 localStorage 取，不进 URL/日志 | D9 §6.5 |
| G-16 | `internal/server` 只按路径读文件目录，不 import 任何分析半区的包（archtest 强制） | §6.5 §11.1#1 |
| G-17 | `analytics.serve_dir` 默认 `./reports`（与 `analyze -o` 同默认值），可覆盖，不走 `rundir` | D16 §6.5 |
| G-18 | 目录不存在不是启动错误：`/reports/*` 一律 404 + 一次性日志提示 | D16 §6.5 |
| G-19 | 版本探测 banner：每个骨架页启动 `fetch('manifest.json')` 比对 `format` 与内置期望常量；三分支（一致/不一致警告不阻断/404 提示） | D14 §6.6 |
| G-20 | 版本探测逻辑写成纯函数（期望 × 实际 → 行为），随看板 JS Node 测试覆盖 | §6.6 §9 |
| G-21 | 版本戳不进切片；页面查 manifest 与"读取方以 manifest 为准入"同构 | §6.6 |
| G-22 | 零编译扩展：复制骨架页、改 JS、指向特定切片即可定制；副本随复制继承版本探测逻辑 | §6.2 §6.6 |
| G-23 | 看板 JS 按切片真实 json tag（snake_case）读数据，不按 Go 结构体字段名（PascalCase）（历史 review N15） | 派生 |
| G-24 | `common.js` 实际随骨架页部署可用（内联或独立 serve）（历史 review N1） | §6.3 |
| G-25 | 骨架页 chrome 本地化策略一致（统一英文或随 manifest.lang）（历史 review N10） | §6.2（派生） |
| G-26 | 看板金额显示随 `-currency`（切片金额已是 Go 侧换算值，前端补符号映射）（历史 review F12） | 派生 |

### H 组 —— 缓存层（方案 §7）

| ID | 事项 | 方案依据 |
|---|---|---|
| H-01 | 全系统唯一 Digest 构造：长度前缀（uvarint）的有序 sha256 链，三性质（顺序敏感/无歧义拼接/重复不抵消） | D8 §7.2 |
| H-02 | 分量取原始字节；标量定宽编码（`int64` BigEndian、`float64` `math.Float64bits`），不用 `fmt.Sprintf` | §7.2 |
| H-03 | 输入哈希直接算内容（`ctxgraph.HashFile` sha256），无 mtime fast path | §7.2 §11.1#2 |
| H-04 | `ctxgraph` md5 内容寻址底座不动，作为字节分量喂进 sha256 链 | §7.2 §11.2 |
| H-05 | Digest 构造是否"只有一个"—— report/journey 双实现是否收敛为共享叶子包（历史 review T2） | D8 |
| H-06 | L1 输入解析缓存归位（`.cache/parse/`），依赖审计文件 sha256 + 解析器版本 | §7.1 |
| H-07 | L2 产物数据缓存新增：`Digest(输入哈希集 ‖ 配置指纹 ‖ 格式版本 ‖ 分析参数)`，整套产物有效则跳过聚合与叙事构建 | §7.1 §7.2 |
| H-08 | L3 表现层缓存新增：`Digest(ViewModel 指纹 ‖ 渲染器版本 ‖ 语言)` | §7.1 §7.2 |
| H-09 | 配置指纹只含影响金额部分（provider `pricing.rates` 覆盖 + 顶层 `exchange_rate` + 标准表 `GeneratedAt`），不是整个 config 哈希（历史 review N11） | §7.2 |
| H-10 | 分析参数指纹含改变取样口径的全部 flag（`-lang`、taskseg profile、自流量排除集、LLM identity 等）；判据"改了会不会改变落盘数值或人读文本" | §7.2 |
| H-11 | 缓存粒度整套一个指纹，无切片级隔离；`-no-cache` 常驻旁路（非过渡开关） | D1 §7.3 §8.3 |
| H-12 | §7.4 失效矩阵七行逐行成立（尤其"二进制版本变化不失效"） | §7.4 |
| H-13 | 详单指纹携带 `(record, manifest, prev)` 身份 + 渲染器版本 + 语言 | §7.2 §1.4 §9 |
| H-14 | L2 命中的全量运行仍执行 orphan 清扫 + compares 索引重建（历史 review F1/minimax） | 派生 §3.4 |
| H-15 | 加/改 `-llm-key` / `-llm-addr` 不静默命中旧 L2 缓存（历史 review F3） | §7.2 |

### I 组 —— 测试守卫与横切不变量（方案 §9 §11）

| ID | 事项 | 方案依据 |
|---|---|---|
| I-01 | `internal/story/golden_test.go` 下沉到 ViewModel 层 | §9 |
| I-02 | `structure_test.go` 的 `LosslessReconstruction` 改写为真自包含断言 | §9 |
| I-03 | `internal/report/e2e_test.go` 保留 + 迁移期"切片并集 ≡ 旧 `Report2` 字段集"等价断言 | §9 |
| I-04 | 详单跨宏观报告/Journey 渲染字节一致 + 指纹携带渲染器版本 | §9 |
| I-05 | `i18n_e2e_test.go` 保留 + 新增"ViewModel 无未 i18n 裸字面量"守卫（历史 review T5） | §9 |
| I-06 | archtest 行数预算登记 `viewmodel_*.go`；`section_*.go` 条目随删除移除 | §9 |
| I-07 | archtest i18n 配对对象改 ViewModel 构建器 | §9 |
| I-08 | archtest 新增：`internal/server` 不 import 分析半区任何包 | §9 §11.1#1 |
| I-09 | 新增守卫：`-render-only` 产物与全量运行字节一致 | §9 |
| I-10 | 新增守卫：缓存命中路径与冷启动路径产物一致（浮点 `1e-6` 容差） | §9 |
| I-11 | 新增守卫：跨语言格式化 fixture 由 `fmtutil` 与看板 JS 各跑一遍 | §9 |
| I-12 | 新增守卫：切片内每个时间点必须同时有 `ts` 与 `ts_display`，缺一即失败 | §9 §5.6 |
| I-13 | 新增守卫：骨架页版本探测纯函数测试 | §9 §6.6 |
| I-14 | 新增守卫：`bodies` 表无孤儿、无悬引用（双向查） | §9 §3.6 |
| I-15 | 新增守卫：工具配对三级 `match` 与决策脊柱实际渲染一致 | §9 §3.6 |
| I-16 | 新增守卫：更窄时间窗重跑，`journeys/details/` 无上一次孤儿文件残留 | §9 D20 |
| I-17 | 新增守卫：orphan 清扫不动 `compares/`、`requests/details|evidence/` | §9 D20 |
| I-18 | 新增守卫：`compares/index.json` ≡ `compares/*.json` 目录内容 | §9 D21 |
| I-19 | 不变量 1：两半区一条契约（`report`/`journey`/`ctxgraph`/`taskseg`/`chatmsg`/`reqdetail` 不 import `router`/`server`/`config`；不建跨包共享胖 ViewModel） | §11.1#1 |
| I-20 | 不变量 2：内容寻址底座不可动摇（缓存判据只能是内容哈希，绝不回退文件时间启发式） | §11.1#2 |
| I-21 | 不变量 3：权限底线 0600/0700 | §11.1#3 |
| I-22 | 不变量 4：单一时区权威（`ts` + `ts_display` 双字段，前端不做二次换算） | §11.1#4 |
| I-23 | 不变量 5：不新增双账本（manifest 不放指标 / 切片不交叉引用 / 前端不重算） | §11.1#5 |

### J 组 —— 历史 review 遗留的零散事项

| ID | 事项 | 来源 |
|---|---|---|
| J-01 | 注释与 test 函数名的历史称谓清理（leaf 包 doc comment 里的 `vmr story`/`vmr report`/`-corpus`，`TestCmdStory_*`） | gemini N9 / claude N9 / round-4 §4.3 |
| J-02 | `RequestRow.TS` 收敛为 epoch ms（而非 RFC3339 字符串）+ 排序键对齐 | claude C3 完整方案 |
| J-03 | `SessionRow.from/to` 是否需从 RFC3339 转 epoch ms（round-4 缩小项） | round-4 §4.2 |
| J-04 | `ValidateManifest` 拒绝残缺 macro 切片集（历史 review N8） | claude N8 |
| J-05 | `vmr-report.md` §8 附录 + 详单回链指向已删除的 `vmr-requests.md`（历史 review N16/N4） | claude N4/N16 |
| J-06 | `cmd_journey_setup.go` 读旧 `stories/vmr-stories.json` 兼容 shim（历史 review N6） | claude N6 |
| J-07 | `WriteRequestsIndex` 顶层 `vmr-requests.json` 兼容副本死分支（历史 review N7） | gemini/claude N7 |
| J-08 | `-render-only` 重渲染累积产物（compares/journeys）语言混排风险（历史 review F11） | gemini F11 |
| J-09 | `config.yaml` endpoint group 旧语法导致定价层降级（历史 review N17） | claude N17 |
| J-10 | `-render-only` L3 缓存命中直接信任磁盘产物，手改 `.md` 后 warm render-only 不修复（历史 review N18） | claude N18 |
| J-11 | 方案 §7.2 "时间窗 -from/-to" 术语与实现不符（`vmr analyze` 无此 flag）（历史 review N12） | claude N12 |

### K 组 —— 进一步改进/优化机会（不属方案缺口，历史 review C 节 + 本轮新提）

| ID | 事项 | 来源 |
|---|---|---|
| K-01 | 流式序列化削峰值内存（`WriteMacroSlices` build-marshal-release 逐切片） | 方案 §11.3 / claude C1 |
| K-02 | `bodies` blob key 用短哈希前缀（前 16 hex 而非全长 64） | claude C2 |
| K-03 | `clientsWithSiblingFile` 恒空分支彻底移除 | claude C3 |
| K-04 | `structureExcerptChars = maxBodyExcerptChars` 别名消除 | claude C4 |

---

## 第二部分：逐项执行详情

### 验证手段

- 基线：`go build ./...` / `go vet ./...` / `go test ./...` 全绿（38 包，`cmd/vmr` 20.8s）。
- 端到端冒烟：`./vmr analyze -o /tmp/vmr-v4-e2e -details logs/vmr-audit-2026-08-24.jsonl.zst`（519 records，15 journeys）——产物拓扑、权限、字段逐一比对方案 §4 与 §3.2/§3.3。
- `-render-only` 字节等价：`cp` 全量产物 → `./vmr analyze -render-only` → `diff -rq` 除 `.cache/` 外**逐字节一致**（manifest `generated_at` 亦一致，实测无漂移）。
- 看板守卫：`TestAllDashboardPages_ReadSnakeCaseFields` + `TestJS_DashboardRenderSmoke` 全绿。
- 逐项核对：读实现 + 读对应测试断言 + 抽查落盘 JSON 结构，不采信任何文档 claim。

### 逐组核验结论

| 组 | 结论 | 关键证据 |
|---|---|---|
| **A 概念归一 / CLI / 拓扑 / 兼容边界** | 🟡 部分完成 | A-01/02/03 代码符号与 CLI 已彻底收敛（`main.go` 仅 `case "analyze"`；无 `report`/`story` 子命令、无 `-corpus`/`-story-only`）。A-04..A-10 冒烟逐字命中：`manifest.json` `format=11` 最后写、`macro/` 五切片、`requests/{index.json,failed.*,details/r-*,evidence/}`、`journeys/{index,details/j-*}`、`compares/{index.*}`、六骨架页根级平铺、`.cache/parse/`；全树 0700/0600。A-08：仓内零 `vmr-report.json` 写出，`Report2` 保留为内存聚合形状，`LoadReport` 从切片重装。A-10：`Manifest` 结构无任何指标字段。**残留见 T-A（注释/文档历史称谓）与 T-B（v4 Analytics §4 过期）**。 |
| **B 数据层切片 + Schema** | ✅ 已全部完成 | B-01 `slices.go` 五切片 + `WriteMacroSlices`，`TestMacroSlices_EquivalenceWithReport2` 在位。B-02 `summary.json` 的 `overall.cost_estimate` / `efficiency` 独立落标量。B-03 `overall.tokens_coverage_pct` 落盘。B-04 `finance.json.cost_coverage{unpriced_count,incomplete_rate_count,degraded_estimate_pct}`。B-05 `manifest.footnotes`（¹²†‡⚠️low-n⭐）+ `disclaimers[]`。B-06 `summary.highlights[]` 实测非空。B-07 `requests/index.json.sessions`（title/alias/tasks 投影，50 项）。B-08 `requests/index.json.journey_link`（15 项，session→`details/j-*.md`）。B-09 `ts`=epoch ms + `ts_display`（`RequestRow`、`manifest.generated_at`、journey `from/to_display` 均双字段）。B-10..B-13 `manifest.go`：`BuildManifest` 在 `rep != nil` 时 `requireMacroSlices` 拒绝残缺；`ValidateManifest` 校验 format + 逐切片 sha256 + macro 集齐全；`writeJSONAtomic` = CreateTemp+Chmod+Rename。 |
| **C Journey JSON 自包含** | ✅ 已全部完成 | C-01 `bodies` 顶级 blob 表（实测 91 项）。C-02 三级 `match` 实测分布 `exact:268 normalized:159 positional:40`。C-03 `CompactionRef.PredecessorExcerptRef`。C-04 tool args/result/compaction 截 3000，`resp_ref` 不截（实测 body 最长 30710，13 处 >3000）。C-05 全 15 journey 无孤儿/无悬引用（64-hex 引用全解析）。C-06 `LLMInterpretation` 记录入 `j-<id>.json.llm_interpretation` + compare 侧 `llm_divergence`；`cmd_render_only.go` 的 `strings.Index("## LLM ")` 刮取 hack 已删，`.md` 是 `.json` 的纯函数。C-07 请求详单显式豁免（O(N²)）。 |
| **D 请求索引删除 + compares 索引** | ✅ 已全部完成 | D-01 `vmr-requests.md`/`-<tag>.md`/`-cron-*.md` 零写出。D-02/03 `requests/index.json` 唯一机读源；`failed.md`/`failed.jsonl` 保留下沉。D-04 旧渲染函数族（`renderChatUserDoc`/`renderSessionCard`/`partitionGroups`/`clientsWithSiblingFile` …）已删（`29b7f42`），仅存一行解释性注释。D-05 `RebuildComparesIndex` 扫 `compares/*.json` 每次 analyze 重建。D-06 `AllSlicePaths` 不含 `compares/*`。D-07/08 `compares/index.md` 空目录出生成引导，给可复制 `vmr analyze -compare`。D-09 `compares/index.md` 实测随 `-lang zh` 出中文标题（`i18n/journey_compares_index.go`）。 |
| **E 文件名约定** | ✅ 已全部完成 | E-01 `detail_file` = `r-20260823-235938.429_agent_..._ok_2f2e3712.md`。E-02/03 journey 文件名 `j-lobster-...`，无 `journey-` 前缀、无 `-partial` 后缀。E-04 CHANGELOG 记 compare 侧 `-partial` 后缀退役（`8663dee`），partiality 经字段 + banner + 索引行 + 看板徽标。 |
| **F ViewModel + 单一渲染路径** | ✅ 已全部完成 | F-01 `renderAllFromDisk` 全量与 `-render-only` 共用，byte-equiv 实测 + `TestRenderOnly_ByteEquivalenceWithFullRun` / `_MacroOnly` / `_Benchmark`。F-03 `rg 'text/template'` 零命中。F-04/05 `viewmodel_*.go` ↔ `i18n/report_*.go` 配对由 `archtest/i18n_test.go` 强制 + `vm_literals_test.go` AST 守卫。F-06/07 `-render-only` 覆盖全部常驻人读产物，作业清单来自 `journeys/index.json`。F-08 三条渲染路径都调 `dashboard.WriteSkeletons`。F-09 `TestRenderOnly_LanguageMismatchRejected`（`m.Lang != requested` → exit 1 + 指引全量重跑）。F-10 无 `vm_*.json` 写出。F-11 `NewTimePoint` 双字段 + §9 守卫。F-12 `testdata/fmt_cases.json` Go/JS 各跑一遍。F-13 `RenderMarkdownFromSummary` 吃 `JourneySummary`。F-14/15 `viewmodel_golden_data_test.go` 比对 VM 结构；`structure_test.go` 的 `LosslessReconstruction` 只喂 `j-<id>.json`。 |
| **G HTML 看板** | 🟡 部分完成 | G-01/02 六页确认，`analyze` 只写 JSON。G-03 `common.js` 有 `svgLineChart`/`svgBarChart`/`svgHeatmap`/`svgLatencyPlot` 四图元——**方案 §6.3 写「散点」，实现是延迟分位图 `svgLatencyPlot` 替代，属合理替换但与字面不符**（见 T-C，低优）。G-04/05 `#data=` hash 传参，根级平铺。G-06/07/08 缺省探测 + `file://` 提示分支在位。G-09/10/11 `render_html*`/`render_compare_html`/`toolwaste_html`/`story/assets` + `-redact`/`-html` flag 全部零残留。G-12..G-18 `server/reports.go`：`analytics.serve` opt-in、`len(APIKeys)==0` 全树 403 不复用放行型 auth、`filepath.Clean` + 逐级 `Lstat` 拒 symlink + 禁目录列表、分层鉴权、`serve_dir` 默认 `./reports` 不走 rundir、缺失 404 + 一次性日志；`archtest` 强制 `server` 不 import 分析半区（report/journey/ctxgraph/taskseg/reqdetail）。G-19/20/21 `ManifestFormat=11` == `EXPECTED_MANIFEST_FORMAT=11`，`versionBehavior` 纯函数 + `js_test.go` 的 `vbCases`。G-23 `TestAllDashboardPages_ReadSnakeCaseFields` 通过。G-24 `common.js` 内联进六页（`src="common.js"` 零命中，`versionBehavior` 命中）。G-25 chrome 已统一英文（round-3 N10）。G-26 `finance.json.pricing.currency` + `CURRENCY_SYMBOLS`。 |
| **H 缓存层** | ✅ 已全部完成 | H-01/H-05 `internal/digest` stdlib-only 叶子包是**唯一** Digest 构造（`zeroInternalDepPackages` 登记）；`internal/journey/digest.go` 仅是 `ComputeJourneyDigest` 组合层，非重实现；`internal/report/digest.go` 已删；`cmd/vmr/digest_parity_test.go` 已退役。H-02 `EncodeInt64` BigEndian / `EncodeFloat64` `Float64bits`。H-03/04 `ComputeInputHashes` sha256 无 mtime fast path；md5 底座作 `[16]byte` 分量喂链。H-06 `.cache/parse/`。H-07 `computeTargetL2` = `Digest(inHashes ‖ pricingFP ‖ ManifestFormat ‖ paramsFP)`。H-08 `ComputeL3Digest(vmFP, RendererVersion, lang)`。H-09 `ComputePricingFingerprint(standardGen, exchangeRates, policies)` 只含影响金额部分，`TestComputePricingFingerprint` 在位（N11 落实）。H-10 `AnalysisParams` 含 Lang/TaskProfile/IncludePartial/IncludeSelfTraffic/SelfTrafficTags/LLMSelfTag/LLMAddr/LLMModel/DisplayCCY/RenderAll/Details/Mode。H-11 无切片级隔离 + `-no-cache` 常驻。H-12 `TestAnalyzeCache_InvalidationMatrix` + `_ColdWarmAndNoCache`。H-13 `journey_prevmanifest_test.go`。H-14 `TestAnalyzeCache_L2HitSweepsOrphanJourneys`（L2 命中也清扫 + 重建 compares 索引）。H-15 LLM identity 入 `paramsFP`（F3 落实）。 |
| **I 守卫与不变量** | ✅ 已全部完成 | I-01 `viewmodel_golden_data_test.go`。I-02 `structure_test.go`。I-03 `slices_test.go` `EquivalenceWithReport2`。I-04 `journey_prevmanifest_test.go`（边界用例）+ reqdetail 渲染指纹携带 `m/prev`。I-05 `archtest/vm_literals_test.go` `TestArchitecture_ViewModelNoBareLiterals`。I-06 `file_sizes_test.go`/`func_sizes_test.go` 登记 `viewmodel_*.go`（本轮顺带修了两处引用已删 `section_*.go` 的注释）。I-07 `i18n_test.go` 配对对象 = `viewmodel_*.go`。I-08 `import_boundaries_test.go` `server` 禁 import 分析半区。I-09..I-18 逐条具名在位（`ByteEquivalenceWithFullRun`/`ColdWarmAndNoCache`/`PureFunctionsAndFixture`/`DualTimeField`（`slices_test.go`）/`vbCases`/`BodiesNoOrphansNoDangling`+`BodiesIntegrity`/`ThreeLevelToolPairing`/`CleanOrphanJourneys`+`L2HitSweepsOrphanJourneys`/`CleanOrphanJourneys_EdgeCases`/`RebuildComparesIndex_ScanAndSelfHealing`）。I-19..I-23 五条不变量 archtest + 断言全守住。 |
| **J 历史 review 遗留零散事项** | 🟡 大部分完成 | J-02 `RequestRow.ts` = epoch ms（`1787500778429`）+ 排序键对齐。J-04 `ValidateManifest`/`BuildManifest` 拒残缺 macro 集。J-05 `report_doc.go`/`reqdetail_detail.go` 链接改指 `requests/index.json` + `request-browser.html`。J-06 `cmd_journey_setup.go` 无 `stories/vmr-stories.json` shim。J-07 `WriteRequestsIndex` 不再写顶层 `vmr-requests.json`（残留一处无害路径归一 fallback，见 T-D）。J-09/N17 `config.yaml` endpoint group 旧语法（见 T-E）。J-10/N18、J-11/N12 属观察/文档脚注项（见 T-F）。**J-01（注释历史称谓）见 T-A，未完成。** |
| **K 优化机会** | 部分处理 | K-04 `structureExcerptChars = maxBodyExcerptChars` 死别名本轮已删（含 3 处 test 引用改名）。K-03 `clientsWithSiblingFile` 函数早已删（仅存注释）。K-01（流式序列化削峰）/ K-02（bodies key 短哈希）记为可选优化，见第三部分 C 节。 |

### 本轮直接修复（提交前）

| # | 修复 | 文件 |
|---|---|---|
| 1 | 方案提案文档 footer 状态行「设计提案，待评审」→「已实施」，与头部 Status 注记一致（A-14） | `analyze_architecture_redesign_opus-5.md` |
| 2 | 删除 `structureExcerptChars` 向后兼容死别名，3 处 test 引用改用 `maxBodyExcerptChars`（K-04，CLAUDE.md 反对兼容 shim） | `internal/journey/structure.go`、`structure_test.go` |
| 3 | `archtest/file_sizes_test.go` 两处注释引用已删的 `render_doc.go`/`section_*.go` → `aggregate.go`/`viewmodel_*.go`（I-06 附带） | `internal/archtest/file_sizes_test.go` |

---

## 第三部分：总结（Phase A —— 方案落地评审）

### A. 基于方案的 review 事项完成度

| 组 | 事项 | 状态 |
|---|---|---|
| A | 概念归一 / CLI / 拓扑 / 兼容边界 | **部分完成**（代码/CLI/拓扑 ✅；注释与 v4 Analytics §4 文档历史称谓残留 —— T-A / T-B） |
| B | 数据层切片 + Schema 演进 | **已全部完成** |
| C | Journey JSON 自包含 D18 | **已全部完成** |
| D | 请求索引删除 D7 + compares 索引 D21 | **已全部完成** |
| E | 文件名约定 D17 / D19 | **已全部完成** |
| F | ViewModel + 单一渲染路径 D3/D4/D5/D10/D11/D12 | **已全部完成** |
| G | HTML 看板 D6/D9/D13/D14/D15/D16 | **部分完成**（功能/安全/版本探测/字段对齐 ✅；「散点」图元字面未兑现 —— T-C，低优） |
| H | 单一 Digest / 三级缓存 / 指纹规范 D1/D8 | **已全部完成** |
| I | 测试守卫（既有迁移 + 18 条新增）+ 五条不变量 | **已全部完成** |
| J | 历史 review 遗留零散事项 | **大部分完成**（J-01 注释清理未完成 —— T-A；T-D/E/F 为低优/观察项） |
| K | 优化机会 | K-04 已修；K-01/K-02 记为可选优化 |

**总体判断**：方案的四个 Phase 与裁决 D1–D21 已**实质、正确落地**。以 `050ad25..69aa242` 的累积效果独立核验，没有发现数据正确性、渲染路径一致性、缓存失效逻辑、安全模型层面的错误；`-render-only` 与全量运行逐字节一致，冒烟产物拓扑与方案 §4 逐字匹配。3 份历史 review + 其后数轮迭代的工作扎实，本轮独立取证**大体证实**其结论（未采信其 claim，均回到代码验证）。**剩余全部为文档/注释一致性问题，无一涉及行为**。

### 部分完成 / 未完成事项详述

#### T-A. 注释与测试符号里的历史命令称谓未清理（J-01）—— **未完成，建议专项**

- **问题描述**：`vmr report` / `vmr story` 作为命令名仍出现在约 51 个 `.go` 文件的 doc comment 里（`internal/fmtutil`、`internal/pricing`、`internal/quota`、`internal/config`、`internal/audit`、`internal/taskseg`、`cmd/vmr/reportconfig.go` 等）；`internal/journey/journeyindex.go` 残留 `storiesDir` / `cmdStory branch` / `reports/stories/` 字样；`cmd/vmr/cmd_journey_test.go` 多处测试失败信息写 `cmdStory ...`；`internal/archtest/import_boundaries_test.go` 与 `doc_refs_test.go` 的说明性注释举例仍用 `vmr story` 及已 retarget 的 `[vmr-requests.md]` 链接示例。
- **根因分析**：命令更名（`050ad25` 起）是结构性改动，注释里的历史称谓分散在几乎所有 leaf 包，多轮实施中无人负责"全仓注释扫一遍"；且 `report` / `story` 在部分注释里是**描述两个消费者角色 / 两个半区**（合法），需要人工甄别，不能无脑替换。前三轮 review 均将其列为低优后延。
- **建议方案**：一次独立的机械清理 pass，逐文件人工过：命令语境 `vmr story` / `vmr report` → `vmr analyze`（含 `vmr story -compare` → `vmr analyze -compare`）；`vmr-stories.json`（指当前文件时）→ `journeys/index.json`；`storiesDir` / `reports/stories/` → `journeys/` 口径；`cmdStory` 测试失败信息 → `cmdJourney` / `analyze`；保留"report 半区 / journey 半区"这类角色描述。archtest 说明性注释里的失效示例一并更新。
- **ROI**：**低优先级，中等工作量（~51 文件），零功能风险**。纯一致性与"新读者不被旧称谓误导"收益。方案 §2.1「把 story 一词从代码、CLI 与产物中一次清干净」在注释层尚未兑现。适合作为一次独立低优 commit 或一个受控 sub-agent 任务，不阻塞任何后续工作。

#### T-B. `docs/VirtualModelRouter_Design_v4_Analytics.md` §4（i18n 一节）仍描述已废弃的 `section_*.go` 组织 —— **部分完成，建议随 T-A 同批**

- **问题描述**：该文档主体（§1–§3、§2.2 数据形状、§2.6 ViewModel、§2.7 缓存、§8）已按 current-state 改写并准确（实测：正确描述 `vmr analyze` 单入口、五切片 + manifest、`internal/digest` 叶子包、单一渲染路径）。但 §4.1 / §4.3 仍写：
  - "延续 `internal/report` 里 `section_*.go` 已经验证过的组织原则：`report_workload.go` 对应 `section_workload.go`，`story_render.go` 对应 `render_md.go`"（`section_*.go` 已全删、`story_render.go` 现为 `journey_render.go`）；
  - "`section_efficiency.go` 的 Markdown 渲染路径不读被覆写的值"（→ `viewmodel_efficiency.go`）；
  - §2.3 已知缺口一段提"暂缓到额度看板（`section_quota.go`）那批"（`section_quota.go` 从未存在，是假设文件名）；
  - 多处 `report`/`story` 包名对（§4.1 L383/385、§4.4、§4.5 `story.Interpret`、§附录 L503/511/522）应为 `report`/`journey`。
- **根因分析**：2D 文档同步组按 grep 清单修了主体，但 §4 的成段改写超出其授权范围（改它会动段落结构），被留下。gemini review T3 曾指出，minimax review 认为已在 `f8e23f0` 完成 —— 本轮独立核验发现 §4 仍过期。
- **建议方案**：按"current state, not changelog"纪律局部改写 §4.1 / §4.3 / §4.4 / §4.5 与附录表的包名与文件名引用：`section_<x>.go` → `viewmodel_<x>.go`（report 侧）；`story_render.go` → `i18n/journey_render.go`；`section_quota.go` 一句改为泛指"额度相关展示"；`report`/`story` 包名对 → `report`/`journey`；`story.Interpret` → `journey.Interpret`。不改段落论证结构，只换失效的符号。
- **ROI**：**中等（半天），随 T-A 同批做**。防止下一个维护者按旧文档去找不存在的 `section_*.go`。该文档自我定位"读完即可维护与二次开发"，过期符号是实打实的误导。

#### T-C. 看板 SVG 图元「散点」未按字面实现 —— **低优，可不改**

- **问题描述**：方案 §6.3 列"覆盖折线/柱状/热力/散点四种形态"。`common.js` 实际提供 `svgLineChart` / `svgBarChart` / `svgHeatmap` / `svgLatencyPlot`（端点 P50/P90/P99 延迟分位图），没有通用散点图元。
- **根因分析**：Phase 2 实现时按看板页的真实需要选了"延迟分位图"这个更专用的形态替代通用散点 —— 六个看板页里没有任何一处需要通用二维散点。
- **建议方案**：二选一 ——(a) 认可替换，把方案 §6.3 的"散点"改为"延迟分位"，登记进 §11.2 取舍表；(b) 若将来 benchmark 看板要画 Spearman 相关散点，再补 `svgScatter`。推荐 (a)。
- **ROI**：**低**。当前无消费方，是"方案字面 vs 实现"的措辞差，非功能缺口。

#### T-D. `WriteRequestsIndex` 残留一处路径归一 fallback —— **低优，可随 T-A 清理**

- **问题描述**：`internal/report/requests.go` 的 `WriteRequestsIndex` 仍有 `if filepath.Base(dir) != "requests" { targetDir = filepath.Join(dir, "requests") }`。当前唯一调用点传的就是 `.../requests`，分支不执行。它不再写任何顶层 `vmr-requests.json`（那个死分支已在 `29b7f42` 删除），只是一段冗余的目录归一。
- **建议方案**：`WriteRequestsIndex` 直接要求/断言 `dir` 已是 `requests` 目录，删掉 fallback。
- **ROI**：**极低**。零行为风险、~3 行，适合随 T-A/N3 收尾一起做，不值得单独立项。

#### T-E. 工作区 `config.yaml` 的 endpoint group 使用旧语法，触发定价层降级（N17）—— **待你决策（不属方案缺口）**

- **问题描述**：`./vmr analyze` 启动即打印 `config: config.yaml not usable (parse yaml: line 222/234/266/293: cannot unmarshal !!seq into map[string][]config.EndpointGroup)`。分析半区已优雅降级（`$` 估算仅用标准价目表、`§2.5` 不带额度对照），不崩溃。
- **根因分析**：本地 `config.yaml` 的模型路由组声明用了序列（`- ...`）而当前 schema 要求映射（`map[string][]EndpointGroup`）。是本地配置文件的语法陈旧，不是分析半区的 bug。
- **建议方案**：对当前工作区 `config.yaml` 第 222/234/266/293 行附近的 endpoint group 声明做格式规整（改为 map 形态），或用 `./vmr check -c config.yaml` 逐条核对。规整后 Phase B 的真实数据测试才能覆盖账户定价覆盖与额度对照路径。
- **ROI**：**中**。不改不影响分析正确性，但会让 Phase B 的定价/额度相关产物一直走降级路径、测不到账户覆盖分支。**Phase B 开始前建议先处理**（见下方 Phase B action plan 的前置项）。

#### T-F. 观察 / 文档脚注项（N11 / N12 / N18）—— **无需改动，仅记录**

- **N11（配置指纹字段核对）**：本轮已核实 `ComputePricingFingerprint` 只纳入 `standardGen` + `exchangeRates` + provider `policies`，不含 `listen` 等无关字段，`TestComputePricingFingerprint` 钉住"改 `StandardGeneratedAt` 必变指纹"。**结论：已正确，观察项关闭。**
- **N12（方案 §7.2 "-from/-to" 术语）**：`vmr analyze` 无 `-from/-to` flag，输入选择靠文件 glob，时间覆盖由 `ComputeInputHashes` 完整捕获。**建议**：方案 §7.2 加一句脚注（零代码），或视为已知措辞差不处理。
- **N18（`-render-only` L3 暖缓存信任磁盘）**：用户手改 `.md` 后 warm `-render-only` 不修复，需 `-no-cache`。属 L3「该 renderer+lang 已渲染过」既有语义，非本方案引入。**建议**：`KNOWN_ISSUES` 一句话登记或维持现状。

### B. 评审过程新发现的问题

| # | 问题 | 严重度 | 处置 |
|---|---|---|---|
| N-1 | 方案提案文档 footer 与头部 Status 注记自相矛盾（footer 说"待评审"，头部说"已实施"） | 低 | ✔️ **已直接修复** |
| N-2 | `structureExcerptChars` 向后兼容死别名（CLAUDE.md 反对兼容 shim） | 低 | ✔️ **已直接修复** |
| N-3 | `archtest/file_sizes_test.go` 两处预算注释引用已删的 `render_doc.go` / `section_*.go` | 低 | ✔️ **已直接修复** |
| N-4 | `requests/evidence/` 除方案 §4 列出的 `sysprompt-<h8>.md` 外还写 `tools-<h8>.md`（工具 schema 证据块） | 极低 | 非缺陷，属方案后的加性扩展；建议方案 §4 拓扑图补一行 `tools-<h8>.md`（随 T-B 一起）|
| N-5 | `internal/journey/digest.go` 版本头 `// Ver 2026-09-15, by pi` 是未来日期（今天 2026-09-07） | 极低 | 非缺陷（CLAUDE.md：陈旧/异常头日期不值得单独修）；仅记录 |

新发现问题全部为低/极低severity，N-1/N-2/N-3 已当场修复，N-4/N-5 记录备查。**没有发现任何行为、数据正确性或安全层面的新问题。**

### C. 进一步改进 / 优化机会（不属方案缺口）

1. **K-01 流式序列化削峰值内存**（方案 §11.3 顺带机会）：`WriteMacroSlices` 目前顺序 build 五个 slice 结构再逐个 marshal，可改 build-marshal-release。低优，仅当 RSS 成为痛点时做。
2. **K-02 `bodies` blob key 用短哈希前缀**：`structure.go` 的 `hashText` 用全长 64-hex sha256 作 `bodies` map key，单个 journey JSON 里每个 ref 多次出现。改前 16 hex（碰撞概率对单 journey 规模可忽略，跨 journey 无需全局唯一）可缩小 `j-<id>.json`。实测一个中等 journey `bodies` 91 项、体积可控，收益有限。低优。
3. **`request-browser.html` 排序键**：已随 J-02（`RequestRow.ts` 转 epoch ms）解决，字符串排序错位风险消除。

### D. 关于 sub-agent 并行的评估

Phase A 剩余修复项（T-A 注释清理、T-B 文档 §4 改写）同属"文档/注释一致性"单一 domain，是一次串行的机械清理，不构成正交可并行的任务集；已当场完成的 N-1/N-2/N-3 体量极小。因此 Phase A **不启用 sub-agent**，由主控顺序完成。T-A / T-B 若获批，适合作为**一个**受控 Pi worker 任务（worktree 隔离、白名单限定 docs + `.go` 注释 + test 函数名），与后续工作正交。

---

## 第四部分：Phase B —— 独立验收测试（真实数据 + 真实 LLM）

（执行过程中回填）

