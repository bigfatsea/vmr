// Ver 2026-09-07, by Claude Sonnet 5

# analyze 看板（HTML 骨架页）复核与修复记录

> 本文件是一轮针对 `vmr analyze` HTML 看板产物的复核 + 修复的过程记录。
> 源起：用户以 `python -m http.server` 托管 `reports/` 逐页走查后提出的三个具体问题
> ＋四项统一化要求。范围限定在 `internal/dashboard/assets/*.html` 与 `common.js`
> 及其测试；不触碰数据层（切片 schema、journey/compare JSON）与路由半区。
>
> 跟踪基线：vmr @ main（起点 847069b）。
>
> **第二轮（2026-09-08）**：用户批准执行 §5.1 / §5.2 / §5.5 三项遗留事项，记录见 §8。

---

## 0. Debrief：问题全景

### 0.1 用户直接报告的问题

| # | 页面 | 报告内容 | 我的定性 |
|---|---|---|---|
| U1 | `journey-viewer.html` | 候选列表里点 "open →" 无反应；复制链接到新窗口打开才行。怀疑同名页面 hash 变化不触发刷新。建议把列表页改名 `JourneyBrowser`。`journey-compare.html` 同病，建议拆 `journey-compare-browser`。 | **根因确认**：同页 `#data=` 导航不重载，JS 不重跑。**不采纳改名方案**，用 `hashchange → reload` 修，理由见 §3.1。 |
| U2 | `journey-compare.html#data=…` | HTML 内容与同名 `.md` 对不上：`.md` 完整（14 项指标 + 初始指令 + 分叉点 + 端点 + 缓存 + sys prompt + 交付物 + 成本 + 溯源 + LLM 解读），HTML 只有一个简单对照表。JSON 数据完整，是 HTML 没画。 | **确认**：`renderCompare` 只消费了 `a_journey`/`b_journey`/`rows`/`tools`，完全忽略 `extras.*` 与 `llm_interpretation`/`llm_divergence`。且 `rows` 的 `kind` 值域对不上（见 N1）。按 `.md` 结构补齐。 |
| U3 | `tool-waste.html` | 布局太窄，应和其他页统一为 1280px。 | **确认**：该页 `max-width: 800px`，是六页里唯一的"卡片"宽度。改 1280。 |

### 0.2 用户的四项统一化要求

| # | 要求 | 现状 |
|---|---|---|
| G1 | 统一所有 HTML 页导航栏样式、布局 | 导航 markup 六页一致；CSS 有轻微漂移（`transition`、`button.primary` 是否进 CSS、tool-waste body 用 mono）。 |
| G2 | 统一默认布局宽度 1280px | 实测：macro-dashboard **1360**、request-browser **1400**、tool-waste **800**、其余三页 1280。 |
| G3 | 每页都要有到对应 Markdown/JSON 文件的下载链接 | **六页全无**。 |
| G4 | 对 `reports/` `reports-en/` 两目录下生成的报告做最后复核，看 HTML/Markdown 还有无明显问题 | 见 §4 复核发现。 |

### 0.3 复核中新发现的问题（详见 §4）

| # | 严重度 | 一句话 |
|---|---|---|
| N1 | 中（bug） | `journey-compare.html` 的 `fmtMetric` 判 `kind === 'dur'`/`'pct'`，而 compare JSON 的 `rows[].kind` 实际是 `ms`/`multiple`/`ratio`/`count`/`tokens` → 时间指标显示成裸毫秒数（`1947365` 而非 `1947.4s`），比值显示成 `2.24073811981272`。 |
| N2 | 中（bug） | `journey-compare.html` 的 `fmtDelta` 自己用 a/b 重算百分比，弃用了 JSON 里现成的 `delta_rel`，也没实现 `.md` 的 `156×`/`0.02×`/`新增`/`-100%` 口径。 |
| N3 | 低（一致性） | `request-browser.html` 所有用户可见文案是中文，其余五页全英文。骨架页是语言中立的（同一文件写进 `reports/` 和 `reports-en/`），必须二选一。 |
| N4 | 低（一致性） | `file://` 提示横幅的文案六页有三种写法。 |
| N5 | 低（一致性） | 版本探测的 `missing` 分支：macro / request-browser 处理了（显式 `banner-missing`），journey-viewer / journey-compare / benchmarks / tool-waste 没处理 `st === 'missing'`（靠 `catch` 兜底，行为差不多但不齐整）。 |
| N6 | 中（缺口，**不在本轮修**） | `journey-viewer.html` 详情视图相对 `journeys/details/j-<id>.md` 也有大面积 parity 缺口（无 System Prompt、无行为指标表、无上下文构成 sparkline、无模型使用/切换、工具只有 chip 不显示 result 正文、无时序图、findings 无 evidence 展开）。用户本轮只点了 compare，此项记录为待决（§5.1）。 |
| N7 | 低 | `common.js` 的 `svgLatencyPlot` 里对 `p50/p90/p99` 有一段"没有真值就按 `dur_ms * 0.7 / 1.3` 编造"的 fallback（`d.dur_ms ? d.dur_ms * 0.7 : 0`）。编造分位数是危险的展示——宁可显示"无数据"。记录为待决（§5.2）。 |
| N8 | 低（一致性/XSS 面） | `request-browser.html` 的行渲染把 `r.model` / `r.endpoint` / `sessionTitle` 等对话派生字符串未转义插进 `innerHTML` 与 `title=""` 属性。带 `"` 的 session title 会破坏 `title` 属性、带 `<` 的会当标签。本轮只改了该页 chrome，未动行渲染逻辑；`journey-viewer` / `journey-compare` 因本轮大改已顺带补了 `esc()`。记录为待决（§5.5）。 |

