# 任务说明书:Pkg-E — `model_defaults` 声明机制 + 移除 endpoint 级覆盖

> 背景文档:`ConfigShape_Simplification_2026-09_v2.md` 的 E.1 / E.2 节(已 cp 到你的 worktree 根目录)。以该文档为准,本 spec 是其落地指令。
> 前置:本分支基线已含 Pkg-A/C/D 的改动(endpoints 已是 protocol-keyed map 形态)——基于其上工作。

## 一、协作原则与红线约束(铁律)

1. **工作区限制**:仅在分配给你的 worktree 目录下操作。
2. **文件修改白名单(极度关键)**:
   - ✅ 允许修改:
     - `internal/config/config.go`(新 ModelDefaults 结构;EndpointGroup 删 Capabilities/MaxContextTokens;VirtualModel 注释更新)
     - `internal/config/config_validate.go`(model_defaults 校验;删除 endpoint 级两字段的校验)
     - `internal/config/apikeys.go`(展开重写需覆盖 model_defaults[].providers)
     - `internal/router/snapshot.go`(buildEndpoints 改查表解析)
     - `internal/core/core.go`(删 Endpoint.ExtraCapabilities / OwnMaxContextTokens 及其注释)
     - `cmd/vmr/cmd_check.go`(删 extra_capabilities / own-max 显示片段)
     - 测试:`internal/config/config_test.go`、`internal/router/router_test.go`,以及**新建** `internal/config/model_defaults_test.go`
     - `config.example.yaml`、`config.example.zh.yaml`(新增 model_defaults 块;models 段落去掉 endpoint 级覆盖)
     - `docs/UserGuide.md`、`docs/UserGuide.zh.md`(condition-routing/capabilities 相关章节重写)
     - `docs/VirtualModelRouter_Design_v4_Core.md`(多模态能力章节端点 override 例子重写为新写法;仅此范围)
   - ❌ 严禁修改白名单以外任何文件,尤其:`cmd/vmr/main_test.go` 以外其他 cmd 测试、`internal/strategy/**`、`internal/server/**`、`CHANGELOG.md`、`KNOWN_ISSUES.md`、其他设计文档、`config.mock.yaml`(不存在于你的 worktree,不要去找)。
3. **代码风格**:注释只写非显然的"why",英文,terse。
4. **Git 规范**:commit message 短、祈使句、无 trailer;只精准 `git add` 白名单文件。
5. **共享文件禁改**:CHANGELOG / KNOWN_ISSUES / 其余设计文档由主控独占;待登记项写 **不提交** 的 `NOTES_FOR_LEAD.md`(本包必记:被移除字段清单 + 一行人工映射说明,供 CHANGELOG 用)。
6. **忽略目录**:`_tmp/`、`archived/`。
7. **无兼容层**:endpoint 级字段直接删,旧写法 = unknown field load error。
8. **会打红的白名单外测试**:若 `internal/server` 或其他白名单外测试引用了被删字段,停止并记录 NOTES 交回主控。

## 二、具体任务清单(Action Plan)

### 任务 1:schema

```yaml
# 顶层,所有虚拟模型共享;整块可省略(省略 = 一切 unconstrained)
model_defaults:
  "*":                          # 通配(可选)
    capabilities: [text, tools]
  MiniMax-M3:                   # key = 真实模型名
    capabilities: [text, tools, image, audio, video, thinking]
    max_context_tokens: 512000
    providers: [openrouter, minimax]   # 不写 = 对所有 provider 生效
```

- 新增 `Config.ModelDefaults map[string]ModelDefaultEntry`;entry = `{Providers []string, Capabilities []string, MaxContextTokens int64}`(具体字段命名/类型自定,语义如上)。
- `EndpointGroup` 删除 `Capabilities` / `MaxContextTokens` 字段;`VirtualModel` 两字段**保留**为显式覆盖(注释改写:覆盖 model_defaults,而非"endpoint 覆盖的 base")。
- 关键语义登记(写入类型注释):
  - 两字段**按维度独立回退**,不整条 entry 绑定;
  - "未声明 = unconstrained" 是机制本体承诺(`core.Endpoint.HasCapability` 语义不变,查表无匹配仍落到空集/0 路径);
  - 同一模型名重复 key 由 YAML 自身拒绝,**无合并规则**;exact key 与 `"*"` 通配同时匹配时 exact 优先(是回退链,不是合并)。

### 任务 2:解析顺序(buildEndpoints 查表)

对每个 `(virtualModel, provider, model)`,每个字段独立:

1. 虚拟模型显式声明(capabilities 非空 / max_context_tokens > 0)→ 用之;
2. `model_defaults[model]` 存在且(providers 未写 OR provider ∈ providers)且**该字段在此 entry 中有值** → 用之;
3. `model_defaults["*"]` 同理 → 用之;
4. 否则 unconstrained(空 / 0)。

注意第 2/3 步是**按字段**判断:exact entry 只写了 max_context_tokens 时,capabilities 仍继续向 `"*"`/unconstrained 回退。

### 任务 3:字段删除与消费点

- `core.Endpoint` 删除 `ExtraCapabilities` / `OwnMaxContextTokens`;`Capabilities`/`MaxContextTokens` 含义不变(已解析的最终值)。条件路由接口零变化(`capabilityCondition.Eligible`/`strategy.WithinContext` 不动)。
- `cmd_check.go`:删除 `extra_capabilities=` 与 own-max 显示片段。
- `apikeys.go`:providers 引用重写扩展到 `model_defaults[*].Providers`(与 endpoints/fallback 同一套 rewrite 逻辑),保证 api_keys 展开后引用一致。

### 任务 4:校验

- `model_defaults[].providers` 中出现未知 provider 名 → load error(fail-fast;在 apikeys 展开之后校验,展开后的名字才有效)。
- `providers` 若出现则不得为空列表;`max_context_tokens >= 0`;key 非空(`"*"` 为保留通配,单独成 key)。
- VirtualModel 覆盖字段沿用既有校验(max_context_tokens >= 0)。

### 任务 5:测试与文档

- 新建 `internal/config/model_defaults_test.go`:三档优先级(虚拟模型覆盖 > exact > `"*"`)、per-field 独立回退、providers 子集限定、未知 provider load error、整块省略 = unconstrained。
- 更新 `router_test.go` 中 ExtraCapabilities/OwnMaxContextTokens 相关断言(按新语义改写,不削弱)。
- 示例配置:按背景文档 E.2 的 mock 演算(model_defaults 写 MiniMax-M3 事实一次;agent 不写;cheap 虚拟模型层显式降级 128000)改写 models 段。
- UserGuide + Core.md 多模态章节:端点 override 例子改写为 model_defaults 写法;说明 sticky key 含虚拟模型名、不受本机制影响(背景文档 E.2 末段)。

## 三、测试与验收步骤

1. `gofmt -l internal/ cmd/` 无输出;`go build ./...`;`go vet ./...`。
2. `go test -race ./internal/config/... ./internal/router/... ./cmd/... ./internal/server/...` 全绿。
3. `go test ./internal/archtest/...` 全绿(行预算按 NOTES 流程申请上调)。
4. `git status -s` 无越界;`NOTES_FOR_LEAD.md` 不提交。
5. Commit:如 `config shape: hoist capabilities/context to model_defaults, drop endpoint-level override`。
