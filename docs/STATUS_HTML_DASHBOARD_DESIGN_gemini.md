<!-- Ver 2026-08-23 17:33, by Gemini -->

# VMR `/status.html` 网页端运行态可视化看板设计方案 (Design Document)

## 1. 任务背景与核心诉求 (Background & Objectives)

### 1.1 演进背景
在完成 VMR `GET /status` 端点迁移、局域网访问授权以及全景运行态数据结构（涵盖实例环境、配置新鲜度、系统资源、并发闸、流量 Telemetry、端点健康状态机、额度配额、审计与缓存）全量升级后，`/status` 已经能够提供高信噪比、零锁开销的结构化 JSON 数据。

然而，纯 JSON 数据在运维人员或开发者通过浏览器直接访问时，存在排版紧凑、层级嵌套深、视觉信噪比低等痛点，尤其在排查多端点故障转移、额度水位以及并发排队时不够直观。

### 1.2 核心目标
1. **新增 `/status.html` 可视化端点**：提供开箱即用、零额外依赖、自包含的单页面 Web 运行态监控看板（Dashboard）。
2. **纯客户端渲染 (CSR, Client-Side Rendering)**：服务端仅负责输出静态 HTML 模板外壳，所有图表与数据均由前端 Vanilla JS 动态拉取 `GET /status` 并填充 DOM。
3. **零外部网络依赖 (Air-gapped / Zero External CDN)**：页面内联包含所有 HTML、CSS 与 JS，绝不从外部 CDN 加载任何第三方库（无 Vue、React、Tailwind、Bootstrap、Chart.js 外部请求），在无网/离线/内网 VPC 环境下 100% 毫秒级秒开。
4. **简洁安全的纯弹窗鉴权 (Strict Modal Auth Flow)**：
   - 自动识别服务端是否启用了 `api_keys`；
   - 绝不通过 URL Query 参数传递 Key（防止肩窥、历史记录残留与 URL 泄露风险）；
   - 客户端仅基于本地 `localStorage` 缓存凭证；若未授权（401）则弹出优雅交互式密码弹窗，输入后持久化并拉取数据；
   - 顶部提供“Key 管理”按钮，展示脱敏尾缀（`sk-***abcd`），支持一键清空/切换。
5. **实时轮询与交互控制**：固定 5 分钟一次的自动刷新（不可调频、无手动刷新控件——需要立即更新时直接刷新页面），状态高亮变色。

---

## 2. 第一性原理技术裁决与方案论证 (First Principles & Rationale)

### 2.1 为什么坚持纯客户端渲染 (CSR) 而非服务端模板渲染 (SSR)？

| 维度 | 方案 A: 服务端渲染 (SSR, Go `html/template`) | 方案 B: 纯客户端渲染 (CSR, Static HTML + Fetch) [推荐] |
| :--- | :--- | :--- |
| **架构耦合度** | 服务端代码需引入 HTML 模板解析、字符串拼接与模板生命周期维护，侵入 Core 服务端。 | **完全解耦**：服务端仅以标准库 `//go:embed` 暴露一个静态字节切片，UI 逻辑全部在浏览器运行。 |
| **服务器开销** | 每次刷新都会消耗 CPU 跑模板引擎渲染，增加服务端负担。 | **零服务端计算开销**：服务端只提供高并发、高性能的 JSON 数据。 |
| **实时更新体验** | 页面刷新需要全量 Reload，屏幕白屏闪烁；若做定时刷新体验极差。 | **无缝局部刷新**：JS 异步 `fetch('/status')`，通过 DOM 重绘平滑更新数值与倒计时，无白屏。 |
| **离线与测试** | 页面逻辑与 Go 运行时强绑定，前端调优必须重启 Go 进程。 | **前后端独立测试**：可直接在本地用静态 Mock JSON 调试 HTML/CSS 样式，开发调优极其敏捷。 |

**裁决结论**：采纳 **方案 B (纯客户端渲染)**。

---

### 2.2 为什么坚持零外部依赖 (Zero External Dependencies)？

