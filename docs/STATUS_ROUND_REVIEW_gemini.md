<!-- Ver 2026-08-23, by review (independent of the three design docs under review) -->

# VMR Status 三件套（端点迁移 / Status Report 重构 / HTML 看板）全量 Review 报告

## 1. Debrief（任务定义）

### 1.1 被审查对象

工作区未提交改动对应的三份设计文档及其落地代码：

| 文档 | 声称做的事 |
| :--- | :--- |
| `docs/STATUS_ENDPOINT_MIGRATION_gemini.md` | `/admin/status` → `/status`；移除 loopback IP 限制；接入 `api_keys` 鉴权（Bearer / x-api-key）；CLI 自动读 Key + `-key` 参数；不保留旧路径别名（404） |
| `docs/STATUS_REPORT_DESIGN_gemini.md` | `/status` JSON 全景重构：`instance.config`/`instance.concurrency` 内聚、`system`（Go 运行时内存/goroutines/磁盘余量）、`traffic`（固定原子计数器 + 5 分量 token）、`audit`/`image_cache` 无状态现场统计、双格式字段、`current_time` |
| `docs/STATUS_HTML_DASHBOARD_DESIGN_gemini.md` | 新增 `/status.html`：CSR 自包含单页看板、零外部 CDN 依赖、`//go:embed` 进二进制、弹窗鉴权 + localStorage、自动轮询/倒计时 |

### 1.2 Review 任务

1. **方案本身**：对三份文档里**每一项已做决策**用第一性原理重审——是否足够简化、是否引入多余复杂度、有无优化/疏漏。文档里的定论不享有豁免权，可被推翻，但要有据。
2. **落地核实**：逐个功能点对照源码，确认"声称的已实现"是否真实存在、是否正确；文档的 claim（测试全绿、零锁、零依赖等）是否有事实依据。
3. **输出**：问题汇总按**用户价值 / 严重程度 / ROI / 风险**多维评分，分梯队；低风险高 ROI 的小改动在 review 过程中直接处理并标注"已处理"，其余标注"待后续"。

### 1.3 Review 边界与原则

- 不重复审查三份文档以外的既有代码（仅当被新改动牵连时顺带核实）。
- 与项目既有原则对齐：KISS/YAGNI、字节级透传、两半区契约、`archtest` 守卫、"先观测后决策"。
- 本次 review 的独立立场：不采信文档自评（含 `_tmp/audit_review_gemini.md` 那份"全 Clean"的自我复查），全部结论以代码与实测为准。

---

## 2. Action Plan（执行计划）

```
[Phase 1] 基线核实
  1.1 读全部未提交 diff 与新增文件
  1.2 go build / go vet / gofmt / 全量测试 + -race + archtest
  1.3 交叉编译验证（darwin/linux/windows，核实文档 claim）

[Phase 2] 端点迁移 Review
  2.1 安全模型重审（loopback 移除 vs api_keys；config.Check 兜底）
  2.2 CLI 行为重审（-addr/-key/config 交互、-brief 兼容）
  2.3 Breaking change（不保留别名）合理性

[Phase 3] Status JSON 重构 Review
  3.1 各新增块字段级价值审计
  3.2 telemetry 埋点实现核实（零锁 claim、by_status 语义）
  3.3 现场目录求和 / Statfs 跨平台
  3.4 双格式字段与 JSON 一致性

[Phase 4] HTML 看板 Review
  4.1 CSR / 零依赖 claim 核实
  4.2 安全面（innerHTML/XSS、key 存储、免密外壳）
  4.3 与 /status 字段耦合一致性（此处抓到了 pct 语义 bug）

[Phase 5] 横切审查
  5.1 文档同步完整性（中英、CHANGELOG、设计文档、KNOWN_ISSUES 登记义务）
  5.2 架构守卫 / 行数预算 / 包边界
  5.3 版本错配（new CLI ↔ old server）兼容

[Phase 6] 收尾
  6.1 低风险高 ROI 项直接修复 + 测试
  6.2 全量回归
  6.3 本报告（评分 + 梯队 + 状态标注）
```

