# 任务说明书:Pkg-D — `endpoints` / `fallback_endpoints` 二层 map 化 + fallback 不可达告警

> 背景文档:`ConfigShape_Simplification_2026-09_v2.md` 的 D.1 / D.2 节(已 cp 到你的 worktree 根目录)。以该文档为准,本 spec 是其落地指令。
> 前置:本分支基线已含 Pkg-A(ttl/timeouts 归集)与 Pkg-C(Provider.Disabled)的改动——基于它们之上工作,勿回退其内容。

## 一、协作原则与红线约束(铁律)

1. **工作区限制**:仅在分配给你的 worktree 目录下操作。
2. **文件修改白名单(极度关键)**:
   - ✅ 允许修改:
     - `internal/config/config.go`(EndpointGroup 删 Protocol 字段;FallbackEndpoints 类型;相关注释)
     - `internal/config/config_validate.go`(map key 校验外提;错误 ctx 改 `endpoints.<protocol>[#N]` 形态)
     - `internal/config/apikeys.go`(providers 引用重写循环适配 map 形态)
     - `internal/config/check.go`(checkModels 适配 + 新增不可达 fallback warning)
     - `internal/router/snapshot.go`(BuildSnapshot 按 map 分桶;fallback 按协议 key 直查)
     - 测试:`internal/config/config_test.go`、`check_test.go`、`apikeys_test.go`、`internal/router/router_test.go`,以及**新建** `internal/router/snapshot_equiv_test.go`(差分等价测试)
     - `config.example.yaml`、`config.example.zh.yaml`、`config.minimal.yaml`、`config.minimal.zh.yaml`(endpoints/fallback 段落 map 化)
     - `docs/UserGuide.md`、`docs/UserGuide.zh.md`(endpoints/fallback 语法段落)
     - `docs/VirtualModelRouter_Design_v4_Core.md`(仅配置参考/示例中出现旧形态的片段,改写为新形态;不做其他内容改动)
   - ❌ 严禁修改白名单以外任何文件,尤其:`internal/core/**`、`cmd/**`、`internal/config/quota.go`、`pricing.go`、`provider.go`、`CHANGELOG.md`、`KNOWN_ISSUES.md`、其他设计文档、`config.mock.yaml`(不存在于你的 worktree,不要去找)。
3. **代码风格**:注释只写非显然的"why",英文,terse。
4. **Git 规范**:commit message 短、祈使句、无 trailer;只精准 `git add` 白名单文件。
5. **共享文件禁改**:CHANGELOG / KNOWN_ISSUES / 其余设计文档由主控独占;待登记项写 **不提交** 的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**:`_tmp/`、`archived/`。
7. **无兼容层**:严格 YAML下旧形态(条目内 `protocol:` 字段)直接成为 unknown field 报错——这是设计意图,不做任何兼容/迁移代码。
8. **会打红的白名单外测试**:理论无(所有构建 EndpointGroup 的测试都在白名单内)。若有例外,停止并记录 NOTES 交回主控。

## 二、具体任务清单(Action Plan)

### 任务 1:schema map 化(D.1)

新形态:

```yaml
agent:
  endpoints:
    openai-completions:
      - providers: [openrouter, openrouter2]
        models: [gemini-3.6-flash-high, gemini-3.1-pro-high]
      - providers: [minimax]
        models: [MiniMax-M3]
        max_context_tokens: 512000      # per-entry 覆盖字段原样保留
    anthropic-messages:
      - providers: [minimax]
        models: [MiniMax-M3]

fallback_endpoints:
  openai-completions:
    - providers: [openrouter2]
      models: [z-ai/glm-5.2]
      priority: 90
```

- `EndpointGroup` 删除 `Protocol` 字段;`VirtualModel.Endpoints` 改为 `map[string][]EndpointGroup`(key=protocol);`Config.FallbackEndpoints` 同样改为 `map[string][]EndpointGroup`。
- per-entry 字段(priority/models/providers/role_map/sticky_ttl/capabilities/max_context_tokens/soft_block_failover)语义零变化;组内 list 保序,跨协议 bucket 无序。
- 类型注释更新:与 `provider.base_url` 同构的论证、map key 过 adapter registry 校验、fallback priority 仍必填 >0。

### 任务 2:校验与错误定位

- map key 层校验协议( `adapter.Get` + unknownProtocolHint),错误信息含 key 名。
- 条目级错误 ctx 从 "endpoint group #N" 改为 `model %q endpoints.<protocol>[#N]` / `fallback_endpoints.<protocol>[#N]`。
- `validateFallbackEndpoints` 的 priority>0 约束保留,ctx 随新形态。

### 任务 3:下游消费点

- `apikeys.go`:`models[].endpoints` 与 `fallback_endpoints` 的 providers 引用重写循环适配 map(注意 map 遍历写回需按 key 整体替换,遵循现有 changed-标记模式)。
- `snapshot.go` BuildSnapshot:`for protocol, groups := range m.Endpoints`;fallback 改为对每个已有 route 的 protocol 直查 `cfg.FallbackEndpoints[protocol]`(语义与现状一致:只 augment 已有入口,never open new ingress;`fallback: false` opt-out 保留)。

### 任务 4:不可达 fallback 告警(D.2)

- `Config.Check` 新增:某 `fallback_endpoints` 的 protocol 在**所有** VirtualModel 的 endpoints key 中都不存在 → `SeverityWarning`:"fallback protocol %q matches no virtual model — it will never be used"(合法演进路径,但必须说出来)。每个不可达 protocol 一条,不逐条目展开。

### 任务 5:差分等价测试(必须)

- 新建 `internal/router/snapshot_equiv_test.go`:测试文件内定义 legacy 形态结构(带 `protocol` 字段的 EndpointGroup + list 形态 wrappers)与转换函数;同一份配置分别以 legacy-YAML→转换→BuildSnapshot 与 new-shape YAML→Parse→BuildSnapshot 两条路径展开,断言 (protocol, provider, model, priority) 展开序列完全一致。形态等价性靠此测试钉住,不靠论证。

### 任务 6:测试更新与文档

- 更新所有以旧形态构造 EndpointGroup 的既有测试(config_test/check_test/apikeys_test/router_test)——保持断言强度,只改形态。
- 示例配置与 UserGuide(及 Core.md 中出现旧形态的片段)同步 map 形态;UserGuide 说明 map 失序无害、组内保序、错误定位改善。

## 三、测试与验收步骤

1. `gofmt -l internal/` 无输出;`go build ./...`;`go vet ./...`。
2. `go test -race ./internal/config/... ./internal/router/... ./cmd/... ./internal/server/...` 全绿。
3. `go test ./internal/archtest/...` 全绿(config.go/snapshot.go 行预算变化时按 NOTES 流程申请上调)。
4. `git status -s` 无越界;`NOTES_FOR_LEAD.md` 不提交。
5. Commit:如 `config shape: bucket endpoints and fallback_endpoints by protocol`。
