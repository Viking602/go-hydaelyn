# 09 — Architecture Boundaries

## Goal

Codify the five principles that prevent the framework from sliding back into a domain-specific runtime. These principles ratify and extend ADR-008.

This document is the source for `docs/architecture-boundaries.md` (a copy with the same content but located outside `product-spec/`, intended as a permanent reference linked from CONTRIBUTING.md and README).

## Principle 1 — Core has no domain vocabulary

**Rule**: Code under `api/`, `internal/core/**`, `internal/run/`, `internal/task/`, `internal/blackboard/`, `internal/mailbox/`, `internal/policy/`, `internal/provider/`, `internal/tool/`, `internal/handoff/`, `internal/approval/`, `internal/governance/` MUST NOT contain identifiers, constants, or strings matching domain words.

Forbidden words include but are not limited to:

```
incident, change, ticket, customer, sales, deploy, repository, document,
review (as a TaskType), action (as a TaskType), synthesis, hazard, lead, agent_review
```

Permitted locations:

- `_examples/`
- `examples/` (v0.8.0 rename)
- `packs/`
- `docs/use-cases/`
- `recipe/` (when the recipe references a domain-specific Capability by name in a metadata field, that is allowed because the recipe itself is acknowledged as configuration not framework code)

**Enforcement**: `scripts/check-business-words.sh` extended to the v0.8.0 word list and locked baseline. `architecture-gate` CI job rejects baseline increases.

**Why**: When the kernel knows what "incident" means, every team that doesn't do incident response is taxed by the abstraction. When it doesn't know, all teams pay only for the primitives they actually use.

## Principle 2 — Recipes compile to Run/Task; no second runtime

**Rule**: Any pattern, recipe, flow, template, or higher-level construct MUST execute by emitting Commands to the existing Runner. A pattern MUST NOT create a parallel execution engine that bypasses Run/Task/Event/Lease/Command/Policy/Outbox.

**Enforcement**:

- All recipe packages import only `api/` and the Runner.
- No recipe package implements its own command bus, event store, or scheduler.
- `internal/core/flow_registry.go` rejects flows whose registration would imply runtime substitution (the v0.7.0 Bypass check, after v0.8.0 cleanup, becomes: any recipe attempting to register a non-standard executor is rejected).

**Why**: Two runtimes is two state machines, two replay paths, two policy enforcement points. The framework promise of "every state change replayable" collapses if even one recipe bypasses it.

## Principle 3 — Capability ≠ Procedure ≠ Policy ≠ Runtime

Four concepts, four owners. Never bundled.

| Concept | What it answers | Where it lives |
|---------|----------------|----------------|
| **Capability** | What can an Agent call? | `api.Capability` (declaration), `api.Tool` (execution binding) |
| **Procedure** | How should an Agent do something? | Pack code, recipe, prompt template — outside the kernel |
| **Policy** | Is an Agent allowed to do something? | `api.PolicyEngine`, `api.PolicyEnforcer` |
| **Runtime** | How is the doing executed and recovered? | Runner, Worker Runtime, Storage |

**Anti-patterns to reject in code review**:

- A Capability whose `Description` encodes step-by-step instructions (that's procedure)
- A PolicyEngine that decides which Capability to call (that's procedure or runtime)
- A recipe that hardcodes a Policy decision (use PolicyEngine)
- A Tool that decides whether it should run (delegate to PolicyEngine)

**Why**: Conflating these is the single most common framework anti-pattern. When Capability description tells the Agent how to use it, two systems become coupled and you can no longer evolve either independently.

## Principle 4 — All side effects must be auditable

**Rule**: Any operation that mutates external state or generates externally-visible output MUST produce:

- A `ToolInvocation` record OR an `ActionAttempt` record
- An `Event` of the appropriate type
- A `TraceSpan` if a trace is active
- A `UsageRecord` for resource accounting

**Enforcement**:

- Runner's `ExecuteTool` path is the only sanctioned route to external side effects from agent code.
- Provider stream completion always emits a `UsageRecord`.
- `OutputGateway` is the only sanctioned route to user-visible output and is mandatory (cannot be bypassed; this is what Principle 5 used to allow via the now-removed `BypassOutputGateway` field).

**Why**: A framework that lets side effects happen unaudited cannot honor its replay or governance promises. Auditability is not optional, it's structural.

## Principle 5 — All long-running work must be resumable

**Rule**: Any unit of work that may exceed a single process lifetime MUST support:

- **Lease**: acquired via `LeaseStore.AcquireWithExpectedVersion`
- **Heartbeat**: extension via `LeaseStore.ExtendLease`
- **Resume**: continuation via `ResumeToken` after process restart
- **Replay**: deterministic projection from the event stream
- **Dead-letter**: termination path with cause record in `DeadLetterStore`
- **Reconcile**: external observers can read run state and rebuild their derived state

**Enforcement**:

- Worker Runtime (doc 04) provides all six mechanisms.
- Storage contract test (doc 05) verifies each.
- Background and reactive Trigger paths use the same lease/heartbeat protocol as user-initiated runs.

**Why**: "It runs to completion in one process" is not a usable durability model for production agents. Servers restart, networks partition, processes get OOM-killed. The framework must assume failure and design for resume.

## Enforcement summary

| Principle | Primary enforcement | Secondary |
|-----------|---------------------|-----------|
| 1. No domain vocabulary | `scripts/check-business-words.sh` + CI gate | Code review against this doc |
| 2. No second runtime | Sentrux `[[boundaries]]` rules in `.sentrux/rules.toml` | Code review |
| 3. Capability ≠ Procedure ≠ Policy ≠ Runtime | ADR-009 + code review | Public docs and naming |
| 4. All side effects auditable | Public API only exposes auditable paths | Runtime invariants in tests |
| 5. Long work resumable | Storage contract tests + Worker Runtime test | Failure injection in CI |

## Mapping to ADRs

- ADR-008 (framework vs business) — Principle 1 codifies the rules
- ADR-005 (CapabilityInvoker) — Principle 3 lives across declaration (`api.Capability`) and enforcement (internal `CapabilityInvoker`)
- ADR-007 (EventStore replay) — Principle 4 + Principle 5
- ADR-009 (Capability public API, new in v0.8.0) — formalizes Principle 3
- ADR-011 (Context four-layer, new in v0.8.0) — supports Principle 3 (do not conflate Memory with Blackboard)
- ADR-012 (Storage contract — revised 2026-05-24 to Position D) — supports Principles 4 and 5. The framework owns the `StoreProvider` contract and the contract test suite. The framework ships no reference implementation, public or otherwise; `storage/` no longer exists in the repository. Applications implement the contract against their own data stack (ent / gorm / sqlc / DBA-controlled DDL) and validate via `contract.RunStoreProviderContractTests`. See `12-migration-guide.md` for the ent-based template.
- ADR-013 (Memory kernel vs pipeline, new in v0.8.0) — supports Principle 3; records why `api.Memory` is storage-only and extraction pipelines live in `recipe/`
- ADR-014 (Agent ontology stance, new in v0.8.0) — supports Principle 1 and Principle 3; records why `AgentProfile.Status`, `PreviousVersionID`, `RunSelector.AgentID/AgentVersion`, and the reserved `hydaelyn.self.*` Capability namespace land in the kernel while subjective-experience constructs are explicitly excluded

## File locations

The full text of these five principles is published in:

- `docs/architecture-boundaries.md` (canonical) — copied from this document
- Referenced from `CONTRIBUTING.md`
- Linked from top of `README.md`
- Referenced by every relevant ADR

## Verification

- `TestArchitectureBoundaries_NoBusinessWordsInCore` — CI check via `scripts/check-business-words.sh`
- `TestArchitectureBoundaries_NoSecondRuntimes` — sentrux gate verifies no recipe package implements internal command bus types
- `TestArchitectureBoundaries_AllSideEffectsProduceUsageRecord` — integration test: register a tool, invoke it, assert UsageRecord exists
- `TestArchitectureBoundaries_ResumeAfterProcessKill` — integration test: start run, kill process, restart, observe run resumes
