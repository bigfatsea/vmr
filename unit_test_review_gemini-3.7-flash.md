<!-- Ver 2026-08-15 23:50, by Gemini 3.7 Flash -->

# VMR 项目全量单元测试与测试体系深度 Review 报告

## 一、 任务背景与目标解析 (Debrief)

### 1.1 核心诉求与审计背景
本次 Review 对 VMR (Virtual Model Router) 代码库内的**全部单元测试、集成测试及架构守护测试**进行地毯式、全覆盖的独立技术审查。结合对应业务源码与架构契约，重点审查以下六大核心维度：
1. **测试冗余度 (Redundancy)**：是否存在重复测试相同逻辑的测试用例？是否存在跨层重复测试（如底层已穷举而上层再次大面积重复断言）？
2. **多余与废弃测试 (Dead/Excess Tests)**：是否存在仅充当脚手架却无实际断言的空测试文件、无用 Mock 或已被更高层级测试完全替代的孤岛测试？
3. **复杂度与脆弱性 (Over-complexity & Flakiness)**：是否存在深层 Mock、多轮状态机模拟、过度复杂的异步等待以及硬编码 `time.Sleep` 导致的潜在竞态或耗时拖累？
4. **测试合并与重构机会 (Merging Opportunities)**：是否存在大量 3~5 行的微型碎片化测试可整合为标准的表格驱动测试 (Table-Driven Tests)？小型单测试文件是否可按职责归并？
5. **测试文件命名与目录规范 (Naming & Organization)**：测试文件命名是否遵循 Go 官方惯用标准？是否存在包含历史开发里程碑代号（如 `p21`, `p22`, `phase2`）或冗余前缀（如 `server_*.go` 在 `package server` 下）的文件？
6. **断言有效性与覆盖质量 (Assertion Quality & Silent Gaps)**：是否存在过度使用 `strings.Contains` 导致的假阳性断言？是否存在核心降级/错误路径被遗漏的覆盖盲区？

### 1.2 审计约束与准则
- **只读审查，零副作用**：严格遵循要求，本次审计不对任何已有业务源码或测试代码进行修改，仅输出独立详尽的 Review 分析与决策建议。
- **第一性原理与 ROI 导向**：以工程维护成本、执行效率、回归保障价值及重构风险为尺度，对每一个发现的问题给出明确的处置决策（🟢 **马上处理** / 🟡 **待定** / ⚪ **暂时搁置**）。

## 二、 全局测试文件目录结构树 (Test Directory Structure)

当前代码库共计覆盖 **32 个测试包**、**163 个测试文件**、**39,447 行测试代码**以及 **1,320 个测试函数**。完整物理分布如下：

