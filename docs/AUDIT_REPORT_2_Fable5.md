<!-- Ver 2026-07-16, by Fable 5 -->
# vmr — 全量审计报告 2.0(过程跟踪)

> **范围**:仓库 `vmr` 全部受版本控制的代码与文档(剔除 `details/`、`logs/` 历史产物),commit `1d50611`(add load testing)。
> **方法**:逐文件精读、逐批落盘记录;每条发现带严重度标记。汇总与三梯队分组见姊妹文件 `AUDIT_SUMMARY_2_Fable5.md`。
> **审计时间**:2026-07-16,by Fable 5。
> **与 1.0 的关系**:`docs/AUDIT_REPORT.md`(2026-07-15 原审 + 2026-07-16 复核)是上一轮;本报告为独立重审,不复制 1.0 条目,但对 1.0 已记录且仍成立的问题会以「(同 1.0 §x)」标注,重点补充 1.0 未覆盖的新文件(loadtest/、UserGuide、Why_vmr、PerformanceTesting 等)与新发现。

严重度:`[S]` 严重 / `[M]` 中等 / `[L]` 轻微 / `[I]` 信息性 / `[Q]` 质疑待确认。

> ## 修复记录:2026-07-16(同日),by Fable 5
>
> 本报告第一梯队 7 项与第二梯队 2 项已全部修复(全量测试 + race 通过,文档同步更新);另按 owner 决定移除了单把 `api_key`(只保留 `api_keys`,破坏性变更,校验层给迁移报错;本地 config.yaml 已迁移)。逐项状态:
> - ✅ **1.B.2** 402/404 先过 contentHint(+2 表驱动用例)
> - ✅ **1.D.1** 配置严格解析 `KnownFields(true)`(未知键拒绝加载,+`TestUnknownFieldRejected`);单把 api_key 连同其无下限问题一并移除
> - ✅ **1.D.2** watch.go 补 3 个集成测试(写入/原子替换/无关文件不触发)
> - ✅ **1.E.2** think_strip 前缀守卫对称化(`thinkShapeGuard`:首个非空 content/text 值以 `<think>` 开头才触发;+3 回归用例含 anthropic text 面);设计文档 §5.5 同步修正
> - ✅ **1.H.1** imgprep panic 恢复留 stderr 痕迹(+可触发 panic 的 fake 图片格式测试)
> - ✅ **1.I.1** diagnose `snippet` rune 安全截断(+测试)
> - ✅ **1.I.2** `vmr replay -stream` 真正生效(`adapter.RewriteStream` 顶层 splice,--record 同步;+单测与端到端测试)
> - ✅ **1.J.1** report 每记录 body 解析只算一次(`recStats` 单次计算传各桶)
> - ✅ **1.J.6** `capStr` rune 安全(+测试)
> - ✅ **1.J.7** report 全部产物 0600/目录 0700(+权限断言);`normDescriptions` 缺项**未**在本轮处理
> - ✅ **1.L.1(部分)** 7 处已删文档死引用全部清理;设计文档"末 6 位"→8(两处)、§4.3 死链移除
> - ✅ **1.L.2** UserGuide 的 -stream 文案随实现转真
>
> 未动的项(仍开放):1.E.1 上游 3xx、1.E.2 CRLF SSE、1.J.3 tag 碰撞、1.J.6 needle 双面、第三梯队全部。

---

## 0. 项目结构与统计

- **待审文件**:git 跟踪文件(剔 details/logs)共 88 个;总行数 ~24,912(含测试与文档)。
- **生产 Go 代码** ~11,500 行,**测试 Go 代码** ~8,300 行,**docs+README** ~4,100 行,**脚本/配置** ~1,000 行。
- **依赖**:`fsnotify v1.10.1`、`klauspost/compress v1.19.0`、`golang.org/x/image v0.43.0`、`yaml.v3 v3.0.1`(+`x/sys` indirect)。go.mod 声明 go 1.25.1,本机 go1.26.5。
- **相对 1.0 的新增面**:`loadtest/`(runner/mockupstream/gentargets + config + README)、`docs/PerformanceTesting_Design_Sonnet5.md`、`docs/UserGuide{,.zh}.md`、`docs/Why_vmr_over_LiteLLM{,.zh}.md`。

模块行数(生产代码):report ~5,655 | router ~1,279 | server ~404+57 | cmd ~694 | imgprep ~595 | replay ~450 | diagnose ~391 | audit ~650 | config ~419 | adapter ~460 | health ~166 | strategy ~76 | core ~135 | rundir ~60 | loadtest ~705。

---

## 1. 逐文件评审(流水账)

### 1.A 基础层(仓库杂项 / rundir / strategy / core)

#### 1.A.1 `go.mod` / `go.sum` / `LICENSE` / `.gitignore`
- **内容**:module `vmr`,go 1.25.1;4 直接依赖 + `x/sys` indirect;go.sum 12 行含 `gopkg.in/check.v1`(yaml.v3 的测试依赖,正常)。LICENSE 标准 MIT(2026 Stanford)。`.gitignore` 注释解释了每组忽略项的原因(details/ 含完整对话体绝不提交、`*.jsonl` 全局忽略防审计数据入库)。
- **问题**:
  - `[I]` go.mod 声明 1.25.1,本机 toolchain 1.26.5 —— 无 `toolchain` 指令,构建取决于本机版本,单人项目可接受。
  - `[L]` `.gitignore` 的 `*.jsonl` 全局忽略:未来若想提交 JSONL 测试 fixture 需 `!` 白名单,目前无此需求(现有测试全部用 t.TempDir 生成)。

#### 1.A.2 `internal/rundir/rundir.go`(60 行)+ 测试(43 行)
- **内容**:三层目录兜底(~/.vmr → TempDir/vmr_x → cwd);纯函数 `resolve` 注入测试;文档注释解释为何不用 TMPDIR(macOS 3 天清理)。
- **问题**:
  - `[I]` `cwd()` 失败回 `"."`,行为稳定,可接受(同 1.0 §1.A.1)。
  - 本次重审无新发现;代码与测试均干净。

#### 1.A.3 `internal/strategy/strategy.go`(76 行)+ 测试(34 行)
- **内容**:Dimension 注册中心 + priority 维度;`sort.SliceStable` 保平手配置序;重复 Register panic(启动期一次性)。
- **问题**:
  - `[L]` `priority.Compare` 用 `a.Priority - b.Priority`,极端 int 值(如 config 写 `priority: -9223372036854775808`)相减会溢出反转排序。config 校验不限 Priority 取值范围(见 1.D)。现实无人这么写,但改成 `cmp.Compare(a.Priority, b.Priority)` 零成本消除。(2.0 新发现)
  - `[Q]` stateful 维度(round_robin)的并发安全约定仍只有注释承诺,无实现无测试(同 1.0 §1.A.2)。等真加时再管。

