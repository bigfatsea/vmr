<!-- Ver 2026-08-03, by Sonnet 5 -->

# vmr 全面评审报告

**评审范围**：全部代码、设计、架构、文档、测试、注释
**评审方式**：第一性原理独立判断，不预设已有文档/规划为正确
**基线 commit**：`c2c6df7`（main，工作区干净）
**评审日期**：2026-08-03

> **2026-08-04 更新**：5.1 节全部 3 项 P0 已核实并修复。5.2 节 7 项 P1 逐项复核：5 项（P1-2/P1-3/P1-4/P1-7）已修复，2 项（P1-5/P1-6）评估后判断本轮成本过高、暂不做，理由见各条目。其中 **P1-1 的 fuzz 补全过程中意外发现并修复了一个真实的无限循环 DoS bug**（`internal/adapter/classify.go` 的共享扫描器 `skipJSONValue`），是本次复核里最重要的发现——详见问题 8 的完整根因分析。5.3 节 16 项 P2 逐一核对：14 项属实并已修复，**2 项（#7、#15）复核后判定是原报告的误判**（分别是已有文档记录的行为、和报表工具真实输出的自有编号，都不是缺陷）——详见各条目"结果"列。标注见各条目及 5.1/5.2/5.3 节。

---

## 0. 评审方法与基线事实

### 0.1 方法

先跑通所有可验证的事实（构建、测试、覆盖率、竞态、依赖），再逐文件精读，最后做横切与项目级回顾。凡是能用命令验证的结论，都验证过；凡是判断，都标明是判断。

刻意不以 `docs/VirtualModelRouter_Design_v4_*.md` 为准绳去核对代码，而是反过来：读代码得出结论，再回头看文档是否与之一致。这一步产出了本报告价值最高的一批发现。

### 0.2 实测基线

```
go build ./...        通过，零输出
go vet ./...          通过，零输出
go test ./...         28 个包全部 ok
go test -race ./...   全部通过
```

| 指标 | 数值 |
| --- | --- |
| 生产代码 | 25,574 行 Go |
| 测试代码 | 23,959 行 Go（测试/生产比 0.94） |
| 文档 | 8,833 行 Markdown |
| Shell | 685 行（`vmr.sh` 609 + `vmr-loadtest.sh` 76） |
| 源文件总数 | 约 180（git 跟踪） |
| 直接依赖 | 4 个（fsnotify、klauspost/compress、x/image、yaml.v3） |
| TODO/FIXME/XXX/HACK | **0 处** |

覆盖率分布（`go test -cover ./...`）：

```
100.0%  fmtutil
 98.5%  health          95.0%  sticky          93.8%  diagnose
 92.1%  story           90.3%  ctxgraph        89.6%  server
 88.6%  chatmsg         88.2%  config          87.5%  story/profile
 87.3%  report          86.1%  adapter         85.7%  anthropic / rundir
 85.2%  strategy / buildinfo                   84.2%  probe
 83.7%  imgprep         82.6%  replay          82.1%  router
 73.7%  cmd/vmr / openairesponses              68.4%  openai
 66.1%  audit           64.9%  core            17.3%  i18n
  0.0%  loadtest/{gentargets,mockupstream,runner}（无测试文件）
```

### 0.3 一句话总体判断

**这是一个质量显著高于同规模开源项目平均水平的代码库。** 架构边界清晰且有可执行测试守护，注释密度高且几乎全部在解释"为什么"而非"是什么"，零技术债标记，测试与生产代码几乎等量，竞态检测干净。

本报告的绝大部分篇幅是在这个高基线上找可改进项。**不要把发现的数量误读成质量差**——恰恰相反，能找出这么多具体问题，是因为代码足够规整到可以被逐条检验。

真正需要处理的问题集中在三类：**注释引用体系已系统性失效**、**配置健康检查在关键路径缺席**、**资源上界在默认配置下未闭合**。其余多为打磨项。

---

## 1. 文件结构全览

```
vmr/
├── CLAUDE.md                          项目简报（供 AI 助手）
├── README.md / README.zh.md           双语入口
├── LICENSE
├── go.mod / go.sum                    Go 1.25.1，4 直接依赖
├── config.example.yaml                235 行，配置项全覆盖
├── pricing.yaml                       130 行，report 成本估算侧车
├── vmr.sh                             609 行，dev 模式 + service 模式管理
├── vmr-loadtest.sh                    76 行
│
├── .github/workflows/
│   ├── ci.yml                         vet + build + test -race（ubuntu）
│   └── release.yml                    4 平台交叉编译 + tarball + checksums
│
├── cmd/vmr/                           CLI，一个子命令一个文件
│   ├── main.go(71)                    dispatch + usage + adapter 注册点
│   ├── cmd_start.go(202)              启动、热重载、优雅关闭
│   ├── cmd_check.go(301)              配置校验 + 路由表打印
│   ├── cmd_status.go(212)             查询运行中实例
│   ├── cmd_diagnose.go(53)            连通性诊断
│   ├── cmd_replay.go(63)              单请求重放
│   ├── cmd_report.go(180)             聚合报表
│   ├── cmd_story.go(461)              任务叙事重建
│   ├── cmd_version.go(21)
│   ├── summary.go(216)                start/check 共享的配置摘要渲染
│   ├── reportconfig.go(86)            report.yaml 解析
│   ├── auditpaths.go(54)              <file|glob>... 位置参数约定
│   └── *_test.go                      2,187 行测试
│
├── internal/
│   ├── core/          (307+62)        共享类型、header 黑名单、token 估算
│   ├── fmtutil/       (57)            展示格式化
│   ├── rundir/        (60)            默认目录解析公式
│   ├── buildinfo/     (90)            VCS 构建标识
│   ├── config/        (546+101+53)    YAML 加载、校验、一致性检查、热监听
│   ├── adapter/       (117+487+357)   Adapter 接口、错误分类、指纹/扫描器
│   │   ├── openai/          (65)
│   │   ├── anthropic/       (70)
│   │   └── openairesponses/ (89)
│   ├── health/        (176)           冷却 + 退避 + 半开单飞
│   ├── probe/         (128)           echo-nonce 探针请求构造
│   ├── strategy/      (174+41)        Dimension（排序）+ Condition（淘汰）
│   ├── sticky/        (104)           会话亲和注册表
│   ├── router/        (552+616+232+195+116+111+96+65+64)  故障转移核心
│   │   ├── router.go      失败转移主循环
│   │   ├── response.go    响应归一化状态机
│   │   ├── responsefix.go MiniMax 专属修复
│   │   ├── snapshot.go    路由表构建与原子安装
│   │   ├── transport.go   上游 client + 流转发
│   │   ├── limiter.go     全局并发闸
│   │   ├── logfmt.go      实时日志格式
│   │   ├── reload.go      热重载结果追踪
│   │   └── probe.go       后台恢复探测
│   ├── server/        (291+158+163+84) HTTP 入口、审计录制、admin
│   ├── audit/         (554+154+96)    JSONL 审计 + zstd 压缩/保留
│   ├── imgprep/       (565+145)       图片降采样 + 磁盘缓存
│   ├── chatmsg/       (350+266+168+…) 消息/SSE/usage 共享解析层
│   ├── ctxgraph/      (355+181+…)     内容寻址 manifest / lineage / stitch
│   ├── report/        (32 文件)       vmr report 聚合与渲染
│   ├── story/         (18 文件)       vmr story 叙事重建
│   │   └── profile/                   agent 专属判定（OpenClaw + 通用）
│   ├── i18n/          (20 文件,1989)  中英双语文本
│   ├── diagnose/      (644)           真实连通性检查
│   ├── replay/        (467)           审计记录重放
│   └── archtest/      (2 文件)        架构不变量可执行检查
│
├── loadtest/                          12 场景压测（gentargets/mockupstream/runner）
├── examples/sample-audit.jsonl        5 行示例审计数据
└── docs/                              设计文档 + 用户指南 + 分发/战略材料
    ├── VirtualModelRouter_Design_v4_Core.md       894 行
    ├── VirtualModelRouter_Design_v4_Analytics.md  376 行
    ├── UserGuide.md / .zh.md                      308/307 行
    ├── Why_vmr_over_LiteLLM.md / .zh.md
    ├── OUTSTANDING_ISSUES_opus-5.md
    ├── Agent任务叙事报告_设计与价值论证_v1.1     1,095 行
    ├── future-strategy/     (4 文件)
    └── distribution-strategy/ (23 文件)
```

---

## 2. 逐模块评审

### M1 · 基础层（core / fmtutil / rundir / buildinfo）

**职责**：无内部依赖的叶子包。`core` 持有 `CanonicalRequest`/`ErrorClass`/`Endpoint` 与 header 黑名单；其余三个各管一件小事。

**做得好的**：

- `Endpoint.Freeze()` 的设计精准：`HealthKey()` 每次调用要做一次 SHA-256，热路径上有约 7 个调用点，于是在 `BuildSnapshot` 里预计算一次，同时保留未 Freeze 时按需计算的降级路径，让测试里的 `&core.Endpoint{...}` 字面量继续可用。这是"优化不牺牲可用性"的正面样本。
- `MarshalNoEscape` 的存在理由写得很清楚：默认 `json.Marshal` 会把 `< > &` 转义成 `\uXXXX`，语义等价但产生了无谓的字节偏离。这种对"字节忠实"的偏执贯穿全项目。
- `fmtutil` 从 `core` 拆出的理由站得住：分析层不该为了打印一个数字而依赖路由域类型。
- `rundir` 拒绝用 `os.TempDir()` 作首选并给出了具体理由（macOS 会清理 3 天未访问的临时文件，而审计数据是成本核算的唯一来源）。
- `buildinfo` 用 Go 自带的 VCS stamp 而非 ldflags，消除了一整类"构建系统必须记得传参"的问题。

**问题**：

1. **`buildinfo` 的注释与仓库事实矛盾**（`buildinfo.go:60-61`）：
   ```go
   // Deliberately not a semver — this project has no release process to produce one
   ```
   但仓库里有 `.github/workflows/release.yml`，打 `v*` tag 会触发 4 平台交叉编译并发布 GitHub Release。注释写作时可能属实，现在已不成立。
   连带后果：release tarball 里的二进制 `vmr version` 显示的是 commit SHA，而用户下载的是 `v1.2.3`。**tag 与二进制自报版本对不上**。

2. `core.go` 内 4 处 `§6.4`/`§1.1`/`§6.5`/`§1.4` 编号引用——见第 3.1 节的系统性问题。

3. `core` 覆盖率 64.9%，是所有非 i18n 包里最低的。作为被所有人依赖的叶子包，这个数字偏低。未覆盖的主要是 `ErrorClass.String()` 的部分分支与 `EstimateTextTokens` 的边界。

**模块级回顾**：基础层是全项目最稳的部分。抽象粒度合适，没有一个类型是"为了将来可能用到"而存在的。唯一的实质问题是 `buildinfo` 的注释已经过时——而这正是后面会反复出现的模式：**代码正确，注释描述的世界已经变了**。

---

### M2 · config

**职责**：YAML 加载、`${ENV}` 展开、严格校验（`KnownFields`）、一致性检查、文件监听热重载。

**做得好的**：

- `KnownFields(true)`：拼错的 key 是加载错误而不是静默忽略。这个决定的价值在于——用户以为生效的配置真的生效了。
- 校验错误消息的质量罕见地高。例如 `sticky_ttl` 超过内存回收兜底值时的报错，完整解释了"为什么这个配置看起来被接受了但会静默失效"，而不是干巴巴一句 "invalid value"。
- 把 `check.go` 从 `validate()` 拆出来的理由正确：`validate()` 必须在第一个错误处中止（`Load` 返回 error），而 `vmr check` 想把所有问题都列出来再总结。两种失败语义不该塞进一个函数。
- 显式拒绝代理环境变量（`http_proxy`/`https_proxy` 只从 config 读），理由是"一个隐式旋钮悄悄改变流量去向，正是路由器不该有的意外"。这个判断我完全同意。
- `Provider` 扁平化 + `base_url` 按协议 keyed 的建模是对的：一个账号可能同时说多种协议，而 api_key/proxy 是账号属性不是协议属性。

**问题**：

4. **`config.Check()` 在两条关键路径上缺席**（重要，见 5.1 节 P0-2）。`Check()` 只被 `vmr check` 和 `vmr diagnose` 调用，而：
   - `vmr start` 直接启动时**不调用**（`cmd_start.go:83-115` 只有 `config.Load`）
   - **热重载路径完全不调用**（`cmd_start.go:132-151`）

   后果具体化：用户改了 config.yaml，把 `api_key: ${DEEPSEEK_KEY}` 写成 `${DEEPSEEK_API_KEY}`，热重载会**接受**这份配置（YAML 合法、结构合法），`expandEnv` 把未设置的变量展开为空串，`validate()` 不检查 api_key 非空，`adapter.BuildRequest` 里 `if ep.APIKey != ""` 于是不注入认证头——请求裸奔发出去，全部 401。日志里看到的是 `CONFIG RELOAD` 后跟着一串 401，没有任何一行说"你的 api_key 是空的"。

   service 模式（launchd `KeepAlive` / systemd `Restart=always`）自动重启时同样不跑 check——只有 `svc_install` 那一次跑过。

   **[已修复 2026-08-04]**：`cmd_start.go` 新增 `logConfigCheckIssues`，在初次启动和每次 reload（fsnotify/SIGHUP，含 service 自动重启触发的那次）成功 `Load` 后调用 `cfg.Check()`，每条 Issue 打一行 `WARN config check: ...`；不阻断启动/重载。已用真实二进制端到端验证（启动、SIGHUP、fsnotify 三条路径）。

