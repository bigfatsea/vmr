<!-- Ver 2026-09-05, by lead (orchestrated multi-agent execution) -->

# 配置形态简化 2026-09 — 执行报告

> 上游分析文档:`ConfigShape_Simplification_2026-09_v2.md`(V2,按域重写版)。
> 本报告记录该文档 A.1+A.2+A.3、C.1、D.1+D.2、E.1+E.2 四个包(D.3 锚点示例按指令排除)
> 的多 Agent 协同落地过程:规划、派发、执行台账、主控收尾与最终验收。
> 协作模式遵循 `docs/prompts/prompt-multi-agent-guide.md`。

---

## 一、执行结果总览

| 包 | 内容 | 提交 | 验收 |
|---|---|---|---|
| Pkg-A | A.1+A.2+A.3:`timeouts.probe` 归入 + `ttl:` 块 + 扩展 Duration 文法(d/w/mo/y)+ 拒绝永久关键字 + TTL 零值消歧(Breaking)+ A.3 UserGuide 补句 | `f147f3e` | 独立复跑全绿 |
| Pkg-C | C.1:`Provider.Disabled` 临时下线开关(单点过滤 + check 告警 + quota 跳过) | `8c4c1d7` | 独立复跑全绿 |
| Pkg-D | D.1+D.2:endpoints/fallback 按协议二层 map 化 + 不可达 fallback Warning + 差分等价测试 | `84c98e0` | 独立复跑全绿 |
| Pkg-E | E.1+E.2:顶层 `model_defaults`(按真实模型名寻址,`providers` 子属性,`"*"` 通配,字段独立回退)+ 移除 endpoint 级覆盖(4 字段净减) | `0f67e25` | 独立复跑全绿 |
| 主控收尾 | router/replay 读取点迁移、13 个测试 fixture 适配、CHANGELOG、KNOWN_ISSUES §2.81、mock 示例修整 | `fbb0987`、`2bca065` | 全量 `-race` + archtest + gofmt + vet 全绿 |

**最终状态**:`go build` / `go vet ./...` / `go test ./...` / `go test -race ./internal/...` /
`go test ./internal/archtest/...` / `gofmt` 全绿;`vmr check` 通过
`config.example.yaml`、`config.minimal.yaml`、`config.mock.yaml`(新形态)。
分支 `main` 领先 origin 18 个提交,未推送。

---

## 二、规划(开工前的编排决策)

### 2.1 文件交集分析与波次设计

对四个任务做非测试 Go 文件交集矩阵分析(规划阶段逐文件核实),结论:**任何两个任务
都存在至少一个共同文件**(`config.go`/`config_validate.go`/`check.go`/`snapshot.go`/
example yaml/UserGuide 两两交错),因此"四包全并行"不可行。唯一低冲突对是
**A ∥ C**(交集仅 `check.go` 的不同函数、example yaml/UserGuide 的不同段落),
据此定为三波:

1. **Wave 1(并行)**:Pkg-A ∥ Pkg-C,各自独立 worktree + 独立 TASK_SPEC;
2. **Wave 2(串行)**:Pkg-D(endpoints map 化,schema 锚点改动,基于 A+C);
3. **Wave 3(串行)**:Pkg-E(model_defaults,依赖 D 的 map 形态)。

顺序依据:D 是最高 ROI 的 schema 锚点改动、E 的示例/文档要引用 map 形态,
故 D 先于 E;A/C 与二者正交,前置并行摊薄总时长。

### 2.2 协作纪律(按 guide 6.2 模板落进每份 TASK_SPEC)

- 每包一份 `docs/tasks/TASK_SPEC_N.md`(已提交留档):红线/白名单/任务清单/验收步骤四段;
- CHANGELOG.md、KNOWN_ISSUES.md、设计文档为主控独占;Worker 待登记项写入
  **不提交**的 `NOTES_FOR_LEAD.md`,主控在 `worktree remove --force` **之前**读取收集;
- Worker 只精准 `git add` 白名单文件;commit 短祈使句、无 trailer;
- 派发用 `pi -p --approve @TASK_SPEC_N.md "<指令>"`,`@file` 独立 argv 项,
  后台运行同步登记 PID(实际 pi 进程为 wrapper 子进程:Wave 1 为 5385/5406,
  Wave 2 为 9292,Wave 3 为 12662),监控以 git 现场为准、处置绑定具体 PID;
