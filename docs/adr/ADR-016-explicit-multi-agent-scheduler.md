# ADR-016 Explicit Multi-Agent Scheduler — `multiagent/` is a first-class kernel package

## Status

Accepted — enforced from the v0.8.0 reconstruction onward. Anchor documents:
`docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §4,
`docs/product-spec/v0.8.0/05-multi-agent-layer.md`,
`docs/product-spec/v0.8.0/11-boundaries.md` Principle 3 (extended).

**Amended for v0.16.0 — 2026-08-28.** `Scheduler`, `SchedulerFunc`,
`SequentialScheduler`, and `Drive` are the supported scheduling protocol. The
unused name-only `api.Flow` registry, the `workflow/` wrapper package, and the
incomplete built-in Graph/DAG implementation are removed. Conditional,
parallel, fan-in, and failure policies belong in application Scheduler
implementations until a real consumer justifies a durable serializable model.

## Context

The pre-reconstruction v0.8.0 design treated multi-agent
coordination as a *Pack concern*. The Runner shipped low-level
primitives — `api.HandoffCommand`, `api.BlackboardReadWriter`,
`ApprovalStore` — and recipes / Packs glued them together via
ad-hoc helpers (`Handoff()`, `RequireApproval()`).

The intent was to keep the kernel minimal. The cost was:

1. **No reusable scheduling logic.** Every Pack reimplemented "who
   runs next" — typically as a long switch on the most recent
   blackboard entry, with no shared abstraction across Packs.
2. **No replayable scheduling state.** Pack-internal scheduling
   decisions did not survive process restart; the recipe's runtime
   knew which agent was next, but if that recipe code crashed mid-
   decision, the Runner could not reconstruct the decision.
3. **No way to verify handoffs.** Pack-helper `Handoff()` carried
   free-form payload. Receiving agents inferred the shape from the
   sending Pack. Schema mismatches were runtime errors.
4. **No clean place for multi-agent eval.** Team-level test cases
   (does this team converge on the right answer with N agents?) had
   no kernel-level harness; each Pack invented its own.

Meanwhile, the framework's stated positioning evolved (see
master spec §1) to: *durable typed multi-agent framework*. Treating
multi-agent coordination as a Pack helper is incoherent with that
positioning.

## Decision

`multiagent/` becomes a **top-level kernel package** alongside
`agent/`, `runner/`, `worker/`. It is the canonical home for every
abstraction that answers "given a team of agent classes and a
shared blackboard, who runs next and what do they receive."

### 1. Package contents

```
multiagent/
├── class.go        // AgentClass
├── instance.go     // AgentInstance
├── team.go         // Team
├── scheduler.go    // Scheduler interface + 3 reference impls
├── dispatch.go     // Dispatch type
├── handoff.go      // Handoff + HandoffStore
├── blackboard.go   // multi-agent BlackboardEntry constraints
├── voting.go       // Voting helpers
├── supervisor.go   // Supervisor helpers
└── events.go       // multi-agent event types
```

### 2. The Scheduler interface

```go
type Scheduler interface {
    Next(ctx context.Context, state TeamState) ([]Dispatch, error)
}
```

A Scheduler decides — programmatically, not via an LLM — which
agents run next given current `TeamState` (latest typed reports,
outstanding handoffs, blackboard contents, failures so far).
Returning more than one Dispatch enables parallel fan-out.

Three reference implementations ship in v0.8.0:

| Scheduler | Decision policy |
|-----------|-----------------|
| `Sequential` | static list of AgentClass; advance on each Result.Valid |
| `Router` | switch on a discriminator field in the most recent TypedReport |
| `Supervisor` | a designated AgentClass emits dispatch decisions as its OutputSchema; Scheduler executes them |

Advanced strategies (Debate / MapReduce / DAG / Swarm) are
explicitly deferred to v0.9.0. The interface is intentionally narrow
so v0.9.0 implementations slot in without breaking v0.8.0.

### 3. AgentClass vs AgentInstance

AgentClass is the *declaration* (instructions, schemas, tools,
LoopPolicy, Capabilities). AgentInstance is an *execution-time
instance* of a class bound to a specific Run. A run may spawn
multiple instances of the same class — e.g. two `ForensicsAgent`
instances investigating two evidence branches in parallel.

This reverses the AgentInstance deferral in the original ADR-014;
see ADR-014 revision for the rationale. AgentInstance.ID
generation is deterministic from `(RunID, Class.Name,
spawn-sequence)` so replay reconstructs the same instance IDs.

### 4. Typed Handoff

```go
type Handoff struct {
    From                 AgentID
    To                   AgentID
    Reason               string
    Payload              json.RawMessage
    EvidenceIDs          []string
    RequiredOutputSchema json.RawMessage
}
```

Handoffs are persisted via `HandoffStore` (added to `UnitOfWork`)
so they are replayable, auditable, and validated against the
receiving agent's `InputSchema`. Free-form prose handoffs at the
framework level are disallowed; Packs may render prose into
`Reason` but the structured `Payload` is the load-bearing channel.

### 5. Scheduler ↔ Runner bridge

The bridge is `api.Task` (enriched per master spec §6):

```
multiagent.Scheduler.Next(state) → []Dispatch
    each Dispatch.Task → Runner persists via TaskStore
    Worker Runtime acquires lease, runs agent.Engine on the Task
    agent.Engine returns agent.Result
    Runner writes Result.Structured to Blackboard as TypedReport
    Runner appends multi-agent Events
    Scheduler.Next reads updated TeamState
