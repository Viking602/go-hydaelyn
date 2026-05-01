# Migration Notes

## 从旧 Team + Pattern 风格迁移到 Runner API

旧模型：

- pattern 或 host drive loop 直接推进 runnable task。
- mailbox、blackboard、queue lease 分散表达任务进度。
- 用户常需要记住多个入口名称。

当前模型：

- `hydaelyn.New()` 创建默认 `Runner`。
- `Runner.StartRun` 创建 `Run + RootTask`。
- `Runner.ExecuteCommand` 作为命令层入口，状态变更走 `StoreProvider + UnitOfWork`。
- planner/router adapter 创建一等 `Task`。
- `DispatchTask` 只写任务信封，不能授予执行权限。
- agent/component 必须 `AcquireTaskExecution` 后才能执行。
- `SubmitTypedReport` 是唯一正式任务提交协议。
- `ResponseTask` 和 `OutputGateway` 是唯一用户消息链路。
- `PolicyEngine.Authorize(ctx, PolicyRequest)` 统一覆盖 dispatch、blackboard、
  handoff、tool call、action、response publish。
- `needs_clarification` 进入 `waiting_user_input`，并通过 resume token 或用户输入恢复。

## 推荐入口

默认：

```go
runner := hydaelyn.New()
```

自定义配置：

```go
runner := hydaelyn.New(hydaelyn.Config{
	PolicyEngine: customPolicy,
})
```

旧的空配置构造器和 `NewRuntime` 构造器仍保留为兼容别名；新代码应使用
`Runner` / `New`，并只在覆盖默认配置时传入 `Config`。

## API 映射

- old team start -> `hydaelyn.New()` + `Runner.StartRun` / `Runner.QueueRun`
- team events -> `Runner.RunEvents`
- team timeline -> `Runner.RunTimeline`
- replay team state -> `Runner.ReplayRunState`
- pattern -> `Flow`
- message-only policy -> `PolicyEngine.Authorize(ctx, PolicyRequest)`
- direct user message write/publish -> `ResponseTask + ResponseOutbox + OutputGateway`

## Tool 与治理迁移

Side-effecting tool 必须补齐：

- `EffectType`
- `RequiresActionTask`
- `Idempotent`
- `PolicyTags` / risk metadata as needed

下游展示最终答案时优先消费 response outbox / `UserMessage`，不要让普通
agent 直接创建或发布用户可见消息。

## Durable runtime

当前版本支持：

- append-only events
- replay
- StoreProvider / UnitOfWork runtime contract
- durable mailbox and response outbox contracts
- approval / resume token / action attempt lifecycle

仍待后续补齐：

- 分布式 worker 调度
- 更完整的 governed tool bus
- 官方外部 durable storage driver
- 外部 observability backend
