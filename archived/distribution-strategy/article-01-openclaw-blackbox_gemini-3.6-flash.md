<!-- Ver 2026-07-31 21:40, by Gemini 3.6 Flash -->

# 我的 Agent 跑了 50 轮任务，我用 VMR 看清了每一步

> **TL;DR**：无人值守的 Coding Agent（如 OpenClaw、Pi Agent 或自定义 Agent）跑了一整夜，控制台打印了数千行日志，最后任务却死锁或报错。传统的 HTTP 日志只能告诉我“调了 50 次 API”，但无法告诉你“中间第 23 轮它为什么开始死循环读同一个文件”。这篇文章分享我如何用 VMR（Virtual Model Router）这个黑匣子工具，零代码侵入地还原 Agent 任务的完整执行叙事。

---

## 0. 现象：无人值守 Agent 的“黑盒噩梦”

昨晚我给 OpenClaw 安排了一个看似简单的重构任务：“将项目里的 12 个模块从原有的回调函数重构为 async/await 模式，并补充单元测试”。

把任务提交后，我就去睡了。第二天早晨醒来，终端界面被巨量的输出刷屏。结果却很尴尬：
- 任务停在了第 47 轮，抛出异常失败；
- API 账单消耗了近 40 万 Token；
- 代码库只改了一半，还有几个文件的测试用例被重构成了一团乱麻。

我试图去翻控制台日志，想找出到底是哪一步开始跑偏的。但面对成千上万行交织在一起的请求日志、工具输出和模型回复，我陷入了绝望：
> **你以为你知道你的 Agent 在干什么，其实你根本不知道。**

传统的日志系统（或者普通的 LLM Gateway）给你的只是平铺的请求记录：
```
[02:14:05] POST /v1/chat/completions - 200 OK - 1245ms - 4120 tokens
[02:14:12] POST /v1/chat/completions - 200 OK - 1890ms - 5230 tokens
[02:14:21] POST /v1/chat/completions - 200 OK - 2100ms - 6400 tokens
... (平铺了 50 条)
```
这些数字告诉你“钱花掉了、请求成功了”，但它像一个黑盒，无法回答三个最致命的问题：
1. **哪一轮开始，Agent 陷入了死循环？**
2. **模型声明的工具（Tools），它究竟真的调用了多少，还是全在白吃 Token？**
3. **中间上游供应商有没有悄悄返回空回复（软屏蔽），导致 Agent 拿着空数据瞎猜？**

---

## 1. 视角转变：Agent 审计不是“记日志”，而是“git 版本的演进”

为了解决这个问题，我写了一个轻量级的工具——**VMR（Virtual Model Router）**。

在设计 VMR 时，我确定了一个核心原则：**VMR 不是一个单纯的 LLM 路由器，它是一个 Agent 运行可见性黑匣子。路由只是获取数据的手段，看见才是目的。**

为什么现有的网关做不好 Agent 可观测性？因为他们把 API 调用当成孤立的 HTTP 请求。但在 Agent 场景下，**Agent 每一轮请求都是在重发前面所有累积的历史上下文**。

换句话说：**Agent 的对话历史不是日志，而是同一份状态在时间轴上的完整快照序列。**

这不就是 git 的工作模式吗？
- 消息是不可变的 `blob`；
- 每次请求的上下文是 `manifest`（哈希向量）；
- 相邻请求之间的变化是 `Append`/`ReplaceTail`/`Splice`/`Contract`（编辑分类）；
- 整个任务退化成一条 `lineage`（演进链）。

有了这个理论基础，VMR 根本不需要你在 Agent 代码里埋点（不需要集成复杂的 SDK），只需要把 Agent 的 `OPENAI_BASE_URL` 或 `ANTHROPIC_BASE_URL` 指向 VMR 的端口（如 `http://127.0.0.1:18080/v1`），VMR 就能在本地自动完成字节级两层审计，并还原 Agent 任务的完整叙事。

---

## 2. 第一步：用 `vmr report` 一眼看清会话全貌

跑完任务后，我只需要在终端敲入一行命令：

```bash
vmr report ~/.vmr/logs/
```

VMR 离线扫描本地的审计 JSONL 文件，几秒钟内就能在终端和当前目录生成一份 `vmr-report.md` 报表。

打开报表，首先看到的是 **§6 会话与任务结构**：

```markdown
### 6. 会话与任务分组概览

Session: sess_openclaw_refactor_01
├── 任务 1: "重构回调函数为 async/await" (共 47 轮, 累计消耗 386,400 Token)
│   ├── 轮次 1-12 [正常推进] 🆕 新增工具调用: fs_read, fs_write
│   ├── 轮次 13 [历史压缩] Compaction 触发 (丢弃了 2 个实体路径)
│   ├── 轮次 14-32 [死循环警告 ⚠️] 连续 18 轮对 `pkg/utils.go` 执行相同读取
│   └── 轮次 33-47 [错误级联] 供应商 A 报 429 ➔ 自动切换至 供应商 B
```

