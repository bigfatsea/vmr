<!-- Ver 2026-09-14, by pi -->

# VMR 控制台统一设计方案（Console Unification）

**状态：v5，方案已定稿，待进入代码实施轮。** 本篇只做方案设计与静态 Demo
（`docs/design/demo/`），**不改动任何现有代码**——`internal/server/*.html`、各 JSON API、
路由挂载全部维持现状。三轮评审 + 2026-09-09 Dashboard 专项 Review 的全部决策已并入正文，
决策记录见 §11，未纳入本轮的条目见 §12。

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
| Key 逻辑 | `Auth` 对象 + modal + 401 重试 | **裸 input，无 401 流程、无 modal** | 自成一套 modal + `promptForKey` | 探测式（先无 Key 请求，失败再要） |
| 页内导航 | 有 | 有 | 只有 Status | 有 |
| Footer | 有（品牌 + GitHub） | 无 | 无 | 无 |
| 刷新 | 5 分钟固定 | 1s 轮询 | 流式 | 无 |

问题不在"缺样式"，而在**缺唯一权威**：同一套视觉语言被抄写了四份且已漂移，每加一个
页面就要再抄一遍。Key 逻辑的三种写法还带来行为不一致——同一台机器的四个页面是三种
鉴权体验。

## 2. 目标与原则

1. **一套令牌，一处权威**：颜色、字号、间距、圆角、组件类只有一份定义；页面不许再出现裸色值，
   连"给一个数字染个色"也要走工具类（`.t-ok/.t-warn/.t-err/.t-dim/.t-key`），不许行内 `style`。
2. **一副骨架，两行结构**：Header 的**第一行在三页之间逐槽同构**——品牌、页内导航、
   刷新/连接槽、uptime、Key 按钮、告警（**最右**）；第二行是**页面自有导轨**
   （Overview=区块锚点条、Log=终端工具条、Help=指南锚点条）。Footer 同构。
   宽度差异只作用于内容区。
3. **一个鉴权入口**：三页共享 `VMRAuth` 单例与同一只 modal，401 → 弹窗 → 存键 → 重试是唯一流程。
   所有浮层（鉴权、告警）共享同一套开合行为：`Esc` 关闭、点遮罩关闭、打开即聚焦、关闭还焦。
4. **Status 与 Stats 合并为一个 Overview 页，单页平铺、无 Tab**：区块自上而下按
   "健康总览 → 预算 → 拓扑 → 实时 → 失败 → 性能 → 流量与用量"排列；长页的可导航性由
   Header 第二行的**区块导轨（含滚动高亮）**承担，不靠用户自己滚。
5. **排版纪律**：标题、区块名与导航**一律不用 emoji**，层级靠字重/字号/颜色区分；emoji 只
   允许出现在紧凑图标控件上（Key 的 🔑、告警的 ⚠️/🚨），品牌标识只用 SVG logo。
6. **数字格式纪律**：
   - K/M/G 人读转写保留两位小数（`48.20M`），**但格式化时若小数部分恰为 `.00` 则整体去掉**
     （`200K`、`100M`、`489 GB`）——结构性常量不该顶着一串无意义的零。这条规则写在格式化
     函数里（`dec2()`），不由调用方逐处判断；
   - 整数计数用千分位（`12,847`）；字节量带单位与空格（`489 GB`、`1.20 GB`）；
   - 百分比同规则（`98%` / `68.20%`）；Headroom 是同一函数的产物（`1` / `0.60` / `2.33`）。
7. **颜色语义唯一**：红/黄/绿**只表达健康度**，永远不表达分类。任何"用颜色区分类别"的
   需求走中性色（cyan / purple / blue）或直接写字。一个健康的账号绝不能因为它的 limit
   恰好是 gate 而顶着一枚红徽章。
8. **首屏回答五个问题**：忙不忙（并发）、量多大（请求）、烧多少（token）、快不快（TTFT）、
   有没有坏（端点健康）。进程自身的内存/协程/磁盘不是这五个之一——它们进 `.sysline`，
   不占首屏指标位；而**纯静态的身份信息（go 版本、os-arch、listen 地址）连 sysline 都不占**，
   只在 Footer 出现一次。
9. **每个数字自带口径**：窗口（`since start` / `last 100` / 选定时间段）与单位写在标签上，
   公式与边界条件进 `title`。屏幕上不允许出现无法判断窗口的聚合数。

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
  /* 页宽与骨架高度（锚点偏移一处定义） */
  --page-max:1280px; --hd-h:88px;
}
```

组件类（console.css 提供，页面只许用类）：

- 结构：`.card` `.card-title` `.section-head` `.sub-head`（区块下一级，只给真正的子表用）
  `.table-wrap`（`.bounded` = 限高 56vh，让 `th` 的 sticky 真正生效）`.grid-2`
- 控件：`.btn`（`.primary`/`.ghost`/`.sm`/`.on`）`.seg` + `.seg-label`（一组互斥按钮及其口径标签）
  `.input` `.refresh-pill`（含 `.paused` 态——它同时是刷新时钟与新鲜度指示器）
  `.warn-btn`（`.has-warn`/`.has-err`/`.quiet`，内含 `.ico` 承载 ⚠️/🚨）
- 状态：`.badge`（`.b-ok/.b-warn/.b-err/.b-info/.b-dim/.b-cyan/.b-purple`，`.tight` 为窄内距变体）
  `.pill`（`.p-live/.p-connecting/.p-down/.p-paused`——**四态必须全部定义**，缺一个就会在
  暂停/断线时掉光样式）`.pri-1..6` + `.pri-n`（超出分层的中性档）`.stall`
- 数据：`.progress` `.sharebar`（表内占比条）`.vitals` + `.vseg`（指标带）`.sysline`
  `.ep`（长端点标识，`max-width` + 省略号 + `title` 全量）`.zero`（无样本行）
  `.mode`（传输方式文字标签，`.m-stream` 青 / `.m-json` 灰——不是图标）
- 浮层：`.modal-overlay/.modal` `.toast` `.wrow`（告警条目）
- 文本工具类：`.t-ok/.t-warn/.t-err/.t-dim/.t-key/.t-accent/.t-strong` `.hint` `.help`（虚下划线，
  提示"此处有 title"）

数字列一律 `font-variant-numeric: tabular-nums`。字号基准 13px。

## 4. 统一骨架：Header（两行）/ Footer

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ [◇] VMR·Console  Overview Log Help          (⟳4:37) (up 3d 4h 12m) [🔑] (🚨3)          │ 行1 · 三页同槽
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Overview  Quota 3  Models 3  Live 6  Failures 6  Performance  Traffic & Usage         │ 行2 · 页面自有
├──────────────────────────────────────────────────────────────────────────────────────┤
│   内容区（Overview/Help: 1280px 居中；Log: 满宽终端）                                  │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ VMR · v4.2.1 · pid 48211 · go1.24 · darwin/arm64 · listen 127.0.0.1:8800  bigfatsea/vmr│
└──────────────────────────────────────────────────────────────────────────────────────┘
```

