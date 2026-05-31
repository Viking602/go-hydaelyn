# 00 — Overview

## Positioning

> Hydaelyn is a durable typed multi-agent framework for Go.
> It ships a strong but bounded single-agent loop, an explicit
> role/class based multi-agent scheduler, and durable execution
> primitives for approvals, audit, resume, and idempotent side effects.

Four load-bearing pieces:

- **Strong bounded Agent Loop** (`agent/`): one agent does one task well. Step trace, schema repair, tool safety, context management, typed failure, budget enforcement. Strong inside its boundary; never imports the layer above it.
- **Explicit Multi-Agent Scheduler** (`multiagent/`): named first-class primitives for team coordination — AgentClass, AgentInstance, Team, Scheduler, Dispatch, Handoff, Blackboard, Voting, Supervisor.
- **Durable Runner** (root + `internal/run`, `internal/task`, …): persists, leases, audits, and resumes every multi-agent decision. Owns the storage contract, the lease CAS, the event log, the approval queue, the outbox, and the idempotency ledger.
- **Product Packs** (`packs/`, `_examples/`): vertical scenarios. Free to encode domain vocabulary; the kernel never does.

## Why the framing changed

Earlier v0.8.0 drafts framed the framework as *thin Engine + thick Runner
+ flagship Pack*. That framing under-served the actual product goal:
multi-agent applications built on Go. Engineers building those need
three things the old framing did not surface clearly:

1. A **strong single-agent loop** with step-level introspection, schema
   repair, and typed failure — without which schedulers cannot make
   correct retry / handoff / approval decisions.
2. An **explicit multi-agent layer** with named primitives —
   Scheduler, Dispatch, Handoff, Team — instead of "Packs glue stuff
   together with helpers."
3. A **narrow durable Runner** that records and resumes scheduler
   decisions rather than making them.

The v0.8.0 reconstruction (master spec
`docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`)
flips the three. The Runner stays durable but narrows; the Agent Loop
gains depth; the Multi-Agent Layer is promoted from Pack helper to
kernel package.

## What v0.8.0 ships

Mapped to the four-layer architecture:

| Layer | What ships in v0.8.0 |
|-------|----------------------|
| Packs | Skeleton `packs/{research,support,devops,aiops}` (doc.go + README), incident-triage demo (16-multi-agent-demo.md) |
| Multi-Agent | `multiagent/` package: AgentClass, AgentInstance, Team, Scheduler interface + Sequential/Router/Supervisor impls, Dispatch, typed Handoff, multi-agent Blackboard, Voting, Supervisor helpers, events. See `05-multi-agent-layer.md`. |
| Agent Loop | `agent/` package: Step trace, StepPolicy, OutputPolicy + repair, Result, ToolSafety, ContextManager, AgentFailure, LoopPolicy. See `03-agent-loop.md`. |
| Durable Runner | Existing 15 stores + 3 new (HandoffStore, TeamStateStore, AgentInstanceStore). Task contract enrichment (InputSchema, OutputSchema, TaskBudget). Worker Runtime unchanged at lease/heartbeat level. See `06-durable-runner.md`. |
| Cross-cutting | Public API hardening (01), Capability layer (02), Storage contract Position D (07), Governance (08), Context four-tier (09), Eval (10) — all carry forward with multi-agent extensions. |

## Scope

In:

- Strong bounded Agent Loop (`03-agent-loop.md`)
- AgentClass / AgentInstance / AgentProfile / Registry (`04-agent-class.md`)
- Multi-Agent Layer (`05-multi-agent-layer.md`)
- Durable Runner narrowing (`06-durable-runner.md`)
- Public API hardening (`01-public-api.md`)
- Capability layer (`02-capability.md`)
- Storage contract Position D + 3 new stores (`07-storage.md`)
- Governance — Usage, TaskBudget, Policy Obligation (`08-governance.md`)
- Context layer four-tier model (`09-context.md`)
- Evaluation framework + multi-agent assertions (`10-evaluation.md`)
- Architecture boundaries — six principles (`11-boundaries.md`)
- Package structure (`12-package-structure.md`)
- Rollout plan — Phase 0-5 (`13-rollout-plan.md`)
- Migration v0.7 → v0.8 (`14-migration-guide.md`)
- Memory optional plugin (`15-memory-optional-plugin.md`)
- Incident triage demo (`16-multi-agent-demo.md`)

Out:

- Advanced scheduler strategies (Debate / MapReduce / DAG / Swarm) — v0.9.0
- New domain-specific packs beyond skeletons — v1.0.0 or later
- v1.0.0 SemVer commitment — v1.0.0 explicitly
- Hosted observability backends — v0.9.0 minimum; `observe/otel/` skeleton may land
- Provider-specific deep integrations — out of framework scope
- UI / Console — out of framework scope
- Built-in Memory backend — never (per ADR-013, optional plugin only)

