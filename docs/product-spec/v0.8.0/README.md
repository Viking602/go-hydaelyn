# Hydaelyn v0.8.0 — Product Spec

> **Theme**: *Durable Typed Multi-Agent Framework*.
> v0.8.0 lifts Hydaelyn from a durable single-agent runtime into a
> first-class multi-agent framework: a strong bounded agent loop,
> an explicit multi-agent scheduler layer, and a narrowed durable
> Runner whose new stores make every multi-agent decision persistable,
> auditable, and resumable.

## Positioning

> Hydaelyn is a durable typed multi-agent framework for Go.
> It ships a strong but bounded single-agent loop, an explicit
> role/class based multi-agent scheduler, and durable execution
> primitives for approvals, audit, resume, and idempotent side effects.

## Four-layer architecture

```
┌─────────────────────────────────────────────┐
│ Packs / Examples                            │
│ incident triage, research, support, devops  │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│ Multi-Agent Layer  (multiagent/)            │
│ AgentClass, AgentInstance, Team, Scheduler  │
│ Dispatch, Handoff, Blackboard, Voting, …    │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│ Agent Loop Layer  (agent/)                  │
│ Step, OutputPolicy, ToolSafety,             │
│ ContextManager, AgentFailure, LoopPolicy    │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│ Durable Runner                              │
│ Run/Task/Event/Lease/Approval/Outbox/       │
│ ActionAttempt/Resume/DeadLetter + 3 new     │
│ stores (Handoff/TeamState/AgentInstance)    │
└─────────────────────────────────────────────┘
```

## Documents

Architecture (read in order):

1. [00 — Overview](00-overview.md) — positioning, scope, breaking changes, design defaults
2. [11 — Architecture Boundaries](11-boundaries.md) — six load-bearing principles
3. [03 — Agent Loop](03-agent-loop.md) — Step trace, OutputPolicy, ToolSafety, ContextManager, AgentFailure
4. [04 — Agent Class](04-agent-class.md) — AgentProfile / AgentClass / AgentInstance / Registry
5. [05 — Multi-Agent Layer](05-multi-agent-layer.md) — Team, Scheduler, Dispatch, typed Handoff, Voting
6. [06 — Durable Runner](06-durable-runner.md) — what Runner owns / does not own

Surfaces:

7. [01 — Public API Hardening](01-public-api.md)
8. [02 — Capability Layer](02-capability.md)
9. [07 — Storage Contract (Position D)](07-storage.md)
10. [08 — Governance](08-governance.md)
11. [09 — Context Layer](09-context.md)
12. [10 — Evaluation Framework](10-evaluation.md)

Execution:

13. [12 — Package Structure](12-package-structure.md)
14. [13 — Rollout Plan (Phase 0 – 5)](13-rollout-plan.md)
15. [14 — Migration Guide v0.7 → v0.8](14-migration-guide.md)
16. [15 — Memory as Optional Plugin](15-memory-optional-plugin.md)

Worked example:

17. [16 — Multi-Agent Demo (Security Incident Triage)](16-multi-agent-demo.md)

## Master spec

The architectural anchor that drove this reconstruction is
`docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`.
All v0.8.0 docs in this folder defer to it.

## ADRs that anchor this release

- ADR-007 — EventStore replay
- ADR-008 (revised) — Framework vs business (with multi-agent primitive exception list)
- ADR-012 (revised) — Storage Position D
- ADR-013 — Memory as optional plugin
- ADR-014 (revised) — Agent ontology (AgentInstance accepted)
- ADR-015 — Strong Bounded Agent Loop
- ADR-016 — Explicit Multi-Agent Scheduler
- ADR-017 — Durable Runner Boundary

## What ships

| Layer | v0.8.0 deliverable |
|-------|--------------------|
| Packs | `packs/{research,support,devops,aiops}` skeletons + `packs/aiops/incident-triage` full demo |
| Multi-Agent | `multiagent/` with AgentClass / AgentInstance / Team / Scheduler interface + Sequential/Router/Supervisor impls / Dispatch / typed Handoff / multi-agent Blackboard / Voting types / 7 multi-agent event kinds |
| Agent Loop | `agent/` with Step / StepPolicy / OutputPolicy + repair / Result / ToolSafety / ContextManager / AgentFailure / LoopPolicy |
| Durable Runner | Existing 15 stores + 3 new (HandoffStore / TeamStateStore / AgentInstanceStore); Task contract enrichment; Worker Runtime unchanged at lease level |
| Cross-cutting | Public API hardening (4 breaking changes), Capability layer with reserved `hydaelyn.self.*`, Position D storage contract, Governance (Usage + TaskBudget unified + Quota + 6 obligations), Context four-tier, Eval + multi-agent assertions |

## What is explicitly out

- Advanced scheduler strategies (Debate / MapReduce / DAG / Swarm) — v0.9.0
- Domain-specific packs beyond skeletons — v1.0.0+
- v1.0.0 SemVer commitment — v1.0.0 explicitly
- Hosted observability backends — v0.9.0 minimum
- Provider deep integrations — out of framework scope
- UI / Console — out of framework scope
- Built-in Memory backend — never (ADR-013)

## Status

Spec rewrite complete (date: 2026-05-24). Implementation rolls out per
[13 — Rollout Plan](13-rollout-plan.md), Phase 0 through Phase 5.
