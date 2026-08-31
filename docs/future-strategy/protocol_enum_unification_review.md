# VMR 协议名枚举统一 —— 最终方案 & Detailed Action Plan

<!-- Date: 2026-09-14 | Status: 最终方案（本篇为本轮分析结论，后续按计划执行） -->

## 0. 决策（已确认）

1. **集中定义协议常量**，消除 20 个生产文件中的散落字符串。
2. **常量值改为与 Pi Agent 等主流工具兼容的写法**：`openai` → `openai-completions`，`anthropic` → `anthropic-messages`，`openai-responses` 保持不变。
3. **全链路更新**：所有代码、配置、文档、测试统一使用新名。
4. **新日志 / 新审计记录写新协议名**，路由侧零兼容负担。
5. **历史审计日志兼容只在分析侧处理**——通过 `audit.Record` 的 `UnmarshalJSON` 归一化钩子，读取旧记录时自动将 `"openai"` → `"openai-completions"`、`"anthropic"` → `"anthropic-messages"`。

---

## 1. 前置审查：关键细微处验证

在规划 Action Plan 前，我按实际代码核准了方案可行性，发现以下必须纳入计划的细节：

### 1.1 审计日志的读取咽喉点不是一处，而是两处，还有一处缓存

| 读取路径 | 解码方式 | 是否受 `UnmarshalJSON` 保护 |
|---|---|---|
| `ctxgraph.ScanCached` → `scan.go:138` `json.Unmarshal(lineBytes, &rec)` | 使用 `audit.Record` 结构体 | ✅ 会触发 `UnmarshalJSON` |
| `report/aggregate.scanAndCacheFile` → `aggregate.go:285` `json.Unmarshal(lineBytes, &arec)` | 使用 `audit.Record` 结构体 | ✅ 会触发 `UnmarshalJSON` |
| `replay` → `replay.go:454/498` `json.Unmarshal(lb, &rv)` | 使用 `replay.recordV1` 或 `replay.recordV2` 结构体（**非** `audit.Record`） | ⚠️ **不受 `audit.Record.UnmarshalJSON` 保护** |
| 事实缓存（`ctxgraph.CachedFile.Facts`） | `json.Unmarshal` 到 `recordFacts`（纯结构体，非 `audit.Record`） | ⚠️ **不受 `UnmarshalJSON` 保护** |

**结论：**
- `audit.Record.UnmarshalJSON` 能保护分析侧（report/story/reqdetail/chatmsg/ctxgraph）的读取路径，这是用户的决策范围 ✅
- **`vmr replay` 也读审计日志**（`replay.go:454` 解码 `recordV2`，`replay.go:498` 解码 `recordV1`），且 `replay` 的 `Record := struct { Protocol string }` 是独立结构体。如果要对历史记录运行 replay，必须在 replay 的 decode 路径也做兼容。**建议在 replay 的两次 decode 处也调用 `core.CanonicalProtocol` 归一化。**
- **事实缓存（`recordFacts`）** 存储了 `Protocol` 和 `Endpoint` 字段。旧缓存中有旧协议名。建议：**bump `ctxgraph.CacheSchemaVersion` 从 1 到 2**，使旧缓存自动失效，第一次运行重扫后重建。

### 1.2 `Attempts[].Endpoint` 标签（`protocol:provider:model`）需要连带归一化

`Endpoint` 字段是单字符串 `"openai:acct:model"`，分析侧通过 `core.SplitEndpointLabel` 解析出 protocol 段。`UnmarshalJSON` 必须在归一化 `Protocol` 的同时，也归一化 `Endpoint` 字符串中的 protocol 段。

需要在 `internal/core` 增加：
```go
func NormalizeEndpointLabel(label string) string {
    proto, provider, model, ok := SplitEndpointLabel(label)
    if !ok { return label }
    return CanonicalProtocol(proto) + ":" + provider + ":" + model
}
```
在 `audit.Record.UnmarshalJSON` 中对每个 `Attempts[i].Endpoint` 调用此函数。

### 1.3 `i18n/story_corpus.go` 中有协议名的自然语言引用

`anthropicCoverageNote` 的文本模板中引用 `"Anthropic"` 作为协议名。重命名后，`protocolShare["anthropic"]` 变为 `protocolShare["anthropic-messages"]`，需要一并更新。

### 1.4 常量集中化本身是措施，不是目标；本次落地时「一步到位」直接写新常量名

不先定义旧值常量再全局替换，而是直接在 `internal/core` 定义新常量：
```go
const (
    ProtocolOpenAICompletions = "openai-completions"
    ProtocolAnthropicMessages = "anthropic-messages"
    ProtocolOpenAIResponses   = "openai-responses"
)
```
然后所有代码从散落字面量改为引用这些常量，同时字面量值就是新名。**零中间过渡态。**

---

## 2. Detailed Action Plan（分 Phase 执行）

### Phase 0：常量定义 + 兼容函数 + 缓存版本（internal/core, internal/ctxgraph）

**改什么：**
- `internal/core/core.go` 或新增 `internal/core/protocol.go`：
  ```go
  package core

  // 协议枚举（集中定义，全链路引用）。
  const (
      ProtocolOpenAICompletions = "openai-completions"
      ProtocolAnthropicMessages = "anthropic-messages"
      ProtocolOpenAIResponses   = "openai-responses"
  )

  // CanonicalProtocol 将历史日志中的旧协议名归一化到当前枚举值。
  // 仅用于读历史审计记录时的兼容咽喉点，绝不在写路径调用。
  func CanonicalProtocol(p string) string {
      switch p {
      case "openai":      return ProtocolOpenAICompletions
      case "anthropic":   return ProtocolAnthropicMessages
      default:            return p
      }
  }

  // NormalizeEndpointLabel 归一化 endpoint label 中的 protocol 段。
  func NormalizeEndpointLabel(label string) string {
      proto, provider, model, ok := SplitEndpointLabel(label)
      if !ok { return label }
      return CanonicalProtocol(proto) + ":" + provider + ":" + model
  }
  ```
- `internal/ctxgraph/cache.go`：`CacheSchemaVersion = 1` → `CacheSchemaVersion = 2`（使旧事实缓存自动失效）。

**验证：**
- `go build ./internal/core/...`。
- `go test ./internal/core/...`。
- `CanonicalProtocol("openai")` 返回 `"openai-completions"`；`NormalizeEndpointLabel("openai:acct:model")` 返回 `"openai-completions:acct:model"`。

---

### Phase 1：审计记录解码归一化（internal/audit）

**改什么：**
- `internal/audit/audit.go`：为 `Record` 添加 `UnmarshalJSON` 方法：
  ```go
  func (r *Record) UnmarshalJSON(data []byte) error {
      type RecordAlias Record // 避免无限递归
      alias := (*RecordAlias)(r)
      if err := json.Unmarshal(data, alias); err != nil {
          return err
      }
      r.Protocol = core.CanonicalProtocol(r.Protocol)
      for i := range r.Attempts {
          r.Attempts[i].Protocol = core.CanonicalProtocol(r.Attempts[i].Protocol)
          r.Attempts[i].Endpoint = core.NormalizeEndpointLabel(r.Attempts[i].Endpoint)
      }
      return nil
  }
  ```
- `internal/audit/audit.go`：`Record.Protocol` 字段注释更新为新枚举名。

**验证：**
- `go build ./internal/audit/...`。
- `go test -run TestRecordUnmarshalJSON ./internal/audit/...`（新增测试：用旧名 `"openai"` 的 JSON 反序列化，断言 `rec.Protocol == "openai-completions"`；旧名 `"anthropic"` 同理；旧名 `"openai:acct:model"` 的 Endpoint 标签归一化）。
- 运行 `go test ./internal/audit/...` 全绿。

---

### Phase 2：Adapter 注册名更新（3 个文件）

**改什么：**
- `internal/adapter/openai/openai.go`：
  - `Register("openai-completions", OpenAI{})`
  - `func (OpenAI) Protocol() string { return "openai-completions" }`
- `internal/adapter/anthropic/anthropic.go`：
  - `Register("anthropic-messages", Anthropic{})`
  - `func (Anthropic) Protocol() string { return "anthropic-messages" }`
- `internal/adapter/openairesponses/openairesponses.go`：不变（`"openai-responses"` 已是最终名）。
- `internal/adapter/adapter.go`：`Protocol()` 注释更新。

**验证：**
- `go build ./internal/adapter/...`。
- `adapter.Names()` 返回 `["anthropic-messages", "openai-completions", "openai-responses"]`（字母序）。
- 运行 `go test ./internal/adapter/...`。

---

### Phase 3：HTTP 路由 + 服务器表面（4 个文件）

**改什么：**
- `internal/server/server.go`：
  - `mux.HandleFunc("POST /v1/chat/completions", s.chatHandler("openai-completions"))`
  - `mux.HandleFunc("POST /v1/messages", s.chatHandler("anthropic-messages"))`
  - `mux.HandleFunc("POST /v1/responses", s.chatHandler("openai-responses"))`（不变）
  - `/v1/models` 的 `vmr_protocol` 字段输出新名。
- `internal/server/admin.go`：
  - `instanceBaseURLs` 的 map key：`"openai-completions"`、`"anthropic-messages"`、`"openai-responses"`
  - 注释更新。
- `internal/server/help.go`：
  - `baseURLs["openai-completions"]`、`baseURLs["anthropic-messages"]`、`baseURLs["openai-responses"]`
- `internal/server/help.html`：
  - 协议徽章：`openai` → `openai-completions`、`anthropic` → `anthropic-messages`、`responses` → `openai-responses`
  - 速查表 Protocol 列：所有 badge-class 和显示文本更新。
  - 「OpenAI Protocol」小节标题 → 「OpenAI Completions Protocol」
  - 各 agent 代码块中对协议的文字说明更新。

**验证：**
- `go build ./internal/server/...`。
- `go test -run TestStatusPage ./internal/server/...`（断言 `/status.html` 包含新协议名显示）。
- `go test -run TestHelpPage ./internal/server/...`（断言 `/help.html` 包含新协议名徽章）。
- 冒烟：`curl /v1/models` 的 `vmr_protocol` 字段输出新名；`curl /status` 的 `base_urls` key 输出新名。

---

### Phase 4：运行时协议分支（8 个文件）

**改什么（每个文件都是 `case "openai"` → `case "openai-completions"`、`case "anthropic"` → `case "anthropic-messages"` 的类似替换）：**

