# Runner Runtime

The root `hydaelyn` package is the recommended façade for the primary Run/Task
runner. Public contracts live in the `api` package; `internal/core` remains the
composition root behind the façade.

Implementation details now live across focused `internal/*` packages. `internal/core`
keeps the Runtime façade, command registration, configuration, compatibility
aliases, and policy/trace glue; domain behavior lives in `internal/{run,task,
mailbox,execution,blackboard,response,report,approval,action,handoff,toolgate,
trace,memory,store,command}`. These packages remain implementation details, not
public extension points.

## Execution Chain

The runner owns this path:

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

`Runner.ExecuteCommand(ctx, Command)` is the command-layer entrypoint.
State-changing commands execute behind the `StoreProvider -> UnitOfWork`
contract so `RunStore + TaskStore + EventStore` updates stay atomic. The
default in-memory runner implements the same contracts as durable drivers.

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
- `QueueRun`, `RunEvents`, `RunTimeline`, and `ReplayRunState` as the run-facing API.
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
- Tool effect metadata and side-effecting tool gating through action-capable tasks.
- Approval manager, resume-token recovery, action attempt lifecycle, and
  reconcile-required flow.
- Response policy obligations, redaction, response outbox, user message store,
  user timeline projection, and publish gateway.
- EventStore replay that rebuilds Run/Task/UserMessage projections without
  redelivering mailbox messages, republishing user messages, or rerunning
  action tools.
- Flow registration as a preset boundary; flows that bypass runner primitives
  are rejected.

## Naming

New code should use:

```go
runner := hydaelyn.NewDevelopment()
```

Use `api.Config{...}` only for overrides. The internal runtime remains an
implementation detail behind the root `Runner` facade.
