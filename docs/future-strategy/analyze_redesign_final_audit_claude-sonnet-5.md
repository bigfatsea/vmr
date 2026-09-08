// Ver 2026-09-08, by Claude (claude-sonnet-5)

# vmr analyze 重构 + 可视化 Stage 1 —— 独立叠加验收审计

<!--
  这是"上一轮 review 完成后的一个独立验收任务"。

  基准方案（设计权威，其余文档只作问题来源）：
    - docs/future-strategy/analyze_architecture_redesign_opus-5.md          (D1–D21, §1–§11, Phase 1–4)
    - docs/future-strategy/agent_session_visualization_deepdive_v1.0.md    (§10 三梯队, §11.2 + 附录 A.1.1–A.1.3/A.1.6/A.3 = Stage 1)

  问题来源（只提取 feature/模块/任务/问题清单，不采信其"已完成/已实现/won't-fix"结论）：
    - analyze_redesign_review_plan_gemini-3.8-flash.md        (T1–T5, F1–F12, G1–G5)
    - analyze_redesign_review_plan_v2_minimax-m3.md           (v2-T1/T2, v2-F1/F2)
    - analyze_redesign_review_claude-sonnet-5.md              (C1–C13, N1–N19, C-imp-1..5)
    - analyze_redesign_review_v4_claude-sonnet-5.md           (A–K 组, T-A..T-F, N-B1..N-B7)
    - analyze_redesign_acceptance_review_claude-sonnet-5.md   (NEW-A..NEW-F, M-01..M-19, Phase B UC)
    - analyze_dashboard_review_fixes_sonnet-5.md              (U1–U3, G1–G4, N1–N8, §5.1–5.5)

  代码基线： 050ad25 (refactor: rename internal/story → journey，重构实施起点，7abf0a4 的父提交)
             至 HEAD = 43763d5，共 132 commits，335 文件，+28720 / −13565。视作一次叠加变更逐项核实。

  执行纪律：
    1. 只从源码 / 测试断言 / 实跑结果取证，完全不采信历史文档的 "已完成" 声称。
    2. 不阅读项目内其他 review 记录的结论；本文件的判断完全独立。
    3. 事实清楚 + 方案明确无争议 + 十足把握 → 当场直接修复并留痕（直接提交 main，无 trailer）。
    4. 复杂 / 有争议 → 记录待用户决策，不动手。
    5. 方案 principle / KNOWN_ISSUES / previous decision 并非圣旨，有理有据可挑战。
    6. Phase A（叠加验收）闭环且无大问题后，执行真实日志 + 真实 LLM 的端到端验收测试（Phase B）。

  基线状态（2026-09-08 审计开始）：
    - go build ./... / go vet ./... ：通过
    - go test ./... ：全绿（38 包，cmd/vmr 105s）
    - gofmt -l ：3 个文件非 canonical（cmd_journey_setup.go / i18n/journey_index.go / journey/viewmodel_build.go）
      —— 由 3135487 + 159d6e5 引入，CI 门禁 `gofmt -l` 会失败。已在审计首步 gofmt -w 修复（FIX-1）。
-->

---

## 第一部分 · Action Plan：核验事项全清单

> 本清单是本次独立审计的核验基准。汇集自两份基准方案 + 6 份历史评审文档中出现过的
> 全部 feature / 模块 / 任务 / 问题编号。**不预设任何一条的落地状态。**
> 标记（第二部分回填）：✅ 已核实完成 ｜ 🟡 部分完成 ｜ ❌ 未完成 ｜ ✔️fixed 本轮直接修复 ｜ ⚖️ 待裁决。

### 组 A —— analyze 架构重构：核心裁决 D1–D21

| ID | 裁决 | 核验目标 | 状态 |
|---|---|---|:---:|
| A-D1 | 切片为消费弹性拆，不为缓存拆；缓存粒度整套一个指纹 | `slices.go` 五切片；L2 单指纹；无切片级隔离 | |
| A-D2 | `vmr-report.json` 单体不保留，一步解构为切片，无兼容视图 | 仓内零写出；宏观渲染源来自切片（经 `LoadReport` 重装 `Report2` 内存形状） | |
| A-D3 | Markdown 不用模板引擎（含 `text/template`），无 `-templates` | `rg text/template` 零命中；固定序列化器 | |
| A-D4 | 全部文案（含章节标题）进 ViewModel，经 i18n；渲染器只表达结构与顺序 | `viewmodel_*.go` + `i18n/report_*.go` 配对；AST 守卫 | |
| A-D5 | `-render-only` 覆盖全部常驻人读产物；`details/`+`evidence/` 永久排除 | 覆盖面表逐行 | |
| A-D6 | 自包含 HTML 废弃，收敛为骨架 + fetch | `render_html*`/`render_compare_html`/`toolwaste_html`/`story/assets` 零残留 | |
| A-D7 | 人读请求索引整族删除（`-<tag>.md`/`-cron-*.md`），`failed.md` 保留 | 零写出；旧渲染死代码清理；`requests/index.json` 唯一机读源 | |
| A-D8 | 全系统唯一 Digest 构造：长度前缀有序 sha256 链；md5 底座原样喂入 | `internal/digest` 叶子包唯一实现；三性质；定宽标量编码 | |
| A-D9 | `/reports/` HTTP 托管默认关；`api_keys` 未配置时硬性 403 | `analytics.serve` opt-in；无 key 全树 403；路径校验；禁目录列表；分层鉴权 | |
| A-D10 | `-render-only` 继承落盘 JSON 语言；换语言必须全量重跑 | `-lang` 与 manifest 不一致 exit 1 + 指引 | |
| A-D11 | 聚合类产物单一渲染路径；全量与 render-only 同代码路径 | `renderAllFromDisk` 共用；byte-equiv 守卫 | |
| A-D12 | ViewModel 不落盘；前端消费 raw 切片自带格式化；跨语言 fixture 钉死漂移 | 无 `vm_*.json` 写出；`fmt_cases.json` 双跑 | |
| A-D13 | 看板 `#data=` hash 传参；磁盘布局与 HTTP 路径逐字一致 | 骨架页根级平铺；相对路径 | |
| A-D14 | 数据版本探测 banner 进骨架页；查 `manifest.format`；不一致警告不阻断 | `EXPECTED_MANIFEST_FORMAT` 与 Go 常量一致；三分支纯函数 | |
| A-D15 | `-redact` 随自包含 HTML 一并删除 | flag + `redact bool` 分支 + 双语文档零残留 | |
| A-D16 | 托管目录默认 `./reports`（与 `analyze -o` 同默认）；不走 `rundir`；目录不存在仅 404 | `analytics.serve_dir` 默认值；缺失非启动错误 | |
| A-D17 | 详单文件名加 `r-` 类型前缀 | `reqdetail.FileName` 单点 + 包装函数透传 | |
| A-D18 | Journey JSON 自包含：tree + 同文件 `bodies` blob 去重表；三级 `match`；compaction 前驱摘录；截断口径统一（RespText 不截）；LLM 解读落 JSON | 逐条 | |
| A-D19 | 文件名去 `-partial` 后缀（journey + compare）；去 `journey-` 冗余前缀 | journey 与 compare 两侧 | |
| A-D20 | 作业清单来自索引；全量运行清扫 `journeys/details/` orphan；严禁波及 `compares/`/`requests/` | `CleanOrphanJourneys` 范围；L2 命中路径也清扫 | |
| A-D21 | `compares/index.{json,md}` 每次 analyze 扫目录派生重建；`compares/` 子树不进 manifest | `RebuildComparesIndex`；`AllSlicePaths` 不含 compares | |

### 组 B —— analyze 架构重构：Phase 1–4 功能模块