| 文件 | 替换内容 | 特别注意事项 |
|---|---|---|
| `internal/respnorm/respnorm.go` | `protocol == "openai"`、`protocol == "openai-responses"`、`protocol == "anthropic"` | 约 660-680 行的"openai-responses 以 openai 开头"保护：改名后 `openai-completions` 与 `openai-responses` 前缀不再重合，这段 `strings.HasPrefix` 逻辑需要重新验算（可能简化，但必须确认无遗漏） |
| `internal/imgprep/imgprep.go` | `protocol == "openai"`、`protocol == "anthropic"`、`protocol == "openai-responses"` | 三个 protocol 各自对应不同 content-block 形状 |
| `internal/router/router.go` | `case "anthropic"`、`case "openai-responses"`、注释 | 注意 `default` 分支处理 `"openai"` 之外的协议——改名后 `"openai"` 变成 `"openai-completions"`，`default` 分支不受影响 |
| `internal/router/probe.go` | `ep.AdapterType == "openai-responses"` | 不变（已是新名） |
| `internal/router/telemetry.go` | `case "openai"`、`case "anthropic"`、`case "openai-responses"`；`by_protocol` map 初始化 key | `snap.Requests.ByProtocol` 的 map key 也变，Dashboard 显示时 `reqs.by_protocol.openai` 需改为 `reqs.by_protocol["openai-completions"]` |
| `internal/probe/probe.go` | 注释 | 更新注释中的协议名 |
| `internal/diagnose/diagnose.go` | `case "openai"`、`case "openai-responses"`、`ep.AdapterType == "openai"` | 两处 decode 后的协议分支 |
| `cmd/vmr/cmd_smoke.go` | `case "anthropic"`、`case "openai-responses"` | smoke 请求体的协议分支 |

**验证：**
- `go build ./...`。
- `go test ./internal/respnorm/...` `go test ./internal/imgprep/...` `go test ./internal/router/...` `go test ./internal/probe/...` `go test ./internal/diagnose/...` `go test ./cmd/vmr/...`（全绿，且 -race 干净）。
- 特别注意 `respnorm/respnorm.go` 中 `openai-responses` 前缀判断的测试覆盖。

---

### Phase 5：分析侧 Protocol 分支 + 显示名引用（6 个文件 + 1 个测试）

**改什么：**
- `internal/report/section_reliability.go`：
  - `rank` 函数的 `case "openai"` → `case "openai-completions"`、`case "anthropic"` → `case "anthropic-messages"`。
  - `protocolBuckets` 注释更新。
- `internal/story/corpus_coverage.go`：
  - `protocolShare["anthropic"]` → `protocolShare["anthropic-messages"]`
  - `journeyAnthropicCoverageNote` 同理。
  - 注释更新。
- `internal/report/factscache.go`：`extractRecordFacts` 中 `arec.Protocol` 经 Phase 1 的 UnmarshalJSON 已归一化，无需额外改动。但注释更新。
- `internal/story/modelusage.go`：`core.SplitEndpointLabel` 解析 Endpoint，经 Phase 1 归一化，无需额外改动。
- `internal/reqdetail/facts.go`：同上。
- `internal/reqdetail/detail.go`：同上。
- `internal/i18n/story_corpus.go`：`AnthropicOnlyCoverageNote` 叙事文本中的 `"Anthropic"` 协议名引用，确保文本与 `"anthropic-messages"` 对应（建议保持显示名 `"Anthropic"` 不变，只改内部引用，但需确认）。

**验证：**
- `go test ./internal/report/...`（特别是 `TestSectionReliability` 等）。
- `go test ./internal/story/...`（特别是 `TestCorpusCoverage` 等）。
- `go test ./internal/reqdetail/...`。
- 新增差分测试：构造一份包含旧协议名 `"openai"` 的 JSONL fixture，跑 `vmr report` 断言分组正确。

---

### Phase 6：Dashboard + Help 页前端 JS（2 个文件）

**改什么：**
- `internal/server/status.html`：
  - `protoNames` 映射：`'openai'` → `'OpenAI'` 变为 `'openai-completions'` → `'OpenAI Completions'`（或简化为 `'OpenAI'`——取决于你想要显示名）。
    - 建议：显示名改用 `'OpenAI Completions'`、`'Anthropic Messages'`、`'OpenAI Responses'`，以匹配生态标准。
  - `badgeClass` 判断：`m.protocol === 'anthropic'` → `m.protocol === 'anthropic-messages'`。
  - `ep.protocol` 显示（端点表格协议列）：`ep.protocol` 值已变为新名，可直接显示。
  - Dashboard 顶部「Connect Your Agent」卡片中协议列表的展示也需更新。
- `internal/server/help.html`：
  - 协议徽章：`badge-primary` 标签 `openai` → `openai-completions`、`badge-anthropic` 标签 `anthropic` → `anthropic-messages`。
  - 速查表 Protocol 列中所有 `openai`/`anthropic` 引用。
  - 各协议节标题：「OpenAI Protocol」→「OpenAI Completions」等。
  - Pi Agent 示例代码块中的 `"api"` 字段是 Pi 的 `api` 值（`openai-completions`），与 VMR 的 protocol 是不同概念——但恰好值相同，所以**奉旨保持不变**（Pi 的 `api: openai-completions` 与 VMR 新名一致）。
  - 注意：`help.html` 中 `"openai"` 作为纯文字引用（如 `badge badge-primary` 标签）和作为 `/status` 数据字段引用（如 `m.protocol`）两处都需要更新。

**验证：**
- `go test -run TestStatusPage ./internal/server/...`（断言 Dashboard HTML 包含新协议名）。
- `go test -run TestHelpPage ./internal/server/...`（断言 Help HTML 包含新协议名徽章）。
- 手动冒烟：打开 `/status.html` 和 `/help.html`，肉眼核对协议显示名。

---

### Phase 7：配置文件（7 个文件）

**改什么：**
| 文件 | 改动 |
|---|---|
| `config.example.yaml` | 所有 `base_url: {openai: ...}` → `{openai-completions: ...}`；`protocol: openai` → `protocol: openai-completions`；`protocol: anthropic` → `protocol: anthropic-messages`；头部注释更新 |
| `config.example.zh.yaml` | 同上（同步翻译） |
| `config.local.yaml` | 同上 |
| `config.mba.yaml` | 同上 |
| `config.yaml` | 同上（实际运行配置） |
| `internal/config/config.go` | 校验错误信息中的 `available: %v` 自动变为新名（因为 `adapter.Names()` 变了），无需改校验逻辑；但需更新注释中的协议名 |
| `internal/config/provider.go` | 注释更新 |

**验证：**
- `./vmr check -c config.yaml` 通过，routing table 显示新协议名。
- `go test ./internal/config/...` 全绿。

---

### Phase 8：文档（4 个文件 + docs/ 目录）

**改什么：**
| 文件 | 改动 |
|---|---|
| `docs/UserGuide.md` | 所有 `openai` / `anthropic` / `openai-responses` 协议表述 → 新名；配置示例块中的 `protocol:` 值；端点路径对应关系说明 |
| `docs/UserGuide.zh.md` | 同步翻译更新 |
| `docs/VirtualModelRouter_Design_v4_Core.md` | 设计文档中的协议枚举定义、示例、决策说明（「当前状态，不是 changelog」——整体替换） |
| `docs/VirtualModelRouter_Design_v4_Analytics.md` | 同步更新 |

**验证：**
- 肉眼核对所有文档中的协议名均已更新。
- `prose` lint 或无硬性要求；但确保 `.zh` 与英文文档同步修改。

---

### Phase 9：测试（65 个文件，400 处字面量）

**策略：**
1. 将测试中的协议字面量**替换为引用 Phase 0 定义的常量**（`core.ProtocolOpenAICompletions` 等），而非继续硬编码新字符串。这样未来再改枚举值只需改常量定义，不波及测试。
2. 按 Phase 1-8 的优先级，当每个 Phase 改完生产代码后，同步更新对应测试文件。
3. 新增兼容性测试：
   - `internal/audit/audit_test.go`：旧协议名 JSON 反序列化测试。
   - `internal/report/`：旧协议名 fixture 的 `vmr report` 差分测试。
   - `internal/replay/`：旧协议名 fixture 的 `vmr replay` 差分测试（若决定给 replay 也加归一化）。

**覆盖范围：** 以下 65 个测试文件中的协议字面量需全部更新：
- `internal/server/*_test.go`（~15 个文件）
- `internal/router/*_test.go`（~10 个文件）
- `internal/config/*_test.go`（~5 个文件）
- `internal/respnorm/*_test.go`（~3 个文件）
- `internal/imgprep/*_test.go`（~2 个文件）
- `internal/audit/*_test.go`（~3 个文件）
- `internal/report/*_test.go`（~8 个文件）
- `internal/story/*_test.go`（~8 个文件）
- `internal/replay/*_test.go`（~3 个文件）
- `internal/diagnose/*_test.go`、`internal/probe/*_test.go`、`internal/adapter/*_test.go`、`internal/core/*_test.go`、`cmd/vmr/*_test.go` 等

**验证：**
- `go test -race ./...` 全绿。
- 新增的兼容性测试通过。

---

### Phase 10：Replay 的历史记录兼容（1 个文件，可选但建议）

**改什么：**
- `internal/replay/replay.go`：在 `json.Unmarshal(lb, &rv)` 解码后，对 `rv.Protocol` 调用 `core.CanonicalProtocol` 归一化：
  ```go
  // replay.go:454
  if err := json.Unmarshal(lb, &rv); err != nil { ... }
  rv.Protocol = core.CanonicalProtocol(rv.Protocol)
  // replay.go:498
  if err := json.Unmarshal(lb, &rv); err != nil { ... }
  rv.Protocol = core.CanonicalProtocol(rv.Protocol)
  ```
  `recordV1`/`recordV2` 结构体中的 `Protocol` 字段同样归一化。

**说明：** replay 不是分析侧，但也会读历史审计记录。用户明确说"只在分析侧做兼容"，但如果 replay 历史记录的需求存在，建议在 replay 中也加归一化。如果确定不需要 replay 历史记录，可以跳过此 Phase。

**验证：**
- 构造旧协议名的 JSONL fixture，用 `vmr replay -req COORD -print` 断言能正确读取并重放。

---

### Phase 11：完整验收

| 步骤 | 命令 / 方法 | 预期 |
|---|---|---|
| 编译 | `go build ./...` | 零错误 |
| 静态检查 | `go vet ./...`、`gofmt -l .` | 零错误 |
| 架构约束 | `go test ./internal/archtest/...` | 通过（若有新文件超出预算，允许调整预算） |
| 全量测试 | `go test -race ./...` | 全绿 |
| 配置校验 | `./vmr check -c config.yaml` | 通过，routing table 显示新协议名 |
| 冒烟路由 | `./vmr start -c config.yaml` 后 `curl /v1/chat/completions` | 正常转发 |
| 冒烟分析 | `./vmr analyze` | 正常产出报告 |
| 旧日志兼容 | 用旧名 JSONL fixture 跑 `vmr report` | 分组、占比、排序正确 |
| 仪表盘 | 浏览器打开 `/status.html` | 协议显示名统一为生态名 |
| 帮助页 | 浏览器打开 `/help.html` | 协议徽章与速查表统一 |
| API 检查 | `curl /v1/models`、`curl /status` | `vmr_protocol`、`base_urls`、`protocol` 字段输出新名 |
| 文档 | 肉眼核对 4 个文档文件 | 所有协议名引用已更新 |
| 中文文档 | 核对 `.zh` 文档 | 与英文文档同步，未见遗漏 |

---

## 3. 执行顺序建议

建议按 Phase 0 → 2 → 1 → 3 → 4 → 5 → 7 → 6 → 8 → 9 → 11 的顺序执行，原因：

