# 5 分钟装好 VMR，看懂你的 Agent 在干什么

> **版本**：v3（纯实操教程）
> **对标 v1**：`article-01-cn-blackbox.md`
> **差异**：不讲故事、不讲竞品、不讲原理。从头到尾就是命令 + 输出 + 怎么读。适合"别废话，让我装上看看"的读者。
> **目标平台**：linux.do（教程区）、V2EX
> **字数**：约 1500 字

---

这是一篇纯实操教程。不讲"为什么"，只讲"怎么做"。5 分钟从零到看懂第一份 Agent 审计报告。

---

## 第一步：安装（30 秒）

```bash
# macOS
brew install bigfatsea/tap/vmr

# Linux / 手动下载
# 从 https://github.com/bigfatsea/vmr/releases/latest 下载对应平台的二进制
# chmod +x vmr && mv vmr /usr/local/bin/

# 验证
vmr version
# 输出：vmr v0.2 (commit: d8d933a, built: 2026-07-28, go1.24)
```

---

## 第二步：配置（1 分钟）

```bash
# 下载配置样例
curl -o config.yaml https://raw.githubusercontent.com/bigfatsea/vmr/main/config.example.yaml
```

编辑 `config.yaml`，最少填三样东西：

```yaml
providers:
  openai:
    minimax:
      api_key: ${MINIMAX_API_KEY}    # 改成你的 key 或环境变量
    deepseek:
      api_key: ${DEEPSEEK_API_KEY}
    openrouter:
      api_key: ${OPENROUTER_API_KEY}

models:
  openai:
    coding:
      endpoints:
        - {provider: openrouter, model: z-ai/glm-5.2}
        - {provider: deepseek, model: deepseek-v4-pro}
        - {provider: minimax, model: MiniMax-M3}
```

校验：

```bash
vmr check -c config.yaml
# 输出路由表，确认每个端点后面都有绿色的 ✓
```

---

## 第三步：跑起来（10 秒）

```bash
vmr start -c config.yaml
```

输出类似：

```
vmr 0.2  starting on 127.0.0.1:8800
models:
  openai/coding
    → openrouter / z-ai/glm-5.2       (priority: 0, cap: all)
    → deepseek   / deepseek-v4-pro    (priority: 0, cap: all)
    → minimax    / MiniMax-M3         (priority: 0, cap: all)
probe: active, timeout=15s
```

---

## 第四步：把 Agent 指过来（30 秒）

```bash
# Claude Code
export ANTHROPIC_BASE_URL=http://127.0.0.1:8800

# 或 OpenClaw
export OPENAI_BASE_URL=http://127.0.0.1:8800/v1
```

然后该怎么用怎么用。VMR 在后台静默记录一切。

---

## 第五步：跑一天后，看报告（1 分钟）

```bash
vmr report
```

打开 `reports/vmr-report.md`。你会看到以下几段。

### 总览

```
总请求: 2,847    成功: 2,786 (97.9%)    失败: 61
平均延迟: 2.1s    p95: 5.3s    p99: 8.7s
总成本估算: ¥14.23
```

### 会话 → 任务 → 轮次

```
Session 2026-07-28
  ├── chat_id=abc123 (1h22m, 💬22, 🛠️14, ⚡15.2s avg)
  │   ├── Turn 1     → 🆕 初始任务描述 → 模型输出 + 2 次工具调用
  │   ├── Turn 2     → 🆕 工具结果 + 模型继续 → 1 次工具调用
  │   └── ...
```

🆕 = 这一轮新增的内容。不用从头翻对话。

### 按端点分析

```
端点                      请求   成功率   平均延迟   成本/1M tok   产出/¥
minimax/MiniMax-M3       1,012   96.8%    1.8s       ¥1.20        12,107
deepseek/deepseek-v4-pro   588   98.1%    3.1s       ¥3.60         7,233
openrouter/GLM-5.2       1,247   99.2%    2.3s       ¥8.40         3,842
```

**看一眼就知道**：MiniMax 性价比最高，GLM 最稳但最贵。

### 工具使用对比

```
工具                    声明    实际调用   调用次数
web_search               ✓        ✓          342
read_file                ✓        ✓        1,847
notion_create_page       ✓        ✗            0
slack_send_message       ✓        ✗            0
jira_create_issue        ✓        ✗            0
```

有 3 个工具从来没被用过——你可以把它们的 schema 从配置里删掉，省 token。

### 按时段可用度

```
时段          总请求   失败   可用度
00:00-02:00    487      3    99.4%
02:00-04:00    412     11    97.3%    ← 凌晨失败率高 3 倍
04:00-06:00    398      8    98.0%
```

凌晨时段的失败率是白天的 3 倍——而且 VMR 自动切换了，你的任务没中断。

---

## 第六步：详细看某条请求（可选）

```bash
# 打开 vmr-requests.md，找到你想细看的那条请求的路径和行号，比如
vmr replay -line "audit/2026-07-28.jsonl:1234" --dry-run
```

这会打印出那一条请求 VMR 发给上游的完整内容。用于排查"那次失败上游到底收到了什么"。

---

## 完了

就这些。一个二进制文件，一份 YAML 配置，一个 `vmr report` 命令。没有数据库，没有 Web UI，没有后台服务依赖。

之后每次想看报告就跑一次 `vmr report`。日志会自动压缩（`.zst`）和过期清理（配置 `audit_retention_days`）。

**项目地址**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)

MIT 开源。个人工具。

---

## 配图建议

每步配一张终端截图，不要美化——就是真实的终端输出。

1. `vmr check` 输出
2. `vmr start` 启动摘要
3. `vmr report` 的会话分组段落
4. `vmr report` 的端点性价比表
5. `vmr report` 的工具使用对比表