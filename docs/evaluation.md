# Evaluation Framework

The `eval/` package makes an agent run testable by *executing* it through the
public `Runner` façade against a deterministic
[`provider/scripted`](../provider/scripted) model, then grading the resulting
`api.Run` with a typed assertion vocabulary. One eval surface, one assertion
vocabulary, one runner — used by package tests, by `_examples/evaluation`, and
by each pack's `eval_test.go`.

> This document describes the framework as it exists in the tree. Items not yet
> implemented are marked **planned**.

## Package layout

```
eval/
├── case.go              EvalCase, Assertion, AssertionFailure
├── harness.go           Harness, DefaultHarness, NewHarness + options, EmbeddingProvider
├── result.go            EvalResult, UsageSummary, SummarizeUsage
├── runner.go            Run, RunSuite, RunMatrix, MatrixParams, MatrixParam
├── assertions/          built-in eval.Assertion implementations
│   ├── output.go        OutputContains, OutputMatchesSchema
│   ├── status.go        RunTerminatedWithStatus, EventEmitted
│   ├── tool.go          ToolCalled, ToolNotCalled, ToolCalledNTimes, ToolCalledWithArg
│   ├── policy.go        PolicyDecisionAllowedBy, PolicyDecisionDeniedBy
│   ├── budget.go        WithinBudget
│   ├── duration.go      WithinDuration
│   ├── trace.go         BlackboardHasItem, ApprovalRequested
│   └── multiagent.go    AgentInstanceSpawned, SchedulerTookPath, HandoffOccurred,
│                        TeamTerminatedSuccessfully, NoNonIdempotentToolAutoRetried,
│                        BPBLikeMetric, CountMatcher, BPBScorer
├── matcher/             value comparators
│   ├── json.go          JSONContains, JSONMatchSchema
│   ├── string.go        ContainsSubstring, ContainsSubstringFold, RegexMatch
│   └── embedding.go     EmbeddingSimilarity (comparator only)
└── reporter/            result renderers
    ├── junit.go         JUnit
    ├── github.go        GitHub
    └── text.go          Text
```

`eval/` imports the root `github.com/Viking602/venat` façade plus
`provider/scripted`, `api`, and `multiagent`. Nothing imports `eval` except
`_test.go` files and per-pack `eval_test.go` files, so the dependency direction
stays acyclic.

## Core types

```go
package eval

type EvalCase struct {
    Name        string
    Description string
    Setup       func() Harness             // when nil, NewHarness() is used
    Input       api.StartRunCommand
    Timeout     time.Duration              // zero means no timeout
    Assertions  []Assertion
}

type Harness interface {
    Runner() *venat.Runner
    RegisterAgent(profile api.AgentProfile) error
    Cleanup()
    EmbeddingProvider() EmbeddingProvider // nil unless EmbeddingSimilarity is used
}

type EvalResult struct {
    Case     string
    Run      api.Run
    Passed   bool
    Failures []AssertionFailure
    Duration time.Duration
    Usage    UsageSummary
}

type Assertion interface {
    Name() string
    Check(ctx context.Context, run api.Run, harness Harness) error
}
```

`UsageSummary` is a typed rollup over `[]api.UsageRecord` (records,
input/output tokens, tool calls, credits, duration). It carries no loose `any`
fields. Fold a slice of records into one with `eval.SummarizeUsage(records)`.

### Harness

`eval.NewHarness(opts ...HarnessOption)` returns the reference `*DefaultHarness`,
backed by a fresh in-memory `venat.NewDevelopment()` runner and a deterministic scripted
provider. It registers a single agent (default id `"agent"`) that owns the
case's task. Options:

- `WithScript([]provider.Event)` — the scripted model's event stream. Defaults
  to a single text delta + done that completes immediately.
- `WithAgentID(string)` — the id the case dispatches its task to.
- `WithModel(string)` — the model name recorded by the worker bridge.
- `WithEmbeddingProvider(EmbeddingProvider)` — injects the provider returned by
  `Harness.EmbeddingProvider()`; only `EmbeddingSimilarity` needs one.

The framework drives the run end to end through the public surface
(`StartRun` → `CreateTask` → `DispatchTask` →
`worker.AgentWorker.ExecuteEnvelope` against the scripted provider → `AdvanceRun`
→ `TransitionRun`), then reads the terminal `api.Run` back. `Cleanup()` is a
no-op for the in-memory default harness.

## Runners

