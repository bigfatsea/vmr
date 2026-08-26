# `/status` 扩展分析报告

> 目标：为 `/status` 的 `models` 块添加 `max_context_tokens` 和 `capabilities`
> 字段，并新增 `instance.base_urls`（从请求自身地址推导），以便 Agent 系统
> （如 Claude Code 的 custom model）配置指向 VMR 时能正确获知上下文长度和能力信息。
> 同时审视 check / diagnose / status / /v1/models 各展示面的数据结构，在能一致处对齐。

---

## 1. 现状分析

### 1.1 `/status` 当前 JSON 结构

`models` 输出位于 `internal/server/admin.go` 的 `adminStatus` 方法：

```go
out := map[string][]epStatus{}  // key: "name [protocol]"
for protocol, byName := range snap.Models {
    for name, route := range byName {
        key := name + " [" + protocol + "]"
        for _, ep := range route.Endpoints {
            out[key] = append(out[key], epStatus{
                Endpoint: ep.Name(),
                Protocol: protocol,
                Priority: ep.Priority,
                Status:   s.rt.Health.Status(ep.HealthKey(), now),
            })
        }
    }
}
```

**当前 JSON 形状**：
```json
{
  "models": {
    "agent [openai]": [
      {
        "endpoint": "openai/p1/sonnet",
        "protocol": "openai",
        "priority": 1,
        "available": true,
        "serving": true,
        "consecutive_failures": 0,
        "last_error": "",
        "cooldown_until": "0001-01-01T00:00:00Z",
        "probing": false
      }
    ]
  }
}
```

### 1.2 数据可用性

`core.Endpoint` 已包含所需字段：

| 字段 | 类型 | 来源 | 说明 |
|------|------|------|------|
| `Capabilities []string` | 切片 | `BuildSnapshot` 中 `mergeCapabilities(base, extra)` | 模型基线 + 端点叠加的并集，已解析完成 |
| `ExtraCapabilities []string` | 切片 | 端点自己的声明 | 纯展示用，用于 `vmr check` 的分层打印 |
| `MaxContextTokens int64` | 数值 | 端点覆盖（非零）或继承模型基线 | 0 = 不限制 |
| `OwnMaxContextTokens int64` | 数值 | 端点自己的覆盖 | 纯展示用，0 = 继承基线 |

因此**不需要解析 config 的额外工作**，直接将 `ep.Capabilities` 和 `ep.MaxContextTokens` 序列化进 JSON 即可。

### 1.3 现有消费者

| 消费者 | 文件 | 读取方式 | 是否需适配 |
|--------|------|----------|-----------|
| `status.html` JS 仪表盘 | `internal/server/status.html` | `Object.keys(data.models)` 遍历 map，`modelsMap[mKey]` 取端点数组 | **是** — 变为数组后需改为 `data.models.forEach(m => m.endpoints)` |
| `vmr status` CLI | `cmd/vmr/cmd_status.go` | `statusResponse.Models` 为 `map[string][]struct{...}` | **是** — 类型定义需改为 `[]type` |
| `vmr status` 渲染 | `cmd/vmr/cmd_status_render.go` | `core.SortedKeys(st.Models)` 遍历 map，按 key 打印 | **是** — 改为数组遍历 `st.Models` |
| 测试: `server_test.go` | `internal/server/server_test.go:100-106` | 内联匿名 struct `Models map[string][]struct{...}` | **是** — 3 处解码 |
| 测试: `active_probe_test.go` | `internal/server/active_probe_test.go:252-258` | 同上 | **是** |
| 测试: `main_test.go` | `cmd/vmr/main_test.go` | Mock server 返回 `map[string]any` | **是** — 2 处 mock 数据 |
| `vmr.sh ps` | `vmr.sh:241-268` | 通过 `vmr status -brief` 间接消费 | CLI 已适配即无影响 |
| 用户文档 | `docs/UserGuide.md`, `docs/UserGuide.zh.md` | 文本描述 | **建议更新** |

---

## 2. 变更方案设计

### 2.1 新 JSON 形状

```json
{
  "models": [
    {
      "id": "agent",
      "protocol": "openai",
      "capabilities": ["text", "tools", "image"],
      "max_context_tokens": 200000,
      "endpoints": [
        {
          "endpoint": "openai/p1/sonnet",
          "protocol": "openai",
          "priority": 1,
          "available": true,
          "serving": true,
          "consecutive_failures": 0,
          "last_error": "",
          "cooldown_until": "0001-01-01T00:00:00Z",
          "probing": false
        }
      ]
    }
  ],
  "instance": {
    "pid": 12345,
    "listen": "0.0.0.0:8800",
    "base_urls": {
      "openai": "http://192.168.0.22:8800/v1/",
      "anthropic": "http://192.168.0.22:8800/v1/",
      "openai-responses": "http://192.168.0.22:8800/v1/"
    },
    ...
  }
}
```

> `instance.base_urls` 展示的是**请求方访问 `/status` 时实际使用的地址**（`r.Host` + 是否 TLS），
> 不是 `listen` 配置——见 D4：用 `127.0.0.1` 访问就显示 `127.0.0.1`，用 `localhost` 就显示 `localhost`，
> 用局域网 IP（如 `192.168.0.22`）就显示该 IP。

### 2.2 关键设计决策

#### D1: 从 `map` 改为 `array` — 重大变更，但可接受

**理由**：
- 旧形状 `map[string][]epStatus` 的 key `"name [protocol]"` 是拼接字符串，无法自然地携带模型级字段（`capabilities`、`max_context_tokens`）
- 新形状 `[]modelStatus` 是标准 REST 风格，每项包含 `id` + `protocol` + 模型级字段 + `endpoints[]`
- `KNOWN_ISSUES` 已明确：**CLI 与 Server 版本必须匹配**，形状变更时不做兼容性处理
- 所有消费者（CLI + 仪表盘 + 测试）在同一版本中同步更新

**风险**：`vmr.sh ps` 通过 `vmr status -brief` 间接消费，`-brief` 只输出 `pid\tlisten\tuptime\tmodels\tversion\tstale\tconfig` 一行，不解析 `models` 数组，**无影响**。

#### D2: `capabilities` — 取并集（union）

模型中不同端点可能有不同的能力声明（`EndpointGroup.Capabilities` 叠加在 `VirtualModel.Capabilities` 基线上）。由于客户端 Agent 需要知道"这个虚拟模型整体支持什么"，我们取同模型下所有端点的 `capabilities` 的并集。

