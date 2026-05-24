# 08 — Evaluation Framework

## Goal

Give framework users a structured way to assert what an Agent did (or didn't do) across a run. Replace the "examples-as-tests" pattern with a stable assertion contract that works against the event projection.

## Why this is hard

Evaluation against a non-deterministic LLM is structurally different from unit testing:

- Same input may produce different output text
- Tool call order may vary
- Token counts vary
- Wall-clock duration varies

So assertions must operate at the **behavioral** level: did the right Tool get called? Did the report contain the required fields? Did the run stay under budget? Did Policy reject the right things? These are projections over the event stream, not equality checks.

## Core types

`eval/types.go` (new package `eval/`):

```go
package eval

import (
    "context"
    "time"

    "github.com/Viking602/go-hydaelyn"
    "github.com/Viking602/go-hydaelyn/api"
)

// EvalCase is one test case for an Agent.
type EvalCase struct {
    Name        string
    Description string

    // Setup runs before the case. Use to register agents, seed blackboards, etc.
    Setup func(ctx context.Context, runner *hydaelyn.Runner) error

    // Input is the Run input to drive the case.
    Input api.RunInput
    AgentID string // profile to invoke; uses runner.RunFromProfile

    // Assertions are checked after the run reaches a terminal state.
    Assertions []Assertion

    // Timeout is the maximum wall-clock for the run.
    Timeout time.Duration

    // Repeats the case N times and requires all to pass. Default 1.
    Repeats int
}

// EvalResult is the outcome of running an EvalCase.
type EvalResult struct {
    Case       EvalCase
    Run        api.Run
    Passed     bool
    Failures   []AssertionFailure
    Duration   time.Duration
    Usage      []api.UsageRecord
}

type AssertionFailure struct {
    Assertion string
    Message   string
}

// Assertion checks a property over the terminal projection of a run.
type Assertion interface {
    Check(ctx context.Context, p Projection) error
    Describe() string
}

// Projection is the read-only view of a completed run that Assertions inspect.
type Projection interface {
    Run() api.Run
    Tasks() []api.Task
    Events() []api.Event
    BlackboardItems(sel api.BlackboardSelector) []api.BlackboardItem
    ToolInvocations() []api.ToolInvocation
    ActionAttempts() []api.ActionAttempt
    UsageRecords() []api.UsageRecord
    PolicyDecisions() []api.PolicyDecision
    TraceSpans() []api.TraceSpan
    UserMessages() []api.UserMessage
}
```

## Runner

`eval/runner.go`:

```go
package eval

// RunCase executes one EvalCase against the given runner and returns the result.
func RunCase(ctx context.Context, runner *hydaelyn.Runner, c EvalCase) EvalResult

// RunSuite executes a list of EvalCases and returns aggregated results.
func RunSuite(ctx context.Context, runner *hydaelyn.Runner, cases []EvalCase) Suite

type Suite struct {
    Results []EvalResult
    Passed  int
    Failed  int
    Duration time.Duration
}
```

## Built-in assertions

`eval/assertions.go`:

```go
// TaskCompleted asserts that at least one task with the given Role reached
// status TaskStatusCompleted.
func TaskCompleted(role string) Assertion

// ToolCalled asserts that a Tool with the given Name was invoked N or more
// times. N defaults to 1.
func ToolCalled(name string, minCount ...int) Assertion

// ToolNotCalled asserts that the named Tool was never invoked.
func ToolNotCalled(name string) Assertion

// NoHighRiskAction asserts that no ActionAttempt with EffectType
// ExternalSideEffect succeeded without an approval.
func NoHighRiskAction() Assertion

// ReportContains asserts that the run produced a final report containing the
// given substring (or matching the given regex if prefixed "/.../" ).
func ReportContains(pattern string) Assertion

// BlackboardHasItem asserts that at least one BlackboardItem matches the
// selector.
func BlackboardHasItem(sel api.BlackboardSelector) Assertion

// WithinBudget asserts that the run's total Credits did not exceed budget.
func WithinBudget(maxCredits int64) Assertion

// WithinDuration asserts that the run finished within the given wall-clock.
func WithinDuration(max time.Duration) Assertion

// PolicyEffect asserts that at least one PolicyDecision had the given Effect
// (e.g., RequireApproval was correctly triggered).
func PolicyEffect(effect api.PolicyEffectKind, minCount ...int) Assertion

// ReplayDeterministic asserts that re-running the projector over the event
// stream produces an identical Projection. This catches non-deterministic
// projector logic but does NOT eliminate model nondeterminism (events are
// fixed at this point).
func ReplayDeterministic() Assertion

// HandoffOccurred asserts that a handoff from one role to another happened.
func HandoffOccurred(fromRole, toRole string) Assertion

// ResumeTokenIssued asserts that a ResumeToken was issued for the given
// subject (e.g., approval).
func ResumeTokenIssued(subject string) Assertion
```

## Composing assertions

```go
case := EvalCase{
    Name:    "research agent finds at least three sources",
    AgentID: "researcher",
    Input:   api.RunInput{Request: "Compare top three Go ORMs"},
    Timeout: 60 * time.Second,
    Assertions: []Assertion{
        TaskCompleted("researcher"),
        ToolCalled("web_search", 3),
        BlackboardHasItem(api.BlackboardSelector{
            ItemTypes: []api.BlackboardItemType{api.BlackboardItemEvidence},
            MinCount:  3,
        }),
        WithinBudget(10_000),
        NoHighRiskAction(),
    },
}
```

## Test harness integration

`eval/testing.go`:

```go
// AssertCasePasses is a t.Helper that runs the case and fails t if any
// assertion fails. Use this from `go test` files for inline eval suites.
func AssertCasePasses(t *testing.T, ctx context.Context, runner *hydaelyn.Runner, c EvalCase)

// AssertSuitePasses similarly for a suite.
func AssertSuitePasses(t *testing.T, ctx context.Context, runner *hydaelyn.Runner, cases []EvalCase)
```

## Dataset packaging

`eval/dataset.go`:

```go
// LoadDataset reads EvalCases from JSON/YAML files.
func LoadDataset(path string) ([]EvalCase, error)

// SaveDataset writes EvalCases to a file. Used to capture and share suites.
func SaveDataset(path string, cases []EvalCase) error
```

JSON schema for cases is published in `eval/schema/dataset.json`.

## Limitations stated explicitly

The framework's evaluation surface does **not** include:

- LLM-as-judge scoring (out of scope; users plug in their own)
- A/B testing infrastructure (out of scope; eval results feed into user analytics)
- Production canary frameworks (out of scope)
- Cost/quality Pareto analysis (out of scope)

These are eval-adjacent but belong in higher-layer packs or external tools.

## Verification

- `TestEvalCase_TaskCompletedAssertion`
- `TestEvalCase_ToolCalledAssertion_CountMatching`
- `TestEvalCase_WithinBudgetFailsWhenExceeded`
- `TestEvalCase_ReplayDeterministicCatchesProjectorBug`
- `TestEvalCase_RepeatsAllMustPass`
- `TestSuite_AggregatesPassedFailedCounts`
- `TestDataset_RoundTripJSON`