**计划偏差**：Phase 2/3 实际按"文档逐条决策 ↔ 代码逐处核实"交织进行（决策与落地无法分割审）；新增了 5.3（版本错配）与"文档登记义务"检查——这是文档未提及、但项目 CLAUDE.md 明确要求的横切项。

---

## 3. 执行过程记录

### 3.1 Phase 1 基线核实

- 40 个文件改动（`git status`），其中 12 个新增（`cmd_status_render.go`、`telemetry.go`/`telemetry_test.go`、`disk_unix.go`/`disk_windows.go`、`status_page.go`/`status_page_test.go`、三份设计文档）。
- **注意**：`internal/server/status.html` 已在 HEAD（commit `bbdbd47`，851 行），本次改动只是 +24 行 footer/GitHub 链接——即看板主体是**提前单独提交**的，工作区只留了嵌入代码与文档。设计文档"Phase 1 创建 status.html"的描述与 git 历史不完全一致（无实质影响，记录备查）。
- `go build ./...`、`go vet ./...`、`gofmt -l`（零输出）、`go test -count=1 ./...`、`go test -race`（server/router/cmd 三包）、`archtest`：**全部通过**。文档"38 个包 100% PASS"与"archtest 全绿"的 claim **属实**。
- `GOOS=windows`/`GOOS=linux` 交叉编译 `internal/server`：通过。文档"跨平台编译"claim **属实**。

### 3.2 Phase 2 端点迁移 Review

**结论：方案成立，未发现需要推翻的决策。**

| 决策 | 重审结论 |
| :--- | :--- |
| 移除 loopback-only，改由 `listen` + `api_keys` 解耦 | **成立**。旧模型里"0.0.0.0 绑定 + loopback 拦截"的组合自相矛盾（用户明示要局域网可达却永远 403）。新模型把"谁能连上"（socket 绑定）与"谁能看"（api_keys）分开，`config.Check()` 对"非 loopback + 无 key"给出 warning 兜底（`vmr check` 与 `/status` 的 `issues` 双通道），已实测（`TestStatusIssuesBlock`）。**补充登记**：`api_keys` 同时是聊天客户端的管理凭证——任何持 key 客户端都能读全部端点/provider/quota/配置路径。对单人/小团队这是正确简化；若未来出现多租户需求再加 `admin_keys`。已登记 KNOWN_ISSUES §2.3。 |
| 不保留 `/admin/status` 别名 | **成立**（KISS，无历史包袱；`vmr.sh ps`、CLI、文档全量同步升级）。CHANGELOG 已标 Breaking。 |
| 仅 Header 鉴权、不开放 URL Query 参数 | **成立**（防泄漏；完全复用 `s.auth`，零重复代码——代码核实属实）。 |
| CLI 自动读 `api_keys[0]` | **有改进空间**：`-addr` 显式指向别的地址时，会静默把本地 config 的 key 发给那个目标。意图是让 `vmr.sh ps` 免手工传 key，代价是"本地 key 会发给任何显式指定的地址"。经权衡（目标地址是使用者自己敲的、只发 key 不进 URL/日志），保留现状并**登记为刻意取舍**（KNOWN_ISSUES §2.3），不做代码改动。 |

**发现的问题**：

- **[已处理] CHANGELOG 内部自相矛盾**：同一 `[Unreleased]` 里，`GET /health` 条目仍写"`/admin/status` stays loopback-only"（该端点本批次已迁移、已解除限制）。已改为"`/status` answers the operational questions (auth-gated by `api_keys`)"。
- **[已处理] CHANGELOG 缺 status payload 扩充条目**：本次对用户可见的最大变更（JSON 结构重排 + system/traffic/audit 新块）没有 CHANGELOG 记录，只有端点迁移一行。已补独立 Breaking 条目（含结构变更明细与滚动升级提示）。
- **[留待] 版本错配硬失败**：见 3.5.3（已定案：不做兼容、直接报错，登记为 §2.2 取舍）。

### 3.3 Phase 3 Status JSON 重构 Review

**结论：六项技术决策全部成立，实现与文档一致。**逐项核实：