**Phase 1 · 数据层闭环**
- B-P1-1 概念归一 `internal/story`→`internal/journey`；`story` 一词从代码/CLI/产物清除
- B-P1-2 `corpus`→`Journey Benchmarks`：`journeys/benchmarks.{json,md}`，`-corpus`→`-benchmark`（无别名）
- B-P1-3 CLI 收敛：删 `vmr report`/`vmr story`；`-story-only`→`-journey-only`（无别名）；单入口 `vmr analyze`
- B-P1-4 五 macro 切片 + `manifest.json`（最后写）+ `requests/index.json` + `requests/failed.{jsonl,md}`
- B-P1-5 §3.3 七类渲染期现算事实下沉：① `tokens_coverage_pct`/`dur_low_n` ② `CostCoverage` ③ `meta.footnotes`/`disclaimers` ④ `highlights` 进 summary ⑤ 会话/任务标题投影进 `requests/index.json` ⑥ `journey_link` 跨产物链接 ⑦ 时间双字段 `ts`(epoch ms)+`ts_display`
- B-P1-6 journey JSON 自包含（D18 全体）
- B-P1-7 目录拓扑归位：`journeys/`、`compares/`、`requests/details|evidence/`、`.cache/parse/`
- B-P1-8 详单文件名 `r-` 前缀（D17）；journey 文件名归一（D19）
- B-P1-9 产物提交顺序与原子性（manifest 最后写、CreateTemp+Chmod 0600+Rename）+ orphan 清扫（D20）
- B-P1-10 `compares/index.{json,md}` 扫目录派生（D21）
- B-P1-11 人读请求索引整族删除（D7），`vmr-report.md` 相关链接改指看板与 `journeys/index.md`

**Phase 2 · HTML 看板**
- B-P2-1 `go:embed` 骨架 + 主题变量系统
- B-P2-2 零依赖内联 SVG 图元：折线 / 柱状 / 热力 / 延迟分位图（`svgLatencyPlot`）
- B-P2-3 六个看板页 + `#data=` 加载协议 + `file://` 提示降级
- B-P2-4 骨架页版本探测 banner + 渲染异常归因
- B-P2-5 前端格式化函数 + 跨语言 fixture
- B-P2-6 删除 `render_html*.go`/`render_compare_html.go`/`toolwaste_html.go` 与 `story/assets/`
- B-P2-7 `server` 挂载 `/reports/*`：显式开关、无 key 拒绝、路径校验、禁目录列表、分层鉴权
- B-P2-8 看板 JS 按切片真实 snake_case json tag 读数据（非 Go PascalCase）
- B-P2-9 `common.js` 实际随骨架页部署可用（内联）
- B-P2-10 骨架页 chrome 本地化策略一致
- B-P2-11 看板金额显示随切片币种（`pricing.currency` + 符号映射）

**Phase 3 · ViewModel 与固定序列化器**
- B-P3-1 report 侧 ViewModel（各 section 改为构建器）
- B-P3-2 journey 侧 ViewModel（`RenderMarkdown` 吃 `JourneySummary`）
- B-P3-2b compare 侧 ViewModel（`RenderComparisonMarkdown` 按 record 渲染）
- B-P3-3 固定结构 Markdown 序列化器（无模板引擎）
- B-P3-4 golden 测试下沉到 ViewModel 层
- B-P3-5 `-render-only`（覆盖面 / 作业清单来自 `journeys/index.json` / 语言继承）
- B-P3-6 ViewModel 中禁止未 i18n 的英文句子字面量（AST 守卫）

**Phase 4 · 产物级缓存**
- B-P4-1 整套产物指纹 + 失效矩阵七行
- B-P4-2 L1 输入解析缓存归位 `.cache/parse/`
- B-P4-3 L2 产物数据缓存：`Digest(输入哈希集 ‖ 配置指纹 ‖ 格式版本 ‖ 分析参数)`
- B-P4-4 L3 表现层缓存：`Digest(ViewModel 指纹 ‖ 渲染器版本 ‖ 语言)`
- B-P4-5 配置指纹只含影响金额部分（pricing overrides + exchange_rate + 标准表 stamp），排除 `listen` 等
- B-P4-6 分析参数指纹含改变取样口径的全部 flag（`-lang`、taskseg profile、自流量排除集、LLM identity 等）
- B-P4-7 `-no-cache` 常驻旁路（非过渡开关）
- B-P4-8 详单指纹携带 `(record, manifest, prev)` 身份 + 渲染器版本 + 语言
- B-P4-9 L2 命中的全量运行仍执行 orphan 清扫 + compares 索引重建
- B-P4-10 冷热一致性测试（浮点 `1e-6` 容差）
- B-P4-11 L2 指纹含 `vmr-quota.json` 内容哈希（仅当 config 声明 quota limits）（NEW-D）
- B-P4-12 zoom 模式 L2 命中校验主产物存在，缺失回落全量（N-B4）

### 组 C —— §9 测试守卫 + §11 不变量

既有守卫迁移：
- C-G9a `golden_test.go` 下沉 ViewModel 层
- C-G9b `structure_test.go` `LosslessReconstruction` 改真自包含断言（只吃 `j-<id>.json`）
- C-G9c `e2e_test.go` + "切片并集 ≡ 旧 Report2 字段集" 等价断言
- C-G9d 详单跨宏观/Journey 渲染字节一致 + 指纹携带渲染器版本
- C-G9e `i18n_e2e_test.go` + "ViewModel 无未 i18n 裸字面量" 守卫
- C-G9f archtest 行数预算登记 `viewmodel_*.go`；`section_*.go` 条目移除
- C-G9g archtest i18n 配对对象改 ViewModel 构建器
- C-G9h archtest：`server` 不 import 分析半区

新增守卫：
- C-G9-1 `-render-only` 产物与全量运行字节一致
- C-G9-2 缓存命中路径与冷启动路径产物一致（浮点 `1e-6` 容差）
- C-G9-3 跨语言格式化 fixture（`fmt_cases.json` 由 `fmtutil` 与看板 JS 各跑一遍）
- C-G9-4 切片内每个时间点必须同时有 `ts` 与 `ts_display`，缺一即失败
- C-G9-5 骨架页版本探测逻辑写成纯函数
- C-G9-6 `bodies` 表无孤儿、无悬引用（双向查）
- C-G9-7 工具配对三级 `match` 与决策脊柱实际渲染一致
- C-G9-8 更窄时间窗重跑，`journeys/details/` 无上一次孤儿文件残留
- C-G9-9 orphan 清扫不动 `compares/`、`requests/details|evidence/`
- C-G9-10 `compares/index.json` ≡ `compares/*.json` 目录内容

不变量：
- C-INV-1 两半区一条契约（`report`/`journey`/`ctxgraph`/`taskseg`/`chatmsg`/`reqdetail` 不 import `router`/`server`/`config`；`server` 不 import 分析半区；不建跨包共享胖 ViewModel）
- C-INV-2 内容寻址底座不可动摇：缓存判据只能是内容哈希
- C-INV-3 权限底线 0600 / 0700（含骨架页、缓存目录）
- C-INV-4 单一时区权威（`ts` + `ts_display` 双字段；前端不做二次换算）
- C-INV-5 不新增双账本（manifest 不放指标 / 切片不交叉引用 / 前端不重算）

### 组 D —— 可视化 Stage 1（deepdive §11.2 / 附录 A.3）—— 本次审计重点（历史评审覆盖最少）

