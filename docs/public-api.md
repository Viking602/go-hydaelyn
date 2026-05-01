# Public API

## Root Package Default

`hydaelyn.New()` is the default startup path. It returns a `*hydaelyn.Runner`
using the default in-memory configuration.

Use `Config` only when overriding defaults:

```go
runner := hydaelyn.New(hydaelyn.Config{
	PolicyEngine: customPolicy,
})
```

The older zero-config form remains source-compatible but is no longer the
recommended style; prefer the no-argument constructor.

## Stable Packages

The major-version public surface includes:

- `hydaelyn` — primary façade and recommended import path
- `agent`
- `blackboard`
- `flow`
- `hook`
- `message`
- `orchestrator` — advanced façade kept for compatibility and extension work
- `policy`
- `provider`
- `tool`
- `transport/mcp`
- `worker` — optional glue between `Runner` and `agent.Engine`

These packages follow the compatibility rules in [SemVer And Compatibility](semver.md).

## Runner Contract

The primary contract is Run/Task orchestration:

- `hydaelyn.New`, `hydaelyn.Runner`, `hydaelyn.Config`
- `Runner.StartRun`, `QueueRun`, `ExecuteCommand`, `RunEvents`, `RunTimeline`, `ReplayRunState`
- `Run`, `Task`, `TaskEnvelope`, `TaskExecutionLease`, `TypedReport`
- `PolicyEngine.Authorize(ctx, PolicyRequest)`
- `ApprovalRequest`, `ResumeToken`, `ActionAttempt`
- `Flow` as preset metadata, not a state-transition bypass
- `worker.AgentWorker` as optional task-envelope executor glue

`Runtime` and `orchestrator.NewRuntime` remain compatibility aliases. New code
should use `Runner` and `hydaelyn.New()`.

## Durable Storage Extension

Durable storage contracts are exposed through the public façade:

- `StoreProvider`
- `UnitOfWork`
- `RunStore`
- `TaskStore`
- `EventStore`
- `BlackboardStore`
- `MailboxOutboxStore`
- `UserMessageStore`
- `TraceStore`

Example:

```go
runner := hydaelyn.New(hydaelyn.Config{
	StoreProvider: myStoreProvider,
})
```

## CLI Surface

The v2 CLI is intentionally minimal:

```text
hydaelyn version
hydaelyn inspect-events --events PATH [--task TASKID]
hydaelyn help
```

The library is the primary surface.

## Internal Surface

These packages remain implementation details:

- `internal/core/*`
- runtime storage and UnitOfWork implementations
- mailbox outbox dispatchers
- command handlers and transition tables
- replay/recovery internals

Hydaelyn does not ship endpoint catalogs, a standard-library router, or a
canonical `net/http` route tree as part of the primary runner API.