- `instance.config` 内聚、`concurrency` 归位、`time`→`current_time`：代码与文档一致；`current_time` 走 `router.WriteJSON` 的 RFC3339 带时区偏移序列化，符合"时区单一权威"原则。
- `system` 纯标准库：`runtime.ReadMemStats`（`heap_alloc`/`sys`）与 `runtime.NumGoroutine` 属实，零平台胶水。**`system.disk.free_space`** 用 `syscall.Statfs`（`Bavail*Bsize`）——unix 正确；**Windows 是桩返回 0**（看板显示 "0 B"）。已登记 KNOWN_ISSUES §2.1（Windows 非目标平台，不写平台胶水）。
- `traffic` 固定原子计数器：**"零锁、零热路径开销" claim 属实**——全部 `atomic.Uint64`，无 map 分配，埋点都在请求终态。`Snapshot()` 每次构建 2 个小 map（by_protocol/by_status），只在 `/status` 读取时发生，不在热路径。
- `audit`/`image_cache` 无状态 `os.ReadDir` 求和：属实，目录扁平、文件数几十到几百，误差可控；错误时静默返回 0（可接受，展示性数据）。
- 双格式字段（`_bytes`+可读串）：服务端冗余但便宜；CLI 与看板各自消费不同形态，保留合理。
- `fmtutil.FmtBytes` 补 GB/TB、`FmtDuration`：有单测，正确。

**发现的问题**：

- **[已处理] `traffic.by_status` 三处路径漏计**（文档声称"按结果状态分类"，但分类不完整）：
  1. `Serve` 在 `snap == nil` 时写 503 不记 outcome；
  2. 模型"协议错配"404 分支不记 outcome（只记了普通 not-found 分支）；
  3. `tryOne` 中客户端在等待上游响应头时断开（`r.Context().Err() != nil`，done=true）不记 outcome——`canceled` 是文档定义的第一类状态，这条最不该漏。
  **已修复**（`internal/router/router.go` 三处补 `RecordOutcome`），并新增 `TestTelemetry_ServeOutcomeCoverage` 锁定 not-found / wrong-protocol / all-failed / 未初始化四条终态路径各记一次。
- **[留待，登记] `by_status` 语义边界**：按请求而非 attempt 计数、且 `ok+canceled+error ≤ total`（401/413/坏 JSON 计入 error；failover 中间 attempt 不计）。这是展示口径不是账本，审计日志才是精确账本。已登记 KNOWN_ISSUES §2.1。
- **[留待] `traffic.tokens.total` 嵌套冗余**（`tokens:{total:{...}}`）——schema 小瑕疵，改造成本与收益不成比例，不动。
- **[留待，登记 1.47] `auditBlock` 在 server 侧重建日志文件名**（`vmr-audit-<date>.jsonl`），与 `internal/audit` 的命名知识重复（两份都用本地时区、目前一致），audit 改名会静默归零。经评审确认留待后续 `internal/audit` 重构时合并（导出 `TodayPath()`/大小访问器），不单独立项。
- **[已处理] `image_cache.enabled` 语义修正**：原实现只查全局 `ImageDownscaleMaxPx > 0`，"全局关 + 某模型显式开"时报告 disabled（缓存实际在跑）——与 pct bug 同类的"看起来正确的错误"。已改为"全局开启 或 任一模型有 >0 显式覆盖"（`internal/server/admin.go` 只读遍历 snapshot，零风险），并新增 `TestStatus_ImageCacheEnabled_Semantics` 锁定语义。per-model 配置细节不进入 /status JSON（那是 `vmr check` 的职责），`enabled` 只是运行态 yes/no。
- **[留待] 版本错配硬失败**：见 3.5.3（已定案：不做兼容、直接报错，登记为 §2.2 取舍）。

### 3.4 Phase 4 HTML 看板 Review

**结论：CSR + 零依赖的架构决策成立。**核实结果：

- 零外部资源依赖 claim **属实**：页面内联 CSS/JS，无任何外部资源 URL；footer 的 GitHub 链接是导航 href 不是资源，断网不影响加载。
- `//go:embed` 嵌入、`Cache-Control: no-cache`、免鉴权静态外壳（不含数据）——属实，`TestStatusPage_ServesHTML` 覆盖。
- 鉴权闭环（401 → 弹窗 → localStorage → 重试；清 key 切换）——属实。轮询/倒计时/冷却徽章——属实。