---

## 1. Action Plan

分两类：**A. 直接修**（事实清楚、根因与方案无争议、10 分把握）；**B. 记录待决**（有取舍/需决策）。

### A. 直接修（本轮执行）

1. **A1 — hashchange 重载**（U1）：`journey-viewer.html` / `journey-compare.html` 在 IIFE 顶部挂 `hashchange → location.reload()`。候选列表 `<a href>` 保持不变（复制链接 / 新窗口打开继续可用），同页点击也能重跑。新增 `common.js` 的 `wireHashReload()` 复用。
2. **A2 — compare 渲染补齐**（U2 + N1 + N2）：`renderCompare` 按 `.md`（`internal/journey/render_compare.go` + `i18n/journey_compare.go`）的章节顺序补齐：对比摘要卡、初始指令、行为剖面（修 `kind` 值域 + `delta` 口径）、墙钟/终止/末轮上下文、工具对比、分叉点、模型与端点、Prompt 缓存（含逐轮曲线）、System Prompt、最终交付物、成本估算、证据溯源、LLM 解读（overall + divergence）。章节标题用英文（与其余五页 chrome 一致），指标行 label 直接取 JSON 里已本地化的 `rows[].label`。
3. **A3 — 宽度统一 1280**（U3 + G2）：`macro-dashboard.html`(1360→1280)、`request-browser.html`(1400→1280)、`tool-waste.html`(800→1280) 的 `.topbar-inner` / `.banner` / `.container` `max-width`。
4. **A4 — 导航/外壳 CSS 统一**（G1）：六页的 `header.topbar` / `.topbar-inner` / `nav.nav-links` / `.banner` / `.container` 收敛为同一份；tool-waste body 字体 mono→sans（与其余五页一致，其 `.topbar` 里冗余的 `font-family: var(--sans)` 随之删除）；macro 的 `nav a { transition }` 保留并补进其余页（低成本、观感更好）。
5. **A5 — 下载链接**（G3）：`common.js` 新增 `sourceBar(links)` 返回统一样式的 "⬇ Source:" 链接条字符串；六页在渲染完成后注入对应的 `.json` / `.md` / `.jsonl` 相对链接。已知限制：配了 `api_keys` 时纯 `<a>` 导航不带 Bearer，会 401——但用户实际用法（`python -m http.server`）无鉴权，覆盖主场景，缺陷记进 §5.3。
6. **A6 — request-browser 英文化**（N3）：三处横幅 span + 版本不一致模板文案译成英文。
7. **A7 — file:// 横幅文案统一**（N4）：六页用同一句。
8. **A8 — missing 分支齐整**（N5）：四页补 `st === 'missing' → banner-missing`。
9. **A9 — 测试跟随**：`js_test.go` 的 compare-detail mock 改用真实 `kind` 值 + 补新章节断言；smoke 沙箱 `window` 补 `addEventListener`。`dashboard_test.go` 的 snake_case 断言按新读法调整。

### B. 记录待决（本轮只写文档）

- **B1 — journey-viewer 详情 parity**（N6）：§5.1
- **B2 — svgLatencyPlot 编造分位数**（N7）：§5.2
- **B3 — 下载链接在鉴权下 401**：§5.3
- **B4 — 骨架页语言中立 vs `reports/` 是中文目录**：§5.4
- **B5 — 用户的"改名 JourneyBrowser / 拆 journey-compare-browser"提案**：§3.1（给出不采纳的理由）

---

## 2. 执行记录（逐项）

> 状态：⬜ 未开始 / 🟡 进行中 / ✅ 完成 / ⏸️ 待决

### A1 — hashchange 重载 ✅

- **改动**：`common.js` 新增 `wireHashReload()`；`journey-viewer.html`、`journey-compare.html` 的 IIFE 顶部（`Theme.init()` 之后）调用。
- **验证**：`go test ./internal/dashboard/`（含 node smoke）通过；手工核对 `#data=` 点击路径。
- **结果**：候选列表点击 → hash 变 → `hashchange` 触发 → `location.reload()` → `getDataParam()` 拿到路径 → 渲染详情。复制链接 / 新窗口打开不受影响（本来就是全新加载）。返回候选列表（hash 清空）同样触发重载。

### A2 — compare 渲染补齐 ✅

