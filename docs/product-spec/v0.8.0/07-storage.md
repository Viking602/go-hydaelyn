# 07 — Storage Contract (Position D)

> Renumbered from `05-storage.md` in the v0.8.0 reconstruction.
> Position D (ADR-012 revised): framework ships **no** `StoreProvider`
> implementation. The framework owns the contract, the conformance
> suite, and the conformance harness; users own backends.

## Decision recap

- The framework's job is to **define** the storage contract and prove backends conform to it.
- The framework's job is **not** to ship a production backend. Postgres / MySQL / SQLite / Mongo / ent live in user code or in third-party adapter modules.
- The framework retains exactly one storage implementation, gated behind `internal/contract/inmemfake`, used only by the conformance suite itself to verify the suite is self-consistent. It is not importable.

## Total stores in v0.8.0: 18

Carried forward from the prior 15 stores:

1. `RunStore`
2. `TaskStore`
3. `EventStore` (monotonic Sequence)
4. `TaskExecutionLeaseStore` (LeaseCAS)
5. `OutboxStore` (FIFO per partition)
6. `IdempotencyStore` (ActionAttemptStore)
7. `ResumeTokenStore`
8. `ApprovalStore`
9. `BlackboardStore`
10. `MailboxStore`
11. `TraceStore`
12. `UsageStore`
13. `DeadLetterStore`
14. `ScheduleStore`
15. `WebhookStore`

New for v0.8.0 (3 stores — required, no capability flag):

16. **`HandoffStore`** — persists `multiagent.Handoff` records
17. **`TeamStateStore`** — persists `multiagent.TeamState` snapshots
18. **`AgentInstanceStore`** — persists `multiagent.AgentInstance` lifecycle

All three are **required** members of `UnitOfWork`. There is no
capability flag for them. Backends that do not implement them are
non-conformant; their conformance suite runs fail.

Rationale: the v0.8.0 four-layer architecture treats the multi-agent
layer as kernel, not optional. Per `11-boundaries.md` Principle 5
(three-surface reconstruction), a backend that cannot persist
HandoffStore + TeamStateStore + AgentInstanceStore cannot satisfy
resume correctness for multi-agent runs. Making them optional would
make resume conditional, which is not acceptable.

## UnitOfWork shape

```go
package api

type UnitOfWork interface {
    Runs() RunStore
    Tasks() TaskStore
    Events() EventStore
    Leases() TaskExecutionLeaseStore
    Outbox() OutboxStore
    Idempotency() IdempotencyStore
    ResumeTokens() ResumeTokenStore
    Approvals() ApprovalStore
    Blackboard() BlackboardStore
    Mailboxes() MailboxStore
    Traces() TraceStore
    Usage() UsageStore
    DeadLetters() DeadLetterStore
    Schedules() ScheduleStore
    Webhooks() WebhookStore

    // v0.8.0 additions — required
    Handoffs() HandoffStore
    TeamStates() TeamStateStore
    AgentInstances() AgentInstanceStore

    Commit() error
    Rollback() error
}
```

## New store contracts

### HandoffStore

```go
type HandoffStore interface {
    Save(ctx context.Context, runID RunID, h multiagent.Handoff) error
    Get(ctx context.Context, runID RunID, handoffID HandoffID) (multiagent.Handoff, error)
    List(ctx context.Context, sel HandoffSelector) ([]multiagent.Handoff, error)
}

type HandoffSelector struct {
    RunID  *RunID
    From   *AgentInstanceID
    To     *AgentInstanceID
    Since  *time.Time
}
```

**Invariants**:

- `Save` is append-only — no update path.
- `(runID, handoffID)` is the unique key. `handoffID` is ULID derived
  by Scheduler.
- `List` MUST return Handoffs in ULID ascending order (i.e. wall-clock
  monotonic per Scheduler tick).

### TeamStateStore

```go
type TeamStateStore interface {
    Save(ctx context.Context, runID RunID, state multiagent.TeamState) error
    Load(ctx context.Context, runID RunID) (multiagent.TeamState, error)
}
```

**Invariants**:

- `Save` overwrites the latest snapshot atomically.
- `Load` returns the most recent snapshot or `ErrNotFound`.
- `TeamState` is small (current dispatch round, scheduler-private bookkeeping). It is NOT the audit trail — that is `EventStore` `EventSchedulerTick`.
- Snapshot frequency is Scheduler's call; the store MUST handle high-frequency writes (1 per tick) without locking out reads.

### AgentInstanceStore