**始终输出该字段**（即使为空数组，不用 `omitempty`）：你的目标布局明确写了 `capabilities: []`，且消费方是 Agent 配置系统，需要一个稳定的、字段恒存在的 schema。空数组语义与 vmr 路由一致 = **不限制 / 支持一切**（`core.Endpoint.HasCapability` 当 `len(Capabilities)==0` 时返回 true）。Agent 读到空数组可据此不设能力限制。

#### D3: `max_context_tokens` — 取最大值

对于同一虚拟模型下的多个端点，`max_context_tokens` 可能不同（端点层可覆盖模型基线）。取最大值作为模型级上下文长度，因为：
- Agent 配置上下文长度时，告知最大可用窗口让 Agent 能充分利用
- 每个端点自己的值仍保留在 `endpoints[].max_context_tokens` 中（如果与模型级不同）

**始终输出该字段**（`0` = 不限制）。同 D2 的理由：Agent 需要一个字段恒存在的稳定 schema。`0` 与 vmr 路由语义一致 = 无上限。

> ⚠️ 设计取舍提醒：`capabilities: []`、`max_context_tokens: 0` 都表示"不限制"，但与"真不支持任何能力"（也不可能发生，因为 `text` 恒真）在字面上容易混淆。建议在 `status.html` 和文档中把空/0 渲染为 `unconstrained`（或 `any`），避免人工读 JSON 时误判。

#### D4: `instance.base_urls` — 从请求自身的地址推导（不用 `listen`）

用户反馈：`base_urls` 应该反映**请求方实际用来访问 `/status` 的地址**，而不是配置里的 `listen`。
- 请求方用 `127.0.0.1` → 显示 `127.0.0.1`
- 请求方用 `localhost` → 显示 `localhost`
- 请求方用其他 IP → 显示该 IP

**实现**：`adminStatus` 已有 `r *http.Request`，取：
- host = `r.Host`（HTTP Host 头 = 客户端访问本服务时使用的地址，含端口；反向代理场景下即为代理对外暴露的地址，恰好是客户端真正要用的）
- scheme = `r.TLS != nil ? "https" : "http"`（客户端若经 TLS 访问，则广告 https）

协议路径（三个协议共享 `/v1/` 前缀，值相同）：
- openai: `POST /v1/chat/completions` → base_url = `<scheme>://<host>/v1/`
- anthropic: `POST /v1/messages` → base_url = `<scheme>://<host>/v1/`
- openai-responses: `POST /v1/responses` → base_url = `<scheme>://<host>/v1/`

边界：
- `r.Host` 为空（极端情况）→ 兜底 `127.0.0.1`
- 大小写保留原样（客户端写 `LOCALHOST` 就显示 `LOCALHOST`，KISS）
- 不做 `X-Forwarded-Host` 处理：广告"客户端实际用过的地址"而非"代理认为的地址"，行为最简单且不会泄露代理内部信息
- 此值仅作展示/配置提示，不参与任何鉴权或路由决策，Host 头可伪造也无安全影响

#### D5: 端点级保留 `max_context_tokens` 和 `capabilities`

在 `endpoints[]` 中的每个端点也带上其自身的 `max_context_tokens` 和 `capabilities`，以便在端点间差异时能逐端点查看。这赋予 `status.html` 仪表盘更细粒度的展示能力。

### 2.3 跨展示面数据结构一致性（check / diagnose / status / /v1/models）

用户要求审视所有展示 Configuration / 虚拟模型 / 端点信息的表面，尽量对齐数据结构。

#### 各展示面现状

| 展示面 | 形态 | 模型标识 | 端点标识 | capabilities/context | 与 /status 的关系 |
|--------|------|----------|----------|----------------------|-------------------|
| `/status`（admin.go） | JSON | map key `"name [protocol]"` → 改为结构化 `id`+`protocol` | `ep.Name()` = `adapter/provider/model` | 无 → 新增（union/max） | 本体（运行时视图） |
| `status.html` | 消费 /status JSON | 渲染 key | 渲染 `ep.endpoint` | 无 → 新增 | 直接消费，同数据 |
| `vmr status` CLI | 解析 /status JSON | 打印 key | 打印 `ep.endpoint` | 无 → 新增 | 直接消费，同数据 |
| `vmr check`（cmd_check.go） | 静态文本表（config 视图） | `name:` 两级缩进（模型级字段在上，协议级端点在下） | `provider/model`（协议在上一级 header） | **有**：模型基线 + 端点自有叠加/覆盖（分层展示，有意为之） | 独立：config 侧、离线、看配置缺口 |
| `vmr diagnose`（diagnose.go） | 文本表 + `-json` 结果数组 | `Result.Group = "name [protocol]"`（diagnose.go:282） | `provider/model`（group 内协议隐含） | 无 | 独立：逐检查结果（ok/warn/fail） |
| `/v1/models`（server.go） | 协议面 JSON | `id` + `vmr_protocol` | 无端点概念 | 无 | 协议 schema，面向 OpenAI/Anthropic SDK |

#### 结论：统一口径

1. **运行时模型视图以 `/status` 为准**：`models` 数组（`id`/`protocol`/`capabilities`/`max_context_tokens`/`endpoints[]`）是唯一权威的运行时结构。`status.html` 与 `vmr status` CLI 直接消费它，三者天然一致、同步演进——这是"统一"最大的落点；其余表面不动这个视图就不该自己再造一份。

2. **人类可读模型标签统一为 `"<name> [<protocol>]"`**：diagnose 的 Group、`vmr status` 的输出、`status.html` 的模型头都用同一格式。抽 `core.ModelLabel(name, protocol)` 纯函数锁住格式（`core` 已有 `SortedKeys` 这类零依赖展示辅助先例），diagnose 与 cmd_status_render 调用它；`status.html` 是 JS，按同一格式内联（格式由 cmd 侧测试断言兜底）。

3. **字段语义统一**：`capabilities` 空 = 不限制、`max_context_tokens` 0 = 不限制，与 `core.Endpoint.HasCapability` 的既有语义一致；check / status / 文档三处同一口径。

4. **端点标识分两档，均为有意**：
   - 运行时/机器面：`ep.Name()` = `adapter/provider/model`（/status 用，core 文档已声明这是权威格式）。
   - config 侧 CLI 表格：`provider/model`（check/diagnose 用；协议已在外层 header/group 中，省略 adapter 是上下文合法的缩写）。两档都是既定格式，不强行统一。

