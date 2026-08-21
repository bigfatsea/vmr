// Ver 2026-08-19 21:34, by Gemini 3.7 Flash

# vmr 命令层重构设计规范 — 一个分析动词，一个读取原语

> **现状（2026-08-21）**：本文档的核心方向已在 P9 阶段采纳
> 落地，但**不是全盘照抄**——P9 明确未采纳本文档提议的 `vmr cat` 新命令与 `--long-flag`/短选项
> 风格。本文档保留作模块级设计细节参考，避免把已拍板的问题重新提出一遍。

本文档作为 `story_report_architecture_opus-5.md` §7.9 的专项实施设计，定义 `vmr` 日志分析与法证命令体系的全新交互规范。

---

## 1. 设计哲学与架构映射

在新的架构体系中，**原料只有 append-only 的审计日志（唯一只读事实），其余一切产物皆为 View**。

用户对日志的探索是**连续变焦**的（宏观大盘 $\rightarrow$ 中观任务叙事 $\rightarrow$ 微观请求法证），旧 CLI 将分析割裂为 `vmr report` 与 `vmr story` 两条命令，导致**套件闭合成了用户的记忆负担**（用户必须手动跑两次命令并指定同一个 `-o`，否则双向链接必定 404）。

新命令系统与 §7.2 的 3×2 矩阵严格对应：

```
                    ┌──────────────────────────────────────────────────────────┐
                    │                      CLI 命令层                           │
                    └───────────┬───────────────────────────────┬──────────────┘
                                │                               │
                        【一个分析动词】                 【一个读取原语】
                          vmr analyze                      vmr cat
                                │                               │
                ┌───────────────┼───────────────┐               │
                ▼               ▼               ▼               ▼
          【宏观视角】     【中观视角】     【共享证据层】    【微观原始事实】
          vmr-report      vmr-stories      evidence/sys*   stdout (JSON)
          vmr-requests    journey-*.md     details/*.md    (替代 details/*.json)
```

---

## 2. 新 CLI 命令表面总览

```text
vmr — Virtual Model Router & Forensic Analytics

分析与法证（Analysis & Forensics）：
  vmr analyze [flags] [globs...]            # 分析动词：默认构建完整闭环分析套件（宏观+中观索引+证据）
  vmr analyze --journey <id|glob> [flags]  # 变焦：聚焦单/多个任务执行叙事（按需物化 details/）
  vmr analyze --compare <id1,id2> [flags]  # 变焦：双任务行为剖面对比与分叉点检测
  vmr analyze --corpus [flags]             # 变焦：语料级指标分布、Finding 命中与相关性统计
  vmr cat <coord>                          # 读取原语：按坐标提取原始审计记录 JSON 至 stdout
  vmr replay -p <provider> <coord> [flags] # 重放原语：按统一坐标提取并重发单次请求

服务与运维（Runtime & Operations）：
  vmr start [-c config.yaml]               # 启动路由代理服务
  vmr status [-c config.yaml | -addr ...]  # 查看运行中实例的健康、并发、配对与配额状态
  vmr check [-c config.yaml] [log|cache]   # 配置校验、路由规则预览与工作目录探测
  vmr diagnose [-c config.yaml]            # DNS/TLS/上游探针连通性与路由活跃度体检
  vmr version                              # 输出编译版本与 VCS 状态
```

---

## 3. 一个分析动词：`vmr analyze`

### 3.1 动词选型裁决
选用 **`analyze`**（保留 `report` 作为别名平滑过渡，但官方主推 `analyze`）。
* **裁决理由**：`report` 带有强烈的“静态宏观报表”暗示；`story` 带有强烈的“叙事文本”暗示。`analyze` 统一涵盖了“全量聚合统计”、“任务因果建图”与“语料挖掘”，是容纳三级变焦最贴切的动词。

### 3.2 运行模式与变焦选择器（Zoom Selectors）
`vmr analyze` 仅通过 **互斥的变焦选择器 Flag** 切换输出范围，所有模式共享一次扫描、一套增量缓存（`.parse-cache/`）与一套上下文图（`internal/ctxgraph`）：

| 变焦模式 | CLI 语法 | 默认产物与行为 | 性能与 I/O 契约 |
| :--- | :--- | :--- | :--- |
| **全量套件（默认）** | `vmr analyze` | 产出 `vmr-report.{md,json}`、`vmr-requests*.md`、`stories/vmr-stories.{md,json}`。**双向链接 100% 闭环且存在**。 | 稳态只扫变化文件；默认不物化 `details/`（详单链接按需惰性生成或在点击时由坐标直接定位）。 |
| **单/多任务叙事** | `vmr analyze -j, --journey <selector>` | 渲染匹配的 `stories/journey-<id>.md` 与 `.json`。**渲染即生成**（自动物化该任务涉及的 `details/<req>.md` 与 `evidence/sysprompt-*.md`）。 | 单 ID 匹配时独占支持 `--llm-addr` 进行深度语义缺陷检测。多匹配时自动批处理。 |
| **双任务对比** | `vmr analyze --compare <id1,id2>` | 产出 `stories/compare-<id1>-vs-<id2>.{md,json}`。对比行为剖面、工具调用序列、上下文构成与分叉点。 | 自动检查并补齐双方 `journey-<id>.md`；支持 `--llm-addr` 归因分析。 |
| **语料全局洞察** | `vmr analyze --corpus` | 产出 `stories/vmr-story-corpus.{md,json}`。计算指标分位数、Finding 关联度、上下文退化（rot）分析与 N-gram 工具模式。 | 独占模式，不渲染个案文本。 |