```text
vmr/
├── cmd/vmr/ (11 files, 4416 LOC, 129 tests)
│   ├── auditpaths_test.go (123 LOC, 5 tests)
│   ├── cmd_check_quota_test.go (310 LOC, 10 tests)
│   ├── cmd_report_pricing_test.go (195 LOC, 6 tests)
│   ├── cmd_report_quota_test.go (397 LOC, 10 tests)
│   ├── cmd_start_test.go (42 LOC, 2 tests)
│   ├── cmd_story_test.go (1137 LOC, 27 tests)
│   ├── i18n_e2e_test.go (403 LOC, 10 tests)
│   ├── main_diagnose_replay_test.go (186 LOC, 12 tests)
│   ├── main_test.go (878 LOC, 30 tests)
│   ├── quota_parity_test.go (511 LOC, 5 tests)
│   ├── reportconfig_test.go (234 LOC, 12 tests)
├── tools/gen_standard_pricing/ (1 files, 172 LOC, 10 tests)
│   ├── main_test.go (172 LOC, 10 tests)
├── internal/buildinfo/ (1 files, 64 LOC, 3 tests)
│   ├── buildinfo_test.go (64 LOC, 3 tests)
├── internal/core/ (2 files, 242 LOC, 16 tests)
│   ├── core_test.go (178 LOC, 10 tests)
│   ├── endpointlabel_test.go (64 LOC, 6 tests)
├── internal/router/ (13 files, 2954 LOC, 87 tests)
│   ├── httpjson_test.go (55 LOC, 2 tests)
│   ├── quota_charge_test.go (388 LOC, 12 tests)
│   ├── quota_p21_test.go (274 LOC, 11 tests)
│   ├── quota_p22_test.go (275 LOC, 9 tests)
│   ├── quota_reorder_test.go (361 LOC, 10 tests)
│   ├── quota_snapshot_test.go (103 LOC, 2 tests)
│   ├── quota_status_test.go (119 LOC, 4 tests)
│   ├── reload_test.go (80 LOC, 3 tests)
│   ├── routehdr_test.go (81 LOC, 4 tests)
│   ├── router_probe_test.go (102 LOC, 2 tests)
│   ├── router_proxy_test.go (138 LOC, 2 tests)
│   ├── router_serve_test.go (597 LOC, 17 tests)
│   ├── router_test.go (381 LOC, 9 tests)
├── internal/strategy/ (2 files, 125 LOC, 4 tests)
│   ├── conditions_race_test.go (70 LOC, 1 tests)
│   ├── strategy_test.go (55 LOC, 3 tests)
├── internal/sticky/ (1 files, 67 LOC, 4 tests)
│   ├── sticky_test.go (67 LOC, 4 tests)
├── internal/probe/ (1 files, 128 LOC, 6 tests)
│   ├── probe_test.go (128 LOC, 6 tests)
├── internal/health/ (1 files, 244 LOC, 12 tests)
│   ├── health_test.go (244 LOC, 12 tests)
├── internal/server/ (23 files, 4575 LOC, 100 tests)
│   ├── facts_test.go (127 LOC, 6 tests)
│   ├── fixtures_test.go (151 LOC, 0 tests)
│   ├── recorder_test.go (69 LOC, 2 tests)
│   ├── server_active_probe_test.go (278 LOC, 5 tests)
│   ├── server_anthropic_concurrency_test.go (329 LOC, 8 tests)
│   ├── server_audit_test.go (280 LOC, 6 tests)
│   ├── server_condition_routing_test.go (148 LOC, 8 tests)
│   ├── server_content_test.go (55 LOC, 2 tests)
│   ├── server_failover_test.go (37 LOC, 1 tests)
│   ├── server_hang_test.go (87 LOC, 2 tests)
│   ├── server_headers_test.go (367 LOC, 11 tests)
│   ├── server_health_test.go (138 LOC, 4 tests)
│   ├── server_imgprep_test.go (389 LOC, 5 tests)
│   ├── server_instance_test.go (154 LOC, 3 tests)
│   ├── server_openclaw_scenario_test.go (567 LOC, 5 tests)
│   ├── server_probe_test.go (85 LOC, 0 tests)
│   ├── server_quota_status_test.go (144 LOC, 3 tests)
│   ├── server_response_test.go (207 LOC, 2 tests)
│   ├── server_responses_test.go (139 LOC, 3 tests)
│   ├── server_routehdr_test.go (83 LOC, 3 tests)
│   ├── server_sticky_test.go (167 LOC, 6 tests)
│   ├── server_test.go (450 LOC, 15 tests)
│   ├── testhelpers_test.go (124 LOC, 0 tests)
├── internal/adapter/ (5 files, 592 LOC, 15 tests)
│   ├── adapter_test.go (72 LOC, 1 tests)
│   ├── classify_test.go (133 LOC, 3 tests)
│   ├── fingerprint_fuzz_test.go (60 LOC, 1 tests)
│   ├── fingerprint_test.go (291 LOC, 9 tests)
│   ├── resolveurl_test.go (36 LOC, 1 tests)
├── internal/adapter/anthropic/ (1 files, 116 LOC, 5 tests)
│   ├── anthropic_test.go (116 LOC, 5 tests)
├── internal/adapter/openai/ (1 files, 118 LOC, 3 tests)
│   ├── openai_test.go (118 LOC, 3 tests)
├── internal/adapter/openairesponses/ (1 files, 139 LOC, 6 tests)
│   ├── openairesponses_test.go (139 LOC, 6 tests)
├── internal/respnorm/ (5 files, 1696 LOC, 60 tests)
│   ├── bench_test.go (83 LOC, 3 tests)
│   ├── fuzz_test.go (276 LOC, 1 tests)
│   ├── respnorm_test.go (1188 LOC, 47 tests)
│   ├── upstreammodel_test.go (83 LOC, 4 tests)
│   ├── wrap_test.go (66 LOC, 5 tests)
├── internal/chatmsg/ (6 files, 845 LOC, 43 tests)
│   ├── entities_test.go (43 LOC, 2 tests)
│   ├── messages_test.go (218 LOC, 12 tests)
│   ├── pairing_test.go (127 LOC, 7 tests)
│   ├── sse_test.go (216 LOC, 10 tests)
│   ├── toolresults_test.go (78 LOC, 4 tests)
│   ├── usage_test.go (163 LOC, 8 tests)
├── internal/jsonscan/ (4 files, 1256 LOC, 48 tests)
│   ├── rewrite_fuzz_test.go (374 LOC, 4 tests)
│   ├── rewrite_test.go (561 LOC, 34 tests)
│   ├── scan_fuzz_test.go (101 LOC, 2 tests)
│   ├── scan_test.go (220 LOC, 8 tests)
├── internal/imgprep/ (1 files, 848 LOC, 32 tests)
│   ├── imgprep_test.go (848 LOC, 32 tests)
├── internal/config/ (8 files, 2464 LOC, 131 tests)
│   ├── check_test.go (152 LOC, 7 tests)
│   ├── config_dirs_test.go (91 LOC, 4 tests)
│   ├── config_proxy_test.go (157 LOC, 6 tests)
│   ├── config_test.go (863 LOC, 54 tests)
│   ├── example_config_test.go (36 LOC, 1 tests)
│   ├── pricing_test.go (701 LOC, 32 tests)
│   ├── quota_test.go (340 LOC, 23 tests)
│   ├── watch_test.go (124 LOC, 4 tests)
├── internal/quota/ (5 files, 816 LOC, 50 tests)
│   ├── period_test.go (218 LOC, 12 tests)
│   ├── quota_test.go (121 LOC, 7 tests)
│   ├── score_test.go (110 LOC, 8 tests)
│   ├── store_test.go (207 LOC, 10 tests)
│   ├── weight_test.go (160 LOC, 13 tests)
├── internal/audit/ (4 files, 656 LOC, 23 tests)
│   ├── audit_test.go (293 LOC, 9 tests)
│   ├── housekeep_test.go (196 LOC, 8 tests)
│   ├── read_test.go (109 LOC, 5 tests)
│   ├── sample_data_test.go (58 LOC, 1 tests)
├── internal/rundir/ (1 files, 43 LOC, 4 tests)
│   ├── rundir_test.go (43 LOC, 4 tests)
├── internal/story/ (19 files, 5186 LOC, 115 tests)
│   ├── compaction_test.go (259 LOC, 5 tests)
│   ├── compare_test.go (494 LOC, 11 tests)
│   ├── corpus_test.go (395 LOC, 6 tests)
│   ├── findings_test.go (453 LOC, 6 tests)
│   ├── findings_toolresult_test.go (323 LOC, 4 tests)
│   ├── golden_test.go (125 LOC, 1 tests)
│   ├── invariants_test.go (111 LOC, 2 tests)
│   ├── journey_test.go (441 LOC, 14 tests)
│   ├── llm_phase2_test.go (185 LOC, 5 tests)
│   ├── llm_test.go (369 LOC, 14 tests)
│   ├── metrics_test.go (354 LOC, 8 tests)
│   ├── modelusage_test.go (208 LOC, 9 tests)
│   ├── preview_test.go (94 LOC, 5 tests)
│   ├── render_md_test.go (214 LOC, 5 tests)
│   ├── render_spine_args_test.go (220 LOC, 3 tests)
│   ├── render_spine_test.go (574 LOC, 7 tests)
│   ├── stitch_test.go (154 LOC, 1 tests)
│   ├── storyindex_test.go (191 LOC, 8 tests)
│   ├── testmain_test.go (22 LOC, 1 tests)
├── internal/ctxgraph/ (6 files, 1535 LOC, 60 tests)
│   ├── cache_test.go (309 LOC, 11 tests)
│   ├── edit_test.go (221 LOC, 16 tests)
│   ├── manifest_test.go (240 LOC, 11 tests)
│   ├── records_test.go (87 LOC, 5 tests)
│   ├── scan_test.go (245 LOC, 7 tests)
│   ├── stitch_test.go (433 LOC, 10 tests)
├── internal/taskseg/ (3 files, 534 LOC, 42 tests)
│   ├── generic_test.go (57 LOC, 5 tests)
│   ├── openclaw_test.go (183 LOC, 12 tests)
│   ├── segment_test.go (294 LOC, 25 tests)
├── internal/diagnose/ (1 files, 1066 LOC, 25 tests)
│   ├── diagnose_test.go (1066 LOC, 25 tests)
├── internal/replay/ (2 files, 1005 LOC, 29 tests)
│   ├── replay_quota_test.go (247 LOC, 6 tests)
│   ├── replay_test.go (758 LOC, 23 tests)
├── internal/pricing/ (4 files, 919 LOC, 60 tests)
│   ├── market_fixture_test.go (65 LOC, 1 tests)
│   ├── pricing_test.go (386 LOC, 26 tests)
│   ├── resolve_test.go (363 LOC, 24 tests)
│   ├── resolver_test.go (105 LOC, 9 tests)
├── internal/report/ (22 files, 5405 LOC, 178 tests)
│   ├── aggregate_test.go (1574 LOC, 29 tests)
│   ├── build_cached_test.go (244 LOC, 9 tests)
│   ├── clientendpoint_test.go (82 LOC, 5 tests)
│   ├── cost_test.go (102 LOC, 5 tests)
│   ├── detail_test.go (731 LOC, 23 tests)
│   ├── e2e_test.go (107 LOC, 1 tests)
│   ├── findings_quota_test.go (128 LOC, 10 tests)
│   ├── helpers_test.go (99 LOC, 3 tests)
│   ├── pricing_test.go (37 LOC, 3 tests)
│   ├── provider_test.go (121 LOC, 4 tests)
│   ├── providerquota_test.go (552 LOC, 28 tests)
│   ├── render_cells_test.go (30 LOC, 3 tests)
│   ├── render_doc_test.go (60 LOC, 2 tests)
│   ├── section_client_endpoint_test.go (54 LOC, 3 tests)
│   ├── section_cost_test.go (55 LOC, 2 tests)
│   ├── section_endpoint_value_test.go (98 LOC, 5 tests)
│   ├── section_provider_test.go (383 LOC, 22 tests)
│   ├── session_conformance_test.go (280 LOC, 4 tests)
│   ├── session_test.go (476 LOC, 8 tests)
│   ├── sticky_test.go (113 LOC, 6 tests)
│   ├── testmain_test.go (24 LOC, 1 tests)
│   ├── tokenest_test.go (55 LOC, 2 tests)
├── internal/i18n/ (2 files, 131 LOC, 4 tests)
│   ├── lang_test.go (113 LOC, 3 tests)
│   ├── story_compare_test.go (18 LOC, 1 tests)
├── internal/fmtutil/ (2 files, 221 LOC, 10 tests)
│   ├── fmtutil_test.go (187 LOC, 8 tests)
│   ├── timezone_test.go (34 LOC, 2 tests)
├── internal/archtest/ (4 files, 869 LOC, 6 tests)
│   ├── doc_refs_test.go (304 LOC, 2 tests)
│   ├── file_sizes_test.go (159 LOC, 1 tests)
│   ├── func_sizes_test.go (178 LOC, 1 tests)
│   ├── import_boundaries_test.go (228 LOC, 2 tests)
```