- **改动**：`journey-compare.html` 的 `renderCompare` 从 ~55 行扩到覆盖 13 个章节；`fmtMetric` 重写 `kind` 值域（`ms`→`FmtDuration`、`multiple`→`N.NN×`、`ratio`→`FmtPercent`、`tokens`→`FmtTokens`、`count`→整数）；`fmtDelta` 重写为 `.md` 的 `formatDelta` 口径（ratio 分档：`≥100→N×`、`≥2→N.N×`、`≤0.1→N.NN×`、`≤0.5→N.N×`、否则 `±N%`；`a==0 && b==0→—`、`a==0→new`、`b==0→-100%`）。
- **新增章节**（顺序对齐 `render_compare.go`）：
  1. Comparison Summary（notable Top3 / 分叉点 / 端点异同 / 终止状态）
  2. Initial Instruction（A/B `<details>` 折叠，`truncated` 标注）
  3. Behavior Profile（14 行，`notable` 行加 ⚠️，脚注）
  4. Wall-clock + Termination 行 + Final-Turn Context 4×2 表
  5. Tool Call Comparison
  6. Divergence Point（light/heavy 分级 + 脚注）
  7. Model & Endpoint Check（A/B 端点列表 + same/diff 断言句）
  8. Prompt Cache Hit Rate（首轮/稳态/min/max 表 + 逐轮曲线 `<details>`）
  9. System Prompt Size & Stability（tokens/changes 表 + excerpt/diff）
  10. Final Deliverable Comparison（A/B step_seq + tool + excerpt 折叠）
  11. Cost Estimate（A/B + 单侧未定价脚注）
  12. Evidence Provenance（source 审计文件列表）
  13. LLM Interpretation（overall + divergence，各带"非事实层"免责 + 缓存标注）
- **HTML 转义**：新增 `esc()`（`& < > "`），所有插入 `innerHTML` 的 JSON 字符串（title / excerpt / instruction / endpoint / llm text 等）过一遍。`.md` 侧本就有 `escapeHTML`，看板此前靠 `innerText`-free 的字符串插值有 XSS 面（对话正文里带 `<script>` 就命中）——一并堵掉。
- **验证**：node smoke 对 `compares/compare-*.json` 真数据渲染，断言无 `undefined`/`NaN` 且新章节标题出现。
- **结果**：HTML 与 `.md` 章节结构一致；数字口径一致（复用同一批 `Fmt*`）。逐轮曲线、`<details>` 折叠、免责语都在。

### A3 — 宽度统一 1280 ✅

- **改动**：三文件 `.topbar-inner` / `.banner` / `.container` 的 `max-width` → `1280px`。`tool-waste.html` 的 `@media (max-width: 640px)` 卡片降级保留。
- **结果**：六页外壳等宽。tool-waste 从"窄卡片"变为标准页；hero 数字仍居中，stats `repeat(4,1fr)` 铺满，脚注 `.hcap`/`.foot` 保留阅读宽度上限。

### A4 — 导航/外壳 CSS 统一 ✅

- **改动**：六页 `header.topbar` / `.topbar-inner` / `nav.nav-links` / `.top-actions` / `.banner` / `.container` 收敛同一份；`nav.nav-links a` 统一带 `transition: color .15s, background .15s`；`tool-waste.html` body `font-family: var(--mono)` → `var(--sans)`，`.topbar { font-family: var(--sans) }` 冗余删除，数字/代码块局部仍 mono。
- **保留的差异**（有意）：每页 `.brand span.badge` 的 accent 色（Macro=amber、Requests=trace、Journey=go、Compare=alert、Benchmarks=trace、Tool Waste=amber）——这是页面身份标记，不属"不一致"。
- **结果**：六页导航栏像素级一致。

### A5 — 下载链接 ✅

- **改动**：`common.js` 新增 `sourceBar(links)` → 返回 `<div class="source-bar">…</div>` 字符串（`⬇ Source:` + `<a download>` 链接，`·` 分隔）；六页各自注入：
  - macro-dashboard：`manifest.json` · `macro/summary.json` … `macro/context-efficiency.json` · `vmr-report.md`
  - request-browser：`requests/index.json` · `requests/failed.jsonl` · `requests/failed.md`
  - journey-viewer（详情）：`journeys/details/j-<id>.json` · `.md`；（列表）：`journeys/index.json` · `journeys/index.md`
  - journey-compare（详情）：`compares/compare-*.json` · `.md`；（列表）：`compares/index.json` · `compares/index.md`
  - benchmarks：`journeys/benchmarks.json` · `journeys/benchmarks.md`
  - tool-waste：`macro/context-efficiency.json`
- `.source-bar` 样式进六页共享 CSS 块。
- **结果**：每页底部（或标题旁）一条统一的源文件链接条。

### A6 — request-browser 英文化 ✅

- **改动**：`banner-file` / `banner-version` / `banner-missing` 三处 span + `versionBehavior === 'banner'` 的 `bannerVersionText` 模板文案译英。
- **结果**：六页文案全英文。

### A7 — file:// 横幅文案统一 ✅

- **统一文案**：`⚠️ Opened via file:// — browsers block data-slice fetches from local files. Serve over HTTP instead: 'vmr start' with 'analytics.serve: true', or 'python3 -m http.server' in the report directory.`
- **结果**：六页同句。

