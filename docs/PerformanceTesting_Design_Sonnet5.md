// Ver 2026-07-16 16:30, by Sonnet 5

# vmr 性能测试方案设计（v2，已落地，含第二轮扩展）

> **本文件性质**：§0-§5 是原始设计（保留作为决策记录，不因后续扩展改写），§6 是实际落地情况（含 2026-07-16 当天的第二轮扩展），以 §6 为准。v2 相对 v1 的变化：先回答"要不要做"，砍掉 Tier 1 微基准（等真出现瓶颈再做），端到端部分改成"自己写的部分只有 mock 上游，压测驱动和报告全部用现成工具/vmr 自己的功能"。

---

## 0. 先回答：这件事有没有必要做？目的和价值是什么？

**诚实的结论：必要性不高，但因为可以做得很便宜，所以值得做——但只值得做成一次性/偶尔跑一下的健全性检查（sanity check），不值得做成常驻维护的性能测试基础设施。**

**为什么必要性不高**：
- vmr 的真实使用场景是 Stanford 自己（或一个 ≤3 人小团队）的 agent 流量，量级是"每分钟几十个请求、并发几个 agent"，远远够不上会暴露 Go HTTP 服务性能问题的量级——Go 的 `net/http` 处理这种量级的字节透传代理，性能从来不是这类工具的瓶颈来源。
- 全程没有任何迹象（用户反馈、审计报告里的发现）指向 vmr 有性能问题——这次审计报告全量复核 78 个源文件，唯一 CPU/内存密集的两处（图片降采样、thinking 泄漏路径的全缓冲）都是**已知、有意为之的设计权衡**，不是意外的性能坑。
- 在没有具体问题指向的前提下预先搭一整套压测基础设施，是典型的"给还不存在的问题找解法"——不符合这个项目一直在坚持的 YAGNI/克制哲学（`docs/vmr_future_strategy_*` 明确写过"特性优先级保持克制"）。

**为什么还是值得做（便宜的前提下）**：
- **catch 单测抓不到的问题类别**：goroutine 泄漏、并发下的锁竞争（health 注册表、audit 写入）、内存在持续高并发下会不会失控——这些不是"数字好不好看"的问题，是"会不会不小心写出一个只有并发压力下才暴露的 bug"的问题，单测和 `-race` 都覆盖不到。
- **图片降采样和 thinking 全缓冲这两条路径的真实成本目前是未知数**——不是"怀疑有问题"，是"压根没量过"，量一次心里有数，成本很低。
- **时机上刚好合适**：`docs/vmr_future_strategy_*` 上次复核的结论是"下一步最高杠杆动作是对外发声（HN/Reddit）"——真发出去之后如果有人上手就把它跑挂了，观感很差；跑一次健全性检查是发声前的低成本保险，不是自证清白。

**结论落地成什么形态**：**一次性/偶尔手动跑的轻量检查，不接 CI，不做成永久维护的测试套件**。跑一次，端到端数字都正常（大概率如此），这件事就翻篇了，不需要再往下细分；只有真的发现某条路径数字异常，才值得针对那一条单独做更细的 profiling（这时候才是 Tier 1 微基准该出场的时候——见 §5，先不做）。

---

## 1. 端到端怎么测：自己写 Go，还是用现成工具？—— 调研结论：混合，且自己写的部分压到最小

调研了几个主流开源压测工具（Vegeta / k6 / oha / hey / Artillery），核心问题是两个：**能不能用配置文件驱动（不想为了测个性能再学一门新的脚本语言）**、**能不能应对 vmr 的 SSE 流式响应**。结论列一下：

| 工具 | 语言/依赖 | 配置方式 | SSE/流式支持 | 备注 |
|---|---|---|---|---|
| **Vegeta** | Go，`go install` 一行装完 | **JSON-lines targets 文件**——每行一个 JSON 对象（`method`/`url`/`header`/`body`，`body` base64 编码），天然支持每条请求配不同的 header/body | 无特殊感知，但作为一个通用 HTTP 客户端，会正常把响应体读到 EOF 为止——对我们要的"总耗时"这个指标完全够用 | 内置延迟百分位报告（`vegeta report`），Go 原生、零新增语言依赖，是这几个里跟这个项目的技术栈最贴的 |
| **k6** | Go 写的引擎 + JS 写测试逻辑 | JS 脚本（不是纯配置文件），"scenarios" 是一等公民概念，功能最全 | 官方无原生 SSE，要用 `xk6-sse` 扩展，得自己 `xk6 build` 出一个定制二进制才能用 | 功能最强但"要学 JS 脚本 + 要自己编译扩展"，对我们这个"越简单越好"的诉求来说过重 |
| **oha** | Rust | 纯命令行参数，不支持按请求变化 body | 无特殊支持 | 适合"打一个固定端点看吞吐"，不适合我们需要多种请求体的场景矩阵 |
| **hey** | Go | 同 oha，命令行参数驱动 | 无特殊支持 | 社区已经普遍转向 oha，hey 维护变少 |
| **Artillery** | Node.js | YAML 配置，功能丰富 | 原生"streaming"支持是给 HLS/MPEG-DASH 媒体流用的，不是 SSE | 引入 Node.js 依赖，对一个纯 Go 项目不划算 |

