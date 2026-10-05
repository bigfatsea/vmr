<!-- // Ver 2026-10-05 16:20, by Claude Opus 4.6 -->

# vmr 配置文件在线编辑 — 可行性评估与实现方案

## 0. 一句话结论

**可行，且成本比直觉低一个数量级**：vmr 已经具备「文件是唯一事实源 + fsnotify/SIGHUP 热重载 + 坏配置永不替换好配置 + `config.Parse` 可对任意字节串做完整校验 + auth-gated console 页面模式」这套完整地基。在线编辑的正确形态不是「把配置搬进进程内存」，而是**加一个把编辑结果写回 config 文件的受控写入口**，让既有热重载管线成为唯一生效路径。推荐「方案 A：文件回写式在线编辑（API + 轻量 console 页面，opt-in）」。

---

## 1. 现状盘点（方案的地基）

在提任何新方案之前，先确认 vmr 现在已经有什么——这决定了哪些「实现方式」其实是免费的。

### 1.1 热重载管线已经是唯一生效路径

`cmd_start.go` 的 reload 闭包（trigger 标注 `fsnotify` / `SIGHUP`）执行：

```
config.Load(path) → newCfg.Check() → router.BuildSnapshot(newCfg) → rt.Install(newSnap) → rt.RecordReload(trigger, err)
```

任何一步失败都「rejected, keeping current config」。这条管线有三个关键性质：

1. **文件是唯一事实源**。UserGuide 明文写着：刻意没有独立的运行时 disable 命令或观测通道——改配置就是改文件，热重载在 debounce 窗口内生效。
2. **坏配置永不替换好配置**，且拒绝是设计内行为，不是异常。
3. **校验入口可复用**：`config.Parse(raw []byte)` 是导出函数，`checkUnknownFields` 保留了用户原始文件的行号——对一份「候选配置文本」做完整校验（含 `${ENV}` 展开、严格 YAML unknown key、两层费率四分量校验、sticky TTL 上限等全部加载期校验）不需要碰磁盘。

### 1.2 「改了但没生效」已经有外部可见的诊断

`internal/router/reload.go` 的 `ReloadState`（`/status` 输出）与 `ConfigStale`（对比 config mtime 与最近一次成功 load 时间）已经回答了「磁盘上的文件是不是正在运行的配置」这个问题。在线编辑功能可以直接复用这套状态做 UI 指示，不需要发明新的诊断。

### 1.3 管理面已有成熟的页面与认证模式

`/status`、`/stats`、`/log` 及其 `status.html` / `models.html` / `log.html` console 页面已确立：

- **认证**：`s.auth` 中间件，复用聊天入口的同一把 `api_keys`（KNOWN_ISSUES 明文记录这是对单人/小团队代理正确的简化，`api_keys` 就是管理凭证）；
- **页面资产**：`console.css` / `console.js` 共享注入（`assembleConsolePage`，启动期一次性组装）；
- **opt-in 先例**：`analytics.serve` 默认关闭、开启后数据请求强制 Bearer key、无 key 时 403 而非裸奔——「涉及敏感数据的网络面默认关」在这个项目里有现成范式。

### 1.4 对此需求的两个边界约束

- **架构边界**：`server` 已经 import `config`（`/status` 在用），在线编辑不需要任何新的跨半耦合；analytics 半完全不参与。
- **archtest**：新增逻辑应落在 `internal/server` 的新文件（如 `admin_config.go`），注意函数级行预算。

---

## 2. 需求界定：「在线编辑」的两种解读

这个口头需求有两个差别巨大的解读，方案分叉全在这里：

| 解读 | 含义 | 与 vmr 现有架构的关系 |
| --- | --- | --- |
| **① 文件回写式** | 浏览器/HTTP 是 config.yaml 的**另一支笔**：编辑 → 校验 → 写回文件 → 既有热重载生效 | 完全顺着现有架构，不新增任何状态 |
| **② 内存态配置中心** | 进程内持有一份可变配置状态，HTTP 直接改它，文件退化为导出/快照 | **推翻**「文件是唯一事实源」这条明文不变式 |

解读 ② 与架构直接冲突（详见方案 C 的否决理由），解读 ① 是唯一推荐方向。下面把四种可选实现全部摊开。

---

## 3. 候选实现方式对比

### 方案 0：不做——外部编辑器 + 既有热重载

现状已支持：任意编辑器改 `config.yaml`，秒级生效，坏配置自动拒绝，`/status` 能看到 staleness。对「SSH + vim 就够」的场景这确实是终态。

