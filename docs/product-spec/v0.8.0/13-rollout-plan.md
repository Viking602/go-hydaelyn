# 13 — Rollout Plan (Phase 0 – Phase 5)

> Renumbered from `11-rollout-plan.md`. Restructured around the four-layer
> architecture. Phase 0 is new — it scaffolds the multiagent/ package
> and the agent/ extensions before any storage contract finalization.

## Phasing principle

Build bottom-up *within* each layer, top-down *between* layers — but
the multi-agent layer cannot exist without the agent/ extensions and
the new stores, so those go first.

Order:

```
Phase 0  →  agent/ extensions, multiagent/ skeleton, lints
Phase 1  →  Public API hardening (breaking changes batched)
Phase 2  →  Storage contract Position D + 3 new stores
Phase 3  →  multiagent/ reference Schedulers + Team API + Handoff/Blackboard
Phase 4  →  Pack skeletons, _examples, eval framework, demo
Phase 5  →  Documentation, ADR cross-linking, release prep
```

Each phase ends with `go test ./... && sentrux check && scripts/check-*` green.

---

## Phase 0 — Loop primitives + multiagent scaffolding (no breaking changes yet)

**Goal**: every type and interface the rest of v0.8.0 needs to compile against, no behavior changes to existing flows.

### 0.1 `agent/` extensions

- `agent/step.go` — `Step`, `StepDecision`, `StepPolicy`
- `agent/output_policy.go` — `OutputPolicy`, repair loop hook points (impl in 0.4)
- `agent/result.go` — `Result` (replaces ad-hoc return shapes used by current `internal/core` paths)
- `agent/tool_safety.go` — `ToolSafety`, `ToolPolicy`
- `agent/context_manager.go` — `ContextManager` interface
- `agent/failure.go` — `AgentFailure`, `FailureKind`
- `agent/loop_policy.go` — `LoopPolicy`

### 0.2 `multiagent/` skeleton

- `multiagent/class.go` — `AgentClass`
- `multiagent/instance.go` — `AgentInstance` + `ComputeInstanceID`
- `multiagent/team.go` — `Team`, `NewTeam`, `AddRole`, `UseScheduler`
- `multiagent/scheduler.go` — `Scheduler` interface, `TeamState`
- `multiagent/dispatch.go` — `Dispatch`
- `multiagent/handoff.go` — `Handoff` (with schema validation stub)
- `multiagent/blackboard.go` — helpers
- `multiagent/voting.go` — types only
- `multiagent/supervisor.go` — `SupervisorDecision`
- `multiagent/events.go` — event kinds

Impls in 0.4. Phase 0 only ships types and stubs that compile.

### 0.3 Lints + CI gates

- `scripts/check-public-any.sh` added; CI green against current main
- `scripts/check-business-words.sh` baseline updated for multi-agent exception list
- `.sentrux.toml` updated: `agent/` cannot import `multiagent/`; `multiagent/` cannot import `internal/`; runner cannot import `multiagent/`

### 0.4 Engine integration of Step/Result/OutputPolicy

- `agent.Engine.Run(ctx, api.Task, OutputPolicy) Result` becomes the canonical entry point
- `internal/core` Engine adapter wraps the existing loop and emits `Step` + `Result` + `EventStepCompleted` (new event kind)
- Repair loop wired: on schema validation failure, model is re-prompted with the validator's error message; bounded by `OutputPolicy.MaxRepairAttempts`

### 0.5 Multi-agent primitive vocabulary exception (ADR-008 revision)

- ADR-008 revision committed (already done in prior session work)
- `check-business-words.sh` exception list updated

**Exit gates**:

- `go build ./...` succeeds
- `go vet ./...` clean
- Sentrux passes new rules
- New `agent.Engine` API works on the existing flagship pack's path (regression test)

---

## Phase 1 — Public API hardening (breaking changes batched)

Anchor: `01-public-api.md`.

- Change 1: remove `Flow.Bypass*` (and remove `ErrFlowBypass`)
- Change 2: type `StartRunResult`
- Change 3: type `RequestApprovalResult`
- Change 4: extend `api.Task` with `Budget *TaskBudget`, `InputSchema`, `OutputSchema` (additive — no break)
- Change 5: doc `ExecuteCommand` as low-level escape hatch
- Changes 6-7: confirm `agent/` + `multiagent/` public surfaces compile cleanly (no signature shake-up beyond what Phase 0 introduced)
- Change 8: lint script for `[]any` returns in `api/`/`agent/`/`multiagent/`

**Exit gates**:

- All 4 breaking changes shipped behind a single tag
- Migration steps documented in `14-migration-guide.md`
- Adapters in `_examples/` updated

---