#### 1.A.4 `internal/core/core.go`(135 行)+ 测试(123 行)
- **内容**:`MarshalNoEscape` / `WriteJSON`/`WriteError`(2026-07-16 从 router/server 合并而来)/ `CanonicalRequest` / `ErrorClass`(10 枚举:6 分类 + 4 审计专用)/ `Endpoint` + `HealthKey`/`Name`。
- **问题**:
  - `[L]` `WriteJSON` 丢弃 `Encode` 错误——`map[string]any` 编码几乎不会失败,网络写失败也无从补救,可接受(同 1.0 §3.1.3 修复后遗留形态)。
  - `[L]` `ErrorClass.String()` 的 `default` 返回 `"transient"`——`ErrTransient` 本身没有显式 case,任何未来新增枚举若忘写 case 会静默变成 "transient" 而非编译/测试报错。建议给 `ErrTransient` 显式 case + default 返回 `"unknown"`,或加 exhaustive 测试。(2.0 新发现,轻微)
  - `[L]` `core_test.go::TestErrorClassString` 只覆盖 6 个分类值,`build`/`network`/`canceled`/`truncated` 四个审计专用值的字符串没有测试锁定——而 report 包按这些字符串归桶,改字符串会静默破坏报表分类。建议补进 map。(2.0 新发现)
  - `[I]` `HealthKey` 用 sha256 前 4 字节指纹,碰撞概率 2^-32 级,可忽略(同 1.0)。

### 1.B 适配层(adapter / classify / openai / anthropic)

#### 1.B.1 `internal/adapter/adapter.go`(71 行)
- **内容**:三方法接口 + database/sql 式注册中心;`BuildRequest` 多返回出站 body 字节(注释明确"返回后不可变更",供审计零拷贝引用)。
- **问题**:
  - `[I]` 接口/注册干净,无新发现。`Names()` 排序输出,供 config 校验错误信息用。

#### 1.B.2 `internal/adapter/classify.go`(269 行)+ `classify_test.go`(179 行)
- **内容**:`DefaultClassify`(status+32KB body 嗅探 → ErrorClass)、`contentHint` 中英双语词表、`RewriteModel` 字节 splice(手写 mini scanner:`topLevelModelValues`/`skipJSONString`/`skipJSONValue`)+ generic 回退。测试 8 用例 + 2 benchmark,覆盖转义引号、嵌套 key、字节保真、零拷贝、缺 key 回退、32KB 截断边界。
- **问题**:
  - `[M]` **402/404 无条件归 ErrEndpoint,跳过 `contentHint` 前置检查**,与 403/429/generic-4xx 分支不一致——某 provider 用 404 承载内容拒绝时会误罚端点健康(同 1.0 §1.B.2 2026-07-16 新发现,**仍未修**)。约 5 行改动。
  - `[L]` **429 的 quota 嗅探词过宽**:`"credit"`/`"balance"` 命中即归 ErrEndpoint(10min 起长冷却)。真限流消息若含 "credits per minute remaining" 这类措辞会被误判成额度耗尽,把只需按 Retry-After 短冷却的端点关小黑屋 10 分钟。建议词表更具体("insufficient credit"/"insufficient balance")或要求与 "insufficient|exceeded" 共现。(2.0 新发现)
  - `[L]` `contentHint` 的 `"sensitive"` 会命中 "case-sensitive" 这类无关措辞——已知取舍(误判仅多一次无害切换),记录在案即可。
  - `[L]` scanner 对畸形 JSON(前导/尾随逗号 `{,}`)会接受并返回 ranges——当前入口均有 `json.Unmarshal` 先行校验,实害为零;但 scanner 的"宽进"语义依赖上游校验这个隐式前提,值得一行注释。 (2.0 新发现)
  - `[L]` 1.0 建议的 fuzz 测试(`testing.F`)仍未补——splice 直接关联字节保真承诺,fuzz 收益高、成本半小时(同 1.0 §1.B.2)。
  - `[I]` `classifySnippetBytes=32KB` 的 lean-large 论证 + 超界不可见的测试锁定,好。

#### 1.B.3 `internal/adapter/openai/openai.go`(57 行)+ 测试(85 行)
- **内容**:透传 Adapter:TrimRight(base)+`/chat/completions`、Header 拷贝后 `Set` Content-Type/Authorization 覆盖;`ep.APIKey==""` 时不发 Authorization(本地 llama.cpp 场景)。测试含 22 用例分类表 + body 一致性断言。
- **问题**:
  - `[I]` 无新发现;`GetBody` 双读断言保证审计引用与实发字节一致,好。

#### 1.B.4 `internal/adapter/anthropic/anthropic.go`(64 行)+ 测试(93 行)
- **内容**:同上但 `/messages` + `x-api-key` + 缺省 `anthropic-version: 2023-06-01`;529→ErrTransient。测试锁定客户端自带 version/beta 头不被覆盖。
- **问题**:
  - `[L]` 默认 version 硬编码 2023-06-01(同 1.0)——Anthropic 向后兼容,维持现状合理。
  - `[I]` 两个 Adapter 的 Header 拷贝/覆盖逻辑逐行一致,重复约 20 行——体量太小不值得抽公共函数,记录即可。

### 1.C 健康与审计层(health / audit)

#### 1.C.1 `internal/health/health.go`(166 行)+ 测试(149 行)
- **内容**:被动健康状态机——transient(2s 起 ×2 封顶 5min)/long(10min 起封顶 1h)双冷却曲线,Retry-After 尊重但封顶 1h,半开单飞探针(Acquire/ReportSuccess/ReportFailure/ReportNeutral 三结局),`Status` 供 /admin/status。全局互斥锁保护 map。测试 9 用例覆盖退避曲线、cap、Retry-After、半开单飞、Neutral 不加深、Success 重置、48h Retry-After 封顶。
- **问题**:
  - `[I]` `Status.CooldownUntil` 用 `omitzero` tag(Go 1.24+ 特性),与 go.mod 1.25.1 一致。
  - `[L]` 冷却参数硬编码不可配(同 1.0 §3.2.1)——"零调参"是设计选择,维持。
  - `[L]` `Status.LastError` 只暴露类名不含原始错误详情,排障需查审计(同 1.0 §1.C.1)。
  - `[I]` `backoff` 循环 O(fails) 但 `d >= cap` 提前返回,fails 无限增长也只迭代 ~9 次,无性能问题。本次重审此包无新发现——是全项目最干净的包之一。