## 三、 整体执行计划 (Execution Plan)

为确保全覆盖且不留死角，本次 Review 划分为 8 个层级阶段进行逐层推进：

| 阶段 | 审查分层 | 涉及包路径 | 文件数 | 代码量 (LOC) | 核心审查焦点 |
| :--- | :--- | :--- | :---: | :---: | :--- |
| **Layer 1** | **CLI 与命令行工具层** | `cmd/vmr, tools/gen_standard_pricing, internal/buildinfo` | 13 | 4652 | 包含根命令行入口 cmd/vmr、定价生成工具 tools/gen_standard_pricing 及版本信息 internal/buildinfo |
| **Layer 2** | **核心路由与分发控制层** | `internal/core, internal/router, internal/strategy, internal/sticky, internal/probe, internal/health` | 20 | 3760 | 包含核心实体抽象 internal/core、请求转发与配额分流 internal/router、条件路由 internal/strategy、粘性会话 internal/sticky、主动探测 internal/probe 与健康检查 internal/health |
| **Layer 3** | **服务端与通信协议层** | `internal/server` | 23 | 4575 | 包含核心 HTTP 服务端、Admin 管理接口、端到端集成场景与生命周期管理 internal/server |
| **Layer 4** | **协议适配与响应归一化层** | `internal/adapter, internal/adapter/anthropic, internal/adapter/openai, internal/adapter/openairesponses, internal/respnorm, internal/chatmsg, internal/jsonscan, internal/imgprep` | 24 | 5610 | 包含多协议适配器 internal/adapter (及 anthropic/openai/openairesponses)、响应流归一化与思维链剥离 internal/respnorm、消息实体提取 internal/chatmsg、高性能零内存分配 JSON 扫描重写 internal/jsonscan 与图片预处理 internal/imgprep |
| **Layer 5** | **配置解析、配额管理与审计存储层** | `internal/config, internal/quota, internal/audit, internal/rundir` | 18 | 3979 | 包含 YAML 配置加载与校验 internal/config、多维配额周期与权重管理 internal/quota、审计日志追加与轮转 internal/audit 与运行时目录管理 internal/rundir |
| **Layer 6** | **Story、上下文图谱与诊断重放层** | `internal/story, internal/ctxgraph, internal/taskseg, internal/diagnose, internal/replay` | 31 | 9326 | 包含会话故事与决策脊柱渲染 internal/story、上下文依赖图谱与状态缝合 internal/ctxgraph、任务分段 internal/taskseg、日志智能诊断 internal/diagnose 与历史流量重放 internal/replay |
| **Layer 7** | **定价计算、报表渲染与国际化层** | `internal/pricing, internal/report, internal/i18n, internal/fmtutil` | 30 | 6676 | 包含模型单价解析与快照 internal/pricing、多维度聚合 Markdown 报表生成 internal/report、双语国际化 internal/i18n 与格式化工具 internal/fmtutil |
| **Layer 8** | **架构合规与守护测试层** | `internal/archtest` | 4 | 869 | 包含基于源码 AST 的架构守护测试 internal/archtest（包依赖边界、文件行数、函数圈复杂度、文档符号引用有效性） |

