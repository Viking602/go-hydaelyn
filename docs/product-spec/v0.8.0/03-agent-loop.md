# 03 — Agent Loop (Strong but Bounded)

> Anchor: ADR-015 Strong Bounded Agent Loop. Master spec §3.

## Goal

Define the public surface and operational protocol of `agent.Engine`
under the v0.8.0 reconstruction. Engine becomes *strong but bounded* —
it owns everything required for one agent to do one task well, and
nothing else.

## What Engine owns

| Capability | Surface |
|------------|---------|
| Step trace | `agent.Step` |
| Output policy + repair | `agent.OutputPolicy`, `agent.Result` |
| Tool safety | `agent.ToolSafety` |
| Context management | `agent.ContextManager` |
| Task-local planning | `agent.StepPolicy` |
| Budget enforcement | reads `api.Task.Budget`, decrements per step |
| Typed failure | `agent.AgentFailure`, `agent.FailureKind` |

What Engine does NOT own is listed exhaustively in ADR-015 §2.

## Step

`agent/step.go`:

```go
package agent

// Step is one iteration of the agent loop. The loop produces an ordered
// sequence of Steps per Engine invocation; the sequence is the audit and
// replay artifact of the loop's reasoning.
type Step struct {
    Index        int           `json:"index"`
    ModelCall    *ModelCall    `json:"modelCall,omitempty"`
    ToolCalls    []ToolCall    `json:"toolCalls,omitempty"`
    Observations []Observation `json:"observations,omitempty"`
    Decision     StepDecision  `json:"decision"`
    BudgetUsed   BudgetUsage   `json:"budgetUsed"`
}

type StepDecision struct {
    Kind   StepDecisionKind `json:"kind"`
    Reason string           `json:"reason,omitempty"`
}

type StepDecisionKind string

const (
    StepContinue       StepDecisionKind = "continue"
    StepCallTool       StepDecisionKind = "call_tool"
    StepProduceOutput  StepDecisionKind = "produce_output"
    StepRequestRepair  StepDecisionKind = "request_repair"
    StepFail           StepDecisionKind = "fail"
)

type BudgetUsage struct {
    Tokens    int64         `json:"tokens"`
    WallClock time.Duration `json:"wallClock"`
    ToolCalls int           `json:"toolCalls"`
}
```

`Step` is appended to `EventStore` as part of `EventStepCompleted` (new
event type) so that replay reconstructs the full loop trajectory.

## StepPolicy (task-local planning)

`agent/step_policy.go`:

```go
package agent

// StepPolicy decides the next StepDecision given current loop state.
// Default implementation: model-driven (the LLM's tool/output choice).
// Custom implementations enable rule-based pre-flight or post-validation
// of model decisions.
type StepPolicy interface {
    Next(ctx context.Context, state LoopState) (StepDecision, error)
}

type LoopState struct {
    History    []Step
    Budget     api.TaskBudget
    LastResult *ToolResult
    Task       api.Task
}
```

Workflow-level (multi-agent) planning is NOT a StepPolicy concern —
that lives in `multiagent.Scheduler` (ADR-016).

## OutputPolicy + repair

`agent/output_policy.go`:

```go
package agent

// OutputPolicy controls how Engine validates and repairs the model's
// final structured output before returning a Result.
type OutputPolicy struct {
    // Schema is a JSON Schema document the output must satisfy.
    Schema json.RawMessage `json:"schema,omitempty"`

    // Validate, if true, runs Schema against the model's final output and
    // sets Result.Valid accordingly. If false, output is returned as-is.
    Validate bool `json:"validate"`

    // Repair, if true and Validate is true, feeds validation errors back
    // to the model up to MaxRepairAttempts times. Each repair attempt is
    // a new Step with StepDecision.Kind=StepRequestRepair.
    Repair bool `json:"repair"`

    // MaxRepairAttempts caps the repair loop. Zero means no repair.
    MaxRepairAttempts int `json:"maxRepairAttempts,omitempty"`
}
```

Repair loop:

1. Model produces candidate output.
2. Validate against `OutputPolicy.Schema`. If valid → return.
3. If invalid and `Repair == false` → return with `Result.Valid = false`.
4. If invalid and `RepairCount < MaxRepairAttempts` → append validation
   error to history, emit Step with `StepRequestRepair`, increment count.
5. If invalid and `RepairCount == MaxRepairAttempts` → return with
   `Result.Failure = &AgentFailure{Kind: FailureRepairFailed}`.

