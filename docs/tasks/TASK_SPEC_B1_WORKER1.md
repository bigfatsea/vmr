# 任务说明书：Batch 1 - Worker 1 (Core Routing, Audit & Ingress Fast Fixes)

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：
     - `internal/respnorm/respnorm.go`
     - `internal/respnorm/respnorm_test.go`
     - `internal/router/transport.go`
     - `internal/audit/audit.go`
     - `internal/audit/audit_test.go`
     - `internal/chatmsg/usage.go`
     - `internal/chatmsg/messages.go`
     - `internal/chatmsg/usage_test.go` (若无则可在 `chatmsg` 包内现有测试文件中添加或新建单测)
     - `internal/chatmsg/messages_test.go` (或现有测试文件)
     - `vmr.sh`
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `CHANGELOG.md`、`KNOWN_ISSUES.md` 或 `docs/` 下的任何设计文档。待登记事项写入不提交的 `NOTES_FOR_LEAD.md`。
3. 代码风格与架构门禁：遵循标准库风格，保持零多余分配，不得引入任何新外部依赖。
4. Git 规范：提交信息简明（如 `fix(respnorm): guard opaque write with mutex; enable http2 upstream; audit html escape; chatmsg response usage`），无 trailer。
5. 忽略目录：`_tmp/`、`archived/`、`logs/`、`reports/` 以及临时编译产物目录视为不存在，严禁读取或修改。

## 二、具体修复任务清单 (Action Plan)

### 任务 1: P-1-1 `respnorm.stream.opaque` 数据竞争修复
- 背景与根因：`internal/respnorm/respnorm.go:459` 和 `:474` 在大响应溢出（>8MB）时，无锁写入 `s.opaque = true`，紧邻下一行就是持锁调用的 `s.noteApplied`。而在消费侧 `OutTokens()`（`:903`）持锁读取 `s.opaque`，产生数据竞争。
- 目标修改：
  1. 在 `respnorm.go` 中，将两处 `s.opaque = true` 移入 `s.mu` 保护下（例如在 `s.mu.Lock()` 块内，或利用 `noteApplied` 守护/持锁设置）。
  2. 在 `internal/respnorm/respnorm_test.go` 中补充 `-race` 覆盖测试：在大响应触发 passthrough 降级时并发调用 `OutTokens()`，验证无 race。

### 任务 2: P-1-2 上游 Transport 启用 HTTP/2
- 背景与根因：`internal/router/transport.go:47` 中设置了自定义 `DialContext`，导致 Go 标准库 `http.Transport` 默认禁用了 HTTP/2 自动升级。
- 目标修改：
  1. 在 `internal/router/transport.go` 的 `&http.Transport{...}` 中显式加上 `ForceAttemptHTTP2: true`。

### 任务 3: P-2-1 审计日志落盘禁用 HTML 转义
- 背景与根因：`internal/audit/audit.go:599` 中 `json.NewEncoder(buf).Encode(rec)` 默认会对 `<`、`>`、`&` 进行 HTML 转义为 `\u003c` 等，破坏了线缆级字节保真（byte-for-byte fidelity）。
- 目标修改：
  1. 在 `audit.go` 的 `Logger.Write` 中：
     ```go
     enc := json.NewEncoder(buf)
     enc.SetEscapeHTML(false)
     if err := enc.Encode(rec); err != nil { ... }
     ```
  2. 在 `internal/audit/audit_test.go` 中增加单测：写入包含 `<foo & "bar">` 的 `audit.Record`，验证落盘读回后为原始 `<foo & "bar">` 字符串，不含 `\u003c` / `\u0026`。

### 任务 4: P-D4-1 `chatmsg.mergeUsage` 支持 OpenAI Responses 嵌套 usage
- 背景与根因：`internal/chatmsg/usage.go:155` 的 `mergeUsage` 仅探测 `obj["usage"]` 和 `Nested(obj, "message", "usage")`。在 openai-responses 协议流式响应中，usage 位于 `Nested(obj, "response", "usage")`。
- 目标修改：
  1. 在 `mergeUsage` 的探测列表中增加 `Nested(obj, "response", "usage")`。
  2. 补充测试用例，验证包含 `response.usage` 对象的 responses 协议 payload 能被正确提取。

### 任务 5: P-D4-2 `chatmsg.RenderPart` 支持 Anthropic `document` 类型
- 背景与根因：`internal/chatmsg/messages.go:123` 的 `RenderPart` 缺少 `case "document":` 分支，导致 Anthropic PDF/Document 附件（包含数 MB base64）落入 `default: jsonIndent(m)`，被全量输出。
- 目标修改：
  1. 在 `RenderPart` 中增加 `case "document":`，比照已有的 `input_file` 与 `image` 处理，解析 `source` 中的 `media_type` 和 base64 数据长度，输出 `📄 [document application/pdf ~1.2MB]` 等紧凑摘要，避免输出巨大 base64。
  2. 补充测试用例验证 `document` 类型的渲染。

### 任务 6: P-7-1 `vmr.sh` 子命令白名单增加 `analyze`
- 背景与根因：`vmr.sh:603` 的 `-c` 自动注入白名单包含 `start|check|status|diagnose|smoke|replay|report|story)`，但遗漏了统一入口 `analyze`。
- 目标修改：
  1. 在 `vmr.sh:603` 将 `analyze` 加入 case 分支列表。

## 三、测试与验收步骤
1. 运行相关单测并开启 race 检测：
   ```bash
   go test -v -race ./internal/respnorm/... ./internal/router/... ./internal/audit/... ./internal/chatmsg/...
   ```
2. 运行架构门禁测试：
   ```bash
   go test -v ./internal/archtest/...
   ```
3. 检查变更状态：
   ```bash
   git status -s
   ```
   （严格确认仅修改了白名单文件）
4. 执行 Commit：
   ```bash
   git add internal/respnorm internal/router/transport.go internal/audit internal/chatmsg vmr.sh
   git commit -m "fix(core): mutex-guard opaque, enable upstream h2, disable audit html escape, and expand chatmsg formats"
   ```