* **现实痛点**：VMR 经常部署在内网网段、私有云 VPC、边缘机房甚至完全断网的离线环境。任何指向 `unpkg.com`、`cdnjs.cloudflare.com` 或 `jsdelivr.net` 的外部链接都会导致静态页面阻塞数秒乃至最终白屏崩溃。
* **技术可行性**：现代浏览器对 CSS Grid、Flexbox、CSS 变量、ES6+ 原生支持极好。使用约 15KB 的原生 CSS + 15KB 的原生 JS，足以构建出媲美 Tailwind/Vue 的现代化暗黑/明亮自适应看板。
* **零维护负担**：无需 npm / webpack / vite 等复杂的前端打包工具链，单一 `status.html` 源文件直接嵌入 Go 二进制，没有任何包依赖老化或安全漏洞扫描问题。

**裁决结论**：全内联纯原生 HTML + CSS + JS，**外部网络依赖数为 0**。

---

### 2.3 鉴权流转与安全交互设计 (Authentication & Security)

为了杜绝通过 URL 地址栏传递 Key 带来的历史记录泄露与肩窥安全隐患，我们采用“**纯弹窗 + 本地缓存**”的极简安全流转：

```
                           用户浏览器访问 GET /status.html
                                        │
                                        ▼
                        服务端直接返回静态 HTML 模板外壳
                    (HTML 外壳不含任何业务数据，免密公开)
                                        │
                                        ▼
                        前端 JS 启动，检查本地凭证:
                      localStorage.getItem('vmr_status_key')
                                        │
                   ┌────────────────────┴────────────────────┐
                   ▼                                         ▼
             【未找到 Key】                             【找到 Key】
                   │                                         │
                   ▼                                         ▼
         尝试裸调 GET /status                       携带 Header: Authorization: Bearer <key>
                   │                                调 GET /status
                   ▼                                         │
        ┌──────────┴──────────┐                              ▼
        ▼                     ▼                    ┌─────────┴─────────┐
    [返回 200 OK]        [返回 401 Unauthorized]  ▼                   ▼
(无 api_keys 模式)      (启用了 api_keys)       [返回 200 OK]   [返回 401/403]
        │                     │                 (鉴权成功)      (Key 已失效/错误)
        ▼                     ▼                      │                 │
    渲染看板            弹出交互式输入 Key 弹窗       │                 ▼
                              │                      │       清除无效 Key，显示报错
                              ▼                      │       重新弹出输入 Key 弹窗
                       用户输入 API Key              │                 ▲
                              │                      │                 │
                              └──────────────────────┴─────────────────┘
                                        │
                                        ▼
                        1. 写入 localStorage 记住凭证
                        2. 关闭弹窗，成功渲染 Dashboard
```

#### 安全与隐私考量：
1. **零 URL 暴露**：地址栏永远干净纯粹（如 `http://127.0.0.1:8800/status.html`），防止在共享屏幕、浏览器历史记录或书签中泄露凭证。
2. **凭证脱敏与随时重置**：右上角提供“Key 管理”按钮，展示当前 Key 的脱敏尾缀（如 `sk-***abcd`），并支持“切换 Key / 退出登录”一键清除 `localStorage`。

---

## 3. 看板布局结构与视觉设计规范 (UI/UX Layout Architecture)

看板整体采用现代 **暗黑极客风 (Dark Modern Theme)**，采用高信噪比卡片网格布局，信息层级由上至下、由宏观到微观依次展开。