```go
func Run(t *testing.T, c EvalCase) EvalResult
func RunSuite(t *testing.T, cases []EvalCase) []EvalResult
func RunMatrix(t *testing.T, cases []EvalCase, params MatrixParams) []EvalResult
```

- `Run` executes one case and marks the test failed (via `t.Errorf`) for each
  failed assertion.
- `RunSuite` runs every case as its own subtest in declaration order.
- `RunMatrix` sweeps every case across every parameter set, producing one
  `EvalResult` per `(param, case)` combination. Combinations run as nested
  subtests named `<param>/<case>`, and each combination gets a distinct run id
  so a shared store never collides. With no params it degenerates to `RunSuite`
  under an implicit `default` param.

```go
type MatrixParams struct{ Params []MatrixParam }

type MatrixParam struct {
    Name  string
    Apply func(base EvalCase) EvalCase // nil runs the base case unchanged
}
```

The testing-agnostic core (`runCase`) does not import `testing`; the
`*testing.T` entry points are thin wrappers over it, so future pack self-checks
can reuse the core without welding the framework to `go test`. There is **no
CLI** — `eval/` is a library only.

## Built-in assertions

All built-in assertions live in `eval/assertions` and implement
`eval.Assertion`. They grade the executed run through the public `Runner`
façade exposed by the `Harness` (events, blackboard, tasks, usage).

### Single-agent (12)

| Assertion | Observes |
|---|---|
| `ToolCalled{Tool}` | tool invoked at least once (action-attempt event or tool-sourced blackboard item) |
| `ToolNotCalled{Tool}` | tool never invoked |
| `ToolCalledNTimes{Tool, Times}` | tool invoked exactly `Times` |
| `ToolCalledWithArg{Tool, Matcher}` | tool invoked with an argument satisfying a `matcher.Matcher` |
| `OutputMatchesSchema{Schema}` | run output validates against a JSON Schema (object `type`/`required` subset) |
| `OutputContains{Substring, CaseSensitive}` | run output contains a substring |
| `RunTerminatedWithStatus{Status}` | run ended with a specific `api.RunStatus` |
| `WithinBudget{MaxCredits}` | summed usage credits stay within the ceiling |
| `WithinDuration{Max}` | `CreatedAt..UpdatedAt` span stays within the bound |
| `PolicyDecisionAllowedBy{Policy}` | a named policy allowed an operation |
| `PolicyDecisionDeniedBy{Policy}` | a named policy denied/aborted an operation |
| `EventEmitted{Type}` | at least one event of an `api.EventType` was emitted |
| `BlackboardHasItem{Selector}` | at least one item matched an `api.BlackboardSelector` |
| `ApprovalRequested{Reason}` | a human approval was requested (optionally matching a reason substring) |

> Note: `ToolCalled`/`ToolNotCalled` count as one entry in the spec's "12";
> together with the rows above the table lists the full single-agent surface.

### Multi-agent (6)

| Assertion / constructor | Observes |
|---|---|
| `AgentInstanceSpawned{ClassName, Count}` / `AgentInstanceSpawnedWith(className, count ...CountMatcher)` | instances of a class spawned, bounded by an optional `CountMatcher` (default: at least one) |
| `SchedulerTookPath{Path}` / `SchedulerTookPathOf(path ...string)` | the scheduler's dispatch sequence (by class name) matched, in order |
| `HandoffOccurred{FromClass, ToClass}` | at least one typed handoff moved work between the named classes |
| `TeamTerminatedSuccessfully{}` | the run reached `RunStatusCompleted` (vs. failure, cancellation, budget exhaustion) |
| `NoNonIdempotentToolAutoRetried{NonIdempotentTools}` | no guarded tool started more than one distinct action attempt (ADR-015 §ToolSafety) |
| `BPBLikeMetric{Scorer, Threshold}` | an application-supplied `BPBScorer` returned a score ≥ `Threshold` |

`CountMatcher` is opaque; build one with `AtLeast(n)`, `AtMost(n)`, or
`Exactly(n)`.

`BPBScorer` is application-supplied — the framework owns only the pass/fail
comparison, the host owns what "quality" means:

```go
type BPBScorer interface {
    Score(ctx context.Context, run api.Run, harness eval.Harness) (float64, error)
}
```

#### Multi-agent event payload convention

The multi-agent assertions read the run's event stream
(`Runner.RunEvents`) and key off camelCase payload fields, mirroring the
action-attempt convention (`EventActionAttemptStarted` carries `toolName`):

