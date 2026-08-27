# /status.html 虚拟模型拓扑视图改版评审文档

## 1. 需求背景与目标

本次改动的目标是对 `/status.html` 的 **Virtual Models & Endpoint Topology（虚拟模型与端点拓扑）** 区域进行视觉与信息结构层面的优化，降低冗余信息，提高可读性与紧凑度。

### 原始视图呈现
```text
Virtual Models & Endpoint Topology
3 models
agent [openai-completions]
audio, image, text, thinking, tools, video
ctx: 512k tok
8 endpoints
PRI    ENDPOINT                                                  PROTOCOL              STATUS    FAILS    CTX / CAPS                                      LAST ERROR
P0     openai-completions/bai/deepseek-v4-flash                  openai-completions    OK        0        512k · text,tools,thinking                      -
P0     openai-completions/bai/deepseek-v4-flash-vision-exp       openai-completions    OK        0        512k · text,tools,thinking                      -
P1     openai-completions/cliproxy/gemini-3.7-flash-high         openai-completions    OK        0        512k · text,tools,thinking,image,audio,video    -
...
```

### 目标视图呈现
```text
Virtual Models & Endpoint Topology
3 models
agent (openai-completions)                                      audio, image, text, thinking, tools, video · ctx: 512k tok · 8 endpoints
PRI    ENDPOINT                                                  STATUS    FAILS    CTX / CAPS                                      LAST ERROR
P0     bai/deepseek-v4-flash                                     OK        0        512k · text,tools,thinking                      -
P0     bai/deepseek-v4-flash-vision-exp                          OK        0        512k · text,tools,thinking                      -
P1     cliproxy/gemini-3.7-flash-high                            OK        0        512k · text,tools,thinking,image,audio,video    -
...
```

---

## 2. 需求逐项分析与可行性评估

| # | 需求项 | 分析与实现方案 | 可行性与执行状态 |
|---|--------|----------------|------------------|
| 1 | 头部模型名 `agent [openai-completions]` 改为 `agent (openai-completions)`，其中 `(openai-completions)` 为 Tag | 将原本拼接的单个文本字符串拆开，模型名保留为 `.model-name`，协议使用 `span.badge` 标签包裹，且自动匹配对应的协议主题色（如 Anthropic 为紫色 `badge-anthropic`，OpenAI 为蓝色 `badge-primary`）。 | **已完成** ✅ |
| 2 | 端点名称去除协议前缀，如 `openai-completions/bai/deepseek-v4-flash` -> `bai/deepseek-v4-flash` | 代码中 `core.Endpoint.Name()` 在无自定义覆盖时默认生成格式为 `<protocol>/<provider>/<model>`。前端在渲染每一行端点时，安全检测若端点以 `ep.protocol + "/"` 开头，则截掉该协议前缀。若存在非标准自定义名称则保留原样。 | **已完成** ✅ |
| 3 | 移除表格中的 `PROTOCOL` 列 | 每个模型卡片（Model Card）在 `/status` 数据结构中本就是一个独立的 `(model, protocol)` 维度，卡片头部已经明确标注了该组端点的所属协议，表格内每一行重复展示 `PROTOCOL` 属于冗余信息。直接从 `<thead>` 与 `<tbody>` 中移除该列。 | **已完成** ✅ |
| 4 | 卡片头部右侧标签右对齐（`capabilities / ctx / endpoints`） | 重构 `.model-header` 的布局结构。左侧放入 `.model-header-left`（包含模型名与协议 Badge），右侧放入 `.model-tags`（包含能力集、上下文上限、端点数量）。通过 CSS `margin-left: auto` 与 flex 布局确保所有元数据标签紧贴右侧对齐。 | **已完成** ✅ |
| 5 | 将 `FAILS` 列改为 `Requests/FAILS`（若可能） | **深入代码排查**：目前实时 `/status` 接口中，`endpointStatus` 结构体仅包含端点健康状态（连续失败数 `consecutive_failures`、熔断/半开状态、最后错误等），**不存在单个端点的实时请求量计数**。<br>路由层的 `router.Telemetry` 严格按照设计原则，只维护全局和按协议/状态的原子计数器（零 map 分配、热路径无锁），并未在热路径维护每个 endpoint 的计数器；离线分析端的审计日志虽然有 endpoint 级请求数，但属于只读离线消费层，不在实时数据链路。<br>因此该项在纯前端层面无法直接获取数据。 | ~~**维持现状并待定** ⚠️~~ → 复议曾实现 → **三评从第一性原理否决，已回滚** ⛔（第 5 节为初评，第 8 节为复议过程，**最终结论见第 9 节**） |
| 6 | 以上规则应用到所有虚拟模型卡片 | `/status.html` 中所有模型卡片统一由 `renderModels()` 函数驱动渲染，修改后天然对所有虚拟模型和协议组生效。 | **已完成** ✅ |