5. **`config.Watch` 吞掉所有 watcher 错误**（`watch.go:45-48`）：
   ```go
   case _, ok := <-watcher.Errors:
       if !ok { return }
   ```
   错误被读出后直接丢弃。如果 fsnotify 出错（inotify 句柄耗尽、目录被删除重建），热重载会静默停止工作，用户无从察觉。这是可观测性缺口，不是功能 bug，但修复成本极低（一个回调或一行日志）。

6. `Watch` 的 debounce timer 存在理论上的重入：`timer.Stop()` 返回 false 说明回调已触发，此时又 `AfterFunc` 一个新的，可能出现两次 `onChange` 并发。`Install` 是原子 swap 所以不会崩，但两次 reload 的日志会交错、`RecordReload` 的计数会翻倍。低风险。

7. `Watch` 返回的停止函数是 `watcher.Close`，但已排期的 timer 可能在 Close 之后仍触发 `onChange`。同样低风险。

**模块级回顾**：config 是全项目设计得最"有主见"的模块——严格 YAML、拒绝环境变量隐式行为、错误消息解释后果而非现象。这些都是正确的偏执。唯一的结构性缺陷是 `Check()` 与 `validate()` 的分层虽然理由充分，却导致了"操作性检查只在人类主动运行诊断命令时才跑"——而配置出错最危险的时刻恰恰是无人值守的热重载。这不是拆分本身的错，是拆分之后忘了给自动路径补上调用。

---

### M3 · adapter（含 openai / anthropic / openairesponses）

**职责**：Adapter 接口 + 编译期注册表；共享的错误分类；字节拼接式的 model/role 重写；会话指纹提取。

**做得好的**：

- **`RewriteModel` 的字节拼接方案**是整个项目最能体现"品味"的设计。常规做法是 unmarshal 成 map、改字段、re-marshal——代价是每次故障转移尝试都要完整解析和复制整个 body（可能几 MB），且 key 顺序、空白全变。这里改成单遍无分配扫描定位 `"model"` 值的字节范围，然后 `prefix + 新值 + suffix` 拼接。**其余每一个字节原样到达上游**。扫描器处理不了的形状（非 JSON 对象、无顶层 model key）降级到通用路径。
- `skipJSONString` 用 `bytes.IndexByte` + 反斜杠奇偶校验跳转，即使穿过多 MB 的 content 字符串也保持 memchr 速度。
- 注册表用 `atomic.Pointer` 无锁读 + mutex 保护的 copy-on-write 写。注释明确指出：**只有原子 swap 没有写锁，在并发写入下会静默丢失更新**——并且这一点有 `TestGetConcurrentWithRegister` 在 `-race` 下守护。这是把一个容易被忽略的正确性细节固化成了测试。
- `DefaultClassify` 的分类规则每一条都标注了取证来源（"MiniMax 对未知模型返回 400 而非 404"、"OpenRouter 用 403 表示审核拦截"）。`contentHint` 显式说明"宁可误判也不漏判"的理由：误判只多一次无害的故障转移，漏判会停止转移或错误地冷却健康端点。
- `TopLevelProbe` 把原本"反射 unmarshal 取 model/stream" + "独立扫描 tools 数组"两遍合并成一遍结构化扫描，且注释精确说明了它在哪些输入上与 `json.Unmarshal` 行为一致（包括 JSON null 的 no-op 语义、重复 key 的 last-write-wins）。

**问题**：

8. **`RewriteRoles` / `RewriteInputRoles` 没有 fuzz 覆盖**（重要）。项目有 `FuzzRewriteModel` 和 `FuzzRewriteStream`，各带语料库。但 `rewriteRolesInTopLevelArray` 是三者中**最复杂**的手写扫描器——它要下降进数组、遍历元素对象、识别 `"role"` key、跳过嵌套值——却完全没有 fuzz。这个函数在每个请求的路径上（只要配了 `role_map`），处理的是完全不受信的客户端 JSON。

   我人工追踪过它的内层 break 路径，没找到死循环，但逻辑脆弱到"人工追踪"不该是唯一的保证手段。

   **[已修复 2026-08-04，且比预期严重]**：补了 `FuzzRewriteRoles`/`FuzzRewriteInputRoles`，30 秒内就在种子语料上发现一个真实的**无限循环**——不是理论风险，是本节人工追踪漏掉的那个死循环。根因在共享底层扫描器 `skipJSONValue`（`classify.go`）：当输入在期望出现 JSON 值的位置直接是一个分隔符字节（`,`/`}`/`]`/空白，例如 `"role"` 键后缺失冒号导致的畸形对象）时，它的 number/字面量兜底分支会在原地返回 `(i, true)`——**声称成功但零进度**。`rewriteRolesInTopLevelArray` 的外层循环形状是 `i, ok = skipJSONValue(...); if !ok { break }; continue`，零进度的"成功"返回让它对同一字节永远重试，处理该请求的 goroutine 挂死。

   影响范围比 issue 8 本身更大：`skipJSONValue` 是共享原语，`internal/adapter/fingerprint.go` 的 `walkArrayElements`（`SessionFingerprint` 用它做 Sticky Model 会话指纹提取）是同一种循环形状，同样会挂死——而且这条路径**不需要配置 `role_map`，每个请求都会走到**，比 `RewriteRoles` 的触发面更宽。一个精心构造的畸形请求体可以挂死处理它的 goroutine，是一个真实的可远程触发的 DoS 面。

   修复：`skipJSONValue` 的 number/字面量分支加了零进度检测，入口即为分隔符时返回 `(0, false)`（"这里根本没有值可读"，而不是"读到了一个空值"），语义上更准确，且消除了整类"扫描器零进度但报成功"的问题，不只是这一个调用点。触发用例已作为 `internal/adapter/testdata/fuzz/FuzzRewriteRoles/fe06b973c84fbdf4` 提交为永久回归测试。另加 `FuzzSessionFingerprint` 给 `walkArrayElements` 同样的语料库回归覆盖。四个 fuzz target 各跑了约 750 万次迭代（30 秒/target）确认无残留死循环或 panic。

9. 三个 adapter 的 `BuildRequest` 高度重复：都是 `RewriteModel` → `RewriteRoles`(变体) → `http.NewRequestWithContext` → 复制 header → `Set("Content-Type")` → 注入凭据。差异只有认证头名（`Authorization` vs `x-api-key`）、role 数组的 key（`messages` vs `input`）、路径常量。

   我**不建议**为此抽象。三段 30 行的重复，比一个"协议参数化的请求构造器"更容易读懂和修改，而且未来某个 provider 需要特殊处理时，重复的版本改起来没有牵连。这符合项目自己的 KISS 立场。但值得在报告里点明——这是有意识的取舍而非疏忽。

10. `contentHint` 的关键词表包含 `"sensitive"`、`"敏感"`、`"合规"` 这类相当泛化的词。上游把请求内容 echo 回错误消息时（不少网关会这么做），一个正常的参数错误可能被误分类为 `ErrContent`，导致本应直接返回客户端的 400 变成遍历所有端点的无谓转移。设计文档已声明"宁宽勿窄"的取舍，属于已知代价而非缺陷。

11. `DefaultClassify` 每次分类分配两个 32KB 字符串（`string(body[...])` 一次，`ToLower` 一次）。仅在错误路径，可接受。

12. `SessionFingerprint` 用 MD5。非安全用途（会话锚点），但在 FIPS 模式的 Go 运行时下 MD5 可能不可用。极边缘。

**模块级回顾**：这是全项目技术含量最高的模块。字节拼接扫描器把"字节忠实透传"从一句口号变成了可验证的实现，而不是靠"我们尽量不改"的自觉。三个 adapter 保持极薄（65-89 行）证明了 Adapter 接口的抽象位置选对了——新增协议真的就是"一个包 + 一个 blank import"。

唯一让我不放心的是 fuzz 覆盖的不对称：最简单的重写器有 fuzz，最复杂的没有。这看起来像是"先写了 model 重写并配了 fuzz，后来加 role 重写时忘了跟上"。

---

### M4 · health / probe / strategy / sticky

**职责**：健康状态机（冷却/退避/半开单飞）、探针请求构造、调度维度与淘汰条件、会话亲和注册表。

**做得好的**：

- `health.Registry` 的 `Acquire`/`ReportSuccess`/`ReportFailure`/`ReportNeutral` 四元组设计干净。`ReportNeutral` 的存在是关键：内容策略拦截、客户端取消、`ErrClient` 这些"与端点健康无关"的结果需要释放半开探测槽但不能影响失败计数。注释明确写了"每个 acquired 的探测必须以三者之一收尾，否则端点永久锁死"——这是一个真实存在过的 bug 类别被固化成了契约。
- `router.tryOne` 里对应地在**每一条**返回路径上都调用了三者之一，包括客户端取消（`r.Context().Err() != nil`）这条容易被遗漏的路径，且带注释解释后果。
- `health` 状态活在 config snapshot 之外，所以冷却状态跨热重载存活。这是对的——重载配置不该重置"这个端点 5 分钟前刚 401 过"的知识。
- `strategy` 把 `Dimension`（排序，看不到请求）和 `Condition`（淘汰，看得到请求）**刻意分成两个接口**，并在注释里明确"不要给 `Dimension.Compare` 加请求参数"。这是一个正确的架构防线：一旦排序能看到请求，"排序"和"过滤"的边界就没了。
- `WithinContext` 刻意不做成 `Condition`，因为它建立在**估算**而非确定性之上，需要一个"永不让估算独自清空候选集"的兜底——而这个兜底只有知道过滤前候选集的调用方能正确实施。这个判断很细致。
- `probe` 的 echo-nonce 设计抓住了一类 HTTP 200 抓不到的故障：中继层返回缓存/罐头响应而真实模型从未运行。`max_tokens: 300` 而非刚好够放 nonce 的理由也写清楚了（推理模型会把预算花在 `<think>` 块里）。
- `conditions.go` 里 `thinking` 条件**刻意不注册**，理由是请求侧信号 `WantsThinking` 还没有检测逻辑，"注册一个看起来实现了但永不触发的条件，比不注册更糟"。这个克制值得称道。

**问题**：

13. **`Serve` 的健康过滤循环对每个端点做 2-3 次独立加锁**（`router.go:77-88`）：
    ```go
    if rt.Health.Status(key, now).Fails > 0 {      // 锁 1
        if rt.Health.Acquire(key, now) {           // 锁 2
    ...
    if rt.Health.Available(key, now) {             // 锁 3
    ```
    而且 `Status()` 构造了完整的 `Status` struct（含 `lastClass.String()`）只为读一个 `Fails` 字段。除性能外还有语义问题：`Status` 和 `Available` 之间状态可能变化，两次读到的不是同一个快照。

    建议合并成一个 `Registry.Classify(key, now) (available bool, needsProbe bool)`，一次加锁返回过滤决策。对本地单用户场景性能影响可忽略，但这是核心热路径上唯一一处明显的粗糙。

14. **`health.Registry.m` 无限增长**。key 是 `HealthKey()`（含 api_key 的 SHA-256 前 4 字节），永不清理。反复修改配置中的 key 会累积陈旧条目。规模上限是"历史上出现过的所有端点配置"，实践中是几十条，不构成真实问题，但确实是一个没有闭合的资源。

15. **`sticky.Registry` 的清扫在持锁状态下 O(n)**（`sticky.go:87-95`）。`entries` 的唯一清理机制是每小时一次的 `Set` 内清扫，删除 24 小时未使用的条目。24 小时内的活跃会话数没有上界。每小时一次的持锁全表扫描会阻塞该时刻的所有请求。本地场景 n 很小，但这是又一处未闭合的上界。

16. **`probe` 的 `Echoed` 用纯子串匹配**，而 nonce 也在请求体里。如果上游把请求内容回显在响应中（某些网关的调试模式），会误判为"活着"。不过状态码检查在前（非 2xx 不到这里），风险不高。

17. **`strategy` 只注册了 `priority` 一个 Dimension**。包注释说"每种调度行为（priority, weight, round_robin, ...）都只是一个 Dimension"，config 有 `strategy: [...]` 数组字段，但实际可填的值只有 `"priority"`。这是一个预留但空置的扩展点。

    我不认为这是过度设计需要删除——`Dimension` 接口本身只有 12 行，成本极低，而且 `Sort` 的多键稳定排序确实需要这个形状。但**文档和 config.example 应当明确说明当前只有 priority 可用**，否则用户会去找 `weight` 怎么配。

18. **后台探针请求不写审计日志**。`runProbe` 直接发请求、读响应、报健康，全程不经过 `audit.Record`。后果：`vmr report` 的成本统计看不到探针消耗的 token。每次探针 `max_tokens: 300`，端点频繁抖动时会持续产生不可见的成本。这是审计完整性的一个真实缺口——审计日志声称是"每请求一行"，但探针请求不在其中。