#### 1.C.2 `internal/audit/audit.go`(406 行)+ `audit_test.go`(195 行)
- **内容**:Record/Exchange/Attempt/ImageInfo/Message 数据结构 + `OutcomeFor` + `Redact`/`mask`/`IsCredentialHeader` + `KeyTag`(尾 8 字符窗口 + 末连字符规则,注释 5 例)+ Logger(JSONL 日轮转、午夜切文件触发 housekeeping、Close 后拒写不重开)。
- **问题**:
  - `[L]` `EncodeBody` 引用不拷贝——1.0 记 [M],现注释已把 ownership contract 写得非常明确(列举全部 5 个调用方),降级 `[L]`:约定仍靠 review 维持,但已是"文档化的约定"。
  - `[L]` `Attempt.RawPreStrip` 类型仍是 `any` 而非 `json.RawMessage`(同 1.0 §1.C.2),消费端要类型断言。
  - `[L]` `retentionDays` 包级全局 atomic(同 1.0 §3.2.2)——main.go reload 路径已确认调用 `SetRetentionDays`,但无测试锁定该不变量。
  - `[L]` `Redact` 浅拷贝 header map:非凭证 header 的 value 切片与原 map 共享底层数组——当前无调用方在 Redact 后原地改 header 值,实害为零;严格起见可 clone 切片。(2.0 新发现,理论性)
  - `[I]` `Write` 的 marshal 在锁外、fd 写在锁内,O_APPEND 单次 write 原子,JSONL 完整性有保障。`Logger.Path()` 无锁读不可变字段,安全。
  - `[I]` `KeyTag` 的 10 用例测试与注释例子一一对应,好。

#### 1.C.3 `internal/audit/housekeep.go`(148 行)+ 测试(158 行)
- **内容**:单次 ReadDir + 文件名日期正则;昨日以前明文压 zstd(tmp+rename+确认后删原文件,crash-safe 可续跑);retention>0 时删过期文件(同轮先压后删)。错误全部 stderr 打印后吞掉。
- **问题**:
  - `[L]` **resume+retention 组合会产生一条误导性 stderr 错误**:目录里同时存在 `X.jsonl` 与 `X.jsonl.zst`(压缩中断残局)且该日期超保留期时,ReadDir 会列出两个条目——处理明文条目时 resume(删原文件)+purge(`X.jsonl.zst` 被删),随后处理 `.zst` 条目时 `purgeOne` 再删一次,`os.Remove` 报 ENOENT 打一条 "retention: remove …: no such file" 错误日志。无实害(文件确实清干净了),但日志会让人误以为清理失败。加一个 `os.IsNotExist` 静默分支即可。(2.0 新发现)
  - `[I]` `compressFile` 的 `defer Close` 错误合并 + `out.Sync()`,fsync 语义正确。
  - `[I]` retention 用文件名日期与本地"今天"计算 cutoff,`t.Before(cutoff)` 严格早于——边界(恰好等于 cutoff 那天)保留,语义合理。

#### 1.C.4 `internal/audit/read.go`(96 行)+ 测试(109 行)
- **内容**:`MaxLogLine=128MB`;`OpenLogFile` 透明解压 `.zst`;`ForEachLine` ReadSlice 循环 + 超长行有界排空 + onSkip 回调,行缓冲复用。
- **问题**:
  - `[I]` 逻辑核对无误:`tooLong` 置位后 `buf[:0]` 保证内存有界;EOF 前无换行的尾行正常回调;测试覆盖全部路径。
  - `[L]` 末行恰好超长且无换行时也计一次 skip——语义上"截断的半行"与"完整超长行"不区分,对 parse_errors 计数无实质影响。(2.0 新发现,信息性)

### 1.D 配置层(config / watch)

#### 1.D.1 `internal/config/config.go`(366 行)+ 3 个测试文件(372+135+93 行)
- **内容**:`Load`/`Parse`(expandEnv → yaml → applyDefaults → validate)、`Provider`(Proxy 三态指针)、`ModelConfig`(ImageDownscaleMaxPx 指针三态)、`Duration` YAML 解析、`ProxySpecFor`(按 scheme 选代理、无 env 回退)、`expandTilde`。测试 ~30 用例覆盖默认值、钳制、三态、代理矛盾校验、目录解析。
- **问题**:
  - `[M]` **未知配置字段静默忽略(typo footgun)**:`yaml.Unmarshal` 到结构体不启用 strict 模式,`max_concurency: 8`(拼错)这类 key 被无声丢弃,用户以为限流生效实际没有。对一个"配置驱动"的工具这是最常见的真实事故来源。修法:用 `yaml.NewDecoder` + `KnownFields(true)`,或至少在 `vmr check`/`diagnose` 里报告未识别的顶层 key。(2.0 新发现,已实测确认:`unknown_key` 解析零报错)
  - `[更正 1.0]` 1.0 §1.D.1 [Q] 称"重复 provider 名 yaml 取最后一个,validate 不报错"——**实测不成立**:yaml.v3 对同一 mapping 的重复 key 直接报 `mapping key "a" already defined`,配置加载即失败。1.0 该条记录有误,2.0 划掉。
  - `[L]` 单把 `api_key`(catch-all)没有最短长度校验(`api_keys` 强制 ≥16)——它不进 KeyTag 无泄漏风险,但 1 字符的 api_key 会被静默接受,鉴权强度形同虚设。建议同样加下限或至少警告。(2.0 新发现)
  - `[L]` `${VAR}` 未定义时静默展开为空串(测试锁定为预期行为)——对 `api_key: ${TYPO}` 场景意味着上游 401 而非本地报错,排障绕路;`vmr diagnose` 的 api_key 状态检查是现有缓解。维持现状可接受,记录。
  - `[L]` `EndpointConfig.Priority` 取值无范围校验,极端值联动 strategy 包的减法溢出(见 1.A.3)。
  - `[I]` `applyDefaults` 的 map 值回写 idiom、proxy 矛盾在 validate 拒绝(而非运行时警告)、`expandTilde` 不吞 `~user`——设计与测试均到位。

#### 1.D.2 `internal/config/watch.go`(53 行)
- **内容**:fsnotify 监听父目录 + 300ms 防抖 + Write/Create/Rename 过滤。
- **问题**:
  - `[M]` **仍无任何单测**(同 1.0 §3.2.3,热重载入口零覆盖)。
  - `[L]` 防抖 timer 的 `Stop()` 竞态:timer 已触发但 `onChange` 尚在执行时新事件会再排一个 timer,两次 `onChange` 可能并发执行——是否安全取决于 main.go 的 reload 是否幂等/串行(见 1.G 交叉验证)。(2.0 新发现,待 1.G 结论)
  - `[L]` `watcher.Errors` 静默丢弃(同 1.0)。

### 1.E 路由层(router / response)