## Phase 2 — Storage contract Position D + 3 new stores

Anchor: `07-storage.md`.

- Add `HandoffStore` / `TeamStateStore` / `AgentInstanceStore` interfaces to `api/`
- Add stores to `UnitOfWork`
- Add corresponding capabilities to `StoreCapabilities` (none — required, not capability-gated)
- Build out `contract/handoffs`, `contract/teamstates`, `contract/agentinstances`
- Add `contract/integration/` with the three-surface resume tests
- Update `contract/internal/inmemfake` to implement the 3 new stores
- Confirm `contract.RunSuite(t, inmemfakeFactory)` passes end-to-end

**Exit gates**:

- `go test ./contract/...` green
- 18 store getters on `UnitOfWork`
- Integration resume tests pass

---

## Phase 3 — multiagent/ reference Schedulers + Team API + Handoff/Blackboard

Anchor: `05-multi-agent-layer.md`.

- `SequentialScheduler` — fixed dispatch order
- `RouterScheduler` — single-step routing based on first agent's TypedReport
- `SupervisorScheduler` — central supervisor agent dispatches and consumes TypedReports
- `Team.Start` / `Team.Resume` layering is unresolved in v0.8.x because
  `multiagent` must not import the durable runner; v0.9.0 owns the
  integration shape.
- Multi-agent Blackboard write/read helpers with required metadata enforcement
- v0.9.0 target: typed Handoff validation against receiving class
  `InputSchema`
- 6 multi-agent event kinds wired in v0.8.x; `EventHandoffPersisted` is
  reserved for the v0.9.0 HandoffStore-backed scheduler flow.
- `VotingResult`, `MajorityVote`, `QuorumVote` (types and helper functions; full Voting scheduler deferred to v0.9.0)

**Exit gates**:

- Unit tests for each Scheduler reference impl
- Resume kill-test passes for each reference Scheduler
- `multiagent/` builds with no `internal/` imports

---

## Phase 4 — Pack skeletons, _examples, eval framework, demo

- `packs/research`, `packs/support`, `packs/devops`, `packs/aiops` — `doc.go` + `README.md`
- `packs/aiops/incident-triage` — full demo per `16-multi-agent-demo.md`
- `eval/` framework with multi-agent assertions
- `_examples/multi-agent-incident-triage/` runs end-to-end against `contract/internal/inmemfake`
- `_examples/single-agent-research/` exercises Step trace + OutputPolicy + repair

**Exit gates**:

- `go test ./packs/...` green
- Demo eval suite passes
- `_examples/` build and (where deterministic) run in CI

---

## Phase 5 — Documentation, ADR cross-linking, release prep

- All 16 product-spec docs cross-linked
- All 17 ADRs cross-linked (ADR-001..017)
- `docs/architecture-boundaries.md` rebuilt from `11-boundaries.md`
- README rewrite reflecting the four-layer positioning
- v0.9.0 roadmap doc updated (Debate / MapReduce / DAG / Swarm
  scheduler strategies, hosted observability)
- Migration guide finalized (`14-migration-guide.md`)
- Release notes draft

**Exit gates**:

- README + CONTRIBUTING reference all six boundary principles
- `make docs-check` (link checker) green
- v0.8.0 tag ready

---

## Risk register

| Risk | Mitigation |
|------|-----------|
| Engine refactor breaks existing flagship pack path | Phase 0 keeps engine impl behind adapter; only the public type changes initially |
| `multiagent/` reference Schedulers ship buggy resume | Phase 2's integration suite must pass before Phase 3 starts |
| Migration guide miscounts breaking changes | Pin to the 4 in `01-public-api.md`; cross-reference each in `14-migration-guide.md` |
| Multi-agent vocabulary leaks into `api/` | `check-business-words.sh` exception list is the only allow channel; review by hand each phase |
| `internal/memory` confused with `memory/` interface | Distinct package paths; ADR-013 + `15-memory-optional-plugin.md` cite both explicitly |

## Parallelizable work

Phases 0 ↔ 1: independent type extensions (Phase 0) vs deletion +
typed return (Phase 1). Can land concurrently if reviewer bandwidth
allows.

Phases 2 ↔ 3: storage contract (Phase 2) can land in parallel with
`multiagent/` skeleton hardening (Phase 3) because they share no
files; integration tests in Phase 2 use stubs of the Phase 3
Schedulers.

Phase 4 work splits cleanly: eval framework, packs, demo are three
sub-streams.

## Verification per phase

Each phase ends with the same gate set:

```
go test ./...
go vet ./...
sentrux check
scripts/check-business-words.sh
scripts/check-public-any.sh
scripts/check-import-boundaries.sh
```

Plus the phase-specific exit gates listed above.