### 3.3 参数与标志规范

```text
用法: vmr analyze [flags] [audit.jsonl|glob...]

变焦模式 (互斥，默认产出完整套件):
  -j, --journey <id|prefix|glob>  聚焦渲染指定任务（支持逗号分隔多个或通配符）
      --compare <id1,id2>         对比两组任务的执行特征与分叉点
      --corpus                    全语料统计（分布、Finding 关联、工具序列模式）

分析与证据控制:
      --details[=true|false|all]  详单生成策略:
                                    false: 不生成详单（默认）
                                    true/on-demand: 任务变焦时按需生成（默认内嵌于 --journey）
                                    all: 强制全量生成所有请求的 details/*.md（法证归档用）
      --include-partial           包含头/尾截断的不完整任务链 (默认 false)
      --show-ungrouped            打印未进入任何任务的独立请求位置（排查孤儿请求）

输出与环境:
  -c, --config <file>             指定主配置文件 (默认 config.yaml，用于推导 log_dir/pricing)
  -o, --out <dir>                 产物输出目录 (默认 ./reports)
      --report-config <file>      分析定制配置 (默认 ./report.yaml)
      --lang <en|zh>              报告输出语言 (默认从 report.yaml 继承或 en)
      --currency <code>           金额展示货币 (如 CNY/USD/JPY，需汇率配置支持)

可选 LLM 语义解读 (仅支持单任务 --journey 或 --compare):
      --llm-addr <host:port>      已运行的 VMR 实例地址 (启用 6 项语义缺陷检测与深度归因)
      --llm-model <name>          VMR 上的虚拟模型名称 (如 agent)
      --llm-key <key>             访问 Token (支持 ${ENV_VAR})
      --llm-cache-dir <dir>       LLM 推理结果磁盘缓存目录
      --llm-dry-run               仅评估并打印证据包 Token 体积，不发起实际调用
```

---

## 4. 一个读取原语：`vmr cat`

### 4.1 设计初衷：接管被删除的 `details/*.json`
§7.6(c) 删除了冗余的 `details/*.json`（消除了 12GB 的磁盘读写放大）。微观机读层不再需要物化文件，因为**审计日志本身就是那一格**。`vmr cat` 负责在毫秒级按统一坐标提取记录原文。

### 4.2 命令语法与输出行为

```bash
# 1. 基础用法：输出单条记录的完整原始 JSON 至 stdout
vmr cat vmr-audit-2026-07-25.jsonl:317

# 2. 结合 jq 进行单请求即时法证
vmr cat 2026-07-25:317 | jq '.attempts[0].norm'

# 3. 提取指定层级切片（--body, --upstream, --diff 等）
vmr cat 2026-07-25:317 --client-req   # 仅提取客户端发给 VMR 的原始请求体
vmr cat 2026-07-25:317 --attempts     # 仅提取 VMR 对上游所有尝试的交互及 header/body diff

# 4. 自动定位日志目录（无需手动输入长路径）
vmr cat -c config.yaml 2026-07-25:317
```

* **Stdout/Stderr 规范**：Stdout 输出纯粹合法的 JSON，无任何装饰日志；任何告警或进度一律走 Stderr。完美支持 Pipeline 重定向与 Shell 脚本消费。

---

## 5. 统一记录选择器（Unified Record Selector）

彻底废除旧版中“行号 (`-line`)、时间戳 (`-ts`)、详单文件路径 (`-detail`)”三种互不通用的拼法，收敛为**统一坐标格式**。

### 5.1 坐标语法规则

$$\text{Coordinate} := \langle \text{FileIdentifier} \rangle : \langle \text{LineNumber} \rangle$$

1. **规范形式（Canonical Form）**：`vmr-audit-2026-07-25.jsonl:317`
   * `FileIdentifier`：去除 `.zst` 的 Basename。
   * `LineNumber`：解压后的 1-based 逻辑物理行号。
2. **简写容错形式（Shorthand Form）**（CLI 自动补全/解析）：
   * 省略前缀与后缀：`2026-07-25:317` $\rightarrow$ 自动匹配 `vmr-audit-2026-07-25.jsonl`
   * 包含绝对/相对路径：`logs/vmr-audit-2026-07-25.jsonl.zst:317` $\rightarrow$ 自动规范化
