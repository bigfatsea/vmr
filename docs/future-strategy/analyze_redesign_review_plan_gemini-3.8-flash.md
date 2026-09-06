# analyze 架构重构方案 — 落地情况 Review

<!-- 2026-09-07。基准文档:docs/future-strategy/analyze_architecture_redesign_opus-5.md(下称"方案")。
     review 范围:050ad25(rename internal/story→journey,方案实施起点)至 a36a48c(当时 main)的全部落地代码。
     纪律:事实清楚、方案明确无争议、有十足把握的 → 直接修并留记录;复杂或有争议的 → 记入"待决策",不动手。 -->

## Review 事项清单(按方案结构组织)

### A. Phase 1 — 数据层闭环
- [x] A1. Report2 一步解构为五 macro 切片 + manifest.json,无兼容视图(§3.2、§8.1)— **部分完成,见 T1**
- [x] A2. §3.3 渲染期现算事实下沉 — 已全部完成(tokens_coverage_pct/dur_low_n、CostCoverage、footnotes/disclaimers 注册表、highlights 进 summary 切片、SessionMeta 投影 + journey_link 进 requests/index.json、ts+ts_display 双字段)
- [x] A3. D18/§3.6 journey JSON 自包含 — 已完成(bodies 顶级 blob 表、structure.bodies 已改 `json:"-"` 不再双份序列化、三级 match `exact|normalized|positional`、compaction 前驱摘录、截断口径数据层统一且 RespText 不截)
- [x] A4. §4 目录拓扑 — 已完成(`.parse-cache→.cache/parse` 一项原缺失,本次 review 补齐,见 F2)
- [x] A5. D17 详单文件名 r- 前缀 — 已完成(FileName 单点 + 两个包装函数透传)
- [x] A6. D19 journey 文件名归一 — 已完成(JourneyReportFile 无 journey- 前缀、无 -partial 后缀;partial 进 JSON 字段 + .md banner + 索引行)
- [x] A7. §3.4 manifest 最后写 + orphan 清扫 — 已完成(finishAnalyze 单一出口:骨架刷新→compares 索引→manifest 最后原子写;CleanOrphanJourneys 只作用于 journeys/details/,测试断言 compares/、requests/details|evidence/ 不受影响;失败运行不落 manifest,不污染 cwd)
- [x] A8. D21/§3.8 compares 索引 — 已完成(扫 compares/*.json 派生、每次成功运行重建、子树不进 AllSlicePaths)
- [x] A9. D7/§3.7 人读请求索引整族删除 — 已完成(failed.md/failed.jsonl 保留;requests.go 过期包注释本次修)
- [x] A10. CLI 收敛 — 已完成(main.go 仅 analyze;report/story 子命令、-corpus、-story-only、-html、-redact 均不存在)

### B. Phase 2 — HTML 看板
- [x] B1. go:embed 骨架 + 主题变量 + 零依赖内联 SVG — 已完成(internal/dashboard 叶子包,0600/0700,无外部依赖有测试钉)
- [x] B2. 六页 + #data= + 磁盘=HTTP 布局 + file:// 提示 — 已完成(六页全有 file:// 降级提示;缺省探测 journeys/index.json、compares/index.json)
- [x] B3. D14/§6.6 版本探测 banner — 已完成(versionBehavior 纯函数 + EXPECTED_MANIFEST_FORMAT=11,六页全接;不一致警告不阻断;manifest 缺失独立提示)
- [x] B4. §5.6 跨语言 fmt fixture — 已完成(testdata/fmt_cases.json,fmtutil 与看板 JS 各跑一遍;金额格式化刻意不钉,已在 KNOWN_ISSUES 登记)
- [x] B5. D6/D15 自包含 HTML 与 -html/-redact 删除 — 已完成(render_html*/render_compare_html/toolwaste_html、story/assets 均不存在)
- [x] B6. D9/§6.5 server /reports/* 托管 — 已完成(analytics.serve 默认关;无 api_keys 全树 403 不复用放行型 auth;Clean+前缀校验+逐级 Lstat 拒 symlink+禁目录列表;骨架免鉴权/数据走 Bearer;每条都有独立测试)
- [x] B7. D16 serve_dir 默认 ./reports,不走 rundir,目录缺失仅 404+一次性日志 — 已完成

### C. Phase 3 — ViewModel 与单一渲染路径
- [x] C1. D3/§5.3 不引模板引擎、固定序列化器 — 已完成(renderTable/renderMarkdown 纯 Go;无 text/template)
- [x] C2. D4/§5.2 文案进 VM、i18n 配对改 viewmodel_* — 已完成(archtest 配对已切换;AGENTS.md 已更新;§5.2 草图的偏差——highlights 入 §0 blocks、Blocks 有序列表、去 Aligns——在包注释中有记录且理由成立)
- [x] C3. D11/§5.0 单一渲染路径 — **部分完成,见 T1**(journey/benchmarks/compares/failed.md 均从落盘 JSON 渲染且与 -render-only 同路径;macro 报告的渲染源是 vmr-report.json 单体而非切片)
- [x] C4. D5/§5.4 -render-only 覆盖面 — 已完成(覆盖全部常驻人读产物,作业清单来自 journeys/index.json 不扫目录,骨架幂等刷新,details/evidence 永不物化)
- [x] C5. D10 语言继承 — 已完成(-lang 与 manifest 不一致报错并指引全量重跑,有测试)
- [x] C6. D12 VM 不落盘 — 已完成
- [x] C7. golden 下沉 VM 层;LosslessReconstruction 改为只吃 JSON — 已完成(TestGoldenVMStructure;structure_test 只喂 j-<id>.json 断言 .md 可重建)

### D. Phase 4 — 产物级缓存
- [x] D8a. D8/§7.2 唯一 Digest — **部分完成,见 T2**(report 与 journey 各有一份实现,由差分测试钉死 wire format 一致;标量编码、长度前缀、md5 底座不动均符合方案)
- [x] D8b. §7.1 L2/L3 + §7.4 失效矩阵 — **部分完成,已由本次 review 补齐**(llm_key 自流量 tag 与 LLM identity 原缺,见 F3)
- [x] D8c. -no-cache 常驻旁路 + 冷热一致 + digest 差分测试 — 已完成

### E. 守卫与横切不变量
- [x] E1. §9 新增守卫 — 基本完成(render-only 字节一致、冷热一致、fmt fixture、ts/ts_display、bodies 无孤儿无悬引用、三级 match 与脊柱渲染一致、orphan 清扫及其范围、compares index ≡ 目录,均有具名测试;"VM 无裸字面量"守卫未写成自动化测试,见 T5)
- [x] E2. archtest — 已完成(server 不依赖分析半区、viewmodel 登记入预算表、section 条目已随删除移除)
- [x] E3. §11.1 不变量 — 已完成(0600/0700 全链路、manifest 无指标、切片无交叉引用、ts/ts_display 双字段)
- [x] E4. 文档同步 — **部分完成,见 T3、T4**(UserGuide 双语与 config.example 双语已同步;README 双语的 CLI 名残留本次修复;CHANGELOG 两处失实表述本次修正;v4 Analytics 设计文档大面积过期,未处理)

---

## 执行记录

### 验证手段
- 全仓 `go build` / `go vet` / `go test ./...` 基线全绿;review 后对触碰的包复跑全绿(cmd/vmr、report、journey、ctxgraph、server、dashboard、config、fmtutil、archtest)。
- 端到端冒烟:`vmr analyze -details -o /tmp/out examples/sample-audit.jsonl` — 产物拓扑与方案 §4 逐字一致(manifest.json format=11 最后写、macro/ 五切片、requests/{index.json,failed.*,details r-*、evidence}、journeys/、compares/、六骨架页、.cache/{fingerprint.json,parse/}),权限 0600/0700。
- 逐项核对方式:读实现 + 读对应测试存在性与断言内容 + 抽查 CHANGELOG/UserGuide/AGENTS.md 与代码行为的一致性。

### 本次 review 直接修复(提交记录)
1. **386f92f** — `.parse-cache/` 改名 `.cache/parse/`(F2);L2 分析参数指纹补 llm_key 自流量 tag 与 LLM identity(F3);commitManifest 静默吞错改 stderr 告警(F4);requests.go 包注释重写(F5);KNOWN_ISSUES 路径/CLI/2.79/2.56 现行状态刷新(F6);CHANGELOG 对 vmr-report.json 的失实表述改为如实(F7)。
2. **373ff09** — 删除 viewmodel.go 过期的"过渡期双路径"注释(F8)。
3. **README 同步提交** — README.md/README.zh.md 全部 `vmr story`/`vmr report`/`-corpus` 残留改为 `vmr analyze` 系(F9),示例命令改为可实际运行的 `./vmr analyze -details -o /tmp/out examples/sample-audit.jsonl` 并实跑验证。

---

## 总结

### 一、方案 review 事项结论

| 事项 | 结论 |
|---|---|
| Phase 1 数据层(A1–A10) | **A1/C3 部分完成(D2 偏差,见 T1),其余已全部完成** |
| Phase 2 看板(B1–B7) | **已全部完成**,且安全模型(无 key 硬拒、路径校验、分层鉴权)超出方案最低要求的部分均有测试背书 |
| Phase 3 ViewModel(C1–C7) | **已全部完成**(C3 的"从切片渲染"字面要求归入 T1 一并裁决) |
| Phase 4 缓存(D8a–D8c) | **部分完成**:指纹算法与失效矩阵本体落地且测试充分;缺两项指纹入参(review 已补)、Digest 双实现(见 T2) |
| 守卫与横切(E1–E4) | **基本完成**:E1 差一条低 ROI 守卫(T5);E4 文档欠账最大(T3、T4) |

**总体判断**:方案的四个 Phase 已按裁决(D1–D21)实质落地,守卫体系(§9)覆盖率很高,没有发现数据正确性层面的错误。偏差集中在两处:**vmr-report.json 单体被保留**(T1,方案明说"不保留",且 CHANGELOG/AGENTS.md 都已宣称删除——代码与文档割裂)和**文档欠账**(T3/T4)。

### 部分完成/未完成事项详述

#### T1. `vmr-report.json` 单体保留,与 D2 及全套文档声明相悖 — **待你决策**
- **问题描述**:方案 D2 明确"`vmr-report.json` 不保留,一步解构为切片,无兼容视图"。实际代码(cmd_report.go)在写出 macro/* 五切片的同时**仍然写出 vmr-report.json**,且 macro 报告 Markdown 的渲染源是它(viewmodel_doc.go 的 LoadReport),不是切片;manifest 的切片清单也不盖它。它承载了切片没有的 Meta 事实(records、parse_errors、details_enabled、self_traffic 披露、report_config_path 等)——这是它被保留的现实原因。而 CHANGELOG(本次已改为如实)、AGENTS.md 模块表、README 等均已按"已删除"表述。
- **根因分析**:Group 1A 只交付了切片与 manifest,单一渲染路径接线(3C)时发现 VM 消费的是完整的 Report2 形状,补齐切片侧 Meta 缺口工作量不小,于是保留单体作为渲染源——是一个未登记的务实捷径,不是能力缺失。
- **建议方案**(二选一):
  - **A. 接受保留并登记**(推荐):在方案文档与 KNOWN_ISSUES 登记偏差——切片是对外消费契约,单体是 macro Markdown 的内部渲染源,两者同源于同一次聚合;顺手把单体补进 manifest 的盖章范围(或在 KNOWN_ISSUES 说明其不在准入内),消除"manifest 校验通过但渲染源可被单独篡改"的窄缝。工作量:文档半天 + manifest 一处小改。
  - **B. 落实 D2 原案**:把 Meta 事实补进切片/manifest,VM 构建器改从切片取数,删除单体写出。工作量:2–4 人天,触及全部 viewmodel_*,需要重验 golden。
- **ROI**:方案里切片化的核心收益(消费弹性、渐进加载、看板契约)A 已全部兑现;B 的边际收益只有"消除双账本"这一条,而两份数据同源于同一次聚合 pass,实际漂移风险仅存在于手动篡改场景。**A 的性价比显著更高**,除非你把"JSON 切片是唯一真源"视为必须兑现的架构承诺。

#### T2. Digest 链构造函数存在 report/journey 两份拷贝 — **未解决(低优先)**
- **问题描述**:方案 D8 说"全系统只有一个 Digest 构造函数"。实际 `internal/report/digest.go` 与 `internal/journey/digest.go` 各有一份,cmd/vmr/digest_parity_test.go 用差分测试钉死两者 wire format 一致。
- **根因分析**:report 与 journey 是分析半区内互不依赖的两个包,单独引入共享 leaf 包的成本大于复制十几行代码。
- **建议方案**:若要收敛,唯一自然宿主是下沉到双方都依赖的叶子包(ctxgraph 或 core)。纯机械重构。
- **ROI**:低。差分测试已消除漂移风险,收敛只减少约 40 行重复;若近期无其他理由碰这两个包的依赖关系,不值得动。

#### T3. v4 Analytics 设计文档大面积过期 — **未完成(建议单独立项)**
- **问题描述**:docs/VirtualModelRouter_Design_v4_Analytics.md 仍以 `vmr report`/`vmr story` 双 CLI、`section_*.go` 渲染器、`vmr-requests.md` 全家、`meta.format = 10`、`-corpus` 为"现状"描述(§0、§2 全段);而同文档的看板一节(§6.4 一带)已按新拓扑改写——同一份文档一半新一半旧。该文档自我定位是"读完即可维护与二次开发",过期内容会误导下一个维护者。
- **根因分析**:2D 文档同步组按 grep 清单修了 UserGuide 与部分设计文档,但设计文档 §2 的成段改写超出其授权范围(改它会动段落结构而非词句),被留下了。
- **建议方案**:按"current state, not changelog"纪律整体改写 §1–§2(报告半区)与 §3 的消费侧描述:单入口 analyze、五切片+manifest、ViewModel 层、单一渲染路径、缓存三级模型;把 §2.2 的 Report2 描述改注"内部聚合形状,渲染源"。与 T1 的裁决联动(T1 选 A 则描述保留单体,选 B 则不写)。
- **ROI**:中。半天到一天,防止下一次有人按旧文档改代码;建议与 T1 同批做。

#### T4. 方案提案文档的状态头过期 — **未完成**
- **问题描述**:analyze_architecture_redesign_opus-5.md 头部仍标"设计提案(未实施)",而它已全部实施(含偏差)。方案的 D2"不保留 vmr-report.json"、§3.5".cache/parse"等裁决与代码现状的出入未在该文档反映。
- **根因分析**:方案文档在实施过程中没有被当作"current state"文档回写。
- **建议方案**:更新状态行为"已实施(2026-09),偏差与登记见 review 记录/KNOWN_ISSUES";T1 裁决后同步 D2 的裁决结果。
- **ROI**:高(几行字),随 T3/T1 一起做。

#### T5. "ViewModel 无未 i18n 裸字面量"守卫未写成自动化测试 — **未解决(低优先)**
- **问题描述**:§9 承诺新增该守卫;实际靠人工核对(当前 viewmodel_*.go 中无裸文案字面量,本次已抽查确认)。
- **根因分析**:该守卫需要 AST 扫描式的自定义检查,JSON tag、"left"/"right" 等合法字面量会制造噪音,实施成本与误报治理不成比例,实施中被放弃且未登记。
- **建议方案**:要么在 KNOWN_ISSUES 登记为"刻意不自动化,靠 golden + 抽查",要么写一个只拦"含空格的英文句子字面量"的窄规则测试。
- **ROI**:低。窄规则半小时,误报概率小;不写则登记一句话。

---

### 二、过程中新发现的问题

| # | 问题 | 状态 |
|---|---|---|
| F1 | 全量运行 / -render-only / 缓存链路本身工作正常,冒烟与测试全绿 | (结论,非问题) |
| F2 | `.parse-cache/` 未按方案 §3.5 与 CHANGELOG 宣告归一为 `.cache/parse/` | **已在 review 中解决**(386f92f) |
| F3 | L2 分析参数指纹缺 llm_key 自流量 tag 与 LLM identity 两个入参:加/改 `-llm-key` 会静默命中旧缓存产出错误的自流量排除;`-journey/-compare` 带新 `-llm-addr` 会被 L2 命中静默吞掉解读请求 | **已在 review 中解决**(386f92f,含 4b/4c 两个矩阵测试用例) |
| F4 | commitManifest 静默吞掉 BuildManifest/WriteManifest 错误(manifest 缺失时快照判无效,但用户无感知) | **已在 review 中解决**(386f92f,改 stderr 告警,fail-closed 语义不变) |
| F5 | internal/report/requests.go 包注释仍描述已删除的 vmr-requests.md 全家 | **已在 review 中解决**(386f92f) |
| F6 | KNOWN_ISSUES 多处把 `.parse-cache`、`vmr report`/`vmr story`、`vmr-requests.json` 当现行状态引用;2.79(别名 flag 漂移)随别名拆除已失效未销;2.56(解压三遍)未补 L2 缓存落地后的现状注记 | **已在 review 中解决**(386f92f) |
| F7 | CHANGELOG Breaking 条目宣称 "`vmr-report.json` is deconstructed" 与代码不符 | **已在 review 中解决**(386f92f 改为如实表述;T1 裁决后再按结果改写) |
| F8 | viewmodel.go 头部"过渡期 vm 前缀双路径"注释在旧渲染器删除后失效 | **已在 review 中解决**(373ff09) |
| F9 | README.md/README.zh.md 共 15+ 处 `vmr story`/`vmr report`/`-corpus` 残留,含一条不可运行的示例命令 | **已在 review 中解决**(README 同步提交,示例实跑验证) |
| F10 | 3C 移交清单的 vm 前缀 helper 改名回退与 vmSkippedAttemptsNote 去重未执行(HANDOVER_P3_NOTES §3) | **未解决** — 见下 |
| F11 | render-only 重渲染 compares/*.md 与 journeys/details/*.md 时保留旧文件的 `## LLM ` 段,compare/journey JSON 的落盘语言可能与 manifest.lang 不同(跨多次运行累积),重渲染语言可能混排 | **未解决** — 见下 |
| F12 | 看板 JS `FmtCurrency` 恒定 `$` 前缀,与 `-currency CNY` 的 Go 侧展示不一致 | **未解决(已登记,不行动)** — KNOWN_ISSUES 已作为"刻意不钉"的裁决登记,前端显示币种属产品议题 |

#### F10. 3C 移交清单未清完(vm 前缀 helper)— **未解决**
- **问题描述**:HANDOVER_P3_NOTES §3 明列:删旧路径后把 `vmCostCell` 等 vm 前缀过渡副本改回原名并删 legacy 原件;`vmSkippedAttemptsNote` 改为直接调用存活的 `renderSkippedAttemptsNote`。实际 legacy 原件已删、副本未改名。
- **根因分析**:3C 的白名单禁止其修改 archtest,改名需要同步动 `file_sizes_test`/`func_sizes_test` 的 `文件:函数名` 键表,被跳过且未回写主控。
- **建议方案**:一次性机械改名(约 20 个函数,archtest 键表同步),`vmSkippedAttemptsNote` 就近调用存活函数。单独一个 cleanup commit。
- **ROI**:低。纯命名一致性,无行为风险;适合随下一次碰 internal/report 的变更顺带做,不值得单独立项。

#### F11. 渲染语言跨运行累积产物可能混排 — **未解决**
- **问题描述**:compares/*.json、journeys/details/j-<id>.json 是跨调用累积产物,各自携带生成时的语言;`-render-only`(及全量运行的 renderAllFromDisk)统一以 manifest.lang 重渲染 Markdown,并用 `## LLM ` 标记保留旧文件的 LLM 段(旧语言)。混着换过语言的目录会得到中英混排的 Markdown。
- **根因分析**:D10 规定"渲染继承 JSON 语言",但累积产物的"JSON 语言"不是一个值;LLM 段保留是既有行为,方案未覆盖。
- **建议方案**:最小修法是重渲染时逐文件采用该 JSON 自身的 lang 字段(compare json 里有 label 语言吗?若没有则加字段,属 schema 加性变更);LLM 段按自身语言保留无需处理。也可以裁决为"可接受,换语言请全量重跑并清理"。
- **ROI**:低。触发条件苛刻(同一目录换语言 + 累积产物),建议先登记不修。

#### F12. 看板金额显示恒 `$` — **不行动**
KNOWN_ISSUES 已把"Go 与 JS 金额格式化是两套行为"登记为裁决(2026-09);币种符号未涵盖在该条内,但同属"前端展示策略"范畴。若要支持非 USD 展示,应作为独立产品议题(切片已有币种与汇率事实,前端缺的只是符号与舍入策略),不归入本次重构。