5. **刻意不统一**：
   - `/v1/models`：协议面 schema（id/object/type/owned_by），由 OpenAI/Anthropic 客户端约定决定，不并入。
   - `vmr check` 的模型头用两级缩进（模型级字段 + 协议级端点列表）而不是 `"name [protocol]"` 扁平标签：check 要同时展示模型级基线字段与多协议端点，两级布局是信息结构使然；diagnose 的 Group 是扁平 Result 数组的归属字段，两者形态不同但语义同源（都标识"某个虚拟模型的某个协议面"）。
   - `vmr diagnose` 的扁平 Result 数组：逐检查语义（每条是独立 ok/warn/fail），重构成嵌套模型树没有消费方受益，不动。

6. **代码共享边界**：只有 `core.ModelLabel` 跨表面共享；`/status` 的视图结构体与 union/max 聚合辅助函数留在 server（唯一生产/消费方）；check/diagnose 各自从 config/snapshot 直接构建（各自展示的信息集合本来不同）。JSON 即契约，用契约测试锁形状（见 Action Plan 第 4 步）。

---

## 3. 影响范围与修改清单

### 3.1 后端修改

| 文件 | 改动 | 优先级 |
|------|------|--------|
| `internal/server/admin.go` | 重构 `adminStatus` 的 models 输出：从 `map[string][]epStatus` 改为 `[]modelStatus`；新增 `modelStatus` 和 `endpointStatus` 类型 | P0 |
| `internal/server/admin.go` | `instance.base_urls`：从请求推导（`r.Host` + `r.TLS`），`instanceBlock` 增加 baseURLs 参数 | P0 |
| `internal/server/admin.go` | 新增 `instanceBaseURLs(scheme, host)`、`unionCapabilities`、`maxContextTokens` 辅助方法 | P0 |
| `internal/server/admin.go` | 新增 `sort` 导入（`sort.Strings`）；`net` 不再需要（无 listen 解析） | P0 |
| `internal/core/core.go` | 新增 `ModelLabel(name, protocol string) string`（`"name [protocol]"` 唯一格式来源，见 2.3） | P1 |
| `internal/server/server.go` | 无改动（`Handler()` 注册不动） | — |

### 3.2 CLI 修改

| 文件 | 改动 | 优先级 |
|------|------|--------|
| `cmd/vmr/cmd_status.go` | `statusResponse.Models` 类型从 `map[string][]struct{...}` 改为 `[]struct{...}`；新增 `id`、`protocol`、`capabilities`、`max_context_tokens`、嵌套 `endpoints` 结构 | P0 |
| `cmd/vmr/cmd_status_render.go` | `printStatus` 中 `models` 遍历改为数组迭代；模型名从 `core.SortedKeys(st.Models)` 改为 `core.ModelLabel(m.ID, m.Protocol)`；端点是 `endpoints[]` 子结构 | P0 |
| `internal/diagnose/diagnose.go` | route 阶段 Group 改用 `core.ModelLabel(name, protocol)`（输出格式不变，纯统一，见 2.3） | P2 |

### 3.3 前端修改

| 文件 | 改动 | 优先级 |
|------|------|--------|
| `internal/server/status.html` | `renderModels` 函数：从 `Object.keys(modelsMap)` 改为 `models.forEach(m => {...})`；模型名从 `mKey` 改为 `m.id` + `m.protocol`；端点是 `m.endpoints` 子数组；新增 `m.capabilities` 和 `m.max_context_tokens` 展示 | P0 |
| `internal/server/status.html` | 在模型 header 中展示 `capabilities` 徽章和 `max_context_tokens` 信息 | P0 |

### 3.4 测试修改

| 文件 | 改动 | 优先级 |
|------|------|--------|
| `internal/server/server_test.go` | 3 处 `Models map[string][]struct{...}` 解码改为新形状 | P0 |
| `internal/server/active_probe_test.go` | 1 处 `Models map[string][]struct{...}` 解码改为新形状 | P0 |
| `internal/server/health_test.go` | 1 处 `Models map[string][]struct{...}` 解码（如存在） | P0 |
| `internal/server/instance_test.go` | 验证 `instance.base_urls` 跟随请求 Host（httptest 下为 `http://127.0.0.1:<port>/v1/`，TLS 变体为 https） | P1 |
| 新增 `/status` JSON 形状契约测试（server 侧） | 断言一个 model 条目的完整字段集与端点子结构，锁住与 status.html / CLI 的契约（见 2.3 结论 6） | P1 |
| `cmd/vmr/main_test.go` | Mock server 的 `models` 数据改为新形状；`TestCmdStatus_WithMockServer` 和 `TestCmdStatus_HalfOpenRendersDistinctFromOK` 中的 mock 数据 | P0 |
| `cmd/vmr/cmd_check_quota_test.go` | 如测试中有 `/status` mock 数据 | 检查 |

### 3.5 文档更新

| 文件 | 改动 | 优先级 |
|------|------|--------|
| `docs/UserGuide.md` | 更新 `/status` 形状描述，添加 `base_urls`（从请求地址推导）说明 | P1 |
| `docs/UserGuide.zh.md` | 同上（中文同步） | P1 |
| `docs/VirtualModelRouter_Design_v4_Core.md` | 如 `/status` 形状有记录，更新之 | P2 |
| `docs/KNOWN_ISSUES_sonnet-5.md` | 注册此次变更的 known issues（如 CLI 版本兼容性确认） | P2 |

---

## 4. 详细 Action Plan

### 第 1 步：后端数据模型变更（`admin.go`）

**任务 1.1**：定义新类型

```go
// in internal/server/admin.go

type endpointStatus struct {
    Endpoint          string `json:"endpoint"`
    Protocol          string `json:"protocol"`
    Priority          int    `json:"priority"`
    health.Status
    // 新增字段（始终输出，空/0 = 不限制，见 D2/D3）
    Capabilities      []string `json:"capabilities"`
    MaxContextTokens  int64    `json:"max_context_tokens"`
}

type modelStatus struct {
    ID                string           `json:"id"`
    Protocol          string           `json:"protocol"`
    Capabilities      []string         `json:"capabilities"`
    MaxContextTokens  int64            `json:"max_context_tokens"`
    Endpoints         []endpointStatus `json:"endpoints"`
}
```

**任务 1.2**：重构 `adminStatus` 的 models 构建逻辑

```go
// 旧：
out := map[string][]epStatus{}

// 新：
var modelsOut []modelStatus
for protocol, byName := range snap.Models {
    for name, route := range byName {
        // 计算模型级字段：capabilities 取并集，max_context_tokens 取最大值
        caps := unionCapabilities(route.Endpoints)
        maxCtx := maxContextTokens(route.Endpoints)
        
        var eps []endpointStatus
        for _, ep := range route.Endpoints {
            eps = append(eps, endpointStatus{
                Endpoint:         ep.Name(),
                Protocol:         protocol,
                Priority:         ep.Priority,
                Status:           s.rt.Health.Status(ep.HealthKey(), now),
                Capabilities:     ep.Capabilities,
                MaxContextTokens: ep.MaxContextTokens,
            })
        }
        modelsOut = append(modelsOut, modelStatus{
            ID:               name,
            Protocol:         protocol,
            Capabilities:     caps,
            MaxContextTokens: maxCtx,
            Endpoints:        eps,
        })
    }
}
```

