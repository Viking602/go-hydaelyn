# 12 — Migration Guide: v0.7 → v0.8

## Audience

Existing downstream users on `github.com/Viking602/go-hydaelyn v0.7.x` upgrading to v0.8.0. This guide is shipped as `docs/migration.md` alongside the release notes.

## Summary of what changes

| Area | Type | Action required |
|------|------|-----------------|
| `api.Flow` Bypass fields | Breaking removal | Delete the fields from any `Flow{}` literal that sets them |
| `api.ErrFlowBypass` | Breaking removal | Remove any error-equality check against it |
| `StartRunCommand` result | Breaking type change | Replace `[]any` triple-assertion with `api.StartRunResult` |
| `RequestApprovalCommand` result | Breaking type change | Replace `[]any` triple-assertion with `api.RequestApprovalResult` |
| `internal/memory` import | Path move (not breaking unless you forked) | Replace with `storage/memory` |
| New types in `api/` | Additive | No action required; consume as needed |
| `AgentProfile` fields | Additive | No action required; existing profiles still valid (empty `Status` = Active) |
| `AgentProfile.Status` + `PreviousVersionID` | Additive | Optional. Set `Status` only if you need lifecycle gating; set `PreviousVersionID` when registering a successor profile |
| `RunSelector` + `Run.AgentVersion` | Additive | Use `RunStore.ListRuns(api.RunSelector{AgentID: …})` to query run history by Agent |
| Reserved `hydaelyn.self.*` Capability names | Additive guard | If you happen to have registered a Capability whose Name starts with `hydaelyn.self.`, rename it — Registry will now reject it |
| `StoreProvider` interface | Additive (new methods) | If you have a custom provider: implement new methods; otherwise none |

## Breaking change 1 — Remove `Flow.Bypass*`

### Before (v0.7.x)

```go
runner.RegisterFlow(api.Flow{
    Name:                "researcher",
    BypassTaskStore:     false,
    BypassPolicyEngine:  false,
    BypassResponseLayer: false,
})
```

Even though every Bypass field was set to `false`, the fields are gone in v0.8.0.

### After (v0.8.0)

```go
runner.RegisterFlow(api.Flow{
    Name: "researcher",
})
```

### What if I had a Bypass field set to `true`?

You did not. The runtime rejected any such flow with `ErrFlowBypass` at registration time in v0.7.x — so no production code can have shipped a `true` value. If your code sets one to true, that path was already dead.

## Breaking change 2 — Remove `ErrFlowBypass`

### Before

```go
if err := runner.RegisterFlow(flow); err != nil {
    if errors.Is(err, api.ErrFlowBypass) {
        log.Fatal("flow attempted to bypass runtime invariants")
    }
    return err
}
```

### After

```go
if err := runner.RegisterFlow(flow); err != nil {
    return err
}
```

The error no longer exists because the condition it signaled cannot happen — there are no Bypass fields to set.

## Breaking change 3 — `StartRunCommand` result is typed

### Before

```go
res, err := runner.ExecuteCommand(ctx, api.StartRunCommand{
    Request: "Compare top three Go ORMs",
})
if err != nil { return err }

slice := res.([]any)         // assertion 1
run := slice[0].(api.Run)     // assertion 2
task := slice[1].(api.Task)   // assertion 3
```

### After

```go
res, err := runner.ExecuteCommand(ctx, api.StartRunCommand{
    Request: "Compare top three Go ORMs",
})
if err != nil { return err }

started := res.(api.StartRunResult) // single assertion
run := started.Run
task := started.RootTask
```

### Preferred (new in v0.8.0)

```go
started, err := runner.QueueRun(ctx, api.RunInput{
    Request: "Compare top three Go ORMs",
})
if err != nil { return err }

run := started.Run
task := started.RootTask
```

`QueueRun` is the typed entrypoint and is the recommended path.

## Breaking change 4 — `RequestApprovalCommand` result is typed

### Before

```go
res, err := runner.ExecuteCommand(ctx, api.RequestApprovalCommand{...})
slice := res.([]any)
approval := slice[0].(api.ApprovalRequest)
token := slice[1].(api.ResumeToken)
```

### After

```go
res, err := runner.ExecuteCommand(ctx, api.RequestApprovalCommand{...})
requested := res.(api.RequestApprovalResult)
approval := requested.Approval
token := requested.Token
```

