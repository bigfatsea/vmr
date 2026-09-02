# 任务说明书：Batch 1 - Worker 2 (Meta Guards, Dead Code Cleanup, Env Guards & i18n)

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：
     - `internal/pricing/pricing.go`
     - `internal/logtee/logtee.go`
     - `internal/logtee/logtee_test.go`
     - `internal/archtest/doc_refs_test.go`
     - `internal/archtest/i18n_test.go` (如新建或修改此测试)
     - `cmd/vmr/reportconfig.go`
     - `cmd/vmr/reportconfig_test.go`
     - `internal/report/pricing.go`
     - `internal/i18n/report_cost.go`
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `CHANGELOG.md`、`KNOWN_ISSUES.md` 或 `docs/` 下的任何设计文档。待登记事项写入不提交的 `NOTES_FOR_LEAD.md`。
3. 代码风格与架构门禁：遵循 Go 标准库设计原则，不引入外部依赖，保持 archtest 全部通过。
4. Git 规范：提交信息简明（如 `fix(meta): remove dead code, align env injection guards, and add archtest i18n pairing guard`），无 trailer。
5. 忽略目录：`_tmp/`、`archived/`、`logs/`、`reports/` 以及临时编译产物目录视为不存在，严禁读取或修改。

## 二、具体修复任务清单 (Action Plan)

### 任务 1: D-1 / D-2 死代码清理
- 背景与根因：
  - `internal/pricing/pricing.go:287` 中的 `IsAggregatorVendor` 全仓零引用（注释声称被 `gen_standard_pricing` 调用，但工具实际调用的是 `Ambiguities()`）。
  - `internal/logtee/logtee.go:123, :141` 中的 `Recent` 与 `Subscribe` 在生产代码中零调用（`/log` 实际使用 `Follow`）。
- 目标修改：
  1. 删除 `internal/pricing/pricing.go` 中的 `IsAggregatorVendor` 及其注释。
  2. 删除 `internal/logtee/logtee.go` 中的 `Recent` 和 `Subscribe` 方法及其注释；同步清理 `internal/logtee/logtee_test.go` 中针对已被删除方法的测试用例，保留对 `Follow`、`Write`、`RingBuffer` 等在用功能的单测。

### 任务 2: S-1 `archtest/doc_refs_test.go` 增强
- 背景与根因：注释中用反引号引用的 `` `pkg.Symbol` `` 符号虽被校验了存在性，但缺少对已删除死代码符号零引用的警示机制。
- 目标修改：
  1. 确保 `doc_refs_test.go` 在校验注释中引用时，不会遗留指向已删除死代码符号（如 `pricing.IsAggregatorVendor`）的失效引用。

### 任务 3: i18n ↔ report 模块一对一配对架构门禁
- 背景与根因：`KNOWN_ISSUES.md` 曾声称微文件 `i18n/report_*.go` 与 `internal/report/section_*.go` 的一一配对由 archtest 强制，但此前 archtest 缺少此项自动化检查。
- 目标修改：
  1. 在 `internal/archtest/` 下（如在 `i18n_test.go` 或 `doc_refs_test.go` 中）补充测试：比对 `internal/report/section_*.go` 与 `internal/i18n/report_*.go` 的模块名集合，断言二者一一对应（若有特例则显式在测试内白名单列出）。

### 任务 4: P-3-2 `reportconfig.go` 环境变量注入守卫对齐
- 背景与根因：`cmd/vmr/reportconfig.go:97` 中的 `expandReportEnv` 比 `internal/config/config.go:373` 少了一项守卫 `strings.HasPrefix(strings.TrimSpace(v), "#")`。且若配置中有以 `#` 开头的注释行，不应将其中的 `${...}` 当作未设置变量或非法注入报错。
- 目标修改：
  1. 在 `expandReportEnv` 中补齐 `strings.HasPrefix(strings.TrimSpace(v), "#")` 守卫，与 `config.go` 的 fail-fast 规则完全一致。
  2. 完善 `cmd/vmr/reportconfig_test.go`（若不存在则创建），编写表驱动测试覆盖换行、`: `、` #` 以及 `#` 前缀注入场景。

### 任务 5: P-5-5 `report/pricing.go` 硬编码文案迁移至 i18n
- 背景与根因：`internal/report/pricing.go:44-50` 中的币种降级文案 `“（请求以 ... 展示，因未配置汇率，降级以 ... 展示）”` 硬编码在 report 包中，未纳入 i18n 管理。
- 目标修改：
  1. 在 `internal/i18n/report_cost.go` 中为 `CostText` 添加方法或字段（如 `CurrencyFallbackNote(requested, actual string) string`），提供中英文支持。
  2. `internal/report/pricing.go` 改为调用 `i18n.Cost(lang)` 提供的翻译。

## 三、测试与验收步骤
1. 运行相关单测：
   ```bash
   go test -v -race ./internal/pricing/... ./internal/logtee/... ./internal/report/... ./cmd/vmr/...
   ```
2. 运行架构门禁测试：
   ```bash
   go test -v ./internal/archtest/...
   ```
3. 检查变更状态：
   ```bash
   git status -s
   ```
4. 执行 Commit：
   ```bash
   git add internal/pricing internal/logtee internal/archtest cmd/vmr/reportconfig.go cmd/vmr/reportconfig_test.go internal/report/pricing.go internal/i18n
   git commit -m "fix(meta): remove dead code, align env injection guards, and add archtest i18n pairing guard"
   ```