3. **哈希别名支持（Hash8 Alias）**：
   * 支持通过 detail 文件名的 hash8 或完整 detail 文件名寻址：`@b9a4c1f0` 或 `20260725_coding_error_b9a4c1f0.md`。

---

## 6. 重放与法证命令的联动升级

### 6.1 `vmr replay` 语法收敛
`replay` 废除 `-line` / `-ts` / `-detail` 三大互斥 Flag，直接接受统一记录选择器：

```bash
# 旧写法: vmr replay -c config.yaml -provider openrouter -detail out/details/20260713_...json
# 旧写法: vmr replay -c config.yaml -provider openrouter -line 317 logs/vmr-audit-2026-07-13.jsonl

# 新写法 (简洁、统一、自解释):
vmr replay -p openrouter 2026-07-13:317
vmr replay -p openrouter 2026-07-13:317 --dry-run
vmr replay -p openrouter 2026-07-13:317 --model z-ai/glm-5.2 --stream=true
```

### 6.2 跨产物“坐标驱动”法证工作流
用户在各视图间的工作流被完全打通：

```
[vmr-report.md §8 或 vmr-requests-failed.md]
              │ (复制报错行坐标: 2026-07-25:317)
              ├─────────────────────────────────────────┐
              ▼                                         ▼
   【查看原始事实】                            【现场重现与重放】
   vmr cat 2026-07-25:317 | jq .           vmr replay -p openrouter 2026-07-25:317 --dry-run
```

---

## 7. 新旧命令体系对照表与迁移映射

| 分析/操作场景 | 旧命令（Legacy CLI） | 新命令（Opus 5 规范） | 改变的底层本质 |
| :--- | :--- | :--- | :--- |
| **完整生成全套分析套件** | 必须执行两次：<br>1. `vmr report -o ./out`<br>2. `vmr story -render-all -o ./out` | `vmr analyze -o ./out` | 一次进程内完成扫描、建图、宏观聚合与中观索引，**套件闭环由工具保证** |
| **单任务叙事分析** | `vmr story -journey j-openclaw-01` | `vmr analyze -j j-openclaw-01` | 决策脊柱补齐（22/22 步）；自动按需生成对应的 `details/` 与 `evidence/` |
| **单任务 + LLM 深度解读** | `vmr story -journey j-01 -llm-addr localhost:8800 -llm-model agent` | `vmr analyze -j j-01 --llm-addr localhost:8800 --llm-model agent` | 支持 6 项语义缺陷检测与证据锚定，结果持久化入 `journey-*.json` |
| **双任务行为对比** | `vmr story -compare j-01,j-02` | `vmr analyze --compare j-01,j-02` | 包含分叉点检测、交付物比对与 Provenance 溯源 |
| **语料全局指标分析** | `vmr story -corpus` | `vmr analyze --corpus` | 纯净语料统计，输出至 `stories/vmr-story-corpus.*` |
| **查看单次请求原始详情** | 打开 `details/*.json`（占用 12GB 磁盘） | `vmr cat 2026-07-25:317` | 消除逐字 JSON 副本，按坐标即时读取原日志 |
| **请求重放 (Replay)** | `vmr replay -provider p1 -line 317 file.jsonl`<br>`vmr replay -provider p1 -detail file.json` | `vmr replay -p p1 2026-07-25:317` | 收敛为统一坐标，不再依赖详单 JSON 文件 |
| **脚本包装器** | `./vmr.sh report` / `./vmr.sh story` | `./vmr.sh analyze` / `./vmr.sh cat` | `./vmr.sh` 自动透传 `-c config.yaml` 与工作路径 |

---

## 8. 权衡取舍与边缘情况处理（Tradeoffs & Edge Cases）

1. **为什么不把 `cat` 做成 `analyze --cat`？**
   * **权衡**：`analyze` 是聚合/渲染引擎（输出文件集、写 Markdown/JSON、带分析耗时）；`cat` 是管道原语（只读单条、毫秒级响应、直通 stdout）。分离为顶层命令符合 Unix 管道哲学（KISS），可无缝配合 `grep`、`jq`、`pbcopy`。
2. **同名文件与路径消歧**：
   * 审计文件天生携带日期（`vmr-audit-YYYY-MM-DD.jsonl`），同名冲突在单实例下概率为 0。若用户手动合并了不同目录的同名日志，`vmr cat` 在检测到冲突时**响亮地报错（Fail Loudly）**并提示传入完整相对路径，坚决不引入复杂的隐式推断规则。
3. **只读安全性与 0600 权限**：
   * 所有分析命令（`analyze`, `cat`）均为**纯只读操作**，绝不修改原始审计日志。生成的 `reports/` 目录与 `evidence/` 文件保持 `0600` 权限纪律（防敏感对话泄漏）。
4. **向后兼容与过渡期**：
   * 在 `cmd/vmr/main.go` 中，暂时保留 `report` 和 `story` 关键字作为 `analyze` 的 Alias，并打印 Deprecation 提示（告知映射关系），确保旧脚本不立即中断，但文档与新输出全面切换为新规范。
