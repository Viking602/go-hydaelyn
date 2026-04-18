# Task Dataflow

## Why This Exists

Hydaelyn already had parallel task execution. The missing piece was explicit task-to-task dataflow.

The runtime now models:

- what a task reads
- what a task writes
- where outputs are published
- how replay rebuilds those outputs later

## Public Fields

### Planner

`planner.TaskSpec` now supports:

- `Reads []string`
- `Writes []string`
- `Publish []team.OutputVisibility`

### Runtime Task

`team.Task` mirrors the same fields:

- `Reads`
- `Writes`
- `Publish`

### Task Result

`team.Result` now includes:

- `Structured map[string]any`
- `ArtifactIDs []string`

## Output Visibility

Supported publish targets:

- `private`
- `shared`
- `blackboard`

If `Publish` is omitted, runtime preserves the old compatibility path and still emits a shared summary message.

## Blackboard Exchanges

`blackboard.State.Exchanges` is the generic task exchange surface.

Each exchange records:

- `key`
- `taskId`
- `valueType`
- `text`
- optional structured payload
- optional artifact refs
- optional claim/finding refs

This does not replace the research evidence model. Claims, findings, evidence, and verifications still exist and remain the verification-native surface.

## Runtime Flow

For a task with explicit dataflow:

1. runtime materializes `Reads` from the blackboard
2. the materialized inputs are appended to the private task session
3. the task runs normally
4. runtime extracts structured output and artifact refs
5. runtime publishes outputs to requested destinations
6. events are recorded for replay

## Replay

Replay now reconstructs:

- task outputs
- artifact refs
- named exchanges
- verification results

This makes task-level synthesis and inspection deterministic over the event log.

## Deepsearch

`deepsearch` now uses the same contract:

- research tasks publish named outputs
- verify tasks read research outputs explicitly
- the final synthesize task reads `supported_findings`

The pattern is no longer hard-coded to finish by string-concatenating research summaries immediately after verification.
