# Durable Execution

## Current Capabilities

Hydaelyn persists enough runner detail to replay task execution, blackboard
exchange flow, user-message outbox state, and action/approval decisions — not
only final summaries.

The primary path exposes durability through:

- `hydaelyn.Runner` methods using `api.*Command` inputs
- `api.StoreProvider -> api.UnitOfWork`
- append-only events
- mailbox outbox
- response outbox
- replay projection

Default startup uses the in-memory store:

```go
runner := hydaelyn.NewDevelopment()
```

Custom durable storage is injected only when needed:

```go
runner, err := hydaelyn.NewProduction(api.Config{
	StoreProvider: myStoreProvider,
	PolicyEngine:  myPolicy,
})
```

## Store Contract

The public storage contracts are:

- `api.StoreProvider`
- `api.UnitOfWork`
- `api.RunStore`
- `api.TaskStore`
- `api.EventStore`
- `api.BlackboardReadWriter`
- `api.MailboxOutboxStore`
- `api.UserMessageStore`
- `api.TraceStore`
State-changing commands run behind the `UnitOfWork` boundary so run, task,
event, mailbox, blackboard, user-message, and trace updates can be committed
atomically by a durable driver.

## Lease Contract

Task execution leases model a concrete execution attempt:

- `leaseId`
- `runId`
- `taskId`
- `taskVersion`
- `holderType`
- `holderId`
- `expiresAt`
- `heartbeatAt`
- `status`

A task can only complete through `SubmitTypedReport` accepted under an active,
matching, version-aware lease.

## Event Contract

Important runner events include:

- `RunStarted`
- `RunStatusChanged`
- `TaskCreated`
- `TaskDispatched`
- `TaskExecutionAcquired`
- `TypedReportSubmitted`
- `TaskCompleted`
- `TaskFailed`
- `BlackboardItemWritten`
- `ApprovalRequested`
- `UserMessageQueued`
- `ResponsePublished`

Replay rebuilds projections from these events without redelivering mailbox
messages, republishing user messages, or rerunning action tools.

## Current Limits

- The default store is in-memory.
- No official production durable backend ships in-tree yet.
- Distributed worker scheduling remains a follow-up layer.
- Trace enrichment exists in event payloads, but full OpenTelemetry export
  remains a follow-up layer.