**任务 1.3**：新增辅助函数 `unionCapabilities`、`maxContextTokens`、`instanceBaseURLs`

```go
func unionCapabilities(endpoints []*core.Endpoint) []string {
    seen := map[string]bool{}
    var out []string
    for _, ep := range endpoints {
        for _, c := range ep.Capabilities {
            if !seen[c] {
                seen[c] = true
                out = append(out, c)
            }
        }
    }
    sort.Strings(out) // 确定性输出，避免 map 遍历顺序导致 JSON 波动
    return out
}

func maxContextTokens(endpoints []*core.Endpoint) int64 {
    var max int64
    for _, ep := range endpoints {
        if ep.MaxContextTokens > max {
            max = ep.MaxContextTokens
        }
    }
    return max
}
```

**任务 1.4**：`instance.base_urls` 从请求地址推导（见 D4）

在 `adminStatus` 中提取 scheme/host，传入 `instanceBlock`：

```go
scheme := "http"
if r.TLS != nil {
    scheme = "https"
}
baseURLs := instanceBaseURLs(scheme, r.Host)
// ...
"instance": s.instanceBlock(snap, len(modelsOut), baseURLs),
```

`instanceBlock` 签名增加 `baseURLs map[string]string` 参数，在其返回 map 中增加 `"base_urls": baseURLs`。

```go
func instanceBaseURLs(scheme, host string) map[string]string {
    if host == "" {
        host = "127.0.0.1" // 无 Host 头的极端兜底
    }
    base := scheme + "://" + host + "/v1/"
    return map[string]string{
        "openai":           base,
        "anthropic":        base,
        "openai-responses": base,
    }
}
```

**任务 1.5**：新增 `core.ModelLabel`（一致性，见 2.3）

```go
// in internal/core/core.go
// ModelLabel is the single human-readable label for a virtual model's
// protocol face: "<name> [<protocol>]". Every surface that shows a model
// to a human (vmr diagnose's route Group, vmr status, the /status
// dashboard) renders the same format so a name is recognizable across
// surfaces. Zero-dep pure function, like SortedKeys.
func ModelLabel(name, protocol string) string {
    return name + " [" + protocol + "]"
}
```

### 第 2 步：CLI 适配（`cmd_status.go` + `cmd_status_render.go`）

**任务 2.1**：更新 `statusResponse.Models` 类型定义

```go
type statusResponse struct {
    // ... 其他字段不变
    Models []struct {
        ID               string   `json:"id"`
        Protocol         string   `json:"protocol"`
        Capabilities     []string `json:"capabilities"`
        MaxContextTokens int64    `json:"max_context_tokens"`
        Endpoints        []struct {
            Endpoint      string    `json:"endpoint"`
            Protocol      string    `json:"protocol"`
            Priority      int       `json:"priority"`
            Fails         int       `json:"consecutive_failures"`
            CooldownUntil time.Time `json:"cooldown_until"`
            LastError     string    `json:"last_error"`
            Available     bool      `json:"available"`
            Probing       bool      `json:"probing"`
            Serving       bool      `json:"serving"`
            // 新增（与 server 侧 schema 对齐）
            Capabilities     []string `json:"capabilities"`
            MaxContextTokens int64    `json:"max_context_tokens"`
        } `json:"endpoints"`
    } `json:"models"`
    // ... 
}
```

**任务 2.2**：更新 `printStatus` 渲染逻辑

```go
// 旧:
for _, name := range core.SortedKeys(st.Models) {
    fmt.Println(name)
    for _, ep := range st.Models[name] {
        // ...
    }
}

// 新:
for _, m := range st.Models {
    modelLabel := m.ID + " [" + m.Protocol + "]"
    fmt.Println(modelLabel)
    // 可选的：打印模型级 capabilities 和 max_context_tokens
    if len(m.Capabilities) > 0 {
        fmt.Printf("  capabilities: %s\n", strings.Join(m.Capabilities, ", "))
    }
    if m.MaxContextTokens > 0 {
        fmt.Printf("  max_context_tokens: %d\n", m.MaxContextTokens)
    }
    for _, ep := range m.Endpoints {
        // 原有渲染逻辑
        state := "ok"
        if !ep.Available {
            state = fmt.Sprintf("COOLDOWN until %s (%s, fails=%d)", ...)
        } else if !ep.Serving {
            state = fmt.Sprintf("half-open (%s, fails=%d%s)", ...)
        }
        fmt.Printf("  p%-3d %-40s %s\n", ep.Priority, ep.Endpoint, state)
    }
}
```

**任务 2.3**：`internal/diagnose/diagnose.go` 的 route 阶段 Group 改用 `core.ModelLabel(name, protocol)`（输出格式不变，去掉重复的 `fmt.Sprintf("%s [%s]", ...)`，见 2.3 结论 2）。

### 第 3 步：前端仪表盘适配（`status.html`）

**任务 3.1**：更新 `renderModels` 函数

```javascript
// 旧:
function renderModels(modelsMap) {
    dom.modelsContainer.innerHTML = '';
    const modelKeys = Object.keys(modelsMap).sort();
    // ...
    modelKeys.forEach(mKey => {
        const endpoints = modelsMap[mKey] || [];
        // ...
        <span class="model-name">${esc(mKey)}</span>
        // ...
    });
}

// 新:
function renderModels(models) {
    dom.modelsContainer.innerHTML = '';
    dom.modelCount.textContent = `${models.length} models`;
    // ...
    models.forEach(m => {
        const endpoints = m.endpoints || [];
        const modelLabel = m.id + ' [' + m.protocol + ']';
        // 模型 header 中展示 capabilities 和 max_context_tokens
        let metaParts = [];
        if (m.capabilities && m.capabilities.length > 0) {
            metaParts.push('capabilities: ' + m.capabilities.join(', '));
        }
        if (m.max_context_tokens && m.max_context_tokens > 0) {
            metaParts.push('max_context: ' + fmtNum(m.max_context_tokens));
        }
        let metaHtml = metaParts.length > 0 ? ' · ' + esc(metaParts.join(' · ')) : '';
        // ...
        <span class="model-name">${esc(modelLabel)}${metaHtml}</span>
        // ... 端点表格不变
        endpoints.forEach(ep => {
            // 端点行中可添加 capabilities 和 max_context_tokens 列
        });
    });
}
```