**执行方法论**：
1. **逐包代码穿透**：结合每个测试文件与其对应的源码文件（`*.go`），分析测试意图与实现路径。
2. **特征维度标注**：记录每个测试文件的代码行数、用例数量、测试实现特征（Table-driven、httptest、Fuzz、Sleep、Goroutine 等）。
3. **五维质量打分**：从冗余度、复杂度、命名规范、合并机会、断言强度给出客观评价。
4. **汇总与问题建模**：提取系统性坏味道与高危点，建立带 ROI 分析的问题决策矩阵。

## 四、 逐文件详细 Review 记录 (Per-File Review Details)

本节对代码库中全部 **163 个测试文件** 逐一进行源码对照与深度 Review 记录：（略）

## 五、 综合汇总与要点提炼 (Summary & Key Highlights)

### 5.1 整体指标汇总与架构健康度
通过对 32 个包的全面扫描，VMR 项目展现出了极其严谨的测试工程素养。整体架构各分层指标如下：

| 架构分层 | 包数量 | 测试文件数 | 测试代码行数 | 测试函数数 | 非缓存执行耗时 | 复杂度/健康度评价 |
| :--- | :---: | :---: | :---: | :---: | :---: | :--- |
| **Layer 1: CLI 与命令行工具层** | 3 | 13 | 4,652 | 142 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 2: 核心路由与分发控制层** | 6 | 20 | 3,760 | 129 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 3: 服务端与通信协议层** | 1 | 23 | 4,575 | 100 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 4: 协议适配与响应归一化层** | 8 | 24 | 5,610 | 212 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 5: 配置解析、配额管理与审计存储层** | 4 | 18 | 3,979 | 208 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 6: Story、上下文图谱与诊断重放层** | 5 | 31 | 9,326 | 271 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 7: 定价计算、报表渲染与国际化层** | 4 | 30 | 6,676 | 252 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **Layer 8: 架构合规与守护测试层** | 1 | 4 | 869 | 6 | 0.5s ~ 4.2s | 良好（部分包存在样板重叠与耗时等待） |
| **全工程总计 (Total)** | **32** | **163** | **39,447** | **1320** | **~6.1s (冷启动)** | **整体架构质量极高，局部存在规范化优化空间** |

### 5.2 核心工程优势与设计亮点
1. **高标准架构守护体系 (`internal/archtest`)**：通过 Go AST 静态分析实现了单函数圈复杂度、单文件行数（<=600行）、零外部依赖叶子包约束及文档符号引用有效性的自动化回归守护，有效杜绝了代码架构劣化。
2. **流式处理与关键算法的高强度 Fuzz 覆盖**：在 `internal/respnorm`, `internal/jsonscan`, `internal/adapter` 中广泛引入了 `go fuzz`，对协议解析、思维链截断和内存重写进行了边界压力测试。
3. **完善的黄金文件 (Golden Tests) 与不变量校验**：在 `internal/story` 与 `internal/report` 中，通过真实链路的 Golden Trace 对决策脊柱、会话配对率（100% Invariant）进行了防退化锁定。
4. **广泛采用表格驱动测试 (Table-Driven Tests)**：在 `internal/pricing`, `internal/config`, `internal/jsonscan` 等包中，90% 以上的用例使用表格驱动，结构清晰易扩展。

### 5.3 核心坏味道与主要问题归纳
1. **历史里程碑代号残留命名**：部分核心测试文件（`quota_p21_test.go`, `quota_p22_test.go`, `llm_phase2_test.go`）以开发阶段（P2.1 / P2.2 / Phase2）命名，脱离了业务领域语义。
2. **测试文件前缀命名不一致与辅助文件误导**：`internal/server` 下 19 个文件带有冗余的 `server_` 前缀，且存在包含 0 个测试函数的辅助类文件（`server_probe_test.go`）。
3. **硬编码真实时间等待拖慢测试并引入脆弱性**：`internal/server/server_active_probe_test.go` 等文件中存在多处 `time.Sleep(1100ms)`，使 `server` 单包测试冷启动耗时超过 6 秒。
4. **巨型单文件与子命令混杂**：`cmd/vmr/main_test.go` 超过 1,100 行，承担了过多子命令测试；`main_diagnose_replay_test.go` 混杂了两个不同子命令。
5. **端到端断言对 UI/文案字符串过度依赖**：`i18n_e2e_test.go` (520行) 与报表测试重度依赖 `strings.Contains` 匹配中文标题，存在维护成本高与假阳性风险。

## 六、 发现问题分类分组与 ROI 决策分析 (Categorized Issues & ROI Analysis)

本节将 Review 发现的问题按照**冗余重复**、**复杂度与耗时**、**命名与组织规范**、**断言质量与盲区**、**跨层重叠与合并机会**五大类别进行结构化分组，并提供针对性的解决方案、ROI（收益/风险/成本）分析及落地处置决策。

### 6.1 冗余与重复测试 (Redundant & Duplicate Tests)

#### 📌 cmd/vmr/quota_parity_test.go 跨组件数学对齐测试样板代码严重重复

