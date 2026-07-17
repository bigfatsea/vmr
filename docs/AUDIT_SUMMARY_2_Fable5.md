<!-- Ver 2026-07-16, by Fable 5 -->
# vmr — 审计 2.0 总结报告

姊妹文件:`AUDIT_REPORT_2_Fable5.md`(逐文件流水账,本文所有结论的出处)。上一轮:`docs/AUDIT_REPORT.md`(1.0,2026-07-15 原审 + 07-16 复核)。

---

## 一、任务 Debrief

**任务**:对仓库 `vmr`(commit `1d50611`)做一次全量重审——git 跟踪的全部 88 个文件、约 2.49 万行(生产 Go ~11.5k、测试 Go ~8.3k、文档 ~4.1k、脚本/配置 ~1k),逐文件精读,边审边落盘(防上下文漂移),记录所有大小问题,最终按 1.0 的三梯队逻辑分组。

**执行方式**:13 个批次(基础层→适配→健康/审计→配置→路由→服务→入口→图片→诊断/重放→报告→loadtest→文档/脚本→汇总),每批读完即写入流水账;对可疑结论做了实证验证(yaml.v3 重复 key 行为实测、`go vet`、全量 `go test`、五个并发密集包的 `-race`)。

**与 1.0 的关系**:1.0 覆盖过的文件本次全部重读,已修复项不再展开;仍成立的 1.0 发现以「(同 1.0 §x)」标注并复核状态;本次重点是 1.0 之后新增的面(loadtest/、UserGuide、Why_vmr、README 重写、PerformanceTesting)与 1.0 漏掉的问题。本次共产出 **~35 条新发现**(1 条 [S] 级都没有,7 条 [M],其余 [L]/[I]),**更正 1.0 一处错误记录**(yaml.v3 对重复 key 实际会报错,1.0 §1.D.1 [Q] 称"取最后一个"不成立),**确认 1.0 的 7 条 [M] 级遗留至今未修**。

---

## 二、项目总体评价

**结论先行:这是一个工程质量显著高于同规模个人项目的代码库,没有发现任何一条会导致数据丢失、凭证泄漏或服务不可用的 [S] 级缺陷。** 可以放心继续在生产使用;本报告的全部发现都属于"让它更好",而非"它有危险"。

分维度:

1. **正确性**:核心路径(failover 循环、半开探针三结局、响应归一化状态机、审计双层记录)逐行核对无逻辑错误;`go vet` 零告警、全量测试含 race 全绿。发现的正确性问题集中在**边界与低概率输入**(CRLF SSE、上游 3xx、正文含 `<think>` 字面量),以及一个真实的功能 bug(`vmr replay -stream` 不生效)。
2. **测试**:~8.3k 行测试 / 11.5k 行生产代码,server 包 57 用例的黑盒覆盖是全项目亮点;response.go 37 用例锁定每条归一化路径。缺口与 1.0 一致:`config/watch.go` 零测试、`vmr.sh` 零测试、RewriteModel 无 fuzz。
3. **文档**:"设计即文档"仍是最强实践——设计文档 §11 决策表连"反转过往判断"都留了痕。本次发现的文档问题都是**滞后而非错误**:KeyTag 窗口 6→8 漏改两处、一处已删文档引用漏标注、§5.5 对 think_strip 守卫的描述强于实现。7 处代码注释仍指向三份已删除的分析文档。
4. **新增面质量**:loadtest 模块(1.0 后新增)质量良好——mock fixture 与被测逻辑的对应关系注释充分,README runbook 连"数字怎么读"都写了;双语 README/UserGuide 抽查无中英漂移、无夸大宣传(除 -stream 一处跟着代码错)。
5. **遗留债务**:1.0 复核标记的 [M] 级问题(402/404 contentHint、imgprep panic 零观测、sanitizeName 碰撞、capStr 字节截断、needle 双面、watch.go 无测试)**一条都没修**——1.0 的第一/第二梯队建议在这一天里基本没有被消费,新工作(loadtest、文档重写)优先于旧修复。这本身值得 owner 注意:审计报告的产出如果不排进队列,复审只会重复确认同样的问题。

---

## 三、问题分组(三梯队)

