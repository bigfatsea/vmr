
## 六、多 Agent 协同标准化实战指南 (Reusable Skill Guide)

> 本章节提炼本次实战的全部第一手经验，形成可复用的标准化多 Agent 研发协作模式。

### 6.1 多 Agent 并行的第一性原理
1. **物理工作区隔离是前提**：绝对不能在同一个工作目录下并发运行多个具有写权限的 Agent。`git worktree` 是本地多 Agent 的轻量级黄金标准。
2. **正交任务才能真并行**：只有**修改文件集合无交集**的任务包才能同时并发。有顺序依赖或共享修改文件的任务必须串行编排。
3. **白名单防御范围蔓延**：AI Agent 天生具有“顺手修改其他文件”的倾向。必须通过任务说明书中的“文件修改白名单”强行锁死变更半径。

---

### 6.2 任务说明书 (Task Spec) 标准模板与核心要素

一份能够让 Worker Agent 自治、高质交付的任务说明书必须包含以下 **5 大核心结构**：

```markdown
# 任务说明书：[分组名称/模块名称]

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：[精确到文件路径，如 internal/foo/foo.go]
   - ❌ 严禁修改：白名单以外的任何文件！
3. 代码风格与架构门禁：[声明依赖限制、行数预算限制等]
4. Git 规范：[指定 Commit Message 格式，严禁随意添加 trailer]
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占，Worker 严禁修改；待登记项写入不提交的 NOTES_FOR_LEAD.md
6. 语义变更预警：若改动可能引发语义变更，说明书须预告会打红的白名单外测试，并声明期望的处理方式（交回主控 / 允许修改 fixture 数据但不得削弱断言、逐条记录于 NOTES_FOR_LEAD.md）
7. 忽略目录：`_tmp/`、`archived/` 以及临时编译产物目录视为不存在，严禁读取或修改

## 二、具体修复任务清单 (Action Plan)
### 任务 1: [问题标题]
- 背景与根因：[说明为什么错，定位到 file:line]
- 目标修改：[具体改动要点，1, 2, 3]
- 验收单测：[说明需要补充什么测试用例]

## 三、测试与验收步骤
1. 局部单元测试：`go test -v -race ./internal/...`
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 检查变更范围：`git status -s`（确认无越界文件）
4. 执行 Commit：`git add ... && git commit -m "..."`
```

---

### 6.3 工作树 (Git Worktree) 生命周期编排规范

主控 Agent 编排流水线标准脚本结构：

```bash
# 1. 准备阶段
git worktree add -b feat/<task-name> ../<worktree-dir> main
cp <task-spec-source.md> ../<worktree-dir>/TASK_SPEC.md   # 源为 docs/tasks/ 下的真实 spec 文件，如 TASK_SPEC_R2_G3.md

# 2. 派发执行阶段 (后台非交互式)
(cd ../<worktree-dir> && pi -p --approve @TASK_SPEC.md "请严格按照 TASK_SPEC.md 要求执行并 commit。" > /tmp/agent.log 2>&1) &
PID=$!

# 3. 监控阶段 (检查 session 日志与 git 状态)
tail -f ~/.pi/agent/sessions/--<worktree-dir>--/*.jsonl

# 4. 回收与合并阶段
git merge feat/<task-name>
go test -race ./...

# 5. 清理阶段
git worktree remove --force ../<worktree-dir>
git branch -d feat/<task-name>
```

> **派发命令注意事项**：
> - `@file` 必须是独立 argv 项，写进引号里会被整串当成文件名导致派发空转。正确形式：`pi -p --approve @A.md @B.md "<指令>"`。
> - 未跟踪的参考文档（如各类审阅/评审产物，主仓库中可能未被 git 跟踪）不会通过 `git worktree add` 进入工作树。主控必须显式 `cp` 进 worktree 目录，并在说明书里点明"这些路径在你的 worktree 中不存在，不要去找"。
> - Pi Agent 的 CLI 命令可直接运行，其模型 Provider 配置已预先配好（本环境指向远端 VMR 服务），Worker 无需自行配置。

---