---

## 3. 代码具体修改点

修改文件：`internal/server/status.html`

### 3.1 CSS 样式扩展
```css
/* 新增头部左右分栏与右对齐容器 */
.model-header-left { display: flex; align-items: center; gap: 8px; min-width: 0; }
.model-tags { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; margin-left: auto; }
```

### 3.2 渲染函数 `renderModels` 调整

#### (1) 模型头部与表头结构重构
```javascript
// 协议 tag 颜色匹配
const protoBadge = (PROTO_URLS[m.protocol] || {}).badge || 'badge-primary';

let html = `
  <div class="model-card">
    <div class="model-header">
      <div class="model-header-left">
        <span class="model-name">${esc(m.id)}</span>
        <span class="badge ${protoBadge}">(${esc(m.protocol)})</span>
      </div>
      <span class="model-tags">
        <span class="badge" title="capabilities (union across endpoints)">${esc(capsMeta)}</span>
        <span class="badge" title="largest context window across endpoints">ctx: ${esc(ctxMeta)}</span>
        <span class="badge">${endpoints.length} endpoints</span>
      </span>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width: 60px">PRI</th>
            <th>ENDPOINT</th>
            <th>STATUS</th>
            <th>FAILS</th>
            <th>CTX / CAPS</th>
            <th>LAST ERROR</th>
          </tr>
        </thead>
        <tbody>`;
```

#### (2) 端点名称前缀截取与行渲染
```javascript
// 去掉端点开头的 protocol/ 前缀
let epName = ep.endpoint;
if (ep.protocol && epName.startsWith(ep.protocol + '/')) {
  epName = epName.slice(ep.protocol.length + 1);
}

html += `
  <tr>
    <td><span class="badge">P${ep.priority}</span></td>
    <td style="color:var(--text-bright)">${esc(epName)}</td>
    <td>${statusBadge}</td>
    <td>${ep.consecutive_failures || 0}</td>
    <td style="color:var(--text-muted); font-size:12px; white-space:nowrap;">${esc(epCtx)} · ${esc(epCaps)}</td>
    <td style="color:var(--text-muted); font-size:12px;">${esc(ep.last_error || '-')}</td>
  </tr>`;
```

---

## 4. 测试与验证情况

1. **Go 编译与单元测试**：
   - 执行 `go build ./...`：通过。
   - 执行 `go test ./internal/server/...`：全部通过（包括 `status_page_test.go`）。
   - 执行 `go test ./...` 全量测试：全部通过。
2. **Node.js 仿真渲染测试**：
   - 提取 `status.html` 内嵌 JS 脚本与 DOM 上下文，注入与实际环境一致的虚拟模型/端点数据进行模拟渲染。
   - 确认生成的 HTML 结构与样式类完全符合预期：
     - `.model-header-left` 内正确渲染 `agent` 和 `<span class="badge badge-primary">(openai-completions)</span>`。
     - `.model-tags` 右对齐显示能力集、上下文与端点数。
     - 表头与表体已成功移除 `PROTOCOL` 列。
     - 端点名成功去除前缀为 `bai/deepseek-v4-flash` 等。

---

## 5. 关于第 5 项（`Requests/FAILS`）的深入说明与建议

用户在需求中提出了：“*change column 'FAILS' to 'Reqests/FAILS', if possible.*”

经过对整体架构与数据流的审查：
1. **当前数据链现状**：
   - 路由核心层（`internal/router`）在请求热路径上为了追求极致性能与零锁争用，只在 `Telemetry` 中维护了进程生命周期的几个全局 `atomic.Uint64`（总请求数、按协议划分的请求数、按状态划分的请求数）。
   - `adminStatus`（`/status` 接口）返回的 `models[].endpoints[]` 数据来源于路由快照与 `health.Registry`。`health.Registry` 内部只记录每个 endpoint 的连续失败数（`fails`）、冷却截止时间（`cooldownUntil`）和熔断探测标记，**没有记录该 endpoint 累计处理了多少次请求**。
   - 契约测试 `internal/server/admin_status_test.go` 严格限定了 endpoint 的 JSON 键集合：`endpoint, protocol, priority, consecutive_failures, available, serving, capabilities, max_context_tokens`。