- **行 1（sticky，毛玻璃底）**，左到右：品牌（SVG logo + `VMR` + `Console` 小徽）、页内导航、
  **刷新/连接槽**、**uptime 徽**、Key 按钮、**告警（最右）**。
  - **刷新/连接槽**是同一个位置的三种页面语义：Overview 放刷新倒计时 `⟳ 4:37`（点击即刷，
    暂停时整枚转黄并显示 `paused`）；Log 放流状态 pill（`streaming`/`paused`/`down`）；
    Help 放 `static`。一个槽位，一件事——"这一页的数据现在是什么状态"。
  - **uptime 徽**：进程存活时长。它会变，且"是不是刚重启过"是运维的高频判断，因此留在
    Header；版本 / pid / go / os-arch / listen 这些永不变的身份信息只在 Footer 出现。
  - **告警**：`⚠️ n`（仅 warning）或 `🚨 n`（含 error），整枚 pill 跟随最高严重度着色，
    为 0 时置灰。位置固定在**最右**——它是视线离开 Header 前的最后一站。
    内容**只承载可操作状态**：config issue、降级/冷却端点、即将耗尽的 quota gate。
    **滚动统计量（"过去 24h 有 84 个错误"）不进告警**：它永远为真，会把徽章永久钉在非零上，
    把运维训练成无视告警；错误率属于 vitals。
- **行 2（页面自有导轨）**：Overview=区块锚点条（带计数与滚动高亮）；Log=终端工具条
  （level 芯片 / 子串过滤 / Pause / Wrap / Copy view）；Help=指南锚点条。三页共用
  `.hd-rail` 容器与 `.rail-link` 样式，只是内容不同。
- **Key 按钮三态**：未存键 → `🔑 Set Key`；已存键 → `🔑 ····abcd`（悬停说明存储位置）；
  点击统一打开 §6 的 modal。
- **Footer**：两栏 slim 条，左=实例完整身份，右=仓库链接。Log 页的 footer 是同构**状态条**
  （左=终端自有状态：行数 / 缓冲 / level / 过滤器；右=实例身份与仓库链接）。
- 导航三页：**Overview / Log / Help**。

## 5. 宽度策略

| 页面 | 内容区 | 理由 |
| --- | --- | --- |
| Overview | `max-width:1280px` 居中 | 宽表（拓扑/Live/Perf）在 1280 下列宽充裕；超宽可读性反降 |
| Help | `max-width:1280px` 居中 | 长文阅读行宽 |
| Log | **满宽 + 100vh 终端布局** | 日志换行毁可读性；按屏高撑满、内部滚动 |

Header/Footer 通栏，内部 `.hd-inner/.rail-inner/.ft-inner` 按所在页对齐。

## 6. 鉴权统一契约（`console.js` 的 `VMRAuth`）

三页唯一的 Key 逻辑来源（demo 内联等价副本，实施轮为 `internal/server/assets/console.js`）：

```js
VMRAuth.get() / set(k) / clear() / has()
VMRAuth.guard(doFetch)                                // 包装页面的数据加载函数
openOverlay(el) / closeOverlay(el) / wireOverlay(el)  // 所有浮层共用
```

- **存储键**：`localStorage['vmr_key']`；启动回读旧键 `vmr_status_key`（一次性迁移，用户无感）。
- **401 唯一流程**：`guard` 捕获 401 → 弹共享 modal（密码框 + 错误行）→ 保存键并自动重试
  原请求一次 → 仍 401 则错误行提示。modal 由 `console.js` 注入 DOM，页面不再手抄。
- **浮层行为统一**：`Esc` 关闭任意打开的浮层、点击遮罩关闭、打开即聚焦首个可交互元素、
  关闭还焦到触发元素。鉴权 modal 与告警 modal 不许有两套开合手感。
- **Key 按钮三态**（§4）；清除键立即生效，下次 401 重新弹窗。
- help 页的"先探测后要键"改为直接走 `guard`（无键探测即 401，行为等价、流程统一）。

## 7. 共享资源的单一来源与注入机制

页面保持**单文件自包含**（保住现状优点），单一来源靠组装而不是复制：

- 仓库里只有一份 `internal/server/assets/console.css` 与 `console.js`（`go:embed`）；
- 页面文件在共享块位置留注入位标记（`/*{{CONSOLE_CSS}}*/`、`/*{{CONSOLE_JS}}*/`），
  server 启动时一次性字符串替换（进程内做一次，非每请求拼接）——与
  `internal/dashboard` 的 `WriteSkeletons` 内联 `common.js` 同一个已验证先例；