| ID | 任务 | 核验目标 | 状态 |
|---|---|---|:---:|
| D-S1-1 | ToolsHash 入 Manifest + `CacheSchemaVersion` 8→9 | `ctxgraph.BuildManifest` 增 `ToolsHash`/`HasTools`；`hashMsgJSON(body["tools"])`；schema 9；golden 重生 | |
| D-S1-2 | Step 层 `CacheBreak` 归因事实 | 六类：`system` / `tools` / `provider_switch` / `history:<editkind>` / `history:stitch` / `unexplained`；附 `cache_break_ratio_from/to`；`unexplained` = Append 且各哈希未变但命中率骤降 | |
| D-S1-3 | CacheBreak 三处渲染 | decision spine 脊柱行、journey-viewer 徽标、compare Cache 章节归因列 | |
| D-S1-4 | 请求级 `vmr diff <coordA> <coordB>` | 复用 replay 坐标定位器（抽成共享 helper）；对比 Header/System/Tools/Messages（消息哈希 LCP + 首分歧 + 双侧尾部增量）+ usage/缓存；3–5 条结构化 verdict + 免责；纯 CLI、英文、不落 reports/、不进缓存指纹 | |
| D-S1-5 | 资产突变追踪器 `internal/journey/artifacts.go` | build 时从结构化文件参数（`path`/`file_path`/`filepath`/`filename`/`file`）+ bash 窄正则（`>` `>>` `tee` `rm` `sed -i` `patch` `mv` `cp` `git apply` `mkdir`）提取；落 `JourneySummary.Artifacts`；bash 启发式带 `heuristic` 免责 | |
| D-S1-6 | Artifacts 消费端 | `.md` 附录表 + journey-viewer 面板一键平滑滚动跳步（RunArtifacts Jump 范式） | |
| D-S1-7 | 重复任务聚类 `internal/journey/clusters.go` | Jaccard 相似度（初始指令归一化 token 集合）≥ 阈值聚合；输出 `clusters: [{anchor_title, members:[{id,model,cost,wall,...}]}]` 落 `journeys/index.json` | |
| D-S1-8 | 聚类消费端 | `journeys/index.md` 分组呈现 + 每簇"最快/最省"标注；journey-viewer 候选列表按簇分组；每成员旁可复制 `vmr analyze -compare <a>,<b>` | |
| D-S1-9 | 聚类阈值进 report.yaml + L2 分析参数指纹（`ComputeAnalysisParamsFingerprint` 加字段） | | |
| D-S1-10 | Stage 1 失效矩阵 / L2 指纹 / manifest 盖章范围同步 | schema 9 bump 触发 L1；新 report.yaml 键入 paramsFP；artifacts/clusters 进 `j-<id>.json` / `index.json`（已在盖章内） | |
| D-S1-11 | Stage 1 文档同步：CHANGELOG `[Unreleased]`、KNOWN_ISSUES、design docs、deepdive、`.zh` siblings | | |
| D-S1-12 | Stage 1 守卫测试：cachebreak 六类归因、artifacts 提取（结构化 + 启发式 + 免责）、`vmr diff` LCP、clusters Jaccard | | |
| D-S1-13 | Stage 1 看板字段对齐：journey-viewer / compare 读 `cache_break`、`artifacts`、`clusters` 的真实 snake_case key | | |

### 组 E —— 历史评审遗留问题全集（逐条列问题本身，不含历史处置结论）

**gemini-3.8-flash：** T1（`vmr-report.json` 单体保留与 D2 冲突）· T2（Digest 双份拷贝）· T3（v4 Analytics 设计文档过期）· T4（方案提案文档状态头过期）· T5（"VM 无裸字面量"守卫未自动化）· F2（`.parse-cache` 未归一）· F3（L2 指纹缺 `llm_key` 自流量 tag + LLM identity）· F4（`commitManifest` 静默吞错）· F5（`requests.go` 包注释过期）· F6（KNOWN_ISSUES 多处旧引用；2.79/2.56 陈旧）· F7（CHANGELOG 单体删除表述失实）· F8（`viewmodel.go` 过渡期注释失效）· F9（README 双语 `vmr story`/`-corpus` 残留 + 不可运行示例）· F10（3C 移交清单 vm 前缀 helper 改名回退未执行）· F11（累积产物换语言 Markdown 中英混排）· F12（看板 `FmtCurrency` 恒 `$`）· G2（`Format` 与 `ManifestFormat` 两个独立常量）

**minimax-m3 v2：** v2-T1（compare 产物保留 `-partial` 后缀）· v2-T2（遗留 `story`/`corpus` 符号与文件名）· v2-F1（L2 命中跳过 orphan 清扫）· v2-F2（`rows.go`/`aggregate.go`/`story_render.go` 三处过期注释）

**claude-sonnet-5（N1–N19）：** N1（`common.js` 未部署）· N2（`llm_interpretation` 未入 journey JSON）· N3（`requests.go` ~250 行死代码 + `report_requests.go` 死文案）· N4（渲染产物 `stories/` 失效链接）· N5（`compares/index.md` 硬编码英文）· N6（`cmd_journey_setup.go` 旧索引 shim）· N7（`WriteRequestsIndex` 顶层 `vmr-requests.json` 死分支）· N8（`ValidateManifest` 不校验 8 份切片齐全）· N9（~200 处注释/test 名历史称谓）· N10（骨架 chrome 本地化不一致）· N11（配置指纹字段核对）· N12（方案 §7.2 `-from/-to` 术语）· N13（`RequestRow` 缺 `ts_display`）· N14（Go 1.26 gofmt 差异）· N15（六骨架页 JS PascalCase 读切片）· N16（`vmr-report.md` §8 指向已删 `vmr-requests.md`）· N17（`config.yaml` endpoint group 旧语法致定价降级）· N18（`-render-only` L3 命中信任磁盘产物）· N19（旧快照新二进制 render-only 丢 LLM 段）

**claude-sonnet-5 C 节改进：** C-imp-1（流式序列化削 RSS）· C-imp-2（`bodies` blob key 短哈希前缀）· C-imp-3（`clientsWithSiblingFile` 恒空分支移除）· C-imp-4（`structureExcerptChars` 别名消除）· C-imp-5（`request-browser.html` 排序键 RFC3339 错位）

**claude-sonnet-5 v4（T-A..T-F / N-B1..N-B7）：** T-A（55 Go 文件注释/测试历史称谓）· T-B（v4 Analytics §4 描述 `section_*.go`）· T-C（看板"散点"图元未按字面实现）· T-D（`WriteRequestsIndex` 路径归一 fallback）· T-E（工作区 `config.yaml` 旧语法 = N17）· T-F（N11/N12/N18 观察/文档项）· N-B1（journey `.md` `→ [详情]` / 证据链接因拓扑下沉全部 404，且被守卫用错误前缀锁定）· N-B2（`journeys/index.md` H1 "VMR Story Index"）· N-B3（`requests/failed.md` 引子指向已删 `vmr-requests-<tag>.md`）· N-B4（zoom L2 命中不校验主产物存在）· N-B5（`success_rate` round2 截断，看板/报告不一致）· N-B6（`finance.json` provider 成本非逐字节确定）· N-B7（`RendererVersion` 未随 N-B1 bump）

**claude-sonnet-5 acceptance（NEW-A..NEW-F）：** NEW-A（journey step `usage` 子对象 PascalCase）· NEW-B（"story half" 描述短语 9 处 + 一处用户可见错误串）· NEW-C（`cmd_journey_setup.go` Go 1.26 gofmt）· NEW-D（L2 命中配额记账冻结）· NEW-E（`_eval/calibrate_p1b.go` 仍用 `story` import）· NEW-F（`benchmarks.md` H1 "Journey Corpus Report"）

