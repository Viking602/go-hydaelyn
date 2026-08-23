# ADR-018 Self-Sufficient Agent Layer — neutral Spec, per-agent model resolution, and subagent-as-tool

## Status

Accepted — effective from v0.8.0. Builds on ADR-015 (Strong Bounded Agent
Loop) and ADR-016 (Explicit Multi-Agent Scheduler). Anchor documents:
`docs/product-spec/v0.8.0/03-agent-loop-layer.md`,
`docs/product-spec/v0.8.0/05-multi-agent-layer.md`,
`docs/product-spec/v0.8.0/11-boundaries.md` Principle 6 (failure crossing).

## Context

ADR-015 gave the agent layer a strong bounded loop — `agent.Engine` — but no
way to *construct* one from a declaration. An `Engine` was assembled field by
field at the call site, which had three consequences:

1. **No single materialization path.** A standalone app, the worker runtime,
   and any future subagent each wired an `Engine` by hand. Model resolution,
   tool selection, and instruction seeding were duplicated and drifted.
2. **One model per deployment.** The `Engine.Provider` was a single
   `provider.Driver`. A `Driver` already takes the model name per request
   (`Request.Model`), so one driver could serve many model *names* — but there
   was no seam to route different agents to different *vendors*. An app that
   wanted a cheap model for a summarizer and a deep model for a critic had to
   hold two engines and wire each by hand.
3. **No first-class subagent.** "An agent that calls another agent" had no
   framework expression. The only multi-agent construct was the `multiagent/`
   team member — an independently scheduled peer with an `AgentInstance`,
   lease, and blackboard presence (ADR-016). Reusing that for simple delegation
   conflates two different relationships and drags the whole scheduler ontology
   into a single-agent app.

The framework's positioning is "build single-agent apps, multi-agent apps, and
let a single agent spawn subagents — switching freely between them." The agent
layer could not do the first or third on its own, and the second only by hand.

## Decision

Make the agent layer self-sufficient with four additions. None of them touches
the `multiagent/` layer, and `agent/**` still MUST NOT import `multiagent/**`
(ADR-016 §6, one-way dependency preserved).

### 1. Neutral `agent.Spec` + `agent.Build`

`agent.Spec` is the executable declaration of one bounded loop: instructions,
model, tool names, loop policy, tuning fields, and the declared input/output
schemas. `agent.Build(spec, deps)` is the **single** materialization path from
a `Spec` to an `Engine`.

```go
type Spec struct {
    Instructions   string
    Model          string
    Tools          []string
    LoopPolicy     LoopPolicy
    ThinkingBudget int
    StopSequences  []string
    ExtraBody      map[string]any
    InputSchema    json.RawMessage
    OutputSchema   json.RawMessage
}

type BuildDeps struct {
    Providers      provider.Resolver
    Tools          *tool.Bus
    Hooks          hook.Chain
    ContextManager ContextManager
}

func Build(spec Spec, deps BuildDeps) (Engine, error)
```

A `Spec` is **neutral**: it says how to run one loop, never how the agent is
*used*. The same `Spec` can be built and then driven as a standalone agent,
wrapped as a subagent tool, or executed as a team member. **Positioning is the
caller's choice, never a property of the `Spec`.** This is the first of three
disciplines this ADR establishes.

`Build` resolves the model, selects the named tool subset (failing at
construction if a named tool is absent, so a misdeclared tool never fails
mid-run), and seeds a default instructions-based `ContextManager` unless `deps`
overrides it. `Spec.InputSchema`/`OutputSchema` travel with the declaration but
are **not** baked into the `Engine`: output validation is a per-task concern
(`api.Task` drives the `OutputPolicy` at `Run` time), not an `Engine` field.

### 2. `provider.Resolver` — per-agent, cross-vendor model selection

```go
type Resolver interface {
    Driver(model string) (Driver, error)
}

func Single(d Driver) Resolver          // trivial: every agent shares one driver
func NewRegistry(drivers ...Driver) *Registry // routes by Driver.Metadata().Models
```

