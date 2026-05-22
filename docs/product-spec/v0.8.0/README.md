# Hydaelyn v0.8.0 — Product Specification

This directory contains the complete design specification for Hydaelyn v0.8.0.

## Theme

**General-purpose, durable, governed Agent runtime for Go.**

v0.8.0 lifts Hydaelyn from "runnable runtime" to "publishable framework": every cross-cutting concern an Agent application meets in production is given a stable, documented public surface — and nothing else creeps into the kernel.

## Documents

| # | Document | Subject |
|---|----------|---------|
| 00 | [overview.md](./00-overview.md) | Positioning, scope, what's in / what's out |
| 01 | [public-api.md](./01-public-api.md) | Bypass field removal + typed results + `ExecuteCommand` retirement |
| 02 | [capability.md](./02-capability.md) | `Capability` + `CapabilityManifest` + MCP/OpenAPI/CLI/LLM exports + reserved `hydaelyn.self.*` namespace |
| 03 | [agent-profile.md](./03-agent-profile.md) | `AgentProfile` extension (incl. `Status` + `PreviousVersionID`) + `ModelPolicy` + `Registry` + `RunSelector` |
| 04 | [execution-layer.md](./04-execution-layer.md) | Worker Runtime + Trigger Runtime + Dead-letter |
| 05 | [storage.md](./05-storage.md) | Storage **contract** (primary surface) + contract test suite + memory/sqlite/mysql/postgres reference implementations + Layer 3 BYO provider on-ramp |
| 06 | [governance.md](./06-governance.md) | `UsageRecord` + `Budget` + Policy Obligation Enforcer |
| 07 | [context.md](./07-context.md) | Blackboard / Memory / Artifact / ContextSource four-layer model — incl. drill-down `Refs`, `Layer` hint, reserved retrieval strategies, `knowledge_graph` source kind |
| 08 | [evaluation.md](./08-evaluation.md) | Eval framework + Assertions |
| 09 | [boundaries.md](./09-boundaries.md) | Architecture principles (5 rules) |
| 10 | [package-structure.md](./10-package-structure.md) | Final directory layout |
| 11 | [rollout-plan.md](./11-rollout-plan.md) | Phase 1-5 execution plan + timeline + risks |
| 12 | [migration-guide.md](./12-migration-guide.md) | v0.7 → v0.8 migration for downstream users |

## Status

Approved scope, design defaults locked in, ready for Phase 1 implementation. Open design points are listed at the end of [overview.md](./00-overview.md).

## Versioning Note

Hydaelyn is pre-1.0. v0.8.0 is the largest single release on the path to v1.0.0. The v0.8.0 spec deliberately reserves schema points (`ContextSourceKnowledgeGraph`, the `hydaelyn.self.*` Capability namespace) without shipping the pipelines that consume them — those land in v0.9.0 per [`../v0.9.0/README.md`](../v0.9.0/README.md). `Memory[T Identified]` ships as an optional plugin (ADR-013 / `13-memory-optional-plugin.md`); the framework defines the verbs and the identity contract but ships no reference implementation, so there are no reserved schema points on the entity type — those belong to the application.

The Agent ontology stance — accept structural identity (`AgentProfile.Status`, `PreviousVersionID`, `RunSelector.AgentID/AgentVersion`) and reject metaphysical identity (no `Agent.Self` type, no `UpdateOwnProfile`, no auto-derived personality) — is recorded in ADR-014 and applied across docs 02, 03, 07, and 09.

The Storage stance — the framework owns the `StoreProvider` contract and the contract test suite, reference implementations (memory/sqlite/mysql/postgres) exist as starting points for forking, and production teams are expected to write their own provider against their data stack (ent / gorm / DBA-controlled DDL) — is documented end-to-end in doc 05 and the Layer 3 on-ramp in doc 12. The framework does not commit to operating any reference implementation at production scale.
