# 05 — Storage Contract and Reference Implementations

## Goal

Make `api.StoreProvider` and the contract test suite the **primary public surface** for durable storage in Hydaelyn. Ship a small number of **reference implementations** (memory, sqlite, mysql, postgres) as dev convenience and as executable documentation of the contract. Production teams with existing data stacks (ent / gorm / sqlc, DBA-controlled DDL, sharding, multi-region) are expected to implement their own `StoreProvider` against their stack — and the contract test suite is what validates that they did it correctly.

## Position

> **The framework owns the contract. The framework does not own production storage operations.**

This is the architectural shift from earlier v0.8.0 drafts that positioned MySQL / Postgres adapters as "the official production storage." That framing forced the framework to absorb DDL ownership, schema-evolution discipline, migration-tool integration, ORM-coexistence policies, and DBA-workflow shimming — none of which a general-purpose agent runtime can credibly do well across every downstream team's infrastructure.

Position C (see design discussion in `docs/architecture-boundaries.md` and ADR-012):

- **Layer 1 — Contract (required, framework-owned)**: `api.StoreProvider`, all store sub-interfaces, the `contract/` test suite. SemVer-stable post v1.0.0.
- **Layer 2 — Reference implementations (framework-shipped convenience)**: `storage/memory` (dev / test), `storage/sqlite` (single-node), `storage/mysql` (multi-node reference), `storage/postgres` (multi-node reference). Maintained to a "passes contract tests" bar, not a "production hardened for every environment" bar.
- **Layer 3 — User-owned production providers**: downstream teams write their own `StoreProvider` using their existing ORM / data layer / migration tools. Validate with `contract.RunStoreProviderContractTests`.

This document specifies Layer 1 in detail, sketches Layer 2 lightly, and gives Layer 3 a clear on-ramp.

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

- **Schema shape**. The framework defines stores by Go interface, not by table layout. A provider can use one table per store, one wide table, document storage, KV, anything. The reference implementations happen to use the table layout described in Layer 2 — that is one valid implementation, not the contract.
- **ID format**. Use UUID, ULID, snowflake, integer, whatever. The framework treats all IDs as opaque strings.
- **Connection management**. `*sql.DB`, `pgxpool`, in-memory map, gRPC client to a remote service — all valid.
- **Migration tooling**. Providers handle their own schema setup. The framework calls `Begin` and expects it to work.
- **Backup / restore / replication**. Out of scope; that's the deployment's concern.

## Layer 2 — Reference implementations

Four reference implementations ship in `storage/`. Each is maintained to one bar: **passes the contract test suite**. Beyond that, they aim for clarity and small surface, not enterprise-grade hardening.

### Selection guidance for users

| Implementation | Intended use | Production-ready? |
|---|---|---|
| `storage/memory` | Tests, examples, ephemeral demos | No — process-local, lost on restart |
| `storage/sqlite` | Single-node deployments, local-first apps, demos with persistence | Yes for single-node scale; not for multi-process writers |
| `storage/mysql` | Reference for multi-node SQL deployments; starting point for teams forking it | Conditional — fine for moderate scale; teams with strong DBA / ORM constraints should treat it as a starting template, not the production answer |
| `storage/postgres` | Reference for multi-node SQL deployments; starting point for teams forking it | Same as mysql |

The phrase **"starting point for teams forking it"** is the load-bearing one. The framework's commitment to `storage/mysql` is "passes contract tests, exists as runnable reference". The framework does NOT commit to:

- Compatibility with every MySQL fork's idiosyncrasies beyond the baseline (MySQL 8.0, OceanBase 4.x, TiDB) tracked in CI
- Tuning for every workload profile
- Schema layouts optimized for any particular team's query patterns
- Integration with arbitrary migration tools (the reference ships embed.FS SQL files; that's the deal)

Teams whose answer to "is this acceptable" is "no" are expected to write their own provider — see Layer 3.

### Reference: memory

`storage/memory/` (moved from `internal/memory/`).

Single-process, clone-on-Begin, replace-on-Commit. Reports `SupportsConcurrentWriters: false`.

Use for: tests, examples, `_examples/`, eval suites, anything that does not need persistence.

### Reference: sqlite

`storage/sqlite/`.

- Driver: `modernc.org/sqlite` (pure Go, no CGO) by default; `mattn/go-sqlite3` available via build tag.
- Schema: `storage/sqlite/schema.sql` + numbered migrations in `storage/sqlite/migrations/`.
- Transactions: `BEGIN IMMEDIATE` per UoW.
- Lease CAS: `UPDATE ... WHERE version = ?`, check affected rows.
- Subscribe: not implemented; polling fallback.
- AutoMigrate: opt-in via `sqlite.Options{AutoMigrate: true}`. SQL files are also exposed via `sqlite.MigrationsFS()` for users who want to manage migrations externally.

Use for: single-node apps, local-first deployments, examples that need durability.

### Reference: mysql

`storage/mysql/`.

- Driver: `github.com/go-sql-driver/mysql`.
- Targets tracked in CI: MySQL 8.0, MariaDB 10.5+, OceanBase 4.x MySQL mode, TiDB 6+.
- Schema: `storage/mysql/schema.sql` + numbered migrations. Uses InnoDB, `utf8mb4`, `CHAR(36)` UUIDs, native `JSON`, `TIMESTAMP(6)`. Avoids `JSON_TABLE` / `JSON_OVERLAPS` / `AUTO_INCREMENT` PKs.
- Lease fairness: `SELECT ... FOR UPDATE SKIP LOCKED`.
- Subscribe: not implemented (MySQL lacks a pub/sub primitive); polling fallback.
- AutoMigrate / external migration: same pattern as sqlite — `Options{AutoMigrate: true}` for convenience or `mysql.MigrationsFS()` for external tooling.

OceanBase 4.x specifics tracked in `storage/mysql/oceanbase_compat.md`:

| Concern | Behavior | Mitigation |
|---|---|---|
| `SELECT ... FOR UPDATE SKIP LOCKED` | Supported in OB 4.x | Used for envelope polling |
| `ob_query_timeout` (default 10s) | Statement-level cap | Keep UoW short |
| `ob_trx_timeout` (default 100s) | Transaction cap | Heartbeat extends lease, not the SQL txn |
| `AUTO_INCREMENT` non-monotonic | Distributed seq | App-generated UUIDs |
| OBProxy routing | Long conns stick | `ConnMaxLifetime` 30 min |
| `JSON_TABLE` / `JSON_OVERLAPS` | Partial / unavailable | Decode JSON in Go |

This matrix is documentation, not a commitment to fix every OB-specific issue in the framework — it tells forkers what to be aware of when they take the reference implementation into their own production.

### Reference: postgres

`storage/postgres/`.

- Driver: `pgx/v5`.
- Schema: `storage/postgres/schema.sql` + migrations. Uses `uuid`, `jsonb`, `timestamptz`.
- Lease CAS: `UPDATE ... WHERE version = ?` with `RETURNING`.
- Subscribe: **deferred to v0.8.1** (LISTEN/NOTIFY). v0.8.0 reports `SupportsBlackboardSubscribe = false` and uses polling.
- AutoMigrate / external migration: same pattern; `postgres.MigrationsFS()`.

## Layer 3 — Bring your own provider

This is the **recommended path** for any team with existing data infrastructure (ent / gorm / sqlc / custom ORM / DBA-controlled schema / sharding / multi-region replicas).

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

A complete worked example using ent against MySQL / OceanBase is shipped in `docs/migration.md` (doc 12) — copy, modify, run, ship.

### What you trade

- **You write more code upfront** (~500-1000 lines for a complete provider).
- **You own schema evolution** — your ORM's migration tool handles it, your DBA reviews changes, your CI/CD ships them. Hydaelyn never touches your DDL.
- **You can shard / partition / replicate** any way you like.
- **You get to use your existing ORM hooks, audit logs, metrics, connection pool, tracing.**
- **Framework version upgrades touch only Go code** — your storage layer is independent.

### What the framework guarantees in return

- The contract test suite is the same gate the framework's own reference implementations pass. Same correctness bar.
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

All four reference implementations (memory, sqlite, mysql, postgres) MUST pass the full suite under CI. External providers MUST pass it before any team should treat them as production-ready.

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

## Migration of moved packages

- `internal/memory/*` → `storage/memory/*`. Public API unchanged (memory was already exposed via `hydaelyn.New()` default).

## Verification

- Contract test suite passes for all four reference implementations on CI matrices:
  - memory: any OS, no service
  - sqlite: any OS, no service
  - mysql: stock MySQL 8.0 + OceanBase 4.x community edition containers
  - postgres: postgres 15+ container
- `contract.RunStoreProviderContractTests` is exported and documented, runnable by external adapter authors
- Doc 12 ships a complete ent-based custom provider example, validated by the contract test suite in CI
- `_examples/storage-custom/` ships a runnable demo of Layer 3 (bring your own provider)