19. `runProbe` 不受 `max_concurrency` 闸门约束。N 个端点同时半开会启 N 个 goroutine。N 有界（配置的端点数），且同端点由 `Acquire` 单飞保证唯一，风险可控。

**模块级回顾**：这四个小包是"每个包只做一件事且做对"的教科书样本，尤其是 `health` 的三态收尾契约和 `strategy` 的双接口分离。

值得单独指出的架构判断：`health` 包**完全不知道**半开端点是怎么被重新验证的——探测策略（后台 goroutine、探针请求形状、超时）全在 `router` 里。这个解耦让 `health` 保持在 176 行且 98.5% 覆盖率，而探测策略可以独立演进。这是正确的职责切分。

主要遗留是三处未闭合的资源上界（health map、sticky map、探针成本不可见），都不紧急，但都属于"本地小工具没事，一旦有人拿去跑共享实例就会遇到"的类别。

---

### M5 · router（核心）

**职责**：故障转移主循环、响应归一化、路由表构建与原子安装、上游传输、并发闸、日志格式、热重载追踪、路由头。

这是项目的核心，也是被拆得最细的模块（9 个文件），且有 `archtest` 守护 `router.go` 的行数预算（700 行，当前 552）。

**做得好的**：

- **主循环的结构**（`Serve`）读起来是一条直线：健康过滤 → 硬条件淘汰 → 上下文估算过滤（带兜底）→ 排序 → sticky 重排 → 遍历尝试。每一步都有注释说明它为什么在这个位置。
- **半开端点的处理是我见过的最干净的方案**：真实流量**从不**碰半开端点。第一个注意到它未探测的请求抢下单飞槽，把它交给后台 goroutine，然后**对本次请求当作它不可用**。真实请求既不等待探测，也不被转移，只承担"发现需要探测"的那点开销。
- **`X-VMR-Failover` 头在每次尝试之前设置而不是循环之后**——因为最终成功的那次尝试会从 `forwardSuccess` 深处直接写响应头，永不返回 `Serve`。所以要把"截至目前的失败"提前写好。这个细节如果没想到，成功的故障转移就会丢失解释它的 trail。
- **`response.go` 的三态传输模式机**（undecided / buffered / passthrough）设计精巧：SSE 默认走真流式，只在第一个载荷事件证明它是 MiniMax 思考形状时才转缓冲；`<think>` 触发的缓冲在闭合标签出现后还能**恢复**流式。客户端只在思考阶段等待，而那个阶段本来也没东西可看。
- `classifyEvent` 的判据是"第一个非空 content/text 值**以**标记**开头**"，而不是"包含标记"。这样一个只是**引用**了 `<think>` 标签的回复（用户问标签格式、代码示例回显）不会被误删。同样的守卫在 `stripThinkingProcess` 里也有。
- `notePatternDetectedIfSuspected`：即使字面标记守卫没触发，只要内容形状仍像泄漏的思考大纲（足够多的编号小节 + 足够的字节量），就在审计 trail 里打一个 `thinking_process_pattern_detected` 标记。**这是给启发式规则配了一个"我可能已经失效了"的自检信号**——供应商改措辞时，`vmr report` 里会出现这个标记而不是静默失效。这个设计很少见，值得称赞。
- `NewUpstreamClient` 显式 `CheckRedirect: ErrUseLastResponse`，理由是默认策略会把 POST 的 301/302/303 悄悄改写成 GET，违反字节忠实。
- `Install` 按"不同代理解析结果"而非按 provider 建 `http.Client`，连接池按代理组共享，且旧 snapshot 被替换时 `CloseIdleConnections()`。
- `installLimiter` 只在容量真变化时替换信号量，并诚实承认容量变更期间存在短暂的超额准入窗口。

**问题**：

20. **`respStream.Read` 返回 `(0, nil)`**（`response.go:231`），违反 `io.Reader` 的约定（"不鼓励返回零字节数且 nil 错误"）。注释说明了目的（让调用方的空闲看门狗能 tick）。

    实测确认唯一消费者是 `copyFlush`（`transport.go`），它能正确处理这个返回。但这形成了一个**隐性耦合**：如果有人把 `copyFlush` 换成 `io.Copy`，会变成 CPU 打满的忙循环。建议在 `Read` 的注释里明确写"本类型只应由 `copyFlush` 消费，不满足通用 `io.Reader` 契约"，或者改用一个不冒充 `io.Reader` 的自定义接口。

21. **错误响应体超过 128KB 会被截断后转发给客户端**（`router.go:404-415`）。注释说 `uerr.body` 是逐字转发的，但 `io.LimitReader(resp.Body, errBodyCap+1)` 之后 `body = body[:errBodyCap]`——超过 128KB 的错误体会被切断，客户端收到一个**不可解析的残缺 JSON**。截断标记只加进审计副本（这部分是对的），但转发给客户端的是坏 JSON 而不是完整错误。极罕见，但这是"字节忠实"承诺的一个未被文档记录的例外。

22. **`Serve` 里 `snap := rt.snap.Load()` 无 nil 检查**。`Install` 未被调用过时会 panic。实际启动流程保证了顺序，纯属防御性缺口。

23. **`responsefix.go` 的 MiniMax 启发式没有任何配置开关**。`thinkPattern`、`stripThinkingProcess`、`softBlockMarkers` 全部硬编码，对所有 provider 无差别生效（虽然守卫很严）。如果误伤，用户唯一的出路是改代码重编译。

    建议：给 provider 或 endpoint 加一个 `response_fix: [minimax_think, minimax_thinking_process]` 的可选白名单，默认保持当前行为（全开）以免破坏现有配置。这既给了用户逃生舱，也让"这些修复是 MiniMax 专属"这个事实在配置层面变得可见。

24. `thinkingProcessEndorsement` 匹配的是英文 `"Looks good. Pro"`。同一模型输出中文思考时不匹配。属于启发式的固有局限，`pattern_detected` 兜底信号能发现它。

25. `modelFieldPattern` 在 passthrough 模式下对每个 SSE block 跑一次 `Match` + 一次 `ReplaceAll`（两遍扫描）。可以合并为直接 `ReplaceAll` 后比较结果。长流上的常数级优化，非紧急。

26. `bufferedCap = 32MB` 与 `recorderBodyCap = 64MB`、`MaxRequestBodyMB = 8` 组合起来的内存上界见 3.2 节。

**模块级回顾**：`router` 是全项目最能体现"复杂度被正确安放"的模块。故障转移主循环保持在 552 行且有测试守护行数预算；供应商特定的脏知识被隔离在 `responsefix.go` 里，且 `response.go` **只单向调用它**，反向绝不发生；传输、并发、日志、快照各自成文件。

`response.go` 的状态机是全项目最难的一段代码，但它难是因为问题本身难（在流式转发中检测并修复供应商泄漏的思考过程，同时不能误伤引用了同样标记的正常回复，还要保持真流式），不是因为实现绕。

我唯一有实质保留的是 **MiniMax 修复的硬编码**——它是唯一一处"vmr 替用户做了一个用户无法否决的内容决定"。项目其他所有地方都极力避免这种事（不加默认 anthropic-version、不跟随重定向、不读代理环境变量），这里却例外了。软拦截检测做成了纯观察（正确），但 `<think>` 剥离是真的改字节。

---

### M6 · server

**职责**：HTTP 入口、认证、请求体缓冲、图片预处理调度、审计录制、loopback-only 的 `/admin/status`。

**做得好的**：

- **认证的"自声明 tag"设计**：`api_keys` 未配置时门是敞开的，但客户端自愿发送的凭据值仍会被 `KeyTag` 提取成标签写入审计。私有网络里的调用方只要把自己的 key 结尾写成 `-alice`，`vmr report` 就能按调用方分组，vmr 侧零配置。这是把一个"没有配置就没有功能"的场景变成了"没有配置也有部分功能"。
- `KeyTag` 的规则（取末 8 字符，若含连字符则取最后一个连字符之后）配上 config 侧 16 字符最小长度校验，形成闭环：**短到会泄漏整个 key 的配置在加载时就被拒绝**。两处的关联在双方注释里互相引用。
- `computeRequestFacts` 的 `imageCount` **不是**自己扫描得来的，而是从 `imgprep.Downscale` 的返回值透传。注释解释了两个理由：正确性（`HasImage` 喂给一个无兜底的硬条件，纯文本请求若因引用了 `"image_downscale=512px"` 被误判会清空所有候选端点——这是一个**真实发生过的事故**）和成本（无图请求全程只付一次存在性检查）。
- `recorder` 正确转发了 `Flush()`，SSE 的流式延迟不受审计录制影响。
- `/admin/status` 的 `config_stale` 字段：把 config 文件 mtime 与**最后一次成功加载**的时间比较（而非最后一次尝试），所以被拒绝的热重载不会清除自己的警告。这个细节想得很到位。

**问题**：

27. **单请求最坏内存 ≈ 104MB**（重要，见 5.1 节 P0-3）：
    - 请求体缓冲：`MaxRequestBodyMB` 默认 8MB
    - 响应归一化缓冲：`bufferedCap` 32MB
    - 审计响应副本：`recorderBodyCap` 64MB（`bytes.Buffer` 增长期还有 2x 峰值）

    而 `max_concurrency` **默认为 0（无限）**。10 个并发大响应请求就是 1GB 驻留。三个常量各自都有合理理由，但**没有任何一处代码或文档把它们加起来看过**。

    **[已修复 2026-08-04]**：`UserGuide.md`/`.zh.md` 各加一段"Per-request memory budget"/"单请求内存预算"说明三者乘积及应对建议；`config.example.yaml` 的 `max_concurrency` 注释补了 104MB 估算。未改 `recorderBodyCap` 默认值——那是需要单独权衡的行为变更，不属于文档层面能一次性替用户做的决定。

28. `/v1/models` 在同一虚拟模型名同时存在于多个协议组时会返回**重复的 `id`**（`server.go:284-289` 对 protocol 和 name 双重循环，每个组合一条）。`sort.Slice` 不稳定，重复项顺序不定。OpenAI 客户端拿到重复 model id 的行为未定义。

29. `/admin/status` 的 loopback 判定基于 `r.RemoteAddr`。若 vmr 部署在同机反向代理之后，代理的 IP 是 loopback，检查会通过。项目定位是本地单机运行，风险低，但值得在 UserGuide 里明说"不要把 vmr 放在反代后面暴露"。

30. `authenticate` 用 `subtle.ConstantTimeCompare` 逐个比对，但整体遍历时间与 key 数量和匹配位置相关，且 `ConstantTimeCompare` 对长度不等的输入立即返回 0（泄漏长度）。本地工具场景无实际意义。

**模块级回顾**：server 层薄（291+158+163+84 = 696 行）且职责清晰，把"HTTP 协议细节"和"路由决策"完全隔开——`chatHandler` 做完缓冲、探测、闸门、图片、facts 之后，剩下的全交给 `rt.Serve`。

`facts.go` 里那段关于"为什么 imageCount 必须从 imgprep 透传"的注释，是整个代码库里事故驱动的注释写得最好的一处：它同时说明了正确性理由、成本理由、以及被替换掉的旧方案（`HasImageMarker` 字节扫描）具体错在哪。这种注释在半年后仍然有效。

内存上界是本模块唯一的实质缺陷，而且它是一个**跨模块**的缺陷——三个常量分别定义在 `config`、`router`、`server`，各自局部合理。

---

### M7 · audit

**职责**：每请求一行的 JSONL 审计、按天轮转、zstd 压缩、保留期清理、共享的低层读取。

**做得好的**：

- **`Write` 在锁外编码**（`sync.Pool` 借缓冲区），只有最终字节进入锁区。注释明确说明了为什么不用 Logger 上的单个共享缓冲区：审计记录可能几 MB，共享缓冲区会把 CPU 密集的 JSON 编码串行化到那把保护廉价文件写入的全局锁下。这是一个正确且不显然的判断。
- `EncodeBody` 的所有权契约（"引用而非克隆，调用方交出后不得再改"）写在注释里，且**每个调用方**都在自己那侧加了对应注释说明它交出的是新切片。这种双向注释配对在实践中很有效。
- `compressOne` 的崩溃安全：临时文件 → rename → 确认在盘 → 才删原文件；下次扫描发现 `.zst` 已存在则补做删除。`kill -9` 在任何一点都不会丢数据或留下截断文件。
- `Close` **刻意不等待**进行中的压缩清扫，理由写清楚了（压缩是崩溃安全的，只碰已轮转的文件，让关机阻塞在多 GB zstd 上毫无价值）。
- `retentionDays` 默认 0（永不删除）而不是某个"合理"的数字，理由是"审计日志是 `vmr report` 成本核算的唯一来源，静默删除不是一个值得默认掉进去的错误"。
- `ForEachLine` 对超长行的处理：有界内存排空并通过 `onSkip` 上报，而不是像 `bufio.Scanner` 那样在 `ErrTooLong` 上中止整个读取。

**问题**：

