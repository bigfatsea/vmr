// Ver 2026-09-07, by Claude (claude-sonnet-5)

# vmr analyze 架构重构方案 —— 独立落地复核与端到端验收报告

<!--
  基准方案:   docs/future-strategy/analyze_architecture_redesign_opus-5.md  (D1–D21, §1–§11)
  问题来源:   上述方案 + 5 份历史评审文档中列出的全部 feature / 模块 / 任务 / 问题
               (analyze_redesign_review_plan_gemini-3.8-flash / _v2_minimax-m3 /
                _review_claude-sonnet-5 / _review_v4_claude-sonnet-5 /
                已删除的 comprehensive 草稿)
  代码基线:   050ad25 (refactor: rename internal/story → journey, 方案实施起点)
              至本轮 HEAD, 视作一次叠加变更逐项核实. 起点确认: 050ad25 是首个重构
              提交 7abf0a4 的父提交.
  评审原则:
    1. 只从源码 / 测试断言 / 实跑结果取证, 完全不采信历史文档的 "已完成 / 已实现" 声称.
    2. 不阅读项目内其他 review 记录 (_review/, docs/tasks/ ledger, NOTES_FOR_LEAD.md,
       _tmp/*review*), 独立判断.
    3. 事实清楚 + 方案明确无争议 + 十足把握 -> 当场直接修复并留痕 (直接提交 main, 无 trailer).
    4. 复杂 / 有争议 -> 记录待用户决策, 不动手.
    5. 方案 principle / KNOWN_ISSUES / previous decision 并非圣旨, 有理有据可挑战.
    6. 第一阶段 (Phase A) Review 闭环且无大问题后, 制定并执行真实日志 + 真实 LLM 的
       端到端验收测试 (Phase B).
-->

---

## 目录

- [第一部分 · Action Plan：功能模块与核验事项全清单](#第一部分--action-plan功能模块与核验事项全清单)
  - [1.A 核心裁决 D1–D21](#1a-核心裁决-d1d21)
  - [1.B Phase 1–4 功能模块](#1b-phase-14-功能模块)
  - [1.C §9 测试守卫清单](#1c-9-测试守卫清单)
  - [1.D §11.1 不变量](#1d-111-不变量)
  - [1.E §11.2 已知取舍（可挑战）与 §11.3 顺带机会](#1e-112-已知取舍可挑战与-113-顺带机会)
  - [1.F 历史评审遗留问题全集（T / F / N / G / C / N-B 系列）](#1f-历史评审遗留问题全集t--f--n--g--c--n-b-系列)
- [第二部分 · Review 执行与代码核验详情](#第二部分--review-执行与代码核验详情)
- [第三部分 · Review 事项完成度总结](#第三部分--review-事项完成度总结)
- [第四部分 · 过程新发现问题与处理](#第四部分--过程新发现问题与处理)
- [第五部分 · 真实数据与真实 LLM 端到端验收测试（Phase B）](#第五部分--真实数据与真实-llm-端到端验收测试phase-b)

---

## 第一部分 · Action Plan：功能模块与核验事项全清单

> 本清单是本次独立 Review 的**核验基准**。汇集自方案正文、D1–D21 裁决、四阶段路线图、
> §9 守卫规范、§11 不变量与取舍，以及 5 份历史评审文档中出现过的全部 T/F/N/G/C/N-B 编号问题。
> **不预设任何一条的落地状态**——每一条都要回到 `050ad25..HEAD` 的实际代码里核实。

### 1.A 核心裁决 D1–D21

| # | 裁决 | 核验目标 |
|---|---|---|
| D1 | 切片为消费弹性拆，不为缓存拆；缓存粒度为整套产物一个指纹 | `slices.go` 五切片；`cache.go` L2 单指纹；无切片级隔离 |
| D2 | `vmr-report.json` 单体不保留，一步解构为切片，无兼容视图、无过渡期 | 仓内零 `vmr-report.json` 写出；宏观渲染源来自切片 |
| D3 | Markdown 不用模板引擎（含 `text/template`），无 `-templates` | `rg text/template` 零命中；固定序列化器 |
| D4 | 全部文案（含章节标题）进 ViewModel，经 i18n，渲染器只表达结构与顺序 | `viewmodel_*.go` + `i18n/report_*.go` 配对；AST 守卫 |
| D5 | `-render-only` 覆盖全部常驻人读产物；`details/`+`evidence/` 永久排除 | 覆盖面表逐行；豁免 details/evidence |
| D6 | 自包含 HTML 废弃，收敛为骨架 + fetch 一种形态 | `render_html*`/`render_compare_html`/`toolwaste_html`/`story/assets` 零残留 |
| D7 | 人读请求索引整族删除（含 `-<tag>.md` / `-cron-*.md`），`failed.md` 保留 | 零写出；旧渲染死代码清理；`requests/index.json` 唯一机读源 |
| D8 | 全系统唯一 Digest 构造：长度前缀的有序 sha256 链；md5 底座原样喂入 | `internal/digest` 叶子包唯一实现；三性质；定宽标量编码 |
| D9 | `/reports/` HTTP 托管默认关闭；`api_keys` 未配置时硬性 403（非降级放行） | `analytics.serve` opt-in；无 key 全树 403；路径校验；禁目录列表；分层鉴权 |
| D10 | `-render-only` 继承落盘 JSON 的语言；换语言必须全量重跑 | `-lang` 与 manifest 不一致 exit 1 + 指引 |
| D11 | 聚合类产物单一渲染路径：聚合→写 JSON→读 JSON→ViewModel→序列化→写 MD；全量与 render-only 同路径 | `renderAllFromDisk` 共用；byte-equiv 守卫 |
| D12 | ViewModel 不落盘；前端消费 raw 切片自带格式化；跨语言 fixture 钉死漂移 | 无 `vm_*.json` 写出；`fmt_cases.json` 双跑 |
| D13 | 看板 `#data=` hash 传参，不用 `?query`；磁盘布局与 HTTP 路径逐字一致 | 骨架页根级平铺；相对路径 |
| D14 | 数据版本探测 banner 进骨架页；查 `manifest.format`；不一致警告不阻断 | `EXPECTED_MANIFEST_FORMAT` 与 Go 常量一致；三分支纯函数 |
| D15 | `-redact` 随自包含 HTML 一并删除 | flag + `redact bool` 分支 + 双语文档零残留 |
| D16 | 托管目录默认 `./reports`（与 `analyze -o` 同默认）；不走 `rundir`；目录不存在仅 404 | `analytics.serve_dir` 默认值；缺失非启动错误 |
| D17 | 详单文件名加 `r-` 类型前缀 | `reqdetail.FileName` 单点 + 包装函数透传 |
| D18 | Journey JSON 自包含：`structure` tree + 同文件 `bodies` blob 去重表；三级 `match`；compaction 前驱摘录；截断口径统一（RespText 不截）；LLM 解读落 JSON | 逐条 |
| D19 | 文件名去 `-partial` 后缀（journey + compare）；partial 由字段 + banner + 索引行表达 | journey 与 compare 两侧；去 `journey-` 冗余前缀 |
| D20 | 作业清单严格来自索引；全量运行清扫 `journeys/details/` orphan；清扫严禁波及 `compares/` 与 `requests/` | `CleanOrphanJourneys` 范围；L2 命中路径也清扫 |
| D21 | `compares/index.{json,md}` 每次 analyze 扫目录派生重建；`compares/` 子树整体不进 manifest 指纹 | `RebuildComparesIndex`；`AllSlicePaths` 不含 compares |

### 1.B Phase 1–4 功能模块

**Phase 1 · 数据层闭环**
- P1-1 概念归一：`internal/story`→`internal/journey` 包与全部代码符号；`story` 一词从代码/CLI/产物清除
- P1-2 `corpus`→`Journey Benchmarks`：`journeys/benchmarks.{json,md}`，`-corpus`→`-benchmark`（无别名）
- P1-3 CLI 收敛：删 `vmr report`/`vmr story` 子命令；`-story-only`→`-journey-only`（无别名）；单入口 `vmr analyze`
- P1-4 五 macro 切片 + `manifest.json`（最后写）；`requests/index.json`；`requests/failed.{jsonl,md}`
- P1-5 §3.3 七类渲染期现算事实下沉：① `tokens_coverage_pct`/`dur_low_n` ② `CostCoverage` 结构体 ③ `meta.footnotes`/`meta.disclaimers` 注册表 ④ `highlights` 进 summary 切片 ⑤ 会话/任务标题映射投影进 `requests/index.json` ⑥ `journey_link` 跨产物链接 ⑦ 时间双字段 `ts`(epoch ms)+`ts_display`
- P1-6 journey JSON 自包含（D18 全体）
- P1-7 目录拓扑归位：`journeys/`、`compares/`、`requests/details|evidence/`、`.cache/parse/`
- P1-8 详单文件名 `r-` 前缀（D17）；journey 文件名归一（D19）
- P1-9 产物提交顺序与原子性（manifest 最后写、CreateTemp+Chmod 0600+Rename）+ orphan 清扫（D20）
- P1-10 `compares/index.{json,md}` 扫目录派生（D21）
- P1-11 人读请求索引整族删除（D7），`vmr-report.md` 相关链接改指看板与 `journeys/index.md`

**Phase 2 · HTML 看板**
- P2-1 `go:embed` 骨架 + 主题变量系统（复用 VMR Forensics 视觉）
- P2-2 零依赖内联 SVG 图元：折线 / 柱状 / 热力 / 延迟分位图（`svgLatencyPlot`）
- P2-3 六个看板页 + `#data=` 加载协议 + `file://` 提示降级
- P2-4 骨架页版本探测 banner + 渲染异常归因（§6.6）
- P2-5 前端格式化函数 + 跨语言 fixture（§5.6）
- P2-6 删除 `render_html*.go`/`render_compare_html.go`/`toolwaste_html.go` 与 `story/assets/`
- P2-7 `server` 挂载 `/reports/*`：显式开关、无 key 拒绝、路径校验、禁目录列表、分层鉴权
- P2-8 看板 JS 按切片真实 snake_case json tag 读数据（非 Go PascalCase 字段名）
- P2-9 `common.js` 实际随骨架页部署可用（内联）
- P2-10 骨架页 chrome 本地化策略一致
- P2-11 看板金额显示随切片币种（`pricing.currency` + 符号映射）

**Phase 3 · ViewModel 与固定序列化器**
- P3-1 report 侧 ViewModel（各 section 改为构建器）
- P3-2 journey 侧 ViewModel —— `render_spine` 重建到自包含 journey JSON 之上（`RenderMarkdown` 吃 `JourneySummary`）
- P3-2b compare 侧 ViewModel（`RenderComparisonMarkdown` 按 record 渲染）
- P3-3 固定结构 Markdown 序列化器（无模板引擎）
- P3-4 golden 测试下沉到 ViewModel 层
- P3-5 `-render-only`（覆盖面按 §5.4 表；作业清单来自 `journeys/index.json`；语言继承）
- P3-6 ViewModel 中禁止未 i18n 的英文句子字面量（AST 守卫）

**Phase 4 · 产物级缓存**
- P4-1 整套产物指纹（有序链式 sha256）与失效矩阵七行
- P4-2 L1 输入解析缓存归位 `.cache/parse/`
- P4-3 L2 产物数据缓存：`Digest(输入哈希集 ‖ 配置指纹 ‖ 格式版本 ‖ 分析参数)`
- P4-4 L3 表现层缓存：`Digest(ViewModel 指纹 ‖ 渲染器版本 ‖ 语言)`
- P4-5 配置指纹只含影响金额部分（pricing overrides + exchange_rate + 标准表 stamp），排除 `listen` 等
- P4-6 分析参数指纹含改变取样口径的全部 flag（`-lang`、taskseg profile、自流量排除集、LLM identity 等）
- P4-7 `-no-cache` 常驻旁路（非过渡开关）
- P4-8 详单指纹携带 `(record, manifest, prev)` 身份 + 渲染器版本 + 语言
- P4-9 L2 命中的全量运行仍执行 orphan 清扫 + compares 索引重建
- P4-10 冷热一致性测试（浮点 `1e-6` 容差）

### 1.C §9 测试守卫清单

既有守卫的去处：
- G9-a `internal/story/golden_test.go` 下沉到 ViewModel 层（比对 VM 结构）
- G9-b `structure_test.go` 的 `LosslessReconstruction` 改写为真自包含断言（只给 `j-<id>.json`）
- G9-c `internal/report/e2e_test.go` 保留 + 迁移期 "切片并集 ≡ 旧 `Report2` 字段集" 等价断言
- G9-d 详单在宏观报告与 Journey 渲染之间字节一致 + 指纹携带渲染器版本
- G9-e `cmd/vmr/i18n_e2e_test.go` 保留 + "ViewModel 无未 i18n 裸字面量" 守卫
- G9-f `archtest` 行数预算登记 `viewmodel_*.go`；`section_*.go` 条目随删除移除
- G9-g `archtest` i18n 配对对象改 ViewModel 构建器
- G9-h `archtest` 新增：`internal/server` 不 import 分析半区任何包

新增守卫（方案 §9 逐条）：
- G9-1 `-render-only` 产物与全量运行字节一致
- G9-2 缓存命中路径与冷启动路径产物一致（浮点 `1e-6` 容差）
- G9-3 跨语言格式化 fixture（`fmt_cases.json` 由 `fmtutil` 与看板 JS 各跑一遍）
- G9-4 切片内每个时间点必须同时有 `ts` 与 `ts_display`，缺一即失败
- G9-5 骨架页版本探测逻辑写成纯函数，随看板 JS Node 测试覆盖
- G9-6 `bodies` 表无孤儿、无悬引用（双向查）
- G9-7 工具配对三级 `match` 与决策脊柱实际渲染一致
- G9-8 更窄时间窗重跑，`journeys/details/` 无上一次孤儿文件残留
- G9-9 orphan 清扫不动 `compares/`、`requests/details|evidence/`
- G9-10 `compares/index.json` ≡ `compares/*.json` 目录内容

### 1.D §11.1 不变量

- INV-1 两半区一条契约：`report`/`journey`/`ctxgraph`/`taskseg`/`chatmsg`/`reqdetail` 不 import `router`/`server`/`config`；`server` 不 import 分析半区；不建跨包共享胖 ViewModel
- INV-2 内容寻址底座不可动摇：缓存判据只能是内容哈希，绝不回退文件时间启发式
- INV-3 权限底线：所有产物 0600 / 目录 0700（含骨架页、缓存目录）
- INV-4 单一时区权威：数据层 `ts` + `ts_display` 双字段，`ts_display` 经 `fmtutil.DisplayZone`；前端不做二次换算
- INV-5 不新增双账本：manifest 不放指标，切片间不交叉引用数值，前端不重算已在 JSON 里的量

### 1.E §11.2 已知取舍（可挑战）与 §11.3 顺带机会

§11.2 取舍（本轮逐条判断是否仍成立、是否值得挑战）：不做切片级缓存隔离 / Markdown 不用模板引擎 /
不支持跨语言 `-render-only` / `requests/details/*.md` 永不进 render-only / 不引图表库 / 专用延迟分位图元 /
二进制版本不进缓存指纹 / journey JSON 携带正文 / 废弃自包含 HTML 与 `-redact` / 删除人读请求索引 /
报告产物无兼容期 / 单一渲染路径 / ViewModel 不落盘 / 看板 `#` 传参 / `file://` 不支持 /
托管目录不走 rundir / 详单 `r-` 前缀 / `-partial` 不进文件名 / 逐任务详情不进 manifest /
`compares/` 有索引但不进 manifest / `ctxgraph` md5 底座不改 / 版本探测进骨架页样板 /
版本不一致只警告不阻断 / 切片不带独立版本戳

§11.3 顺带机会：Phase 1 序列化写成流式（逐切片构建、写完即释放）以削 RSS 峰值。

### 1.F 历史评审遗留问题全集（T / F / N / G / C / N-B 系列）

> 逐条列出**问题本身**，不含历史文档给出的处置结论。核验时确认现行代码是否已妥善处理，
> 或是否存在被历史评审误判 / 遗漏 / 引入新问题的情形。

**gemini-3.8-flash 评审：**
- T1 `vmr-report.json` 单体保留与 D2 冲突（代码与文档割裂）
- T2 Digest 链构造函数存在 report/journey 两份拷贝
- T3 `docs/VirtualModelRouter_Design_v4_Analytics.md` 大面积过期（§0/§2 全段）
- T4 方案提案文档头部状态过期
- T5 "ViewModel 无未 i18n 裸字面量" 守卫未写成自动化测试
- F2 `.parse-cache/` 未归一为 `.cache/parse/`
- F3 L2 分析参数指纹缺 `llm_key` 自流量 tag 与 LLM identity
- F4 `commitManifest` 静默吞掉 BuildManifest/WriteManifest 错误
- F5 `internal/report/requests.go` 包注释仍描述已删除的 `vmr-requests.md` 全家
- F6 `KNOWN_ISSUES` 多处旧命令/旧路径引用；2.79/2.56 陈旧
- F7 `CHANGELOG` 关于单体删除表述不准确
- F8 `viewmodel.go` 头部 "过渡期双路径" 注释失效
- F9 README 双语中 `vmr story`/`vmr report`/`-corpus` 残留（含不可运行示例命令）
- F10 3C 移交清单 vm 前缀 helper 改名回退与 `vmSkippedAttemptsNote` 去重未执行
- F11 跨多次运行累积产物（compare/journey）换语言时 Markdown 中英混排
- F12 看板 JS `FmtCurrency` 恒 `$` 前缀
- G2 `internal/report` 的 `Format = 11` 与 `ManifestFormat = 11` 是两个独立常量
- G3/G4/G5 —— 二次执行轮的小发现（§2.5 详单豁免边界、zh golden fixture、`computeTargetL2` 隐式约定）

**minimax-m3 v2 评审：**
- v2-T1 Compare 产物保留 `-partial` 后缀
- v2-T2 遗留 `story`/`corpus` 代码符号与文件名
- v2-F1 L2 缓存命中全量运行跳过 orphan 清扫
- v2-F2 `rows.go`/`aggregate.go`/`i18n/story_render.go` 三处过期注释

**claude-sonnet-5 评审（N1–N19）：**
- N1 `common.js` 未部署 → 所有看板页渲染失败（`WriteSkeletons` 只写 `.html`）
- N2 `llm_interpretation` 未入 journey JSON，`-render-only` 靠 `strings.Index("## LLM ")` 刮取旧 md
- N3 `requests.go` ~250 行旧 Markdown 请求索引渲染死代码 + `report_requests.go` 死文案
- N4 渲染产物中指向 `stories/` 的失效链接（会话表、journey 回链、report 回链层级错）
- N5 `compares/index.md` 硬编码英文，不跟随语言
- N6 `cmd_journey_setup.go` 保留读旧 `stories/vmr-stories.json` 的兼容 shim
- N7 `WriteRequestsIndex` 顶层 `vmr-requests.json` 兼容副本死分支
- N8 `ValidateManifest` 不校验 8 份切片是否齐全，只校验已记录的
- N9 ~200 处注释 + test 函数名仍用 `vmr story`/`vmr report`/`-corpus`
- N10 骨架页 chrome 本地化不一致（macro 中文 / request-browser 英文，均不响应数据语言）
- N11 `resolvePricingFingerprint` 未逐字段核对是否只含影响金额部分
- N12 方案 §7.2 提 "时间窗 -from/-to"，`vmr analyze` 无此 flag
- N13 `RequestRow` 缺 `ts_display` → request-browser Time 列空白
- N14 Go 1.26 尾随注释 gofmt 差异（`go.mod` 声明 1.25.1）
- N15 6 个骨架页 JS 普遍按 PascalCase 读切片 → 数据大面积为空
- N16 `vmr-report.md` §8 附录与详单回链指向已删的 `vmr-requests.md`
- N17 本地 `config.yaml` endpoint group 旧语法 → 触发定价层降级
- N18 `-render-only` L3 缓存命中直接信任磁盘产物，手改 `.md` 后 warm render-only 不修复
- N19 旧二进制快照（JSON 无 `llm_interpretation`）在新二进制 `-render-only` 会丢 LLM 段（无兼容期立场）

**claude-sonnet-5 评审 C 节改进机会：**
- C-imp-1 流式序列化削峰值内存（`WriteMacroSlices` build-marshal-release）
- C-imp-2 `bodies` blob key 用短哈希前缀（16 hex 而非 64）
- C-imp-3 `clientsWithSiblingFile` 恒空分支彻底移除
- C-imp-4 `structureExcerptChars = maxBodyExcerptChars` 别名消除
- C-imp-5 `request-browser.html` 排序键（`ts` RFC3339 字符串排序错位）

**claude-sonnet-5 v4 评审 T-A..T-F / N-B1..N-B7：**
- T-A 注释与测试符号里的历史命令称谓（55 个 Go 文件）
- T-B `VirtualModelRouter_Design_v4_Analytics.md` §4（i18n 一节）仍描述 `section_*.go`
- T-C 看板 SVG 图元 "散点" 未按字面实现
- T-D `WriteRequestsIndex` 残留一处路径归一 fallback
- T-E 工作区 `config.yaml` endpoint group 旧语法（= N17）
- T-F 观察/文档脚注项（N11 / N12 / N18）
- N-B1 journey `.md` 决策脊柱 `→ [详情]` / System-Prompt 证据链接因目录拓扑下沉全部 404（`../details/` 应为 `../../requests/details/`），且被两个守卫测试用错误期望前缀 "锁定"
- N-B2 `journeys/index.md` H1 曾为 "VMR Story Index"（退役术语泄漏进渲染产物）
- N-B3 `requests/failed.md` 引子指向已删的 `vmr-requests-<tag>.md`，自称 "额外索引"
- N-B4 zoom 模式（`-compare`/`-journey`）L2 缓存命中不校验主产物存在，产物被删后静默不重建
- N-B5 `macro/summary.json` 的 `success_rate` 被 round2 截断，看板与报告显示不同成功率
- N-B6 `macro/finance.json` provider 成本非逐字节确定（Go map 遍历顺序 → FP 加法非结合）
- N-B7 `RendererVersion` 未随 N-B1 渲染层改动 bump → 旧快照 `-render-only` L3 命中保留旧链接

---

## 第二部分 · Review 执行与代码核验详情

### 2.0 基线

- `go version`：go1.26.5 darwin/arm64（`go.mod` 声明 `go 1.25.1`，CI 矩阵用 1.25）
- `go build -o vmr ./cmd/vmr`：通过
- `go test ./...`：**全绿（38 包）**，`internal/archtest` 单独复跑通过
- 代码基线 `050ad25..HEAD`：105 commits，289 文件，+23317 / -13150。`050ad25` 确认为首个重构提交 `7abf0a4` 的父提交。

### 2.1 验证手段

- 逐条 D1–D21 / 每个 N-finding：回到源码 + 测试断言取证（`rg` 精确定位），不采信历史文档 claim。
- 真实日志端到端实跑：`./vmr analyze -c config.yaml -o /tmp/vmr-verify -details logs/vmr-audit-2026-08-24.jsonl.zst`（519 records，15 journeys，1 断头跳过），另跑 `-benchmark`、`-lang en`、`-render-only`、`-no-cache`。
- 产物拓扑 / 权限 / manifest / 切片 schema / 链接可解析性逐一比对方案 §4 与 §3.2/§3.3。
- 看板守卫 `go test ./internal/dashboard/`（`TestAllDashboardPages_ReadSnakeCaseFields` + `TestJS_DashboardRenderSmoke`）全绿。

### 2.2 逐组核验结论（独立取证）

| 组 | 结论 | 关键证据（本轮独立核实） |
|---|---|---|
| **D1–D21 核心裁决** | ✅ 全部落地，无数据正确性 / 安全模型错误 | D2：`rg vmr-report.json --type go -g '!*_test.go'` 仅剩 4 处注释，零写出代码；宏观渲染源 `LoadReport` 从切片重装 `Report2`。D3：`rg text/template` 零命中。D6/D15：`render_html*`/`toolwaste_html`/`story/assets` 零残留；无 `-redact`/`-html` flag。D8/T2：`internal/report/digest.go` 已删，`internal/digest` 叶子包唯一实现，`digest_parity_test.go` 已退役。D9：`server/reports.go` `len(APIKeys)==0` → 全树 403。D11：`-render-only` 与全量运行产物 `diff -rq` **逐字节一致**（真实数据，含 benchmark zoom 后）。D17：`detail_file` = `r-<ts>_..._h8.md`。D18：`bodies` 顶级去重表 + 三级 `match`（实测 exact/normalized/positional 混合）+ `resp_ref` 不截；`llm_interpretation`/`llm_divergence` 入 JSON，`cmd_render_only.go` 的 `strings.Index("## LLM ")` 刮取 hack 已删。D19：`j-<id>.md` 无 `journey-` 前缀、无 `-partial`。D20：`CleanOrphanJourneys` 严格限 `journeys/details/`，L2 命中路径也清扫。D21：`compares/` 不进 `AllSlicePaths`，每次 analyze 扫目录重建。 |
| **Phase 1 数据层** | ✅ | 五 macro 切片 + `requests/index.json` + `journeys/index.json` 齐备；`manifest.json` format=11 最后写；`generated_at` 双字段；`footnotes`(6)+`disclaimers`(3) 注册表；`requests/index.json` 行含 `ts`(epoch ms `1787500778429`)+`ts_display`+`detail_file`+`sessions`(50)+`journey_link`(15)；`summary.highlights`(2) 非空；`finance.cost_coverage{unpriced_count,incomplete_rate_count,degraded_estimate_pct}` + `pricing.currency=USD`。 |
| **Phase 2 看板** | ✅（一处 schema 瑕疵见 NEW-A） | 6 骨架页根级平铺，`common.js` 内联（`rg 'src="common.js"'` 零命中），`EXPECTED_MANIFEST_FORMAT=11` == Go `ManifestFormat=11`，`svgLatencyPlot` 存在，`file://` 降级分支在位，chrome 统一英文，`CURRENCY_SYMBOLS` + `setCurrency`。`TestAllDashboardPages_ReadSnakeCaseFields` 通过——但它是 denylist 而非全量扫描（见 NEW-A）。 |
| **Phase 3 ViewModel / 单一渲染路径** | ✅ | `renderAllFromDisk` 全量与 render-only 共用；`viewmodel_*.go` ↔ `i18n/report_*.go` 配对 + `vm_literals_test.go` AST 守卫；golden 下沉 VM 结构（`viewmodel_golden_data_test.go`）；`structure_test.go` 的 `LosslessReconstruction` 只喂 `j-<id>.json`；`-lang` 与 manifest 不一致拒绝。EN 变体实跑：全人读产物英文，用户 prompt 内容按 passthrough 不译（正确）。 |
| **Phase 4 缓存** | ✅ 主体（一处失效矩阵缺口见 NEW-D） | L1 `.cache/parse/`；L2 `computeTargetL2 = Digest(inHashes ‖ pricingFP ‖ ManifestFormat ‖ paramsFP)`；L3 `ComputeL3Digest`；`paramsFP` 含 Lang/TaskProfile/IncludePartial/IncludeSelfTraffic/SelfTrafficTags/LLMSelfTag/LLMAddr/LLMModel/DisplayCCY/RenderAll/Details/Mode；LLM identity 仅在 journey:/compare: mode 入指纹；`-no-cache` 常驻；`zoomArtifactMissing` 校验（N-B4）；provider 成本 `round6`（N-B6）；`success_rate` 全精度（N-B5，实测 summary.json = `0.9884393063583815`，§0 显示 98.8%，一致）。L2 命中实测（"L2/L3 缓存命中"提示）。 |
| **§9 守卫 + §11 不变量** | ✅ | INV-1：`rg '"vmr/internal/(router\|server\|config)"'` 在分析半区非测试文件零命中；`archtest` 全绿。INV-3：实测 vmr 自建目录 700 / 文件 600（预先 `mkdir` 的顶层目录保留 755——非 vmr 行为）。§9 新增守卫逐条具名在位。 |
| **T/F/N/G/C/N-B 历史遗留** | ✅ 已处理（3 处发现见第四部分）| N-B1 实测已修：journey `.md` 链接为 `../../requests/details/`、`../../requests/evidence/`，从 `journeys/details/` 可解析。N-B2：`journeys/index.md` H1 = "VMR Journey 索引" / "VMR Journey Index"。N-B3：`failed.md` 引子指向 `requests/index.json`。N17/T-E：**当前 `config.yaml` 已迁移**，`./vmr check -c config.yaml` 通过（v4 评审时的旧语法已不复存在）。 |

**独立总判断**：方案四个 Phase + D1–D21 已**实质、正确落地**。以 `050ad25..HEAD` 累积效果独立核验（回代码取证、真实数据实跑、未采信任何历史文档 claim），
**未发现数据正确性、缓存失效逻辑、安全模型层面的错误**；`-render-only` 与全量运行逐字节一致，冒烟产物拓扑与 §4 逐字匹配。
历史 5 份评审 + 数轮迭代的工作扎实，本轮独立取证大体证实其结论。**新发现 5 项问题，全为低/中 severity，其中 1 项当场直接修复。**

---

## 第三部分 · Review 事项完成度总结

### 3.1 基于方案的全部 Review 事项列表与状态

| 模块 / 组 | 事项 | 状态 | 依据 |
|---|---|:---:|---|
| 核心裁决 | D1–D21（21 项） | ✅ 全部完成 | 见 2.2 逐条证据 |
| Phase 1 | 概念归一 / CLI 收敛 / 五切片 / §3.3 七类现算事实下沉 / 拓扑归位 / `r-` 前缀 / D19 / 提交顺序 + orphan 清扫 / compares 索引 / 请求索引删除 | ✅ 全部完成 | 拓扑实跑逐字命中；切片 schema 完整 |
| Phase 2 | go:embed 骨架 / 内联 SVG 四图元 / 六页 + `#data=` + file:// / 版本 banner / 前端 fmt fixture / 旧 HTML 删除 / `/reports/` 托管安全 / snake_case 字段 / common.js 内联 / chrome 一致 / 币种符号 | 🟡 部分完成 | 全部功能落地；`chatmsg.Usage` 子对象 PascalCase 泄漏进 journey 切片 schema（NEW-A，低-中） |
| Phase 3 | report/journey/compare 三侧 ViewModel / 固定序列化器 / golden 下沉 / `-render-only` 覆盖面 + 语言继承 / AST 裸字面量守卫 | ✅ 全部完成 | 逐字节等价实测；EN 变体实跑 |
| Phase 4 | 整套产物指纹 + 失效矩阵 / L1-L3 / 配置指纹 / 分析参数指纹 / `-no-cache` / 详单指纹 / L2 命中清扫 + compares 重建 / 冷热一致 | 🟡 部分完成 | 主体落地且测试充分；L2 失效矩阵缺 `vmr-quota.json` 一路输入，配额记账在 L2 命中时冻结（NEW-D，中） |
| §9 守卫 | 既有迁移（8 项）+ 新增（10+ 项） | ✅ 全部完成 | 逐条具名在位；`TestAllDashboardPages_ReadSnakeCaseFields` 是 denylist（覆盖面有限，见 NEW-A） |
| §11.1 不变量 | INV-1..INV-5（5 条） | ✅ 全部完成 | archtest 强制 + 实测 |
| 历史遗留 | T1–T5 / F2–F12 / G2 / v2-T1/T2/F1/F2 / N1–N19 / C-imp-1..5 / T-A–T-F / N-B1–N-B7 | ✅ 全部完成 | 见 2.2 末行；本轮独立复核证实（3 处未尽见第四部分） |

### 3.2 部分完成事项详述

见第四部分 NEW-A（Phase 2 schema 瑕疵）与 NEW-D（Phase 4 失效矩阵缺口）——两者均为本轮**新发现**，非方案原案要求未落地。方案 D1–D21 + Phase 1–4 的**原案要求项无一未完成**。

---

## 第四部分 · 过程新发现问题与处理

### 4.1 汇总清单

| 编号 | 问题 | 严重度 | 处置 |
|---|---|:---:|:---:|
| **NEW-A** | `journeys/details/j-<id>.json` 的 step `usage` 子对象序列化 Go 字段名（`In`/`Out`/`CacheRead`/`CacheWrite`/`Reasoning`），与其余全 snake_case 的切片 schema 不一致；N15 守卫（denylist）未覆盖 | 低-中（一致性 / 守卫盲区，无功能破坏） | ⚖️ 记录待裁决（建议修） |
| **NEW-B** | `story→journey` 更名遗漏 "story half" 描述短语：9 处 doc 注释 + `dispatchDefaultSuite` 的 `fmt.Errorf("analyze (story half)")`（唯一用户可见），T-A/N9 曾声称"全仓 55 文件已清理" | 低（一致性；一处用户可见错误串） | ✔️ **已直接修复**（commit `8a35c0b`） |
| **NEW-C** | `cmd/vmr/cmd_journey_setup.go` 在 Go 1.26.5 下非 gofmt-canonical（尾随注释对齐规则变化）；`go.mod` 声明 1.25.1，CI 用 1.25 | 极低（工具链版本差；CI 绿） | 仅记录，不修（属"仓库升级 Go 1.26"专项） |
| **NEW-D** | L2 缓存命中时 `macro/finance.json` 的 provider 配额区块被冻结在上次全量运行时刻——L2 指纹不含 `vmr-quota.json` 内容哈希，也不含 wall-clock；同批日志隔时重跑（配额周期滚动 / 其间路由过流量）→ §2.5 账户消耗表与 `finance.json` 给出过期的配额进度与已用量 | 中（分析半区展示视图失准；路由半区权威记账不受影响） | ⚖️ 记录待裁决（建议修） |
| **NEW-E** | `_eval/calibrate_p1b.go`（tracked，但 `_` 前缀目录 → `go build/test ./...` 与 CI 均不含）仍用 `story` import 别名 + `internal/story/llm_findings.go` 路径引用 | 极低（不编译、不测试、不发布的校准脚本） | 仅记录 |

### 4.2 详述（问题、根因、建议方案、ROI）

#### NEW-A：journey 切片 step `usage` 子对象 PascalCase 泄漏

- **问题描述**：`internal/journey/structure.go` 的 `StepStructure.Usage chatmsg.Usage json:"usage"` 内嵌 `chatmsg.Usage`，而 `chatmsg.Usage` 的字段（`In, Out, CacheRead, CacheWrite, Reasoning`）**无 json tag**。落盘的 `j-<id>.json` 里每个 step 的 `usage` 因此形如 `{"In":53182,"Out":629,"CacheRead":44800,"CacheWrite":0,"Reasoning":532}`——与切片 schema 其余部分（全 snake_case）不一致。方案 D12/§5.6「前端直接消费领域切片 raw 值」+ §6.2「零编译扩展：复制骨架页指向切片即可定制」意味着切片 schema 是第三方消费契约；自写看板按 `usage.in` 读会得到 `undefined`。
- **根因**：`chatmsg.Usage` 是 leaf 包共享类型（`ctxgraph.Manifest.Usage` 也内嵌它，序列化进 `.cache/parse/*.json`），历史上从未加过 json tag；D18 把它新纳入了用户可见的 journey JSON 契约，但未处理字段名规范。N15 的守卫 `TestAllDashboardPages_ReadSnakeCaseFields` 是**具名坏串黑名单**而非通用扫描，`usage.In` 不在名单内；仓内自带的 `journey-viewer.html:325` 恰好按 PascalCase 读（`usage.In || 0`），所以**当前无功能破坏**，纯属潜在不一致 + 守卫盲区。
- **建议方案**（推荐 A）：
  - **A**：给 `chatmsg.Usage` 字段加 json tag（`in`/`out`/`cache_read`/`cache_write`/`reasoning`）。Go unmarshal 大小写不敏感回退，旧 `.cache/parse` 与旧 journey JSON 仍可读入，无需 bump 解析器版本。连带：`journey-viewer.html` 改读 `usage.in`/`usage.out`；N15 守卫 denylist 增 `usage.In`/`usage.Out`；`UPDATE_GOLDEN=1` 重生 `internal/journey`（及可能 `internal/ctxgraph`/`internal/report`）golden，逐一 diff 确认仅键名变化。机械但跨 2–3 包 golden。
  - **B**：仅在 `structure.go` 用本地 `stepUsage` 结构体（snake_case tag）替换内嵌 `chatmsg.Usage`，build 时转换。blast radius 收敛到 journey 包，但多一个平行类型。
- **ROI**：中低。当前不影响任何产物正确性与自带看板；收益是切片契约一致性 + 补上守卫盲区，利于第三方看板与未来维护者。建议作为一次独立小改动（含 golden 重生）。

#### NEW-D：L2 缓存命中时配额记账冻结

- **问题描述**：`computeTargetL2`（`cmd/vmr/cmd_analyze_cache.go`）的输入哈希集 = `report.ComputeInputHashes(r.paths)`，**仅含审计日志文件**。报表 §2.5「账户消耗与额度」表与 `macro/finance.json` 的 `provider_quotas` 区块的数据来自 `<log_dir>/vmr-quota.json`（脚注 ² 明写"实时计数器"），其 `period_elapsed_pct` 更是 wall-clock 的纯函数。二者都不在任何缓存指纹里。实测：对同一日志隔 6 分钟两次 `vmr analyze`，第二次 L2 命中（"L2/L3 缓存命中，产物已是最新"），`finance.json` 的 `period_start`/`period_ends_at`/`period_elapsed_pct` 与已用量**冻结**在首次运行时刻。config.yaml 有 `1min`/`1h`/`1d`/`5h`/`1mo` 多种配额窗口，`1min`/`1h` 窗口在两次运行间必然滚动。
- **根因**：方案 §7.0 第 3 条明确点名的失效模式是"配置变更不改日志，但报表金额全变了"；§7.2 的判据是"改了它会不会改变任何一个落盘数值或人读文本——会，就进指纹"。`vmr-quota.json` 内容变化**正是同一形状**（改变落盘数值 + 人读文本），但 §7.4 失效矩阵从未列出配额文件这一路输入，实现也就没纳入。
- **影响面**：仅分析半区的展示视图。路由半区的 `quota.Registry`（`vmr-quota.json` 的写方）是权威记账，路由决策不受影响。但 §2.5 的定位就是"这个套餐/代理买得值不值 / 是否快撞上限"——过期答案对这一决策有实际误导。
- **建议方案**：
  - **A（推荐，契合 §7.2 原则）**：报表读取 `vmr-quota.json` 时把其内容 sha256 并入 L2 输入哈希集。副作用：活跃使用中（vmr 持续路由）L2 几乎不命中——但那种场景报表本就应重算；§7.1 点名的 L2 受益场景"反复重跑的开发循环"通常在分析历史日志、配额不动，L2 仍命中。
  - **A'（更彻底）**：把 `period_elapsed_pct` 这类"as-of-now"派生值从切片里剔除，切片只存 `period_start`/`period_ends_at`（事实），渲染侧按 `manifest.generated_at` 或 live now 现算 elapsed%——符合 D12「只是原样透传的字段不该存在」。已用量仍需 A 的指纹修法。
  - **B（低成本兜底）**：在 `KNOWN_ISSUES` 登记为已知局限——L2 命中时配额记账是"上次全量运行时的快照"，要实时数据用 `-no-cache` 或路由半区 `/status`；§2.5 表脚注注明快照时刻。
  - **C（不推荐）**：配额区块每次运行现算、绕过 L2——违反"整套产物一个指纹"。
- **ROI**：中。修 A 约 1 处指纹入参 + 1 个定向测试；A' 另需触及切片 schema + 渲染侧 + golden；B 是几行文档。是否值得取决于对"L2 命中时配额可陈旧"的容忍度——如果 §2.5 是用户实际盯的表，建议 A。

### 4.3 关于 §11.2 已知取舍的挑战

逐条复核 §11.2 的 24 条取舍，**均仍成立、无一值得推翻**：论证充分，且本轮实测（逐字节等价、拓扑匹配、安全模型）未提供任何反证。§11.3 顺带机会（流式序列化削 RSS）已由 K-01（`WriteMacroSlices` build-marshal-release）落地。

### 4.4 关于 sub-agent 并行的评估

Phase A 为深度串行的源码核验，无正交可并行任务集；唯一直接修复（NEW-B）体量极小。**未启用 sub-agent，主控串行完成**——符合多 agent 指南 §6.1「不值得并行则主控直接做」。

---

## 第五部分 · 真实数据与真实 LLM 端到端验收测试（Phase B）

Phase A 结论：方案 D1–D21 + Phase 1–4 实质正确落地，仅 2 项**新发现**（NEW-A 低-中 / NEW-D 中）待裁决，
1 项当场修复（NEW-B）。**无阻塞性大问题**，据此执行 Phase B 端到端验收。

### 5.1 执行环境

- **二进制**：本轮重建 `go build -o vmr ./cmd/vmr`（含 NEW-B 修复，commit `8a35c0b`）。
- **真实 LLM**：`report.yaml` → `http://192.168.0.22:8800/v1/`，model `cheap`，key `sk-xxx…vmrstory`。
  实测 `/health` 200、`/v1/models` = `[agent, cheap, coding, fast]`、`cheap` chat 回 "pong"。**真实调用，非 mock**。
- **真实配置**：`config.yaml`（当前工作区版本，`./vmr check` 通过——N17 旧语法已迁移）。
- **真实日志**（`logs/`）：
  - 数据集 A（主快照）：`logs/vmr-audit-2026-08-{22,23,24,25}.jsonl.zst`（多日窗口，跨日聚合 / 多 journey lineage / workloads-by-date）
  - 数据集 B（单日，可比性 + 冷热一致）：`logs/vmr-audit-2026-08-24.jsonl.zst`（519 records，与历轮评审同源）
  - 数据集 C（大输入生存性）：`logs/vmr-audit-2026-07-16.jsonl.zst`（44 MB）
- **产物目录**：`reports/`（数据集 A，zh，主）；`reports-en/`（数据集 A，en，抽测）；`/tmp/vmr-b-*`（B/C 及对照）。
  权限基线 0600/0700。`reports/` 与 `reports-en/` 均 gitignore。

### 5.2 验收用例（Action Plan）

| UC | 命令要点 | 验收点 |
|---|---|---|
| **B-01** 默认套件（zh，多日） | `analyze -c config.yaml -report-config report.yaml -lang zh -o reports -details <A 4 日志>` | §4 拓扑 + 0600/0700；manifest format=11 最后写 + 8 slice sha256；五 macro 切片字段完整（highlights/footnotes/disclaimers/cost_coverage 非空）；`requests/index.json` ts=epoch ms + ts_display + sessions + journey_link；跨日 workloads/by_date；`details/r-*.md` |
| **B-02** Benchmarks（zh） | `analyze … -o reports -benchmark <A>` | `journeys/benchmarks.{json,md}` 生成并进 manifest；指标分布 / Finding 检出率 / Spearman；provenance 随 zoom 保留 |
| **B-03** 单 Journey + 真实 LLM（zh） | `analyze … -o reports -journey <id> -llm-addr … -llm-model cheap -llm-key … -llm-cache-dir reports/.cache/llm <A>` | 真实调用 `cheap`；`j-<id>.json.llm_interpretation`（model/scope=""/status/duration_ms/text）；`.md` 渲染 `## LLM 解读（模型：cheap）`；zoom 不动兄弟 journey |
| **B-04** 双 Journey A/B 对比 + 真实 LLM（zh） | `analyze … -o reports -compare <a>,<b> -llm-addr … -llm-cache-dir reports/.cache/llm <A>` | `compares/compare-*.{json,md}` + `compares/index.{json,md}` 重建；`llm_interpretation` + `llm_divergence` 两 record 入 JSON；`.md` 两段从 record 渲染；`compares/` 不进 manifest；验证 B-03 的 journey `llm_interpretation` 不被对比操作冲掉（NEW-03 历史回归面） |
| **B-05** render-only（zh） | `analyze -o reports -render-only -no-cache` | 与 B-01..04 累积产物**逐字节一致**（除 `.cache/`）；语言继承；骨架幂等刷新；LLM 段从 JSON 恢复不刮 `.md` |
| **B-06** 英文变体（抽测） | `analyze … -lang en -o reports-en -benchmark <A>` + 默认套件 | 全人读产物英文；`Code` 跨语言稳定；用户 prompt 内容 passthrough 不译 |
| **B-07** 冷热一致 + `-no-cache`（数据集 B） | `analyze … -o /tmp/vmr-b-single <B>` ×2（一次 L2 命中，一次 `-no-cache`） | 除 `.cache/` / `generated_at` / 配额 wall-clock 字段（NEW-D）外逐字节一致 |
| **B-08** 看板真实数据渲染 | Node 沙箱喂 `reports/` 真实切片渲染 6 页/8 模式 | 关键单元格非空 / 非 `—` / 无 `undefined`/`NaN`/`[object Object]`；金额随币种；`#data=` 相对路径可解析；`common.js` 内联 |
| **B-09** 大输入生存性（数据集 C） | `analyze … -o /tmp/vmr-b-big <C 44MB>` | 不 OOM、不崩、产物完整 |
| **B-10** 内容复核 | 人读 `vmr-report.md` / 一个 `j-*.md` / `benchmarks.md` / `compare-*.md` / `failed.md` / `journeys/index.md` | 无失效链接、无空段、无中英混排、数字自洽（合计行 / 覆盖率 / 置信度标记）；LLM 段落质量 |

### 5.3 执行记录

**数据集**：`logs/vmr-audit-2026-08-{22,23,24,25}.jsonl.zst`（120 + 53 + 519 + 885 = 1577 records，
排除 56 条自指流量 → 1521；46 candidate journeys，40 渲染，1 断头跳过）。
真实 LLM：`http://192.168.0.22:8800/v1/` model `cheap`（真实调用，非 mock）。

| UC | 结果 | 关键观察 |
|---|---|---|
| **B-01** 默认套件（zh，4 日） | ✅ 通过 | §4 拓扑逐字命中；权限全 0700/0600（vmr 自建目录链每级 0700）；manifest format=11 最后写、7 slice sha256；`by_date` 跨日 74/52/518/877 = 1521 与 `overall.requests` 一致；`summary.overall`：`success_rate=0.9809335963182118`（§0 显示 98.1%，一致）、`highlights`(2)、`meta`（records=1577 / self_traffic_exclusion_active）；`finance`：`cost_coverage`、`pricing.currency=USD`、`standard_generated_at`；`requests/index.json` 1521 行，`ts`=epoch ms（`1787331209378`）+ `ts_display` + `detail_file`(`r-` 前缀) + `sessions`(50) + `journey_link`(15)；附录如实披露"排除 56 条自指流量（1577 → 1521）"。 |
| **B-02** Benchmarks（zh） | ✅ 通过 | `journeys/benchmarks.{json,md}` 生成并进 manifest（8 slices）；45 journey 分析；14 类指标分布 + 5 条 Finding 命中率 + 33 组 Spearman 相关；均值敏感性免责 + "本批语料 0.0% Anthropic Messages → 部分检测器测不出来" 显式披露。**发现 NEW-F**（H1 术语，已修）。 |
| **B-03** 单 Journey + 真实 LLM（zh） | ✅ 通过 | `-journey j-pimini-…0357ba6c`（7 任务 133 轮）；真实调用（evidence pack 52405 chars / ~13101 tok）；`j-<id>.json.llm_interpretation` = `{model:cheap, status:ok, duration_ms:5910, text:…}`（无 scope，正确）；`.md` 渲染 `## LLM 解读（模型：cheap）` + 免责语；`reports/.cache/llm/` 6 文件。解读正文切题、连贯。 |
| **B-04** 双 Journey 对比 + 真实 LLM（zh） | ✅ 通过 | 2 次真实调用（overall ~12589 tok + divergence ~193 tok）；`compare-*.json` 落 `llm_interpretation`（scope=overall, duration_ms=6544）+ `llm_divergence`（scope=divergence, duration_ms=3379）；`.md` 两段 `## LLM 解读（模型：cheap · 整体对比 / · 分叉点）`；`compares/index.{json,md}` 重建；`compares/` 不进 manifest。 |
| **NEW-03 回归** | ✅ 通过 | 对 B-03 的 journey `0357ba6c` 再跑一次 `-compare`（含它），其 `llm_interpretation` **仍完整保留**（status ok，text 1700 字）——`a75e57e` 的磁盘既有状态保护在真实数据下成立。 |
| **B-05** render-only（zh） | ✅ 通过 | B-01..04 累积后（含 benchmark + 2 组 compare + 2 处 LLM record）`-render-only -no-cache` 产物与源 `diff -rq` **逐字节一致**（除 `.cache/`）；LLM 段从 JSON 恢复，`cmd_render_only.go` 无 `## LLM` 刮取。 |
| **B-06** 英文变体（抽测） | ✅ 通过（发现 1 处，已修） | 默认套件 + benchmark，`-lang en`；`manifest.lang=en`；`vmr-report.md` / `journeys/index.md` 全英文 chrome，用户 prompt 内容 passthrough 不译（正确）。**发现 NEW-F**：`journeys/benchmarks.md` H1 = "Journey Corpus Report" —— 退役术语泄漏进产物标题，已直接修复。 |
| **B-07** 冷热一致 + `-no-cache`（单日 08-24） | ✅ 通过 + 发现 NEW-D | L2 命中实测（"L2/L3 缓存命中"提示）；`TestAnalyzeCache_ColdWarmAndNoCache`（1e-6 容差）绿。**发现 NEW-D**：L2 命中时 `finance.json` 的 provider 配额区块（`period_start/ends_at/elapsed_pct` + 已用量）被冻结在上次全量运行时刻——`vmr-quota.json` 内容与 wall-clock 不在 L2 指纹里。 |
| **B-08** 看板真实数据渲染 | ✅ 通过 | Node 沙箱喂 `reports/` 真实切片：`summary`/`finance`/`reliability`/`context-efficiency`/`requests` 全 snake_case、类型正确；`success_rate` 全精度；`FmtCurrency(1.23,'CNY')` = `¥1.23`（F12）；`versionBehavior` 纯函数三分支正常；`benchmarks.journey_count`；journey `bodies` blob 表。**确认 NEW-A**：step `usage` 子对象键 = `[In,Out,CacheRead,CacheWrite,Reasoning]`（PascalCase，`journey-viewer.html` 恰好按此读，无功能破坏）。`TestAllDashboardPages_ReadSnakeCaseFields` + `TestJS_DashboardRenderSmoke` 全绿。 |
| **B-09** 大输入生存性（44 MB） | ✅ 通过 | `logs/vmr-audit-2026-07-16.jsonl.zst`（1815 records）：不 OOM、不崩、产物拓扑完整、manifest format=11、`-no-cache` 亦通过；耗时 ~2 分钟。 |
| **B-10** 内容复核 | ✅ 通过 | `vmr-report.md` §0–§8 + 附录结构完整、合计/覆盖率/置信度标记（⭐¹⚠️low-n）自洽、§8 指向 `requests/index.json` + `request-browser.html`（无 `vmr-requests.md`）；`journeys/index.md` H1 = "VMR Journey 索引"，16→46 候选分类分组正确；**1459 个 journey→详单/证据/index/report 导航链接全部可解析（0 断链）**——N-B1 在真实数据下彻底闭环；`compare-*.md` 结构完整、两段 LLM 解读渲染正常。唯一发现是 `benchmarks.md` H1 术语（NEW-F，已修）。 |

**Phase B 总判断**：方案落地的功能特性在真实多日日志 + 真实 LLM 下按预期生成。
产物拓扑 / 权限 / 字节一致性 / 缓存失效 / 语言继承 / 看板渲染 / LLM 解读持久化 / 跨命令数据保护
均无实质错误。**新发现 NEW-F（已修）；NEW-D 在此轮暴露（Phase A 已记录，待裁决）；NEW-A 再次确认（待裁决）。**

### 5.4 验收测试中发现的问题与处置

| 编号 | 问题 | 严重度 | 处置 |
|---|---|:---:|:---:|
| **NEW-F** | `journeys/benchmarks.md` 的 H1 渲染 "# Journey Corpus Report" / "# Journey 语料统计报告"——§2.2 明确 `corpus`→`Journey Benchmarks`，文件名/flag/包名都改了，唯独渲染产物标题没改；`benchmarks_test.go` 把旧标题当正确基线锁定（与 N-B1 同类的"守卫锁死错误基线"） | 低（术语；无功能破坏） | ✔️ **已直接修复**（commit `e18dba8`）：EN → `# Journey Benchmarks`，ZH → `# Journey 基准统计报告`，测试断言与 2 处 test 报错串同步更新 |
| **NEW-D** | （详见第四部分 4.2）L2 命中时配额记账冻结 | 中 | ⚖️ 记录待裁决（Phase A 已详述，Phase B 中在 B-07 暴露实证） |
| **NEW-A** | （详见第四部分 4.2）step `usage` 子对象 PascalCase | 低-中 | ⚖️ 记录待裁决（Phase A 已详述，Phase B 中在 B-08 二次确认） |

> Phase B 未发现任何**新增的**行为、数据正确性或安全层面的缺陷。NEW-A/NEW-D 均为 Phase A 已记录项在真实数据下的确证。

### 5.5 报告目录导览（`reports/` — 供人工复核）

> 主快照：`logs/vmr-audit-2026-08-{22,23,24,25}.jsonl.zst`（1577 records / 46 journeys / 1521 请求）· 语言 zh ·
> config 定价按标准价目表（`pricing.currency=USD`）· 含 B-02 benchmark、B-03/B-04 两组真实 LLM 解读。
> 英文抽测另存 `reports-en/`（同数据、`-lang en`、macro + benchmark，无 LLM/compare/journey zoom）。
> **本快照在 Phase C 全部术语清理完成后做过一次最终干净重生（见第六部分末）。**

| 路径 | 对应用例 | 人工复核要点 |
|---|---|---|
| `reports/manifest.json` | 全套快照准入令牌 | `format=11`；`slices` 列 8 份（5 macro + journeys/index + benchmarks + requests/index）的 sha256；`generated_at` 双字段；`footnotes`(6)/`disclaimers`(3) 注册表；`time_range` 跨 08-22..08-25 |
| `reports/vmr-report.md` | B-01 宏观报告（§0–§8 + 附录） | 人读主入口。§2.5 账户消耗（标准价目表，无 quota 对照列时为降级）；§7 工具浪费；§8 指向 `requests/index.json` + `request-browser.html`；附录如实披露自指流量排除 56 条 |
| `reports/macro/summary.json` | B-01 总览切片 | `overall` 全指标 + `highlights` + `meta` 溯源；`success_rate` 全精度 raw（N-B5） |
| `reports/macro/finance.json` | B-01 成本切片 | `by_model`/`by_client`/`providers` + `cost_coverage` + `pricing.currency`；**provider 配额区块在 L2 命中时可陈旧（NEW-D）** |
| `reports/macro/{reliability,workloads,context-efficiency}.json` | B-01 可用性 / 负载 / 上下文效率切片 | 端点可用率、失败分类、按日/时段分布、会话膨胀、compaction、工具 schema 浪费 |
| `reports/requests/index.json` | B-01 请求明细机读单一真源（1521 行） | 每行 `ts`(epoch ms) + `ts_display` + `detail_file`(`r-` 前缀) + `req` 坐标；`sessions` / `journey_link` 投影 |
| `reports/requests/failed.{md,jsonl}` | B-01 排障入口（29 条） | 故障聚类；引子指向 `requests/index.json`（N-B3 修复后） |
| `reports/requests/details/r-*.md`（1521） | B-01 `-details` 单请求全量捕获 | journey `.md` 的 `→ [详情]` 链接目标（N-B1 修复后 1459/1459 可解析） |
| `reports/requests/evidence/{sysprompt,tools}-*.md` | B-01 内容寻址证据块 | 系统提示词正文 / 工具 schema，被宏观详单与 journey 脊柱共用 |
| `reports/journeys/index.{md,json}` | B-01 journey 候选集（46 条，H1 = "VMR Journey 索引"） | 挑一个 journey 看的入口；heartbeat 类默认折叠 |
| `reports/journeys/details/j-*.{json,md}`（40） | B-01 单任务叙事 | `.json` 自包含（`structure` tree + `bodies` blob + 三级 `match`）；`.md` 决策脊柱 + `## 行为指标` + Findings；`j-pimini-…0357ba6c` 是 B-03 带 `## LLM 解读` 的那条 |
| `reports/journeys/benchmarks.{md,json}` | B-02 群体基准 | 14 类指标分布 / 5 条 Finding 检出率 / 33 组 Spearman 相关；H1 = "Journey 基准统计报告"（NEW-F 修复后） |
| `reports/compares/compare-j-pimini-…9773d372-vs-…0824c1a6.{json,md}` | B-04 双任务 A/B 对照 + 真实 LLM | `.json` 带 `llm_interpretation` + `llm_divergence` 两 record；`.md` 两段 `## LLM 解读` |
| `reports/compares/compare-j-pimini-…0357ba6c-vs-…823df5a5.{json,md}` | NEW-03 回归验证用 | 对比操作后 A 侧 journey 的 `llm_interpretation` 未被冲掉 |
| `reports/compares/index.{md,json}` | B-04 "跑过哪些对照"发现入口 | 扫目录派生；不进 manifest |
| `reports/*.html`（6） | B-08 看板骨架页 | `macro-dashboard` / `request-browser` / `journey-viewer` / `journey-compare` / `benchmarks` / `tool-waste`；浏览器打开需经静态服务器（`file://` 有降级提示）；`common.js` 已内联 |
| `reports/.cache/{parse,llm,fingerprint.json}` | L1 解析缓存 / LLM 解读缓存 / L2·L3 产物指纹 | 可再生，非交付物 |

---

## 第六部分 · Phase C —— 概念/术语迁移的全项目彻底清理

（执行中回填 —— action plan 见 6.1，逐项处置见 6.2，验证与最终重生见 6.3）
