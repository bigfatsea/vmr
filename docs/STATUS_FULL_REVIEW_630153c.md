<!-- Ver 2026-08-23, review report. Language: zh (deliverable for the requester). -->

# Commit #630153c 全面 Review 报告 —— `/status` 端点迁移 + Status Report 重构 + HTML Dashboard

> Review 范围：`docs/STATUS_ENDPOINT_MIGRATION_gemini.md`、`docs/STATUS_REPORT_DESIGN_gemini.md`、
> `docs/STATUS_HTML_DASHBOARD_DESIGN_gemini.md` 三份设计文档的方案本身，以及 commit `630153c`
> （51 文件，+2821/−440）的全部相关实现。按任务要求，未阅读 `docs/STATUS_ROUND_REVIEW_gemini.md`，
> 结论不受其影响。

---

## 1. Debrief —— 任务理解（顶层指导）

**任务目标**：对三份设计文档的方案 + 全部落地代码做一次不受既有结论束缚的全面 review。
不是"核对文档与代码是否一致"的验收，而是三层：

1. **方案层**：三份文档的所有决策都要回到第一性原理重新审视——是否足够简单、是否引入不必要
   复杂度、是否有更优解、是否有疏漏。文档里已经写死的"定论"（不保留别名、不设 admin_keys、
   固定原子计数器、无状态目录扫描、localStorage 凭证、版本必须匹配等）全部可以被挑战，前提是
   有理有据。
2. **落地层**：逐一核实每个功能点是否真的实现了、实现与文档声明是否一致、文档中的 claim 是否
   有事实依据（例如"向后兼容"、"零锁竞争"、"38 包全绿"）。
3. **汇总结**：把所有发现按用户价值 / 严重程度 / ROI / 风险四个维度评分，分梯队给出处置建议。

**任务约束（自我设定的边界）**：
- 偏好简单有效；不为边际收益引入高复杂度。因此对"数据扩充"类问题（如系统级 CPU、文件描述符、
  per-endpoint token 矩阵等被设计文档裁掉的项）不再翻案——它们被裁掉是合理的。
- 发现的任何问题都以源码证据为准，不凭文档文字臆断。
- 不读 STATUS_ROUND_REVIEW_gemini.md，保持独立判断。

**Review 基调**：这组改动整体工程质量高、文档与代码吻合度好（三份文档本身就是实施报告）。
我的独立判断结论先行：**没有高危问题，没有凭证泄漏/数据丢失/阻断性缺陷；核心问题集中在
"两个自产数字口径冲突"、"JS 侧零守卫"和"若干文档声明与实现立场矛盾"三类**，全部成本可控。

---

## 2. Action Plan —— 执行计划

```
[Phase 0] 文档研读
  └── 三份设计文档通读，提取：核心决策表 / JSON Schema 声明 / 验收清单 / 实施自述
[Phase 1] Commit 全景
  └── git show --stat 630153c：51 文件、+2821/−440 的结构盘点；发现 status.html 实为
      前一个 commit (bbdbd47) 创建、本 commit 集成并修复
[Phase 2] 服务端核对（/status 数据源）
  ├── server.go：路由 + s.auth 接入（对照迁移文档 §4.1）
  ├── admin.go：instance/system/traffic/audit/image_cache 各 block（对照报告设计 §6 Schema）
  ├── router/telemetry.go + 埋点位置（router.go/limiter.go）——验证"零锁原子"claim
  ├── health.go Status / limiter / reload / quota —— 数据来源逐一核对
[Phase 3] CLI 核对
  ├── cmd_status.go / cmd_status_render.go：-key、自动注入、-brief 兼容、渲染覆盖
  └── main_test.go 新增测试
[Phase 4] 看板核对
  ├── status.html 全量阅读：鉴权流、渲染字段、esc() 转义、轮询、倒计时
  ├── 对照设计文档 §3 布局与 §5 伪代码逐项核对
  └── 特别注意 bbdbd47 → 630153c 之间 75 行 diff（修复了 XSS 与 quota pct 双乘）
[Phase 5] 验证执行
  ├── go build ./... / go vet / archtest
  ├── go test 目标包（发现 1 次 flaky：TestActiveProbe_UpstreamFailureGoesToReportFailure）
  ├── 父 commit worktree 复现：确认 flaky 非本 commit 引入
  ├── GOOS=windows / GOOS=linux 交叉编译（验证"跨平台编译全通"claim）
[Phase 6] 设计挑战与 claim 核实（第一性原理）
  ├── §7.3"向后兼容"声明 vs 实际 json.Unmarshal 行为（instance.config string→object 双向硬失败）
  ├── Serving *bool 兜底在本 commit 后是否可达（git log -S 溯源）
  ├── by_status 与 audit.outcome 在截断场景的口径对比（router.go vs audit.go OutcomeFor）
  ├── runtime.ReadMemStats 每请求 STW 与看板 5s 轮询的张力
  ├── 看板 JS 字段引用的测试守卫缺失
  └── 其余文档 claim 逐条对照实现（零锁/无状态扫描/38 包全绿/跨平台）
[Phase 7] 汇总评分与梯队
```

