# ADR-007 EventStore 与 Replay 语义

## 状态

已接受

## 背景

在 v0.6 之前，team runtime 的状态主要依赖内存中的 `RunState` 与 `TeamStore` 快照。这样的问题是：

- 中断后缺少可重建的事件流
- pause / approval / abort 只有终态，没有过程证据
- admin inspect 无法回放任务生命周期

## 决策

- 引入 `storage.EventStore`
- 首批事件类型：
  - `TeamStarted`
  - `PlanCreated`
  - `TaskScheduled`
  - `TaskStarted`
  - `TaskCompleted`
  - `TaskFailed`
  - `ApprovalRequested`
  - `CheckpointSaved`
  - `TeamCompleted`
- runtime 在 team/task 生命周期中同步写事件
- `ReplayTeamState` 通过事件流重建 `RunState`

## 当前语义

- team 创建时会记录 `TeamStarted`
- planner 存在时会记录 `PlanCreated`
- task 初始生成时会记录 `TaskScheduled`
- task 进入执行时会记录 `TaskStarted`
- task 成功/失败时会记录 `TaskCompleted` / `TaskFailed`
- `ask-human` 会记录 `ApprovalRequested`
- `AbortTeam` 会落 `CheckpointSaved`
- team 正常完成会落 `TeamCompleted`

## 影响

- pause / resume / replay / abort 已经有了可持久化基础
- admin 可以查看 team events 并回放 team 状态
- 后续 v0.7 的 checkpoint / idempotency / lease 机制可以继续在这套事件模型上扩展
