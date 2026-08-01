# Durable Execution

## Current Capabilities

Venat persists enough runner detail to replay task execution, blackboard
exchange flow, user-message outbox state, and action/approval decisions — not
only final summaries.

The primary path exposes durability through:

- `venat.Runner` methods using `api.*Command` inputs
- `api.StoreProvider -> api.UnitOfWork`
- append-only events
- mailbox outbox
- response outbox
- replay projection

Default startup uses the in-memory store:

```go
runner := venat.NewDevelopment()
```

Custom durable storage is injected only when needed:

```go
runner, err := venat.NewProduction(api.Config{
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
- `api.ActionAttemptStore`
- `api.ApprovalStore`
- `api.ResumeTokenStore`
- `api.HandoffStore`
- `api.TeamStateStore`
- `api.AgentInstanceStore`
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

## Retry And Action Recovery

`api.RetryPolicy` controls durable task attempts:

- `MaxAttempts` includes the initial execution.
- `Backoff` is the first retry delay and doubles per attempt.
- `MaxBackoff` optionally caps that delay.

A failed `api.TypedReport` is redispatched only when `Retryable` is true and the
attempt limit has not been reached. `api.Task.Attempts`,
`api.TaskEnvelope.Attempts`, `api.Task.Error`, and
`api.TaskEnvelope.NextRetryAt` expose the current attempt, full failure reason,
and scheduled backoff.

`worker.AgentWorker.ExecuteContinuing` follows those retry envelopes
automatically. Each retry is a fresh full-engine attempt. It rebuilds context
from the last committed execution checkpoint, discards uncommitted partial
assistant output, and preserves the original task input. Provider streams may
first reconnect within their own bounded transport loop; a provider retry never
replays a stream after assistant content has been observed.

Execution checkpoints are accepted only from the active lease owner at the
current task version. Each checkpoint is limited to 8 MiB. A task can retain
at most 4,096 checkpoints and 512 MiB of checkpoint events.

Guarded write and external-side-effect tools use the durable action-attempt
journal. The operation key is derived from the canonical input rather than a
provider-generated call ID, so regenerated tool calls cannot repeat a completed
non-idempotent action. The journal persists the exact result returned to the
model. An unknown outcome enters reconciliation and must be resolved before
the saved call can continue. Approval interruptions are recorded as
not-executed and can be resumed without being mislabeled as an unknown side
effect.

## Event Contract

Important runner events include:

- `RunStarted`
- `RunStatusChanged`
- `TaskCreated`
- `TaskDispatched`
- `TaskExecutionAcquired`
- `TypedReportSubmitted`
- `TaskExecutionCheckpointed`
- `ActionAttemptStarted`
- `ActionAttemptUpdated`
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