**任务 3.2**：在端点表格中新增可选的 `capabilities` 和 `max_context_tokens` 列

在端点表格的 `thead` 中添加两列，tbody 中渲染对应值。如果该端点值与模型级相同，可以省略以节约空间；或者始终显示（更一致）。

### 第 4 步：测试更新

**任务 4.1**：`internal/server/server_test.go`

约 3 处内联类型定义需要更新。例如：

```go
// 旧:
var out struct {
    Models map[string][]struct {
        Endpoint      string    `json:"endpoint"`
        Available     bool      `json:"available"`
        Fails         int       `json:"consecutive_failures"`
        LastError     string    `json:"last_error"`
        CooldownUntil time.Time `json:"cooldown_until"`
    } `json:"models"`
}

// 新:
var out struct {
    Models []struct {
        ID       string `json:"id"`
        Protocol string `json:"protocol"`
        Endpoints []struct {
            Endpoint      string    `json:"endpoint"`
            Available     bool      `json:"available"`
            Fails         int       `json:"consecutive_failures"`
            LastError     string    `json:"last_error"`
            CooldownUntil time.Time `json:"cooldown_until"`
        } `json:"endpoints"`
    } `json:"models"`
}
```

访问逻辑也要更新：
```go
// 旧:
eps := out.Models["vm [openai]"]

// 新:
var eps []struct{...}
for _, m := range out.Models {
    if m.ID == "vm" && m.Protocol == "openai" {
        eps = m.Endpoints
        break
    }
}
```

**任务 4.2**：`internal/server/active_probe_test.go`

同上，更新 `Models` 类型和访问逻辑。

**任务 4.3**：`cmd/vmr/main_test.go`

更新 Mock handler 的 JSON 编码：

```go
// 旧:
"models": map[string]any{
    "vm [openai]": []map[string]any{
        {
            "endpoint":             "openai/p1/m",
            "protocol":             "openai",
            "priority":             1,
            "consecutive_failures": 0,
            "available":            true,
            "serving":              true,
        },
    },
},

// 新:
"models": []map[string]any{
    {
        "id":       "vm",
        "protocol": "openai",
        "endpoints": []map[string]any{
            {
                "endpoint":             "openai/p1/m",
                "protocol":             "openai",
                "priority":             1,
                "consecutive_failures": 0,
                "available":            true,
                "serving":              true,
            },
        },
    },
},
```

同时更新测试断言中的字符串匹配（`"vm [openai]"` → `"vm [openai]"` 仍存在，因为 `printStatus` 渲染的模型标签格式不变）。

**任务 4.4**：新增验证测试

- 在 `internal/server/instance_test.go` 中添加测试：请求 `http://127.0.0.1:<port>/status` 时 `instance.base_urls` 为 `http://127.0.0.1:<port>/v1/`；用 `httptest.NewTLSServer` 变体验证 https（如值得）。
- 新增 `/status` JSON 形状契约测试：解码一个 model 条目，断言字段集恰好为 `id`/`protocol`/`capabilities`/`max_context_tokens`/`endpoints`，端点内字段集恰好为既有 8 个 + `capabilities` + `max_context_tokens`——这是 status.html 与 CLI 所依赖的契约，形状漂移在此被抓住。
- `internal/core` 的 `ModelLabel` 单测。

**任务 4.5**：一致性复核（check / diagnose 对齐）

- 复核 `vmr check` 输出词汇：`capabilities` / `max_context_tokens` 字段名与 /status 一致（已是，无需改）。
- `vmr diagnose` route 阶段 Group 确认输出与 `core.ModelLabel` 一致（任务 2.3 已改）。
- `status.html` 模型头格式 `id + ' [' + protocol + ']'` 与 `core.ModelLabel` 一致（JS 内联，由 cmd 侧测试断言 "vm [openai]" 兜底）。

### 第 5 步：集成验证

**任务 5.1**：编译测试

```bash
go build -o vmr ./cmd/vmr
go test ./internal/archtest/...  # 确保架构边界未破坏
go test ./internal/server/...    # 所有 server 测试通过
go test ./cmd/vmr/...            # 所有 CLI 测试通过
```

**任务 5.2**：手动验证

```bash
# 1. 启动一个带配置的实例
./vmr start -c testdata/config.yaml &
# 2. 检查 /status JSON
curl -s http://127.0.0.1:8800/status | jq '.models'
# 3. 检查 instance.base_urls
curl -s http://127.0.0.1:8800/status | jq '.instance.base_urls'
# 4. 打开仪表盘
open http://127.0.0.1:8800/status.html
# 5. 检查 CLI 输出
./vmr status -c testdata/config.yaml
# 6. 用不同 Host 访问验证 base_urls 跟随请求地址
curl -s -H 'Host: localhost:8800' http://127.0.0.1:8800/status | jq '.instance.base_urls'
curl -s http://192.168.0.22:8800/status | jq '.instance.base_urls'   # 局域网 IP（如适用）
```

### 第 6 步：文档同步

**任务 6.1**：更新 `docs/UserGuide.md`

在 `/status` 的表格行中补充：
- `models` 现在为数组，每项包含 `id`、`protocol`、`capabilities`、`max_context_tokens`、`endpoints`
- `instance.base_urls` 字段说明：值来自**访问方请求的地址**（`r.Host` + TLS 与否），不是 `listen` 配置；用 `127.0.0.1` 访问就显示 `127.0.0.1`，用 `localhost` 就显示 `localhost`，用其他 IP 就显示该 IP
- 一致性约定：模型的人类可读标签统一为 `"<name> [<protocol>]"`（`core.ModelLabel`）；空 `capabilities` / 0 `max_context_tokens` = 不限制

**任务 6.2**：同步 `docs/UserGuide.zh.md`

**任务 6.3**：在 `docs/KNOWN_ISSUES_sonnet-5.md` 中注册

记录此次形状变更（map→array），确认"CLI 与 Server 版本必须匹配"原则已覆盖此变更；并登记一致性约定（`core.ModelLabel` 为 `"name [protocol]"` 唯一格式来源；/v1/models、check 分层视图、diagnose 扁平结果按设计保持差异）。

### 第 7 步：收尾

**任务 7.1**：执行 `gofmt -l` 检查格式

```bash
gofmt -l internal/server/ cmd/vmr/
```

**任务 7.2**：更新 `CHANGELOG.md`

在 `[Unreleased]` 的 `Changed` 或 `Added` 分类下添加条目。

---

## 5. 时间线估算