### 6.4 主控 Agent (Lead Orchestrator) 的质量收敛守则
1. **不要盲信 Git Merge 成功**：Git 文本合并成功并不代表编译能通过或逻辑自洽。主控必须在合并后在根目录下统一跑全局测试套件（`-race` + `archtest`）。
2. **审查 Commit 洁净度**：检查 Worker Agent 是否不小心提交了临时测试脚本、配置文件或未追踪的产物。
3. **保持线性历史 (Clean History)**：对于无冲突的正交分支，优先使用 Fast-Forward 或结构明确的 Merge Commit。
4. **共享文件由主控独占**：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档进所有 Worker 的禁改清单；Worker 把待登记项写进不提交的 NOTES_FOR_LEAD.md。多批并行能否零冲突，这一条是主因。
5. **验收必须独立复跑**：不能只看 Worker 自述。Worker 的最终验收命令可能未跑完就退出，主控必须独立复跑确认。"Worker 说全绿"和"主控看到全绿"是两件事。
6. **最终汇总前事实核查**：主控在回收所有子 Agent 的反馈和输出、汇总成最终结果之前，对其中关键事项进行事实核查，避免盲信文档中或子 Agent 反馈中所声称的结论。

---

### 6.5 典型陷阱与避坑清单

| 陷阱场景 | 危害表现 | 防御与解决策略 |
|---|---|---|
| **未锁白名单导致的范围漂移** | Agent A 顺手改了 Agent B 负责的文件，合并时发生冲突 | 在说明书中用 ❌ 明确声明严禁修改的文件列表 |
| **测试中注册同名组件** | 测试并发运行时因重复 Register 触发 panic | 动态生成带随机时间戳的测试组件名称（如 `test-probe-${timestamp}`） |
| **Timer 阻塞与通道死锁** | 流式转发在极端超时时阻塞 | 重置 Timer 时使用非阻塞 select：`if !t.Stop() { select { case <-t.C: default: } }` |
| **Worktree 删除报错** | Worktree 包含未跟踪的 `TASK_SPEC.md` 导致 `git worktree remove` 失败 | 使用 `--force` 强制删除已完成的工作树目录 |
| **未跟踪参考文档没进 Worktree** | Worker 找不到审阅/评审文档，无从下手或自行猜测 | 主控显式 `cp` 进 Worktree，并在说明书里声明"这些路径在你的 worktree 中不存在，不要去找" |
| **共享文件被 Worker 改动** | 登记项与文档状态失控、跨批冲突 | 共享文件由主控独占，Worker 的待登记项写进不提交的 NOTES_FOR_LEAD.md |
| **语义变更打红白名单外测试** | Worker 措手不及，擅自改测试或静默忽略 | 说明书预先预告会打红的测试，并声明期望处理方式（交回主控 / 改 fixture 不削弱断言并逐条记录） |
| **验收只信 Worker 自述** | Worker 的验收命令提前退出，实际未全绿 | 主控独立复跑最终验收命令，"worker 说全绿"≠"主控看到全绿" |

---

### 6.6 Worker 执行纪律与独立判断

1. **第一性原理与独立判断（敢于挑战历史定论）**：
   - 切勿盲从项目设计文档、代码注释中记录的历史定论、历史决策或既有取舍（如文档中的 decisions、decided-not-to-fix 等声明）——那些均属于特定历史阶段的背景产物。
   - 评估代码与既有结论时，必须**回归第一性原理与高阶设计原则**，立足于代码现状进行客观、独立的批判性审视。只要有理有据，应**敢于挑战旧有假设与既定结论**，指出因系统演进而暴露出的新瓶颈、假设失效或潜在坏味道。
2. **忽略目录**：严禁读取或修改 `_tmp/`、`archived/` 以及临时编译产物目录，视其不存在。
3. **资源与耗时**：长耗时任务可按需持续占用计算资源，直至所有阶段执行完毕。
4. **Token 与效率控制**：
   - 思考（Thinking）与响应（Response）保持紧凑精炼、逻辑直接，避免空话与冗余客套。
   - 工具调用尽量批量化合并处理；所有结论必须有具体的源码位置作为事实支撑。
