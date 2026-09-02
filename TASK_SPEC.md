# 任务说明书：Group 4 - 离线分析性能提升与资产解耦 (Analytics Scale & Clean Asset)

## 一、协作原则与红线约束（铁律）
1. **工作区限制**：仅在当前指定的 Worktree 目录下操作，严禁跨目录。
2. **文件修改白名单（极度关键）**：
   - ✅ 允许修改：
     - `internal/story/render_html_assets.go`
     - `internal/story/assets/*`（新建静态资产目录与文件）
     - `internal/story/render_html_dashboard.go`
     - `internal/story/journey.go`
     - `internal/story/journey_stepfacts.go`
     - `internal/story/journey_test.go`
     - `internal/story/render_html_test.go`
     - `internal/ctxgraph/manifest.go`
     - `internal/ctxgraph/manifest_test.go`
     - `internal/config/config.go`
     - `internal/config/config_validate.go`（新建拆分文件）
     - `internal/config/config_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！
3. **架构门禁与规范**：
   - 严格遵守 `internal/archtest` 架构约束与行数预算（默认单文件 700 行，单函数 120 行）。
   - 分析半区（`story`/`ctxgraph`）绝不反向依赖 `router`/`server`/`config`。
4. **Git 规范**：
   - Commit message 保持精炼，无任何 trailer。
5. **共享文件禁改**：
   - `CHANGELOG.md` / `KNOWN_ISSUES.md` / 设计文档由主控独占，严禁修改；待记录项写入不提交的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**：
   - `_tmp/`、`archived/` 视为不存在，严禁读取或修改。

---

## 二、具体任务清单 (Action Plan)

### 任务 1: `story` 前端 HTML/CSS 静态资产利用 `//go:embed` 独立抽离 (ISSUE-13)
- **背景与根因**：`internal/story/render_html_assets.go` 中内联了大量长字符串常量（CSS 样式与 JS 脚本），缺乏语法高亮和 Lint 支持。
- **目标修改**：
  - 在 `internal/story/assets/` 目录下创建 `style.css`、`dashboard.js` 等静态资产文件；
  - 在 `render_html_assets.go` 中使用 Go 1.16+ `//go:embed` 将静态文件嵌入为变量，替代原来的巨型字符串字面量。
- **验收单测**：
  - 运行 `internal/story/render_html_test.go`，确保生成的 HTML 页面内容与此前 100% 字节一致。

### 任务 2: `ctxgraph` 历史消息 Hash 计算局部缓存优化 (ISSUE-10)
- **背景与根因**：`internal/ctxgraph/manifest.go:178` 在长会话扫描期间，每一轮均对全量历史消息对象调用 `json.Marshal` 计算 MD5 哈希。在首轮包含超大 Base64 图片时产生 $O(N^2)$ 的重复序列化开销。
- **目标修改**：
  - 在会话扫描或 `BuildManifest` 期间，对未变更的历史消息 Hash 进行局部复用或缓存，避免对前序相同消息反复调用 `json.Marshal`。
- **验收单测**：
  - 运行 `internal/ctxgraph` 全量单测，确保 Manifest 生成的哈希序列与基准完全一致。

### 任务 3: `story` Step 事实增量提取优化 (ISSUE-16)
- **背景与根因**：`story.buildFrom` 针对每个 Step 均全量解析消息，未利用 `ctxgraph` 已计算出的 `DeltaStart`（LCP 最长公共前缀）。
- **目标修改**：
  - 在生成 Step 事件与事实时，结合 `DeltaStart` 仅对本轮新增的消息进行深度提取，前序消息直接复用，降低长会话解析 CPU 占用。
- **验收单测**：
  - 运行 `internal/story` 下所有单测，确保 Journey 产出的 Event 与 Step 事实完全一致。

### 任务 4: `internal/config` 校验长函数物理拆分 (ISSUE-17)
- **背景与根因**：`internal/config/config.go` 体积庞大，校验方法堆积。
- **目标修改**：
  - 将 `config.validate()` 及相关的各子系统校验函数迁移至同包的新文件 `config_validate.go`，降低 `config.go` 单文件行数至预算内。
- **验收单测**：
  - 运行 `internal/config` 下全量单元测试与 `archtest`。

---

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/story/... ./internal/ctxgraph/... ./internal/config/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 全局测试：`go test -race ./...`
4. 检查变更范围：`git status -s`（确认无白名单外文件变动）
5. 执行 Commit：`git add -A && git commit -m "refactor(analytics): embed story assets, manifest hash cache, incremental step facts, and split config validate"`