```
+--------------------------------------------------------------------------------------------------+
|  VMR STATUS DASHBOARD    [ v4.0.0-b5749f7 ] [ pid:38491 ] [ darwin/arm64 ]     [ 🔑 Key ]          |
+--------------------------------------------------------------------------------------------------+
|  ⚠️ WARNING: Config file modified 14:35:00 (stale) - Reload rejected: yaml line 42 syntax error     |
+--------------------------------------------------------------------------------------------------+
|  [ ⚡ Concurrency ]       [ 💻 System & Host ]      [ 📊 Traffic Telemetry ]  [ 🪙 Token Breakdown ] |
|  In-Flight: 3 / 64       Heap: 17.6MB / 46.0MB     Requests: 1,420 total     Fresh In: 5.29M        |
|  Waiting: 0              Goroutines: 38            OK: 1,395 | Err: 10       Cache Hit: 3.25M (38%) |
|  [||||||............]    Disk Free: 477.6GB        Cancel: 15                Out: 1.24M / Reas: 845K|
+--------------------------------------------------------------------------------------------------+
|  QUOTA & RATE LIMIT BUDGETS                                                                      |
|  ┌─────────────────────────────────────────────────────────────────────────────────────────────┐ |
|  │ PROVIDER   METRIC/WINDOW  ROLE    USAGE / LIMIT (PCT)         HEADROOM  RESETS IN  SCOPE        │ |
|  │ volcengine tokens/1mo     bucket  [||||||||||.......] 69.0%   0.31      14d 6h     [deepseek-v3]│ |
|  │ deepseek   cost/1d        gate    [|||||||||||||....] 83.0%   0.17      8h 22m     all models   │ |
|  └─────────────────────────────────────────────────────────────────────────────────────────────┘ |
+--------------------------------------------------------------------------------------------------+
|  VIRTUAL MODELS & ENDPOINT HEALTH                                                                |
|  ┌─────────────────────────────────────────────────────────────────────────────────────────────┐ |
|  │ claude-3-5-sonnet [anthropic]                                          2 endpoints serving  │ |
|  │  P0  anthropic/direct/claude-3-5-sonnet-20241022               [ 🟢 OK ]       fails=0      │ |
|  │  P1  bedrock/us-east-1/claude-3-5-sonnet-v2                     [ 🟡 COOLDOWN ] fails=2 (12s)│ |
|  └─────────────────────────────────────────────────────────────────────────────────────────────┘ |
|  ┌─────────────────────────────────────────────────────────────────────────────────────────────┐ |
|  │ deepseek-chat [openai]                                                 3 endpoints serving  │ |
|  │  P0  deepseek/official/deepseek-chat                           [ 🟢 OK ]       fails=0      │ |
|  │  P1  volcengine/ark/deepseek-v3                                [ 🔵 HALF-OPEN ] probing...  │ |
|  │  P2  siliconflow/direct/deepseek-ai/DeepSeek-V3                [ 🟢 OK ]       fails=0      │ |
|  └─────────────────────────────────────────────────────────────────────────────────────────────┘ |
+--------------------------------------------------------------------------------------------------+
|  STORAGE & AUXILIARY                                                                             |
|  Audit Log: Active 1.4MB | Total 12.4MB (30d retention)        Image Cache: 11.9MB / 50.0MB      |
+--------------------------------------------------------------------------------------------------+
```

---

### 3.1 核心视觉组件与分层细节

#### Section 1: 顶部控制栏 (Header & App Bar)
* **实例徽标**：`VMR Status Dashboard`，附带版本标签、进程 PID、Go 版本、操作系统与架构徽章；
* **环境路径 Tooltip**：鼠标悬停在标题或徽标上，浮层展示当前工作目录 `cwd` 与二进制文件路径 `executable`；
* **运行时间 (Uptime)**：实时显示格式化运行时间（如 `3h 34m 5s`），由前端根据 `started_at` 本地计时器每秒自动递增刷新；
* **刷新策略 (Refresh Policy)**：固定 5 分钟自动刷新一次，不可调频；不提供手动刷新控件——需要立即更新时用户直接刷新页面即可（2026-08-23 决策，替代初稿的可调轮询方案）；
* **鉴权状态 (Auth Indicator)**：
  - 绿锁图标：`Public`（免密模式）；
  - 蓝锁图标：`Authorized (sk-***abcd)`，点击可弹出修改/清除弹窗。

#### Section 2: 告警与异常横幅 (Alert Banners, 条件渲染)
* **配置过期横幅 (Config Stale Alert)**：当 `instance.config.stale == true` 时亮起黄/红色警告栏，提示配置文件已修改但尚未热重载生效；
* **重载失败横幅 (Reload Rejected Alert)**：当最近一次热重载失败时，直接展示被拒绝的错误详情（如 YAML 行语法错误），避免运维盲区；
* **静态检查告警 (Config Issues)**：当 `instance.config.issues` 存在项时，逐条展示 Warning 告警（如非回环暴露、探针超时过长等）。

#### Section 3: 核心指标卡片矩阵 (Metrics Cards Grid, 4 列网格)
1. **并发闸门卡片 (Concurrency)**：
   - 活跃槽位指示：`in_flight / limit`，附带动态百分比进度条（绿->黄->红）；
   - 排队等待数：`waiting` 队列深度（高亮指示背压）。
