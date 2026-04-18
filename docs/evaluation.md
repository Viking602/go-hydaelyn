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
events, err := runtime.TeamEvents(ctx, teamID)
if err != nil {
	return err
}
report := evaluation.Evaluate(events)
```

CLI support:

```bash
hydaelyn evaluate --events events.json
```