## Result

`agent/result.go`:

```go
package agent

// Result is the typed return shape of Engine.Run. Exactly one of
// (Valid && Failure == nil) or (Failure != nil) is true on return.
type Result struct {
    Text        string          `json:"text,omitempty"`
    Structured  json.RawMessage `json:"structured,omitempty"`
    Valid       bool            `json:"valid"`
    RepairCount int             `json:"repairCount"`
    Failure     *AgentFailure   `json:"failure,omitempty"`
    Steps       []Step          `json:"steps,omitempty"`
}
```

Returning `(string, error)` or `(json.RawMessage, error)` from Engine
entry points is rejected at code review (ADR-015 §3).

## ToolSafety

`agent/tool_safety.go`:

```go
package agent

// ToolSafety classifies the side-effect profile of a Tool. In v0.8.0 it is
// a declared policy vocabulary only: runtime side-effect gating is enforced
// through tool.Definition metadata and the worker GovernedToolBus. Engine
// consumption of ToolSafety/ToolPolicy is reserved for v0.9.0.
type ToolSafety int

const (
    // ToolReadOnly: the tool produces no side effect. Auto-retry safe.
    ToolReadOnly ToolSafety = iota

    // ToolIdempotentSideEffect: the tool mutates state but does so
    // idempotently when invoked with the same idempotency key. Auto-
    // retry safe with the idempotency key threaded through.
    ToolIdempotentSideEffect

    // ToolNonIdempotentSideEffect: the tool mutates state and repeating
    // an invocation with the same input produces a different effect
    // (e.g. money transfer, ticket creation, kubectl apply without
    // server-side apply). Engine-level retry blocking for this enum is a
    // v0.9.0-reserved behavior; v0.8.0 gates concrete side effects through
    // RequiresActionTask / Runner ActionAttempt metadata.
    ToolNonIdempotentSideEffect
)

// ToolPolicy is the per-tool execution policy vocabulary reserved for
// Engine integration in v0.9.0.
type ToolPolicy struct {
    Timeout        time.Duration
    Retry          RetryPolicy
    ErrorBehavior  ToolErrorBehavior
    Safety         ToolSafety
    MaxConcurrency int
}
```

Intended retry rules per safety (v0.9.0-reserved; not enforced by
`agent.Engine` in v0.8.0):

| Safety | Engine auto-retry | Runner path required |
|--------|-------------------|----------------------|
| `ToolReadOnly` | yes | no |
| `ToolIdempotentSideEffect` | yes (with idempotency key) | optional |
| `ToolNonIdempotentSideEffect` | **no** | yes — `ApprovalStore` + `ActionAttemptStore` |

## ContextManager

`agent/context_manager.go`:

```go
package agent

// ContextManager owns prompt construction, history compaction, and
// evidence selection for the agent loop. It is the consumer side of
// Memory and Blackboard; it does not write to either.
type ContextManager interface {
    // Build assembles the prompt sent to the model for the next Step.
    Build(ctx context.Context, state AgentState) (Prompt, error)

    // Compact reduces history to fit token budget. Engine calls this when
    // history would exceed the model's context window or the per-step
    // budget allotment.
    Compact(ctx context.Context, history History) (History, error)

    // SelectEvidence picks evidence items relevant to the current step.
    // Returned items become part of the prompt's evidence section.
    SelectEvidence(ctx context.Context, state AgentState) ([]Evidence, error)
}

type AgentState struct {
    Task        api.Task
    Steps       []Step
    Blackboard  api.BlackboardCommittedReader
    MemoryGet   func(MemorySelector) ([]MemoryEntry, error) // nil if no Memory configured
}
```

Default implementation (`agent.DefaultContextManager`) builds a simple
prompt of instructions + last N steps + selected evidence. Pack authors
register custom implementations when they need domain-specific selection
heuristics.

## AgentFailure

`agent/failure.go`:

