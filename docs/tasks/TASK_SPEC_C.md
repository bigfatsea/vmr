# 任务说明书:Pkg-C — `Provider.Disabled` 临时下线开关

> 背景文档:`ConfigShape_Simplification_2026-09_v2.md` 的 C.1 节(已 cp 到你的 worktree 根目录)。以该文档为准,本 spec 是其落地指令。

## 一、协作原则与红线约束(铁律)

1. **工作区限制**:仅在分配给你的 worktree 目录下操作。
2. **文件修改白名单(极度关键)**:
   - ✅ 允许修改:
     - `internal/config/provider.go`(Provider 结构加字段)
     - `internal/config/check.go`(仅 `checkProviders` 及新增一个 check 函数)
     - `internal/router/snapshot.go`(BuildSnapshot/BuildQuotaSpecs 过滤)
     - **新建**测试文件:`internal/config/provider_disabled_test.go`、`internal/router/snapshot_disabled_test.go`(测试只放新文件)
     - `config.example.yaml`、`config.example.zh.yaml`(仅 providers 段落,加注释形态示例)
     - `docs/UserGuide.md`、`docs/UserGuide.zh.md`(仅 provider 相关段落新增小节)
   - ❌ 严禁修改白名单以外任何文件,尤其:`internal/config/config.go`、`config_validate.go`、`apikeys.go`、`config_test.go`、`check_test.go`、`cmd/**`、`config.minimal*.yaml`、`CHANGELOG.md`、`KNOWN_ISSUES.md`、设计文档、`config.mock.yaml`(不存在于你的 worktree,不要去找)。
3. **代码风格**:注释只写非显然的"why",英文,terse 风格。
4. **Git 规范**:commit message 短、祈使句、**无任何 trailer**。只 `git add <白名单内具体文件>`,严禁 `git add .`。
5. **共享文件禁改**:CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占。待登记项写入 **不提交** 的 `NOTES_FOR_LEAD.md`。
6. **忽略目录**:`_tmp/`、`archived/` 视为不存在。
7. **并发抗干扰**:同仓库可能有其他 Agent 并行作业(特别注意:另一个 Agent 可能正在改 `check.go` 的 `checkTimeouts`/`config.go` 的时间字段——与你无关,你的 diff 必须不波及那些区域)。`git status` 看到的无关改动一律不触碰。
8. **会打红的白名单外测试**:预期不会打红(现有测试没有 disabled: true 的配置)。若确有白名单外测试因你的改动失败,**停止并记录到 NOTES_FOR_LEAD.md 交回主控**,不得擅自越界修改、不得削弱断言。

## 二、具体任务清单(Action Plan)

### 任务 1:字段与语义

- `Provider` 增加 `Disabled bool`(yaml `disabled`),注释写明:语义是"在所有下游消费点把它当作不存在",不是"标记但保留数据";默认 false = 在线(常态)。
- `expandProviderAPIKeys` 的子账户展开按结构体复制,`Disabled` 自动继承——在 apikeys 相关注释或测试中确认这一点(不改 apikeys.go 代码本身)。

### 任务 2:单点过滤(internal/router/snapshot.go)

- **过滤点必须单一**:在 BuildSnapshot 入口构建 `enabled` 集合(或单一 helper),下游(buildEndpoints 的 (provider,model) 展开、fallback 展开、BuildQuotaSpecs)一律经它判定;**严禁**在多处各自散写 `p.Disabled` 判断,否则 reload 序列上易漂移。
- `BuildQuotaSpecs` 跳过 disabled provider(不为已下线账户留无主计数器)。
- `/status` 经 BuildSnapshot 间接受益,零改动(验证即可,不加观察通道)。
- 热重载无需新机制(快照原子换指针、sticky 自行过滤),注释提及即可,不写代码。

### 任务 3:check 告警(internal/config/check.go)

- `checkProviders`:disabled provider **跳过** "api_key missing" 检查(已下线账户缺 key 是噪音)。
- 新增检查:某 provider 出现在 `models[].endpoints[].providers` 或 `fallback_endpoints[].providers` 中但 `disabled: true` → `SeverityWarning`(不是 error):"provider %q is disabled but still referenced by ...; it carries no traffic until re-enabled"。合法演进路径(改回 false + reload 即恢复),但必须让 `vmr check` 说出来。
- validate 不动:disabled provider 仍参与全部结构校验(base_url、quota 等);"被引用但未声明"照旧是 error。

### 任务 4:测试(只写新文件)

- `internal/config/provider_disabled_test.go`:
  - disabled provider 被引用 → validate 通过 + Check 出 warning;
  - disabled provider 缺 api_key → 不再出 api_key missing issue;
  - api_keys 展开的子账户继承 disabled;
  - unreferenced + disabled → 无新增告警(维持现状)。
- `internal/router/snapshot_disabled_test.go`:
  - disabled provider 不出现在任何 route(含 fallback 注入);
  - disabled provider 不产生 QuotaSpec。

### 任务 5:文档与示例

- `config.example.yaml` + `.zh`:providers 段加一行注释形态 `# disabled: true  # 临时下线整个 provider(含其全部 endpoint 展开),改回 false 并 reload 即恢复`。
- UserGuide en/zh:provider 段新增短小节 "Temporarily disabling a provider"(语义边界:等价于不存在;/status 不出现;热重载即生效;引用它的 endpoint 不报错但有 check 警告)。中英同步。

## 三、测试与验收步骤

1. `gofmt -l internal/` 无输出。
2. `go build -o /tmp/vmr-c ./cmd/vmr`。
3. `go vet ./internal/config/... ./internal/router/...`。
4. `go test -race ./internal/config/... ./internal/router/... ./internal/server/...` 全绿。
5. `go test ./internal/archtest/...` 全绿(snapshot.go 有行预算,必要时按 Pkg-A 同样的 NOTES 流程申请上调)。
6. `git status -s` 确认无越界文件;`NOTES_FOR_LEAD.md` 不提交。
7. Commit:如 `config: add provider disabled switch for temporary takedown`。