- 主控独立复跑验收(不采信 Worker 自述),合并后全量测试。

---

## 三、执行台账

| 阶段 | 事项 | 结果 |
|---|---|---|
| Preflight | 分支/工作区检查、pi 连通性 ping、下游消费点核对 | 通过 |
| Wave 1 | Pkg-A(PID 5385)∥ Pkg-C(PID 5406)并行,约 22 分钟双双提交 | 完成 |
| Wave 1 验收 | 主控复跑 C(config/router `-race` + archtest + gofmt) | 全绿,零越界 |
| Wave 1 收尾 | A 触发 spec 停止条件两条(见 §4.1);合并 A→C 零冲突;主控改 4 个生产读取点 + 13 个 fixture,删过渡镜像字段 | 完成 |
| Wave 2 | Pkg-D(PID 9292),约 42 分钟提交;改动 76 文件(机械波及面远超预估) | 完成 |
| Wave 2 验收 | 主控复跑全量测试 + 抽查 schema diff + 审计白名单外改动 | 全绿;4 个非测试源码适配均编译必需,予以接受 |
| Wave 3 | Pkg-E(PID 12662),约 22 分钟提交 | 完成 |
| Wave 3 验收 | 主控复跑全量测试 + 审计 4 处白名单外测试适配 | 全绿 |
| 主控收尾 | CHANGELOG `[Unreleased]`(3 条 Breaking + 4 条 Added)、KNOWN_ISSUES §2.81、config.mock.yaml 新形态重写 + priority 注释纠错、终验 | 完成 |

---

## 四、偏离与决策记录(执行中实际发生的)

### 4.1 Pkg-A 触发 spec 前提不成立(spec 停止条件按设计工作)

1. **生产代码读取点**:`internal/router/probe.go`、`internal/router/snapshot.go`、
   `internal/replay/replay.go` 共 4 处读取被移除的旧 Config 字段,而 router/replay
   不在 A 的白名单内。Worker 未越界,改为在 `config.Config` 上加 3 个 `yaml:"-"`
   过渡镜像字段保编译,并在 NOTES 列明全部读取点。主控合并后迁移读取点至
   `Timeouts.Probe` / `TTL.Sticky` / `TTL.ImageCache.Days()` 并删除镜像字段(`fbb0987`)。
2. **13 个白名单外测试 fixture** 引用旧 YAML 键(`probe_timeout:` / `sticky_ttl: 10m`),
   解析即失败。主控逐一改为 `timeouts: {probe: X}` / `ttl: {sticky: 10m}` 形态,
   未削弱任何断言。

### 4.2 Pkg-D/E 的白名单外机械适配(均编译/严格解析所必需,NOTES 逐文件登记)

- D:`internal/diagnose/diagnose.go`、`internal/replay/replay.go`、
  `cmd/vmr/cmd_smoke.go`、`cmd/vmr/cmd_check.go` 读取旧 `eg.Protocol` 字段的 4 处
  适配(遍历方式随 map 形态,输出确定性用排序 key 保证);约 45 个测试 fixture 文件
  的形态迁移(远超预估的 4 个,系 map 化波及全仓 config fixture)。
- E:`cmd/vmr/main_test.go`、`internal/router/snapshot_equiv_test.go`、
  `internal/server/fixtures_test.go`、`internal/server/admin_status_test.go`
  4 处因 endpoint 级字段删除的机械适配(改为 model_defaults 形态)。

### 4.3 落地时敲定的悬决项(已登记 KNOWN_ISSUES §2.81)

- **TTL 换算**:`CalendarDuration` 固定长度近似(d=24h/w=7d/mo=30d/y=365d,与
  quota `every` 同约定),额外接受裸整数=天(旧 `*_days` 数值迁移路径);
  TTL→天数**向上取整**(亚天值至少 1 天),避免 `12h` 截断为 0 恰好落进
  audit/imgprep "0 = 不删" 的旧语义。
