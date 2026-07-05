# 10 — Evaluation Framework

> Renumbered from `08-evaluation.md`. The v0.8.0 change is the
> addition of multi-agent assertions (SchedulerTookPath,
> HandoffOccurred, AgentInstanceSpawned, TeamTerminatedSuccessfully,
> NoNonIdempotentToolAutoRetried, BPBLikeMetric — borrowed from
> autoresearch per master spec §11). The single-agent surface and
> assertion vocabulary are preserved.

## Goal

Make every Agent / Recipe / Pack testable by the framework, not by ad-hoc Go tests scattered through `_examples/`. One eval surface, one assertion vocabulary, one runner.

## Package

```
eval/
├── README.md
├── case.go           (EvalCase type)
├── runner.go         (eval.Run, eval.RunSuite)
├── result.go         (EvalResult)
├── assertions/
│   ├── output.go     (output content / structure)
│   ├── tool.go       (tool invocation)
│   ├── policy.go     (policy decision)
│   ├── budget.go     (within budget)
│   ├── duration.go   (within time)
│   ├── trace.go      (trace shape)
│   └── multiagent.go (NEW — multi-agent specific)
├── matcher/
│   ├── json.go       (JSONContains, JSONMatchSchema)
│   ├── string.go     (RegexMatch, ContainsSubstring)
│   └── embedding.go  (EmbeddingSimilarity > threshold)
└── reporter/
    ├── junit.go
    ├── github.go
    └── text.go
```

## Core types

```go
package eval

type EvalCase struct {
    Name        string
    Description string
    Setup       func(t *testing.T) Harness
    Input       api.RunInput
    Timeout     time.Duration
    Assertions  []Assertion
}

type Harness interface {
    Runner() *hydaelyn.Runner
    RegisterAgent(profile api.AgentProfile)
    Cleanup()
}

type EvalResult struct {
    Case       string
    Run        api.Run
    Passed     bool
    Failures   []AssertionFailure
    Duration   time.Duration
    Usage      api.UsageSummary
}

type Assertion interface {
    Name() string
    Check(ctx context.Context, run api.Run, harness Harness) error
}
```

## Built-in assertions

Single-agent (carry forward, 12 assertions):

- `ToolCalled(name string)` / `ToolNotCalled(name string)`
- `ToolCalledWithArg(name string, matcher Matcher)`
- `ToolCalledNTimes(name string, n int)`
- `OutputMatchesSchema(schema json.RawMessage)`
- `OutputContains(matcher Matcher)`
- `RunTerminatedWithStatus(status api.RunStatus)`
- `WithinBudget(maxCredits int64)`
- `WithinDuration(maxDuration time.Duration)`
- `PolicyDecisionAllowedBy(name string)` / `PolicyDecisionDeniedBy(name string)`
- `EventEmitted(kind api.EventKind)`
- `BlackboardHasItem(sel api.BlackboardSelector)`
- `ApprovalRequested(name string)`

Multi-agent (NEW, 6 assertions):

- `AgentInstanceSpawned(className string, count ...CountMatcher)` —
  asserts one or more Instances of a class spawned. `CountMatcher`
  includes `AtLeast(n)`, `AtMost(n)`, `Exactly(n)`.
- `SchedulerTookPath(path ...string)` — asserts the Scheduler's
  Dispatch sequence (by ClassName) matched the given path. Useful
  for SequentialScheduler / RouterScheduler validation.
- `HandoffOccurred(fromClass, toClass string)` — asserts at least
  one Handoff from a `fromClass` instance to a `toClass` instance.
- `TeamTerminatedSuccessfully()` — asserts the Team's terminal
  status is success (vs scheduler failure, vs budget exhausted).
- `NoNonIdempotentToolAutoRetried()` — v0.9.0-reserved assertion that
  guarded non-idempotent tool actions do not create multiple distinct
  action attempts without Approval. It does not imply `agent.Engine`
  consumes `ToolSafety` in v0.8.0.
- `BPBLikeMetric(scorer eval.BPBScorer, threshold float64)` —
  autoresearch-borrowed quality metric template. Scorer is
  application-supplied; framework provides the harness and the
  pass/fail comparator.

## Multi-agent EvalCase example

See `16-multi-agent-demo.md` for a full end-to-end case using
`AgentInstanceSpawned`, `HandoffOccurred`, `BlackboardHasItem`,
`ToolCalled`, `ApprovalRequested`, `NoNonIdempotentToolAutoRetried`,
`WithinBudget`, `TeamTerminatedSuccessfully`.

## Matchers

```go
package matcher

type Matcher interface {
    Match(actual any) (bool, string)
}

// Built-in matchers
func JSONContains(partial any) Matcher
func JSONMatchSchema(schema json.RawMessage) Matcher
func RegexMatch(pattern string) Matcher
func ContainsSubstring(s string) Matcher
func EmbeddingSimilarity(reference string, threshold float64) Matcher
```

`EmbeddingSimilarity` is the only matcher that requires a provider — applications inject one via `Harness.EmbeddingProvider()`. The framework ships only the comparator.

## Runners

```go
func Run(t *testing.T, c EvalCase) EvalResult
func RunSuite(t *testing.T, cases []EvalCase) []EvalResult
func RunMatrix(t *testing.T, cases []EvalCase, params MatrixParams) []EvalResult
```

`RunMatrix` runs the same cases across N models / providers / parameter sets for regression sweeps.

## Reporters

- `reporter.JUnit` — JUnit XML for CI
- `reporter.GitHub` — GitHub Checks annotations
- `reporter.Text` — terminal-friendly summary

## Integration with packs

Each `packs/<domain>/` ships an `eval_test.go` that calls
`eval.RunSuite(t, cases)` with domain-specific cases. The incident
triage demo (`16-multi-agent-demo.md`) is the canonical example.

## Non-goals

- Hosted eval dashboard. Out of scope; integrate with existing CI dashboards via reporters.
- Eval-as-a-service. Framework only.
- Synthetic data generation. Application's call.

## Verification

- `TestEvalRun_PassingCaseReturnsPassed`
- `TestEvalRun_FailingAssertionFailsCase`
- `TestEvalRun_TimeoutMarksFailed`
- `TestEvalRunSuite_ParallelExecution`
- `TestEvalRunMatrix_AllParamCombinationsExecuted`
- `TestAssertion_ToolCalledWithArg_JSONMatcher`
- `TestAssertion_AgentInstanceSpawned_AtLeastMatcher`
- `TestAssertion_SchedulerTookPath_SequentialMatch`
- `TestAssertion_HandoffOccurred_DetectsTypedHandoff`
- `TestAssertion_NoNonIdempotentToolAutoRetried_DetectsViolation`
- `TestAssertion_TeamTerminatedSuccessfully`
- `TestAssertion_BPBLikeMetric_ScorerThresholdComparator`
- `TestReporter_JUnit_OutputValidatesAgainstXSD`
