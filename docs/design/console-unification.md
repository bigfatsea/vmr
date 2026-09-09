<!-- Ver 2026-09-14, by pi -->

# VMR 控制台统一设计方案（Console Unification）

**状态：草案 v3（已吸收两轮评审意见），待终审。** 本篇只做方案设计与静态 Demo
（`docs/design/demo/`），**不改动任何现有代码**——`internal/server/*.html`、各 JSON API、
路由挂载全部维持现状。实施轮次见 §10；仍开放的决策点在 §11。

配套 Demo（双击 `index.html` 即可打开，纯静态、纯 Mock、`file://` 直开、无网络请求）：

- `docs/design/demo/index.html` — Demo 入口
- `docs/design/demo/overview.html` — 合并后的 Overview 页（原 Status+Stats 平铺单页，含模拟交互）
- `docs/design/demo/log.html` — Log 页（满宽终端，模拟流式输出与断线重连）
- `docs/design/demo/help.html` — Help 页（Agent 配置指南，手风琴 + 复制）

## 1. 现状盘点：四个页面、四套实现

| | status.html | stats.html | log.html | help.html |
| --- | --- | --- | --- | --- |
| 容器宽度 | 1280px | **1400px** | 满宽 | 1280px |
| 正文字号 | 14px | **13px** | 14px | 14px（行高 1.6，与他页 1.5 不一致） |
| `:root` 令牌 | 基础 + 紫/橙/粉 | 基础 + 青/紫（**自定义子集**） | 基础子集 | 基础子集 |
| Key 逻辑 | `Auth` 对象 + modal + 401 重试 | **裸 input，无 401 流程、无 modal** | 自成一套 modal + `promptForKey` + 401 | 探测式（先无 Key 请求，失败再要） |
| 页内导航 | 有 | 有 | 只有 Status | 有 |
| Footer | 有（品牌 + GitHub） | 无 | 无 | 无 |
| 刷新 | 5 分钟固定 | 1s 轮询 | 流式 | 无 |

问题不在"缺样式"，而在**缺唯一权威**：同一套视觉语言被抄写了四份且已漂移，每加一个
页面就要再抄一遍。Key 逻辑的三种写法还带来行为不一致——同一台机器的四个页面是三种
鉴权体验。

## 2. 目标与原则

1. **一套令牌，一处权威**：颜色、字号、间距、圆角、组件类只有一份定义；页面不许再出现裸色值。
2. **一副骨架**：Header（品牌 + 页内导航 + 告警铃 + 刷新倒计时 + 版本/pid + uptime + 连接
   状态 + Key 按钮）与 Footer 所有页面同构；宽度差异只作用于内容区。
3. **一个鉴权入口**：四页共享 `VMRAuth` 单例与同一只 modal，401 → 弹窗 → 存键 → 重试是唯一流程。
4. **Status 与 Stats 合并为一个 Overview 页，单页平铺、无 Tab**：区块自上而下按
   "健康总览 → 预算 → 拓扑 → 实时 → 性能与用量"排列，锚点可直达；整页随倒计时统一刷新。
5. **排版纪律**：标题与导航**一律不用 emoji**，层级靠字重/字号/颜色区分；emoji 只允许
   出现在个别紧凑图标按钮上（如 🔑），品牌标识只用 SVG logo。
6. **数字格式纪律**：K/M/G 人读转写**一律保留两位小数**（`48.20M`、`200.00K`）；整数计数
   用千分位（`12,847`）；Headroom 一律两位小数无量纲比值（§8.5）。
7. **保留的优点**：暗色 GitHub 风科技感、单文件自包含（无 CDN）、纯 CSR。

## 3. 设计令牌（唯一权威）

真实实现中只有一个来源：`internal/server/assets/console.css`（`go:embed`），见 §7 注入机制。