> 划分标准沿用 1.0:严重度 = 修复紧迫性 × 修复成本。第一梯队 = 推荐尽快改(详述+方案);第二梯队 = 改与不改都合理(说明问题+简要方案);第三梯队 = 不动也无妨(点出即可)。

### 3.1 第一梯队(7 项,合计预估 ≤1 天工作量)

> **状态更新(2026-07-16 同日):本节 7 项已全部修复**,连同第二梯队第 2、3 条(capStr/snippet rune 安全、watch.go 测试);另按 owner 决定移除单把 `api_key` 只保留 `api_keys`(破坏性变更,校验给迁移报错)。逐项细节见 `AUDIT_REPORT_2_Fable5.md` 顶部"修复记录"。

#### 3.1.1 [M] `vmr replay` 的 `-stream` flag 完全不生效(2.0 新发现,`internal/replay/replay.go`)
- **问题**:`opts.Stream` 只写入 `creq.Stream`,而两个 Adapter 的 `BuildRequest` 都不读该字段——出站 body 的 `"stream"` 仍是审计记录原值,传不传 flag 行为完全相同;`writeReplayRecord` 也记录原值。`docs/UserGuide.md` L171 还在宣传它能 "force streaming on/off"。这是全项目唯一一个"承诺了但没实现"的用户可见功能。
- **方案(确定)**:二选一——(a) 在 replay 里对 body 顶层 `stream` 字段做改写(可走 `RewriteModel` 的 generic 重建路径,~20 行 + 测试);(b) 删掉 flag 与文档文案。倾向 (a):调试工具里"把流式请求改非流式重放"是真实需求。

#### 3.1.2 [M] `think_strip` 触发守卫不对称,存在低概率数据损坏向量(2.0 新发现,`internal/router/response.go`)
- **问题**:ThinkingProcess 剥离有"首个 content 值前缀"守卫,但 `<think>` 剥离的触发是 `bytes.Contains(事件任意位置)` + 对整个缓冲体 `ReplaceAll`。正文合法出现 `<think>...</think>` 字面量(用户请模型解释 think 标签、代码示例里输出该格式——在开发者工具场景并不罕见)会被**静默删除**。设计文档 §5.5 声称"误伤需响应开头恰好命中触发形态",描述强于实现。
- **方案(确定)**:给 think 路径加对称守卫——只在首个载荷事件的 content 值以 `<think>` 开头(可跳过转义空白)时才进入缓冲/剥离;或退一档只剥第一个块。同步修正设计文档 §5.5 的守卫描述。现有 37 个归一化测试是安全网,新增 2-3 个"正文中段含 think 字面量不被剥"的用例锁定。

#### 3.1.3 [M] 配置未知字段静默忽略(2.0 新发现,`internal/config/config.go`)
- **问题**:`yaml.Unmarshal` 非 strict,`max_concurency: 8` 这类拼写错误被无声丢弃,用户以为限流/超时生效实际没有。对"配置驱动"工具这是最常见的真实事故来源;项目现有的"坏配置拒绝加载"哲学恰恰要求这里 fail-fast。已实测确认。
- **方案(确定)**:`yaml.NewDecoder(bytes.Reader)` + `KnownFields(true)` 替换 `yaml.Unmarshal`(Parse 一处改动);跑一遍现有 config 测试确认无误伤。若担心向后兼容(用户 config 里可能有注释性质的自定义 key),至少在 `vmr check`/`diagnose` 里报告未识别 key。

#### 3.1.4 [M] `vmr report` 详单/索引文件权限与审计源矛盾(2.0 新发现,`internal/report/detail.go` 等)
- **问题**:审计日志刻意 0600/0700(明文对话防外泄),但由它派生的 `details/*.md|json`、各索引/报表文件全部 0644、目录 0755——同样的对话正文,多用户机器上全局可读。与项目自己的安全决策自相矛盾。
- **方案(确定)**:`WriteDetails`/`WriteRequests`/`cmdReport` 的 `os.WriteFile`/`MkdirAll` 统一改 0600/0700,~6 处字面量。

