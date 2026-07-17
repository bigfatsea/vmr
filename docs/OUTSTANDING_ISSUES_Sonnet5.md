<!-- Ver 2026-07-17 12:00, by Sonnet 5 -->
# vmr — 遗留问题清单(Outstanding Issues)

> **性质**:本文档汇总自四份历史审计——`AUDIT_REPORT.md`(1.0)、`AUDIT_REPORT_2_Fable5.md`/`AUDIT_SUMMARY_2_Fable5.md`(2.0,Fable 5)、`audit_report_v2_logs_pi_agent.md`/`audit_report_v2_summary_pi_agent.md`(2.0,Pi Agent)、`AUDIT_REPORT_V3_Sonnet5.md`(V3,前三份的交叉核实定稿)——只保留经本次核对**截至今天(commit HEAD,`internal/server/server_v22_test.go` 重命名之后)仍然成立**的问题。
>
> **不再收录的条目**:已修复、已核实为非问题、或经审阅判定为"有意为之的设计决策"的条目不再出现在这里(它们的完整论证过程、修复记录留在 git 历史里——四份历史审计文档已在本次整理中删除,可用 `git log --all --diff-filter=D -- docs/AUDIT_REPORT*.md docs/audit_report*.md` 找到,再用 `git show <commit>^:<path>` 取回全文)。归纳起来,历史上共有约 21 项被确认修复、3 项被确认为非问题或设计决策,不再列出。
>
> **本次额外核实的变化**:`core.ErrorClass.String()` 的 `default` 分支此前既没有 `ErrTransient` 的显式 `case`、也没有测试锁定 4 个审计专用值的字符串——这两点已在后续的代码整理中补齐(见 `internal/core/core.go`/`core_test.go`),原本归在第二梯队的这一条相应降级,详见 §3 第一条。
>
> 严重度:`[S]` 严重 / `[M]` 中等 / `[L]` 轻微。梯队标准沿用历史审计:**第一梯队** = 推荐尽快处理(问题+方案+工作量);**第二梯队** = 改与不改都合理(问题+简要方案);**第三梯队** = 点出即可,附一句建议方向。

---

## 0. 结论先行

- 没有 `[S]` 级别之外的严重缺陷,没有会导致数据丢失、凭证泄漏或服务不可用的问题。项目可以继续放心用于生产。
- 第一梯队只有 2 项,且都是"先加观测、暂不改行为"的低风险方案,合计工作量 <1 天。
- 第二梯队 8 项、第三梯队约 25 项,均为"改与不改都合理"或"点出即可"级别,没有时间压力。

---

## 1. 第一梯队(推荐尽快处理)

### 1.1 [S] `stripThinkingProcess` 强绑定 MiniMax wording,且无任何缓解措施——唯一 [S] 级、连续三轮审计未处理

- **位置**:`internal/router/response.go::stripThinkingProcess`。
- **问题**:MiniMax M3 的 thinking=medium 响应剥离依赖硬编码的 `"Thinking Process:"` 头 + `"Looks good. Pro(ceed)?"` 自认可标记。MiniMax 一旦改措辞,thinking 内容会整段泄漏进用户可见输出并被存入对话历史,下一轮又喂回模型形成反馈循环——且**没有任何软降级或观测手段**,运维完全不知道剥离已经失效,直到用户报告"AI 把内心独白说出来了"。
- **影响范围**:每一个 `thinking=medium` 请求都走这条路径,是高频场景,不是边界 case。
- **方案(按 ROI 排序)**:
  1. **【推荐先做】纯观测告警**:响应满足"含编号小节(`1.`/`2.`/`3.`)+ 长 content(>1KB)+ 未命中现有 strip 标记"时,往 `Attempt.Norm` 打一个 `thinking_process_pattern_detected` 标记,供 `vmr report` 统计触发频率。~50 行 + 测试,零回归风险(纯观测,不改字节、不影响 failover/健康)。
  2. 视 1 的实测频率决定是否做降级(命中疑似泄漏时退化到 `overflow_raw_passthrough` 同款处理)或加可配置 trigger 关键字——这是产品决策,不建议跳过 1 直接做。
