# Provider 级 `sticky_ttl` 与 `role_map` 配置收敛设计

## 1. 概述与背景

### 1.1 问题描述
在目前的配置模型中，`sticky_ttl`（会话粘性有效时长）与 `role_map`（请求角色重命名映射）均定义在 `EndpointGroup`（即 `models.<virtual-model>.endpoints[<protocol>][]`）层级。

这种设计在实际生产与日常使用中带来了显著的冗余和摩擦：
1. **`role_map` 的高度重复配置**：
   部分国产或开源网关（如 DashScope/千问、DeepSeek 官网 API）在协议入口层严格校验 `role` 字段，拒收 OpenAI 为 o1/o3 引入的 `developer` 角色（报 400 错误）。由于这是特定上游厂商网关的实现行为，当用户在多个虚拟模型（例如 `coding`、`agent`、`chat`）中引用该 Provider 时，必须在每个虚拟模型的每个条目下反复粘贴 `role_map: {developer: system}`，严重违背 DRY（Don't Repeat Yourself）原则。
2. **`sticky_ttl` 挂在端点组造成的认知负担与伪精确**：
   虽然缓存寿命理论上与模型和算力池挂钩，但端点层级的细粒度配置要求用户在路由拓扑中到处声明生命周期。对于单一自研厂商（如 DeepSeek 磁盘长缓存）而言，同一厂商名下的模型共享相同的后端缓存机制；对于聚合型网关（如 OpenRouter）而言，其背后的物理实例动态调度，在 VMR 层面即便细配到模型也无法消除随机性。在端点层配置反而增加了心智负担。
3. **解除多 Provider 端点组的隐性耦合**：
   在原设计中，当用户使用多账号聚合语法糖（如 `providers: [openrouter, deepseek]`）时，端点组上的 `sticky_ttl` 或 `role_map` 会被同组内的所有 Provider 强制共享。若 DeepSeek 需要 2 小时磁盘缓存且需重写 `developer`，而 OpenRouter 只需要 10 分钟且原生支持 `developer`，用户就无法使用聚合语法，被迫将配置拆散为多个冗长条目。下沉到 Provider 后，各 Provider 独立携带自身属性，多 Provider 语法糖的能力得到彻底释放。
4. **针对通用 Header/Body 操作的边界裁决**：
   曾有设想引入一套统一的声明式 DSL（基于 JSONPath、Action、通配符等）来一揽子解决未来所有 Header/Body 的增删改需求。经过深度推演，通用深层 JSON 变更在 Go 语言中会导致性能骤降、GC 停顿、破坏字节保真度（Byte-faithful passthrough），并引入内卷化脚本系统的复杂度。因此，当前最佳决策是收敛范围：**坚决不引入重型通用修改框架，仅将已证明是高频刚需的 `sticky_ttl` 与 `role_map` 提升到 `Provider` 层级**。

---

## 2. 第一性原理与架构论证

### 2.1 为什么 `sticky_ttl` 属于 Provider 基础设施层？
从第一性原理出发，Session Sticky（会话粘性路由）的本质是**尽力而为（Best-effort）的软亲和性加速，而非强一致性的硬事务**：
- **容错弹性高**：TTL 设置 5 分钟还是 10 分钟，对路由的正确性毫无破坏。TTL 过期后，路由器只是重新按优先级与配额评估；若该端点依然处于最优位，请求依然会落回该端点；即使切到其他端点，也仅仅是损失一次缓存优惠，请求依然能够成功完成。
- **聚合供应商的不可控性**：面对 OpenRouter 这类多模型网关，VMR 无法探知其下游具体的机房和算力池分配。强行在端点层做精细微调属于典型的“伪精确（False Precision）”。
- **关注点分离（Separation of Concerns）**：
  - `Provider` 实体负责承载**“上游账号与基础设施属性”**（BaseURL、APIKey、Proxy、Quota、StickyTTL）。
  - `Model` 实体仅聚焦于**“路由拓扑与调度策略”**（优先级、模型匹配、故障降级）。
- **默认值与继承**：
  提供全局兜底 `ttl.sticky`（默认 10m，覆盖 Anthropic/OpenAI 的基准窗口），特定厂商（如 DeepSeek 官网）只需在 Provider 声明一次 `sticky_ttl: 2h`，即可让该 Provider 展开出的所有模型端点自然生效。

