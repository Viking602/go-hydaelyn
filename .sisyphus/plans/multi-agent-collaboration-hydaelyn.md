# Hydaelyn Multi-Agent Collaboration v1

## TL;DR
> **Summary**: Generalize Hydaelyn’s proven `deepsearch` shape into a reusable collaboration pattern for complex multi-agent work, while keeping the runtime supervisor-led and making reliability the first-class concern.
> **Deliverables**:
> - A new generalized collaboration pattern derived from `patterns/deepsearch`
> - Reliability contracts for blackboard writes, queued execution, verifier gating, and failure-policy-aware cancellation
> - Engineering workflow mapping implemented as planner-managed tasks on the same runtime substrate
> - Backward-compatible deepsearch regression coverage and observability for the new lifecycle
> **Effort**: Large
> **Parallel**: YES - 2 implementation waves + final verification wave
> **Critical Path**: 1 → 2 → 3 → 6 → 7 → 9 → 10

## Context
### Original Request
- 深度分析文章《Agentic Design Patterns》中的 Multi-Agent Collaboration，并针对当前 Hydaelyn 项目制定完整方案。

### Interview Summary
- Scope confirmed: cover both runtime collaboration and engineering workflow.
- Primary objective: reliability first, not raw throughput.
- Verification choice: extend the existing Go unit/integration testing style; do not introduce a heavy new test stack in v1.

### Metis Review (gaps addressed)
- Constrained v1 scope to a deepsearch-derived collaboration pattern, not a new orchestration platform.
- Made the missing contracts explicit: collaboration state machine, blackboard consistency, verifier authority, failure/cancellation matrix, and deepsearch compatibility.
- Added guardrails: no peer-to-peer agent mesh, no MCP-as-runtime, no separate verifier runtime/service, no workflow DSL rewrite.
- Added named failure-mode tests and observability requirements so acceptance is agent-executable and not vague.

## Work Objectives
### Core Objective
Ship a production-safe v1 collaboration model for Hydaelyn that preserves the current supervisor-led runtime, reuses `planner.Plan/Review/Replan`, `scheduler.TaskQueue`, and blackboard evidence exchange, and supports engineering-style phases as ordinary planner tasks rather than a second runtime.

### Deliverables
- New `patterns/collab` pattern based on the `deepsearch` execution shape.
- Versioned, namespaced collaboration contract for task outputs and blackboard exchanges.
- Queue/state reliability hardening to reject stale writes and prevent duplicate committed outputs.
- Explicit verifier gate that controls synthesis/finalization.
- Failure-policy-aware cancellation/replan semantics.
- Engineering workflow template that maps `plan -> implement -> review -> verify -> synthesize` into the existing runtime.
- Deepsearch compatibility coverage and rollout isolation.

### Collaboration State Machine
- `planned` -> planner emits collaboration tasks and runtime instantiates them.
- `leased/running` -> scheduler/runtime assigns or acquires executable tasks.
- `evidence_written` -> task publishes namespaced outputs to shared/blackboard destinations.
- `verified` -> verifier task publishes allow/block evidence for gated flows.
- `synthesized/finalized` -> supervisor/synthesis consumes only permitted keys and commits the final result.
- `cancelled/failed/superseded` -> failure policy or replan makes prior outputs non-authoritative; late results must be ignored.

### Default Assumptions Applied
- V1 includes one concrete engineering workflow template, not a reusable end-user workflow DSL.
- Deepsearch must remain behaviorally compatible and serve as the first adopter/reference pattern.
- Verifier gating is mandatory before final synthesis/finalization in guarded flows; it is not broadened to all tool actions in v1.
- Additional latency is acceptable when required to prevent duplicate commits, stale writes, or ungated synthesis.

### Definition of Done (verifiable conditions with commands)
- `go test ./...` exits `0` with no failing package.
- `go test ./host -run '^TestMultiAgentCollaboration_'` exits `0`.
- `go test ./patterns/... -run '^(TestCollabPattern_|TestDeepsearchCompatibility_)'` exits `0`.
- `go test ./storage ./blackboard -run '^(Test.*Version|Test.*Stale|Test.*Conflict)'` exits `0`.
- `go test ./host -run '^TestEngineeringWorkflow_'` exits `0`.

### Must Have
- Supervisor-led DAG remains the top-level orchestration model.
- Collaboration state transitions are explicit and observable.
- Blackboard writes are namespaced and conflict-aware.
- Queued execution remains idempotent under lease expiry / retry.
- Verifier outputs gate synthesis and finalization.
- Engineering workflow phases are planner tasks on the same runtime.
- Existing deepsearch behavior remains supported during rollout.

