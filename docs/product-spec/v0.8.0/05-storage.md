# 05 — Storage Contract (Position D)

## Goal

Make `api.StoreProvider` and the contract test suite the **sole public surface** the framework ships for durable storage in Hydaelyn. Applications implement the contract against their own data stack (ent / gorm / sqlc / DBA-controlled DDL / sharding / multi-region) and validate the implementation with `contract.RunStoreProviderContractTests`. The framework ships no `api.StoreProvider` implementation — public or otherwise — beyond the non-exported in-memory adapter the framework's own contract self-test runs against.

## Position

> **The framework owns the contract and the contract test suite. The framework does not own — and does not ship — any `api.StoreProvider` implementation.**

This is the architectural shift from earlier v0.8.0 drafts that positioned MySQL / Postgres adapters as "the official production storage." That framing forced the framework to absorb DDL ownership, schema-evolution discipline, migration-tool integration, ORM-coexistence policies, and DBA-workflow shimming — none of which a general-purpose agent runtime can credibly do well across every downstream team's infrastructure.

A subsequent intermediate position (Position C, the original text of this document and ADR-012) attempted to keep "reference implementations" under `storage/{memory,sqlite,mysql,postgres}` as a "starting point for forking." That phrasing turned out to be load-bearing in one direction only — downstream teams read it as "production answer regardless of README wording," and the framework ended up implicitly responsible for operational properties of every fork.

Position D resolves both: the framework owns the contract, the application owns the implementation, and there is no in-tree implementation that could be mistaken for either.

- **Layer 1 — Contract (required, framework-owned)**: `api.StoreProvider`, all store sub-interfaces, the `contract/` test suite. SemVer-stable post v1.0.0.
- **Layer 2 — REMOVED.** Earlier drafts shipped reference implementations under `storage/{memory,sqlite,mysql,postgres}`. The 2026-05-24 revision to ADR-012 (Position D) deletes them. Applications implement Layer 1 against their own data stack — see the ent-based template in `12-migration-guide.md`.
- **Layer 3 — User-owned production providers**: downstream teams write their own `StoreProvider` using their existing ORM / data layer / migration tools. Validate with `contract.RunStoreProviderContractTests`. This is now the **only** production path.

This document specifies Layer 1 in detail and gives Layer 3 a clear on-ramp.

## Layer 1 — The StoreProvider contract

`api/store.go` defines the complete public surface. Every `StoreProvider` MUST satisfy this contract; the contract test suite (later in this doc) is the executable specification.

### Top-level provider

```go
type StoreProvider interface {
    Begin(ctx context.Context) (UnitOfWork, error)
    Capabilities(ctx context.Context) (StoreCapabilities, error)
    Close(ctx context.Context) error
}

type UnitOfWork interface {
    Commit() error
    Rollback() error

    Runs()           RunStore
    Tasks()          TaskStore
    Events()         EventStore
    TraceSpans()     TraceSpanStore
    Blackboard()     BlackboardStore
    Mailbox()        MailboxStore
    UserMessages()   UserMessageStore
    Leases()         LeaseStore
    Approvals()      ApprovalStore
    ResumeTokens()   ResumeTokenStore
    ActionAttempts() ActionAttemptStore
    AgentProfiles()  AgentProfileStore   // doc 03
    Capabilities()   CapabilityStore     // doc 03
    UsageRecords()   UsageStore          // doc 06
    DeadLetters()    DeadLetterStore     // doc 04
}
```

### Capability reporting

```go
// StoreCapabilities is the provider's self-declaration of optional features.
// The runtime branches on these flags rather than probing the provider.
type StoreCapabilities struct {
    SupportsTransactions        bool // UoW commit/rollback are atomic
    SupportsBlackboardSubscribe bool // can deliver Blackboard pushes without polling
    SupportsListPending         bool // ResumeTokens / Outbox enumeration
    SupportsConcurrentWriters   bool // safe under N>1 worker processes
    SupportsDeadLetterRequeue   bool // dead-lettered envelopes can be re-queued
}
```

A provider that returns `false` for an optional capability is **valid** — the runtime falls back to a polling / single-writer / non-requeue path. The capability flags exist so the framework never silently corrupts state when a provider chose not to implement an optional feature.

### Lease CAS — the critical contract

Every multi-writer correctness guarantee in Hydaelyn flows through `LeaseStore.AcquireWithExpectedVersion`. The contract here is non-negotiable:

```go
type LeaseStore interface {
    SaveLease(context.Context, TaskExecutionLease) error
    LoadLease(context.Context, string) (TaskExecutionLease, error)
    ActiveLeaseForTask(context.Context, string, string) (TaskExecutionLease, bool, error)

    // AcquireWithExpectedVersion atomically persists `lease` if and only if
    // the currently-stored lease for the same ID has version == expectedVersion.
    // Returns (true, nil) on successful acquire; (false, nil) on version
    // mismatch (another worker won the race). MUST be atomic — no observable
    // intermediate state where two acquires both think they succeeded.
    AcquireWithExpectedVersion(ctx context.Context, lease TaskExecutionLease, expectedVersion uint64) (bool, error)

    // ExtendLease atomically extends Expiry if and only if the current holder
    // equals workerID. Returns (false, nil) if the lease was lost.
    ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error)
}
```

