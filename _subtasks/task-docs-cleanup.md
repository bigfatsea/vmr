# 任务说明书：文档与历史命令称谓清理专项 (T-A, T-B, T-C, T-F)

## 一、协作原则与红线（铁律）
1. 工作区限制：仅在当前指定 worktree 目录内操作。
2. 变更白名单（极度关键）：
   - ✅ 仅允许修改白名单内的文件（注释与文档），具体见下文清单。
   - ❌ 绝对禁止修改主控正在修改的文件：
     `internal/report/metrics.go`、`internal/report/provider.go`、`internal/report/slices.go`、`internal/report/requests.go`、`internal/journey/structure.go`、`cmd/vmr/cmd_analyze_cache.go`、`config.yaml`、`CHANGELOG.md`、`docs/future-strategy/analyze_redesign_review_v4_claude-sonnet-5.md`。
3. 共享文件由主控独占：`CHANGELOG.md` 与主 review 报告由主控独占；待登记事项写入不提交的 `NOTES_FOR_LEAD.md`。
4. 交付规范：
   - 保持代码行为不变（零功能变更、仅更新 doc comment、字符串注释、测试断言失败信息里的旧命令名）。
   - 保留角色概念描述（如合法描述 "report 半区 / journey 半区" 不需要替换）。
   - Commit 规范：短小命令式，严禁任何 trailer（含 `Co-Authored-By` 等）。

---

## 二、任务清单 (Action Plan)

### 任务 1: T-A 注释与测试符号里的历史命令称谓清理 (J-01)
- **背景与目标**：
  历史命令 `vmr story` 与 `vmr report` 已收敛为 `vmr analyze`；旧索引 `vmr-stories.json` 已改为 `journeys/index.json`；旧目录 `reports/stories/` 已改为 `journeys/`。但在约 50 个 Go 文件的注释与测试失败报错字符串中仍残留旧命令称谓。
- **具体要求**：
  1. 逐文件审查以下文件中的注释及测试信息：
     - 命令语境：`vmr story` / `vmr report` → `vmr analyze`（如 `vmr story -compare` → `vmr analyze -compare`，`vmr story -journey` → `vmr analyze -journey`，`vmr report -details` → `vmr analyze -details` 等）。
     - 产物语境：`vmr-stories.json` → `journeys/index.json`；`storiesDir` / `reports/stories/` → `journeys/`。
     - 测试报错语境：`cmd/vmr/cmd_journey_test.go` 中 `t.Fatalf("cmdStory ...")` → `t.Fatalf("cmdJourney ...")` 或 `t.Fatalf("analyze ...")`。
     - `internal/archtest/import_boundaries_test.go` 中关于 `vmr story` / `vmr report` 的说明性注释更新。
  2. **重要甄别**：保留合法描述系统两半区的词汇（如 "report 半区 / journey 半区"、"report 侧 / journey 侧"）。
  3. 白名单 Go 文件列表：
     - `_eval/calibrate_p1b.go`
     - `loadtest/runner/main.go`
     - `tools/gen_standard_pricing/main.go`
     - `cmd/vmr/selftraffic.go`
     - `cmd/vmr/reportconfig.go`
     - `cmd/vmr/cmd_report_quota.go`
     - `cmd/vmr/taskprofile.go`
     - `cmd/vmr/cmd_journey_test.go`
     - `cmd/vmr/cmd_analyze_test.go`
     - `cmd/vmr/i18n_e2e_test.go`
     - `cmd/vmr/cmd_report_pricing_test.go`
     - `cmd/vmr/check_pricing.go`
     - `cmd/vmr/quota_parity_test.go`
     - `cmd/vmr/cmd_journey_report_crosscheck_test.go`
     - `internal/archtest/func_sizes_test.go`
     - `internal/archtest/import_boundaries_test.go`
     - `internal/pricing/resolve.go`
     - `internal/pricing/resolve_test.go`
     - `internal/pricing/pricing.go`
     - `internal/pricing/pricing_test.go`
     - `internal/pricing/resolver.go`
     - `internal/server/server.go`
     - `internal/server/server_test.go`
     - `internal/server/recorder.go`
     - `internal/fmtutil/timezone.go`
     - `internal/fmtutil/fmtutil.go`
     - `internal/audit/audit.go`
     - `internal/core/core.go`
     - `internal/respnorm/respnorm.go`
     - `internal/respnorm/respnorm_test.go`
     - `internal/rundir/rundir.go`
     - `internal/i18n/lang.go`
     - `internal/reqdetail/ensure.go`
     - `internal/reqdetail/ensure_test.go`
     - `internal/reqdetail/render.go`
     - `internal/replay/replay.go`
     - `internal/journey/journeyindex.go`
     - `internal/journey/journeyindex_test.go`
     - `internal/journey/journey.go`
     - `internal/journey/ensure_details.go`
     - `internal/quota/quota.go`
     - `internal/quota/weight.go`
     - `internal/quota/store.go`
     - `internal/ctxgraph/cache.go`
     - `internal/ctxgraph/cache_test.go`
     - `internal/ctxgraph/manifest_test.go`
     - `internal/config/config.go`
     - `internal/config/quota.go`
     - `internal/config/pricing.go`
     - `internal/config/pricing_test.go`
     - `internal/chatmsg/sse_test.go`
     - `internal/report/selftraffic_test.go`
     - `internal/report/detail.go`
     - `internal/report/aggregate.go`
     - `internal/report/session_test.go`

