## 六、多 Agent 协同标准化实战指南 (Reusable Skill Guide)

> 可复用的标准化多 Agent 并行协作模式，适用于研发、信息整理等任意可拆解的任务类型。

### 6.1 并行的第一性原理

1. **先问是否值得并行**：整体任务简单，或任务包间依赖强、多数须串行时，并行的编排开销大于收益——主控直接顺序完成即可，不必启用 Sub Agent。
2. **工作区隔离是前提**：并发 Agent 不得在同一工作区写入同一批产物。代码类写任务用 `git worktree` 隔离；只读任务或产出互不相交的任务可直接在主工作区并行。
3. **正交任务才能真并行**：只有产出/变更集合无交集的任务才能同时派发；有依赖或共享产物的任务必须串行编排。
4. **白名单锁死变更半径**：Agent 天生有"顺手多改"的倾向，必须用任务说明书中的白名单强行约束。

---

### 6.2 任务说明书 (Task Spec) 模板

说明书统一存放于主工作区 `_subtasks/`，由主控创建、验收通过后清理，Worker 只读不写。一份让 Worker 自治交付的说明书至少包含三部分（下例以代码任务为例，其余任务类型按同结构替换对应项）：

```markdown
# 任务说明书：[任务组名]

## 一、协作原则与红线（铁律）
1. 工作区限制：仅在指定工作区内操作。
2. 变更白名单（极度关键）：✅ 允许改动 [精确到路径]；❌ 白名单以外一律禁止。
3. 共享文件由主控独占（如 CHANGELOG、设计文档）；待登记事项写入不提交的 NOTES_FOR_LEAD.md。
4. 忽略目录：临时产物目录（`_tmp/`、`archived/` 等）视为不存在，严禁读取或修改。
5. 并发抗干扰：同目录可能有其他 Agent 作业，对其输出钝感；绝不触碰无关改动，只精准暂存白名单内文件。
6. 语义变更预警：[预判会打红的既有验收，声明期望处理方式：交回主控 / 修数据但不削弱断言并逐条记录]。
7. 交付规范：[命名与格式约束；代码任务另附 commit 规范，严禁添加 trailer]。

## 二、任务清单 (Action Plan)
### 任务 1: [标题]
- 背景与目标：[为什么做、做成什么样，定位到具体位置]
- 具体产出：[要点 1, 2, 3]
- 验收方式：[可复验的命令或客观标准]

## 三、验收与交付
1. 自验：逐条跑完上述验收方式。
2. 范围自查：`git status -s` 确认无越界文件（非代码任务则核对交付物清单）。
3. 交付：代码任务执行 commit；其他任务报告产物路径。
```

---

### 6.3 派发与监控编排

代码类写任务用 worktree 隔离；只读或产出不相交的任务跳过第 1 步直接派发。

```bash
# 0. 连通性验证（可选；收到 pong 即可，勿探究配置）
pi -p "ping" < /dev/null

# 1. 准备 worktree（代码类写任务）
git worktree add -b feat/<task-name> ../<worktree-dir> main
cp _subtasks/<task-name>.md ../<worktree-dir>/TASK_SPEC.md   # 未跟踪的 spec 与参考文档须显式 cp 进来，并在 spec 中声明 worker 工作区内不存在的路径（严禁自行寻找）

# 2. 派发（后台非交互式；--mode json 把全部事件实时以 JSONL 写入进度日志）
#    严禁 -p：print 模式运行期间 stdout 无输出，结束时才吐最终回复——盲等的根源
#    非 worktree 任务：去掉 cd，@TASK_SPEC.md 改为 @_subtasks/<task-name>.md
(cd ../<worktree-dir> && pi --mode json --approve @TASK_SPEC.md "严格按 TASK_SPEC.md 执行并交付。" \
  < /dev/null > /tmp/agent-<task-name>.jsonl 2> /tmp/agent-<task-name>.err) &
PID=$!   # 同步登记 PID；严禁 pkill 等盲杀

# 3. 监控（事件流实时可见：每条命令执行、测试运行、错误重试）
tail -f /tmp/agent-<task-name>.jsonl | jq -c '
  select(.type == "tool_execution_start" or .type == "tool_execution_end") |
  {ev: .type, tool: .toolName, cmd: (.args.command // empty), err: (.isError // empty)}'
# message_end 事件含完整回复正文，事后可回放；辅以任务清单对照，确认真实推进而非空转

# 4. 回收：代码任务 merge 后全局验收；其他任务直接收取产物
git merge feat/<task-name> && go test -race ./...

# 5. 清理：全部任务复核验收通过后，清掉 worktree 与 _subtasks/（执行记录保留在外，不清）
git worktree remove --force ../<worktree-dir> && git branch -d feat/<task-name>
rm -rf _subtasks/
```

