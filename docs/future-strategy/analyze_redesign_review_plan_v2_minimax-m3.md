# analyze 架构重构方案 — 落地情况独立 Review（v2）

<!--
    独立 review。不预设前轮 review（analyze_redesign_review_plan_gemini-3.8-flash.md）的任何结论。
    每一条都从源码与测试断言里找事实依据；前轮 review 仅作交叉参考、不作输入。

    基线:
      - 起点: 050ad25（rename internal/story → journey，方案实施起点）
      - 终点: review 开始时的 HEAD（f8e23f0）
      - 共 59 个提交

    方法:
      - 逐条 D1–D21 / 逐章 §1–§11 从源码与测试断言取证，不轻信任何文档 claim
      - 端到端冒烟: ./vmr analyze -o /tmp/out examples/sample-audit.jsonl，
        产物拓扑与权限逐一比对方案 §4
      - 行为性验证（非只读代码）: L2 命中、orphan 清扫、-render-only 语言继承
        均实际运行验证，其中 L2-hit orphan 残留一项做了红绿验证

    执行纪律:
      - 事实清楚、方案明确无争议 → 当场修，留 commit 与红绿验证记录
      - 复杂/争议 → 记入"待你决策"，不动手
-->

---

## 一、方案 review 事项结论

> 标记：✅ 已全部完成 ｜ 🟡 部分完成 ｜ ❌ 未完成

### §0 裁决 D1–D21