`Build` calls `deps.Providers.Driver(spec.Model)` once per agent. Because a
`Driver` already takes the model per request, the `Resolver` adds exactly one
new dimension: *which driver a given model name belongs to*. A single-provider
deployment passes `provider.Single(driver)`; a cross-vendor deployment passes a
`Registry` populated with one driver per vendor, and each agent's `Spec.Model`
selects its driver. Duplicate model names resolve last-registration-wins.

There is **no automatic fallback model.** A `Resolver` that cannot serve a model
returns `ErrNoDriverForModel`, which `Build` surfaces as a construction error.
Routing is explicit; degradation policy, if any, belongs to the application.

### 3. `agent.AsTool` + `SubagentDef` — agent-as-tool

```go
func AsTool(child Engine, def SubagentDef) tool.Driver

type SubagentDef struct {
    Name        string
    Description string
    InputSchema tool.Schema
    MaxDepth    int            // 0 → DefaultSubagentMaxDepth (4)
    Budget      *api.TaskBudget
    Effect      tool.EffectType // optional floor; aggregation can only raise it
}
```

`AsTool` wraps an already-materialized child `Engine` as a `tool.Driver` the
parent invokes from within its own loop. This is the **subagent** positioning:
*agent-as-tool, subordinate*. It is deliberately distinct from a `multiagent/`
team member (*agent-as-peer, independent*). The two relationships share the
`Engine` execution mechanism but nothing else:

| | subagent (`agent.AsTool`) | team member (`multiagent`) |
|---|---|---|
| Relationship | subordinate (a tool) | peer (scheduled) |
| Identity | none | `AgentInstance` + lease |
| Control | parent retains it | scheduler holds it |
| Visibility | one tool call in the parent's trace | blackboard / `TypedReport` |
| Budget / failure | subordinate to the parent | governed by the scheduler |

The second discipline: **`agent.AsTool` has zero `multiagent`-ontology
contact.** It depends only on `Engine`, `tool`, and `api` — never on `Spec`,
`AgentClass`, or anything under `multiagent/`. It consumes a materialized
`Engine`, not a declaration, so the subagent path works in an app that never
imports the multi-agent layer.

The third discipline: **a subagent's budget, failure, and observability are
subordinate to the parent.** Concretely:

- The delegation counts as **one** of the parent's `MaxToolCalls` and appears
  as **one** `ToolCallTrace` in the parent's `Step` trace.
- A child failure (`Result.Failure != nil`) becomes an **error tool result**
  (`IsError`) carrying the failure reason and its typed `AgentFailure`
  classification — **never a Go error.** A subagent failure therefore never
  hard-aborts the parent loop; the parent observes the error result and decides
  (mirrors boundaries Principle 6: a failure crosses as typed data, not a bare
  error).
- A child success surfaces the child's final answer. When the child ends with
  trailing assistant text, that text is the tool result's content. When the
  child instead submits its answer through a **terminal tool** — so it completes
  with no trailing assistant text and (under the empty per-delegation
  `OutputPolicy`) no structured output — the wrapper falls back to that terminal
  tool's own content and structured payload rather than returning a blank
  result. Only a **terminal** tool result is promoted: a non-terminal tool
  observation is mid-run state, never the final answer. The terminal result's
  `IsError` status is carried through, so a terminal submit tool that rejects its
  input (an error tool result) surfaces as an error delegation rather than a
  completed one. A child that runs out of iterations (`StopReasonMaxTurns`)
  without producing any answer returns an **error tool result**, so a truncated,
  non-converged run is never reported to the parent as a completed delegation.
- `MaxDepth` (default 4) caps subagent nesting via a context-carried depth
  counter; exceeding it returns an error tool result rather than recursing.
- The child runs under `SubagentDef.Budget` when set, else its own `Engine`
  `LoopPolicy`.

A subagent must also be **no safer to the parent's governance than its child.**
`AsTool.Definition()` aggregates the governance metadata of every tool the child
engine may call — effect type, approval requirement, action-task requirement,
risk level, and policy tags — and advertises the worst case. The aggregation
mirrors `worker.toolDefinitionToRunnerTool`: an approval-gated child tool that
declares no explicit effect normalizes to an external side effect, so the
parent's tool-gate derives the same persisted policy it would for a direct tool.
A tool-less (pure-reasoning) child aggregates to read-only — the genuinely safe
case. `SubagentDef.Effect` sets an optional floor for children whose tools are
not statically visible (registered lazily); aggregation takes the maximum of the
floor and the child effect, so the floor can only **raise** the advertised risk,
never lower it. Policy tags are deduplicated and sorted because `Bus`
enumeration is map-ordered and the advertised definition must be replay-stable
(ADR-007). Without this, advertising a fixed read-only effect would let a
side-effecting delegation bypass the approval the child's own tools require.

