# ADR-012 Storage Contract and Position C — the framework owns the contract, not production storage

## Status

Accepted — enforced from the v0.8.0 roadmap onward. Anchor documents: `docs/product-spec/v0.8.0/05-storage.md`, `docs/product-spec/v0.8.0/09-boundaries.md` line 117 (Principle 4/5 → ADR-012 mapping), `docs/product-spec/v0.8.0/12-migration-guide.md` (Layer 3 ent-based provider template).

**Revised 2026-05-24:** Superseded by Position D — see the *Revised decision* section at the bottom of this ADR. Layer 2 (framework-shipped reference implementations) is withdrawn; the framework now ships the contract only. The Layer 1 + Layer 3 split remains intact.

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

### Layer 2 — Reference implementations (WITHDRAWN — see Revised decision)

This section described `storage/{memory,sqlite,mysql,postgres}` as framework-shipped reference implementations under the rule "passes contract tests, exists as runnable reference." Under the revised decision (Position D, see below) these implementations have been deleted from the repository. The framework no longer ships any `api.StoreProvider` implementation. The contract test suite continues to be exercised in framework CI via a non-exported in-memory adapter at `contract/internal/inmemfake/`, which is structurally unreachable from user code and is NOT a reference implementation.

### Layer 3 — User-owned production providers (recommended production path)

Downstream teams implement `StoreProvider` against their own data stack (ent / gorm / sqlc / DBA-controlled DDL / sharding / multi-region) and validate with `contract.RunStoreProviderContractTests`.

- **The user owns the schema entirely.** Table layout, indexes, migrations, DBA review, CI/CD rollout — all via the team's existing process. The framework never touches DDL.
- **Connection pool is shareable.** The team's `*sql.DB` (or `pgxpool`, or ent client) is used by both ent and the Hydaelyn provider; transactional consistency is wrapped by the team's UoW.
- **Version upgrades decouple.** A Hydaelyn v0.8.x → v0.9.x upgrade touches Go code only; storage evolves independently.

`docs/product-spec/v0.8.0/12-migration-guide.md` provides a complete ent-based provider template (~80 lines) as a copy-and-modify starting point.

## Immediately-effective hard constraints

1. **05-storage.md position statement is normative text**:
   > The framework owns the contract and the contract test suite. The framework does not own — and does not ship — any `api.StoreProvider` implementation.
2. **No framework-shipped `api.StoreProvider` implementation.** The `storage/` directory does not exist in the repository. Past references to `storage/sqlite | storage/mysql | storage/postgres | storage/memory` in documentation are historical only.
3. **The production path is Layer 3.** Applications implement `api.StoreProvider` against their own data stack. The ent-based template in `docs/product-spec/v0.8.0/12-migration-guide.md` is the canonical starting point.
4. **CI matrix has exactly one semantic.** The framework runs `contract.RunStoreProviderContractTests` against the non-exported `contract/internal/inmemfake` adapter on every PR. The matrix does not chase operational scenarios such as "large transactions on OB 4.x", "OBProxy routing under failover", or "cross-region writes" — those belong to whoever ships an implementation.
5. **Contract tests are the framework's product surface.** Any change that would weaken `contract.RunStoreProviderContractTests` requires an explicit ADR amendment.

## Revised decision (2026-05-24) — Position D

The principle recorded in `13-memory-optional-plugin.md` applies to `api.StoreProvider` too: the framework owns the verbs and invariants; the application owns the nouns, the schema, and the storage. The asymmetry between Memory (no reference impl) and StoreProvider (ref impls retained) is rejected.

Position D in one sentence:

> The framework ships the contract (`api.StoreProvider`, `contract.RunStoreProviderContractTests`) and nothing else. There are no framework-shipped StoreProvider implementations. Applications implement the contract against their own data stack and validate with the contract test suite.

Why C did not survive: "starting point for forking" was read as "production answer regardless of README wording," and the framework ended up implicitly responsible for operational properties of every fork. Position D removes the ambiguity at the only place where it could exist — the existence of an in-tree implementation.

The contract test suite is the framework's load-bearing test artifact and continues to run on every PR via `contract/internal/inmemfake`. The adapter wraps the existing `internal/memory.Provider` (which remains as the runtime's internal default), lives under Go's `internal/` to be structurally unreachable from user code, and exists for one reason: to prevent suite rot. It is not a backend.

## Impact

- **Phase 2 duration drops from 17-20+3 days to 15 days.** Time previously budgeted for polishing MySQL/Postgres production edge cases is redirected into contract test coverage and the ent template.
- **All OB-specific risks in the risk register are downgraded.** From "framework must reach production stability on OB 4.x" to "reference impl passes CI".
- **Any PR review may cite this ADR** as the basis for rejecting language that positions mysql/postgres as the production answer, rejecting schema changes that bleed into framework releases, and rejecting workload-specific optimization of reference impls.
- **Downstream expectation management.** v0.8.0 release notes and the top of the README state that the production path is Layer 3. Under the original Position C, "I ran storage/mysql in production and hit X" issues were routed to the Layer 3 on-ramp. Under the revised Position D no such issue can arise — `storage/` does not ship, so the routing rule is moot.

## References

- Design: `docs/product-spec/v0.8.0/05-storage.md` (full Layer 1/2/3 specification)
- Boundaries: `docs/product-spec/v0.8.0/09-boundaries.md` Principle 4/5 → ADR-012 mapping
- Migration: `docs/product-spec/v0.8.0/12-migration-guide.md` (Path A reference / Path B BYO + ent template + decision rule)
- README: `docs/product-spec/v0.8.0/README.md` Versioning Note, Storage stance paragraph
- Rollout: `docs/product-spec/v0.8.0/11-rollout-plan.md` Phase 2 restructure
- Related ADRs: ADR-008 (framework vs business boundary), ADR-013 (Memory kernel vs pipeline)