| # | 裁决 | 状态 | 事实依据 |
|---|---|---|---|
| D1 | 切片为消费弹性拆、缓存整套一个指纹 | ✅ | `internal/report/slices.go` 五切片；`cache.go` L2 单指纹（H 节核对） |
| D2 | `vmr-report.json` 一步删除 | ✅ | 仓内零写出代码；`Report2` 保留为内存聚合形状，`LoadReport`（viewmodel_doc.go）从切片重装它，`-render-only` 与全量运行共用同一 VM 构建（D11 成立）。`rows.go` 的 Report2 注释原称"top-level JSON output"——过期，已修（见 F2） |
| D3 | Markdown 不引模板引擎 | ✅ | `rg 'text/template'` 零命中；`RenderMarkdown` 是 VM 的固定序列化器 |
| D4 | 全部文案进 ViewModel | ✅ | viewmodel_*.go 全部经 i18n 查表；`archtest/vm_literals_test.go` AST 守卫在位 |
| D5 | `-render-only` 覆盖全部常驻人读产物 | ✅ | `renderAllFromDisk` 实测覆盖 vmr-report.md / journeys/index.md / details j-*.md / benchmarks.md / compares/* / failed.md；details/evidence 豁免；骨架页幂等刷新 |
| D6 | 自包含 HTML 废弃 | ✅ | `render_html*`/`render_compare_html`/`toolwaste_html` 零命中；`dashboard/assets/` 六页骨架 |
| D7 | 人读请求索引整族删除 | ✅ | `vmr-requests-<tag>.md` / `-cron-*.md` 零写出；failed.md/failed.jsonl 保留 |
| D8 | 全系统唯一 Digest | ✅ | `internal/digest/` stdlib-only 叶子包（长度前缀有序 sha256 链 + 定宽标量编码）；report/journey 双侧调用方；包注释钉住三性质；md5 底座未动 |
| D9 | `/reports/` 默认关 + 无 key 硬拒 | ✅ | `server/reports.go`: opt-in 才挂路由；`len(APIKeys)==0` 全树 403 且不复用放行型 auth；Clean+前缀+逐级 Lstat 拒 symlink+禁目录列表；分层鉴权（.html 免 / 数据 Bearer） |
| D10 | `-render-only` 继承 manifest 语言 | ✅ | 实测：lang 不匹配 exit 1 且报错指引全量重跑；匹配 exit 0 |
| D11 | 单一渲染路径 | ✅ | `renderAllFromDisk` 由 `-render-only` 与全量运行共用（cmd_analyze.go dispatchDefaultSuite 也走它）；macro 报告渲染源 = `LoadReport`（切片），不是单体 |
| D12 | ViewModel 不落盘 | ✅ | 无 vm_*.json 写出；前端消费 raw 切片 + 自带格式化（common.js） |
| D13 | `#data=` hash 传参 | ✅ | 六页 `location.hash` 解析；磁盘布局 = HTTP 路径 |
| D14 | manifest format banner | ✅ | `EXPECTED_MANIFEST_FORMAT=11` 与 Go `ManifestFormat=11` 一致；`versionBehavior` 纯函数 + js_test 覆盖；不一致警告不阻断 |
| D15 | `-redact` 删除 | ✅ | CLI/渲染器零残留 |
| D16 | 托管目录默认 `./reports`、不走 rundir | ✅ | `reportsState.resolve()` 按进程 cwd；目录缺失 404 + 每目录一次日志（missingDirWarner） |
| D17 | 详单 `r-` 前缀 | ✅ | `reqdetail.FileName` 输出 `r-<ts>_<virt>_<real>_<outcome>_<h8>.md`，三个包装函数透传 |
| D18 | Journey JSON 自包含（tree + bodies blob + 三级 match） | ✅ | `JourneySummary.Bodies` 顶级 blob 表；structure 的 bodies `json:"-"`；`match: exact|normalized|positional`；compaction 前驱摘录；截断口径数据层统一、RespText 不截；`structure_test.go` 双向断言（无孤儿/无悬引用）+ LosslessReconstruction 只吃 JSON |
| D19 | `-partial` 不进文件名 | ✅ | journey 侧与 compare 侧均完整落地。compare 文件名不再带 `-partial` 后缀（8663dee），partiality 经 `Comparison.Partial` 字段 + `.md` 顶部 banner + `compares/index.md` 行标记 + 看板徽标全链路表达 |
| D20 | 作业清单来自索引 + orphan 清扫限 journeys/details/ | ✅ | 清扫实现与范围正确（clean_orphans.go 路径严格隔离；测试断言 compares/、requests/* 不受影响）；L2 命中路径的清扫缺口已由 F1 修复 |
| D21 | compares/index 扫目录派生、子树不进 manifest | ✅ | `RebuildComparesIndex` 每次 analyze 重建（含 L2 命中路径与 -render-only 路径，实测）；`AllSlicePaths` 不含 compares/* |

### 分章结论

| 章节 | 状态 | 说明 |
|---|---|---|
| §1 现状基线 | ✅ | 方案所列旧产物全部确认废弃（vmr-report.json / stories/ / vmr-requests-* / 自包含 HTML / .parse-cache）。`reports/` 下残留旧产物是历史测试样例，非 analyze 现行为 |
| §2 概念模型归一 | ✅ | 包名/CLI/落盘名/代码符号/文件名已彻底清理（见 T2 落地）。story/corpus 词族已从全部生产代码中清除 |
| §3 数据层 | ✅ | 五切片/现算事实下沉/提交顺序/compares 索引全部落地；D19 compare 侧落地；L2-hit 清扫闭环 |
| §4 目录拓扑 | ✅ | 冒烟产物拓扑与 §4 逐字一致（含 .cache/parse），权限全 0600 |
| §5 ViewModel 渲染 | ✅ | §5.0–5.6 全部落地；VM 构建器吃统一的 Report2 内存形状（切片经 LoadReport 重装），-render-only 与全量运行同路径 |
| §6 HTML 看板 | ✅ | 六页 + 安全模型 + 版本探测 + file:// 降级 + 币种显示全部落地 |
| §7 缓存层 | ✅ | 三级模型、失效矩阵、-no-cache 旁路、llm 指纹入参全落地；L2 命中路径的行为缺口见 F1 |
| §8 兼容边界 | ✅ | 无兼容期一步到位；manifest 三职责齐备且无指标数值；format 单一版本单位（Format=ManifestFormat 别名） |
| §9 测试守卫 | ✅ | 既有守卫全部迁移/保留；新增守卫逐条具名在位（含 vm_literals） |
| §10 路线图 | ✅ | 四阶段全部实施；旧渲染路径/旧 CLI/旧产物零残留 |
| §11 不变量 | ✅ | 五条不变量全守住（archtest 强制 + 测试断言） |

---

## 二、待决策事项

> **全部待决策事项已闭环**。经你裁决：
> - **T1 选 A**（完整落实 D19，已落地于 commit `8663dee`）
> - **T2 选彻底清理**（一次性机械重命名，已落地）
> 当前无未决议题。

---

## 三、过程中新发现的问题

### 已在 review 中直接解决

#### F1. L2 缓存命中的全量运行跳过 orphan 清扫（D20/§3.4 行为缺口）— **已修**

- **发现过程**：行为验证（非代码阅读）——全量运行后手动往 `journeys/details/` 放 orphan 文件，重跑同输入；第二次运行 L2 命中直接 `return nil`，orphan 残留。对照 `-no-cache` 路径清扫正常。
- **根因**：清扫函数调用点只在 `dispatchDefaultSuite`（冷启动路径）；`dispatchAnalyze` 顶端的 `tryL2Cache` 命中分支提前返回，绕过了它。L2 命中在语义上就是"同输入同参数的一次全量运行"，清扫职责应随行。
- **修复**：`tryL2Cache` 命中分支内，`mode == "default"` 时从 `journeys/index.json`（L2 命中保证它属于本快照）读 active id 集并执行 `CleanOrphanJourneys`。zoom 模式不清扫（其 L2 digest 与 default 不同，分支天然不触发；且 zoom 运行本就不该动兄弟 journey 的详情文件）。
- **验证**：新增 `TestAnalyzeCache_L2HitSweepsOrphanJourneys`，红绿验证通过（移除修复 → FAIL；恢复 → PASS）。
- **连带重构**：修复使 `cmd_analyze.go` 超出 archtest 700 行预算；按预算注释自己的纪律（拆分而非提数），把缓存函数族（analyzeModeString/computeTargetL2/tryL2Cache/recordPostAnalyzeCache/tryRenderOnlyL3Cache/recordRenderOnlyL3Cache）拆到新文件 `cmd_analyze_cache.go`——与其测试文件 `cmd_analyze_cache_test.go` 成对。

#### F2. 三处过期注释与代码现状相悖 — **已修**

- `internal/report/rows.go`：`Report2` 注释自称"the top-level JSON output"——D2 落地后它是**内存聚合形状**（切片唯一落盘；`LoadReport` 从切片重装它供 `-render-only` 使用）。重写注释，写明 D2/D11 下的定位与双消费者。
- `internal/report/aggregate.go`：文件头注释称"Rendering lives in render_doc.go + one section_*.go per numbered section"——`render_doc.go`/`section_*.go` 早已删除，现为 viewmodel_*.go + 固定序列化器。重写注释并指向 redesign 方案文档。
- `internal/i18n/story_render.go`：配对注释指向 `render_md.go`"both render paths"——该文件只剩 helpers，唯一渲染路径是 viewmodel。重写注释指向 viewmodel_build/viewmodel_spine。
- 三处均为注释级修正，零行为变化；`go build`/相关包测试全绿。

---

## 四、执行记录

### 验证手段

1. `go build ./...` / `go vet ./...` / `gofmt -l` 全绿。
2. `go test ./...` 全绿（含 -race 未跑，本次未触碰并发代码路径；archtest 在内 37 包 ok）。
3. 端到端冒烟 `./vmr analyze -o /tmp/out examples/sample-audit.jsonl`：产物拓扑与方案 §4 逐字一致，26 个文件全部 0600；`-render-only` 同目录可运行；语言不匹配拒绝（exit 1）。
4. 行为验证：L2 命中、orphan 清扫、compares 索引重建在冷/热/-no-cache 三路径下实测。
5. 红绿验证：F1 的修复移除后新测试确实 FAIL。

### 提交记录

1. `49e316d` — fix(analyze): sweep orphan journeys on L2-hit full runs; refresh stale ownership comments (包含 F1, F2 及缓存代码拆分)
2. `8663dee` — feat(compare): carry partiality as data, retire the -partial filename suffix (D19) (T1 落实)
3. 待提交 — refactor(analytics): eradicate legacy story/corpus symbols and filenames (T2 落实)

### 与前轮 review 的关系

前轮（analyze_redesign_review_plan_gemini-3.8-flash.md）的结论经本轮独立取证**大体得到证实**：D2 落地为"单体删除 + Report2 内存形状保留"（前轮 T1 方案 A 的延伸）、digest 叶子包（前轮 T2）、vm_literals 守卫（前轮 T5）、.cache/parse 归位与 L2 指纹补齐（前轮 F2/F3）均属实。本轮不重复其已裁决事项，仅就**新发现**与**其未覆盖的行为路径**（L2 命中分支）补账。前轮 T3（v4 Analytics 设计文档改写）已在 f8e23f0 完成，本轮确认其状态头与内容为现行状态。