```css
:root {
  /* 表面 */
  --bg:#0d1117; --surface:#161b22; --surface-2:#1c2128;
  --border:#30363d; --border-soft:rgba(48,54,61,.4); --well:#090d13;
  /* 文本 */
  --text:#c9d1d9; --text-muted:#8b949e; --text-bright:#f0f6fc;
  /* 语义色（bg 变体统一 15% 透明度） */
  --accent:#58a6ff; --accent-hover:#79c0ff;
  --green:#3fb950; --yellow:#d29922; --orange:#e8813a;
  --red:#f85149; --purple:#bc8cff; --cyan:#39c5cf; --pink:#f778ba;
  /* 字体 */
  --font-sans:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;
  --font-mono:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace;
  --fs-xs:11px; --fs-sm:12px; --fs-md:13px; --fs-lg:14px; --fs-xl:16px; --fs-2xl:18px; --fs-3xl:20px;
  /* 间距与圆角 */
  --sp-1:4px; --sp-2:8px; --sp-3:12px; --sp-4:16px; --sp-5:20px; --sp-6:24px;
  --r-sm:6px; --r-md:8px; --r-pill:999px;
  /* 页宽 */
  --page-max:1280px;
}
```

组件类（console.css 提供，页面只许用类）：`.card` `.card-title` `.section-head` `.btn`
（`.primary`/`.ghost`/`.sm`/`.on`）`.badge`（`.b-ok/.b-warn/.b-err/.b-info/.b-dim/.b-cyan/.b-purple`）
`.pill`（`.p-live/.p-connecting/.p-down/.p-paused`）`.input` `.table-wrap`（内嵌 `table.c`）
`.progress` `.chip` `.modal-overlay/.modal` `.vitals`（指标带）`.sysline`（系统详情单行）
`.toast` `.pri-1..6`。数字列一律 `font-variant-numeric: tabular-nums`。字号基准 13px。

## 4. 统一骨架：Header / Nav / Footer

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ [◇] VMR·Console  Overview Log Help   (⚠3) (⟳4:37) (v4.2.1·pid 48211) (up 3d 4h) (●) [🔑] │ sticky
├──────────────────────────────────────────────────────────────────────────────────────┤
│   内容区（Overview/Help: 1280px 居中；Log: 满宽终端）                                  │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Virtual Model Router (VMR) · v4.2.1                                    bigfatsea/vmr │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

- **Header**（sticky，毛玻璃底）：左=品牌（SVG logo + `VMR` + `Console` 小徽）；中=导航
  （纯文字 pill，当前页高亮）；右=告警铃、刷新倒计时、版本/pid 徽、uptime 徽、连接状态
  pill、Key 按钮。
- **告警铃**（SVG 图标 + 数量徽）：只显示 warning/error 的**数量**，点击弹出小窗列出
  具体条目（严重度徽 + 文案 + 时间）。首屏不再被静态告警横幅占据，可发现性不变。
- **刷新倒计时**（mono 数字 + ⟳ SVG）：距下次整页自动刷新的剩余时间（5:00）；**可点击**，
  点击立即刷新全部区块并归位。整页只有这一个刷新节奏（§8.3）。
- **版本/pid 徽 + uptime 徽**：pid 从内容区收进 Header 与版本同徽；uptime 是进程存活
  时长，与刷新倒计时是两个正交概念，各自独立显示。
- **Key 按钮三态**：未存键 → `🔑 Set Key`；已存键 → `🔑 ····abcd`（悬停说明存储位置）；
  点击统一打开 §6 的 modal。
- **Footer**：两栏 slim 条。Log 页的 footer 是同构**状态条**（行数 / 缓冲 / 过滤器状态）。
- 导航三页：**Overview / Log / Help**（`/stats.html` 退役映射见 §8.6）。

## 5. 宽度策略

| 页面 | 内容区 | 理由 |
| --- | --- | --- |
| Overview | `max-width:1280px` 居中 | 宽表（拓扑/Live/Perf）在 1280 下列宽充裕；超宽可读性反降 |
| Help | `max-width:1280px` 居中 | 长文阅读行宽 |
| Log | **满宽 + 100vh 终端布局** | 日志换行毁可读性；按屏高撑满、内部滚动 |

Header/Footer 通栏，内部 `.hd-inner/.ft-inner` 按所在页对齐。

## 6. 鉴权统一契约（`console.js` 的 `VMRAuth`）

四页唯一的 Key 逻辑来源（demo 内联等价副本，实施轮为 `internal/server/assets/console.js`）：

```js
VMRAuth.get() / set(k) / clear() / has()
VMRAuth.guard(doFetch)   // 包装页面的数据加载函数
```

