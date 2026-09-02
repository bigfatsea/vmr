# 任务说明书：Group 1 - 接入安全与生命周期加固 (Ingress & Lifecycle Security)

## 一、协作原则与红线约束（铁律）
1. **工作区限制**：仅在当前指定的 Worktree 目录下操作，严禁跨目录。
2. **文件修改白名单（极度关键）**：
   - ✅ 允许修改：
     - `internal/server/server.go`
     - `internal/server/server_test.go`（或新增针对性测试文件如 `internal/server/slowloris_test.go`）
     - `cmd/vmr/cmd_start.go`
     - `internal/router/candidates.go`
     - `internal/router/probe.go`
     - `internal/router/router_probe_test.go`
     - `internal/router/router.go`（仅允许为 Router 结构体注入 context 支持）
   - ❌ 严禁修改：白名单以外的任何文件！
3. **架构门禁与规范**：
   - 严格遵守 `internal/archtest` 架构约束与行数预算（默认单文件 700 行，单函数 120 行）。
   - 零新增外部依赖，纯 Go 标准库。
4. **Git 规范**：
   - Commit message 保持精炼，无任何 trailer（如 Co-Authored-By 等）。
5. **共享文件禁改**：
   - `CHANGELOG.md` / `KNOWN_ISSUES.md` / 设计文档由主控独占，严禁修改；待记录项写入不提交的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**：
   - `_tmp/`、`archived/` 视为不存在，严禁读取或修改。

---

## 二、具体任务清单 (Action Plan)

### 任务 1: HTTP Slowloris 慢读连接耗尽防御 (ISSUE-01)
- **背景与根因**：`cmd_start.go` 启动 `http.Server` 时仅设置了 `ReadHeaderTimeout: 10s`。在 `internal/server/server.go:203` 中，Handler 在获取并发槽位（`AcquireSlot`）之前直接调用 `io.ReadAll(http.MaxBytesReader(...))` 读取完整 Body。慢速恶意客户端以 1 字节/秒 的速率传输 Body 时，可无限期挂起底层 Goroutine 并占满连接池。
- **目标修改**：
  - 在 `internal/server/server.go` 中，在调用 `io.ReadAll` 读取 Body 之前，利用 Go 1.20+ `http.ResponseController` 设置读取截止时间（如 60s），读取完毕后重置截止时间（`time.Time{}`）：
    ```go
    rc := http.NewResponseController(w)
    _ = rc.SetReadDeadline(time.Now().Add(60 * time.Second))
    body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, snap.Cfg.MaxRequestBodyBytes()))
    _ = rc.SetReadDeadline(time.Time{})
    ```
- **验收单测**：
  - 在 `internal/server/` 下编写或补充单元测试，验证正常请求能顺利读取 Body，且当读取超时时能够正确捕获错误并返回 400 Bad Request。

### 任务 2: 配置热重载脱敏规则生效窗口竞态修复 (ISSUE-02)
- **背景与根因**：`cmd/vmr/cmd_start.go:217-220` 的 `reload` 函数中，先执行了 `rt.Install(newSnap)` 挂载新路由快照，随后才调用 `audit.SetExtraRedactHeaders(newCfg.ExtraRedactHeaders)`。在微秒级窗口期内，新进入的请求若携带新配置的敏感 Header 会被旧规则放行并明文落盘。
- **目标修改**：
  - 调整 `cmd_start.go` 中 `reload` 函数的执行顺序，先更新全局审计配置（`audit.SetRetentionDays`, `audit.SetExtraRedactHeaders`），最后挂载快照 `rt.Install(newSnap)` 并记录 reload 状态。

### 任务 3: 后台探针 Goroutine 优雅停机 Context 传播 (ISSUE-04)
- **背景与根因**：`internal/router/candidates.go:37` 中 `go rt.runProbe(ep, snap)` 触发后台探针时，`probe.go` 内部使用的是 `context.WithTimeout(context.Background(), ...)`，未绑定服务退出 Context。
- **目标修改**：
  - 为 `Router` 增加服务根上下文支持（如 `ctx context.Context` 与 `CancelFunc`，或在 `router.New` / `SetContext` 中传入），当服务收到关闭信号时，未决的后台探针能够及时感知并取消。

---

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/server/... ./internal/router/... ./cmd/vmr/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 全局测试：`go test -race ./...`
4. 检查变更范围：`git status -s`（确认无白名单外文件变动）
5. 执行 Commit：`git add -A && git commit -m "fix(security): patch slowloris timeout, reload redaction race, and probe ctx"`