**推荐：Vegeta。** 理由：Go 原生（这个团队的默认技术栈，零新增运行时依赖）、JSON-lines targets 文件正好是"配置文件驱动"最简单直接的实现、内置延迟百分位统计（不用自己写报告代码）、对 SSE 没有特殊感知但这恰好不是问题——见下面 §2 的关键设计。

### 一个关键简化：TTFB/流式细节不需要压测工具懂，vmr 自己的审计日志已经有

我们想知道"哪条场景因为全缓冲而变慢了"，第一反应是"压测工具要能测出 TTFB（首字节延迟）"——但 vmr 自己已经在做这件事：**每条请求的 `ttft_ms`/`dur_ms` 都会写进审计日志，`vmr report` 已经会按虚拟模型分桶算出每个桶的 p50/p95**（这是这个项目现成的、已经测试过的功能，不是要新写的东西）。

所以设计上可以偷懒偷得很彻底：**Vegeta 只负责"客户端视角看到的总延迟/吞吐/成功率"（对外部调用方来说最终关心的数字），而"是不是因为全缓冲变慢了"这种更细的归因，跑完压测之后直接 `vmr report` 一下审计日志，按虚拟模型（= 场景）分组的 p50/p95/TTFB 就自动出来了，不用为这件事另外写一行报告代码。**

这也顺带回答了"场景怎么区分"：**用虚拟模型名当场景标签**——每个场景在 loadtest 用的 `config.yaml` 里配一个虚拟模型（`baseline`/`thinking_leak`/`big_image`/`failover` 等），mock 上游按 model 名决定要模拟哪种响应形态，Vegeta targets 文件里每个场景对应一个 target（打向对应虚拟模型），跑完之后 `vmr report` 的 `ByModel` 表天然就是按场景分组的结果——**vmr 自己的路由机制 + 自己的报表功能，双双被复用成了测试基础设施的一部分，不用重新发明。**

---

## 2. 需要自己写的东西：只有 mock 上游

跟前一版方案一样，mock 上游没有现成工具能替代——没有任何通用压测工具会假装自己是一个 LLM provider，更不用说模拟 vmr 已知要应对的具体怪癖形态（MiniMax 的 thinking 泄漏文本、SSE 分块节奏）。这部分依然要写，但复用现有测试代码里已经验证过的模式（`internal/server/server_test.go::newUpstream`、`server_hang_test.go::stallingUpstream`），量不大（预计 100-150 行）：一个 `httptest.NewServer`，按收到请求的 `model` 字段分发到几种预先写死的响应形态。

## 3. 整体流程（全部是命令行操作，没有新的 Go 程序入口）

```bash
# 1. 起 mock 上游（一个小 Go 程序，唯一需要新写的代码）
go run tools/loadtest/mockupstream.go   # 监听某个端口，按 model 名分发响应形态

# 2. 用一份专门的 loadtest config.yaml 起真实的 vmr——就是正常的 `vmr start`，
#    不是什么特殊模式；provider 的 base_url 指向上一步起的 mock
./vmr start -c loadtest/config.yaml &

# 3. Vegeta 打过去，targets 文件里每行一个场景（不同虚拟模型 = 不同 mock 响应形态）
vegeta attack -targets=loadtest/targets.json -rate=20 -duration=30s | vegeta report

# 4. 客户端视角的数字（总延迟、吞吐、成功率）来自上一步 vegeta report 的输出；
#    服务端视角、按场景细分的 TTFB/耗时，直接用 vmr 自己的报表工具，零新代码：
./vmr report "$(./vmr dirs -c loadtest/config.yaml log)/vmr-audit-*.jsonl"
```

