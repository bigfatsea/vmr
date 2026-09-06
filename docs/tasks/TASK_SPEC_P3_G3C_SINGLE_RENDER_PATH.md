# 任务说明书：Group 3C - 单轨渲染接线与 -render-only（Phase 3 收尾，串行）

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §5.0（D11 单一渲染路径）、§5.4（-render-only 覆盖面）、§5.5（D10 语言）、§8.3（删旧路径）、§9（回归守卫）。
> **前置：Group 3A、3B 均已合并**（report/journey 两侧 VM 与固定序列化器已就绪，新旧路径字节等价已由过渡测试钉住）。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与删除：
     - 删除：`internal/report/render_doc.go` 及旧渲染路径文件（`section_*.go` 中被 VM 构建器取代的渲染函数体，构建器保留）、`internal/journey` 侧旧渲染入口（`render_md.go`/`render_spine*.go` 中吃内存 `*Journey` 的旧路径，VM 构建器保留）、两侧对应的旧 golden 过渡测试
     - 修改：`cmd/vmr/cmd_analyze.go` 及相关 `cmd/vmr/*.go`、`cmd/vmr/*_test.go`、`internal/report/*_test.go`、`internal/journey/*_test.go`
     - 新建：`-render-only` 的 cmd 接线与测试
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 VM 构建器与序列化器本身**（3A/3B 交付物；发现缺陷记录 `NOTES_FOR_LEAD.md` 交回主控）；**不得修改 `internal/archtest/**`**（删文件导致行数预算表条目过期时，仅允许移除对应已删除文件的条目并记 NOTES）。
3. 架构纪律：聚合类产物只有一条渲染路径——全量运行 = 先写 JSON 切片 → 从落盘 JSON 构建 VM → 序列化 → 写 Markdown（D11）；详单/证据是显式豁免的另一类产物，永不进该路径（§5.0 末段）。
4. Git 规范：短命令式提交信息，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档主控独占；待登记项写 `NOTES_FOR_LEAD.md`。
6. 严禁 `git add .`，精准暂存。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: 全量运行切到单轨渲染（D11）
- `cmd/vmr` 各模式（默认套件、`-macro-only`、`-journey`、`-benchmark`）的 Markdown 产出改为：写完 JSON 切片后**从落盘 JSON 读取** → VM 构建 → 序列化 → 写 Markdown；删除一切"内存聚合直接喂渲染器"的旁路。
- `-journey`/`-benchmark` 的 `.md` 重写输入相应改为 `journeys/details/j-<id>.json`、`journeys/benchmarks.json`。
- 详单（`requests/details/`）与证据保持既有懒物化路径不动。

### 任务 2: `-render-only`（§5.4 表格）
- `vmr analyze -render-only [-o dir]`：校验 `manifest.json` 存在且记录的切片指纹与磁盘一致（完整校验档，Go 侧读取方纪律），通过后跳过全部日志解压/解析/聚合，只做 JSON 读取 → VM → 序列化 → 写盘。
- 覆盖面（必须诚实）：`vmr-report.md`、`journeys/index.md`、`benchmarks.md`、`journeys/details/j-<id>.md`（作业清单来自 `journeys/index.json`，不扫目录，D20）、`compares/index.md` 与 `compare-*.md`（扫 `compares/*.json` 派生）、`requests/failed.md`。**永不**物化 `requests/details/*.md` 与 `requests/evidence/`。
- 每次 `-render-only` 调用同样幂等刷新骨架页。
- 语言：渲染产物语言 = JSON 落盘语言（D10），`-lang` 与 JSON 不一致时报错并指引全量重跑。

### 任务 3: 删除旧渲染路径与移交清单（详见 docs/tasks/HANDOVER_P3_NOTES.md，必读）
- 删两侧旧渲染入口与吃内存结构的旧函数（保留 VM 构建器）；删新旧字节等价过渡测试；archtest 预算表移除已删文件条目。
- **3B 移交**：① `JourneyStructure.Bodies` 改 `json:"-"`（当前 j-<id>.json 把 blob 表序列化两份，`structure.bodies` 与顶级 `bodies` 重复，约 2 倍 blob 字节；设计 §3.6 只留顶级一份），`TestBuildStructure_VolumeBoundedByStepsNotProseLength` 尺寸守卫改为 marshal `JourneySummary`；② 删除死代码 `internal/journey/mdlite.go` + `_test.go`（2B 删 render_compare_html 后零生产调用方）。
- **3A 移交**：删旧路径后把 `vm` 前缀的过渡副本helper 改回原名并删 legacy 原件（完整清单见 HANDOVER_P3_NOTES §3）；`viewmodel_provider.go` 的 `vmSkippedAttemptsNote` 改为直接调用 providerquota.go 存活的 `renderSkippedAttemptsNote` 逻辑。
- **明确不在本组做**（后续独立变更）：`section_cost.go` 两条从未进 i18n 的英文裸文案迁入 i18n（会改 vmr-report.md 字节，独立措辞变更）；CLAUDE.md/设计文档的 i18n 配对表述更新由主控负责。

### 任务 4: 守卫与测试
- 新增 §9 守卫：`-render-only` 产物与全量运行产物**字节一致**（同路径的结构性保证，测试防将来分叉）。
- 更新受影响的 cmd 测试（旧入口删除、模式语义变化）。

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -race ./internal/report/... ./internal/journey/... ./cmd/vmr/...`
3. 架构门禁：`go test ./internal/archtest/...`
4. 手工冒烟：造最小审计日志跑全量 → 记录产物哈希；改一 FLAG 重跑 `-render-only` → 产物哈希一致（结论记 `NOTES_FOR_LEAD.md`）
5. `git status -s` 确认无越界
6. Commit：`git commit -m "feat(analyze): single render path from disk json, add -render-only, drop legacy renderers"`