#### 3.1.5 [M] `imgprep.Downscale` 的 panic 恢复零观测(1.0 §4.17 遗留,仍未修)
- **问题**:`recover()` 静默丢弃并回退原字节,无日志、无 audit 标记。stdlib 解码器对对抗性输入 panic 时,降采样"永久失效"而运维完全看不见——项目里其他 fail-open 路径(如 overflow_raw_passthrough)都留痕,唯独这里没有。
- **方案(确定)**:recover 分支往 stderr 打一行 + 在返回的 ImageInfo(或新增一个包级计数)里留 `decode_panic_recovered` 标记,~10 行。

#### 3.1.6 [M] `classify.go` 402/404 跳过 contentHint(1.0 §1.B.2 遗留,仍未修)
- **问题**:四个 4xx 分支里唯独 402/404 不做内容嗅探,404 承载内容拒绝的 provider 会被误判 ErrEndpoint(10min 长冷却)而非 ErrContent(零惩罚)。
- **方案(确定)**:分支前加同款 `contentHint` 检查,~5 行 + 2 个表驱动用例。

#### 3.1.7 [M] `vmr report` 每记录 body 解析成本被桶数放大 6~8 倍(2.0 上修 1.0 §1.I.1)
- **问题**:`bodyBytes`(整 body re-marshal)与 `messageCount`/`roleChars`(整棵消息树遍历)在 addRecord×4 + addHour×2 + addEndpointRequest×2 里各算一遍,同一记录同样结果重复计算——GB 级日志下 `vmr report` 慢的主因(叠加 Build 与 AnalyzeSessions 各读一遍文件)。
- **方案(确定)**:record 循环里先算一次 `(bytesIn, bytesOut, msgCount, roleChars)` 打包传入各 add 函数,~30 行重构,行为零变化,现有 45 个测试全兜底。

### 3.2 第二梯队(改与不改都合理)

1. **[M] sanitizeName 后的 ClientKeyTag 碰撞静默覆盖导出文件**(1.0 遗留):`bob/eve` 与 `bob-eve` 清洗后同名,sibling jsonl/index 互相覆盖且无警告。方案:按清洗后文件名建 `used` 集合,碰撞时加后缀或 stderr 警告(export.go 与 detail.go 两处)。
2. **[M] `capStr` 字节截断非 rune 安全**(1.0 遗留)+ **diagnose `snippet` 同款**(2.0):中文内容截断产非法 UTF-8 进报表/诊断输出。方案:复用 render.go 已有的 rune 截断,两处替换。
3. **[M] `config/watch.go` 零测试**(1.0 遗留):热重载入口无覆盖。方案:t.TempDir + 写文件触发 fsnotify 的 1-2 个集成用例,~30 分钟。
4. **[M] SSE 事件分隔符只认 `\n\n`**(2.0):CRLF 框架(规范合法)的上游静默失去流式(整响应扣到 EOF/32MB),且无 applied 标记。方案:`eventSep` 兼容 `\r\n\r\n`,或至少检测到 CR 框架时记一个 applied 标记降级透传。现接入厂商都用 LF,休眠风险。
5. **[M] 上游 3xx 未显式处理**(2.0):http.Client 默认跟随重定向,POST 301/302/303 会变 GET——与直连等价原则相悖。方案:Transport 配 `CheckRedirect: 不跟随`,3xx 当普通响应透传;LLM API 现实几乎不发 3xx,休眠风险。
6. **[S/M] `stripThinkingProcess` 强绑 MiniMax wording**(1.0 §3.1.1,维持"功能决策"定性):不是可直接修的 bug;下一步具体切入点仍是加"识别到思考形态但 marker 不认识"的观测标记。
7. **[M] `needle` 200 字节上限的漏链无日志**(1.0 遗留):compaction 该链没链上时运行时不可见。方案:未匹配时 stderr/metric 留痕。
8. **[L] 429 的 quota 嗅探词过宽**(2.0):`credit`/`balance` 裸词命中即长冷却,真限流消息含 "credits per minute" 会被关 10 分钟。方案:词表收紧为 "insufficient X" 共现。
9. **[L] 已删文档引用清理**(1.0 观察 9,2.0 列出完整清单):7 处代码/脚本注释 + 设计文档 L126 死链 + L466/L509 的过时"末 6 位"(实为 8)。方案:一次 sweep,~15 分钟,顺带把 KeyTag 设计要点内联到设计文档确认完整。
10. **[L] `RewriteModel` 补 fuzz 测试**(1.0 遗留):splice 直接关联字节保真承诺,`testing.F` 半小时。
11. **[L] 单把 `api_key` 无最短长度校验**(2.0):1 字符 key 被静默接受。方案:同 api_keys 加下限或 `vmr check` 警告。
12. **[L] `normDescriptions` 缺 `opaque`/`overflow_raw_passthrough`**(2.0):详单渲染成"(未知步骤)"。补两行。
13. **[L] `writeRequestRows`/`writeReplayRecord`/gentargets 的 Close/Write 错误被吞**(1.0+2.0):磁盘满时"导出成功"但文件不完整。各加一层检查。