| 步骤 | 任务 | 预估工时 |
|------|------|---------|
| 1 | 后端数据类型变更（含 base_urls 请求推导 + core.ModelLabel） | 2h |
| 2 | CLI 适配（含 diagnose 标签统一） | 1h |
| 3 | 前端仪表盘适配 | 1.5h |
| 4 | 测试更新（含形状契约测试 + 一致性复核） | 2h |
| 5 | 集成验证 | 0.5h |
| 6 | 文档同步 | 0.5h |
| 7 | 收尾（gofmt, CHANGELOG） | 0.5h |
| **合计** | | **~8h** |

---

## 6. 风险与注意事项

1. **`vmr.sh ps` 无影响**：`-brief` 模式只输出 `models` 计数（`st.Instance.Models`），不解析 `models` 数组内容。

2. **`vmr.sh status` 穿透**：脚本最终调用 `vmr status -c ...`，CLI 已适配则无问题。

3. **`vmr status -addr` 跨版本**：旧 server 返回的 `models` 是 map，新 CLI 解析时会因 JSON 类型不匹配而报错。`KNOWN_ISSUES` 已明确："CLI 与 Server 版本必须匹配，任何不一致直接报错。"这是预期行为，无需额外处理。

4. **`status.html` 的 `localStorage` 存储的 API Key**：不受形状变更影响。

5. **`capabilities`/`max_context_tokens` 始终输出（不用 `omitempty`）**：这是 D2/D3 的决策。空数组/0 表示不限制，与 vmr 路由语义一致。消费方（Agent / status.html / CLI）必须按"字段恒存在"来处理，不要依赖字段缺失。若你更想保持向后兼容的省略语义，可改用 `omitempty`，但这会让 Agent schema 不稳定——本报告推荐始终输出。

6. **`capabilities` 排序**：为确保 JSON 输出确定性，`unionCapabilities` 的结果应排序（`sort.Strings`），避免因 map 遍历顺序产生非确定性输出。

7. **`base_urls` 取自请求 Host，是可伪造/可变的展示字段**：不参与鉴权或路由，无安全影响；反向代理后它恰好显示代理对外的地址（客户端真正要用的），是特性不是 bug。

8. **`base_urls` 随请求方不同而变化**：同一实例，`curl 127.0.0.1` 与 `curl localhost` 看到的 `instance.base_urls` 不同——这是设计意图（"告诉你客户端该用的地址"），文档里写清楚即可，不要试图缓存/固定。

9. **一致性是"口径"而非"代码"**：唯一跨表面共享的代码是 `core.ModelLabel`；其余靠"契约测试锁 /status 形状 + 各表面复用同一 JSON"。不要在 check/diagnose 里强行引入 /status 的结构体——它们展示的信息集合本来不同，强行共用反而增加耦合。

---

## 7. 结论

**可行性：高**。所需数据已经存在于 `core.Endpoint` 中，不需要解析 config 的额外工作。变更涉及面清晰可控：
- 后端：`admin.go`（形状重构 + base_urls 请求推导）+ `core.go`（ModelLabel）
- CLI：`cmd_status.go` 类型定义 + `cmd_status_render.go` 遍历逻辑 + `diagnose.go` 标签统一
- 前端：`status.html` 的 `renderModels` 函数
- 测试：4 个测试文件的内联类型/mock 数据 + 新增形状契约测试
- 文档：2 个文件

**推荐采用新形状（map→array）**，因其更清晰地表达了"虚拟模型 → 端点"的层级关系，且项目已明确不做跨版本兼容，无需保留旧形状。

**`instance.base_urls` 从请求推导**（`r.Host` + `r.TLS`），跟随请求方实际使用的地址，而非 `listen` 配置。

**一致性结论**：运行时视图以 `/status` 为准（status.html 与 vmr status CLI 直接消费同一 JSON，天然一致）；模型标签统一为 `core.ModelLabel` 的 `"<name> [<protocol>]"` 格式；字段语义（空 capabilities / 0 context = 不限制）与 core 一致；`/v1/models`（协议 schema）、check 的分层 config 视图、diagnose 的扁平检查结果按设计保持差异，不做强行统一。

**注意**：`adminStatus` 中 `instanceBlock(snap, len(out))` 的模型计数不受影响——旧 `len(out)`（map 键数）与新 `len(modelsOut)`（数组元素数）都等于 (name, protocol) 组合数，语义不变。
---

## 8. 执行记录（2026-08-26 落地）

> 本章是 §4 Action Plan 的施工日志：按步骤记录实际做了什么、与计划的偏差、验证结果。分析结论（§1-§7）保持为设计时的原文，未回写改动。

### 8.1 第 1 步：后端（`internal/server/admin.go` + `internal/core`）

- 新增导出类型 `modelStatus{ID, Protocol, Capabilities, MaxContextTokens, Endpoints}` 与 `endpointStatus{Endpoint, Protocol, Priority, health.Status, Capabilities, MaxContextTokens}`，全部字段**始终输出**（无 `omitempty`）。
- `adminStatus`：`map[string][]epStatus` 拼接键 -> `[]modelStatus` 结构化数组；按 `core.SortedKeys(protocol)` 再 `core.SortedKeys(name)` 双层排序，保证 JSON 确定性。模型计数 `len(models)` 与旧 `len(out)` 语义一致（(name, protocol) 组合数）。
- 新增 `unionCapabilities`（并集 + `sort.Strings` + nil 兜底为 `[]string{}`，空数组而非 null）、`maxContextTokens`（取最大，0=不限制）、`instanceBaseURLs(scheme, host)`（空 host 兜底 `127.0.0.1`）、`requestScheme(r)`（`r.TLS != nil` -> https）。
- `instanceBlock` 签名增加 `baseURLs map[string]string`，`base_urls` 直接进入 instance 块（按 D4：host 取 `r.Host`、scheme 取 TLS 与否，回显请求自身的地址）。
- `internal/core/core.go`：新增 `ModelLabel(name, protocol)`（`"<name> [<protocol>]"` 唯一格式来源），紧邻 `SortedKeys`，同为零依赖纯函数；`endpointlabel_test.go` 加单测。

### 8.2 第 2 步：CLI（`cmd/vmr/` + `internal/diagnose/`）

- `cmd_status.go`：`statusResponse.Models` 改为 `[]struct{ID, Protocol, Capabilities, MaxContextTokens, Endpoints[]...}`；端点结构在原有 8 字段上追加 `capabilities`/`max_context_tokens`。`Serving` 的注释原样保留。
- `cmd_status_render.go`：数组遍历替代 `core.SortedKeys`；模型头改用 `core.ModelLabel(m.ID, m.Protocol)`；模型级输出两行 `capabilities: a, b` / `max_context_tokens: N`（仅在非空/非 0 时打印，`vmr check` 同一口径）；端点行渲染逻辑不变。
- `internal/diagnose/diagnose.go`：route 阶段 `Group` 改用 `core.ModelLabel(name, protocol)`，删除重复的 `fmt.Sprintf`，输出逐字节不变。