- **存储键**：`localStorage['vmr_key']`；启动回读旧键 `vmr_status_key`（一次性迁移，用户无感）。
- **401 唯一流程**：`guard` 捕获 401 → 弹共享 modal（密码框 + 错误行）→ 保存键并自动重试
  原请求一次 → 仍 401 则错误行提示。modal 由 `console.js` 注入 DOM，页面不再手抄；
  `Esc` 关闭、打开即聚焦。
- **Key 按钮三态**（§4）；清除键立即生效，下次 401 重新弹窗。
- help 页的"先探测后要键"改为直接走 `guard`（无键探测即 401，行为等价、流程统一）。

## 7. 共享资源的单一来源与注入机制

页面保持**单文件自包含**（保住现状优点），单一来源靠组装而不是复制：

- 仓库里只有一份 `internal/server/assets/console.css` 与 `console.js`（`go:embed`）；
- 页面文件在共享块位置留注入位标记（`/*{{CONSOLE_CSS}}*/`、`/*{{CONSOLE_JS}}*/`），
  server 启动时一次性字符串替换（进程内做一次，非每请求拼接）——与
  `internal/dashboard` 的 `WriteSkeletons` 内联 `common.js` 同一个已验证先例；
- 骨架（header/footer/两只 modal）由 `console.js` 的 `mountConsole(active)` 注入 DOM，
  页面只提供内容——导航增删、告警数量、倒计时、Key 状态只改一处。

**本轮 Demo 的物理形态**：为满足"双击即开"，demo 三页内联共享块的等价副本，不改变方案。

## 8. Status + Stats 合并：Overview 单页方案

### 8.1 Status 现状块 → Overview 的映射（压缩分析）

| 现状块（status/stats） | 压缩策略 | 去处 |
| --- | --- | --- |
| Header 徽章行（version/pid/listen/osarch/uptime） | pid 收进 Header 与版本同徽；listen/osarch/go 收进悬停提示；uptime 独立徽 | Header |
| 四张大指标卡 + Storage 卡 | 合成**一条 vitals 指标带**（单卡四段）；系统明细/存储/audit 收成**单行** `.sysline`（v3 评审：无折叠、无标题） | 首屏 |
| 黄色告警 banner | 移除，并入**告警铃**弹窗 | Header |
| Quota 表 | 保留，列重构（§8.4-e） | Quota Budgets 区 |
| Models 拓扑卡组 | **改全宽表**：一行 = 虚拟模型 × 主端点，含 Headroom 列（§8.5）；Fallback 端点**单列一张表**（不挂模型列） | Virtual Models 区 |
| Connect Your Agent 卡 | **删除**（导航已有 Help；连接信息归 Help 页） | — |
| stats 并发卡 | 删除（与 vitals 并发段重复，去重） | — |
| stats in-flight 表 | 原样并入，列重构（§8.4-a），**随整页刷新** | Live Requests 区 |
| stats Requests-per-Hour | 升级为 **Requests & Tokens** 组合图（§8.4-d），置于 Performance 之后 | 图表区 |
| stats by_provider_model / by_key×2 | 原样并入，列重构（§8.4-b/c） | Performance 区 |

### 8.2 单页平铺与整页刷新

v1 曾推荐页内 Tab（理由：不同刷新节奏的数据隔离）。两轮评审后定案**单页平铺**，且
**整页统一刷新**：不存在多节奏并存的复杂度——

- Header 的倒计时（5:00）是唯一的刷新时钟，**点击立即刷新**；`document.hidden` 时暂停；
- Live 表不再有自己的 1s 轮询（v3 评审拍板：进行中请求的观测精度让位于页面安静度；
  需要亚分钟粒度时再议）；
- 各区块锚点（`#quota/#models/#live/#perf/#charts`）承担直达定位。

```
Overview 页（单页，自上而下）
├─ vitals 指标带 + sysline 系统详情单行            ← /status
├─ Quota Budgets                                   ← /status
├─ Virtual Models & Endpoint Topology              ← /status
│   └─ Fallback Endpoints（独立表）
├─ Live Requests                                   ← /stats
├─ Performance by Provider & Model（10/100 切换）  ← /stats
├─ Requests & Tokens（24h/3d/7d）                  ← /stats
└─ Usage by Upstream Key Label / by Caller         ← /stats（与图表同时间段联动）
```

### 8.4 表 Schema（v3 定稿）

