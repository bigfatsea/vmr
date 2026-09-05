# 任务说明书:Pkg-A — 时间字段归集(`timeouts:`/`ttl:`)+ Duration 文法统一 + TTL 零值消歧

> 背景文档:`ConfigShape_Simplification_2026-09_v2.md` 的 A.1 / A.2 / A.3 节(已 cp 到你的 worktree 根目录)。以该文档为准,本 spec 是其落地指令。

## 一、协作原则与红线约束(铁律)

1. **工作区限制**:仅在分配给你的 worktree 目录下操作。
2. **文件修改白名单(极度关键)**:
   - ✅ 允许修改:
     - `internal/config/config.go`(Duration 类型、Timeouts/Config 结构、applyDefaults)
     - `internal/config/config_validate.go`(validateBasic)
     - `internal/config/check.go`(仅 `checkTimeouts` 函数)
     - `internal/server/server.go`(仅 `CacheTTLDays` 一处调用点的换算)
     - `cmd/vmr/cmd_check.go`、`cmd/vmr/cmd_start.go`、`cmd/vmr/main_test.go`
     - `internal/config/config_test.go`、`internal/config/check_test.go`
     - 新建测试文件:`internal/config/ttl_test.go`
     - `config.example.yaml`、`config.example.zh.yaml`(仅顶层时间字段相关段落)
     - `docs/UserGuide.md`、`docs/UserGuide.zh.md`(仅时间字段与零值语义相关段落 + A.3 一句话)
   - ❌ 严禁修改白名单以外任何文件,尤其:`internal/router/**`、`internal/config/provider.go`、`internal/config/apikeys.go`、`internal/config/quota.go`、`config.minimal*.yaml`、`CHANGELOG.md`、`KNOWN_ISSUES.md`、`docs/VirtualModelRouter_Design_v4_*.md`、`config.mock.yaml`(不存在于你的 worktree,不要去找)。
3. **代码风格**:注释只写非显然的"why",英文, terse 风格;遵循包内既有注释密度。禁止 narration 注释。
4. **Git 规范**:commit message 短、祈使句、**无任何 trailer**(尤其 Co-Authored-By)。只 `git add <白名单内具体文件>`,严禁 `git add .`/`git add -A`。
5. **共享文件禁改**:CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占。待登记项写入 **不提交** 的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**:`_tmp/`、`archived/` 视为不存在。
7. **并发抗干扰**:同仓库可能有其他 Agent 并行作业;`git status` 看到的无关改动/未跟踪文件一律不探查、不触碰、不回滚。暂存只精准指定白名单文件。
8. **会打红的白名单外测试**:本改动会移除 YAML 字段,若发现白名单外测试引用了旧字段且无法不改而通过(理论上不应发生——`internal/router` 不消费这些字段),**停止并记录到 NOTES_FOR_LEAD.md,交回主控**,不得擅自越界修改。

## 二、具体任务清单(Action Plan)

### 任务 1:字段归集与更名(A.1)

新形态(顶层):

```yaml
timeouts:            # 等多久:请求路径上限,Go Duration 语法
  connect: 10s
  response_header: 120s
  stream_idle: 120s
  probe: 15s         # ← 从顶层 probe_timeout 移入

ttl:                 # 活多久:生命周期/淘汰
  sticky: 10m        # Go Duration 语法(min/h 量级);≤ core.StickyBackstopTTL 校验原样保留
  image_cache: 14d   # 扩展语法
  audit_retention: 90d
```

- `Config` 删除顶层字段 `ProbeTimeout` / `StickyTTL` / `ImageCacheTTLDays` / `AuditRetentionDays`;`Timeouts` 增加 `Probe`;新增 `TTL` 结构(三个字段如上)。
- `applyDefaults` 同步:probe/sticky/image_cache/audit_retention 的默认值逻辑搬入新位置,常量语义不变(DefaultProbeTimeout/DefaultStickyTTL/DefaultImageCacheTTLDays;新增 audit retention 默认 90d)。

### 任务 2:扩展 Duration 文法(A.1+A.2)