## Breaking changes summary

Total: 4 mechanical changes to public API (carried from prior v0.8.0 draft). All grouped in Phase 1.

1. `api.Flow`: remove all `Bypass*` fields.
2. `api.ErrFlowBypass` removed.
3. `Runner.ExecuteCommand(StartRunCommand)` returns `api.StartRunResult` (was `[]any{Run, RootTask}`).
4. `Runner.ExecuteCommand(RequestApprovalCommand)` returns `api.RequestApprovalResult` (was `[]any{Approval, Token}`).

Additive (no break):

- `api.Task` gains `InputSchema`, `OutputSchema`, `Budget` (all omitempty).
- `api.AgentProfile` gains `Status`, `PreviousVersionID`, `Version`, `Instructions`, `Model`, `Capabilities`, `Triggers`, `Governance`.
- `agent/` package gains `Step`, `OutputPolicy`, `Result`, `ToolSafety`, `ContextManager`, `AgentFailure`, `LoopPolicy`, `StepPolicy`.
- `multiagent/` is a new top-level package.
- `UnitOfWork` gains 3 stores: `HandoffStore`, `TeamStateStore`, `AgentInstanceStore`.

Package path changes:

- `internal/memory` stays internal (no move).
- New top-level packages: `agent/` (extended), `multiagent/` (NEW), `memory/` (interface only), `artifact/`, `eval/`, `recipe/`, `contract/`, `packs/`, `transport/openapi/`, `transport/webhook/`, `transport/cron/`, `transport/event/`, `observe/otel/`.
- `storage/` does NOT exist — Position D (ADR-012 revised).

## Theme statement (for README and release notes)

> Hydaelyn v0.8.0 — *Durable Typed Multi-Agent Framework*.
> v0.8.0 lifts Hydaelyn from a durable single-agent runtime into a
> first-class multi-agent framework. The kernel ships a strong bounded
> agent loop (Step trace, schema repair, ToolSafety, ContextManager,
> typed AgentFailure), an explicit Multi-Agent Layer (AgentClass /
> AgentInstance / Team / Scheduler / Dispatch / typed Handoff /
> Voting / Supervisor), and a narrowed durable Runner whose new stores
> (HandoffStore, TeamStateStore, AgentInstanceStore) make every
> multi-agent decision persistable, auditable, and resumable. The
> framework remains domain-free; everything new is mechanism, not
> policy. Per ADR-012 (revised, Position D) the framework ships no
> `api.StoreProvider` implementation; per ADR-013 the framework ships
> no `api.Memory` backend.

## Open design defaults (locked unless rejected)

| # | Topic | Default |
|---|-------|---------|
| 1 | `UsageRecord.Credits` semantic | Abstract integer cost unit; framework records, adapters define meaning. |
| 2 | Storage path | Position D (ADR-012 revised). Framework owns contract + suite; no implementation ships. |
| 3 | `Memory[T]` interface | Optional plugin (ADR-013). Verbs in kernel, storage in application. |
| 4 | `Scheduler` v0.8.0 implementations | Sequential / Router / Supervisor. Debate / MapReduce / DAG / Swarm deferred to v0.9.0. |
| 5 | `AgentInstance.ID` generation | Deterministic from `(RunID, ClassName, SpawnSequence)`. Random ID rejected. |
| 6 | `TaskBudget` enforcement boundary | Engine, not Runner. Runner records `UsageRecord` and persists `Task.Budget`. |
| 7 | `Handoff` discipline | Typed only. `Handoff.Payload` MUST validate against receiving class's `InputSchema`. Free-form prose handoffs rejected at kernel level. |
| 8 | `AgentFailure` boundary | The only failure shape that crosses the agent → multiagent boundary. Bare `error` rejected at code review. |

## Read order

1. `11-boundaries.md` — the six principles enforced by everything else (NEW: Principle 6 — typed failure across layers)
2. `03-agent-loop.md` → `04-agent-class.md` → `05-multi-agent-layer.md` → `06-durable-runner.md` — the four-layer architecture
3. `01-public-api.md` → `02-capability.md` → `07-storage.md` — surfaces
4. `08-governance.md` → `09-context.md` → `10-evaluation.md` — cross-cutting concerns
5. `12-package-structure.md` → `13-rollout-plan.md` → `14-migration-guide.md` → `15-memory-optional-plugin.md` — execution
6. `16-multi-agent-demo.md` — worked example (5-role incident triage)
