# 11 — Architecture Boundaries

> Anchor: ADR-008 (revised), ADR-015, ADR-016, ADR-017.
> Renumbered from `09-boundaries.md` in the v0.8.0 reconstruction.

## Goal

Codify the **six** principles that prevent the framework from sliding
back into either a domain-specific runtime or a coordination-free
agent bag. Principles 1-5 carry forward (with extensions); Principle 6
is new for v0.8.0.

This document is the source for `docs/architecture-boundaries.md` (a
copy with the same content, located outside `product-spec/`, linked
from CONTRIBUTING.md and README).

> **Post-release clarification (current `main` / v0.14):** The live
> boundary document is now `docs/architecture-boundaries.md`. This
> versioned file remains the maintained v0.8.0 design record; use tag
> `v0.8.0` for the exact released code. Current `multiagent/` may import
> the runtime-neutral `stream/` package. Import-boundary checks cover
> production and test imports, including worker/packs/coding reverse
> edges and a named coding-test exception. Sentrux handles the
> complementary global cycle, coupling, and God File checks.

## Principle 1 — Core has no domain vocabulary

**Rule**: Code under `api/`, `internal/core/**`, `internal/run/`,
`internal/task/`, `internal/blackboard/`, `internal/mailbox/`,
`internal/policy/`, `internal/provider/`, `internal/tool/`,
`internal/handoff/`, `internal/approval/`, `internal/governance/`,
`agent/**`, **and `multiagent/**`** MUST NOT contain identifiers,
constants, or strings matching domain words.

Forbidden words (closed list — adding requires ADR amendment):

```
incident, change, ticket, customer, sales, deploy, repository, document,
review (as a TaskType), action (as a TaskType), synthesis, hazard, lead,
agent_review
```

**Multi-agent primitive exception** (per ADR-008 revised):

```
Scheduler, Supervisor, Voting, Debate, Handoff, Dispatch, Team,
AgentClass, AgentInstance, TypedReport, TeamState
```

These are framework primitives for multi-agent coordination and are
**permitted** in `api/`, `agent/`, `multiagent/`. They are not business
vocabulary.

Permitted locations for unrestricted domain vocabulary:

- `_examples/`, `examples/`, `packs/`, `recipe/`, `docs/use-cases/`,
  `docs/`

**Enforcement**: `scripts/check-business-words.sh` baseline updated
alongside ADR-008 revision. `architecture-gate` CI job rejects baseline
increases.

## Principle 2 — Recipes compile to Run/Task; no second runtime

**Rule**: Any pattern, recipe, flow, template, or higher-level construct
MUST execute by emitting Commands to the existing Runner. A pattern
MUST NOT create a parallel execution engine that bypasses
Run/Task/Event/Lease/Command/Policy/Outbox.

**New for v0.8.0**: Multi-agent Schedulers are NOT "second runtimes."
They emit Dispatches that Runner persists as Tasks via the existing
TaskStore. A Scheduler that bypasses Runner (writes its own event
store, manages its own lease) is in violation.

**Enforcement**: existing sentrux `[[boundaries]]` rules extended to
forbid `multiagent/` from importing any `internal/` package other than
through `api/`.

## Principle 3 — Five concepts, five owners (never bundled)

| Concept | What it answers | Where it lives |
|---------|----------------|----------------|
| **Capability** | What can an Agent call? | `api.Capability` (declaration), `api.Tool` (execution binding) |
| **Procedure** | How should an Agent do something? | Pack code, recipe, prompt template — outside the kernel |
| **Policy** | Is an Agent allowed to do something? | `api.PolicyEngine`, `api.PolicyEnforcer` |
| **Runtime** | How is the doing executed and recovered? | Runner, Worker Runtime, Storage |
| **Scheduling** | Who runs next, in what team configuration? | `multiagent.Scheduler`, `multiagent.Team` |

Procedure includes Agent Skills: reusable procedure/instruction bundles may
reference tools but do not authorize tools, make policy decisions, schedule
agents, or persist runtime state.

Five concepts (extended from four). Conflating any two is the
single most common framework anti-pattern.

**Anti-patterns to reject at code review** (extended):

- A Capability whose `Description` encodes step-by-step instructions (procedure)
- A PolicyEngine that decides which Capability to call (procedure or scheduling)
- A recipe that hardcodes a Policy decision (use PolicyEngine)
- A Tool that decides whether it should run (delegate to PolicyEngine)
- **A Scheduler that mutates external state directly** (route through agent tools)
- **A Runner that picks the next agent** (delegate to Scheduler)
- **An Engine that schedules other agents** (one-way dependency: multiagent → agent)

## Principle 4 — All side effects must be auditable

**Rule**: Any operation that mutates external state or generates
externally-visible output MUST produce:

- A `ToolInvocation` record OR an `ActionAttempt` record
- An `Event` of the appropriate type
- A `TraceSpan` if a trace is active
- A `UsageRecord` for resource accounting