看到这个视图的瞬间，真相大白了！
我根本不需要去读 47 轮对话的每一行字：
- 任务在前 12 轮非常顺畅；
- 关键的分水岭在**第 13 轮**——系统触发了上下文压缩（Compaction），这一压缩丢弃了 `pkg/utils.go` 的关键接口定义；
- 从**第 14 轮开始**，Agent 因为失忆，陷入了对 `pkg/utils.go` 的反复读取与尝试，白白浪费了 18 轮 Token！

---

## 3. 第二步：用 `vmr story` 纵向还原任务现场

看到了全局问题后，我想知道第 13 轮和第 14 轮到底发生了什么细节。我运行了 VMR 的叙事重建子命令：

```bash
vmr story -journey sess_openclaw_refactor_01
```

`vmr story` 像播放电影一样，将这个 Agent 任务逐 Step 展现在终端里：

```markdown
--------------------------------------------------------------------------------
Step 13/47 | [Compaction 触发] 消息数: 42 ➔ 12 | Context Token: 84,200 ➔ 12,100
--------------------------------------------------------------------------------
[System Prompt 衍生摘要]: "用户要求重构代码，之前已修改了 3 个文件..."
[实体丢失追踪]: ⚠️ `pkg/utils.go` 接口签名 `AsyncCallbackFn` 在摘要中未被提及！

--------------------------------------------------------------------------------
Step 14/47 | 客户端 ➔ 上游 (DeepSeek-V3) | 耗时: 1420ms
--------------------------------------------------------------------------------
[Agent 思考与动作]:
  > 尝试解析 `pkg/service.go` 时找不到 `AsyncCallbackFn` 声明。
  > 决定调用工具 `fs_read(path: "pkg/utils.go")`

[工具配对校验 (F9 不变量)]:
  └─ ToolCall: call_9872a -> fs_read("pkg/utils.go")
  └─ ToolResult: 200 OK (返回 150 行代码)
```

通过 `vmr story` 的纵向还原，我不仅看到了模型在说什么，更看到了 **Tool Call ↔ Tool Result 的精确配对** 以及 **Compaction 信息损失摘要**。

在普通的日志里，你需要自己去匹配 `call_id` 和返回结果；而在 VMR 里，由于在网关层就做好了因果配对断言，工具调用的入参和返回值被天然绑定在了一起。

---

## 4. 第三步：两层字节审计与物理级 Replay 复现

在分析过程中，我还发现第 33 轮发生了一次供应商切换（Failover）。

许多网关会隐藏 Failover 的细节，或者重新封装响应。但 VMR 坚持 **字节级保真（Byte-faithful）** 原则——它会在本地记录两层完整字节：
1. **客户端 ↔ VMR 的原始收发字节**；
2. **VMR ↔ 上游供应商（每个 Attempt）的实际出入字节**。

在生成的 `vmr-requests.md` 详单里，我可以清晰地看到第 33 轮的底牌：

```
Attempt 1 (deepseek/chat): 429 Too Many Requests (耗时 120ms) -> 记录冷却
Attempt 2 (minimax/m2):    200 OK (耗时 1850ms) -> 成功转发并归一化
HTTP Header 回传: X-VMR-Failover: deepseek/chat:429
```

更酷的是，如果我怀疑上游返回的响应有 Bug，我不需要重新跑整个 50 轮 Agent，直接使用 `vmr replay` 命令：

```bash
vmr replay -line 1452 --dry-run
```

VMR 会使用与生产环境完全相同的请求构建代码，从审计记录中复原第 33 轮的请求，原封不动地重发给上游！这使得任何偶发性的供应商故障、响应格式异常都能被**物理级重现和取证**。

---

## 5. 总结：透明路由是手段，看见才是目的

经过这一套黑匣子分析，我只花了 3 分钟就定位了问题：
1. 优化了 OpenClaw 的 Compaction 提示词，要求其在压缩历史时强制保留导出的 `interface` 签名；
2. 在 VMR 配置里开启了 `sticky_model`，保证同一会话的上下文能持续命中供应商的 Prompt Cache，节省了 40% 的 Token 费用；
3. 将 Agent 从盲目重复运行的窘境中解脱出来。

VMR 的单二进制文件只有 12MB，无需部署 PostgreSQL，无需安装 Redis，跑在后台就像一个安静的飞行记录仪。

如果你也在用 OpenClaw、Pi Agent、AutoGPT 或自己编写的 Agent 框架，不妨给你的系统也挂上这个黑匣子：

- **项目开源地址**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
- **一键安装（macOS）**：`brew install bigfatsea/tap/vmr`

让你的 Agent 运行不再是碰运气的“黑盒试错”。
