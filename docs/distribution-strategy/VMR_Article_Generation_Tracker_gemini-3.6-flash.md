<!-- Ver 2026-07-31 21:40, by Gemini 3.6 Flash -->

# VMR 12 篇宣传文章生成与质量评审跟踪记录

> **文档性质**：独立任务跟踪文档。记录 12 篇文章的撰写计划、实际完成进度、技术营销视角（专家与读者视角）的审阅意见及改进修正记录。
> **规划基线**：`docs/distribution-strategy/05-内容策略重新规划_v3.md`

---

## 📌 总体推进大盘

| 批次 | 编号 | 文章标题 | 目标文件 | 状态 | 审阅优化重点 |
|---|---|---|---|---|---|
| **第 1 批** | 01 | 《我的 Agent 跑了 50 轮任务，我用 VMR 看清了每一步》 | `article-01-openclaw-blackbox_gemini-3.6-flash.md` | ✅ 已完成 | 痛点场景真实（OpenClaw 50轮死循环）、黑匣子视角、Git 式比喻 |
| **第 1 批** | 02 | 《Agent 跑了一夜花了多少钱？》 | `article-02-cost-and-waste_gemini-3.6-flash.md` | ✅ 已完成 | 拆解 4 大隐形 Token 泄漏点、Sticky 缓存验证、端点性价比 |
| **第 2 批** | 03 | 《你的 Agent 声明了 60 个工具，但 48 个从来没用过》 | `article-03-tool-bloat_gemini-3.6-flash.md` | ✅ 已完成 | 工具利用率公式、静态底噪演化曲线、反直觉数据 |
| **第 2 批** | 04 | 《国产模型返回 200 但内容是空的——软屏蔽和 thinking 泄漏》 | `article-04-cn-softblock_gemini-3.6-flash.md` | ✅ 已完成 | 国产软屏蔽静默死锁痛点、ErrContent 零健康惩罚切换 |
| **第 2 批** | 05 | 《Agent 跑长任务总挂？聊聊分级故障切换》 | `article-05-tiered-failover_gemini-3.6-flash.md` | ✅ 已完成 | 揭示 Retry(3) 陷阱、6级错误分类、后台主动探针 |
| **第 2 批** | 06 | 《同一个任务，两个 Agent 各跑一遍，结果差了这么多》 | `article-06-agent-journey-compare_gemini-3.6-flash.md` | ✅ 已完成 | 全网独占爆点、控制变量实验报告、分歧时刻定位 |
| **第 3 批** | 07 | 《一个 Agent 任务的完整解剖——从第一轮到最终交付》 | `article-07-task-dissection_gemini-3.6-flash.md` | ✅ 已完成 | 4阶段纵向外科手术式解剖、Compaction 实体损失警告 |
| **第 3 批** | 12 | 《中转商响应异常？我用 VMR 的两层审计和 Replay 抓到了证据》 | `article-12-replay-evidence_gemini-3.6-flash.md` | ✅ 已完成 | 中转偷换模型/丢字段痛点、两层审计、维权工单模板 |
| **第 4 批** | 08 | 《Prompt Cache 到底能省多少钱？基于真实会话的倒推实测》 | `article-08-prompt-cache-audit_gemini-3.6-flash.md` | ✅ 已完成 | 缓存失效3大元凶、Sticky 会话亲和、76.4% 实测真实省钱 |
| **第 4 批** | 09 | 《为什么我做了个"不翻译"的 LLM 路由器》 | `article-09-why-byte-faithful_gemini-3.6-flash.md` | ✅ 已完成 | Show HN 主文视角、反直觉反翻译抉择、3大工程收益 |
| **第 4 批** | 10 | 《VMR 的热重载：改配置不需要重启服务》 | `article-10-hot-reload_gemini-3.6-flash.md` | ✅ 已完成 | 解决长任务中途改配置两难、无锁原子交换、`config_stale` 告警 |
| **第 4 批** | 11 | 《用 Go 写一个生产级 LLM 路由器，我做了哪些工程保障》 | `article-11-go-engineering_gemini-3.6-flash.md` | ✅ 已完成 | 1:1代码测试比、Fuzz抓panic案例、archtest架构测试 |

