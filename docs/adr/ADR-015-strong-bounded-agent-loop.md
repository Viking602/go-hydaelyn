# ADR-015 Strong Bounded Agent Loop — `agent.Engine` owns task-local correctness, nothing more

## Status

Accepted — enforced from the v0.8.0 reconstruction onward. Anchor documents:
`docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §3,
`docs/product-spec/v0.8.0/03-agent-loop.md`,
`docs/product-spec/v0.8.0/11-boundaries.md` Principle 6.

## Context

Earlier v0.8.0 drafts described `agent.Engine` as *thin* — a small,
stable while-loop with as little behavior as possible, with all
substantive work delegated to Runner primitives and Pack glue. The
intent was to keep the Engine's surface minimal and the framework's
value concentrated in the Runner.

In practice this framing causes two problems:

1. **A weak agent dooms multi-agent scheduling.** If individual
   agents return free-form prose, fail with bare `error`, retry
   side-effecting tools without idempotency, and accumulate
   unbounded context, then no Scheduler upstream can route
   reliably. The Scheduler's correctness is upper-bounded by the
   weakest agent in the team.
2. **"Thin" is misleading vocabulary.** The actual Engine code in
   the reconstruction is ~600-800 LOC owning step tracing, schema
   validation, repair, tool dispatch with safety classification,
   context selection, budget enforcement, and typed failure
   construction. Calling that "thin" produces a vocabulary mismatch
   that bleeds into design discussions.

The right framing is **strong but bounded**. Engine owns task-local
correctness completely; everything else is explicitly outside Engine.

## Decision

`agent.Engine` is *strong but bounded*. Three lists define the
boundary.

### 1. What Engine owns (the *strong* part)

| Capability | Form | Spec anchor |
|------------|------|-------------|
| Step trace | `agent.Step{Index, ModelCall, ToolCalls, Observations, Decision, BudgetUsed}` | 03-agent-loop §Step |
| Task-local planning | `agent.StepPolicy{ Next(ctx, state) StepDecision }` | 03-agent-loop §Step planning |
| Output policy + repair | `agent.OutputPolicy{Schema, Validate, Repair, MaxRepairAttempts}` | 03-agent-loop §OutputPolicy |
| Typed result | `agent.Result{Text, Structured, Valid, RepairCount, Failure}` | 03-agent-loop §Result |
| Tool safety classification | `agent.ToolSafety = ReadOnly \| IdempotentSideEffect \| NonIdempotentSideEffect` | 03-agent-loop §ToolSafety |
| Context management | `agent.ContextManager{Build, Compact, SelectEvidence}` | 03-agent-loop §ContextManager / 09-context.md |
| Budget enforcement | reads `Task.Budget`, decrements per Step | 03-agent-loop §Budget |
| Typed failure | `agent.AgentFailure{Kind, Reason, Retryable, Escalatable, EvidenceIDs}` + `FailureKind` enum | 03-agent-loop §AgentFailure |

### 2. What Engine explicitly does NOT own (the *bounded* part)

| Concern | Owner |
|---------|-------|
| Choosing the next agent | `multiagent.Scheduler` (ADR-016) |
| Persisting Run / Task / Event state | Runner (ADR-017) |
| Acquiring leases | Runner (`LeaseStore.AcquireWithExpectedVersion`) |
| Queueing approvals | Runner (`ApprovalStore`) |
| Idempotency ledger | Runner (`ActionAttemptStore`) |
| Dead-letter | Runner (`DeadLetterStore`) |
| Workflow-level planning | Scheduler / Pack |
| Multi-agent handoff routing | `multiagent.Scheduler` + `HandoffStore` |
| Cross-run / long-term memory | application via `api.Memory[T]` (ADR-013) |

### 3. Hard constraints (immediately effective)

- `agent/**` MUST NOT import `multiagent/**` or framework-internal
  scheduling state. The dependency direction is one-way:
  multiagent → agent, never the reverse.
- `agent.Result` MUST be the only successful return shape from the
  loop. Returning `(string, error)` or `(json.RawMessage, error)`
  from Engine entry points is rejected at code review.
- `agent.AgentFailure` is the *only* error shape that crosses the
  agent → multiagent boundary. Bare `error` is not permitted on
  that boundary (enforced by Principle 6 in 11-boundaries.md).
- Engine MUST NOT auto-retry tools whose `ToolSafety ==
  ToolNonIdempotentSideEffect`. Non-idempotent retries must route
  through Approval + ActionAttempt at the Runner layer.
- Engine's `ContextManager` MUST NOT issue writes to
  `api.Memory[T]`. Memory writes are application/recipe code; the
  loop reads.

### 4. Strengthening is staged

The seven capabilities in §1 are not all v0.8.0-day-zero. The
staging plan (recorded in 13-rollout-plan.md):

| Phase | Capability landed |
|-------|-------------------|
| 1 | Step trace, OutputPolicy + repair, Result, ToolSafety |
| 1 | ContextManager interface (default no-op impl) |
| 1 | AgentFailure + FailureKind enum |
| 2 | Engine integration with multiagent.Scheduler via Task contract |
| 4 | Eval coverage for each FailureKind |

Each capability is testable in isolation; the staging order honors
the dependency direction enforced in §3.

## Consequences

- Multi-agent scheduling becomes upper-bounded by the *Engine
  contract*, not by the weakest agent in the team. A Scheduler
  that depends on `Result.Structured` against a declared schema is
  immune to agents that "decide to return prose this time."
- "Thin Engine" framings in older docs / PR descriptions are
  withdrawn. Cite this ADR when rejecting reframings.
- Engine contributors have a clear surface to extend: add to one of
  the seven §1 capabilities, not invent a new top-level concept
  inside `agent/`.
- The ban on Engine touching Runner state machines means the loop
  remains replayable from `EventStore` alone — there is no
  hidden in-Engine state to reconstruct.

## Compatibility with existing ADRs

- **ADR-008** — Engine's strengthening does not introduce business
  vocabulary. Words like *Scheduler*, *Supervisor*, *Voting* belong
  to `multiagent/`, not `agent/`, and are covered by the ADR-008
  revision. `agent/` itself remains domain-free.
- **ADR-013** — ContextManager reads `api.Memory[T]` via the
  application-provided interface. The framework still ships no
  Memory implementation; the Engine's strengthening does not change
  Memory's status as an optional plugin.
- **ADR-014 (revised)** — Engine produces no `Self`/persona
  artifacts; the strengthening adds only structural capabilities.
  AgentInstance (now accepted for v0.8.0) is a `multiagent/`
  concept, not an Engine concept.

## References

- Master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §3
- Design: `docs/product-spec/v0.8.0/03-agent-loop.md`
- Boundary: `docs/product-spec/v0.8.0/11-boundaries.md` Principle 6
- Companion ADRs: ADR-016 (Scheduler), ADR-017 (Durable Runner Boundary)
- Related: ADR-008 (framework vs business), ADR-013 (memory plugin), ADR-014 (agent ontology)
