# 任务说明书：Group 2 - 流式转发热路径与内存优化 (Hot-Path Streaming & Buffer Pooling)

## 一、协作原则与红线约束（铁律）
1. **工作区限制**：仅在当前指定的 Worktree 目录下操作，严禁跨目录。
2. **文件修改白名单（极度关键）**：
   - ✅ 允许修改：
     - `internal/router/transport.go`
     - `internal/router/stream_health_test.go`
     - `internal/router/router_test.go`
     - `internal/respnorm/respnorm.go`
     - `internal/respnorm/respnorm_test.go`
     - `internal/respnorm/bench_test.go`
     - `internal/jsonscan/rewrite.go`
     - `internal/jsonscan/rewrite_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！
3. **架构门禁与规范**：
   - 严格遵守 `internal/archtest` 架构约束与行数预算（默认单文件 700 行，单函数 120 行）。
   - 坚守 Byte-Faithful Passthrough 原则，不引入通用的中间表示（IR）。
4. **Git 规范**：
   - Commit message 保持精炼，无任何 trailer。
5. **共享文件禁改**：
   - `CHANGELOG.md` / `KNOWN_ISSUES.md` / 设计文档由主控独占，严禁修改；待记录项写入不提交的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**：
   - `_tmp/`、`archived/` 视为不存在，严禁读取或修改。

---

## 二、具体任务清单 (Action Plan)

### 任务 1: `copyFlush` Ping-Pong Buffer 双缓冲池零分配改造 (ISSUE-06)
- **背景与根因**：`internal/router/transport.go:122` 中，`copyFlush` 的 Reader 协程对每个收到的 Chunk 执行 `data = append([]byte(nil), buf[:n]...)` 进行深拷贝，规避主协程 `w.Write` 时的竞态。长流式响应下产生数万次 32KB 堆分配。
- **目标修改**：
  - 在 `copyFlush` 中引入 Ping-Pong 双缓冲或容量为 2 的缓冲区通道 `bufPool := make(chan []byte, 2)`。
  - Reader 协程从 `bufPool` 取出切片读取数据，并将装有数据的切片通过 `ch` 发送给主协程；
  - 主协程完成 `w.Write` 与 `flusher.Flush()` 之后，将该切片重新放回 `bufPool` 供下一次读取复用。
  - 确保异常退出、超时（Idle Timer）、ClientWriteError 发生时，通道与协程均安全回收无泄漏。
- **验收单测**：
  - 运行 `internal/router` 下现有的流式转发、断流及健康测试，确保高并发与异常断流下 `-race` 全绿且零内存泄漏。

### 任务 2: `respnorm` 输出切片容量保护 (ISSUE-07)
- **背景与根因**：`internal/respnorm/respnorm.go:323-324` 在 `stream.Read` 中输出部分消费时执行 `s.out = s.out[n:]`，导致切片底层容量不断缩水。当完全消费（`len == 0`）后，下一次 `ingest` 追加写入时被迫重新分配数组并拷贝旧数据。
- **目标修改**：
  - 在 `stream.Read` 中：
    - 若 `n == len(s.out)`（全部消费）：执行 `s.out = s.out[:0]` 保留底层容量；
    - 若 `n < len(s.out)`（部分消费）：执行 `copy(s.out, s.out[n:])` 并 `s.out = s.out[:len(s.out)-n]` 前移数据。
- **验收单测**：
  - 运行 `internal/respnorm` 下全量单元测试与 `bench_test.go`，验证流式正规化功能完全一致且内存扩容次数显著下降。

### 任务 3: `RewriteModel` 零分配字符串引号拼接 (ISSUE-14)
- **背景与根因**：`internal/jsonscan/rewrite.go:31` 中的 `RewriteModel` 调用了 `MarshalNoEscape(model)` 生成带引号模型名，其实例化了 `bytes.Buffer` 和 `json.Encoder`。
- **目标修改**：
  - 避免创建 `json.Encoder`，改用标准库 `strconv.AppendQuote` 或预分配切片构建带双引号的目标模型 JSON 字节串。
- **验收单测**：
  - 运行 `internal/jsonscan` 下所有单测与 Fuzz 测试。

---

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/router/... ./internal/respnorm/... ./internal/jsonscan/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 全局测试：`go test -race ./...`
4. 检查变更范围：`git status -s`（确认无白名单外文件变动）
5. 执行 Commit：`git add -A && git commit -m "perf(streaming): double-buffering copyFlush, preserve respnorm out cap, and zero-alloc RewriteModel"`