- 骨架（header 两行、footer、两只 modal）由 `console.js` 的 `mountConsole(active, rail)`
  注入 DOM，页面只提供内容与自己的导轨条目——导航增删、告警数量、倒计时、Key 状态只改一处。

**本轮 Demo 的物理形态**：为满足"双击即开"，demo 三页内联共享块的等价副本，不改变方案。

## 8. Status + Stats 合并：Overview 单页方案

### 8.1 Status 现状块 → Overview 的映射（压缩分析）

| 现状块（status/stats） | 压缩策略 | 去处 |
| --- | --- | --- |
| Header 徽章行（version/pid/listen/osarch/uptime） | uptime 留 Header；version/pid 进 `.sysline` 首段；go/os-arch/listen 只进 Footer | Header / sysline / Footer |
| 四张大指标卡 + Storage 卡 | 合成**一条 vitals 指标带**（单卡五段，§8.3）；系统明细/存储/audit 收成**单行** `.sysline`（无折叠、无标题） | 首屏 |
| 黄色告警 banner | 移除，并入 Header 最右的**告警 pill**（只留可操作项） | Header |
| Quota 表 | 保留，列重构（§8.4-a） | Quota Budgets 区 |
| Models 拓扑卡组 | **改全宽表**（§8.4-b）：一行 = 虚拟模型 × 主端点，虚拟模型名带协议前缀，含 Headroom 与 Share 列；Fallback 端点**单列一张同构表** | Virtual Models 区 |
| Connect Your Agent 卡 | **删除**（导航已有 Help；连接信息归 Help 页的 Connection 卡） | — |
| stats 并发卡 | 删除（与 vitals 并发段重复，去重） | — |
| stats in-flight 表 | 并入，列序与列义重构（§8.4-c），**随整页刷新** | Live Requests 区 |
| —（新增） | **Recent Failures**（§8.4-d）：填补 Live 与小时聚合之间的诊断盲区 | Recent Failures 区 |
| stats by_provider_model | 并入，列重构（§8.4-e） | Performance 区 |
| stats Requests-per-Hour | 升级为 **Requests & Tokens** 组合图（§8.4-f） | Traffic & Usage 带 |
| stats by_key×2 | 并入，列重构（§8.4-g），与图表同属 Traffic & Usage 带 | Traffic & Usage 带 |

### 8.2 单页平铺、区块导轨与整页刷新

**定案：单页平铺 + 整页统一刷新，只有一个刷新时钟。**

- Header 的倒计时（5:00）是唯一的刷新节奏，**点击立即刷新**；`document.hidden` 时暂停；
  暂停状态由倒计时 pill 自身表达（转黄 + `paused`），不另设连接徽。
- Live 表**不做**独立轮询。**将来要提升实时精度时走 SSE，而不是第二个轮询时钟**——
  秒级轮询会在页面上引入两套节奏与两个"数据有多旧"的答案，而 SSE 是单向推送，
  Live 区可以在整页节奏之外自然更新，不需要第二个时钟。本轮不做，先跑最简形态。
- 长页的可导航性由 Header 第二行的**区块导轨**承担：`Overview / Quota / Models / Live /
  Failures / Performance / Traffic & Usage`，条目带计数（Live 显示当前在飞数、Failures 显示
  缓冲条数），滚动时高亮当前区块。锚点偏移统一用 `--hd-h`，不在各处手写像素。

```
Overview 页（单页，自上而下）
├─ vitals 指标带（5 段）+ sysline 系统详情单行        ← /status
├─ Quota Budgets                                     ← /status
├─ Virtual Models & Endpoint Topology                ← /status + /stats（Share 列 join）
│   └─ Fallback Endpoints（同构独立表）
├─ Live Requests                                     ← /stats（in-flight 注册表）
├─ Recent Failures                                   ← /stats（新增 recent_errors 环）
├─ Performance by Provider & Model（10/100 切换）    ← /stats
└─ Traffic & Usage（单一时间窗控制整带）             ← /stats
    ├─ Requests & Tokens 组合图
    └─ Usage by Upstream Key Label / by Caller
```

### 8.3 vitals 指标带：五段

| 段 | 主数 | 副行 | 窗口标签 |
| --- | --- | --- | --- |
| Concurrency | `running / limit` | `N queued`（非零转黄）· `limit N` | `· now` |
| Requests | 累计请求数 | `ok` / `cancel` / `err + 错误率%` | `· since start` |
| Tokens | 四分量总和 | `in` / `out` / `cache N%`（口径进 title） | `· since start` |
| TTFT p50 | 全端点 p50（来自 `/stats` 的 `overall` 窗口块——分位数不可合并，必须服务端算） | `p90` · `tok/s p50` | `· last 100` |
| Endpoints | `healthy / total` | `N half-open` · `N cooldown` | `· now` |

原第四段 "System"（heap · goroutines · disk）**删除**：它与紧随其后的 `.sysline` 逐字重复，
且占据了首屏五个槽位中的一个去回答一个几乎不会先问的问题。腾出的两个槽位交给
**延迟**与**端点健康**——它们才是"快不快 / 有没有坏"的抓手，原方案里要滚到页面中部才看得到。

`.sysline` 只承载**会变的运行态**：`vmr <version> · pid` / heap+sys+goroutines / disk free /
audit 开关与占用 / image cache 开关与占用。go 版本、os-arch、listen 地址是启动即固定的
身份信息，只在 Footer 出现一次。

### 8.4 表 Schema（v5 定稿）

**a. Quota Budgets**