### Must NOT Have (guardrails, AI slop patterns, scope boundaries)
- No peer-to-peer free-form agent mesh.
- No MCP server/runtime orchestration substrate.
- No separate verifier runtime/service in v1.
- No generic workflow platform rewrite or new DSL before pattern validation.
- No manual-only acceptance criteria.

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: tests-after + existing Go stdlib `testing` / integration style.
- QA policy: Every task includes agent-executed happy-path and failure/edge scenarios.
- Evidence: `.sisyphus/evidence/task-{N}-{slug}.log` for command output; in-memory/assertion evidence stays inside named Go tests.

## Execution Strategy
### Parallel Execution Waves
> Target: 5 tasks in Wave 1, 5 tasks in Wave 2.

Wave 1: contract and reliability foundations (`team`, `planner`, `blackboard`, `storage`, `host`, `observe`)
Wave 2: collaboration behavior, engineering workflow mapping, backward compatibility (`patterns`, `host`, `planner`, `examples/tests`)

### Dependency Matrix (full, all tasks)
| Task | Depends On | Blocks |
|---|---|---|
| 1 | - | 2, 6, 8, 9 |
| 2 | 1 | 3, 6, 7, 9 |
| 3 | 2 | 4, 6, 10 |
| 4 | 3 | 7, 8, 10 |
| 5 | 1 | 7, 8, 10 |
| 6 | 1, 2, 3 | 7, 9, 10 |
| 7 | 2, 4, 5, 6 | 9, 10 |
| 8 | 1, 4, 5 | 10 |
| 9 | 1, 2, 6, 7 | 10 |
| 10 | 3, 4, 5, 6, 7, 8, 9 | Final Wave |

### Agent Dispatch Summary (wave → task count → categories)
- Wave 1 → 5 tasks → `ultrabrain` (contract-heavy), `unspecified-high` (runtime/storage), `deep` (observability/reliability)
- Wave 2 → 5 tasks → `deep` (pattern integration), `unspecified-high` (runtime behavior), `quick` (compatibility polish where safe)
- Final Verification Wave → 4 tasks → oracle / unspecified-high / deep

## TODOs
> Implementation + Test = ONE task. Never separate.
> EVERY task MUST have: Agent Profile + Parallelization + QA Scenarios.