### 2.2 为什么 `role_map` 属于 Provider 协议适配层？
- **厂商网关的 Enum 校验现状**：
  第三方提供商对 `/v1/chat/completions` 的实现通常在入口层采用静态模型校验（如 Pydantic 或固定结构体解析）。只要厂商网关没有兼容 OpenAI 新增的 `developer` 角色，该 Provider 旗下的所有模型在收到该字段时都会统一报 400 错误。
- **账号维度的适配一致性**：
  用户对接一个 Provider 账号，本质上是对接一套特定的上游 API 规范实现。在 Provider 层统一配置角色映射，能够一次性修复该账号下全部模型的协议方言差异。

### 2.3 为什么暂不引入通用的 Header/Body 规则引擎？
- **坚守字节级忠实透传（Byte-faithful Passthrough）**：
  VMR 不维护通用的 AST 中间表示，全链路使用 `internal/jsonscan` 进行零分配的字节拼接。通用 JSONPath 匹配与深度替换库要么要求反序列化为 `map[string]any`（破坏浮点数精度、重排键序、引发高并发 GC 压力），要么缺乏对数组通配符的原生就地修改能力。
- **防止自制劣质编程语言（Inner Platform Effect）**：
  通用的规则匹配（包含动作、原值引用、模糊替换）会迅速膨胀为难以调试和维护的微型解释器。保持简单、只做已知刚需，是单二进制高效路由器的生命线。

---

## 3. 现状代码分析与数据流

当前系统中，`sticky_ttl` 与 `role_map` 贯穿配置解析、快照编译与运行诊断全流程：

```
[YAML Config]
  └─ models.<name>.endpoints[].sticky_ttl
  └─ models.<name>.endpoints[].role_map
         │
         ▼ (config.validate)
  EndpointGroup 校验 (合法时长、BackstopTTL 限制)
         │
         ▼ (router.BuildSnapshot)
  遍历 models → 遍历 endpoints → 构造 core.Endpoint
         │
         ├─ core.Endpoint.StickyTTL = eg.StickyTTL (或 fallback 全局 ttl.sticky)
         └─ core.Endpoint.RoleMap   = eg.RoleMap
         │
         ▼ (Runtime Hot Path)
  ├─ router/candidates.go: time.Since(lastUsed) < ep.StickyTTL (路由判定)
  └─ adapter/request.go:   jsonscan.RewriteRoles(body, ep.RoleMap) (字节改写)
```

### 3.1 核心数据结构现状
- **`internal/config/config.go`**：
  `EndpointGroup` 结构体持有 `RoleMap map[string]string` 和 `StickyTTL *Duration`。
- **`internal/config/provider.go`**：
  `Provider` 结构体目前仅持有 `Name`, `BaseURL`, `APIKey`, `APIKeys`, `Proxy`, `Quota`, `Pricing`, `Disabled`，尚未包含这两个字段。
- **`internal/core/core.go`**：
  `core.Endpoint` 结构体持有已解析后的终态属性：`StickyTTL time.Duration` 与 `RoleMap map[string]string`。
- **`internal/router/snapshot.go`**：
  `buildEndpoints()` 遍历 `eg.Providers` 时，从 `eg.StickyTTL` 和 `eg.RoleMap` 读取配置并赋值给每个 `core.Endpoint`。
- **`internal/diagnose/diagnose.go` & `internal/replay/replay.go`**：
  由于目前 `RoleMap` 挂在 `EndpointGroup`，连通性诊断与回放工具为了获取一个 Provider 对应的 `RoleMap`，不得不反向遍历 `Config.Models` 下所有协议桶与端点组进行模式查找（如 `replay.go` 的 `resolveRoleMap` 包含三层嵌套循环）。

---

## 4. 目标设计方案

### 4.1 YAML 配置定义变更

#### 变动前（旧规范）：
```yaml
ttl:
  sticky: 10m

providers:
  - name: deepseek
    base_url:
      openai-completions: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_KEY}

  - name: dashscope
    base_url:
      openai-completions: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${DASHSCOPE_KEY}

models:
  coding:
    endpoints:
      openai-completions:
        - providers: [deepseek]
          models: [deepseek-chat]
          sticky_ttl: 2h                   # 冗余配置 1
        - providers: [dashscope]
          models: [qwen-max]
          role_map: {developer: system}   # 冗余配置 2

  agent:
    endpoints:
      openai-completions:
        - providers: [deepseek]
          models: [deepseek-reasoner]
          sticky_ttl: 2h                   # 再次重复配置
        - providers: [dashscope]
          models: [qwen-plus]
          role_map: {developer: system}   # 再次重复配置

fallback_endpoints:
  openai-completions:
    - providers: [dashscope]
      models: [qwen-plus]
      priority: 90
      role_map: {developer: system}       # 兜底端点同样被迫重复配置
```