### 8.3 第 3 步：前端（`internal/server/status.html`）

- `renderModels`：`Object.keys()` 遍历 map -> 数组 `forEach`；模型头 `m.id [m.protocol]`（与 `core.ModelLabel` 同格式，JS 内联）。
- 模型头新增两枚徽章：capabilities 并集（空则渲染 `unconstrained`，不渲染空徽章--§2.2 D2 取舍提醒的落地）与 `ctx: 200k tok`（0 则 `unconstrained`）。
- 端点表新增 `CTX / CAPS` 列（端点自有覆盖值，无则 `-`）；新增 `fmtCtx`（200000 -> `200k`）。
- 空态与 0 模型卡片逻辑保留。

### 8.4 第 4 步：测试

- `internal/server/server_test.go`（2 处）、`active_probe_test.go`（1 处）：内联 `Models map[string][]struct{...}` 全部改为新形状；`out.Models["vm [openai]"]` 访问改为「先按 `ID=="vm" && Protocol=="openai"` 定位模型，再取 `Endpoints`」。
- `cmd/vmr/main_test.go`：两个 mock handler 的 `models` 从 map 改为数组（`"id": "vm", "protocol": "openai", "endpoints": [...]`）；断言 `"vm [openai]"` 不变（渲染格式没变）。
- `cmd/vmr/cmd_check_quota_test.go`：两处空 `models` map -> `[]any{}`。
- 新增 `internal/server/admin_status_test.go`：
  - `TestAdminStatusModelsShapeContract`——契约测试：模型条目字段集**恰好**为 `id/protocol/capabilities/max_context_tokens/endpoints` 五键；端点允许键集封顶（出现新键即红，提示连带更新 CLI/dashboard 消费方）；`capabilities` 必须是数组（空也是 `[]` 非 null）；`max_context_tokens` 必须存在。§2.3 结论 6「JSON 即契约」的执行机制。
  - `TestAdminStatusModelsAggregateValues`——聚合语义：模型基线 `[text]` + 端点叠加 `[vision]` -> 并集 `text,vision`；模型基线 128000 + 端点覆盖 200000 -> 200000。
  - 修正：fixture 初稿误用了不存在的 `extra_capabilities` 键（严格 YAML 直接报 unknown field）--端点自声明就是 `capabilities`，`ExtraCapabilities` 只是 snapshot 里保留的展示字段。
- `internal/server/instance_test.go`：新增 `TestStatusInstanceBaseURLs`——httptest 下 `base_urls` == `http://<listener-addr>/v1/`（三协议同值）；`Host: localhost:8800` 回显 `http://localhost:8800/v1/`；`httptest.NewTLSServer` 变体验证 https scheme。

### 8.5 第 5 步：集成验证

- `go build` / `go vet ./...` / `gofmt -l` / `shellcheck vmr.sh vmr-loadtest.sh` 全部干净。
- `go test ./... -count=1` 全绿；`go test -race ./internal/server/ ./internal/core/ ./internal/diagnose/` 全绿（`cmd/vmr` 的 `-race` 曾有一次 `TestQuotaParity_CostMetric_ReportMatchesRouter` 偶发失败，单跑与全量重跑均稳定通过，与本变更无关的既有 flake）。
- `internal/archtest` 全绿（行数/函数预算、导入边界、文档引用完整性）。
- 真实实例冒烟（`listen: 127.0.0.1:18899`，模型 `coding` 基线 `[text]/128000` + 端点叠加 `[vision]/200000`）：
  - `/status` JSON：`models[0]` = `{id: "coding", protocol: "openai", capabilities: ["text","vision"], max_context_tokens: 200000, endpoints: [...含端点级字段]}`，形状与 §2.1 设计一致。
  - `base_urls` 经 `127.0.0.1:18899` 访问 -> `http://127.0.0.1:18899/v1/`；改 `Host: localhost:18899` -> `http://localhost:18899/v1/`（逐字节回显，大小写保留）。
  - `vmr status` CLI：`coding [openai]` + `capabilities: text, vision` + `max_context_tokens: 200000` + 端点行 `p0 openai/p1/gpt-test ok`。
  - `vmr diagnose`：route Group 仍为 `coding [openai]`（格式统一后输出不变）。

### 8.6 第 6 步：文档同步

- `docs/UserGuide.md` / `docs/UserGuide.zh.md`（同键同步）：`GET /status` 行补 `models` 数组语义（union/max/空=不限制）与 `base_urls` 回显语义；`vmr status` 行补 per-model capabilities/context。
- `docs/KNOWN_ISSUES_sonnet-5.md`：§2.2 在「CLI 与 Server 版本必须匹配」条目追加 `models` 形状变更受该原则覆盖一句，并新增 `base_urls` 回显决策一条；§2.4 新增「一致性靠统一口径 + 契约测试，不靠共享结构体」一条（含三处刻意不统一的理由）。
- `docs/VirtualModelRouter_Design_v4_Core.md`：检索确认无 `/status` 形状的既有记载，无需改动。

### 8.7 第 7 步：收尾

- `CHANGELOG.md` `[Unreleased]`：Added 两条（models 数组 + base_urls），Changed 一条（ModelLabel 统一）。

### 8.8 与计划的偏差

| 计划 | 实际 | 原因 |
|------|------|------|
| 任务 1.4 备选：`net.SplitHostPort` 解析 listen | 未采用（按 D4 重写后的方案执行） | base_urls 改为请求推导，`net` 导入不再需要，`admin.go` 只新增 `sort` |
| 测试 YAML 用 `extra_capabilities` | 改为 `capabilities` | 严格 YAML 下该键不存在；端点自声明键就是 `capabilities`，`ExtraCapabilities` 是 snapshot 展示字段 |
| `instance_test.go` 的 TLS 变体「如值得」 | 做了 | 成本为零（httptest.NewTLSServer 一行），且 https scheme 是 D4 的一半语义 |
| `health_test.go` 检查「如存在」 | 不存在 models 解码，未动 | 预检查表中的保守条目 |

### 8.9 最终总结

**全部落地，零遗留。** 14 个文件修改 + 2 个新文件（`admin_status_test.go`、本文档），`go test ./...`、`-race`、`archtest`、`gofmt`、`shellcheck` 全绿。

核心成果：