- **工作量**:~50 行 + 1 个测试文件,半天以内。
- **为什么排第一**:不是难度问题,是它已经被三轮审计(1.0→2.0×2→V3)连续标记为唯一 [S] 级问题,却连最便宜的纯观测方案都没有落地。建议下次单独排期处理,不要再让第五轮审计重复确认同一件事。

### 1.2 [M] compaction 链接 `needle` 200 字节上限的漏链无观测

- **位置**:`internal/report/session.go::linkCompactions`/`needle`(约第 700-738 行)。
- **问题**:`linkCompactions` 用 200 字节的子串包含检查双向链接 compaction 调用与被压缩/续接的会话。如果 compaction marker 或会话锚点文本在原文里出现的位置超过 200 字节,链接会被直接漏检——`vmr-requests-index.md` 里对应的 `Summarizes`/`ContinuesTo` 字段静默留空,排障时无法区分"确实没有关联"还是"链接失败了"。
- **方案(确定)**:未匹配时加一条 debug 级日志(如 `log.Printf("compaction linking: needle %q not found in any session", ...)`),不改变现有匹配逻辑本身。
- **工作量**:~10 行。
- **owner 建议**:纯观测补丁,和 1.1 的"先观测再决定要不要改行为"思路一致,可以和 1.1 一起排进同一个批次。

---

## 2. 第二梯队(改与不改都合理)

1. **[L] `housekeep.go::purgeOne` 在 resume+retention 组合场景下产生误导性 ENOENT 日志**——`internal/audit/housekeep.go:141-146`,`purgeOne` 无 `os.IsNotExist` 判断。目录里同时存在 `X.jsonl`(压缩中断残局)与其 `.zst` 且超保留期时,resume 删原文件 + purge 删 `.zst`,随后 `purgeOne` 对同一 `.zst` 再删一次会报错。无实害,纯日志噪音。**方案**:该分支加 `os.IsNotExist` 静默跳过,~3 行。

2. **[L] `audit.go::Redact` 浅拷贝非凭证 header 的 value 切片**——`internal/audit/audit.go:209`(`out[k] = vs`),与原 header 共享底层数组。理论上调用方在 Redact 后原地改值会互相污染,当前无调用方这样做,实害为零。**方案**:需要严格时把 `out[k] = vs` 改成 `out[k] = append([]string(nil), vs...)`。

3. **[L] `audit.Attempt.RawPreStrip` 字段类型仍为 `any`**——`internal/audit/audit.go:146`,消费端要类型断言,不利 schema 化。**方案**:改成 `json.RawMessage`(需要同步检查所有读取该字段的消费方,尤其是 `report/detail.go` 的渲染逻辑)。

4. **[L] `strategy.priority.Compare` 用减法比较 `Priority`,极端值可溢出反转排序**——`internal/strategy/strategy.go:75`(`a.Priority - b.Priority`)。config 对 `Priority` 取值无范围校验,联动风险同源。**方案**:改 `cmp.Compare(a.Priority, b.Priority)`,零成本消除;或至少给 config 的 `Priority` 字段加合理范围校验。

5. **[L] 上游 3xx 重定向未显式处理**——`internal/router/router.go` 全文无 `CheckRedirect`。`http.Client` 默认跟随重定向,POST 301/302/303 会被静默改写成 GET,与"直连等价"的设计原则相悖。LLM API 现实中几乎不发 3xx,是休眠风险。**方案**:构造 `http.Client` 时给 `Transport` 配 `CheckRedirect: 不跟随`,3xx 当普通响应透传给客户端。

6. **[L] `writeRequestRows` 的 `defer f.Close()` 错误被吞**——`internal/report/export.go:419`。磁盘写满时"导出成功"但文件不完整不会报错。**方案**:改成具名返回 + defer 里合并 Close 错误(参考同文件 `housekeep.go::compressFile` 已有的模式)。