**dashboard review（U1–U3 / G1–G4 / N1–N8）：** U1（同页 `#data=` 不重载）· U2（compare HTML 与 .md 不对等）· U3（tool-waste 800px）· G1（导航栏统一）· G2（宽度统一 1280）· G3（每页下载链接）· G4（`reports/`/`reports-en/` 最后复核）· N1（compare `kind` 值域错）· N2（compare `fmtDelta` 弃用 `delta_rel`）· N3（`request-browser` 中文）· N4（file:// 横幅三种写法）· N5（missing 分支不齐整）· N6（journey-viewer 详情 parity 缺口）· N7（`svgLatencyPlot` 编造分位数）· N8（`request-browser` 行渲染未转义）· §5.3（下载链接鉴权 401）· §5.4（骨架页 chrome i18n）

### 组 F —— §11.2 已知取舍（逐条判断是否仍成立、是否值得挑战）+ §11.3 顺带机会

24 条取舍 + `KNOWN_ISSUES` 相关条目；§11.3 流式序列化削 RSS（= C-imp-1）。

---

## 第二部分 · 逐项执行详情

### 2.0 验证手段

- 基线：`go build ./...` / `go vet ./...` / `go test ./...`（38 包）全绿；`go test ./internal/archtest/` 通过。
- 端到端冒烟（无 LLM）：`/tmp/vmr-audit analyze -c config.yaml -o /tmp/vmr-a-e2e -details logs/vmr-audit-2026-08-24.jsonl.zst`（519 records，15 journeys）——产物拓扑 / 权限 / manifest / 切片 schema / Stage 1 事实逐一核对。
- `-render-only` 字节等价：`cp` 全量产物 → `-render-only` → `diff -rq` 除 `.cache/` **逐字节一致**。
- 逐项核对：读实现 + 读对应测试断言 + 抽查落盘 JSON 结构，不采信任何文档 claim。
- `./vmr check -c config.yaml` 通过（T-E/N17 config 迁移已落地，无旧语法）。

### 2.1 组 A / B / C（analyze 架构重构）—— 抽样独立复核结论

历史 6 份评审 + Phase B 端到端已重度覆盖此三组。本轮以 `050ad25..HEAD` 累积效果做定向抽查，
**结论：实质、正确落地，未发现数据正确性 / 缓存失效 / 安全模型层面的错误。** 关键证据：

| 事项 | 独立核实结果 |
|---|---|
| A-D2 单体删除 | `internal/report/digest.go` 已删；仓内零 `vmr-report.json` 写出；宏观渲染源 `LoadReport` 从切片重装 `Report2` 内存形状 |
| A-D8 / T2 唯一 Digest | `internal/digest` stdlib-only 叶子包唯一实现；`internal/journey/digest.go` 仅 `ComputeJourneyDigest` 组合层 |
| A-D11 单一渲染路径 | 全量产物 `cp` → `-render-only` → `diff -rq`（除 `.cache/`）**逐字节一致** |
| A-D17/D19 文件名 | `requests/details/r-<ts>_..._h8.md`；`journeys/details/j-<id>.{json,md}` 无 `journey-` 前缀、无 `-partial` |
| B-P1-5 §3.3 事实下沉 | `manifest.footnotes`(6) + `disclaimers`(3)；`summary.highlights` 非空；`finance.cost_coverage`；`requests/index.json` 行含 `ts`(epoch ms) + `ts_display` + `sessions` + `journey_link` |
| A-D9 `/reports/` 安全 | `server/reports.go`：`analytics.serve` opt-in、`len(APIKeys)==0` 全树 403、Clean+前缀+Lstat 拒 symlink+禁目录列表、分层鉴权；archtest 强制 `server` 不 import 分析半区 |
| C-INV-3 权限 | vmr 自建输出目录链每级 0700 / 文件 0600（实测） |
| N-B1 链接 | journey `.md` 相对 `.md` 链接 **494/494 可解析**（`../../requests/details|evidence/`） |
| N5 compares/index.md 双语 | `-lang zh` 输出 `# Journey 对照索引` |
| M-01..M-19 / N9 / T-A 退役术语 | 核心 Go 代码（非 test、非 `_eval`）`rg 'vmr story|vmr report|internal/story|stories/|vmr-stories|vmr-story-corpus|.parse-cache'` **零命中**；无 `TestCmdStory_*` |
| NEW-A `chatmsg.Usage` | 五字段带 json tag（`in`/`out`/`cache_read`/`cache_write`/`reasoning`） |
| N-B7 `RendererVersion` | = 2 |
| archtest | 全绿（导入边界 / 行数预算 / i18n 配对 / doc_refs） |

> 抽查未覆盖处的默认信任度由 Phase B 端到端补齐。

### 2.2 组 D（可视化 Stage 1）—— 本轮审计重点，逐项结论

> 代码基线：`8cda62c`（ToolsHash）· `7f381ae`（vmr diff）· `34162a3`（cache break）· `159d6e5`（artifacts + clusters）· `05e96a5`（viewer artifacts 面板）。历史评审均在这些提交**之前**结束，本组基本无独立评审覆盖。

| ID | 结论 | 证据 / 缺口 |
|---|:---:|---|
| D-S1-1 ToolsHash 入 Manifest + schema 8→9 | ✅ | `ctxgraph.CacheSchemaVersion = 9`；`Manifest.ToolsHash`/`HasTools` 存在；CHANGELOG 记 `tools_hash` + schema 9 |
| D-S1-2 Step `CacheBreak` 归因事实 | 🟡 | 六类归因全实现（`cachebreak.go`），8 个单测覆盖全部类别；**但 `unexplained` 归因把"正常 token 稀释导致的命中率波动"误判为"击穿"——见 S1-L（本组最重问题）** |
| D-S1-3 三处渲染 | 🟡 | decision spine 脊柱行 ✅（`viewmodel_spine.go` + i18n `CacheBreakBadge` EN/ZH）；journey-viewer 徽标 ✅（`journey-viewer.html:478`）；compare `.md` Cache Breaks 表 ✅（`render_compare.go:289`）——**但 `journey-compare.html`（看板）不渲染 breaks 表，只渲染命中率曲线——见 S1-F** |
| D-S1-4 `vmr diff <a> <b>` | 🟡 | `cmd_diff.go`：复用 `loadAuditRecord` 共享定位器（与 `vmr replay` 同一函数）✅；Header/System/Tools/Messages LCP + 首分歧 + 双侧尾部 ✅；5 条结构化 verdict + 免责 ✅；纯 CLI、英文、不落盘、不进缓存 ✅；11 个测试 ✅。**缺口见 S1-G（stale TODO + 未用 `Manifest.ToolsHash`，漏检工具 schema 变更）** |
| D-S1-5 `artifacts.go` 提取器 | ✅ | 结构化文件参数（`deliverableFileKeys`）+ bash 窄正则（`>` `>>` `tee` `rm` `sed -i` `patch`/`git apply`）；`heuristic` 免责标注；build 时（`summary.go:115` `ExtractArtifacts(j)`，在截断前）；2 个单测。**heuristic 质量瑕疵见 S1-K（非缺陷）** |
| D-S1-6 artifacts 消费端 | ✅ | `.md` 附录表 `## 触达文件与资产`（`viewmodel_build.go:buildVMArtifacts`）；journey-viewer 面板 + `<a href="#step-N" onclick="...scrollIntoView({behavior:'smooth'})">` 跳步（`journey-viewer.html:452`）；实测 32 个 artifact 正确渲染 |
| D-S1-7 `clusters.go` 聚类 | 🟡 | Jaccard 相似度聚合 ✅；`journeys/index.json` 落 `clusters` ✅；实测 2 簇。**缺口：① `ClusterMember` 无 cost/wall/model/net_working_ms 字段（设计 A.1.6 明列），"最快/最省标注"无法产出——S1-A；② 聚类键用 `Title` 而非"初始指令归一化 token"（设计明列）——S1-B** |
| D-S1-8 聚类消费端 | 🟡 | `index.md` 分组渲染 ✅（`journeyindex.go:311`）+ 每簇给可复制 `vmr analyze -compare`（仅前两成员）✅。**缺口：① journey-viewer 候选列表**不**按簇分组（设计明列）——S1-D；② 无"最快/最省"标注（依赖 S1-A）；③ 每簇仅一条 compare 建议而非"每成员旁"** |
| D-S1-9 阈值进 report.yaml + paramsFP | ❌ | 相似度阈值 `0.45` **硬编码**在 `clusters.go:75`；report.yaml 无对应键；`ComputeAnalysisParamsFingerprint` 无 cluster 字段。设计（A.1.6 步骤 3 / A.3）明确要求——见 S1-C |
| D-S1-10 失效矩阵 / 盖章范围同步 | ✅ | schema 9 bump 触发 L1 失效；`artifacts` 进 `j-<id>.json`（盖章内经 journeys/index）、`clusters` 进 `journeys/index.json`（盖章切片） |
| D-S1-11 文档同步 | 🟡 | CHANGELOG `[Unreleased]` 有条目**但结构损坏（重复 `### Added`/`### Changed`/`### Fixed` 段）——S1-I**；`docs/UserGuide.md` + `.zh` **完全没有** `vmr diff` / cache 击穿 / 触达文件 / 任务聚类——S1-H；deepdive §11.2 对聚类的落地描述**过度声称**（"每成员旁 compare 建议""viewer 分组"实际未做） |
| D-S1-12 守卫测试 | 🟡 | cachebreak 8 测试（全类别）✅；`vmr diff` 11 测试 ✅；**artifacts 仅 2 测试、clusters 仅 1 测试——偏薄**（尤其 clusters 有多处缺口却无回归钉） |
| D-S1-13 看板字段对齐 | 🟡 | journey-viewer 读 `cache_break`/`cache_break_ratio_*`/`artifacts` 真实 snake_case ✅；**compare 看板缺 `breaks`（S1-F）；无页面消费 `clusters`（S1-D）** |