实际执行与计划一致；Phase 6 中新增了"截断口径对比"与"Serving 溯源"两个计划外的核实点，
因为 Phase 2/3 阅读代码时发现它们与文档 claim 有潜在冲突——属于必要补充，未偏离主线。

---

## 3. 执行过程记录

### 3.1 文档研读要点

**STATUS_ENDPOINT_MIGRATION**：决策核心 = URL 规范化（`/admin/status`→`/status`）、网络范围
（listen 绑定）与身份认证（api_keys）解耦、不保留兼容别名、仅 Header 鉴权、CLI 自动注入 key。

**STATUS_REPORT_DESIGN**：JSON 重构决策 = instance 内聚（config/concurrency 归位）、system 纯
stdlib（ReadMemStats/Statfs）、traffic 固定维度原子计数器（拒动态 Map）、磁盘占用无状态现场
读取、双格式（数值 + 人类可读）、三个梯队裁剪 + 禁入清单。

**STATUS_HTML_DASHBOARD_DESIGN**：CSR 纯客户端渲染、零外部依赖单文件、localStorage 凭证 +
弹窗鉴权、可调轮询（5s/15s/30s/60s）、暗黑主题卡片布局。

### 3.2 Commit 全景

- 51 文件、+2821/−440。核心新增：`internal/router/telemetry.go`(+123)、`internal/server/admin.go`
  (重写至 307 行)、`cmd/vmr/cmd_status_render.go`(+160)、`internal/server/status_page.go`(+21)、
  `internal/server/disk_{unix,windows}.go`、`internal/server/status.html`。
- **发现**：`status.html`（904 行）实际由前一 commit `bbdbd47` 创建，`630153c` 集成并做了 75 行
  修改。这 75 行 diff 含两处真实修复：**① 给所有 innerHTML 内插补上 `esc()` HTML 转义**
  （bbdbd47 原版把 config 字符串直接拼进 DOM，存在配置内容 XSS 面）；**② 修复 quota pct 双乘**
  （原版 `Math.round(q.pct * 100)`，而服务端已发百分比）。这两处修正说明集成过程本身做了一轮
  质量把关，予以记录。

### 3.3 服务端核对结论（均落地）