| Event type | Payload key(s) read |
|---|---|
| `multiagent.EventAgentInstanceCreated` | `className` |
| `multiagent.EventDispatchEmitted` | `className` |
| `multiagent.EventTypedHandoff` | `from`, `to` |
| `api.EventActionAttemptStarted` | `toolName` |

An application driving a `Team` need only append these events with the
documented payload keys for the multi-agent assertions to observe its
scheduler, handoffs, and instance spawns.

## Matchers

```go
package matcher

type Matcher interface {
    Match(actual any) (bool, string) // (true, "") on match; (false, detail) otherwise
}

func JSONContains(partial any) Matcher                                          // deep structural containment
func JSONMatchSchema(schema json.RawMessage) Matcher                            // JSON Schema keyword subset
func RegexMatch(pattern string) Matcher                                         // regexp over rendered text
func ContainsSubstring(s string) Matcher                                        // case-sensitive substring
func ContainsSubstringFold(s string) Matcher                                    // case-insensitive substring
func EmbeddingSimilarity(reference string, threshold float64, p EmbeddingProvider) Matcher
```

`Match` takes `actual any` because it is a genuine generic comparison over
heterogeneous observed values (tool arguments, blackboard payloads, run output);
every return is typed.

`JSONMatchSchema` supports the same keyword subset as the agent loop's
`OutputPolicy` validator: `type`
(`object`/`array`/`string`/`number`/`integer`/`boolean`), `properties`,
`required`, `items`, `enum`, and `additionalProperties`.

`EmbeddingSimilarity` is the only matcher with a host dependency. The framework
ships the cosine-similarity comparator but no embedding model; applications
inject one via `Harness.EmbeddingProvider()` (`WithEmbeddingProvider`). A nil
provider is reported as a mismatch rather than panicking.

## Reporters

A reporter renders `[]eval.EvalResult` for CI and humans. `reporter` imports
`eval`; `eval` never imports `reporter`.

- `reporter.JUnit{SuiteName}` — `Render(results) ([]byte, error)` emits JUnit
  XML that validates against the JUnit schema.
- `reporter.GitHub{Title}` — `Render(results) string` / `Write(w, results)`
  emits GitHub Actions workflow commands (`::error::` per failed assertion, a
  trailing `::notice::` with totals).
- `reporter.Text{ShowPassing}` — `Render(results) string` / `Write(w, results)`
  emits a plain-text PASS/FAIL summary with a totals line.

## Example

```go
package mypack_test

import (
    "testing"

    "github.com/Viking602/venat/api"
    "github.com/Viking602/venat/eval"
    "github.com/Viking602/venat/eval/assertions"
    "github.com/Viking602/venat/provider"
)

func TestSummaryQuality(t *testing.T) {
    cases := []eval.EvalCase{{
        Name:        "summary-quality",
        Description: "produce a concise summary",
        Setup: func() eval.Harness {
            return eval.NewHarness(eval.WithScript([]provider.Event{
                {Kind: provider.EventTextDelta, Text: "summary complete"},
                {Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
            }))
        },
        Input: api.StartRunCommand{RunID: "eval-run", RootTaskID: "root", Request: "summarize a task"},
        Assertions: []eval.Assertion{
            assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
            assertions.OutputContains{Substring: "summary"},
            assertions.EventEmitted{Type: api.EventTaskCompleted},
        },
    }}
    eval.RunSuite(t, cases)
}
```

A runnable variant (without `*testing.T`) lives at
[`_examples/evaluation/main.go`](../_examples/evaluation/main.go); run it with
`go run ./_examples/evaluation`.

## Integration with packs

Pack production code does not carry eval cases. Each pack keeps its smoke
suite in `_test.go` and runs it through `eval.RunSuite(t, cases)`. See
[`packs/research/eval_test.go`](../packs/research/eval_test.go) for the
canonical per-pack self-check. Swapping the harness's scripted provider for a
live one turns a smoke suite into a quality gate without changing the case
shape.

## Non-goals

- Hosted eval dashboard — integrate with existing CI dashboards via reporters.
- Eval-as-a-service — the framework is a library only; there is no `venat`
  eval CLI.
- Synthetic data generation — the application's call.
- Artifact bundles, gate-policy rollups, baseline regression comparison, and
  flaky-quota management are out of scope (see the
  [plan](plans/eval-harness-growth.md) §2).

## Planned

- **`eval/README.md`** — the spec lists a package-level README that has not yet
  landed; this document is the reference until it does.
- **Richer matrix axes** — `RunMatrix` currently sweeps caller-supplied
  parameter sets. Built-in model/provider sweep helpers are planned.