7. **[L] `reassembleSSE`(语义重组,`internal/report/render.go`)与 `router/response.go` 的 SSE 状态机是两套独立实现**——核实历次审计一致维持"暂不合并"判断:一个是字节级转发(必须保真、增量处理),一个是语义重组(为渲染服务、可以整体处理),关注点不同,合并成本高于收益。**方案**:暂不处理;如果未来两边的 SSE 解析规则出现不一致的 bug,再考虑抽取共享的"事件切分"层(不含语义提取部分)。

8. **[L] `vmr report` 对同一批文件 `Build` 与 `AnalyzeSessions` 各完整读一遍**——`cmd/vmr/main.go::cmdReport` 是两次独立调用,各自完整扫描一遍输入文件(含 zst 解压)。GB 级日志下批处理时间翻倍。**方案**:合并成一趟遍历,让两个分析器共享同一次 `ForEachLine` 扫描——改动量较大(>200 行,两个分析器的内部状态需要重新组织成"单趟喂入"的形状),ROI 不如第一梯队条目,建议只在真的遇到"报告跑太慢"的实际投诉时再做。

---

## 3. 第三梯队(点出即可)

### 3.1 核心代码类

| 问题 | 情况 | 建议方向 |
|---|---|---|
| `ErrorClass.String()` 的 `default` 兜底为 `"transient"` | 已补全:10 个声明值现在全部有显式 `case`,`TestErrorClassString` 也补齐了 4 个审计专用值的字符串锁定(`internal/core/core.go`/`core_test.go`)。仅剩一个纯命名判断:`default` 是否该返回 `"unknown"` 而不是复用 `"transient"` | 产品决策,不影响正确性(default 分支现在只覆盖"声明集合之外的非法值",实际不可达);如果要改,把 `default` 返回值换成 `"unknown"` 即可,一行 |
| `HealthKey` sha256 前 4 字节截断 | 碰撞概率 2^-32 级,可忽略 | 不建议改 |
| 健康冷却参数硬编码(`transientBase`/`longCap` 等) | "零调参"是既有设计选择 | 不建议改;若真要做,加对应 config 项 |
| `retentionDays` 包级全局 atomic 无测试锁定不变量 | `main.go` 的 reload 路径确认会调 `SetRetentionDays`,但没有专门测试锁住这个顺序 | 补一个 `SetRetentionDays`→`RetentionDays` 往返断言即可 |
| `IngressPath` 写死 openai/anthropic 两协议 | 未来加第三个协议时这里要记得同步改 | 加协议时把它挪进 `Adapter` 接口的一个方法 |
| `${VAR}` 未定义时静默展开为空串 | 已文档化的预期行为,`vmr diagnose` 的 api_key 检查是现有缓解 | 不建议改;如需要,`diagnose` 里可以专门检测"展开后为空"的情况并提示 |
| YAML 解析错误信息不含行号 | `yaml.v3` 库本身的限制 | 升级库或包一层更友好的错误信息,优先级低 |
| `adminStatus` 对带 zone 的 IPv6(`::1%lo0`)loopback 判断未测 | fail-closed 方向,无害 | 补一个针对性测试用例锁定即可 |
| 客户端流中途断开与完整成功在审计里不可区分 | 2xx 已提交后客户端断开,`error` 字段留空,与真正成功完全一样 | `Attempt`/`Record` 加一个 `client_disconnected` 布尔位 |
| `truncated` 请求的用量不归入任何端点行 | 流被截断的请求因最后一次 attempt 带 Error,没有 `successEp`,tokens/bytes/TTFT 不计入任何端点统计 | `addEndpointRequest` 改成即使 truncated 也按最后尝试的 endpoint 计入 |
| `markdown.go::ms()` 恰好 1000ms 边界的显示格式 | `v > 1000` 而非 `>=`,1000ms 显示成 "1000ms" 而非 "1.0s" | 纯观感,`>` 改 `>=` 一行 |
| `RewriteModel`/`RewriteStream` 缺 fuzz 测试 | splice 直接关联字节保真承诺 | 加 `testing.F`,约半小时 |
| scanner 对畸形 JSON(如 `{,}`)"宽进",依赖上游 `json.Unmarshal` 先行校验 | 当前入口都有校验在前,实害为零,但这是隐式前提 | 加一行注释说明这个隐式依赖即可,不用改逻辑 |
| `contentHint` 的 `"sensitive"` 命中 `"case-sensitive"` 等无关措辞 | 已知取舍:误判仅多一次无害 failover | 词表可以改成要求前后有空格或特定上下文,优先级低 |
| `openai`/`anthropic` 两个 adapter 的 header 拷贝循环重复约 4 行 × 2 处 | 四轮审计一致判断体量太小、凭证 header 名字与格式不同,抽公共函数收益不明显 | 维持现状 |
| `detail.go::sanitizeName` 不去重连续 `-` | `+` 量词已合并大部分场景 | 如需要,`strings.ReplaceAll` 再扫一遍多余的 `--` |
| `fileLinksCell` 裸 HTML 链接 | 非 Markdown 链接语法 | 改成 `[text](url)` 语法,一行 |