#### 1.E.1 `internal/router/router.go`(689 行)+ `router_test.go`/`router_proxy_test.go`
- **内容**:ModelRoute(EffectiveOrder/EffectiveImageDownscaleMaxPx nil-safe)、Snapshot(按代理解析分组共享 http.Client)、BuildSnapshot、Install(原子换快照 + CloseIdleConnections)、并发闸(limiter 信号量 + waiting/inFlight 计数)、Serve(健康过滤→排序→failover 循环)、tryOne(build/网络/4xx/成功四路径 + 探针三结局释放)、copyFlush(32KB chunk + idle watchdog goroutine)、parseRetryAfter、IngressPath。
- **问题**:
  - `[M]` **上游 3xx 的处理未显式考虑**:`http.Client` 默认跟随重定向(POST 301/302/303 会变 GET!307/308 保持 POST 并用 GetBody 重放;跨域时 Go 会剥 Authorization 但不剥 x-api-key 自定义头——实际上 Go 只剥 sensitive headers 到不同 host)。未跟随的 3xx(如 300/304)落进 `status < 400` 成功分支,记 ReportSuccess 并透传。LLM API 现实中几乎不发 3xx,但一旦某 provider 迁移域名发 301,行为是"静默改写为 GET 后请求新地址"而非透传 301——与直连等价原则相悖。建议 Transport 层 `CheckRedirect: 不跟随`,把 3xx 当普通响应透传。(2.0 新发现)
  - `[L]` `Serve` 在首次 `Install` 前被调用会 nil 指针 panic(`snap.Models`)——main.go 的启动顺序保证 Install 先于 ListenAndServe(见 1.G 交叉验证),纯防御性备注。
  - `[L]` 客户端在流中途断开时(2xx 已提交),`copyErr` 因 `r.Context().Err() != nil` 不记 truncated,attempt 无任何标记,审计里与完整成功不可区分(outcome 仍为 ok,`canceled` 仅在未写出任何响应时才断言)。想区分"客户端半途弃流"需比对 body 完整性。低优,记录语义边界。(2.0 新发现)
  - `[I]` `copyFlush` 的 timer Stop/drain/Reset 模式逐行核对无误;watchdog 超时后由 `tryOne` 的 `defer body.Close()` 解除 reader goroutine 阻塞,goroutine 无泄漏。
  - `[I]` 4xx body 读取带 stream_idle watchdog + 128KB cap + 审计侧独立截断标记(1.0 §3.1.5 修复形态),核对与设计文档 §9.2 约定 1 一致。
  - `[I]` `installLimiter` 容量变化瞬间的短暂超额已注释承认(同 1.0);`AcquireSlot` 的 waiting 计数在 ctx 取消路径正确递减。

#### 1.E.2 `internal/router/response.go`(590 行)+ `response_test.go`(786 行,37 用例)
- **内容**:响应归一化状态机(undecided/buffered/passthrough + opaque),model 字段回写、think 块剥离(closer 后恢复流式)、Thinking Process 剥离(双重守卫)、[DONE] 追加(仅 openai+SSE+未发)、soft block 纯观测、32MB 溢出降级 opaque、applied/rawPreStrip 审计轨迹。测试覆盖跨 chunk、1 字节读、ping 风暴 O(n) 保证、溢出、恢复流式、软拦截、无误伤守卫等。
- **问题**:
  - `[M]` **`think_strip` 的触发守卫弱于设计文档的描述**:doc §5.5 称误伤需"响应开头恰好命中触发形态",但代码里 `<think>` 的判定是 `bytes.Contains(ev, thinkOpenMarker)`(事件任意位置)且 `finalizeBuffered` 对整个缓冲体跑 `thinkPattern.ReplaceAll`——正文中段合法出现的 `<think>...</think>` 字面文本(如用户请模型解释 think 标签格式、模型在代码块里输出示例)会被从响应中静默删除。ThinkingProcess 路径有"首个 content 值前缀"守卫,think 路径没有对称守卫。这是真实的数据损坏向量(概率低但受用户输入直接影响)。建议:think_strip 也加"首个 content 值以 <think> 开头"前置守卫,或至少只剥第一个块。(2.0 新发现;1.0 §3.1.1 只讨论了 wording 脆弱性,未指出守卫不对称)
  - `[M]` **SSE 事件分隔符只认 `\n\n`,CRLF 框架(`\r\n\r\n`,SSE 规范合法)的上游会静默失去流式**:passthrough/undecided 都找不到事件边界,整个响应被扣到 EOF(<32MB)或 32MB 溢出才降级 opaque——功能不坏但 TTFB 退化为全响应时长,且无任何 applied 标记提示发生了什么。现接入的厂商都用 `\n`,风险休眠;若要修,`eventSep` 匹配两种即可。(2.0 新发现)
  - `[L]` `modelFieldPattern` 对响应体内**嵌套** `"model":"…"` 字段同样改写(regex 不限层级);`TestRespStream_NestedModelInDelta` 的注释声称 "rewrite still applies at the top level only" 但断言只验证了顶层被改写、**没有**锁定嵌套不被改——注释与实现不符,测试也没锁住注释声称的性质。现实中响应嵌套 model 字段极罕见,记录不符点。(2.0 新发现)
  - `[L]` `emitBlock`/`finalizeBuffered` 的 regex 替换串 `${1}+clientModel` 未转义——虚拟模型名含 `$` 时会被 regexp 展开语义误解(config 里的名字理论上可含 `$`)。用 `strings.Replace` 风格拼接或 `regexp.QuoteMeta` 对待替换串。(2.0 新发现,极低概率)
  - `[L]` `sawDone` 用 `bytes.Contains(block, "data: [DONE]")`——正文 content 里字面出现该串(如文档/代码示例)会误置 sawDone,导致该响应真正缺 [DONE] 时不补。极边缘。(2.0 新发现)
  - `[L]` `stripThinkingProcess` 强绑 MiniMax wording(同 1.0 §3.1.1,[S/M] 主发现,仍成立);`containsSoftBlockMarker` 仅 2 标记(同 1.0 §3.2.6);溢出降级三步重复代码两处(同 1.0)。
  - `[I]` `scanned` 偏移防 ping 风暴 O(n²)、`Read` 的 (0,nil) 让 watchdog tick、srcErr 先吐已产出字节再报错——设计细致,测试都锁了。

### 1.F 服务层(server / recorder)

#### 1.F.1 `internal/server/server.go`(347 行)
- **内容**:路由表(两个聊天入口 + models + admin/status)、authenticate(constant-time 比较,catch-all 不打标签、api_keys 命中打 KeyTag、无 key 配置时自报身份)、headerBlocklist(13 项)+ FilterClientHeaders(导出供 replay)、chatHandler(审计 defer → auth → 缓冲 body → probe 解析 → 并发闸 → 图片降采样 → Serve)、models(合并格式)、adminStatus(loopback-only)。
- **问题**:
  - `[L]` **`Bearer ` 前缀匹配大小写敏感**:`strings.TrimPrefix(auth, "Bearer ")`——RFC 7235 规定 auth-scheme 大小写不敏感,发 `bearer sk-…` 的客户端会被 401。主流 SDK 都发标准拼写,休眠问题;改 `EqualFold` 前缀判断即可。(2.0 新发现)
  - `[L]` 未知虚拟模型的请求也会先占一个并发槽、并在带图时跑完 imgprep 解析,最后才在 `Serve` 里 404——顺序是刻意的(probe 提前、gate 覆盖 CPU 阶段),但 404 路径的这点浪费可通过把 route 存在性检查提前到 gate 之前消除。极低优先级。(2.0 新发现)
  - `[L]` `adminStatus` 对带 zone 的 IPv6(`::1%lo0`)ParseIP 失败会 403——fail-closed 方向,安全无害(同 1.0 [Q],方向确认无问题)。
  - `[I]` recorder 的 `bytes.Buffer` 无上限完整记录响应体——审计"不截断"的文档化决策;大响应双份内存(转发 + 审计)是已知代价。
  - `[I]` 1.0 §3.1.2(models 无鉴权)确认不成立:`server.go:39` 为 `s.auth(s.models)`。