**a. Live Requests**

| 列 | 说明 |
| --- | --- |
| State | queued（黄）/ running（青） |
| Seq | 进程内单调号 |
| Model | **协议前缀的虚拟模型**：`openai-responses:coding`（协议段淡色） |
| Caller | `client_key_tag` |
| Provider : Key : Model | `packycode : main : claude-sonnet-4`（分隔符淡色）；未发出为 — |
| Att | 当前 attempt 序号（failover 可见） |
| Elapsed | 到达至今 |
| First / Last Tok | 合并列：`0.81s · 3s ago`（首块延迟 · 末块距今）；流式末块 ≥10s 显示红色 stall 徽章 |
| Tok in / Tok out | **拆两列**，右对齐 tabular-nums |

（v1 的 Queue+Route 列按评审意见删除；Key Label 并入 Provider : Key : Model。）

**b. Performance by Provider & Model**

| 列 | 说明 |
| --- | --- |
| Provider : Key : Model | key_label 内嵌，**独立 Key Label 列删除** |
| Req (ok/err) | 单列合并 |
| Tok (in+cw) / cr | 输入侧两值：fresh+cache_write 与 cache_read |
| Tok out | 输出侧 |
| TTFT p50 / p90 | 合并列（nearest-rank，**last 10 / last 100 可切换**） |
| TPS p50 / p90 | 合并列：流式扣 TTFT、非流式全时长，两键不混样 |
| Tok/s p50 / p90 | **单请求四分量 token 总和 ÷ dur** |

> **Tok/s 与 TPS 是两个有意的不同口径**：TPS 只看输出 token、流式剔除首块等待（生成
> 速度）；Tok/s 是四分量总和 ÷ 整请求时长（整请求 token 吞吐，含输入摊销）。并列展示、
> 不互相替代，标题区写明口径防漂移。

**c. Usage by Upstream Key Label / by Caller**（两表同构）

| Key Label（或 Caller） | Req (ok/err) | Tok (in+cw) / cr | Tok out |

两表与 Requests & Tokens 图表**共享同一时间段选择**（§8.4-d），三处标题右侧各有一组
24h/3d/7d 按钮，任一处点击三处同步。

**d. Requests & Tokens 组合图**

- 默认**过去 24 小时**，可选 3d / 7d（7d 上限）；多日档按 3h/6h 桶聚合；与 Usage 两表联动。
- **Requests = 折线**（右轴）；**Tokens = 柱**（左轴）：in 一根（堆叠 fresh/cache_write/
  cache_read）+ out 一根，逐桶成组。
- 纯 SVG 实现（无图表库，保持零依赖纪律）。
- **Hover tooltip（自定义 DOM，非原生 title）逐项列全**：时间范围、requests、
  `tok in 总计 = fresh + cw + cr`（分项全列）、tok out——一个悬停读全桶。

**e. Quota Budgets**

| 列 | 说明 |
| --- | --- |
| Provider : Key | 账号身份（key_label 对齐 §8.4-b 的键） |
| Limit | **颜色即角色**：蓝=bucket、红=gate（Role 列删除，语义入悬停提示） |
| Models | `all models` 或具体模型名列表 |
| Amount → Used | Amount 在左（先看盘子再看吃掉多少） |
| Progress + est % | 合并列：进度条内联，条下 `est 2.10%` |
| Headroom | **两位小数无量纲比值**（§8.5）：`0.60` / `2.33`；`<1` 黄（超前消耗）、`≥1` 绿（欠用）、`0` 红（耗尽） |
| Resets | 周期重置时点 |

### 8.5 Headroom：口径与数据可得性

**口径**：Headroom 是 quota 文档 Core Algorithm 定义的无量纲比值——
`(1 - used_frac) / max(time_left_frac, ε)`，clamp 到 `[0, HeadroomCap=5]`：`=1` 严格按进度
消耗、`>1` 欠用、`<1` 超前、`=0` 耗尽；**无量纲**因此 requests 账号与 tokens 账号可直接
比较。展示为**两位小数比值**，不做绝对量转写（绝对量随周期长度含义漂移）。`/status`
的 quota 行已输出 `headroom float64`，口径与路由评分完全同源。

**端点表 Headroom 列的数据可得性**：可行，且不需要任何新的采集点——这是一次纯读侧
的 join：