31. **多实例共享 `log_dir` 时 housekeep 可能删除他人正在写的文件**。实例 A 在今天写入，实例 B 昨天启动且 `l.date` 仍是昨天、仍持有 fd 追加写。A 的 housekeep 看到"日期不是今天"的文件就压缩并删除原文件——B 继续往已删除的 inode 写，数据永久丢失。

    实践中要求实例跨午夜运行且共享目录才会触发，很窄。但文档里没有"不要共享 log_dir"的警告。

32. **`Redact` 只脱敏固定的 7 个 header 名**。客户端若发送 `X-Custom-Token` 之类的自定义凭据 header，会明文写入审计文件。vmr 自己的 adapter 只用 `Authorization`/`x-api-key`，所以是客户端侧的风险。

33. **没有"不记录 body"的配置开关**。审计文件包含完整请求体和响应体（用户的全部对话内容），文件权限 0600。这是设计明示的取舍，但对某些用户（合规环境、共享机器）是硬阻碍。建议提供 `audit_bodies: false` 之类的选项，保留元数据（时长、token、端点、错误分类）而丢弃正文——`vmr report` 的大部分统计章节仍可工作。

34. `EncodeBody` 用 `json.Valid(body)` 做一次完整扫描，随后 `json.RawMessage` 被 Encoder 编码时又验证一次。多 MB body 上是双倍开销。次要。

35. `l.f.Write` 在锁内同步落盘。慢盘上多 MB 记录会阻塞所有并发请求的审计写入。注释强调了 encode 在锁外，却没提 write 本身阻塞。属于已接受的取舍（换成缓冲写会牺牲崩溃安全），但注释可以更完整。

36. **`examples/sample-audit.jsonl` 没有被任何测试引用**。`internal/report` 与 `audit.Record` 是编译期耦合的（CLAUDE.md 明确列为不变量），但这份示例数据只靠人工维护。schema 一变它就静默过时。加一个反序列化它的测试成本极低。

**模块级回顾**：audit 是一个"看起来简单、实际每个细节都被想过"的模块。锁外编码、所有权契约、崩溃安全的压缩、超长行的有界处理——这些都不是初版会写出来的东西。

从架构上看，`audit` 定位为"只记录 + 提供共享的低层读取"，聚合（report）和重放（replay）建在它之上、在各自的包里，同时外部脚本（jq/DuckDB）也能直接读这些文件。这个定位让 JSONL 格式成为一个**公开契约**而不是内部细节——这也是为什么 schema 变更需要同步更新 `internal/report` 被列为不变量。

隐私控制的缺失（无法关闭 body 记录）是我认为最值得补的一项，因为它直接限制了产品的可用场景。

---

### M8 · imgprep

**职责**：检测请求中的内联图片，在配置开启时降采样，带磁盘缓存。

**做得好的**：

- **磁盘缓存的真正理由不是省 CPU，而是字节稳定性**：上游 prompt cache 按精确字节/token 匹配，而通用 JPEG 编码器不保证每次输出完全一致。跳过重编码是唯一能保证上游缓存不会静默 miss 的办法。这个洞察比"缓存省时间"深一层。
- `maxDecodePixels = 64MP` 防解压炸弹：畸形图片可以声明巨大尺寸而编码字节很小。
- **`defer recover()` 且记录一行 stderr**：注释说明了为什么不能静默——本包每条 fail-open 路径都留痕（审计元数据、跳过的图片信息），一个被吞掉的 panic 会让降采样对某张图永久失效而运维侧零信号。
- **"检测到"与"可解码"刻意独立**：type 字段匹配就算检测到，即使本包的解码器读不懂它的像素头。因为 `len(images)` 被用作 `HasImage` 的权威信号，喂给一个无兜底的硬路由条件——vmr 自己不认识的格式对上游供应商仍然是真图片。
- `cacheStore` 用唯一命名的临时文件 + rename，并发请求算出同一条目时不会看到半写状态。
- 清扫节流按目录 keyed（`sync.Map`）而非单个全局标志，让用不同 `t.TempDir()` 的测试互不干扰。

**问题**：

37. **图片降采样是一个字节偏离，但不在 CLAUDE.md 的"sanctioned deviations"清单里**。CLAUDE.md 的"Byte-faithful passthrough"不变量只列了两项例外：model 名重写、以及基于证据的供应商 quirk 修复。而 `rewriteBody` 在实际改写图片时会做完整的 unmarshal → 修改 → re-marshal，**key 顺序和空白全部改变**。这是一个比 model 重写大得多的偏离，却没被列入不变量的例外清单。

    实现本身是对的（只在真正改写时重建，其余情况原样返回同一个底层数组）。问题纯在文档：不变量的表述不完整。

38. **含大图请求的内存放大**。`rewriteBody` 的层层 unmarshal/marshal（body → top map → msgs 数组 → blocks 数组 → 新 base64 字符串 → 逐层重新 marshal）会产生原 body 的 3-5 倍峰值内存。8MB 请求体上限意味着峰值可到 30-40MB，再叠加 M6 提到的其他缓冲。

**模块级回顾**：imgprep 是一个"功能边界划得很干净"的模块——只碰请求、只碰内联图片、从不抓取远程 URL、任何失败都退回原字节。这些边界让它的失败模式全部收敛到"什么都不做"。

那条"检测到 ≠ 可解码"的注释是本模块的精华：它记录了一个只有在把这个包接进硬路由条件之后才会暴露的正确性要求。

---

### M9 · chatmsg / ctxgraph（分析层基础）

**职责**：`chatmsg` 是消息/SSE/usage 的共享解析层；`ctxgraph` 是内容寻址的 manifest / 编辑分类 / lineage / 跨谱系缝合。

**做得好的**：

- `chatmsg` 从 `internal/report` 里提取出来的理由正确：`ctxgraph` 和 `story` 也需要同样的解析，否则要么重复实现要么把 report 整包导出。提取后 report 保留了 43 行的委托层（`chatmsg_compat.go`），让原有调用点和测试一字不改。
- **`ctxgraph.Classify` 的五态编辑分类（Append/ReplaceTail/Splice/Contract/Fork）刻意是纯结构化的**——不做模板/标记匹配。理由写清楚了：至少存在三种真实的 compaction 形态，其中两种根本没有标记可匹配。结构化判据（LCP 长度、覆盖率、公共后缀长度）对任何 agent 客户端一视同仁。
- 阈值常量（`contractLenRatio` 0.6、`forkCoverage` 0.5、`tailSlack` 2、`spliceMinTailMatch` 2）全部标注了校准语料（7112 条记录、168 个多轮会话），且**刻意不做成配置项**——理由是"用户无法校准他们无从测量的东西，而且这里搞错只会让报表解释得差一点，不会改变流量走向"。这个"什么该配、什么不该配"的判断很清醒。
- `spliceMinTailMatch = 2` 而非 1 的理由：防止单条短通用回复（"好的"/"OK"）纯属巧合地匹配上。
- `report/session.go` 直接消费 `ctxgraph.Lineage`/`Classify`，不再有私有的 hash/LCP 实现。CLAUDE.md 把这一点列为不变量，并说明了不这么做会产生的 bug 类别（私有实现与 ctxgraph 对"历史在哪里被重置"静默产生分歧）。`session_conformance_test.go` 有 4 个一致性测试守护这一点。

**问题**：

39. `ctxgraph/edit.go` 的注释里有 `F11`、`S2`、`Appendix C.3`、`Appendix A.7`、`T2.1`、`§5` 等 6 类编号引用，**其中 `F11`/`Appendix C.3`/`Appendix A.7`/`T2.1` 在整个 docs/ 目录中完全不存在**。见 3.1 节。

40. `coverage()` 每次调用构建一个 `map[Hash]bool`。在 lineage 扫描中对每对相邻 manifest 都会调用。离线工具，非热路径，可接受。

**模块级回顾**：这两个包是"分析层做对了"的证明。`chatmsg` 作为无状态叶子把三个消费者的解析统一了；`ctxgraph` 把"一段对话历史在哪里断裂"这个模糊问题变成了可计算、可校准、可测试的结构化判据。

`archtest` 强制 `ctxgraph` 不依赖 `report`（因为 report 已合法依赖 ctxgraph，反向会构成真实的循环风险），这个防线设得对。

唯一的问题是注释的引用体系——本模块的注释信息密度极高（每个阈值都解释了校准来源和调整方法），但相当一部分指针已经断了，读者顺着 `Appendix A.7` 去找会一无所获。

---

### M10 · report

**职责**：把审计 JSONL 聚合成 `vmr-report.{json,md}` + `vmr-requests.{jsonl,md}` + 逐请求详单。

**结构**（32 个文件，10,412 行含测试）：数据形状在 `rows.go`，聚合遍历在 `aggregate.go`，会话/任务分组在 `session.go`，渲染拆成 `render_doc.go`（运行顺序 + 表格原语）+ 每章节一个 `section_*.go`。

**做得好的**：

- **"新章节 = 新文件，不是往已有文件里加行"** 这条规则由 `archtest` 的行数预算强制。这是把一条容易被侵蚀的组织约定变成了测试失败。
- `archtest` 的注释记录了这条规则的来历：渲染器曾经是一个 1053 行的 `aggregate_render.go`。
- 大量确定性测试（`TestBuildIsDeterministic`、`TestBuildFindings*IsDeterministic`、多个平局场景）——报表输出的稳定性被当成一等公民对待。这对一个会被 diff 的 Markdown 产物是必要的。
- `pricing.go` 的货币处理：每个价格字段接受裸数字或带货币码的字符串，未定义的货币是**加载期错误而非静默归零**。
- 输出权限 0600/0700 且在 `.gitignore` 里——因为详单携带完整对话正文。

**问题**：

41. **`archtest` 的行数预算漏掉了实际最大的文件**。预算表里有 `internal/report/aggregate.go`（1000）和 `render_doc.go`（400），注释称它们是"report 的两个最大文件"。但实测：

    ```
    1059  internal/report/detail.go     ← 最大，无预算
     999  internal/report/aggregate.go  ← 有预算 1000，已用 99.9%
     978  internal/report/session.go    ← 第三大，无预算
    ```

    `detail.go` 已经超过了给 `aggregate.go` 定的那个数，而且完全不在守护范围内。`aggregate.go` 距离触发只剩 1 行——下一次改动就会撞线，届时的诱惑是"把 1000 改成 1100"，而注释已经预先反对了这件事（"split it, don't just raise this number"）。

42. `chatmsg_compat.go`（43 行纯委托）是迁移期的遗留兼容层。注释坦白说明了它的目的（让调用点和测试一字不改）。这是合理的过渡设计，但过渡期该有终点——建议要么排期消除，要么在注释里明确"这是永久保留的，因为 report 内部偏好小写命名"。

43. `report` 覆盖率 87.3%，但 `aggregate_test.go` 单文件 1277 行是全项目最大的测试文件。测试文件本身没有行数预算守护。

**模块级回顾**：report 是全项目文件数最多的包，但它的组织方式（数据形状 / 聚合 / 分组 / 渲染分层，渲染再按章节分文件）让 32 个文件读起来并不混乱。`archtest` 的行数预算是这个组织能维持下去的关键——只是预算表本身已经落后于代码。

`internal/report` 与 `audit.Record` 的编译期耦合被明确列为不变量，这是诚实的：与其假装解耦，不如承认耦合并要求同步修改。

---

### M11 · story（含 profile 子包）

**职责**：从 `ctxgraph` lineage 链重建 Journey/Task/Step/Event 叙事、九项行为剖面指标、双 Journey 对比、Markdown 渲染、可选的 LLM 解读层。

**做得好的**：

- **`llm.go` 的三层原则**：LLM 只接收本包已经算好的数字，从不自己生成新数字，输出渲染成明确标注且独立缓存的区块，绝不混进事实层章节。这个约束把"LLM 幻觉污染报表"这个风险从设计上排除了，而不是靠提示词祈祷。
- **端点解析刻意选最简方案**：手动指定一个已在运行的 vmr 实例的 `host:port` + 该实例自己的虚拟模型名，不做 config.yaml 的 provider/model 解析，不做故障转移，不自动拉起进程。整个客户端就是一次 `net/http` POST。这让 story 完全不依赖 `internal/router`/`internal/adapter`。
- `promptVersionFor(lang)` 把语言拼进磁盘缓存 key，切换 `-lang` 不会复用另一种语言的缓存解读。
- `ListCandidates` 排除单 manifest 的 lineage，理由是"一个单请求 lineage 结构上就是一次定时单发调用（OpenClaw 的 heartbeat/dream_diary 之类），本来也没有任务叙事可讲"——这是用结构而非内容标记做判断，与 ctxgraph 的哲学一致。
- `profile/` 子包隔离了唯一一处 agent 专属逻辑（真实指令/无回复判定），有 OpenClaw 感知实现和通用兜底两个版本。
- `golden_test.go` + `testdata/golden.md` + `golden_zh.md` 守护渲染输出；`invariants_test.go` 守护工具调用配对必须 100%。

**问题**：