`TaskExecutionLease` gains:

```go
type TaskExecutionLease struct {
    // ... existing fields
    Version uint64    `json:"version"` // monotonic, CAS source of truth
    Expiry  time.Time `json:"expiry"`  // auto-release after this
}
```

Implementation note for any provider author: the SQL idiom is `UPDATE leases SET version=version+1, ... WHERE id=? AND version=?` and check affected rows == 1. The Redis idiom is a Lua script with WATCH. The Cassandra idiom is `IF version = ?`. The point is the framework only sees the result of the CAS — the implementation can be anything that delivers atomic compare-and-swap.

### Event ordering — the replay contract

```go
type EventStore interface {
    Append(ctx context.Context, event Event) error
    ListByRun(ctx context.Context, runID string) ([]Event, error)
    ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]Event, error)
}
```

Contract requirements:

- `Append` MUST assign a strictly-monotonic `Event.Sequence` within a RunID.
- `ListByRun` MUST return events in `Sequence` order.
- `ListAfter(runID, N)` MUST return exactly the events with `Sequence > N`, in order.
- Sequencing is per-RunID, not global — providers do not need a global sequence.

Replay determinism depends on this. A provider that batches Appends and may reorder them within a run breaks replay.

### Outbox FIFO — the user-message contract

```go
type UserMessageStore interface {
    Queue(ctx context.Context, msg UserMessage) error
    ListQueued(ctx context.Context) ([]UserMessage, error)
    MarkSent(ctx context.Context, msgID string) error

    // ListPendingFor restricts to one recipient / run; FIFO within the
    // returned slice.
    ListPendingFor(ctx context.Context, sel UserMessageSelector) ([]UserMessage, error)
}
```

Contract: messages queued in order T1 < T2 MUST appear in the same order when scanned by the outbox. This is the at-least-once delivery contract honored by the runtime's outbox scanner.

### Resume token enumeration

```go
type ResumeTokenStore interface {
    SaveResumeToken(context.Context, ResumeToken) error
    LoadResumeToken(context.Context, string) (ResumeToken, error)
    ListPending(ctx context.Context, sel ResumeTokenSelector) ([]ResumeToken, error)
}
```

Pagination via `Cursor` is OPTIONAL — providers MAY return everything in one call. `ListPending` MUST exclude already-consumed tokens.

### New stores from docs 03, 04, 06

```go
type AgentProfileStore interface { /* CRUD + ListByAgentSelector */ }
type CapabilityStore   interface { /* CRUD + ListByCapabilitySelector */ }
type UsageStore        interface { /* Append-only + Query + SumCredits */ }
type DeadLetterStore   interface { /* Append + List + Requeue (optional) */ }
```

Detailed methods are in the respective specs (doc 03, doc 04, doc 06). Each is just CRUD with a selector — no exotic semantics, providers implement these with the same SQL idioms they use everywhere else.

### What the contract intentionally does NOT mandate

- **Schema shape.** The framework defines stores by Go interface, not by table layout. A provider can use one table per store, one wide table, document storage, KV, anything.
- **ID format.** Use UUID, ULID, snowflake, integer, whatever. The framework treats all IDs as opaque strings.
- **Connection management.** `*sql.DB`, `pgxpool`, in-memory map, gRPC client to a remote service — all valid.
- **Migration tooling.** Providers handle their own schema setup. The framework calls `Begin` and expects it to work.
- **Backup / restore / replication.** Out of scope; that's the deployment's concern.

## Layer 3 — Bring your own provider

This is the **only** production path. There is no Layer 2 fallback.

### The on-ramp

```go
package mystorage

type Provider struct {
    db *gorm.DB // or *ent.Client, or sqlx, or whatever
}

func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) { /* ... */ }
func (p *Provider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
    return api.StoreCapabilities{
        SupportsTransactions:        true,
        SupportsBlackboardSubscribe: false, // unless you implement push
        SupportsListPending:         true,
        SupportsConcurrentWriters:   true,
        SupportsDeadLetterRequeue:   true,
    }, nil
}
func (p *Provider) Close(ctx context.Context) error { /* close pool */ }
```

Implement each `*Store` interface against your data layer. **You own the schema entirely** — define tables / columns / indexes / migrations using your ORM's native tooling (ent's `Schema.Create`, Atlas, Flyway, your CI/CD pipeline, whatever).

### The validation gate

```go
func TestMyProvider(t *testing.T) {
    contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
        db := newTestDB(t) // your test setup
        provider := mystorage.New(db)
        cleanup := func() { provider.Close(context.Background()) }
        return provider, cleanup
    })
}
```

If this passes, the provider is correct as far as Hydaelyn is concerned. If it fails, the failure message names the specific contract clause violated.