| 设计声明 | 实现 | 结论 |
| :--- | :--- | :--- |
| `GET /status` 套 `s.auth`，无 key 恒放行 | server.go: `mux.HandleFunc("GET /status", s.auth(s.adminStatus))`；`authenticate` 空 key 列表返回 true | ✅ |
| 旧路径 404、不保留别名 | Go 1.22 mux 精确匹配，`/admin/status` 无路由 → 404（有测试断言） | ✅ |
| instance 内聚：config(path/mtime/stale/reload/issues) + concurrency | admin.go `instanceBlock` 完整实现 | ✅ |
| system：goroutines + heap_alloc/sys 双格式 + statfs 磁盘余量 | `systemBlock` 完整实现；`disk_unix.go`/`disk_windows.go`（Windows 桩） | ✅ |
| traffic：固定原子计数，total/by_protocol/by_status + token 5 分量 + sticky | telemetry.go 11 个 `atomic.Uint64`，`Snapshot()` 组装 map；埋点位于 chatHandler/Router.Serve/tryOne/forwardSuccess | ✅ |
| audit/image_cache：无状态现场 `os.ReadDir` 求和 | `dirTotalSize`；`active_file_size` 用当日文件名重建（= KNOWN_ISSUES 1.47 已登记） | ✅ |
| current_time（RFC3339 带时区） | `"current_time": now`（time.Time 编码） | ✅ |
| 无 quota 时整块省略 | `if len(qs) > 0` 条件挂载 | ✅ |
| 双格式（`_bytes` + 人类可读） | FmtBytes（新增 GB/TB 档）+ FmtDuration 全量使用 | ✅ |

**埋点一次一终态验证**：逐条跟踪了 RecordRequest/RecordOutcome 的所有调用路径，确认每个 ingress
请求恰好记录一次 outcome（401、body 读失败、probe 失败、缺 model、503 未初始化、404 错协议、
全部失败、成功、客户端取消、槽位等待取消）。`total = ok + canceled + error` 可成立。
`TestTelemetry_ServeOutcomeCoverage` 对终态路径有专门覆盖。

### 3.4 CLI 核对结论

- `-key` 参数、config 自动注入 `api_keys[0]`、`-addr` 分支静默回退读本地 config —— 均实现，与
  迁移文档 §4.2 一致；`TestCmdStatus_WithAPIKeys` 覆盖。
- `-brief` 输出字段序不变（pid/listen/uptime/models/version/stale/config），`vmr.sh ps` 兼容性保持。
- **渲染覆盖**：printStatus 渲染 instance 行 / stale / issues / reload / concurrency / sticky /
  quota / models；**不渲染** system / traffic.requests / traffic.tokens / audit / image_cache /
  current_time（结构体解码了但未使用）——与报告设计文档 Phase 4"后续迭代按需引入"的声明一致，
  不算缺口，但见 P7。

### 3.5 看板核对结论

- 鉴权流转（无 key 裸调 → 200/401 → 弹窗 → localStorage → 脱敏尾缀展示）与设计文档 §2.3 流程图一致。
- `esc()` 覆盖全部 innerHTML 内插点（banner/model/quota/error 文本），XSS 面闭合。
- 轮询、倒计时、uptime 每秒 tick、cooldown 秒级递减均实现。
- 服务端 `statusPage` 免鉴权直出外壳（不含业务数据），`/status` 数据请求走 `s.auth` —— 与设计一致。
- 见 P2/P5/P6/P8/P9/P14 的改进点。

### 3.6 验证执行记录

| 验证项 | 结果 |
| :--- | :--- |
| `go build ./...` | ✅ |
| `go vet`（server/router/cmd） | ✅ |
| `go test ./internal/archtest/...` | ✅（行数/导入边界/文档引用全过） |
| `go test` 目标包 | ⚠️ `TestActiveProbe_UpstreamFailureGoesToReportFailure` 全仓并行跑时失败 1 次（`fails=1, want >=2`）；单独跑、整包连跑 3 次均通过 → **时序 flaky** |
| 父 commit (bbdbd47) worktree 复现 | 同一测试通过 → **flaky 非本 commit 引入**（本 commit 只改了其中的 URL 路径） |
| GOOS=windows / GOOS=linux 交叉编译 | ✅（"跨平台编译全通"claim 属实） |
| "38 包全绿" claim | 基本属实（上述 flaky 除外，属既有测试问题） |

### 3.7 第一性原理挑战与 claim 核实（核心发现）