### 3.3 第三梯队(点出即可,不展开)

代码类:`strategy.Compare` 减法溢出(极端 priority);`ErrorClass.String()` default 兜底成 "transient" + 4 个审计枚举字符串无测试锁定;`Redact` 浅拷贝共享 value 切片;housekeep resume+retention 组合的误导性 ENOENT 日志;`ForEachLine` 超长尾行的 skip 语义;`${VAR}` 未定义静默空展开;reload 无串行化(已核实为良性竞态);`Bearer ` 前缀大小写敏感;未知模型请求占并发槽;旧日志 `canceled by client` 回退类名不归一;truncated 请求的用量不归任何端点行;`ms()` 恰 1000ms 边界;`RawPreStrip` 类型仍 `any`;`retentionDays` 包级全局;健康冷却参数硬编码;`IngressPath` 写死两协议;`containsSoftBlockMarker` 仅 2 标记;`reassembleSSE` 双实现;响应 model 改写不限层级且测试注释与实现不符;替换串 `$` 未转义;`sawDone` 字面量误置;客户端中途弃流在审计中与完整成功不可区分;scanner 接受畸形 JSON 依赖上游校验的隐式前提。

脚本/杂项类:vmr.sh 的 systemd ExecStart 未引号、env 文件值含空格解析失败、`find -newer` 因 loadtest 改动触发重建、IPv6 端口探测;loadtest 三处地址常量需人工同步、出错路径 SIGKILL 留假崩溃痕迹;README 的 admin/status 同机他人可读;`.gitignore` 的 `*.jsonl` 全局忽略对未来 fixture 的影响;go.mod 无 toolchain 指令。

---

## 四、未尽事宜(本次审计的边界与遗留)

1. **范围外未审**:`details/`(~2070 个历史 JSON/MD 审计快照)与 `logs/` 运行产物,按任务约定剔除;`_tmp/` 同。
2. **双语文档为抽查而非逐句比对**:README/UserGuide/Why_vmr 的中英版核对了结构、数字、命令与关键承诺(未发现漂移),但没有逐句对读——若要发布级保证,建议单独跑一次机器辅助 diff。
3. **动态验证的深度有限**:跑了 `go vet`、全量 `go test`、五个并发包的 `-race`(全绿),但**没有**实际重跑 loadtest(报告引用的 4100 请求实测数字来自 `docs/PerformanceTesting_Design_Sonnet5.md` §6 的记录)、没有对真实上游做端到端验证、没有 fuzz。
4. **未跟踪文件待 owner 处置**:仓库根目录的 `audit_report_v2_logs_pi_agent.md`(另一 agent 的并行审计稿,91KB)——建议决定入库/移 `_tmp/`/删除,否则 `git status` 永远不干净。本报告未参考其内容。
5. **1.0 遗留项的消化机制**:1.0 复核后的第一/第二梯队建议一天内零消化(见 §二.5)。建议把 3.1 节七项(合计 ≤1 天)排成一个明确的修复批次,修完后在 1.0/2.0 两份报告的对应条目回填状态——沿用 1.0 复核时"✅ 已修复/❌ 不成立/⚠️ 仍成立"的标注惯例,避免第三轮审计再度重复确认。
6. **本报告自身**:`docs/AUDIT_REPORT_2_Fable5.md` 与本文件均未提交,入库与否由 owner 决定(若入库,建议与 1.0 的 `AUDIT_REPORT.md` 放同级并在其头部互链)。