#### 变动后（目标规范）：
```yaml
ttl:
  sticky: 10m                              # 全局基准默认值

providers:
  - name: deepseek
    base_url:
      openai-completions: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_KEY}
    sticky_ttl: 2h                         # 一次性声明：该账号下所有模型享有 2 小时磁盘缓存

  - name: dashscope
    base_url:
      openai-completions: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${DASHSCOPE_KEY}
    role_map: {developer: system}          # 一次性声明：百炼网关不支持 developer，统一转 system

models:
  coding:
    endpoints:
      openai-completions:
        - providers: [deepseek]
          models: [deepseek-chat]           # 自动继承 deepseek 的 2h
        - providers: [dashscope]
          models: [qwen-max]               # 自动继承 dashscope 的 role_map

  agent:
    endpoints:
      openai-completions:
        - providers: [deepseek]
          models: [deepseek-reasoner]       # 自动继承
        - providers: [dashscope]
          models: [qwen-plus]              # 自动继承

fallback_endpoints:
  openai-completions:
    - providers: [dashscope]
      models: [qwen-plus]
      priority: 90                         # 自动继承 dashscope 的 role_map，零重复
```

> **注意（关于 `fallback_endpoints` 的联动消除）**：
> 根级 `fallback_endpoints` 底层与虚拟模型端点复用同一个 `EndpointGroup` 结构。移除端点级配置后，`fallback_endpoints` 中的兜底候选同样自然继承 Provider 层的 `role_map` 与 `sticky_ttl`，无需为兜底链路单独做任何兼容维护。

### 4.2 结构体定义演进
1. **`internal/config/provider.go`**：
   在 `Provider` 结构体中新增字段：
   ```go
   type Provider struct {
       Name      string            `yaml:"name"`
       BaseURL   map[string]string `yaml:"base_url"`
       APIKey    string            `yaml:"api_key"`
       APIKeys   map[string]string `yaml:"api_keys"`
       Proxy     bool              `yaml:"proxy"`
       Quota     *QuotaConfig      `yaml:"quota"`
       Pricing   *ProviderPricingConfig `yaml:"pricing"`
       Disabled  bool              `yaml:"disabled"`

       // RoleMap rewrites message roles (e.g. {"developer":"system"}) for
       // all requests sent to this provider account.
       RoleMap map[string]string `yaml:"role_map"`

       // StickyTTL overrides the global ttl.sticky for all endpoints backed
       // by this provider account. nil = inherit global ttl.sticky.
       StickyTTL *Duration `yaml:"sticky_ttl"`
   }
   ```
2. **`internal/config/config.go`**：
   从 `EndpointGroup` 结构体中彻底移除 `RoleMap` 与 `StickyTTL` 字段。严格校验（`KnownFields`）将确保若用户在端点组误写这两个字段，配置加载时能立即拦截报错，杜绝配置无效的隐患。

### 4.3 多 Key 展开（`api_keys:`）天然兼容
在 `internal/config/apikeys.go` 中，`expandProviderAPIKeys` 通过浅拷贝 `child := p` 将单项 Provider 展开为 `<name>-<label>` 多个虚拟 Provider。
因此，主 Provider 上配置的 `RoleMap` 和 `StickyTTL` 指针会**自动且无缝地**复制并继承到每一个展开后的子 Key 实例中，下游无需编写任何特殊处理代码。

### 4.4 运行时与核心数据模型不变性
**`core.Endpoint` 的结构保持不变**：
```go
type Endpoint struct {
    Provider    string
    RoleMap     map[string]string // 依然挂在 Endpoint 上，供 adapter.BuildUpstreamRequest 使用
    StickyTTL   time.Duration     // 依然挂在 Endpoint 上，供 router.Serve/candidates 判定
    ...
}
```
通过将配置在 `router.BuildSnapshot` 构建阶段直接从 `Provider` 注入到 `core.Endpoint`，所有运行时热路径（`internal/router`、`internal/adapter`、`internal/sticky`、`internal/respnorm`）的调用逻辑和高性能字节扫描**完全不需要改动**。

