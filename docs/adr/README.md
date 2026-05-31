# ADR Queue

Architecture Decision Records, landed by milestone.

## Pre-v0.8.0 (landed)

- [ADR-001 `Profile` vs `AgentInstance` separation](ADR-001-profile-agent-instance-separation.md)
- [ADR-002 Task DAG and `FailurePolicy`](ADR-002-task-dag-failure-policy.md)
- [ADR-003 Plugin Registry and lifecycle](ADR-003-plugin-registry-lifecycle.md)
- [ADR-004 Middleware order and short-circuit](ADR-004-middleware-order-and-short-circuit.md)
- [ADR-005 `CapabilityInvoker` governance layer](ADR-005-capability-invoker-governance-layer.md)
- [ADR-006 Blackboard / Evidence data model](ADR-006-blackboard-evidence-model.md)
- [ADR-007 EventStore and replay semantics](ADR-007-eventstore-replay-semantics.md)
- [ADR-008 Framework vs business boundary](ADR-008-framework-vs-business.md)

ADR-001 through ADR-008 were originally authored in Chinese (before the English-only docs convention was adopted) and have since been translated to English to match the rest of the repository.

## v0.8.0 (complete)

- [ADR-009 Capability Public API](ADR-009-capability-public-api.md) — declaration vs execution vs enforcement; the four exports (MCP / OpenAPI / CLI / LLM tool def); reserved `hydaelyn.self.*` namespace
- [ADR-010 Usage, Budget, and Policy composition](ADR-010-usage-budget-policy-composition.md) — measurement (`UsageRecord`) feeds enforcement (`BudgetPolicy` ⊆ `PolicyEngine`); conditional authorization via `PolicyEnforcer` obligations
- [ADR-011 Context four-layer model](ADR-011-context-four-layer-model.md) — Blackboard / Memory / Artifact / ContextSource; `ContextScope` axis
- [ADR-012 Storage contract and Position C](ADR-012-storage-contract-position-c.md) — framework owns contract + test suite; reference impls are starting points; production = BYO provider
- [ADR-013 Memory kernel vs pipeline](ADR-013-memory-kernel-vs-pipeline.md) — `api.Memory` is storage interface only; extraction pipelines live in `recipe/`
- [ADR-014 Agent ontology stance](ADR-014-agent-ontology-stance.md) — accept structural identity (`Status`, `PreviousVersionID`, `RunSelector`, `hydaelyn.self.*` reserved); reject metaphysical identity (no `Agent.Self`, no `UpdateOwnProfile`, no auto-derived personality)

All six v0.8.0 ADRs are locked before Phase 1 implementation begins. Each carries an explicit list of anti-patterns it exists to reject, so PR review can cite a concrete clause rather than re-debate the principle.

## Convention

All ADRs MUST be written in English. Conversation language with users may be Chinese, but the artifact in the repo is English.

## Format

Each v0.8.0 ADR follows this structure:

1. **Status** — Accepted; date and the spec docs that anchor it.
2. **Context** — what was broken or missing that this ADR addresses.
3. **Decision** — the concrete rule (with code or type signatures where useful).
4. **Anti-patterns rejected by this ADR** — named patterns reviewers may cite by name.
5. **Impact** — what this enables, what it forecloses.
6. **References** — links to anchor specs, companion ADRs, and any external designs cited.
