# Architecture Boundaries

Canonical live document for Venat import seams and ownership rules.
Historical v0.8.0 wording lives in
`docs/product-spec/v0.8.0/11-boundaries.md`. This file is what CI and
`CONTRIBUTING.md` point at.

Anchors: ADR-008 (revised), ADR-009 (amended), ADR-012 Position D,
ADR-015, ADR-016, ADR-017.

## Layers

The five-layer stack is a documentation map, not a proof that every
import is strictly downward:

```
Packs / Workflow / Examples     domain configuration; host-mounted
        ↓ host wiring
Worker integration (worker/)    poll, lease, execute, team drive
        ↓
Multi-Agent (multiagent/)       schedule, dispatch, handoff, vote
        ↓
Agent Loop (agent/)             one-task bounded loop
        ↓
Durable Runner (root + internal) persist, recover, govern
```

`coding/` is a domain runtime, not a sixth kernel layer. Packs name
coding tools; hosts bind `coding.NewToolSet` drivers.

`eval/` is a test harness. Its production import of `worker/` (and the
root façade) is a declared bridge so cases can drive a real run. That
bridge is not a license for packs or `coding/` production code to
import `worker/` or the root module.

`internal/memory` is the process-local development and test
`StoreProvider`. It is not crash-durable and is not a Position D
reference implementation.

## Import seams

Enforced by `scripts/check-import-boundaries.sh` on production,
`TestImports`, and `XTestImports`:

| Package | Must not import |
| ------- | --------------- |
| `api/` | any Venat package |
| `agent/` | `multiagent/` |
| `multiagent/` | root module, `worker/`, `internal/` |
| root façade | `multiagent/` |
| `worker/` | `packs/`, `coding/` |
| `packs/` | `coding/`, `worker/`, root module |
| `coding/` | `worker/`, `packs/`, root module |

Allowed on purpose:

- `multiagent/` → `stream/` (runtime-neutral collaboration primitive)
- `worker/` → root `venat` (integration seam)
- `agent/` → `api/`, `provider/`, `tool/`, `skill/`, `stream/`
- `eval/` → `worker/` and the root façade (declared harness bridge)
- `coding` test files → root façade and `worker/` (named exception for
  the eval-regression harness in `coding/eval_regression_test.go`)

Reverse-edge bans stay even when the five-layer picture is only
documentation. Sentrux 0.5.7 `layer_direction` stays off. It cannot
express the façade → `internal/core` composition root. Sentrux still
enforces cycles, coupling, and god files.

## Six principles

### 1. Core has no domain vocabulary

Code under `api/`, `internal/**`, `agent/**`, and `multiagent/**` must
not contain the closed business-word list. Multi-agent primitives
(`Scheduler`, `Supervisor`, `Voting`, `Handoff`, `Dispatch`, `Team`,
`AgentClass`, `AgentInstance`, `TypedReport`, `TeamState`) are
framework words (ADR-008). Domain vocabulary belongs in `packs/`,
`_examples/`, and docs.

Enforcement: `scripts/check-business-words.sh`.

### 2. Recipes compile to Run/Task; no second runtime

Patterns, recipes, workflows, and schedulers emit Commands or Dispatches
that the Runner persists. A package that grows its own event store,
lease, or outbox is a second runtime.

### 3. Five concepts, five owners

| Concept | Owner |
| ------- | ----- |
| Capability | `api.Capability` / `api.Tool` |
| Procedure | packs, recipes, skills |
| Policy | `api.PolicyEngine` |
| Runtime | Runner, worker, storage contract |
| Scheduling | `multiagent.Scheduler` |

### 4. Side effects are auditable

Mutations that leave the process produce an ActionAttempt or
ToolInvocation, an Event, a TraceSpan when a trace is active, and a
UsageRecord when metering applies.

### 5. Long-running work is resumable

Lease, heartbeat, resume, replay, dead-letter, and reconcile reconstruct
run state, agent step traces, and scheduler snapshots independently.

### 6. Typed failure crosses layers

Bare `error` must not be the only signal at agent → multiagent or
multiagent → pack boundaries. Use `agent.AgentFailure` and
`multiagent.SchedulerFailureError` (or a terminal Run status).

## Storage and memory

- **Position D (ADR-012):** the framework ships `api.StoreProvider` and
  `contract.RunStoreProviderContractTests`. Applications own schema and
  implementation. `internal/memory` is a development default, not a
  backend product.
- **Memory (ADR-013):** `api.Memory[T]` is the optional plugin.
  `memory/` is a deprecated compatibility package.

## Public any-field contract

Exported functions in `api/`, `agent/`, `multiagent/`, and the root
package must not return `[]any`. Exported fields must not be loose
`any` unless tagged `// godoc-allow-any`. Enforcement:
`scripts/check-public-any.sh`.

## Verification

```
make architecture-check
```

runs Sentrux, `check-business-words.sh`, `check-public-any.sh`, and
`check-import-boundaries.sh`. `make verify` includes this target.