---

## 5. 详细实施改造清单

### 5.1 配置解析与校验（`internal/config/`）
1. **`provider.go`**：
   - 为 `Provider` 添加 `RoleMap` 与 `StickyTTL` 字段及注释。
2. **`config.go`**：
   - 从 `EndpointGroup` 删除 `RoleMap` 与 `StickyTTL`。
3. **`config_validate.go`**：
   - 移除原 `validateEndpointGroup` 中关于 `eg.StickyTTL` 的正数与 `StickyBackstopTTL`（24h）校验。
   - 在 `validateProviders` 中添加针对 `p.StickyTTL` 的校验：
     - 检查 `p.StickyTTL.D() > 0`。
     - 检查 `p.StickyTTL.D() <= core.StickyBackstopTTL`（超过直接报错并给出友好提示）。
   - 校验 `p.RoleMap`：
     - 若 `len(p.RoleMap) == 0`，在校验阶段将其规整（reset）为 `nil` 并写回 `c.Providers[i].RoleMap = nil`，避免无意义的空 map 分配与逃逸。
     - 遍历键值对：`strings.TrimSpace(k) == ""` 或 `strings.TrimSpace(v) == ""` 时拒绝加载。
     - 禁止自身映射到自身（`k == v`，如 `system: system`），防止无意义的配置干扰。
4. **单元测试迁移**：
   - 将 `config_test.go` 中针对端点组 `role_map` / `sticky_ttl` 的测试用例重构为针对 `Provider` 层级的测试（包括缺省继承全局、覆盖生效、非法负数、超过 24 小时硬上限拦截等用例）。

### 5.2 快照生成与端点构建（`internal/router/`）
1. **`snapshot.go`**：
   - 在 `buildEndpoints` 遍历 `eg.Providers` 内部，直接使用已查出的 Provider 实例 `p`：
     ```go
     epStickyTTL := globalStickyTTL
     if p.StickyTTL != nil {
         epStickyTTL = p.StickyTTL.D()
     }
     ...
     RoleMap:   p.RoleMap,
     StickyTTL: epStickyTTL,
     ```
2. **测试用例调整**：
   - 更新 `router_test.go` 与 `snapshot_equiv_test.go`，适配新的 YAML 结构与断言。

### 5.3 诊断与回放工具简化（`internal/diagnose/` & `internal/replay/`）
1. **`internal/replay/replay.go`**：
   - 废除原有的三层嵌套扫描函数 `resolveRoleMap`。
   - 直接通过 `cfg.ProviderByName(opts.Provider)` 一行代码取得 `p.RoleMap`，极大精简代码量。
2. **`internal/diagnose/diagnose.go`**：
   - **消除历史妥协**：原 `collectEndpointTriples` 返回 `map[epKey]map[string]string`，其内部注释曾指出“跨虚拟模型若对同一三元组声明不同 role_map 无法调和”。收敛至 Provider 后，`collectEndpointTriples` 签名直接简化为返回 `[]epKey`（纯去重三元组列表），构造探测端点时直接通过 `p.RoleMap` 取值，彻底拔除这处历史残留妥协。
   - **优化失败 Hint 提示语**：
     当 `RoleCompatRequest` 失败且端点未配 `RoleMap` 时，提示语从原先模糊的指导升级为精准点名 Provider：
     ```go
     hint = fmt.Sprintf(` — no role_map configured; if provider %q rejects the "developer" role, add role_map: {developer: system} to its provider definition in config.yaml`, ep.Provider)
     ```

### 5.4 命令行输出与展示（`cmd/vmr/`）
1. **`cmd_check.go`**：
   - 在 `=== Providers ===` 概览区块中，排版对齐展示 Provider 声明的自定义 `sticky_ttl` 与 `role_map`：
     ```
     dashscope:
       api_key: sk-***...
       base_url(openai-completions): https://dashscope...
       proxy: direct
       role_map: developer->system
       sticky_ttl: 2h
     ```
   - 在 `=== Models ===` 路由表展示阶段，端点行（`Endpoint`）依然完整显示生效的 `sticky_ttl` 与 `role_map`：
     - `role_map`：非空时输出（如 `role_map=developer->system`）；
     - `sticky_ttl`：当且仅当 `ep.StickyTTL != cfg.TTL.Sticky.D()`（即与全局 10m 默认值不同）时输出，保持输出精简清晰。