Or, preferred:

```go
requested, err := runner.RequestApproval(ctx, ...)
```

## Path move — `internal/memory` → `storage/memory`

If you imported `github.com/Viking602/go-hydaelyn/internal/memory` (you should not have — it was under `internal/`), the new path is:

```go
import "github.com/Viking602/go-hydaelyn/storage/memory"
```

If you used the public `hydaelyn.New(...)` constructor with default settings, no change required — the constructor wires the new path internally.

## What you now have access to (additive)

These types are new in v0.8.0 and require no migration action. Consume them when you are ready:

### Persistent storage — pick a path

v0.8.0 takes **Position C** on storage: the framework owns the `StoreProvider` contract and ships reference implementations; production teams with existing data stacks are expected to bring their own provider. Two paths, choose by team context.

#### Path A — Use a reference implementation (fastest)

Good when: team has no strong DBA / ORM constraint, scale is moderate, schema being framework-owned is acceptable.

```go
import "github.com/Viking602/go-hydaelyn/storage/sqlite"

provider, err := sqlite.Open(ctx, "/var/lib/hydaelyn/state.db",
    sqlite.Options{AutoMigrate: true})
runner, _ := hydaelyn.New(ctx, api.Config{StoreProvider: provider, /* ... */})
```

For MySQL / OceanBase 4.x:

```go
import "github.com/Viking602/go-hydaelyn/storage/mysql"

// DSN works for stock MySQL, MariaDB, TiDB, or OceanBase 4.x MySQL mode.
// For OceanBase via OBProxy:
//   user@tenant#cluster:password@tcp(obproxy:2883)/hydaelyn?parseTime=true&loc=Local
provider, err := mysql.Open(ctx, dsn, mysql.Options{
    AutoMigrate: true, // or false + run mysql.MigrationsFS() through your tool
    TablePrefix: "hyd_",
})
```

OceanBase-specific guidance for the reference impl:

- `ConnMaxLifetime` 30 minutes for OBProxy routing.
- Reference schema avoids `JSON_TABLE` / `JSON_OVERLAPS` / `AUTO_INCREMENT` PKs.
- See `storage/mysql/oceanbase_compat.md` for the full matrix.

Postgres:

```go
import "github.com/Viking602/go-hydaelyn/storage/postgres"

provider, err := postgres.Open(ctx, "postgres://...", postgres.Options{AutoMigrate: true})
```

v0.8.0 Postgres reference impl does NOT include `LISTEN/NOTIFY`-based Subscribe — polling fallback only. Subscribe lands in v0.8.1.

#### Path B — Bring your own provider (recommended for production)

Good when: team uses ent / gorm / sqlc / custom ORM, has DBA-controlled DDL, runs production scale, wants framework upgrades not to touch DDL.

The framework's commitment: the `StoreProvider` contract is SemVer-stable, and `contract.RunStoreProviderContractTests` validates correctness. The team's commitment: implement the interfaces against your own schema and tooling.

A complete ent-based template, validated by the contract test suite, ships as `_examples/storage-ent-mysql/`. The shape:

```go
package entstorage

import (
    "context"
    "entgo.io/ent/dialect"
    entsql "entgo.io/ent/dialect/sql"

    "github.com/Viking602/go-hydaelyn/api"
    "your-app/ent"
)

// Provider wraps an ent client and satisfies api.StoreProvider.
// Schemas (ent/schema/HydRun.go, HydTask.go, ...) are defined in your project
// — owned by your team's ent generator and migration pipeline.
type Provider struct {
    client *ent.Client
    db     *entsql.Driver
}

func New(client *ent.Client) *Provider { return &Provider{client: client} }

func (p *Provider) Begin(ctx context.Context) (api.UnitOfWork, error) {
    tx, err := p.client.Tx(ctx)
    if err != nil { return nil, err }
    return &uow{tx: tx}, nil
}

func (p *Provider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
    return api.StoreCapabilities{
        SupportsTransactions:        true,
        SupportsBlackboardSubscribe: false, // unless you implement push via NOTIFY / Redis
        SupportsListPending:         true,
        SupportsConcurrentWriters:   true,
        SupportsDeadLetterRequeue:   true,
    }, nil
}

func (p *Provider) Close(ctx context.Context) error { return p.client.Close() }

type uow struct{ tx *ent.Tx }

func (u *uow) Commit() error   { return u.tx.Commit() }
func (u *uow) Rollback() error { return u.tx.Rollback() }

func (u *uow) Runs() api.RunStore     { return &runStore{tx: u.tx} }
func (u *uow) Tasks() api.TaskStore   { return &taskStore{tx: u.tx} }
func (u *uow) Events() api.EventStore { return &eventStore{tx: u.tx} }
// ... 12 more *Store implementations
```

