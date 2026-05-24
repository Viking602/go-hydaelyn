# 01 — Public API Hardening

## Goals

Make the public API tell the truth. Remove fields that the runtime rejects. Stop returning `[]any` from public commands.

## Change 1 — Remove `Flow.Bypass*`

**Current state** (api/types.go:224-229):

```go
type Flow struct {
    Name                     string `json:"name"`
    BypassTaskStore          bool   `json:"bypassTaskStore,omitempty"`
    BypassPolicyEngine       bool   `json:"bypassPolicyEngine,omitempty"`
    BypassTaskExecutionLease bool   `json:"bypassTaskExecutionLease,omitempty"`
    BypassHandoff            bool   `json:"bypassHandoff,omitempty"`
    BypassResponseLayer      bool   `json:"bypassResponseLayer,omitempty"`
    BypassOutputGateway      bool   `json:"bypassOutputGateway,omitempty"`
}
```

`internal/core/flow_registry.go:8-14` rejects any flow with any `Bypass*=true` and returns `ErrFlowBypass`. The fields are pure decoys — setting any to true causes registration failure.

**Target state**:

```go
type Flow struct {
    Name string `json:"name"`
}
```

**Files changed**:

- `api/types.go`: remove 6 fields from `Flow`
- `api/errors.go`: remove `ErrFlowBypass`
- `internal/core/model/flow.go`: remove 6 fields from internal model
- `internal/core/model/errors.go`: remove `ErrFlowBypass`
- `internal/core/errors.go`: remove `ErrFlowBypass` re-export
- `internal/core/flow_registry.go`: remove `Bypass*` check, RegisterFlow becomes 4 lines
- `internal/core/adapter/types.go:475-480`: remove 6 fields from round-trip
- `internal/core/adapter/errors.go:59,86`: remove `ErrFlowBypass` from translation tables
- `internal/core/runtime_test.go:619`: remove `TestRegisterFlow_RejectsBypass` test, replace with a smoke test that successful registration works
- `errors.go` (top level): remove `ErrFlowBypass` re-export

**Breaking-change classification**: pre-1.0 minor bump; documented in `12-migration-guide.md`.

## Change 2 — Type `StartRunResult`

**Current state** (runner.go:50):

```go
case api.StartRunCommand:
    started, ok := result.(core.StartRunResult)
    if !ok { return result, true }
    return []any{adapter.RunFromModel(started.Run), adapter.TaskFromModel(started.Root)}, true
```

Public callers must do `result.([]any)[0].(api.Run)` — a triple type assertion. The internal type `core.StartRunResult` already exists; it just isn't exported.

**Target state** — add to `api/types.go`:

```go
// StartRunResult is the typed result of StartRunCommand. Use Runner.QueueRun
// for the typed entrypoint; this type is also returned from
// Runner.ExecuteCommand for low-level callers.
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

**Current state** (runner.go:99-103):

```go
case api.RequestApprovalCommand:
    requested, ok := result.(core.RequestApprovalResult)
    if !ok { return result, true }
    return []any{adapter.ApprovalRequestFromModel(requested.Approval), adapter.ResumeTokenFromModel(requested.Token)}, true
```

**Target state** — add to `api/types.go`:

```go
// RequestApprovalResult is the typed result of RequestApprovalCommand.
type RequestApprovalResult struct {
    Approval ApprovalRequest `json:"approval"`
    Token    ResumeToken     `json:"token"`
}
```

Update runner.go accordingly.

## Change 4 — `ExecuteCommand` documentation

Mark `Runner.ExecuteCommand` as a low-level escape hatch:

```go
// ExecuteCommand runs a command through the internal command bus and returns
// the typed result. For most use cases prefer the typed methods (QueueRun,
// RequestApproval, AcquireTaskExecution, …) which avoid the type assertion
// and provide better compile-time signatures. ExecuteCommand exists for tools
// (replay, migration, admin) that operate generically over commands.
func (r *Runner) ExecuteCommand(ctx context.Context, command api.Command) (any, error)
```

No signature change. Documentation only.

## Change 5 — Lint to prevent regression

Add `golangci-lint` custom rule or `revive` rule (depending on existing config in `.golangci.yml`):

- Forbid `[]any` as the named return type of any exported function in `api/` or root package.
- Forbid public type field of type `any` unless explicitly tagged `// godoc-allow-any`.

Concrete implementation: a script at `scripts/check-public-any.sh` invoked from CI similar to `check-business-words.sh`. Pattern:

```bash
grep -rnE '^func .* \) \(.*\[\]any' api/ *.go
```

## Verification

- `go build ./...` succeeds
- `go test ./...` succeeds with the 1 removed test (`TestRegisterFlow_RejectsBypass`) replaced
- New typed-result tests:
  - `TestExecuteCommand_StartRunReturnsTypedResult`
  - `TestExecuteCommand_RequestApprovalReturnsTypedResult`
- CI gate `scripts/check-public-any.sh` passes
