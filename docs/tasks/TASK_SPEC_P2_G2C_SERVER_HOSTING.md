# 任务说明书：Group 2C - /reports/ 安全托管与配置项

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §6.5（托管与安全模型，裁决 D9/D16）。
> 本组与 Group 2A（纯新增 internal/dashboard）、Phase 1 余量（cmd/vmr）完全正交，可并行。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：
     - `internal/server/reports.go` (新建)、`internal/server/server.go`（仅挂载点）、`internal/server/reports_test.go` (新建)
     - `internal/config/config.go`、`internal/config/config_validate.go`、`internal/config/*_test.go`
     - `config.example.yaml`、`config.example.zh.yaml`（双语同步是硬规则：同键同构同例值，仅文案翻译）
     - `internal/archtest/import_boundaries_test.go`（仅新增 server 的禁 import 条目）
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 `internal/archtest/file_sizes_test.go`**；**不得修改 `internal/report`、`internal/journey`、`cmd/`**。
3. 代码风格与架构门禁：遵守 archtest 既有预算；`internal/server` 不得 import 分析半区任何包（本组要在 archtest 里把这条从"没写"变成"写死"）。
4. Git 规范：短命令式提交信息，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占；待登记项写入不提交的 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：严禁 `git add .`，仅 `git add <file>` 精准暂存。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: 配置项（D9-1 / D16）
- `internal/config`：新增顶级 `analytics` 段：
  - `serve: bool`，默认 `false`——不开不挂路由，路由不存在时访问 `/reports/*` 得普通 404。
  - `serve_dir: string`，默认 `./reports`——相对 `vmr start` 进程工作目录解析。
  - 严格 YAML（`KnownFields`）：结构体、校验、默认值补齐；`config.example.yaml` 与 `config.example.zh.yaml` 双语同步；`example_config_test` 如有键清单断言一并更新。
- `serve_dir` 是纯字符串配置项，**不是**分析半区传来的对象；`internal/config` 不因此依赖任何分析包。

### 任务 2: /reports/* 处理器（D9 全部四条）
`internal/server/reports.go`：
1. **无 key 即硬拒绝**：`len(APIKeys) == 0` 时一律 403 并在启动日志说明原因——**不复用**当前"无 key 放行"的通用 `auth` 包装器（那是路由 API 的既有行为，不能让承载对话正文的报表搭车放行）。
2. **分层鉴权**：`.html` 骨架页免鉴权直出（零业务数据，同 status.html）；`.json` / `.jsonl` / `.md` 数据请求走鉴权（`Authorization: Bearer`，复用既有 key 校验逻辑）。前端从 localStorage 取 key（2A 组页面已按同一契约实现，无需本组关心前端）。
3. **路径校验**：`filepath.Clean` + serve_dir 前缀校验 + 拒绝符号链接 + **禁用目录列表**（目录请求一律 404，`requests/details/` 单目录数千文件，列表响应本身就是 DoS）。`..` 穿越与绝对路径逃逸都要有测试。
4. 目录不存在不是启动错误：`vmr start` 正常启动，`/reports/*` 一律 404，日志提示"尚未生成分析产物"（可日志节流或仅启动时提示一次，避免每请求刷屏——写明所选策略）。
- 挂载点在 `server.go`：`serve == false` 时完全不注册路由。

### 任务 3: archtest 边界写死
- `import_boundaries_test.go`：新增 `"vmr/internal/server"` 条目，禁 import 分析半区全部包（`report`、`journey`、`ctxgraph`、`taskseg`、`chatmsg`、`reqdetail`、`reqdetail` 的兄弟包按现有 map 风格列全）；注释说明这是两半区契约的 server 侧（设计文档 Part 2 两半区一条契约）。

### 任务 4: 测试
- `reports_test.go` 用 `httptest` 覆盖：serve 关闭（路由不存在）、无 key 403、有 key 后 .html 免鉴权 / .json 鉴权、路径穿越拒绝、符号链接拒绝、目录列表 404、目录缺失 404、正常取回文件内容。
- `go test -race ./internal/server/... ./internal/config/...` 全绿。

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -v -race ./internal/server/... ./internal/config/...`
3. 架构门禁：`go test -v ./internal/archtest/...`
4. 检查变更范围：`git status -s`（确认无越界文件）
5. Commit：`git add <白名单文件> && git commit -m "feat(server): opt-in /reports hosting with auth-gated data files and analytics config"`