2. **系统资源卡片 (System Resources)**：
   - 活跃堆内存：`memory.heap_alloc` (如 `17.6 MB`)；
   - 系统总分配内存：`memory.sys` (如 `46.0 MB`)；
   - Goroutines 协程总数；
   - 数据分区磁盘剩余空间：`disk.free_space` (如 `477.6 GB`)。
3. **请求吞吐与状态卡片 (Traffic Requests)**：
   - 总请求数：`requests.total`；
   - 状态分量标签：`OK: 1,395` (绿色), `Canceled: 15` (灰色), `Error: 10` (红色)；
   - 协议分布：`OpenAI: 820` | `Anthropic: 600`。
4. **Token 计量与缓存效益卡片 (Tokens & Cache Efficiency)**：
   - Fresh In、Cache Read（计算缓存命中率 %）、Cache Write、Reasoning、Out 5 维数据；
   - 会话黏性活跃数：`sticky.entries`。
5. **存储与辅助卡片 (Storage & Audit, 底部收拢)**：
   - 审计日志：当天活跃文件大小、目录总大小、留存天数；
   - 图片缓存：当前已用缓存大小 vs 50MB 上限进度条。

#### Section 4: 虚拟模型与端点健康监控 (Virtual Models & Endpoints)
* 针对每一个虚拟模型（如 `claude-3-5-sonnet [anthropic]`）生成一个独立的模型卡片；
* 卡片内部为端点列表（Table/List），展示：
  - **优先级 (Priority)**：`P0`, `P1`, `P2`；
  - **端点名称 (Endpoint Name)**：如 `volcengine/ark/deepseek-v3`；
  - **健康状态徽章 (Status Badge)**：
    - 🟢 `OK / Serving`：正常服务中；
    - 🟡 `COOLDOWN (12s)`：触发故障降级冷却，显示实时倒计时、连续失败次数 `fails=2` 及最近一次错误分类；
    - 🔵 `HALF-OPEN`：冷却已到期、等待探活探测；
    - 🟣 `PROBING`：当前正有后台探测协程占用单飞名额；
  - **最近错误信息 (Last Error)**：如 `status=429 (rate_limited)`。

#### Section 5: 配额与预算看板 (Quota & Budget Cards, 条件渲染)
* 若当前实例未配置 quota，本区域自动折叠；
* 若已配置，按 `Provider` 网格化展示配额卡片：
  - 头部：Provider 名称、计量维度（`tokens` / `requests` / `cost`）、时间窗口（`1d` / `1mo`）及角色（`bucket` 桶 或 `gate` 闸）；
  - 进度条：`used / amount` 消耗进度条与百分比（支持根据余量变色：<70% 绿色，70-90% 黄色，>90% 红色）；
  - 余量分数与重置时间：`headroom` 分数、`resets in ...` 倒计时；
  - 适用范围与权重：`scope: models=[...]`、`token_weights` / `model_multipliers` 详细参数。

---

## 4. 服务端嵌入与路由集成方案 (Server Integration)

### 4.1 静态资源嵌入机制 (`//go:embed`)

在 `internal/server` 包中创建 `status.html`，并通过 Go 1.16+ 原生 `embed` 特性编译进可执行文件：

```go
// internal/server/status_page.go
package server

import (
	_ "embed"
	"net/http"
)

//go:embed status.html
var statusHTMLPage []byte

// statusPageHandler serves the self-contained static HTML dashboard.
func (s *Server) statusPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(statusHTMLPage)
}
```

### 4.2 路由挂载与鉴权边界

在 `internal/server/server.go` 的 `Handler()` 中注册路由：

```go
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.chatHandler("openai"))
	mux.HandleFunc("POST /v1/messages", s.chatHandler("anthropic"))
	mux.HandleFunc("POST /v1/responses", s.chatHandler("openai-responses"))
	mux.HandleFunc("GET /v1/models", s.auth(s.models))
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /status", s.auth(s.adminStatus))
	mux.HandleFunc("GET /status.html", s.statusPageHandler) // 静态页面外壳免密直出，数据请求由客户端 fetch('/status') 鉴权
	return mux
}
```