2. **如果要在后续支持该字段所需的改动**：
   - 需要在 `health.Registry` 或 `router.Telemetry` 中新增按 endpoint 的原子计数器或并发 map。
   - 在 `router.tryOne` 的请求转发成功/失败回调中递增对应 endpoint 的 counter。
   - 在 `internal/server/admin.go` 中暴露 `requests` 字段，并同步更新 `admin_status_test.go` 的接口契约测试。
3. **建议**：
   - 本次改动先保持 `FAILS` 列的纯前端展示与现有语义（连续失败数）；
   - 若后续确实需要实时端点级请求统计，可作为独立的后端特性（Routing/Telemetry 增强）进行评估与开发。

---

## 6. 当前工作区状态

按照要求，所有修改已保留在工作区中，**未执行 Git Commit**：
- `M internal/server/status.html`
- `?? docs/REVIEW_STATUS_PAGE_UPDATE.md`（本文档）

等待人工审核。


> 注：第 7 节起为后续评审（2026-08-28）追加。需求 #5（端点级 requests）经历了「初评待定 → 复议实现 → 三评从第一性原理否决并回滚」三个阶段，最终结论见第 9 节，工作区文件清单以第 10 节为准。前面各节保留过程，不回改。

---

## 7. 二次评审：方案与实现的逐行核对（2026-08-28）

### 7.1 评审方法

不依赖第 1–6 节的自述，直接读源码核对每一项需求的落点：

- 前端改动：`git diff internal/server/status.html` 全量；
- 数据来源：`internal/server/admin.go`（`/status` JSON 组装）、`internal/health/health.go`（`Status` / `consecutive_failures` 的真实语义）、`internal/router/telemetry.go`（进程内计数器）、`internal/router/router.go`（`Serve` / `tryOne` 热路径）、`internal/core/core.go`（`Endpoint.Name()` / `HealthKey()`）；
- 契约测试：`internal/server/admin_status_test.go`（endpoint JSON 键集合）、`internal/server/status_page_test.go`；
- 交叉消费方：`cmd/vmr/cmd_status.go` / `cmd_status_render.go`（`vmr status` 手写解析同一份 JSON）、`internal/report`（分析半区的端点聚合）。

### 7.2 逐项核对结论

| # | 需求 | 源码核对 | 结论 |
|---|------|----------|------|
| 1 | 协议改为 Tag、自动配色 | `renderModels` 里 `protoBadge = protoMeta.badge \|\| 'badge-primary'`；`PROTO_URLS` 中 `anthropic-messages → badge-anthropic`（紫 `#bc8cff`）、`openai-completions` / `openai-responses → badge-primary`（蓝）。`.model-name` 只剩 `${esc(m.id)}`，协议进独立 `span.badge`。标签文案见第 11.1 节（改用 `PROTO_URLS[...].name` 友好名、去括号，与 Connect 卡片一致）。 | 与方案一致 ✅ |
| 2 | 端点名去协议前缀 | `core.Endpoint.computeName()` = `AdapterType + "/" + Provider + "/" + Model`，**全代码库无自定义端点名机制**（`config` 无 endpoint `name:` 字段，`core.Endpoint` 无对应字段），前缀恒定存在。前端 `epName.startsWith(ep.protocol + '/')` 命中即 `slice`，否则原样——守卫为纯防御。另核对 `endpointStatus.Protocol` 恒等于 `ep.AdapterType`（`BuildSnapshot` 按 `eg.Protocol` 分组且 `AdapterType: eg.Protocol`），前缀匹配不会错位。 | 与方案一致、边界安全 ✅ |
| 3 | 移除 `PROTOCOL` 列 | thead / tbody 各删一列；`/status` JSON 仍输出 `protocol` 键（`admin_status_test.go` 仍要求它），前端仍用 `ep.protocol` 做前缀截取——JSON 契约未动，仅显示层收敛。 | 与方案一致 ✅ |
| 4 | 头部右侧标签右对齐 | 新增 `.model-header-left` / `.model-tags`（`margin-left:auto` + `flex-wrap:wrap` + `justify-content:flex-end`），`.model-header` 本就是 `display:flex; justify-content:space-between`。 | 与方案一致 ✅ |
| 5 | `FAILS` 加 `Requests` | 初评「待定」→ 复议曾实现（第 8 节）→ 三评从第一性原理否决并**回滚**（第 9 节）。最终：`FAILS` 列维持现状。 | 不做 ⛔（理由见第 9 节） |
| 6 | 应用到所有卡片 | 单一 `renderModels()` 驱动，天然全量。 | 与方案一致 ✅ |

