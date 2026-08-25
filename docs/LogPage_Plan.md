# `/log` 实时日志页 — 设计方案与执行计划

状态：已实施（见 git 历史）。本文档记录需求、方案与取舍；"决策记录"是各实现
决策的 why 出处。实施中与原稿的两处偏差已就地更新：心跳归属 handler 而非
logtee（D5），以及未接线的 Server 上 /log 返回 503（Step 2）。

## 1. 需求

vmr 的运行时日志（access log：某时刻来了什么请求、路由到哪个 endpoint、token 用量、
failover 过程等）目前只打到进程 stderr，运维上只能 `tail -f` 日志文件或盯着终端看。
目标：

- 浏览器直接访问 vmr 监听端口上的一个页面，即可看到这份日志的**持续实时输出**，
  效果等同网页版 `tail -f`。
- **不是 audit 日志**。audit（JSONL）是每请求的完整结构化记录，供 `vmr analyze`
  消费；这里要的是控制台上那行人类可读的摘要。两者数据源不同，互不替代。
- 尽量简单：优先一个流式接口解决问题；HTML 页面仅作为浏览器带不了
  `Authorization` header 的补足手段，且复用 `/status.html` 的既有模式。

## 2. 方案选择与取舍

### 2.1 数据从哪来

| 方案 | 思路 | 结论 |
| --- | --- | --- |
| **A. 进程内 tee + 内存环形缓冲** | 在 `cmd/vmr` 创建 logger 的 writer 外包一层 tee，写入有界 ring buffer 并广播给订阅者；`/log` 先回放缓冲再持续推送 | **采用** |
| B. 日志另落一份文件，`/log` 模拟 tail -f 读文件 | 需新增文件输出、每连接 offset、轮转边界处理 | 否决——多出一套文件状态机，而进程本来就拿得到日志行，绕道文件系统没有收益 |
| C. 复用 audit JSONL | 数据不对（完整记录 ≠ access log），且格式是给机器的 | 否决 |

选 A 的关键前提（已核实成立）：所有运行时日志都经过同一个 `*log.Logger`
（`cmd/vmr/cmd_start.go` 里 `log.New(stampWriter{os.Stderr}, "", 0)` 创建，
传给 `router.New(logger)`；server 侧借道 `rt.Logf`）。所以**只有一个拦截点**，
接线只改一行。已知例外：启动 banner 与 panic 直写 stderr，不被捕获——banner 只在
启动时出现一次、panic 时进程将死，均属可接受盲区。

### 2.2 流式协议：text/plain，不做 JSONL

**决定：`GET /log` 输出 `text/plain; charset=utf-8` 的 chunked 流，一行一条日志，
与 stderr 上看到的逐字节一致。**

理由：

- 日志在源头就已经是格式化好的文本行（`internal/router/logfmt.go`），logger 的
  `Write` 一次收到一行完整内容。要出 JSONL 就得在这里反向解析自己的文本格式，
  或重构 logfmt 为结构化事件再双路渲染——为当前唯一消费者（人眼 + 一个 HTML 页）
  引入一套 schema，不值。
- `log.html` 用 `fetch` + `ReadableStream` 逐块 append 到 `<pre>`，纯文本天然可行，
  不需要任何解析。
- 若将来出现机器消费方（如告警网关）需要结构化字段，届时**增量**加一个
  `/log.jsonl`（同一 tee、不同渲染），不构成返工。这是明确的扩展点，不是欠债。
- SSE 同理否决：`text/event-stream` 的帧格式对纯文本浏览是纯开销，且 EventSource
  无法携带自定义 header，鉴权问题并不会因此消失。

### 2.3 鉴权：`/log` 收紧、`/log.html` 裸壳

沿用 `/status` ↔ `/status.html` 的成熟分工：

- **`GET /log` 走 `s.auth`**（Bearer / x-api-key，与 `/status` 完全一致）。日志含
  client key tag、endpoint、模型名、用量，属敏感面，绝不裸奔。