### 3.2 脚本/配置/文档类

| 问题 | 情况 | 建议方向 |
|---|---|---|
| `go.mod` 无 `toolchain` 指令 | 声明 go1.25.1,本机实测 go1.26.5,单人项目可接受 | 可选:加一行 `toolchain go1.26.5` 固定构建环境 |
| `.gitignore` 的 `*.jsonl` 全局忽略 | 对未来想提交的测试 fixture 有潜在影响 | 真需要时加 `!path/to/fixture.jsonl` 白名单 |
| `vmr.sh` 的 `ExecStart=$BIN start -c $CFG` 未加引号 | 路径含空格时 systemd 单元会解析错误,当前部署路径不含空格,纯理论风险 | 加双引号包裹:`ExecStart="$BIN" start -c "$CFG"` |
| `write_env_file` 对含空格/引号的值处理 | 同理,`printf '%s=%s\n' "$var" "${!var}"` 写入的值如果本身含换行或特殊字符,`launchd`/`systemd` 的 EnvironmentFile 解析可能出错 | API key 现实中不含空格,优先级低;真需要可以改成对值做简单转义 |
| `loadtest/` 下 `runner`/`config.yaml`/`gentargets` 三处地址常量需人工同步 | 一次性压测工具,当前无自动化保障三处一致 | 抽成一个共享的小配置文件,或在三处互相加注释指引 |
| loadtest 出错中断走 `Process.Kill()`(SIGKILL)而非 Interrupt | 留下"有 START 无 STOP"的假崩溃日志痕迹,不影响压测结果本身 | 改用 `os.Interrupt`,给几秒优雅退出窗口后再 Kill |
| `loadtest/gentargets/main.go` 的 `out.Write`/`out.Close()` 错误被忽略 | 磁盘满时 `targets.json` 可能静默截断 | 补错误检查,~5 行 |
| README 的 `admin/status` 示例"无需 api_key"与实现一致 | 意味着同机其他用户可读健康拓扑,单机单用户场景可接受,已文档化 | 不建议改 |

---

## 4. 方法论说明(供下一轮审计参考)

- 本文档是**唯一**需要跟踪的当前状态。历史上 1.0→2.0(两份并行)→V3 反复出现"审计产出不进队列,下一轮重复确认同一批问题"的情况,这次借清理历史文档的机会把这个循环断掉:往后每次核实/处理,直接在本文档对应条目上更新或删除,不再另起一份新报告。
- 如果需要查某个已修复/已关闭条目当年的完整论证,四份历史审计文档已删除但仍在 git 历史里:`git log --oneline --all -- 'docs/AUDIT_REPORT*' 'docs/audit_report*'` 定位提交,`git show <commit>^:<path>` 取回删除前的内容。