### 7.3 发现的问题与处理

评审未发现「方案 → 实现」层面的功能性缺陷或安全回归。两处需要说明/收口的点：

1. **看板模型名标签与 `core.ModelLabel` 的显示约定发生分叉（可接受）。**
   改版前 status.html 的注释写着「与 `vmr status` / `vmr diagnose` 同用 `core.ModelLabel`（`name [protocol]`）」。改版后看板用 `name` + `(protocol)` 徽章，`vmr status` CLI 仍是 `name [protocol]`。这是本次视觉改版的**预期结果**，不是 bug——看板是可视化面，CLI 是文本对齐面。改版已把 status.html 里那条过时注释改写为准确描述，不去「修正」CLI 与之统一（会破坏 CLI 列对齐，得不偿失）。

2. **首次评审第 4 节的验证力度略高于仓库实际。**
   第 4 节称跑过「Node.js 仿真渲染测试」——该产物不在仓库中，属人工验证描述；而当时的 `status_page_test.go` 并未对「PROTOCOL 列已移除」「新头部结构」做任何断言。
   *处理*：在 `status_page_test.go:TestStatusPage_ServesHTML` 补了结构断言——`model-header-left` / `model-tags` 必须存在，`>PROTOCOL</th>` 必须不存在。这几项被测试锁定（此断言与 #5 的回滚无关，保留）。

---

## 8. 需求 #5 复议记录（过程，已被第 9 节推翻）

> 本节记录复议时「决定实现」的推理与落地。结论已在三评被推翻，代码已回滚。保留是为了让「为什么当时觉得值得做、后来又为什么不做」这条线索完整。

### 8.1 `fails` 的真实语义

`/status` 端点项的 `consecutive_failures` 来自 `health.Registry.state.fails`。它是**健康状态机的内部量表**，不是失败账本：

- **连续**失败数，非累计——`ReportSuccess` 里 `s.fails = 0`，任何一次成功即清零；
- 存在的唯一理由是驱动指数退避：`ReportFailure` 里 `backoff(base, cap, s.fails)`；
- `health.Registry` 包注释：只做失败驱动的冷却状态机。

所以 `fails` 是退避算法的副产品；端点级「请求总数」在整条路由热路径上并不存在（`router.Telemetry` 只有固定原子计数器，`health.Registry` 无任何计数）。加 `requests` 是**新增计数器**，不是暴露已有数据。用户对此的判断完全正确。

### 8.2 当时的实现方案（已回滚）

- `router.Telemetry` 加 `endpointReqs sync.Map`（`map[string]*atomic.Uint64`，key = `Endpoint.HealthKey()`）+ `RecordEndpointRequest` / `EndpointRequests`；
- `tryOne` 在 `client.Do` 返回后计一次；
- `endpointStatus` 加 `Requests uint64`，`/status.html` 加 `REQUESTS` 列，`vmr status` 有流量时追加 `reqs=N`；
- 契约测试键集合 +`requests`；设计文档与 KNOWN_ISSUES 各登记一条。

当时的判定是「Effort 小、风险低」。**这个判定不成立**，原因见下节。

---

## 9. 需求 #5 三评：从第一性原理否决（2026-08-28，最终结论）

用户对复议方案提出三点质疑，逐一回应后否决：

### 9.1 语义不搭：累计值 vs 当前量表

`consecutive_failures` 是「此刻连续失败几次」的**瞬时量表**，`requests` 是**只增的累计值**。把两者并列，读者要在同一行里切换两种时间语义（"1234 / 3" = 累计 1234 次、此刻连续失败 3 次）。用户的直觉对：真要做，得是**独立于 `consecutive_failures` 的累计 `failed`** + 累计 `requests` + 累计 `ok`，而不是拿健康量表凑数。

