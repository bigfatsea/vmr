# 任务说明书：Group 2A - internal/dashboard 看板骨架资产包

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §6（HTML 看板）、§5.6（跨语言 fixture）、§6.6（版本探测）。
> 本组是纯新增包，与 Phase 1 / Group 2C 完全正交，可与它们并行。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：`internal/dashboard/**`（全部为新建：`dashboard.go`、`assets/*.html`、`assets/*.js`、`js_test.go`、`testdata/fmt_cases.json` 等）
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 `internal/archtest/**`**（`internal/dashboard/dashboard.go` 的行数预算已由主控预登记，上限 400 行）；**不得修改 `cmd/`**（骨架页写入 analyze 路径的接线由后续 Group 2B 完成，本组只交付 `WriteSkeletons(dir)` API）；**不得修改 `internal/report`、`internal/journey` 等**（只读参考）。
3. 代码风格与架构门禁：
   - `internal/dashboard` 是叶子包：只依赖 stdlib，禁止 import 任何 `vmr/internal/*`。
   - 不引前端构建链（npm/bundler/框架），手写 HTML + 内联 CSS/JS；不引图表库（零依赖内联 SVG）。
   - `dashboard.go` ≤ 400 行（archtest 已登记）；超了就拆文件并在说明书中注明（拆出的新 .go 文件如超默认 700 行才需找主控）。
4. Git 规范：短命令式提交信息，严禁任何 trailer（包括 Co-Authored-By）。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占；待登记项写入不提交的 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：同仓库可能有其他 Agent 并行作业；严禁 `git add .`，仅 `git add <file>` 精准暂存。

## 二、背景与数据契约

骨架页是常驻静态资产，零业务数据：浏览器侧 `fetch` 相对路径的 JSON 切片渲染。切片拓扑（§4，磁盘布局 = HTTP 路径布局，页面只发相对路径 fetch）：

```
manifest.json
macro/{summary,finance,reliability,workloads,context-efficiency}.json
requests/{index.json, failed.jsonl, failed.md}
journeys/{index.json, benchmarks.json}
journeys/details/j-<id>.json
compares/{index.json, compare-<a>-vs-<b>.json}
```

字段名以 main 上已合并的切片实现为准（只读参考 `internal/report/rows.go`、`internal/report/export.go`、`internal/journey/`、golden testdata；**不要去找本 worktree 中不存在的文档**）。

## 三、具体研发任务清单 (Action Plan)

### 任务 1: 包骨架与 go:embed
- `dashboard.go`：`//go:embed assets` + `WriteSkeletons(dir string) error`——把六个骨架页幂等覆盖写到 `dir` 根级（平铺，不下沉子目录），文件权限 0600；目录不存在则创建（0700）。
- 单测：幂等性（跑两遍结果一致）、权限位、写入目录为空时的行为。

### 任务 2: 六个骨架页（§6.2）
| 页面 | 消费 |
|---|---|
| `macro-dashboard.html` | `macro/*.json`（五个切片全消费，Tab 分区渐进加载） |
| `request-browser.html` | `requests/index.json`——筛选/排序/分面是必需能力：按客户端/模型/端点/outcome/时间窗筛，按耗时/token/cache-eff 排；行内链向 `requests/details/` 与所属 journey |
| `journey-viewer.html` | `journeys/details/j-<id>.json`；无 `#data=` 时探测 `journeys/index.json` 列出候选 |
| `journey-compare.html` | `compares/compare-*.json`；无 `#data=` 时取 `compares/index.json` |
| `benchmarks.html` | `journeys/benchmarks.json` |
| `tool-waste.html` | `macro/context-efficiency.json` |

通用协议（每页一致）：
- **`#data=` hash 传参**（不用 `?`），值为相对骨架页自身目录的路径；`location.hash` 自行解析。
- **file:// 降级**：`fetch` 失败且 `location.protocol === 'file:'` 时显示"用 vmr 自带托管或任意静态服务器打开"提示，不做静默空白页。
- **版本探测（§6.6）**：每页启动先 `fetch('manifest.json')`；核心判定写成纯函数 `versionBehavior(expected, actual) → "ok" | "banner" | "missing"`（期望版本为页面内置常量）：一致正常渲染；不一致出横幅（写明期望/实际版本号，修复动作指向全量 `vmr analyze`）**渲染继续**；manifest 404 出"这不是一套完整的 analyze 产物"提示。渲染异常一律 catch，错误提示把 manifest format 不一致列为首要嫌疑。
- **鉴权契约（与 2C 组的约定，逐字一致）**：数据类请求（.json/.jsonl/.md）带 `Authorization: Bearer <key>`，key 取 `localStorage.getItem('vmr_status_key')`（与 `internal/server/status.html` 同一存储键，可复用其解锁 UI 模式）；收到 401/403 弹出 key 输入。骨架页 `.html` 本身免鉴权。
- **时间双字段纪律（§5.6）**：显示一律用 `ts_display`，排序/筛选只用 `ts`（epoch ms），前端不写任何时区换算代码。
- **journey-viewer 补充**：候选列表支持勾选两条 journey，生成可复制的 `vmr analyze -compare <a>,<b>` 命令行（不跳转对比页——对比只能 CLI 离线算）。

### 任务 3: 零依赖内联 SVG 图表基元（§6.3）
- 折线/柱状/热力/散点四种，手写坐标轴与 path，复用 "VMR Forensics" 视觉系统（暗色飞行记录仪 / 亮色工程方格纸）。
- 主题系统：CSS 变量注入，暗/亮/跟随系统，与 `/status.html` 视觉一致。
- macro-dashboard 至少落地：成本趋势（折线）、按模型成本（柱状）、按时段流量（热力）、延迟分位（散点或折线）各一处真实消费切片数据。

### 任务 4: 前端格式化函数 + 跨语言 fixture（§5.6）
- JS 侧实现与 `internal/fmtutil` 对应的纯函数：`FmtTokens` / `FmtBytes` / `FmtPercent` / 货币。
- fixture `internal/dashboard/testdata/fmt_cases.json`，格式固定为：
  `{"cases": [{"fn": "FmtTokens", "input": <number>, "want": "<display string>"}, ...]}`
  用例值从 `internal/fmtutil` 的既有测试中选取有代表性的对齐样本（含 0、负数、大数、边界）。**该文件是 2B 组 Go 侧 fmtutil 测试的消费契约，字段名不得擅改；如需增条目只追加，不改结构。**
- Node 测试：`js_test.go` 用 `exec.Command("node")` 跑一段断言脚本执行纯函数（`versionBehavior`、格式化函数）对比 fixture；`node` 不存在时 `t.Skip`。不进任何构建链。

## 四、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -v -race ./internal/dashboard/...`（含 Node 子进程测试；本机 node v24 可用）
3. 架构门禁：`go test ./internal/archtest/...`（只跑，不改）
4. 手工冒烟：写一个临时目录跑 `WriteSkeletons`，`python3 -m http.server` 起在目录父级，浏览器逐页打开确认无 JS 错、fetch 相对路径正确（把冒烟结论写进 NOTES_FOR_LEAD.md）
5. 检查变更范围：`git status -s`（确认无越界文件）
6. Commit：`git add internal/dashboard && git commit -m "feat(dashboard): static skeleton pages with fetch-based rendering, svg primitives, and cross-lang fmt fixture"`