```go
type AgentInstanceStore interface {
    Save(ctx context.Context, inst multiagent.AgentInstance) error
    Get(ctx context.Context, id AgentInstanceID) (multiagent.AgentInstance, error)
    List(ctx context.Context, sel AgentInstanceSelector) ([]multiagent.AgentInstance, error)
}

type AgentInstanceSelector struct {
    RunID     *RunID
    ClassName *string
    Status    *AgentInstanceStatus
}
```

**Invariants**:

- `Save` upserts on `inst.ID`. `inst.ID` is deterministic per `ComputeInstanceID(RunID, ClassName, SpawnSeq)` (`04-agent-class.md`).
- Instance status transitions are append-only via `EventStore`
  (`EventInstanceSpawned` → `EventInstanceCompleted` |
  `EventInstanceFailed`). `AgentInstanceStore.Save` reflects the
  latest status; the audit trail is the event log.

## Capability flags

`StoreCapabilities` carries forward (e.g. `SupportsBlackboardSelector`,
`SupportsRunSelectorByCreatedAt`). v0.8.0 adds **no** new capability
flags — the new stores are required, not capability-gated.

## Conformance suite

`contract/`:

```
contract/
├── README.md
├── suite.go               (top-level RunSuite(t, factory))
├── runs/                  (RunStore tests)
├── tasks/                 (TaskStore tests)
├── events/                (EventStore tests — monotonic Sequence)
├── leases/                (LeaseCAS tests)
├── outbox/                (FIFO + idempotency tests)
├── ...
├── handoffs/              (NEW — HandoffStore tests)
├── teamstates/            (NEW — TeamStateStore tests)
├── agentinstances/        (NEW — AgentInstanceStore tests)
└── internal/inmemfake/    (framework-internal — not importable)
```

`contract/suite.go`:

```go
package contract

type Factory func(t *testing.T) api.StoreProvider

func RunSuite(t *testing.T, f Factory) {
    runs.Run(t, f); tasks.Run(t, f); events.Run(t, f); leases.Run(t, f)
    outbox.Run(t, f); idempotency.Run(t, f); resumetokens.Run(t, f)
    approvals.Run(t, f); blackboard.Run(t, f); mailboxes.Run(t, f)
    traces.Run(t, f); usage.Run(t, f); deadletters.Run(t, f)
    schedules.Run(t, f); webhooks.Run(t, f)
    // v0.8.0 — required
    handoffs.Run(t, f); teamstates.Run(t, f); agentinstances.Run(t, f)
}
```

User adapters call `contract.RunSuite(t, MyFactory)` and pass.
That's the contract.

## Multi-agent specific suite tests

`contract/handoffs/`:

- `TestHandoffStore_AppendOnlyIsAppendOnly`
- `TestHandoffStore_ListReturnsULIDOrder`
- `TestHandoffStore_SelectorByFromAndTo`
- `TestHandoffStore_PayloadJSONRoundTrip`

`contract/teamstates/`:

- `TestTeamStateStore_AtomicSnapshotOverwrite`
- `TestTeamStateStore_LoadReturnsLatestOrNotFound`
- `TestTeamStateStore_HighFrequencyWriteDoesNotBlockReads`

`contract/agentinstances/`:

- `TestAgentInstanceStore_UpsertOnDeterministicID`
- `TestAgentInstanceStore_ListByClassName`
- `TestAgentInstanceStore_SelectorByStatus`
- `TestAgentInstanceStore_LifecycleEventsMatchStoreStatus` (integration with `EventStore`)

## Resume integration tests

Beyond the per-store tests, the suite includes integration tests that
validate three-surface reconstruction (`11-boundaries.md` Principle 5):

`contract/integration/`:

- `TestResume_ReconstructsRunState` — Runner alone, no multi-agent
- `TestResume_ReconstructsStepTrace` — agent.Engine via EventStore
- `TestResume_ReconstructsSchedulerDecisions` — TeamStateStore +
  EventSchedulerTick
- `TestResume_AllThreeSurfacesAfterKill` — kill mid-Scheduler tick,
  restart, observe Run + Steps + Scheduler decisions all reconstructed
  independently

## Non-goals

- Shipping a Postgres / MySQL / SQLite adapter. Users own this.
- Shipping a "reference" backend beyond the framework-internal
  `inmemfake` used to self-check the suite.
- Performance benchmarks. Backend authors run those against their
  target deployment.

## Verification

- `go test ./contract/...` succeeds against `contract/internal/inmemfake`
- New tests listed in `contract/handoffs/`, `contract/teamstates/`,
  `contract/agentinstances/`, `contract/integration/` are present and
  pass
- `UnitOfWork` interface in `api/` has 18 store getters
- No production-quality `StoreProvider` ships outside `contract/internal/inmemfake`
- ADR-012 (revised) and ADR-017 cross-reference this document