1. **`/status` 的 `models` 成为结构化数组**——每个虚拟模型 × 协议一条，携带 `capabilities`（跨端点并集，空=不限制）与 `max_context_tokens`（跨端点最大值，0=不限制），端点级字段同时保留。Agent 系统（如 Claude Code custom model）现在能从 `/status` 直接读出上下文长度与能力，正是本次变更的出发点。
2. **`instance.base_urls` 回显请求自身**——Host 头 + TLS 与否，`127.0.0.1` 访问见 `127.0.0.1`、`localhost` 访问见 `localhost`、局域网 IP 访问见该 IP；纯展示字段，不参与鉴权/路由。
3. **跨展示面一致性以「口径 + 契约测试」落地**——`core.ModelLabel` 锁死 `"<name> [<protocol>]"` 标签格式（diagnose/CLI 同源）；`/status` JSON 形状由契约测试锁定，形状漂移会先红在测试上而不是消费方手里；`/v1/models`、`vmr check` 分层视图、`vmr diagnose` 扁平结果按 §2.3 的论证刻意保持差异，未强行统一。
4. **遵循项目原则**——CLI 与 Server 同版本同步更新（无兼容层）；两个决策已注册进 `KNOWN_ISSUES`（§2.2 base_urls 回显、§2.4 一致性口径），后续评审者不会重新提出已 settled 的问题。

冒烟实测（真实实例 + curl + CLI + diagnose）与设计文档 §2.1/§2.2 的目标形状逐字段一致。

---

## 9. 复核记录（2026-08-26，验收 review）

> 对 §1-§8 做了一次方案 → 设计 → Action Plan → 执行记录 → 实际代码的逐项核对：读全部 diff、
> 跑 `go build`/`go vet`/`gofmt`/`shellcheck`/`go test ./...`/`go test -race`/`archtest`、
> 起真实实例做端到端冒烟（`/status` JSON、`base_urls` 换 Host 验证、`vmr status` CLI 输出）。

### 9.1 方案与设计层面

未发现绕弯路或明显疏漏。D1-D5 的取舍（map→array、capabilities 取并集、max_context_tokens
取最大值、base_urls 从请求推导、端点级保留自身值）均有清楚的理由且与 `core.Endpoint` 既有
语义（`HasCapability` 的空=不限制）一致。2.3 节对 check/diagnose/status/`/v1/models` 四个
展示面"统一口径、不强行共享结构体"的结论是对的——`vmr check` 回答"配置是什么样"，
`/status` 回答"运行时聚合后是什么样"，是两个不同问题，合并会丢语义。

### 9.2 实现与代码层面：发现并已修复 1 处真实缺陷

**问题**：`unionCapabilities`（模型级）把 `nil` 显式归一化为 `[]string{}`，但端点级
`endpointStatus.Capabilities` 在原实现中直接赋值 `ep.Capabilities`，未做同样归一化。
`core.Endpoint.Capabilities` 在端点未声明任何能力时是 `nil`（`router.mergeCapabilities`
的文档注释明确写了"nil when both are empty"），因此**没有声明能力的端点会在 JSON 里输出
`"capabilities": null`，而不是文档和代码注释都承诺的 `[]`**。

这直接违反了本次变更的核心设计承诺（D2 及 `endpointStatus` 类型的文档注释：
"capabilities ... always emitted ([] ... ) so consumers can rely on the keys"），也正好
命中本次变更服务的目标场景——外部 Agent 系统按 JSON Schema 消费这个字段时，`type: array`
校验遇到 `null` 会失败，而 `vmr status`/`status.html` 因为都用了防御性的 `len(...) > 0`
判断，恰好没有暴露这个问题，所以执行记录里的验证矩阵没有覆盖到它。

**修复**（已直接改，判断清楚、无争议）：
- `internal/server/admin.go`：新增 `nonNilStrings(s []string) []string` helper（nil → `[]string{}`），
  `unionCapabilities` 末尾与端点循环里的 `Capabilities: nonNilStrings(ep.Capabilities)` 都改用它。
- `internal/server/admin_status_test.go`：`TestAdminStatusModelsShapeContract` 补充断言——
  该测试的 fixture 本就没给端点声明 capabilities，之前只检查键"存在"（`ok`），`null` 值下
  `ok` 依然为真，所以测试本身不会红；现在改为类型断言 `ep["capabilities"].([]any)`，
  锁死"必须是数组、不能是 null"，回归会在这里先炸。
- 已验证：临时还原掉修复后重跑该测试确实红（`models[0].endpoints[0].capabilities = <nil>`），
  说明新断言是有效的回归防线，不是摆设。
- 端到端复测：`go build`/`go vet`/`gofmt -l`/`shellcheck`/`go test ./...`/
  `go test -race ./internal/server/... ./internal/core/... ./internal/diagnose/...`/
  `go test ./internal/archtest/...` 全绿；真实实例冒烟重新跑过一遍，`base_urls` 换
  `Host: localhost` 复现正确。

### 9.3 交叉核查确认无遗漏

- `/status` JSON 的所有历史消费方（`server_test.go`、`active_probe_test.go`、`main_test.go`、
  `cmd_check_quota_test.go`、`cmd_status.go`/`cmd_status_render.go`、`status.html`）全部
  搜索确认已同步，没有漏改的 `snap.Models`/`"models"` 引用（`grep` 全仓库核对，其余命中
  均为 config 侧 `cfg.Models`/router 内部 `snap.Models`，与 `/status` JSON 无关）。
- `vmr.sh` 未直接解析 `.models`（`grep` 确认零命中），§6 风险评估里"`vmr.sh ps` 无影响"
  的结论成立。
- `docs/VirtualModelRouter_Design_v4_Core.md` 里 `/status` 那一行是概括性描述，未列出
  `models` 具体形状或 `base_urls` 字段，属于文档既有的抽象粒度，不需要跟随本次改动。
- `extra_capabilities` 只在 `vmr check` 自己的输出标签与其测试里出现（合法），未在任何
  config YAML 或本次新增 fixture 里残留。
- `core.ModelLabel` 的 3 处消费方（`vmr diagnose`、`vmr status` CLI、`status.html` 内联
  JS 字符串）逐一核对格式一致，`status.html` 那处虽是 JS 内联、没法直接调用 Go 函数，
  但格式由 `main_test.go` 里 `"vm [openai]"` 的断言间接兜底，符合 §4 任务 4.5 的设计。

### 9.4 结论

**无遗留的大问题需要人工决策**。除上述已直接修复的 1 处小缺陷外，方案设计合理、
Action Plan 执行到位、代码与设计文档逐字段吻合，可以提交。