---

## 第三部分 · Review 事项完成度总结

### 3.1 组 A / B / C / E（analyze 架构重构 + 历史评审遗留）

**结论：方案 D1–D21、Phase 1–4、§9 守卫、§11 不变量、6 份历史评审的 T/F/N/G/C/N-B/NEW 全系列事项，
在 `050ad25..HEAD` 累积效果下均已实质落地。** 本轮独立抽查（读码 + 真实日志冒烟 + `-render-only` 字节等价 +
archtest）未发现新的数据正确性 / 缓存失效 / 安全 / 不变量层面问题。历史评审工作扎实，其结论经本轮独立取证
（不采信 claim）大体证实。

**唯一在本组直接修复的是 FIX-1（gofmt 回归）**——见下。其余组 A/B/C/E 事项标记为 **✅ 已核实完成**。

### 3.2 组 D（可视化 Stage 1）—— 完成度逐项

| ID | 状态 | 说明 |
|---|:---:|---|
| D-S1-1 ToolsHash + schema 9 | ✅ 已完成 | |
| D-S1-2 CacheBreak 归因事实 | 🟡 部分完成 | 六类归因结构完整，但 `unexplained` 阈值逻辑产生大量假阳性（S1-L） |
| D-S1-3 三处渲染 | 🟡 部分完成 | spine / viewer / compare.md 三处 ✅；compare **看板** 缺 breaks 表（S1-F） |
| D-S1-4 `vmr diff` | 🟡 部分完成 | 主体 ✅；未用 `Manifest.ToolsHash`，漏检工具 schema 变更 + stale TODO（S1-G） |
| D-S1-5 artifacts 提取器 | ✅ 已完成 | |
| D-S1-6 artifacts 消费端 | ✅ 已完成 | |
| D-S1-7 clusters 聚类 | 🟡 部分完成 | member 缺 cost/timing 字段（S1-A）；键用 Title（S1-B） |
| D-S1-8 聚类消费端 | 🟡 部分完成 | index.md ✅；viewer 分组 ❌（S1-D）；最快/最省标注 ❌（S1-A） |
| D-S1-9 阈值进 report.yaml + paramsFP | ❌ 未完成 | 硬编码 0.45（S1-C） |
| D-S1-10 失效矩阵 / 盖章同步 | ✅ 已完成 | |
| D-S1-11 文档同步 | 🟡 部分完成 | CHANGELOG 结构损坏（S1-I）；UserGuide 缺失（S1-H）；deepdive §11.2 过度声称 |
| D-S1-12 守卫测试 | 🟡 部分完成 | artifacts / clusters 测试偏薄 |
| D-S1-13 看板字段对齐 | 🟡 部分完成 | 见 S1-F / S1-D |

### 3.3 部分完成 / 未完成事项详述（问题描述 / 根因 / 建议方案 / ROI）

见第四部分（本轮新发现问题与 Stage 1 缺口合并叙述——它们本质是同一批"Stage 1 落地未尽"事项）。

---

## 第四部分 · 过程新发现问题与处理

### 4.1 汇总

| 编号 | 问题 | 严重度 | 处置 |
|---|---|:---:|:---:|
| FIX-1 | `gofmt -l` 报 3 文件非 canonical（CI 门禁失败），由 3135487 / 159d6e5 引入 | 中（CI 红） | ✔️ **已直接修复** |
| S1-L | Step `cache_break` 的 `unexplained` 归因把正常命中率波动（如 98%→70%）误判为"击穿" | **中-高**（核心特性信噪比） | ⚖️ 待裁决（含具体阈值建议 + 实测数据） |
| S1-F | `journey-compare.html` 不渲染 Cache Breaks 归因表（`.md` 有、JSON 有数据） | 中（看板↔.md parity 回归） | ✔️ **已直接修复** |
| S1-G | `vmr diff` 未使用 `Manifest.ToolsHash`，漏检"工具名相同但 schema 变更"；stale TODO | 中（漏报） | ✔️ **已直接修复** |
| S1-I | CHANGELOG `[Unreleased]` (a) 结构损坏——2× `### Added` / 3× `### Changed` / 2× `### Fixed`；(b) block-2（重构前既有条目）大量引用 `vmr story` / `vmr report` / `internal/story` / `-corpus` / `vmr-stories.md` / `vmr-requests-*.md`——与同一发版内"删除这些"的 breaking 条目自相矛盾 | 中（发版 Release body 畸形 + 自相矛盾） | ⚖️ 待裁决（方案见 4.2；比初判更大，不宜盲改） |
| S1-A | 任务聚类 `ClusterMember` 缺 `cost`/`wall`/`model`/`net_working_ms`（设计 A.1.6 明列），"哪次最省/最快"无法回答 | 中（特性价值折损） | ⚖️ 待裁决 |
| S1-C | 聚类相似度阈值 `0.45` 硬编码，未进 report.yaml、未进 L2 分析参数指纹（设计明确要求） | 低-中 | ⚖️ 待裁决（YAGNI 讨论） |
| S1-D | 任务聚类未在 journey-viewer 候选列表按簇分组（设计 A.1.6 明列） | 低-中 | ⚖️ 待裁决 |
| S1-B | 聚类键用 `JourneyIndexRow.Title` 而非"初始指令归一化 token 集合"（设计明列） | 低（可能可接受） | ⚖️ 待裁决 |
| S1-H | `docs/UserGuide.md` + `.zh` 完全没有 `vmr diff` / cache 击穿归因 / 触达文件 / 任务聚类 | 低-中（用户文档欠账） | ⚖️ 待裁决（可代做，需双语） |
| S1-E | `ComputeCacheBreak` 对"工具集新增/移除"不归因（`system` 对称处理了新增/移除，`tools` 只在两侧都有工具时才判） | 低（归因盲区） | ⚖️ 待裁决 |
| S1-M | 聚类纳入 `cron` 类 journey；锚点标题带原始 `[cron:UUID` 前缀（UUID token 使同 cron 必然聚类） | 低（噪音 / 观感） | ⚖️ 待裁决 |
| S1-J | `cachebreak.go` / `digest.go` 版本头是未来日期（`2026-09-16` / `-15`，今天 09-08） | 极低 | 仅记录（CLAUDE.md：头日期不值得单独修） |
| S1-K | `artifacts.go` bash 启发式质量瑕疵（`bashApplyRe` 捕获 `patch -p1` 的 `-p1` 而非文件名；op 升级注释与代码不符） | 极低（heuristic 已免责标注） | 仅记录 |
| deepdive §11.2 | 对聚类落地的描述过度声称（"每成员旁 compare 建议""viewer 分组"实际未做） | 低（文档失实） | ⚖️ 随 S1-A/D 裁决后一并回写 |