- **测试**(11 文件 ~2,981 行,57 用例):auth(单 key/多 key tag/自报 tag/x-api-key)、failover(默认试遍/max_attempts 上限/内容错不冷却/全部内容错返回最后错误)、headers(危险头剥离/元数据透传/Accept-Encoding/gzip 透明解压/Retry-After 透传/version 不重复)、audit(双层记录/流式/被拒请求/128KB 截断标记)、imgprep 端到端(全局/覆盖/强制关/缓存)、探针释放(cancel/client-error)、并发闸(容量/等待者取消)、双协议隔离、openclaw 24 轮场景。覆盖广度是全项目最好的。
- `[I]` 测试基建大量复用 fake upstream + 真实 HTTP 服务器,黑盒程度高,重构安全网扎实。

#### 1.F.2 `internal/server/recorder.go`(57 行)+ 测试
- **内容**:tee ResponseWriter,记录 status/TTFT/全量 body,Flush 透传。
- **问题**:`[I]` 单 goroutine 使用,无并发问题;`status==0` 时 `message()` 返回 nil 与 `canceled` 判定联动正确。无新发现。

### 1.G 入口(cmd/vmr)

#### 1.G.1 `cmd/vmr/main.go`(694 行)+ 测试(166+188 行)
- **内容**:7 子命令分发;cmdStart(SetRetentionDays 先于 audit.New 的顺序注释、logStart/logStop marker、fsnotify+SIGHUP 双通道 reload、SIGTERM 10s 优雅 drain);logConfigSummary(EffectiveOrder 共享排序、key:EMPTY 提示、proxy 按 provider 一行);cmdCheck/cmdStatus/cmdDirs/cmdReport(session 分析失败降级)/cmdReplay/cmdDiagnose。
- **问题**:
  - `[L]` **`reload` 无串行化**(交叉验证 1.D.2 的 [Q]):fsnotify 防抖 timer 与 SIGHUP goroutine 可并发调用 `reload`。逐项核对后果:`rt.Install` 原子交换且每个被换出的快照都会被关闭 idle 连接;`SetRetentionDays` 原子;`auditDirInUse` 只读——并发 reload 实害仅限日志交错与 `installLimiter` 的 Load/Store 非原子导致理论上多换一次信号量(短暂超额,与已接受的容量变化窗口同级)。结论:良性竞态,不必修;若要修,一个 `sync.Mutex` 包住 reload 即可。(2.0 新发现+核实)
  - `[L]` `cmdReport` 中 `report.Build` 与 `report.AnalyzeSessions` 对同一批文件各完整读一遍(zst 解压也是两遍)——GB 级日志下时间翻倍。批处理工具可接受,已知性能点(与 1.0 §1.I.1 bodyBytes 重复 marshal 同属"report 慢"的成因)。
  - `[I]` `logStop` 不覆盖 panic 路径——1.0 §3.1.6 已复核定论"低 ROI 不做",维持。
  - `[I]` `SetRetentionDays` 先于 `audit.New` 的不变量有注释锁定但无测试锁定(同 1.0 §3.2.2 tail)。
  - `[I]` `cmdStatus` 用裸 Transport 绕开一切代理设置访问本机 admin 端点,注释解释到位。
  - `[I]` main 启动顺序 `rt.Install(snap)` 先于 `ListenAndServe`,1.E.1 的 nil-snapshot 防御性备注确认不可达。
- **测试**:21 用例,覆盖 flag 解析、错误路径、report 产物存在性、diagnose 三态出口码、replay 参数互斥——CLI 层合适的黑盒粒度。

### 1.H 图片处理(imgprep)

#### 1.H.1 `internal/imgprep/imgprep.go`(450 行)+ `imgprep_test.go`(640 行,25 用例)
- **内容**:`HasImageMarker` 廉价预检 → `rewriteBody/rewriteMessage/rewriteBlock` 树遍历(未知字段字节保留)→ `processImage`(DecodeConfig 头部检测无条件跑;需缩放才整图解码 → BiLinear → 白底摊平 → JPEG q85;GIF 一律跳过;64MP 解压炸弹守卫;顶层 `recover()` fail-open)。缓存查找在"确认需处理"之后。
- **问题**:
  - `[M]` **`Downscale` 的 panic 恢复零观测**(同 1.0 §1.L.1/§4.17,**仍未修**):`recover()` 时静默丢弃 images 元数据、返回原字节,无日志/无 audit 标记。stdlib 解码器对对抗性输入 panic 时运维完全不可见。建议至少在 ImageInfo 或 stderr 留一条痕。
  - `[L]` `processImage` 具名返回 `err` 恒为 nil(所有 return 均字面 nil,`image.Decode` 的错误被吞进 fail-open),调用方的 `err != nil` 分支全部死代码(同 1.0 §1.L.1 [Q],仍在)。清理或改注释。
  - `[L]` 降采样触发时整个 body 经 `map[string]json.RawMessage` 重序列化,**顶层与该消息的 key 顺序变为字母序**——偏离字节保真比 model splice 大;好在 Go map marshal 排序是确定性的,对上游 prompt cache 是稳定输入,不构成缓存失效向量。文档(§7)只说"未知字段字节不动",没说 key 序会变——建议文档补一句。(2.0 新发现,信息性)
  - `[L]` 畸形 data URI(缺 `;base64,`)在 OpenAI 路径标 `Remote:true`(同 1.0 [I],不准确但无害);header 解码失败的图片完全不记 ImageInfo,审计对"损坏图片"无感知。(后半为 2.0 补充)
  - `[I]` GIF 单/多帧一律跳过的决策与注释(DecodeAll 无界解码的原因链)与 1.0 §1.L.1 修复记录一致,`TestSingleFrameGIFUntouched`/`TestAnimatedGIFUntouched` 锁定。
  - `[I]` `scaledSize` 的 `<1` 钳制覆盖极端长宽比;测试含混合结局多图、bomb 守卫、fail-open 三连,覆盖到位。