- [x] 1. Freeze the v1 collaboration contract in `planner` and `team`

  **What to do**: Extend `planner.TaskSpec` and `team.Task` with the minimum metadata needed to represent engineering-style collaboration stages without introducing new top-level runtime phases. Define a stable v1 contract for task class, verifier requirements, publish namespace, and idempotency hints. Keep `PhasePlanning/Research/Verify/Synthesize` intact for v1; model `plan/implement/review/verify/synthesize` as task metadata over the existing runtime.
  **Must NOT do**: Do not rename `team.Phase`; do not create a second workflow engine; do not add a generic DSL or peer-mesh message model.

  **Recommended Agent Profile**:
  - Category: `ultrabrain` - Reason: this task sets the contract that all downstream reliability and pattern work depends on.
  - Skills: `[]` - No special skill is needed beyond precise type/runtime reasoning.
  - Omitted: `[think]` - Planning is already complete; executor should implement, not reopen design.

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 2, 6, 8, 9 | Blocked By: none

  **References** (executor has NO interview context - be exhaustive):
  - API/Type: `planner/planner.go:9-42` - existing `TaskSpec`, `Template`, `Plan`, and verification policy surface to extend without breaking planner flow.
  - API/Type: `planner/planner.go:45-85` - `PlanRequest`, `ReviewDecision`, and `Planner` lifecycle that must remain the supervisor entrypoint.
  - API/Type: `team/team.go:33-69` - existing phase / task kind / failure-policy enums; preserve compatibility.
  - API/Type: `team/team.go:145-187` - runtime `Task` and `RunState` fields to augment for collaboration metadata and version-safe execution.
  - Pattern: `docs/task-dataflow.md:16-31` - existing Reads/Writes/Publish contract to extend rather than replace.
  - Pattern: `docs/plugin-development.md:29-45` - planner output must still compile through `planner -> team -> host`; no separate orchestration engine.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `planner.TaskSpec` and `team.Task` can express collaboration stage metadata, namespace, verifier requirements, and idempotency hints without requiring new runtime phases.
  - [ ] Existing deepsearch plan/task construction continues to compile unchanged or with a compatibility-normalized default path.
  - [ ] `go test ./planner ./team ./host -run '^(TestPlanTasksCarryCollaborationMetadata|TestBuildPlannedStatePreservesCollaborationContract)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path contract propagation
    Tool: Bash
    Steps: run `go test ./planner ./team ./host -run '^TestPlanTasksCarryCollaborationMetadata$'`
    Expected: exit code 0; generated runtime tasks preserve metadata such as stage, namespace, and verifier requirement.
    Evidence: .sisyphus/evidence/task-1-collaboration-contract.log

  Scenario: Compatibility defaulting
    Tool: Bash
    Steps: run `go test ./host -run '^TestBuildPlannedStatePreservesCollaborationContract$'`
    Expected: exit code 0; legacy tasks without the new metadata still normalize into a valid runtime contract.
    Evidence: .sisyphus/evidence/task-1-collaboration-contract-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `planner/planner.go`, `team/team.go`, matching tests

- [x] 2. Add namespaced, conflict-aware blackboard exchange rules

  **What to do**: Introduce a collaboration publish contract for blackboard exchanges so engineering tasks write only namespaced keys such as `impl.<task>`, `review.<task>`, `verify.<task>`, and synthesis/verifiers consume only the allowed keys. Add version/conflict data to exchange publication so stale or superseded writes can be rejected deterministically. Ensure verifier-published keys remain the only source for synthesis in guarded flows.
  **Must NOT do**: Do not allow arbitrary free-form exchange keys from worker output; do not let synthesis read raw implementation outputs when verifier gating is required.

  **Recommended Agent Profile**:
  - Category: `ultrabrain` - Reason: this defines the shared-state contract and controls correctness under concurrency.
  - Skills: `[]` - The task is domain-specific to Hydaelyn’s blackboard/dataflow model.
  - Omitted: `[hunt]` - This is greenfield contract hardening, not root-cause debugging.

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 3, 6, 7, 9 | Blocked By: 1

  **References** (executor has NO interview context - be exhaustive):
  - API/Type: `blackboard/blackboard.go:96-118` - `State`, `PublishRequest`, and `PublishResult` are the persistence surface to extend.
  - Pattern: `host/runtime_blackboard.go:12-79` - current publish/update path for research and verify tasks; generalize this without breaking supported findings.
  - Pattern: `host/runtime_blackboard.go:97-123` - `exchangeForTaskOutput` currently infers output types; use it as the central place to attach namespace/version metadata.
  - Pattern: `host/runtime_dataflow.go:21-55` - current input materialization and `supported_findings` behavior; use the same mechanism for gated collaboration keys.
  - Pattern: `docs/task-dataflow.md:49-95` - blackboard exchanges are already the generic task exchange surface and deepsearch already reads `supported_findings`.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Collaboration tasks publish only namespaced keys and blackboard rejects stale/conflicting writes by contract.
  - [ ] Verifier-gated synthesis reads verifier-approved keys rather than raw implementation/review keys.
  - [ ] `go test ./blackboard ./host -run '^(TestCollaborationBlackboard_RejectsStaleExchangeWrite|TestCollaborationBlackboard_RequiresVerifierNamespaces)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path namespaced publication
    Tool: Bash
    Steps: run `go test ./host -run '^TestCollaborationBlackboard_RequiresVerifierNamespaces$'`
    Expected: exit code 0; synthesis-visible reads resolve only verifier-approved namespaced keys.
    Evidence: .sisyphus/evidence/task-2-blackboard-contract.log

  Scenario: Stale write rejection
    Tool: Bash
    Steps: run `go test ./blackboard ./host -run '^TestCollaborationBlackboard_RejectsStaleExchangeWrite$'`
    Expected: exit code 0; stale publication attempt returns an explicit conflict and leaves the authoritative exchange unchanged.
    Evidence: .sisyphus/evidence/task-2-blackboard-contract-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `blackboard/blackboard.go`, `host/runtime_blackboard.go`, `host/runtime_dataflow.go`, matching tests