```go
package agent

// AgentFailure is the typed failure shape that crosses the
// agent → multiagent boundary. Bare error is NOT permitted on that
// boundary (ADR-015 §3, 11-boundaries.md Principle 6).
type AgentFailure struct {
    Kind        FailureKind `json:"kind"`
    Reason      string      `json:"reason,omitempty"`
    Retryable   bool        `json:"retryable"`
    Escalatable bool        `json:"escalatable"`
    EvidenceIDs []string    `json:"evidenceIds,omitempty"`
}

type FailureKind string

const (
    FailureBudgetExhausted      FailureKind = "budget_exhausted"
    FailureToolUnavailable      FailureKind = "tool_unavailable"
    FailureSchemaInvalid        FailureKind = "schema_invalid"
    FailureRepairFailed         FailureKind = "repair_failed"
    FailureUnsafeAction         FailureKind = "unsafe_action"
    FailureInsufficientEvidence FailureKind = "insufficient_evidence"
)
```

Scheduler decisions per FailureKind (informative, not enforced):

| Kind | Typical Scheduler action |
|------|--------------------------|
| `FailureBudgetExhausted` | Terminate or escalate to Supervisor |
| `FailureToolUnavailable` | Retry with backoff, then escalate |
| `FailureSchemaInvalid` | Retry same agent with stricter prompt; max 1-2 |
| `FailureRepairFailed` | Switch agent class or request human approval |
| `FailureUnsafeAction` | Request human approval; do not retry automatically |
| `FailureInsufficientEvidence` | v0.9.0 target: dispatch upstream agent to gather more evidence |

## Engine entry point

`agent/agent.go`:

```go
package agent

// Engine is the agent loop. Each Run executes one task per ADR-015.
type Engine struct {
    Provider       provider.Driver
    Tools          map[string]Tool
    StepPolicy     StepPolicy
    ContextManager ContextManager
    LoopPolicy     LoopPolicy
}

// Run executes the agent loop for one Task and returns a typed Result.
// The caller is typically the Worker Runtime invoking Engine on behalf
// of a Scheduler dispatch.
func (e *Engine) Run(ctx context.Context, task api.Task, policy OutputPolicy) Result
```

Note the typed return: `Result`, not `(Result, error)`. Failures live in
`Result.Failure`. Engine returning bare `error` is reserved for
programmer errors (nil provider, nil tool map, etc.) and is treated as
a panic-equivalent by Worker Runtime.

## LoopPolicy

```go
type LoopPolicy struct {
    MaxSteps      int           // hard cap on Step count; AgentFailure if exceeded
    StepTimeout   time.Duration // per-Step timeout
    AllowParallel bool          // permit parallel tool calls within one Step
}
```

`LoopPolicy` is per-AgentClass; multiagent.Scheduler reads it from
AgentClass and passes it to Engine via Task input.

## Storage anchors

Engine itself writes nothing to storage directly. The Worker Runtime
that hosts Engine writes:

- `EventStepCompleted` to `EventStore` after each Step
- `agent.Result.Steps` to `Run.Trace` (via existing TraceStore) for audit

Engine remains a pure function over (Task, OutputPolicy, dependencies)
→ Result, which keeps it replay-friendly.

## Hard rules

1. Engine never imports `multiagent/**` (one-way dependency).
2. Engine never writes to `api.Memory[T]`; ContextManager reads only.
3. Engine never auto-retries `ToolNonIdempotentSideEffect` tools.
4. Engine returns `Result` only; bare error returns are rejected at
   review.
5. `AgentFailure` is the only failure shape that crosses the
   agent → multiagent boundary.
6. Engine respects `Task.Budget`. Exhaustion produces
   `FailureBudgetExhausted`, not a partial silent result.

## Verification

- `TestEngine_StepTraceComplete` — Result.Steps has Index, ModelCall, ToolCalls populated correctly
- `TestEngine_SchemaRepairLoop` — invalid output repaired N times then returned with Result.Valid
- `TestEngine_RepairExceedsMaxAttempts_FailureKind` — Result.Failure.Kind == FailureRepairFailed
- `TestEngine_NonIdempotentToolNotRetried` — auto-retry skipped; AgentFailure Kind == FailureUnsafeAction
- `TestEngine_BudgetExhausted_FailureKind` — token budget exceeded → FailureBudgetExhausted
- `TestEngine_ToolUnavailable_FailureKind` — provider returns 503 → FailureToolUnavailable, Retryable: true
- `TestContextManager_CompactsHistoryUnderBudget` — Compact reduces history within budget
- `TestStepPolicy_Default_ModelDriven` — default StepPolicy delegates to model tool/output choice
- `TestEngine_NeverImportsMultiagent` — sentrux boundary check
- `TestResult_NoBareErrorReturn` — `go vet`-style check rejecting Engine APIs that return bare error
