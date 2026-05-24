# 00 — Overview

## Positioning

> Hydaelyn is a general-purpose, durable, governed Agent runtime for Go.

Three load-bearing words:

- **General-purpose**: kernel has zero domain vocabulary. Any agent shape (chat, tool-using, planner-executor, DAG workflow, multi-agent, human-in-the-loop, background, reactive) compiles down to the same primitives (Run/Task/Event/Lease/Command/Policy/Report/Outbox).
- **Durable**: every state change is a command, every command is replayable, every long-running task can be acquired by lease and resumed by another worker.
- **Governed**: every side effect is auditable, every high-risk action passes policy, every approval flow has a resume token, every usage is metered.

## Why v0.8.0 exists

The runtime kernel is already solid. v0.4.x → v0.7.x established:

- Run/Task state machine with command bus
- Blackboard with subscribe/wait/selector
- Mailbox with fan-out + lease + ack/dead-letter
- Handoff protocol with cycle detection
- Approval protocol with ResumeToken
- Trace span lifecycle
- Tool contract with PolicyEngine hook
- DAG await semantics (All/Any/Quorum)
- Framework-vs-business boundary enforced by CI (ADR-008)

What's missing for v0.8.0:

1. **Public API still has lies**: `Flow.Bypass*` fields exist but RegisterFlow rejects them; some `ExecuteCommand` paths return `[]any`.
2. **No declarative agent surface**: Tools are runnable but cannot be exported as schema to MCP / OpenAPI / LLM tool definitions without ad-hoc glue.
3. **No worker runtime**: `worker.AgentWorker.ExecuteEnvelope` runs a single envelope; nothing polls, retries, dead-letters, or shuts down gracefully.
4. **No durable storage contract**: only an in-memory provider; no documented `StoreProvider` contract for production teams to implement against; no contract test suite for adapter authors.
5. **No usage metering or budget control**: agents can spend unbounded tokens / tool calls / time.
6. **Policy obligations aren't enforced**: `PolicyDecision.Obligations` is a slice no one reads.
7. **Context model is incomplete**: Blackboard exists, but no Memory (long-term, agent/user scoped), Artifact (large blobs), or ContextSource (external references).
8. **No evaluation framework**: examples exist, structured eval doesn't.
9. **No contract test suites for ecosystem adapters**.

v0.8.0 closes all nine gaps.

## Scope

In:

- Public API hardening (doc 01)
- Capability layer (doc 02)
- AgentProfile extension + Registry (doc 03)
- Worker Runtime + Trigger Runtime (doc 04)
- Storage **contract** (primary surface) + contract test suite + memory/sqlite/mysql/postgres reference implementations (doc 05)
- Usage / Budget + Policy Obligation Enforcer (doc 06)
- Context Layer four-tier model (doc 07)
- Evaluation framework (doc 08)
- Architecture boundaries doc + ADR-009..012 (doc 09)
- Package restructure (doc 10)

Out:

- New domain-specific packs (research/devops/customer-support) — v1.0.0 or later
- v1.0.0 SemVer commitment — v1.0.0 explicitly
- Hosted observability backends (OpenTelemetry exporter integrations) — v0.9.0 minimum, `observe/otel/` skeleton may land but is optional
- Provider-specific deep integrations (Anthropic Files API, OpenAI Realtime, etc.) — out of framework scope
- UI / Console — out of framework scope

## Breaking changes summary

Total: 4 mechanical changes to public API. All grouped in Phase 1.

1. `api.Flow`: removed `BypassTaskStore`, `BypassPolicyEngine`, `BypassTaskExecutionLease`, `BypassHandoff`, `BypassResponseLayer`, `BypassOutputGateway`.
2. `api.ErrFlowBypass` removed.
3. `Runner.ExecuteCommand(StartRunCommand)` now returns `api.StartRunResult` (was `[]any{Run, RootTask}`).
4. `Runner.ExecuteCommand(RequestApprovalCommand)` now returns `api.RequestApprovalResult` (was `[]any{Approval, Token}`).

No other public types are removed or renamed in v0.8.0. All new fields on `api.AgentProfile` are `omitempty` and additive.

Package path moves (also breaking):

- `internal/memory` stays internal — earlier drafts moved it to `storage/memory`; per ADR-012 (revised, Position D) that move is withdrawn.
- New top-level packages: `memory/`, `artifact/`, `eval/`, `recipe/`, `contract/`, `packs/`, `transport/openapi/`, `transport/webhook/`, `transport/scheduler/`, `transport/event/`, `observe/otel/`. `storage/` does NOT exist — the framework ships no `api.StoreProvider` implementation.

## Theme statement (for README and release notes)

> Hydaelyn v0.8.0 — *Public Framework Release*. v0.8.0 ships the abstractions every Go agent application needs: declarative Capability manifests, an extended AgentProfile with model and governance fields, a complete Worker Runtime, a **storage contract** (`api.StoreProvider` + `contract.RunStoreProviderContractTests`) that applications implement against their own data stack, usage metering, policy obligation enforcement, and a four-layer context model. The runtime kernel remains domain-free; everything new is mechanism, not policy. Per ADR-012 (revised, Position D) the framework ships no `api.StoreProvider` implementation.

## Open design defaults (locked unless rejected)

These were AI-defaulted to minimize risk; flag any you want to override:

| # | Topic | Default |
|---|-------|---------|
| 1 | `UsageRecord.Credits` semantic | Abstract integer cost unit; framework records it but does not assign a meaning. Adapters/packs define conversion to currency or token-equivalent. |
| 2 | Storage path | **Position D (ADR-012 revised)**: framework ships the `api.StoreProvider` contract and `contract.RunStoreProviderContractTests` only. No reference implementations, public or otherwise — `storage/` does not exist. Applications implement the contract against their own data stack (ent / gorm / sqlc / DBA-controlled DDL). See doc 05 and doc 12 for the ent-based template. |
| 3 | `Memory[T]` interface | Define the generic `Memory[T Identified]` optional-plugin contract in `api/memory.go`. The framework ships no reference implementation — applications either implement against their existing database / ORM or skip the interface entirely. |
| 4 | `AgentSelector` filter fields | `IDs []string`, `Roles []string`, `Groups []string`, `Tags []string`, `Version string`, `Capabilities []string`. All optional, AND-combined. |
| 5 | `Trigger.Filter` shape | `map[string]string` for v0.8.0. Adapters parse semantics. Reserved escape hatch: a future `FilterRaw json.RawMessage` may be added without breaking. |
| 6 | `Credits` unit precision | `int64`, intended as "milli-credits" or whatever the adapter defines. No floating point. |

## Read order

1. `09-boundaries.md` — the principles enforced by everything else
2. `01-public-api.md` → `02-capability.md` → `03-agent-profile.md` — the public surface in dependency order
3. `04-execution-layer.md` → `05-storage.md` — the runtime layer
4. `06-governance.md` → `07-context.md` → `08-evaluation.md` — cross-cutting concerns
5. `10-package-structure.md` → `11-rollout-plan.md` → `12-migration-guide.md` — execution