### 5.5 文档与示例文件同步
1. **示例文件更新**：
   - `config.example.yaml` 与 `config.example.zh.yaml`：将 DeepSeek 的 `sticky_ttl: 2h` 与百炼的 `role_map: {developer: system}` 迁移至 `providers:` 节点下，并在注释中阐明最佳实践。
   - `config.mock.yaml`：同步更新测试 mock 配置。
2. **文档同步更新（保持中英一致）**：
   - `docs/UserGuide.md` 与 `docs/UserGuide.zh.md`：更新“角色映射”与“会话粘性”章节，说明字段位于 Provider 层级。
   - `docs/VirtualModelRouter_Design_v4_Core.md`：
     - 更新 Sticky Model 章节与决策表（原对比为“端点 vs 虚拟模型”，更新为“Provider 级 vs 端点级”并阐明收敛原因）；
     - 更新第 106 行关于 `role_map` 挂在端点组的历史描述，对齐为当前 Provider 级架构。

### 5.6 测试用例影响与迁移矩阵

| 测试文件 | 原测试函数 / 位置 | 改造意图与断言迁移 |
| :--- | :--- | :--- |
| `internal/config/config_test.go` | `TestRoleMapConfig`, `TestRoleMapUnsetIsNil` | 迁移为断言 `cfg.Providers[0].RoleMap`，测试缺省为 nil、正常解析与空 map 规整 |
| `internal/config/config_test.go` | `TestStickyTTLPerEndpointOverride`, `TestStickyTTLPerEndpointAboveBackstopRejected` | 迁移为断言 `cfg.Providers[0].StickyTTL`，测试合法值、负数拒绝、超过 24h 硬上限拒绝 |
| `internal/router/snapshot_equiv_test.go` | 测试 fixture 中端点行的 `role_map` / `sticky_ttl` | 挪至对应的 `providers:` 节点下，验证快照生成后 `core.Endpoint` 的等价性 |
| `internal/router/router_test.go` | `TestBuildSnapshot_RoleMap`, `TestBuildSnapshot_StickyTTL` | 更新 YAML fixture，验证模型端点正确继承 Provider 上的 `role_map` 与 `sticky_ttl` |
| `internal/server/sticky_test.go` | `TestSticky_TTLExpiry` | 端到端 HTTP 集成测试 fixture 改造：将 `sticky_ttl: 200ms` 从端点移动至 Provider `p2` 定义行 |
| `internal/replay/replay_test.go` | `TestResolveRoleMap_*` (4 个测试) | 原函数废除，改测 Provider 级直取逻辑与缺省 nil 行为 |
| `internal/diagnose/diagnose_test.go` | `TestTestEndpoint_OpenAIDeveloperRole_*` | fixture 中将 `role_map` 配置在 `providers` 下，验证诊断器正向通过与错误提示文本匹配 |

### 5.7 `CHANGELOG.md` 登记与 Breaking Change 迁移指引

在 `CHANGELOG.md` 的 `[Unreleased]` 区域登记：

```markdown
### Changed
- **Config Breaking Change**: `sticky_ttl` and `role_map` moved from `EndpointGroup` (`models.<name>.endpoints[<protocol>][]`) to `Provider` (`providers[]`). Old configs specifying these fields under endpoints will be rejected at load due to strict YAML validation (`KnownFields: true`).
  - **Migration**: Cut `sticky_ttl` and `role_map` from individual endpoint entries and place them once under the matching provider in `providers:`.
```

---

## 6. 影响面评估与架构保证

1. **零运行时性能开销**：
   改动纯粹在配置加载与快照构建期完成。生成后的 `core.Endpoint` 数据形状与改动前完全同构，请求分发路由（`Serve` / `tryOne`）与字节改写引擎零变动。
2. **架构守卫与约束遵循**：
   - `internal/config` 依然遵守 YAML 严格解析（`KnownFields`），旧版端点级配置无法混入。
   - `Provider` 相关的修改发生在 `internal/config/provider.go`，有效规避 `config.go` 触碰行数预算限制。
   - 代码不引入任何外部重型三方库，保持零外部依赖与单二进制轻量性。