- [x] 3. Add compare-and-swap semantics for persisted team state

  **What to do**: Add version/CAS semantics to persisted team state so queued workers cannot overwrite newer team snapshots after lease expiry, replan, or retry. Implement memory-driver support first and thread the version checks through runtime save/persist helpers used by queued execution.
  **Must NOT do**: Do not solve this with coarse global locks across the whole runtime; do not silently last-write-wins queued state.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: it is invasive storage/runtime work but bounded to concrete persistence semantics.
  - Skills: `[]` - Existing repo patterns are sufficient.
  - Omitted: `[check]` - Post-implementation review belongs to the final wave, not this task itself.

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 4, 6, 10 | Blocked By: 2

  **References** (executor has NO interview context - be exhaustive):
  - API/Type: `storage/storage.go:47-51` - `TeamStore` currently exposes plain `Save/Load/List`; evolve this to support version-aware writes.
  - Pattern: `storage/storage.go:109-150` - memory driver is the first implementation target and current baseline.
  - Pattern: `host/runtime_queue.go:58-72` - queued task execution persists updated team state after task completion; stale state must be rejected here.
  - Pattern: `host/runtime_queue.go:165-177` - queued execution reloads team state from storage; tie version checks to this path.
  - Pattern: `host/runtime.go:577-596` - `driveTeam` loop persists team snapshots and must remain compatible.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `team.RunState` persistence has an explicit version/CAS path for queued mutation.
  - [ ] A stale queued worker cannot overwrite a newer saved team snapshot.
  - [ ] `go test ./storage ./host -run '^(TestTeamStoreCompareAndSwap|TestQueuedStatePersistRejectsStaleVersion)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path CAS save
    Tool: Bash
    Steps: run `go test ./storage -run '^TestTeamStoreCompareAndSwap$'`
    Expected: exit code 0; save with the expected version succeeds and increments the persisted version.
    Evidence: .sisyphus/evidence/task-3-storage-cas.log

  Scenario: Stale queued persistence
    Tool: Bash
    Steps: run `go test ./host -run '^TestQueuedStatePersistRejectsStaleVersion$'`
    Expected: exit code 0; outdated queued worker save is rejected and the newer state remains authoritative.
    Evidence: .sisyphus/evidence/task-3-storage-cas-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `storage/storage.go`, queued/runtime save helpers, matching tests

- [x] 4. Harden queue lease handling against duplicate committed outputs

  **What to do**: Make queued execution idempotent when leases expire, retries race, or the same task is re-acquired. Ensure completion, publication, and final commit are single-authoritative even if work is executed more than once. Tie release/heartbeat/result persistence to the new CAS semantics instead of optimistic last-write-wins.
  **Must NOT do**: Do not assume lease release alone prevents duplicate commit; do not let re-acquired tasks append duplicate blackboard outputs or final results.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: runtime queue semantics are concrete but require careful concurrency handling.
  - Skills: `[]` - Existing queue/runtime patterns provide enough local guidance.
  - Omitted: `[hunt]` - This is proactive hardening, not reactive debugging.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 7, 8, 10 | Blocked By: 3

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `host/runtime_queue.go:12-117` - lease TTL, acquire/heartbeat/release, and queued execution flow.
  - Pattern: `host/runtime_queue.go:165-180` - queued state loading and task execution entrypoint.
  - Pattern: `host/runtime.go:598-629` - task outcomes are applied, lifecycle events recorded, and blackboard updates published here.
  - Test: `host/runtime_scheduler_integration_test.go` - existing scheduler integration coverage to extend rather than bypass.
  - Test: `host/runtime_distributed_test.go` - distributed/queued behavior test surface to reuse for duplicate-execution cases.

  **Acceptance Criteria** (agent-executable only):
  - [ ] A task may be re-executed after lease expiry, but only one authoritative completion/publish path is committed.
  - [ ] Duplicate queued execution does not create duplicate final outputs, claims, or exchanges.
  - [ ] `go test ./host -run '^(TestMultiAgentCollaboration_LeaseExpiryDoesNotDuplicateCommit|TestMultiAgentCollaboration_QueuedRetryIsIdempotent)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path queued completion
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_QueuedRetryIsIdempotent$'`
    Expected: exit code 0; retried queued execution produces one committed completion and one authoritative publish set.
    Evidence: .sisyphus/evidence/task-4-queue-idempotency.log

  Scenario: Lease expiry duplicate execution
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_LeaseExpiryDoesNotDuplicateCommit$'`
    Expected: exit code 0; second worker cannot create a duplicate commit after lease expiry race.
    Evidence: .sisyphus/evidence/task-4-queue-idempotency-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `host/runtime_queue.go`, `host/runtime.go`, matching queue/integration tests

