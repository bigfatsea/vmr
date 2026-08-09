<!-- Ver 2026-08-09 15:30, by Opus 5 -->

# Quota-Aware Routing 交付状态总览（P1 + P2）

本文只回答一个问题：**`docs/TokenPlan_Quota_Routing_Design_opus-5.md` 描述的设计终态，
到今天实际落地了多少、还差什么、后面怎么办。**

- 设计终态与论证：`docs/TokenPlan_Quota_Routing_Design_opus-5.md`
- 分批落地计划与复盘：`docs/TokenPlan_Quota_P1_DevPlan_opus-5.md`（P1）、
  `docs/TokenPlan_Quota_P2_DevPlan_opus-5.md`（P2.1/P2.2，§12 为 2026-08-09 的交付后复核）

本文不重复上述文档的论证，只做状态核对；每一条状态都对着源码确认过，不是从计划文档推断的。

---

## 1. 一句话结论

**P1、P2 已全部交付并可用；P3、P4 未启动（P3 的触发条件本就是"有真实运行数据再判断"）。**
按设计文档 §14.1 自己数的"终态十四项机制"计，已落地十项、永久砍掉两项、剩两项在 P3/P4。
真正影响可用性的剩余缺口只有一处：**内置标准价目表的四分量完整率只有 7%**，
使得 `metric: cost` 在多数模型上必须靠账号覆盖（`providers[].pricing.overrides`）才配得起来——
这是设计文档 §4.2① 早已如实写明的风险，不是实施偏差，但需要用户明确知道。

---

## 2. 终态机制逐项核对

设计文档 §14.1 列了终态的十四项机制，逐项状态：

| # | 机制 | 状态 | 落点 / 说明 |
|---|---|---|---|
| 1 | 三种 metric（requests / tokens / cost） | ✅ 全部落地 | `core.QuotaMetric`；`router/quota.go` 的 `chargeQuota`/`chargeCost` |
| 2 | 多窗口 + `min()` 归并 | ⬜ P3 | 配置层显式拒绝多条 Limit（不是静默忽略），`config/quota.go` |
| 3 | 桶 / 闸角色 | ⬜ P3 | 单条 Limit 时它自己就是桶，判定逻辑尚未需要 |
| 4 | 环形分桶（rolling） | ⬜ P3 | `rolling: true` 是加载期"计划中"错误 |
| 5 | `(every, since)` 周期数学 | ✅ | `quota/period.go`，含月末截断/跨年/DST |
| 6 | `model_multipliers` | ✅ P2.1 | 账号级、**计费时**套用（`applyModelMultiplier`），向上取整 |
| 7 | `token_weights` | ✅ P2.1 | 账号级、**读取时**套用（`baseAmount`），缺省全 1.0 |
| 8 | Scope（`models:`） | ⬜ 降级为"有真实案例才做" | 设计文档 §14.1 已决定；配置层显式拒绝 |
| 9 | 标准定价表 + 生成脚本 | ✅ P2.2 | `internal/pricing/standard.generated.yaml`（721 行，`go:embed`）+ `tools/gen_standard_pricing` |
| 10 | per-provider 价格覆盖 + ID 映射 | ✅ P2.2 | `providers[].pricing` 的 `map` / `overrides`（`discount` / 显式费率 / `date_*`/`hour_*` 时间窗 / `"*"` 通配） |
| 11 | `Source` 抽象（官方用量 API） | ⛔ 永久砍掉 | 设计文档 §14.1 的决定：写第一个适配器时再抽（P4） |
| 12 | 持久化 + 惰性重置 | ✅ P1 | `quota/store.go` 的 `vmr-quota.json`（0600），5s flusher + 退出前强制 flush |
| 13 | 梯队占位重排 | ✅ P1 | `reorderByQuota`/`reorderTier`，只重排挂了 Limit 的成员 |
| 14 | usage 嗅探 + 降级估算 | ✅ P1 | `router/response.go` 的事件级门禁 + `tokenCharge` 的字节估算兜底 |

十四项里：**已落地 10、P3 待做 3（#2/#3/#4）、降级观察 1（#8）、永久砍掉 1（#11）**。

---

## 3. P1 / P2 范围逐条

### 3.1 P1 — 单桶均衡 ✅ 全部交付（2026-08-07）

| 范围项 | 状态 |
|---|---|
| `providers[].quota.limits` 恰好一条；`metric: requests\|tokens`；`every` + `since`；`amount` | ✅ |
| `requests` 每次成功 +1；`tokens` = usage 嗅探 + 降级估算 + `estimated` 占比 | ✅ |
| 仅 tumbling 窗口，惰性比较周期 key 重置 | ✅ |
| `Counters` 按四分量原始值存 + 原子落盘 + 启动加载 | ✅ |
| `headroom` 算法 + 贪心降序 + 梯队占位重排 + sticky 覆盖 | ✅ |
| `/admin/status` quota 段（含四分量明细）、`X-VMR-Route-Reason` 的 `pick=quota`、`vmr check` 打印（含生效时区） | ✅ |