---

## 5. 前端单文件 HTML/CSS/JS 架构设计 (Frontend Architecture)

### 5.1 模块结构划分
`internal/server/status.html` 组织为三大内联区块：
1. `<style>`：暗色系 CSS 变量定义、CSS Grid/Flexbox 响应式排版、状态徽章、进度条动画与模态弹窗样式；
2. `<body>`：HTML 骨架模板，包含 Header、Alert 容器、Metrics 容器、Models 容器、Quota 容器及 Auth Modal 模态框；
3. `<script>`：纯原生 Vanilla JS（无任何外部依赖）：
   - `AuthManager`：管理 `localStorage`、弹窗触发与凭证组装；
   - `DataLoader`：执行 `fetch('/status')`，捕获 401 错误，管理加载状态与自动轮询定时器；
   - `DOMRenderer`：解析 JSON 数据，动态构建与更新 DOM 节点；
   - `TimerManager`：管理运行时间与冷却时间秒级本地倒计时。

### 5.2 核心 JS 逻辑伪代码设计

```javascript
// --- 凭证管理模块 ---
const Auth = {
  getKey() {
    return localStorage.getItem('vmr_status_key') || '';
  },
  setKey(k) { localStorage.setItem('vmr_status_key', k); },
  clearKey() { localStorage.removeItem('vmr_status_key'); }
};

// --- 数据拉取模块 ---
async function fetchStatus() {
  const key = Auth.getKey();
  const headers = {};
  if (key) {
    headers['Authorization'] = 'Bearer ' + key;
  }

  try {
    const resp = await fetch('/status', { headers });
    if (resp.status === 401) {
      showAuthModal('Authentication Required. Please enter API Key:');
      return;
    }
    if (!resp.ok) {
      throw new Error(`Server error: ${resp.status} ${resp.statusText}`);
    }
    const data = await resp.json();
    hideAuthModal();
    renderDashboard(data);
  } catch (err) {
    showErrorBanner(err.message);
  }
}
```

---

## 6. 验证计划与测试矩阵 (Verification Plan)

| 测试场景 | 验证操作 | 预期行为 |
| :--- | :--- | :--- |
| **1. 免密实例访问** | 服务端不配 `api_keys`，浏览器直接访问 `http://127.0.0.1:8800/status.html` | 页面直接加载，无弹窗，各区块数据实时完整呈现。 |
| **2. 有密实例初次访问** | 服务端配置 `api_keys`，清除缓存后访问 `/status.html` | 页面立即弹出输入 Key 模态框，输入正确 Key 后成功展示数据并存入 `localStorage`。 |
| **3. 错误密码拦截** | 在弹窗中输入错误 Key 点击连接 | 弹窗提示“Invalid API key, please try again”，保持拦截，不暴露任何数据。 |
| **4. 离线/无外网环境验证** | 断开网络连接或在 DevTools Network 中模拟 Offline / 阻断外部域名 | 页面加载耗时 <10ms，零外部请求失败，UI 渲染完好。 |
| **5. 自动轮询与端点倒计时** | 制造一个冷却中的端点，等待自动刷新周期 | 冷却时间秒级倒计时平滑递减，5 分钟到期后静默刷新并更新状态。 |
| **6. 移动端与响应式排版** | 调整浏览器窗口宽度为 375px (手机视口) | 4 列卡片自适应折叠为 1 列，表格支持横向滚动，排版无溢出。 |

---

## 7. 详细实施行动计划 (Detailed Action Plan)