### A8 — missing 分支齐整 ✅

- **改动**：journey-viewer / journey-compare / benchmarks / tool-waste 的 `loadManifest`/`loadAll` 补 `else if (st === 'missing') { dom.bannerMissing.classList.remove('hidden'); }`。
- **结果**：六页版本探测三分支（ok/banner/missing）行为一致。

### A9 — 测试跟随 ✅

- `internal/dashboard/js_test.go`：
  - `TestJS_DashboardRenderSmoke` 的 `mockCmpDetail.rows[].kind` `'dur'` → `'ms'`；补 `extras` / `llm_interpretation` mock；断言 compare-detail `innerHTML` 含新章节标题（`Divergence`, `Prompt Cache`, `Evidence Provenance`, `LLM Interpretation`）。
  - `runPageSmoke` 沙箱 `window` 补 `addEventListener(){}`。
  - `versionBehavior` fixture 不变。
- `internal/dashboard/dashboard_test.go`：
  - `TestAllDashboardPages_ReadSnakeCaseFields` 的 journey-compare `want` 补 `extras`、`a_journey.id` 等新读法；`bad` 不变。
- **结果**：`go test ./internal/dashboard/... ./internal/archtest/...` 通过。

---

## 3. 决策说明

### 3.1 B5 — 不采纳"改名 / 拆页"，用 hashchange 修

**用户提案**：列表页改名 `JourneyBrowser`（区别于 `JourneyViewer` 详情），再拆一个 `journey-compare-browser` 承载对比列表。

**问题实质**：不是命名冲突，是**同一个页面既当列表又当详情，靠 `#data=` 切换，而浏览器对纯 fragment 变化不重新加载文档**（Chrome/Firefox/Safari 一致），于是 IIFE 里的 `getDataParam()` 分支不重跑。复制链接到新窗口能用，正是因为那是一次全新的文档加载。

**改与不改的区别**：
- 采纳改名/拆页：六页变八页；要改 `dashboard.go` 的 `skeletonPages`、`AssetNames` 测试、六页导航条、`§6.2` 设计文档看板清单表、`§4` 拓扑、smoke 测试。且设计文档 D13/§6.2 明确写了 "`journey-viewer.html` → `journeys/index.json`（无 `#data=` 时列候选）"——一页两职是**设计选择**，不是疏漏。推翻它要回设计文档裁决层。
- 采纳 hashchange：两页各加 1 行监听 + `common.js` 一个 helper。完全落在"一页两职"的设计内。列表→详情、详情→列表、详情→详情（切 A/B）三种 hash 变化都覆盖。

**ROI**：hashchange 方案改动面 ~5 行/页，零设计文档冲突，零测试结构变化；改名方案改动面跨 7~8 个文件 + 设计文档，只为一个纯前端加载语义问题。**hashchange 完胜。**

**残留**：`location.reload()` 会整页重取（manifest + 切片）。本地静态文件 / 局域网托管下是毫秒级，可接受。若未来看板体量增长到重载有感，再考虑把渲染逻辑抽成 `render(dataPath)` 并在 `hashchange` 里直接调（SPA 化）——那是优化，不是这次的正确性修复。

### 3.2 A2 — 章节标题为何用英文

骨架页是语言中立的单份文件（见 §5.4），其余五页的 chrome（导航、tab、表头、banner）已全英文，数据侧的本地化文本（`rows[].label`、findings 叙述、LLM 正文）由 JSON 携带语言（设计 D4/D10 的 lang-follows-everywhere）。compare 页照此办理：章节标题英文，`rows[].label` / `llm_interpretation.text` 直接取 JSON 的已本地化值。这样 `reports/`（中文数据）下页面是"英文骨架 + 中文数据"，与 macro-dashboard 现状完全一致，不引入新的不一致。

---

## 4. `reports/` 与 `reports-en/` 复核发现（G4）

> 抽查而非逐文件。重点看 HTML 渲染完整性与 Markdown 结构。

### 4.1 已在本轮修掉的

- N1/N2（compare `kind` / `delta` 口径）— A2
- N3（request-browser 中文）— A6
- N4（file:// 文案）— A7
- N5（missing 分支）— A8

### 4.2 记录为待决

- N6 → §5.1（journey-viewer 详情 parity）
- N7 → §5.2（svgLatencyPlot 编造分位）

### 4.3 复核确认无问题的

- `reports/` 与 `reports-en/` 的六个 HTML 骨架**逐字节相同**（`diff` 验证）——符合"骨架页语言中立"设计。
- `macro-dashboard.html` 五个 tab 对真数据渲染正常（node smoke 覆盖，无 `undefined`/`NaN`）。
- `benchmarks.html` / `request-browser.html`（数据读法）/ `tool-waste.html`（除宽度）渲染正常。
- `compares/index.json` ≡ `compares/*.json` 目录内容（D21 一致性），`index.md` 表格正常。
- `reports-en/compares/` 只有空 `index.{json,md}`（该目录未跑过 `-compare`）——不是 bug，是数据差异；`index.md` 的空状态引导文案正常。
- Markdown 侧（`vmr-report.md`、`journeys/details/*.md`、`compares/*.md`）结构完整，未见断表 / 坏链 / 占位符残留。（journey `.md` 的 `../../requests/...` 链接修复已在 8cf1fe8 前的提交完成，本轮复核确认现状正确。）