**发现的问题（本 phase 问题密度最高，且全部被 Gemini 自评"全 Clean"漏掉）**：

- **[已处理] quota 进度条 pct 100× bug（严重度最高）**：服务端 `pct` 已是百分数（`Used/Amount*100`，如 48.2），看板却 `Math.round(q.pct * 100)` → 4820，进度条恒满。**quota 是路由半区的旗舰功能，看板的 quota 展示在配置了 quota 的实例上完全失真**。已修复（`internal/server/status.html` 改为 `Math.round(q.pct)`），并在 `quota_status_test.go` 增加常驻断言锁定"服务端 pct 就是百分数"这一契约（防下一个消费方再乘 100）。
- **[已处理] innerHTML 未转义（XSS 防御面）**：`renderModels`/`renderQuota`/banner 把 provider/endpoint/model 名、YAML 错误文本直接插进 innerHTML。注入源全是 operator 配置内容（攻击面≈配置所有者攻击自己），但看板可达性与 `/status` 相同，属于不值得冒险的防御缺口。已加 `esc()`（&<>"' 全转义）并应用到全部插值点。
- **[已处理] fetch 失败静默**：文档声称"showErrorBanner"，实现只有 `console.error`——服务端挂掉时看板静默展示旧快照，像活着一样。已加醒目错误横幅（"Cannot reach vmr … showing the last successful snapshot"），成功后由 `renderDashboard` 自然清掉。
- **[已处理] Key 脱敏展示缺失**：文档声称"顶部展示脱敏尾缀（sk-***abcd）"，实现只有"🔑 Key"。已实现 `updateAuthButton()`：有 key 显示 `🔑 ***后4位`，无 key 显示 `🔑 Key`，保存/清除即时刷新。
- **[留待] 文档其他小偏差**：刷新倒计时是数字非环形（无实质影响）；看板不展示 `estimated_pct`；错误 key 弹窗文案与文档流程基本一致。均归 Tier 3。
- **[观察] 看板是 /status 的第三个消费方**（CLI 结构体、看板 JS、离线工具），pct bug 正是"多消费方各自反推语义"漂移的实例——已用服务端契约断言止血，长期根治方向是让 /status 文档把每个字段的"原始单位"写明（已体现在 CHANGELOG 与 KNOWN_ISSUES）。

### 3.5 Phase 5 横切审查

#### 3.5.1 文档同步完整性

- README / README.zh / UserGuide / UserGuide.zh / config.example(.zh) / CLAUDE.md / Core / Quota / Strategy 设计文档：`/admin/status` → `/status` 引用全部同步，中英一致（grep 全仓核对，无残留引用）。
- **[已处理] Core 设计文档漂移**：§4.3 端点表 `/status` 行仍是旧内容（`reload`/`sticky.entries` 顶层描述），`instance` 块字段清单仍写 `config_stale`/`config_mtime`（新 schema 已改为 `instance.config.{path,mtime,stale,reload,issues}`）。已更新行文与字段清单，并补充 `/status.html` 端点行。
- **[已处理] KNOWN_ISSUES 登记义务**：按项目约定（"tradeoff argued only in a source comment is not tracked"），本轮决策未登记。已补：§2.1（by_status/by_protocol 计数口径、Windows disk 桩）、§2.2（版本必须匹配、直接报错）、§2.3（共享 api_keys 安全模型、`-addr` 回退 key、localStorage + esc 姿态）、§1.47（audit 文件名重复），并同步 §0 计数（16→17）与 §4 表格。

#### 3.5.2 架构守卫

- `internal/archtest` 全绿；`cmd_status_render.go` 拆分后行数合规；`admin.go` 扩张后仍在线内。未触碰包边界（`server`↔`router` 通过 `rt.Telemetry` 字段访问，`core`/`chatmsg` 无新增耦合；`usage.Fresh()` 已存在于 `chatmsg`，本次零新增）。

#### 3.5.3 版本错配（new CLI ↔ old server）