按计划完成了对文档决策的重新审视。绝大多数决策经核实成立（见 §4.2 维持清单），以下为发现
的实质问题，按重要程度排列，证据均已在源码中确认：

**(a) `by_status` 与 audit `outcome` 在流式中途截断时口径冲突** —— router.go `forwardSuccess`：
`RecordOutcome(copyErr == nil && status != "CANCELED", ...)`，TRUNCATED 计为 error；而 audit 侧
`OutcomeFor(rw.status=200, canceled=false)` 返回 "ok"。同一请求：`/status` 显示 error，审计日志
记 ok。这是**两个自产数字互相矛盾**，项目纪律（"analytics 数字与 routing 数字必须可对齐、
差分测试钉住"）未覆盖新引入的 telemetry。

**(b) STATUS_REPORT_DESIGN §7.3 "向后兼容"声明与实现立场直接矛盾** —— 文档声称 "json.Unmarshal
默认忽略未定义的键…结构体的演进不会影响老版本客户端"。实际本次 `instance.config` 由 string
变为 object：老 CLI 解新响应、新 CLI 解老响应都会在类型不匹配处**硬失败**（实现以 "server and
client vmr versions differ" 报错——这是 CHANGELOG 与 KNOWN_ISSUES §2.2 的显式立场"版本必须匹配"）。
文档该节文字需要更正。

**(c) `Serving *bool` 兜底在本 commit 后实际不可达** —— `Serving` 字段在 f7daef8 加入（早于本
commit）；任何 pre-630153c server 的 `instance.config` 都是 string，新 CLI 在解析到
`instance.config` 时即硬失败，永远走不到 serving 渲染分支。KNOWN_ISSUES §2.2 声称保留它，
但按项目自己的 KISS 逻辑，这是 10 行死路径——要么删，要么明确其"通用字段新增模式"价值。

**(d) 看板 JS 对 /status JSON 形状零守卫** —— status.html 中所有字段引用（`data.xxx`/`q.xxx`/
`ep.xxx`）是手写镜像，无任何测试/静态检查；Go 侧形状变更会被编译期 + 测试捕获，JS 侧静默
失效（显示 0/-）。项目有 archtest 文化，这是同类问题在 JS 侧的真空地带。

**(e) `/status` 每次请求 `runtime.ReadMemStats`（stop-the-world）** —— admin.go:174。设计文档
以"15 分钟一次低频查询"为开销基准，但看板提供 5s 轮询，多标签页/多监控端会放大 STW 频率。
单机低频场景可忽略，但值得记录。

**(f) 其余 claim 全部核实为真**：原子计数零锁（代码形态确认）、无状态目录扫描（`dirTotalSize`
实现确认）、双格式（确认）、Windows 桩已登记（KNOWN_ISSUES §2.3 确认）、看板零外部依赖
（HTML 全量阅读确认）、跨平台编译（实测确认）。

---

## 4. 问题清单与梯队

### 4.1 评分维度说明

每项按四个维度 1–5 打分：**价值**（修复后对用户/项目的收益）、**严重度**（不修的当下危害）、
**ROI**（收益/成本比）、**风险**（问题若放任不管的潜在风险）。

### 4.2 维持的文档决策（核对结论，不翻案）

| 决策 | 理由（第一性原理复核） |
| :--- | :--- |
| 移除 loopback 限制、网络范围与认证解耦 | LAN 监控是真实需求；默认 listen 仍是 `127.0.0.1:8800`（config.go:388），非默认暴露有 `config.Check()` warning 兜底 |
| 不保留 `/admin/status` 别名 | 单二进制、作者自管升级，别名是纯包袱 |
| 仅 Header 鉴权、不做 URL query key | 防泄漏正确；复用 `s.auth` 零重复代码 |
| 用聊天 `api_keys` 兼作状态鉴权、不设 admin_keys | 当前单人/小团队规模正确简化（KNOWN_ISSUES §2.3 已论证） |
| traffic 固定原子计数器、拒动态 Map | 零锁、零热重载漂移，正确工程决策 |
| audit/image_cache 无状态现场扫描 | 避开写路径累加器漂移/死锁复杂度，正确 |
| system 纯标准库、Windows 磁盘桩 | 目标平台 macOS/Linux，平台胶水违反 YAGNI |
| 看板 CSR + 单文件内联 + localStorage 凭证 | 复杂度隔离在一个静态文件里，不进 Go 热路径；esc() 已闭合注入面 |
| 版本必须匹配、形状变更直接报错 | 暴露未走完的升级流程，优于降级渲染（但文档表述需更正，见 P3） |