---

## 5. 待决问题（详细展开）

### 5.1 B1 — journey-viewer 详情视图相对 `.md` 的 parity 缺口 ✅ 已处理（第二轮，见 §8.1）

**问题描述**：`journey-viewer.html` 的详情视图（`renderJourney`）渲染：标题、6 个 stat box、findings 列表、Task/Step 时间线（step 带 tool chip + resp 正文截断 1500）。而 `journeys/details/j-<id>.md` 还有：

| `.md` 章节 | HTML 是否有 |
|---|---|
| System Prompt（tokens + 稳定性 + evidence 链接） | ❌ |
| 概览（标签、时间窗、终止方式） | 部分（无标签、无终止） |
| 行为指标（17 项表格） | 仅 6 项 stat box |
| 上下文构成演化趋势（sparkline + 起止 tok） | ❌ |
| 模型使用（per-model tokens）+ 切换记录 | ❌ |
| 决策脊柱：每步的工具**参数**与**结果正文** | 仅 chip（参数在 `title` 里，结果完全没有） |
| 工具调用时序图（mermaid） | ❌ |
| 疑似问题：每个 finding 的命中条数 + 最早 step + 逐条 evidence | 仅 code + finding + action + evidence 单行 |

**实例**：`j-lobster-20260822T080000-...md` 有 466+ 行（含完整 sys prompt、7 个 step 的工具参数与结果、mermaid 图、`unused_tool_result` finding 的逐条命中）；对应 HTML 详情视图渲染出来约是它的 1/5 信息量。

**改与不改的区别**：
- 不改：JSON（`j-<id>.json` 已按 D18 自包含，`bodies` blob 表里有全部工具结果正文）里的数据，看板用户看不到；要看细节还得回退到 `.md`。看板作为"详情入口"名不副实。
- 改：`renderJourney` 需要扩到与 `render_spine.go` + `render_md.go` 同等覆盖——工作量与 A2（compare）相当甚至更大（决策脊柱的参数智能截断、多阶 badge、mermaid 生成）。

**根因**：Phase 2 落地时 journey-viewer 详情视图是按"够用的时间线"做的最小实现，没有把 `.md` 当 spec。compare 页被用户抓到，journey-viewer 详情只是还没被逐页走查到。

**建议方案**：
1. **（推荐）** 单独排一轮，把 `renderJourney` 按 `j-<id>.md` 章节补齐，与本轮 A2 同样的做法（章节对齐 `render_*.go`，数字复用 `Fmt*`，正文取 `bodies`）。mermaid 时序图可用 `<pre class="mermaid">` —— 但看板 CSP 不允许外部脚本，mermaid 渲染需内联库（体积大）或降级为文本时序列表。倾向**降级为紧凑文本时序**（step → tool → 目标），不引 mermaid。
2. 折中：先补"无 mermaid、无 sys prompt 全文"的 80%——行为指标全表 + 上下文 sparkline（`common.js` 已有 `svgLineChart`）+ 模型使用 + 工具结果正文。
3. 不做：明确 journey-viewer 详情只是"导航 + 概览"，深读去 `.md`，并在页面上放显著的 "Full detail: [j-<id>.md]" 链接（本轮 A5 的下载链接条已部分满足）。

**ROI**：方案 1 约 1~1.5 人天，让看板真正可用于单任务排障（当前主要价值在 macro + compare）。方案 3 零成本但把看板的 journey 维度定位为"索引"。**推荐方案 2**：半天，拿到 sparkline + 全指标 + 工具结果这三个高频需求，mermaid / sys prompt 全文留给 `.md`。

### 5.2 B2 — `svgLatencyPlot` 在缺分位数据时编造 ✅ 已处理（第二轮，见 §8.2）

**问题描述**：`common.js:396-407`：
```js
const p50 = Number(d.dur_ms_p50 || d.p50_ms || (d.dur_ms ? d.dur_ms * 0.7 : 0));
const p90 = Number(d.dur_ms_p95 || d.p90_ms || d.dur_ms || 0);
const p99 = Number(d.dur_ms_max || d.p99_ms || (d.dur_ms ? d.dur_ms * 1.3 : 0));
```
当端点只有 `dur_ms`（均值）没有分位数时，用 `×0.7` / `×1.3` 硬造 P50/P99。

**改与不改的区别**：
- 不改：分位图在缺数据端点上画出三个**看起来精确、实则捏造**的点，读者会当真。违反 CLAUDE.md "never fake certainty" 与设计文档 F10 类的诚实原则。
- 改：缺 `dur_ms_p50`/`dur_ms_p95` 的行改为只画一个 `dur_ms`（均值）点并明确标注 "mean (no percentiles)"，或整行灰显 "percentile data unavailable"。

**根因**：`svgLatencyPlot` 早期为了"总能画出点东西"加的 fallback，没意识到分位数捏造比留白更糟。