`loadtest/` 目录下只有三样东西：`config.yaml`（loadtest 专用配置，各虚拟模型指向 mock 上游）、`targets.json`（Vegeta 的场景定义）、`mockupstream.go`（唯一的自定义代码）。不放 `cmd/` 下（不进 `go build ./cmd/vmr` 的产物，不影响"单二进制"这个卖点），也不需要一个专门的 `tools/loadtest/main.go` 编排程序——Vegeta 和 `vmr report` 已经是编排本身。

## 4. 场景矩阵（沿用 v1 的判断，覆盖开销特征明显不同的几条路径）

| 虚拟模型名（= 场景） | mock 上游行为 | 覆盖的代码路径 |
|---|---|---|
| `baseline` | 小 JSON，非流式 | 基线——vmr 纯路由开销下限 |
| `stream_normal` | SSE 流，多个小 chunk，正常内容 | 真流式转发（`modePassthrough`） |
| `thinking_leak` | SSE 流，MiniMax "Thinking Process:" 形态 | 已知最差路径——全程缓冲到 EOF，这个数字最值得被量出来 |
| `big_image` | 请求侧带一张超过阈值的合成大图（复用 `imgprep_test.go` 的图片生成思路） | 图片降采样的完整 decode→scale→encode 链路——vmr 里 CPU 成本最高的一段 |
| `long_history` | 请求体是几十 KB～1MB 的长对话历史 | JSON 探测扫描 + model 字段 splice + 审计全量写盘，在大 body 下的开销 |
| `failover` | 前几次尝试返回 429/500，最后一次成功 | health 状态机 + 冷却 + 故障切换循环的开销 |

这六个场景对应六行 Vegeta targets、config.yaml 里六个虚拟模型——增量成本很低（复制粘贴改几个字段），所以没有进一步精简的必要；但也就到此为止，不做协议交叉（openai/anthropic 都测）、不做并发梯度扫描——按 §0 的结论，这是一次性健全性检查，不是要画一条完整的性能曲线。并发就用 Vegeta 的 `-rate` 挑一个温和值（比如 20/s）跑 30 秒，数字明显不对劲再考虑加压，不预先假设需要做压力上限探测。

## 5. 明确不做的事（除非 §0 的检查真的发现了问题）

- **不做 Tier 1 微基准**——`go test -bench` 这条路留着，但只在端到端跑出来某条场景数字明显异常、需要定位到具体是哪个函数慢的时候才启用，不预先写。
- **不做并发梯度扫描 / 找拐点**——不是这次的目标。
- **不接 CI、不做历史趋势追踪**——这是一次性检查，不是长期维护的回归测试。
- **不做协议交叉验证**（openai vs anthropic 都跑一遍）——两个协议共用同一套响应归一化代码，风险集中在协议无关的部分，没必要翻倍场景数。

## 6. 落地情况（2026-07-16，已实现并跑通；同日按用户要求做了第二轮扩展）

四个问题都在初次实现时定了下来：

1. **工具选型**：Vegeta，按方案执行。
2. **场景矩阵**：最初六个场景原样落地——`baseline`/`stream_normal`/`thinking_leak`/`big_image`/`long_history`/`failover`。
3. **定性**：一次性/偶尔手动跑，不接 CI，运行方式和"读数字要注意什么"写进了 [`loadtest/README.md`](../loadtest/README.md)。
4. **目录位置**：`loadtest/`，仓库根目录下（不是 `_tmp/`）——判断是"偶尔重跑"比"用一次就扔"更符合实际用途，值得留在仓库里。生成产物（`targets.json`/`logs/`）都进了 `.gitignore`，只有源代码和配置真正入库。

**同日第二轮扩展**（用户要求"多几个场景、提升并发数和请求数、设几个典型组合多轮测试、结果出一份 Markdown 报告"）：