Each `*Store` is a thin adapter from `api.Foo` types to your ent client's CRUD. The lease CAS is the only delicate one — typical implementation:

```go
func (s *leaseStore) AcquireWithExpectedVersion(
    ctx context.Context, lease api.TaskExecutionLease, expectedVersion uint64,
) (bool, error) {
    affected, err := s.tx.HydLease.
        Update().
        Where(hydlease.ID(lease.ID), hydlease.Version(expectedVersion)).
        SetVersion(lease.Version).
        SetHolder(lease.Holder).
        SetExpiry(lease.Expiry).
        Save(ctx)
    if err != nil { return false, err }
    return affected == 1, nil // CAS success = exactly one row updated
}
```

Validation: in your test suite, run the contract test suite against your provider:

```go
func TestEntProvider_ContractCompliance(t *testing.T) {
    contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
        client := newTestEntClient(t) // your test DB setup
        provider := entstorage.New(client)
        cleanup := func() { provider.Close(context.Background()) }
        return provider, cleanup
    })
}
```

If this passes, the provider is correct.

**Connection sharing with your app:**

```go
// Single *sql.DB for the whole process
db, _ := sql.Open("mysql", dsn)
db.SetMaxOpenConns(50)
db.SetConnMaxLifetime(30 * time.Minute) // OBProxy friendly

// ent uses it for business tables
drv := entsql.OpenDB(dialect.MySQL, db)
entClient := ent.NewClient(ent.Driver(drv))
entClient.Schema.Create(ctx) // ent manages its own + your hyd_* tables

// Hydaelyn uses ent for framework tables via your provider
provider := entstorage.New(entClient)
runner, _ := hydaelyn.New(ctx, api.Config{StoreProvider: provider})
```

**Transaction discipline:**