- **优点**：零成本，零风险。
- **缺点**：不解决真实痛点——远程/移动场景（手机上想关掉一个出问题的 provider）、不想记 YAML 结构、想要一个「改前先验证、改完确认生效」的闭环。SIGHUP 是给机器用的，浏览器不是编辑器。
- **结论**：作为基线保留，但不是本需求的答案。它的存在恰恰说明方案 A 应该做得薄。

### 方案 A：文件回写式在线编辑（推荐）

**机制**：新增 auth-gated 的 `GET /config` + `PUT /config`。GET 返回**磁盘上**的原始 YAML 字节（不是运行中配置）加元数据（mtime、ReloadState、running-vs-disk 是否一致）；PUT 接收完整 YAML 文本，走「校验 → 原子写回 → 同步触发 reload → 返回结果」四步，然后由既有 fsnotify/SIGHUP 同一条管线生效。前端是一个复用 console 模式的 `config.html`（textarea + Validate/Save 按钮），也可纯 curl。

关键设计点：

1. **校验先于落盘，且用与热重载完全相同的代码**：`config.Parse(body)` + `cfg.Check()` + `router.BuildSnapshot(cfg)`（dry-run，产物即弃）。三步全过才写文件。因为校验代码与 reload 管线逐字相同，写盘后被 reload 拒绝的概率趋近于零（唯一残余：`${ENV}` 在校验与重读之间的环境变化——同一个进程，实际上不存在）。
2. **原子写回**：同目录临时文件（0600）+ `rename`。这同时满足审计文件权限不变式，且 fsnotify watch 的是父目录、监听 Write/Create/Rename，rename 落位的事件本来就在既有处理范围内。
3. **reload 触发走注入的同步闭包而非被动等 fsnotify**：`cmd_start` 已有加锁的 `reload(trigger)` 闭包（`reloadMu` 串行化 fsnotify/SIGHUP），把它经 `WithConfigReload(...)` 注入 server，PUT 处理器写盘后直接调 `reload("api")` 并把结果（成功/拒绝原因）返回给调用方。这消除了两个被动等待的失败态：debounce 窗口的不确定性，以及 watch goroutine 已死（KNOWN_ISSUES 记录过 panic 后热重载永久下线）时「写成功了却永不生效」。
4. **副作用**：写盘本身也会触发 fsnotify 事件 → 300ms 后 reload 闭包再跑一次，重读同一文件。幂等，代价是一行重复的 CONFIG RELOAD 日志；若在意，可在闭包内用 mtime 与上次成功 load 比较、相同则降为一条 log，不必做更多。
5. **防丢更新（可选，建议做）**：PUT 带 `If-Match: <mtime 或内容 hash>`（GET 返回 ETag），不匹配返回 409。防止「两个窗口/人与外部编辑器互相覆盖」。单人本地场景可后置，但成本极低。

- **优点**：
  - 零新增运行时状态、零新增生效路径——config 文件仍是唯一事实源，`ConfigStale`/`ReloadState`/`vmr check`/`vmr diagnose` 全部继续成立；
  - 注释与格式天然保留（编辑的是原始文本，不是反序列化后的对象）；
  - `${ENV}` 语义自动一致（校验与 reload 都在同一进程环境里展开）；
  - 实现量最小：一个 `admin_config.go` + 一个 `config.html` + cmd_start 一行注入。
- **缺点**：
  - 编辑体验是「整个 YAML 文本」，没有字段级表单；
  - 它是一个**有写能力的网络端点**，风险等级高于只读的 `/status`（见 §4 安全设计）。
- **变体 A2（可叠加）**：在 A 之上为高频操作提供窄口径字段级 API，如 `POST /config/providers/{name}/disabled {true|false}`——服务端读文件、改一个节点、走同一条校验+写回管线（用 `yaml.Node` 定点改写以保留注释）。UserGuide 里「vendor 限流时关掉一个 provider」是最典型的在线编辑动机，值得作为 A 落地后的第一个增量，而不是一开始就做。

### 方案 B：结构化表单编辑器（服务端按 schema 渲染表单、marshal 回 YAML）

**机制**：为 `Config` 的每个字段生成表单，服务端把表单值 marshal 成 YAML 写盘。

- **否决理由**：
  1. **注释全灭**。config.example.yaml 靠行内注释当文档用；`yaml.Marshal` 一个 Go struct 会把用户文件里所有注释和格式抹掉，等于每次保存都销毁文档。要保留注释得走 `yaml.Node` 全树合并，工作量数倍且仍难覆盖所有形状。
  2. **schema 面积大且持续演化**：providers/models/aliases/endpoint groups/quota/pricing 规则/guard/analytics……每个字段一个表单控件，config 包每加一个字段前端就要跟一次。这是把 `config.example.yaml` 用代码重新实现一遍，维护成本与收益完全不成比例。
  3. 与本项目「elimination over conditionals」的哲学相逆：方案 A 用「编辑文本 + 现成校验器」消掉了整个 schema 到 UI 的映射问题。