- [x] 5. Emit collaboration lifecycle observability at every critical transition

  **What to do**: Add structured lifecycle events/counters/log attributes for lease acquired, heartbeat expired, stale write rejected, verifier passed/blocked, cancellation propagated, and synthesis committed. Reuse the existing observer/middleware/event path so the new collaboration pattern is debuggable without building a new telemetry system.
  **Must NOT do**: Do not add ad hoc logging only in one code path; do not emit events with no team/task correlation identifiers.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this crosses host, observe, and middleware boundaries and must stay coherent.
  - Skills: `[]` - The repository already has the required observer patterns.
  - Omitted: `[design]` - No UI work is involved.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 7, 8, 10 | Blocked By: 1

  **References** (executor has NO interview context - be exhaustive):
  - API/Type: `middleware/middleware.go:49-97` - middleware chain is the preferred cross-cutting integration surface.
  - Pattern: `host/runtime_events.go` - existing task/team event emission path to extend for collaboration-specific transitions.
  - Test: `host/runtime_observe_test.go:43-79` - baseline observer test proving counters/spans capture runtime activity.
  - Test: `host/runtime_observe_test.go:81-121` - logs must include trace/correlation identifiers on denied/error paths.
  - API/Type: `storage/storage.go:68-98` - event types and event store are already available for lifecycle recording.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Collaboration lifecycle events are emitted with team/task/trace correlation for queue, verifier, cancel, and finalize transitions.
  - [ ] Observer-based tests prove counters/logs/spans are emitted for collaboration flows and conflict paths.
  - [ ] `go test ./host -run '^(TestMultiAgentCollaboration_EmitsLifecycleObservability|TestMultiAgentCollaboration_LogsConflictTraceContext)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path lifecycle telemetry
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_EmitsLifecycleObservability$'`
    Expected: exit code 0; spans/counters/logs include lease, publish, verifier, and finalization transitions.
    Evidence: .sisyphus/evidence/task-5-observability.log

  Scenario: Conflict/error telemetry
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_LogsConflictTraceContext$'`
    Expected: exit code 0; stale write / cancellation / verifier block paths emit trace-linked log data.
    Evidence: .sisyphus/evidence/task-5-observability-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `host/runtime_events.go`, observer/middleware wiring, matching tests

