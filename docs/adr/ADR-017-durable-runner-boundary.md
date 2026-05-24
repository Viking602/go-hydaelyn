# ADR-017 Durable Runner Boundary — Runner persists multi-agent decisions, it does not make them

## Status

Accepted — enforced from the v0.8.0 reconstruction onward. Anchor documents:
`docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §5,
`docs/product-spec/v0.8.0/06-durable-runner.md`,
`docs/product-spec/v0.8.0/11-boundaries.md` Principle 5.

## Context

Pre-reconstruction v0.8.0 framing positioned the Runner as the place
where "multi-agent coordination lives". The Runner shipped
HandoffCommand, BlackboardReadWriter, ApprovalStore, and the implicit
expectation that recipes drove scheduling decisions through it.

In practice this overloads the Runner with two unrelated
responsibilities:

1. **Durable execution** — persistence, replay, lease, approval
   queue, idempotency ledger. These are runtime-property concerns.
2. **Multi-agent coordination** — picking the next agent, routing
   handoffs, accumulating team state. These are scheduling
   concerns.

Bundling these means:

- Scheduling improvements (a new routing strategy) cause Runner
  surface changes.
- Runtime improvements (a new lease backend) require revalidating
  scheduling assumptions.
- The Runner's invariants ("every state change replayable") cannot
  be reasoned about independently of "how does the Scheduler
  decide what to dispatch next."

With `multiagent/` becoming a first-class package (ADR-016), the
overload becomes a wart. The Runner's job needs to be redefined.

## Decision

The Runner's positioning shifts from

> *multi-agent coordination lives in Runner*

to

> *durable execution of multi-agent decisions lives in Runner*.

The Runner persists, leases, audits, and resumes; it does not
schedule.

### 1. What Runner owns

| Concern | Surface |
|---------|---------|
| Run / Task lifecycle | `RunStore`, `TaskStore`, `Runner.QueueRun`, `Runner.NextEnvelope`, `Runner.AckEnvelope` |
| Event log | `EventStore` with monotonic `Sequence` per Run (ADR-007) |
| Lease CAS | `LeaseStore.AcquireWithExpectedVersion`, `ExtendLease` |
| Approval queue | `ApprovalStore` + `ResumeToken` |
| Outbox FIFO | `MailboxOutboxStore`, `UserMessageStore` |
| Idempotency ledger | `ActionAttemptStore` |
| Dead-letter | `DeadLetterStore` |
| Trace spans | `TraceStore`, `TraceSpanUpdater` |
| Usage metering | `UsageStore` |
| Storage capabilities | `StoreCapabilities`, `CapabilityReporter` |
| **New for v0.8.0** | `HandoffStore`, `TeamStateStore`, `AgentInstanceStore` |

The three new stores hold *multi-agent state* but not *multi-agent
decisions*. Runner records that a Handoff happened (auditable,
replayable). It does not decide who the Handoff goes to — the
Scheduler does.

### 2. What Runner does NOT own

| Concern | Owner |
|---------|-------|
| Picking the next agent | `multiagent.Scheduler` (ADR-016) |
| Maintaining a multi-agent state machine | `multiagent.Scheduler` |
| Translating natural-language handoffs into commands | rejected outright (typed Handoff only — ADR-016) |
| Agent reasoning, schema repair, context selection | `agent.Engine` (ADR-015) |
| Cross-run application memory writes | application via `api.Memory[T]` (ADR-013) |

### 3. The Runner-to-Scheduler bridge

The bridge is the enriched `api.Task` (master spec §6):

```go
type Task struct {
    ID           TaskID
    Role         string
    Input        json.RawMessage
    Budget       *TaskBudget     // new in v0.8.0
    InputSchema  json.RawMessage // new in v0.8.0
    OutputSchema json.RawMessage // new in v0.8.0
}
```

Flow:

```
multiagent.Scheduler.Next → []Dispatch{Task}
    Runner persists Task via TaskStore
    Worker Runtime acquires lease, runs agent.Engine
    agent.Engine reads Task.{InputSchema, OutputSchema, Budget}
    agent.Engine returns Result
    Runner appends events, writes TypedReport to Blackboard
    Runner emits scheduler-tick event
    multiagent.Scheduler.Next reads updated state