- Do NOT nest Hydaelyn UoW inside ent.Tx (or vice versa) — they share the connection pool but each opens its own transaction. Nesting risks deadlocks and connection exhaustion.
- For "business action + queue a run" atomicity, use the Outbox pattern (`hyd_outbox` row written in your business transaction; Hydaelyn's outbox scanner picks it up).

**Schema evolution:**

- Your ent schema (with the `hyd_*` table definitions) is owned by your team. `ent generate` + `entClient.Schema.Create()` or Atlas runs your migrations on your normal CI/CD flow.
- When Hydaelyn adds new columns to the `StoreProvider` contract (e.g., `Run.AgentVersion`), you update your ent schema to match. The contract is documented in `api/store.go`; columns map 1:1. `Memory[T]` is **not** part of `StoreProvider` — under ADR-013 it is an optional plugin owned entirely by the application, so the migration guidance here does not apply to it.
- The framework will not run any DDL against your DB. Ever.

#### Decision rule

If you read both paths and aren't sure, default to **Path A** for v0.8.0 and migrate to Path B in v0.8.1 or v0.9.0 if any of these become true: scale problems, DBA pushback, schema customization needs, ORM hook integration needs, multi-region routing.

### Capability declaration

```go
cap := myTool.AsCapability()
manifest := api.CapabilityManifest{
    Name:         "my-service",
    Version:      "1.0.0",
    Capabilities: []api.Capability{cap},
}
```

### Worker Runtime

```go
import "github.com/Viking602/go-hydaelyn/worker"

rt := worker.NewRuntime(worker.Config{
    Runner:            runner,
    WorkerID:          "worker-1",
    Engine:            agentEngine,
    Concurrency:       4,
    HeartbeatInterval: 30 * time.Second,
})

go rt.Run(ctx) // blocks until ctx done; graceful shutdown
```

### Memory and Artifact

```go
// Memory is an optional plugin. The framework ships no reference
// implementation. If your application uses long-term memory, implement
// api.Memory[YourEntry] against your existing database/ORM and pass it
// into the runtime via api.Config. If not, simply omit it — the runtime
// does not require Memory to be configured.
```

### Usage metering and Budget

`UsageRecord` writes happen automatically. To consume them:

```go
records, err := store.UsageStore().Query(ctx, api.UsageSelector{
    RunID: runID,
})
total, _ := store.UsageStore().SumUsageCredits(ctx, api.UsageSelector{RunID: runID})
```

To bound a run:

```go
profile.Governance = api.GovernancePolicy{
    MaxCreditsPerRun: 10_000,
    MaxRuntime:       30 * time.Minute,
}
```

### Triggers

To run a Profile on a schedule:

```go
profile.Triggers = []api.Trigger{{
    Type:   api.TriggerSchedule,
    Source: "0 9 * * 1-5", // cron: weekday 9am
}}
```

Wire `transport/scheduler` into the runner and the trigger fires `RunFromProfile` automatically.

### Eval framework

```go
import "github.com/Viking602/go-hydaelyn/eval"

cases := []eval.EvalCase{...}
eval.AssertSuitePasses(t, ctx, runner, cases)
```

## Custom `StoreProvider` implementers (rare)

If you wrote a custom `api.StoreProvider` (you almost certainly didn't — most users use the built-in memory/sqlite/postgres providers), v0.8.0 extends the interface:

```go
type StoreProvider interface {
    // existing methods unchanged

    // new in v0.8.0:
    AgentProfileStore() AgentProfileStore
    CapabilityStore()   CapabilityStore
    UsageStore()        UsageStore
    DeadLetterStore()   DeadLetterStore
    Capabilities()      StoreCapabilities
}

type LeaseStore interface {
    // existing methods unchanged

    // new in v0.8.0:
    AcquireWithExpectedVersion(ctx context.Context, taskID string, expectedVersion uint64, ...) (TaskExecutionLease, error)
    ExtendLease(ctx context.Context, leaseID string, expectedVersion uint64, until time.Time) (TaskExecutionLease, error)
}

type ResumeTokenStore interface {
    // existing methods unchanged

    // new in v0.8.0:
    ListPending(ctx context.Context, sel ResumeTokenSelector) ([]ResumeToken, error)
}
```

The contract test suite (`contract.RunStoreProviderContractTests`) verifies every adapter passes the same 30+ tests. We strongly recommend running it against your custom adapter before upgrading production traffic:

```go
func TestMyCustomProvider(t *testing.T) {
    contract.RunStoreProviderContractTests(t, func() api.StoreProvider {
        return mycustom.NewProvider(...)
    })
}
```

## Recommended upgrade sequence

1. **Compile against v0.8.0-alpha** (Phase 1 release). Fix the four breaking changes mechanically. Build green.
2. **Run existing tests.** They should pass without changes if you used recommended APIs.
3. **(Optional) Add a storage adapter.** Pick SQLite, MySQL (incl. OceanBase 4.x), or Postgres. The memory provider continues to work for tests.
4. **(Optional) Add UsageRecord observability.** Dashboards consume `UsageStore().Query`.
5. **(Optional) Add Budget enforcement.** Set `Governance.MaxCreditsPerRun` per profile.
6. **(Optional) Add Triggers.** Schedule / webhook / event-driven runs replace your custom orchestration.
7. **(Optional) Add Eval suite.** Convert ad-hoc test scripts to `EvalCase` form.

Each step after #2 is non-breaking and can be adopted independently.

## Estimated effort

- **Compile fix** (steps 1-2 above): 30 minutes for a typical downstream codebase. Four breaking changes, all mechanical, supported by `gofix`-friendly patterns.
- **Storage adapter migration**: 1-2 days, mostly schema validation and dual-write rollout if zero downtime is required.
- **Optional adoption (Budget / Triggers / Eval)**: per feature, scoped to user's needs.

## Support

If you hit an unexpected breakage during migration:

- Open an issue with the minimal reproducer
- Tag `v0.8.0-migration` in the issue title
- Include the Go version and v0.7.x version you upgraded from

The release/v0.8.0 branch is open for fixes throughout Phase 5 (`docs/product-spec/11-rollout-plan.md`); migration-blocking bugs are fixed before the final tag.