### 任务 2: T-B `docs/VirtualModelRouter_Design_v4_Analytics.md` 更新
- **背景与目标**：
  该文档 §4 仍存在已废弃的 `section_*.go` 及历史 `story` 包名引用。按 "current state, not changelog" 准则更新。
- **具体要求**：
  1. §4.1 / §4.3 / §4.4 / §4.5 与附录表：
     - `section_<x>.go` → `viewmodel_<x>.go`（report 侧）；
     - `story_render.go` → `i18n/journey_render.go`；
     - `story.Interpret` → `journey.Interpret`；
     - `report`/`story` 包名对 → `report`/`journey`；
     - `internal/journey`（原 `story`）；
     - §2.3 中已知缺口提及的 "暂缓到额度看板（`section_quota.go`）那批" 改为泛指 "暂缓到额度展示相关改动那批"。
     - 其它历史遗留的 `story` 包名称谓统一为 `journey`。

### 任务 3: T-C 看板 SVG 图元「散点」未按字面实现之文档更新
- **背景与目标**：
  方案 §6.3 曾列 "折线/柱状/热力/散点四种形态"，实际看板中散点被专用延迟分位图元（`svgLatencyPlot`）替代。
- **具体要求**：
  在 `docs/future-strategy/analyze_architecture_redesign_opus-5.md` 中：
  1. §6.3 中将 "散点" 更新为 "延迟分位图（`svgLatencyPlot`）"。
  2. 在 §11.2 取舍表中添加一行说明：看板选用专用延迟分位图元而非通用二维散点，满足端点时延分布的真实可视化诉求。

### 任务 4: T-F 观察项文档注记 (N12 / N18)
- **背景与目标**：
  N12: 方案 §7.2 中提到 "-from/-to"，实际 CLI 输入为 glob 路径；
  N18: `-render-only` L3 暖缓存命中信任磁盘产物。
- **具体要求**：
  1. 在 `docs/future-strategy/analyze_architecture_redesign_opus-5.md` §7.2 处添加简短注记，说明时间覆盖由审计输入文件本身的内容哈希集合决定，CLI 通过文件 glob 指定输入范围。
  2. 在 `KNOWN_ISSUES` 中简要登记：`-render-only` 在 L3 缓存命中时信任磁盘上已有 `.md` 产物，若用户手工修改了 `.md` 文件需要强制重绘，应使用 `-no-cache`。

---

## 三、验收与交付
1. 自验：
   - 运行 `go test ./...` 确保所有包（尤其 archtest）全绿。
   - 运行 `git diff` 确认没有意外破坏任何业务逻辑或编译单元。
2. 范围自查：
   - `git status -s` 确认只修改了上述白名单范围内的文件。
3. 交付：
   - 执行 git commit，信息例如：`docs: clean up retired command references and sync analytics design doc`（无 trailer）。