44. **11 处引用 `_tmp/plan_sonnet-5.md`，该文件被 gitignore 且已不存在**（重要，见 5.1 节 P0-1）。**[已修复 2026-08-04]**：全部 11 处替换为事实性描述，同一行内绑在一起的失效 Appendix 编号引用一并清除（全项目 ~520 处编号引用的大扫除见 P3-3，已在后续一轮处理完毕）。分布：

    ```
    internal/story/llm.go              4 处（含包注释首段）
    internal/story/compare.go          4 处
    internal/story/render_compare.go   1 处
    internal/story/compare_test.go     2 处
    cmd/vmr/cmd_story.go               2 处
    ```

    最严重的一处在 `cmd/vmr/cmd_story.go:76`——`-llm-addr` 这个 flag 的**帮助文本**里写着 `see _tmp/plan_sonnet-5.md`：

    ```go
    llmAddr := fs.String("llm-addr", "", "... enables the optional LLM
      interpretation section on -compare's report (Step 4a; see
      _tmp/plan_sonnet-5.md). Never auto-started; ...")
    ```

    任何运行 `vmr story -h` 的用户都会看到这行，然后去找一个 `.gitignore` 里的、已被删除的文件。

45. `journey.go` 766 行，是 story 包最大的文件，无 `archtest` 行数预算守护。

46. `llm.go` 的包注释同时引用了 `Appendix C.6/G`、`§3.3/C.7`、`§5.5`——其中 `Appendix C.6`、`Appendix G`、`C.7` 在 docs/ 中都不存在。

**模块级回顾**：story 是全项目"产品想象力"最强的部分——把审计日志重建成一个 agent 任务的完整叙事，还能做双任务对比。`llm.go` 的三层约束显示了对 LLM 输出可信度的清醒认识。

但它也是**注释引用腐化最严重**的模块。原因不难推测：这是最近开发的功能，开发时有一份详细的 `_tmp/plan_sonnet-5.md` 作为工作文档，代码注释大量引用它，功能完成后计划文件按约定删除了（CLAUDE.md 要求"delete on completion"），但注释里的指针没人清理。

这暴露了一个流程缺陷：**CLAUDE.md 要求完成后删除计划文件，但没有要求先清理代码里对它的引用**。

---

### M12 · i18n

**职责**：`vmr report` / `vmr story` 输出的中英双语文本，零依赖叶子包。

**做得好的**：

- **按产出文件组织而非单一大目录**：`report_workload.go` 配 `internal/report/section_workload.go`，`story_render.go` 配 `internal/story/render_md.go`。理由是"措辞改动待在渲染它的代码旁边的一个小文件里，而不是一个条目会与其支撑的章节静默失配的独立目录"。这个判断我同意——集中式 i18n 目录在实践中几乎总会积累死条目。
- 零依赖，与 `core`/`fmtutil` 同层，所以 report、story、cmd 都能依赖它而不违反 `archtest` 的边界。
- `Lang` 零值是 `EN`，增量迁移中未更新的调用点按构造保持英文行为。
- **只有 `cli.go` 用 `Sprintf`**（16 处），其余全是字符串拼接。这消除了整整一类"格式化参数数量错配导致 `%!d(MISSING)`"的风险——这在低覆盖率的 i18n 包里是很重要的防护。

**问题**：

47. **`Parse` 的注释声称大小写不敏感，实现是穷举**（`lang.go:39-50`）：
    ```go
    // Parse accepts "en"/"english" and "zh"/"chinese"/"zh-cn" (case-insensitive)
    case "en", "EN", "english", "English":
    ```
    `"Zh"`、`"EnGlish"`、`"CHINESE"` 都会失败并返回错误。修复是一行（`strings.ToLower(s)`），但更重要的是注释在说谎。

48. **覆盖率 17.3%，全项目最低**。绝大部分文本函数从未被任何测试调用。缓解因素有两个：无 `Sprintf` 参数风险（见上），且 `cmd/vmr/i18n_e2e_test.go` 有 10 个端到端测试真实跑通了 zh 路径的报表和叙事生成。

    但仍有未走到的 ZH 分支——具体说，只在特定报表章节触发的文案（某些错误类别、某些边界统计）可能从未被渲染过。

**模块级回顾**：i18n 的组织方式是一个不落俗套的正确选择。放弃"集中 catalog"换取"文本贴着渲染代码"，在这个规模（1989 行、20 个文件）下是对的。

覆盖率数字看着吓人，但实际风险被两个因素压住了（无格式化参数、有 e2e 覆盖主路径）。真要提升，最经济的做法不是补单测，而是加一个遍历所有 `*Text(lang)` 构造器、断言每个字段非空的反射测试。

---

### M13 · diagnose / replay

**职责**：`vmr diagnose` 做真实连通性检查；`vmr replay` 重放单条审计记录。

**做得好的**：

- 两者都复用真实流量使用的 `adapter.BuildRequest` / `router.NewUpstreamClient`。`replay` 的包注释明确："重放的请求逐字节等于 vmr 当初会发出的，不是它的近似"。这个保证只有靠复用同一份构造代码才成立。
- `diagnose` 在 `cfg.Check()` 发现任何问题时**跳过整个连通性测试**——对一份已知操作性损坏的配置拨号毫无意义。
- `replay` 提供三种选记录的方式（`-detail` 单记录文件、`-ts` 时间戳、`-line` 行号），且 `Run` 校验互斥。`-detail` 直接吃 `vmr report` 已经写出的 `details/*.json`，不需要数行号——这是把两个工具串起来的正确接口。
- `audit.IsCredentialHeader` 的存在专门服务于 replay：审计记录里的凭据 header 是掩码占位符（`Bearer ***c1d4`），重放时必须剥掉，否则会把假凭据当真的发给上游。这个跨模块的坑被显式命名并导出成函数。
- `diagnose` 覆盖率 93.8%，`replay` 82.6%，测试文件分别 1015 行和 756 行。

**问题**：

49. `probe.RoleCompatRequest` 只被 `diagnose` 使用（两消息形状，用于验证 provider 是否接受 `developer` 角色或 role_map 是否正确改写）。注释说明了为什么运行时探针刻意保持单消息的最小形状。这个不对称是有意的，不是问题——但意味着 `probe` 包同时服务两个用途不同的调用方，未来若形状需求分化会有压力。

**模块级回顾**：这两个命令是"可运维性"的具体体现，而且**建对了地基**——不是重新实现一遍请求构造，而是复用生产路径。这让它们的诊断结论真正有说服力：`vmr replay` 说"这样发出去会失败"，那就是真的会失败。

`diagnose` 在配置有问题时拒绝拨号，是一个很好的产品判断：不要让用户在一份坏配置上看一堆网络错误然后猜哪个才是根因。

---

### M14 · cmd/vmr + shell 脚本 + loadtest

**做得好的**：

- `main.go` 只有 71 行：dispatch + usage + adapter blank import 注册点。一个子命令一个文件的约定被严格遵守。
- `cmd_start.go` 的启动/关闭日志设计：ASCII banner（无 unicode 制表符，任何终端/`less`/`grep` 管道下渲染一致）+ 一行带时间戳可 grep 的 `VMR START pid= config= listen=`，关闭时对应的 `VMR STOP reason= uptime=`。**每个 START 都有配对的 STOP**，日志文件自己就能回答"这个进程跑了多久、为什么停的"。
- 捕获 `SIGTERM` 做优雅排空（10 秒），因为 vmr.sh / systemd / launchd 都用 SIGTERM，而 Go 默认不捕获——不处理的话进程会在请求中途直接消失且日志无痕。
- `stampWriter` 包装 writer 而不是改每个 `logger.Printf` 调用点，利用了"`log.Logger` 每行恰好调一次 `Write`"这个事实。
- `vmr.sh cmd_start` 先跑 `vmr check`（拒绝守护化一份坏配置），再检测端口占用（区分"另一个 checkout 占了同一个端口"和裸的 bind 错误），再 `warn_if_stale`（源码比二进制新）。这三层预检查覆盖了实际最常见的三种启动失败。
- `loadtest` 的 12 个场景各自对应一条成本剖面真正不同的代码路径（真流式 / 全缓冲思考泄漏 / 缓冲后恢复 / 大响应 / 单图多图 / GIF 快路径 / 长历史 / 故障转移 / 三种协议基线）。
- loadtest 的服务端数字**直接解析本次运行自己的审计 JSONL**，从不 shell out 到 `vmr report`、从不 import `internal/report`。理由写得很对："压测的结果不能依赖另一个命令的渲染流水线"。
- 客户端百分位按 `plain`(9 场景) / `image`(3 场景) 分组而非混成一个数——图片处理是 vmr 最贵的路径，混在一起会让 p95/p99 主要在讲那 3 个场景。

**问题**：

50. **685 行 shell 脚本零测试覆盖**。`vmr.sh` 包含 launchd plist 渲染、systemd unit 渲染、服务安装/卸载、环境文件生成（0600）、跨平台进程发现、端口占用检测。这些是用户最先接触的代码，也是出错时最难诊断的（渲染错的 plist 会静默不启动）。

    最低成本的改善是加 `shellcheck` 进 CI（本机未安装，无法在本次评审中运行）。

51. **`main.go` 的 usage 没有列 `-lang`**。`vmr report` 和 `vmr story` 都支持 `-lang en|zh`（有 10 个 e2e 测试），usage 的两行里都没有它。用户只能从 `-h` 的 flag 列表里发现。

52. usage 首行 `vmr <start|check> [-c config.yaml]` 把 start 和 check 合并，而下面每个命令都单独一行，结构不一致；`vmr version` 孤零零在最后。

53. `svc_install` 时跑了一次 `check`，但服务自动重启（launchd `KeepAlive` / systemd `Restart=always`）时不跑。与 M2 的问题 4 同源。**[已修复 2026-08-04]**：见问题 4 的修复说明——自动重启触发的 reload 现在与 fsnotify/SIGHUP 走同一条已接入 `Check()` 的代码路径。

**模块级回顾**：CLI 层薄且规整。`cmd_start.go` 的日志设计（配对的 START/STOP 标记、重载的 bar 分隔）显示了对"事后从日志重建时间线"这个真实运维场景的理解。

`loadtest` 是一个惊喜——它不是那种"跑个 hey 看 QPS"的应付性压测，而是按代码路径的成本剖面精心设计的 12 个场景，还想清楚了结果的独立性（不依赖 report）和分组的合理性（图片单独统计）。

Shell 层是全项目唯一一块完全无测试守护的代码，且恰好是用户接触面最大的部分。

---

### M15 · 文档体系、配置示例、CI

**做得好的**：

- `config.example.yaml`（235 行）**覆盖了全部 33 个 YAML 字段**——我逐个 grep 验证过，无遗漏。这在配置驱动的工具里很难得。
- `UserGuide.md` / `.zh.md` 双语同步，且用注释掉的 YAML 块形式讲解配置项，每项都带默认值和后果说明。
- 设计文档分成 Core（路由核心，894 行）和 Analytics（报表叙事，376 行）两部分，各有"关键决策与取舍"表，Core 还有"已识别、暂不落地的清理项"表。CLAUDE.md 明确要求"在把某个看起来奇怪的行为当成 bug 之前，先查这两张表"。
- `.gitignore` 写得非常仔细，每条规则都有注释解释（为什么 `/logs/` 也覆盖了 `logs/loadtest/`、为什么 `details/` 绝不能提交）。
- CI 跑 `go vet` + `go build` + `go test -race`。release workflow 做 4 平台交叉编译 + tarball + sha256 checksums + 自动 release notes。

**问题**：

54. **CLAUDE.md 说 "No Makefile, no CI config in-repo"（第 34 行），但仓库里有两个 workflow**。这是 CLAUDE.md 里唯一一处与事实直接矛盾的陈述，而 CLAUDE.md 的定位是"给 AI 助手的项目简报"——一条错误的事实会直接误导后续的工作。

55. **CLAUDE.md 的模块表遗漏了 `internal/i18n`**——一个 20 文件、1989 行的完整包。表里列了 `archtest`（2 文件）却漏了 i18n。

56. **CLAUDE.md 未提及 `internal/report/pricing.go`**（365 行 + 236 行测试）的成本估算能力，也未提及 `pricing.yaml` 这个仓库根目录的配置文件。

57. **CLAUDE.md 的 "Byte-faithful passthrough" 不变量未包含图片降采样**（见问题 37）。

58. CI 只在 `ubuntu-latest` 上跑，但主力开发平台是 darwin（`vmr.sh` 有专门的 launchd 分支）。macOS 特有的路径（`os.UserHomeDir` 行为、`$TMPDIR` 清理假设、launchd 渲染）在 CI 中从不执行。

59. CI 没有 `golangci-lint` / `staticcheck`，没有覆盖率门槛，没有 `gofmt -l` 检查。考虑到项目自称"不做 linter 的活，交给工具"，CI 里没有 linter 是个空档。

60. **依赖略过时**：
    ```
    klauspost/compress  v1.19.0 → v1.19.1
    golang.org/x/image  v0.43.0 → v0.44.0
    golang.org/x/sys    v0.13.0 → v0.47.0   (indirect，落后 34 个小版本)
    golang.org/x/text   v0.38.0 → v0.40.0   (indirect)
    ```
    `x/sys` 落后较多。没有 Dependabot 或等价机制。

