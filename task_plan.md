# go-hydaelyn 架构优化计划

**生成时间：** 2026-05-04  
**基线数据：** sentrux scan（317 文件，32,323 行，406 import 边，Quality Signal 6,179/10,000）  
**目标：** Quality Signal → 7,500+

---

## 优先级总览

| ID | 问题 | 严重度 | 状态 |
|----|------|--------|------|
| P0 | Modularity 极低（3,355） — runner.go 79 方法 | 🔴 高 | pending |
| P1 | 测试覆盖率 9.7%（25/258 文件） | 🔴 高 | pending |
| P2 | Bus Factor 85.7%（180 文件单人维护） | 🟡 中 | pending |
| P3 | 30 个热点 + 50 个 co-change 隐式耦合对 | 🟡 中 | pending |
| P4 | 双 blackboard 层（命名冲突、层次膨胀） | 🟡 中 | pending |
| P5 | model_aliases.go（242 行别名胶水，应完全删除） | 🟢 低 | pending |

---

## Phase 0 — 基线快照

**目标：** 记录改动前的 sentrux 指标，作为所有 Phase 的对比基准。

**操作：**
```bash
sentrux scan .
sentrux health
sentrux test_gaps --limit 20
```

**验收：** findings.md 中有完整基线数据。

**状态：** complete（数据已在本次会话中采集）

---

## Phase 1 — 删除 model_aliases.go（P5）

**优先于测试先做，原因：** 改动是纯删除 + 加 import，零业务逻辑变化，make build 即可验证，成本极低，且消除后续 Phase 的认知负担。

**背景决策：**
- `internal/core/model_aliases.go`（242 行）把 `model` 包的所有类型/常量/错误重导出到 `core` 命名空间
- 项目内已有 55 个文件直接 import `internal/core/model`，别名层未能隐藏任何东西
- 仅 3 个文件从外部 import `internal/core` 并使用别名类型：`runner.go`、`hydaelyn.go`、`internal/cli/inspect.go`
- **决策：整个文件删除，不是削减**

**具体步骤：**

1. 删除 `internal/core/model_aliases.go`
2. `make build` 查看编译错误
3. 对每个编译错误：
   - `internal/core/*.go` 内部文件：加 `import "github.com/Viking602/go-hydaelyn/internal/core/model"` 并改 `ErrXxx` → `model.ErrXxx`
   - `runner.go`：加 model import，改 `core.Run` → `model.Run`，`core.Task` → `model.Task` 等（命令类型 `core.AdvanceRunCommand` 等不用动，它们定义在 core 本身）
   - `hydaelyn.go`、`internal/cli/inspect.go`：同上按编译错误处理
4. `make build && make test`

**不要碰的：**
- `core.AdvanceRunCommand`、`core.DispatchTaskCommand` 等命令类型（定义在 `internal/core`，不是别名）
- `core.Runtime`、`core.NewRuntime` 等（同上）

**验收指标：**
```
model_aliases.go 不再存在
make build — 零错误
make test  — 全绿
```

---

## Phase 2 — 拆分 runner.go（P0 Modularity）

**目标：** runner.go 从 603 行 / 79 方法 → < 60 行；Modularity 3,355 → 5,000+

**背景决策：**  
`runner.go` 是 God Interface，单文件承载全部领域的公共方法，是 Modularity 指标低的直接原因。  
拆分为同包内多文件（`package hydaelyn`），**零破坏性变更**，编译器自动验证无遗漏。

**文件拆分方案：**

