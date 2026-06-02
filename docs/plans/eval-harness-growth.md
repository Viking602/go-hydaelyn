# Plan — Grow `eval/` into the spec's Harness-based framework

> Status: `active` (approved 2026-06-01)
> Target spec: [`docs/product-spec/v0.8.0/10-evaluation.md`](../product-spec/v0.8.0/10-evaluation.md)
> Supersedes the stale narrative in [`docs/evaluation.md`](../evaluation.md)

This document is the single source of truth for the eval-harness work. Each
implementation step reads this file for the API shape, the verified type
mapping, and the milestone boundaries. **No step may invent type names — use
the verified mapping in §3.**

## 1. Why this work exists

The repo currently holds **four contradictory definitions** of what "evaluation"
is:

1. **Current code** (`eval/eval.go` + `eval/assert/`): a passive assertion
   helper. Its own doc comment says "It is not a benchmark harness… the grader
   does NOT execute the agent run." Four assertions: Contains, Equals, Regex,
   JudgedBy.
2. **`docs/evaluation.md`** (stale, orphaned): documents `evaluation.Evaluate`,
   `evalrun.Run`, `evalrun.RunSuite`, `hydaelyn run-deterministic`, and a 10-file
   artifact bundle. **Every package and CLI command it names was deleted.**
3. **`docs/product-spec/v0.8.0/10-evaluation.md`** (canonical spec): a third,
   richer design — a `Harness` that *executes* the agent run, 18 assertions
   (12 single-agent + 6 multi-agent), matchers, and JUnit/GitHub/Text reporters.
4. **The legacy deterministic harness** (deleted, recoverable from git at
   `a3d8f0d^`): `gate.go`, `judge.go`, `groundedness.go`, `citation.go`,
   `refusal.go`, `temporal.go`, `manifest.go`, `policy.go`, `score.go`,
   `replay.go`. Removed deliberately across `17e2da4 → d57f04e → a3d8f0d →
   9cb5397 (feat!: drop legacy/ tree)` as part of the v0.8.0 "framework
   purification / publishable framework" effort.

**Decision: grow the current `eval/` into definition #3 (the canonical spec).**
Commit `27f1280` calls today's `eval/` an "evaluation framework skeleton"; this
plan completes the skeleton into the documented framework. We explicitly do
**not** revive definition #4.

## 2. Scope

### In scope
- Rework `eval/` so the framework *runs* a case via a `Harness` backed by a
  deterministic `provider/scripted` provider, instead of grading a
  caller-supplied `Outcome`.
- `EvalCase`, `EvalResult`, `Harness`, `Run`/`RunSuite`/`RunMatrix`.
- 12 single-agent assertions + 6 multi-agent assertions (§5).
- `matcher/` package (JSON, string, embedding-similarity comparator).
- `reporter/` package (JUnit XML, GitHub Checks, text).
- Rewrite `docs/evaluation.md` and fix README to describe only what exists.

### Out of scope (deliberately — these are the deleted #4 surface and the spec's
own non-goals)
- ❌ Artifact bundles (`events.json` / `state.replayed.json` / `score.json` /
  `summary.md` files).
- ❌ Gate policy, score rollup files, `hydaelyn eval gate`, **any `hydaelyn
  eval *` CLI subcommand**.
- ❌ Baseline regression comparison, flaky-quota management, historical archival.
- ❌ Hosted eval dashboard, eval-as-a-service, synthetic data generation
  (spec non-goals).

The CLI change in this plan is **only** removing the phantom command docs.

## 3. Verified type mapping (spec name → real `api/` type)

The spec drifted from the real API surface. These substitutions are **mandatory**:

| Spec `10-evaluation.md` says | Reality | Use this |
|---|---|---|
| `api.RunInput` | does not exist | `api.StartRunCommand` (field `Request string`) |
| `api.UsageSummary` | does not exist | `[]api.UsageRecord` + a typed summary struct (no loose `any`, ADR-009) |
| `api.EventKind` | does not exist | `api.EventType` |
| `api.RunStatus` | exists (`api/status.go`) | use directly |
| `api.BlackboardSelector`, `api.BlackboardItem`, `api.AgentProfile`, `api.Run`, `api.Task` | exist | use directly |

Verified Runner façade methods available to back assertions/harness:
`StartRun(ctx, api.StartRunCommand) (api.Run, api.Task, error)`,
`Run(ctx, runID) (api.Run, error)`, `RunEvents(ctx, runID) ([]api.Event, error)`,
`ListEvents`, `RegisterAgent(api.AgentProfile)`, `RegisterTool(api.Tool)`,
`SelectItems(ctx, runID, api.BlackboardSelector)`, `InvokeTool`,
`RequestApproval`, `StartActionAttempt`, `TraceSpans(runID) []api.TraceSpan`.

Deterministic provider:
`provider/scripted.New([]provider.Event) *ScriptedProvider` and
`provider/scripted.LoadScript(path) ([]provider.Event, error)`.