- **`GET /log.html` 不鉴权**——它只是静态 HTML/JS 壳，不含任何业务数据。JS 先不带
  key 直接请求 `/log`；若配置了 api_keys 得到 401，则弹输入框要求输入 Key，
  重试时带上 `Authorization: Bearer <key>`。
- 因为有了 HTML 壳这条路，**第一版方案里的 `?key=` 查询参数凭证取消**——key 不进
  URL，也就不进代理日志和浏览器历史，比原方案更干净。

`log.html` 直接复用 `status.html` 的 `localStorage['vmr_status_key']`：两个页面共用
同一个 key，用户在其中一页输过，另一页免输。

补充说明（无需额外代码）：server 不返回 CORS 头，浏览器跨源读取被默认策略挡住，
外部网页无法诱导访客浏览器偷读本机 `/log`。

### 2.4 慢消费者：丢弃并插标记行

主路径（`rt.logf`，处于请求热路径上）**永不因订阅者阻塞**。每个订阅者一条有界
channel（容量 64 行）：写满即对该订阅者丢行，恢复时插入一行
`... dropped N lines ...` 标记后继续。

用户已确认"跳了就跳了没关系"；但**静默跳过不可取**——排障时会误读成
"vmr 没打日志"。标记行成本一行代码，保留。（若实现时嫌多余可去掉，不影响其余设计。）

### 2.5 空闲心跳：30s 一行空行

连接空闲超过 30 秒发一个 `\n`，防止中间层（反代、LB）掐断空闲连接。间隔做成
包级变量以便测试注入短间隔；直连场景下它同时起到"页面还活着"的视觉确认作用。

### 2.6 接口语义固定，零参数

- 连接建立即回放 ring buffer 全部内容（约最近几百行），随后自动跟随新行——
  默认就是 `tail -f` + `tail -n <buffer>` 的合体。
- **不支持任何查询参数**（第一版的 `?n=`/`?follow=` 取消）。能回放多少行由 ring
  缓冲尺寸决定，暴露成参数只会制造"参数给了但缓冲里没有"的错觉。行为固定、
  文档一句话说清。

### 2.7 决策记录（汇总）

| # | 决策 | 备选与否决理由 |
| --- | --- | --- |
| D1 | 进程内 tee + ring buffer，不落盘、不走文件 | 文件 tail 方案多一套状态机无收益 |
| D2 | 输出 text/plain 逐字节同 stderr，非 JSONL/SSE | 源头已是文本行；JSONL 需自解析或双路渲染，无消费者受益；未来可增量加 `/log.jsonl` |
| D3 | `/log` 走 s.auth；`/log.html` 裸壳 + JS 带 header + 401 弹窗 | 复用 status 模式；取消 `?key=` 参数，key 不进 URL |
| D4 | 订阅 channel 满 → 丢行 + `dropped N lines` 标记 | 主路径零阻塞优先；静默丢弃误导排障 |
| D5 | 30s 空闲心跳空行，间隔可注入测试；**实现在 `/log` handler 内**（`time.Timer` 收到真实日志行即重置，真正"空闲 30s"才发），logtee 保持无时序逻辑的纯广播总线 | 防中间层掐断；handler 是唯一观察得到投递活动的一方，时钟放它手里才是真的 idle 心跳 |
| D6 | 无查询参数，固定"全量回放 + 跟随" | 回放上限即缓冲尺寸，参数化制造错觉 |
| D7 | ring buffer 固定 512 行（常量，不进配置） | 单行通常 < 500B，总量 ~250KB 上限；进配置属于过度设计 |
| D8 | `log.html` 与 `status.html` 共用 `localStorage['vmr_status_key']` | 同一部署同一 key，免二次输入 |

## 3. 最终方案

### 3.1 新增叶子包 `internal/logtee`

零内部依赖（满足 archtest 叶子规则），不认识 log/server/router 的任何类型：

实际落地（`internal/logtee/logtee.go`）：内部为固定容量 `[]string` 环形缓冲 +
订阅者表，一把 mutex 保护全部状态（log.Logger 自身串行化 Write，锁竞争可忽略）。
API：`New(capLines int)`（<=0 panic）、`Write`（io.Writer，一次一行，追加并广播，
永不返回错误）、`Recent(n int)`（n<=0 返回全部）、`Subscribe()`（返回直播 channel +
取消函数）、`Subscribers()`（测试内省）。无任何时序参数——心跳在 handler 侧，
见 D5。