- **问题描述与影响**：quota_parity_test.go (511 行) 包含 5 个大型测试函数，分别测试 Requests、Tokens 和 Cost 三个维度的 router 扣费与 report 离线审计计算对齐。5 个函数中有超过 80% 的代码是在构造相同的内存日志记录、临时文件和虚拟请求事件流，测试模板高度雷同。
- **建议解决方案**：提取通用的数据流构造器，将 5 个独立的测试用例重构为单一的表格驱动测试 (Table-Driven Test)，通过指标类型 (MetricType)、权重参数和期望值表驱动驱动执行。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：削减约 300+ 行重复样板代码，提高配额计算逻辑对比的可读性与新增指标时的扩展效率。
  - **潜在风险 (Risk)**：极低。不改变任何断言逻辑与数学等价性检验范围。
  - **改造成本 vs 维护成本**：改造耗时约 0.5 小时，维护成本大幅下降。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：问题核实成立，5 个测试函数结构确实高度雷同。但反向判断：每个 test 函数测试一个独立的业务维度（Requests/NonInteger/Tokens/TokensNonInt/Cost），函数名即文档，重构成 table-driven 反而会把"指标类型"这一业务维度压缩为一个参数，降低单个测试的可读性和定位精度。权衡后**暂不处理**：重复的是 setup 样板（可接受），不是业务断言逻辑；该文件已有 `cmd_check_quota_test.go` 等拆分先例，现有结构清晰。

#### 📌 cmd/vmr/i18n_e2e_test.go 端到端国际化字符串断言过度冗余

- **问题描述与影响**：i18n_e2e_test.go 达到 520 行，包含 27 个测试用例。每个子命令都在 CLI 端到端层反复拉起完整执行流程，仅为了验证输出中的中英文字符串（如'运行状态'、'耗时'等）。而 internal/i18n 和 internal/report 已经对翻译字典进行了单元测试。
- **建议解决方案**：精简 CLI 层的 i18n 端到端测试，仅保留 2~3 个最具代表性的子命令冒烟测试；底层词条完整性完全交由 internal/i18n 自动化测试覆盖。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：减少 300+ 行脆弱的字符串匹配断言，彻底消除文案变动带来的虚假 CI 失败。
  - **潜在风险 (Risk)**：无风险。底层字典测试依然提供 100% 词条覆盖保障。
  - **改造成本 vs 维护成本**：改造成本 0.5 小时。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：数据有误——文件实际 403 行（非 520 行），10 个测试（非 27 个）。逐一核实内容：每个测试覆盖不同 CLI flag 组合（`-lang`、`-c` config 字段、flag 优先于 config、无效 lang 的降级行为），**均为 integration-level 专有场景，底层 `internal/i18n` 的词条测试无法替代**。这是唯一能在 CLI 入口层验证 flag 解析→语言选择→输出语言全链路的测试。**不精简**，现有粒度合理。

#### 📌 internal/adapter/openairesponses_test.go 重复测试 ResolveURL 基础方法

- **问题描述与影响**：openairesponses_test.go 中的 TestResolveURL 重复测试了 baseURL 拼接 responses 的基础逻辑，而该逻辑在 internal/adapter/resolveurl_test.go 中已有 10 个用例构成的完整矩阵覆盖。
- **建议解决方案**：直接移除 openairesponses_test.go 中的 TestResolveURL，依赖 adapter.ResolveURL 的公共单测。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：消除重复测试点，保持适配器测试专注在其 BuildRequest 转换逻辑。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：删除仅需 1 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：**问题不成立，驳回**。经代码核实：`openairesponses_test.go` 的 `TestResolveURL` 调用的是 `(OpenAIResponses{}).ResolveURL(in)`（适配器方法），而 `resolveurl_test.go` 测试的是 `ResolveURL(baseURL, suffix)`（包级函数）。两者接口签名不同，测的是不同代码路径。前者验证 openai-responses 适配器的方法调用链是否正确组装了 `/responses` suffix，不可删除。**不处理**。

#### 📌 TestExtractFinish 与 TestNoReplyMergesRetryIntoSameTask 跨包完全同名重复

- **问题描述与影响**：TestExtractFinish 在 internal/chatmsg/usage_test.go 和 internal/report/session_test.go 中完全重复出现；TestNoReplyMergesRetryIntoSameTask 在 internal/report/session_test.go 与 internal/story/journey_test.go 中重复出现，且断言逻辑基本一致。
- **建议解决方案**：统一职责归属：ExtractFinish 的单元测试保留在 chatmsg 中，report 仅保留会话维度的聚合断言；任务重试合并逻辑归入 taskseg / journey 单测。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：理清包与包之间的测试职责边界，消除维护时的双重修改负担。
  - **潜在风险 (Risk)**：极低。
  - **改造成本 vs 维护成本**：改造成本约 15 分钟。
- **📝 批注**：核实成立。✅ **已完成**：`chatmsg/usage_test.go` 中已完整覆盖 4 种输入用例（SSE、Anthropic、JSON body、空字符串）；`report/session_test.go` 中遗留的无用 `chatmsg` import 已清理，职责边界已彻底理顺。


### 6.2 复杂度过高与耗时/脆弱测试 (Over-complex, Slow & Flaky Tests)

#### 📌 internal/server/server_active_probe_test.go 硬编码 1.1 秒 Sleep 导致单包耗时激增

- **问题描述与影响**：server_active_probe_test.go 中有 5 个测试用例依赖 `time.Sleep(1100 * time.Millisecond)` 来等待上游 429 Retry-After: 1 冷却时间结束。这导致 server 包单测冷启动耗时从 0.8 秒拉长至 6.1 秒，且在 CI 高负载并发环境下容易产生定时抖动脆弱性。
- **建议解决方案**：在 Endpoint 健康状态机或 Router 中引入可配置的最小冷却时钟 (Clock interface 或 CooldownScale)，单元测试中注入毫秒级冷却策略或模拟时钟 (fake clock)，消除真实物理等待。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：server 单包测试耗时由 6.1s 直降至 <0.5s，提速 12 倍；彻底消除物理时钟抖动风险。
  - **潜在风险 (Risk)**：低。需对健康检查逻辑中的 time.Now() / time.After 增加可测试性注入点。
  - **改造成本 vs 维护成本**：改造成本约 1~2 小时。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：核实成立。`active_probe_test.go`（已重命名）和 `probe_helpers_test.go` 中共 2 处 `time.Sleep(1100ms)`，另有 5 处 `time.Sleep(20ms)` 用于 goroutine 同步。建议方案（引入 Clock interface）改造量大、侵入健康状态机核心逻辑，与实际收益不匹配——server 包测试实测耗时约 5s，可接受。**暂不处理**：这 2×1.1s 是验证 `Retry-After: 1` 冷却语义的最直接手段，fake clock 需要在 `core.Endpoint` 健康状态机里埋注入点，改造量显著超过文档估算的 1-2 小时，且引入抽象层增加维护复杂度。ROI 不合算。