| 列 | 说明 |
| --- | --- |
| Provider : Key | 账号身份（key_label 对齐 §8.4-e 的键） |
| Limit | **徽章自述角色**：`bucket · tokens / 1mo`（青）/ `gate · requests / 1d`（紫），完整语义进悬停。**不靠颜色编码角色**——用红=gate 会让一个 13% 用量、headroom 2.33 的健康账号顶着红徽，违反 §2-7 |
| Models | `all models` 或具体模型名列表 |
| Amount → Used | Amount 在左（先看盘子再看吃掉多少） |
| Progress | 进度条 + 条下一行 `68.20% used`；接近耗尽时补一句剩余量（`98% used · 200 left`）并转红。**不显示在飞请求的预估增量**——为一个跨刷新周期就失效的估算值付出一次读侧计算与一行解释文案，不划算 |
| Headroom | 两位小数无量纲比值（§8.5） |
| Resets | **绝对时刻 + 相对时长**：`04:00 · in 47m`。同一列内三种格式（`in 16d` / `04:00 (9h)`）不可比 |

**b. Virtual Models & Endpoint Topology**（Fallback 表同构，仅首列不同）

| 列 | 说明 |
| --- | --- |
| Virtual Model | **带协议前缀**：淡色 `anthropic-messages:` + 亮色模型名。虚拟模型是 per-protocol 的，接入方要在这张表上就能判断该往哪个协议发；跨端点用 rowspan 合并，组首行加实线上边框 |
| PRI（Fallback 表为 ORD） | 优先级层级。Fallback 的排序是**独立空间**，用中性 `.pri-n` 徽章从 1 起编号，不套主模型的 P1/P2 色板 |
| Provider : Key : Model | key 段紫色；超长走 `.ep` 省略号 + `title` 全量 |
| Health | `healthy` / `half-open` / `cooldown 2m 13s`。**悬停必须给因果**：连续失败次数 + 最近错误类别；half-open 额外说明"下一个请求是唯一的试探" |
| Headroom | 账户级 quota headroom，与 §8.4-a 同源同色阶（§8.5）；无 quota 的账户 `—` |
| Share 24h | **迷你条 + 百分比**，该端点在过去 24h 转发请求中的份额。这一列把"配置意图"与"运行现实"放进同一行——没有它，拓扑表是纯配置视图，回答不了"我配了 5 个端点，现在谁在扛" |
| Context / Capabilities | 合并列：`200K` + 淡色 `· tools · image · thinking`。上下文窗口与能力标签都是同一个端点的静态属性，拆两列只是把一行读两遍 |

Share 列的数据来自 `/status`（拓扑）与 `/stats`（`by_provider_model` 的请求数）的一次
**客户端 join**——同一次刷新里各取一次，不污染 `/status` 的契约。口径按**请求数**，
不按 token（token 份额受模型差异影响，回答不了"流量落在哪"）。

**c. Live Requests**

| 列 | 说明 |
| --- | --- |
| State | queued（黄）/ running（青） |
| Elapsed | 到达至今。**紧随 State**——"哪一条卡了"是这张表的第一问题，诊断列不能排在末尾；queued 超过阈值转黄（排队积压是事故，不是抖动） |
| Model | 协议前缀 + 虚拟模型名 + **传输方式标签**（`.mode`：`stream` / `json`，写字不用图标） |
| Caller | `client_key_tag` |
| Provider : Key : Model | 未发出显示 `— routing`；超长走 `.ep` |
| Att | 当前 attempt 序号。**`1` 淡显、`—` 表示未发出、`≥2` 转黄徽章**——常态值不许跟异常值抢注意力 |
| First / Last Byte | 合并列：`0.81s · 3s ago`（首块延迟 · 末块距今）。列名跟随底层字段 `first_byte_at`/`last_byte_at` 写 **Byte**——同一张表里 `Tok in`/`Tok out` 装的是真 token 计数，两个词必须各归各的；流式末块 ≥10s 显示 `stalled Ns` 红徽，阈值写进悬停 |
| Tok in `est` / Tok out `est` | 拆两列，右对齐 tabular-nums。**表头明写 `est`**；压缩响应体数不出 out，显示 `—` 而不是 `0` |

**`Seq` 列删除**：进程内单调号是日志行之间的关联手段，不是这张表的诊断信息，
不值得一列宽度。

表体 `.bounded` 限高 56vh 并让表头 sticky；超过 10 行显示 `Show all N` 切换，不留"…还有 N 条"的死胡同。
空态两行：陈述 + "已完成的请求不在这里，见下方 Recent Failures / Performance / Traffic"——
避免"表是空的"被误读成"服务没在跑"。

**d. Recent Failures**（新增）

Live（正在发生）与小时聚合（发生过什么）之间存在一段诊断盲区：**"刚刚那条为什么失败"
在控制台上无处可查**。这张表补上它。

| 列 | 说明 |
| --- | --- |
| When | `14:32:07 · 2m`（绝对时刻 + 相对） |
| Model | 协议前缀 + 虚拟模型 + 传输方式标签 |
| Caller | `client_key_tag` |
| Provider : Key : Model | 从未转发成功的请求显示 `— never forwarded` |
| Att | 放弃前的尝试次数（`≥2` 转黄徽章） |
| Outcome | 错误类别徽章 + 淡色 HTTP 码：`upstream_5xx 502` / `rate_limit 429` / `transient 500` / `timeout` / `no_candidate` / `canceled`（canceled 用黄，其余用红）。**类别取自路由半区自己的 `ErrorClass`，控制台绝不重新推导** |
| Dur | client-view 总耗时 |

区块标题右侧是一排**按错误类别的计数芯片，同时也是过滤器**：一次 429 风暴能在几分钟内
把 50 条缓冲全部占满，没有分类过滤就等于看不见其余类别。芯片按计数降序，点击即筛、再点即清。