```

The Runner never inspects the *content* of `Task.Input` or
`Result.Structured` for scheduling purposes. Those bytes are
opaque to it; only the Scheduler interprets them.

### 4. Hard constraints (immediately effective)

- Runner code (root package, `internal/run/`, `internal/task/`,
  `internal/blackboard/`, `internal/mailbox/`, etc.) MUST NOT
  import `multiagent/**`. Runner serves Scheduler over the `api/`
  surface only.
- `api.HandoffCommand` is retained for explicit Pack-driven
  handoffs (back-compat); new code SHOULD route handoffs through
  `multiagent.Scheduler` and `HandoffStore`. The deprecation path
  is documented in 14-migration-guide.md.
- Runner emits a `EventSchedulerTick` after each persisted result
  so external Scheduler implementations can subscribe rather than
  poll.
- Resume after process kill MUST reconstruct: Run state (Runner),
  Step trace (Engine, via EventStore), Scheduler decision history
  (TeamStateStore + EventStore). The three reconstructions are
  independent — failing one does not corrupt the others.
- `TaskBudget` enforcement happens at Engine boundary (per
  ADR-015). Runner persists `Task.Budget` and reports usage via
  `UsageStore`, but does not itself terminate runs on budget
  exhaustion — that surfaces as `FailureBudgetExhausted` from
  Engine, which the Scheduler decides how to act on.

### 5. Storage contract changes

`UnitOfWork` gains three methods:

```go
type UnitOfWork interface {
    // ... existing 15 stores
    Handoffs()       HandoffStore       // new in v0.8.0
    TeamStates()     TeamStateStore     // new in v0.8.0
    AgentInstances() AgentInstanceStore // new in v0.8.0
}
```

Per ADR-012 Position D, the framework ships no implementation of
these stores. Application providers must implement them and pass
`contract.RunStoreProviderContractTests` (which is extended for the
three new stores in v0.8.0).

`StoreCapabilities` gains no new flag in v0.8.0 — the three new
stores are required, not optional. Applications upgrading from a
v0.7-era provider must add implementations as part of the
migration (documented in 14-migration-guide.md).

## Consequences

- Runner becomes a coherent durable-execution kernel. Its
  invariants (replay determinism, lease CAS, FIFO outbox, audit
  completeness) are reasoned about without reference to scheduling
  policy.
- Scheduling improvements (new Scheduler implementations,
  voting/debate patterns in v0.9.0) ship in `multiagent/` without
  touching Runner code.
- Storage providers can be reviewed against the contract alone;
  scheduling semantics are not part of the storage contract.
- The Runner's API surface shrinks — Pack-helper handoffs deprecate
  in favor of Scheduler-driven handoffs, reducing public surface
  area.

## Compatibility with existing ADRs

- **ADR-007 (EventStore replay)** — strengthened. The Runner's
  promise of monotonic Sequence per RunID is now also load-bearing
  for Scheduler replay.
- **ADR-008 (revised)** — Runner remains free of business
  vocabulary. The three new stores carry only framework-primitive
  nouns (Handoff, TeamState, AgentInstance), all on the multi-agent
  primitive exception list.
- **ADR-012 (Position D)** — extended. Framework still ships no
  StoreProvider; the contract simply has three more stores.
- **ADR-013** — unchanged. Memory remains an optional plugin;
  Runner does not see Memory.
- **ADR-014 (revised)** — AgentInstance is a Scheduler-layer
  concept; Runner persists it via `AgentInstanceStore` but does not
  reason about agent identity.

## References

- Master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §5
- Design: `docs/product-spec/v0.8.0/06-durable-runner.md`
- Storage contract additions: `docs/product-spec/v0.8.0/07-storage.md`
- Migration: `docs/product-spec/v0.8.0/14-migration-guide.md`
- Companion ADRs: ADR-015 (Engine), ADR-016 (Scheduler)
- Related: ADR-007 (EventStore replay), ADR-012 (storage Position D), ADR-014 (agent ontology)