| 目标文件 | 方法（共 79 个） |
|---------|----------------|
| `runner.go` | 只保留：`Runner` struct + `commandResultFromCore` + `cloneAnyMap` + `cloneStringMap` |
| `runner_run.go` | `QueueRun`, `StartRun`, `AdvanceRun`, `TransitionRun`, `Run`, `RunEvents`, `Events`, `Replay`, `ReplayRunState`, `Recover`, `RunTimeline` |
| `runner_task.go` | `CreateTask`, `Task`, `ReadyTasks`, `TransitionTask`, `SaveTask`, `LoadTask`, `ListTasks` |
| `runner_mailbox.go` | `DispatchTask`, `DispatchTaskFanOut`, `AckEnvelope`, `DeadLetter`, `QueueEnvelope`, `LoadEnvelope`, `UpdateEnvelope`, `ListEnvelopes` |
| `runner_blackboard.go` | `WriteItem`, `SelectItems`, `Subscribe`, `WaitForBlackboard` |
| `runner_response.go` | `SubmitTypedReport`, `SubmitResponseOutput`, `PublishResponse`, `DrainResponseOutbox`, `ResponseOutbox`, `SubmitUserInput`, `QueueMessage`, `LoadMessage`, `UpdateMessage`, `ListMessages`, `ListQueuedMessages` |
| `runner_governance.go` | `AcquireTaskExecution`, `HeartbeatTaskExecution`, `ReleaseTaskExecution`, `ActiveLeaseCount`, `RequestHandoff`, `RequestApproval`, `DecideApproval`, `RecoverResumeToken`, `ResumeTokens`, `StartActionAttempt`, `CompleteActionAttempt`, `StartTraceSpan`, `EndTraceSpan`, `TraceSpans`, `SaveTraceSpan`, `ListTraceSpans` |
| `runner_admin.go` | `RegisterAgent`, `Agents`, `RegisterTool`, `RegisterFlow`, `SetMessagePolicy`, `SetPolicyEngine`, `SetOutputGateway`, `SetPipeline`, `StoreProvider`, `Begin`, `SaveRun`, `LoadRun`, `AppendEvent`, `ListEvents` |

**注意：** `runTaskResultFromCore`、`mailboxResultFromCore`、`governanceResultFromCore` 这三个辅助函数跟着对应领域文件走（或保留在 runner.go 内均可）。

**验收指标：**
```
wc -l runner.go            — < 60 行
make build && make test    — 全绿
sentrux health             — Modularity 分数上升
```

---

## Phase 3 — 补测试（P1）

**目标：** 覆盖率 9.7% → 40%+（有测试文件的源文件从 25 → 100+）

**背景决策：**  
Phase 1、2 先做（无逻辑变化，不需要测试兜底），Phase 3 为后续有风险的重构（Phase 4）提供安全网。

**补测优先级（按风险 × 影响面排序）：**

| 优先级 | 包 | 理由 |
|--------|-----|------|
| 1 | `internal/core/` command_bus、command_bus_uow | 所有命令的调度根路径 |
| 2 | `internal/core/` state_transitions、state_dependency | 状态机，业务核心 |
| 3 | `internal/mailbox/` dispatch、resolver | 多 Agent 通信，co-change 对最多 |
| 4 | `internal/execution/` service、lease | 执行租约，并发竞争场景 |
| 5 | `internal/core/` approval、projection_replay | 审批流 + 事件重放 |
| 6 | `internal/core/adapter/` 全量 | 边界翻译层，类型错配 bug 温床 |

**验收指标：**
```
sentrux test_gaps --limit 20   — 高风险未测试文件 < 140
make test                       — 全绿
```

---

## Phase 4 — 合并双 blackboard 层（P4 Depth）

**目标：** 消除层次冗余，Depth 分 5,333 → 6,500+

**背景决策：**
- `internal/blackboard/service.go`：`WriteItem` 函数（写入 + 追加 Event + Trace）
- `internal/core/blackboard/service.go`：`Service` struct（`Subscribe` + `WaitForBlackboard`）
- 二者职责不同但同名，是层次感混乱的来源
- 方案：将 `internal/core/blackboard/Service` 迁移到 `internal/blackboard/subscribe.go`，删除 `internal/core/blackboard/` 目录