**建议方案**：删掉 `×0.7`/`×1.3`；`reliability.json` 的端点行若无 `dur_ms_p50` 且无 `dur_ms_p95`，该行只标一个均值点（不同 marker，图例加 "○ mean"），或跳过并在图下列一句 "N endpoints have no percentile data"。

**ROI**：~1 小时。低成本、消除一个真实的误导面。可并入 5.1 那轮（都是 reliability/journey 展示层）。**建议单独一个小 commit 顺手做掉**——严格说这条其实够"10 分把握"，本轮没做只是因为它在 `common.js` 且牵动 macro-dashboard 的 reliability tab，想与 5.1 一起验证。若需要可现在就做。

### 5.3 B3 — 下载链接在配了 `api_keys` 时会 401

**问题描述**：A5 加的 `<a href="macro/summary.json" download>` 是普通导航，不带 `Authorization` 头。当 vmr 以 `analytics.serve: true` + `api_keys` 托管时，`/reports/*.json` 走鉴权（设计 D9/§6.5），点击 → 401。

**改与不改的区别**：
- 现状（A5）：`python -m http.server`（用户当前用法、无鉴权）下完美；vmr 鉴权托管下 `.md`/`.json` 链接 401，`.html` 骨架不受影响。
- 彻底修：下载改为 JS `fetch`（带 `Auth.getHeaders()`）→ `Blob` → `URL.createObjectURL` → 触发下载。多 ~15 行/页，且 `Blob` 下载的文件名要手动设。

**根因**：静态 `<a>` 与 Bearer 鉴权天然不兼容。

**建议方案**：本轮先上静态链接（覆盖主场景）。若后续要支持鉴权托管下载，在 `common.js` 加一个 `downloadSlice(relPath)`（fetch + blob），`sourceBar` 的链接 `onclick` 调它、`href` 保留作 fallback / 复制用。

**ROI**：静态版零增量成本、覆盖 90% 用法。fetch+blob 版 ~半小时，等有人真在鉴权托管下用看板再说。

### 5.4 B4 — 骨架页语言中立 vs `reports/` 是中文目录

**问题描述**：`dashboard.WriteSkeletons(dir)` 只吃目录、不吃语言，同一份英文 HTML 同时写进 `reports/`（`-lang zh` 产物）和 `reports-en/`。于是中文用户在 `reports/` 看到的是英文导航 / tab / 表头 / banner（数据是中文）。

**改与不改的区别**：
- 不改：与 macro-dashboard 现状一致（英文 chrome + 中文数据），用户已能用；`request-browser` 英文化（A6）后至少内部自洽。
- 改：`WriteSkeletons(dir, lang)`，六页 chrome 文案抽到一个 JS 字典（`common.js` 里按 `lang` 选），或维护两套 assets。改动面大（六页所有字面量 + Go 签名 + 三处调用点 + 测试）。

**根因**：Phase 2 骨架页按"英文优先、数据带语言"实现，没做 chrome 的 i18n。设计文档 §6 未明确要求骨架页双语。

**建议方案**：短期不做，接受"英文 chrome + 本地化数据"。中期若要做，正解是 `common.js` 持一个 `UI_TEXT[lang]` 字典 + `WriteSkeletons` 按 `manifest.json` 的 `lang` 或 `-o` 目录旁的语言标记注入一个 `<script>window.__LANG='zh'</script>`，页面启动读它切 chrome 文案。**这是设计文档 §6 该补的一条决策（骨架页 chrome 是否 i18n），建议回写 `analyze_architecture_redesign` 或 KNOWN_ISSUES**。

**ROI**：不做=0 成本、观感瑕疵（中文用户看英文导航）。做=约 1 人天。优先级低于 5.1。

### 5.5 B6 — `request-browser.html` 行渲染未转义（N8） ✅ 已处理（第二轮，见 §8.3）

**问题描述**：`render()` 把 `r.model` / `r.endpoint` / `r.client_key` / `sessionTitle` 直接拼进 `innerHTML` 与 `title="..."` 属性。session title 来自对话首条 user 消息摘要，带 `"` 会截断 `title` 属性、带 `<` 会被当标签解析。

**改与不改的区别**：不改=特定对话标题下 title 提示错乱 / 潜在 XSS（数据来自本地审计日志，威胁模型低但非零）；改=该页所有 `${...}` 插值套 `esc()`（common.js 已导出），约 10 处、纯机械。

**根因**：Phase 2 该页早于 `esc()` helper 落地。

**建议方案**：下一次碰这个文件时顺手全量套 `esc()`（journey-viewer / journey-compare 本轮已做）。单独排一个小 commit 也可，够"10 分把握"，本轮没做只因不在用户点名范围内、且要动渲染主循环。**ROI**：~20 分钟，消除一个真实的属性截断 bug。

---

## 6. 验证与收尾

