# 01 — Public API Hardening

## Goals

Make the public API tell the truth. Remove fields that the runtime rejects. Stop returning `[]any` from public commands. Extend `api.Task` with the schemas and budget that the v0.8.0 four-layer architecture requires.

## Change 1 — Flow already precision-shrunk (no further work in v0.8.0)

Status: **done**. The `Bypass*` removal that this section originally
described shipped in a prior commit. Current state:

```go
type Flow struct {
    Name            string `json:"name"`
    PlannerPreset   string `json:"plannerPreset,omitempty"`
    RouterPreset    string `json:"routerPreset,omitempty"`
    PolicyPreset    string `json:"policyPreset,omitempty"`
    ProjectorPreset string `json:"projectorPreset,omitempty"`
}
```

The four `*Preset` fields are non-policy hints used by the flow
registry to pick concrete planner / router / policy / projector
implementations from named presets registered with the runtime. They
do not bypass any subsystem and do not encode business policy. They
are retained.

v0.8.0 ships no further `api.Flow` shape changes. If a future release
folds preset hints into a richer registry-side API, that lands in a
later spec rather than v0.8.0.

## Change 2 — Type `StartRunResult`

**Target state** — add to `api/types.go`:

```go
type StartRunResult struct {
    Run      Run  `json:"run"`
    RootTask Task `json:"rootTask"`
}
```

Update `runner.go:46-50`:

```go
case api.StartRunCommand:
    started, ok := result.(core.StartRunResult)
    if !ok { return result, true }
    return api.StartRunResult{
        Run:      adapter.RunFromModel(started.Run),
        RootTask: adapter.TaskFromModel(started.Root),
    }, true
```

## Change 3 — Type `RequestApprovalResult`

**Target state** — add to `api/types.go`:

```go
type RequestApprovalResult struct {
    Approval ApprovalRequest `json:"approval"`
    Token    ResumeToken     `json:"token"`
}
```

Update runner.go accordingly.

## Change 4 — `Task` contract extension (NEW for v0.8.0)

Per ADR-017 and `06-durable-runner.md`, `api.Task` carries the schemas
and budget the multi-agent flow requires.

**Target state** — add to `api/types.go`:

```go
type Task struct {
    ID    TaskID
    Role  string
    Input json.RawMessage

    // v0.8.0 additions (all omitempty, additive)
    Budget       *TaskBudget     `json:"budget,omitempty"`
    InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
    OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type TaskBudget struct {
    MaxTokens    int64         `json:"maxTokens,omitempty"`
    MaxWallClock time.Duration `json:"maxWallClock,omitempty"`
    MaxToolCalls int           `json:"maxToolCalls,omitempty"`
    MaxSteps     int           `json:"maxSteps,omitempty"`
}
```

`TaskBudget` is the unified per-Task budget consumed by `agent.Engine`
and summed by `multiagent.Scheduler` for team-level observability. The
prior `agent.LoopBudget` / `runner.RunBudget` / per-tool-call budget
forks are collapsed into this single source of truth.

## Change 5 — `ExecuteCommand` documentation

Mark `Runner.ExecuteCommand` as a low-level escape hatch:

```go
// ExecuteCommand runs a command through the internal command bus and returns
// the typed result. For most use cases prefer the typed methods (QueueRun,
// RequestApproval, AcquireTaskExecution, …) which avoid the type assertion
// and provide better compile-time signatures.
func (r *Runner) ExecuteCommand(ctx context.Context, command api.Command) (any, error)
```

No signature change. Documentation only.

## Change 6 — `agent/` package public surface (NEW)

The Agent Loop layer is itself a public surface — `03-agent-loop.md`
specifies the full type set (`Step`, `StepPolicy`, `OutputPolicy`,
`Result`, `ToolSafety`, `ContextManager`, `AgentFailure`,
`FailureKind`, `LoopPolicy`). This document records that those types
land under `agent/` rather than `api/` because they bind to runtime
concerns the kernel data model does not own.

Cross-references:

- `agent.Result.Failure *agent.AgentFailure` — the only failure
  shape that crosses the agent → multiagent boundary
  (`11-boundaries.md` Principle 6).
- `agent.Engine.Run(ctx, api.Task, agent.OutputPolicy) agent.Result`
  — typed return; bare error is rejected.

## Change 7 — `multiagent/` package public surface (NEW)

`multiagent/` is the new top-level kernel package
(`05-multi-agent-layer.md`). Its public surface includes `AgentClass`,
`AgentInstance`, `Team`, `Scheduler`, `Dispatch`, `Handoff`,
`BlackboardEntry`, `VotingResult`, `SupervisorDecision`, and the
events listed in `05-multi-agent-layer.md` §Multi-agent events.

`multiagent/` imports `api/` and `agent/` only. `internal/` imports
from `multiagent/` are forbidden by sentrux rules.

## Change 8 — Lint to prevent regression

Add `golangci-lint` custom rule or `revive` rule:

- Forbid `[]any` as the named return type of any exported function in
  `api/`, root package, `agent/`, or `multiagent/`.
- Forbid public type field of type `any` unless explicitly tagged
  `// godoc-allow-any`.
- Forbid `agent.Engine`'s entry points returning bare `error` for
  business failures (must be `agent.Result.Failure`).

Concrete implementation: a script at `scripts/check-public-any.sh`
invoked from CI similar to `check-business-words.sh`. Pattern:

```bash
grep -rnE '^func .* \) \(.*\[\]any' api/ agent/ multiagent/ *.go
```

## Verification

- `go build ./...` succeeds
- `go test ./...` succeeds with the 1 removed test (`TestRegisterFlow_RejectsBypass`) replaced
- New typed-result tests:
  - `TestExecuteCommand_StartRunReturnsTypedResult`
  - `TestExecuteCommand_RequestApprovalReturnsTypedResult`
- `TestTask_BudgetInputOutputSchema_JSONRoundTrip` — Task with TaskBudget + InputSchema + OutputSchema survives JSON round-trip
- `TestTaskBudget_ZeroValueOmittedInJSON` — empty Task.Budget = nil, not `{}`
- CI gate `scripts/check-public-any.sh` passes against `api/`, `agent/`, `multiagent/`