**与 Header 告警的分工**（两者都在讲"坏事"，边界必须写死）：告警承载**需要动手的状态**
（配置有错、端点被冷却、额度即将耗尽），它有明确的收敛条件；Recent Failures 承载
**单条请求的既成事实**，它只会随时间滚出缓冲。一个回答"我该做什么"，另一个回答"刚刚发生了什么"。

数据来源：livestats 新增一个 `recent_errors[]` 内存环（容量 50），与 TTFT ring 同族——
**纯内存、瞬态、重启清空、不落盘、不含任何正文**，与既有隐私分级一致。区块副标题写明
这三条性质，避免被当成审计记录。条目保留进程内请求号，悬停可见——它与 Log 页每行打印的
是同一个号，是两页之间唯一的关联手段（Live 表已不再单列 Seq）。

**e. Performance by Provider & Model**

| 列 | 说明 |
| --- | --- |
| Provider : Key : Model | key_label 内嵌；独立 Key Label 列删除 |
| Mode | `stream` / `json`，**独立一列、写字不用图标**（见下） |
| Requests | **窗口内的实际样本数**。ring 只收成功样本，所以它是「这一行的分位数究竟建立在多少个请求上」——不满窗口容量时转黄并在悬停里说明。失败不在这张表里，由 Recent Failures 与图上的错误标记承担 |
| Tok in+cw / cr | 窗口内输入侧两值：fresh+cache_write 与 cache_read |
| Tok out | 窗口内输出侧 |
| TTFT p50 / p90 | 首 token 延迟，nearest-rank |
| Tok/s p50 / p90 | **单请求四分量 token 总和 ÷ 该请求总耗时**，再取 nearest-rank 分位 |

**一行 = 一个 (端点 × 传输方式)**：ring 的键含 `stream`，设计上"绝不互混分位数"，
所以表的行键也必须含它。**传输方式写成独立一列的文字标签，不用图标**——图标要求读者
先学一遍图例才能读表，而这是一个只有两种取值的分类维度，直接写字的成本是一列宽度、
收益是零学习成本。同一端点的两行之间加组分隔线。

**整行同窗口，不混两个时间基**：`last 10 / last 100` 是这张表唯一的窗口，**样本数、token
求和、分位全部跟着它走**，而且都来自同一批成功样本。`Requests` 因此不是"顺便给个总量"，
它是**窗口的填充度**：显示 43 就是这一行只跑过 43 个请求，它的 p90 建立在 43 个样本上而
不是 100 个——没有这个数，一个 12 样本的 p90 会被当成 100 样本的 p90 来读。

**这张表不分 ok/err**：ring 只收成功样本，硬要在这里拼出错误分布就得让 ring 也收失败样本，
为一列信息把一个纯粹的"性能样本池"变成混合池。失败该看的是**哪一条、为什么**，那是
Recent Failures 的事；"什么时候开始错的"是图上的错误标记；总体错误率在 vitals。

某行在窗口内无样本时，整行 `.zero` 淡显、`Req` 显示 `0`、其余列 `—` 并挂 `no samples`
徽章，**不显示 `0.00M`**——0 是一个值，`—` 才是"没测到"。

**TPS 列删除，Tok/s 保留**：两列本就重复（都在回答"这个端点吐得快不快"），并排三种
速率口径只会让读者先分辨口径再读数。留下的是 `Tok/s`——名字自解释，口径也更完整
（含输入摊销的整请求吞吐），而 TPS 那种"流式剔除首块等待"的细分，价值已经由旁边的
`TTFT` 列单独承担了。

> **数据模型依赖（LiveStats 文档已同步改好，两边现在对得上）**：这张表要求 ring 条目从
> `(ts, dur_ms, ttft_ms, tokens.out)` 拓宽为 `(ts, dur_ms, ttft_ms, tokens 四分量)`，ring key
> 从 `(provider, model, stream)` 变为 `(provider, key_label, model, stream)`，
> `last_10`/`last_100` 从纯分位升级为含 `n` 与 `tokens` 的**窗口块**，并撤销 `tps` 只留 `toks`。
> **ring 的准入规则不变——仍然只收 `outcome=ok` 且已转发的样本**：把失败样本塞进性能池
> 只为了凑一个错误分布，会把一个口径干净的样本池变成混合池，而失败明细已经由
> Recent Failures 单独承担。

**f. Requests & Tokens 组合图**

- 默认**过去 24 小时**，可选 3d / 7d（7d 上限）；多日档按 3h/6h 桶聚合。
- **Requests = 折线**（右轴）；**Tokens = 柱**（左轴）：in 一根（堆叠 fresh/cache_write/
  cache_read）+ out 一根，逐桶成组。两侧轴各写一个轴名（`tokens` / `requests`），
  只靠图例说明轴归属会让人反复回看。
- **含错误的桶在折线上打红点**：这是全页唯一的时间序列，错误尖峰若不在这里显形，就
  哪里都看不到"什么时候开始坏的"。数据来自 `hourly[]` 已有的 `error` 计数，无需新采集点。
- 纯 SVG 实现（无图表库，保持零依赖纪律）。
- **Hover**：淡蓝导引带标出当前桶（否则不知道 tooltip 在说哪一根），tooltip 逐项列全——
  时间范围、requests + 错误数与错误率、`tok in 总计 = fresh + cw + cr`（分项全列）、tok out。
  tooltip 先渲染后测宽再定位，不用硬编码宽度猜测（右边缘会溢出）。

**g. Usage by Upstream Key Label / by Caller**（两表同构）

| Key Label（或 Caller） | Req ok / err | Tok in+cw / cr | Tok out | Share |

- **按总 token 降序**：这两张表唯一的问题是"谁在烧"，第一大户必须是第一行；
- **Share 列**：`.sharebar` 迷你条 + 百分比；
- **`<tfoot>` 合计行**：没有分母的份额是不可读的。