- **何时会翻案**：只有当出现大量非技术用户多租户场景时才值得——那也不是 vmr 的定位（Strategy 文档：单人/小团队本地路由）。

### 方案 C：内存态配置中心（进程内可变配置，文件退化为导出）

- **否决理由**（这条要写进 KNOWN_ISSUES 的 decided-not-to-fix，防止将来被重新提出）：
  1. 直接推翻 UserGuide 明文的「config file is the single source of truth」——外部编辑器、`vmr check`、`vmr diagnose`、reload 诊断全部会给出与运行态不符的答案，出现「两份事实」。
  2. 现有的 staleness 诊断（`ConfigStale` 的语义就是「磁盘文件 ≠ 正在运行的配置」）在内存态方案里失去意义或变成永久告警。
  3. `vmr.sh` / service 模式、故障排查习惯（「看文件就知道在跑什么」）全被破坏。
  4. 换来唯一的好处（字段级即时性），方案 A2 的窄口径 API 已能覆盖。
- **结论**：架构性否决，不是工作量问题。

### 对比总表

| 维度 | 方案 0 不做 | **方案 A 文件回写**（+A2） | 方案 B 表单编辑器 | 方案 C 内存态 |
| --- | --- | --- | --- | --- |
| 新增运行时状态 | 无 | **无** | 无 | 大（可变配置状态机） |
| 生效路径 | 已有 | **既有 reload 管线，唯一** | 既有 | 新增独立路径 |
| 注释/格式保留 | — | **天然保留** | 全灭 | 全灭 |
| schema 演化维护 | — | **零**（校验器即 schema） | 每字段跟一次 | 每字段跟一次 |
| 实现量 | — | **小**（一个 handler 文件 + 一个页面） | 大 | 大且伤架构 |
| 与现有不变式冲突 | 无 | **无** | 无 | 直接冲突 |
| 远程/移动可用 | 否（需 SSH） | **是** | 是 | 是 |
| 结论 | 基线 | **推荐** | 否决 | 架构性否决 |

---

## 4. 推荐方案 A 的具体设计

### 4.1 API 面

```
GET  /config          # auth-gated
  ← 200  text/yaml 原始文件字节 + 响应头:
        X-Config-Mtime: <RFC3339>
        ETag: <sha256(body) 前 16 位>
        X-Config-Running-Stale: true|false   # 复用 router.ConfigStale 语义
        X-Reload-State: ok|rejected|never    # 复用 ReloadState
PUT  /config          # auth-gated, body = 完整 YAML 文本, If-Match 可选
  服务端四步: Parse → Check → BuildSnapshot(dry-run) → 原子写盘(0600, tmp+rename)
  → 调注入的 reload 闭包 (trigger="api") → 200 返回 {reloaded: true, warnings: [...]}
  校验失败 → 400 返回带行号的错误原文；If-Match 不匹配 → 409
POST /config/validate # auth-gated, body = 候选 YAML，只校验不落盘（编辑器的「Validate」按钮）
```

GET 返回**磁盘文件**而不是运行中配置，这是刻意的：文件是事实源，返回运行中对象再让用户反推 YAML 会制造两份真相。

### 4.2 代码落点

- `internal/server/admin_config.go`：三个 handler + 原子写 helper。`server` 已 import `config` 与 `router`，无新边界；注意 archtest 行预算（校验与写盘各是一个函数，天然不大）。
- `internal/server`：`WithConfigReload(fn func()) *Server` 注入项（与 `WithInstance` 同型；nil 时 PUT 返回 501「本实例未启用配置写入口」——比如未来 `vmr replay` 复用 server 时）。
- `cmd/vmr/cmd_start.go`：一行接线，把既有 `reload` 闭包包一层传入。`reloadMu` 原样复用，天然串行化 api/fsnotify/SIGHUP 三种 trigger。
- `internal/server/assets/config.html`：console 模式页面——textarea、Validate/Save 按钮、顶部 staleness/reload-state 徽标、保存后展示 Check() 的 WARN/CONFIG PROBLEMS 摘要。先用纯 textarea（console.js 内加百来行），编辑器高亮（CodeMirror 单文件版）留作后续升级，不影响 API 形状。
- 路由注册（`server.go` `Handler()`）加三行，全部过 `s.auth`。

