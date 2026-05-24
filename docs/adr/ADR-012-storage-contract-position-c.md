# ADR-012 Storage Contract and Position C — the framework owns the contract, not production storage

## Status

Accepted — enforced from the v0.8.0 roadmap onward. Anchor documents: `docs/product-spec/v0.8.0/05-storage.md`, `docs/product-spec/v0.8.0/09-boundaries.md` line 117 (Principle 4/5 → ADR-012 mapping), `docs/product-spec/v0.8.0/12-migration-guide.md` (Layer 3 ent-based provider template).

## Context

Before v0.7.x Hydaelyn shipped only one in-process storage implementation under `internal/memory`. During v0.8.0 design the project briefly committed to an "official production storage" path — MySQL (including OceanBase 4.x / TiDB) and Postgres adapters as the production-recommended choice, with the framework owning DDL, schema evolution, migration-tool integration, and ORM coexistence policies.

That path is not sustainable for these reasons:

1. **DDL ownership conflict.** Downstream teams typically use ent / gorm / sqlc / DBA-controlled DDL. A framework-owned schema collides hard with each team's ORM, migration tooling, CI pipeline, and DBA review process.
2. **Dialect trap.** MySQL 8.0, MariaDB, OceanBase 4.x, and TiDB each carry their own semantics (`AUTO_INCREMENT` monotonicity, `JSON_TABLE` availability, `SKIP LOCKED` support, proxy stickiness, timeout knobs). Maintaining a long-lived compatibility matrix is a DBaaS vendor's job, not an agent runtime's.
3. **Asymmetric evolution.** If framework releases carry schema changes, downstream teams must either stop the world or run framework-bundled migration tools in production — neither is acceptable.
4. **Ambiguous "reference vs production" promise.** Branding `storage/mysql/` as both a reference impl and a production answer made the framework implicitly responsible for each downstream's operations.

The project needed an architectural stance that does not depend on the framework operating external databases on behalf of users.

## Decision

Adopt **Position C — a three-layer structure**:

### Layer 1 — Contract (mandatory, framework-owned)

- `api.StoreProvider` and all sub-store interfaces (`RunStore`, `TaskStore`, `EventStore`, `LeaseStore`, `UserMessageStore`, `ResumeTokenStore`, `AgentProfileStore`, `CapabilityStore`, `UsageStore`, `DeadLetterStore`, ...).
- `api.StoreCapabilities` self-declared feature bits (`SupportsTransactions`, `SupportsBlackboardSubscribe`, `SupportsListPending`, `SupportsConcurrentWriters`, `SupportsDeadLetterRequeue`). The runtime branches on flags instead of probing the provider.
- `contract.RunStoreProviderContractTests` — the public contract test suite, ~35 cases (15 CRUD + 4 transactions + 5 lease CAS + 3 event ordering + 5 resume tokens / outbox + 2 replay + 1 capability self-consistency).
- Non-negotiable invariants enforced at the contract layer:
  - **Lease CAS:** `AcquireWithExpectedVersion` must be atomic; with N=10 concurrent acquirers, exactly one wins.
  - **Event ordering:** `Append` assigns a strictly monotonic `Sequence` per RunID; replay determinism depends on this.
  - **Outbox FIFO:** messages queued for the same recipient/run appear strictly in queue order.

Layer 1 follows SemVer after v1.0.0. Optional new store interfaces may be added (declared via new `StoreCapabilities` flags), but no existing interface is removed or break-changed.

### Layer 2 — Reference implementations (framework-shipped convenience)

- `storage/memory` — in-process, clone-on-Begin, replace-on-Commit. Reports `SupportsConcurrentWriters: false`. Use for tests / examples / ephemeral demos.
- `storage/sqlite` — `modernc.org/sqlite` pure-Go driver, `BEGIN IMMEDIATE`, single-node persistent demo.
- `storage/mysql` — `go-sql-driver/mysql`; CI matrix covers MySQL 8.0 + MariaDB 10.5+ + OceanBase 4.x community edition + TiDB 6+; `SELECT ... FOR UPDATE SKIP LOCKED` provides lease fairness.
- `storage/postgres` — `pgx/v5`; `UPDATE ... WHERE version = ? RETURNING`. **`LISTEN/NOTIFY`-based Subscribe is deferred to v0.8.1**; v0.8.0 reports `SupportsBlackboardSubscribe = false` and uses polling.