三条验收全部通过，证据见 P1 计划 §9。

### 3.2 P2.1 — 账号级折算 ✅ 全部交付

| 范围项 | 状态 |
|---|---|
| `token_weights`（四分量，缺省全 1.0，零值陷阱有专项测试钉住） | ✅ |
| `model_multipliers`（`"*"` 通配，缺省 1.0，只作用于 requests/tokens） | ✅ |
| 计费时（非读取时）套用 `model_multipliers`，聚合语义有测试钉住 | ✅ |
| 越界配置校验（配在无对应 metric 的账号上必须报错） | ✅ |
| `vmr check` / `/admin/status` / `vmr status` 展示生效倍率与权重 | ✅ |

### 3.3 P2.2 — cost 计量 + 定价基础设施 ✅ 全部交付

| 范围项 | 状态 |
|---|---|
| 新叶子包 `internal/pricing`：`Rate`/`Table`/三层解析/四步 canonical key 解析/`RateAt` 时间窗 | ✅ |
| `go:embed` 双表（`standard.generated.yaml` + `standard.curated.yaml`），脚本只写前者 | ✅（curated 现有 8 条国产第一方厂商条目，见 §4） |
| `tools/gen_standard_pricing` 生成脚本（per-token → per-1M，缺失分量不补 0） | ✅ |
| 用户补充表 `pricing.supplement`（路径给了但文件不存在 = 加载期错误） | ✅ |
| `providers[].pricing` 的 `map` + `overrides` | ✅ |
| `metric: cost`：`Counters.Cost` 计费时定价、事后改价不改历史 | ✅ |
| 四项费率不齐 → 加载期错误（`AllPathsComplete`，比设计文档字面更严） | ✅ |
| `vmr report` 迁移到 `internal/pricing`，`costFor` 订正含 `cache_read` | ✅ |
| 破坏性变更：删除 `pricing.yaml` 与 `vmr report -pricing` | ✅ |

---

## 4. 已知缺口与未落地事项

按"是否影响可用性"排序，不按批次。

### 4.1 影响可用性

**① 内置标准表的四分量完整率只有 7%（729 行里 52 行齐全）。**
`metric: cost` 的加载期门槛是"四项费率全部有值"（缺失当 0 会低估消耗 → 超支，是最坏失效方向），
所以绝大多数模型**光靠内置表配不出 `metric: cost`**，必须写 `providers[].pricing.overrides`
的显式四分量费率。上游数据本身就是这样（2988 条里只有 240 条四项齐全，且缓存费率主要集中在
西方厂商），设计文档 §4.2① 与 §13 已如实写明"标准表是消除入门断崖的基线，不是 `cost` 的充分数据源"。
**当前的实际含义**：标准表对 `vmr report` 的 $ 估算价值很高（`costFor` 缺分量按 0 计，是尽力而为），
对 `metric: cost` 则主要是省掉输入输出两项、缓存两项仍多半要手写。
**2026-08-09 更新**：`standard.curated.yaml` 已从空文件补上 8 条国产第一方厂商条目
（Moonshot/Kimi 三档、Zhipu/GLM 两档、Volcengine/Doubao 两档、Tencent Hunyuan 一档，均逐条核对官方定价页并注明来源与日期）——
均补的是标准表完全没收录、或刚发布还没被上游快照收录的当前旗舰模型，全部因 `cache_write`（部分还缺 `cache_read`）
未公开而**仍不满足四项齐全**，因此这批新增没有改变 52/729 这个"完整"计数，价值主要在 `vmr report` 的尽力而为估算上
（从完全没有价格到有 2-3 个分量），`metric: cost` 场景仍需账号覆盖补齐缺口。
逐步补齐更多国产第一方厂商的四分量费率仍是本项唯一的实质解法，属于持续的数据维护工作，不是代码工作。

**② `vmr replay` 消耗真实上游额度但不计费。**
P1 计划 §8 第 2 项，已从"永久盲区"改判为"近期跟进任务"，但**至今未落地也未排期**。
影响：开发者高频用 `vmr replay` 重放长上下文请求时，本地计数与上游真实剩余持续静默漂移。
修法成本低（一次性 `Registry` 加载 + 成功后计费 + 退出前 flush，不需要后台 flusher）。
**建议**：作为下一个独立小任务处理，不必等 P3。

### 4.2 不影响可用性，但与设计文档有落差