### 9.2 「一做就停不下来」：作用域根本不是「一个计数器」

用户点破的关键。端点级真正关心的是 **请求数 / 成功数 / 失败数 / token 消耗**，全是累计值。那么诚实的作用域不是「加一个 `requests`」，而是「加一个端点级 stats 块」：`requests` + `ok` + `failed` + 5 个 token 分量 ≈ 8 个动态 key 计数器 × N 端点。这就是一张**按端点的 stats map**，是对 `router.Telemetry`「全固定原子、热路径零 map 零锁」设计的实质性背离——不是「可接受的小例外」。

复议方案是两头不讨好：既付了架构代价（往固定原子的 Telemetry 里塞动态 map），又只交付了 1/8 的价值（只有 `requests`，还语义打架）。

### 9.3 第一性原理：这个数据早就有了，而且在对的地方

vmr 是「**带审计/分析层的路由器**」，两个半区。审计日志逐条记录了每次 attempt 的端点标签、token、结果、错误类。**分析半区 `internal/report` 已经完整产出端点级聚合**——`EndpointRow`：`Attempts` / `OK` / `Forwarded` / `Failed` / `Availability` / `ErrorRate` / `ErrorClasses` / `Requests` / `RequestsOK` / token / cost，还带 by-date 与 cross-date。这正是 `vmr analyze` / `vmr report` 的「Endpoint Health」小节。

用户要的「每个端点请求多少、成功多少、失败多少、消费多少 token（累计值）」——**已经存在，而且实现得更好**：数据源是可持久化的审计日志（进程重启不丢）、可按时间/任务/模型切片、口径由差分测试锁定（`cmd/vmr/quota_parity_test.go`）。

`/status` 的职责是 **liveness / 当前健康**：进程活着吗、配置新鲜吗、并发多少、哪个端点此刻在冷却。`consecutive_failures` 属于这里，因为它是「当前状态」。累计账不属于这里。给 `/status` 塞一份进程内、重启即失的实时副本：
- 与分析半区重复计数——正是 CLAUDE.md「一个分析数字复现一个路由数字必须差分测试锁定」那条不变式要防的负担；
- 进程内 + 重启归零，对「累计账」这个用途是**错的**，甚至会误导（重启后看着像端点没吃过流量）。

**一个信号**：复议时为了让这个改动「说得通」，我不得不在 `KNOWN_ISSUES` 写一段话去辩护那个架构例外。为一个看板列写一条「架构例外辩护」，本身就说明性价比不对。

### 9.4 最终结论

| 项 | 结论 |
|----|------|
| 需求 1–4、6 | 保留（纯前端，改得好） |
| 需求 5（`FAILS` 加 `Requests`） | **不做**。`FAILS` 列维持现状（`consecutive_failures`，健康量表，属于 liveness 视图） |
| 端点级累计账（requests / ok / failed / tokens） | 已在 `vmr analyze` / `vmr report` 的 Endpoint Health 小节完整提供，不在 `/status` 重复 |
| 已落地的代码 | **全部回滚**（`telemetry.go` / `router.go` / `admin.go` / CLI / 各测试 / 设计文档），逐字节回到 HEAD |
| 唯一保留的小改动 | `status.html` 的 `FAILS` 表头加了 `title`，说明它是「此刻连续失败数」并指向 `vmr analyze` 看累计账；`KNOWN_ISSUES` 登记「刻意不加端点级累计计数器」及理由 |

### 9.5 关于「Effort 评估」这件事本身

用户的方法论提醒是对的：**已经做了不等于要坚持**。复议时的「Effort 小」评估错在只算了「写这几行代码」的成本，没算：
- 往一个有明确设计哲学的包里引入它明文排除的东西，这个**架构债**的成本；
- 「做一半」的语义不一致成本；
- 与分析半区**双账本**的长期对账成本；
- 以及最根本的——**这个需求其实已经被满足了**，只是在另一个半区。

结合项目定位（KISS / YAGNI / 单人小团队 / 分析半区本就比路由核心大），正确动作是把用户指到 `vmr analyze`，而不是给热路径加负担。

---

## 10. 需求 #5 回滚后的工作区状态

**未执行 Git Commit。** #5 相关代码已全部回滚，改动为：