广播语义：向每个订阅者 non-blocking send；channel 满则置该订阅者的 dropped 计数，
下次成功投递前先补发标记行。订阅者断开（HTTP client 断连）调用取消函数，
从列表摘除并关闭 channel。

### 3.2 接线（`cmd/vmr/cmd_start.go`）

```go
tee := logtee.New(logtee.DefaultCapLines) // 512
logger := log.New(stampWriter{io.MultiWriter(os.Stderr, tee)}, "", 0)
...
srv := &http.Server{ ..., Handler: server.New(rt, auditLog).
    WithLogTee(tee).WithInstance(*path, startTime).Handler(), ... }
```

stampWriter 包在 MultiWriter 外层：时间戳只盖一次，stderr 与 tee 收到的是同一份
带时间戳的行，保证 `/log` 输出与终端逐字节一致。热重载不动 logger 实例，tee 天然跨
reload 存活，无需任何处理。

### 3.3 `GET /log`（新文件 `internal/server/admin_log.go`）

```
mux.HandleFunc("GET /log", s.auth(s.adminLog))     // Handler() 中注册，紧挨 /status
mux.HandleFunc("GET /log.html", s.logPage)
```

handler 行为：

1. 响应头：`Content-Type: text/plain; charset=utf-8`、`Cache-Control: no-store`；
   先 `WriteHeader(200)` 再开始推流。
2. `Recent(全部)` 逐行写出并 Flush。
3. `Subscribe()` 后循环 select：
   - 日志行 → 写出 + Flush；
   - 心跳 tick（30s 无行时）→ 写 `\n` + Flush；
   - `r.Context().Done()` → 取消订阅，返回。

放 admin.go 同级的理由不变：它和 `/status` 一样回答"这个进程发生了什么"，
属于 admin 面，不属于客户端 ingress 面。

### 3.4 `log.html`（新文件 `internal/server/log.html` + `log_page.go`）

结构与 `status_page.go`/`status.html` 完全同构（`//go:embed` + 两个 header）：

- 页面主体是一个占满视口的 `<pre>`，深色底等宽字体，底部一行状态栏
  （连接状态 · 已接收行数 · Key 按钮）。
- JS：`fetch('/log', {headers})` → 401 则复用 status.html 的输入框交互取 key →
  `response.body.getReader()` 循环 read、按 `\n` 切行 append 到 `<pre>`，自动滚底
  （用户向上滚动时暂停自动滚动，出现"回到底部"提示）。
- 断线（网络错误/服务重启）显示重试按钮，手动点击重新走一遍连接流程。
  不做自动重连——手动刷新语义更清楚，也避免重启风暴下的重连洪水。

### 3.5 安全清单

- `/log` 必须鉴权（s.auth）；`/log.html` 壳内零业务数据。
- 不设 CORS 头（维持现状即安全）。
- ring buffer 只存内存，进程退出即消失，无落盘、无新的 0600 文件。
- 日志行内容本身已是现有 stderr 输出的超集之真子集（逐字节相同），不引入新的
   泄露面。

## 4. Action Plan

按依赖顺序分五步，每步独立可编译、可测试。全程遵守：函数/文件行数注意 archtest
预算；并发路径的测试跑 `-race`。

### Step 1 — `internal/logtee` 包 ✅

改动：
- 新建 `internal/logtee/logtee.go`（约 100 行）+ `doc.go` 或包注释。
- 新建 `internal/logtee/logtee_test.go`。

要点：
- `New` 校验 capLines > 0（<=0 panic，编程错误）。
- `Write` 对超长行不截断（保持逐字节一致承诺）；buffer 存 string。
- 订阅者注册表用 mutex 保护（本项目惯例：COW 写路径必须持锁）。

