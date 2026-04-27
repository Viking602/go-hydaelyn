# Orchestrator Runtime

`orchestrator` is the public facade for the primary Run/Task runtime. It keeps
business platform semantics and implementation details out of the public
package; command handlers, state transitions, storage, mailbox, lease,
blackboard, handoff, approval, action, response, trace, and replay internals
live under `internal/runtime`.

## Execution Chain

The runtime owns this path:

```text
StartRun
  -> created Run + RootTask
  -> AdvanceRun
  -> IntentAnalyzer event
  -> Planner creates TodoPlan
  -> PlanValidator validates plan
  -> TaskRouter creates RoutingPlan
  -> DispatchTask writes TaskEnvelope
  -> AcquireTaskExecution grants TaskExecutionLease
  -> AckEnvelope confirms envelope handling
  -> SubmitTypedReport validates lease + owner + task_version
  -> TaskStore/EventStore/Blackboard projections update
  -> ResponseTask queues sanitized UserMessage
  -> OutputGateway marks the queued message published
```

Mailbox delivery is notification only. The execution permission boundary is
`TaskExecutionLease`, and task completion is `TypedReport` accepted under that
active lease.

`Runtime.ExecuteCommand(ctx, RuntimeCommand)` is the command-layer entrypoint.
State-changing commands execute behind the internal `StoreProvider ->
UnitOfWork` contract so `RunStore + TaskStore + EventStore` updates stay
atomic. The memory runtime implements the same internal contracts as durable
drivers; it is no longer a separate semantic path.

## State Ownership

- `Run` tracks one execution instance.
- `Task` is the current task state record.
- Events are append-only replay/audit input.
- Blackboard items store shared facts and handoff context, never task ownership.
- Response outbox stores only policy-checked, redacted user payloads.
- `OutputGateway` is the only code path that marks a user message as published.
- `PolicyEngine.Authorize(ctx, PolicyRequest)` governs dispatch, blackboard
  read/write, handoff, tool call, action, and response publish boundaries.
- `TraceStore` records spans for pipeline, mailbox, lease, blackboard, policy,
  handoff, action, response, and replay-facing operations.

## Current Package Surface

The in-memory implementation covers the contract-level primitives:

- Run/task creation, strict run/task state transitions, and dependency readiness.
- `QueueRun`, `RunEvents`, `RunTimeline`, and `ReplayRunState` as the new run-facing API.
- Mailbox outbox dispatch, ack, retry scheduling, dead-letter, and task monitor
  decision events. Dead-letter policy is owned by `TaskMonitor`.
- Version-aware task execution leases.
- Typed report submission for success, completion-criteria rejection, partial
  success, retryable failure, blocked, approval, clarification, handoff, and
  action result paths.
- `needs_clarification` moves the run/task to `waiting_user_input` and creates
  a resumable blocker.
- Handoff owner transfer with critical `handoff_context` written before owner
  change events.
- Tool effect metadata and side-effecting tool gating through `ActionTask`.
- Approval manager, resume-token recovery, action attempt lifecycle, and
  reconcile-required flow.
- Response policy obligations, redaction, response outbox, user message store,
  user timeline projection, and publish gateway.
- EventStore replay that rebuilds Run/Task/UserMessage projections without
  redelivering mailbox messages, republishing user messages, or rerunning
  action tools.
- Flow registration as a preset boundary; flows that bypass runtime primitives
  are rejected.

## Adapter Boundary

Existing `host.StartTeam`, `QueueTeam`, `TeamEvents`, `TeamTimeline`, and
`ReplayTeamState` remain available through `legacy/host` or
`hydaelyn.NewTeamRuntime` as compatibility entrypoints. New orchestration work
should wrap existing planners, profiles, tools, and patterns around
`orchestrator.Runtime` instead of letting a pattern own durable state
transitions directly.