- **现状**：`statusResponse` 强类型；新 CLI 查旧 server（`instance.config` 是字符串）→ `json.Unmarshal` 类型错误整体失败；旧 CLI 查新 server 同理。`vmr.sh ps` 因 `|| true` 退化为标注行不受影响。
- **评审结论（定案）**：**不做兼容处理，直接报错是正确行为**。单二进制、可随时重启的项目里 CLI 与 Server 理应始终同版本，版本不一致说明升级没走完，报错正是暴露它。`json.RawMessage` 双形状兼容层只覆盖一个滚动升级窗口，却会永久留在代码里，违反 KISS。原 KNOWN_ISSUES 1.46（待定）已移入 §2.2 作为刻意取舍登记；错误信息已包装为明确提示（"server and client vmr versions differ - restart the server with the matching binary"，纯诊断，非兼容机制）。字段*新增*（`serving` 的 `*bool` 兜底）与形状*变更*是两回事：前者无法也不需报错，后者自然报错——`*bool` 保留。

---

## 4. 问题汇总：评分、梯队与状态

评分维度说明：**用户价值**（修好对使用者/运维的实际收益）、**严重程度**（现状有多糟）、**ROI**（价值 ÷ (成本+风险)）、**风险**（改动本身的破坏面）。状态：✅ 已在本轮 review 中处理；⏳ 留待后续。

### 4.1 第一梯队 —— 建议立即处理（本次已全部就地处理）

| # | 问题 | 用户价值 | 严重度 | ROI | 风险 | 状态 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| T1-1 | 看板 quota 进度条 pct ×100（quota 是旗舰功能，看板相关区域完全失真） | 高 | **高** | 高 | 极低 | ✅ `status.html` 修复 + `quota_status_test.go` 契约断言 |
| T1-2 | `traffic.by_status` 漏计三条终态路径（canceled 是最不该漏的一类） | 中 | 中 | 高 | 极低 | ✅ `router.go` 三处补埋点 + `TestTelemetry_ServeOutcomeCoverage` |

### 4.2 第二梯队 —— 建议尽快处理（成本可控，非当务之急）

| # | 问题 | 用户价值 | 严重度 | ROI | 风险 | 状态 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| T2-1 | 版本错配 JSON 硬失败 | - | - | - | - | ✅ 已定案：不做兼容、直接报错（KNOWN_ISSUES §2.2 登记为刻意取舍；错误信息已包装为明确版本提示） |
| T2-2 | `image_cache.enabled` 漏模型级覆盖 | 低 | 低 | 中 | 低 | ✅ 已修：聚合语义改为"全局开 或 任一模型显式开"（`admin.go` + `TestStatus_ImageCacheEnabled_Semantics`） |
| T2-3 | `audit` 文件名知识在 server 侧重复 | 低 | 低 | 中 | 低 | ⏳ 已定案：留待 `internal/audit` 重构合并（KNOWN_ISSUES 1.47 登记） |

### 4.3 第三梯队 —— 可改可不改（含已明确"不做"的取舍）

| # | 问题 | 用户价值 | 严重度 | ROI | 风险 | 状态 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| T3-1 | 看板不展示 `estimated_pct` / 刷新倒计时非环形等文档小偏差 | 低 | 低 | 低 | 极低 | ⏳ |
| T3-2 | `tokens.total` 嵌套冗余 | 低 | 低 | 低 | 低 | ⏳ |
| T3-3 | CLI 不渲染新块（system/traffic/audit）——设计文档明说"后续迭代"，且看板已覆盖 | 低 | 低 | 低 | 低 | ⏳ 维持原判 |
| T3-4 | Windows `disk.free_space` 桩返回 0（KNOWN_ISSUES §2.1 登记为取舍） | 低 | 低 | 低 | 低 | ⏳ 不做（非目标平台） |
| T3-5 | `-addr` 回退把本地 key 发给任意目标（KNOWN_ISSUES §2.3 登记为取舍） | 低 | 低 | 低 | 低 | ⏳ 不做（便利性优先，使用者自己敲的目标地址） |
| T3-6 | 看板 key 存 localStorage（KNOWN_ISSUES §2.3 登记；已用 esc() 收窄残余面） | 低 | 低 | 低 | 低 | ⏳ 不做（零 URL 暴露收益更大） |
| T3-7 | `by_status`/`by_protocol` 含未鉴权请求的口径（KNOWN_ISSUES §2.1 登记） | 低 | 低 | 低 | 低 | ⏳ 不做（展示口径，账本在审计日志） |