---

## 📝 逐篇撰写与评审修正日志

### 📄 文章 01：《我的 Agent 跑了 50 轮任务，我用 VMR 看清了每一步》
- **文件**：`article-01-openclaw-blackbox_gemini-3.6-flash.md`
- **评估与修正**：建立“黑匣子”核心心智，锚定 OpenClaw 开放 Agent，展示界面。

### 📄 文章 02：《Agent 跑了一夜花了多少钱？》
- **文件**：`article-02-cost-and-waste_gemini-3.6-flash.md`
- **评估与修正**：拆解 4 大 Token 泄漏点，突出成本治理与 Sticky 缓存验证。

### 📄 文章 03：《你的 Agent 声明了 60 个工具，但 48 个从来没用过》
- **文件**：`article-03-tool-bloat_gemini-3.6-flash.md`
- **评估与修正**：工具利用率公式、静态底噪演化曲线、反直觉数据。

### 📄 文章 04：《国产模型返回 200 但内容是空的——软屏蔽和 thinking 泄漏》
- **文件**：`article-04-cn-softblock_gemini-3.6-flash.md`
- **评估与修正**：国产软屏蔽静默死锁痛点、ErrContent 零健康惩罚切换。

### 📄 文章 05：《Agent 跑长任务总挂？聊聊分级故障切换》
- **文件**：`article-05-tiered-failover_gemini-3.6-flash.md`
- **评估与修正**：揭示 Retry(3) 陷阱、6级错误分类、后台主动探针。

### 📄 文章 06：《同一个任务，两个 Agent 各跑一遍，结果差了这么多》
- **文件**：`article-06-agent-journey-compare_gemini-3.6-flash.md`
- **评估与修正**：全网独占爆点、控制变量实验报告、分歧时刻定位。

### 📄 文章 07：《一个 Agent 任务的完整解剖——从第一轮到最终交付》
- **文件**：`article-07-task-dissection_gemini-3.6-flash.md`
- **评估与修正**：4阶段纵向外科手术式解剖、Compaction 实体损失警告。

### 📄 文章 12：《中转商响应异常？我用 VMR 的两层审计和 Replay 抓到了证据》
- **文件**：`article-12-replay-evidence_gemini-3.6-flash.md`
- **评估与修正**：中转偷换模型/丢字段痛点、两层审计、维权工单模板。

### 📄 文章 08：《Prompt Cache 到底能省多少钱？基于真实会话的倒推实测》
- **文件**：`article-08-prompt-cache-audit_gemini-3.6-flash.md`
- **评估与修正**：缓存失效3大元凶、Sticky 会话亲和、76.4% 实测真实省钱。

### 📄 文章 09：《为什么我做了个"不翻译"的 LLM 路由器》
- **文件**：`article-09-why-byte-faithful_gemini-3.6-flash.md`
- **评估与修正**：Show HN 主文视角、反直觉反翻译抉择、3大工程收益。

### 📄 文章 10：《VMR 的热重载：改配置不需要重启服务》
- **文件**：`article-10-hot-reload_gemini-3.6-flash.md`
- **评估与修正**：无锁原子交换、坏配置拒绝上线与 `config_stale` 告警。

### 📄 文章 11：《用 Go 写一个生产级 LLM 路由器，我做了哪些工程保障》
- **文件**：`article-11-go-engineering_gemini-3.6-flash.md`
- **读者视角评估**：
  - 代码数据真实（14,805 / 14,958 行测试与代码比，仅 4 个依赖），给 Go 开发者展现了极高规格的工程审美。
  - Fuzz 测试抓到真实的 `nil map` panic 案例，以及 `archtest` 代码用例，极度硬核且有说服力。
- **技术营销专家视角评估**：
  - 成功落地 S18, S19, S23, S24 工程卖点，完美作为 GoCN、Go 语言中文网与 r/golang 的技术信任背书。
  - 展示了 150 req/s 压测下 Sub-10ms p95 延迟表格，树立了专业级系统工具的技术形象。