The framework runs the same suite on every PR via a non-exported in-memory adapter at `contract/internal/inmemfake/` — same correctness bar, no special-cased framework path.

A complete worked example using ent against MySQL / OceanBase is shipped in `12-migration-guide.md` — copy, modify, run, ship.

### What you trade

- **You write more code upfront** (~500-1000 lines for a complete provider).
- **You own schema evolution** — your ORM's migration tool handles it, your DBA reviews changes, your CI/CD ships them. Hydaelyn never touches your DDL.
- **You can shard / partition / replicate** any way you like.
- **You get to use your existing ORM hooks, audit logs, metrics, connection pool, tracing.**
- **Framework version upgrades touch only Go code** — your storage layer is independent.

### What the framework guarantees in return

- The contract test suite is the same gate the framework runs in its own CI via `contract/internal/inmemfake`. Same correctness bar.
- The contract is SemVer-stable post v1.0.0. v0.8.0 → v1.0.0 may add OPTIONAL store interfaces (declared via new `StoreCapabilities` flags) but will not remove or break-change existing ones.
- New optional features arrive as additive capability flags. Your provider can return `false` and the runtime falls back gracefully.

## Contract test suite

`contract/` is a top-level package — public API, importable by external adapter authors.

```go
package contract

func RunStoreProviderContractTests(t *testing.T, factory ProviderFactory)

type ProviderFactory func(t *testing.T) (api.StoreProvider, func())
```

The suite covers:

### Basic CRUD (15 tests)

`TestSaveAndLoad_Run`, `TestSaveAndLoad_Task`, `TestAppendAndList_Events`, `TestSaveAndList_TraceSpans`, `TestWriteAndSelect_BlackboardItems`, `TestQueueAndLoad_UserMessage`, `TestQueueAndLoad_Envelope`, `TestSaveAndLoad_Lease`, `TestSaveAndLoad_Approval`, `TestSaveAndLoad_ResumeToken`, `TestSaveAndList_AgentProfiles`, `TestSaveAndList_Capabilities`, `TestSaveAndList_UsageRecords`, `TestSaveAndList_DeadLetters`, `TestSaveAndList_ActionAttempts`.

### Transaction semantics (4 tests)

`TestUnitOfWork_CommitPersistsAll`, `TestUnitOfWork_RollbackDiscardsAll`, `TestUnitOfWork_ReadOwnWrites`, `TestUnitOfWork_IsolatedFromConcurrentBegin`.

### Lease CAS (5 tests)

`TestLease_AcquireWithExpectedVersion_Succeeds`, `TestLease_AcquireWithExpectedVersion_FailsOnStaleVersion`, `TestLease_ExtendLease_HonorsWorkerID`, `TestLease_ExtendLease_RejectsAfterTransfer`, `TestLease_ConcurrentAcquireOnlyOneWins` (N=10 goroutines, exactly one wins).

### Event ordering (3 tests)

`TestEvents_AppendPreservesOrder`, `TestEvents_ListEventsByRunID_ReturnsInOrder`, `TestEvents_SequenceMonotonic`.

### Resume tokens + Outbox (5 tests)

`TestResumeToken_ListPending_ExcludesConsumed`, `TestResumeToken_ListPending_Pagination`, `TestMessageOutbox_ScanReturnsQueued`, `TestMessageOutbox_FIFO`, `TestMessageOutbox_UpdateRemovesFromQueue`.

### Replay determinism (2 tests)

`TestReplay_SameInputSameOutput`, `TestReplay_PartialReplay`.

### Capability self-consistency (1 test)

`TestCapabilities_AreSelfConsistent` — every optional feature exercised by the suite is gated by the corresponding `Capabilities()` flag; flipping the flag to `false` causes the test to skip rather than fail.

### Total: ~35 tests

External providers MUST pass the full suite before any team should treat them as production-ready. The framework's own CI runs the suite on every PR via `contract/internal/inmemfake` to prevent suite rot.

## RunSelector

Added in support of doc 03 ontology:

```go
type RunSelector struct {
    IDs          []string    `json:"ids,omitempty"`
    AgentID      string      `json:"agentId,omitempty"`
    AgentVersion string      `json:"agentVersion,omitempty"`
    Statuses     []RunStatus `json:"statuses,omitempty"`
    Since        time.Time   `json:"since,omitempty"`
    Until        time.Time   `json:"until,omitempty"`
    Limit        int         `json:"limit,omitempty"`
}

type RunStore interface {
    // ... existing methods
    ListRuns(ctx context.Context, sel RunSelector) ([]Run, error)
}
```

Contract requirement: `ListRuns` filters AND-combine all set fields. Pagination beyond `Limit` is provider's choice (cursor / offset / none).

## Verification

- `contract.RunStoreProviderContractTests` is exported and documented, runnable by external adapter authors.
- Framework CI runs the suite on every PR via the non-exported `contract/internal/inmemfake` adapter (`go test -race ./contract/...`).
- `12-migration-guide.md` ships a complete ent-based custom provider template, intended to be copied into the application repository and validated against `contract.RunStoreProviderContractTests` there.