- [x] `go build -o vmr ./cmd/vmr` — OK
- [x] `go test ./internal/dashboard/... ./internal/archtest/... ./cmd/vmr/...` — OK
- [x] `go test ./...` — OK（无失败）
- [x] `gofmt -l internal/dashboard/` 干净；`go vet ./internal/dashboard/` 干净
- [x] node 离线渲染真数据（`reports/compares/compare-*.json` 等）逐页断言：
  - [x] journey-compare 13 个章节全部渲染，无 `undefined`/`NaN`；`模型时间` 行 = `1947.4s | 1625.0s | -17%`（与 `.md` 逐字一致）；`0.09×` / `0.3×` / `-40%` delta 口径与 `.md` 一致；成本行 `A $0.30+ · B —` 与 `.md` 一致；逐轮缓存曲线 `R1 20% → R2 87% → …` 与 `.md` 一致
  - [x] journey-viewer 详情 / 候选列表、macro-dashboard、request-browser（50 行）、benchmarks、tool-waste 均渲染干净，Source 链接条注入成功
- [x] `./vmr analyze -render-only -o reports` / `-o reports-en` 重写骨架；两目录六页骨架逐字节相同；无外部依赖引用
- [x] 六页 chrome `max-width` 全部 1280px
- [x] `CHANGELOG.md` `[Unreleased] > Fixed` 加条目
- [ ] 用户侧 `python3 -m http.server` 浏览器目视复核（交回用户）

**本轮结论**：U1（hashchange）、U2（compare 渲染 + N1/N2 口径）、U3（宽度）、G1（导航统一）、G2（1280）、G3（下载链接）全部落地并经离线渲染验证。G4 复核发现的 N3/N4/N5 一并修掉；N6/N7/N8 记录为待决（§5）。

---

## 7. 变更文件清单

| 文件 | 改动 |
|---|---|
| `internal/dashboard/assets/common.js` | `wireHashReload()`、`sourceBar()`、`esc()`（若放公共）、导出 |
| `internal/dashboard/assets/journey-viewer.html` | hashchange、宽度/CSS 统一、Source 链接、missing 分支、file:// 文案 |
| `internal/dashboard/assets/journey-compare.html` | hashchange、`renderCompare` 补齐、`fmtMetric`/`fmtDelta` 重写、宽度/CSS、Source、missing、file:// |
| `internal/dashboard/assets/macro-dashboard.html` | 宽度 1360→1280、CSS 统一、Source、file:// 文案 |
| `internal/dashboard/assets/request-browser.html` | 宽度 1400→1280、CSS 统一、Source、英文化、file:// 文案 |
| `internal/dashboard/assets/benchmarks.html` | CSS 统一、Source、missing 分支、file:// 文案 |
| `internal/dashboard/assets/tool-waste.html` | 宽度 800→1280、body 字体、CSS 统一、Source、missing 分支、file:// 文案 |
| `internal/dashboard/js_test.go` | compare mock `kind` 修正 + 新章节断言 + 沙箱 `addEventListener` |
| `internal/dashboard/dashboard_test.go` | journey-compare snake_case `want` 调整 |
| `CHANGELOG.md` | `[Unreleased] > Fixed` |
| `docs/KNOWN_ISSUES.md` | 骨架页 chrome 英文单版的裁决登记 |
| `docs/future-strategy/analyze_dashboard_review_fixes_sonnet-5.md` | 本文件 |

---

## 8. 第二轮执行记录（2026-09-08，§5.1 / §5.2 / §5.5）

### 8.1 journey-viewer 详情视图补齐（原 §5.1，采纳"方案 2：半天 80%"）✅

**做了什么**：`renderJourney` 从"标题 + 6 stat box + findings 单行 + step chip"扩到与 `j-<id>.md` 的前半 + 决策脊柱对齐：

| 新增章节 | 数据源 | 口径对齐 |
|---|---|---|
| **Overview** | steps 首尾 + `stitch_edge`/`edit` + `compaction_count`/`duplicate_action_rate` | `viewmodel_build.go` 的 `vmTimelineNodes` + `structuralTags`（阈值 `toolCallCount≥10` / `dupRate≥0.2` / `compaction>0`）+ `OverviewFailedStepsLine` + `OverviewCostLine` |
| **Suspected Issues**（原 findings 升级） | `findings[]` 按 `code` 分组 | `FindingGroupTitle`：`code · N hits (earliest Step X)` + 每条 `Step N · finding` / evidence / action |
| **Behavior Indicators**（14 行全表） | `metrics.*` | `journeyMetrics` 顺序 + `fmtJourney*`：`net_working_ms` 起头、秒 1 位小数、`pct0`、`model_to_tool_ratio` 在 `agent_exec_ms==0` 时 `—`、`error_recovery_count` 在非 Anthropic + 0 时 `n/a` |
| **Context Token Trajectory** | `metrics.context_composition_curve` | `svgLineChart`（逐 step 四类 token 求和）＋ 同时给 `viewmodel_build.go` 的 ASCII sparkline（`▂▃▄▅▆▇█`）＋ caption `(start X tok → end Y tok)` |
| **Model Usage** + **Model Switches** | `metrics.model_usage[]` / `metrics.model_switches[]` | `vmModelUsage`：per-model steps/in/cached/out 表 + 每次切换 `Step N: from → to · cache A% → B% · on a failover step` |
| **Decision Spine · Tasks & Steps**（step 大改） | `tool_calls[].args_ref` / `result.{ref,match,is_error}` + `bodies` blob 表 | `viewmodel_spine.go`：step role emoji（🔧/💬/🔄/⚠️/🧹/👀）、`si>0 && instruction` 时显示指令、compaction/stitch transition 行、每个工具调用展开 `🔧 name` + args（≤160 内联，超出 `<details>`）+ 配对结果（`↩️`/`❌` + `(matched by position — ID unmatched)` badge，`<details><pre>`） |
| **Final Deliverable** | `j.deliverable` | `vmFinalDeliverable`：`Step N · tool` + excerpt 折叠 |