61. `docs/` 下有 23 个 `distribution-strategy/` 文件和 4 个 `future-strategy/` 文件（合计约 3,500 行），是市场/内容策略材料而非技术文档。它们与工程文档混在同一个 `docs/` 目录下，且大部分带模型名后缀（`_gemini-3.6-flash.md`），看起来是生成物而非维护中的文档。建议分离到 `marketing/` 或独立仓库——否则 `docs/` 作为"技术文档入口"的信号被稀释了。

62. README 里有一节标题是 `## §0 Summary`——在面向新用户的 README 里出现内部编号，是编号引用习惯外溢的症状。

**模块级回顾**：技术文档（设计文档 + UserGuide + config.example）的质量与代码相称：每个决策都有理由，每个配置项都有默认值和后果说明，双语同步。

问题集中在两处：**CLAUDE.md 已经落后于代码**（错误陈述 1 处、遗漏 2 处、不变量不完整 1 处），以及 **CI 覆盖面偏窄**（单平台、无 linter、无依赖更新机制）。

`docs/` 目录混入大量非技术材料，是一个组织问题而非质量问题，但它影响"新人从哪里开始读"这个第一印象。

---

## 3. 横切分析

### 3.1 注释引用体系已系统性失效（本次评审最重要的发现）

CLAUDE.md 的 "Conventions" 一节写着：

> **No section numbers in cross-references** — 在代码注释和文档之间，写章节的名字（"the sticky-effectiveness section"、"the decided-not-to-fix table"）或用一个短句描述事实，**永远不要写 `§6.5` / `Appendix C.6` / `F9`**。章节号在每次文档编辑时都会重排；名字或简短描述能存活，数字会静默过时并指向错误的地方。

这条规则是对的。而且**它描述的问题已经大规模发生了**。

代码中的编号引用统计（`internal/` + `cmd/`）：

| 引用形式 | 出现次数 | 状态 |
| --- | --- | --- |
| `§x` / `§x.y` | 约 340 处 | 部分有效，大量失效 |
| `Appendix X.Y` | 约 55 处 | **几乎全部失效** |
| `F<n>` | 约 70 处 | 仅 F9/F10 有效 |
| `T<n>.<n>` | 约 55 处 | **全部失效** |

具体核对（两份设计文档的实际章节结构 vs 代码引用）：

**Core 文档实际有**：1, 2, 3, 3.1, 4, 4.1-4.3, 5, 5.4, 5.5, 6, 6.1-6.5, 7, 7.1, 8, 9, 9.1-9.5, 10, 11, 12, 13, 13.1-13.2, 14, 15, 15.1-15.2

**Analytics 文档实际有**：0, 1, 2, 2.1-2.6, 3, 3.0-3.8, 4, 4.1-4.7, 5, 6, 7, 8

**确定失效的引用**：

| 代码里的引用 | 事实 |
| --- | --- |
| `§1.1` `§1.2` `§1.3` `§1.4` `§1.5` `§1.6` | 两份文档的 §1 都没有子章节 |
| `§6.6` `§6.7` | Core 只到 6.5；Analytics §6 无子章节 |
| `§7.2` `§7.3` | Core 只有 7.1；Analytics §7 无子章节 |
| `§5.1` `§5.6` | 两份文档的 §5 都无对应子章节 |
| `Appendix A.3` `A.7` `C.3` `C.4` `C.5` `C.6` `C.7` `E.2` `F.5` `F.7` `G` `H.4` | **全 docs/ 目录中只出现过一次 "Appendix E"**，其余全部不存在 |
| `F1` `F2` `F4` `F5` `F6` `F8` `F11` `F12` | 文档中只有 F9、F10 |
| `T2.1`-`T2.5`、`T3.1`-`T3.3` | 文档中一处都没有 |

**更隐蔽的问题**：即使是"能找到对应章节"的裸 `§6.4`，也是**歧义的**——两份文档都有 §6，读者不知道该翻哪本。只有少数引用写全了文件名（`docs/VirtualModelRouter_Design_v4_Core.md §6.4`，16 处），这些是有效的。

**为什么这重要**：这些注释的信息密度非常高（阈值的校准来源、某个判断的取证过程、某个非显然设计的理由）。它们**本来**是这个代码库最有价值的资产之一。一个断了的指针不只是"找不到"——它会让读者怀疑整段注释的可信度，进而降低阅读注释的意愿。

**为什么会发生**：CLAUDE.md 自己给出了答案，且明确说了"这条规则从现在起生效——代码里已有的编号引用不会一次性清扫"。也就是说，**规则的制定者已经知道存量问题存在，只是选择了不处理**。本次评审的贡献是量化了它的规模（约 520 处引用，其中过半失效）并证明它已经不是理论风险。

**独立的一类**：11 处引用 `_tmp/plan_sonnet-5.md`（M11 问题 44）。这个更糟——它引用的文件在 `.gitignore` 里，从设计上就不可能被任何读者看到，而且实测已经不存在了。其中一处还印在了用户可见的 CLI 帮助文本里。

### 3.2 资源上界未闭合

三个缓冲上限分别定义在三个包，各自局部合理，从未被合起来看过：

| 常量 | 位置 | 值 | 理由（各自都成立） |
| --- | --- | --- | --- |
| `MaxRequestBodyMB` | `config` | 8MB | 稳定性上限，防超大请求 |
| `bufferedCap` | `router/response.go` | 32MB | 防失控上游把归一化缓冲撑爆 |
| `recorderBodyCap` | `server/recorder.go` | 64MB | 防超大响应让审计副本无界增长 |

单请求最坏驻留 ≈ 104MB，且 `bytes.Buffer` 增长期还有约 2x 峰值。而 **`max_concurrency` 默认为 0（无限）**。

再叠加 M8 提到的图片处理内存放大（含大图请求的 3-5 倍峰值），一个"8MB 含图请求 + 大响应"的并发场景很容易吃掉数百 MB。

这不是 bug——每个常量都做了正确的局部决策，`max_concurrency` 默认无限对本地单用户也合理。但**没有任何一处文档告诉用户这三个数字的乘积意味着什么**，也没有任何测试或断言把它们关联起来。

`internal/health.Registry.m` 和 `internal/sticky.Registry.entries` 是另外两处无上界的 map（M4 问题 14/15），规模小得多。

### 3.3 并发正确性

**结论：干净。** `go test -race ./...` 全绿，且并发相关的设计都有显式的推理：

- 两个 init 期注册表（`adapter.registry`、`strategy.conditions`）用 `atomic.Pointer` 无锁读 + mutex 保护的 copy-on-write 写，且**注释明确指出没有写锁会静默丢更新**，有 `-race` 测试守护。
- `reloadTracker` **刻意**用 mutex 而非 atomic swap，理由写清楚了（真正罕见的写、`Count` 是 read-modify-write）。这种"知道有个更炫的模式但这里不适用"的判断很少见。
- `Snapshot` 整体原子替换，在途请求保留启动时的版本。
- `audit.Logger` 的编码在锁外、写入在锁内，理由充分。
- `imgprep` 的清扫节流按目录 keyed，避免测试间干扰。
- `copyFlush` 的 goroutine 生命周期正确：`defer close(done)` + caller 的 `defer body.Close()` 组合保证读 goroutine 一定退出。

已知的可接受窗口都被注释承认了：limiter 容量变更期的短暂超额准入、`Status`/`Available` 两次读之间的状态漂移。

### 3.4 错误处理姿态

全项目一致地采用"**fail-open 到不做事，fail-fast 到不启动**"：

- 配置层 fail-fast：拼错的 key、非法的 URL、矛盾的 proxy 设置全是加载错误。
- 运行时 fail-open：`imgprep` 任何解析/解码失败退回原字节；`response.go` 任何存疑都退回"未修改"；`RewriteModel` 扫描器处理不了就走通用路径；`config.Watch` 出错时 SIGHUP 仍可用。
- 审计失败**从不**影响请求：错误返回给调用方记日志然后忽略。

这个姿态是对的，也被严格执行了。

唯一的例外是 `config.Check()` 的检查结果在自动路径上被完全跳过（3.1 之外本次评审最重要的功能性发现）——那些检查恰恰是"配置在语法上合法但操作上损坏"的唯一防线。

### 3.5 测试策略

**优点**：

- 测试/生产比 0.94，多数包 82-98% 覆盖率。
- 有 fuzz（2 个）、有 golden 文件（story 的中英双语）、有不变量测试（工具调用配对必须 100%）、有一致性测试（report 的会话分组必须与 ctxgraph 一致，4 个测试）、有确定性测试（报表输出在多次运行/平局场景下必须字节一致）、有架构测试（import 边界 + 文件行数预算）、有 e2e（i18n 双语走通完整报表生成）、有 benchmark（`BenchmarkBuild`）。
- `archtest` 这个包的存在本身就是一个好主意：把"文档里写过但从没人检查"的架构规则变成测试失败。它的包注释说得很准："一个没有自动检查的绊线，是一根没人真正看见它被绊到的绊线。"

**缺口**：

| 缺口 | 影响 |
| --- | --- |
| 685 行 shell 零覆盖 | 用户接触面最大的代码 |
| `RewriteRoles`/`RewriteInputRoles` 无 fuzz | 最复杂的扫描器，处理不受信输入 |
| `archtest` 行数预算漏掉实际最大文件 | `detail.go`(1059) / `session.go`(978) / `journey.go`(766) |
| `archtest` import 边界只覆盖 3 个包 | `core`"无内部依赖"、`fmtutil` 零依赖、`adapter` 不依赖 router/server 都没检查 |
| `examples/sample-audit.jsonl` 无测试 | schema 变更时静默过时 |
| `i18n` 17.3% | ZH 分支存在未执行路径 |
| CI 单平台 | macOS 特有路径从不在 CI 执行 |

### 3.6 性能

设计上有明确的性能意识，且都是**有依据的**优化而非猜测：

- `Endpoint.Freeze()`：避免热路径上重复 SHA-256（约 7 个调用点）。
- `RewriteModel` 字节拼接：避免每次转移尝试的完整 parse + copy。
- `TopLevelProbe`：把两遍扫描合并成一遍。
- `imageCount` 透传：无图请求全程只付一次存在性检查。
- `decide()` 的 `scanned` 游标：让 ping 密集的流保持线性而非平方。
- `audit` 的 `sync.Pool` + 锁外编码。
- `Install` 按代理解析结果共享 `http.Client`：连接池按组复用。
- 注册表无锁读。

剩余的小开销（`DefaultClassify` 的两次 32KB 分配、`modelFieldPattern` 的两遍扫描、`EncodeBody` 的双重 JSON 验证、健康过滤的 2-3 次加锁）都在非关键路径或影响可忽略。

`loadtest` 的 12 场景设计说明性能是被真实测量过的，不是靠感觉。

### 3.7 安全姿态

**做得好的**：

- header 黑名单默认转发 + 小黑名单，每一条黑名单项都注明了理由（`accept-encoding` 那条尤其好：转发客户端的 Accept-Encoding 会让 Go Transport 的透明 gzip 失效，归一化器会对 gzip 字节跑正则，客户端收到压缩字节却没有 `Content-Encoding` 头）。
- 凭据在审计中掩码（保留 auth-scheme 前缀，只留末 4 字符）。
- `api_keys` 最短 16 字符的校验，专门服务于 `KeyTag` 不泄漏整个 key。
- 审计文件 0600、报表输出 0600/0700、服务环境文件 0600。
- `maxDecodePixels` 防解压炸弹；`imgprep` 的 `recover()` 防解码器 panic。
- `headerSafe` 剥离不能出现在 header 值里的字符。
- `/admin/status` loopback-only。
- 从不跟随上游重定向。

**可改进**：

- `Redact` 只覆盖固定 7 个 header 名，自定义凭据 header 会明文入审计（问题 32）。
- 无法关闭 body 记录，审计文件含全部对话正文（问题 33）。
- loopback 判定在反代场景下可绕过（问题 29）。
- 认证的时序侧信道（问题 30，本地场景无意义）。

### 3.8 抽象与扩展点

项目在"什么该抽象"上判断得很准：

**做对的抽象**：`Adapter`（新协议 = 一个包 + 一个 blank import，三个实现各 65-89 行证明位置选对了）、`Dimension`/`Condition` 双接口、`chatmsg` 共享解析层、`ctxgraph` 内容寻址层。

**刻意不抽象的**：三个 adapter 的 `BuildRequest` 重复（问题 9）、`sticky` 的指纹与 `report` 的会话锚点是两份独立实现（注释明确说"故意的，不是疏忽"，因为两者的容错取舍相反）、`facts.go` 的 `indexUnescapedQuote` 与 `adapter` 的 `skipJSONString` 各留一份。

**空置的扩展点**：`strategy` 只有 `priority` 一个 Dimension（问题 17）。这是唯一一处"接口预留了但只有一个实现"，成本很低（接口 12 行），但配置字段 `strategy: [...]` 会让用户以为有别的选项。

**明确拒绝的**：无运行时插件系统（只有编译期 blank import）、无 provider SDK、无跨协议翻译、无 canonical IR。这些"不做什么"的决定比"做了什么"更能说明架构的清醒程度。

---

## 4. 项目级回顾

### 4.1 这个项目的核心判断是什么

vmr 的立身之本是一句话：**永不翻译协议，只改 URL / key / model 字段，其余字节原样透传。**