### 4.3 问题清单

| # | 问题 | 价值 | 严重度 | ROI | 风险 |
| :--- | :--- | :---: | :---: | :---: | :---: |
| P1 | `by_status.error` 与 audit `outcome` 在流式截断时口径冲突（同一请求一个记 error 一个记 ok） | 3 | 2 | 4 | 2 |
| P2 | 看板 JS 与 /status JSON 形状零守卫，字段变更静默破坏看板 | 4 | 3 | 4 | 3 |
| P3 | STATUS_REPORT_DESIGN §7.3 "向后兼容"声明与实现"版本必须匹配"立场矛盾 | 3 | 2 | 3 | 1 |
| P4 | `Serving *bool` 兜底在本 commit 后不可达（10 行死路径） | 2 | 1 | 3 | 1 |
| P5 | 看板 UI 未展示 responses 协议、cache_write、reasoning（JSON 与设计稿都有） | 2 | 1 | 3 | 1 |
| P6 | `/status` 无 `Cache-Control: no-store`（health 有，看板轮询依赖不被缓存） | 2 | 1 | 4 | 1 |
| P7 | CLI 未渲染新增环境字段（cwd/executable/go_version/os_arch），排障仍要 curl | 2 | 1 | 3 | 1 |
| P8 | 每请求 `runtime.ReadMemStats`（STW）与看板 5s 轮询的张力 | 2 | 1 | 2 | 1 |
| P9 | 看板失效 key 不自动清除，需手动打开 Key 管理 | 1 | 1 | 2 | 1 |
| P10 | uptime 双格式不一致（JSON "3h 34m 5s" vs CLI -brief "3h34m5s"） | 1 | 1 | 1 | 1 |
| P11 | telemetry 协议名硬编码，新增协议会静默漏计 by_protocol（total 仍计） | 2 | 1 | 2 | 1 |
| P12 | `TestActiveProbe_UpstreamFailureGoesToReportFailure` 时序 flaky（**非本 commit 引入**，CI 偶发红） | 2 | 2 | 2 | 2 |
| P13 | 外部 JSON 消费者（curl/jq 脚本）的形状破坏只在 CHANGELOG 提示，迁移文档 §7 只提了路径变更 | 2 | 1 | 2 | 1 |
| P14 | 看板无 Content-Security-Policy（注入面已被 esc() 闭合，此为纵深防御） | 1 | 1 | 2 | 1 |

### 4.4 梯队划分与理由

**第一梯队 —— 建议马上处理（正确性语义 + 高 ROI 防护，成本都极低）**

- **P1（by_status 与 audit 截断口径）**。全项目最重的纪律是"自产数字必须可解释、可对齐"，
  本次引入的 telemetry 是唯一与既有账本（audit outcome）共享词汇（ok/error/canceled）却口径
  不同的新数字，且没有任何差分测试。当前危害小（截断场景低频），但放任不管的后果是：未来
  任何人对账时发现 `/status` 与 `vmr report` 错误数不一致，需要重新考古。修复成本 ≈ 0：
  KNOWN_ISSUES 补一条语义说明 + 在 telemetry.go 的 RecordOutcome 处加一句注释，指明
  "error 含截断、审计顶层 outcome 对 200 记 ok（截断见 attempt.ErrorClass）"。