**③ 报表的"溯源可见"只做到聚合级，没做到逐行级。**
设计文档 §4.2③ 护栏 #2 要求"报表免责声明要能说明**每行费率**来自标准表 / 补充表 / 账号覆盖的哪一层"。
现状：`vmr report` §2 只给一个汇总（标准表生成日期 + 补充表路径 + override 条数），
单行 $ 数字看不出它走的是哪一层。
**判断**：真要做需要在 `pricing.Resolve` 的返回值里带上来源标记，并一路穿到 `report` 的行结构里
——不是小改动，而收益是"事后追问某一行为什么是这个价"，属于低频需求。
**建议**：不单独立项，等 P4 做 `section_quota.go` 时一并考虑（那时报表已经要改一轮）。

**④ 生成脚本的 canonical key 做了归一化，不是上游 JSON 的顶层 key 原文。**
脚本产出的 key 统一是 `<litellm_provider>/<basename>`（如 `anthropic/claude-3-7-sonnet-20250219`），
而上游对该模型的顶层 key 是裸的 `claude-3-7-sonnet-20250219`（2988 条里 563 条是裸 key）。
**后果**：一是四步自动解析的第 ③ 步（裸模型名查表）对内置表恒不命中，实际由第 ④ 步（唯一后缀匹配）承担
——功能没问题，只是有一步是冗余的；二是设计文档 §4.2② "键对齐使得用户的补充表可以直接回贡上游"
这条理由被削弱（键空间不再逐字相同）。
**判断**：归一化对 vmr 自己的解析更好（消歧、去重都靠它），改回去反而更差。
**建议**：不改代码，把设计文档 §4.2② 的"可直接回贡"措辞在下次修订时降级为"结构可机械转换后回贡"。

**⑤ `ParseTable` 不校验表内费率的正负与有限性。**
账号覆盖那一层（手写的一两条 override）已经在 2026-08-09 复核里补了校验，
表文件这一层没有——因为标准表是脚本产出 + `go:embed` 的受控数据，补充表是用户自备的完整表，
两者要出现负数/NaN 都得先绕过生成脚本或手工构造整表。
**建议**：登记备查，ROI 不足以现在做。

### 4.3 按计划就该在后面做的

- **P3（多约束）**：多条 Limit + `min()` 归并、桶/闸角色、rolling 环形分桶、Scope。
  **触发条件未满足**——设计文档明确要求"P1/P2 上线后的运行数据显示 429 频率或尾延迟代价确实值得"，
  不是排期到了就做。立项时还需一并复核设计文档 §14.3 记的两个开放问题
  （桶/闸分层是否值得保留、Ring 是否值得给所有账号做）。
- **P4（校准与看板）**：`vmr report` 的 `section_quota.go`、官方用量 API 适配器（写它时才抽 `Source`）、
  标准表刷新纳入定期流程。**未排期。**
  其中"标准表刷新纳入定期流程"是当前最接近可以立刻做的一项——脚本已经有了，
  缺的只是把"什么时候跑、谁跑"定下来。

---

## 5. 2026-08-09 交付后复核修掉的问题

明细见 `docs/TokenPlan_Quota_P2_DevPlan_opus-5.md` §12，此处只列清单：

1. `metric: tokens` 的 `estimated_pct` 分子分母单位不一致（配了 `token_weights` 才暴露）；
2. `pricing.exchange_rate` 未校验正负/有限性；
3. 所有新增数值字段只做 `<= 0` 符号检查，`.nan`/`.inf` 能通过 `vmr check`；
4. 非 USD 币种下 `vmr report` 对"没有 `pricing:` 块"的 provider 会"标 CNY、算 USD"；
5. 标准表新鲜度护栏只做了报表一半，`vmr check` 没有生成日期也没有过期提示；
6. `AllPathsComplete` 会校验不可达的 override，可能 false rejection；
7. `pricing.map` 写错 canonical key 时静默回退到自动解析，可能匹配到别的模型的价格；
8. `config.example.yaml` 实际通不过 `vmr check`（既有问题，P2 DoD 曾据此打勾）。

---

## 6. 当前验收证据

- `go build ./... && go vet ./... && gofmt -l . && go test ./... -race` 全绿；
  `go test ./internal/archtest/...` 全绿（导入边界 + 文件行数预算）。
- `vmr check -c config.example.yaml` 在干净环境（无 `https_proxy` 环境变量）下输出 `=== OK ===`。
- 真实端到端复验（mock 上游 + `vmr start` + 真实 HTTP 请求）：
  - `metric: cost` 账号计费 `used=639.0000/5000.0000 CNY`，手工验算 = (1M in × 15 + 1M out × 75) USD × 7.1；
  - `vmr report` 对**两个** provider（一个配了 cost 额度、一个什么都没配）都给出 639.0000 CNY
    ——即 §5 第 4 条修复后的正确行为。