```

Scheduler is stateless across ticks; all coordination state lives
in `TeamStateStore`. This is what makes Scheduler decisions
replayable from EventStore alone.

### 6. Hard constraints (immediately effective)

- `multiagent/**` MAY import `api/`, `agent/`, `stream/`, and the standard
  library. It MUST NOT import the root Runner package, `worker/`, or any
  `internal/` package. `stream/` is the intentional exception because it is a
  runtime-neutral collaboration primitive and does not expose Runner state.
- `agent/**` MUST NOT import `multiagent/**` (one-way dependency).
- Schedulers MUST NOT side-effect external systems directly.
  All side effects route through agent tools, which route through
  Runner.
- `HandoffStore`, `TeamStateStore`, `AgentInstanceStore` are added
  to `UnitOfWork`. Storage providers that omit them fail contract
  tests.
- Scheduler-level failure (e.g. no agent can handle current state)
  MUST emit a `multiagent.SchedulerFailure` event and surface to
  Runner as a typed terminal Run status — not as a panic or bare
  error.

These import constraints are checked from `go list` output by
`scripts/check-import-boundaries.sh`. Sentrux 0.5.7 still enforces global cycle,
coupling, and god-file limits, but cannot precisely encode path-scoped import
rules; it is therefore complementary to, not a replacement for, the script.

### 7. Multi-agent primitive vocabulary

These nouns/verbs are framework primitives, not business
vocabulary:

```
Scheduler, Supervisor, Voting, Debate, Handoff, Dispatch,
Team, AgentClass, AgentInstance, Blackboard, TypedReport
```

ADR-008's business-word ban does NOT apply to them. The ADR-008
revision records this exception explicitly so the
`check-business-words.sh` baseline stays honest.

## Consequences

- Pack authors stop writing scheduling code. A Pack picks a
  Scheduler implementation and supplies AgentClass definitions
  and schemas; the kernel handles the rest.
- Scheduling decisions become replayable. Restarting a process
  mid-run reconstructs the next Dispatch from EventStore +
  TeamStateStore.
- Handoff schema validation moves from runtime surprise to
  framework-enforced — the receiving agent's `InputSchema` is
  checked against the Handoff `Payload` before dispatch.
- Team-level eval gets a kernel-level harness — `eval/` consumes
  `TeamState` snapshots and can assert against multi-agent
  trajectories.
- The framework's positioning sentence (master spec §1) becomes
  truthful at the directory level: the four-layer architecture is
  reflected in the package tree (`multiagent/` exists as a sibling
  of `agent/`).

## Compatibility with existing ADRs

- **ADR-008 (revised)** — multi-agent primitives are exempt from
  the business-word ban; explicit list in the revision.
- **ADR-014 (revised)** — AgentInstance is now an accepted
  structural concept (lives in `multiagent/`), reversing the
  earlier deferral. Metaphysical-identity red lines (no
  `Self`/persona/UpdateOwnProfile/auto-derived personality) remain
  in force.
- **ADR-007** — Scheduler tick events become part of the monotonic
  EventStream; replay determinism extends to multi-agent
  decisions.
- **ADR-012 (Position D)** — `HandoffStore`/`TeamStateStore`/
  `AgentInstanceStore` are added to the contract; the framework
  still ships no implementation. Applications implement against
  their own data stack.

## References

- Master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §4
- Design: `docs/product-spec/v0.8.0/05-multi-agent-layer.md`
- Storage additions: `docs/product-spec/v0.8.0/07-storage.md`
- Boundary: `docs/product-spec/v0.8.0/11-boundaries.md` Principle 3 (extended)
- Companion ADRs: ADR-015 (Engine), ADR-017 (Runner)
- Related: ADR-007 (EventStore replay), ADR-008 (framework vs business), ADR-012 (storage), ADR-014 (agent ontology)