- **P2（看板零守卫）**。这是三类问题里唯一"现在没坏、但坏了无人知"的高风险项：Go 侧任何
  字段改名都有编译期/测试拦截，JS 侧完全没有。一次字段重构事故（比如把 `by_protocol` 改成
  `byProtocol`）会让看板静默显示 0/–，而看板是本次交付的核心价值之一。低成本方案：一个 Go
  测试解析 status.html，提取 JS 引用的字段名，与服务器真实 /status 响应的字段集做差集断言
  （fixture 可与现有 instance_test.go 共用）。**在 archtest 文化下这是顺理成章的补丁。**

**第二梯队 —— 建议尽快处理（文档准确性 + 一致性，非当务之急）**

- **P3（文档 §7.3 兼容声明矛盾）**。文档是"当前状态"不是"changelog"，一句错误的兼容性声明
  会在下次讨论版本策略时误导决策。改几行字即可；同时把设计样例与实现的两处字段差异
  （quota.detail 嵌套 vs 实际平铺 fresh/cache_read/...、by_protocol 键 "responses" vs 实际
  "openai-responses"）一并订正，注明"样例为示意，以实现为准"。
- **P4（Serving *bool 死路径）**。与 KNOWN_ISSUES §2.2 的判断相反：既然立场是"版本必须匹配、
  形状变更硬报错"，那么在形状变更之后，serving 的指针兜底已经不可达。要么删掉换回裸 bool
  （约 10 行 + 注释，简化），要么在 KNOWN_ISSUES 里明确"这是为未来字段新增保留的通用模式"
  而不是为旧 server 兜底。两者取一，当前的中间态（声称不兼容、代码留兼容）与 KISS 不符。
- **P5（看板展示缺口）**。responses 协议数、cache_write、reasoning 在 JSON 里都有，UI 丢弃。
  几行 JS 补齐即与设计稿（"Reas: 845K"）和 JSON 对齐。
- **P6（Cache-Control）**。一行 header，与 health 端点的既有论证（"well-behaved
  intermediaries"）保持一致；看板轮询语义上本就要求 /status 不被缓存。
- **P12（flaky 测试）**。非本 commit 引入，但 CI `-race ./...` 会真实偶发红。单独立项修
  active_probe 测试的等待逻辑（把固定 sleep 换成对条件的轮询等待），与本 commit 解耦。

**第三梯队 —— 可改可不改（ROI 低或观察项）**

- **P7**：CLI 渲染 cwd/exe——设计文档已声明"后续迭代"，属锦上添花。
- **P8**：ReadMemStats STW——单机低频场景可忽略；如未来看板高频使用成真，再换
  `runtime/metrics` 或加 1s 缓存，现在不值得动。
- **P9 / P10 / P14**：看板 UX 细节、uptime 格式观感、CSP 纵深防御——各自成本极低但收益也极低。
- **P11**：telemetry 协议硬编码——加一种协议时顺手补即可，或加一条小测试提醒。
- **P13**：外部消费者文档提示——在迁移文档"遗留问题"补一句形状破坏即可。

---

## 5. 结论

本次三件套（端点迁移 / 报告重构 / 看板）是一个**决策质量高、落地完整、测试纪律好**的改动：

- 三份文档的绝大多数决策经第一性原理复核成立，被裁掉的候选（CPU 采样、fd 计数、per-endpoint
  token 矩阵、动态 QPS 窗口）拒绝得有理有据；
- 全部功能点落地，文档与代码吻合度高，跨平台编译、archtest、测试全绿（一处与本次改动无关的
  flaky 除外）；
- 集成过程本身还修复了初版看板的 XSS 面与 quota 百分比双乘 bug。

需要动手的实质问题只有两个且都很轻：**P1 把 by_status 的截断口径与审计对齐并登记**、
**P2 给看板的 JS 字段引用补一道廉价守卫**。其余是文档订正（P3）与一致性小修（P4–P6）。
按项目"简单优先"的哲学，不建议为 P8/P9/P10/P14 这类边际项投入。

---

## 6. 处置结果（2026-08-23 评审后落地）