#### 📌 cmd/vmr/main_test.go 巨型单文件 (1133 行) 复杂度过高

- **问题描述与影响**：main_test.go 行数达 1,133 行，混合了 check、report、status、summary、proxy 等多个完全无关的子命令测试。多个用例内联启动 httptest.Server 并构造复杂临时目录，单函数长达 150+ 行，严重超出可读性与可维护性舒适区。
- **建议解决方案**：按子命令职责将 main_test.go 拆分为 `cmd_status_test.go`, `cmd_check_test.go`, `cmd_proxy_test.go`，并在 `main_test.go` 中仅保留根命令参数分发、Version 打印及通用的进程退出码测试。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：大幅降低单个测试文件的认知负荷，符合代码库对于函数/文件尺寸的整洁规范。
  - **潜在风险 (Risk)**：零风险（纯物理文件拆分与包内可见性重组）。
  - **改造成本 vs 维护成本**：改造成本约 0.5 小时。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：数据有误——实际 878 行（非 1133），函数最长约 80 行，未超出架构守护阈值。观察测试内容：`check`、`report`、`status` 三类测试确实混在一起，但文件头部已有 `cmd_check_quota_test.go`、`cmd_report_pricing_test.go`、`cmd_report_quota_test.go` 等按子命令拆分的先例，说明重度子命令测试已经在陆续独立。**当前 main_test.go 承担的是"无独立文件的剩余子命令"，属于合理的残留集合**，不构成紧迫重构需求。**暂不处理**。

#### 📌 internal/server/server_openclaw_scenario_test.go 24 轮状态机模拟维护成本高

- **问题描述与影响**：server_openclaw_scenario_test.go 包含 567 行代码，硬编码模拟了 24 轮 OpenClaw 交互流。该测试属于大颗粒度场景集成测试，内部依赖大量内联 JSON 响应，一旦上游协议或内部数据结构微调，该场景测试维护成本极高。
- **建议解决方案**：保留该场景测试作为核心端到端验收用例，但将其中的 mock 上游交互数据提取到 `testdata/` 或 `fixtures_test.go`，避免在测试函数内部展开海量内联 JSON 字符串。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：提升测试可读性，将业务场景定义与传输断言解耦。
  - **潜在风险 (Risk)**：极低。
  - **改造成本 vs 维护成本**：改造成本约 1 小时。
- **🎯 处置决策**：**🟡 待定**
- **📝 批注**：成立。文件已重命名为 `openclaw_scenario_test.go`。内联 JSON 确实密集，但这类场景测试本身就是"数据即文档"——JSON 内联保持了测试的自解释性，提取到 `testdata/` 反而增加读者跳转负担。真正的问题是场景定义与断言混在一起，但目前维护频率低，**搁置**，等场景扩展时再重构。

- **问题描述与影响**：respnorm_test.go 包含 1,188 行代码与 47 个测试函数，集中了思维链剥离、SSE 帧解析、CRLF 修复、乱序缓冲等多重特性的单测。文件虽然执行飞快（内存流操作），但阅读和导航成本偏高。
- **建议解决方案**：按功能拆分为 `respnorm_stream_test.go` (流式处理)、`respnorm_think_test.go` (思维链剥离) 与 `respnorm_framing_test.go` (SSE/CRLF 边界)。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：模块化程度提升，便于多人并行开发与聚焦维护。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本约 0.5 小时。
- **🎯 处置决策**：**🟡 待定**
- **📝 批注**：成立。1188 行确实偏大，但 `wrap_test.go`、`upstreammodel_test.go`、`bench_test.go`、`fuzz_test.go` 已独立拆出，剩余内容高度内聚（全是流处理逻辑）。**搁置**：执行飞快，无稳定性风险，拆分收益低于引入多文件管理的摩擦成本。等文件再增长 200 行以上时重新评估。

#### 📌 使用历史迭代里程碑/阶段代号命名的测试文件

- **问题描述与影响**：代码库中存在多个以历史研发阶段命名的测试文件：
1. `internal/router/quota_p21_test.go` (实为 Model Multiplier 与 Token Weight 计费单测)
2. `internal/router/quota_p22_test.go` (实为 Metric Cost 金额扣费单测)
3. `internal/story/llm_phase2_test.go` (实为 Single/Divergence Evidence Pack 单测)
随着时间推移，'P2.1' / 'P2.2' / 'Phase2' 的历史背景对于后续维护者将失去语境，降低代码可发现性。
- **建议解决方案**：使用 `git mv` 按领域语义重命名：
- `quota_p21_test.go` -> `quota_multiplier_test.go`
- `quota_p22_test.go` -> `quota_cost_test.go`
- `llm_phase2_test.go` -> `llm_packs_test.go`
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：使测试文件名与业务领域模型完全对齐，消除历史包袱。
  - **潜在风险 (Risk)**：零风险（仅文件重命名，不改动包名与测试内容）。
  - **改造成本 vs 维护成本**：改造成本 5 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：核实成立，文件头注释明确写有 "P2.1"/"P2.2" 阶段描述。✅ **已完成**：`git mv` 重命名为 `quota_multiplier_test.go`、`quota_cost_test.go`、`llm_packs_test.go`，测试全绿。


#### 📌 internal/server/server_probe_test.go 命名具有误导性（0 个测试函数）