- **Phase 0**（常量定义）是前提，所有后续 Phase 依赖它。
- **Phase 2**（adapter 注册）是"协议名从哪来"，改了之后 `adapter.Names()` 返回新名，config 校验自动生效——先改它，后面的 config 改动才能验证。
- **Phase 1**（UnmarshalJSON 归一化）应在测试之前完成，因为测试可能涉及读旧日志 fixture。
- **Phase 3-4**（HTTP + 分支）独立于 5-6，可并行。
- **Phase 5**（分析侧）必须在 Phase 1 之后（依赖归一化）。
- **Phase 7**（配置文件）必须在 Phase 2 之后（依赖 adapter 新名）。
- **Phase 6**（前端）在 Phase 3 之后（依赖 `/status` 新字段名）。
- **Phase 8**（文档）与实现无关，可并行但建议最后通读一遍确保无遗漏。
- **Phase 9**（测试）贯穿始终，每个 Phase 完成后同步更新对应测试。

---

## 4. 变更清单速查表（一键索引）

| Phase | 文件 | 核心改动 |
|---|---|---|
| 0 | `internal/core/core.go`（或新 `protocol.go`） | 新增 `Protocol*` 常量 + `CanonicalProtocol` + `NormalizeEndpointLabel` |
| 0 | `internal/ctxgraph/cache.go` | `CacheSchemaVersion` 1 → 2 |
| 1 | `internal/audit/audit.go` | `Record.UnmarshalJSON` 归一化 hook |
| 2 | `internal/adapter/openai/openai.go` | `Register("openai-completions")` + `Protocol()` |
| 2 | `internal/adapter/anthropic/anthropic.go` | `Register("anthropic-messages")` + `Protocol()` |
| 2 | `internal/adapter/adapter.go` | 注释 |
| 3 | `internal/server/server.go` | `chatHandler` 参数 + `vmr_protocol` |
| 3 | `internal/server/admin.go` | `instanceBaseURLs` map key |
| 3 | `internal/server/help.go` | `baseURLs` map key |
| 3 | `internal/server/help.html` | 协议徽章 + 速查表 + 节标题 |
| 4 | `internal/respnorm/respnorm.go` | 协议分支 case + 前缀检查 |
| 4 | `internal/imgprep/imgprep.go` | 协议分支 case |
| 4 | `internal/router/router.go` | 协议分支 case |
| 4 | `internal/router/probe.go` | 协议分支 case |
| 4 | `internal/router/telemetry.go` | 协议分支 + `by_protocol` map key |
| 4 | `internal/probe/probe.go` | 注释 |
| 4 | `internal/diagnose/diagnose.go` | 协议分支 case |
| 4 | `cmd/vmr/cmd_smoke.go` | 协议分支 case |
| 5 | `internal/report/section_reliability.go` | `rank` case + `endpointProtocol` |
| 5 | `internal/story/corpus_coverage.go` | `share["anthropic-messages"]` + 注释 |
| 5 | `internal/i18n/story_corpus.go` | 叙事文本引用更新 |
| 6 | `internal/server/status.html` | `protoNames` map + `badgeClass` + 协议显示 |
| 6 | `internal/server/help.html` | 协议徽章 + 速查表 |
| 7 | `config.example.yaml` | `protocol: openai-completions` 等 |
| 7 | `config.example.zh.yaml` | 同步 |
| 7 | `config.local.yaml` | 同步 |
| 7 | `config.mba.yaml` | 同步 |
| 7 | `config.yaml` | 同步 |
| 7 | `internal/config/config.go` | 注释 + 校验错误信息（自动） |
| 7 | `internal/config/provider.go` | 注释 |
| 8 | `docs/UserGuide.md` | 全量替换 |
| 8 | `docs/UserGuide.zh.md` | 同步 |
| 8 | `docs/VirtualModelRouter_Design_v4_Core.md` | 全量替换 |
| 8 | `docs/VirtualModelRouter_Design_v4_Analytics.md` | 全量替换 |
| 9 | 65 个测试文件 | 字面量 → 常量引用 |
| 10 | `internal/replay/replay.go` | 两次 decode 后的 `CanonicalProtocol`（可选） |

---

## 5. 风险与待确认

1. **replay 历史记录兼容（Phase 10）**：用户明确说"只在分析侧做兼容"。如果 `vmr replay` 只需要读新日志，则 Phase 10 可跳过；如果需要读旧日志（如回放历史失败请求），则 replay 也需要归一化。**请确认：replay 是否需要读历史审计记录？**
2. **`respnorm/respnorm.go` 的 `openai-responses` 前缀判断**：改名后 `openai-completions` 与 `openai-responses` 不再共享 `"openai"` 前缀，`strings.HasPrefix` 旧逻辑变得多余。但这部分逻辑可能与其他保护交织，**改时必须重新验算，不能简单删除**。
3. **事实缓存一次性失效**：bump `CacheSchemaVersion` 会让第一次 `vmr analyze`/`report` 重扫所有文件，耗时可能较长（取决于日志量）。这是**一次性**代价，后续运行恢复正常。
4. **`i18n/story_corpus.go` 的叙事文本**：`AnthropicOnlyCoverageNote` 的文本中 `"Anthropic"` 是自然语言显示名，不是内部枚举值。建议保持显示名 `"Anthropic"` 不变，只改内部引用——但需确认无冲突。
---

## 6. 执行记录（2026-08-27，by Sonnet 5）

### 6.1 用户反馈与方案最终调整

- **Replay 完全不兼容**：`vmr replay` 不读历史日志、不基于旧枚举回放，整功能只认新枚举值 → **Phase 10 整个删除**，Phase 9 的 replay 差分测试删除，风险项 #1 关闭。
- **兼容面收敛**：只在解析历史 Agent 审计日志文件时生效，即仅 `audit.Record.UnmarshalJSON` 一处 + `ctxgraph.CacheSchemaVersion` 失效重建。约一个月过渡期，之后拆除。
- **落地前复核发现的方案缺陷**：
  1. **方案漏了 `internal/adapter/fingerprint.go`**：`SessionFingerprint()` 有 `case "openai"`/`case "anthropic"`，由 `router.go` 传入 adapter-type 驱动。漏改会导致改名后 sticky-session 指纹匹配全部 fallthrough → prompt-cache 会话粘性静默失效（路由回归，非文案）。已纳入并连带 `fingerprint_test.go`/`fingerprint_fuzz_test.go`。
  2. **`internal/pricing/resolve.go:78` 是误报**：那里的 `"anthropic"` 是上游 vendor 名（注释里与 `"deepseek"` 并列），非协议名，未改动。
  3. **`respnorm.go` 的前缀风险被高估**：`appendDone()` 是 `s.protocol != "openai"` 字面比较，无 `HasPrefix`。改名后是注释重写 + 字面量替换，非逻辑坑。
  4. **`NormalizeEndpointLabel` 改为只改前导 token**：方案原写法用 `SplitEndpointLabel` 重组会把历史 `/` 分隔标签强制转成 `:` 分隔（详单渲染的既存格式会变）。改为只替换协议段、分隔符与其余字节原样，协议名已是新名时直接原样返回。

### 6.2 确认项（用户已拍板）

| 项 | 决定 |
|---|---|
| 脏工作树（help 页 WIP） | 由本次先提交为独立 commit（`dc75f42`），再在干净树上做改名 |
| 改名提交粒度 | 一个大 commit |
| 前端协议显示名 | `OpenAI Completions` / `Anthropic Messages` / `OpenAI Responses` |

### 6.3 实际落地内容

**Phase 0 — 常量 + 兼容函数 + 缓存版本**
- 新增 `internal/core/protocol.go`：`ProtocolOpenAICompletions`/`ProtocolAnthropicMessages`/`ProtocolOpenAIResponses` 常量 + `CanonicalProtocol` + `NormalizeEndpointLabel`（均带 `TODO(2026-10)` 拆除标记）。
- `internal/ctxgraph/cache.go`：`CacheSchemaVersion` 1 → 2。

**Phase 1 — 审计解码归一化**
- `internal/audit/audit.go`：`Record.UnmarshalJSON` 归一化 `Protocol` + `Attempts[].Protocol` + `Attempts[].Endpoint`；字段注释更新。
- 新增 `TestRecordUnmarshalJSON_NormalizesLegacyProtocolNames`（含 `/` 分隔标签保分隔符断言 + 新名 round-trip 断言）。

**Phase 2 — Adapter 注册**
- `openai/openai.go`、`anthropic/anthropic.go`、`openairesponses/openairesponses.go` 全部改为引用 `core.Protocol*` 常量（`Register` + `Protocol()`）。
- `adapter/adapter.go` 注释、`adapter/fingerprint.go` 三处分支（方案外补漏）。

**Phase 3 — HTTP / 服务器表面**
- `server/server.go`：`chatHandler(core.Protocol*)`、`/v1/models` 注释。
- `server/admin.go`：`instanceBaseURLs` map key 改常量。
- `server/help.go`：`baseURLs[core.Protocol*]` + 新增 `core` import。

**Phase 4 — 运行时协议分支**（全部改为 `core.Protocol*` 常量，新增 `core` import 到 `respnorm`/`imgprep`/`router/telemetry`）
- `respnorm/respnorm.go`（含 `appendDone` 长注释重写）、`imgprep/imgprep.go`、`router/router.go`（`IngressPath`）、`router/probe.go`、`router/telemetry.go`（`RecordRequest` + `ByProtocol` map key）、`diagnose/diagnose.go`、`cmd/vmr/cmd_smoke.go`。`probe/probe.go` 无旧名，跳过。

**Phase 5 — 分析侧**
- `report/section_reliability.go`：`protocolBuckets` 的 `rank()` + 注释 + `core` import。
- `story/corpus_coverage.go`：`protocolShare[core.ProtocolAnthropicMessages]` + 注释 + `core` import。
- `i18n/story_corpus.go`：确认 `"Anthropic"` 为自然语言显示名，**不改**。
- 新增 `TestBuild_LegacyProtocolNamesNormalized`（旧名 JSONL fixture 跑 `report.Build` 断言归一化）。

**Phase 6 — 前端 JS**
- `server/status.html`：`protoNames` map（新显示名）、`badgeClass`、`baseURLs['openai-completions'/'anthropic-messages']`、`by_protocol` key、Base URL 卡片徽章、metric title。
- `server/help.html`：协议徽章（`badge-primary`/`badge-anthropic` 内文本）、节标题（`OpenAI Completions Protocol` 等）、Base URL 卡片徽章、`m.protocol` 默认值与 `badgeClass`。**未动** agent 自身配置值（如 Continue.dev `"provider": "openai"`、Aider `openai-api-base:`）与 Pi Agent `"api": "openai-completions"`（本就一致）。

**Phase 7 — 配置文件**
- `config.example.yaml`、`config.example.zh.yaml`、`config.local.yaml`、`config.mba.yaml`、`config.yaml`：`protocol:` 值、`base_url` map key（inline `{...}` 与多行两种形式）、注释。
- `internal/config/config.go`、`internal/config/provider.go`：注释。
- 校验逻辑无需改（`adapter.Names()` 自动生效）。

