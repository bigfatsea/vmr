# 任务说明书：Group 3 - 报文解析性能与健壮性修复 (Parsing Engine & SSOT Guard)

## 一、协作原则与红线约束（铁律）
1. **工作区限制**：仅在当前指定的 Worktree 目录下操作，严禁跨目录。
2. **文件修改白名单（极度关键）**：
   - ✅ 允许修改：
     - `internal/chatmsg/usage.go`
     - `internal/chatmsg/sse.go`
     - `internal/chatmsg/usage_test.go`
     - `internal/chatmsg/sse_test.go`
     - `internal/story/llm.go`
     - `internal/story/llm_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！
3. **架构门禁与规范**：
   - 严格遵守 `internal/archtest` 架构约束与行数预算（默认单文件 700 行，单函数 120 行）。
   - `chatmsg` 保持单一事实源（SSOT）无状态纯函数特性，可以依赖 `internal/jsonscan`，绝不依赖 `router`/`server`/`config`。
4. **Git 规范**：
   - Commit message 保持精炼，无任何 trailer。
5. **共享文件禁改**：
   - `CHANGELOG.md` / `KNOWN_ISSUES.md` / 设计文档由主控独占，严禁修改；待记录项写入不提交的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**：
   - `_tmp/`、`archived/` 视为不存在，严禁读取或修改。

---

## 二、具体任务清单 (Action Plan)

### 任务 1: `extractTruncatedText` 转义引号 `IndexUnescapedQuote` 修复 (ISSUE-03)
- **背景与根因**：`internal/chatmsg/usage.go:292` 中的 `extractTruncatedText` 使用 `bytes.IndexByte(val, '"')` 查找 JSON 字符串结束位置。当异常截断文本中包含转义双引号 `\"`（如代码块、转义对话）时，误将内联引号识别为闭合符，导致文本被腰斩、Token 估算严重低估。
- **目标修改**：
  - 在 `extractTruncatedText` 中，使用 `jsonscan.IndexUnescapedQuote(val)` 代替朴素的 `bytes.IndexByte(val, '"')`。
  - 确保包含 `\"` 的文本能完整提取至真实的未转义双引号处（或 buffer 尾部）。
- **验收单测**：
  - 在 `internal/chatmsg/usage_test.go` 中补充测试用例，验证输入包含 `\"hello world\"` 的截断 JSON 时文本不会被提前截断。

### 任务 2: 长流式 SSE 报文全量 `strings.Split("\n")` 消除 (ISSUE-09)
- **背景与根因**：`internal/chatmsg/sse.go:31` (`ReassembleSSE`) 与 `internal/chatmsg/usage.go:118` (`MergeUsageWithProtocol`) 在解析大型 SSE 字符串/字节流时直接调用 `strings.Split(raw, "\n")`。对于 10MB 量级的大型流，会产生数十万个子串切片指针与巨大的 GC 压力。
- **目标修改**：
  - 将 `ReassembleSSE` 与 `MergeUsageWithProtocol` 中的 `strings.Split` 循环改写为基于游标的行扫描（如 `strings.IndexByte(raw, '\n')` 或 `bytes.IndexByte(b, '\n')`），逐行流式解析，彻底消除 `[]string` 切片数组的堆分配。
- **验收单测**：
  - 运行 `internal/chatmsg` 既有全部单测，并补充大型多行 SSE 报文的解析一致性测试。

### 任务 3: `story` LLM 解释层增加退避重试与超时弹性 (ISSUE-24)
- **背景与根因**：`internal/story/llm.go:315` 中的 HTTP Client 设置了固定 120 秒超时且无任何重试。遇到网络抖动或 429 时长上下文分析直接中断。
- **目标修改**：
  - 在 `story/llm.go` 的 `doLLMRequest` / `CallLLM` 逻辑中，增加对 429 / 5xx / 瞬态网络错误的重试机制（如最多重试 2 次，带指数退避和 Jitter），并正确读取 `Retry-After` Header。
- **验收单测**：
  - 在 `internal/story/llm_test.go` 中补充重试相关的模拟单测。

---

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/chatmsg/... ./internal/story/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 全局测试：`go test -race ./...`
4. 检查变更范围：`git status -s`（确认无白名单外文件变动）
5. 执行 Commit：`git add -A && git commit -m "fix(parsing): unescaped quote fix, cursor SSE scan, and llm retry"`