**The only maintenance promise for Layer 2** is "passes `contract.RunStoreProviderContractTests`". The framework does NOT commit to:

- Compatibility with every MySQL fork's edge-case semantics
- Tuning for any specific workload profile
- Schema or index optimization for any specific query pattern
- Integration with arbitrary migration tools (the references expose `embed.FS` and an optional `Options{AutoMigrate: true}` — that is the deal)

Documentation, godoc, README, and release notes for `storage/{sqlite,mysql,postgres}` use the phrase **"starting point for teams forking it"** consistently. This is load-bearing language.

### Layer 3 — User-owned production providers (recommended production path)

Downstream teams implement `StoreProvider` against their own data stack (ent / gorm / sqlc / DBA-controlled DDL / sharding / multi-region) and validate with `contract.RunStoreProviderContractTests`.

- **The user owns the schema entirely.** Table layout, indexes, migrations, DBA review, CI/CD rollout — all via the team's existing process. The framework never touches DDL.
- **Connection pool is shareable.** The team's `*sql.DB` (or `pgxpool`, or ent client) is used by both ent and the Hydaelyn provider; transactional consistency is wrapped by the team's UoW.
- **Version upgrades decouple.** A Hydaelyn v0.8.x → v0.9.x upgrade touches Go code only; storage evolves independently.

`docs/product-spec/v0.8.0/12-migration-guide.md` provides a complete ent-based provider template (~80 lines) as a copy-and-modify starting point.

## Immediately-effective hard constraints

1. **05-storage.md line 9 position statement is normative text**:
   > The framework owns the contract. The framework does not own production storage operations.
2. **`storage/{mysql,postgres}` README, godoc, and release notes MUST NOT use the words "production", "official", or "recommended for production"**. The consistent phrasing is "reference implementation / starting point for forking".
3. **The official production recommendation is Layer 3.** Documentation and examples must direct downstream teams to "start by copying the ent template in 12-migration-guide.md" as the default choice.
4. **CI matrix has exactly one semantic.** Reference impls pass `contract.RunStoreProviderContractTests`. The matrix does not chase operational scenarios such as "large transactions on OB 4.x", "OBProxy routing under failover", or "cross-region writes".
5. **Contract tests are written before reference implementations.** The first action of v0.8.0 Phase 2 is to land the named test list in `contract/store_provider.go` (bodies may be `t.Skip("TODO")`). Reference impls are written *to make a specific contract test green*, never the reverse.

## In-scope exceptions

- **A downstream team may fork `storage/mysql`** as a production template rather than adopt Layer 3. The framework does not block this, but the README carries no endorsement and operational responsibility belongs entirely to the downstream.
- **The OceanBase 4.x compatibility matrix** stays in `storage/mysql/oceanbase_compat.md` as forker-facing notes, not as a framework compatibility commitment.

## Impact

- **Phase 2 duration drops from 17-20+3 days to 15 days.** Time previously budgeted for polishing MySQL/Postgres production edge cases is redirected into contract test coverage and the ent template.
- **All OB-specific risks in the risk register are downgraded.** From "framework must reach production stability on OB 4.x" to "reference impl passes CI".
- **Any PR review may cite this ADR** as the basis for rejecting language that positions mysql/postgres as the production answer, rejecting schema changes that bleed into framework releases, and rejecting workload-specific optimization of reference impls.
- **Downstream expectation management.** v0.8.0 release notes and the top of the README state that the production path is Layer 3; any "I ran storage/mysql in production and hit X" issue is routed via the issue template to the Layer 3 on-ramp.

## References

- Design: `docs/product-spec/v0.8.0/05-storage.md` (full Layer 1/2/3 specification)
- Boundaries: `docs/product-spec/v0.8.0/09-boundaries.md` Principle 4/5 → ADR-012 mapping
- Migration: `docs/product-spec/v0.8.0/12-migration-guide.md` (Path A reference / Path B BYO + ent template + decision rule)
- README: `docs/product-spec/v0.8.0/README.md` Versioning Note, Storage stance paragraph
- Rollout: `docs/product-spec/v0.8.0/11-rollout-plan.md` Phase 2 restructure
- Related ADRs: ADR-008 (framework vs business boundary), ADR-013 (Memory kernel vs pipeline)