- `core.Endpoint` 在 BuildSnapshot 时已携带 `Quota *QuotaSpec`（同一展开账户共享同一
  指针，quota 本就是**账户属性**而非模型属性）；
- `/status` 的构建方（server）同时持有 `rt.Quota`（`quota.Registry`），对每个端点按其
  applicable limits 取 `Registry.Used(provider, LimitKey, PeriodStart)`，经
  `quota.Headroom(UsedFrac, TimeLeftFrac)`（或组合评分的同一导出入口）得到该端点的
  有效 headroom；无 quota 配置的账户显示 `—`（unmetered）；
- 已知语义：同账户多端点的 headroom 必然相同——这是 quota 账户语义的正确呈现，不是
  缺陷；`api_keys` 展开的子账户各自有独立计数器，`Provider : Key` 键正好把它们区分开；
- 差分纪律照旧：展示值必须调 quota 包的既有导出入口读取，不得复述公式。

### 8.6 退役映射（实施轮）

| 旧 | 去 |
| --- | --- |
| `status.html` | 重构为 Overview（本方案） |
| `stats.html` 各区块 | 并入 Overview 对应区块；`/stats` **JSON API 不变**（LiveStats 设计文档的读路径契约继续有效） |
| `/stats.html` URL | 待拍板（§11）：重定向壳 → `/#live` 锚点，或直接下线 |
| Connect 卡内容 | 归 Help 页（已有 Agent 指南） |

## 9. Demo 交付说明

| 文件 | 内容 | 模拟交互 |
| --- | --- | --- |
| `overview.html` | 平铺单页：vitals+sysline / Quota / Models+Fallback / Live / Performance / Requests&Tokens 图 / Usage×2 | ① 告警铃弹窗（3 条 mock）② 刷新倒计时点击即刷（**Live 表随整页推进**：请求生命周期、failover、stall 跨刷新演化）③ 图表 hover **DOM tooltip**（requests + tok in 总计及三分项 + tok out 全列）④ 24h/3d/7d 三处联动切换（图表 + 两张 Usage 表同步重渲）⑤ Performance last 10/100 切换 ⑥ 鉴权 modal（存键/清除/演示 401）⑦ Demo 控制面板（暂停/注卡死/演示 401/重置） |
| `log.html` | 满宽终端 | Mock 流式四色行、自动滚动 + 上滚暂停 + "↓ N new" chip、Pause（⌘P）、子串过滤、~45s 模拟断线 + Retry、统一 footer 状态条 |
| `help.html` | Agent 指南 | 手风琴、片段复制（✓ 反馈）、连接检查（读共享 Key） |
| `index.html` | Demo 入口 | — |

所有页面右上 `DEMO · mock data` 缎带；Overview 右下 Demo 控制面板仅存在于 demo。
Mock 无任何网络行为，数据形状与 `/status`、`/stats` 现有 JSON 契约一致。

## 10. 实施建议（下一轮代码落地，本轮不做）

1. 落 `internal/server/assets/console.css` + `console.js`（单一来源），页面改模板 + 注入位；
   server 启动时一次性组装（对齐 `internal/dashboard` 的 `commonJSTag` 先例）。
2. `status.html` 重构为 Overview（§8 映射），`stats.html` 按 §11-1 拍板处理，
   `log.html`/`help.html` 换骨架；`admin_log_test.go` 等页面 marker 断言同步更新。
3. `/status` payload 增补：告警列表（config issues + 降级端点汇总）与端点 headroom
   （§8.5 的读侧 join）；`/stats` 的 usage/图表数据按 range 查询参数输出。
4. 鉴权收敛：删四页各自的 Auth/promptForKey，接 `VMRAuth.guard`；回归点=各 API 的 401 路径。
5. archtest 无新边界（页面仍是 embed 资产）；行预算如触线按惯例调整。

## 11. 待拍板决策点

1. **`/stats.html` 退役方式**：重定向壳 → Overview 的 Live 区锚点（书签不烂），还是直接下线？
2. **zh 变体范围**：help 已有 `.zh` 兄弟页；Overview/Log 是否也补 `.zh`（模板注入机制下成本可控）？
3. **Log 页 footer**：状态条形态（demo 现状）还是与标准 Footer 完全同构？
