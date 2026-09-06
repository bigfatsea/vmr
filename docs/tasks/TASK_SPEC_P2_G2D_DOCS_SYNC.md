# 任务说明书：Group 2D - 文档同步（2B 破坏性变更的 user-facing 文档改写）

> 背景：自包含 HTML 渲染器废弃（`-html`/`-redact` 删除、`tool-waste.html` 自包含卡片不再产出、骨架页 + fetch 替代），以及此前已落地的 CLI 收敛（`vmr report`/`vmr story` 别名删除、`-corpus`→`-benchmark`、`-story-only`→`-journey-only`）与产物拓扑重建（`stories/`→`journeys/`、`j-<id>` 文件名、`macro/*.json` 切片、`manifest.json`、人读请求索引删除、`requests/details|evidence/`）。代码事实以 main 为准（`cmd/vmr/main.go` 的 usage 文案、`docs/tasks/TASK_SPEC_P2_G2B_HTML_RETIREMENT.md` 的 NOTES 记录、CHANGELOG `[Unreleased]` 的 Breaking 条目）。
> 本组**只碰文档**，与并行作业的 3A/3B（代码组）完全正交。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：`docs/UserGuide.md`、`docs/UserGuide.zh.md`、`docs/VirtualModelRouter_Design_v4_Analytics.md`
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**CLAUDE.md、CHANGELOG.md、KNOWN_ISSUES、README**、任何 `.go`、任何 `docs/tasks/**` 均由主控独占。`VirtualModelRouter_Design_v4_Analytics.md` 无 `.zh` 姊妹篇，不新建。
3. 文档纪律（CLAUDE.md Conventions）：
   - **双语同步是硬规则**：UserGuide 的每处修改，`.md` 与 `.zh.md` 同键同构同步，仅文案翻译。
   - **Docs are current state, not changelogs**：把过期段落改写为当前状态，不追加"已于 X 日删除"式的修订历史（git history 已有故事）。
   - **不硬编码易变数量**；机制与类别优先。
   - **交叉引用用名称不钉章节号**。
4. Git 规范：短命令式提交信息，严禁任何 trailer。严禁 `git add .`。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: UserGuide 双语同步（主要内容）
以 `cmd/vmr` 实际行为为准逐段核对并改写：
1. **自包含 HTML 段落**（EN §633 附近、ZH §627 附近）：`-html`/`-redact` 段删除；骨架页 + fetch 形态替代描述（每次 analyze 幂等刷新六页、`#data=` 传参、需静态服务器或 `/reports/` 托管、`file://` 不支持）。
2. **`-compare` 段**（EN §653、ZH §647）：去掉 `-html` 对照看板的句子，其余保留；对比产物现在还有 `compares/index.{json,md}` 发现入口。
3. **CLI 速查表**（EN §720/§722、ZH §714/§716）：`vmr story` 行删除（别名已退役）；`vmr analyze` 行去掉 `-corpus`/`-story-only`/`-html`/`-redact`，补 `-benchmark`/`-journey-only`；正文里所有 `vmr report`/`vmr story` 提法与产物路径（`stories/vmr-stories.*`、`journey-*.md`、`vmr-requests.md`）改为当前拓扑（`journeys/index.*`、`j-<id>.{json,md}`、`journeys/benchmarks.*`、`macro/*.json`、`requests/*`）。
4. **tool-waste 卡片段**（EN §564、ZH §558）：自包含卡片描述改为"数据在 `macro/context-efficiency.json`，骨架页 `tool-waste.html` 消费它"。
5. 全文 grep `stories/`、`vmr-stories`、`journey-`、`-corpus`、`-story-only`、`-html`、`-redact`、`tool-waste.html`、`vmr-report.json`、`vmr-requests`，逐处核对是否仍与当前行为一致——不一致即改写。注意 `-html` 会误命中 `extra_redact_headers` 等无关词，人工判读。

### 任务 2: Analytics 设计文档同步
`docs/VirtualModelRouter_Design_v4_Analytics.md` §3.4（HTML 看板段，约 L225）与 §3.7（约 L307 的 `-html` 提法）按当前形态改写：自包含渲染器段落替换为骨架页 + fetch 形态（指向实现事实，不复制设计提案全文）；`-redact` 表述删除。其余章节不动。

## 三、测试与验收步骤
1. `go test ./internal/archtest/...`（doc_refs 守卫只认 internal 路径引用，改写时不要引入已删除文件的路径）
2. 双语对照自查：改动的每个小节，`.md`/`.zh.md` 结构一致
3. `git status -s` 确认仅三个白名单文件变更
4. Commit：`git commit -m "docs: sync user guide and analytics design to skeleton-page topology and cli convergence"`