### 4.4 梯队划分理由

- **第一梯队 = "功能正确性/核心语义"级缺陷**，两者都是"现状下必然出错、修复近乎零成本"：T1-1 让旗舰功能在看板里失真，T1-2 让新加的遥测语义不完整。都不涉及方案再探讨，直接修。
- **第二梯队 = 已定案的三项**：T2-1 经评审确认"版本必须匹配、直接报错"是最简正确模型，兼容层收益（覆盖一个滚动升级窗口）不抵永久复杂度，故不做、登记为取舍；T2-2 语义修正零风险且消除了一个"看起来正确的错误"字段，直接修掉；T2-3 是 3 行知识重复，留待 audit 重构时顺带合并，登记待办。
- **第三梯队 = 明确取舍或收益过低**：T3-3~T3-7 已论证为刻意取舍（KNOWN_ISSUES §2 登记，含理由），不修是为了 KISS；T3-1/T3-2 是纯收益过低的打磨项。**这些"不做"本身就是结论**，防止未来被重复提出。

---

## 5. 对三份设计文档本身的总体评价

1. **方案质量整体高**：三个文档的决策几乎全部经得起第一性原理重审——"loopback 限制与 0.0.0.0 绑定自相矛盾"（端点迁移）、"固定原子计数器 vs 动态 Map"（traffic）、"现场 ReadDir vs 写路径累加器"（audit/imgprep）、"CSR + embed 零依赖"（看板）都是正确且与项目哲学一致的简化。**没有需要推翻的决策。**
2. **主要失分在"验证环节"**：三份文档都自称"100% 通过/零问题"，`_tmp/audit_review_gemini.md` 的逐文件复查更是全部"Clean"——但 pct ×100、by_status 漏计、Core 文档漂移、CHANGELOG 自相矛盾全在它的视野内。**文档的"已完成/已验证"claim 只能当过程陈述，不能当正确性证据**；这也是本项目"验收对象要是纪律是否还成立"那条教训（KNOWN_ISSUES §4.2）的又一次印证。
3. **数据扩充的克制值得肯定**：文档明确砍掉了动态端点级 token 矩阵、实时 QPS 窗口、会话指纹遍历明细等"有价值但复杂度不成比例"的候选，与用户"为一点点收益引入高复杂度不可取"的原则一致。

## 6. 本轮 review 直接处理的改动清单

| 文件 | 改动 |
| :--- | :--- |
| `internal/server/status.html` | quota pct ×100 修复；`esc()` 全量转义；fetch 失败错误横幅；Key 脱敏按钮 |
| `internal/router/router.go` | 三处补 `RecordOutcome`（snap==nil / 协议错配 404 / 客户端取消） |
| `internal/router/telemetry_test.go` | 新增 `TestTelemetry_ServeOutcomeCoverage` |
| `internal/server/quota_status_test.go` | 锁定服务端 `pct` 为百分数语义的常驻断言 |
| `internal/server/admin.go` + `instance_test.go` | `image_cache.enabled` 聚合语义修正（全局或任一模型显式开）+ `TestStatus_ImageCacheEnabled_Semantics` |
| `cmd/vmr/cmd_status.go` | 解析失败错误包装（明确提示 server/client 版本不一致需重启，纯诊断非兼容机制） |
| `CHANGELOG.md` | 修复 `/health` 条目矛盾；补 status payload 扩充 + 滚动升级提示 |
| `docs/VirtualModelRouter_Design_v4_Core.md` | 修正 `/status` 端点行与 instance 块字段清单（消除旧扁平结构漂移） |
| `docs/KNOWN_ISSUES_sonnet-5.md` | 登记 1.47（audit 文件名重复）+ §2.1/§2.2/§2.3 取舍（含版本匹配政策）+ §0/§4 计数同步 |

**最终验证**：`go build ./...`、`go vet`、`gofmt`、`go test -count=1 ./...`、`go test -race`（router/server/cmd）、`archtest`、darwin/linux/windows 交叉编译——全部通过。
