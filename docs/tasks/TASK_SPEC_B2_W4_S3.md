# 任务说明书：Batch 2 - Worker 4（S-3 主线：记录事实，停止反推）

> 全表 ROI 最高的主线。修的不是三个 bug，而是"分析半区靠 (status, error) 反推路由半区行为"
> 这一整类漂移。两个已证实的错误形态：
> ① softblock（`router/softblock.go:88`：2xx + `ErrorClass=content`，不进 `forwardSuccess`，
>    配额零扣）被 `report/ingest.go:146` 的 `a.HasResponse && a.Status < 400` 误计为 Forwarded，
>    打破 `rows.go:266-273` 明写的恒等式「重算列 = Forwarded × multiplier」；
> ② 截断 anthropic 流：`message_start` 的 Out≈1 占位值被 `chatmsg/usage.go:88` 的
>    `u.In > 0 || u.Out > 0` 析取判定为"usage 可信"，§2.5 重算得 Out=1 而路由实扣
>    `max(1, outEst)`——系统性少算 output 且渲染成精确值。

## 一、协作原则与红线约束（铁律）

1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：
     - `internal/audit/audit.go`、`internal/audit/audit_test.go`
     - `internal/router/router.go`（限 forwardSuccess 及其紧邻的 attempt 记账）、`internal/router/router_test.go`
     - `internal/chatmsg/usage.go`、`internal/chatmsg/usage_test.go`
     - `internal/respnorm/usagesniff.go`、`internal/respnorm/usagesniff_test.go`
     - `internal/ctxgraph/manifest.go` 及其测试文件
     - `internal/report/ingest.go`、`recextract.go`、`session.go`、`rows.go`、`providerquota.go`（后者仅在 Forwarded 语义需要时只读性小调）及对应 `*_test.go`
     - `internal/replay/replay.go`（限 chargeReplay/recordView 及其紧邻）、`internal/replay/replay_test.go`
     - `internal/story/cost.go`、`internal/story/cost_test.go`
     - `cmd/vmr/quota_parity_test.go`
   - ❌ 严禁修改：白名单以外任何文件。**严禁修改 `CHANGELOG.md`、`docs/KNOWN_ISSUES.md`、`docs/` 下任何设计文档**——待登记项全部写进不提交的 `NOTES_FOR_LEAD.md`。
3. 语义变更预警（本任务必然打红的既有测试）：
   - `ctxgraph`/`report`/`story` 中所有围绕单布尔 `UsageOK` 的断言；
   - `replay` 的 chargeReplay 计费断言；
   - `quota_parity_test.go` 的 fixture 形状。
   期望处理方式：允许更新 fixture 与断言以匹配**新语义**，但**不得削弱断言强度**（例如不许把
   差分断言改成恒真式）；每处语义性（非机械性）改动逐条记录 `NOTES_FOR_LEAD.md`。拿不准的
   交回主控，不要自行放宽。
4. 架构门禁：`go test ./internal/archtest/...` 必须全绿。`audit.Record`/`Manifest` 结构变更
   与 `report` 侧消费必须同一提交内完成（编译期耦合是仓库明文不变量）。
5. Git 规范：提交信息简明祈使句，**零 trailer**。可分多个 commit（建议按 ①审计字段→②分侧
   下沉→③消费侧切换→④replay→⑤parity 分步）。
6. 忽略目录：`_tmp/`、`archived/`、`logs/`、`reports/`、`_review/` 视为不存在。
7. 独立判断：以下规格是目标与约束，不是逐行处方；实现细节你有设计权，但每条**不变量**
   （标 ⚑）不可违背。

## 二、具体任务清单

### 任务 ① audit.Attempt 增 `Forwarded bool`
- `internal/audit/audit.go` 的 `Attempt` struct 增字段（json tag 建议 `forwarded`，加解释性
  doc comment：唯一置位点、softblock 不置位、截断流仍置位）。
- ⚑ **唯一置位点是 `router.forwardSuccess`**（`router.go:488` 起）。softblock 路径
  （`softblock.go`）、>=400 错误路径、build/network 失败路径一律不得置位。
- 截断流（SetTruncated 在成功转发之后）必须是 `Forwarded=true`。
- 兼容性（⚑）：历史 JSONL 无此字段 → 零值 false 与事实不符。分析侧消费必须走统一谓词
  （见任务②），规则：`Forwarded==true` 直接采信；`Forwarded==false` 且
  `Response!=nil && Status<400 && ErrorClass==""` 视为旧格式记录按旧行为算 forwarded
  （新格式的 softblock 有 `ErrorClass=="content"` 不会误入此分支）。谓词收拢在一个具名
  函数里 + doc comment 说明为何存在，不得在多个调用点各写一遍内联式。

### 任务 ② 分析侧改读字段，删反推谓词
- `report/ingest.go:146`（EndpointRow.Forwarded++）与 `report/recextract.go:174`
  （endpointInfo 的 servedEp/successEp 判定）改走任务①的谓词。
