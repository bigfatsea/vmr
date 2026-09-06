// Ver 2026-09-06 20:14, by gemini-3.8-flash

# 任务说明书：Group 1C - Journey 切片自包含化与命名归一

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：
     - `internal/journey/structure.go`
     - `internal/journey/journey.go`
     - `internal/journey/corpus.go`
     - `internal/journey/candidates.go`
     - `internal/journey/storyindex.go`
     - `internal/journey/render_md.go`
     - `internal/journey/clean_orphans.go` (新建或扩展)
     - `internal/journey/*_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `internal/report/`、`internal/reqdetail/`、`cmd/`！
3. 代码风格与架构门禁：
   - 严禁 import `router`, `server`, `config`，严禁 import `internal/report`（archtest 强制）。
   - 遵循单向流与行数预算限制。
4. Git 规范：提交信息遵循短命令式，如 `feat(journey): self-contained journey json with bodies blob store and clean filenames`，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占，Worker 严禁修改；待登记项写入不提交的 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：严禁 `git add .`，仅 `git add <file>` 精准暂存白名单文件。

---

## 二、具体研发任务清单 (Action Plan)

### 任务 1: Journey JSON 自包含化（D18 / §3.6）
- 背景：当前 `JourneyStructure` 装不下 `.md` 渲染所需的工具结果正文、三级配对置信度与 Compaction 前驱摘录。JSON 必须成为无损单一真源。
- 目标修改：
  1. 在 `internal/journey/structure.go` 中，为 `JourneySummary` 补齐同文件 `bodies` blob 表（`map[string]string`，按内容哈希去重）。
  2. 结构体调整：
     - `ToolCallRef`: 引用 `args_ref string`；结果增加结构 `ToolResultRef { Ref string, Match string, IsError bool }`。其中 `Match` 升级为三级状态：`"exact"` / `"normalized"` / `"positional"`（消除旧有的二值 `Matched bool`）。
     - `CompactionRef`: 包含 `TokensBefore int` 与 `PredecessorExcerptRef string`（指向 bodies 表中的哈希）。
     - RespText / Reasoning 通过 `RespRef string` 存入 bodies 表，**不设字符上限**（保持 `.md` 渲染不缩水）。
     - 工具参数与工具结果统一按 3000 字符上限在数据层截断并入 bodies。
  3. 完善 `bodies` 表生成逻辑：去重存储，保证每个 ref 都能在 bodies 中解析，且无无用孤儿 blob。

### 任务 2: 文件名归一与取消 `-partial` 后缀（D19 / §1.1）
- 背景：消除 `journey-` 历史冗余前缀（原 `journey-j-...`）；取消 `-partial` 编进文件名（partial 是加载范围的函数，不属于内容寻址 ID）。
- 目标修改：
  1. 产物统一命名为 `j-<id>.{json,md}`，与 ID 保持 1:1 全函数对齐。
  2. `IsPartialHead` 状态仅保留在 JSON 字段（`Partial bool`）、`.md` 顶部 banner 和索引行标记，不得再作为文件名后缀。
  3. `corpus` 产物重定位与更名：生成 `journeys/benchmarks.{json,md}`。

### 任务 3: 全量运行清扫 `journeys/details/` 孤儿文件（D20 / §3.4）
- 背景：时间窗收窄时，旧运行会残留过期的 `j-<id>.*` 详情文件。
- 目标修改：
  1. 实现 `CleanOrphanJourneys(detailsDir string, activeIDs []string) (cleaned int, err error)`。
  2. **边界铁律**：清扫范围必须严格局限于 `journeys/details/` 目录！绝对不得碰触 `compares/` 与 `requests/details|evidence/`。

### 任务 4: 单元测试与完整性断言
- 在 `internal/journey/` 测试中补充：
  1. `bodies` 表完整性单测：无悬引用（每个 ref 都能查到正文）、无孤儿 blob。
  2. 工具配对一致性单测：断言三级 `match` 正确标定。
  3. 孤儿清扫单测：确认只清理不在索引内的 `j-<id>` 文件，且绝不误删其他目录文件。

---

## 三、测试与验收步骤
1. 运行单测：`go test -v -race ./internal/journey/...`
2. 架构门禁：`go test -v ./internal/archtest/...`
3. 检查变更：`git status -s`（严格无白名单外改动）
4. 执行 Commit：`git add internal/journey/... && git commit -m "feat(journey): self-contained journey json with bodies blob store and clean filenames"`