> 评审定调的两条顶层原则，后续处理均以此为前提：
>
> **原则一（版本一致性）**：CLI、Server 与 Status Report 视为同一二进制的三面，必须版本一致；
> 版本不一致造成的任何问题直接报错，不做兼容性处理。VMR 是个人/小团队工具、高速迭代期、
> 用户量极少，历史兼容性是给自己没事找事。后续所有代码、清理、文档更新均基于此原则。
> 已作为唯一权威表述落在 `docs/KNOWN_ISSUES_sonnet-5.md` §2.2（原条目升级为"任何不一致直接报错，
> 无字段级例外"）。
>
> **原则二（不做 JS 守卫）**：看板对 `/status` JSON 形状不做防守。解析只有 `status.html` 一个文件，
> Go 侧形状变更后直接改这个文件的 JS 即可；加守卫无非是把"改 HTML"变成"改 HTML + 改守卫"，
> 纯增负担。以 Go 版 status report 为基准，JSON 变了就改 HTML。P2 据此不处理（原文建议撤回）。

### 6.1 已完成项与实际落地方式

| # | 处置 | 实际落地 |
| :--- | :--- | :--- |
| P1 | ✅ 按 review 建议方案 | ① `internal/router/telemetry.go` 的 `RecordOutcome` 补 doc comment：说明截断计入 `error` 而审计顶层记 `ok` 是回答不同问题的两本账，禁止未重新决策就对齐；② `router.go` 的 `forwardSuccess` 调用点加一行指向注释；③ `docs/KNOWN_ISSUES_sonnet-5.md` §2.2 新增登记条目（含"对账发现不一致先查这里"的提示）；④ `STATUS_REPORT_DESIGN_gemini.md` 的 by_status 字段行同步标注口径差异 |
| P2 | ❌ 不做（用户决策） | 见上方原则二。无任何守卫代码落地 |
| P3 | ✅ 完成 | `STATUS_REPORT_DESIGN_gemini.md` §7.3 整节重写：删除与实现立场矛盾的"向后兼容"声明，改为版本一致性策略表述并指向 KNOWN_ISSUES §2.2；样例中 `by_protocol` 键名订正为 `openai-responses`；quota 样例块加注"样例为示意、字段以实现为准" |
| P4 | ✅ 清理死代码 | `cmd/vmr/cmd_status.go` 的 `Serving *bool` 改回 `Serving bool`（长注释压缩为三行，指向版本一致原则）；`cmd_status_render.go` 删除 nil 兜底启发式（`Fails > 0`），直接使用 `ep.Serving`；删除测试 `TestCmdStatus_OlderServerMissingServingField_HealthyEndpointStillOK`；KNOWN_ISSUES §2.2 同步改写——版本必须匹配的原则不再留任何字段级例外，并记录该兜底不可达的原因（旧 server 响应在 `instance.config` 类型不匹配处即硬失败） |
| P5 | ✅ 已拍板采纳方案 1 并落地（分析见 §6.2） | `status.html`：Traffic 卡协议行改为 OpenAI / Anthropic / Responses 三元显示；Token 卡新增 Cache Write / Reasoning 一行。共约 10 行 HTML+JS，零状态、零逻辑新增 |
| P6 | ✅ 完成 | `internal/server/admin.go` 的 `adminStatus` 入口加 `Cache-Control: no-store`（注释与 `/health` 同理），看板轮询语义不再依赖中间件行为 |
| P12 | ✅ 完成（改动很小） | 根因：探针 handler 内递增 hits 计数器，而 `ReportFailure` 在响应写出后才执行——测试在 `hits==2` 后单次读 `/status`，恰好赛过这个窗口就会看到陈旧的 `fails=1`。修复：改为轮询 `/status` 直到期望健康态出现（复用同一 deadline，超时后仍走原有断言给出末次观测状态）。`-race -count=3` 通过；CHANGELOG Fixed 有记录 |
| 刷新频率 | ✅ 完成 | `status.html`：自动刷新固定 5 分钟（`REFRESH_SECONDS = 300`），删除频率下拉框（5s/15s/30s/60s/Off）、倒计时指示与手动刷新按钮及关联 CSS（`.spin` 等）；需要立即更新时用户刷新页面即可，页面不为手工刷新提供额外功能。保留的唯一立即拉取路径是鉴权弹窗保存后的首次 fetch——那是登录流程的一部分，不是手动刷新控件。`STATUS_HTML_DASHBOARD_DESIGN_gemini.md` 的轮询相关声明（§决策、ASCII 布局图、头部组件清单、验收用例 5、结论段）已同步改为固定 5 分钟表述 |
| P13 | ✅ 补一句文档说明 | `STATUS_ENDPOINT_MIGRATION_gemini.md` §7.3 遗留问题第 1 条补第三小点：payload 形状同批重构，外部 jq/curl 脚本需按新形状改写，仅提示不设兼容层 |