#### 1.H.2 `internal/imgprep/cache.go`(145 行)
- **内容**:`sha256(原图)+maxPx` 键;`cacheLookup` 命中刷 mtime;`cacheStore` CreateTemp+Rename 原子写、全程 best-effort;`sweepState`(sync.Map 按目录)+ `maybeSweepCache` 每日一次异步清扫(TTL + `.tmp-` 孤儿)。
- **问题**:
  - `[I]` 磁盘无容量上限(仅 TTL)——文档化取舍(同 1.0);`MkdirAll 0o700` 不收紧已存在目录权限(同 1.0);sweep 与 lookup 的 TTL 边界 TOCTOU 退化为 cache miss(同 1.0)。三条均维持原判。
  - `[I]` 本次重审无新发现;`maybeSweepCache` 先记日期后起 goroutine 防同日重试风暴,细节正确。

### 1.I 诊断与重放(diagnose / replay)

#### 1.I.1 `internal/diagnose/diagnose.go`(391 行)+ 测试(417 行,11 用例)
- **内容**:4 阶段(config 校验 / env:DNS+TLS+proxy+api_key / connect:真实最小请求 / route:静态预览带连通标注);`checkConcurrency=8`;走代理的 provider 跳过直连 DNS/TLS(有测试锁);`testEndpoint` 复用 `router.NewUpstreamClient` + `adapter.BuildRequest`;`runConcurrent` 泛型保序。
- **问题**:
  - `[L]` Phase 2 的 DNS/TLS/proxy 拨号超时硬编码 5s,不随 `-test-timeout` 联动(同 1.0 §1.H.1 2026-07-16 新发现,**仍未修**)。
  - `[L]` `snippet` 以 `s[:120]` 字节截断,中文错误消息(国内厂商常见)可能被切在 UTF-8 字符中间,输出含非法字节。与 report 包 `capStr` 同类问题(1.0 §1.I.6 [M] 的姊妹场景)。改 rune 截断即可。(2.0 新发现)
  - `[L]` `minimalBody` 每次 diagnose 对每端点真实计费一次(同 1.0);`testEndpoint` 每端点新建一个 http.Client,一次性工具可接受。
  - `[I]` 测试用真实 TLS server/黑洞地址/慢 provider 模拟并发,覆盖充分。

#### 1.I.2 `internal/replay/replay.go`(450 行)+ 测试(631 行,21 用例)
- **内容**:三种定位方式互斥(-detail/-ts/-line)+ `recordView`(RawMessage 保原字节)+ `replayHeaders` 双重过滤(blocklist + 凭证掩码头)+ dry-run / 真发 / `-record` 写独立 JSONL(字段布局对齐 live record)。
- **问题**:
  - `[M]` **`-stream` flag 实际不起作用**:`opts.Stream` 只写进 `creq.Stream`,而两个 Adapter 的 `BuildRequest` 都不读 `CanonicalRequest.Stream`——出站 body 里的 `"stream"` 字段仍是记录原值,replay 的行为与不传该 flag 完全相同(`writeReplayRecord` 也记 `rv.Stream` 原值)。flag 帮助文本承诺 "force stream on/off",属于挂了旗子不干活。修法:定位到 body 顶层 `stream` 字段做 splice/重建(可复用 RewriteModel 的 generic 路径),或干脆删掉这个 flag。(2.0 新发现)
  - `[I]` 1.0 §1.H.2 的 `-protocol` 覆盖误导报错一条:现版错误消息已含协议名并提示 "pass -model",实质已缓解,不再列为待办。
  - `[L]` `writeReplayRecord` 的 `defer f.Close()` 错误被吞(与 export.go 同款,1.0 §1.I.3);磁盘写满时 -record "成功"但文件不完整。
  - `[I]` `loadRecordByTS` 毫秒歧义报错而非猜测、`ForEachLine` 跳坏行——调试工具的正确取向;测试覆盖三定位器全部交叉路径。

### 1.J 报告(report)——最大模块(~5,655 行生产代码,45 测试)

#### 1.J.1 `internal/report/report.go`(805 行)+ `report_test.go`(448 行)
- **内容**:Format 9 changelog 注释;8 类桶(Rows/Overall/ByModel/ByDate/Endpoints/EndpointsAll/Hours/HoursOfDay)单趟填充,每桶独立收原始值算真百分位;`attemptErrorClass` 新旧格式回退;`bodyBytes`/`percentiles`/`round2` 等 helper。
- **问题**:
  - `[M]` **每条记录的 body 解析成本被桶数放大**:`addRecord` 被调 4 次(Rows/Overall/ByModel/ByDate)+ `addHour` 2 次 + `addEndpointRequest` ≤2 次,每次都各自跑 `bodyBytes`(整 body re-marshal)与 `messageCount`/`roleChars`(整棵消息树遍历)——同一条记录同样的结果算 6~8 遍。GB 级日志下这是 `vmr report` 慢的主因(比 1.0 §1.I.1 的估计更重)。修法:在 record 循环里先算一次 `(bytesIn/bytesOut/msgCount/roleChars)` 再传入各 add 函数,~30 行改动,收益数倍。(2.0 上修)
  - `[L]` **旧格式日志的 canceled 归类与文档不符**:设计文档 §9.2 约定 4 称四种非 HTTP 路径"本来就是 class: 详情 前缀",但 `canceled` 的 Error 实为 `"canceled by client"`(无冒号)——`attemptErrorClass` 回退时返回整句 `"canceled by client"` 而非 `"canceled"`,旧日志在 `error_classes` 分布里出现长尾键名。新日志有 ErrorClass 字段不受影响。文档或回退逻辑二选一修正。(2.0 新发现)
  - `[L]` 流中断(truncated)的请求因最后 attempt 带 Error 而没有 successEp,其 tokens/bytes/TTFT 不归入任何 endpoint 行——端点表对"曾经服务过但断流"的流量视而不见。语义可辩护(客户端没拿全),记录即可。(2.0 新发现)
  - `[I]` `percentiles` nearest-rank 边界(n=1)核对正确;HoursOfDay/EndpointsAll 独立收原始值的设计与注释一致。

#### 1.J.2 `internal/report/usage.go`(178 行)
- **内容**:四形态 usage 提取;Anthropic In=三字段相加、OpenAI prompt_tokens 已含缓存,以 `input_tokens` 键存在性切规则;`mergeUsage` 按字段取 max 兼容累计流。
- **问题**:`[I]` 本次逐行核对无新发现;DeepSeek `prompt_cache_miss_tokens` 未处理(1.0 [L],理论性,维持)。

#### 1.J.3 `internal/report/export.go`(460 行)
- **内容**:WorkloadRow/SessionRow/ToolShapeRow 聚合 + `vmr-requests.jsonl` 写出 + 按 ClientKeyTag sibling 导出。
- **问题**:
  - `[M]` **tag 经 `sanitizeName` 后可碰撞,sibling 文件互相覆盖且无检测**(同 1.0 §1.I.6 新发现,**仍未修**;detail.go 的 tag index 同样受影响)。
  - `[L]` `writeRequestRows` 的 `defer f.Close()` 错误被吞(同 1.0 §1.I.3,仍未修)。
  - `[I]` SessionRows 的 `MessagesKnown++` 无条件累加——核实过:能进 session 的记录必然解析过 chat body(非 chat 体落 Ungrouped),语义无误。

