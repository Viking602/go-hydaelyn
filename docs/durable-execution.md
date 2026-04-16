# Durable Execution

## 当前能力

当前 runtime 已支持：

- `EventStore`
- `ReplayTeamState`
- `pause / resume / abort`
- `ApprovalRequested` 事件
- `QueueTeam / RunQueueWorker / RecoverQueueLeases`

## Admin

当前 admin 已支持：

- `/teams`
- `/teams/{id}`
- `/teams/{id}/events`
- `/teams/{id}/resume`
- `/teams/{id}/replay`
- `/teams/{id}/abort`
- `/scheduler/drain`
- `/scheduler/recover`

## 当前限制

- queue 仍以内存模型为主
- 还没有真正的外部 durable backend
- checkpoint 还没有细粒度 payload