### 6.2 P5 决策分析（已拍板：方案 1，2026-08-23）

**问题本质**：JSON 里已有、设计稿里也画了的三个数字（responses 协议请求数、cache_write、reasoning），
UI 渲染时丢弃了。这不是缺陷，是渲染覆盖面落后于数据面一步。

**备选方案对比**：

1. **补齐 UI 显示（已采用）**——纯展示层追加：HTML 加一个 stat-item、JS 加两行 `textContent`
   拼接。无新状态、无新逻辑分支、无新依赖；且看板是本次交付的核心价值之一，数据面与展示面
   对齐是它的本职。成本 ≈ 10 分钟，复杂度增量 ≈ 0。
2. **维持现状**——省 10 行，但留下"JSON 说有、UI 说没有"的永久小裂缝；下次有人对照设计稿
   验收还会再提一次（本次 review 就是证明）。
3. **从 JSON 里裁掉这三个字段**——方向反了：token 5 分量是配额感知路由的核心观测面，
   responses 协议计数是三协议入口对称性的自然结果；为了少渲染两行而裁数据面得不偿失。

**结论**：方案 1 代价落在“极小”档，按既定规则（代价小→完善）直接执行；已由用户拍板确认采纳方案 1，落地为最终状态。

### 6.3 明确不处理项（登记即闭环）

| # | 处置 | 说明 |
| :--- | :--- | :--- |
| P7 | ❌ 不做 | CLI 渲染 cwd/executable 等——设计文档已声明"后续迭代按需引入"，维持 |
| P8 | ❌ 不做 | ReadMemStats STW——固定 5 分钟刷新后，轮询频率上限比原 15s 设计还低一个量级，张力进一步消解；若未来出现高频监控端需求再换 `runtime/metrics` |
| P9 | ❌ 不做 | 失效 key 不自动清除——UX 边际项 |
| P10 | ❌ 不做 | uptime 双格式差异（JSON "3h 34m 5s" vs CLI "3h34m5s"）——各自场景内自洽，观感问题 |
| P11 | ⏸ 观察项 | telemetry 协议名硬编码——新增第四种 ingress 协议时顺手补一行 switch case 即可（漏计只影响 by_protocol 展示行，total 仍准），不单独立规则 |
| P14 | ❌ 不做 | CSP——注入面已被 esc() 闭合，纵深防御的边际收益不值得给单文件看板加一层配置心智 |

### 6.4 落地验证

- `go build ./...`、`go vet`（server/router/cmd）、`gofmt -l`：全绿；
- `go test -race ./internal/server/ ./internal/router/ ./cmd/vmr/`：全绿（含修复后的探针测试连跑）；
- `go test ./internal/archtest/`：通过（router 与 cmd/vmr 均有改动，按纪律重跑）；
- CHANGELOG `[Unreleased]` 已补 Changed×2（看板刷新策略 + /status Cache-Control）、Removed×1
  （serving 兜底）、Fixed×1（flaky 测试），并把截断口径备注并入既有 Breaking 条目。