**Traffic & Usage 用一个时间窗控件统辖整带**：图表与两张 Usage 表收进同一个区块，
区块标题右侧**只保留一组** `24h/3d/7d`，副标题写明"the window below applies to the chart
and both usage tables"。三个永远同步的相同控件不是冗余保险，而是"哪个才算数"的歧义源。

### 8.5 Headroom：口径、色阶与数据可得性

**口径**：Headroom 是 quota 文档 Core Algorithm 定义的无量纲比值——
`(1 - used_frac) / max(time_left_frac, ε)`，clamp 到 `[0, HeadroomCap=5]`：`=1` 严格按进度
消耗、`>1` 欠用、`<1` 超前、`=0` 耗尽；**无量纲**因此 requests 账号与 tokens 账号可直接
比较。展示为两位小数比值（走 §2-6 的同一格式化函数），不做绝对量转写（绝对量随周期
长度含义漂移）。`/status` 的 quota 行已输出 `headroom float64`，口径与路由评分完全同源。

**色阶（唯一权威，Quota 表与端点表必须一致）**：

| 值域 | 颜色 | 含义 |
| --- | --- | --- |
| `= 0` | 红 | 耗尽，该账号已被排除出路由 |
| `< 1` | 黄 | 超前消耗 |
| `≥ 1` | 绿 | 按进度或欠用 |

**只有两档半，不加第三档。** `1.00` 是 quota 算法里唯一有语义的分界；再引入一个
（比如 `< 0.5` 转红）就是在 UI 层制造一个没有权威来源的阈值，评分曲线一改就脱节。
**"快要用完了"的紧迫感由 Progress 列的红条承担**：两列各自回答一个问题（还剩多少 /
花得快不快），混色只会互相污染。色阶由这张表决定，不许逐行手写颜色。

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

### 8.6 极端状态契约（每个区块都必须实现）

一个只在"数据齐全且正常"下好看的 dashboard 会在真正需要它的那一刻失效。四类状态是硬要求：

| 状态 | 呈现 |
| --- | --- |
| 空 | 一句陈述 + 一句解释（Live 表与 Failures 表见 §8.4-c/d）。绝不留白表，也绝不让空态被读成故障 |
| 未知 vs 零 | 未知一律 `—` 并挂 `title` 说明为何未知（压缩体的 est_out、无 quota 的 headroom、无样本的分位）。`0` 只用于真实的零 |
| 降级 | 自动刷新暂停/失败 → 倒计时 pill 转黄并显示 `paused`（Log 页为流状态 pill 转 `down`）。数据是旧的这件事必须留在屏幕上，不能被静默刷新掩盖 |
| 超长文本 | 端点标识、模型名、caller 名一律 `.ep` 截断 + `title` 全量；表容器横向滚动是兜底，不是设计 |

Log 页另有两条：断线用**独立横幅 + Reconnect 按钮**（不许把"回到底部"的芯片改造成重连
按钮——一个控件两种语义会在最需要冷静的时刻误操作）；行数状态在过滤时显示
`N shown / M lines`，不显示会误导的单一总数。

### 8.7 退役映射（实施轮）

| 旧 | 去 |
| --- | --- |
| `status.html` | 重构为 Overview（本方案） |
| `stats.html` 各区块 | 并入 Overview 对应区块；`/stats` **JSON API 保留并增补**（见 §10），读路径契约继续有效 |
| `/stats.html` URL | **直接下线**，不留重定向壳。本机自用工具，书签成本可忽略；留一个只为兼容的空壳文件与路由，是给一个短生命周期的问题付长期维护成本。`CHANGELOG` 的 `Removed` 里点名 |
| Connect 卡内容 | 归 Help 页的 Connection 卡（已含 base_url / 模型与其协议 / auth 说明 + 复制按钮） |

## 9. Demo 交付说明

| 文件 | 内容 | 模拟交互 |
| --- | --- | --- |
| `overview.html` | 平铺单页：vitals(5段)+sysline / Quota / Models+Fallback / Live / Recent Failures / Performance / Traffic & Usage（图 + 两张 Usage 表，单一时间窗） | ① 区块导轨跳转 + 滚动高亮 ② 告警 pill 弹窗（3 条可操作 mock，⚠️/🚨 随严重度切换）③ 刷新倒计时点击即刷（Live 表随整页推进：请求生命周期、failover、stall 跨刷新演化）④ 图表 hover 导引带 + DOM tooltip（requests、错误数与错误率、tok in 总计及三分项、tok out）⑤ 单一 24h/3d/7d 控件联动图表与两表 ⑥ Performance 窗口切换（整行同窗口：计数、token、分位一起变；json 行在 last-10 下呈现 `no samples` 态）⑦ 鉴权 modal（存键/清除/演示 401）⑧ Demo 控制面板（暂停/注卡死/注排队积压/演示 401/重置，可折叠） |
| `log.html` | 满宽终端 | level 芯片过滤、子串过滤（命中高亮）、Wrap 切换、Copy view、Pause（⌘P，含可见按钮）、自动滚动 + 上滚暂停 + "↓ N new" chip、~45s 模拟断线 → **独立横幅 + Reconnect**、统一 footer 状态条（`N shown / M lines`） |
| `help.html` | Agent 指南 | Connection 卡（base_url 复制、模型 × 协议对照、auth 说明）、指南锚点导轨、手风琴、片段复制（✓ 反馈）、连接检查（读共享 Key）、Troubleshooting 直链 Overview 锚点 |
| `index.html` | Demo 入口 | — |

**Demo 按目标形态画，跑在现网 payload 前面**：其中一部分数据 `/status` / `/stats` 目前
还给不出来（全局 TTFT、端点 headroom 与流量份额、Fallback 分表、Recent Failures、
Performance 的窗口块、图表的 3d/7d 窗口、Header 告警列表）。逐字段的对照与缺口编号见
§10.1 / §10.2——实施轮照那张表把后端补齐再接线，不要先把页面搭起来留一批空格子。

