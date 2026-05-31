# ADR-002 Task DAG and `FailurePolicy`

## Status

Accepted

## Context

The repository previously had three semantic gaps:

- `executeTasks` would execute all pending tasks, rather than only runnable tasks
- The task graph had no systematic validation; cyclic dependencies, missing dependencies, and duplicate IDs could all quietly slip into the running state
- After a task failed, the pattern could still continue to aggregate, resulting in silent degradation

## Decision

- `RunState.Validate()` is responsible for validating the task graph for duplicate IDs, missing dependencies, cyclic dependencies, and assignee validity
- `Runtime.executeTasks()` only schedules `RunnableTasks()`
- `Task` introduces `FailurePolicy`
- Four policy classes are currently supported: `fail_fast`, `retry`, `degrade`, `skip_optional`
- For a pending task whose blocking dependency has failed, the runtime first resolves it into `failed` or `skipped`, to avoid deadlocking the team
- Once a blocking failure occurs, the team immediately enters `failed`, and further aggregation is prohibited

## Impact

- The execution semantics of linear, parallel, and diamond DAGs are predictable
- Failed dependencies will not be executed prematurely
- Failure is reclaimed from the pattern stitching layer back into the runtime semantic layer

## Follow-up

- `retry` is currently a basic capability; later, in the v0.7 durable runtime, it must interlock with lease, idempotency, and checkpoint
- `degrade` and `skip_optional` still need to coordinate with the verifier/synthesizer for finer-grained output control