## 4. Architecture & dependency direction (gate-safe)

`eval/` will import the root `github.com/Viking602/go-hydaelyn` façade plus
`provider/scripted`, `api`, and `multiagent`. This is **precedented and
gate-safe**:

- `.sentrux/rules.toml` constrains only `internal/*` and service/domain packages
  (those must not import the root/orchestrator/worker). Top-level extension
  packages are unrestricted.
- `worker/` already imports the root façade (`worker/worker.go`). `eval/` follows
  the same pattern.
- Gate constraints to respect: `max_cycles = 0` (root must never import `eval`),
  `max_coupling = "B"` per file, `no_god_files` (≤15 imports/file). Splitting
  into `assertions/`, `matcher/`, `reporter/` subpackages naturally satisfies
  these.
- ADR-009: `Matcher.Match(actual any)` uses `any` as a **parameter** (genuine
  generic comparison) — allowed. All returns and exported fields stay typed.

### Data flow (acyclic)

```
EvalCase ──Setup()──▶ Harness ──┬─ Runner() (hydaelyn.New)
   │                            └─ provider/scripted (deterministic model output)
   │
   └─Input(api.StartRunCommand)─▶ [run agent loop to terminal RunStatus]
                                          │
                              api.Run + RunEvents + Blackboard + Usage
                                          │
                              Assertion.Check(ctx, run, harness)
                                          │
                       []AssertionFailure ─▶ EvalResult ─▶ Reporter{JUnit,GitHub,Text}
```

Dependency arrows point downward only; nothing imports `eval` except `_test.go`
files and future `packs/<domain>/eval_test.go`.

## 5. Target API (from the spec, with §3 substitutions applied)

```go
package eval

type EvalCase struct {
    Name        string
    Description string
    Setup       func(t *testing.T) Harness
    Input       api.StartRunCommand // was api.RunInput
    Timeout     time.Duration
    Assertions  []Assertion
}

type Harness interface {
    Runner() *hydaelyn.Runner
    RegisterAgent(profile api.AgentProfile)
    Cleanup()
    // EmbeddingProvider is optional; only EmbeddingSimilarity matcher needs it.
    EmbeddingProvider() EmbeddingProvider
}

type EvalResult struct {
    Case     string
    Run      api.Run
    Passed   bool
    Failures []AssertionFailure
    Duration time.Duration
    Usage    UsageSummary // typed summary computed from []api.UsageRecord
}

type Assertion interface {
    Name() string
    Check(ctx context.Context, run api.Run, harness Harness) error
}
```

**Single-agent assertions (12):** `ToolCalled` / `ToolNotCalled`,
`ToolCalledWithArg`, `ToolCalledNTimes`, `OutputMatchesSchema`, `OutputContains`,
`RunTerminatedWithStatus(api.RunStatus)`, `WithinBudget`, `WithinDuration`,
`PolicyDecisionAllowedBy` / `PolicyDecisionDeniedBy`, `EventEmitted(api.EventType)`,
`BlackboardHasItem(api.BlackboardSelector)`, `ApprovalRequested`.

**Multi-agent assertions (6):** `AgentInstanceSpawned`, `SchedulerTookPath`,
`HandoffOccurred`, `TeamTerminatedSuccessfully`, `NoNonIdempotentToolAutoRetried`,
`BPBLikeMetric`.

**Matchers:** `JSONContains`, `JSONMatchSchema`, `RegexMatch`, `ContainsSubstring`,
`EmbeddingSimilarity` (comparator only; provider injected via Harness).

**Reporters:** `reporter.JUnit`, `reporter.GitHub`, `reporter.Text`.

**Runners:** `Run(t *testing.T, c EvalCase) EvalResult`,
`RunSuite(t *testing.T, cases []EvalCase) []EvalResult`,
`RunMatrix(t *testing.T, cases []EvalCase, params MatrixParams) []EvalResult`.

## 6. Key decisions

1. **Clean break, no compat shim.** The current `Outcome` / `Eval` /
   `Run(supply func…)` API and the 4 assertions are *replaced* by the spec
   signatures. Justified: pre-1.0 (v0.8.0), the code is officially a "skeleton",
   and a compat layer would create a fifth narrative. The 4 existing assertions
   are re-expressed under the new model (`OutputContains` + matchers).
2. **Core is testing-agnostic; `*testing.T` is a thin outer wrapper.** Internal
   `runCase(ctx, c) EvalResult` does not import `testing`; `Run(t, c)` wraps it.
   This satisfies the spec's `testing.T` entry point without welding the
   framework to `go test`, leaving future `packs/` self-checks able to reuse the
   core. (It is still **not** a CLI — that stays out of scope.)
3. **Multi-agent assertions land last.** They depend on `multiagent/`
   scheduler/handoff/instance event streams and are the hardest to reproduce
   deterministically. Single-agent loop closes first.
