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
| Run / Task lifecycle | `RunStore`, `TaskStore`, Runner commands |
| Event log | `EventStore` with monotonic `Sequence` per Run (ADR-007) |
| Task lease CAS | `TaskExecutionLeaseStore` |
| Approval and resume | `ApprovalStore`, `ResumeTokenStore` |
| Outbox FIFO | `MailboxOutboxStore`, `UserMessageStore` |
| Side-effect idempotency | `ActionAttemptStore` |
| Dead-letter and trace facts | `DeadLetterStore`, `TraceStore` |
| Granular measurement | `UsageStore` |
| Multi-agent durable facts | `HandoffStore`, `TeamStateStore`, `AgentInstanceStore` |
| Optional deployment facts | `AgentDefinitionStore` |
| Optional aggregate capacity | `AdmissionReservationStore` |
| Optional coordination claims | `ResourceClaimStore` |
| Storage feature discovery | `StoreCapabilities`, `CapabilityReporter` |

These stores hold facts and CAS state, not product decisions. Runner records
that a handoff, admission transition, or resource claim happened. The scheduler,
single-run coordinator, deployment assembler, or application decides which
operation to request.

### 2. What Runner does NOT own

| Concern | Owner |
|---------|-------|
| Picking the next agent | `multiagent.Scheduler` |
| Maintaining a multi-agent state machine | `multiagent.Scheduler` |
| Coordinating one agent's durable execution | `worker.SingleRunner` |
| Installing declarative agent deployments | `worker.DefinitionDeployment` |
| Choosing governance limits and policy values | application or Pack |
| Interpreting resource keys as files/repos/accounts | application |
| Agent reasoning, schema repair, context selection | `agent.Engine` |
| Cross-run application memory writes | application via `api.Memory[T]` |

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
- Runner emits `EventSchedulerTick` after each persisted result so external
  Scheduler implementations can subscribe rather than poll.
- Resume after process kill MUST reconstruct Run state, agent Step state, and
  Scheduler state independently.
- `TaskBudget` enforcement happens at the Engine boundary. Runner persists the
  budget and typed failure but does not terminate a task independently.
- Aggregate admission is acquired by `worker.SingleRunner`,
  `worker.TeamRunner`, or a host before dispatch. Runner only exposes the
  atomic store-backed command and persists its transitions.
- Resource claims are opaque coordination keys. When a task lease and claims
  are acquired together, the store operation MUST be all-or-nothing.
- Definition snapshots are immutable execution inputs. A resumed Run uses its
  recorded definition revision rather than the deployment's current revision.

### 5. Storage contract changes

The durable multi-agent stores remain required members of `UnitOfWork`.
Definition snapshots, admission reservations, and resource claims are optional
extensions discovered through `StoreCapabilities` and narrow UnitOfWork
extension interfaces. A provider that reports a capability MUST expose the
matching store from the same transaction.

The features themselves fail closed:

- a deployment using immutable definition resume requires definition snapshots;
- aggregate quota, run-window, concurrency, or failure-breaker rules require
  admission reservations;
- tasks declaring resource claims require resource-claim support.

Providers that do not use those features remain conformant. Providers that
advertise them run the corresponding capability-gated contract suites.

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