- **场景矩阵从 6 个扩到 11 个**：新增 `think_tag`（`<think>` 标签形态——先缓冲后恢复流式的路径，跟 `thinking_leak` 的"全程缓冲到底"是两条不同代码路径）、`big_response`（大体积非流式响应，压测响应侧而非请求侧）、`multi_image`（一条消息里 3 张不同尺寸的图，含跨阈值和不跨阈值混合）、`gif`（确认 GIF 的"永不缩放"快速跳过路径在压力下依然便宜）、`anthropic_baseline`（Anthropic 协议适配器，此前只测了 openai 协议——§5 原判断"两个协议共用同一套归一化代码，没必要翻倍"，用户要求后按"反正成本很低"补上了）。
- **新增 `loadtest/runner/main.go`**：一个编排程序（Go 标准库 `os/exec`，无新依赖），把此前需要手动跑的四步（起 mock → 起 vmr → 生成 targets → `vegeta attack`）和读数（`vmr report`）串成一条命令 `go run ./loadtest/runner`。这不违反 §3 "没有新的 Go 程序入口"的原判断——原判断针对的是"要不要一个特殊的压测模式"，`runner` 只是把命令行步骤脚本化，Vegeta 和 `vmr report` 仍然是真正做事的工具，没有重新发明。
- **"几个典型组合"落地为三档递增负载**：`light`（10 req/s × 10s）/`moderate`（50 req/s × 20s）/`heavy`（150 req/s × 20s），一次运行里依次跑完三档，而不是只跑一个固定的 `-rate`。
- **报告落地为 `loadtest/report.md`**（Markdown，gitignored，按需重新生成）：第一段是三档负载各自的 Vegeta 客户端视角表（p50/p95/p99/max、成功率）；第二段直接把 `vmr report` 生成的 `按模型`/`端点可用度` 两张表原样嵌入——没有另写一套报告渲染代码，复用 `vmr report` 自己的 Markdown 输出。

**实测跑通的结果**（三档负载共 4100 个请求，11 个场景 × 3 轮）：三档全部 100% 成功率；客户端视角 p95 分别是 51ms/47ms/23ms（`heavy` 档反而更低，是三档共享同一批"探测阶段"冷启动开销被摊薄的正常现象，不是变快了）。服务端视角（`vmr report` 按模型表）：`big_image` p50/p95 请求耗时 20ms/47ms、`multi_image` 11ms/23ms，其余九个场景全部在 0-6ms 之间——跟 §0 的预期一致：**vmr 自己的路由/透传/归一化/协议适配开销可以忽略不计，唯一有实质成本的是图片降采样，且随图片数量线性增长（`multi_image` 三张图的开销约等于 `big_image` 一张大图的一半，量级上说得通）**。`端点可用度` 表确认 `failover` 场景的 `mock_fail1`/`mock_fail2`/`mock_ok` 三个端点都被走到。结论没变：**跑一次，数字都正常，不需要再往下细分（Tier 1 微基准继续不做）**。

实现过程中两个值得记录的教训：

1. **SSE chunk 边界影响正则匹配**：mock 上游最初把 `thinking_leak` 场景的"练习版"自我认可标记（"Looks good. Pro"）和"真正的"自我认可标记（"Looks good. Proceed"）写进了**同一个 SSE chunk**，导致 `stripThinkingProcess` 的正则（`[^\n]*`，只在真实换行处停止，JSON 转义的 `\n` 不算）把两个标记之间的内容当成一个整体匹配，剥离位置选错，思考文本没剥干净。修复方式是把两个标记拆进两个不同的 SSE data: 行（真实 MiniMax 流式返回时它们也确实是分开的两个 chunk，参见 `internal/router/response_test.go::TestStripThinkingProcess_MultipleEndorsements`）。
2. **`json.Marshal` 默认转义破坏字面量标签匹配**：新增 `think_tag` 场景时，mock 上游最初用裸 `json.Marshal` 序列化 SSE payload，把字面量 `<think>`/`</think>` 转成了 `<think>`，永远匹配不上 vmr 里按字节比较的 `thinkOpenMarker`/`thinkCloseMarker`。修复方式是改用 `json.NewEncoder` + `SetEscapeHTML(false)`（跟 vmr 自己的 `core.MarshalNoEscape` 用的是同一个模式，同一个原因）。
3. （附带的工程教训，非代码 bug）**`runner` 最初用 `go run ./loadtest/mockupstream` 启动 mock 上游，`Process.Kill()` 只杀得掉 `go run` 这层包装进程，杀不掉它 fork 出来的真正编译产物**，导致每次跑完都留一个孤儿进程占着端口。改成先 `go build` 出一个临时二进制、直接 exec 这个二进制后解决——这也是为什么 vmr 本体一直是用编译好的 `./vmr start` 而不是 `go run ./cmd/vmr` 来起的同一个原因，只是这次是 mock 上游踩了同一个坑。

---

Sources:
- [GitHub - tsenart/vegeta](https://github.com/tsenart/vegeta)
- [GitHub - grafana/k6](https://github.com/grafana/k6)
- [GitHub - phymbert/xk6-sse](https://github.com/phymbert/xk6-sse)
- [K6 vs Vegeta for performance testing](https://medium.com/@shehan.akhs/k6-vs-vegeta-for-performance-testing-88488bce22c2)
- [Use k6 or Oha for application load testing](https://docs.hotosm.org/decisions/0009-load-testing/)