4. **`docs/evaluation.md` is rewritten, not deleted**; richer spec items not yet
   implemented are marked `planned`. README example/table wording aligns to
   "Evaluation framework".
5. **Nothing is committed or pushed by the implementation run.** Work lands in
   the working tree on a feature branch for human review.

## 7. Milestones

### M0 — Wiring spike (GATE, ~1–2 days)
Prove a `provider/scripted` provider can drive a full agent run to a terminal
`api.RunStatus` through the public surface (`hydaelyn.New(...)` + `provider/scripted`
+ whatever `agent.Engine` / `worker` wiring is required).
- **Deliverable:** one end-to-end test that starts a run with a scripted script
  and asserts it reaches a terminal status, plus a short written note of the exact
  wiring used.
- **Premise-collapse check:** if there is no clean public way to wire the scripted
  provider into a Runner and drive the loop to terminal, **STOP** and report back —
  do not start M1. B's `Harness` is not viable as-specified and the approach must
  change.

### M1 — Core types + Harness + carry-over assertions
`eval/case.go` (`EvalCase`), `eval/result.go` (`EvalResult`, `UsageSummary`),
`eval/harness.go` (`Harness` + default impl using M0's wiring),
`eval/runner.go` (`runCase`, `Run`, `RunSuite`). First assertions:
`OutputContains`, `OutputMatchesSchema`, `RunTerminatedWithStatus`,
`EventEmitted`. Rewrite `eval/eval_test.go` and `_examples/evaluation/main.go`
to the new API. Gate: `make verify`.

### M2 — Remaining single-agent assertions + matcher package
`eval/assertions/{tool,policy,budget,duration,trace}.go` (the other 8 assertions),
`eval/matcher/{json,string,embedding}.go`. `EmbeddingSimilarity` ships only the
comparator; provider via `Harness.EmbeddingProvider()`. Tests per assertion.
Gate: `make verify`.

### M3 — Reporter package
`eval/reporter/{junit,github,text}.go`. JUnit output validates against the JUnit
XSD. Gate: `make verify`.

### M4 — `RunMatrix` + multi-agent assertions
`eval/runner.go` gains `RunMatrix` + `MatrixParams`.
`eval/assertions/multiagent.go`: `AgentInstanceSpawned`, `SchedulerTookPath`,
`HandoffOccurred`, `TeamTerminatedSuccessfully`, `NoNonIdempotentToolAutoRetried`,
`BPBLikeMetric` (scorer supplied by application; framework provides harness +
comparator). Reads `multiagent/` event streams. Gate: `make verify`.

### M5 — Documentation close-out (P0 fix)
Rewrite `docs/evaluation.md` to describe the real `eval/` framework; mark
not-yet-built spec items `planned`. Fix README: the `_examples/evaluation` row
and the `eval/` row to "Evaluation framework"; keep the `docs/evaluation.md`
link. Add one `packs/<domain>/eval_test.go` sample calling `RunSuite`. Gate:
`make ci-local`.

## 8. Verification (from the spec's own list)

`TestEvalRun_PassingCaseReturnsPassed`, `TestEvalRun_FailingAssertionFailsCase`,
`TestEvalRun_TimeoutMarksFailed`, `TestEvalRunSuite_ParallelExecution`,
`TestEvalRunMatrix_AllParamCombinationsExecuted`,
`TestAssertion_ToolCalledWithArg_JSONMatcher`,
`TestAssertion_AgentInstanceSpawned_AtLeastMatcher`,
`TestAssertion_SchedulerTookPath_SequentialMatch`,
`TestAssertion_HandoffOccurred_DetectsTypedHandoff`,
`TestAssertion_NoNonIdempotentToolAutoRetried_DetectsViolation`,
`TestAssertion_TeamTerminatedSuccessfully`,
`TestAssertion_BPBLikeMetric_ScorerThresholdComparator`,
`TestReporter_JUnit_OutputValidatesAgainstXSD`.

Gates: `make verify` between milestones; `make ci-local` (incl.
`architecture-check`: confirm `eval→root` is acyclic, coupling ≤ B, no god file)
at the end.

## 9. Rollback & blast radius
- > 8 files, 3 new subpackages (`assertions/`, `matcher/`, `reporter/`) — large,
  acknowledged.
- Code + docs only. No storage schema, no migration, no external state. Any step
  is `git revert`-clean.
- The only breaking change is the `eval` public API signature, contained to this
  repo (`eval_test.go`, `_examples/evaluation`, future packs).

## 10. Deferred unknowns (explicit, with owner)
- **Scripted-provider ↔ agent-loop wiring** — owner: M0 spike; exit criterion:
  one case reaches terminal status. Front-loaded as the gating milestone.
- **`BPBScorer` interface shape** — owner: M4; deferred because it does not block
  the first 12 assertions. Spec: scorer is application-supplied, framework gives
  harness + pass/fail comparator.
