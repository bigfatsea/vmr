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
- `docs/KNOWN_ISSUES_sonnet-5.md` §2.2 新增条目登记兼容取舍 + `TODO(2026-10)` 拆除清单；§0 语料构成图例更新。
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