- [x] 6. Implement `patterns/collab` as the generalized collaboration seed pattern

  **What to do**: Create a new `patterns/collab` package by extracting the reusable control shape from `patterns/deepsearch`: supervisor start, branch task creation, optional verifier stage, and final synthesis. Keep the runtime generic and let the new pattern specialize task generation/metadata for collaboration flows. Preserve the deepsearch reference path instead of mutating it into an incompatible abstraction.
  **Must NOT do**: Do not fork the runtime into “deepsearch runtime” vs “collaboration runtime”; do not break direct `deepsearch.New()` registration.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this is the central architectural bridge between the article pattern and the repo’s existing runtime.
  - Skills: `[]` - No external skill is needed if the executor follows existing pattern structure closely.
  - Omitted: `[think]` - Decisions are already fixed; implement the chosen pattern shape.

  **Parallelization**: Can Parallel: NO | Wave 2 | Blocks: 7, 9, 10 | Blocked By: 1, 2, 3

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `patterns/deepsearch/deepsearch.go:23-56` - `PlanTemplate` shows how a pattern seeds planner task hints.
  - Pattern: `patterns/deepsearch/deepsearch.go:58-168` - current start/advance control loop is the concrete model to generalize.
  - Pattern: `host/runtime_planning.go:27-97` - planned team startup and runtime task construction path the new pattern must fit.
  - API/Type: `team/team.go:199-203` - `team.Pattern` interface contract.
  - Pattern: `README.md` - states deepsearch is the first multi-agent pattern and MCP is not the core runtime model; the new pattern must respect that design direction.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `patterns/collab` registers as a normal Hydaelyn pattern and uses the same runtime `StartTeam()/Advance()` path.
  - [ ] The new pattern can create branch tasks, verifier tasks, and synthesis tasks without modifying runtime phase semantics.
  - [ ] `go test ./patterns/... -run '^(TestCollabPattern_StartBuildsPlannedCollaboration|TestCollabPattern_AdvanceCreatesVerifierAndSynthesis)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path collaboration pattern
    Tool: Bash
    Steps: run `go test ./patterns/... -run '^TestCollabPattern_StartBuildsPlannedCollaboration$'`
    Expected: exit code 0; the new pattern creates supervisor/worker state and collaboration tasks with the expected metadata.
    Evidence: .sisyphus/evidence/task-6-collab-pattern.log

  Scenario: Verify/synthesize stage transition
    Tool: Bash
    Steps: run `go test ./patterns/... -run '^TestCollabPattern_AdvanceCreatesVerifierAndSynthesis$'`
    Expected: exit code 0; completed branch work advances into verifier and synthesis stages without changing the global runtime model.
    Evidence: .sisyphus/evidence/task-6-collab-pattern-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `patterns/collab/*`, shared tests if needed, no breaking rename of `patterns/deepsearch/*`

- [x] 7. Make verifier outputs the only synthesis gate in guarded collaboration flows

  **What to do**: Implement the verifier contract so verifier tasks consume published branch outputs, publish structured pass/fail evidence, and explicitly block synthesis/finalization when required evidence is missing or contradicted. Engineering-style tasks must route through this same gate. Reuse the existing supported-findings logic as the compatibility anchor, but generalize it to collaboration namespaces.
  **Must NOT do**: Do not let synthesis read implementation outputs directly when verification is required; do not bury verifier logic inside ad hoc pattern code.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this task connects runtime dataflow, blackboard, observability, and pattern behavior.
  - Skills: `[]` - Existing repo primitives are sufficient.
  - Omitted: `[check]` - Final review happens later after the full slice is integrated.

  **Parallelization**: Can Parallel: NO | Wave 2 | Blocks: 9, 10 | Blocked By: 2, 4, 5, 6

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `host/runtime_blackboard.go:40-78` - current verify-task handling, supported findings publication, and output exchange writes.
  - Pattern: `host/runtime_dataflow.go:21-55` - materialized reads and the current special-case supported findings behavior.
  - API/Type: `blackboard/blackboard.go:96-118` - verification results and exchange state live here.
  - Pattern: `patterns/deepsearch/deepsearch.go:121-157` - current verify/synthesize phase sequencing to preserve conceptually.
  - Test: `host/runtime_blackboard_test.go` - existing verification-aware synthesis behavior should be extended, not bypassed.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Required verifier tasks publish explicit allow/block evidence that synthesis respects.
  - [ ] Missing evidence, verifier failure, or contradiction prevents final synthesis from committing.
  - [ ] `go test ./host -run '^(TestMultiAgentCollaboration_VerifierBlocksSynthesisOnMissingEvidence|TestMultiAgentCollaboration_VerifierPublishesSynthesisGate)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path verifier-approved synthesis
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_VerifierPublishesSynthesisGate$'`
    Expected: exit code 0; verifier publishes the gate evidence and synthesis completes only after that evidence is available.
    Evidence: .sisyphus/evidence/task-7-verifier-gate.log

  Scenario: Missing evidence blocks synthesis
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_VerifierBlocksSynthesisOnMissingEvidence$'`
    Expected: exit code 0; synthesis remains blocked or unscheduled until required verifier evidence exists.
    Evidence: .sisyphus/evidence/task-7-verifier-gate-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: verifier-related host/dataflow/blackboard logic and matching tests

- [x] 8. Make cancellation and replanning respect explicit failure policy

  **What to do**: Implement a per-task failure-policy matrix (`fail_fast`, `retry`, `degrade`, `skip_optional`) that controls sibling cancellation, retry scheduling, and replan eligibility. Ensure parent/child cancellation, optional branches, and post-replan stale results all resolve deterministically. Keep planner `Review/Replan` as the only authority for global route changes.
  **Must NOT do**: Do not cancel all siblings on every failure; do not let workers mutate global plan semantics outside planner review/replan.

  **Recommended Agent Profile**:
  - Category: `ultrabrain` - Reason: failure-policy semantics are subtle and directly tied to reliability.
  - Skills: `[]` - Existing runtime/planner surfaces are enough.
  - Omitted: `[hunt]` - Executor is implementing defined policy rules, not investigating an unknown bug.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 10 | Blocked By: 1, 4, 5

  **References** (executor has NO interview context - be exhaustive):
  - API/Type: `team/team.go:62-69` - existing failure-policy enum values to honor and extend behavior for.
  - Pattern: `host/runtime.go:598-629` - task outcome processing and lifecycle recording where status transitions are applied.
  - Pattern: `host/runtime_planning.go:40-54` - planner-generated plans enter runtime here; keep planner authority central.
  - API/Type: `planner/planner.go:57-85` - `ReviewActionContinue/Complete/Replan/Abort/AskHuman/Escalate` are the only valid global control decisions.
  - Test: `host/runtime_abort_test.go`, `host/runtime_planner_test.go`, `host/runtime_dag_test.go` - extend existing cancellation / DAG / replanning coverage.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Fail-fast branches cancel the correct dependents, while degrade/skip-optional branches preserve unrelated work.
  - [ ] Replan can supersede stale outputs without letting late results re-open cancelled work.
  - [ ] `go test ./host -run '^(TestMultiAgentCollaboration_CancelsChildrenByFailurePolicy|TestMultiAgentCollaboration_ReplanRejectsLateSupersededResult)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path failure policy routing
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_CancelsChildrenByFailurePolicy$'`
    Expected: exit code 0; fail-fast tasks cancel only the correct dependents and best-effort branches remain eligible when policy allows.
    Evidence: .sisyphus/evidence/task-8-failure-policy.log

  Scenario: Late stale result after replan
    Tool: Bash
    Steps: run `go test ./host -run '^TestMultiAgentCollaboration_ReplanRejectsLateSupersededResult$'`
    Expected: exit code 0; a late result from a superseded branch is ignored and cannot mutate authoritative state.
    Evidence: .sisyphus/evidence/task-8-failure-policy-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: runtime/planner cancellation logic and matching tests

- [x] 9. Map engineering workflow phases onto planner-managed collaboration tasks

  **What to do**: Add an engineering workflow template over the new collaboration pattern so `plan -> implement -> review -> verify -> synthesize` is expressed as planner tasks with explicit Reads/Writes/Publish and verifier rules. Reuse normal agent roles/profiles and task metadata rather than inventing a separate workflow runtime. Scope v1 to one concrete engineering flow template; defer generalized workflow authoring APIs.
  **Must NOT do**: Do not build a new workflow engine, YAML DSL, or user-facing universal config system in v1; do not bypass `planner.Plan/Review/Replan`.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this task maps the article’s collaboration pattern into the repo’s most strategic product-facing use case.
  - Skills: `[]` - The repository already exposes the required planner/task surfaces.
  - Omitted: `[design]` - No visual workflow builder is part of scope.

  **Parallelization**: Can Parallel: NO | Wave 2 | Blocks: 10 | Blocked By: 1, 2, 6, 7

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `host/runtime_planning.go:27-97` - planned-team startup and template-to-runtime conversion path.
  - Pattern: `docs/plugin-development.md:29-45` - planner plugins emit task-level dataflow and must stay on the existing runtime path.
  - Pattern: `docs/task-dataflow.md:65-95` - task-level Reads/Writes/Publish/replay flow and deepsearch’s current use of explicit dataflow.
  - API/Type: `planner/planner.go:14-42` - task spec and plan metadata surface for workflow templates.
  - Pattern: `team/team.go:145-187` - runtime task/run state fields to populate for engineering workflow steps.

  **Acceptance Criteria** (agent-executable only):
  - [ ] A planner-managed engineering workflow can be built entirely from collaboration tasks on the existing runtime.
  - [ ] Implement/review/verify phases use explicit dataflow keys and verifier gates rather than hidden shared context.
  - [ ] `go test ./host ./planner -run '^(TestEngineeringWorkflow_MapsPhasesToPlannerTasks|TestEngineeringWorkflow_UsesExplicitDataflowAndVerifierGates)$'` exits `0`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path engineering workflow mapping
    Tool: Bash
    Steps: run `go test ./host ./planner -run '^TestEngineeringWorkflow_MapsPhasesToPlannerTasks$'`
    Expected: exit code 0; the engineering flow is produced as planner tasks over the collaboration pattern with no alternate runtime.
    Evidence: .sisyphus/evidence/task-9-engineering-workflow.log

  Scenario: Missing explicit dataflow
    Tool: Bash
    Steps: run `go test ./host ./planner -run '^TestEngineeringWorkflow_UsesExplicitDataflowAndVerifierGates$'`
    Expected: exit code 0; hidden context or ungated synthesis is rejected by the workflow contract.
    Evidence: .sisyphus/evidence/task-9-engineering-workflow-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: planner template/plugin wiring, collaboration pattern integration, matching tests

- [x] 10. Preserve deepsearch behavior and isolate rollout of the generalized pattern

  **What to do**: Keep `deepsearch` behaviorally intact while introducing the generalized collaboration pattern. Add regression tests that prove deepsearch still performs research -> verify -> synthesize correctly, then add isolated registration / example wiring for the new collaboration pattern so adopters can opt in without migrating existing teams immediately.
  **Must NOT do**: Do not silently change deepsearch semantics; do not force existing callers to adopt collaboration metadata before the compatibility layer exists.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: this is integration-heavy compatibility work with broad test impact.
  - Skills: `[]` - Standard repo/test patterns are enough.
  - Omitted: `[write]` - Natural-language polish is secondary to executable compatibility proof.

  **Parallelization**: Can Parallel: NO | Wave 2 | Blocks: Final Wave | Blocked By: 3, 4, 5, 6, 7, 8, 9

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `patterns/deepsearch/deepsearch.go:58-168` - preserve current deepsearch sequencing and verification behavior.
  - Pattern: `README.md` - deepsearch is documented as the current first pattern; collaboration must be additive.
  - Test: `host/runtime_team_test.go`, `host/runtime_scheduler_integration_test.go`, `host/runtime_blackboard_test.go`, `host/runtime_planner_test.go` - extend the existing test matrix instead of inventing a new one.
  - Example: `examples/research/main.go` - preserve the current example path while adding an isolated collaboration example/registration path.
  - Docs: `docs/public-api.md` - update only if exported API or registration guidance changes.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Existing deepsearch tests still pass and new compatibility tests prove no unintended semantic break.
  - [ ] The generalized collaboration pattern is opt-in and can be registered without changing existing deepsearch callers.
  - [ ] `go test ./... -run '^(TestDeepsearchCompatibility_|TestMultiAgentCollaboration_)'` exits `0`.
  - [ ] `go build ./examples/...` exits `0` if a new example or registration path is added.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Happy path deepsearch compatibility
    Tool: Bash
    Steps: run `go test ./... -run '^TestDeepsearchCompatibility_'`
    Expected: exit code 0; existing deepsearch research, verify, and synthesize semantics remain intact.
    Evidence: .sisyphus/evidence/task-10-deepsearch-compat.log

  Scenario: Collaboration rollout isolation
    Tool: Bash
    Steps: run `go test ./... -run '^TestMultiAgentCollaboration_'`; if an example path is added, run `go build ./examples/...`
    Expected: exit code 0; the new collaboration path works without forcing deepsearch callers onto the new contract prematurely.
    Evidence: .sisyphus/evidence/task-10-deepsearch-compat-error.log
  ```

  **Commit**: NO | Message: `defer to wave commit` | Files: `patterns/deepsearch/*`, `patterns/collab/*`, host tests, examples/docs only if required by public registration

## Final Verification Wave (MANDATORY — after ALL implementation tasks)
> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**
> **Never mark F1-F4 as checked before getting user's okay.** Rejection or user feedback -> fix -> re-run -> present again -> wait for okay.
- [ ] F1. Plan Compliance Audit — oracle

  **Tool**: oracle + Bash
  **Steps**:
  - Review the final diff against `.sisyphus/plans/multi-agent-collaboration-hydaelyn.md` task-by-task.
  - Run `go test ./...`.
  - Confirm every implemented behavior maps back to Tasks 1-10 and no mandatory acceptance criterion was skipped.
  **Expected**:
  - `go test ./...` exits `0`.
  - Oracle reports no missing mandatory task, no verifier-bypass path, and no violation of the v1 guardrails.
  **Evidence**: `.sisyphus/evidence/f1-plan-compliance.log`

- [ ] F2. Code Quality Review — unspecified-high

  **Tool**: Bash
  **Steps**:
  - Run targeted regression commands: `go test ./host -run '^TestMultiAgentCollaboration_'`, `go test ./patterns/... -run '^(TestCollabPattern_|TestDeepsearchCompatibility_)'`, and `go test ./storage ./blackboard -run '^(Test.*Version|Test.*Stale|Test.*Conflict)'`.
  - Inspect failures for duplication, dead code paths, unsafe defaulting, or contract drift introduced by the new collaboration pattern.
  **Expected**:
  - All commands exit `0`.
  - No newly introduced test-only hacks, dead compatibility shims, or unreachable branches remain.
  **Evidence**: `.sisyphus/evidence/f2-code-quality.log`

- [ ] F3. Agent-Executed QA Sweep — unspecified-high

  **Tool**: Bash
  **Steps**:
  - Run `go test ./host -run '^(TestMultiAgentCollaboration_|TestEngineeringWorkflow_)'`.
  - If an example or registration path changed, run `go build ./examples/...`.
  - Review the emitted logs/evidence to confirm queued retry, stale write rejection, verifier block, replan supersession, and engineering workflow mapping all executed in automated tests.
  **Expected**:
  - All commands exit `0`.
  - Automated QA covers both happy-path and failure-path scenarios without human/manual intervention.
  **Evidence**: `.sisyphus/evidence/f3-agent-qa.log`

- [ ] F4. Scope Fidelity Check — deep

  **Tool**: deep + Bash
  **Steps**:
  - Compare the final implementation against the plan’s Must Have / Must NOT Have sections.
  - Verify no peer-mesh orchestration, no MCP-as-runtime, no separate verifier service, and no workflow DSL/platform rewrite were introduced.
  - Confirm deepsearch remains additive/compatible and the engineering workflow remains planner-task-based.
  **Expected**:
  - Review reports zero scope violations.
  - Any optional/example additions are clearly isolated and do not change existing deepsearch call paths by default.
  **Evidence**: `.sisyphus/evidence/f4-scope-fidelity.log`

## Commit Strategy
- Commit after each wave, not after each individual task.
- Wave 1 commit: `feat(collab): add collaboration contracts and runtime reliability guards`
- Wave 2 commit: `feat(collab): add generalized collaboration pattern and workflow mapping`
- Verification/fix follow-up commit only if final review finds issues.

## Success Criteria
- Hydaelyn can execute a generalized collaboration pattern without inventing a second orchestration substrate.
- Reliability-sensitive failure modes (stale write, duplicate lease execution, verifier bypass, policy-mismatched cancellation) are covered by named automated tests.
- Engineering workflow phases run through planner-generated tasks over the same supervisor/scheduler/blackboard path.
- Deepsearch remains operational and regression-tested during rollout.