#### 1.J.4 `internal/report/markdown.go`(592 行)
- **内容**:7 张表渲染,全部读预聚合桶;`mergeIntoCollapsed` 折叠行收集原始值重算 p50/p95;各类型一套 avg/cell helper。
- **问题**:
  - `[L]` `ms(v)` 用 `v > 1000`,恰好 1000ms 显示 "1000ms" 而 1001ms 显示 "1.0s"——纯观感,顺手改 `>=` 即可。(2.0 新发现,鸡毛蒜皮)
  - `[I]` helper 按类型重复(avgTokensInOut ×5)——1.0 已记"可泛型化但目前清楚",维持。

#### 1.J.5 `internal/report/render.go`(594 行)
- **内容**:codeFence 动态围栏、details 折叠、preview(rune 安全)、chatMessages/roleChars/messageCount、reassembleSSE(双协议增量重组)、finalMessage。
- **问题**:
  - `[I]` 本次逐行核对无新发现;`reassembleSSE` 与 router/response.go 的功能重叠仍是两套独立实现(1.0 §3.2.7,维持"暂不合并"判断——两侧关注点不同:router 是字节级转发,report 是语义重组)。

#### 1.J.6 `internal/report/session.go`(782 行)+ `session_test.go`(793 行)
- **内容**:LCP 分组(parentWindow=16)+ 多信号融合(Traceparent/metadata.user_id/anchor hash/chat_id)+ OpenClaw envelope 剥离 + NoReply 重试合并 + compaction 三特征识别与双向链接 + 每记录特征提取。
- **问题**(1.0 的 4 条 [M] 全部复核仍成立、均未修):
  - `[M]` `capStr` 字节截断非 rune 安全(§1.I.6)——`needle`/`firstText`/`respText`/TraceID 展示多处使用,中文内容截断可产非法 UTF-8。`render.go` 的 `preview` 已有现成 rune 安全实现。
  - `[M]` `needle` 200 字节上限的双面问题:误链(假阳性)+ 漏链无日志(假阴性)。
  - `[L]` `parentWindow=16` 无实测依据可查(1.0 复核结论维持);anchor hash 撞车(两个会话首条消息完全相同)会合并会话——设计固有假设,文档已述。
  - `[I]` `deltaHasNewInstruction` 的 parentKeys 内容级去重(防 prune 误开任务)、NoReply 不开新任务——注释引用了真实日志观察,启发式的可信度记录到位。