- 新增类型(命名自定,如 `CalendarDuration`)供 `ttl.image_cache` / `ttl.audit_retention` 使用:接受 `Nd / Nw / Nmo / Ny`(不区分大小写;d=24h、w=7d、mo=30d、y=365d)。
- **显式拒绝** `forever` / `permanent` / `never` 关键字 → load error,报错信息说明"不支持永久语义,需要长期保留请写具体大数(如 90000d)"。
- 现有 `Duration`(Go 语法)保持不变,继续服务 `timeouts.*` 与 `ttl.sticky`。
- 零值语义:`0`(含 `0d`)与未写同义 = 用默认;负数同样走默认(applyDefaults 以 `<= 0` 判定)。不引入指针三态。在类型注释中写明这套极性及"永久语义已被取消"。

### 任务 3:下游消费点与 Breaking 语义(A.2)

- `audit_retention` 新语义:**0/未写 = 用默认 90d**(旧"0 = 永不删除"铲除)。这是 Breaking Change。
  - `cmd_start.go` 两处 `audit.SetRetentionDays(...)`:由 TTL 换算天数,换算用**向上取整**(亚天数值至少 1 天,避免 12h 被换成 0 而意外落入 audit 包"0 = 不删"的旧语义)。在 NOTES_FOR_LEAD.md 记录该换算决策。
- `cmd_check.go` 的 `audit_retention` 显示行与 probe 相关显示行随字段搬家更新(显示格式如 `90d`)。
- `internal/server/server.go` 的 `imgprep.CacheTTLDays`:调用点换算(同样向上取整、正值至少 1 天),不改 imgprep 包签名。
- `config_validate.go`:删除 `audit_retention_days >= 0` / image_cache 相关旧校验;sticky backstop 校验改为对 `ttl.sticky`。

### 任务 4:UserGuide 与示例配置(含 A.3)

- `config.example.yaml` + `.zh`:顶层时间字段段落改为新形态,注释同步(含 audit_retention 的新零值语义警告:"依赖旧'永不删除'语义请写 90000d 或更大")。
- UserGuide en/zh:对应字段表/段落改写;**逐字段显式标注零值含义**(TTL 三字段"0=用默认";顺带在 max_attempts/max_concurrency 处标明"0=无上限"——这是 A.2 的登记要求);`audit_retention` 处加 Breaking 警告与迁移写法。
- A.3:在 provider/api_keys 相关段落补一句:"proxy 决定该 provider 所有连接的走向,是一次连接期选择,与 api_key/api_keys 的账号展开正交"。中英同步。

### 任务 5:测试

- 新建 `internal/config/ttl_test.go` 覆盖:d/w/mo/y 各单位解析与换算、大小写不敏感、`forever/permanent/never` 拒绝、`0d`/未写/负数 → 默认、`ttl.sticky` 超 backstop 报错、`timeouts.probe` 默认值。
- 更新 `config_test.go` / `check_test.go` / `main_test.go` 中引用旧字段名的用例(main_test.go 的 `audit_retention_days: 30` 与期望显示 `30d` 等)。
- 不削弱任何既有断言;语义断言按新语义改写并保持强度。

## 三、测试与验收步骤

1. `gofmt -l internal/ cmd/` 无输出。
2. `go build -o /tmp/vmr-a ./cmd/vmr`。
3. `go vet ./internal/config/... ./cmd/... ./internal/server/...`。
4. `go test -race ./internal/config/... ./internal/server/... ./cmd/...` 全绿。
5. `go test ./internal/archtest/...` 全绿(你改了 config.go/config_validate.go 行数,注意 per-file/per-function 行预算;确需超限,在 NOTES_FOR_LEAD.md 说明并只上调对应表项——archtest 的预算表本身在白名单外,需要上调时写入 NOTES 交主控执行)。
6. `git status -s` 确认无越界文件;`NOTES_FOR_LEAD.md` 不提交。
7. Commit(可多个):如 `config shape: gather time fields under timeouts/ttl with extended duration units`。
