# Evaluation

## Purpose

The in-tree `evaluation` package turns runtime events into a baseline report that can be used in tests, smoke checks, and CI.

## Current Metrics

- `taskCompletionRate`
- `blockingFailureRate`
- `retrySuccessRate`
- `supportedClaimRatio`
- `synthesisInputCoverage`
- `endToEndLatency`
- `toolCallCount`
- `tokenBudgetHitRate`

## Data Source

`evaluation.Evaluate(events)` consumes the persisted event stream and uses replayed state where needed.

That means the metrics depend on runtime recording:

- task lifecycle events
- task input/output dataflow events
- verification deltas
- task budget and usage payloads

## Example

```go
events, err := runner.TeamEvents(ctx, teamID)
if err != nil {
	return err
}
report := evaluation.Evaluate(events)
```

## Deterministic Cases

`evalrun.Run` executes one scripted case and writes a replayable artifact bundle:

- `events.json`
- `state.final.json`
- `state.replayed.json`
- `answer.txt`
- `tool_calls.jsonl`
- `model_events.jsonl`
- `evaluation.report.json`
- `quality.score.json`
- `score.json`
- `summary.md`

`evalrun.RunSuite` and `hydaelyn run-deterministic` batch a directory of case files and emit suite-level artifacts under `suites/<name>/<timestamp>/`:

- `suite.json`
- `cases.json`
- `score.json`
- `capability.report.json`
- `summary.md`

## CLI

Single score report generation:

```bash
hydaelyn evaluate --events events.json
```

Deterministic suite execution:

```bash
go run ./cmd/hydaelyn run-deterministic \
  --case-dir evalcase/testdata \
  --workspace .
```