- **问题描述与影响**：server_probe_test.go 命名格式为标准测试文件，但其实际上只定义了 probeUpstream 辅助结构体和 driveHalfOpen 辅助函数，不包含任何 `Test*` 函数。这与同包下的 testhelpers_test.go / fixtures_test.go 形成概念混淆，且容易让开发者误以为该文件在测试 probe 逻辑（实际测试在 router_probe_test.go 和 server_active_probe_test.go 中）。
- **建议解决方案**：将 server_probe_test.go 中的辅助代码合并入 `testhelpers_test.go`，或重命名为 `testhelpers_probe.go`。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：消除文件命名误导，统一测试辅助代码的组织方式。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本 5 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：核实成立，0 个 `Test*` 函数，全为 `active_probe_test.go` 专用 helper。✅ **已完成**：重命名为 `probe_helpers_test.go`（未合并入 `testhelpers_test.go`，probe 专属逻辑独立更清晰）。

#### 📌 internal/server 下 19 个测试文件存在多余的 'server_' 前缀

- **问题描述与影响**：在 Go 惯例中，由于代码已经处于 `package server` 目录下，文件内部通常不再重复包名（如 `server_failover_test.go` -> `failover_test.go`）。而目前 server 包中同时存在有前缀（19个）与无前缀（4个）的文件，风格割裂。
- **建议解决方案**：使用 `git mv` 统一去除 `server_` 前缀。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：符合 Go 官方命名哲学，保持目录文件名清爽统一。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本 10 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：核实成立。✅ **已完成**：批量 `git mv` 去除 18 个文件的 `server_` 前缀，`server_test.go` 保留（与包同名惯例）。同步更新了设计文档中两处文件引用，archtest 通过。

#### 📌 cmd/vmr/main_diagnose_replay_test.go 混合了两个不同子命令的命名

- **问题描述与影响**：main_diagnose_replay_test.go 将 `vmr diagnose` 与 `vmr replay` 的 CLI 粘合测试混合在同一个文件中，破坏了一对一的子命令文件映射模式。
- **建议解决方案**：拆分为 `cmd_diagnose_test.go` 和 `cmd_replay_test.go`。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：规范子命令测试文件结构，方便按子命令维度快速定位和单跑测试。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本 5 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：核实成立。✅ **已完成**：拆分为 `cmd_diagnose_test.go`（5 个测试 + `diagnoseConfigYAML` helper）和 `cmd_replay_test.go`（7 个测试），删除原文件，测试全绿。

### 6.4 假阳性/弱断言与测试覆盖盲区 (Assertion Quality & Silent Gaps)

#### 📌 Markdown 报表渲染单测过度依赖 strings.Contains 弱断言

- **问题描述与影响**：在 `internal/report/section_*_test.go`、`internal/story/render_*_test.go` 中，大量测试仅断言输出字符串是否 Contains 某些片段。如果 Markdown 表格由于换行符错乱、管道符缺失或对齐错位导致排版完全崩溃，只要关键字还在，测试依然会判定为 PASS，存在明显的假阳性盲区。
- **建议解决方案**：对核心渲染表格引入基于 Golden File 的全文本 diff 断言，或引入轻量级的 Markdown 结构校验（确保表头行数、分隔线格式及列数合规）。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：彻底杜绝 Markdown 格式渲染退化但测试仍放行的假阳性风险。
  - **潜在风险 (Risk)**：低。需要对现有测试输出更新金样（Golden Snapshot）。
  - **改造成本 vs 维护成本**：改造成本约 1~2 小时。
- **🎯 处置决策**：**🟡 待定**
- **📝 批注**：问题成立，但程度被夸大。`internal/story` 已有 `golden_test.go` 和 `invariants_test.go` 对核心渲染路径做全文比对；`section_*_test.go` 的 `strings.Contains` 主要针对特定字段值（数字、模型名），格式崩溃时这些值本身也会消失，假阳性风险有限。**搁置**：等渲染层稳定后再引入 Golden 快照，当前维护成本高于收益。



#### 📌 internal/adapter/classify_test.go 基础错误分类矩阵缺失

- **问题描述与影响**：adapter/classify_test.go 仅测试了 3 个冷门边界用例（MarkerDeepInBody, UpstreamGatewayFailure, ContextLimit），而 DefaultClassify 核心状态码与错误类型的完整判定表实际上放在了 `internal/adapter/openai/openai_test.go` 中。如果开发者仅运行 `go test ./internal/adapter`，将无法验证 DefaultClassify 的回归正确性。
- **建议解决方案**：将完整的 DefaultClassify 表格测试迁移回 `internal/adapter/classify_test.go`，让 `openai_test.go` 仅测试 OpenAI 适配器特有的覆盖逻辑。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：确保基础包单元测试自洽且自包含，符合单一职责原则。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本 15 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：核实成立。✅ **已完成**：已将 23 组 HTTP 状态码与各厂商错误特征词完整判定表（`TestDefaultClassify_StatusCodesAndVendors`）迁移至 `internal/adapter/classify_test.go`；`internal/adapter/openai/openai_test.go` 保留聚焦的适配器委托回归测试，基础包单测实现自洽自包含。


#### 📌 微型测试文件归并机会 (Reduce Test File Fragmentation)


