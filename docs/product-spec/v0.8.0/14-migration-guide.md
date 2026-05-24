# 14 — Migration Guide v0.7 → v0.8

> Renumbered from `12-migration-guide.md`. Adds a multi-agent section
> covering the new `multiagent/` package and the optional `agent/`
> primitives application code may consume.

## Scope of breaks

v0.8.0 ships 4 mechanical breaking changes (all in `01-public-api.md`)
and a large additive surface (`agent/` extensions, `multiagent/`, 3
new stores, Task contract extension). Application code that does NOT
touch the 4 breaking points compiles and runs unchanged.

## Mechanical change 1 — `api.Flow.Bypass*` removed

**Diff**: drop all `Bypass*=true` flags from any `api.Flow` literal.
The runtime rejected them anyway (`internal/core/flow_registry.go`),
so production code never relied on them taking effect.

```diff
- f := api.Flow{Name: "investigate", BypassPolicyEngine: true}
+ f := api.Flow{Name: "investigate"}
```

`api.ErrFlowBypass` is also removed; any switch on it should be
deleted.

## Mechanical change 2 — `StartRunCommand` returns typed result

**Diff**:

```diff
  result, _ := runner.ExecuteCommand(ctx, startRun)
- list := result.([]any)
- run := list[0].(api.Run)
- task := list[1].(api.Task)
+ started := result.(api.StartRunResult)
+ run := started.Run
+ task := started.RootTask
```

Or, preferably, switch to the typed facade:

```go
started, err := runner.StartRun(ctx, runInput)
```

## Mechanical change 3 — `RequestApprovalCommand` returns typed result

Same shape as Change 2:

```diff
- list := result.([]any)
- approval := list[0].(api.ApprovalRequest)
- token := list[1].(api.ResumeToken)
+ r := result.(api.RequestApprovalResult)
+ approval := r.Approval
+ token := r.Token
```

## Mechanical change 4 — `api.Task` extension (additive)

`api.Task` gains `Budget`, `InputSchema`, `OutputSchema`. All are
optional. Existing constructors of `api.Task` continue to compile.

If your code constructs Tasks for use with `agent.Engine`, populate
`OutputSchema` (and optionally `InputSchema` and `Budget`) so the
Strong Bounded Loop can repair output and enforce budget.

```go
task := api.Task{
    ID:    api.NewTaskID(),
    Role:  "research",
    Input: inputJSON,
    OutputSchema: schemas.ResearchReportSchema, // recommended
    Budget: &api.TaskBudget{
        MaxTokens:    50_000,
        MaxWallClock: 5 * time.Minute,
        MaxSteps:     20,
    },
}
```

## Adopting the agent/ extensions (optional but recommended)

The Strong Bounded Loop is opt-in. Existing engines continue to work
via the v0.7 adapter for one release. To adopt:

1. Replace any bare `Run(ctx, ...) (string, error)` with
   `Run(ctx, task, OutputPolicy) agent.Result`.
2. Inspect `Result.Failure` for typed failure handling.
3. Optionally implement `agent.ContextManager` if you have custom
   context selection logic.
4. Set `agent.ToolPolicy.Safety` on every Tool when registering — the
   Engine refuses to auto-retry `ToolNonIdempotentSideEffect` and
   routes them through Approval + ActionAttempt.

## Adopting multiagent/ (new in v0.8.0)

If you only use single-agent flows, you can skip this section.

For multi-role workflows:

1. Define `multiagent.AgentClass` per role (Name, InputSchema,
   OutputSchema, Tools, LoopPolicy).
2. Construct a `multiagent.Team` and register Classes via `AddRole`.
3. Pick a Scheduler: `SequentialScheduler` for fixed pipelines,
   `RouterScheduler` for simple branch decisions,
   `SupervisorScheduler` for a central supervisor coordinating
   subordinates.
4. Run with `team.Start(ctx, input)` and resume with
   `team.Resume(ctx, runID)`.

Reference: `16-multi-agent-demo.md` (5-role incident triage).

## ent template (optional adapter migration)

The framework ships no ent template; users wanting one in their fork can use this skeleton:

```
schema/
├── run.go
├── task.go
├── event.go
├── lease.go
├── outbox.go
├── idempotency.go
├── resumetoken.go
├── approval.go
├── blackboard_entry.go
├── mailbox.go
├── trace_span.go
├── usage_record.go
├── deadletter.go
├── schedule.go
├── webhook.go
├── handoff.go            # NEW v0.8
├── team_state.go         # NEW v0.8
└── agent_instance.go     # NEW v0.8
```

Each schema maps 1:1 to the contract interfaces. The conformance
suite verifies field semantics and selector queries; the schema
generator decides the column types.

## Storage backend authors

Backends must add three stores:

```go
type UnitOfWork interface {
    // ...existing 15 store getters...
    Handoffs() HandoffStore
    TeamStates() TeamStateStore
    AgentInstances() AgentInstanceStore
}
```

Run `contract.RunSuite(t, MyFactory)` to verify. The new
`contract/handoffs`, `contract/teamstates`, `contract/agentinstances`,
and `contract/integration/` test packages cover the new contract.

## Memory plugin authors

ADR-013 (`15-memory-optional-plugin.md`) is unchanged. The interface
lives in `memory/`; backends remain application-owned. If you bind a
memory backend to an AgentID via `Registry.BindMemory`, the
`hydaelyn.self.memory.read` Capability activates for that agent.

## Tool authors

If you author Tools, set the appropriate `ToolSafety` when
registering. This drives the agent.Engine's retry / Approval routing.

```go
runner.RegisterTool(api.Tool{
    Name:            "isolate_host",
    EffectType:      api.ToolEffectExternalSideEffect,
    Safety:          agent.ToolNonIdempotentSideEffect, // engine will NOT auto-retry
    RequiresActionTask: true,
    InputSchema:     schemas.IsolateHostInput,
    OutputSchema:    schemas.IsolateHostOutput,
    Invoke:          isolateHostFn,
})
```

## Lint and CI updates

If you have downstream CI:

- Adopt `scripts/check-public-any.sh` against your own exported packages.
- If you have a sentrux config, mirror the import rules
  (`agent/` ↛ `multiagent/`; `multiagent/` ↛ `internal/`).

## Open questions during migration

If migration surfaces conflicts:

- Open a discussion at [GitHub Discussions](https://github.com/Viking602/go-hydaelyn/discussions).
- Reference the master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`.
- For ADR clarifications, comment on the ADR PR threads.

## Verification of migration

For each upgrading application:

- [ ] `go build ./...` succeeds against v0.8.0
- [ ] Removed all `Bypass*` flags from `Flow` literals
- [ ] Updated all `ExecuteCommand` consumers that consume `StartRunResult` / `RequestApprovalResult`
- [ ] If using `agent.Engine` directly, switched to typed `Result`
- [ ] If using a storage backend, added the 3 new stores and `contract.RunSuite(t, factory)` passes
- [ ] If using Tools with side effects, classified each with `ToolSafety`
- [ ] If adopting multi-agent, defined Classes and chose a reference Scheduler
- [ ] Reviewed `_examples/` for parallel patterns
