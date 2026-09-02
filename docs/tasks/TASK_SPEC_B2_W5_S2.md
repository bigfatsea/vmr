# 任务说明书：Batch 2 - Worker 5（S-2 机制：让沉默变响 + P-5-2 providerquota 跳过上报）

## 一、协作原则与红线约束
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：
     - `internal/report/providerquota.go`、`internal/report/providerquota_test.go`
     - `internal/chatmsg/usage.go`、`internal/chatmsg/messages.go`、`internal/chatmsg/sse.go`（仅在需要计数触点时）、`internal/chatmsg/*_test.go`
     - `internal/story/corpus_contextrot.go`、`internal/story/corpus_contextrot_test.go`
     - `cmd/vmr/protocol_coverage_test.go`
   - ❌ 严禁修改：白名单以外任何文件。**严禁修改 CHANGELOG.md、KNOWN_ISSUES.md、docs/** 下任何设计文档。
3. W4 已合并入 main，`UsageInOK`/`UsageOutOK` 可用。`ctxgraph.Manifest.UsageInOK`/`UsageOutOK` 和 `report.ReqInfo` 的同名字段已存在。
4. Git 规范：提交信息简明，无 trailer。可分多个 commit。
5. 忽略目录：`_tmp/`、`archived/`、`logs/`、`reports/`、`_review/`。

## 二、具体任务清单

### 任务 1: P-5-2 §2.5 配额窗口静默漏计 => 让沉默变响
- 背景：`internal/report/providerquota.go:204` 遇到 `quotas[provider]` 不匹配时直接 `continue`，不计数不标记不提示。
- 目标：
  1. 在 `providerquota.go` 的遍历循环中累计跳过数（`skippedAttempts`）。
  2. 在 §2.5 表下渲染一行 `"%d attempts skipped (unknown provider: %s, %s, …)"`（列出前 3 个不匹配的 provider 名，+ 剩余总数）。
- 不需要修复归因逻辑（跨改名点的归因是另一个问题），只需让静默变响。

### 任务 2: `chatmsg` 未识别形状计数
- 背景：`mergeUsage` 的 3 个 holder 列表和 `RenderPart` 的 switch-case 各有一个 `default` 分支，新形状落入时静默不处理。
- 目标：
  1. 为未识别的 content part type 和 usage holder 设计计数机制（包级原子计数器/返回计数/其他——你选择，但 ⚑ 不得引入 goroutine-unsafe 全局状态，不得影响路由半区的热路径）。
  2. 在 `vmr analyze` 输出中显示一行（例如 `"%d request(s) with unrecognized content part(s), %d with unrecognized usage holder(s)"`），让操作员知道存在未识别的形状谱系。

### 任务 3: `cmd/vmr/protocol_coverage_test.go`
- 背景：测试的输入形状词汇表和生产代码的枚举列表是同一份手工清单，永远同时漏同一形态。
- 目标：
  1. 新增 `protocol_coverage_test.go`，由 `adapter.Names()`（`internal/adapter/adapter.go:92`）驱动，遍历已注册的协议 × 典型响应形状。
  2. 至少覆盖：正常 JSON 响应、SSE 流式响应、截断流、4xx 错误、softblock 形状（2xx + `error_class` content）。
  3. ⚑ 测试对 `chatmsg` 包命中靶心（usage 解析正确 + 形状识别计数正确），不依赖完整 audit 管线（单元测试，非端到端）。选址在 `cmd/vmr` 有先例（`quota_parity_test.go`）。

### 任务 4: `corpus_contextrot` unknown 桶
- 背景：`internal/story/corpus_contextrot.go:71-73` 把 `UsageInOK=false` 的步骤归入 "0-32k" 桶，污染最小桶的错误率（W4 已落地 `UsageInOK`/`UsageOutOK`）。
- 目标：
  1. 在 `bucketIndexForTokens` 之前判断 `s.Manifest == nil || !s.Manifest.UsageInOK`，命中则跳过归桶或归入专门的 "usage unknown" 伪桶。
  2. 在 Context Rot 表下方注明 `"%d step(s) excluded: no in-token usage data"`。

## 三、测试与验收步骤
1. `go test -race -count=1 ./internal/report/ ./internal/chatmsg/ ./internal/story/ ./cmd/vmr/ ./internal/archtest/`
2. `gofmt -l .` 无输出
3. `git status -s` 确认无白名单外文件
4. 如有任何语义性测试改动，逐条注明于 NOTES_FOR_LEAD.md