所有页面右上 `DEMO · mock data` 缎带；Overview 右下 Demo 控制面板仅存在于 demo。
Mock 无任何网络行为，数据形状与 `/status`、`/stats` 现有及增补后的 JSON 契约一致；
虚拟模型与 ingress 协议的对应关系在 Live 表、Failures 表、拓扑表、Help 页四处保持同一份
定义（一个虚拟模型只属于一个协议），mock 数据自身不许互相矛盾。

## 10. 实施轮：数据缺口与落地建议

### 10.1 数据缺口对照表

**Demo 是按目标形态画的，不是按现网 payload 画的。** 下表逐字段对照"页面上显示了什么"与
"现在的 `/status` / `/stats` 到底给不给"，实施轮照着它一次把缺口补齐——否则会出现页面
搭好了、某几格永远是 `—` 的局面。三种状态：**已有**（现网 payload 里就有）、
**派生**（数据已有，前端算一下即可）、**缺**（后端必须新增，编号见 §10.2）。

| 区块 | 页面上的数据 | 来源 | 状态 |
| --- | --- | --- | --- |
| vitals | 并发 running / limit / queued | `/status` `instance.concurrency{limit,in_flight,waiting}` | 已有 |
| vitals | 请求 total · ok · cancel · err | `/status` `traffic.requests{total,by_status}`（键为 `ok`/`canceled`/`error`） | 已有 |
| vitals | 错误率 % | 上一行前端相除 | 派生 |
| vitals | token 总量 · in · out · cache% | `/status` `traffic.tokens.total`——**注意它是五分量**（`in`/`cache_write`/`cache_read`/`reasoning`/`out`），页面写死总量与 cache 分母的口径，别默认四分量 | 已有 |
| vitals | 全端点 TTFT p50 / p90 · tok/s p50 | — | **缺 G1** |
| vitals | 端点 healthy / total · half-open · cooldown | `/status` `models[].endpoints[].{available,serving,consecutive_failures,cooldown_until}`，三态由前端归类 | 派生 |
| sysline | version · pid | `/status` `instance` | 已有 |
| sysline | heap / sys · goroutines · disk free | `/status` `system` | 已有 |
| sysline | audit 开关/占用/文件数 · image cache | `/status` `audit` / `image_cache` | 已有 |
| Header | uptime | `/status` `instance.uptime` | 已有 |
| Header | 告警列表（配置问题 / 降级端点 / 濒临耗尽的 gate） | — | **缺 G2** |
| Quota | provider · role · metric/period · models · amount · used · pct · headroom · period_ends_at | `/status` `quota[]` | 已有 |
| Quota | 「还剩 N」 | `amount - used`，前端算 | 派生 |
| 拓扑 | 虚拟模型 + 协议 | `/status` `models[].{id,protocol}` | 已有 |
| 拓扑 | PRI | `endpoints[].priority` | 已有 |
| 拓扑 | `provider : key : model` 三段 | 现在只有合成的 `endpoint` 名（`adapterType/provider/model`），拆字符串正是 LiveStats 判定为脆弱的做法 | **缺 G3** |
| 拓扑 | Health + 成因悬停 | `endpoints[]` 内嵌的 `health.Status{consecutive_failures,last_error,cooldown_until}` | 已有 |
| 拓扑 | 端点级 Headroom | 需要 `core.Endpoint.Quota` 与 `quota.Registry` 的读侧 join（§8.5） | **缺 G4** |
| 拓扑 | Share 24h | `hourly[]` 按 `(provider,key_label,model)` 折叠，前端算 | 派生（依赖 **G6**） |
| 拓扑 | Context / Capabilities | `endpoints[].{max_context_tokens,capabilities}` | 已有 |
| 拓扑 | **Fallback 独立表** | fallback 端点当前被**合并进每个模型的 `endpoints[]`**（`core.Endpoint.FromFallback` 有标记，但 `/status` 没把它输出），页面分不出哪几行是 fallback | **缺 G5** |
| Live Requests | 全部列（state/elapsed/model/caller/endpoint/attempt/首末块/est in·out） | `/stats` `inflight[]`（`router.InflightEntry`） | 已有 |
| Recent Failures | 整个区块 | — | **缺 G7** |
| Recent Failures | 错误类别芯片与过滤 | 前端按 `error_class` 分组 | 派生（依赖 G7） |
| Performance | 行键含 `key_label` | 现在 `by_provider_model[]` 只有 `provider`/`model`/`stream` | **缺 G8** |
| Performance | Requests（窗口内样本数）· 窗口内 token 四项 | `last_10`/`last_100` 现在只有分位数，没有 `n` 与 `tokens` | **缺 G8** |
| Performance | TTFT p50 / p90 | 已有，但要随窗口块改形状 | **缺 G8**（形状） |
| Performance | Tok/s p50 / p90 | 现在输出的是 `tps`（口径不同） | **缺 G8** |
| Performance | Mode（stream / json） | `by_provider_model[].stream` | 已有 |
| Traffic 图 | 逐桶 requests · errors · token 四分量 | `/stats` `hourly[]` | 已有 |
| Traffic 图 | 24h / 3d / 7d 窗口 | hourly 尾窗现在固定 48 小时，3d(72h)、7d(168h) 都超了 | **缺 G6** |
| Usage 两表 | 按选定区间的 key_label / caller 用量 | `hourly[]` 折叠（`by_key_label[]`/`by_client_key_tag[]` 是全量累计，口径不同，不能直接用） | 派生（依赖 G6） |

### 10.2 缺口清单