- **`model_defaults` 无合并规则**:V2 文档的"待敲定项"(同模型多条声明取
  max/后写/拒绝)实为伪问题——YAML map 重复 key 自身报错,exact key 与 `"*"`
  通配同时匹配是**回退链**(exact 优先),不是合并。
- **`BuildQuotaSpecs` 双形态**:BuildSnapshot 路径走 `BuildQuotaSpecsDisabled`
  (跳过 disabled,不留无主计数器);原 `BuildQuotaSpecs` 供 `replay.chargeReplay`
  使用(定向单 (provider, model),与在线路由状态无关,是正确语义)。
- **全 disabled endpoint-group 保留空 route**:走常规 no-candidates 失败路径,
  非 unknown-model(测试钉住)。
- **disabled 引用告警逐引用点发**:N 处引用 N 条 warning,每条点名具体位置。

### 4.4 D.3(锚点示例)按指令排除

`token_weights`/`model_multipliers` 的 YAML 锚点示例属文档项,本轮未动
(CHANGELOG 中既有条目已覆盖该指引)。

---

## 五、Breaking 变更与迁移指引(摘要)

1. **`audit_retention` 零值语义**:"0 = 永不删除"取消,`0`/未写 = 用默认 90d。
   依赖旧语义的部署请改写 `90000d` 或更大值,否则 reload 后开始按 90d 清理。
2. **endpoints/fallback map 化**:条目内 `protocol:` 字段删除,改为
   `endpoints: {<protocol>: [...]}` / `fallback_endpoints: {<protocol>: [...]}`;
   严格 YAML 对旧形态直接报 unknown field,无兼容层。
3. **endpoint 级 `capabilities`/`max_context_tokens` 移除**:事实归一至顶层
   `model_defaults`(按真实模型名寻址);虚拟模型层保留为显式覆盖/降级。
4. **顶层时间字段更名**:`probe_timeout`→`timeouts.probe`;`sticky_ttl`→`ttl.sticky`;
   `image_cache_ttl_days`→`ttl.image_cache`;`audit_retention_days`→`ttl.audit_retention`。

完整条目见 `CHANGELOG.md` `[Unreleased]`(发布前随 tag 改标题)。

---

## 六、多 Agent 协同复盘(对 guide 的印证与修正)

- **印证**:白名单 + NOTES_FOR_LEAD 机制是零冲突的关键——Wave 1 两个 worker 同仓库
  并行 22 分钟,合并零冲突;两次 spec 前提不成立都按停止条件正确止步、留痕交回,
  没有发生越界改代码。
- **偏差一(白名单预估不足)**:D 的 fixture 波及面预估 4 个文件、实际约 45 个;
  E 也触发 4 处。教训:schema 级改动的白名单应按"引用该 schema 的全部测试 fixture"
  圈定,或显式授权"测试 fixture 形态迁移随代码同包、生产代码越界仍须止步"——
  本轮 D/E 的 NOTES 透明登记弥补了这点,主控逐文件审计后放行。
- **偏差二(生产读取点漏估)**:A 的 spec 断言"internal/router 不消费这些字段"
  未经验证即写入,实际有 4 处。教训:spec 的"不会打红外测试"前提必须先 grep
  核实再落笔;过渡镜像字段是一个好的兜底模式(保 worker 独立可验收,主控收尾删除)。
- **监控实操**:`pi -p` 非交互模式日志到结束才落盘,进度监控以 worktree 的
  `git status`/`git log` 为准;`$!` 捕获的是 wrapper bash,真实 pi 进程需 `ps --ppid`
  二次定位后登记。

---

## 七、遗留与后续

- `main` 未推送(领先 origin 18 提交);推送/发版时按 CHANGELOG 约定改 `[Unreleased]` 标题。
- `config.mock.yaml` / `pricing.mock.yaml` 为未跟踪本地示例(与改动前一致保持未跟踪),
  mock 已按新形态重写并通过 `vmr check`(占位 key 未设的告警属预期)。
- V2 文档 G.1 #7 的 mock 零碎修整(priority 注释反向纠错、max_concurrency 零值标注)
  已随本轮主控收尾完成;strategy/USD 注释项已核对无需改动。