**具体步骤：**

1. 在 `internal/blackboard/` 新建 `subscribe.go`，将 `internal/core/blackboard/service.go` 的 `Service` struct 迁移进来（调整 package 声明即可，import 路径相同）
2. 更新引用了 `internal/core/blackboard` 的文件（预计只有 `internal/core/blackboard_subscribe.go` 和 `internal/core/blackboard_wait.go`）
3. 删除 `internal/core/blackboard/` 目录
4. `make build && make test`

**此 Phase 依赖 Phase 3 的测试覆盖作为安全网。**

**验收指标：**
```
ls internal/core/blackboard/   — 目录不存在
make build && make test        — 全绿
sentrux health                 — Depth raw 从 7 → 6
```

---

## Phase 5 — 隐式耦合显式化（P3）

**目标：** 50 个 co-change 对中的高频对，加接口隔离

**背景决策：**  
co-change 对是在 import 图中不相连、但总是一起修改的文件，属于未被接口捕获的隐式依赖。  
需要先通过 git log 分析确认实际的高频对，再逐对判断是否需要引入接口。

**步骤：**
1. `git log --follow --name-only` 分析 90 天内最高频共同修改的文件对
2. 对每个高频对：判断是否应该提取公共接口，或者合并为同一包
3. 根据判断逐个处理，每次 `make build && make test`

**验收指标：**
```
sentrux git_stats              — coupling_pairs_found 从 50 下降
sentrux health                 — Equality 分数提升
```

---

## Phase 6 — Bus Factor 降低（P2，持续进行）

**目标：** 新协作者 1 小时内理解架构并能开始贡献

**操作（与其他 Phase 并行，不阻塞）：**

1. 补充 `AGENTS.md`（sentrux deepinit 格式）：每个 `internal/` 子包一段说明，描述职责、入口、禁区
2. 将 `.sentrux/rules.toml` 中的架构注释提炼为 `docs/architecture-boundaries.md`
3. 为 Phase 4（blackboard 合并）新增 ADR，记录决策过程

---

## 执行时间线

```
Week 1    Phase 1  删除 model_aliases.go（1-2天，低风险热身）
          Phase 2  拆分 runner.go（3-4天，纯文件移动）
            ↓
Week 2-4  Phase 3  补测试（2-3周，按优先级推进）
            ↓
Week 5    Phase 4  合并 blackboard 层（需要 Phase 3 兜底）
            ↓
Week 6    Phase 5  co-change 对显式化（轻量分析 + 按需改造）
            ↓
持续      Phase 6  文档知识外化（与任意 Phase 并行）
```

---

## 预期最终指标

| 指标 | 基线 | 目标 |
|------|------|------|
| Quality Signal | 6,179 | **7,500+** |
| Modularity | 3,355 | **5,000+** |
| Depth | 5,333（7层） | **6,500+**（6层） |
| 测试覆盖（有测试文件比） | 9.7% | **40%+** |
| runner.go 行数 | 603 | **< 60** |
| model_aliases.go | 242 行 | **不存在** |

---

## 决策日志

| 日期 | 决策 | 理由 |
|------|------|------|
| 2026-05-04 | model_aliases.go 完全删除，而非削减 | 55 个文件已直接 import model，别名层未能隐藏任何东西，双重维护无价值 |
| 2026-05-04 | Phase 1/2 先于补测试执行 | 两个 Phase 均为零逻辑变更（删除/文件移动），编译器验证充分，不需要测试兜底 |
| 2026-05-04 | runner.go 拆分为同包多文件 | 保持公共 API 不变，零破坏性，编译器自动验证 |
| 2026-05-04 | blackboard 合并方向：core/blackboard → internal/blackboard | internal/blackboard 是正确的 domain 层，core 级别不应持有 service struct |

---

## 错误记录

| 错误 | Phase | 处理方式 |
|------|-------|---------|
| （暂无） | — | — |