### 4.2 详述

#### FIX-1 — gofmt 回归 ✔️ 已修复

- **问题描述**：`gofmt -l .` 报 `cmd/vmr/cmd_journey_setup.go`（`3135487` 引入）、`internal/i18n/journey_index.go`、`internal/journey/viewmodel_build.go`（均 `159d6e5`）非 canonical——纯空白/空行对齐（结构体字段列对齐、函数前空行）。
- **影响**：CI（`.github/workflows/ci.yml`）跑 `gofmt -l` 并对非空输出 fail。这三个文件均由本审计范围内的 Stage 1 提交引入，属回归。（区别于历史评审的 N14/NEW-C：那是 Go 1.26 尾随注释规则，本机 1.26 vs CI 1.25 差异；这三个是 Go 1.25 下就脏。）
- **处置**：`gofmt -w` 三文件；`go build ./...` 通过；零行为变化。随本轮修复提交。

#### S1-L — `cache_break` 的 `unexplained` 归因信噪比过低 —— ⚖️ 待裁决（本组最重）

- **问题描述**：`ComputeCacheBreak`（`internal/journey/cachebreak.go`）第 6 档：当 `Append` 边 + sys/tools/端点均未变，但 `prevRatio >= 0.50 && curRatio < prevRatio - 0.25` 时归因为 `unexplained`。它只看**相对跌幅**，从不看 `curRatio` 的**绝对值**。多轮 agent 对话里，一步塞进一个大工具结果（如读一个 40KB 文件）会天然把下一轮的命中率从 0.98 稀释到 0.70——KV 前缀完全复用，只是新鲜 input 占比高了。这**不是**击穿，正是设计 §7.0 自己承认的"正常增量稀释"。
- **实测数据**（`logs/vmr-audit-2026-08-24`，32 个 `unexplained` 归因的 `from→to`）：
  - **真击穿**（cur ≈ 0）：`0.85→0`、`0.94→0`、`1.0→0`、`1.0→0.001`、`0.90→0.005`、`0.71→0.008`、`0.90→0.09` …约 18 个
  - **正常波动误判**：`0.98→0.71`、`0.98→0.62`、`0.93→0.46`、`0.97→0.62`、`0.89→0.61`、`0.81→0.55`、`0.99→0.73`、`0.995→0.21` …约 14 个
  - 渲染实证：`j-pimini-…0357ba6c.md` 出现 `Step 64 · Cache: 异常骤降 (99%→73%)` 徽标——73% 是健康命中率，这个徽标是误导。
- **根因**：阈值逻辑抄自"相对跌幅"直觉，遗漏了"真击穿必然把命中率打到接近 0（整段前缀要重编码）"这一物理约束。`ShouldDisplayCacheBreak` 又对 `unexplained` 一律 return true，于是假阳性全部进脊柱 + 看板徽标。
- **建议方案**：`unexplained` 追加绝对值门槛——`curRatio` 必须**同时**满足 `< 0.15`（或 `< 0.20`）。实测数据下这条能干净切分：真击穿 cur ∈ {0, 0.001–0.09}，假阳性 cur ∈ {0.21, 0.39, 0.46, 0.55, 0.61, 0.62, 0.73}。
  - 代码改动：`cachebreak.go` 第 6 档加 `&& curRatio < CacheDropAbsFloor`（新常量 0.15）；`cachebreak_test.go` 的 `TestComputeCacheBreak_UnexplainedDrop` 补"高相对跌幅但 cur 仍健康 → None"用例。
  - 连带：`compare.go` 的 `unexp` 计数、脊柱/看板徽标自动收敛。
- **ROI**：**高**。~5 行代码 + 1 个测试用例，把这个"VMR 核心差异化"特性（cache 经济学法医学）从"一半是噪音"救回"高信噪比"。不改则该徽标长期训练用户忽略它。
- **为何不当场改**：阈值属"需要真实数据标定误杀率"一类（deepdive 自己对 loop 检测的同款纪律），且 0.15 vs 0.20 有取舍空间，交你拍板具体数值。

#### S1-F — `journey-compare.html` 不渲染 Cache Breaks 归因表 —— ✔️ 已直接修复

- **问题描述**：Stage 1 给 compare `.md` 加了 "Cache Breaks" 对照表（`render_compare.go:289`，`t.CacheBreaksTableHeader` + A/B 各类别计数）。数据在 compare JSON 里（`extras.cache.a.breaks` / `.b.breaks`，`CacheStats.Breaks map[string]int json:"breaks,omitempty"`）。但 `journey-compare.html` 的 "Prompt Cache Hit Rate" 章节只渲染命中率（`ex.cache.a` 的 first/steady/min/max + 曲线），**不渲染 breaks**。
- **根因**：Stage 1 往 `.md` 渲染器加章节时没同步 `journey-compare.html`——正是 dashboard review U2（"HTML 与同名 .md 对不上"）的同类回归，由 Stage 1 引入。
- **处置**：`journey-compare.html` 的 "Prompt Cache Hit Rate" 章节命中率表后追加 breaks 计数表（镜像 `render_compare.go` 六列：Unexplained Drop / Provider Switch / System Prompt / Tools Churn / History Break），字段读 `ca.breaks` / `cb.breaks`，`history:` 前缀求和等价 `historyBreakCount`。`js_test.go` compare-detail mock 补 `breaks`。缺 `breaks` 时全部 `|| 0`，无 undefined。`go test ./internal/dashboard/` 全绿。实测 compare JSON 携 `a.breaks={history:replace_tail:3, provider_switch:1, unexplained:3}` 等，`.md` 与看板同源。

#### S1-G — `vmr diff` 未使用 `Manifest.ToolsHash` —— ✔️ 已直接修复

- **问题描述**：`cmd_diff.go:108` 有 stale TODO `// TODO: switch to Manifest.ToolsHash once W1 lands.`——W1（ToolsHash，`8cda62c`）已落地。当前 tools 行只比工具**名**集合（`chatmsg.ToolNames`），两个请求工具名相同但某工具的 **description / parameters schema** 变了（真实 cache 击穿诱因，`ToolsHash` 正是为此加的）时 `vmr diff` 报 `(same)`。
- **根因**：`vmr diff`（`7f381ae`）比 ToolsHash（`8cda62c`）早合入，TODO 留了没回收。
- **处置**：保留工具名 `+/−` 差异；`makeToolsRow` 增 `mA, mB` 入参——当名集合相同（`ann == "(same)"`）但 `mA.HasTools && mB.HasTools && mA.ToolsHash != mB.ToolsHash` 时注记改为 `(same names, tool schema changed)`。删 stale TODO。`cmd_diff_test.go` 加 `TestCmdDiff_ToolSchemaChangedSameNames`（同名不同 description）。全部 `TestCmdDiff` 绿。

#### S1-I — CHANGELOG `[Unreleased]` 结构损坏 + 退役术语泄漏 —— ⚖️ 待裁决

分两层，第二层比初判严重：

