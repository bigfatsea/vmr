# 任务说明书：Unit 4 - 零分配与配置鲁棒性打磨 (Zero-Alloc & Config Hardening)

## 一、协作原则与红线约束（铁律）
1. **工作区限制**：仅在当前指定的 Worktree 目录下操作，严禁跨目录。
2. **文件修改白名单（极度关键）**：
   - ✅ 允许修改：
     - `internal/tokenutil/tokenutil.go`
     - `internal/tokenutil/tokenutil_test.go`
     - `internal/config/config.go`
     - `internal/config/config_validate.go`
     - `internal/config/config_test.go`
     - `internal/config/quota.go`
     - `internal/config/quota_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！
3. **架构门禁与规范**：
   - 严格遵守 `internal/archtest` 架构约束与行数预算（单文件 700 行，单函数 120 行）。
   - 零新增外部依赖，纯 Go 标准库。
4. **Git 规范**：
   - Commit message 保持精炼，无任何 trailer（如 Co-Authored-By 等）。
5. **共享文件禁改**：
   - `CHANGELOG.md` / `KNOWN_ISSUES.md` / 设计文档由主控独占，严禁修改；待记录项写入不提交的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**：
   - `_tmp/`、`archived/` 视为不存在，严禁读取或修改。

---

## 二、具体任务清单 (Action Plan)

### 任务 1: `tokenutil.EstimateText` 实现纯字符串遍历零堆分配 (IS-09)
- **背景与根因**：`internal/tokenutil/tokenutil.go:121-123` 中，`EstimateText(s)` 实现为 `return Estimate([]byte(s))`，存在一次强制的 `[]byte` 堆内存拷贝与逃逸，违背了叶子包零内存分配（Zero heap allocation）的承诺。
- **目标修改**：
  - 在 `tokenutil.go` 中新增 `AnalyzeString(s string) CharStats`，通过 `for _, r := range s` 直接遍历字符串字符分类。
  - 将 `EstimateText(s string) int64` 改为调用 `EstimateFromStats(AnalyzeString(s))`，消除 `[]byte(s)` 内存分配。
- **验收单测**：
  - 在 `internal/tokenutil/tokenutil_test.go` 中补充 `BenchmarkEstimateText` 或 `testing.AllocsPerRun` 测试，验证 `EstimateText` 达到 0 allocs/op。

### 任务 2: 负数配置参数改为 Fail-fast 校验报错 (IS-10)
- **背景与根因**：`internal/config/config.go:405-413` 中，`max_attempts < 0`、`max_concurrency < 0`、`audit_retention_days < 0` 等非法负数在 `applyDefaults` 中被静默重置为 0，违背了全仓 Fail-fast 严格校验纪律。
- **目标修改**：
  - 在 `internal/config/config_validate.go` 的 `validateBasic` 中增加对 `MaxAttempts < 0`、`MaxConcurrency < 0`、`AuditRetentionDays < 0` 的显式拦截报错。
  - 调整 `applyDefaults` 与测试用例中对负数配置的预期行为。
- **验收单测**：
  - 更新 `internal/config/config_test.go`，验证负数配置能正确触发加载失败错误。

### 任务 3: `parseSince` 纯时间解析统一继承基准时间锚点 (IS-13)
- **背景与根因**：`internal/config/quota.go:228-230` 中，`parseSince` 在解析 `hh:mm[:ss]` 纯时间格式时内部直接调用了 `time.Now()`，未继承统一传入的基准时间锚点，破坏了配置测试与重载的确定性。
- **目标修改**：
  - 为 `parseSince` 增加 `now time.Time` 参数，从上层 `validateQuota` / `validateLimit`（已有 `now` 参数）统一下传。
- **验收单测**：
  - 在 `internal/config/quota_test.go` 中验证纯时间格式解析能够精确绑定传入的基准锚点。

### 任务 4: 环境变量 `${ENV}` 替换增加行首 `#` 注释截断防御 (IS-15)
- **背景与根因**：`internal/config/config.go:371-378` 中，`badVar` 检查了 `\n`、`: ` 和 ` #`，但若环境变量以 `#` 开头（例如 `API_KEY="#secret"`），展开后整行在 YAML 语义下变成注释，导致值静默变为空串。
- **目标修改**：
  - 在 `expandEnv` 的非法字符判定中增加 `strings.HasPrefix(strings.TrimSpace(v), "#")` 校验。
- **验收单测**：
  - 在 `internal/config/config_test.go` 中增加单测，验证包含以 `#` 开头的环境变量时 `expandEnv` 触发明确报错。

---

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/tokenutil/... ./internal/config/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 全局测试：`go test -race ./...`
4. 检查变更范围：`git status -s`（确认无白名单外文件变动）
5. 执行 Commit：`git add -A && git commit -m "perf(config): zero-alloc tokenutil string estimate, strict fail-fast config bounds, anchored parseSince, and env comment guard"`
