# 11 — Rollout Plan

## Goal

Sequence the v0.8.0 work into phases that build on each other without forcing rework. Identify integration points where the external downstream project needs to be involved.

## Why five phases

Many docs depend on others structurally:

- Storage adapters can't be written without finalized store interfaces.
- Worker Runtime needs Lease CAS, which lives in Storage.
- Registry needs Capability and AgentProfile types defined first.
- Trigger transport needs Registry.
- Eval needs Projection model stable, which needs Usage/Policy chains finalized.

Phases respect these dependencies.

## Phase 1 — Public API foundations (week 1)

**Goal**: ship the type definitions that all later phases reference.

**Work**:

- Doc 01 in full: `Bypass*` removal, `StartRunResult` + `RequestApprovalResult` exported, `ExecuteCommand` doc update, `scripts/check-public-any.sh`
- Doc 02 partial: `api.Capability` + `api.CapabilityManifest` defined, `Tool.AsCapability()` implemented, JSON round-trip tests (exports are Phase 4). Reserved `HydaelynSelfNamespace` constant + four `CapabilityNameSelf*` constants + `ErrCapabilityNameReserved` defined; `Registry.RegisterCapability` rejects reserved-prefix registrations.
- Doc 03 partial: `api.AgentProfile` extended with `Version`, `Instructions`, `Model`, `Capabilities`, `Triggers`, `Governance` fields. **Plus the ontology fields**: `Status` (with `AgentStatus` enum: Active/Draft/Paused/Retired) and `PreviousVersionID`. `ModelPolicy`, `Trigger`, `TriggerType`, `GovernancePolicy` defined. `RunSelector` defined (with `AgentID` + `AgentVersion` filters); `Run.AgentVersion` field added; `RunStore.ListRuns(sel)` defined. Registry interface + selectors defined. In-process Registry implementation. **Not yet wired** into the Trigger transports. Memory `self` token resolution implemented in Runner (doc 07 self-knowledge convention).
- Doc 06 partial: `UsageRecord`, `Budget`, `Quota` types defined. `UsageStore` interface defined. **Not yet wired** to provider/tool call sites.
- Doc 07 partial: `ContextScope`, `Identified`, `Memory[T]`, `MemorySelector`, `Artifact`, `ArtifactStore`, `ContextSource` (incl. `ContextSourceKnowledgeGraph` reserved). **No reference implementation ships for `Memory[T]`** — ADR-013 (revised) makes it an optional plugin owned by the application. Default implementations for `ArtifactStore` (`artifact/filesystem`, `artifact/inmem`) ship in Phase 3.

**Deliverable**: a v0.8.0-alpha that compiles, all new types in `api/` available, no behavior change for existing users beyond the 4 documented breaks.

**Integration with downstream project**: send the new `AgentProfile` + `Capability` + `ModelPolicy` shapes for review BEFORE finalizing this phase. Once they land, changing fields is breaking.

**Duration estimate**: 5 working days.

## Phase 2 — Storage contract + Execution layer (weeks 2-4)

**Goal**: lock the `StoreProvider` contract, ship the contract test suite as the primary public deliverable, ship reference implementations as convenience, deliver the durable polling Worker Runtime.

**Position**: doc 05 was first reframed around Position C (contract-first, reference implementations as starter / dev tooling, production-grade providers expected to be downstream-owned) and then further reduced on 2026-05-24 to Position D (no reference implementations at all — see ADR-012 revised). This phase's effort allocates accordingly: contract quality and contract tests are the deliverable; there is no reference-adapter work.

**Work**:

- Doc 05 contract foundations: extend `LeaseStore` with CAS, extend `ResumeTokenStore`, `UserMessageOutboxScanner`, add `StoreCapabilities`. Add `AgentProfileStore`, `CapabilityStore`, `UsageStore`, `DeadLetterStore`, `RunSelector + RunStore.ListRuns`. Update `UnitOfWork`. **Contract is locked at end of this phase.**
- Build `contract/` package + `RunStoreProviderContractTests` suite (~35 tests). This is the primary public deliverable of Phase 2.
- Build `contract/internal/inmemfake/` — a non-exported `api.StoreProvider` (wraps `internal/memory.Provider`) used solely by framework CI to self-test the contract suite. Structurally unreachable from user code via Go's `internal/` rule.
- Doc 04 in full: `worker.Runtime` with poll loop, lease, heartbeat, retry, dead-letter, graceful shutdown, concurrency pool, backoff strategy. `DeadLetterStore` and `DeadLetterSink` wired.
- CI: runs the contract test suite against the inmemfake adapter on every PR.

**Deliverable**: v0.8.0-beta where the contract is locked, the contract test suite is exported, the framework self-tests the suite via `contract/internal/inmemfake`, and the Worker Runtime survives process kill against the runtime's internal default store.

**Integration with downstream project**: external user (running OceanBase 4.x in MySQL mode) implements `api.StoreProvider` against their own ent stack and validates with `contract.RunStoreProviderContractTests`. Doc 12 ships the complete template. There is no framework-shipped MySQL/Postgres/SQLite reference to start from — and that absence is the point.