> **监控与故障恢复**：可观测性完全来自 `--mode json` 事件流，零依赖 Worker 自觉汇报。卡死判据可程序化：连续多分钟无新 `tool_execution_start`，或连续 `tool_execution_end` 且 `isError: true` 重试同一命令。可恢复的：kill 后用 `--session <session-file> --mode json "<补充指令>"` 续跑，上下文完整保留；不可恢复的：按登记的 PID 终止、回退改动、重新派发。

> **派发命令要点**：
> - `@file` 必须是独立 argv 项，写进引号会被当成文件名导致派发空转：`pi --mode json --approve @A.md @B.md "<指令>"`。
> - 多 worker 并发时每个派发用独立的进度日志（按任务名区分），严禁共用。
> - Pi CLI 已预配置打通（绑定远端 VMR 服务），主控与 Worker 严禁额外配置或研究其模型设定。

---

### 6.4 主控 (Lead) 的质量收敛守则

1. **整合后全局验收**：合并/汇拢各 Worker 产出后，主控独立复跑全局验收（代码任务：全局测试 + 架构门禁；其他任务：交叉核对）——文本合并成功不代表逻辑自洽，"Worker 说全绿"和"主控看到全绿"是两件事。
2. **审查交付洁净度**：检查是否混入临时脚本、无关文件或未跟踪产物。
3. **共享产物由主控独占**：Worker 的待登记事项一律写入不提交的 NOTES_FOR_LEAD.md；多批并行能否零冲突，这一条是主因。
4. **汇总前事实核查**：对 Worker 反馈中的关键结论独立核实，避免盲信。
5. **计划先行、全程有账**：开工前先定完整计划（任务拆解与依赖排序），对照计划逐项登记状态与 PID（待派发 / 进行中 / 已交付 / 已整合），逐项销项；规划、执行过程摘要、验收结果与整体总结须记录成文——用户指定了文件就写在该文件，否则主控自建一份（如 `EXECUTION_REPORT.md`），不放入验收后即清空的 `_subtasks/`。
6. **故障按需处置**：依据日志与现场状态独立判断，可恢复则续跑，不可恢复则按 PID 终止重派——既不盲目重启，也不放任空转。

---

### 6.5 典型陷阱速查

| 陷阱 | 防御 |
|---|---|
| 未锁白名单导致范围漂移，产出互相覆盖或合并冲突 | 说明书用 ❌ 明确声明禁改清单 |
| 并行 Worker 的临时文件/测试组件同名冲突 | 命名携带任务标识或随机后缀（如 `probe-${timestamp}`） |
| worktree 含未跟踪文件导致 `git worktree remove` 失败 | 清理用 `--force` |

---

### 6.6 Worker 执行纪律

1. **独立判断**：不盲从既有文档与历史结论，立足现状审视；有理有据时敢于挑战旧有假设。
2. **资源与效率**：长耗时任务可持续占用资源直至完成；思考与响应紧凑直接，工具调用批量化，结论必须落到具体出处。
3. **变更边界克制**：对其他并发作业的输出与中间产物钝感；绝不探查、改动或回滚无关修改；`git add <file>` 精准暂存，严禁 `git add .`。