#### 1.J.7 `internal/report/detail.go`(1249 行)+ `detail_test.go`(552 行)
- **内容**:每记录 .md+.json 详单、索引(Chat User → Session → Task 分组 + 全量时间序表)、tag sibling 索引、emoji diff(headers/body/messages/tools 全列出仅标变化)、RawPreStrip 展示、norm 步骤中文描述表。
- **问题**:
  - `[M]` **详单/索引文件权限 0o644、目录 0o755,而源头审计日志刻意用 0600/0700**:details/*.md|json 含与审计完全相同的完整对话内容,多用户机器上全局可读——与审计侧"明文密钥/对话防外泄"的既有决策自相矛盾(凭证虽已打码,对话正文本身就是敏感物)。建议详单输出统一 0600/0700。(2.0 新发现)
  - `[L]` `normDescriptions` 缺 `opaque`/`overflow_raw_passthrough` 两个真实会出现的 norm 值,渲染成"(未知步骤)"——表与 response.go 的 applied 集合不同步。补两行即可。(2.0 新发现)
  - `[L]` `sanitizeName` 连续 `-` 边界(1.0 复核后已收窄,维持 [L]);`fileLinksCell` 裸 HTML 链接(1.0,维持)。
  - `[I]` `attemptUpstream` 的 SplitN(3) 兼容 OpenRouter 带 `/` 模型名——注释与实现一致。

### 1.K 负载测试(loadtest/)——2.0 新覆盖(1.0 时尚不存在)

#### 1.K.1 `loadtest/runner/main.go`(278 行)
- **内容**:一键编排——build mockupstream → 起 mock + vmr → gentargets → 三档 Vegeta(10/50/150 rps)→ 优雅停 vmr → `vmr report` → 从 vmr-report.md 原样抽取"按模型/端点可用度"两节拼进 loadtest/report.md。注释解释了为何用编译产物而非 `go run`(Kill 只杀 wrapper 留孤儿)。
- **问题**:
  - `[L]` 三处硬编码常量必须跨文件同步:runner 的 `vmrAddr/mockAddr` ↔ config.yaml 的 `listen`/base_url ↔ gentargets 的 `vmrAddr`。任一改动需记得改其余两处;gentargets 注释有提示,runner 没有。(2.0 新发现,一次性工具可接受)
  - `[L]` 出错中断的轮次走 `defer Process.Kill()`(SIGKILL)而非 Interrupt——vmr 不会写 VMR STOP marker,loadtest/logs 里留下"有 START 无 STOP"的假崩溃痕迹。仅影响排查体验。(2.0 新发现)
  - `[I]` `extractTables` 找不到标题时明确报 "headings may have changed"——与 vmr-report.md 的渲染耦合有 fail-fast 保护,好。

#### 1.K.2 `loadtest/mockupstream/main.go`(232 行)
- **内容**:按 model 前缀分发 5 种响应形态(baseline/stream_normal/thinking_leak/think_tag/big_response)+ 两个恒 500 路径 + anthropic /messages;`writeSSE` 关 HTML 转义(注释点明与 core.MarshalNoEscape 同因);thinking_leak 的双 endorsement 构造与 response_test 的"取最后一个"规则呼应。
- **问题**:`[I]` 无新发现;fixture 与被测逻辑的对应关系注释充分,是高质量的测试基建。

#### 1.K.3 `loadtest/gentargets/main.go`(195 行)+ `loadtest/config.yaml`(63 行)+ `loadtest/README.md`(67 行)+ `.gitignore`/`logs/.gitkeep`
- **内容**:11 场景 targets.json 生成(合成 JPEG/动图 GIF,故意不入库);config 每场景一个虚拟模型(scenario 名即报表分组键)、failover 场景三端点两坏一好;README 是完整 runbook(含"数字怎么读"——thinking_leak 无 TTFB 收益是已知成本、failover 稳态数字的解释)。
- **问题**:
  - `[L]` gentargets 的 `out.Write`/`defer out.Close()` 错误全部忽略——磁盘满时 targets.json 静默截断,Vegeta 会拿部分场景跑出误导性结果。加错误检查 ~5 行。(2.0 新发现)
  - `[I]` README 与 design doc/config/代码四者交叉引用一致,实测口径(11 场景、三档)与 runner 常量一致。整个 loadtest 模块质量良好,无严重问题。

### 1.L 文档与配置面(docs / README / config.example.yaml / vmr.sh)

#### 1.L.1 `docs/VirtualModelRouter_System_Design_v2.md`(652 行,全文精读)
- **内容**:定位/协议/架构/Adapter/归一化/健康/图片/审计/配置/决策表(§11 五十余条)/路线图/暂缓清理项/diagnose+replay。与代码的同步度总体极高(errBodyCap、GIF 决策、writeError 统一、KeyTag 语义等当日改动均已反映)。
- **问题**:
  - `[L]` **两处过时的"末 6 位"**:§10 配置示例注释(L466)与校验规则段(L509)仍写 "KeyTag 的末 6 位窗口"——`keyTagLen` 已于 2026-07-16 从 6 改为 8(audit.go、config.example.yaml、UserGuide 均已是 8),设计文档漏改。(2.0 新发现)
  - `[L]` §4.3(L126)引用 `docs/ClientAPIKeyGrouping_Design_Sonnet5.md`(§六)**未标注该文档已删除**——同一文件内 L443/L446/L450 对三份已删文档都补了"已删除"说明,唯独这处漏了,读者会去找一个不存在的文件。(2.0 新发现)
  - `[M]` §5.5"已知边界"声称 think-strip 误伤需"响应开头恰好命中触发形态"——与实现不符(`<think>` 是 contains-anywhere 触发,见 1.E.2),文档给读者的安全感强于代码实际提供的。与 1.E.2 的守卫不对称是同一问题的文档面。
- **交叉发现:已删文档的引用清单(截至本次审计)**:`internal/server/server.go:57`、`internal/report/export.go:386`、`internal/report/session.go:5`/`:59`、`internal/report/detail.go:171`、`internal/audit/audit_test.go:109`、`vmr.sh:78` 共 7 处代码/脚本注释仍指向三份已删除的分析文档,均无"已删"标注(同 1.0 §2 观察 9,**仍未清理**)。

#### 1.L.2 `README.md`/`README.zh.md`(98/97 行)+ `docs/Why_vmr_over_LiteLLM{,.zh}.md`(51×2 行)+ `docs/UserGuide{,.zh}.md`(198×2 行)
- **内容**(全部为 1.0 之后新增/重写):README 双语对照逐段一致,卖点清单与实现核对无夸大(150 req/s、9/11 场景 p95<6ms 与 loadtest 实测一致;"两个 MiniMax 修复是全部字节级偏离"与 response.go 一致)。Why_vmr 用变压器/转换插头类比,"什么时候该用 LiteLLM"一节坦诚。UserGuide 覆盖配置/透传/健康/审计/图片/CLI,与代码逐项核对基本一致(KeyTag=8 正确、`/v1/responses` 等不在范围的声明明确)。
- **问题**:
  - `[M→关联]` UserGuide L171 宣传 `vmr replay` 的 `-stream true|false` 可以 "force streaming on/off"——**该 flag 实际不生效**(见 1.I.2),文档跟着代码一起虚假承诺。修 flag 或删文案。
  - `[L]` README 的 `admin/status` 示例注明 "no api_key required even if one is set"——与实现一致(该端点只查 loopback 不走 auth),但这意味着同机其他用户可读健康拓扑;单机单用户场景可接受,记录。(2.0 新发现,信息性)
  - `[I]` 中英双语的数字/命令/示例逐项抽查无漂移。

#### 1.L.3 `docs/PerformanceTesting_Design_Sonnet5.md`(126 行)+ `docs/SensitiveWordFilter_Analysis_Fable5.md`(216 行)+ `docs/vmr_future_strategy_deepseek-v4-flash.md`(721 行)
- **内容**:性能测试方案 v2——§0 "要不要做"的诚实论证 + §6 落地实录(4100 请求实测数字、三条教训:SSE chunk 边界影响正则、json.Marshal HTML 转义破坏字面量、go run 孤儿进程);敏感词分析(1.0 已审,未变,结论"第一阶段纯观测"已落地为 soft_block_detected);战略文档(1.0 当日已人工复核修订,未再变化)。
- **问题**:`[I]` 三份文档状态与 1.0 记录一致,无新发现。PerformanceTesting §6 的教训记录是"设计即文档"实践的又一正例。

#### 1.L.4 `config.example.yaml`(132 行)
- **内容**:全字段注释示例;KeyTag 派生规则 5 例(窗口=8,正确);api_keys 16 字符下限的泄漏原因说明;跨协议 provider 重名示例;无鉴权自报身份说明。
- **问题**:`[I]` 与 config.go/audit.go 逐项核对一致,无发现。是 ClientAPIKeyGrouping 设计文档删除后该知识现存最完整的用户侧载体(与设计文档 §9.4 并列)。

#### 1.L.5 `vmr.sh`(416 行)
- **内容**:dev/service 双模式;lazy resolve_log_dir(注释解释 set -e 陷阱);port_holder lsof 容错;write_env_file(仅抓 config 引用的 ${VAR},0600,先 chmod 后写值——顺序正确);launchd TCC 规避(WorkingDirectory=$HOME);bootout 防 KeepAlive 复活。
- **问题**:
  - `[L]` L78 注释引用已删除的 `docs/AuditLogCompression_Analysis_Sonnet5.md`(死链清单见 1.L.1)。
  - `[L]` `render_unit` 的 `ExecStart=$BIN start -c $CFG` 未加引号,仓库路径含空格时 systemd 单元损坏;`write_env_file` 的 `VAR=value` 行对含空格/引号的值在 launchd `. env` 下会解析失败——API key 现实中不含空格,理论性。(2.0 新发现)
  - `[L]` `ensure_bin` 的 `find . -name '*.go' -newer` 会因 loadtest/ 下的改动触发主二进制重建——无害,顺带记录。(2.0 新发现,鸡毛蒜皮)
  - `[I]` 无测试(同 1.0,bash 脚本测试成本高,维持)。

#### 1.L.6 仓库杂项:未跟踪文件
- `[I]` 仓库根目录存在未跟踪的 `audit_report_v2_logs_pi_agent.md`(91KB,2026-07-16 17:33,"by Pi Agent (minimax-m3)")——另一个 agent 并行产出的 audit 2.0 过程稿,含与本报告部分重叠的发现(如已删文档引用清单)。它不在 git 内也不在 .gitignore 内,`git status` 会一直显示为 untracked。建议 owner 决定去留(入库、移入 _tmp、或删除);本报告为独立重审,未参考其内容形成结论。

---

## 2. 验证记录

- `go vet ./...`:零告警。
- `go test ./...`:16 个包全部通过(loadtest 三个 main 包无测试,符合定位)。
- `go test -race` 于并发密集的 router/server/health/audit/imgprep 五包:全部通过。
- 本报告为逐文件精读的过程记录;跨模块观察、三梯队问题分组与建议方案见姊妹文件 **`AUDIT_SUMMARY_2_Fable5.md`**。
