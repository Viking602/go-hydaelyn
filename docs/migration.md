# Migration Notes

## v0.7 → v0.8 — public framework release

v0.8.0 promotes Hydaelyn from "runnable runtime" to "publishable framework." Most v0.7 callers can upgrade with a single search-and-replace pass. The full plan, including the Path A (use a reference store impl) vs Path B (bring-your-own-provider) decision rule and an end-to-end ent-based provider template, lives in `docs/product-spec/v0.8.0/12-migration-guide.md`.

### Mechanical breaking changes

| Old | New |
| --- | --- |
| `api.Flow{BypassTaskStore, BypassPolicyEngine, BypassTaskExecutionLease, BypassHandoff, BypassResponseLayer, BypassOutputGateway}` | removed — recipes do not bypass runtime invariants |
| `api.ErrFlowBypass` | removed |
| `runner.ExecuteCommand(StartRunCommand)` returning `[]any{Run, RootTask}` | returns `api.StartRunResult{Run, RootTask}` |
| `runner.ExecuteCommand(RequestApprovalCommand)` returning `[]any{Approval, Token}` | returns `api.RequestApprovalResult{Approval, Token}` |
| `runner.ExecuteCommand(AcquireTaskExecutionCommand)` returning `[]any{Lease, bool}` | returns `api.AcquireTaskExecutionResult{Lease, Acquired}` |

`api.AgentProfile` itself is unchanged; new declarative fields land on the new `api.AgentDefinition` type instead.

### New surfaces to discover

| Need | Reach for |
| --- | --- |
| Declare an agent (instructions, model, capabilities, triggers, governance) ahead of time | `api.AgentDefinition` (then `.AsProfile()` for runtime attribution) |
| Publish a system's callable surface to MCP / future renderers | `api.Capability` + `api.CapabilityManifest` |
| Cron / webhook / event / manual entrypoints | `transport/cron`, `transport/webhook`, `transport/event`, `api.Trigger` |
| Background worker that polls envelopes, leases, heartbeats, drains | `worker.Runtime` (plug your own `EnvelopePoller`) |
| Production-grade durable store | implement `api.StoreProvider` and run `contract.RunStoreProviderContractTests` against it |
| Local durable store for development | Implement `api.StoreProvider` against your own data stack — see `docs/product-spec/v0.8.0/12-migration-guide.md` for the ent-based template. The framework no longer ships reference storage backends; see ADR-012 (revised, Position D). |
| Bundle a vertical "research / support / devops / aiops" preset | `packs.Pack` + `packs.Registry` |
| Grade an agent run in CI | `eval.Eval` / `eval.Run` with assertions from `eval/assert` |

> Schedule-based triggers now live in `transport/cron`. The old
> `transport/scheduler` import path remains as a deprecated compatibility shim
> that re-exports `cron.Driver`, `cron.Options`, and `cron.New`; switch to
> `transport/cron` and update your imports.

Naming boundaries:
- `transport/cron`: time-based trigger transport; decides when a run starts.
- `workflow`: user-facing workflow definitions; compiles to `multiagent.Graph`.
- `multiagent.Scheduler`: dispatch decision primitive; decides which agent/task runs next.
- `flow` / `api.Flow`: preset adapter metadata; configures runtime adapters and never bypasses Runner invariants.

### What's not in v0.8.0 yet

- OpenAPI / CLI renderers for `CapabilityManifest` (MCP renderer ships).
- `api.ArtifactStore`, `api.BudgetPolicy`, `api.PolicyEnforcer` — types and interfaces deferred to v0.8.1+. The data shapes they will consume (UsageRecord, Budget, ContextSource) are stable in v0.8.0.
- `api.Memory[T Identified]` — ships in v0.8.0 as an optional plugin. The framework defines the verbs (Write/Read/Forget) and the identity contract (Identified). The application defines `T` and provides the storage backend; no reference implementation ships. See ADR-013 (revised) and `docs/product-spec/v0.8.0/13-memory-optional-plugin.md`.
- OpenTelemetry exporter (`observe/otel/`).

See `docs/release-notes/v0.8.0.md` for the full deferral list.

---

## 从旧 Team + Pattern 风格迁移到 Runner API

旧模型：

- pattern 或 host drive loop 直接推进 runnable task。
- mailbox、blackboard、queue lease 分散表达任务进度。
- 用户常需要记住多个入口名称。

当前模型：

- `hydaelyn.New()` 创建默认 `Runner`。
- `Runner.StartRun` 创建 `Run + RootTask`。
- `Runner.ExecuteCommand` 作为命令层入口，状态变更走 `api.StoreProvider + api.UnitOfWork`。
- planner/router adapter 创建一等 `api.Task`。
- `DispatchTask` 只写任务信封，不能授予执行权限。
- agent/component 必须 `AcquireTaskExecution` 后才能执行。
- `SubmitTypedReport` 是唯一正式任务提交协议。
- `ResponseTask` 和 `OutputGateway` 是唯一用户消息链路。
- `api.PolicyEngine.Authorize(ctx, api.PolicyRequest)` 统一覆盖 dispatch、blackboard、
  handoff、tool call、action、response publish。
- `needs_clarification` 进入 `waiting_user_input`，并通过 resume token 或用户输入恢复。

## 推荐入口

默认：

```go
runner := hydaelyn.New()
```

自定义配置：

```go
runner := hydaelyn.New(api.Config{
	PolicyEngine: customPolicy,
})
```

内部 `Runtime` 仍存在于实现层，但新代码应通过 `Runner` + `api` 契约扩展，
不要直接 import `internal/core`。

## API 映射

- old team start -> `hydaelyn.New()` + `Runner.StartRun` / `Runner.QueueRun`
- team events -> `Runner.RunEvents`
- team timeline -> `Runner.RunTimeline`
- replay team state -> `Runner.ReplayRunState`
- pattern -> `api.Flow`
- message-only policy -> `api.PolicyEngine.Authorize(ctx, api.PolicyRequest)`
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
- `api.StoreProvider / api.UnitOfWork` runtime contract
- durable mailbox and response outbox contracts
- approval / resume token / action attempt lifecycle

仍待后续补齐：

- 分布式 worker 调度
- 更完整的 governed tool bus
- 官方外部 durable storage driver
- 外部 observability backend