这个判断的价值在于它**同时**解决了三个问题：

1. **正确性**：不翻译就不会翻译错。LiteLLM 类工具的 bug 有相当比例出在协议转换的边角（工具调用格式、多模态块、流式事件序列）。
2. **前向兼容**：供应商加了新参数？直接透传，vmr 不需要改代码。
3. **可验证性**："客户端通过 vmr 收到的字节 == 直连收到的字节"是一个可以逐字节检验的命题，而"翻译是否正确"不是。

围绕这个核心，所有其他决策都能推导出来：为什么用字节拼接而非 unmarshal/marshal、为什么不加默认 `anthropic-version`、为什么不跟随重定向、为什么响应归一化的每一条都要有严格的内容守卫且存疑就退回不修改、为什么 `X-VMR-*` 头被明确列为"vmr 自产元数据"这个例外类别。

**这是一个有单一核心洞察并围绕它一致执行的项目。** 这比"功能很多"稀有得多。

### 4.2 复杂度分布是否合理

| 层 | 生产代码 | 判断 |
| --- | --- | --- |
| 路由核心（core/config/adapter/health/probe/strategy/sticky/router/server/audit/imgprep） | 约 9,000 行 | 合理。核心循环 552 行且有预算守护 |
| 分析层（chatmsg/ctxgraph/report/story/i18n） | 约 12,000 行 | **偏重** |
| 工具（diagnose/replay/loadtest） | 约 2,300 行 | 合理 |
| CLI + shell | 约 2,600 行 | 合理 |

**分析层（report + story + i18n + ctxgraph + chatmsg）的生产代码量已经超过路由核心。** 这是一个值得正视的事实。

我的判断：**这不是失控，但需要被明确承认**。理由：

- `vmr report` / `vmr story` 是**离线只读消费者**，不在请求路径上，不被 `internal/router`/`internal/server` 导入，`archtest` 强制这个边界。它们再大也不会拖累路由的正确性或性能。
- 从产品角度，"能把 agent 任务的执行历史重建成叙事、能对比两次任务的行为差异"是 vmr 相对于同类工具的真正差异化能力——一个纯路由器很容易被替代，一个能解释 agent 到底花了多少钱在什么地方的工具不容易。
- 但 CLAUDE.md 的开篇是"Local-run, single-binary, config-driven LLM router"，把分析工具描述成"Beyond routing, two offline tools"。**代码量的事实是分析层已经是主体**。这个定位的偏差应该在文档里被诚实反映，否则新贡献者会对这个仓库的重心产生误判。

### 4.3 演进痕迹与技术债

从注释和结构能清晰读出演进路径：

1. 单协议路由器 → 双协议 → 三协议（`openai-responses` 是最近加的，`c963db9`）
2. 审计日志 → `vmr report` → `vmr story`（叙事重建）→ `-compare` → LLM 解读层
3. 单文件 `router.go`(948 行) → 拆成 9 个文件 + `archtest` 预算守护
4. 单文件 `aggregate_render.go`(1053 行) → `render_doc.go` + 每章节一个文件
5. `report` 私有的消息解析 → 提取成 `chatmsg` 共享层（留下 43 行委托层）
6. `report` 私有的会话分组 → 改为消费 `ctxgraph`（留下 4 个一致性测试）
7. 英文单语 → `internal/i18n` 双语

**技术债总量非常低**：零 TODO/FIXME，两处已知的过渡结构（`chatmsg_compat.go` 的 43 行委托、`strategy` 的空置扩展点）都有注释说明。

**但有一类债在快速累积且无人管理**：注释里的引用腐化（3.1 节）。它的累积速度与开发速度成正比——每次开发一个新特性，就会产生一批指向当时工作文档的引用，特性完成、工作文档删除，引用就成了悬空指针。`internal/story` 是最新的模块，也是这个问题最严重的模块，这个相关性不是巧合。

### 4.4 与自身声明的一致性

我把 CLAUDE.md 声明的每条不变量与代码逐条对照：

| 声明 | 核对结果 |
| --- | --- |
| 字节忠实透传，只有 model 重写 + quirk 修复两类例外 | **不完整**——图片降采样是第三类，未列 |
| 无 provider SDK | 属实（4 个依赖，全是基础库） |
| 只有编译期插件注册 | 属实 |
| 严格 YAML（`KnownFields`） | 属实 |
| `Condition` 与 `Dimension` 分离 | 属实，且注释显式防守 |
| adapter 从不碰响应体 | 属实 |
| 审计 0600 / 报表 0600-0700 | 属实 |
| `report` 与 `audit.Record` 编译期耦合 | 属实且被承认 |
| `ctxgraph`/`chatmsg` 是唯一的共享真相源 | 属实，有 4 个一致性测试守护 |
| 注册表写路径必须加锁 | 属实，有 `-race` 测试 |
| `archtest` 强制边界与行数 | **部分**——边界只覆盖 3 个包，行数漏掉最大文件 |
| "No Makefile, no CI config in-repo" | **错误**——有两个 workflow |
| 模块表 | **遗漏 i18n** |
| 不用编号交叉引用 | **存量约 520 处，过半失效** |

**13 条中 4 条有偏差。** 对于一份自称"只做定位，不复述文档"的简报，这个准确率需要提高——因为它是 AI 助手和新贡献者的第一入口，一条错误陈述的传播成本很高。

### 4.5 这个项目最值得学习的三件事

1. **`archtest` 包**：把"设计文档里写过但从来没人检查"的规则变成测试失败。行数预算的注释直接写明"超了就拆文件，不要改这个数字"——它连规避手段都预先堵上了。这个模式适用于任何有架构约定的项目。

2. **给启发式规则配自检信号**：`notePatternDetectedIfSuspected` 在字面标记守卫没触发、但内容形状仍然可疑时，往审计 trail 打一个标记。这样供应商改措辞导致规则失效时，运维侧**会看到信号**，而不是静默降级。绝大多数项目的启发式规则失效时是完全无声的。

3. **注释解释"被替换掉的旧方案错在哪"**：`facts.go` 里关于 `imageCount` 必须从 imgprep 透传的那段注释，同时说明了正确性理由、成本理由、以及旧的 `HasImageMarker` 字节扫描具体在什么场景下出的事故。这种注释半年后仍然有效，因为它记录的是**为什么不能改回去**。

---

## 5. 问题清单与建议

### 5.1 P0 — 建议优先处理

**全部 3 项已于 2026-08-04 核实并修复**，逐项复核结论与实际改动见下；未采纳的子建议单独注明理由。

**P0-1 · 清理 `_tmp/plan_sonnet-5.md` 的 11 处悬空引用 — ✅ 已修复**

- 影响：`cmd/vmr/cmd_story.go:76` 的 `-llm-addr` 帮助文本**打印给终端用户**，指向一个 gitignore 且已删除的文件。
- 位置：`story/llm.go`(4)、`story/compare.go`(4)、`story/render_compare.go`(1)、`story/compare_test.go`(2)、`cmd/vmr/cmd_story.go`(2)
- 建议：把每处引用替换成一句话的事实描述（例如 "Addr 是唯一开关：不设则整个 LLM 段落不渲染"），CLI 帮助文本里直接删掉该引用。
- **实际改动**：按建议逐处替换；同一行内绑定的失效 Appendix 编号引用一并清除；全项目 ~520 处编号引用的大扫除见 P3-3，已在后续一轮处理完毕。
- 顺带建议（未采纳）：在 CLAUDE.md 的"Think and Plan"约定里补一条删除计划文件前先 grep 引用的规则——该约定实际位于用户全局私有 `~/.claude/CLAUDE.md`，不在本仓库范围内，不予代管。

**P0-2 · 在 `vmr start` 和热重载路径上调用 `config.Check()` — ✅ 已修复**

- 影响：热重载接受一份 `api_key` 为空（`${VAR}` 拼错/未导出）的配置，静默把所有流量打成 401，日志无任何提示。service 模式自动重启同理。
- 位置：`cmd/vmr/cmd_start.go:83-115`（启动）、`cmd_start.go:132-151`（reload 闭包）
- 建议：两处都在 `config.Load` 成功后调用 `newCfg.Check()`，把每个 `Issue` 打成一行 `WARN`。**不建议**让它阻止启动/重载——`Check()` 的问题按定义是"能跑但可能不对"，硬失败会让一个可恢复的小问题变成服务中断。但必须可见。
- **实际改动**：新增 `logConfigCheckIssues` helper，两处调用点均按建议接入；已用真实二进制端到端验证（启动、SIGHUP、fsnotify 三条路径都正确打印 WARN）。
- 可选增强（未采纳）：把 `Check()` 的结果暴露到 `/admin/status`——超出这项修复本身要解决的问题（静默生效的操作性损坏配置），会牵连 `ReloadState`/`admin.go` 及其测试，留作后续。

**P0-3 · 把三个缓冲上限的乘积效应写进文档并给出防护建议 — ✅ 已修复（文档层）**

- 事实：8MB(请求) + 32MB(归一化) + 64MB(审计) ≈ 104MB/请求，`max_concurrency` 默认无限。
- 建议（按成本递增）：
  1. 在 `UserGuide.md` 的配置节加一段"内存预算"，把三个数字和 `max_concurrency` 的关系写清楚，给出"共享实例请设置 `max_concurrency`"的建议。
  2. 在 `config.example.yaml` 的 `max_concurrency` 注释里补一句最坏内存估算。
  3. 考虑给 `recorderBodyCap` 降一档（64MB 的审计副本对绝大多数场景是过量的）或让它可配置。
- **实际改动**：采纳第 1、2 档（`UserGuide.md`/`.zh.md` + `config.example.yaml` 注释）。第 3 档**未采纳**：降低 `recorderBodyCap` 默认值是一个真实的行为变更（可能让大响应的审计记录不完整），需要单独权衡取舍，不是这份报告能替用户一次性做的决定。

### 5.2 P1 — 值得排期

**P1-1 · 给 `RewriteRoles` / `RewriteInputRoles` 补 fuzz — ✅ 已修复，且发现并修复了一个真实死循环**
最复杂的手写扫描器、处理不受信输入、却是三个重写器里唯一没有 fuzz 的。复用 `FuzzRewriteModel` 的形状即可。**实际结果**：不止补了覆盖——fuzz 30 秒内就在 `internal/adapter/classify.go` 的共享扫描器 `skipJSONValue` 里找到一个真实的无限循环（畸形请求体可挂死处理它的 goroutine，且触发面不限于配了 `role_map` 的端点，`SessionFingerprint` 的会话指纹提取共享同一原语，每个请求都会走到）。已修复 + 落地 4 个 fuzz target（`FuzzRewriteRoles`/`FuzzRewriteInputRoles`/`FuzzSessionFingerprint` 新增，各跑 30 秒 ≈750 万次迭代确认干净）。详见问题 8 的完整根因分析。

**P1-2 · 修正 CLAUDE.md 的 4 处偏差 — ✅ 已修复**
删掉 "no CI config in-repo"（改成简述两个 workflow）；模块表补 `internal/i18n`；补 `report/pricing.go` 与 `pricing.yaml`；"Byte-faithful passthrough"不变量补上图片降采样这第三类例外。**实际改动**：四处均按建议修复；修复过程中发现草稿曾错误写成"pricing.yaml gitignored by default"，核实（`git check-ignore`/`git ls-files`）后订正为"tracked in git"——写文档陈述前也要核实，不能凭印象。

**P1-3 · 扩展 `archtest` 覆盖面 — ✅ 已修复**
- 行数预算补上 `report/detail.go`(1059)、`report/session.go`(978)、`story/journey.go`(766)；`aggregate.go` 已用到预算的 99.9%，需要先拆再定新预算。
- import 边界补上 `core` 无内部依赖、`fmtutil` 零依赖、`adapter` 不依赖 router/server、`router` 不依赖 server。
- **实际改动**：三个文件按建议加了预算（各约 15% headroom：1150/1100/850），新增 `TestArchitecture_ZeroInternalDepPackages` 覆盖 core/fmtutil 的零依赖承诺，`forbiddenImports` 加了 adapter/router 两条边界。`aggregate.go` 本身的预算**未动**——按建议它需要先拆分文件再谈新预算，拆分是比这项 P1 更大的独立工作，不在本轮范围。

**P1-4 · `config.Watch` 上报 watcher 错误 — ✅ 已修复**
给 `Watch` 加一个 `onError func(error)` 参数，`cmd_start.go` 传入一个打日志的闭包。热重载静默停止工作是最难诊断的故障类型之一。**实际改动**：按建议实现；`onError` 为 nil 时行为不变（三个既有测试改传 `nil` 验证兼容）；新增一个测试确认正常操作下 `onError` 不会误触发（无法在单元测试里跨平台可靠地人为触发真实 fsnotify 错误，这点在测试注释里说明了）。

**P1-5 · 给 MiniMax 响应修复加配置开关 — ⏸ 评估后判断本轮不做，理由见下**