### 4.3 安全设计（写端点 ≠ 读端点）

这是整个方案里唯一真正的新风险面，必须显式决策：

1. **opt-in，默认关闭**。config 加一个字段（挂在现有顶层，如 `admin: { config_edit: true }`，沿用 `analytics.serve` 的 opt-in 先例）。理由：写端点的暴露面是「完全控制」——能改 `api_keys` 自己、能加上游 key、能改 `listen`——这与 `/status` 的读暴露是不同量级，默认开不符合本项目对网络面的保守惯例。
2. **鉴权与 `/status` 完全同策略**（经负责人确认的修订：放弃原「无 `api_keys` 一律 403」提议）：配了 `api_keys` 就要凭证；未配则开放，与局域网自用环境、以及 `/status`「网络可达性与身份认证解耦」的既有取舍保持一致。实现上直接复用 `s.auth` 中间件，不新增任何分支。
3. **`config.Check()` 的既有警告沿用**：非 loopback listen 本来就会 WARN；这些 warning 经 PUT/validate 响应与 console 页面原样透传，不发明新的分类。
4. **自锁防护随策略修订一并取消**：允许改 `api_keys`，允许置空（与「未配 key 开放」的既定策略一致）；空引用 `${ENV}` 等运营风险由 `Check()` 的 warning 透传承载，不加特判。
5. 需要重启才生效的字段（`log_dir`、启动期才构建的 `guard:` 引擎——`logGuardReloadWarning` 已处理）在 PUT 响应和 console 页面上原样透传 Check()/reload 的提示，不发明新的分类。

### 4.4 测试

- 单元：三步校验各自失败返回 400 且带原始行号；原子写（写后权限 0600、rename 后无临时文件残留）；无 api_keys → 403；config_edit 未开 → 403/404；If-Match 失配 → 409；注入 reload 闭包 nil → 501。
- 集成（复用 `cmd_start_test.go` 的实例模式）：PUT 合法配置 → 断言 `ReloadState.Trigger == "api"`、`OK == true`、新路由表生效；PUT 非法配置 → 文件未被改动（mtime 不变）且 400。
- 并发：PUT 与 SIGHUP 并发打 `reloadMu` 串行正确性（`go test -race`）。

### 4.5 文档与登记义务（repo 惯例）

- `UserGuide.md` + `UserGuide.zh.md` 同步新增章节；`config.example.yaml` + `.zh.yaml` 加 `admin:` 字段注释（描述机制，不写易变数字）。
- `KNOWN_ISSUES.md` 登记：方案 C（内存态配置）为 deliberate non-fix 及理由；fsnotify 写盘后 300ms 重复 reload 的幂等说明（若选择容忍而非 mtime 去重）。
- `CHANGELOG.md` `[Unreleased]` Added 条目。
- 设计文档：这一改动属于 Part 1（Core）的 server 面，落地时按惯例更新 Core 文档相应章节与取舍表。

### 4.6 实施切分（建议顺序）

1. **P1（核心，一天内量级）**：`admin_config.go` 三 handler + WithConfigReload 注入 + cmd_start 接线 + 单元/集成测试 + 安全门（opt-in、403）。此刻 curl 已完整可用。
2. **P2**：`config.html` console 页面（textarea 版）+ ETag/If-Match。
3. **P3（可选增量）**：A2 窄口径 disabled/priority 快捷 API；CodeMirror 编辑器升级；mtime 去重消除重复 reload 日志。

---

## 5. 风险与未验证假设

- **未验证**：`rename` 落位在所有目标平台（linux/darwin CI 矩阵）都会触发 fsnotify 的 Create/Rename 事件并被现有过滤条件放行——从 `watch.go` 源码读是成立的（监听父目录、放行 Create/Rename），但实施时应补一条覆盖「外部编辑器 rename 替换文件」的既有回归测试确认，避免在线编辑的写盘方式与 watch 行为在某些编辑器形态下脱节。
- **假设**：本评估按「单人/小团队本地部署」的既有定位取安全默认（opt-in + api_keys 强制）。若未来出现多用户暴露形态，§4.3 需要整体重估。
- **残余风险**：PUT 写盘与外部编辑器并发写仍是 last-writer-wins（If-Match 只能防「基于过期版本」的写，防不了真正的同刻竞写）；单人场景可接受，登记 KNOWN_ISSUES 即可。
- **明确不做**：方案 C 的内存态配置；方案 B 的全量表单。若日后有人重提，以本文 §3 与 KNOWN_ISSUES 登记条目为答复。