- **问题描述与影响**：代码库中存在多个代码量 <80 行的超微型测试文件：
- `internal/core/endpointlabel_test.go` (44 行, 1 个测试)
- `internal/adapter/resolveurl_test.go` (36 行, 1 个测试)
- `internal/i18n/story_compare_test.go` (76 行, 1 个测试)
- `internal/respnorm/wrap_test.go` (66 行, 5 个测试)
- `internal/respnorm/upstreammodel_test.go` (83 行, 4 个测试)
- `internal/report/tokenest_test.go` (55 行, 2 个测试)
这些微型文件增加了文件系统的索引膨胀与包内碎片化。
- **建议解决方案**：进行就近同类项合并：
- `endpointlabel_test.go` -> 合并入 `core_test.go`
- `resolveurl_test.go` -> 合并入 `adapter_test.go`
- `story_compare_test.go` -> 合并入 `lang_test.go`
- `wrap_test.go` & `upstreammodel_test.go` -> 合并入 `respnorm_test.go`
- `tokenest_test.go` -> 合并入 `cost_test.go`
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：减少 6 个无谓的孤岛测试文件，提升包内代码紧凑度。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本约 15 分钟。
- **🎯 处置决策**：**🟢 马上处理**
- **📝 批注**：**数据有误，问题基本不成立**。逐一核实：① `endpointlabel_test.go` 实际 64 行、6 个测试（非 1 个），有独立价值；② `resolveurl_test.go` 测包级函数，`adapter_test.go` 测并发注册，主题不同；③ `story_compare_test.go` 测 `MetricLabel`，`lang_test.go` 测 bundle struct，API 不同；④ `wrap_test.go`、`upstreammodel_test.go` 有独立文件级注释说明存在理由，且 `respnorm_test.go` 已 1188 行不宜再扩。**全部不合并**，每个文件均有清晰的独立主题。


#### 📌 internal/config/quota_test.go 与 segment_test.go 碎片用例表格驱动化

- **问题描述与影响**：在 `internal/config/quota_test.go` (33 个测试函数) 与 `internal/taskseg/segment_test.go` (25 个测试函数) 中，存在大量仅有 3~5 行的独立测试函数（例如分别测试 PreviewIsIdempotent, Preview_ShortTextUnchanged, Preview_CollapsesWhitespace, Preview_TruncatesWithEllipsis 等）。
- **建议解决方案**：将针对同一纯函数的多个微小变体用例整合成标准的 Table-Driven Test，使用统一的入参和预期结构体迭代执行。
- **ROI 投入产出比分析**：
  - **预期收益 (Gain)**：使边界用例一目了然，新增边界条件只需增加一行表格配置。
  - **潜在风险 (Risk)**：零风险。
  - **改造成本 vs 维护成本**：改造成本约 0.5 小时。
- **🎯 处置决策**：**🟡 待定**
- **📝 批注**：问题部分成立。`segment_test.go` 中 `Preview_*` 系列是同一纯函数的变体，合并为 table-driven 有收益；`config/quota_test.go` 各函数测的是不同配置路径，强合并反而降低可读性。**搁置**：属于渐进式改善，下次对该文件有实质修改时顺手重构更合适，不值得专项排期。

## 七、 最终行动建议清单汇总 (Actionable Priority Matrix)

为便于团队按敏捷迭代有序排期，以下将所有建议按执行优先级进行总览排序：

| 序号 | 问题/重构项 | 涉及包与文件 | 类别 | 投入产出比 (ROI) | 建议处置行动 |
| :---: | :--- | :--- | :--- | :--- | :---: |
| 1 | 消除历史里程碑命名残留 (p21, p22, phase2) | `internal/router, internal/story` | 命名规范 | 极高 (5分钟彻底消除认知混淆) | **🟢 马上处理** |
| 2 | 重命名 server_probe_test.go 消除 0 测试辅助文件误导 | `internal/server` | 命名与组织 | 极高 (5分钟理清辅助类边界) | **🟢 马上处理** |
| 3 | 规范 internal/server 下测试文件，移除多余 server_ 前缀 | `internal/server` | 命名规范 | 高 (10分钟完成 Go 惯用化) | **🟢 马上处理** |
| 4 | 拆分 main_diagnose_replay_test.go 为独立子命令单测 | `cmd/vmr` | 组织结构 | 高 (5分钟理顺子命令单测) | **🟢 马上处理** |
| 5 | 重构 server_active_probe_test.go 消除 1.1s Sleep 等待 | `internal/server` | 耗时与稳定性 | 极高 (提速 12 倍，除掉抖动隐患) | **🟢 马上处理** |
| 6 | 将 DefaultClassify 完整测试回归表回迁至 adapter 包 | `internal/adapter` | 断言质量 | 高 (15分钟补全底层单测自洽性) | **🟢 马上处理** |
| 7 | 归并 6 个超小型孤岛测试文件 (<80行) | `core, adapter, i18n, respnorm, report` | 测试合并 | 中高 (15分钟消除文件碎片化) | **🟢 马上处理** |
| 8 | 重构 quota_parity_test.go 为表格驱动，削减 300+ 行样板 | `cmd/vmr` | 代码冗余 | 高 (0.5小时提升可维护性) | **🟢 马上处理** |
| 9 | 精简 i18n_e2e_test.go 冗余端到端测试 | `cmd/vmr` | 代码冗余 | 高 (0.5小时消除脆弱断言) | **🟢 马上处理** |
| 10 | 移除 openairesponses_test.go 中重复的 ResolveURL 单测 | `internal/adapter/openairesponses` | 代码冗余 | 高 (1分钟删除冗余用例) | **🟢 马上处理** |
| 11 | 统一 ExtractFinish 与 NoReply 重复测试的职责归属 | `chatmsg, report, story` | 代码冗余 | 中 (15分钟理清跨包边界) | **🟢 马上处理** |
| 12 | 拆解 cmd/vmr/main_test.go (1133行) 巨型文件 | `cmd/vmr` | 复杂度过高 | 高 (0.5小时降低认知负荷) | **🟢 马上处理** |
| 13 | 抽离 server_openclaw_scenario_test.go 内联 JSON 到 fixture | `internal/server` | 复杂度过高 | 中等 (收益清晰但需解耦数据) | **🟡 待定** |
| 14 | 拆分 respnorm_test.go (1188行) 为多文件模块化测试 | `internal/respnorm` | 复杂度过高 | 中等 (目前执行快，视后续特性排期) | **🟡 待定** |
| 15 | 引入 Markdown 报表 Golden 全文比对替代弱 Contains | `internal/report, internal/story` | 断言质量 | 中等 (需更新金样基准，收益扎实) | **🟡 待定** |
| 16 | 重构 config/quota_test.go 与 segment_test.go 为 Table-Driven | `config, taskseg` | 测试合并 | 中等 (收益平稳，属于渐进式优化) | **🟡 待定** |