**Phase 8 — 文档**
- `docs/UserGuide.md` / `.zh.md`、`docs/VirtualModelRouter_Design_v4_Core.md`（含设计决策表、审计日志示例、诊断章节）、`README.md` / `.zh.md`（报告示例端点标签）。
- `KNOWN_ISSUES` 新增条目登记兼容取舍 + `TODO(2026-10)` 拆除清单；语料构成图例更新。
- `CHANGELOG.md` `[Unreleased]`：协议改名条目 + `/help` 页条目。

**Phase 9 — 测试**（约 80 个 `_test.go` 文件，451 处字面量）
- 策略：协议值字面量替换为新字符串（非全部换常量——一个月后兼容层拆除，未来再改枚举可能性低，且换常量对 65+ 文件是更大改动面）。
- 关键区分（未误改）：pricing 的 vendor 名 `"anthropic"`（`RateFor`/`ProviderByName`/canonical key `anthropic/claude-...`）、provider 名 `"anthropic"`、`splitEndpointLabel` 的 `/` 分隔 legacy 用例、agent 自身配置值。
- 端点标签断言（`X-VMR-Endpoint` header `openai/p/m`、`endpoint: openai:p:m`、diagnose 的列对齐 padding）逐一按新名重算。

### 6.4 验收结果

| 项 | 命令 | 结果 |
|---|---|---|
| 编译 | `go build ./...` | ✅ 零错误 |
| 静态检查 | `go vet ./...`、`gofmt -l internal/ cmd/` | ✅ 零输出 |
| 架构约束 | `go test ./internal/archtest/...` | ✅ 通过（无需调整预算） |
| 全量测试 | `go test ./...` | ✅ 35 包全绿 |
| 竞态 | `go test -race ./internal/{router,audit,health,sticky,quota,server,respnorm}/... ./cmd/vmr/...` | ✅ 全绿 |
| 配置校验 | `./vmr check -c config.yaml` / `config.example.yaml` / `config.example.zh.yaml` / `config.mba.yaml` | ✅ `=== OK ===`，routing table 显示新协议名 |
| 分析冒烟 | `./vmr analyze` | ✅ 缓存 v2 重扫 43 文件后正常产出 report + stories（首次重扫耗时数分钟，一次性代价） |
| 新增兼容测试 | `TestRecordUnmarshalJSON_*`、`TestBuild_LegacyProtocolNamesNormalized` | ✅ 通过 |

**已知无关问题**：`config.local.yaml` 校验失败（`field provider not found in type config.EndpointGroup`）—— 该文件在本次改动**之前**就已失效（用了已废弃的单数 `provider:` key），非本次回归，未处理。

### 6.5 提交

- `dc75f42` feat(server): add /help agent configuration guide + dashboard Connect card（WIP 前置提交）
- 改名主提交：112 文件，`internal/core/protocol.go` 新增，一个 commit。

---

## 7. 方案与落地情况全面 Review（基于 Commit #793912）

<!-- Date: 2026-08-27 | Reviewer: Claude -->

### 7.1 前期方案分析与问题真实性验证

对比方案制定阶段的假设与实际代码实现，对前期指出的几个关键问题进行定性与复核：

1. **事实缓存失效（CacheSchemaVersion 1→2）—— 结论：真实成立，执行到位**
   - **分析**：`ctxgraph.recordFacts` 中持久化了 `Protocol` 和 `Endpoint` 字符串。历史事实缓存如果直接加载，会越过 `audit.Record.UnmarshalJSON`，导致分析侧直接读取到旧协议名。将 `CacheSchemaVersion` 提升至 2 强制全量重扫，彻底切断了旧缓存污染。

2. **分析侧单点咽喉归一化（`audit.Record.UnmarshalJSON`）—— 结论：真实成立，设计精简有效**
   - **分析**：`report`、`story`、`reqdetail` 和 `ctxgraph` 的日志反序列化全部依赖 `audit.Record`。在反序列化阶段对 `Protocol`、`Attempts[].Protocol` 和 `Attempts[].Endpoint` 进行归一化，既做到了对上层分析业务的透明兼容，又避免了在数十个分析文件中增加胶水代码。

3. **`vmr replay` 历史记录兼容需求（原 Phase 10）—— 结论：业务假设不成立，已正确裁撤**
   - **分析**：方案初期认为 `replay` 需要支持回放历史日志，拟增加归一化处理。实际业务定位中，`replay` 是在线重放与即时调试工具，无需承担老版本日志兼容的历史包袱。最终决定 `replay` 仅认新枚举值，裁撤 Phase 10 属于正确的收敛决策。

4. **`respnorm.go` 前缀截断风险 —— 结论：风险不成立（误报）**
   - **分析**：前期方案担忧 `openai-responses` 与 `openai-completions` 前缀重叠可能导致 `appendDone()` 等逻辑失效。实际审查代码发现，原代码使用 `protocol != "openai"` 精确比对，并不存在 `strings.HasPrefix` 等模糊匹配，改名后直接替换为常量比对即可，不存在隐式逻辑陷阱。

5. **`internal/pricing/resolve.go:78` 协议名污染 —— 结论：不成立（误报）**
   - **分析**：该文件中的 `"anthropic"` 字符串是模型供应商（Vendor）标识，用于计费匹配（与 `"deepseek"` 并列），并非 Ingress Protocol。维持现状不修改是正确的。

---

### 7.2 方案设计本身的漏洞与改进复盘

1. **重大疏漏：遗漏 `internal/adapter/fingerprint.go` 的协议分发**
   - **漏洞说明**：原始 Action Plan 列举的改造清单中完全遗漏了 `internal/adapter/fingerprint.go`。`SessionFingerprint` 内部通过 `switch protocol` 对 `"openai"`、`"anthropic"` 和 `"openai-responses"` 分流提取会话指纹。
   - **严重性**：若未在落地前捕获此项，请求到达 `router.Serve` 时协议已变成 `openai-completions` / `anthropic-messages`，`SessionFingerprint` 会直接 fallthrough 返回空指纹，导致全站的 Prompt Cache 会话粘性（Sticky Routing）在改名后静默全面失效。
   - **结论**：落地执行前补充该文件改动（改为引用 `core.Protocol*`），修复了这一隐蔽业务回归风险。

2. **方案设计缺陷：`NormalizeEndpointLabel` 粗暴重构分隔符**
   - **缺陷说明**：原方案设计的 `NormalizeEndpointLabel` 试图用 `SplitEndpointLabel` 解析后再用 `:` 重新拼装。但系统早期历史日志曾使用 `/` 作为标签分隔符（如 `openai/provider/model`）。原方案实现会导致旧日志中的 `/` 标签被强行改写为 `:`，破坏历史既有格式。
   - **改进情况**：实际代码改为仅替换第一个分隔符（`:` 或 `/`）之前的协议名称 token，保留原有分隔符及后续字节不变，避免了数据二次污染。

3. **测试字面量替换策略的务实调整**
   - **方案偏离与权衡**：原计划要求将 65+ 测试文件中的测试字面量全部换成 `core.Protocol*` 常量。实际落地时，生产代码全量引用常量，而测试代码主要将字面量更新为 `"openai-completions"` 等具体新值。
   - **评估**：这一调整是合理的。测试直接使用具体字符串字面量，能够更真实地验证配置文件解析与网络协议传输，同时避免为了短期过渡修改过多测试文件。

---

### 7.3 实现执行情况复核与遗留 Gap（漏网之鱼）

经对 Commit #793912（涉及 114 个文件、1790 处插入 / 1122 处删除）的全量复核，代码质量与契约一致性整体很高，35 个包测试与 `-race` 检查均通过。但在细节上仍存在少量漏网之鱼与不一致之处：

#### 1. 设计文档与用户手册中残留的旧协议写法（3 处）
- **`docs/VirtualModelRouter_Design_v4_Core.md:459`**：在 Sticky Model 的配置示例中，依然残留了早期版本按协议分层的旧结构（`models:\n  openai:\n    agent:`），未更新为当前的扁平模型与 `protocol: openai-completions` 格式。
- **`docs/VirtualModelRouter_Design_v4_Quota.md:729`**：配额设计文档示例中存在 `base_url: {anthropic: https://example.invalid/anthropic}`，遗漏了 `-messages` 后缀。
- **`docs/UserGuide.md:168` 与 `docs/UserGuide.zh.md:167`**：正文说明中提到 audit trail 示例依然写作 `(openai:volcengine-main:deepseek-v4-pro)`，应更新为 `(openai-completions:volcengine-main:deepseek-v4-pro)`。

#### 2. 前端 Help 页面速查表微小文本不一致（1 处）
- **`internal/server/help.html:779`**：速查表表格中，Claude Code 标注为 `anthropic-messages`，Pi Agent/Codex 等标注为 `openai-completions`，但 Cursor CLI 这一行的 Protocol 徽章文本仍显示为 `<span class="badge badge-orange">responses</span>`（未统一为 `openai-responses`）。

#### 3. 部分分析侧测试 Mock 数据仍使用旧前缀（非功能影响）
- 在 `internal/report/provider_test.go`、`internal/report/clientendpoint_test.go`、`internal/report/providerquota_test.go`、`internal/report/section_endpoint_value_test.go`、`internal/report/cost_test.go` 等文件中，部分单元测试构造的 mock `EndpointRow` 仍写有 `Endpoint: "openai:acct1:m"`。
- **说明**：这些测试属于内存级内部聚合计算，不经过 `audit.Record.UnmarshalJSON`，测试自身断言能够自洽跑通，不影响功能正确性；但测试夹具未完全与新协议格式统一。
- 类似地，`internal/ctxgraph/manifest_test.go:16` 中仍保留了 `Protocol: "openai"`。

#### 4. 技术债务生命周期管理
- 兼容过渡逻辑（`core.CanonicalProtocol`、`core.NormalizeEndpointLabel`、`audit.Record.UnmarshalJSON`）及对应的单测均已显式标注 `TODO(2026-10)`，并在 `KNOWN_ISSUES` 进行了完整登记，具备清晰的下线生命周期。

---

### 7.4 Review 总结与后续建议

| 维度 | 评估结论 |
|---|---|
| **方案设计** | 核心思路（全链路新枚举 + 分析侧反序列化单点兼容 + 缓存版本失效）清晰有效，且在执行中纠正了分隔符改写与 `SessionFingerprint` 遗漏问题。 |
| **落地执行** | 生产代码 100% 使用 `core.Protocol*` 常量，HTTP 路由、遥测、流式规范化、图片处理及配置解析均完全对齐，架构测试与竞态检查全部绿灯。 |
| **遗留小项** | 存在 3 处文档示例/正文旧写法、1 处 HTML 速查表徽章文本微小不一致，以及部分非反序列化测试的 Mock 标签残留。 |

**建议改进清单（按需在后续维护时清理）：**
1. 修正 `docs/VirtualModelRouter_Design_v4_Core.md:459`、`docs/VirtualModelRouter_Design_v4_Quota.md:729` 及 `UserGuide` 中 3 处旧协议示例文本。
2. 将 `internal/server/help.html:779` 中 Cursor CLI 的协议徽章文本由 `responses` 改为 `openai-responses`。
3. 待 2026-10 过渡期结束后，按 `KNOWN_ISSUES` 登记清单集中拆除 `audit.Record.UnmarshalJSON` 兼容垫片及相关测试。


---

