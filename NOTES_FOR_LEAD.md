# Notes for Lead (Group 4A - Phase 4)

## 1. 产物级缓存（L2/L3）与指纹机制落地总结
- **唯一的 Digest 构造（D8）**：
  - 在 `internal/report/digest.go` 和 `internal/journey/digest.go` 落地了长度前缀（`uvarint`）的有序 SHA-256 链。
  - 单测（`digest_test.go`）严格验证了三条性质：顺序敏感、无歧义拼接、重复不抵消。
  - 标量按固定宽度大端编码（`int64`/`uint64` 走 `binary.BigEndian`，`float64` 走 `math.Float64bits`），无 `fmt.Sprintf` 漂移隐患。
  - `ctxgraph` 底层的 MD5 内容寻址底座原样保留，16 字节 MD5 摘要作为原始字节分量喂入 SHA-256 链。
- **整套产物 L2 缓存（§7.1 / D1）**：
  - 依赖：`Digest(输入文件哈希按序…, 配置指纹, 格式版本, 分析参数)`。
  - 输入哈希基于 `ctxgraph.HashFile` 的 SHA-256 内容哈希（无 mtime fast path）。
  - 配置指纹严格仅提取影响金额部分（provider 费率覆盖、顶层 `exchange_rate`、内嵌标准表 `GeneratedAt`）。
  - 分析参数覆盖所有影响取样口径与落盘产物的 flag（时间窗、`-lang`、task profile、自流量排除集合、`currency`、`details`、`render-all`、工作模式等）。
  - 命中时直接跳过 `setupStoryRun`（候选扫描、建图、Journey 全量解压构建等）与宏观报告聚合，极大降低二次运行开销。
  - 指纹记录存放于 `{outDir}/.cache/fingerprint.json`，权限遵循 0600/0700。
- **L3 表现层缓存**：
  - 依赖：`Digest(ViewModel 指纹, 渲染器版本, 语言)`。
  - `ViewModel 指纹` 通过 `report.ComputeVMFingerprintFromManifest` 提取各切片 SHA-256 指纹。
  - L3 命中时跳过"读 JSON → VM → 序列化"，直接保留既有 Markdown 产物。
  - 当渲染器版本改变或 Markdown 文件缺失时，触发单轨重绘（`renderAllFromDisk`）。
- **`-no-cache` 常驻旁路**：
  - 在 `vmr analyze` 中常驻支持 `-no-cache` flag，任何时候均可强制退回全量重算。
  - 与 `-render-only` 正交（`-render-only -no-cache` 亦可强制重绘 Markdown 表现层）。

## 2. 手工冒烟验证结果
- **测试用例**：基于 `./examples/sample-audit.jsonl` 进行端到端分析测试。
- **耗时对比**：
  - 冷启动（Cold Run 1）：~0.591s
  - 缓存命中（Warm Run 2，L2/L3 Hit）：~0.015s（显著下降，约 40x 加速）
  - 旁路全量（No-Cache Run 3，`-no-cache`）：~0.031s
- **产物一致性**：
  - 冷启动与缓存命中产物对比：除带有时钟戳记的 `manifest.json` 外，所有切片及 Markdown 产物**逐字节一致**（BYTE-IDENTICAL）。
  - 冷启动与 `-no-cache` 产物对比：除时钟戳记外**逐字节一致**（BYTE-IDENTICAL）。

## 3. 建议主控登记项（CHANGELOG / KNOWN_ISSUES）
- **CHANGELOG [Unreleased] Added**:
  - `analyze`: 引入产物级 L2（数据切片）与 L3（Markdown 表现层）双层内容指纹缓存，基于长度前缀有序 SHA-256 链（D8），同一输入重复分析耗时大幅降低。
  - `analyze`: 新增 `-no-cache` 常驻 flag，支持旁路全量重算对照。
- **CHANGELOG [Unreleased] Changed**:
  - `analyze -render-only`: 接入 L3 缓存感知，在表现层产物完好时避免冗余重绘，支持 `-no-cache` 强制重写。