**Duration estimate**: 9 working days (down from Position C's 15).

| Sub-item | Days |
|---|---|
| Contract types + interfaces + `RunSelector` | 2 |
| Contract test suite (~35 tests) — the primary deliverable | 5 |
| `contract/internal/inmemfake` self-test adapter | 1 |
| Worker Runtime (doc 04 full) | 1 |
| **Total** | **9** |

The further shrink from Position C's 15 days reflects the elimination of all reference-implementation work: no sqlite, no mysql with the OceanBase compat matrix, no postgres. The contract surface is what the framework owns; the freed budget reinforces contract test quality, which is the actual long-term asset.

## Phase 3 — Governance + Context (weeks 5-6)

**Goal**: cost observable and bounded; obligation enforcement live.

**Work**:

- Doc 06 in full: wire `UsageRecord` write at every provider stream completion, every ToolInvocation, every ActionAttempt. `BudgetPolicy` implemented and composed into PolicyEngine chain. `EventUsageRecorded`, `EventBudgetExceeded`, `EventBudgetWarn` emitted.
- `policy/enforcer.go` + 6 built-in obligation implementations. Wired into Blackboard read path, Tool invocation result path, Trace export path, Handoff transfer path, User message path.
- Doc 07 in full: `Memory[T]` interface (already shipped in Phase 1; no further deliverable). `ArtifactStore` interface + `artifact/filesystem` + `artifact/inmem` implementations. `BlackboardItem.ArtifactRefs` semantics documented + resolver against `ArtifactStore`.
- ADR-010 (Usage/Budget composition) written.
- ADR-011 (Context four-layer model) written.

**Deliverable**: a v0.8.0-rc1 where agents can be cost-bounded and obligation-secured.

**Integration with downstream project**: external user reviews their existing policy and tool registration; possibly migrates to opt-in to `PolicyEnforcer`. UsageRecord write is automatic; their dashboards may want to consume it.

**Duration estimate**: 10 working days.

## Phase 4 — Interop surfaces (weeks 7-8)

**Goal**: declarative Capability and Profile data flows out to MCP, OpenAPI, CLI, LLM tool definitions, scheduler, webhook, event bus.

**Work**:

- Doc 02 exports: MCP descriptor render, OpenAPI document render (`transport/openapi/`), CLI subcommand generator, LLM `ToolDefinition` render in `provider/`.
- Doc 04 trigger runtimes: `transport/scheduler/` (cron via robfig/cron/v3), `transport/webhook/` (HTTP listener), `transport/event/` (in-process bus).
- Wire Registry into all four transport adapters: each reads Profiles + Capabilities from the configured Registry and dispatches via Runner.
- ADR-009 (Capability public API) written.

**Deliverable**: a v0.8.0-rc2 where an Agent can be exposed via MCP, OpenAPI, CLI, or triggered on schedule/webhook/event without any custom integration code.

**Integration with downstream project**: external user picks one or two interop paths to validate end-to-end. Concrete demo: "register a Profile with a webhook trigger, hit the webhook, observe a Run starts and completes."

**Duration estimate**: 10 working days.

## Phase 5 — Verification, docs, release (week 9)

**Goal**: ship-ready v0.8.0.

**Work**:

- Doc 08 in full: `eval/` package, built-in assertions, dataset I/O, test harness integration. At least 5 example EvalCases shipping with the framework.
- Doc 09 in full: `docs/architecture-boundaries.md` published; CONTRIBUTING.md + README.md linked. Sentrux rules updated for new boundaries. CI gates verified.
- Doc 10 in full: empty pack skeletons added (`packs/research`, `packs/support`, `packs/devops`, `packs/aiops`). Each has `doc.go` and `README.md`. `examples/` curated showcase added.
- Remaining contract test suites: `RunProviderContractTests`, `RunToolDriverContractTests`, `RunPolicyEngineContractTests`, `RunOutputGatewayContractTests`.
- ADR-012 (Storage contract stability) written.
- ADR-013 (Memory kernel vs pipeline) written.
- ADR-014 (Agent ontology stance) written — records: accept structural ontology (Status, PreviousVersionID, RunSelector, reserved `hydaelyn.self.*`); reject metaphysical ontology (no Agent.Identity, no UpdateOwnProfile, no auto-derived personality, no built-in reflection hooks).
- `recipe/memory-pyramid/` and `recipe/context-canvas/` reserved namespaces created with `doc.go` + `README.md` (no Go implementation in v0.8.0; v0.9.0 deliverable).
- `docs/release-notes/v0.8.0.md` rewritten — full v0.8.0 changelog, migration guidance, theme statement.
- `docs/release-notes/archive/framework-purification.md` (if the old v2.0.md content is retained).
- `docs/migration.md` v0.7 → v0.8 guide.
- README updated with new positioning, differences table, agent mode index.
- Downstream project completes its upgrade against v0.8.0-rc2; bugs found are fixed before final tag.

**Duration estimate**: 5-7 working days.

## Total

| Phase | Weeks | Working days |
|-------|-------|--------------|
| 1 | 1 | 5-6 |
| 2 | 2-4 | 15 |
| 3 | 5-6 | 10 |
| 4 | 7-8 | 10 |
| 5 | 9 | 5-7 |
| **Total** | **~9 weeks** | **45-48** |

The drop from ~10 weeks to ~9 weeks is the dividend of Position C: removing framework ownership of production storage operations frees ~5 days from Phase 2 (no Atlas integration, no schema-evolution discipline as framework feature, no migration-CLI tooling, narrower quality bar for reference adapters).

## Branch strategy

A long-lived `release/v0.8.0` branch tracks the cumulative state. Each phase produces one or more PRs into this branch, not into `main`. `main` stays stable on v0.7.x.

When v0.8.0 is finalized, the entire `release/v0.8.0` branch merges into `main` as a single squash merge with the rewritten release notes. Tag `v0.8.0` from `main`.

Rationale: phases are interdependent; merging phase 1 to main would leave 8 weeks of unstable subsequent work in a state where users on `main` can hit half-implemented APIs.

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Postgres LISTEN/NOTIFY edge cases | — | — | **Already mitigated**: Postgres Subscribe deferred to v0.8.1. v0.8.0 ships Postgres adapter with polling fallback only. |
| OceanBase 4.x MySQL mode diverges from stock MySQL 8.0 on specific SQL (SKIP LOCKED, JSON funcs, txn timeouts) | **none under Position D** | n/a — framework ships no MySQL implementation | Per ADR-012 (revised, Position D) the framework ships no `storage/mysql/`. OB / MySQL fork compatibility is entirely an application-side concern, scoped by whoever writes the `api.StoreProvider`. The contract suite validates correctness; DBMS-specific quirks are out of the framework's scope. |
| OceanBase OBProxy connection routing causes lease drops under failover in reference impl | **low (downgraded under Position C)** | Reference impl users self-mitigate; framework provides guidance | `ConnMaxLifetime` 30 min; worker heartbeat extends lease independently of SQL connection lifetime; lease CAS detects stale ownership. Production teams running OB at scale are expected to use Layer 3 (own provider) regardless. |
| Contract test suite is incomplete and lets a broken external provider pass | medium | External providers ship subtle bugs | Contract tests are Phase 2's primary public deliverable, allocated 5 working days; every reference impl must pass before phase exits; tests are reviewed alongside the contract definition. |
| External project's existing flow blocks on Bypass field removal | high | Phase 1 stalls | Send migration patch directly; estimate 30 min downstream change |
| `Trigger.Filter map[string]string` proves too weak in Phase 4 | medium | Trigger v2 needed in v0.9.0 | Reserved future field name `FilterRaw json.RawMessage` documented in doc 03 |
| OpenAPI export complexity blows scope | low | Phase 4 slips | Ship minimal exporter (one POST per Capability); rich features deferred |
| Memory interface gets used wrongly (becomes Blackboard surrogate) | medium | Eval projections break | Document explicitly + add lint suggestion in code review checklist |
| Contract test suite reveals existing memory provider has hidden bugs | low | Phase 2 slips by 2-3 days | Acceptable; finding bugs is the point |

## Decision checkpoints

| End of phase | Decision required |
|--------------|-------------------|
| Phase 1 | External user reviewed and confirmed AgentProfile + Capability + ModelPolicy shapes. **No further changes to these fields without a new ADR.** |
| Phase 2 | Contract test suite is locked and reviewed (the framework's primary v0.8.0 storage deliverable). Per ADR-012 (revised, Position D) the framework ships no reference implementations; the suite is exercised in CI via the non-exported `contract/internal/inmemfake` adapter. Production providers are downstream-owned per doc 12. |
| Phase 3 | Default `PolicyEnforcer` shipped: yes/no opt-in. Recommend: shipped enabled by default, with a config switch to disable for legacy callers. |
| Phase 4 | Trigger runtimes: ship all three or subset. Recommend: scheduler + webhook in v0.8.0; event bus optional (interface defined, in-process default). |
| Phase 5 | Eval `Repeats` flakiness threshold: ship the framework with deterministic-only assertions documented, semantic-similarity assertions deferred to v0.9.0. |

## What "done" looks like for v0.8.0

- All 14 product-spec docs reflect shipped behavior
- `StoreProvider` contract is locked; `contract.RunStoreProviderContractTests` is exported and documented
- `go build ./...` and `go test ./...` green on memory, sqlite, mysql (stock + OB 4.x + TiDB 6), postgres CI matrices
- At least one external (downstream) provider has passed the contract test suite — validates that Layer 3 is reachable for real teams
- External downstream project has merged its v0.8.0 upgrade PR
- ADR-009, ADR-010, ADR-011, ADR-012, ADR-013, ADR-014 published
- `docs/release-notes/v0.8.0.md` complete
- `docs/architecture-boundaries.md` published
- README and CONTRIBUTING updated
- `v0.8.0` tag created from main