| # | 缺口 | 归属 | 备注 |
| --- | --- | --- | --- |
| G1 | `/stats` 新增 `overall` 合并窗口块 | livestats | 分位数不可合并，首屏那个全局 TTFT p50 只能服务端算 |
| G2 | `/status` 新增告警列表 | server | 配置问题 + 降级/冷却端点 + 濒临耗尽的 quota gate。只放**可操作状态**，滚动统计量不进（§4） |
| G3 | `/status` 端点行拆出 `provider` / `key_label` / `model` | server | 保留现有 `endpoint` 合成名不动，只加字段 |
| G4 | `/status` 端点行补 `headroom` | server | 纯读侧 join，必须调 quota 包既有导出入口，不复述公式（§8.5） |
| G5 | `/status` 端点行补 `from_fallback` | server | `core.Endpoint.FromFallback` 已有，只是没输出；没有它就画不出 Fallback 独立表 |
| G6 | `/stats` 支持 `?range=24h\|3d\|7d` | livestats | hourly 尾窗随之放宽到 24/72/168 小时，上限 7d |
| G7 | `/stats` 新增 `recent_errors[]` | livestats | 容量 50 的纯内存环，含 `error_class`（直接引用 `core.ErrorClass`，不重新分类） |
| G8 | ring 与 `by_provider_model[]` 改形 | livestats | key 加 `key_label`；条目存 token 四分量；`last_10`/`last_100` 升级为含 `n` 与 `tokens` 的窗口块；撤销 `tps` 改 `toks`。**准入规则不变，仍只收成功样本** |

G1 / G6 / G7 / G8 在 LiveStats 设计文档里已逐条改好，两份文档现在对得上；
G2–G5 属于 Part 1 的 `/status` 契约，只在本文登记。

### 10.3 落地顺序

1. **先补 §10.2 的 G1–G8，后端先行**。页面每一格都能拿到真数据之后再接线，否则会留下
   一批"先渲染成 `—`、以后再说"的格子，而"以后"通常不会来。`/stats` 侧（G1/G6/G7/G8）在
   LiveStats 设计文档里已逐条改好；`/status` 侧（G2–G5）只在本文登记。
2. 落 `internal/server/assets/console.css` + `console.js`（单一来源），页面改模板 + 注入位；
   server 启动时一次性组装（对齐 `internal/dashboard` 的 `commonJSTag` 先例）。
   数字格式化的 `dec2()`（§2-6）落在 `console.js`，前端不逐处判断。
3. `status.html` 重构为 Overview（§8 映射），`stats.html` 与 `/stats.html` 路由下线，
   `log.html`/`help.html` 换骨架；`admin_log_test.go` 等页面 marker 断言同步更新。
4. 鉴权收敛：删各页自有的 Auth/promptForKey，接 `VMRAuth.guard`；回归点=各 API 的 401 路径。
5. zh 变体：**只有 Help 保留 `.zh` 兄弟页**。Overview / Log 的内容 90% 是标识符、指标名与
   数字，翻译后反而增加认知摩擦——运维对这些术语的英文形式更熟。
6. archtest 无新边界（页面仍是 embed 资产）；行预算如触线按惯例调整。

## 11. 决策记录

| # | 议题 | 决定 |
| --- | --- | --- |
| 1 | Live 区刷新节奏 | **维持整页 5 分钟**，先跑最简形态。将来提升实时精度走 **SSE**，不引入第二个轮询时钟（§8.2） |
| 2 | Performance 是否按 `stream` 拆行 | **拆行**，传输方式用**独立 `Mode` 列的文字标签**——图标要先学图例才能读表（§8.4-e） |
| 3 | 拓扑是否补流量份额 | **补 `Share 24h` 列**，按请求数，客户端 join `/status` 与 `/stats`（§8.4-b） |
| 4 | Headroom 是否加第三档色阶 | **维持两档**，紧迫感交给 Progress 列（§8.5） |
| 5 | 是否引入"最近失败请求" | **引入**，livestats 新增 `recent_errors[]` 内存环（§8.4-d） |
| 6 | `/stats.html` 退役方式 | **直接下线**，不留重定向壳（§8.7） |
| 7 | 结构性常量的两位小数 | **在格式化函数里判断**：小数部分为 `.00` 则整体去掉，规则统一（§2-6） |
| 8 | zh 变体范围 | **只有 Help 保留 zh**（§10-6） |
| 9 | Log 页 Footer 形态 | **维持状态条**：左=终端自有状态，右=三页共有的实例身份，事实上已与标准 Footer 同构（§4） |
| 10 | Usage 表的 $ 成本估算 | **本轮不做**，记入 `ROADMAP`（§12） |
| 11 | Quota 的在飞预估增量 `est %` | **删除**，简化读侧计算（§8.4-a） |
| 12 | Performance 的 TPS / Tok/s 两列 | **只删 TPS，保留 Tok/s**——两列本就重复，留下名字自解释、口径更完整的那个（§8.4-e） |
| 13 | Live 表的 Seq 列 | **删除**，关联号不占诊断列宽（§8.4-c） |
| 14 | Live 表 `First / Last` 列名 | 用 **`First / Last Byte`**，跟随底层字段命名；同表内 `Tok in/out` 是 token 计数，两个词不混用（§8.4-c） |

## 12. 未纳入本轮（记入 ROADMAP）

- **Usage 表的 $ 成本估算**：`internal/pricing` 已有两层费率解析能力，`/stats` 目前不带价格。
  落地时必须贯彻 pricing 包的既有纪律——unpriced 显示 `—` 而不是 `$0.00`（"nil 是未知不是免费"）。
- **vitals 的趋势暗示**：当前五段都是标量，没有"比一小时前更糟了吗"的信号。
  `hourly[]` 的数据足以画迷你 sparkline，但会显著抬高指标带高度，暂不做。
