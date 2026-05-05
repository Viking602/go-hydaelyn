# Task Dataflow

## Why This Exists

Hydaelyn already had parallel task execution. The missing piece was explicit task-to-task dataflow.

The runtime now models:

- what a task reads
- what a task writes
- where outputs are published
- how replay rebuilds those outputs later

## Public Fields

### Planner Output

Planner output is exposed through `api.TodoPlan` and `api.TodoItem`. A todo
item can describe what a runtime task should read and write:

- `Reads []string`
- `Writes []string`

### Runtime Task Command

`api.CreateTaskCommand` maps dataflow intent onto the Run/Task runtime:

- `ReadSelectors []api.BlackboardSelector`
- `WriteTargets []string`

The persisted `api.Task` mirrors those fields so dispatch and replay can see
the same contract.

### Task Report

`api.TypedReport` carries the worker result. Reports can include summary text,
structured blackboard items, action outcomes, handoff context, and status values
such as `success`, `blocked`, `needs_approval`, or `needs_clarification`.

## Output Visibility

Runtime output is represented by explicit stores instead of the old
`private/shared/blackboard` publish enum:

- private execution context lives in task state, leases, traces, and events
- shared agent facts are written as `api.BlackboardItem`
- user-visible output is queued as `api.UserMessage` through the response outbox

## Blackboard Exchanges

`api.BlackboardItem` is the generic task exchange surface.

Each item records:

- `key`
- `taskId`
- `type`
- `content`
- optional payload
- optional artifact refs
- optional evidence refs
- visibility

This does not replace the research evidence model. Claims, findings, evidence, and verifications still exist and remain the verification-native surface.

## Runtime Flow

For a task with explicit dataflow:

1. runtime materializes `Reads` from the blackboard
2. the materialized inputs are appended to the private task session
3. the task runs normally
4. runtime extracts structured output and artifact refs
5. runtime writes blackboard items or queues response output through commands
6. events are recorded for replay

## Replay

Replay now reconstructs:

- task outputs
- artifact refs
- blackboard items
- verification results

This makes task-level synthesis and inspection deterministic over the event log.

## Deepsearch

`deepsearch` now uses the same contract:

- research tasks publish named outputs
- verify tasks read research outputs explicitly
- the final synthesize task reads `supported_findings`

The pattern is no longer hard-coded to finish by string-concatenating research summaries immediately after verification.
