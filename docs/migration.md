# Migration Notes

## 从 v0.1 风格迁移到当前 runtime

## 从 Team + Pattern 迁移到 Orchestrator Runtime

旧模型：

- `Pattern.Start` 创建完整 `team.RunState`
- pattern 或 host drive loop 直接推进 runnable task
- mailbox、blackboard、queue lease 分散表达任务进度

新模型：

- `orchestrator.StartRun` 创建 `Run + RootTask`
- `Runtime.ExecuteCommand` 作为命令层入口，状态变更走 `StoreProvider + UnitOfWork`
- planner/router adapter 创建一等 `Task`
- `DispatchTask` 只写任务信封，不能授予执行权限
- agent/component 必须 `AcquireTaskExecution` 后才能执行
- `SubmitTypedReport` 是唯一正式任务提交协议
- `ResponseTask` 和 `OutputGateway` 是唯一用户消息链路
- `PolicyEngine.Authorize(ctx, PolicyRequest)` 统一覆盖 dispatch、blackboard、
  handoff、tool call、action、response publish
- `needs_clarification` 进入 `waiting_user_input`，并通过 resume token 或用户输入恢复

兼容策略：

- `hydaelyn.New` 默认返回 Orchestrator runtime。
- `host.StartTeam` / `hydaelyn.NewTeamRuntime` 仍可用，但只作为 legacy compatibility entrypoint。
- 新代码优先使用 root `hydaelyn` / `orchestrator` 包；旧 pattern 可以作为 planner/router preset 输入。
- side-effecting tool 必须补齐 `EffectType` / `RequiresActionTask` / `Idempotent`
  等治理字段，并通过 ActionTask 运行。
- 下游展示最终答案时优先消费 response outbox / `UserMessage`，不要让普通
  agent 直接创建或发布用户可见消息。

API 映射：

- `StartTeam` -> `StartRun` / `QueueRun`
- `QueueTeam` -> `QueueRun`
- `TeamEvents` -> `RunEvents`
- `TeamTimeline` -> `RunTimeline`
- `ReplayTeamState` -> `ReplayRunState`
- `Pattern` -> `Flow`
- `SupervisorProfile` -> `ControllerProfile`
- `WorkerProfiles` -> `AgentProfiles`
- message-only policy -> `PolicyEngine.Authorize(ctx, PolicyRequest)`
- direct user message write/publish -> `ResponseTask + ResponseOutbox + OutputGateway`

### 1. Task assignee

旧模型：

- 直接用 profile 名承载 assignee

新模型：

- 运行时 identity 由 `AgentInstance` 承载
- task 绑定 `RequiredRole + AssigneeAgentID`

### 2. Pattern 语义

旧模型：

- pattern 直接承担拆分和调度

新模型：

- pattern 可以提供 template
- planner 可以生成 typed plan
- runtime 按 planner review/replan 驱动 team

### 3. Verification

旧模型：

- 直接拼 summary

新模型：

- research task 先发布到 `blackboard`
- verify task 产生结构化 `VerificationResult`
- synthesizer 只吃 supported claims

### 4. Capability 治理

旧模型：

- provider/tool 走各自调用路径

新模型：

- provider/tool 已接入统一 `CapabilityInvoker`
- timeout / retry / permission / approval / rate limit 已进入 capability policy

### 5. Durable runtime

当前版本已经支持：

- EventStore
- replay
- pause / resume / abort
- admin inspect/replay/events
- StoreProvider / UnitOfWork runtime contract
- durable mailbox and response outbox contracts
- approval / resume token / action attempt lifecycle

仍待后续补齐：

- 分布式 worker 调度
- 更广义 capability 接入
- 外部 observability backend