读代码后发现实际成本比报告原文预期的高：`newRespStream`（`response.go`）当前完全不感知 provider/endpoint 身份，要做成可配置开关需要新增配置 schema（`Provider`/`EndpointGroup` 加字段 + `KnownFields` 校验 + 合法值校验）、`core.Endpoint` 新增字段并考虑 `Freeze()`、`BuildSnapshot` 计算生效值、一路传到 `newRespStream` 的构造签名——这是一条贯穿 `config`→`core`→`router` 三层的改动，外加对应测试（默认全开保持兼容、单项关闭生效、跨 endpoint-group 独立生效）和文档同步。这个量级已经不是"给 P1 打个勾"能覆盖的，仓促做容易把配置 schema 设计得不够周全，之后还要再改一遍。**建议**：作为独立任务排期，不在本轮 P1 批处理里做。

**P1-6 · 探针请求纳入审计 — ⏸ 评估后判断本轮不做，理由见下**

读代码后发现两个比报告原文预期更大的成本：(1) `Router` 结构体目前不持有 `audit.Logger`（审计日志由 `server` 持有，逐请求传 `*audit.Record` 进 `Serve`）——`runProbe` 在 `router` 包内的后台 goroutine 里跑，要写审计就要给 `Router` 加一个可选的 `Audit *audit.Logger` 字段并在 `cmd_start.go` 里接好。(2) `audit.Record` 的 schema 里 `Client Exchange` 不是指针、`Request Message` 也不是 `omitempty`——探针没有真实客户端请求，会被迫塞一个假的/空的 `Client.Request`，而 CLAUDE.md 明确要求"changing the audit record structure requires updating `internal/report` and its tests in the same change"，还需要决定 `vmr report` 的可靠性/延迟/成交量统计是否要把探针记录排除在外（不排除会让"这个端点错误率 50%"之类的统计被探针流量污染，`ReplayOf` 这个先例字段目前 `report` 端完全没有特殊处理，照抄这个先例大概率不是探针记录真正需要的行为）。这些是需要设计取舍的问题，不是照抄 `ReplayOf` 模式就能糊过去的。**建议**：作为独立任务排期，需要先决定 report 端如何呈现探针记录，再动 schema。

**P1-7 · CI 补齐 — ✅ 已修复（`macos-latest`/`gofmt -l`/`shellcheck`），`staticcheck` 评估后未采纳**
加 `macos-latest` 到 matrix（主力开发平台，且有 launchd 专属分支）；加 `gofmt -l` 检查；加 `shellcheck`（685 行 shell 目前零守护）；考虑 `staticcheck`。**实际改动**：`ci.yml` 拆成三个 job——`test`（矩阵 ubuntu+macos）、`gofmt`、`shellcheck`（用 ubuntu-latest runner 自带的 shellcheck，未加第三方 action）。`gofmt -l` 本地已验证跑通（顺带发现并修复了本轮新增的两个 fuzz 测试文件的格式问题）。`shellcheck` **未能本地验证**——本机不在联网环境，`brew install shellcheck` 拉取失败，和最初这份报告写作时遇到的限制一样；这一步的真实结果要等它在 GitHub Actions 上第一次跑才知道，如果对 `vmr.sh`/`vmr-loadtest.sh` 现有代码有真实发现，那是它存在的意义，需要另开任务处理，不属于这次"加检查"本身的范围。`staticcheck` **未采纳**：25,574 行生产代码上跑一个新 linter 很可能一次性冒出大量未经筛选的既有发现，本轮没有时间预算去逐条判断哪些是真问题、哪些该压制，贸然接入还可能让 CI 从下一次 push 就直接变红——比不加更糟。

### 5.3 P2 — 打磨

**2026-08-04 复核**：16 项逐一核对代码。14 项属实并已修复；2 项（#7、#15）复核后发现是**误判**——核对代码/实际输出后确认原判断不成立，未做改动（理由见各自"结果"列）。

| # | 问题 | 位置 | 建议 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | `i18n.Parse` 注释称大小写不敏感，实现是穷举 | `i18n/lang.go:43` | 改用 `strings.ToLower`，一行 | ✅ 已修复，补了混合大小写用例的回归测试 |
| 2 | `usage()` 未列 `-lang` | `cmd/vmr/main.go:65-66` | 补上 | ✅ 已修复 |
| 3 | `buildinfo` 注释称"无 release process" | `buildinfo/buildinfo.go:60` | 更新注释；考虑 release 时注入 tag | ✅ 注释已更新；**未采纳**"注入 tag"——这会引入 `-ldflags`，与该包自己的文档明确拒绝的设计原则（"no -ldflags, no generated version.go"）直接冲突，接受的代价是官方 release 包的 `vmr version` 显示 commit SHA 而非 tag |
| 4 | `/v1/models` 跨协议同名会返回重复 id | `server/server.go:284` | 按 id 去重，或把 protocol 并进 id | ✅ 已修复，按 id 去重（协议按 sorted key 顺序取第一个，结果确定性），补了跨协议同名场景的回归测试 |
| 5 | 健康过滤每端点 2-3 次加锁 | `router/router.go:77-88` | 合并成单次 `Classify(key, now)` | ✅ 已修复，新增 `health.Registry.Classify`，顺带消除了原两次独立加锁之间的状态漂移窗口 |
| 6 | `respStream.Read` 返回 `(0,nil)` | `router/response.go:231` | 注释注明"仅供 copyFlush 消费" | ✅ 已修复 |
| 7 | >128KB 错误体转发给客户端时被截断成坏 JSON | `router/router.go:404` | 至少在文档的例外清单里记一笔 | ❌ **误判**——设计文档「六条约定」第 1 条（`docs/VirtualModelRouter_Design_v4_Core.md`）已明确记录这一行为，包括"转发给客户端的字节仍是未改动的截断前缀（byte-faithful 对客户端始终成立）"，决策-取舍表也记录了 64KB→128KB 上限调整的理由。原报告这条结论未成立，未做改动 |
| 8 | `examples/sample-audit.jsonl` 无测试守护 | — | 加一个反序列化断言测试 | ✅ 已修复 |
| 9 | 多实例共享 log_dir 会互删文件 | `audit/housekeep.go` | UserGuide 加警告，或用 lockfile | ✅ 已修复（文档层）：`UserGuide.md`/`.zh.md` 加警告段落。**未采纳** lockfile 方案——已有文档警告降低了触发概率（需要显式配置重叠 log_dir），lockfile 引入跨进程协调的复杂度和新失败模式，对这个窄触发场景不成比例 |
| 10 | `Redact` 只覆盖 7 个固定 header | `audit/audit.go:316` | 考虑加可配置的额外脱敏名单 | ✅ 已修复，新增 `extra_redact_headers` 配置项，仿 `audit_retention_days`/`SetRetentionDays` 的既有模式接入 `audit`/`replay` 两处调用方 |
| 11 | `strategy` 只有 priority 一个 Dimension | `strategy/strategy.go:69` | 文档/示例注明当前唯一可选值 | ✅ 已修复 |
| 12 | `Serve` 里 `snap` 无 nil 检查 | `router/router.go:51` | 防御性 nil 检查 | ✅ 已修复，返回 503 而非 panic，补了回归测试 |
| 13 | 依赖过时（x/sys 落后 34 个小版本） | `go.mod` | `go get -u` + 启用 Dependabot | ✅ 已修复，`go get -u` 更新 3 个直接/间接依赖，新增 `.github/dependabot.yml`（gomod + github-actions，周检查） |
| 14 | `docs/` 混入 27 个市场/战略文件 | `docs/` | 移到 `marketing/` 或独立仓库 | ⏸ **不做**——这是仓库组织的结构性决策（是否拆分独立仓库、如何处理已有链接），不是代码缺陷，应由用户决定，不在这轮批处理范围内 |
| 15 | README 出现 `## §0 Summary` | `README.md:63` | 去掉编号 | ❌ **误判**——这不是文档里的编号交叉引用，是 `vmr-report.md` **真实生成输出**的字面章节标题（`internal/i18n/report_doc.go` 的 `SummaryTitle: "§0 Summary"`，`§0`-`§8`/`§6.7` 是这份报表刻意设计的自有目录编号，参见 `aggregate.go` 包注释"报告按九个编号章节组织"）。README 里的示例如实反映了工具的真实输出；去掉编号反而会让示例与实际产出不符。原报告把这个与 CLAUDE.md"不要用编号做跨文档引用"的规则混为一谈——那条规则针对的是注释里指向*其他*文档章节的脆弱指针，不是一份文档给*自己*的章节编目录 |
| 16 | `chatmsg_compat.go` 43 行委托层 | `report/` | 排期消除，或注明永久保留 | ✅ 已修复，选择"消除"而非"注明永久保留"：核实发现真实调用点有 68 处（跨 9 个文件），比报告原估计的量级大，但全部是机械的标识符改名（`chatMessage`→`chatmsg.Message` 等），逐文件替换 + 编译器兜底验证后删除委托层文件，`go build`/`go vet`/`go test -race` 全绿 |

### 5.4 P3 — 产品方向建议（非缺陷）

**P3-1 · 提供关闭 body 记录的选项**
`audit_bodies: false`（保留元数据：时长、token、端点、错误分类、norm trail），丢弃请求/响应正文。`vmr report` 的可靠性、延迟、成本、端点价值等章节仍可工作，只有详单和 `vmr story` 失效。这会直接打开合规环境和共享机器这两个当前被排除的场景。

**P3-2 · 在文档里正视分析层的分量**
CLAUDE.md 把 report/story 描述成 "Beyond routing, two offline tools"，但它们的生产代码量已超过路由核心。建议改成对等的两部分描述——这也更准确地传达了这个项目的差异化价值所在。

**P3-3 · 存量编号引用的处置决策 — ✅ 已修复**
建议是至少做一次**分级处置**：立刻删除 `Appendix *`/`T<n>.<n>`/`F<n>`（F9/F10 除外，两者在 Analytics.md 里仍是有名有实的稳定标签）；逐步替换裸 `§x.y`；保留已写全文件名的 ~28 处（`docs/..._Core.md §6.4` 形式）。

**实际改动**：全项目按此三档逐一核对并处理，拆成 6 组并行核实（每组约 50 处候选行），逐行判断"这是指向 design doc 的失效引用"还是"报告/叙事自身 `§0`-`§8` 的稳定自指编号"（后者不属于本条问题，一律不动）。结果：
- `Appendix *`/`T<n>.<n>`/裸 `F<n>`（F9/F10 除外）：全部清除，改写成不带编号的事实性描述，逐处核对全仓库确认零残留。
- 派生发现（不在原分类里，一并处理）：若干处 `design doc D<n>`（D1/D3/D4）同样在当前 docs/ 里查无此编号，按同一原则清除。
- 裸 `design doc §x.y`：按"能直接删则删、删了会丢信息则换成一句话描述"处理，不新引入编号——比报告原建议的"带文件名"更进一步，直接对齐 CLAUDE.md 收紧后的"只引用文档名字和章节名称"原则。
- 已带文件名的 ~28 处：按建议保留不动，留作后续可选清理（见 `docs/OUTSTANDING_ISSUES_opus-5.md`）。
- 顺带订正 2 处报告自身编号的内部漂移（`section_compaction.go`/`aggregate.go` 的 compaction 小节注释误写 `§6.4`，实际渲染标题是 `§6.7`）和 3 处引用了更早"V2 draft"自有编号方案的过时注释（`rows.go` 的 `SessionRow`/`ToolShapeRow`/`RequestRow`，已改标现行的 `§6`/`§7`/`§8`）——这两类都是"自指编号也会漂"的活例子，而不是本条要解决的"指向 design doc"问题，顺手一并修正。
- `go build`/`go vet`/`go test ./...` 全绿。

---

## 6. 结论

vmr 是一个**核心判断清晰、执行高度一致、工程纪律罕见地好**的项目。

它的价值不在于功能数量，而在于把"永不翻译协议"这一个洞察贯彻到了每一个决策：字节拼接的 model 重写、拒绝默认 header、拒绝跟随重定向、拒绝读代理环境变量、响应归一化的每条规则都配严格守卫且存疑就不改。这种一致性让整个系统的行为可以从一条原则推导出来，而不需要记忆一堆特例。

工程实践上有三处值得被其他项目借鉴：`archtest` 把架构约定变成测试失败、给启发式规则配失效自检信号、注释记录"被替换掉的旧方案错在哪"。

本次评审找出的问题里，**真正需要行动的是三件事**：

1. **注释引用体系已系统性失效**（约 520 处编号引用过半悬空，另有 11 处指向已删除的 gitignore 文件，其中一处印在用户可见的 CLI 帮助里）。这不影响运行，但持续侵蚀这个代码库最有价值的资产——那些高信息密度的"为什么"注释。而且它的累积速度与开发速度成正比。

2. **`config.Check()` 在自动路径上缺席**。这是唯一一个能造成真实生产故障的发现：热重载会接受一份 api_key 为空的配置并静默把全部流量打成 401。

3. **资源上界未闭合**。三个缓冲常量各自合理，乘起来是 104MB/请求，而并发默认无限。没有文档、没有断言、没有测试把它们关联起来看过。

其余发现都属于打磨。以这个代码库的整体水准，上述三项处理完之后，它在同类开源项目里的工程质量会处在很靠前的位置。

---

*本报告由独立评审生成，未修改仓库中任何已有文件。全部结论基于 commit `c2c6df7` 的实际代码，可验证的部分均已实测。*