**(a) 结构损坏（清晰、可机械修）**：`[Unreleased]` 下有 **2× `### Added`、3× `### Changed`、2× `### Fixed`**（`CHANGELOG.md` 行 24/31/36/50/60/98/177）。第一批（行 24–49）是重构 + Stage 1 内容，被追加在**顶部**，没并进下方既有同类段。Keep a Changelog 要求每版本每类型一段；`release.yml` 逐字提取 `[Unreleased]` 作 GitHub Release body——现在发版会得到重复标题的畸形 body。**根因**：多轮 feature 分支 / 评审各自 append 自己的块（`8ef6ffc` 等），从不合并。

**(b) block-2（重构前既有 `[Unreleased]` 条目）通篇退役术语，与同版发版的删除条目自相矛盾**：`### Changed` / `### Fixed`（行 60–201）里几十处 `vmr story` / `vmr report`（作命令，如行 106/115/137/145/148）、`internal/story`（行 127/137/145）、`vmr-stories.md` / `stories/journey-<id>.md`（行 158/160/163/164）、`vmr-requests-<tag>.md` / `vmr-requests-failed.md` / `vmr-requests.md`（行 76/77/140/162）、`vmr analyze -corpus`（行 89/130/131/142/143）。这些条目描述"在 `vmr story` 里修了个 bug""`vmr-stories.md` 现在会声明…"——而**同一个 `[Unreleased]` 发版**的 breaking 条目（行 33/34）明写"`vmr story` 子命令删除""`vmr-requests.md` 整族删除"。发版 Release body 会同时说"删掉了 X"和"在 X 里修了个 bug"。
- **这也戳破了 acceptance review 的 Phase C "根治完成" / "全仓退役 token 终扫…零命中"声称**——那次 sweep **有意排除了 CHANGELOG**（"CHANGELOG 与 KNOWN_ISSUES 的'已闭环'trail"），所以"零命中"仅指代码 + 当前态文档，不含 CHANGELOG。
- **根因 / 张力**：CHANGELOG 条目是提交时点的历史记录（当时 `vmr story` 还在，"在 `vmr story` 里修了 X" 是准确的）。但 `[Unreleased]` 是"净变更"——一批条目一起发，读者看到的是 vN vs vN-1 的差异；对一个同版就删掉的命令做的修复，对用户是净不可见的。

- **建议方案**（三选一，需你定方向）：
  - **A（最小）**：只修 (a)——把顶部三段并进下方对应段，产出每类型一段、Keep a Changelog 顺序。所有条目保留、不改措辞。(b) 挂 `KNOWN_ISSUES` 或 `release.yml` 注记"`[Unreleased]` 含历史术语，发版前需人工过一遍"。~30 分钟。
  - **B（推荐，正解）**：(a) + 对 (b) 做一次 `[Unreleased]` 净变更重写——把"在已删命令里修 bug"类条目要么改写为现名（若特性存活并迁移，如失败聚类 → `requests/failed.md`），要么并进重构 breaking 条目（若特性被整体重构掉）。产出一份"如果今天发版，用户看到的净变更"。~2–3 人天，需逐条判断特性存活情况。
  - **C（挑战 CLAUDE.md 的"CHANGELOG 是 trail"）**：承认 `[Unreleased]` 就是会脏、发版时才归整，`release.yml` 加一步人工确认闸。零成本，但把问题推给未来的发版者。
- **ROI**：A 修掉畸形 body（发版硬伤），留下自相矛盾。B 一次性把 `[Unreleased]` 收敛成可发状态，但工作量大。**倾向 A 现在做 + B 排进"下次发版前"专项**。
- **为何不当场改**：(b) 涉及逐条判断十几个特性"重构后去哪了"，有编辑判断成分，且 CLAUDE.md 对 CHANGELOG 有"不改 trail"的纪律张力——需你先定 A/B/C。

#### S1-A — 任务聚类 member 缺 cost / 时长字段 —— ⚖️ 待裁决

- **问题描述**：`clusters.go` 的 `ClusterMember` = `{ID, Client, Requests, Steps, Rendered}`。设计 A.1.6 的输出契约是 `members: [{id, model, cost, wall, net_working_ms}]`，且"每簇给出'最快/最省'标注"。数据源 `JourneyIndexRow` **本身就没有** cost / wall-clock / model / net_working_ms——只有请求数、任务数、步数、时间窗。`index.md` 每个 member 只渲染 `- \`id\` · requests=N`。
- **根因**：聚类实现挂在 `journeys/index.json` 的候选行上，而候选行是"轻量索引"（不做逐 journey 指标聚合，那要读每个 `j-<id>.json` 或重算 metrics）。设计假定索引行已带成本/时长，实际没有。
- **建议方案**：
  - **A**（补齐）：`ComputeTaskClusters` 阶段对每个 member 读其 `j-<id>.json` 的 `metrics`（cost / net_working_ms / model_usage），填进 `ClusterMember`；`index.md` 标注每簇 `⭐ 最省` / `⚡ 最快`。工作量：聚类要能访问 `details/` 目录（目前只吃 `[]JourneyIndexRow`）——签名变更 + 读盘 + 渲染 + 测试，约 0.5–1 人天。
  - **B**（降规格）：接受"聚类只回答'跑过几次 + 是哪几个'，成本对比点进去自己看"，把设计 A.1.6 的 `{model,cost,wall}` 契约与"最快/最省标注"划掉，deepdive §11.2 同步。工作量：文档 10 分钟。
- **ROI**：A 的边际价值 = 把"从簇里一眼看出哪次最省"这个 A.1.6 的核心卖点兑现；B 承认 MVP 只做发现、不做排序。**倾向 A**——"哪次最省/最快"正是 A.1.6 论证聚类价值的那句话；但若近期不打算深耕聚类，B 也站得住。

#### S1-C — 聚类阈值硬编码 —— ⚖️ 待裁决

- **问题描述**：相似度阈值 `0.45` 硬编码在 `clusters.go`（`sim >= 0.45` + doc 注释）。设计 A.1.6 步骤 3 与 A.3 表明确："阈值进 report.yaml 并入 L2 分析参数指纹（`ComputeAnalysisParamsFingerprint` 已有位，加一个字段）"。当前 report.yaml 无此键，`ComputeAnalysisParamsFingerprint` 无 cluster 字段。
- **根因**：实现取了 MVP 捷径。
- **建议方案**：
  - **A**（照设计）：report.yaml 加 `cluster_similarity_threshold: 0.45`（默认值），`AnalysisParams` 加字段并入 `paramsFP`（改阈值 → L2 失效 → 重算聚类）。约 20 行 + 1 测试。
  - **B**（挑战设计，YAGNI）：3 人团队 + 单一使用场景下，一个永远不会调的阈值做成配置项是过度设计。硬编码 0.45、注释写明"如需调整改这里重编译"，符合 KISS。deepdive A.1.6 步骤 3 划掉这条。
- **ROI**：A 兑现设计一致性 + 未来可调；B 少一个配置面。**本团队规模下倾向 B**——除非你预见要频繁 A/B 阈值。无论 A/B，都应让 deepdive 与代码一致。

#### S1-D — 聚类未在 journey-viewer 分组 —— ⚖️ 待裁决

- **问题描述**：设计 A.1.6 步骤 2："journey-viewer 候选列表按簇分组"。`journeys/index.json` 已带 `clusters` 字段，但 `journey-viewer.html` 的候选列表（`renderCandidates`）不消费它——只有 `index.md` 渲染了聚类。
- **根因**：Stage 1 的看板消费端只做了 artifacts 面板（`05e96a5`），聚类的看板侧没做。
- **建议方案**：`journey-viewer.html` 候选列表渲染前，若 `idx.clusters` 非空，先渲染一个 "Task Clusters" 折叠区（每簇 anchor title + 成员链接 + 可复制 compare 命令），再渲染完整候选表。约 25 行 JS + smoke 断言。
- **ROI**：中低。看板的聚类维度目前是"只有 .md 有"。若你认为 `index.md` 的聚类展示已够（多数人从 .md 或 index 进），可标记为 won't-do 并同步 deepdive。