### 4. `multiagent.AgentClass.ToSpec()` — the bridge, materialization not positioning

```go
func (c AgentClass) ToSpec() agent.Spec
```

`ToSpec` projects a class's *executable* fields (instructions, model, tools,
loop policy, schemas) onto the neutral `Spec`, and drops the multiagent-only
ontology (`Name`, `Description`, `Capabilities`) that describes the role's
*position* in a team. It is **materialization, not positioning**: it does not
decide whether the result runs standalone, as a subagent, or as a team member.
A team member and a standalone agent built from the same class therefore share
an identical `Spec` — and an identical `Engine` once `Build` resolves models and
tools. This is what lets an application move a role between single-agent and
multi-agent deployments without re-describing it.

Note that `multiagent` remains a pure scheduler (ADR-016): it emits `Dispatch`,
it does not construct engines. An `Executor` that wants per-member model
selection uses `class.ToSpec()` + `agent.Build` with a `Resolver` to build the
member engine; `ToSpec` is the seam that makes this possible without the
scheduler knowing about providers.

### Child usage accounting

A subagent's token usage is folded back into the parent's loop `Usage` after
the tool returns. When the parent has `MaxTokens` set, the child also inherits
the remaining parent budget unless `SubagentDef.Budget` is tighter. The
delegation still counts as one parent tool call.

## Consequences

- **A single construction path.** Standalone apps, the worker runtime, and
  subagents all build engines through `agent.Build`. Model resolution, tool
  selection, and instruction seeding live in one place.
- **Per-agent, cross-vendor models become a one-liner.** Register one driver
  per vendor in a `Registry`, give each `Spec` its model name, and every agent
  routes itself. The `_examples/subagent` example runs three agents on three
  vendors through one `Registry`.
- **Single ↔ multi-agent is a free switch.** The same neutral `Spec` (directly,
  or via `AgentClass.ToSpec`) feeds all three positionings. An app starts
  single-agent, adds a subagent, or graduates a role to a team member without
  re-authoring the agent.
- **Subagent ≠ team member is enforced by structure, not convention.**
  `agent.AsTool` cannot reach the `multiagent` ontology because of the one-way
  dependency, so the distinction cannot quietly erode.
- **Public-any contract holds (ADR-009).** `Spec.ExtraBody` (a provider
  passthrough) carries the `// godoc-allow-any` escape hatch; no exported
  function added here returns `[]any`.

## Compatibility with existing ADRs

- **ADR-015 (Engine)** — `Build` materializes the same `Engine`; the loop is
  unchanged. `Spec`/`Build` sit above the loop, not inside it.
- **ADR-016 (multiagent)** — the one-way `agent/ ↛ multiagent/` dependency is
  preserved. `ToSpec` lives in `multiagent/` (which already imports `agent/`),
  not the reverse. The scheduler stays pure; `ToSpec` is only a projection.
- **ADR-009 (public-any)** — observed; see Consequences.
- **ADR-010 (budget composition)** — child token usage is folded into the
  parent loop; `SubagentDef.Budget` may still tighten the child ceiling.
- **ADR-014 (agent ontology)** — a subagent has no identity, persona, or
  self-model; it is a tool. No ontology red line is crossed.

## References

- Companion ADRs: ADR-015 (Engine), ADR-016 (multi-agent scheduler),
  ADR-017 (Runner boundary)
- Related: ADR-009 (public-any), ADR-010 (budget), ADR-014 (agent ontology)
- Boundary: `docs/product-spec/v0.8.0/11-boundaries.md` Principle 6
- Example: `_examples/subagent`
- Code: `agent/spec.go`, `agent/subagent.go`, `provider/resolver.go`,
  `multiagent/class.go`
