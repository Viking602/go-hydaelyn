# Public API

## Root Package Default

`hydaelyn.New()` is the default startup path. It returns a `*hydaelyn.Runner`
using the default in-memory configuration.

Import `github.com/Viking602/go-hydaelyn/api` for all public contracts:

```go
runner := hydaelyn.New(api.Config{
	PolicyEngine: customPolicy,
})
```

The zero-config form remains the preferred startup path when you do not need
to override storage, policy, output, or pipeline dependencies.

## Stable Packages

The major-version public surface includes:

- `hydaelyn` — primary façade and recommended import path for construction
- `api` — Config, commands, interfaces, Run/Task value contracts, constants, errors
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

The primary contract is split across the root facade and the api package:

- `hydaelyn.New`, `hydaelyn.Runner`
- `api.Config`
- `api.StartRunCommand`, `api.CreateTaskCommand`, other `api.*Command` values
- `api.Run`, `api.Task`, `api.TaskEnvelope`, `api.TaskExecutionLease`, `api.TypedReport`
- `api.PolicyEngine.Authorize(ctx, api.PolicyRequest)`
- `api.ApprovalRequest`, `api.ResumeToken`, `api.ActionAttempt`
- `api.Flow` as preset metadata, not a state-transition bypass
- `worker.AgentWorker` as optional task-envelope executor glue

`internal/core` remains implementation detail. New code should not import it.

## Durable Storage Extension

Durable storage contracts are exposed through `api`:

- `api.StoreProvider`
- `api.UnitOfWork`
- `api.RunStore`
- `api.TaskStore`
- `api.EventStore`
- `api.BlackboardReadWriter`
- `api.MailboxOutboxStore`
- `api.UserMessageStore`
- `api.TraceStore`

Example:

```go
runner := hydaelyn.New(api.Config{
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