#### S1-H — UserGuide 缺 Stage 1 内容 —— ⚖️ 待裁决（可代做）

- **问题描述**：`docs/UserGuide.md` 与 `docs/UserGuide.zh.md` 完全没有：`vmr diff` 子命令、cache 击穿归因（报告新维度）、"触达文件与资产"章节、任务聚类。CLAUDE.md 硬规则：每个有 `.zh` 兄弟的文档同一改动内同步更新。
- **根因**：Stage 1 只更新了 CHANGELOG，没碰 UserGuide。
- **建议方案**：UserGuide 的 §"Agent task narratives (journeys)" 补 cache 击穿归因 + 触达文件 + 任务聚类三小段；`vmr diff` 在 §"The audit log" 或命令总览处补一段（含"纯 CLI、英文、不落 reports/"）；`.zh` 同步。约 1–1.5 小时（含中译）。
- **ROI**：中低。用户文档欠账，不阻塞功能。**我可以代做**——若你同意，纳入本轮。

#### S1-B / S1-E / S1-M / S1-J / S1-K

- **S1-B（聚类键用 Title）**：`ComputeTaskClusters` 对 `r.Title` 分词做 Jaccard，设计要求"初始指令归一化 token"。Title 对 coding agent 多由 taskseg 从首条指令派生（≈ 截断的初始指令），实测 2 簇质量尚可。**根因**：`InitialInstructionFact` 提取器在 `compare.go`，聚类为省一次全量读 `j-<id>.json` 用了现成的 Title。**建议**：若做 S1-A（member 已要读 `j-<id>.json`），顺带改用初始指令全文；否则维持 Title + 在注释/deepdive 写明这是有意的近似。**ROI**：低。
- **S1-E（tools 新增/移除不归因）**：`ComputeCacheBreak` 第 2 档 `cur.HasSys != prev.HasSys` 对称处理了 sys 的新增/移除；第 3 档 `prev.HasTools && cur.HasTools && ...` 只在两侧都有工具时判 `tools`。工具集从无到有（少见但可能——如后续轮启用工具）是真实 cache 断点却落进第 6 档 `unexplained` 或 `None`。**建议**：第 3 档改 `(prev.HasTools != cur.HasTools) || (prev.HasTools && cur.HasTools && prev.ToolsHash != cur.ToolsHash)`。**ROI**：低（触发少）。可随 S1-L 一起改。
- **S1-M（cron 进聚类）**：`IsNoiseCategory` 只认 `heartbeat`（`cron`/`subagent` 有意不算 noise，见 `journeyindex.go:48` 注释），所以 cron journey 进聚类。实测出现 `[cron:7d9d7b8f… Daily N` 簇（同一日 cron 两次运行）。锚点标题带原始 `[cron:UUID` 前缀，UUID token 使同 cron 必然聚类。**判断**：cron 确实是"真任务"，两次运行聚在一起有一定价值（如首次失败重跑）；但锚点标题应剥掉 `[cron:UUID ]` 装饰。**ROI**：低。
- **S1-J（未来日期版本头）**：`cachebreak.go`（`2026-09-16`）、`digest.go`（`2026-09-15`）——`pi` agent 写的未来日期。CLAUDE.md 说头日期不值得单独修；未来日期比陈旧日期更怪但仍纯装饰。**仅记录。**
- **S1-K（bash 启发式质量）**：`bashApplyRe` 对 `patch -p1 < f.patch` 捕获 `-p1`；op 升级注释说 `write>edit>delete>bash` 但代码里 delete/bash 从不升级。均在 `heuristic:true` 免责范围内。**仅记录。**

---

### 4.3 关于 sub-agent 并行的评估

按你的批复（"内联为主 + 只读校验 fan-out"）：Phase A 的核心是 Stage 1 的深度逐项核验（判断成分重、
需连贯上下文），redesign 组已有 6 份历史评审 + 本轮抽查覆盖。剩余工作（3 个直接修复已完成、若干
待裁决项、Phase B 串行测试、文档更新）无正交可并行的只读校验包值得 fan-out——每个 `pi` worker 冷启动
重推上下文的成本 > 收益。**本轮未启用 sub-agent，主控串行完成**（与 v4 / acceptance 两轮评审的同款结论）。

### 4.4 Phase B 前置观察 —— LLM 端点响应性

- 实测 `report.yaml` 端点 `192.168.0.22:8800`：`/health` 200（20ms）、`/v1/models` = `[agent,cheap,coding,fast]`、
  `cheap` 对 `ping` 200（1.5s，回 "Pong!"，`thought_signature` 显示后端是 Gemini 系）——**端点本身可用**。
- 但一次 `vmr analyze -compare`（默认从 CWD 自动加载 `report.yaml`，触发 LLM 解读层）**卡了 6 分 31 秒**
  （0% CPU、TCP 连接 ESTABLISHED）后才被我 kill。对照 `internal/journey/llm.go`：`llmHTTPTimeout = 120s`
  每次 HTTP 调用，`maxRetries` 次重试带退避——`120s × 3 + backoff ≈ 6–7 分钟`，与实测吻合。
- **不是无限挂死**（有界，最终会以 `status:"failed"` 渲染空段），但 compare 是 2 次 LLM 调用（overall +
  divergence），worst case ~14 分钟、期间零进度输出。
- **对 Phase B 的影响**：进入 Phase B 前请确认端点对**大 prompt（~13K token evidence pack）**也能秒级响应，
  否则单个 UC 会跑十几分钟。**记录为 Phase B 观察项 OBS-1**（是否该给 LLM 解读层加进度输出 / 收紧
  `120s × N` 预算——非 Phase A 缺陷，留待 Phase B 定性）。

---

## 第五部分 · 真实数据 + 真实 LLM 端到端验收测试（Phase B）

**前置**：Phase A 结论为"重构组实质落地、Stage 1 有若干落地未尽项（多为待裁决，无阻塞性大问题）"。
按任务定义 → 执行 Phase B。**Phase B 需你先确认 `report.yaml` 的 LLM 端点对大 prompt 秒级响应
（见 4.4 / OBS-1）。**

（待端点就绪后执行；用例 UC-1..10 与结果回填于此。产物落 `reports/`（zh 主）、`reports-en/`（en 抽测）。）

---

## 附 · 本轮提交清单

| # | 内容 | 文件 |
|---|---|---|
| FIX-1 | gofmt 回归（CI 门禁） | `cmd/vmr/cmd_journey_setup.go`、`internal/i18n/journey_index.go`、`internal/journey/viewmodel_build.go` |
| S1-G | `vmr diff` 用 `Manifest.ToolsHash` 检工具 schema 变更 + 删 stale TODO | `cmd/vmr/cmd_diff.go`、`cmd/vmr/cmd_diff_test.go` |
| S1-F | `journey-compare.html` 补 Cache Breaks 归因表（对齐 `compare-*.md`） | `internal/dashboard/assets/journey-compare.html`、`internal/dashboard/js_test.go` |
| 文档 | 本审计报告 | `docs/future-strategy/analyze_redesign_final_audit_claude-sonnet-5.md` |

---

## 第三部分 · Review 事项完成度总结

（待审计完成后回填。）

---

## 第四部分 · 过程新发现问题与处理

（待审计完成后回填。）

---

## 第五部分 · 真实数据 + 真实 LLM 端到端验收测试（Phase B）

（Phase A 闭环后执行。）