- ⚑ `rows.go:266-273` 的恒等式注释所钉的关系（重算列 = Forwarded × multiplier）必须
  重新成立，且由任务⑤的差分测试钉住。

### 任务 ③ chatmsg 分侧规则下沉：`ExtractUsageSides`
- 新增导出函数（建议签名 `ExtractUsageSides(body any, protocol string) (Usage, inOK, outOK bool)`，
  可按实现调整，但 ⚑ 必须同时覆盖 map 与 string/SSE 两种输入形态，与
  `ExtractUsageWithProtocol` 对齐）。
- 规则来源：`respnorm/usagesniff.go:100-105` 的 `usageBlockSides`——anthropic
  message_start 的 In 真 / Out 占位≈1 → `(true, false)`；其余形状按解析值。把
  `messageStartMarker` 判定一并下沉或由 respnorm 传入，实现放哪由你定，但 ⚑ 规则的
  **唯一权威**必须在 `chatmsg`，`respnorm` 只能调用不能另抄一份。
- `respnorm/usagesniff.go` 改为委托调用；`UsageSides()` 的对外语义（增量维护、截断流
  两侧各自 ok）不得回归——`respnorm` 包内既有测试必须全绿。

### 任务 ④ UsageOK → UsageInOK / UsageOutOK
- `ctxgraph/manifest.go:56` 的 `UsageOK` 拆为 `UsageInOK`/`UsageOutOK`（json tag 相应
  `usage_in_ok`/`usage_out_ok`；旧 `usage_ok` 字段不再写出）。manifest 消费点
  （`:149` 的降级估算判定）改为按侧生效：哪侧 ok 用哪侧真值，缺侧走 EstIn/EstOut。
  ⚑ 解析缓存（`.parse-cache`/manifest 派生物）出现旧字段时按缺失处理自然重建即可，
  不写迁移代码，但在 NOTES_FOR_LEAD 记一句影响面。
- `report/session.go:94` 的 `UsageOK` 同拆（`:350` 填充点、`recextract.go:135/:233`
  消费点按侧适配——哪些统计需要 In 侧、哪些需要 Out 侧，逐一过一遍再改，不要机械替换）。
- `story/cost.go:119` 的 `!s.Manifest.UsageOK` 门按侧拆分（cost 同时吃 In/Out 两侧：
  侧 ok 用真值，侧缺走估算；`estimated` 旗标按混算比例如实传递，不许把估算侧渲染成精确）。

### 任务 ⑤ replay 对齐（P-2-2）
- `replay/chargeReplay`（`replay.go:293-305`）改调 `router.TokenCountersSides`（签名
  见 `router/quota.go:186` doc comment），sides 来源用任务③的 `ExtractUsageSides`。
  ⚑ 不得再传 `u.In > 0 || u.Out > 0`。
- `recordView`（`replay.go:70-81`）增解码审计记录根级 `facts`，`inEst` 优先取
  `facts.EstimatedTokens`（live 侧 `server/facts.go:81-85` 的同源字段——剔除 base64
  的估算基），缺失才回退 `tokenutil.Estimate(reqBody)`。_respBody 侧维持 Estimate
  （路由半区 live 路径对响应同样用估算基——见 router 调用点的既有行为，不要单方面改）。
- `cmd/vmr/quota_parity_test.go`：
  - fixture 扩为 `protocol × {正常, 截断, softblock, 4xx}`（3 协议全组合）；
  - ⚑ 路由侧断言必须走路由自身导出入口（TokenCounters/ChargeResponse），不得重述公式；
  - 新增 replay↔router 角：对同一 fixture，replay 计费与 live 计费的 raw/estimated
    一致（softblock 行两者都为零扣）。

### 任务 ⑥（顺手，2 行）`ctxgraph` contextrot 前置不属你——跳过。
（P-D6-4 的 unknown 桶归批次二 Worker 5，其消费你产出的 UsageInOK。）

## 三、测试与验收步骤
1. `go test -race -count=1 ./internal/audit/ ./internal/router/ ./internal/chatmsg/ ./internal/respnorm/ ./internal/ctxgraph/ ./internal/report/ ./internal/replay/ ./internal/story/ ./cmd/vmr/`
2. `go test -count=1 ./internal/archtest/...`
3. `go vet ./...`；`gofmt -l .` 无输出（archtest 白名单文件除外）
4. `git status -s` 确认无白名单外文件
5. 分步 commit（见红线 5）

## 四、NOTES_FOR_LEAD 必录项
- 所有语义性测试改动的逐条清单；
- manifest/缓存格式变更的影响面；
- 设计文档需要同步的点（Analytics/Quota/Core 三份中涉及 UsageOK、Forwarded、chargeReplay 的段落）；
- 你在实现中发现的、本规格没覆盖的事实偏差。
