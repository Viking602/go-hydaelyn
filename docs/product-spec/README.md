# Hydaelyn Product Specification

Source-of-truth design documents for Hydaelyn, organized by release.

## Versions

| Version | Status | Theme |
|---------|--------|-------|
| [v0.8.0](./v0.8.0/) | **Active** — implementation in progress | Durable Typed Multi-Agent Framework: strong bounded agent loop, explicit multi-agent scheduler layer (`multiagent/`), narrowed durable Runner, Capability layer, Position D storage contract, four-layer context, eval framework with multi-agent assertions |
| [v0.9.0](./v0.9.0/) | Roadmap stub | Advanced scheduler strategies (Debate / MapReduce / DAG / Swarm), Memory pipeline (L0→L3), symbolic short-term context (Mermaid canvas), knowledge-graph context source, hosted observability surfaces |

## Positioning (v0.8.0+)

> Hydaelyn is a durable typed multi-agent framework for Go.
> It ships a strong but bounded single-agent loop, an explicit
> role/class based multi-agent scheduler, and durable execution
> primitives for approvals, audit, resume, and idempotent side effects.

Four load-bearing layers:

```
Packs / Examples
        ↓
Multi-Agent Layer  (multiagent/)
        ↓
Agent Loop Layer   (agent/)
        ↓
Durable Runner     (runner + internal/run/task/event/...)
```

## Conventions

- Each version directory is self-contained: every doc inside `v0.8.0/` describes what v0.8.0 ships, not what is planned next.
- Cross-version references use the form `[doc 09 in v0.9.0](./v0.9.0/09-context.md)` so links remain unambiguous.
- Decisions land via ADRs in `../adr/`. The version directory's `11-boundaries.md` and `13-rollout-plan.md` enumerate which ADRs that version introduces.

## Read order for new contributors

1. Start at the active version's [README](./v0.8.0/README.md).
2. Read `11-boundaries.md` first — every other doc operates under the six principles.
3. Read the four-layer chain: `03-agent-loop` → `04-agent-class` → `05-multi-agent-layer` → `06-durable-runner`.
4. Then follow the dependency order listed in `00-overview.md`.

## Master spec

The architectural anchor for v0.8.0 lives at
`../superpowers/specs/2026-05-24-agent-layer-business-stance.md`. All
v0.8.0 docs in `v0.8.0/` defer to it.

## ADR index (v0.8.0 era)

- ADR-007 — EventStore replay
- ADR-008 (revised) — Framework vs business + multi-agent primitive exception list
- ADR-012 (revised) — Storage Position D
- ADR-013 — Memory as optional plugin
- ADR-014 (revised) — Agent ontology (AgentInstance accepted)
- ADR-015 — Strong Bounded Agent Loop
- ADR-016 — Explicit Multi-Agent Scheduler
- ADR-017 — Durable Runner Boundary