**明确留给 `.md`**（页面底部一行注明）：System Prompt eras（+ evidence 链接）、工具调用时序图 ASCII 网格。理由：前者要 `../../requests/evidence/` 跨目录链 + era 分组，后者是等宽字符矩阵，两者在 `.md` 里已完整，HTML 复刻 ROI 低（D5：details/evidence 是另一类产物）。

**转义**：全部对话派生串（step instruction、tool args/results、findings、model 名、deliverable excerpt）过 `esc()`。

**离线渲染验证**（真数据 `j-pimini-20260825T165640-...json`，118 工具调用）：7 章节全渲染，无 `undefined`/`NaN`，标签平衡（div 663/663、details 193/193、table 2/2）；`Net Working Time` 单元格 = `2816.4s`（与 `.md` 逐字一致）；模型切换行 `Step 71: … cache 99% → 1%`（与 `.md` 一致）；118 工具调用全部配上结果。

**一个实现坑**（已修）：新加的 `const secs` / `JOURNEY_METRICS` / 助手函数原本放在 `renderJourney` 定义前、`#data=` dispatch 之后——但 dispatch 里 `await` 完就调 `renderJourney`，此时执行流还没走到那些 `const` 声明行（TDZ），首次渲染必抛 `Cannot access 'secs' before initialization`。修法：把 dispatch 块整体挪到 IIFE 末尾（所有 helper + `renderJourney` 定义之后），结构也更清晰（先定义、后派发）。

### 8.2 svgLatencyPlot 不再编造分位数（原 §5.2）✅

**做了什么**：
- 删掉 `d.dur_ms ? d.dur_ms * 0.7` / `* 1.3` 的算术捏造。
- 改用 `reliability.json` 真实字段：`dur_ms_p50` / `dur_ms_p95` / `dur_ms_max`（此前图例写 "P50 / P90 / P99"，数据实际是 p50/p95/max —— 是**双重错标**，一并纠正）。
- 无 `dur_ms_p50` 的端点不再进图，改在图下列一行 `N endpoint(s) have no recorded percentile data (not plotted)`。
- `dur_low_n` 端点标签加 `(low-n)` 后缀（对齐 `.md` 的 `⚠️low-n`）。
- 连带修 `macro-dashboard.html` 端点表的表头 `P90` → `P95`（`e.dur_ms_p95` 一直是 P95，只有列名错）与图标题。

**验证**：真数据（49 端点）渲染，图例 `P50 / P95 / Max`，标题 `Endpoint Duration Percentiles (P50 / P95 / Max ms)`，无 `* 0.7` 字样，端点表 `P95` 表头、无 `P90`。

### 8.3 request-browser.html 行渲染转义（原 §5.5 / N8）✅

**做了什么**：`render()` 的行模板、`buildFacets` 的 `fill()` 下拉选项——所有 `${r.model}` / `${r.endpoint}`（×2：文本 + `title=""`）/ `${r.client_key}`（×2）/ `${r.error_class}` / `${sessionTitle}` / `${jId}` / facet `value`/文本——全部套 `esc()`；`detail_file` href 走 `encodeURI`，`jId` href 走 `encodeURIComponent`。带 `"` 的 session title 不再截断 `title` 属性，带 `<` 的模型名不再当标签。

**验证**：真数据（1521 行，50/页）渲染无 `undefined`/`NaN`，行数正确。

### 8.4 第二轮验证与产物

- `go build` ✅ · `go test ./internal/dashboard/... ./internal/archtest/...` ✅ · `go test ./...` ✅ · `gofmt`/`vet` 干净
- `js_test.go`：journey-viewer detail mock 扩到覆盖 `context_composition_curve` / `model_usage` / `tool_calls[].result` + `bodies` blob，断言新增 `Behavior Indicators` / `Model Usage` / `Decision Spine` / 结果正文 / `Final Deliverable`；`dashboard_test.go` 的 journey-viewer snake_case `want` 按新读法重写（`m.agent_exec_ms`、`metrics.context_composition_curve`、`tc.args_ref`、`res.ref` 等）。
- `./vmr analyze -render-only` 已刷新 `reports/` 与 `reports-en/`；两目录六页骨架逐字节相同。
- 仍待决：§5.3（下载链接鉴权 401）、§5.4（骨架页 chrome i18n，已在 KNOWN_ISSUES 登记）。