```
M  internal/server/status.html          # 需求 1-4、6 + FAILS 表头 title 澄清
M  internal/server/status_page_test.go  # 拓扑视图结构断言
M  docs/KNOWN_ISSUES_sonnet-5.md        # 登记「刻意不加端点级累计计数器」
?? docs/REVIEW_STATUS_PAGE_UPDATE.md    # 本文档
```

已回滚、不在 diff 中：`internal/router/telemetry.go`、`internal/router/router.go`、`internal/router/telemetry_test.go`、`internal/server/admin.go`、`internal/server/admin_status_test.go`、`cmd/vmr/cmd_status.go`、`cmd/vmr/cmd_status_render.go`、`docs/VirtualModelRouter_Design_v4_Core.md`。

---

## 11. 第四轮微调：协议标签与优先级配色（2026-08-28）

用户对已落地的视觉改动提两点：

### 11.1 协议标签去括号、用友好名

- **问题**：改版把协议渲染成 `(openai-completions)`——用户原始需求里的括号只是用来在文字中表示「这是个 tag」，不是要字面显示；而且用的是枚举原值。
- **改动**：`renderModels` 改用 `PROTO_URLS[m.protocol].name`（`OpenAI Completions` / `Anthropic Messages` / `OpenAI Responses`）+ 对应 `badge` 类，与「Connect Your Agent」卡片对协议的命名完全一致；无匹配时回退到原始字符串。去掉括号。
- 位置：`internal/server/status.html` `renderModels`，`protoLabel` / `protoMeta`。

### 11.2 优先级 Pill 按「层级」配色

- **目标**：一眼看出哪些端点在同一优先级（= 一个 failover group），不用逐个辨认 P 数字。
- **方案**：按**层级序号**而非 P 数字本身配色。一张模型卡里，把端点的 distinct priority 升序排列，第 1 档 → `pri-0`、第 2 档 → `pri-1`……所以「第一个模型 P0/P1/P2/P3」与「第二个模型 P10/P11/P12/P13」的四档颜色一一对应（P0 与 P10 同色）。超过 7 档一律钳到 `pri-6`。
- **配色**：7 档 cool→warm，彼此区分度明显（蓝 `#58a6ff` → 青 `#39c5cf` → 绿 `#3fb950` → 金 `#d2a922` → 橙 `#e8813a` → 粉 `#f778ba` → 红 `#f85149`），沿用既有 badge 样式（`rgba(...,0.15/0.16)` 底 + 实色字 + `rgba(...,0.35)` 边）。不是低对比渐变。
- 为什么 cool→warm 而非纯字面梯度：P0 是最优先 / 最常命中的一档，用冷静的蓝；越靠后越「热」，隐含「这是兜底档」的直觉。层级到色的映射本身与 P 数字解绑（用户的核心诉求）。
- 位置：`status.html` 新增 `.pri-0`…`.pri-6` CSS；`renderModels` 里 `priTier` 映射表；行内 `<span class="badge pri-${priTier[ep.priority] || 0}">`；`PRI` 表头加 `title` 说明「同色 = 同层级」。

**验证**：`node --check` 内嵌 JS 语法通过；层级映射逻辑用独立脚本核对（P0/P1/P2/P3 与 P10/P11/P12/P13 得到相同的 pri-0..pri-3；第 8 档起钳到 pri-6）；起 `vmr start`（agent P0/P1/P2 + claude P10/P11）实跑确认 `/status.html` 含 `.pri-0`…`.pri-6` 与 `protoLabel`；`gofmt` / `go build` / `go test ./internal/server/...` 全绿。

---

## 12. 最终工作区状态（2026-08-28，含第 11 节）

**未执行 Git Commit。**

```
M  internal/server/status.html          # 需求 1-4、6；FAILS/PRI 表头 title；协议友好名（去括号）；优先级层级配色 .pri-0..6
M  internal/server/status_page_test.go  # 拓扑视图结构断言（model-header-left / model-tags / 无 PROTOCOL 列）
M  docs/KNOWN_ISSUES_sonnet-5.md        # 登记「刻意不加端点级累计计数器」及第一性原理理由
?? docs/REVIEW_STATUS_PAGE_UPDATE.md    # 本文档
```

需求 #5 的实时端点计数改动已全部回滚（清单见第 10 节）。

验证：`go build ./...`、`gofmt -l`、`go vet ./...`、`go test ./...`（含 `-race` 对 router / server / health）全部通过；内嵌 JS `node --check` 通过。

等待人工审核。
