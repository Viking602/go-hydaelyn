# 06 — Durable Runner Boundary

> Anchor: ADR-017 Durable Runner Boundary. Master spec §5.

## Goal

Document the Runner's narrowed responsibility under v0.8.0: persist,
lease, audit, and resume *multi-agent decisions made by the Scheduler*
— not make those decisions itself. Also document the Worker Runtime
(formerly in `04-execution-layer.md`) under the new architecture.

## Positioning shift

| Old (pre-reconstruction) | New (v0.8.0) |
|--------------------------|--------------|
| Multi-agent coordination lives in Runner | Durable execution of multi-agent decisions lives in Runner |
| Runner ships HandoffCommand / approval flow / blackboard primitives, Packs glue them | Runner persists Handoff / TeamState / AgentInstance; multiagent.Scheduler drives the glue |

## What Runner owns

Carried forward from v0.7.x and prior v0.8.0 drafts:

| Concern | Surface |
|---------|---------|
| Run / Task lifecycle | `RunStore`, `TaskStore`, `Runner.QueueRun`, `NextEnvelope`, `AckEnvelope` |
| Event log (monotonic Sequence) | `EventStore` (ADR-007) |
| Lease CAS | `LeaseStore.AcquireWithExpectedVersion`, `ExtendLease` |
| Approval queue | `ApprovalStore`, `ResumeTokenStore` |
| Outbox FIFO | `MailboxOutboxStore`, `UserMessageStore` |
| Idempotency ledger | `ActionAttemptStore` |
| Dead-letter | `DeadLetterStore` |
| Trace spans | `TraceStore`, `TraceSpanUpdater` |
| Usage metering | `UsageStore` |
| Storage capability reporting | `StoreCapabilities`, `CapabilityReporter` |

New for v0.8.0 (per ADR-016, ADR-017):

| Concern | Surface |
|---------|---------|
| Multi-agent handoff persistence | `HandoffStore` |
| Scheduler tick state | `TeamStateStore` |
| Per-Run agent instance ledger | `AgentInstanceStore` |

These three new stores are added to `UnitOfWork` and are **required**
of any StoreProvider (no StoreCapabilities flag — the contract is
mandatory). Storage providers that omit them fail
`contract.RunStoreProviderContractTests`.

Full store interfaces in `07-storage.md`.

## What Runner does NOT own

| Concern | Owner |
|---------|-------|
| Picking the next agent | `multiagent.Scheduler` |
| Multi-agent state machine | `multiagent.Scheduler` (state in TeamStateStore) |
| Free-form natural-language handoffs | rejected outright (typed only) |
| Agent reasoning, schema repair | `agent.Engine` |
| Cross-run application memory | application via `api.Memory[T]` |

## Task contract enrichment

```go
// api/types.go
type Task struct {
    ID           TaskID
    Role         string
    Input        json.RawMessage

    // v0.8.0 additions (all omitempty, additive)
    Budget       *TaskBudget     `json:"budget,omitempty"`
    InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
    OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// TaskBudget is the unified per-Task budget consumed by Engine and
// summed by Scheduler for team-level observability.
type TaskBudget struct {
    MaxTokens    int64         `json:"maxTokens,omitempty"`
    MaxWallClock time.Duration `json:"maxWallClock,omitempty"`
    MaxToolCalls int           `json:"maxToolCalls,omitempty"`
    MaxSteps     int           `json:"maxSteps,omitempty"`
}
```

Flow:

```
multiagent.Scheduler.Next → []Dispatch
  Dispatch.Task carries InputSchema, OutputSchema, Budget
  Runner persists Task
  Worker Runtime acquires Lease
  agent.Engine.Run(Task, OutputPolicy{Schema: Task.OutputSchema})
    Engine decrements Task.Budget per Step
    Engine validates final output against Task.OutputSchema
  agent.Engine returns Result
  Worker writes Result.Structured as TypedReport to Blackboard
  Runner appends events; emits EventSchedulerTick
  Scheduler.Next consumes updated TeamState
```

Runner never inspects `Task.Input` or `Result.Structured` for routing.
Those bytes are opaque to Runner; only the Scheduler interprets them.

## Worker Runtime

The Worker Runtime carries over from the prior `04-execution-layer.md`
mostly intact. It is the production worker process that pulls envelopes
off the mailbox, acquires leases, runs `agent.Engine`, and handles
retries / dead-lettering / graceful shutdown.

`worker/runtime.go`:

```go
package worker

type Runtime struct {
    Runner            *hydaelyn.Runner
    WorkerID          string
    Engine            AgentEngine    // wraps agent.Engine
    Concurrency       int            // default 1
    PollInterval      time.Duration  // default 250ms
    HeartbeatInterval time.Duration  // default 30s
    LeaseExtension    time.Duration  // default 2min
    MaxAttempts       int            // default 3
    BackoffStrategy   BackoffStrategy
    DeadLetterSink    DeadLetterSink
}

func (r *Runtime) Start(ctx context.Context) error
func (r *Runtime) Shutdown(ctx context.Context) error
```

Per-envelope operational protocol (unchanged from prior spec):

1. Poll: `Runner.NextEnvelope(ctx, WorkerID)`.
2. Acquire lease: `LeaseStore.AcquireWithExpectedVersion`.
3. Heartbeat goroutine extends lease every `HeartbeatInterval`.
4. Execute via `AgentEngine`.
5. On success: Ack envelope, release lease, emit `EventEnvelopeAcked`.
6. On error: classify (retryable / non-retryable via `Retryable() bool`),
   either requeue with backoff or dead-letter.
7. On context cancel: complete current or release lease, exit.

### What changes for multi-agent

`AgentEngine.ExecuteEnvelope` now:

1. Loads `Task` (including `InputSchema`, `OutputSchema`, `Budget`).
2. Constructs `agent.OutputPolicy` from `Task.OutputSchema`.
3. Calls `agent.Engine.Run(ctx, task, policy)` → returns
   `agent.Result`.
4. On `Result.Failure != nil`, emits failure event with typed
   `AgentFailure.Kind` (not bare error).
5. Writes `Result.Structured` to Blackboard via `multiagent.Write`
   (with required metadata WrittenBy, StepID).
6. Emits `EventInstanceCompleted`.

The Worker Runtime itself is unchanged at the lease/heartbeat/poll
level — only its inner `AgentEngine` invocation form changes.

### Dead-letter and BackoffStrategy

Unchanged from prior spec. `BackoffStrategy` interface and
`ExponentialBackoff` default carry forward.

## Trigger Runtime

`transport/scheduler/`, `transport/webhook/`, `transport/event/`
carry forward from the prior `04-execution-layer.md`. They translate
declarative `AgentProfile.Triggers` into `Runner.RunFromProfile` calls
(for single-agent runs) or `Team.Start` calls (for Pack-managed teams).

No structural changes for v0.8.0 — they were already aligned with the
narrowed Runner role.

## Hard rules

1. Runner code (root package + `internal/run/`, `internal/task/`,
   etc.) MUST NOT import `multiagent/**`. Communication is via the
   `api/` surface only.
2. `Task.Input` and `Result.Structured` are opaque to Runner; routing
   decisions made on their content are a Scheduler concern.
3. New `HandoffStore`, `TeamStateStore`, `AgentInstanceStore` are
   required of all StoreProvider implementations (no capability flag).
4. Resume after process kill MUST reconstruct three independent
   surfaces: Run state (Runner), Step trace (Engine, via EventStore),
   Scheduler decision history (TeamStateStore + EventStore). One
   reconstruction failing must not corrupt the other two.
5. `TaskBudget` enforcement boundary is Engine, not Runner. Runner
   records `UsageRecord` and persists `Task.Budget` but does not
   itself terminate a run on budget exhaustion.

## Verification

- `TestTask_BudgetInputOutputSchema_RoundTrip` — JSON round-trip preserves all three additions
- `TestRunner_NeverImportsMultiagent` — sentrux boundary check
- `TestRunner_OpaqueResultStructured` — Runner does not inspect `Result.Structured` bytes
- `TestWorkerRuntime_TaskCarriesOutputSchema` — engine receives OutputPolicy populated from Task.OutputSchema
- `TestWorkerRuntime_HandoffPersistedBeforeDispatch` — Dispatch.Task persisted only after HandoffStore append commits
- `TestRunner_ResumeReconstructsThreeSurfaces` — kill mid-run, restart, observe Run + Steps + Scheduler decisions all rehydrated
- `TestUnitOfWork_RequiresHandoffTeamStateAgentInstanceStores` — providers missing any of the three new stores fail contract test
- `TestEventSchedulerTick_AppendedPerResultPersist` — every Result write produces a tick event
- `TestRunner_NoFreeFormHandoff` — attempts to persist Handoff with non-validating Payload rejected
