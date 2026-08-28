# ADR Queue

Architecture Decision Records, landed by milestone.

## Pre-v0.8.0 (landed)

- [ADR-001 `Profile` vs `AgentInstance` separation](ADR-001-profile-agent-instance-separation.md)
- [ADR-002 Task DAG and `FailurePolicy`](ADR-002-task-dag-failure-policy.md)
- [ADR-003 Plugin Registry and lifecycle](ADR-003-plugin-registry-lifecycle.md)
- [ADR-004 Middleware order and short-circuit](ADR-004-middleware-order-and-short-circuit.md)
- [ADR-005 `CapabilityInvoker` governance layer](ADR-005-capability-invoker-governance-layer.md)
- [ADR-006 Blackboard / Evidence data model](ADR-006-blackboard-evidence-model.md) — **superseded**; live path is generic `BlackboardItem`
- [ADR-007 EventStore and replay semantics](ADR-007-eventstore-replay-semantics.md)
- [ADR-008 Framework vs business boundary](ADR-008-framework-vs-business.md)

ADR-001 through ADR-008 were originally authored in Chinese (before the English-only docs convention was adopted) and have since been translated to English to match the rest of the repository.

## v0.8.0 (complete)

- [ADR-009 Capability Public API](ADR-009-capability-public-api.md) — declaration vs execution vs enforcement; MCP export ships; OpenAPI / CLI / LLM tool def remain Deferred; reserved `hydaelyn.self.*` namespace; 2026-08-18 amendment keeps `map[string]any` schemas and `RequiresApproval`
- [ADR-010 Usage, Budget, and Policy composition](ADR-010-usage-budget-policy-composition.md) — measurement (`UsageRecord`) feeds enforcement (`BudgetPolicy` ⊆ `PolicyEngine`); conditional authorization via `PolicyEnforcer` obligations
- [ADR-011 Context four-layer model](ADR-011-context-four-layer-model.md) — Blackboard / Memory / Artifact / ContextSource; `ContextScope` axis
- [ADR-012 Storage contract and Position D](ADR-012-storage-contract-position-d.md): framework owns contract verbs and conformance tests; applications own schema and implementation; no reference backend ships
- [ADR-013 Memory kernel vs pipeline](ADR-013-memory-kernel-vs-pipeline.md) — `api.Memory` is storage interface only; extraction pipelines live in `recipe/`
- [ADR-014 Agent ontology stance](ADR-014-agent-ontology-stance.md) — accept structural identity (`Status`, `PreviousVersionID`, `RunSelector`, `hydaelyn.self.*` reserved); reject metaphysical identity (no `Agent.Self`, no `UpdateOwnProfile`, no auto-derived personality)

All six v0.8.0 ADRs are locked before Phase 1 implementation begins. Each carries an explicit list of anti-patterns it exists to reject, so PR review can cite a concrete clause rather than re-debate the principle.

## v0.8 reconstruction (landed)

- [ADR-015 Strong bounded agent loop](ADR-015-strong-bounded-agent-loop.md)
- [ADR-016 Explicit multi-agent scheduler](ADR-016-explicit-multi-agent-scheduler.md)
- [ADR-017 Durable runner boundary](ADR-017-durable-runner-boundary.md)
- [ADR-018 Self-sufficient agent layer](ADR-018-self-sufficient-agent-layer.md)

## v0.12.0 (complete)

- [ADR-019 Project Identity — Venat](ADR-019-project-identity-venat.md) — clean repository, module, root package, CLI, and local skill-directory cutover; preserved persisted skill wire identifiers

## v0.15.0 (architecture program)

- [ADR-020 v0.15 architecture program](ADR-020-v015-architecture-program.md) — order and scope for the structural repair
- [ADR-021 Memory surface unification](ADR-021-memory-surface-unification.md) — `api.Memory[T]` only; `memory/` deprecated
- [ADR-022 UnitOfWork capability split](ADR-022-unit-of-work-capability-split.md) — narrow store interfaces; composite remains
- [ADR-023 Dual-model convergence](ADR-023-dual-model-convergence.md) — `api` types become internal spec types; adapter deleted store-by-store
- [ADR-024 Five-layer architecture](ADR-024-five-layer-architecture.md) — worker integration layer; import seams
- [ADR-025 Runner façade slimming](ADR-025-runner-facade-slim.md) — typed methods, domain sub-façades, no-context helpers removed
- [ADR-026 Identity and collaboration types](ADR-026-identity-and-collaboration-types.md) — `api.AgentProfile` / `AgentDefinition`; persist Handoff and BlackboardEntry
- [ADR-027 Package map and ArtifactStore](ADR-027-package-map-and-artifact-store.md) — alias packages deprecated; ArtifactStore is contract-only

## v0.16.0 candidate

- ADR-016/020/021/025/026/027 amendments complete the compatibility windows,
  remove the redundant Flow/workflow and speculative DAG surfaces, and record
  the demand-driven public-surface cutover.
- [ADR-028 Agent harness and the session store](ADR-028-agent-harness-and-session.md) — **Experimental**; durable CAS lane ownership, bounded finalization, strict restore validation, and no tools/hooks/budgets until Engine convergence

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
