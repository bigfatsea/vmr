# 任务说明书：Group 4A - 产物级缓存（L2/L3）与指纹（Phase 4，单组串行）

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §7（D1/D8、三级模型、失效矩阵）、§8.3（-no-cache 旁路常驻）。
> **前置：Group 3C 已合并**（单轨渲染已落地，L3 挂在 VM 渲染前才有意义）。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：`internal/report/`（缓存调度与指纹：新文件 `digest.go`/`cache*.go` + 接线点的最小改动 + `*_test.go`）、`internal/journey/`（L2/L3 接线点最小改动 + `*_test.go`）、`cmd/vmr/cmd_analyze.go`、`cmd/vmr/cmd_report.go`、`cmd/vmr/*_test.go`（`-no-cache` flag）
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 `internal/ctxgraph/**`**（md5 内容寻址底座原样保留，见下）、**不得修改 `internal/fmtutil`、`internal/chatmsg`、`internal/archtest`**（新文件如超默认 700 行预算拆文件解决）。
3. 架构纪律：
   - **缓存判据一律 sha256，`ctxgraph` 的 md5 底座一个字节都不动**（D8：md5 是语料内部身份标签，作为字节分量喂进 sha256 链）。
   - 缓存粒度 = **整套产物一个指纹**，不做切片级隔离（D1）。
   - 指纹分量取原始字节，标量按固定宽度编码（`binary.BigEndian` / `math.Float64bits`），不用 `fmt.Sprintf`。
4. Git 规范：短命令式提交信息，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档主控独占；待登记项写 `NOTES_FOR_LEAD.md`。
6. 严禁 `git add .`，精准暂存。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: 唯一的 Digest 构造（D8）
- `internal/report/digest.go`：`Digest(components ...[]byte) [32]byte`——长度前缀（`uvarint`）的有序 sha256 链。三条性质缺一不可：顺序敏感、无歧义拼接、重复不抵消；单测逐条钉死（含 `Digest(a,a) ≠ Digest()`、`Digest("ab","c") ≠ Digest("a","bc")`）。

### 任务 2: L2 产物数据缓存（§7.1）
- 依赖 = `Digest(输入文件哈希按序…, 配置指纹, 格式版本, 分析参数)`：
  - 输入哈希：`ctxgraph.HashFile` 的 sha256 内容哈希（**没有 mtime fast path**）。
  - 配置指纹：仅影响金额的部分——各 provider 的 `pricing.rates` 覆盖、顶层 `exchange_rate`、内嵌标准表 `GeneratedAt`。不是整个 config.yaml。
  - 分析参数：时间窗、`-lang`、taskseg profile、自流量排除集、一切改变取样口径的 flag。判据："改了它会不会改变任何一个落盘数值或人读文本"。
- 命中：跳过聚合与叙事构建（Journey 半区两遍未缓存的全量解压是主要收益，§7.1）；未命中：全量重算后重写指纹记录。
- 指纹记录存放位置自定（建议输出根下 `.cache/` 内一份 json），属可再生缓存，权限 0600/0700。

### 任务 3: L3 表现层缓存
- 依赖 = `Digest(ViewModel 指纹, 渲染器版本, 语言)`；数据与渲染器任一未变则跳过渲染与写盘。
- 与 3C 的单轨渲染衔接：L3 命中时跳过"读 JSON → VM → 序列化"，直接保留既有 `.md`。

### 任务 4: `-no-cache` 旁路
- 常驻 flag（不是过渡开关）：任何缓存可疑行为都能立刻退回全量重算对照。

### 任务 5: 守卫测试（§9 / §7.4）
- 失效矩阵逐行测试：日志增改 / 定价汇率改 / report.yaml 改 / 分析参数改 / 渲染器改 / 格式版本升级 → 对应层级失效；二进制版本变化（无格式变更）→ 全命中。
- 缓存命中路径与冷启动路径产物一致（浮点容差沿用 `1e-6` 惯例）。
- `-render-only` 与缓存交互：L2 有效时 `-render-only` 依旧工作（它本来就只走 JSON→VM→MD，与 L2 无关——用测试钉住这个正交性）。

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -race ./internal/report/... ./internal/journey/... ./cmd/vmr/...`
3. 架构门禁：`go test ./internal/archtest/...`
4. 手工冒烟：同一输入连续两次 analyze，第二次耗时应显著下降且产物字节一致；`-no-cache` 后与冷启动一致（结论记 `NOTES_FOR_LEAD.md`）
5. `git status -s` 确认无越界
6. Commit：`git commit -m "feat(analyze): product-level l2/l3 caches keyed by ordered sha256 digest chain, -no-cache bypass"`