```
[Phase 1] 静态页面模板构建 (Single-file HTML/CSS/JS Dashboard)
  ├── 1. 创建 internal/server/status.html:
  │      ├── 编写现代化暗黑系极简 CSS (CSS Grid, Flexbox, 自定义变量, 响应式断点)
  │      ├── 编写完整 HTML 容器骨架 (Header, Banners, Metrics, Models, Quota, Storage, Auth Modal)
  │      └── 编写原生 Vanilla JS 逻辑 (AuthManager, DataLoader, DOMRenderer, TimerManager, Polling)
  └── 2. 独立浏览器预览与 DOM 渲染准确性核验 (确保零外部网络请求)

[Phase 2] 服务端静态资源嵌入与路由集成 (Go Server Embed & Route Handler)
  ├── 1. 创建 internal/server/status_page.go:
  │      ├── 引入 //go:embed status.html
  │      └── 实现 statusPageHandler (设置 Content-Type: text/html, Cache-Control: no-cache)
  ├── 2. 在 internal/server/server.go 的 Handler() 中挂载 GET /status.html (免密直出静态外壳)
  └── 3. 编写 internal/server/status_page_test.go 单元测试 (断言状态码 200, Content-Type, HTML 包含核心容器)

[Phase 3] 文档全面同步 (Documentation Updates)
  ├── 1. 更新 README.md 与 README.zh.md (在端点列表中追加 GET /status.html 说明)
  ├── 2. 更新 docs/UserGuide.md 与 docs/UserGuide.zh.md (在端点汇总表中记录 /status.html 及其浏览器交互)
  └── 3. 更新 CHANGELOG.md (记录新增 /status.html 静态可视化看板)

[Phase 4] 架构守卫与全仓回归验证 (Verification & Arch Guard)
  ├── 1. 执行 go test -v ./internal/archtest 验证架构行数与引用合规
  ├── 2. 执行 go test -count=1 ./... 验证全仓 38 个模块测试 100% 通过
  └── 3. 跨平台交叉编译校验 (Darwin / Linux / Windows)

[Phase 5] 进度追踪与总结归档 (Tracking & Execution Summary)
  └── 更新 STATUS_HTML_DASHBOARD_DESIGN_gemini.md 追加执行过程跟踪与最终执行总结
```

---

## 8. 过程进度跟踪报告 (Progress Tracking Report)

| 阶段 / 任务项 | 涉及模块 / 文件 | 状态 | 验证结果 |
| :--- | :--- | :---: | :--- |
| **Phase 1: 静态页面模板构建** | `internal/server/status.html` | **已完成** | 纯自包含暗黑单页模板，原生 CSS Grid/Flexbox，Vanilla JS 零外部 CDN 请求 |
| **Phase 2: 服务端嵌入与路由** | `internal/server/status_page.go`<br>`internal/server/server.go`<br>`internal/server/status_page_test.go` | **已完成** | Go `//go:embed` 静态嵌入，挂载 `GET /status.html`，单元测试 `TestStatusPage_ServesHTML` PASS |
| **Phase 3: 文档全面同步** | `README.md`<br>`README.zh.md`<br>`docs/UserGuide.md`<br>`docs/UserGuide.zh.md`<br>`CHANGELOG.md` | **已完成** | 中英文 README 与 UserGuide 端点速查表全面收录，CHANGELOG 记录新增功能 |
| **Phase 4: 全量回归与守卫** | `internal/archtest/...`<br>全仓 38 个 Go 模块 | **已完成** | `archtest` 架构守卫 100% PASS，Darwin/Linux/Windows 交叉编译全通，全仓单测 100% PASS |
| **Phase 5: 总结与归档** | `docs/STATUS_HTML_DASHBOARD_DESIGN_gemini.md` | **已完成** | 完成最终执行总结归档与清理 |

---

## 9. 落地结果与最终执行总结 (Execution Summary)

本次 `/status.html` 端点升级遵循第一性原理与 KISS/YAGNI 原则，成功为 VMR 提供了开箱即用、零外部依赖的高性能运行态 Web 可视化看板：

1. **核心落地成果**：
   - **自包含零外网依赖**：全内联 HTML+CSS+JS（无第三方框架、无外部 CDN 请求），在断网、私有内网 VPC 环境下 100% 毫秒级秒开；
   - **纯客户端渲染 (CSR)**：Go 二进制通过 `//go:embed` 静态直出模板外壳，UI 状态全部在浏览器中动态填充，服务端计算开销为 0；
   - **安全鉴权闭环**：杜绝 URL 传参泄露风险，基于 `localStorage` 缓存凭证，未授权时弹出优雅交互式密码弹窗并支持随时脱敏管理；
   - **交互式运维控制**：固定 5 分钟自动轮询，实时展现并发排队、系统资源、宏观吞吐、端点健康状态机与额度水位；
   - **架构与质量合规**：38 个包全量测试与架构守卫 100% PASS，跨平台编译无任何阻碍。