## 8. 第二轮逐项核实与补漏（2026-08-27，by Sonnet 5）

对 §7 提出的每一条按原码逐项核实，结论如下。

### 8.1 §7.1 / §7.2 的问题定性 —— 全部核实为准确

| §7 条目 | 核实结论 |
|---|---|
| CacheSchemaVersion 1→2 必要性 | ✅ 属实。`ctxgraph/manifest.go:105` `Protocol: rec.Protocol`、`report/factscache.go:151/175` 复用 `ctxgraph.CacheSchemaVersion` —— 旧事实缓存直接加载会越过 `UnmarshalJSON`。已到位。 |
| `audit.Record.UnmarshalJSON` 单点咽喉 | ✅ 属实。`ctxgraph/scan.go:137`、`report/aggregate.go` 的 `scanAndCacheFile` 均解码进 `audit.Record`。 |
| replay 裁撤 Phase 10 | ✅ 正确。`internal/replay` 独立结构体解码，用户明确不兼容。 |
| respnorm 前缀风险（误报） | ✅ 确认误报。`respnorm.go:677` 是 `s.protocol != core.ProtocolOpenAICompletions` 精确比对，无 `HasPrefix`。 |
| `pricing/resolve.go:78`（误报） | ✅ 确认误报。该 `"anthropic"` 在注释里与 `"deepseek"` 并列，是 vendor 名。 |
| 漏 `adapter/fingerprint.go` | ✅ 属实且严重。已在落地时补（`SessionFingerprint` 三处分支改常量）。 |
| `NormalizeEndpointLabel` 分隔符 | ✅ 属实。已改为只替换前导 token。 |

### 8.2 §7.3 遗留 Gap 逐项核实

| §7.3 条目 | 核实结论 | 处理 |
|---|---|---|
| **1a. `Design_v4_Core.md:459` Sticky 示例用旧两层结构 `models:\n openai:`** | ✅ 属实。该 YAML 块用了扁平化改造前的 `models.<protocol>.<name>` 层级 + 单数 `model:` + 无 `protocol:` 字段，与同文档 373 行的当前正确写法不一致。**注：这不是本次改名引入的，是 flat-provider 迁移时的历史遗漏。** | ✅ 已修：重写为当前扁平 schema（对齐 373 行），每条 endpoint 加 `protocol: openai-completions` |
| **1b. `Design_v4_Quota.md:729` `base_url: {anthropic: ...}`** | ✅ 属实。**该文档整份不在原方案 Phase 8 清单里**（Phase 8 只列了 UserGuide + Core + Analytics），是方案范围漏项。全文仅此一处。 | ✅ 已修：`{anthropic-messages: ...}` |
| **1c. `UserGuide.md:168` / `.zh.md:167` 审计示例 `openai:volcengine-main:deepseek-v4-pro`** | ✅ 属实。行内 `` `openai:volcengine-main:deepseek-v4-pro` `` 是 audit trail 端点标签示例，改名后真实日志产出 `openai-completions:...`。 | ✅ 已修（含反引号包裹，两个文件同步） |
| **2. `help.html` Cursor CLI 徽章 `responses`（§7 说 779，实为 736 与 789 两处）** | ✅ 属实。同页 724/728 行标准化为 `openai-responses`，这两处 badge 仍写裸 `responses`。（改名 perl 只处理了 `badge-primary`/`badge-anthropic`，未覆盖 `badge-orange`。） | ✅ 已修：两处 → `openai-responses` |
| **3. 分析侧测试 Mock 夹具仍写 `openai:acct1:m` / `Protocol: "openai"`（约 10 个文件，60+ 处）** | ✅ 属实但纯装饰。这些是 `rec2`/`EndpointRow`/直接构造的 `audit.Attempt`/`ctxgraph.Manifest` 结构体字面量，**不经 `UnmarshalJSON`**，测试断言自洽跑通。其中 `provider_test.go:72-73`、`reqdetail/detail_test.go:60-66` 是**故意**混用 `/` 与 `:` 两种分隔符测 `splitEndpoint` 解析。 | ⏸️ **需你拍板**，见 §8.4 |

### 8.3 §7 未提及、本轮补充发现的漏项

| 位置 | 问题 | 处理 |
|---|---|---|
| **`loadtest/config.yaml`**（git 跟踪，`vmr-loadtest.sh` 使用） | `protocol: openai` ×10、`protocol: anthropic` ×1、`base_url: {openai:...,anthropic:...}` ×4。**不在原方案任何清单里**。改名后 `./vmr check -c loadtest/config.yaml` 会 `unknown adapter type` 失败，压测流程中断。CI 不跑压测，故 CI 不会红。 | ✅ 已修 + `./vmr check -c loadtest/config.yaml` → `=== OK ===` |
| `respnorm/respnorm.go:36` 包注释 "openai-protocol SSE responses" | 措辞不精确（实际判定是 `== openai-completions`，"openai-protocol" 可被读成含 responses） | ✅ 已修：→ "openai-completions SSE responses" |
| `adapter/fingerprint.go:125` 注释 `the "openai" case's` | 分支已改常量，注释残留旧名 | ✅ 已修：→ `the openai-completions case's` |
| `diagnose/diagnose.go:370` 注释 "An openai-protocol endpoint" | 同上（原方案 Phase 4 说 diagnose "注释" 要更新，落地时只改了 449 行漏了 370） | ✅ 已修：→ "An openai-completions endpoint" |

**已核实确认无需处理的**：
- `i18n/story_corpus.go` / `story_spine.go` 的 "Anthropic 协议" / "non-Anthropic protocol" 叙事文本 —— 自然语言显示名，`Anthropic` 是 `anthropic-messages` 的可读名，代码侧 key 判定已用常量。
- `i18n/report_tokens.go` "Anthropic 缓存创建" —— vendor 的 prompt-cache 计费特性名，非协议。
- `chatmsg/sse.go` / `usage.go` 注释 "openai chunk" / "anthropic events" —— 描述两家 wire format 形状的通用简写，与 ingress 协议命名无关。
- `Design_v4_Analytics.md` —— 全文无协议枚举值残留（line 181 "openai 的 choices[] / anthropic 的顶层 content[]" 是 wire-format 简写，同段落也用 "Responses" 简写，风格一致）。
- `pricing.example.yaml`、`.github/`、`vmr.sh`、`vmr-loadtest.sh`、`loadtest/*.go` —— 已 sweep，无协议枚举值。

### 8.4 需你拍板：分析侧测试 Mock 夹具是否统一改名

**现状**：约 10 个 `internal/report/*_test.go`、`internal/reqdetail/*_test.go`、`internal/ctxgraph/manifest_test.go` 里，结构体字面量仍写 `Endpoint: "openai:acct1:m"` / `Protocol: "openai"`（60+ 处）。这些**不经 `UnmarshalJSON`**、断言自洽、`go test ./...` 全绿。

**选项 A（改）**：blanket `openai:` → `openai-completions:`、`Protocol: "openai"` → `"openai-completions"`。好处：全仓 grep `"openai:"` 归零，读代码不会误以为 `openai:` 还是合法当前值。代价：60+ 处纯夹具改动，且 `provider_test.go` / `detail_test.go` 里**故意**保留的 `/` vs `:` 分隔符测试用例需要小心不要误改语义。

**选项 B（不改，推荐）**：保留现状。理由：这些字符串在对应测试里是**不透明的聚合 key**（测排序/上卷/分账，标签值本身无语义）；`splitEndpoint` 的 legacy 格式测试**需要**旧写法；一个月后兼容层拆除时这批夹具也不影响；改动面大、收益低。可只在 `manifest_test.go` 等 1-2 个"看起来像真实数据"的关键夹具上做示范性更新。

§7 原作者也将此项列为"按需在后续维护时清理"（低优先）。

### 8.5 本轮改动验收

| 项 | 结果 |
|---|---|
| `go build ./...` / `gofmt -l` | ✅ 零错误 |
| `go test ./...` | ✅ 全绿 |
| `go test ./internal/archtest/...` | ✅ 通过 |
| `./vmr check -c loadtest/config.yaml` | ✅ `=== OK ===`（之前会失败） |

**本轮提交**：文档 4 处（Core Sticky 示例、Quota base_url、UserGuide ×2 审计示例）+ `help.html` 徽章 2 处 + `loadtest/config.yaml` + 3 处代码注释精确化。

---

## 9. 第三轮：分析侧测试夹具彻底改名（Option A 落地，2026-08-27，by Sonnet 5）

用户决策：§8.4 选 **Option A —— 改彻底,一处不留**。除 `audit.Record.UnmarshalJSON` +
`core.CanonicalProtocol` 这唯一一处历史日志转换点（及其两个专属测试）外,全仓任何地方
都不得再出现旧协议 token。

### 9.1 任务规划与进度（`_tmp/plan_sonnet-5.md`）

逐文件改 → 逐包 `go test -count=1` → 打勾。共 20 个文件：

| 文件 | 改动 |
|---|---|
| `internal/core/endpointlabel_test.go` | blanket `openai` → `openai-completions`（含 `SplitEndpointLabel`/`EndpointLabel` 的输入、断言、Errorf 文案、注释、invalid-input cases） |
| `internal/core/core_test.go` | `AdapterType:` 值 ×10 + `Name()` 断言 ×2 + 局部变量 `openai`/`anthropic` → `openaiEP`/`anthropicEP` + 修正 `TestHealthKeyProtocolPrefixAvoidsCollision` 注释里对已废弃 `providers.openai/providers.anthropic` 结构的引用 |
| `internal/ctxgraph/manifest_test.go` | `Protocol: "openai"` |
| `internal/sticky/sticky_test.go` | `Set` 值 + 断言（sticky registry 存的是 `Endpoint.Name()` 斜杠形式） |
| `internal/story/modelusage_test.go` | `"openai/old-provider/old-model"` slash-format 用例（"legacy" 指分隔符不是协议名） |
| `internal/reqdetail/detail_test.go` | `TestAttemptUpstreamFallback` 的 `Endpoint`/`Protocol` 输入 + `wantProtocol` 断言 + `TestRealModelFallback` |
| `internal/reqdetail/ensure_test.go` | `Endpoint` ×2 |
| `internal/router/router_serve_test.go` | `TestIngressPath` 的 Errorf label ×2 + `TestRedirect...` 的 `Contains(body,"anthropic")` → `"anthropic-messages"`（原来靠子串匹配侥幸通过） |
| `cmd/vmr/i18n_e2e_test.go` | attempts fixture 的 `endpoint` |
| `internal/report/*_test.go` ×7 | `section_client_endpoint`/`provider`/`providerquota`(~40处)/`sticky`/`clientendpoint`/`cost`/`section_endpoint_value` —— 所有 `EndpointRow.Endpoint` / `rec2.endpoint` / `stickyEntry.protocol` 字面量及配套断言。`provider_test.go` 与 `cost_test.go` 里**故意混用 `:` 与 `/` 分隔符**的解析测试保留两种分隔符、只换协议 token |
| `internal/config/config_proxy_test.go` | `Provider.BaseURL` map key `{"openai":...}` + `ProxySpecFor(p, "openai")` 协议参数 ×5（第一轮只扫了 YAML 文本，漏了 Go map literal 与函数参数） |
| `internal/config/config_test.go` | 两处注释（`TestProviderServesBothProtocols` / `TestOpenAIResponsesProtocolAccepted`） |
| `cmd/vmr/quota_parity_test.go` | `core.EndpointLabel("openai", ...)` → `"openai-completions"` —— **原来该 parity 测试隐性依赖 `NormalizeEndpointLabel` 归一化才能对上号,现在不再依赖 compat shim**（2026-10 拆除后不会突然红） |