**New for v0.8.0**: Scheduler Dispatches and instance lifecycle changes
produce audit events (`EventDispatchEmitted`, `EventInstanceSpawned`,
`EventInstanceCompleted`, `EventSchedulerTick`). `EventHandoffPersisted`
is reserved for the v0.9.0 HandoffStore-backed scheduler flow.

## Principle 5 — All long-running work must be resumable

**Rule** (unchanged in substance): any unit of work that may exceed a
single process lifetime MUST support Lease, Heartbeat, Resume, Replay,
Dead-letter, Reconcile.

**New for v0.8.0**: resume reconstructs **three independent surfaces**:

1. Run state (Runner — via Run/Task/Event stores)
2. Step trace (agent.Engine — via EventStore EventStepCompleted)
3. Scheduler decision history (multiagent.Scheduler — via
   TeamStateStore + EventStore EventSchedulerTick)

Failing to reconstruct any one MUST NOT corrupt the others. This is
what makes the four-layer architecture survive process kill.

## Principle 6 — Typed failure crosses layers (NEW for v0.8.0)

**Rule**: Bare `error` MUST NOT cross the agent → multiagent boundary
or the multiagent → Pack boundary. Failures MUST be expressed as:

- `agent.AgentFailure` (with `FailureKind` enum) at the
  agent → multiagent boundary.
- `multiagent.SchedulerFailure` or a typed terminal Run status at the
  multiagent → Pack boundary.

**Why**: scheduler decisions, retry policies, and team-level eval all
require failures to be categorized rather than stringified. A bare
error tells the Scheduler "something broke" but cannot tell it
*how* to react (retry? escalate? request approval?). Typed failure
makes correct reactions possible.

**Enforcement**: code review checks every Engine return signature.
`agent.Result.Failure *agent.AgentFailure` is the only failure channel.
Bare error returns from Engine entry points are rejected.

## Enforcement summary

| Principle | Primary enforcement | Secondary |
|-----------|---------------------|-----------|
| 1. No domain vocabulary (with multi-agent exception list) | `scripts/check-business-words.sh` + CI gate | Code review against this doc |
| 2. No second runtime | Sentrux `[[boundaries]]` rules | Code review |
| 3. Five concepts, five owners | Code review against §3 table | Public docs and naming |
| 4. All side effects auditable | Public API only exposes auditable paths | Runtime invariants in tests |
| 5. Long work resumable (3-surface reconstruction) | Storage contract tests + Worker Runtime test | Failure injection in CI |
| 6. Typed failure across layers | Code review on Engine signatures | Lint: forbid bare error returns from Engine entry points |

## Mapping to ADRs

- ADR-007 (EventStore replay) — Principle 4 + Principle 5
- ADR-008 (revised, framework vs business) — Principle 1 + multi-agent exception list
- ADR-012 (Position D, storage) — Principle 4 + Principle 5
- ADR-013 (Memory kernel vs pipeline) — Principle 3
- ADR-014 (revised, agent ontology) — Principle 1 + Principle 3 (AgentInstance accepted)
- ADR-015 (Strong Bounded Agent Loop) — Principle 3 + Principle 6
- ADR-016 (Explicit Multi-Agent Scheduler) — Principle 2 + Principle 3 + Principle 4
- ADR-017 (Durable Runner Boundary) — Principle 3 + Principle 4 + Principle 5

## File locations

The full text of these six principles is published in:

- `docs/architecture-boundaries.md` (canonical) — copied from this document
- Referenced from `CONTRIBUTING.md`
- Linked from top of `README.md`
- Referenced by every ADR listed in §Mapping

The first item is now the live document. Use this versioned record for the
historical v0.8.0 design and
[`docs/architecture-boundaries.md`](../../architecture-boundaries.md)
for current seams.

## Verification

- `TestArchitectureBoundaries_NoBusinessWordsInCore` — `scripts/check-business-words.sh`
- `TestArchitectureBoundaries_MultiagentPrimitivesAllowed` — exception list words appear in `multiagent/`, `agent/`, `api/` without triggering CI failure
- `TestArchitectureBoundaries_NoSecondRuntimes` — sentrux verifies no recipe / `multiagent` package implements its own event store
- `TestArchitectureBoundaries_AllSideEffectsProduceUsageRecord` — register tool, invoke, assert UsageRecord
- `TestArchitectureBoundaries_ResumeReconstructsThreeSurfaces` — kill mid-run, restart, observe Run + Steps + Scheduler decisions
- `TestArchitectureBoundaries_TypedFailureAtBoundaries` — vet-style check rejecting Engine APIs that return bare error
- `TestArchitectureBoundaries_AgentNeverImportsMultiagent` — sentrux one-way dependency check
- `TestArchitectureBoundaries_RunnerNeverImportsMultiagent` — sentrux check
- `TestArchitectureBoundaries_MultiagentNeverImportsRunner` — sentrux check (only `api/` allowed)
