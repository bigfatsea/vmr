// Ver 2026-09-06 20:30, by gemini-3.8-flash

# 任务说明书：Group 1B - 请求明细与证据拓扑归位

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：
     - `internal/reqdetail/*`
     - `internal/report/requests.go`
     - `internal/report/requests_failed.go`
     - `internal/report/detail.go`
     - `internal/report/*_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `internal/journey/`、`cmd/`！
3. 代码风格与架构门禁：
   - 保持 leaf package 边界，`reqdetail` 严禁依赖任何路由半区与消费半区包。
   - 保持既有函数签名兼容：`WriteRequestsIndex` 等公共导出函数签名保持兼容（由 Group 1D 后续统一收敛），避免导致白名单外的 `cmd/vmr` 出现编译报错。
   - 严禁违背 `archtest` 单向依赖与行数预算。
4. Git 规范：提交信息遵循短命令式，如 `feat(requests): r- prefix for details, sink evidence to requests/evidence, delete markdown request indexes`，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占，Worker 严禁修改；待登记项写入不提交的 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：严禁 `git add .`，仅 `git add <file>` 精准暂存白名单文件。

---

## 二、具体研发任务清单 (Action Plan)

### 任务 1: 详单文件名加 `r-` 前缀（D17 / §1.1）
- 背景：请求坐标与详单文件名是派生关系，给详单加 `r-` 前缀（与 journey 的 `j-` 同一套约定，使文件名自述类型）。
- 目标修改：
  1. 在 `internal/reqdetail/detail.go` 中的 `FileName(ts, virtualModel, realModel, outcome, req)`，增加 `r-` 前缀：
     `r-{ts}_{virtualModel}_{realModel}_{outcome}_{h8}.md`
  2. 两个包装函数 `FileNameForRecord` 与 `FileNameForManifest` 透传调用自动继承。
  3. 同步更新 `internal/reqdetail/` 的单元测试断言。

### 任务 2: 证据与详单目录拓扑下沉（§3.5 / §4）
- 背景：目标拓扑中，证据与详单从根目录对齐下沉至 `requests/` 空间。
- 目标修改：
  1. `internal/report/detail.go` 中，详单输出至 `requests/details/`，证据输出至 `requests/evidence/sysprompt-<h8>.md`。
  2. 异常与失败请求输出由 `internal/report/requests_failed.go` 生成：`requests/failed.jsonl` 与 `requests/failed.md`（D7 保留排障入口，废弃根目录 `vmr-requests-failed.*`）。

### 任务 3: 人读请求索引整族删除与 `requests/index.json` 补齐（D7 / §3.7 / §3.3）
- 背景：人读请求索引（`vmr-requests.md`、`vmr-requests-<tag>.md`、`vmr-requests-cron-*.md`）没有独占职责，整族废弃；浏览交互交给后续的 `request-browser.html`。
- 目标修改：
  1. 彻底删除 `WriteRequestsIndex` 中生成人读 Markdown 索引（含按 tag、cron 拆分）的代码，仅保留机读输出。
  2. 保持 `WriteRequestsIndex` 导出函数签名兼容，内部调用新机读生成逻辑。
  3. 输出机读真源：`requests/index.json`。结构体 `RequestsIndex` 补齐投影数据（§3.3）：
     - `Requests`: `[]RequestRow`
     - `Sessions`: `map[string]SessionMeta`（会话标题、别名、任务标题映射投影）
     - `JourneyLink`: `map[string]string`（会话/lineage ID -> 所属 journey ID）
  4. 同步更新 `internal/report/` 中的相关测试（如 `aggregate_test.go` 中旧 `vmr-requests.md` 的断言改为验证 `requests/index.json` 与 `requests/failed.*`）。

---

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/reqdetail/...` 与 `go test -v -race ./internal/report/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 检查变更范围：`git status -s`（确认无越界文件）
4. 执行 Commit：`git add ... && git commit -m "feat(requests): r- prefix for details, sink evidence to requests/evidence, delete markdown request indexes"`