### 9.2 明确保留旧字符串的位置（已逐一对源码核实：均非协议枚举，属 vendor / provider-name / pricing canonical key 命名空间）

| 位置 | 为什么不是协议名 |
|---|---|
| `internal/audit/audit_test.go` `TestRecordUnmarshalJSON_*`、`internal/report/aggregate_test.go` `TestBuild_LegacyProtocolNamesNormalized` | **旧名是被测输入**——这两个测试的存在就是为了验证转换。删兼容层时一起删。 |
| `tools/gen_standard_pricing/main.go` `primaryVendors` map、`main_test.go` 全部 | `"openai"`/`"anthropic"` 与 `"gemini"`/`"xai"`/`"cohere"` 并列,是**厂商名**,用于构建 `vendor/model` 定价键。 |
| `internal/pricing/*_test.go`、`cmd/vmr/cmd_report_pricing_test.go` | `RateFor("anthropic", model)` / `Resolve("anthropic", ...)` / `"anthropic/claude-3-5-sonnet"` —— 定价解析的 **vendor 前缀 / canonical key**,不是 ingress protocol。 |
| `internal/config/pricing_test.go` | provider **命名为** `"anthropic"` 是该测试的主题（行 43 注释明说"happens to match the standard table's vendor prefix"）——测的就是 provider 名撞 vendor 前缀时的解析路径。 |
| `internal/router/quota_cost_test.go` | provider `name: anthropic` + model `claude-3-7-sonnet-20250219`——`metric: cost` 靠 provider 名当 vendor 前缀去标准表查 `anthropic/claude-3-7-sonnet-*` 的费率。改名 provider 会让 `PricingRate` 解析失败,测试直接 fail。`Quota.Charge/Used/AddEstimatedCost("anthropic",...)` 的第一个参数是 **provider 名**。 |
| `internal/config/config_test.go:801`（已更新为新名后仍非枚举）、`i18n` 叙事文本、`chatmsg` wire-format 注释 | 见 §8.3 已核实清单。 |

### 9.3 收尾扩展：全仓 grep 复查后又清掉的一批（不止测试夹具）

第一遍改完 20 个测试文件后，逐类做全仓 `grep` 复查，发现"改彻底"必须覆盖的不止结构体字面量，还有：

**生产代码注释 / 包文档**（wire-format 与协议族简写，逐条换成新枚举名）：
- `internal/chatmsg/sse.go`、`internal/chatmsg/messages.go`：`// openai chunk` / `// anthropic: top-level content blocks` / `anthropic keeps system as top-level` 等
- `internal/respnorm/respnorm.go`（`appendDone` 开头注释 `speaks the OpenAI protocol`、`pathology as openai content`）、`internal/respnorm/minimax.go`
- `internal/server/facts.go`（`protocol-aware: openai content[].image_url`）、`internal/server/server.go`（`the Anthropic protocol headers`）
- `internal/adapter/fingerprint.go`（`the "openai" case's`）、`internal/diagnose/diagnose.go`（`An openai-protocol endpoint`）
- `internal/config/config.go`/`provider.go`、`internal/config/config_test.go` 多处注释里的 `openai-protocol`/`anthropic-protocol`/`providers.openai` 旧结构引用

**测试里的 Errorf / 用例名 / 注释**：`anthropic_concurrency_test.go`、`server_test.go`、`diagnose_test.go`、`router/providergroup_test.go`、`router/router_serve_test.go`、`report/session_test.go`、`chatmsg/pairing_test.go`/`sse_test.go`、`reqdetail/detail_test.go`、`taskseg/segment_test.go` 等的 `openai-protocol` / `"openai shape"` / `Options.Protocol=anthropic` 等

**i18n 用户可见输出**（`internal/i18n/story_corpus.go`、`story_spine.go`）：覆盖率披露注记里的"Anthropic 协议" / "non-Anthropic protocol" → 统一用**选定的显示名 "Anthropic Messages"**（`> ⚠️ 本 journey 全部请求均为非 Anthropic Messages 协议…`）。连带 `internal/story/testdata/golden{,_zh}.md` 用 `UPDATE_GOLDEN=1` 重生成，diff 仅这一句。

**配置文件注释**：`config.yaml`/`config.mba.yaml`/`config.example.yaml`/`.zh.yaml`（`openai-protocol entry`、`用 Anthropic 协议来说话`、header 里的 `("openai" | "anthropic" | ...)` 枚举列表）、`config.local.yaml` header 的旧 schema 迁移注释、`loadtest/config.yaml`（第二轮已修 protocol 值，本轮无残留）

**当前状态设计文档**（4 个 canonical + UserGuide + KNOWN_ISSUES + README）：所有"openai 协议 / openai protocol / Anthropic-protocol"散文表述 → 新枚举名（含 `Design_v4_Core.md` 决策表被否方案格、`Design_v4_Analytics.md` 的 `is_error` 覆盖率讨论、`KNOWN_ISSUES` §0/§40 的语料分布图例，冻结的百分比数字不动、只换协议标签）。

### 9.4 明确保留旧字符串的完整白名单（逐一对源码核实：均非 ingress 协议枚举）

| 命名空间 | 位置 | 为什么保留 |
|---|---|---|
| **兼容转换点本身** | `core/protocol.go` 的 `case "openai"/"anthropic"`、`audit.go` `UnmarshalJSON` doc、`audit_test.go` `TestRecordUnmarshalJSON_*`、`report/aggregate_test.go` `TestBuild_LegacyProtocolNamesNormalized` | 旧名是被测输入 / 转换逻辑本体。2026-10 一起删。 |
| **pricing vendor / canonical key** | `internal/pricing/*`（`RateFor`/`Resolve`/`resolveCanonicalKey("anthropic",…)`、`"anthropic/claude-*"`、`"openai/gpt-*"`、`standard_price_generated.yaml`、`primaryVendors` map）、`tools/gen_standard_pricing/*`、`cmd/vmr/cmd_report_pricing_test.go`、`config/pricing_test.go` 的 `pricingCfgNamed(…,"anthropic",…)` + `ProviderByName("anthropic")`、`router/quota_cost_test.go` 的 `name: anthropic` provider + `Quota.*("anthropic",…)`、config.example/UserGuide 里 `- name: anthropic` + `anthropic/claude-*` 示例 | `"anthropic"` 是**上游厂商名**（与 `gemini`/`deepseek`/`xai` 并列），用于 `vendor/model` 定价键匹配。改成 `anthropic-messages` 会让费率解析失败。 |
| **Go 包名** | `internal/adapter/openai` / `internal/adapter/anthropic` 目录名及所有对它们的引用（`adapter/{openai,anthropic,openairesponses}`、"the openai/anthropic adapters"） | Go 包名不能带连字符；`Register()` 传的是独立的 `core.Protocol*` 常量，与包名解耦。 |
| **HTML 元素 id / CSS 类 / 模板占位符 / 选定显示名** | `help.html`/`status.html` 的 `dash-base-url-openai`、`badge-anthropic`、`{{BASE_URL_OPENAI}}`、`"Anthropic Messages"` 徽章文本 | id/class/占位符是标识符不是枚举值；`"Anthropic Messages"` 是第 6.2 节选定的前端显示名。 |
| **第三方工具配置值** | `help.html:710` Continue.dev 的 `"provider": "openai"` | 这是 Continue.dev 自己 schema 的 provider-type 值，改了会让用户的 Continue.dev 配置失效。 |
| **测试里的模型名 / 假 key / 生态术语** | `m-openai`/`vm-openai` 虚拟模型名、`"leaked-anthropic-key"` 脱敏测试用假 key、`"self-described-OpenAI-compatible providers"`（"OpenAI 兼容" 是行业通用术语，类比 "S3-compatible"）、`resolve_test.go` 的 `"my-openai-account"` provider 名、URL 路径 `api.deepseek.com/anthropic/v1` | 都是名字 / 术语 / 数据，不是 ingress 协议枚举。 |
| **vendor 计费特性名** | `i18n/report_tokens.go` "Anthropic 缓存创建，溢价计费" | 指 Anthropic **公司**的 prompt-cache 计费模型，非协议。 |
| **冻结分析快照** | `docs/future-strategy/story_report_*.md`、`docs/data/model_prices_and_context_window.json`（外部 litellm 数据） | 非 current-state 文档；其 `openai:` 标签是分析当时日志的原样，改了等于篡改历史测量。（`protocol_enum_unification_review.md` 本篇例外——它是本次迁移的记录。） |

### 9.5 验收