测试：
- 基本：写入 N 行 → `Recent` 顺序正确、环形覆盖正确（写 cap+10 行只剩最后 cap 行）。
- 广播：两个订阅者各收到全部行；一个取消后不再收到。
- 慢消费者：订阅者不读、写满 64+N 行 → 主 `Write` 不阻塞（计时断言）、恢复读取后
  收到 dropped 标记行。
- `Write` 返回值始终等于 len(p)、error 恒 nil。
- `-race ./internal/logtee/...`。

验收：`go test -race ./internal/logtee/...` 全绿；`go build ./...` 通过。

### Step 2 — `GET /log` handler ✅

改动：
- 新建 `internal/server/admin_log.go`（handler + `WithLogTee` setter + logPage embed，
  预算内单文件；若超预算把 `logPage` 拆 `log_page.go` 对齐 status_page.go 结构）。
- `internal/server/server.go`：`Handler()` 注册两条路由；`Server` 增加 `logTee *logtee.Tee`
  字段（nil 合法——未接线时 `/log` 返回 503 + 一行说明，测试环境大量存在无 tee 的 Server）。

测试（`internal/server/admin_log_test.go`）：
- 无 tee → 503。
- 有 tee：先写几行历史 → 请求 → 断言响应体以历史行开头；再往 tee 写一行 →
  在超时窗口内读到该行；cancel context → 订阅被清理（tee 侧断言订阅数为 0）。
- 未带 key 且配置了 api_keys → 401；带正确 Bearer → 200。
- 响应头断言（Content-Type / Cache-Control）。
- 心跳：`SetHeartbeat(50ms)` 注入 → 空闲期收到空行。
- `-race`。

验收：`go test -race ./internal/server/ -run TestAdminLog` 全绿。

### Step 3 — `cmd/vmr` 接线 ✅

改动：
- `cmd/vmr/cmd_start.go`：创建 tee、logger 改包 MultiWriter（见 3.2）、
  `WithLogTee` 接入 Server。

测试：
- `cmd/vmr/cmd_start_test.go` 补一例：起 Server + tee → logger.Printf →
  GET /log 读到该行（端到端最小闭环）。

验收：`go test -race ./cmd/vmr/...`；手工冒烟：`go build -o vmr ./cmd/vmr &&
./vmr start -c <test config>`，curl 带 key 请求 `/log`，另开终端打一条真实请求，
观察两处输出一致；`./vmr.sh restart` 后浏览器打开 `/log.html` 走完 401→输 key→
跟随 的完整流程。

### Step 4 — `log.html` 页面 ✅

改动：
- 新建 `internal/server/log.html`（自包含，无外链资源，风格对齐 status.html）。
- Step 2 已埋好路由与 embed。

测试：
- 手工为主（浏览器体验）：无 key 配置时直接出流；有 key 配置时 401 → 输入框 →
  出流；key 错误 → 提示重输；向上滚动暂停自动滚底；服务重启后显示重试按钮。
- 自动化兜底一条：`log_page_test.go` 断言 embed 非空且 Content-Type 正确
  （对齐 status_page_test.go）。

验收：两种鉴权配置下人工走查通过。

### Step 5 — 收尾 ✅

依次执行并核对：

1. `gofmt -l .` 干净；`go vet ./...` 干净。
2. `go test ./internal/archtest/...`（新增了包边界，必跑）。
3. `go test -race ./...` 全量。
4. 文档同步（双语同改，见项目约定）：
   - `docs/UserGuide.md` / `UserGuide.zh.md`：新增"/log 与 log.html"小节
     （端点、鉴权行为、512 行回放上限、30s 心跳、无参数）。
   - `README.md` / `README.zh.md`（若有端点列表则同步，否则仅在特性段提一句）。
   - `CHANGELOG.md` `[Unreleased]` → `Added`：条目一句话。
5. 本文档勾销 Action Plan 各步，状态改为"已实施"。

### 明确不做（本版范围外）

- `/log.jsonl` 结构化输出（扩展点，见 D2）。
- 自动重连、行着色、级别过滤、搜索。
- ring buffer 容量配置化（D7）。
- banner / panic 捕获（盲区已接受，见 2.1）。