| 项 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` / `gofmt -l internal/ cmd/ tools/` | ✅ 零输出 |
| `go test ./...` | ✅ 全绿（golden 已重生成；`TestCmdStory_JourneyWithRealLLM` 首跑 flaky——真实 LLM 端点，隔离重跑通过，与本改动无关） |
| `go test ./internal/archtest/...` | ✅ 通过 |
| `go test -race ./internal/{core,audit,router,report,story,sticky,ctxgraph,server,respnorm}/...` | ✅ 全绿 |
| `./vmr check` × 6 份配置（含 `loadtest/config.yaml`） | ✅ 全 `=== OK ===` |
| 全仓 grep 复查（`.go`/`.yaml`/`.html` + 7 份 current-state 文档） | ✅ 残留的 `"openai"`/`"anthropic"` 全部落在 §9.4 白名单内，零 ingress 协议枚举残留 |

### 9.6 提交

- 本轮：52 个文件（测试夹具 + 生产注释 + i18n + golden + 配置注释 + 当前状态文档），`_tmp/plan_sonnet-5.md` 已删。

---

## 10. 全面复核与最终收官验收（Commit #0d33a47 & #7a55d89）

<!-- Date: 2026-08-27 | Reviewer: Claude -->

### 10.1 增量 Commit 逐一复核（#0d33a47 & #7a55d89）

基于源码与真实 Diff，对第二轮和第三轮的两个提交进行细致核实：

1. **Commit #0d33a47（第二轮补漏与文档/配置修复）核实**：
   - **`loadtest/config.yaml`**：10 处 `protocol: openai-completions`、1 处 `protocol: anthropic-messages`、4 处 `base_url` key 均已正确更新。实测 `./vmr check -c loadtest/config.yaml` 成功通过（输出 `=== OK ===`）。此前该文件因缺少更新会导致压测时加载失败。
   - **`docs/VirtualModelRouter_Design_v4_Core.md:456-478`**：Sticky Model 示例已从旧版两层 map 嵌套结构重写为扁平 endpoint-group 结构，并补齐 `protocol: openai-completions`。
   - **`docs/VirtualModelRouter_Design_v4_Quota.md:729`**：`base_url: {anthropic-messages: ...}` 已补齐 `-messages`。
   - **`docs/UserGuide.md:168` / `UserGuide.zh.md:167`**：审计标签示例已同步修正为 `openai-completions:volcengine-main:deepseek-v4-pro`。
   - **`internal/server/help.html`**：Cursor CLI 徽章在 736、789 两处均已规范化为 `openai-responses`。
   - **注释精确化**：`respnorm.go:36`、`fingerprint.go:125`、`diagnose.go:370` 中的协议注释均已更新。

2. **Commit #7a55d89（第三轮全仓彻底清扫 / Option A）核实**：
   - **测试 Mock 夹具彻底更新**：覆及 `internal/report/*_test.go`（7 个文件）、`internal/reqdetail/detail_test.go`、`internal/core/endpointlabel_test.go`、`internal/core/core_test.go`、`internal/ctxgraph/manifest_test.go`、`cmd/vmr/i18n_e2e_test.go` 等。所有的直接构造字面量均已更新为 `openai-completions:` 前缀，同时保留了针对 `/` 与 `:` 两种分隔符解析的兼容性测试。
   - **`config_proxy_test.go` 补漏**：修正了第一轮仅替换 YAML 文本而遗漏的 Go 代码参数 `ProxySpecFor(p, "openai-completions")` 与 `BaseURL` map key。
   - **`quota_parity_test.go` 解耦**：`EndpointLabel` 构建改用 `openai-completions`，测试数据与真实生产对齐。
   - **自然语言与 i18n 覆盖率披露文本统一**：`story_corpus.go` 和 `story_spine.go` 的披露文本统一使用前端选定的显示名 `"Anthropic Messages"`，配套黄金测试 `golden.md`/`golden_zh.md` 仅差异更新该单句。
   - **当前状态设计文档与配置注释全量更新**：`Design_v4_Core.md`、`Design_v4_Analytics.md`、`KNOWN_ISSUES`、`UserGuide.md`/`.zh.md`、`config.example.yaml`/`.zh.yaml` 中的文字叙述已全部与新枚举对齐。

---

### 10.2 独立全盘源码扫描与事实核查

使用正则匹配针对代码库全量 Git 跟踪文件（`.go`、`.yaml`、`.md`、`.html`）进行了全盘检索，核实结果如下：

1. **生产代码中的 bare 协议字符串**：
   - 全仓生产代码中仅保留了 `internal/core/protocol.go:27,29` 的 `case "openai"` / `case "anthropic"` 转换分支，属于兼容垫片核心逻辑本身。
   - 无任何漏网的字符串硬编码比对或分支分流。

2. **测试代码中的 bare 字符串分布**：
   - 剩余全部集中在以下三类合理场景，完全符合 §9.4 白名单规范：
     1. **兼容垫片本身的针对性测试**（`audit_test.go`、`aggregate_test.go` 验证旧日志反序列化归一化）。
     2. **计费模型命名空间**（`internal/pricing`、`gen_standard_pricing` 等测试中的 vendor 名称 `"anthropic"` / `"openai"`，以及 provider 命名为 `"anthropic"` 的专用用例）。
     3. **配额账本账号名**（`quota_cost_test.go` 中按 provider 名字记账的测试）。

3. **配置文件与当前设计文档**：
   - 所有当前生效的示例配置（`config.example.yaml`、`config.example.zh.yaml`、`loadtest/config.yaml`）以及核心设计文档中，零遗留旧协议枚举值或旧版 `base_url` key。
   - 历史分析快照（如 `docs/future-strategy/story_report_*.md`）保持历史测量数据原貌，未进行篡改。

4. **架构与回归验证**：
   - 全量测试 `go test ./...` 35 个包全部 PASS。
   - 竞态检查 `go test -race ./...` 干净无告警。
   - 架构规则 `go test ./internal/archtest/...` 零违规。
   - 配置文件校验 `./vmr check -c loadtest/config.yaml` 正常通过。

---

### 10.3 结论与最终评估

本轮协议枚举值重命名（`openai` → `openai-completions`、`anthropic` → `anthropic-messages`）已达成**全链路彻底重构与收官**：
1. **彻底性**：全仓消除了生产代码、配置文件、当前文档以及测试夹具中所有的旧协议字面量，无悬空引用的技术碎片。
2. **安全性**：会话指纹分发（`SessionFingerprint`）、多协议隔离、流式 `[DONE]` 哨兵、图片压缩、配额记账与诊断探测等核心逻辑均已闭环验证。
3. **可维护性**：集中定义的 `core.Protocol*` 常量已作为单一可信来源贯穿全局；唯一的兼容层（`audit.Record.UnmarshalJSON` + `core.CanonicalProtocol`）具备明确的 `TODO(2026-10)` 生命周期标记并在 `KNOWN_ISSUES` 建档。

---

## 11. 第四轮独立复核（2026-08-27，by Sonnet 5）

<!-- Reviewer: Claude。本轮不信任前文任何"已核实/✅"结论，全部以 commit #793912f / #0d33a47 / #7a55d89 的真实 diff 与当前源码重新对账。 -->

### 11.1 结论摘要

三个 commit 的落地**整体扎实**：单点咽喉兼容设计（`audit.Record.UnmarshalJSON` + `CacheSchemaVersion` 失效）是正确且经得起推敲的，生产代码侧无逻辑坑，独立重跑 `go build` / `go vet` / `gofmt -l` / `go test ./...` / `go test -race`（audit/core/report）/ `archtest` 全绿，六份配置 `./vmr check` 全 `OK`。

但 §10.3 的两条断言与事实不符，需要修正：

- **「全仓消除了……测试夹具中所有的旧协议字面量，无悬空引用」** —— 不成立。`examples/sample-audit.jsonl` 是漏网之鱼，且不在 §9.4 白名单里（详见 11.2）。
- **§9.5 / §10.2「全仓 grep 复查」** —— 复查范围是 `.go` / `.yaml` / `.html` + 文档，**从未 grep 过 `.jsonl`**，因此上面这条漏项在前三轮 review 里结构性地看不到。

前几轮对方案缺陷的定性（`fingerprint.go` 遗漏、`NormalizeEndpointLabel` 分隔符、`respnorm` 前缀误报、`pricing/resolve.go` 误报、replay 裁撤）——本轮逐条对源码重核，**全部准确**，无需推翻。

### 11.2 唯一实质性漏项：`examples/sample-audit.jsonl`

**事实**（`git grep`，当前 HEAD）：该文件 5 条记录，每条 `"protocol":"openai"`，每个 `attempts[].protocol":"openai"`，每个 `attempts[].endpoint":"openai:coder-primary:coder-large"` / `"openai:coder-backup:coder-large-mini"`。commit #7a55d89 未触及此文件。

**它不是可以豁免的历史快照**：

| 判据 | 实际情况 |
|---|---|
| 引用位置 | `README.md:27` / `README.zh.md:27` 明文：「Real output from the checked-in [`examples/sample-audit.jsonl`] — run `./vmr report -o /tmp/out examples/sample-audit.jsonl` and compare」 |
| 测试引用 | `internal/audit/sample_data_test.go` 的包注释：「as "what an audit record looks like"」——定位是**当前形状样例**，不是历史日志 |
| 目录 | `examples/`，不是 §9.4 白名单里的 `docs/future-strategy/` 冻结分析快照 |
| 白名单 | §9.4 完整白名单里**没有**这个文件 |

**当前影响（低，但非零）**：兼容垫片在读时归一化，`./vmr report examples/sample-audit.jsonl` 实测产出 `openai-completions:coder-primary:coder-large`（已验证），所以今天 README 的「run and compare」不会翻车——它反而无意中成了垫片的一个 end-to-end 活样例。

**2026-10 垫片拆除后的影响（中）**：`CanonicalProtocol` 删除后，同一条 README 指令产出的报表里协议列与端点标签会变成裸 `openai` / `openai:coder-primary:coder-large`，与全仓其它一切不一致；`details/*` 的 coordinate-hash 文件名也会随标签变化而改变。README 让用户「compare」，届时对不上。

**建议（二选一，本 review 不代为执行）**：
1. **直接改新名**（推荐）：README 要的是当前格式；垫片已有专属单测（`TestRecordUnmarshalJSON_*`、`TestBuild_LegacyProtocolNamesNormalized`）兜底，样例文件改新名不损失任何测试覆盖。
2. **保留旧名**：则必须补进 §9.4 白名单，注明「刻意保留为垫片的 e2e 活样例」，并列入 2026-10 拆除清单（拆垫片时同步改新名）。

现状是「既没改、也没登记」——这正是本轮要指出的 gap。

### 11.3 对前几轮"已核实"结论的独立再验证（抽查，均通过）

| 前文声称 | 本轮独立核实方式 | 结论 |
|---|---|---|
| `audit.Record.UnmarshalJSON` 覆盖所有分析侧读路径 | grep 全部 `json.Unmarshal(..., &rec)` 到 `audit.Record` 的站点 | 实际有 **5 处**（`ctxgraph/records.go:96`、`ctxgraph/scan.go:138`、`report/aggregate.go:286`、`report/detail.go:258`、`report/session.go:322`），方案只列了 2 处——但类型方法设计让 5 处**自动全覆盖**，欠列无后果 ✅ |
| `NormalizeEndpointLabel` 只改前导 token、保留 `/` 与 `:` | 手工走查 4 类输入（`:` 型、`/` 型、model 内含 `:`、model 内含 `/`、空串、无分隔符） | 「取最早分隔符」逻辑与 `SplitEndpointLabel` 一致，全部正确；`canon==proto` 提前返回避免了新名再拼接 ✅ |
| `CacheSchemaVersion` 1→2 同时失效 ctxgraph 与 report 缓存 | 查 `factscache.go:151/175` 是否复用 `ctxgraph.CacheSchemaVersion` | 是，两处缓存单一常量门控，一次 bump 全覆盖 ✅ |
| `respnorm.appendDone` 前缀风险是误报 | 读 `respnorm.go:677` 实际代码 | `s.protocol != core.ProtocolOpenAICompletions` 精确比对，改名前后行为字节一致 ✅ |
| `fingerprint.go` 遗漏已在落地时补 | 读 #793912f 的 `fingerprint.go` diff | 三处分支（`openai-responses` / `anthropic` / `openai` case）+ import 均已补，`fingerprint_test.go` / `_fuzz_test.go` 同步 ✅ |
| `sticky` 会不会有旧 endpoint key 残留 | 查 `internal/sticky` 有无落盘代码 | 纯内存 registry，无持久化，改名无跨重启污染 ✅ |
| quota 记账不受影响 | CLAUDE.md + `quota_cost_test.go` 保留 `name: anthropic` provider | quota 按 provider **名**记账，与协议枚举正交，`vmr-quota.json` 无需迁移 ✅ |
| 配置全绿 | 独立 `./vmr check` × 4（example / zh / loadtest / mba） | 全 `valid`；`config.local.yaml` 的失败是改名**之前**就有的废弃 `provider:` 单数 key，与本轮无关 ✅ |

### 11.4 非 bug 的观察项（供后续维护判断，不强制处理）

1. **协议显示名三套表述并存**：
   - `status.html` 徽章 → `OpenAI Completions` / `Anthropic Messages` / `OpenAI Responses`（友好名）
   - `help.html` 徽章 → 裸枚举 `openai-completions` / `anthropic-messages` / `openai-responses`
   - `i18n` story 覆盖率披露文本 → `Anthropic Messages`
   help 页用裸枚举有其道理（用户要照着往 config 里抄），但三个面三种写法，值得有意识地拍一次板。

2. **i18n 文案是对早期建议的一次未标注反转**：§5「点 4」与 §5.1 都建议「显示名 `Anthropic` 保持不变，只改内部引用」。§9.3 反转成 `Anthropic Messages`，产出「non-Anthropic Messages protocol」「Anthropic Messages tool results」「非 Anthropic Messages 协议」这类略拗口的散文（`golden.md` / `golden_zh.md` 已按此重生成）。这是拿可读性换 token 纯净度，可接受，但属于推翻了前文一个明确建议而没在文里说明。

3. **`CanonicalProtocol` / `NormalizeEndpointLabel` 的「禁止写路径调用」只是注释**：无 `archtest` / 测试强制。按本项目自己的原则（「一个只写在源码注释里的取舍等于没登记」），一条禁止 `router` / `server` 引用这两个函数的 `archtest` 规则是这 ~2 个月窗口期的廉价保险。可选。

4. **2026-10 拆除清单不完整**：`KNOWN_ISSUES` 列了删 `CanonicalProtocol` / `NormalizeEndpointLabel` / `Record.UnmarshalJSON` + 三处测试，但没提：(a) 本次给 `internal/audit` 新增的 `vmr/internal/core` import 要一并撤；(b) `CacheSchemaVersion` **不应回退**（保持 2 或进 3，回退会尝试复用早已不存在的 v1 缓存文件语义）；(c) 若 `sample-audit.jsonl` 选择保留旧名，它也在清单内。

5. **`KNOWN_ISSUES:124`** 仍写「Responses API（`openairesponses`）」，在同一句里与 `openai-completions` / `anthropic-messages` 枚举值并列时，用 Go 包名当简写略显不齐。纯装饰。

6. **section-number 交叉引用**：#793912f commit message 与 `KNOWN_ISSUES` 用「`KNOWN_ISSUES` 2.2」/「§2.2」——与 CLAUDE.md「No section numbers in cross-references，Cite a document and a section name」的约定相悖。纯装饰。

### 11.5 方案 vs 落地的偏差（均属合理，仅记录）

| 偏差 | 评估 |
|---|---|
| Phase 9 计划「测试字面量 → `core.Protocol*` 常量」，实际用裸新字符串 | 合理。测试应验证具体 wire 值；且垫片 ~2 个月后整体消失，再改枚举概率极低 |
| Phase 10（replay 兼容）整个删除 | 正确。`internal/replay` 用独立结构体解码，且业务上明确只认新枚举 |
| 方案「变更清单速查表」漏了 `docs/VirtualModelRouter_Design_v4_Quota.md` 整份、`loadtest/config.yaml` 整份 | 第二轮 #0d33a47 补上。说明方案的清单不是穷举，执行时必须以全仓 grep 兜底——而这次 grep 又漏了 `.jsonl`（见 11.2），教训重复了一次 |
| 方案列 audit decode 站点 2 处，实际 5 处 | 无后果（类型方法自动覆盖），但反映方案对「读路径到底有几条」的盘点不准 |

### 11.6 建议清单（优先级排序）

1. **[中]** 处理 `examples/sample-audit.jsonl`：改新名，或补进 §9.4 白名单 + 2026-10 清单（11.2）。修正 §10.3「零遗留 / 彻底性」的措辞。
2. **[低]** 统一或有意识确认协议显示名的三套表述（11.4.1）。
3. **[低]** 补全 `KNOWN_ISSUES` 的 2026-10 拆除清单：`internal/audit` 的 core import、`CacheSchemaVersion` 不回退（11.4.4）。
4. **[低-可选]** 加 `archtest` 规则禁止路由/服务侧引用 `CanonicalProtocol` / `NormalizeEndpointLabel`（11.4.3）。
5. **[装饰]** `KNOWN_ISSUES:124` 措辞、section-number 交叉引用（11.4.5 / 11.4.6）。

### 11.7 独立验收结果（本轮实跑）

| 项 | 命令 | 结果 |
|---|---|---|
| 编译 | `go build ./...` | ✅ |
| 静态检查 | `go vet ./...` / `gofmt -l internal/ cmd/ tools/` | ✅ 零输出 |
| 全量测试 | `go test ./...` | ✅ 全绿 |
| 竞态（抽样） | `go test -race ./internal/{audit,core,report}/...` | ✅ |
| 架构约束 | `go test ./internal/archtest/...` | ✅ |
| 配置校验 | `./vmr check -c` × {example, example.zh, loadtest, mba} | ✅ 全 valid |
| 垫片 e2e | `./vmr report examples/sample-audit.jsonl` | ✅ 归一化产出 `openai-completions:*` 标签（同时印证 11.2 的漏项确实被垫片"救"了） |

---

## 12. §11 残留项处置（2026-08-27，by Sonnet 5）

按「能改则改、事实清楚无争议的直接处理」的要求，对 §11.6 清单逐项处置。

### 12.1 已处理

| # | 项 | 处置 | 文件 |
|---|---|---|---|
| 1 | **`examples/sample-audit.jsonl` 残留旧枚举名**（§11.2） | 采用 §11.2 的**方案 1（改新名）**：5 条记录的 `"protocol":"openai"` → `"openai-completions"`、`attempts[].endpoint` 的 `openai:` 前缀 → `openai-completions:`。依据：① `README.md:30-33` 的「run and compare」示例**早已**写成 `openai-completions:coder-primary:coder-large`——文件不改的话，2026-10 垫片拆除后该示例会对不上；② 垫片有专属单测（`TestRecordUnmarshalJSON_*` 覆盖 `:`/`/` 两种分隔符 + round-trip、`TestBuild_LegacyProtocolNamesNormalized` 覆盖 report 端到端），样例文件改新名不损失任何测试覆盖；③ `TestSampleAuditJSONLDeserializes` 只验 schema 形状，与垫片无关。改后 `./vmr report` 直接产出 `openai-completions:*`（不再依赖垫片），`python3 -c json.loads` 5 行全部合法，`go test ./internal/audit/...` 绿。 | `examples/sample-audit.jsonl` |
| 3 | **`KNOWN_ISSUES` §2.2 的 2026-10 拆除清单不完整**（§11.4.4） | 把单句 TODO 扩成 5 步清单：① 删 `CanonicalProtocol`/`NormalizeEndpointLabel`（常量保留）；② 删 `Record.UnmarshalJSON` **并撤掉本次为它新增的 `vmr/internal/core` import**；③ 删两个 `TODO(2026-10)` 测试（**precise：是两个不是"三处"**——原文 `TestRecordUnmarshalJSON_*` 的 `*` 让人误以为多个，实际只有 `TestRecordUnmarshalJSON_NormalizesLegacyProtocolNames` 一个）；④ `CacheSchemaVersion` **保持 2 不回退**；⑤ `sample-audit.jsonl` 已随本轮改新名，无需处理。 | `KNOWN_ISSUES` |
| 5a | **`KNOWN_ISSUES` §1.28 / §1.22 用 `openairesponses` 冒充 protocol 字段值**（§11.4.5 的升级版——这是真 bug 不只是措辞） | `protocol` 字段的合法值从来是 `openai-responses`（连字符）；`openairesponses` 只是 Go 包目录名。原文 §1.28 触发条件 `protocol == "openairesponses"` **永远不可能匹配任何真实记录**，等于把重估触发条件写死成"永不触发"。三处（§1.28 分布行、§1.28 触发条件行、§1.22 汇总表行）`openairesponses` → `openai-responses`。§2.2 的 `adapter/{openai,anthropic,openairesponses}` 是包路径，**保留不动**。 | `KNOWN_ISSUES` |

**验收**：`go build ./...` / `go vet ./...` / `gofmt -l` 零输出；`go test ./...` 全绿；`go test ./internal/archtest/...`（含 doc-reference 守卫，KNOWN_ISSUES 被编辑）绿；`./vmr report examples/sample-audit.jsonl` 产出报表中 `openai-completions` ×20、裸 `openai:` ×0。

### 12.2 评估后不改（附理由）

| # | 项 | 结论与理由 |
|---|---|---|
| 2 | **协议显示名三套表述**（§11.4.1） | **撤回该观察项——细查后不成立**。`help.html` 是内部自洽的刻意设计：**散文/小节标题用友好名**（`OpenAI Completions Protocol`、Base URL 标签 `OpenAI Completions`），**徽章用裸枚举**（`openai-completions`），因为徽章的作用就是「照着往 `protocol:` 里抄的字面量」。`status.html` 是监控面板不是配置指南，全用友好名合理。`i18n` story 散文用 `Anthropic Messages` 与 help 页「散文=友好名」惯例一致。三个面各自内部一致，且遵循同一条模式（散文→友好名，配置字面量徽章→裸枚举）。无需改动。 |
| — | **i18n 文案「Anthropic Messages」是对 §5.1 早期建议的反转**（§11.4.2） | 不改。golden 已在 #7a55d89 重生成，仅为散文再churn 一次 golden 不值当；且 §11.4.1 已认定「散文=友好名」是成立的惯例，`Anthropic Messages` 符合该惯例。属可接受的取舍，保留 §11.4.2 的记录即可。 |
| 4 | **加 `archtest` 规则禁止写路径引用 `CanonicalProtocol`/`NormalizeEndpointLabel`**（§11.4.3） | 不做。现有 `archtest` 是 import-boundary + 符号存在性检查，「符号 X 不得被包 Y 引用」需要新写一类 AST 选择器扫描（遍历每个包非测试文件找 `core.CanonicalProtocol` selector）——这是新增一类测试骨架的成本，而被保护对象是一个 ~2 个月后即整体拆除的垫片。成本 > 收益。约束已写在两个函数的 doc comment（`MUST NOT be called on any write path`），且当前全仓引用点仅 `audit.Record.UnmarshalJSON` 一处（已核实）。 |
| 5b | **section-number 交叉引用「§2.2」**（§11.4.6） | 不改。① commit message 已提交不可变；② `KNOWN_ISSUES` 内部的「移入 §2.2」是**同一文档内**的编辑台账记录（"这条从 §1 挪到了 §2 哪个小节"），不是 CLAUDE.md 所指的**跨文档**引用（"Cite a document and a section name"）；台账里写目标小节号反而比写小节名更精确。非违规。 |
| — | **方案清单不穷举 / audit decode 站点盘点不准**（§11.5） | 无可改项，属过程复盘，保留记录。 |

### 12.3 对 §10.3 的更正

§10.3 第 1 条「全仓消除了……测试夹具中所有的旧协议字面量，无悬空引用」在写下时**不准确**（`examples/sample-audit.jsonl` 是反例，且 §9.5/§10.2 的 grep 从未覆盖 `.jsonl`）。经 §12.1 处理后该断言**现已成立**：全仓（含 `.jsonl`）除 `core.CanonicalProtocol` / `audit.Record.UnmarshalJSON` 这唯一一处历史日志转换点外，无旧 ingress 协议枚举残留。§9.4 白名单无需新增条目。


